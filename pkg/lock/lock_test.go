package lock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============== OptimisticLocker Tests ==============

func TestOptimisticLocker_BasicLock(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	// 测试基本加锁
	token, err := locker.TryLock(ctx, "test-key", time.Second*10)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// 测试解锁
	err = locker.Unlock(ctx, "test-key", token)
	assert.NoError(t, err)
}

func TestOptimisticLocker_LockConflict(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	// 第一次加锁
	token1, err := locker.TryLock(ctx, "conflict-key", time.Second*10)
	require.NoError(t, err)

	// 第二次加锁应该冲突
	_, err = locker.TryLock(ctx, "conflict-key", time.Second*10)
	assert.ErrorIs(t, err, ErrLockConflict)

	// 解锁后可以重新加锁
	err = locker.Unlock(ctx, "conflict-key", token1)
	require.NoError(t, err)

	token2, err := locker.TryLock(ctx, "conflict-key", time.Second*10)
	assert.NoError(t, err)
	assert.NotEmpty(t, token2)

	_ = locker.Unlock(ctx, "conflict-key", token2)
}

func TestOptimisticLocker_ExpiredLock(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	// 加锁，TTL 很短
	token, err := locker.TryLock(ctx, "expire-key", time.Millisecond*50)
	require.NoError(t, err)

	// 等待过期
	time.Sleep(time.Millisecond * 100)

	// 过期后应该可以重新获取锁
	newToken, err := locker.TryLock(ctx, "expire-key", time.Second*10)
	assert.NoError(t, err)
	assert.NotEmpty(t, newToken)

	// 用旧 token 解锁应该失败
	err = locker.Unlock(ctx, "expire-key", token)
	assert.ErrorIs(t, err, ErrUnlockFailed)

	// 用新 token 解锁
	err = locker.Unlock(ctx, "expire-key", newToken)
	assert.NoError(t, err)
}

func TestOptimisticLocker_UnlockTokenMismatch(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	token, err := locker.TryLock(ctx, "mismatch-key", time.Second*10)
	require.NoError(t, err)

	// 用错误的 token 解锁
	err = locker.Unlock(ctx, "mismatch-key", "wrong-token")
	assert.ErrorIs(t, err, ErrUnlockFailed)

	// 用正确的 token 解锁
	err = locker.Unlock(ctx, "mismatch-key", token)
	assert.NoError(t, err)
}

func TestOptimisticLocker_BlockingLock(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	// 先获取锁
	token1, err := locker.TryLock(ctx, "blocking-key", time.Second*10)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var acquired int32

	// 启动 goroutine 尝试阻塞获取锁
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
		defer cancel()

		token, err := locker.Lock(ctx, "blocking-key", time.Second*10)
		if err == nil {
			atomic.StoreInt32(&acquired, 1)
			_ = locker.Unlock(context.Background(), "blocking-key", token)
		}
	}()

	// 等待一小段时间后释放锁
	time.Sleep(time.Millisecond * 100)
	err = locker.Unlock(ctx, "blocking-key", token1)
	require.NoError(t, err)

	wg.Wait()

	// 应该成功获取锁
	assert.Equal(t, int32(1), atomic.LoadInt32(&acquired))
}

func TestOptimisticLocker_ConcurrentUpdate(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	const goroutines = 100
	const key = "concurrent-key"

	var successCount int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			token, err := locker.Lock(ctx, key, time.Second*5)
			if err != nil {
				return
			}
			defer locker.Unlock(context.Background(), key, token)

			// 模拟操作
			time.Sleep(time.Millisecond * 10)
			atomic.AddInt64(&successCount, 1)
		}()
	}

	wg.Wait()

	// 应该只有一个 goroutine 能成功获取锁并执行
	// 由于 Lock 是阻塞的，所有 goroutine 都应该成功
	assert.Equal(t, int64(goroutines), successCount)
}

func TestOptimisticLocker_CleanupExpired(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	// 创建多个锁，部分会过期
	_, _ = locker.TryLock(ctx, "expire-1", time.Millisecond*10)
	_, _ = locker.TryLock(ctx, "expire-2", time.Millisecond*10)
	_, _ = locker.TryLock(ctx, "keep-1", time.Second*10)

	// 等待部分过期
	time.Sleep(time.Millisecond * 50)

	// 清理过期锁
	count := locker.CleanupExpired()
	assert.Equal(t, 2, count)
}

