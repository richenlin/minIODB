package security

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"minIODB/config"

	"github.com/gin-gonic/gin"
)

// ContextKey 上下文键类型
type ContextKey string

const (
	UserContextKey   ContextKey = "user"
	ClaimsContextKey ContextKey = "claims"
)

// SecurityMiddleware 安全中间件
type SecurityMiddleware struct {
	authManager *AuthManager
	corsConfig  config.CORSConfig
	stopCh      chan struct{}
}

// NewSecurityMiddleware 创建安全中间件
func NewSecurityMiddleware(authManager *AuthManager, corsConfig config.CORSConfig) *SecurityMiddleware {
	return &SecurityMiddleware{
		authManager: authManager,
		corsConfig:  corsConfig,
		stopCh:      make(chan struct{}),
	}
}

// Stop 停止安全中间件的后台goroutine
func (sm *SecurityMiddleware) Stop() {
	close(sm.stopCh)
}

// AuthRequired 需要认证的中间件
func (sm *SecurityMiddleware) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果认证被禁用，直接通过
		if !sm.authManager.IsEnabled() {
			c.Next()
			return
		}

		// 提取token
		token := sm.extractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "missing_token",
				"message": "Authorization token is required",
			})
			c.Abort()
			return
		}

		// 验证token
		claims, err := sm.authManager.ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "invalid_token",
				"message": "Invalid or expired token",
			})
			c.Abort()
			return
		}

		// 获取用户信息
		user := &User{
			ID:       claims.UserID,
			Username: claims.Username,
		}

		// 将用户信息和claims存储到上下文
		c.Set(string(UserContextKey), user)
		c.Set(string(ClaimsContextKey), claims)

		c.Next()
	}
}

// OptionalAuth 可选认证中间件
func (sm *SecurityMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果认证被禁用，直接通过
		if !sm.authManager.IsEnabled() {
			c.Next()
			return
		}

		// 提取token
		token := sm.extractToken(c)
		if token == "" {
			c.Next()
			return
		}

		// 验证token
		claims, err := sm.authManager.ValidateToken(token)
		if err != nil {
			// token无效，但不阻止请求
			c.Next()
			return
		}

		// 获取用户信息
		user := &User{
			ID:       claims.UserID,
			Username: claims.Username,
		}

		// 将用户信息和claims存储到上下文
		c.Set(string(UserContextKey), user)
		c.Set(string(ClaimsContextKey), claims)

		c.Next()
	}
}

// CORS 跨域中间件 - 使用白名单验证
// 安全规则：
// 1. 只有白名单中的 origin 才会被允许
// 2. 禁止 * 与 Allow-Credentials: true 同时出现
// 3. 空白名单表示不允许任何跨域请求
func (sm *SecurityMiddleware) CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		origin := c.Request.Header.Get("Origin")

		// 检查 origin 是否在白名单中
		allowedOrigin := sm.isOriginAllowed(origin)

		if allowedOrigin != "" {
			// 设置允许的来源（使用实际匹配的 origin，而不是反射）
			c.Header("Access-Control-Allow-Origin", allowedOrigin)

			// 设置允许的方法
			if len(sm.corsConfig.AllowMethods) > 0 {
				c.Header("Access-Control-Allow-Methods", strings.Join(sm.corsConfig.AllowMethods, ", "))
			}

			// 设置允许的请求头
			if len(sm.corsConfig.AllowHeaders) > 0 {
				c.Header("Access-Control-Allow-Headers", strings.Join(sm.corsConfig.AllowHeaders, ", "))
			}

			// 设置暴露的响应头
			c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Cache-Control, Content-Language, Content-Type")

			// 设置是否允许携带凭证
			// 注意：当 AllowCredentials 为 true 时，Access-Control-Allow-Origin 不能是 "*"
			if sm.corsConfig.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}

			// 设置预检请求缓存时间
			if sm.corsConfig.MaxAge > 0 {
				c.Header("Access-Control-Max-Age", fmt.Sprintf("%d", sm.corsConfig.MaxAge))
			}
		}
		// 如果 origin 不在白名单中，不设置任何 CORS 头，浏览器会阻止跨域请求

		// 处理预检请求
		if method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// isOriginAllowed 检查 origin 是否在白名单中
// 返回空字符串表示不允许，返回 origin 值表示允许
func (sm *SecurityMiddleware) isOriginAllowed(origin string) string {
	if origin == "" {
		return ""
	}

	// 遍历白名单检查
	for _, allowedOrigin := range sm.corsConfig.AllowedOrigins {
		// 支持精确匹配
		if allowedOrigin == origin {
			return origin
		}

		// 支持通配符匹配（例如：*.example.com）
		if strings.HasPrefix(allowedOrigin, "*.") {
			domain := allowedOrigin[2:] // 去掉 "*."
			// 检查是否以该域名结尾
			if strings.HasSuffix(origin, domain) {
				// 确保是子域名（不是任意后缀匹配）
				originHost := origin
				if strings.Contains(origin, "://") {
					parts := strings.SplitN(origin, "://", 2)
					if len(parts) == 2 {
						originHost = parts[1]
					}
				}
				// 去掉端口部分
				if strings.Contains(originHost, ":") {
					originHost = strings.Split(originHost, ":")[0]
				}
				if originHost == domain || strings.HasSuffix(originHost, "."+domain) {
					return origin
				}
			}
		}
	}

	return ""
}

