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

	"golang.org/x/crypto/bcrypt"

	"minIODB/config"
	"minIODB/internal/backup"
	"minIODB/internal/buffer"
	"minIODB/internal/compaction"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
	"minIODB/internal/metadata"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/recovery"
	"minIODB/internal/replication"
	"minIODB/internal/service"
	"minIODB/internal/storage"
	"minIODB/internal/subscription"
	grpcTransport "minIODB/internal/transport/grpc"
	restTransport "minIODB/internal/transport/rest"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"
	"minIODB/pkg/version"

	"minIODB/internal/dashboard"
	"minIODB/internal/dashboard/logbuffer"
	"minIODB/internal/dashboard/sse"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

func main() {
	// 处理 --hash-password 命令行参数（用于生成密码哈希）
	if len(os.Args) > 1 && os.Args[1] == "--hash-password" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: miniodb --hash-password <password>")
			os.Exit(1)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(os.Args[2]), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(hash))
		os.Exit(0)
	}

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

	// 提前创建日志缓冲区并设置捕获器，确保从启动开始的所有日志都被捕获
	// hub 在 Dashboard 创建后再设置
	var logBuf *logbuffer.LogBuffer
	var captureWriter *logger.CaptureWriter
	if cfg.Dashboard.Enabled {
		logBuf = logbuffer.NewBuffer(5000)
		captureWriter = logger.NewCaptureWriter(
			logger.NewMultiWriteSyncerFromBase(),
			logBuf,
			nil, // hub 稍后设置
		)
		logger.SetCaptureWriter(captureWriter)
	}

	logger.Sugar.Infof("Starting MinIODB server with config: %s", configPath)

	// JWT Secret 强制检查：当 auth mode 为 "token" 或 "jwt" 时，必须配置 JWT Secret
	if cfg.Security.Mode == "token" || cfg.Security.Mode == "jwt" {
		if cfg.Security.JWTSecret == "" {
			logger.Sugar.Fatalf("FATAL: JWT secret is required when auth mode is '%s'. Please set JWT_SECRET environment variable or configure security.jwt_secret in config file", cfg.Security.Mode)
		}
	}

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

	// 初始化MinIO客户端（使用统一配置 network.pools.minio）
	primaryMinio, err := storage.NewMinioClientWrapper(cfg.GetMinIO(), logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create primary MinIO client: %v", err)
	}

	// 初始化并发缓冲区
	concurrentBuffer := buffer.NewConcurrentBuffer(
		poolManager,
		cfg,
		cfg.GetBackupMinIO().Bucket,
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

	// 初始化元数据管理器
	// - 完整模式（Redis + 元数据备份已启用）：全量元数据备份/恢复功能可用
	// - standalone 模式（无 Redis）：元数据管理器仍然创建，但只用于表配置的
	//   MinIO 降级存储；备份/恢复功能不可用（gracefully degraded）
	var metadataManager *metadata.Manager
	{
		// 使用主 MinIO bucket 作为 standalone 降级存储的 bucket
		standaloneBucket := cfg.GetMinIO().Bucket

		// 备份 bucket 优先用配置的元数据专用 bucket，否则回落到主 bucket
		backupBucket := cfg.Backup.Metadata.Bucket
		if backupBucket == "" {
			backupBucket = standaloneBucket
		}

		metadataConfig := metadata.Config{
			NodeID: cfg.Server.NodeID,
			Backup: metadata.BackupConfig{
				Enabled:       cfg.Backup.Metadata.Enabled,
				Interval:      cfg.Backup.Metadata.Interval,
				RetentionDays: cfg.Backup.Metadata.RetentionDays,
				Bucket:        backupBucket,
			},
			Recovery: metadata.RecoveryConfig{
				Bucket: backupBucket,
			},
		}

		metadataManager = metadata.NewManager(storageInstance, storageInstance, &metadataConfig, logger.Logger)

		if cfg.Backup.Metadata.Enabled {
			if err := metadataManager.Start(); err != nil {
				logger.Sugar.Warnf("Failed to start metadata manager: %v", err)
			} else {
				logger.Sugar.Info("Metadata manager started successfully")
			}
		} else if redisPool == nil {
			// standalone 模式（无 Redis + 未启用元数据备份）：
			// 仅用于表配置 MinIO 降级，不启动备份定时任务
			logger.Sugar.Info("Metadata manager created in standalone mode (table config MinIO fallback only, no backup scheduler)")
		}
	}

	// 初始化全局 RuntimeCollector（统一运行时采样）
	poolStatsProvider := func() (activeConns, maxConns int64) {
		minioPool := poolManager.GetMinIOPool()
		if minioPool == nil {
			return 0, 0
		}
		stats := minioPool.GetStats()
		if stats == nil {
			return 0, 0
		}
		return stats.ActiveConns, stats.ActiveConns + stats.IdleConns
	}
	bufferStatsProvider := func() int64 {
		return int64(concurrentBuffer.PendingWrites())
	}
	rc := metrics.InitGlobalRuntimeCollector(
		metrics.WithPoolStatsProvider(poolStatsProvider),
		metrics.WithBufferStatsProvider(bufferStatsProvider),
		metrics.WithThresholds(metrics.DefaultLoadThresholds()),
		metrics.WithInterval(5*time.Second),
	)
	rc.Start(ctx)
	logger.Sugar.Info("RuntimeCollector started")

	// 启动指标服务器和告警管理器（Dashboard 启用时由 9090 上的 dashboard 服务一并提供 /metrics）
	var metricsServer *http.Server
	if cfg.Metrics.Enabled && !cfg.Dashboard.Enabled {
		metricsServer = startMetricsServer(cfg)
		logger.Sugar.Info("Metrics server started")
	}

	// 初始化并启动 Compaction Manager
	var compactionManager *compaction.Manager
	if cfg.Compaction.Enabled {
		minioPool := poolManager.GetMinIOPool()
		if minioPool != nil {
			// 构建分层合并配置
			var tieredConfig *compaction.TieredCompactionConfig
			if cfg.Compaction.Tiered.Enabled {
				tieredConfig = &compaction.TieredCompactionConfig{
					Enabled:         cfg.Compaction.Tiered.Enabled,
					Levels:          make([]compaction.CompactionLevel, len(cfg.Compaction.Tiered.Levels)),
					MaxFilesToMerge: cfg.Compaction.Tiered.MaxFilesToMerge,
					CheckInterval:   cfg.Compaction.CheckInterval,
					TempDir:         cfg.Compaction.TempDir,
					CompressionType: cfg.Compaction.CompressionType,
					MaxRowsPerFile:  cfg.Compaction.MaxRowsPerFile,
				}
				for i, level := range cfg.Compaction.Tiered.Levels {
					tieredConfig.Levels[i] = compaction.CompactionLevel{
						Name:            level.Name,
						MaxFileSize:     level.MaxFileSize,
						TargetFileSize:  level.TargetFileSize,
						MinFilesToMerge: level.MinFilesToMerge,
						CooldownPeriod:  level.CooldownPeriod,
					}
				}
			}

			compactionConfig := &compaction.Config{
				TargetFileSize:    cfg.Compaction.TargetFileSize,
				MinFilesToCompact: cfg.Compaction.MinFilesToCompact,
				MaxFilesToCompact: cfg.Compaction.MaxFilesToCompact,
				CooldownPeriod:    cfg.Compaction.CooldownPeriod,
				CheckInterval:     cfg.Compaction.CheckInterval,
				TempDir:           cfg.Compaction.TempDir,
				CompressionType:   cfg.Compaction.CompressionType,
				MaxRowsPerFile:    cfg.Compaction.MaxRowsPerFile,
				TieredConfig:      tieredConfig,
			}

			var err error
			compactionManager, err = compaction.NewManager(minioPool.GetClient(), cfg.GetMinIO().Bucket, compactionConfig, logger.Logger)
			if err != nil {
				logger.Sugar.Warnf("Failed to create compaction manager: %v", err)
			} else {
				compactionManager.Start(ctx)
				tieredStatus := "disabled"
				if tieredConfig != nil && tieredConfig.Enabled {
					tieredStatus = fmt.Sprintf("enabled (%d levels)", len(tieredConfig.Levels))
				}
				logger.Sugar.Infof("Compaction manager started - check_interval: %v, target_file_size: %d, tiered: %s",
					cfg.Compaction.CheckInterval, cfg.Compaction.TargetFileSize, tieredStatus)
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
	// 在 standalone 模式（无 Redis）下，把 MinIO client 传给 service 层以持久化表配置
	var serviceMinioClient *minio.Client
	if minioPool := poolManager.GetMinIOPool(); minioPool != nil {
		serviceMinioClient = minioPool.GetClient()
	}
	miniodbService, err := service.NewMinIODBService(cfg, logger.Logger, ingesterService, querierService, redisPool, metadataManager, serviceMinioClient)
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
	grpcService, err := grpcTransport.NewServerWithMinIO(miniodbService, *cfg, primaryMinio, logger.Logger)
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
	restServer, err := restTransport.NewServerWithMinIO(miniodbService, cfg, primaryMinio, logger.Logger)
	if err != nil {
		logger.Sugar.Fatalf("Failed to create REST server: %v", err)
	}
	restServer.SetCoordinators(writeCoord, queryCoord)
	if metadataManager != nil {
		restServer.SetMetadataManager(metadataManager)
	}

	// 启动 Dashboard（如果启用）
	var dashSrv *dashboard.Server
	if cfg.Dashboard.Enabled {
		dashSrv, err = dashboard.NewServer(
			miniodbService,
			cfg,
			logger.Logger,
			restServer.AuthManager(),
			logBuf,
		)
		if err != nil {
			logger.Sugar.Fatalf("Failed to create dashboard server: %v", err)
		}
		if captureWriter != nil {
			captureWriter.SetHub(dashSrv.GetHub())
		}
		dashSrv.MountRoutes(restServer.Router().Group(cfg.Dashboard.BasePath))
		go func() {
			dashSrv.Start(ctx)
		}()
		logger.Sugar.Infof("Dashboard mounted at %s on port %s", cfg.Dashboard.BasePath, cfg.Server.RestPort)
	}

	go func() {
		if err := restServer.Start(cfg.Server.RestPort); err != nil {
			logger.Sugar.Fatalf("Failed to start REST server: %v", err)
		}
	}()

	replicator := initReplication(ctx, cfg, poolManager, redisPool, logger.Logger)

	var sseHub *sse.Hub
	if dashSrv != nil {
		sseHub = dashSrv.GetHub()
	}
	backupScheduler, executor := initBackupSubsystem(ctx, cfg, poolManager, primaryMinio, metadataManager, redisPool, sseHub, logger.Logger)

	// Wire dashboard references after subsystem init
	if dashSrv != nil {
		if replicator != nil {
			dashSrv.SetReplicator(replicator)
		}
		if backupScheduler != nil {
			dashSrv.SetBackupScheduler(backupScheduler)
		}
		if executor != nil {
			dashSrv.SetBackupStore(executor.GetPlanStore())
		}
		// 热切换分布式模式所需依赖
		dashSrv.SetPoolManager(poolManager)
		dashSrv.SetConfigPath(configPath)
		dashSrv.SetServiceRegistry(serviceRegistry)
		dashSrv.SetQueryCoordinator(queryCoord)
	}

	logger.Sugar.Info("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, &wg, grpcService, restServer, metricsServer, metadataManager, redisPool, compactionManager, subscriptionManager, dashSrv, replicator, backupScheduler)
	logger.Sugar.Info("MinIODB server stopped")
}

func initReplication(
	ctx context.Context,
	cfg *config.Config,
	poolManager *pool.PoolManager,
	redisPool *pool.RedisPool,
	zapLogger *zap.Logger,
) *replication.Replicator {
	backupPool := poolManager.GetBackupMinIOPool()
	if backupPool == nil {
		zapLogger.Sugar().Info("Backup MinIO not configured, replication disabled")
		return nil
	}

	srcPool := poolManager.GetMinIOPool()
	dstPool := backupPool
	dstUploader, err := storage.NewMinioClientWrapperFromClient(dstPool.GetClient(), zapLogger)
	if err != nil {
		zapLogger.Sugar().Warnf("Failed to create dst uploader: %v, replication disabled", err)
		return nil
	}

	replicator, err := replication.NewReplicator(
		srcPool,
		dstUploader,
		&cfg.Backup.Replication,
		redisPool,
		zapLogger,
		replication.WithReplicatorSnapshotProvider(metrics.GetRuntimeSnapshot),
	)
	if err != nil {
		zapLogger.Sugar().Warnf("Failed to create replicator: %v, replication disabled", err)
		return nil
	}

	go replicator.Start(ctx)
	zapLogger.Sugar().Info("Replicator started")
	return replicator
}

func initBackupSubsystem(
	ctx context.Context,
	cfg *config.Config,
	poolManager *pool.PoolManager,
	primaryMinio storage.Uploader,
	metadataManager *metadata.Manager,
	redisPool *pool.RedisPool,
	hub *sse.Hub,
	zapLogger *zap.Logger,
) (*backup.Scheduler, *backup.Executor) {
	// 冷备总开关：backup.enabled 控制
	if !cfg.Backup.Enabled {
		return nil, nil
	}

	// backup.enabled=true：注入系统内置备份计划
	injectSystemPlans(cfg, zapLogger)

	backupTarget := backup.ResolveBackupTarget(cfg, poolManager, zapLogger)
	if backupTarget == nil {
		zapLogger.Sugar().Warn("Backup target not available, scheduler disabled")
		return nil, nil
	}

	planStore := backup.NewRedisPlanStore(redisPool, zapLogger)
	executor := backup.NewExecutor(primaryMinio, backupTarget, metadataManager, redisPool, planStore, cfg, zapLogger)
	backupScheduler := backup.NewScheduler(executor, planStore, hub, zapLogger)

	for i := range cfg.Backup.Schedules {
		if cfg.Backup.Schedules[i].ID == "" {
			cfg.Backup.Schedules[i].ID = fmt.Sprintf("schedule-%d", i)
		}
		if err := backupScheduler.AddPlan(&cfg.Backup.Schedules[i]); err != nil {
			zapLogger.Sugar().Warnf("Failed to add backup schedule %s: %v", cfg.Backup.Schedules[i].Name, err)
		}
	}

	if err := backupScheduler.Start(ctx); err != nil {
		zapLogger.Sugar().Warnf("Failed to start backup scheduler: %v", err)
	} else {
		zapLogger.Sugar().Infof("Backup scheduler started - plans: %d", len(cfg.Backup.Schedules))
	}

	return backupScheduler, executor
}

// injectSystemPlans 注入系统内置备份计划（backup.enabled=true 时调用）
func injectSystemPlans(cfg *config.Config, zapLogger *zap.Logger) {
	hasMetadataPlan := false
	hasFullPlan := false
	for _, s := range cfg.Backup.Schedules {
		if s.BackupType == "metadata" {
			hasMetadataPlan = true
		}
		if s.BackupType == "full" {
			hasFullPlan = true
		}
	}

	// 自动元数据备份计划
	if !hasMetadataPlan && cfg.Backup.Metadata.Enabled {
		interval := cfg.Backup.Metadata.Interval
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		cfg.Backup.Schedules = append(cfg.Backup.Schedules, config.BackupSchedule{
			ID:            "system-metadata-backup",
			Name:          "System Metadata Backup",
			Enabled:       true,
			BackupType:    "metadata",
			Interval:      interval,
			RetentionDays: 7,
			System:        true,
		})
		zapLogger.Sugar().Infof("Injected system metadata backup plan (interval: %v)", interval)
	}

	// 自动全量备份计划
	if !hasFullPlan {
		interval := time.Duration(cfg.Backup.Interval) * time.Second
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		cfg.Backup.Schedules = append(cfg.Backup.Schedules, config.BackupSchedule{
			ID:            "system-full-backup",
			Name:          "System Full Backup",
			Enabled:       true,
			BackupType:    "full",
			Interval:      interval,
			RetentionDays: 7,
			System:        true,
		})
		zapLogger.Sugar().Infof("Injected system full backup plan (interval: %v)", interval)
	}
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup,
	grpcService *grpcTransport.Server, restServer *restTransport.Server,
	metricsServer *http.Server, metadataManager *metadata.Manager, redisPool *pool.RedisPool,
	compactionManager *compaction.Manager, subscriptionManager *subscription.Manager,
	dashSrv *dashboard.Server, replicator *replication.Replicator, backupScheduler *backup.Scheduler) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	logger.Sugar.Info("Received shutdown signal")

	// 1. 首先取消Context，通知所有goroutine停止
	logger.Sugar.Info("Canceling all contexts...")
	cancel()

	// 创建独立的 shutdownCtx 用于关闭操作，避免使用已取消的 ctx
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 2. 停止接收新请求
	logger.Sugar.Info("Stopping gRPC server...")
	grpcService.Stop()
	logger.Sugar.Info("gRPC server stopped")

	logger.Sugar.Info("Stopping REST server...")
	if err := restServer.Stop(shutdownCtx); err != nil {
		logger.Sugar.Errorf("Error shutting down REST server: %v", err)
	} else {
		logger.Sugar.Info("REST server stopped")
	}

	if dashSrv != nil {
		logger.Sugar.Info("Stopping Dashboard server...")
		dashSrv.Stop()
		logger.Sugar.Info("Dashboard server stopped")
	}

	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			logger.Sugar.Errorf("Error shutting down metrics server: %v", err)
		} else {
			logger.Sugar.Info("Metrics server stopped")
		}
	}

	// 3. 停止后台任务
	// 停止 Replicator
	if replicator != nil {
		logger.Sugar.Info("Stopping replicator...")
		replicator.Stop()
		logger.Sugar.Info("Replicator stopped")
	}

	// 停止 Backup Scheduler
	if backupScheduler != nil {
		logger.Sugar.Info("Stopping backup scheduler...")
		backupScheduler.Stop()
		logger.Sugar.Info("Backup scheduler stopped")
	}

	// 停止订阅管理器
	if subscriptionManager != nil {
		logger.Sugar.Info("Stopping subscription manager...")
		if err := subscriptionManager.Stop(shutdownCtx); err != nil {
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

func getMetricsHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		var lines []string
		lines = append(lines, "# HELP miniodb_info MinIODB service information")
		lines = append(lines, "# TYPE miniodb_info gauge")
		lines = append(lines, fmt.Sprintf(`miniodb_info{version="%s",node_id="%s"} 1`, version.Get(), cfg.Server.NodeID))
		lines = append(lines, "# HELP miniodb_start_time_seconds MinIODB start time in unix timestamp")
		lines = append(lines, "# TYPE miniodb_start_time_seconds gauge")
		lines = append(lines, fmt.Sprintf("miniodb_start_time_seconds %d", time.Now().Unix()))
		for _, line := range lines {
			w.Write([]byte(line + "\n"))
		}
		logger.Sugar.Debugf("Metrics endpoint accessed - metric_lines: %d", len(lines))
	}
}

func startMetricsServer(cfg *config.Config) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", getMetricsHandler(cfg))
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
