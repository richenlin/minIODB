package grpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	miniodb "minIODB/api/proto/miniodb/v1"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/logger"
	"minIODB/internal/metadata"
	"minIODB/internal/metrics"
	"minIODB/internal/security"
	"minIODB/internal/service"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server 统一的gRPC服务器实现
type Server struct {
	miniodb.UnimplementedMinIODBServiceServer
	miniodb.UnimplementedAuthServiceServer
	miniodbService   *service.MinIODBService
	writeCoordinator *coordinator.WriteCoordinator
	queryCoordinator *coordinator.QueryCoordinator
	metadataManager  *metadata.Manager
	cfg              config.Config

	// 认证相关
	authManager     *security.AuthManager
	grpcInterceptor *security.GRPCInterceptor
	grpcServer      *grpc.Server

	// 智能限流相关
	smartRateLimiter     *security.SmartRateLimiter
	grpcSmartRateLimiter *security.GRPCSmartRateLimiter
}

// NewServer 创建新的gRPC服务器
func NewServer(ctx context.Context, miniodbService *service.MinIODBService, cfg config.Config) (*Server, error) {
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

	// 创建智能限流器
	var smartRateLimiter *security.SmartRateLimiter
	var grpcSmartRateLimiter *security.GRPCSmartRateLimiter

	if cfg.Security.SmartRateLimit.Enabled {
		// 转换配置格式
		smartRateLimitConfig := security.SmartRateLimiterConfig{
			Enabled:         cfg.Security.SmartRateLimit.Enabled,
			DefaultTier:     cfg.Security.SmartRateLimit.DefaultTier,
			CleanupInterval: cfg.Security.SmartRateLimit.CleanupInterval,
		}

		// 转换限流等级
		for _, tier := range cfg.Security.SmartRateLimit.Tiers {
			smartRateLimitConfig.Tiers = append(smartRateLimitConfig.Tiers, security.RateLimitTier{
				Name:            tier.Name,
				RequestsPerSec:  tier.RequestsPerSec,
				BurstSize:       tier.BurstSize,
				Window:          tier.Window,
				BackoffDuration: tier.BackoffDuration,
			})
		}

		// 转换路径限制
		for _, pathLimit := range cfg.Security.SmartRateLimit.PathLimits {
			smartRateLimitConfig.PathLimits = append(smartRateLimitConfig.PathLimits, security.PathRateLimit{
				Pattern: pathLimit.Pattern,
				Tier:    pathLimit.Tier,
				Enabled: pathLimit.Enabled,
			})
		}

		smartRateLimiter = security.NewSmartRateLimiter(smartRateLimitConfig)
		grpcSmartRateLimiter = security.NewGRPCSmartRateLimiter(smartRateLimiter)
		logger.LogInfo(ctx, "gRPC smart rate limiter initialized with %d tiers and %d path rules", zap.Int("tiers", len(smartRateLimitConfig.Tiers)), zap.Int("path_rules", len(smartRateLimitConfig.PathLimits)))
	} else {
		// 使用默认配置但禁用
		defaultConfig := security.GetDefaultSmartRateLimiterConfig()
		defaultConfig.Enabled = false
		smartRateLimiter = security.NewSmartRateLimiter(defaultConfig)
		grpcSmartRateLimiter = security.NewGRPCSmartRateLimiter(smartRateLimiter)
		logger.LogInfo(ctx, "gRPC smart rate limiter disabled")
	}

	server := &Server{
		miniodbService:       miniodbService,
		cfg:                  cfg,
		authManager:          authManager,
		grpcInterceptor:      grpcInterceptor,
		smartRateLimiter:     smartRateLimiter,
		grpcSmartRateLimiter: grpcSmartRateLimiter,
	}

	// 构建拦截器链
	var unaryInterceptors []grpc.UnaryServerInterceptor
	var streamInterceptors []grpc.StreamServerInterceptor

	// 添加智能限流拦截器（优先）
	if cfg.Security.SmartRateLimit.Enabled && grpcSmartRateLimiter != nil {
		unaryInterceptors = append(unaryInterceptors, grpcSmartRateLimiter.UnaryServerInterceptor())
		streamInterceptors = append(streamInterceptors, grpcSmartRateLimiter.StreamServerInterceptor())
		logger.LogInfo(ctx, "Smart rate limiter middleware enabled for gRPC API")
	} else if cfg.Security.RateLimit.Enabled {
		// 传统限流拦截器（向后兼容）
		grpcInterceptor.EnableRateLimit(cfg.Security.RateLimit.RequestsPerMinute)
		unaryInterceptors = append(unaryInterceptors, grpcInterceptor.RateLimitInterceptor())
		streamInterceptors = append(streamInterceptors, grpcInterceptor.StreamRateLimitInterceptor())
		logger.LogInfo(ctx, "Traditional rate limiter middleware enabled for gRPC API (%d req/min)", zap.Int("requests_per_minute", cfg.Security.RateLimit.RequestsPerMinute))
	} else {
		logger.LogInfo(ctx, "Rate limiting disabled for gRPC API")
	}

	// 添加JWT认证拦截器（根据配置决定是否启用）
	if authManager.IsEnabled() {
		unaryInterceptors = append(unaryInterceptors, grpcInterceptor.UnaryServerInterceptor())
		streamInterceptors = append(streamInterceptors, grpcInterceptor.StreamServerInterceptor())
		logger.LogInfo(ctx, "JWT authentication enabled for gRPC API")
	} else {
		logger.LogInfo(ctx, "JWT authentication disabled for gRPC API")
	}

	// 准备gRPC服务器选项 - 集成网络配置优化
	var grpcOpts []grpc.ServerOption

	// 添加网络配置优化参数
	grpcNetworkConfig := getGRPCNetworkConfig(&cfg)

	// Keep-Alive配置 - 优化长连接
	kaParams := keepalive.ServerParameters{
		Time:    grpcNetworkConfig.KeepAliveTime,    // 30s
		Timeout: grpcNetworkConfig.KeepAliveTimeout, // 5s
	}
	kaPolicy := keepalive.EnforcementPolicy{
		MinTime:             10 * time.Second, // 最小Keep-Alive间隔
		PermitWithoutStream: true,             // 允许无流时发送Keep-Alive
	}
	grpcOpts = append(grpcOpts, grpc.KeepaliveParams(kaParams))
	grpcOpts = append(grpcOpts, grpc.KeepaliveEnforcementPolicy(kaPolicy))

	// 连接配置
	grpcOpts = append(grpcOpts, grpc.ConnectionTimeout(grpcNetworkConfig.ConnectionTimeout))
	grpcOpts = append(grpcOpts, grpc.MaxSendMsgSize(grpcNetworkConfig.MaxSendMsgSize))
	grpcOpts = append(grpcOpts, grpc.MaxRecvMsgSize(grpcNetworkConfig.MaxRecvMsgSize))

	// 添加拦截器
	if len(unaryInterceptors) > 1 {
		grpcOpts = append(grpcOpts, grpc.ChainUnaryInterceptor(unaryInterceptors...))
		grpcOpts = append(grpcOpts, grpc.ChainStreamInterceptor(streamInterceptors...))
	} else if len(unaryInterceptors) == 1 {
		grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(unaryInterceptors[0]))
		grpcOpts = append(grpcOpts, grpc.StreamInterceptor(streamInterceptors[0]))
	}

	// 创建gRPC服务器
	grpcServer := grpc.NewServer(grpcOpts...)

	// 注册服务
	miniodb.RegisterMinIODBServiceServer(grpcServer, server)
	miniodb.RegisterAuthServiceServer(grpcServer, server)
	server.grpcServer = grpcServer

	// 确定限流状态
	rateLimitStatus := "disabled"
	if cfg.Security.SmartRateLimit.Enabled {
		rateLimitStatus = fmt.Sprintf("smart limiter enabled (%d tiers)", len(cfg.Security.SmartRateLimit.Tiers))
	} else if cfg.Security.RateLimit.Enabled {
		rateLimitStatus = fmt.Sprintf("traditional limiter enabled (%d req/min)", cfg.Security.RateLimit.RequestsPerMinute)
	}

	logger.LogInfo(ctx, "gRPC server initialized with authentication mode: %s, rate limit: %s", zap.String("mode", cfg.Security.Mode), zap.String("rate_limit", rateLimitStatus))

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
func (s *Server) Start(ctx context.Context, port string) error {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", port, err)
	}

	logger.LogInfo(ctx, "gRPC server starting on port %s", zap.String("port", port))
	if err := s.grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC server: %w", err)
	}

	return nil
}