// ============== RedisLocker Tests ==============

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestRedisLocker_BasicLock(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	token, err := locker.TryLock(ctx, "test-key", time.Second*10)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	err = locker.Unlock(ctx, "test-key", token)
	assert.NoError(t, err)
}

func TestRedisLocker_LockConflict(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	token1, err := locker.TryLock(ctx, "conflict-key", time.Second*10)
	require.NoError(t, err)

	_, err = locker.TryLock(ctx, "conflict-key", time.Second*10)
	assert.ErrorIs(t, err, ErrLockConflict)

	err = locker.Unlock(ctx, "conflict-key", token1)
	require.NoError(t, err)

	token2, err := locker.TryLock(ctx, "conflict-key", time.Second*10)
	assert.NoError(t, err)

	_ = locker.Unlock(ctx, "conflict-key", token2)
}

func TestRedisLocker_LockTimeout(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client,
		WithRetryDelay(time.Millisecond*10),
		WithMaxRetry(3),
	)
	ctx := context.Background()

	// 先获取锁
	token, err := locker.TryLock(ctx, "timeout-key", time.Second*10)
	require.NoError(t, err)

	// 使用短超时尝试获取同一把锁
	ctx2, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel()

	_, err = locker.Lock(ctx2, "timeout-key", time.Second*10)
	assert.ErrorIs(t, err, ErrLockTimeout)

	_ = locker.Unlock(ctx, "timeout-key", token)
}

func TestRedisLocker_Extend(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	token, err := locker.TryLock(ctx, "extend-key", time.Millisecond*100)
	require.NoError(t, err)

	// 延长 TTL
	err = locker.Extend(ctx, "extend-key", token, time.Second*10)
	assert.NoError(t, err)

	// 等过原 TTL 时间
	time.Sleep(time.Millisecond * 150)

	// 锁应该还在
	locked, err := locker.IsLocked(ctx, "extend-key")
	assert.NoError(t, err)
	assert.True(t, locked)

	_ = locker.Unlock(ctx, "extend-key", token)
}

func TestRedisLocker_UnlockWithWrongToken(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	token, err := locker.TryLock(ctx, "wrong-token-key", time.Second*10)
	require.NoError(t, err)

	// 用错误的 token 解锁
	err = locker.Unlock(ctx, "wrong-token-key", "wrong-token")
	assert.ErrorIs(t, err, ErrUnlockFailed)

	// 锁应该还在
	locked, err := locker.IsLocked(ctx, "wrong-token-key")
	assert.NoError(t, err)
	assert.True(t, locked)

	// 用正确的 token 解锁
	err = locker.Unlock(ctx, "wrong-token-key", token)
	assert.NoError(t, err)
}

func TestRedisLocker_ConcurrentLock(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	const goroutines = 50
	const key = "concurrent-redis-key"

	var successCount int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			token, err := locker.Lock(ctx, key, time.Second*5)
			if err != nil {
				return
			}
			defer locker.Unlock(context.Background(), key, token)

			// 模拟操作
			time.Sleep(time.Millisecond * 5)
			atomic.AddInt64(&successCount, 1)
		}()
	}

	wg.Wait()

	// 所有 goroutine 应该都能成功（阻塞等待）
	assert.Equal(t, int64(goroutines), successCount)
}

func TestRedisLocker_IsLocked(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	// 初始应该没有锁
	locked, err := locker.IsLocked(ctx, "islocked-key")
	assert.NoError(t, err)
	assert.False(t, locked)

	// 加锁后应该有锁
	token, err := locker.TryLock(ctx, "islocked-key", time.Second*10)
	require.NoError(t, err)

	locked, err = locker.IsLocked(ctx, "islocked-key")
	assert.NoError(t, err)
	assert.True(t, locked)

	_ = locker.Unlock(ctx, "islocked-key", token)
}

func TestRedisLocker_GetLockTTL(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	ttl := time.Second * 10
	token, err := locker.TryLock(ctx, "ttl-key", ttl)
	require.NoError(t, err)

	remaining, err := locker.GetLockTTL(ctx, "ttl-key")
	assert.NoError(t, err)
	assert.Greater(t, remaining.Milliseconds(), int64(9000)) // 应该接近 10s
	assert.Less(t, remaining.Milliseconds(), int64(10001))

	_ = locker.Unlock(ctx, "ttl-key", token)
}

