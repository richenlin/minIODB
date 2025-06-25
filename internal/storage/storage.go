package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/metrics"
	"minIODB/internal/pool"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
)

// Storage 存储接口
type Storage interface {
	// Redis operations
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	SAdd(ctx context.Context, key string, members ...interface{}) error
	SMembers(ctx context.Context, key string) ([]string, error)
	Exists(ctx context.Context, keys ...string) (int64, error)
	
	// MinIO operations
	PutObject(ctx context.Context, bucketName, objectName, filePath string) error
	GetObject(ctx context.Context, bucketName, objectName, filePath string) error
	ListObjects(ctx context.Context, bucketName, prefix string) ([]string, error)
	DeleteObject(ctx context.Context, bucketName, objectName string) error
	
	// Health check
	HealthCheck(ctx context.Context) error
	
	// Close connections
	Close() error
}

// StorageImpl 存储实现
type StorageImpl struct {
	poolManager *pool.PoolManager
	config      *config.Config
}

// NewStorage 创建新的存储实例
func NewStorage(cfg *config.Config) (Storage, error) {
	// 创建连接池管理器配置
	poolConfig := &pool.PoolManagerConfig{
		Redis: pool.RedisPoolConfig{
			Addr:            cfg.Pool.Redis.Addr,
			Password:        cfg.Pool.Redis.Password,
			DB:              cfg.Pool.Redis.DB,
			PoolSize:        cfg.Pool.Redis.PoolSize,
			MinIdleConns:    cfg.Pool.Redis.MinIdleConns,
			MaxConnAge:      cfg.Pool.Redis.MaxConnAge,
			PoolTimeout:     cfg.Pool.Redis.PoolTimeout,
			IdleTimeout:     cfg.Pool.Redis.IdleTimeout,
			IdleCheckFreq:   cfg.Pool.Redis.IdleCheckFreq,
			DialTimeout:     cfg.Pool.Redis.DialTimeout,
			ReadTimeout:     cfg.Pool.Redis.ReadTimeout,
			WriteTimeout:    cfg.Pool.Redis.WriteTimeout,
			MaxRetries:      cfg.Pool.Redis.MaxRetries,
			MinRetryBackoff: cfg.Pool.Redis.MinRetryBackoff,
			MaxRetryBackoff: cfg.Pool.Redis.MaxRetryBackoff,
		},
		MinIO: pool.MinIOPoolConfig{
			Endpoint:               cfg.Pool.MinIO.Endpoint,
			AccessKeyID:            cfg.Pool.MinIO.AccessKeyID,
			SecretAccessKey:        cfg.Pool.MinIO.SecretAccessKey,
			UseSSL:                 cfg.Pool.MinIO.UseSSL,
			Region:                 cfg.Pool.MinIO.Region,
			MaxIdleConns:           cfg.Pool.MinIO.MaxIdleConns,
			MaxIdleConnsPerHost:    cfg.Pool.MinIO.MaxIdleConnsPerHost,
			MaxConnsPerHost:        cfg.Pool.MinIO.MaxConnsPerHost,
			IdleConnTimeout:        cfg.Pool.MinIO.IdleConnTimeout,
			DialTimeout:            cfg.Pool.MinIO.DialTimeout,
			TLSHandshakeTimeout:    cfg.Pool.MinIO.TLSHandshakeTimeout,
			ResponseHeaderTimeout:  cfg.Pool.MinIO.ResponseHeaderTimeout,
			ExpectContinueTimeout:  cfg.Pool.MinIO.ExpectContinueTimeout,
			MaxRetries:             cfg.Pool.MinIO.MaxRetries,
			RetryDelay:             cfg.Pool.MinIO.RetryDelay,
			RequestTimeout:         cfg.Pool.MinIO.RequestTimeout,
			KeepAlive:              cfg.Pool.MinIO.KeepAlive,
			DisableKeepAlive:       cfg.Pool.MinIO.DisableKeepAlive,
			DisableCompression:     cfg.Pool.MinIO.DisableCompression,
		},
		HealthCheckInterval: cfg.Pool.HealthCheckInterval,
	}
	
	// 如果配置了备份MinIO，添加到配置中
	if cfg.Pool.BackupMinIO != nil {
		poolConfig.BackupMinIO = &pool.MinIOPoolConfig{
			Endpoint:               cfg.Pool.BackupMinIO.Endpoint,
			AccessKeyID:            cfg.Pool.BackupMinIO.AccessKeyID,
			SecretAccessKey:        cfg.Pool.BackupMinIO.SecretAccessKey,
			UseSSL:                 cfg.Pool.BackupMinIO.UseSSL,
			Region:                 cfg.Pool.BackupMinIO.Region,
			MaxIdleConns:           cfg.Pool.BackupMinIO.MaxIdleConns,
			MaxIdleConnsPerHost:    cfg.Pool.BackupMinIO.MaxIdleConnsPerHost,
			MaxConnsPerHost:        cfg.Pool.BackupMinIO.MaxConnsPerHost,
			IdleConnTimeout:        cfg.Pool.BackupMinIO.IdleConnTimeout,
			DialTimeout:            cfg.Pool.BackupMinIO.DialTimeout,
			TLSHandshakeTimeout:    cfg.Pool.BackupMinIO.TLSHandshakeTimeout,
			ResponseHeaderTimeout:  cfg.Pool.BackupMinIO.ResponseHeaderTimeout,
			ExpectContinueTimeout:  cfg.Pool.BackupMinIO.ExpectContinueTimeout,
			MaxRetries:             cfg.Pool.BackupMinIO.MaxRetries,
			RetryDelay:             cfg.Pool.BackupMinIO.RetryDelay,
			RequestTimeout:         cfg.Pool.BackupMinIO.RequestTimeout,
			KeepAlive:              cfg.Pool.BackupMinIO.KeepAlive,
			DisableKeepAlive:       cfg.Pool.BackupMinIO.DisableKeepAlive,
			DisableCompression:     cfg.Pool.BackupMinIO.DisableCompression,
		}
	}

	// 创建连接池管理器
	poolManager, err := pool.NewPoolManager(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool manager: %w", err)
	}

	storage := &StorageImpl{
		poolManager: poolManager,
		config:      cfg,
	}

	log.Println("Storage initialized with connection pools")
	return storage, nil
}

