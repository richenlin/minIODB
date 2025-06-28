package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

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

	"minIODB/internal/metadata"
	"minIODB/internal/pool"
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
	storageInstance, err := storage.NewStorage(cfg)
	if err != nil {
		log.Fatalf("Failed to create storage instance: %v", err)
	}
	defer storageInstance.Close()

	// 获取连接池管理器
	poolManager := storageInstance.GetPoolManager()

	// 获取Redis连接池（支持高可用）
	redisPool := poolManager.GetRedisPool()
	if redisPool == nil {
		log.Fatalf("Failed to get Redis pool from storage")
	}

	// 初始化存储引擎优化器（第四阶段集成）
	var storageEngine *storage.StorageEngine
	if cfg.StorageEngine.Enabled {
		log.Println("Initializing storage engine optimizer...")
		storageEngine = storage.NewStorageEngine(cfg, redisPool.GetRedisClient())

		// 启动自动优化
		if cfg.StorageEngine.AutoOptimization {
			if err := storageEngine.StartAutoOptimization(); err != nil {
				log.Printf("Failed to start auto optimization: %v", err)
			} else {
				log.Println("Storage engine auto optimization started")
			}
		}

		// 启动性能监控
		if cfg.StorageEngine.EnableMonitoring {
			log.Println("Storage engine performance monitoring enabled")
		}
	}

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

	// 初始化并发缓冲区
	concurrentBuffer := buffer.NewConcurrentBuffer(
		poolManager,
		cfg,
		cfg.Backup.MinIO.Bucket,
		cfg.Server.NodeID,
		nil, // 使用默认配置
	)

	ingesterService := ingest.NewIngester(concurrentBuffer)
	querierService, err := query.NewQuerier(redisPool.GetRedisClient(), primaryMinio, cfg, concurrentBuffer, logger)
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
	queryCoord := coordinator.NewQueryCoordinator(redisPool.GetRedisClient(), serviceRegistry, querierService)

	// 启动元数据备份管理器
	var metadataManager *metadata.Manager
	if cfg.Backup.Metadata.Enabled {
		metadataConfig := metadata.Config{
			NodeID: cfg.Server.NodeID,
			Backup: metadata.BackupConfig{
				Interval:      cfg.Backup.Metadata.Interval,
				RetentionDays: cfg.Backup.Metadata.RetentionDays,
				Bucket:        cfg.Backup.Metadata.Bucket,
			},
			Recovery: metadata.RecoveryConfig{
				Bucket: cfg.Backup.Metadata.Bucket,
			},
		}

		metadataManager = metadata.NewManager(storageInstance, storageInstance, &metadataConfig)

		if err := metadataManager.Start(); err != nil {
			log.Printf("Failed to start metadata manager: %v", err)
		} else {
			log.Println("Metadata manager started successfully")
		}
	}

	// 创建gRPC传输层
	grpcServer := startGRPCServer(cfg, ingesterService, querierService, writeCoord,
		queryCoord, redisPool, primaryMinio, backupMinio, metadataManager)

	// 启动REST服务器
	restServer := startRESTServer(cfg, ingesterService, querierService, writeCoord,
		queryCoord, redisPool, primaryMinio, backupMinio, metadataManager)

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
		// Redis 健康检查 - 使用连接池的健康检查
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return redisPool.HealthCheck(ctx)
	}))

	healthChecker.AddCheck(recovery.NewDatabaseHealthCheck("minio", func() error {
		// MinIO 健康检查
		return nil // 这里应该实际检查 MinIO 连接
	}))

	// 启动健康检查器
	recoveryHandler.SafeGoWithContext(ctx, func(ctx context.Context) {
		healthChecker.Start(ctx)
	})

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		go startBackupRoutine(primaryMinio, backupMinio, *cfg)
	}

	log.Println("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, grpcServer, restServer, metricsServer, systemMonitor, metadataManager, storageEngine)

	log.Println("MinIODB server stopped")
}

