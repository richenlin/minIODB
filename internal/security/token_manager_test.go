package security

import (
	"context"
	"testing"
)

func TestTokenManager_GenerateRefreshToken(t *testing.T) {
	tm := NewTokenManager(nil) // 使用本地存储

	token1, err := tm.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	token2, err := tm.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	// 令牌应该不同
	if token1 == token2 {
		t.Error("Generated tokens should be different")
	}

	// 令牌长度应该正确（32字节 = 64字符十六进制）
	if len(token1) != 64 {
		t.Errorf("Token length = %d, want 64", len(token1))
	}
}

func TestTokenManager_StoreAndValidateRefreshToken(t *testing.T) {
	tm := NewTokenManager(nil)
	ctx := context.Background()

	token, err := tm.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	userID := "test-user-123"

	// 存储令牌
	err = tm.StoreRefreshToken(ctx, token, userID)
	if err != nil {
		t.Fatalf("StoreRefreshToken() error = %v", err)
	}

	// 验证令牌
	retrievedUserID, err := tm.ValidateRefreshToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}

	if retrievedUserID != userID {
		t.Errorf("ValidateRefreshToken() = %v, want %v", retrievedUserID, userID)
	}
}

func TestTokenManager_RevokeRefreshToken(t *testing.T) {
	tm := NewTokenManager(nil)
	ctx := context.Background()

	token, err := tm.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	userID := "test-user-123"

	// 存储令牌
	err = tm.StoreRefreshToken(ctx, token, userID)
	if err != nil {
		t.Fatalf("StoreRefreshToken() error = %v", err)
	}

	// 撤销令牌
	err = tm.RevokeRefreshToken(ctx, token)
	if err != nil {
		t.Fatalf("RevokeRefreshToken() error = %v", err)
	}

	// 验证令牌应该失败
	_, err = tm.ValidateRefreshToken(ctx, token)
	if err == nil {
		t.Error("Expected error when validating revoked token")
	}
}

func TestTokenManager_AccessTokenBlacklist(t *testing.T) {
	tm := NewTokenManager(nil)
	ctx := context.Background()

	accessToken := "test-access-token"

	// 令牌应该不在黑名单中
	if tm.IsTokenRevoked(ctx, accessToken) {
		t.Error("Token should not be revoked initially")
	}

	// 撤销访问令牌
	err := tm.RevokeAccessToken(ctx, accessToken)
	if err != nil {
		t.Fatalf("RevokeAccessToken() error = %v", err)
	}

	// 令牌应该在黑名单中
	if !tm.IsTokenRevoked(ctx, accessToken) {
		t.Error("Token should be revoked after RevokeAccessToken")
	}
}

func TestTokenManager_InvalidRefreshToken(t *testing.T) {
	tm := NewTokenManager(nil)
	ctx := context.Background()

	// 验证不存在的令牌
	_, err := tm.ValidateRefreshToken(ctx, "non-existent-token")
	if err == nil {
		t.Error("Expected error when validating non-existent token")
	}
}

func TestTokenManager_EmptyToken(t *testing.T) {
	tm := NewTokenManager(nil)
	ctx := context.Background()

	// 验证空令牌
	_, err := tm.ValidateRefreshToken(ctx, "")
	if err == nil {
		t.Error("Expected error when validating empty token")
	}
}

// TestTokenSecurity 测试令牌安全性
func TestTokenSecurity(t *testing.T) {
	tm := NewTokenManager(nil)

	// 生成多个令牌，确保它们都是唯一的
	tokens := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		token, err := tm.GenerateRefreshToken()
		if err != nil {
			t.Fatalf("GenerateRefreshToken() error = %v", err)
		}

		if tokens[token] {
			t.Errorf("Duplicate token generated: %s", token)
		}
		tokens[token] = true
	}
}

// BenchmarkGenerateRefreshToken 基准测试令牌生成性能
func BenchmarkGenerateRefreshToken(b *testing.B) {
	tm := NewTokenManager(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tm.GenerateRefreshToken()
		if err != nil {
			b.Fatalf("GenerateRefreshToken() error = %v", err)
		}
	}
}

// BenchmarkStoreRefreshToken 基准测试令牌存储性能
func BenchmarkStoreRefreshToken(b *testing.B) {
	tm := NewTokenManager(nil)
	ctx := context.Background()

	tokens := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		token, _ := tm.GenerateRefreshToken()
		tokens[i] = token
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := tm.StoreRefreshToken(ctx, tokens[i], "user-123")
		if err != nil {
			b.Fatalf("StoreRefreshToken() error = %v", err)
		}
	}
}
