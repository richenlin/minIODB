package replication

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"minIODB/config"
	"minIODB/internal/metrics"
	"minIODB/internal/storage"
	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

type ReplicatorStatus string

const (
	StatusIdle       ReplicatorStatus = "idle"
	StatusRunning    ReplicatorStatus = "running"
	StatusThrottled  ReplicatorStatus = "throttled"
	StatusPaused     ReplicatorStatus = "paused"
	StatusError      ReplicatorStatus = "error"
	StatusOverloaded ReplicatorStatus = "overloaded"
)

var (
	ErrReplicatorStopped = errors.New("replicator stopped")
	ErrSrcPoolNil        = errors.New("source pool is nil")
	ErrDstUploaderNil    = errors.New("destination uploader is nil")
)

type Replicator struct {
	srcPool          *pool.MinIOPool // Held to prevent pool from being GC'd while replicator is active
	srcUploader      storage.Uploader
	dstUploader      storage.Uploader
	checkpoint       *CheckpointStore
	throttler        *Throttler
	cfg              *config.ReplicationConfig
	logger           *zap.Logger
	stopCh           chan struct{}
	stopOnce         sync.Once
	wg               sync.WaitGroup
	snapshotProvider func() *metrics.RuntimeSnapshot

	mu              sync.RWMutex
	status          ReplicatorStatus
	lastSyncTime    time.Time
	lastSyncCount   int64
	lastSyncSkipped int64
	lastSyncFailed  int64
	currentBucket   string
	syncCycles      int64
	errorMsg        string
}

type ReplicatorStats struct {
	Status          ReplicatorStatus `json:"status"`
	LastSyncTime    time.Time        `json:"last_sync_time"`
	LastSyncCount   int64            `json:"last_sync_count"`
	LastSyncSkipped int64            `json:"last_sync_skipped"`
	LastSyncFailed  int64            `json:"last_sync_failed"`
	CurrentBucket   string           `json:"current_bucket"`
	SyncCycles      int64            `json:"sync_cycles"`
	ErrorMsg        string           `json:"error_msg,omitempty"`
	CheckpointStore *CheckpointStore `json:"-"`
	Throttler       *Throttler       `json:"-"`
}

type ReplicatorOption func(*Replicator)

func WithReplicatorCheckpoint(checkpoint *CheckpointStore) ReplicatorOption {
	return func(r *Replicator) {
		if checkpoint != nil {
			r.checkpoint = checkpoint
		}
	}
}

func WithReplicatorThrottler(throttler *Throttler) ReplicatorOption {
	return func(r *Replicator) {
		if throttler != nil {
			r.throttler = throttler
		}
	}
}

func WithReplicatorSnapshotProvider(provider func() *metrics.RuntimeSnapshot) ReplicatorOption {
	return func(r *Replicator) {
		if provider != nil {
			r.snapshotProvider = provider
		}
	}
}

func NewReplicator(
	srcPool *pool.MinIOPool,
	dstUploader storage.Uploader,
	cfg *config.ReplicationConfig,
	redisPool *pool.RedisPool,
	logger *zap.Logger,
	opts ...ReplicatorOption,
) (*Replicator, error) {
	if srcPool == nil {
		return nil, ErrSrcPoolNil
	}
	if dstUploader == nil {
		return nil, ErrDstUploaderNil
	}
	if cfg == nil {
		cfg = &config.ReplicationConfig{}
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	throttleCfg := ThrottleConfig{
		MaxQPS:              cfg.MaxQPS,
		MaxConcurrentCopies: cfg.Workers,
	}
	if throttleCfg.MaxQPS <= 0 {
		throttleCfg.MaxQPS = DefaultThrottleConfig().MaxQPS
	}
	if throttleCfg.MaxConcurrentCopies <= 0 {
		throttleCfg.MaxConcurrentCopies = DefaultThrottleConfig().MaxConcurrentCopies
	}

	r := &Replicator{
		srcPool:     srcPool,
		srcUploader: storage.NewMinioClientWrapperFromClient(srcPool.GetClient(), logger),
		dstUploader: dstUploader,
		cfg:         cfg,
		logger:      logger,
		stopCh:      make(chan struct{}),
		status:      StatusIdle,
	}

	for _, opt := range opts {
		opt(r)
	}

	if r.snapshotProvider == nil {
		r.snapshotProvider = metrics.GetRuntimeSnapshot
	}

	if r.checkpoint == nil {
		checkpointPrefix := cfg.CheckpointKeyPrefix
		if checkpointPrefix == "" {
			checkpointPrefix = "miniodb:replication:checkpoint"
		}
		r.checkpoint = NewCheckpointStore(redisPool, checkpointPrefix, "", logger)
	}

	if r.throttler == nil {
		r.throttler = NewThrottler(throttleCfg, WithLogger(logger))
	}

	return r, nil
}

func (r *Replicator) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.syncLoop(ctx)
}

