package query

import (
	"testing"
	"time"

	"minIODB/internal/storage"
)

func TestExtractPredicates(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected int
	}{
		{
			name:     "simple equality",
			sql:      "SELECT * FROM users WHERE id = 'user123'",
			expected: 1,
		},
		{
			name:     "range query",
			sql:      "SELECT * FROM orders WHERE amount > 100 AND amount < 1000",
			expected: 2,
		},
		{
			name:     "timestamp range",
			sql:      "SELECT * FROM logs WHERE timestamp >= '2024-01-01' AND timestamp <= '2024-01-31'",
			expected: 2,
		},
		{
			name:     "no where clause",
			sql:      "SELECT * FROM users",
			expected: 0,
		},
		{
			name:     "with group by",
			sql:      "SELECT region, COUNT(*) FROM sales WHERE year = 2024 GROUP BY region",
			expected: 1,
		},
	}

	fp := NewFilePruner()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicates := fp.ExtractPredicates(tt.sql)
			if len(predicates) != tt.expected {
				t.Errorf("Expected %d predicates, got %d", tt.expected, len(predicates))
			}
		})
	}
}

func TestFilePruning(t *testing.T) {
	files := []*storage.FileMetadata{
		{
			FilePath:  "data_2024_01.parquet",
			RowCount:  1000,
			MinValues: map[string]interface{}{"timestamp": int64(1704067200)},
			MaxValues: map[string]interface{}{"timestamp": int64(1706745599)},
		},
		{
			FilePath:  "data_2024_02.parquet",
			RowCount:  1000,
			MinValues: map[string]interface{}{"timestamp": int64(1706745600)},
			MaxValues: map[string]interface{}{"timestamp": int64(1709251200)},
		},
		{
			FilePath:  "data_2024_03.parquet",
			RowCount:  1000,
			MinValues: map[string]interface{}{"timestamp": int64(1709251200)},
			MaxValues: map[string]interface{}{"timestamp": int64(1711929600)},
		},
	}

	fp := NewFilePruner()

	predicates := []Predicate{
		{Column: "timestamp", Operator: ">=", Value: int64(1706745600)},
	}

	result := fp.PruneFiles(files, predicates)

	if len(result) != 2 {
		t.Errorf("Expected 2 files after pruning, got %d", len(result))
	}
}

func TestFilePruningNoPredicates(t *testing.T) {
	files := []*storage.FileMetadata{
		{FilePath: "file1.parquet", RowCount: 100},
		{FilePath: "file2.parquet", RowCount: 100},
	}

	fp := NewFilePruner()
	result := fp.PruneFiles(files, nil)

	if len(result) != len(files) {
		t.Errorf("Expected all files when no predicates, got %d", len(result))
	}
}

func TestQueryOptimizer(t *testing.T) {
	files := []*storage.FileMetadata{
		{
			FilePath:  "old_data.parquet",
			RowCount:  5000,
			MinValues: map[string]interface{}{"year": int64(2020)},
			MaxValues: map[string]interface{}{"year": int64(2022)},
		},
		{
			FilePath:  "recent_data.parquet",
			RowCount:  3000,
			MinValues: map[string]interface{}{"year": int64(2023)},
			MaxValues: map[string]interface{}{"year": int64(2024)},
		},
	}

	optimizer := NewQueryOptimizer()

	sql := "SELECT * FROM sales WHERE year >= 2023"
	result, err := optimizer.OptimizeQuery(sql, files)

	if err != nil {
		t.Fatalf("OptimizeQuery failed: %v", err)
	}

	if result.FilesSkipped != 1 {
		t.Errorf("Expected 1 file skipped, got %d", result.FilesSkipped)
	}

	if len(result.SelectedFiles) != 1 {
		t.Errorf("Expected 1 selected file, got %d", len(result.SelectedFiles))
	}
}

func TestCompareValues(t *testing.T) {
	fp := NewFilePruner()

	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected int
	}{
		{"int64 less", int64(5), int64(10), -1},
		{"int64 equal", int64(10), int64(10), 0},
		{"int64 greater", int64(15), int64(10), 1},
		{"float64 less", float64(5.5), float64(10.5), -1},
		{"string less", "aaa", "bbb", -1},
		{"string equal", "aaa", "aaa", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fp.compareValues(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestRowGroupPrunerHints(t *testing.T) {
	rp := NewRowGroupPruner()

	predicates := []Predicate{
		{Column: "timestamp", Operator: ">=", Value: time.Now()},
		{Column: "status", Operator: "=", Value: "active"},
	}

	hints := rp.GetPushdownHints(predicates)

	if hints["pushdown_enabled"] != true {
		t.Error("Expected pushdown_enabled to be true")
	}

	columns, ok := hints["filter_columns"].([]string)
	if !ok {
		t.Fatal("Expected filter_columns to be []string")
	}

	if len(columns) != 2 {
		t.Errorf("Expected 2 filter columns, got %d", len(columns))
	}
}
