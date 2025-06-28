package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/metadata"
	"minIODB/internal/metrics"
	"minIODB/internal/security"
	"minIODB/internal/service"

	"google.golang.org/grpc"
)

// Server is the implementation of the OlapServiceServer
type Server struct {
	olapv1.UnimplementedOlapServiceServer
	olapService      *service.OlapService
	writeCoordinator *coordinator.WriteCoordinator
	queryCoordinator *coordinator.QueryCoordinator
	metadataManager  *metadata.Manager
	cfg              config.Config

	// 认证相关
	authManager     *security.AuthManager
	grpcInterceptor *security.GRPCInterceptor
	grpcServer      *grpc.Server
}

// NewServer creates a new gRPC server
func NewServer(olapService *service.OlapService, cfg config.Config) (*Server, error) {
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
	
	// 配置限流
	if cfg.Security.RateLimit.Enabled {
		grpcInterceptor.EnableRateLimit(cfg.Security.RateLimit.RequestsPerMinute)
	}

	server := &Server{
		olapService:     olapService,
		cfg:             cfg,
		authManager:     authManager,
		grpcInterceptor: grpcInterceptor,
	}

	// 构建拦截器链
	var unaryInterceptors []grpc.UnaryServerInterceptor
	var streamInterceptors []grpc.StreamServerInterceptor
	
	// 添加限流拦截器（如果启用）
	if cfg.Security.RateLimit.Enabled {
		unaryInterceptors = append(unaryInterceptors, grpcInterceptor.RateLimitInterceptor())
		streamInterceptors = append(streamInterceptors, grpcInterceptor.StreamRateLimitInterceptor())
	}
	
	// 添加认证拦截器
	unaryInterceptors = append(unaryInterceptors, grpcInterceptor.UnaryServerInterceptor())
	streamInterceptors = append(streamInterceptors, grpcInterceptor.StreamServerInterceptor())
	
	// 创建gRPC服务器，集成拦截器链
	var grpcServer *grpc.Server
	if len(unaryInterceptors) > 1 {
		grpcServer = grpc.NewServer(
			grpc.ChainUnaryInterceptor(unaryInterceptors...),
			grpc.ChainStreamInterceptor(streamInterceptors...),
		)
	} else {
		grpcServer = grpc.NewServer(
			grpc.UnaryInterceptor(unaryInterceptors[0]),
			grpc.StreamInterceptor(streamInterceptors[0]),
		)
	}

	// 注册服务
	olapv1.RegisterOlapServiceServer(grpcServer, server)
	server.grpcServer = grpcServer

	rateLimitStatus := "disabled"
	if cfg.Security.RateLimit.Enabled {
		rateLimitStatus = fmt.Sprintf("enabled (%d req/min)", cfg.Security.RateLimit.RequestsPerMinute)
	}
	log.Printf("gRPC server initialized with authentication mode: %s, rate limit: %s", cfg.Security.Mode, rateLimitStatus)

	return server, nil
}

// SetCoordinators 设置协调器
func (s *Server) SetCoordinators(writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator) {
	s.writeCoordinator = writeCoord
	s.queryCoordinator = queryCoord
}

// SetMetadataManager 设置元数据管理器
func (s *Server) SetMetadataManager(manager *metadata.Manager) {
	s.metadataManager = manager
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
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("Write request from user: %s (ID: %s) for data ID: %s", user.Username, user.ID, req.Id)
		}
	} else {
		log.Printf("Received Write request for ID: %s", req.Id)
	}

	// 使用写入协调器进行分布式路由
	if s.writeCoordinator != nil {
		targetNode, err := s.writeCoordinator.RouteWrite(req)
		if err != nil {
			return &olapv1.WriteResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to route write: %v", err),
			}, nil
		}

		// 如果路由到本地节点，则直接处理
		if targetNode == "local" {
			return s.olapService.Write(ctx, req)
		}

		// 如果路由到远程节点，返回成功（实际写入已在 RouteWrite 中完成）
		return &olapv1.WriteResponse{
			Success: true,
			Message: fmt.Sprintf("Write routed to node: %s", targetNode),
		}, nil
	}

	// 如果没有协调器，回退到本地处理
	return s.olapService.Write(ctx, req)
}

