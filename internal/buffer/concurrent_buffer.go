package buffer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/internal/metrics"
	"minIODB/internal/pool"
	"minIODB/internal/storage"
	"minIODB/internal/utils"

	"github.com/minio/minio-go/v7"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
	"go.uber.org/zap"
)

// DataRow defines the structure for our Parquet file records
type DataRow struct {
	ID        string `json:"id" parquet:"name=id, type=BYTE_ARRAY, convertedtype=UTF8"`
	Timestamp int64  `json:"timestamp" parquet:"name=timestamp, type=INT64"`
	Payload   string `json:"payload" parquet:"name=payload, type=BYTE_ARRAY, convertedtype=UTF8"`
	Table     string `json:"table" parquet:"name=table, type=BYTE_ARRAY, convertedtype=UTF8"`
}

const tempDir = "temp_parquet"

// ConcurrentBufferConfig 并发缓冲区配置
type ConcurrentBufferConfig struct {
	BufferSize        int           `yaml:"buffer_size"`         // 缓冲区大小
	FlushInterval     time.Duration `yaml:"flush_interval"`      // 刷新间隔
	WorkerPoolSize    int           `yaml:"worker_pool_size"`    // 工作池大小
	TaskQueueSize     int           `yaml:"task_queue_size"`     // 任务队列大小
	BatchFlushSize    int           `yaml:"batch_flush_size"`    // 批量刷新大小
	EnableBatching    bool          `yaml:"enable_batching"`     // 启用批量处理
	FlushTimeout      time.Duration `yaml:"flush_timeout"`       // 刷新超时
	MaxRetries        int           `yaml:"max_retries"`         // 最大重试次数
	RetryDelay        time.Duration `yaml:"retry_delay"`         // 重试延迟
	MemoryThreshold   int64         `yaml:"memory_threshold"`    // 内存阈值（字节），超过此值触发刷新
	EnableMemoryFlush bool          `yaml:"enable_memory_flush"` // 启用基于内存压力的刷新
}

// DefaultConcurrentBufferConfig 返回默认配置
func DefaultConcurrentBufferConfig() *ConcurrentBufferConfig {
	return &ConcurrentBufferConfig{
		BufferSize:        1000,
		FlushInterval:     30 * time.Second,
		WorkerPoolSize:    10,
		TaskQueueSize:     100,
		BatchFlushSize:    5,
		EnableBatching:    true,
		FlushTimeout:      60 * time.Second,
		MaxRetries:        3,
		RetryDelay:        1 * time.Second,
		MemoryThreshold:   512 * 1024 * 1024, // 512MB 默认内存阈值
		EnableMemoryFlush: true,              // 默认启用内存压力刷新
	}
}

// FlushTask 刷新任务
type FlushTask struct {
	BufferKey string
	Rows      []DataRow
	Priority  int // 任务优先级，数字越小优先级越高
	CreatedAt time.Time
	Retries   int
}

