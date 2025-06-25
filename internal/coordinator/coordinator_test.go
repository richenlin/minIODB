package coordinator

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/discovery"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func setupTestEnvironment(t *testing.T) (*redis.Client, *discovery.ServiceRegistry, func()) {
	// 启动内存Redis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	
	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	// 创建测试配置
	cfg := config.Config{
		Redis: config.RedisConfig{
			Addr:     mr.Addr(),
			Password: "",
			DB:       0,
		},
	}
	
	// 创建服务注册器
	registry, err := discovery.NewServiceRegistry(cfg, "test-node-1", "9001")
	require.NoError(t, err)
	
	// 启动服务注册
	err = registry.Start()
	require.NoError(t, err)
	
	cleanup := func() {
		registry.Stop()
		redisClient.Close()
		mr.Close()
	}
	
	return redisClient, registry, cleanup
}

func TestNewWriteCoordinator(t *testing.T) {
	_, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	wc := NewWriteCoordinator(registry)
	assert.NotNil(t, wc)
	assert.NotNil(t, wc.registry)
}

func TestNewQueryCoordinator(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	assert.NotNil(t, qc)
	assert.Equal(t, redisClient, qc.redisClient)
	assert.Equal(t, registry, qc.registry)
}

func TestWriteCoordinator_RouteWrite(t *testing.T) {
	_, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	wc := NewWriteCoordinator(registry)
	
	payload, _ := structpb.NewStruct(map[string]interface{}{
		"key": "value",
	})
	
	req := &olapv1.WriteRequest{
		Id:        "test-id-123",
		Timestamp: timestamppb.Now(),
		Payload:   payload,
	}
	
	targetNode, err := wc.RouteWrite(req)
	assert.NoError(t, err)
	
	// 应该路由到某个节点（本地或远程）
	assert.True(t, targetNode == "local" || strings.Contains(targetNode, ":"))
}

func TestWriteCoordinator_RouteWrite_LocalNode(t *testing.T) {
	_, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	wc := NewWriteCoordinator(registry)
	
	// 设置当前节点信息使其匹配哈希环中的节点
	nodeInfo := registry.GetNodeInfo()
	targetAddr := fmt.Sprintf("%s:%s", nodeInfo.Address, nodeInfo.Port)
	
	payload, _ := structpb.NewStruct(map[string]interface{}{
		"key": "value",
	})
	
	req := &olapv1.WriteRequest{
		Id:        "test-id-123",
		Timestamp: timestamppb.Now(),
		Payload:   payload,
	}
	
	targetNode, err := wc.RouteWrite(req)
	assert.NoError(t, err)
	assert.True(t, targetNode == "local" || targetNode == targetAddr)
}

func TestWriteCoordinator_RouteWrite_NoNodes(t *testing.T) {
	_, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	wc := NewWriteCoordinator(registry)
	
	// 清空哈希环中的所有节点
	hashRing := registry.GetHashRing()
	nodes := hashRing.GetNodes()
	for _, node := range nodes {
		hashRing.Remove(node)
	}
	
	payload, _ := structpb.NewStruct(map[string]interface{}{
		"key": "value",
	})
	
	req := &olapv1.WriteRequest{
		Id:        "test-id-123",
		Timestamp: timestamppb.Now(),
		Payload:   payload,
	}
	
	_, err := wc.RouteWrite(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no available nodes")
}

func TestQueryCoordinator_parseQueryConditions(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	tests := []struct {
		name     string
		sql      string
		wantIDs  []string
		wantDates int // 期望的日期数量
	}{
		{
			name:     "Simple ID query",
			sql:      "SELECT * FROM table WHERE id = 'test-123'",
			wantIDs:  []string{"test-123"},
			wantDates: 1,
		},
		{
			name:     "Query with timestamp",
			sql:      "SELECT * FROM table WHERE timestamp > '2023-01-01'",
			wantIDs:  []string{"*"},
			wantDates: 7,
		},
		{
			name:     "Query without conditions",
			sql:      "SELECT * FROM table",
			wantIDs:  []string{"*"},
			wantDates: 1,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, dates := qc.parseQueryConditions(tt.sql)
			assert.Equal(t, tt.wantIDs, ids)
			assert.Equal(t, tt.wantDates, len(dates))
		})
	}
}

