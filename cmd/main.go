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
	"minIODB/internal/logger"
	"minIODB/internal/metrics"
	"minIODB/internal/monitoring"
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
	ctx := context.Background()
	// 加载配置
	cfg, err := config.LoadConfig(ctx, configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if err != nil {
		logger.LogFatal(ctx, "Failed to create logger: %v", zap.Error(err))
	}
	defer logger.Sync()

	logger.LogInfo(ctx, "Starting MinIODB server...")

	// 创建上下文
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 创建恢复处理器
	recoveryHandler := recovery.NewRecoveryHandler("main", logger.GetLogger())
	defer recoveryHandler.Recover()

	// 初始化存储层
	storageInstance, err := storage.NewStorage(ctx, cfg)
	if err != nil {
		logger.LogFatal(ctx, "Failed to create storage instance: %v", zap.Error(err))
	}
	defer storageInstance.Close(ctx)

	// 获取连接池管理器
	poolManager := storageInstance.GetPoolManager()

	// 获取Redis连接池（支持高可用）
	redisPool := poolManager.GetRedisPool()
	if redisPool != nil {
		logger.LogInfo(ctx, "Redis connection pool initialized successfully")
	} else {
		logger.LogInfo(ctx, "Redis disabled - running in single-node mode")
	}

	// 初始化存储引擎优化器（如果配置启用）
	var storageEngine *storage.StorageEngine
	if cfg.StorageEngine.Enabled {
		storageEngine = storage.NewStorageEngine(ctx, cfg, redisPool)
		logger.LogInfo(ctx, "Storage engine optimizer initialized successfully")
	} else {
		logger.LogInfo(ctx, "Storage engine optimizer disabled - using standard storage operations")
	}

	primaryMinio, err := storage.NewMinioClientWrapper(cfg.MinIO)
	if err != nil {
		logger.LogFatal(ctx, "Failed to create primary MinIO client: %v", zap.Error(err))
	}

	var backupMinio storage.Uploader
	if cfg.Backup.Enabled {
		backupMinio, err = storage.NewMinioClientWrapper(cfg.Backup.MinIO)
		if err != nil {
			logger.LogFatal(ctx, "Failed to create backup MinIO client: %v", zap.Error(err))
		}
	}

	// 初始化并发缓冲区
	concurrentBuffer := buffer.NewConcurrentBuffer(
		ctx,
		poolManager,
		cfg,
		cfg.Backup.MinIO.Bucket,
		cfg.Server.NodeID,
		nil, // 使用默认配置
	)

	ingesterService := ingest.NewIngester(concurrentBuffer)

	// 先创建索引系统（需要在querier之前创建）
	var indexSystem *storage.IndexSystem
	if redisPool != nil {
		indexSystem = storage.NewIndexSystem(ctx, redisPool)
		logger.LogInfo(ctx, "Index system initialized successfully")
	} else {
		logger.LogInfo(ctx, "Index system disabled - Redis not available")
	}

	querierService, err := query.NewQuerier(ctx, redisPool, primaryMinio, cfg, concurrentBuffer, logger.GetLogger(), indexSystem)
	if err != nil {
		logger.LogFatal(ctx, "Failed to create querier service: %v", zap.Error(err))
	}

	// 初始化服务注册与发现
	serviceRegistry, err := discovery.NewServiceRegistry(ctx, *cfg, cfg.Server.NodeID, cfg.Server.GrpcPort)
	if err != nil {
		logger.LogFatal(ctx, "Failed to create service registry: %v", zap.Error(err))
	}

	// 启动服务注册
	if err := serviceRegistry.Start(ctx); err != nil {
		logger.LogFatal(ctx, "Failed to start service registry: %v", zap.Error(err))
	}
	defer serviceRegistry.Stop(ctx)

	// 初始化协调器
	writeCoord := coordinator.NewWriteCoordinator(serviceRegistry)

	queryCoord := coordinator.NewQueryCoordinator(
		ctx,
		redisPool, // 使用连接池
		serviceRegistry,
		querierService,
		cfg,
		indexSystem, // 新增：索引系统
	)

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

		metadataManager = metadata.NewManager(ctx, storageInstance, storageInstance, &metadataConfig)

		if err := metadataManager.Start(ctx); err != nil {
			logger.LogFatal(ctx, "Failed to start metadata manager: %v", zap.Error(err))
		} else {
			logger.LogInfo(ctx, "Metadata manager started successfully")
		}
	}

	// 初始化服务
	logger.LogInfo(ctx, "Initializing services...")

	// 创建MinIODB服务
	miniodbService, err := service.NewMinIODBService(cfg, ingesterService, querierService, redisPool, metadataManager, primaryMinio.GetClient())
	if err != nil {
		logger.LogFatal(ctx, "Failed to create MinIODB service: %v", zap.Error(err))
	}

	// 创建健康检查器
	healthChecker := monitoring.NewHealthChecker(redisPool, primaryMinio.GetClient(), nil, cfg)

	// 启动健康检查
	go healthChecker.StartHealthCheck(ctx, 30*time.Second)

	// 创建gRPC传输层
	grpcServer := startGRPCServer(ctx, cfg, miniodbService, writeCoord,
		queryCoord, redisPool, primaryMinio, backupMinio, metadataManager)

	// 启动REST服务器
	restServer := startRESTServer(ctx, cfg, miniodbService, writeCoord,
		queryCoord, redisPool, primaryMinio, backupMinio, metadataManager)

	// 启动指标服务器和告警管理器
	var metricsServer *http.Server
	var systemMonitor *metrics.SystemMonitor
	var alertManager *monitoring.AlertManager
	if cfg.Metrics.Enabled {
		metricsServer = startMetricsServer(ctx, cfg)
		systemMonitor = metrics.NewSystemMonitor()
		systemMonitor.Start()

		// 启动告警管理器
		alertManager = monitoring.NewAlertManager(ctx, 5*time.Minute) // 5分钟去重窗口
		alertManager.AddNotifier(&monitoring.LogNotifier{})
		alertManager.Start(ctx)
		logger.LogInfo(ctx, "Alert manager started")
	}

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		go startBackupRoutine(ctx, primaryMinio, backupMinio, *cfg)
	}

	logger.LogInfo(ctx, "MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, grpcServer, restServer, metricsServer, systemMonitor, alertManager, metadataManager, storageEngine)

	logger.LogInfo(ctx, "MinIODB server stopped")
}

