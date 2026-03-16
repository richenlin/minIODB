package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// APIKeyPair API凭证对（包含Key和Secret）
type APIKeyPair struct {
	Key         string `yaml:"key" json:"key"`
	Secret      string `yaml:"secret" json:"secret"`
	Role        string `yaml:"role" json:"role"`                   // 预留字段，当前默认 "root"（全部权限）
	DisplayName string `yaml:"display_name" json:"display_name"` // 显示名称
}

// AuthConfig 认证配置
type AuthConfig struct {
	Mode            string        `yaml:"mode" json:"mode"`                         // "none" 或 "token"
	TokenExpiration time.Duration `yaml:"token_expiration" json:"token_expiration"`
	Issuer          string        `yaml:"issuer" json:"issuer"`
	Audience        string        `yaml:"audience" json:"audience"`
	// API凭证对：key 作为 JWT payload 的 user_id，secret 作为该用户专属的 HMAC 签名密钥
	APIKeyPairs []APIKeyPair `yaml:"api_key_pairs" json:"api_key_pairs"`
}

// DefaultAuthConfig 默认认证配置
var DefaultAuthConfig = AuthConfig{
	Mode:            "token",
	TokenExpiration: 24 * time.Hour,
	Issuer:          "miniodb",
	Audience:        "miniodb-api",
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

// credentialInfo 存储凭证信息（secret 明文保存，用作 JWT 签名密钥）
type credentialInfo struct {
	secret      string // 明文 secret，作为 JWT 签名/验证密钥
	role        string
	displayName string
}

// AuthManager 认证管理器
type AuthManager struct {
	config      *AuthConfig
	credentials map[string]credentialInfo // apiKey -> credentialInfo
	mu          sync.RWMutex
}

// NewAuthManager 创建认证管理器
func NewAuthManager(config *AuthConfig) (*AuthManager, error) {
	if config == nil {
		config = &DefaultAuthConfig
	}

	am := &AuthManager{
		config:      config,
		credentials: make(map[string]credentialInfo),
	}

	// 从配置加载凭证（secret 直接存储，不做哈希处理）
	for _, keyPair := range config.APIKeyPairs {
		if keyPair.Key != "" && keyPair.Secret != "" {
			role := keyPair.Role
			if role == "" {
				role = "root"
			}
			am.credentials[keyPair.Key] = credentialInfo{
				secret:      keyPair.Secret,
				role:        role,
				displayName: keyPair.DisplayName,
			}
		}
	}

	return am, nil
}

// IsEnabled 检查认证是否启用
func (am *AuthManager) IsEnabled() bool {
	return am.config.Mode == "token"
}

// ValidateToken 验证 JWT token
//
// 验证流程：
//  1. 不验证签名地解析 JWT，取出 payload 中的 user_id
//  2. 根据 user_id 查找该用户的 secret（作为签名密钥）
//  3. 用该 secret 完整验证 JWT 签名及有效期
func (am *AuthManager) ValidateToken(tokenString string) (*Claims, error) {
	if am.config.Mode == "none" {
		return nil, fmt.Errorf("authentication is disabled")
	}

	// Step 1：不验证签名，解析出 payload 取 user_id
	p := jwt.NewParser()
	unverified, _, err := p.ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	unverifiedClaims, ok := unverified.Claims.(*Claims)
	if !ok || unverifiedClaims.UserID == "" {
		return nil, fmt.Errorf("invalid token: missing user_id in payload")
	}

	// Step 2：根据 user_id 查找该用户的签名密钥
	am.mu.RLock()
	cred, exists := am.credentials[unverifiedClaims.UserID]
	am.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("invalid token: unknown user")
	}

	// Step 3：用用户自己的 secret 完整验证 JWT
	verified, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(cred.secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}

	if claims, ok := verified.Claims.(*Claims); ok && verified.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// GenerateToken 为指定用户生成 JWT，使用该用户自己的 secret 作为签名密钥
func (am *AuthManager) GenerateToken(userID, username string) (string, error) {
	if am.config.Mode == "none" {
		return "", fmt.Errorf("authentication is disabled")
	}

	am.mu.RLock()
	cred, exists := am.credentials[userID]
	am.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("user not found: %s", userID)
	}
	if cred.secret == "" {
		return "", fmt.Errorf("no secret configured for user: %s", userID)
	}

	expiration := am.config.TokenExpiration
	if expiration == 0 {
		expiration = 24 * time.Hour
	}

	claims := &Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    am.config.Issuer,
			Audience:  jwt.ClaimStrings{am.config.Audience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cred.secret))
}

// ValidateCredentials 验证 API 凭证，使用 constant-time 比对防止时序攻击
// 返回 matched=true 表示凭证有效，同时返回角色和显示名称
func (am *AuthManager) ValidateCredentials(apiKey, apiSecret string) (matched bool, role string, displayName string) {
	if apiKey == "" || apiSecret == "" {
		return false, "", ""
	}

	am.mu.RLock()
	cred, exists := am.credentials[apiKey]
	am.mu.RUnlock()

	if !exists {
		return false, "", ""
	}

	// constant-time 比对，防止时序攻击
	if subtle.ConstantTimeCompare([]byte(cred.secret), []byte(apiSecret)) == 1 {
		return true, cred.role, cred.displayName
	}
	return false, "", ""
}

// ValidateCredentialsWithRole 验证凭证并返回角色信息
// Deprecated: 请使用 ValidateCredentials，两者功能相同
func (am *AuthManager) ValidateCredentialsWithRole(apiKey, apiSecret string) (matched bool, role string, displayName string) {
	return am.ValidateCredentials(apiKey, apiSecret)
}

// AddCredential 添加或更新 API 凭证
func (am *AuthManager) AddCredential(apiKey, apiSecret string) error {
	return am.AddCredentialWithRole(apiKey, apiSecret, "root", "")
}

// AddCredentialWithRole 添加或更新 API 凭证（含角色信息）
func (am *AuthManager) AddCredentialWithRole(apiKey, apiSecret, role, displayName string) error {
	if apiKey == "" || apiSecret == "" {
		return nil
	}
	if role == "" {
		role = "root"
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	am.credentials[apiKey] = credentialInfo{
		secret:      apiSecret,
		role:        role,
		displayName: displayName,
	}
	return nil
}

// RemoveCredential 移除 API 凭证
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

// GetUserRole 获取指定用户的角色
func (am *AuthManager) GetUserRole(userID string) (string, bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	if cred, ok := am.credentials[userID]; ok {
		return cred.role, true
	}
	return "", false
}

// ExtractUserFromToken 从 token 中提取用户信息
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

// TokenExpiration 返回 token 过期时间配置
func (am *AuthManager) TokenExpiration() time.Duration {
	if am.config.TokenExpiration == 0 {
		return 24 * time.Hour
	}
	return am.config.TokenExpiration
}

// generateRandomString 生成随机十六进制字符串
func generateRandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// extractBearerToken 从 Authorization header 中提取 Bearer token
func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
