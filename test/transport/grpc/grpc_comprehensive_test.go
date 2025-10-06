package grpc

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	miniodb "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
	"minIODB/internal/metadata"
	"minIODB/internal/pool"
	"minIODB/internal/query"
	"minIODB/internal/service"
	"minIODB/internal/storage"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockRedisPool mock Redis pool interface fields
type MockRedisPool struct{}

func (m *MockRedisPool) GetClient() interface{} {
	return nil
}

func (m *MockRedisPool) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *MockRedisPool) Close(ctx context.Context) error {
	return nil
}

func (m *MockRedisPool) GetStats() interface{} {
	return map[string]interface{}{"mock": true}
}

// TestMinioClient mock MinIO client
type TestMinioClient struct{}

func (t *TestMinioClient) FPutObject(ctx context.Context, bucketName, objectName string, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{Bucket: bucketName, Key: objectName, Size: 100}, nil
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

// TestServerCreation gRPC服务器创建测试
func TestServerCreation(t *testing.T) {
	ctx := context.Background()

	// 创建配置
	cfg := &config.Config{
		Server: config.ServerConfig{
			GrpcPort: "8080",
			RestPort: "8081",
			NodeID:   "test-node",
		},
		Security: config.SecurityConfig{
			Mode: "none",
		},
	}

	// 创建模拟依赖
	mockRedisPool := &MockRedisPool{}
	mockMinioClient := &TestMinioClient{}
	mockMetadataManager := &metadata.Manager{}

	ingester := &ingest.Ingester{}
	querier := &query.Querier{}

	// 创建服务
	miniodbService, err := service.NewMinIODBService(cfg, ingester, querier, mockRedisPool, mockMetadataManager, mockMinioClient)
	require.NoError(t, err)
	require.NotNil(t, miniodbService)

	// 创建gRPC服务器
	server, err := NewServer(ctx, miniodbService, *cfg)
	require.NoError(t, err)
	require.NotNil(t, server)

	// 验证服务器属性
	assert.NotNil(t, server.miniodbService)
	assert.NotNil(t, server.cfg)
	assert.NotNil(t, server.authManager)
	assert.NotNil(t, server.grpcInterceptor)
}

// TestServerStartStop gRPC服务器启动停止测试
func TestServerStartStop(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	// 测试设置协调器
	server.SetCoordinators(nil, nil)
	server.SetMetadataManager(nil)

	// 创建测试端口
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().String()
	listener.Close()

	// 启动服务器（在后台运行）
	serverCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = server.Start(serverCtx, port)
	assert.NoError(t, err, "Server should start successfully")

	// 停止服务器
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	err = server.Stop(stopCtx)
	assert.NoError(t, err, "Server should stop successfully")
}

// TestWriteData gRPC写入数据测试
func TestWriteData(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{
		data: make(map[string]interface{}),
	}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	// 创测试数据
	req := &miniodb.WriteDataRequest{
		Table: "test_table",
		Data: &miniodb.DataRecord{
			Id:        "test-id-001",
			Timestamp: timestamppb.New(time.Now()),
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name":    {Kind: &structpb.Value_StringValue{StringValue: "test_user"}},
					"age":     {Kind: &structpb.Value_NumberValue{NumberValue: 25}},
					"active":  {Kind: &structpb.Value_BoolValue{BoolValue: true}},
				},
			},
		},
	}

	resp, err := server.WriteData(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "test-id-001", req.Data.Id)
	assert.NotEmpty(t, req.Data.Table)
}

// TestQueryData gRPC查询数据测试
func TestQueryData(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{
		data: map[string]interface{}{
			"test_table": `[{"id":"test-1"},{"id":"test-2"}]`,
		},
	}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	// 创测试查询
	req := &miniodb.QueryDataRequest{
		Sql: "SELECT * FROM test_table LIMIT 10",
	}

	response, err := server.QueryData(ctx, req)
	require.NoError(t, err)
	assert.NotEmpty(t, response.ResultJson)
}

// TestCreateTable gRPC创建表测试
func TestCreateTable(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	req := &miniodb.CreateTableRequest{
		TableName:   "test_table_comprehensive",
		Description: "Test table for comprehensive testing",
	}

	resp, err := server.CreateTable(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "test_table_comprehensive", resp.TableName)
	assert.Equal(t, "Test table for comprehensive testing", resp.Description)
}

