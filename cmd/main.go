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

	"minIODB/config"
	"minIODB/internal/buffer"
	"minIODB/internal/compaction"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
	"minIODB/internal/metadata"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/recovery"
	"minIODB/internal/service"
	"minIODB/internal/storage"
	grpcTransport "minIODB/internal/transport/grpc"
	restTransport "minIODB/internal/transport/rest"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"
)

func main() {
	// 解析命令行参数
	configPath := ""
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Logger.Error("Failed to load config", zap.Error(err))
		os.Exit(1)
	}

	// 初始化日志
	logCfg := logger.LogConfig{
		Level:      cfg.Log.Level,
		Format:     cfg.Log.Format,
		Output:     cfg.Log.Output,
		Filename:   cfg.Log.Filename,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
		Compress:   cfg.Log.Compress,
	}
	logger.InitLogger(logCfg)
	defer logger.Close()

	logger.Logger.Info("Starting MinIODB server...", zap.String("config_path", configPath))

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建恢复处理器
	stdLogger := log.New(os.Stdout, "[RECOVERY] ", log.LstdFlags)
	recoveryHandler := recovery.NewRecoveryHandler("main", stdLogger)
	defer recoveryHandler.Recover()

	// 初始化存储层
	storageInstance, err := storage.NewStorage(cfg)
	if err != nil {
		logger.Logger.Error("Failed to create storage instance", zap.Error(err))
		os.Exit(1)
	}
	defer storageInstance.Close()

	// 获取连接池管理器
	poolManager := storageInstance.GetPoolManager()

	// 获取Redis连接池（支持高可用）
	redisPool := poolManager.GetRedisPool()
	if redisPool != nil {
		logger.Logger.Info("Redis connection pool initialized successfully")
	} else {
		logger.Logger.Warn("Redis disabled - running in single-node mode")
	}

	// 注意：高级存储引擎优化功能已实现但当前保持禁用以维持系统简单性
	// 相关文件保留在 internal/storage/ 目录下供未来使用
	// 包括: engine.go, index_system.go, memory.go, shard.go 等

	// 初始化MinIO客户端
	primaryMinio, err := storage.NewMinioClientWrapper(cfg.MinIO)
	if err != nil {
		logger.Logger.Error("Failed to create primary MinIO client", zap.Error(err))
		os.Exit(1)
	}

	var backupMinio storage.Uploader
	if cfg.Backup.Enabled {
		backupMinio, err = storage.NewMinioClientWrapper(cfg.Backup.MinIO)
		if err != nil {
			logger.Logger.Error("Failed to create backup MinIO client", zap.Error(err))
			os.Exit(1)
		}
	}

	// 初始化并发缓冲区
	concurrentBuffer := buffer.NewConcurrentBuffer(
		poolManager,
		cfg,
		cfg.Backup.MinIO.Bucket,
		cfg.Server.NodeID,
		nil,
	)

	// 初始化ingester服务
	ingesterService := ingest.NewIngester(concurrentBuffer)

	// 初始化查询服务
	querierService, err := query.NewQuerier(redisPool, primaryMinio, cfg, concurrentBuffer, logger.Logger)
	if err != nil {
		logger.Logger.Error("Failed to create querier service", zap.Error(err))
		os.Exit(1)
	}

	// 初始化服务注册与发现
	serviceRegistry, err := discovery.NewServiceRegistry(*cfg, cfg.Server.NodeID, cfg.Server.GrpcPort)
	if err != nil {
		logger.Logger.Error("Failed to create service registry", zap.Error(err))
		os.Exit(1)
	}

	// 初始化服务
	logger.Logger.Info("Initializing services...")

	// 初始化协调器
	writeCoord := coordinator.NewWriteCoordinator(serviceRegistry)

	var localQuerier coordinator.LocalQuerier = querierService
	queryCoord := coordinator.NewQueryCoordinator(
		redisPool,
		serviceRegistry,
		localQuerier,
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
			logger.Logger.Warn("Failed to start metadata manager", zap.Error(err))
		} else {
			logger.Logger.Info("Metadata manager started successfully")
		}
	}

	// 启动指标服务器和告警管理器
	var metricsServer *http.Server
	var systemMonitor *metrics.SystemMonitor
	if cfg.Metrics.Enabled {
		metricsServer = startMetricsServer(cfg)
		systemMonitor = metrics.NewSystemMonitor()
		systemMonitor.Start()
		logger.Logger.Info("Metrics server and system monitor started")
	}

	// 初始化并启动 Compaction Manager
	var compactionManager *compaction.Manager
	if cfg.Compaction.Enabled {
		minioPool := poolManager.GetMinIOPool()
		if minioPool != nil {
			compactionConfig := &compaction.Config{
				TargetFileSize:    cfg.Compaction.TargetFileSize,
				MinFilesToCompact: cfg.Compaction.MinFilesToCompact,
				MaxFilesToCompact: cfg.Compaction.MaxFilesToCompact,
				CooldownPeriod:    cfg.Compaction.CooldownPeriod,
				CheckInterval:     cfg.Compaction.CheckInterval,
				TempDir:           cfg.Compaction.TempDir,
				CompressionType:   cfg.Compaction.CompressionType,
			}

			var err error
			compactionManager, err = compaction.NewManager(minioPool.GetClient(), cfg.MinIO.Bucket, compactionConfig)
			if err != nil {
				logger.Logger.Warn("Failed to create compaction manager", zap.Error(err))
			} else {
				compactionManager.Start(ctx)
				logger.Logger.Info("Compaction manager started",
					zap.Duration("check_interval", cfg.Compaction.CheckInterval),
					zap.Int64("target_file_size", cfg.Compaction.TargetFileSize))
			}
		} else {
			logger.Logger.Warn("Compaction manager disabled - MinIO pool not available")
		}
	}

	// 创建MinIODB服务
	miniodbService, err := service.NewMinIODBService(cfg, ingesterService, querierService, redisPool, metadataManager)
	if err != nil {
		logger.Logger.Error("Failed to create MinIODB service", zap.Error(err))
		os.Exit(1)
	}

	// 启动服务注册
	if err := serviceRegistry.Start(); err != nil {
		logger.Logger.Error("Failed to start service registry", zap.Error(err))
		os.Exit(1)
	}
	defer serviceRegistry.Stop()

	// 启动gRPC服务器
	grpcService, err := grpcTransport.NewServer(miniodbService, *cfg)
	if err != nil {
		logger.Logger.Fatal("Failed to create gRPC service", zap.Error(err))
	}
	grpcService.SetCoordinators(writeCoord, queryCoord)
	if metadataManager != nil {
		grpcService.SetMetadataManager(metadataManager)
	}

	go func() {
		if err := grpcService.Start(cfg.Server.GrpcPort); err != nil {
			logger.Logger.Fatal("Failed to start gRPC server", zap.Error(err))
		}
	}()

	// 启动REST服务器
	restServer := restTransport.NewServer(miniodbService, cfg)
	restServer.SetCoordinators(writeCoord, queryCoord)

	go func() {
		if err := restServer.Start(cfg.Server.RestPort); err != nil {
			logger.Logger.Fatal("Failed to start REST server", zap.Error(err))
		}
	}()

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		go startBackupRoutine(primaryMinio, backupMinio, *cfg)
	}

	logger.Logger.Info("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, grpcService, restServer, metricsServer, systemMonitor, metadataManager, redisPool, compactionManager)
	logger.Logger.Info("MinIODB server stopped")
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc,
	grpcService *grpcTransport.Server, restServer *restTransport.Server,
	metricsServer *http.Server, systemMonitor *metrics.SystemMonitor,
	metadataManager *metadata.Manager, redisPool *pool.RedisPool,
	compactionManager *compaction.Manager) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	logger.Logger.Info("Received shutdown signal")

	// 停止 Compaction Manager
	if compactionManager != nil {
		logger.Logger.Info("Stopping compaction manager...")
		compactionManager.Stop()
		logger.Logger.Info("Compaction manager stopped")
	}

	logger.Logger.Info("Stopping gRPC server...")
	grpcService.Stop()
	logger.Logger.Info("gRPC server stopped")

	logger.Logger.Info("Stopping REST server...")
	ctx2, cancel2 := context.WithTimeout(ctx, 30*time.Second)
	defer cancel2()

	if err := restServer.Stop(ctx2); err != nil {
		logger.Logger.Error("Error shutting down REST server", zap.Error(err))
	} else {
		logger.Logger.Info("REST server stopped")
	}

	logger.Logger.Info("Stopping metrics server...")
	if err := metricsServer.Shutdown(ctx); err != nil {
		logger.Logger.Error("Error shutting down metrics server", zap.Error(err))
	} else {
		logger.Logger.Info("Metrics server stopped")
	}

	logger.Logger.Info("Waiting for background tasks to complete...")
	time.Sleep(2 * time.Second)

	cancel()

	logger.Logger.Info("Shutdown complete")
	os.Exit(0)
}

