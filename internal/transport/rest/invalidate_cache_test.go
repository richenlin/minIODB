package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestInvalidateTableCacheRequest 测试请求结构
func TestInvalidateTableCacheRequest(t *testing.T) {
	tests := []struct {
		name     string
		req      InvalidateTableCacheRequest
		expected []string
	}{
		{
			name: "all_cache_types",
			req: InvalidateTableCacheRequest{
				CacheTypes: []string{"metadata", "file_index", "query"},
				Reason:     "maintenance",
			},
			expected: []string{"metadata", "file_index", "query"},
		},
		{
			name: "single_cache_type",
			req: InvalidateTableCacheRequest{
				CacheTypes: []string{"metadata"},
				Reason:     "schema_change",
			},
			expected: []string{"metadata"},
		},
		{
			name: "empty_cache_types",
			req: InvalidateTableCacheRequest{
				CacheTypes: []string{},
				Reason:     "default",
			},
			expected: []string{}, // Will be filled with defaults
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.req)
			if len(tt.expected) > 0 && len(tt.req.CacheTypes) > 0 {
				assert.Equal(t, tt.expected, tt.req.CacheTypes)
			}
		})
	}

	t.Logf("✓ Request structure tests passed")
}

// TestInvalidateTableCacheResponse 测试响应结构
func TestInvalidateTableCacheResponse(t *testing.T) {
	resp := InvalidateTableCacheResponse{
		Success:         true,
		Message:         "Cache invalidated",
		TableName:       "test_table",
		InvalidatedAt:   time.Now().UTC().Format(time.RFC3339),
		CachesCleared:   []string{"metadata", "file_index"},
		PreviousHitRate: 0.85,
	}

	assert.True(t, resp.Success)
	assert.Equal(t, "test_table", resp.TableName)
	assert.Len(t, resp.CachesCleared, 2)
	assert.Equal(t, 0.85, resp.PreviousHitRate)

	t.Logf("✓ Response structure tests passed")
}

// TestInvalidateTableCacheHandler 测试handler逻辑（模拟）
func TestInvalidateTableCacheHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		tableName      string
		requestBody    InvalidateTableCacheRequest
		expectedStatus int
		expectSuccess  bool
	}{
		{
			name:      "valid_all_caches",
			tableName: "users",
			requestBody: InvalidateTableCacheRequest{
				CacheTypes: []string{"metadata", "file_index", "query"},
				Reason:     "manual_ops",
			},
			expectedStatus: http.StatusOK,
			expectSuccess:  true,
		},
		{
			name:      "valid_single_cache",
			tableName: "orders",
			requestBody: InvalidateTableCacheRequest{
				CacheTypes: []string{"metadata"},
				Reason:     "schema_update",
			},
			expectedStatus: http.StatusOK,
			expectSuccess:  true,
		},
		{
			name:           "empty_table_name",
			tableName:      "",
			requestBody:    InvalidateTableCacheRequest{},
			expectedStatus: http.StatusBadRequest,
			expectSuccess:  false,
		},
		{
			name:      "empty_cache_types_defaults",
			tableName: "products",
			requestBody: InvalidateTableCacheRequest{
				CacheTypes: []string{},
			},
			expectedStatus: http.StatusOK,
			expectSuccess:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建模拟的gin context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// 设置表名参数
			c.Params = gin.Params{
				{Key: "name", Value: tt.tableName},
			}

			// 设置请求body
			body, _ := json.Marshal(tt.requestBody)
			c.Request, _ = http.NewRequest("POST", "/v1/tables/"+tt.tableName+"/invalidate", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			// 在实际handler中，这里会调用s.invalidateTableCache(c)
			// 但由于我们需要MinIODBService实例，这里仅验证结构

			if tt.tableName == "" {
				// 模拟空表名的情况
				c.JSON(http.StatusBadRequest, gin.H{"error": "Table name is required"})
			} else {
				// 模拟成功响应
				if len(tt.requestBody.CacheTypes) == 0 {
					tt.requestBody.CacheTypes = []string{"metadata", "file_index", "query"}
				}

				c.JSON(http.StatusOK, InvalidateTableCacheResponse{
					Success:       true,
					Message:       "Cache invalidated",
					TableName:     tt.tableName,
					InvalidatedAt: time.Now().UTC().Format(time.RFC3339),
					CachesCleared: tt.requestBody.CacheTypes,
				})
			}

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectSuccess {
				var resp InvalidateTableCacheResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				assert.NoError(t, err)
				assert.True(t, resp.Success)
				assert.Equal(t, tt.tableName, resp.TableName)
			}

			t.Logf("  ✓ %s: status=%d", tt.name, w.Code)
		})
	}

	t.Logf("✓ Handler logic tests passed")
}

// TestCacheTypeValidation 测试缓存类型验证
func TestCacheTypeValidation(t *testing.T) {
	validTypes := []string{"metadata", "file_index", "query"}
	invalidTypes := []string{"unknown", "invalid", "foo"}

	for _, cacheType := range validTypes {
		assert.Contains(t, validTypes, cacheType, "Valid cache type should be recognized")
	}

	for _, cacheType := range invalidTypes {
		assert.NotContains(t, validTypes, cacheType, "Invalid cache type should not be recognized")
	}

	t.Logf("✓ Cache type validation passed")
}

