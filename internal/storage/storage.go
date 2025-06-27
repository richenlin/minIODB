package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/pool"

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
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64) error
	GetObject(ctx context.Context, bucketName, objectName string) (*minio.Object, error)
	ListObjects(ctx context.Context, bucketName string, prefix string) <-chan minio.ObjectInfo
	DeleteObject(ctx context.Context, bucketName, objectName string) error

	// Health check
	HealthCheck(ctx context.Context) error

	// Close connections
	Close() error

	// Get pool manager
	GetPoolManager() *pool.PoolManager

	// Get stats
	GetStats() map[string]interface{}

	// Redis mode detection
	GetRedisMode() string
	IsRedisCluster() bool
	IsRedisSentinel() bool
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
		Redis: &pool.RedisPoolConfig{
			Mode:             pool.RedisMode(cfg.Redis.Mode),
			Addr:             cfg.Redis.Addr,
			Password:         cfg.Redis.Password,
			DB:               cfg.Redis.DB,
			MasterName:       cfg.Redis.MasterName,
			SentinelAddrs:    cfg.Redis.SentinelAddrs,
			SentinelPassword: cfg.Redis.SentinelPassword,
			ClusterAddrs:     cfg.Redis.ClusterAddrs,
			PoolSize:         cfg.Redis.PoolSize,
			MinIdleConns:     cfg.Redis.MinIdleConns,
			MaxConnAge:       cfg.Redis.MaxConnAge,
			PoolTimeout:      cfg.Redis.PoolTimeout,
			IdleTimeout:      cfg.Redis.IdleTimeout,
			IdleCheckFreq:    time.Minute, // 默认值
			DialTimeout:      cfg.Redis.DialTimeout,
			ReadTimeout:      cfg.Redis.ReadTimeout,
			WriteTimeout:     cfg.Redis.WriteTimeout,
			MaxRetries:       3,                      // 默认值
			MinRetryBackoff:  8 * time.Millisecond,   // 默认值
			MaxRetryBackoff:  512 * time.Millisecond, // 默认值
			MaxRedirects:     cfg.Redis.MaxRedirects,
			ReadOnly:         cfg.Redis.ReadOnly,
			RouteByLatency:   false, // 默认值
			RouteRandomly:    false, // 默认值
		},
		MinIO: &pool.MinIOPoolConfig{
			Endpoint:              cfg.MinIO.Endpoint,
			AccessKeyID:           cfg.MinIO.AccessKeyID,
			SecretAccessKey:       cfg.MinIO.SecretAccessKey,
			UseSSL:                cfg.MinIO.UseSSL,
			Region:                "us-east-1",      // 默认值
			MaxIdleConns:          100,              // 默认值
			MaxIdleConnsPerHost:   10,               // 默认值
			MaxConnsPerHost:       0,                // 默认值
			IdleConnTimeout:       90 * time.Second, // 默认值
			DialTimeout:           30 * time.Second, // 默认值
			TLSHandshakeTimeout:   10 * time.Second, // 默认值
			ResponseHeaderTimeout: 0,                // 默认值
			ExpectContinueTimeout: 1 * time.Second,  // 默认值
			MaxRetries:            3,                // 默认值
			RetryDelay:            1 * time.Second,  // 默认值
			RequestTimeout:        0,                // 默认值
			KeepAlive:             30 * time.Second, // 默认值
			DisableKeepAlive:      false,            // 默认值
			DisableCompression:    false,            // 默认值
		},
		HealthCheckInterval: 30 * time.Second, // 默认值
	}

	// 如果配置了备份MinIO，添加到配置中
	if cfg.Backup.Enabled && cfg.Backup.MinIO.Endpoint != "" {
		poolConfig.BackupMinIO = &pool.MinIOPoolConfig{
			Endpoint:              cfg.Backup.MinIO.Endpoint,
			AccessKeyID:           cfg.Backup.MinIO.AccessKeyID,
			SecretAccessKey:       cfg.Backup.MinIO.SecretAccessKey,
			UseSSL:                cfg.Backup.MinIO.UseSSL,
			Region:                "us-east-1",      // 默认值
			MaxIdleConns:          100,              // 默认值
			MaxIdleConnsPerHost:   10,               // 默认值
			MaxConnsPerHost:       0,                // 默认值
			IdleConnTimeout:       90 * time.Second, // 默认值
			DialTimeout:           30 * time.Second, // 默认值
			TLSHandshakeTimeout:   10 * time.Second, // 默认值
			ResponseHeaderTimeout: 0,                // 默认值
			ExpectContinueTimeout: 1 * time.Second,  // 默认值
			MaxRetries:            3,                // 默认值
			RetryDelay:            1 * time.Second,  // 默认值
			RequestTimeout:        0,                // 默认值
			KeepAlive:             30 * time.Second, // 默认值
			DisableKeepAlive:      false,            // 默认值
			DisableCompression:    false,            // 默认值
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
func (s *StorageImpl) Get(ctx context.Context, key string) (string, error) {
	redisPool := s.poolManager.GetRedisPool()
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
func (s *StorageImpl) Del(ctx context.Context, keys ...string) error {
	redisPool := s.poolManager.GetRedisPool()
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

// SAdd 添加集合成员
func (s *StorageImpl) SAdd(ctx context.Context, key string, members ...interface{}) error {
	redisPool := s.poolManager.GetRedisPool()
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

// SMembers 获取集合成员
func (s *StorageImpl) SMembers(ctx context.Context, key string) ([]string, error) {
	redisPool := s.poolManager.GetRedisPool()
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

// Exists 检查键是否存在
func (s *StorageImpl) Exists(ctx context.Context, keys ...string) (int64, error) {
	redisPool := s.poolManager.GetRedisPool()
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

// MinIO operations

// PutObject 上传对象
func (s *StorageImpl) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64) error {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端不可用")
	}

	_, err := client.PutObject(ctx, bucketName, objectName, reader, objectSize, minio.PutObjectOptions{})
	return err
}

// GetObject 获取对象
func (s *StorageImpl) GetObject(ctx context.Context, bucketName, objectName string) (*minio.Object, error) {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return nil, fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("MinIO客户端不可用")
	}

	return client.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
}

// ListObjects 列出对象
func (s *StorageImpl) ListObjects(ctx context.Context, bucketName string, prefix string) <-chan minio.ObjectInfo {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		// 返回空channel
		ch := make(chan minio.ObjectInfo)
		close(ch)
		return ch
	}

	client := minioPool.GetClient()
	if client == nil {
		// 返回空channel
		ch := make(chan minio.ObjectInfo)
		close(ch)
		return ch
	}

	return client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
}

// DeleteObject 删除对象
func (s *StorageImpl) DeleteObject(ctx context.Context, bucketName, objectName string) error {
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端不可用")
	}

	return client.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
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
func (s *StorageImpl) GetStats() map[string]interface{} {
	if s.poolManager == nil {
		return nil
	}
	return s.poolManager.GetStats()
}

// Redis mode detection methods

// GetRedisMode 获取Redis模式
func (s *StorageImpl) GetRedisMode() string {
	redisPool := s.poolManager.GetRedisPool()
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
func (s *StorageImpl) IsRedisCluster() bool {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return false
	}
	return redisPool.GetMode() == pool.RedisModeCluster
}

// IsRedisSentinel 检查是否为哨兵模式
func (s *StorageImpl) IsRedisSentinel() bool {
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return false
	}
	return redisPool.GetMode() == pool.RedisModeSentinel
}
