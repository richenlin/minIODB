package storage

import (
	"context"
	"crypto/md5"
	"fmt"
	"math"
	"minIODB/internal/logger"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ShardOptimizer 智能分片优化器
type ShardOptimizer struct {
	shardStrategies     map[string]*ShardStrategy
	consistentHashRing  *ConsistentHashRing
	dataLocalityManager *DataLocalityManager
	hotColdSeparator    *HotColdSeparator
	rebalancer          *ShardRebalancer
	stats               *ShardStats
	mutex               sync.RWMutex
}

// ShardStrategy 分片策略
type ShardStrategy struct {
	Name               string            `json:"name"`
	Algorithm          string            `json:"algorithm"`           // hash, range, list, composite
	ShardingKeys       []string          `json:"sharding_keys"`       // 分片键
	ShardCount         int               `json:"shard_count"`         // 分片数量
	ReplicationFactor  int               `json:"replication_factor"`  // 副本因子
	LoadBalancing      bool              `json:"load_balancing"`      // 负载均衡
	AutoRebalance      bool              `json:"auto_rebalance"`      // 自动重平衡
	ConsistencyLevel   string            `json:"consistency_level"`   // consistency level
	DataDistribution   string            `json:"data_distribution"`   // uniform, weighted, adaptive
	AffinityRules      []AffinityRule    `json:"affinity_rules"`      // 亲和性规则
	Predicates         []string          `json:"predicates"`          // 查询谓词
	PartitionTolerance bool              `json:"partition_tolerance"` // 分区容错
	Properties         map[string]string `json:"properties"`          // 自定义属性
}

// AffinityRule 亲和性规则
type AffinityRule struct {
	Type       string   `json:"type"`       // node, rack, region
	Constraint string   `json:"constraint"` // required, preferred
	Keys       []string `json:"keys"`       // 关联键
	Values     []string `json:"values"`     // 关联值
	Weight     float64  `json:"weight"`     // 权重
}

// ConsistentHashRing 一致性哈希环
type ConsistentHashRing struct {
	ring         map[uint32]string     // 哈希环
	sortedHashes []uint32              // 排序的哈希值
	nodes        map[string]*ShardNode // 节点信息
	virtualNodes int                   // 虚拟节点数
	mutex        sync.RWMutex
}

// ShardNode 分片节点
type ShardNode struct {
	NodeID       string            `json:"node_id"`
	Address      string            `json:"address"`
	Zone         string            `json:"zone"`
	Rack         string            `json:"rack"`
	Weight       float64           `json:"weight"`
	Capacity     int64             `json:"capacity"`
	UsedCapacity int64             `json:"used_capacity"`
	Status       string            `json:"status"` // active, inactive, maintenance
	LoadFactor   float64           `json:"load_factor"`
	Health       float64           `json:"health"` // 0.0-1.0
	LastSeen     time.Time         `json:"last_seen"`
	Properties   map[string]string `json:"properties"`
	Shards       []string          `json:"shards"` // 管理的分片
}

// DataLocalityManager 数据局部性管理器
type DataLocalityManager struct {
	affinityMap      map[string][]string       // 数据亲和性映射
	localityScores   map[string]float64        // 局部性得分
	accessPatterns   map[string]*AccessPattern // 访问模式
	cacheStrategy    *LocalityCache            // 局部性缓存策略
	migrationQueue   chan *MigrationTask       // 迁移任务队列
	migrationWorkers int                       // 迁移工作线程数
	stats            *LocalityStats            // 局部性统计
	mutex            sync.RWMutex
}

// AccessPattern 访问模式
type AccessPattern struct {
	DataKey       string            `json:"data_key"`
	AccessCount   int64             `json:"access_count"`
	LastAccess    time.Time         `json:"last_access"`
	AccessNodes   map[string]int64  `json:"access_nodes"`   // 节点访问计数
	AccessRegions map[string]int64  `json:"access_regions"` // 区域访问计数
	Frequency     float64           `json:"frequency"`      // 访问频率
	Locality      float64           `json:"locality"`       // 局部性得分
	Temperature   string            `json:"temperature"`    // hot, warm, cold
	Properties    map[string]string `json:"properties"`
}

// LocalityCache 局部性缓存
type LocalityCache struct {
	CacheSize      int                    `json:"cache_size"`
	TTL            time.Duration          `json:"ttl"`
	EvictionPolicy string                 `json:"eviction_policy"` // lru, lfu, fifo
	HotDataRatio   float64                `json:"hot_data_ratio"`
	WarmDataRatio  float64                `json:"warm_data_ratio"`
	ColdDataRatio  float64                `json:"cold_data_ratio"`
	CacheEntries   map[string]*CacheEntry `json:"cache_entries"`
	mutex          sync.RWMutex
}

// CacheEntry 缓存条目
type CacheEntry struct {
	Key         string      `json:"key"`
	Value       interface{} `json:"value"`
	Size        int64       `json:"size"`
	AccessCount int64       `json:"access_count"`
	LastAccess  time.Time   `json:"last_access"`
	Temperature string      `json:"temperature"`
	ExpiryTime  time.Time   `json:"expiry_time"`
}

// HotColdSeparator 冷热数据分离器
type HotColdSeparator struct {
	hotThreshold    float64                     `json:"hot_threshold"`    // 热数据阈值
	coldThreshold   float64                     `json:"cold_threshold"`   // 冷数据阈值
	temperatureMap  map[string]*DataTemperature `json:"temperature_map"`  // 数据温度映射
	storageStrategy map[string]*StorageClass    `json:"storage_strategy"` // 存储策略
	migrationRules  []MigrationRule             `json:"migration_rules"`  // 迁移规则
	migrationStats  *MigrationStats             `json:"migration_stats"`  // 迁移统计
	analyzer        *TemperatureAnalyzer        `json:"analyzer"`         // 温度分析器
	mutex           sync.RWMutex
}

// DataTemperature 数据温度
type DataTemperature struct {
	DataKey         string    `json:"data_key"`
	Temperature     float64   `json:"temperature"` // 0.0-1.0 (cold-hot)
	AccessFrequency float64   `json:"access_frequency"`
	LastAccess      time.Time `json:"last_access"`
	FirstAccess     time.Time `json:"first_access"`
	AccessCount     int64     `json:"access_count"`
	DataSize        int64     `json:"data_size"`
	StorageClass    string    `json:"storage_class"` // hot, warm, cold, archive
	Prediction      float64   `json:"prediction"`    // 温度预测
	Confidence      float64   `json:"confidence"`    // 预测置信度
}

// StorageClass 存储类别
type StorageClass struct {
	Name              string        `json:"name"`
	Type              string        `json:"type"`               // ssd, hdd, cloud, archive
	Performance       float64       `json:"performance"`        // 性能得分
	Cost              float64       `json:"cost"`               // 成本
	Availability      float64       `json:"availability"`       // 可用性
	Durability        float64       `json:"durability"`         // 持久性
	AccessTime        time.Duration `json:"access_time"`        // 访问时间
	Throughput        int64         `json:"throughput"`         // 吞吐量 MB/s
	Capacity          int64         `json:"capacity"`           // 容量
	Compression       bool          `json:"compression"`        // 压缩支持
	Encryption        bool          `json:"encryption"`         // 加密支持
	ReplicationFactor int           `json:"replication_factor"` // 副本因子
}

// MigrationRule 迁移规则
type MigrationRule struct {
	Name             string   `json:"name"`
	TriggerCondition string   `json:"trigger_condition"` // time, access, size, temperature
	SourceClass      string   `json:"source_class"`
	TargetClass      string   `json:"target_class"`
	Schedule         string   `json:"schedule"`   // cron expression
	Priority         int      `json:"priority"`   // 1-10
	Throttling       float64  `json:"throttling"` // 节流 0.0-1.0
	Conditions       []string `json:"conditions"`
	Actions          []string `json:"actions"`
}

// ShardRebalancer 分片重平衡器
type ShardRebalancer struct {
	rebalanceStrategy string              `json:"rebalance_strategy"` // manual, automatic, scheduled
	triggerThreshold  float64             `json:"trigger_threshold"`  // 触发阈值
	migrationLimit    int                 `json:"migration_limit"`    // 同时迁移限制
	rebalanceTasks    chan *RebalanceTask `json:"-"`                  // 重平衡任务
	taskHistory       []*RebalanceHistory `json:"task_history"`       // 任务历史
	mutex             sync.RWMutex
}

// RebalanceTask 重平衡任务
type RebalanceTask struct {
	TaskID     string            `json:"task_id"`
	TaskType   string            `json:"task_type"` // shard_move, replica_add, replica_remove
	SourceNode string            `json:"source_node"`
	TargetNode string            `json:"target_node"`
	ShardID    string            `json:"shard_id"`
	DataSize   int64             `json:"data_size"`
	Priority   int               `json:"priority"`
	Status     string            `json:"status"`   // pending, running, completed, failed
	Progress   float64           `json:"progress"` // 0.0-1.0
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	Error      string            `json:"error"`
	Properties map[string]string `json:"properties"`
}

// RebalanceHistory 重平衡历史
type RebalanceHistory struct {
	TaskID        string        `json:"task_id"`
	TaskType      string        `json:"task_type"`
	Status        string        `json:"status"`
	DataMoved     int64         `json:"data_moved"`
	Duration      time.Duration `json:"duration"`
	CompletedAt   time.Time     `json:"completed_at"`
	NodesAffected []string      `json:"nodes_affected"`
	Reason        string        `json:"reason"`
}

// MigrationTask 迁移任务
type MigrationTask struct {
	TaskID      string    `json:"task_id"`
	DataKey     string    `json:"data_key"`
	SourceClass string    `json:"source_class"`
	TargetClass string    `json:"target_class"`
	DataSize    int64     `json:"data_size"`
	Priority    int       `json:"priority"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
	ScheduledAt time.Time `json:"scheduled_at"`
}

// TemperatureAnalyzer 温度分析器
type TemperatureAnalyzer struct {
	analysisWindow     time.Duration                  `json:"analysis_window"`
	predictionModel    string                         `json:"prediction_model"` // linear, exponential, ml
	temperatureHistory map[string][]TemperatureRecord `json:"temperature_history"`
	modelAccuracy      float64                        `json:"model_accuracy"`
	lastAnalysis       time.Time                      `json:"last_analysis"`
	mutex              sync.RWMutex
}

// TemperatureRecord 温度记录
type TemperatureRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	Temperature float64   `json:"temperature"`
	AccessCount int64     `json:"access_count"`
	Prediction  float64   `json:"prediction"`
}

// ShardStats 分片统计
type ShardStats struct {
	TotalShards       int64                 `json:"total_shards"`
	ActiveShards      int64                 `json:"active_shards"`
	RebalanceCount    int64                 `json:"rebalance_count"`
	MigrationCount    int64                 `json:"migration_count"`
	HotDataRatio      float64               `json:"hot_data_ratio"`
	ColdDataRatio     float64               `json:"cold_data_ratio"`
	LocalityScore     float64               `json:"locality_score"`
	LoadBalance       float64               `json:"load_balance"`
	PerformanceGain   float64               `json:"performance_gain"`
	StorageSavings    float64               `json:"storage_savings"`
	LastOptimization  time.Time             `json:"last_optimization"`
	OptimizationCount int64                 `json:"optimization_count"`
	NodeStats         map[string]*NodeStats `json:"node_stats"`
	mutex             sync.RWMutex
}

// NodeStats 节点统计
type NodeStats struct {
	NodeID         string    `json:"node_id"`
	ShardCount     int       `json:"shard_count"`
	DataSize       int64     `json:"data_size"`
	LoadFactor     float64   `json:"load_factor"`
	HealthScore    float64   `json:"health_score"`
	LocalityScore  float64   `json:"locality_score"`
	LastRebalance  time.Time `json:"last_rebalance"`
	MigrationCount int64     `json:"migration_count"`
}

// LocalityStats 局部性统计
type LocalityStats struct {
	TotalDataKeys    int64         `json:"total_data_keys"`
	LocalHits        int64         `json:"local_hits"`
	RemoteHits       int64         `json:"remote_hits"`
	LocalityRatio    float64       `json:"locality_ratio"`
	AvgAccessTime    time.Duration `json:"avg_access_time"`
	CacheHitRate     float64       `json:"cache_hit_rate"`
	MigrationSuccess int64         `json:"migration_success"`
	MigrationFailure int64         `json:"migration_failure"`
	LastOptimization time.Time     `json:"last_optimization"`
}

// MigrationStats 迁移统计
type MigrationStats struct {
	TotalMigrations int64     `json:"total_migrations"`
	HotToCold       int64     `json:"hot_to_cold"`
	ColdToHot       int64     `json:"cold_to_hot"`
	WarmMigrations  int64     `json:"warm_migrations"`
	DataMoved       int64     `json:"data_moved"`       // bytes
	CostSavings     float64   `json:"cost_savings"`     // cost units
	PerformanceGain float64   `json:"performance_gain"` // percentage
	LastMigration   time.Time `json:"last_migration"`
	SuccessRate     float64   `json:"success_rate"`
}

// NewShardOptimizer 创建分片优化器
func NewShardOptimizer() *ShardOptimizer {
	optimizer := &ShardOptimizer{
		shardStrategies:     make(map[string]*ShardStrategy),
		consistentHashRing:  NewConsistentHashRing(1000), // 1000个虚拟节点
		dataLocalityManager: NewDataLocalityManager(),
		hotColdSeparator:    NewHotColdSeparator(),
		rebalancer:          NewShardRebalancer(),
		stats: &ShardStats{
			NodeStats: make(map[string]*NodeStats),
		},
	}

	// 初始化分片策略
	optimizer.initShardStrategies()

	return optimizer
}

// NewConsistentHashRing 创建一致性哈希环
func NewConsistentHashRing(virtualNodes int) *ConsistentHashRing {
	return &ConsistentHashRing{
		ring:         make(map[uint32]string),
		sortedHashes: make([]uint32, 0),
		nodes:        make(map[string]*ShardNode),
		virtualNodes: virtualNodes,
	}
}

// NewDataLocalityManager 创建数据局部性管理器
func NewDataLocalityManager() *DataLocalityManager {
	return &DataLocalityManager{
		affinityMap:      make(map[string][]string),
		localityScores:   make(map[string]float64),
		accessPatterns:   make(map[string]*AccessPattern),
		cacheStrategy:    NewLocalityCache(),
		migrationQueue:   make(chan *MigrationTask, 1000),
		migrationWorkers: 10,
		stats:            &LocalityStats{},
	}
}

// NewLocalityCache 创建局部性缓存
func NewLocalityCache() *LocalityCache {
	return &LocalityCache{
		CacheSize:      10000,
		TTL:            time.Hour,
		EvictionPolicy: "lru",
		HotDataRatio:   0.2,
		WarmDataRatio:  0.3,
		ColdDataRatio:  0.5,
		CacheEntries:   make(map[string]*CacheEntry),
	}
}

// NewHotColdSeparator 创建冷热数据分离器
func NewHotColdSeparator() *HotColdSeparator {
	return &HotColdSeparator{
		hotThreshold:    0.8,
		coldThreshold:   0.2,
		temperatureMap:  make(map[string]*DataTemperature),
		storageStrategy: make(map[string]*StorageClass),
		migrationRules:  make([]MigrationRule, 0),
		migrationStats:  &MigrationStats{},
		analyzer:        NewTemperatureAnalyzer(),
	}
}

// NewTemperatureAnalyzer 创建温度分析器
func NewTemperatureAnalyzer() *TemperatureAnalyzer {
	return &TemperatureAnalyzer{
		analysisWindow:     24 * time.Hour,
		predictionModel:    "exponential",
		temperatureHistory: make(map[string][]TemperatureRecord),
		modelAccuracy:      0.85,
		lastAnalysis:       time.Now(),
	}
}

// NewShardRebalancer 创建分片重平衡器
func NewShardRebalancer() *ShardRebalancer {
	return &ShardRebalancer{
		rebalanceStrategy: "automatic",
		triggerThreshold:  0.8,
		migrationLimit:    5,
		rebalanceTasks:    make(chan *RebalanceTask, 100),
		taskHistory:       make([]*RebalanceHistory, 0),
	}
}

// initShardStrategies 初始化分片策略
func (so *ShardOptimizer) initShardStrategies() {
	strategies := []*ShardStrategy{
		{
			Name:              "hash_uniform",
			Algorithm:         "hash",
			ShardingKeys:      []string{"id"},
			ShardCount:        16,
			ReplicationFactor: 3,
			LoadBalancing:     true,
			AutoRebalance:     true,
			ConsistencyLevel:  "eventual",
			DataDistribution:  "uniform",
		},
		{
			Name:              "range_temporal",
			Algorithm:         "range",
			ShardingKeys:      []string{"timestamp"},
			ShardCount:        32,
			ReplicationFactor: 2,
			LoadBalancing:     true,
			AutoRebalance:     false,
			ConsistencyLevel:  "strong",
			DataDistribution:  "temporal",
		},
		{
			Name:              "composite_user_time",
			Algorithm:         "composite",
			ShardingKeys:      []string{"user_id", "timestamp"},
			ShardCount:        64,
			ReplicationFactor: 3,
			LoadBalancing:     true,
			AutoRebalance:     true,
			ConsistencyLevel:  "eventual",
			DataDistribution:  "adaptive",
		},
		{
			Name:              "geo_distributed",
			Algorithm:         "list",
			ShardingKeys:      []string{"region"},
			ShardCount:        8,
			ReplicationFactor: 2,
			LoadBalancing:     false,
			AutoRebalance:     false,
			ConsistencyLevel:  "strong",
			DataDistribution:  "geographical",
		},
	}

	for _, strategy := range strategies {
		so.shardStrategies[strategy.Name] = strategy
	}
}

// AddNode 添加节点到哈希环
func (chr *ConsistentHashRing) AddNode(ctx context.Context, node *ShardNode) {
	chr.mutex.Lock()
	defer chr.mutex.Unlock()

	chr.nodes[node.NodeID] = node

	// 为节点创建虚拟节点
	for i := 0; i < chr.virtualNodes; i++ {
		virtualNodeID := fmt.Sprintf("%s:%d", node.NodeID, i)
		hash := chr.hash(virtualNodeID)
		chr.ring[hash] = node.NodeID
		chr.sortedHashes = append(chr.sortedHashes, hash)
	}

	// 排序哈希值
	sort.Slice(chr.sortedHashes, func(i, j int) bool {
		return chr.sortedHashes[i] < chr.sortedHashes[j]
	})

	logger.LogInfo(ctx, "Added node %s to consistent hash ring with %d virtual nodes", zap.String("node_id", node.NodeID), zap.Int("virtual_nodes", chr.virtualNodes))
}

// RemoveNode 从哈希环移除节点
func (chr *ConsistentHashRing) RemoveNode(ctx context.Context, nodeID string) {
	chr.mutex.Lock()
	defer chr.mutex.Unlock()

	delete(chr.nodes, nodeID)

	// 移除虚拟节点
	newSortedHashes := make([]uint32, 0)
	for _, hash := range chr.sortedHashes {
		if chr.ring[hash] != nodeID {
			newSortedHashes = append(newSortedHashes, hash)
		} else {
			delete(chr.ring, hash)
		}
	}
	chr.sortedHashes = newSortedHashes

	logger.LogInfo(ctx, "Removed node %s from consistent hash ring", zap.String("node_id", nodeID))
}

// GetNode 获取数据对应的节点
func (chr *ConsistentHashRing) GetNode(ctx context.Context, key string) string {
	chr.mutex.RLock()
	defer chr.mutex.RUnlock()

	if len(chr.sortedHashes) == 0 {
		return ""
	}

	hash := chr.hash(key)

	// 在哈希环上找到第一个大于等于key hash的节点
	idx := sort.Search(len(chr.sortedHashes), func(i int) bool {
		return chr.sortedHashes[i] >= hash
	})

	// 如果没找到，选择第一个节点（环形结构）
	if idx == len(chr.sortedHashes) {
		idx = 0
	}

	return chr.ring[chr.sortedHashes[idx]]
}

// hash 哈希函数
func (chr *ConsistentHashRing) hash(key string) uint32 {
	h := md5.Sum([]byte(key))
	return uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
}

// GetShardingStrategy 获取分片策略
func (so *ShardOptimizer) GetShardingStrategy(ctx context.Context, dataPattern string, workloadType string) *ShardStrategy {
	so.mutex.RLock()
	defer so.mutex.RUnlock()

	// 根据数据模式和工作负载选择最优策略
	switch {
	case workloadType == "analytical" && dataPattern == "temporal":
		return so.shardStrategies["range_temporal"]
	case workloadType == "geographical":
		return so.shardStrategies["geo_distributed"]
	case dataPattern == "user_based":
		return so.shardStrategies["composite_user_time"]
	default:
		return so.shardStrategies["hash_uniform"]
	}
}

// OptimizeDataPlacement 优化数据放置
func (so *ShardOptimizer) OptimizeDataPlacement(ctx context.Context, dataKey string, dataSize int64, accessPattern *AccessPattern) (string, error) {
	// 分析访问模式
	temperature := so.hotColdSeparator.AnalyzeTemperature(ctx, dataKey, accessPattern)

	// 选择存储类别
	storageClass := so.hotColdSeparator.SelectStorageClass(ctx, temperature)

	// 选择最优节点
	optimalNode := so.selectOptimalNode(ctx, dataKey, temperature, storageClass)

	// 更新局部性信息
	so.dataLocalityManager.UpdateAffinityMap(ctx, dataKey, optimalNode, accessPattern)

	logger.LogInfo(ctx, "Optimized data placement for key %s: node=%s, storage_class=%s, temperature=%.2f", zap.String("data_key", dataKey), zap.String("optimal_node", optimalNode), zap.String("storage_class", storageClass), zap.Float64("temperature", temperature.Temperature))

	return optimalNode, nil
}

// selectOptimalNode 选择最优节点
func (so *ShardOptimizer) selectOptimalNode(ctx context.Context, dataKey string, temperature *DataTemperature, storageClass string) string {
	// 使用一致性哈希环获取基础节点
	baseNode := so.consistentHashRing.GetNode(ctx, dataKey)

	// 根据温度和存储类别调整节点选择
	if temperature.Temperature > so.hotColdSeparator.hotThreshold {
		// 热数据：选择高性能节点
		return so.selectHighPerformanceNode(ctx, baseNode)
	} else if temperature.Temperature < so.hotColdSeparator.coldThreshold {
		// 冷数据：选择低成本节点
		return so.selectLowCostNode(ctx, baseNode)
	}

	return baseNode
}

// selectHighPerformanceNode 选择高性能节点
func (so *ShardOptimizer) selectHighPerformanceNode(ctx context.Context, fallbackNode string) string {
	so.consistentHashRing.mutex.RLock()
	defer so.consistentHashRing.mutex.RUnlock()

	var bestNode string
	var bestScore float64

	for nodeID, node := range so.consistentHashRing.nodes {
		if node.Status != "active" {
			continue
		}

		// 计算性能得分：健康度 + (1 - 负载因子) + 权重
		score := node.Health + (1.0 - node.LoadFactor) + node.Weight*0.5

		if bestNode == "" || score > bestScore {
			bestScore = score
			bestNode = nodeID
		}
	}

	if bestNode == "" {
		return fallbackNode
	}

	return bestNode
}

// selectLowCostNode 选择低成本节点
func (so *ShardOptimizer) selectLowCostNode(ctx context.Context, fallbackNode string) string {
	so.consistentHashRing.mutex.RLock()
	defer so.consistentHashRing.mutex.RUnlock()

	var bestNode string
	var bestScore float64

	for nodeID, node := range so.consistentHashRing.nodes {
		if node.Status != "active" {
			continue
		}

		// 计算成本效益得分：容量利用率 + 健康度 - 负载因子
		utilizationRatio := float64(node.UsedCapacity) / float64(node.Capacity)
		score := (1.0 - utilizationRatio) + node.Health - node.LoadFactor

		if bestNode == "" || score > bestScore {
			bestScore = score
			bestNode = nodeID
		}
	}

	if bestNode == "" {
		return fallbackNode
	}

	return bestNode
}

// AnalyzeTemperature 分析数据温度
func (hcs *HotColdSeparator) AnalyzeTemperature(ctx context.Context, dataKey string, accessPattern *AccessPattern) *DataTemperature {
	hcs.mutex.Lock()
	defer hcs.mutex.Unlock()

	// 获取现有温度记录或创建新记录
	temperature, exists := hcs.temperatureMap[dataKey]
	if !exists {
		temperature = &DataTemperature{
			DataKey:      dataKey,
			FirstAccess:  time.Now(),
			StorageClass: "warm", // 默认为温数据
		}
		hcs.temperatureMap[dataKey] = temperature
	}

	// 更新访问信息
	temperature.LastAccess = time.Now()
	temperature.AccessCount = accessPattern.AccessCount
	temperature.AccessFrequency = accessPattern.Frequency

	// 计算温度值
	temperature.Temperature = hcs.calculateTemperature(ctx, accessPattern)

	// 分析器预测
	prediction := hcs.analyzer.PredictTemperature(ctx, dataKey, temperature)
	temperature.Prediction = prediction.Temperature
	temperature.Confidence = prediction.Confidence

	return temperature
}

// calculateTemperature 计算数据温度
func (hcs *HotColdSeparator) calculateTemperature(ctx context.Context, accessPattern *AccessPattern) float64 {
	now := time.Now()

	// 时间衰减因子
	timeSinceLastAccess := now.Sub(accessPattern.LastAccess)
	timeDecay := math.Exp(-timeSinceLastAccess.Hours() / 24.0) // 24小时半衰期

	// 频率权重
	frequencyWeight := math.Min(accessPattern.Frequency/100.0, 1.0) // 归一化到0-1

	// 局部性权重
	localityWeight := accessPattern.Locality

	// 综合温度计算
	temperature := (timeDecay*0.4 + frequencyWeight*0.4 + localityWeight*0.2)

	return math.Max(0.0, math.Min(1.0, temperature))
}

// SelectStorageClass 选择存储类别
func (hcs *HotColdSeparator) SelectStorageClass(ctx context.Context, temperature *DataTemperature) string {
	if temperature.Temperature >= hcs.hotThreshold {
		return "hot"
	} else if temperature.Temperature <= hcs.coldThreshold {
		return "cold"
	} else {
		return "warm"
	}
}

// PredictTemperature 预测数据温度
func (ta *TemperatureAnalyzer) PredictTemperature(ctx context.Context, dataKey string, current *DataTemperature) *DataTemperature {
	ta.mutex.Lock()
	defer ta.mutex.Unlock()

	// 获取历史温度记录
	history, exists := ta.temperatureHistory[dataKey]
	if !exists || len(history) < 2 {
		// 没有足够的历史数据，返回当前温度
		return &DataTemperature{
			Temperature: current.Temperature,
			Confidence:  0.5, // 低置信度
		}
	}

	// 使用指数衰减模型预测
	var prediction float64
	var confidence float64

	switch ta.predictionModel {
	case "linear":
		prediction, confidence = ta.linearPrediction(ctx, history)
	case "exponential":
		prediction, confidence = ta.exponentialPrediction(ctx, history)
	default:
		prediction = current.Temperature
		confidence = 0.5
	}

	return &DataTemperature{
		Temperature: prediction,
		Confidence:  confidence,
	}
}

// linearPrediction 线性预测
func (ta *TemperatureAnalyzer) linearPrediction(ctx context.Context, history []TemperatureRecord) (float64, float64) {
	if len(history) < 2 {
		return 0.0, 0.0
	}

	// 简单线性回归
	n := float64(len(history))
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0

	for i, record := range history {
		x := float64(i)
		y := record.Temperature
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// 计算斜率和截距
	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	intercept := (sumY - slope*sumX) / n

	// 预测下一个点
	nextX := n
	prediction := slope*nextX + intercept

	// 计算置信度（基于R²）
	// 这里简化为基于历史数据的稳定性
	confidence := math.Min(0.9, ta.modelAccuracy)

	return math.Max(0.0, math.Min(1.0, prediction)), confidence
}

// exponentialPrediction 指数预测
func (ta *TemperatureAnalyzer) exponentialPrediction(ctx context.Context, history []TemperatureRecord) (float64, float64) {
	if len(history) < 2 {
		return 0.0, 0.0
	}

	// 指数平滑
	alpha := 0.3 // 平滑因子
	prediction := history[0].Temperature

	for i := 1; i < len(history); i++ {
		prediction = alpha*history[i].Temperature + (1-alpha)*prediction
	}

	confidence := math.Min(0.85, ta.modelAccuracy)

	return prediction, confidence
}

// UpdateAffinityMap 更新亲和性映射
func (dlm *DataLocalityManager) UpdateAffinityMap(ctx context.Context, dataKey, nodeID string, accessPattern *AccessPattern) {
	dlm.mutex.Lock()
	defer dlm.mutex.Unlock()

	// 更新数据键的节点亲和性
	if nodes, exists := dlm.affinityMap[dataKey]; exists {
		// 检查节点是否已存在
		found := false
		for _, node := range nodes {
			if node == nodeID {
				found = true
				break
			}
		}
		if !found {
			dlm.affinityMap[dataKey] = append(nodes, nodeID)
		}
	} else {
		dlm.affinityMap[dataKey] = []string{nodeID}
	}

	// 更新局部性得分
	dlm.localityScores[dataKey] = dlm.calculateLocalityScore(ctx, accessPattern)

	// 更新访问模式
	dlm.accessPatterns[dataKey] = accessPattern
}

// calculateLocalityScore 计算局部性得分
func (dlm *DataLocalityManager) calculateLocalityScore(ctx context.Context, accessPattern *AccessPattern) float64 {
	if accessPattern == nil {
		return 0.5
	}

	// 基于访问节点的分布计算局部性
	totalAccess := int64(0)
	maxNodeAccess := int64(0)

	for _, count := range accessPattern.AccessNodes {
		totalAccess += count
		if count > maxNodeAccess {
			maxNodeAccess = count
		}
	}

	if totalAccess == 0 {
		return 0.5
	}

	// 局部性得分 = 最大节点访问数 / 总访问数
	localityScore := float64(maxNodeAccess) / float64(totalAccess)

	return localityScore
}

// TriggerRebalance 触发重平衡
func (sr *ShardRebalancer) TriggerRebalance(ctx context.Context, reason string) {
	sr.mutex.Lock()
	defer sr.mutex.Unlock()

	taskID := fmt.Sprintf("rebalance_%d", time.Now().Unix())

	task := &RebalanceTask{
		TaskID:     taskID,
		TaskType:   "shard_rebalance",
		Priority:   5,
		Status:     "pending",
		StartTime:  time.Now(),
		Properties: map[string]string{"reason": reason},
	}

	select {
	case sr.rebalanceTasks <- task:
		logger.LogInfo(ctx, "Rebalance task %s queued: %s", zap.String("task_id", taskID), zap.String("reason", reason))
	default:
		logger.LogInfo(ctx, "Rebalance task queue full, dropping task: %s", zap.String("reason", reason))
	}
}

// GetStats 获取分片统计信息
func (so *ShardOptimizer) GetStats() *ShardStats {
	so.stats.mutex.RLock()
	defer so.stats.mutex.RUnlock()

	// 创建统计信息副本
	statsCopy := *so.stats
	statsCopy.NodeStats = make(map[string]*NodeStats)
	for nodeID, nodeStats := range so.stats.NodeStats {
		nodeStatsCopy := *nodeStats
		statsCopy.NodeStats[nodeID] = &nodeStatsCopy
	}

	return &statsCopy
}

// UpdateStats 更新统计信息
func (so *ShardOptimizer) UpdateStats(ctx context.Context) {
	so.stats.mutex.Lock()
	defer so.stats.mutex.Unlock()

	// 统计分片信息
	so.stats.TotalShards = int64(len(so.consistentHashRing.nodes))

	// 计算负载均衡度
	so.stats.LoadBalance = so.calculateLoadBalance(ctx)

	// 计算局部性得分
	so.stats.LocalityScore = so.dataLocalityManager.calculateOverallLocalityScore(ctx)

	// 统计冷热数据比例
	so.updateTemperatureRatios()

	so.stats.LastOptimization = time.Now()
	so.stats.OptimizationCount++
}

// calculateLoadBalance 计算负载均衡度
func (so *ShardOptimizer) calculateLoadBalance(ctx context.Context) float64 {
	so.consistentHashRing.mutex.RLock()
	defer so.consistentHashRing.mutex.RUnlock()

	if len(so.consistentHashRing.nodes) == 0 {
		return 1.0
	}

	loadFactors := make([]float64, 0, len(so.consistentHashRing.nodes))
	for _, node := range so.consistentHashRing.nodes {
		loadFactors = append(loadFactors, node.LoadFactor)
	}

	// 计算标准差
	mean := 0.0
	for _, lf := range loadFactors {
		mean += lf
	}
	mean /= float64(len(loadFactors))

	variance := 0.0
	for _, lf := range loadFactors {
		variance += (lf - mean) * (lf - mean)
	}
	variance /= float64(len(loadFactors))

	stdDev := math.Sqrt(variance)

	// 负载均衡度 = 1 - 标准差（标准差越小，均衡度越高）
	return math.Max(0.0, 1.0-stdDev)
}

// calculateOverallLocalityScore 计算整体局部性得分
func (dlm *DataLocalityManager) calculateOverallLocalityScore(ctx context.Context) float64 {
	dlm.mutex.RLock()
	defer dlm.mutex.RUnlock()

	if len(dlm.localityScores) == 0 {
		return 0.5
	}

	totalScore := 0.0
	for _, score := range dlm.localityScores {
		totalScore += score
	}

	return totalScore / float64(len(dlm.localityScores))
}

// updateTemperatureRatios 更新温度比例
func (so *ShardOptimizer) updateTemperatureRatios() {
	so.hotColdSeparator.mutex.RLock()
	defer so.hotColdSeparator.mutex.RUnlock()

	if len(so.hotColdSeparator.temperatureMap) == 0 {
		so.stats.HotDataRatio = 0.0
		so.stats.ColdDataRatio = 0.0
		return
	}

	hotCount := 0
	coldCount := 0

	for _, temp := range so.hotColdSeparator.temperatureMap {
		if temp.Temperature >= so.hotColdSeparator.hotThreshold {
			hotCount++
		} else if temp.Temperature <= so.hotColdSeparator.coldThreshold {
			coldCount++
		}
	}

	total := float64(len(so.hotColdSeparator.temperatureMap))
	so.stats.HotDataRatio = float64(hotCount) / total
	so.stats.ColdDataRatio = float64(coldCount) / total
}
