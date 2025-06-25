package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/discovery"

	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// QueryPlan 查询计划
type QueryPlan struct {
	SQL          string                    `json:"sql"`
	TargetNodes  []string                  `json:"target_nodes"`
	FileMapping  map[string][]string       `json:"file_mapping"` // node -> files
	IsDistributed bool                     `json:"is_distributed"`
}

// QueryResult 查询结果
type QueryResult struct {
	NodeID string `json:"node_id"`
	Data   string `json:"data"`
	Error  string `json:"error,omitempty"`
}

// WriteCoordinator 写入协调器
type WriteCoordinator struct {
	registry *discovery.ServiceRegistry
}

// QueryCoordinator 查询协调器
type QueryCoordinator struct {
	redisClient *redis.Client
	registry    *discovery.ServiceRegistry
}

// NewWriteCoordinator 创建写入协调器
func NewWriteCoordinator(registry *discovery.ServiceRegistry) *WriteCoordinator {
	return &WriteCoordinator{
		registry: registry,
	}
}

// NewQueryCoordinator 创建查询协调器
func NewQueryCoordinator(redisClient *redis.Client, registry *discovery.ServiceRegistry) *QueryCoordinator {
	return &QueryCoordinator{
		redisClient: redisClient,
		registry:    registry,
	}
}

// RouteWrite 路由写入请求到对应的节点
func (wc *WriteCoordinator) RouteWrite(req *olapv1.WriteRequest) (string, error) {
	// 根据ID计算目标节点
	dataKey := req.Id
	targetNode := wc.registry.GetNodeForKey(dataKey)
	
	if targetNode == "" {
		return "", fmt.Errorf("no available nodes for key: %s", dataKey)
	}
	
	// 如果是本节点，直接返回
	currentNode := fmt.Sprintf("%s:%s", 
		wc.registry.GetNodeInfo().Address, 
		wc.registry.GetNodeInfo().Port)
	
	if targetNode == currentNode {
		return "local", nil
	}
	
	// 转发到目标节点
	err := wc.forwardWriteRequest(targetNode, req)
	if err != nil {
		return "", fmt.Errorf("failed to forward write request to %s: %w", targetNode, err)
	}
	
	return targetNode, nil
}

// forwardWriteRequest 转发写入请求到目标节点
func (wc *WriteCoordinator) forwardWriteRequest(targetNode string, req *olapv1.WriteRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	conn, err := grpc.DialContext(ctx, targetNode, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to node %s: %w", targetNode, err)
	}
	defer conn.Close()
	
	client := olapv1.NewOlapServiceClient(conn)
	_, err = client.Write(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to write to node %s: %w", targetNode, err)
	}
	
	log.Printf("Successfully forwarded write request to node %s", targetNode)
	return nil
}

// ExecuteDistributedQuery 执行分布式查询
func (qc *QueryCoordinator) ExecuteDistributedQuery(sql string) (string, error) {
	// 1. 解析查询，生成查询计划
	plan, err := qc.generateQueryPlan(sql)
	if err != nil {
		return "", fmt.Errorf("failed to generate query plan: %w", err)
	}
	
	if !plan.IsDistributed {
		// 单节点查询，直接执行
		return qc.executeSingleNodeQuery(sql)
	}
	
	// 2. 并发执行分布式查询
	results, err := qc.executeDistributedQueryPlan(plan)
	if err != nil {
		return "", fmt.Errorf("failed to execute distributed query: %w", err)
	}
	
	// 3. 聚合结果
	return qc.aggregateResults(results)
}

// generateQueryPlan 生成查询计划
func (qc *QueryCoordinator) generateQueryPlan(sql string) (*QueryPlan, error) {
	ctx := context.Background()
	
	// 简化的SQL解析 - 实际项目中应该使用专业的SQL解析器
	plan := &QueryPlan{
		SQL:         sql,
		FileMapping: make(map[string][]string),
	}
	
	// 提取查询条件中的ID和时间范围
	ids, dateRange := qc.parseQueryConditions(sql)
	
	// 如果没有指定ID或者是通配符，需要查询所有节点
	if len(ids) == 0 || (len(ids) == 1 && ids[0] == "*") {
		nodes, err := qc.registry.DiscoverNodes()
		if err != nil {
			return nil, fmt.Errorf("failed to discover nodes: %w", err)
		}
		
		for _, node := range nodes {
			nodeAddr := fmt.Sprintf("%s:%s", node.Address, node.Port)
			plan.TargetNodes = append(plan.TargetNodes, nodeAddr)
		}
		plan.IsDistributed = len(plan.TargetNodes) > 1
		return plan, nil
	}
	
	// 根据ID和日期范围获取相关文件
	nodeFileMap := make(map[string][]string)
	targetNodeSet := make(map[string]bool) // 用于跟踪所有目标节点
	
	for _, id := range ids {
		for _, date := range dateRange {
			redisKey := fmt.Sprintf("index:id:%s:%s", id, date)
			files, err := qc.redisClient.SMembers(ctx, redisKey).Result()
			if err != nil && err != redis.Nil {
				log.Printf("WARN: failed to get files for key %s: %v", redisKey, err)
				continue
			}
			
			// 根据一致性哈希确定这些文件应该由哪个节点处理
			targetNode := qc.registry.GetNodeForKey(fmt.Sprintf("%s:%s", id, date))
			if targetNode != "" {
				// 无论是否有文件，都要将节点标记为目标节点
				targetNodeSet[targetNode] = true
				if len(files) > 0 {
					nodeFileMap[targetNode] = append(nodeFileMap[targetNode], files...)
				}
			}
		}
	}
	
	// 构建查询计划 - 包含所有目标节点，无论是否有文件
	for node := range targetNodeSet {
		plan.TargetNodes = append(plan.TargetNodes, node)
		if files, exists := nodeFileMap[node]; exists {
			plan.FileMapping[node] = files
		}
	}
	
	plan.IsDistributed = len(plan.TargetNodes) > 1
	return plan, nil
}

