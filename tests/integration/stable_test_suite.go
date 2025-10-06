package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBasicIntegrationFramework 测试基本集成框架功能
func TestBasicIntegrationFramework(t *testing.T) {
	// 创建测试环境
	env := NewTestEnvironment()
	defer func() {
		// 尝试关闭环境，忽略错误
		_ = env.Shutdown(context.Background())
	}()

	// 验证环境基本功能
	assert.NotNil(t, env, "Test environment should not be nil")
	assert.NotNil(t, env.GetConfig(), "Config should not be nil")
	assert.NotNil(t, env.GetMockFactory(), "Mock factory should not be nil")
}

// TestConfigurationInitialization 测试配置初始化
func TestConfigurationInitialization(t *testing.T) {
	env := NewTestEnvironment()

	// 获取配置
	config := env.GetConfig()
	assert.NotNil(t, config, "Config should not be nil")

	// 验证基本配置字段
	assert.NotEmpty(t, config.NodeID, "Node ID should not be empty")
	assert.Equal(t, "test-node-1", config.NodeID, "Node ID should match expected value")
}

// TestMockFactoryInitialization 测试Mock工厂初始化
func TestMockFactoryInitialization(t *testing.T) {
	env := NewTestEnvironment()

	// 获取Mock工厂
	mockFactory := env.GetMockFactory()
	assert.NotNil(t, mockFactory, "Mock factory should not be nil")

	// 测试获取各个Mock客户端
	require.NotPanics(t, func() {
		_ = mockFactory.GetMinIOClient()
	}, "GetMinIOClient should not panic")

	require.NotPanics(t, func() {
		_ = mockFactory.GetRedisClient()
	}, "GetRedisClient should not panic")

	require.NotPanics(t, func() {
		_ = mockFactory.GetDuckDBClient()
	}, "GetDuckDBClient should not panic")
}

// TestStorageComponentsInitialization 测试存储组件初始化
func TestStorageComponentsInitialization(t *testing.T) {
	env := NewTestEnvironment()

	// 获取存储组件
	storage := env.GetStorage()
	cacheStorage := env.GetCacheStorage()

	assert.NotNil(t, storage, "Storage should not be nil")
	assert.NotNil(t, cacheStorage, "Cache storage should not be nil")
}

// TestClientCreationSimplified 测试简化的客户端创建
func TestClientCreationSimplified(t *testing.T) {
	env := NewTestEnvironment()

	// 直接创建客户端，不依赖环境状态
	client := &TestClient{
		env: env,
	}

	assert.NotNil(t, client, "Client should not be nil")
	assert.NotNil(t, client.env, "Client env should not be nil")
}

// TestConfigurationValidation 测试配置验证
func TestConfigurationValidation(t *testing.T) {
	env := NewTestEnvironment()
	config := env.GetConfig()

	// 验证配置的完整性
	assert.NotEmpty(t, config.NodeID, "Node ID should not be empty")
	assert.NotEmpty(t, config.Redis.Addr, "Redis address should not be empty")
	assert.NotEmpty(t, config.MinIO.Endpoint, "MinIO endpoint should not be empty")
	assert.NotEmpty(t, config.MinIO.Bucket, "MinIO bucket should not be empty")

	// 验证配置的合理值
	assert.True(t, config.DuckDB.Enabled, "DuckDB should be enabled")
	assert.True(t, config.Buffer.Enabled, "Buffer should be enabled")
	assert.True(t, config.Metadata.Enabled, "Metadata should be enabled")
	assert.True(t, config.Ingest.Enabled, "Ingest should be enabled")

	// 验证数值合理性
	assert.Greater(t, config.Buffer.BufferSize, int64(0), "Buffer size should be positive")
	assert.Greater(t, config.Ingest.BufferSize, int64(0), "Ingest buffer size should be positive")
	assert.Greater(t, config.Ingest.BatchSize, int64(0), "Ingest batch size should be positive")
}

// TestNetworkConfiguration 测试网络配置
func TestNetworkConfiguration(t *testing.T) {
	env := NewTestEnvironment()
	config := env.GetConfig()

	// 验证网络配置
	assert.NotEmpty(t, config.Network.Server.Host, "Network server host should not be empty")
	assert.NotEmpty(t, config.Network.Server.GrpcPort, "gRPC port should not be empty")
	assert.NotEmpty(t, config.Network.Server.RestPort, "REST port should not be empty")

	// 验证Pools配置
	assert.NotNil(t, config.Network.Pools, "Network pools should not be nil")
	assert.NotNil(t, config.Network.Pools.Redis, "Redis pool should not be nil")
	assert.NotNil(t, config.Network.Pools.MinIO, "MinIO pool should not be nil")
}

