package metadata

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"minIODB/internal/storage"
	"minIODB/pkg/logger"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// RecoveryManager 恢复管理器
type RecoveryManager struct {
	// 新的分离存储接口
	cacheStorage  storage.CacheStorage
	objectStorage storage.ObjectStorage

	// 向后兼容的统一存储接口
	storage storage.Storage

	nodeID string
	bucket string
	logger *zap.Logger
	mutex  sync.RWMutex

	// 加密配置
	encryptionEnabled bool
	encryptionKey     []byte
}

// RecoveryOptions 恢复选项
type RecoveryOptions struct {
	// 时间范围
	FromTime *time.Time `json:"from_time,omitempty"`
	ToTime   *time.Time `json:"to_time,omitempty"`

	// 元数据类型过滤
	MetadataTypes []MetadataType `json:"metadata_types,omitempty"`

	// 键模式过滤
	KeyPatterns []string `json:"key_patterns,omitempty"`

	// 恢复模式
	Mode RecoveryMode `json:"mode"`

	// 是否覆盖现有数据
	Overwrite bool `json:"overwrite"`

	// 是否验证数据
	Validate bool `json:"validate"`

	// 是否为干运行模式
	DryRun bool `json:"dry_run"`

	// 过滤器选项
	Filters map[string]interface{} `json:"filters,omitempty"`

	// 是否并行执行
	Parallel bool `json:"parallel"`
}

// RecoveryMode 恢复模式
type RecoveryMode string

const (
	RecoveryModeComplete RecoveryMode = "complete" // 完整恢复
	RecoveryModePartial  RecoveryMode = "partial"  // 部分恢复
	RecoveryModeDryRun   RecoveryMode = "dry_run"  // 干运行（不实际执行）
)

// RecoveryResult 恢复结果
type RecoveryResult struct {
	BackupFile       string                 `json:"backup_file"`        // 备份文件名
	BackupObjectName string                 `json:"backup_object_name"` // 备份对象名称
	StartTime        time.Time              `json:"start_time"`         // 开始时间
	EndTime          time.Time              `json:"end_time"`           // 结束时间
	Duration         time.Duration          `json:"duration"`           // 持续时间
	EntriesTotal     int                    `json:"entries_total"`      // 总条目数
	EntriesOK        int                    `json:"entries_ok"`         // 成功处理条目数
	EntriesSkipped   int                    `json:"entries_skipped"`    // 跳过的条目数
	EntriesError     int                    `json:"entries_error"`      // 错误条目数
	TotalEntries     int                    `json:"total_entries"`      // 总条目数（向后兼容）
	ProcessedEntries int                    `json:"processed_entries"`  // 已处理条目数（向后兼容）
	SkippedEntries   int                    `json:"skipped_entries"`    // 跳过的条目数（向后兼容）
	ErrorEntries     int                    `json:"error_entries"`      // 错误条目数（向后兼容）
	Success          bool                   `json:"success"`            // 是否成功
	Errors           []string               `json:"errors"`             // 错误信息
	Details          map[string]interface{} `json:"details"`            // 详细信息
}

// BackupInfo 备份信息
type BackupInfo struct {
	ObjectName   string    `json:"object_name"`   // 对象名称
	NodeID       string    `json:"node_id"`       // 节点ID
	Size         int64     `json:"size"`          // 大小
	LastModified time.Time `json:"last_modified"` // 最后修改时间
	Timestamp    time.Time `json:"timestamp"`     // 备份时间戳
}

// NewRecoveryManager 创建恢复管理器（向后兼容）
func NewRecoveryManager(storage storage.Storage, nodeID, bucket string, logger *zap.Logger) *RecoveryManager {
	return NewRecoveryManagerWithStorages(storage, nil, nil, nodeID, bucket, false, nil)
}

