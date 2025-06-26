package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTablesFromSQL(t *testing.T) {
	// 创建一个测试用的Querier实例
	querier := &Querier{}

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "Simple FROM clause",
			sql:      "SELECT * FROM users",
			expected: []string{"users"},
		},
		{
			name:     "FROM with JOIN",
			sql:      "SELECT u.name, d.name FROM users u JOIN departments d ON u.dept_id = d.id",
			expected: []string{"users", "departments"},
		},
		{
			name:     "Multiple JOINs",
			sql:      "SELECT * FROM users u JOIN departments d ON u.dept_id = d.id JOIN locations l ON d.location_id = l.id",
			expected: []string{"users", "departments", "locations"},
		},
		{
			name:     "Complex query with WHERE",
			sql:      "SELECT departments.department_name, AVG(employees.salary) FROM employees JOIN departments ON employees.department_id = departments.department_id WHERE employees.age > 25",
			expected: []string{"employees", "departments"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := querier.extractTablesFromSQL(tt.sql)
			assert.ElementsMatch(t, tt.expected, result, "Expected tables %v, got %v", tt.expected, result)
		})
	}
}

func TestParseWhereClause(t *testing.T) {
	// 创建一个测试用的Querier实例
	querier := &Querier{}

	tests := []struct {
		name        string
		sql         string
		expectedID  string
		expectedDay string
	}{
		{
			name:        "Simple WHERE with id",
			sql:         "SELECT * FROM users WHERE id = 'user123'",
			expectedID:  "user123",
			expectedDay: "",
		},
		{
			name:        "WHERE with day",
			sql:         "SELECT * FROM users WHERE day = '2024-01-01'",
			expectedID:  "",
			expectedDay: "2024-01-01",
		},
		{
			name:        "WHERE with both id and day",
			sql:         "SELECT * FROM users WHERE id = 'user123' AND day = '2024-01-01'",
			expectedID:  "user123",
			expectedDay: "2024-01-01",
		},
		{
			name:        "WHERE with double quotes",
			sql:         `SELECT * FROM users WHERE id = "user123" AND day = "2024-01-01"`,
			expectedID:  "user123",
			expectedDay: "2024-01-01",
		},
		{
			name:        "No WHERE clause",
			sql:         "SELECT * FROM users",
			expectedID:  "",
			expectedDay: "",
		},
		{
			name:        "WHERE with other conditions",
			sql:         "SELECT * FROM users WHERE name = 'John' AND id = 'user123' AND age > 25",
			expectedID:  "user123",
			expectedDay: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := querier.parseWhereClause(tt.sql)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedID, result.ID, "Expected ID %s, got %s", tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedDay, result.Day, "Expected Day %s, got %s", tt.expectedDay, result.Day)
		})
	}
}

func TestExtractTablesFromSQLRegex(t *testing.T) {
	// 测试备用的正则表达式方法
	querier := &Querier{}

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "Simple FROM clause",
			sql:      "SELECT * FROM users",
			expected: []string{"users"},
		},
		{
			name:     "FROM with JOIN",
			sql:      "SELECT * FROM users u JOIN departments d ON u.dept_id = d.id",
			expected: []string{"users", "departments"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := querier.extractTablesFromSQLRegex(tt.sql)
			assert.ElementsMatch(t, tt.expected, result, "Expected tables %v, got %v", tt.expected, result)
		})
	}
}

func TestParseWhereClauseRegex(t *testing.T) {
	// 测试备用的正则表达式方法
	querier := &Querier{}

	tests := []struct {
		name        string
		sql         string
		expectedID  string
		expectedDay string
	}{
		{
			name:        "Simple WHERE with id",
			sql:         "SELECT * FROM users WHERE id = 'user123'",
			expectedID:  "user123",
			expectedDay: "",
		},
		{
			name:        "WHERE with both id and day",
			sql:         "SELECT * FROM users WHERE id = 'user123' AND day = '2024-01-01'",
			expectedID:  "user123",
			expectedDay: "2024-01-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := querier.parseWhereClauseRegex(tt.sql)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedID, result.ID, "Expected ID %s, got %s", tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedDay, result.Day, "Expected Day %s, got %s", tt.expectedDay, result.Day)
		})
	}
}

