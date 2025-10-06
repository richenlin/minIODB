package discovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRedisPool is a mock implementation of RedisPool for testing
type MockRedisPool struct {
	mock.Mock
}

func (m *MockRedisPool) GetRedisClient() *redis.Client {
	args := m.Called()
	return args.Get(0).(*redis.Client)
}

func (m *MockRedisPool) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRedisPool) GetStats() interface{} {
	args := m.Called()
	return args.Get(0)
}

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

// MockRedisClient2 is a mock implementation of Redis client for testing
type MockRedisClient2 struct {
	mock.Mock
}

func (m *MockRedisClient2) HSet(ctx context.Context, key string, values ...interface{}) *redis.BoolCmd {
	args := m.Called(ctx, key, values)
	return redis.NewBoolCmd(ctx, args.Bool(0))
}

func (m *MockRedisClient2) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	args := m.Called(ctx, key, value, expiration)
	return redis.NewStatusCmd(ctx, args.String(0))
}

func (m *MockRedisClient2) HDel(ctx context.Context, key string, fields ...string) *redis.IntCmd {
	args := m.Called(ctx, key, fields)
	return redis.NewIntCmd(ctx, int64(args.Int(0)))
}

func (m *MockRedisClient2) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	args := m.Called(ctx, keys)
	return redis.NewIntCmd(ctx, int64(args.Int(0)))
}

func (m *MockRedisClient2) HGetAll(ctx context.Context, key string) *redis.StringStringMapCmd {
	args := m.Called(ctx, key)
	return redis.NewStringStringMapCmd(ctx, args.Get(0).(map[string]string))
}

func (m *MockRedisClient2) Get(ctx context.Context, key string) *redis.StringCmd {
	args := m.Called(ctx, key)
	return redis.NewStringCmd(ctx, args.String(0))
}

func (m *MockRedisClient2) Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd {
	args := m.Called(ctx, key, expiration)
	return redis.NewBoolCmd(ctx, args.Bool(0))
}

func (m *MockRedisClient2) Ping(ctx context.Context) *redis.StatusCmd {
	args := m.Called(ctx)
	return redis.NewStatusCmd(ctx, args.String(0))
}

func (m *MockRedisClient2) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestNewServiceRegistry_SingleNode(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled: false, // Redis disabled
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")

	assert.NoError(t, err, "Should create service registry without Redis")
	assert.NotNil(t, registry, "Registry should not be nil")
	assert.False(t, registry.redisEnabled, "Redis should be disabled")
	assert.Equal(t, "test-node-1", registry.nodeID, "Node ID should match")
	assert.Equal(t, "8080", registry.grpcPort, "gRPC port should match")
}

func TestNewServiceRegistry_WithRedis(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		Redis: config.RedisConfig{
			Enabled:  true,
			Addr:     "localhost:6379",
			Password: "testpass",
			DB:       1,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled:  true,
					Addr:     "localhost:6379",
					Password: "testpass",
					DB:       1,
					PoolSize: 10,
				},
			},
		},
	}

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")

	assert.NoError(t, err, "Should create service registry with Redis")
	assert.NotNil(t, registry, "Registry should not be nil")
	assert.True(t, registry.redisEnabled, "Redis should be enabled")
	assert.NotNil(t, registry.redisPool, "Redis pool should not be nil")
}

func TestServiceRegistry_StartStop_SingleNode(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	// Test start
	err = registry.Start(ctx)
	assert.NoError(t, err, "Start should succeed for single node mode")
	assert.True(t, registry.isRunning, "Registry should be running")

	// Test start when already running
	err = registry.Start(ctx)
	assert.Error(t, err, "Start should fail when already running")

	// Test stop
	err = registry.Stop(ctx)
	assert.NoError(t, err, "Stop should succeed")
	assert.False(t, registry.isRunning, "Registry should not be running")
}

func TestServiceRegistry_DiscoverNodes_SingleNode(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	nodes, err := registry.DiscoverNodes(ctx)
	assert.NoError(t, err, "DiscoverNodes should succeed")
	assert.Len(t, nodes, 1, "Should return exactly one node in single node mode")

	assert.Equal(t, "test-node-1", nodes[0].NodeID)
	assert.Equal(t, "localhost:8080", nodes[0].Address)
	assert.Equal(t, "8080", nodes[0].Port)
	assert.Equal(t, "healthy", nodes[0].Status)
	assert.Equal(t, "single-node", nodes[0].Metadata["mode"])
}

func TestServiceRegistry_GetNodeForKey_SingleNode(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	nodeID := registry.GetNodeForKey("test-key")
	assert.Equal(t, "test-node-1", nodeID, "Should return current node ID in single node mode")
}

func TestServiceRegistry_IsNodeHealthy_SingleNode(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	// Test current node is healthy
	isHealthy := registry.IsNodeHealthy("test-node-1")
	assert.True(t, isHealthy, "Current node should be healthy")

	// Test other node is not healthy
	isHealthy = registry.IsNodeHealthy("other-node")
	assert.False(t, isHealthy, "Other node should not be healthy in single node mode")
}

