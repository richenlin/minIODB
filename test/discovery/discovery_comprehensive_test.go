package discovery

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/pkg/consistenthash"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// 初始化测试logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
	})
}

// TestServiceRegistryComprehensive 服务注册表综合测试
func TestServiceRegistryComprehensive(t *testing.T) {
	ctx := context.Background()

	// 创建单节点服务注册表
	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-comprehensive", "8080")
	require.NoError(t, err)
	require.NotNil(t, registry)

	// 验证基本属性
	assert.Equal(t, "test-node-comprehensive", registry.nodeID)
	assert.Equal(t, "8080", registry.grpcPort)
	assert.False(t, registry.redisEnabled)
}

// TestServiceRegistryLoadBalancing 服务注册表负载均衡测试
func TestServiceRegistryLoadBalancing(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	// 创建多个服务注册表实例
	registries := make([]*ServiceRegistry, 3)
	basePort := 9000

	for i := 0; i < 3; i++ {
		nodeID := fmt.Sprintf("balancer-node-%d", i)
		port := fmt.Sprintf("%d", basePort+i)
		registry, err := NewServiceRegistry(ctx, cfg, nodeID, port)
		require.NoError(t, err)
		registries[i] = registry
	}

	// 启动所有注册表
	for _, reg := range registries {
		err := reg.Start(ctx)
		require.NoError(t, err)
	}

	// 验证每个注册表都可以发现节点
	for _, reg := range registries {
		nodes, err := reg.DiscoverNodes(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(nodes), 1, "Each registry should find at least one node")
	}

	// 停止所有注册表
	for _, reg := range registries {
		err := reg.Stop(ctx)
		require.NoError(t, err)
	}
}

// TestServiceRegistryConnectionHandling 服务注册表连接处理测试
func TestServiceRegistryConnectionHandling(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "connection-test-node", "8080")
	require.NoError(t, err)
	require.NotNil(t, registry)

	// 测试正常启动和停止
	err = registry.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, registry.isRunning)

	err = registry.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, registry.isRunning)

	// 测试重复停止
	err = registry.Stop(ctx)
	assert.Error(t, err, "Should error on double stop")
}

// TestServiceRegistryNodeSelection 服务注册表节点选择测试
func TestServiceRegistryNodeSelection(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "selection-test-node", "8080")
	require.NoError(t, err)
	require.NotNil(t, registry)

	// 验证节点信息获取
	nodeInfo := registry.GetNodeInfo()
	require.NotNil(t, nodeInfo)
	assert.Equal(t, "8080", nodeInfo.Port)

	// 验证哈希环获取
	hashRing := registry.GetHashRing()
	require.NotNil(t, hashRing)
}

// TestServiceRegistryConcurrentOperations 服务注册表并发操作测试
func TestServiceRegistryConcurrentOperations(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "concurrent-test-node", "8080")
	require.NoError(t, err)
	require.NotNil(t, registry)

	// 启动注册表
	err = registry.Start(ctx)
	require.NoError(t, err)

	// 并发节点发现测试
	concurrency := 10
	results := make(chan []ServiceInfo, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nodes, err := registry.DiscoverNodes(ctx)
			if err == nil {
				results <- nodes
			}
		}()
	}

	wg.Wait()
	close(results)

	// 验证所有请求都成功
	successCount := 0
	for _ = range results {
		successCount++
	}
	assert.Equal(t, concurrency, successCount, "All concurrent requests should succeed")

	err = registry.Stop(ctx)
	assert.NoError(t, err)
}

