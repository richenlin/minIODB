package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/internal/pool"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// TableManager 表管理器
type TableManager struct {
	redisPool    *pool.RedisPool
	primaryMinio *minio.Client
	backupMinio  *minio.Client
	cfg          *config.Config

	// 元数据缓存（减少MinIO访问）
	metadataCache      map[string]*TableManagerInfo
	metadataCacheMutex sync.RWMutex
	metadataCacheTTL   time.Duration

	// 表级锁（用于.metadata原子创建）
	tableLocks      map[string]*sync.Mutex
	tableLocksMutex sync.RWMutex
}

// TableManagerInfo 表管理器表信息
type TableManagerInfo struct {
	Name      string              `json:"name"`
	Config    *config.TableConfig `json:"config"`
	CreatedAt string              `json:"created_at"`
	LastWrite string              `json:"last_write"`
	Status    string              `json:"status"`
}

// TableManagerStats 表管理器统计信息
type TableManagerStats struct {
	RecordCount  int64  `json:"record_count"`
	FileCount    int64  `json:"file_count"`
	SizeBytes    int64  `json:"size_bytes"`
	OldestRecord string `json:"oldest_record"`
	NewestRecord string `json:"newest_record"`
}

// NewTableManager 创建表管理器
func NewTableManager(redisPool *pool.RedisPool, primaryMinio *minio.Client, backupMinio *minio.Client, cfg *config.Config) *TableManager {
	return &TableManager{
		redisPool:        redisPool,
		primaryMinio:     primaryMinio,
		backupMinio:      backupMinio,
		cfg:              cfg,
		metadataCache:    make(map[string]*TableManagerInfo),
		metadataCacheTTL: cfg.QueryOptimization.MetadataCacheTTL, // 从配置读取TTL
		tableLocks:       make(map[string]*sync.Mutex),
	}
}

