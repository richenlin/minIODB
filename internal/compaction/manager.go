package compaction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"minIODB/internal/storage"
	"minIODB/pkg/logger"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

type Config struct {
	TargetFileSize    int64
	MinFilesToCompact int
	MaxFilesToCompact int
	CooldownPeriod    time.Duration
	CheckInterval     time.Duration
	TempDir           string
	CompressionType   string
	MaxRowsPerFile    int64 // 每个文件最大行数
}

func DefaultConfig() *Config {
	return &Config{
		TargetFileSize:    128 * 1024 * 1024,
		MinFilesToCompact: 5,
		MaxFilesToCompact: 20,
		CooldownPeriod:    5 * time.Minute,
		CheckInterval:     10 * time.Minute,
		TempDir:           filepath.Join(os.TempDir(), "miniodb_compaction"),
		CompressionType:   "snappy",
		MaxRowsPerFile:    1000000, // 100 万行
	}
}

type Manager struct {
	config      *Config
	minioClient *minio.Client
	bucket      string
	mu          sync.Mutex
	running     bool
	stopCh      chan struct{}
	wg          sync.WaitGroup
	stats       *Stats
}

type Stats struct {
	mu               sync.RWMutex
	CompactionsRun   int64
	FilesCompacted   int64
	BytesBefore      int64
	BytesAfter       int64
	LastCompactionAt time.Time
	Errors           int64
}

type FileInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
}

func NewManager(minioClient *minio.Client, bucket string, config *Config) (*Manager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := os.MkdirAll(config.TempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Manager{
		config:      config,
		minioClient: minioClient,
		bucket:      bucket,
		stopCh:      make(chan struct{}),
		stats:       &Stats{},
	}, nil
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.runCompactionLoop(ctx)

	logger.GetLogger().Info("Compaction manager started",
		zap.Duration("interval", m.config.CheckInterval))
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	close(m.stopCh)
	m.wg.Wait()

	logger.GetLogger().Info("Compaction manager stopped")
}

func (m *Manager) runCompactionLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			if err := m.RunCompaction(ctx); err != nil {
				logger.GetLogger().Error("Compaction failed", zap.Error(err))
				m.stats.mu.Lock()
				m.stats.Errors++
				m.stats.mu.Unlock()
			}
		}
	}
}

func (m *Manager) RunCompaction(ctx context.Context) error {
	tables, err := m.listTables(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}

	for _, table := range tables {
		if err := m.compactTable(ctx, table); err != nil {
			logger.GetLogger().Warn("Failed to compact table",
				zap.String("table", table),
				zap.Error(err))
		}
	}

	return nil
}

func (m *Manager) listTables(ctx context.Context) ([]string, error) {
	tableSet := make(map[string]struct{})

	objectCh := m.minioClient.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Recursive: false,
	})

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}

		if len(object.Key) > 0 && object.Key[len(object.Key)-1] == '/' {
			tableName := object.Key[:len(object.Key)-1]
			tableSet[tableName] = struct{}{}
		}
	}

	tables := make([]string, 0, len(tableSet))
	for table := range tableSet {
		tables = append(tables, table)
	}

	return tables, nil
}

func (m *Manager) compactTable(ctx context.Context, table string) error {
	files, err := m.listTableFiles(ctx, table)
	if err != nil {
		return err
	}

	candidates := m.selectCompactionCandidates(files)
	if len(candidates) < m.config.MinFilesToCompact {
		logger.GetLogger().Debug("Not enough files for compaction",
			zap.String("table", table),
			zap.Int("files", len(candidates)))
		return nil
	}

	return m.compactFiles(ctx, table, candidates)
}

func (m *Manager) listTableFiles(ctx context.Context, table string) ([]FileInfo, error) {
	var files []FileInfo
	prefix := table + "/"

	objectCh := m.minioClient.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}

		if filepath.Ext(object.Key) == ".parquet" {
			files = append(files, FileInfo{
				Path:    object.Key,
				Size:    object.Size,
				ModTime: object.LastModified,
			})
		}
	}

	return files, nil
}

func (m *Manager) selectCompactionCandidates(files []FileInfo) []FileInfo {
	now := time.Now()
	var candidates []FileInfo

	for _, f := range files {
		if now.Sub(f.ModTime) < m.config.CooldownPeriod {
			continue
		}

		if f.Size < m.config.TargetFileSize {
			candidates = append(candidates, f)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ModTime.Before(candidates[j].ModTime)
	})

	if len(candidates) > m.config.MaxFilesToCompact {
		candidates = candidates[:m.config.MaxFilesToCompact]
	}

	return candidates
}

