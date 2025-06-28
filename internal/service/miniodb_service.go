package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
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
	"google.golang.org/protobuf/types/known/structpb"
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

	// OLAP系统中的更新策略：先删除后重新插入
	// 1. 首先尝试删除现有记录
	if s.querier != nil {
		deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = '%s'", tableName, req.Id)
		_, err := s.querier.ExecuteQuery(deleteSQL)
		if err != nil {
			log.Printf("WARN: Delete query during update failed: %v", err)
			// 即使删除失败，我们仍然可以继续插入（可能是新记录）
		}
	}

	// 2. 插入更新后的数据
	if s.ingester != nil {
		// 构建写入请求
		writeReq := &miniodb.WriteRequest{
			Table:     tableName,
			Id:        req.Id,
			Timestamp: req.Timestamp,
			Payload:   req.Payload,
		}

		// 如果没有提供时间戳，使用当前时间
		if writeReq.Timestamp == nil {
			writeReq.Timestamp = timestamppb.Now()
		}

		// 执行插入
		if err := s.ingester.IngestData(writeReq); err != nil {
			log.Printf("ERROR: Failed to ingest updated data for table %s, ID %s: %v", tableName, req.Id, err)
			return &miniodb.UpdateDataResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to update record: %v", err),
			}, nil
		}
	} else {
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: "Ingester not available for update operation",
		}, nil
	}

	// 3. 清理相关的缓存
	if s.redisClient != nil {
		// 清理相关的缓存项
		cachePattern := fmt.Sprintf("cache:table:%s:id:%s:*", tableName, req.Id)
		if keys, err := s.redisClient.Keys(ctx, cachePattern).Result(); err == nil {
			if len(keys) > 0 {
				s.redisClient.Del(ctx, keys...)
				log.Printf("Cleaned %d cache entries for updated record", len(keys))
			}
		}

		// 清理查询缓存（因为数据已更新）
		queryCachePattern := fmt.Sprintf("query_cache:*%s*", tableName)
		if keys, err := s.redisClient.Keys(ctx, queryCachePattern).Result(); err == nil {
			if len(keys) > 0 {
				s.redisClient.Del(ctx, keys...)
				log.Printf("Cleaned %d query cache entries for table %s", len(keys), tableName)
			}
		}
	}

	// 4. 更新表的最后写入时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		log.Printf("WARN: Failed to update last write time for table %s: %v", tableName, err)
	}

	log.Printf("Successfully updated record %s in table %s", req.Id, tableName)
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

	deletedCount := int32(0)

	// 1. 从已持久化的数据中删除（通过DuckDB执行DELETE语句）
	if s.querier != nil {
		// 构建DELETE SQL语句
		deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = '%s'", tableName, req.Id)

		// 执行删除操作
		result, err := s.querier.ExecuteQuery(deleteSQL)
		if err != nil {
			log.Printf("WARN: Delete query failed: %v", err)
			return &miniodb.DeleteDataResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to delete record from storage: %v", err),
			}, nil
		}

		// 解析删除结果，获取实际删除的行数
		// 这里简化处理，假设删除成功就是1行
		if strings.Contains(result, "DELETE") || strings.Contains(result, "success") || result != "" {
			deletedCount = 1
		}
	}

	// 2. 清理相关的缓存和索引
	if s.redisClient != nil {
		// 清理相关的缓存项
		cachePattern := fmt.Sprintf("cache:table:%s:id:%s:*", tableName, req.Id)
		if keys, err := s.redisClient.Keys(ctx, cachePattern).Result(); err == nil {
			if len(keys) > 0 {
				s.redisClient.Del(ctx, keys...)
				log.Printf("Cleaned %d cache entries for deleted record", len(keys))
			}
		}

		// 更新表的记录计数
		recordCountKey := fmt.Sprintf("table:%s:record_count", tableName)
		s.redisClient.Decr(ctx, recordCountKey)
	}

	// 3. 更新表的最后修改时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		log.Printf("WARN: Failed to update last write time for table %s: %v", tableName, err)
	}

	if deletedCount > 0 {
		log.Printf("Successfully deleted %d records for ID %s from table %s", deletedCount, req.Id, tableName)
		return &miniodb.DeleteDataResponse{
			Success:      true,
			Message:      fmt.Sprintf("Record %s deleted successfully from table %s", req.Id, tableName),
			DeletedCount: deletedCount,
		}, nil
	} else {
		return &miniodb.DeleteDataResponse{
			Success:      false,
			Message:      fmt.Sprintf("No records found with ID %s in table %s", req.Id, tableName),
			DeletedCount: 0,
		}, nil
	}
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
		protoStruct, err := s.mapToProtobufStruct(payload)
		if err != nil {
			log.Printf("WARN: Failed to convert payload to protobuf struct: %v", err)
			// 创建一个空的Struct而不是跳过整个记录
			protoStruct = &structpb.Struct{Fields: make(map[string]*structpb.Value)}
		}
		record.Payload = protoStruct

		records = append(records, record)
	}

	return records, nil
}