// Stop 停止gRPC服务器
func (s *Server) Stop(ctx context.Context) {
	if s.grpcServer != nil {
		logger.LogInfo(ctx, "Stopping gRPC server...")
		s.grpcServer.GracefulStop()
	}
}

// 数据操作相关方法实现

// WriteData 实现写入数据API
func (s *Server) WriteData(ctx context.Context, req *miniodb.WriteDataRequest) (*miniodb.WriteDataResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("WriteData")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			logger.LogInfo(ctx, "WriteData request from user: %s (ID: %s) for data ID: %s", zap.String("username", user.Username), zap.String("id", user.ID), zap.String("data_id", req.Data.Id))
		}
	} else {
		logger.LogInfo(ctx, "Received WriteData request for ID: %s", zap.String("data_id", req.Data.Id))
	}

	// 调用服务方法处理请求
	result, err := s.miniodbService.WriteData(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "WriteData failed")
		return nil, err
	}

	return result, nil
}

// QueryData 实现查询数据API
func (s *Server) QueryData(ctx context.Context, req *miniodb.QueryDataRequest) (*miniodb.QueryDataResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("QueryData")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.UserFromContext(ctx); ok {
			logger.LogInfo(ctx, "QueryData request from user: %s (ID: %s)", zap.String("username", user.Username), zap.String("id", user.ID))
		}
	} else {
		logger.LogInfo(ctx, "Received QueryData request: %s", zap.String("sql", req.Sql))
	}

	// 调用服务方法处理请求
	result, err := s.miniodbService.QueryData(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "QueryData failed")
		return nil, err
	}

	return result, nil
}

