package buffer

import "context"

// CacheInvalidator 缓存失效通知接口
// 当缓冲区数据刷新到存储后，通过此接口通知查询层失效相关缓存
type CacheInvalidator interface {
	// InvalidateTable 使指定表的缓存失效
	InvalidateTable(ctx context.Context, tableName string) error
}

// NoOpCacheInvalidator 空操作的缓存失效器（用于测试或禁用缓存失效）
type NoOpCacheInvalidator struct{}

func (n *NoOpCacheInvalidator) InvalidateTable(ctx context.Context, tableName string) error {
	return nil
}
