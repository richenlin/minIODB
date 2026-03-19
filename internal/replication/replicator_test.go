package replication

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"minIODB/config"
	"minIODB/internal/metrics"
	"minIODB/internal/storage"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockUploader struct {
	objects     map[string]minio.ObjectInfo
	copyCalls   []string
	putCalls    []string
	getCalls    []string
	streamCalls []string
	mu          sync.Mutex
	copyErr     error
	getErr      error
	putErr      error
	listErr     error
	statErr     error
	copyDelay   time.Duration
}

func newMockUploader() *mockUploader {
	return &mockUploader{
		objects: make(map[string]minio.ObjectInfo),
	}
}

func (m *mockUploader) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	return true, nil
}

func (m *mockUploader) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	return nil
}

func (m *mockUploader) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{}, nil
}

func (m *mockUploader) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return minio.UploadInfo{}, m.putErr
	}
	m.putCalls = append(m.putCalls, objectName)
	return minio.UploadInfo{}, nil
}

func (m *mockUploader) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.getCalls = append(m.getCalls, objectName)
	return []byte("mock-data"), nil
}

func (m *mockUploader) GetObjectStream(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.streamCalls = append(m.streamCalls, objectName)
	return io.NopCloser(bytes.NewReader([]byte("mock-data"))), nil
}

func (m *mockUploader) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	return nil
}

func (m *mockUploader) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)
	go func() {
		defer close(ch)
		m.mu.Lock()
		defer m.mu.Unlock()
		for key, info := range m.objects {
			if m.listErr != nil {
				ch <- minio.ObjectInfo{Err: m.listErr}
				return
			}
			info.Key = key
			ch <- info
		}
	}()
	return ch
}

func (m *mockUploader) CopyObject(ctx context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	if m.copyDelay > 0 {
		time.Sleep(m.copyDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.copyErr != nil {
		return minio.UploadInfo{}, m.copyErr
	}
	m.copyCalls = append(m.copyCalls, src.Object)
	return minio.UploadInfo{}, nil
}

func (m *mockUploader) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statErr != nil {
		return minio.ObjectInfo{}, m.statErr
	}
	if info, ok := m.objects[objectName]; ok {
		return info, nil
	}
	return minio.ObjectInfo{}, errors.New("object not found")
}

func (m *mockUploader) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[objectName]
	return ok, nil
}

func (m *mockUploader) ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]storage.ObjectInfo, error) {
	return nil, nil
}

func (m *mockUploader) addMockObject(key string, lastModified time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = minio.ObjectInfo{
		Key:          key,
		LastModified: lastModified,
		Size:         1024,
		ContentType:  "application/octet-stream",
		UserMetadata: map[string]string{},
	}
}

func (m *mockUploader) getCopyCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.copyCalls...)
}

func (m *mockUploader) getPutCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.putCalls...)
}

func (m *mockUploader) getGetCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.getCalls...)
}

func (m *mockUploader) getStreamCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.streamCalls...)
}

func TestReplicatorStatus_String(t *testing.T) {
	tests := []struct {
		status   ReplicatorStatus
		expected string
	}{
		{StatusIdle, "idle"},
		{StatusRunning, "running"},
		{StatusThrottled, "throttled"},
		{StatusPaused, "paused"},
		{StatusError, "error"},
		{StatusOverloaded, "overloaded"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.status))
		})
	}
}

func TestSyncResult(t *testing.T) {
	result := &SyncResult{
		TotalObjects: 100,
		SyncedCount:  80,
		SkippedCount: 15,
		FailedCount:  5,
		Duration:     time.Second * 10,
	}

	assert.Equal(t, int64(100), result.TotalObjects)
	assert.Equal(t, int64(80), result.SyncedCount)
	assert.Equal(t, int64(15), result.SkippedCount)
	assert.Equal(t, int64(5), result.FailedCount)
	assert.Equal(t, time.Second*10, result.Duration)
}

func TestAtomicReplicatorStatus(t *testing.T) {
	status := NewAtomicReplicatorStatus(StatusIdle)
	assert.Equal(t, StatusIdle, status.Load())

	status.Store(StatusRunning)
	assert.Equal(t, StatusRunning, status.Load())

	status.Store(StatusError)
	assert.Equal(t, StatusError, status.Load())
}

