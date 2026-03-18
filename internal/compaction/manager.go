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

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// CompactionLevel 分层合并的单层配置
type CompactionLevel struct {
	Name            string        `yaml:"name"`               // 层级名称（如 L0, L1, L2, L3）
	MaxFileSize     int64         `yaml:"max_file_size"`      // 该层最大文件大小（bytes）
	TargetFileSize  int64         `yaml:"target_file_size"`   // 合并后目标大小（bytes）
	MinFilesToMerge int           `yaml:"min_files_to_merge"` // 最少合并文件数
	CooldownPeriod  time.Duration `yaml:"cooldown_period"`    // 文件冷却时间
}

// TieredCompactionConfig 分层合并配置
type TieredCompactionConfig struct {
	Enabled         bool              `yaml:"enabled"`            // 是否启用分层合并
	Levels          []CompactionLevel `yaml:"levels"`             // 分层配置
	MaxFilesToMerge int               `yaml:"max_files_to_merge"` // 单次合并的最大文件数
	CheckInterval   time.Duration     `yaml:"check_interval"`     // 检查间隔
	TempDir         string            `yaml:"temp_dir"`           // 临时目录
	CompressionType string            `yaml:"compression_type"`   // 压缩类型
	MaxRowsPerFile  int64             `yaml:"max_rows_per_file"`  // 每个文件最大行数
}

// DefaultTieredCompactionConfig 返回默认的分层合并配置
// 默认配置参考 LSM-Tree 思想，按文件大小分为 4 层：
// L0: < 16MB - 最小文件层，频繁合并，至少 5 个文件触发
// L1: 16MB ~ 64MB - 小文件层，至少 3 个文件触发
// L2: 64MB ~ 256MB - 中等文件层，至少 2 个文件触发
// L3: 256MB ~ 1GB - 大文件层，较少合并
func DefaultTieredCompactionConfig() *TieredCompactionConfig {
	return &TieredCompactionConfig{
		Enabled: true,
		Levels: []CompactionLevel{
			{
				Name:            "L0",
				MaxFileSize:     16 * 1024 * 1024, // 16MB
				TargetFileSize:  16 * 1024 * 1024, // 16MB
				MinFilesToMerge: 5,                // L0 至少 5 个文件才触发合并
				CooldownPeriod:  2 * time.Minute,
			},
			{
				Name:            "L1",
				MaxFileSize:     64 * 1024 * 1024, // 64MB
				TargetFileSize:  64 * 1024 * 1024, // 64MB
				MinFilesToMerge: 3,                // L1 至少 3 个文件触发
				CooldownPeriod:  5 * time.Minute,
			},
			{
				Name:            "L2",
				MaxFileSize:     256 * 1024 * 1024, // 256MB
				TargetFileSize:  256 * 1024 * 1024, // 256MB
				MinFilesToMerge: 2,                 // L2 至少 2 个文件触发
				CooldownPeriod:  10 * time.Minute,
			},
			{
				Name:            "L3",
				MaxFileSize:     1024 * 1024 * 1024, // 1GB
				TargetFileSize:  512 * 1024 * 1024,  // 512MB
				MinFilesToMerge: 2,
				CooldownPeriod:  30 * time.Minute,
			},
		},
		MaxFilesToMerge: 20,
		CheckInterval:   10 * time.Minute,
		TempDir:         filepath.Join(os.TempDir(), "miniodb_compaction"),
		CompressionType: "snappy",
		MaxRowsPerFile:  1000000, // 100 万行
	}
}

type Config struct {
	TargetFileSize    int64
	MinFilesToCompact int
	MaxFilesToCompact int
	CooldownPeriod    time.Duration
	CheckInterval     time.Duration
	TempDir           string
	CompressionType   string
	MaxRowsPerFile    int64 // 每个文件最大行数
	// 分层合并配置
	TieredConfig *TieredCompactionConfig
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
		TieredConfig:      DefaultTieredCompactionConfig(),
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
	logger      *zap.Logger
}

type Stats struct {
	mu               sync.RWMutex
	CompactionsRun   int64
	FilesCompacted   int64
	BytesBefore      int64
	BytesAfter       int64
	LastCompactionAt time.Time
	Errors           int64
	// 分层统计
	TieredCompactions  map[string]int64 // 每层合并次数
	WriteAmplification float64          // 写放大比例（总写入/有效数据）
}

