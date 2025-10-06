package buffer

import (
	"context"
	"minIODB/internal/logger"
	"runtime"
	"testing"
	"time"
)

func init() {
	// 初始化logger用于测试
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
}

// TestMemoryPressureFlush 测试基于内存压力的刷新
func TestMemoryPressureFlush(t *testing.T) {
	ctx := context.Background()

	// 设置一个很低的内存阈值以便测试
	config := &ConcurrentBufferConfig{
		BufferSize:        1000,
		FlushInterval:     1 * time.Hour, // 很长的刷新间隔，确保只由内存触发
		WorkerPoolSize:    2,
		TaskQueueSize:     10,
		MemoryThreshold:   1 * 1024 * 1024, // 1MB 阈值（测试用）
		EnableMemoryFlush: true,
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	// 记录初始内存
	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)
	initialMemory := memStatsBefore.Alloc

	t.Logf("Initial memory: %s", formatBytes(int64(initialMemory)))
	t.Logf("Memory threshold: %s", formatBytes(config.MemoryThreshold))

	// 模拟添加数据（不会真正触发刷新，因为没有poolManager）
	for i := 0; i < 100; i++ {
		row := DataRow{
			ID:        "test-id",
			Timestamp: time.Now().UnixNano(),
			Payload:   "test data",
		}
		cb.Add(ctx, row)
	}

	// 触发自适应刷新检查
	cb.checkAdaptiveFlush(ctx)

	// 检查当前内存
	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)
	currentMemory := memStatsAfter.Alloc

	t.Logf("Current memory: %s", formatBytes(int64(currentMemory)))
	t.Logf("Memory threshold: %s", formatBytes(config.MemoryThreshold))

	if currentMemory > uint64(config.MemoryThreshold) {
		t.Logf("Memory exceeds threshold - adaptive flush would be triggered")
	} else {
		t.Logf("Memory within threshold - no flush needed")
	}
}

// TestMemoryFlushDisabled 测试禁用内存刷新
func TestMemoryFlushDisabled(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:        1000,
		FlushInterval:     1 * time.Hour,
		WorkerPoolSize:    2,
		TaskQueueSize:     10,
		MemoryThreshold:   1 * 1024, // 1KB 非常低的阈值
		EnableMemoryFlush: false,    // 禁用内存刷新
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	// 添加数据
	for i := 0; i < 100; i++ {
		row := DataRow{
			ID:        "test-id",
			Timestamp: time.Now().UnixNano(),
			Payload:   "test data",
		}
		cb.Add(ctx, row)
	}

	// 即使内存可能超过阈值，也不应该触发刷新
	cb.checkAdaptiveFlush(ctx)

	t.Log("Memory flush is disabled - no memory-based flush should occur")
}

// TestMemoryThresholdConfiguration 测试不同的内存阈值配置
func TestMemoryThresholdConfiguration(t *testing.T) {
	testCases := []struct {
		name            string
		memoryThreshold int64
		enableFlush     bool
	}{
		{
			name:            "512MB_threshold",
			memoryThreshold: 512 * 1024 * 1024,
			enableFlush:     true,
		},
		{
			name:            "1GB_threshold",
			memoryThreshold: 1024 * 1024 * 1024,
			enableFlush:     true,
		},
		{
			name:            "disabled",
			memoryThreshold: 0,
			enableFlush:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			config := &ConcurrentBufferConfig{
				BufferSize:        1000,
				FlushInterval:     1 * time.Hour,
				WorkerPoolSize:    2,
				TaskQueueSize:     10,
				MemoryThreshold:   tc.memoryThreshold,
				EnableMemoryFlush: tc.enableFlush,
			}

			_ = NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			currentMemory := int64(memStats.Alloc)

			t.Logf("Current memory: %s", formatBytes(currentMemory))
			t.Logf("Threshold: %s", formatBytes(tc.memoryThreshold))
			t.Logf("Flush enabled: %v", tc.enableFlush)

			if tc.enableFlush && tc.memoryThreshold > 0 {
				if currentMemory > tc.memoryThreshold {
					t.Logf("Memory exceeds threshold - flush would trigger")
				} else {
					t.Logf("Memory within threshold")
				}
			}
		})
	}
}

// TestFormatBytes 测试字节格式化函数
func TestFormatBytes(t *testing.T) {
	testCases := []struct {
		bytes    int64
		expected string
	}{
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 512, "512.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
	}

	for _, tc := range testCases {
		result := formatBytes(tc.bytes)
		if result != tc.expected {
			t.Errorf("formatBytes(%d) = %s, expected %s", tc.bytes, result, tc.expected)
		}
	}
}

// TestBufferUtilizationAndMemoryPressure 测试缓冲区利用率和内存压力的组合
func TestBufferUtilizationAndMemoryPressure(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:        100, // 小缓冲区
		FlushInterval:     1 * time.Hour,
		WorkerPoolSize:    2,
		TaskQueueSize:     10,
		MemoryThreshold:   512 * 1024 * 1024, // 512MB
		EnableMemoryFlush: true,
	}

	// 测试1：缓冲区接近满（应触发刷新）
	t.Run("buffer_near_full", func(t *testing.T) {
		cb1 := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

		// 添加数据直到接近满
		for i := 0; i < 95; i++ {
			row := DataRow{
				ID:        "test-id",
				Timestamp: time.Now().UnixNano(),
				Payload:   "test data",
			}
			cb1.Add(ctx, row)
		}

		// 检查缓冲区统计
		stats := cb1.GetStats(ctx)
		t.Logf("Buffer size: %d/%d", stats.BufferSize, config.BufferSize)

		// 触发检查
		cb1.checkAdaptiveFlush(ctx)
	})

	// 测试2：正常负载（不应触发刷新）
	t.Run("normal_load", func(t *testing.T) {
		cb2 := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

		// 添加少量数据
		for i := 0; i < 10; i++ {
			row := DataRow{
				ID:        "test-id",
				Timestamp: time.Now().UnixNano(),
				Payload:   "test data",
			}
			cb2.Add(ctx, row)
		}

		stats := cb2.GetStats(ctx)
		t.Logf("Buffer size: %d/%d (normal load)", stats.BufferSize, config.BufferSize)

		// 触发检查
		cb2.checkAdaptiveFlush(ctx)
	})
}

// TestAdaptiveFlushWithQueryActivity 测试查询活动对自适应刷新的影响
func TestAdaptiveFlushWithQueryActivity(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:        100,
		FlushInterval:     1 * time.Hour,
		WorkerPoolSize:    2,
		TaskQueueSize:     10,
		MemoryThreshold:   512 * 1024 * 1024,
		EnableMemoryFlush: true,
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	// 添加数据到80%
	for i := 0; i < 85; i++ {
		row := DataRow{
			ID:        "test-id",
			Timestamp: time.Now().UnixNano(),
			Payload:   "test data",
		}
		cb.Add(ctx, row)
	}

	// 模拟频繁查询（通过GetBufferData）
	for i := 0; i < 15; i++ {
		cb.GetBufferData(ctx, "test-key")
	}

	stats := cb.GetStats(ctx)
	t.Logf("Buffer utilization: %d/%d", stats.BufferSize, config.BufferSize)
	t.Logf("Query count: %d", cb.queryCount)

	// 高利用率 + 频繁查询应该触发刷新
	cb.checkAdaptiveFlush(ctx)
}

// BenchmarkCheckAdaptiveFlush 性能测试
func BenchmarkCheckAdaptiveFlush(b *testing.B) {
	ctx := context.Background()
	config := DefaultConcurrentBufferConfig()
	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.checkAdaptiveFlush(ctx)
	}
}
