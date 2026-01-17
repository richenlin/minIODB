package service

import (
	"context"
	"testing"
	"time"

	"minIODB/api/proto/miniodb/v1"
	"minIODB/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestIDGeneration_Integration 集成测试ID生成功能
func TestIDGeneration_Integration(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			NodeID: "test-node-1",
		},
		TableManagement: config.TableManagementConfig{
			DefaultTable: "default",
		},
		Tables: config.TablesConfig{
			DefaultConfig: config.TableConfig{
				AutoGenerateID: false, // 默认不自动生成
				IDStrategy:     "user_provided",
				IDValidation: config.IDValidationRules{
					MaxLength: 255,
					Pattern:   "^[a-zA-Z0-9_-]+$",
				},
			},
		},
	}

	// 创建服务
	service, err := NewMinIODBService(cfg, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, service)

	ctx := context.Background()

	t.Run("User Provided ID - Success", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"name": "Test User",
			"age":  30,
		})

		req := &miniodb.WriteDataRequest{
			Table: "default",
			Data: &miniodb.DataRecord{
				Id:        "user-123",
				Timestamp: timestamppb.New(time.Now()),
				Payload:   payload,
			},
		}

		// 验证应该通过
		err := service.validateWriteRequest(req)
		assert.NoError(t, err)
	})

	t.Run("User Provided ID - Missing ID Required", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"name": "Test User",
		})

		req := &miniodb.WriteDataRequest{
			Table: "default",
			Data: &miniodb.DataRecord{
				Id:        "", // 空ID
				Timestamp: timestamppb.New(time.Now()),
				Payload:   payload,
			},
		}

		// 应该报错（默认配置require ID）
		err := service.validateWriteRequest(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ID is required")
	})

	t.Run("UUID Generation", func(t *testing.T) {
		// 修改表配置为UUID生成
		tableConfig := config.TableConfig{
			AutoGenerateID: true,
			IDStrategy:     "uuid",
			IDValidation: config.IDValidationRules{
				MaxLength: 255,
				Pattern:   "^[a-zA-Z0-9_-]+$",
			},
		}

		// 生成ID
		id, err := service.GenerateID(ctx, "test_table", tableConfig)
		assert.NoError(t, err)
		assert.NotEmpty(t, id)
		assert.Equal(t, 36, len(id)) // UUID格式长度
	})

	t.Run("Snowflake Generation", func(t *testing.T) {
		tableConfig := config.TableConfig{
			AutoGenerateID: true,
			IDStrategy:     "snowflake",
			IDPrefix:       "order",
			IDValidation: config.IDValidationRules{
				MaxLength: 255,
				Pattern:   "^[a-zA-Z0-9_-]+$",
			},
		}

		// 生成多个ID确保唯一性
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id, err := service.GenerateID(ctx, "test_table", tableConfig)
			assert.NoError(t, err)
			assert.NotEmpty(t, id)
			assert.Contains(t, id, "order-") // 包含前缀
			assert.False(t, ids[id], "Duplicate ID generated: %s", id)
			ids[id] = true
		}
	})

	t.Run("Custom Generation", func(t *testing.T) {
		tableConfig := config.TableConfig{
			AutoGenerateID: true,
			IDStrategy:     "custom",
			IDPrefix:       "log",
			IDValidation: config.IDValidationRules{
				MaxLength: 255,
				Pattern:   "^[a-zA-Z0-9_-]+$",
			},
		}

		id, err := service.GenerateID(ctx, "test_table", tableConfig)
		assert.NoError(t, err)
		assert.NotEmpty(t, id)
		assert.Contains(t, id, "log-") // 包含前缀
		assert.Contains(t, id, "-")    // 包含分隔符
	})

	t.Run("Validation - Pattern Match", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"data": "test",
		})

		// 有效ID
		req := &miniodb.WriteDataRequest{
			Table: "default",
			Data: &miniodb.DataRecord{
				Id:        "valid-id_123",
				Timestamp: timestamppb.New(time.Now()),
				Payload:   payload,
			},
		}
		err := service.validateWriteRequest(req)
		assert.NoError(t, err)

		// 无效ID（包含空格）
		req.Data.Id = "invalid id with spaces"
		err = service.validateWriteRequest(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid characters")
	})

	t.Run("Validation - Max Length", func(t *testing.T) {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"data": "test",
		})

		// 超长ID
		longID := string(make([]byte, 300))
		for i := range longID {
			longID = string(append([]byte(longID[:i]), 'a'))
		}

		req := &miniodb.WriteDataRequest{
			Table: "default",
			Data: &miniodb.DataRecord{
				Id:        longID,
				Timestamp: timestamppb.New(time.Now()),
				Payload:   payload,
			},
		}

		err := service.validateWriteRequest(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum")
	})
}

// TestTableConfigManager_Integration 测试表配置管理器
func TestTableConfigManager_Integration(t *testing.T) {
	defaultConfig := config.TableConfig{
		AutoGenerateID: false,
		IDStrategy:     "user_provided",
		IDValidation: config.IDValidationRules{
			MaxLength: 255,
		},
	}

	manager := NewTableConfigManager(nil, defaultConfig)
	ctx := context.Background()

	t.Run("Get Default Config", func(t *testing.T) {
		cfg := manager.GetTableConfig(ctx, "non_existent_table")
		assert.Equal(t, "user_provided", cfg.IDStrategy)
		assert.False(t, cfg.AutoGenerateID)
	})

	t.Run("Cache Functionality", func(t *testing.T) {
		tableName := "test_table"

		// 第一次获取（从默认配置）
		cfg1 := manager.GetTableConfig(ctx, tableName)
		assert.Equal(t, "user_provided", cfg1.IDStrategy)

		// 检查缓存
		cachedNames := manager.GetCachedTableNames()
		// 注意：因为没有从Redis加载，可能不会缓存
		t.Logf("Cached tables: %v", cachedNames)
	})

	t.Run("Invalidate Cache", func(t *testing.T) {
		tableName := "test_table"

		// 使缓存失效
		manager.InvalidateCache(tableName)

		// 再次获取应该返回默认配置
		cfg := manager.GetTableConfig(ctx, tableName)
		assert.Equal(t, "user_provided", cfg.IDStrategy)
	})

	t.Run("Clear All Cache", func(t *testing.T) {
		manager.ClearCache()
		cachedNames := manager.GetCachedTableNames()
		assert.Empty(t, cachedNames)
	})
}
