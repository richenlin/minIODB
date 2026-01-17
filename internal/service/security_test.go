package service

import (
	"testing"

	"minIODB/api/proto/miniodb/v1"
	"minIODB/config"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQueryData_SQLInjectionProtection(t *testing.T) {
	cfg := &config.Config{
		TableManagement: config.TableManagementConfig{
			DefaultTable: "users",
		},
	}

	svc := &MinIODBService{
		cfg: cfg,
	}

	tests := []struct {
		name        string
		sql         string
		wantErrCode codes.Code
	}{
		{
			name:        "有效查询 - 基本SELECT",
			sql:         "SELECT * FROM users",
			wantErrCode: codes.OK,
		},
		{
			name:        "有效查询 - 使用默认表",
			sql:         "SELECT * FROM table",
			wantErrCode: codes.OK,
		},
		{
			name:        "SQL注入 - 试图注入DROP TABLE",
			sql:         "SELECT * FROM table; DROP TABLE users; --",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "SQL注入 - 试图使用UNION注入",
			sql:         "SELECT * FROM table UNION SELECT * FROM secrets",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "SQL注入 - 试图使用注释",
			sql:         "SELECT * FROM table--",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "SQL注入 - 试图使用多语句",
			sql:         "SELECT * FROM table; SELECT * FROM secrets",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "SQL注入 - 试图使用子查询注入",
			sql:         "SELECT * FROM table WHERE id = (SELECT password FROM secrets)",
			wantErrCode: codes.OK,
		},
		{
			name:        "SQL注入 - 试图使用UNION注入",
			sql:         "SELECT * FROM table UNION SELECT * FROM secrets",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "拒绝 - 使用DELETE",
			sql:         "DELETE FROM table",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "拒绝 - 使用INSERT",
			sql:         "INSERT INTO table VALUES (1, 2)",
			wantErrCode: codes.InvalidArgument,
		},
		{
			name:        "拒绝 - 使用UPDATE",
			sql:         "UPDATE table SET name = 'test'",
			wantErrCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &miniodb.QueryDataRequest{
				Sql:   tt.sql,
				Limit: 0,
			}

			err := svc.validateQueryRequest(req)
			if tt.wantErrCode == codes.OK {
				if err != nil {
					t.Errorf("validateQueryRequest() unexpected error = %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateQueryRequest() expected error with code %v, got nil", tt.wantErrCode)
				} else {
					st, ok := status.FromError(err)
					if !ok {
						t.Errorf("validateQueryRequest() expected gRPC status error, got %T", err)
					} else if st.Code() != tt.wantErrCode {
						t.Errorf("validateQueryRequest() expected code %v, got %v", tt.wantErrCode, st.Code())
					}
				}
			}
		})
	}
}

func TestQueryData_LegacyTableReplacement(t *testing.T) {
	cfg := &config.Config{
		TableManagement: config.TableManagementConfig{
			DefaultTable: "users",
		},
	}

	svc := &MinIODBService{
		cfg: cfg,
	}

	tests := []struct {
		name         string
		sql          string
		wantContains string
	}{
		{
			name:         "替换大写的FROM table",
			sql:          "SELECT * FROM table",
			wantContains: "FROM \"users\"",
		},
		{
			name:         "替换小写的from table",
			sql:          "select * from table",
			wantContains: "from \"users\"",
		},
		{
			name:         "混合大小写 - 只替换小写",
			sql:          "SELECT * FROM table WHERE id > 10",
			wantContains: "FROM \"users\"",
		},
		{
			name:         "不应替换table作为其他表名的一部分",
			sql:          "SELECT * FROM table_data",
			wantContains: "table_data",
		},
		{
			name:         "处理table_data中的table",
			sql:          "SELECT * FROM table_data WHERE id > 10",
			wantContains: "table_data",
		},
		{
			name:         "不应替换引号包裹的table",
			sql:          "SELECT * FROM \"table\"",
			wantContains: "\"table\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &miniodb.QueryDataRequest{
				Sql:   tt.sql,
				Limit: 0,
			}

			resultSQL, err := svc.rewriteLegacyTable(req.Sql)
			if err != nil {
				t.Errorf("rewriteLegacyTable() unexpected error = %v", err)
				return
			}

			if !contains(resultSQL, tt.wantContains) {
				t.Errorf("rewriteLegacyTable() = %v, want to contain %q", resultSQL, tt.wantContains)
			}

			// 验证替换后的SQL仍然安全
			if err := svc.validateQueryRequest(&miniodb.QueryDataRequest{Sql: resultSQL}); err != nil {
				t.Errorf("rewritten SQL is not valid: %v", err)
			}
		})
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
