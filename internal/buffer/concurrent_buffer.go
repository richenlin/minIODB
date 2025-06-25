package buffer

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"minIODB/internal/metrics"
	"minIODB/internal/pool"

	"github.com/minio/minio-go/v7"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// ConcurrentBufferConfig 并发缓冲区配置
type ConcurrentBufferConfig struct {
	BufferSize       int           `yaml:"buffer_size"`        // 缓冲区大小
	FlushInterval    time.Duration `yaml:"flush_interval"`     // 刷新间隔
	WorkerPoolSize   int           `yaml:"worker_pool_size"`   // 工作池大小
	TaskQueueSize    int           `yaml:"task_queue_size"`    // 任务队列大小
	BatchFlushSize   int           `yaml:"batch_flush_size"`   // 批量刷新大小
	EnableBatching   bool          `yaml:"enable_batching"`    // 启用批量处理
	FlushTimeout     time.Duration `yaml:"flush_timeout"`      // 刷新超时
	MaxRetries       int           `yaml:"max_retries"`        // 最大重试次数
	RetryDelay       time.Duration `yaml:"retry_delay"`        // 重试延迟
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
	}
}

// FlushTask 刷新任务
type FlushTask struct {
	BufferKey string
	Rows      []DataRow
	Priority  int       // 任务优先级，数字越小优先级越高
	CreatedAt time.Time
	Retries   int
}

// ConcurrentBuffer 支持并发刷新的缓冲区
type ConcurrentBuffer struct {
	config      *ConcurrentBufferConfig
	poolManager *pool.PoolManager
	backupBucket string
	nodeID      string

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
}

// ConcurrentBufferStats 并发缓冲区统计信息
type ConcurrentBufferStats struct {
	TotalTasks       int64 `json:"total_tasks"`
	CompletedTasks   int64 `json:"completed_tasks"`
	FailedTasks      int64 `json:"failed_tasks"`
	QueuedTasks      int64 `json:"queued_tasks"`
	ActiveWorkers    int64 `json:"active_workers"`
	AvgFlushTime     int64 `json:"avg_flush_time_ms"`
	TotalFlushTime   int64 `json:"total_flush_time_ms"`
	LastFlushTime    int64 `json:"last_flush_time"`
	BufferSize       int64 `json:"buffer_size"`
	PendingWrites    int64 `json:"pending_writes"`
	mutex            sync.RWMutex
}

// Worker 工作线程
type Worker struct {
	id       int
	buffer   *ConcurrentBuffer
	active   int64 // 原子操作标志
	stopChan chan struct{}
}

// NewConcurrentBuffer 创建新的并发缓冲区
func NewConcurrentBuffer(poolManager *pool.PoolManager, backupBucket, nodeID string, config *ConcurrentBufferConfig) *ConcurrentBuffer {
	if config == nil {
		config = DefaultConcurrentBufferConfig()
	}

	cb := &ConcurrentBuffer{
		config:       config,
		poolManager:  poolManager,
		backupBucket: backupBucket,
		nodeID:       nodeID,
		buffer:       make(map[string][]DataRow),
		taskQueue:    make(chan *FlushTask, config.TaskQueueSize),
		workers:      make([]*Worker, config.WorkerPoolSize),
		shutdown:     make(chan struct{}),
		stats:        &ConcurrentBufferStats{},
	}

	// 启动工作线程
	cb.startWorkers()

	// 启动定期刷新
	go cb.periodicFlush()

	log.Printf("Concurrent buffer initialized with %d workers, queue size %d", 
		config.WorkerPoolSize, config.TaskQueueSize)
	
	return cb
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
			log.Printf("Worker %d stopping", w.id)
			return
		case <-w.buffer.shutdown:
			log.Printf("Worker %d shutting down", w.id)
			return
		}
	}
}

