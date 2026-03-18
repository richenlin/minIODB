// Package lock 提供分布式锁和乐观锁实现
// 支持 standalone（乐观锁）和 distributed（Redis 分布式锁）两种模式
package lock

import (
	"context"
	"errors"
	"time"
)

// Locker 锁接口，standalone 和 distributed 模式均实现此接口
type Locker interface {
	// Lock 加锁，返回 token（用于解锁校验）
	// 若锁已被持有，阻塞直到超时返回 ErrLockTimeout
	Lock(ctx context.Context, key string, ttl time.Duration) (token string, err error)

	// Unlock 解锁，token 必须与加锁时返回的一致（防误解）
	Unlock(ctx context.Context, key string, token string) error

	// TryLock 非阻塞尝试加锁，失败立即返回 ErrLockConflict
	TryLock(ctx context.Context, key string, ttl time.Duration) (token string, err error)
}

// LockKey 生成标准锁键名
// 格式：lock:table:{table}:id:{id}
func LockKey(table, id string) string {
	return "lock:table:" + table + ":id:" + id
}

// LockKeyWithPrefix 生成带前缀的锁键名
// 格式：{prefix}:table:{table}:id:{id}
func LockKeyWithPrefix(prefix, table, id string) string {
	return prefix + ":table:" + table + ":id:" + id
}

// 错误定义
var (
	// ErrLockTimeout 获取锁超时
	ErrLockTimeout = errors.New("lock: acquire timeout")
	// ErrLockConflict 锁冲突（已被其他持有者锁定）
	ErrLockConflict = errors.New("lock: key already locked")
	// ErrUnlockFailed 解锁失败（token 不匹配或已过期）
	ErrUnlockFailed = errors.New("lock: unlock failed, token mismatch or expired")
	// ErrInvalidToken 无效的 token
	ErrInvalidToken = errors.New("lock: invalid token")
)

// DefaultLockTTL 默认锁 TTL
const DefaultLockTTL = 30 * time.Second

// DefaultRetryDelay 默认重试间隔
const DefaultRetryDelay = 20 * time.Millisecond

// DefaultMaxRetry 默认最大重试次数
const DefaultMaxRetry = 50