// Query implements the Query rpc
func (s *Server) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("Query")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("Query request from user: %s (ID: %s) with SQL: %s", user.Username, user.ID, req.Sql)
		}
	} else {
		log.Printf("Received Query request with SQL: %s", req.Sql)
	}

	// 使用查询协调器进行分布式查询
	if s.queryCoordinator != nil {
		result, err := s.queryCoordinator.ExecuteDistributedQuery(req.Sql)
		if err != nil {
			return &olapv1.QueryResponse{
				ResultJson: fmt.Sprintf(`{"error": "Distributed query failed: %v"}`, err),
			}, nil
		}

		return &olapv1.QueryResponse{
			ResultJson: result,
		}, nil
	}

	// 如果没有协调器，回退到本地处理
	return s.olapService.Query(ctx, req)
}

// TriggerBackup implements the manual backup RPC
func (s *Server) TriggerBackup(ctx context.Context, req *olapv1.TriggerBackupRequest) (*olapv1.TriggerBackupResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("TriggerBackup")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("TriggerBackup request from user: %s (ID: %s) for ID %s, Day %s", user.Username, user.ID, req.Id, req.Day)
		}
	} else {
		log.Printf("Received TriggerBackup request for ID %s, Day %s", req.Id, req.Day)
	}

	// 委托给service层处理
	return s.olapService.TriggerBackup(ctx, req)
}

// RecoverData implements data recovery RPC
func (s *Server) RecoverData(ctx context.Context, req *olapv1.RecoverDataRequest) (*olapv1.RecoverDataResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("RecoverData")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("RecoverData request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received RecoverData request")
	}

	// 委托给service层处理
	return s.olapService.RecoverData(ctx, req)
}

// HealthCheck implements health check RPC
func (s *Server) HealthCheck(ctx context.Context, req *olapv1.HealthCheckRequest) (*olapv1.HealthCheckResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("HealthCheck")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 委托给service层处理
	return s.olapService.HealthCheck(ctx, req)
}

// GetStats implements stats retrieval RPC
func (s *Server) GetStats(ctx context.Context, req *olapv1.GetStatsRequest) (*olapv1.GetStatsResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("GetStats")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 委托给service层处理
	return s.olapService.GetStats(ctx, req)
}

// GetNodes implements node information retrieval RPC
func (s *Server) GetNodes(ctx context.Context, req *olapv1.GetNodesRequest) (*olapv1.GetNodesResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("GetNodes")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 委托给service层处理
	return s.olapService.GetNodes(ctx, req)
}

// 元数据管理RPC方法实现

// TriggerMetadataBackup 触发元数据备份
func (s *Server) TriggerMetadataBackup(ctx context.Context, req *olapv1.TriggerMetadataBackupRequest) (*olapv1.TriggerMetadataBackupResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("TriggerMetadataBackup")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("TriggerMetadataBackup request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received TriggerMetadataBackup request")
	}

	if s.metadataManager == nil {
		return &olapv1.TriggerMetadataBackupResponse{
			Success: false,
			Message: "Metadata manager not available",
		}, nil
	}

	if err := s.metadataManager.TriggerBackup(ctx); err != nil {
		return &olapv1.TriggerMetadataBackupResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to trigger metadata backup: %v", err),
		}, nil
	}

	return &olapv1.TriggerMetadataBackupResponse{
		Success:  true,
		Message:  "Metadata backup triggered successfully",
		BackupId: fmt.Sprintf("backup_%d", time.Now().Unix()),
	}, nil
}

// ListMetadataBackups 列出元数据备份
func (s *Server) ListMetadataBackups(ctx context.Context, req *olapv1.ListMetadataBackupsRequest) (*olapv1.ListMetadataBackupsResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("ListMetadataBackups")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("ListMetadataBackups request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received ListMetadataBackups request")
	}

	if s.metadataManager == nil {
		return &olapv1.ListMetadataBackupsResponse{
			Backups: []*olapv1.MetadataBackupInfo{},
			Total:   0,
		}, nil
	}

	days := req.Days
	if days <= 0 {
		days = 30 // 默认30天
	}

	backups, err := s.metadataManager.ListBackups(ctx, int(days))
	if err != nil {
		return &olapv1.ListMetadataBackupsResponse{
			Backups: []*olapv1.MetadataBackupInfo{},
			Total:   0,
		}, nil
	}

	var backupInfos []*olapv1.MetadataBackupInfo
	for _, backup := range backups {
		backupInfos = append(backupInfos, &olapv1.MetadataBackupInfo{
			Id:          backup.ObjectName,
			Timestamp:   backup.Timestamp.Format(time.RFC3339),
			Size:        backup.Size,
			Status:      "completed",
			Description: fmt.Sprintf("Backup from node %s", backup.NodeID),
		})
	}

	return &olapv1.ListMetadataBackupsResponse{
		Backups: backupInfos,
		Total:   int32(len(backupInfos)),
	}, nil
}

