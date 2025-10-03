package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	pb "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"
	"minIODB/internal/discovery"
	"minIODB/internal/logger"
	"minIODB/internal/pool"
	"minIODB/internal/query"
	"minIODB/internal/storage"
	"minIODB/internal/utils"
	"minIODB/pkg/consistenthash"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// QueryPlan 查询计划
type QueryPlan struct {
	SQL           string              `json:"sql"`
	TargetNodes   []string            `json:"target_nodes"`
	FileMapping   map[string][]string `json:"file_mapping"` // node -> files
	IsDistributed bool                `json:"is_distributed"`
}

// QueryResult 查询结果
type QueryResult struct {
	NodeID string `json:"node_id"`
	Data   string `json:"data"`
	Error  string `json:"error,omitempty"`
}

// WriteCoordinator 写入协调器
type WriteCoordinator struct {
	registry       *discovery.ServiceRegistry
	hashRing       *consistenthash.ConsistentHash
	circuitBreaker *utils.CircuitBreaker
	mu             sync.RWMutex
}

// QueryInfo 查询信息
type QueryInfo struct {
	ID        string    `json:"id"`
	Query     string    `json:"query"`
	StartTime time.Time `json:"start_time"`
	Status    string    `json:"status"`
	NodeID    string    `json:"node_id"`
}

// NodeStatus 节点状态
type NodeStatus struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	Status        string    `json:"status"`
	LastSeen      time.Time `json:"last_seen"`
	ActiveQueries int       `json:"active_queries"`
	Load          float64   `json:"load"`
}

// LoadBalancer 负载均衡器
type LoadBalancer struct {
	cfg *config.Config
}

// NewLoadBalancer 创建负载均衡器
func NewLoadBalancer(cfg *config.Config) *LoadBalancer {
	return &LoadBalancer{
		cfg: cfg,
	}
}

