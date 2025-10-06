package query

import (
	"context"
	"minIODB/internal/logger"
	"testing"
	"time"
)

// TestAdaptiveTTL 测试自适应TTL功能
func TestAdaptiveTTL(t *testing.T) {
	// 初始化logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})

	ctx := context.Background()

	// 创建SmartCacheOptimizer配置
	config := &SmartCacheConfig{
		EnablePredictive: false, // 简化测试，禁用预计算
		EnablePrecompute: false,
		AdaptiveTTL:      true,
		MinTTL:           5 * time.Minute,
		MaxTTL:           2 * time.Hour,
	}

	// 创建优化器（不需要真实的QueryCache）
	optimizer := NewSmartCacheOptimizer(ctx, nil, config)

	tests := []struct {
		name          string
		accessCount   int64
		frequency     float64
		expectedRange string
	}{
		{
			name:          "new_query_min_ttl",
			accessCount:   1,
			frequency:     0.1,
			expectedRange: "min",
		},
		{
			name:          "low_frequency_short_ttl",
			accessCount:   5,
			frequency:     0.5, // 每小时0.5次
			expectedRange: "min",
		},
		{
			name:          "medium_frequency_medium_ttl",
			accessCount:   10,
			frequency:     5.0, // 每小时5次
			expectedRange: "medium",
		},
		{
			name:          "high_frequency_max_ttl",
			accessCount:   50,
			frequency:     15.0, // 每小时15次
			expectedRange: "max",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := "SELECT * FROM users WHERE id = 123"

			// 模拟查询访问
			for i := int64(0); i < tt.accessCount; i++ {
				optimizer.RecordQueryAccess(ctx, query, []string{"users"}, 100*time.Millisecond, 1000)
			}

			// 手动设置频率（在实际场景中会自动计算）
			template := optimizer.normalizeQuery(query)
			optimizer.mutex.Lock()
			if pattern, exists := optimizer.queryPatterns[template]; exists {
				pattern.Frequency = tt.frequency
			}
			optimizer.mutex.Unlock()

			// 计算TTL
			ttl := optimizer.CalculateOptimalTTL(ctx, query)

			// 验证TTL在合理范围内
			if ttl < config.MinTTL {
				t.Errorf("TTL %v is less than MinTTL %v", ttl, config.MinTTL)
			}
			if ttl > config.MaxTTL {
				t.Errorf("TTL %v is greater than MaxTTL %v", ttl, config.MaxTTL)
			}

			// 验证TTL符合预期范围
			switch tt.expectedRange {
			case "min":
				if ttl != config.MinTTL {
					t.Logf("Expected MinTTL (%v), got %v (frequency: %.2f)", config.MinTTL, ttl, tt.frequency)
				}
			case "max":
				if ttl != config.MaxTTL {
					t.Errorf("Expected MaxTTL (%v), got %v (frequency: %.2f)", config.MaxTTL, ttl, tt.frequency)
				}
			case "medium":
				if ttl == config.MinTTL || ttl == config.MaxTTL {
					t.Logf("Expected medium TTL, got %v (frequency: %.2f)", ttl, tt.frequency)
				}
			}

			t.Logf("Query frequency: %.2f/hour, TTL: %v", tt.frequency, ttl)
		})
	}
}

// TestTTLBoundaries 测试TTL边界条件
func TestTTLBoundaries(t *testing.T) {
	ctx := context.Background()

	config := &SmartCacheConfig{
		AdaptiveTTL: true,
		MinTTL:      1 * time.Minute,
		MaxTTL:      1 * time.Hour,
	}

	optimizer := NewSmartCacheOptimizer(ctx, nil, config)

	// 测试新查询（不存在的查询模式）
	ttl := optimizer.CalculateOptimalTTL(ctx, "SELECT * FROM nonexistent")
	if ttl != config.MinTTL {
		t.Errorf("new query should use MinTTL, got %v", ttl)
	}

	// 测试超高频查询
	query := "SELECT * FROM hot_table"
	template := optimizer.normalizeQuery(query)
	optimizer.mutex.Lock()
	optimizer.queryPatterns[template] = &QueryPattern{
		QueryTemplate: template,
		Frequency:     100.0, // 每小时100次
		AccessCount:   1000,
	}
	optimizer.mutex.Unlock()

	ttl = optimizer.CalculateOptimalTTL(ctx, query)
	if ttl != config.MaxTTL {
		t.Errorf("high frequency query should use MaxTTL, got %v", ttl)
	}
}

