package service

import (
	"context"
	"fmt"
	"log"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/duckdb"
	"minIODB/internal/minio"
	"minIODB/internal/redis"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OlapService OLAP服务实现
type OlapService struct {
	olapv1.UnimplementedOlapServiceServer
	
	cfg               config.Config
	minioClient       *minio.Client
	duckDBClient      *duckdb.Client
	redisClient       *redis.Client
	serviceRegistry   *discovery.ServiceRegistry
	writeCoordinator  *coordinator.WriteCoordinator
	queryCoordinator  *coordinator.QueryCoordinator
}

// NewOlapService 创建OLAP服务实例
func NewOlapService(cfg config.Config, nodeID string, grpcPort string) (*OlapService, error) {
	// 初始化MinIO客户端
	minioClient, err := minio.NewClient(cfg.MinIO)
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	// 初始化DuckDB客户端
	duckDBClient, err := duckdb.NewClient(cfg.DuckDB)
	if err != nil {
		return nil, fmt.Errorf("failed to create DuckDB client: %w", err)
	}

	// 初始化Redis客户端
	redisClient := redis.NewRedisClient(cfg.Redis)

	// 初始化服务注册与发现
	serviceRegistry, err := discovery.NewServiceRegistry(cfg, nodeID, grpcPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create service registry: %w", err)
	}

	// 启动服务注册
	if err := serviceRegistry.Start(); err != nil {
		return nil, fmt.Errorf("failed to start service registry: %w", err)
	}

	// 初始化协调器
	writeCoordinator := coordinator.NewWriteCoordinator(serviceRegistry)
	queryCoordinator := coordinator.NewQueryCoordinator(redisClient.Client, serviceRegistry)

	return &OlapService{
		cfg:               cfg,
		minioClient:       minioClient,
		duckDBClient:      duckDBClient,
		redisClient:       redisClient,
		serviceRegistry:   serviceRegistry,
		writeCoordinator:  writeCoordinator,
		queryCoordinator:  queryCoordinator,
	}, nil
}

// Write 写入数据
func (s *OlapService) Write(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	log.Printf("Received write request for ID: %s", req.Id)

	// 验证请求
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.Data == "" {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	// 路由写入请求
	targetNode, err := s.writeCoordinator.RouteWrite(req)
	if err != nil {
		log.Printf("ERROR: failed to route write request: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to route write request: %v", err))
	}

	// 如果路由到远程节点，直接返回成功
	if targetNode != "local" {
		log.Printf("Write request routed to node: %s", targetNode)
		return &olapv1.WriteResponse{
			Success: true,
			Message: fmt.Sprintf("Data routed to node: %s", targetNode),
		}, nil
	}

	// 本地处理写入请求
	return s.processLocalWrite(ctx, req)
}

// processLocalWrite 处理本地写入请求
func (s *OlapService) processLocalWrite(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	// 写入到MinIO
	filename, err := s.minioClient.WriteData(ctx, req.Id, req.Data)
	if err != nil {
		log.Printf("ERROR: failed to write to MinIO: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to write to MinIO: %v", err))
	}

	// 更新索引到Redis
	if err := s.redisClient.UpdateIndex(ctx, req.Id, filename); err != nil {
		log.Printf("WARN: failed to update index: %v", err)
		// 索引更新失败不影响写入成功
	}

	log.Printf("Successfully wrote data for ID: %s to file: %s", req.Id, filename)

	return &olapv1.WriteResponse{
		Success: true,
		Message: fmt.Sprintf("Data written successfully to file: %s", filename),
	}, nil
}

// Query 查询数据
func (s *OlapService) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	log.Printf("Received query request: %s", req.Sql)

	// 验证请求
	if req.Sql == "" {
		return nil, status.Error(codes.InvalidArgument, "sql is required")
	}

	// 执行分布式查询
	result, err := s.queryCoordinator.ExecuteDistributedQuery(req.Sql)
	if err != nil {
		log.Printf("ERROR: distributed query failed: %v", err)
		
		// 如果分布式查询失败，尝试本地查询作为降级
		localResult, localErr := s.executeLocalQuery(ctx, req.Sql)
		if localErr != nil {
			log.Printf("ERROR: local query also failed: %v", localErr)
			return nil, status.Error(codes.Internal, fmt.Sprintf("query failed: %v", err))
		}
		
		log.Printf("Fallback to local query succeeded")
		return &olapv1.QueryResponse{
			ResultJson: localResult,
		}, nil
	}

	log.Printf("Distributed query completed successfully")
	return &olapv1.QueryResponse{
		ResultJson: result,
	}, nil
}

// executeLocalQuery 执行本地查询
func (s *OlapService) executeLocalQuery(ctx context.Context, sql string) (string, error) {
	// 从Redis获取相关文件
	files, err := s.redisClient.GetFilesForQuery(ctx, sql)
	if err != nil {
		log.Printf("WARN: failed to get files from index: %v", err)
		// 如果索引查询失败，使用默认的文件发现逻辑
		files, err = s.minioClient.ListFiles(ctx, "")
		if err != nil {
			return "", fmt.Errorf("failed to list files: %w", err)
		}
	}

	if len(files) == 0 {
		return "[]", nil // 返回空结果
	}

	// 使用DuckDB执行查询
	result, err := s.duckDBClient.ExecuteQuery(ctx, sql, files)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
	}

	return result, nil
}

// GetStats 获取服务统计信息
func (s *OlapService) GetStats(ctx context.Context, req *olapv1.StatsRequest) (*olapv1.StatsResponse, error) {
	log.Printf("Received stats request")

	// 获取查询统计
	queryStats := s.queryCoordinator.GetQueryStats()
	
	// 获取存储统计
	storageStats, err := s.minioClient.GetStats(ctx)
	if err != nil {
		log.Printf("WARN: failed to get storage stats: %v", err)
		storageStats = map[string]interface{}{
			"error": err.Error(),
		}
	}

	// 获取索引统计
	indexStats, err := s.redisClient.GetStats(ctx)
	if err != nil {
		log.Printf("WARN: failed to get index stats: %v", err)
		indexStats = map[string]interface{}{
			"error": err.Error(),
		}
	}

	// 组合统计信息
	stats := map[string]interface{}{
		"node_info":      s.serviceRegistry.GetNodeInfo(),
		"cluster_stats":  queryStats,
		"storage_stats":  storageStats,
		"index_stats":    indexStats,
	}

	// 转换为JSON字符串
	result, err := s.redisClient.Marshal(stats)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to marshal stats: %v", err))
	}

	return &olapv1.StatsResponse{
		StatsJson: result,
	}, nil
}

// Close 关闭服务
func (s *OlapService) Close() error {
	log.Printf("Shutting down OLAP service")

	var errors []error

	// 停止服务注册
	if s.serviceRegistry != nil {
		s.serviceRegistry.Stop()
	}

	// 关闭DuckDB连接
	if s.duckDBClient != nil {
		if err := s.duckDBClient.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close DuckDB: %w", err))
		}
	}

	// 关闭Redis连接
	if s.redisClient != nil {
		if err := s.redisClient.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close Redis: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}

	log.Printf("OLAP service shutdown completed")
	return nil
} 