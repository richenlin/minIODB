package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"minIODB/pkg/retry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetry_Success(t *testing.T) {
	attempts := 0
	config := retry.Config{
		MaxAttempts:     3,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
		Multiplier:      2.0,
	}

	err := retry.Do(context.Background(), config, "test_operation", func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return retry.NewRetryableError(errors.New("temporary error"))
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestRetry_NonRetryableError(t *testing.T) {
	attempts := 0
	config := retry.Config{
		MaxAttempts:     3,
		InitialInterval: 10 * time.Millisecond,
	}

	err := retry.Do(context.Background(), config, "test_operation", func(ctx context.Context) error {
		attempts++
		return errors.New("non-retryable error")
	})

	assert.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestRetry_MaxAttemptsExceeded(t *testing.T) {
	attempts := 0
	config := retry.Config{
		MaxAttempts:     3,
		InitialInterval: 10 * time.Millisecond,
	}

	err := retry.Do(context.Background(), config, "test_operation", func(ctx context.Context) error {
		attempts++
		return retry.NewRetryableError(errors.New("persistent error"))
	})

	assert.Error(t, err)
	assert.Equal(t, 3, attempts)
	assert.Contains(t, err.Error(), "max retry attempts")
}

func TestRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	config := retry.Config{
		MaxAttempts:     10,
		InitialInterval: 100 * time.Millisecond,
	}

	// 在第一次尝试后取消上下文
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := retry.Do(ctx, config, "test_operation", func(ctx context.Context) error {
		attempts++
		return retry.NewRetryableError(errors.New("error"))
	})

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled) || attempts < 10, "Should cancel or not complete all attempts")
}

func TestRetryableError(t *testing.T) {
	baseErr := errors.New("base error")
	retryErr := retry.NewRetryableError(baseErr)

	assert.True(t, retry.IsRetryable(retryErr))
	assert.Contains(t, retryErr.Error(), "retryable error")
	assert.Equal(t, baseErr, errors.Unwrap(retryErr))

	nonRetryErr := errors.New("non-retryable")
	assert.False(t, retry.IsRetryable(nonRetryErr))
}

func TestDoWithMetrics(t *testing.T) {
	attempts := 0
	metricsCallCount := 0
	config := retry.Config{
		MaxAttempts:     3,
		InitialInterval: 10 * time.Millisecond,
	}

	err := retry.DoWithMetrics(
		context.Background(),
		config,
		"test_operation",
		func(ctx context.Context) error {
			attempts++
			if attempts < 2 {
				return retry.NewRetryableError(errors.New("temporary error"))
			}
			return nil
		},
		func(attempt int, err error, duration time.Duration) {
			metricsCallCount++
			assert.Greater(t, duration, time.Duration(0))
		},
	)

	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 2, metricsCallCount)
}

func TestCircuitBreaker_NormalOperation(t *testing.T) {
	cb := retry.NewCircuitBreaker("test", retry.DefaultCircuitBreakerConfig)
	ctx := context.Background()

	// 正常操作应该成功
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, retry.CircuitBreakerClosed, cb.GetState())
}

func TestCircuitBreaker_Opening(t *testing.T) {
	config := retry.CircuitBreakerConfig{
		FailureThreshold:       3,
		RecoveryTimeout:        100 * time.Millisecond,
		SuccessThreshold:       2,
		RequestVolumeThreshold: 3,
	}
	cb := retry.NewCircuitBreaker("test", config)
	ctx := context.Background()

	// 执行足够的请求以达到阈值并触发熔断
	for i := 0; i < 3; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
	}

	assert.Equal(t, retry.CircuitBreakerOpen, cb.GetState())
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	config := retry.CircuitBreakerConfig{
		FailureThreshold:       2,
		RecoveryTimeout:        50 * time.Millisecond,
		SuccessThreshold:       2,
		RequestVolumeThreshold: 2,
	}
	cb := retry.NewCircuitBreaker("test", config)
	ctx := context.Background()

	// 触发熔断
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
	}

	require.Equal(t, retry.CircuitBreakerOpen, cb.GetState())

	// 等待恢复超时
	time.Sleep(60 * time.Millisecond)

	// 执行成功操作
	for i := 0; i < 2; i++ {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
		assert.NoError(t, err)
	}

	// 应该恢复到关闭状态
	assert.Equal(t, retry.CircuitBreakerClosed, cb.GetState())
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := retry.NewCircuitBreaker("test", retry.DefaultCircuitBreakerConfig)
	ctx := context.Background()

	// 执行一些操作
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	cb.Execute(ctx, func(ctx context.Context) error {
		return errors.New("failure")
	})

	stats := cb.GetStats()
	assert.Equal(t, "test", stats.Name)
	assert.Equal(t, 2, stats.Requests)
	assert.Equal(t, 1, stats.Failures)
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := retry.CircuitBreakerConfig{
		FailureThreshold:       2,
		RecoveryTimeout:        100 * time.Millisecond,
		SuccessThreshold:       2,
		RequestVolumeThreshold: 2,
	}
	cb := retry.NewCircuitBreaker("test", config)
	ctx := context.Background()

	// 触发熔断
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
	}

	require.Equal(t, retry.CircuitBreakerOpen, cb.GetState())

	// 重置熔断器
	cb.Reset()

	assert.Equal(t, retry.CircuitBreakerClosed, cb.GetState())
	stats := cb.GetStats()
	assert.Equal(t, 0, stats.Failures)
	assert.Equal(t, 0, stats.Requests)
}

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		state    retry.CircuitBreakerState
		expected string
	}{
		{retry.CircuitBreakerClosed, "closed"},
		{retry.CircuitBreakerOpen, "open"},
		{retry.CircuitBreakerHalfOpen, "half-open"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}
