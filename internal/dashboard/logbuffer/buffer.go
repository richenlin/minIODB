package logbuffer

import (
	"strings"
	"sync"

	"minIODB/internal/dashboard/model"
)

const DefaultMaxSize = 5000

type LogBuffer struct {
	entries []model.LogEntry
	head    int
	tail    int
	size    int
	maxSize int
	mu      sync.RWMutex
}

func NewBuffer(maxSize int) *LogBuffer {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	return &LogBuffer{
		entries: make([]model.LogEntry, maxSize),
		maxSize: maxSize,
	}
}

func (b *LogBuffer) Add(entry model.LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size < b.maxSize {
		b.entries[b.tail] = entry
		b.tail = (b.tail + 1) % b.maxSize
		b.size++
	} else {
		b.entries[b.tail] = entry
		b.tail = (b.tail + 1) % b.maxSize
		b.head = (b.head + 1) % b.maxSize
	}
}

func (b *LogBuffer) GetAll() []model.LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return nil
	}

	result := make([]model.LogEntry, b.size)
	for i := 0; i < b.size; i++ {
		idx := (b.head + i) % b.maxSize
		result[i] = b.entries[idx]
	}
	return result
}

func (b *LogBuffer) Query(params model.LogQueryParams) model.LogQueryResult {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return model.LogQueryResult{
			Logs:     []model.LogEntry{},
			Total:    0,
			Page:     params.Page,
			PageSize: params.PageSize,
		}
	}

	filtered := make([]model.LogEntry, 0, b.size)
	for i := 0; i < b.size; i++ {
		idx := (b.head + i) % b.maxSize
		entry := b.entries[idx]

		if !b.matchFilter(entry, params) {
			continue
		}
		filtered = append(filtered, entry)
	}

	total := int64(len(filtered))

	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 {
		pageSize = 20
	}

	start := (page - 1) * pageSize
	end := start + pageSize

	if start >= len(filtered) {
		return model.LogQueryResult{
			Logs:     []model.LogEntry{},
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		}
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	return model.LogQueryResult{
		Logs:     filtered[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
}

func (b *LogBuffer) matchFilter(entry model.LogEntry, params model.LogQueryParams) bool {
	if params.Level != "" && !strings.EqualFold(entry.Level, params.Level) {
		return false
	}

	if params.StartTime > 0 && entry.Timestamp < params.StartTime {
		return false
	}

	if params.EndTime > 0 && entry.Timestamp > params.EndTime {
		return false
	}

	if params.Keyword != "" {
		keyword := strings.ToLower(params.Keyword)
		if !strings.Contains(strings.ToLower(entry.Message), keyword) {
			return false
		}
	}

	return true
}

func (b *LogBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}
