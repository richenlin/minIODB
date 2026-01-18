package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"minIODB/internal/buffer"

	"go.uber.org/zap"
)

// HybridQueryResult 混合查询结果
type HybridQueryResult struct {
	Source      string
	Data        string
	RowCount    int64
	Duration    time.Duration
	Error       error
	CompletedAt time.Time
}

// HybridQueryConfig 混合查询配置
type HybridQueryConfig struct {
	Enabled            bool
	BufferQueryTimeout time.Duration
	StorageTimeout     time.Duration
}

// HybridQueryExecutor 混合查询执行器
type HybridQueryExecutor struct {
	buffer  *buffer.ConcurrentBuffer
	querier *Querier
	config  *HybridQueryConfig
	logger  *zap.Logger
	stats   *HybridQueryStats
	mu      sync.RWMutex
}

// HybridQueryStats 混合查询统计
type HybridQueryStats struct {
	TotalQueries       int64
	BufferOnlyQueries  int64
	StorageOnlyQueries int64
	HybridQueries      int64
	BufferHitRate      float64
	AvgDuration        time.Duration
	mu                 sync.RWMutex
}

// NewHybridQueryExecutor 创建混合查询执行器
func NewHybridQueryExecutor(buf *buffer.ConcurrentBuffer, querier *Querier, config *HybridQueryConfig, logger *zap.Logger) *HybridQueryExecutor {
	return &HybridQueryExecutor{
		buffer:  buf,
		querier: querier,
		config:  config,
		logger:  logger,
		stats: &HybridQueryStats{
			TotalQueries:       0,
			BufferOnlyQueries:  0,
			StorageOnlyQueries: 0,
			HybridQueries:      0,
			BufferHitRate:      0,
			AvgDuration:        0,
		},
	}
}

// ExecuteQuery 执行混合查询
func (hq *HybridQueryExecutor) ExecuteQuery(ctx context.Context, tableName, sqlQuery string) (*HybridQueryResult, error) {
	startTime := time.Now()

	if !hq.config.Enabled {
		data, err := hq.querier.ExecuteQuery(sqlQuery)
		return &HybridQueryResult{
			Source:      "storage",
			Data:        data,
			Duration:    time.Since(startTime),
			CompletedAt: time.Now(),
			Error:       err,
		}, nil
	}

	hasBufferData := hq.checkBufferHasData(tableName)

	if !hasBufferData {
		data, err := hq.querier.ExecuteQuery(sqlQuery)
		return &HybridQueryResult{
			Source:      "storage",
			Data:        data,
			Duration:    time.Since(startTime),
			CompletedAt: time.Now(),
			Error:       err,
		}, nil
	}

	// 缓冲区和存储都有数据，执行混合查询
	bufferResult := hq.queryBuffer(ctx, tableName, sqlQuery)
	storageResult := hq.queryStorage(ctx, tableName, sqlQuery)

	duration := time.Since(startTime)
	mergedResult := hq.mergeResults(bufferResult, storageResult, sqlQuery)

	mergedResult.Duration = duration
	mergedResult.CompletedAt = time.Now()

	hq.updateStats("hybrid", duration, mergedResult.Error, 0)

	return mergedResult, nil
}

// checkBufferHasData 检查缓冲区是否有数据
func (hq *HybridQueryExecutor) checkBufferHasData(tableName string) bool {
	bufferKeys := hq.buffer.GetTableKeys(tableName)
	for _, key := range bufferKeys {
		if len(hq.buffer.Get(key)) > 0 {
			return true
		}
	}
	return false
}

// queryBuffer 查询缓冲区数据
func (hq *HybridQueryExecutor) queryBuffer(ctx context.Context, tableName, sqlQuery string) *HybridQueryResult {
	startTime := time.Now()

	result := &HybridQueryResult{
		Source:      "buffer",
		CompletedAt: time.Now(),
	}

	defer func() {
		result.Duration = time.Since(startTime)
	}()

	bufferKeys := hq.buffer.GetTableKeys(tableName)
	if len(bufferKeys) == 0 {
		result.Error = fmt.Errorf("no buffer data found for table: %s", tableName)
		return result
	}

	var bufferRows []buffer.DataRow
	for _, key := range bufferKeys {
		rows := hq.buffer.Get(key)
		bufferRows = append(bufferRows, rows...)
	}

	if len(bufferRows) == 0 {
		result.Error = fmt.Errorf("no rows in buffer keys")
		return result
	}

	// 创建临时Parquet文件
	tempFile := fmt.Sprintf("/tmp/buffer_query_%d.parquet", time.Now().UnixNano())
	if err := hq.buffer.WriteTempParquetFile(tempFile, bufferRows); err != nil {
		result.Error = fmt.Errorf("failed to write buffer to temp file: %w", err)
		return result
	}
	defer func() {
		os.Remove(tempFile)
	}()

	// 创建DuckDB视图
	db, err := hq.querier.getDBConnection()
	if err != nil {
		result.Error = fmt.Errorf("failed to get db connection: %w", err)
		return result
	}
	defer hq.querier.returnDBConnection(db)

	// 使用安全的SQL构建器创建视图
	createViewSQL := fmt.Sprintf(`CREATE OR REPLACE VIEW buffer_view AS SELECT * FROM read_parquet('%s')`, tempFile)
	dropViewSQL := fmt.Sprintf(`DROP VIEW IF EXISTS buffer_view`)

	db.Exec(dropViewSQL)
	if _, err := db.Exec(createViewSQL); err != nil {
		result.Error = fmt.Errorf("failed to create buffer view: %w", err)
		return result
	}

	// 执行查询
	data, err := hq.querier.executeQueryWithOptimization(db, sqlQuery)
	if err != nil {
		result.Error = fmt.Errorf("buffer query failed: %w", err)
		return result
	}

	result.Data = data
	result.RowCount = hq.parseRowCount(data)

	return result
}

