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
