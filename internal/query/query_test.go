package query

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/config"

	"go.uber.org/zap"
)

// TestQuerierBasicStructure tests basic structure creation
func TestQuerierBasicStructure(t *testing.T) {
	logger := zap.NewNop() // Use no-op logger for testing

	querier := &Querier{
		queryStats: &QueryStats{
			TablesQueried: make(map[string]int64),
			QueryTypes:    make(map[string]int64),
			FastestQuery:  time.Hour,
		},
		config: &config.Config{
			QueryOptimization: config.QueryOptimizationConfig{
				QueryTimeout:       30 * time.Second,
				SlowQueryThreshold: 1 * time.Second,
				LocalFileIndexTTL:  5 * time.Minute,
			},
		},
		logger:      logger,
		tempDir:     "/tmp/test",
		initializedViews: make(map[string]bool),
		localFileIndex:   make(map[string][]string),
		localIndexLastScan: make(map[string]time.Time),
	}

	ctx := context.Background()

	// Test basic query stats operations
	tables := []string{"users"}
	queryTime := 100 * time.Millisecond
	queryType := "SELECT"

	// Test stats update
	querier.updateQueryStats(ctx, tables, queryTime, queryType)

	stats := querier.GetQueryStats(ctx)
	if stats.TotalQueries != 1 {
		t.Errorf("Expected TotalQueries to be 1, got %d", stats.TotalQueries)
	}
	if stats.TablesQueried["users"] != 1 {
		t.Errorf("Expected TablesQueried['users'] to be 1, got %d", stats.TablesQueried["users"])
	}
	if stats.QueryTypes["SELECT"] != 1 {
		t.Errorf("Expected QueryTypes['SELECT'] to be 1, got %d", stats.QueryTypes["SELECT"])
	}

	// Test cache hit stats
	querier.updateCacheHitStats(ctx, tables, queryTime/2)
	stats = querier.GetQueryStats(ctx)
	if stats.TotalQueries != 2 {
		t.Errorf("Expected TotalQueries to be 2, got %d", stats.TotalQueries)
	}
	if stats.CacheHits != 1 {
		t.Errorf("Expected CacheHits to be 1, got %d", stats.CacheHits)
	}

	// Test error stats
	querier.updateErrorStats(ctx)
	stats = querier.GetQueryStats(ctx)
	if stats.ErrorCount != 1 {
		t.Errorf("Expected ErrorCount to be 1, got %d", stats.ErrorCount)
	}
}

// TestFormatResults tests result formatting
func TestFormatResults(t *testing.T) {
	querier := &Querier{}

	// Test with nil results
	result := querier.formatResults(nil)
	if result != "[]" {
		t.Errorf("Expected formatted result to be '[]', got '%s'", result)
	}

	// Test with empty results
	result = querier.formatResults([]map[string]interface{}{})
	if result != "[]" {
		t.Errorf("Expected formatted result to be '[]', got '%s'", result)
	}

	// Test with sample data
	results := []map[string]interface{}{
		{"id": "test-id", "name": "Test User"},
		{"id": "test-id-2", "name": "Another User"},
	}

	result = querier.formatResults(results)
	if result == "" {
		t.Error("Expected non-empty formatted result")
	}
	// Basic format checks
	if len(result) < 10 {
		t.Error("Expected longer formatted result")
	}
}

// TestPreparedStatementDetection tests prepared statement detection logic
func TestPreparedStatementDetection(t *testing.T) {
	querier := &Querier{}

	tests := []struct {
		name     string
		sql      string
		expected bool
	}{
		{
			name:     "count_aggregation",
			sql:      "SELECT COUNT(*) FROM users",
			expected: true,
		},
		{
			name:     "sum_aggregation",
			sql:      "SELECT SUM(amount) FROM orders",
			expected: true,
		},
		{
			name:     "group_by",
			sql:      "SELECT category, COUNT(*) FROM products GROUP BY category",
			expected: true,
		},
		{
			name:     "simple_select",
			sql:      "SELECT * FROM users",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result := querier.canUsePreparedStatement(ctx, tt.sql)
			if result != tt.expected {
				t.Errorf("Expected %v for SQL '%s', got %v", tt.expected, tt.sql, result)
			}
		})
	}
}