package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
)

// MetadataType 元数据类型
type MetadataType string

const (
	MetadataTypeServiceRegistry MetadataType = "service_registry" // 服务注册信息
	MetadataTypeDataIndex       MetadataType = "data_index"       // 数据索引
	MetadataTypeTableSchema     MetadataType = "table_schema"     // 表结构
	MetadataTypeClusterInfo     MetadataType = "cluster_info"     // 集群信息
)

// MetadataEntry 元数据条目
type MetadataEntry struct {
	Type      MetadataType  `json:"type"`
	Key       string        `json:"key"`
	Value     interface{}   `json:"value"`
	Timestamp time.Time     `json:"timestamp"`
	TTL       time.Duration `json:"ttl,omitempty"`
}

// BackupSnapshot 备份快照
type BackupSnapshot struct {
	NodeID    string           `json:"node_id"`
	Timestamp time.Time        `json:"timestamp"`
	Version   string           `json:"version"`
	Entries   []*MetadataEntry `json:"entries"`
	Checksum  string           `json:"checksum"` // SHA-256校验和
	Size      int64            `json:"size"`     // 备份大小（字节）
}

// BackupManager 备份管理器
type BackupManager struct {
	// 新的分离存储接口
	cacheStorage  storage.CacheStorage
	objectStorage storage.ObjectStorage

	// 向后兼容的统一存储接口
	storage storage.Storage

	nodeID string
	bucket string

	// 配置
	config BackupConfig

	// 状态管理
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mutex    sync.RWMutex
	logger   *log.Logger
	isActive bool

	// 统计信息
	stats BackupStats
}

// BackupConfig 备份配置
type BackupConfig struct {
	Bucket            string        `yaml:"bucket"`             // 存储桶名称
	Interval          time.Duration `yaml:"interval"`           // 备份间隔
	RetentionDays     int           `yaml:"retention_days"`     // 保留天数
	CompressionLevel  int           `yaml:"compression_level"`  // 压缩级别
	EncryptionEnabled bool          `yaml:"encryption_enabled"` // 是否启用加密
	MaxBackupSize     int64         `yaml:"max_backup_size"`    // 最大备份大小
	CleanupInterval   time.Duration `yaml:"cleanup_interval"`   // 清理间隔
}

// BackupStats 备份统计信息
type BackupStats struct {
	TotalBackups      int           `json:"total_backups"`       // 总备份数
	LastBackupTime    time.Time     `json:"last_backup_time"`    // 最后备份时间
	LastBackupSize    int64         `json:"last_backup_size"`    // 最后备份大小
	TotalBackupSize   int64         `json:"total_backup_size"`   // 总备份大小
	SuccessfulBackups int           `json:"successful_backups"`  // 成功备份数
	FailedBackups     int           `json:"failed_backups"`      // 失败备份数
	AverageBackupTime time.Duration `json:"average_backup_time"` // 平均备份时间
}

// NewBackupManager 创建备份管理器（向后兼容）
func NewBackupManager(ctx context.Context, storage storage.Storage, nodeID string, config BackupConfig) *BackupManager {
	return NewBackupManagerWithStorages(ctx, storage, nil, nil, nodeID, config)
}

// NewBackupManagerWithStorages 使用分离的存储接口创建备份管理器
func NewBackupManagerWithStorages(ctx context.Context, unifiedStorage storage.Storage, cacheStorage storage.CacheStorage, objectStorage storage.ObjectStorage, nodeID string, config BackupConfig) *BackupManager {
	ctx, cancel := context.WithCancel(ctx)

	manager := &BackupManager{
		storage:       unifiedStorage,
		cacheStorage:  cacheStorage,
		objectStorage: objectStorage,
		nodeID:        nodeID,
		bucket:        config.Bucket,
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		logger:        log.New(log.Writer(), "[BACKUP] ", log.LstdFlags),
		isActive:      false,
		stats:         BackupStats{},
	}

	// 如果没有提供分离的存储接口，则从统一存储接口获取
	if manager.cacheStorage == nil && unifiedStorage != nil {
		manager.cacheStorage = unifiedStorage
	}
	if manager.objectStorage == nil && unifiedStorage != nil {
		manager.objectStorage = unifiedStorage
	}

	return manager
}

