package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"minIODB/internal/config"

	"github.com/go-redis/redis/v8"
)

// TableConfigMetadata 表配置元数据
type TableConfigMetadata struct {
	TableName string             `json:"table_name"`
	Config    config.TableConfig `json:"config"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

// SaveTableConfig 保存表配置到元数据
func (m *Manager) SaveTableConfig(ctx context.Context, tableName string, tableConfig config.TableConfig) error {
	client, err := m.getRedisClient()
	if err != nil {
		return fmt.Errorf("failed to get redis client: %w", err)
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
		log.Printf("WARN: Failed to add table to config list: %v", err)
	}

	m.logger.Printf("Saved table config for table: %s", tableName)
	return nil
}

// GetTableConfig 从元数据获取表配置
func (m *Manager) GetTableConfig(ctx context.Context, tableName string) (*config.TableConfig, error) {
	client, err := m.getRedisClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get redis client: %w", err)
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

// ListTableConfigs 列出所有表配置
func (m *Manager) ListTableConfigs(ctx context.Context) ([]string, error) {
	client, err := m.getRedisClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get redis client: %w", err)
	}

	tables, err := client.SMembers(ctx, "metadata:table_configs").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list table configs: %w", err)
	}

	return tables, nil
}

// DeleteTableConfig 删除表配置
func (m *Manager) DeleteTableConfig(ctx context.Context, tableName string) error {
	client, err := m.getRedisClient()
	if err != nil {
		return fmt.Errorf("failed to get redis client: %w", err)
	}

	key := fmt.Sprintf("metadata:table_config:%s", tableName)

	// 删除配置
	if err := client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete table config: %w", err)
	}

	// 从列表中移除
	if err := client.SRem(ctx, "metadata:table_configs", tableName).Err(); err != nil {
		log.Printf("WARN: Failed to remove table from config list: %v", err)
	}

	m.logger.Printf("Deleted table config for table: %s", tableName)
	return nil
}

// GetTableConfigMetadata 获取表配置的完整元数据（包含时间戳等）
func (m *Manager) GetTableConfigMetadata(ctx context.Context, tableName string) (*TableConfigMetadata, error) {
	client, err := m.getRedisClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get redis client: %w", err)
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
