package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"minIODB/pkg/logger"

	"go.uber.org/zap"
)

// Manager 订阅管理器
type Manager struct {
	subscribers map[SubscriberType]Subscriber
	handlers    map[string]EventHandler // table -> handler
	mu          sync.RWMutex
	running     bool
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewManager 创建订阅管理器
func NewManager() *Manager {
	return &Manager{
		subscribers: make(map[SubscriberType]Subscriber),
		handlers:    make(map[string]EventHandler),
	}
}

// RegisterSubscriber 注册订阅者
func (m *Manager) RegisterSubscriber(subscriber Subscriber) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subType := subscriber.Type()
	if _, exists := m.subscribers[subType]; exists {
		return fmt.Errorf("subscriber type %s already registered", subType)
	}

	m.subscribers[subType] = subscriber
	logger.GetLogger().Info("Registered subscriber",
		zap.String("type", string(subType)))
	return nil
}

// UnregisterSubscriber 注销订阅者
func (m *Manager) UnregisterSubscriber(subType SubscriberType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subscriber, exists := m.subscribers[subType]
	if !exists {
		return fmt.Errorf("subscriber type %s not found", subType)
	}

	// 停止订阅者
	if subscriber.Status() == StatusRunning {
		if err := subscriber.Stop(context.Background()); err != nil {
			logger.GetLogger().Warn("Failed to stop subscriber before unregistering",
				zap.String("type", string(subType)),
				zap.Error(err))
		}
	}

	delete(m.subscribers, subType)
	logger.GetLogger().Info("Unregistered subscriber",
		zap.String("type", string(subType)))
	return nil
}

// GetSubscriber 获取订阅者
func (m *Manager) GetSubscriber(subType SubscriberType) (Subscriber, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subscriber, exists := m.subscribers[subType]
	if !exists {
		return nil, fmt.Errorf("subscriber type %s not found", subType)
	}
	return subscriber, nil
}

// Start 启动所有订阅者
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("manager is already running")
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.running = true
	m.mu.Unlock()

	var errs []error
	for subType, subscriber := range m.subscribers {
		if err := subscriber.Start(m.ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to start %s subscriber: %w", subType, err))
			logger.GetLogger().Error("Failed to start subscriber",
				zap.String("type", string(subType)),
				zap.Error(err))
		} else {
			logger.GetLogger().Info("Started subscriber",
				zap.String("type", string(subType)))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to start some subscribers: %v", errs)
	}

	logger.GetLogger().Info("Subscription manager started",
		zap.Int("subscriber_count", len(m.subscribers)))
	return nil
}

// Stop 停止所有订阅者
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	var errs []error
	for subType, subscriber := range m.subscribers {
		if err := subscriber.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop %s subscriber: %w", subType, err))
			logger.GetLogger().Error("Failed to stop subscriber",
				zap.String("type", string(subType)),
				zap.Error(err))
		} else {
			logger.GetLogger().Info("Stopped subscriber",
				zap.String("type", string(subType)))
		}
	}

	m.wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop some subscribers: %v", errs)
	}

	logger.GetLogger().Info("Subscription manager stopped")
	return nil
}

// Publish 发布事件到指定类型的订阅者
func (m *Manager) Publish(ctx context.Context, subType SubscriberType, event *DataEvent) error {
	subscriber, err := m.GetSubscriber(subType)
	if err != nil {
		return err
	}

	if subscriber.Status() != StatusRunning {
		return fmt.Errorf("subscriber %s is not running", subType)
	}

	return subscriber.Publish(ctx, event)
}

