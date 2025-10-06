package mock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockFactoryCreation 测试Mock工厂创建
func TestMockFactoryCreation(t *testing.T) {
	// 创建Mock工厂
	factory := NewMockFactory()
	assert.NotNil(t, factory, "Mock factory should not be nil")
}

// TestMockFactoryComponents 测试Mock工厂组件
func TestMockFactoryComponents(t *testing.T) {
	factory := NewMockFactory()

	// 测试MinIO客户端
	minioClient := factory.GetMinIOClient()
	assert.NotNil(t, minioClient, "MinIO client should not be nil")

	// 测试Redis客户端
	redisClient := factory.GetRedisClient()
	assert.NotNil(t, redisClient, "Redis client should not be nil")

	// 测试DuckDB客户端
	duckdbClient := factory.GetDuckDBClient()
	assert.NotNil(t, duckdbClient, "DuckDB client should not be nil")

	// 验证组件是单例
	assert.Equal(t, minioClient, factory.GetMinIOClient(), "MinIO client should be singleton")
	assert.Equal(t, redisClient, factory.GetRedisClient(), "Redis client should be singleton")
	assert.Equal(t, duckdbClient, factory.GetDuckDBClient(), "DuckDB client should be singleton")
}

// TestMockFactoryReset 测试Mock工厂重置
func TestMockFactoryReset(t *testing.T) {
	factory := NewMockFactory()

	// 获取组件
	minioClient := factory.GetMinIOClient()
	redisClient := factory.GetRedisClient()
	duckdbClient := factory.GetDuckDBClient()

	// 执行重置
	factory.Reset()

	// 验证组件仍然存在（重置应该是安全的）
	assert.Equal(t, minioClient, factory.GetMinIOClient(), "MinIO client should be same after reset")
	assert.Equal(t, redisClient, factory.GetRedisClient(), "Redis client should be same after reset")
	assert.Equal(t, duckdbClient, factory.GetDuckDBClient(), "DuckDB client should be same after reset")
}

// TestMockResponseCreation 测试Mock响应创建
func TestMockResponseCreation(t *testing.T) {
	// 测试成功响应
	successResponse := MockResponse{
		Success: true,
		Data:    "test-data",
		Error:   nil,
	}
	assert.True(t, successResponse.Success, "Success should be true")
	assert.Equal(t, "test-data", successResponse.Data, "Data should match")
	assert.Nil(t, successResponse.Error, "Error should be nil")

	// 测试失败响应
	failureResponse := MockResponse{
		Success: false,
		Data:    nil,
		Error:   assert.AnError,
	}
	assert.False(t, failureResponse.Success, "Success should be false")
	assert.Nil(t, failureResponse.Data, "Data should be nil")
	assert.NotNil(t, failureResponse.Error, "Error should not be nil")
}

// TestConcurrentFactoryCreation 测试并发工厂创建
func TestConcurrentFactoryCreation(t *testing.T) {
	const numGoroutines = 10
	const numOperations = 5
	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				// 创建Mock工厂
				factory := NewMockFactory()
				if factory == nil {
					errors <- assert.AnError
					return
				}

				// 获取组件
				if factory.GetMinIOClient() == nil {
					errors <- assert.AnError
					return
				}

				if factory.GetRedisClient() == nil {
					errors <- assert.AnError
					return
				}

				if factory.GetDuckDBClient() == nil {
					errors <- assert.AnError
					return
				}

				// 添加小延迟
				time.Sleep(time.Millisecond)
			}

			errors <- nil
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 检查是否有错误
	for i := 0; i < numGoroutines; i++ {
		err := <-errors
		assert.NoError(t, err, "Concurrent factory creation should not cause errors")
	}
}

// TestConcurrentComponentAccess 测试并发组件访问
func TestConcurrentComponentAccess(t *testing.T) {
	factory := NewMockFactory()

	const numGoroutines = 10
	const numOperations = 10
	done := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				// 并发访问所有组件
				minioClient := factory.GetMinIOClient()
				redisClient := factory.GetRedisClient()
				duckdbClient := factory.GetDuckDBClient()

				if minioClient == nil || redisClient == nil || duckdbClient == nil {
					errors <- assert.AnError
					return
				}

				// 添加小延迟
				time.Sleep(time.Microsecond * 10)
			}

			errors <- nil
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 检查是否有错误
	for i := 0; i < numGoroutines; i++ {
		err := <-errors
		assert.NoError(t, err, "Concurrent component access should not cause errors")
	}
}