func TestSQLParser_ParseQuery(t *testing.T) {
	parser := NewSQLParser()

	tests := []struct {
		name        string
		sql         string
		expected    *QueryInfo
		expectError bool
	}{
		{
			name: "标准 SELECT 查询",
			sql:  "SELECT * FROM users WHERE id = '123' AND day = '2024-01-01'",
			expected: &QueryInfo{
				QueryType: "SELECT",
				Tables:    []string{"users"},
				Conditions: map[string]interface{}{
					"id":  "123",
					"day": "2024-01-01",
				},
				IsSupported: true,
			},
		},
		{
			name: "带 JOIN 的查询",
			sql:  "SELECT u.name, p.title FROM users u JOIN posts p ON u.id = p.user_id WHERE u.id = '456'",
			expected: &QueryInfo{
				QueryType: "SELECT",
				Tables:    []string{"users", "posts"},
				Conditions: map[string]interface{}{
					"id": "456",
				},
				IsSupported: true,
			},
		},
		{
			name: "INSERT 语句",
			sql:  "INSERT INTO users (id, name) VALUES ('789', 'John')",
			expected: &QueryInfo{
				QueryType:   "INSERT",
				Tables:      []string{"users"},
				IsSupported: true,
			},
		},
		{
			name: "UPDATE 语句",
			sql:  "UPDATE users SET name = 'Jane' WHERE id = '123'",
			expected: &QueryInfo{
				QueryType: "UPDATE",
				Tables:    []string{"users"},
				Conditions: map[string]interface{}{
					"id": "123",
				},
				IsSupported: true,
			},
		},
		{
			name: "DELETE 语句",
			sql:  "DELETE FROM users WHERE id = '123'",
			expected: &QueryInfo{
				QueryType: "DELETE",
				Tables:    []string{"users"},
				Conditions: map[string]interface{}{
					"id": "123",
				},
				IsSupported: true,
			},
		},
		{
			name: "DuckDB PIVOT 语句（降级处理）",
			sql:  "PIVOT cities ON year USING sum(population)",
			expected: &QueryInfo{
				QueryType:   "PIVOT",
				Tables:      []string{"cities"},
				IsSupported: false,
			},
		},
		{
			name: "带 QUALIFY 的查询（降级处理）",
			sql:  "SELECT * FROM sales QUALIFY ROW_NUMBER() OVER (PARTITION BY region ORDER BY amount DESC) = 1",
			expected: &QueryInfo{
				QueryType:   "SELECT",
				Tables:      []string{"sales"},
				IsSupported: false,
			},
		},
		{
			name: "复杂 DuckDB 语法（降级处理）",
			sql:  "SELECT * FROM table1 WHERE id = '123' QUALIFY rank() OVER (ORDER BY value) <= 10",
			expected: &QueryInfo{
				QueryType: "SELECT",
				Tables:    []string{"table1"},
				Conditions: map[string]interface{}{
					"id": "123",
				},
				IsSupported: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ParseQuery(tt.sql)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expected.QueryType, result.QueryType)
			assert.Equal(t, tt.expected.IsSupported, result.IsSupported)
			assert.ElementsMatch(t, tt.expected.Tables, result.Tables)

			// 检查条件
			for key, expectedValue := range tt.expected.Conditions {
				actualValue, exists := result.Conditions[key]
				assert.True(t, exists, "条件 %s 应该存在", key)
				assert.Equal(t, expectedValue, actualValue, "条件 %s 的值不匹配", key)
			}

			assert.Equal(t, tt.sql, result.OriginalSQL)
		})
	}
}

func TestSQLParser_FallbackParsing(t *testing.T) {
	parser := NewSQLParser()

	tests := []struct {
		name     string
		sql      string
		expected *QueryInfo
	}{
		{
			name: "PIVOT 语句降级解析",
			sql:  "PIVOT sales_data ON quarter USING sum(revenue)",
			expected: &QueryInfo{
				QueryType:   "PIVOT",
				Tables:      []string{"sales_data"},
				IsSupported: false,
			},
		},
		{
			name: "复杂 JOIN 降级解析",
			sql:  "SELECT * FROM table1 t1 LEFT JOIN table2 t2 ON t1.id = t2.ref_id WHERE t1.status = 'active'",
			expected: &QueryInfo{
				QueryType:  "SELECT",
				Tables:     []string{"table1"},
				JoinTables: []string{"table2"},
				Conditions: map[string]interface{}{
					"status": "active",
				},
				IsSupported: false, // 如果 Vitess 解析失败会降级
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 直接调用降级解析器
			info := &QueryInfo{
				OriginalSQL: tt.sql,
				Conditions:  make(map[string]interface{}),
				Tables:      []string{},
				JoinTables:  []string{},
			}
			result, err := parser.fallbackParse(info)

			assert.NoError(t, err)
			assert.Equal(t, tt.expected.QueryType, result.QueryType)
			assert.Equal(t, tt.expected.IsSupported, result.IsSupported)
			assert.ElementsMatch(t, tt.expected.Tables, result.Tables)
			assert.ElementsMatch(t, tt.expected.JoinTables, result.JoinTables)

			// 检查条件
			for key, expectedValue := range tt.expected.Conditions {
				actualValue, exists := result.Conditions[key]
				assert.True(t, exists, "条件 %s 应该存在", key)
				assert.Equal(t, expectedValue, actualValue, "条件 %s 的值不匹配", key)
			}
		})
	}
}

