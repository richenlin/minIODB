package security

import (
	"context"
	"fmt"
	"minIODB/internal/monitoring"
	"net/http"
	"strings"
	"sync"
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

// TablePermissionRequired 表级权限验证中间件（新增）
func (sm *SecurityMiddleware) TablePermissionRequired(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从请求中提取表名（支持多种方式）
		tableName := extractTableName(c)

		if tableName == "" {
			// 如果没有指定表，允许通过（由后续逻辑处理）
			c.Next()
			return
		}

		// 获取用户信息
		userInterface, exists := c.Get(string(UserContextKey))
		if !exists && sm.authManager.IsEnabled() {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "authentication_required",
				"message": "Authentication required for table operations",
			})
			c.Abort()
			return
		}

		// 如果未认证，检查是否有ACL
		if !exists {
			aclMgr := sm.authManager.GetTableACLManager()
			if !aclMgr.CheckPermission(tableName, "", permission, []string{}) {
				c.JSON(http.StatusForbidden, gin.H{
					"error":   "permission_denied",
					"message": fmt.Sprintf("No %s permission for table %s", permission, tableName),
				})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		user := userInterface.(*User)
		aclMgr := sm.authManager.GetTableACLManager()

		// 检查权限
		hasPermission := aclMgr.CheckPermission(tableName, user.ID, permission, user.Roles)

		// 记录ACL检查指标
		result := "allowed"
		if !hasPermission {
			result = "denied"
		}
		// 导入 monitoring 包后取消注释
		monitoring.RecordTableACLCheck(tableName, string(permission), result)

		if !hasPermission {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "permission_denied",
				"message": fmt.Sprintf("User %s does not have %s permission for table %s", user.Username, permission, tableName),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractTableName 从请求中提取表名
func extractTableName(c *gin.Context) string {
	// 1. 尝试从URL参数获取
	tableName := c.Param("table")
	if tableName != "" {
		return tableName
	}

	// 2. 尝试从查询参数获取
	tableName = c.Query("table")
	if tableName != "" {
		return tableName
	}

	// 3. 尝试从请求头获取
	tableName = c.GetHeader("X-Table-Name")
	if tableName != "" {
		return tableName
	}

	// 4. 尝试从请求体获取
	if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut {
		var reqBody map[string]interface{}
		if err := c.ShouldBindJSON(&reqBody); err == nil {
			if t, ok := reqBody["table"].(string); ok {
				return t
			}
			if t, ok := reqBody["table_name"].(string); ok {
				return t
			}
		}
	}

	return ""
}
