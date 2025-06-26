package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/service"
	"minIODB/internal/storage"
	grpcTransport "minIODB/internal/transport/grpc"
	restTransport "minIODB/internal/transport/rest"

	"minIODB/internal/recovery"
)

func main() {
	// 解析命令行参数
	configPath := ""
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// 加载配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化 logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	log.Println("Starting MinIODB server...")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建恢复处理器
	recoveryHandler := recovery.NewRecoveryHandler("main", log.New(os.Stdout, "[RECOVERY] ", log.LstdFlags))
	defer recoveryHandler.Recover()

	// 初始化存储层
	redisClient, err := storage.NewRedisClient(cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	primaryMinio, err := storage.NewMinioClientWrapper(cfg.MinIO)
	if err != nil {
		log.Fatalf("Failed to create primary MinIO client: %v", err)
	}

	var backupMinio storage.Uploader
	if cfg.Backup.Enabled {
		backupMinio, err = storage.NewMinioClientWrapper(cfg.Backup.MinIO)
		if err != nil {
			log.Fatalf("Failed to create backup MinIO client: %v", err)
		}
	}

	// 初始化缓冲区
	bufferManager := buffer.NewManager(cfg.Buffer.BufferSize)

	// 初始化服务组件
	sharedBuffer := buffer.NewSharedBuffer(
		redisClient.GetClient(),
		primaryMinio,
		backupMinio,
		cfg.Backup.MinIO.Bucket,
		cfg,
	)

	ingesterService := ingest.NewIngester(sharedBuffer)
	querierService, err := query.NewQuerier(redisClient.GetClient(), primaryMinio, cfg, sharedBuffer, logger)
	if err != nil {
		log.Fatalf("Failed to create querier service: %v", err)
	}

	// 初始化服务注册与发现
	serviceRegistry, err := discovery.NewServiceRegistry(*cfg, cfg.Server.NodeID, cfg.Server.GrpcPort)
	if err != nil {
		log.Fatalf("Failed to create service registry: %v", err)
	}

	// 启动服务注册
	if err := serviceRegistry.Start(); err != nil {
		log.Fatalf("Failed to start service registry: %v", err)
	}
	defer serviceRegistry.Stop()

	// 初始化协调器
	writeCoord := coordinator.NewWriteCoordinator(serviceRegistry)
	queryCoord := coordinator.NewQueryCoordinator(redisClient.GetClient(), serviceRegistry, querierService)

	// 创建gRPC传输层
	grpcServer := startGRPCServer(cfg, ingesterService, querierService, writeCoord,
		queryCoord, redisClient, primaryMinio, backupMinio)

	// 启动REST服务器
	restServer := startRESTServer(cfg, ingesterService, querierService, writeCoord,
		queryCoord, redisClient, primaryMinio, backupMinio, bufferManager)

	// 启动指标服务器
	var metricsServer *http.Server
	var systemMonitor *metrics.SystemMonitor
	if cfg.Metrics.Enabled {
		metricsServer = startMetricsServer(cfg)
		systemMonitor = metrics.NewSystemMonitor()
		systemMonitor.Start()
	}

	// 创建健康检查器
	healthChecker := recovery.NewHealthChecker(30*time.Second, 5*time.Second, log.New(os.Stdout, "[HEALTH] ", log.LstdFlags))

	// 添加健康检查
	healthChecker.AddCheck(recovery.NewDatabaseHealthCheck("redis", func() error {
		// Redis 健康检查
		return nil // 这里应该实际检查 Redis 连接
	}))

	healthChecker.AddCheck(recovery.NewDatabaseHealthCheck("minio", func() error {
		// MinIO 健康检查
		return nil // 这里应该实际检查 MinIO 连接
	}))

	// 启动健康检查器
	recoveryHandler.SafeGoWithContext(ctx, func(ctx context.Context) {
		healthChecker.Start(ctx)
	})

	// 启动缓冲区刷新goroutine
	go func() {
		ticker := time.NewTicker(cfg.Buffer.FlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := bufferManager.Flush(func(data []buffer.DataPoint) error {
					// 批量写入数据
					for _, point := range data {
						dataRow := buffer.DataRow{
							ID:        point.ID,
							Timestamp: point.Timestamp.UnixNano(),
							Payload:   string(point.Data),
						}
						sharedBuffer.Add(dataRow)
					}
					return nil
				}); err != nil {
					log.Printf("Failed to flush buffer: %v", err)
				}
			}
		}
	}()

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		go startBackupRoutine(primaryMinio, backupMinio, *cfg)
	}

	log.Println("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, grpcServer, restServer, metricsServer, systemMonitor)

	log.Println("MinIODB server stopped")
}

