package performance

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BenchmarkConfigCreation 基准测试配置创建
func BenchmarkConfigCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = createTestConfig()
	}
}

// BenchmarkMockFactoryCreation 基准测试Mock工厂创建
func BenchmarkMockFactoryCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = createTestMockFactory()
	}
}

// BenchmarkTestEnvironmentCreation 基准测试测试环境创建
func BenchmarkTestEnvironmentCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = createTestEnvironment()
	}
}

// BenchmarkConcurrentEnvironmentCreation 基准测试并发环境创建
func BenchmarkConcurrentEnvironmentCreation(b *testing.B) {
	const concurrency = 10
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = createTestEnvironment()
		}
	})
}

// BenchmarkMockClientAccess 基准测试Mock客户端访问
func BenchmarkMockClientAccess(b *testing.B) {
	env := createTestEnvironment()
	factory := env.GetMockFactory()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = factory.GetMinIOClient()
		_ = factory.GetRedisClient()
		_ = factory.GetDuckDBClient()
	}
}

// BenchmarkStorageOperations 基准测试存储操作
func BenchmarkStorageOperations(b *testing.B) {
	env := createTestEnvironment()
	storage := env.GetStorage()
	ctx := context.Background()

	// 预热
	storage.Put(ctx, "warmup-key", []byte("warmup-data"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("benchmark-key-%d", i%1000)
		data := []byte(fmt.Sprintf("benchmark-data-%d", i))

		storage.Put(ctx, key, data)
		storage.Get(ctx, key)
		storage.Delete(ctx, key)
	}
}

// BenchmarkCacheStorageOperations 基准测试缓存存储操作
func BenchmarkCacheStorageOperations(b *testing.B) {
	env := createTestEnvironment()
	cacheStorage := env.GetCacheStorage()
	ctx := context.Background()

	// 预热
	cacheStorage.Put(ctx, "warmup-key", []byte("warmup-data"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("benchmark-cache-key-%d", i%1000)
		data := []byte(fmt.Sprintf("benchmark-cache-data-%d", i))

		cacheStorage.Put(ctx, key, data)
		cacheStorage.Get(ctx, key)
		cacheStorage.Delete(ctx, key)
	}
}

// BenchmarkConcurrentStorageOperations 基准测试并发存储操作
func BenchmarkConcurrentStorageOperations(b *testing.B) {
	env := createTestEnvironment()
	storage := env.GetStorage()
	ctx := context.Background()

	// 预热
	storage.Put(ctx, "warmup-key", []byte("warmup-data"))

	const concurrency = 10
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := fmt.Sprintf("benchmark-key-%d-%d", pb.N%1000, pb.N%100)
			data := []byte(fmt.Sprintf("benchmark-data-%d-%d", pb.N, pb.N))

			storage.Put(ctx, key, data)
			storage.Get(ctx, key)
			storage.Delete(ctx, key)
		}
	})
}

// BenchmarkConfigurationAccess 基准测试配置访问
func BenchmarkConfigurationAccess(b *testing.B) {
	env := createTestEnvironment()
	config := env.GetConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.NodeID
		_ = config.Redis.Addr
		_ = config.MinIO.Endpoint
		_ = config.DuckDB.Path
		_ = config.Buffer.BufferSize
		_ = config.Ingest.BufferSize
	}
}

// BenchmarkMockFactoryOperations 基准测试Mock工厂操作
func BenchmarkMockFactoryOperations(b *testing.B) {
	factory := createTestMockFactory()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		minioClient := factory.GetMinIOClient()
		redisClient := factory.GetRedisClient()
		duckdbClient := factory.GetDuckDBClient()

		// 简化操作，避免优化
		_ = minioClient != nil
		_ = redisClient != nil
		_ = duckdbClient != nil
	}
}

// BenchmarkMemoryAllocation 基准测试内存分配
func BenchmarkMemoryAllocation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 创建各种对象，测试内存分配
		_ = createTestConfig()
		_ = createTestMockFactory()
		_ = createTestEnvironment()
		_ = fmt.Sprintf("test-string-%d", i)
		_ = []byte(fmt.Sprintf("test-bytes-%d", i))
		_ = map[string]interface{}{
			"key1": "value1",
			"key2": i,
			"key3": true,
		}
	}
}

// BenchmarkStringOperations 基准测试字符串操作
func BenchmarkStringOperations(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 测试常见字符串操作
		str := fmt.Sprintf("benchmark-string-%d", i%10000)
		_ = str + "-suffix"
		_ = str[:len(str)-1]
		_ = strconv.Itoa(i)
		_ = fmt.Sprint(i, "-", "test")
	}
}