// ConcurrentBuffer 支持并发刷新的缓冲区
type ConcurrentBuffer struct {
	config         *ConcurrentBufferConfig
	appConfig      *config.Config
	poolManager    *pool.PoolManager
	backupBucket   string
	nodeID         string
	shardOptimizer *storage.ShardOptimizer // 分片优化器

	// 缓冲区数据
	buffer map[string][]DataRow
	mutex  sync.RWMutex

	// 工作池相关
	taskQueue    chan *FlushTask
	workers      []*Worker
	workerWg     sync.WaitGroup
	shutdown     chan struct{}
	shutdownOnce sync.Once

	// 统计信息
	stats *ConcurrentBufferStats

	// 内存优化器（新增）
	memoryOptimizer *storage.MemoryOptimizer

	// 自适应刷新策略
	queryCount    int64 // 查询计数
	lastQueryTime int64 // 最后查询时间（纳秒）
	adaptiveFlush bool  // 是否启用自适应刷新

	// 缓存失效通知
	cacheInvalidator CacheInvalidator // 缓存失效器（在刷新后通知查询层）
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

// NewConcurrentBuffer 创建新的并发缓冲区（集成内存优化器）
func NewConcurrentBuffer(ctx context.Context, poolManager *pool.PoolManager, appConfig *config.Config, backupBucket, nodeID string, config *ConcurrentBufferConfig) *ConcurrentBuffer {
	if config == nil {
		config = DefaultConcurrentBufferConfig()
	}

	cb := &ConcurrentBuffer{
		config:        config,
		appConfig:     appConfig,
		poolManager:   poolManager,
		backupBucket:  backupBucket,
		nodeID:        nodeID,
		buffer:        make(map[string][]DataRow),
		taskQueue:     make(chan *FlushTask, config.TaskQueueSize),
		workers:       make([]*Worker, config.WorkerPoolSize),
		shutdown:      make(chan struct{}),
		stats:         &ConcurrentBufferStats{},
		adaptiveFlush: true, // 启用自适应刷新
	}

	// 初始化内存优化器（如果配置启用）
	if appConfig != nil && appConfig.StorageEngine.Memory.EnablePooling {
		memConfig := &storage.MemoryConfig{
			MaxMemoryUsage:  appConfig.StorageEngine.Memory.MaxMemoryUsage,
			PoolSizes:       appConfig.StorageEngine.Memory.MemoryPoolSizes,
			GCInterval:      appConfig.StorageEngine.Memory.GCInterval,
			ZeroCopyEnabled: appConfig.StorageEngine.Memory.EnableZeroCopy,
		}
		cb.memoryOptimizer = storage.NewMemoryOptimizer(ctx, memConfig)
		logger.LogInfo(ctx, "Memory optimizer initialized for concurrent buffer")
	} else {
		logger.LogInfo(ctx, "Memory optimizer disabled - using standard memory allocation")
	}

	// 初始化分片优化器（如果存储引擎启用）
	if appConfig != nil && appConfig.StorageEngine.Enabled {
		cb.shardOptimizer = storage.NewShardOptimizer()
		logger.LogInfo(ctx, "Shard optimizer initialized for concurrent buffer")
	}

	// 启动工作线程
	cb.startWorkers(ctx)

	// 启动定期刷新
	go cb.periodicFlush(ctx)

	logger.LogInfo(ctx, "Concurrent buffer initialized with %d workers, queue size %d",
		zap.Int("worker_pool_size", config.WorkerPoolSize),
		zap.Int("task_queue_size", config.TaskQueueSize))

	return cb
}

// startWorkers 启动工作线程
func (cb *ConcurrentBuffer) startWorkers(ctx context.Context) {
	for i := 0; i < cb.config.WorkerPoolSize; i++ {
		worker := &Worker{
			id:       i,
			buffer:   cb,
			stopChan: make(chan struct{}),
		}
		cb.workers[i] = worker
		cb.workerWg.Add(1)
		go worker.run(ctx)
	}
}

// Worker.run 工作线程主循环
func (w *Worker) run(ctx context.Context) {
	defer w.buffer.workerWg.Done()

	for {
		select {
		case task := <-w.buffer.taskQueue:
			if task != nil {
				atomic.StoreInt64(&w.active, 1)
				w.processTask(ctx, task)
				atomic.StoreInt64(&w.active, 0)
			}
		case <-w.stopChan:
			logger.LogInfo(ctx, "Worker %d stopping", zap.Int("worker_id", w.id))
			return
		case <-w.buffer.shutdown:
			logger.LogInfo(ctx, "Worker %d shutting down", zap.Int("worker_id", w.id))
			return
		}
	}
}

// Worker.processTask 处理刷新任务
func (w *Worker) processTask(ctx context.Context, task *FlushTask) {
	startTime := time.Now()

	// 更新统计信息
	w.buffer.updateStats(ctx, func(stats *ConcurrentBufferStats) {
		atomic.AddInt64(&stats.ActiveWorkers, 1)
	})

	defer func() {
		duration := time.Since(startTime)
		w.buffer.updateStats(ctx, func(stats *ConcurrentBufferStats) {
			atomic.AddInt64(&stats.ActiveWorkers, -1)
			atomic.AddInt64(&stats.TotalFlushTime, duration.Milliseconds())
			atomic.AddInt64(&stats.CompletedTasks, 1)
			stats.LastFlushTime = time.Now().Unix()

			// 更新平均刷新时间
			totalTasks := atomic.LoadInt64(&stats.CompletedTasks)
			if totalTasks > 0 {
				stats.AvgFlushTime = atomic.LoadInt64(&stats.TotalFlushTime) / totalTasks
			}
		})
	}()

	// 执行刷新任务
	err := w.flushData(ctx, task.BufferKey, task.Rows)
	if err != nil {
		logger.LogInfo(ctx, "Worker %d: flush task failed for key %s: %v", zap.Int("worker_id", w.id), zap.String("buffer_key", task.BufferKey), zap.Error(err))

		// 重试逻辑
		if task.Retries < w.buffer.config.MaxRetries {
			task.Retries++
			task.Priority++ // 降低优先级

			// 延迟重试
			go func() {
				time.Sleep(w.buffer.config.RetryDelay * time.Duration(task.Retries))
				select {
				case w.buffer.taskQueue <- task:
					logger.LogInfo(ctx, "Retrying flush task for key %s (attempt %d)", zap.String("buffer_key", task.BufferKey), zap.Int("attempt", task.Retries+1))
				case <-w.buffer.shutdown:
					// 系统关闭，放弃重试
				}
			}()
		} else {
			logger.LogInfo(ctx, "Worker %d: max retries exceeded for key %s", zap.Int("worker_id", w.id), zap.String("buffer_key", task.BufferKey))
			w.buffer.updateStats(ctx, func(stats *ConcurrentBufferStats) {
				atomic.AddInt64(&stats.FailedTasks, 1)
			})
		}
	} else {
		logger.LogInfo(ctx, "Worker %d: successfully flushed %d rows for key %s", zap.Int("worker_id", w.id), zap.Int("rows", len(task.Rows)), zap.String("buffer_key", task.BufferKey))
	}
}

// Worker.flushData 执行数据刷新
func (w *Worker) flushData(ctx context.Context, bufferKey string, rows []DataRow) error {
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
	if err := w.writeParquetFile(ctx, localFilePath, rows); err != nil {
		return fmt.Errorf("failed to write parquet file: %w", err)
	}

	// 上传到存储
	return w.uploadToStorage(ctx, bufferKey, localFilePath)
}

// Worker.writeParquetFile 写入Parquet文件
func (w *Worker) writeParquetFile(ctx context.Context, filePath string, rows []DataRow) error {
	fw, err := local.NewLocalFileWriter(filePath)
	if err != nil {
		return fmt.Errorf("failed to create local file writer: %w", err)
	}
	defer fw.Close()

	pw, err := writer.NewParquetWriter(fw, new(DataRow), 4)
	if err != nil {
		return fmt.Errorf("failed to create parquet writer: %w", err)
	}
	pw.RowGroupSize = 128 * 1024 * 1024 // 128M
	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	for _, row := range rows {
		if err := pw.Write(row); err != nil {
			return fmt.Errorf("failed to write row: %w", err)
		}
	}

	return pw.WriteStop()
}

// Worker.uploadToStorage 上传到存储
func (w *Worker) uploadToStorage(ctx context.Context, bufferKey, localFilePath string) error {
	// 如果poolManager为nil（测试模式），直接返回成功
	if w.buffer.poolManager == nil {
		logger.LogInfo(ctx, "Test mode: skipping upload for key %s", zap.String("buffer_key", bufferKey))
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, w.buffer.config.FlushTimeout)
	defer cancel()

	// 生成对象名（支持智能分片优化）
	objectName := w.generateObjectName(ctx, bufferKey)

	// 上传到主存储
	minioPool := w.buffer.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	// 获取主存储桶名称
	primaryBucket := "miniodb-data" // 默认值
	if w.buffer.appConfig != nil && w.buffer.appConfig.MinIO.Bucket != "" {
		primaryBucket = w.buffer.appConfig.MinIO.Bucket
	}

	minioMetrics := metrics.NewMinIOMetrics("upload_primary")

	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		_, err := client.FPutObject(ctx, primaryBucket, objectName, localFilePath, minio.PutObjectOptions{})
		return err
	})

	if err != nil {
		minioMetrics.Finish("error")
		return fmt.Errorf("failed to upload to primary MinIO: %w", err)
	}
	minioMetrics.Finish("success")

	// 上传到备份存储（如果配置了）
	if backupPool := w.buffer.poolManager.GetBackupMinIOPool(); backupPool != nil && w.buffer.backupBucket != "" {
		backupMetrics := metrics.NewMinIOMetrics("upload_backup")

		err := backupPool.ExecuteWithRetry(ctx, func() error {
			client := backupPool.GetClient()
			_, err := client.FPutObject(ctx, w.buffer.backupBucket, objectName, localFilePath, minio.PutObjectOptions{})
			return err
		})

		if err != nil {
			logger.LogInfo(ctx, "WARN: failed to upload to backup storage: %v", zap.Error(err))
			backupMetrics.Finish("error")
		} else {
			backupMetrics.Finish("success")
		}
	}

	// 更新Redis索引
	if err := w.updateRedisIndex(ctx, bufferKey, objectName); err != nil {
		return err
	}

	// 提取表名并通知缓存失效
	// bufferKey格式: 表名/ID/日期
	parts := strings.Split(bufferKey, "/")
	if len(parts) >= 1 && w.buffer.cacheInvalidator != nil {
		tableName := parts[0]
		if err := w.buffer.cacheInvalidator.InvalidateTable(ctx, tableName); err != nil {
			// 缓存失效失败不应导致整个刷新失败，只记录警告
			logger.LogWarn(ctx, "Failed to invalidate cache after flush",
				zap.String("table", tableName),
				zap.Error(err),
			)
		} else {
			logger.LogInfo(ctx, "Cache invalidated after flush",
				zap.String("table", tableName),
				zap.String("buffer_key", bufferKey),
			)
		}
	}

	return nil
}