// SecurityHeaders 安全头中间件
func (sm *SecurityMiddleware) SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		c.Next()
	}
}

// RequestLogger 请求日志中间件
func (sm *SecurityMiddleware) RequestLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return ""
	})
}

// RateLimiter 改进的内存限流器
func (sm *SecurityMiddleware) RateLimiter(requestsPerMinute int) gin.HandlerFunc {
	// 使用线程安全的实现
	type clientData struct {
		requests []time.Time
		mutex    sync.RWMutex
	}
	
	clients := make(map[string]*clientData)
	var globalMutex sync.RWMutex
	
	// 清理goroutine，每30秒清理一次过期数据
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-sm.stopCh:
				return
			case <-ticker.C:
				globalMutex.Lock()
				now := time.Now()
				for ip, data := range clients {
					data.mutex.Lock()
					var validRequests []time.Time
					for _, reqTime := range data.requests {
						if now.Sub(reqTime) < time.Minute {
							validRequests = append(validRequests, reqTime)
						}
					}
					if len(validRequests) == 0 {
						delete(clients, ip)
					} else {
						data.requests = validRequests
					}
					data.mutex.Unlock()
				}
				globalMutex.Unlock()
			}
		}
	}()
	
	return func(c *gin.Context) {
		// 如果requestsPerMinute <= 0，则不限流
		if requestsPerMinute <= 0 {
			c.Next()
			return
		}
		
		clientIP := c.ClientIP()
		now := time.Now()
		
		// 获取或创建客户端数据
		globalMutex.RLock()
		data, exists := clients[clientIP]
		globalMutex.RUnlock()
		
		if !exists {
			globalMutex.Lock()
			// 双重检查
			if data, exists = clients[clientIP]; !exists {
				data = &clientData{
					requests: make([]time.Time, 0, requestsPerMinute+10),
				}
				clients[clientIP] = data
			}
			globalMutex.Unlock()
		}
		
		data.mutex.Lock()
		defer data.mutex.Unlock()
		
		// 清理过期的请求记录
		var validRequests []time.Time
		for _, reqTime := range data.requests {
			if now.Sub(reqTime) < time.Minute {
				validRequests = append(validRequests, reqTime)
			}
		}
		data.requests = validRequests
		
		// 检查请求频率
		if len(data.requests) >= requestsPerMinute {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": fmt.Sprintf("Too many requests. Limit: %d requests per minute", requestsPerMinute),
				"limit":   requestsPerMinute,
				"window":  "1 minute",
			})
			c.Abort()
			return
		}
		
		// 记录当前请求
		data.requests = append(data.requests, now)
		
		c.Next()
	}
}

// extractToken 从请求中提取token
func (sm *SecurityMiddleware) extractToken(c *gin.Context) string {
	// 从Authorization头提取
	bearerToken := c.GetHeader("Authorization")
	if bearerToken != "" {
		if strings.HasPrefix(bearerToken, "Bearer ") {
			return strings.TrimPrefix(bearerToken, "Bearer ")
		}
		// 直接返回token（兼容不带Bearer前缀的情况）
		return bearerToken
	}

	// 从查询参数提取
	token := c.Query("token")
	if token != "" {
		return token
	}

	// 从表单提取
	token = c.PostForm("token")
	if token != "" {
		return token
	}

	return ""
}

// GetUserFromContext 从gin上下文获取用户信息
func GetUserFromContext(c *gin.Context) (*User, bool) {
	user, exists := c.Get(string(UserContextKey))
	if !exists {
		return nil, false
	}
	
	if u, ok := user.(*User); ok {
		return u, true
	}
	
	return nil, false
}

// GetClaimsFromContext 从gin上下文获取claims
func GetClaimsFromContext(c *gin.Context) (*Claims, bool) {
	claims, exists := c.Get(string(ClaimsContextKey))
	if !exists {
		return nil, false
	}
	
	if c, ok := claims.(*Claims); ok {
		return c, true
	}
	
	return nil, false
}

// ContextWithUser 将用户信息添加到context
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}

// UserFromContext 从context获取用户信息
func UserFromContext(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(UserContextKey).(*User)
	return user, ok
}

// ContextWithClaims 将claims添加到context
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, ClaimsContextKey, claims)
}

// ClaimsFromContext 从context获取claims
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey).(*Claims)
	return claims, ok
} 