func startBackupRoutine(primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
	backupInterval := time.Duration(cfg.Backup.Interval) * time.Hour
	if backupInterval == 0 {
		backupInterval = 24 * time.Hour
	}

	ticker := time.NewTicker(backupInterval)
	defer ticker.Stop()

	logger.Logger.Info("Backup routine started", zap.Duration("interval", backupInterval))

	for range ticker.C {
		logger.Logger.Info("Starting scheduled backup...")

		if err := performDataBackup(primaryMinio, backupMinio, cfg); err != nil {
			logger.Logger.Error("Backup failed", zap.Error(err))
		} else {
			logger.Logger.Info("Backup completed successfully")
		}
	}
}

func startMetricsServer(cfg *config.Config) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)

		var metrics []string
		metrics = append(metrics, "# HELP miniodb_info MinIODB service information")
		metrics = append(metrics, "# TYPE miniodb_info gauge")
		metrics = append(metrics, fmt.Sprintf(`miniodb_info{version="1.0.0",node_id="%s"} 1`, cfg.Server.NodeID))
		metrics = append(metrics, "# HELP miniodb_start_time_seconds MinIODB start time in unix timestamp")
		metrics = append(metrics, "# TYPE miniodb_start_time_seconds gauge")
		metrics = append(metrics, fmt.Sprintf("miniodb_start_time_seconds %d", time.Now().Unix()))

		for _, metric := range metrics {
			w.Write([]byte(metric + "\n"))
		}

		logger.Logger.Debug("Metrics endpoint accessed", zap.Int("metric_lines", len(metrics)))
	})

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Metrics.Port),
		Handler: mux,
	}

	go func() {
		logger.Logger.Info("Starting metrics server", zap.String("port", cfg.Metrics.Port))
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Logger.Fatal("Failed to start metrics server", zap.Error(err))
		}
	}()

	return metricsServer
}

func performDataBackup(primaryMinio, backupMinio storage.Uploader, cfg config.Config) error {
	ctx := context.Background()
	maxRetries := 3
	retryDelay := 5 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logger.Logger.Warn("Retry attempt", zap.Int("attempt", attempt), zap.Int("max_retries", maxRetries))
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
		logger.Logger.Warn("Backup completed with errors", zap.Int("error_count", len(errors)))
		for _, err := range errors {
			logger.Logger.Debug("Backup error", zap.String("error", err))
		}
	}

	duration := time.Since(backupStartTime)
	logger.Logger.Info("Backup summary", zap.Int64("objects", backupCount), zap.Int64("size_bytes", totalSize), zap.Duration("duration", duration))

	if backupCount == 0 {
		return fmt.Errorf("no objects were backed up")
	}

	return nil
}

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

// 下面的函数保持不变...
