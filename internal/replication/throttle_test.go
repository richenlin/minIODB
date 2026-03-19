package replication

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"minIODB/internal/metrics"
)

func mockSnapshotProvider(level metrics.LoadLevel) func() *metrics.RuntimeSnapshot {
	return func() *metrics.RuntimeSnapshot {
		return &metrics.RuntimeSnapshot{
			LoadLevel:     level,
			ThrottleRatio: 0.0,
			CollectedAt:   time.Now(),
		}
	}
}

func TestNewThrottler(t *testing.T) {
	tests := []struct {
		name    string
		config  ThrottleConfig
		wantQPS int
		wantSem int
	}{
		{
			name:    "default config",
			config:  DefaultThrottleConfig(),
			wantQPS: 50,
			wantSem: 4,
		},
		{
			name: "custom config",
			config: ThrottleConfig{
				MaxQPS:              100,
				MaxConcurrentCopies: 8,
			},
			wantQPS: 100,
			wantSem: 8,
		},
		{
			name: "zero values use defaults",
			config: ThrottleConfig{
				MaxQPS:              0,
				MaxConcurrentCopies: 0,
			},
			wantQPS: 50,
			wantSem: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			throttler := NewThrottler(tt.config)
			if throttler == nil {
				t.Fatal("NewThrottler returned nil")
			}
			if throttler.GetMaxQPS() != tt.wantQPS {
				t.Errorf("MaxQPS = %d, want %d", throttler.GetMaxQPS(), tt.wantQPS)
			}
			if throttler.GetMaxConcurrent() != tt.wantSem {
				t.Errorf("MaxConcurrent = %d, want %d", throttler.GetMaxConcurrent(), tt.wantSem)
			}
		})
	}
}

func TestThrottler_Wait_QPSLimit(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              5,
		MaxConcurrentCopies: 20,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	for i := 0; i < 5; i++ {
		err := throttler.Wait(context.Background())
		if err != nil {
			t.Errorf("Wait() initial burst error = %v", err)
		}
		throttler.Release()
	}

	start := time.Now()
	for i := 0; i < 10; i++ {
		err := throttler.Wait(context.Background())
		if err != nil {
			t.Errorf("Wait() error = %v", err)
		}
		throttler.Release()
	}
	elapsed := time.Since(start)

	if elapsed < 1*time.Second {
		t.Errorf("QPS limiting not effective: elapsed = %v, expected >= 1s", elapsed)
	}
}

func TestThrottler_Wait_ConcurrentLimit(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              1000,
		MaxConcurrentCopies: 2,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	var acquired int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := throttler.Wait(context.Background())
			if err == nil {
				atomic.AddInt32(&acquired, 1)
				time.Sleep(100 * time.Millisecond)
				throttler.Release()
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)

	initial := atomic.LoadInt32(&acquired)
	if initial > 2 {
		t.Errorf("too many concurrent acquires: %d, expected <= 2", initial)
	}

	wg.Wait()

	final := atomic.LoadInt32(&acquired)
	if final != 5 {
		t.Errorf("not all goroutines completed: %d, expected 5", final)
	}
}

func TestThrottler_Release(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              1000,
		MaxConcurrentCopies: 2,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)

	err := throttler.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	if throttler.GetAvailableSlots() != 1 {
		t.Errorf("semaphore slots after acquire = %d, expected 1", throttler.GetAvailableSlots())
	}

	throttler.Release()

	if throttler.GetAvailableSlots() != 2 {
		t.Errorf("semaphore slots after release = %d, expected 2", throttler.GetAvailableSlots())
	}
}

func TestThrottler_AdaptiveWait_Idle(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	start := time.Now()
	err := throttler.AdaptiveWait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("AdaptiveWait() error = %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("AdaptiveWait() on idle took too long: %v", elapsed)
	}
	throttler.Release()

	if throttler.GetCurrentLoad() != metrics.LoadIdle {
		t.Errorf("GetCurrentLoad() = %v, want LoadIdle", throttler.GetCurrentLoad())
	}
}

func TestThrottler_AdaptiveWait_Normal(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadNormal)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	start := time.Now()
	err := throttler.AdaptiveWait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("AdaptiveWait() error = %v", err)
	}
	if elapsed < 5*time.Millisecond {
		t.Errorf("AdaptiveWait() on normal should add delay, elapsed = %v", elapsed)
	}
	throttler.Release()

	limit := throttler.GetCurrentQPSLimit()
	expectedLimit := float64(config.MaxQPS) * 0.7
	if limit != expectedLimit {
		t.Errorf("QPS limit = %f, want %f", limit, expectedLimit)
	}
}

