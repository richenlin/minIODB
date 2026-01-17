package storage

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"minIODB/pkg/pool"

	"github.com/bits-and-blooms/bloom/v3"
)

// IndexSystem 索引系统
type IndexSystem struct {
	bloomFilters     map[string]*BloomFilterIndex
	minMaxIndexes    map[string]*MinMaxIndex
	invertedIndexes  map[string]*InvertedIndex
	bitmapIndexes    map[string]*BitmapIndex
	compositeIndexes map[string]*CompositeIndex

	indexConfig *IndexConfig
	stats       *IndexStats
	redisPool   *pool.RedisPool
	mutex       sync.RWMutex
}

// IndexConfig 索引配置
type IndexConfig struct {
	BloomFilterConfig *BloomFilterConfig `json:"bloom_filter"`
	MinMaxConfig      *MinMaxConfig      `json:"minmax"`
	InvertedConfig    *InvertedConfig    `json:"inverted"`
	BitmapConfig      *BitmapConfig      `json:"bitmap"`
	CompositeConfig   *CompositeConfig   `json:"composite"`

	EnableAutoIndex   bool   `json:"enable_auto_index"`
	IndexThreshold    int64  `json:"index_threshold"`
	MaintenanceWindow string `json:"maintenance_window"`
	CompressionLevel  int    `json:"compression_level"`
	CacheSize         int64  `json:"cache_size"`
}

// BloomFilterConfig BloomFilter配置
type BloomFilterConfig struct {
	FalsePositiveRate float64       `json:"false_positive_rate"`
	EstimatedElements uint          `json:"estimated_elements"`
	HashFunctions     uint          `json:"hash_functions"`
	EnableCache       bool          `json:"enable_cache"`
	TTL               time.Duration `json:"ttl"`
}

// MinMaxConfig MinMax索引配置
type MinMaxConfig struct {
	EnableStatistics bool   `json:"enable_statistics"`
	PrecisionDigits  int    `json:"precision_digits"`
	CompressionType  string `json:"compression_type"`
	EnablePredicate  bool   `json:"enable_predicate"`
}

// InvertedConfig 倒排索引配置
type InvertedConfig struct {
	TokenizerType   string             `json:"tokenizer_type"`
	EnableStemming  bool               `json:"enable_stemming"`
	EnableStopWords bool               `json:"enable_stop_words"`
	MinTokenLength  int                `json:"min_token_length"`
	MaxTokenLength  int                `json:"max_token_length"`
	CustomStopWords []string           `json:"custom_stop_words"`
	FieldWeights    map[string]float64 `json:"field_weights"`
}

// BitmapConfig 位图索引配置
type BitmapConfig struct {
	CompressionType    string `json:"compression_type"` // roaring, wah, ewah
	EnableOptimization bool   `json:"enable_optimization"`
	ChunkSize          int    `json:"chunk_size"`
}

// CompositeConfig 复合索引配置
type CompositeConfig struct {
	MaxColumns         int      `json:"max_columns"`
	EnablePartialMatch bool     `json:"enable_partial_match"`
	SortOrder          []string `json:"sort_order"`
}

// BloomFilterIndex BloomFilter索引
type BloomFilterIndex struct {
	name         string
	bloomFilter  *bloom.BloomFilter
	config       *BloomFilterConfig
	stats        *BloomStats
	elementCount uint
	lastUpdate   time.Time
	mutex        sync.RWMutex
}

// BloomStats BloomFilter统计
type BloomStats struct {
	TotalQueries   int64     `json:"total_queries"`
	TruePositives  int64     `json:"true_positives"`
	FalsePositives int64     `json:"false_positives"`
	TrueNegatives  int64     `json:"true_negatives"`
	ActualFPRate   float64   `json:"actual_fp_rate"`
	FilterSize     uint      `json:"filter_size"`
	HashFunctions  uint      `json:"hash_functions"`
	ElementCount   uint      `json:"element_count"`
	LastReset      time.Time `json:"last_reset"`
}

