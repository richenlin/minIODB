package storage

import (
	"context"
	"fmt"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/internal/pool"
)

// StorageFactoryImpl 存储工厂实现
type StorageFactoryImpl struct {
	poolManager *pool.PoolManager
	config      *config.Config
}

// NewStorageFactory 创建新的存储工厂
func NewStorageFactory(ctx context.Context, cfg *config.Config) (StorageFactory, error) {
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

	// 如果配置了备份MinIO，添加到配置中
	if cfg.Backup.Enabled && cfg.Backup.MinIO.Endpoint != "" {
		poolConfig.BackupMinIO = getBackupMinIOPoolConfig(cfg)
	}

	// 创建连接池管理器
	poolManager, err := pool.NewPoolManager(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool manager: %w", err)
	}

	factory := &StorageFactoryImpl{
		poolManager: poolManager,
		config:      cfg,
	}

	if cfg.Redis.Enabled {
		logger.LogInfo(ctx, "Storage factory initialized with Redis and MinIO connection pools")
	} else {
		logger.LogInfo(ctx, "Storage factory initialized with MinIO connection pool only (Redis disabled)")
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
func (f *StorageFactoryImpl) Close(ctx context.Context) error {
	if f.poolManager != nil {
		return f.poolManager.Close(ctx)
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
func NewUnifiedStorage(ctx context.Context, cfg *config.Config) (Storage, error) {
	factory, err := NewStorageFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}

	cacheStorage, err := factory.CreateCacheStorage()
	if err != nil {
		factory.Close(ctx)
		return nil, fmt.Errorf("failed to create cache storage: %w", err)
	}

	objectStorage, err := factory.CreateObjectStorage()
	if err != nil {
		factory.Close(ctx)
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
func (u *UnifiedStorageImpl) Close(ctx context.Context) error {
	var err error

	// 关闭缓存存储
	if u.CacheStorage != nil {
		if closeErr := u.CacheStorage.Close(ctx); closeErr != nil {
			err = closeErr
		}
	}

	// 关闭对象存储
	if u.ObjectStorage != nil {
		if closeErr := u.ObjectStorage.Close(ctx); closeErr != nil {
			err = closeErr
		}
	}

	// 关闭连接池管理器
	if u.poolManager != nil {
		if closeErr := u.poolManager.Close(ctx); closeErr != nil {
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

// getEnhancedMinIOPoolConfig 获取增强的MinIO池配置
func getEnhancedMinIOPoolConfig(cfg *config.Config) *pool.MinIOPoolConfig {
	// 检查是否有新的网络配置
	if cfg.Network.Pools.MinIO.MaxIdleConns > 0 {
		minioConfig := &cfg.Network.Pools.MinIO
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

	// 回退到旧配置，使用优化的默认值
	return &pool.MinIOPoolConfig{
		Endpoint:              cfg.MinIO.Endpoint,
		AccessKeyID:           cfg.MinIO.AccessKeyID,
		SecretAccessKey:       cfg.MinIO.SecretAccessKey,
		UseSSL:                cfg.MinIO.UseSSL,
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

// getBackupMinIOPoolConfig 获取备份MinIO池配置
func getBackupMinIOPoolConfig(cfg *config.Config) *pool.MinIOPoolConfig {
	// 检查是否有新的网络配置
	if cfg.Network.Pools.BackupMinIO != nil && cfg.Network.Pools.BackupMinIO.MaxIdleConns > 0 {
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

	// 回退到旧配置，使用优化的默认值
	return &pool.MinIOPoolConfig{
		Endpoint:              cfg.Backup.MinIO.Endpoint,
		AccessKeyID:           cfg.Backup.MinIO.AccessKeyID,
		SecretAccessKey:       cfg.Backup.MinIO.SecretAccessKey,
		UseSSL:                cfg.Backup.MinIO.UseSSL,
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
	// 检查是否有新的网络配置
	if cfg.Network.Pools.HealthCheckInterval > 0 {
		return cfg.Network.Pools.HealthCheckInterval
	}

	// 返回默认值
	return 15 * time.Second // 优化的默认值，比原来的30s更频繁
}
