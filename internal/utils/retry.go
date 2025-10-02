package utils

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"minIODB/internal/metrics"
)

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts     int           // 最大重试次数
	InitialInterval time.Duration // 初始重试间隔
	MaxInterval     time.Duration // 最大重试间隔
	Multiplier      float64       // 退避倍数
	MaxJitter       time.Duration // 最大抖动时间
}

// DefaultRetryConfig 默认重试配置
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:     3,
	InitialInterval: 100 * time.Millisecond,
	MaxInterval:     5 * time.Second,
	Multiplier:      2.0,
	MaxJitter:       100 * time.Millisecond,
}

// RetryableError 可重试的错误
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("retryable error: %v", e.Err)
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// NewRetryableError 创建可重试错误
func NewRetryableError(err error) *RetryableError {
	return &RetryableError{Err: err}
}

// IsRetryable 检查错误是否可重试
func IsRetryable(err error) bool {
	var retryableErr *RetryableError
	return errors.As(err, &retryableErr)
}

// RetryFunc 可重试的函数类型
type RetryFunc func(ctx context.Context) error

// Retry 执行重试逻辑
func Retry(ctx context.Context, config RetryConfig, operation string, fn RetryFunc) error {
	var lastErr error
	interval := config.InitialInterval

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		// 记录重试指标
		retryMetrics := metrics.NewGRPCMetrics(fmt.Sprintf("retry_%s", operation))
		
		err := fn(ctx)
		if err == nil {
			retryMetrics.Finish("success")
			return nil
		}

		lastErr = err
		
		// 检查是否可重试
		if !IsRetryable(err) {
			retryMetrics.Finish("non_retryable_error")
			log.Printf("Non-retryable error in %s (attempt %d/%d): %v", operation, attempt, config.MaxAttempts, err)
			return err
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			retryMetrics.Finish("context_cancelled")
			return ctx.Err()
		}

		// 如果不是最后一次尝试，则等待重试
		if attempt < config.MaxAttempts {
			retryMetrics.Finish("retry")
			log.Printf("Retrying %s (attempt %d/%d) after %v: %v", operation, attempt, config.MaxAttempts, interval, err)
			
			select {
			case <-time.After(interval):
				// 计算下次重试间隔
				interval = time.Duration(float64(interval) * config.Multiplier)
				if interval > config.MaxInterval {
					interval = config.MaxInterval
				}
				// 添加抖动
				if config.MaxJitter > 0 {
					jitter := time.Duration(float64(config.MaxJitter) * (0.5 + 0.5*float64(time.Now().UnixNano()%2)))
					interval += jitter
				}
			case <-ctx.Done():
				retryMetrics.Finish("context_cancelled")
				return ctx.Err()
			}
		} else {
			retryMetrics.Finish("max_attempts_exceeded")
		}
	}

	log.Printf("Max retry attempts exceeded for %s: %v", operation, lastErr)
	return fmt.Errorf("max retry attempts (%d) exceeded for %s: %w", config.MaxAttempts, operation, lastErr)
}

// CircuitBreakerState 熔断器状态
type CircuitBreakerState int

const (
	CircuitBreakerClosed CircuitBreakerState = iota
	CircuitBreakerOpen
	CircuitBreakerHalfOpen
)

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	FailureThreshold   int           // 失败阈值
	RecoveryTimeout    time.Duration // 恢复超时
	SuccessThreshold   int           // 成功阈值（半开状态下）
	RequestVolumeThreshold int       // 请求量阈值
	SlidingWindowSize  time.Duration // 滑动窗口大小
}

// DefaultCircuitBreakerConfig 默认熔断器配置
var DefaultCircuitBreakerConfig = CircuitBreakerConfig{
	FailureThreshold:       5,
	RecoveryTimeout:        30 * time.Second,
	SuccessThreshold:       3,
	RequestVolumeThreshold: 10,
	SlidingWindowSize:      60 * time.Second,
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	config       CircuitBreakerConfig
	state        CircuitBreakerState
	failures     int
	successes    int
	requests     int
	lastFailTime time.Time
	mu           sync.RWMutex
	name         string
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(name string, config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config:       config,
		state:        CircuitBreakerClosed,
		name:         name,
	}
}

// Execute 执行操作
func (cb *CircuitBreaker) Execute(ctx context.Context, fn RetryFunc) error {
	cb.mu.Lock()
	
	// 检查熔断器状态
	switch cb.state {
	case CircuitBreakerOpen:
		// 检查是否可以进入半开状态
		if time.Since(cb.lastFailTime) > cb.config.RecoveryTimeout {
			cb.state = CircuitBreakerHalfOpen
			cb.successes = 0
			log.Printf("Circuit breaker %s: transitioning to half-open state", cb.name)
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker %s is open", cb.name)
		}
	case CircuitBreakerHalfOpen:
		// 半开状态，允许有限的请求通过
	case CircuitBreakerClosed:
		// 关闭状态，正常处理请求
	}
	
	cb.requests++
	cb.mu.Unlock()

	// 执行操作
	err := fn(ctx)
	
	cb.mu.Lock()
	defer cb.mu.Unlock()
	
	if err != nil {
		cb.onFailure()
		return err
	}
	
	cb.onSuccess()
	return nil
}

// onFailure 处理失败
func (cb *CircuitBreaker) onFailure() {
	cb.failures++
	cb.lastFailTime = time.Now()
	
	switch cb.state {
	case CircuitBreakerClosed:
		// 检查是否需要打开熔断器
		if cb.requests >= cb.config.RequestVolumeThreshold && 
		   cb.failures >= cb.config.FailureThreshold {
			cb.state = CircuitBreakerOpen
			log.Printf("Circuit breaker %s: opening due to %d failures out of %d requests", 
				cb.name, cb.failures, cb.requests)
		}
	case CircuitBreakerHalfOpen:
		// 半开状态下失败，直接打开
		cb.state = CircuitBreakerOpen
		log.Printf("Circuit breaker %s: opening from half-open state due to failure", cb.name)
	}
}

// onSuccess 处理成功
func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case CircuitBreakerHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = CircuitBreakerClosed
			cb.failures = 0
			cb.requests = 0
			log.Printf("Circuit breaker %s: closing from half-open state after %d successes", 
				cb.name, cb.successes)
		}
	case CircuitBreakerClosed:
		// 重置失败计数器
		if cb.failures > 0 {
			cb.failures = 0
		}
	}
}

// GetState 获取熔断器状态
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats 获取熔断器统计信息
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	
	return map[string]interface{}{
		"state":     cb.state,
		"failures":  cb.failures,
		"successes": cb.successes,
		"requests":  cb.requests,
	}
} 