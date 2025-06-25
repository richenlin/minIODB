package buffer

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

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
}

const tempDir = "temp_parquet"
const minioBucket = "olap-data"

// SharedBuffer handles in-memory buffering and flushing to persistent storage.
type SharedBuffer struct {
	redisClient   *redis.Client
	primaryClient storage.Uploader
	backupClient  storage.Uploader // Can be nil
	backupBucket  string
	nodeID        string // 当前节点ID

	buffer        map[string][]DataRow
	mu            sync.RWMutex
	shutdown      chan struct{}
	bufferSize    int
	flushInterval time.Duration

	// For testing only
	flushDone chan struct{}
}

// NewSharedBuffer creates a new SharedBuffer and starts its background flush mechanism.
func NewSharedBuffer(redisClient *redis.Client, primaryClient storage.Uploader, backupClient storage.Uploader, backupBucket string, bufferSize int, flushInterval time.Duration) *SharedBuffer {
	return NewSharedBufferWithNodeID(redisClient, primaryClient, backupClient, backupBucket, bufferSize, flushInterval, "")
}

// NewSharedBufferWithNodeID creates a new SharedBuffer with specific node ID
func NewSharedBufferWithNodeID(redisClient *redis.Client, primaryClient storage.Uploader, backupClient storage.Uploader, backupBucket string, bufferSize int, flushInterval time.Duration, nodeID string) *SharedBuffer {
	b := &SharedBuffer{
		redisClient:   redisClient,
		primaryClient: primaryClient,
		backupClient:  backupClient,
		backupBucket:  backupBucket,
		nodeID:        nodeID,
		buffer:        make(map[string][]DataRow),
		shutdown:      make(chan struct{}),
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
	}
	go b.runFlusher()
	return b
}

// Add adds a DataRow to the buffer.
func (b *SharedBuffer) Add(row DataRow) {
	b.mu.Lock()
	defer b.mu.Unlock()

	t := time.Unix(0, row.Timestamp)
	dayStr := t.Format("2006-01-02")
	bufferKey := fmt.Sprintf("%s/%s", row.ID, dayStr)

	b.buffer[bufferKey] = append(b.buffer[bufferKey], row)

	if len(b.buffer[bufferKey]) >= b.bufferSize {
		// copy rows to avoid race condition when flushing
		rowsToFlush := make([]DataRow, len(b.buffer[bufferKey]))
		copy(rowsToFlush, b.buffer[bufferKey])
		go b.flushBuffer(bufferKey, rowsToFlush)
		delete(b.buffer, bufferKey)
	}
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

func (b *SharedBuffer) runFlusher() {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.mu.Lock()
			for key, rows := range b.buffer {
				if len(rows) > 0 {
					rowsToFlush := make([]DataRow, len(rows))
					copy(rowsToFlush, rows)
					go b.flushBuffer(key, rowsToFlush)
					delete(b.buffer, key)
				}
			}
			b.mu.Unlock()
		case <-b.shutdown:
			b.mu.Lock()
			for key, rows := range b.buffer {
				if len(rows) > 0 {
					b.flushBuffer(key, rows)
				}
			}
			b.mu.Unlock()
			return
		}
	}
}

func (b *SharedBuffer) flushBuffer(bufferKey string, rows []DataRow) {
	if len(rows) == 0 {
		return
	}
	log.Printf("Flushing buffer for key %s with %d rows", bufferKey, len(rows))

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
	objectName := fmt.Sprintf("%s/%d.parquet", bufferKey, time.Now().UnixNano())

	if err := b.ensureBucketExists(ctx, minioBucket, b.primaryClient); err != nil {
		log.Printf("ERROR: failed to ensure bucket exists for key %s: %v", bufferKey, err)
		return
	}

	if _, err := b.primaryClient.FPutObject(ctx, minioBucket, objectName, localFilePath, minio.PutObjectOptions{}); err != nil {
		log.Printf("ERROR: failed to upload to primary minio for key %s: %v", bufferKey, err)
		return
	}
	log.Printf("Successfully uploaded %s to Primary MinIO", objectName)

	// Automatic backup if enabled
	if b.backupClient != nil {
		err := b.ensureBucketExists(ctx, b.backupBucket, b.backupClient)
		if err != nil {
			log.Printf("ERROR: failed to ensure backup bucket exists for key %s: %v", bufferKey, err)
			// Don't return, primary write succeeded
		} else {
			_, err = b.backupClient.FPutObject(ctx, b.backupBucket, objectName, localFilePath, minio.PutObjectOptions{})
			if err != nil {
				log.Printf("ERROR: failed to upload to backup minio for key %s: %v", bufferKey, err)
			} else {
				log.Printf("Successfully backed up %s", objectName)
			}
		}
	}

	redisKey := fmt.Sprintf("index:id:%s", bufferKey)
	if _, err := b.redisClient.SAdd(ctx, redisKey, objectName).Result(); err != nil {
		log.Printf("ERROR: failed to update redis index for key %s: %v", bufferKey, err)
		return
	}
	log.Printf("Successfully updated index %s in Redis", redisKey)

	// 如果有节点ID，同时维护节点到数据的映射
	if b.nodeID != "" {
		nodeDataKey := fmt.Sprintf("node:data:%s", b.nodeID)
		if _, err := b.redisClient.SAdd(ctx, nodeDataKey, redisKey).Result(); err != nil {
			log.Printf("ERROR: failed to update node data mapping for key %s: %v", bufferKey, err)
		} else {
			log.Printf("Successfully updated node data mapping %s -> %s", b.nodeID, redisKey)
		}
	}

	// Signal for tests that a flush has completed
	if b.flushDone != nil {
		b.flushDone <- struct{}{}
	}
}

func (b *SharedBuffer) writeParquetFile(filePath string, rows []DataRow) error {
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
	if err = pw.WriteStop(); err != nil {
		return fmt.Errorf("failed to stop parquet writer: %w", err)
	}
	return nil
}

func (b *SharedBuffer) ensureBucketExists(ctx context.Context, bucketName string, client storage.Uploader) error {
	exists, err := client.BucketExists(ctx, bucketName)
	if err != nil {
		return err
	}
	if !exists {
		if err = client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
			return err
		}
		log.Printf("Successfully created bucket: %s", bucketName)
	}
	return nil
}

// Stop gracefully shuts down the buffer, flushing any remaining data.
func (b *SharedBuffer) Stop() {
	close(b.shutdown)
}

// WriteTempParquetFile writes a slice of DataRows to a temporary parquet file
// and returns the path. This is used by the Querier.
func (b *SharedBuffer) WriteTempParquetFile(filePath string, rows []DataRow) error {
	// This just calls the private method. The logic is already there.
	return b.writeParquetFile(filePath, rows)
}
