package buffer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"minIODB/config"
	"minIODB/internal/metrics"
	"minIODB/internal/storage"
	"minIODB/internal/wal"
	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
	"go.uber.org/zap"
)

// bufferKeyPool 用于复用 strings.Builder，减少 bufferKey 构建时的内存分配
// bufferKey 格式: tableName/idPart/dayStr，通常长度约 30-50 字节
var bufferKeyPool = sync.Pool{
	New: func() interface{} {
		return new(strings.Builder)
	},
}

// getBufferKeyBuilder 从池中获取 strings.Builder
func getBufferKeyBuilder() *strings.Builder {
	return bufferKeyPool.Get().(*strings.Builder)
}

// putBufferKeyBuilder 将 strings.Builder 放回池中
func putBufferKeyBuilder(sb *strings.Builder) {
	sb.Reset()
	bufferKeyPool.Put(sb)
}

// emptyIDPlaceholder 空 ID 时的占位符，避免 bufferKey 出现 "table//date" 导致 MinIO 对象名非法
const emptyIDPlaceholder = "_"

// walFailThreshold WAL 连续失败阈值，超过此值触发紧急 flush
const walFailThreshold = 3

// normalizeObjectKey 合并多余斜杠并去掉首尾 "/"，避免 MinIO 报 "Object name contains unsupported characters"
func normalizeObjectKey(key string) string {
	return strings.Trim(path.Clean(key), "/")
}

