package pool

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// RedisMode Redis运行模式
type RedisMode string

const (
	RedisModeStandalone RedisMode = "standalone" // 单机模式
	RedisModeSentinel   RedisMode = "sentinel"   // 哨兵模式
	RedisModeCluster    RedisMode = "cluster"    // 集群模式
)

// String 返回RedisMode的字符串表示
func (m RedisMode) String() string {
	return string(m)
}

// RedisPoolConfig Redis连接池配置
type RedisPoolConfig struct {
	// 基础配置
	Mode     RedisMode `yaml:"mode"`     // Redis模式: standalone, sentinel, cluster
	Addr     string    `yaml:"addr"`     // 单机模式地址
	Password string    `yaml:"password"` // 密码
	DB       int       `yaml:"db"`       // 数据库编号（集群模式不支持）

	// 哨兵模式配置
	MasterName       string   `yaml:"master_name"`       // 主节点名称
	SentinelAddrs    []string `yaml:"sentinel_addrs"`    // 哨兵地址列表
	SentinelPassword string   `yaml:"sentinel_password"` // 哨兵密码

	// 集群模式配置
	ClusterAddrs []string `yaml:"cluster_addrs"` // 集群地址列表

	// 连接池配置
	PoolSize      int           `yaml:"pool_size"`       // 连接池大小
	MinIdleConns  int           `yaml:"min_idle_conns"`  // 最小空闲连接数
	MaxConnAge    time.Duration `yaml:"max_conn_age"`    // 连接最大生存时间
	PoolTimeout   time.Duration `yaml:"pool_timeout"`    // 获取连接超时
	IdleTimeout   time.Duration `yaml:"idle_timeout"`    // 空闲连接超时
	IdleCheckFreq time.Duration `yaml:"idle_check_freq"` // 空闲连接检查频率

	// 网络配置
	DialTimeout  time.Duration `yaml:"dial_timeout"`  // 连接超时
	ReadTimeout  time.Duration `yaml:"read_timeout"`  // 读取超时
	WriteTimeout time.Duration `yaml:"write_timeout"` // 写入超时

	// 重试配置
	MaxRetries      int           `yaml:"max_retries"`       // 最大重试次数
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff"` // 最小重试间隔
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff"` // 最大重试间隔

	// 集群特定配置
	MaxRedirects   int  `yaml:"max_redirects"`    // 最大重定向次数
	ReadOnly       bool `yaml:"read_only"`        // 只读模式
	RouteByLatency bool `yaml:"route_by_latency"` // 按延迟路由
	RouteRandomly  bool `yaml:"route_randomly"`   // 随机路由
}

// DefaultRedisPoolConfig 返回默认Redis连接池配置
func DefaultRedisPoolConfig() *RedisPoolConfig {
	cpuCount := runtime.NumCPU()
	return &RedisPoolConfig{
		Mode:            RedisModeStandalone,
		Addr:            "localhost:6379",
		Password:        "",
		DB:              0,
		PoolSize:        cpuCount * 10,
		MinIdleConns:    cpuCount,
		MaxConnAge:      30 * time.Minute,
		PoolTimeout:     4 * time.Second,
		IdleTimeout:     5 * time.Minute,
		IdleCheckFreq:   time.Minute,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		MaxRedirects:    8,
		ReadOnly:        false,
		RouteByLatency:  false,
		RouteRandomly:   true,
	}
}

// RedisPool Redis连接池
type RedisPool struct {
	config *RedisPoolConfig
	client redis.Cmdable // 统一接口，支持单机、哨兵、集群
	logger *zap.Logger

	// 具体客户端实例
	standaloneClient *redis.Client
	sentinelClient   *redis.Client
	clusterClient    *redis.ClusterClient

	mutex sync.RWMutex
	stats *RedisPoolStats

	// 动态调整相关
	dynamicConfig *DynamicPoolConfig
	adjustTicker  *time.Ticker
	adjustStopCh  chan struct{}
	currentSize   int
}

