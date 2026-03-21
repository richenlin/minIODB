package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	pb "minIODB/api/proto/miniodb/v1"
	"minIODB/config"
	"minIODB/internal/discovery"
	"minIODB/internal/query"
	"minIODB/pkg/consistenthash"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"
	"minIODB/pkg/retry"

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
	circuitBreaker *retry.CircuitBreaker
	cfg            *config.Config
	mu             sync.RWMutex
	logger         *zap.Logger
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
	strategy string
	cfg      *config.Config
	counter  uint64
}

func NewLoadBalancer(cfg *config.Config) *LoadBalancer {
	strategy := config.DefaultCoordinatorLoadBalanceStrategy
	if cfg != nil && cfg.Coordinator.LoadBalanceStrategy != "" {
		strategy = cfg.Coordinator.LoadBalanceStrategy
	}
	return &LoadBalancer{strategy: strategy, cfg: cfg}
}

func (lb *LoadBalancer) SelectNode(nodes []*discovery.NodeInfo, nodeStatus map[string]*NodeStatus) string {
	if len(nodes) == 0 {
		return ""
	}

	healthyNodes := lb.filterHealthyNodes(nodes, nodeStatus)
	if len(healthyNodes) == 0 {
		healthyNodes = nodes
	}

	switch lb.strategy {
	case "least_load":
		var bestNode *discovery.NodeInfo
		bestLoad := math.MaxFloat64
		for _, n := range healthyNodes {
			if st, ok := nodeStatus[n.ID]; ok {
				if st.Load < bestLoad {
					bestLoad = st.Load
					bestNode = n
				}
			} else {
				if 0 < bestLoad {
					bestLoad = 0
					bestNode = n
				}
			}
		}
		if bestNode != nil {
			return bestNode.ID
		}
	case "random":
		idx := rand.Intn(len(healthyNodes))
		return healthyNodes[idx].ID
	default:
		idx := atomic.AddUint64(&lb.counter, 1) % uint64(len(healthyNodes))
		return healthyNodes[idx].ID
	}

	return healthyNodes[0].ID
}

func (lb *LoadBalancer) filterHealthyNodes(nodes []*discovery.NodeInfo, nodeStatus map[string]*NodeStatus) []*discovery.NodeInfo {
	if lb.cfg == nil {
		return nodes
	}

	maxLoad := lb.cfg.Coordinator.MaxNodeLoad
	if maxLoad <= 0 {
		maxLoad = config.DefaultCoordinatorMaxNodeLoad
	}

	var healthy []*discovery.NodeInfo
	for _, n := range nodes {
		if st, ok := nodeStatus[n.ID]; ok {
			if st.Load < maxLoad {
				healthy = append(healthy, n)
			}
		} else {
			healthy = append(healthy, n)
		}
	}
	return healthy
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

func (hc *HealthChecker) IsNodeHealthy(nodeID string, nodeStatus map[string]*NodeStatus) bool {
	st, exists := nodeStatus[nodeID]
	if !exists {
		return false
	}

	maxLoad := config.DefaultCoordinatorMaxNodeLoad
	if hc.cfg != nil && hc.cfg.Coordinator.MaxNodeLoad > 0 {
		maxLoad = hc.cfg.Coordinator.MaxNodeLoad
	}

	return st.Load < maxLoad
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
	redisPool       *pool.RedisPool
	registry        *discovery.ServiceRegistry
	localQuerier    LocalQuerier
	cfg             *config.Config
	circuitBreaker  *retry.CircuitBreaker
	mu              sync.RWMutex
	activeQueries   map[string]*QueryInfo
	nodeStatus      map[string]*NodeStatus
	loadBalancer    *LoadBalancer
	healthChecker   *HealthChecker
	metrics         *CoordinatorMetrics
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	queryAnalyzer   *QueryAnalyzer   // 查询分析器
	strategyFactory *StrategyFactory // 聚合策略工厂
	logger          *zap.Logger
}

// LocalQuerier 本地查询接口
type LocalQuerier interface {
	ExecuteQuery(sql string) (string, error)
	ExecuteUpdate(sql string) (int64, error)
}

// NewWriteCoordinator 创建写入协调器
func NewWriteCoordinator(registry *discovery.ServiceRegistry, cfg *config.Config, logger *zap.Logger) *WriteCoordinator {
	cb := retry.NewCircuitBreaker("write_coordinator", retry.DefaultCircuitBreakerConfig, logger)

	// 从配置获取哈希环虚拟节点数，默认 150
	hashRingReplicas := 150
	if cfg != nil && cfg.Coordinator.HashRingReplicas > 0 {
		hashRingReplicas = cfg.Coordinator.HashRingReplicas
	}

	return &WriteCoordinator{
		registry:       registry,
		hashRing:       consistenthash.New(hashRingReplicas),
		circuitBreaker: cb,
		cfg:            cfg,
		logger:         logger,
	}
}

// NewQueryCoordinator 创建查询协调器
func NewQueryCoordinator(redisPool *pool.RedisPool, registry *discovery.ServiceRegistry, localQuerier LocalQuerier, cfg *config.Config, logger *zap.Logger) *QueryCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	cb := retry.NewCircuitBreaker("query_coordinator", retry.DefaultCircuitBreakerConfig, logger)

	qc := &QueryCoordinator{
		redisPool:       redisPool,
		registry:        registry,
		localQuerier:    localQuerier,
		cfg:             cfg,
		circuitBreaker:  cb,
		activeQueries:   make(map[string]*QueryInfo),
		nodeStatus:      make(map[string]*NodeStatus),
		loadBalancer:    NewLoadBalancer(cfg),
		healthChecker:   NewHealthChecker(cfg),
		metrics:         NewCoordinatorMetrics(),
		ctx:             ctx,
		cancel:          cancel,
		queryAnalyzer:   NewQueryAnalyzer(),   // 初始化查询分析器
		strategyFactory: NewStrategyFactory(), // 初始化聚合策略工厂
		logger:          logger,
	}

	// 启动监控goroutine
	qc.wg.Add(1)
	go qc.monitorNodes()

	return qc
}