// CreateTable 创建表
func (tm *TableManager) CreateTable(ctx context.Context, tableName string, tableConfig *config.TableConfig, ifNotExists bool) error {
	// 验证表名
	if !tm.cfg.IsValidTableName(ctx, tableName) {
		return fmt.Errorf("invalid table name: %s", tableName)
	}

	// 检查表是否已存在
	exists, err := tm.TableExists(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	if exists {
		if ifNotExists {
			return nil // 表已存在但使用了IF NOT EXISTS，不报错
		}
		return fmt.Errorf("table %s already exists", tableName)
	}

	// 检查表数量限制
	totalTables, err := tm.getTotalTableCount(ctx)
	if err != nil {
		return fmt.Errorf("failed to get table count: %w", err)
	}

	if totalTables >= tm.cfg.TableManagement.MaxTables {
		return fmt.Errorf("maximum number of tables (%d) reached", tm.cfg.TableManagement.MaxTables)
	}

	// 使用默认配置填充未设置的字段
	if tableConfig == nil {
		tableConfig = &tm.cfg.Tables.DefaultConfig
	} else {
		// 合并配置
		defaultConfig := tm.cfg.Tables.DefaultConfig
		if tableConfig.BufferSize <= 0 {
			tableConfig.BufferSize = defaultConfig.BufferSize
		}
		if tableConfig.FlushInterval <= 0 {
			tableConfig.FlushInterval = defaultConfig.FlushInterval
		}
		if tableConfig.RetentionDays <= 0 {
			tableConfig.RetentionDays = defaultConfig.RetentionDays
		}
		if tableConfig.Properties == nil {
			tableConfig.Properties = make(map[string]string)
		}
	}

	// 如果Redis连接池为空，在MinIO中创建元数据标记文件
	if tm.redisPool == nil {
		logger.LogInfo(ctx, "Redis disabled, creating metadata marker in MinIO for table: %s", zap.String("table_name", tableName))
		return tm.createTableMetadataInMinIO(ctx, tableName, tableConfig)
	}

	redisClient := tm.redisPool.GetClient()

	// 创建表的Redis记录
	now := time.Now().UTC().Format(time.RFC3339)

	// 添加到表列表
	if err := redisClient.SAdd(ctx, "tables:list", tableName).Err(); err != nil {
		return fmt.Errorf("failed to add table to list: %w", err)
	}

	// 设置表配置
	configKey := fmt.Sprintf("table:%s:config", tableName)
	configData := map[string]interface{}{
		"buffer_size":    tableConfig.BufferSize,
		"flush_interval": int64(tableConfig.FlushInterval.Seconds()),
		"retention_days": tableConfig.RetentionDays,
		"backup_enabled": tableConfig.BackupEnabled,
	}

	// 添加属性
	for k, v := range tableConfig.Properties {
		configData["prop_"+k] = v
	}

	if err := redisClient.HMSet(ctx, configKey, configData).Err(); err != nil {
		return fmt.Errorf("failed to set table config: %w", err)
	}

	// 设置创建时间
	createdAtKey := fmt.Sprintf("table:%s:created_at", tableName)
	if err := redisClient.Set(ctx, createdAtKey, now, 0).Err(); err != nil {
		return fmt.Errorf("failed to set table created_at: %w", err)
	}

	// 初始化表统计
	statsKey := fmt.Sprintf("table:%s:stats", tableName)
	statsData := map[string]interface{}{
		"record_count":  0,
		"file_count":    0,
		"size_bytes":    0,
		"oldest_record": "",
		"newest_record": "",
	}
	if err := redisClient.HMSet(ctx, statsKey, statsData).Err(); err != nil {
		return fmt.Errorf("failed to initialize table stats: %w", err)
	}

	logger.LogInfo(ctx, "Created table: %s", zap.String("table_name", tableName))
	return nil
}

// DropTable 删除表
func (tm *TableManager) DropTable(ctx context.Context, tableName string, ifExists bool, cascade bool) (int32, error) {
	// 检查表是否存在
	exists, err := tm.TableExists(ctx, tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to check table existence: %w", err)
	}

	if !exists {
		if ifExists {
			return 0, nil // 表不存在但使用了IF EXISTS，不报错
		}
		return 0, fmt.Errorf("table %s does not exist", tableName)
	}

	var filesDeleted int32

	// 如果启用了cascade，删除表数据
	if cascade {
		deletedCount, err := tm.deleteTableData(ctx, tableName)
		if err != nil {
			return 0, fmt.Errorf("failed to delete table data: %w", err)
		}
		filesDeleted = int32(deletedCount)
	}

	// 如果Redis连接池为空，跳过Redis操作
	if tm.redisPool == nil {
		logger.LogInfo(ctx, "Redis disabled, skipping Redis operations for table deletion: %s", zap.String("table_name", tableName))
		return filesDeleted, nil
	}

	redisClient := tm.redisPool.GetClient()

	// 从表列表中移除
	if err := redisClient.SRem(ctx, "tables:list", tableName).Err(); err != nil {
		return filesDeleted, fmt.Errorf("failed to remove table from list: %w", err)
	}

	// 删除表配置
	configKey := fmt.Sprintf("table:%s:config", tableName)
	if err := redisClient.Del(ctx, configKey).Err(); err != nil {
		logger.LogInfo(ctx, "WARN: failed to delete table config: %v", zap.Error(err))
	}

	// 删除创建时间
	createdAtKey := fmt.Sprintf("table:%s:created_at", tableName)
	if err := redisClient.Del(ctx, createdAtKey).Err(); err != nil {
		logger.LogInfo(ctx, "WARN: failed to delete table created_at: %v", zap.Error(err))
	}

	// 删除最后写入时间
	lastWriteKey := fmt.Sprintf("table:%s:last_write", tableName)
	if err := redisClient.Del(ctx, lastWriteKey).Err(); err != nil {
		logger.LogInfo(ctx, "WARN: failed to delete table last_write: %v", zap.Error(err))
	}

	// 删除表统计
	statsKey := fmt.Sprintf("table:%s:stats", tableName)
	if err := redisClient.Del(ctx, statsKey).Err(); err != nil {
		logger.LogInfo(ctx, "WARN: failed to delete table stats: %v", zap.Error(err))
	}

	logger.LogInfo(ctx, "Dropped table: %s (files deleted: %d)", zap.String("table_name", tableName), zap.Int32("files_deleted", filesDeleted))
	return filesDeleted, nil
}

// ListTables 列出表
func (tm *TableManager) ListTables(ctx context.Context, pattern string) ([]*TableManagerInfo, error) {
	// 如果Redis连接池为空，使用MinIO发现表
	if tm.redisPool == nil {
		logger.LogInfo(ctx, "Redis disabled, discovering tables from MinIO")
		return tm.listTablesFromMinIO(ctx, pattern)
	}

	redisClient := tm.redisPool.GetClient()

	// 获取所有表名
	tableNames, err := redisClient.SMembers(ctx, "tables:list").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get table list: %w", err)
	}

	var tables []*TableManagerInfo
	for _, tableName := range tableNames {
		// 如果有模式，进行匹配
		if pattern != "" && !tm.matchPattern(tableName, pattern) {
			continue
		}

		tableInfo, err := tm.getTableInfo(ctx, tableName)
		if err != nil {
			logger.LogInfo(ctx, "WARN: failed to get info for table %s: %v", zap.String("table_name", tableName), zap.Error(err))
			continue
		}

		tables = append(tables, tableInfo)
	}

	return tables, nil
}