// RedisPoolStats Redis连接池统计信息
type RedisPoolStats struct {
	Mode            string            `json:"mode"`
	TotalConns      uint32            `json:"total_conns"`
	IdleConns       uint32            `json:"idle_conns"`
	StaleConns      uint32            `json:"stale_conns"`
	Hits            uint64            `json:"hits"`
	Misses          uint64            `json:"misses"`
	Timeouts        uint64            `json:"timeouts"`
	TotalRequests   uint64            `json:"total_requests"`
	SuccessRequests uint64            `json:"success_requests"`
	FailedRequests  uint64            `json:"failed_requests"`
	AvgResponseTime time.Duration     `json:"avg_response_time"`
	LastHealthCheck time.Time         `json:"last_health_check"`
	HealthStatus    string            `json:"health_status"`
	ClusterNodes    map[string]string `json:"cluster_nodes,omitempty"` // 集群节点状态
}

// DynamicPoolConfig 动态连接池配置
type DynamicPoolConfig struct {
	MinConnections     int           `yaml:"min_connections"`      // 最小连接数
	MaxConnections     int           `yaml:"max_connections"`      // 最大连接数
	ScaleUpThreshold   float64       `yaml:"scale_up_threshold"`   // 扩容阈值（使用率 > 80%）
	ScaleDownThreshold float64       `yaml:"scale_down_threshold"` // 缩容阈值（使用率 < 20%）
	CheckInterval      time.Duration `yaml:"check_interval"`       // 检查间隔
	ScaleStep          int           `yaml:"scale_step"`           // 每次扩缩容步长
}

// DefaultDynamicPoolConfig 返回默认动态连接池配置
func DefaultDynamicPoolConfig() *DynamicPoolConfig {
	cpuCount := runtime.NumCPU()
	return &DynamicPoolConfig{
		MinConnections:     cpuCount,
		MaxConnections:     cpuCount * 20,
		ScaleUpThreshold:   0.8, // 80%
		ScaleDownThreshold: 0.2, // 20%
		CheckInterval:      30 * time.Second,
		ScaleStep:          cpuCount,
	}
}

