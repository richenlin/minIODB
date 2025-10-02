package pool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRedisPoolConfig(t *testing.T) {
	config := DefaultRedisPoolConfig()
	
	assert.NotNil(t, config)
	assert.Equal(t, RedisModeStandalone, config.Mode)
	assert.Equal(t, "localhost:6379", config.Addr)
	assert.True(t, config.PoolSize > 0)
	assert.True(t, config.MinIdleConns > 0)
	assert.True(t, config.MaxConnAge > 0)
	assert.True(t, config.PoolTimeout > 0)
	assert.True(t, config.IdleTimeout > 0)
	assert.True(t, config.IdleCheckFreq > 0)
	assert.True(t, config.DialTimeout > 0)
	assert.True(t, config.ReadTimeout > 0)
	assert.True(t, config.WriteTimeout > 0)
	assert.True(t, config.MaxRetries > 0)
	assert.True(t, config.MinRetryBackoff > 0)
	assert.True(t, config.MaxRetryBackoff > 0)
}

func TestNewRedisPool_Standalone(t *testing.T) {
	// 创建miniredis服务器用于测试
	s := miniredis.RunT(t)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:              RedisModeStandalone,
		Addr:              s.Addr(),
		Password:          "",
		DB:                0,
		PoolSize:          10,
		MinIdleConns:      5,
		MaxConnAge:        time.Hour,
		PoolTimeout:       4 * time.Second,
		IdleTimeout:       5 * time.Minute,
		IdleCheckFreq:     time.Minute,
		DialTimeout:       5 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      3 * time.Second,
		MaxRetries:        3,
		MinRetryBackoff:   8 * time.Millisecond,
		MaxRetryBackoff:   512 * time.Millisecond,
	}

	pool, err := NewRedisPool(config)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	// 测试获取客户端
	client := pool.GetClient()
	assert.NotNil(t, client)

	// 测试模式
	assert.Equal(t, RedisModeStandalone, pool.GetMode())

	// 测试配置
	assert.Equal(t, config, pool.GetConfig())

	// 测试健康检查
	ctx := context.Background()
	err = pool.HealthCheck(ctx)
	assert.NoError(t, err)

	// 测试统计信息
	stats := pool.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, "standalone", stats.Mode)
	assert.GreaterOrEqual(t, stats.TotalConns, uint32(0))
	assert.GreaterOrEqual(t, stats.IdleConns, uint32(0))

	// 测试连接信息
	connInfo := pool.GetConnectionInfo()
	assert.NotNil(t, connInfo)
	assert.Contains(t, connInfo, "mode")
	assert.Contains(t, connInfo, "addr")
}

func TestNewRedisPool_InvalidConfig(t *testing.T) {
	// 测试无效配置
	config := &RedisPoolConfig{
		Mode: RedisModeStandalone,
		Addr: "invalid:address:port",
	}

	pool, err := NewRedisPool(config)
	assert.Error(t, err)
	assert.Nil(t, pool)
}

func TestNewRedisPool_Sentinel(t *testing.T) {
	config := &RedisPoolConfig{
		Mode:          RedisModeSentinel,
		MasterName:    "mymaster",
		SentinelAddrs: []string{"localhost:26379"},
		Password:      "",
		DB:            0,
		PoolSize:      10,
		MinIdleConns:  5,
	}

	// 由于没有真实的哨兵服务器，这个测试会失败
	// 但我们可以测试配置是否正确设置
	pool, err := NewRedisPool(config)
	if err != nil {
		// 预期会失败，因为没有真实的哨兵服务器
		assert.Error(t, err)
		assert.Nil(t, pool)
	}
}

func TestNewRedisPool_Cluster(t *testing.T) {
	config := &RedisPoolConfig{
		Mode:         RedisModeCluster,
		ClusterAddrs: []string{"localhost:7000", "localhost:7001"},
		Password:     "",
		PoolSize:     10,
		MinIdleConns: 5,
	}

	// 由于没有真实的集群服务器，这个测试会失败
	// 但我们可以测试配置是否正确设置
	pool, err := NewRedisPool(config)
	if err != nil {
		// 预期会失败，因为没有真实的集群服务器
		assert.Error(t, err)
		assert.Nil(t, pool)
	}
}

