package query

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	cacheHits      atomic.Int64
	cacheMisses    atomic.Int64
	cacheKeyPrefix string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// LRU 缓存（O(1) 逐出）
	lru *lruCache
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

// lruEntry LRU 缓存条目（用于 container/list）
type lruEntry struct {
	key       string    // Redis 缓存键
	size      int64     // 条目大小（字节）
	expiresAt time.Time // 过期时间
}

// lruCache LRU 缓存管理器（O(1) 逐出）
// 使用 container/list（双向链表）+ map 实现
// 链表头部：最近使用；链表尾部：最少使用
type lruCache struct {
	capacity int64                    // 最大容量（字节）
	usedSize int64                    // 当前使用大小（字节）
	items    map[string]*list.Element // key -> list.Element（O(1) 查找）
	order    *list.List               // 访问顺序链表（O(1) 移动/删除）
	mu       sync.RWMutex             // 保护并发访问
}

// newLRUCache 创建 LRU 缓存
func newLRUCache(capacity int64) *lruCache {
	return &lruCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// get 查找并移动到链表头部（O(1)）
func (l *lruCache) get(key string) (*lruEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[key]; ok {
		entry := elem.Value.(*lruEntry)
		// 检查是否过期
		if time.Now().Before(entry.expiresAt) {
			l.order.MoveToFront(elem) // 移动到头部（最近使用）
			return entry, true
		}
		// 过期则删除
		l.order.Remove(elem)
		delete(l.items, key)
		l.usedSize -= entry.size
	}
	return nil, false
}

// put 添加到链表头部（O(1)）
func (l *lruCache) put(key string, size int64, ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 如果已存在，先删除旧的
	if elem, ok := l.items[key]; ok {
		oldEntry := elem.Value.(*lruEntry)
		l.usedSize -= oldEntry.size
		l.order.Remove(elem)
	}

	// 创建新条目并添加到头部
	entry := &lruEntry{
		key:       key,
		size:      size,
		expiresAt: time.Now().Add(ttl),
	}
	elem := l.order.PushFront(entry)
	l.items[key] = elem
	l.usedSize += size
}

// remove 删除指定键（O(1)）
func (l *lruCache) remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[key]; ok {
		entry := elem.Value.(*lruEntry)
		l.usedSize -= entry.size
		l.order.Remove(elem)
		delete(l.items, key)
	}
}

// evictOne 逐出一个最少使用的条目（O(1)）
// 返回被逐出的键和大小
func (l *lruCache) evictOne() (string, int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 从链表尾部获取最少使用的条目
	elem := l.order.Back()
	if elem == nil {
		return "", 0, false
	}

	entry := elem.Value.(*lruEntry)
	key := entry.key
	size := entry.size

	// 从链表和 map 中删除
	l.order.Remove(elem)
	delete(l.items, key)
	l.usedSize -= size

	return key, size, true
}

// evictForSpace 逐出直到腾出足够空间（O(1) 每次逐出）
// 返回被逐出的键列表
func (l *lruCache) evictForSpace(requiredSpace int64) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	var evictedKeys []string
	var freedSpace int64

	// 从链表尾部逐出，直到腾出足够空间
	for freedSpace < requiredSpace && l.order.Len() > 0 {
		elem := l.order.Back()
		if elem == nil {
			break
		}

		entry := elem.Value.(*lruEntry)
		evictedKeys = append(evictedKeys, entry.key)
		freedSpace += entry.size

		l.order.Remove(elem)
		delete(l.items, entry.key)
		l.usedSize -= entry.size
	}

	return evictedKeys
}

// needsEviction 检查是否需要逐出
func (l *lruCache) needsEviction(newSize int64) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.usedSize+newSize > l.capacity
}

// size 获取当前使用大小
func (l *lruCache) size() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.usedSize
}

// len 获取条目数量
func (l *lruCache) len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.order.Len()
}

// NewQueryCache 创建查询缓存管理器
func NewQueryCache(redisClient *redis.Client, config *CacheConfig, logger *zap.Logger) *QueryCache {
	return NewQueryCacheWithContext(context.Background(), redisClient, config, logger)
}

// NewQueryCacheWithContext 创建带Context的查询缓存管理器
func NewQueryCacheWithContext(ctx context.Context, redisClient *redis.Client, config *CacheConfig, logger *zap.Logger) *QueryCache {
	if config == nil {
		config = &CacheConfig{
			DefaultTTL:     30 * time.Minute,
			MaxCacheSize:   100 * 1024 * 1024, // 100MB
			EnableMetrics:  true,
			KeyPrefix:      "query_cache:",
			EvictionPolicy: "lru",
		}
	}

	cacheCtx, cancel := context.WithCancel(ctx)
	qc := &QueryCache{
		redisClient:    redisClient,
		logger:         logger,
		defaultTTL:     config.DefaultTTL,
		maxCacheSize:   config.MaxCacheSize,
		enableMetrics:  config.EnableMetrics,
		cacheKeyPrefix: config.KeyPrefix,
		ctx:            cacheCtx,
		cancel:         cancel,
		lru:            newLRUCache(config.MaxCacheSize),
	}

	// 监听Context取消
	go func() {
		<-ctx.Done()
		qc.Stop()
	}()

	return qc
}