// mapToProtobufStruct 将map[string]interface{}转换为protobuf Struct
func (s *MinIODBService) mapToProtobufStruct(data map[string]interface{}) (*structpb.Struct, error) {
	fields := make(map[string]*structpb.Value)

	for key, value := range data {
		protoValue, err := s.interfaceToProtobufValue(value)
		if err != nil {
			log.Printf("WARN: Failed to convert field %s: %v", key, err)
			// 跳过有问题的字段，而不是整个转换失败
			continue
		}
		fields[key] = protoValue
	}

	return &structpb.Struct{Fields: fields}, nil
}

// interfaceToProtobufValue 将interface{}转换为protobuf Value
func (s *MinIODBService) interfaceToProtobufValue(value interface{}) (*structpb.Value, error) {
	if value == nil {
		return structpb.NewNullValue(), nil
	}

	switch v := value.(type) {
	case bool:
		return structpb.NewBoolValue(v), nil
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
	case string:
		return structpb.NewStringValue(v), nil
	case []interface{}:
		// 处理数组
		var listValues []*structpb.Value
		for _, item := range v {
			itemValue, err := s.interfaceToProtobufValue(item)
			if err != nil {
				log.Printf("WARN: Failed to convert array item: %v", err)
				continue
			}
			listValues = append(listValues, itemValue)
		}
		return structpb.NewListValue(&structpb.ListValue{Values: listValues}), nil
	case map[string]interface{}:
		// 处理嵌套对象
		nestedStruct, err := s.mapToProtobufStruct(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert nested map: %w", err)
		}
		return structpb.NewStructValue(nestedStruct), nil
	default:
		// 对于未知类型，尝试转换为字符串
		return structpb.NewStringValue(fmt.Sprintf("%v", v)), nil
	}
}

// StreamWrite 流式写入数据
func (s *MinIODBService) StreamWrite(stream miniodb.MinIODBService_StreamWriteServer) error {
	log.Printf("Starting stream write session")

	ctx := stream.Context()
	successCount := int32(0)
	errorCount := int32(0)
	var lastError error

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stream write cancelled by client")
			return ctx.Err()
		default:
		}

		// 接收写入请求
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// 客户端结束流，发送最终响应
				var errors []string
				if lastError != nil {
					errors = append(errors, lastError.Error())
				}

				finalResponse := &miniodb.StreamWriteResponse{
					Success:      errorCount == 0,
					RecordsCount: int64(successCount),
					Errors:       errors,
				}

				log.Printf("Stream write completed: %d success, %d errors", successCount, errorCount)
				return stream.SendAndClose(finalResponse)
			}
			log.Printf("ERROR: Failed to receive stream write request: %v", err)
			return status.Error(codes.Internal, fmt.Sprintf("Failed to receive request: %v", err))
		}

		// 处理批量写入请求
		batchSize := len(req.Records)
		if err := s.processStreamWriteRequest(ctx, req); err != nil {
			errorCount += int32(batchSize)
			lastError = err
			log.Printf("ERROR: Stream write failed for %d records in table %s: %v", batchSize, req.Table, err)
		} else {
			successCount += int32(batchSize)
			log.Printf("Stream write success for %d records in table %s", batchSize, req.Table)
		}

		// 可以选择是否在每次写入后发送确认（这里简化为只在最后发送）
		// 如果需要实时反馈，可以调用stream.Send()发送中间响应
	}
}

