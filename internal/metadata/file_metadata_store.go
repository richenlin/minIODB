package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"minIODB/internal/storage"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// FileMetadataStore 文件元数据存储接口
// 支持分布式模式（Redis）和单节点模式（MinIO sidecar）
type FileMetadataStore interface {
	// Store 存储文件元数据
	Store(ctx context.Context, objectName string, metadata *storage.FileMetadata) error
	// Load 加载文件元数据
	Load(ctx context.Context, objectName string) (*storage.FileMetadata, error)
	// LoadBatch 批量加载文件元数据
	LoadBatch(ctx context.Context, objectNames []string) ([]*storage.FileMetadata, error)
	// Delete 删除文件元数据
	Delete(ctx context.Context, objectName string) error
}

// =============================================================================
// Redis 实现（分布式模式）
// =============================================================================

// RedisMetadataStore Redis 元数据存储实现
type RedisMetadataStore struct {
	redisPool *pool.RedisPool
	keyPrefix string
	ttl       time.Duration
}

// NewRedisMetadataStore 创建 Redis 元数据存储
func NewRedisMetadataStore(redisPool *pool.RedisPool) *RedisMetadataStore {
	return &RedisMetadataStore{
		redisPool: redisPool,
		keyPrefix: "metadata:file:",
		ttl:       30 * 24 * time.Hour, // 30 天
	}
}

func (s *RedisMetadataStore) Store(ctx context.Context, objectName string, metadata *storage.FileMetadata) error {
	if s.redisPool == nil {
		return fmt.Errorf("redis pool not available")
	}

	client := s.redisPool.GetClient()
	metadataKey := s.keyPrefix + objectName

	// 序列化 map 字段
	minValuesJSON, _ := json.Marshal(metadata.MinValues)
	maxValuesJSON, _ := json.Marshal(metadata.MaxValues)
	nullCountsJSON, _ := json.Marshal(metadata.NullCounts)

	fields := map[string]interface{}{
		"file_path":   objectName,
		"file_size":   metadata.FileSize,
		"row_count":   metadata.RowCount,
		"row_groups":  metadata.RowGroupCount,
		"min_values":  string(minValuesJSON),
		"max_values":  string(maxValuesJSON),
		"null_counts": string(nullCountsJSON),
		"compression": metadata.CompressionType,
		"created_at":  metadata.CreatedAt.Format(time.RFC3339),
		"stored_at":   time.Now().Format(time.RFC3339),
	}

	if err := client.HSet(ctx, metadataKey, fields).Err(); err != nil {
		return fmt.Errorf("failed to store metadata in Redis: %w", err)
	}

	client.Expire(ctx, metadataKey, s.ttl)

	logger.GetLogger().Debug("Stored file metadata to Redis",
		zap.String("object", objectName),
		zap.Int64("row_count", metadata.RowCount))

	return nil
}

func (s *RedisMetadataStore) Load(ctx context.Context, objectName string) (*storage.FileMetadata, error) {
	if s.redisPool == nil {
		return nil, fmt.Errorf("redis pool not available")
	}

	client := s.redisPool.GetClient()
	metadataKey := s.keyPrefix + objectName

	fields, err := client.HGetAll(ctx, metadataKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata from Redis: %w", err)
	}

	if len(fields) == 0 {
		return nil, nil // 元数据不存在
	}

	return parseRedisFields(objectName, fields), nil
}

func (s *RedisMetadataStore) LoadBatch(ctx context.Context, objectNames []string) ([]*storage.FileMetadata, error) {
	result := make([]*storage.FileMetadata, 0, len(objectNames))

	for _, objectName := range objectNames {
		metadata, err := s.Load(ctx, objectName)
		if err != nil {
			logger.GetLogger().Warn("Failed to load metadata",
				zap.String("object", objectName),
				zap.Error(err))
			// 返回基础元数据
			metadata = &storage.FileMetadata{
				FilePath:  objectName,
				MinValues: make(map[string]interface{}),
				MaxValues: make(map[string]interface{}),
			}
		} else if metadata == nil {
			metadata = &storage.FileMetadata{
				FilePath:  objectName,
				MinValues: make(map[string]interface{}),
				MaxValues: make(map[string]interface{}),
			}
		}
		result = append(result, metadata)
	}

	return result, nil
}

