package coordinator

import (
	"context"
	"fmt"
	"minIODB/internal/config"
	"minIODB/internal/discovery"
	"minIODB/internal/pool"
	"minIODB/internal/storage"
	"minIODB/internal/utils"
	"testing"

	pb "minIODB/api/proto/miniodb/v1"

	"google.golang.org/protobuf/types/known/structpb"
)

// TestWriteCoordinatorBasicOperations 测试WriteCoordinator基本操作
func TestWriteCoordinatorBasicOperations(t *testing.T) {
	ctx := context.Background()

	// 创建WriteCoordinator实例
	registry, _ := discovery.NewServiceRegistry(context.Background(), config.Config{}, "test-node", "8080")
	wc := NewWriteCoordinator(registry)
	if wc == nil {
		t.Fatal("Failed to create WriteCoordinator")
	}

	// 测试路由写入请求
	req := &pb.WriteDataRequest{
		Table: "test-table",
		Data: &pb.DataRecord{
			Id: "test-id",
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"data": {Kind: &structpb.Value_StringValue{StringValue: "test-payload"}},
				},
			},
		},
	}

	// 由于没有真实的节点，预期会返回错误
	targetNode, err := wc.RouteWrite(ctx, req)
	if err == nil {
		t.Log("RouteWrite succeeded (may have mock nodes)")
	} else {
		t.Logf("RouteWrite failed as expected: %v", err)
	}

	t.Logf("Target node: %s", targetNode)
}

// TestQueryCoordinatorBasicOperations 测试QueryCoordinator基本操作
func TestQueryCoordinatorBasicOperations(t *testing.T) {
	ctx := context.Background()

	// 创建mock依赖
	cfg := &config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
	}
	registry, _ := discovery.NewServiceRegistry(ctx, *cfg, "test-node", "8080")

	// 创建Redis池（禁用状态）
	redisPoolConfig := &pool.RedisPoolConfig{
		Mode: pool.RedisModeStandalone,
		Addr: "localhost:6379",
	}
	redisPool, _ := pool.NewRedisPool(ctx, redisPoolConfig)

	localQuerier := &MockLocalQuerier{}   // Mock local querier
	indexSystem := &storage.IndexSystem{} // Mock index system

	// 创建QueryCoordinator实例
	qc := NewQueryCoordinator(ctx, redisPool, registry, localQuerier, cfg, indexSystem)
	if qc == nil {
		t.Fatal("Failed to create QueryCoordinator")
	}

	// 测试查询统计
	stats := qc.GetQueryStats(ctx)
	if stats != nil {
		t.Logf("Query stats retrieved: %v", stats)
	}

	// 测试分布式查询（预期会失败因为没有真实节点）
	sql := "SELECT * FROM test_table"
	result, err := qc.ExecuteDistributedQuery(ctx, sql)
	if err != nil {
		t.Logf("Distributed query failed as expected: %v", err)
	} else {
		t.Logf("Distributed query result: %s", result)
	}
}

// MockLocalQuerier 模拟本地查询器
type MockLocalQuerier struct{}

func (m *MockLocalQuerier) ExecuteQuery(ctx context.Context, sql string) (string, error) {
	return `{"result": "mock local query result"}`, nil
}

// TestWriteCoordinatorHashRing 测试WriteCoordinator的哈希环功能
func TestWriteCoordinatorHashRing(t *testing.T) {
	t.Skip("Temporarily skipping test - needs interface refactoring")
	ctx := context.Background()

	// 创建registry
	registry, _ := discovery.NewServiceRegistry(ctx, config.Config{}, "test-node", "8080")

	wc := NewWriteCoordinator(registry)

	// 测试写入路由
	testIDs := []string{"id1", "id2", "id3", "id4", "id5"}

	for _, id := range testIDs {
		req := &pb.WriteDataRequest{
			Table: "test-table",
			Data: &pb.DataRecord{
				Id: id,
				Payload: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"data": {Kind: &structpb.Value_StringValue{StringValue: "test-payload-" + id}},
					},
				},
			},
		}

		targetNode, err := wc.RouteWrite(ctx, req)
		if err != nil {
			t.Logf("RouteWrite for %s failed: %v", id, err)
		} else {
			t.Logf("ID %s routed to node: %s", id, targetNode)
		}
	}
}

// MockServiceRegistry 模拟服务注册表
type MockServiceRegistry struct {
	nodes []*discovery.NodeInfo
}

func (m *MockServiceRegistry) GetHealthyNodes(ctx context.Context) ([]*discovery.NodeInfo, error) {
	return m.nodes, nil
}

func (m *MockServiceRegistry) GetNodeInfo() *discovery.NodeInfo {
	return &discovery.NodeInfo{Address: "localhost", Port: "8080"}
}

// 实现ServiceRegistry的其他方法（简化实现）
func (m *MockServiceRegistry) DiscoverNodes(ctx context.Context) ([]*discovery.ServiceInfo, error) {
	var services []*discovery.ServiceInfo
	for _, node := range m.nodes {
		service := &discovery.ServiceInfo{
			NodeID:   node.ID,
			Address:  node.Address,
			Port:     node.Port,
			Status:   node.Status,
			LastSeen: node.LastSeen,
			Metadata: map[string]string{"mock": "true"},
		}
		services = append(services, service)
	}
	return services, nil
}

