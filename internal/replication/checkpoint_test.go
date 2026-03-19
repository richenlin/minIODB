package replication

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"minIODB/pkg/pool"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestRedisPool(t *testing.T) (*pool.RedisPool, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	cfg := pool.DefaultRedisPoolConfig()
	cfg.Addr = mr.Addr()
	cfg.PoolSize = 5

	logger := zap.NewNop()
	redisPool, err := pool.NewRedisPool(cfg, logger)
	if err != nil {
		mr.Close()
		t.Fatalf("Failed to create Redis pool: %v", err)
	}

	return redisPool, mr
}

func newTestCheckpointStore(t *testing.T, redisPool *pool.RedisPool) *CheckpointStore {
	fallbackDir := t.TempDir()
	logger := zap.NewNop()
	return NewCheckpointStore(redisPool, "test:checkpoint", fallbackDir, logger)
}

func TestCheckpointStore_SaveAndLoad(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	checkpoint := NewSyncCheckpoint()
	checkpoint.LastSyncTime = time.Now().UTC().Truncate(time.Second)
	checkpoint.UpdateObjectVersion("obj1", time.Now().UTC().Truncate(time.Second))
	checkpoint.UpdateObjectVersion("obj2", time.Now().UTC().Truncate(time.Second))
	checkpoint.AddFailedObject("obj3")

	if err := store.Save(ctx, "test-bucket", checkpoint); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !loaded.LastSyncTime.Equal(checkpoint.LastSyncTime) {
		t.Errorf("LastSyncTime mismatch: got %v, want %v", loaded.LastSyncTime, checkpoint.LastSyncTime)
	}

	if len(loaded.ObjectVersions) != 2 {
		t.Errorf("ObjectVersions count mismatch: got %d, want 2", len(loaded.ObjectVersions))
	}

	if loaded.SyncedCount != 2 {
		t.Errorf("SyncedCount mismatch: got %d, want 2", loaded.SyncedCount)
	}

	if len(loaded.FailedObjects) != 1 || loaded.FailedObjects[0] != "obj3" {
		t.Errorf("FailedObjects mismatch: got %v", loaded.FailedObjects)
	}
}

func TestCheckpointStore_Clear(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	checkpoint := NewSyncCheckpoint()
	checkpoint.UpdateObjectVersion("obj1", time.Now())

	if err := store.Save(ctx, "test-bucket", checkpoint); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := store.Clear(ctx, "test-bucket"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	loaded, err := store.Load(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("Load after clear failed: %v", err)
	}

	if loaded.LastSyncTime != (time.Time{}) {
		t.Errorf("Expected empty LastSyncTime after clear, got %v", loaded.LastSyncTime)
	}

	if len(loaded.ObjectVersions) != 0 {
		t.Errorf("Expected empty ObjectVersions after clear, got %d", len(loaded.ObjectVersions))
	}
}

func TestCheckpointStore_FallbackToFile(t *testing.T) {
	fallbackDir := t.TempDir()
	logger := zap.NewNop()
	store := NewCheckpointStore(nil, "test:checkpoint", fallbackDir, logger)
	ctx := context.Background()

	if !store.IsDegraded() {
		t.Error("Expected store to be in degraded mode without Redis")
	}

	checkpoint := NewSyncCheckpoint()
	checkpoint.LastSyncTime = time.Now().UTC().Truncate(time.Second)
	checkpoint.UpdateObjectVersion("obj1", time.Now().UTC().Truncate(time.Second))

	if err := store.Save(ctx, "test-bucket", checkpoint); err != nil {
		t.Fatalf("Save to file failed: %v", err)
	}

	expectedPath := filepath.Join(fallbackDir, "checkpoint_test-bucket.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Fallback file not created at %s", expectedPath)
	}

	loaded, err := store.Load(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("Load from file failed: %v", err)
	}

	if !loaded.LastSyncTime.Equal(checkpoint.LastSyncTime) {
		t.Errorf("LastSyncTime mismatch: got %v, want %v", loaded.LastSyncTime, checkpoint.LastSyncTime)
	}

	if len(loaded.ObjectVersions) != 1 {
		t.Errorf("ObjectVersions count mismatch: got %d, want 1", len(loaded.ObjectVersions))
	}
}