// Worker.generateObjectName 生成对象名（支持智能分片）
func (w *Worker) generateObjectName(ctx context.Context, bufferKey string) string {
	timestamp := time.Now().UnixNano()

	// 如果分片优化器未启用，使用默认命名
	if w.buffer.shardOptimizer == nil {
		return fmt.Sprintf("%s/%d.parquet", bufferKey, timestamp)
	}

	// 使用分片优化器决定数据放置
	// 解析 bufferKey: 表名/ID/日期
	parts := strings.Split(bufferKey, "/")
	if len(parts) != 3 {
		// 格式不正确，使用默认命名
		return fmt.Sprintf("%s/%d.parquet", bufferKey, timestamp)
	}

	tableName := parts[0]
	dataID := parts[1]
	dateStr := parts[2]

	// 生成分片键
	shardKey := fmt.Sprintf("%s:%s", tableName, dataID)

	// 创建访问模式（简化版，实际可以基于历史数据）
	accessPattern := &storage.AccessPattern{
		DataKey:       shardKey,
		AccessCount:   1,
		LastAccess:    time.Now(),
		AccessNodes:   make(map[string]int64),
		AccessRegions: make(map[string]int64),
		Frequency:     0.1,    // 新数据默认低频率
		Locality:      0.5,    // 中等局部性
		Temperature:   "warm", // 新数据默认为温数据
		Properties:    make(map[string]string),
	}

	// 使用分片优化器优化数据放置
	optimalNode, err := w.buffer.shardOptimizer.OptimizeDataPlacement(
		ctx,
		shardKey,
		0, // dataSize will be calculated later
		accessPattern,
	)

	if err != nil {
		logger.LogInfo(ctx, "WARN: shard optimization failed for key %s: %v, using default placement", zap.String("buffer_key", bufferKey), zap.Error(err))
		return fmt.Sprintf("%s/%d.parquet", bufferKey, timestamp)
	}

	// 生成带分片信息的对象名
	// 格式: 表名/日期/分片节点/ID_时间戳.parquet
	return fmt.Sprintf("%s/%s/shard_%s/%s_%d.parquet", tableName, dateStr, optimalNode, dataID, timestamp)
}

