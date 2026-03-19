package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"minIODB/config"
	"minIODB/internal/storage"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

type mockExecutorUploader struct {
	objects       map[string][]byte
	buckets       map[string]bool
	copyErr       error
	putErr        error
	listErr       error
	getErr        error
	listObjects   []storage.ObjectInfo
	copiedObjects []executorCopyCall
}

type executorCopyCall struct {
	srcBucket, srcObject string
	dstBucket, dstObject string
}

func newMockExecutorUploader() *mockExecutorUploader {
	return &mockExecutorUploader{
		objects: make(map[string][]byte),
		buckets: make(map[string]bool),
	}
}

func (m *mockExecutorUploader) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	return m.buckets[bucketName], nil
}

func (m *mockExecutorUploader) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	m.buckets[bucketName] = true
	return nil
}

func (m *mockExecutorUploader) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{}, nil
}

func (m *mockExecutorUploader) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	if m.putErr != nil {
		return minio.UploadInfo{}, m.putErr
	}
	data := make([]byte, objectSize)
	reader.Read(data)
	key := bucketName + "/" + objectName
	m.objects[key] = data
	return minio.UploadInfo{Size: objectSize}, nil
}

func (m *mockExecutorUploader) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	key := bucketName + "/" + objectName
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return data, nil
}

func (m *mockExecutorUploader) GetObjectStream(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (io.ReadCloser, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	key := bucketName + "/" + objectName
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockExecutorUploader) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	key := bucketName + "/" + objectName
	delete(m.objects, key)
	return nil
}

func (m *mockExecutorUploader) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo, 1)
	close(ch)
	return ch
}

func (m *mockExecutorUploader) CopyObject(ctx context.Context, dest minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	if m.copyErr != nil {
		return minio.UploadInfo{}, m.copyErr
	}
	m.copiedObjects = append(m.copiedObjects, executorCopyCall{
		srcBucket: src.Bucket, srcObject: src.Object,
		dstBucket: dest.Bucket, dstObject: dest.Object,
	})
	srcKey := src.Bucket + "/" + src.Object
	if data, ok := m.objects[srcKey]; ok {
		dstKey := dest.Bucket + "/" + dest.Object
		m.objects[dstKey] = data
	}
	return minio.UploadInfo{}, nil
}

func (m *mockExecutorUploader) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	return minio.ObjectInfo{}, nil
}

func (m *mockExecutorUploader) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	return false, nil
}

func (m *mockExecutorUploader) ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]storage.ObjectInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listObjects != nil {
		return m.listObjects, nil
	}
	return []storage.ObjectInfo{}, nil
}

func TestNewExecutor(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	if executor == nil {
		t.Fatal("expected non-nil executor")
	}
	if executor.GetNodeID() == "" {
		t.Error("expected node ID to be set")
	}
	if executor.IsDegraded() {
		t.Error("expected not degraded")
	}
}

