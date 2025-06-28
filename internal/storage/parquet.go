package storage

import (
	"log"
	"sync"
	"time"

	"github.com/xitongsys/parquet-go/parquet"
)

// Parquetet存储优化器
type Parquet struct {
	compressionStrategies map[string]CompressionStrategy
	partitionStrategies   map[string]PartitionStrategy
	metadataIndex        *ParquetMetadataIndex
	stats                *ParquetStats
	mutex                sync.RWMutex
}

// CompressionStrategy 压缩策略
type CompressionStrategy struct {
	Name        string  `json:"name"`
	Codec       parquet.CompressionCodec `json:"codec"`
	Description string  `json:"description"`
	Ratio       float64 `json:"compression_ratio"`  // 压缩比
	Speed       float64 `json:"compression_speed"`  // 压缩速度 (MB/s)
	CPUCost     float64 `json:"cpu_cost"`          // CPU成本 (1-10)
	UseCase     string  `json:"use_case"`          // 适用场景
}

// PartitionStrategy 分区策略
type PartitionStrategy struct {
	Name            string                 `json:"name"`
	RowGroupSize    int64                  `json:"row_group_size"`
	PageSize        int64                  `json:"page_size"`
	DictPageSize    int64                  `json:"dict_page_size"`
	EnableDict      bool                   `json:"enable_dictionary"`
	EnableStats     bool                   `json:"enable_statistics"`
	Predicates      []string               `json:"predicates"`        // 谓词下推支持
	SortColumns     []string               `json:"sort_columns"`      // 排序列
	BloomColumns    []string               `json:"bloom_columns"`     // BloomFilter列
	MinMaxColumns   []string               `json:"minmax_columns"`    // MinMax索引列
	OptimizationHint string                `json:"optimization_hint"` // 优化提示
}

// ParquetMetadataIndex Parquet元数据索引
type ParquetMetadataIndex struct {
	FilesIndex      map[string]*FileMetadata    `json:"files_index"`
	ColumnsIndex    map[string]*ColumnMetadata  `json:"columns_index"`
	SchemaIndex     map[string]*SchemaMetadata  `json:"schema_index"`
	CompressionData map[string]*CompressionData `json:"compression_data"`
	mutex           sync.RWMutex
}

// FileMetadata 文件元数据
type FileMetadata struct {
	FilePath        string            `json:"file_path"`
	FileSize        int64             `json:"file_size"`
	RowCount        int64             `json:"row_count"`
	RowGroupCount   int               `json:"row_group_count"`
	CompressionType string            `json:"compression_type"`
	CreatedAt       time.Time         `json:"created_at"`
	Schema          string            `json:"schema"`
	MinValues       map[string]interface{} `json:"min_values"`
	MaxValues       map[string]interface{} `json:"max_values"`
	NullCounts      map[string]int64  `json:"null_counts"`
	BloomFilters    map[string]bool   `json:"bloom_filters"`
	SortOrder       []string          `json:"sort_order"`
	Partitions      map[string]string `json:"partitions"`
}

// ColumnMetadata 列元数据
type ColumnMetadata struct {
	ColumnName      string      `json:"column_name"`
	DataType        string      `json:"data_type"`
	Encoding        string      `json:"encoding"`
	CompressionType string      `json:"compression_type"`
	DistinctCount   int64       `json:"distinct_count"`
	MinValue        interface{} `json:"min_value"`
	MaxValue        interface{} `json:"max_value"`
	NullCount       int64       `json:"null_count"`
	TotalSize       int64       `json:"total_size"`
	UncompressedSize int64      `json:"uncompressed_size"`
	HasBloomFilter  bool        `json:"has_bloom_filter"`
	HasDictionary   bool        `json:"has_dictionary"`
	Cardinality     float64     `json:"cardinality"`     // 基数
	Selectivity     float64     `json:"selectivity"`     // 选择性
	IndexType       string      `json:"index_type"`      // 索引类型
}

// SchemaMetadata 模式元数据
type SchemaMetadata struct {
	SchemaName    string             `json:"schema_name"`
	Version       string             `json:"version"`
	ColumnCount   int                `json:"column_count"`
	PrimaryKeys   []string           `json:"primary_keys"`
	PartitionKeys []string           `json:"partition_keys"`
	SortKeys      []string           `json:"sort_keys"`
	Columns       []*ColumnMetadata  `json:"columns"`
	Indexes       map[string]IndexMetadata `json:"indexes"`
	Constraints   []string           `json:"constraints"`
	Properties    map[string]string  `json:"properties"`
}