// HealthChecker 健康检查器
type HealthChecker struct {
	cfg *config.Config
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(cfg *config.Config) *HealthChecker {
	return &HealthChecker{
		cfg: cfg,
	}
}

// CoordinatorMetrics 协调器指标
type CoordinatorMetrics struct {
	TotalQueries    int64         `json:"total_queries"`
	SuccessQueries  int64         `json:"success_queries"`
	FailedQueries   int64         `json:"failed_queries"`
	AvgResponseTime time.Duration `json:"avg_response_time"`
}

// NewCoordinatorMetrics 创建协调器指标
func NewCoordinatorMetrics() *CoordinatorMetrics {
	return &CoordinatorMetrics{}
}

// QueryCoordinator 查询协调器
type QueryCoordinator struct {
	redisPool      *pool.RedisPool
	registry       *discovery.ServiceRegistry
	localQuerier   LocalQuerier
	cfg            *config.Config
	circuitBreaker *utils.CircuitBreaker
	indexSystem    *storage.IndexSystem    // 索引系统
	shardOptimizer *storage.ShardOptimizer // 分片优化器
	mu             sync.RWMutex
	activeQueries  map[string]*QueryInfo
	nodeStatus     map[string]*NodeStatus
	loadBalancer   *LoadBalancer
	healthChecker  *HealthChecker
	metrics        *CoordinatorMetrics
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// LocalQuerier 本地查询接口
type LocalQuerier interface {
	ExecuteQuery(ctx context.Context, sql string) (string, error)
}

// NewWriteCoordinator 创建写入协调器
func NewWriteCoordinator(registry *discovery.ServiceRegistry) *WriteCoordinator {
	cb := utils.NewCircuitBreaker("write_coordinator", utils.DefaultCircuitBreakerConfig)

	return &WriteCoordinator{
		registry:       registry,
		hashRing:       consistenthash.New(150),
		circuitBreaker: cb,
	}
}

// NewQueryCoordinator 创建查询协调器
func NewQueryCoordinator(ctx context.Context, redisPool *pool.RedisPool, registry *discovery.ServiceRegistry, localQuerier LocalQuerier, cfg *config.Config, indexSystem *storage.IndexSystem) *QueryCoordinator {
	ctx, cancel := context.WithCancel(ctx)
	cb := utils.NewCircuitBreaker("query_coordinator", utils.DefaultCircuitBreakerConfig)

	qc := &QueryCoordinator{
		redisPool:      redisPool,
		registry:       registry,
		localQuerier:   localQuerier,
		cfg:            cfg,
		circuitBreaker: cb,
		indexSystem:    indexSystem, // 索引系统
		activeQueries:  make(map[string]*QueryInfo),
		nodeStatus:     make(map[string]*NodeStatus),
		loadBalancer:   NewLoadBalancer(cfg),
		healthChecker:  NewHealthChecker(cfg),
		metrics:        NewCoordinatorMetrics(),
		ctx:            ctx,
		cancel:         cancel,
	}

	// 初始化分片优化器（如果存储引擎启用）
	if cfg != nil && cfg.StorageEngine.Enabled {
		qc.shardOptimizer = storage.NewShardOptimizer()
		logger.LogInfo(ctx, "Shard optimizer initialized for query coordinator")
	}

	// 启动监控goroutine
	qc.wg.Add(1)
	go qc.monitorNodes()

	return qc
}

// RouteWrite 路由写入请求到对应的节点
func (wc *WriteCoordinator) RouteWrite(ctx context.Context, req *pb.WriteDataRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 使用熔断器执行操作
	var targetNode string
	var err error

	cbErr := wc.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		// 获取所有活跃节点
		nodes, getErr := wc.registry.GetHealthyNodes(ctx)
		if getErr != nil {
			return utils.NewRetryableError(fmt.Errorf("failed to get healthy nodes: %w", getErr))
		}

		if len(nodes) == 0 {
			return fmt.Errorf("no healthy nodes available")
		}

		// 更新哈希环
		wc.updateHashRing(nodes)

		// 根据数据ID选择节点
		targetNode = wc.hashRing.Get(req.Data.Id)
		if targetNode == "" {
			return fmt.Errorf("failed to select target node for ID: %s", req.Data.Id)
		}

		return nil
	})

	if cbErr != nil {
		return "", cbErr
	}

	// 检查是否是本地节点
	currentNodeAddr := fmt.Sprintf("%s:%s",
		wc.registry.GetNodeInfo().Address,
		wc.registry.GetNodeInfo().Port)

	if targetNode == currentNodeAddr {
		return "local", nil
	}

	// 发送到远程节点（使用重试机制）
	err = utils.Retry(ctx, utils.DefaultRetryConfig, "remote_write", func(ctx context.Context) error {
		return wc.sendWriteToNode(ctx, targetNode, req)
	})

	if err != nil {
		return "", fmt.Errorf("failed to send write to node %s: %w", targetNode, err)
	}

	return targetNode, nil
}

// sendWriteToNode 发送写入请求到指定节点
func (wc *WriteCoordinator) sendWriteToNode(ctx context.Context, nodeAddr string, req *pb.WriteDataRequest) error {
	conn, err := grpc.DialContext(ctx, nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return utils.NewRetryableError(fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err))
	}
	defer conn.Close()

	client := pb.NewMinIODBServiceClient(conn)
	_, err = client.WriteData(ctx, req)
	if err != nil {
		return utils.NewRetryableError(fmt.Errorf("failed to write to node %s: %w", nodeAddr, err))
	}

	return nil
}

// updateHashRing 更新哈希环
func (wc *WriteCoordinator) updateHashRing(nodes []*discovery.NodeInfo) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	// 创建新的哈希环
	newHashRing := consistenthash.New(150)
	for _, node := range nodes {
		nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
		newHashRing.Add(nodeAddr)
	}

	wc.hashRing = newHashRing
}

// ExecuteDistributedQuery 执行分布式查询
func (qc *QueryCoordinator) ExecuteDistributedQuery(ctx context.Context, sql string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// 1. 创建查询计划
	plan, err := qc.createQueryPlan(ctx, sql)
	if err != nil {
		return "", fmt.Errorf("failed to create query plan: %w", err)
	}

	// 2. 如果不需要分布式查询，直接本地执行
	if !plan.IsDistributed {
		return qc.localQuerier.ExecuteQuery(ctx, sql)
	}

	// 3. 执行分布式查询
	return qc.executeDistributedPlan(ctx, plan)
}