// DataRow defines the structure for our Parquet file records.
// Fields are written as top-level Parquet columns via JSONWriter for DuckDB compatibility.
type DataRow struct {
	ID        string                 `json:"id"`
	Timestamp int64                  `json:"timestamp"`
	Table     string                 `json:"table"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

const tempDir = "temp_parquet"

// ConcurrentBufferConfig 并发缓冲区配置
type ConcurrentBufferConfig struct {
	BufferSize     int           `yaml:"buffer_size"`       // 缓冲区大小
	FlushInterval  time.Duration `yaml:"flush_interval"`    // 刷新间隔
	WorkerPoolSize int           `yaml:"worker_pool_size"`  // 工作池大小
	TaskQueueSize  int           `yaml:"task_queue_size"`   // 任务队列大小
	BatchFlushSize int           `yaml:"batch_flush_size"`  // 批量刷新大小
	EnableBatching bool          `yaml:"enable_batching"`   // 启用批量处理
	FlushTimeout   time.Duration `yaml:"flush_timeout"`     // 刷新超时
	MaxRetries     int           `yaml:"max_retries"`       // 最大重试次数
	RetryDelay     time.Duration `yaml:"retry_delay"`       // 重试延迟
	WALEnabled     bool          `yaml:"wal_enabled"`       // 是否启用 WAL
	WALDir         string        `yaml:"wal_dir"`           // WAL 目录
	WALSyncOnWrite bool          `yaml:"wal_sync_on_write"` // WAL 每次写入是否同步
}

// walDataRow WAL 中存储的数据行格式
type walDataRow struct {
	BufferKey string  `json:"buffer_key"`
	Row       DataRow `json:"row"`
}

// walTombstoneRow WAL 中存储的墓碑记录格式
type walTombstoneRow struct {
	TableName string `json:"table_name"`
	ID        string `json:"id"`
}

// DefaultConcurrentBufferConfig 返回默认配置
func DefaultConcurrentBufferConfig() *ConcurrentBufferConfig {
	return &ConcurrentBufferConfig{
		BufferSize:     1000,
		FlushInterval:  30 * time.Second,
		WorkerPoolSize: 10,
		TaskQueueSize:  100,
		BatchFlushSize: 5,
		EnableBatching: true,
		FlushTimeout:   60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
		WALEnabled:     true,
		WALDir:         "data/wal",
		WALSyncOnWrite: true,
	}
}

// FlushTask 刷新任务
type FlushTask struct {
	BufferKey string
	Rows      []DataRow
	Priority  int // 任务优先级，数字越小优先级越高
	CreatedAt time.Time
	Retries   int
	MaxSeqNum uint64 // WAL 中此批次数据的最大序列号，用于成功后截断
}

// ConcurrentBuffer 支持并发刷新的缓冲区
type ConcurrentBuffer struct {
	config       *ConcurrentBufferConfig
	appConfig    *config.Config
	poolManager  *pool.PoolManager
	backupBucket string
	nodeID       string

	// 缓冲区数据
	buffer map[string][]DataRow
	mutex  sync.RWMutex

	// WAL 持久化
	wal               *wal.WAL
	walEnabled        bool
	lastFlushedSeq    map[string]uint64 // bufferKey -> lastSeqNum
	walSeqMutex       sync.RWMutex
	recoveryMaxSeqNum uint64 // WAL 恢复时的最大 seqNum，恢复完成后用于全量截断

	// 工作池相关
	taskQueue    chan *FlushTask
	workers      []*Worker
	workerWg     sync.WaitGroup
	shutdown     chan struct{}
	shutdownOnce sync.Once

	// 统计信息
	stats *ConcurrentBufferStats

	// pendingWrites 使用 atomic.Int64 维护待写入计数，避免锁内遍历 buffer
	pendingWrites atomic.Int64

	// WAL 降级计数器
	walFailCount atomic.Int64 // 连续 WAL 失败计数
	walDegraded  atomic.Bool  // 是否处于降级模式

	logger *zap.Logger
}

// ConcurrentBufferStats 并发缓冲区统计信息
type ConcurrentBufferStats struct {
	TotalTasks     int64 `json:"total_tasks"`
	CompletedTasks int64 `json:"completed_tasks"`
	FailedTasks    int64 `json:"failed_tasks"`
	QueuedTasks    int64 `json:"queued_tasks"`
	ActiveWorkers  int64 `json:"active_workers"`
	AvgFlushTime   int64 `json:"avg_flush_time_ms"`
	TotalFlushTime int64 `json:"total_flush_time_ms"`
	LastFlushTime  int64 `json:"last_flush_time"`
	BufferSize     int64 `json:"buffer_size"`
	PendingWrites  int64 `json:"pending_writes"`
	mutex          sync.RWMutex
}

// Worker 工作线程
type Worker struct {
	id       int
	buffer   *ConcurrentBuffer
	active   int64 // 原子操作标志
	stopChan chan struct{}
}

// NewConcurrentBuffer 创建新的并发缓冲区
func NewConcurrentBuffer(poolManager *pool.PoolManager, appConfig *config.Config,
	backupBucket, nodeID string, config *ConcurrentBufferConfig, logger *zap.Logger) *ConcurrentBuffer {
	if config == nil {
		config = DefaultConcurrentBufferConfig()
	}

	cb := &ConcurrentBuffer{
		config:         config,
		appConfig:      appConfig,
		poolManager:    poolManager,
		backupBucket:   backupBucket,
		nodeID:         nodeID,
		buffer:         make(map[string][]DataRow),
		taskQueue:      make(chan *FlushTask, config.TaskQueueSize),
		workers:        make([]*Worker, config.WorkerPoolSize),
		shutdown:       make(chan struct{}),
		stats:          &ConcurrentBufferStats{},
		walEnabled:     config.WALEnabled,
		lastFlushedSeq: make(map[string]uint64),
		logger:         logger,
	}

	// 初始化 WAL (如果启用)
	if config.WALEnabled {
		walCfg := wal.DefaultConfig(config.WALDir)
		walCfg.SyncOnWrite = config.WALSyncOnWrite

		w, err := wal.New(walCfg, cb.logger)
		if err != nil {
			cb.logger.Error("Failed to initialize WAL, continuing without WAL",
				zap.Error(err),
				zap.String("wal_dir", config.WALDir))
			cb.walEnabled = false
		} else {
			cb.wal = w
			cb.logger.Info("WAL initialized successfully",
				zap.String("wal_dir", config.WALDir),
				zap.Bool("sync_on_write", config.WALSyncOnWrite))

			// 从 WAL 恢复数据
			if err := cb.recoverFromWAL(); err != nil {
				cb.logger.Warn("Failed to recover from WAL",
					zap.Error(err))
			}
		}
	}

	// 启动工作线程
	cb.startWorkers()

	// 启动定期刷新
	go cb.periodicFlush()

	// WAL 恢复后，等待 buffer 排空，再做一次全量 WAL 截断
	if cb.walEnabled && cb.wal != nil && cb.recoveryMaxSeqNum > 0 {
		go cb.waitAndTruncateRecoveryWAL()
	}

	cb.logger.Info("Concurrent buffer initialized",
		zap.Int("worker_pool_size", config.WorkerPoolSize),
		zap.Int("task_queue_size", config.TaskQueueSize),
		zap.Bool("wal_enabled", cb.walEnabled))

	return cb
}

// recoverFromWAL 从 WAL 恢复数据
func (cb *ConcurrentBuffer) recoverFromWAL() error {
	if cb.wal == nil {
		return nil
	}

	recoveredCount := 0
	tombstoneCount := 0
	var maxSeqNum uint64

	tombstones := make(map[string]struct{})

	err := cb.wal.Replay(func(record *wal.Record) error {
		if record.SeqNum > maxSeqNum {
			maxSeqNum = record.SeqNum
		}

		switch record.Type {
		case wal.RecordTypeData:
			var wr walDataRow
			if err := json.Unmarshal(record.Data, &wr); err != nil {
				cb.logger.Warn("Failed to unmarshal WAL data record during recovery",
					zap.Error(err),
					zap.Uint64("seq_num", record.SeqNum))
				return nil
			}

			tombstoneKey := wr.Row.Table + "/" + wr.Row.ID
			if _, isTombstoned := tombstones[tombstoneKey]; isTombstoned {
				return nil
			}

			cb.buffer[wr.BufferKey] = append(cb.buffer[wr.BufferKey], wr.Row)

			cb.walSeqMutex.Lock()
			if record.SeqNum > cb.lastFlushedSeq[wr.BufferKey] {
				cb.lastFlushedSeq[wr.BufferKey] = record.SeqNum
			}
			cb.walSeqMutex.Unlock()

			recoveredCount++

		case wal.RecordTypeTombstone:
			var tr walTombstoneRow
			if err := json.Unmarshal(record.Data, &tr); err != nil {
				cb.logger.Warn("Failed to unmarshal WAL tombstone record during recovery",
					zap.Error(err),
					zap.Uint64("seq_num", record.SeqNum))
				return nil
			}

			tombstoneKey := tr.TableName + "/" + tr.ID
			tombstones[tombstoneKey] = struct{}{}

			prefix := tr.TableName + "/"
			for bufferKey, rows := range cb.buffer {
				if !strings.HasPrefix(bufferKey, prefix) {
					continue
				}
				newRows := make([]DataRow, 0, len(rows))
				for _, row := range rows {
					if row.ID != tr.ID {
						newRows = append(newRows, row)
					}
				}
				if len(newRows) == 0 {
					delete(cb.buffer, bufferKey)
				} else {
					cb.buffer[bufferKey] = newRows
				}
			}

			tombstoneCount++
			cb.logger.Debug("Applied WAL tombstone during recovery",
				zap.String("table", tr.TableName),
				zap.String("id", tr.ID),
				zap.Uint64("seq_num", record.SeqNum))
		}

		return nil
	})

	if err != nil {
		return err
	}

	if recoveredCount > 0 || tombstoneCount > 0 {
		cb.recoveryMaxSeqNum = maxSeqNum
		cb.logger.Info("Recovered data from WAL",
			zap.Int("records", recoveredCount),
			zap.Int("tombstones", tombstoneCount),
			zap.Int("buffer_keys", len(cb.buffer)),
			zap.Uint64("max_seq_num", maxSeqNum))

		cb.pendingWrites.Store(int64(recoveredCount))
		cb.updateStats(func(stats *ConcurrentBufferStats) {
			stats.BufferSize = int64(len(cb.buffer))
			stats.PendingWrites = cb.pendingWrites.Load()
		})
	}

	return nil
}

// startWorkers 启动工作线程
func (cb *ConcurrentBuffer) startWorkers() {
	for i := 0; i < cb.config.WorkerPoolSize; i++ {
		worker := &Worker{
			id:       i,
			buffer:   cb,
			stopChan: make(chan struct{}),
		}
		cb.workers[i] = worker
		cb.workerWg.Add(1)
		go worker.run()
	}
}

// Worker.run 工作线程主循环
func (w *Worker) run() {
	defer w.buffer.workerWg.Done()

	for {
		select {
		case task := <-w.buffer.taskQueue:
			if task != nil {
				atomic.StoreInt64(&w.active, 1)
				w.processTask(task)
				atomic.StoreInt64(&w.active, 0)
			}
		case <-w.stopChan:
			w.buffer.logger.Info("Worker stopping", zap.Int("id", w.id))
			return
		case <-w.buffer.shutdown:
			w.buffer.logger.Info("Worker received shutdown signal, draining remaining tasks", zap.Int("id", w.id))
			// Drain remaining tasks from queue before exiting
			for {
				select {
				case task := <-w.buffer.taskQueue:
					if task != nil {
						atomic.StoreInt64(&w.active, 1)
						w.processTask(task)
						atomic.StoreInt64(&w.active, 0)
					}
				default:
					w.buffer.logger.Info("Worker drained all tasks, exiting", zap.Int("id", w.id))
					return
				}
			}
		}
	}
}

// Worker.processTask 处理刷新任务
func (w *Worker) processTask(task *FlushTask) {
	startTime := time.Now()

	// 更新统计信息
	w.buffer.updateStats(func(stats *ConcurrentBufferStats) {
		stats.ActiveWorkers++
	})

	defer func() {
		duration := time.Since(startTime)
		w.buffer.updateStats(func(stats *ConcurrentBufferStats) {
			stats.ActiveWorkers--
			stats.TotalFlushTime += duration.Milliseconds()
			stats.CompletedTasks++
			stats.LastFlushTime = time.Now().Unix()

			// 更新平均刷新时间
			totalTasks := stats.CompletedTasks
			if totalTasks > 0 {
				stats.AvgFlushTime = stats.TotalFlushTime / totalTasks
			}
		})
	}()

	// 执行刷新任务
	err := w.flushData(task.BufferKey, task.Rows)
	if err != nil {
		w.buffer.logger.Info("Worker flush task failed", zap.Int("id", w.id), zap.String("buffer_key", task.BufferKey), zap.Error(err))

		// 重试逻辑
		if task.Retries < w.buffer.config.MaxRetries {
			task.Retries++
			task.Priority++ // 降低优先级

			// 延迟重试
			go func() {
				time.Sleep(w.buffer.config.RetryDelay * time.Duration(task.Retries))
				select {
				case w.buffer.taskQueue <- task:
					w.buffer.logger.Info("Retrying flush task", zap.String("buffer_key", task.BufferKey), zap.Int("attempt", task.Retries+1))
				case <-w.buffer.shutdown:
					// 系统关闭，放弃重试
				}
			}()
		} else {
			w.buffer.logger.Info("Worker max retries exceeded", zap.Int("id", w.id), zap.String("buffer_key", task.BufferKey))
			w.buffer.updateStats(func(stats *ConcurrentBufferStats) {
				stats.FailedTasks++
			})
		}
	} else {
		w.buffer.logger.Debug("Worker successfully flushed rows", zap.Int("id", w.id), zap.Int("rows", len(task.Rows)), zap.String("buffer_key", task.BufferKey))

		// Flush 成功后，截断 WAL
		if w.buffer.walEnabled && w.buffer.wal != nil && task.MaxSeqNum > 0 {
			if err := w.buffer.wal.Truncate(task.MaxSeqNum); err != nil {
				w.buffer.logger.Warn("Failed to truncate WAL after successful flush",
					zap.Error(err),
					zap.String("buffer_key", task.BufferKey),
					zap.Uint64("seq_num", task.MaxSeqNum))
			} else {
				w.buffer.logger.Debug("WAL truncated after successful flush",
					zap.String("buffer_key", task.BufferKey),
					zap.Uint64("seq_num", task.MaxSeqNum))
			}
		}
	}
}

// Worker.flushData 执行数据刷新
func (w *Worker) flushData(bufferKey string, rows []DataRow) error {
	if len(rows) == 0 {
		return nil
	}

	// 创建临时文件
	localFilePath := filepath.Join(tempDir, fmt.Sprintf("%s-%d-%d.parquet", bufferKey, w.id, time.Now().UnixNano()))

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(localFilePath), 0755); err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.Remove(localFilePath)

	// 写入Parquet文件
	if err := w.writeParquetFile(localFilePath, rows); err != nil {
		return fmt.Errorf("failed to write parquet file: %w", err)
	}

	// 在上传前确保数据持久化到磁盘，避免操作系统缓存导致数据丢失
	if err := w.syncTempFile(localFilePath); err != nil {
		return fmt.Errorf("failed to sync temp parquet file: %w", err)
	}

	// 上传到存储
	return w.uploadToStorage(bufferKey, localFilePath)
}

// Worker.writeParquetFile 写入Parquet文件（使用 JSONWriter 支持动态列 schema）
func (w *Worker) writeParquetFile(filePath string, rows []DataRow) error {
	if len(rows) == 0 {
		return nil
	}

	fw, err := local.NewLocalFileWriter(filePath)
	if err != nil {
		return fmt.Errorf("failed to create local file writer: %w", err)
	}
	defer fw.Close()

	// Step 1: Merge all rows' field keys (union) with first-seen value for type inference.
	allFieldKeys := make(map[string]interface{})
	for _, row := range rows {
		for k, v := range row.Fields {
			if _, exists := allFieldKeys[k]; !exists {
				allFieldKeys[k] = v
			}
		}
	}

	// Step 2: Compute the global mapping ONCE for the entire batch.
	// Both the schema and every row's JSON must use this same mapping.
	keyToColName := buildGlobalFieldMapping(allFieldKeys)

	// Step 3: Build schema from the global mapping.
	schemaJSON := buildParquetSchemaJSONFromMapping(keyToColName, allFieldKeys)

	pw, err := writer.NewJSONWriter(schemaJSON, fw, 4)
	if err != nil {
		return fmt.Errorf("failed to create parquet writer: %w", err)
	}
	pw.RowGroupSize = 128 * 1024 * 1024
	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	// Step 4: Serialize every row using the SAME global mapping.
	for _, row := range rows {
		rowJSON, err := marshalRowToJSONWithMapping(row, keyToColName)
		if err != nil {
			return fmt.Errorf("failed to marshal row: %w", err)
		}
		if err := pw.Write(rowJSON); err != nil {
			return fmt.Errorf("failed to write row: %w", err)
		}
	}

	return pw.WriteStop()
}

// sanitizeColumnNameRe matches characters that are not safe for Parquet column names
var sanitizeColumnNameRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// sanitizeColumnName converts an arbitrary string to a safe Parquet column name
// (lowercase letters, digits, underscores; must start with a letter or underscore).
func sanitizeColumnName(name string) string {
	safe := sanitizeColumnNameRe.ReplaceAllString(name, "_")
	if len(safe) == 0 {
		return "_col"
	}
	if safe[0] >= '0' && safe[0] <= '9' {
		safe = "_" + safe
	}
	return strings.ToLower(safe)
}

// inferParquetType infers the Parquet type tag string from a Go value.
func inferParquetType(v interface{}) string {
	switch val := v.(type) {
	case bool:
		return "type=BOOLEAN"
	case int, int32, int64:
		return "type=INT64"
	case float32, float64:
		return "type=DOUBLE"
	case json.Number:
		if _, err := val.Int64(); err == nil {
			return "type=INT64"
		}
		return "type=DOUBLE"
	default:
		return "type=BYTE_ARRAY, convertedtype=UTF8"
	}
}

// systemColumnNames is the set of reserved system column names that user fields must not overwrite.
var systemColumnNames = map[string]struct{}{
	"id":         {},
	"timestamp":  {},
	"table_name": {},
}

// resolveFieldNames sanitizes each field key and resolves collisions by appending a numeric
// suffix (_2, _3, …) when two original keys map to the same sanitized name, or when a
// sanitized name collides with a system column.  The returned map is keyed by the final
// (collision-free) column name and valued by the original field value.
func resolveFieldNames(fields map[string]interface{}) map[string]interface{} {
	mapping := buildGlobalFieldMapping(fields)
	result := make(map[string]interface{}, len(fields))
	for origKey, colName := range mapping {
		result[colName] = fields[origKey]
	}
	return result
}

// buildGlobalFieldMapping computes a stable mapping from original field keys to final
// Parquet column names (after sanitization and collision resolution).  This mapping must
// be computed once for the full batch so that both the schema and per-row serialization
// use identical column names.
//
// Rules:
//   - Keys are sorted for deterministic suffix assignment.
//   - System column names ("id", "timestamp", "table_name") are pre-reserved; any user
//     key that sanitizes to one of them gets a "_2" (or higher) suffix.
//   - When two original keys produce the same sanitized name, subsequent ones get "_2",
//     "_3", … suffixes (skipping any already-taken name).
func buildGlobalFieldMapping(fields map[string]interface{}) map[string]string {
	seen := make(map[string]int, len(fields))
	result := make(map[string]string, len(fields))

	// Deterministic ordering so the suffix assignment is stable across calls.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		safe := sanitizeColumnName(k)
		// Reserve system column names: treat them as already seen once.
		if _, reserved := systemColumnNames[safe]; reserved {
			if seen[safe] == 0 {
				seen[safe] = 1
			}
		}
		count := seen[safe]
		var finalName string
		if count == 0 {
			finalName = safe
		} else {
			finalName = fmt.Sprintf("%s_%d", safe, count+1)
		}
		seen[safe]++
		result[k] = finalName
	}
	return result
}

// buildParquetSchemaJSON constructs the JSON schema string for parquet-go's JSONWriter.
// Fixed columns: id, timestamp, table_name. Dynamic columns are derived from fields after
// conflict resolution via resolveFieldNames.
func buildParquetSchemaJSON(fields map[string]interface{}) string {
	mapping := buildGlobalFieldMapping(fields)
	return buildParquetSchemaJSONFromMapping(mapping, fields)
}

// buildParquetSchemaJSONFromMapping constructs the JSON schema string using a pre-computed
// global field mapping (original key → final column name) and the field values for type
// inference.  This ensures the schema matches the column names used in per-row JSON.
func buildParquetSchemaJSONFromMapping(mapping map[string]string, fields map[string]interface{}) string {
	fixedFields := []string{
		`{"Tag":"name=id, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}`,
		`{"Tag":"name=timestamp, type=INT64, convertedtype=TIMESTAMP_MICROS, repetitiontype=REQUIRED"}`,
		`{"Tag":"name=table_name, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=REQUIRED"}`,
	}

	dynamicFields := make([]string, 0, len(mapping))
	for origKey, colName := range mapping {
		parquetType := inferParquetType(fields[origKey])
		dynamicFields = append(dynamicFields, fmt.Sprintf(`{"Tag":"name=%s, %s, repetitiontype=OPTIONAL"}`, colName, parquetType))
	}
	sort.Strings(dynamicFields)

	allFields := append(fixedFields, dynamicFields...)
	return fmt.Sprintf(`{"Tag":"name=parquet-go-root","Fields":[%s]}`, strings.Join(allFields, ","))
}