// CompressionData 压缩数据统计
type CompressionData struct {
	Algorithm        string    `json:"algorithm"`
	OriginalSize     int64     `json:"original_size"`
	CompressedSize   int64     `json:"compressed_size"`
	CompressionRatio float64   `json:"compression_ratio"`
	CompressionTime  time.Duration `json:"compression_time"`
	DecompressionTime time.Duration `json:"decompression_time"`
	CPUUsage         float64   `json:"cpu_usage"`
	MemoryUsage      int64     `json:"memory_usage"`
	Timestamp        time.Time `json:"timestamp"`
}

// IndexMetadata 索引元数据
type IndexMetadata struct {
	IndexName   string   `json:"index_name"`
	IndexType   string   `json:"index_type"`  // bloom, minmax, bitmap, inverted
	Columns     []string `json:"columns"`
	Size        int64    `json:"size"`
	Selectivity float64  `json:"selectivity"`
	HitRate     float64  `json:"hit_rate"`
	CreateTime  time.Time `json:"create_time"`
	UpdateTime  time.Time `json:"update_time"`
}

// ParquetStats Parquet统计信息
type ParquetStats struct {
	TotalFiles         int64   `json:"total_files"`
	TotalSize          int64   `json:"total_size"`
	TotalRows          int64   `json:"total_rows"`
	AvgCompressionRatio float64 `json:"avg_compression_ratio"`
	AvgQueryTime       time.Duration `json:"avg_query_time"`
	OptimalStrategy    string  `json:"optimal_strategy"`
	HotColumns         []string `json:"hot_columns"`
	ColdColumns        []string `json:"cold_columns"`
	FragmentationRatio float64 `json:"fragmentation_ratio"`
	LastOptimized      time.Time `json:"last_optimized"`
	mutex              sync.RWMutex
}

// NewParquet 创建Parquet优化器
func NewParquet() *Parquet {
	optimizer := &Parquet{
		compressionStrategies: make(map[string]CompressionStrategy),
		partitionStrategies:   make(map[string]PartitionStrategy),
		metadataIndex:        &ParquetMetadataIndex{
			FilesIndex:      make(map[string]*FileMetadata),
			ColumnsIndex:    make(map[string]*ColumnMetadata),
			SchemaIndex:     make(map[string]*SchemaMetadata),
			CompressionData: make(map[string]*CompressionData),
		},
		stats: &ParquetStats{
			HotColumns:  make([]string, 0),
			ColdColumns: make([]string, 0),
		},
	}

	// 初始化压缩策略
	optimizer.initCompressionStrategies()
	
	// 初始化分区策略
	optimizer.initPartitionStrategies()

	return optimizer
}

// initCompressionStrategies 初始化压缩策略
func (po *Parquet) initCompressionStrategies() {
	strategies := []CompressionStrategy{
		{
			Name:        "uncompressed",
			Codec:       parquet.CompressionCodec_UNCOMPRESSED,
			Description: "无压缩 - 最快读写，最大存储空间",
			Ratio:       1.0,
			Speed:       1000.0,
			CPUCost:     1.0,
			UseCase:     "临时数据、快速原型",
		},
		{
			Name:        "snappy",
			Codec:       parquet.CompressionCodec_SNAPPY,
			Description: "Snappy压缩 - 平衡的压缩比和速度",
			Ratio:       3.5,
			Speed:       400.0,
			CPUCost:     3.0,
			UseCase:     "通用场景、实时数据处理",
		},
		{
			Name:        "gzip",
			Codec:       parquet.CompressionCodec_GZIP,
			Description: "GZIP压缩 - 高压缩比，中等CPU成本",
			Ratio:       6.0,
			Speed:       150.0,
			CPUCost:     5.0,
			UseCase:     "存储优化、批处理场景",
		},
		{
			Name:        "lzo",
			Codec:       parquet.CompressionCodec_LZO,
			Description: "LZO压缩 - 快速压缩解压缩",
			Ratio:       3.8,
			Speed:       350.0,
			CPUCost:     3.5,
			UseCase:     "中等压缩需求、高并发读取",
		},
		{
			Name:        "brotli",
			Codec:       parquet.CompressionCodec_BROTLI,
			Description: "Brotli压缩 - 最高压缩比，高CPU成本",
			Ratio:       8.5,
			Speed:       80.0,
			CPUCost:     8.0,
			UseCase:     "长期存储、网络传输优化",
		},
		{
			Name:        "lz4",
			Codec:       parquet.CompressionCodec_LZ4,
			Description: "LZ4压缩 - 极快速度，中等压缩比",
			Ratio:       3.2,
			Speed:       500.0,
			CPUCost:     2.5,
			UseCase:     "高频读写、低延迟要求",
		},
		{
			Name:        "zstd",
			Codec:       parquet.CompressionCodec_ZSTD,
			Description: "ZSTD压缩 - 优秀的压缩比和速度平衡",
			Ratio:       7.2,
			Speed:       200.0,
			CPUCost:     6.0,
			UseCase:     "现代存储、AI/ML工作负载",
		},
	}

	for _, strategy := range strategies {
		po.compressionStrategies[strategy.Name] = strategy
	}
}

