package security

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	tokenBlacklistKeyPrefix = "token:blacklist:"
	refreshTokenKeyPrefix   = "token:refresh:"
	tokenBlacklistTTL       = 24 * time.Hour
	refreshTokenTTL         = 7 * 24 * time.Hour
)

type TokenManager struct {
	redisClient     *redis.Client
	localBlacklist  map[string]time.Time
	localRefreshMap map[string]string
	mutex           sync.RWMutex
	useRedis        bool
}

func NewTokenManager(redisClient *redis.Client) *TokenManager {
	tm := &TokenManager{
		redisClient:     redisClient,
		localBlacklist:  make(map[string]time.Time),
		localRefreshMap: make(map[string]string),
		useRedis:        redisClient != nil,
	}

	if !tm.useRedis {
		go tm.cleanupLocalBlacklist()
	}

	return tm
}

func (tm *TokenManager) GenerateRefreshToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate refresh token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func (tm *TokenManager) StoreRefreshToken(ctx context.Context, refreshToken, userID string) error {
	if tm.useRedis {
		key := refreshTokenKeyPrefix + refreshToken
		return tm.redisClient.Set(ctx, key, userID, refreshTokenTTL).Err()
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.localRefreshMap[refreshToken] = userID
	return nil
}

func (tm *TokenManager) ValidateRefreshToken(ctx context.Context, refreshToken string) (string, error) {
	if tm.useRedis {
		key := refreshTokenKeyPrefix + refreshToken
		userID, err := tm.redisClient.Get(ctx, key).Result()
		if err == redis.Nil {
			return "", fmt.Errorf("refresh token not found or expired")
		}
		if err != nil {
			return "", fmt.Errorf("failed to validate refresh token: %w", err)
		}
		return userID, nil
	}

	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	userID, exists := tm.localRefreshMap[refreshToken]
	if !exists {
		return "", fmt.Errorf("refresh token not found")
	}
	return userID, nil
}

func (tm *TokenManager) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if tm.useRedis {
		key := refreshTokenKeyPrefix + refreshToken
		return tm.redisClient.Del(ctx, key).Err()
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	delete(tm.localRefreshMap, refreshToken)
	return nil
}

func (tm *TokenManager) RevokeAccessToken(ctx context.Context, token string) error {
	if tm.useRedis {
		key := tokenBlacklistKeyPrefix + token
		return tm.redisClient.Set(ctx, key, "revoked", tokenBlacklistTTL).Err()
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.localBlacklist[token] = time.Now().Add(tokenBlacklistTTL)
	return nil
}

func (tm *TokenManager) IsTokenRevoked(ctx context.Context, token string) bool {
	if tm.useRedis {
		key := tokenBlacklistKeyPrefix + token
		exists, err := tm.redisClient.Exists(ctx, key).Result()
		return err == nil && exists > 0
	}

	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	expiry, exists := tm.localBlacklist[token]
	return exists && time.Now().Before(expiry)
}

func (tm *TokenManager) cleanupLocalBlacklist() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		tm.mutex.Lock()
		now := time.Now()
		for token, expiry := range tm.localBlacklist {
			if now.After(expiry) {
				delete(tm.localBlacklist, token)
			}
		}
		tm.mutex.Unlock()
	}
}
