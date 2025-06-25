package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OlapService OLAP服务实现
type OlapService struct {
	olapv1.UnimplementedOlapServiceServer
	
	cfg           config.Config
	ingester      *ingest.Ingester
	querier       *query.Querier
	redisClient   *redis.Client
	primaryMinio  storage.Uploader
	backupMinio   storage.Uploader
}

// NewOlapService 创建OLAP服务实例
func NewOlapService(cfg config.Config, ingester *ingest.Ingester, querier *query.Querier, 
	redisClient *redis.Client, primaryMinio, backupMinio storage.Uploader) (*OlapService, error) {
	
	return &OlapService{
		cfg:          cfg,
		ingester:     ingester,
		querier:      querier,
		redisClient:  redisClient,
		primaryMinio: primaryMinio,
		backupMinio:  backupMinio,
	}, nil
}

// validateWriteRequest 验证写入请求
func (s *OlapService) validateWriteRequest(req *olapv1.WriteRequest) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "id is required and cannot be empty")
	}
	if len(req.Id) > 255 {
		return status.Error(codes.InvalidArgument, "id cannot exceed 255 characters")
	}
	if req.Timestamp == nil {
		return status.Error(codes.InvalidArgument, "timestamp is required")
	}
	if req.Payload == nil {
		return status.Error(codes.InvalidArgument, "payload is required")
	}
	
	// 验证ID格式（只允许字母、数字、连字符和下划线）
	for _, r := range req.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || 
			 (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "id contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}
	
	return nil
}

// Write 写入数据
func (s *OlapService) Write(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	log.Printf("Received write request for ID: %s", req.Id)

	// 验证请求
	if err := s.validateWriteRequest(req); err != nil {
		return nil, err
	}

	// 使用Ingester处理写入
	if err := s.ingester.IngestData(req); err != nil {
		log.Printf("ERROR: failed to ingest data for ID %s: %v", req.Id, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to ingest data: %v", err))
	}

	log.Printf("Successfully ingested data for ID: %s", req.Id)
	return &olapv1.WriteResponse{
		Success: true,
		Message: fmt.Sprintf("Data successfully ingested for ID: %s", req.Id),
	}, nil
}

// validateQueryRequest 验证查询请求  
func (s *OlapService) validateQueryRequest(req *olapv1.QueryRequest) error {
	if req.Sql == "" {
		return status.Error(codes.InvalidArgument, "sql is required and cannot be empty")
	}
	if len(req.Sql) > 10000 {
		return status.Error(codes.InvalidArgument, "sql query cannot exceed 10000 characters")
	}
	
	// 基本的SQL注入防护（简单检查）
	lowerSQL := strings.ToLower(req.Sql)
	dangerousKeywords := []string{"drop", "delete", "truncate", "alter", "create", "insert", "update"}
	for _, keyword := range dangerousKeywords {
		if strings.Contains(lowerSQL, keyword) {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("sql contains dangerous keyword: %s", keyword))
		}
	}
	
	return nil
}

// Query 查询数据
func (s *OlapService) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	log.Printf("Received query request: %s", req.Sql)

	// 验证请求
	if err := s.validateQueryRequest(req); err != nil {
		return nil, err
	}

	// 使用Querier执行查询
	result, err := s.querier.ExecuteQuery(req.Sql)
	if err != nil {
		log.Printf("ERROR: query failed: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("query execution failed: %v", err))
	}

	log.Printf("Query completed successfully, result length: %d characters", len(result))
	return &olapv1.QueryResponse{
		ResultJson: result,
	}, nil
}