// marshalRowToJSON converts a DataRow to the JSON string expected by JSONWriter.
// User fields are written first (with collision resolution), then system columns overwrite
// any same-named entries to guarantee metadata integrity.
// NOTE: This function computes a per-row mapping, which is correct only when all rows
// have the same field set.  For mixed-field batches, use marshalRowToJSONWithMapping.
func marshalRowToJSON(row DataRow) (string, error) {
	return marshalRowToJSONWithMapping(row, buildGlobalFieldMapping(row.Fields))
}

// marshalRowToJSONWithMapping converts a DataRow to the JSON string expected by JSONWriter
// using a pre-computed global field mapping (original key → final column name).
// Fields absent from this row are simply omitted (JSONWriter treats missing OPTIONAL columns as null).
func marshalRowToJSONWithMapping(row DataRow, keyToColName map[string]string) (string, error) {
	m := make(map[string]interface{}, len(row.Fields)+3)
	// Write user fields using the global mapping so column names match the schema.
	for origKey, v := range row.Fields {
		if colName, ok := keyToColName[origKey]; ok {
			m[colName] = v
		}
		// Fields not in the global mapping (should not happen in normal usage) are dropped.
	}
	// System columns are written last so they always take precedence.
	m["id"] = row.ID
	m["timestamp"] = row.Timestamp
	m["table_name"] = row.Table
	b, err := json.Marshal(m)
	return string(b), err
}

