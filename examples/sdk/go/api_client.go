// MinIODB Go gRPC 客户端示例
//
// 本示例展示如何使用 gRPC 调用 MinIODB 的各种 API
// 包括数据操作、表管理、流式操作等
//
// 使用方法:
//
//	go run api_client.go -addr localhost:8080
//
// 需要先编译 proto 文件:
//
//	cd api/proto/miniodb/v1
//	protoc --go_out=. --go-grpc_out=. miniodb.proto
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"minIODB/pkg/logger"
	"os"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	miniodb "minIODB/api/proto/miniodb/v1"
)

// MinioDBClient MinIODB gRPC 客户端封装
type MinioDBClient struct {
	conn   *grpc.ClientConn
	client miniodb.MinIODBServiceClient
	addr   string
}

// NewMinioDBClient 创建新的 MinIODB 客户端
func NewMinioDBClient(addr string) (*MinioDBClient, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MinIODB server: %w", err)
	}

	return &MinioDBClient{
		conn:   conn,
		client: miniodb.NewMinIODBServiceClient(conn),
		addr:   addr,
	}, nil
}

// Close 关闭连接
func (c *MinioDBClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ===============================================
// 数据操作
// ===============================================

// WriteData 写入数据
func (c *MinioDBClient) WriteData(ctx context.Context, table string, id string, payload map[string]interface{}) error {
	payloadStruct, err := structpb.NewStruct(payload)
	if err != nil {
		return fmt.Errorf("failed to create payload struct: %w", err)
	}

	req := &miniodb.WriteDataRequest{
		Table: table,
		Data: &miniodb.DataRecord{
			Id:        id,
			Timestamp: timestamppb.Now(),
			Payload:   payloadStruct,
		},
	}

	resp, err := c.client.WriteData(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("write failed: %s", resp.Message)
	}

	logger.Logger.Info("Data written successfully to table", zap.String("table", table), zap.String("id", id), zap.String("node", resp.NodeId))
	return nil
}

// QueryData 查询数据
func (c *MinioDBClient) QueryData(ctx context.Context, sql string, limit int32) (map[string]interface{}, error) {
	req := &miniodb.QueryDataRequest{
		Sql:   sql,
		Limit: limit,
	}

	resp, err := c.client.QueryData(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query data: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(resp.ResultJson), &result); err != nil {
		return nil, fmt.Errorf("failed to parse query result: %w", err)
	}

	logger.Logger.Info("Query executed successfully", zap.Bool("has_more", resp.HasMore))
	return result, nil
}

// UpdateData 更新数据
func (c *MinioDBClient) UpdateData(ctx context.Context, table string, id string, payload map[string]interface{}) error {
	payloadStruct, err := structpb.NewStruct(payload)
	if err != nil {
		return fmt.Errorf("failed to create payload struct: %w", err)
	}

	req := &miniodb.UpdateDataRequest{
		Table:     table,
		Id:        id,
		Timestamp: timestamppb.Now(),
		Payload:   payloadStruct,
	}

	resp, err := c.client.UpdateData(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to update data: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("update failed: %s", resp.Message)
	}

	logger.Logger.Info("Data updated successfully", zap.String("id", id))
	return nil
}

// DeleteData 删除数据
func (c *MinioDBClient) DeleteData(ctx context.Context, table string, id string) (int32, error) {
	req := &miniodb.DeleteDataRequest{
		Table:      table,
		Id:         id,
		SoftDelete: false,
	}

	resp, err := c.client.DeleteData(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to delete data: %w", err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("delete failed: %s", resp.Message)
	}

	logger.Logger.Info("Data deleted successfully", zap.Int32("deleted_count", resp.DeletedCount))
	return resp.DeletedCount, nil
}

// ===============================================
// 流式操作
// ===============================================

// StreamWrite 批量流式写入
func (c *MinioDBClient) StreamWrite(ctx context.Context, table string, records []*miniodb.DataRecord) (int64, error) {
	stream, err := c.client.StreamWrite(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to create stream: %w", err)
	}

	// 分批发送
	batchSize := 100
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batchReq := &miniodb.StreamWriteRequest{
			Table:   table,
			Records: records[i:end],
		}

		if err := stream.Send(batchReq); err != nil {
			return 0, fmt.Errorf("failed to send batch: %w", err)
		}

		logger.Logger.Info("Sent batch", zap.Int("start", i), zap.Int("end", end-1))
	}

	// 获取最终响应
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return 0, fmt.Errorf("failed to receive response: %w", err)
	}

	logger.Logger.Info("Stream write completed", zap.Int64("records_count", resp.RecordsCount), zap.Int("errors_count", len(resp.Errors)))
	return resp.RecordsCount, nil
}