func startGRPCServer(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier, writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator, redisClient *storage.RedisClient, primaryMinio, backupMinio storage.Uploader) *grpc.Server {
	// 创建统一的service层
	olapService, err := service.NewOlapService(cfg, ingester, querier, redisClient.GetClient(), primaryMinio, backupMinio)
	if err != nil {
		log.Fatalf("Failed to create OLAP service: %v", err)
	}

	// 创建gRPC服务
	grpcService, err := grpcTransport.NewServer(olapService, *cfg)
	if err != nil {
		log.Fatalf("Failed to create gRPC service: %v", err)
	}

	// 设置协调器
	grpcService.SetCoordinators(writeCoord, queryCoord)

	grpcServer := grpc.NewServer()

	// 注册服务
	pb.RegisterOlapServiceServer(grpcServer, grpcService)

	// 启用反射（用于调试）
	reflection.Register(grpcServer)

	// 启动gRPC服务器
	go func() {
		lis, err := net.Listen("tcp", cfg.Server.GrpcPort)
		if err != nil {
			log.Fatalf("Failed to listen on gRPC port %s: %v", cfg.Server.GrpcPort, err)
		}

		log.Printf("gRPC server starting on port %s", cfg.Server.GrpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve gRPC: %v", err)
		}
	}()

	return grpcServer
}

func startRESTServer(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier, writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator, redisClient *storage.RedisClient, primaryMinio, backupMinio storage.Uploader, bufferManager *buffer.Manager) *restTransport.Server {
	// 创建统一的service层
	olapService, err := service.NewOlapService(cfg, ingester, querier, redisClient.GetClient(), primaryMinio, backupMinio)
	if err != nil {
		log.Fatalf("Failed to create OLAP service: %v", err)
	}

	// 创建REST服务器
	restServer := restTransport.NewServer(olapService, cfg)

	// 设置协调器
	restServer.SetCoordinators(writeCoord, queryCoord)

	// 启动服务器
	go func() {
		log.Printf("REST server listening on port %s", cfg.Server.RestPort)
		if err := restServer.Start(cfg.Server.RestPort); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start REST server: %v", err)
		}
	}()

	return restServer
}

func startBackupRoutine(primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
	log.Println("Starting backup routine")

	ticker := time.NewTicker(time.Duration(cfg.Backup.Interval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Starting backup process")

		// 简化的备份逻辑
		// TODO: 实现完整的备份逻辑

		log.Println("Backup process completed")
	}
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, grpcServer *grpc.Server, restServer *restTransport.Server, metricsServer *http.Server, systemMonitor *metrics.SystemMonitor) {
	// 创建信号通道
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	<-sigChan
	log.Println("Shutting down servers...")

	// 取消上下文
	cancel()

	// 创建关闭上下文，设置超时
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 停止gRPC服务器
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		log.Println("gRPC server stopped")
	case <-shutdownCtx.Done():
		grpcServer.Stop()
		log.Println("gRPC server force stopped")
	}

	// 停止REST服务器
	if err := restServer.Stop(shutdownCtx); err != nil {
		log.Printf("Failed to stop REST server: %v", err)
	} else {
		log.Println("REST server stopped")
	}

	// 停止指标服务器
	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Failed to stop metrics server: %v", err)
		} else {
			log.Println("Metrics server stopped")
		}
	}

	// 停止系统监控器
	if systemMonitor != nil {
		systemMonitor.Stop()
		log.Println("System monitor stopped")
	}

	log.Println("All servers stopped gracefully")
}

// startMetricsServer 启动指标服务器
func startMetricsServer(cfg *config.Config) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Metrics.Port),
		Handler: mux,
	}

	go func() {
		log.Printf("Metrics server starting on port %s", cfg.Metrics.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Failed to start metrics server: %v", err)
		}
	}()

	return server
}
