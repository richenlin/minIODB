package integration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	pb "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestClient 集成测试客户端
type TestClient struct {
	env         *TestEnvironment
	config      *config.Config
	conn         *grpc.ClientConn
	client       pb.MinIODBServiceClient
}

// NewTestClient 创建新的测试客户端（通过环境）
func (env *TestEnvironment) CreateTestClient() (*TestClient, error) {
	if !env.IsRunning() {
		return nil, fmt.Errorf("test environment is not running")
	}

	// 创建gRPC连接
	conn, err := grpc.DialContext(context.Background(),
		fmt.Sprintf("localhost:%s", env.GetConfig().Server.GrpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %v", err)
	}

	client := pb.NewMinIODBServiceClient(conn)

	testClient := &TestClient{
		env:   env,
		config: env.GetConfig(),
		conn:   conn,
		client: client,
	}

	return testClient, nil
}

// Close 关闭客户端和所有资源
func (c *TestClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// GetStatus 获取服务状态
func (c *TestClient) GetStatus(ctx context.Context) (*pb.GetStatusResponse, error) {
	if c.client == nil {
		return nil, errors.New("client is not initialized")
	}

	req := &pb.GetStatusRequest{}
	resp, err := c.client.GetStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetStatus failed: %v", err)
	}

	return resp, nil
}

// HealthCheck 执行健康检查
func (c *TestClient) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return errors.New("client is not initialized")
	}

	req := &pb.HealthCheckRequest{}
	resp, err := c.client.HealthCheck(ctx, req)
	if err != nil {
		return fmt.Errorf("HealthCheck failed: %v", err)
	}

	if resp.Status != "OK" {
		return fmt.Errorf("service is not healthy: %s", resp.Message)
	}

	return nil
}

// CreateTable 创建表
func (c *TestClient) CreateTable(ctx context.Context, tableName string, columns map[string]string) (*pb.CreateTableResponse, error) {
	if c.client == nil {
		return nil, errors.New("client is not initialized")
	}

	// 构建表配置
	properties := make(map[string]string)
	for k, v := range columns {
		properties[k] = v
	}

	config := &pb.TableConfig{
		BufferSize:           1024 * 1024, // 1MB
		FlushIntervalSeconds: 10,
		RetentionDays:      30,
		BackupEnabled:      true,
		Properties:         properties,
	}

	req := &pb.CreateTableRequest{
		TableName:    tableName,
		Config:       config,
		IfNotExists:  true,
	}

	resp, err := c.client.CreateTable(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CreateTable failed: %v", err)
	}

	return resp, nil
}

// WriteData 写入数据
func (c *TestClient) WriteData(ctx context.Context, tableName string, id string, data string) (*pb.WriteDataResponse, error) {
	if c.client == nil {
		return nil, errors.New("client is not initialized")
	}

	// 构建数据负载
	payload, err := stringToStruct(data)
	if err != nil {
		return nil, fmt.Errorf("failed to create payload: %v", err)
	}

	req := &pb.WriteDataRequest{
		Table: tableName,
		Data: &pb.DataRecord{
			Id:        id,
			Timestamp: timestamppb.Now(),
			Payload:    payload,
		},
	}

	resp, err := c.client.WriteData(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("WriteData failed: %v", err)
	}

	return resp, nil
}

// ReadData 读取数据
func (c *TestClient) ReadData(ctx context.Context, tableName string, id string) (*pb.ReadDataResponse, error) {
	if c.client == nil {
		return nil, errors.New("client is not initialized")
	}

	req := &pb.ReadDataRequest{
		Table: tableName,
		Id:    id,
	}

	resp, err := c.client.ReadData(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ReadData failed: %v", err)
	}

	return resp, nil
}

// ListTables 列出表
func (c *TestClient) ListTables(ctx context.Context) (*pb.ListTablesResponse, error) {
	if c.client == nil {
		return nil, errors.New("client is not initialized")
	}

	req := &pb.ListTablesRequest{
		Pattern: "*",
	}

	resp, err := c.client.ListTables(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ListTables failed: %v", err)
	}

	return resp, nil
}

// ExecuteTransaction 执行事务
func (c *TestClient) ExecuteTransaction(ctx context.Context, operations func(ctx context.Context, client *TestClient) error) error {
	if c.client == nil {
		return errors.New("client is not initialized")
	}

	// 这里简化事务处理，实际应该根据系统的事务支持来实现
	// 对于测试目的，我们顺序执行操作
	return operations(ctx, c)
}

// stringToStruct 将字符串转换为protobuf结构
func stringToStruct(data string) (*structpb.Struct, error) {
	return structpb.NewStruct(map[string]interface{}{
		"data": data,
	}), nil
}

// interfaceToStruct 将interface转换为protobuf结构
func interfaceToStruct(data interface{}) (*structpb.Struct, error) {
	switch v := data.(type) {
	case string:
		return stringToStruct(v)
	case map[string]interface{}:
		fields := make(map[string]*structpb.Value)
		for key, value := range v {
			protobufValue, err := interfaceToValue(value)
			if err != nil {
				return nil, fmt.Errorf("failed to convert field %s: %v", key, err)
			}
			fields[key] = protobufValue
		}
		return structpb.NewStruct(fields), nil
	default:
		return nil, fmt.Errorf("unsupported data type: %T", data)
	}
}

// interfaceToValue 将interface转换为protobuf值
func interfaceToValue(value interface{}) (*structpb.Value, error) {
	switch v := value.(type) {
	case nil:
		return structpb.NewNullValue(), nil
	case bool:
		return structpb.NewBoolValue(v), nil
	case string:
		return structpb.NewStringValue(v), nil
	case int:
		return structpb.NewNumberValue(float64(v)), nil
	case int32:
		return structpb.NewNumberValue(float64(v)), nil
	case int64:
		return structpb.NewNumberValue(float64(v)), nil
	case float32:
		return structpb.NewNumberValue(float64(v)), nil
	case float64:
		return structpb.NewNumberValue(v), nil
	case []interface{}:
		listValues := make([]*structpb.Value, 0)
		for _, item := range v {
			itemValue, err := interfaceToValue(item)
			if err != nil {
				return nil, err
			}
			listValues = append(listValues, itemValue)
		}
		return structpb.NewListValue(&structpb.ListValue{Values: listValues}), nil
	case map[string]interface{}:
		mapValues := make(map[string]*structpb.Value)
		for k, item := range v {
			itemValue, err := interfaceToValue(item)
			if err != nil {
				return nil, err
			}
			mapValues[k] = itemValue
		}
		return structpb.NewStructValue(&structpb.Struct{Fields: mapValues}), nil
	default:
		return nil, fmt.Errorf("unsupported value type: %T", value)
	}
}