// processStreamWriteRequest 处理批量流式写入请求
func (s *MinIODBService) processStreamWriteRequest(ctx context.Context, req *miniodb.StreamWriteRequest) error {
	// 处理批量记录
	for _, record := range req.Records {
		// 转换为标准写入请求
		writeReq := &miniodb.WriteDataRequest{
			Table: req.Table,
			Data:  record,
		}

		// 复用现有的写入逻辑
		response, err := s.WriteData(ctx, writeReq)
		if err != nil {
			return fmt.Errorf("record %s failed: %v", record.Id, err)
		}

		if !response.Success {
			return fmt.Errorf("record %s failed: %s", record.Id, response.Message)
		}
	}

	return nil
}

// StreamQuery 流式查询数据
func (s *MinIODBService) StreamQuery(req *miniodb.StreamQueryRequest, stream miniodb.MinIODBService_StreamQueryServer) error {
	log.Printf("Processing stream query request: %s", req.Sql)

	// 验证请求
	if err := s.validateStreamQueryRequest(req); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// 设置默认批次大小
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100 // 默认批次大小
	}

	// 执行查询
	resultJson, err := s.querier.ExecuteQuery(req.Sql)
	if err != nil {
		log.Printf("ERROR: Stream query failed: %v", err)
		return status.Error(codes.Internal, fmt.Sprintf("Query failed: %v", err))
	}

	// 转换查询结果为记录
	records, err := s.ConvertResultToRecords(resultJson)
	if err != nil {
		log.Printf("ERROR: Failed to convert query result to records: %v", err)
		return status.Error(codes.Internal, fmt.Sprintf("Failed to convert result: %v", err))
	}

	// 分批发送结果
	totalRecords := len(records)
	offset := 0

	// 处理游标（简单实现：游标表示起始位置）
	if req.Cursor != "" {
		if startOffset, err := strconv.Atoi(req.Cursor); err == nil && startOffset > 0 {
			offset = startOffset
		}
	}

	for offset < totalRecords {
		// 计算当前批次的结束位置
		end := offset + int(batchSize)
		if end > totalRecords {
			end = totalRecords
		}

		// 准备当前批次的记录
		batch := records[offset:end]

		// 检查是否有更多数据
		hasMore := end < totalRecords

		// 生成下一个游标
		var nextCursor string
		if hasMore {
			nextCursor = strconv.Itoa(end)
		}

		// 发送批次数据
		response := &miniodb.StreamQueryResponse{
			Records: batch,
			HasMore: hasMore,
			Cursor:  nextCursor,
		}

		if err := stream.Send(response); err != nil {
			log.Printf("ERROR: Failed to send stream response: %v", err)
			return status.Error(codes.Internal, fmt.Sprintf("Failed to send response: %v", err))
		}

		log.Printf("Sent batch of %d records (offset: %d, hasMore: %t)", len(batch), offset, hasMore)

		// 移动到下一批次
		offset = end

		// 检查上下文是否被取消
		if stream.Context().Err() != nil {
			log.Printf("Stream query cancelled by client")
			return status.Error(codes.Canceled, "Stream query cancelled")
		}
	}

	log.Printf("Stream query completed successfully, total records: %d", totalRecords)
	return nil
}