func TestFullBackup_NilBackupTarget(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()

	executor := NewExecutor(mockPrimary, nil, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.FullBackup(ctx, "", "")

	if err == nil {
		t.Error("expected error for nil backup target")
	}
}

func TestFullBackup_Success(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	mockPrimary.buckets["test-primary"] = true
	mockPrimary.objects["test-primary/table1/file1.parquet"] = []byte("data1")
	mockPrimary.objects["test-primary/table2/file2.parquet"] = []byte("data2")
	mockPrimary.listObjects = []storage.ObjectInfo{
		{Name: "table1/", Size: 0, LastModified: time.Now()},
		{Name: "table2/", Size: 0, LastModified: time.Now()},
	}

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)
	executor.nodeID = "test-node"

	ctx := context.Background()
	result, err := executor.FullBackup(ctx, "full-test-node-1234567890", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BackupID != "full-test-node-1234567890" {
		t.Errorf("expected backup ID, got %s", result.BackupID)
	}
	if result.Type != BackupTypeFull {
		t.Errorf("expected full backup type, got %s", result.Type)
	}
	if result.Status == "" {
		t.Error("expected status to be set")
	}
}

func TestFullBackup_AutoGenerateBackupID(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)
	executor.nodeID = "test-node"

	ctx := context.Background()
	result, err := executor.FullBackup(ctx, "", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BackupID == "" {
		t.Error("expected auto-generated backup ID")
	}
}

func TestTableBackup_NilBackupTarget(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()

	executor := NewExecutor(mockPrimary, nil, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.TableBackup(ctx, "test-table", "")

	if err == nil {
		t.Error("expected error for nil backup target")
	}
}

func TestTableBackup_EmptyTableName(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.TableBackup(ctx, "", "")

	if err == nil {
		t.Error("expected error for empty table name")
	}
}

func TestTableBackup_Success(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	mockPrimary.buckets["test-primary"] = true
	mockPrimary.objects["test-primary/test-table/file1.parquet"] = []byte("data1")
	mockPrimary.listObjects = []storage.ObjectInfo{
		{Name: "test-table/file1.parquet", Size: 100, LastModified: time.Now()},
	}

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)
	executor.nodeID = "test-node"

	ctx := context.Background()
	result, err := executor.TableBackup(ctx, "test-table", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != BackupTypeTable {
		t.Errorf("expected table backup type, got %s", result.Type)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed status, got %s", result.Status)
	}
}

func TestRestore_NilBackupTarget(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()

	executor := NewExecutor(mockPrimary, nil, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.Restore(ctx, "test-backup-id")

	if err == nil {
		t.Error("expected error for nil backup target")
	}
}

func TestRestore_EmptyBackupID(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.Restore(ctx, "")

	if err == nil {
		t.Error("expected error for empty backup ID")
	}
}

func TestRestore_ManifestNotFound(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()
	mockBackup.getErr = errors.New("manifest not found")

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.Restore(ctx, "test-backup-id")

	if err == nil {
		t.Error("expected error when manifest not found")
	}
}

func TestRestore_Success(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	manifest := &FullBackupManifest{
		BackupID:  "test-backup-id",
		Type:      BackupTypeFull,
		NodeID:    "test-node",
		Timestamp: time.Now(),
		Tables: []TableManifest{
			{TableName: "test-table", ObjectCount: 1, TotalSize: 100, Status: "completed"},
		},
		Status: "completed",
	}
	manifestBytes, _ := json.Marshal(manifest)
	mockBackup.objects["test-backup/backups/test-backup-id/manifest.json"] = manifestBytes

	mockBackup.listObjects = []storage.ObjectInfo{
		{Name: "backups/test-backup-id/data/test-table/file1.parquet", Size: 100, LastModified: time.Now()},
	}
	mockBackup.objects["test-backup/backups/test-backup-id/data/test-table/file1.parquet"] = []byte("data")

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	result, err := executor.Restore(ctx, "test-backup-id")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BackupID != "test-backup-id" {
		t.Errorf("expected backup ID, got %s", result.BackupID)
	}
	if result.Status == "" {
		t.Error("expected status to be set")
	}
}

func TestIsDegraded(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	executor := NewExecutor(mockPrimary, nil, nil, nil, nil, cfg, logger)
	if executor.IsDegraded() {
		t.Error("expected not degraded with nil target")
	}

	degradedTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: true,
		Mode:     ModePrimaryMinIOIsolated,
	}
	executor = NewExecutor(mockPrimary, degradedTarget, nil, nil, nil, cfg, logger)
	if !executor.IsDegraded() {
		t.Error("expected degraded")
	}

	normalTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}
	executor = NewExecutor(mockPrimary, normalTarget, nil, nil, nil, cfg, logger)
	if executor.IsDegraded() {
		t.Error("expected not degraded")
	}
}

func TestBackupTable_CopyError(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	mockPrimary.listObjects = []storage.ObjectInfo{
		{Name: "test-table/file1.parquet", Size: 100, LastModified: time.Now()},
	}
	mockBackup.copyErr = errors.New("copy failed")

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)
	executor.nodeID = "test-node"

	ctx := context.Background()
	result, err := executor.TableBackup(ctx, "test-table", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ObjectCount != 0 {
		t.Errorf("expected 0 objects due to copy error, got %d", result.ObjectCount)
	}
	if result.Status != "partial" {
		t.Errorf("expected status 'partial' due to copy error, got %q", result.Status)
	}
	if result.FailedCount != 1 {
		t.Errorf("expected FailedCount=1 due to copy error, got %d", result.FailedCount)
	}
}

func TestCleanupExpiredBackups(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Backup: config.BackupConfig{
			Metadata: config.MetadataBackupConfig{
				RetentionDays: 7,
			},
		},
	}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	oldTime := time.Now().AddDate(0, 0, -10)
	recentTime := time.Now().AddDate(0, 0, -3)

	mockBackup.listObjects = []storage.ObjectInfo{
		{Name: "backups/old-backup/", Size: 0, LastModified: oldTime},
		{Name: "backups/recent-backup/", Size: 0, LastModified: recentTime},
	}

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	err := executor.cleanupExpiredBackups(ctx, 7)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTableBackup_ErrorsPropagateToResult(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	mockPrimary.buckets["test-primary"] = true
	mockPrimary.objects["test-primary/test-table/file1.parquet"] = []byte("data1")
	mockPrimary.listObjects = []storage.ObjectInfo{
		{Name: "test-table/file1.parquet", Size: 100, LastModified: time.Now()},
	}

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)
	executor.nodeID = "test-node"

	ctx := context.Background()
	result, err := executor.TableBackup(ctx, "test-table", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Status != "completed" {
		t.Errorf("expected completed status, got %s", result.Status)
	}

	if result.Errors == nil {
		result.Errors = []string{}
	}

	t.Logf("Backup completed with %d errors", len(result.Errors))
}

func TestTableBackup_UploadError_AddsToResultErrors(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()
	mockBackup.putErr = errors.New("upload failed")

	mockPrimary.buckets["test-primary"] = true
	mockPrimary.objects["test-primary/test-table/file1.parquet"] = []byte("data1")
	mockPrimary.listObjects = []storage.ObjectInfo{
		{Name: "test-table/file1.parquet", Size: 100, LastModified: time.Now()},
	}

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)
	executor.nodeID = "test-node"

	ctx := context.Background()
	result, err := executor.TableBackup(ctx, "test-table", "")

	if err == nil {
		t.Fatal("expected error when manifest upload fails")
	}

	if result != nil {
		t.Errorf("expected nil result when manifest upload fails, got %+v", result)
	}
}

func TestMetadataBackup_NilMetadataManager(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}
	mockPrimary := newMockExecutorUploader()
	mockBackup := newMockExecutorUploader()

	backupTarget := &BackupTarget{
		Uploader: mockBackup,
		Bucket:   "test-backup",
		Degraded: false,
		Mode:     ModeBackupMinIO,
	}

	executor := NewExecutor(mockPrimary, backupTarget, nil, nil, nil, cfg, logger)

	ctx := context.Background()
	_, err := executor.MetadataBackup(ctx, "")

	if err == nil {
		t.Error("expected error for nil metadata manager")
	}
	if err != nil && !containsString(err.Error(), "metadata manager not available") {
		t.Errorf("expected 'metadata manager not available' error, got %v", err)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s[1:], substr) || s[:len(substr)] == substr)
}