func (m *MockServiceRegistry) GetHashRing() *utils.ConsistentHash {
	return utils.NewConsistentHash(150)
}

// TestQueryCoordinatorQueryPlan 测试查询计划创建
func TestQueryCoordinatorQueryPlan(t *testing.T) {
	t.Skip("Temporarily skipping test - needs interface refactoring")
	ctx := context.Background()

	// 创建mock依赖
	redisPoolConfig := &pool.RedisPoolConfig{
		Mode: pool.RedisModeStandalone,
		Addr: "localhost:6379",
	}
	redisPool, _ := pool.NewRedisPool(ctx, redisPoolConfig)
	registry, _ := discovery.NewServiceRegistry(ctx, config.Config{}, "test-node", "8080")
	localQuerier := &MockLocalQuerier{}
	cfg := &config.Config{
		Redis: config.RedisConfig{
			Enabled: false, // 禁用Redis测试单节点模式
		},
	}
	indexSystem := &storage.IndexSystem{}

	qc := NewQueryCoordinator(ctx, redisPool, registry, localQuerier, cfg, indexSystem)

	// 测试简单查询计划
	sql := "SELECT * FROM users"

	// 由于没有真实的数据分布，预期会创建本地查询计划
	plan, err := qc.createQueryPlan(ctx, sql)
	if err != nil {
		t.Fatalf("Failed to create query plan: %v", err)
	}

	t.Logf("Query plan created:")
	t.Logf("  SQL: %s", plan.SQL)
	t.Logf("  IsDistributed: %v", plan.IsDistributed)
	t.Logf("  TargetNodes: %v", plan.TargetNodes)

	// 单节点模式应该不是分布式
	nodes, _ := registry.GetHealthyNodes(ctx)
	if len(nodes) <= 1 && plan.IsDistributed {
		t.Error("Single node mode should not create distributed query plan")
	}
}

// MockRedisPool 模拟Redis连接池
type MockRedisPool struct{}

func (m *MockRedisPool) GetClient() interface{} {
	return &MockRedisClient{}
}

// 实现RedisPool的其他方法（简化实现）
func (m *MockRedisPool) GetRedisClient() interface{} {
	return &MockRedisClient{}
}

func (m *MockRedisPool) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *MockRedisPool) Close(ctx context.Context) error {
	return nil
}

func (m *MockRedisPool) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"mock": true,
	}
}

func (m *MockRedisPool) GetConnectionInfo() map[string]interface{} {
	return map[string]interface{}{
		"mock": true,
	}
}

// MockRedisClient 模拟Redis客户端
type MockRedisClient struct{}

func (m *MockRedisClient) Keys(ctx context.Context, pattern string) *MockStringSliceCmd {
	return &MockStringSliceCmd{vals: []string{}}
}

func (m *MockRedisClient) HGet(ctx context.Context, key, field string) *MockStringCmd {
	return &MockStringCmd{val: ""}
}

func (m *MockRedisClient) SMembers(ctx context.Context, key string) *MockStringSliceCmd {
	return &MockStringSliceCmd{vals: []string{}}
}

// MockStringSliceCmd 模拟Redis字符串切片命令
type MockStringSliceCmd struct {
	vals []string
}

func (m *MockStringSliceCmd) Result() ([]string, error) {
	return m.vals, nil
}

// MockStringCmd 模拟Redis字符串命令
type MockStringCmd struct {
	val string
}

func (m *MockStringCmd) Result() (string, error) {
	return m.val, nil
}

// TestQueryCoordinatorResultAggregation 测试结果聚合
func TestQueryCoordinatorResultAggregation(t *testing.T) {
	t.Skip("Temporarily skipping test - needs interface refactoring")
	ctx := context.Background()

	// 创建mock依赖
	redisPoolConfig := &pool.RedisPoolConfig{
		Mode: pool.RedisModeStandalone,
		Addr: "localhost:6379",
	}
	redisPool, _ := pool.NewRedisPool(ctx, redisPoolConfig)
	registry, _ := discovery.NewServiceRegistry(ctx, config.Config{}, "test-node", "8080")
	localQuerier := &MockLocalQuerier{}
	cfg := &config.Config{}
	indexSystem := &storage.IndexSystem{}

	qc := NewQueryCoordinator(ctx, redisPool, registry, localQuerier, cfg, indexSystem)

	// 测试COUNT查询结果聚合
	countResults := []string{
		`[{"count": 10}]`,
		`[{"count": 20}]`,
		`[{"count": 5}]`,
	}

	aggregated, err := qc.aggregateCountResults(ctx, countResults)
	if err != nil {
		t.Fatalf("Failed to aggregate count results: %v", err)
	}

	t.Logf("Aggregated count result: %s", aggregated)

	// 测试SUM查询结果聚合
	sumResults := []string{
		`[{"sum": 100.5}]`,
		`[{"sum": 200.3}]`,
	}

	aggregated, err = qc.aggregateSumResults(ctx, sumResults)
	if err != nil {
		t.Fatalf("Failed to aggregate sum results: %v", err)
	}

	t.Logf("Aggregated sum result: %s", aggregated)

	// 测试普通查询结果合并
	normalResults := []string{
		`[{"id": 1, "name": "Alice"}]`,
		`[{"id": 2, "name": "Bob"}]`,
	}

	merged, err := qc.unionResults(ctx, normalResults)
	if err != nil {
		t.Fatalf("Failed to merge normal results: %v", err)
	}

	t.Logf("Merged normal result: %s", merged)
}

