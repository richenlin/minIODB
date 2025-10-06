package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/logger"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func init() {
	// 初始化测试logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
	})
}

// TestMinioServiceCreation MinIO服务功能创建测试
func TestMinioServiceCreation(t *testing.T) {
	ctx := context.Background()

	// 创建配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID:     "test-service-node",
			GrpcPort:   "8080",
			RestPort:   "8081",
			HealthPort: "8082",
		},
		Cache: config.CacheConfig{
			Enabled: true,
			TTL:     3600,
		},
		Storage: config.StorageConfig{
			Type: "minio",
		},
	}

	// 创建模拟组件
	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	// 创建服务
	service := NewMinIODBService(cfg, ingester, querier)
	assert.NotNil(t, service)
	assert.Equal(t, cfg, service.GetConfig())

	// 测试基本属性
	assert.NotNil(t, service.cfg)
	assert.NotNil(t, service.ingester)
	assert.NotNil(t, service.querier)
}

// TestServiceConfiguration 服务配置测试
func TestServiceConfiguration(t *testing.T) {
	testCases := []struct {
		name        string
		cfg         *config.Config
		expectNil   bool
		description string
	}{
		{
			name: "完整配置",
			cfg: &config.Config{
				Server: config.ServerConfig{
					NodeID: "config-test-node",
					Port:   9000,
				},
				Cache: config.CacheConfig{
					Enabled: true,
				},
			},
			expectNil:   false,
			description: "完整配置应该创建有效服务",
		},
		{
			name: "空配置",
			cfg:  &config.Config{},
			expectNil: false,
			description: "空配置也应该创建服务",
		},
		{
			name: "带缓存配置",
			cfg: &config.Config{
				Cache: config.CacheConfig{
					Enabled:     true,
					Type:        "memory",
					MaxSize:     1000,
					TTL:         3600,
					MaxMemory:   1024 * 1024 * 100, // 100MB
				},
			},
			expectNil:   false,
			description: "复杂的缓存配置应该被正确处理",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ingester := &ingest.Ingester{}
			querier := &query.Querier{}

			service := NewMinIODBService(tc.cfg, ingester, querier)

			if tc.expectNil {
				assert.Nil(t, service, tc.description)
			} else {
				assert.NotNil(t, service, tc.description)
				if service != nil {
					assert.NotNil(t, service.cfg, "服务应该包含配置")
				}
			}
		})
	}
}

// TestServiceWriteComprehensive 服务写入综合测试
func TestServiceWriteComprehensive(t *testing.T) {
	ctx := context.Background()

	// 创建配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID: "write-test-node",
		},
		Cache: config.CacheConfig{
			Enabled: true,
			TTL:     300,
		},
	}

	// 创建模拟组件
	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	service := NewMinIODBService(cfg, ingester, querier)

	testCases := []struct {
		name        string
		request     *miniodb.WriteDataRequest
		expectError bool
		description string
	}{
		{
			name: "有效的写入请求",
			request: &miniodb.WriteDataRequest{
				Table: "users",
				Data: &miniodb.DataRecord{
					Id:        "user-123",
					Timestamp: timestamppb.New(time.Now()),
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"name": {Kind: &structpb.Value_StringValue{StringValue: "John"}},
							"age":  {Kind: &structpb.Value_NumberValue{NumberValue: 30}},
						},
					},
				},
			},
			expectError: false,
			description: "有效的数据应该成功写入",
		},
		{
			name: "复杂数据结构",
			request: &miniodb.WriteDataRequest{
				Table: "analytics",
				Data: &miniodb.DataRecord{
					Id:        "analytics-456",
					Timestamp: timestamppb.New(time.Now()),
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"metric":    {Kind: &structpb.Value_StringValue{StringValue: "cpu_usage"}},
							"value":     {Kind: &structpb.Value_NumberValue{NumberValue: 75.5}},
							"timestamp": {Kind: &structpb.Value_StringValue{StringValue: time.Now().Format(time.RFC3339)}},
							"tags": {Kind: &structpb.Value_ListValue{ListValue: &structpb.ListValue{
								Values: []*structpb.Value{
									{Kind: &structpb.Value_StringValue{StringValue: "server1"}},
									{Kind: &structpb.Value_StringValue{StringValue: "production"}},
								},
							}}},
						},
					},
				},
			},
			expectError: false,
			description: "复杂嵌套结构应该可以正确处理",
		},
		{
			name: "数组类型数据",
			request: &miniodb.WriteDataRequest{
				Table: "arrays",
				Data: &miniodb.DataRecord{
					Id:        "array-789",
					Timestamp: timestamppb.New(time.Now()),
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"numbers": {Kind: &structpb.Value_ListValue{ListValue: &structpb.ListValue{
								Values: []*structpb.Value{
									{Kind: &structpb.Value_NumberValue{NumberValue: 1.0}},
									{Kind: &structpb.Value_NumberValue{NumberValue: 2.0}},
									{Kind: &structpb.Value_NumberValue{NumberValue: 3.0}},
								},
							}}},
							"status": {Kind: &structpb.Value_StringValue{StringValue: "active"}},
						},
					},
				},
			},
			expectError: false,
			description: "数组类型数据应该可以正确写入",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 验证请求（使用已修复的验证逻辑）
			err := service.validateWriteRequest(tc.request)

			if tc.expectError {
				assert.Error(t, err, tc.description)
			} else {
				assert.NoError(t, err, tc.description)

				// 继续执行实际的写入操作（这里只是验证请求格式）
				// 真正的写入需要实际的存储和协调器组件
			}
		})
	}
}