func (s *RedisMetadataStore) Delete(ctx context.Context, objectName string) error {
	if s.redisPool == nil {
		return nil
	}

	client := s.redisPool.GetClient()
	metadataKey := s.keyPrefix + objectName
	return client.Del(ctx, metadataKey).Err()
}

// =============================================================================
// MinIO Sidecar 实现（单节点模式）
// =============================================================================

// MinIOMetadataStore MinIO sidecar 元数据存储实现
type MinIOMetadataStore struct {
	minioClient *minio.Client
	bucket      string
}

// NewMinIOMetadataStore 创建 MinIO 元数据存储
func NewMinIOMetadataStore(minioClient *minio.Client, bucket string) *MinIOMetadataStore {
	return &MinIOMetadataStore{
		minioClient: minioClient,
		bucket:      bucket,
	}
}

// getMetadataObjectName 获取元数据 sidecar 文件名
func (s *MinIOMetadataStore) getMetadataObjectName(objectName string) string {
	return objectName + ".meta.json"
}

func (s *MinIOMetadataStore) Store(ctx context.Context, objectName string, metadata *storage.FileMetadata) error {
	if s.minioClient == nil {
		return fmt.Errorf("minio client not available")
	}

	// 序列化元数据
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// 上传 sidecar 文件
	metaObjectName := s.getMetadataObjectName(objectName)
	reader := bytes.NewReader(metaJSON)

	_, err = s.minioClient.PutObject(ctx, s.bucket, metaObjectName, reader, int64(len(metaJSON)), minio.PutObjectOptions{
		ContentType: "application/json",
	})
	if err != nil {
		return fmt.Errorf("failed to upload metadata to MinIO: %w", err)
	}

	logger.GetLogger().Debug("Stored file metadata to MinIO sidecar",
		zap.String("object", objectName),
		zap.String("meta_object", metaObjectName),
		zap.Int64("row_count", metadata.RowCount))

	return nil
}

func (s *MinIOMetadataStore) Load(ctx context.Context, objectName string) (*storage.FileMetadata, error) {
	if s.minioClient == nil {
		return nil, fmt.Errorf("minio client not available")
	}

	metaObjectName := s.getMetadataObjectName(objectName)

	// 尝试获取 sidecar 文件
	obj, err := s.minioClient.GetObject(ctx, s.bucket, metaObjectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil // sidecar 不存在
	}
	defer obj.Close()

	// 检查对象是否存在
	_, err = obj.Stat()
	if err != nil {
		// 对象不存在
		return nil, nil
	}

	// 读取并解析
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata storage.FileMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// 转换值类型（JSON 解析后的数字是 float64）
	metadata.MinValues = convertMetadataValues(metadata.MinValues)
	metadata.MaxValues = convertMetadataValues(metadata.MaxValues)

	return &metadata, nil
}

func (s *MinIOMetadataStore) LoadBatch(ctx context.Context, objectNames []string) ([]*storage.FileMetadata, error) {
	result := make([]*storage.FileMetadata, 0, len(objectNames))

	for _, objectName := range objectNames {
		metadata, err := s.Load(ctx, objectName)
		if err != nil {
			logger.GetLogger().Debug("Failed to load metadata from sidecar",
				zap.String("object", objectName),
				zap.Error(err))
		}
		if metadata == nil {
			// sidecar 不存在，返回基础元数据
			metadata = &storage.FileMetadata{
				FilePath:  objectName,
				MinValues: make(map[string]interface{}),
				MaxValues: make(map[string]interface{}),
			}
		}
		result = append(result, metadata)
	}

	return result, nil
}

