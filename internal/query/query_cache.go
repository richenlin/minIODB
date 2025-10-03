package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"minIODB/internal/logger"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// QueryCache 查询缓存管理器
type QueryCache struct {
	redisClient    *redis.Client
	logger         *zap.Logger
	defaultTTL     time.Duration
	maxCacheSize   int64
	enableMetrics  bool
	cacheHits      int64
	cacheMisses    int64
	cacheKeyPrefix string
}

// CacheConfig 缓存配置
type CacheConfig struct {
	DefaultTTL     time.Duration `yaml:"default_ttl"`     // 默认缓存时间
	MaxCacheSize   int64         `yaml:"max_cache_size"`  // 最大缓存大小（字节）
	EnableMetrics  bool          `yaml:"enable_metrics"`  // 启用指标收集
	KeyPrefix      string        `yaml:"key_prefix"`      // 缓存键前缀
	EvictionPolicy string        `yaml:"eviction_policy"` // 淘汰策略
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Query       string            `json:"query"`        // 原始查询
	QueryHash   string            `json:"query_hash"`   // 查询哈希
	Result      string            `json:"result"`       // 查询结果
	Tables      []string          `json:"tables"`       // 涉及的表
	Metadata    map[string]string `json:"metadata"`     // 元数据
	CreatedAt   time.Time         `json:"created_at"`   // 创建时间
	AccessCount int64             `json:"access_count"` // 访问次数
	Size        int64             `json:"size"`         // 结果大小
}

// CacheStats 缓存统计
type CacheStats struct {
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	HitRatio    float64 `json:"hit_ratio"`
	TotalSize   int64   `json:"total_size"`
	EntryCount  int64   `json:"entry_count"`
	EvictedKeys int64   `json:"evicted_keys"`
}

// NewQueryCache 创建查询缓存管理器
func NewQueryCache(redisClient *redis.Client, config *CacheConfig, logger *zap.Logger) *QueryCache {
	if config == nil {
		config = &CacheConfig{
			DefaultTTL:     30 * time.Minute,
			MaxCacheSize:   100 * 1024 * 1024, // 100MB
			EnableMetrics:  true,
			KeyPrefix:      "query_cache:",
			EvictionPolicy: "lru",
		}
	}

	return &QueryCache{
		redisClient:    redisClient,
		logger:         logger,
		defaultTTL:     config.DefaultTTL,
		maxCacheSize:   config.MaxCacheSize,
		enableMetrics:  config.EnableMetrics,
		cacheKeyPrefix: config.KeyPrefix,
	}
}

// Get 从缓存获取查询结果
func (qc *QueryCache) Get(ctx context.Context, query string, tables []string) (*CacheEntry, bool) {
	// 生成缓存键
	cacheKey := qc.generateCacheKey(query, tables)

	// 从Redis获取缓存
	cached, err := qc.redisClient.Get(ctx, cacheKey).Result()
	if err != nil {
		if err != redis.Nil {
			qc.logger.Warn("Failed to get cache", zap.String("key", cacheKey), zap.Error(err))
		}
		qc.recordCacheMiss()
		return nil, false
	}

	// 反序列化缓存条目
	var entry CacheEntry
	if err := json.Unmarshal([]byte(cached), &entry); err != nil {
		qc.logger.Warn("Failed to unmarshal cache entry", zap.String("key", cacheKey), zap.Error(err))
		qc.recordCacheMiss()
		return nil, false
	}

	// 更新访问统计
	qc.updateAccessStats(ctx, cacheKey, &entry)
	qc.recordCacheHit()

	logger.LogInfo(ctx, "Cache HIT for query hash: %s", zap.String("query_hash", entry.QueryHash[:8]))
	return &entry, true
}

