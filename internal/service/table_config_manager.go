package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/pool"

	"github.com/go-redis/redis/v8"
)

// TableConfigManager 表配置管理器
// 负责管理表配置的缓存、热更新和持久化
type TableConfigManager struct {
	redisPool      *pool.RedisPool
	defaultConfig  config.TableConfig
	configCache    map[string]*config.TableConfig
	cacheMutex     sync.RWMutex
	cacheTTL       time.Duration
	lastUpdateTime map[string]time.Time
}

// NewTableConfigManager 创建表配置管理器
func NewTableConfigManager(redisPool *pool.RedisPool, defaultConfig config.TableConfig) *TableConfigManager {
	return &TableConfigManager{
		redisPool:      redisPool,
		defaultConfig:  defaultConfig,
		configCache:    make(map[string]*config.TableConfig),
		cacheTTL:       5 * time.Minute, // 本地缓存TTL
		lastUpdateTime: make(map[string]time.Time),
	}
}

// GetTableConfig 获取表配置（带缓存）
func (m *TableConfigManager) GetTableConfig(ctx context.Context, tableName string) config.TableConfig {
	// 1. 检查本地缓存
	m.cacheMutex.RLock()
	if cachedConfig, exists := m.configCache[tableName]; exists {
		// 检查缓存是否过期
		if lastUpdate, ok := m.lastUpdateTime[tableName]; ok {
			if time.Since(lastUpdate) < m.cacheTTL {
				m.cacheMutex.RUnlock()
				return *cachedConfig
			}
		}
	}
	m.cacheMutex.RUnlock()

	// 2. 从Redis读取
	redisConfig, err := m.loadFromRedis(ctx, tableName)
	if err == nil && redisConfig != nil {
		// 更新本地缓存
		m.updateLocalCache(tableName, redisConfig)
		return *redisConfig
	}

	// 3. 返回默认配置
	return m.defaultConfig
}

// SetTableConfig 设置表配置（持久化到Redis）
func (m *TableConfigManager) SetTableConfig(ctx context.Context, tableName string, config config.TableConfig) error {
	// 1. 保存到Redis
	if err := m.saveToRedis(ctx, tableName, &config); err != nil {
		return fmt.Errorf("failed to save config to redis: %w", err)
	}

	// 2. 更新本地缓存
	m.updateLocalCache(tableName, &config)

	return nil
}

// loadFromRedis 从Redis加载配置
func (m *TableConfigManager) loadFromRedis(ctx context.Context, tableName string) (*config.TableConfig, error) {
	if m.redisPool == nil {
		return nil, fmt.Errorf("redis pool not available")
	}

	redisClient := m.redisPool.GetClient()
	key := fmt.Sprintf("table:%s:config", tableName)

	// 从Redis读取配置
	configMap, err := redisClient.HGetAll(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 配置不存在
		}
		return nil, err
	}

	if len(configMap) == 0 {
		return nil, nil
	}

	// 解析配置
	tableConfig := m.defaultConfig // 从默认配置开始

	// 解析基本字段
	if val, ok := configMap["id_strategy"]; ok {
		tableConfig.IDStrategy = val
	}
	if val, ok := configMap["id_prefix"]; ok {
		tableConfig.IDPrefix = val
	}
	if val, ok := configMap["auto_generate_id"]; ok {
		tableConfig.AutoGenerateID = val == "true"
	}
	if val, ok := configMap["id_validation_max_length"]; ok {
		if maxLen, err := parseInt(val); err == nil && maxLen > 0 {
			tableConfig.IDValidation.MaxLength = maxLen
		}
	}
	if val, ok := configMap["id_validation_pattern"]; ok {
		tableConfig.IDValidation.Pattern = val
	}

	return &tableConfig, nil
}

// saveToRedis 保存配置到Redis
func (m *TableConfigManager) saveToRedis(ctx context.Context, tableName string, tableConfig *config.TableConfig) error {
	if m.redisPool == nil {
		return fmt.Errorf("redis pool not available")
	}

	redisClient := m.redisPool.GetClient()
	key := fmt.Sprintf("table:%s:config", tableName)

	// 构建配置映射
	configMap := map[string]interface{}{
		"id_strategy":                 tableConfig.IDStrategy,
		"id_prefix":                   tableConfig.IDPrefix,
		"auto_generate_id":            fmt.Sprintf("%t", tableConfig.AutoGenerateID),
		"id_validation_max_length":    fmt.Sprintf("%d", tableConfig.IDValidation.MaxLength),
		"id_validation_pattern":       tableConfig.IDValidation.Pattern,
		"id_validation_allowed_chars": tableConfig.IDValidation.AllowedChars,
	}

	// 使用HSET保存配置
	return redisClient.HMSet(ctx, key, configMap).Err()
}

// updateLocalCache 更新本地缓存
func (m *TableConfigManager) updateLocalCache(tableName string, tableConfig *config.TableConfig) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	// 复制配置以避免并发问题
	configCopy := *tableConfig
	m.configCache[tableName] = &configCopy
	m.lastUpdateTime[tableName] = time.Now()
}

// InvalidateCache 使缓存失效
func (m *TableConfigManager) InvalidateCache(tableName string) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	delete(m.configCache, tableName)
	delete(m.lastUpdateTime, tableName)
}

// ClearCache 清空所有缓存
func (m *TableConfigManager) ClearCache() {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	m.configCache = make(map[string]*config.TableConfig)
	m.lastUpdateTime = make(map[string]time.Time)
}

// GetCachedTableNames 获取所有已缓存的表名
func (m *TableConfigManager) GetCachedTableNames() []string {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	names := make([]string, 0, len(m.configCache))
	for name := range m.configCache {
		names = append(names, name)
	}
	return names
}

// parseInt 解析整数（辅助函数）
func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}
