package replication

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minIODB/pkg/pool"

	"go.uber.org/zap"
)

type SyncCheckpoint struct {
	LastSyncTime   time.Time            `json:"last_sync_time"`
	ObjectVersions map[string]time.Time `json:"object_versions"`
	SyncedCount    int64                `json:"synced_count"`
	FailedObjects  []string             `json:"failed_objects"`
}

type CheckpointStoreInterface interface {
	Load(ctx context.Context, bucket string) (*SyncCheckpoint, error)
	Save(ctx context.Context, bucket string, checkpoint *SyncCheckpoint) error
	Clear(ctx context.Context, bucket string) error
	IsDegraded() bool
	ResetDegraded()
}

type CheckpointStore struct {
	redisPool   *pool.RedisPool
	keyPrefix   string
	fallbackDir string
	logger      *zap.Logger

	mu         sync.RWMutex
	degraded   bool
	fallbackMu sync.Mutex
}

func NewCheckpointStore(redisPool *pool.RedisPool, keyPrefix, fallbackDir string, logger *zap.Logger) *CheckpointStore {
	if keyPrefix == "" {
		keyPrefix = "miniodb:replication:checkpoint"
	}

	return &CheckpointStore{
		redisPool:   redisPool,
		keyPrefix:   keyPrefix,
		fallbackDir: fallbackDir,
		logger:      logger,
		degraded:    redisPool == nil,
	}
}

func (s *CheckpointStore) Load(ctx context.Context, bucket string) (*SyncCheckpoint, error) {
	key := s.buildKey(bucket)

	s.mu.RLock()
	degraded := s.degraded
	s.mu.RUnlock()

	if !degraded && s.redisPool != nil {
		checkpoint, err := s.loadFromRedis(ctx, key)
		if err == nil {
			return checkpoint, nil
		}

		s.logger.Warn("Failed to load checkpoint from Redis, falling back to local file",
			zap.String("bucket", bucket),
			zap.Error(err))

		s.mu.Lock()
		s.degraded = true
		s.mu.Unlock()
	}

	return s.loadFromFile(bucket)
}

func (s *CheckpointStore) Save(ctx context.Context, bucket string, checkpoint *SyncCheckpoint) error {
	if checkpoint == nil {
		return fmt.Errorf("checkpoint cannot be nil")
	}

	key := s.buildKey(bucket)

	s.mu.RLock()
	degraded := s.degraded
	s.mu.RUnlock()

	if !degraded && s.redisPool != nil {
		err := s.saveToRedis(ctx, key, checkpoint)
		if err == nil {
			return nil
		}

		s.logger.Warn("Failed to save checkpoint to Redis, falling back to local file",
			zap.String("bucket", bucket),
			zap.Error(err))

		s.mu.Lock()
		s.degraded = true
		s.mu.Unlock()
	}

	return s.saveToFile(bucket, checkpoint)
}

func (s *CheckpointStore) Clear(ctx context.Context, bucket string) error {
	key := s.buildKey(bucket)

	s.mu.RLock()
	degraded := s.degraded
	s.mu.RUnlock()

	var redisErr, fileErr error

	if !degraded && s.redisPool != nil {
		redisErr = s.clearFromRedis(ctx, key)
		if redisErr != nil {
			s.logger.Warn("Failed to clear checkpoint from Redis",
				zap.String("bucket", bucket),
				zap.Error(redisErr))
		}
	}

	fileErr = s.clearFromFile(bucket)
	if fileErr != nil {
		s.logger.Warn("Failed to clear checkpoint from local file",
			zap.String("bucket", bucket),
			zap.Error(fileErr))
	}

	if redisErr != nil && fileErr != nil {
		return fmt.Errorf("failed to clear checkpoint from both Redis and local file: redis=%v, file=%v", redisErr, fileErr)
	}

	return nil
}

func (s *CheckpointStore) buildKey(bucket string) string {
	return fmt.Sprintf("%s:%s", s.keyPrefix, bucket)
}

func (s *CheckpointStore) loadFromRedis(ctx context.Context, key string) (*SyncCheckpoint, error) {
	client := s.redisPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	data, err := client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get from redis: %w", err)
	}

	var checkpoint SyncCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	if checkpoint.ObjectVersions == nil {
		checkpoint.ObjectVersions = make(map[string]time.Time)
	}
	if checkpoint.FailedObjects == nil {
		checkpoint.FailedObjects = []string{}
	}

	if len(checkpoint.ObjectVersions) > maxCheckpointEntries {
		checkpoint.pruneOldEntries(maxCheckpointEntries)
	}

	return &checkpoint, nil
}

func (s *CheckpointStore) saveToRedis(ctx context.Context, key string, checkpoint *SyncCheckpoint) error {
	client := s.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}

	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	if err := client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("set to redis: %w", err)
	}

	return nil
}

func (s *CheckpointStore) clearFromRedis(ctx context.Context, key string) error {
	client := s.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}

	if err := client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("delete from redis: %w", err)
	}

	return nil
}