// RecoverMetadata 恢复元数据
func (s *Server) RecoverMetadata(ctx context.Context, req *olapv1.RecoverMetadataRequest) (*olapv1.RecoverMetadataResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("RecoverMetadata")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("RecoverMetadata request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received RecoverMetadata request")
	}

	if s.metadataManager == nil {
		return &olapv1.RecoverMetadataResponse{
			Success: false,
			Message: "Metadata manager not available",
		}, nil
	}

	options := metadata.RecoveryOptions{
		Overwrite: req.ForceOverwrite,
		DryRun:    req.Mode == "dry_run",
	}

	// 设置恢复模式
	switch req.Mode {
	case "dry_run":
		options.Mode = metadata.RecoveryModeDryRun
	case "complete":
		options.Mode = metadata.RecoveryModeComplete
	default:
		options.Mode = metadata.RecoveryModeComplete
	}

	var result *metadata.RecoveryResult
	var err error

	if req.BackupFile == "" {
		// 从最新备份恢复
		result, err = s.metadataManager.RecoverFromLatest(ctx, options)
	} else {
		// 从指定备份恢复
		result, err = s.metadataManager.RecoverFromBackup(ctx, req.BackupFile, options)
	}

	if err != nil {
		return &olapv1.RecoverMetadataResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to recover metadata: %v", err),
		}, nil
	}

	return &olapv1.RecoverMetadataResponse{
		Success:        result.Success,
		Message:        "Metadata recovery completed",
		RecoveredItems: int32(result.EntriesOK),
		RecoveredKeys:  []string{}, // 实际的键列表需要从result.Details中提取
		Warnings:       result.Errors,
	}, nil
}

// GetMetadataStatus 获取元数据状态
func (s *Server) GetMetadataStatus(ctx context.Context, req *olapv1.GetMetadataStatusRequest) (*olapv1.MetadataStatusResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("GetMetadataStatus")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("GetMetadataStatus request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received GetMetadataStatus request")
	}

	if s.metadataManager == nil {
		return &olapv1.MetadataStatusResponse{
			Status:       "unavailable",
			LastBackup:   "",
			NextBackup:   "",
			TotalEntries: 0,
			TypeCounts:   make(map[string]int32),
		}, nil
	}

	// 获取状态信息 - 由于Manager没有GetStatus方法，我们返回基本信息
	return &olapv1.MetadataStatusResponse{
		Status:       "healthy",
		LastBackup:   "",
		NextBackup:   "",
		TotalEntries: 0,
		TypeCounts:   make(map[string]int32),
	}, nil
}

// ValidateMetadataBackup 验证元数据备份
func (s *Server) ValidateMetadataBackup(ctx context.Context, req *olapv1.ValidateMetadataBackupRequest) (*olapv1.ValidateMetadataBackupResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("ValidateMetadataBackup")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			log.Printf("ValidateMetadataBackup request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received ValidateMetadataBackup request")
	}

	if s.metadataManager == nil {
		return &olapv1.ValidateMetadataBackupResponse{
			Valid:   false,
			Message: "Metadata manager not available",
			Errors:  []string{"Metadata manager is not initialized"},
		}, nil
	}

	if err := s.metadataManager.ValidateBackup(ctx, req.BackupFile); err != nil {
		return &olapv1.ValidateMetadataBackupResponse{
			Valid:   false,
			Message: fmt.Sprintf("Backup validation failed: %v", err),
			Errors:  []string{err.Error()},
		}, nil
	}

	return &olapv1.ValidateMetadataBackupResponse{
		Valid:   true,
		Message: "Backup validation successful",
		Errors:  []string{},
	}, nil
}

// convertTypeCounts 转换类型计数格式
func convertTypeCounts(counts map[string]int) map[string]int32 {
	result := make(map[string]int32)
	for k, v := range counts {
		result[k] = int32(v)
	}
	return result
}