// initPartitionStrategies 初始化分区策略
func (po *Parquet) initPartitionStrategies() {
	strategies := []PartitionStrategy{
		{
			Name:            "small_files",
			RowGroupSize:    32 * 1024 * 1024,   // 32MB
			PageSize:        64 * 1024,          // 64KB
			DictPageSize:    1024 * 1024,        // 1MB
			EnableDict:      true,
			EnableStats:     true,
			OptimizationHint: "适用于小文件，快速查询",
		},
		{
			Name:            "large_files",
			RowGroupSize:    256 * 1024 * 1024,  // 256MB
			PageSize:        1024 * 1024,        // 1MB
			DictPageSize:    4 * 1024 * 1024,    // 4MB
			EnableDict:      true,
			EnableStats:     true,
			OptimizationHint: "适用于大文件，批处理查询",
		},
		{
			Name:            "analytical",
			RowGroupSize:    128 * 1024 * 1024,  // 128MB
			PageSize:        256 * 1024,         // 256KB
			DictPageSize:    2 * 1024 * 1024,    // 2MB
			EnableDict:      true,
			EnableStats:     true,
			BloomColumns:    []string{"id", "user_id", "status"},
			MinMaxColumns:   []string{"timestamp", "amount", "price"},
			SortColumns:     []string{"timestamp", "user_id"},
			OptimizationHint: "适用于分析查询，OLAP工作负载",
		},
		{
			Name:            "streaming",
			RowGroupSize:    16 * 1024 * 1024,   // 16MB
			PageSize:        32 * 1024,          // 32KB
			DictPageSize:    512 * 1024,         // 512KB
			EnableDict:      false,
			EnableStats:     true,
			OptimizationHint: "适用于流式数据，实时摄取",
		},
		{
			Name:            "compression_optimized",
			RowGroupSize:    64 * 1024 * 1024,   // 64MB
			PageSize:        128 * 1024,         // 128KB
			DictPageSize:    1024 * 1024,        // 1MB
			EnableDict:      true,
			EnableStats:     true,
			OptimizationHint: "优化压缩比，适用于长期存储",
		},
	}

	for _, strategy := range strategies {
		po.partitionStrategies[strategy.Name] = strategy
	}
}

// OptimizeWriteStrategy 优化写入策略
func (po *Parquet) OptimizeWriteStrategy(dataSize int64, dataType string, useCase string) (*PartitionStrategy, *CompressionStrategy) {
	po.mutex.RLock()
	defer po.mutex.RUnlock()

	// 根据数据大小选择分区策略
	var partitionStrategy *PartitionStrategy
	if dataSize < 10*1024*1024 { // 小于10MB
		strategy := po.partitionStrategies["small_files"]
		partitionStrategy = &strategy
	} else if dataSize > 100*1024*1024 { // 大于100MB
		strategy := po.partitionStrategies["large_files"]
		partitionStrategy = &strategy
	} else {
		strategy := po.partitionStrategies["analytical"]
		partitionStrategy = &strategy
	}

	// 根据用例选择压缩策略
	var compressionStrategy *CompressionStrategy
	switch useCase {
	case "realtime", "streaming":
		strategy := po.compressionStrategies["lz4"]
		compressionStrategy = &strategy
	case "storage", "archival":
		strategy := po.compressionStrategies["zstd"]
		compressionStrategy = &strategy
	case "analytical", "olap":
		strategy := po.compressionStrategies["snappy"]
		compressionStrategy = &strategy
	case "network", "transfer":
		strategy := po.compressionStrategies["brotli"]
		compressionStrategy = &strategy
	default:
		strategy := po.compressionStrategies["snappy"]
		compressionStrategy = &strategy
	}

	log.Printf("Selected strategies - Partition: %s, Compression: %s for use case: %s, size: %d bytes",
		partitionStrategy.Name, compressionStrategy.Name, useCase, dataSize)

	return partitionStrategy, compressionStrategy
}