// NewRedisPool 创建新的Redis连接池
func NewRedisPool(config *RedisPoolConfig, logger *zap.Logger) (*RedisPool, error) {
	if config == nil {
		config = DefaultRedisPoolConfig()
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	pool := &RedisPool{
		config:        config,
		logger:        logger,
		stats:         &RedisPoolStats{Mode: string(config.Mode)},
		dynamicConfig: DefaultDynamicPoolConfig(),
		adjustStopCh:  make(chan struct{}),
		currentSize:   config.PoolSize,
	}

	// 根据模式创建相应的客户端
	switch config.Mode {
	case RedisModeStandalone:
		if err := pool.createStandaloneClient(); err != nil {
			return nil, fmt.Errorf("failed to create standalone client: %w", err)
		}
	case RedisModeSentinel:
		if err := pool.createSentinelClient(); err != nil {
			return nil, fmt.Errorf("failed to create sentinel client: %w", err)
		}
	case RedisModeCluster:
		if err := pool.createClusterClient(); err != nil {
			return nil, fmt.Errorf("failed to create cluster client: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported Redis mode: %s", config.Mode)
	}

	// 执行健康检查
	initTimeout := config.DialTimeout
	if initTimeout <= 0 {
		initTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	if err := pool.HealthCheck(ctx); err != nil {
		logger.Warn("Redis initial health check failed, will retry in background",
			zap.Error(err),
			zap.String("addr", config.Addr),
		)
	}

	logger.Sugar().Infof("Redis pool initialized in %s mode", config.Mode)
	return pool, nil
}

// createStandaloneClient 创建单机模式客户端
func (p *RedisPool) createStandaloneClient() error {
	if p.config.Addr == "" {
		return fmt.Errorf("address is required for standalone mode")
	}

	options := &redis.Options{
		Addr:               p.config.Addr,
		Password:           p.config.Password,
		DB:                 p.config.DB,
		PoolSize:           p.config.PoolSize,
		MinIdleConns:       p.config.MinIdleConns,
		MaxConnAge:         p.config.MaxConnAge,
		PoolTimeout:        p.config.PoolTimeout,
		IdleTimeout:        p.config.IdleTimeout,
		IdleCheckFrequency: p.config.IdleCheckFreq,
		DialTimeout:        p.config.DialTimeout,
		ReadTimeout:        p.config.ReadTimeout,
		WriteTimeout:       p.config.WriteTimeout,
		MaxRetries:         p.config.MaxRetries,
		MinRetryBackoff:    p.config.MinRetryBackoff,
		MaxRetryBackoff:    p.config.MaxRetryBackoff,
	}

	p.standaloneClient = redis.NewClient(options)
	p.client = p.standaloneClient
	return nil
}

// createSentinelClient 创建哨兵模式客户端
func (p *RedisPool) createSentinelClient() error {
	if p.config.MasterName == "" {
		return fmt.Errorf("master name is required for sentinel mode")
	}
	if len(p.config.SentinelAddrs) == 0 {
		return fmt.Errorf("sentinel addresses are required for sentinel mode")
	}

	options := &redis.FailoverOptions{
		MasterName:         p.config.MasterName,
		SentinelAddrs:      p.config.SentinelAddrs,
		SentinelPassword:   p.config.SentinelPassword,
		Password:           p.config.Password,
		DB:                 p.config.DB,
		PoolSize:           p.config.PoolSize,
		MinIdleConns:       p.config.MinIdleConns,
		MaxConnAge:         p.config.MaxConnAge,
		PoolTimeout:        p.config.PoolTimeout,
		IdleTimeout:        p.config.IdleTimeout,
		IdleCheckFrequency: p.config.IdleCheckFreq,
		DialTimeout:        p.config.DialTimeout,
		ReadTimeout:        p.config.ReadTimeout,
		WriteTimeout:       p.config.WriteTimeout,
		MaxRetries:         p.config.MaxRetries,
		MinRetryBackoff:    p.config.MinRetryBackoff,
		MaxRetryBackoff:    p.config.MaxRetryBackoff,
	}

	p.sentinelClient = redis.NewFailoverClient(options)
	p.client = p.sentinelClient
	return nil
}

// createClusterClient 创建集群模式客户端
func (p *RedisPool) createClusterClient() error {
	if len(p.config.ClusterAddrs) == 0 {
		return fmt.Errorf("cluster addresses are required for cluster mode")
	}

	options := &redis.ClusterOptions{
		Addrs:              p.config.ClusterAddrs,
		Password:           p.config.Password,
		PoolSize:           p.config.PoolSize,
		MinIdleConns:       p.config.MinIdleConns,
		MaxConnAge:         p.config.MaxConnAge,
		PoolTimeout:        p.config.PoolTimeout,
		IdleTimeout:        p.config.IdleTimeout,
		IdleCheckFrequency: p.config.IdleCheckFreq,
		DialTimeout:        p.config.DialTimeout,
		ReadTimeout:        p.config.ReadTimeout,
		WriteTimeout:       p.config.WriteTimeout,
		MaxRetries:         p.config.MaxRetries,
		MinRetryBackoff:    p.config.MinRetryBackoff,
		MaxRetryBackoff:    p.config.MaxRetryBackoff,
		MaxRedirects:       p.config.MaxRedirects,
		ReadOnly:           p.config.ReadOnly,
		RouteByLatency:     p.config.RouteByLatency,
		RouteRandomly:      p.config.RouteRandomly,
	}

	p.clusterClient = redis.NewClusterClient(options)
	p.client = p.clusterClient
	return nil
}

// GetClient 获取Redis客户端（统一接口）
func (p *RedisPool) GetClient() redis.Cmdable {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.client
}

// GetUniversalClient 获取UniversalClient接口（用于故障切换管理器）
func (p *RedisPool) GetUniversalClient() redis.UniversalClient {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// 根据模式返回相应的客户端
	switch p.config.Mode {
	case RedisModeCluster:
		return p.clusterClient
	default:
		// 单机和哨兵模式都返回*redis.Client
		if p.standaloneClient != nil {
			return p.standaloneClient
		}
		return p.sentinelClient
	}
}

// GetMode 获取Redis模式
func (p *RedisPool) GetMode() RedisMode {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.config.Mode
}

// GetStandaloneClient 获取单机客户端
func (p *RedisPool) GetStandaloneClient() *redis.Client {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.standaloneClient
}

// GetSentinelClient 获取哨兵客户端
func (p *RedisPool) GetSentinelClient() *redis.Client {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.sentinelClient
}

// GetClusterClient 获取集群客户端
func (p *RedisPool) GetClusterClient() *redis.ClusterClient {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.clusterClient
}

// HealthCheck 健康检查
func (p *RedisPool) HealthCheck(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	startTime := time.Now()
	defer func() {
		p.stats.LastHealthCheck = time.Now()
		p.stats.AvgResponseTime = time.Since(startTime)
	}()

	// 基础连通性检查
	if err := p.client.Ping(ctx).Err(); err != nil {
		p.stats.HealthStatus = "unhealthy"
		return fmt.Errorf("ping failed: %w", err)
	}

	// 根据模式进行特定检查
	switch p.config.Mode {
	case RedisModeStandalone:
		if err := p.checkStandaloneHealth(ctx); err != nil {
			p.stats.HealthStatus = "unhealthy"
			return err
		}
	case RedisModeSentinel:
		if err := p.checkSentinelHealth(ctx); err != nil {
			p.stats.HealthStatus = "unhealthy"
			return err
		}
	case RedisModeCluster:
		if err := p.checkClusterHealth(ctx); err != nil {
			p.stats.HealthStatus = "unhealthy"
			return err
		}
	}

	p.stats.HealthStatus = "healthy"
	return nil
}

// checkStandaloneHealth 检查单机模式健康状态
func (p *RedisPool) checkStandaloneHealth(ctx context.Context) error {
	// 使用PING命令测试连接
	result, err := p.standaloneClient.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to ping server: %w", err)
	}

	// 验证PING响应
	if result != "PONG" {
		return fmt.Errorf("unexpected ping response: %s", result)
	}

	return nil
}

// checkSentinelHealth 检查哨兵模式健康状态
func (p *RedisPool) checkSentinelHealth(ctx context.Context) error {
	// 使用PING命令测试连接
	result, err := p.sentinelClient.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to ping server: %w", err)
	}

	// 验证PING响应
	if result != "PONG" {
		return fmt.Errorf("unexpected ping response: %s", result)
	}

	return nil
}

// checkClusterHealth 检查集群模式健康状态
func (p *RedisPool) checkClusterHealth(ctx context.Context) error {
	// 获取集群节点信息
	nodes, err := p.clusterClient.ClusterNodes(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	// 解析节点状态
	p.stats.ClusterNodes = make(map[string]string)
	lines := strings.Split(nodes, "\n")
	healthyNodes := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 8 {
			nodeId := parts[0]
			flags := parts[2]

			p.stats.ClusterNodes[nodeId] = flags

			// 检查节点是否健康 (master或slave且连接正常)
			if (strings.Contains(flags, "master") || strings.Contains(flags, "slave")) &&
				!strings.Contains(flags, "fail") && !strings.Contains(flags, "fail?") {
				healthyNodes++
			}
		}
	}

	if healthyNodes == 0 {
		return fmt.Errorf("no healthy nodes found in cluster")
	}

	return nil
}

// GetStats 获取连接池统计信息
func (p *RedisPool) GetStats() *RedisPoolStats {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stats := *p.stats

	// 获取连接池统计信息
	switch p.config.Mode {
	case RedisModeStandalone:
		if p.standaloneClient != nil {
			poolStats := p.standaloneClient.PoolStats()
			stats.TotalConns = poolStats.TotalConns
			stats.IdleConns = poolStats.IdleConns
			stats.StaleConns = poolStats.StaleConns
			stats.Hits = uint64(poolStats.Hits)
			stats.Misses = uint64(poolStats.Misses)
			stats.Timeouts = uint64(poolStats.Timeouts)
		}
	case RedisModeSentinel:
		if p.sentinelClient != nil {
			poolStats := p.sentinelClient.PoolStats()
			stats.TotalConns = poolStats.TotalConns
			stats.IdleConns = poolStats.IdleConns
			stats.StaleConns = poolStats.StaleConns
			stats.Hits = uint64(poolStats.Hits)
			stats.Misses = uint64(poolStats.Misses)
			stats.Timeouts = uint64(poolStats.Timeouts)
		}
	case RedisModeCluster:
		if p.clusterClient != nil {
			poolStats := p.clusterClient.PoolStats()
			stats.TotalConns = poolStats.TotalConns
			stats.IdleConns = poolStats.IdleConns
			stats.StaleConns = poolStats.StaleConns
			stats.Hits = uint64(poolStats.Hits)
			stats.Misses = uint64(poolStats.Misses)
			stats.Timeouts = uint64(poolStats.Timeouts)
		}
	}

	return &stats
}

// UpdatePoolSize 动态更新连接池大小
func (p *RedisPool) UpdatePoolSize(newSize int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if newSize <= 0 {
		return fmt.Errorf("pool size must be positive")
	}

	p.config.PoolSize = newSize

	// 注意: go-redis不支持动态调整连接池大小
	// 这里只更新配置，实际生效需要重新创建连接池
	p.logger.Sugar().Infof("Pool size updated to %d (requires restart to take effect)", newSize)
	return nil
}

// GetConfig 获取配置信息
func (p *RedisPool) GetConfig() *RedisPoolConfig {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// 创建配置副本
	configCopy := *p.config
	return &configCopy
}

// Close 关闭连接池
func (p *RedisPool) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var err error

	switch p.config.Mode {
	case RedisModeStandalone:
		if p.standaloneClient != nil {
			err = p.standaloneClient.Close()
			p.standaloneClient = nil
		}
	case RedisModeSentinel:
		if p.sentinelClient != nil {
			err = p.sentinelClient.Close()
			p.sentinelClient = nil
		}
	case RedisModeCluster:
		if p.clusterClient != nil {
			err = p.clusterClient.Close()
			p.clusterClient = nil
		}
	}

	p.client = nil

	if err != nil {
		return fmt.Errorf("failed to close Redis client: %w", err)
	}

	p.logger.Sugar().Infof("Redis pool (%s mode) closed successfully", p.config.Mode)
	return nil
}

