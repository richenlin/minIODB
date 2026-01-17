package pool

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPoolManagerConfig(t *testing.T) {
	config := DefaultPoolManagerConfig()
	
	assert.NotNil(t, config)
	assert.NotNil(t, config.Redis)
	assert.NotNil(t, config.MinIO)
	assert.Equal(t, RedisModeStandalone, config.Redis.Mode)
	assert.Equal(t, "localhost:6379", config.Redis.Addr)
	assert.Equal(t, "localhost:9000", config.MinIO.Endpoint)
	assert.Equal(t, "minioadmin", config.MinIO.AccessKeyID)
	assert.Equal(t, "minioadmin", config.MinIO.SecretAccessKey)
	assert.Equal(t, 30*time.Second, config.HealthCheckInterval)
}

func TestPoolManagerConfig_Validation(t *testing.T) {
	// 测试空配置
	config := &PoolManagerConfig{}
	assert.NotNil(t, config)
	
	// 测试部分配置
	config.Redis = &RedisPoolConfig{
		Mode: RedisModeStandalone,
		Addr: "localhost:6379",
	}
	config.MinIO = &MinIOPoolConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
	}
	config.HealthCheckInterval = time.Minute
	
	assert.Equal(t, RedisModeStandalone, config.Redis.Mode)
	assert.Equal(t, "localhost:6379", config.Redis.Addr)
	assert.Equal(t, "localhost:9000", config.MinIO.Endpoint)
	assert.Equal(t, time.Minute, config.HealthCheckInterval)
} 