// getTableLock 获取表级锁（用于原子创建）
func (tm *TableManager) getTableLock(tableName string) *sync.Mutex {
	tm.tableLocksMutex.Lock()
	defer tm.tableLocksMutex.Unlock()

	if lock, exists := tm.tableLocks[tableName]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	tm.tableLocks[tableName] = lock
	return lock
}

// createTableMetadataInMinIO 在MinIO中原子创建表元数据标记文件（单节点模式）
func (tm *TableManager) createTableMetadataInMinIO(ctx context.Context, tableName string, tableConfig *config.TableConfig) error {
	if tm.primaryMinio == nil {
		return fmt.Errorf("MinIO client not initialized")
	}

	// 步骤1: 获取表级锁，保证单机原子性
	tableLock := tm.getTableLock(tableName)
	tableLock.Lock()
	defer tableLock.Unlock()

	bucket := tm.cfg.MinIO.Bucket
	metadataKey := fmt.Sprintf("%s/.metadata", tableName)

	// 步骤2: Stat检查，确保.metadata不存在
	logger.LogInfo(ctx, "Checking if .metadata exists for table",
		zap.String("table", tableName),
		zap.String("key", metadataKey))

	_, err := tm.primaryMinio.StatObject(ctx, bucket, metadataKey, minio.StatObjectOptions{})
	if err == nil {
		// 对象已存在
		logger.LogWarn(ctx, ".metadata already exists for table",
			zap.String("table", tableName))
		return fmt.Errorf("table %s already exists (metadata file present)", tableName)
	}

	// 验证错误类型 - 只有在对象不存在时才继续
	minioErr, ok := err.(minio.ErrorResponse)
	if !ok || minioErr.Code != "NoSuchKey" {
		// 其他错误（网络问题等）
		return fmt.Errorf("failed to check metadata existence: %w", err)
	}

	logger.LogInfo(ctx, ".metadata does not exist, proceeding with creation",
		zap.String("table", tableName))

	// 步骤3: 构建元数据JSON
	metadata := map[string]interface{}{
		"table_name": tableName,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"status":     "active",
		"version":    "1.0.0",
		"config": map[string]interface{}{
			"buffer_size":            tableConfig.BufferSize,
			"flush_interval_seconds": int(tableConfig.FlushInterval.Seconds()),
			"retention_days":         tableConfig.RetentionDays,
			"backup_enabled":         tableConfig.BackupEnabled,
			"properties":             tableConfig.Properties,
		},
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// 步骤4: 使用条件写上传（如果MinIO支持If-None-Match）
	// 注意：MinIO v7不直接支持If-None-Match，但通过Stat+Lock组合保证原子性
	logger.LogInfo(ctx, "Creating .metadata file",
		zap.String("table", tableName),
		zap.Int("size", len(metadataJSON)))

	_, err = tm.primaryMinio.PutObject(
		ctx,
		bucket,
		metadataKey,
		strings.NewReader(string(metadataJSON)),
		int64(len(metadataJSON)),
		minio.PutObjectOptions{
			ContentType: "application/json",
			UserMetadata: map[string]string{
				"X-Created-By":    "MinIODB",
				"X-Creation-Time": time.Now().UTC().Format(time.RFC3339),
			},
		},
	)

	if err != nil {
		logger.LogError(ctx, err, "Failed to create .metadata file",
			zap.String("table", tableName))
		return fmt.Errorf("failed to create metadata marker in MinIO: %w", err)
	}

	// 步骤5: 最终验证 - 确认.metadata已成功创建
	_, err = tm.primaryMinio.StatObject(ctx, bucket, metadataKey, minio.StatObjectOptions{})
	if err != nil {
		logger.LogError(ctx, err, "Failed to verify .metadata creation",
			zap.String("table", tableName))
		return fmt.Errorf("metadata creation verification failed: %w", err)
	}

	logger.LogInfo(ctx, "Successfully created .metadata atomically",
		zap.String("table", tableName),
		zap.String("key", metadataKey))
	return nil
}

// getCachedTableInfo 从缓存获取表信息（优化2）
func (tm *TableManager) getCachedTableInfo(ctx context.Context, tableName string) (*TableManagerInfo, bool) {
	tm.metadataCacheMutex.RLock()
	defer tm.metadataCacheMutex.RUnlock()

	info, exists := tm.metadataCache[tableName]
	return info, exists
}

// setCachedTableInfo 设置表信息缓存（优化2）
func (tm *TableManager) setCachedTableInfo(ctx context.Context, tableName string, info *TableManagerInfo) {
	tm.metadataCacheMutex.Lock()
	defer tm.metadataCacheMutex.Unlock()

	tm.metadataCache[tableName] = info
	logger.LogInfo(ctx, "Cached metadata for table %s", zap.String("table_name", tableName))
}

// InvalidateCachedTableInfo 使表缓存失效（优化2，公开方法用于REST API）
func (tm *TableManager) InvalidateCachedTableInfo(ctx context.Context, tableName string) {
	tm.metadataCacheMutex.Lock()
	defer tm.metadataCacheMutex.Unlock()

	delete(tm.metadataCache, tableName)
	logger.LogInfo(ctx, "Invalidated cache for table %s", zap.String("table_name", tableName))
}

// listTablesFromMinIO 从MinIO发现表（单节点模式）
func (tm *TableManager) listTablesFromMinIO(ctx context.Context, pattern string) ([]*TableManagerInfo, error) {
	if tm.primaryMinio == nil {
		return []*TableManagerInfo{}, nil
	}

	bucket := tm.cfg.MinIO.Bucket

	// 列出bucket中的所有前缀（每个前缀代表一个表）
	objectCh := tm.primaryMinio.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    "",
		Recursive: false,
	})

	tableMap := make(map[string]bool)
	for object := range objectCh {
		if object.Err != nil {
			logger.LogInfo(ctx, "WARN: error listing objects: %v", zap.Error(object.Err))
			continue
		}

		// 提取表名（第一级目录）
		parts := strings.Split(strings.TrimPrefix(object.Key, "/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			tableName := parts[0]
			if pattern == "" || tm.matchPattern(tableName, pattern) {
				tableMap[tableName] = true
			}
		}
	}

	// 转换为TableManagerInfo列表
	var tables []*TableManagerInfo
	for tableName := range tableMap {
		// 先检查缓存
		if cachedInfo, exists := tm.getCachedTableInfo(ctx, tableName); exists {
			tables = append(tables, cachedInfo)
			continue
		}

		// 缓存未命中，使用默认配置并缓存
		defaultConfig := tm.cfg.Tables.DefaultConfig
		info := &TableManagerInfo{
			Name:      tableName,
			Config:    &defaultConfig,
			CreatedAt: "",
			LastWrite: "",
			Status:    "active",
		}

		tm.setCachedTableInfo(ctx, tableName, info)
		tables = append(tables, info)
	}

	logger.LogInfo(ctx, "Discovered %d tables from MinIO (%d from cache)", zap.Int("tables", len(tables)), zap.Int("cache", len(tableMap)-len(tables)))
	return tables, nil
}