// Worker.syncTempFile 确保临时文件数据持久化到磁盘
// 在上传到 MinIO 前调用，避免操作系统页缓存导致数据丢失
func (w *Worker) syncTempFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open temp file for sync: %w", err)
	}
	defer file.Close()

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file to disk: %w", err)
	}

	w.buffer.logger.Debug("Successfully synced temp parquet file",
		zap.String("file_path", filePath))

	return nil
}

// Worker.uploadToStorage 上传到存储
func (w *Worker) uploadToStorage(bufferKey, localFilePath string) error {
	// 如果poolManager为nil（测试模式），直接返回成功
	if w.buffer.poolManager == nil {
		w.buffer.logger.Info("Test mode: skipping upload", zap.String("buffer_key", bufferKey))
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), w.buffer.config.FlushTimeout)
	defer cancel()

	rawObjectName := fmt.Sprintf("%s/%d.parquet", bufferKey, time.Now().UnixNano())
	objectName := normalizeObjectKey(rawObjectName)

	// 上传到主存储
	minioPool := w.buffer.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	// 获取主存储桶名称（优先使用 Buffer.DefaultBucket，否则使用 MinIO.Bucket）
	primaryBucket := ""
	if w.buffer.appConfig != nil {
		if w.buffer.appConfig.Buffer.DefaultBucket != "" {
			primaryBucket = w.buffer.appConfig.Buffer.DefaultBucket
		} else if w.buffer.appConfig.GetMinIO().Bucket != "" {
			primaryBucket = w.buffer.appConfig.GetMinIO().Bucket
		}
	}
	if primaryBucket == "" {
		primaryBucket = "miniodb-data" // 最终默认值
	}

	minioMetrics := metrics.NewMinIOMetrics("upload_primary")

	// 使用故障切换管理器上传到主池（带自动故障切换）
	err := w.buffer.poolManager.ExecuteWithFailover(ctx, func(pool *pool.MinIOPool) error {
		client := pool.GetClient()
		_, err := client.FPutObject(ctx, primaryBucket, objectName, localFilePath, minio.PutObjectOptions{})
		return err
	})

	if err != nil {
		minioMetrics.Finish("error")
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}
	minioMetrics.Finish("success")

	// 异步同步到备份池（不阻塞主流程）
	if failoverMgr := w.buffer.poolManager.GetFailoverManager(); failoverMgr != nil {
		failoverMgr.EnqueueSync(primaryBucket, objectName, localFilePath)
	}

	// 提取并存储 Parquet 文件元数据到 Redis（用于查询时的文件剪枝）
	if err := w.extractAndStoreFileMetadata(ctx, localFilePath, objectName); err != nil {
		w.buffer.logger.Warn("Failed to store file metadata (non-fatal)", zap.Error(err))
		// 不阻塞主流程，继续更新索引
	}

	if err := w.updateRedisIndex(ctx, bufferKey, objectName); err != nil {
		w.buffer.logger.Error("Redis index update failed, rolling back MinIO upload", zap.Error(err))
		w.rollbackMinIOUpload(ctx, primaryBucket, objectName)
		return fmt.Errorf("failed to update metadata, upload rolled back: %w", err)
	}

	return nil
}

