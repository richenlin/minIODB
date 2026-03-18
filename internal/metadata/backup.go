package metadata

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// MetadataType 元数据类型
type MetadataType string

const (
	MetadataTypeServiceRegistry MetadataType = "service_registry" // 服务注册信息
	MetadataTypeDataIndex       MetadataType = "data_index"       // 数据索引
	MetadataTypeTableSchema     MetadataType = "table_schema"     // 表结构
	MetadataTypeTableConfig     MetadataType = "table_config"     // 表配置
	MetadataTypeClusterInfo     MetadataType = "cluster_info"     // 集群信息
)

// BackupMode 备份模式
type BackupMode int

const (
	BackupModeFull        BackupMode = iota // 全量备份
	BackupModeIncremental                   // 增量备份
)

// String 返回备份模式的字符串表示
func (bm BackupMode) String() string {
	switch bm {
	case BackupModeFull:
		return "full"
	case BackupModeIncremental:
		return "incremental"
	default:
		return "unknown"
	}
}

// IncrementalBackupConfig 增量备份配置
type IncrementalBackupConfig struct {
	LastBackupTime time.Time `json:"last_backup_time"` // 上次备份时间
	Enabled        bool      `json:"enabled"`          // 是否启用增量备份
}

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
	NodeID      string           `json:"node_id"`
	Timestamp   time.Time        `json:"timestamp"`
	Version     string           `json:"version"`
	Entries     []*MetadataEntry `json:"entries"`
	BackupMode  BackupMode       `json:"backup_mode"`  // 备份模式
	BaseVersion string           `json:"base_version"` // 增量备份的基础版本（全量备份的版本号）
}

// BackupConfig 备份配置
type BackupConfig struct {
	Enabled           bool          `yaml:"enabled"`            // 是否启用备份
	Interval          time.Duration `yaml:"interval"`           // 备份间隔
	RetentionDays     int           `yaml:"retention_days"`     // 保留天数
	Bucket            string        `yaml:"bucket"`             // 存储桶
	EncryptionEnabled bool          `yaml:"encryption_enabled"` // 是否启用加密
	EncryptionKey     []byte        `yaml:"encryption_key"`     // AES-256 加密密钥（32字节）
	BackupMode        BackupMode    `yaml:"backup_mode"`        // 备份模式（全量/增量）
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
	logger   *zap.Logger
	isActive bool

	// 统计信息
	stats BackupStats
}

// encrypt 使用 AES-256-GCM 加密数据
func (bm *BackupManager) encrypt(plaintext []byte) ([]byte, error) {
	if !bm.config.EncryptionEnabled || len(bm.config.EncryptionKey) == 0 {
		return plaintext, nil
	}

	if len(bm.config.EncryptionKey) != keyLength {
		return nil, fmt.Errorf("invalid encryption key length: expected %d, got %d", keyLength, len(bm.config.EncryptionKey))
	}

	block, err := aes.NewCipher(bm.config.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ciphertext...), nil
}