// TieredStats 分层合并统计
type TieredStats struct {
	Level          string
	CompactionsRun int64
	FilesCompacted int64
	BytesCompacted int64
	AvgMergeRatio  float64
}

type FileInfo struct {
	Path    string
	Size    int64
	ModTime time.Time
}

func NewManager(minioClient *minio.Client, bucket string, config *Config, logger *zap.Logger) (*Manager, error) {
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
		logger:      logger,
	}, nil
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	// 重建 stopCh 以支持多次 Start/Stop
	m.stopCh = make(chan struct{})
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.runCompactionLoop(ctx)

	m.logger.Info("Compaction manager started",
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

	m.logger.Info("Compaction manager stopped")
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
				m.logger.Error("Compaction failed", zap.Error(err))
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
			m.logger.Warn("Failed to compact table",
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
	// 如果启用了分层合并，使用分层策略
	if m.config.TieredConfig != nil && m.config.TieredConfig.Enabled {
		return m.compactTableTiered(ctx, table)
	}

	// 原有的简单合并逻辑
	files, err := m.listTableFiles(ctx, table)
	if err != nil {
		return err
	}

	candidates := m.selectCompactionCandidates(files)
	if len(candidates) < m.config.MinFilesToCompact {
		m.logger.Debug("Not enough files for compaction",
			zap.String("table", table),
			zap.Int("files", len(candidates)))
		return nil
	}

	return m.compactFiles(ctx, table, candidates)
}

// compactTableTiered 使用分层合并策略压缩表
// 核心思想：
// 1. 按文件大小分配到不同层级
// 2. 每层独立合并，避免频繁合并大文件
// 3. 从低层向高层合并，减少写放大
func (m *Manager) compactTableTiered(ctx context.Context, table string) error {
	files, err := m.listTableFiles(ctx, table)
	if err != nil {
		return err
	}

	// 按层级分类文件
	tieredFiles := m.classifyFilesByTier(files)

	// 从低层到高层依次尝试合并
	// L0 -> L1 -> L2 -> L3
	for i, level := range m.config.TieredConfig.Levels {
		levelFiles := tieredFiles[level.Name]
		if len(levelFiles) < level.MinFilesToMerge {
			m.logger.Debug("Not enough files for tiered compaction",
				zap.String("table", table),
				zap.String("level", level.Name),
				zap.Int("files", len(levelFiles)),
				zap.Int("min_required", level.MinFilesToMerge))
			continue
		}

		// 选择该层的合并候选文件
		candidates := m.selectTieredCompactionCandidates(levelFiles, level, i)
		if len(candidates) < level.MinFilesToMerge {
			continue
		}

		m.logger.Info("Starting tiered compaction",
			zap.String("table", table),
			zap.String("level", level.Name),
			zap.Int("files", len(candidates)))

		if err := m.compactTieredFiles(ctx, table, candidates, level, i); err != nil {
			m.logger.Warn("Tiered compaction failed",
				zap.String("table", table),
				zap.String("level", level.Name),
				zap.Error(err))
			// 继续尝试下一层
			continue
		}

		// 更新分层统计
		m.stats.mu.Lock()
		if m.stats.TieredCompactions == nil {
			m.stats.TieredCompactions = make(map[string]int64)
		}
		m.stats.TieredCompactions[level.Name]++
		m.stats.mu.Unlock()
	}

	return nil
}

// classifyFilesByTier 将文件按大小分类到不同层级
func (m *Manager) classifyFilesByTier(files []FileInfo) map[string][]FileInfo {
	result := make(map[string][]FileInfo)
	for _, level := range m.config.TieredConfig.Levels {
		result[level.Name] = []FileInfo{}
	}

	for _, f := range files {
		level := m.getFileLevel(f.Size)
		if level != "" {
			result[level] = append(result[level], f)
		}
	}

	return result
}

// getFileLevel 根据文件大小获取所属层级
func (m *Manager) getFileLevel(size int64) string {
	for _, level := range m.config.TieredConfig.Levels {
		if size <= level.MaxFileSize {
			return level.Name
		}
	}
	// 超过最大层级的文件归入最后一层
	if len(m.config.TieredConfig.Levels) > 0 {
		return m.config.TieredConfig.Levels[len(m.config.TieredConfig.Levels)-1].Name
	}
	return ""
}

// selectTieredCompactionCandidates 选择分层合并的候选文件
// 优先选择：1. 冷却时间已过的文件 2. 较小的文件 3. 较早的文件
func (m *Manager) selectTieredCompactionCandidates(files []FileInfo, level CompactionLevel, levelIndex int) []FileInfo {
	now := time.Now()
	var candidates []FileInfo

	for _, f := range files {
		// 检查冷却时间
		if now.Sub(f.ModTime) < level.CooldownPeriod {
			continue
		}
		// 确保文件在该层范围内
		if f.Size > level.MaxFileSize {
			continue
		}
		candidates = append(candidates, f)
	}

	// 按文件大小排序（小文件优先，减少合并开销）
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Size != candidates[j].Size {
			return candidates[i].Size < candidates[j].Size
		}
		return candidates[i].ModTime.Before(candidates[j].ModTime)
	})

	// 限制最大合并文件数
	maxFiles := m.config.TieredConfig.MaxFilesToMerge
	if len(candidates) > maxFiles {
		candidates = candidates[:maxFiles]
	}

	return candidates
}

