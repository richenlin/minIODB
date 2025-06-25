package auth

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 认证中间件
type AuthMiddleware struct {
	authManager *AuthManager
	skipPaths   map[string]bool
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(authManager *AuthManager, skipPaths []string) *AuthMiddleware {
	skipMap := make(map[string]bool)
	for _, path := range skipPaths {
		skipMap[path] = true
	}
	
	return &AuthMiddleware{
		authManager: authManager,
		skipPaths:   skipMap,
	}
}

// AuthRequired 认证中间件函数
func (am *AuthMiddleware) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否跳过认证
		if am.skipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		// 获取认证头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
				"code":  "MISSING_AUTH",
			})
			c.Abort()
			return
		}

		// 验证认证信息
		claims, err := am.authManager.Authenticate(authHeader)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
				"code":  "AUTH_FAILED",
			})
			c.Abort()
			return
		}

		// 将认证信息存储到上下文
		c.Set("auth_claims", claims)
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("roles", claims.Roles)

		c.Next()
	}
}

// RequireRole 要求特定角色的中间件
func (am *AuthMiddleware) RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, exists := c.Get("auth_claims")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
				"code":  "MISSING_AUTH",
			})
			c.Abort()
			return
		}

		authClaims, ok := claims.(*Claims)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "invalid authentication claims",
				"code":  "INVALID_CLAIMS",
			})
			c.Abort()
			return
		}

		if !authClaims.HasRole(requiredRole) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "insufficient permissions",
				"code":  "INSUFFICIENT_PERMISSIONS",
				"required_role": requiredRole,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyRole 要求任一角色的中间件
func (am *AuthMiddleware) RequireAnyRole(requiredRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, exists := c.Get("auth_claims")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
				"code":  "MISSING_AUTH",
			})
			c.Abort()
			return
		}

		authClaims, ok := claims.(*Claims)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "invalid authentication claims",
				"code":  "INVALID_CLAIMS",
			})
			c.Abort()
			return
		}

		if !authClaims.HasAnyRole(requiredRoles...) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "insufficient permissions",
				"code":  "INSUFFICIENT_PERMISSIONS",
				"required_roles": requiredRoles,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// OptionalAuth 可选认证中间件
func (am *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		// 尝试验证认证信息
		claims, err := am.authManager.Authenticate(authHeader)
		if err == nil {
			// 认证成功，将信息存储到上下文
			c.Set("auth_claims", claims)
			c.Set("user_id", claims.UserID)
			c.Set("username", claims.Username)
			c.Set("roles", claims.Roles)
		}
		// 认证失败时不阻止请求，继续处理

		c.Next()
	}
}

// GetClaims 从Gin上下文获取认证声明
func GetClaims(c *gin.Context) (*Claims, bool) {
	claims, exists := c.Get("auth_claims")
	if !exists {
		return nil, false
	}

	authClaims, ok := claims.(*Claims)
	return authClaims, ok
}

// GetUserID 从Gin上下文获取用户ID
func GetUserID(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return "", false
	}

	id, ok := userID.(string)
	return id, ok
}

// GetUsername 从Gin上下文获取用户名
func GetUsername(c *gin.Context) (string, bool) {
	username, exists := c.Get("username")
	if !exists {
		return "", false
	}

	name, ok := username.(string)
	return name, ok
}

// GetRoles 从Gin上下文获取用户角色
func GetRoles(c *gin.Context) ([]string, bool) {
	roles, exists := c.Get("roles")
	if !exists {
		return nil, false
	}

	roleList, ok := roles.([]string)
	return roleList, ok
}

// HasRole 检查当前用户是否具有指定角色
func HasRole(c *gin.Context, role string) bool {
	roles, exists := GetRoles(c)
	if !exists {
		return false
	}

	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole 检查当前用户是否具有任一指定角色
func HasAnyRole(c *gin.Context, roles ...string) bool {
	userRoles, exists := GetRoles(c)
	if !exists {
		return false
	}

	for _, role := range roles {
		for _, userRole := range userRoles {
			if userRole == role {
				return true
			}
		}
	}
	return false
}

// IsAuthenticated 检查是否已认证
func IsAuthenticated(c *gin.Context) bool {
	_, exists := c.Get("auth_claims")
	return exists
}

// CORSMiddleware CORS中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// RateLimitMiddleware 简单的速率限制中间件
func RateLimitMiddleware(maxRequests int) gin.HandlerFunc {
	// 这里可以实现基于内存或Redis的速率限制
	// 为了简化，这里只是一个占位符实现
	return func(c *gin.Context) {
		// TODO: 实现实际的速率限制逻辑
		c.Next()
	}
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\"\n",
			param.ClientIP,
			param.TimeStamp.Format("02/Jan/2006:15:04:05 -0700"),
			param.Method,
			param.Path,
			param.Request.Proto,
			param.StatusCode,
			param.Latency,
			param.Request.UserAgent(),
			param.ErrorMessage,
		)
	})
}

// SecurityHeadersMiddleware 安全头中间件
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Next()
	}
}

// RequestIDMiddleware 请求ID中间件
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// generateRequestID 生成请求ID
func generateRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("%x", bytes)
}

// ErrorHandlerMiddleware 错误处理中间件
func ErrorHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 处理错误
		if len(c.Errors) > 0 {
			err := c.Errors.Last()
			
			// 根据错误类型返回不同的状态码
			switch {
			case strings.Contains(err.Error(), "invalid token"):
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "invalid token",
					"code":  "INVALID_TOKEN",
				})
			case strings.Contains(err.Error(), "insufficient permissions"):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "insufficient permissions",
					"code":  "INSUFFICIENT_PERMISSIONS",
				})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
					"code":  "INTERNAL_ERROR",
				})
			}
		}
	}
} 