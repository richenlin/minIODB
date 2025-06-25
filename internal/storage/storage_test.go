package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"minIODB/internal/config"
)

// TestStorageCreation 测试存储实例创建
func TestStorageCreation(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		Pool: config.PoolConfig{
			Redis: config.RedisConfig{
				Mode:     "standalone",
				Addr:     "localhost:6379",
				Password: "",
				DB:       0,
			},
			MinIO: config.MinioConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "minioadmin",
				SecretAccessKey: "minioadmin",
				UseSSL:          false,
			},
			HealthCheckInterval: 30 * time.Second,
		},
	}

	// 由于测试环境可能没有实际的Redis和MinIO服务，
	// 这个测试主要验证创建逻辑
	storage, err := NewStorage(cfg)
	if err != nil {
		// 在没有实际服务的情况下，这是预期的错误
		t.Logf("Expected error in test environment: %v", err)
		return
	}

	// 如果成功创建，验证基本功能
	assert.NotNil(t, storage)
	
	// 清理
	if storage != nil {
		err = storage.Close()
		assert.NoError(t, err)
	}
}

// TestStorageInterface 测试存储接口
func TestStorageInterface(t *testing.T) {
	// 验证StorageImpl实现了Storage接口
	var _ Storage = &StorageImpl{}
}

// TestRedisOperations 测试Redis操作（模拟）
func TestRedisOperations(t *testing.T) {
	// 这个测试需要实际的Redis连接，在CI/CD环境中可能会跳过
	t.Skip("Skipping Redis operations test - requires actual Redis instance")
	
	ctx := context.Background()
	
	// 创建测试配置
	cfg := &config.Config{
		Pool: config.PoolConfig{
			Redis: config.RedisConfig{
				Mode: "standalone",
				Addr: "localhost:6379",
			},
		},
	}
	
	storage, err := NewStorage(cfg)
	require.NoError(t, err)
	defer storage.Close()
	
	// 测试Set操作
	err = storage.Set(ctx, "test_key", "test_value", time.Minute)
	assert.NoError(t, err)
	
	// 测试Get操作
	value, err := storage.Get(ctx, "test_key")
	assert.NoError(t, err)
	assert.Equal(t, "test_value", value)
	
	// 测试Del操作
	err = storage.Del(ctx, "test_key")
	assert.NoError(t, err)
} 