func TestCheckpointStore_RedisFailureFallback(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	checkpoint := NewSyncCheckpoint()
	checkpoint.UpdateObjectVersion("obj1", time.Now())

	if err := store.Save(ctx, "test-bucket", checkpoint); err != nil {
		redisPool.Close()
		mr.Close()
		t.Fatalf("Initial save failed: %v", err)
	}

	mr.Close()
	redisPool.Close()

	loaded, err := store.Load(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("Load after Redis failure should fallback to file: %v", err)
	}

	if loaded == nil {
		t.Error("Expected checkpoint to be loaded from fallback file")
	}

	if !store.IsDegraded() {
		t.Error("Expected store to be in degraded mode after Redis failure")
	}
}

func TestCheckpointStore_NilCheckpoint(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	err := store.Save(ctx, "test-bucket", nil)
	if err == nil {
		t.Error("Expected error when saving nil checkpoint")
	}
}

func TestCheckpointStore_LoadNonExistent(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	loaded, err := store.Load(ctx, "nonexistent-bucket")
	if err != nil {
		t.Fatalf("Load non-existent should not error: %v", err)
	}

	if loaded == nil {
		t.Error("Expected empty checkpoint, got nil")
	}

	if loaded.LastSyncTime != (time.Time{}) {
		t.Errorf("Expected zero LastSyncTime, got %v", loaded.LastSyncTime)
	}

	if loaded.ObjectVersions == nil {
		t.Error("ObjectVersions should be initialized")
	}
}

func TestSyncCheckpoint_UpdateObjectVersion(t *testing.T) {
	c := NewSyncCheckpoint()

	now := time.Now().UTC().Truncate(time.Second)
	c.UpdateObjectVersion("test-obj", now)

	if c.SyncedCount != 1 {
		t.Errorf("SyncedCount mismatch: got %d, want 1", c.SyncedCount)
	}

	if c.LastSyncTime.IsZero() {
		t.Error("LastSyncTime should be set")
	}

	modTime, ok := c.GetObjectVersion("test-obj")
	if !ok {
		t.Error("Object version should exist")
	}
	if !modTime.Equal(now) {
		t.Errorf("Object version mismatch: got %v, want %v", modTime, now)
	}
}

func TestSyncCheckpoint_AddAndRemoveFailedObject(t *testing.T) {
	c := NewSyncCheckpoint()

	c.AddFailedObject("fail1")
	c.AddFailedObject("fail2")

	if len(c.FailedObjects) != 2 {
		t.Errorf("FailedObjects count mismatch: got %d, want 2", len(c.FailedObjects))
	}

	c.RemoveFailedObject("fail1")

	if len(c.FailedObjects) != 1 {
		t.Errorf("FailedObjects count after remove: got %d, want 1", len(c.FailedObjects))
	}

	if c.FailedObjects[0] != "fail2" {
		t.Errorf("Remaining object mismatch: got %s, want fail2", c.FailedObjects[0])
	}
}

func TestSyncCheckpoint_AddFailedObject_Deduplication(t *testing.T) {
	c := NewSyncCheckpoint()

	c.AddFailedObject("fail1")
	c.AddFailedObject("fail1")
	c.AddFailedObject("fail1")

	if len(c.FailedObjects) != 1 {
		t.Errorf("Duplicate objects should be skipped: got %d, want 1", len(c.FailedObjects))
	}
}

func TestSyncCheckpoint_AddFailedObject_MaxLimit(t *testing.T) {
	c := NewSyncCheckpoint()

	for i := 0; i < MaxFailedObjects+100; i++ {
		c.AddFailedObject(string(rune(i)))
	}

	if len(c.FailedObjects) > MaxFailedObjects {
		t.Errorf("FailedObjects should be limited to %d, got %d", MaxFailedObjects, len(c.FailedObjects))
	}
}