// Start 启动备份管理器
func (bm *BackupManager) Start(ctx context.Context) error {
	bm.logger.Printf("Starting metadata backup manager, interval: %v", bm.config.Interval)
	bm.logger.Printf("Backup configuration: bucket=%s, storage=%v, objectStorage=%v, cacheStorage=%v",
		bm.bucket, bm.storage != nil, bm.objectStorage != nil, bm.cacheStorage != nil)

	// 立即执行一次备份
	if err := bm.performBackup(ctx); err != nil {
		bm.logger.Printf("Initial backup failed: %v", err)
	}

	// 启动定期备份
	bm.wg.Add(1)
	go bm.backupLoop(ctx)

	// 启动清理任务
	bm.wg.Add(1)
	go bm.cleanupLoop(ctx)

	return nil
}

// Stop 停止备份管理器
func (bm *BackupManager) Stop(ctx context.Context) error {
	bm.logger.Println("Stopping metadata backup manager")

	bm.cancel()
	bm.wg.Wait()

	bm.logger.Println("Metadata backup manager stopped")
	return nil
}

// backupLoop 备份循环
func (bm *BackupManager) backupLoop(ctx context.Context) {
	defer bm.wg.Done()

	ticker := time.NewTicker(bm.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case <-ticker.C:
			if err := bm.performBackup(ctx); err != nil {
				bm.incrementErrorCount()
				bm.logger.Printf("Backup failed: %v", err)
			}
		}
	}
}

// performBackup 执行备份
func (bm *BackupManager) performBackup(ctx context.Context) error {
	startTime := time.Now()
	bm.logger.Printf("Starting metadata backup")
	bm.logger.Printf("Backup target bucket: %s", bm.bucket)

	// 收集元数据
	entries, err := bm.collectMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect metadata: %w", err)
	}

	// 获取当前版本号并递增
	currentVersion, err := bm.getCurrentAndIncrementVersion(ctx)
	if err != nil {
		bm.logger.Printf("Failed to get/increment version, using default: %v", err)
		currentVersion = "1.0.0"
	}

	// 创建备份快照（先不设置Checksum）
	snapshot := &BackupSnapshot{
		NodeID:    bm.nodeID,
		Timestamp: startTime,
		Version:   currentVersion,
		Entries:   entries,
	}

	// 序列化快照（不含checksum）
	dataWithoutChecksum, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// 计算SHA-256校验和
	hash := sha256.Sum256(dataWithoutChecksum)
	checksum := hex.EncodeToString(hash[:])

	// 添加校验和和大小到快照
	snapshot.Checksum = checksum
	snapshot.Size = int64(len(dataWithoutChecksum))

	// 重新序列化完整的快照
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal final snapshot: %w", err)
	}

	// 上传到MinIO
	objectName := fmt.Sprintf("metadata-backup/%s/%s/backup_%s.json", bm.nodeID, startTime.Format("2006/01/02"), startTime.Format("20060102_150405"))

	reader := bytes.NewReader(data)

	// 调试日志
	bm.logger.Printf("Attempting to upload backup to bucket: %s, object: %s", bm.bucket, objectName)

	// 使用新的存储接口
	var uploadErr error
	if bm.objectStorage != nil {
		bm.logger.Printf("Using object storage interface for backup upload")
		uploadErr = bm.objectStorage.PutObject(ctx, bm.bucket, objectName, reader, int64(len(data)))
	} else if bm.storage != nil {
		bm.logger.Printf("Using unified storage interface for backup upload")
		uploadErr = bm.storage.PutObject(ctx, bm.bucket, objectName, reader, int64(len(data)))
	} else {
		return fmt.Errorf("no object storage available")
	}

	if uploadErr != nil {
		bm.logger.Printf("Backup upload failed to bucket %s: %v", bm.bucket, uploadErr)
		return fmt.Errorf("failed to upload backup: %w", uploadErr)
	}

	// 更新统计信息
	bm.updateStats(startTime)

	duration := time.Since(startTime)
	bm.logger.Printf("Backup completed successfully, entries: %d, version: %s, duration: %v, object: %s",
		len(entries), currentVersion, duration, objectName)

	return nil
}