// MinMaxIndex MinMax索引
type MinMaxIndex struct {
	name       string
	columnName string
	dataType   string
	minValue   interface{}
	maxValue   interface{}
	nullCount  int64
	totalCount int64
	config     *MinMaxConfig
	statistics *ColumnStatistics
	lastUpdate time.Time
	mutex      sync.RWMutex
}

// ColumnStatistics 列统计信息
type ColumnStatistics struct {
	Mean          float64          `json:"mean"`
	Median        float64          `json:"median"`
	Mode          interface{}      `json:"mode"`
	StandardDev   float64          `json:"standard_deviation"`
	Variance      float64          `json:"variance"`
	Percentiles   map[int]float64  `json:"percentiles"`
	Histogram     map[string]int64 `json:"histogram"`
	DistinctCount int64            `json:"distinct_count"`
	Cardinality   float64          `json:"cardinality"`
	Skewness      float64          `json:"skewness"`
	Kurtosis      float64          `json:"kurtosis"`
}

// InvertedIndex 倒排索引
type InvertedIndex struct {
	name          string
	columnName    string
	termIndex     map[string]*PostingList
	documentCount int64
	config        *InvertedConfig
	tokenizer     *Tokenizer
	stemmer       *Stemmer
	stats         *InvertedStats
	lastUpdate    time.Time
	mutex         sync.RWMutex
}

// PostingList 倒排列表
type PostingList struct {
	Term         string     `json:"term"`
	DocumentFreq int64      `json:"document_frequency"`
	TotalFreq    int64      `json:"total_frequency"`
	Postings     []*Posting `json:"postings"`
	LastUpdate   time.Time  `json:"last_update"`
}

// Posting 倒排记录
type Posting struct {
	DocumentID   string    `json:"document_id"`
	TermFreq     int       `json:"term_frequency"`
	Positions    []int     `json:"positions"`
	Score        float64   `json:"score"`
	LastAccessed time.Time `json:"last_accessed"`
}

// InvertedStats 倒排索引统计
type InvertedStats struct {
	TotalTerms       int64                    `json:"total_terms"`
	TotalDocuments   int64                    `json:"total_documents"`
	TotalPostings    int64                    `json:"total_postings"`
	AvgDocLength     float64                  `json:"avg_doc_length"`
	VocabularySize   int64                    `json:"vocabulary_size"`
	IndexSize        int64                    `json:"index_size"`
	CompressionRatio float64                  `json:"compression_ratio"`
	QueryPerformance map[string]time.Duration `json:"query_performance"`
}

// BitmapIndex 位图索引
type BitmapIndex struct {
	name        string
	columnName  string
	bitmaps     map[interface{}]*RoaringBitmap
	cardinality int64
	config      *BitmapConfig
	stats       *BitmapStats
	lastUpdate  time.Time
	mutex       sync.RWMutex
}

// RoaringBitmap Roaring位图
type RoaringBitmap struct {
	bits        map[uint32]bool
	cardinality uint32
	compressed  bool
	lastAccess  time.Time
}

// BitmapStats 位图索引统计
type BitmapStats struct {
	TotalBitmaps     int64         `json:"total_bitmaps"`
	TotalBits        int64         `json:"total_bits"`
	CompressionRatio float64       `json:"compression_ratio"`
	QueryTime        time.Duration `json:"avg_query_time"`
	UpdateTime       time.Duration `json:"avg_update_time"`
	MemoryUsage      int64         `json:"memory_usage"`
}

// CompositeIndex 复合索引
type CompositeIndex struct {
	name       string
	columns    []string
	keyIndex   map[string]*CompositeKey
	config     *CompositeConfig
	stats      *CompositeStats
	lastUpdate time.Time
	mutex      sync.RWMutex
}

// CompositeKey 复合键
type CompositeKey struct {
	Values       []interface{} `json:"values"`
	DocumentIDs  []string      `json:"document_ids"`
	KeyHash      string        `json:"key_hash"`
	Cardinality  int64         `json:"cardinality"`
	LastAccessed time.Time     `json:"last_accessed"`
}