// GetConnectionInfo 获取连接信息
func (p *RedisPool) GetConnectionInfo() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	info := map[string]interface{}{
		"mode":      p.config.Mode,
		"pool_size": p.config.PoolSize,
	}

	switch p.config.Mode {
	case RedisModeStandalone:
		info["addr"] = p.config.Addr
		info["db"] = p.config.DB
	case RedisModeSentinel:
		info["master_name"] = p.config.MasterName
		info["sentinel_addrs"] = p.config.SentinelAddrs
		info["db"] = p.config.DB
	case RedisModeCluster:
		info["cluster_addrs"] = p.config.ClusterAddrs
		info["read_only"] = p.config.ReadOnly
	}

	return info
}

// ExecuteWithRetry 执行带重试的Redis操作
func (p *RedisPool) ExecuteWithRetry(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		p.stats.TotalRequests++

		err := operation()
		if err == nil {
			p.stats.SuccessRequests++
			return nil
		}

		lastErr = err
		p.stats.FailedRequests++

		if attempt < p.config.MaxRetries {
			// 计算退避时间
			backoff := p.config.MinRetryBackoff * time.Duration(1<<uint(attempt))
			if backoff > p.config.MaxRetryBackoff {
				backoff = p.config.MaxRetryBackoff
			}

			p.logger.Sugar().Infof("Redis operation failed (attempt %d/%d), retrying in %v: %v",
				attempt+1, p.config.MaxRetries+1, backoff, err)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", p.config.MaxRetries+1, lastErr)
}

// GetConcreteClient 获取具体的Redis客户端类型
// 注意：此方法仅用于需要具体客户端类型的场景，一般情况下应使用GetClient()
func (p *RedisPool) GetConcreteClient() interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	switch p.config.Mode {
	case RedisModeStandalone:
		return p.standaloneClient
	case RedisModeSentinel:
		return p.sentinelClient
	case RedisModeCluster:
		return p.clusterClient
	default:
		return nil
	}
}