// validateStreamQueryRequest 验证流式查询请求
func (s *MinIODBService) validateStreamQueryRequest(req *miniodb.StreamQueryRequest) error {
	if req.Sql == "" {
		return fmt.Errorf("SQL query is required")
	}

	if req.BatchSize < 0 {
		return fmt.Errorf("batch_size must be non-negative")
	}

	if req.BatchSize > 10000 {
		return fmt.Errorf("batch_size too large, maximum is 10000")
	}

	return nil
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

// BackupMetadata 备份元数据
func (s *MinIODBService) BackupMetadata(ctx context.Context, req *miniodb.BackupMetadataRequest) (*miniodb.BackupMetadataResponse, error) {
	log.Printf("Processing backup metadata request, force: %v", req.Force)

	// 获取备份管理器
	backupManager := s.metadataMgr.GetBackupManager()
	if backupManager == nil {
		return &miniodb.BackupMetadataResponse{
			Success:   false,
			Message:   "Backup manager not available",
			BackupId:  "",
			Timestamp: nil,
		}, nil
	}

	// 执行手动备份
	if err := s.metadataMgr.ManualBackup(ctx); err != nil {
		log.Printf("ERROR: Failed to create backup: %v", err)
		return &miniodb.BackupMetadataResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to create backup: %v", err),
			BackupId:  "",
			Timestamp: nil,
		}, nil
	}

	// 获取最新备份信息以返回备份ID
	recoveryManager := s.metadataMgr.GetRecoveryManager()
	backupTime := time.Now()
	if recoveryManager != nil {
		if latestBackup, err := recoveryManager.GetLatestBackup(ctx); err == nil {
			return &miniodb.BackupMetadataResponse{
				Success:   true,
				Message:   "Backup completed successfully",
				BackupId:  latestBackup.ObjectName,
				Timestamp: timestamppb.New(latestBackup.Timestamp),
			}, nil
		}
	}

	// 如果无法获取备份信息，仍然返回成功
	return &miniodb.BackupMetadataResponse{
		Success:   true,
		Message:   "Backup completed successfully",
		BackupId:  fmt.Sprintf("backup_%d", backupTime.Unix()),
		Timestamp: timestamppb.New(backupTime),
	}, nil
}

// RestoreMetadata 恢复元数据
func (s *MinIODBService) RestoreMetadata(ctx context.Context, req *miniodb.RestoreMetadataRequest) (*miniodb.RestoreMetadataResponse, error) {
	log.Printf("Processing restore metadata request, backup_file: %s, from_latest: %v", req.BackupFile, req.FromLatest)

	// 获取恢复管理器
	recoveryManager := s.metadataMgr.GetRecoveryManager()
	if recoveryManager == nil {
		return &miniodb.RestoreMetadataResponse{
			Success: false,
			Message: "Recovery manager not available",
		}, nil
	}

	// 构建恢复选项
	options := metadata.RecoveryOptions{
		Overwrite: req.Overwrite,
		Validate:  req.Validate,
		DryRun:    req.DryRun,
		Parallel:  req.Parallel,
		Filters:   make(map[string]interface{}),
	}

	// 转换filters
	for k, v := range req.Filters {
		options.Filters[k] = v
	}

	// 设置键模式过滤
	if len(req.KeyPatterns) > 0 {
		options.KeyPatterns = req.KeyPatterns
	}

	// 设置恢复模式
	if req.DryRun {
		options.Mode = metadata.RecoveryModeDryRun
	} else {
		options.Mode = metadata.RecoveryModeComplete
	}

	var result *metadata.RecoveryResult
	var err error

	// 执行恢复
	if req.FromLatest || req.BackupFile == "" {
		log.Printf("Recovering from latest backup")
		result, err = recoveryManager.RecoverFromLatest(ctx, options)
	} else {
		log.Printf("Recovering from backup file: %s", req.BackupFile)
		result, err = recoveryManager.RecoverFromBackup(ctx, req.BackupFile, options)
	}

	if err != nil {
		log.Printf("ERROR: Failed to restore metadata: %v", err)
		return &miniodb.RestoreMetadataResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to restore metadata: %v", err),
		}, nil
	}

	// 构建响应
	response := &miniodb.RestoreMetadataResponse{
		Success:        result.Success,
		Message:        "Metadata restored successfully",
		BackupFile:     result.BackupObjectName,
		EntriesTotal:   int32(result.EntriesTotal),
		EntriesOk:      int32(result.EntriesOK),
		EntriesSkipped: int32(result.EntriesSkipped),
		EntriesError:   int32(result.EntriesError),
		Duration:       result.Duration.String(),
		Errors:         result.Errors,
		Details:        make(map[string]string),
	}

	// 转换details
	for k, v := range result.Details {
		if str, ok := v.(string); ok {
			response.Details[k] = str
		} else {
			response.Details[k] = fmt.Sprintf("%v", v)
		}
	}

	// 如果有错误，更新消息
	if !result.Success {
		response.Message = "Metadata restore completed with errors"
	}

	log.Printf("Restore completed: success=%v, total=%d, ok=%d, errors=%d",
		result.Success, result.EntriesTotal, result.EntriesOK, result.EntriesError)

	return response, nil
}