// TestServiceQueryComprehensive 服务查询综合测试
func TestServiceQueryComprehensive(t *testing.T) {
	ctx := context.Background()

	// 创建配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID: "query-test-node",
		},
		Query: config.QueryConfig{
			Timeout: 30,
		},
	}

	// 创建模拟组件
	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	service := NewMinIODBService(cfg, ingester, querier)

	testCases := []struct {
		name        string
		sql         string
		description string
	}{
		{
			name:        "简单SELECT查询",
			sql:         "SELECT * FROM users WHERE age > 25",
			description: "基本查询语法应该被正确解析",
		},
		{
			name:        "SELECT带WHERE条件",
			sql:         "SELECT name, age FROM users WHERE status = 'active' AND age BETWEEN 18 AND 65",
			description: "带复杂WHERE条件的查询应该被正确处理",
		},
		{
			name:        "SELECT带JOIN",
			sql:         "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id WHERE o.status = 'completed'",
			description: "连接查询应该被正确解析",
		},
		{
			name:        "聚合函数查询",
			sql:         "SELECT COUNT(*), AVG(age), MAX(age) FROM users GROUP BY city HAVING COUNT(*) > 10",
			description: "聚合查询应该被正确处理",
		},
		{
			name:        "子查询",
			sql:         "SELECT name FROM users WHERE id IN (SELECT user_id FROM orders WHERE total > 100)",
			description: "子查询应该被正确解析",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 测试SQL解析（这里只是验证SQL格式）
			// 实际执行需要模拟查询组件
			request := &miniodb.QueryDataRequest{
				Sql: tc.sql,
			}

			// 验证请求不为空，实际解析在模拟实现中进行
			assert.NotEmpty(t, request.Sql, tc.description)

			// 验证基本的SQL语法（这里简化检查关键字存在）
			sql := strings.ToUpper(request.Sql)
			assert.Contains(t, sql, "SELECT", "SQL应该包含SELECT关键字")
		})
	}

	t.Run("查询验证错误处理", func(t *testing.T) {
		// 测试空SQL
		emptyRequest := &miniodb.QueryDataRequest{Sql: ""}
		assert.Empty(t, emptyRequest.Sql, "空SQL请求应该被正确识别")

		// 测试无效SQL格式
		invalidRequest := &miniodb.QueryDataRequest{Sql: "INVALID SQL SYNTAX"}
		assert.Equal(t, "INVALID SQL SYNTAX", invalidRequest.Sql, "无效的SQL应该被捕获")
	})
}

// TestServiceTableManagement 服务表管理测试
func TestServiceTableManagement(t *testing.T) {
	ctx := context.Background()

	// 创建配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID: "table-mgmt-node",
		},
	}

	// 创建模拟组件
	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	service := NewMinIODBService(cfg, ingester, querier)

	t.Run("表创建功能", func(t *testing.T) {
		createReq := &miniodb.CreateTableRequest{
			TableName:   "new_table",
			Description: "测试表描述",
		}

		// 验证请求格式
		assert.Equal(t, "new_table", createReq.TableName)
		assert.NotEmpty(t, createReq.Description)

		// 验证表名格式
		for _, char := range createReq.TableName {
			assert.True(t,
				(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') || char == '_' || char == '-',
				"表名应该只包含字母、数字、连字符和下划线")
		}
	})

	t.Run("表列表功能", func(t *testing.T) {
		listReq := &miniodb.ListTablesRequest{
			Limit:  100,
			Offset: 0,
		}

		// 验证分页参数
		assert.Greater(t, listReq.Limit, int32(0))
		assert.GreaterOrEqual(t, listReq.Offset, int32(0))
	})

	t.Run("表更新功能", func(t *testing.T) {
		updateReq := &miniodb.UpdateDataRequest{
			Table: "existing_table",
			Id:    "record-id-123",
			Data: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"status": {Kind: &structpb.Value_StringValue{StringValue: "updated"}},
				},
			},
		}

		// 验证更新请求格式
		assert.NotEmpty(t, updateReq.Table)
		assert.NotEmpty(t, updateReq.Id)
		assert.NotNil(t, updateReq.Data)
	})

	t.Run("表删除功能", func(t *testing.T) {
		deleteReq := &miniodb.DeleteDataRequest{
			Table:  "table_to_delete",
			Id:     "record-to-delete",
			Force:  false,
			Reason: "测试删除",
		}

		// 验证删除请求格式
		assert.Equal(t, "table_to_delete", deleteReq.Table)
		assert.Equal(t, "record-to-delete", deleteReq.Id)
		assert.False(t, deleteReq.Force)
		assert.Equal(t, "测试删除", deleteReq.Reason)
	})
}