// compactTieredFiles 执行分层合并
func (m *Manager) compactTieredFiles(ctx context.Context, table string, files []FileInfo, level CompactionLevel, levelIndex int) error {
	tempDir := filepath.Join(m.config.TieredConfig.TempDir, fmt.Sprintf("tiered_%s_%d", level.Name, time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	var allRecords []map[string]interface{}
	var totalSizeBefore int64
	reader := storage.NewParquetReader(storage.DefaultParquetReaderConfig(), m.logger)

	// 下载并读取所有文件
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

	// 计算目标文件大小和数量
	targetSize := level.TargetFileSize
	if targetSize <= 0 {
		targetSize = m.config.TieredConfig.Levels[levelIndex].TargetFileSize
	}

	outputDir := filepath.Join(tempDir, "output")
	writerConfig := storage.DefaultParquetWriterConfig(outputDir)
	writerConfig.CompressionType = m.config.TieredConfig.CompressionType
	maxRows := m.config.TieredConfig.MaxRowsPerFile
	if maxRows <= 0 {
		maxRows = 1000000
	}
	writerConfig.MaxRowsPerFile = int(maxRows)

	writer, err := storage.NewParquetWriter(writerConfig, m.logger)
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

	// 上传合并后的文件，标记为升级到下一层
	nextLevel := ""
	if levelIndex < len(m.config.TieredConfig.Levels)-1 {
		nextLevel = m.config.TieredConfig.Levels[levelIndex+1].Name
	}

	var totalSizeAfter int64
	for _, outputFile := range outputFiles {
		info, err := os.Stat(outputFile)
		if err != nil {
			m.logger.Warn("Failed to stat compacted file", zap.String("file", outputFile), zap.Error(err))
			continue
		}
		totalSizeAfter += info.Size()

		// 文件命名：表名/层级_时间戳_文件名
		var objectName string
		if nextLevel != "" {
			objectName = fmt.Sprintf("%s/%s_promoted_%d_%s", table, nextLevel, time.Now().UnixNano(), filepath.Base(outputFile))
		} else {
			objectName = fmt.Sprintf("%s/compacted_%d_%s", table, time.Now().UnixNano(), filepath.Base(outputFile))
		}

		if err := m.uploadFile(ctx, outputFile, objectName); err != nil {
			return fmt.Errorf("failed to upload %s: %w", objectName, err)
		}
	}

	// 删除旧文件
	for _, f := range files {
		if err := m.minioClient.RemoveObject(ctx, m.bucket, f.Path, minio.RemoveObjectOptions{}); err != nil {
			m.logger.Warn("Failed to remove old file",
				zap.String("file", f.Path),
				zap.Error(err))
		}
	}

	// 更新统计
	m.stats.mu.Lock()
	m.stats.CompactionsRun++
	m.stats.FilesCompacted += int64(len(files))
	m.stats.BytesBefore += totalSizeBefore
	m.stats.BytesAfter += totalSizeAfter
	m.stats.LastCompactionAt = time.Now()
	// 计算写放大：合并后的数据量 / 原始数据量
	if totalSizeBefore > 0 {
		writeAmp := float64(totalSizeBefore+totalSizeAfter) / float64(totalSizeBefore)
		// 使用指数移动平均更新写放大
		if m.stats.WriteAmplification == 0 {
			m.stats.WriteAmplification = writeAmp
		} else {
			m.stats.WriteAmplification = 0.9*m.stats.WriteAmplification + 0.1*writeAmp
		}
	}
	m.stats.mu.Unlock()

	m.logger.Info("Tiered compaction completed",
		zap.String("table", table),
		zap.String("level", level.Name),
		zap.String("promoted_to", nextLevel),
		zap.Int("inputFiles", len(files)),
		zap.Int("outputFiles", len(outputFiles)),
		zap.Int64("sizeBefore", totalSizeBefore),
		zap.Int64("sizeAfter", totalSizeAfter),
		zap.Float64("ratio", func() float64 {
			if totalSizeBefore > 0 {
				return float64(totalSizeAfter) / float64(totalSizeBefore)
			}
			return 1.0
		}()))

	return nil
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
	m.logger.Info("Starting compaction",
		zap.String("table", table),
		zap.Int("files", len(files)))

	tempDir := filepath.Join(m.config.TempDir, fmt.Sprintf("compact_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	var allRecords []map[string]interface{}
	var totalSizeBefore int64
	reader := storage.NewParquetReader(storage.DefaultParquetReaderConfig(), m.logger)

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

	writer, err := storage.NewParquetWriter(writerConfig, m.logger)
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
		info, err := os.Stat(outputFile)
		if err != nil {
			m.logger.Warn("Failed to stat compacted file", zap.String("file", outputFile), zap.Error(err))
			continue
		}
		totalSizeAfter += info.Size()

		objectName := fmt.Sprintf("%s/compacted_%d_%s", table, time.Now().UnixNano(), filepath.Base(outputFile))
		if err := m.uploadFile(ctx, outputFile, objectName); err != nil {
			return fmt.Errorf("failed to upload %s: %w", objectName, err)
		}
	}

	for _, f := range files {
		if err := m.minioClient.RemoveObject(ctx, m.bucket, f.Path, minio.RemoveObjectOptions{}); err != nil {
			m.logger.Warn("Failed to remove old file",
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

	m.logger.Info("Compaction completed",
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

	// 复制分层统计
	tieredCompactions := make(map[string]int64)
	for k, v := range m.stats.TieredCompactions {
		tieredCompactions[k] = v
	}

	return Stats{
		CompactionsRun:     m.stats.CompactionsRun,
		FilesCompacted:     m.stats.FilesCompacted,
		BytesBefore:        m.stats.BytesBefore,
		BytesAfter:         m.stats.BytesAfter,
		LastCompactionAt:   m.stats.LastCompactionAt,
		Errors:             m.stats.Errors,
		TieredCompactions:  tieredCompactions,
		WriteAmplification: m.stats.WriteAmplification,
	}
}

// GetTieredStats 获取分层合并统计信息
func (m *Manager) GetTieredStats() []TieredStats {
	m.stats.mu.RLock()
	defer m.stats.mu.RUnlock()

	var result []TieredStats
	if m.config.TieredConfig == nil {
		return result
	}

	for _, level := range m.config.TieredConfig.Levels {
		ts := TieredStats{
			Level: level.Name,
		}
		if m.stats.TieredCompactions != nil {
			ts.CompactionsRun = m.stats.TieredCompactions[level.Name]
		}
		result = append(result, ts)
	}

	return result
}

// GetWriteAmplification 获取当前写放大比例
// 返回值说明：1.0 表示无放大，2.0 表示写放大 2 倍
func (m *Manager) GetWriteAmplification() float64 {
	m.stats.mu.RLock()
	defer m.stats.mu.RUnlock()
	return m.stats.WriteAmplification
}

func (m *Manager) ForceCompact(ctx context.Context, table string) error {
	return m.compactTable(ctx, table)
}