// BenchmarkConcurrencyContention 基准测试并发争用
func BenchmarkConcurrencyContention(b *testing.B) {
	env := createTestEnvironment()
	storage := env.GetStorage()
	ctx := context.Background()

	const numOperations = 1000
	const numWorkers = 10
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for i := 0; i < numOperations; i++ {
			key := fmt.Sprintf("concurrent-key-%d-%d", pb.N%numWorkers, i)
			data := []byte(fmt.Sprintf("concurrent-data-%d-%d", pb.N, i))

			storage.Put(ctx, key, data)
			storage.Get(ctx, key)
		storage.Delete(ctx, key)
		}
	})
}

// BenchmarkLargeDataOperations 基准测试大数据操作
func BenchmarkLargeDataOperations(b *testing.B) {
	env := createTestEnvironment()
	storage := env.GetStorage()
	ctx := context.Background()

	// 创建大数据块
	largeData := make([]byte, 1024*1024) // 1MB
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("large-data-key-%d", i%100)
		storage.Put(ctx, key, largeData)
		storage.Get(ctx, key)
		storage.Delete(ctx, key)
	}
}

// BenchmarkSmallDataOperations 基准测试小数据操作
func BenchmarkSmallDataOperations(b *testing.B) {
	env := createTestEnvironment()
	storage := env.GetStorage()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("small-data-key-%d", i%10000)
		data := []byte(fmt.Sprintf("small-data-%d", i))

		storage.Put(ctx, key, data)
		storage.Get(ctx, key)
		storage.Delete(ctx, key)
	}
}

// BenchmarkBatchOperations 基准测试批量操作
func BenchmarkBatchOperations(b *testing.B) {
	env := createTestEnvironment()
	storage := env.GetStorage()
	ctx := context.Background()

	const batchSize = 100
	b.ResetTimer()

	for i := 0; i < b.N; i += batchSize {
		// 批量操作
		for j := 0; j < batchSize; j++ {
			key := fmt.Sprintf("batch-key-%d-%d", i, j)
			data := []byte(fmt.Sprintf("batch-data-%d-%d", i, j))
			storage.Put(ctx, key, data)
		}

		// 批量读取
		for j := 0; j < batchSize; j++ {
			key := fmt.Sprintf("batch-key-%d-%d", i, j)
			storage.Get(ctx, key)
		}

		// 批量删除
		for j := 0; j < batchSize; j++ {
			key := fmt.Sprintf("batch-key-%d-%d", i, j)
			storage.Delete(ctx, key)
		}
	}
}

// BenchmarkEnvironmentTeardown 基准测试环境清理
func BenchmarkEnvironmentTeardown(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := createTestEnvironment()
		// 模拟清理操作
		_ = env.GetConfig()
		_ = env.GetMockFactory()
		_ = env.GetStorage()
		_ = env.GetCacheStorage()
	}
}

// TestPerformanceInfrastructure 测试性能基础设施
func TestPerformanceInfrastructure(t *testing.T) {
	// 测试配置创建
	config := &PerformanceConfig{
		Duration:        10 * time.Second,
		Concurrency:     5,
		WarmupDuration: 2 * time.Second,
		ReportInterval:   1 * time.Second,
		MaxLatency:      100 * time.Millisecond,
		MinThroughput:   1000,
		CustomMetrics:   []string{"cpu_usage", "memory_usage"},
	}

	assert.NotNil(t, config, "Performance config should not be nil")
	assert.Equal(t, 10*time.Second, config.Duration, "Duration should match")
	assert.Equal(t, 5, config.Concurrency, "Concurrency should match")
	assert.Equal(t, 2*time.Second, config.WarmupDuration, "Warmup duration should match")
}

// TestPerformanceTestResult 测试性能测试结果
func TestPerformanceTestResult(t *testing.T) {
	result := &PerformanceTestResult{
		TestName:      "test-benchmark",
		StartTime:      time.Now().Add(-time.Minute),
		EndTime:        time.Now(),
		TotalOperations: 10000,
		SuccessfulOps:  9500,
	}

	// 计算衍生字段
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.FailedOps = result.TotalOperations - result.SuccessfulOps
	result.ErrorRate = float64(result.FailedOps) / float64(result.TotalOperations) * 100
	result.Throughput = float64(result.SuccessfulOps) / result.Duration.Seconds()

	assert.Equal(t, "test-benchmark", result.TestName, "Test name should match")
	assert.Equal(t, 10000, result.TotalOperations, "Total operations should match")
	assert.Equal(t, 9500, result.SuccessfulOps, "Successful operations should match")
	assert.Equal(t, 500, result.FailedOps, "Failed operations should match")
	assert.Equal(t, 5.0, result.ErrorRate, "Error rate should match")
	assert.Greater(t, result.Throughput, 0.0, "Throughput should be positive")
}

