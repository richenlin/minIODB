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
	"log"
	"time"

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

	log.Printf("✓ Data written successfully to table '%s', ID: %s, Node: %s", table, id, resp.NodeId)
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

	log.Printf("✓ Query executed successfully, has_more: %v", resp.HasMore)
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

	log.Printf("✓ Data updated successfully, ID: %s", id)
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

	log.Printf("✓ Data deleted successfully, deleted_count: %d", resp.DeletedCount)
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

		log.Printf("  Sent batch %d-%d", i, end-1)
	}

	// 获取最终响应
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return 0, fmt.Errorf("failed to receive response: %w", err)
	}

	log.Printf("✓ Stream write completed: %d records, %d errors", resp.RecordsCount, len(resp.Errors))
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
		log.Printf("  Received batch: %d records, has_more: %v, cursor: %s",
			len(resp.Records), resp.HasMore, resp.Cursor)

		if !resp.HasMore {
			break
		}
	}

	log.Printf("✓ Stream query completed: %d total records", len(allRecords))
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

	log.Printf("✓ Table '%s' created successfully", tableName)
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

	log.Printf("✓ Listed %d tables", resp.Total)
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

	log.Printf("✓ Got table info for '%s'", tableName)
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

	log.Printf("✓ Table '%s' deleted successfully, files deleted: %d", tableName, resp.FilesDeleted)
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

	log.Printf("✓ Metadata backed up successfully, ID: %s", resp.BackupId)
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

	log.Printf("✓ Metadata restored successfully: total=%d, ok=%d, skipped=%d, errors=%d",
		resp.EntriesTotal, resp.EntriesOk, resp.EntriesSkipped, resp.EntriesError)
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

	log.Printf("✓ Listed %d backups", resp.Total)
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

	log.Printf("✓ Health check: status=%s, version=%s", resp.Status, resp.Version)
	return resp.Status, nil
}

// GetStatus 获取状态
func (c *MinioDBClient) GetStatus(ctx context.Context) (*miniodb.GetStatusResponse, error) {
	req := &miniodb.GetStatusRequest{}
	resp, err := c.client.GetStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	log.Printf("✓ Got status: total_nodes=%d", resp.TotalNodes)
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
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	log.Println("========================================")
	log.Println("MinIODB Go gRPC Client Example")
	log.Printf("Server: %s\n", *addr)
	log.Println("========================================")

	// 1. 健康检查
	log.Println("\n--- Health Check ---")
	if _, err := client.HealthCheck(ctx); err != nil {
		log.Printf("Health check failed: %v", err)
	}

	// 2. 创建表
	log.Println("\n--- Create Table ---")
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
		log.Printf("Create table failed (may already exist): %v", err)
	}

	// 3. 写入数据
	log.Println("\n--- Write Data ---")
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
			log.Printf("Write failed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 4. 查询数据
	log.Println("\n--- Query Data ---")
	result, err := client.QueryData(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 3", tableName), 3)
	if err != nil {
		log.Printf("Query failed: %v", err)
	} else {
		jsonData, _ := json.MarshalIndent(result, "", "  ")
		log.Printf("Query result:\n%s", string(jsonData))
	}

	// 5. 更新数据
	log.Println("\n--- Update Data ---")
	updatePayload := map[string]interface{}{
		"name":       "Updated User 0",
		"updated_at": time.Now().Format(time.RFC3339),
	}
	if err := client.UpdateData(ctx, tableName, "user_0", updatePayload); err != nil {
		log.Printf("Update failed: %v", err)
	}

	// 6. 删除数据
	log.Println("\n--- Delete Data ---")
	if _, err := client.DeleteData(ctx, tableName, "user_4"); err != nil {
		log.Printf("Delete failed: %v", err)
	}

	// 7. 流式写入
	log.Println("\n--- Stream Write ---")
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
		log.Printf("Stream write failed: %v", err)
	}

	// 8. 列出表
	log.Println("\n--- List Tables ---")
	tables, err := client.ListTables(ctx, "*")
	if err != nil {
		log.Printf("List tables failed: %v", err)
	} else {
		for _, t := range tables {
			log.Printf("  - Table: %s, Status: %s", t.Name, t.Status)
		}
	}

	// 9. 获取表信息
	log.Println("\n--- Get Table Info ---")
	tableInfo, err := client.GetTable(ctx, tableName)
	if err != nil {
		log.Printf("Get table info failed: %v", err)
	} else {
		if tableInfo.Stats != nil {
			log.Printf("  - Record count: %d", tableInfo.Stats.RecordCount)
			log.Printf("  - File count: %d", tableInfo.Stats.FileCount)
			log.Printf("  - Size: %d bytes", tableInfo.Stats.SizeBytes)
		}
	}

	// 10. 获取状态
	log.Println("\n--- Get Status ---")
	status, err := client.GetStatus(ctx)
	if err != nil {
		log.Printf("Get status failed: %v", err)
	} else {
		log.Printf("  - Total nodes: %d", status.TotalNodes)
		log.Printf("  - Buffer stats: %v", status.BufferStats)
		log.Printf("  - Redis stats: %v", status.RedisStats)
	}

	// 11. 列出备份
	log.Println("\n--- List Backups ---")
	backups, err := client.ListBackups(ctx, 30)
	if err != nil {
		log.Printf("List backups failed: %v", err)
	} else {
		log.Printf("  Found %d backups in the last 30 days", len(backups))
		for _, b := range backups {
			log.Printf("  - Backup: %s, Size: %d, Node: %s", b.ObjectName, b.Size, b.NodeId)
		}
	}

	log.Println("\n========================================")
	log.Println("Example completed successfully!")
	log.Println("========================================")
}