// decrypt 使用 AES-256-GCM 解密数据
func (bm *BackupManager) decrypt(ciphertext []byte) ([]byte, error) {
	if !bm.config.EncryptionEnabled || len(bm.config.EncryptionKey) == 0 {
		return ciphertext, nil
	}

	if len(bm.config.EncryptionKey) != keyLength {
		return nil, fmt.Errorf("invalid encryption key length: expected %d, got %d", keyLength, len(bm.config.EncryptionKey))
	}

	block, err := aes.NewCipher(bm.config.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

const (
	encryptedMarker = "ENCRYPTED:" // 加密数据标记前缀
	keyLength       = 32           // AES-256 密钥长度

	// Redis keys for backup state
	backupStateKey         = "metadata:backup:state"
	backupLastFullTimeKey  = "metadata:backup:last_full_time"
	backupLastFullVersion  = "metadata:backup:last_full_version"
	backupIncrementalCount = "metadata:backup:incremental_count"
)

// BackupStats 备份统计信息
type BackupStats struct {
	TotalBackups         int               `json:"total_backups"`          // 总备份数
	LastBackupTime       time.Time         `json:"last_backup_time"`       // 最后备份时间
	LastBackupSize       int64             `json:"last_backup_size"`       // 最后备份大小
	TotalBackupSize      int64             `json:"total_backup_size"`      // 总备份大小
	SuccessfulBackups    int               `json:"successful_backups"`     // 成功备份数
	FailedBackups        int               `json:"failed_backups"`         // 失败备份数
	AverageBackupTime    time.Duration     `json:"average_backup_time"`    // 平均备份时间
	LastFullBackupTime   time.Time         `json:"last_full_backup_time"`  // 最后全量备份时间
	IncrementalBackupCfg IncrementalBackup `json:"incremental_backup_cfg"` // 增量备份配置状态
}

// IncrementalBackup 增量备份状态
type IncrementalBackup struct {
	LastBackupTime   time.Time `json:"last_backup_time"`  // 上次备份时间
	LastFullVersion  string    `json:"last_full_version"` // 上次全量备份版本
	IncrementalCount int       `json:"incremental_count"` // 增量备份次数
	Enabled          bool      `json:"enabled"`           // 是否启用
}

// NewBackupManager 创建备份管理器（向后兼容）
func NewBackupManager(storage storage.Storage, nodeID string, config BackupConfig, logger *zap.Logger) *BackupManager {
	return NewBackupManagerWithStorages(storage, nil, nil, nodeID, config, logger)
}

// NewBackupManagerWithStorages 使用分离的存储接口创建备份管理器
func NewBackupManagerWithStorages(unifiedStorage storage.Storage, cacheStorage storage.CacheStorage, objectStorage storage.ObjectStorage, nodeID string, config BackupConfig, logger *zap.Logger) *BackupManager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &BackupManager{
		storage:       unifiedStorage,
		cacheStorage:  cacheStorage,
		objectStorage: objectStorage,
		nodeID:        nodeID,
		bucket:        config.Bucket,
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		logger:        logger,
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
func (bm *BackupManager) Start() error {
	bm.logger.Info("Starting metadata backup manager", zap.Duration("interval", bm.config.Interval))
	bm.logger.Info("Backup configuration", zap.String("bucket", bm.bucket), zap.Bool("storage", bm.storage != nil), zap.Bool("objectStorage", bm.objectStorage != nil), zap.Bool("cacheStorage", bm.cacheStorage != nil))

	// 立即执行一次备份
	if err := bm.performBackup(); err != nil {
		bm.logger.Error("Initial backup failed", zap.Error(err))
	}

	// 启动定期备份
	bm.wg.Add(1)
	go bm.backupLoop()

	// 启动清理任务
	bm.wg.Add(1)
	go bm.cleanupLoop()

	return nil
}

// Stop 停止备份管理器
func (bm *BackupManager) Stop() error {
	bm.logger.Info("Stopping metadata backup manager")

	bm.cancel()
	bm.wg.Wait()

	bm.logger.Info("Metadata backup manager stopped")
	return nil
}

// backupLoop 备份循环
func (bm *BackupManager) backupLoop() {
	defer bm.wg.Done()

	ticker := time.NewTicker(bm.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case <-ticker.C:
			if err := bm.performBackup(); err != nil {
				bm.incrementErrorCount()
				bm.logger.Error("Backup failed", zap.Error(err))
			}
		}
	}
}

// performBackup 执行备份
func (bm *BackupManager) performBackup() error {
	startTime := time.Now()
	bm.logger.Info("Starting metadata backup")
	bm.logger.Info("Backup target bucket", zap.String("bucket", bm.bucket))

	// 确定备份模式
	backupMode := bm.determineBackupMode()
	bm.logger.Info("Backup mode determined", zap.String("mode", backupMode.String()))

	// 收集元数据
	var entries []*MetadataEntry
	var err error

	if backupMode == BackupModeIncremental {
		// 增量备份：只收集变化的元数据
		lastBackupTime, err := bm.getLastBackupTime()
		if err != nil {
			bm.logger.Warn("Failed to get last backup time, falling back to full backup", zap.Error(err))
			backupMode = BackupModeFull
		} else {
			entries, err = bm.collectMetadataIncremental(lastBackupTime)
			if err != nil {
				return fmt.Errorf("failed to collect incremental metadata: %w", err)
			}
			bm.logger.Info("Incremental backup collected entries",
				zap.Int("entries", len(entries)),
				zap.Time("since", lastBackupTime))
		}
	}

	// 全量备份或增量备份无数据时收集所有元数据
	if backupMode == BackupModeFull || entries == nil {
		entries, err = bm.collectMetadata()
		if err != nil {
			return fmt.Errorf("failed to collect metadata: %w", err)
		}
	}

	// 获取当前版本号并递增
	currentVersion, err := bm.getCurrentAndIncrementVersion()
	if err != nil {
		bm.logger.Error("Failed to get/increment version, using default", zap.Error(err))
		currentVersion = "1.0.0"
	}

	// 获取基础版本（增量备份需要）
	baseVersion := ""
	if backupMode == BackupModeIncremental {
		baseVersion, _ = bm.getLastFullBackupVersion()
	}

	// 创建备份快照
	snapshot := &BackupSnapshot{
		NodeID:      bm.nodeID,
		Timestamp:   startTime,
		Version:     currentVersion,
		Entries:     entries,
		BackupMode:  backupMode,
		BaseVersion: baseVersion,
	}

	// 序列化快照
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// 如果启用加密，进行 AES-256-GCM 加密
	if bm.config.EncryptionEnabled {
		originalSize := len(data)
		encryptedData, err := bm.encrypt(data)
		if err != nil {
			return fmt.Errorf("failed to encrypt backup: %w", err)
		}
		data = encryptedData
		bm.logger.Info("Backup encrypted with AES-256-GCM",
			zap.Int("original_size", originalSize),
			zap.Int("encrypted_size", len(data)))
	}

	// 上传到MinIO
	var objectPrefix string
	if backupMode == BackupModeIncremental {
		objectPrefix = fmt.Sprintf("metadata-backup/%s/%s/incremental_%s.json", bm.nodeID, startTime.Format("2006/01/02"), startTime.Format("20060102_150405"))
	} else {
		objectPrefix = fmt.Sprintf("metadata-backup/%s/%s/backup_%s.json", bm.nodeID, startTime.Format("2006/01/02"), startTime.Format("20060102_150405"))
	}

	reader := bytes.NewReader(data)

	// 调试日志
	bm.logger.Info("Attempting to upload backup to bucket", zap.String("bucket", bm.bucket), zap.String("object", objectPrefix))

	// 使用新的存储接口
	var uploadErr error
	if bm.objectStorage != nil {
		bm.logger.Info("Using object storage interface for backup upload")
		uploadErr = bm.objectStorage.PutObject(context.Background(), bm.bucket, objectPrefix, reader, int64(len(data)))
	} else if bm.storage != nil {
		bm.logger.Info("Using unified storage interface for backup upload")
		uploadErr = bm.storage.PutObject(context.Background(), bm.bucket, objectPrefix, reader, int64(len(data)))
	} else {
		return fmt.Errorf("no object storage available")
	}

	if uploadErr != nil {
		bm.logger.Error("Backup upload failed to bucket", zap.String("bucket", bm.bucket), zap.Error(uploadErr))
		return fmt.Errorf("failed to upload backup: %w", uploadErr)
	}

	// 更新备份状态
	if err := bm.updateBackupState(backupMode, startTime, currentVersion); err != nil {
		bm.logger.Warn("Failed to update backup state", zap.Error(err))
	}

	// 更新统计信息
	bm.updateStatsWithMode(startTime, backupMode)

	duration := time.Since(startTime)
	bm.logger.Info("Backup completed successfully",
		zap.Int("entries", len(entries)),
		zap.String("version", currentVersion),
		zap.String("mode", backupMode.String()),
		zap.Duration("duration", duration),
		zap.String("object", objectPrefix))

	return nil
}

// getCurrentAndIncrementVersion 获取当前版本号并递增
func (bm *BackupManager) getCurrentAndIncrementVersion() (string, error) {
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

	ctx := context.Background()

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
func (bm *BackupManager) CollectMetadata() ([]*MetadataEntry, error) {
	return bm.collectMetadata()
}

// collectMetadata 收集元数据
func (bm *BackupManager) collectMetadata() ([]*MetadataEntry, error) {
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
	ctx := context.Background()

	// 收集服务注册信息
	if serviceEntries, err := bm.collectServiceRegistry(ctx, redisClient); err != nil {
		bm.logger.Error("Failed to collect service registry", zap.Error(err))
	} else {
		entries = append(entries, serviceEntries...)
	}

	// 收集数据索引
	if indexEntries, err := bm.collectDataIndex(ctx, redisClient); err != nil {
		bm.logger.Error("Failed to collect data index", zap.Error(err))
	} else {
		entries = append(entries, indexEntries...)
	}

	// 收集表结构信息
	if schemaEntries, err := bm.collectTableSchema(ctx, redisClient); err != nil {
		bm.logger.Error("Failed to collect table schema", zap.Error(err))
	} else {
		entries = append(entries, schemaEntries...)
	}

	// 收集集群信息
	if clusterEntries, err := bm.collectClusterInfo(ctx, redisClient); err != nil {
		bm.logger.Error("Failed to collect cluster info", zap.Error(err))
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
func (bm *BackupManager) cleanupLoop() {
	defer bm.wg.Done()

	// 每天执行一次清理
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case <-ticker.C:
			if err := bm.cleanupOldBackups(); err != nil {
				bm.logger.Error("Cleanup failed", zap.Error(err))
			}
		}
	}
}

// cleanupOldBackups 清理旧备份
func (bm *BackupManager) cleanupOldBackups() error {
	cutoffTime := time.Now().AddDate(0, 0, -bm.config.RetentionDays)
	prefix := fmt.Sprintf("metadata-backup/%s/", bm.nodeID)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
				bm.logger.Error("Failed to delete expired backup", zap.String("object_name", obj.Name), zap.Error(deleteErr))
			} else {
				bm.logger.Info("Deleted expired backup", zap.String("object_name", obj.Name))
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
		"node_id":             bm.nodeID,
		"last_backup":         bm.stats.LastBackupTime,
		"total_backups":       bm.stats.TotalBackups,
		"failed_backups":      bm.stats.FailedBackups,
		"interval":            bm.config.Interval,
		"retention_days":      bm.config.RetentionDays,
		"backup_mode":         bm.config.BackupMode.String(),
		"last_full_backup":    bm.stats.LastFullBackupTime,
		"incremental_enabled": bm.stats.IncrementalBackupCfg.Enabled,
	}
}

// determineBackupMode 确定备份模式
func (bm *BackupManager) determineBackupMode() BackupMode {
	// 如果配置中指定了增量备份模式
	if bm.config.BackupMode == BackupModeIncremental {
		// 检查是否有上次全量备份
		lastFullTime, err := bm.getLastFullBackupTime()
		if err != nil || lastFullTime.IsZero() {
			bm.logger.Info("No previous full backup found, performing full backup")
			return BackupModeFull
		}
		return BackupModeIncremental
	}
	return BackupModeFull
}

// getLastBackupTime 获取上次备份时间（用于增量备份）
func (bm *BackupManager) getLastBackupTime() (time.Time, error) {
	redisClient, err := bm.getRedisClient()
	if err != nil {
		return time.Time{}, err
	}

	ctx := context.Background()
	timeStr, err := redisClient.Get(ctx, backupStateKey).Result()
	if err == redis.Nil {
		return time.Time{}, fmt.Errorf("no previous backup time found")
	} else if err != nil {
		return time.Time{}, fmt.Errorf("failed to get last backup time: %w", err)
	}

	lastTime, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse last backup time: %w", err)
	}

	return lastTime, nil
}

// getLastFullBackupTime 获取上次全量备份时间
func (bm *BackupManager) getLastFullBackupTime() (time.Time, error) {
	redisClient, err := bm.getRedisClient()
	if err != nil {
		return time.Time{}, err
	}

	ctx := context.Background()
	timeStr, err := redisClient.Get(ctx, backupLastFullTimeKey).Result()
	if err == redis.Nil {
		return time.Time{}, nil
	} else if err != nil {
		return time.Time{}, fmt.Errorf("failed to get last full backup time: %w", err)
	}

	lastTime, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse last full backup time: %w", err)
	}

	return lastTime, nil
}