// Get 从缓存获取查询结果
func (qc *QueryCache) Get(ctx context.Context, query string, tables []string) (*CacheEntry, bool) {
	// 生成缓存键
	cacheKey := qc.generateCacheKey(query, tables)

	// 先检查本地 LRU 缓存（O(1)）
	if entry, ok := qc.lru.get(cacheKey); ok {
		// LRU 中存在，从 Redis 获取数据
		cached, err := qc.redisClient.Get(ctx, cacheKey).Result()
		if err == nil {
			var cacheEntry CacheEntry
			if err := json.Unmarshal([]byte(cached), &cacheEntry); err == nil {
				qc.updateAccessStats(ctx, cacheKey, &cacheEntry)
				qc.recordCacheHit()
				qc.logger.Sugar().Infof("Cache HIT for query hash: %s", cacheEntry.QueryHash[:8])
				return &cacheEntry, true
			}
		}
		// Redis 获取失败，从 LRU 中移除
		qc.lru.remove(cacheKey)
		_ = entry // entry 用于确认 LRU 中存在
	}

	// LRU 中不存在，尝试从 Redis 直接获取（兼容旧数据）
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

	// 添加到 LRU 缓存
	qc.lru.put(cacheKey, entry.Size, qc.defaultTTL)

	// 更新访问统计
	qc.updateAccessStats(ctx, cacheKey, &entry)
	qc.recordCacheHit()

	qc.logger.Sugar().Infof("Cache HIT for query hash: %s", entry.QueryHash[:8])
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

	// 生成缓存键
	cacheKey := qc.generateCacheKey(query, tables)
	entrySize := int64(len(data))

	// 检查是否需要逐出（O(1)）
	if qc.lru.needsEviction(entrySize) {
		if err := qc.evictLRU(ctx, entrySize); err != nil {
			qc.logger.Warn("Cache eviction failed", zap.Error(err))
			// 继续存储，不返回错误
		}
	}

	// 存储到 Redis
	if err := qc.redisClient.Set(ctx, cacheKey, data, qc.defaultTTL).Err(); err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	// 添加到本地 LRU 缓存（O(1)）
	qc.lru.put(cacheKey, entrySize, qc.defaultTTL)

	// 更新表级索引（用于缓存失效）
	qc.updateTableIndex(ctx, tables, cacheKey)

	qc.logger.Sugar().Infof("Cache SET for query hash: %s, size: %d bytes", entry.QueryHash[:8], entry.Size)
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

		// 从本地 LRU 中删除
		for _, key := range keysToDelete {
			qc.lru.remove(key)
		}

		qc.logger.Sugar().Infof("Invalidated %d cache entries for tables: %v", len(keysToDelete), tables)
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
	qc.wg.Add(1)
	go func() {
		defer qc.wg.Done()

		updateCtx, cancel := context.WithTimeout(qc.ctx, 5*time.Second)
		defer cancel()

		data, err := json.Marshal(entry)
		if err == nil {
			qc.redisClient.Set(updateCtx, cacheKey, data, qc.defaultTTL)
		}
	}()
}

// Stop 停止QueryCache的后台goroutine
func (qc *QueryCache) Stop() {
	if qc.cancel != nil {
		qc.cancel()
	}
	qc.wg.Wait()
	qc.logger.Info("QueryCache stopped gracefully")
}

// evictLRU 执行 LRU 淘汰（O(1) 操作）
// 使用 container/list + map 实现：
// - 直接从链表尾部删除最少使用的条目
// - 从 map 中删除对应的键
// - 从 Redis 中删除对应的缓存
func (qc *QueryCache) evictLRU(ctx context.Context, requiredSpace int64) error {
	// 获取需要逐出的键列表（O(1) 每次逐出）
	evictedKeys := qc.lru.evictForSpace(requiredSpace)

	// 从 Redis 中删除这些键
	if len(evictedKeys) > 0 {
		if err := qc.redisClient.Del(ctx, evictedKeys...).Err(); err != nil {
			qc.logger.Warn("Failed to delete evicted keys from Redis", zap.Error(err))
			// 不返回错误，本地 LRU 已更新
		}

		for _, key := range evictedKeys {
			qc.logger.Sugar().Infof("Evicted cache entry (LRU): %s", key)
		}
	}

	return nil
}

// recordCacheHit 记录缓存命中
func (qc *QueryCache) recordCacheHit() {
	if qc.enableMetrics {
		qc.cacheHits.Add(1)
	}
}

// recordCacheMiss 记录缓存未命中
func (qc *QueryCache) recordCacheMiss() {
	if qc.enableMetrics {
		qc.cacheMisses.Add(1)
	}
}

// GetStats 获取缓存统计
func (qc *QueryCache) GetStats(ctx context.Context) *CacheStats {
	stats := &CacheStats{
		Hits:   qc.cacheHits.Load(),
		Misses: qc.cacheMisses.Load(),
	}

	// 计算命中率
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRatio = float64(stats.Hits) / float64(total)
	}

	// 从 LRU 缓存获取大小和条目数量（O(1)）
	stats.TotalSize = qc.lru.size()
	stats.EntryCount = int64(qc.lru.len())

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
		qc.logger.Sugar().Infof("Cleared %d cache entries from Redis", len(keys))
	}

	// 清空本地 LRU 缓存
	qc.lru = newLRUCache(qc.maxCacheSize)

	// 重置指标
	qc.cacheHits.Store(0)
	qc.cacheMisses.Store(0)

	return nil
}
