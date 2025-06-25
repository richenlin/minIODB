package pool

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisPoolConfig Redis连接池配置
type RedisPoolConfig struct {
	// 基础连接配置
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	
	// 连接池配置
	PoolSize        int           `yaml:"pool_size"`         // 最大连接数
	MinIdleConns    int           `yaml:"min_idle_conns"`    // 最小空闲连接数
	MaxConnAge      time.Duration `yaml:"max_conn_age"`      // 连接最大生命周期
	PoolTimeout     time.Duration `yaml:"pool_timeout"`      // 获取连接超时
	IdleTimeout     time.Duration `yaml:"idle_timeout"`      // 空闲连接超时
	IdleCheckFreq   time.Duration `yaml:"idle_check_freq"`   // 空闲连接检查频率
	
	// 网络超时配置
	DialTimeout  time.Duration `yaml:"dial_timeout"`  // 连接超时
	ReadTimeout  time.Duration `yaml:"read_timeout"`  // 读超时
	WriteTimeout time.Duration `yaml:"write_timeout"` // 写超时
	
	// 重试配置
	MaxRetries      int           `yaml:"max_retries"`       // 最大重试次数
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff"` // 最小重试间隔
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff"` // 最大重试间隔
}

// DefaultRedisPoolConfig 返回默认的Redis连接池配置
func DefaultRedisPoolConfig() *RedisPoolConfig {
	return &RedisPoolConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		
		// 连接池配置 - 基于CPU核心数优化
		PoolSize:        runtime.NumCPU() * 2, // CPU核心数的2倍
		MinIdleConns:    runtime.NumCPU(),     // CPU核心数
		MaxConnAge:      30 * time.Minute,     // 30分钟
		PoolTimeout:     4 * time.Second,      // 4秒
		IdleTimeout:     5 * time.Minute,      // 5分钟
		IdleCheckFreq:   1 * time.Minute,      // 1分钟
		
		// 网络超时配置
		DialTimeout:  5 * time.Second,  // 5秒
		ReadTimeout:  3 * time.Second,  // 3秒
		WriteTimeout: 3 * time.Second,  // 3秒
		
		// 重试配置
		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
	}
}

// RedisPool Redis连接池管理器
type RedisPool struct {
	client *redis.Client
	config *RedisPoolConfig
}

// NewRedisPool 创建新的Redis连接池
func NewRedisPool(config *RedisPoolConfig) (*RedisPool, error) {
	if config == nil {
		config = DefaultRedisPoolConfig()
	}
	
	// 创建Redis客户端选项
	options := &redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
		
		// 连接池配置
		PoolSize:      config.PoolSize,
		MinIdleConns:  config.MinIdleConns,
		MaxConnAge:    config.MaxConnAge,
		PoolTimeout:   config.PoolTimeout,
		IdleTimeout:   config.IdleTimeout,
		IdleCheckFreq: config.IdleCheckFreq,
		
		// 网络超时配置
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		
		// 重试配置
		MaxRetries:      config.MaxRetries,
		MinRetryBackoff: config.MinRetryBackoff,
		MaxRetryBackoff: config.MaxRetryBackoff,
	}
	
	client := redis.NewClient(options)
	
	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	pool := &RedisPool{
		client: client,
		config: config,
	}
	
	return pool, nil
}

// GetClient 获取Redis客户端
func (p *RedisPool) GetClient() *redis.Client {
	return p.client
}

// GetStats 获取连接池统计信息
func (p *RedisPool) GetStats() *redis.PoolStats {
	return p.client.PoolStats()
}

// HealthCheck 健康检查
func (p *RedisPool) HealthCheck(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}

// Close 关闭连接池
func (p *RedisPool) Close() error {
	return p.client.Close()
}

// GetConfig 获取连接池配置
func (p *RedisPool) GetConfig() *RedisPoolConfig {
	return p.config
}

// UpdatePoolSize 动态更新连接池大小
func (p *RedisPool) UpdatePoolSize(newSize int) {
	// 注意：go-redis不支持动态调整连接池大小
	// 这里只是更新配置，实际需要重新创建客户端
	p.config.PoolSize = newSize
}

// GetConnectionInfo 获取连接信息
func (p *RedisPool) GetConnectionInfo() map[string]interface{} {
	stats := p.GetStats()
	return map[string]interface{}{
		"total_conns":   stats.TotalConns,
		"idle_conns":    stats.IdleConns,
		"stale_conns":   stats.StaleConns,
		"hits":          stats.Hits,
		"misses":        stats.Misses,
		"timeouts":      stats.Timeouts,
		"pool_size":     p.config.PoolSize,
		"min_idle":      p.config.MinIdleConns,
		"max_conn_age":  p.config.MaxConnAge.String(),
		"idle_timeout":  p.config.IdleTimeout.String(),
	}
} 