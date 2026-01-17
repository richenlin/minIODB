package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"minIODB/internal/buffer"

	"go.uber.org/zap"
)

// HybridQueryResult 混合查询结果
type HybridQueryResult struct {
	Source      string // "buffer", "storage", "hybrid"
	Data        string // JSON格式的查询结果
	RowCount    int64  // 结果行数
	Duration    time.Duration
	Error       error
	CompletedAt time.Time
}

// HybridQueryConfig 混合查询配置
type HybridQueryConfig struct {
	Enabled            bool          `yaml:"enabled"`              // 是否启用混合查询
	BufferQueryTimeout time.Duration `yaml:"buffer_query_timeout"` // 缓冲区查询超时
	StorageTimeout     time.Duration `yaml:"storage_timeout"`      // 存储查询超时
	MaxConcurrent      int           `yaml:"max_concurrent"`       // 最大并发查询数
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
	TotalQueries       int64         `json:"total_queries"`
	BufferOnlyQueries  int64         `json:"buffer_only_queries"`
	StorageOnlyQueries int64         `json:"storage_only_queries"`
	HybridQueries      int64         `json:"hybrid_queries"`
	BufferHitRate      float64       `json:"buffer_hit_rate"`
	AvgDuration        time.Duration `json:"avg_duration"`
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
		},
	}
}

// ExecuteQuery 执行混合查询
func (hq *HybridQueryExecutor) ExecuteQuery(ctx context.Context, tableName, sqlQuery string) (*HybridQueryResult, error) {
	startTime := time.Now()

	if !hq.config.Enabled {
		// 混合查询未启用，使用普通查询
		hq.updateStats("storage", time.Since(startTime), nil, 1)
		return &HybridQueryResult{
			Source:      "storage",
			Duration:    time.Since(startTime),
			CompletedAt: time.Now(),
		}, hq.querier.ExecuteQuery(sqlQuery)
	}

	// 检查缓冲区是否有数据
	bufferKeys := hq.buffer.GetTableKeys(tableName)
	hasBufferData := false
	for _, key := range bufferKeys {
		if len(hq.buffer.Get(key)) > 0 {
			hasBufferData = true
			break
		}
	}

	if !hasBufferData {
		// 缓冲区无数据，直接查询存储
		hq.updateStats("storage", time.Since(startTime), nil, 0)
		return &HybridQueryResult{
			Source:      "storage",
			Duration:    time.Since(startTime),
			CompletedAt: time.Now(),
		}, hq.querier.ExecuteQuery(sqlQuery)
	}

	// 缓冲区有数据，执行混合查询
	bufferResultCh := make(chan *HybridQueryResult, 1)
	storageResultCh := make(chan *HybridQueryResult, 1)

	var wg sync.WaitGroup
	wg.Add(2)

	// 并行查询缓冲区和存储
	go func() {
		defer wg.Done()
		result := hq.queryBuffer(ctx, tableName, sqlQuery)
		bufferResultCh <- result
	}()

	go func() {
		defer wg.Done()
		result := hq.queryStorage(ctx, tableName, sqlQuery)
		storageResultCh <- result
	}()

	// 等待两个查询完成
	go func() {
		wg.Wait()
		close(bufferResultCh)
		close(storageResultCh)
	}()

	// 收集结果
	bufferResult := <-bufferResultCh
	storageResult := <-storageResultCh

	// 合并结果
	mergedResult := hq.mergeResults(bufferResult, storageResult, sqlQuery)

	duration := time.Since(startTime)
	mergedResult.Duration = duration
	mergedResult.CompletedAt = time.Now()

	// 更新统计
	var source string
	if bufferResult.Error == nil && storageResult.Error == nil {
		source = "hybrid"
	} else if bufferResult.Error == nil {
		source = "buffer"
	} else if storageResult.Error == nil {
		source = "storage"
	} else {
		source = "error"
	}

	hq.updateStats(source, duration, mergedResult.Error, 0)

	return mergedResult, nil
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

	// 获取缓冲区数据
	bufferKeys := hq.buffer.GetTableKeys(tableName)
	var allRows []buffer.DataRow

	for _, key := range bufferKeys {
		rows := hq.buffer.Get(key)
		allRows = append(allRows, rows...)
	}

	if len(allRows) == 0 {
		result.Error = fmt.Errorf("no buffer data found for table: %s", tableName)
		return result
	}

	// 创建临时Parquet文件并查询
	tempFile := fmt.Sprintf("/tmp/buffer_query_%d.parquet", time.Now().UnixNano())

	if err := hq.buffer.WriteTempParquetFile(tempFile, allRows); err != nil {
		result.Error = fmt.Errorf("failed to write buffer to temp file: %w", err)
		return result
	}

	defer func() {
		// 清理临时文件
		// os.Remove(tempFile)
	}()

	// 创建DuckDB视图并查询
	db, err := hq.querier.getDBConnection()
	if err != nil {
		result.Error = fmt.Errorf("failed to get db connection: %w", err)
		return result
	}
	defer hq.querier.returnDBConnection(db)

	createSQL := fmt.Sprintf(`CREATE OR REPLACE VIEW buffer_view AS SELECT * FROM read_parquet('%s')`, tempFile)
	if _, err := db.Exec(createSQL); err != nil {
		result.Error = fmt.Errorf("failed to create buffer view: %w", err)
		return result
	}

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

	// 使用标准查询流程
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

	if bufferResult.Error != nil && storageResult.Error != nil {
		result.Error = fmt.Errorf("both buffer and storage queries failed: buffer=%v, storage=%v",
			bufferResult.Error, storageResult.Error)
		return result
	}

	if bufferResult.Error != nil {
		// 缓冲区查询失败，返回存储结果
		result.Source = "storage"
		result.Data = storageResult.Data
		result.RowCount = storageResult.RowCount
		result.Duration = storageResult.Duration
		return result
	}

	if storageResult.Error != nil {
	// 存储查询失败，返回缓冲区结果
		result.Source = "buffer"
		result.Data = bufferResult.Data
		result.RowCount = bufferResult.RowCount
		result.Duration = bufferResult.Duration
		return result
	}

	// 缓冲区查询成功，返回存储查询
	result.Source = "storage"
		result.Data = storageResult.Data
	result.RowCount = storageResult.RowCount
		result.Duration = storageResult.Duration
	return result
}

	// 两者都成功，合并结果并去重
	mergedData, err := hq.mergeAndDeduplicate(bufferResult.Data, storageResult.Data, sqlQuery)
	if err != nil {
		result.Error = fmt.Errorf("failed to merge results: %w", err)
		return result
	}

	result.Data = mergedData
	result.RowCount = hq.parseRowCount(mergedData)
	result.Duration = bufferResult.Duration + storageResult.Duration

	return result
}