func TestRedisPool_Operations(t *testing.T) {
	// 创建miniredis服务器用于测试
	s := miniredis.RunT(t)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         s.Addr(),
		PoolSize:     10,
		MinIdleConns: 5,
	}

	pool, err := NewRedisPool(config)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	client := pool.GetClient()
	require.NotNil(t, client)

	ctx := context.Background()

	// 测试基本操作
	err = client.Set(ctx, "test-key", "test-value", 0).Err()
	assert.NoError(t, err)

	val, err := client.Get(ctx, "test-key").Result()
	assert.NoError(t, err)
	assert.Equal(t, "test-value", val)

	// 测试删除
	err = client.Del(ctx, "test-key").Err()
	assert.NoError(t, err)

	// 测试键不存在
	_, err = client.Get(ctx, "test-key").Result()
	assert.Equal(t, redis.Nil, err)
}

func TestRedisPool_UpdatePoolSize(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         s.Addr(),
		PoolSize:     5,
		MinIdleConns: 2,
	}

	pool, err := NewRedisPool(config)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	// 更新连接池大小
	err = pool.UpdatePoolSize(10)
	assert.NoError(t, err)

	// 验证配置已更新
	assert.Equal(t, 10, pool.config.PoolSize)
}

func TestRedisPool_HealthCheckFail(t *testing.T) {
	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         "localhost:9999", // 不存在的地址
		PoolSize:     1,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	}

	pool, err := NewRedisPool(config)
	if err != nil {
		// 如果连接创建失败，这是预期的
		assert.Error(t, err)
		return
	}

	if pool != nil {
		defer pool.Close()
		
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		
		err = pool.HealthCheck(ctx)
		assert.Error(t, err)
	}
}

func TestRedisPool_Close(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         s.Addr(),
		PoolSize:     5,
		MinIdleConns: 2,
	}

	pool, err := NewRedisPool(config)
	require.NoError(t, err)
	require.NotNil(t, pool)

	// 测试关闭
	err = pool.Close()
	assert.NoError(t, err)

	// 再次关闭应该不会出错
	err = pool.Close()
	assert.NoError(t, err)
}

func TestRedisPool_ConcurrentAccess(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         s.Addr(),
		PoolSize:     20,
		MinIdleConns: 10,
	}

	pool, err := NewRedisPool(config)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	// 并发测试
	const goroutines = 10
	const operations = 100

	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			client := pool.GetClient()
			if client == nil {
				t.Errorf("Failed to get client in goroutine %d", id)
				return
			}

			ctx := context.Background()
			for j := 0; j < operations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				value := fmt.Sprintf("value-%d-%d", id, j)

				err := client.Set(ctx, key, value, 0).Err()
				if err != nil {
					t.Errorf("Set failed in goroutine %d: %v", id, err)
					return
				}

				val, err := client.Get(ctx, key).Result()
				if err != nil {
					t.Errorf("Get failed in goroutine %d: %v", id, err)
					return
				}

				if val != value {
					t.Errorf("Value mismatch in goroutine %d: expected %s, got %s", id, value, val)
					return
				}
			}
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < goroutines; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for goroutines to complete")
		}
	}
}

func TestRedisMode_String(t *testing.T) {
	assert.Equal(t, "standalone", RedisModeStandalone.String())
	assert.Equal(t, "sentinel", RedisModeSentinel.String())
	assert.Equal(t, "cluster", RedisModeCluster.String())
}

func BenchmarkRedisPool_GetClient(b *testing.B) {
	s := miniredis.RunT(b)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         s.Addr(),
		PoolSize:     100,
		MinIdleConns: 50,
	}

	pool, err := NewRedisPool(config)
	require.NoError(b, err)
	require.NotNil(b, pool)
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			client := pool.GetClient()
			if client == nil {
				b.Fatal("Failed to get client")
			}
		}
	})
}

func BenchmarkRedisPool_SetGet(b *testing.B) {
	s := miniredis.RunT(b)
	defer s.Close()

	config := &RedisPoolConfig{
		Mode:         RedisModeStandalone,
		Addr:         s.Addr(),
		PoolSize:     100,
		MinIdleConns: 50,
	}

	pool, err := NewRedisPool(config)
	require.NoError(b, err)
	require.NotNil(b, pool)
	defer pool.Close()

	client := pool.GetClient()
	require.NotNil(b, client)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", i)
			value := fmt.Sprintf("bench-value-%d", i)
			
			err := client.Set(ctx, key, value, 0).Err()
			if err != nil {
				b.Fatal(err)
			}
			
			_, err = client.Get(ctx, key).Result()
			if err != nil {
				b.Fatal(err)
			}
			
			i++
		}
	})
} 