// getCurrentAndIncrementVersion 获取当前版本号并递增
func (bm *BackupManager) getCurrentAndIncrementVersion(ctx context.Context) (string, error) {
	var redisClient redis.Cmdable

	// 获取Redis客户端
	if bm.cacheStorage != nil {
		client := bm.cacheStorage.GetClient()
		if client == nil {
			return "", fmt.Errorf("redis client not available from cache storage")
		}
		var ok bool
		redisClient, ok = client.(redis.Cmdable)
		if !ok {
			return "", fmt.Errorf("invalid redis client type from cache storage")
		}
	} else if bm.storage != nil {
		poolManager := bm.storage.GetPoolManager()
		if poolManager == nil {
			return "", fmt.Errorf("pool manager not available")
		}
		redisPool := poolManager.GetRedisPool()
		if redisPool == nil {
			return "", fmt.Errorf("redis pool not available")
		}
		client := redisPool.GetClient()
		if client == nil {
			return "", fmt.Errorf("redis client not available")
		}
		var ok bool
		redisClient, ok = client.(redis.Cmdable)
		if !ok {
			return "", fmt.Errorf("invalid redis client type")
		}
	} else {
		return "", fmt.Errorf("no cache storage available")
	}

	// 获取当前版本号
	currentVersion, err := redisClient.Get(ctx, "metadata:version").Result()
	if err == redis.Nil {
		// 如果版本不存在，设置默认版本
		currentVersion = "1.0.0"
	} else if err != nil {
		return "", fmt.Errorf("failed to get version from redis: %w", err)
	}

	// 解析版本号并递增
	newVersion := bm.incrementVersion(currentVersion)

	// 更新Redis中的版本号
	if err := redisClient.Set(ctx, "metadata:version", newVersion, 0).Err(); err != nil {
		return "", fmt.Errorf("failed to update version in redis: %w", err)
	}

	return newVersion, nil
}

// incrementVersion 递增版本号
func (bm *BackupManager) incrementVersion(version string) string {
	parts := strings.Split(version, ".")

	// 确保有三个部分
	for len(parts) < 3 {
		parts = append(parts, "0")
	}

	// 递增补丁版本号
	if patch, err := strconv.Atoi(parts[2]); err == nil {
		parts[2] = strconv.Itoa(patch + 1)
	} else {
		parts[2] = "1"
	}

	return strings.Join(parts, ".")
}

// collectMetadata 收集元数据（公共方法）
func (bm *BackupManager) CollectMetadata(ctx context.Context) ([]*MetadataEntry, error) {
	return bm.collectMetadata(ctx)
}