// getLastFullBackupVersion 获取上次全量备份版本
func (bm *BackupManager) getLastFullBackupVersion() (string, error) {
	redisClient, err := bm.getRedisClient()
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	version, err := redisClient.Get(ctx, backupLastFullVersion).Result()
	if err == redis.Nil {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("failed to get last full backup version: %w", err)
	}

	return version, nil
}

// getRedisClient 获取 Redis 客户端
func (bm *BackupManager) getRedisClient() (redis.Cmdable, error) {
	if bm.cacheStorage != nil {
		client := bm.cacheStorage.GetClient()
		if client == nil {
			return nil, fmt.Errorf("redis client not available from cache storage")
		}
		redisClient, ok := client.(redis.Cmdable)
		if !ok {
			return nil, fmt.Errorf("invalid redis client type from cache storage")
		}
		return redisClient, nil
	} else if bm.storage != nil {
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
		redisClient, ok := client.(redis.Cmdable)
		if !ok {
			return nil, fmt.Errorf("invalid redis client type")
		}
		return redisClient, nil
	}
	return nil, fmt.Errorf("no cache storage available")
}

// collectMetadataIncremental 增量收集元数据（基于对象最后修改时间）
func (bm *BackupManager) collectMetadataIncremental(since time.Time) ([]*MetadataEntry, error) {
	// 获取 Redis 客户端
	redisClient, err := bm.getRedisClient()
	if err != nil {
		return nil, err
	}

	var entries []*MetadataEntry
	ctx := context.Background()

	// 收集自上次备份以来变化的元数据
	// 这里通过检查 MinIO 中存储的数据对象的 LastModified 来确定增量备份范围
	// 同时也检查 Redis 中键的最后修改时间

	// 收集服务注册信息（增量）
	if serviceEntries, err := bm.collectServiceRegistryIncremental(ctx, redisClient, since); err != nil {
		bm.logger.Error("Failed to collect incremental service registry", zap.Error(err))
	} else {
		entries = append(entries, serviceEntries...)
	}

	// 收集数据索引（增量）
	if indexEntries, err := bm.collectDataIndexIncremental(ctx, redisClient, since); err != nil {
		bm.logger.Error("Failed to collect incremental data index", zap.Error(err))
	} else {
		entries = append(entries, indexEntries...)
	}

	// 收集表结构信息（增量）
	if schemaEntries, err := bm.collectTableSchemaIncremental(ctx, redisClient, since); err != nil {
		bm.logger.Error("Failed to collect incremental table schema", zap.Error(err))
	} else {
		entries = append(entries, schemaEntries...)
	}

	// 收集集群信息（增量）
	if clusterEntries, err := bm.collectClusterInfoIncremental(ctx, redisClient, since); err != nil {
		bm.logger.Error("Failed to collect incremental cluster info", zap.Error(err))
	} else {
		entries = append(entries, clusterEntries...)
	}

	return entries, nil
}