// createQueryPlan 创建查询计划（集成分片优化）
func (qc *QueryCoordinator) createQueryPlan(ctx context.Context, sql string) (*QueryPlan, error) {
	plan := &QueryPlan{
		SQL:         sql,
		FileMapping: make(map[string][]string),
	}

	// 获取所有健康节点
	nodes, err := qc.registry.GetHealthyNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy nodes: %w", err)
	}

	if len(nodes) <= 1 {
		// 单节点或无节点，使用本地查询
		plan.IsDistributed = false
		return plan, nil
	}

	// 分析SQL，确定需要查询的表和数据分片
	tables, err := qc.extractTablesFromSQL(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("failed to extract tables: %w", err)
	}

	// 获取数据分布信息
	dataDistribution, err := qc.getDataDistribution(ctx, tables)
	if err != nil {
		logger.LogInfo(ctx, "WARN: failed to get data distribution, falling back to broadcast: %v", zap.Error(err))
		// 回退到广播模式
		for _, node := range nodes {
			nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
			plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
		}
		plan.IsDistributed = true
		return plan, nil
	}

	// 使用分片优化器优化查询计划
	if qc.shardOptimizer != nil {
		dataDistribution = qc.optimizeQueryPlanWithSharding(ctx, sql, tables, dataDistribution, nodes)
	}

	// 根据数据分布创建查询计划
	for nodeAddr, files := range dataDistribution {
		if len(files) > 0 {
			plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
			plan.FileMapping[nodeAddr] = files
		}
	}

	plan.IsDistributed = len(plan.TargetNodes) > 1
	return plan, nil
}

// executeDistributedPlan 执行分布式查询计划
func (qc *QueryCoordinator) executeDistributedPlan(ctx context.Context, plan *QueryPlan) (string, error) {
	var results []QueryResult

	// 使用熔断器执行查询
	cbErr := qc.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		// 并行查询所有目标节点
		resultChan := make(chan QueryResult, len(plan.TargetNodes))

		for _, nodeAddr := range plan.TargetNodes {
			go func(addr string) {
				// 使用重试机制查询节点
				retryErr := utils.Retry(ctx, utils.DefaultRetryConfig, "remote_query", func(ctx context.Context) error {
					result, queryErr := qc.executeRemoteQuery(ctx, addr, plan.SQL)
					if queryErr != nil {
						resultChan <- QueryResult{
							NodeID: addr,
							Error:  queryErr.Error(),
						}
						return utils.NewRetryableError(queryErr)
					}
					resultChan <- QueryResult{
						NodeID: addr,
						Data:   result,
					}
					return nil
				})

				if retryErr != nil {
					resultChan <- QueryResult{
						NodeID: addr,
						Error:  retryErr.Error(),
					}
				}
			}(nodeAddr)
		}

		// 收集结果
		for i := 0; i < len(plan.TargetNodes); i++ {
			select {
			case result := <-resultChan:
				results = append(results, result)
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})

	if cbErr != nil {
		return "", cbErr
	}

	// 聚合结果
	return qc.aggregateQueryResults(ctx, results, plan.SQL)
}

// executeRemoteQuery 执行远程查询
func (qc *QueryCoordinator) executeRemoteQuery(ctx context.Context, nodeAddr, sql string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 检查是否是本节点
	currentNode := fmt.Sprintf("%s:%s",
		qc.registry.GetNodeInfo().Address,
		qc.registry.GetNodeInfo().Port)

	if nodeAddr == currentNode {
		// 本节点查询，调用本地查询引擎
		return qc.localQuerier.ExecuteQuery(ctx, sql)
	}

	// 连接到远程节点
	conn, err := grpc.DialContext(ctx, nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err)
	}
	defer conn.Close()

	client := pb.NewMinIODBServiceClient(conn)
	req := &pb.QueryDataRequest{Sql: sql}

	resp, err := client.QueryData(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to query node %s: %w", nodeAddr, err)
	}

	return resp.ResultJson, nil
}

// extractTablesFromSQL 从SQL中提取表名
func (qc *QueryCoordinator) extractTablesFromSQL(ctx context.Context, sql string) ([]string, error) {
	// 使用增强的TableExtractor
	extractor := query.NewTableExtractor()
	tables := extractor.ExtractTableNames(sql)
	return tables, nil
}