// GetRedisClient 获取Redis客户端（兼容性方法）
func (p *RedisPool) GetRedisClient() *redis.Client {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	switch p.config.Mode {
	case RedisModeStandalone:
		return p.standaloneClient
	case RedisModeSentinel:
		return p.sentinelClient
	default:
		return nil
	}
}

// PoolStatus 连接池状态
type PoolStatus struct {
	TotalConnections  int `json:"total_connections"`
	ActiveConnections int `json:"active_connections"`
	IdleConnections   int `json:"idle_connections"`
}

// GetPoolStatus 获取连接池状态
func (p *RedisPool) GetPoolStatus() *PoolStatus {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	status := &PoolStatus{}

	switch p.config.Mode {
	case RedisModeStandalone:
		if p.standaloneClient != nil {
			poolStats := p.standaloneClient.PoolStats()
			status.TotalConnections = int(poolStats.TotalConns)
			status.ActiveConnections = int(poolStats.TotalConns - poolStats.IdleConns)
			status.IdleConnections = int(poolStats.IdleConns)
		}
	case RedisModeSentinel:
		if p.sentinelClient != nil {
			poolStats := p.sentinelClient.PoolStats()
			status.TotalConnections = int(poolStats.TotalConns)
			status.ActiveConnections = int(poolStats.TotalConns - poolStats.IdleConns)
			status.IdleConnections = int(poolStats.IdleConns)
		}
	case RedisModeCluster:
		if p.clusterClient != nil {
			poolStats := p.clusterClient.PoolStats()
			status.TotalConnections = int(poolStats.TotalConns)
			status.ActiveConnections = int(poolStats.TotalConns - poolStats.IdleConns)
			status.IdleConnections = int(poolStats.IdleConns)
		}
	}

	return status
}