func (r *Replicator) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})
	r.wg.Wait()

	if r.throttler != nil {
		r.throttler.Stop()
	}
}

func (r *Replicator) syncLoop(ctx context.Context) {
	defer r.wg.Done()

	interval := time.Duration(r.cfg.Interval) * time.Second
	if interval <= 0 {
		interval = time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	r.logger.Info("Replicator started",
		zap.Duration("interval", interval),
		zap.Int("max_qps", r.cfg.MaxQPS),
		zap.Int("workers", r.cfg.Workers))

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("Replicator stopping due to context cancellation")
			return
		case <-r.stopCh:
			r.logger.Info("Replicator stopping due to stop signal")
			return
		case <-ticker.C:
			if err := r.syncOnce(ctx); err != nil {
				if errors.Is(err, ErrReplicatorStopped) {
					return
				}
				r.logger.Error("Sync cycle failed", zap.Error(err))
			}
		}
	}
}

func (r *Replicator) syncOnce(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.stopCh:
		return ErrReplicatorStopped
	default:
	}

	r.mu.Lock()
	r.status = StatusRunning
	r.errorMsg = ""
	r.mu.Unlock()

	startTime := time.Now()

	snapshotProvider := r.snapshotProvider
	if snapshotProvider == nil {
		snapshotProvider = metrics.GetRuntimeSnapshot
	}
	snap := snapshotProvider()
	if snap != nil && snap.LoadLevel == metrics.LoadOverloaded {
		r.mu.Lock()
		r.status = StatusOverloaded
		r.mu.Unlock()
		r.logger.Warn("Sync skipped due to system overload")
		return nil
	}

	bucket := r.cfg.SourceBucket
	if bucket == "" {
		bucket = r.currentBucket
	}
	if bucket == "" {
		bucket = "default"
	}

	r.mu.Lock()
	r.currentBucket = bucket
	r.mu.Unlock()

	objectsToSync, err := r.detectIncremental(ctx, bucket)
	if err != nil {
		r.mu.Lock()
		r.status = StatusError
		r.errorMsg = err.Error()
		r.mu.Unlock()
		return fmt.Errorf("detect incremental: %w", err)
	}

	if len(objectsToSync) == 0 {
		r.mu.Lock()
		r.status = StatusIdle
		r.lastSyncTime = time.Now()
		r.mu.Unlock()
		r.logger.Debug("No objects to sync")
		return nil
	}

	var syncedCount, skippedCount, failedCount int64

	for _, objectKey := range objectsToSync {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.stopCh:
			return ErrReplicatorStopped
		default:
		}

		if err := r.throttler.AdaptiveWait(ctx); err != nil {
			if errors.Is(err, ErrThrottleOverloaded) {
				r.mu.Lock()
				r.status = StatusThrottled
				r.mu.Unlock()
				r.logger.Warn("Sync paused due to system overload during copy",
					zap.Int("synced", int(syncedCount)),
					zap.Int("remaining", len(objectsToSync)-int(syncedCount)-int(skippedCount)-int(failedCount)))
				break
			}
			if errors.Is(err, ErrThrottleStopped) {
				return ErrReplicatorStopped
			}
			return fmt.Errorf("adaptive wait failed during copy: %w", err)
		}

		if err := r.copyObject(ctx, bucket, objectKey); err != nil {
			failedCount++
			r.logger.Error("Failed to copy object",
				zap.String("bucket", bucket),
				zap.String("object", objectKey),
				zap.Error(err))
		} else {
			syncedCount++
		}

		r.throttler.Release()
	}

	r.mu.Lock()
	r.status = StatusIdle
	r.lastSyncTime = time.Now()
	r.lastSyncCount = syncedCount
	r.lastSyncSkipped = skippedCount
	r.lastSyncFailed = failedCount
	r.syncCycles++
	r.mu.Unlock()

	r.logger.Info("Sync cycle completed",
		zap.Duration("duration", time.Since(startTime)),
		zap.Int64("synced", syncedCount),
		zap.Int64("skipped", skippedCount),
		zap.Int64("failed", failedCount))

	return nil
}