func TestServiceRegistry_GetHealthyNodes_SingleNode(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	nodes, err := registry.GetHealthyNodes(ctx)
	assert.NoError(t, err, "GetHealthyNodes should succeed")
	assert.Len(t, nodes, 1, "Should return exactly one node")

	assert.Equal(t, "test-node-1", nodes[0].ID)
	assert.Equal(t, "localhost", nodes[0].Address)
	assert.Equal(t, "8080", nodes[0].Port)
	assert.Equal(t, "healthy", nodes[0].Status)
}

func TestServiceRegistry_GetNodeInfo(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	nodeInfo := registry.GetNodeInfo()
	assert.NotNil(t, nodeInfo, "Node info should not be nil")
	assert.Equal(t, "test-node-1", nodeInfo.ID)
	assert.Equal(t, "localhost", nodeInfo.Address)
	assert.Equal(t, "8080", nodeInfo.Port)
	assert.Equal(t, "healthy", nodeInfo.Status)
}

func TestServiceRegistry_GetHashRing(t *testing.T) {
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

	registry, err := NewServiceRegistry(ctx, cfg, "test-node-1", "8080")
	assert.NoError(t, err)

	hashRing := registry.GetHashRing()
	assert.NotNil(t, hashRing, "Hash ring should not be nil")
}

func TestHashString(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"test", 3556498},
		{"hello", 69609650},
		{"world", 113318802},
		{"", 0},
	}

	for _, test := range tests {
		result := hashString(test.input)
		assert.Equal(t, test.expected, result, "Hash should be consistent for input: %s", test.input)
		assert.True(t, result >= 0, "Hash should be non-negative for input: %s", test.input)
	}
}

func TestServiceInfo_JSONSerialization(t *testing.T) {
	serviceInfo := &ServiceInfo{
		NodeID:   "test-node-1",
		Address:  "localhost",
		Port:     "8080",
		Status:   "healthy",
		LastSeen: time.Now(),
		Metadata: map[string]string{
			"mode":   "single-node",
			"region": "us-west",
		},
	}

	// Test JSON serialization
	jsonData, err := json.Marshal(serviceInfo)
	assert.NoError(t, err, "JSON marshaling should succeed")

	// Test JSON deserialization
	var decodedService ServiceInfo
	err = json.Unmarshal(jsonData, &decodedService)
	assert.NoError(t, err, "JSON unmarshaling should succeed")

	assert.Equal(t, serviceInfo.NodeID, decodedService.NodeID)
	assert.Equal(t, serviceInfo.Address, decodedService.Address)
	assert.Equal(t, serviceInfo.Port, decodedService.Port)
	assert.Equal(t, serviceInfo.Status, decodedService.Status)
	assert.Equal(t, serviceInfo.Metadata, decodedService.Metadata)
}

func TestNodeInfo_JSONSerialization(t *testing.T) {
	nodeInfo := &NodeInfo{
		ID:       "test-node-1",
		Address:  "localhost",
		Port:     "8080",
		Status:   "healthy",
		LastSeen: time.Now(),
	}

	// Test JSON serialization
	jsonData, err := json.Marshal(nodeInfo)
	assert.NoError(t, err, "JSON marshaling should succeed")

	// Test JSON deserialization
	var decodedNode NodeInfo
	err = json.Unmarshal(jsonData, &decodedNode)
	assert.NoError(t, err, "JSON unmarshaling should succeed")

	assert.Equal(t, nodeInfo.ID, decodedNode.ID)
	assert.Equal(t, nodeInfo.Address, decodedNode.Address)
	assert.Equal(t, nodeInfo.Port, decodedNode.Port)
	assert.Equal(t, nodeInfo.Status, decodedNode.Status)
}

func TestNewServiceRegistry_ConfigCompatibility(t *testing.T) {
	ctx := context.Background()

	// Test with old Redis config
	cfg1 := config.Config{
		Redis: config.RedisConfig{
			Enabled:  true,
			Addr:     "old-redis:6379",
			Password: "oldpass",
			DB:       1,
			PoolSize: 10,
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled: false, // New config disabled
				},
			},
		},
	}

	registry1, err := NewServiceRegistry(ctx, cfg1, "test-node-1", "8080")
	assert.NoError(t, err)
	assert.True(t, registry1.redisEnabled, "Should use old Redis config when new config is disabled")

	// Test with new Redis config
	cfg2 := config.Config{
		Redis: config.RedisConfig{
			Enabled: false, // Old config disabled
		},
		Network: config.NetworkConfig{
			Pools: config.PoolsConfig{
				Redis: config.EnhancedRedisConfig{
					Enabled:  true,
					Addr:     "new-redis:6379",
					Password: "newpass",
					DB:       2,
					PoolSize: 20,
				},
			},
		},
	}

	registry2, err := NewServiceRegistry(ctx, cfg2, "test-node-1", "8080")
	assert.NoError(t, err)
	assert.True(t, registry2.redisEnabled, "Should use new Redis config when old config is disabled")
}