func TestReplicator_GetStatus(t *testing.T) {
	logger := zap.NewNop()

	r := &Replicator{
		logger: logger,
		stopCh: make(chan struct{}),
		status: StatusIdle,
	}

	assert.Equal(t, StatusIdle, r.GetStatus())

	r.mu.Lock()
	r.status = StatusRunning
	r.mu.Unlock()

	assert.Equal(t, StatusRunning, r.GetStatus())
}

func TestReplicator_GetStats(t *testing.T) {
	logger := zap.NewNop()

	r := &Replicator{
		logger:       logger,
		stopCh:       make(chan struct{}),
		status:       StatusIdle,
		lastSyncTime: time.Now(),
		syncCycles:   5,
	}

	stats := r.GetStats()
	assert.Equal(t, StatusIdle, stats.Status)
	assert.Equal(t, int64(0), stats.LastSyncCount)
	assert.Equal(t, int64(5), stats.SyncCycles)
}

func TestReplicator_IsRunning(t *testing.T) {
	logger := zap.NewNop()

	r := &Replicator{
		logger: logger,
		stopCh: make(chan struct{}),
		status: StatusIdle,
	}

	assert.False(t, r.IsRunning())

	r.mu.Lock()
	r.status = StatusRunning
	r.mu.Unlock()

	assert.True(t, r.IsRunning())

	r.mu.Lock()
	r.status = StatusThrottled
	r.mu.Unlock()

	assert.True(t, r.IsRunning())

	r.mu.Lock()
	r.status = StatusIdle
	r.mu.Unlock()

	assert.False(t, r.IsRunning())
}

func TestReplicator_Stop(t *testing.T) {
	logger := zap.NewNop()

	r := &Replicator{
		logger:    logger,
		stopCh:    make(chan struct{}),
		throttler: NewThrottler(DefaultThrottleConfig()),
	}

	r.Stop()
	r.Stop()

	select {
	case <-r.stopCh:
	default:
		t.Error("stopCh should be closed after Stop()")
	}
}

func TestReplicator_DetectIncremental_EmptyBucket(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Empty(t, objects)
}

func TestReplicator_DetectIncremental_NewObjects(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("obj1", time.Now().Add(-time.Hour))
	mockSrc.addMockObject("obj2", time.Now().Add(-30*time.Minute))
	mockSrc.addMockObject("obj3", time.Now())

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:new", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Len(t, objects, 3)
}

func TestReplicator_DetectIncremental_Incremental(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	now := time.Now()
	mockSrc.addMockObject("obj1", now.Add(-2*time.Hour))
	mockSrc.addMockObject("obj2", now.Add(-time.Hour))
	mockSrc.addMockObject("obj3", now)

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:incr", t.TempDir(), logger)

	cp := NewSyncCheckpoint()
	cp.UpdateObjectVersion("obj1", now.Add(-2*time.Hour))
	cp.UpdateObjectVersion("obj2", now.Add(-time.Hour))
	err := checkpoint.Save(context.Background(), "test-bucket", cp)
	require.NoError(t, err)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "obj3", objects[0])
}

func TestReplicator_DetectIncremental_ModifiedObject(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	now := time.Now()
	mockSrc.addMockObject("obj1", now)

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:mod", t.TempDir(), logger)

	cp := NewSyncCheckpoint()
	cp.UpdateObjectVersion("obj1", now.Add(-time.Hour))
	err := checkpoint.Save(context.Background(), "test-bucket", cp)
	require.NoError(t, err)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "obj1", objects[0])
}

func TestReplicator_DetectIncremental_ListError(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.listErr = errors.New("list error")
	mockSrc.addMockObject("obj1", time.Now())

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:listerr", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, _, err := r.detectIncremental(context.Background(), "test-bucket")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list objects error")
}

func TestReplicator_CopyObject(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("test-obj", time.Now())
	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:  100,
		Workers: 4,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:copy", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	err := r.copyObject(context.Background(), "test-bucket", "test-obj")
	require.NoError(t, err)

	streamCalls := mockSrc.getStreamCalls()
	assert.Contains(t, streamCalls, "test-obj", "GetObjectStream should be called on source for streaming transfer")

	putCalls := mockDst.getPutCalls()
	assert.Contains(t, putCalls, "test-obj", "PutObject should be called on destination")

	copyCalls := mockDst.getCopyCalls()
	assert.Empty(t, copyCalls, "CopyObject should NOT be called - it doesn't work for cross-server replication")
}