// ============== LockKey Tests ==============

func TestLockKey(t *testing.T) {
	key := LockKey("users", "123")
	assert.Equal(t, "lock:table:users:id:123", key)
}

func TestLockKeyWithPrefix(t *testing.T) {
	key := LockKeyWithPrefix("myapp", "users", "123")
	assert.Equal(t, "myapp:table:users:id:123", key)
}

// ============== Factory Tests ==============

func TestNewLocker_StandaloneMode(t *testing.T) {
	cfg := &Config{
		RunMode: ModeStandalone,
	}
	locker := NewLocker(cfg, nil)

	_, ok := locker.(*OptimisticLocker)
	assert.True(t, ok)
}

func TestNewLocker_DistributedModeWithPool(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// 创建 redis pool（简化版测试）
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	// 直接使用 client 测试
	locker := NewLockerWithClient(client)

	_, ok := locker.(*RedisLocker)
	assert.True(t, ok)
}

func TestNewLockerFromRedisMode(t *testing.T) {
	// standalone 模式应该返回 OptimisticLocker
	locker := NewLockerFromRedisMode("standalone", nil)
	_, ok := locker.(*OptimisticLocker)
	assert.True(t, ok)

	// sentinel 模式无 pool 应该降级为 OptimisticLocker
	locker = NewLockerFromRedisMode("sentinel", nil)
	_, ok = locker.(*OptimisticLocker)
	assert.True(t, ok)

	// cluster 模式无 pool 应该降级为 OptimisticLocker
	locker = NewLockerFromRedisMode("cluster", nil)
	_, ok = locker.(*OptimisticLocker)
	assert.True(t, ok)
}

func TestNewLockerWithClient_NilClient(t *testing.T) {
	locker := NewLockerWithClient(nil)
	_, ok := locker.(*OptimisticLocker)
	assert.True(t, ok, "nil client should fall back to OptimisticLocker")
}

func TestNewStandaloneLocker(t *testing.T) {
	locker := NewStandaloneLocker()
	_, ok := locker.(*OptimisticLocker)
	assert.True(t, ok)
}

func TestNewDistributedLocker_NilClient(t *testing.T) {
	locker := NewDistributedLocker(nil)
	_, ok := locker.(*OptimisticLocker)
	assert.True(t, ok, "nil client should fall back to OptimisticLocker")
}

// ============== Error Tests ==============

func TestErrors(t *testing.T) {
	// 确保错误类型正确
	assert.Error(t, ErrLockTimeout)
	assert.Error(t, ErrLockConflict)
	assert.Error(t, ErrUnlockFailed)
	assert.Error(t, ErrInvalidToken)

	// 确保可以使用 errors.Is
	assert.True(t, errors.Is(ErrLockTimeout, ErrLockTimeout))
	assert.True(t, errors.Is(ErrLockConflict, ErrLockConflict))
}

// ============== Context Cancellation Tests ==============

func TestOptimisticLocker_ContextCancellation(t *testing.T) {
	locker := NewOptimisticLocker()
	ctx := context.Background()

	// 先获取锁
	token, err := locker.TryLock(ctx, "cancel-key", time.Second*10)
	require.NoError(t, err)

	// 创建一个可取消的 context
	ctx2, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 尝试获取锁应该返回超时
	_, err = locker.Lock(ctx2, "cancel-key", time.Second*10)
	assert.ErrorIs(t, err, ErrLockTimeout)

	_ = locker.Unlock(ctx, "cancel-key", token)
}

func TestRedisLocker_ContextCancellation(t *testing.T) {
	mr, client := setupMiniredis(t)
	defer mr.Close()
	defer client.Close()

	locker := NewRedisLocker(client)
	ctx := context.Background()

	// 先获取锁
	token, err := locker.TryLock(ctx, "cancel-redis-key", time.Second*10)
	require.NoError(t, err)

	// 创建一个可取消的 context
	ctx2, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 尝试获取锁应该返回超时
	_, err = locker.Lock(ctx2, "cancel-redis-key", time.Second*10)
	assert.ErrorIs(t, err, ErrLockTimeout)

	_ = locker.Unlock(ctx, "cancel-redis-key", token)
}
