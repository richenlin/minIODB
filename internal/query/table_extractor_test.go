package query

import (
	"reflect"
	"testing"
)

func TestSimpleTableExtractor_ExtractTableNames(t *testing.T) {
	extractor := NewSimpleTableExtractor()

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "Simple SELECT with single table",
			sql:      "SELECT * FROM users",
			expected: []string{"users"},
		},
		{
			name:     "SELECT with table alias",
			sql:      "SELECT * FROM users u",
			expected: []string{"users"},
		},
		{
			name:     "SELECT with AS alias",
			sql:      "SELECT * FROM users AS u",
			expected: []string{"users"},
		},
		{
			name:     "INNER JOIN with two tables",
			sql:      "SELECT * FROM users u INNER JOIN orders o ON u.id = o.user_id",
			expected: []string{"users", "orders"},
		},
		{
			name:     "LEFT JOIN with aliases",
			sql:      "SELECT u.name, o.amount FROM users u LEFT JOIN orders o ON u.id = o.user_id",
			expected: []string{"users", "orders"},
		},
		{
			name:     "Multiple JOINs",
			sql:      "SELECT * FROM users u JOIN orders o ON u.id = o.user_id JOIN products p ON o.product_id = p.id",
			expected: []string{"users", "orders", "products"},
		},
		{
			name:     "RIGHT JOIN",
			sql:      "SELECT * FROM users u RIGHT JOIN orders o ON u.id = o.user_id",
			expected: []string{"users", "orders"},
		},
		{
			name:     "OUTER JOIN",
			sql:      "SELECT * FROM users u FULL OUTER JOIN orders o ON u.id = o.user_id",
			expected: []string{"users", "orders"},
		},
		{
			name:     "Case insensitive",
			sql:      "select * from USERS join ORDERS on users.id = orders.user_id",
			expected: []string{"users", "orders"},
		},
		{
			name:     "Complex query with subqueries",
			sql:      "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)",
			expected: []string{"users", "orders"},
		},
		{
			name:     "Empty query",
			sql:      "",
			expected: []string{},
		},
		{
			name:     "No FROM clause",
			sql:      "SELECT 1",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.ExtractTableNames(tt.sql)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ExtractTableNames() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestSimpleTableExtractor_ValidateTableNames(t *testing.T) {
	extractor := NewSimpleTableExtractor()

	tests := []struct {
		name     string
		tables   []string
		expected []string
	}{
		{
			name:     "Valid table names",
			tables:   []string{"users", "orders", "user_profiles"},
			expected: []string{"users", "orders", "user_profiles"},
		},
		{
			name:     "Mix of valid and invalid names",
			tables:   []string{"users", "123invalid", "valid_table", "table-name"},
			expected: []string{"users", "valid_table"},
		},
		{
			name:     "Names starting with underscore",
			tables:   []string{"_private_table", "__system_table"},
			expected: []string{"_private_table", "__system_table"},
		},
		{
			name:     "Invalid names",
			tables:   []string{"123table", "table@name", "table name"},
			expected: []string{},
		},
		{
			name:     "Empty input",
			tables:   []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.ValidateTableNames(tt.tables)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ValidateTableNames() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestSimpleTableExtractor_extractFromTables(t *testing.T) {
	extractor := NewSimpleTableExtractor()

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "Single FROM table",
			sql:      "select * from users",
			expected: []string{"users"},
		},
		{
			name:     "FROM with alias",
			sql:      "select * from users u",
			expected: []string{"users"},
		},
		{
			name:     "FROM with AS alias",
			sql:      "select * from users as u",
			expected: []string{"users"},
		},
		{
			name:     "Multiple FROM clauses in subquery",
			sql:      "select * from users where id in (select user_id from orders)",
			expected: []string{"users", "orders"},
		},
		{
			name:     "No FROM clause",
			sql:      "select 1",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.extractFromTables(tt.sql)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("extractFromTables() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestSimpleTableExtractor_extractJoinTables(t *testing.T) {
	extractor := NewSimpleTableExtractor()

	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "Simple JOIN",
			sql:      "select * from users join orders on users.id = orders.user_id",
			expected: []string{"orders"},
		},
		{
			name:     "LEFT JOIN",
			sql:      "select * from users left join orders on users.id = orders.user_id",
			expected: []string{"orders"},
		},
		{
			name:     "RIGHT JOIN with alias",
			sql:      "select * from users right join orders o on users.id = o.user_id",
			expected: []string{"orders"},
		},
		{
			name:     "INNER JOIN",
			sql:      "select * from users inner join orders on users.id = orders.user_id",
			expected: []string{"orders"},
		},
		{
			name:     "FULL OUTER JOIN",
			sql:      "select * from users full outer join orders on users.id = orders.user_id",
			expected: []string{"orders"},
		},
		{
			name:     "Multiple JOINs",
			sql:      "select * from users join orders on users.id = orders.user_id join products on orders.product_id = products.id",
			expected: []string{"orders", "products"},
		},
		{
			name:     "No JOIN",
			sql:      "select * from users",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.extractJoinTables(tt.sql)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("extractJoinTables() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// Benchmark tests for performance comparison
func BenchmarkSimpleTableExtractor_ExtractTableNames(b *testing.B) {
	extractor := NewSimpleTableExtractor()
	sql := "SELECT u.name, o.amount, p.title FROM users u LEFT JOIN orders o ON u.id = o.user_id INNER JOIN products p ON o.product_id = p.id WHERE u.active = true"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractor.ExtractTableNames(sql)
	}
}
