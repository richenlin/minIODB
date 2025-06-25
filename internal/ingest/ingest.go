package ingest

import (
	"fmt"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"

	"google.golang.org/protobuf/encoding/protojson"
)

// Ingester handles converting gRPC requests and adding them to the shared buffer.
type Ingester struct {
	buffer *buffer.SharedBuffer
}

// NewIngester creates a new Ingester.
func NewIngester(buf *buffer.SharedBuffer) *Ingester {
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

	row := buffer.DataRow{
		ID:        req.Id,
		Timestamp: req.Timestamp.AsTime().UnixNano(),
		Payload:   string(payloadBytes),
	}

	i.buffer.Add(row)
	return nil
}