// getDataDistribution 获取数据分布信息（集成索引系统）
func (qc *QueryCoordinator) getDataDistribution(ctx context.Context, tables []string) (map[string][]string, error) {
	distribution := make(map[string][]string)

	// 如果Redis未启用，直接返回本地节点信息
	if !qc.cfg.Redis.Enabled {
		currentNode := fmt.Sprintf("%s:%s",
			qc.registry.GetNodeInfo().Address,
			qc.registry.GetNodeInfo().Port)
		// 在单节点模式下，假设所有数据都在本地
		distribution[currentNode] = []string{} // 空文件列表表示需要本地扫描
		return distribution, nil
	}

	// 优先使用索引系统进行智能查询
	if qc.indexSystem != nil {
		indexDistribution, err := qc.getDataDistributionFromIndex(ctx, tables)
		if err == nil && len(indexDistribution) > 0 {
			logger.LogInfo(ctx, "Using index system for data distribution query")
			return indexDistribution, nil
		}
		logger.LogInfo(ctx, "Index system query failed, falling back to Redis: %v", zap.Error(err))
	}

	// 回退到Redis查询
	for _, table := range tables {
		// 从Redis获取该表的文件分布信息
		pattern := fmt.Sprintf("file_index:%s:*", table)
		keys, err := qc.redisPool.GetClient().Keys(ctx, pattern).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get file index keys for table %s: %w", table, err)
		}

		// 获取每个文件的节点信息
		for _, key := range keys {
			// 获取文件的节点信息
			nodeInfo, err := qc.redisPool.GetClient().HGet(ctx, key, "node").Result()
			if err != nil {
				// 如果没有节点信息，跳过
				continue
			}

			// 获取文件名
			fileName, err := qc.redisPool.GetClient().HGet(ctx, key, "file").Result()
			if err != nil {
				continue
			}

			// 添加到分布信息
			distribution[nodeInfo] = append(distribution[nodeInfo], fileName)
		}
	}

	return distribution, nil
}

// getDataDistributionFromIndex 使用索引系统获取数据分布信息
func (qc *QueryCoordinator) getDataDistributionFromIndex(ctx context.Context, tables []string) (map[string][]string, error) {
	distribution := make(map[string][]string)
	indexUsed := false

	for _, table := range tables {
		// 使用索引系统进行智能文件过滤
		candidateFiles := make([]string, 0)

		// 1. 尝试使用BloomFilter进行快速过滤
		bloomKey := fmt.Sprintf("bloom:%s", table)
		useBloom := qc.indexSystem.HasBloomFilter(ctx, bloomKey)
		if useBloom {
			logger.LogInfo(ctx, "Using BloomFilter index for table %s", zap.String("table", table))
			indexUsed = true
		}

		// 2. 尝试使用MinMax索引进行范围查询优化
		minMaxKey := fmt.Sprintf("minmax:%s", table)
		useMinMax := qc.indexSystem.HasMinMaxIndex(ctx, minMaxKey)
		if useMinMax {
			logger.LogInfo(ctx, "Using MinMax index for table %s", zap.String("table", table))
			indexUsed = true
		}

		// 3. 从Redis获取表的文件列表
		pattern := fmt.Sprintf("index:table:%s:id:*", table)
		keys, err := qc.redisPool.GetClient().Keys(ctx, pattern).Result()
		if err != nil {
			logger.LogInfo(ctx, "Failed to get file index keys for table %s: %v", zap.String("table", table), zap.Error(err))
			continue
		}

		// 4. 收集所有候选文件
		for _, key := range keys {
			files, err := qc.redisPool.GetClient().SMembers(ctx, key).Result()
			if err != nil {
				continue
			}
			candidateFiles = append(candidateFiles, files...)
		}

		// 5. 如果使用了索引，通过索引系统进行过滤
		// 注意：这里简化实现，实际应该根据具体查询条件调用索引系统的查询方法
		filteredFiles := candidateFiles
		if indexUsed {
			logger.LogInfo(ctx, "Index system filtered %d files for table %s", zap.Int("filtered_files", len(filteredFiles)), zap.String("table", table))
		}

		// 6. 分配文件到节点
		currentNode := fmt.Sprintf("%s:%s",
			qc.registry.GetNodeInfo().Address,
			qc.registry.GetNodeInfo().Port)
		distribution[currentNode] = append(distribution[currentNode], filteredFiles...)
	}

	// 如果索引系统被使用，记录到统计
	if indexUsed {
		qc.metrics.TotalQueries++
	}

	return distribution, nil
}