// UpdateData 实现更新数据API
func (s *Server) UpdateData(ctx context.Context, req *miniodb.UpdateDataRequest) (*miniodb.UpdateDataResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("UpdateData")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 调用服务方法处理请求
	result, err := s.miniodbService.UpdateData(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "UpdateData failed")
		return nil, err
	}

	return result, nil
}

// DeleteData 实现删除数据API
func (s *Server) DeleteData(ctx context.Context, req *miniodb.DeleteDataRequest) (*miniodb.DeleteDataResponse, error) {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("DeleteData")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	// 调用服务方法处理请求
	result, err := s.miniodbService.DeleteData(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "DeleteData failed")
		return nil, err
	}

	return result, nil
}

// 流式API实现

// StreamWrite 实现流式写入API
func (s *Server) StreamWrite(stream miniodb.MinIODBService_StreamWriteServer) error {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("StreamWrite")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	var totalRecords int64
	var errors []string
	var table string // 用于记录处理的表

	ctx := stream.Context()
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			// 处理完所有数据
			logger.LogInfo(ctx, "StreamWrite completed, processed %d records for table %s", zap.Int64("total_records", totalRecords), zap.String("table", table))
			return stream.SendAndClose(&miniodb.StreamWriteResponse{
				Success:      len(errors) == 0,
				RecordsCount: totalRecords,
				Errors:       errors,
			})
		}

		if err != nil {
			logger.LogError(ctx, err, "StreamWrite receive error")
			return err
		}

		// 记录表名（用于日志）
		if table == "" && req.Table != "" {
			table = req.Table
		}

		// 批量处理记录
		for _, record := range req.Records {
			writeReq := &miniodb.WriteDataRequest{
				Table: req.Table,
				Data:  record,
			}

			_, err := s.miniodbService.WriteData(ctx, writeReq)
			if err != nil {
				errMsg := fmt.Sprintf("Error writing record ID %s: %v", record.Id, err)
				errors = append(errors, errMsg)
				logger.LogError(ctx, err, "Failed to write record in stream", zap.String("record_id", record.Id))
			} else {
				totalRecords++
			}
		}

		// 实现背压控制，每处理1000条记录暂停一下
		if totalRecords%1000 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// StreamQuery 实现流式查询API
