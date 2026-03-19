// Package metrics provides runtime metrics collection and monitoring capabilities.
//
// This package re-exports types and constants from minIODB/pkg/throttle for convenience:
//   - LoadLevel: Represents system load level (Idle, Normal, Busy, Overloaded)
//   - LoadThresholds: Configuration thresholds for load classification
//   - Load* constants: Load level values (LoadIdle, LoadNormal, LoadBusy, LoadOverloaded)
//
// The RuntimeCollector periodically collects runtime metrics including memory usage,
// goroutine count, GC pause times, and connection pool statistics. These metrics
// are used for system health monitoring and load-based throttling decisions.
package metrics

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"minIODB/pkg/throttle"
)

type LoadLevel = throttle.LoadLevel

const (
	LoadIdle       = throttle.LoadIdle
	LoadNormal     = throttle.LoadNormal
	LoadBusy       = throttle.LoadBusy
	LoadOverloaded = throttle.LoadOverloaded
)

type LoadThresholds = throttle.LoadThresholds

func DefaultLoadThresholds() LoadThresholds {
	return throttle.DefaultLoadThresholds()
}

const (
	throttleRatioIdle       = 0.0
	throttleRatioNormal     = 0.3
	throttleRatioBusy       = 0.7
	throttleRatioOverloaded = 1.0
)

type RuntimeSnapshot struct {
	Goroutines     int       `json:"goroutines"`
	HeapAllocMB    float64   `json:"heap_alloc_mb"`
	HeapSysMB      float64   `json:"heap_sys_mb"`
	GCPauseMs      float64   `json:"gc_pause_ms"`
	NumGC          uint32    `json:"num_gc"`
	MinIOPoolUsage float64   `json:"minio_pool_usage"`
	PendingWrites  int64     `json:"pending_writes"`
	LoadLevel      LoadLevel `json:"load_level"`
	ThrottleRatio  float64   `json:"throttle_ratio"`
	CPUPercent     float64   `json:"cpu_percent"`
	UptimeSeconds  float64   `json:"uptime_seconds"`
	CollectedAt    time.Time `json:"collected_at"`
	Stale          bool      `json:"stale"`
}

func (s *RuntimeSnapshot) GetLoadLevel() LoadLevel {
	return s.LoadLevel
}

func (s *RuntimeSnapshot) GetThrottleRatio() float64 {
	return s.ThrottleRatio
}

type RuntimeCollector struct {
	snapshot            atomic.Pointer[RuntimeSnapshot]
	poolStatsProvider   func() (activeConns, maxConns int64)
	bufferStatsProvider func() int64
	thresholds          LoadThresholds
	interval            time.Duration
	stopCh              chan struct{}
	stopOnce            sync.Once
	wg                  sync.WaitGroup
	running             atomic.Bool
	startTime           time.Time
	lastCPUTime         time.Duration
	lastCollectTime     time.Time
}

var globalRuntimeCollector atomic.Pointer[RuntimeCollector]

func NewRuntimeCollector(opts ...RuntimeCollectorOption) *RuntimeCollector {
	now := time.Now()
	rc := &RuntimeCollector{
		thresholds:      DefaultLoadThresholds(),
		interval:        5 * time.Second,
		stopCh:          make(chan struct{}),
		startTime:       now,
		lastCPUTime:     getCPUTime(),
		lastCollectTime: now,
	}
	for _, opt := range opts {
		opt(rc)
	}
	rc.snapshot.Store(&RuntimeSnapshot{
		LoadLevel:     LoadIdle,
		CollectedAt:   now,
		UptimeSeconds: 0,
	})
	return rc
}

type RuntimeCollectorOption func(*RuntimeCollector)

func WithPoolStatsProvider(provider func() (activeConns, maxConns int64)) RuntimeCollectorOption {
	return func(rc *RuntimeCollector) {
		rc.poolStatsProvider = provider
	}
}

func WithBufferStatsProvider(provider func() int64) RuntimeCollectorOption {
	return func(rc *RuntimeCollector) {
		rc.bufferStatsProvider = provider
	}
}

func WithThresholds(thresholds LoadThresholds) RuntimeCollectorOption {
	return func(rc *RuntimeCollector) {
		rc.thresholds = thresholds
	}
}

func WithInterval(interval time.Duration) RuntimeCollectorOption {
	return func(rc *RuntimeCollector) {
		rc.interval = interval
	}
}

func (rc *RuntimeCollector) Start(ctx context.Context) error {
	if !rc.running.CompareAndSwap(false, true) {
		return errors.New("runtime collector already running")
	}
	rc.wg.Add(1)
	go rc.collectLoop(ctx)
	return nil
}

func (rc *RuntimeCollector) Stop() {
	rc.stopOnce.Do(func() {
		close(rc.stopCh)
	})
	rc.wg.Wait()
	rc.running.Store(false)
}

func (rc *RuntimeCollector) Snapshot() *RuntimeSnapshot {
	return rc.snapshot.Load()
}

func (rc *RuntimeCollector) collectLoop(ctx context.Context) {
	defer rc.wg.Done()

	ticker := time.NewTicker(rc.interval)
	defer ticker.Stop()

	rc.collect()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		case <-ticker.C:
			rc.collect()
		}
	}
}

