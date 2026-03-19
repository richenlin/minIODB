package throttle

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var (
	ErrThrottleOverloaded = errors.New("system overloaded, throttle wait aborted")
	ErrThrottleStopped    = errors.New("throttler stopped")
)

type ThrottleConfig struct {
	MaxQPS              int
	MaxConcurrentCopies int
}

func DefaultThrottleConfig() ThrottleConfig {
	return ThrottleConfig{
		MaxQPS:              50,
		MaxConcurrentCopies: 4,
	}
}

type Throttler struct {
	config ThrottleConfig

	qpsLimiter       *rate.Limiter
	sem              chan struct{}
	snapshotProvider func() LoadLevelProvider
	thresholds       LoadThresholds

	stopCh   chan struct{}
	stopOnce sync.Once
	logger   *zap.Logger

	mu              sync.RWMutex
	currentLoad     LoadLevel
	overloadedSince time.Time
}

func NewThrottler(config ThrottleConfig, opts ...ThrottlerOption) *Throttler {
	if config.MaxQPS <= 0 {
		config.MaxQPS = DefaultThrottleConfig().MaxQPS
	}
	if config.MaxConcurrentCopies <= 0 {
		config.MaxConcurrentCopies = DefaultThrottleConfig().MaxConcurrentCopies
	}

	t := &Throttler{
		config:           config,
		qpsLimiter:       rate.NewLimiter(rate.Limit(config.MaxQPS), config.MaxQPS),
		sem:              make(chan struct{}, config.MaxConcurrentCopies),
		snapshotProvider: func() LoadLevelProvider { return nil },
		thresholds:       DefaultLoadThresholds(),
		stopCh:           make(chan struct{}),
		logger:           zap.NewNop(),
		currentLoad:      LoadIdle,
	}

	for i := 0; i < config.MaxConcurrentCopies; i++ {
		t.sem <- struct{}{}
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

type ThrottlerOption func(*Throttler)

func WithSnapshotProvider(provider func() LoadLevelProvider) ThrottlerOption {
	return func(t *Throttler) {
		if provider != nil {
			t.snapshotProvider = provider
		}
	}
}

func WithThresholds(thresholds LoadThresholds) ThrottlerOption {
	return func(t *Throttler) {
		t.thresholds = thresholds
	}
}

func WithLogger(logger *zap.Logger) ThrottlerOption {
	return func(t *Throttler) {
		if logger != nil {
			t.logger = logger
		}
	}
}

func (t *Throttler) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.stopCh:
		return ErrThrottleStopped
	default:
	}

	if err := t.qpsLimiter.Wait(ctx); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.stopCh:
		return ErrThrottleStopped
	case <-t.sem:
		return nil
	}
}

func (t *Throttler) AdaptiveWait(ctx context.Context) error {
	select {
	case <-t.stopCh:
		return ErrThrottleStopped
	default:
	}

	var loadLevel LoadLevel
	if provider := t.snapshotProvider(); provider != nil {
		loadLevel = provider.GetLoadLevel()

		t.mu.Lock()
		t.currentLoad = loadLevel
		if loadLevel == LoadOverloaded {
			if t.overloadedSince.IsZero() {
				t.overloadedSince = time.Now()
			}
		} else {
			t.overloadedSince = time.Time{}
		}
		t.mu.Unlock()
	}

	switch loadLevel {
	case LoadOverloaded:
		var throttleRatio float64
		if provider := t.snapshotProvider(); provider != nil {
			throttleRatio = provider.GetThrottleRatio()
		}
		t.logger.Warn("system overloaded, pausing throttle for 30s",
			zap.String("load_level", loadLevel.String()),
			zap.Float64("throttle_ratio", throttleRatio))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.stopCh:
			return ErrThrottleStopped
		case <-time.After(30 * time.Second):
			return ErrThrottleOverloaded
		}

	case LoadBusy:
		var throttleRatio float64
		if provider := t.snapshotProvider(); provider != nil {
			throttleRatio = provider.GetThrottleRatio()
		}
		t.logger.Warn("system busy, applying aggressive throttling",
			zap.String("load_level", loadLevel.String()),
			zap.Float64("throttle_ratio", throttleRatio))

		adjustedQPS := float64(t.config.MaxQPS) * 0.3
		t.qpsLimiter.SetLimit(rate.Limit(adjustedQPS))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}

	case LoadNormal:
		adjustedQPS := float64(t.config.MaxQPS) * 0.7
		t.qpsLimiter.SetLimit(rate.Limit(adjustedQPS))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}

	case LoadIdle:
		t.qpsLimiter.SetLimit(rate.Limit(t.config.MaxQPS))
	}

	return t.Wait(ctx)
}

func (t *Throttler) Release() {
	select {
	case t.sem <- struct{}{}:
	default:
		t.logger.Warn("semaphore release failed: channel full, possible double release")
	}
}

func (t *Throttler) GetCurrentLoad() LoadLevel {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentLoad
}

func (t *Throttler) GetOverloadedDuration() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.overloadedSince.IsZero() {
		return 0
	}
	return time.Since(t.overloadedSince)
}

func (t *Throttler) GetStats() ThrottleStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	availableSlots := len(t.sem)
	currentLimit := t.qpsLimiter.Limit()

	return ThrottleStats{
		MaxQPS:           t.config.MaxQPS,
		CurrentQPSLimit:  float64(currentLimit),
		MaxConcurrent:    t.config.MaxConcurrentCopies,
		AvailableSlots:   availableSlots,
		CurrentLoadLevel: t.currentLoad.String(),
		OverloadedSince:  t.overloadedSince,
	}
}

type ThrottleStats struct {
	MaxQPS           int       `json:"max_qps"`
	CurrentQPSLimit  float64   `json:"current_qps_limit"`
	MaxConcurrent    int       `json:"max_concurrent"`
	AvailableSlots   int       `json:"available_slots"`
	CurrentLoadLevel string    `json:"current_load_level"`
	OverloadedSince  time.Time `json:"overloaded_since"`
}

func (t *Throttler) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
}

func (t *Throttler) ResetLimit() {
	t.qpsLimiter.SetLimit(rate.Limit(t.config.MaxQPS))
}

func (t *Throttler) SetQPSLimit(qps int) {
	if qps <= 0 {
		qps = t.config.MaxQPS
	}
	t.qpsLimiter.SetLimit(rate.Limit(qps))
}