// CompositeStats 复合索引统计
type CompositeStats struct {
	TotalKeys        int64                    `json:"total_keys"`
	TotalDocuments   int64                    `json:"total_documents"`
	AvgKeyLength     float64                  `json:"avg_key_length"`
	KeyDistribution  map[int]int64            `json:"key_distribution"`
	QueryPerformance map[string]time.Duration `json:"query_performance"`
	IndexEfficiency  float64                  `json:"index_efficiency"`
}

// IndexStats 索引系统统计
type IndexStats struct {
	TotalIndexes    int64              `json:"total_indexes"`
	IndexTypes      map[string]int64   `json:"index_types"`
	TotalQueries    int64              `json:"total_queries"`
	CacheHitRate    float64            `json:"cache_hit_rate"`
	AvgQueryTime    time.Duration      `json:"avg_query_time"`
	IndexSizes      map[string]int64   `json:"index_sizes"`
	MemoryUsage     int64              `json:"memory_usage"`
	DiskUsage       int64              `json:"disk_usage"`
	MaintenanceTime time.Duration      `json:"maintenance_time"`
	LastMaintenance time.Time          `json:"last_maintenance"`
	IndexEfficiency map[string]float64 `json:"index_efficiency"`
	mutex           sync.RWMutex
}

// Tokenizer 分词器
type Tokenizer struct {
	tokenizerType string
	stopWords     map[string]bool
	minLength     int
	maxLength     int
}

// Stemmer 词干提取器
type Stemmer struct {
	language string
	enabled  bool
}

// QueryPredicate 查询谓词
type QueryPredicate struct {
	Column        string        `json:"column"`
	Operator      string        `json:"operator"` // =, !=, <, <=, >, >=, IN, LIKE, CONTAINS
	Value         interface{}   `json:"value"`
	Values        []interface{} `json:"values"` // for IN operator
	CaseSensitive bool          `json:"case_sensitive"`
}