func (rc *RuntimeCollector) collect() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	now := time.Now()
	snap := &RuntimeSnapshot{
		Goroutines:    runtime.NumGoroutine(),
		HeapAllocMB:   float64(memStats.HeapAlloc) / 1024 / 1024,
		HeapSysMB:     float64(memStats.HeapSys) / 1024 / 1024,
		NumGC:         memStats.NumGC,
		CollectedAt:   now,
		UptimeSeconds: now.Sub(rc.startTime).Seconds(),
	}

	if memStats.NumGC > 0 {
		lastPauseIdx := (memStats.NumGC + 255) % 256
		snap.GCPauseMs = float64(memStats.PauseNs[lastPauseIdx]) / 1e6
	}

	if rc.poolStatsProvider != nil {
		activeConns, maxConns := rc.poolStatsProvider()
		if maxConns > 0 {
			snap.MinIOPoolUsage = float64(activeConns) / float64(maxConns)
		}
	}

	if rc.bufferStatsProvider != nil {
		snap.PendingWrites = rc.bufferStatsProvider()
	}

	currentCPUTime := getCPUTime()
	elapsed := now.Sub(rc.lastCollectTime)
	if elapsed > 0 {
		cpuDelta := currentCPUTime - rc.lastCPUTime
		snap.CPUPercent = float64(cpuDelta) / float64(elapsed) * 100
	}
	rc.lastCPUTime = currentCPUTime
	rc.lastCollectTime = now

	snap.LoadLevel = rc.determineLoadLevel(snap)
	snap.ThrottleRatio = rc.calculateThrottleRatio(snap.LoadLevel)

	rc.snapshot.Store(snap)

	UpdateMemoryUsage(float64(memStats.HeapAlloc))
	UpdateGoroutineCount(float64(snap.Goroutines))
}

func (rc *RuntimeCollector) determineLoadLevel(snap *RuntimeSnapshot) LoadLevel {
	levels := make([]LoadLevel, 0, 5)

	levels = append(levels, rc.classifyByGoroutines(snap.Goroutines))
	levels = append(levels, rc.classifyByMemoryRatio(snap.HeapAllocMB, snap.HeapSysMB))
	levels = append(levels, rc.classifyByPoolUsage(snap.MinIOPoolUsage))
	levels = append(levels, rc.classifyByGCPause(snap.GCPauseMs))
	levels = append(levels, rc.classifyByPendingWrites(snap.PendingWrites))

	maxLevel := LoadIdle
	for _, level := range levels {
		if level > maxLevel {
			maxLevel = level
		}
	}
	return maxLevel
}

func (rc *RuntimeCollector) classifyByGoroutines(n int) LoadLevel {
	switch {
	case n >= rc.thresholds.GoroutineBusy:
		return LoadOverloaded
	case n >= rc.thresholds.GoroutineNormal:
		return LoadBusy
	case n >= rc.thresholds.GoroutineIdle:
		return LoadNormal
	default:
		return LoadIdle
	}
}

func (rc *RuntimeCollector) classifyByMemoryRatio(heapAllocMB, heapSysMB float64) LoadLevel {
	if heapSysMB == 0 {
		return LoadIdle
	}
	ratio := heapAllocMB / heapSysMB
	switch {
	case ratio >= rc.thresholds.MemoryRatioBusy:
		return LoadOverloaded
	case ratio >= rc.thresholds.MemoryRatioNormal:
		return LoadBusy
	case ratio >= rc.thresholds.MemoryRatioIdle:
		return LoadNormal
	default:
		return LoadIdle
	}
}

func (rc *RuntimeCollector) classifyByPoolUsage(usage float64) LoadLevel {
	switch {
	case usage >= rc.thresholds.PoolUsageBusy:
		return LoadOverloaded
	case usage >= rc.thresholds.PoolUsageNormal:
		return LoadBusy
	case usage >= rc.thresholds.PoolUsageIdle:
		return LoadNormal
	default:
		return LoadIdle
	}
}

func (rc *RuntimeCollector) classifyByGCPause(pauseMs float64) LoadLevel {
	switch {
	case pauseMs >= rc.thresholds.GCPauseMsBusy:
		return LoadOverloaded
	case pauseMs >= rc.thresholds.GCPauseMsNormal:
		return LoadBusy
	case pauseMs >= rc.thresholds.GCPauseMsIdle:
		return LoadNormal
	default:
		return LoadIdle
	}
}

func (rc *RuntimeCollector) classifyByPendingWrites(n int64) LoadLevel {
	switch {
	case n >= rc.thresholds.PendingWritesBusy:
		return LoadOverloaded
	case n >= rc.thresholds.PendingWritesNormal:
		return LoadBusy
	case n >= rc.thresholds.PendingWritesIdle:
		return LoadNormal
	default:
		return LoadIdle
	}
}

func (rc *RuntimeCollector) calculateThrottleRatio(level LoadLevel) float64 {
	switch level {
	case LoadIdle:
		return throttleRatioIdle
	case LoadNormal:
		return throttleRatioNormal
	case LoadBusy:
		return throttleRatioBusy
	case LoadOverloaded:
		return throttleRatioOverloaded
	default:
		return throttleRatioIdle
	}
}

func InitGlobalRuntimeCollector(opts ...RuntimeCollectorOption) *RuntimeCollector {
	c := NewRuntimeCollector(opts...)
	globalRuntimeCollector.Store(c)
	return c
}

const snapshotMaxAge = 30 * time.Second

func getCPUTime() time.Duration {
	var rusage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err != nil {
		return 0
	}
	return time.Duration(rusage.Utime.Nano()+rusage.Stime.Nano()) * time.Nanosecond
}

func GetRuntimeSnapshot() *RuntimeSnapshot {
	c := globalRuntimeCollector.Load()
	if c == nil {
		return &RuntimeSnapshot{
			LoadLevel:   LoadIdle,
			CollectedAt: time.Now(),
			Stale:       true,
		}
	}
	snap := c.Snapshot()
	if !c.running.Load() || time.Since(snap.CollectedAt) > snapshotMaxAge {
		snapCopy := *snap
		snapCopy.Stale = true
		return &snapCopy
	}
	return snap
}
