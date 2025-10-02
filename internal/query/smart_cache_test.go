package query

import (
	"context"
	"testing"
	"time"
)

func TestNewSmartCacheOptimizer(t *testing.T) {
	cache := &QueryCache{}
	config := &SmartCacheConfig{
		EnablePredictive: true,
		AdaptiveTTL:      true,
		MinTTL:           1 * time.Minute,
		MaxTTL:           1 * time.Hour,
	}

	optimizer := NewSmartCacheOptimizer(cache, config)

	if optimizer == nil {
		t.Fatal("NewSmartCacheOptimizer returned nil")
	}

	if optimizer.queryCache != cache {
		t.Error("queryCache not set correctly")
	}

	if optimizer.config != config {
		t.Error("config not set correctly")
	}

	if optimizer.queryPatterns == nil {
		t.Error("queryPatterns not initialized")
	}
}

func TestDefaultSmartCacheConfig(t *testing.T) {
	config := DefaultSmartCacheConfig()

	if !config.EnablePredictive {
		t.Error("Expected EnablePredictive to be true")
	}

	if !config.AdaptiveTTL {
		t.Error("Expected AdaptiveTTL to be true")
	}

	if !config.CacheWarmupEnabled {
		t.Error("Expected CacheWarmupEnabled to be true")
	}

	if config.MinTTL != 5*time.Minute {
		t.Errorf("Expected MinTTL 5m, got %v", config.MinTTL)
	}

	if config.MaxTTL != 2*time.Hour {
		t.Errorf("Expected MaxTTL 2h, got %v", config.MaxTTL)
	}

	if config.PatternThreshold != 5 {
		t.Errorf("Expected PatternThreshold 5, got %d", config.PatternThreshold)
	}
}

func TestRecordQueryAccess(t *testing.T) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	query := "SELECT * FROM users WHERE id = ?"
	tables := []string{"users"}
	latency := 100 * time.Millisecond
	resultSize := int64(1024)

	// Should not panic
	optimizer.RecordQueryAccess(query, tables, latency, resultSize)

	// Record multiple times to build pattern
	for i := 0; i < 10; i++ {
		optimizer.RecordQueryAccess(query, tables, latency, resultSize)
	}

	// Check patterns were recorded
	patterns := optimizer.GetQueryPatterns()
	if patterns == nil {
		t.Error("GetQueryPatterns returned nil")
	}
}

func TestCalculateOptimalTTL(t *testing.T) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	tests := []struct {
		name        string
		query       string
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{
			name:        "Simple SELECT",
			query:       "SELECT * FROM users",
			expectedMin: 5 * time.Minute,
			expectedMax: 2 * time.Hour,
		},
		{
			name:        "Aggregate query",
			query:       "SELECT COUNT(*) FROM orders",
			expectedMin: 5 * time.Minute,
			expectedMax: 2 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttl := optimizer.CalculateOptimalTTL(tt.query)
			if ttl < tt.expectedMin || ttl > tt.expectedMax {
				t.Errorf("TTL %v out of range [%v, %v]", ttl, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestGetPredictiveQueries(t *testing.T) {
	cache := &QueryCache{}
	config := DefaultSmartCacheConfig()
	config.PatternThreshold = 2
	optimizer := NewSmartCacheOptimizer(cache, config)

	// Record some queries to create patterns
	query1 := "SELECT * FROM users WHERE id = 1"
	tables := []string{"users"}

	for i := 0; i < 5; i++ {
		optimizer.RecordQueryAccess(query1, tables, 100*time.Millisecond, 1024)
	}

	// Get predictive queries
	predictions := optimizer.GetPredictiveQueries()

	// Should return a list (may be empty)
	if predictions == nil {
		t.Error("GetPredictiveQueries returned nil")
	}
}

func TestWarmupCache(t *testing.T) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	ctx := context.Background()
	queries := []string{
		"SELECT * FROM users",
		"SELECT * FROM products",
	}

	err := optimizer.WarmupCache(ctx, queries)
	if err != nil {
		t.Errorf("WarmupCache returned error: %v", err)
	}

	stats := optimizer.GetStats()
	if stats.CacheWarmupCount != int64(len(queries)) {
		t.Errorf("Expected CacheWarmupCount=%d, got %d", len(queries), stats.CacheWarmupCount)
	}
}

func TestGetStats(t *testing.T) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	// Record some accesses
	optimizer.RecordQueryAccess("SELECT 1", []string{"test"}, 50*time.Millisecond, 512)
	optimizer.RecordQueryAccess("SELECT 2", []string{"test"}, 100*time.Millisecond, 1024)

	stats := optimizer.GetStats()

	// Just verify we can get stats without error
	if stats == nil {
		t.Error("GetStats returned nil")
	}
}

func TestGetQueryPatterns(t *testing.T) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	query := "SELECT * FROM test"
	tables := []string{"test"}

	// Record multiple times to create pattern
	for i := 0; i < 10; i++ {
		optimizer.RecordQueryAccess(query, tables, 100*time.Millisecond, 2048)
	}

	patterns := optimizer.GetQueryPatterns()

	// Patterns map should exist
	if patterns == nil {
		t.Error("GetQueryPatterns returned nil")
	}
}

func TestNormalizeQuery(t *testing.T) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	tests := []struct {
		input string
	}{
		{input: "SELECT * FROM users WHERE id = 123"},
		{input: "SELECT * FROM users WHERE name = 'test'"},
		{input: "SELECT * FROM orders WHERE user_id = 456 AND status = 'active'"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := optimizer.normalizeQuery(tt.input)
			// Just verify it returns something (implementation may vary)
			if result == "" {
				t.Error("normalizeQuery returned empty string")
			}
		})
	}
}

func BenchmarkCalculateOptimalTTL(b *testing.B) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	query := "SELECT * FROM users WHERE id = 1"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		optimizer.CalculateOptimalTTL(query)
	}
}

func BenchmarkRecordQueryAccess(b *testing.B) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	query := "SELECT * FROM users WHERE id = 1"
	tables := []string{"users"}
	latency := 100 * time.Millisecond
	resultSize := int64(1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		optimizer.RecordQueryAccess(query, tables, latency, resultSize)
	}
}

func BenchmarkNormalizeQuery(b *testing.B) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	query := "SELECT * FROM users WHERE id = 123 AND name = 'test' AND age = 25"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		optimizer.normalizeQuery(query)
	}
}

func BenchmarkGetPredictiveQueries(b *testing.B) {
	cache := &QueryCache{}
	optimizer := NewSmartCacheOptimizer(cache, DefaultSmartCacheConfig())

	// Setup some patterns
	for i := 0; i < 10; i++ {
		optimizer.RecordQueryAccess("SELECT * FROM test", []string{"test"}, 100*time.Millisecond, 1024)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		optimizer.GetPredictiveQueries()
	}
}