// DescribeTable 描述表
func (tm *TableManager) DescribeTable(ctx context.Context, tableName string) (*TableManagerInfo, *TableManagerStats, error) {
	// 检查表是否存在
	exists, err := tm.TableExists(ctx, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check table existence: %w", err)
	}

	if !exists {
		return nil, nil, fmt.Errorf("table %s does not exist", tableName)
	}

	// 获取表信息
	tableInfo, err := tm.getTableInfo(ctx, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get table info: %w", err)
	}

	// 获取表统计
	tableStats, err := tm.getTableStats(ctx, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get table stats: %w", err)
	}

	return tableInfo, tableStats, nil
}

// TableExists 检查表是否存在
func (tm *TableManager) TableExists(ctx context.Context, tableName string) (bool, error) {
	// 如果Redis连接池为空，检查MinIO中是否存在该表的数据
	if tm.redisPool == nil {
		return tm.tableExistsInMinIO(ctx, tableName)
	}

	redisClient := tm.redisPool.GetClient()
	return redisClient.SIsMember(ctx, "tables:list", tableName).Result()
}

// tableExistsInMinIO 检查MinIO中表是否存在（单节点模式）
func (tm *TableManager) tableExistsInMinIO(ctx context.Context, tableName string) (bool, error) {
	if tm.primaryMinio == nil {
		return false, nil
	}

	bucket := tm.cfg.MinIO.Bucket
	prefix := tableName + "/"

	// 列出该前缀下的对象，如果有任何对象则表存在
	objectCh := tm.primaryMinio.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		MaxKeys:   1,
		Recursive: false,
	})

	for range objectCh {
		return true, nil
	}

	return false, nil
}

