package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"minIODB/config"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// minioTableConfigPrefix 是表配置在 MinIO bucket 中的存储前缀（standalone 降级路径）。
const minioTableConfigPrefix = "_system/table_configs/"

// TableConfigMetadata 表配置元数据
type TableConfigMetadata struct {
	TableName string             `json:"table_name"`
	Config    config.TableConfig `json:"config"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

// SaveTableConfig 保存表配置到元数据。
// 优先写 Redis；Redis 不可用时降级写 MinIO。
func (m *Manager) SaveTableConfig(ctx context.Context, tableName string, tableConfig config.TableConfig) error {
	client, redisErr := m.getRedisClient()
	if redisErr != nil {
		// Redis 不可用：降级到 MinIO
		return m.saveTableConfigToMinio(ctx, tableName, tableConfig)
	}

	// 构建元数据
	now := time.Now().UTC().Format(time.RFC3339)
	metadata := TableConfigMetadata{
		TableName: tableName,
		Config:    tableConfig,
		UpdatedAt: now,
	}

	// 检查是否已存在
	key := fmt.Sprintf("metadata:table_config:%s", tableName)
	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to check if table config exists: %w", err)
	}

	if exists == 0 {
		metadata.CreatedAt = now
	} else {
		// 保留原有的创建时间
		oldData, err := client.Get(ctx, key).Result()
		if err == nil {
			var oldMetadata TableConfigMetadata
			if err := json.Unmarshal([]byte(oldData), &oldMetadata); err == nil {
				metadata.CreatedAt = oldMetadata.CreatedAt
			}
		}
	}

	// 序列化
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal table config: %w", err)
	}

	// 保存到Redis
	if err := client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save table config: %w", err)
	}

	// 添加到表列表
	if err := client.SAdd(ctx, "metadata:table_configs", tableName).Err(); err != nil {
		m.logger.Sugar().Infof("WARN: Failed to add table to config list: %v", err)
	}

	m.logger.Info("Saved table config for table", zap.String("table_name", tableName))
	return nil
}

// GetTableConfig 从元数据获取表配置。
// 优先读 Redis；Redis 不可用时降级读 MinIO。
func (m *Manager) GetTableConfig(ctx context.Context, tableName string) (*config.TableConfig, error) {
	client, redisErr := m.getRedisClient()
	if redisErr != nil {
		// Redis 不可用：降级到 MinIO
		return m.getTableConfigFromMinio(ctx, tableName)
	}

	key := fmt.Sprintf("metadata:table_config:%s", tableName)
	data, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // 不存在
	} else if err != nil {
		return nil, fmt.Errorf("failed to get table config: %w", err)
	}

	var metadata TableConfigMetadata
	if err := json.Unmarshal([]byte(data), &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal table config: %w", err)
	}

	return &metadata.Config, nil
}

// ListTableConfigs 列出所有表配置。
// 优先读 Redis；Redis 不可用时降级读 MinIO。
func (m *Manager) ListTableConfigs(ctx context.Context) ([]string, error) {
	client, redisErr := m.getRedisClient()
	if redisErr != nil {
		return m.listTableConfigsFromMinio(ctx)
	}

	tables, err := client.SMembers(ctx, "metadata:table_configs").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list table configs: %w", err)
	}

	return tables, nil
}

// DeleteTableConfig 删除表配置。
// 优先删 Redis；Redis 不可用时降级删 MinIO。
func (m *Manager) DeleteTableConfig(ctx context.Context, tableName string) error {
	client, redisErr := m.getRedisClient()
	if redisErr != nil {
		return m.deleteTableConfigFromMinio(ctx, tableName)
	}

	key := fmt.Sprintf("metadata:table_config:%s", tableName)

	// 删除配置
	if err := client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete table config: %w", err)
	}

	// 从列表中移除
	if err := client.SRem(ctx, "metadata:table_configs", tableName).Err(); err != nil {
		m.logger.Sugar().Infof("WARN: Failed to remove table from config list: %v", err)
	}

	m.logger.Info("Deleted table config for table", zap.String("table_name", tableName))
	return nil
}

// GetTableConfigMetadata 获取表配置的完整元数据（包含时间戳等）。
// 优先读 Redis；Redis 不可用时降级读 MinIO。
func (m *Manager) GetTableConfigMetadata(ctx context.Context, tableName string) (*TableConfigMetadata, error) {
	client, redisErr := m.getRedisClient()
	if redisErr != nil {
		// 降级到 MinIO
		entry, err := m.loadTableConfigEntryFromMinio(ctx, tableName)
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	key := fmt.Sprintf("metadata:table_config:%s", tableName)
	data, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get table config metadata: %w", err)
	}

	var metadata TableConfigMetadata
	if err := json.Unmarshal([]byte(data), &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal table config metadata: %w", err)
	}

	return &metadata, nil
}

// ── MinIO 降级方法（standalone 模式，无 Redis） ──────────────────────────────

// minioTableConfigBucket 返回用于存储表配置的 bucket 名。
func (m *Manager) minioTableConfigBucket() string {
	if m.config != nil && m.config.Backup.Bucket != "" {
		return m.config.Backup.Bucket
	}
	return ""
}

// minioTableConfigKey 返回表配置在 MinIO 上的对象名。
func minioTableConfigKey(tableName string) string {
	return minioTableConfigPrefix + tableName + ".json"
}

// saveTableConfigToMinio 将表配置持久化到 MinIO。
func (m *Manager) saveTableConfigToMinio(ctx context.Context, tableName string, tableConfig config.TableConfig) error {
	if m.storage == nil {
		return fmt.Errorf("metadata: no storage available for MinIO fallback")
	}
	bucket := m.minioTableConfigBucket()
	if bucket == "" {
		return fmt.Errorf("metadata: MinIO bucket not configured for table config fallback")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := &TableConfigMetadata{
		TableName: tableName,
		Config:    tableConfig,
		UpdatedAt: now,
		CreatedAt: now,
	}

	// 尝试保留已有的 CreatedAt
	if existing, err := m.loadTableConfigEntryFromMinio(ctx, tableName); err == nil && existing != nil {
		entry.CreatedAt = existing.CreatedAt
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("metadata: marshal table config: %w", err)
	}

	key := minioTableConfigKey(tableName)
	if err := m.storage.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("metadata: put table config to MinIO %q: %w", key, err)
	}

	m.logger.Info("Saved table config to MinIO (standalone fallback)", zap.String("table_name", tableName), zap.String("key", key))
	return nil
}

// getTableConfigFromMinio 从 MinIO 读取表配置。
func (m *Manager) getTableConfigFromMinio(ctx context.Context, tableName string) (*config.TableConfig, error) {
	entry, err := m.loadTableConfigEntryFromMinio(ctx, tableName)
	if err != nil || entry == nil {
		return nil, err
	}
	return &entry.Config, nil
}

// deleteTableConfigFromMinio 从 MinIO 删除表配置。
func (m *Manager) deleteTableConfigFromMinio(ctx context.Context, tableName string) error {
	if m.storage == nil {
		return fmt.Errorf("metadata: no storage available for MinIO fallback")
	}
	bucket := m.minioTableConfigBucket()
	if bucket == "" {
		return fmt.Errorf("metadata: MinIO bucket not configured")
	}
	key := minioTableConfigKey(tableName)
	if err := m.storage.DeleteObject(ctx, bucket, key); err != nil {
		return fmt.Errorf("metadata: delete table config from MinIO %q: %w", key, err)
	}
	m.logger.Info("Deleted table config from MinIO (standalone fallback)", zap.String("table_name", tableName))
	return nil
}

// listTableConfigsFromMinio 从 MinIO 列出所有已持久化配置的表名。
func (m *Manager) listTableConfigsFromMinio(ctx context.Context) ([]string, error) {
	if m.storage == nil {
		return nil, nil
	}
	bucket := m.minioTableConfigBucket()
	if bucket == "" {
		return nil, nil
	}

	objects, err := m.storage.ListObjectsSimple(ctx, bucket, minioTableConfigPrefix)
	if err != nil {
		return nil, fmt.Errorf("metadata: list table configs from MinIO: %w", err)
	}

	var names []string
	for _, obj := range objects {
		name := strings.TrimPrefix(obj.Name, minioTableConfigPrefix)
		name = strings.TrimSuffix(name, ".json")
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// loadTableConfigEntryFromMinio 读取 MinIO 上的完整表配置条目（含时间戳）。
func (m *Manager) loadTableConfigEntryFromMinio(ctx context.Context, tableName string) (*TableConfigMetadata, error) {
	if m.storage == nil {
		return nil, nil
	}
	bucket := m.minioTableConfigBucket()
	if bucket == "" {
		return nil, nil
	}

	key := minioTableConfigKey(tableName)
	data, err := m.storage.GetObjectBytes(ctx, bucket, key)
	if err != nil {
		// 对象不存在时不视为错误
		if isMinioNotFoundErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("metadata: get table config from MinIO %q: %w", key, err)
	}

	var entry TableConfigMetadata
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("metadata: unmarshal table config from MinIO %q: %w", key, err)
	}
	return &entry, nil
}

// isMinioNotFoundErr 判断是否为 MinIO "对象不存在" 错误。
func isMinioNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "The specified key does not exist") || strings.Contains(msg, "404")
}