// ListBackups 列出备份
func (s *MinIODBService) ListBackups(ctx context.Context, req *miniodb.ListBackupsRequest) (*miniodb.ListBackupsResponse, error) {
	log.Printf("Processing list backups request, days: %d", req.Days)

	// 获取恢复管理器
	recoveryManager := s.metadataMgr.GetRecoveryManager()
	if recoveryManager == nil {
		return &miniodb.ListBackupsResponse{
			Backups: []*miniodb.BackupInfo{},
			Total:   0,
		}, nil
	}

	// 设置默认天数
	days := int(req.Days)
	if days <= 0 {
		days = 30 // 默认查询30天内的备份
	}

	// 获取备份列表
	backupInfos, err := recoveryManager.ListBackups(ctx, days)
	if err != nil {
		log.Printf("ERROR: Failed to list backups: %v", err)
		return &miniodb.ListBackupsResponse{
			Backups: []*miniodb.BackupInfo{},
			Total:   0,
		}, nil
	}

	// 转换为protobuf格式
	var protoBackups []*miniodb.BackupInfo
	for _, backup := range backupInfos {
		protoBackup := &miniodb.BackupInfo{
			ObjectName:   backup.ObjectName,
			NodeId:       backup.NodeID,
			Timestamp:    timestamppb.New(backup.Timestamp),
			Size:         backup.Size,
			LastModified: timestamppb.New(backup.LastModified),
		}
		protoBackups = append(protoBackups, protoBackup)
	}

	log.Printf("Found %d backups in the last %d days", len(protoBackups), days)

	return &miniodb.ListBackupsResponse{
		Backups: protoBackups,
		Total:   int32(len(protoBackups)),
	}, nil
}