// Worker.updateRedisIndex 更新Redis索引
func (w *Worker) updateRedisIndex(ctx context.Context, bufferKey, objectName string) error {
	// 如果poolManager为nil（测试模式），直接返回成功
	if w.buffer.poolManager == nil {
		logger.LogInfo(ctx, "Test mode: skipping Redis index update for key %s", zap.String("buffer_key", bufferKey))
		return nil
	}

	redisPool := w.buffer.poolManager.GetRedisPool()
	if redisPool == nil {
		// 【修复】单节点模式下Redis不可用是正常情况，不应返回错误
		// 数据已经上传到MinIO，索引可以通过扫描MinIO bucket来构建
		logger.LogInfo(ctx, "Single-node mode: skipping Redis index update for key %s (Redis not available)", zap.String("buffer_key", bufferKey))
		return nil
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
			logger.LogInfo(ctx, "WARN: failed to update node data mapping: %v", zap.Error(err))
		}
	}

	return nil
}

// SetCacheInvalidator 设置缓存失效器
func (cb *ConcurrentBuffer) SetCacheInvalidator(invalidator CacheInvalidator) {
	cb.cacheInvalidator = invalidator
}

// Add 添加数据到缓冲区
func (cb *ConcurrentBuffer) Add(ctx context.Context, row DataRow) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	t := time.Unix(0, row.Timestamp)
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
	bufferKey := fmt.Sprintf("%s/%s/%s", tableName, row.ID, dayStr)

	cb.buffer[bufferKey] = append(cb.buffer[bufferKey], row)

	// 更新统计信息
	cb.updateStats(ctx, func(stats *ConcurrentBufferStats) {
		atomic.StoreInt64(&stats.BufferSize, int64(len(cb.buffer)))
		totalPending := int64(0)
		for _, rows := range cb.buffer {
			totalPending += int64(len(rows))
		}
		atomic.StoreInt64(&stats.PendingWrites, totalPending)
	})

	// 检查是否需要刷新
	if len(cb.buffer[bufferKey]) >= cb.config.BufferSize {
		cb.triggerFlush(ctx, bufferKey, false)
	}
}

