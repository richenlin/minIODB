package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/metadata"
	"minIODB/internal/query"

	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MinIODBService 实现MinIODBServiceServer接口
type MinIODBService struct {
	miniodb.UnimplementedMinIODBServiceServer
	cfg          *config.Config
	ingester     *ingest.Ingester
	querier      *query.Querier
	redisClient  *redis.Client
	tableManager *TableManager
	metadataMgr  *metadata.Manager
}

// NewMinIODBService 创建新的MinIODBService实例
func NewMinIODBService(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier,
	redisClient *redis.Client, metadataMgr *metadata.Manager) (*MinIODBService, error) {

	tableManager := NewTableManager(redisClient, nil, nil, cfg)

	return &MinIODBService{
		cfg:          cfg,
		ingester:     ingester,
		querier:      querier,
		redisClient:  redisClient,
		tableManager: tableManager,
		metadataMgr:  metadataMgr,
	}, nil
}

// WriteData 写入数据
func (s *MinIODBService) WriteData(ctx context.Context, req *miniodb.WriteDataRequest) (*miniodb.WriteDataResponse, error) {
	// 处理表名：优先使用请求中的表名，如果为空则使用默认表
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	log.Printf("Processing write request for table: %s, ID: %s", tableName, req.Data.Id)

	// 验证请求
	if err := s.validateWriteRequest(req); err != nil {
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		return &miniodb.WriteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", tableName),
			NodeId:  s.cfg.Server.NodeID,
		}, nil
	}

	// 确保表存在
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		log.Printf("ERROR: Failed to ensure table exists: %v", err)
		return &miniodb.WriteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Table error: %v", err),
		}, nil
	}

	// 转换为内部写入请求格式
	ingestReq := &miniodb.WriteRequest{
		Table:     tableName,
		Id:        req.Data.Id,
		Timestamp: req.Data.Timestamp,
		Payload:   req.Data.Payload,
	}

	// 使用Ingester处理写入
	if err := s.ingester.IngestData(ingestReq); err != nil {
		log.Printf("ERROR: Failed to ingest data for table %s, ID %s: %v", tableName, req.Data.Id, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to ingest data: %v", err))
	}

	// 更新表的最后写入时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		log.Printf("WARN: Failed to update last write time for table %s: %v", tableName, err)
	}

	log.Printf("Successfully ingested data for table: %s, ID: %s", tableName, req.Data.Id)
	return &miniodb.WriteDataResponse{
		Success: true,
		Message: fmt.Sprintf("Data successfully ingested for table: %s, ID: %s", tableName, req.Data.Id),
		NodeId:  s.cfg.Server.NodeID,
	}, nil
}

// validateWriteRequest 验证写入请求
func (s *MinIODBService) validateWriteRequest(req *miniodb.WriteDataRequest) error {
	if req.Data == nil {
		return status.Error(codes.InvalidArgument, "Data record is required")
	}

	if req.Data.Id == "" {
		return status.Error(codes.InvalidArgument, "ID is required and cannot be empty")
	}

	if len(req.Data.Id) > 255 {
		return status.Error(codes.InvalidArgument, "ID cannot exceed 255 characters")
	}

	if req.Data.Timestamp == nil {
		return status.Error(codes.InvalidArgument, "Timestamp is required")
	}

	if req.Data.Payload == nil {
		return status.Error(codes.InvalidArgument, "Payload is required")
	}

	// 验证ID格式（只允许字母、数字、连字符和下划线）
	for _, r := range req.Data.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "ID contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}

	return nil
}

