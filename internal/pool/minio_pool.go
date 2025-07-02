package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOPoolConfig MinIO连接池配置
type MinIOPoolConfig struct {
	// 基础连接配置
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	UseSSL          bool   `yaml:"use_ssl"`
	Region          string `yaml:"region"`
	
	// HTTP连接池配置
	MaxIdleConns        int           `yaml:"max_idle_conns"`         // 最大空闲连接数
	MaxIdleConnsPerHost int           `yaml:"max_idle_conns_per_host"` // 每个主机的最大空闲连接数
	MaxConnsPerHost     int           `yaml:"max_conns_per_host"`     // 每个主机的最大连接数
	IdleConnTimeout     time.Duration `yaml:"idle_conn_timeout"`      // 空闲连接超时
	
	// 超时配置
	DialTimeout           time.Duration `yaml:"dial_timeout"`            // 连接超时
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout"`   // TLS握手超时
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"` // 响应头超时
	ExpectContinueTimeout time.Duration `yaml:"expect_continue_timeout"` // Expect: 100-continue超时
	
	// 重试和背压配置
	MaxRetries    int           `yaml:"max_retries"`    // 最大重试次数
	RetryDelay    time.Duration `yaml:"retry_delay"`    // 重试延迟
	RequestTimeout time.Duration `yaml:"request_timeout"` // 请求超时
	
	// 连接保活配置
	KeepAlive         time.Duration `yaml:"keep_alive"`          // TCP保活间隔
	DisableKeepAlive  bool          `yaml:"disable_keep_alive"`  // 禁用保活
	DisableCompression bool         `yaml:"disable_compression"` // 禁用压缩
}

// DefaultMinIOPoolConfig 返回默认的MinIO连接池配置
func DefaultMinIOPoolConfig() *MinIOPoolConfig {
	return &MinIOPoolConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
		Region:          "us-east-1",
		
		// HTTP连接池配置 - 基于CPU核心数优化
		MaxIdleConns:        runtime.NumCPU() * 10, // CPU核心数的10倍
		MaxIdleConnsPerHost: runtime.NumCPU() * 2,  // CPU核心数的2倍
		MaxConnsPerHost:     runtime.NumCPU() * 4,  // CPU核心数的4倍
		IdleConnTimeout:     90 * time.Second,      // 90秒
		
		// 超时配置
		DialTimeout:           10 * time.Second, // 10秒
		TLSHandshakeTimeout:   10 * time.Second, // 10秒
		ResponseHeaderTimeout: 10 * time.Second, // 10秒
		ExpectContinueTimeout: 1 * time.Second,  // 1秒
		
		// 重试配置
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
		RequestTimeout: 30 * time.Second,
		
		// 连接保活配置
		KeepAlive:          30 * time.Second, // 30秒
		DisableKeepAlive:   false,
		DisableCompression: false,
	}
}

// MinIOPool MinIO连接池管理器
type MinIOPool struct {
	client    *minio.Client
	config    *MinIOPoolConfig
	transport *http.Transport
	mutex     sync.RWMutex
	stats     *MinIOPoolStats
}

// MinIOPoolStats MinIO连接池统计信息
type MinIOPoolStats struct {
	TotalRequests    int64 `json:"total_requests"`
	SuccessRequests  int64 `json:"success_requests"`
	FailedRequests   int64 `json:"failed_requests"`
	RetryRequests    int64 `json:"retry_requests"`
	ActiveConns      int64 `json:"active_conns"`
	IdleConns        int64 `json:"idle_conns"`
	TotalConnTime    int64 `json:"total_conn_time_ms"`
	AvgResponseTime  int64 `json:"avg_response_time_ms"`
	LastRequestTime  int64 `json:"last_request_time"`
	mutex            sync.RWMutex
}

// NewMinIOPool 创建新的MinIO连接池
func NewMinIOPool(config *MinIOPoolConfig) (*MinIOPool, error) {
	if config == nil {
		config = DefaultMinIOPoolConfig()
	}
	
	// 创建优化的HTTP传输层
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   config.DialTimeout,
			KeepAlive: config.KeepAlive,
		}).DialContext,
		
		// 连接池配置
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     config.MaxConnsPerHost,
		IdleConnTimeout:     config.IdleConnTimeout,
		
		// 超时配置
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,
		
		// 其他配置
		DisableKeepAlives: config.DisableKeepAlive,
		DisableCompression: config.DisableCompression,
		
		// TLS配置
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false, // 生产环境应该设置为false
		},
	}
	
	// 创建MinIO客户端选项
	options := &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
		Region: config.Region,
		Transport: transport,
	}
	
	log.Printf("Creating MinIO client for endpoint: %s", config.Endpoint)
	client, err := minio.New(config.Endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}
	
	pool := &MinIOPool{
		client:    client,
		config:    config,
		transport: transport,
		stats:     &MinIOPoolStats{},
	}
	
	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), config.RequestTimeout)
	defer cancel()
	
	log.Printf("Testing MinIO connection to %s with timeout %v", config.Endpoint, config.RequestTimeout)
	if err := pool.healthCheck(ctx); err != nil {
		return nil, fmt.Errorf("MinIO connection test failed: %w", err)
	}
	
	return pool, nil
}