// CreateOptimizedWriter 创建优化的Parquet写入器
// MockParquetWriter 模拟Parquet写入器
type MockParquetWriter struct {
	FilePath         string
	PartitionStrategy *PartitionStrategy
	CompressionStrategy *CompressionStrategy
}

func (po *Parquet) CreateOptimizedWriter(filePath string, schema interface{}, partitionStrategy *PartitionStrategy, compressionStrategy *CompressionStrategy) (*MockParquetWriter, error) {
	log.Printf("Creating optimized Parquet writer for file: %s", filePath)
	log.Printf("Using compression: %s, row group size: %d", compressionStrategy.Name, partitionStrategy.RowGroupSize)

	// 记录元数据
	po.recordFileMetadata(filePath, partitionStrategy, compressionStrategy)

	// 返回模拟的写入器（实际应用中应返回真实的ParquetWriter）
	return &MockParquetWriter{
		FilePath:         filePath,
		PartitionStrategy: partitionStrategy,
		CompressionStrategy: compressionStrategy,
	}, nil
}

// recordFileMetadata 记录文件元数据
func (po *Parquet) recordFileMetadata(filePath string, partitionStrategy *PartitionStrategy, compressionStrategy *CompressionStrategy) {
	po.metadataIndex.mutex.Lock()
	defer po.metadataIndex.mutex.Unlock()

	metadata := &FileMetadata{
		FilePath:        filePath,
		CompressionType: compressionStrategy.Name,
		CreatedAt:       time.Now(),
		MinValues:       make(map[string]interface{}),
		MaxValues:       make(map[string]interface{}),
		NullCounts:      make(map[string]int64),
		BloomFilters:    make(map[string]bool),
		SortOrder:       partitionStrategy.SortColumns,
		Partitions:      make(map[string]string),
	}

	po.metadataIndex.FilesIndex[filePath] = metadata
}

// AnalyzeCompressionPerformance 分析压缩性能
func (po *Parquet) AnalyzeCompressionPerformance(sampleData []byte) map[string]*CompressionData {
	results := make(map[string]*CompressionData)

	for name, strategy := range po.compressionStrategies {
		if name == "uncompressed" {
			continue // 跳过无压缩策略
		}

		_ = time.Now() // startTime for timing measurement
		
		// 模拟压缩性能（实际实现需要调用压缩库）
		compressionRatio := strategy.Ratio + (float64(len(sampleData))/1024.0)*0.001 // 简化计算
		compressionTime := time.Duration(float64(len(sampleData))/strategy.Speed) * time.Microsecond
		
		endTime := time.Now()

		results[name] = &CompressionData{
			Algorithm:         name,
			OriginalSize:      int64(len(sampleData)),
			CompressedSize:    int64(float64(len(sampleData)) / compressionRatio),
			CompressionRatio:  compressionRatio,
			CompressionTime:   compressionTime,
			DecompressionTime: compressionTime / 2, // 解压通常更快
			CPUUsage:         strategy.CPUCost,
			MemoryUsage:      int64(len(sampleData)) / 4, // 估算内存使用
			Timestamp:        endTime,
		}
	}

	// 更新全局压缩数据统计
	po.metadataIndex.mutex.Lock()
	for name, data := range results {
		po.metadataIndex.CompressionData[name] = data
	}
	po.metadataIndex.mutex.Unlock()

	return results
}

