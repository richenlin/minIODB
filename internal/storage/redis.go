package storage

import (
	"context"
	"time"

	"minIODB/config"
	"minIODB/internal/metrics"

	"github.com/go-redis/redis/v8"
)

// RedisClient Redis客户端封装
type RedisClient struct {
	client *redis.Client
}

// NewRedisClient 创建新的Redis客户端
func NewRedisClient(cfg config.RedisConfig) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisClient{
		client: client,
	}, nil
}

// GetClient 获取原始Redis客户端
func (r *RedisClient) GetClient() *redis.Client {
	return r.client
}

// Ping 测试连接
func (r *RedisClient) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Set 设置键值对
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	redisMetrics := metrics.NewRedisMetrics("Set")
	
	err := r.client.Set(ctx, key, value, expiration).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// Get 获取键对应的值
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	redisMetrics := metrics.NewRedisMetrics("Get")
	
	result, err := r.client.Get(ctx, key).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return "", err
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// Del 删除键
func (r *RedisClient) Del(ctx context.Context, keys ...string) error {
	redisMetrics := metrics.NewRedisMetrics("Del")
	
	err := r.client.Del(ctx, keys...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// HSet 设置哈希字段
func (r *RedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	redisMetrics := metrics.NewRedisMetrics("HSet")
	
	err := r.client.HSet(ctx, key, values...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// HGet 获取哈希字段值
func (r *RedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	redisMetrics := metrics.NewRedisMetrics("HGet")
	
	result, err := r.client.HGet(ctx, key, field).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return "", err
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// HGetAll 获取哈希所有字段
func (r *RedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	redisMetrics := metrics.NewRedisMetrics("HGetAll")
	
	result, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return nil, err
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// HDel 删除哈希字段
func (r *RedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	redisMetrics := metrics.NewRedisMetrics("HDel")
	
	err := r.client.HDel(ctx, key, fields...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// SAdd 向集合添加成员
func (r *RedisClient) SAdd(ctx context.Context, key string, members ...interface{}) error {
	redisMetrics := metrics.NewRedisMetrics("SAdd")
	
	err := r.client.SAdd(ctx, key, members...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// SMembers 获取集合所有成员
func (r *RedisClient) SMembers(ctx context.Context, key string) ([]string, error) {
	redisMetrics := metrics.NewRedisMetrics("SMembers")
	
	result, err := r.client.SMembers(ctx, key).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return nil, err
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// SRem 从集合移除成员
func (r *RedisClient) SRem(ctx context.Context, key string, members ...interface{}) error {
	redisMetrics := metrics.NewRedisMetrics("SRem")
	
	err := r.client.SRem(ctx, key, members...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// Expire 设置键的过期时间
func (r *RedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	redisMetrics := metrics.NewRedisMetrics("Expire")
	
	err := r.client.Expire(ctx, key, expiration).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return err
	}
	
	redisMetrics.Finish("success")
	return nil
}

// Close 关闭连接
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// Keys 获取匹配模式的键列表
func (r *RedisClient) Keys(ctx context.Context, pattern string) ([]string, error) {
	redisMetrics := metrics.NewRedisMetrics("Keys")
	
	result, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return nil, err
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// Info 获取Redis服务器信息
func (r *RedisClient) Info(ctx context.Context) (string, error) {
	redisMetrics := metrics.NewRedisMetrics("Info")
	
	result, err := r.client.Info(ctx).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return "", err
	}
	
	redisMetrics.Finish("success")
	return result, nil
} 