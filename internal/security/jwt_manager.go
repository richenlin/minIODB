package security

import (
	"time"
)

// JWTManager 简单的JWT管理器包装器
type JWTManager struct {
	authManager *AuthManager
}

// NewJWTManager 创建新的JWT管理器
func NewJWTManager(secret string, expiration time.Duration) *JWTManager {
	authConfig := &AuthConfig{
		Mode:            "jwt",
		JWTSecret:       secret,
		TokenExpiration: expiration,
		Issuer:          "miniodb",
		Audience:        "miniodb-api",
	}

	authManager, err := NewAuthManager(authConfig)
	if err != nil {
		// 如果创建失败，使用默认配置
		authManager, _ = NewAuthManager(&AuthConfig{
			Mode:            "jwt",
			JWTSecret:       "default-secret-change-in-production",
			TokenExpiration: 24 * time.Hour,
			Issuer:          "miniodb",
			Audience:        "miniodb-api",
		})
	}

	return &JWTManager{
		authManager: authManager,
	}
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