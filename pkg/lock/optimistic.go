package lock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// OptimisticLocker 基于 sync.Map + atomic 的本地乐观锁
// 适用于 standalone 模式（单进程）
type OptimisticLocker struct {
	// key -> *lockEntry
	locks sync.Map
}

type lockEntry struct {
	token     string
	expiresAt int64 // UnixNano，原子读写
}

// NewOptimisticLocker 创建乐观锁实例
func NewOptimisticLocker() *OptimisticLocker {
	return &OptimisticLocker{}
}

// TryLock 非阻塞尝试加锁
func (o *OptimisticLocker) TryLock(_ context.Context, key string, ttl time.Duration) (string, error) {
	token := uuid.NewString()
	entry := &lockEntry{
		token:     token,
		expiresAt: time.Now().Add(ttl).UnixNano(),
	}

	// LoadOrStore：若 key 不存在则存入并返回 loaded=false
	actual, loaded := o.locks.LoadOrStore(key, entry)
	if loaded {
		existing := actual.(*lockEntry)
		// 检查是否已过期（自动回收过期锁）
		if atomic.LoadInt64(&existing.expiresAt) > time.Now().UnixNano() {
			return "", ErrLockConflict
		}
		// 过期锁：用 CompareAndSwap 替换
		if !o.locks.CompareAndSwap(key, existing, entry) {
			return "", ErrLockConflict
		}
	}
	return token, nil
}

// Lock 阻塞加锁，直到成功或 ctx 取消
func (o *OptimisticLocker) Lock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	const retryInterval = 10 * time.Millisecond
	for {
		token, err := o.TryLock(ctx, key, ttl)
		if err == nil {
			return token, nil
		}
		if err != ErrLockConflict {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ErrLockTimeout
		case <-time.After(retryInterval):
		}
	}
}

// Unlock 解锁
func (o *OptimisticLocker) Unlock(_ context.Context, key string, token string) error {
	actual, ok := o.locks.Load(key)
	if !ok {
		return nil // 已过期自动释放，视为成功
	}
	entry := actual.(*lockEntry)
	if entry.token != token {
		return ErrUnlockFailed
	}
	// 使用 CompareAndDelete 避免 TOCTOU 竞态
	// 确保 entry 没有被其他 goroutine 替换
	// Go 1.20+ 支持 sync.Map.CompareAndDelete
	if !o.locks.CompareAndDelete(key, entry) {
		// entry 已被替换，说明锁已被其他 goroutine 获取
		return ErrUnlockFailed
	}
	return nil
}

// CleanupExpired 清理所有过期的锁
// 可定期调用以释放内存
func (o *OptimisticLocker) CleanupExpired() int {
	count := 0
	now := time.Now().UnixNano()
	o.locks.Range(func(key, value interface{}) bool {
		entry := value.(*lockEntry)
		if atomic.LoadInt64(&entry.expiresAt) <= now {
			o.locks.Delete(key)
			count++
		}
		return true
	})
	return count
}
