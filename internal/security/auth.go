package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// APIKeyPair API凭证对（包含Key和Secret）
type APIKeyPair struct {
	Key    string `yaml:"key" json:"key"`
	Secret string `yaml:"secret" json:"secret"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	Mode            string        `yaml:"mode" json:"mode"`                         // "none" 或 "token"
	JWTSecret       string        `yaml:"jwt_secret" json:"jwt_secret"`
	TokenExpiration time.Duration `yaml:"token_expiration" json:"token_expiration"`
	Issuer          string        `yaml:"issuer" json:"issuer"`
	Audience        string        `yaml:"audience" json:"audience"`
	// 预设的token列表，用于简单的token验证
	ValidTokens []string `yaml:"valid_tokens" json:"valid_tokens"`
	// API凭证对列表（推荐使用）
	APIKeyPairs []APIKeyPair `yaml:"api_key_pairs" json:"api_key_pairs"`
}

// DefaultAuthConfig 默认认证配置
var DefaultAuthConfig = AuthConfig{
	Mode:            "none",
	JWTSecret:       "",
	TokenExpiration: 24 * time.Hour,
	Issuer:          "miniodb",
	Audience:        "miniodb-api",
	ValidTokens:     []string{},
}

// Claims JWT声明
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// User 简化的用户结构
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// AuthManager 认证管理器
type AuthManager struct {
	config      *AuthConfig
	credentials map[string]string // apiKey -> bcrypt hash of apiSecret
	mu          sync.RWMutex      // 保护 credentials 的并发访问
}

// NewAuthManager 创建认证管理器
func NewAuthManager(config *AuthConfig) (*AuthManager, error) {
	if config == nil {
		config = &DefaultAuthConfig
	}

	// 如果是token模式但没有配置JWT密钥，生成一个
	if config.Mode == "token" && config.JWTSecret == "" {
		secret, err := generateRandomString(32)
		if err != nil {
			return nil, fmt.Errorf("failed to generate JWT secret: %w", err)
		}
		config.JWTSecret = secret
	}

	am := &AuthManager{
		config:      config,
		credentials: make(map[string]string),
	}

	// 从配置加载凭证
	for _, keyPair := range config.APIKeyPairs {
		if keyPair.Key != "" && keyPair.Secret != "" {
			am.credentials[keyPair.Key] = hashPassword(keyPair.Secret)
		}
	}

	return am, nil
}

// IsEnabled 检查认证是否启用
func (am *AuthManager) IsEnabled() bool {
	return am.config.Mode == "token"
}

// ValidateToken 验证token
func (am *AuthManager) ValidateToken(tokenString string) (*Claims, error) {
	if am.config.Mode == "none" {
		return nil, fmt.Errorf("authentication is disabled")
	}

	// 如果配置了预设token列表，先检查静态token
	if len(am.config.ValidTokens) > 0 {
		for _, validToken := range am.config.ValidTokens {
			if tokenString == validToken {
				// 返回默认用户信息
				return &Claims{
					UserID:   "static-user",
					Username: "static-user",
				}, nil
			}
		}
	}

	// 如果没有JWT密钥，只能使用静态token
	if am.config.JWTSecret == "" {
		return nil, fmt.Errorf("invalid token")
	}

	// 验证JWT token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(am.config.JWTSecret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// GenerateToken 生成JWT token
func (am *AuthManager) GenerateToken(userID, username string) (string, error) {
	if am.config.Mode == "none" {
		return "", fmt.Errorf("authentication is disabled")
	}

	if am.config.JWTSecret == "" {
		return "", fmt.Errorf("JWT secret not configured")
	}

	claims := &Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(am.config.TokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    am.config.Issuer,
			Audience:  []string{am.config.Audience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(am.config.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateCredentials 验证API凭证
// 返回 true 表示凭证有效，false 表示无效
func (am *AuthManager) ValidateCredentials(apiKey, apiSecret string) bool {
	if apiKey == "" || apiSecret == "" {
		return false
	}

	am.mu.RLock()
	defer am.mu.RUnlock()

	hashedSecret, exists := am.credentials[apiKey]
	if !exists {
		return false
	}

	return verifyPassword(apiSecret, hashedSecret)
}

// AddCredential 添加或更新API凭证（用于动态管理）
func (am *AuthManager) AddCredential(apiKey, apiSecret string) {
	if apiKey == "" || apiSecret == "" {
		return
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	am.credentials[apiKey] = hashPassword(apiSecret)
}

// RemoveCredential 移除API凭证
func (am *AuthManager) RemoveCredential(apiKey string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	delete(am.credentials, apiKey)
}

// HasCredentials 检查是否配置了任何凭证
func (am *AuthManager) HasCredentials() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return len(am.credentials) > 0
}

// ExtractUserFromToken 从token中提取用户信息
func (am *AuthManager) ExtractUserFromToken(tokenString string) (*User, error) {
	claims, err := am.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	return &User{
		ID:       claims.UserID,
		Username: claims.Username,
	}, nil
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// hashPassword 哈希密码
func hashPassword(password string) string {
	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hashedPassword)
}

// verifyPassword 验证密码
func verifyPassword(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
} 