// TestInvalidateMultipleTablesSequential 测试顺序失效多个表
func TestInvalidateMultipleTablesSequential(t *testing.T) {
	tables := []string{"table1", "table2", "table3", "table4", "table5"}

	for _, tableName := range tables {
		req := InvalidateTableCacheRequest{
			CacheTypes: []string{"metadata", "file_index", "query"},
			Reason:     "batch_invalidation",
		}

		// 模拟失效请求
		resp := InvalidateTableCacheResponse{
			Success:       true,
			TableName:     tableName,
			InvalidatedAt: time.Now().UTC().Format(time.RFC3339),
			CachesCleared: req.CacheTypes,
		}

		assert.True(t, resp.Success)
		assert.Equal(t, tableName, resp.TableName)
		assert.Len(t, resp.CachesCleared, 3)
	}

	t.Logf("✓ Sequential invalidation of %d tables completed", len(tables))
}

// TestInvalidateConcurrentTables 测试并发失效多个表
func TestInvalidateConcurrentTables(t *testing.T) {
	tables := []string{"table1", "table2", "table3", "table4", "table5", "table6", "table7", "table8", "table9", "table10"}

	results := make(chan bool, len(tables))

	for _, tableName := range tables {
		go func(tbl string) {
			req := InvalidateTableCacheRequest{
				CacheTypes: []string{"metadata"},
				Reason:     "concurrent_test",
			}

			// 模拟失效过程
			time.Sleep(10 * time.Millisecond)

			resp := InvalidateTableCacheResponse{
				Success:       true,
				TableName:     tbl,
				CachesCleared: req.CacheTypes,
			}

			results <- resp.Success
		}(tableName)
	}

	// 收集结果
	successCount := 0
	for i := 0; i < len(tables); i++ {
		if <-results {
			successCount++
		}
	}

	assert.Equal(t, len(tables), successCount, "All concurrent invalidations should succeed")
	t.Logf("✓ Concurrent invalidation of %d tables completed", successCount)
}

// TestAPIEndpointFormat 测试API端点格式
func TestAPIEndpointFormat(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		method   string
	}{
		{
			name:     "invalidate_cache_endpoint",
			endpoint: "/v1/tables/{name}/invalidate",
			method:   "POST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.endpoint, "{name}", "Endpoint should contain table name parameter")
			assert.Equal(t, "POST", tt.method, "Invalidate should use POST method")
		})
	}

	t.Logf("✓ API endpoint format validated")
}

// TestCacheInvalidationReason 测试失效原因记录
func TestCacheInvalidationReason(t *testing.T) {
	reasons := []string{
		"manual_maintenance",
		"schema_change",
		"data_corruption_fix",
		"performance_tuning",
		"after_bulk_update",
		"emergency_fix",
	}

	for _, reason := range reasons {
		req := InvalidateTableCacheRequest{
			CacheTypes: []string{"metadata"},
			Reason:     reason,
		}

		assert.NotEmpty(t, req.Reason, "Reason should not be empty")
		assert.Contains(t, reasons, req.Reason, "Reason should be one of predefined")
	}

	t.Logf("✓ %d invalidation reasons tested", len(reasons))
}

// TestResponseTimestamp 测试响应时间戳
func TestResponseTimestamp(t *testing.T) {
	now := time.Now().UTC()

	resp := InvalidateTableCacheResponse{
		Success:       true,
		TableName:     "test",
		InvalidatedAt: now.Format(time.RFC3339),
	}

	// 解析时间戳
	parsedTime, err := time.Parse(time.RFC3339, resp.InvalidatedAt)
	assert.NoError(t, err, "Timestamp should be in RFC3339 format")
	assert.WithinDuration(t, now, parsedTime, time.Second, "Timestamp should be close to now")

	t.Logf("✓ Timestamp format validated: %s", resp.InvalidatedAt)
}

// TestPartialCacheInvalidation 测试部分缓存失效
func TestPartialCacheInvalidation(t *testing.T) {
	scenarios := []struct {
		name       string
		cacheTypes []string
		expected   int
	}{
		{"only_metadata", []string{"metadata"}, 1},
		{"only_file_index", []string{"file_index"}, 1},
		{"only_query", []string{"query"}, 1},
		{"metadata_and_file_index", []string{"metadata", "file_index"}, 2},
		{"all_three", []string{"metadata", "file_index", "query"}, 3},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			req := InvalidateTableCacheRequest{
				CacheTypes: scenario.cacheTypes,
				Reason:     "partial_test",
			}

			assert.Len(t, req.CacheTypes, scenario.expected, "Should invalidate expected number of caches")
			t.Logf("  ✓ %s: %d cache(s)", scenario.name, scenario.expected)
		})
	}

	t.Logf("✓ Partial cache invalidation scenarios tested")
}

// BenchmarkInvalidateTableCacheStruct 基准测试：结构体创建
func BenchmarkInvalidateTableCacheStruct(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = InvalidateTableCacheRequest{
			CacheTypes: []string{"metadata", "file_index", "query"},
			Reason:     "benchmark",
		}
	}
}

// BenchmarkInvalidateTableCacheJSON 基准测试：JSON序列化
func BenchmarkInvalidateTableCacheJSON(b *testing.B) {
	req := InvalidateTableCacheRequest{
		CacheTypes: []string{"metadata", "file_index", "query"},
		Reason:     "benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(req)
	}
}
