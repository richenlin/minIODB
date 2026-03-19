package metrics

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLoadLevelString(t *testing.T) {
	tests := []struct {
		level    LoadLevel
		expected string
	}{
		{LoadIdle, "idle"},
		{LoadNormal, "normal"},
		{LoadBusy, "busy"},
		{LoadOverloaded, "overloaded"},
		{LoadLevel(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("LoadLevel.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultLoadThresholds(t *testing.T) {
	thresholds := DefaultLoadThresholds()

	if thresholds.GoroutineIdle != 500 {
		t.Errorf("GoroutineIdle = %d, want 500", thresholds.GoroutineIdle)
	}
	if thresholds.GoroutineNormal != 2000 {
		t.Errorf("GoroutineNormal = %d, want 2000", thresholds.GoroutineNormal)
	}
	if thresholds.GoroutineBusy != 5000 {
		t.Errorf("GoroutineBusy = %d, want 5000", thresholds.GoroutineBusy)
	}
	if thresholds.MemoryRatioIdle != 0.3 {
		t.Errorf("MemoryRatioIdle = %v, want 0.3", thresholds.MemoryRatioIdle)
	}
	if thresholds.MemoryRatioNormal != 0.6 {
		t.Errorf("MemoryRatioNormal = %v, want 0.6", thresholds.MemoryRatioNormal)
	}
	if thresholds.MemoryRatioBusy != 0.8 {
		t.Errorf("MemoryRatioBusy = %v, want 0.8", thresholds.MemoryRatioBusy)
	}
}

func TestNewRuntimeCollector(t *testing.T) {
	rc := NewRuntimeCollector()
	if rc == nil {
		t.Fatal("NewRuntimeCollector returned nil")
	}
	if rc.interval != 5*time.Second {
		t.Errorf("default interval = %v, want 5s", rc.interval)
	}
	snap := rc.Snapshot()
	if snap == nil {
		t.Fatal("initial snapshot is nil")
	}
}

func TestNewRuntimeCollectorWithOptions(t *testing.T) {
	poolProvider := func() (int64, int64) { return 5, 10 }
	bufferProvider := func() int64 { return 100 }
	customThresholds := LoadThresholds{
		GoroutineIdle:   100,
		GoroutineNormal: 500,
		GoroutineBusy:   1000,
	}

	rc := NewRuntimeCollector(
		WithPoolStatsProvider(poolProvider),
		WithBufferStatsProvider(bufferProvider),
		WithThresholds(customThresholds),
		WithInterval(10*time.Second),
	)

	if rc.poolStatsProvider == nil {
		t.Error("poolStatsProvider not set")
	}
	if rc.bufferStatsProvider == nil {
		t.Error("bufferStatsProvider not set")
	}
	if rc.thresholds.GoroutineIdle != 100 {
		t.Errorf("custom threshold not applied: %d", rc.thresholds.GoroutineIdle)
	}
	if rc.interval != 10*time.Second {
		t.Errorf("custom interval not applied: %v", rc.interval)
	}
}

func TestRuntimeCollectorStartStop(t *testing.T) {
	rc := NewRuntimeCollector(WithInterval(100 * time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rc.Start(ctx)

	time.Sleep(250 * time.Millisecond)

	snap := rc.Snapshot()
	if snap == nil {
		t.Fatal("snapshot is nil after start")
	}
	if snap.CollectedAt.IsZero() {
		t.Error("CollectedAt is zero")
	}
	if snap.Goroutines <= 0 {
		t.Errorf("Goroutines = %d, want > 0", snap.Goroutines)
	}

	rc.Stop()
}

func TestRuntimeCollectorStopWaits(t *testing.T) {
	rc := NewRuntimeCollector(WithInterval(10 * time.Millisecond))
	ctx := context.Background()

	rc.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		rc.Stop()
		close(done)
	}()

	select {
	case <-time.After(1 * time.Second):
		t.Error("Stop did not complete within 1 second")
	case <-done:
	}
}

func TestRuntimeCollectorSnapshot(t *testing.T) {
	poolProvider := func() (int64, int64) { return 3, 10 }
	bufferProvider := func() int64 { return 500 }

	rc := NewRuntimeCollector(
		WithPoolStatsProvider(poolProvider),
		WithBufferStatsProvider(bufferProvider),
	)

	rc.collect()

	snap := rc.Snapshot()
	if snap == nil {
		t.Fatal("snapshot is nil")
	}

	if snap.MinIOPoolUsage != 0.3 {
		t.Errorf("MinIOPoolUsage = %v, want 0.3", snap.MinIOPoolUsage)
	}
	if snap.PendingWrites != 500 {
		t.Errorf("PendingWrites = %d, want 500", snap.PendingWrites)
	}
	if snap.HeapAllocMB <= 0 {
		t.Errorf("HeapAllocMB = %v, want > 0", snap.HeapAllocMB)
	}
}

func TestDetermineLoadLevel(t *testing.T) {
	tests := []struct {
		name       string
		snapshot   *RuntimeSnapshot
		thresholds LoadThresholds
		wantLevel  LoadLevel
	}{
		{
			name: "idle system",
			snapshot: &RuntimeSnapshot{
				Goroutines:     100,
				HeapAllocMB:    100,
				HeapSysMB:      500,
				GCPauseMs:      1,
				MinIOPoolUsage: 0.1,
				PendingWrites:  50,
			},
			thresholds: DefaultLoadThresholds(),
			wantLevel:  LoadIdle,
		},
		{
			name: "normal system",
			snapshot: &RuntimeSnapshot{
				Goroutines:     1000,
				HeapAllocMB:    250,
				HeapSysMB:      500,
				GCPauseMs:      10,
				MinIOPoolUsage: 0.4,
				PendingWrites:  500,
			},
			thresholds: DefaultLoadThresholds(),
			wantLevel:  LoadNormal,
		},
		{
			name: "busy system",
			snapshot: &RuntimeSnapshot{
				Goroutines:     3000,
				HeapAllocMB:    380,
				HeapSysMB:      500,
				GCPauseMs:      30,
				MinIOPoolUsage: 0.7,
				PendingWrites:  2000,
			},
			thresholds: DefaultLoadThresholds(),
			wantLevel:  LoadBusy,
		},
		{
			name: "overloaded system",
			snapshot: &RuntimeSnapshot{
				Goroutines:     6000,
				HeapAllocMB:    450,
				HeapSysMB:      500,
				GCPauseMs:      60,
				MinIOPoolUsage: 0.9,
				PendingWrites:  6000,
			},
			thresholds: DefaultLoadThresholds(),
			wantLevel:  LoadOverloaded,
		},
		{
			name: "mixed metrics - takes highest",
			snapshot: &RuntimeSnapshot{
				Goroutines:     100,
				HeapAllocMB:    450,
				HeapSysMB:      500,
				GCPauseMs:      1,
				MinIOPoolUsage: 0.1,
				PendingWrites:  50,
			},
			thresholds: DefaultLoadThresholds(),
			wantLevel:  LoadOverloaded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := NewRuntimeCollector(WithThresholds(tt.thresholds))
			got := rc.determineLoadLevel(tt.snapshot)
			if got != tt.wantLevel {
				t.Errorf("determineLoadLevel() = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}

func TestClassifyByGoroutines(t *testing.T) {
	rc := NewRuntimeCollector()

	tests := []struct {
		n        int
		expected LoadLevel
	}{
		{100, LoadIdle},
		{500, LoadNormal},
		{1000, LoadNormal},
		{2000, LoadBusy},
		{4000, LoadBusy},
		{5000, LoadOverloaded},
		{10000, LoadOverloaded},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			if got := rc.classifyByGoroutines(tt.n); got != tt.expected {
				t.Errorf("classifyByGoroutines(%d) = %v, want %v", tt.n, got, tt.expected)
			}
		})
	}
}

func TestClassifyByMemoryRatio(t *testing.T) {
	rc := NewRuntimeCollector()

	tests := []struct {
		name     string
		alloc    float64
		sys      float64
		expected LoadLevel
	}{
		{"idle", 100, 500, LoadIdle},
		{"normal", 250, 500, LoadNormal},
		{"busy", 350, 500, LoadBusy},
		{"overloaded", 450, 500, LoadOverloaded},
		{"zero sys", 100, 0, LoadIdle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rc.classifyByMemoryRatio(tt.alloc, tt.sys); got != tt.expected {
				t.Errorf("classifyByMemoryRatio(%v, %v) = %v, want %v", tt.alloc, tt.sys, got, tt.expected)
			}
		})
	}
}

func TestClassifyByPoolUsage(t *testing.T) {
	rc := NewRuntimeCollector()

	tests := []struct {
		usage    float64
		expected LoadLevel
	}{
		{0.1, LoadIdle},
		{0.3, LoadNormal},
		{0.5, LoadNormal},
		{0.6, LoadBusy},
		{0.7, LoadBusy},
		{0.8, LoadOverloaded},
		{0.9, LoadOverloaded},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("usage=%.1f", tt.usage), func(t *testing.T) {
			if got := rc.classifyByPoolUsage(tt.usage); got != tt.expected {
				t.Errorf("classifyByPoolUsage(%v) = %v, want %v", tt.usage, got, tt.expected)
			}
		})
	}
}

func TestClassifyByGCPause(t *testing.T) {
	rc := NewRuntimeCollector()

	tests := []struct {
		pauseMs  float64
		expected LoadLevel
	}{
		{1, LoadIdle},
		{5, LoadNormal},
		{15, LoadNormal},
		{20, LoadBusy},
		{40, LoadBusy},
		{50, LoadOverloaded},
		{100, LoadOverloaded},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("pauseMs=%.0f", tt.pauseMs), func(t *testing.T) {
			if got := rc.classifyByGCPause(tt.pauseMs); got != tt.expected {
				t.Errorf("classifyByGCPause(%v) = %v, want %v", tt.pauseMs, got, tt.expected)
			}
		})
	}
}

func TestClassifyByPendingWrites(t *testing.T) {
	rc := NewRuntimeCollector()

	tests := []struct {
		n        int64
		expected LoadLevel
	}{
		{50, LoadIdle},
		{100, LoadNormal},
		{500, LoadNormal},
		{1000, LoadBusy},
		{3000, LoadBusy},
		{5000, LoadOverloaded},
		{10000, LoadOverloaded},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.n), func(t *testing.T) {
			if got := rc.classifyByPendingWrites(tt.n); got != tt.expected {
				t.Errorf("classifyByPendingWrites(%d) = %v, want %v", tt.n, got, tt.expected)
			}
		})
	}
}

func TestCalculateThrottleRatio(t *testing.T) {
	rc := NewRuntimeCollector()

	tests := []struct {
		level    LoadLevel
		expected float64
	}{
		{LoadIdle, 0.0},
		{LoadNormal, 0.3},
		{LoadBusy, 0.7},
		{LoadOverloaded, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			if got := rc.calculateThrottleRatio(tt.level); got != tt.expected {
				t.Errorf("calculateThrottleRatio(%v) = %v, want %v", tt.level, got, tt.expected)
			}
		})
	}
}

func TestInitGlobalRuntimeCollector(t *testing.T) {
	original := globalRuntimeCollector.Load()
	defer func() { globalRuntimeCollector.Store(original) }()

	rc := InitGlobalRuntimeCollector(WithInterval(1 * time.Second))

	if rc == nil {
		t.Fatal("InitGlobalRuntimeCollector returned nil")
	}
	if globalRuntimeCollector.Load() != rc {
		t.Error("globalRuntimeCollector not set correctly")
	}
}

func TestGetRuntimeSnapshot(t *testing.T) {
	original := globalRuntimeCollector.Load()
	defer func() { globalRuntimeCollector.Store(original) }()

	globalRuntimeCollector.Store(nil)
	snap := GetRuntimeSnapshot()
	if snap == nil {
		t.Fatal("GetRuntimeSnapshot returned nil when no global collector")
	}
	if snap.LoadLevel != LoadIdle {
		t.Errorf("default snapshot LoadLevel = %v, want idle", snap.LoadLevel)
	}

	c := NewRuntimeCollector()
	c.collect()
	globalRuntimeCollector.Store(c)
	snap = GetRuntimeSnapshot()
	if snap == nil {
		t.Fatal("GetRuntimeSnapshot returned nil")
	}
}

func TestRuntimeCollectorContextCancel(t *testing.T) {
	rc := NewRuntimeCollector(WithInterval(50 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())

	rc.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	cancel()

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		rc.wg.Wait()
		close(done)
	}()

	select {
	case <-time.After(500 * time.Millisecond):
		t.Error("collector did not stop on context cancel")
	case <-done:
	}
}

func TestRuntimeCollectorConcurrentSnapshot(t *testing.T) {
	rc := NewRuntimeCollector(WithInterval(10 * time.Millisecond))
	ctx := context.Background()
	rc.Start(ctx)
	defer rc.Stop()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = rc.Snapshot()
			time.Sleep(time.Millisecond)
		}
		close(done)
	}()

	<-done
}
