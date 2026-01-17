package storage

import (
	"context"
	"fmt"
	"time"

	"minIODB/pkg/pool"
)

// CacheStorageImpl Redis缓存存储实现
type CacheStorageImpl struct {
	poolManager *pool.PoolManager
}

// NewCacheStorage 创建新的缓存存储实例
func NewCacheStorage(poolManager *pool.PoolManager) CacheStorage {
	return &CacheStorageImpl{
		poolManager: poolManager,
	}
}

// Set 设置键值对
func (c *CacheStorageImpl) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis连接池不可用")
	}

	// 根据Redis模式执行操作
	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		client := redisPool.GetClusterClient()
		if client == nil {
			return fmt.Errorf("Redis集群客户端不可用")
		}
		return client.Set(ctx, key, value, expiration).Err()
	case pool.RedisModeSentinel:
		client := redisPool.GetSentinelClient()
		if client == nil {
			return fmt.Errorf("Redis哨兵客户端不可用")
		}
		return client.Set(ctx, key, value, expiration).Err()
	default: // standalone
		client := redisPool.GetClient()
		if client == nil {
			return fmt.Errorf("Redis客户端不可用")
		}
		return client.Set(ctx, key, value, expiration).Err()
	}
}

// Get 获取键值
func (c *CacheStorageImpl) Get(ctx context.Context, key string) (string, error) {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return "", fmt.Errorf("Redis连接池不可用")
	}

	// 根据Redis模式执行操作
	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		client := redisPool.GetClusterClient()
		if client == nil {
			return "", fmt.Errorf("Redis集群客户端不可用")
		}
		return client.Get(ctx, key).Result()
	case pool.RedisModeSentinel:
		client := redisPool.GetSentinelClient()
		if client == nil {
			return "", fmt.Errorf("Redis哨兵客户端不可用")
		}
		return client.Get(ctx, key).Result()
	default: // standalone
		client := redisPool.GetClient()
		if client == nil {
			return "", fmt.Errorf("Redis客户端不可用")
		}
		return client.Get(ctx, key).Result()
	}
}

// Del 删除键
func (c *CacheStorageImpl) Del(ctx context.Context, keys ...string) error {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis连接池不可用")
	}

	// 根据Redis模式执行操作
	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		client := redisPool.GetClusterClient()
		if client == nil {
			return fmt.Errorf("Redis集群客户端不可用")
		}
		return client.Del(ctx, keys...).Err()
	case pool.RedisModeSentinel:
		client := redisPool.GetSentinelClient()
		if client == nil {
			return fmt.Errorf("Redis哨兵客户端不可用")
		}
		return client.Del(ctx, keys...).Err()
	default: // standalone
		client := redisPool.GetClient()
		if client == nil {
			return fmt.Errorf("Redis客户端不可用")
		}
		return client.Del(ctx, keys...).Err()
	}
}

// Exists 检查键是否存在
func (c *CacheStorageImpl) Exists(ctx context.Context, keys ...string) (int64, error) {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return 0, fmt.Errorf("Redis连接池不可用")
	}

	// 根据Redis模式执行操作
	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		client := redisPool.GetClusterClient()
		if client == nil {
			return 0, fmt.Errorf("Redis集群客户端不可用")
		}
		return client.Exists(ctx, keys...).Result()
	case pool.RedisModeSentinel:
		client := redisPool.GetSentinelClient()
		if client == nil {
			return 0, fmt.Errorf("Redis哨兵客户端不可用")
		}
		return client.Exists(ctx, keys...).Result()
	default: // standalone
		client := redisPool.GetClient()
		if client == nil {
			return 0, fmt.Errorf("Redis客户端不可用")
		}
		return client.Exists(ctx, keys...).Result()
	}
}

// SAdd 向集合添加成员
func (c *CacheStorageImpl) SAdd(ctx context.Context, key string, members ...interface{}) error {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis连接池不可用")
	}

	// 根据Redis模式执行操作
	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		client := redisPool.GetClusterClient()
		if client == nil {
			return fmt.Errorf("Redis集群客户端不可用")
		}
		return client.SAdd(ctx, key, members...).Err()
	case pool.RedisModeSentinel:
		client := redisPool.GetSentinelClient()
		if client == nil {
			return fmt.Errorf("Redis哨兵客户端不可用")
		}
		return client.SAdd(ctx, key, members...).Err()
	default: // standalone
		client := redisPool.GetClient()
		if client == nil {
			return fmt.Errorf("Redis客户端不可用")
		}
		return client.SAdd(ctx, key, members...).Err()
	}
}

// SMembers 获取集合所有成员
func (c *CacheStorageImpl) SMembers(ctx context.Context, key string) ([]string, error) {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return nil, fmt.Errorf("Redis连接池不可用")
	}

	// 根据Redis模式执行操作
	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		client := redisPool.GetClusterClient()
		if client == nil {
			return nil, fmt.Errorf("Redis集群客户端不可用")
		}
		return client.SMembers(ctx, key).Result()
	case pool.RedisModeSentinel:
		client := redisPool.GetSentinelClient()
		if client == nil {
			return nil, fmt.Errorf("Redis哨兵客户端不可用")
		}
		return client.SMembers(ctx, key).Result()
	default: // standalone
		client := redisPool.GetClient()
		if client == nil {
			return nil, fmt.Errorf("Redis客户端不可用")
		}
		return client.SMembers(ctx, key).Result()
	}
}

// GetRedisMode 获取Redis模式
func (c *CacheStorageImpl) GetRedisMode() string {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return "unknown"
	}

	switch redisPool.GetMode() {
	case pool.RedisModeCluster:
		return "cluster"
	case pool.RedisModeSentinel:
		return "sentinel"
	default:
		return "standalone"
	}
}

// IsRedisCluster 检查是否为集群模式
func (c *CacheStorageImpl) IsRedisCluster() bool {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return false
	}
	return redisPool.GetMode() == pool.RedisModeCluster
}

// IsRedisSentinel 检查是否为哨兵模式
func (c *CacheStorageImpl) IsRedisSentinel() bool {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return false
	}
	return redisPool.GetMode() == pool.RedisModeSentinel
}

// HealthCheck 健康检查
func (c *CacheStorageImpl) HealthCheck(ctx context.Context) error {
	redisPool := c.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}

	return redisPool.HealthCheck(ctx)
}

// GetStats 获取统计信息
func (c *CacheStorageImpl) GetStats() map[string]interface{} {
	if c.poolManager == nil {
		return nil
	}

	stats := c.poolManager.GetStats()
	if stats == nil {
		return nil
	}

	// 只返回Redis相关的统计信息
	redisStats := make(map[string]interface{})
	if redis, ok := stats["redis"]; ok {
		redisStats["redis"] = redis
	}

	return redisStats
}

// GetClient 获取Redis客户端
func (c *CacheStorageImpl) GetClient() interface{} {
	if c.poolManager == nil {
		return nil
	}

	pool := c.poolManager.GetRedisPool()
	if pool == nil {
		return nil
	}

	return pool.GetClient()
}

// Close 关闭连接
func (c *CacheStorageImpl) Close() error {
	// 连接池由PoolManager管理，这里不需要手动关闭
	return nil
}