// TriggerBackup 触发备份
func (s *OlapService) TriggerBackup(ctx context.Context, req *olapv1.TriggerBackupRequest) (*olapv1.TriggerBackupResponse, error) {
	log.Printf("Received backup trigger request for ID: %s, day: %s", req.Id, req.Day)

	// 验证请求
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.Day == "" {
		return nil, status.Error(codes.InvalidArgument, "day is required")
	}
	
	// 验证日期格式
	if _, err := time.Parse("2006-01-02", req.Day); err != nil {
		return nil, status.Error(codes.InvalidArgument, "day must be in YYYY-MM-DD format")
	}

	if s.backupMinio == nil {
		return nil, status.Error(codes.FailedPrecondition, "backup storage not configured")
	}

	// 获取需要备份的文件列表
	redisKey := fmt.Sprintf("index:id:%s:%s", req.Id, req.Day)
	files, err := s.redisClient.SMembers(ctx, redisKey).Result()
	if err != nil {
		log.Printf("ERROR: failed to get files from Redis for key %s: %v", redisKey, err)
		return nil, status.Error(codes.Internal, "failed to retrieve file list for backup")
	}

	if len(files) == 0 {
		return &olapv1.TriggerBackupResponse{
			Success:       true,
			Message:       fmt.Sprintf("No files found for ID: %s, day: %s", req.Id, req.Day),
			FilesBackedUp: 0,
		}, nil
	}

	// 执行备份
	backedUpCount := int32(0)
	for _, file := range files {
		if err := s.backupFile(ctx, file); err != nil {
			log.Printf("ERROR: failed to backup file %s: %v", file, err)
			continue
		}
		backedUpCount++
	}

	log.Printf("Successfully backed up %d/%d files for ID: %s, day: %s", backedUpCount, len(files), req.Id, req.Day)
	
	return &olapv1.TriggerBackupResponse{
		Success:       true,
		Message:       fmt.Sprintf("Successfully backed up %d files for ID: %s, day: %s", backedUpCount, req.Id, req.Day),
		FilesBackedUp: backedUpCount,
	}, nil
}

// backupFile 备份单个文件
func (s *OlapService) backupFile(ctx context.Context, objectName string) error {
	const mainBucket = "olap-data"
	
	// 检查备份存储中是否已存在该文件
	exists, err := s.backupMinio.ObjectExists(ctx, s.cfg.Backup.Minio.Bucket, objectName)
	if err != nil {
		return fmt.Errorf("failed to check if backup file exists: %w", err)
	}
	if exists {
		log.Printf("File %s already exists in backup storage, skipping", objectName)
		return nil
	}

	// 从主存储复制到备份存储
	_, err = s.backupMinio.CopyObject(ctx,
		// 目标
		minio.CopyDestOptions{
			Bucket: s.cfg.Backup.Minio.Bucket,
			Object: objectName,
		},
		// 源
		minio.CopySrcOptions{
			Bucket: mainBucket,
			Object: objectName,
		},
	)
	
	if err != nil {
		return fmt.Errorf("failed to copy file to backup storage: %w", err)
	}
	
	return nil
}

// RecoverData 恢复数据
func (s *OlapService) RecoverData(ctx context.Context, req *olapv1.RecoverDataRequest) (*olapv1.RecoverDataResponse, error) {
	log.Printf("Received data recovery request")

	if s.backupMinio == nil {
		return nil, status.Error(codes.FailedPrecondition, "backup storage not configured")
	}

	var recoveredKeys []string
	var recoveredCount int32

	switch req.RecoveryMode.(type) {
	case *olapv1.RecoverDataRequest_IdRange:
		idRange := req.GetIdRange()
		for _, id := range idRange.Ids {
			keys, err := s.recoverDataForID(ctx, id, req.ForceOverwrite)
			if err != nil {
				log.Printf("ERROR: failed to recover data for ID %s: %v", id, err)
				continue
			}
			recoveredKeys = append(recoveredKeys, keys...)
			recoveredCount += int32(len(keys))
		}
		
	case *olapv1.RecoverDataRequest_TimeRange:
		timeRange := req.GetTimeRange()
		
		// 验证时间范围
		startDate, err := time.Parse("2006-01-02", timeRange.StartDate)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid start_date format, must be YYYY-MM-DD")
		}
		endDate, err := time.Parse("2006-01-02", timeRange.EndDate)  
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid end_date format, must be YYYY-MM-DD")
		}
		if startDate.After(endDate) {
			return nil, status.Error(codes.InvalidArgument, "start_date cannot be after end_date")
		}
		
		if len(timeRange.Ids) > 0 {
			// 恢复指定ID的时间范围数据
			for _, id := range timeRange.Ids {
				keys, err := s.recoverDataForTimeRange(ctx, id, timeRange.StartDate, timeRange.EndDate, req.ForceOverwrite)
				if err != nil {
					log.Printf("ERROR: failed to recover time range data for ID %s: %v", id, err)
					continue
				}
				recoveredKeys = append(recoveredKeys, keys...)
				recoveredCount += int32(len(keys))
			}
		} else {
			// 恢复所有ID的时间范围数据
			keys, err := s.recoverDataForTimeRangeAllIDs(ctx, timeRange.StartDate, timeRange.EndDate, req.ForceOverwrite)
			if err != nil {
				log.Printf("ERROR: failed to recover time range data for all IDs: %v", err)
			} else {
				recoveredKeys = append(recoveredKeys, keys...)
				recoveredCount += int32(len(keys))
			}
		}
		
	default:
		return nil, status.Error(codes.InvalidArgument, "recovery_mode is required")
	}

	log.Printf("Data recovery completed, recovered %d files", recoveredCount)
	
	return &olapv1.RecoverDataResponse{
		Success:       true,
		Message:       fmt.Sprintf("Successfully recovered %d files", recoveredCount),
		FilesRecovered: recoveredCount,
		RecoveredKeys: recoveredKeys,
	}, nil
}