// UpdateRedisPool 热更新 Redis 连接池（用于分布式模式热切换）
func (qc *QueryCoordinator) UpdateRedisPool(newPool *pool.RedisPool) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.redisPool = newPool
	qc.logger.Info("QueryCoordinator Redis pool updated")
}

// RouteWrite 路由写入请求到对应的节点
func (wc *WriteCoordinator) RouteWrite(req *pb.WriteDataRequest) (string, error) {
	// 从配置获取写超时，默认 10 秒
	writeTimeout := 10 * time.Second
	if wc.cfg != nil && wc.cfg.Coordinator.WriteTimeout > 0 {
		writeTimeout = wc.cfg.Coordinator.WriteTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	// 使用熔断器执行操作
	var targetNode string
	var err error

	cbErr := wc.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		// 获取所有活跃节点
		nodes, getErr := wc.registry.GetHealthyNodes()
		if getErr != nil {
			return retry.NewRetryableError(fmt.Errorf("failed to get healthy nodes: %w", getErr))
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
	err = retry.Do(ctx, retry.DefaultConfig, wc.logger, "remote_write", func(ctx context.Context) error {
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
		return retry.NewRetryableError(fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err))
	}
	defer conn.Close()

	client := pb.NewMinIODBServiceClient(conn)
	_, err = client.WriteData(ctx, req)
	if err != nil {
		return retry.NewRetryableError(fmt.Errorf("failed to write to node %s: %w", nodeAddr, err))
	}

	return nil
}

// updateHashRing 更新哈希环
func (wc *WriteCoordinator) updateHashRing(nodes []*discovery.NodeInfo) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	// 从配置获取哈希环虚拟节点数，默认 150
	hashRingReplicas := 150
	if wc.cfg != nil && wc.cfg.Coordinator.HashRingReplicas > 0 {
		hashRingReplicas = wc.cfg.Coordinator.HashRingReplicas
	}

	// 创建新的哈希环
	newHashRing := consistenthash.New(hashRingReplicas)
	for _, node := range nodes {
		nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
		newHashRing.Add(nodeAddr)
	}

	wc.hashRing = newHashRing
}

// ExecuteDistributedQuery 执行分布式查询
func (qc *QueryCoordinator) ExecuteDistributedQuery(sql string) (string, error) {
	// 从配置获取分布式查询超时，默认 30 秒
	queryTimeout := 30 * time.Second
	if qc.cfg != nil && qc.cfg.Coordinator.DistributedQueryTimeout > 0 {
		queryTimeout = qc.cfg.Coordinator.DistributedQueryTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	// 1. 创建查询计划
	plan, err := qc.createQueryPlan(sql)
	if err != nil {
		return "", fmt.Errorf("failed to create query plan: %w", err)
	}

	// 2. 如果不需要分布式查询，直接本地执行
	if !plan.IsDistributed {
		return qc.localQuerier.ExecuteQuery(sql)
	}

	// 3. 执行分布式查询
	return qc.executeDistributedPlan(ctx, plan)
}

// createQueryPlan 创建查询计划
func (qc *QueryCoordinator) createQueryPlan(sql string) (*QueryPlan, error) {
	plan := &QueryPlan{
		SQL:         sql,
		FileMapping: make(map[string][]string),
	}

	// 获取所有健康节点
	nodes, err := qc.registry.GetHealthyNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy nodes: %w", err)
	}

	if len(nodes) <= 1 {
		// 单节点或无节点，使用本地查询
		plan.IsDistributed = false
		return plan, nil
	}

	// 分析SQL，确定需要查询的表和数据分片
	tables, err := qc.extractTablesFromSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to extract tables: %w", err)
	}

	// 获取数据分布信息
	dataDistribution, err := qc.getDataDistribution(tables)
	if err != nil {
		logger.LogWarn(context.Background(), "Failed to get data distribution, falling back to broadcast", zap.Error(err))
		// 回退到广播模式：使用 HealthChecker 过滤不健康节点
		qc.mu.RLock()
		nodeStatusCopy := qc.nodeStatus
		qc.mu.RUnlock()

		for _, node := range nodes {
			nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
			if qc.healthChecker.IsNodeHealthy(node.ID, nodeStatusCopy) {
				plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
			}
		}
		// 如果所有节点都不健康，回退到使用所有节点
		if len(plan.TargetNodes) == 0 {
			for _, node := range nodes {
				nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
				plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
			}
		}
		plan.IsDistributed = true
		return plan, nil
	}

	// 根据数据分布创建查询计划
	for nodeAddr, files := range dataDistribution {
		if len(files) > 0 {
			plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
			plan.FileMapping[nodeAddr] = files
		}
	}

	plan.IsDistributed = len(plan.TargetNodes) > 1

	// 数据分布成功后，使用 HealthChecker 过滤不健康节点
	// 注意：LoadBalancer 不应覆盖数据分布决策，仅在广播模式下使用
	qc.mu.RLock()
	nodeStatusCopy := qc.nodeStatus
	qc.mu.RUnlock()

	// 使用 HealthChecker 过滤不健康节点
	var healthyNodes []string
	for _, nodeAddr := range plan.TargetNodes {
		nodeID := qc.extractNodeIDFromAddr(nodeAddr, nodes)
		if nodeID != "" && qc.healthChecker.IsNodeHealthy(nodeID, nodeStatusCopy) {
			healthyNodes = append(healthyNodes, nodeAddr)
		}
	}
	if len(healthyNodes) > 0 {
		plan.TargetNodes = healthyNodes
	}

	return plan, nil
}

