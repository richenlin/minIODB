package subscription

import (
	"context"
	"fmt"
	"sync"
)

// SubscriberType 订阅者类型
type SubscriberType string

const (
	SubscriberTypeRedis SubscriberType = "redis"
	SubscriberTypeKafka SubscriberType = "kafka"
)

// SubscriberStatus 订阅者状态
type SubscriberStatus string

const (
	StatusStopped  SubscriberStatus = "stopped"
	StatusStarting SubscriberStatus = "starting"
	StatusRunning  SubscriberStatus = "running"
	StatusStopping SubscriberStatus = "stopping"
	StatusError    SubscriberStatus = "error"
)

// EventHandler 事件处理函数
type EventHandler func(ctx context.Context, event *DataEvent) error

// ErrorHandler 错误处理函数
type ErrorHandler func(event *DataEvent, err error)

// Subscriber 订阅者接口
type Subscriber interface {
	// Type 返回订阅者类型
	Type() SubscriberType

	// Start 启动订阅
	Start(ctx context.Context) error

	// Stop 停止订阅
	Stop(ctx context.Context) error

	// Status 获取状态
	Status() SubscriberStatus

	// Publish 发布事件到订阅通道
	Publish(ctx context.Context, event *DataEvent) error

	// PublishBatch 批量发布事件
	PublishBatch(ctx context.Context, events []*DataEvent) error

	// Subscribe 订阅事件（消费端）
	Subscribe(ctx context.Context, tables []string, handler EventHandler) error

	// Unsubscribe 取消订阅
	Unsubscribe(ctx context.Context, tables []string) error

	// SetErrorHandler 设置错误处理器
	SetErrorHandler(handler ErrorHandler)

	// Stats 获取统计信息
	Stats() *SubscriberStats
}

// SubscriberStats 订阅者统计信息
type SubscriberStats struct {
	Type             SubscriberType   `json:"type"`
	Status           SubscriberStatus `json:"status"`
	PublishedEvents  int64            `json:"published_events"`
	ConsumedEvents   int64            `json:"consumed_events"`
	FailedEvents     int64            `json:"failed_events"`
	PendingEvents    int64            `json:"pending_events"`
	LastPublishTime  int64            `json:"last_publish_time"`
	LastConsumeTime  int64            `json:"last_consume_time"`
	LastErrorTime    int64            `json:"last_error_time,omitempty"`
	LastError        string           `json:"last_error,omitempty"`
	SubscribedTables []string         `json:"subscribed_tables"`
}

// BaseSubscriber 基础订阅者实现（提供公共功能）
type BaseSubscriber struct {
	subscriberType SubscriberType
	status         SubscriberStatus
	errorHandler   ErrorHandler
	stats          *SubscriberStats
	mu             sync.RWMutex
}

// NewBaseSubscriber 创建基础订阅者
func NewBaseSubscriber(subType SubscriberType) *BaseSubscriber {
	return &BaseSubscriber{
		subscriberType: subType,
		status:         StatusStopped,
		stats: &SubscriberStats{
			Type:             subType,
			Status:           StatusStopped,
			SubscribedTables: []string{},
		},
	}
}

// Type 返回订阅者类型
func (b *BaseSubscriber) Type() SubscriberType {
	return b.subscriberType
}

// Status 获取状态
func (b *BaseSubscriber) Status() SubscriberStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

// SetStatus 设置状态
func (b *BaseSubscriber) SetStatus(status SubscriberStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = status
	b.stats.Status = status
}

// SetErrorHandler 设置错误处理器
func (b *BaseSubscriber) SetErrorHandler(handler ErrorHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errorHandler = handler
}

// HandleError 处理错误
func (b *BaseSubscriber) HandleError(event *DataEvent, err error) {
	b.mu.RLock()
	handler := b.errorHandler
	b.mu.RUnlock()

	if handler != nil {
		handler(event, err)
	}
}

// Stats 获取统计信息
func (b *BaseSubscriber) Stats() *SubscriberStats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// 返回副本
	stats := *b.stats
	return &stats
}

// IncrPublished 增加已发布计数
func (b *BaseSubscriber) IncrPublished(count int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.PublishedEvents += count
}

