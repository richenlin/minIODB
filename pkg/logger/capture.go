package logger

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"

	"minIODB/internal/dashboard/logbuffer"
	"minIODB/internal/dashboard/model"
	"minIODB/internal/dashboard/sse"
)

var _ zapcore.WriteSyncer = (*CaptureWriter)(nil)

type CaptureWriter struct {
	underlying zapcore.WriteSyncer
	buffer     *logbuffer.LogBuffer
	hub        *sse.Hub
	mu         sync.RWMutex
	lineBuf    bytes.Buffer
	bufMu      sync.Mutex
}

func NewCaptureWriter(underlying zapcore.WriteSyncer, buffer *logbuffer.LogBuffer, hub *sse.Hub) *CaptureWriter {
	return &CaptureWriter{
		underlying: underlying,
		buffer:     buffer,
		hub:        hub,
	}
}

func (w *CaptureWriter) SetHub(hub *sse.Hub) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hub = hub
}

func (w *CaptureWriter) Write(p []byte) (n int, err error) {
	if w.underlying != nil {
		n, err = w.underlying.Write(p)
	}

	if len(p) == 0 {
		return n, err
	}

	w.bufMu.Lock()
	defer w.bufMu.Unlock()

	for _, b := range p {
		if b == '\n' {
			w.processLine(w.lineBuf.Bytes())
			w.lineBuf.Reset()
		} else {
			w.lineBuf.WriteByte(b)
		}
	}

	return n, err
}

func (w *CaptureWriter) processLine(data []byte) {
	if len(data) == 0 || data[0] != '{' {
		return
	}

	entry, parseErr := w.parseLogEntry(data)
	if parseErr == nil && entry != nil {
		if w.buffer != nil {
			w.buffer.Add(*entry)
		}
		w.mu.RLock()
		hub := w.hub
		w.mu.RUnlock()
		if hub != nil {
			hub.Publish("logs", entry)
		}
	}
}

func (w *CaptureWriter) Sync() error {
	w.bufMu.Lock()
	if w.lineBuf.Len() > 0 {
		w.processLine(w.lineBuf.Bytes())
		w.lineBuf.Reset()
	}
	w.bufMu.Unlock()

	if w.underlying != nil {
		return w.underlying.Sync()
	}
	return nil
}

func (w *CaptureWriter) parseLogEntry(data []byte) (*model.LogEntry, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	entry := &model.LogEntry{
		Fields: make(map[string]interface{}),
	}

	for key, value := range raw {
		lowerKey := strings.ToLower(key)
		switch lowerKey {
		case "level":
			if s, ok := value.(string); ok {
				entry.Level = strings.ToLower(s)
			}
		case "ts":
			entry.Timestamp = parseTimestamp(value)
		case "timestamp":
			if entry.Timestamp == 0 {
				entry.Timestamp = parseTimestamp(value)
			}
		case "msg", "message":
			if s, ok := value.(string); ok {
				entry.Message = s
			}
		default:
			entry.Fields[key] = value
		}
	}

	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().Unix()
	}

	return entry, nil
}

func parseTimestamp(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case string:
		if ts, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(ts)
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Unix()
		}
		if t, err := time.Parse("2006-01-02T15:04:05.000Z0700", v); err == nil {
			return t.Unix()
		}
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