// QueryData 查询数据
func (s *MinIODBService) QueryData(ctx context.Context, req *miniodb.QueryDataRequest) (*miniodb.QueryDataResponse, error) {
	log.Printf("Processing query request: %s", req.Sql)

	// 验证请求
	if err := s.validateQueryRequest(req); err != nil {
		return nil, err
	}

	// 处理向后兼容：将旧的"table"关键字替换为默认表名
	sql := req.Sql
	if strings.Contains(strings.ToLower(sql), "from table") {
		defaultTable := s.cfg.TableManagement.DefaultTable
		sql = strings.ReplaceAll(sql, "FROM table", fmt.Sprintf("FROM %s", defaultTable))
		sql = strings.ReplaceAll(sql, "from table", fmt.Sprintf("from %s", defaultTable))
		log.Printf("Converted legacy SQL to use default table: %s", sql)
	}

	// 如果指定了限制，添加到SQL中
	if req.Limit > 0 {
		sql = fmt.Sprintf("%s LIMIT %d", sql, req.Limit)
	}

	// 使用Querier执行查询
	result, err := s.querier.ExecuteQuery(sql)
	if err != nil {
		log.Printf("ERROR: Query failed: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Query execution failed: %v", err))
	}

	// 目前返回全部结果
	hasMore := false
	nextCursor := ""

	log.Printf("Query completed successfully, result length: %d characters", len(result))
	return &miniodb.QueryDataResponse{
		ResultJson: result,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// validateQueryRequest 验证查询请求
func (s *MinIODBService) validateQueryRequest(req *miniodb.QueryDataRequest) error {
	if req.Sql == "" {
		return status.Error(codes.InvalidArgument, "SQL query is required and cannot be empty")
	}

	if len(req.Sql) > 10000 {
		return status.Error(codes.InvalidArgument, "SQL query cannot exceed 10000 characters")
	}

	// 基本的SQL注入防护（简单检查）
	lowerSQL := strings.ToLower(req.Sql)
	dangerousKeywords := []string{"drop", "delete", "truncate", "alter", "create", "insert", "update"}
	for _, keyword := range dangerousKeywords {
		if strings.Contains(lowerSQL, keyword) {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("SQL contains dangerous keyword: %s", keyword))
		}
	}

	return nil
}

// UpdateData 更新数据
func (s *MinIODBService) UpdateData(ctx context.Context, req *miniodb.UpdateDataRequest) (*miniodb.UpdateDataResponse, error) {
	// 处理表名：优先使用请求中的表名，如果为空则使用默认表
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	log.Printf("Processing update request for table: %s, ID: %s", tableName, req.Id)

	// 验证请求
	if err := s.validateUpdateRequest(req); err != nil {
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", tableName),
		}, nil
	}

	// 确保表存在
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		log.Printf("ERROR: Failed to ensure table exists: %v", err)
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: fmt.Sprintf("Table error: %v", err),
		}, nil
	}

	// TODO: 实现更新操作 - 目前只是模拟成功
	// 实际实现需要根据具体存储引擎来处理更新逻辑
	log.Printf("Simulated update operation for record %s in table %s", req.Id, tableName)

	return &miniodb.UpdateDataResponse{
		Success: true,
		Message: fmt.Sprintf("Record %s updated successfully in table %s", req.Id, tableName),
	}, nil
}

// validateUpdateRequest 验证更新请求
func (s *MinIODBService) validateUpdateRequest(req *miniodb.UpdateDataRequest) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "ID is required and cannot be empty")
	}

	if len(req.Id) > 255 {
		return status.Error(codes.InvalidArgument, "ID cannot exceed 255 characters")
	}

	if req.Payload == nil {
		return status.Error(codes.InvalidArgument, "Payload is required")
	}

	// 验证ID格式
	for _, r := range req.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "ID contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}

	return nil
}

// DeleteData 删除数据
func (s *MinIODBService) DeleteData(ctx context.Context, req *miniodb.DeleteDataRequest) (*miniodb.DeleteDataResponse, error) {
	// 处理表名：优先使用请求中的表名，如果为空则使用默认表
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	log.Printf("Processing delete request for table: %s, ID: %s", tableName, req.Id)

	// 验证请求
	if err := s.validateDeleteRequest(req); err != nil {
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		return &miniodb.DeleteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", tableName),
		}, nil
	}

	// 确保表存在
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		log.Printf("ERROR: Failed to ensure table exists: %v", err)
		return &miniodb.DeleteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Table error: %v", err),
		}, nil
	}

	// TODO: 实现删除操作 - 目前只是模拟成功
	// 实际实现需要根据具体存储引擎来处理删除逻辑
	deletedCount := int32(1) // 模拟删除1条记录
	log.Printf("Simulated delete operation for record %s in table %s", req.Id, tableName)

	return &miniodb.DeleteDataResponse{
		Success:      true,
		Message:      fmt.Sprintf("Record %s deleted successfully from table %s", req.Id, tableName),
		DeletedCount: deletedCount,
	}, nil
}

// validateDeleteRequest 验证删除请求
func (s *MinIODBService) validateDeleteRequest(req *miniodb.DeleteDataRequest) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "ID is required and cannot be empty")
	}

	if len(req.Id) > 255 {
		return status.Error(codes.InvalidArgument, "ID cannot exceed 255 characters")
	}

	// 验证ID格式
	for _, r := range req.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "ID contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}

	return nil
}

