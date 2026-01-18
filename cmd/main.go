// @title           MinIODB API
// @version         1.0
// @description     基于MinIO+DuckDB+Redis的分布式OLAP系统API
// @description     支持高性能数据写入、SQL查询、表管理、元数据备份等功能

// @contact.name   MinIODB Support
// @contact.url    https://github.com/richenlin/minIODB

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8081
// @BasePath  /v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT Bearer Token认证，格式: Bearer {token}

// @tag.name 认证
// @tag.description 用户认证和Token管理

// @tag.name 数据操作
// @tag.description 数据写入、查询、更新、删除

// @tag.name 表管理
// @tag.description 表的创建、查询、删除

// @tag.name 元数据
// @tag.description 元数据备份、恢复、状态查询

// @tag.name 系统监控
// @tag.description 健康检查、状态查询、性能指标

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/minio/minio-go/v7"

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
	"minIODB/internal/subscription"
	grpcTransport "minIODB/internal/transport/grpc"
	restTransport "minIODB/internal/transport/rest"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"
)

func main() {
	// 创建全局WaitGroup用于等待所有goroutine
	var wg sync.WaitGroup

	// 解析命令行参数，支持多个默认配置文件路径
	configPath := ""
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	} else {
		// 尝试默认配置文件路径
		defaultPaths := []string{
			"config/config.yaml",
			"config.yaml",
			"./config/config.yaml",
		}
		for _, path := range defaultPaths {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		// 配置加载失败时logger还未初始化，使用默认logger
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志（必须在加载配置后，使用日志前）
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

	logger.Sugar.Infof("Starting MinIODB server with config: %s", configPath)

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建恢复处理器
	recoveryHandler := recovery.NewRecoveryHandler("main", logger.Logger)
	defer recoveryHandler.Recover()

	// 初始化存储层
	storageInstance, err := storage.NewStorage(cfg, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create storage instance: %v", err)
	}
	defer storageInstance.Close()

	// 获取连接池管理器
	poolManager := storageInstance.GetPoolManager()

	// 获取Redis连接池（支持高可用）
	redisPool := poolManager.GetRedisPool()
	if redisPool != nil {
		logger.Sugar.Info("Redis connection pool initialized successfully")
	} else {
		logger.Sugar.Warn("Redis disabled - running in single-node mode")
	}

	// 注意：高级存储引擎优化功能已实现但当前保持禁用以维持系统简单性
	// 相关文件保留在 internal/storage/ 目录下供未来使用
	// 包括: engine.go, index_system.go, memory.go, shard.go 等

	// 初始化MinIO客户端
	primaryMinio, err := storage.NewMinioClientWrapper(cfg.MinIO, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create primary MinIO client: %v", err)
	}

	var backupMinio storage.Uploader
	if cfg.Backup.Enabled {
		backupMinio, err = storage.NewMinioClientWrapper(cfg.Backup.MinIO, logger.Logger)
		if err != nil {
			logger.Sugar.Fatalf("Failed to create backup MinIO client: %v", err)
		}
	}

	// 初始化并发缓冲区
	concurrentBuffer := buffer.NewConcurrentBuffer(
		poolManager,
		cfg,
		cfg.Backup.MinIO.Bucket,
		cfg.Server.NodeID,
		nil,
		logger.Logger,
	)

	// 初始化ingester服务
	ingesterService := ingest.NewIngester(concurrentBuffer)

	// 初始化查询服务
	querierService, err := query.NewQuerier(redisPool, primaryMinio, cfg, concurrentBuffer, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create querier service: %v", err)
	}

	// 初始化服务注册与发现
	serviceRegistry, err := discovery.NewServiceRegistry(*cfg, cfg.Server.NodeID, cfg.Server.GrpcPort, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create service registry: %v", err)
	}

	// 初始化服务
	logger.Sugar.Info("Initializing services...")

	// 初始化协调器
	writeCoord := coordinator.NewWriteCoordinator(serviceRegistry, cfg, logger.Logger)

	var localQuerier coordinator.LocalQuerier = querierService
	queryCoord := coordinator.NewQueryCoordinator(
		redisPool,
		serviceRegistry,
		localQuerier,
		cfg,
		logger.Logger,
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

		metadataManager = metadata.NewManager(storageInstance, storageInstance, &metadataConfig, logger.Logger)

		if err := metadataManager.Start(); err != nil {
			logger.Sugar.Warnf("Failed to start metadata manager: %v", err)
		} else {
			logger.Sugar.Info("Metadata manager started successfully")
		}
	}

	// 启动指标服务器和告警管理器
	var metricsServer *http.Server
	var systemMonitor *metrics.SystemMonitor
	if cfg.Metrics.Enabled {
		metricsServer = startMetricsServer(cfg)
		systemMonitor = metrics.NewSystemMonitor()
		systemMonitor.Start()
		logger.Sugar.Info("Metrics server and system monitor started")
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
			compactionManager, err = compaction.NewManager(minioPool.GetClient(), cfg.MinIO.Bucket, compactionConfig, logger.Logger)
			if err != nil {
				logger.Sugar.Warnf("Failed to create compaction manager: %v", err)
			} else {
				compactionManager.Start(ctx)
				logger.Sugar.Infof("Compaction manager started - check_interval: %v, target_file_size: %d",
					cfg.Compaction.CheckInterval, cfg.Compaction.TargetFileSize)
			}
		} else {
			logger.Sugar.Warn("Compaction manager disabled - MinIO pool not available")
		}
	}

	// 初始化数据订阅管理器
	var subscriptionManager *subscription.Manager
	if cfg.Subscription.Enabled {
		subscriptionManager = subscription.NewManager(logger.Logger)

		// 初始化 Redis 订阅者
		if cfg.Subscription.Redis.Enabled && redisPool != nil {
			redisSubscriber, err := subscription.NewRedisSubscriber(redisPool, &cfg.Subscription.Redis, logger.Logger)
			if err != nil {
				logger.Sugar.Warnf("Failed to create Redis subscriber: %v", err)
			} else {
				if err := subscriptionManager.RegisterSubscriber(redisSubscriber); err != nil {
					logger.Sugar.Warnf("Failed to register Redis subscriber: %v", err)
				}
			}
		}

		// 初始化 Kafka 订阅者
		if cfg.Subscription.Kafka.Enabled {
			kafkaSubscriber, err := subscription.NewKafkaSubscriber(&cfg.Subscription.Kafka, logger.Logger)
			if err != nil {
				logger.Sugar.Warnf("Failed to create Kafka subscriber: %v", err)
			} else {
				if err := subscriptionManager.RegisterSubscriber(kafkaSubscriber); err != nil {
					logger.Sugar.Warnf("Failed to register Kafka subscriber: %v", err)
				}
			}
		}

		// 启动订阅管理器
		if subscriptionManager.SubscriberCount() > 0 {
			if err := subscriptionManager.Start(ctx); err != nil {
				logger.Sugar.Warnf("Failed to start subscription manager: %v", err)
			} else {
				logger.Sugar.Infof("Subscription manager started - subscriber_count: %d",
					subscriptionManager.SubscriberCount())
			}
		}
	}

	// 创建MinIODB服务
	miniodbService, err := service.NewMinIODBService(cfg, logger.Logger, ingesterService, querierService, redisPool, metadataManager)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create MinIODB service: %v", err)
	}

	// 设置订阅管理器到服务（用于发布写入事件）
	if subscriptionManager != nil {
		miniodbService.SetSubscriptionManager(subscriptionManager)
	}

	// 启动服务注册
	if err := serviceRegistry.Start(); err != nil {
		logger.Sugar.Fatalf("Failed to start service registry: %v", err)
	}
	defer serviceRegistry.Stop()

	// 启动gRPC服务器
	grpcService, err := grpcTransport.NewServer(miniodbService, *cfg, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create gRPC service: %v", err)
	}
	grpcService.SetCoordinators(writeCoord, queryCoord)
	if metadataManager != nil {
		grpcService.SetMetadataManager(metadataManager)
	}

	go func() {
		if err := grpcService.Start(cfg.Server.GrpcPort); err != nil {
			logger.Sugar.Fatalf("Failed to start gRPC server: %v", err)
		}
	}()

	// 启动REST服务器
	restServer, err := restTransport.NewServer(miniodbService, cfg, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create REST server: %v", err)
	}
	restServer.SetCoordinators(writeCoord, queryCoord)

	go func() {
		if err := restServer.Start(cfg.Server.RestPort); err != nil {
			logger.Sugar.Fatalf("Failed to start REST server: %v", err)
		}
	}()

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startBackupRoutine(ctx, primaryMinio, backupMinio, *cfg)
		}()
	}

	logger.Sugar.Info("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, &wg, grpcService, restServer, metricsServer, systemMonitor, metadataManager, redisPool, compactionManager, subscriptionManager)
	logger.Sugar.Info("MinIODB server stopped")
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup,
	grpcService *grpcTransport.Server, restServer *restTransport.Server,
	metricsServer *http.Server, systemMonitor *metrics.SystemMonitor,
	metadataManager *metadata.Manager, redisPool *pool.RedisPool,
	compactionManager *compaction.Manager, subscriptionManager *subscription.Manager) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	logger.Sugar.Info("Received shutdown signal")

	// 1. 首先取消Context，通知所有goroutine停止
	logger.Sugar.Info("Canceling all contexts...")
	cancel()

	// 2. 停止接收新请求
	logger.Sugar.Info("Stopping gRPC server...")
	grpcService.Stop()
	logger.Sugar.Info("gRPC server stopped")

	logger.Sugar.Info("Stopping REST server...")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	if err := restServer.Stop(ctx2); err != nil {
		logger.Sugar.Errorf("Error shutting down REST server: %v", err)
	} else {
		logger.Sugar.Info("REST server stopped")
	}

	logger.Sugar.Info("Stopping metrics server...")
	if err := metricsServer.Shutdown(context.Background()); err != nil {
		logger.Sugar.Errorf("Error shutting down metrics server: %v", err)
	} else {
		logger.Sugar.Info("Metrics server stopped")
	}

	// 3. 停止后台任务
	// 停止订阅管理器
	if subscriptionManager != nil {
		logger.Sugar.Info("Stopping subscription manager...")
		if err := subscriptionManager.Stop(ctx); err != nil {
			logger.Sugar.Errorf("Error stopping subscription manager: %v", err)
		} else {
			logger.Sugar.Info("Subscription manager stopped")
		}
	}

	if compactionManager != nil {
		logger.Sugar.Info("Stopping compaction manager...")
		compactionManager.Stop()
		logger.Sugar.Info("Compaction manager stopped")
	}

	// 4. 等待所有goroutine完成（带超时）
	logger.Sugar.Info("Waiting for background tasks to complete...")
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Sugar.Info("All goroutines stopped gracefully")
	case <-time.After(30 * time.Second):
		logger.Sugar.Warn("Timeout waiting for goroutines, forcing shutdown")
	}

	logger.Sugar.Info("Shutdown complete")
	os.Exit(0)
}

