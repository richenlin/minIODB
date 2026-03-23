package storage

import (
	"context"
	"fmt"
	"time"

	"minIODB/config"
	"minIODB/pkg/pool"

	"go.uber.org/zap"
)

// StorageFactoryImpl 存储工厂实现
type StorageFactoryImpl struct {
	poolManager *pool.PoolManager
	config      *config.Config
}

// NewStorageFactory 创建新的存储工厂
func NewStorageFactory(cfg *config.Config, logger *zap.Logger) (StorageFactory, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// 创建连接池管理器配置
	poolConfig := &pool.PoolManagerConfig{
		MinIO:               getEnhancedMinIOPoolConfig(cfg),
		HealthCheckInterval: getHealthCheckInterval(cfg),
	}

	// 只有在Redis启用时才配置Redis连接池
	if cfg.Redis.Enabled {
		// 获取优化的Redis配置（优先使用新的网络配置）
		redisConfig := getEnhancedRedisConfig(cfg)

		poolConfig.Redis = &pool.RedisPoolConfig{
			Mode:             pool.RedisMode(redisConfig.Mode),
			Addr:             redisConfig.Addr,
			Password:         redisConfig.Password,
			DB:               redisConfig.DB,
			MasterName:       redisConfig.MasterName,
			SentinelAddrs:    redisConfig.SentinelAddrs,
			SentinelPassword: redisConfig.SentinelPassword,
			ClusterAddrs:     redisConfig.ClusterAddrs,
			PoolSize:         redisConfig.PoolSize,
			MinIdleConns:     redisConfig.MinIdleConns,
			MaxConnAge:       redisConfig.MaxConnAge,
			PoolTimeout:      redisConfig.PoolTimeout,
			IdleTimeout:      redisConfig.IdleTimeout,
			IdleCheckFreq:    redisConfig.IdleCheckFreq,
			DialTimeout:      redisConfig.DialTimeout,
			ReadTimeout:      redisConfig.ReadTimeout,
			WriteTimeout:     redisConfig.WriteTimeout,
			MaxRetries:       redisConfig.MaxRetries,
			MinRetryBackoff:  redisConfig.MinRetryBackoff,
			MaxRetryBackoff:  redisConfig.MaxRetryBackoff,
			MaxRedirects:     redisConfig.MaxRedirects,
			ReadOnly:         redisConfig.ReadOnly,
			RouteByLatency:   redisConfig.RouteByLatency,
			RouteRandomly:    redisConfig.RouteRandomly,
		}
	}

	// 如果配置了备份MinIO，添加到配置中（不依赖 backup.enabled，replication 等功能也需要备份池）
	if cfg.GetBackupMinIO().Endpoint != "" {
		poolConfig.BackupMinIO = getBackupMinIOPoolConfig(cfg)
	}

	// 添加Failover配置
	poolConfig.Failover = pool.FailoverConfig{
		Enabled:             cfg.Network.Pools.Failover.Enabled,
		HealthCheckInterval: cfg.Network.Pools.Failover.HealthCheckInterval,
		AsyncSync: pool.AsyncSyncConfig{
			QueueSize:     cfg.Network.Pools.Failover.AsyncSync.QueueSize,
			WorkerCount:   cfg.Network.Pools.Failover.AsyncSync.WorkerCount,
			RetryTimes:    cfg.Network.Pools.Failover.AsyncSync.RetryTimes,
			RetryInterval: cfg.Network.Pools.Failover.AsyncSync.RetryInterval,
			SyncTimeout:   cfg.Network.Pools.Failover.AsyncSync.SyncTimeout,
		},
	}

	// 创建连接池管理器
	poolManager, err := pool.NewPoolManager(poolConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool manager: %w", err)
	}

	factory := &StorageFactoryImpl{
		poolManager: poolManager,
		config:      cfg,
	}

	if cfg.Redis.Enabled {
		logger.Info("Storage factory initialized with Redis and MinIO connection pools")
	} else {
		logger.Info("Storage factory initialized with MinIO connection pool only (Redis disabled)")
	}
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
	logger      *zap.Logger
}

