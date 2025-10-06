package service

import (
	"context"
	"testing"
	"time"

	pb "minIODB/api/proto/miniodb/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestMinIODBServiceBasicOperations 测试MinIODBService基本操作
func TestMinIODBServiceBasicOperations(t *testing.T) {
	// 创建一个空的MinIODBService实例用于测试基本功能
	service := &MinIODBService{}

	// 测试验证功能，这些方法不会引起panic
	validWriteReq := &pb.WriteDataRequest{
		Table: "test_table",
		Data: &pb.DataRecord{
			Id:        "test_id",
			Timestamp: timestamppb.New(time.Now()),
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"data": {Kind: &structpb.Value_StringValue{StringValue: "test"}},
				},
			},
		},
	}

	// 测试写入请求验证 - 这个方法应该不会panic
	err := service.validateWriteRequest(validWriteReq)
	assert.NoError(t, err, "Valid write request should pass validation")

	validQueryReq := &pb.QueryDataRequest{
		Sql: "SELECT * FROM test_table",
	}

	// 测试查询请求验证 - 这个方法应该不会panic
	err = service.validateQueryRequest(validQueryReq)
	assert.NoError(t, err, "Valid query request should pass validation")
}

// TestMinIODBServiceValidation 测试请求验证功能
func TestMinIODBServiceValidation(t *testing.T) {
	service := &MinIODBService{}

	// 测试写入请求验证
	testCases := []struct {
		name    string
		req     *pb.WriteDataRequest
		wantErr bool
	}{
		{
			name: "Valid request",
			req: &pb.WriteDataRequest{
				Table: "test_table",
				Data: &pb.DataRecord{
					Id: "test_id",
					Timestamp: &timestamppb.Timestamp{
						Seconds: time.Now().Unix(),
						Nanos:   0,
					},
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"data": {Kind: &structpb.Value_StringValue{StringValue: "test"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Nil data record",
			req: &pb.WriteDataRequest{
				Table: "test_table",
				Data:  nil,
			},
			wantErr: true,
		},
		{
			name: "Empty ID",
			req: &pb.WriteDataRequest{
				Table: "test_table",
				Data: &pb.DataRecord{
					Id: "",
					Timestamp: &timestamppb.Timestamp{
						Seconds: time.Now().Unix(),
						Nanos:   0,
					},
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"data": {Kind: &structpb.Value_StringValue{StringValue: "test"}},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.validateWriteRequest(tc.req)

			if tc.wantErr {
				assert.Error(t, err, "Validation should fail")
			} else {
				assert.NoError(t, err, "Validation should succeed")
			}
		})
	}
}

// TestMinIODBServiceQueryValidation 测试查询验证
func TestMinIODBServiceQueryValidation(t *testing.T) {
	service := &MinIODBService{}

	testCases := []struct {
		name    string
		req     *pb.QueryDataRequest
		wantErr bool
	}{
		{
			name: "Valid query",
			req: &pb.QueryDataRequest{
				Sql: "SELECT * FROM test_table WHERE id = ?",
			},
			wantErr: false,
		},
		{
			name: "Empty SQL",
			req: &pb.QueryDataRequest{
				Sql: "",
			},
			wantErr: true,
		},
		{
			name: "Nil SQL",
			req: &pb.QueryDataRequest{
				Sql: "",
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.validateQueryRequest(tc.req)

			if tc.wantErr {
				assert.Error(t, err, "Query validation should fail")
			} else {
				assert.NoError(t, err, "Query validation should succeed")
			}
		})
	}
}

// TestMinIODBServiceHelperFunctions 测试辅助函数
func TestMinIODBServiceHelperFunctions(t *testing.T) {
	service := &MinIODBService{}

	// 测试mapToProtobufStruct函数
	testData := map[string]interface{}{
		"id":     123,
		"name":   "test",
		"active": true,
		"score":  95.5,
	}

	ctx := context.Background()
	result, err := service.mapToProtobufStruct(ctx, testData)
	require.NoError(t, err, "mapToProtobufStruct should succeed")
	require.NotNil(t, result, "Result should not be nil")

	// 验证字段映射
	assert.Equal(t, float64(123), result.Fields["id"].GetNumberValue())
	assert.Equal(t, "test", result.Fields["name"].GetStringValue())
	assert.Equal(t, true, result.Fields["active"].GetBoolValue())
	assert.Equal(t, 95.5, result.Fields["score"].GetNumberValue())
}

// BenchmarkMinIODBServiceQueryValidation 基准测试查询验证
func BenchmarkMinIODBServiceQueryValidation(b *testing.B) {
	service := &MinIODBService{}
	req := &pb.QueryDataRequest{
		Sql: "SELECT * FROM test_table WHERE id = ?",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := service.validateQueryRequest(req)
		if err != nil {
			b.Fatalf("Validation failed: %v", err)
		}
	}
}

// TestMinIODBServiceValidationFailures 测试验证失败场景
func TestMinIODBServiceValidationFailures(t *testing.T) {
	service := &MinIODBService{}

	testCases := []struct {
		name    string
		req     *pb.WriteDataRequest
		wantErr bool
	}{
		{
			name: "Empty table name",
			req: &pb.WriteDataRequest{
				Table: "",
				Data: &pb.DataRecord{
					Id:        "test_id",
					Timestamp: timestamppb.New(time.Now()),
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"data": {Kind: &structpb.Value_StringValue{StringValue: "test"}},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Empty ID",
			req: &pb.WriteDataRequest{
				Table: "test_table",
				Data: &pb.DataRecord{
					Id:        "",
					Timestamp: timestamppb.New(time.Now()),
					Payload: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"data": {Kind: &structpb.Value_StringValue{StringValue: "test"}},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Nil data record",
			req: &pb.WriteDataRequest{
				Table: "test_table",
				Data:  nil,
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.validateWriteRequest(tc.req)

			if tc.wantErr {
				assert.Error(t, err, "Validation should fail")
			} else {
				assert.NoError(t, err, "Validation should succeed")
			}
		})
	}
}

// TestMinIODBServiceQueryValidationFailures 测试查询验证失败场景
func TestMinIODBServiceQueryValidationFailures(t *testing.T) {
	service := &MinIODBService{}

	testCases := []struct {
		name    string
		req     *pb.QueryDataRequest
		wantErr bool
	}{
		{
			name: "Empty SQL",
			req: &pb.QueryDataRequest{
				Sql: "",
			},
			wantErr: true,
		},
		{
			name: "Only whitespace SQL",
			req: &pb.QueryDataRequest{
				Sql: "   ",
			},
			wantErr: true,
		},
		{
			name: "Nil SQL",
			req: &pb.QueryDataRequest{
				Sql: "",
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.validateQueryRequest(tc.req)

			if tc.wantErr {
				assert.Error(t, err, "Query validation should fail")
			} else {
				assert.NoError(t, err, "Query validation should succeed")
			}
		})
	}
}

// TestMinIODBServiceInterfaceToProtobufValue 测试interface转protobuf功能
func TestMinIODBServiceInterfaceToProtobufValue(t *testing.T) {
	service := &MinIODBService{}
	ctx := context.Background()

	testCases := []struct {
		name     string
		input    interface{}
		wantType string
	}{
		{
			name:     "String value",
			input:    "test string",
			wantType: "string",
		},
		{
			name:     "Integer value",
			input:    float64(123),
			wantType: "number",
		},
		{
			name:     "Float value",
			input:    45.67,
			wantType: "number",
		},
		{
			name:     "Boolean true",
			input:    true,
			wantType: "bool",
		},
		{
			name:     "Boolean false",
			input:    false,
			wantType: "bool",
		},
		{
			name:     "Nil value",
			input:    nil,
			wantType: "null",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.interfaceToProtobufValue(ctx, tc.input)
			assert.NoError(t, err, "interfaceToProtobufValue should succeed")
			require.NotNil(t, result, "Result should not be nil")

			switch tc.wantType {
			case "string":
				assert.Equal(t, tc.input.(string), result.GetStringValue())
			case "number":
				assert.Equal(t, tc.input.(float64), result.GetNumberValue())
			case "bool":
				assert.Equal(t, tc.input.(bool), result.GetBoolValue())
			case "null":
				assert.NotNil(t, result.GetNullValue())
			}
		})
	}
}