// collectServiceRegistryIncremental 增量收集服务注册信息
func (bm *BackupManager) collectServiceRegistryIncremental(ctx context.Context, client redis.Cmdable, since time.Time) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	keys, err := client.Keys(ctx, "service:*").Result()
	if err != nil {
		return nil, err
	}

	for _, key := range keys {
		// 检查键的空闲时间来判断是否被修改
		idleTime, err := client.ObjectIdleTime(ctx, key).Result()
		if err != nil {
			// 如果无法获取空闲时间，保守起见包含该键
			bm.logger.Debug("Could not get idle time for key, including in backup", zap.String("key", key))
		} else {
			// 如果键的空闲时间大于备份间隔，说明没有修改，跳过
			timeSinceModification := time.Duration(idleTime) * time.Second
			if timeSinceModification > time.Since(since) {
				continue
			}
		}

		keyType, err := client.Type(ctx, key).Result()
		if err != nil {
			continue
		}

		var value interface{}
		var ttl time.Duration

		if ttlVal, err := client.TTL(ctx, key).Result(); err == nil && ttlVal > 0 {
			ttl = ttlVal
		}

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

// collectDataIndexIncremental 增量收集数据索引
func (bm *BackupManager) collectDataIndexIncremental(ctx context.Context, client redis.Cmdable, since time.Time) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	patterns := []string{"index:*", "table:*", "partition:*"}

	for _, pattern := range patterns {
		keys, err := client.Keys(ctx, pattern).Result()
		if err != nil {
			continue
		}

		for _, key := range keys {
			// 检查键的空闲时间
			idleTime, err := client.ObjectIdleTime(ctx, key).Result()
			if err != nil {
				bm.logger.Debug("Could not get idle time for key, including in backup", zap.String("key", key))
			} else {
				timeSinceModification := time.Duration(idleTime) * time.Second
				if timeSinceModification > time.Since(since) {
					continue
				}
			}

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

// collectTableSchemaIncremental 增量收集表结构信息
func (bm *BackupManager) collectTableSchemaIncremental(ctx context.Context, client redis.Cmdable, since time.Time) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	keys, err := client.Keys(ctx, "schema:*").Result()
	if err != nil {
		return entries, err
	}

	for _, key := range keys {
		// 检查键的空闲时间
		idleTime, err := client.ObjectIdleTime(ctx, key).Result()
		if err != nil {
			bm.logger.Debug("Could not get idle time for key, including in backup", zap.String("key", key))
		} else {
			timeSinceModification := time.Duration(idleTime) * time.Second
			if timeSinceModification > time.Since(since) {
				continue
			}
		}

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

// collectClusterInfoIncremental 增量收集集群信息
func (bm *BackupManager) collectClusterInfoIncremental(ctx context.Context, client redis.Cmdable, since time.Time) ([]*MetadataEntry, error) {
	var entries []*MetadataEntry

	patterns := []string{"cluster:*", "node:*", "shard:*"}

	for _, pattern := range patterns {
		keys, err := client.Keys(ctx, pattern).Result()
		if err != nil {
			continue
		}

		for _, key := range keys {
			// 检查键的空闲时间
			idleTime, err := client.ObjectIdleTime(ctx, key).Result()
			if err != nil {
				bm.logger.Debug("Could not get idle time for key, including in backup", zap.String("key", key))
			} else {
				timeSinceModification := time.Duration(idleTime) * time.Second
				if timeSinceModification > time.Since(since) {
					continue
				}
			}

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

// updateBackupState 更新备份状态到 Redis
func (bm *BackupManager) updateBackupState(backupMode BackupMode, backupTime time.Time, version string) error {
	redisClient, err := bm.getRedisClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	// 更新最后备份时间
	if err := redisClient.Set(ctx, backupStateKey, backupTime.Format(time.RFC3339), 0).Err(); err != nil {
		return fmt.Errorf("failed to update backup state: %w", err)
	}

	// 如果是全量备份，更新全量备份相关状态
	if backupMode == BackupModeFull {
		if err := redisClient.Set(ctx, backupLastFullTimeKey, backupTime.Format(time.RFC3339), 0).Err(); err != nil {
			return fmt.Errorf("failed to update last full backup time: %w", err)
		}
		if err := redisClient.Set(ctx, backupLastFullVersion, version, 0).Err(); err != nil {
			return fmt.Errorf("failed to update last full backup version: %w", err)
		}
		// 重置增量备份计数
		if err := redisClient.Set(ctx, backupIncrementalCount, "0", 0).Err(); err != nil {
			bm.logger.Warn("Failed to reset incremental count", zap.Error(err))
		}
	} else {
		// 增量备份，递增计数
		if err := redisClient.Incr(ctx, backupIncrementalCount).Err(); err != nil {
			bm.logger.Warn("Failed to increment incremental count", zap.Error(err))
		}
	}

	return nil
}

// updateStatsWithMode 更新带模式的统计信息
func (bm *BackupManager) updateStatsWithMode(backupTime time.Time, backupMode BackupMode) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	bm.stats.LastBackupTime = backupTime
	bm.stats.TotalBackups++
	bm.stats.IncrementalBackupCfg.Enabled = bm.config.BackupMode == BackupModeIncremental
	bm.stats.IncrementalBackupCfg.LastBackupTime = backupTime

	if backupMode == BackupModeFull {
		bm.stats.LastFullBackupTime = backupTime
		bm.stats.IncrementalBackupCfg.LastFullVersion = ""
		bm.stats.IncrementalBackupCfg.IncrementalCount = 0
	} else {
		bm.stats.IncrementalBackupCfg.IncrementalCount++
	}
}

// GetIncrementalBackupConfig 获取增量备份配置
func (bm *BackupManager) GetIncrementalBackupConfig() IncrementalBackupConfig {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()

	return IncrementalBackupConfig{
		LastBackupTime: bm.stats.LastBackupTime,
		Enabled:        bm.config.BackupMode == BackupModeIncremental,
	}
}

// SetBackupMode 设置备份模式
func (bm *BackupManager) SetBackupMode(mode BackupMode) {
	bm.mutex.Lock()
	defer bm.mutex.Unlock()

	bm.config.BackupMode = mode
	bm.logger.Info("Backup mode changed", zap.String("mode", mode.String()))
}

// ForceFullBackup 强制执行一次全量备份
func (bm *BackupManager) ForceFullBackup() error {
	bm.mutex.Lock()
	originalMode := bm.config.BackupMode
	bm.config.BackupMode = BackupModeFull
	bm.mutex.Unlock()

	err := bm.performBackup()

	// 恢复原来的模式
	bm.mutex.Lock()
	bm.config.BackupMode = originalMode
	bm.mutex.Unlock()

	return err
}

// GetBackupMode 获取当前备份模式
func (bm *BackupManager) GetBackupMode() BackupMode {
	bm.mutex.RLock()
	defer bm.mutex.RUnlock()
	return bm.config.BackupMode
}