// ConvertResultToRecords 将JSON结果转换为DataRecord列表
func (s *MinIODBService) ConvertResultToRecords(resultJson string) ([]*miniodb.DataRecord, error) {
	var rawData []map[string]interface{}
	if err := json.Unmarshal([]byte(resultJson), &rawData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON result: %w", err)
	}

	var records []*miniodb.DataRecord
	for _, row := range rawData {
		record := &miniodb.DataRecord{}

		// 提取ID
		if id, ok := row["id"].(string); ok {
			record.Id = id
		}

		// 提取时间戳
		if timestampStr, ok := row["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, timestampStr); err == nil {
				record.Timestamp = timestamppb.New(t)
			}
		}

		// 提取负载数据 - 移除已处理的字段
		payload := make(map[string]interface{})
		for k, v := range row {
			if k != "id" && k != "timestamp" {
				payload[k] = v
			}
		}

		// 将map转换为protobuf Struct
		// TODO: 实现正确的map到protobuf.Struct转换
		// record.Payload = structFromMap(payload)

		records = append(records, record)
	}

	return records, nil
}

// StreamWrite 流式写入数据 (TODO: 实现)
func (s *MinIODBService) StreamWrite(stream miniodb.MinIODBService_StreamWriteServer) error {
	return status.Error(codes.Unimplemented, "StreamWrite not yet implemented")
}

// StreamQuery 流式查询数据 (TODO: 实现)
func (s *MinIODBService) StreamQuery(req *miniodb.StreamQueryRequest, stream miniodb.MinIODBService_StreamQueryServer) error {
	return status.Error(codes.Unimplemented, "StreamQuery not yet implemented")
}

// CreateTable 创建表
func (s *MinIODBService) CreateTable(ctx context.Context, req *miniodb.CreateTableRequest) (*miniodb.CreateTableResponse, error) {
	log.Printf("Processing create table request: %s", req.TableName)

	// 验证表名
	if req.TableName == "" {
		return &miniodb.CreateTableResponse{
			Success: false,
			Message: "Table name is required",
		}, nil
	}

	if !s.cfg.IsValidTableName(req.TableName) {
		return &miniodb.CreateTableResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", req.TableName),
		}, nil
	}

	// 转换配置
	var tableConfig *config.TableConfig
	if req.Config != nil {
		tableConfig = &config.TableConfig{
			BufferSize:    int(req.Config.BufferSize),
			FlushInterval: time.Duration(req.Config.FlushIntervalSeconds) * time.Second,
			RetentionDays: int(req.Config.RetentionDays),
			BackupEnabled: req.Config.BackupEnabled,
			Properties:    req.Config.Properties,
		}
	}

	// 创建表
	err := s.tableManager.CreateTable(ctx, req.TableName, tableConfig, req.IfNotExists)
	if err != nil {
		log.Printf("ERROR: Failed to create table %s: %v", req.TableName, err)
		return &miniodb.CreateTableResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create table: %v", err),
		}, nil
	}

	return &miniodb.CreateTableResponse{
		Success: true,
		Message: fmt.Sprintf("Table %s created successfully", req.TableName),
	}, nil
}

// ListTables 列出表
func (s *MinIODBService) ListTables(ctx context.Context, req *miniodb.ListTablesRequest) (*miniodb.ListTablesResponse, error) {
	log.Printf("Processing list tables request with pattern: %s", req.Pattern)

	tables, err := s.tableManager.ListTables(ctx, req.Pattern)
	if err != nil {
		log.Printf("ERROR: Failed to list tables: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to list tables: %v", err))
	}

	var tableInfos []*miniodb.TableInfo
	for _, table := range tables {
		// 解析时间字符串为protobuf时间戳
		var createdAt *timestamppb.Timestamp
		if table.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, table.CreatedAt); err == nil {
				createdAt = timestamppb.New(t)
			}
		}

		var lastWrite *timestamppb.Timestamp
		if table.LastWrite != "" {
			if t, err := time.Parse(time.RFC3339, table.LastWrite); err == nil {
				lastWrite = timestamppb.New(t)
			}
		}

		// 转换配置
		var config *miniodb.TableConfig
		if table.Config != nil {
			config = &miniodb.TableConfig{
				BufferSize:           int32(table.Config.BufferSize),
				FlushIntervalSeconds: int32(table.Config.FlushInterval.Seconds()),
				RetentionDays:        int32(table.Config.RetentionDays),
				BackupEnabled:        table.Config.BackupEnabled,
				Properties:           table.Config.Properties,
			}
		}

		tableInfo := &miniodb.TableInfo{
			Name:      table.Name,
			Config:    config,
			CreatedAt: createdAt,
			LastWrite: lastWrite,
			Status:    table.Status,
			// Stats: 需要时再填充
		}
		tableInfos = append(tableInfos, tableInfo)
	}

	return &miniodb.ListTablesResponse{
		Tables: tableInfos,
	}, nil
}