func TestQueryCoordinator_generateQueryPlan(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	// 定义一个设置测试节点的函数
	setupTestNodes := func() {
		ctx := context.Background()
		
		// 清理现有的注册信息
		redisClient.Del(ctx, "nodes:services")
		redisClient.Del(ctx, "nodes:health:test-node-1")
		
		// 手动注册两个节点到Redis
		node1Data := `{
			"id": "192.168.1.1:9001",
			"address": "192.168.1.1",
			"port": "9001",
			"status": "healthy",
			"last_seen": "2025-06-25T14:20:53+08:00"
		}`
		node2Data := `{
			"id": "192.168.1.2:9001",
			"address": "192.168.1.2",
			"port": "9001",
			"status": "healthy",
			"last_seen": "2025-06-25T14:20:53+08:00"
		}`
		
		// 添加节点到nodes:services (正确的ServiceRegistryKey)
		redisClient.HSet(ctx, "nodes:services", "192.168.1.1:9001", node1Data)
		redisClient.HSet(ctx, "nodes:services", "192.168.1.2:9001", node2Data)
		
		// 设置节点健康状态键
		redisClient.Set(ctx, "nodes:health:192.168.1.1:9001", "healthy", time.Hour)
		redisClient.Set(ctx, "nodes:health:192.168.1.2:9001", "healthy", time.Hour)
		
		// 验证Redis中的数据
		registryContents, _ := redisClient.HGetAll(ctx, "nodes:services").Result()
		t.Logf("Redis nodes:services contents: %+v", registryContents)
		
		// 验证健康状态
		health1, err1 := redisClient.Get(ctx, "nodes:health:192.168.1.1:9001").Result()
		health2, err2 := redisClient.Get(ctx, "nodes:health:192.168.1.2:9001").Result()
		t.Logf("Health check - Node1: %s (err: %v), Node2: %s (err: %v)", health1, err1, health2, err2)
		
		// 手动更新哈希环以包含新添加的节点
		hashRing := registry.GetHashRing()
		// 清空现有节点
		existingNodes := hashRing.GetNodes()
		for _, node := range existingNodes {
			hashRing.Remove(node)
		}
		// 添加新节点
		hashRing.Add("192.168.1.1:9001", "192.168.1.2:9001")
		t.Logf("Hash ring updated with nodes: 192.168.1.1:9001, 192.168.1.2:9001")
	}
	
	tests := []struct {
		name      string
		condition string
		expected  QueryPlan
	}{
		{
			name:      "Specific ID query",
			condition: "SELECT * FROM table WHERE id = 'user:1'",
			expected: QueryPlan{
				IsDistributed: false,
				TargetNodes:   []string{"192.168.1.2:9001"}, // 根据一致性哈希的实际结果修正
			},
		},
		{
			name:      "Query all data",
			condition: "SELECT * FROM table",
			expected: QueryPlan{
				IsDistributed: true,
				TargetNodes:   []string{"192.168.1.1:9001", "192.168.1.2:9001"},
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 在每个子测试前重新设置节点
			setupTestNodes()
			
			// 等待一小段时间确保Redis操作完成
			time.Sleep(100 * time.Millisecond)
			
			// 验证节点发现
			discoveredNodes, err := registry.DiscoverNodes()
			t.Logf("Discovered nodes: %d, error: %v", len(discoveredNodes), err)
			
			for i, node := range discoveredNodes {
				t.Logf("Node %d: %+v", i, node)
			}
			
			// 验证结果
			plan, err := qc.generateQueryPlan(tt.condition)
			assert.NoError(t, err)
			t.Logf("Plan: IsDistributed=%v, TargetNodes=%v", plan.IsDistributed, plan.TargetNodes)
			
			assert.Equal(t, tt.expected.IsDistributed, plan.IsDistributed)
			// 使用ElementsMatch来比较切片内容，不依赖顺序
			assert.ElementsMatch(t, tt.expected.TargetNodes, plan.TargetNodes)
		})
	}
}

func TestQueryCoordinator_buildNodeSpecificQuery(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	originalSQL := "SELECT * FROM table WHERE id = 'test'"
	files := []string{"file1.parquet", "file2.parquet"}
	
	nodeSQL := qc.buildNodeSpecificQuery(originalSQL, files)
	
	expected := "SELECT * FROM read_parquet(['s3://olap-data/file1.parquet','s3://olap-data/file2.parquet']) WHERE id = 'test'"
	assert.Equal(t, expected, nodeSQL)
}

func TestQueryCoordinator_buildNodeSpecificQuery_NoFiles(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	originalSQL := "SELECT * FROM table WHERE id = 'test'"
	files := []string{}
	
	nodeSQL := qc.buildNodeSpecificQuery(originalSQL, files)
	assert.Equal(t, originalSQL, nodeSQL)
}

func TestQueryCoordinator_aggregateResults(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	results := []*QueryResult{
		{
			NodeID: "node1",
			Data:   `[{"id": "1", "value": "a"}]`,
		},
		{
			NodeID: "node2",
			Data:   `[{"id": "2", "value": "b"}]`,
		},
	}
	
	aggregated, err := qc.aggregateResults(results)
	assert.NoError(t, err)
	assert.Contains(t, aggregated, `"id":"1"`)
	assert.Contains(t, aggregated, `"id":"2"`)
}

func TestQueryCoordinator_aggregateResults_WithErrors(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	results := []*QueryResult{
		{
			NodeID: "node1",
			Data:   `[{"id": "1", "value": "a"}]`,
		},
		{
			NodeID: "node2",
			Error:  "connection failed",
		},
	}
	
	aggregated, err := qc.aggregateResults(results)
	assert.NoError(t, err) // 应该成功，因为有一个节点返回了数据
	assert.Contains(t, aggregated, `"id":"1"`)
}

func TestQueryCoordinator_aggregateResults_AllErrors(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	results := []*QueryResult{
		{
			NodeID: "node1",
			Error:  "connection failed",
		},
		{
			NodeID: "node2",
			Error:  "timeout",
		},
	}
	
	_, err := qc.aggregateResults(results)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all nodes failed")
}

func TestQueryCoordinator_GetQueryStats(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	// 添加测试节点
	hashRing := registry.GetHashRing()
	hashRing.Add("192.168.1.1:9001", "192.168.1.2:9001")
	
	stats := qc.GetQueryStats()
	
	// 检查统计信息的字段
	assert.Contains(t, stats, "total_nodes")
	assert.Contains(t, stats, "hash_ring_stats")
	assert.Contains(t, stats, "nodes")
	assert.Equal(t, 3, len(stats)) // 总共3个字段
	
	// 检查节点总数 - 应该是1，因为setupTestEnvironment会注册一个测试节点
	assert.Equal(t, 1, stats["total_nodes"])
}

func TestQueryCoordinator_executeSingleNodeQuery(t *testing.T) {
	redisClient, registry, cleanup := setupTestEnvironment(t)
	defer cleanup()
	
	qc := NewQueryCoordinator(redisClient, registry)
	
	result, err := qc.executeSingleNodeQuery("SELECT * FROM table")
	assert.NoError(t, err)
	assert.Contains(t, result, "single node query not implemented")
} 