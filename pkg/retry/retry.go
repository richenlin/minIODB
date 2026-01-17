package retry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// Config 重试配置
type Config struct {
	MaxAttempts     int           // 最大重试次数
	InitialInterval time.Duration // 初始重试间隔
	MaxInterval     time.Duration // 最大重试间隔
	Multiplier      float64       // 退避倍数
	MaxJitter       time.Duration // 最大抖动时间
}

// DefaultConfig 默认重试配置
var DefaultConfig = Config{
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

// Func 可重试的函数类型
type Func func(ctx context.Context) error

// Do 执行重试逻辑
// operation 参数用于日志记录和监控
func Do(ctx context.Context, config Config, operation string, fn Func) error {
	var lastErr error
	interval := config.InitialInterval

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			if attempt > 1 {
				log.Printf("Operation %s succeeded after %d attempts", operation, attempt)
			}
			return nil
		}

		lastErr = err

		// 检查是否可重试
		if !IsRetryable(err) {
			log.Printf("Non-retryable error in %s (attempt %d/%d): %v", operation, attempt, config.MaxAttempts, err)
			return err
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 如果不是最后一次尝试，则等待重试
		if attempt < config.MaxAttempts {
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
				return ctx.Err()
			}
		}
	}

	log.Printf("Max retry attempts exceeded for %s: %v", operation, lastErr)
	return fmt.Errorf("max retry attempts (%d) exceeded for %s: %w", config.MaxAttempts, operation, lastErr)
}

// WithMetrics 返回一个带监控指标的重试函数
// 这个函数允许调用者传入自定义的指标记录回调
type MetricsFunc func(attempt int, err error, duration time.Duration)

// DoWithMetrics 执行带指标记录的重试逻辑
func DoWithMetrics(ctx context.Context, config Config, operation string, fn Func, metrics MetricsFunc) error {
	var lastErr error
	interval := config.InitialInterval

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		start := time.Now()
		err := fn(ctx)
		duration := time.Since(start)

		// 记录指标
		if metrics != nil {
			metrics(attempt, err, duration)
		}

		if err == nil {
			if attempt > 1 {
				log.Printf("Operation %s succeeded after %d attempts in %v", operation, attempt, duration)
			}
			return nil
		}

		lastErr = err

		// 检查是否可重试
		if !IsRetryable(err) {
			log.Printf("Non-retryable error in %s (attempt %d/%d): %v", operation, attempt, config.MaxAttempts, err)
			return err
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 如果不是最后一次尝试，则等待重试
		if attempt < config.MaxAttempts {
			log.Printf("Retrying %s (attempt %d/%d) after %v: %v", operation, attempt, config.MaxAttempts, interval, err)

			select {
			case <-time.After(interval):
				// 计算下次重试间隔（指数退避）
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
				return ctx.Err()
			}
		}
	}

	log.Printf("Max retry attempts exceeded for %s: %v", operation, lastErr)
	return fmt.Errorf("max retry attempts (%d) exceeded for %s: %w", config.MaxAttempts, operation, lastErr)
}