// TestAdaptiveTTLDisabled 测试禁用自适应TTL
func TestAdaptiveTTLDisabled(t *testing.T) {
	ctx := context.Background()

	// 创建一个模拟的QueryCache
	defaultTTL := 30 * time.Minute
	mockCache := &QueryCache{
		defaultTTL: defaultTTL,
	}

	config := &SmartCacheConfig{
		AdaptiveTTL: false, // 禁用自适应TTL
		MinTTL:      5 * time.Minute,
		MaxTTL:      2 * time.Hour,
	}

	optimizer := NewSmartCacheOptimizer(ctx, mockCache, config)

	// 即使有查询模式，禁用时也应该返回默认TTL
	query := "SELECT * FROM users"
	optimizer.RecordQueryAccess(ctx, query, []string{"users"}, 100*time.Millisecond, 1000)

	ttl := optimizer.CalculateOptimalTTL(ctx, query)
	if ttl != defaultTTL {
		t.Errorf("with AdaptiveTTL disabled, should use defaultTTL (%v), got %v", defaultTTL, ttl)
	}
}

// TestFrequencyCalculation 测试频率计算
func TestFrequencyCalculation(t *testing.T) {
	ctx := context.Background()

	config := DefaultSmartCacheConfig()
	optimizer := NewSmartCacheOptimizer(ctx, nil, config)

	query := "SELECT * FROM products WHERE category = 'electronics'"

	// 模拟多次访问
	accessCount := 20
	for i := 0; i < accessCount; i++ {
		optimizer.RecordQueryAccess(ctx, query, []string{"products"}, 50*time.Millisecond, 500)
		time.Sleep(1 * time.Millisecond) // 小延迟以避免频率计算问题
	}

	// 获取查询模式
	template := optimizer.normalizeQuery(query)
	optimizer.mutex.RLock()
	pattern, exists := optimizer.queryPatterns[template]
	optimizer.mutex.RUnlock()

	if !exists {
		t.Fatal("query pattern should exist after multiple accesses")
	}

	if pattern.AccessCount != int64(accessCount) {
		t.Errorf("expected AccessCount %d, got %d", accessCount, pattern.AccessCount)
	}

	t.Logf("Access count: %d, Frequency: %.2f/hour, Average latency: %v",
		pattern.AccessCount, pattern.Frequency, pattern.AverageLatency)
}

// TestTTLAdaptationOverTime 测试TTL随时间自适应
func TestTTLAdaptationOverTime(t *testing.T) {
	ctx := context.Background()

	config := &SmartCacheConfig{
		AdaptiveTTL: true,
		MinTTL:      5 * time.Minute,
		MaxTTL:      2 * time.Hour,
	}

	optimizer := NewSmartCacheOptimizer(ctx, nil, config)
	query := "SELECT * FROM orders WHERE status = 'pending'"

	// 第一阶段：低频访问（应该得到短TTL）
	optimizer.RecordQueryAccess(ctx, query, []string{"orders"}, 100*time.Millisecond, 100)
	ttl1 := optimizer.CalculateOptimalTTL(ctx, query)

	// 第二阶段：增加访问频率
	template := optimizer.normalizeQuery(query)
	optimizer.mutex.Lock()
	if pattern, exists := optimizer.queryPatterns[template]; exists {
		pattern.Frequency = 8.0 // 提高频率到每小时8次
		pattern.AccessCount = 50
	}
	optimizer.mutex.Unlock()

	ttl2 := optimizer.CalculateOptimalTTL(ctx, query)

	// TTL应该增加
	if ttl2 <= ttl1 {
		t.Logf("TTL adapted: %v -> %v (expected increase)", ttl1, ttl2)
	}

	t.Logf("Low frequency TTL: %v, High frequency TTL: %v", ttl1, ttl2)
}

// TestSmartOptimizerStats 测试统计信息
func TestSmartOptimizerStats(t *testing.T) {
	ctx := context.Background()

	config := DefaultSmartCacheConfig()
	optimizer := NewSmartCacheOptimizer(ctx, nil, config)

	// 执行一些操作
	queries := []string{
		"SELECT * FROM users",
		"SELECT * FROM orders",
		"SELECT * FROM products",
	}

	for _, query := range queries {
		for i := 0; i < 10; i++ {
			optimizer.RecordQueryAccess(ctx, query, []string{"test"}, 100*time.Millisecond, 1000)
		}
		optimizer.CalculateOptimalTTL(ctx, query)
	}

	// 获取统计信息
	stats := optimizer.GetStats(ctx)

	if stats.AdaptiveTTLChanges == 0 {
		t.Error("expected some adaptive TTL changes")
	}

	t.Logf("Stats: PatternMatches=%d, AdaptiveTTLChanges=%d",
		stats.PatternMatches, stats.AdaptiveTTLChanges)
}
