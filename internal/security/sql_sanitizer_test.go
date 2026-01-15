package security

import (
	"strings"
	"testing"
)

func TestSQLSanitizer_ValidateTableName(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	tests := []struct {
		name      string
		tableName string
		wantErr   bool
	}{
		{"valid table name", "users", false},
		{"valid with underscore", "user_data", false},
		{"valid with numbers", "table123", false},
		{"empty name", "", true},
		{"starts with number", "123table", true},
		{"contains space", "user table", true},
		{"contains special chars", "user@table", true},
		{"SQL keyword", "select", true},
		{"too long", strings.Repeat("a", 129), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizer.ValidateTableName(tt.tableName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTableName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLSanitizer_ValidateID(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid ID", "user-123", false},
		{"valid with underscore", "user_123", false},
		{"alphanumeric", "abc123", false},
		{"empty ID", "", true},
		{"contains space", "user 123", true},
		{"contains special chars", "user@123", true},
		{"too long", strings.Repeat("a", 256), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizer.ValidateID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLSanitizer_BuildSafeDeleteSQL(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	tests := []struct {
		name      string
		tableName string
		id        string
		wantErr   bool
		wantSQL   string
	}{
		{
			name:      "valid delete",
			tableName: "users",
			id:        "user-123",
			wantErr:   false,
			wantSQL:   `DELETE FROM "users" WHERE id = 'user-123'`,
		},
		{
			name:      "invalid table name",
			tableName: "123invalid",
			id:        "user-123",
			wantErr:   true,
		},
		{
			name:      "invalid ID",
			tableName: "users",
			id:        "user@123",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, err := sanitizer.BuildSafeDeleteSQL(tt.tableName, tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildSafeDeleteSQL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && sql != tt.wantSQL {
				t.Errorf("BuildSafeDeleteSQL() = %v, want %v", sql, tt.wantSQL)
			}
		})
	}
}

func TestSQLSanitizer_ValidateSelectQuery(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"valid select", "SELECT * FROM users", false},
		{"valid with where", "SELECT name FROM users WHERE age > 18", false},
		{"empty query", "", true},
		{"not select", "INSERT INTO users VALUES (1)", true},
		{"contains drop", "SELECT * FROM users; DROP TABLE users", true},
		{"contains comment", "SELECT * FROM users -- comment", true},
		{"too long", "SELECT " + strings.Repeat("a", 10000), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizer.ValidateSelectQuery(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSelectQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLSanitizer_QuoteIdentifier(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{"simple identifier", "users", `"users"`},
		{"with quotes", `user"table`, `"user""table"`},
		{"empty", "", `""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizer.QuoteIdentifier(tt.identifier)
			if got != tt.want {
				t.Errorf("QuoteIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLSanitizer_QuoteLiteral(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	tests := []struct {
		name    string
		literal string
		want    string
	}{
		{"simple literal", "user123", `'user123'`},
		{"with quotes", `user'name`, `'user''name'`},
		{"empty", "", `''`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizer.QuoteLiteral(tt.literal)
			if got != tt.want {
				t.Errorf("QuoteLiteral() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSQLInjectionPrevention 测试SQL注入防护
func TestSQLInjectionPrevention(t *testing.T) {
	sanitizer := NewSQLSanitizer()

	maliciousInputs := []struct {
		name      string
		tableName string
		id        string
	}{
		{"classic injection", "users", "'; DROP TABLE users; --"},
		{"union attack", "users", "' UNION SELECT * FROM passwords --"},
		{"comment attack", "users", "' /**/OR/**/1=1/**/--"},
		{"encoded attack", "users", "' OR 1=1 #"},
		{"table name injection", "users'; DROP TABLE users; --", "user123"},
	}

	for _, tt := range maliciousInputs {
		t.Run(tt.name, func(t *testing.T) {
			// 所有恶意输入都应该被拒绝
			_, err := sanitizer.BuildSafeDeleteSQL(tt.tableName, tt.id)
			if err == nil {
				t.Errorf("Expected error for malicious input: table=%s, id=%s", tt.tableName, tt.id)
			}
		})
	}
}
