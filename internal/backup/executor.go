package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/internal/metadata"
	"minIODB/internal/storage"
	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

const (
	BackupTypeFull     = "full"
	BackupTypeTable    = "table"
	BackupTypeMetadata = "metadata"
)

type FullBackupManifest struct {
	BackupID       string          `json:"backup_id"`
	Type           string          `json:"type"`
	NodeID         string          `json:"node_id"`
	Timestamp      time.Time       `json:"timestamp"`
	Tables         []TableManifest `json:"tables"`
	MetadataBackup string          `json:"metadata_backup"`
	Status         string          `json:"status"`
	FailedCount    int64           `json:"failed_count,omitempty"`
	Degraded       bool            `json:"degraded"`
	Errors         []string        `json:"errors,omitempty"`
}

type TableBackupManifest struct {
	BackupID       string    `json:"backup_id"`
	Type           string    `json:"type"`
	TableName      string    `json:"table_name"`
	NodeID         string    `json:"node_id"`
	Timestamp      time.Time `json:"timestamp"`
	ObjectCount    int64     `json:"object_count"`
	TotalSize      int64     `json:"total_size"`
	MetadataBackup string    `json:"metadata_backup,omitempty"`
	Status         string    `json:"status"`
	FailedCount    int64     `json:"failed_count,omitempty"`
	Degraded       bool      `json:"degraded"`
	Errors         []string  `json:"errors,omitempty"`
}

type TableManifest struct {
	TableName   string `json:"table_name"`
	ObjectCount int64  `json:"object_count"`
	TotalSize   int64  `json:"total_size"`
	Status      string `json:"status"`
	FailedCount int64  `json:"failed_count,omitempty"`
}