func (s *Server) StreamQuery(req *miniodb.StreamQueryRequest, stream miniodb.MinIODBService_StreamQueryServer) error {
	// 记录gRPC指标
	grpcMetrics := metrics.NewGRPCMetrics("StreamQuery")
	defer func() {
		grpcMetrics.Finish("success")
	}()

	ctx := stream.Context()
	logger.LogInfo(ctx, "Received StreamQuery request: %s", zap.String("sql", req.Sql))

	// 设置默认批次大小，如果未指定
	batchSize := int32(100)
	if req.BatchSize > 0 {
		batchSize = req.BatchSize
	}

	// 初始化游标
	cursor := req.Cursor

	// 循环查询，分批发送结果
	for {
		// 创建查询请求
		queryReq := &miniodb.QueryDataRequest{
			Sql:    req.Sql,
			Limit:  batchSize,
			Cursor: cursor,
		}

		// 执行查询
		queryResp, err := s.miniodbService.QueryData(ctx, queryReq)
		if err != nil {
			logger.LogError(ctx, err, "StreamQuery batch failed")
			return err
		}

		// 转换结果为记录列表（这里需要服务实现将结果JSON转换为DataRecord对象列表）
		records, err := s.miniodbService.ConvertResultToRecords(ctx, queryResp.ResultJson)
		if err != nil {
			logger.LogError(ctx, err, "Failed to convert query result to records")
			return err
		}

		// 发送批次数据
		err = stream.Send(&miniodb.StreamQueryResponse{
			Records: records,
			HasMore: queryResp.HasMore,
			Cursor:  queryResp.NextCursor,
		})
		if err != nil {
			logger.LogError(ctx, err, "Failed to send stream batch")
			return err
		}

		// 如果没有更多数据，退出循环
		if !queryResp.HasMore {
			break
		}

		// 更新游标，准备获取下一批数据
		cursor = queryResp.NextCursor

		// 添加轻微延迟，避免过度负载
		time.Sleep(5 * time.Millisecond)
	}

	logger.LogInfo(ctx, "StreamQuery completed successfully")
	return nil
}

// 表管理相关方法实现

// CreateTable 实现创建表API
func (s *Server) CreateTable(ctx context.Context, req *miniodb.CreateTableRequest) (*miniodb.CreateTableResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.CreateTable(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "CreateTable failed")
		return nil, err
	}

	return result, nil
}

// ListTables 实现列出表API
func (s *Server) ListTables(ctx context.Context, req *miniodb.ListTablesRequest) (*miniodb.ListTablesResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.ListTables(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "ListTables failed")
		return nil, err
	}

	return result, nil
}

// GetTable 实现获取表信息API
func (s *Server) GetTable(ctx context.Context, req *miniodb.GetTableRequest) (*miniodb.GetTableResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.GetTable(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "GetTable failed")
		return nil, err
	}

	return result, nil
}

// DeleteTable 实现删除表API
func (s *Server) DeleteTable(ctx context.Context, req *miniodb.DeleteTableRequest) (*miniodb.DeleteTableResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.DeleteTable(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "DeleteTable failed")
		return nil, err
	}

	return result, nil
}

// 元数据管理相关方法实现

// BackupMetadata 实现备份元数据API
func (s *Server) BackupMetadata(ctx context.Context, req *miniodb.BackupMetadataRequest) (*miniodb.BackupMetadataResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.BackupMetadata(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "BackupMetadata failed")
		return nil, err
	}

	return result, nil
}

// RestoreMetadata 实现恢复元数据API
func (s *Server) RestoreMetadata(ctx context.Context, req *miniodb.RestoreMetadataRequest) (*miniodb.RestoreMetadataResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.RestoreMetadata(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "RestoreMetadata failed")
		return nil, err
	}

	return result, nil
}

// ListBackups 实现列出备份API
func (s *Server) ListBackups(ctx context.Context, req *miniodb.ListBackupsRequest) (*miniodb.ListBackupsResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.ListBackups(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "ListBackups failed")
		return nil, err
	}

	return result, nil
}

// GetMetadataStatus 实现获取元数据状态API
func (s *Server) GetMetadataStatus(ctx context.Context, req *miniodb.GetMetadataStatusRequest) (*miniodb.GetMetadataStatusResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.GetMetadataStatus(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "GetMetadataStatus failed")
		return nil, err
	}

	return result, nil
}

// 健康检查相关方法实现