// SetDynamicConfig 设置动态调整配置
func (p *RedisPool) SetDynamicConfig(config *DynamicPoolConfig) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.dynamicConfig = config
}

// GetDynamicConfig 获取动态调整配置
func (p *RedisPool) GetDynamicConfig() *DynamicPoolConfig {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.dynamicConfig
}

// StartDynamicAdjustment 启动动态调整
func (p *RedisPool) StartDynamicAdjustment() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.adjustTicker != nil {
		return // 已经启动
	}

	p.adjustTicker = time.NewTicker(p.dynamicConfig.CheckInterval)
	go func() {
		for {
			select {
			case <-p.adjustTicker.C:
				p.adjustPoolSize()
			case <-p.adjustStopCh:
				return
			}
		}
	}()

	p.logger.Sugar().Infof("Redis pool dynamic adjustment started with interval %v", p.dynamicConfig.CheckInterval)
}

// StopDynamicAdjustment 停止动态调整
func (p *RedisPool) StopDynamicAdjustment() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.adjustTicker != nil {
		p.adjustTicker.Stop()
		p.adjustTicker = nil
	}
	close(p.adjustStopCh)
	p.adjustStopCh = make(chan struct{})

	p.logger.Sugar().Info("Redis pool dynamic adjustment stopped")
}

// getUsageRate 获取连接池使用率
func (p *RedisPool) getUsageRate() float64 {
	status := p.GetPoolStatus()
	if status.TotalConnections == 0 {
		return 0
	}
	return float64(status.ActiveConnections) / float64(status.TotalConnections)
}

