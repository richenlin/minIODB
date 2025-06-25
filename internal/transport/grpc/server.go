package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/metrics"
	"minIODB/internal/security"
	"minIODB/internal/service"

	"google.golang.org/grpc"
)

// Server is the implementation of the OlapServiceServer
type Server struct {
	olapv1.UnimplementedOlapServiceServer
	olapService     *service.OlapService
	cfg             config.Config
	
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

	server := &Server{
		olapService:     olapService,
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

	// 委托给service层处理
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

	// 委托给service层处理
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