func startGRPCServer(cfg *config.Config, ingester *ingest.Ingester,
	querier *query.Querier, writeCoord *coordinator.WriteCoordinator,
	queryCoord *coordinator.QueryCoordinator, redisPool *pool.RedisPool,
	primaryMinio, backupMinio storage.Uploader, metadataManager *metadata.Manager) *grpc.Server {

	// 创建统一MinIODB服务
	miniodbService, err := service.NewMinIODBService(cfg, ingester, querier,
		redisPool.GetRedisClient(), metadataManager)
	if err != nil {
		log.Fatalf("Failed to create MinIODB service: %v", err)
	}

	// 创建gRPC服务
	grpcService, err := grpcTransport.NewServer(miniodbService, *cfg)
	if err != nil {
		log.Fatalf("Failed to create gRPC service: %v", err)
	}

	// 设置协调器
	grpcService.SetCoordinators(writeCoord, queryCoord)

	// 设置元数据管理器
	if metadataManager != nil {
		grpcService.SetMetadataManager(metadataManager)
	}

	// 启动gRPC服务器
	go func() {
		log.Printf("Starting gRPC server on port %s", cfg.Server.GrpcPort)
		if err := grpcService.Start(cfg.Server.GrpcPort); err != nil {
			log.Fatalf("Failed to start gRPC server: %v", err)
		}
	}()

	return nil // gRPC服务器在内部管理
}

func startRESTServer(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier,
	writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator,
	redisPool *pool.RedisPool, primaryMinio, backupMinio storage.Uploader, metadataManager *metadata.Manager) *http.Server {

	// 创建统一MinIODB服务
	miniodbService, err := service.NewMinIODBService(cfg, ingester, querier,
		redisPool.GetRedisClient(), metadataManager)
	if err != nil {
		log.Fatalf("Failed to create MinIODB service for REST: %v", err)
	}

	// 创建REST服务
	restServer := restTransport.NewServer(miniodbService, cfg)

	// 设置协调器
	restServer.SetCoordinators(writeCoord, queryCoord)

	// 设置元数据管理器
	if metadataManager != nil {
		restServer.SetMetadataManager(metadataManager)
	}

	// 启动REST服务器
	go func() {
		log.Printf("Starting REST server on port %s", cfg.Server.RestPort)
		// 检查端口是否已经包含冒号
		port := cfg.Server.RestPort
		if port[0] != ':' {
			port = ":" + port
		}
		if err := restServer.Start(port); err != nil {
			log.Fatalf("Failed to start REST server: %v", err)
		}
	}()

	return nil
}

func startBackupRoutine(primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
	backupInterval := time.Duration(cfg.Backup.Interval) * time.Hour
	if backupInterval == 0 {
		backupInterval = 24 * time.Hour // 默认每24小时备份一次
	}

	ticker := time.NewTicker(backupInterval)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Starting scheduled backup...")
		// 这里实现备份逻辑
	}
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc,
	grpcServer *grpc.Server, restServer *http.Server,
	metricsServer *http.Server, systemMonitor *metrics.SystemMonitor,
	metadataManager *metadata.Manager, storageEngine *storage.StorageEngine) {

	// 捕获中断信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Received shutdown signal")
	cancel() // 取消上下文

	// 停止系统监视器
	if systemMonitor != nil {
		systemMonitor.Stop()
	}

	// 停止元数据管理器
	if metadataManager != nil {
		if err := metadataManager.Stop(); err != nil {
			log.Printf("Error stopping metadata manager: %v", err)
		}
	}

	// 关闭存储引擎
	if storageEngine != nil {
		log.Println("Closing storage engine...")
		// TODO: 实现存储引擎关闭逻辑
	}

	// 停止gRPC服务器
	if grpcServer != nil {
		log.Println("Stopping gRPC server...")
		grpcServer.GracefulStop()
	}

	// 停止REST服务器
	if restServer != nil {
		log.Println("Stopping REST server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := restServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down REST server: %v", err)
		}
	}

	// 停止指标服务器
	if metricsServer != nil {
		log.Println("Stopping metrics server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down metrics server: %v", err)
		}
	}
}

func startMetricsServer(cfg *config.Config) *http.Server {
	// 创建简单的metrics处理器
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# MinIODB Metrics\n# TODO: Implement metrics collection\n"))
	})

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Metrics.Port),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting metrics server on port %s", cfg.Metrics.Port)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start metrics server: %v", err)
		}
	}()

	return metricsServer
}