func TestQueryInfo_ConvertToQueryFilter(t *testing.T) {
	tests := []struct {
		name     string
		info     *QueryInfo
		expected *QueryFilter
	}{
		{
			name: "转换包含 id 和 day 的查询信息",
			info: &QueryInfo{
				Conditions: map[string]interface{}{
					"id":  "123",
					"day": "2024-01-01",
				},
			},
			expected: &QueryFilter{
				ID:  "123",
				Day: "2024-01-01",
			},
		},
		{
			name: "转换只包含 id 的查询信息",
			info: &QueryInfo{
				Conditions: map[string]interface{}{
					"id": "456",
				},
			},
			expected: &QueryFilter{
				ID:  "456",
				Day: "",
			},
		},
		{
			name: "转换空条件的查询信息",
			info: &QueryInfo{
				Conditions: map[string]interface{}{},
			},
			expected: &QueryFilter{
				ID:  "",
				Day: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.info.ConvertToQueryFilter()
			assert.Equal(t, tt.expected.ID, result.ID)
			assert.Equal(t, tt.expected.Day, result.Day)
		})
	}
}

func TestSQLParser_ExtractTableNames(t *testing.T) {
	parser := NewSQLParser()

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "简单 FROM 子句",
			sql:      "SELECT * FROM users",
			expected: []string{"users"},
		},
		{
			name:     "多个表的 JOIN",
			sql:      "SELECT * FROM users u JOIN posts p ON u.id = p.user_id LEFT JOIN comments c ON p.id = c.post_id",
			expected: []string{"users", "posts", "comments"},
		},
		{
			name:     "PIVOT 语句",
			sql:      "PIVOT sales_data ON quarter USING sum(revenue)",
			expected: []string{"sales_data"},
		},
		{
			name:     "复杂查询",
			sql:      "SELECT * FROM table1 t1 INNER JOIN table2 t2 ON t1.id = t2.ref_id RIGHT JOIN table3 t3 ON t2.id = t3.ref_id",
			expected: []string{"table1", "table2", "table3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.extractTableNamesRegex(tt.sql)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestSQLParser_ExtractConditions(t *testing.T) {
	parser := NewSQLParser()

	tests := []struct {
		name     string
		sql      string
		expected map[string]interface{}
	}{
		{
			name: "单个等值条件",
			sql:  "SELECT * FROM users WHERE id = '123'",
			expected: map[string]interface{}{
				"id": "123",
			},
		},
		{
			name: "多个等值条件",
			sql:  "SELECT * FROM users WHERE id = '123' AND day = '2024-01-01' AND status = 'active'",
			expected: map[string]interface{}{
				"id":     "123",
				"day":    "2024-01-01",
				"status": "active",
			},
		},
		{
			name:     "没有 WHERE 子句",
			sql:      "SELECT * FROM users",
			expected: map[string]interface{}{},
		},
		{
			name:     "非等值条件（不支持）",
			sql:      "SELECT * FROM users WHERE age > 18",
			expected: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.extractSimpleConditionsRegex(tt.sql)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// 集成测试：测试与现有 Querier 的兼容性
func TestQuerier_Integration(t *testing.T) {
	// 这里可以添加集成测试，测试新的解析器与现有系统的兼容性
	// 由于需要 Redis 和其他依赖，这里只做基本的结构测试

	parser := NewSQLParser()
	assert.NotNil(t, parser)

	// 测试解析器可以正确处理各种 SQL 语句
	testQueries := []string{
		"SELECT * FROM users WHERE id = '123'",
		"INSERT INTO users (id, name) VALUES ('456', 'John')",
		"UPDATE users SET name = 'Jane' WHERE id = '123'",
		"DELETE FROM users WHERE id = '123'",
		"PIVOT sales ON quarter USING sum(revenue)", // DuckDB 特有语法
	}

	for _, query := range testQueries {
		info, err := parser.ParseQuery(query)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, query, info.OriginalSQL)

		// 测试转换为 QueryFilter
		filter := info.ConvertToQueryFilter()
		assert.NotNil(t, filter)
	}
}