func (w *Worker) rollbackMinIOUpload(ctx context.Context, bucket, objectName string) {
	minioPool := w.buffer.poolManager.GetMinIOPool()
	if minioPool == nil {
		w.buffer.logger.Error("ERROR: cannot rollback MinIO upload - pool unavailable")
		return
	}

	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		return client.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
	})

	if err != nil {
		w.buffer.logger.Error("Failed to rollback MinIO upload", zap.String("bucket", bucket), zap.String("object_name", objectName), zap.Error(err))
	} else {
		w.buffer.logger.Info("Successfully rolled back MinIO upload", zap.String("bucket", bucket), zap.String("object_name", objectName))
	}
}

// Worker.updateRedisIndex 更新Redis索引
func (w *Worker) updateRedisIndex(ctx context.Context, bufferKey, objectName string) error {
	// 如果poolManager为nil（测试模式），直接返回成功
	if w.buffer.poolManager == nil {
		w.buffer.logger.Info("Test mode: skipping Redis index update", zap.String("buffer_key", bufferKey))
		return nil
	}

	redisPool := w.buffer.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()

	// 从bufferKey解析表名、ID和日期 (新格式: 表名/ID/YYYY-MM-DD)
	parts := strings.Split(bufferKey, "/")
	if len(parts) != 3 {
		return fmt.Errorf("invalid buffer key format: %s, expected format: table/id/date", bufferKey)
	}

	tableName := parts[0]
	id := parts[1]
	day := parts[2]

	// 使用表级索引格式
	redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", tableName, id, day)

	if _, err := client.SAdd(ctx, redisKey, objectName).Result(); err != nil {
		return fmt.Errorf("failed to update redis index: %w", err)
	}

	// 更新节点数据映射
	if w.buffer.nodeID != "" {
		nodeDataKey := fmt.Sprintf("node:data:%s", w.buffer.nodeID)
		if _, err := client.SAdd(ctx, nodeDataKey, redisKey).Result(); err != nil {
			w.buffer.logger.Warn("Failed to update node data mapping", zap.Error(err))
		}
	}

	return nil
}

// extractAndStoreFileMetadata 提取 Parquet 文件元数据并存储
// 支持两种存储模式：
// 1. 分布式模式：存储到 Redis
// 2. 单节点模式：存储到 MinIO sidecar 文件
// 元数据包括 MinValues/MaxValues，用于查询时的文件剪枝优化
func (w *Worker) extractAndStoreFileMetadata(ctx context.Context, localFilePath, objectName string) error {
	// 检查是否可用
	if w.buffer.poolManager == nil {
		return nil // 测试模式，跳过
	}

	// 使用 Parquet Reader 提取文件元数据
	parquetReader := storage.NewParquetReader(nil, w.buffer.logger.With(zap.String("local_file_path", localFilePath)))
	fileMetadata, err := parquetReader.GetFileMetadata(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to extract parquet metadata: %w", err)
	}

	// 更新文件路径为对象名
	fileMetadata.FilePath = objectName

	// 获取存储资源
	redisPool := w.buffer.poolManager.GetRedisPool()
	minioPool := w.buffer.poolManager.GetMinIOPool()

	// 获取存储桶名称（优先使用 Buffer.DefaultBucket，否则使用 MinIO.Bucket）
	bucket := ""
	if w.buffer.appConfig != nil {
		if w.buffer.appConfig.Buffer.DefaultBucket != "" {
			bucket = w.buffer.appConfig.Buffer.DefaultBucket
		} else if w.buffer.appConfig.GetMinIO().Bucket != "" {
			bucket = w.buffer.appConfig.GetMinIO().Bucket
		}
	}
	if bucket == "" {
		bucket = "miniodb-data" // 最终默认值
	}

	var storedToRedis, storedToMinIO bool
	var lastErr error

	// 1. 尝试存储到 Redis（分布式模式）
	if redisPool != nil {
		if err := w.storeMetadataToRedis(ctx, objectName, fileMetadata); err != nil {
			w.buffer.logger.Debug("Failed to store metadata to Redis (will try MinIO sidecar)",
				zap.String("object", objectName),
				zap.Error(err))
			lastErr = err
		} else {
			storedToRedis = true
		}
	}

	// 2. 同时存储到 MinIO sidecar（单节点模式或作为备份）
	if minioPool != nil {
		if err := w.storeMetadataToMinIOSidecar(ctx, minioPool, bucket, objectName, fileMetadata); err != nil {
			w.buffer.logger.Debug("Failed to store metadata to MinIO sidecar",
				zap.String("object", objectName),
				zap.Error(err))
			lastErr = err
		} else {
			storedToMinIO = true
		}
	}

	// 只要有一个成功就算成功
	if storedToRedis || storedToMinIO {
		w.buffer.logger.Debug("Stored file metadata",
			zap.String("object", objectName),
			zap.Int64("row_count", fileMetadata.RowCount),
			zap.Int("min_values_count", len(fileMetadata.MinValues)),
			zap.Int("max_values_count", len(fileMetadata.MaxValues)),
			zap.Bool("redis", storedToRedis),
			zap.Bool("minio_sidecar", storedToMinIO))
		return nil
	}

	return lastErr
}

// storeMetadataToRedis 存储元数据到 Redis
func (w *Worker) storeMetadataToRedis(ctx context.Context, objectName string, fileMetadata *storage.FileMetadata) error {
	redisPool := w.buffer.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("redis pool not available")
	}

	client := redisPool.GetClient()
	// 使用配置的 key 前缀，默认为 "file_meta:"
	keyPrefix := "file_meta:"
	if w.buffer.appConfig != nil && w.buffer.appConfig.FileMetadata.KeyPrefix != "" {
		keyPrefix = w.buffer.appConfig.FileMetadata.KeyPrefix
	}
	metadataKey := fmt.Sprintf("%s%s", keyPrefix, objectName)

	// 序列化 map 字段
	minValuesJSON, _ := json.Marshal(fileMetadata.MinValues)
	maxValuesJSON, _ := json.Marshal(fileMetadata.MaxValues)
	nullCountsJSON, _ := json.Marshal(fileMetadata.NullCounts)

	fields := map[string]interface{}{
		"file_path":   objectName,
		"file_size":   fileMetadata.FileSize,
		"row_count":   fileMetadata.RowCount,
		"row_groups":  fileMetadata.RowGroupCount,
		"min_values":  string(minValuesJSON),
		"max_values":  string(maxValuesJSON),
		"null_counts": string(nullCountsJSON),
		"compression": fileMetadata.CompressionType,
		"created_at":  fileMetadata.CreatedAt.Format(time.RFC3339),
		"stored_at":   time.Now().Format(time.RFC3339),
	}

	if err := client.HSet(ctx, metadataKey, fields).Err(); err != nil {
		return fmt.Errorf("failed to store metadata in Redis: %w", err)
	}

	// 设置过期时间（使用配置的 TTL，默认 30 天）
	ttl := 30 * 24 * time.Hour
	if w.buffer.appConfig != nil && w.buffer.appConfig.FileMetadata.TTL > 0 {
		ttl = w.buffer.appConfig.FileMetadata.TTL
	}
	client.Expire(ctx, metadataKey, ttl)

	return nil
}

