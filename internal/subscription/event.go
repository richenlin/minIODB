package subscription

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EventType 事件类型
type EventType string

const (
	EventTypeInsert EventType = "insert" // 插入新记录
	EventTypeUpdate EventType = "update" // 更新记录
	EventTypeDelete EventType = "delete" // 删除记录
	EventTypeBatch  EventType = "batch"  // 批量操作
)

// Priority 事件优先级
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// DataRecord 数据记录
type DataRecord struct {
	ID        string                 `json:"id"`                   // 记录 ID
	Timestamp int64                  `json:"timestamp"`            // 时间戳（毫秒）
	Payload   map[string]interface{} `json:"payload"`              // 数据负载
	Version   int64                  `json:"version,omitempty"`    // 版本号（用于更新）
	DeletedAt *int64                 `json:"deleted_at,omitempty"` // 删除时间（用于软删除）
}

// EventMetadata 事件元数据
type EventMetadata struct {
	Source       string            `json:"source,omitempty"`        // 数据来源（如 web_app, mobile_app）
	BatchID      string            `json:"batch_id,omitempty"`      // 批次 ID
	PartitionKey string            `json:"partition_key,omitempty"` // 分区键（用于 Kafka）
	Priority     Priority          `json:"priority,omitempty"`      // 优先级
	TraceID      string            `json:"trace_id,omitempty"`      // 追踪 ID（分布式追踪）
	Tags         map[string]string `json:"tags,omitempty"`          // 自定义标签
	RetryCount   int               `json:"retry_count,omitempty"`   // 重试次数
}

// DataEvent 数据事件（订阅数据流格式）
type DataEvent struct {
	EventID   string        `json:"event_id"`   // 事件唯一 ID
	EventType EventType     `json:"event_type"` // 事件类型
	Table     string        `json:"table"`      // 目标表名
	Timestamp time.Time     `json:"timestamp"`  // 事件时间
	Records   []DataRecord  `json:"records"`    // 数据记录
	Metadata  EventMetadata `json:"metadata"`   // 元数据
}

// NewDataEvent 创建新的数据事件
func NewDataEvent(eventType EventType, table string, records []DataRecord) *DataEvent {
	return &DataEvent{
		EventID:   generateEventID(),
		EventType: eventType,
		Table:     table,
		Timestamp: time.Now().UTC(),
		Records:   records,
		Metadata: EventMetadata{
			Priority: PriorityNormal,
		},
	}
}

// NewInsertEvent 创建插入事件
func NewInsertEvent(table string, record DataRecord) *DataEvent {
	return NewDataEvent(EventTypeInsert, table, []DataRecord{record})
}

// NewBatchInsertEvent 创建批量插入事件
func NewBatchInsertEvent(table string, records []DataRecord) *DataEvent {
	return NewDataEvent(EventTypeBatch, table, records)
}

// NewUpdateEvent 创建更新事件
func NewUpdateEvent(table string, record DataRecord) *DataEvent {
	return NewDataEvent(EventTypeUpdate, table, []DataRecord{record})
}

// NewDeleteEvent 创建删除事件
func NewDeleteEvent(table string, recordID string) *DataEvent {
	now := time.Now().UnixMilli()
	return NewDataEvent(EventTypeDelete, table, []DataRecord{
		{ID: recordID, Timestamp: now, DeletedAt: &now},
	})
}

// WithSource 设置数据来源
func (e *DataEvent) WithSource(source string) *DataEvent {
	e.Metadata.Source = source
	return e
}

// WithBatchID 设置批次 ID
func (e *DataEvent) WithBatchID(batchID string) *DataEvent {
	e.Metadata.BatchID = batchID
	return e
}

// WithPartitionKey 设置分区键
func (e *DataEvent) WithPartitionKey(key string) *DataEvent {
	e.Metadata.PartitionKey = key
	return e
}

// WithPriority 设置优先级
func (e *DataEvent) WithPriority(priority Priority) *DataEvent {
	e.Metadata.Priority = priority
	return e
}

// WithTraceID 设置追踪 ID
func (e *DataEvent) WithTraceID(traceID string) *DataEvent {
	e.Metadata.TraceID = traceID
	return e
}

// WithTags 设置标签
func (e *DataEvent) WithTags(tags map[string]string) *DataEvent {
	e.Metadata.Tags = tags
	return e
}

// ToJSON 序列化为 JSON
func (e *DataEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ToJSONString 序列化为 JSON 字符串
func (e *DataEvent) ToJSONString() (string, error) {
	data, err := e.ToJSON()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FromJSON 从 JSON 反序列化
func FromJSON(data []byte) (*DataEvent, error) {
	var event DataEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DataEvent: %w", err)
	}
	return &event, nil
}

// FromJSONString 从 JSON 字符串反序列化
func FromJSONString(data string) (*DataEvent, error) {
	return FromJSON([]byte(data))
}

// Validate 验证事件有效性
func (e *DataEvent) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("event_id is required")
	}
	if e.Table == "" {
		return fmt.Errorf("table is required")
	}
	if e.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	if len(e.Records) == 0 {
		return fmt.Errorf("records cannot be empty")
	}

	// 验证每条记录
	for i, record := range e.Records {
		if record.ID == "" {
			return fmt.Errorf("record[%d].id is required", i)
		}
	}

	return nil
}

// RecordCount 返回记录数量
func (e *DataEvent) RecordCount() int {
	return len(e.Records)
}

// IsBatch 判断是否为批量事件
func (e *DataEvent) IsBatch() bool {
	return e.EventType == EventTypeBatch || len(e.Records) > 1
}

// GetPartitionKey 获取分区键，如果未设置则使用表名
func (e *DataEvent) GetPartitionKey() string {
	if e.Metadata.PartitionKey != "" {
		return e.Metadata.PartitionKey
	}
	return e.Table
}

// generateEventID 生成事件 ID
func generateEventID() string {
	return fmt.Sprintf("evt_%s", uuid.New().String()[:12])
}

// StreamKey Redis Stream key 生成
func (e *DataEvent) StreamKey(prefix string) string {
	return fmt.Sprintf("%s%s", prefix, e.Table)
}

// KafkaTopic Kafka topic 生成
func (e *DataEvent) KafkaTopic(prefix string) string {
	return fmt.Sprintf("%s%s", prefix, e.Table)
}

// Clone 克隆事件（深拷贝）
func (e *DataEvent) Clone() *DataEvent {
	clone := &DataEvent{
		EventID:   e.EventID,
		EventType: e.EventType,
		Table:     e.Table,
		Timestamp: e.Timestamp,
		Records:   make([]DataRecord, len(e.Records)),
		Metadata:  e.Metadata,
	}

	for i, r := range e.Records {
		clone.Records[i] = DataRecord{
			ID:        r.ID,
			Timestamp: r.Timestamp,
			Version:   r.Version,
			DeletedAt: r.DeletedAt,
		}
		if r.Payload != nil {
			clone.Records[i].Payload = make(map[string]interface{})
			for k, v := range r.Payload {
				clone.Records[i].Payload[k] = v
			}
		}
	}

	if e.Metadata.Tags != nil {
		clone.Metadata.Tags = make(map[string]string)
		for k, v := range e.Metadata.Tags {
			clone.Metadata.Tags[k] = v
		}
	}

	return clone
}