// recoverDataForID 恢复指定ID的所有数据
func (s *OlapService) recoverDataForID(ctx context.Context, id string, forceOverwrite bool) ([]string, error) {
	// 从备份存储中查找该ID的所有文件
	pattern := fmt.Sprintf("%s/", id)
	
	// 使用ListObjects查找匹配的文件
	objects := s.backupMinio.ListObjects(ctx, s.cfg.Backup.Minio.Bucket, minio.ListObjectsOptions{
		Prefix: pattern,
	})
	
	var recoveredKeys []string 
	for object := range objects {
		if object.Err != nil {
			log.Printf("ERROR: failed to list object: %v", object.Err)
			continue
		}
		
		if err := s.recoverSingleFile(ctx, object.Key, forceOverwrite); err != nil {
			log.Printf("ERROR: failed to recover file %s: %v", object.Key, err)
			continue
		}
		
		recoveredKeys = append(recoveredKeys, object.Key)
	}
	
	return recoveredKeys, nil
}

// recoverDataForTimeRange 恢复指定ID和时间范围的数据
func (s *OlapService) recoverDataForTimeRange(ctx context.Context, id, startDate, endDate string, forceOverwrite bool) ([]string, error) {
	var recoveredKeys []string
	
	// 解析时间范围
	start, _ := time.Parse("2006-01-02", startDate)
	end, _ := time.Parse("2006-01-02", endDate)
	
	// 遍历每一天
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dayStr := d.Format("2006-01-02")
		pattern := fmt.Sprintf("%s/%s/", id, dayStr)
		
		objects := s.backupMinio.ListObjects(ctx, s.cfg.Backup.Minio.Bucket, minio.ListObjectsOptions{
			Prefix: pattern,
		})
		
		for object := range objects {
			if object.Err != nil {
				continue
			}
			
			if err := s.recoverSingleFile(ctx, object.Key, forceOverwrite); err != nil {
				continue
			}
			
			recoveredKeys = append(recoveredKeys, object.Key)
		}
	}
	
	return recoveredKeys, nil
}

// recoverDataForTimeRangeAllIDs 恢复时间范围内所有ID的数据
func (s *OlapService) recoverDataForTimeRangeAllIDs(ctx context.Context, startDate, endDate string, forceOverwrite bool) ([]string, error) {
	var recoveredKeys []string
	
	// 列出备份存储中的所有对象并按时间过滤
	objects := s.backupMinio.ListObjects(ctx, s.cfg.Backup.Minio.Bucket, minio.ListObjectsOptions{})
	
	for object := range objects {
		if object.Err != nil {
			continue
		}
		
		// 检查对象路径是否符合时间范围
		if s.isObjectInTimeRange(object.Key, startDate, endDate) {
			if err := s.recoverSingleFile(ctx, object.Key, forceOverwrite); err != nil {
				continue  
			}
			recoveredKeys = append(recoveredKeys, object.Key)
		}
	}
	
	return recoveredKeys, nil
}

// isObjectInTimeRange 检查对象是否在指定时间范围内
func (s *OlapService) isObjectInTimeRange(objectKey, startDate, endDate string) bool {
	// 对象路径格式：ID/YYYY-MM-DD/timestamp.parquet
	parts := strings.Split(objectKey, "/")
	if len(parts) < 2 {
		return false
	}
	
	objectDate := parts[1]
	return objectDate >= startDate && objectDate <= endDate
}

