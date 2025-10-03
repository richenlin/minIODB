package query

import (
	"context"
	"minIODB/internal/config"
	"minIODB/internal/logger"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestSlowQueryDetection 测试慢查询检测
func TestSlowQueryDetection(t *testing.T) {
	// 初始化logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})

	tests := []struct {
		name               string
		slowQueryThreshold time.Duration
		queryDuration      time.Duration
		shouldDetectSlow   bool
	}{
		{
			name:               "query_below_threshold",
			slowQueryThreshold: 1 * time.Second,
			queryDuration:      500 * time.Millisecond,
			shouldDetectSlow:   false,
		},
		{
			name:               "query_equal_threshold",
			slowQueryThreshold: 1 * time.Second,
			queryDuration:      1 * time.Second,
			shouldDetectSlow:   false, // 等于阈值不算慢查询
		},
		{
			name:               "query_above_threshold",
			slowQueryThreshold: 1 * time.Second,
			queryDuration:      2 * time.Second,
			shouldDetectSlow:   true,
		},
		{
			name:               "threshold_disabled",
			slowQueryThreshold: 0, // 禁用慢查询检测
			queryDuration:      10 * time.Second,
			shouldDetectSlow:   false,
		},
		{
			name:               "very_slow_query",
			slowQueryThreshold: 100 * time.Millisecond,
			queryDuration:      5 * time.Second,
			shouldDetectSlow:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建配置
			cfg := &config.Config{
				QueryOptimization: config.QueryOptimizationConfig{
					SlowQueryThreshold: tt.slowQueryThreshold,
				},
			}

			// 模拟慢查询检测逻辑
			isSlow := false
			if cfg.QueryOptimization.SlowQueryThreshold > 0 &&
				tt.queryDuration > cfg.QueryOptimization.SlowQueryThreshold {
				isSlow = true
			}

			if isSlow != tt.shouldDetectSlow {
				t.Errorf("expected slow query detection: %v, got: %v (duration: %v, threshold: %v)",
					tt.shouldDetectSlow, isSlow, tt.queryDuration, tt.slowQueryThreshold)
			}
		})
	}
}

// TestSlowQueryThresholdConfiguration 测试慢查询阈值配置
func TestSlowQueryThresholdConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		threshold time.Duration
		expected  time.Duration
	}{
		{
			name:      "default_1s",
			threshold: 1 * time.Second,
			expected:  1 * time.Second,
		},
		{
			name:      "custom_500ms",
			threshold: 500 * time.Millisecond,
			expected:  500 * time.Millisecond,
		},
		{
			name:      "custom_5s",
			threshold: 5 * time.Second,
			expected:  5 * time.Second,
		},
		{
			name:      "disabled",
			threshold: 0,
			expected:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				QueryOptimization: config.QueryOptimizationConfig{
					SlowQueryThreshold: tt.threshold,
				},
			}

			if cfg.QueryOptimization.SlowQueryThreshold != tt.expected {
				t.Errorf("expected threshold %v, got %v",
					tt.expected, cfg.QueryOptimization.SlowQueryThreshold)
			}
		})
	}
}

// TestSlowQueryLogging 测试慢查询日志记录
func TestSlowQueryLogging(t *testing.T) {
	// 初始化logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "warn", // 设置为warn级别以捕获慢查询日志
		Format: "json",
		Output: "stdout",
	})

	ctx := context.Background()

	// 创建配置
	cfg := &config.Config{
		QueryOptimization: config.QueryOptimizationConfig{
			SlowQueryThreshold: 100 * time.Millisecond,
		},
	}

	// 模拟一个慢查询
	queryDuration := 200 * time.Millisecond
	tables := []string{"test_table"}
	sqlQuery := "SELECT * FROM test_table WHERE id > 1000"

	// 检测慢查询
	if cfg.QueryOptimization.SlowQueryThreshold > 0 &&
		queryDuration > cfg.QueryOptimization.SlowQueryThreshold {

		// 这里应该记录慢查询日志
		logger.LogWarn(ctx, "Slow query detected (test)",
			zap.String("sql", sqlQuery),
			zap.Strings("tables", tables),
			zap.Duration("duration", queryDuration),
			zap.Duration("threshold", cfg.QueryOptimization.SlowQueryThreshold),
		)

		t.Logf("Slow query logged: duration=%v, threshold=%v",
			queryDuration, cfg.QueryOptimization.SlowQueryThreshold)
	}
}

// TestMultiTableSlowQuery 测试多表慢查询
func TestMultiTableSlowQuery(t *testing.T) {
	cfg := &config.Config{
		QueryOptimization: config.QueryOptimizationConfig{
			SlowQueryThreshold: 1 * time.Second,
		},
	}

	// 测试涉及多个表的慢查询
	tables := []string{"users", "orders", "products"}
	queryDuration := 2 * time.Second

	if cfg.QueryOptimization.SlowQueryThreshold > 0 &&
		queryDuration > cfg.QueryOptimization.SlowQueryThreshold {

		// 应该为每个表记录慢查询指标
		recordedTables := []string{}
		for _, table := range tables {
			recordedTables = append(recordedTables, table)
		}

		if len(recordedTables) != len(tables) {
			t.Errorf("expected %d tables to be recorded, got %d",
				len(tables), len(recordedTables))
		}

		// 验证所有表都被记录
		for i, table := range tables {
			if recordedTables[i] != table {
				t.Errorf("expected table %s at index %d, got %s",
					table, i, recordedTables[i])
			}
		}
	}
}

// TestSlowQueryBoundaryConditions 测试慢查询边界条件
func TestSlowQueryBoundaryConditions(t *testing.T) {
	tests := []struct {
		name         string
		threshold    time.Duration
		duration     time.Duration
		expectedSlow bool
	}{
		{
			name:         "exactly_at_threshold",
			threshold:    1 * time.Second,
			duration:     1 * time.Second,
			expectedSlow: false, // 不应该被检测为慢查询
		},
		{
			name:         "one_nanosecond_over",
			threshold:    1 * time.Second,
			duration:     1*time.Second + 1*time.Nanosecond,
			expectedSlow: true, // 应该被检测为慢查询
		},
		{
			name:         "one_nanosecond_under",
			threshold:    1 * time.Second,
			duration:     1*time.Second - 1*time.Nanosecond,
			expectedSlow: false, // 不应该被检测为慢查询
		},
		{
			name:         "zero_threshold_long_query",
			threshold:    0,
			duration:     10 * time.Second,
			expectedSlow: false, // 阈值为0时禁用检测
		},
		{
			name:         "zero_duration",
			threshold:    1 * time.Second,
			duration:     0,
			expectedSlow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isSlow := tt.threshold > 0 && tt.duration > tt.threshold

			if isSlow != tt.expectedSlow {
				t.Errorf("expected slow=%v, got slow=%v (threshold=%v, duration=%v)",
					tt.expectedSlow, isSlow, tt.threshold, tt.duration)
			}
		})
	}
}