// NewUnifiedStorage 创建统一存储实例（向后兼容）
func NewUnifiedStorage(cfg *config.Config, logger *zap.Logger) (Storage, error) {
	factory, err := NewStorageFactory(cfg, logger)
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
		logger:        logger,
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

// Helper functions for enhanced configurations

// getEnhancedRedisConfig 获取增强的Redis配置，优先使用新的网络配置
func getEnhancedRedisConfig(cfg *config.Config) *config.EnhancedRedisConfig {
	// 检查是否有新的网络配置
	if cfg.Network.Pools.Redis.PoolSize > 0 {
		return &cfg.Network.Pools.Redis
	}

	// 回退到旧配置，转换为增强配置格式
	return &config.EnhancedRedisConfig{
		Mode:             cfg.Redis.Mode,
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
	}
}

// getEnhancedMinIOPoolConfig 获取增强的MinIO池配置（统一使用 Network.Pools.MinIO）
func getEnhancedMinIOPoolConfig(cfg *config.Config) *pool.MinIOPoolConfig {
	minioConfig := &cfg.Network.Pools.MinIO
	// 若 Pools 中已配置连接池参数则使用，否则用统一配置 + 池默认值
	if minioConfig.Endpoint != "" {
		return &pool.MinIOPoolConfig{
			Endpoint:              minioConfig.Endpoint,
			AccessKeyID:           minioConfig.AccessKeyID,
			SecretAccessKey:       minioConfig.SecretAccessKey,
			UseSSL:                minioConfig.UseSSL,
			Region:                minioConfig.Region,
			MaxIdleConns:          minioConfig.MaxIdleConns,
			MaxIdleConnsPerHost:   minioConfig.MaxIdleConnsPerHost,
			MaxConnsPerHost:       minioConfig.MaxConnsPerHost,
			IdleConnTimeout:       minioConfig.IdleConnTimeout,
			DialTimeout:           minioConfig.DialTimeout,
			TLSHandshakeTimeout:   minioConfig.TLSHandshakeTimeout,
			ResponseHeaderTimeout: minioConfig.ResponseHeaderTimeout,
			ExpectContinueTimeout: minioConfig.ExpectContinueTimeout,
			MaxRetries:            minioConfig.MaxRetries,
			RetryDelay:            minioConfig.RetryDelay,
			RequestTimeout:        minioConfig.RequestTimeout,
			KeepAlive:             minioConfig.KeepAlive,
			DisableKeepAlive:      minioConfig.DisableKeepAlive,
			DisableCompression:    minioConfig.DisableCompression,
		}
	}
	// 仅当 Pools 未配置时回退到统一配置（GetMinIO）+ 池默认值
	m := cfg.GetMinIO()
	return &pool.MinIOPoolConfig{
		Endpoint:              m.Endpoint,
		AccessKeyID:           m.AccessKeyID,
		SecretAccessKey:       m.SecretAccessKey,
		UseSSL:                m.UseSSL,
		Region:                "us-east-1",            // 默认值
		MaxIdleConns:          300,                    // 优化默认值
		MaxIdleConnsPerHost:   150,                    // 优化默认值
		MaxConnsPerHost:       300,                    // 优化默认值
		IdleConnTimeout:       90 * time.Second,       // 默认值
		DialTimeout:           5 * time.Second,        // 优化超时
		TLSHandshakeTimeout:   5 * time.Second,        // 优化超时
		ResponseHeaderTimeout: 15 * time.Second,       // 优化超时
		ExpectContinueTimeout: 1 * time.Second,        // 默认值
		MaxRetries:            3,                      // 默认值
		RetryDelay:            100 * time.Millisecond, // 优化重试延迟
		RequestTimeout:        60 * time.Second,       // 优化超时
		KeepAlive:             30 * time.Second,       // 默认值
		DisableKeepAlive:      false,                  // 默认值
		DisableCompression:    false,                  // 默认值
	}
}

// getBackupMinIOPoolConfig 获取备份MinIO池配置（统一使用 Network.Pools.BackupMinIO / GetBackupMinIO）
func getBackupMinIOPoolConfig(cfg *config.Config) *pool.MinIOPoolConfig {
	if cfg.Network.Pools.BackupMinIO != nil && cfg.Network.Pools.BackupMinIO.Endpoint != "" {
		backupConfig := cfg.Network.Pools.BackupMinIO
		return &pool.MinIOPoolConfig{
			Endpoint:              backupConfig.Endpoint,
			AccessKeyID:           backupConfig.AccessKeyID,
			SecretAccessKey:       backupConfig.SecretAccessKey,
			UseSSL:                backupConfig.UseSSL,
			Region:                backupConfig.Region,
			MaxIdleConns:          backupConfig.MaxIdleConns,
			MaxIdleConnsPerHost:   backupConfig.MaxIdleConnsPerHost,
			MaxConnsPerHost:       backupConfig.MaxConnsPerHost,
			IdleConnTimeout:       backupConfig.IdleConnTimeout,
			DialTimeout:           backupConfig.DialTimeout,
			TLSHandshakeTimeout:   backupConfig.TLSHandshakeTimeout,
			ResponseHeaderTimeout: backupConfig.ResponseHeaderTimeout,
			ExpectContinueTimeout: backupConfig.ExpectContinueTimeout,
			MaxRetries:            backupConfig.MaxRetries,
			RetryDelay:            backupConfig.RetryDelay,
			RequestTimeout:        backupConfig.RequestTimeout,
			KeepAlive:             backupConfig.KeepAlive,
			DisableKeepAlive:      backupConfig.DisableKeepAlive,
			DisableCompression:    backupConfig.DisableCompression,
		}
	}
	m := cfg.GetBackupMinIO()
	return &pool.MinIOPoolConfig{
		Endpoint:              m.Endpoint,
		AccessKeyID:           m.AccessKeyID,
		SecretAccessKey:       m.SecretAccessKey,
		UseSSL:                m.UseSSL,
		Region:                "us-east-1",            // 默认值
		MaxIdleConns:          200,                    // 备份可以少一些连接
		MaxIdleConnsPerHost:   100,                    // 备份可以少一些连接
		MaxConnsPerHost:       200,                    // 备份可以少一些连接
		IdleConnTimeout:       90 * time.Second,       // 默认值
		DialTimeout:           5 * time.Second,        // 优化超时
		TLSHandshakeTimeout:   5 * time.Second,        // 优化超时
		ResponseHeaderTimeout: 15 * time.Second,       // 优化超时
		ExpectContinueTimeout: 1 * time.Second,        // 默认值
		MaxRetries:            3,                      // 默认值
		RetryDelay:            100 * time.Millisecond, // 优化重试延迟
		RequestTimeout:        60 * time.Second,       // 优化超时
		KeepAlive:             30 * time.Second,       // 默认值
		DisableKeepAlive:      false,                  // 默认值
		DisableCompression:    false,                  // 默认值
	}
}

// getHealthCheckInterval 获取健康检查间隔
func getHealthCheckInterval(cfg *config.Config) time.Duration {
	if cfg.Network.Pools.HealthCheckInterval > 0 {
		return cfg.Network.Pools.HealthCheckInterval
	}

	return 15 * time.Second
}

// NewStorage 创建新的存储实例（从 storage.go 迁移）
func NewStorage(cfg *config.Config, logger *zap.Logger) (Storage, error) {
	logger.Info("Warning: NewStorage is deprecated, consider using NewStorageFactory for better architecture")
	return NewUnifiedStorage(cfg, logger)
}