func (s *CheckpointStore) getFallbackPath(bucket string) string {
	if s.fallbackDir == "" {
		s.fallbackDir = os.TempDir()
	}
	safeBucket := filepath.Base(bucket)
	return filepath.Join(s.fallbackDir, fmt.Sprintf("checkpoint_%s.json", safeBucket))
}

func (s *CheckpointStore) loadFromFile(bucket string) (*SyncCheckpoint, error) {
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()

	path := s.getFallbackPath(bucket)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncCheckpoint{
				ObjectVersions: make(map[string]time.Time),
				FailedObjects:  []string{},
			}, nil
		}
		return nil, fmt.Errorf("read file: %w", err)
	}

	var checkpoint SyncCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	if checkpoint.ObjectVersions == nil {
		checkpoint.ObjectVersions = make(map[string]time.Time)
	}
	if checkpoint.FailedObjects == nil {
		checkpoint.FailedObjects = []string{}
	}

	if len(checkpoint.ObjectVersions) > maxCheckpointEntries {
		checkpoint.pruneOldEntries(maxCheckpointEntries)
	}

	return &checkpoint, nil
}

func (s *CheckpointStore) saveToFile(bucket string, checkpoint *SyncCheckpoint) error {
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()

	path := s.getFallbackPath(bucket)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create fallback directory: %w", err)
	}

	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

func (s *CheckpointStore) clearFromFile(bucket string) error {
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()

	path := s.getFallbackPath(bucket)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove file: %w", err)
	}

	return nil
}

func (s *CheckpointStore) IsDegraded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.degraded
}

func (s *CheckpointStore) ResetDegraded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.degraded = false
}

func NewSyncCheckpoint() *SyncCheckpoint {
	return &SyncCheckpoint{
		ObjectVersions: make(map[string]time.Time),
		FailedObjects:  []string{},
	}
}

func (c *SyncCheckpoint) UpdateObjectVersion(objectKey string, lastModified time.Time) {
	if c.ObjectVersions == nil {
		c.ObjectVersions = make(map[string]time.Time)
	}
	if len(c.ObjectVersions) >= maxCheckpointEntries {
		c.pruneOldEntries(maxCheckpointEntries / 2)
	}
	c.ObjectVersions[objectKey] = lastModified
	c.SyncedCount++
	c.LastSyncTime = time.Now()
}

type checkpointEntry struct {
	key       string
	timestamp time.Time
}

type minHeap []checkpointEntry

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].timestamp.Before(h[j].timestamp) }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *minHeap) Push(x interface{}) {
	*h = append(*h, x.(checkpointEntry))
}

func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (c *SyncCheckpoint) pruneOldEntries(keep int) {
	if len(c.ObjectVersions) <= keep {
		return
	}

	h := &minHeap{}
	heap.Init(h)

	for k, v := range c.ObjectVersions {
		entry := checkpointEntry{key: k, timestamp: v}
		if h.Len() < keep {
			heap.Push(h, entry)
		} else if v.After((*h)[0].timestamp) {
			heap.Pop(h)
			heap.Push(h, entry)
		}
	}

	newVersions := make(map[string]time.Time, keep)
	for _, entry := range *h {
		newVersions[entry.key] = entry.timestamp
	}
	c.ObjectVersions = newVersions
}

const MaxFailedObjects = 1000

const maxCheckpointEntries = 100000

func (c *SyncCheckpoint) AddFailedObject(objectKey string) {
	for _, key := range c.FailedObjects {
		if key == objectKey {
			return
		}
	}
	if len(c.FailedObjects) >= MaxFailedObjects {
		return
	}
	c.FailedObjects = append(c.FailedObjects, objectKey)
}

func (c *SyncCheckpoint) GetObjectVersion(objectKey string) (time.Time, bool) {
	if c.ObjectVersions == nil {
		return time.Time{}, false
	}
	t, ok := c.ObjectVersions[objectKey]
	return t, ok
}

func (c *SyncCheckpoint) RemoveFailedObject(objectKey string) {
	for i, key := range c.FailedObjects {
		if key == objectKey {
			c.FailedObjects = append(c.FailedObjects[:i], c.FailedObjects[i+1:]...)
			break
		}
	}
}

func (c *SyncCheckpoint) Clone() *SyncCheckpoint {
	clone := &SyncCheckpoint{
		LastSyncTime: c.LastSyncTime,
		SyncedCount:  c.SyncedCount,
	}

	if c.ObjectVersions != nil {
		clone.ObjectVersions = make(map[string]time.Time, len(c.ObjectVersions))
		for k, v := range c.ObjectVersions {
			clone.ObjectVersions[k] = v
		}
	}

	if c.FailedObjects != nil {
		clone.FailedObjects = make([]string, len(c.FailedObjects))
		copy(clone.FailedObjects, c.FailedObjects)
	}

	return clone
}
