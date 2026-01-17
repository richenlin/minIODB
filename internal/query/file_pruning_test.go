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

// TestFilePruningWithRealMetadata 测试使用真实元数据进行文件剪枝
func TestFilePruningWithRealMetadata(t *testing.T) {
	// 模拟从 Redis 加载的元数据（包含 MinValues/MaxValues）
	files := []*storage.FileMetadata{
		{
			FilePath:        "users/u001/2024-01-15/1705312800000000000.parquet",
			FileSize:        10240,
			RowCount:        100,
			RowGroupCount:   1,
			CompressionType: "snappy",
			MinValues: map[string]interface{}{
				"timestamp": int64(1705276800000000000), // 2024-01-15 00:00:00
				"amount":    int64(10),
			},
			MaxValues: map[string]interface{}{
				"timestamp": int64(1705363199999999999), // 2024-01-15 23:59:59
				"amount":    int64(500),
			},
		},
		{
			FilePath:        "users/u001/2024-01-16/1705399200000000000.parquet",
			FileSize:        15360,
			RowCount:        150,
			RowGroupCount:   1,
			CompressionType: "snappy",
			MinValues: map[string]interface{}{
				"timestamp": int64(1705363200000000000), // 2024-01-16 00:00:00
				"amount":    int64(20),
			},
			MaxValues: map[string]interface{}{
				"timestamp": int64(1705449599999999999), // 2024-01-16 23:59:59
				"amount":    int64(1000),
			},
		},
		{
			FilePath:        "users/u001/2024-01-17/1705485600000000000.parquet",
			FileSize:        8192,
			RowCount:        80,
			RowGroupCount:   1,
			CompressionType: "snappy",
			MinValues: map[string]interface{}{
				"timestamp": int64(1705449600000000000), // 2024-01-17 00:00:00
				"amount":    int64(50),
			},
			MaxValues: map[string]interface{}{
				"timestamp": int64(1705535999999999999), // 2024-01-17 23:59:59
				"amount":    int64(300),
			},
		},
	}

	optimizer := NewQueryOptimizer()

	// 测试 1: 时间范围查询，应该只返回 2 个文件
	sql := "SELECT * FROM users WHERE timestamp >= 1705363200000000000"
	result, err := optimizer.OptimizeQuery(sql, files)
	if err != nil {
		t.Fatalf("OptimizeQuery failed: %v", err)
	}

	if result.FilesSkipped != 1 {
		t.Errorf("Test 1: Expected 1 file skipped, got %d", result.FilesSkipped)
	}

	if len(result.SelectedFiles) != 2 {
		t.Errorf("Test 1: Expected 2 selected files, got %d", len(result.SelectedFiles))
	}

	// 测试 2: amount 范围查询，应该跳过金额范围不匹配的文件
	sql2 := "SELECT * FROM users WHERE amount > 500"
	result2, err := optimizer.OptimizeQuery(sql2, files)
	if err != nil {
		t.Fatalf("OptimizeQuery failed: %v", err)
	}

	if len(result2.SelectedFiles) != 1 {
		t.Errorf("Test 2: Expected 1 selected file (only file with max amount 1000), got %d", len(result2.SelectedFiles))
	}

	// 测试 3: 无过滤条件，应该返回所有文件
	sql3 := "SELECT * FROM users"
	result3, err := optimizer.OptimizeQuery(sql3, files)
	if err != nil {
		t.Fatalf("OptimizeQuery failed: %v", err)
	}

	if result3.FilesSkipped != 0 {
		t.Errorf("Test 3: Expected 0 files skipped, got %d", result3.FilesSkipped)
	}

	if len(result3.SelectedFiles) != 3 {
		t.Errorf("Test 3: Expected 3 selected files, got %d", len(result3.SelectedFiles))
	}
}

// TestConvertMetadataValues 测试元数据值类型转换
func TestConvertMetadataValues(t *testing.T) {
	// 模拟从 JSON 解析后的原始数据
	rawValues := map[string]interface{}{
		"int_as_string":   "12345",
		"float_as_string": "123.45",
		"pure_string":     "hello",
		"json_number":     float64(67890),
		"json_float":      float64(678.90),
	}

	// 创建一个简单的 Querier 来测试转换函数
	q := &Querier{}
	converted := q.convertMetadataValues(rawValues)

	// 验证整数字符串转换
	if v, ok := converted["int_as_string"].(int64); !ok || v != 12345 {
		t.Errorf("Expected int_as_string to be int64(12345), got %v (%T)", converted["int_as_string"], converted["int_as_string"])
	}

	// 验证浮点数字符串转换
	if v, ok := converted["float_as_string"].(float64); !ok || v != 123.45 {
		t.Errorf("Expected float_as_string to be float64(123.45), got %v (%T)", converted["float_as_string"], converted["float_as_string"])
	}

	// 验证纯字符串保持不变
	if v, ok := converted["pure_string"].(string); !ok || v != "hello" {
		t.Errorf("Expected pure_string to be string(hello), got %v (%T)", converted["pure_string"], converted["pure_string"])
	}

	// 验证 JSON 整数转换（float64 -> int64）
	if v, ok := converted["json_number"].(int64); !ok || v != 67890 {
		t.Errorf("Expected json_number to be int64(67890), got %v (%T)", converted["json_number"], converted["json_number"])
	}

	// 验证 JSON 浮点数保持为 float64
	if v, ok := converted["json_float"].(float64); !ok || v != 678.90 {
		t.Errorf("Expected json_float to be float64(678.90), got %v (%T)", converted["json_float"], converted["json_float"])
	}
}
