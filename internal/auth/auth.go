package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrExpiredToken   = errors.New("token expired")
	ErrInvalidAPIKey  = errors.New("invalid API key")
	ErrMissingAuth    = errors.New("missing authentication")
	ErrInvalidAuthFormat = errors.New("invalid authentication format")
)

// Claims JWT声明
type Claims struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// AuthConfig 认证配置
type AuthConfig struct {
	JWTSecret     string        `mapstructure:"jwt_secret"`
	TokenExpiry   time.Duration `mapstructure:"token_expiry"`
	APIKeys       []string      `mapstructure:"api_keys"`
	EnableJWT     bool          `mapstructure:"enable_jwt"`
	EnableAPIKey  bool          `mapstructure:"enable_api_key"`
}

// AuthManager 认证管理器
type AuthManager struct {
	config    AuthConfig
	jwtSecret []byte
	apiKeys   map[string]bool
}

// NewAuthManager 创建认证管理器
func NewAuthManager(config AuthConfig) (*AuthManager, error) {
	if config.JWTSecret == "" && config.EnableJWT {
		return nil, errors.New("JWT secret is required when JWT is enabled")
	}
	
	if len(config.APIKeys) == 0 && config.EnableAPIKey {
		return nil, errors.New("API keys are required when API key authentication is enabled")
	}

	// 构建API密钥映射
	apiKeyMap := make(map[string]bool)
	for _, key := range config.APIKeys {
		apiKeyMap[key] = true
	}

	return &AuthManager{
		config:    config,
		jwtSecret: []byte(config.JWTSecret),
		apiKeys:   apiKeyMap,
	}, nil
}

// GenerateToken 生成JWT令牌
func (am *AuthManager) GenerateToken(userID, username string, roles []string) (string, error) {
	if !am.config.EnableJWT {
		return "", errors.New("JWT authentication is disabled")
	}

	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Roles:    roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(am.config.TokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "minIODB",
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(am.jwtSecret)
}

// ValidateToken 验证JWT令牌
func (am *AuthManager) ValidateToken(tokenString string) (*Claims, error) {
	if !am.config.EnableJWT {
		return nil, errors.New("JWT authentication is disabled")
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return am.jwtSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	// 检查令牌是否过期
	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return nil, ErrExpiredToken
	}

	return claims, nil
}

// ValidateAPIKey 验证API密钥
func (am *AuthManager) ValidateAPIKey(apiKey string) error {
	if !am.config.EnableAPIKey {
		return errors.New("API key authentication is disabled")
	}

	if apiKey == "" {
		return ErrInvalidAPIKey
	}

	// 使用常量时间比较防止时序攻击
	found := false
	for validKey := range am.apiKeys {
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(validKey)) == 1 {
			found = true
			break
		}
	}

	if !found {
		return ErrInvalidAPIKey
	}

	return nil
}

// ParseAuthHeader 解析认证头
func (am *AuthManager) ParseAuthHeader(authHeader string) (string, string, error) {
	if authHeader == "" {
		return "", "", ErrMissingAuth
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return "", "", ErrInvalidAuthFormat
	}

	authType := strings.ToLower(parts[0])
	credentials := parts[1]

	switch authType {
	case "bearer":
		return "jwt", credentials, nil
	case "apikey":
		return "apikey", credentials, nil
	default:
		return "", "", ErrInvalidAuthFormat
	}
}

// Authenticate 统一认证方法
func (am *AuthManager) Authenticate(authHeader string) (*Claims, error) {
	authType, credentials, err := am.ParseAuthHeader(authHeader)
	if err != nil {
		return nil, err
	}

	switch authType {
	case "jwt":
		return am.ValidateToken(credentials)
	case "apikey":
		if err := am.ValidateAPIKey(credentials); err != nil {
			return nil, err
		}
		// API密钥认证成功，返回默认声明
		return &Claims{
			UserID:   "api-user",
			Username: "API User",
			Roles:    []string{"api"},
		}, nil
	default:
		return nil, ErrInvalidAuthFormat
	}
}

// GenerateAPIKey 生成随机API密钥
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// HasRole 检查用户是否具有指定角色
func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole 检查用户是否具有任一指定角色
func (c *Claims) HasAnyRole(roles ...string) bool {
	for _, role := range roles {
		if c.HasRole(role) {
			return true
		}
	}
	return false
}

// AuthContext 认证上下文键
type AuthContext string

const (
	AuthContextKey AuthContext = "auth_claims"
)

// WithAuthContext 将认证信息添加到上下文
func WithAuthContext(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, AuthContextKey, claims)
}

// FromAuthContext 从上下文获取认证信息
func FromAuthContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(AuthContextKey).(*Claims)
	return claims, ok
}

// RequireRole 要求特定角色的中间件辅助函数
func RequireRole(claims *Claims, requiredRole string) error {
	if claims == nil {
		return ErrMissingAuth
	}
	
	if !claims.HasRole(requiredRole) {
		return fmt.Errorf("required role '%s' not found", requiredRole)
	}
	
	return nil
}

// RequireAnyRole 要求任一角色的中间件辅助函数
func RequireAnyRole(claims *Claims, requiredRoles ...string) error {
	if claims == nil {
		return ErrMissingAuth
	}
	
	if !claims.HasAnyRole(requiredRoles...) {
		return fmt.Errorf("required roles %v not found", requiredRoles)
	}
	
	return nil
} 