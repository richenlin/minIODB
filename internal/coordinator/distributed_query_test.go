package coordinator

import (
	"encoding/json"
	"testing"

	"minIODB/config"
	"minIODB/internal/discovery"
	"minIODB/pkg/pool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLocalQuerier 模拟本地查询器
type MockLocalQuerier struct {
	mock.Mock
}

func (m *MockLocalQuerier) ExecuteQuery(sql string) (string, error) {
	args := m.Called(sql)
	return args.String(0), args.Error(1)
}

// 为了测试，我们需要创建一个简单的测试环境
func createTestQueryCoordinator() *QueryCoordinator {
	// 创建Redis连接池配置
	cfg := &config.Config{
		Redis: config.RedisConfig{
			Enabled:  true,
			Mode:     "standalone",
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
	}

	// 转换为RedisPoolConfig
	redisPoolConfig := &pool.RedisPoolConfig{
		Mode:     pool.RedisModeStandalone,
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}
	// 使用默认配置填充其他字段
	defaultConfig := pool.DefaultRedisPoolConfig()
	redisPoolConfig.PoolSize = defaultConfig.PoolSize
	redisPoolConfig.MinIdleConns = defaultConfig.MinIdleConns
	redisPoolConfig.MaxConnAge = defaultConfig.MaxConnAge
	redisPoolConfig.PoolTimeout = defaultConfig.PoolTimeout
	redisPoolConfig.IdleTimeout = defaultConfig.IdleTimeout
	redisPoolConfig.IdleCheckFreq = defaultConfig.IdleCheckFreq
	redisPoolConfig.DialTimeout = defaultConfig.DialTimeout
	redisPoolConfig.ReadTimeout = defaultConfig.ReadTimeout
	redisPoolConfig.WriteTimeout = defaultConfig.WriteTimeout
	redisPoolConfig.MaxRetries = defaultConfig.MaxRetries
	redisPoolConfig.MinRetryBackoff = defaultConfig.MinRetryBackoff
	redisPoolConfig.MaxRetryBackoff = defaultConfig.MaxRetryBackoff
	redisPoolConfig.MaxRedirects = defaultConfig.MaxRedirects
	redisPoolConfig.ReadOnly = defaultConfig.ReadOnly
	redisPoolConfig.RouteByLatency = defaultConfig.RouteByLatency
	redisPoolConfig.RouteRandomly = defaultConfig.RouteRandomly

	// 创建Redis连接池（不会真正连接）
	redisPool, _ := pool.NewRedisPool(redisPoolConfig)

	registry := &discovery.ServiceRegistry{}
	localQuerier := &MockLocalQuerier{}

	return NewQueryCoordinator(redisPool, registry, localQuerier, cfg)
}

func TestQueryCoordinator_extractTablesFromSQL(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "Simple SELECT",
			sql:      "SELECT * FROM users",
			expected: []string{"users"},
		},
		{
			name:     "SELECT with JOIN",
			sql:      "SELECT * FROM users JOIN orders ON users.id = orders.user_id",
			expected: []string{"users", "orders"},
		},
		{
			name:     "Complex query with multiple JOINs",
			sql:      "SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id INNER JOIN products ON orders.product_id = products.id",
			expected: []string{"users", "orders", "products"},
		},
		{
			name:     "Case insensitive",
			sql:      "select * from USERS join ORDERS on users.id = orders.user_id",
			expected: []string{"USERS", "ORDERS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc := createTestQueryCoordinator()

			tables, err := qc.extractTablesFromSQL(tt.sql)

			assert.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, tables)
		})
	}
}

func TestQueryCoordinator_aggregateCountResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []string
		expected int
	}{
		{
			name: "Single count result",
			results: []string{
				`[{"count": 10}]`,
			},
			expected: 10,
		},
		{
			name: "Multiple count results",
			results: []string{
				`[{"count": 10}]`,
				`[{"count": 20}]`,
				`[{"count": 5}]`,
			},
			expected: 35,
		},
		{
			name: "Mixed valid and invalid results",
			results: []string{
				`[{"count": 10}]`,
				`invalid json`,
				`[{"count": 15}]`,
			},
			expected: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc := createTestQueryCoordinator()

			result, err := qc.aggregateCountResults(tt.results)

			assert.NoError(t, err)

			// 解析结果
			var data []map[string]interface{}
			err = json.Unmarshal([]byte(result), &data)
			assert.NoError(t, err)
			assert.Len(t, data, 1)

			count, ok := data[0]["count"].(float64)
			assert.True(t, ok)
			assert.Equal(t, float64(tt.expected), count)
		})
	}
}

func TestQueryCoordinator_unionResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []string
		expected int // 期望的记录总数
	}{
		{
			name: "Single result",
			results: []string{
				`[{"id": 1, "name": "user1"}, {"id": 2, "name": "user2"}]`,
			},
			expected: 2,
		},
		{
			name: "Multiple results",
			results: []string{
				`[{"id": 1, "name": "user1"}]`,
				`[{"id": 2, "name": "user2"}]`,
				`[{"id": 3, "name": "user3"}]`,
			},
			expected: 3,
		},
		{
			name: "Mixed valid and invalid results",
			results: []string{
				`[{"id": 1, "name": "user1"}]`,
				`invalid json`,
				`[{"id": 2, "name": "user2"}]`,
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc := createTestQueryCoordinator()

			result, err := qc.unionResults(tt.results)

			assert.NoError(t, err)

			// 解析结果
			var data []map[string]interface{}
			err = json.Unmarshal([]byte(result), &data)
			assert.NoError(t, err)
			assert.Len(t, data, tt.expected)
		})
	}
}

func TestQueryCoordinator_aggregateQueryResults_distributed(t *testing.T) {
	tests := []struct {
		name     string
		results  []QueryResult
		sql      string
		expected string
	}{
		{
			name: "SELECT query - union results",
			results: []QueryResult{
				{NodeID: "node1", Data: `[{"id": 1, "name": "user1"}]`, Error: ""},
				{NodeID: "node2", Data: `[{"id": 2, "name": "user2"}]`, Error: ""},
			},
			sql:      "SELECT * FROM users",
			expected: `[{"id": 1, "name": "user1"}, {"id": 2, "name": "user2"}]`,
		},
		{
			name: "COUNT query - aggregate count",
			results: []QueryResult{
				{NodeID: "node1", Data: `[{"count": 10}]`, Error: ""},
				{NodeID: "node2", Data: `[{"count": 20}]`, Error: ""},
			},
			sql:      "SELECT COUNT(*) FROM users",
			expected: `[{"count": 30}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc := createTestQueryCoordinator()

			result, err := qc.aggregateQueryResults(tt.results, tt.sql)

			assert.NoError(t, err)

			// 解析并比较结果
			var actualData, expectedData []map[string]interface{}
			err = json.Unmarshal([]byte(result), &actualData)
			assert.NoError(t, err)

			err = json.Unmarshal([]byte(tt.expected), &expectedData)
			assert.NoError(t, err)

			assert.Equal(t, expectedData, actualData)
		})
	}
}
