package security

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAuthConfigCreation 测试AuthConfig创建
func TestAuthConfigCreation(t *testing.T) {
	config := &AuthConfig{
		Mode:            "token",
		JWTSecret:       "test-secret-key",
		TokenExpiration: 2 * time.Hour,
		Issuer:          "miniodb-test",
		Audience:        "miniodb-api",
		ValidTokens:     []string{"token1", "token2"},
	}

	assert.Equal(t, "token", config.Mode, "Mode should match")
	assert.Equal(t, "test-secret-key", config.JWTSecret, "JWT secret should match")
	assert.Equal(t, 2*time.Hour, config.TokenExpiration, "Token expiration should match")
	assert.Equal(t, "miniodb-test", config.Issuer, "Issuer should match")
	assert.Equal(t, "miniodb-api", config.Audience, "Audience should match")
	assert.Equal(t, []string{"token1", "token2"}, config.ValidTokens, "Valid tokens should match")
}

// TestDefaultAuthConfig 测试默认认证配置
func TestDefaultAuthConfig(t *testing.T) {
	config := DefaultAuthConfig

	assert.Equal(t, "none", config.Mode, "Default mode should be none")
	assert.Equal(t, "", config.JWTSecret, "Default JWT secret should be empty")
	assert.Equal(t, 24*time.Hour, config.TokenExpiration, "Default token expiration should be 24 hours")
	assert.Equal(t, "miniodb", config.Issuer, "Default issuer should be miniodb")
	assert.Equal(t, "miniodb-api", config.Audience, "Default audience should be miniodb-api")
	assert.Empty(t, config.ValidTokens, "Default valid tokens should be empty")
}

// TestClaimsCreation 测试JWT声明创建
func TestClaimsCreation(t *testing.T) {
	claims := &Claims{
		UserID:   "user123",
		Username: "testuser",
		Roles:    []string{"admin", "user"},
	}

	assert.Equal(t, "user123", claims.UserID, "User ID should match")
	assert.Equal(t, "testuser", claims.Username, "Username should match")
	assert.Equal(t, []string{"admin", "user"}, claims.Roles, "Roles should match")
}

// TestUserCreation 测试User结构体创建
func TestUserCreation(t *testing.T) {
	user := &User{
		ID:       "user456",
		Username: "anotheruser",
		Roles:    []string{"viewer", "editor"},
	}

	assert.Equal(t, "user456", user.ID, "User ID should match")
	assert.Equal(t, "anotheruser", user.Username, "Username should match")
	assert.Equal(t, []string{"viewer", "editor"}, user.Roles, "Roles should match")
}

// TestAuthConfigValidation 测试AuthConfig验证
func TestAuthConfigValidation(t *testing.T) {
	testCases := []struct {
		name     string
		config   *AuthConfig
		valid    bool
	}{
		{
			name: "Valid token mode config",
			config: &AuthConfig{
				Mode:            "token",
				JWTSecret:       "secret",
				TokenExpiration: time.Hour,
				Issuer:          "test",
				Audience:        "api",
			},
			valid: true,
		},
		{
			name: "Valid none mode config",
			config: &AuthConfig{
				Mode:            "none",
				TokenExpiration: time.Hour,
				Issuer:          "test",
				Audience:        "api",
			},
			valid: true,
		},
		{
			name: "Invalid mode",
			config: &AuthConfig{
				Mode:            "invalid",
				TokenExpiration: time.Hour,
				Issuer:          "test",
				Audience:        "api",
			},
			valid: false,
		},
		{
			name: "Empty JWT secret in token mode",
			config: &AuthConfig{
				Mode:            "token",
				JWTSecret:       "",
				TokenExpiration: time.Hour,
				Issuer:          "test",
				Audience:        "api",
			},
			valid: false,
		},
		{
			name: "Zero token expiration",
			config: &AuthConfig{
				Mode:            "token",
				JWTSecret:       "secret",
				TokenExpiration: 0,
				Issuer:          "test",
				Audience:        "api",
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 简单验证逻辑
			valid := validateAuthConfig(tc.config)
			assert.Equal(t, tc.valid, valid, "Validation result should match expected")
		})
	}
}

