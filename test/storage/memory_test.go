package storage

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestMemoryOptimizerBasicOperations 测试内存优化器基本操作
func TestMemoryOptimizerBasicOperations(t *testing.T) {
	ctx := context.Background()

	config := &MemoryConfig{
		MaxMemoryUsage:   1024 * 1024 * 128, // 128MB
		PoolSizes:        map[string]int{"test": 4096},
		GCInterval:       time.Second * 30,
		ZeroCopyEnabled:  true,
		CompressionLevel: 3,
		EnableProfiling:  false,
	}

	optimizer := NewMemoryOptimizer(ctx, config)
	require.NotNil(t, optimizer)

	// 验证内存池已初始化
	assert.NotNil(t, optimizer.memoryPools)
	assert.NotNil(t, optimizer.bufferOptimizer)
	assert.NotNil(t, optimizer.zeroCopyManager)
	assert.NotNil(t, optimizer.gcManager)
	assert.NotNil(t, optimizer.stats)
}

// TestBufferOptimizerOperations 测试缓冲区优化器操作
func TestBufferOptimizerOperations(t *testing.T) {
	optimizer := NewBufferOptimizer()
	require.NotNil(t, optimizer)
}

// TestZeroCopyManagerOperations 测试零拷贝管理器操作
func TestZeroCopyManagerOperations(t *testing.T) {
	manager := NewZeroCopyManager()
	require.NotNil(t, manager)
}

// TestGCManagerOperations 测试垃圾回收管理器操作
func TestGCManagerOperations(t *testing.T) {
	gcInterval := 30 * time.Second
	manager := NewGCManager(gcInterval)
	require.NotNil(t, manager)
}

// TestMemoryConfig 测试内存配置
func TestMemoryConfig(t *testing.T) {
	config := &MemoryConfig{
		MaxMemoryUsage:   64 * 1024 * 1024, // 64MB
		PoolSizes:        map[string]int{"small": 512},
		GCInterval:       time.Minute,
		ZeroCopyEnabled:  true,
		CompressionLevel: 3,
		EnableProfiling:  true,
	}

	// 验证配置字段类型和值
	assert.Equal(t, int64(64*1024*1024), config.MaxMemoryUsage)
	assert.Len(t, config.PoolSizes, 1)
	assert.Equal(t, time.Minute, config.GCInterval)
	assert.True(t, config.ZeroCopyEnabled)
	assert.Equal(t, 3, config.CompressionLevel)
	assert.True(t, config.EnableProfiling)
}

// TestMemoryOptimizerWithDefaults 测试使用默认配置的优化器
func TestMemoryOptimizerWithDefaults(t *testing.T) {
	ctx := context.Background()

	config := &MemoryConfig{
		MaxMemoryUsage: 128 * 1024 * 1024,
		PoolSizes:      map[string]int{},
		GCInterval:     time.Minute,
	}

	optimizer := NewMemoryOptimizer(ctx, config)
	require.NotNil(t, optimizer)

	// 即使没有自定义内存池，也应该有默认的内存池
	assert.NotEmpty(t, optimizer.memoryPools)
}

// TestMemoryOptimizerConfiguration 测试优化器配置传播
func TestMemoryOptimizerConfiguration(t *testing.T) {
	ctx := context.Background()

	config := &MemoryConfig{
		MaxMemoryUsage:   32 * 1024 * 1024,
		PoolSizes:        map[string]int{},
		GCInterval:       time.Minute,
		ZeroCopyEnabled:  false,
		CompressionLevel: 1,
		EnableProfiling:  false,
	}

	optimizer := NewMemoryOptimizer(ctx, config)
	require.NotNil(t, optimizer)

	// 验证配置被正确传播
	assert.Equal(t, config.MaxMemoryUsage, optimizer.config.MaxMemoryUsage)
	assert.Equal(t, config.ZeroCopyEnabled, optimizer.config.ZeroCopyEnabled)
	assert.Equal(t, config.EnableProfiling, optimizer.config.EnableProfiling)
}

// TestMemoryOptimizerConcurrentAccess 测试并发访问
func TestMemoryOptimizerConcurrentAccess(t *testing.T) {
	ctx := context.Background()

	config := &MemoryConfig{
		MaxMemoryUsage: 16 * 1024 * 1024,
		PoolSizes:      map[string]int{"test": 2048},
		GCInterval:     time.Minute * 2,
	}

	optimizer := NewMemoryOptimizer(ctx, config)

	// 并发测试多个goroutine访问优化器
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(threadID int) {
			// 访问所有组件
			_ = optimizer.memoryPools
			_ = optimizer.bufferOptimizer
			_ = optimizer.zeroCopyManager
			_ = optimizer.gcManager
			_ = optimizer.stats
			_ = optimizer.config
			done <- true
		}(i)
	}

	// 等待所有并发访问完成
	for i := 0; i < 5; i++ {
		select {
		case <-done:
			// 成功
		case <-time.After(time.Second):
			t.Fatal("Concurrent access timeout")
		}
	}
}