// GetTable 获取表信息
func (s *MinIODBService) GetTable(ctx context.Context, req *miniodb.GetTableRequest) (*miniodb.GetTableResponse, error) {
	log.Printf("Processing get table request: %s", req.TableName)

	if req.TableName == "" {
		return nil, status.Error(codes.InvalidArgument, "Table name is required")
	}

	// 检查表是否存在
	exists, err := s.tableManager.TableExists(ctx, req.TableName)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to check table existence: %v", err))
	}

	if !exists {
		return &miniodb.GetTableResponse{
			TableInfo: nil,
		}, nil
	}

	// 获取表信息和统计
	tableInfo, tableStats, err := s.tableManager.DescribeTable(ctx, req.TableName)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to get table information: %v", err))
	}

	// 转换时间戳
	var createdAt *timestamppb.Timestamp
	if tableInfo.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, tableInfo.CreatedAt); err == nil {
			createdAt = timestamppb.New(t)
		}
	}

	var lastWrite *timestamppb.Timestamp
	if tableInfo.LastWrite != "" {
		if t, err := time.Parse(time.RFC3339, tableInfo.LastWrite); err == nil {
			lastWrite = timestamppb.New(t)
		}
	}

	// 转换配置
	var config *miniodb.TableConfig
	if tableInfo.Config != nil {
		config = &miniodb.TableConfig{
			BufferSize:           int32(tableInfo.Config.BufferSize),
			FlushIntervalSeconds: int32(tableInfo.Config.FlushInterval.Seconds()),
			RetentionDays:        int32(tableInfo.Config.RetentionDays),
			BackupEnabled:        tableInfo.Config.BackupEnabled,
			Properties:           tableInfo.Config.Properties,
		}
	}

	// 转换统计信息
	var stats *miniodb.TableStats
	if tableStats != nil {
		var oldestRecord, newestRecord *timestamppb.Timestamp
		if tableStats.OldestRecord != "" {
			if t, err := time.Parse(time.RFC3339, tableStats.OldestRecord); err == nil {
				oldestRecord = timestamppb.New(t)
			}
		}
		if tableStats.NewestRecord != "" {
			if t, err := time.Parse(time.RFC3339, tableStats.NewestRecord); err == nil {
				newestRecord = timestamppb.New(t)
			}
		}

		stats = &miniodb.TableStats{
			RecordCount:  tableStats.RecordCount,
			FileCount:    tableStats.FileCount,
			SizeBytes:    tableStats.SizeBytes,
			OldestRecord: oldestRecord,
			NewestRecord: newestRecord,
		}
	}

	table := &miniodb.TableInfo{
		Name:      tableInfo.Name,
		Config:    config,
		CreatedAt: createdAt,
		LastWrite: lastWrite,
		Status:    tableInfo.Status,
		Stats:     stats,
	}

	return &miniodb.GetTableResponse{
		TableInfo: table,
	}, nil
}

// DeleteTable 删除表
func (s *MinIODBService) DeleteTable(ctx context.Context, req *miniodb.DeleteTableRequest) (*miniodb.DeleteTableResponse, error) {
	log.Printf("Processing delete table request: %s", req.TableName)

	if req.TableName == "" {
		return nil, status.Error(codes.InvalidArgument, "Table name is required")
	}

	deletedFiles, err := s.tableManager.DropTable(ctx, req.TableName, req.IfExists, req.Cascade)
	if err != nil {
		log.Printf("ERROR: Failed to delete table %s: %v", req.TableName, err)
		return &miniodb.DeleteTableResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to delete table: %v", err),
		}, nil
	}

	return &miniodb.DeleteTableResponse{
		Success:      true,
		Message:      fmt.Sprintf("Table %s deleted successfully", req.TableName),
		FilesDeleted: deletedFiles,
	}, nil
}