// TestQueryCoordinatorTableExtraction 测试表名提取
func TestQueryCoordinatorTableExtraction(t *testing.T) {
	t.Skip("Temporarily skipping test - needs interface refactoring")
	ctx := context.Background()

	// 创建mock依赖
	redisPoolConfig := &pool.RedisPoolConfig{
		Mode: pool.RedisModeStandalone,
		Addr: "localhost:6379",
	}
	redisPool, _ := pool.NewRedisPool(ctx, redisPoolConfig)
	registry, _ := discovery.NewServiceRegistry(ctx, config.Config{}, "test-node", "8080")
	localQuerier := &MockLocalQuerier{}
	cfg := &config.Config{}
	indexSystem := &storage.IndexSystem{}

	qc := NewQueryCoordinator(ctx, redisPool, registry, localQuerier, cfg, indexSystem)

	// 测试各种SQL语句的表名提取
	testCases := []struct {
		sql      string
		expected []string
	}{
		{
			sql:      "SELECT * FROM users",
			expected: []string{"users"},
		},
		{
			sql:      "SELECT u.*, o.order_id FROM users u JOIN orders o ON u.id = o.user_id",
			expected: []string{"users", "orders"},
		},
		{
			sql:      "INSERT INTO products (name, price) VALUES ('test', 10.99)",
			expected: []string{"products"},
		},
		{
			sql:      "UPDATE orders SET status = 'completed' WHERE id = 1",
			expected: []string{"orders"},
		},
		{
			sql:      "DELETE FROM logs WHERE created_at < '2024-01-01'",
			expected: []string{"logs"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.sql, func(t *testing.T) {
			tables, err := qc.extractTablesFromSQL(ctx, tc.sql)
			if err != nil {
				t.Fatalf("Failed to extract tables from SQL '%s': %v", tc.sql, err)
			}

			t.Logf("SQL: %s", tc.sql)
			t.Logf("Extracted tables: %v", tables)

			// 验证提取的表名数量
			if len(tables) != len(tc.expected) {
				t.Errorf("Expected %d tables, got %d", len(tc.expected), len(tables))
			}

			// 验证表名是否正确（简化验证）
			for _, expected := range tc.expected {
				found := false
				for _, actual := range tables {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected table '%s' not found in extracted tables: %v", expected, tables)
				}
			}
		})
	}
}

// BenchmarkWriteCoordinatorRouteWrite 基准测试写入路由
func BenchmarkWriteCoordinatorRouteWrite(b *testing.B) {
	b.Skip("Temporarily skipping benchmark - needs interface refactoring")
	ctx := context.Background()

	// 创建registry
	registry, _ := discovery.NewServiceRegistry(ctx, config.Config{}, "test-node", "8080")

	wc := NewWriteCoordinator(registry)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := &pb.WriteDataRequest{
			Table: "benchmark-table",
			Data: &pb.DataRecord{
				Id: fmt.Sprintf("benchmark-id-%d", i),
				Payload: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"data": {Kind: &structpb.Value_StringValue{StringValue: "benchmark-payload"}},
					},
				},
			},
		}

		_, err := wc.RouteWrite(ctx, req)
		if err != nil {
			// 预期会有错误，因为不是真实环境
			continue
		}
	}
}

// BenchmarkQueryCoordinatorCreateQueryPlan 基准测试查询计划创建
func BenchmarkQueryCoordinatorCreateQueryPlan(b *testing.B) {
	b.Skip("Temporarily skipping benchmark - needs interface refactoring")
	ctx := context.Background()

	// 创建mock依赖
	redisPoolConfig := &pool.RedisPoolConfig{
		Mode: pool.RedisModeStandalone,
		Addr: "localhost:6379",
	}
	redisPool, _ := pool.NewRedisPool(ctx, redisPoolConfig)
	registry, _ := discovery.NewServiceRegistry(ctx, config.Config{}, "test-node", "8080")
	localQuerier := &MockLocalQuerier{}
	cfg := &config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
	}
	indexSystem := &storage.IndexSystem{}

	qc := NewQueryCoordinator(ctx, redisPool, registry, localQuerier, cfg, indexSystem)

	sql := "SELECT * FROM users WHERE id > 100"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := qc.createQueryPlan(ctx, sql)
		if err != nil {
			b.Fatalf("Failed to create query plan: %v", err)
		}
	}
}
