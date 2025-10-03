package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
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
	logger *log.Logger
	mutex  sync.RWMutex
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
func NewRecoveryManager(storage storage.Storage, nodeID, bucket string) *RecoveryManager {
	return NewRecoveryManagerWithStorages(storage, nil, nil, nodeID, bucket)
}

// NewRecoveryManagerWithStorages 使用分离的存储接口创建恢复管理器
func NewRecoveryManagerWithStorages(unifiedStorage storage.Storage, cacheStorage storage.CacheStorage, objectStorage storage.ObjectStorage, nodeID, bucket string) *RecoveryManager {
	manager := &RecoveryManager{
		storage:       unifiedStorage,
		cacheStorage:  cacheStorage,
		objectStorage: objectStorage,
		nodeID:        nodeID,
		bucket:        bucket,
		logger:        log.New(log.Writer(), "[RECOVERY] ", log.LstdFlags),
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

	rm.logger.Printf("Starting recovery from backup: %s", backupObjectName)

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
		rm.logger.Printf("Dry run mode: would recover %d entries", len(filteredEntries))
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

	// 选择恢复模式：并行或顺序
	if options.Parallel {
		rm.logger.Printf("Using parallel recovery mode")
		result = rm.recoverEntriesParallel(ctx, client, filteredEntries, options, result)
	} else {
		rm.logger.Printf("Using sequential recovery mode")
		result = rm.recoverEntriesSequential(ctx, client, filteredEntries, options, result)
	}

	result.Success = result.EntriesError == 0
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	rm.logger.Printf("Recovery completed: success=%v, total=%d, ok=%d, error=%d, duration=%v",
		result.Success, result.EntriesTotal, result.EntriesOK, result.EntriesError, result.Duration)

	return result, nil
}

// recoverEntriesSequential 顺序恢复条目
func (rm *RecoveryManager) recoverEntriesSequential(ctx context.Context, client redis.Cmdable, entries []*MetadataEntry, options RecoveryOptions, result *RecoveryResult) *RecoveryResult {
	pipe := client.TxPipeline()
	var pipeCommands int
	const batchSize = 100

	for _, entry := range entries {
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
		if pipeCommands >= batchSize {
			if _, err := pipe.Exec(ctx); err != nil {
				rm.logger.Printf("Pipeline execution failed: %v", err)
			}
			pipe = client.TxPipeline()
			pipeCommands = 0
		}
	}

	// 执行剩余命令
	if pipeCommands > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			rm.logger.Printf("Final pipeline execution failed: %v", err)
		}
	}

	return result
}

// recoverEntriesParallel 并行恢复条目
func (rm *RecoveryManager) recoverEntriesParallel(ctx context.Context, client redis.Cmdable, entries []*MetadataEntry, options RecoveryOptions, result *RecoveryResult) *RecoveryResult {
	const (
		batchSize  = 100 // 每批处理100条
		numWorkers = 10  // 10个并发worker
	)

	// 将entries分批
	batches := rm.splitIntoBatches(entries, batchSize)
	rm.logger.Printf("Split %d entries into %d batches (batch_size=%d)", len(entries), len(batches), batchSize)

	// 创建任务通道和结果通道
	taskChan := make(chan []*MetadataEntry, len(batches))
	type batchResult struct {
		ok     int
		errors []string
	}
	resultChan := make(chan batchResult, len(batches))

	// 启动worker pool
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for batch := range taskChan {
				batchRes := rm.processBatch(ctx, client, batch, options, workerID)
				resultChan <- batchRes
			}
		}(i)
	}

	// 发送任务
	for _, batch := range batches {
		taskChan <- batch
	}
	close(taskChan)

	// 等待所有worker完成
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集结果
	for batchRes := range resultChan {
		result.EntriesOK += batchRes.ok
		result.ProcessedEntries += batchRes.ok
		result.EntriesError += len(batchRes.errors)
		result.ErrorEntries += len(batchRes.errors)
		result.Errors = append(result.Errors, batchRes.errors...)
	}

	rm.logger.Printf("Parallel recovery completed: ok=%d, errors=%d", result.EntriesOK, result.EntriesError)
	return result
}