// Worker.processTask 处理刷新任务
func (w *Worker) processTask(task *FlushTask) {
	startTime := time.Now()
	
	// 更新统计信息
	w.buffer.updateStats(func(stats *ConcurrentBufferStats) {
		atomic.AddInt64(&stats.ActiveWorkers, 1)
	})
	
	defer func() {
		duration := time.Since(startTime)
		w.buffer.updateStats(func(stats *ConcurrentBufferStats) {
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
	err := w.flushData(task.BufferKey, task.Rows)
	if err != nil {
		log.Printf("Worker %d: flush task failed for key %s: %v", w.id, task.BufferKey, err)
		
		// 重试逻辑
		if task.Retries < w.buffer.config.MaxRetries {
			task.Retries++
			task.Priority++ // 降低优先级
			
			// 延迟重试
			go func() {
				time.Sleep(w.buffer.config.RetryDelay * time.Duration(task.Retries))
				select {
				case w.buffer.taskQueue <- task:
					log.Printf("Retrying flush task for key %s (attempt %d)", task.BufferKey, task.Retries+1)
				case <-w.buffer.shutdown:
					// 系统关闭，放弃重试
				}
			}()
		} else {
			log.Printf("Worker %d: max retries exceeded for key %s", w.id, task.BufferKey)
			w.buffer.updateStats(func(stats *ConcurrentBufferStats) {
				atomic.AddInt64(&stats.FailedTasks, 1)
			})
		}
	} else {
		log.Printf("Worker %d: successfully flushed %d rows for key %s", w.id, len(task.Rows), task.BufferKey)
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

	// 上传到存储
	return w.uploadToStorage(bufferKey, localFilePath)
}

// Worker.writeParquetFile 写入Parquet文件
func (w *Worker) writeParquetFile(filePath string, rows []DataRow) error {
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
func (w *Worker) uploadToStorage(bufferKey, localFilePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.buffer.config.FlushTimeout)
	defer cancel()

	objectName := fmt.Sprintf("%s/%d.parquet", bufferKey, time.Now().UnixNano())

	// 上传到主存储
	minioPool := w.buffer.poolManager.GetMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	minioMetrics := metrics.NewMinIOMetrics("upload_primary")
	
	err := minioPool.ExecuteWithRetry(ctx, func() error {
		client := minioPool.GetClient()
		_, err := client.FPutObject(ctx, minioBucket, objectName, localFilePath, minio.PutObjectOptions{})
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
			log.Printf("WARN: failed to upload to backup storage: %v", err)
			backupMetrics.Finish("error")
		} else {
			backupMetrics.Finish("success")
		}
	}

	// 更新Redis索引
	return w.updateRedisIndex(ctx, bufferKey, objectName)
}

// Worker.updateRedisIndex 更新Redis索引
func (w *Worker) updateRedisIndex(ctx context.Context, bufferKey, objectName string) error {
	redisPool := w.buffer.poolManager.GetRedisPool()
	if redisPool == nil {
		return fmt.Errorf("Redis pool not available")
	}

	client := redisPool.GetClient()
	redisKey := fmt.Sprintf("index:id:%s", bufferKey)
	
	if _, err := client.SAdd(ctx, redisKey, objectName).Result(); err != nil {
		return fmt.Errorf("failed to update redis index: %w", err)
	}

	// 更新节点数据映射
	if w.buffer.nodeID != "" {
		nodeDataKey := fmt.Sprintf("node:data:%s", w.buffer.nodeID)
		if _, err := client.SAdd(ctx, nodeDataKey, redisKey).Result(); err != nil {
			log.Printf("WARN: failed to update node data mapping: %v", err)
		}
	}

	return nil
}

// Add 添加数据到缓冲区
func (cb *ConcurrentBuffer) Add(row DataRow) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	t := time.Unix(0, row.Timestamp)
	dayStr := t.Format("2006-01-02")
	bufferKey := fmt.Sprintf("%s/%s", row.ID, dayStr)

	cb.buffer[bufferKey] = append(cb.buffer[bufferKey], row)
	
	// 更新统计信息
	cb.updateStats(func(stats *ConcurrentBufferStats) {
		atomic.StoreInt64(&stats.BufferSize, int64(len(cb.buffer)))
		totalPending := int64(0)
		for _, rows := range cb.buffer {
			totalPending += int64(len(rows))
		}
		atomic.StoreInt64(&stats.PendingWrites, totalPending)
	})

	// 检查是否需要刷新
	if len(cb.buffer[bufferKey]) >= cb.config.BufferSize {
		cb.triggerFlush(bufferKey, false)
	}
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
		cb.updateStats(func(stats *ConcurrentBufferStats) {
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
		log.Printf("System shutting down, executing immediate flush for key %s", bufferKey)
		worker := cb.workers[0] // 使用第一个worker
		worker.processTask(task)
	default:
		log.Printf("WARN: task queue full, dropping flush task for key %s", bufferKey)
		cb.updateStats(func(stats *ConcurrentBufferStats) {
			atomic.AddInt64(&stats.FailedTasks, 1)
		})
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
			log.Println("Periodic flush stopped")
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
	return int(atomic.LoadInt64(&cb.stats.BufferSize))
}

// PendingWrites 获取待写入数据数量
func (cb *ConcurrentBuffer) PendingWrites() int {
	return int(atomic.LoadInt64(&cb.stats.PendingWrites))
}

// GetStats 获取统计信息
func (cb *ConcurrentBuffer) GetStats() *ConcurrentBufferStats {
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
func (cb *ConcurrentBuffer) updateStats(updater func(*ConcurrentBufferStats)) {
	cb.stats.mutex.Lock()
	defer cb.stats.mutex.Unlock()
	updater(cb.stats)
}

// Stop 停止缓冲区
func (cb *ConcurrentBuffer) Stop() {
	cb.shutdownOnce.Do(func() {
		log.Println("Stopping concurrent buffer...")
		
		// 刷新所有剩余数据
		cb.flushAllBuffers()
		
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
		
		log.Println("Concurrent buffer stopped successfully")
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