// GetMetadataStatus 获取元数据状态
func (s *MinIODBService) GetMetadataStatus(ctx context.Context, req *miniodb.GetMetadataStatusRequest) (*miniodb.GetMetadataStatusResponse, error) {
	log.Printf("Processing get metadata status request")

	// 获取备份管理器和恢复管理器
	backupManager := s.metadataMgr.GetBackupManager()
	recoveryManager := s.metadataMgr.GetRecoveryManager()

	// 构建备份状态
	backupStatus := make(map[string]string)
	var lastBackup, nextBackup *timestamppb.Timestamp
	healthStatus := "healthy"

	if backupManager == nil {
		backupStatus["status"] = "not_configured"
		healthStatus = "degraded"
	} else {
		// 获取备份统计信息
		stats := backupManager.GetStats()
		for k, v := range stats {
			backupStatus[k] = fmt.Sprintf("%v", v)
		}

		// 设置状态
		if backupManager.IsEnabled() {
			backupStatus["status"] = "enabled"
		} else {
			backupStatus["status"] = "disabled"
			healthStatus = "degraded"
		}
	}

	// 获取最新备份信息
	if recoveryManager != nil {
		if latestBackup, err := recoveryManager.GetLatestBackup(ctx); err == nil {
			lastBackup = timestamppb.New(latestBackup.Timestamp)
			backupStatus["last_backup_size"] = fmt.Sprintf("%d", latestBackup.Size)
			backupStatus["last_backup_object"] = latestBackup.ObjectName
		}
	}

	// 计算下次备份时间（这是一个估算，实际逻辑可能更复杂）
	if lastBackup != nil && backupManager != nil && backupManager.IsEnabled() {
		// 假设备份间隔为1小时，这个可以从配置中获取
		nextBackupTime := lastBackup.AsTime().Add(1 * time.Hour)
		nextBackup = timestamppb.New(nextBackupTime)
		backupStatus["next_backup_estimated"] = nextBackupTime.Format(time.RFC3339)
	}

	// 获取节点ID
	nodeID := s.metadataMgr.GetNodeID()
	if nodeID == "" {
		nodeID = s.cfg.Server.NodeID
	}

	// 检查元数据管理器健康状态
	if err := s.metadataMgr.HealthCheck(ctx); err != nil {
		healthStatus = "unhealthy"
		backupStatus["health_check_error"] = err.Error()
	}

	response := &miniodb.GetMetadataStatusResponse{
		NodeId:       nodeID,
		BackupStatus: backupStatus,
		LastBackup:   lastBackup,
		NextBackup:   nextBackup,
		HealthStatus: healthStatus,
	}

	log.Printf("Metadata status: nodeID=%s, health=%s, backups_enabled=%s",
		nodeID, healthStatus, backupStatus["status"])

	return response, nil
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

// GetStatus 获取状态
func (s *MinIODBService) GetStatus(ctx context.Context, req *miniodb.GetStatusRequest) (*miniodb.GetStatusResponse, error) {
	log.Printf("Processing get status request")

	// 收集缓冲区统计信息
	bufferStats := make(map[string]int64)
	if s.ingester != nil {
		if stats := s.ingester.GetBufferStats(); stats != nil {
			bufferStats["total_tasks"] = stats.TotalTasks
			bufferStats["completed_tasks"] = stats.CompletedTasks
			bufferStats["failed_tasks"] = stats.FailedTasks
			bufferStats["queued_tasks"] = stats.QueuedTasks
			bufferStats["active_workers"] = stats.ActiveWorkers
			bufferStats["buffer_size"] = stats.BufferSize
			bufferStats["pending_writes"] = stats.PendingWrites
			bufferStats["avg_flush_time_ms"] = stats.AvgFlushTime
			bufferStats["last_flush_time"] = stats.LastFlushTime
		}
	}

	// 收集Redis统计信息
	redisStats := make(map[string]int64)
	if s.redisClient != nil {
		if poolStats := s.redisClient.PoolStats(); poolStats != nil {
			redisStats["hits"] = int64(poolStats.Hits)
			redisStats["misses"] = int64(poolStats.Misses)
			redisStats["timeouts"] = int64(poolStats.Timeouts)
			redisStats["total_conns"] = int64(poolStats.TotalConns)
			redisStats["idle_conns"] = int64(poolStats.IdleConns)
			redisStats["stale_conns"] = int64(poolStats.StaleConns)
		}

		// 获取Redis内存使用情况
		if info, err := s.redisClient.Info(ctx, "memory").Result(); err == nil {
			lines := strings.Split(info, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "used_memory:") {
					if parts := strings.Split(line, ":"); len(parts) == 2 {
						if memUsage, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
							redisStats["used_memory_bytes"] = memUsage
						}
					}
				}
			}
		}

		// 获取Redis键数量统计
		if dbSize, err := s.redisClient.DBSize(ctx).Result(); err == nil {
			redisStats["total_keys"] = dbSize
		}
	}

	// 收集MinIO统计信息
	minioStats := make(map[string]int64)
	// 注意：这里只是示例，实际的MinIO统计信息需要通过MinIO管理API获取
	minioStats["connection_status"] = 1 // 1表示连接正常，0表示连接异常

	// 收集查询引擎统计信息
	if s.querier != nil {
		if queryStats := s.querier.GetQueryStats(); queryStats != nil {
			bufferStats["total_queries"] = queryStats.TotalQueries
			bufferStats["cache_hits"] = queryStats.CacheHits
			bufferStats["cache_misses"] = queryStats.CacheMisses
			bufferStats["error_count"] = queryStats.ErrorCount
			bufferStats["file_downloads"] = queryStats.FileDownloads
			bufferStats["file_cache_hits"] = queryStats.FileCacheHits
			bufferStats["avg_query_time_ms"] = int64(queryStats.AvgQueryTime.Milliseconds())
			bufferStats["fastest_query_ms"] = int64(queryStats.FastestQuery.Milliseconds())
			bufferStats["slowest_query_ms"] = int64(queryStats.SlowestQuery.Milliseconds())
		}
	}

	// 收集节点信息
	var nodes []*miniodb.NodeInfo

	// 当前节点信息
	currentNode := &miniodb.NodeInfo{
		Id:       s.cfg.Server.NodeID,
		Status:   "running",
		Type:     "primary", // 可以根据实际配置调整
		Address:  fmt.Sprintf("localhost:%s", strings.TrimPrefix(s.cfg.Server.GrpcPort, ":")),
		LastSeen: time.Now().Unix(),
	}
	nodes = append(nodes, currentNode)

	// 从Redis发现其他节点（如果有的话）
	if s.redisClient != nil {
		nodePattern := "service:nodes:*"
		if keys, err := s.redisClient.Keys(ctx, nodePattern).Result(); err == nil {
			for _, key := range keys {
				if nodeInfo, err := s.redisClient.HGetAll(ctx, key).Result(); err == nil {
					nodeID := strings.TrimPrefix(key, "service:nodes:")
					if nodeID != s.cfg.Server.NodeID { // 排除当前节点
						lastSeen, _ := strconv.ParseInt(nodeInfo["last_seen"], 10, 64)
						node := &miniodb.NodeInfo{
							Id:       nodeID,
							Status:   nodeInfo["status"],
							Type:     nodeInfo["type"],
							Address:  nodeInfo["address"],
							LastSeen: lastSeen,
						}
						nodes = append(nodes, node)
					}
				}
			}
		}
	}

	// 更新当前节点在Redis中的状态
	if s.redisClient != nil {
		nodeKey := fmt.Sprintf("service:nodes:%s", s.cfg.Server.NodeID)
		nodeData := map[string]interface{}{
			"status":    "running",
			"type":      "primary",
			"address":   fmt.Sprintf("localhost:%s", strings.TrimPrefix(s.cfg.Server.GrpcPort, ":")),
			"last_seen": time.Now().Unix(),
		}
		s.redisClient.HMSet(ctx, nodeKey, nodeData)
		s.redisClient.Expire(ctx, nodeKey, 60*time.Second) // 60秒过期
	}

	response := &miniodb.GetStatusResponse{
		Timestamp:   timestamppb.New(time.Now()),
		BufferStats: bufferStats,
		RedisStats:  redisStats,
		MinioStats:  minioStats,
		Nodes:       nodes,
		TotalNodes:  int32(len(nodes)),
	}

	log.Printf("Status collected: %d nodes, buffer_pending=%d, redis_keys=%d",
		len(nodes), bufferStats["pending_writes"], redisStats["total_keys"])

	return response, nil
}

