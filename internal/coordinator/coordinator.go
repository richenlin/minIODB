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
	"minIODB/internal/utils"
	"minIODB/pkg/consistenthash"

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
func NewQueryCoordinator(redisClient *redis.Client, registry *discovery.ServiceRegistry) *QueryCoordinator {
	cb := utils.NewCircuitBreaker("query_coordinator", utils.DefaultCircuitBreakerConfig)
	
	return &QueryCoordinator{
		redisClient:    redisClient,
		registry:       registry,
		circuitBreaker: cb,
	}
}

// RouteWrite 路由写入请求到对应的节点
func (wc *WriteCoordinator) RouteWrite(req *olapv1.WriteRequest) (string, error) {
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
func (wc *WriteCoordinator) sendWriteToNode(ctx context.Context, nodeAddr string, req *olapv1.WriteRequest) error {
	conn, err := grpc.DialContext(ctx, nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return utils.NewRetryableError(fmt.Errorf("failed to connect to node %s: %w", nodeAddr, err))
	}
	defer conn.Close()

	client := olapv1.NewOlapServiceClient(conn)
	_, err = client.Write(ctx, req)
	if err != nil {
		return utils.NewRetryableError(fmt.Errorf("failed to write to node %s: %w", nodeAddr, err))
	}

	return nil
}

// updateHashRing 更新哈希环
func (wc *WriteCoordinator) updateHashRing(nodes []discovery.NodeInfo) {
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

	var results []string
	var err error

	// 使用熔断器执行查询
	cbErr := qc.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		// 获取所有健康节点
		nodes, getErr := qc.registry.GetHealthyNodes()
		if getErr != nil {
			return utils.NewRetryableError(fmt.Errorf("failed to get healthy nodes: %w", getErr))
		}

		if len(nodes) == 0 {
			return fmt.Errorf("no healthy nodes available")
		}

		// 并行查询所有节点
		resultChan := make(chan string, len(nodes))
		errorChan := make(chan error, len(nodes))

		for _, node := range nodes {
			go func(nodeAddr string) {
				// 使用重试机制查询节点
				retryErr := utils.Retry(ctx, utils.DefaultRetryConfig, "remote_query", func(ctx context.Context) error {
					result, queryErr := qc.executeRemoteQuery(nodeAddr, sql)
					if queryErr != nil {
						return utils.NewRetryableError(queryErr)
					}
					resultChan <- result
					return nil
				})
				
				if retryErr != nil {
					errorChan <- retryErr
				}
			}(fmt.Sprintf("%s:%s", node.Address, node.Port))
		}

		// 收集结果
		var errors []error
		for i := 0; i < len(nodes); i++ {
			select {
			case result := <-resultChan:
				results = append(results, result)
			case queryErr := <-errorChan:
				errors = append(errors, queryErr)
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// 如果有部分成功，记录错误但继续
		if len(errors) > 0 {
			log.Printf("Some query nodes failed: %v", errors)
		}

		if len(results) == 0 {
			return fmt.Errorf("all query nodes failed")
		}

		return nil
	})

	if cbErr != nil {
		return "", cbErr
	}

	// 聚合结果
	aggregatedResult, err := qc.aggregateResults(results)
	if err != nil {
		return "", fmt.Errorf("failed to aggregate results: %w", err)
	}

	return aggregatedResult, nil
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
func (qc *QueryCoordinator) aggregateResults(results []string) (string, error) {
	// 简单的结果合并逻辑
	// 在实际应用中，这里需要根据SQL查询类型进行更复杂的聚合
	if len(results) == 0 {
		return "[]", nil
	}
	
	if len(results) == 1 {
		return results[0], nil
	}
	
	// 多个结果的简单合并
	return fmt.Sprintf(`{"aggregated_results": %v}`, results), nil
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