// mergeAndDeduplicate 合并结果并去重
func (hq *HybridQueryExecutor) mergeAndDeduplicate(bufferData, storageData, sqlQuery string) (string, error) {
	// 简化实现：合并两个JSON数组
	// 实际生产环境中需要根据SQL类型和表结构进行更复杂的去重

	if bufferData == "" {
		return storageData, nil
	}

	if storageData == "" {
		return bufferData, nil
	}

	// 解析JSON数组
	var bufferRows, storageRows []map[string]interface{}

	if err := json.Unmarshal([]byte(bufferData), &bufferRows); err != nil {
		return storageData, nil
	}

	if err := json.Unmarshal([]byte(storageData), &storageRows); err != nil {
		return bufferData, nil
	}

	// 创建去重集合
	seen := make(map[string]bool)
	var mergedRows []map[string]interface{}

	// 添加缓冲区数据
	for _, row := range bufferRows {
		key := hq.getRowKey(row)
		if !seen[key] {
			seen[key] = true
			mergedRows = append(mergedRows, row)
		}
	}

	// 添加存储数据（跳过已存在的）
	for _, row := range storageRows {
		key := hq.getRowKey(row)
		if !seen[key] {
			seen[key] = true
			mergedRows = append(mergedRows, row)
		}
	}

	// 序列化合并后的结果
	mergedJSON, err := json.Marshal(mergedRows)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged results: %w", err)
	}

	return string(mergedJSON), nil
}

// getRowKey 获取行的唯一键（用于去重）
func (hq *HybridQueryExecutor) getRowKey(row map[string]interface{}) string {
	// 简化实现：使用ID和时间戳作为键
	// 实际生产环境中应该使用所有主键和关键列

	id, ok := row["id"].(string)
	if !ok {
		id = ""
	}

	timestamp, ok := row["timestamp"].(float64)
	if !ok {
		timestamp = 0
	}

	return fmt.Sprintf("%s_%d", id, timestamp)
}

// parseRowCount 解析查询结果的行数
func (hq *HybridQueryExecutor) parseRowCount(data string) int64 {
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
