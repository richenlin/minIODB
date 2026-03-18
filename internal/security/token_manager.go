package security

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

const (
	tokenBlacklistKeyPrefix = "token:blacklist:"
	refreshTokenKeyPrefix   = "token:refresh:"
	tokenBlacklistTTL       = 24 * time.Hour
	refreshTokenTTL         = 7 * 24 * time.Hour
	tokenBlacklistBucket    = "security"
	tokenBlacklistPrefix    = "blacklist/"
)

type MinioUploader interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error)
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
}

type TokenManager struct {
	redisClient     *redis.Client
	minioClient     MinioUploader
	localBlacklist  map[string]time.Time
	localRefreshMap map[string]string
	mutex           sync.RWMutex
	useRedis        bool
	useMinio        bool
	stopCh          chan struct{}
	wg              sync.WaitGroup
	logger          *zap.Logger
}

type blacklistedToken struct {
	Token     string    `json:"token"`
	RevokedAt time.Time `json:"revoked_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewTokenManager 保留原有构造函数（向后兼容）
func NewTokenManager(redisClient *redis.Client) *TokenManager {
	return NewTokenManagerWithContext(context.Background(), redisClient, nil, nil)
}

// NewTokenManagerWithMinIO 创建带MinIO持久化的TokenManager
func NewTokenManagerWithMinIO(ctx context.Context, redisClient *redis.Client, minioClient MinioUploader, logger *zap.Logger) *TokenManager {
	return NewTokenManagerWithContext(ctx, redisClient, minioClient, logger)
}

// NewTokenManagerWithContext 完整构造函数
func NewTokenManagerWithContext(ctx context.Context, redisClient *redis.Client, minioClient MinioUploader, logger *zap.Logger) *TokenManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	tm := &TokenManager{
		redisClient:     redisClient,
		minioClient:     minioClient,
		localBlacklist:  make(map[string]time.Time),
		localRefreshMap: make(map[string]string),
		useRedis:        redisClient != nil,
		useMinio:        minioClient != nil,
		stopCh:          make(chan struct{}),
		logger:          logger,
	}

	if !tm.useRedis {
		tm.wg.Add(1)
		go tm.cleanupLocalBlacklist()
	}

	if tm.useMinio {
		tm.wg.Add(1)
		go tm.loadBlacklistFromMinIO(ctx)
	}

	go func() {
		<-ctx.Done()
		tm.Stop()
	}()

	return tm
}

func (tm *TokenManager) hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (tm *TokenManager) ensureBucketExists(ctx context.Context) error {
	if tm.minioClient == nil {
		return fmt.Errorf("minio client not configured")
	}

	exists, err := tm.minioClient.BucketExists(ctx, tokenBlacklistBucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		if err := tm.minioClient.MakeBucket(ctx, tokenBlacklistBucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		tm.logger.Info("Created security bucket for token blacklist", zap.String("bucket", tokenBlacklistBucket))
	}

	return nil
}

func (tm *TokenManager) persistToMinIO(ctx context.Context, tokenHash string, revokedAt, expiresAt time.Time) error {
	if !tm.useMinio {
		return nil
	}

	if err := tm.ensureBucketExists(ctx); err != nil {
		tm.logger.Warn("Failed to ensure bucket exists", zap.Error(err))
		return err
	}

	entry := blacklistedToken{
		RevokedAt: revokedAt,
		ExpiresAt: expiresAt,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal blacklist entry: %w", err)
	}

	objectName := tokenBlacklistPrefix + tokenHash + ".json"
	reader := bytes.NewReader(data)

	_, err = tm.minioClient.PutObject(ctx, tokenBlacklistBucket, objectName, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/json",
	})
	if err != nil {
		tm.logger.Warn("Failed to persist token to MinIO",
			zap.String("token_hash", tokenHash),
			zap.Error(err))
		return fmt.Errorf("failed to persist token to MinIO: %w", err)
	}

	tm.logger.Debug("Token persisted to MinIO", zap.String("token_hash", tokenHash))
	return nil
}

func (tm *TokenManager) loadBlacklistFromMinIO(ctx context.Context) {
	defer tm.wg.Done()

	if tm.minioClient == nil {
		return
	}

	if err := tm.ensureBucketExists(ctx); err != nil {
		tm.logger.Warn("Failed to ensure bucket exists during load", zap.Error(err))
		return
	}

	objectCh := tm.minioClient.ListObjects(ctx, tokenBlacklistBucket, minio.ListObjectsOptions{
		Prefix:    tokenBlacklistPrefix,
		Recursive: true,
	})

	loadedCount := 0
	now := time.Now()

	for object := range objectCh {
		if object.Err != nil {
			tm.logger.Warn("Error listing blacklist objects", zap.Error(object.Err))
			continue
		}

		if !strings.HasSuffix(object.Key, ".json") {
			continue
		}

		data, err := tm.minioClient.GetObject(ctx, tokenBlacklistBucket, object.Key, minio.GetObjectOptions{})
		if err != nil {
			tm.logger.Warn("Failed to get blacklist object",
				zap.String("object", object.Key),
				zap.Error(err))
			continue
		}

		var entry blacklistedToken
		if err := json.Unmarshal(data, &entry); err != nil {
			tm.logger.Warn("Failed to unmarshal blacklist entry",
				zap.String("object", object.Key),
				zap.Error(err))
			continue
		}

		if now.After(entry.ExpiresAt) {
			continue
		}

		tokenHash := strings.TrimSuffix(strings.TrimPrefix(object.Key, tokenBlacklistPrefix), ".json")

		tm.mutex.Lock()
		tm.localBlacklist[tokenHash] = entry.ExpiresAt
		tm.mutex.Unlock()

		if tm.useRedis {
			redisKey := tokenBlacklistKeyPrefix + tokenHash
			ttl := time.Until(entry.ExpiresAt)
			if ttl > 0 {
				if err := tm.redisClient.Set(ctx, redisKey, "revoked", ttl).Err(); err != nil {
					tm.logger.Warn("Failed to sync token to Redis",
						zap.String("token_hash", tokenHash),
						zap.Error(err))
				}
			}
		}

		loadedCount++
	}

	if loadedCount > 0 {
		tm.logger.Info("Loaded blacklist from MinIO",
			zap.Int("count", loadedCount))
	}
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
	tokenHash := tm.hashToken(token)
	expiresAt := time.Now().Add(tokenBlacklistTTL)

	if tm.useMinio {
		if err := tm.persistToMinIO(ctx, tokenHash, time.Now(), expiresAt); err != nil {
			tm.logger.Warn("Failed to persist token to MinIO, continuing with Redis/memory", zap.Error(err))
		}
	}

	if tm.useRedis {
		key := tokenBlacklistKeyPrefix + tokenHash
		if err := tm.redisClient.Set(ctx, key, "revoked", tokenBlacklistTTL).Err(); err != nil {
			return err
		}
		return nil
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	tm.localBlacklist[tokenHash] = expiresAt
	return nil
}

func (tm *TokenManager) IsTokenRevoked(ctx context.Context, token string) bool {
	tokenHash := tm.hashToken(token)

	if tm.useRedis {
		key := tokenBlacklistKeyPrefix + tokenHash
		exists, err := tm.redisClient.Exists(ctx, key).Result()
		return err == nil && exists > 0
	}

	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	expiry, exists := tm.localBlacklist[tokenHash]
	return exists && time.Now().Before(expiry)
}

func (tm *TokenManager) cleanupLocalBlacklist() {
	defer tm.wg.Done()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tm.mutex.Lock()
			now := time.Now()
			cleaned := 0
			for token, expiry := range tm.localBlacklist {
				if now.After(expiry) {
					delete(tm.localBlacklist, token)
					cleaned++
				}
			}
			tm.mutex.Unlock()

		case <-tm.stopCh:
			return
		}
	}
}

// Stop 停止TokenManager的后台goroutine
func (tm *TokenManager) Stop() {
	close(tm.stopCh)
	tm.wg.Wait()
}
