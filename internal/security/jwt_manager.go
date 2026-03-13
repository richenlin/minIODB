package security

import (
	"fmt"
	"time"
)

// JWTManager 简单的JWT管理器包装器
type JWTManager struct {
	authManager *AuthManager
}

// NewJWTManager 创建新的JWT管理器
// 当 secret 为空时返回 error，强制要求配置 JWT Secret
func NewJWTManager(secret string, expiration time.Duration) (*JWTManager, error) {
	if secret == "" {
		return nil, fmt.Errorf("JWT secret must be configured, cannot be empty")
	}

	authConfig := &AuthConfig{
		Mode:            "jwt",
		JWTSecret:       secret,
		TokenExpiration: expiration,
		Issuer:          "miniodb",
		Audience:        "miniodb-api",
	}

	authManager, err := NewAuthManager(authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth manager: %w", err)
	}

	return &JWTManager{
		authManager: authManager,
	}, nil
}

// GenerateToken 生成JWT令牌
func (jm *JWTManager) GenerateToken(userID string) (string, error) {
	return jm.authManager.GenerateToken(userID, userID)
}

// ValidateToken 验证JWT令牌
func (jm *JWTManager) ValidateToken(token string) (string, error) {
	claims, err := jm.authManager.ValidateToken(token)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}