// GetClient 获取MinIO客户端
func (p *MinIOPool) GetClient() *minio.Client {
	return p.client
}

// healthCheck 健康检查
func (p *MinIOPool) healthCheck(ctx context.Context) error {
	// 尝试列出存储桶来测试连接
	buckets, err := p.client.ListBuckets(ctx)
	if err != nil {
		log.Printf("MinIO health check failed: %v (endpoint: %s)", err, p.config.Endpoint)
		return err
	}
	log.Printf("MinIO health check successful, found %d buckets (endpoint: %s)", len(buckets), p.config.Endpoint)
	return nil
}

// HealthCheck 公开的健康检查方法
func (p *MinIOPool) HealthCheck(ctx context.Context) error {
	return p.healthCheck(ctx)
}

// GetStats 获取连接池统计信息
func (p *MinIOPool) GetStats() *MinIOPoolStats {
	p.stats.mutex.RLock()
	defer p.stats.mutex.RUnlock()
	
	// 返回统计信息的副本
	return &MinIOPoolStats{
		TotalRequests:   p.stats.TotalRequests,
		SuccessRequests: p.stats.SuccessRequests,
		FailedRequests:  p.stats.FailedRequests,
		RetryRequests:   p.stats.RetryRequests,
		ActiveConns:     p.stats.ActiveConns,
		IdleConns:       p.stats.IdleConns,
		TotalConnTime:   p.stats.TotalConnTime,
		AvgResponseTime: p.stats.AvgResponseTime,
		LastRequestTime: p.stats.LastRequestTime,
	}
}

// updateStats 更新统计信息
func (p *MinIOPool) updateStats(success bool, responseTime time.Duration, retried bool) {
	p.stats.mutex.Lock()
	defer p.stats.mutex.Unlock()
	
	p.stats.TotalRequests++
	p.stats.LastRequestTime = time.Now().Unix()
	p.stats.TotalConnTime += responseTime.Milliseconds()
	
	if p.stats.TotalRequests > 0 {
		p.stats.AvgResponseTime = p.stats.TotalConnTime / p.stats.TotalRequests
	}
	
	if success {
		p.stats.SuccessRequests++
	} else {
		p.stats.FailedRequests++
	}
	
	if retried {
		p.stats.RetryRequests++
	}
}

// ExecuteWithRetry 带重试机制的操作执行
func (p *MinIOPool) ExecuteWithRetry(ctx context.Context, operation func() error) error {
	var lastErr error
	startTime := time.Now()
	
	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		err := operation()
		
		if err == nil {
			// 操作成功
			responseTime := time.Since(startTime)
			p.updateStats(true, responseTime, attempt > 0)
			return nil
		}
		
		lastErr = err
		
		// 如果不是最后一次尝试，等待重试延迟
		if attempt < p.config.MaxRetries {
			select {
			case <-ctx.Done():
				responseTime := time.Since(startTime)
				p.updateStats(false, responseTime, attempt > 0)
				return ctx.Err()
			case <-time.After(p.config.RetryDelay):
				// 继续重试
			}
		}
	}
	
	// 所有重试都失败了
	responseTime := time.Since(startTime)
	p.updateStats(false, responseTime, true)
	return fmt.Errorf("operation failed after %d retries: %w", p.config.MaxRetries, lastErr)
}

// GetConfig 获取连接池配置
func (p *MinIOPool) GetConfig() *MinIOPoolConfig {
	return p.config
}

// GetConnectionInfo 获取连接信息
func (p *MinIOPool) GetConnectionInfo() map[string]interface{} {
	stats := p.GetStats()
	
	return map[string]interface{}{
		"endpoint":                p.config.Endpoint,
		"use_ssl":                 p.config.UseSSL,
		"region":                  p.config.Region,
		"max_idle_conns":          p.config.MaxIdleConns,
		"max_idle_conns_per_host": p.config.MaxIdleConnsPerHost,
		"max_conns_per_host":      p.config.MaxConnsPerHost,
		"idle_conn_timeout":       p.config.IdleConnTimeout.String(),
		"dial_timeout":            p.config.DialTimeout.String(),
		"request_timeout":         p.config.RequestTimeout.String(),
		"total_requests":          stats.TotalRequests,
		"success_requests":        stats.SuccessRequests,
		"failed_requests":         stats.FailedRequests,
		"retry_requests":          stats.RetryRequests,
		"avg_response_time_ms":    stats.AvgResponseTime,
		"last_request_time":       stats.LastRequestTime,
	}
}

// Close 关闭连接池
func (p *MinIOPool) Close() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}

// UpdateConfig 更新配置（需要重新创建客户端）
func (p *MinIOPool) UpdateConfig(newConfig *MinIOPoolConfig) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	// 关闭当前连接
	p.Close()
	
	// 创建新的连接池
	newPool, err := NewMinIOPool(newConfig)
	if err != nil {
		return err
	}
	
	// 更新当前实例
	p.client = newPool.client
	p.config = newPool.config
	p.transport = newPool.transport
	
	return nil
} 