// parseQueryConditions 解析查询条件（简化实现）
func (qc *QueryCoordinator) parseQueryConditions(sql string) ([]string, []string) {
	// 这是一个简化的实现，实际项目中应该使用专业的SQL解析器
	var ids []string
	var dates []string
	
	// 简单的字符串匹配来提取条件
	sqlLower := strings.ToLower(sql)
	
	// 提取ID条件 - 查找 id = 'xxx' 或 id IN ('xxx', 'yyy')
	if strings.Contains(sqlLower, "id =") {
		// 简化处理，假设格式为 id = 'value'
		parts := strings.Split(sql, "'")
		if len(parts) >= 2 {
			ids = append(ids, parts[1])
		}
	}
	
	// 如果没有指定ID，返回空列表（表示查询所有）
	if len(ids) == 0 {
		ids = []string{"*"} // 通配符表示所有ID
	}
	
	// 提取日期范围 - 简化处理
	if strings.Contains(sqlLower, "timestamp") || strings.Contains(sqlLower, "day") {
		// 默认查询最近7天
		now := time.Now()
		for i := 0; i < 7; i++ {
			date := now.AddDate(0, 0, -i).Format("2006-01-02")
			dates = append(dates, date)
		}
	} else {
		// 默认查询今天
		dates = append(dates, time.Now().Format("2006-01-02"))
	}
	
	return ids, dates
}

// executeSingleNodeQuery 执行单节点查询
func (qc *QueryCoordinator) executeSingleNodeQuery(sql string) (string, error) {
	// 在当前节点执行查询
	// 这里应该调用本地的查询引擎
	return `[{"message": "single node query not implemented"}]`, nil
}

// executeDistributedQueryPlan 执行分布式查询计划
func (qc *QueryCoordinator) executeDistributedQueryPlan(plan *QueryPlan) ([]*QueryResult, error) {
	var wg sync.WaitGroup
	results := make([]*QueryResult, len(plan.TargetNodes))
	
	for i, node := range plan.TargetNodes {
		wg.Add(1)
		go func(index int, nodeAddr string) {
			defer wg.Done()
			
			result := &QueryResult{NodeID: nodeAddr}
			
			// 为该节点构建特定的查询
			nodeSQL := qc.buildNodeSpecificQuery(plan.SQL, plan.FileMapping[nodeAddr])
			
			// 执行远程查询
			data, err := qc.executeRemoteQuery(nodeAddr, nodeSQL)
			if err != nil {
				result.Error = err.Error()
				log.Printf("ERROR: query failed on node %s: %v", nodeAddr, err)
			} else {
				result.Data = data
			}
			
			results[index] = result
		}(i, node)
	}
	
	wg.Wait()
	return results, nil
}

// buildNodeSpecificQuery 为特定节点构建查询
func (qc *QueryCoordinator) buildNodeSpecificQuery(originalSQL string, files []string) string {
	if len(files) == 0 {
		return originalSQL
	}
	
	// 构建文件路径列表
	var s3Files []string
	for _, file := range files {
		s3Files = append(s3Files, fmt.Sprintf("s3://olap-data/%s", file))
	}
	
	fileList := "'" + strings.Join(s3Files, "','") + "'"
	
	// 替换表名为具体的文件列表
	nodeSQL := strings.Replace(originalSQL, "FROM table", 
		fmt.Sprintf("FROM read_parquet([%s])", fileList), 1)
	
	return nodeSQL
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
		// 本节点查询，应该调用本地查询引擎
		return `[{"message": "local query not implemented"}]`, nil
	}
	
	// 连接到远程节点
	conn, err := grpc.DialContext(ctx, nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err)
	}
	defer conn.Close()
	
	client := olapv1.NewOlapServiceClient(conn)
	req := &olapv1.QueryRequest{Sql: sql}
	
	resp, err := client.Query(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to query node %s: %w", nodeAddr, err)
	}
	
	return resp.ResultJson, nil
}

// aggregateResults 聚合查询结果
func (qc *QueryCoordinator) aggregateResults(results []*QueryResult) (string, error) {
	var allData []map[string]interface{}
	var errors []string
	
	for _, result := range results {
		if result.Error != "" {
			errors = append(errors, fmt.Sprintf("Node %s: %s", result.NodeID, result.Error))
			continue
		}
		
		// 解析节点返回的JSON数据
		var nodeData []map[string]interface{}
		if err := json.Unmarshal([]byte(result.Data), &nodeData); err != nil {
			errors = append(errors, fmt.Sprintf("Node %s: failed to parse result: %v", result.NodeID, err))
			continue
		}
		
		allData = append(allData, nodeData...)
	}
	
	// 如果有错误但也有成功的结果，记录警告
	if len(errors) > 0 {
		log.Printf("WARN: some nodes failed during query: %v", errors)
	}
	
	// 如果所有节点都失败了
	if len(allData) == 0 && len(errors) > 0 {
		return "", fmt.Errorf("all nodes failed: %v", errors)
	}
	
	// 聚合结果
	aggregatedResult, err := json.Marshal(allData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal aggregated results: %w", err)
	}
	
	return string(aggregatedResult), nil
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
		"total_nodes":    len(nodes),
		"hash_ring_stats": qc.registry.GetHashRing().Stats(),
		"nodes":          nodes,
	}
} 