package security

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTManager gRPC 层专用的 JWT 工具，使用单一全局 secret 签发/验证 token。
// 与 AuthManager 的 per-user 签名机制相互独立，不得混用。
type JWTManager struct {
	secret     string
	expiration time.Duration
	issuer     string
	audience   string
}

// NewJWTManager 创建 gRPC 专用 JWT 管理器，secret 不允许为空。
func NewJWTManager(secret string, expiration time.Duration) (*JWTManager, error) {
	if secret == "" {
		return nil, fmt.Errorf("JWT secret must be configured, cannot be empty")
	}
	return &JWTManager{
		secret:     secret,
		expiration: expiration,
		issuer:     "miniodb",
		audience:   "miniodb-api",
	}, nil
}

// GenerateToken 用全局 secret 生成 JWT，userID 写入 payload。
func (jm *JWTManager) GenerateToken(userID string) (string, error) {
	claims := &Claims{
		UserID:   userID,
		Username: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jm.expiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    jm.issuer,
			Audience:  jwt.ClaimStrings{jm.audience},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jm.secret))
}

// ValidateToken 验证 JWT 并返回 userID。
func (jm *JWTManager) ValidateToken(tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(jm.secret), nil
	})
	if err != nil {
		return "", fmt.Errorf("token validation failed: %w", err)
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims.UserID, nil
	}
	return "", fmt.Errorf("invalid token")
}