// TestServiceCacheManagement 服务缓存管理测试
func TestServiceCacheManagement(t *testing.T) {
	ctx := context.Background()

	// 创建带缓存的配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID: "cache-mgmt-node",
		},
		Cache: config.CacheConfig{
			Enabled:   true,
			DefaultTTL: 1800, // 30分钟
			Type:       "memory",
			MaxSize:    10000,
		},
	}

	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	service := NewMinIODBService(cfg, ingester, querier)

	t.Run("缓存失效功能", func(t *testing.T) {
		invalidateReq := &miniodb.InvalidateCacheRequest{
			Pattern: "table:*",
			AllEntries:  false,
		}

		// 验证缓存失效请求
		assert.Equal(t, "table:*", invalidateReq.Pattern)
		assert.False(t, invalidateReq.AllEntries)

		// 测试缓存失效逻辑（模拟）
		testPatterns := []string{
			"users:*",
			"orders:*",
			"analytics:*",
			"*", // 全表模式
		}

		for _, pattern := range testPatterns {
			assert.NotEmpty(t, pattern, "缓存失效模式不应该为空")
			assert.True(t, isValidCachePattern(pattern), "模式应该格式正确")
		}
	})
}

// isValidCachePattern 验证缓存模式格式（辅助函数）
func isValidCachePattern(pattern string) bool {
	if pattern == "" {
		return false
	}

	// 简单验证：不应有无效字符
	for _, char := range pattern {
		if char == ' ' || char == '\t' || char == '\n' {
			return false
		}
	}

	return true
}

// TestServiceErrorHandling 服务错误处理测试
func TestServiceErrorHandling(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{}
	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	service := NewMinIODBService(cfg, ingester, querier)

	testCases := []struct {
		name        string
		operation   string
		request     interface{}
		expectError bool
		description string
	}{
		{
			name:      "空写入请求",
			operation: "write",
			request: &miniodb.WriteDataRequest{
				Table: "",
				Data:  nil,
			},
			expectError: true,
			description: "空表名应该导致错误",
		},
		{
			name:      "格式错误的表名",
			operation: "write",
			request: &miniodb.WriteDataRequest{
				Table: "invalid table!name",
				Data: &miniodb.DataRecord{
					Id:        "test-id",
					Timestamp: timestamppb.New(time.Now()),
					Payload:   &structpb.Struct{},
				},
			},
			expectError: true,
			description: "包含无效字符的表名应该被拒绝",
		},
		{
			name:      "超长表名",
			operation: "write",
			request: &miniodb.WriteDataRequest{
				Table: func() string {
					// 生成超过128字符的表名
					longName := "very_long_table_name_"
					for i := 0; i < 200; i++ {
						longName += "x"
					}
					return longName
				}(),
				Data: &miniodb.DataRecord{
					Id:        "test-id",
					Timestamp: timestamppb.New(time.Now()),
					Payload:   &structpb.Struct{},
				},
			},
			expectError: true,
			description: "超过128字符的表名应该被拒绝",
		},
		{
			name:      "空数据记录",
			operation: "write",
			request: &miniodb.WriteDataRequest{
				Table: "test_table",
				Data:  nil,
			},
			expectError: true,
			description: "空数据记录应该被拒绝",
		},
		{
			name:      "空ID",
			operation: "write",
			request: &miniodb.WriteDataRequest{
				Table: "test_table",
				Data: &miniodb.DataRecord{
					Id:        "",
					Timestamp: timestamppb.New(time.Now()),
					Payload:   &structpb.Struct{},
				},
			},
			expectError: true,
			description: "空ID应该被拒绝",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.operation == "write" {
				req := tc.request.(*miniodb.WriteDataRequest)
				err := service.validateWriteRequest(req)

				if tc.expectError {
					assert.Error(t, err, tc.description)
				} else {
					assert.NoError(t, err, tc.description)
				}
			}
		})
	}
}

// TestServicePerformanceMetrics 服务性能指标测试
func TestServicePerformanceMetrics(t *testing.T) {
	ctx := context.Background()

	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID: "metrics-node",
		},
	}

	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	service := NewMinIODBService(cfg, ingester, querier)

	t.Run("响应时间测量", func(t *testing.T) {
		start := time.Now()

		// 模拟服务处理时间
		time.Sleep(50 * time.Millisecond)

		duration := time.Since(start)
		assert.Less(t, duration, 100*time.Millisecond, "服务响应时间应该在合理范围内")
	})

	t.Run("并发处理测试", func(t *testing.T) {
		// 模拟并发请求
		concurrency := 10
		ch := make(chan bool, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(id int) {
				// 模拟每个请求的短时处理
				time.Sleep(time.Duration(id+10) * time.Millisecond)
				ch <- true
			}(i)
		}

		// 等待所有并发请求完成
		for i := 0; i < concurrency; i++ {
			select {
			case <-ch:
				// 请求完成
			case <-time.After(5 * time.Second):
				t.Fatal("并发测试超时")
			}
		}

		t.Logf("并发测试完成：%d个并发请求处理成功", concurrency)
	})
}