// storeMetadataToMinIOSidecar 存储元数据到 MinIO sidecar 文件
func (w *Worker) storeMetadataToMinIOSidecar(ctx context.Context, minioPool *pool.MinIOPool, bucket, objectName string, fileMetadata *storage.FileMetadata) error {
	// 序列化元数据
	metaJSON, err := json.Marshal(fileMetadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	// 上传 sidecar 文件
	metaObjectName := objectName + ".meta.json"

	return minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		reader := bytes.NewReader(metaJSON)
		_, err := client.PutObject(ctx, bucket, metaObjectName, reader, int64(len(metaJSON)), minio.PutObjectOptions{
			ContentType: "application/json",
		})
		return err
	})
}

// Add 添加数据到缓冲区
// 返回 error 当 WAL 连续失败超过阈值时
func (cb *ConcurrentBuffer) Add(row DataRow) error {
	t := time.UnixMicro(row.Timestamp)
	dayStr := t.Format("2006-01-02")

	// 修复：使用表名/ID/日期格式，确保查询能正确找到缓冲区数据
	tableName := row.Table
	if tableName == "" {
		// 如果DataRow中没有表名，使用默认表名
		if cb.appConfig != nil {
			tableName = cb.appConfig.GetDefaultTableName()
		} else {
			tableName = "default_table"
		}
	}
	idPart := row.ID
	if idPart == "" {
		idPart = emptyIDPlaceholder
	}

	// 使用 sync.Pool 复用 strings.Builder，减少内存分配
	sb := getBufferKeyBuilder()
	// 预估大小：表名 + ID + 日期 + 2个斜杠，通常约 30-50 字节
	sb.Grow(len(tableName) + len(idPart) + len(dayStr) + 2)
	sb.WriteString(tableName)
	sb.WriteByte('/')
	sb.WriteString(idPart)
	sb.WriteByte('/')
	sb.WriteString(dayStr)
	bufferKey := sb.String()
	putBufferKeyBuilder(sb)

	// 准备 WAL 数据（在锁内准备，避免数据竞争）
	var walDataToWrite []byte
	var needFlush bool
	var insertIdx int // 记录插入位置，用于回滚时精确删除

	cb.mutex.Lock()
	// 写入内存缓冲区
	insertIdx = len(cb.buffer[bufferKey]) // 记录写入前的长度，即新元素的索引
	cb.buffer[bufferKey] = append(cb.buffer[bufferKey], row)
	needFlush = len(cb.buffer[bufferKey]) >= cb.config.BufferSize

	// 准备 WAL 数据
	if cb.walEnabled && cb.wal != nil {
		walData := walDataRow{
			BufferKey: bufferKey,
			Row:       row,
		}
		var err error
		walDataToWrite, err = json.Marshal(walData)
		if err != nil {
			cb.logger.Error("Failed to marshal WAL data",
				zap.Error(err),
				zap.String("buffer_key", bufferKey))
			walDataToWrite = nil // 标记为不需要写入 WAL
		}
	}

	// 更新统计信息（使用 atomic 计数器，避免锁内遍历 buffer）
	cb.pendingWrites.Add(1)
	cb.updateStats(func(stats *ConcurrentBufferStats) {
		stats.BufferSize = int64(len(cb.buffer))
		stats.PendingWrites = cb.pendingWrites.Load()
	})
	cb.mutex.Unlock()

	// WAL 写入在锁外执行（WAL 内部有自己的 mutex 保护并发写入安全）
	if cb.walEnabled && cb.wal != nil && len(walDataToWrite) > 0 {
		seqNum, err := cb.wal.Append(wal.RecordTypeData, walDataToWrite)
		if err != nil {
			// 递增 WAL 失败计数
			failCount := cb.walFailCount.Add(1)
			cb.logger.Warn("WAL append failed",
				zap.Error(err),
				zap.String("buffer_key", bufferKey),
				zap.Int64("consecutive_failures", failCount))

			// 超过阈值时触发紧急 flush 并返回错误
			if failCount >= walFailThreshold {
				cb.walDegraded.Store(true)
				cb.logger.Error("WAL degraded mode activated, triggering emergency flush",
					zap.Int64("fail_count", failCount),
					zap.Int("threshold", walFailThreshold))

				// 回滚：从 buffer 中移除刚写入的数据，避免调用方重试导致重复写入
				// 使用 insertIdx 精确删除当前 goroutine 写入的数据，而非删除最后一个元素
				cb.mutex.Lock()
				if rows, exists := cb.buffer[bufferKey]; exists && insertIdx < len(rows) {
					// 按索引删除，而非删除最后一个元素（避免竞态条件）
					cb.buffer[bufferKey] = append(rows[:insertIdx], rows[insertIdx+1:]...)
					// 如果 slice 为空，删除该 key
					if len(cb.buffer[bufferKey]) == 0 {
						delete(cb.buffer, bufferKey)
					}
				}
				// 更新 pendingWrites 计数
				cb.pendingWrites.Add(-1)
				cb.mutex.Unlock()

				// 异步执行紧急 flush，避免阻塞当前请求
				go cb.emergencyFlush()
				return fmt.Errorf("WAL consecutive failures (%d), entering degraded mode", failCount)
			}
		} else {
			// WAL 成功时重置失败计数
			cb.walFailCount.Store(0)
			cb.walDegraded.Store(false)
			// 记录此 bufferKey 的最新序列号
			cb.walSeqMutex.Lock()
			cb.lastFlushedSeq[bufferKey] = seqNum
			cb.walSeqMutex.Unlock()
		}
	}

	if needFlush {
		cb.triggerFlush(bufferKey, false)
	}
	return nil
}

