package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/security"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"google.golang.org/grpc"
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
	
	// 认证相关
	authManager     *security.AuthManager
	grpcInterceptor *security.GRPCInterceptor
	grpcServer      *grpc.Server
}

// NewServer creates a new gRPC server
func NewServer(redisClient *redis.Client, primaryMinio storage.Uploader, backupMinio storage.Uploader, cfg config.Config) (*Server, error) {
	buf := buffer.NewSharedBuffer(redisClient, primaryMinio, backupMinio, cfg.Backup.Minio.Bucket, 10, 5*time.Second)

	querier, err := query.NewQuerier(redisClient, primaryMinio, cfg.Minio, buf)
	if err != nil {
		return nil, err
	}

	// 初始化认证管理器
	authConfig := &security.AuthConfig{
		Mode:            cfg.Security.Mode,
		JWTSecret:       cfg.Security.JWTSecret,
		TokenExpiration: 24 * time.Hour,
		Issuer:          "miniodb",
		Audience:        "miniodb-grpc",
		ValidTokens:     cfg.Security.ValidTokens,
	}

	authManager, err := security.NewAuthManager(authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth manager: %w", err)
	}

	// 创建gRPC拦截器
	grpcInterceptor := security.NewGRPCInterceptor(authManager)

	server := &Server{
		ingester:        ingest.NewIngester(buf),
		querier:         querier,
		buffer:          buf,
		redisClient:     redisClient,
		primaryMinio:    primaryMinio,
		backupMinio:     backupMinio,
		cfg:             cfg,
		authManager:     authManager,
		grpcInterceptor: grpcInterceptor,
	}

	// 创建gRPC服务器，集成拦截器
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(grpcInterceptor.UnaryServerInterceptor()),
		grpc.StreamInterceptor(grpcInterceptor.StreamServerInterceptor()),
	)

	// 注册服务
	olapv1.RegisterOlapServiceServer(grpcServer, server)
	server.grpcServer = grpcServer

	log.Printf("gRPC server initialized with authentication mode: %s", cfg.Security.Mode)

	return server, nil
}

// Start 启动gRPC服务器
func (s *Server) Start(port string) error {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", port, err)
	}

	log.Printf("gRPC server starting on port %s", port)
	if err := s.grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC server: %w", err)
	}

	return nil
}

// Stop 停止gRPC服务器
func (s *Server) Stop() {
	if s.grpcServer != nil {
		log.Println("Stopping gRPC server...")
		s.grpcServer.GracefulStop()
	}
}

// Write implements the Write rpc
func (s *Server) Write(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("Write")
	
	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("Write request from user: %s (ID: %s) for data ID: %s", user.Username, user.ID, req.Id)
		}
	} else {
		log.Printf("Received Write request for ID: %s", req.Id)
	}

	err := s.ingester.IngestData(req)
	if err != nil {
		log.Printf("Failed to ingest data: %v", err)
		grpcMetrics.Finish("error")
		return &olapv1.WriteResponse{Success: false, Message: "Failed to ingest data"}, nil
	}

	grpcMetrics.Finish("success")
	return &olapv1.WriteResponse{Success: true, Message: "Write request received"}, nil
}

// Query implements the Query rpc
func (s *Server) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("Query")
	
	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("Query request from user: %s (ID: %s) with SQL: %s", user.Username, user.ID, req.Sql)
		}
	} else {
		log.Printf("Received Query request with SQL: %s", req.Sql)
	}

	result, err := s.querier.ExecuteQuery(req.Sql)
	if err != nil {
		grpcMetrics.Finish("error")
		return nil, err
	}

	grpcMetrics.Finish("success")
	return &olapv1.QueryResponse{ResultJson: result}, nil
}

// TriggerBackup implements the manual backup RPC
func (s *Server) TriggerBackup(ctx context.Context, req *olapv1.TriggerBackupRequest) (*olapv1.TriggerBackupResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("TriggerBackup")
	
	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("TriggerBackup request from user: %s (ID: %s) for ID %s, Day %s", user.Username, user.ID, req.Id, req.Day)
		}
	} else {
		log.Printf("Received TriggerBackup request for ID %s, Day %s", req.Id, req.Day)
	}

	if s.backupMinio == nil {
		grpcMetrics.Finish("error")
		return &olapv1.TriggerBackupResponse{Success: false, Message: "Backup is not enabled in configuration"}, nil
	}

	redisKey := fmt.Sprintf("index:id:%s:%s", req.Id, req.Day)
	objectNames, err := s.redisClient.SMembers(ctx, redisKey).Result()
	if err != nil {
		grpcMetrics.Finish("error")
		return nil, fmt.Errorf("failed to get objects from redis: %w", err)
	}

	if len(objectNames) == 0 {
		grpcMetrics.Finish("success")
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

	grpcMetrics.Finish("success")
	return &olapv1.TriggerBackupResponse{
		Success:       true,
		Message:       fmt.Sprintf("Backup process completed. Backed up %d files.", successCount),
		FilesBackedUp: successCount,
	}, nil
}