// HealthCheck 实现健康检查API
func (s *Server) HealthCheck(ctx context.Context, req *miniodb.HealthCheckRequest) (*miniodb.HealthCheckResponse, error) {
	// 调用服务方法处理请求
	err := s.miniodbService.HealthCheck(ctx)
	if err != nil {
		logger.LogError(ctx, err, "HealthCheck failed")
		return &miniodb.HealthCheckResponse{
			Status:    "unhealthy",
			Timestamp: timestamppb.Now(),
			Version:   "1.0.0",
			Details: map[string]string{
				"error": err.Error(),
			},
		}, nil
	}

	return &miniodb.HealthCheckResponse{
		Status:    "healthy",
		Timestamp: timestamppb.Now(),
		Version:   "1.0.0",
		Details: map[string]string{
			"message": "All systems operational",
		},
	}, nil
}

// GetStatus 实现获取状态API
func (s *Server) GetStatus(ctx context.Context, req *miniodb.GetStatusRequest) (*miniodb.GetStatusResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.GetStatus(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "GetStatus failed")
		return nil, err
	}

	return result, nil
}

// GetMetrics 实现获取性能指标API
func (s *Server) GetMetrics(ctx context.Context, req *miniodb.GetMetricsRequest) (*miniodb.GetMetricsResponse, error) {
	// 调用服务方法处理请求
	result, err := s.miniodbService.GetMetrics(ctx, req)
	if err != nil {
		logger.LogError(ctx, err, "GetMetrics failed")
		return nil, err
	}

	return result, nil
}

// 认证服务相关方法实现

// GetToken 实现获取JWT令牌API
func (s *Server) GetToken(ctx context.Context, req *miniodb.GetTokenRequest) (*miniodb.GetTokenResponse, error) {
	// 验证API密钥和密码 (简单实现，实际应该验证credentials)
	if req.ApiKey == "" || req.Secret == "" {
		return nil, fmt.Errorf("api key and secret are required")
	}

	// 生成JWT token
	accessToken, err := s.authManager.GenerateToken(req.ApiKey, req.ApiKey)
	if err != nil {
		logger.LogError(ctx, err, "Token generation failed")
		return nil, fmt.Errorf("invalid credentials: %w", err)
	}

	return &miniodb.GetTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: "refresh_" + accessToken, // 简单实现
		ExpiresIn:    3600,                     // 1小时
		TokenType:    "Bearer",
	}, nil
}

// RefreshToken 实现刷新JWT令牌API
func (s *Server) RefreshToken(ctx context.Context, req *miniodb.RefreshTokenRequest) (*miniodb.RefreshTokenResponse, error) {
	// 简单实现：验证refresh token并生成新token
	if req.RefreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	// 这里应该验证refresh token，简单实现直接生成新的
	accessToken, err := s.authManager.GenerateToken("refresh_user", "refresh_user")
	if err != nil {
		logger.LogError(ctx, err, "Token refresh failed")
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	return &miniodb.RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: "refresh_" + accessToken,
		ExpiresIn:    3600,
		TokenType:    "Bearer",
	}, nil
}

// RevokeToken 实现撤销JWT令牌API
func (s *Server) RevokeToken(ctx context.Context, req *miniodb.RevokeTokenRequest) (*miniodb.RevokeTokenResponse, error) {
	// 简单实现：记录token撤销（实际应该加入黑名单）
	if req.Token == "" {
		return nil, fmt.Errorf("token is required")
	}

	logger.LogInfo(ctx, "Token revoked: %s", zap.String("token", req.Token[:10]+"..."))

	return &miniodb.RevokeTokenResponse{
		Success: true,
		Message: "Token revoked successfully",
	}, nil
}

// getGRPCNetworkConfig 获取gRPC网络配置
func getGRPCNetworkConfig(cfg *config.Config) *config.GRPCNetworkConfig {
	// 如果配置中有自定义的网络设置，则使用它
	if cfg.Network.Server.GRPC.ConnectionTimeout > 0 {
		return &cfg.Network.Server.GRPC
	}

	// 否则返回默认配置
	return &config.GRPCNetworkConfig{
		ConnectionTimeout: 120 * time.Second,
		KeepAliveTime:     30 * time.Second,
		KeepAliveTimeout:  5 * time.Second,
		MaxSendMsgSize:    4 * 1024 * 1024, // 4MB
		MaxRecvMsgSize:    4 * 1024 * 1024, // 4MB
	}
}