// extractNodeIDFromAddr 从节点地址提取节点ID
func (qc *QueryCoordinator) extractNodeIDFromAddr(addr string, nodes []*discovery.NodeInfo) string {
	for _, node := range nodes {
		nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
		if nodeAddr == addr {
			return node.ID
		}
	}
	return ""
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
				var finalResult QueryResult
				retryErr := retry.Do(ctx, retry.DefaultConfig, qc.logger, "remote_query", func(ctx context.Context) error {
					result, queryErr := qc.executeRemoteQuery(addr, plan.SQL)
					if queryErr != nil {
						return retry.NewRetryableError(queryErr)
					}
					finalResult = QueryResult{NodeID: addr, Data: result}
					return nil
				})
				if retryErr != nil {
					finalResult = QueryResult{NodeID: addr, Error: retryErr.Error()}
				}
				resultChan <- finalResult
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
	return qc.aggregateQueryResults(results, plan.SQL)
}

// executeRemoteQuery 执行远程查询
func (qc *QueryCoordinator) executeRemoteQuery(nodeAddr, sql string) (string, error) {
	// 从配置获取远程查询超时，默认 10 秒
	remoteTimeout := 10 * time.Second
	if qc.cfg != nil && qc.cfg.Coordinator.RemoteQueryTimeout > 0 {
		remoteTimeout = qc.cfg.Coordinator.RemoteQueryTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()

	// 检查是否是本节点
	currentNode := fmt.Sprintf("%s:%s",
		qc.registry.GetNodeInfo().Address,
		qc.registry.GetNodeInfo().Port)

	if nodeAddr == currentNode {
		// 本节点查询，调用本地查询引擎
		return qc.localQuerier.ExecuteQuery(sql)
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
func (qc *QueryCoordinator) extractTablesFromSQL(sql string) ([]string, error) {
	// 使用增强的TableExtractor
	extractor := query.NewTableExtractor()
	tables := extractor.ExtractTableNames(sql)
	return tables, nil
}

// getDataDistribution 获取数据分布信息
func (qc *QueryCoordinator) getDataDistribution(tables []string) (map[string][]string, error) {
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

	ctx := context.Background()

	for _, table := range tables {
		// 从Redis获取该表的文件分布信息
		pattern := fmt.Sprintf("file_index:%s:*", table)
		keys, err := pool.ScanKeys(ctx, qc.redisPool.GetClient(), pattern)
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

// aggregateQueryResults 聚合查询结果
func (qc *QueryCoordinator) aggregateQueryResults(results []QueryResult, sql string) (string, error) {
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
		logger.LogWarn(context.Background(), "Some query nodes failed", zap.Int("error_count", len(errors)), zap.Any("errors", errors))
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
	return qc.mergeResults(successResults, sql)
}

// mergeResults 合并多个查询结果
func (qc *QueryCoordinator) mergeResults(results []string, sql string) (string, error) {
	// 使用查询分析器分析 SQL
	analyzed := qc.queryAnalyzer.Analyze(sql)

	// 获取适合的聚合策略
	strategyName := qc.queryAnalyzer.GetMapReduceStrategy(analyzed)
	strategy := qc.strategyFactory.GetStrategy(strategyName)

	logger.LogDebug(context.Background(), "Using aggregation strategy",
		zap.String("strategy", strategyName),
		zap.Int("result_count", len(results)))

	// 使用策略进行结果聚合
	return strategy.Reduce(results, analyzed)
}

// aggregateCountResults 聚合COUNT查询结果
func (qc *QueryCoordinator) aggregateCountResults(results []string) (string, error) {
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
func (qc *QueryCoordinator) aggregateSumResults(results []string) (string, error) {
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
func (qc *QueryCoordinator) unionResults(results []string) (string, error) {
	var allData []map[string]interface{}

	for _, result := range results {
		// 解析JSON结果
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			logger.LogWarn(context.Background(), "Failed to parse result JSON", zap.Error(err))
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
func (qc *QueryCoordinator) GetQueryStats() map[string]interface{} {
	nodes, err := qc.registry.DiscoverNodes()
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
			qc.refreshNodeStatus()
		}
	}
}

func (qc *QueryCoordinator) refreshNodeStatus() {
	if qc.registry == nil {
		return
	}

	nodes, err := qc.registry.DiscoverNodes()
	if err != nil {
		qc.logger.Debug("Failed to discover nodes", zap.Error(err))
		return
	}

	qc.mu.Lock()
	defer qc.mu.Unlock()

	activeIDs := make(map[string]bool)
	for _, node := range nodes {
		activeIDs[node.NodeID] = true
		status, exists := qc.nodeStatus[node.NodeID]
		if !exists {
			status = &NodeStatus{
				ID:      node.NodeID,
				Address: node.Address,
				Status:  string(node.State),
			}
			qc.nodeStatus[node.NodeID] = status
		}
		status.LastSeen = node.LastSeen
		if node.Metrics != nil {
			status.ActiveQueries = int(node.Metrics.ActiveQueries)
			status.Load = node.Metrics.CPUPercent
		}
	}

	for id := range qc.nodeStatus {
		if !activeIDs[id] {
			delete(qc.nodeStatus, id)
		}
	}
}

// Stop 停止查询协调器
func (qc *QueryCoordinator) Stop() error {
	qc.logger.Info("Stopping query coordinator...")

	// 取消Context
	if qc.cancel != nil {
		qc.cancel()
	}

	// 等待所有goroutine完成
	qc.wg.Wait()

	qc.logger.Info("Query coordinator stopped gracefully")
	return nil
}
