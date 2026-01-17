package query

import (
	"database/sql"
	"testing"

	"minIODB/config"
	"minIODB/internal/security"

	_ "github.com/marcboeker/go-duckdb"
	"go.uber.org/zap"
)

func TestExecuteQuery_SQLValidation(t *testing.T) {
	// 创建测试数据库
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	// 创建临时目录
	tempDir := t.TempDir()

	cfg := &config.Config{
		MinIO: config.MinioConfig{
			Bucket: "test-bucket",
		},
	}

	// 创建logger
	logger := zap.NewNop()

	// 创建Querier
	querier, err := NewQuerier(nil, nil, cfg, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create querier: %v", err)
	}
	defer querier.Close()

	querier.tempDir = tempDir

	tests := []struct {
		name        string
		sql         string
		wantErr     bool
		errContains string
	}{
		{
			name:    "空SQL",
			sql:     "",
			wantErr: true,
		},
		{
			name:        "DELETE语句",
			sql:         "DELETE FROM users WHERE id = 1",
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "DROP语句",
			sql:         "DROP TABLE users",
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "INSERT语句",
			sql:         "INSERT INTO users VALUES (1, 'test')",
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "UPDATE语句",
			sql:         "UPDATE users SET name = 'test'",
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "UNION注入",
			sql:         "SELECT * FROM users UNION SELECT * FROM admins",
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "注释注入",
			sql:         "SELECT * FROM users--",
			wantErr:     true,
			errContains: "not allowed",
		},
		{
			name:        "多语句攻击",
			sql:         "SELECT * FROM users; DELETE FROM users",
			wantErr:     true,
			errContains: "not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := querier.ExecuteQuery(tt.sql)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExecuteQuery() expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("ExecuteQuery() error = %v, want to contain %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ExecuteQuery() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestExecuteQueryWithOptimization_SQLValidation(t *testing.T) {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{}
	logger := zap.NewNop()

	querier, err := NewQuerier(nil, nil, cfg, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create querier: %v", err)
	}
	defer querier.Close()

	tests := []struct {
		name        string
		sql         string
		wantErr     bool
		errContains string
	}{
		{
			name:        "DELETE语句",
			sql:         "DELETE FROM users",
			wantErr:     true,
			errContains: "validation failed",
		},
		{
			name:        "注入攻击",
			sql:         "SELECT * FROM users; DROP TABLE users",
			wantErr:     true,
			errContains: "not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := querier.executeQueryWithOptimization(db, tt.sql)

			if tt.wantErr {
				if err == nil {
					t.Errorf("executeQueryWithOptimization() expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("executeQueryWithOptimization() error = %v, want to contain %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("executeQueryWithOptimization() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestCanUsePreparedStatement(t *testing.T) {
	cfg := &config.Config{}
	logger := zap.NewNop()

	querier, err := NewQuerier(nil, nil, cfg, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create querier: %v", err)
	}
	defer querier.Close()

	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"COUNT聚合", "SELECT COUNT(*) FROM users", true},
		{"SUM聚合", "SELECT SUM(amount) FROM orders", true},
		{"AVG聚合", "SELECT AVG(price) FROM products", true},
		{"GROUP BY", "SELECT status, COUNT(*) FROM users GROUP BY status", true},
		{"简单SELECT", "SELECT * FROM users", false},
		{"WHERE条件", "SELECT * FROM users WHERE id > 10", false},
		{"ORDER BY", "SELECT * FROM users ORDER BY name", false},
		{"JOIN", "SELECT * FROM users u JOIN orders o ON u.id = o.user_id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := querier.canUsePreparedStatement(tt.sql)
			if got != tt.want {
				t.Errorf("canUsePreparedStatement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLInjectionProtection(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		shouldBlock bool
	}{
		{"有效查询1", "SELECT * FROM users WHERE id = 1", false},
		{"有效查询2", "SELECT name, email FROM users ORDER BY created_at DESC LIMIT 10", false},
		{"有效查询3", "SELECT COUNT(*) FROM users GROUP BY status", false},

		{"注入: 单引号", "SELECT * FROM users WHERE id = '1' OR '1'='1'", false},
		{"注入: 分号", "SELECT * FROM users; DELETE FROM users", true},
		{"注入: 注释", "SELECT * FROM users--", true},
		{"注入: 多行注释", "SELECT * FROM users/* comment */", true},
		{"注入: DROP", "SELECT * FROM users; DROP TABLE users", true},
		{"注入: DELETE", "SELECT * FROM users; DELETE FROM users", true},
		{"注入: UNION", "SELECT * FROM users UNION SELECT * FROM admins", true},
		{"注入: INSERT", "SELECT * FROM users; INSERT INTO users VALUES (1, 'hacker')", true},
		{"注入: UPDATE", "SELECT * FROM users; UPDATE users SET password = 'hacked'", true},
		{"注入: EXEC", "SELECT * FROM users; EXEC xp_cmdshell 'dir'", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := security.DefaultSanitizer.ValidateSelectQuery(tt.sql)
			blocked := err != nil

			if blocked != tt.shouldBlock {
				t.Errorf("ValidateSelectQuery(%q) blocked=%v, want %v (error: %v)",
					tt.sql, blocked, tt.shouldBlock, err)
			}
		})
	}
}

func TestConcurrentQueryValidation(t *testing.T) {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{}
	logger := zap.NewNop()

	querier, err := NewQuerier(nil, nil, cfg, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create querier: %v", err)
	}
	defer querier.Close()

	sqlQueries := []string{
		"SELECT * FROM users; DROP TABLE users",
		"SELECT * FROM users--",
		"SELECT * FROM users UNION SELECT * FROM admins",
		"DELETE FROM users",
		"SELECT * FROM users",
	}

	// 并发执行查询验证
	done := make(chan bool, len(sqlQueries))
	for _, sql := range sqlQueries {
		go func(s string) {
			err := security.DefaultSanitizer.ValidateSelectQuery(s)
			blocked := err != nil
			if s == "SELECT * FROM users" {
				if blocked {
					t.Errorf("Valid query was blocked: %s", s)
				}
			} else {
				if !blocked {
					t.Errorf("Malicious query was not blocked: %s", s)
				}
			}
			done <- true
		}(sql)
	}

	// 等待所有goroutine完成
	for i := 0; i < len(sqlQueries); i++ {
		<-done
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstr(s[:len(s)-1], substr) || s[len(s)-len(substr):] == substr)
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
