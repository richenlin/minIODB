package security

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitTier 限流等级
type RateLimitTier struct {
	Name            string        `yaml:"name"`
	RequestsPerSec  float64       `yaml:"requests_per_sec"`
	BurstSize       int           `yaml:"burst_size"`
	Window          time.Duration `yaml:"window"`
	BackoffDuration time.Duration `yaml:"backoff_duration"`
}

// PathRateLimit 路径限流配置
type PathRateLimit struct {
	Pattern string `yaml:"pattern"`
	Tier    string `yaml:"tier"`
	Enabled bool   `yaml:"enabled"`
}

// SmartRateLimiterConfig 智能限流器配置
type SmartRateLimiterConfig struct {
	Enabled     bool              `yaml:"enabled"`
	DefaultTier string            `yaml:"default_tier"`
	Tiers       []RateLimitTier   `yaml:"tiers"`
	PathLimits  []PathRateLimit   `yaml:"path_limits"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

// TokenBucket 令牌桶实现
type TokenBucket struct {
	capacity       int           // 桶容量
	tokens         int           // 当前令牌数
	refillRate     float64       // 令牌补充速率 (tokens/second)
	lastRefill     time.Time     // 上次补充时间
	mutex          sync.RWMutex  // 读写锁
}

// NewTokenBucket 创建新的令牌桶
func NewTokenBucket(capacity int, refillRate float64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity, // 初始时桶是满的
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// TryConsume 尝试消费令牌
func (tb *TokenBucket) TryConsume(tokens int) bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	// 补充令牌
	tb.refill()
	
	// 检查是否有足够的令牌
	if tb.tokens >= tokens {
		tb.tokens -= tokens
		return true
	}
	
	return false
}

// refill 补充令牌（内部方法，调用时需要持有锁）
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	
	// 计算应该补充的令牌数
	tokensToAdd := int(elapsed * tb.refillRate)
	if tokensToAdd > 0 {
		tb.tokens += tokensToAdd
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}
		tb.lastRefill = now
	}
}

// GetWaitTime 获取下次可用时间
func (tb *TokenBucket) GetWaitTime(tokens int) time.Duration {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()
	
	tb.refill()
	
	if tb.tokens >= tokens {
		return 0
	}
	
	// 计算需要等待的时间
	tokensNeeded := tokens - tb.tokens
	waitSeconds := float64(tokensNeeded) / tb.refillRate
	return time.Duration(waitSeconds * float64(time.Second))
}

// ClientRateLimit 客户端限流状态
type ClientRateLimit struct {
	tokenBucket    *TokenBucket
	backoffUntil   time.Time
	violationCount int
	lastViolation  time.Time
	mutex          sync.RWMutex
}

// SmartRateLimiter 智能限流器
type SmartRateLimiter struct {
	config  SmartRateLimiterConfig
	clients map[string]*ClientRateLimit
	tiers   map[string]RateLimitTier
	mutex   sync.RWMutex
}

// NewSmartRateLimiter 创建智能限流器
func NewSmartRateLimiter(config SmartRateLimiterConfig) *SmartRateLimiter {
	limiter := &SmartRateLimiter{
		config:  config,
		clients: make(map[string]*ClientRateLimit),
		tiers:   make(map[string]RateLimitTier),
	}
	
	// 构建等级映射
	for _, tier := range config.Tiers {
		limiter.tiers[tier.Name] = tier
	}
	
	// 启动清理goroutine
	go limiter.cleanup()
	
	return limiter
}

// GetDefaultConfig 获取默认配置
func GetDefaultSmartRateLimiterConfig() SmartRateLimiterConfig {
	return SmartRateLimiterConfig{
		Enabled:     true,
		DefaultTier: "standard",
		Tiers: []RateLimitTier{
			{
				Name:            "health",
				RequestsPerSec:  100,
				BurstSize:       200,
				Window:          time.Minute,
				BackoffDuration: 1 * time.Second,
			},
			{
				Name:            "query",
				RequestsPerSec:  50,
				BurstSize:       100,
				Window:          time.Minute,
				BackoffDuration: 2 * time.Second,
			},
			{
				Name:            "write",
				RequestsPerSec:  30,
				BurstSize:       60,
				Window:          time.Minute,
				BackoffDuration: 5 * time.Second,
			},
			{
				Name:            "standard",
				RequestsPerSec:  20,
				BurstSize:       40,
				Window:          time.Minute,
				BackoffDuration: 3 * time.Second,
			},
			{
				Name:            "strict",
				RequestsPerSec:  10,
				BurstSize:       20,
				Window:          time.Minute,
				BackoffDuration: 10 * time.Second,
			},
		},
		PathLimits: []PathRateLimit{
			{Pattern: "/v1/health", Tier: "health", Enabled: true},
			{Pattern: "/health", Tier: "health", Enabled: true},
			{Pattern: "/metrics", Tier: "health", Enabled: true},
			{Pattern: "/v1/query", Tier: "query", Enabled: true},
			{Pattern: "/v1/data", Tier: "write", Enabled: true},
			{Pattern: "/v1/tables", Tier: "write", Enabled: true},
			{Pattern: "/v1/backup", Tier: "strict", Enabled: true},
		},
		CleanupInterval: 5 * time.Minute,
	}
}

// getTierForPath 获取路径对应的限流等级
func (srl *SmartRateLimiter) getTierForPath(path string) RateLimitTier {
	// 检查路径配置
	for _, pathLimit := range srl.config.PathLimits {
		if pathLimit.Enabled && strings.HasPrefix(path, pathLimit.Pattern) {
			if tier, exists := srl.tiers[pathLimit.Tier]; exists {
				return tier
			}
		}
	}
	
	// 返回默认等级
	if defaultTier, exists := srl.tiers[srl.config.DefaultTier]; exists {
		return defaultTier
	}
	
	// 如果没有默认等级，返回一个保守的配置
	return RateLimitTier{
		Name:            "fallback",
		RequestsPerSec:  10,
		BurstSize:       20,
		Window:          time.Minute,
		BackoffDuration: 5 * time.Second,
	}
}

// getClientRateLimit 获取或创建客户端限流状态
func (srl *SmartRateLimiter) getClientRateLimit(clientIP string, tier RateLimitTier) *ClientRateLimit {
	key := fmt.Sprintf("%s:%s", clientIP, tier.Name)
	
	srl.mutex.RLock()
	client, exists := srl.clients[key]
	srl.mutex.RUnlock()
	
	if !exists {
		srl.mutex.Lock()
		// 双重检查
		if client, exists = srl.clients[key]; !exists {
			client = &ClientRateLimit{
				tokenBucket: NewTokenBucket(tier.BurstSize, tier.RequestsPerSec),
			}
			srl.clients[key] = client
		}
		srl.mutex.Unlock()
	}
	
	return client
}

// checkRateLimit 检查限流
func (srl *SmartRateLimiter) checkRateLimit(clientIP, path string) (bool, time.Duration, error) {
	if !srl.config.Enabled {
		return true, 0, nil
	}
	
	tier := srl.getTierForPath(path)
	client := srl.getClientRateLimit(clientIP, tier)
	
	client.mutex.Lock()
	defer client.mutex.Unlock()
	
	now := time.Now()
	
	// 检查是否在退避期
	if now.Before(client.backoffUntil) {
		waitTime := client.backoffUntil.Sub(now)
		return false, waitTime, fmt.Errorf("client in backoff period")
	}
	
	// 尝试消费令牌
	if client.tokenBucket.TryConsume(1) {
		// 成功消费，重置违规计数
		if now.Sub(client.lastViolation) > tier.Window {
			client.violationCount = 0
		}
		return true, 0, nil
	}
	
	// 令牌不足，记录违规
	client.violationCount++
	client.lastViolation = now
	
	// 计算等待时间
	waitTime := client.tokenBucket.GetWaitTime(1)
	
	// 如果违规次数过多，进入退避期
	if client.violationCount >= 5 {
		backoffDuration := tier.BackoffDuration * time.Duration(client.violationCount-4)
		if backoffDuration > 5*time.Minute {
			backoffDuration = 5 * time.Minute
		}
		client.backoffUntil = now.Add(backoffDuration)
		waitTime = backoffDuration
	}
	
	return false, waitTime, fmt.Errorf("rate limit exceeded")
}

// cleanup 清理过期的客户端数据
func (srl *SmartRateLimiter) cleanup() {
	ticker := time.NewTicker(srl.config.CleanupInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		srl.mutex.Lock()
		now := time.Now()
		for key, client := range srl.clients {
			client.mutex.RLock()
			// 如果客户端长时间没有活动，删除它
			inactive := now.Sub(client.lastViolation) > 2*srl.config.CleanupInterval && 
						 now.After(client.backoffUntil)
			client.mutex.RUnlock()
			
			if inactive {
				delete(srl.clients, key)
			}
		}
		srl.mutex.Unlock()
	}
}

// Middleware 返回Gin中间件
func (srl *SmartRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		path := c.Request.URL.Path
		
		allowed, waitTime, err := srl.checkRateLimit(clientIP, path)
		
		if !allowed {
			tier := srl.getTierForPath(path)
			
			// 设置响应头
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%.0f", tier.RequestsPerSec))
			c.Header("X-RateLimit-Burst", fmt.Sprintf("%d", tier.BurstSize))
			c.Header("X-RateLimit-Window", tier.Window.String())
			
			if waitTime > 0 {
				c.Header("Retry-After", fmt.Sprintf("%.0f", waitTime.Seconds()))
			}
			
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate_limit_exceeded",
				"message":     fmt.Sprintf("Rate limit exceeded for %s API", tier.Name),
				"tier":        tier.Name,
				"limit":       tier.RequestsPerSec,
				"burst":       tier.BurstSize,
				"window":      tier.Window.String(),
				"retry_after": fmt.Sprintf("%.1fs", waitTime.Seconds()),
				"details":     err.Error(),
			})
			c.Abort()
			return
		}
		
		c.Next()
	}
}

// GetStats 获取限流统计信息
func (srl *SmartRateLimiter) GetStats() map[string]interface{} {
	srl.mutex.RLock()
	defer srl.mutex.RUnlock()
	
	stats := map[string]interface{}{
		"enabled":       srl.config.Enabled,
		"total_clients": len(srl.clients),
		"tiers":         len(srl.tiers),
		"path_limits":   len(srl.config.PathLimits),
	}
	
	// 统计各等级的客户端数量
	tierStats := make(map[string]int)
	for key := range srl.clients {
		parts := strings.Split(key, ":")
		if len(parts) == 2 {
			tier := parts[1]
			tierStats[tier]++
		}
	}
	stats["tier_distribution"] = tierStats
	
	return stats
} 