package service

import (
	"context"
	"fmt"
	"log"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/pool"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OlapService OLAP服务实现
type OlapService struct {
	olapv1.UnimplementedOlapServiceServer
	
	cfg         config.Config
	poolManager *pool.PoolManager
}

// NewOlapService 创建OLAP服务实例
func NewOlapService(cfg config.Config, nodeID string, grpcPort string) (*OlapService, error) {
	// 创建连接池管理器配置
	poolConfig := &pool.PoolManagerConfig{
		Redis: &pool.RedisPoolConfig{
			Mode:         pool.RedisModeStandalone,
			Addr:         cfg.Redis.Addr,
			Password:     cfg.Redis.Password,
			DB:           cfg.Redis.DB,
			PoolSize:     cfg.Redis.PoolSize,
			MinIdleConns: cfg.Redis.MinIdleConns,
		},
		MinIO: &pool.MinIOPoolConfig{
			Endpoint:        cfg.Minio.Endpoint,
			AccessKeyID:     cfg.Minio.AccessKeyID,
			SecretAccessKey: cfg.Minio.SecretAccessKey,
			UseSSL:          cfg.Minio.UseSSL,
			Region:          cfg.Minio.Region,
		},
		HealthCheckInterval: cfg.Pool.HealthCheckInterval,
	}

	// 初始化连接池管理器
	poolManager, err := pool.NewPoolManager(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool manager: %w", err)
	}

	return &OlapService{
		cfg:         cfg,
		poolManager: poolManager,
	}, nil
}

// Write 写入数据
func (s *OlapService) Write(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	log.Printf("Received write request for ID: %s", req.Id)

	// 验证请求
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.Payload == nil {
		return nil, status.Error(codes.InvalidArgument, "payload is required")
	}

	// 处理写入请求
	return s.processLocalWrite(ctx, req)
}

// processLocalWrite 处理本地写入请求
func (s *OlapService) processLocalWrite(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	// 获取MinIO客户端
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return nil, status.Error(codes.Internal, "MinIO pool not available")
	}
	
	_ = minioPool.GetClient()
	
	// 这里需要实现实际的写入逻辑
	// 目前先返回成功响应
	log.Printf("Would write data for ID: %s using MinIO client", req.Id)

	return &olapv1.WriteResponse{
		Success: true,
		Message: fmt.Sprintf("Data write simulated for ID: %s", req.Id),
	}, nil
}

// Query 查询数据
func (s *OlapService) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	log.Printf("Received query request: %s", req.Sql)

	// 验证请求
	if req.Sql == "" {
		return nil, status.Error(codes.InvalidArgument, "sql is required")
	}

	// 执行查询
	result, err := s.executeLocalQuery(ctx, req.Sql)
	if err != nil {
		log.Printf("ERROR: query failed: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("query failed: %v", err))
	}

	log.Printf("Query completed successfully")
	return &olapv1.QueryResponse{
		ResultJson: result,
	}, nil
}

// executeLocalQuery 执行本地查询
func (s *OlapService) executeLocalQuery(ctx context.Context, sql string) (string, error) {
	// 获取Redis客户端
	redisPool := s.poolManager.GetRedisPool()
	if redisPool == nil {
		return "", fmt.Errorf("Redis pool not available")
	}
	
	// 获取MinIO客户端
	minioPool := s.poolManager.GetMinIOPool()
	if minioPool == nil {
		return "", fmt.Errorf("MinIO pool not available")
	}

	// 这里需要实现实际的查询逻辑
	// 目前先返回模拟结果
	log.Printf("Would execute query: %s", sql)
	
	return `[{"result": "simulated query result"}]`, nil
}

// GetStats 获取服务统计信息
func (s *OlapService) GetStats(ctx context.Context, req *olapv1.GetStatsRequest) (*olapv1.GetStatsResponse, error) {
	log.Printf("Received stats request")

	// 获取连接池统计信息
	stats := s.poolManager.GetOverallStats()
	
	// 创建响应
	response := &olapv1.GetStatsResponse{
		Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
		BufferStats: make(map[string]int64),
		RedisStats:  make(map[string]int64),
		MinioStats:  make(map[string]int64),
	}
	
	// 填充统计信息
	if redisPool := s.poolManager.GetRedisPool(); redisPool != nil {
		redisStats := redisPool.GetStats()
		response.RedisStats["total_conns"] = int64(redisStats.TotalConns)
		response.RedisStats["idle_conns"] = int64(redisStats.IdleConns)
	}
	
	if minioPool := s.poolManager.GetMinIOPool(); minioPool != nil {
		minioStats := minioPool.GetStats()
		response.MinioStats["total_requests"] = minioStats.TotalRequests
		response.MinioStats["success_requests"] = minioStats.SuccessRequests
		response.MinioStats["failed_requests"] = minioStats.FailedRequests
	}
	
	log.Printf("Stats: %v", stats)
	
	return response, nil
}

// HealthCheck 健康检查
func (s *OlapService) HealthCheck(ctx context.Context, req *olapv1.HealthCheckRequest) (*olapv1.HealthCheckResponse, error) {
	log.Printf("Received health check request")

	// 检查连接池健康状态
	isHealthy := s.poolManager.IsHealthy(ctx)
	
	status := "healthy"
	if !isHealthy {
		status = "unhealthy"
	}
	
	return &olapv1.HealthCheckResponse{
		Status:    status,
		Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
		Version:   "1.0.0",
		Details: map[string]string{
			"pool_manager": fmt.Sprintf("healthy: %t", isHealthy),
		},
	}, nil
}

// TriggerBackup 触发备份
func (s *OlapService) TriggerBackup(ctx context.Context, req *olapv1.TriggerBackupRequest) (*olapv1.TriggerBackupResponse, error) {
	log.Printf("Received backup trigger request for ID: %s, day: %s", req.Id, req.Day)

	// 模拟备份操作
	return &olapv1.TriggerBackupResponse{
		Success:       true,
		Message:       fmt.Sprintf("Backup triggered for ID: %s, day: %s", req.Id, req.Day),
		FilesBackedUp: 0, // 模拟值
	}, nil
}

// RecoverData 恢复数据
func (s *OlapService) RecoverData(ctx context.Context, req *olapv1.RecoverDataRequest) (*olapv1.RecoverDataResponse, error) {
	log.Printf("Received data recovery request")

	// 模拟恢复操作
	return &olapv1.RecoverDataResponse{
		Success:        true,
		Message:        "Data recovery simulated",
		FilesRecovered: 0,
		RecoveredKeys:  []string{},
	}, nil
}

// GetNodes 获取节点信息
func (s *OlapService) GetNodes(ctx context.Context, req *olapv1.GetNodesRequest) (*olapv1.GetNodesResponse, error) {
	log.Printf("Received nodes request")

	// 模拟节点信息
	nodes := []*olapv1.NodeInfo{
		{
			Id:       "node-1",
			Status:   "active",
			Type:     "primary",
			Address:  "localhost:9090",
			LastSeen: time.Now().Unix(),
		},
	}

	return &olapv1.GetNodesResponse{
		Nodes: nodes,
		Total: int32(len(nodes)),
	}, nil
}

// Close 关闭服务
func (s *OlapService) Close() error {
	log.Printf("Closing OLAP service")
	
	if s.poolManager != nil {
		return s.poolManager.Close()
	}
	
	return nil
} 