// triggerFlush 触发刷新
func (cb *ConcurrentBuffer) triggerFlush(bufferKey string, force bool) {
	cb.mutex.Lock()
	rows, exists := cb.buffer[bufferKey]
	if !exists || (!force && len(rows) == 0) {
		cb.mutex.Unlock()
		return
	}

	// 复制数据并清空缓冲区
	rowsCopy := make([]DataRow, len(rows))
	copy(rowsCopy, rows)
	delete(cb.buffer, bufferKey)

	// 获取此 bufferKey 的最大 WAL 序列号
	var maxSeqNum uint64
	if cb.walEnabled {
		cb.walSeqMutex.RLock()
		maxSeqNum = cb.lastFlushedSeq[bufferKey]
		cb.walSeqMutex.RUnlock()
	}

	// 更新 pendingWrites 计数（使用 atomic，避免锁内遍历）
	flushedCount := len(rowsCopy)
	cb.pendingWrites.Add(-int64(flushedCount))

	cb.mutex.Unlock()

	// 创建刷新任务
	task := &FlushTask{
		BufferKey: bufferKey,
		Rows:      rowsCopy,
		Priority:  0, // 默认优先级
		CreatedAt: time.Now(),
		Retries:   0,
		MaxSeqNum: maxSeqNum,
	}

	// 提交任务到队列
	select {
	case cb.taskQueue <- task:
		cb.updateStats(func(stats *ConcurrentBufferStats) {
			stats.TotalTasks++
			stats.QueuedTasks++
		})

		// 记录缓冲区刷新指标
		if force {
			metrics.RecordBufferFlush("triggered_by_time")
		} else {
			metrics.RecordBufferFlush("triggered_by_size")
		}
	case <-cb.shutdown:
		// 系统关闭，直接执行刷新
		cb.logger.Info("System shutting down, executing immediate flush", zap.String("buffer_key", bufferKey))
		worker := cb.workers[0] // 使用第一个worker
		worker.processTask(task)
	default:
		// 背压：等待 5 秒，避免直接丢弃
		timer := time.NewTimer(5 * time.Second)
		select {
		case cb.taskQueue <- task:
			timer.Stop()
		case <-cb.shutdown:
			timer.Stop()
			// 尝试同步处理
			cb.logger.Warn("Shutdown during backpressure, attempting sync processing")
		case <-timer.C:
			cb.logger.Error("Task queue full after backpressure timeout, task dropped",
				zap.String("buffer_key", bufferKey))
			cb.updateStats(func(stats *ConcurrentBufferStats) {
				stats.FailedTasks++
			})
		}
	}
}

// waitAndTruncateRecoveryWAL 等待 WAL 恢复数据全部 flush 完成后，截断历史 WAL 文件。
// 避免下次重启时重复 replay 已经落盘的数据。
func (cb *ConcurrentBuffer) waitAndTruncateRecoveryWAL() {
	maxSeq := cb.recoveryMaxSeqNum
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cb.shutdown:
			return
		case <-ticker.C:
			cb.mutex.RLock()
			pendingKeys := len(cb.buffer)
			cb.mutex.RUnlock()

			queueLen := len(cb.taskQueue)
			if pendingKeys == 0 && queueLen == 0 {
				if err := cb.wal.Truncate(maxSeq + 1); err != nil {
					cb.logger.Warn("Post-recovery WAL truncation failed", zap.Error(err))
				} else {
					cb.logger.Info("Post-recovery WAL truncation complete",
						zap.Uint64("truncated_up_to", maxSeq))
				}
				// 重置，避免重复触发
				cb.recoveryMaxSeqNum = 0
				return
			}
		}
	}
}

// periodicFlush 定期刷新
func (cb *ConcurrentBuffer) periodicFlush() {
	ticker := time.NewTicker(cb.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cb.flushAllBuffers()
		case <-cb.shutdown:
			cb.logger.Info("Periodic flush stopped")
			return
		}
	}
}

// flushAllBuffers 刷新所有缓冲区
func (cb *ConcurrentBuffer) flushAllBuffers() {
	cb.mutex.RLock()
	keys := make([]string, 0, len(cb.buffer))
	for key := range cb.buffer {
		keys = append(keys, key)
	}
	cb.mutex.RUnlock()

	// 批量处理或逐个处理
	if cb.config.EnableBatching && len(keys) > cb.config.BatchFlushSize {
		cb.batchFlush(keys)
	} else {
		for _, key := range keys {
			cb.triggerFlush(key, true)
		}
	}
}