// Redis operations

// Set 设置键值对
func (s *StorageImpl) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisMetrics := metrics.NewRedisMetrics("set")
	
	err := client.Set(ctx, key, value, expiration).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}
	
	redisMetrics.Finish("success")
	return nil
}

// Get 获取键值
func (s *StorageImpl) Get(ctx context.Context, key string) (string, error) {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return "", fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisMetrics := metrics.NewRedisMetrics("get")
	
	result, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			redisMetrics.Finish("not_found")
			return "", fmt.Errorf("key %s not found", key)
		}
		redisMetrics.Finish("error")
		return "", fmt.Errorf("failed to get key %s: %w", key, err)
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// Del 删除键
func (s *StorageImpl) Del(ctx context.Context, keys ...string) error {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisMetrics := metrics.NewRedisMetrics("del")
	
	err := client.Del(ctx, keys...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return fmt.Errorf("failed to delete keys: %w", err)
	}
	
	redisMetrics.Finish("success")
	return nil
}

// SAdd 添加集合成员
func (s *StorageImpl) SAdd(ctx context.Context, key string, members ...interface{}) error {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisMetrics := metrics.NewRedisMetrics("sadd")
	
	err := client.SAdd(ctx, key, members...).Err()
	if err != nil {
		redisMetrics.Finish("error")
		return fmt.Errorf("failed to add members to set %s: %w", key, err)
	}
	
	redisMetrics.Finish("success")
	return nil
}

