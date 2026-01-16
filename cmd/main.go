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

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
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
	if redisPool != nil {
		log.Println("Redis connection pool initialized successfully")
	} else {
		log.Println("Redis disabled - running in single-node mode")
	}

	// 注意：高级存储引擎优化功能已实现但当前保持禁用以维持系统简单性
	// 相关文件保留在 internal/storage/ 目录下供未来使用
	// 包括: engine.go, index_system.go, memory.go, shard.go 等

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
	querierService, err := query.NewQuerier(redisPool, primaryMinio, cfg, concurrentBuffer, logger)
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

	queryCoord := coordinator.NewQueryCoordinator(
		redisPool, // 使用连接池
		serviceRegistry,
		querierService,
		cfg,
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

		metadataManager = metadata.NewManager(storageInstance, storageInstance, &metadataConfig)

		if err := metadataManager.Start(); err != nil {
			log.Printf("Failed to start metadata manager: %v", err)
		} else {
			log.Println("Metadata manager started successfully")
		}
	}

	// 初始化服务
	log.Println("Initializing services...")

	// 创建MinIODB服务
	miniodbService, err := service.NewMinIODBService(cfg, ingesterService, querierService, redisPool, metadataManager)
	if err != nil {
		log.Fatalf("Failed to create MinIODB service: %v", err)
	}

	// 创建健康检查器
	healthChecker := monitoring.NewHealthChecker(redisPool, primaryMinio.GetClient(), nil, cfg)

	// 启动健康检查
	go healthChecker.StartHealthCheck(ctx, 30*time.Second)

	// 创建gRPC传输层
	grpcServer := startGRPCServer(cfg, miniodbService, writeCoord,
		queryCoord, redisPool, primaryMinio, backupMinio, metadataManager)

	// 启动REST服务器
	restServer := startRESTServer(cfg, miniodbService, writeCoord,
		queryCoord, redisPool, primaryMinio, backupMinio, metadataManager)

	// 启动指标服务器
	var metricsServer *http.Server
	var systemMonitor *metrics.SystemMonitor
	if cfg.Metrics.Enabled {
		metricsServer = startMetricsServer(cfg)
		systemMonitor = metrics.NewSystemMonitor()
		systemMonitor.Start()
	}

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		go startBackupRoutine(primaryMinio, backupMinio, *cfg)
	}

	log.Println("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, grpcServer, restServer, metricsServer, systemMonitor, metadataManager, nil)

	log.Println("MinIODB server stopped")
}

func startGRPCServer(cfg *config.Config, miniodbService *service.MinIODBService,
	writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator,
	redisPool *pool.RedisPool, primaryMinio, backupMinio storage.Uploader,
	metadataManager *metadata.Manager) *grpc.Server {

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

func startRESTServer(cfg *config.Config, miniodbService *service.MinIODBService,
	writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator,
	redisPool *pool.RedisPool, primaryMinio, backupMinio storage.Uploader,
	metadataManager *metadata.Manager) *http.Server {

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
		backupInterval = 24 * time.Hour
	}

	ticker := time.NewTicker(backupInterval)
	defer ticker.Stop()

	log.Printf("Backup routine started, interval: %v", backupInterval)

	for range ticker.C {
		log.Println("Starting scheduled backup...")

		if err := performDataBackup(primaryMinio, backupMinio, cfg); err != nil {
			log.Printf("Backup failed: %v", err)
		} else {
			log.Println("Backup completed successfully")
		}
	}
}

// performDataBackup 执行数据备份
func performDataBackup(primaryMinio, backupMinio storage.Uploader, cfg config.Config) error {
	ctx := context.Background()
	maxRetries := 3
	retryDelay := 5 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			log.Printf("Retry attempt %d/%d for backup", attempt, maxRetries)
			time.Sleep(retryDelay)
			retryDelay *= 2
		}

		if err := executeBackupWithRetry(ctx, primaryMinio, backupMinio, cfg); err != nil {
			if attempt == maxRetries {
				return fmt.Errorf("backup failed after %d attempts: %w", maxRetries, err)
			}
			continue
		}

		return nil
	}

	return nil
}

// executeBackupWithRetry 执行备份并记录状态
func executeBackupWithRetry(ctx context.Context, primaryMinio, backupMinio storage.Uploader, cfg config.Config) error {
	backupStartTime := time.Now()

	bucket := cfg.MinIO.Bucket

	objectsCh := primaryMinio.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})

	var backupCount int64
	var totalSize int64
	var errors []string

	for object := range objectsCh {
		if object.Err != nil {
			errors = append(errors, fmt.Sprintf("List error: %v", object.Err))
			continue
		}

		src := object.Key

		if err := copyObject(ctx, primaryMinio, backupMinio, bucket, src); err != nil {
			errors = append(errors, fmt.Sprintf("Copy error for %s: %v", src, err))
			continue
		}

		backupCount++
		totalSize += object.Size
	}

	if len(errors) > 0 {
		log.Printf("Backup completed with %d errors", len(errors))
		for _, err := range errors {
			log.Printf("  - %s", err)
		}
	}

	duration := time.Since(backupStartTime)
	log.Printf("Backup summary: objects=%d, size=%d, duration=%v", backupCount, totalSize, duration)

	if backupCount == 0 {
		return fmt.Errorf("no objects were backed up")
	}

	return nil
}

// copyObject 复制单个对象
func copyObject(ctx context.Context, primaryMinio, backupMinio storage.Uploader, bucket, objectName string) error {
	srcOpts := minio.CopySrcOptions{
		Bucket: bucket,
		Object: objectName,
	}

	_, err := backupMinio.CopyObject(ctx, minio.CopyDestOptions{
		Bucket: bucket,
	}, srcOpts)
	return err
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc,
	grpcServer *grpc.Server, restServer *http.Server,
	metricsServer *http.Server, systemMonitor *metrics.SystemMonitor,
	metadataManager *metadata.Manager, _ interface{}) {

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

	// 注意：存储引擎优化功能已禁用，无需关闭处理

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
			w.Write([]byte(metric + "\n"))
		}

		log.Printf("Metrics endpoint accessed, returned %d metric lines", len(metrics))
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
