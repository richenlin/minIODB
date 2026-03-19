package replication

import (
	"go.uber.org/zap"

	"minIODB/internal/metrics"
	"minIODB/pkg/throttle"
)

var (
	ErrThrottleOverloaded = throttle.ErrThrottleOverloaded
	ErrThrottleStopped    = throttle.ErrThrottleStopped
)

type ThrottleConfig = throttle.ThrottleConfig
type ThrottleStats = throttle.ThrottleStats

type Throttler struct {
	*throttle.Throttler
}

func DefaultThrottleConfig() ThrottleConfig {
	return throttle.DefaultThrottleConfig()
}

type snapshotAdapter struct {
	snap *metrics.RuntimeSnapshot
}

func (a *snapshotAdapter) GetLoadLevel() throttle.LoadLevel {
	if a.snap == nil {
		return throttle.LoadIdle
	}
	return throttle.LoadLevel(a.snap.LoadLevel)
}

func (a *snapshotAdapter) GetThrottleRatio() float64 {
	if a.snap == nil {
		return 0
	}
	return a.snap.ThrottleRatio
}

func NewThrottler(config ThrottleConfig, opts ...ThrottlerOption) *Throttler {
	var adaptedOpts []throttle.ThrottlerOption
	for _, opt := range opts {
		adaptedOpts = append(adaptedOpts, func(t *throttle.Throttler) {
			opt(&Throttler{Throttler: t})
		})
	}
	return &Throttler{Throttler: throttle.NewThrottler(config, adaptedOpts...)}
}

type ThrottlerOption func(*Throttler)

func WithSnapshotProvider(provider func() *metrics.RuntimeSnapshot) ThrottlerOption {
	return func(t *Throttler) {
		adaptedProvider := func() throttle.LoadLevelProvider {
			snap := provider()
			if snap == nil {
				return nil
			}
			return &snapshotAdapter{snap: snap}
		}
		throttle.WithSnapshotProvider(adaptedProvider)(t.Throttler)
	}
}

func WithThresholds(thresholds metrics.LoadThresholds) ThrottlerOption {
	return func(t *Throttler) {
		adapted := throttle.LoadThresholds{
			GoroutineIdle:       thresholds.GoroutineIdle,
			GoroutineNormal:     thresholds.GoroutineNormal,
			GoroutineBusy:       thresholds.GoroutineBusy,
			MemoryRatioIdle:     thresholds.MemoryRatioIdle,
			MemoryRatioNormal:   thresholds.MemoryRatioNormal,
			MemoryRatioBusy:     thresholds.MemoryRatioBusy,
			PoolUsageIdle:       thresholds.PoolUsageIdle,
			PoolUsageNormal:     thresholds.PoolUsageNormal,
			PoolUsageBusy:       thresholds.PoolUsageBusy,
			GCPauseMsIdle:       thresholds.GCPauseMsIdle,
			GCPauseMsNormal:     thresholds.GCPauseMsNormal,
			GCPauseMsBusy:       thresholds.GCPauseMsBusy,
			PendingWritesIdle:   thresholds.PendingWritesIdle,
			PendingWritesNormal: thresholds.PendingWritesNormal,
			PendingWritesBusy:   thresholds.PendingWritesBusy,
		}
		throttle.WithThresholds(adapted)(t.Throttler)
	}
}

func WithLogger(logger *zap.Logger) ThrottlerOption {
	return func(t *Throttler) {
		throttle.WithLogger(logger)(t.Throttler)
	}
}

func (t *Throttler) GetCurrentLoad() throttle.LoadLevel {
	return t.Throttler.GetCurrentLoad()
}

func (t *Throttler) GetStats() ThrottleStats {
	return t.Throttler.GetStats()
}

func (t *Throttler) GetMaxQPS() int {
	return t.Throttler.GetStats().MaxQPS
}

func (t *Throttler) GetMaxConcurrent() int {
	return t.Throttler.GetStats().MaxConcurrent
}

func (t *Throttler) GetAvailableSlots() int {
	return t.Throttler.GetStats().AvailableSlots
}

func (t *Throttler) GetCurrentQPSLimit() float64 {
	return t.Throttler.GetStats().CurrentQPSLimit
}
