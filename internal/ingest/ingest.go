package ingest

import (
	"fmt"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"

	"google.golang.org/protobuf/encoding/protojson"
)

// Ingester handles converting gRPC requests and adding them to the shared buffer.
type Ingester struct {
	buffer *buffer.ConcurrentBuffer
}

// NewIngester creates a new Ingester.
func NewIngester(buf *buffer.ConcurrentBuffer) *Ingester {
	return &Ingester{
		buffer: buf,
	}
}

// IngestData converts the request and adds it to the buffer.
func (i *Ingester) IngestData(req *olapv1.WriteRequest) error {
	payloadBytes, err := protojson.Marshal(req.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// 使用请求中的表名，如果为空则使用默认值
	tableName := req.Table
	if tableName == "" {
		tableName = "default" // protobuf注释中说明的默认值
	}

	row := buffer.DataRow{
		Table:     tableName, // 设置表名，这样缓冲区键格式就正确了
		ID:        req.Id,
		Timestamp: req.Timestamp.AsTime().UnixNano(),
		Payload:   string(payloadBytes),
	}

	i.buffer.Add(row)
	return nil
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