func startGRPCServer(ctx context.Context, cfg *config.Config, miniodbService *service.MinIODBService,
	writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator,
	redisPool *pool.RedisPool, primaryMinio, backupMinio storage.Uploader,
	metadataManager *metadata.Manager) *grpc.Server {

	// 创建gRPC服务
	grpcService, err := grpcTransport.NewServer(ctx, miniodbService, *cfg)
	if err != nil {
		logger.LogFatal(ctx, "Failed to create gRPC service: %v", zap.Error(err))
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
		if err := grpcService.Start(ctx, cfg.Server.GrpcPort); err != nil {
			logger.LogFatal(ctx, "Failed to start gRPC server: %v", zap.Error(err))
		}
	}()

	return nil // gRPC服务器在内部管理
}

func startRESTServer(ctx context.Context, cfg *config.Config, miniodbService *service.MinIODBService,
	writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator,
	redisPool *pool.RedisPool, primaryMinio, backupMinio storage.Uploader,
	metadataManager *metadata.Manager) *http.Server {

	// 创建REST服务
	restServer := restTransport.NewServer(ctx, miniodbService, cfg)

	// 设置协调器
	restServer.SetCoordinators(writeCoord, queryCoord)

	// 设置元数据管理器
	if metadataManager != nil {
		restServer.SetMetadataManager(metadataManager)
	}

	// 启动REST服务器
	go func() {
		logger.LogInfo(ctx, "Starting REST server on port %s", zap.String("port", cfg.Server.RestPort))
		// 检查端口是否已经包含冒号
		port := cfg.Server.RestPort
		if port[0] != ':' {
			port = ":" + port
		}
		if err := restServer.Start(ctx, port); err != nil {
			logger.LogFatal(ctx, "Failed to start REST server: %v", zap.Error(err))
		}
	}()

	return nil
}