// GetMetrics 获取监控指标
func (s *MinIODBService) GetMetrics(ctx context.Context, req *miniodb.GetMetricsRequest) (*miniodb.GetMetricsResponse, error) {
	log.Printf("Processing get metrics request")

	// 收集性能指标
	performanceMetrics := make(map[string]float64)

	// 查询引擎性能指标
	if s.querier != nil {
		if queryStats := s.querier.GetQueryStats(); queryStats != nil {
			// 计算查询性能指标
			if queryStats.TotalQueries > 0 {
				performanceMetrics["query_success_rate"] = float64(queryStats.TotalQueries-queryStats.ErrorCount) / float64(queryStats.TotalQueries)
				performanceMetrics["avg_query_time_seconds"] = queryStats.AvgQueryTime.Seconds()
				performanceMetrics["fastest_query_seconds"] = queryStats.FastestQuery.Seconds()
				performanceMetrics["slowest_query_seconds"] = queryStats.SlowestQuery.Seconds()
			}

			// 缓存命中率
			totalCacheRequests := queryStats.CacheHits + queryStats.CacheMisses
			if totalCacheRequests > 0 {
				performanceMetrics["cache_hit_rate"] = float64(queryStats.CacheHits) / float64(totalCacheRequests)
			}

			// 文件缓存命中率
			totalFileRequests := queryStats.FileDownloads + queryStats.FileCacheHits
			if totalFileRequests > 0 {
				performanceMetrics["file_cache_hit_rate"] = float64(queryStats.FileCacheHits) / float64(totalFileRequests)
			}
		}
	}

	// 缓冲区性能指标
	if s.ingester != nil {
		if bufferStats := s.ingester.GetBufferStats(); bufferStats != nil {
			// 计算缓冲区效率指标
			if bufferStats.TotalTasks > 0 {
				performanceMetrics["buffer_success_rate"] = float64(bufferStats.CompletedTasks) / float64(bufferStats.TotalTasks)
				performanceMetrics["buffer_error_rate"] = float64(bufferStats.FailedTasks) / float64(bufferStats.TotalTasks)
			}

			// 缓冲区处理时间
			if bufferStats.AvgFlushTime > 0 {
				performanceMetrics["avg_flush_time_seconds"] = float64(bufferStats.AvgFlushTime) / 1000.0
			}
		}
	}

	// 收集资源使用情况
	resourceUsage := make(map[string]int64)

	// Redis资源使用
	if s.redisClient != nil {
		if poolStats := s.redisClient.PoolStats(); poolStats != nil {
			resourceUsage["redis_total_connections"] = int64(poolStats.TotalConns)
			resourceUsage["redis_idle_connections"] = int64(poolStats.IdleConns)
			resourceUsage["redis_active_connections"] = int64(poolStats.TotalConns - poolStats.IdleConns)
		}

		// Redis内存使用
		if info, err := s.redisClient.Info(ctx, "memory").Result(); err == nil {
			lines := strings.Split(info, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "used_memory:") {
					if parts := strings.Split(line, ":"); len(parts) == 2 {
						if memUsage, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
							resourceUsage["redis_memory_bytes"] = memUsage
						}
					}
				} else if strings.HasPrefix(line, "used_memory_peak:") {
					if parts := strings.Split(line, ":"); len(parts) == 2 {
						if memPeak, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
							resourceUsage["redis_memory_peak_bytes"] = memPeak
						}
					}
				}
			}
		}

		// Redis键数量
		if dbSize, err := s.redisClient.DBSize(ctx).Result(); err == nil {
			resourceUsage["redis_total_keys"] = dbSize
		}
	}

	// 缓冲区资源使用
	if s.ingester != nil {
		if bufferStats := s.ingester.GetBufferStats(); bufferStats != nil {
			resourceUsage["buffer_pending_writes"] = bufferStats.PendingWrites
			resourceUsage["buffer_active_workers"] = bufferStats.ActiveWorkers
			resourceUsage["buffer_queued_tasks"] = bufferStats.QueuedTasks
			resourceUsage["buffer_total_tasks"] = bufferStats.TotalTasks
		}
	}

	// 收集系统信息
	systemInfo := make(map[string]string)
	systemInfo["node_id"] = s.cfg.Server.NodeID
	systemInfo["version"] = "1.0.0"
	systemInfo["build_time"] = time.Now().Format(time.RFC3339)
	systemInfo["uptime_seconds"] = fmt.Sprintf("%.0f", time.Since(time.Now()).Seconds()) // 这里需要实际的启动时间

	// 配置信息
	systemInfo["grpc_port"] = s.cfg.Server.GrpcPort
	systemInfo["rest_port"] = s.cfg.Server.RestPort
	systemInfo["redis_mode"] = "standalone" // 可以从Redis客户端获取

	// 表统计信息
	if s.tableManager != nil {
		if tableList, err := s.tableManager.ListTables(ctx, "*"); err == nil {
			systemInfo["total_tables"] = fmt.Sprintf("%d", len(tableList))
		}
	}

	// 元数据管理器信息
	if s.metadataMgr != nil {
		stats := s.metadataMgr.GetStats()
		for k, v := range stats {
			systemInfo[fmt.Sprintf("metadata_%s", k)] = fmt.Sprintf("%v", v)
		}

		// 备份状态
		if backupManager := s.metadataMgr.GetBackupManager(); backupManager != nil {
			if backupManager.IsEnabled() {
				systemInfo["backup_enabled"] = "true"
			} else {
				systemInfo["backup_enabled"] = "false"
			}
		}
	}

	// 计算一些高级指标
	if performanceMetrics["query_success_rate"] > 0 {
		if performanceMetrics["query_success_rate"] >= 0.95 {
			systemInfo["service_health"] = "excellent"
		} else if performanceMetrics["query_success_rate"] >= 0.9 {
			systemInfo["service_health"] = "good"
		} else if performanceMetrics["query_success_rate"] >= 0.8 {
			systemInfo["service_health"] = "fair"
		} else {
			systemInfo["service_health"] = "poor"
		}
	} else {
		systemInfo["service_health"] = "unknown"
	}

	response := &miniodb.GetMetricsResponse{
		Timestamp:          timestamppb.New(time.Now()),
		PerformanceMetrics: performanceMetrics,
		ResourceUsage:      resourceUsage,
		SystemInfo:         systemInfo,
	}

	log.Printf("Metrics collected: %d performance metrics, %d resource metrics, health=%s",
		len(performanceMetrics), len(resourceUsage), systemInfo["service_health"])

	return response, nil
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