// collectMetadata 收集元数据
func (bm *BackupManager) collectMetadata(ctx context.Context) ([]*MetadataEntry, error) {
	var redisClient redis.Cmdable

	// 获取Redis客户端
	if bm.cacheStorage != nil {
		// 从缓存存储获取客户端
		client := bm.cacheStorage.GetClient()
		if client == nil {
			return nil, fmt.Errorf("redis client not available from cache storage")
		}

		var ok bool
		redisClient, ok = client.(redis.Cmdable)
		if !ok {
			return nil, fmt.Errorf("invalid redis client type from cache storage")
		}
	} else if bm.storage != nil {
		// 通过 Storage 的 PoolManager 获取 Redis 客户端
		poolManager := bm.storage.GetPoolManager()
		if poolManager == nil {
			return nil, fmt.Errorf("pool manager not available")
		}

		redisPool := poolManager.GetRedisPool()
		if redisPool == nil {
			return nil, fmt.Errorf("redis pool not available")
		}

		client := redisPool.GetClient()
		if client == nil {
			return nil, fmt.Errorf("redis client not available")
		}

		// 类型断言为redis.Cmdable
		var ok bool
		redisClient, ok = client.(redis.Cmdable)
		if !ok {
			return nil, fmt.Errorf("invalid redis client type")
		}
	} else {
		return nil, fmt.Errorf("no cache storage available")
	}

	var entries []*MetadataEntry

	// 收集服务注册信息
	if serviceEntries, err := bm.collectServiceRegistry(ctx, redisClient); err != nil {
		bm.logger.Printf("Failed to collect service registry: %v", err)
	} else {
		entries = append(entries, serviceEntries...)
	}

	// 收集数据索引
	if indexEntries, err := bm.collectDataIndex(ctx, redisClient); err != nil {
		bm.logger.Printf("Failed to collect data index: %v", err)
	} else {
		entries = append(entries, indexEntries...)
	}

	// 收集表结构信息
	if schemaEntries, err := bm.collectTableSchema(ctx, redisClient); err != nil {
		bm.logger.Printf("Failed to collect table schema: %v", err)
	} else {
		entries = append(entries, schemaEntries...)
	}

	// 收集集群信息
	if clusterEntries, err := bm.collectClusterInfo(ctx, redisClient); err != nil {
		bm.logger.Printf("Failed to collect cluster info: %v", err)
	} else {
		entries = append(entries, clusterEntries...)
	}

	return entries, nil
}

// collectServiceRegistry 收集服务注册信息
func (bm *BackupManager) collectServiceRegistry(ctx context.Context, client redis.Cmdable) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	// 获取所有服务注册相关的键
	keys, err := client.Keys(ctx, "service:*").Result()
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		// 获取键类型
		keyType, err := client.Type(ctx, key).Result()
		if err != nil {
			continue
		}

		var value interface{}
		var ttl time.Duration

		// 获取TTL
		if ttlVal, err := client.TTL(ctx, key).Result(); err == nil && ttlVal > 0 {
			ttl = ttlVal
		}

		// 根据类型获取值
		switch keyType {
		case "string":
			if val, err := client.Get(ctx, key).Result(); err == nil {
				value = val
			}
		case "hash":
			if val, err := client.HGetAll(ctx, key).Result(); err == nil {
				value = val
			}
		case "set":
			if val, err := client.SMembers(ctx, key).Result(); err == nil {
				value = val
			}
		}

		if value != nil {
			entries = append(entries, &MetadataEntry{
				Type:      MetadataTypeServiceRegistry,
				Key:       key,
				Value:     value,
				Timestamp: time.Now(),
				TTL:       ttl,
			})
		}
	}

	return entries, nil
}

// collectDataIndex 收集数据索引
func (bm *BackupManager) collectDataIndex(ctx context.Context, client redis.Cmdable) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	// 获取所有数据索引相关的键
	patterns := []string{"index:*", "table:*", "partition:*"}

	for _, pattern := range patterns {
		keys, err := client.Keys(ctx, pattern).Result()
		if err != nil {
			continue
		}

		for _, key := range keys {
			value, err := client.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			entries = append(entries, &MetadataEntry{
				Type:      MetadataTypeDataIndex,
				Key:       key,
				Value:     value,
				Timestamp: time.Now(),
			})
		}
	}

	return entries, nil
}

// collectTableSchema 收集表结构信息
func (bm *BackupManager) collectTableSchema(ctx context.Context, client redis.Cmdable) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	// 获取所有表结构相关的键
	keys, err := client.Keys(ctx, "schema:*").Result()
	if err != nil {
		return entries, err
	}

	for _, key := range keys {
		value, err := client.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}

		entries = append(entries, &MetadataEntry{
			Type:      MetadataTypeTableSchema,
			Key:       key,
			Value:     value,
			Timestamp: time.Now(),
		})
	}

	return entries, nil
}

