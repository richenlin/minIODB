package query

import "testing"

func TestNormalizeQuery_PreservesStringLiterals(t *testing.T) {
	qc := &QueryCache{}
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "SELECT * FROM users WHERE name = 'John'",
			expected: "select * from users where name = 'John'",
		},
		{
			input:    "SELECT * FROM users WHERE name = 'john'",
			expected: "select * from users where name = 'john'",
		},
		{
			input:    "SELECT * FROM T WHERE a = 'Hello World' AND b = 1",
			expected: "select * from t where a = 'Hello World' and b = 1",
		},
	}
	for _, tt := range tests {
		result := qc.normalizeQuery(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeQuery(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestNormalizeQuery_DoubleQuoteEscape(t *testing.T) {
	qc := &QueryCache{}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple query",
			input:    "SELECT * FROM users WHERE name = 'John'",
			expected: "select * from users where name = 'John'",
		},
		{
			name:     "SQL escaped single quote (P3-3)",
			input:    "SELECT * FROM users WHERE name = 'O''Brien'",
			expected: "select * from users where name = 'O''Brien'",
		},
		{
			name:     "multiple escaped quotes",
			input:    "SELECT * FROM users WHERE name = 'it''s a test'",
			expected: "select * from users where name = 'it''s a test'",
		},
		{
			name:     "consecutive escaped quotes",
			input:    "SELECT * FROM t WHERE msg = 'Hello''''World'",
			expected: "select * from t where msg = 'Hello''''World'",
		},
		{
			name:     "no quotes in query",
			input:    "SELECT * FROM USERS",
			expected: "select * from users",
		},
		{
			name:     "backslash escape",
			input:    "SELECT * FROM users WHERE path = 'C:\\Data'",
			expected: "select * from users where path = 'C:\\Data'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := qc.normalizeQuery(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeQuery(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
