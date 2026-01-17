package coordinator

import (
	"encoding/json"
	"testing"
)

func TestQueryAnalyzer(t *testing.T) {
	analyzer := NewQueryAnalyzer()

	tests := []struct {
		name          string
		sql           string
		expectedType  QueryType
		expectedAggs  int
		expectedGroup int
	}{
		{
			name:          "simple select",
			sql:           "SELECT * FROM users",
			expectedType:  QueryTypeSimple,
			expectedAggs:  0,
			expectedGroup: 0,
		},
		{
			name:          "count query",
			sql:           "SELECT COUNT(*) FROM orders",
			expectedType:  QueryTypeAggregate,
			expectedAggs:  1,
			expectedGroup: 0,
		},
		{
			name:          "sum query",
			sql:           "SELECT SUM(amount) FROM orders",
			expectedType:  QueryTypeAggregate,
			expectedAggs:  1,
			expectedGroup: 0,
		},
		{
			name:          "avg query",
			sql:           "SELECT AVG(price) FROM products",
			expectedType:  QueryTypeAggregate,
			expectedAggs:  1,
			expectedGroup: 0,
		},
		{
			name:          "group by query",
			sql:           "SELECT region, SUM(amount) FROM sales GROUP BY region",
			expectedType:  QueryTypeGroupBy,
			expectedAggs:  1,
			expectedGroup: 1,
		},
		{
			name:          "multiple aggregations",
			sql:           "SELECT COUNT(*), SUM(amount), AVG(price) FROM orders",
			expectedType:  QueryTypeAggregate,
			expectedAggs:  3,
			expectedGroup: 0,
		},
		{
			name:          "order by with limit",
			sql:           "SELECT * FROM users ORDER BY created_at DESC LIMIT 10",
			expectedType:  QueryTypeOrderBy,
			expectedAggs:  0,
			expectedGroup: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.Analyze(tt.sql)

			if result.Type != tt.expectedType {
				t.Errorf("Expected type %v, got %v", tt.expectedType, result.Type)
			}

			if len(result.Aggregations) != tt.expectedAggs {
				t.Errorf("Expected %d aggregations, got %d", tt.expectedAggs, len(result.Aggregations))
			}

			if len(result.GroupByKeys) != tt.expectedGroup {
				t.Errorf("Expected %d group by keys, got %d", tt.expectedGroup, len(result.GroupByKeys))
			}
		})
	}
}

func TestSimpleAggregateStrategy(t *testing.T) {
	strategy := &SimpleAggregateStrategy{}
	analyzer := NewQueryAnalyzer()

	results := []string{
		`[{"count": 100}]`,
		`[{"count": 150}]`,
		`[{"count": 50}]`,
	}

	analyzed := analyzer.Analyze("SELECT COUNT(*) as count FROM orders")

	output, err := strategy.Reduce(results, analyzed)
	if err != nil {
		t.Fatalf("Reduce failed: %v", err)
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if len(data) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(data))
	}

	if count, ok := data[0]["count"].(float64); !ok || count != 300 {
		t.Errorf("Expected count 300, got %v", data[0]["count"])
	}
}

func TestAvgDecomposeStrategy(t *testing.T) {
	strategy := &AvgDecomposeStrategy{}
	analyzer := NewQueryAnalyzer()

	analyzed := analyzer.Analyze("SELECT AVG(price) as avg_price FROM products")

	mapSQL := strategy.MapSQL("SELECT AVG(price) FROM products", analyzed)
	if !containsIgnoreCase(mapSQL, "SUM(price)") || !containsIgnoreCase(mapSQL, "COUNT(price)") {
		t.Errorf("MapSQL should decompose AVG into SUM and COUNT: %s", mapSQL)
	}

	results := []string{
		`[{"_sum_price": 1000, "_count_price": 10}]`,
		`[{"_sum_price": 2000, "_count_price": 20}]`,
	}

	output, err := strategy.Reduce(results, analyzed)
	if err != nil {
		t.Fatalf("Reduce failed: %v", err)
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if len(data) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(data))
	}

	if avg, ok := data[0]["avg_price"].(float64); !ok || avg != 100 {
		t.Errorf("Expected avg 100 (3000/30), got %v", data[0]["avg_price"])
	}
}

func TestGroupAggregateStrategy(t *testing.T) {
	strategy := &GroupAggregateStrategy{}
	analyzer := NewQueryAnalyzer()

	analyzed := analyzer.Analyze("SELECT region, SUM(amount) as total FROM sales GROUP BY region")

	results := []string{
		`[{"region": "EAST", "total": 1000}, {"region": "WEST", "total": 2000}]`,
		`[{"region": "EAST", "total": 500}, {"region": "WEST", "total": 1500}]`,
	}

	output, err := strategy.Reduce(results, analyzed)
	if err != nil {
		t.Fatalf("Reduce failed: %v", err)
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if len(data) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(data))
	}
}

func TestTopNMergeStrategy(t *testing.T) {
	strategy := &TopNMergeStrategy{}
	analyzer := NewQueryAnalyzer()

	analyzed := analyzer.Analyze("SELECT * FROM users ORDER BY score DESC LIMIT 3")

	results := []string{
		`[{"name": "Alice", "score": 95}, {"name": "Bob", "score": 85}]`,
		`[{"name": "Charlie", "score": 90}, {"name": "David", "score": 80}]`,
	}

	output, err := strategy.Reduce(results, analyzed)
	if err != nil {
		t.Fatalf("Reduce failed: %v", err)
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if len(data) != 3 {
		t.Errorf("Expected 3 rows (LIMIT 3), got %d", len(data))
	}
}

func TestUnionStrategy(t *testing.T) {
	strategy := &UnionStrategy{}
	analyzed := &AnalyzedQuery{}

	results := []string{
		`[{"id": 1}, {"id": 2}]`,
		`[{"id": 3}, {"id": 4}]`,
	}

	output, err := strategy.Reduce(results, analyzed)
	if err != nil {
		t.Fatalf("Reduce failed: %v", err)
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if len(data) != 4 {
		t.Errorf("Expected 4 rows, got %d", len(data))
	}
}

func TestStrategyFactory(t *testing.T) {
	factory := NewStrategyFactory()

	tests := []string{
		"simple_aggregate",
		"avg_decompose",
		"group_aggregate",
		"topn_merge",
		"union",
	}

	for _, name := range tests {
		strategy := factory.GetStrategy(name)
		if strategy == nil {
			t.Errorf("Strategy %s should not be nil", name)
		}
	}

	unknown := factory.GetStrategy("unknown_strategy")
	if unknown == nil {
		t.Error("Unknown strategy should return default (union)")
	}
}

func TestRequiresDistributedProcessing(t *testing.T) {
	analyzer := NewQueryAnalyzer()

	tests := []struct {
		sql      string
		expected bool
	}{
		{"SELECT * FROM users", false},
		{"SELECT COUNT(*) FROM users", true},
		{"SELECT region, SUM(amount) FROM sales GROUP BY region", true},
		{"SELECT * FROM users ORDER BY id LIMIT 10", true},
	}

	for _, tt := range tests {
		analyzed := analyzer.Analyze(tt.sql)
		result := analyzer.RequiresDistributedProcessing(analyzed)
		if result != tt.expected {
			t.Errorf("SQL %s: expected %v, got %v", tt.sql, tt.expected, result)
		}
	}
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && (s[:len(substr)] == substr || containsIgnoreCase(s[1:], substr)))
}