type BackupResult struct {
	BackupID     string   `json:"backup_id"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	ObjectCount  int64    `json:"object_count"`
	TotalSize    int64    `json:"total_size"`
	FailedCount  int64    `json:"failed_count,omitempty"`
	Duration     string   `json:"duration"`
	Degraded     bool     `json:"degraded"`
	Errors       []string `json:"errors,omitempty"`
	MetadataFile string   `json:"metadata_file,omitempty"`
}

type RestoreResult struct {
	BackupID    string   `json:"backup_id"`
	Status      string   `json:"status"`
	ObjectCount int64    `json:"object_count"`
	Duration    string   `json:"duration"`
	Errors      []string `json:"errors,omitempty"`
}

type Executor struct {
	primaryUploader storage.Uploader
	backupTarget    *BackupTarget
	metadataMgr     *metadata.Manager
	redisPool       *pool.RedisPool
	planStore       PlanStore
	cfg             *config.Config
	logger          *zap.Logger
	nodeID          string
}

func NewExecutor(
	primaryUploader storage.Uploader,
	backupTarget *BackupTarget,
	metadataMgr *metadata.Manager,
	redisPool *pool.RedisPool,
	planStore PlanStore,
	cfg *config.Config,
	logger *zap.Logger,
) *Executor {
	nodeID := "unknown"
	if metadataMgr != nil {
		nodeID = metadataMgr.GetNodeID()
	}

	return &Executor{
		primaryUploader: primaryUploader,
		backupTarget:    backupTarget,
		metadataMgr:     metadataMgr,
		redisPool:       redisPool,
		planStore:       planStore,
		cfg:             cfg,
		logger:          logger,
		nodeID:          nodeID,
	}
}

func (e *Executor) GetPlanStore() PlanStore {
	return e.planStore
}

func (e *Executor) FullBackup(ctx context.Context, backupID string, planID string) (*BackupResult, error) {
	startTime := time.Now()

	if e.backupTarget == nil {
		return nil, fmt.Errorf("backup target not available")
	}

	if backupID == "" {
		backupID = fmt.Sprintf("full-%s-%d", e.nodeID, startTime.Unix())
	}

	e.logger.Info("Starting full backup",
		zap.String("backup_id", backupID),
		zap.String("plan_id", planID),
		zap.Bool("degraded", e.backupTarget.Degraded))

	if err := e.backupTarget.EnsureBucket(ctx); err != nil {
		return nil, fmt.Errorf("ensure backup bucket: %w", err)
	}

	result := &BackupResult{
		BackupID: backupID,
		Type:     BackupTypeFull,
		Status:   "running",
		Degraded: e.backupTarget.Degraded,
	}

	execution := &BackupExecution{
		ID:        fmt.Sprintf("exec-%s-%d", backupID, startTime.UnixNano()),
		PlanID:    planID,
		Status:    ExecutionStatusRunning,
		StartTime: startTime,
	}

	if planID != "" && e.planStore != nil {
		if err := e.planStore.SaveExecution(ctx, execution); err != nil {
			e.logger.Warn("Failed to save initial execution record", zap.Error(err))
		}
	}

	var metadataBackupID string
	if e.metadataMgr != nil {
		if err := e.metadataMgr.ManualBackup(ctx); err != nil {
			e.logger.Warn("Metadata backup failed, continuing with data backup",
				zap.Error(err))
			result.Errors = append(result.Errors, fmt.Sprintf("metadata backup: %v", err))
		} else {
			metadataBackupID = fmt.Sprintf("metadata-%s-%d", e.nodeID, startTime.Unix())
		}
	}

	tables, err := e.listTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	manifest := &FullBackupManifest{
		BackupID:       backupID,
		Type:           BackupTypeFull,
		NodeID:         e.nodeID,
		Timestamp:      startTime,
		MetadataBackup: metadataBackupID,
		Status:         "running",
		Degraded:       e.backupTarget.Degraded,
	}

	var (
		totalObjects int64
		totalSize    int64
		totalFailed  int64
		wg           sync.WaitGroup
		mu           sync.Mutex
	)

	for _, tableName := range tables {
		wg.Add(1)
		go func(table string) {
			defer wg.Done()

			tableManifest, err := e.backupTable(ctx, table, backupID)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				e.logger.Warn("Table backup failed",
					zap.String("table", table),
					zap.Error(err))
				result.Errors = append(result.Errors, fmt.Sprintf("table %s: %v", table, err))
				manifest.Tables = append(manifest.Tables, TableManifest{
					TableName: table,
					Status:    "failed",
				})
				return
			}

			manifest.Tables = append(manifest.Tables, *tableManifest)
			totalObjects += tableManifest.ObjectCount
			totalSize += tableManifest.TotalSize
			totalFailed += tableManifest.FailedCount
		}(tableName)
	}
	wg.Wait()

	manifest.Status = "completed"
	if len(result.Errors) > 0 || totalFailed > 0 {
		manifest.Status = "partial"
	}
	manifest.FailedCount = totalFailed

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestKey := fmt.Sprintf("backups/%s/manifest.json", backupID)
	_, err = e.backupTarget.Uploader.PutObject(ctx, e.backupTarget.Bucket, manifestKey,
		toReader(manifestBytes), int64(len(manifestBytes)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
	if err != nil {
		return nil, fmt.Errorf("upload manifest: %w", err)
	}

	duration := time.Since(startTime)
	result.Status = manifest.Status
	result.ObjectCount = totalObjects
	result.TotalSize = totalSize
	result.FailedCount = totalFailed
	result.Duration = duration.String()
	result.MetadataFile = manifestKey

	e.logger.Info("Full backup completed",
		zap.String("backup_id", backupID),
		zap.String("status", result.Status),
		zap.Int64("objects", totalObjects),
		zap.Int64("size", totalSize),
		zap.Int64("failed", totalFailed),
		zap.String("duration", result.Duration))

	if planID != "" && e.planStore != nil {
		if manifest.Status == "partial" {
			execution.Status = ExecutionStatusFailed
		} else {
			execution.Status = ExecutionStatusCompleted
		}
		endTime := startTime.Add(duration)
		execution.EndTime = &endTime
		execution.Manifest = manifestKey
		if len(result.Errors) > 0 {
			execution.Error = result.Errors[0]
		}
		if err := e.planStore.SaveExecution(ctx, execution); err != nil {
			e.logger.Warn("Failed to save final execution record", zap.Error(err))
		}
	}

	if e.cfg != nil && e.cfg.Backup.Metadata.RetentionDays > 0 {
		if err := e.cleanupExpiredBackups(ctx, e.cfg.Backup.Metadata.RetentionDays); err != nil {
			e.logger.Warn("Failed to cleanup expired backups", zap.Error(err))
		}
	}

	return result, nil
}

func (e *Executor) TableBackup(ctx context.Context, tableName string, planID string) (*BackupResult, error) {
	startTime := time.Now()

	if e.backupTarget == nil {
		return nil, fmt.Errorf("backup target not available")
	}

	if tableName == "" {
		return nil, fmt.Errorf("table name is required")
	}

	backupID := fmt.Sprintf("table-%s-%s-%d", tableName, e.nodeID, startTime.Unix())

	e.logger.Info("Starting table backup",
		zap.String("backup_id", backupID),
		zap.String("table", tableName),
		zap.String("plan_id", planID),
		zap.Bool("degraded", e.backupTarget.Degraded))

	if err := e.backupTarget.EnsureBucket(ctx); err != nil {
		return nil, fmt.Errorf("ensure backup bucket: %w", err)
	}

	result := &BackupResult{
		BackupID: backupID,
		Type:     BackupTypeTable,
		Status:   "running",
		Degraded: e.backupTarget.Degraded,
	}

	execution := &BackupExecution{
		ID:        fmt.Sprintf("exec-%s-%d", backupID, startTime.UnixNano()),
		PlanID:    planID,
		Status:    ExecutionStatusRunning,
		StartTime: startTime,
	}

	if planID != "" && e.planStore != nil {
		if err := e.planStore.SaveExecution(ctx, execution); err != nil {
			e.logger.Warn("Failed to save initial execution record", zap.Error(err))
		}
	}

	var tableConfig *config.TableConfig
	if e.metadataMgr != nil {
		tc, err := e.metadataMgr.GetTableConfig(ctx, tableName)
		if err != nil {
			e.logger.Warn("Failed to get table config, using defaults",
				zap.String("table", tableName),
				zap.Error(err))
		} else {
			tableConfig = tc
			if tc != nil && tc.BackupEnabled != nil && !*tc.BackupEnabled {
				e.logger.Warn("Table backup is disabled in config, proceeding anyway",
					zap.String("table", tableName))
			}
		}
	}

	tableManifest, err := e.backupTable(ctx, tableName, backupID)
	if err != nil {
		return nil, fmt.Errorf("backup table: %w", err)
	}

	manifest := &TableBackupManifest{
		BackupID:    backupID,
		Type:        BackupTypeTable,
		TableName:   tableName,
		NodeID:      e.nodeID,
		Timestamp:   startTime,
		ObjectCount: tableManifest.ObjectCount,
		TotalSize:   tableManifest.TotalSize,
		Status:      tableManifest.Status,
		FailedCount: tableManifest.FailedCount,
		Degraded:    e.backupTarget.Degraded,
	}

	if tableConfig != nil {
		configKey := fmt.Sprintf("backups/%s/table-config.json", backupID)
		configBytes, err := json.MarshalIndent(tableConfig, "", "  ")
		if err != nil {
			e.logger.Warn("Failed to marshal table config", zap.Error(err))
			result.Errors = append(result.Errors, fmt.Sprintf("marshal table config: %v", err))
		} else {
			_, err = e.backupTarget.Uploader.PutObject(ctx, e.backupTarget.Bucket, configKey,
				toReader(configBytes), int64(len(configBytes)), minio.PutObjectOptions{
					ContentType: "application/json",
				})
			if err != nil {
				e.logger.Warn("Failed to upload table config", zap.Error(err))
				result.Errors = append(result.Errors, fmt.Sprintf("upload table config: %v", err))
			}
		}
	}

	if e.redisPool != nil {
		redisMetadata, err := e.collectTableRedisMetadata(ctx, tableName)
		if err != nil {
			e.logger.Warn("Failed to collect Redis metadata",
				zap.String("table", tableName),
				zap.Error(err))
			result.Errors = append(result.Errors, fmt.Sprintf("redis metadata: %v", err))
		} else if len(redisMetadata) > 0 {
			metadataKey := fmt.Sprintf("backups/%s/redis-metadata.json", backupID)
			metadataBytes, err := json.MarshalIndent(redisMetadata, "", "  ")
			if err != nil {
				e.logger.Warn("Failed to marshal Redis metadata", zap.Error(err))
				result.Errors = append(result.Errors, fmt.Sprintf("marshal redis metadata: %v", err))
			} else {
				_, err = e.backupTarget.Uploader.PutObject(ctx, e.backupTarget.Bucket, metadataKey,
					toReader(metadataBytes), int64(len(metadataBytes)), minio.PutObjectOptions{
						ContentType: "application/json",
					})
				if err != nil {
					e.logger.Warn("Failed to upload Redis metadata", zap.Error(err))
					result.Errors = append(result.Errors, fmt.Sprintf("upload redis metadata: %v", err))
				}
			}
		}
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestKey := fmt.Sprintf("backups/%s/manifest.json", backupID)
	_, err = e.backupTarget.Uploader.PutObject(ctx, e.backupTarget.Bucket, manifestKey,
		toReader(manifestBytes), int64(len(manifestBytes)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
	if err != nil {
		return nil, fmt.Errorf("upload manifest: %w", err)
	}

	duration := time.Since(startTime)
	result.Status = tableManifest.Status
	result.ObjectCount = tableManifest.ObjectCount
	result.TotalSize = tableManifest.TotalSize
	result.FailedCount = tableManifest.FailedCount
	result.Duration = duration.String()
	result.MetadataFile = manifestKey

	e.logger.Info("Table backup completed",
		zap.String("backup_id", backupID),
		zap.String("table", tableName),
		zap.Int64("objects", result.ObjectCount),
		zap.Int64("size", result.TotalSize),
		zap.Int64("failed", result.FailedCount),
		zap.String("status", result.Status),
		zap.String("duration", result.Duration))

	if planID != "" && e.planStore != nil {
		if tableManifest.Status == "partial" {
			execution.Status = ExecutionStatusFailed
		} else {
			execution.Status = ExecutionStatusCompleted
		}
		endTime := startTime.Add(duration)
		execution.EndTime = &endTime
		execution.Manifest = manifestKey
		if err := e.planStore.SaveExecution(ctx, execution); err != nil {
			e.logger.Warn("Failed to save final execution record", zap.Error(err))
		}
	}

	var retentionDays int
	if tableConfig != nil && tableConfig.RetentionDays > 0 {
		retentionDays = tableConfig.RetentionDays
	} else if e.cfg != nil && e.cfg.Backup.Metadata.RetentionDays > 0 {
		retentionDays = e.cfg.Backup.Metadata.RetentionDays
	}
	if retentionDays > 0 {
		if err := e.cleanupExpiredTableBackups(ctx, tableName, retentionDays); err != nil {
			e.logger.Warn("Failed to cleanup expired table backups",
				zap.String("table", tableName),
				zap.Error(err))
		}
	}

	return result, nil
}

func (e *Executor) MetadataBackup(ctx context.Context, planID string) (*BackupResult, error) {
	startTime := time.Now()
	backupID := fmt.Sprintf("metadata-%s-%d", e.nodeID, startTime.Unix())

	e.logger.Info("Starting metadata backup",
		zap.String("backup_id", backupID),
		zap.String("plan_id", planID))

	if e.metadataMgr == nil {
		return nil, fmt.Errorf("metadata manager not available")
	}

	if err := e.backupTarget.EnsureBucket(ctx); err != nil {
		return nil, fmt.Errorf("ensure backup bucket: %w", err)
	}

	result := &BackupResult{
		BackupID: backupID,
		Type:     BackupTypeMetadata,
		Status:   "running",
		Degraded: e.backupTarget.Degraded,
	}

	execution := &BackupExecution{
		ID:        fmt.Sprintf("exec-%s-%d", backupID, startTime.UnixNano()),
		PlanID:    planID,
		Status:    ExecutionStatusRunning,
		StartTime: startTime,
	}

	if planID != "" && e.planStore != nil {
		if err := e.planStore.SaveExecution(ctx, execution); err != nil {
			e.logger.Warn("Failed to save initial execution record", zap.Error(err))
		}
	}

	if err := e.metadataMgr.ManualBackup(ctx); err != nil {
		e.logger.Error("Metadata backup failed",
			zap.String("backup_id", backupID),
			zap.Error(err))
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())

		if planID != "" && e.planStore != nil {
			execution.Status = ExecutionStatusFailed
			execution.Error = err.Error()
			endTime := time.Now()
			execution.EndTime = &endTime
			if saveErr := e.planStore.SaveExecution(ctx, execution); saveErr != nil {
				e.logger.Warn("Failed to save failed execution record", zap.Error(saveErr))
			}
		}

		return result, fmt.Errorf("metadata backup failed: %w", err)
	}

	duration := time.Since(startTime)
	result.Status = "completed"
	result.Duration = duration.String()

	e.logger.Info("Metadata backup completed",
		zap.String("backup_id", backupID),
		zap.String("status", result.Status),
		zap.String("duration", result.Duration))

	if planID != "" && e.planStore != nil {
		execution.Status = ExecutionStatusCompleted
		endTime := time.Now()
		execution.EndTime = &endTime
		if err := e.planStore.SaveExecution(ctx, execution); err != nil {
			e.logger.Warn("Failed to save final execution record", zap.Error(err))
		}
	}

	return result, nil
}

func (e *Executor) Restore(ctx context.Context, backupID string) (*RestoreResult, error) {
	startTime := time.Now()

	if e.backupTarget == nil {
		return nil, fmt.Errorf("backup target not available")
	}

	if backupID == "" {
		return nil, fmt.Errorf("backup ID is required")
	}

	e.logger.Info("Starting restore",
		zap.String("backup_id", backupID))

	result := &RestoreResult{
		BackupID: backupID,
		Status:   "running",
	}

	manifestKey := fmt.Sprintf("backups/%s/manifest.json", backupID)
	manifestData, err := e.backupTarget.Uploader.GetObject(ctx, e.backupTarget.Bucket, manifestKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest FullBackupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if manifest.MetadataBackup != "" && e.metadataMgr != nil {
		recoveryMgr := e.metadataMgr.GetRecoveryManager()
		if recoveryMgr != nil {
			opts := metadata.RecoveryOptions{
				Mode:      metadata.RecoveryModeComplete,
				Overwrite: true,
				Validate:  true,
				DryRun:    false,
			}
			_, err := recoveryMgr.RecoverFromBackup(ctx, manifest.MetadataBackup, opts)
			if err != nil {
				e.logger.Warn("Metadata restore failed", zap.Error(err))
				result.Errors = append(result.Errors, fmt.Sprintf("metadata restore: %v", err))
			}
		}
	}

	var (
		totalObjects int64
		wg           sync.WaitGroup
		mu           sync.Mutex
	)

	for _, tableManifest := range manifest.Tables {
		if tableManifest.Status == "failed" {
			continue
		}

		wg.Add(1)
		go func(tm TableManifest) {
			defer wg.Done()

			count, err := e.restoreTable(ctx, tm.TableName, backupID)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				e.logger.Warn("Table restore failed",
					zap.String("table", tm.TableName),
					zap.Error(err))
				result.Errors = append(result.Errors, fmt.Sprintf("table %s: %v", tm.TableName, err))
				return
			}
			totalObjects += count
		}(tableManifest)
	}
	wg.Wait()

	duration := time.Since(startTime)
	result.Status = "completed"
	if len(result.Errors) > 0 {
		result.Status = "partial"
	}
	result.ObjectCount = totalObjects
	result.Duration = duration.String()

	e.logger.Info("Restore completed",
		zap.String("backup_id", backupID),
		zap.String("status", result.Status),
		zap.Int64("objects", result.ObjectCount),
		zap.String("duration", result.Duration))

	return result, nil
}

func (e *Executor) backupTable(ctx context.Context, tableName, backupID string) (*TableManifest, error) {
	if e.primaryUploader == nil {
		return nil, fmt.Errorf("primary uploader not available")
	}

	primaryBucket := e.cfg.GetMinIO().Bucket
	prefix := fmt.Sprintf("%s/", tableName)

	objects, err := e.primaryUploader.ListObjectsSimple(ctx, primaryBucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}

	manifest := &TableManifest{
		TableName:   tableName,
		ObjectCount: 0,
		TotalSize:   0,
		Status:      "completed",
	}

	var copiedCount, failedCount int64

	for _, obj := range objects {
		destKey := fmt.Sprintf("backups/%s/data/%s", backupID, obj.Name)

		_, err := e.backupTarget.Uploader.CopyObject(ctx,
			minio.CopyDestOptions{
				Bucket: e.backupTarget.Bucket,
				Object: destKey,
			},
			minio.CopySrcOptions{
				Bucket: primaryBucket,
				Object: obj.Name,
			})
		if err != nil {
			e.logger.Warn("Failed to copy object",
				zap.String("object", obj.Name),
				zap.Error(err))
			failedCount++
			continue
		}

		copiedCount++
		manifest.TotalSize += obj.Size
	}

	manifest.ObjectCount = copiedCount

	if failedCount > 0 {
		manifest.Status = "partial"
		manifest.FailedCount = failedCount
	} else {
		manifest.Status = "completed"
	}

	return manifest, nil
}

func (e *Executor) restoreTable(ctx context.Context, tableName, backupID string) (int64, error) {
	if e.primaryUploader == nil {
		return 0, fmt.Errorf("primary uploader not available")
	}

	primaryBucket := e.cfg.GetMinIO().Bucket
	prefix := fmt.Sprintf("backups/%s/data/%s/", backupID, tableName)

	objects, err := e.backupTarget.Uploader.ListObjectsSimple(ctx, e.backupTarget.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	if err != nil {
		return 0, fmt.Errorf("list backup objects: %w", err)
	}

	var count int64
	for _, obj := range objects {
		originalKey := obj.Name[len(fmt.Sprintf("backups/%s/data/", backupID)):]

		_, err := e.primaryUploader.CopyObject(ctx,
			minio.CopyDestOptions{
				Bucket: primaryBucket,
				Object: originalKey,
			},
			minio.CopySrcOptions{
				Bucket: e.backupTarget.Bucket,
				Object: obj.Name,
			})
		if err != nil {
			e.logger.Warn("Failed to restore object",
				zap.String("object", obj.Name),
				zap.Error(err))
			continue
		}
		count++
	}

	return count, nil
}

func (e *Executor) listTables(ctx context.Context) ([]string, error) {
	if e.primaryUploader == nil {
		return nil, fmt.Errorf("primary uploader not available")
	}

	primaryBucket := e.cfg.GetMinIO().Bucket

	objects, err := e.primaryUploader.ListObjectsSimple(ctx, primaryBucket, minio.ListObjectsOptions{
		Recursive: false,
	})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}

	tableSet := make(map[string]struct{})
	for _, obj := range objects {
		for i, c := range obj.Name {
			if c == '/' {
				tableSet[obj.Name[:i]] = struct{}{}
				break
			}
		}
	}

	tables := make([]string, 0, len(tableSet))
	for table := range tableSet {
		tables = append(tables, table)
	}

	return tables, nil
}

func (e *Executor) collectTableRedisMetadata(ctx context.Context, tableName string) (map[string]string, error) {
	if e.redisPool == nil {
		return nil, fmt.Errorf("redis pool not available")
	}

	client := e.redisPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("redis client not available")
	}

	pattern := fmt.Sprintf("table:%s:*", tableName)
	metadata := make(map[string]string)

	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("scan keys: %w", err)
		}

		for _, key := range keys {
			val, err := client.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			metadata[key] = val
		}

		if cursor == 0 {
			break
		}
	}

	return metadata, nil
}

func (e *Executor) cleanupExpiredBackups(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	objects, err := e.backupTarget.Uploader.ListObjectsSimple(ctx, e.backupTarget.Bucket, minio.ListObjectsOptions{
		Prefix:    "backups/",
		Recursive: true,
	})
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}

	backupDirs := make(map[string]time.Time)
	for _, obj := range objects {
		parts := bytes.Split([]byte(obj.Name), []byte("/"))
		if len(parts) >= 2 {
			backupDir := string(parts[0]) + "/" + string(parts[1])
			if _, exists := backupDirs[backupDir]; !exists || obj.LastModified.After(backupDirs[backupDir]) {
				backupDirs[backupDir] = obj.LastModified
			}
		}
	}

	for backupDir, lastModified := range backupDirs {
		if lastModified.Before(cutoff) {
			if err := e.removeBackupDirectory(ctx, backupDir+"/"); err != nil {
				e.logger.Warn("Failed to remove expired backup directory",
					zap.String("backup", backupDir),
					zap.Error(err))
			} else {
				e.logger.Info("Removed expired backup directory",
					zap.String("backup", backupDir))
			}
		}
	}

	return nil
}

func (e *Executor) cleanupExpiredTableBackups(ctx context.Context, tableName string, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	prefix := fmt.Sprintf("backups/table-%s-", tableName)
	objects, err := e.backupTarget.Uploader.ListObjectsSimple(ctx, e.backupTarget.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	})
	if err != nil {
		return fmt.Errorf("list table backups: %w", err)
	}

	for _, obj := range objects {
		if obj.LastModified.Before(cutoff) {
			if err := e.removeBackupDirectory(ctx, obj.Name); err != nil {
				e.logger.Warn("Failed to remove expired table backup",
					zap.String("backup", obj.Name),
					zap.Error(err))
			} else {
				e.logger.Info("Removed expired table backup",
					zap.String("backup", obj.Name))
			}
		}
	}

	return nil
}

func (e *Executor) removeBackupDirectory(ctx context.Context, backupPrefix string) error {
	objects, err := e.backupTarget.Uploader.ListObjectsSimple(ctx, e.backupTarget.Bucket, minio.ListObjectsOptions{
		Prefix:    backupPrefix,
		Recursive: true,
	})
	if err != nil {
		return fmt.Errorf("list backup objects: %w", err)
	}

	for _, obj := range objects {
		if err := e.backupTarget.Uploader.RemoveObject(ctx, e.backupTarget.Bucket, obj.Name, minio.RemoveObjectOptions{}); err != nil {
			e.logger.Warn("Failed to remove backup object",
				zap.String("object", obj.Name),
				zap.Error(err))
		}
	}

	return nil
}

func (e *Executor) GetNodeID() string {
	return e.nodeID
}

func (e *Executor) IsDegraded() bool {
	return e.backupTarget != nil && e.backupTarget.Degraded
}

func toReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