func TestSyncCheckpoint_Clone(t *testing.T) {
	c := NewSyncCheckpoint()
	c.LastSyncTime = time.Now().UTC().Truncate(time.Second)
	c.SyncedCount = 10
	c.UpdateObjectVersion("obj1", time.Now())
	c.AddFailedObject("fail1")

	clone := c.Clone()

	if !clone.LastSyncTime.Equal(c.LastSyncTime) {
		t.Error("LastSyncTime not cloned correctly")
	}

	if clone.SyncedCount != c.SyncedCount {
		t.Error("SyncedCount not cloned correctly")
	}

	clone.UpdateObjectVersion("obj2", time.Now())
	if len(c.ObjectVersions) == len(clone.ObjectVersions) {
		t.Error("Cloning should create a copy, not share the map")
	}

	clone.AddFailedObject("fail2")
	if len(c.FailedObjects) == len(clone.FailedObjects) {
		t.Error("Cloning should create a copy, not share the slice")
	}
}

func TestCheckpointStore_ResetDegraded(t *testing.T) {
	store := NewCheckpointStore(nil, "test", "/tmp", zap.NewNop())

	if !store.IsDegraded() {
		t.Error("Store should start in degraded mode with nil RedisPool")
	}

	store.ResetDegraded()

	if store.IsDegraded() {
		t.Error("Store should not be degraded after reset")
	}
}

func TestCheckpointStore_MultipleBuckets(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	cp1 := NewSyncCheckpoint()
	cp1.UpdateObjectVersion("obj1", time.Now())
	if err := store.Save(ctx, "bucket1", cp1); err != nil {
		t.Fatalf("Save bucket1 failed: %v", err)
	}

	cp2 := NewSyncCheckpoint()
	cp2.UpdateObjectVersion("obj2", time.Now())
	if err := store.Save(ctx, "bucket2", cp2); err != nil {
		t.Fatalf("Save bucket2 failed: %v", err)
	}

	loaded1, err := store.Load(ctx, "bucket1")
	if err != nil {
		t.Fatalf("Load bucket1 failed: %v", err)
	}

	loaded2, err := store.Load(ctx, "bucket2")
	if err != nil {
		t.Fatalf("Load bucket2 failed: %v", err)
	}

	if _, ok := loaded1.ObjectVersions["obj1"]; !ok {
		t.Error("bucket1 should have obj1")
	}
	if _, ok := loaded1.ObjectVersions["obj2"]; ok {
		t.Error("bucket1 should NOT have obj2")
	}
	if _, ok := loaded2.ObjectVersions["obj2"]; !ok {
		t.Error("bucket2 should have obj2")
	}
	if _, ok := loaded2.ObjectVersions["obj1"]; ok {
		t.Error("bucket2 should NOT have obj1")
	}
}