// IndexQuery 索引查询
type IndexQuery struct {
	IndexName  string            `json:"index_name"`
	Predicates []*QueryPredicate `json:"predicates"`
	LogicalOp  string            `json:"logical_op"` // AND, OR, NOT
	OrderBy    []string          `json:"order_by"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
}

// QueryResult 查询结果
type QueryResult struct {
	DocumentIDs   []string               `json:"document_ids"`
	TotalMatches  int64                  `json:"total_matches"`
	QueryTime     time.Duration          `json:"query_time"`
	IndexesUsed   []string               `json:"indexes_used"`
	EstimatedCost float64                `json:"estimated_cost"`
	ActualCost    float64                `json:"actual_cost"`
	CacheHit      bool                   `json:"cache_hit"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// NewIndexSystem 创建索引系统
func NewIndexSystem(redisPool *pool.RedisPool) *IndexSystem {
	return &IndexSystem{
		bloomFilters:     make(map[string]*BloomFilterIndex),
		minMaxIndexes:    make(map[string]*MinMaxIndex),
		invertedIndexes:  make(map[string]*InvertedIndex),
		bitmapIndexes:    make(map[string]*BitmapIndex),
		compositeIndexes: make(map[string]*CompositeIndex),
		indexConfig:      NewDefaultIndexConfig(),
		stats: &IndexStats{
			IndexTypes:      make(map[string]int64),
			IndexSizes:      make(map[string]int64),
			IndexEfficiency: make(map[string]float64),
		},
		redisPool: redisPool,
	}
}

// NewDefaultIndexConfig 创建默认索引配置
func NewDefaultIndexConfig() *IndexConfig {
	return &IndexConfig{
		BloomFilterConfig: &BloomFilterConfig{
			FalsePositiveRate: 0.01,
			EstimatedElements: 100000,
			HashFunctions:     4,
			EnableCache:       true,
			TTL:               24 * time.Hour,
		},
		MinMaxConfig: &MinMaxConfig{
			EnableStatistics: true,
			PrecisionDigits:  6,
			CompressionType:  "snappy",
			EnablePredicate:  true,
		},
		InvertedConfig: &InvertedConfig{
			TokenizerType:   "standard",
			EnableStemming:  true,
			EnableStopWords: true,
			MinTokenLength:  2,
			MaxTokenLength:  50,
			CustomStopWords: []string{},
			FieldWeights:    make(map[string]float64),
		},
		BitmapConfig: &BitmapConfig{
			CompressionType:    "roaring",
			EnableOptimization: true,
			ChunkSize:          1024,
		},
		CompositeConfig: &CompositeConfig{
			MaxColumns:         5,
			EnablePartialMatch: true,
			SortOrder:          []string{"asc"},
		},
		EnableAutoIndex:   true,
		IndexThreshold:    10000,
		MaintenanceWindow: "02:00-04:00",
		CompressionLevel:  6,
		CacheSize:         100 * 1024 * 1024, // 100MB
	}
}

// CreateBloomFilter 创建BloomFilter索引
func (is *IndexSystem) CreateBloomFilter(name, columnName string) error {
	is.mutex.Lock()
	defer is.mutex.Unlock()

	config := is.indexConfig.BloomFilterConfig
	bloomFilter := bloom.NewWithEstimates(config.EstimatedElements, config.FalsePositiveRate)

	index := &BloomFilterIndex{
		name:        name,
		bloomFilter: bloomFilter,
		config:      config,
		stats: &BloomStats{
			FilterSize:    config.EstimatedElements,
			HashFunctions: config.HashFunctions,
			LastReset:     time.Now(),
		},
		lastUpdate: time.Now(),
	}

	is.bloomFilters[name] = index
	is.updateIndexStats("bloom_filter", 1)

	log.Printf("Created BloomFilter index: %s for column: %s", name, columnName)
	return nil
}

// CreateMinMaxIndex 创建MinMax索引
func (is *IndexSystem) CreateMinMaxIndex(name, columnName, dataType string) error {
	is.mutex.Lock()
	defer is.mutex.Unlock()

	index := &MinMaxIndex{
		name:       name,
		columnName: columnName,
		dataType:   dataType,
		config:     is.indexConfig.MinMaxConfig,
		statistics: &ColumnStatistics{
			Percentiles: make(map[int]float64),
			Histogram:   make(map[string]int64),
		},
		lastUpdate: time.Now(),
	}

	is.minMaxIndexes[name] = index
	is.updateIndexStats("minmax", 1)

	log.Printf("Created MinMax index: %s for column: %s", name, columnName)
	return nil
}

// CreateInvertedIndex 创建倒排索引
func (is *IndexSystem) CreateInvertedIndex(name, columnName string) error {
	is.mutex.Lock()
	defer is.mutex.Unlock()

	config := is.indexConfig.InvertedConfig

	index := &InvertedIndex{
		name:       name,
		columnName: columnName,
		termIndex:  make(map[string]*PostingList),
		config:     config,
		tokenizer:  NewTokenizer(config),
		stemmer:    NewStemmer("english", config.EnableStemming),
		stats: &InvertedStats{
			QueryPerformance: make(map[string]time.Duration),
		},
		lastUpdate: time.Now(),
	}

	is.invertedIndexes[name] = index
	is.updateIndexStats("inverted", 1)

	log.Printf("Created Inverted index: %s for column: %s", name, columnName)
	return nil
}

// CreateBitmapIndex 创建位图索引
func (is *IndexSystem) CreateBitmapIndex(name, columnName string) error {
	is.mutex.Lock()
	defer is.mutex.Unlock()

	index := &BitmapIndex{
		name:       name,
		columnName: columnName,
		bitmaps:    make(map[interface{}]*RoaringBitmap),
		config:     is.indexConfig.BitmapConfig,
		stats:      &BitmapStats{},
		lastUpdate: time.Now(),
	}

	is.bitmapIndexes[name] = index
	is.updateIndexStats("bitmap", 1)

	log.Printf("Created Bitmap index: %s for column: %s", name, columnName)
	return nil
}

// CreateCompositeIndex 创建复合索引
func (is *IndexSystem) CreateCompositeIndex(name string, columns []string) error {
	is.mutex.Lock()
	defer is.mutex.Unlock()

	if len(columns) > is.indexConfig.CompositeConfig.MaxColumns {
		return fmt.Errorf("too many columns for composite index: %d > %d",
			len(columns), is.indexConfig.CompositeConfig.MaxColumns)
	}

	index := &CompositeIndex{
		name:     name,
		columns:  columns,
		keyIndex: make(map[string]*CompositeKey),
		config:   is.indexConfig.CompositeConfig,
		stats: &CompositeStats{
			KeyDistribution:  make(map[int]int64),
			QueryPerformance: make(map[string]time.Duration),
		},
		lastUpdate: time.Now(),
	}

	is.compositeIndexes[name] = index
	is.updateIndexStats("composite", 1)

	log.Printf("Created Composite index: %s for columns: %v", name, columns)
	return nil
}

// AddToBloomFilter 添加元素到BloomFilter
func (is *IndexSystem) AddToBloomFilter(indexName string, value string) error {
	is.mutex.RLock()
	index, exists := is.bloomFilters[indexName]
	is.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("BloomFilter index not found: %s", indexName)
	}

	index.mutex.Lock()
	defer index.mutex.Unlock()

	index.bloomFilter.AddString(value)
	index.elementCount++
	index.lastUpdate = time.Now()

	return nil
}

