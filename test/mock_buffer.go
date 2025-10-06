package test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"minIODB/internal/buffer"
)

// MockConcurrentBuffer 是用于测试的Mock缓冲区实现
type MockConcurrentBuffer struct {
	data       map[string][]buffer.DataRow
	mutex      sync.RWMutex
	stats      *buffer.ConcurrentBufferStats
	flushCount int
}

// NewMockConcurrentBuffer 创建新的Mock缓冲区
func NewMockConcurrentBuffer() *MockConcurrentBuffer {
	return &MockConcurrentBuffer{
		data:  make(map[string][]buffer.DataRow),
		stats: &buffer.ConcurrentBufferStats{},
	}
}

// Add 添加数据到缓冲区
func (m *MockConcurrentBuffer) Add(ctx context.Context, row buffer.DataRow) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	t := time.Unix(0, row.Timestamp)
	dayStr := t.Format("2006-01-02")
	tableName := row.Table
	if tableName == "" {
		tableName = "default_table"
	}
	bufferKey := fmt.Sprintf("%s/%s/%s", tableName, row.ID, dayStr)

	m.data[bufferKey] = append(m.data[bufferKey], row)
	m.updateStats(func(stats *buffer.ConcurrentBufferStats) {
		stats.BufferSize = int64(len(m.data))
		totalPending := int64(0)
		for _, rows := range m.data {
			totalPending += int64(len(rows))
		}
		stats.PendingWrites = totalPending
	})
}

// Get 获取缓冲区数据
func (m *MockConcurrentBuffer) Get(ctx context.Context, key string) []buffer.DataRow {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	rows := make([]buffer.DataRow, len(m.data[key]))
	copy(rows, m.data[key])
	return rows
}

// GetAllKeys 获取所有缓冲区键
func (m *MockConcurrentBuffer) GetAllKeys(ctx context.Context) []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	keys := make([]string, 0, len(m.data))
	for key := range m.data {
		keys = append(keys, key)
	}
	return keys
}

// GetTableKeys 获取指定表的所有缓冲区键
func (m *MockConcurrentBuffer) GetTableKeys(ctx context.Context, tableName string) []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	prefix := tableName + "/"
	keys := make([]string, 0)
	for key := range m.data {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys
}

// GetBufferData 获取指定键的缓冲区数据
func (m *MockConcurrentBuffer) GetBufferData(ctx context.Context, key string) []buffer.DataRow {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	rows, exists := m.data[key]
	if !exists {
		return []buffer.DataRow{}
	}

	result := make([]buffer.DataRow, len(rows))
	copy(result, rows)
	return result
}

// GetStats 获取统计信息
func (m *MockConcurrentBuffer) GetStats(ctx context.Context) *buffer.ConcurrentBufferStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return &buffer.ConcurrentBufferStats{
		TotalTasks:     m.stats.TotalTasks,
		CompletedTasks: m.stats.CompletedTasks,
		FailedTasks:    m.stats.FailedTasks,
		QueuedTasks:    m.stats.QueuedTasks,
		ActiveWorkers:  m.stats.ActiveWorkers,
		AvgFlushTime:   m.stats.AvgFlushTime,
		TotalFlushTime: m.stats.TotalFlushTime,
		LastFlushTime:  m.stats.LastFlushTime,
		BufferSize:     m.stats.BufferSize,
		PendingWrites:  m.stats.PendingWrites,
	}
}

// updateStats 更新统计信息
func (m *MockConcurrentBuffer) updateStats(updater func(*buffer.ConcurrentBufferStats)) {
	updater(m.stats)
}

// FlushDataPoints 手动刷新所有缓冲区
func (m *MockConcurrentBuffer) FlushDataPoints(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 模拟刷新操作
	m.flushCount++
	m.data = make(map[string][]buffer.DataRow)
	m.updateStats(func(stats *buffer.ConcurrentBufferStats) {
		stats.CompletedTasks++
		stats.BufferSize = 0
		stats.PendingWrites = 0
		stats.LastFlushTime = time.Now().Unix()
	})
	return nil
}

// Stop 停止缓冲区
func (m *MockConcurrentBuffer) Stop(ctx context.Context) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.data = make(map[string][]buffer.DataRow)
}

// SetCacheInvalidator 设置缓存失效器
func (m *MockConcurrentBuffer) SetCacheInvalidator(invalidator buffer.CacheInvalidator) {
	// Mock实现，不需要实际操作
}

// WriteTempParquetFile 写入临时Parquet文件
func (m *MockConcurrentBuffer) WriteTempParquetFile(ctx context.Context, filePath string, rows []buffer.DataRow) error {
	// Mock实现，直接返回成功
	return nil
}

// InvalidateTableConfig 使表配置缓存失效
func (m *MockConcurrentBuffer) InvalidateTableConfig(ctx context.Context, tableName string) {
	// Mock实现，不需要实际操作
}

// GetTableBufferKeys 获取指定表的所有缓冲区键
func (m *MockConcurrentBuffer) GetTableBufferKeys(ctx context.Context, tableName string) []string {
	return m.GetTableKeys(ctx, tableName)
}

// AddDataPoint 添加数据点到缓冲区
func (m *MockConcurrentBuffer) AddDataPoint(ctx context.Context, id string, data []byte, timestamp time.Time) {
	row := buffer.DataRow{
		ID:        id,
		Timestamp: timestamp.UnixNano(),
		Payload:   string(data),
	}
	m.Add(ctx, row)
}

// GetMemoryStats 获取内存优化统计
func (m *MockConcurrentBuffer) GetMemoryStats(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{
		"enabled": false,
		"message": "Memory optimizer not initialized in mock",
	}
}

// GetFlushCount 获取刷新次数（用于测试验证）
func (m *MockConcurrentBuffer) GetFlushCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.flushCount
}

// Size 获取缓冲区大小
func (m *MockConcurrentBuffer) Size(ctx context.Context) int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.data)
}

// PendingWrites 获取待写入数据数量
func (m *MockConcurrentBuffer) PendingWrites(ctx context.Context) int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	total := 0
	for _, rows := range m.data {
		total += len(rows)
	}
	return total
}