// collectClusterInfo 收集集群信息
func (bm *BackupManager) collectClusterInfo(ctx context.Context, client redis.Cmdable) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	// 获取集群相关信息
	patterns := []string{"cluster:*", "node:*", "shard:*"}

	for _, pattern := range patterns {
		keys, err := client.Keys(ctx, pattern).Result()
		if err != nil {
			continue
		}

		for _, key := range keys {
			keyType, err := client.Type(ctx, key).Result()
			if err != nil {
				continue
			}

			var value interface{}
			switch keyType {
			case "string":
				if val, err := client.Get(ctx, key).Result(); err == nil {
					value = val
				}
			case "hash":
				if val, err := client.HGetAll(ctx, key).Result(); err == nil {
					value = val
				}
			}

			if value != nil {
				entries = append(entries, &MetadataEntry{
					Type:      MetadataTypeClusterInfo,
					Key:       key,
					Value:     value,
					Timestamp: time.Now(),
				})
			}
		}
	}

	return entries, nil
}

// cleanupLoop 清理循环
func (bm *BackupManager) cleanupLoop(ctx context.Context) {
	defer bm.wg.Done()

	// 每天执行一次清理
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case <-ticker.C:
			if err := bm.cleanupOldBackups(ctx); err != nil {
				bm.logger.Printf("Cleanup failed: %v", err)
			}
		}
	}
}

// cleanupOldBackups 清理旧备份
func (bm *BackupManager) cleanupOldBackups(ctx context.Context) error {
	cutoffTime := time.Now().AddDate(0, 0, -bm.config.RetentionDays)
	prefix := fmt.Sprintf("metadata-backup/%s/", bm.nodeID)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// 列出所有备份对象
	var objects []storage.ObjectInfo
	var err error

	if bm.objectStorage != nil {
		objects, err = bm.objectStorage.ListObjectsSimple(ctx, bm.bucket, prefix)
	} else if bm.storage != nil {
		objects, err = bm.storage.ListObjectsSimple(ctx, bm.bucket, prefix)
	} else {
		return fmt.Errorf("no object storage available")
	}

	if err != nil {
		return fmt.Errorf("failed to list backup objects: %w", err)
	}

	for _, obj := range objects {
		if obj.LastModified.Before(cutoffTime) {
			// 删除过期备份
			var deleteErr error
			if bm.objectStorage != nil {
				deleteErr = bm.objectStorage.DeleteObject(ctx, bm.bucket, obj.Name)
			} else if bm.storage != nil {
				deleteErr = bm.storage.DeleteObject(ctx, bm.bucket, obj.Name)
			}

			if deleteErr != nil {
				bm.logger.Printf("Failed to delete expired backup %s: %v", obj.Name, deleteErr)
			} else {
				bm.logger.Printf("Deleted expired backup: %s", obj.Name)
			}
		}
	}

	return nil
}

// updateStats 更新统计信息
func (bm *BackupManager) updateStats(backupTime time.Time) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	bm.stats.LastBackupTime = backupTime
	bm.stats.TotalBackups++
}

// incrementErrorCount 增加错误计数
func (bm *BackupManager) incrementErrorCount() {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	bm.stats.FailedBackups++
}

// IsEnabled 检查备份管理器是否启用
func (bm *BackupManager) IsEnabled() bool {
	return bm != nil && bm.ctx != nil
}

// GetStats 获取统计信息
func (bm *BackupManager) GetStats() map[string]interface{} {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	return map[string]interface{}{
		"node_id":        bm.nodeID,
		"last_backup":    bm.stats.LastBackupTime,
		"total_backups":  bm.stats.TotalBackups,
		"failed_backups": bm.stats.FailedBackups,
		"interval":       bm.config.Interval,
		"retention_days": bm.config.RetentionDays,
	}
}
