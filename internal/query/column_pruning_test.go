package query

import (
	"testing"
)

func TestColumnPruner(t *testing.T) {
	pruner := NewColumnPruner(true)

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "select all",
			sql:      "SELECT * FROM table",
			expected: []string{"*"},
		},
		{
			name:     "select specific columns",
			sql:      "SELECT col1, col2, col3 FROM table",
			expected: []string{"col1", "col2", "col3"},
		},
		{
			name:     "select with aggregate",
			sql:      "SELECT col1, COUNT(col2), SUM(col3) FROM table",
			expected: []string{"col1"},
		},
		{
			name:     "select with distinct",
			sql:      "SELECT DISTINCT col1 FROM table",
			expected: []string{"col1"},
		},
		{
			name:     "select count star",
			sql:      "SELECT COUNT(*) FROM table",
			expected: []string{"*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := pruner.ExtractRequiredColumns(tt.sql)

			if len(cols) != len(tt.expected) {
				t.Errorf("Expected %d columns, got %d", len(tt.expected), len(cols))
			}

			for i, expected := range tt.expected {
				if i >= len(cols) {
					t.Errorf("Missing column: %s", expected)
					continue
				}
				if cols[i] != expected {
					t.Errorf("Expected %s, got %s", expected, cols[i])
				}
			}
		})
	}
}

func TestColumnPrunerDisabled(t *testing.T) {
	pruner := NewColumnPruner(false)

	cols := pruner.ExtractRequiredColumns("SELECT col1, col2 FROM table")
	if len(cols) != 1 || cols[0] != "*" {
		t.Errorf("Expected ['*'] when disabled, got %v", cols)
	}
}

func TestColumnPrunerCache(t *testing.T) {
	pruner := NewColumnPruner(true)

	sql := "SELECT col1, col2 FROM table"

	// 第一次调用
	cols1 := pruner.ExtractRequiredColumns(sql)

	// 第二次调用（应该从缓存获取）
	cols2 := pruner.ExtractRequiredColumns(sql)

	if len(cols1) != len(cols2) {
		t.Errorf("Cache miss: %d != %d", len(cols1), len(cols2))
	}

	for i, col := range cols1 {
		if cols2[i] != col {
			t.Errorf("Cache mismatch at index %d: %s != %s", i, col, cols2[i])
		}
	}
}

func TestColumnPrunerClearCache(t *testing.T) {
	pruner := NewColumnPruner(true)

	// 添加一些缓存
	pruner.ExtractRequiredColumns("SELECT col1 FROM table1")
	pruner.ExtractRequiredColumns("SELECT col2 FROM table2")

	stats := pruner.GetCacheStats()
	if cacheSize, ok := stats["cache_size"]; !ok {
		t.Error("Expected cache_size in stats")
	} else if cacheSize.(int) != 2 {
		t.Errorf("Expected cache_size 2, got %d", cacheSize.(int))
	}

	// 清除缓存
	pruner.ClearCache()

	stats = pruner.GetCacheStats()
	if cacheSize, ok := stats["cache_size"]; !ok {
		t.Error("Expected cache_size in stats")
	} else if cacheSize.(int) != 0 {
		t.Errorf("Expected cache_size 0 after clear, got %d", cacheSize.(int))
	}
}

func TestColumnPrunerAggregateFunctions(t *testing.T) {
	pruner := NewColumnPruner(true)

	tests := []struct {
		name    string
		sql     string
		exclude []string
	}{
		{
			name:    "COUNT",
			sql:     "SELECT COUNT(col) FROM table",
			exclude: []string{"col"},
		},
		{
			name:    "SUM",
			sql:     "SELECT SUM(amount) FROM table",
			exclude: []string{"amount"},
		},
		{
			name:    "AVG",
			sql:     "SELECT AVG(price) FROM table",
			exclude: []string{"price"},
		},
		{
			name:    "multiple aggregates",
			sql:     "SELECT COUNT(*), SUM(amount), AVG(price) FROM table",
			exclude: []string{"amount", "price"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := pruner.ExtractRequiredColumns(tt.sql)

			for _, excluded := range tt.exclude {
				for _, col := range cols {
					if col == excluded {
						t.Errorf("Aggregate function column should be excluded: %s", excluded)
					}
				}
			}
		})
	}
}

func TestColumnPrunerTableAlias(t *testing.T) {
	pruner := NewColumnPruner(true)

	sql := "SELECT t.col1, t.col2 FROM table AS t"

	cols := pruner.ExtractRequiredColumns(sql)

	// 应该去除表别名，只保留列名
	for _, col := range cols {
		if len(col) > 2 && col[0:2] == "t." {
			t.Errorf("Expected table alias to be removed, got: %s", col)
		}
	}
}

func TestColumnPrunerDistinct(t *testing.T) {
	pruner := NewColumnPruner(true)

	sql := "SELECT DISTINCT col1, col2 FROM table"

	cols := pruner.ExtractRequiredColumns(sql)

	expected := []string{"col1", "col2"}
	if len(cols) != len(expected) {
		t.Errorf("Expected %d columns, got %d", len(expected), len(cols))
	}

	for i, exp := range expected {
		if cols[i] != exp {
			t.Errorf("Expected %s, got %s", exp, cols[i])
		}
	}
}

func TestColumnPrunerComplexSQL(t *testing.T) {
	pruner := NewColumnPruner(true)

	tests := []struct {
		name     string
		sql      string
		expected int
	}{
		{
			name:     "WHERE clause",
			sql:      "SELECT col1, col2 FROM table WHERE col3 > 100",
			expected: 2,
		},
		{
			name:     "GROUP BY",
			sql:      "SELECT col1, COUNT(*) as cnt FROM table GROUP BY col1",
			expected: 2,
		},
		{
			name:     "ORDER BY",
			sql:      "SELECT col1, col2 FROM table ORDER BY col1",
			expected: 2,
		},
		{
			name:     "multiple conditions",
			sql:      "SELECT col1, col2, col3 FROM table WHERE col4 > 100 AND col5 < 200",
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := pruner.ExtractRequiredColumns(tt.sql)

			if len(cols) != tt.expected {
				t.Errorf("Expected %d columns, got %d", tt.expected, len(cols))
			}
		})
	}
}

func TestColumnPrunerEmptySQL(t *testing.T) {
	pruner := NewColumnPruner(true)

	cols := pruner.ExtractRequiredColumns("")

	if len(cols) != 0 {
		t.Errorf("Expected empty columns for empty SQL, got %v", cols)
	}
}