func startBackupRoutine(ctx context.Context, primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
	backupInterval := time.Duration(cfg.Backup.Interval) * time.Hour
	if backupInterval == 0 {
		backupInterval = 24 * time.Hour
	}

	ticker := time.NewTicker(backupInterval)
	defer ticker.Stop()

	logger.Sugar.Infof("Backup routine started - interval: %v", backupInterval)

	for {
		select {
		case <-ticker.C:
			logger.Sugar.Info("Starting scheduled backup...")
			if err := performDataBackup(primaryMinio, backupMinio, cfg); err != nil {
				logger.Sugar.Errorf("Backup failed: %v", err)
			} else {
				logger.Sugar.Info("Backup completed successfully")
			}
		case <-ctx.Done():
			logger.Sugar.Info("Backup routine stopped gracefully")
			return
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

		logger.Sugar.Debugf("Metrics endpoint accessed - metric_lines: %d", len(metrics))
	})

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Metrics.Port),
		Handler: mux,
	}

	go func() {
		logger.Sugar.Infof("Starting metrics server"+": %s", "port", cfg.Metrics.Port)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Sugar.Fatalf("Failed to start metrics server: %v", err)
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
			logger.Sugar.Warnf("Retry attempt %d/%d", attempt, maxRetries)
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
		logger.Sugar.Warnf("Backup completed with %d errors", len(errors))
		for _, err := range errors {
			logger.Sugar.Debugf("Backup error: %s", err)
		}
	}

	duration := time.Since(backupStartTime)
	logger.Sugar.Infof("Backup summary - objects: %d, size: %d bytes, duration: %v", backupCount, totalSize, duration)

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