func (m *Manager) compactFiles(ctx context.Context, table string, files []FileInfo) error {
	logger.GetLogger().Info("Starting compaction",
		zap.String("table", table),
		zap.Int("files", len(files)))

	tempDir := filepath.Join(m.config.TempDir, fmt.Sprintf("compact_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	var allRecords []map[string]interface{}
	var totalSizeBefore int64
	reader := storage.NewParquetReader(storage.DefaultParquetReaderConfig())

	for _, f := range files {
		localPath := filepath.Join(tempDir, filepath.Base(f.Path))

		if err := m.downloadFile(ctx, f.Path, localPath); err != nil {
			return fmt.Errorf("failed to download %s: %w", f.Path, err)
		}

		records, err := reader.ReadFile(localPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", f.Path, err)
		}

		allRecords = append(allRecords, records...)
		totalSizeBefore += f.Size
	}

	outputDir := filepath.Join(tempDir, "output")
	writerConfig := storage.DefaultParquetWriterConfig(outputDir)
	writerConfig.CompressionType = m.config.CompressionType
	// 使用配置的最大行数，默认 100 万
	maxRows := m.config.MaxRowsPerFile
	if maxRows <= 0 {
		maxRows = 1000000
	}
	writerConfig.MaxRowsPerFile = int(maxRows)

	writer, err := storage.NewParquetWriter(writerConfig)
	if err != nil {
		return err
	}

	if err := writer.Write(allRecords); err != nil {
		writer.Close()
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	outputFiles, err := filepath.Glob(filepath.Join(outputDir, "*.parquet"))
	if err != nil {
		return err
	}

	var totalSizeAfter int64
	for _, outputFile := range outputFiles {
		info, _ := os.Stat(outputFile)
		totalSizeAfter += info.Size()

		objectName := fmt.Sprintf("%s/compacted_%d_%s", table, time.Now().UnixNano(), filepath.Base(outputFile))
		if err := m.uploadFile(ctx, outputFile, objectName); err != nil {
			return fmt.Errorf("failed to upload %s: %w", objectName, err)
		}
	}

	for _, f := range files {
		if err := m.minioClient.RemoveObject(ctx, m.bucket, f.Path, minio.RemoveObjectOptions{}); err != nil {
			logger.GetLogger().Warn("Failed to remove old file",
				zap.String("file", f.Path),
				zap.Error(err))
		}
	}

	m.stats.mu.Lock()
	m.stats.CompactionsRun++
	m.stats.FilesCompacted += int64(len(files))
	m.stats.BytesBefore += totalSizeBefore
	m.stats.BytesAfter += totalSizeAfter
	m.stats.LastCompactionAt = time.Now()
	m.stats.mu.Unlock()

	logger.GetLogger().Info("Compaction completed",
		zap.String("table", table),
		zap.Int("inputFiles", len(files)),
		zap.Int("outputFiles", len(outputFiles)),
		zap.Int64("sizeBefore", totalSizeBefore),
		zap.Int64("sizeAfter", totalSizeAfter),
		zap.Float64("ratio", float64(totalSizeAfter)/float64(totalSizeBefore)))

	return nil
}

func (m *Manager) downloadFile(ctx context.Context, objectName, localPath string) error {
	return m.minioClient.FGetObject(ctx, m.bucket, objectName, localPath, minio.GetObjectOptions{})
}

func (m *Manager) uploadFile(ctx context.Context, localPath, objectName string) error {
	_, err := m.minioClient.FPutObject(ctx, m.bucket, objectName, localPath, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

func (m *Manager) GetStats() Stats {
	m.stats.mu.RLock()
	defer m.stats.mu.RUnlock()

	return Stats{
		CompactionsRun:   m.stats.CompactionsRun,
		FilesCompacted:   m.stats.FilesCompacted,
		BytesBefore:      m.stats.BytesBefore,
		BytesAfter:       m.stats.BytesAfter,
		LastCompactionAt: m.stats.LastCompactionAt,
		Errors:           m.stats.Errors,
	}
}

func (m *Manager) ForceCompact(ctx context.Context, table string) error {
	return m.compactTable(ctx, table)
}
