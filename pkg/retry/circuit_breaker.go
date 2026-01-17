package retry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"minIODB/pkg/logger"
)

// CircuitBreakerState 熔断器状态
type CircuitBreakerState int

const (
	// CircuitBreakerClosed 熔断器关闭（正常状态）
	CircuitBreakerClosed CircuitBreakerState = iota
	// CircuitBreakerOpen 熔断器打开（熔断状态）
	CircuitBreakerOpen
	// CircuitBreakerHalfOpen 熔断器半开（恢复测试状态）
	CircuitBreakerHalfOpen
)

// String 返回状态的字符串表示
func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitBreakerClosed:
		return "closed"
	case CircuitBreakerOpen:
		return "open"
	case CircuitBreakerHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	FailureThreshold       int           // 失败阈值（触发熔断的失败次数）
	RecoveryTimeout        time.Duration // 恢复超时（熔断后多久尝试恢复）
	SuccessThreshold       int           // 成功阈值（半开状态下需要的成功次数）
	RequestVolumeThreshold int           // 请求量阈值（最小请求数）
	SlidingWindowSize      time.Duration // 滑动窗口大小
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
// 实现了经典的熔断器模式，用于防止级联故障
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
		config: config,
		state:  CircuitBreakerClosed,
		name:   name,
	}
}

// Execute 通过熔断器执行操作
func (cb *CircuitBreaker) Execute(ctx context.Context, fn Func) error {
	cb.mu.Lock()

	// 检查熔断器状态
	switch cb.state {
	case CircuitBreakerOpen:
		// 检查是否可以进入半开状态
		if time.Since(cb.lastFailTime) > cb.config.RecoveryTimeout {
			cb.state = CircuitBreakerHalfOpen
			cb.successes = 0
			logger.GetLogger().Sugar().Infof("Circuit breaker %s: transitioning to half-open state", cb.name)
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
			logger.GetLogger().Sugar().Infof("Circuit breaker %s: opening due to %d failures out of %d requests",
				cb.name, cb.failures, cb.requests)
		}
	case CircuitBreakerHalfOpen:
		// 半开状态下失败，直接打开
		cb.state = CircuitBreakerOpen
		logger.GetLogger().Sugar().Infof("Circuit breaker %s: opening from half-open state due to failure", cb.name)
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
			logger.GetLogger().Sugar().Infof("Circuit breaker %s: closing from half-open state after %d successes",
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
func (cb *CircuitBreaker) GetStats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Stats{
		Name:      cb.name,
		State:     cb.state,
		Failures:  cb.failures,
		Successes: cb.successes,
		Requests:  cb.requests,
	}
}

// Stats 熔断器统计信息
type Stats struct {
	Name      string
	State     CircuitBreakerState
	Failures  int
	Successes int
	Requests  int
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitBreakerClosed
	cb.failures = 0
	cb.successes = 0
	cb.requests = 0
	logger.GetLogger().Sugar().Infof("Circuit breaker %s: reset to closed state", cb.name)
}