// triggerFlush 触发刷新
func (cb *ConcurrentBuffer) triggerFlush(ctx context.Context, bufferKey string, force bool) {
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
	cb.mutex.Unlock()

	// 创建刷新任务
	task := &FlushTask{
		BufferKey: bufferKey,
		Rows:      rowsCopy,
		Priority:  0, // 默认优先级
		CreatedAt: time.Now(),
		Retries:   0,
	}

	// 提交任务到队列
	select {
	case cb.taskQueue <- task:
		cb.updateStats(ctx, func(stats *ConcurrentBufferStats) {
			atomic.AddInt64(&stats.TotalTasks, 1)
			atomic.AddInt64(&stats.QueuedTasks, 1)
		})

		// 记录缓冲区刷新指标
		if force {
			metrics.RecordBufferFlush("triggered_by_time")
		} else {
			metrics.RecordBufferFlush("triggered_by_size")
		}
	case <-cb.shutdown:
		// 系统关闭，直接执行刷新
		logger.LogInfo(ctx, "System shutting down, executing immediate flush for key %s", zap.String("buffer_key", bufferKey))
		worker := cb.workers[0] // 使用第一个worker
		worker.processTask(ctx, task)
	default:
		logger.LogInfo(ctx, "WARN: task queue full, dropping flush task for key %s", zap.String("buffer_key", bufferKey))
		cb.updateStats(ctx, func(stats *ConcurrentBufferStats) {
			atomic.AddInt64(&stats.FailedTasks, 1)
		})
	}
}

// periodicFlush 定期刷新
func (cb *ConcurrentBuffer) periodicFlush(ctx context.Context) {
	ticker := time.NewTicker(cb.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cb.flushAllBuffers(ctx)
		case <-cb.shutdown:
			logger.LogInfo(ctx, "Periodic flush stopped")
			return
		}
	}
}

// flushAllBuffers 刷新所有缓冲区
func (cb *ConcurrentBuffer) flushAllBuffers(ctx context.Context) {
	cb.mutex.RLock()
	keys := make([]string, 0, len(cb.buffer))
	for key := range cb.buffer {
		keys = append(keys, key)
	}
	cb.mutex.RUnlock()

	// 批量处理或逐个处理
	if cb.config.EnableBatching && len(keys) > cb.config.BatchFlushSize {
		cb.batchFlush(ctx, keys)
	} else {
		for _, key := range keys {
			cb.triggerFlush(ctx, key, true)
		}
	}
}

