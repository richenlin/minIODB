package grpc

import (
	"context"
	"fmt"
	"log"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
)

// Server is the implementation of the OlapServiceServer
type Server struct {
	olapv1.UnimplementedOlapServiceServer
	ingester     *ingest.Ingester
	querier      *query.Querier
	buffer       *buffer.SharedBuffer
	redisClient  *redis.Client
	primaryMinio storage.Uploader
	backupMinio  storage.Uploader
	cfg          config.Config
}

// NewServer creates a new gRPC server
func NewServer(redisClient *redis.Client, primaryMinio storage.Uploader, backupMinio storage.Uploader, cfg config.Config) (*Server, error) {
	buf := buffer.NewSharedBuffer(redisClient, primaryMinio, backupMinio, cfg.Backup.Minio.Bucket, 10, 5*time.Second)

	querier, err := query.NewQuerier(redisClient, primaryMinio, cfg.Minio, buf)
	if err != nil {
		return nil, err
	}

	return &Server{
		ingester:     ingest.NewIngester(buf),
		querier:      querier,
		buffer:       buf,
		redisClient:  redisClient,
		primaryMinio: primaryMinio,
		backupMinio:  backupMinio,
		cfg:          cfg,
	}, nil
}

// Write implements the Write rpc
func (s *Server) Write(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	log.Printf("Received Write request for ID: %s", req.Id)

	err := s.ingester.IngestData(req)
	if err != nil {
		log.Printf("Failed to ingest data: %v", err)
		return &olapv1.WriteResponse{Success: false, Message: "Failed to ingest data"}, nil
	}

	return &olapv1.WriteResponse{Success: true, Message: "Write request received"}, nil
}

// Query implements the Query rpc
func (s *Server) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	log.Printf("Received Query request with SQL: %s", req.Sql)

	result, err := s.querier.ExecuteQuery(req.Sql)
	if err != nil {
		return nil, err
	}

	return &olapv1.QueryResponse{ResultJson: result}, nil
}

// TriggerBackup implements the manual backup RPC
func (s *Server) TriggerBackup(ctx context.Context, req *olapv1.TriggerBackupRequest) (*olapv1.TriggerBackupResponse, error) {
	log.Printf("Received TriggerBackup request for ID %s, Day %s", req.Id, req.Day)

	if s.backupMinio == nil {
		return &olapv1.TriggerBackupResponse{Success: false, Message: "Backup is not enabled in configuration"}, nil
	}

	redisKey := fmt.Sprintf("index:id:%s:%s", req.Id, req.Day)
	objectNames, err := s.redisClient.SMembers(ctx, redisKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get objects from redis: %w", err)
	}

	if len(objectNames) == 0 {
		return &olapv1.TriggerBackupResponse{Success: true, Message: "No objects found to back up", FilesBackedUp: 0}, nil
	}

	var successCount int32 = 0
	for _, objName := range objectNames {
		src := minio.CopySrcOptions{
			Bucket: "olap-data", // Primary bucket
			Object: objName,
		}
		dst := minio.CopyDestOptions{
			Bucket: s.cfg.Backup.Minio.Bucket,
			Object: objName,
		}
		_, err := s.backupMinio.CopyObject(ctx, dst, src)
		if err != nil {
			log.Printf("ERROR: failed to back up object %s: %v", objName, err)
			// Decide if to continue or fail the whole request
		} else {
			successCount++
		}
	}

	return &olapv1.TriggerBackupResponse{
		Success:       true,
		Message:       fmt.Sprintf("Backup process completed. Backed up %d files.", successCount),
		FilesBackedUp: successCount,
	}, nil
}

// RecoverData implements the data recovery RPC
func (s *Server) RecoverData(ctx context.Context, req *olapv1.RecoverDataRequest) (*olapv1.RecoverDataResponse, error) {
	log.Printf("Received RecoverData request")

	if s.backupMinio == nil {
		return &olapv1.RecoverDataResponse{
			Success: false,
			Message: "Backup is not enabled in configuration",
		}, nil
	}

	var dataKeys []string
	var err error

	// 根据不同的恢复模式获取需要恢复的数据键
	switch mode := req.RecoveryMode.(type) {
	case *olapv1.RecoverDataRequest_NodeId:
		dataKeys, err = s.getDataKeysByNodeId(ctx, mode.NodeId)
	case *olapv1.RecoverDataRequest_IdRange:
		dataKeys, err = s.getDataKeysByIdRange(ctx, mode.IdRange)
	case *olapv1.RecoverDataRequest_TimeRange:
		dataKeys, err = s.getDataKeysByTimeRange(ctx, mode.TimeRange)
	default:
		return &olapv1.RecoverDataResponse{
			Success: false,
			Message: "Invalid recovery mode specified",
		}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get data keys: %w", err)
	}

	if len(dataKeys) == 0 {
		return &olapv1.RecoverDataResponse{
			Success:        true,
			Message:        "No data found to recover",
			FilesRecovered: 0,
		}, nil
	}

	log.Printf("Found %d data keys to recover", len(dataKeys))

	var totalFilesRecovered int32 = 0
	var recoveredKeys []string

	// 对每个数据键进行恢复
	for _, dataKey := range dataKeys {
		filesRecovered, err := s.recoverDataForKey(ctx, dataKey, req.ForceOverwrite)
		if err != nil {
			log.Printf("ERROR: failed to recover data for key %s: %v", dataKey, err)
			continue
		}
		totalFilesRecovered += filesRecovered
		if filesRecovered > 0 {
			recoveredKeys = append(recoveredKeys, dataKey)
		}
	}

	return &olapv1.RecoverDataResponse{
		Success:        true,
		Message:        fmt.Sprintf("Recovery completed. Recovered %d files for %d data keys.", totalFilesRecovered, len(recoveredKeys)),
		FilesRecovered: totalFilesRecovered,
		RecoveredKeys:  recoveredKeys,
	}, nil
}

