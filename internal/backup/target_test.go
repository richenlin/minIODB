package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"minIODB/config"
	"minIODB/internal/storage"
	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
)

type mockUploader struct {
	bucketExists    bool
	bucketExistsErr error
	makeBucketErr   error
}

func (m *mockUploader) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	return m.bucketExists, m.bucketExistsErr
}

func (m *mockUploader) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	return m.makeBucketErr
}

func (m *mockUploader) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{}, nil
}

func (m *mockUploader) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{}, nil
}

func (m *mockUploader) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error) {
	return nil, nil
}

func (m *mockUploader) GetObjectStream(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (m *mockUploader) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	return nil
}

func (m *mockUploader) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)
	close(ch)
	return ch
}

func (m *mockUploader) CopyObject(ctx context.Context, dest minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{}, nil
}

func (m *mockUploader) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return minio.ObjectInfo{}, nil
}

func (m *mockUploader) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	return false, nil
}

func (m *mockUploader) ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]storage.ObjectInfo, error) {
	return nil, nil
}

func TestBackupTarget_EnsureBucket_BucketExists(t *testing.T) {
	mock := &mockUploader{bucketExists: true}
	target := &BackupTarget{
		Uploader: mock,
		Bucket:   "test-bucket",
	}

	err := target.EnsureBucket(context.Background())
	assert.NoError(t, err)
}

func TestBackupTarget_EnsureBucket_CreateBucket(t *testing.T) {
	mock := &mockUploader{bucketExists: false}
	target := &BackupTarget{
		Uploader: mock,
		Bucket:   "test-bucket",
	}

	err := target.EnsureBucket(context.Background())
	assert.NoError(t, err)
}

func TestBackupTarget_EnsureBucket_BucketExistsError(t *testing.T) {
	mock := &mockUploader{
		bucketExists:    false,
		bucketExistsErr: errors.New("connection error"),
	}
	target := &BackupTarget{
		Uploader: mock,
		Bucket:   "test-bucket",
	}

	err := target.EnsureBucket(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check bucket existence")
}

func TestBackupTarget_EnsureBucket_MakeBucketError(t *testing.T) {
	mock := &mockUploader{
		bucketExists:  false,
		makeBucketErr: errors.New("permission denied"),
	}
	target := &BackupTarget{
		Uploader: mock,
		Bucket:   "test-bucket",
	}

	err := target.EnsureBucket(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create bucket")
}

func TestBackupTarget_String(t *testing.T) {
	tests := []struct {
		name     string
		target   *BackupTarget
		expected string
	}{
		{
			name: "normal mode",
			target: &BackupTarget{
				Bucket:   "backup-bucket",
				Mode:     ModeBackupMinIO,
				Degraded: false,
			},
			expected: "BackupTarget{bucket=backup-bucket, mode=backup_minio}",
		},
		{
			name: "degraded mode",
			target: &BackupTarget{
				Bucket:   "primary-bucket-backups",
				Mode:     ModePrimaryMinIOIsolated,
				Degraded: true,
			},
			expected: "BackupTarget{bucket=primary-bucket-backups, mode=primary_minio_isolated (degraded)}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.target.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBackupTarget_DegradedBucketName(t *testing.T) {
	cfg := &config.Config{}
	cfg.Network.Pools.MinIO = config.EnhancedMinIOConfig{
		Bucket: "mydata",
	}
	cfg.Network.Pools.BackupMinIO = nil

	primaryCfg := cfg.GetMinIO()
	degradedBucket := primaryCfg.Bucket + "-backups"

	assert.Equal(t, "mydata-backups", degradedBucket)
}

func TestBackupTarget_ModeConstants(t *testing.T) {
	assert.Equal(t, "backup_minio", ModeBackupMinIO)
	assert.Equal(t, "primary_minio_isolated", ModePrimaryMinIOIsolated)
}

type mockMinIOClientProvider struct {
	backupPool  *pool.MinIOPool
	primaryPool *pool.MinIOPool
}

func (m *mockMinIOClientProvider) GetMinIOPool() *pool.MinIOPool {
	return m.primaryPool
}

func (m *mockMinIOClientProvider) GetBackupMinIOPool() *pool.MinIOPool {
	return m.backupPool
}

func TestResolveBackupTarget_BothPoolsNil(t *testing.T) {
	cfg := &config.Config{}
	cfg.Network.Pools.MinIO = config.EnhancedMinIOConfig{
		Bucket: "test-bucket",
	}
	cfg.Network.Pools.BackupMinIO = nil

	provider := &mockMinIOClientProvider{
		backupPool:  nil,
		primaryPool: nil,
	}

	result := ResolveBackupTarget(cfg, provider, nil)
	assert.Nil(t, result)
}