func (r *Replicator) detectIncremental(ctx context.Context, bucket string) ([]string, error) {
	checkpoint, err := r.checkpoint.Load(ctx, bucket)
	if err != nil {
		r.logger.Warn("Failed to load checkpoint, starting fresh", zap.Error(err))
		checkpoint = NewSyncCheckpoint()
	}

	var objectsToSync []string

	objectCh := r.srcUploader.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Recursive: true,
	})

	for obj := range objectCh {
		if obj.Err != nil {
			return nil, fmt.Errorf("list objects error: %w", obj.Err)
		}

		if obj.Key == "" {
			continue
		}

		lastSynced, exists := checkpoint.GetObjectVersion(obj.Key)
		if !exists || obj.LastModified.After(lastSynced) {
			objectsToSync = append(objectsToSync, obj.Key)
		}
	}

	return objectsToSync, nil
}

func (r *Replicator) copyObject(ctx context.Context, bucket, objectKey string) error {
	src := minio.CopySrcOptions{
		Bucket: bucket,
		Object: objectKey,
	}
	dst := minio.CopyDestOptions{
		Bucket: bucket,
		Object: objectKey,
	}

	_, err := r.dstUploader.CopyObject(ctx, dst, src)
	if err != nil {
		return fmt.Errorf("copy object %s: %w", objectKey, err)
	}

	checkpoint, loadErr := r.checkpoint.Load(ctx, bucket)
	if loadErr != nil {
		checkpoint = NewSyncCheckpoint()
	}

	checkpoint.UpdateObjectVersion(objectKey, time.Now())

	if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
		r.logger.Warn("Failed to save checkpoint after copy",
			zap.String("object", objectKey),
			zap.Error(saveErr))
	}

	return nil
}

func (r *Replicator) GetStatus() ReplicatorStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Replicator) GetStats() ReplicatorStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return ReplicatorStats{
		Status:          r.status,
		LastSyncTime:    r.lastSyncTime,
		LastSyncCount:   r.lastSyncCount,
		LastSyncSkipped: r.lastSyncSkipped,
		LastSyncFailed:  r.lastSyncFailed,
		CurrentBucket:   r.currentBucket,
		SyncCycles:      r.syncCycles,
		ErrorMsg:        r.errorMsg,
		CheckpointStore: r.checkpoint,
		Throttler:       r.throttler,
	}
}

func (r *Replicator) ForceSync(ctx context.Context) error {
	return r.syncOnce(ctx)
}

func (r *Replicator) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status == StatusRunning || r.status == StatusThrottled
}

type SyncResult struct {
	TotalObjects int64
	SyncedCount  int64
	SkippedCount int64
	FailedCount  int64
	Duration     time.Duration
	Error        error
}

func (r *Replicator) SyncBucket(ctx context.Context, bucket string) (*SyncResult, error) {
	result := &SyncResult{}
	startTime := time.Now()

	defer func() {
		result.Duration = time.Since(startTime)
	}()

	objectsToSync, err := r.detectIncremental(ctx, bucket)
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("detect incremental: %w", err)
	}

	result.TotalObjects = int64(len(objectsToSync))

	for _, objectKey := range objectsToSync {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			return result, ctx.Err()
		case <-r.stopCh:
			result.Error = ErrReplicatorStopped
			return result, ErrReplicatorStopped
		default:
		}

		if err := r.throttler.AdaptiveWait(ctx); err != nil {
			if errors.Is(err, ErrThrottleOverloaded) {
				r.logger.Warn("Sync bucket paused due to overload",
					zap.String("bucket", bucket),
					zap.Int64("synced", result.SyncedCount))
				break
			}
			result.Error = err
			return result, err
		}

		if err := r.copyObject(ctx, bucket, objectKey); err != nil {
			result.FailedCount++
			r.logger.Error("Failed to copy object in SyncBucket",
				zap.String("bucket", bucket),
				zap.String("object", objectKey),
				zap.Error(err))
		} else {
			result.SyncedCount++
		}

		r.throttler.Release()
	}

	return result, nil
}

type AtomicReplicatorStatus struct {
	value atomic.Value
}

func NewAtomicReplicatorStatus(initial ReplicatorStatus) *AtomicReplicatorStatus {
	a := &AtomicReplicatorStatus{}
	a.value.Store(initial)
	return a
}

func (a *AtomicReplicatorStatus) Load() ReplicatorStatus {
	return a.value.Load().(ReplicatorStatus)
}

func (a *AtomicReplicatorStatus) Store(status ReplicatorStatus) {
	a.value.Store(status)
}