// getDataKeysByNodeId 根据节点ID获取数据键
func (s *Server) getDataKeysByNodeId(ctx context.Context, nodeId string) ([]string, error) {
	nodeDataKey := fmt.Sprintf("node:data:%s", nodeId)
	return s.redisClient.SMembers(ctx, nodeDataKey).Result()
}

// getDataKeysByIdRange 根据ID范围获取数据键
func (s *Server) getDataKeysByIdRange(ctx context.Context, idRange *olapv1.IdRangeFilter) ([]string, error) {
	var allKeys []string

	// 处理具体的ID列表
	for _, id := range idRange.Ids {
		pattern := fmt.Sprintf("index:id:%s:*", id)
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			return nil, err
		}
		allKeys = append(allKeys, keys...)
	}

	// 处理ID模式匹配
	if idRange.IdPattern != "" {
		pattern := fmt.Sprintf("index:id:%s:*", idRange.IdPattern)
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			return nil, err
		}
		allKeys = append(allKeys, keys...)
	}

	return allKeys, nil
}

// getDataKeysByTimeRange 根据时间范围获取数据键
func (s *Server) getDataKeysByTimeRange(ctx context.Context, timeRange *olapv1.TimeRangeFilter) ([]string, error) {
	var allKeys []string

	// 解析时间范围
	startDate, err := time.Parse("2006-01-02", timeRange.StartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start date format: %w", err)
	}
	endDate, err := time.Parse("2006-01-02", timeRange.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid end date format: %w", err)
	}

	// 生成日期范围内的所有日期
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dayStr := d.Format("2006-01-02")

		if len(timeRange.Ids) > 0 {
			// 如果指定了ID列表，只查询这些ID
			for _, id := range timeRange.Ids {
				pattern := fmt.Sprintf("index:id:%s:%s", id, dayStr)
				keys, err := s.redisClient.Keys(ctx, pattern).Result()
				if err != nil {
					return nil, err
				}
				allKeys = append(allKeys, keys...)
			}
		} else {
			// 否则查询该日期的所有数据
			pattern := fmt.Sprintf("index:id:*:%s", dayStr)
			keys, err := s.redisClient.Keys(ctx, pattern).Result()
			if err != nil {
				return nil, err
			}
			allKeys = append(allKeys, keys...)
		}
	}

	return allKeys, nil
}

// recoverDataForKey 恢复特定数据键的所有文件
func (s *Server) recoverDataForKey(ctx context.Context, dataKey string, forceOverwrite bool) (int32, error) {
	// 从备份存储获取文件列表
	backupFiles, err := s.redisClient.SMembers(ctx, dataKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get backup files from redis: %w", err)
	}

	if len(backupFiles) == 0 {
		log.Printf("No backup files found for key %s", dataKey)
		return 0, nil
	}

	var successCount int32 = 0

	for _, fileName := range backupFiles {
		// 检查主存储中是否已存在该文件
		if !forceOverwrite {
			exists, err := s.primaryMinio.BucketExists(ctx, "olap-data")
			if err == nil && exists {
				// 这里简化处理，实际应该检查具体文件是否存在
				log.Printf("File %s may already exist in primary storage, skipping (use force_overwrite to override)", fileName)
				continue
			}
		}

		// 从备份存储复制到主存储
		src := minio.CopySrcOptions{
			Bucket: s.cfg.Backup.Minio.Bucket,
			Object: fileName,
		}
		dst := minio.CopyDestOptions{
			Bucket: "olap-data", // 主存储桶
			Object: fileName,
		}

		_, err := s.primaryMinio.CopyObject(ctx, dst, src)
		if err != nil {
			log.Printf("ERROR: failed to recover file %s: %v", fileName, err)
			continue
		}

		successCount++
		log.Printf("Successfully recovered file %s", fileName)
	}

	return successCount, nil
}

// Stop gracefully stops the server's components
func (s *Server) Stop() {
	s.buffer.Stop()
	s.querier.Close()
}