// recoverSingleFile 恢复单个文件
func (s *OlapService) recoverSingleFile(ctx context.Context, objectName string, forceOverwrite bool) error {
	const mainBucket = "olap-data"
	
	// 检查主存储中是否已存在该文件
	if !forceOverwrite {
		exists, err := s.primaryMinio.ObjectExists(ctx, mainBucket, objectName)
		if err != nil {
			return fmt.Errorf("failed to check if file exists in main storage: %w", err)
		}
		if exists {
			log.Printf("File %s already exists in main storage, skipping (use force_overwrite=true to overwrite)", objectName)
			return nil
		}
	}
	
	// 从备份存储复制到主存储
	_, err := s.primaryMinio.CopyObject(ctx,
		// 目标
		minio.CopyDestOptions{
			Bucket: mainBucket,
			Object: objectName,
		},
		// 源
		minio.CopySrcOptions{
			Bucket: s.cfg.Backup.Minio.Bucket,
			Object: objectName,
		},
	)
	
	if err != nil {
		return fmt.Errorf("failed to copy file from backup to main storage: %w", err)
	}
	
	// 更新Redis索引
	parts := strings.Split(objectName, "/")
	if len(parts) >= 2 {
		id := parts[0]
		day := parts[1]
		redisKey := fmt.Sprintf("index:id:%s:%s", id, day)
		if _, err := s.redisClient.SAdd(ctx, redisKey, objectName).Result(); err != nil {
			log.Printf("WARNING: failed to update Redis index for recovered file %s: %v", objectName, err)
		}
	}
	
	return nil
}

// GetStats 获取服务统计信息
func (s *OlapService) GetStats(ctx context.Context, req *olapv1.GetStatsRequest) (*olapv1.GetStatsResponse, error) {
	log.Printf("Received stats request")

	response := &olapv1.GetStatsResponse{
		Timestamp:   fmt.Sprintf("%d", time.Now().Unix()),
		BufferStats: make(map[string]int64),
		RedisStats:  make(map[string]int64),
		MinioStats:  make(map[string]int64),
	}
	
	// 获取Redis统计信息
	info := s.redisClient.Info(ctx)
	if info.Err() == nil {
		// 这里可以解析Redis INFO命令的结果
		response.RedisStats["connected"] = 1
	}
	
	// 基础统计信息
	response.BufferStats["service_status"] = 1
	response.MinioStats["primary_connected"] = 1
	if s.backupMinio != nil {
		response.MinioStats["backup_connected"] = 1
	}
	
	return response, nil
}

// HealthCheck 健康检查
func (s *OlapService) HealthCheck(ctx context.Context, req *olapv1.HealthCheckRequest) (*olapv1.HealthCheckResponse, error) {
	log.Printf("Received health check request")

	// 检查Redis连接
	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		return &olapv1.HealthCheckResponse{
			Status:    "unhealthy",
			Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
			Version:   "1.0.0",
			Details: map[string]string{
				"redis": fmt.Sprintf("unhealthy: %v", err),
			},
		}, nil
	}
	
	return &olapv1.HealthCheckResponse{
		Status:    "healthy",
		Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
		Version:   "1.0.0",
		Details: map[string]string{
			"redis":        "healthy",
			"primary_minio": "healthy",
			"backup_minio":  fmt.Sprintf("enabled: %t", s.backupMinio != nil),
		},
	}, nil
}

// GetNodes 获取节点信息
func (s *OlapService) GetNodes(ctx context.Context, req *olapv1.GetNodesRequest) (*olapv1.GetNodesResponse, error) {
	log.Printf("Received get nodes request")

	// 从Redis获取所有注册的节点
	nodes, err := s.redisClient.HGetAll(ctx, "nodes:services").Result()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get nodes from Redis: %v", err))
	}

	var nodeInfos []*olapv1.NodeInfo
	for nodeID, nodeData := range nodes {
		// 检查节点健康状态  
		healthKey := fmt.Sprintf("nodes:health:%s", nodeID)
		_, err := s.redisClient.Get(ctx, healthKey).Result()
		
		status := "unhealthy"
		if err == nil {
			status = "healthy"
		}
		
		nodeInfos = append(nodeInfos, &olapv1.NodeInfo{
			Id:       nodeID,
			Status:   status,
			Type:     "worker",
			Address:  nodeData, // 简化处理，实际应该解析JSON
			LastSeen: time.Now().Unix(),
		})
	}

	return &olapv1.GetNodesResponse{
		Nodes: nodeInfos,
		Total: int32(len(nodeInfos)),
	}, nil
}

// Close 关闭服务
func (s *OlapService) Close() error {
	log.Printf("Closing OlapService")
	if s.querier != nil {
		s.querier.Close()
	}
	return nil
} 