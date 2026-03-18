package lock

import (
	"time"

	"github.com/go-redis/redis/v8"

	"minIODB/pkg/pool"
)

// RunMode 运行模式类型
type RunMode string

const (
	// ModeStandalone 单机模式
	ModeStandalone RunMode = "standalone"
	// ModeDistributed 分布式模式（sentinel 或 cluster）
	ModeDistributed RunMode = "distributed"
)

// Config 锁配置
type Config struct {
	// RunMode 运行模式：standalone 或 distributed
	RunMode RunMode
	// RetryDelay 重试间隔（仅 RedisLocker 使用）
	RetryDelay time.Duration
	// MaxRetry 最大重试次数（仅 RedisLocker 使用）
	MaxRetry int
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		RunMode:    ModeStandalone,
		RetryDelay: DefaultRetryDelay,
		MaxRetry:   DefaultMaxRetry,
	}
}

// NewLocker 根据运行模式创建对应的 Locker
// standalone 模式 → OptimisticLocker（本地乐观锁）
// distributed 模式 → RedisLocker（Redis 分布式锁）
func NewLocker(cfg *Config, redisPool *pool.RedisPool) Locker {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.RunMode == ModeDistributed && redisPool != nil {
		client := redisPool.GetUniversalClient()
		if client != nil {
			return NewRedisLocker(client,
				WithRetryDelay(cfg.RetryDelay),
				WithMaxRetry(cfg.MaxRetry),
			)
		}
	}

	return NewOptimisticLocker()
}

// NewLockerFromRedisMode 根据 Redis 模式创建 Locker
// mode 为 "sentinel" 或 "cluster" 时使用 Redis 分布式锁
// mode 为 "standalone" 时使用乐观锁
func NewLockerFromRedisMode(mode string, redisPool *pool.RedisPool) Locker {
	if mode == "sentinel" || mode == "cluster" {
		if redisPool != nil {
			client := redisPool.GetUniversalClient()
			if client != nil {
				return NewRedisLocker(client)
			}
		}
	}
	return NewOptimisticLocker()
}

// NewLockerWithClient 使用已有的 Redis 客户端创建分布式锁
// 适用于需要自定义 Redis 客户端配置的场景
func NewLockerWithClient(client redis.UniversalClient, opts ...RedisLockOption) Locker {
	if client != nil {
		return NewRedisLocker(client, opts...)
	}
	return NewOptimisticLocker()
}

// NewStandaloneLocker 创建单机模式的乐观锁
func NewStandaloneLocker() Locker {
	return NewOptimisticLocker()
}

// NewDistributedLocker 创建分布式模式的 Redis 锁
func NewDistributedLocker(client redis.UniversalClient, opts ...RedisLockOption) Locker {
	if client == nil {
		// 降级为乐观锁
		return NewOptimisticLocker()
	}
	return NewRedisLocker(client, opts...)
}