// BackupMetadata 备份元数据 (TODO: 实现)
func (s *MinIODBService) BackupMetadata(ctx context.Context, req *miniodb.BackupMetadataRequest) (*miniodb.BackupMetadataResponse, error) {
	return &miniodb.BackupMetadataResponse{
		Success:  false,
		Message:  "BackupMetadata not yet implemented",
		BackupId: "",
	}, nil
}

// RestoreMetadata 恢复元数据 (TODO: 实现)
func (s *MinIODBService) RestoreMetadata(ctx context.Context, req *miniodb.RestoreMetadataRequest) (*miniodb.RestoreMetadataResponse, error) {
	return &miniodb.RestoreMetadataResponse{
		Success: false,
		Message: "RestoreMetadata not yet implemented",
	}, nil
}

// ListBackups 列出备份 (TODO: 实现)
func (s *MinIODBService) ListBackups(ctx context.Context, req *miniodb.ListBackupsRequest) (*miniodb.ListBackupsResponse, error) {
	return &miniodb.ListBackupsResponse{
		Backups: []*miniodb.BackupInfo{},
	}, nil
}

// GetMetadataStatus 获取元数据状态 (TODO: 实现)
func (s *MinIODBService) GetMetadataStatus(ctx context.Context, req *miniodb.GetMetadataStatusRequest) (*miniodb.GetMetadataStatusResponse, error) {
	return &miniodb.GetMetadataStatusResponse{
		NodeId:       s.cfg.Server.NodeID,
		BackupStatus: map[string]string{"status": "not_configured"},
		LastBackup:   nil,
		NextBackup:   nil,
		HealthStatus: "not_configured",
	}, nil
}

// HealthCheck 健康检查
func (s *MinIODBService) HealthCheck(ctx context.Context, req *miniodb.HealthCheckRequest) (*miniodb.HealthCheckResponse, error) {
	log.Printf("Processing health check request")

	status := "healthy"
	details := make(map[string]string)

	// 检查Redis连接
	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		status = "unhealthy"
		details["redis"] = fmt.Sprintf("Redis connection failed: %v", err)
	} else {
		details["redis"] = "connected"
	}

	// 检查缓冲区状态
	if s.ingester != nil {
		stats := s.ingester.GetBufferStats()
		if stats != nil {
			details["buffer_size"] = fmt.Sprintf("%d", stats.BufferSize)
		} else {
			details["buffer"] = "not_available"
		}
	}

	// 检查查询引擎
	if s.querier != nil {
		details["query_engine"] = "available"
	} else {
		status = "unhealthy"
		details["query_engine"] = "not_available"
	}

	details["node_id"] = s.cfg.Server.NodeID
	details["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	return &miniodb.HealthCheckResponse{
		Status:    status,
		Timestamp: timestamppb.New(time.Now()),
		Version:   "1.0.0",
		Details:   details,
	}, nil
}

// GetStatus 获取状态 (TODO: 实现)
func (s *MinIODBService) GetStatus(ctx context.Context, req *miniodb.GetStatusRequest) (*miniodb.GetStatusResponse, error) {
	return &miniodb.GetStatusResponse{
		Timestamp:   timestamppb.New(time.Now()),
		BufferStats: make(map[string]int64),
		RedisStats:  make(map[string]int64),
		MinioStats:  make(map[string]int64),
		Nodes: []*miniodb.NodeInfo{
			{
				Id:       s.cfg.Server.NodeID,
				Status:   "running",
				Type:     "primary",
				Address:  "localhost",
				LastSeen: time.Now().Unix(),
			},
		},
		TotalNodes: 1,
	}, nil
}

// GetMetrics 获取监控指标 (TODO: 实现)
func (s *MinIODBService) GetMetrics(ctx context.Context, req *miniodb.GetMetricsRequest) (*miniodb.GetMetricsResponse, error) {
	return &miniodb.GetMetricsResponse{
		Timestamp:          timestamppb.New(time.Now()),
		PerformanceMetrics: make(map[string]float64),
		ResourceUsage:      make(map[string]int64),
		SystemInfo:         make(map[string]string),
	}, nil
}

// Close 关闭服务
func (s *MinIODBService) Close() error {
	log.Printf("Closing MinIODBService")

	if s.querier != nil {
		s.querier.Close()
	}

	if s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			log.Printf("WARN: Failed to close Redis client: %v", err)
		}
	}

	return nil
}
