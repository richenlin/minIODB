package coordinator

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/discovery"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

func setupTestEnvironment(t *testing.T) (*redis.Client, *discovery.ServiceRegistry, func()) {
	// 使用真实的Redis客户端进行测试
	redisClient := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       1, // 使用DB 1进行测试，避免与实际数据冲突
	})

	// 测试Redis连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		t.Skipf("Redis not available for testing: %v", err)
	}

	// 清理测试数据
	redisClient.FlushDB(ctx)

	// 创建服务注册表
	cfg := config.Config{
		Redis: config.RedisConfig{
			Addr:     "localhost:6379",
			Password: "",
			DB:       1,
		},
	}
	registry, _ := discovery.NewServiceRegistry(cfg, "test-node-1", "8080")

	cleanup := func() {
		redisClient.FlushDB(context.Background())
		redisClient.Close()
	}

	return redisClient, registry, cleanup
}

func TestNewWriteCoordinator(t *testing.T) {
	_, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()

	wc := NewWriteCoordinator(registry)
	assert.NotNil(t, wc)
	assert.NotNil(t, wc.registry)
	assert.NotNil(t, wc.hashRing)
}

func TestNewQueryCoordinator(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()

	qc := NewQueryCoordinator(redisClient, registry)
	assert.NotNil(t, qc)
	assert.NotNil(t, qc.redisClient)
	assert.NotNil(t, qc.registry)
}

func TestQueryCoordinator_aggregateResults(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()

	qc := NewQueryCoordinator(redisClient, registry)
	
	results := []string{
		`[{"id": "1", "value": "a"}]`,
		`[{"id": "2", "value": "b"}]`,
	}
	
	aggregated, err := qc.aggregateResults(results)
	assert.NoError(t, err)
	assert.Contains(t, aggregated, `"id":"1"`)
	assert.Contains(t, aggregated, `"id":"2"`)
}

func TestQueryCoordinator_GetQueryStats(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()

	qc := NewQueryCoordinator(redisClient, registry)
	
	stats := qc.GetQueryStats()
	assert.NotNil(t, stats)
	// 验证统计信息包含预期的字段
	assert.Contains(t, stats, "total_queries")
	assert.Contains(t, stats, "successful_queries")
	assert.Contains(t, stats, "failed_queries")
}