// TestBloomFilter 测试BloomFilter
func (is *IndexSystem) TestBloomFilter(indexName string, value string) (bool, error) {
	is.mutex.RLock()
	index, exists := is.bloomFilters[indexName]
	is.mutex.RUnlock()

	if !exists {
		return false, fmt.Errorf("BloomFilter index not found: %s", indexName)
	}

	index.mutex.RLock()
	defer index.mutex.RUnlock()

	result := index.bloomFilter.TestString(value)

	// 更新统计信息
	index.stats.TotalQueries++
	if result {
		// 这里需要实际验证是否为真正的positive
		// 简化起见，假设有一定的false positive rate
		index.stats.TruePositives++
	} else {
		index.stats.TrueNegatives++
	}

	// 计算实际false positive rate
	if index.stats.TotalQueries > 0 {
		index.stats.ActualFPRate = float64(index.stats.FalsePositives) / float64(index.stats.TotalQueries)
	}

	return result, nil
}

// UpdateMinMaxIndex 更新MinMax索引
func (is *IndexSystem) UpdateMinMaxIndex(indexName string, value interface{}) error {
	is.mutex.RLock()
	index, exists := is.minMaxIndexes[indexName]
	is.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("MinMax index not found: %s", indexName)
	}

	index.mutex.Lock()
	defer index.mutex.Unlock()

	// 更新最小值和最大值
	if index.minValue == nil || is.compareValues(value, index.minValue) < 0 {
		index.minValue = value
	}
	if index.maxValue == nil || is.compareValues(value, index.maxValue) > 0 {
		index.maxValue = value
	}

	index.totalCount++
	index.lastUpdate = time.Now()

	return nil
}

