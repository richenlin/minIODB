package replication

import (
	"container/heap"
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

	checkpointBatchSize   = 100
	defaultMaxObjectsSync = 10000
)

var (
	ErrReplicatorStopped = errors.New("replicator stopped")
	ErrSrcPoolNil        = errors.New("source pool is nil")
	ErrDstUploaderNil    = errors.New("destination uploader is nil")
)

type objectToSync struct {
	key          string
	lastModified time.Time
}

type maxObjectHeap []objectToSync

func (h maxObjectHeap) Len() int           { return len(h) }
func (h maxObjectHeap) Less(i, j int) bool { return h[i].lastModified.Before(h[j].lastModified) }
func (h maxObjectHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *maxObjectHeap) Push(x interface{}) {
	*h = append(*h, x.(objectToSync))
}

func (h *maxObjectHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type Replicator struct {
	srcPool          *pool.MinIOPool
	srcUploader      storage.Uploader
	dstUploader      storage.Uploader
	checkpoint       CheckpointStoreInterface
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
	Status          ReplicatorStatus         `json:"status"`
	LastSyncTime    time.Time                `json:"last_sync_time"`
	LastSyncCount   int64                    `json:"last_sync_count"`
	LastSyncSkipped int64                    `json:"last_sync_skipped"`
	LastSyncFailed  int64                    `json:"last_sync_failed"`
	CurrentBucket   string                   `json:"current_bucket"`
	SyncCycles      int64                    `json:"sync_cycles"`
	ErrorMsg        string                   `json:"error_msg,omitempty"`
	CheckpointStore CheckpointStoreInterface `json:"-"`
	Throttler       *Throttler               `json:"-"`
}

type ReplicatorOption func(*Replicator)

func WithReplicatorCheckpoint(checkpoint CheckpointStoreInterface) ReplicatorOption {
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

	srcUploader, err := storage.NewMinioClientWrapperFromClient(srcPool.GetClient(), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create src uploader: %w", err)
	}

	r := &Replicator{
		srcPool:     srcPool,
		srcUploader: srcUploader,
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

	checkpoint, objectsToSync, err := r.detectIncremental(ctx, bucket)
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

	for i, objectKey := range objectsToSync {
		select {
		case <-ctx.Done():
			if err := r.checkpoint.Save(ctx, bucket, checkpoint); err != nil {
				r.logger.Warn("Failed to save checkpoint on context done", zap.Error(err))
			}
			return ctx.Err()
		case <-r.stopCh:
			if err := r.checkpoint.Save(ctx, bucket, checkpoint); err != nil {
				r.logger.Warn("Failed to save checkpoint on stop", zap.Error(err))
			}
			return ErrReplicatorStopped
		default:
		}

		if err := r.throttler.AdaptiveWait(ctx); err != nil {
			if errors.Is(err, ErrThrottleOverloaded) {
				skippedCount = int64(len(objectsToSync)) - syncedCount - failedCount
				r.mu.Lock()
				r.status = StatusThrottled
				r.mu.Unlock()
				r.logger.Warn("Sync paused due to system overload during copy",
					zap.Int64("synced", syncedCount),
					zap.Int64("skipped", skippedCount))
				break
			}
			if errors.Is(err, ErrThrottleStopped) {
				if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
					r.logger.Warn("Failed to save checkpoint on throttle stopped", zap.Error(saveErr))
				}
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
			checkpoint.UpdateObjectVersion(objectKey, time.Now())

			if (int64(i)+1)%checkpointBatchSize == 0 {
				if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
					r.logger.Warn("Failed to save checkpoint batch",
						zap.Int64("synced", syncedCount),
						zap.Error(saveErr))
				}
			}
		}

		r.throttler.Release()
	}

	if err := r.checkpoint.Save(ctx, bucket, checkpoint); err != nil {
		r.logger.Warn("Failed to save final checkpoint", zap.Error(err))
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

func (r *Replicator) detectIncremental(ctx context.Context, bucket string) (*SyncCheckpoint, []string, error) {
	checkpoint, err := r.checkpoint.Load(ctx, bucket)
	if err != nil {
		r.logger.Warn("Failed to load checkpoint, starting fresh", zap.Error(err))
		checkpoint = NewSyncCheckpoint()
	}

	maxObjects := r.cfg.MaxObjectsPerSync
	if maxObjects <= 0 {
		maxObjects = defaultMaxObjectsSync
	}

	h := &maxObjectHeap{}
	heap.Init(h)
	var totalCount int

	objectCh := r.srcUploader.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Recursive: true,
	})

	for obj := range objectCh {
		if obj.Err != nil {
			return nil, nil, fmt.Errorf("list objects error: %w", obj.Err)
		}

		if obj.Key == "" {
			continue
		}

		lastSynced, exists := checkpoint.GetObjectVersion(obj.Key)
		if !exists || obj.LastModified.After(lastSynced) {
			totalCount++
			entry := objectToSync{key: obj.Key, lastModified: obj.LastModified}
			if h.Len() < maxObjects {
				heap.Push(h, entry)
			} else if obj.LastModified.After((*h)[0].lastModified) {
				heap.Pop(h)
				heap.Push(h, entry)
			}
		}
	}

	objectsToSync := make([]string, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		objectsToSync[i] = heap.Pop(h).(objectToSync).key
	}

	if totalCount > maxObjects {
		r.logger.Warn("Objects to sync exceeds limit, prioritizing most recent",
			zap.Int("total", totalCount),
			zap.Int("selected", len(objectsToSync)),
			zap.Int("limit", maxObjects))
	}

	return checkpoint, objectsToSync, nil
}

func (r *Replicator) copyObject(ctx context.Context, bucket, objectKey string) error {
	objInfo, err := r.srcUploader.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return fmt.Errorf("stat object %s from source: %w", objectKey, err)
	}

	reader, err := r.srcUploader.GetObjectStream(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("get object stream %s from source: %w", objectKey, err)
	}
	defer reader.Close()

	putOpts := minio.PutObjectOptions{
		ContentType:  objInfo.ContentType,
		UserMetadata: objInfo.UserMetadata,
	}

	_, err = r.dstUploader.PutObject(ctx, bucket, objectKey, reader, objInfo.Size, putOpts)
	if err != nil {
		return fmt.Errorf("put object %s to destination: %w", objectKey, err)
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

	checkpoint, objectsToSync, err := r.detectIncremental(ctx, bucket)
	if err != nil {
		result.Error = err
		return result, fmt.Errorf("detect incremental: %w", err)
	}

	result.TotalObjects = int64(len(objectsToSync))

	for i, objectKey := range objectsToSync {
		select {
		case <-ctx.Done():
			if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
				r.logger.Warn("Failed to save checkpoint on context done", zap.Error(saveErr))
			}
			result.Error = ctx.Err()
			return result, ctx.Err()
		case <-r.stopCh:
			if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
				r.logger.Warn("Failed to save checkpoint on stop", zap.Error(saveErr))
			}
			result.Error = ErrReplicatorStopped
			return result, ErrReplicatorStopped
		default:
		}

		if err := r.throttler.AdaptiveWait(ctx); err != nil {
			if errors.Is(err, ErrThrottleOverloaded) {
				result.SkippedCount = result.TotalObjects - result.SyncedCount - result.FailedCount
				r.logger.Warn("Sync bucket paused due to overload",
					zap.String("bucket", bucket),
					zap.Int64("synced", result.SyncedCount),
					zap.Int64("skipped", result.SkippedCount))
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
			checkpoint.UpdateObjectVersion(objectKey, time.Now())

			if (int64(i)+1)%checkpointBatchSize == 0 {
				if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
					r.logger.Warn("Failed to save checkpoint batch",
						zap.Int64("synced", result.SyncedCount),
						zap.Error(saveErr))
				}
			}
		}

		r.throttler.Release()
	}

	if err := r.checkpoint.Save(ctx, bucket, checkpoint); err != nil {
		r.logger.Warn("Failed to save final checkpoint", zap.Error(err))
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
