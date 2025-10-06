package buffer

import (
	"context"
	"time"
)

// BufferInterface 定义了缓冲区的接口
type BufferInterface interface {
	// 基本操作
	Add(ctx context.Context, row DataRow)
	Get(ctx context.Context, key string) []DataRow
	GetAllKeys(ctx context.Context) []string
	GetTableKeys(ctx context.Context, tableName string) []string
	GetBufferData(ctx context.Context, key string) []DataRow
	GetStats(ctx context.Context) *ConcurrentBufferStats
	Stop(ctx context.Context)

	// 刷新操作
	FlushDataPoints(ctx context.Context) error

	// 缓存失效
	SetCacheInvalidator(invalidator CacheInvalidator)
	InvalidateTableConfig(ctx context.Context, tableName string)

	// 文件操作
	WriteTempParquetFile(ctx context.Context, filePath string, rows []DataRow) error

	// 兼容性方法
	GetTableBufferKeys(ctx context.Context, tableName string) []string
	AddDataPoint(ctx context.Context, id string, data []byte, timestamp time.Time)
	GetMemoryStats(ctx context.Context) map[string]interface{}

	// 辅助方法
	Size(ctx context.Context) int
	PendingWrites(ctx context.Context) int
}

// 确保ConcurrentBuffer实现了BufferInterface接口
var _ BufferInterface = (*ConcurrentBuffer)(nil)