func TestCheckpointStore_ConcurrentAccess(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	done := make(chan bool)

	for i := 0; i < 5; i++ {
		go func(id int) {
			bucket := "concurrent-bucket"
			for j := 0; j < 10; j++ {
				cp := NewSyncCheckpoint()
				cp.UpdateObjectVersion("obj", time.Now())
				_ = store.Save(ctx, bucket, cp)
				_, _ = store.Load(ctx, bucket)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestCheckpointStore_EmptyKeyPrefix(t *testing.T) {
	fallbackDir := t.TempDir()
	logger := zap.NewNop()
	store := NewCheckpointStore(nil, "", fallbackDir, logger)

	if store.keyPrefix != "miniodb:replication:checkpoint" {
		t.Errorf("Expected default key prefix, got %s", store.keyPrefix)
	}
}

func TestSyncCheckpoint_GetObjectVersion_NotExist(t *testing.T) {
	c := NewSyncCheckpoint()

	_, ok := c.GetObjectVersion("nonexistent")
	if ok {
		t.Error("Expected ok=false for non-existent object")
	}
}

func TestSyncCheckpoint_NilObjectVersions(t *testing.T) {
	c := &SyncCheckpoint{}

	_, ok := c.GetObjectVersion("test")
	if ok {
		t.Error("Expected ok=false when ObjectVersions is nil")
	}

	c.UpdateObjectVersion("test", time.Now())
	if c.ObjectVersions == nil {
		t.Error("ObjectVersions should be initialized")
	}
}

func TestCheckpointStore_ClearFromRedisError(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	cfg := pool.DefaultRedisPoolConfig()
	cfg.Addr = mr.Addr()
	logger := zap.NewNop()
	redisPool, err := pool.NewRedisPool(cfg, logger)
	if err != nil {
		mr.Close()
		t.Fatalf("Failed to create Redis pool: %v", err)
	}

	fallbackDir := t.TempDir()
	store := NewCheckpointStore(redisPool, "test:checkpoint", fallbackDir, logger)
	ctx := context.Background()

	cp := NewSyncCheckpoint()
	cp.UpdateObjectVersion("obj1", time.Now())
	if err := store.Save(ctx, "test-bucket", cp); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	mr.Close()
	redisPool.Close()

	err = store.Clear(ctx, "test-bucket")
	if err != nil {
		t.Errorf("Clear should not fail completely when Redis is unavailable: %v", err)
	}
}

func TestCheckpointStore_SaveToFileError(t *testing.T) {
	logger := zap.NewNop()
	invalidDir := "/nonexistent/path/that/cannot/be/created"
	store := NewCheckpointStore(nil, "test:checkpoint", invalidDir, logger)
	ctx := context.Background()

	cp := NewSyncCheckpoint()
	cp.UpdateObjectVersion("obj1", time.Now())

	os.Chmod("/nonexistent", 0000)
	defer os.Chmod("/nonexistent", 0755)

	err := store.Save(ctx, "test-bucket", cp)
	if err == nil {
		t.Error("Expected error when saving to invalid path")
	}
}

func TestCheckpointStore_RedisClientNil(t *testing.T) {
	fallbackDir := t.TempDir()
	logger := zap.NewNop()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	cfg := pool.DefaultRedisPoolConfig()
	cfg.Addr = mr.Addr()
	redisPool, err := pool.NewRedisPool(cfg, logger)
	if err != nil {
		mr.Close()
		t.Fatalf("Failed to create Redis pool: %v", err)
	}

	redisPool.Close()
	mr.Close()

	store := NewCheckpointStore(redisPool, "test:checkpoint", fallbackDir, logger)
	ctx := context.Background()

	cp := NewSyncCheckpoint()
	cp.UpdateObjectVersion("obj1", time.Now())

	err = store.Save(ctx, "test-bucket", cp)
	if err != nil {
		t.Errorf("Save should fallback to file when Redis client is unavailable: %v", err)
	}
}

func TestCheckpointStore_RedisGetError(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	cfg := pool.DefaultRedisPoolConfig()
	cfg.Addr = mr.Addr()
	logger := zap.NewNop()
	redisPool, err := pool.NewRedisPool(cfg, logger)
	if err != nil {
		mr.Close()
		t.Fatalf("Failed to create Redis pool: %v", err)
	}

	fallbackDir := t.TempDir()
	store := NewCheckpointStore(redisPool, "test:checkpoint", fallbackDir, logger)
	ctx := context.Background()

	cp := NewSyncCheckpoint()
	cp.UpdateObjectVersion("obj1", time.Now())
	if err := store.Save(ctx, "test-bucket", cp); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	mr.SetError("redis error")

	_, err = store.Load(ctx, "test-bucket")
	if err != nil {
		t.Errorf("Load should fallback to file on Redis error: %v", err)
	}
}

func TestCheckpointStore_SaveInvalidJSON(t *testing.T) {
	redisPool, mr := newTestRedisPool(t)
	defer mr.Close()
	defer redisPool.Close()

	store := newTestCheckpointStore(t, redisPool)
	ctx := context.Background()

	client := redisPool.GetClient()
	key := store.buildKey("test-bucket")
	client.Set(ctx, key, "invalid json data", 0)

	loaded, err := store.Load(ctx, "test-bucket")
	if err != nil {
		t.Errorf("Load should fallback to file on invalid JSON: %v", err)
	}

	if !store.IsDegraded() {
		t.Error("Store should be in degraded mode after Redis JSON error")
	}

	if loaded == nil {
		t.Error("Should return empty checkpoint from fallback file")
	}
}

func TestCheckpointStore_ClearFileNotExist(t *testing.T) {
	fallbackDir := t.TempDir()
	logger := zap.NewNop()
	store := NewCheckpointStore(nil, "test:checkpoint", fallbackDir, logger)
	ctx := context.Background()

	err := store.Clear(ctx, "nonexistent-bucket")
	if err != nil {
		t.Errorf("Clear should not fail when file does not exist: %v", err)
	}
}

func TestSyncCheckpoint_PruneOldEntries_LargeDataset(t *testing.T) {
	cp := NewSyncCheckpoint()
	now := time.Now()
	entryCount := 200000
	keep := 50000

	for i := 0; i < entryCount; i++ {
		cp.ObjectVersions[fmt.Sprintf("obj%d", i)] = now.Add(time.Duration(i) * time.Millisecond)
	}

	assert.Equal(t, entryCount, len(cp.ObjectVersions))

	cp.pruneOldEntries(keep)

	assert.Equal(t, keep, len(cp.ObjectVersions), "Should keep exactly %d entries", keep)

	for key := range cp.ObjectVersions {
		idxStr := strings.TrimPrefix(key, "obj")
		idx, err := strconv.Atoi(idxStr)
		require.NoError(t, err, "Key should have numeric suffix")
		assert.GreaterOrEqual(t, idx, entryCount-keep, "Should keep the most recent entries")
	}
}

func TestSyncCheckpoint_PruneOldEntries_KeepsNewest(t *testing.T) {
	cp := NewSyncCheckpoint()
	now := time.Now()

	cp.ObjectVersions["oldest"] = now.Add(-3 * time.Hour)
	cp.ObjectVersions["middle"] = now.Add(-2 * time.Hour)
	cp.ObjectVersions["newest"] = now.Add(-1 * time.Hour)
	cp.ObjectVersions["brand-new"] = now

	cp.pruneOldEntries(2)

	assert.Equal(t, 2, len(cp.ObjectVersions))
	assert.Contains(t, cp.ObjectVersions, "newest")
	assert.Contains(t, cp.ObjectVersions, "brand-new")
	assert.NotContains(t, cp.ObjectVersions, "oldest")
	assert.NotContains(t, cp.ObjectVersions, "middle")
}

func TestCheckpointStore_LoadPrunesOversizedCheckpoint(t *testing.T) {
	fallbackDir := t.TempDir()
	logger := zap.NewNop()
	store := NewCheckpointStore(nil, "test:prune", fallbackDir, logger)
	ctx := context.Background()

	largeCp := NewSyncCheckpoint()
	now := time.Now()
	entryCount := 150000
	for i := 0; i < entryCount; i++ {
		largeCp.ObjectVersions[fmt.Sprintf("obj%d", i)] = now.Add(time.Duration(i) * time.Millisecond)
	}

	err := store.Save(ctx, "test-bucket", largeCp)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "test-bucket")
	require.NoError(t, err)

	assert.LessOrEqual(t, len(loaded.ObjectVersions), maxCheckpointEntries,
		"Loaded checkpoint should be pruned to maxCheckpointEntries")
}

func BenchmarkPruneOldEntries(b *testing.B) {
	now := time.Now()

	benchmarks := []struct {
		name       string
		entryCount int
		keep       int
	}{
		{"1k_entries_keep_100", 1000, 100},
		{"10k_entries_keep_1k", 10000, 1000},
		{"50k_entries_keep_10k", 50000, 10000},
		{"100k_entries_keep_50k", 100000, 50000},
		{"200k_entries_keep_50k", 200000, 50000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			cp := NewSyncCheckpoint()
			for i := 0; i < bm.entryCount; i++ {
				cp.ObjectVersions[fmt.Sprintf("obj%d", i)] = now.Add(time.Duration(i) * time.Millisecond)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cpCopy := &SyncCheckpoint{
					ObjectVersions: make(map[string]time.Time, len(cp.ObjectVersions)),
				}
				for k, v := range cp.ObjectVersions {
					cpCopy.ObjectVersions[k] = v
				}
				cpCopy.pruneOldEntries(bm.keep)
			}
		})
	}
}