// TestMockFactoryConsistency 测试Mock工厂一致性
func TestMockFactoryConsistency(t *testing.T) {
	// 创建多个工厂实例
	factory1 := NewMockFactory()
	factory2 := NewMockFactory()
	factory3 := NewMockFactory()

	// 每个工厂应该返回相同的实例
	assert.Equal(t, factory1.GetMinIOClient(), factory2.GetMinIOClient(), "MinIO clients should be same across factories")
	assert.Equal(t, factory2.GetMinIOClient(), factory3.GetMinIOClient(), "MinIO clients should be same across factories")
	assert.Equal(t, factory1.GetRedisClient(), factory2.GetRedisClient(), "Redis clients should be same across factories")
	assert.Equal(t, factory2.GetRedisClient(), factory3.GetRedisClient(), "Redis clients should be same across factories")
	assert.Equal(t, factory1.GetDuckDBClient(), factory2.GetDuckDBClient(), "DuckDB clients should be same across factories")
	assert.Equal(t, factory2.GetDuckDBClient(), factory3.GetDuckDBClient(), "DuckDB clients should be same across factories")
}

// TestMockResponseEquality 测试Mock响应相等性
func TestMockResponseEquality(t *testing.T) {
	// 创建相同的响应
	resp1 := MockResponse{
		Success: true,
		Data:    "test-data",
		Error:   nil,
	}

	resp2 := MockResponse{
		Success: true,
		Data:    "test-data",
		Error:   nil,
	}

	// 测试相等性（这需要根据实际的MockResponse定义实现）
	// 这里我们简化处理，只验证基本字段
	assert.Equal(t, resp1.Success, resp2.Success, "Success should be equal")
	assert.Equal(t, resp1.Data, resp2.Data, "Data should be equal")
	if resp1.Error == nil && resp2.Error == nil {
		// 两个错误都为nil，被认为是相等的
	} else if resp1.Error != nil && resp2.Error != nil {
		// 两个错误都非nil，检查错误消息
		assert.Equal(t, resp1.Error.Error(), resp2.Error.Error(), "Error messages should be equal")
	} else {
		// 一个错误为nil，另一个非nil，不相等
		t.Error("Error states should match")
	}
}

// TestMockResponseSerialization 测试Mock响应序列化
func TestMockResponseSerialization(t *testing.T) {
	// 创建测试响应
	resp := MockResponse{
		Success: true,
		Data:    map[string]interface{}{
			"key1": "value1",
			"key2": 42,
			"key3": true,
		},
		Error: nil,
	}

	// 简单序列化测试（这里我们主要测试数据结构完整性）
	assert.NotNil(t, resp, "Mock response should not be nil")
	assert.True(t, resp.Success, "Success should be true")
	assert.NotNil(t, resp.Data, "Data should not be nil")
	assert.Nil(t, resp.Error, "Error should be nil")

	// 验证数据结构
	if dataMap, ok := resp.Data.(map[string]interface{}); ok {
		assert.Equal(t, 3, len(dataMap), "Data map should have 3 elements")
		assert.Equal(t, "value1", dataMap["key1"], "key1 should have correct value")
		assert.Equal(t, 42, dataMap["key2"], "key2 should have correct value")
		assert.Equal(t, true, dataMap["key3"], "key3 should have correct value")
	} else {
		t.Error("Data should be a map")
	}
}

// BenchmarkMockFactoryCreation 基准测试Mock工厂创建性能
func BenchmarkMockFactoryCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewMockFactory()
	}
}

// BenchmarkMockComponentAccess 基准测试Mock组件访问性能
func BenchmarkMockComponentAccess(b *testing.B) {
	factory := NewMockFactory()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = factory.GetMinIOClient()
		_ = factory.GetRedisClient()
		_ = factory.GetDuckDBClient()
	}
}

// BenchmarkMockFactoryReset 基准测试Mock工厂重置性能
func BenchmarkMockFactoryReset(b *testing.B) {
	factory := NewMockFactory()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		factory.Reset()
	}
}

// BenchmarkMockResponseCreation 基准测试Mock响应创建性能
func BenchmarkMockResponseCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MockResponse{
			Success: true,
			Data:    "test-data",
			Error:   nil,
		}
	}
}

// TestMockFactoryMemoryUsage 测试Mock工厂内存使用
func TestMockFactoryMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	const numFactories = 1000
	const numIterations = 100

	// 创建多个工厂实例
	factories := make([]*MockFactory, numFactories)
	for i := 0; i < numFactories; i++ {
		factories[i] = NewMockFactory()
	}

	// 执行多次操作
	for i := 0; i < numIterations; i++ {
		for j := 0; j < numFactories; j++ {
			factory := factories[j]
			factory.GetMinIOClient()
			factory.GetRedisClient()
			factory.GetDuckDBClient()
		}
	}

	// 执行重置
	for i := 0; i < numFactories; i++ {
		factories[i].Reset()
	}

	// 验证工厂仍然可用
	for i := 0; i < numFactories; i++ {
		assert.NotNil(t, factories[i], "Factory should still exist")
		assert.NotNil(t, factories[i].GetMinIOClient(), "MinIO client should still exist")
		assert.NotNil(t, factories[i].GetRedisClient(), "Redis client should still exist")
		assert.NotNil(t, factories[i].GetDuckDBClient(), "DuckDB client should still exist")
	}

	// 测试清理（GC会自动处理）
	factories = nil
}