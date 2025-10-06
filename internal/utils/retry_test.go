package utils

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetryableError(t *testing.T) {
	originalErr := errors.New("test error")
	retryableErr := NewRetryableError(originalErr)

	assert.Error(t, retryableErr)
	assert.Equal(t, "retryable error: test error", retryableErr.Error())
	assert.Equal(t, originalErr, retryableErr.Unwrap())
}

func TestIsRetryable(t *testing.T) {
	// Test with retryable error
	originalErr := errors.New("test error")
	retryableErr := NewRetryableError(originalErr)
	assert.True(t, IsRetryable(retryableErr))

	// Test with non-retryable error
	nonRetryableErr := errors.New("non-retryable")
	assert.False(t, IsRetryable(nonRetryableErr))
}

func TestDefaultRetryConfig(t *testing.T) {
	assert.Equal(t, 3, DefaultRetryConfig.MaxAttempts)
	assert.Equal(t, 100*time.Millisecond, DefaultRetryConfig.InitialInterval)
	assert.Equal(t, 5*time.Second, DefaultRetryConfig.MaxInterval)
	assert.Equal(t, 2.0, DefaultRetryConfig.Multiplier)
	assert.Equal(t, 100*time.Millisecond, DefaultRetryConfig.MaxJitter)
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	assert.Equal(t, 5, DefaultCircuitBreakerConfig.FailureThreshold)
	assert.Equal(t, 30*time.Second, DefaultCircuitBreakerConfig.RecoveryTimeout)
	assert.Equal(t, 3, DefaultCircuitBreakerConfig.SuccessThreshold)
	assert.Equal(t, 10, DefaultCircuitBreakerConfig.RequestVolumeThreshold)
	assert.Equal(t, 60*time.Second, DefaultCircuitBreakerConfig.SlidingWindowSize)
}

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig)

	assert.NotNil(t, cb)
	assert.Equal(t, "test", cb.name)
	assert.Equal(t, CircuitBreakerClosed, cb.GetState())
}

func TestCircuitBreakerInitialState(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig)
	stats := cb.GetStats()

	assert.Equal(t, CircuitBreakerClosed, stats["state"])
	assert.Equal(t, 0, stats["failures"])
	assert.Equal(t, 0, stats["successes"])
	assert.Equal(t, 0, stats["requests"])
}