func startBackupRoutine(ctx context.Context, primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
	backupInterval := time.Duration(cfg.Backup.Interval) * time.Hour
	if backupInterval == 0 {
		backupInterval = 24 * time.Hour // 默认每24小时备份一次
	}

	ticker := time.NewTicker(backupInterval)
	defer ticker.Stop()

	for range ticker.C {
		logger.LogInfo(ctx, "Starting scheduled backup...")
		// 这里实现备份逻辑
	}
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc,
	grpcServer *grpc.Server, restServer *http.Server,
	metricsServer *http.Server, systemMonitor *metrics.SystemMonitor,
	alertManager *monitoring.AlertManager,
	metadataManager *metadata.Manager, storageEngine *storage.StorageEngine) {

	// 捕获中断信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.LogInfo(ctx, "Received shutdown signal")
	cancel() // 取消上下文

	// 停止告警管理器
	if alertManager != nil {
		alertManager.Stop(ctx)
		logger.LogInfo(ctx, "Alert manager stopped")
	}

	// 停止系统监视器
	if systemMonitor != nil {
		systemMonitor.Stop()
	}

	// 停止元数据管理器
	if metadataManager != nil {
		if err := metadataManager.Stop(ctx); err != nil {
			logger.LogError(ctx, err, "Error stopping metadata manager: %v")
		}
	}

	// 停止存储引擎优化器
	if storageEngine != nil {
		logger.LogInfo(ctx, "Stopping storage engine optimizer...")
		if err := storageEngine.Stop(ctx); err != nil {
			logger.LogError(ctx, err, "Error stopping storage engine: %v")
		}
	}

	// 停止gRPC服务器
	if grpcServer != nil {
		logger.LogInfo(ctx, "Stopping gRPC server...")
		grpcServer.GracefulStop()
	}

	// 停止REST服务器
	if restServer != nil {
		logger.LogInfo(ctx, "Stopping REST server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
		defer shutdownCancel()
		if err := restServer.Shutdown(shutdownCtx); err != nil {
			logger.LogError(ctx, err, "Error shutting down REST server: %v")
		}
	}

	// 停止指标服务器
	if metricsServer != nil {
		logger.LogInfo(ctx, "Stopping metrics server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
		defer shutdownCancel()
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			logger.LogError(ctx, err, "Error shutting down metrics server: %v")
		}
	}
}

func startMetricsServer(ctx context.Context, cfg *config.Config) *http.Server {
	// 创建metrics处理器
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)

		// 生成Prometheus格式的metrics
		var metrics []string

		// 基础信息
		metrics = append(metrics, "# HELP miniodb_info MinIODB service information")
		metrics = append(metrics, "# TYPE miniodb_info gauge")
		metrics = append(metrics, fmt.Sprintf(`miniodb_info{version="1.0.0",node_id="%s"} 1`, cfg.Server.NodeID))

		// 系统启动时间
		metrics = append(metrics, "# HELP miniodb_start_time_seconds MinIODB start time in unix timestamp")
		metrics = append(metrics, "# TYPE miniodb_start_time_seconds gauge")
		metrics = append(metrics, fmt.Sprintf("miniodb_start_time_seconds %d", time.Now().Unix()))

		// 服务端口信息
		metrics = append(metrics, "# HELP miniodb_port_info MinIODB port configuration")
		metrics = append(metrics, "# TYPE miniodb_port_info gauge")
		metrics = append(metrics, fmt.Sprintf(`miniodb_port_info{port="%s",type="grpc"} 1`, cfg.Server.GrpcPort))
		metrics = append(metrics, fmt.Sprintf(`miniodb_port_info{port="%s",type="rest"} 1`, cfg.Server.RestPort))
		metrics = append(metrics, fmt.Sprintf(`miniodb_port_info{port="%s",type="metrics"} 1`, cfg.Metrics.Port))

		// 配置信息metrics
		metrics = append(metrics, "# HELP miniodb_config_info MinIODB configuration information")
		metrics = append(metrics, "# TYPE miniodb_config_info gauge")

		// Buffer配置
		if cfg.Buffer.BufferSize > 0 {
			metrics = append(metrics, fmt.Sprintf("miniodb_config_buffer_size %d", cfg.Buffer.BufferSize))
			metrics = append(metrics, fmt.Sprintf("miniodb_config_flush_interval_seconds %d", int(cfg.Buffer.FlushInterval.Seconds())))
		}

		// Redis配置
		metrics = append(metrics, fmt.Sprintf(`miniodb_config_redis_mode{mode="%s"} 1`, cfg.Redis.Mode))
		if cfg.Redis.PoolSize > 0 {
			metrics = append(metrics, fmt.Sprintf("miniodb_config_redis_pool_size %d", cfg.Redis.PoolSize))
		}

		// MinIO配置
		if cfg.MinIO.Bucket != "" {
			metrics = append(metrics, fmt.Sprintf(`miniodb_config_minio_bucket{bucket="%s"} 1`, cfg.MinIO.Bucket))
		}

		// 备份配置
		if cfg.Backup.Enabled {
			metrics = append(metrics, "miniodb_config_backup_enabled 1")
			metrics = append(metrics, fmt.Sprintf("miniodb_config_backup_interval_seconds %d", cfg.Backup.Interval))
		} else {
			metrics = append(metrics, "miniodb_config_backup_enabled 0")
		}

		// 表管理配置
		if cfg.TableManagement.AutoCreateTables {
			metrics = append(metrics, "miniodb_config_auto_create_tables 1")
		} else {
			metrics = append(metrics, "miniodb_config_auto_create_tables 0")
		}
		metrics = append(metrics, fmt.Sprintf("miniodb_config_max_tables %d", cfg.TableManagement.MaxTables))
		metrics = append(metrics, fmt.Sprintf(`miniodb_config_default_table{table="%s"} 1`, cfg.TableManagement.DefaultTable))

		// 健康状态metrics
		metrics = append(metrics, "# HELP miniodb_health_status MinIODB service health status")
		metrics = append(metrics, "# TYPE miniodb_health_status gauge")
		metrics = append(metrics, "miniodb_health_status 1") // 1表示healthy，0表示unhealthy

		// 网络配置metrics
		if cfg.Network.Server.GRPC.MaxRecvMsgSize > 0 {
			metrics = append(metrics, "# HELP miniodb_grpc_config MinIODB gRPC configuration")
			metrics = append(metrics, "# TYPE miniodb_grpc_config gauge")
			metrics = append(metrics, fmt.Sprintf("miniodb_grpc_max_recv_msg_size_bytes %d", cfg.Network.Server.GRPC.MaxRecvMsgSize))
			metrics = append(metrics, fmt.Sprintf("miniodb_grpc_max_send_msg_size_bytes %d", cfg.Network.Server.GRPC.MaxSendMsgSize))
			metrics = append(metrics, fmt.Sprintf("miniodb_grpc_keepalive_time_seconds %d", int(cfg.Network.Server.GRPC.KeepAliveTime.Seconds())))
			metrics = append(metrics, fmt.Sprintf("miniodb_grpc_keepalive_timeout_seconds %d", int(cfg.Network.Server.GRPC.KeepAliveTimeout.Seconds())))
		}

		// REST配置metrics
		if cfg.Network.Server.REST.ReadTimeout > 0 {
			metrics = append(metrics, "# HELP miniodb_rest_config MinIODB REST configuration")
			metrics = append(metrics, "# TYPE miniodb_rest_config gauge")
			metrics = append(metrics, fmt.Sprintf("miniodb_rest_read_timeout_seconds %d", int(cfg.Network.Server.REST.ReadTimeout.Seconds())))
			metrics = append(metrics, fmt.Sprintf("miniodb_rest_write_timeout_seconds %d", int(cfg.Network.Server.REST.WriteTimeout.Seconds())))
			metrics = append(metrics, fmt.Sprintf("miniodb_rest_idle_timeout_seconds %d", int(cfg.Network.Server.REST.IdleTimeout.Seconds())))
		}

		// 安全配置metrics
		metrics = append(metrics, "# HELP miniodb_security_config MinIODB security configuration")
		metrics = append(metrics, "# TYPE miniodb_security_config gauge")
		if cfg.Security.EnableTLS {
			metrics = append(metrics, "miniodb_security_tls_enabled 1")
		} else {
			metrics = append(metrics, "miniodb_security_tls_enabled 0")
		}
		metrics = append(metrics, fmt.Sprintf(`miniodb_security_mode{mode="%s"} 1`, cfg.Security.Mode))

		// 写入所有metrics
		for _, metric := range metrics {
			_, _ = w.Write([]byte(metric + "\n"))
		}

		logger.LogInfo(ctx, fmt.Sprintf("Metrics endpoint accessed, returned %d metric lines", len(metrics)))
	})

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Metrics.Port),
		Handler: mux,
	}

	go func() {
		logger.LogInfo(ctx, fmt.Sprintf("Starting metrics server on port %s", cfg.Metrics.Port))
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.LogFatal(ctx, "Failed to start metrics server: %v", zap.Error(err))
		}
	}()

	return metricsServer
}