// Set 将查询结果存入缓存
func (qc *QueryCache) Set(ctx context.Context, query string, result string, tables []string) error {
	// 创建缓存条目
	entry := &CacheEntry{
		Query:       query,
		QueryHash:   qc.hashQuery(query),
		Result:      result,
		Tables:      tables,
		Metadata:    qc.extractMetadata(query, result),
		CreatedAt:   time.Now(),
		AccessCount: 1,
		Size:        int64(len(result)),
	}

	// 序列化条目
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	// 检查缓存大小限制
	if err := qc.checkCacheSizeLimit(ctx, int64(len(data))); err != nil {
		qc.logger.Warn("Cache size limit exceeded", zap.Error(err))
		return err
	}

	// 生成缓存键
	cacheKey := qc.generateCacheKey(query, tables)

	// 存储到Redis
	if err := qc.redisClient.Set(ctx, cacheKey, data, qc.defaultTTL).Err(); err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	// 更新表级索引（用于缓存失效）
	qc.updateTableIndex(ctx, tables, cacheKey)

	logger.LogInfo(ctx, "Cache SET for query hash: %s, size: %d bytes", zap.String("query_hash", entry.QueryHash[:8]), zap.Int64("size", entry.Size))
	return nil
}

// InvalidateByTables 根据表名失效相关缓存
func (qc *QueryCache) InvalidateByTables(ctx context.Context, tables []string) error {
	var keysToDelete []string

	for _, table := range tables {
		indexKey := qc.getTableIndexKey(table)

		// 获取该表相关的所有缓存键
		cacheKeys, err := qc.redisClient.SMembers(ctx, indexKey).Result()
		if err != nil {
			continue
		}

		keysToDelete = append(keysToDelete, cacheKeys...)

		// 删除表索引
		qc.redisClient.Del(ctx, indexKey)
	}

	// 批量删除缓存键
	if len(keysToDelete) > 0 {
		if err := qc.redisClient.Del(ctx, keysToDelete...).Err(); err != nil {
			return fmt.Errorf("failed to delete cache keys: %w", err)
		}
		logger.LogInfo(ctx, "Invalidated %d cache entries for tables: %v", zap.Int("count", len(keysToDelete)), zap.String("tables", fmt.Sprintf("%v", tables)))
	}

	return nil
}

// generateCacheKey 生成缓存键
func (qc *QueryCache) generateCacheKey(query string, tables []string) string {
	// 标准化查询（移除多余空格，转换为小写）
	normalizedQuery := qc.normalizeQuery(query)

	// 结合查询和表信息生成哈希
	combined := normalizedQuery + "|" + strings.Join(tables, ",")
	hash := qc.hashQuery(combined)

	return qc.cacheKeyPrefix + hash
}

// normalizeQuery 标准化查询语句
func (qc *QueryCache) normalizeQuery(query string) string {
	// 移除多余空格
	normalized := strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
	// 转换为小写（除了字符串字面量）
	return strings.ToLower(normalized)
}

// hashQuery 生成查询哈希
func (qc *QueryCache) hashQuery(query string) string {
	hash := sha256.Sum256([]byte(query))
	return hex.EncodeToString(hash[:])
}

// extractMetadata 提取查询元数据
func (qc *QueryCache) extractMetadata(query, result string) map[string]string {
	metadata := make(map[string]string)

	// 提取查询类型
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if strings.HasPrefix(queryLower, "select") {
		metadata["type"] = "select"
	} else if strings.HasPrefix(queryLower, "count") {
		metadata["type"] = "count"
	} else {
		metadata["type"] = "unknown"
	}

	// 提取结果大小
	metadata["result_size"] = fmt.Sprintf("%d", len(result))

	// 提取是否包含聚合函数
	if strings.Contains(queryLower, "group by") ||
		strings.Contains(queryLower, "count(") ||
		strings.Contains(queryLower, "sum(") ||
		strings.Contains(queryLower, "avg(") {
		metadata["has_aggregation"] = "true"
	} else {
		metadata["has_aggregation"] = "false"
	}

	return metadata
}

// updateTableIndex 更新表级索引
func (qc *QueryCache) updateTableIndex(ctx context.Context, tables []string, cacheKey string) {
	for _, table := range tables {
		indexKey := qc.getTableIndexKey(table)
		qc.redisClient.SAdd(ctx, indexKey, cacheKey)
		qc.redisClient.Expire(ctx, indexKey, qc.defaultTTL*2) // 索引TTL是缓存TTL的2倍
	}
}

// getTableIndexKey 获取表索引键
func (qc *QueryCache) getTableIndexKey(table string) string {
	return qc.cacheKeyPrefix + "table_index:" + table
}

