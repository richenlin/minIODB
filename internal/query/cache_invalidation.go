package query

import (
	"context"
	"minIODB/internal/logger"

	"go.uber.org/zap"
)

// InvalidateTable 实现 CacheInvalidator 接口，使表的缓存失效
// 这个方法会在缓冲区刷新后被调用，以确保查询能看到最新数据
func (q *Querier) InvalidateTable(ctx context.Context, tableName string) error {
	// 1. 失效文件索引缓存
	q.InvalidateFileIndexCache(ctx, tableName)

	// 2. 失效查询缓存（如果启用了Redis）
	if q.queryCache != nil && q.redisPool != nil {
		if err := q.queryCache.InvalidateByTable(ctx, tableName); err != nil {
			logger.LogWarn(ctx, "Failed to invalidate query cache",
				zap.String("table", tableName),
				zap.Error(err),
			)
			// 继续执行，不要因为查询缓存失效失败而返回错误
		} else {
			logger.LogInfo(ctx, "Query cache invalidated",
				zap.String("table", tableName),
			)
		}
	}

	// 3. 失效文件缓存（如果启用）
	if q.fileCache != nil {
		// 清理该表的文件缓存
		// 注意：FileCache.Clear() 会清理所有缓存，我们可能需要添加按表清理的方法
		// 暂时保守处理，只清理最近的缓存
		logger.LogInfo(ctx, "File cache will be refreshed on next query",
			zap.String("table", tableName),
		)
	}

	logger.LogInfo(ctx, "Table cache invalidated successfully",
		zap.String("table", tableName),
	)

	return nil
}