// GetOptimalCompressionStrategy 获取最优压缩策略
func (po *Parquet) GetOptimalCompressionStrategy(workloadType string, priorityFactor float64) *CompressionStrategy {
	po.mutex.RLock()
	defer po.mutex.RUnlock()

	var bestStrategy *CompressionStrategy
	var bestScore float64

	for _, strategy := range po.compressionStrategies {
		score := po.calculateCompressionScore(strategy, workloadType, priorityFactor)
		if bestStrategy == nil || score > bestScore {
			bestScore = score
			strategyValue := strategy
			bestStrategy = &strategyValue
		}
	}

	log.Printf("Selected optimal compression strategy: %s (score: %.2f) for workload: %s",
		bestStrategy.Name, bestScore, workloadType)

	return bestStrategy
}

// calculateCompressionScore 计算压缩策略得分
func (po *Parquet) calculateCompressionScore(strategy CompressionStrategy, workloadType string, priorityFactor float64) float64 {
	var score float64

	switch workloadType {
	case "realtime":
		// 实时场景：优先考虑速度，其次是CPU成本
		score = strategy.Speed*0.6 - strategy.CPUCost*0.3 + strategy.Ratio*0.1
	case "storage":
		// 存储场景：优先考虑压缩比，其次是CPU成本
		score = strategy.Ratio*0.7 + strategy.Speed*0.1 - strategy.CPUCost*0.2
	case "analytical":
		// 分析场景：平衡压缩比和速度
		score = strategy.Ratio*0.4 + strategy.Speed*0.4 - strategy.CPUCost*0.2
	case "network":
		// 网络传输：优先考虑压缩比
		score = strategy.Ratio*0.8 + strategy.Speed*0.1 - strategy.CPUCost*0.1
	default:
		// 默认：平衡所有因素
		score = strategy.Ratio*0.3 + strategy.Speed*0.4 - strategy.CPUCost*0.3
	}

	// 应用优先级因子
	score *= priorityFactor

	return score
}

// GetMetadataIndex 获取元数据索引
func (po *Parquet) GetMetadataIndex() *ParquetMetadataIndex {
	return po.metadataIndex
}

// GetStats 获取统计信息
func (po *Parquet) GetStats() *ParquetStats {
	po.stats.mutex.RLock()
	defer po.stats.mutex.RUnlock()
	
	// 创建副本以避免并发访问问题
	statsCopy := *po.stats
	statsCopy.HotColumns = make([]string, len(po.stats.HotColumns))
	copy(statsCopy.HotColumns, po.stats.HotColumns)
	statsCopy.ColdColumns = make([]string, len(po.stats.ColdColumns))
	copy(statsCopy.ColdColumns, po.stats.ColdColumns)
	
	return &statsCopy
}

// UpdateStats 更新统计信息
func (po *Parquettats(fileSize int64, rowCount int64, compressionRatio float64, queryTime time.Duration) {
	po.stats.mutex.Lock()
	defer po.stats.mutex.Unlock()

	po.stats.TotalFiles++
	po.stats.TotalSize += fileSize
	po.stats.TotalRows += rowCount
	
	// 更新平均压缩比
	if po.stats.TotalFiles == 1 {
		po.stats.AvgCompressionRatio = compressionRatio
	} else {
		po.stats.AvgCompressionRatio = (po.stats.AvgCompressionRatio*float64(po.stats.TotalFiles-1) + compressionRatio) / float64(po.stats.TotalFiles)
	}
	
	// 更新平均查询时间
	if po.stats.TotalFiles == 1 {
		po.stats.AvgQueryTime = queryTime
	} else {
		po.stats.AvgQueryTime = (po.stats.AvgQueryTime*time.Duration(po.stats.TotalFiles-1) + queryTime) / time.Duration(po.stats.TotalFiles)
	}
	
	po.stats.LastOptimized = time.Now()
}

// GetCompressionStrategies 获取所有压缩策略
func (po *Parquet) GetCompressionStrategies() map[string]CompressionStrategy {
	po.mutex.RLock()
	defer po.mutex.RUnlock()
	
	strategies := make(map[string]CompressionStrategy)
	for name, strategy := range po.compressionStrategies {
		strategies[name] = strategy
	}
	return strategies
}

// GetPartitionStrategies 获取所有分区策略  
func (po *Parquet) GetPartitionStrategies() map[string]PartitionStrategy {
	po.mutex.RLock()
	defer po.mutex.RUnlock()
	
	strategies := make(map[string]PartitionStrategy)
	for name, strategy := range po.partitionStrategies {
		strategies[name] = strategy
	}
	return strategies
} 