// compareValues 比较两个值
func (is *IndexSystem) compareValues(a, b interface{}) int {
	switch va := a.(type) {
	case int:
		if vb, ok := b.(int); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case int64:
		if vb, ok := b.(int64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case float64:
		if vb, ok := b.(float64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case string:
		if vb, ok := b.(string); ok {
			return strings.Compare(va, vb)
		}
	case time.Time:
		if vb, ok := b.(time.Time); ok {
			if va.Before(vb) {
				return -1
			} else if va.After(vb) {
				return 1
			}
			return 0
		}
	}
	return 0
}

// QueryMinMaxIndex 查询MinMax索引
func (is *IndexSystem) QueryMinMaxIndex(indexName string, predicate *QueryPredicate) (bool, error) {
	is.mutex.RLock()
	index, exists := is.minMaxIndexes[indexName]
	is.mutex.RUnlock()

	if !exists {
		return false, fmt.Errorf("MinMax index not found: %s", indexName)
	}

	index.mutex.RLock()
	defer index.mutex.RUnlock()

	// 根据谓词判断是否可能匹配
	switch predicate.Operator {
	case "=":
		// 值必须在min和max之间
		return is.compareValues(predicate.Value, index.minValue) >= 0 &&
			is.compareValues(predicate.Value, index.maxValue) <= 0, nil
	case "<":
		// 如果最小值都大于等于查询值，则没有匹配
		return is.compareValues(index.minValue, predicate.Value) < 0, nil
	case "<=":
		return is.compareValues(index.minValue, predicate.Value) <= 0, nil
	case ">":
		// 如果最大值都小于等于查询值，则没有匹配
		return is.compareValues(index.maxValue, predicate.Value) > 0, nil
	case ">=":
		return is.compareValues(index.maxValue, predicate.Value) >= 0, nil
	default:
		return true, nil // 无法判断，返回可能匹配
	}
}

// AddToInvertedIndex 添加文档到倒排索引
func (is *IndexSystem) AddToInvertedIndex(indexName, documentID, text string) error {
	is.mutex.RLock()
	index, exists := is.invertedIndexes[indexName]
	is.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("Inverted index not found: %s", indexName)
	}

	index.mutex.Lock()
	defer index.mutex.Unlock()

	// 分词
	tokens := index.tokenizer.Tokenize(text)

	// 词干提取
	if index.stemmer.enabled {
		tokens = index.stemmer.StemTokens(tokens)
	}

	// 更新倒排索引
	for position, token := range tokens {
		postingList, exists := index.termIndex[token]
		if !exists {
			postingList = &PostingList{
				Term:       token,
				Postings:   make([]*Posting, 0),
				LastUpdate: time.Now(),
			}
			index.termIndex[token] = postingList
		}

		// 查找是否已有该文档的记录
		var posting *Posting
		for _, p := range postingList.Postings {
			if p.DocumentID == documentID {
				posting = p
				break
			}
		}

		if posting == nil {
			posting = &Posting{
				DocumentID:   documentID,
				TermFreq:     0,
				Positions:    make([]int, 0),
				LastAccessed: time.Now(),
			}
			postingList.Postings = append(postingList.Postings, posting)
			postingList.DocumentFreq++
		}

		posting.TermFreq++
		posting.Positions = append(posting.Positions, position)
		postingList.TotalFreq++
	}

	index.documentCount++
	index.lastUpdate = time.Now()

	return nil
}

// QueryInvertedIndex 查询倒排索引
func (is *IndexSystem) QueryInvertedIndex(indexName, query string) ([]*Posting, error) {
	is.mutex.RLock()
	index, exists := is.invertedIndexes[indexName]
	is.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("Inverted index not found: %s", indexName)
	}

	index.mutex.RLock()
	defer index.mutex.RUnlock()

	startTime := time.Now()

	// 分词和处理查询
	tokens := index.tokenizer.Tokenize(query)
	if index.stemmer.enabled {
		tokens = index.stemmer.StemTokens(tokens)
	}

	// 获取所有匹配的posting lists
	var allPostings []*Posting
	for _, token := range tokens {
		if postingList, exists := index.termIndex[token]; exists {
			allPostings = append(allPostings, postingList.Postings...)
		}
	}

	// 计算TF-IDF分数
	for _, posting := range allPostings {
		posting.Score = is.calculateTFIDF(posting, index.documentCount)
	}

	// 按分数排序
	sort.Slice(allPostings, func(i, j int) bool {
		return allPostings[i].Score > allPostings[j].Score
	})

	// 记录查询性能
	queryTime := time.Since(startTime)
	index.stats.QueryPerformance[query] = queryTime

	return allPostings, nil
}

// calculateTFIDF 计算TF-IDF分数
func (is *IndexSystem) calculateTFIDF(posting *Posting, totalDocs int64) float64 {
	// TF: term frequency
	tf := float64(posting.TermFreq)

	// IDF: inverse document frequency (简化计算)
	// 这里需要知道包含该term的文档数，简化起见使用posting的文档频率
	idf := math.Log(float64(totalDocs) / (1.0 + 1.0)) // 简化计算

	return tf * idf
}

