package monitoring

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
)

// HealthStatus 健康状态
type HealthStatus struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// ComponentHealth 组件健康状态
type ComponentHealth struct {
	Redis  *HealthStatus `json:"redis"`
	MinIO  *HealthStatus `json:"minio"`
	DB     *HealthStatus `json:"db"`
	System *HealthStatus `json:"system"`
}

// OverallHealth 整体健康状态
type OverallHealth struct {
	Status     string           `json:"status"`
	Timestamp  string           `json:"timestamp"`
	Components *ComponentHealth `json:"components"`
}

// HealthChecker 健康检查器
type HealthChecker struct {
	redisPool   *pool.RedisPool
	minioClient *minio.Client
	db          *sql.DB
	cfg         *config.Config
	startTime   time.Time
	mu          sync.RWMutex
}

// NewHealthChecker 创建新的健康检查器
func NewHealthChecker(redisPool *pool.RedisPool, minioClient *minio.Client, db *sql.DB, cfg *config.Config) *HealthChecker {
	return &HealthChecker{
		redisPool:   redisPool,
		minioClient: minioClient,
		db:          db,
		cfg:         cfg,
		startTime:   time.Now(),
	}
}

// CheckHealth 执行健康检查
func (hc *HealthChecker) CheckHealth(ctx context.Context) *OverallHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	now := time.Now().UTC().Format(time.RFC3339)

	// 并发检查各个组件
	redisHealth := hc.checkRedisHealth(ctx)
	minioHealth := hc.checkMinIOHealth(ctx)
	dbHealth := hc.checkDBHealth(ctx)
	systemHealth := hc.checkSystemHealth(ctx)

	// 确定整体状态
	overallStatus := "healthy"
	if redisHealth.Status == "unhealthy" || minioHealth.Status == "unhealthy" ||
		dbHealth.Status == "unhealthy" || systemHealth.Status == "unhealthy" {
		overallStatus = "unhealthy"
	}

	return &OverallHealth{
		Status:    overallStatus,
		Timestamp: now,
		Components: &ComponentHealth{
			Redis:  redisHealth,
			MinIO:  minioHealth,
			DB:     dbHealth,
			System: systemHealth,
		},
	}
}

// checkRedisHealth 检查Redis健康状态
func (hc *HealthChecker) checkRedisHealth(ctx context.Context) *HealthStatus {
	start := time.Now()
	defer func() {
		RecordHealthCheck("redis", true, time.Since(start))
	}()

	now := time.Now().UTC().Format(time.RFC3339)

	// 如果Redis连接池为空，认为Redis被禁用
	if hc.redisPool == nil {
		return &HealthStatus{
			Status:    "disabled",
			Timestamp: now,
			Message:   "Redis is disabled",
		}
	}

	// 获取Redis客户端并测试连接
	redisClient := hc.redisPool.GetClient()
	if redisClient == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   "Failed to get Redis client from pool",
		}
	}

	// 执行PING命令
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   fmt.Sprintf("Redis ping failed: %v", err),
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Timestamp: now,
		Message:   "Redis is healthy",
	}
}

// checkMinIOHealth 检查MinIO连接
func (hc *HealthChecker) checkMinIOHealth(ctx context.Context) *HealthStatus {
	start := time.Now()
	defer func() {
		RecordHealthCheck("minio", true, time.Since(start))
	}()

	now := time.Now().UTC().Format(time.RFC3339)

	// 检查MinIO连接
	if hc.minioClient == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   "MinIO client is not initialized",
		}
	}

	// 检查存储桶是否存在
	exists, err := hc.minioClient.BucketExists(ctx, hc.cfg.MinIO.Bucket)
	if err != nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   fmt.Sprintf("MinIO bucket check failed: %v", err),
		}
	}

	if !exists {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   fmt.Sprintf("MinIO bucket '%s' does not exist", hc.cfg.MinIO.Bucket),
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Timestamp: now,
		Message:   fmt.Sprintf("MinIO bucket '%s' is accessible", hc.cfg.MinIO.Bucket),
	}
}

// checkDBHealth 检查数据库连接
func (hc *HealthChecker) checkDBHealth(ctx context.Context) *HealthStatus {
	start := time.Now()
	defer func() {
		RecordHealthCheck("db", true, time.Since(start))
	}()

	now := time.Now().UTC().Format(time.RFC3339)

	if hc.db == nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   "Database connection is nil",
		}
	}

	// 检查数据库连接
	if err := hc.db.PingContext(ctx); err != nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   fmt.Sprintf("Database ping failed: %v", err),
		}
	}

	// 获取数据库统计信息
	stats := hc.db.Stats()

	return &HealthStatus{
		Status:    "healthy",
		Timestamp: now,
		Message: fmt.Sprintf("Database healthy (Open: %d, InUse: %d, Idle: %d)",
			stats.OpenConnections, stats.InUse, stats.Idle),
	}
}

// checkSystemHealth 检查系统资源
func (hc *HealthChecker) checkSystemHealth(ctx context.Context) *HealthStatus {
	start := time.Now()
	defer func() {
		RecordHealthCheck("system", true, time.Since(start))
	}()

	now := time.Now().UTC().Format(time.RFC3339)

	// 获取系统资源信息
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 检查内存使用率
	memUsageMB := float64(m.Alloc) / 1024 / 1024
	maxMemoryMB := float64(hc.cfg.System.MaxMemoryMB)

	if maxMemoryMB > 0 && memUsageMB > maxMemoryMB {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   fmt.Sprintf("Memory usage too high: %.2f MB (limit: %.2f MB)", memUsageMB, maxMemoryMB),
		}
	}

	// 检查Goroutine数量
	numGoroutines := runtime.NumGoroutine()
	maxGoroutines := hc.cfg.System.MaxGoroutines

	if maxGoroutines > 0 && numGoroutines > maxGoroutines {
		return &HealthStatus{
			Status:    "unhealthy",
			Timestamp: now,
			Message:   fmt.Sprintf("Too many goroutines: %d (limit: %d)", numGoroutines, maxGoroutines),
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Timestamp: now,
		Message:   fmt.Sprintf("System healthy (Memory: %.2f MB, Goroutines: %d)", memUsageMB, numGoroutines),
	}
}

// IsHealthy 检查整体是否健康
func (hc *HealthChecker) IsHealthy(ctx context.Context) bool {
	health := hc.CheckHealth(ctx)
	return health.Status == "healthy"
}

// GetReadiness 获取就绪状态
func (hc *HealthChecker) GetReadiness(ctx context.Context) bool {
	// 就绪状态要求所有关键组件都正常
	health := hc.CheckHealth(ctx)

	// 检查各个组件的健康状态
	if health.Components.Redis != nil && health.Components.Redis.Status == "unhealthy" {
		return false
	}
	if health.Components.MinIO != nil && health.Components.MinIO.Status == "unhealthy" {
		return false
	}
	if health.Components.DB != nil && health.Components.DB.Status == "unhealthy" {
		return false
	}
	if health.Components.System != nil && health.Components.System.Status == "unhealthy" {
		return false
	}

	return true
}

// GetLiveness 获取存活状态
func (hc *HealthChecker) GetLiveness(ctx context.Context) bool {
	// 存活状态只需要基本服务运行
	return true
}

// StartHealthCheck 启动定期健康检查
func (hc *HealthChecker) StartHealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Starting health check with interval: %v", interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Health check stopped")
			return
		case <-ticker.C:
			health := hc.CheckHealth(ctx)
			if health.Status == "unhealthy" {
				log.Printf("Health check failed: %s", health.Status)
				// 可以在这里添加告警逻辑
			}
		}
	}
}