func TestReplicator_CopyObject_UsesGetPutNotServerSideCopy(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("cross-server-obj", time.Now())
	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:  100,
		Workers: 4,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:crossserver", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	err := r.copyObject(context.Background(), "primary-bucket", "cross-server-obj")
	require.NoError(t, err)

	srcStreamCalls := mockSrc.getStreamCalls()
	assert.Contains(t, srcStreamCalls, "cross-server-obj",
		"Cross-server replication must use GetObjectStream to fetch data from source")

	dstPutCalls := mockDst.getPutCalls()
	dstCopyCalls := mockDst.getCopyCalls()

	assert.Contains(t, dstPutCalls, "cross-server-obj",
		"Cross-server replication must use PutObject to send data to destination")
	assert.Empty(t, dstCopyCalls,
		"Cross-server replication must NOT use CopyObject (server-side copy) - it only works within same MinIO instance")
}

func TestReplicator_CopyObject_GetError(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("test-obj", time.Now())
	mockSrc.getErr = errors.New("get failed")
	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:  100,
		Workers: 4,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:geterr", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	err := r.copyObject(context.Background(), "test-bucket", "test-obj")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get object")
}

func TestReplicator_CopyObject_PutError(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("test-obj", time.Now())
	mockDst := newMockUploader()
	mockDst.putErr = errors.New("put failed")

	cfg := &config.ReplicationConfig{
		MaxQPS:  100,
		Workers: 4,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:puterr", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	err := r.copyObject(context.Background(), "test-bucket", "test-obj")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "put object")
}

func TestReplicator_CopyObject_StreamingTransfer(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("large-obj", time.Now())
	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:  100,
		Workers: 4,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:stream", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	err := r.copyObject(context.Background(), "test-bucket", "large-obj")
	require.NoError(t, err)

	streamCalls := mockSrc.getStreamCalls()
	assert.Contains(t, streamCalls, "large-obj", "copyObject should use GetObjectStream for streaming")

	getCalls := mockSrc.getGetCalls()
	assert.NotContains(t, getCalls, "large-obj", "copyObject should NOT use GetObject (full read) for streaming")

	putCalls := mockDst.getPutCalls()
	assert.Contains(t, putCalls, "large-obj", "PutObject should be called on destination")
}

func TestReplicator_SyncBucket(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("obj1", time.Now())
	mockSrc.addMockObject("obj2", time.Now())
	mockSrc.addMockObject("obj3", time.Now())

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:bucket", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(3), result.TotalObjects)
	assert.Equal(t, int64(3), result.SyncedCount)
	assert.Equal(t, int64(0), result.FailedCount)
	assert.NoError(t, result.Error)
}

func TestReplicator_SyncBucket_WithFailure(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("obj1", time.Now())
	mockSrc.addMockObject("obj2", time.Now())

	mockDst := newMockUploader()
	mockDst.putErr = errors.New("put failed")

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:fail", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.TotalObjects)
	assert.Equal(t, int64(0), result.SyncedCount)
	assert.Equal(t, int64(2), result.FailedCount)
}

func TestReplicator_SyncBucket_ContextCancel(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("obj1", time.Now())
	mockSrc.copyDelay = time.Second * 2

	mockDst := newMockUploader()
	mockDst.copyDelay = time.Second * 2

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:cancel", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.SyncBucket(ctx, "test-bucket")
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReplicator_SyncBucket_EmptyBucket(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:empty", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.TotalObjects)
	assert.Equal(t, int64(0), result.SyncedCount)
	assert.NoError(t, result.Error)
}

func TestReplicator_ForceSync(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("obj1", time.Now())
	mockSrc.addMockObject("obj2", time.Now())

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:force", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	err := r.ForceSync(context.Background())
	require.NoError(t, err)

	stats := r.GetStats()
	assert.Equal(t, int64(2), stats.LastSyncCount)
	assert.Equal(t, int64(1), stats.SyncCycles)
}

func TestReplicator_ForceSync_Overloaded(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	mockSrc.addMockObject("obj1", time.Now())

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	snapshotProvider := func() *metrics.RuntimeSnapshot {
		return &metrics.RuntimeSnapshot{
			LoadLevel:     metrics.LoadOverloaded,
			ThrottleRatio: 1.0,
		}
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg,
		WithSnapshotProvider(snapshotProvider),
		WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:overload", t.TempDir(), logger)

	r := &Replicator{
		srcUploader:      mockSrc,
		dstUploader:      mockDst,
		cfg:              cfg,
		throttler:        throttler,
		checkpoint:       checkpoint,
		logger:           logger,
		stopCh:           make(chan struct{}),
		status:           StatusIdle,
		snapshotProvider: snapshotProvider,
	}

	err := r.ForceSync(context.Background())
	require.NoError(t, err)

	assert.Equal(t, StatusOverloaded, r.GetStatus())
}

type countingCheckpointStore struct {
	*CheckpointStore
	loadCalls int
	saveCalls int
	mu        sync.Mutex
}

func (c *countingCheckpointStore) Load(ctx context.Context, bucket string) (*SyncCheckpoint, error) {
	c.mu.Lock()
	c.loadCalls++
	c.mu.Unlock()
	return c.CheckpointStore.Load(ctx, bucket)
}

func (c *countingCheckpointStore) Save(ctx context.Context, bucket string, checkpoint *SyncCheckpoint) error {
	c.mu.Lock()
	c.saveCalls++
	c.mu.Unlock()
	return c.CheckpointStore.Save(ctx, bucket, checkpoint)
}

func (c *countingCheckpointStore) getLoadCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadCalls
}

func (c *countingCheckpointStore) getSaveCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveCalls
}

func TestReplicator_CheckpointBatchSaving(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	objectCount := 250
	for i := 0; i < objectCount; i++ {
		mockSrc.addMockObject(fmt.Sprintf("obj%d", i), time.Now())
	}

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   1000,
		Workers:  10,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()

	baseCheckpoint := NewCheckpointStore(nil, "test:checkpoint:batch", t.TempDir(), logger)
	countingStore := &countingCheckpointStore{CheckpointStore: baseCheckpoint}

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  countingStore,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(objectCount), result.SyncedCount)

	loadCalls := countingStore.getLoadCalls()
	saveCalls := countingStore.getSaveCalls()

	assert.Equal(t, 1, loadCalls, "Checkpoint should be loaded only once per sync cycle")

	expectedSaveCalls := objectCount/checkpointBatchSize + 1
	if objectCount%checkpointBatchSize != 0 {
		expectedSaveCalls++
	}
	assert.LessOrEqual(t, saveCalls, expectedSaveCalls,
		"Checkpoint should be saved in batches (every %d objects) + final save, not once per object",
		checkpointBatchSize)
	assert.Less(t, saveCalls, objectCount,
		"Save calls (%d) should be much less than object count (%d)",
		saveCalls, objectCount)

	t.Logf("Synced %d objects with %d Load calls and %d Save calls (batch size: %d)",
		objectCount, loadCalls, saveCalls, checkpointBatchSize)
}

