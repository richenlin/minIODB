package backup

import (
	"context"
	"fmt"

	"minIODB/config"
	"minIODB/internal/storage"
	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

const (
	ModeBackupMinIO          = "backup_minio"
	ModePrimaryMinIOIsolated = "primary_minio_isolated"
)

type MinIOClientProvider interface {
	GetMinIOPool() *pool.MinIOPool
	GetBackupMinIOPool() *pool.MinIOPool
}

type BackupTarget struct {
	Uploader storage.Uploader
	Bucket   string
	Degraded bool
	Mode     string
}

func ResolveBackupTarget(cfg *config.Config, poolManager MinIOClientProvider, logger *zap.Logger) *BackupTarget {
	backupPool := poolManager.GetBackupMinIOPool()
	backupCfg := cfg.GetBackupMinIO()

	if backupPool != nil && backupCfg.Endpoint != "" {
		if logger != nil {
			logger.Info("Using backup MinIO for backup target",
				zap.String("bucket", backupCfg.Bucket),
				zap.String("endpoint", backupCfg.Endpoint))
		}
		uploader, err := storage.NewMinioClientWrapperFromClient(backupPool.GetClient(), logger)
		if err != nil {
			if logger != nil {
				logger.Error("Failed to create uploader from backup pool", zap.Error(err))
			}
			return nil
		}
		return &BackupTarget{
			Uploader: uploader,
			Bucket:   backupCfg.Bucket,
			Degraded: false,
			Mode:     ModeBackupMinIO,
		}
	}

	primaryPool := poolManager.GetMinIOPool()
	if primaryPool == nil {
		if logger != nil {
			logger.Error("Both backup MinIO and primary MinIO pools are unavailable")
		}
		return nil
	}

	primaryCfg := cfg.GetMinIO()
	degradedBucket := primaryCfg.Bucket + "-backups"

	if logger != nil {
		logger.Warn("Backup MinIO not available, degrading to primary MinIO",
			zap.String("bucket", degradedBucket),
			zap.String("endpoint", primaryCfg.Endpoint))
	}

	uploader, err := storage.NewMinioClientWrapperFromClient(primaryPool.GetClient(), logger)
	if err != nil {
		if logger != nil {
			logger.Error("Failed to create uploader from primary pool", zap.Error(err))
		}
		return nil
	}
	return &BackupTarget{
		Uploader: uploader,
		Bucket:   degradedBucket,
		Degraded: true,
		Mode:     ModePrimaryMinIOIsolated,
	}
}

func (t *BackupTarget) EnsureBucket(ctx context.Context) error {
	exists, err := t.Uploader.BucketExists(ctx, t.Bucket)
	if err != nil {
		return fmt.Errorf("check bucket existence: %w", err)
	}
	if !exists {
		if err := t.Uploader.MakeBucket(ctx, t.Bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", t.Bucket, err)
		}
	}
	return nil
}

func (t *BackupTarget) String() string {
	degradedStr := ""
	if t.Degraded {
		degradedStr = " (degraded)"
	}
	return fmt.Sprintf("BackupTarget{bucket=%s, mode=%s%s}", t.Bucket, t.Mode, degradedStr)
}