// updateAccessStats 更新访问统计
func (qc *QueryCache) updateAccessStats(ctx context.Context, cacheKey string, entry *CacheEntry) {
	entry.AccessCount++

	// 异步更新Redis中的访问计数
	go func() {
		updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		data, err := json.Marshal(entry)
		if err == nil {
			qc.redisClient.Set(updateCtx, cacheKey, data, qc.defaultTTL)
		}
	}()
}

// checkCacheSizeLimit 检查缓存大小限制
func (qc *QueryCache) checkCacheSizeLimit(ctx context.Context, newEntrySize int64) error {
	// 获取当前缓存大小
	currentSize, err := qc.getCurrentCacheSize(ctx)
	if err != nil {
		return nil // 忽略错误，继续缓存
	}

	// 检查是否超过限制
	if currentSize+newEntrySize > qc.maxCacheSize {
		// 执行LRU淘汰
		if err := qc.evictLRU(ctx, newEntrySize); err != nil {
			return fmt.Errorf("failed to evict cache entries: %w", err)
		}
	}

	return nil
}

// getCurrentCacheSize 获取当前缓存大小
func (qc *QueryCache) getCurrentCacheSize(ctx context.Context) (int64, error) {
	// 使用Redis SCAN遍历缓存键
	var totalSize int64
	iter := qc.redisClient.Scan(ctx, 0, qc.cacheKeyPrefix+"*", 100).Iterator()

	for iter.Next(ctx) {
		key := iter.Val()
		if strings.Contains(key, "table_index:") {
			continue // 跳过索引键
		}

		// 获取键的大小
		size, err := qc.redisClient.StrLen(ctx, key).Result()
		if err == nil {
			totalSize += size
		}
	}

	return totalSize, iter.Err()
}

// evictLRU 执行LRU淘汰
func (qc *QueryCache) evictLRU(ctx context.Context, requiredSpace int64) error {
	// 获取所有缓存条目及其访问时间
	entries := make(map[string]*CacheEntry)
	iter := qc.redisClient.Scan(ctx, 0, qc.cacheKeyPrefix+"*", 100).Iterator()

	for iter.Next(ctx) {
		key := iter.Val()
		if strings.Contains(key, "table_index:") {
			continue
		}

		data, err := qc.redisClient.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var entry CacheEntry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			continue
		}

		entries[key] = &entry
	}

	// 按访问计数和创建时间排序，删除最少使用的条目
	var freedSpace int64
	for key, entry := range entries {
		if freedSpace >= requiredSpace {
			break
		}

		// 删除条目
		qc.redisClient.Del(ctx, key)
		freedSpace += entry.Size

		logger.LogInfo(ctx, "Evicted cache entry: %s, size: %d", zap.String("query_hash", entry.QueryHash[:8]), zap.Int64("size", entry.Size))
	}

	return nil
}

// recordCacheHit 记录缓存命中
func (qc *QueryCache) recordCacheHit() {
	if qc.enableMetrics {
		qc.cacheHits++
	}
}

// recordCacheMiss 记录缓存未命中
func (qc *QueryCache) recordCacheMiss() {
	if qc.enableMetrics {
		qc.cacheMisses++
	}
}

// GetStats 获取缓存统计
func (qc *QueryCache) GetStats(ctx context.Context) *CacheStats {
	stats := &CacheStats{
		Hits:   qc.cacheHits,
		Misses: qc.cacheMisses,
	}

	// 计算命中率
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRatio = float64(stats.Hits) / float64(total)
	}

	// 获取缓存大小信息
	totalSize, _ := qc.getCurrentCacheSize(ctx)
	stats.TotalSize = totalSize

	// 计算条目数量
	iter := qc.redisClient.Scan(ctx, 0, qc.cacheKeyPrefix+"*", 10).Iterator()
	for iter.Next(ctx) {
		if !strings.Contains(iter.Val(), "table_index:") {
			stats.EntryCount++
		}
	}

	return stats
}

// Clear 清空缓存
func (qc *QueryCache) Clear(ctx context.Context) error {
	iter := qc.redisClient.Scan(ctx, 0, qc.cacheKeyPrefix+"*", 100).Iterator()
	var keys []string

	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if len(keys) > 0 {
		if err := qc.redisClient.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to clear cache: %w", err)
		}
		logger.LogInfo(ctx, "Cleared %d cache entries", zap.Int("count", len(keys)))
	}

	// 重置指标
	qc.cacheHits = 0
	qc.cacheMisses = 0

	return nil
}