// EnsureTableExists 确保表存在，如果不存在则自动创建（如果启用了自动创建）
func (tm *TableManager) EnsureTableExists(ctx context.Context, tableName string) error {
	exists, err := tm.TableExists(ctx, tableName)
	if err != nil {
		return err
	}

	if !exists {
		if !tm.cfg.TableManagement.AutoCreateTables {
			return fmt.Errorf("table %s does not exist and auto-creation is disabled", tableName)
		}

		// 自动创建表
		return tm.CreateTable(ctx, tableName, nil, true)
	}

	return nil
}

// UpdateLastWrite 更新表的最后写入时间
func (tm *TableManager) UpdateLastWrite(ctx context.Context, tableName string) error {
	// 如果Redis连接池为空，跳过更新
	if tm.redisPool == nil {
		return nil
	}

	redisClient := tm.redisPool.GetClient()
	lastWriteKey := fmt.Sprintf("table:%s:last_write", tableName)
	now := time.Now().UTC().Format(time.RFC3339)
	return redisClient.Set(ctx, lastWriteKey, now, 0).Err()
}

// getTableInfo 获取表信息
func (tm *TableManager) getTableInfo(ctx context.Context, tableName string) (*TableManagerInfo, error) {
	// 获取表配置
	tableConfig, err := tm.getTableConfigFromRedis(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// 如果Redis连接池为空，返回基本信息
	if tm.redisPool == nil {
		return &TableManagerInfo{
			Name:      tableName,
			Config:    tableConfig,
			CreatedAt: "",
			LastWrite: "",
			Status:    "active",
		}, nil
	}

	redisClient := tm.redisPool.GetClient()

	// 获取创建时间
	createdAtKey := fmt.Sprintf("table:%s:created_at", tableName)
	createdAt, err := redisClient.Get(ctx, createdAtKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get created_at: %w", err)
	}

	// 获取最后写入时间
	lastWriteKey := fmt.Sprintf("table:%s:last_write", tableName)
	lastWrite, err := redisClient.Get(ctx, lastWriteKey).Result()
	if err != nil && err != redis.Nil {
		lastWrite = ""
	}

	return &TableManagerInfo{
		Name:      tableName,
		Config:    tableConfig,
		CreatedAt: createdAt,
		LastWrite: lastWrite,
		Status:    "active",
	}, nil
}

// getTableConfigFromRedis 从Redis获取表配置
func (tm *TableManager) getTableConfigFromRedis(ctx context.Context, tableName string) (*config.TableConfig, error) {
	// 如果Redis连接池为空，返回默认配置
	if tm.redisPool == nil {
		return &tm.cfg.Tables.DefaultConfig, nil
	}

	redisClient := tm.redisPool.GetClient()
	configKey := fmt.Sprintf("table:%s:config", tableName)
	configData, err := redisClient.HGetAll(ctx, configKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get table config: %w", err)
	}

	if len(configData) == 0 {
		// 如果没有配置，返回默认配置
		return &tm.cfg.Tables.DefaultConfig, nil
	}

	tableConfig := &config.TableConfig{
		Properties: make(map[string]string),
	}

	// 解析配置
	if bufferSizeStr, ok := configData["buffer_size"]; ok {
		if bufferSize, err := strconv.Atoi(bufferSizeStr); err == nil {
			tableConfig.BufferSize = bufferSize
		}
	}

	if flushIntervalStr, ok := configData["flush_interval"]; ok {
		if flushIntervalSec, err := strconv.ParseInt(flushIntervalStr, 10, 64); err == nil {
			tableConfig.FlushInterval = time.Duration(flushIntervalSec) * time.Second
		}
	}

	if retentionDaysStr, ok := configData["retention_days"]; ok {
		if retentionDays, err := strconv.Atoi(retentionDaysStr); err == nil {
			tableConfig.RetentionDays = retentionDays
		}
	}

	if backupEnabledStr, ok := configData["backup_enabled"]; ok {
		tableConfig.BackupEnabled = backupEnabledStr == "true" || backupEnabledStr == "1"
	}

	// 解析属性
	for key, value := range configData {
		if strings.HasPrefix(key, "prop_") {
			propKey := strings.TrimPrefix(key, "prop_")
			tableConfig.Properties[propKey] = value
		}
	}

	return tableConfig, nil
}

// getTableStats 获取表统计信息
func (tm *TableManager) getTableStats(ctx context.Context, tableName string) (*TableManagerStats, error) {
	// 如果Redis连接池为空，返回空统计
	if tm.redisPool == nil {
		return &TableManagerStats{}, nil
	}

	redisClient := tm.redisPool.GetClient()
	statsKey := fmt.Sprintf("table:%s:stats", tableName)
	statsData, err := redisClient.HGetAll(ctx, statsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get table stats: %w", err)
	}

	stats := &TableManagerStats{}

	if recordCountStr, ok := statsData["record_count"]; ok {
		if recordCount, err := strconv.ParseInt(recordCountStr, 10, 64); err == nil {
			stats.RecordCount = recordCount
		}
	}

	if fileCountStr, ok := statsData["file_count"]; ok {
		if fileCount, err := strconv.ParseInt(fileCountStr, 10, 64); err == nil {
			stats.FileCount = fileCount
		}
	}

	if sizeBytesStr, ok := statsData["size_bytes"]; ok {
		if sizeBytes, err := strconv.ParseInt(sizeBytesStr, 10, 64); err == nil {
			stats.SizeBytes = sizeBytes
		}
	}

	if oldestRecord, ok := statsData["oldest_record"]; ok {
		stats.OldestRecord = oldestRecord
	}

	if newestRecord, ok := statsData["newest_record"]; ok {
		stats.NewestRecord = newestRecord
	}

	return stats, nil
}

// deleteTableData 删除表的所有数据
func (tm *TableManager) deleteTableData(ctx context.Context, tableName string) (int, error) {
	// 如果Redis连接池为空，跳过Redis操作
	if tm.redisPool == nil {
		return 0, nil
	}

	redisClient := tm.redisPool.GetClient()

	// 获取表的所有索引键
	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	keys, err := redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get table index keys: %w", err)
	}

	var totalDeleted int

	// 删除每个索引对应的文件
	for _, key := range keys {
		files, err := redisClient.SMembers(ctx, key).Result()
		if err != nil {
			logger.LogInfo(ctx, "WARN: failed to get files for key %s: %v", zap.String("key", key), zap.Error(err))
			continue
		}

		// 从MinIO删除文件
		for _, file := range files {
			if err := tm.primaryMinio.RemoveObject(ctx, tm.cfg.MinIO.Bucket, file, minio.RemoveObjectOptions{}); err != nil {
				logger.LogInfo(ctx, "WARN: failed to delete file %s from primary MinIO: %v", zap.String("file", file), zap.Error(err))
			} else {
				totalDeleted++
			}

			// 从备份MinIO删除文件（如果存在）
			if tm.backupMinio != nil && tm.cfg.Backup.Enabled {
				if err := tm.backupMinio.RemoveObject(ctx, tm.cfg.Backup.MinIO.Bucket, file, minio.RemoveObjectOptions{}); err != nil {
					logger.LogInfo(ctx, "WARN: failed to delete file %s from backup MinIO: %v", zap.String("file", file), zap.Error(err))
				}
			}
		}

		// 删除索引键
		if err := redisClient.Del(ctx, key).Err(); err != nil {
			logger.LogInfo(ctx, "WARN: failed to delete index key %s: %v", zap.String("key", key), zap.Error(err))
		}
	}

	return totalDeleted, nil
}

// getTotalTableCount 获取表总数
func (tm *TableManager) getTotalTableCount(ctx context.Context) (int, error) {
	// 如果Redis连接池为空，返回0
	if tm.redisPool == nil {
		return 0, nil
	}

	redisClient := tm.redisPool.GetClient()
	count, err := redisClient.SCard(ctx, "tables:list").Result()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// matchPattern 简单的模式匹配（支持*通配符）
func (tm *TableManager) matchPattern(str, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}

	// 简单实现：只支持前缀和后缀匹配
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(str, prefix)
	}

	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(str, suffix)
	}

	// 精确匹配
	return str == pattern
}