func TestThrottler_AdaptiveWait_Busy(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadBusy)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	start := time.Now()
	err := throttler.AdaptiveWait(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("AdaptiveWait() error = %v", err)
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("AdaptiveWait() on busy should add 50ms delay, elapsed = %v", elapsed)
	}
	throttler.Release()

	limit := throttler.GetCurrentQPSLimit()
	expectedLimit := float64(config.MaxQPS) * 0.3
	if limit != expectedLimit {
		t.Errorf("QPS limit = %f, want %f", limit, expectedLimit)
	}
}

func TestThrottler_AdaptiveWait_Overloaded(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadOverloaded)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	start := time.Now()
	err := throttler.AdaptiveWait(context.Background())
	elapsed := time.Since(start)

	if err != ErrThrottleOverloaded {
		t.Errorf("AdaptiveWait() error = %v, want %v", err, ErrThrottleOverloaded)
	}
	if elapsed < 28*time.Second {
		t.Errorf("AdaptiveWait() on overloaded should pause 30s, elapsed = %v", elapsed)
	}
}

func TestThrottler_AdaptiveWait_ContextCancel(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadOverloaded)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := throttler.AdaptiveWait(ctx)

	if err != context.DeadlineExceeded {
		t.Errorf("AdaptiveWait() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestThrottler_GetStats(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              50,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadNormal)),
		WithLogger(zap.NewNop()),
	)

	stats := throttler.GetStats()

	if stats.MaxQPS != config.MaxQPS {
		t.Errorf("MaxQPS = %d, want %d", stats.MaxQPS, config.MaxQPS)
	}
	if stats.MaxConcurrent != config.MaxConcurrentCopies {
		t.Errorf("MaxConcurrent = %d, want %d", stats.MaxConcurrent, config.MaxConcurrentCopies)
	}
	if stats.AvailableSlots != config.MaxConcurrentCopies {
		t.Errorf("AvailableSlots = %d, want %d", stats.AvailableSlots, config.MaxConcurrentCopies)
	}
}

func TestThrottler_ResetLimit(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadBusy)),
		WithLogger(zap.NewNop()),
	)

	_ = throttler.AdaptiveWait(context.Background())
	throttler.Release()

	throttler.ResetLimit()

	limit := throttler.GetCurrentQPSLimit()
	if limit != float64(config.MaxQPS) {
		t.Errorf("after ResetLimit, QPS limit = %f, want %f", limit, float64(config.MaxQPS))
	}
}

func TestThrottler_SetQPSLimit(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)

	throttler.SetQPSLimit(50)

	limit := throttler.GetCurrentQPSLimit()
	if limit != 50 {
		t.Errorf("after SetQPSLimit, QPS limit = %f, want 50", limit)
	}

	throttler.SetQPSLimit(0)

	limit = throttler.GetCurrentQPSLimit()
	if limit != float64(config.MaxQPS) {
		t.Errorf("after SetQPSLimit(0), QPS limit = %f, want %f", limit, float64(config.MaxQPS))
	}
}

func TestThrottler_Stop(t *testing.T) {
	config := DefaultThrottleConfig()
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)

	throttler.Stop()

	err := throttler.Wait(context.Background())
	if err != ErrThrottleStopped {
		t.Errorf("Wait() after Stop() error = %v, want %v", err, ErrThrottleStopped)
	}

	err = throttler.AdaptiveWait(context.Background())
	if err != ErrThrottleStopped {
		t.Errorf("AdaptiveWait() after Stop() error = %v, want %v", err, ErrThrottleStopped)
	}
}

func TestThrottler_GetOverloadedDuration(t *testing.T) {
	config := DefaultThrottleConfig()
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)

	if dur := throttler.GetOverloadedDuration(); dur != 0 {
		t.Errorf("GetOverloadedDuration() = %v, want 0", dur)
	}
}

func TestThrottler_ConcurrentUsage(t *testing.T) {
	config := ThrottleConfig{
		MaxQPS:              100,
		MaxConcurrentCopies: 4,
	}
	throttler := NewThrottler(config,
		WithSnapshotProvider(mockSnapshotProvider(metrics.LoadIdle)),
		WithLogger(zap.NewNop()),
	)
	defer throttler.Stop()

	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := throttler.Wait(context.Background())
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}
			atomic.AddInt32(&successCount, 1)
			time.Sleep(10 * time.Millisecond)
			throttler.Release()
		}()
	}

	wg.Wait()

	if successCount != 20 {
		t.Errorf("successCount = %d, want 20", successCount)
	}
	if errorCount != 0 {
		t.Errorf("errorCount = %d, want 0", errorCount)
	}
}