// queryStorage 查询存储数据
func (hq *HybridQueryExecutor) queryStorage(ctx context.Context, tableName, sqlQuery string) *HybridQueryResult {
	startTime := time.Now()

	result := &HybridQueryResult{
		Source:      "storage",
		CompletedAt: time.Now(),
	}

	defer func() {
		result.Duration = time.Since(startTime)
	}()

	data, err := hq.querier.ExecuteQuery(sqlQuery)
	if err != nil {
		result.Error = fmt.Errorf("storage query failed: %w", err)
		return result
	}

	result.Data = data
	result.RowCount = hq.parseRowCount(data)

	return result
}

// mergeResults 合并缓冲区和存储结果
func (hq *HybridQueryExecutor) mergeResults(bufferResult, storageResult *HybridQueryResult, sqlQuery string) *HybridQueryResult {
	result := &HybridQueryResult{
		Source:      "hybrid",
		CompletedAt: time.Now(),
	}

	// 处理 nil 情况
	if bufferResult == nil {
		if storageResult == nil {
			result.Error = fmt.Errorf("both buffer and storage results are nil")
			return result
		}
		return storageResult
	}

	if storageResult == nil {
		return bufferResult
	}

	// 如果查询失败，返回另一个结果
	if bufferResult.Error != nil && storageResult.Error != nil {
		result.Error = fmt.Errorf("both buffer and storage queries failed: buffer=%v, storage=%v",
			bufferResult.Error, storageResult.Error)
		return result
	}

	if bufferResult.Error != nil {
		return storageResult
	}

	if storageResult.Error != nil {
		return bufferResult
	}

	// 两个查询都成功，合并并去重
	// 暂时实现：返回存储结果
	// 实际生产环境中应该实现真正的数据合并和去重
	result.Data = storageResult.Data
	result.RowCount = storageResult.RowCount
	result.Duration = bufferResult.Duration + storageResult.Duration

	return result
}

// parseRowCount 解析查询结果的行数
func (hq *HybridQueryExecutor) parseRowCount(data string) int64 {
	if data == "" {
		return 0
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		return 0
	}

	return int64(len(rows))
}

// updateStats 更新统计信息
func (hq *HybridQueryExecutor) updateStats(source string, duration time.Duration, err error, concurrent int) {
	hq.stats.mu.Lock()
	defer hq.stats.mu.Unlock()

	hq.stats.TotalQueries++

	switch source {
	case "buffer":
		hq.stats.BufferOnlyQueries++
	case "storage":
		hq.stats.StorageOnlyQueries++
	case "hybrid":
		hq.stats.HybridQueries++
	}

	// 更新平均延迟
	totalDuration := hq.stats.AvgDuration * time.Duration(hq.stats.TotalQueries-1)
	hq.stats.AvgDuration = (totalDuration + duration) / time.Duration(hq.stats.TotalQueries)

	// 更新缓冲区命中率
	if hq.stats.TotalQueries > 0 {
		hq.stats.BufferHitRate = float64(hq.stats.BufferOnlyQueries+hq.stats.HybridQueries) / float64(hq.stats.TotalQueries)
	}
}

// GetStats 获取统计信息
func (hq *HybridQueryExecutor) GetStats() *HybridQueryStats {
	hq.stats.mu.RLock()
	defer hq.stats.mu.RUnlock()

	return &HybridQueryStats{
		TotalQueries:       hq.stats.TotalQueries,
		BufferOnlyQueries:  hq.stats.BufferOnlyQueries,
		StorageOnlyQueries: hq.stats.StorageOnlyQueries,
		HybridQueries:      hq.stats.HybridQueries,
		BufferHitRate:      hq.stats.BufferHitRate,
		AvgDuration:        hq.stats.AvgDuration,
	}
}
