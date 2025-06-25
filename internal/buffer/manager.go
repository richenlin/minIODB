package buffer

import (
	"sync"
	"time"
)

// DataPoint 表示缓冲区中的数据点
type DataPoint struct {
	ID        string
	Data      []byte
	Timestamp time.Time
}

// FlushFunc 定义刷新函数类型
type FlushFunc func(data []DataPoint) error

// Manager 缓冲区管理器
type Manager struct {
	buffer   []DataPoint
	mutex    sync.RWMutex
	maxSize  int
	flushCh  chan struct{}
}

// NewManager 创建新的缓冲区管理器
func NewManager(maxSize int) *Manager {
	return &Manager{
		buffer:  make([]DataPoint, 0, maxSize),
		maxSize: maxSize,
		flushCh: make(chan struct{}, 1),
	}
}

// Add 添加数据点到缓冲区
func (m *Manager) Add(id string, data []byte, timestamp time.Time) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已满
	if len(m.buffer) >= m.maxSize {
		return false
	}

	// 添加数据点
	m.buffer = append(m.buffer, DataPoint{
		ID:        id,
		Data:      data,
		Timestamp: timestamp,
	})

	// 如果达到最大容量，触发刷新
	if len(m.buffer) >= m.maxSize {
		select {
		case m.flushCh <- struct{}{}:
		default:
		}
	}

	return true
}

// Flush 刷新缓冲区
func (m *Manager) Flush(flushFunc FlushFunc) error {
	m.mutex.Lock()
	if len(m.buffer) == 0 {
		m.mutex.Unlock()
		return nil
	}

	// 复制数据以避免长时间持有锁
	data := make([]DataPoint, len(m.buffer))
	copy(data, m.buffer)
	
	// 清空缓冲区
	m.buffer = m.buffer[:0]
	m.mutex.Unlock()

	// 执行刷新操作
	return flushFunc(data)
}

// Size 返回当前缓冲区大小
func (m *Manager) Size() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.buffer)
}

// IsFull 检查缓冲区是否已满
func (m *Manager) IsFull() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.buffer) >= m.maxSize
}

// Clear 清空缓冲区
func (m *Manager) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.buffer = m.buffer[:0]
}

// FlushChannel 返回刷新通道
func (m *Manager) FlushChannel() <-chan struct{} {
	return m.flushCh
} 