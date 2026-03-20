package ingest

import (
	"context"
	"encoding/json"
	"fmt"

	olapv1 "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/security"

	"google.golang.org/protobuf/types/known/structpb"
)

// Ingester handles converting gRPC requests and adding them to the shared buffer.
type Ingester struct {
	buffer            *buffer.ConcurrentBuffer
	encryptionManager *security.FieldEncryptionManager
}

// NewIngester creates a new Ingester.
func NewIngester(buf *buffer.ConcurrentBuffer) *Ingester {
	return &Ingester{
		buffer: buf,
	}
}

// NewIngesterWithEncryption creates a new Ingester with field encryption support.
func NewIngesterWithEncryption(buf *buffer.ConcurrentBuffer, encryptionManager *security.FieldEncryptionManager) *Ingester {
	return &Ingester{
		buffer:            buf,
		encryptionManager: encryptionManager,
	}
}

// SetEncryptionManager sets the encryption manager for the ingester.
func (i *Ingester) SetEncryptionManager(manager *security.FieldEncryptionManager) {
	i.encryptionManager = manager
}

// IngestData converts the request and adds it to the buffer.
// If field encryption is enabled, sensitive fields are encrypted before storage.
func (i *Ingester) IngestData(req *olapv1.WriteRequest) error {
	fields := structpbStructToMap(req.Payload)

	// Apply field encryption if enabled
	if i.encryptionManager != nil && i.encryptionManager.IsEnabled() {
		encryptedFields, err := i.encryptionManager.EncryptFields(fields)
		if err != nil {
			return fmt.Errorf("failed to encrypt fields: %w", err)
		}
		fields = encryptedFields
	}

	tableName := req.Table
	if tableName == "" {
		tableName = "default"
	}

	row := buffer.DataRow{
		Table:     tableName,
		ID:        req.Id,
		Timestamp: req.Timestamp.AsTime().UnixNano(),
		Fields:    fields,
	}

	if err := i.buffer.Add(row); err != nil {
		return fmt.Errorf("failed to add data to buffer: %w", err)
	}
	return nil
}

// IngestDataWithTableConfig ingests data with table-specific encryption settings.
func (i *Ingester) IngestDataWithTableConfig(req *olapv1.WriteRequest, tableEncryptionManager *security.FieldEncryptionManager) error {
	fields := structpbStructToMap(req.Payload)

	// Use table-specific encryption manager if provided
	encryptionManager := tableEncryptionManager
	if encryptionManager == nil {
		encryptionManager = i.encryptionManager
	}

	// Apply field encryption if enabled
	if encryptionManager != nil && encryptionManager.IsEnabled() {
		encryptedFields, err := encryptionManager.EncryptFields(fields)
		if err != nil {
			return fmt.Errorf("failed to encrypt fields: %w", err)
		}
		fields = encryptedFields
	}

	tableName := req.Table
	if tableName == "" {
		tableName = "default"
	}

	row := buffer.DataRow{
		Table:     tableName,
		ID:        req.Id,
		Timestamp: req.Timestamp.AsTime().UnixNano(),
		Fields:    fields,
	}

	if err := i.buffer.Add(row); err != nil {
		return fmt.Errorf("failed to add data to buffer: %w", err)
	}
	return nil
}

// structpbStructToMap converts a protobuf structpb.Struct to a Go map.
func structpbStructToMap(s *structpb.Struct) map[string]interface{} {
	if s == nil || s.Fields == nil {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	for k, v := range s.Fields {
		result[k] = structpbValueToInterface(v)
	}
	return result
}

// structpbValueToInterface converts a protobuf structpb.Value to an interface{}.
func structpbValueToInterface(v *structpb.Value) interface{} {
	if v == nil {
		return nil
	}

	switch kind := v.Kind.(type) {
	case *structpb.Value_NullValue:
		return nil
	case *structpb.Value_NumberValue:
		return kind.NumberValue
	case *structpb.Value_StringValue:
		return kind.StringValue
	case *structpb.Value_BoolValue:
		return kind.BoolValue
	case *structpb.Value_StructValue:
		return structpbStructToMap(kind.StructValue)
	case *structpb.Value_ListValue:
		if kind.ListValue == nil {
			return []interface{}{}
		}
		list := make([]interface{}, len(kind.ListValue.Values))
		for i, item := range kind.ListValue.Values {
			list[i] = structpbValueToInterface(item)
		}
		return list
	default:
		return nil
	}
}

// mapToStructpbStruct converts a Go map to a protobuf structpb.Struct.
func mapToStructpbStruct(m map[string]interface{}) (*structpb.Struct, error) {
	if m == nil {
		return &structpb.Struct{Fields: make(map[string]*structpb.Value)}, nil
	}

	fields := make(map[string]*structpb.Value)
	for k, v := range m {
		protoValue, err := interfaceToStructpbValue(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s: %w", k, err)
		}
		fields[k] = protoValue
	}
	return &structpb.Struct{Fields: fields}, nil
}

// interfaceToStructpbValue converts an interface{} to a protobuf structpb.Value.
func interfaceToStructpbValue(v interface{}) (*structpb.Value, error) {
	if v == nil {
		return structpb.NewNullValue(), nil
	}

	switch val := v.(type) {
	case bool:
		return structpb.NewBoolValue(val), nil
	case int:
		return structpb.NewNumberValue(float64(val)), nil
	case int32:
		return structpb.NewNumberValue(float64(val)), nil
	case int64:
		return structpb.NewNumberValue(float64(val)), nil
	case float32:
		return structpb.NewNumberValue(float64(val)), nil
	case float64:
		return structpb.NewNumberValue(val), nil
	case string:
		return structpb.NewStringValue(val), nil
	case []interface{}:
		values := make([]*structpb.Value, len(val))
		for i, item := range val {
			protoValue, err := interfaceToStructpbValue(item)
			if err != nil {
				return nil, err
			}
			values[i] = protoValue
		}
		return structpb.NewListValue(&structpb.ListValue{Values: values}), nil
	case map[string]interface{}:
		structVal, err := mapToStructpbStruct(val)
		if err != nil {
			return nil, err
		}
		return structpb.NewStructValue(structVal), nil
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return structpb.NewStringValue(string(val)), nil
		}
		return structpb.NewNumberValue(f), nil
	default:
		// Fallback to string representation
		return structpb.NewStringValue(fmt.Sprintf("%v", val)), nil
	}
}

// FlushBuffer 手动刷新缓冲区
func (i *Ingester) FlushBuffer() error {
	if i.buffer == nil {
		return fmt.Errorf("buffer not initialized")
	}

	// 触发手动刷新所有缓冲区
	return i.buffer.FlushDataPoints()
}

// GetBufferStats 获取缓冲区统计信息
func (i *Ingester) GetBufferStats() *buffer.ConcurrentBufferStats {
	if i.buffer == nil {
		return nil
	}
	return i.buffer.GetStats()
}

// RemoveFromBuffer 从缓冲区中移除指定记录
// 用于 DeleteData 和 UpdateData 操作，确保未 flush 的数据也能被正确删除
func (i *Ingester) RemoveFromBuffer(tableName, id string) int {
	if i.buffer == nil {
		return 0
	}
	return i.buffer.Remove(tableName, id)
}

// ClearTableBuffer 清除指定表在 buffer 中的所有数据
// 用于 DropTable 操作，确保删除表后内存中的数据也被清理
func (i *Ingester) ClearTableBuffer(ctx context.Context, tableName string) int {
	if i.buffer == nil {
		return 0
	}
	_ = ctx // ClearTable 不需要 ctx，保留参数兼容性
	return i.buffer.ClearTable(tableName)
}