// adjustPoolSize 调整连接池大小
func (p *RedisPool) adjustPoolSize() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	usage := p.calculateUsageRateLocked()
	config := p.dynamicConfig

	p.logger.Sugar().Debugf("Redis pool usage rate: %.2f%%, current size: %d", usage*100, p.currentSize)

	if usage > config.ScaleUpThreshold {
		p.scaleUpLocked()
	} else if usage < config.ScaleDownThreshold {
		p.scaleDownLocked()
	}
}

// calculateUsageRateLocked 计算使用率（调用时已持有锁）
func (p *RedisPool) calculateUsageRateLocked() float64 {
	var totalConns, idleConns uint32

	switch p.config.Mode {
	case RedisModeStandalone:
		if p.standaloneClient != nil {
			poolStats := p.standaloneClient.PoolStats()
			totalConns = poolStats.TotalConns
			idleConns = poolStats.IdleConns
		}
	case RedisModeSentinel:
		if p.sentinelClient != nil {
			poolStats := p.sentinelClient.PoolStats()
			totalConns = poolStats.TotalConns
			idleConns = poolStats.IdleConns
		}
	case RedisModeCluster:
		if p.clusterClient != nil {
			poolStats := p.clusterClient.PoolStats()
			totalConns = poolStats.TotalConns
			idleConns = poolStats.IdleConns
		}
	}

	if totalConns == 0 {
		return 0
	}
	return float64(totalConns-idleConns) / float64(totalConns)
}

// scaleUpLocked 扩容（调用时已持有锁）
func (p *RedisPool) scaleUpLocked() {
	config := p.dynamicConfig
	newSize := p.currentSize + config.ScaleStep

	if newSize > config.MaxConnections {
		newSize = config.MaxConnections
	}

	if newSize == p.currentSize {
		return // 已达到最大值
	}

	p.logger.Sugar().Infof("Scaling up Redis pool from %d to %d (usage > %.0f%%)",
		p.currentSize, newSize, config.ScaleUpThreshold*100)

	p.currentSize = newSize
	p.config.PoolSize = newSize

	// 重新创建客户端以应用新的连接池大小
	if err := p.recreateClientLocked(); err != nil {
		p.logger.Sugar().Errorf("Failed to scale up Redis pool: %v", err)
	}
}

// scaleDownLocked 缩容（调用时已持有锁）
func (p *RedisPool) scaleDownLocked() {
	config := p.dynamicConfig
	newSize := p.currentSize - config.ScaleStep

	if newSize < config.MinConnections {
		newSize = config.MinConnections
	}

	if newSize == p.currentSize {
		return // 已达到最小值
	}

	p.logger.Sugar().Infof("Scaling down Redis pool from %d to %d (usage < %.0f%%)",
		p.currentSize, newSize, config.ScaleDownThreshold*100)

	p.currentSize = newSize
	p.config.PoolSize = newSize

	// 重新创建客户端以应用新的连接池大小
	if err := p.recreateClientLocked(); err != nil {
		p.logger.Sugar().Errorf("Failed to scale down Redis pool: %v", err)
	}
}

// recreateClientLocked 重新创建客户端（调用时已持有锁）
func (p *RedisPool) recreateClientLocked() error {
	// 先关闭旧客户端
	switch p.config.Mode {
	case RedisModeStandalone:
		if p.standaloneClient != nil {
			_ = p.standaloneClient.Close()
		}
	case RedisModeSentinel:
		if p.sentinelClient != nil {
			_ = p.sentinelClient.Close()
		}
	case RedisModeCluster:
		if p.clusterClient != nil {
			_ = p.clusterClient.Close()
		}
	}

	// 重新创建客户端
	switch p.config.Mode {
	case RedisModeStandalone:
		if err := p.createStandaloneClient(); err != nil {
			return err
		}
	case RedisModeSentinel:
		if err := p.createSentinelClient(); err != nil {
			return err
		}
	case RedisModeCluster:
		if err := p.createClusterClient(); err != nil {
			return err
		}
	}

	return nil
}

// GetCurrentPoolSize 获取当前连接池大小
func (p *RedisPool) GetCurrentPoolSize() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.currentSize
}