// TestLatencyDistribution 测试延迟分布计算
func TestLatencyDistribution(t *testing.T) {
	// 创建测试延迟数据
	latencyData := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
		60 * time.Millisecond,
		70 * time.Millisecond,
		80 * time.Millisecond,
		90 * time.Millisecond,
		100 * time.Millisecond,
	}

	dist := CalculateLatencyDistribution(latencyData)

	assert.NotNil(t, dist, "Latency distribution should not be nil")
	assert.Equal(t, 10*time.Millisecond, dist.Min, "Min latency should match")
	assert.Equal(t, 100*time.Millisecond, dist.Max, "Max latency should match")
	assert.Equal(t, int64(10), dist.OperationCount, "Operation count should match")

	// 验证百分位数（基于10个数据点的近似值）
	assert.Equal(t, 30*time.Millisecond, dist.P25, "P25 latency should match")
	assert.Equal(t, 50*time.Millisecond, dist.P50, "P50 latency should match")
	assert.Equal(t, 80*time.Millisecond, dist.P90, "P90 latency should match")
	assert.Equal(t, 95*time.Millisecond, dist.P95, "P95 latency should match")
	assert.Equal(t, 100*time.Millisecond, dist.P99, "P99 latency should match")
}

// TestThroughputStatistics 测试吞吐量统计计算
func TestThroughputStatistics(t *testing.T) {
	// 创建测试吞吐量数据
	throughputData := []float64{
		100.0, 150.0, 200.0, 120.0, 180.0,
		140.0, 160.0, 190.0, 110.0, 170.0,
		130.0, 145.0, 155.0, 165.0, 175.0,
		185.0, 195.0, 105.0, 115.0, 125.0,
	}

	stats := CalculateThroughputStatistics(throughputData)

	assert.NotNil(t, stats, "Throughput statistics should not be nil")
	assert.Equal(t, 100.0, stats.Min, "Min throughput should match")
	assert.Equal(t, 200.0, stats.Max, "Max throughput should match")

	// 验证平均值（基于20个数据点的计算）
	expectedAvg := 155.5 // (100+150+200+120+180+140+160+190+110+170+130+145+155+165+175+185+195+105+115+125) / 20
	assert.InDelta(t, expectedAvg, stats.Average, 0.001, "Average throughput should match expected")

	assert.Greater(t, 0.0, stats.Variance, "Variance should be positive")
	assert.Greater(t, 0.0, stats.StdDev, "StdDev should be positive")
}

// TestPerformanceTestRunnerCreation 测试性能测试运行器创建
func TestPerformanceTestRunnerCreation(t *testing.T) {
	config := &PerformanceConfig{
		Duration:      10 * time.Second,
		Concurrency:   5,
	}

	// 创建一个简单的模拟指标收集器
	metricsCollector := createMockMetricsCollector()

	runner := NewPerformanceTestRunner(config, metricsCollector)

	assert.NotNil(t, runner, "Performance test runner should not be nil")
	assert.Equal(t, config, runner.config, "Config should match")
	assert.Equal(t, metricsCollector, runner.metrics, "Metrics collector should match")
}

// Mock helper functions

// createTestConfig 创建测试配置
func createTestConfig() *PerformanceConfig {
	return &PerformanceConfig{
		Duration:        10 * time.Second,
		Concurrency:     5,
		WarmupDuration:  2 * time.Second,
		ReportInterval:   1 * time.Second,
		MaxLatency:      100 * time.Millisecond,
		MinThroughput:   1000,
		CustomMetrics:   []string{},
	}
}

// createTestMockFactory 创建测试Mock工厂
func createTestMockFactory() interface{} {
	// 简化实现，返回一个接口
	return struct{}{}
}

// createTestEnvironment 创建测试环境
func createTestEnvironment() interface{} {
	// 简化实现，返回一个接口
	return struct{}{}
}

// createMockMetricsCollector 创建模拟指标收集器
func createMockMetricsCollector() interface{} {
	// 简化实现，返回一个接口
	return struct{}{}
}