// PublishToAll 发布事件到所有订阅者
func (m *Manager) PublishToAll(ctx context.Context, event *DataEvent) error {
	m.mu.RLock()
	subscribers := make([]Subscriber, 0, len(m.subscribers))
	for _, s := range m.subscribers {
		subscribers = append(subscribers, s)
	}
	m.mu.RUnlock()

	var errs []error
	for _, subscriber := range subscribers {
		if subscriber.Status() == StatusRunning {
			if err := subscriber.Publish(ctx, event); err != nil {
				errs = append(errs, fmt.Errorf("failed to publish to %s: %w", subscriber.Type(), err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to publish to some subscribers: %v", errs)
	}
	return nil
}

// PublishBatch 批量发布事件到指定类型的订阅者
func (m *Manager) PublishBatch(ctx context.Context, subType SubscriberType, events []*DataEvent) error {
	subscriber, err := m.GetSubscriber(subType)
	if err != nil {
		return err
	}

	if subscriber.Status() != StatusRunning {
		return fmt.Errorf("subscriber %s is not running", subType)
	}

	return subscriber.PublishBatch(ctx, events)
}

// Subscribe 订阅表的事件
func (m *Manager) Subscribe(ctx context.Context, subType SubscriberType, tables []string, handler EventHandler) error {
	subscriber, err := m.GetSubscriber(subType)
	if err != nil {
		return err
	}

	// 存储 handler 映射
	m.mu.Lock()
	for _, table := range tables {
		m.handlers[table] = handler
	}
	m.mu.Unlock()

	return subscriber.Subscribe(ctx, tables, handler)
}

// Unsubscribe 取消订阅
func (m *Manager) Unsubscribe(ctx context.Context, subType SubscriberType, tables []string) error {
	subscriber, err := m.GetSubscriber(subType)
	if err != nil {
		return err
	}

	// 移除 handler 映射
	m.mu.Lock()
	for _, table := range tables {
		delete(m.handlers, table)
	}
	m.mu.Unlock()

	return subscriber.Unsubscribe(ctx, tables)
}

// Stats 获取所有订阅者的统计信息
func (m *Manager) Stats() map[SubscriberType]*SubscriberStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[SubscriberType]*SubscriberStats)
	for subType, subscriber := range m.subscribers {
		stats[subType] = subscriber.Stats()
	}
	return stats
}

// IsRunning 检查管理器是否运行中
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// SubscriberCount 返回注册的订阅者数量
func (m *Manager) SubscriberCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subscribers)
}

// DefaultEventHandler 默认事件处理器（转发到缓冲区）
type DefaultEventHandler struct {
	bufferWriter BufferWriter
}

// BufferWriter 缓冲区写入接口
type BufferWriter interface {
	Add(table string, id string, timestamp int64, payload string) error
}

// NewDefaultEventHandler 创建默认事件处理器
func NewDefaultEventHandler(writer BufferWriter) *DefaultEventHandler {
	return &DefaultEventHandler{
		bufferWriter: writer,
	}
}

// Handle 处理事件
func (h *DefaultEventHandler) Handle(ctx context.Context, event *DataEvent) error {
	if h.bufferWriter == nil {
		return fmt.Errorf("buffer writer not configured")
	}

	for _, record := range event.Records {
		// 将 payload 转换为 JSON 字符串
		payloadJSON, err := jsonMarshal(record.Payload)
		if err != nil {
			logger.GetLogger().Error("Failed to marshal payload",
				zap.String("event_id", event.EventID),
				zap.String("record_id", record.ID),
				zap.Error(err))
			continue
		}

		if err := h.bufferWriter.Add(event.Table, record.ID, record.Timestamp, string(payloadJSON)); err != nil {
			logger.GetLogger().Error("Failed to add record to buffer",
				zap.String("event_id", event.EventID),
				zap.String("record_id", record.ID),
				zap.Error(err))
			return err
		}
	}

	logger.GetLogger().Debug("Processed event",
		zap.String("event_id", event.EventID),
		zap.String("table", event.Table),
		zap.Int("record_count", len(event.Records)))

	return nil
}

// jsonMarshal JSON 序列化辅助函数
func jsonMarshal(v interface{}) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(v)
}

// HealthCheck 健康检查
func (m *Manager) HealthCheck(ctx context.Context) map[SubscriberType]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[SubscriberType]bool)
	for subType, subscriber := range m.subscribers {
		status := subscriber.Status()
		health[subType] = status == StatusRunning
	}
	return health
}

// WaitForReady 等待所有订阅者就绪
func (m *Manager) WaitForReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for subscribers to be ready")
		}

		allReady := true
		m.mu.RLock()
		for _, subscriber := range m.subscribers {
			if subscriber.Status() != StatusRunning {
				allReady = false
				break
			}
		}
		m.mu.RUnlock()

		if allReady {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// 继续等待
		}
	}
}