// SMembers 获取集合成员
func (s *StorageImpl) SMembers(ctx context.Context, key string) ([]string, error) {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return nil, fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisMetrics := metrics.NewRedisMetrics("smembers")
	
	result, err := client.SMembers(ctx, key).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return nil, fmt.Errorf("failed to get members of set %s: %w", key, err)
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// Exists 检查键是否存在
func (s *StorageImpl) Exists(ctx context.Context, keys ...string) (int64, error) {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return 0, fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisMetrics := metrics.NewRedisMetrics("exists")
	
	result, err := client.Exists(ctx, keys...).Result()
	if err != nil {
		redisMetrics.Finish("error")
		return 0, fmt.Errorf("failed to check existence of keys: %w", err)
	}
	
	redisMetrics.Finish("success")
	return result, nil
}

// MinIO operations

// PutObject 上传对象
func (s *StorageImpl) PutObject(ctx context.Context, bucketName, objectName, filePath string) error {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	minioMetrics := metrics.NewMinIOMetrics("put_object")
	
	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		_, err := client.FPutObject(ctx, bucketName, objectName, filePath, minio.PutObjectOptions{})
		return err
	})
	
	if err != nil {
		minioMetrics.Finish("error")
		return fmt.Errorf("failed to put object %s: %w", objectName, err)
	}
	
	minioMetrics.Finish("success")
	return nil
}

// GetObject 下载对象
func (s *StorageImpl) GetObject(ctx context.Context, bucketName, objectName, filePath string) error {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	minioMetrics := metrics.NewMinIOMetrics("get_object")
	
	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		return client.FGetObject(ctx, bucketName, objectName, filePath, minio.GetObjectOptions{})
	})
	
	if err != nil {
		minioMetrics.Finish("error")
		return fmt.Errorf("failed to get object %s: %w", objectName, err)
	}
	
	minioMetrics.Finish("success")
	return nil
}

// ListObjects 列出对象
func (s *StorageImpl) ListObjects(ctx context.Context, bucketName, prefix string) ([]string, error) {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return nil, fmt.Errorf("MinIO pool not available")
	}

	var objects []string
	minioMetrics := metrics.NewMinIOMetrics("list_objects")
	
	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		objectCh := client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		})
		
		objects = objects[:0] // 清空切片但保留容量
		for object := range objectCh {
			if object.Err != nil {
				return object.Err
			}
			objects = append(objects, object.Key)
		}
		return nil
	})
	
	if err != nil {
		minioMetrics.Finish("error")
		return nil, fmt.Errorf("failed to list objects with prefix %s: %w", prefix, err)
	}
	
	minioMetrics.Finish("success")
	return objects, nil
}

// DeleteObject 删除对象
func (s *StorageImpl) DeleteObject(ctx context.Context, bucketName, objectName string) error {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	minioMetrics := metrics.NewMinIOMetrics("delete_object")
	
	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		return client.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
	})
	
	if err != nil {
		minioMetrics.Finish("error")
		return fmt.Errorf("failed to delete object %s: %w", objectName, err)
	}
	
	minioMetrics.Finish("success")
	return nil
}

// HealthCheck 健康检查
func (s *StorageImpl) HealthCheck(ctx context.Context) error {
	// 检查Redis连接池健康状态
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}
	
	if err := redisPool.HealthCheck(ctx); err != nil {
		return fmt.Errorf("Redis health check failed: %w", err)
	}

	// 检查MinIO连接池健康状态
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}
	
	if err := minioPool.HealthCheck(ctx); err != nil {
		return fmt.Errorf("MinIO health check failed: %w", err)
	}

	// 检查备份MinIO连接池健康状态（如果存在）
	if backupPool := s.poolManager.GetBackupMinIOPool(); backupPool != nil {
		if err := backupPool.HealthCheck(ctx); err != nil {
			log.Printf("WARN: Backup MinIO health check failed: %v", err)
		}
	}

	return nil
}

// Close 关闭连接
func (s *StorageImpl) Close() error {
	if s.poolManager != nil {
		return s.poolManager.Close()
	}
	return nil
}

// GetPoolManager 获取连接池管理器（用于其他组件）
func (s *StorageImpl) GetPoolManager() *pool.PoolManager {
	return s.poolManager
}

// GetStats 获取连接池统计信息
func (s *StorageImpl) GetStats() interface{} {
	if s.poolManager == nil {
		return nil
	}
	return s.poolManager.GetStats()
} 