// TestClaimsValidation 测试JWT声明验证
func TestClaimsValidation(t *testing.T) {
	testCases := []struct {
		name   string
		claims *Claims
		valid  bool
	}{
		{
			name: "Valid claims",
			claims: &Claims{
				UserID:   "user123",
				Username: "testuser",
				Roles:    []string{"user"},
			},
			valid: true,
		},
		{
			name: "Empty user ID",
			claims: &Claims{
				UserID:   "",
				Username: "testuser",
				Roles:    []string{"user"},
			},
			valid: false,
		},
		{
			name: "Empty username",
			claims: &Claims{
				UserID:   "user123",
				Username: "",
				Roles:    []string{"user"},
			},
			valid: false,
		},
		{
			name: "Valid claims with multiple roles",
			claims: &Claims{
				UserID:   "user123",
				Username: "testuser",
				Roles:    []string{"admin", "user", "guest"},
			},
			valid: true,
		},
		{
			name: "Valid claims with empty roles",
			claims: &Claims{
				UserID:   "user123",
				Username: "testuser",
				Roles:    []string{},
			},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 简单验证逻辑
			valid := validateClaims(tc.claims)
			assert.Equal(t, tc.valid, valid, "Claims validation result should match expected")
		})
	}
}

// TestUserValidation 测试User验证
func TestUserValidation(t *testing.T) {
	testCases := []struct {
		name  string
		user  *User
		valid bool
	}{
		{
			name: "Valid user",
			user: &User{
				ID:       "user123",
				Username: "testuser",
				Roles:    []string{"user"},
			},
			valid: true,
		},
		{
			name: "Empty user ID",
			user: &User{
				ID:       "",
				Username: "testuser",
				Roles:    []string{"user"},
			},
			valid: false,
		},
		{
			name: "Empty username",
			user: &User{
				ID:       "user123",
				Username: "",
				Roles:    []string{"user"},
			},
			valid: false,
		},
		{
			name: "Valid user with multiple roles",
			user: &User{
				ID:       "user456",
				Username: "anotheruser",
				Roles:    []string{"admin", "editor"},
			},
			valid: true,
		},
		{
			name: "Valid user with empty roles",
			user: &User{
				ID:       "user789",
				Username: "thirduser",
				Roles:    []string{},
			},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 简单验证逻辑
			valid := validateUser(tc.user)
			assert.Equal(t, tc.valid, valid, "User validation result should match expected")
		})
	}
}

// TestAuthConfigCopy 测试AuthConfig复制
func TestAuthConfigCopy(t *testing.T) {
	original := &AuthConfig{
		Mode:            "token",
		JWTSecret:       "test-secret",
		TokenExpiration: time.Hour,
		Issuer:          "test-issuer",
		Audience:        "test-audience",
		ValidTokens:     []string{"token1", "token2"},
	}

	// 创建副本（模拟深度复制）
	copy := *original
	copy.JWTSecret = "modified-secret"
	copy.TokenExpiration = 2 * time.Hour

	// 验证原对象没有被修改
	assert.Equal(t, "test-secret", original.JWTSecret, "Original JWT secret should not be modified")
	assert.Equal(t, time.Hour, original.TokenExpiration, "Original token expiration should not be modified")

	// 验证副本对象被正确修改
	assert.Equal(t, "modified-secret", copy.JWTSecret, "Copied JWT secret should be modified")
	assert.Equal(t, 2*time.Hour, copy.TokenExpiration, "Copied token expiration should be modified")
}

// TestClaimsFields 测试Claims字段
func TestClaimsFields(t *testing.T) {
	claims := &Claims{
		UserID:   "test-user-id",
		Username: "test-username",
		Roles:    []string{"admin", "user"},
	}

	assert.Equal(t, "test-user-id", claims.UserID, "User ID should match")
	assert.Equal(t, "test-username", claims.Username, "Username should match")
	assert.Equal(t, []string{"admin", "user"}, claims.Roles, "Roles should match")
}

// BenchmarkAuthConfigCreation 基准测试AuthConfig创建性能
func BenchmarkAuthConfigCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &AuthConfig{
			Mode:            "token",
			JWTSecret:       "test-secret",
			TokenExpiration: time.Hour,
			Issuer:          "miniodb",
			Audience:        "miniodb-api",
			ValidTokens:     []string{"token1", "token2"},
		}
	}
}

// BenchmarkClaimsCreation 基准测试Claims创建性能
func BenchmarkClaimsCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &Claims{
			UserID:   "user123",
			Username: "testuser",
			Roles:    []string{"admin", "user"},
		}
	}
}

// validateAuthConfig 验证AuthConfig的辅助函数
func validateAuthConfig(config *AuthConfig) bool {
	if config.Mode == "token" && config.JWTSecret == "" {
		return false
	}
	if config.Mode == "token" && config.TokenExpiration <= 0 {
		return false
	}
	if config.Mode != "none" && config.Mode != "token" {
		return false
	}
	return true
}

// validateClaims 验证Claims的辅助函数
func validateClaims(claims *Claims) bool {
	if claims.UserID == "" {
		return false
	}
	if claims.Username == "" {
		return false
	}
	return true
}

// validateUser 验证User的辅助函数
func validateUser(user *User) bool {
	if user.ID == "" {
		return false
	}
	if user.Username == "" {
		return false
	}
	return true
}