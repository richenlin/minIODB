package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	pb "minIODB/api/proto/olap/v1"
	"minIODB/internal/discovery"
	"minIODB/internal/query"
	"minIODB/internal/utils"
	"minIODB/pkg/consistenthash"

	"github.com/go-redis/redis/v8"
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

// QueryCoordinator 查询协调器
type QueryCoordinator struct {
	redisClient    *redis.Client
	registry       *discovery.ServiceRegistry
	circuitBreaker *utils.CircuitBreaker
	localQuerier   LocalQuerier // 添加本地查询接口
}

// LocalQuerier 本地查询接口
type LocalQuerier interface {
	ExecuteQuery(sql string) (string, error)
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
func NewQueryCoordinator(redisClient *redis.Client, registry *discovery.ServiceRegistry, localQuerier LocalQuerier) *QueryCoordinator {
	cb := utils.NewCircuitBreaker("query_coordinator", utils.DefaultCircuitBreakerConfig)

	return &QueryCoordinator{
		redisClient:    redisClient,
		registry:       registry,
		circuitBreaker: cb,
		localQuerier:   localQuerier,
	}
}

// RouteWrite 路由写入请求到对应的节点
func (wc *WriteCoordinator) RouteWrite(req *pb.WriteRequest) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 使用熔断器执行操作
	var targetNode string
	var err error

	cbErr := wc.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		// 获取所有活跃节点
		nodes, getErr := wc.registry.GetHealthyNodes()
		if getErr != nil {
			return utils.NewRetryableError(fmt.Errorf("failed to get healthy nodes: %w", getErr))
		}

		if len(nodes) == 0 {
			return fmt.Errorf("no healthy nodes available")
		}

		// 更新哈希环
		wc.updateHashRing(nodes)

		// 根据数据ID选择节点
		targetNode = wc.hashRing.Get(req.Id)
		if targetNode == "" {
			return fmt.Errorf("failed to select target node for ID: %s", req.Id)
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
func (wc *WriteCoordinator) sendWriteToNode(ctx context.Context, nodeAddr string, req *pb.WriteRequest) error {
	conn, err := grpc.DialContext(ctx, nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return utils.NewRetryableError(fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err))
	}
	defer conn.Close()

	client := pb.NewOlapServiceClient(conn)
	_, err = client.Write(ctx, req)
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
func (qc *QueryCoordinator) ExecuteDistributedQuery(sql string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		log.Printf("WARN: failed to get data distribution, falling back to broadcast: %v", err)
		// 回退到广播模式
		for _, node := range nodes {
			nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
			plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
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
					result, queryErr := qc.executeRemoteQuery(addr, plan.SQL)
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
	return qc.aggregateQueryResults(results, plan.SQL)
}

// executeRemoteQuery 执行远程查询
func (qc *QueryCoordinator) executeRemoteQuery(nodeAddr, sql string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	client := pb.NewOlapServiceClient(conn)
	req := &pb.QueryRequest{Sql: sql}

	resp, err := client.Query(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to query node %s: %w", nodeAddr, err)
	}

	return resp.ResultJson, nil
}

// extractTablesFromSQL 从SQL中提取表名
func (qc *QueryCoordinator) extractTablesFromSQL(sql string) ([]string, error) {
	// 使用现有的SimpleTableExtractor
	extractor := query.NewSimpleTableExtractor()
	tables := extractor.ExtractTableNames(sql)
	return tables, nil
}

// getDataDistribution 获取数据分布信息
func (qc *QueryCoordinator) getDataDistribution(tables []string) (map[string][]string, error) {
	ctx := context.Background()
	distribution := make(map[string][]string)

	for _, table := range tables {
		// 从Redis获取该表的文件分布信息
		pattern := fmt.Sprintf("file_index:%s:*", table)
		keys, err := qc.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get file index keys for table %s: %w", table, err)
		}

		// 获取每个文件的节点信息
		for _, key := range keys {
			// 获取文件的节点信息
			nodeInfo, err := qc.redisClient.HGet(ctx, key, "node").Result()
			if err != nil {
				// 如果没有节点信息，跳过
				continue
			}

			// 获取文件名
			fileName, err := qc.redisClient.HGet(ctx, key, "file").Result()
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
		log.Printf("Some query nodes failed: %v", errors)
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
	// 分析SQL类型来决定如何合并结果
	lowerSQL := strings.ToLower(sql)

	// 如果是聚合查询（COUNT, SUM, AVG等），需要特殊处理
	if strings.Contains(lowerSQL, "count(") {
		return qc.aggregateCountResults(results)
	}

	if strings.Contains(lowerSQL, "sum(") {
		return qc.aggregateSumResults(results)
	}

	// 对于普通SELECT查询，简单合并所有结果
	return qc.unionResults(results)
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
			log.Printf("WARN: failed to parse result JSON: %v", err)
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