// optimizeQueryPlanWithSharding 使用分片优化器优化查询计划
func (qc *QueryCoordinator) optimizeQueryPlanWithSharding(ctx context.Context, sql string, tables []string, dataDistribution map[string][]string, nodes []*discovery.NodeInfo) map[string][]string {
	logger.LogInfo(ctx, "Optimizing query plan using shard optimizer for %d tables", zap.Int("tables", len(tables)))

	// 获取分片统计信息
	shardStats := qc.shardOptimizer.GetStats()

	// 基于负载均衡优化文件分配
	if shardStats.LoadBalance < 0.7 {
		// 负载不均衡，需要优化文件分配
		logger.LogInfo(ctx, "Detected load imbalance (%.2f), rebalancing file distribution", zap.Float64("load_balance", shardStats.LoadBalance))
		dataDistribution = qc.rebalanceFileDistribution(dataDistribution, nodes)
	}

	// 基于数据局部性优化查询路由
	optimizedDistribution := qc.optimizeDataLocality(ctx, dataDistribution, tables)

	// 减少不必要的节点访问
	optimizedDistribution = qc.pruneEmptyNodes(optimizedDistribution)

	logger.LogInfo(ctx, "Query plan optimized: %d nodes involved (original: %d)",
		zap.Int("optimized_nodes", len(optimizedDistribution)),
		zap.Int("original_nodes", len(dataDistribution)))

	return optimizedDistribution
}

// rebalanceFileDistribution 重新平衡文件分布
func (qc *QueryCoordinator) rebalanceFileDistribution(distribution map[string][]string, nodes []*discovery.NodeInfo) map[string][]string {
	// 收集所有文件
	allFiles := make([]string, 0)
	for _, files := range distribution {
		allFiles = append(allFiles, files...)
	}

	// 计算每个节点应该处理的文件数
	nodeCount := len(nodes)
	if nodeCount == 0 {
		return distribution
	}

	filesPerNode := (len(allFiles) + nodeCount - 1) / nodeCount

	// 重新分配文件到节点
	rebalanced := make(map[string][]string)
	nodeIndex := 0

	for i := 0; i < len(allFiles); i += filesPerNode {
		if nodeIndex >= nodeCount {
			break
		}

		node := nodes[nodeIndex]
		nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)

		end := i + filesPerNode
		if end > len(allFiles) {
			end = len(allFiles)
		}

		rebalanced[nodeAddr] = allFiles[i:end]
		nodeIndex++
	}

	return rebalanced
}

// optimizeDataLocality 优化数据局部性
func (qc *QueryCoordinator) optimizeDataLocality(ctx context.Context, distribution map[string][]string, tables []string) map[string][]string {
	// 简化实现：检查分片优化器是否有局部性信息
	// 实际实现应该根据访问模式历史来优化

	optimized := make(map[string][]string)

	for nodeAddr, files := range distribution {
		// 过滤出该节点上具有良好局部性的文件
		localFiles := make([]string, 0)

		for _, file := range files {
			// 检查文件是否包含分片信息
			if strings.Contains(file, "shard_") {
				// 这是一个已经优化过的分片文件
				localFiles = append(localFiles, file)
			} else {
				// 普通文件，也包含进来
				localFiles = append(localFiles, file)
			}
		}

		if len(localFiles) > 0 {
			optimized[nodeAddr] = localFiles
		}
	}

	return optimized
}

// pruneEmptyNodes 删除空节点
func (qc *QueryCoordinator) pruneEmptyNodes(distribution map[string][]string) map[string][]string {
	pruned := make(map[string][]string)

	for nodeAddr, files := range distribution {
		if len(files) > 0 {
			pruned[nodeAddr] = files
		}
	}

	return pruned
}