// batchFlush 批量刷新
func (cb *ConcurrentBuffer) batchFlush(keys []string) {
	// 按批次大小分组
	for i := 0; i < len(keys); i += cb.config.BatchFlushSize {
		end := i + cb.config.BatchFlushSize
		if end > len(keys) {
			end = len(keys)
		}

		batch := keys[i:end]
		for _, key := range batch {
			cb.triggerFlush(key, true)
		}

		// 批次间的小延迟，避免突发负载
		if i+cb.config.BatchFlushSize < len(keys) {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Get 获取缓冲区数据
func (cb *ConcurrentBuffer) Get(key string) []DataRow {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	rows := make([]DataRow, len(cb.buffer[key]))
	copy(rows, cb.buffer[key])
	return rows
}

// GetAllKeys 获取所有缓冲区键
func (cb *ConcurrentBuffer) GetAllKeys() []string {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	keys := make([]string, 0, len(cb.buffer))
	for key := range cb.buffer {
		keys = append(keys, key)
	}
	return keys
}

// Size 获取缓冲区大小
func (cb *ConcurrentBuffer) Size() int {
	cb.stats.mutex.RLock()
	defer cb.stats.mutex.RUnlock()
	return int(cb.stats.BufferSize)
}

// PendingWrites 获取待写入数据数量
func (cb *ConcurrentBuffer) PendingWrites() int {
	return int(cb.pendingWrites.Load())
}

// GetStats 获取统计信息
func (cb *ConcurrentBuffer) GetStats() *ConcurrentBufferStats {
	cb.stats.mutex.Lock()
	defer cb.stats.mutex.Unlock()

	// 实时更新队列长度（直接赋值，已有 Lock 保护）
	cb.stats.QueuedTasks = int64(len(cb.taskQueue))

	// 使用 atomic 计数器获取 pendingWrites
	pendingWrites := cb.pendingWrites.Load()

	return &ConcurrentBufferStats{
		TotalTasks:     cb.stats.TotalTasks,
		CompletedTasks: cb.stats.CompletedTasks,
		FailedTasks:    cb.stats.FailedTasks,
		QueuedTasks:    cb.stats.QueuedTasks,
		ActiveWorkers:  cb.stats.ActiveWorkers,
		AvgFlushTime:   cb.stats.AvgFlushTime,
		TotalFlushTime: cb.stats.TotalFlushTime,
		LastFlushTime:  cb.stats.LastFlushTime,
		BufferSize:     cb.stats.BufferSize,
		PendingWrites:  pendingWrites,
	}
}

// updateStats 更新统计信息
func (cb *ConcurrentBuffer) updateStats(updater func(*ConcurrentBufferStats)) {
	cb.stats.mutex.Lock()
	defer cb.stats.mutex.Unlock()
	updater(cb.stats)
}

// Stop 停止缓冲区
func (cb *ConcurrentBuffer) Stop() {
	cb.shutdownOnce.Do(func() {
		cb.logger.Info("Stopping concurrent buffer...")

		// 1. 刷新所有剩余缓冲数据
		cb.flushAllBuffers()

		// 2. 关闭 shutdown 信号
		close(cb.shutdown)

		// 3. 等待 worker 完成（worker 会 drain 剩余任务后退出）
		cb.workerWg.Wait()

		// 4. 关闭任务队列（此时所有 worker 已退出，可以安全关闭）
		close(cb.taskQueue)

		// 5. 关闭 WAL
		if cb.walEnabled && cb.wal != nil {
			if err := cb.wal.Close(); err != nil {
				cb.logger.Warn("Failed to close WAL",
					zap.Error(err))
			} else {
				cb.logger.Info("WAL closed successfully")
			}
		}

		cb.logger.Info("Concurrent buffer stopped successfully")
	})
}

// WriteTempParquetFile 写入临时Parquet文件（用于查询）
func (cb *ConcurrentBuffer) WriteTempParquetFile(filePath string, rows []DataRow) error {
	// 使用第一个worker的方法
	if len(cb.workers) > 0 {
		return cb.workers[0].writeParquetFile(filePath, rows)
	}
	return fmt.Errorf("no workers available")
}

// GetTableKeys 获取指定表的所有缓冲区键
func (cb *ConcurrentBuffer) GetTableKeys(tableName string) []string {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	prefix := tableName + "/"
	keys := make([]string, 0) // 初始化为空slice而不是nil
	for key := range cb.buffer {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys
}

// InvalidateTableConfig 使表配置缓存失效（兼容接口，ConcurrentBuffer不需要此功能）
func (cb *ConcurrentBuffer) InvalidateTableConfig(tableName string) {
	// ConcurrentBuffer不缓存表配置，此方法为兼容性保留
	cb.logger.Info("InvalidateTableConfig called (no-op in ConcurrentBuffer)", zap.String("table_name", tableName))
}

// GetTableBufferKeys 获取指定表的所有缓冲区键（别名方法，兼容性）
func (cb *ConcurrentBuffer) GetTableBufferKeys(tableName string) []string {
	return cb.GetTableKeys(tableName)
}

// ClearTable 清除指定表在 buffer 中的所有数据（用于删表操作）
// 同时清理 WAL 中该表的 tombstone 记录相关状态
// 返回被移除的记录数
func (cb *ConcurrentBuffer) ClearTable(tableName string) (removedCount int) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.walSeqMutex.Lock()
	defer cb.walSeqMutex.Unlock()

	prefix := tableName + "/"
	for bufferKey, rows := range cb.buffer {
		if !strings.HasPrefix(bufferKey, prefix) {
			continue
		}
		removedCount += len(rows)
		delete(cb.buffer, bufferKey)
		delete(cb.lastFlushedSeq, bufferKey)
	}

	if removedCount > 0 {
		cb.pendingWrites.Add(-int64(removedCount))
		cb.updateStats(func(stats *ConcurrentBufferStats) {
			stats.BufferSize = int64(len(cb.buffer))
			stats.PendingWrites = cb.pendingWrites.Load()
		})
	}

	if removedCount > 0 {
		cb.logger.Info("Cleared table from buffer",
			zap.String("table", tableName),
			zap.Int("removed_count", removedCount))
	}

	return removedCount
}

// Remove 从 buffer 中移除指定表和 ID 的所有记录
// 用于 Delete/Update 操作时清理 buffer 中尚未 flush 的数据
// 同时写入 WAL Tombstone 记录，确保崩溃恢复时不会"复活"已删除数据
// 返回被移除的记录数
func (cb *ConcurrentBuffer) Remove(tableName, id string) (removedCount int) {
	cb.mutex.Lock()

	prefix := tableName + "/"
	for bufferKey, rows := range cb.buffer {
		if !strings.HasPrefix(bufferKey, prefix) {
			continue
		}
		newRows := make([]DataRow, 0, len(rows))
		for _, row := range rows {
			if row.ID != id {
				newRows = append(newRows, row)
			} else {
				removedCount++
			}
		}
		if len(newRows) == 0 {
			delete(cb.buffer, bufferKey)
		} else if len(newRows) < len(rows) {
			cb.buffer[bufferKey] = newRows
		}
	}

	if removedCount > 0 {
		cb.pendingWrites.Add(-int64(removedCount))
		cb.updateStats(func(stats *ConcurrentBufferStats) {
			stats.BufferSize = int64(len(cb.buffer))
			stats.PendingWrites = cb.pendingWrites.Load()
		})
	}

	cb.mutex.Unlock()

	if removedCount > 0 && cb.walEnabled && cb.wal != nil {
		tombstone := walTombstoneRow{
			TableName: tableName,
			ID:        id,
		}
		tombstoneData, err := json.Marshal(tombstone)
		if err != nil {
			cb.logger.Error("Failed to marshal WAL tombstone",
				zap.Error(err),
				zap.String("table", tableName),
				zap.String("id", id))
		} else {
			_, err := cb.wal.Append(wal.RecordTypeTombstone, tombstoneData)
			if err != nil {
				cb.logger.Warn("Failed to write WAL tombstone",
					zap.Error(err),
					zap.String("table", tableName),
					zap.String("id", id))
			} else {
				cb.logger.Debug("Wrote WAL tombstone",
					zap.String("table", tableName),
					zap.String("id", id),
					zap.Int("removed_count", removedCount))
			}
		}
	} else if removedCount > 0 {
		cb.logger.Debug("Removed records from buffer (no WAL)",
			zap.String("table", tableName),
			zap.String("id", id),
			zap.Int("count", removedCount))
	}

	return removedCount
}

// AddDataPoint 添加数据点到缓冲区（兼容bufferManager接口）
// data 应为 JSON 编码的 map；若解析失败则以 "raw" 键存储原始内容。
// 注意：此方法忽略 Add 返回的错误，保持向后兼容
func (cb *ConcurrentBuffer) AddDataPoint(id string, data []byte, timestamp time.Time) {
	var fields map[string]interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		fields = map[string]interface{}{"raw": string(data)}
	}
	row := DataRow{
		ID:        id,
		Timestamp: timestamp.UnixMicro(),
		Fields:    fields,
	}
	_ = cb.Add(row) // 忽略错误，保持向后兼容
}

// FlushDataPoints 手动刷新所有缓冲区（兼容bufferManager接口）
func (cb *ConcurrentBuffer) FlushDataPoints() error {
	cb.flushAllBuffers()
	return nil
}

// emergencyFlush 紧急刷新所有缓冲区数据
// 当 WAL 连续失败超过阈值时调用，立即将内存数据刷入存储，防止数据丢失
func (cb *ConcurrentBuffer) emergencyFlush() {
	cb.logger.Warn("Emergency flush triggered due to WAL degradation")

	// 获取所有 buffer keys
	cb.mutex.RLock()
	keys := make([]string, 0, len(cb.buffer))
	for key := range cb.buffer {
		keys = append(keys, key)
	}
	cb.mutex.RUnlock()

	// 触发所有 buffer 的紧急 flush
	for _, key := range keys {
		cb.triggerFlush(key, true) // force=true 强制刷新
	}

	cb.logger.Info("Emergency flush completed",
		zap.Int("buffers_flushed", len(keys)))
}

// IsWALDegraded 返回当前是否处于 WAL 降级模式
func (cb *ConcurrentBuffer) IsWALDegraded() bool {
	return cb.walDegraded.Load()
}

// GetWALFailCount 返回当前 WAL 连续失败计数
func (cb *ConcurrentBuffer) GetWALFailCount() int64 {
	return cb.walFailCount.Load()
}
