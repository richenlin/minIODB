package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"minIODB/config"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

const (
	// minioConfigPrefix 是表配置在 MinIO bucket 中的存储前缀。
	// 以 _system/ 开头使其与业务数据隔离。
	minioConfigPrefix = "_system/table_configs/"
)

// minioTableConfigEntry 是存储在 MinIO 上的表配置文件格式。
type minioTableConfigEntry struct {
	TableName string             `json:"table_name"`
	Config    config.TableConfig `json:"config"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// MinioConfigStore 在 MinIO 上持久化表配置，用于 standalone（无 Redis）模式。
// 对象路径：<bucket>/_system/table_configs/<tableName>.json
type MinioConfigStore struct {
	client *minio.Client
	bucket string
	logger *zap.Logger
}

// NewMinioConfigStore 创建 MinIO 配置存储实例。
// client 或 bucket 为空时返回 nil，上层按 nil 判断是否可用。
func NewMinioConfigStore(client *minio.Client, bucket string, logger *zap.Logger) *MinioConfigStore {
	if client == nil || bucket == "" {
		return nil
	}
	return &MinioConfigStore{
		client: client,
		bucket: bucket,
		logger: logger,
	}
}

// objectKey 返回表配置的 MinIO 对象名。
func (s *MinioConfigStore) objectKey(tableName string) string {
	return minioConfigPrefix + tableName + ".json"
}

// Save 将表配置持久化到 MinIO。
func (s *MinioConfigStore) Save(ctx context.Context, tableName string, cfg config.TableConfig) error {
	now := time.Now().UTC()

	// 尝试读取已有条目以保留 CreatedAt
	var createdAt time.Time
	if existing, err := s.load(ctx, tableName); err == nil && existing != nil {
		createdAt = existing.CreatedAt
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	entry := minioTableConfigEntry{
		TableName: tableName,
		Config:    cfg,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("minio config store: marshal failed: %w", err)
	}

	key := s.objectKey(tableName)
	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return fmt.Errorf("minio config store: put object %q: %w", key, err)
	}

	s.logger.Debug("Saved table config to MinIO", zap.String("table", tableName), zap.String("key", key))
	return nil
}

// Get 从 MinIO 读取表配置。未找到时返回 (nil, nil)。
func (s *MinioConfigStore) Get(ctx context.Context, tableName string) (*config.TableConfig, error) {
	entry, err := s.load(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	return &entry.Config, nil
}

// Delete 删除 MinIO 上的表配置。
func (s *MinioConfigStore) Delete(ctx context.Context, tableName string) error {
	key := s.objectKey(tableName)
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio config store: remove object %q: %w", key, err)
	}
	return nil
}

// ListTableNames 列出 MinIO 上所有已持久化配置的表名。
func (s *MinioConfigStore) ListTableNames(ctx context.Context) ([]string, error) {
	objectCh := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    minioConfigPrefix,
		Recursive: false,
	})

	var names []string
	for obj := range objectCh {
		if obj.Err != nil {
			return nil, fmt.Errorf("minio config store: list objects: %w", obj.Err)
		}
		// 从对象名中提取表名：去掉前缀和 .json 后缀
		name := strings.TrimPrefix(obj.Key, minioConfigPrefix)
		name = strings.TrimSuffix(name, ".json")
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// load 是内部读取方法，返回完整的 entry 以便保留时间戳。
func (s *MinioConfigStore) load(ctx context.Context, tableName string) (*minioTableConfigEntry, error) {
	key := s.objectKey(tableName)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio config store: get object %q: %w", key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		// MinIO 找不到对象时返回 NoSuchKey 错误
		if isMinioNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("minio config store: read object %q: %w", key, err)
	}

	var entry minioTableConfigEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("minio config store: unmarshal %q: %w", key, err)
	}
	return &entry, nil
}

// isMinioNotFound 判断错误是否代表对象不存在。
func isMinioNotFound(err error) bool {
	if err == nil {
		return false
	}
	errResp := minio.ToErrorResponse(err)
	return errResp.Code == "NoSuchKey" || errResp.StatusCode == 404
}
