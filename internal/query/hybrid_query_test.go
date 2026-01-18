package query

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/buffer"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewHybridQueryExecutor(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled:            true,
		BufferQueryTimeout: 5 * time.Second,
		StorageTimeout:     10 * time.Second,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	assert.NotNil(t, executor)
	assert.Equal(t, buf, executor.buffer)
	assert.Equal(t, config, executor.config)
	assert.NotNil(t, executor.stats)
	assert.Equal(t, int64(0), executor.stats.TotalQueries)
}

func TestExecuteQuery_Disabled(t *testing.T) {
	t.Skip("需要完整的Querier实例，跳过集成测试")

	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: false,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	ctx := context.Background()
	result, err := executor.ExecuteQuery(ctx, "test_table", "SELECT * FROM test_table")

	assert.NotNil(t, err)
	assert.Equal(t, "storage", result.Source)
}

func TestExecuteQuery_NoBufferData(t *testing.T) {
	t.Skip("需要完整的Querier实例，跳过集成测试")

	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	ctx := context.Background()
	result, err := executor.ExecuteQuery(ctx, "test_table", "SELECT * FROM test_table")

	assert.NotNil(t, err)
	assert.Equal(t, "storage", result.Source)
}

func TestUpdateStats_BufferOnly(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	duration := 100 * time.Millisecond
	executor.updateStats("buffer", duration, nil, 0)

	assert.Equal(t, int64(1), executor.stats.TotalQueries)
	assert.Equal(t, int64(1), executor.stats.BufferOnlyQueries)
	assert.Equal(t, duration, executor.stats.AvgDuration)
	assert.Equal(t, float64(1), executor.stats.BufferHitRate)
}

func TestUpdateStats_StorageOnly(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	duration := 200 * time.Millisecond
	executor.updateStats("storage", duration, nil, 0)

	assert.Equal(t, int64(1), executor.stats.TotalQueries)
	assert.Equal(t, int64(1), executor.stats.StorageOnlyQueries)
	assert.Equal(t, duration, executor.stats.AvgDuration)
	assert.Equal(t, float64(0), executor.stats.BufferHitRate)
}

func TestUpdateStats_Hybrid(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	duration := 150 * time.Millisecond
	executor.updateStats("hybrid", duration, nil, 0)

	assert.Equal(t, int64(1), executor.stats.TotalQueries)
	assert.Equal(t, int64(1), executor.stats.HybridQueries)
	assert.Equal(t, duration, executor.stats.AvgDuration)
	assert.Equal(t, float64(1), executor.stats.BufferHitRate)
}

func TestUpdateStats_AverageDuration(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	executor.updateStats("buffer", 100*time.Millisecond, nil, 0)
	executor.updateStats("storage", 200*time.Millisecond, nil, 0)

	expectedAvg := (100*time.Millisecond + 200*time.Millisecond) / 2
	assert.Equal(t, expectedAvg, executor.stats.AvgDuration)
}

func TestGetStats(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	executor.updateStats("buffer", 100*time.Millisecond, nil, 0)
	executor.updateStats("storage", 200*time.Millisecond, nil, 0)
	executor.updateStats("hybrid", 150*time.Millisecond, nil, 0)

	stats := executor.GetStats()

	assert.Equal(t, int64(3), stats.TotalQueries)
	assert.Equal(t, int64(1), stats.BufferOnlyQueries)
	assert.Equal(t, int64(1), stats.StorageOnlyQueries)
	assert.Equal(t, int64(1), stats.HybridQueries)
	assert.InDelta(t, 0.6667, stats.BufferHitRate, 0.01)
	assert.Equal(t, 150*time.Millisecond, stats.AvgDuration)
}

func TestParseRowCount(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	assert.Equal(t, int64(0), executor.parseRowCount(""))

	data := `[{"id": 1}, {"id": 2}, {"id": 3}]`
	count := executor.parseRowCount(data)
	assert.Equal(t, int64(3), count)

	invalidJSON := `{invalid json}`
	count = executor.parseRowCount(invalidJSON)
	assert.Equal(t, int64(0), count)

	emptyArray := `[]`
	count = executor.parseRowCount(emptyArray)
	assert.Equal(t, int64(0), count)
}

func TestMergeResults_BothErrors(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	bufferResult := &HybridQueryResult{
		Error: assert.AnError,
	}
	storageResult := &HybridQueryResult{
		Error: assert.AnError,
	}

	result := executor.mergeResults(bufferResult, storageResult, "")
	assert.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Error(), "both buffer and storage queries failed")
}

func TestMergeResults_BufferError(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	bufferResult := &HybridQueryResult{
		Error: assert.AnError,
	}
	storageResult := &HybridQueryResult{
		Data:     "storage data",
		RowCount: 10,
	}

	result := executor.mergeResults(bufferResult, storageResult, "")
	assert.Equal(t, storageResult, result)
}

func TestMergeResults_StorageError(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	bufferResult := &HybridQueryResult{
		Data:     "buffer data",
		RowCount: 5,
	}
	storageResult := &HybridQueryResult{
		Error: assert.AnError,
	}

	result := executor.mergeResults(bufferResult, storageResult, "")
	assert.Equal(t, bufferResult, result)
}

func TestMergeResults_Success(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	bufferResult := &HybridQueryResult{
		Data:     `[{"id": 1}]`,
		RowCount: 1,
		Duration: 100 * time.Millisecond,
	}
	storageResult := &HybridQueryResult{
		Data:     `[{"id": 2}, {"id": 3}]`,
		RowCount: 2,
		Duration: 200 * time.Millisecond,
	}

	result := executor.mergeResults(bufferResult, storageResult, "")
	assert.Equal(t, "hybrid", result.Source)
	assert.Equal(t, storageResult.Data, result.Data)
	assert.Equal(t, storageResult.RowCount, result.RowCount)
	assert.Equal(t, 300*time.Millisecond, result.Duration)
}

func TestMergeResults_NilStorageResult(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	bufferResult := &HybridQueryResult{
		Data:     "buffer data",
		RowCount: 5,
	}
	storageResult := (*HybridQueryResult)(nil)

	result := executor.mergeResults(bufferResult, storageResult, "")
	assert.Equal(t, bufferResult, result)
}

func TestHybridQueryStats_Concurrency(t *testing.T) {
	logger := zap.NewNop()
	buf := createTestBuffer(t)
	config := &HybridQueryConfig{
		Enabled: true,
	}

	executor := NewHybridQueryExecutor(buf, nil, config, logger)

	done := make(chan bool)

	for i := 0; i < 100; i++ {
		go func() {
			executor.updateStats("buffer", 100*time.Millisecond, nil, 0)
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	assert.Equal(t, int64(100), executor.stats.TotalQueries)
	assert.Equal(t, int64(100), executor.stats.BufferOnlyQueries)
	assert.Equal(t, float64(1), executor.stats.BufferHitRate)
}

func createTestBuffer(t *testing.T) *buffer.ConcurrentBuffer {
	bufferConfig := &buffer.ConcurrentBufferConfig{
		BufferSize:     1000,
		FlushInterval:  30 * time.Second,
		WorkerPoolSize: 5,
		TaskQueueSize:  100,
		BatchFlushSize: 100,
		EnableBatching: true,
		FlushTimeout:   5 * time.Second,
		MaxRetries:     3,
		RetryDelay:     100 * time.Millisecond,
	}

	buf := buffer.NewConcurrentBuffer(nil, nil, "test-bucket", "test-node", bufferConfig)

	return buf
}