// batchFlush 批量刷新
func (cb *ConcurrentBuffer) batchFlush(ctx context.Context, keys []string) {
	// 按批次大小分组
	for i := 0; i < len(keys); i += cb.config.BatchFlushSize {
		end := i + cb.config.BatchFlushSize
		if end > len(keys) {
			end = len(keys)
		}

		batch := keys[i:end]
		for _, key := range batch {
			cb.triggerFlush(ctx, key, true)
		}

		// 批次间的小延迟，避免突发负载
		if i+cb.config.BatchFlushSize < len(keys) {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Get 获取缓冲区数据
func (cb *ConcurrentBuffer) Get(ctx context.Context, key string) []DataRow {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	rows := make([]DataRow, len(cb.buffer[key]))
	copy(rows, cb.buffer[key])
	return rows
}

// GetAllKeys 获取所有缓冲区键
func (cb *ConcurrentBuffer) GetAllKeys(ctx context.Context) []string {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	keys := make([]string, 0, len(cb.buffer))
	for key := range cb.buffer {
		keys = append(keys, key)
	}
	return keys
}

// Size 获取缓冲区大小
func (cb *ConcurrentBuffer) Size(ctx context.Context) int {
	return int(atomic.LoadInt64(&cb.stats.BufferSize))
}

// PendingWrites 获取待写入数据数量
func (cb *ConcurrentBuffer) PendingWrites(ctx context.Context) int {
	return int(atomic.LoadInt64(&cb.stats.PendingWrites))
}

// GetStats 获取统计信息
func (cb *ConcurrentBuffer) GetStats(ctx context.Context) *ConcurrentBufferStats {
	cb.stats.mutex.RLock()
	defer cb.stats.mutex.RUnlock()

	// 实时更新队列长度
	queuedTasks := int64(len(cb.taskQueue))
	atomic.StoreInt64(&cb.stats.QueuedTasks, queuedTasks)

	return &ConcurrentBufferStats{
		TotalTasks:     atomic.LoadInt64(&cb.stats.TotalTasks),
		CompletedTasks: atomic.LoadInt64(&cb.stats.CompletedTasks),
		FailedTasks:    atomic.LoadInt64(&cb.stats.FailedTasks),
		QueuedTasks:    queuedTasks,
		ActiveWorkers:  atomic.LoadInt64(&cb.stats.ActiveWorkers),
		AvgFlushTime:   atomic.LoadInt64(&cb.stats.AvgFlushTime),
		TotalFlushTime: atomic.LoadInt64(&cb.stats.TotalFlushTime),
		LastFlushTime:  cb.stats.LastFlushTime,
		BufferSize:     atomic.LoadInt64(&cb.stats.BufferSize),
		PendingWrites:  atomic.LoadInt64(&cb.stats.PendingWrites),
	}
}

// updateStats 更新统计信息
func (cb *ConcurrentBuffer) updateStats(ctx context.Context, updater func(*ConcurrentBufferStats)) {
	cb.stats.mutex.Lock()
	defer cb.stats.mutex.Unlock()
	updater(cb.stats)
}

// Stop 停止缓冲区
func (cb *ConcurrentBuffer) Stop(ctx context.Context) {
	cb.shutdownOnce.Do(func() {
		logger.LogInfo(ctx, "Stopping concurrent buffer...")

		// 刷新所有剩余数据
		cb.flushAllBuffers(ctx)

		// 关闭shutdown channel
		close(cb.shutdown)

		// 停止所有worker
		for _, worker := range cb.workers {
			close(worker.stopChan)
		}

		// 等待所有worker完成
		cb.workerWg.Wait()

		// 关闭任务队列
		close(cb.taskQueue)

		logger.LogInfo(ctx, "Concurrent buffer stopped successfully")
	})
}

// WriteTempParquetFile 写入临时Parquet文件（用于查询）
func (cb *ConcurrentBuffer) WriteTempParquetFile(ctx context.Context, filePath string, rows []DataRow) error {
	// 使用第一个worker的方法
	if len(cb.workers) > 0 {
		return cb.workers[0].writeParquetFile(ctx, filePath, rows)
	}
	return fmt.Errorf("no workers available")
}

// GetTableKeys 获取指定表的所有缓冲区键
func (cb *ConcurrentBuffer) GetTableKeys(ctx context.Context, tableName string) []string {
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
func (cb *ConcurrentBuffer) InvalidateTableConfig(ctx context.Context, tableName string) {
	// ConcurrentBuffer不缓存表配置，此方法为兼容性保留
	logger.LogInfo(ctx, "InvalidateTableConfig called for table %s (no-op in ConcurrentBuffer)", zap.String("table", tableName))
}

// GetTableBufferKeys 获取指定表的所有缓冲区键（别名方法，兼容性）
func (cb *ConcurrentBuffer) GetTableBufferKeys(ctx context.Context, tableName string) []string {
	return cb.GetTableKeys(ctx, tableName)
}

// GetBufferData 获取指定键的缓冲区数据（用于混合查询）
func (cb *ConcurrentBuffer) GetBufferData(ctx context.Context, key string) []DataRow {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	// 记录查询活动
	atomic.AddInt64(&cb.queryCount, 1)
	atomic.StoreInt64(&cb.lastQueryTime, time.Now().UnixNano())

	// 触发自适应刷新检查
	go cb.CheckAdaptiveFlush(ctx)

	rows, exists := cb.buffer[key]
	if !exists {
		return []DataRow{}
	}

	// 返回副本以避免并发问题
	result := make([]DataRow, len(rows))
	copy(result, rows)
	return result
}

// CheckAdaptiveFlush 检查是否需要自适应刷新（优化3）
func (cb *ConcurrentBuffer) CheckAdaptiveFlush(ctx context.Context) {
	if !cb.adaptiveFlush {
		return
	}

	// 获取统计信息
	pendingWrites := atomic.LoadInt64(&cb.stats.PendingWrites)
	queryCount := atomic.LoadInt64(&cb.queryCount)

	// 自适应策略：
	// 1. 如果缓冲区有大量待写入数据（>80%）且查询频繁（>10次/分钟），触发刷新
	// 2. 如果缓冲区接近满（>90%），立即刷新
	// 3. 如果内存占用超过阈值，触发刷新（新增）

	bufferUtilization := float64(pendingWrites) / float64(cb.config.BufferSize)

	// 计算查询频率（最近1分钟）
	lastQueryTime := atomic.LoadInt64(&cb.lastQueryTime)
	timeSinceLastQuery := time.Since(time.Unix(0, lastQueryTime))
	queryFrequent := queryCount > 10 && timeSinceLastQuery < time.Minute

	shouldFlush := false
	reason := ""

	// 检查缓冲区利用率
	if bufferUtilization > 0.9 {
		shouldFlush = true
		reason = "buffer nearly full (>90%)"
	} else if bufferUtilization > 0.8 && queryFrequent {
		shouldFlush = true
		reason = "high buffer utilization (>80%) with frequent queries"
	}

	// 检查内存压力（如果启用）
	if !shouldFlush && cb.config.EnableMemoryFlush && cb.config.MemoryThreshold > 0 {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		// 使用Alloc（当前分配的堆内存）作为内存压力指标
		currentMemory := int64(memStats.Alloc)
		memoryUtilization := float64(currentMemory) / float64(cb.config.MemoryThreshold)

		if currentMemory > cb.config.MemoryThreshold {
			shouldFlush = true
			reason = fmt.Sprintf("memory pressure (current: %s, threshold: %s, utilization: %.1f%%)",
				utils.FormatBytes(currentMemory),
				utils.FormatBytes(cb.config.MemoryThreshold),
				memoryUtilization*100)
		}
	}

	if shouldFlush {
		logger.LogInfo(ctx,
			"Adaptive flush triggered",
			zap.String("reason", reason),
			zap.Float64("buffer_utilization", bufferUtilization*100),
			zap.Int64("query_count", queryCount))
		cb.flushAllBuffers(ctx)

		// 重置查询计数
		atomic.StoreInt64(&cb.queryCount, 0)

		// 触发GC以释放内存（可选，根据情况调整）
		if cb.config.EnableMemoryFlush {
			go runtime.GC()
		}
	}
}

// AddDataPoint 添加数据点到缓冲区（兼容bufferManager接口）
func (cb *ConcurrentBuffer) AddDataPoint(ctx context.Context, id string, data []byte, timestamp time.Time) {
	row := DataRow{
		ID:        id,
		Timestamp: timestamp.UnixNano(),
		Payload:   string(data),
	}
	cb.Add(ctx, row)
}

// FlushDataPoints 手动刷新所有缓冲区（兼容bufferManager接口）
func (cb *ConcurrentBuffer) FlushDataPoints(ctx context.Context) error {
	cb.flushAllBuffers(ctx)
	return nil
}

// GetMemoryStats 获取内存优化统计（新增）
func (cb *ConcurrentBuffer) GetMemoryStats(ctx context.Context) map[string]interface{} {
	if cb.memoryOptimizer == nil {
		return map[string]interface{}{
			"enabled": false,
			"message": "Memory optimizer not initialized",
		}
	}

	stats := cb.memoryOptimizer.GetStats()
	return map[string]interface{}{
		"enabled":             true,
		"total_allocated":     stats.TotalAllocated,
		"current_usage":       stats.CurrentUsage,
		"peak_usage":          stats.PeakUsage,
		"pool_efficiency":     stats.PoolEfficiency,
		"fragmentation_ratio": stats.FragmentationRatio,
		"gc_efficiency":       stats.GCEfficiency,
	}
}
