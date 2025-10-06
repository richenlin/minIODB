package ingest

import (
	"context"
	"fmt"
	"testing"
	"time"

	olapv1 "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// 创建测试用的Mock缓冲区
func createMockBuffer(t *testing.T) *buffer.MockConcurrentBuffer {
	mockBuffer := buffer.NewMockConcurrentBuffer()
	return mockBuffer
}

func TestNewIngester(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)

	assert.NotNil(t, ingester)
	assert.NotNil(t, ingester.buffer)
}

func TestIngestData(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	tests := []struct {
		name        string
		req         *olapv1.WriteRequest
		expectError bool
	}{
		{
			name: "成功写入带表名的数据",
			req: &olapv1.WriteRequest{
				Table:     "users",
				Id:        "user-123",
				Timestamp: timestamppb.New(time.Now()),
				Payload: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": structpb.NewStringValue("John Doe"),
						"age":  structpb.NewNumberValue(30),
					},
				},
			},
			expectError: false,
		},
		{
			name: "成功写入无表名的数据(使用默认表名)",
			req: &olapv1.WriteRequest{
				Table:     "",
				Id:        "default-456",
				Timestamp: timestamppb.New(time.Now()),
				Payload: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"value": structpb.NewNumberValue(100),
					},
				},
			},
			expectError: false,
		},
		{
			name: "成功写入包含复杂payload的数据",
			req: &olapv1.WriteRequest{
				Table:     "orders",
				Id:        "order-789",
				Timestamp: timestamppb.New(time.Now()),
				Payload: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"items": structpb.NewListValue(&structpb.ListValue{
							Values: []*structpb.Value{
								structpb.NewStringValue("item1"),
								structpb.NewStringValue("item2"),
							},
						}),
						"total": structpb.NewNumberValue(299.99),
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ingester.IngestData(ctx, tt.req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// 验证数据已添加到缓冲区
				tableName := tt.req.Table
				if tableName == "" {
					tableName = "default"
				}
				bufferedData := ingester.GetBufferedData(ctx, tableName)
				assert.NotEmpty(t, bufferedData, "缓冲区应包含数据")
			}
		})
	}
}

func TestFlushBuffer(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	// 添加一些测试数据
	req := &olapv1.WriteRequest{
		Table:     "test_flush",
		Id:        "flush-123",
		Timestamp: timestamppb.New(time.Now()),
		Payload: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"data": structpb.NewStringValue("test"),
			},
		},
	}

	err := ingester.IngestData(ctx, req)
	require.NoError(t, err)

	// 执行刷新
	err = ingester.FlushBuffer(ctx)
	assert.NoError(t, err)

	// 验证刷新次数增加
	assert.Equal(t, 1, mockBuffer.GetFlushCount())
}

func TestFlushBuffer_NilBuffer(t *testing.T) {
	ingester := &Ingester{buffer: nil}
	ctx := context.Background()

	err := ingester.FlushBuffer(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffer not initialized")
}

func TestGetBufferStats(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	// 获取统计信息
	stats := ingester.GetBufferStats(ctx)
	assert.NotNil(t, stats)
}

func TestGetBufferStats_NilBuffer(t *testing.T) {
	ingester := &Ingester{buffer: nil}
	ctx := context.Background()

	stats := ingester.GetBufferStats(ctx)
	assert.Nil(t, stats)
}

func TestGetBufferedData(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	// 添加测试数据
	tableName := "test_table"
	req := &olapv1.WriteRequest{
		Table:     tableName,
		Id:        "data-123",
		Timestamp: timestamppb.New(time.Now()),
		Payload: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"value": structpb.NewNumberValue(42),
			},
		},
	}

	err := ingester.IngestData(ctx, req)
	require.NoError(t, err)

	// 获取缓冲数据
	data := ingester.GetBufferedData(ctx, tableName)
	assert.NotEmpty(t, data)

	// 验证数据内容
	found := false
	for _, row := range data {
		if row.Table == tableName && row.ID == "data-123" {
			found = true
			assert.Contains(t, row.Payload, "value")
			break
		}
	}
	assert.True(t, found, "应该找到添加的数据")
}

func TestGetBufferedData_NilBuffer(t *testing.T) {
	ingester := &Ingester{buffer: nil}
	ctx := context.Background()

	data := ingester.GetBufferedData(ctx, "test_table")
	assert.Empty(t, data)
}

func TestGetBufferedData_EmptyTable(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	// 获取不存在的表的数据
	data := ingester.GetBufferedData(ctx, "non_existent_table")
	assert.Empty(t, data)
}

func TestGetAllBufferedTables(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	// 添加多个表的数据
	tables := []string{"users", "orders", "products"}
	for _, table := range tables {
		req := &olapv1.WriteRequest{
			Table:     table,
			Id:        "id-" + table,
			Timestamp: timestamppb.New(time.Now()),
			Payload: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"data": structpb.NewStringValue("test"),
				},
			},
		}

		err := ingester.IngestData(ctx, req)
		require.NoError(t, err)
	}

	// 获取所有表名
	allTables := ingester.GetAllBufferedTables(ctx)
	assert.NotEmpty(t, allTables)

	// 验证所有表都被返回
	for _, expected := range tables {
		assert.Contains(t, allTables, expected)
	}
}

func TestGetAllBufferedTables_NilBuffer(t *testing.T) {
	ingester := &Ingester{buffer: nil}
	ctx := context.Background()

	tables := ingester.GetAllBufferedTables(ctx)
	assert.Empty(t, tables)
}

func TestGetAllBufferedTables_EmptyBuffer(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	tables := ingester.GetAllBufferedTables(ctx)
	assert.Empty(t, tables)
}

// 并发测试
func TestIngestData_Concurrent(t *testing.T) {
	mockBuffer := createMockBuffer(t)
	defer mockBuffer.Stop(context.Background())

	ingester := NewIngester(mockBuffer)
	ctx := context.Background()

	// 并发写入数据
	concurrentWrites := 100
	done := make(chan bool, concurrentWrites)

	for i := 0; i < concurrentWrites; i++ {
		go func(index int) {
			req := &olapv1.WriteRequest{
				Table:     "concurrent_test",
				Id:        fmt.Sprintf("id-%d", index),
				Timestamp: timestamppb.New(time.Now()),
				Payload: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"index": structpb.NewNumberValue(float64(index)),
					},
				},
			}

			err := ingester.IngestData(ctx, req)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// 等待所有写入完成
	for i := 0; i < concurrentWrites; i++ {
		<-done
	}

	// 验证数据完整性
	data := ingester.GetBufferedData(ctx, "concurrent_test")
	assert.NotEmpty(t, data)
}
