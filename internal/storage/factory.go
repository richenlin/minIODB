package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/pool"
)

// StorageFactoryImpl 存储工厂实现
type StorageFactoryImpl struct {
	poolManager *pool.PoolManager
	config      *config.Config
}

// NewStorageFactory 创建新的存储工厂
func NewStorageFactory(cfg *config.Config) (StorageFactory, error) {
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

	factory := &StorageFactoryImpl{
		poolManager: poolManager,
		config:      cfg,
	}

	log.Println("Storage factory initialized with connection pools")
	return factory, nil
}

// CreateCacheStorage 创建缓存存储实例
func (f *StorageFactoryImpl) CreateCacheStorage() (CacheStorage, error) {
	if f.poolManager == nil {
		return nil, fmt.Errorf("pool manager not available")
	}

	return NewCacheStorage(f.poolManager), nil
}

// CreateObjectStorage 创建对象存储实例
func (f *StorageFactoryImpl) CreateObjectStorage() (ObjectStorage, error) {
	if f.poolManager == nil {
		return nil, fmt.Errorf("pool manager not available")
	}

	return NewObjectStorage(f.poolManager), nil
}

// CreateBackupObjectStorage 创建备份对象存储实例
func (f *StorageFactoryImpl) CreateBackupObjectStorage() (ObjectStorage, error) {
	if f.poolManager == nil {
		return nil, fmt.Errorf("pool manager not available")
	}

	// 检查是否配置了备份存储
	if f.poolManager.GetBackupMinIOPool() == nil {
		return nil, fmt.Errorf("backup storage not configured")
	}

	return NewBackupObjectStorage(f.poolManager), nil
}

// GetPoolManager 获取连接池管理器
func (f *StorageFactoryImpl) GetPoolManager() *pool.PoolManager {
	return f.poolManager
}

// Close 关闭工厂和所有连接
func (f *StorageFactoryImpl) Close() error {
	if f.poolManager != nil {
		return f.poolManager.Close()
	}
	return nil
}

// UnifiedStorageImpl 统一存储实现（向后兼容）
type UnifiedStorageImpl struct {
	CacheStorage
	ObjectStorage
	poolManager *pool.PoolManager
}

// NewUnifiedStorage 创建统一存储实例（向后兼容）
func NewUnifiedStorage(cfg *config.Config) (Storage, error) {
	factory, err := NewStorageFactory(cfg)
	if err != nil {
		return nil, err
	}

	cacheStorage, err := factory.CreateCacheStorage()
	if err != nil {
		factory.Close()
		return nil, fmt.Errorf("failed to create cache storage: %w", err)
	}

	objectStorage, err := factory.CreateObjectStorage()
	if err != nil {
		factory.Close()
		return nil, fmt.Errorf("failed to create object storage: %w", err)
	}

	return &UnifiedStorageImpl{
		CacheStorage:  cacheStorage,
		ObjectStorage: objectStorage,
		poolManager:   factory.GetPoolManager(),
	}, nil
}

// GetPoolManager 获取连接池管理器（统一存储接口）
func (u *UnifiedStorageImpl) GetPoolManager() *pool.PoolManager {
	return u.poolManager
}

// HealthCheck 健康检查（解决模糊选择器问题）
func (u *UnifiedStorageImpl) HealthCheck(ctx context.Context) error {
	// 检查缓存存储健康状态
	if u.CacheStorage != nil {
		if err := u.CacheStorage.HealthCheck(ctx); err != nil {
			return fmt.Errorf("cache storage health check failed: %w", err)
		}
	}

	// 检查对象存储健康状态
	if u.ObjectStorage != nil {
		if err := u.ObjectStorage.HealthCheck(ctx); err != nil {
			return fmt.Errorf("object storage health check failed: %w", err)
		}
	}

	return nil
}

// GetStats 获取统计信息（解决模糊选择器问题）
func (u *UnifiedStorageImpl) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// 合并缓存存储统计信息
	if u.CacheStorage != nil {
		cacheStats := u.CacheStorage.GetStats()
		for k, v := range cacheStats {
			stats["cache_"+k] = v
		}
	}

	// 合并对象存储统计信息
	if u.ObjectStorage != nil {
		objectStats := u.ObjectStorage.GetStats()
		for k, v := range objectStats {
			stats["object_"+k] = v
		}
	}

	// 添加连接池管理器统计信息
	if u.poolManager != nil {
		poolStats := u.poolManager.GetStats()
		stats["pool"] = poolStats
	}

	return stats
}

// Close 关闭统一存储（解决模糊选择器问题）
func (u *UnifiedStorageImpl) Close() error {
	var err error

	// 关闭缓存存储
	if u.CacheStorage != nil {
		if closeErr := u.CacheStorage.Close(); closeErr != nil {
			err = closeErr
		}
	}

	// 关闭对象存储
	if u.ObjectStorage != nil {
		if closeErr := u.ObjectStorage.Close(); closeErr != nil {
			err = closeErr
		}
	}

	// 关闭连接池管理器
	if u.poolManager != nil {
		if closeErr := u.poolManager.Close(); closeErr != nil {
			err = closeErr
		}
	}

	return err
}
