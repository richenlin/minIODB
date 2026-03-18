package service

import (
	"context"
	"testing"

	"minIODB/api/proto/miniodb/v1"
	"minIODB/config"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
)

func TestQueryData_SQLInjectionProtection(t *testing.T) {
	cfg := &config.Config{
		TableManagement: config.TableManagementConfig{
			DefaultTable: "users",
		},
	}

	logger := zap.NewNop()
	svc := &MinIODBService{
		cfg:    cfg,
		logger: logger,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &miniodb.QueryDataRequest{
				Sql:   tt.sql,
				Limit: 0,
			}

			_, err := svc.QueryData(context.Background(), req)

			if tt.wantErrCode != codes.OK {
				if err == nil {
					t.Errorf("QueryData() expected error, got nil")
				}
				if tt.wantErrCode != codes.OK && err != nil {
					t.Errorf("QueryData() unexpected success for %q, want error %s", tt.sql, err)
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
			wantContains: "FROM \"users\"",
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
			name:         "不应替换引号包裹的table",
			sql:          `SELECT * FROM "table"`,
			wantContains: `"table"`,
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

			if err := svc.validateQueryRequest(&miniodb.QueryDataRequest{Sql: resultSQL}); err != nil {
				t.Errorf("rewritten SQL is not valid: %v", err)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