// IncrConsumed 增加已消费计数
func (b *BaseSubscriber) IncrConsumed(count int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.ConsumedEvents += count
}

// IncrFailed 增加失败计数
func (b *BaseSubscriber) IncrFailed(count int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.FailedEvents += count
}

// SetLastError 设置最后错误
func (b *BaseSubscriber) SetLastError(err error, timestamp int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.LastError = err.Error()
	b.stats.LastErrorTime = timestamp
}

// AddSubscribedTable 添加订阅的表
func (b *BaseSubscriber) AddSubscribedTable(table string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range b.stats.SubscribedTables {
		if t == table {
			return
		}
	}
	b.stats.SubscribedTables = append(b.stats.SubscribedTables, table)
}

// RemoveSubscribedTable 移除订阅的表
func (b *BaseSubscriber) RemoveSubscribedTable(table string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	tables := make([]string, 0, len(b.stats.SubscribedTables))
	for _, t := range b.stats.SubscribedTables {
		if t != table {
			tables = append(tables, t)
		}
	}
	b.stats.SubscribedTables = tables
}

// SubscriberOption 订阅者配置选项
type SubscriberOption func(*SubscriberOptions)

// SubscriberOptions 订阅者选项
type SubscriberOptions struct {
	BatchSize     int
	MaxRetries    int
	RetryDelay    int64 // 毫秒
	BlockTimeout  int64 // 毫秒
	EnableAutoAck bool
	ConsumerGroup string
	ConsumerName  string
}

// DefaultSubscriberOptions 默认订阅者选项
func DefaultSubscriberOptions() *SubscriberOptions {
	return &SubscriberOptions{
		BatchSize:     100,
		MaxRetries:    3,
		RetryDelay:    1000,
		BlockTimeout:  5000,
		EnableAutoAck: true,
		ConsumerGroup: "miniodb-workers",
		ConsumerName:  "worker-1",
	}
}

// WithBatchSize 设置批量大小
func WithBatchSize(size int) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.BatchSize = size
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(retries int) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.MaxRetries = retries
	}
}

// WithRetryDelay 设置重试延迟
func WithRetryDelay(delayMs int64) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.RetryDelay = delayMs
	}
}

// WithBlockTimeout 设置阻塞超时
func WithBlockTimeout(timeoutMs int64) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.BlockTimeout = timeoutMs
	}
}

// WithAutoAck 设置自动确认
func WithAutoAck(enable bool) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.EnableAutoAck = enable
	}
}

// WithConsumerGroup 设置消费者组
func WithConsumerGroup(group string) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.ConsumerGroup = group
	}
}

// WithConsumerName 设置消费者名称
func WithConsumerName(name string) SubscriberOption {
	return func(o *SubscriberOptions) {
		o.ConsumerName = name
	}
}

// ErrSubscriberNotRunning 订阅者未运行错误
var ErrSubscriberNotRunning = fmt.Errorf("subscriber is not running")

// ErrSubscriberAlreadyRunning 订阅者已运行错误
var ErrSubscriberAlreadyRunning = fmt.Errorf("subscriber is already running")

// ErrInvalidEvent 无效事件错误
var ErrInvalidEvent = fmt.Errorf("invalid event")

// ErrPublishFailed 发布失败错误
type ErrPublishFailed struct {
	Event *DataEvent
	Cause error
}

func (e *ErrPublishFailed) Error() string {
	return fmt.Sprintf("failed to publish event %s: %v", e.Event.EventID, e.Cause)
}

func (e *ErrPublishFailed) Unwrap() error {
	return e.Cause
}

// ErrConsumeFailed 消费失败错误
type ErrConsumeFailed struct {
	Event *DataEvent
	Cause error
}

func (e *ErrConsumeFailed) Error() string {
	if e.Event != nil {
		return fmt.Sprintf("failed to consume event %s: %v", e.Event.EventID, e.Cause)
	}
	return fmt.Sprintf("failed to consume event: %v", e.Cause)
}

func (e *ErrConsumeFailed) Unwrap() error {
	return e.Cause
}
