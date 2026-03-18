package lock

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// unlockScript Lua 脚本：原子校验 token 后删除，防止误解他人的锁
var unlockScript = redis.NewScript(`
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	else
		return 0
	end
`)

// RedisLocker 基于 Redis SETNX 的分布式锁
// 适用于 distributed 模式（多节点）
type RedisLocker struct {
	client     redis.UniversalClient // 兼容 standalone/sentinel/cluster
	retryDelay time.Duration         // 加锁重试间隔
	maxRetry   int                   // 最大重试次数
}

// RedisLockOption 配置选项
type RedisLockOption func(*RedisLocker)

// WithRetryDelay 设置重试间隔
func WithRetryDelay(d time.Duration) RedisLockOption {
	return func(r *RedisLocker) { r.retryDelay = d }
}

// WithMaxRetry 设置最大重试次数
func WithMaxRetry(n int) RedisLockOption {
	return func(r *RedisLocker) { r.maxRetry = n }
}

// NewRedisLocker 创建 Redis 分布式锁实例
func NewRedisLocker(client redis.UniversalClient, opts ...RedisLockOption) *RedisLocker {
	l := &RedisLocker{
		client:     client,
		retryDelay: DefaultRetryDelay,
		maxRetry:   DefaultMaxRetry,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// TryLock 非阻塞尝试加锁
func (r *RedisLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	token := uuid.NewString()
	ok, err := r.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrLockConflict
	}
	return token, nil
}

// Lock 阻塞加锁，直到成功或 ctx 取消/超时
func (r *RedisLocker) Lock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	for i := 0; i < r.maxRetry; i++ {
		// 先检查 context 是否已取消
		if ctx.Err() != nil {
			return "", ErrLockTimeout
		}

		token, err := r.TryLock(ctx, key, ttl)
		if err == nil {
			return token, nil
		}
		if err != ErrLockConflict {
			// 检查是否是 context 错误
			if ctx.Err() != nil {
				return "", ErrLockTimeout
			}
			return "", err // Redis 连接错误等，不重试
		}
		select {
		case <-ctx.Done():
			return "", ErrLockTimeout
		case <-time.After(r.retryDelay):
		}
	}
	return "", ErrLockTimeout
}

// Unlock 解锁
func (r *RedisLocker) Unlock(ctx context.Context, key string, token string) error {
	if token == "" {
		return ErrInvalidToken
	}
	result, err := unlockScript.Run(ctx, r.client, []string{key}, token).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrUnlockFailed // token 不匹配或已过期
	}
	return nil
}

// Extend 延长锁的 TTL
// 可用于长时间操作需要延长锁持有时间的场景
func (r *RedisLocker) Extend(ctx context.Context, key string, token string, ttl time.Duration) error {
	if token == "" {
		return ErrInvalidToken
	}
	// 使用 Lua 脚本确保原子性：只有 token 匹配时才延长
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`)
	result, err := script.Run(ctx, r.client, []string{key}, token, ttl.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrUnlockFailed
	}
	return nil
}

// IsLocked 检查 key 是否被锁定
func (r *RedisLocker) IsLocked(ctx context.Context, key string) (bool, error) {
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// GetLockTTL 获取锁的剩余 TTL
func (r *RedisLocker) GetLockTTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}