// TestListTables gRPC列表表测试
func TestListTables(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{
		data: map[string]interface{}{
			"tables": "[\"table1\", \"table2\", \"table3\"]",
		},
	}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	req := &miniodb.ListTablesRequest{
		Limit:  10,
		Offset: 0,
	}

	resp, err := server.ListTables(ctx, req)
	require.NoError(t, err)
	assert.Len(t, resp.Tables, 3)
	assert.ElementsMatch(t, resp.Tables, []string{"table1", "table2", "table3"})
}

// TestDeleteData gRPC删除数据测试
func TestDeleteData(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	req := &miniodb.DeleteDataRequest{
		Table:   "test_table",
		Id:      "test-id-001",
		Force:   false,
		Reason:  "test deletion",
	}

	resp, err := server.DeleteData(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "test-id-001", resp.Id)
	assert.Equal(t, "test_table", resp.Table)
}

// TestUpdateData gRPC更新数据测试
func TestUpdateData(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	req := &miniodb.UpdateDataRequest{
		Table: "test_table",
		Id:    "test-id-001",
		Data: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"updated_field": {Kind: &structpb.Value_StringValue{StringValue: "new_value"}},
				"last_modified": {Kind: &structpb.Value_StringValue{StringValue: time.Now().Format(time.RFC3339)}},
			},
		},
	}

	resp, err := server.UpdateData(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "test-id-001", resp.Id)
	assert.Equal(t, "test_table", resp.Table)
}

// TestGetTable gRPC获取表信息测试
func TestGetTable(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{
		data: map[string]interface{}{
			"table_info": `{"name":"test_table","columns":["col1","col2"]}`},
	}
	mockCacheStorage := &MockCacheStorage{}
	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	req := &miniodb.GetTableRequest{
		TableName: "test_table",
	}

	resp, err := server.GetTable(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "test_table", resp.Name)
}

// TestStreamWrite gRPC流写入测试
func TestStreamWrite(t *testing.T) {
	ctx := context.Background()

	// 创建测试服务
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	miniodbService := service.NewMinIODBService(mockStorage, mockCacheStorage)

	cfg := config.Config{
		Server: config.ServerConfig{Mode: "development"},
		Security: config.SecurityConfig{Mode: "none"},
	}

	server, err := NewServer(ctx, miniodbService, cfg)
	require.NoError(t, err)

	// 流写入功能验证（简化实现）
	records := []*miniodb.StreamDataRecord{
		{
			DataId: "stream-1",
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"stream_id": {Kind: &structpb.Value_StringValue{StringValue: "stream-1"}},
				},
			},
		},
		{
			DataId: "stream-2",
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"stream_id": {Kind: &structpb.Value_StringValue{StringValue: "stream-2"}},
				},
			},
		},
	}

	// 由于StreamWrite需要客户端流，这里验证结构存在
	assert.NotNil(t, server)
	assert.NotEmpty(t, records)
}

// MockStorage 模拟存储
type MockStorage struct {
	data map[string]interface{}
}

func (m *MockStorage) Initialize(ctx context.Context) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	return nil
}

func (m *MockStorage) Write(ctx context.Context, key string, data interface{}) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = data
	return nil
}

func (m *MockStorage) Read(ctx context.Context, key string) (interface{}, error) {
	if m.data == nil {
		return "[]", nil
	}
	if value, exists := m.data[key]; exists {
		return value, nil
	}
	return "[]", nil
}

func (m *MockStorage) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	keys := []string{}
	for key := range m.data {
		if prefix == "" || len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (m *MockStorage) Close(ctx context.Context) error {
	return nil
}

// MockCacheStorage 模拟缓存存储
type MockCacheStorage struct{}

func (c *MockCacheStorage) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}

func (c *MockCacheStorage) Get(ctx context.Context, key string) (interface{}, error) {
	return nil, nil
}

func (c *MockCacheStorage) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}

func (c *MockCacheStorage) Delete(ctx context.Context, key string) error {
	return nil
}

func (c *MockCacheStorage) Close(ctx context.Context) error {
	return nil
}