func TestReplicator_CheckpointBatchSaving_ExactBatchBoundary(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	objectCount := checkpointBatchSize
	for i := 0; i < objectCount; i++ {
		mockSrc.addMockObject(fmt.Sprintf("obj%d", i), time.Now())
	}

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   1000,
		Workers:  10,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()

	baseCheckpoint := NewCheckpointStore(nil, "test:checkpoint:boundary", t.TempDir(), logger)
	countingStore := &countingCheckpointStore{CheckpointStore: baseCheckpoint}

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  countingStore,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(objectCount), result.SyncedCount)

	saveCalls := countingStore.getSaveCalls()

	assert.Equal(t, 2, saveCalls,
		"Exact %d objects should trigger 2 saves: 1 batch save + 1 final save",
		checkpointBatchSize)
}

func TestReplicator_CheckpointBatchSaving_LessThanBatchSize(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	objectCount := checkpointBatchSize - 1
	for i := 0; i < objectCount; i++ {
		mockSrc.addMockObject(fmt.Sprintf("obj%d", i), time.Now())
	}

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   1000,
		Workers:  10,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()

	baseCheckpoint := NewCheckpointStore(nil, "test:checkpoint:less", t.TempDir(), logger)
	countingStore := &countingCheckpointStore{CheckpointStore: baseCheckpoint}

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  countingStore,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(objectCount), result.SyncedCount)

	saveCalls := countingStore.getSaveCalls()

	assert.Equal(t, 1, saveCalls,
		"Less than %d objects should trigger only 1 final save, no batch saves",
		checkpointBatchSize)
}

