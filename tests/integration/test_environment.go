package integration

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	pb "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"
	"minIODB/internal/mock"
	"minIODB/internal/storage"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestEnvironment 集成测试环境
type TestEnvironment struct {
	config        *config.Config
	mockFactory   *mock.MockFactory
	storage       storage.Storage
	cacheStorage  storage.CacheStorage
	logger        *zap.Logger
	shutdownMutex sync.RWMutex
	isRunning     bool
}

// NewTestEnvironment 创建新的集成测试环境
func NewTestEnvironment() *TestEnvironment {
	// 创建测试配置
	cfg := createTestConfig()

	// 创建Mock工厂
	mockFactory := mock.NewMockFactory()

	// 创建存储层
	storage := &MockStorageAdapter{
		data: make(map[string][]byte),
	}
	cacheStorage := &MockCacheStorageAdapter{
		data: make(map[string][]byte),
	}

	// 创建Logger（使用zap而不是logger包）
	testLogger := zap.NewExample()

	env := &TestEnvironment{
		config:       cfg,
		mockFactory:  mockFactory,
		storage:      storage,
		cacheStorage: cacheStorage,
		logger:       testLogger,
		isRunning:    true,
	}

	return env
}

// Start 启动测试环境
func (env *TestEnvironment) Start(ctx context.Context) error {
	env.shutdownMutex.Lock()
	defer env.shutdownMutex.Unlock()

	if env.isRunning {
		return fmt.Errorf("test environment is already running")
	}

	// 初始化Mock外部服务
	err := env.mockFactory.StartAllServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to start mock services: %v", err)
	}

	// 初始化存储
	err = env.storage.Initialize(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %v", err)
	}

	env.isRunning = true
	log.Println("Test environment started successfully")
	return nil
}

// Shutdown 关闭测试环境
func (env *TestEnvironment) Shutdown(ctx context.Context) error {
	env.shutdownMutex.Lock()
	defer env.shutdownMutex.Unlock()

	if !env.isRunning {
		return fmt.Errorf("test environment is not running")
	}

	// 关闭Mock外部服务
	err := env.mockFactory.StopAllServices(ctx)
	if err != nil {
		log.Printf("Warning: failed to stop mock services: %v", err)
	}

	// 关闭存储
	err = env.storage.Shutdown(ctx)
	if err != nil {
		log.Printf("Warning: failed to shutdown storage: %v", err)
	}

	env.isRunning = false
	log.Println("Test environment shutdown successfully")
	return nil
}

// IsRunning 检查环境是否在运行
func (env *TestEnvironment) IsRunning() bool {
	env.shutdownMutex.RLock()
	defer env.shutdownMutex.RUnlock()
	return env.isRunning
}

// GetConfig 获取配置
func (env *TestEnvironment) GetConfig() *config.Config {
	return env.config
}

// GetMockFactory 获取Mock工厂
func (env *TestEnvironment) GetMockFactory() *mock.MockFactory {
	return env.mockFactory
}

// GetStorage 获取存储
func (env *TestEnvironment) GetStorage() storage.Storage {
	return env.storage
}

// GetCacheStorage 获取缓存存储
func (env *TestEnvironment) GetCacheStorage() storage.CacheStorage {
	return env.cacheStorage
}

// GetLogger 获取Logger
func (env *TestEnvironment) GetLogger() *zap.Logger {
	return env.logger
}

// CreateTestClientInternal 创建测试客户端（内部方法）
func (env *TestEnvironment) CreateTestClientInternal() (*TestClient, error) {
	if !env.IsRunning() {
		return nil, fmt.Errorf("test environment is not running")
	}

	client := &TestClient{
		env: env,
		config: env.GetConfig(),
	}

	return client, nil
}

// ExecuteIntegrationTest 执行集成测试
func (env *TestEnvironment) ExecuteIntegrationTest(t *testing.T, testName string, testFunc func(ctx context.Context, client *TestClient)) {
	t.Run(testName, func(t *testing.T) {
		require.True(t, env.IsRunning(), "Test environment must be running")

		// 创建测试客户端
		client, err := env.CreateTestClient()
		require.NoError(t, err, "Should create test client successfully")
		defer client.Close()

		// 执行测试函数
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testFunc(ctx, client)
	})
}

// createTestConfig 创建测试配置
func createTestConfig() *config.Config {
	return &config.Config{
		NodeID: "test-node-1",
		Redis: config.RedisConfig{
			Mode: "standalone",
			Addr: "localhost:6379",
		},
		MinIO: config.MinIOConfig{
			Endpoint:        "localhost:9000",
			AccessKeyID:     "test-access-key",
			SecretAccessKey: "test-secret-key",
			Bucket:          "test-bucket",
			Region:          "us-east-1",
			SSL:             false,
		},
		DuckDB: config.DuckDBConfig{
			Enabled: true,
			Path:    "/tmp/test-duckdb",
			Options: map[string]interface{}{
				"threads":   2,
				"max_memory": "256MB",
			},
		},
		Server: config.ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		Metadata: config.MetadataConfig{
			Enabled: true,
			BackupInterval: 5 * time.Minute,
		},
		Ingest: config.IngestConfig{
			Enabled:      true,
			BufferSize:   1024 * 1024, // 1MB
			FlushTimeout: 10 * time.Second,
			Compression:  true,
			BatchSize:    1000,
		},
	}
}

// MockStorageAdapter 存储适配器的Mock实现
type MockStorageAdapter struct {
	data map[string][]byte
}

func (m *MockStorageAdapter) Initialize(ctx context.Context) error {
	return nil
}

func (m *MockStorageAdapter) Shutdown(ctx context.Context) error {
	m.data = make(map[string][]byte)
	return nil
}

func (m *MockStorageAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	data, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return data, nil
}

func (m *MockStorageAdapter) Put(ctx context.Context, key string, data []byte) error {
	m.data[key] = make([]byte, len(data))
	copy(m.data[key], data)
	return nil
}

func (m *MockStorageAdapter) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *MockStorageAdapter) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for key := range m.data {
		if len(prefix) == 0 || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (m *MockStorageAdapter) Exists(ctx context.Context, key string) (bool, error) {
	_, exists := m.data[key]
	return exists, nil
}

// MockCacheStorageAdapter 缓存存储适配器的Mock实现
type MockCacheStorageAdapter struct {
	data map[string][]byte
}

func (m *MockCacheStorageAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	data, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return data, nil
}

func (m *MockCacheStorageAdapter) Put(ctx context.Context, key string, data []byte) error {
	m.data[key] = make([]byte, len(data))
	copy(m.data[key], data)
	return nil
}

func (m *MockCacheStorageAdapter) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *MockCacheStorageAdapter) Close() error {
	m.data = make(map[string][]byte)
	return nil
}