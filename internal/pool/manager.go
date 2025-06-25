package pool

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// PoolManager 统一连接池管理器
type PoolManager struct {
	redisPool  *RedisPool
	minioPool  *MinIOPool
	backupPool *MinIOPool // 备份MinIO连接池
	mutex      sync.RWMutex
	
	// 监控相关
	healthCheckInterval time.Duration
	stopHealthCheck     chan struct{}
	healthCheckRunning  bool
}

// PoolManagerConfig 连接池管理器配置
type PoolManagerConfig struct {
	Redis               *RedisPoolConfig  `yaml:"redis"`
	MinIO               *MinIOPoolConfig  `yaml:"minio"`
	BackupMinIO         *MinIOPoolConfig  `yaml:"backup_minio,omitempty"`
	HealthCheckInterval time.Duration     `yaml:"health_check_interval"`
}

// DefaultPoolManagerConfig 返回默认的连接池管理器配置
func DefaultPoolManagerConfig() *PoolManagerConfig {
	return &PoolManagerConfig{
		Redis:               DefaultRedisPoolConfig(),
		MinIO:               DefaultMinIOPoolConfig(),
		BackupMinIO:         nil, // 可选
		HealthCheckInterval: 30 * time.Second,
	}
}

// NewPoolManager 创建新的连接池管理器
func NewPoolManager(config *PoolManagerConfig) (*PoolManager, error) {
	if config == nil {
		config = DefaultPoolManagerConfig()
	}
	
	// 创建Redis连接池
	redisPool, err := NewRedisPool(config.Redis)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis pool: %w", err)
	}
	
	// 创建MinIO连接池
	minioPool, err := NewMinIOPool(config.MinIO)
	if err != nil {
		redisPool.Close()
		return nil, fmt.Errorf("failed to create MinIO pool: %w", err)
	}
	
	// 创建备份MinIO连接池（如果配置了）
	var backupPool *MinIOPool
	if config.BackupMinIO != nil {
		backupPool, err = NewMinIOPool(config.BackupMinIO)
		if err != nil {
			log.Printf("WARN: failed to create backup MinIO pool: %v", err)
			// 备份连接池失败不影响主要功能
		}
	}
	
	manager := &PoolManager{
		redisPool:           redisPool,
		minioPool:           minioPool,
		backupPool:          backupPool,
		healthCheckInterval: config.HealthCheckInterval,
		stopHealthCheck:     make(chan struct{}),
	}
	
	// 启动健康检查
	manager.startHealthCheck()
	
	log.Println("Connection pool manager initialized successfully")
	return manager, nil
}

// GetRedisPool 获取Redis连接池
func (pm *PoolManager) GetRedisPool() *RedisPool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return pm.redisPool
}

// GetMinIOPool 获取MinIO连接池
func (pm *PoolManager) GetMinIOPool() *MinIOPool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return pm.minioPool
}

// GetBackupMinIOPool 获取备份MinIO连接池
func (pm *PoolManager) GetBackupMinIOPool() *MinIOPool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return pm.backupPool
}

// startHealthCheck 启动健康检查
func (pm *PoolManager) startHealthCheck() {
	if pm.healthCheckRunning {
		return
	}
	
	pm.healthCheckRunning = true
	go pm.healthCheckLoop()
}

// healthCheckLoop 健康检查循环
func (pm *PoolManager) healthCheckLoop() {
	ticker := time.NewTicker(pm.healthCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			pm.performHealthCheck()
		case <-pm.stopHealthCheck:
			log.Println("Health check stopped")
			return
		}
	}
}

// performHealthCheck 执行健康检查
func (pm *PoolManager) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// 检查Redis连接池
	if err := pm.redisPool.HealthCheck(ctx); err != nil {
		log.Printf("WARN: Redis health check failed: %v", err)
	}
	
	// 检查MinIO连接池
	if err := pm.minioPool.HealthCheck(ctx); err != nil {
		log.Printf("WARN: MinIO health check failed: %v", err)
	}
	
	// 检查备份MinIO连接池
	if pm.backupPool != nil {
		if err := pm.backupPool.HealthCheck(ctx); err != nil {
			log.Printf("WARN: Backup MinIO health check failed: %v", err)
		}
	}
}

// GetOverallStats 获取所有连接池的统计信息
func (pm *PoolManager) GetOverallStats() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	stats := make(map[string]interface{})
	
	// Redis统计信息
	if pm.redisPool != nil {
		stats["redis"] = pm.redisPool.GetConnectionInfo()
	}
	
	// MinIO统计信息
	if pm.minioPool != nil {
		stats["minio"] = pm.minioPool.GetConnectionInfo()
	}
	
	// 备份MinIO统计信息
	if pm.backupPool != nil {
		stats["backup_minio"] = pm.backupPool.GetConnectionInfo()
	}
	
	// 总体信息
	stats["health_check_interval"] = pm.healthCheckInterval.String()
	stats["health_check_running"] = pm.healthCheckRunning
	stats["timestamp"] = time.Now().Unix()
	
	return stats
}

