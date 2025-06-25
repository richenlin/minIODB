package buffer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"minIODB/internal/pool"
)

func TestDefaultConcurrentBufferConfig(t *testing.T) {
	config := DefaultConcurrentBufferConfig()
	
	assert.NotNil(t, config)
	assert.Equal(t, 1000, config.BufferSize)
	assert.Equal(t, 30*time.Second, config.FlushInterval)
	assert.Equal(t, 10, config.WorkerPoolSize)
	assert.Equal(t, 100, config.TaskQueueSize)
	assert.Equal(t, 5, config.BatchFlushSize)
	assert.True(t, config.EnableBatching)
	assert.Equal(t, 60*time.Second, config.FlushTimeout)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.RetryDelay)
}

func TestNewConcurrentBuffer_Success(t *testing.T) {
	// 创建一个简单的mock pool manager
	poolManager := &pool.PoolManager{}
	config := DefaultConcurrentBufferConfig()
	config.BufferSize = 1024
	config.FlushInterval = 100 * time.Millisecond
	config.WorkerPoolSize = 2
	
	buffer := NewConcurrentBuffer(poolManager, "", "test-node", config)
	require.NotNil(t, buffer)
	defer buffer.Stop()
	
	// 测试缓冲区是否正确初始化
	assert.Equal(t, config.BufferSize, buffer.config.BufferSize)
	assert.Equal(t, config.WorkerPoolSize, buffer.config.WorkerPoolSize)
	assert.NotNil(t, buffer.taskQueue)
}

func TestConcurrentBuffer_GetStats(t *testing.T) {
	poolManager := &pool.PoolManager{}
	config := DefaultConcurrentBufferConfig()
	config.BufferSize = 1024
	config.FlushInterval = 100 * time.Millisecond
	config.WorkerPoolSize = 2
	
	buffer := NewConcurrentBuffer(poolManager, "", "test-node", config)
	require.NotNil(t, buffer)
	defer buffer.Stop()
	
	// 获取统计信息
	stats := buffer.GetStats()
	assert.NotNil(t, stats)
	
	// 验证初始统计数据
	assert.Equal(t, int64(0), stats.TotalTasks)
	assert.Equal(t, int64(0), stats.CompletedTasks)
	assert.Equal(t, int64(0), stats.FailedTasks)
} 