func (s *MinIOMetadataStore) Delete(ctx context.Context, objectName string) error {
	if s.minioClient == nil {
		return nil
	}

	metaObjectName := s.getMetadataObjectName(objectName)
	return s.minioClient.RemoveObject(ctx, s.bucket, metaObjectName, minio.RemoveObjectOptions{})
}

// =============================================================================
// 组合存储（支持 Redis + MinIO 回退）
// =============================================================================

// CompositeMetadataStore 组合元数据存储（优先 Redis，回退到 MinIO sidecar）
type CompositeMetadataStore struct {
	redisStore *RedisMetadataStore
	minioStore *MinIOMetadataStore
}

// NewCompositeMetadataStore 创建组合元数据存储
func NewCompositeMetadataStore(redisPool *pool.RedisPool, minioClient *minio.Client, bucket string) *CompositeMetadataStore {
	var redisStore *RedisMetadataStore
	var minioStore *MinIOMetadataStore

	if redisPool != nil {
		redisStore = NewRedisMetadataStore(redisPool)
	}
	if minioClient != nil {
		minioStore = NewMinIOMetadataStore(minioClient, bucket)
	}

	return &CompositeMetadataStore{
		redisStore: redisStore,
		minioStore: minioStore,
	}
}

func (s *CompositeMetadataStore) Store(ctx context.Context, objectName string, metadata *storage.FileMetadata) error {
	var lastErr error

	// 存储到 Redis（如果可用）
	if s.redisStore != nil {
		if err := s.redisStore.Store(ctx, objectName, metadata); err != nil {
			logger.GetLogger().Warn("Failed to store metadata to Redis",
				zap.String("object", objectName),
				zap.Error(err))
			lastErr = err
		}
	}

	// 同时存储到 MinIO sidecar（如果可用）
	if s.minioStore != nil {
		if err := s.minioStore.Store(ctx, objectName, metadata); err != nil {
			logger.GetLogger().Warn("Failed to store metadata to MinIO sidecar",
				zap.String("object", objectName),
				zap.Error(err))
			lastErr = err
		}
	}

	return lastErr
}

func (s *CompositeMetadataStore) Load(ctx context.Context, objectName string) (*storage.FileMetadata, error) {
	// 优先从 Redis 加载
	if s.redisStore != nil {
		metadata, err := s.redisStore.Load(ctx, objectName)
		if err == nil && metadata != nil && len(metadata.MinValues) > 0 {
			return metadata, nil
		}
	}

	// 回退到 MinIO sidecar
	if s.minioStore != nil {
		metadata, err := s.minioStore.Load(ctx, objectName)
		if err == nil && metadata != nil {
			// 如果 Redis 可用，缓存到 Redis
			if s.redisStore != nil && len(metadata.MinValues) > 0 {
				_ = s.redisStore.Store(ctx, objectName, metadata)
			}
			return metadata, nil
		}
	}

	// 都不可用，返回基础元数据
	return &storage.FileMetadata{
		FilePath:  objectName,
		MinValues: make(map[string]interface{}),
		MaxValues: make(map[string]interface{}),
	}, nil
}

func (s *CompositeMetadataStore) LoadBatch(ctx context.Context, objectNames []string) ([]*storage.FileMetadata, error) {
	result := make([]*storage.FileMetadata, 0, len(objectNames))

	for _, objectName := range objectNames {
		metadata, _ := s.Load(ctx, objectName)
		result = append(result, metadata)
	}

	return result, nil
}