// NewTokenizer 创建分词器
func NewTokenizer(config *InvertedConfig) *Tokenizer {
	stopWords := make(map[string]bool)

	// 默认停用词
	defaultStopWords := []string{"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for", "of", "with", "by"}
	for _, word := range defaultStopWords {
		stopWords[word] = true
	}

	// 自定义停用词
	for _, word := range config.CustomStopWords {
		stopWords[word] = true
	}

	return &Tokenizer{
		tokenizerType: config.TokenizerType,
		stopWords:     stopWords,
		minLength:     config.MinTokenLength,
		maxLength:     config.MaxTokenLength,
	}
}

// Tokenize 分词
func (t *Tokenizer) Tokenize(text string) []string {
	// 简单的空格分词（实际应用中可以使用更复杂的分词器）
	words := strings.Fields(strings.ToLower(text))

	var tokens []string
	for _, word := range words {
		// 去除标点符号
		word = strings.Trim(word, ".,!?;:\"'()[]{}'-")

		// 检查长度
		if len(word) < t.minLength || len(word) > t.maxLength {
			continue
		}

		// 检查停用词
		if t.stopWords[word] {
			continue
		}

		tokens = append(tokens, word)
	}

	return tokens
}

// NewStemmer 创建词干提取器
func NewStemmer(language string, enabled bool) *Stemmer {
	return &Stemmer{
		language: language,
		enabled:  enabled,
	}
}

// StemTokens 词干提取
func (s *Stemmer) StemTokens(tokens []string) []string {
	if !s.enabled {
		return tokens
	}

	// 简单的英语词干提取（实际应用中可以使用Porter Stemmer等算法）
	stemmed := make([]string, len(tokens))
	for i, token := range tokens {
		stemmed[i] = s.stemWord(token)
	}

	return stemmed
}

// stemWord 词干提取单词
func (s *Stemmer) stemWord(word string) string {
	// 简单的后缀去除规则
	suffixes := []string{"ing", "ed", "er", "est", "ly", "s"}

	for _, suffix := range suffixes {
		if strings.HasSuffix(word, suffix) && len(word) > len(suffix)+2 {
			return word[:len(word)-len(suffix)]
		}
	}

	return word
}

// updateIndexStats 更新索引统计
func (is *IndexSystem) updateIndexStats(indexType string, delta int64) {
	is.stats.mutex.Lock()
	defer is.stats.mutex.Unlock()

	is.stats.TotalIndexes += delta
	is.stats.IndexTypes[indexType] += delta
}

// GetStats 获取索引统计信息
func (is *IndexSystem) GetStats() *IndexStats {
	is.stats.mutex.RLock()
	defer is.stats.mutex.RUnlock()

	statsCopy := &IndexStats{
		TotalIndexes:    is.stats.TotalIndexes,
		TotalQueries:    is.stats.TotalQueries,
		CacheHitRate:    is.stats.CacheHitRate,
		AvgQueryTime:    is.stats.AvgQueryTime,
		MemoryUsage:     is.stats.MemoryUsage,
		DiskUsage:       is.stats.DiskUsage,
		MaintenanceTime: is.stats.MaintenanceTime,
		LastMaintenance: is.stats.LastMaintenance,
		IndexTypes:      make(map[string]int64),
		IndexSizes:      make(map[string]int64),
		IndexEfficiency: make(map[string]float64),
	}

	for k, v := range is.stats.IndexTypes {
		statsCopy.IndexTypes[k] = v
	}
	for k, v := range is.stats.IndexSizes {
		statsCopy.IndexSizes[k] = v
	}
	for k, v := range is.stats.IndexEfficiency {
		statsCopy.IndexEfficiency[k] = v
	}

	return statsCopy
}