// UpdateRedisPool 更新Redis连接池
func (pm *PoolManager) UpdateRedisPool(config *RedisPoolConfig) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	// 创建新的Redis连接池
	newPool, err := NewRedisPool(config)
	if err != nil {
		return fmt.Errorf("failed to create new Redis pool: %w", err)
	}
	
	// 关闭旧连接池
	if pm.redisPool != nil {
		pm.redisPool.Close()
	}
	
	pm.redisPool = newPool
	log.Println("Redis connection pool updated successfully")
	return nil
}

// UpdateMinIOPool 更新MinIO连接池
func (pm *PoolManager) UpdateMinIOPool(config *MinIOPoolConfig) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	// 创建新的MinIO连接池
	newPool, err := NewMinIOPool(config)
	if err != nil {
		return fmt.Errorf("failed to create new MinIO pool: %w", err)
	}
	
	// 关闭旧连接池
	if pm.minioPool != nil {
		pm.minioPool.Close()
	}
	
	pm.minioPool = newPool
	log.Println("MinIO connection pool updated successfully")
	return nil
}

// UpdateBackupMinIOPool 更新备份MinIO连接池
func (pm *PoolManager) UpdateBackupMinIOPool(config *MinIOPoolConfig) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	if config == nil {
		// 禁用备份连接池
		if pm.backupPool != nil {
			pm.backupPool.Close()
			pm.backupPool = nil
		}
		log.Println("Backup MinIO connection pool disabled")
		return nil
	}
	
	// 创建新的备份MinIO连接池
	newPool, err := NewMinIOPool(config)
	if err != nil {
		return fmt.Errorf("failed to create new backup MinIO pool: %w", err)
	}
	
	// 关闭旧连接池
	if pm.backupPool != nil {
		pm.backupPool.Close()
	}
	
	pm.backupPool = newPool
	log.Println("Backup MinIO connection pool updated successfully")
	return nil
}

// Close 关闭所有连接池
func (pm *PoolManager) Close() error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	// 停止健康检查
	if pm.healthCheckRunning {
		close(pm.stopHealthCheck)
		pm.healthCheckRunning = false
	}
	
	var errors []string
	
	// 关闭Redis连接池
	if pm.redisPool != nil {
		if err := pm.redisPool.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("Redis pool close error: %v", err))
		}
	}
	
	// 关闭MinIO连接池
	if pm.minioPool != nil {
		pm.minioPool.Close()
	}
	
	// 关闭备份MinIO连接池
	if pm.backupPool != nil {
		pm.backupPool.Close()
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("connection pool close errors: %v", errors)
	}
	
	log.Println("All connection pools closed successfully")
	return nil
}

// IsHealthy 检查所有连接池是否健康
func (pm *PoolManager) IsHealthy(ctx context.Context) bool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	// 检查Redis
	if pm.redisPool != nil {
		if err := pm.redisPool.HealthCheck(ctx); err != nil {
			return false
		}
	}
	
	// 检查MinIO
	if pm.minioPool != nil {
		if err := pm.minioPool.HealthCheck(ctx); err != nil {
			return false
		}
	}
	
	// 备份MinIO不影响整体健康状态
	return true
}

// GetDetailedHealthStatus 获取详细的健康状态
func (pm *PoolManager) GetDetailedHealthStatus(ctx context.Context) map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	status := make(map[string]interface{})
	
	// Redis健康状态
	if pm.redisPool != nil {
		err := pm.redisPool.HealthCheck(ctx)
		status["redis"] = map[string]interface{}{
			"healthy": err == nil,
			"error":   err,
		}
	} else {
		status["redis"] = map[string]interface{}{
			"healthy": false,
			"error":   "Redis pool not initialized",
		}
	}
	
	// MinIO健康状态
	if pm.minioPool != nil {
		err := pm.minioPool.HealthCheck(ctx)
		status["minio"] = map[string]interface{}{
			"healthy": err == nil,
			"error":   err,
		}
	} else {
		status["minio"] = map[string]interface{}{
			"healthy": false,
			"error":   "MinIO pool not initialized",
		}
	}
	
	// 备份MinIO健康状态
	if pm.backupPool != nil {
		err := pm.backupPool.HealthCheck(ctx)
		status["backup_minio"] = map[string]interface{}{
			"healthy": err == nil,
			"error":   err,
		}
	} else {
		status["backup_minio"] = map[string]interface{}{
			"healthy": true, // 备份是可选的
			"error":   "Backup MinIO pool not configured",
		}
	}
	
	// 整体状态
	overallHealthy := true
	if redisStatus, ok := status["redis"].(map[string]interface{}); ok {
		if !redisStatus["healthy"].(bool) {
			overallHealthy = false
		}
	}
	if minioStatus, ok := status["minio"].(map[string]interface{}); ok {
		if !minioStatus["healthy"].(bool) {
			overallHealthy = false
		}
	}
	
	status["overall"] = map[string]interface{}{
		"healthy":   overallHealthy,
		"timestamp": time.Now().Unix(),
	}
	
	return status
} 