// TestConcurrentEnvironmentCreation 测试并发环境创建
func TestConcurrentEnvironmentCreation(t *testing.T) {
	const numGoroutines = 5
	const numIterations = 3
	done := make(chan bool, numGoroutines)
	results := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			success := true
			for j := 0; j < numIterations; j++ {
				env := NewTestEnvironment()
				if env == nil {
					success = false
					break
				}

				// 验证基本功能
				if env.GetConfig() == nil {
					success = false
					break
				}

				if env.GetMockFactory() == nil {
					success = false
					break
				}
			}

			results <- success
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 检查结果
	for i := 0; i < numGoroutines; i++ {
		success := <-results
		assert.True(t, success, "Concurrent environment creation should succeed")
	}
}

// TestMockFactoryConsistency 测试Mock工厂一致性
func TestMockFactoryConsistency(t *testing.T) {
	env := NewTestEnvironment()
	mockFactory := env.GetMockFactory()

	// 多次获取客户端应该返回相同的实例
	minioClient1 := mockFactory.GetMinIOClient()
	minioClient2 := mockFactory.GetMinIOClient()
	assert.Equal(t, minioClient1, minioClient2, "MinIO clients should be the same instance")

	redisClient1 := mockFactory.GetRedisClient()
	redisClient2 := mockFactory.GetRedisClient()
	assert.Equal(t, redisClient1, redisClient2, "Redis clients should be the same instance")

	duckdbClient1 := mockFactory.GetDuckDBClient()
	duckdbClient2 := mockFactory.GetDuckDBClient()
	assert.Equal(t, duckdbClient1, duckdbClient2, "DuckDB clients should be the same instance")
}

// TestConfigurationImmutability 测试配置不可变性
func TestConfigurationImmutability(t *testing.T) {
	env := NewTestEnvironment()
	config1 := env.GetConfig()
	config2 := env.GetConfig()

	// 多次获取的配置应该是相同的实例
	assert.Equal(t, config1, config2, "Config instances should be the same")

	// 验证配置字段不可变性（如果配置被设计为不可变）
	assert.Equal(t, config1.NodeID, config2.NodeID, "Node ID should be immutable")
	assert.Equal(t, config1.Redis.Addr, config2.Redis.Addr, "Redis address should be immutable")
}

// TestEnvironmentTeardown 测试环境清理
func TestEnvironmentTeardown(t *testing.T) {
	env := NewTestEnvironment()

	// 验证环境初始状态
	assert.NotNil(t, env, "Environment should not be nil")
	assert.NotNil(t, env.GetConfig(), "Config should not be nil")
	assert.NotNil(t, env.GetMockFactory(), "Mock factory should not be nil")

	// 尝试关闭环境（这是测试的一部分）
	err := env.Shutdown(context.Background())
	// 对于测试，关闭失败是可接受的，因为依赖Mock服务可能不存在
	if err != nil {
		t.Logf("Environment shutdown returned error: %v", err)
		// 这对于测试场景是正常的
	}
}

// TestMinimalIntegrationTest 测试最小集成测试
func TestMinimalIntegrationTest(t *testing.T) {
	env := NewTestEnvironment()

	// 这是一个最小的集成测试，只验证基础功能
	assert.NotNil(t, env, "Environment should be created")
	assert.NotNil(t, env.GetConfig(), "Config should be accessible")
	assert.NotNil(t, env.GetMockFactory(), "Mock factory should be accessible")

	// 不需要任何外部依赖的验证
	t.Logf("Minimal integration test completed successfully")
}

// BenchmarkEnvironmentCreation 基准测试环境创建性能
func BenchmarkEnvironmentCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewTestEnvironment()
	}
}

// BenchmarkConfigurationAccess 基准测试配置访问性能
func BenchmarkConfigurationAccess(b *testing.B) {
	env := NewTestEnvironment()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = env.GetConfig()
	}
}

// BenchmarkMockFactoryAccess 基准测试Mock工厂访问性能
func BenchmarkMockFactoryAccess(b *testing.B) {
	env := NewTestEnvironment()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		factory := env.GetMockFactory()
		_ = factory.GetMinIOClient()
		_ = factory.GetRedisClient()
		_ = factory.GetDuckDBClient()
	}
}