func TestReplicator_CheckpointBatchSaving_IOStormPrevention(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	objectCount := 1000
	for i := 0; i < objectCount; i++ {
		mockSrc.addMockObject(fmt.Sprintf("obj%d", i), time.Now())
	}

	mockDst := newMockUploader()

	cfg := &config.ReplicationConfig{
		MaxQPS:   1000,
		Workers:  10,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()

	baseCheckpoint := NewCheckpointStore(nil, "test:checkpoint:storm", t.TempDir(), logger)
	countingStore := &countingCheckpointStore{CheckpointStore: baseCheckpoint}

	r := &Replicator{
		srcUploader: mockSrc,
		dstUploader: mockDst,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  countingStore,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	result, err := r.SyncBucket(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(objectCount), result.SyncedCount)

	loadCalls := countingStore.getLoadCalls()
	saveCalls := countingStore.getSaveCalls()

	totalIO := loadCalls + saveCalls
	oldIOCount := objectCount * 2
	ioReduction := float64(oldIOCount-totalIO) / float64(oldIOCount) * 100

	assert.Equal(t, 1, loadCalls, "Should load checkpoint only once")
	assert.LessOrEqual(t, saveCalls, objectCount/checkpointBatchSize+2,
		"Save calls should be batched")

	t.Logf("IO reduction: %.1f%% (old: %d IO ops, new: %d IO ops for %d objects)",
		ioReduction, oldIOCount, totalIO, objectCount)
	assert.Greater(t, ioReduction, 90.0,
		"Batch saving should reduce IO operations by at least 90%%")
}

func TestReplicator_DetectIncremental_MemoryLimit(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	totalObjects := 50000
	maxObjects := 100

	now := time.Now()
	for i := 0; i < totalObjects; i++ {
		mockSrc.addMockObject(fmt.Sprintf("obj%d", i), now.Add(time.Duration(i)*time.Millisecond))
	}

	cfg := &config.ReplicationConfig{
		MaxQPS:            100,
		Workers:           4,
		Interval:          60,
		MaxObjectsPerSync: maxObjects,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:memory", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Len(t, objects, maxObjects, "Should limit objects to MaxObjectsPerSync")
}

func TestReplicator_DetectIncremental_PrioritizesRecentObjects(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	now := time.Now()

	mockSrc.addMockObject("old-obj", now.Add(-24*time.Hour))
	mockSrc.addMockObject("mid-obj", now.Add(-1*time.Hour))
	mockSrc.addMockObject("new-obj", now)

	cfg := &config.ReplicationConfig{
		MaxQPS:            100,
		Workers:           4,
		Interval:          60,
		MaxObjectsPerSync: 2,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:priority", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Len(t, objects, 2, "Should return exactly MaxObjectsPerSync objects")
	assert.Contains(t, objects, "new-obj", "Should include newest object")
	assert.Contains(t, objects, "mid-obj", "Should include second newest object")
	assert.NotContains(t, objects, "old-obj", "Should exclude oldest object")
}

func TestReplicator_DetectIncremental_DefaultLimit(t *testing.T) {
	logger := zap.NewNop()

	mockSrc := newMockUploader()
	for i := 0; i < 100; i++ {
		mockSrc.addMockObject(fmt.Sprintf("obj%d", i), time.Now().Add(time.Duration(i)*time.Millisecond))
	}

	cfg := &config.ReplicationConfig{
		MaxQPS:   100,
		Workers:  4,
		Interval: 60,
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	throttler := NewThrottler(throttleCfg, WithLogger(logger))
	defer throttler.Stop()
	checkpoint := NewCheckpointStore(nil, "test:checkpoint:default", t.TempDir(), logger)

	r := &Replicator{
		srcUploader: mockSrc,
		cfg:         cfg,
		throttler:   throttler,
		checkpoint:  checkpoint,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	_, objects, err := r.detectIncremental(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.Len(t, objects, 100, "Should return all objects when below default limit")
}