func (s *CompositeMetadataStore) Delete(ctx context.Context, objectName string) error {
	var lastErr error

	if s.redisStore != nil {
		if err := s.redisStore.Delete(ctx, objectName); err != nil {
			lastErr = err
		}
	}

	if s.minioStore != nil {
		if err := s.minioStore.Delete(ctx, objectName); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// =============================================================================
// 辅助函数
// =============================================================================

// parseRedisFields 解析 Redis Hash 字段为 FileMetadata
func parseRedisFields(objectName string, fields map[string]string) *storage.FileMetadata {
	metadata := &storage.FileMetadata{
		FilePath:  objectName,
		MinValues: make(map[string]interface{}),
		MaxValues: make(map[string]interface{}),
	}

	if fileSizeStr, ok := fields["file_size"]; ok {
		if fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64); err == nil {
			metadata.FileSize = fileSize
		}
	}

	if rowCountStr, ok := fields["row_count"]; ok {
		if rowCount, err := strconv.ParseInt(rowCountStr, 10, 64); err == nil {
			metadata.RowCount = rowCount
		}
	}

	if rowGroupsStr, ok := fields["row_groups"]; ok {
		if rowGroups, err := strconv.Atoi(rowGroupsStr); err == nil {
			metadata.RowGroupCount = rowGroups
		}
	}

	if compression, ok := fields["compression"]; ok {
		metadata.CompressionType = compression
	}

	if createdAtStr, ok := fields["created_at"]; ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			metadata.CreatedAt = createdAt
		}
	}

	// 解析 MinValues
	if minValuesJSON, ok := fields["min_values"]; ok {
		var minValues map[string]interface{}
		if err := json.Unmarshal([]byte(minValuesJSON), &minValues); err == nil {
			metadata.MinValues = convertMetadataValues(minValues)
		}
	}

	// 解析 MaxValues
	if maxValuesJSON, ok := fields["max_values"]; ok {
		var maxValues map[string]interface{}
		if err := json.Unmarshal([]byte(maxValuesJSON), &maxValues); err == nil {
			metadata.MaxValues = convertMetadataValues(maxValues)
		}
	}

	// 解析 NullCounts
	if nullCountsJSON, ok := fields["null_counts"]; ok {
		var nullCounts map[string]int64
		if err := json.Unmarshal([]byte(nullCountsJSON), &nullCounts); err == nil {
			metadata.NullCounts = nullCounts
		}
	}

	return metadata
}

// convertMetadataValues 转换元数据值为适当的类型
func convertMetadataValues(values map[string]interface{}) map[string]interface{} {
	if values == nil {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	for key, value := range values {
		switch v := value.(type) {
		case string:
			// 尝试解析为数字
			if intVal, err := strconv.ParseInt(v, 10, 64); err == nil {
				result[key] = intVal
			} else if floatVal, err := strconv.ParseFloat(v, 64); err == nil {
				result[key] = floatVal
			} else {
				result[key] = v
			}
		case float64:
			// JSON 数字默认解析为 float64
			if v == float64(int64(v)) {
				result[key] = int64(v)
			} else {
				result[key] = v
			}
		default:
			result[key] = v
		}
	}
	return result
}

// GetMetadataStore 根据配置创建合适的元数据存储
// redisPool 可以为 nil（单节点模式）
// minioClient 和 bucket 用于 MinIO sidecar 存储
func GetMetadataStore(redisPool *pool.RedisPool, minioClient *minio.Client, bucket string) FileMetadataStore {
	// 如果 Redis 可用，使用组合存储（Redis + MinIO sidecar）
	// 如果 Redis 不可用，只使用 MinIO sidecar
	return NewCompositeMetadataStore(redisPool, minioClient, bucket)
}

// IsRedisAvailable 检查 Redis 是否可用
func IsRedisAvailable(redisPool *pool.RedisPool) bool {
	if redisPool == nil {
		return false
	}
	client := redisPool.GetClient()
	if client == nil {
		return false
	}
	// 尝试 ping
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if cmdable, ok := client.(redis.Cmdable); ok {
		return cmdable.Ping(ctx).Err() == nil
	}
	return false
}

// ExtractTableNameFromObjectName 从对象名提取表名
// 对象名格式: tableName/id/date/timestamp.parquet
func ExtractTableNameFromObjectName(objectName string) string {
	parts := strings.Split(objectName, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