// RecoverData implements the data recovery RPC
func (s *Server) RecoverData(ctx context.Context, req *olapv1.RecoverDataRequest) (*olapv1.RecoverDataResponse, error) {
	grpcMetrics := metrics.NewGRPCMetrics("RecoverData")
	defer func() {
		grpcMetrics.Finish("success")
	}()

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

// HealthCheck implements the health check RPC
func (s *Server) HealthCheck(ctx context.Context, req *olapv1.HealthCheckRequest) (*olapv1.HealthCheckResponse, error) {
	log.Printf("Received HealthCheck request")

	// 检查Redis连接
	details := make(map[string]string)
	status := "healthy"

	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		status = "unhealthy"
		details["redis"] = "connection failed"
	} else {
		details["redis"] = "connected"
	}

	// 检查MinIO连接
	if s.primaryMinio != nil {
		if exists, err := s.primaryMinio.BucketExists(ctx, "olap-data"); err != nil {
			details["minio_primary"] = "connection failed"
			if status == "healthy" {
				status = "degraded"
			}
		} else if exists {
			details["minio_primary"] = "connected"
		} else {
			details["minio_primary"] = "bucket not found"
		}
	}

	if s.backupMinio != nil {
		if exists, err := s.backupMinio.BucketExists(ctx, s.cfg.Backup.Minio.Bucket); err != nil {
			details["minio_backup"] = "connection failed"
		} else if exists {
			details["minio_backup"] = "connected"
		} else {
			details["minio_backup"] = "bucket not found"
		}
	}

	return &olapv1.HealthCheckResponse{
		Status:    status,
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   "1.0.0",
		Details:   details,
	}, nil
}

// GetStats implements the system statistics RPC
func (s *Server) GetStats(ctx context.Context, req *olapv1.GetStatsRequest) (*olapv1.GetStatsResponse, error) {
	log.Printf("Received GetStats request")

	// 获取缓冲区统计
	bufferStats := make(map[string]int64)
	if s.buffer != nil {
		bufferStats["size"] = int64(s.buffer.Size())
		bufferStats["pending_writes"] = int64(s.buffer.PendingWrites())
	}

	// 获取Redis统计
	redisStats := make(map[string]int64)
	if _, err := s.redisClient.Info(ctx).Result(); err == nil {
		// 解析Redis info信息
		redisStats["connected_clients"] = 1 // 简化统计
		redisStats["used_memory"] = 0       // 需要解析info字符串
	}

	// 获取MinIO统计（简化版本）
	minioStats := make(map[string]int64)
	minioStats["primary_connected"] = 0
	minioStats["backup_connected"] = 0
	
	if s.primaryMinio != nil {
		if _, err := s.primaryMinio.BucketExists(ctx, "olap-data"); err == nil {
			minioStats["primary_connected"] = 1
		}
	}
	
	if s.backupMinio != nil {
		if _, err := s.backupMinio.BucketExists(ctx, s.cfg.Backup.Minio.Bucket); err == nil {
			minioStats["backup_connected"] = 1
		}
	}

	return &olapv1.GetStatsResponse{
		Timestamp:   time.Now().Format(time.RFC3339),
		BufferStats: bufferStats,
		RedisStats:  redisStats,
		MinioStats:  minioStats,
	}, nil
}

// GetNodes implements the cluster nodes information RPC
func (s *Server) GetNodes(ctx context.Context, req *olapv1.GetNodesRequest) (*olapv1.GetNodesResponse, error) {
	log.Printf("Received GetNodes request")

	// 构建节点信息
	nodes := []*olapv1.NodeInfo{
		{
			Id:       s.cfg.Server.NodeID,
			Status:   "healthy",
			Type:     "local",
			Address:  fmt.Sprintf(":%s", s.cfg.Server.GRPCPort),
			LastSeen: time.Now().Unix(),
		},
	}

	// 这里应该从服务注册中心获取其他节点信息
	// 由于当前没有实现服务发现，只返回本地节点

	return &olapv1.GetNodesResponse{
		Nodes: nodes,
		Total: int32(len(nodes)),
	}, nil
}