// splitIntoBatches 将条目分批
func (rm *RecoveryManager) splitIntoBatches(entries []*MetadataEntry, batchSize int) [][]*MetadataEntry {
	var batches [][]*MetadataEntry

	for i := 0; i < len(entries); i += batchSize {
		end := i + batchSize
		if end > len(entries) {
			end = len(entries)
		}
		batches = append(batches, entries[i:end])
	}

	return batches
}

// processBatch 处理一批条目（幂等）
func (rm *RecoveryManager) processBatch(ctx context.Context, client redis.Cmdable, batch []*MetadataEntry, options RecoveryOptions, workerID int) struct {
	ok     int
	errors []string
} {
	result := struct {
		ok     int
		errors []string
	}{}

	// 为这一批创建pipeline
	pipe := client.TxPipeline()

	for _, entry := range batch {
		if err := rm.recoverEntry(ctx, pipe, entry, options, client); err != nil {
			result.errors = append(result.errors, fmt.Sprintf("Worker %d: Failed to recover %s: %v", workerID, entry.Key, err))
			continue
		}
		result.ok++
	}

	// 执行这一批的所有命令
	if result.ok > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			rm.logger.Printf("Worker %d: Batch execution failed: %v", workerID, err)
			// 如果批量执行失败，标记所有成功的为错误
			result.errors = append(result.errors, fmt.Sprintf("Worker %d: Batch exec failed for %d entries", workerID, result.ok))
			result.ok = 0
		} else {
			rm.logger.Printf("Worker %d: Successfully processed batch of %d entries", workerID, result.ok)
		}
	}

	return result
}

// downloadBackup 下载备份文件
func (rm *RecoveryManager) downloadBackup(ctx context.Context, objectName string) (*BackupSnapshot, error) {
	rm.logger.Printf("Downloading backup: %s", objectName)

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

	// 解析JSON
	var snapshot BackupSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to parse backup file: %w", err)
	}

	rm.logger.Printf("Downloaded backup: %s, entries: %d, timestamp: %v",
		objectName, len(snapshot.Entries), snapshot.Timestamp)

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

	// 1. 验证校验和（如果存在）
	if snapshot.Checksum != "" {
		rm.logger.Printf("Validating backup checksum: %s", snapshot.Checksum[:16]+"...")

		// 保存原始校验和
		originalChecksum := snapshot.Checksum

		// 创建不含校验和的副本用于验证
		snapshotForValidation := &BackupSnapshot{
			NodeID:    snapshot.NodeID,
			Timestamp: snapshot.Timestamp,
			Version:   snapshot.Version,
			Entries:   snapshot.Entries,
			// 不包含Checksum和Size，因为计算校验和时这些字段为空
		}

		// 序列化用于校验的数据
		dataForValidation, err := json.Marshal(snapshotForValidation)
		if err != nil {
			return fmt.Errorf("failed to marshal snapshot for validation: %w", err)
		}

		// 计算校验和
		hash := sha256.Sum256(dataForValidation)
		calculatedChecksum := hex.EncodeToString(hash[:])

		// 比较校验和
		if calculatedChecksum != originalChecksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s (backup may be corrupted or tampered)",
				originalChecksum[:16]+"...", calculatedChecksum[:16]+"...")
		}

		rm.logger.Printf("Checksum validation passed")
	} else {
		rm.logger.Printf("Warning: backup has no checksum, skipping checksum validation")
	}

	// 2. 基本验证
	if snapshot.NodeID == "" {
		return fmt.Errorf("backup missing node ID")
	}

	if snapshot.Timestamp.IsZero() {
		return fmt.Errorf("backup missing timestamp")
	}

	if len(snapshot.Entries) == 0 {
		return fmt.Errorf("backup contains no entries")
	}

	// 3. 验证条目
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

	rm.logger.Printf("Backup validation passed: %s, entries: %d", objectName, len(snapshot.Entries))
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
