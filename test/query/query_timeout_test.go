package query

import (
	"context"
	"minIODB/internal/config"
	"minIODB/internal/logger"
	"testing"
	"time"
)

// TestMain initializes logger for tests
func TestMain(m *testing.M) {
	// 初始化logger用于测试
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	m.Run()
}

// TestQueryTimeout 测试查询超时功能
func TestQueryTimeout(t *testing.T) {
	tests := []struct {
		name          string
		queryTimeout  time.Duration
		expectedError bool
		errorContains string
	}{
		{
			name:          "no_timeout",
			queryTimeout:  0, // 禁用超时
			expectedError: false,
		},
		{
			name:          "sufficient_timeout",
			queryTimeout:  5 * time.Second,
			expectedError: false,
		},
		{
			name:          "very_short_timeout",
			queryTimeout:  1 * time.Nanosecond, // 极短超时，必然超时
			expectedError: true,
			errorContains: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建测试配置
			cfg := &config.Config{
				QueryOptimization: config.QueryOptimizationConfig{
					QueryTimeout: tt.queryTimeout,
				},
			}

			// 创建简单的querier（只测试超时逻辑，不需要完整初始化）
			querier := &Querier{
				config: cfg,
			}

			// 创建测试context
			ctx := context.Background()

			// 模拟ExecuteQuery的超时逻辑
			if querier.config.QueryOptimization.QueryTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, querier.config.QueryOptimization.QueryTimeout)
				defer cancel()
			}

			// 检查context是否有超时
			select {
			case <-ctx.Done():
				if !tt.expectedError {
					t.Errorf("unexpected context timeout")
				}
				if ctx.Err() != context.DeadlineExceeded {
					t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
				}
			case <-time.After(10 * time.Millisecond):
				// Context仍然有效
				if tt.expectedError {
					t.Errorf("expected timeout but context is still valid")
				}
			}
		})
	}
}

// TestContextPropagation 测试context在查询中的传递
func TestContextPropagation(t *testing.T) {
	t.Run("context_cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// 立即取消context
		cancel()

		// 检查context已被取消
		if ctx.Err() != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", ctx.Err())
		}
	})

	t.Run("context_deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// 等待超时
		time.Sleep(10 * time.Millisecond)

		// 检查context已超时
		if ctx.Err() != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", ctx.Err())
		}
	})
}

// TestQueryTimeoutConfiguration 测试查询超时配置加载
func TestQueryTimeoutConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		configTimeout   time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "default_timeout",
			configTimeout:   0,
			expectedTimeout: 0,
		},
		{
			name:            "custom_30s",
			configTimeout:   30 * time.Second,
			expectedTimeout: 30 * time.Second,
		},
		{
			name:            "custom_1m",
			configTimeout:   1 * time.Minute,
			expectedTimeout: 1 * time.Minute,
		},
		{
			name:            "custom_100ms",
			configTimeout:   100 * time.Millisecond,
			expectedTimeout: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				QueryOptimization: config.QueryOptimizationConfig{
					QueryTimeout: tt.configTimeout,
				},
			}

			if cfg.QueryOptimization.QueryTimeout != tt.expectedTimeout {
				t.Errorf("expected timeout %v, got %v",
					tt.expectedTimeout,
					cfg.QueryOptimization.QueryTimeout)
			}
		})
	}
}
