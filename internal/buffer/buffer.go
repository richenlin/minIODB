package buffer

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/metrics"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// DataRow defines the structure for our Parquet file records
type DataRow struct {
	ID        string `json:"id" parquet:"name=id, type=BYTE_ARRAY, convertedtype=UTF8"`
	Timestamp int64  `json:"timestamp" parquet:"name=timestamp, type=INT64"`
	Payload   string `json:"payload" parquet:"name=payload, type=BYTE_ARRAY, convertedtype=UTF8"`
	Table     string `json:"table" parquet:"name=table, type=BYTE_ARRAY, convertedtype=UTF8"`
}

const tempDir = "temp_parquet"

// SharedBuffer handles in-memory buffering and flushing to persistent storage.
type SharedBuffer struct {
	redisClient   *redis.Client
	primaryClient storage.Uploader
	backupClient  storage.Uploader // Can be nil
	backupBucket  string
	nodeID        string // 当前节点ID
	config        *config.Config

	buffer       map[string][]DataRow           // key: "{TABLE}/{ID}/{YYYY-MM-DD}"
	tableConfigs map[string]*config.TableConfig // 表级配置缓存
	mu           sync.RWMutex
	shutdown     chan struct{}

	// For testing only
	flushDone chan struct{}
}

// NewSharedBuffer creates a new SharedBuffer and starts its background flush mechanism.
func NewSharedBuffer(redisClient *redis.Client, primaryClient storage.Uploader, backupClient storage.Uploader, backupBucket string, cfg *config.Config) *SharedBuffer {
	return NewSharedBufferWithNodeID(redisClient, primaryClient, backupClient, backupBucket, cfg, "")
}

// NewSharedBufferWithNodeID creates a new SharedBuffer with specific node ID
func NewSharedBufferWithNodeID(redisClient *redis.Client, primaryClient storage.Uploader, backupClient storage.Uploader, backupBucket string, cfg *config.Config, nodeID string) *SharedBuffer {
	b := &SharedBuffer{
		redisClient:   redisClient,
		primaryClient: primaryClient,
		backupClient:  backupClient,
		backupBucket:  backupBucket,
		nodeID:        nodeID,
		config:        cfg,
		buffer:        make(map[string][]DataRow),
		tableConfigs:  make(map[string]*config.TableConfig),
		shutdown:      make(chan struct{}),
	}
	go b.runFlusher()
	return b
}

// Add adds a DataRow to the buffer with table support.
func (b *SharedBuffer) Add(row DataRow) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 获取表配置
	tableConfig := b.getTableConfig(row.Table)

	t := time.Unix(0, row.Timestamp)
	dayStr := t.Format("2006-01-02")
	bufferKey := fmt.Sprintf("%s/%s/%s", row.Table, row.ID, dayStr)

	b.buffer[bufferKey] = append(b.buffer[bufferKey], row)

	// 更新缓冲区大小指标
	metrics.UpdateBufferSize(fmt.Sprintf("table_%s", row.Table), float64(len(b.buffer[bufferKey])))

	if len(b.buffer[bufferKey]) >= tableConfig.BufferSize {
		// copy rows to avoid race condition when flushing
		rowsToFlush := make([]DataRow, len(b.buffer[bufferKey]))
		copy(rowsToFlush, b.buffer[bufferKey])
		go b.flushBuffer(bufferKey, rowsToFlush)
		delete(b.buffer, bufferKey)

		// 记录缓冲区刷新指标
		metrics.RecordBufferFlush(fmt.Sprintf("table_%s_triggered_by_size", row.Table))
	}
}

// getTableConfig 获取表配置，如果不存在则使用默认配置
func (b *SharedBuffer) getTableConfig(tableName string) *config.TableConfig {
	// 先检查缓存
	if config, exists := b.tableConfigs[tableName]; exists {
		return config
	}

	// 从全局配置获取
	config := b.config.GetTableConfig(tableName)

	// 缓存配置
	b.tableConfigs[tableName] = config

	return config
}

// InvalidateTableConfig 使表配置缓存失效
func (b *SharedBuffer) InvalidateTableConfig(tableName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tableConfigs, tableName)
}

// Get retrieves data from the buffer for a given key. It is read-safe.
func (b *SharedBuffer) Get(key string) []DataRow {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Return a copy to prevent race conditions on the slice
	rows := make([]DataRow, len(b.buffer[key]))
	copy(rows, b.buffer[key])
	return rows
}