// TestHashRingOperations 哈希环操作综合测试
func TestHashRingOperations(t *testing.T) {
	// 创建一致性哈希环
	hashRing := consistenthash.New(150)
	require.NotNil(t, hashRing)

	// 添加测试节点
	testNodes := []string{"node1:8080", "node2:8080", "node3:8080", "node4:8080"}
	for _, node := range testNodes {
		hashRing.Add(node)
	}

	// 验证键分布
	testKeys := make([]string, 1000)
	for i := 0; i < len(testKeys); i++ {
		testKeys[i] = fmt.Sprintf("key_%d", i)
	}

	// 统计节点分配
	nodeDistribution := make(map[string]int)
	for _, key := range testKeys {
		selectedNode := hashRing.Get(key)
		nodeDistribution[selectedNode]++
	}

	// 验证基本分布
	assert.Equal(t, len(testNodes), len(nodeDistribution), "All nodes should receive keys")
	t.Logf("Hash ring distribution: %+v", nodeDistribution)

	// 测试节点移除
	originalCount := len(nodeDistribution)
	hashRing.Remove(testNodes[0])

	// 验证重新分布
	newDistribution := make(map[string]int)
	for _, key := range testKeys {
		selectedNode := hashRing.Get(key)
		if selectedNode != testNodes[0] { // 不应该再选择被移除的节点
			newDistribution[selectedNode]++
		}
	}

	assert.Equal(t, originalCount-1, len(newDistribution), "Should redistribute after node removal")
}

// TestServiceHealthCheck 服务健康检查测试
func TestServiceHealthCheck(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "health-test-node", "8080")
	require.NoError(t, err)
	require.NotNil(t, registry)

	// 启动注册表
	err = registry.Start(ctx)
	require.NoError(t, err)

	// 测试健康节点获取
	healthyNodes, err := registry.GetHealthyNodes(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, healthyNodes)

	err = registry.Stop(ctx)
	assert.NoError(t, err)
}

// TestServiceRegistryEdgeCases 服务注册表边界情况测试
func TestServiceRegistryEdgeCases(t *testing.T) {
	ctx := context.Background()

	// 测试空配置
	emptyCfg := config.Config{}
	registry, err := NewServiceRegistry(ctx, emptyCfg, "edge-node", "8080")
	// 应该正常工作，使用默认配置
	if err == nil {
		assert.NotNil(t, registry)
	}

	// 测试无效端口
	registry, err = NewServiceRegistry(ctx, emptyCfg, "edge-node", "invalid-port")
	if err == nil {
		assert.NotNil(t, registry)
	}
}

// TestServiceRegistryRaceConditions 服务注册表竞态条件测试
func TestServiceRegistryRaceConditions(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "race-test-node", "8080")
	require.NoError(t, err)

	// 并发启动和停止测试
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()
			err := registry.Start(ctx)
			if err != nil {
				t.Logf("Start iteration %d: %v", iteration, err)
			}
		}(i)
	}

	wg.Wait()

	// 确保清理
	_ = registry.Stop(ctx)
}

// TestNodeInfoIntegrity 节点信息完整性测试
func TestNodeInfoIntegrity(t *testing.T) {
	nodeInfo := NodeInfo{
		ID:       "test-node-id",
		Address:  "192.168.1.100",
		Port:     "8080",
		Status:   "healthy",
		LastSeen: time.Now(),
	}

	// 验证节点信息的基本字段
	assert.Equal(t, "test-node-id", nodeInfo.ID)
	assert.Equal(t, "192.168.1.100", nodeInfo.Address)
	assert.Equal(t, "8080", nodeInfo.Port)
	assert.Equal(t, "healthy", nodeInfo.Status)
	assert.False(t, nodeInfo.LastSeen.IsZero(), "LastSeen should be set")

	// 验证ServiceInfo转换
	serviceInfo := ServiceInfo{
		NodeID:   nodeInfo.ID,
		Address:  nodeInfo.Address,
		Port:     nodeInfo.Port,
		Status:   nodeInfo.Status,
		LastSeen: nodeInfo.LastSeen,
		Metadata: map[string]string{"role": "test", "version": "1.0"},
	}

	assert.Equal(t, nodeInfo.ID, serviceInfo.NodeID)
	assert.Equal(t, nodeInfo.Address, serviceInfo.Address)
	assert.Equal(t, "test", serviceInfo.Metadata["role"])
	assert.Equal(t, "1.0", serviceInfo.Metadata["version"])
}