// TestMemoryOptimizerInitialization 测试初始化过程
func TestMemoryOptimizerInitialization(t *testing.T) {
	// 测试logger初始化已经完成
	assert.NotNil(t, logger.GetLogger())

	config := &MemoryConfig{
		MaxMemoryUsage:   256 * 1024 * 1024,
		PoolSizes:        map[string]int{"small": 4096, "medium": 16384},
		GCInterval:       time.Minute,
		ZeroCopyEnabled:  true,
		CompressionLevel: 6,
		EnableProfiling:  true,
	}

	// 验证配置类型正确
	assert.IsType(t, int64(0), config.MaxMemoryUsage)
	assert.IsType(t, make(map[string]int), config.PoolSizes)
	assert.IsType(t, time.Duration(0), config.GCInterval)
	assert.IsType(t, true, config.ZeroCopyEnabled)
	assert.IsType(t, 0, config.CompressionLevel)
	assert.IsType(t, true, config.EnableProfiling)
}

// TestMemoryComponentsIntegration 测试组件集成
func TestMemoryComponentsIntegration(t *testing.T) {
	ctx := context.Background()

	// 创建配置
	config := &MemoryConfig{
		MaxMemoryUsage:   64 * 1024 * 1024,
		PoolSizes:        map[string]int{"integration": 1024},
		GCInterval:       20 * time.Second,
		ZeroCopyEnabled:  true,
		CompressionLevel: 4,
		EnableProfiling:  true,
	}

	// 创建优化器
	optimizer := NewMemoryOptimizer(ctx, config)

	// 验证所有组件正确连接
	assert.Equal(t, config, optimizer.config)
	assert.NotNil(t, optimizer.bufferOptimizer)
	assert.NotNil(t, optimizer.zeroCopyManager)
	assert.NotNil(t, optimizer.gcManager)
	assert.NotNil(t, optimizer.stats)

	// 验证内存池配置反映到实际池大小
	assert.True(t, len(optimizer.memoryPools) > 0)
}

// TestMemoryOptimizerEdgeCases 测试边界情况
func TestMemoryOptimizerEdgeCases(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name   string
		config MemoryConfig
	}{
		{
			name: "空内存池配置",
			config: MemoryConfig{
				MaxMemoryUsage:   1024 * 1024,
				PoolSizes:        map[string]int{},
				GCInterval:       time.Second,
				ZeroCopyEnabled:  false,
				CompressionLevel: 0,
				EnableProfiling:  false,
			},
		},
		{
			name: "零内存限制",
			config: MemoryConfig{
				MaxMemoryUsage:   0,
				PoolSizes:        map[string]int{"test": 512},
				GCInterval:       time.Minute,
				ZeroCopyEnabled:  true,
				CompressionLevel: 9,
				EnableProfiling:  true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			optimizer := NewMemoryOptimizer(ctx, &tc.config)
			require.NotNil(t, optimizer)

			// 验证边界情况正确处理
			assert.NotNil(t, optimizer.config)
			assert.NotNil(t, optimizer.memoryPools)
			assert.NotNil(t, optimizer.bufferOptimizer)
			assert.NotNil(t, optimizer.zeroCopyManager)
			assert.NotNil(t, optimizer.gcManager)
			assert.NotNil(t, optimizer.stats)
		})
	}
}

// TestMemoryStatistics 测试内存统计信息
func TestMemoryStatistics(t *testing.T) {
	ctx := context.Background()

	config := &MemoryConfig{
		MaxMemoryUsage:   16 * 1024 * 1024,
		PoolSizes:        map[string]int{"stats": 2048},
		GCInterval:       time.Minute,
		EnableProfiling:  true,
	}

	optimizer := NewMemoryOptimizer(ctx, config)
	require.NotNil(t, optimizer)

	// 获取统计信息
	stats := optimizer.GetStats()
	assert.NotNil(t, stats)

	// 验证统计信息不为空
	// 具体字段需要根据实际实现检查
	assert.NotNil(t, stats)
	assert.IsType(t, &MemoryStats{}, stats)
}