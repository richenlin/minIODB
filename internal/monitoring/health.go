package monitoring

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"minIODB/internal/storage"
	
	"github.com/go-redis/redis/v8"
)

// HealthStatus 健康状态
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
)

// ComponentHealth 组件健康状态
type ComponentHealth struct {
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
}

// OverallHealth 整体健康状态
type OverallHealth struct {
	Status     HealthStatus       `json:"status"`
	Components []ComponentHealth  `json:"components"`
	Timestamp  time.Time         `json:"timestamp"`
	Uptime     time.Duration     `json:"uptime"`
}

// HealthChecker 健康检查器
type HealthChecker struct {
	redisClient *redis.Client
	minioClient storage.Uploader
	db          *sql.DB
	startTime   time.Time
	mu          sync.RWMutex
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(redisClient *redis.Client, minioClient storage.Uploader, db *sql.DB) *HealthChecker {
	return &HealthChecker{
		redisClient: redisClient,
		minioClient: minioClient,
		db:          db,
		startTime:   time.Now(),
	}
}

// CheckHealth 执行健康检查
func (hc *HealthChecker) CheckHealth(ctx context.Context) *OverallHealth {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	var components []ComponentHealth
	overallStatus := HealthStatusHealthy

	// 检查Redis连接
	redisHealth := hc.checkRedis(ctx)
	components = append(components, redisHealth)
	if redisHealth.Status != HealthStatusHealthy {
		overallStatus = HealthStatusDegraded
	}

	// 检查MinIO连接
	minioHealth := hc.checkMinIO(ctx)
	components = append(components, minioHealth)
	if minioHealth.Status != HealthStatusHealthy {
		if overallStatus == HealthStatusDegraded {
			overallStatus = HealthStatusUnhealthy
		} else {
			overallStatus = HealthStatusDegraded
		}
	}

	// 检查数据库连接
	dbHealth := hc.checkDatabase(ctx)
	components = append(components, dbHealth)
	if dbHealth.Status != HealthStatusHealthy {
		if overallStatus == HealthStatusDegraded {
			overallStatus = HealthStatusUnhealthy
		} else {
			overallStatus = HealthStatusDegraded
		}
	}

	// 检查系统资源
	systemHealth := hc.checkSystemResources(ctx)
	components = append(components, systemHealth)
	if systemHealth.Status != HealthStatusHealthy {
		overallStatus = HealthStatusDegraded
	}

	return &OverallHealth{
		Status:     overallStatus,
		Components: components,
		Timestamp:  time.Now(),
		Uptime:     time.Since(hc.startTime),
	}
}

// checkRedis 检查Redis连接
func (hc *HealthChecker) checkRedis(ctx context.Context) ComponentHealth {
	start := time.Now()
	
	// 创建带超时的上下文
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 执行ping命令
	err := hc.redisClient.Ping(checkCtx).Err()
	duration := time.Since(start)

	if err != nil {
		return ComponentHealth{
			Name:      "redis",
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Redis ping failed: %v", err),
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	// 检查响应时间
	if duration > 1*time.Second {
		return ComponentHealth{
			Name:      "redis",
			Status:    HealthStatusDegraded,
			Message:   fmt.Sprintf("Redis response time is slow: %v", duration),
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	return ComponentHealth{
		Name:      "redis",
		Status:    HealthStatusHealthy,
		Message:   "Redis is healthy",
		Timestamp: time.Now(),
		Duration:  duration,
	}
}

// checkMinIO 检查MinIO连接
func (hc *HealthChecker) checkMinIO(ctx context.Context) ComponentHealth {
	start := time.Now()
	
	// 创建带超时的上下文
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 检查存储桶是否存在
	exists, err := hc.minioClient.BucketExists(checkCtx, "olap-data")
	duration := time.Since(start)

	if err != nil {
		return ComponentHealth{
			Name:      "minio",
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("MinIO connection failed: %v", err),
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	if !exists {
		return ComponentHealth{
			Name:      "minio",
			Status:    HealthStatusDegraded,
			Message:   "MinIO bucket 'olap-data' does not exist",
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	// 检查响应时间
	if duration > 2*time.Second {
		return ComponentHealth{
			Name:      "minio",
			Status:    HealthStatusDegraded,
			Message:   fmt.Sprintf("MinIO response time is slow: %v", duration),
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	return ComponentHealth{
		Name:      "minio",
		Status:    HealthStatusHealthy,
		Message:   "MinIO is healthy",
		Timestamp: time.Now(),
		Duration:  duration,
	}
}

// checkDatabase 检查数据库连接
func (hc *HealthChecker) checkDatabase(ctx context.Context) ComponentHealth {
	start := time.Now()
	
	if hc.db == nil {
		return ComponentHealth{
			Name:      "database",
			Status:    HealthStatusUnhealthy,
			Message:   "Database connection is nil",
			Timestamp: time.Now(),
			Duration:  0,
		}
	}

	// 创建带超时的上下文
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 执行简单查询
	err := hc.db.PingContext(checkCtx)
	duration := time.Since(start)

	if err != nil {
		return ComponentHealth{
			Name:      "database",
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Database ping failed: %v", err),
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	// 检查响应时间
	if duration > 1*time.Second {
		return ComponentHealth{
			Name:      "database",
			Status:    HealthStatusDegraded,
			Message:   fmt.Sprintf("Database response time is slow: %v", duration),
			Timestamp: time.Now(),
			Duration:  duration,
		}
	}

	return ComponentHealth{
		Name:      "database",
		Status:    HealthStatusHealthy,
		Message:   "Database is healthy",
		Timestamp: time.Now(),
		Duration:  duration,
	}
}

// checkSystemResources 检查系统资源
func (hc *HealthChecker) checkSystemResources(ctx context.Context) ComponentHealth {
	start := time.Now()
	
	// 这里可以添加系统资源检查，如内存、CPU等
	// 目前简化为基本检查
	
	return ComponentHealth{
		Name:      "system",
		Status:    HealthStatusHealthy,
		Message:   "System resources are healthy",
		Timestamp: time.Now(),
		Duration:  time.Since(start),
	}
}

// IsHealthy 检查整体是否健康
func (hc *HealthChecker) IsHealthy(ctx context.Context) bool {
	health := hc.CheckHealth(ctx)
	return health.Status == HealthStatusHealthy
}

// GetReadiness 获取就绪状态
func (hc *HealthChecker) GetReadiness(ctx context.Context) bool {
	// 就绪状态要求所有关键组件都正常
	health := hc.CheckHealth(ctx)
	
	for _, component := range health.Components {
		if component.Name == "redis" || component.Name == "minio" {
			if component.Status == HealthStatusUnhealthy {
				return false
			}
		}
	}
	
	return true
}

// GetLiveness 获取存活状态
func (hc *HealthChecker) GetLiveness(ctx context.Context) bool {
	// 存活状态只需要基本服务运行
	return true
} 