// NewRecoveryManagerWithStorages 使用分离的存储接口创建恢复管理器
func NewRecoveryManagerWithStorages(unifiedStorage storage.Storage, cacheStorage storage.CacheStorage, objectStorage storage.ObjectStorage, nodeID, bucket string, encryptionEnabled bool, encryptionKey []byte) *RecoveryManager {
	manager := &RecoveryManager{
		storage:           unifiedStorage,
		cacheStorage:      cacheStorage,
		objectStorage:     objectStorage,
		nodeID:            nodeID,
		bucket:            bucket,
		logger:            logger.Logger,
		encryptionEnabled: encryptionEnabled,
		encryptionKey:     encryptionKey,
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

// decrypt 使用 AES-256-GCM 解密数据
func (rm *RecoveryManager) decrypt(ciphertext []byte) ([]byte, error) {
	if !rm.encryptionEnabled || len(rm.encryptionKey) == 0 {
		return ciphertext, nil
	}

	if len(rm.encryptionKey) != keyLength {
		return nil, fmt.Errorf("invalid encryption key length: expected %d, got %d", keyLength, len(rm.encryptionKey))
	}

	block, err := aes.NewCipher(rm.encryptionKey)
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

// ListBackups 列出可用的备份
func (rm *RecoveryManager) ListBackups(ctx context.Context, days int) ([]*BackupInfo, error) {
	prefix := fmt.Sprintf("metadata-backup/%s/", rm.nodeID)

	// 列出所有备份对象
	var objects []storage.ObjectInfo
	var err error

	if rm.objectStorage != nil {
		objects, err = rm.objectStorage.ListObjectsSimple(ctx, rm.bucket, prefix)
	} else if rm.storage != nil {
		objects, err = rm.storage.ListObjectsSimple(ctx, rm.bucket, prefix)
	} else {
		return nil, fmt.Errorf("no object storage available")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list backup objects: %w", err)
	}

	var backups []*BackupInfo
	cutoffTime := time.Now().AddDate(0, 0, -days)

	for _, obj := range objects {
		if obj.LastModified.Before(cutoffTime) {
			continue
		}

		// 解析备份文件名获取时间戳
		timestamp, err := rm.parseBackupTimestamp(obj.Name)
		if err == nil {
			backups = append(backups, &BackupInfo{
				ObjectName:   obj.Name,
				NodeID:       rm.nodeID,
				Size:         obj.Size,
				LastModified: obj.LastModified,
				Timestamp:    timestamp,
			})
		}
	}

	// 按时间戳排序
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

// GetLatestBackup 获取最新备份
func (rm *RecoveryManager) GetLatestBackup(ctx context.Context) (*BackupInfo, error) {
	backups, err := rm.ListBackups(ctx, 30) // 查找最近30天的备份
	if err != nil {
		return nil, err
	}

	if len(backups) == 0 {
		return nil, fmt.Errorf("no backup found for node %s", rm.nodeID)
	}

	return backups[0], nil
}

// RecoverFromLatest 从最新备份恢复
func (rm *RecoveryManager) RecoverFromLatest(ctx context.Context, options RecoveryOptions) (*RecoveryResult, error) {
	latestBackup, err := rm.GetLatestBackup(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest backup: %w", err)
	}

	return rm.RecoverFromBackup(ctx, latestBackup.ObjectName, options)
}

// RecoverFromBackup 从指定备份恢复
func (rm *RecoveryManager) RecoverFromBackup(ctx context.Context, backupObjectName string, options RecoveryOptions) (*RecoveryResult, error) {
	startTime := time.Now()
	result := &RecoveryResult{
		BackupFile:       backupObjectName,
		BackupObjectName: backupObjectName,
		StartTime:        startTime,
		Details:          make(map[string]interface{}),
	}

	rm.logger.Info("Starting recovery from backup", zap.String("backup_object_name", backupObjectName))

	// 下载备份文件
	snapshot, err := rm.downloadBackup(ctx, backupObjectName)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to download backup: %v", err))
		return result, err
	}

	result.EntriesTotal = len(snapshot.Entries)
	result.TotalEntries = len(snapshot.Entries)
	result.Details["backup_timestamp"] = snapshot.Timestamp
	result.Details["backup_version"] = snapshot.Version

	// 过滤条目
	filteredEntries := rm.filterEntries(snapshot.Entries, options)
	result.Details["entries_after_filter"] = len(filteredEntries)

	if options.Mode == RecoveryModeDryRun {
		rm.logger.Info("Dry run mode: would recover entries", zap.Int("entries", len(filteredEntries)))
		result.Success = true
		result.EntriesOK = len(filteredEntries)
		result.ProcessedEntries = len(filteredEntries)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	// 执行恢复 - 获取Redis客户端
	var client redis.Cmdable

	if rm.cacheStorage != nil {
		// 从缓存存储获取客户端
		redisClient := rm.cacheStorage.GetClient()
		if redisClient == nil {
			result.Success = false
			result.Errors = append(result.Errors, "Redis client not available from cache storage")
			return result, fmt.Errorf("Redis client not available from cache storage")
		}

		var ok bool
		client, ok = redisClient.(redis.Cmdable)
		if !ok {
			result.Success = false
			result.Errors = append(result.Errors, "Invalid redis client type from cache storage")
			return result, fmt.Errorf("Invalid redis client type from cache storage")
		}
	} else if rm.storage != nil {
		// 通过 Storage 的 PoolManager 获取 Redis 客户端
		poolManager := rm.storage.GetPoolManager()
		if poolManager == nil {
			result.Success = false
			result.Errors = append(result.Errors, "Pool manager not available")
			return result, fmt.Errorf("Pool manager not available")
		}

		redisPool := poolManager.GetRedisPool()
		if redisPool == nil {
			result.Success = false
			result.Errors = append(result.Errors, "Redis pool not available")
			return result, fmt.Errorf("Redis pool not available")
		}

		redisClient := redisPool.GetClient()
		if redisClient == nil {
			result.Success = false
			result.Errors = append(result.Errors, "Redis client not available")
			return result, fmt.Errorf("Redis client not available")
		}

		var ok bool
		client, ok = redisClient.(redis.Cmdable)
		if !ok {
			result.Success = false
			result.Errors = append(result.Errors, "Invalid redis client type")
			return result, fmt.Errorf("Invalid redis client type")
		}
	} else {
		result.Success = false
		result.Errors = append(result.Errors, "No cache storage available")
		return result, fmt.Errorf("No cache storage available")
	}

	// 开始事务（如果支持）
	pipe := client.TxPipeline()
	var pipeCommands int

	for _, entry := range filteredEntries {
		if err := rm.recoverEntry(ctx, pipe, entry, options, client); err != nil {
			result.EntriesError++
			result.ErrorEntries++
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to recover %s: %v", entry.Key, err))
			continue
		}

		pipeCommands++
		result.EntriesOK++
		result.ProcessedEntries++

		// 批量执行，避免管道过大
		if pipeCommands >= 100 {
			if _, err := pipe.Exec(ctx); err != nil {
				rm.logger.Error("Pipeline execution failed", zap.Error(err))
			}
			pipe = client.TxPipeline()
			pipeCommands = 0
		}
	}

	// 执行剩余命令
	if pipeCommands > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			rm.logger.Error("Final pipeline execution failed", zap.Error(err))
		}
	}

	result.Success = result.EntriesError == 0
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	rm.logger.Info("Recovery completed", zap.Bool("success", result.Success), zap.Int("total", result.EntriesTotal), zap.Int("ok", result.EntriesOK), zap.Int("error", result.EntriesError), zap.Duration("duration", result.Duration))

	return result, nil
}

// downloadBackup 下载备份文件
func (rm *RecoveryManager) downloadBackup(ctx context.Context, objectName string) (*BackupSnapshot, error) {
	rm.logger.Info("Downloading backup", zap.String("object_name", objectName))

	// 下载备份文件
	var data []byte
	var err error

	if rm.objectStorage != nil {
		data, err = rm.objectStorage.GetObjectBytes(ctx, rm.bucket, objectName)
	} else if rm.storage != nil {
		data, err = rm.storage.GetObjectBytes(ctx, rm.bucket, objectName)
	} else {
		return nil, fmt.Errorf("no object storage available")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to download backup file: %w", err)
	}

	// 如果启用加密，先尝试解密
	if rm.encryptionEnabled && len(rm.encryptionKey) > 0 {
		decryptedData, decryptErr := rm.decrypt(data)
		if decryptErr != nil {
			rm.logger.Warn("Failed to decrypt backup, trying to parse as plain JSON", zap.Error(decryptErr))
			// 解密失败，可能数据本身就是未加密的，继续尝试解析
		} else {
			data = decryptedData
			rm.logger.Info("Backup decrypted successfully")
		}
	}

	// 解析JSON
	var snapshot BackupSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to parse backup file: %w", err)
	}

	rm.logger.Info("Downloaded backup", zap.String("object_name", objectName), zap.Int("entries", len(snapshot.Entries)), zap.Time("timestamp", snapshot.Timestamp))

	return &snapshot, nil
}

// filterEntries 过滤条目
func (rm *RecoveryManager) filterEntries(entries []*MetadataEntry, options RecoveryOptions) []*MetadataEntry {
	var filtered []*MetadataEntry

	for _, entry := range entries {
		// 时间过滤
		if options.FromTime != nil && entry.Timestamp.Before(*options.FromTime) {
			continue
		}
		if options.ToTime != nil && entry.Timestamp.After(*options.ToTime) {
			continue
		}

		// 元数据类型过滤
		if len(options.MetadataTypes) > 0 {
			found := false
			for _, t := range options.MetadataTypes {
				if entry.Type == t {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// 键模式过滤
		if len(options.KeyPatterns) > 0 {
			matched := false
			for _, pattern := range options.KeyPatterns {
				if strings.Contains(entry.Key, pattern) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		filtered = append(filtered, entry)
	}

	return filtered
}

// recoverEntry 恢复单个条目
func (rm *RecoveryManager) recoverEntry(ctx context.Context, pipe redis.Pipeliner, entry *MetadataEntry, options RecoveryOptions, client redis.Cmdable) error {
	// 检查是否需要覆盖
	if !options.Overwrite {
		exists, err := client.Exists(ctx, entry.Key).Result()
		if err != nil {
			return fmt.Errorf("failed to check key existence: %w", err)
		}
		if exists > 0 {
			return nil // 跳过已存在的键
		}
	}

	// 根据值类型恢复
	switch value := entry.Value.(type) {
	case string:
		pipe.Set(ctx, entry.Key, value, entry.TTL)

	case map[string]interface{}:
		// 转换为map[string]string
		hashMap := make(map[string]interface{})
		for k, v := range value {
			hashMap[k] = v
		}
		pipe.HMSet(ctx, entry.Key, hashMap)
		if entry.TTL > 0 {
			pipe.Expire(ctx, entry.Key, entry.TTL)
		}

	case []interface{}:
		// 集合类型
		members := make([]interface{}, len(value))
		copy(members, value)
		pipe.SAdd(ctx, entry.Key, members...)
		if entry.TTL > 0 {
			pipe.Expire(ctx, entry.Key, entry.TTL)
		}

	default:
		// 尝试JSON序列化
		if data, err := json.Marshal(value); err == nil {
			pipe.Set(ctx, entry.Key, string(data), entry.TTL)
		} else {
			return fmt.Errorf("unsupported value type for key %s", entry.Key)
		}
	}

	return nil
}

// parseBackupTimestamp 解析备份时间戳
func (rm *RecoveryManager) parseBackupTimestamp(objectName string) (time.Time, error) {
	// 从对象名称中提取时间戳
	// 格式: metadata-backup/{nodeID}/{date}/backup_{timestamp}.json
	parts := strings.Split(objectName, "/")
	if len(parts) < 4 {
		return time.Time{}, fmt.Errorf("invalid backup object name format")
	}

	filename := parts[len(parts)-1]
	if !strings.HasPrefix(filename, "backup_") || !strings.HasSuffix(filename, ".json") {
		return time.Time{}, fmt.Errorf("invalid backup filename format")
	}

	timestampStr := strings.TrimSuffix(strings.TrimPrefix(filename, "backup_"), ".json")
	return time.Parse("20060102_150405", timestampStr)
}

// ValidateBackup 验证备份文件
func (rm *RecoveryManager) ValidateBackup(ctx context.Context, objectName string) error {
	snapshot, err := rm.downloadBackup(ctx, objectName)
	if err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}

	// 基本验证
	if snapshot.NodeID == "" {
		return fmt.Errorf("backup missing node ID")
	}

	if snapshot.Timestamp.IsZero() {
		return fmt.Errorf("backup missing timestamp")
	}

	if len(snapshot.Entries) == 0 {
		return fmt.Errorf("backup contains no entries")
	}

	// 验证条目
	for i, entry := range snapshot.Entries {
		if entry.Key == "" {
			return fmt.Errorf("entry %d missing key", i)
		}
		if entry.Type == "" {
			return fmt.Errorf("entry %d missing type", i)
		}
		if entry.Value == nil {
			return fmt.Errorf("entry %d missing value", i)
		}
	}

	rm.logger.Info("Backup validation passed", zap.String("object_name", objectName), zap.Int("entries", len(snapshot.Entries)))
	return nil
}

// GetRecoveryStatus 获取恢复状态（用于监控正在进行的恢复）
func (rm *RecoveryManager) GetRecoveryStatus() map[string]interface{} {
	// 这里可以实现恢复进度跟踪
	// 目前返回基本信息
	return map[string]interface{}{
		"node_id": rm.nodeID,
		"bucket":  rm.bucket,
	}
}
