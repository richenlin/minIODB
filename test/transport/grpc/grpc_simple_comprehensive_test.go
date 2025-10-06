package grpc

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/api/proto/miniodb/v1"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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

// TestWriteRequestProtoBuf write request protobuf structure test
func TestWriteRequestProtoBuf(t *testing.T) {
	// 测试写入请求协议缓冲区结构
	req := &miniodb.WriteDataRequest{
		Table: "test_table",
		Data: &miniodb.DataRecord{
			Id:        "test-id-001",
			Timestamp: timestamppb.New(time.Now()),
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"user_id": {Kind: &structpb.Value_StringValue{StringValue: "user123"}},
					"score":   {Kind: &structpb.Value_NumberValue{NumberValue: 95.5}},
					"active":  {Kind: &structpb.Value_BoolValue{BoolValue: true}},
				},
			},
		},
	}

	// 验证协议缓冲区结构
	assert.NotEmpty(t, req.Table)
	assert.Equal(t, "test_table", req.Table)
	assert.Equal(t, "test-id-001", req.Data.Id)
	assert.NotNil(t, req.Data.Timestamp)
	assert.NotNil(t, req.Data.Payload)
}

// TestQueryRequestProtoBuf query request protobuf structure test
func TestQueryRequestProtoBuf(t *testing.T) {
	// 测试查询请求协议缓冲区结构
	req := &miniodb.QueryDataRequest{
		Sql: "SELECT * FROM users WHERE age > 25",
	}

	// 验证查询请求结构
	assert.NotEmpty(t, req.Sql)
	assert.Contains(t, req.Sql, "SELECT")
	assert.Contains(t, req.Sql, "FROM users")
}

// TestCreateTableRequestProtoBuf create table request protobuf structure test
func TestCreateTableRequestProtoBuf(t *testing.T) {
	req := &miniodb.CreateTableRequest{
		TableName:   "products",
		IfNotExists: true,
	}

	// 验证创建表请求
	assert.Equal(t, "products", req.TableName)
	assert.True(t, req.IfNotExists)
}

// TestListTablesRequestProtoBuf list tables request protobuf structure test
func TestListTablesRequestProtoBuf(t *testing.T) {
	req := &miniodb.ListTablesRequest{
		Limit:   50,
		Offset:  0,
	}

	// 验证列表请求
	assert.Equal(t, int32(50), req.Limit)
	assert.Equal(t, int32(0), req.Offset)
}

// TestDataRecordProtoBuf data record protobuf structure test
func TestDataRecordProtoBuf(t *testing.T) {
	record := &miniodb.DataRecord{
		Id:        "record-12345",
		Timestamp: timestamppb.New(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)),
		Payload: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"type":      {Kind: &structpb.Value_StringValue{StringValue: "user"}},
				"count":     {Kind: &structpb.Value_NumberValue{NumberValue: 42}},
				"processed": {Kind: &structpb.Value_BoolValue{BoolValue: false}},
			},
		},
	}

	// 验证数据记录结构
	assert.Equal(t, "record-12345", record.Id)
	assert.NotNil(t, record.Timestamp)
	assert.NotNil(t, record.Payload)
	assert.Equal(t, "user", record.Payload.Fields["type"].GetStringValue())
}

// TestStreamDataRecordProtoBuf stream data record protobuf structure test
func TestStreamDataRecordProtoBuf(t *testing.T) {
	streamRecord := &miniodb.StreamDataRecord{
		DataId: "stream-001",
		Payload: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"metric": {Kind: &structpb.Value_StringValue{StringValue: "temperature"}},
				"value":  {Kind: &structpb.Value_NumberValue{NumberValue: 23.5}},
			},
		},
	}

	// 验证流数据记录
	assert.Equal(t, "stream-001", streamRecord.DataId)
	assert.Equal(t, "temperature", streamRecord.Payload.Fields["metric"].GetStringValue())
	assert.Equal(t, 23.5, streamRecord.Payload.Fields["value"].GetNumberValue())
}

// TestSecurityConfig 安全配置测试
func TestSecurityConfig(t *testing.T) {
	securityConfig := &config.SecurityConfig{
		Mode:        "none",
		JWTSecret:   "test-secret",
		TokenExpiration: time.Hour * 24,
		Issuer:      "miniodb",
		Audience:    "miniodb-api",
		ValidTokens: []string{"token1", "token2"},
	}

	assert.Equal(t, "none", securityConfig.Mode)
	assert.Equal(t, "test-secret", securityConfig.JWTSecret)
	assert.Equal(t, 24*time.Hour, securityConfig.TokenExpiration)
	assert.Len(t, securityConfig.ValidTokens, 2)
}

// TestResponseStructures response structures test
func TestResponseStructures(t *testing.T) {
	// 测试WriteDataResponse
	writeResp := &miniodb.WriteDataResponse{
		Id:        "record-abc",
		Table:     "test_table",
		Status:    "success",
		Message:   "Data written successfully",
		Timestamp: timestamppb.New(time.Now()),
	}

	assert.Equal(t, "record-abc", writeResp.Id)
	assert.Equal(t, "success", writeResp.Status)
	assert.Equal(t, "Data written successfully", writeResp.Message)

	queryResp := &miniodb.QueryDataResponse{
		ResultJson: `[{"id":"1","name":"record1"}]`,
		Success:    true,
		Message:    "Query executed successfully",
		Count:      1,
		Duration:   250, // milliseconds
	}

	assert.NotEmpty(t, queryResp.ResultJson)
	assert.True(t, queryResp.Success)
	assert.Equal(t, "Query executed successfully", queryResp.Message)
	assert.Equal(t, int64(1), queryResp.Count)
}

// TestErrorResponse error response test
func TestErrorResponse(t *testing.T) {
	// 模拟错误响应
	resp := &miniodb.WriteDataResponse{
		Id:      "failed-record",
		Status:  "error",
		Message: "Validation failed: id cannot be empty",
	}

	assert.Equal(t, "failed-record", resp.Id)
	assert.Equal(t, "error", resp.Status)
	assert.Contains(t, resp.Message, "Validation failed")
}