// StreamQuery 流式查询
func (c *MinioDBClient) StreamQuery(ctx context.Context, sql string, batchSize int32) ([]*miniodb.DataRecord, error) {
	req := &miniodb.StreamQueryRequest{
		Sql:       sql,
		BatchSize: batchSize,
		Cursor:    "",
	}

	stream, err := c.client.StreamQuery(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream query: %w", err)
	}

	var allRecords []*miniodb.DataRecord
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}

		allRecords = append(allRecords, resp.Records...)
		logger.Logger.Info("Received batch", zap.Int("records_count", len(resp.Records)), zap.Bool("has_more", resp.HasMore), zap.String("cursor", resp.Cursor))

		if !resp.HasMore {
			break
		}
	}

	logger.Logger.Info("Stream query completed", zap.Int("total_records", len(allRecords)))
	return allRecords, nil
}

// ===============================================
// 表管理
// ===============================================

// CreateTable 创建表
func (c *MinioDBClient) CreateTable(ctx context.Context, tableName string, config *miniodb.TableConfig) error {
	req := &miniodb.CreateTableRequest{
		TableName:   tableName,
		Config:      config,
		IfNotExists: true,
	}

	resp, err := c.client.CreateTable(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("create table failed: %s", resp.Message)
	}

	logger.Logger.Info("Table created successfully", zap.String("table", tableName))
	return nil
}