// GetTableBufferKeys 获取指定表的所有缓冲区键
func (b *SharedBuffer) GetTableBufferKeys(tableName string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	prefix := tableName + "/"
	var keys []string
	for key := range b.buffer {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys
}

func (b *SharedBuffer) runFlusher() {
	// 为每个表创建独立的刷新定时器
	tableTimers := make(map[string]*time.Ticker)

	// 主定时器，用于检查表级配置更新
	mainTicker := time.NewTicker(1 * time.Minute)
	defer mainTicker.Stop()

	for {
		select {
		case <-mainTicker.C:
			b.mu.Lock()

			// 按表分组处理
			tableBuffers := make(map[string][]string)
			for key := range b.buffer {
				parts := splitBufferKey(key)
				if len(parts) >= 3 {
					tableName := parts[0]
					tableBuffers[tableName] = append(tableBuffers[tableName], key)
				}
			}

			// 为每个表检查是否需要刷新
			for tableName, keys := range tableBuffers {
				tableConfig := b.getTableConfig(tableName)

				// 检查是否需要按时间刷新
				for _, key := range keys {
					rows := b.buffer[key]
					if len(rows) > 0 {
						// 检查最老的数据是否超过刷新间隔
						oldestTime := time.Unix(0, rows[0].Timestamp)
						if time.Since(oldestTime) >= tableConfig.FlushInterval {
							rowsToFlush := make([]DataRow, len(rows))
							copy(rowsToFlush, rows)
							go b.flushBuffer(key, rowsToFlush)
							delete(b.buffer, key)

							// 记录缓冲区刷新指标
							metrics.RecordBufferFlush(fmt.Sprintf("table_%s_triggered_by_time", tableName))
						}
					}
				}
			}

			b.mu.Unlock()

		case <-b.shutdown:
			b.mu.Lock()
			for key, rows := range b.buffer {
				if len(rows) > 0 {
					b.flushBuffer(key, rows)
					// 记录缓冲区刷新指标
					parts := splitBufferKey(key)
					if len(parts) >= 3 {
						metrics.RecordBufferFlush(fmt.Sprintf("table_%s_triggered_by_shutdown", parts[0]))
					}
				}
			}
			b.mu.Unlock()

			// 清理定时器
			for _, ticker := range tableTimers {
				ticker.Stop()
			}
			return
		}
	}
}

// splitBufferKey 分割缓冲区键
func splitBufferKey(key string) []string {
	// key格式: "{TABLE}/{ID}/{YYYY-MM-DD}"
	parts := make([]string, 0, 3)
	start := 0
	slashCount := 0

	for i, char := range key {
		if char == '/' {
			if slashCount < 2 {
				parts = append(parts, key[start:i])
				start = i + 1
				slashCount++
			}
		}
	}

	// 添加最后一部分
	if start < len(key) {
		parts = append(parts, key[start:])
	}

	return parts
}

func (b *SharedBuffer) flushBuffer(bufferKey string, rows []DataRow) {
	if len(rows) == 0 {
		return
	}
	log.Printf("Flushing buffer for key %s with %d rows", bufferKey, len(rows))

	// 解析缓冲区键获取表名
	parts := splitBufferKey(bufferKey)
	if len(parts) < 3 {
		log.Printf("ERROR: invalid buffer key format: %s", bufferKey)
		return
	}
	tableName := parts[0]

	localFilePath := filepath.Join(tempDir, fmt.Sprintf("%s-%d.parquet", bufferKey, time.Now().UnixNano()))

	// 确保创建完整的目录结构
	localFileDir := filepath.Dir(localFilePath)
	if err := os.MkdirAll(localFileDir, 0755); err != nil {
		log.Printf("ERROR: failed to create temp dir %s: %v", localFileDir, err)
		return
	}
	defer os.Remove(localFilePath)

	if err := b.writeParquetFile(localFilePath, rows); err != nil {
		log.Printf("ERROR: failed to write parquet file for key %s: %v", bufferKey, err)
		return
	}

	ctx := context.Background()
	// 新的对象路径格式: TABLE/ID/YYYY-MM-DD/timestamp.parquet
	objectName := fmt.Sprintf("%s/%d.parquet", bufferKey, time.Now().UnixNano())

	// Upload to primary MinIO
	minioMetrics := metrics.NewMinIOMetrics("upload_primary")
	exists, err := b.primaryClient.BucketExists(ctx, b.config.MinIO.Bucket)
	if err != nil {
		log.Printf("ERROR: primary bucket check failed: %v", err)
		minioMetrics.Finish("error")
		return
	}
	if !exists {
		log.Printf("ERROR: primary bucket does not exist: %s", b.config.MinIO.Bucket)
		minioMetrics.Finish("error")
		return
	}

	_, err = b.primaryClient.FPutObject(ctx, b.config.MinIO.Bucket, objectName, localFilePath, minio.PutObjectOptions{})
	if err != nil {
		log.Printf("ERROR: failed to upload to primary MinIO: %v", err)
		minioMetrics.Finish("error")
		return
	}
	minioMetrics.Finish("success")

	// Upload to backup MinIO if available and enabled for this table
	tableConfig := b.getTableConfig(tableName)
	if b.backupClient != nil && tableConfig.BackupEnabled {
		backupMetrics := metrics.NewMinIOMetrics("upload_backup")
		exists, err := b.backupClient.BucketExists(ctx, b.backupBucket)
		if err != nil {
			log.Printf("ERROR: backup bucket check failed: %v", err)
			backupMetrics.Finish("error")
		} else if !exists {
			log.Printf("ERROR: backup bucket does not exist: %s", b.backupBucket)
			backupMetrics.Finish("error")
		} else {
			_, err = b.backupClient.FPutObject(ctx, b.backupBucket, objectName, localFilePath, minio.PutObjectOptions{})
			if err != nil {
				log.Printf("ERROR: failed to upload to backup MinIO: %v", err)
				backupMetrics.Finish("error")
			} else {
				backupMetrics.Finish("success")
			}
		}
	}

	// Update Redis index with new format: index:table:{TABLE}:id:{ID}:{YYYY-MM-DD}
	redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", parts[0], parts[1], parts[2])
	if err := b.redisClient.SAdd(ctx, redisKey, objectName).Err(); err != nil {
		log.Printf("ERROR: failed to update Redis index for key %s: %v", redisKey, err)
		return
	}

	// 更新表统计信息
	b.updateTableStats(ctx, tableName, int64(len(rows)))

	log.Printf("Successfully flushed buffer for key %s", bufferKey)

	// Notify for testing
	if b.flushDone != nil {
		select {
		case b.flushDone <- struct{}{}:
		default:
		}
	}
}

// updateTableStats 更新表统计信息
func (b *SharedBuffer) updateTableStats(ctx context.Context, tableName string, recordCount int64) {
	statsKey := fmt.Sprintf("table:%s:stats", tableName)

	// 增加记录数
	if err := b.redisClient.HIncrBy(ctx, statsKey, "record_count", recordCount).Err(); err != nil {
		log.Printf("WARN: failed to update record count for table %s: %v", tableName, err)
	}

	// 增加文件数
	if err := b.redisClient.HIncrBy(ctx, statsKey, "file_count", 1).Err(); err != nil {
		log.Printf("WARN: failed to update file count for table %s: %v", tableName, err)
	}

	// 更新最新记录时间
	now := time.Now().UTC().Format(time.RFC3339)
	if err := b.redisClient.HSet(ctx, statsKey, "newest_record", now).Err(); err != nil {
		log.Printf("WARN: failed to update newest record time for table %s: %v", tableName, err)
	}

	// 如果是第一次写入，设置最老记录时间
	exists, err := b.redisClient.HExists(ctx, statsKey, "oldest_record").Result()
	if err == nil && !exists {
		if err := b.redisClient.HSet(ctx, statsKey, "oldest_record", now).Err(); err != nil {
			log.Printf("WARN: failed to set oldest record time for table %s: %v", tableName, err)
		}
	}
}

func (b *SharedBuffer) writeParquetFile(filePath string, rows []DataRow) error {
	fw, err := local.NewLocalFileWriter(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file writer: %w", err)
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
			pw.WriteStop()
			return fmt.Errorf("failed to write row: %w", err)
		}
	}

	if err := pw.WriteStop(); err != nil {
		return fmt.Errorf("failed to finalize parquet file: %w", err)
	}

	return nil
}

func (b *SharedBuffer) ensureBucketExists(ctx context.Context, bucketName string, client storage.Uploader) error {
	exists, err := client.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("bucket %s does not exist", bucketName)
	}
	return nil
}

func (b *SharedBuffer) Stop() {
	close(b.shutdown)
}

// WriteTempParquetFile is for testing purposes
func (b *SharedBuffer) WriteTempParquetFile(filePath string, rows []DataRow) error {
	return b.writeParquetFile(filePath, rows)
}

func (b *SharedBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	total := 0
	for _, rows := range b.buffer {
		total += len(rows)
	}
	return total
}

func (b *SharedBuffer) PendingWrites() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.buffer)
}

func (b *SharedBuffer) GetAllKeys() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	keys := make([]string, 0, len(b.buffer))
	for k := range b.buffer {
		keys = append(keys, k)
	}
	return keys
}

// GetTableKeys 获取指定表的所有缓冲区键
func (b *SharedBuffer) GetTableKeys(tableName string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	prefix := tableName + "/"
	var keys []string
	for key := range b.buffer {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys
}