// OptimizeIndexes 优化索引
func (is *IndexSystem) OptimizeIndexes(ctx context.Context) error {
	log.Println("Starting index optimization...")

	startTime := time.Now()

	// 优化BloomFilter索引
	if err := is.optimizeBloomFilters(); err != nil {
		log.Printf("Failed to optimize bloom filters: %v", err)
	}

	// 优化MinMax索引
	if err := is.optimizeMinMaxIndexes(); err != nil {
		log.Printf("Failed to optimize minmax indexes: %v", err)
	}

	// 优化倒排索引
	if err := is.optimizeInvertedIndexes(); err != nil {
		log.Printf("Failed to optimize inverted indexes: %v", err)
	}

	// 更新统计信息
	is.stats.mutex.Lock()
	is.stats.MaintenanceTime = time.Since(startTime)
	is.stats.LastMaintenance = time.Now()
	is.stats.mutex.Unlock()

	log.Printf("Index optimization completed in %v", time.Since(startTime))
	return nil
}

// optimizeBloomFilters 优化BloomFilter索引
func (is *IndexSystem) optimizeBloomFilters() error {
	is.mutex.RLock()
	filters := make([]*BloomFilterIndex, 0, len(is.bloomFilters))
	for _, filter := range is.bloomFilters {
		filters = append(filters, filter)
	}
	is.mutex.RUnlock()

	for _, filter := range filters {
		filter.mutex.Lock()

		// 检查是否需要重建（false positive rate过高）
		if filter.stats.ActualFPRate > filter.config.FalsePositiveRate*2 {
			log.Printf("Rebuilding bloom filter %s due to high FP rate: %.4f",
				filter.name, filter.stats.ActualFPRate)

			// 重建过程（这里简化处理）
			newSize := filter.stats.ElementCount * 2
			filter.bloomFilter = bloom.NewWithEstimates(uint(newSize), filter.config.FalsePositiveRate)
			filter.stats.LastReset = time.Now()
			filter.stats.FalsePositives = 0
			filter.stats.TotalQueries = 0
		}

		filter.mutex.Unlock()
	}

	return nil
}

// optimizeMinMaxIndexes 优化MinMax索引
func (is *IndexSystem) optimizeMinMaxIndexes() error {
	is.mutex.RLock()
	indexes := make([]*MinMaxIndex, 0, len(is.minMaxIndexes))
	for _, index := range is.minMaxIndexes {
		indexes = append(indexes, index)
	}
	is.mutex.RUnlock()

	for _, index := range indexes {
		index.mutex.Lock()

		// 更新统计信息
		if index.config.EnableStatistics && index.totalCount > 0 {
			// 计算列统计信息（这里简化处理）
			index.statistics.DistinctCount = index.totalCount // 简化
			index.statistics.Cardinality = float64(index.statistics.DistinctCount) / float64(index.totalCount)
		}

		index.mutex.Unlock()
	}

	return nil
}

// optimizeInvertedIndexes 优化倒排索引
func (is *IndexSystem) optimizeInvertedIndexes() error {
	is.mutex.RLock()
	indexes := make([]*InvertedIndex, 0, len(is.invertedIndexes))
	for _, index := range is.invertedIndexes {
		indexes = append(indexes, index)
	}
	is.mutex.RUnlock()

	for _, index := range indexes {
		index.mutex.Lock()

		// 清理过期的posting
		for term, postingList := range index.termIndex {
			filteredPostings := make([]*Posting, 0, len(postingList.Postings))
			for _, posting := range postingList.Postings {
				// 保留最近访问的posting（简化策略）
				if time.Since(posting.LastAccessed) < 24*time.Hour {
					filteredPostings = append(filteredPostings, posting)
				}
			}
			postingList.Postings = filteredPostings
			postingList.DocumentFreq = int64(len(filteredPostings))

			// 如果没有posting了，删除这个term
			if len(filteredPostings) == 0 {
				delete(index.termIndex, term)
			}
		}

		// 更新统计信息
		index.stats.TotalTerms = int64(len(index.termIndex))
		index.stats.VocabularySize = index.stats.TotalTerms

		index.mutex.Unlock()
	}

	return nil
}