// ListTables 列出表
func (c *MinioDBClient) ListTables(ctx context.Context, pattern string) ([]*miniodb.TableInfo, error) {
	req := &miniodb.ListTablesRequest{
		Pattern: pattern,
	}

	resp, err := c.client.ListTables(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	logger.Logger.Info("Listed tables", zap.Int32("total", resp.Total))
	return resp.Tables, nil
}

// GetTable 获取表信息
func (c *MinioDBClient) GetTable(ctx context.Context, tableName string) (*miniodb.TableInfo, error) {
	req := &miniodb.GetTableRequest{
		TableName: tableName,
	}

	resp, err := c.client.GetTable(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get table info: %w", err)
	}

	logger.Logger.Info("Got table info", zap.String("table", tableName))
	return resp.TableInfo, nil
}

// DeleteTable 删除表
func (c *MinioDBClient) DeleteTable(ctx context.Context, tableName string) (int32, error) {
	req := &miniodb.DeleteTableRequest{
		TableName: tableName,
		IfExists:  false,
		Cascade:   false,
	}

	resp, err := c.client.DeleteTable(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to delete table: %w", err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("delete table failed: %s", resp.Message)
	}

	logger.Logger.Info("Table deleted successfully", zap.String("table", tableName), zap.Int32("files_deleted", resp.FilesDeleted))
	return resp.FilesDeleted, nil
}

// ===============================================
// 元数据管理
// ===============================================

// BackupMetadata 备份元数据
func (c *MinioDBClient) BackupMetadata(ctx context.Context) (string, error) {
	req := &miniodb.BackupMetadataRequest{
		Force: false,
	}

	resp, err := c.client.BackupMetadata(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to backup metadata: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("backup failed: %s", resp.Message)
	}

	logger.Logger.Info("Metadata backed up successfully", zap.String("id", resp.BackupId))
	return resp.BackupId, nil
}

// RestoreMetadata 恢复元数据
func (c *MinioDBClient) RestoreMetadata(ctx context.Context, fromLatest bool) error {
	req := &miniodb.RestoreMetadataRequest{
		FromLatest: fromLatest,
		DryRun:     false,
		Overwrite:  false,
		Validate:   true,
		Parallel:   true,
	}

	resp, err := c.client.RestoreMetadata(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to restore metadata: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("restore failed: %s", resp.Message)
	}

	logger.Logger.Info("Metadata restored successfully", zap.Int32("total", resp.EntriesTotal), zap.Int32("ok", resp.EntriesOk), zap.Int32("skipped", resp.EntriesSkipped), zap.Int32("errors", resp.EntriesError))
	return nil
}

// ListBackups 列出备份
func (c *MinioDBClient) ListBackups(ctx context.Context, days int32) ([]*miniodb.BackupInfo, error) {
	req := &miniodb.ListBackupsRequest{
		Days: days,
	}

	resp, err := c.client.ListBackups(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	logger.Logger.Info("Listed backups", zap.Int32("total", resp.Total))
	return resp.Backups, nil
}

// ===============================================
// 健康检查和监控
// ===============================================

// HealthCheck 健康检查
func (c *MinioDBClient) HealthCheck(ctx context.Context) (string, error) {
	req := &miniodb.HealthCheckRequest{}
	resp, err := c.client.HealthCheck(ctx, req)
	if err != nil {
		return "", fmt.Errorf("health check failed: %w", err)
	}

	logger.Logger.Info("Health check", zap.String("status", resp.Status), zap.String("version", resp.Version))
	return resp.Status, nil
}

// GetStatus 获取状态
func (c *MinioDBClient) GetStatus(ctx context.Context) (*miniodb.GetStatusResponse, error) {
	req := &miniodb.GetStatusRequest{}
	resp, err := c.client.GetStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	logger.Logger.Info("Got status", zap.Int32("total_nodes", resp.TotalNodes))
	return resp, nil
}

// ===============================================
// 主程序
// ===============================================

func main() {
	addr := flag.String("addr", "localhost:8080", "MinIODB gRPC server address")
	flag.Parse()

	ctx := context.Background()

	// 创建客户端
	client, err := NewMinioDBClient(*addr)
	if err != nil {
		logger.Logger.Error("Failed to create client", zap.Error(err))
		os.Exit(1)
	}
	defer client.Close()

	logger.Logger.Info("MinIODB Go gRPC Client Example", zap.String("server", *addr))

	// 1. 健康检查
	logger.Logger.Info("--- Health Check ---")
	if _, err := client.HealthCheck(ctx); err != nil {
		logger.Logger.Error("Health check failed", zap.Error(err))
	}

	// 2. 创建表
	logger.Logger.Info("--- Create Table ---")
	tableName := "users"
	config := &miniodb.TableConfig{
		BufferSize:           1000,
		FlushIntervalSeconds: 60,
		RetentionDays:        30,
		BackupEnabled:        true,
		IdStrategy:           "uuid",
		AutoGenerateId:       true,
		IdValidation: &miniodb.IDValidationRules{
			MaxLength: 255,
			Pattern:   "^[a-zA-Z0-9_-]+$",
		},
	}
	if err := client.CreateTable(ctx, tableName, config); err != nil {
		logger.Logger.Error("Create table failed (may already exist)", zap.Error(err))
	}

	// 3. 写入数据
	logger.Logger.Info("--- Write Data ---")
	for i := 0; i < 5; i++ {
		payload := map[string]interface{}{
			"name":       fmt.Sprintf("User %d", i),
			"email":      fmt.Sprintf("user%d@example.com", i),
			"age":        25 + i,
			"active":     true,
			"created_at": time.Now().Format(time.RFC3339),
		}
		recordID := fmt.Sprintf("user_%d", i)
		if err := client.WriteData(ctx, tableName, recordID, payload); err != nil {
			logger.Logger.Error("Write failed", zap.Error(err))
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 4. 查询数据
	logger.Logger.Info("--- Query Data ---")
	result, err := client.QueryData(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 3", tableName), 3)
	if err != nil {
		logger.Logger.Error("Query failed", zap.Error(err))
	} else {
		jsonData, _ := json.MarshalIndent(result, "", "  ")
		logger.Logger.Info("Query result", zap.String("result", string(jsonData)))
	}

	// 5. 更新数据
	logger.Logger.Info("--- Update Data ---")
	updatePayload := map[string]interface{}{
		"name":       "Updated User 0",
		"updated_at": time.Now().Format(time.RFC3339),
	}
	if err := client.UpdateData(ctx, tableName, "user_0", updatePayload); err != nil {
		logger.Logger.Error("Update failed", zap.Error(err))
	}

	// 6. 删除数据
	logger.Logger.Info("--- Delete Data ---")
	if _, err := client.DeleteData(ctx, tableName, "user_4"); err != nil {
		logger.Logger.Error("Delete failed", zap.Error(err))
	}

	// 7. 流式写入
	logger.Logger.Info("--- Stream Write ---")
	var streamRecords []*miniodb.DataRecord
	for i := 10; i < 20; i++ {
		payload, _ := structpb.NewStruct(map[string]interface{}{
			"name":       fmt.Sprintf("Stream User %d", i),
			"batch":      true,
			"created_at": time.Now().Format(time.RFC3339),
		})
		streamRecords = append(streamRecords, &miniodb.DataRecord{
			Id:        fmt.Sprintf("stream_user_%d", i),
			Timestamp: timestamppb.Now(),
			Payload:   payload,
		})
	}
	if _, err := client.StreamWrite(ctx, tableName, streamRecords); err != nil {
		logger.Logger.Error("Stream write failed", zap.Error(err))
	}

	// 8. 列出表
	logger.Logger.Info("--- List Tables ---")
	tables, err := client.ListTables(ctx, "*")
	if err != nil {
		logger.Logger.Error("List tables failed", zap.Error(err))
	} else {
		for _, t := range tables {
			logger.Logger.Info("Table", zap.String("name", t.Name), zap.String("status", t.Status))
		}
	}

	// 9. 获取表信息
	logger.Logger.Info("--- Get Table Info ---")
	tableInfo, err := client.GetTable(ctx, tableName)
	if err != nil {
		logger.Logger.Error("Get table info failed", zap.Error(err))
	} else {
		if tableInfo.Stats != nil {
			logger.Logger.Info("Record count", zap.Int64("count", tableInfo.Stats.RecordCount))
			logger.Logger.Info("File count", zap.Int64("count", tableInfo.Stats.FileCount))
			logger.Logger.Info("Size", zap.Int64("size", tableInfo.Stats.SizeBytes))
		}
	}

	// 10. 获取状态
	logger.Logger.Info("--- Get Status ---")
	status, err := client.GetStatus(ctx)
	if err != nil {
		logger.Logger.Error("Get status failed", zap.Error(err))
	} else {
		logger.Logger.Info("Total nodes", zap.Int32("total", status.TotalNodes))
		logger.Logger.Info("Buffer stats", zap.Any("stats", status.BufferStats))
		logger.Logger.Info("Redis stats", zap.Any("stats", status.RedisStats))
	}

	// 11. 列出备份
	logger.Logger.Info("--- List Backups ---")
	backups, err := client.ListBackups(ctx, 30)
	if err != nil {
		logger.Logger.Error("List backups failed", zap.Error(err))
	} else {
		logger.Logger.Info("Found backups", zap.Int("count", len(backups)))
		for _, b := range backups {
			logger.Logger.Info("Backup", zap.String("name", b.ObjectName), zap.Int64("size", b.Size), zap.String("node", b.NodeId))
		}
	}

	logger.Logger.Info("========================================")
	logger.Logger.Info("Example completed successfully!")
	logger.Logger.Info("========================================")
}
