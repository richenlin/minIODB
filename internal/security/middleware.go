package security

import (
	"context"
	"net/http"
	"strings"
	"time"

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
}

// NewSecurityMiddleware 创建安全中间件
func NewSecurityMiddleware(authManager *AuthManager) *SecurityMiddleware {
	return &SecurityMiddleware{
		authManager: authManager,
	}
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

// CORS 跨域中间件
func (sm *SecurityMiddleware) CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		origin := c.Request.Header.Get("Origin")

		// 允许的来源
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}

		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, UPDATE")
		c.Header("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, Authorization")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Cache-Control, Content-Language, Content-Type")
		c.Header("Access-Control-Allow-Credentials", "true")

		// 处理预检请求
		if method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
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

// RateLimiter 简单的内存限流器
func (sm *SecurityMiddleware) RateLimiter(requestsPerMinute int) gin.HandlerFunc {
	// 简单的内存限流实现
	clients := make(map[string][]time.Time)
	
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		now := time.Now()
		
		// 清理过期的请求记录
		if requests, exists := clients[clientIP]; exists {
			var validRequests []time.Time
			for _, reqTime := range requests {
				if now.Sub(reqTime) < time.Minute {
					validRequests = append(validRequests, reqTime)
				}
			}
			clients[clientIP] = validRequests
		}
		
		// 检查请求频率
		if len(clients[clientIP]) >= requestsPerMinute {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": "Too many requests",
			})
			c.Abort()
			return
		}
		
		// 记录当前请求
		clients[clientIP] = append(clients[clientIP], now)
		
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