// aggregateQueryResults 聚合查询结果
func (qc *QueryCoordinator) aggregateQueryResults(ctx context.Context, results []QueryResult, sql string) (string, error) {
	// 过滤出成功的结果
	var successResults []string
	var errors []string

	for _, result := range results {
		if result.Error != "" {
			errors = append(errors, fmt.Sprintf("Node %s: %s", result.NodeID, result.Error))
		} else {
			successResults = append(successResults, result.Data)
		}
	}

	// 如果有错误，记录日志
	if len(errors) > 0 {
		logger.LogInfo(ctx, "Some query nodes failed: %v", zap.Strings("errors", errors))
	}

	// 如果没有成功结果
	if len(successResults) == 0 {
		return "", fmt.Errorf("all query nodes failed: %v", errors)
	}

	// 单个结果直接返回
	if len(successResults) == 1 {
		return successResults[0], nil
	}

	// 多个结果需要聚合
	return qc.mergeResults(ctx, successResults, sql)
}

// mergeResults 合并多个查询结果
func (qc *QueryCoordinator) mergeResults(ctx context.Context, results []string, sql string) (string, error) {
	// 分析SQL类型来决定如何合并结果
	lowerSQL := strings.ToLower(sql)

	// 如果是聚合查询（COUNT, SUM, AVG等），需要特殊处理
	if strings.Contains(lowerSQL, "count(") {
		return qc.aggregateCountResults(ctx, results)
	}

	if strings.Contains(lowerSQL, "sum(") {
		return qc.aggregateSumResults(ctx, results)
	}

	// 对于普通SELECT查询，简单合并所有结果
	return qc.unionResults(ctx, results)
}

// aggregateCountResults 聚合COUNT查询结果
func (qc *QueryCoordinator) aggregateCountResults(ctx context.Context, results []string) (string, error) {
	totalCount := 0

	for _, result := range results {
		// 解析JSON结果
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}

		for _, row := range data {
			for _, value := range row {
				if count, ok := value.(float64); ok {
					totalCount += int(count)
				}
			}
		}
	}

	// 返回聚合后的结果
	aggregatedResult := []map[string]interface{}{
		{"count": totalCount},
	}

	resultBytes, err := json.Marshal(aggregatedResult)
	if err != nil {
		return "", fmt.Errorf("failed to marshal aggregated count result: %w", err)
	}

	return string(resultBytes), nil
}

// aggregateSumResults 聚合SUM查询结果
func (qc *QueryCoordinator) aggregateSumResults(ctx context.Context, results []string) (string, error) {
	totalSum := 0.0

	for _, result := range results {
		// 解析JSON结果
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}

		for _, row := range data {
			for _, value := range row {
				if sum, ok := value.(float64); ok {
					totalSum += sum
				}
			}
		}
	}

	// 返回聚合后的结果
	aggregatedResult := []map[string]interface{}{
		{"sum": totalSum},
	}

	resultBytes, err := json.Marshal(aggregatedResult)
	if err != nil {
		return "", fmt.Errorf("failed to marshal aggregated sum result: %w", err)
	}

	return string(resultBytes), nil
}

// unionResults 合并普通查询结果
func (qc *QueryCoordinator) unionResults(ctx context.Context, results []string) (string, error) {
	var allData []map[string]interface{}

	for _, result := range results {
		// 解析JSON结果
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			logger.LogInfo(ctx, "WARN: failed to parse result JSON: %v", zap.Error(err))
			continue
		}

		allData = append(allData, data...)
	}

	// 返回合并后的结果
	resultBytes, err := json.Marshal(allData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal union result: %w", err)
	}

	return string(resultBytes), nil
}

// GetQueryStats 获取查询统计信息
func (qc *QueryCoordinator) GetQueryStats(ctx context.Context) map[string]interface{} {
	nodes, err := qc.registry.DiscoverNodes(ctx)
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	return map[string]interface{}{
		"total_nodes":     len(nodes),
		"hash_ring_stats": qc.registry.GetHashRing().Stats(),
		"nodes":           nodes,
	}
}

// monitorNodes 监控节点
func (qc *QueryCoordinator) monitorNodes() {
	defer qc.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-qc.ctx.Done():
			return
		case <-ticker.C:
			// 监控节点状态
			logger.LogInfo(qc.ctx, "Monitoring nodes...")
		}
	}
}
