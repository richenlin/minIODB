package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"minIODB/config"
	"minIODB/internal/backup"
	"minIODB/internal/buffer"
	"minIODB/internal/compaction"
	"minIODB/internal/coordinator"
	"minIODB/internal/dashboard"
	"minIODB/internal/dashboard/logbuffer"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
	"minIODB/internal/metadata"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/replication"
	"minIODB/internal/service"
	"minIODB/internal/storage"
	"minIODB/internal/subscription"
	grpcTransport "minIODB/internal/transport/grpc"
	restTransport "minIODB/internal/transport/rest"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"
	"minIODB/pkg/version"

	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// App 聚合所有服务组件，管理应用生命周期
type App struct {
	cfg    *config.Config
	logger *zap.Logger

	// Configuration path (for hot reload)
	configPath string

	// Infrastructure
	storageInstance storage.Storage
	poolManager     *pool.PoolManager
	redisPool       *pool.RedisPool
	primaryMinio    *storage.MinioClientWrapper

	// Core Services
	buffer          *buffer.ConcurrentBuffer
	ingester        *ingest.Ingester
	querier         *query.Querier
	miniodbService  *service.MinIODBService
	metadataManager *metadata.Manager

	// Coordination
	serviceRegistry *discovery.ServiceRegistry
	writeCoord      *coordinator.WriteCoordinator
	queryCoord      *coordinator.QueryCoordinator

	// Transport
	grpcServer *grpcTransport.Server
	restServer *restTransport.Server
	dashSrv    *dashboard.Server

	// Background Services
	replicator        *replication.Replicator
	backupScheduler   *backup.Scheduler
	backupExecutor    *backup.Executor
	compactionManager *compaction.Manager
	subscriptionMgr   *subscription.Manager
	metricsServer     *http.Server

	// Dashboard helpers
	logBuf        *logbuffer.LogBuffer
	captureWriter *logger.CaptureWriter

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	fatalCh chan error
}

// NewApp 创建新的 App 实例
func NewApp(cfg *config.Config, logger *zap.Logger, configPath string) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		cfg:        cfg,
		logger:     logger,
		configPath: configPath,
		ctx:        ctx,
		cancel:     cancel,
		fatalCh:    make(chan error, 3),
	}
}

// InitStorage 初始化存储层
func (a *App) InitStorage() error {
	var err error
	a.storageInstance, err = storage.NewStorage(a.cfg, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create storage instance: %w", err)
	}

	a.poolManager = a.storageInstance.GetPoolManager()
	a.redisPool = a.poolManager.GetRedisPool()

	if a.redisPool != nil {
		a.logger.Sugar().Info("Redis connection pool initialized successfully")
	} else {
		a.logger.Sugar().Warn("Redis disabled - running in single-node mode")
	}

	a.primaryMinio, err = storage.NewMinioClientWrapper(a.cfg.GetMinIO(), a.logger)
	if err != nil {
		return fmt.Errorf("failed to create primary MinIO client: %w", err)
	}

	return nil
}

// InitServices 初始化核心服务
func (a *App) InitServices() error {
	// 初始化并发缓冲区
	a.buffer = buffer.NewConcurrentBuffer(
		a.poolManager,
		a.cfg,
		a.cfg.GetBackupMinIO().Bucket,
		a.cfg.Server.NodeID,
		nil,
		a.logger,
	)

	// 初始化 ingester 服务
	a.ingester = ingest.NewIngester(a.buffer)

	// 初始化查询服务
	var err error
	a.querier, err = query.NewQuerier(a.redisPool, a.primaryMinio, a.cfg, a.buffer, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create querier service: %w", err)
	}

	// 初始化服务注册与发现
	a.serviceRegistry, err = discovery.NewServiceRegistry(*a.cfg, a.cfg.Server.NodeID, a.cfg.Server.GrpcPort, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create service registry: %w", err)
	}

	// 初始化协调器
	a.writeCoord = coordinator.NewWriteCoordinator(a.serviceRegistry, a.cfg, a.logger)

	var localQuerier coordinator.LocalQuerier = a.querier
	a.queryCoord = coordinator.NewQueryCoordinator(
		a.redisPool,
		a.serviceRegistry,
		localQuerier,
		a.cfg,
		a.logger,
	)

	// 创建 MinIODB 服务
	var serviceMinioClient *minio.Client
	if minioPool := a.poolManager.GetMinIOPool(); minioPool != nil {
		serviceMinioClient = minioPool.GetClient()
	}
	a.miniodbService, err = service.NewMinIODBService(a.cfg, a.logger, a.ingester, a.querier, a.redisPool, a.metadataManager, serviceMinioClient)
	if err != nil {
		return fmt.Errorf("failed to create MinIODB service: %w", err)
	}

	// 设置订阅管理器到服务（用于发布写入事件）
	if a.subscriptionMgr != nil {
		a.miniodbService.SetSubscriptionManager(a.subscriptionMgr)
	}

	// 启动服务注册
	if err := a.serviceRegistry.Start(); err != nil {
		return fmt.Errorf("failed to start service registry: %w", err)
	}

	return nil
}

// InitMetadata 初始化元数据管理器
func (a *App) InitMetadata() error {
	// 使用主 MinIO bucket 作为 standalone 降级存储的 bucket
	standaloneBucket := a.cfg.GetMinIO().Bucket

	// 备份 bucket 优先用配置的元数据专用 bucket，否则回落到主 bucket
	backupBucket := a.cfg.Backup.Metadata.Bucket
	if backupBucket == "" {
		backupBucket = standaloneBucket
	}

	metadataConfig := metadata.Config{
		NodeID: a.cfg.Server.NodeID,
		Backup: metadata.BackupConfig{
			Enabled:       a.cfg.Backup.Metadata.Enabled,
			Interval:      a.cfg.Backup.Metadata.Interval,
			RetentionDays: a.cfg.Backup.Metadata.RetentionDays,
			Bucket:        backupBucket,
		},
		Recovery: metadata.RecoveryConfig{
			Bucket: backupBucket,
		},
	}

	a.metadataManager = metadata.NewManager(a.storageInstance, a.storageInstance, &metadataConfig, a.logger)

	if a.cfg.Backup.Metadata.Enabled {
		if err := a.metadataManager.Start(); err != nil {
			a.logger.Sugar().Warnf("Failed to start metadata manager: %v", err)
		} else {
			a.logger.Sugar().Info("Metadata manager started successfully")
		}
	} else if a.redisPool == nil {
		// standalone 模式（无 Redis + 未启用元数据备份）：
		// 仅用于表配置 MinIO 降级，不启动备份定时任务
		a.logger.Sugar().Info("Metadata manager created in standalone mode (table config MinIO fallback only, no backup scheduler)")
	}

	return nil
}

// InitMetrics 初始化指标服务器和运行时采集器
func (a *App) InitMetrics() error {
	// 初始化全局 RuntimeCollector（统一运行时采样）
	poolStatsProvider := func() (activeConns, maxConns int64) {
		minioPool := a.poolManager.GetMinIOPool()
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
		return int64(a.buffer.PendingWrites())
	}
	rc := metrics.InitGlobalRuntimeCollector(
		metrics.WithPoolStatsProvider(poolStatsProvider),
		metrics.WithBufferStatsProvider(bufferStatsProvider),
		metrics.WithThresholds(metrics.DefaultLoadThresholds()),
		metrics.WithInterval(5*time.Second),
	)
	rc.Start(a.ctx)
	a.logger.Sugar().Info("RuntimeCollector started")

	// 启动指标服务器（Dashboard 启用时由 dashboard 服务一并提供 /metrics）
	if a.cfg.Metrics.Enabled && !a.cfg.Dashboard.Enabled {
		a.metricsServer = a.startMetricsServer()
		a.logger.Sugar().Info("Metrics server started")
	}

	return nil
}

// InitBackgroundServices 初始化后台服务（Compaction、Subscription）
func (a *App) InitBackgroundServices() error {
	// 初始化并启动 Compaction Manager
	if a.cfg.Compaction.Enabled {
		minioPool := a.poolManager.GetMinIOPool()
		if minioPool != nil {
			compactionConfig := a.buildCompactionConfig()
			var err error
			a.compactionManager, err = compaction.NewManager(minioPool.GetClient(), a.cfg.GetMinIO().Bucket, compactionConfig, a.logger)
			if err != nil {
				a.logger.Sugar().Warnf("Failed to create compaction manager: %v", err)
			} else {
				a.compactionManager.Start(a.ctx)
				tieredStatus := "disabled"
				if compactionConfig.TieredConfig != nil && compactionConfig.TieredConfig.Enabled {
					tieredStatus = fmt.Sprintf("enabled (%d levels)", len(compactionConfig.TieredConfig.Levels))
				}
				a.logger.Sugar().Infof("Compaction manager started - check_interval: %v, target_file_size: %d, tiered: %s",
					a.cfg.Compaction.CheckInterval, a.cfg.Compaction.TargetFileSize, tieredStatus)
			}
		} else {
			a.logger.Sugar().Warn("Compaction manager disabled - MinIO pool not available")
		}
	}

	// 初始化数据订阅管理器
	if a.cfg.Subscription.Enabled {
		a.subscriptionMgr = subscription.NewManager(a.logger)

		// 初始化 Redis 订阅者
		if a.cfg.Subscription.Redis.Enabled && a.redisPool != nil {
			redisSubscriber, err := subscription.NewRedisSubscriber(a.redisPool, &a.cfg.Subscription.Redis, a.logger)
			if err != nil {
				a.logger.Sugar().Warnf("Failed to create Redis subscriber: %v", err)
			} else {
				if err := a.subscriptionMgr.RegisterSubscriber(redisSubscriber); err != nil {
					a.logger.Sugar().Warnf("Failed to register Redis subscriber: %v", err)
				}
			}
		}

		// 初始化 Kafka 订阅者
		if a.cfg.Subscription.Kafka.Enabled {
			kafkaSubscriber, err := subscription.NewKafkaSubscriber(&a.cfg.Subscription.Kafka, a.logger)
			if err != nil {
				a.logger.Sugar().Warnf("Failed to create Kafka subscriber: %v", err)
			} else {
				if err := a.subscriptionMgr.RegisterSubscriber(kafkaSubscriber); err != nil {
					a.logger.Sugar().Warnf("Failed to register Kafka subscriber: %v", err)
				}
			}
		}

		// 启动订阅管理器
		if a.subscriptionMgr.SubscriberCount() > 0 {
			if err := a.subscriptionMgr.Start(a.ctx); err != nil {
				a.logger.Sugar().Warnf("Failed to start subscription manager: %v", err)
			} else {
				a.logger.Sugar().Infof("Subscription manager started - subscriber_count: %d",
					a.subscriptionMgr.SubscriberCount())
			}
		}
	}

	return nil
}

// InitTransport 初始化传输层（必须在 InitServices 之后）
func (a *App) InitTransport() error {
	var err error

	// 启动 gRPC 服务器
	a.grpcServer, err = grpcTransport.NewServerWithMinIO(a.miniodbService, *a.cfg, a.primaryMinio, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create gRPC service: %w", err)
	}
	a.grpcServer.SetCoordinators(a.writeCoord, a.queryCoord)
	if a.metadataManager != nil {
		a.grpcServer.SetMetadataManager(a.metadataManager)
	}

	// 启动 REST 服务器
	a.restServer, err = restTransport.NewServerWithMinIO(a.miniodbService, a.cfg, a.primaryMinio, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create REST server: %w", err)
	}
	a.restServer.SetCoordinators(a.writeCoord, a.queryCoord)
	if a.metadataManager != nil {
		a.restServer.SetMetadataManager(a.metadataManager)
	}

	// 初始化 Dashboard 日志捕获
	if a.cfg.Dashboard.Enabled {
		a.logBuf = logbuffer.NewBuffer(5000)
		a.captureWriter = logger.NewCaptureWriter(
			logger.NewMultiWriteSyncerFromBase(),
			a.logBuf,
			nil, // hub 稍后设置
		)
		logger.SetCaptureWriter(a.captureWriter)
	}

	return nil
}

// InitReplication 初始化复制服务
func (a *App) InitReplication() error {
	backupPool := a.poolManager.GetBackupMinIOPool()
	if backupPool == nil {
		a.logger.Sugar().Info("Backup MinIO not configured, replication disabled")
		return nil
	}

	srcPool := a.poolManager.GetMinIOPool()
	dstPool := backupPool
	dstUploader, err := storage.NewMinioClientWrapperFromClient(dstPool.GetClient(), a.logger)
	if err != nil {
		a.logger.Sugar().Warnf("Failed to create dst uploader: %v, replication disabled", err)
		return nil
	}

	a.replicator, err = replication.NewReplicator(
		srcPool,
		dstUploader,
		&a.cfg.Backup.Replication,
		a.redisPool,
		a.logger,
		replication.WithReplicatorSnapshotProvider(metrics.GetRuntimeSnapshot),
	)
	if err != nil {
		a.logger.Sugar().Warnf("Failed to create replicator: %v, replication disabled", err)
		return nil
	}

	go a.replicator.Start(a.ctx)
	a.logger.Sugar().Info("Replicator started")
	return nil
}

// InitBackup 初始化备份子系统
func (a *App) InitBackup() error {
	// 冷备总开关：backup.enabled 控制
	if !a.cfg.Backup.Enabled {
		return nil
	}

	// backup.enabled=true：注入系统内置备份计划
	a.injectSystemPlans()

	backupTarget := backup.ResolveBackupTarget(a.cfg, a.poolManager, a.logger)
	if backupTarget == nil {
		a.logger.Sugar().Warn("Backup target not available, scheduler disabled")
		return nil
	}

	planStore := backup.NewRedisPlanStore(a.redisPool, a.logger)
	a.backupExecutor = backup.NewExecutor(a.primaryMinio, backupTarget, a.metadataManager, a.redisPool, planStore, a.cfg, a.logger)
	a.backupScheduler = backup.NewScheduler(a.backupExecutor, planStore, nil, a.logger) // hub 稍后设置

	for i := range a.cfg.Backup.Schedules {
		if a.cfg.Backup.Schedules[i].ID == "" {
			a.cfg.Backup.Schedules[i].ID = fmt.Sprintf("schedule-%d", i)
		}
		if err := a.backupScheduler.AddPlan(&a.cfg.Backup.Schedules[i]); err != nil {
			a.logger.Sugar().Warnf("Failed to add backup schedule %s: %v", a.cfg.Backup.Schedules[i].Name, err)
		}
	}

	if err := a.backupScheduler.Start(a.ctx); err != nil {
		a.logger.Sugar().Warnf("Failed to start backup scheduler: %v", err)
	} else {
		a.logger.Sugar().Infof("Backup scheduler started - plans: %d", len(a.cfg.Backup.Schedules))
	}

	return nil
}

// WireDashboard 配置 Dashboard（必须在 InitReplication + InitBackup 之后）
func (a *App) WireDashboard() {
	if !a.cfg.Dashboard.Enabled {
		return
	}

	var err error

	// 创建 Dashboard 服务器
	a.dashSrv, err = dashboard.NewServer(
		a.miniodbService,
		a.cfg,
		a.logger,
		a.restServer.AuthManager(),
		a.logBuf,
	)
	if err != nil {
		a.logger.Sugar().Fatalf("Failed to create dashboard server: %v", err)
	}

	// 设置日志捕获 hub
	if a.captureWriter != nil {
		a.captureWriter.SetHub(a.dashSrv.GetHub())
	}

	// 挂载 Dashboard 路由
	a.dashSrv.MountRoutes(a.restServer.Router().Group(a.cfg.Dashboard.BasePath))

	// 获取 SSE Hub（目前 backup scheduler 在创建时 hub 为 nil，保持原行为）
	_ = a.dashSrv.GetHub()

	// 设置 Dashboard 引用
	if a.replicator != nil {
		a.dashSrv.SetReplicator(a.replicator)
	}
	if a.backupScheduler != nil {
		a.dashSrv.SetBackupScheduler(a.backupScheduler)
	}
	if a.backupExecutor != nil {
		a.dashSrv.SetBackupStore(a.backupExecutor.GetPlanStore())
	}
	// 热切换分布式模式所需依赖
	a.dashSrv.SetPoolManager(a.poolManager)
	a.dashSrv.SetConfigPath(a.configPath)
	a.dashSrv.SetServiceRegistry(a.serviceRegistry)
	a.dashSrv.SetQueryCoordinator(a.queryCoord)

	go a.dashSrv.Start(a.ctx)
	a.logger.Sugar().Infof("Dashboard mounted at %s on port %s", a.cfg.Dashboard.BasePath, a.cfg.Server.RestPort)
}

// Start 启动所有服务
func (a *App) Start() {
	// 启动 gRPC
	go func() {
		if err := a.grpcServer.Start(a.cfg.Server.GrpcPort); err != nil {
			a.fatalCh <- fmt.Errorf("gRPC server failed: %w", err)
		}
	}()

	// 启动 REST
	go func() {
		if err := a.restServer.Start(a.cfg.Server.RestPort); err != nil {
			a.fatalCh <- fmt.Errorf("REST server failed: %w", err)
		}
	}()

	a.logger.Sugar().Info("MinIODB server started successfully")
}

// Shutdown 优雅关闭所有服务
func (a *App) Shutdown(shutdownCtx context.Context) {
	// 1. 首先取消 Context，通知所有 goroutine 停止
	a.logger.Sugar().Info("Canceling all contexts...")
	a.cancel()

	// 2. 停止接收新请求
	a.logger.Sugar().Info("Stopping gRPC server...")
	a.grpcServer.Stop()
	a.logger.Sugar().Info("gRPC server stopped")

	a.logger.Sugar().Info("Stopping REST server...")
	if err := a.restServer.Stop(shutdownCtx); err != nil {
		a.logger.Sugar().Errorf("Error shutting down REST server: %v", err)
	} else {
		a.logger.Sugar().Info("REST server stopped")
	}

	if a.dashSrv != nil {
		a.logger.Sugar().Info("Stopping Dashboard server...")
		a.dashSrv.Stop()
		a.logger.Sugar().Info("Dashboard server stopped")
	}

	if a.metricsServer != nil {
		if err := a.metricsServer.Shutdown(shutdownCtx); err != nil {
			a.logger.Sugar().Errorf("Error shutting down metrics server: %v", err)
		} else {
			a.logger.Sugar().Info("Metrics server stopped")
		}
	}

	// 3. 停止后台任务
	if a.replicator != nil {
		a.logger.Sugar().Info("Stopping replicator...")
		a.replicator.Stop()
		a.logger.Sugar().Info("Replicator stopped")
	}

	if a.backupScheduler != nil {
		a.logger.Sugar().Info("Stopping backup scheduler...")
		a.backupScheduler.Stop()
		a.logger.Sugar().Info("Backup scheduler stopped")
	}

	if a.subscriptionMgr != nil {
		a.logger.Sugar().Info("Stopping subscription manager...")
		if err := a.subscriptionMgr.Stop(shutdownCtx); err != nil {
			a.logger.Sugar().Errorf("Error stopping subscription manager: %v", err)
		} else {
			a.logger.Sugar().Info("Subscription manager stopped")
		}
	}

	if a.compactionManager != nil {
		a.logger.Sugar().Info("Stopping compaction manager...")
		a.compactionManager.Stop()
		a.logger.Sugar().Info("Compaction manager stopped")
	}

	if a.metadataManager != nil {
		a.logger.Sugar().Info("Stopping metadata manager...")
		if err := a.metadataManager.Stop(); err != nil {
			a.logger.Sugar().Errorf("Error stopping metadata manager: %v", err)
		} else {
			a.logger.Sugar().Info("Metadata manager stopped")
		}
	}

	a.logger.Sugar().Info("Shutdown complete")
}

// Close 清理资源
func (a *App) Close() {
	if a.serviceRegistry != nil {
		a.serviceRegistry.Stop()
	}
	if a.storageInstance != nil {
		a.storageInstance.Close()
	}
}

// FatalCh 返回致命错误通道
func (a *App) FatalCh() <-chan error {
	return a.fatalCh
}

// Context 返回应用上下文
func (a *App) Context() context.Context {
	return a.ctx
}

// Cancel 返回取消函数
func (a *App) Cancel() context.CancelFunc {
	return a.cancel
}

// buildCompactionConfig 构建 Compaction 配置
func (a *App) buildCompactionConfig() *compaction.Config {
	var tieredConfig *compaction.TieredCompactionConfig
	if a.cfg.Compaction.Tiered.Enabled {
		tieredConfig = &compaction.TieredCompactionConfig{
			Enabled:         a.cfg.Compaction.Tiered.Enabled,
			Levels:          make([]compaction.CompactionLevel, len(a.cfg.Compaction.Tiered.Levels)),
			MaxFilesToMerge: a.cfg.Compaction.Tiered.MaxFilesToMerge,
			CheckInterval:   a.cfg.Compaction.CheckInterval,
			TempDir:         a.cfg.Compaction.TempDir,
			CompressionType: a.cfg.Compaction.CompressionType,
			MaxRowsPerFile:  a.cfg.Compaction.MaxRowsPerFile,
		}
		for i, level := range a.cfg.Compaction.Tiered.Levels {
			tieredConfig.Levels[i] = compaction.CompactionLevel{
				Name:            level.Name,
				MaxFileSize:     level.MaxFileSize,
				TargetFileSize:  level.TargetFileSize,
				MinFilesToMerge: level.MinFilesToMerge,
				CooldownPeriod:  level.CooldownPeriod,
			}
		}
	}

	return &compaction.Config{
		TargetFileSize:    a.cfg.Compaction.TargetFileSize,
		MinFilesToCompact: a.cfg.Compaction.MinFilesToCompact,
		MaxFilesToCompact: a.cfg.Compaction.MaxFilesToCompact,
		CooldownPeriod:    a.cfg.Compaction.CooldownPeriod,
		CheckInterval:     a.cfg.Compaction.CheckInterval,
		TempDir:           a.cfg.Compaction.TempDir,
		CompressionType:   a.cfg.Compaction.CompressionType,
		MaxRowsPerFile:    a.cfg.Compaction.MaxRowsPerFile,
		TieredConfig:      tieredConfig,
	}
}

// injectSystemPlans 注入系统内置备份计划（backup.enabled=true 时调用）
func (a *App) injectSystemPlans() {
	hasMetadataPlan := false
	for _, s := range a.cfg.Backup.Schedules {
		if s.BackupType == "metadata" {
			hasMetadataPlan = true
			break
		}
	}

	// 自动注入元数据备份计划（默认开启）
	if !hasMetadataPlan && a.cfg.Backup.Metadata.Enabled {
		interval := a.cfg.Backup.Metadata.Interval
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		a.cfg.Backup.Schedules = append(a.cfg.Backup.Schedules, config.BackupSchedule{
			ID:            "system-metadata-backup",
			Name:          "System Metadata Backup",
			Enabled:       true,
			BackupType:    "metadata",
			Interval:      interval,
			RetentionDays: 7,
			System:        true,
		})
		a.logger.Sugar().Infof("Injected system metadata backup plan (interval: %v)", interval)
	}

	// 全量备份不自动规划，由用户通过 Dashboard 手动发起
}

// startMetricsServer 启动指标服务器
func (a *App) startMetricsServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", a.getMetricsHandler())
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", a.cfg.Metrics.Port),
		Handler: mux,
	}
	go func() {
		a.logger.Sugar().Infof("Starting metrics server on port %s", a.cfg.Metrics.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Sugar().Errorf("Metrics server error (non-fatal): %v", err)
		}
	}()
	return server
}

// getMetricsHandler 返回指标处理器
func (a *App) getMetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		var lines []string
		lines = append(lines, "# HELP miniodb_info MinIODB service information")
		lines = append(lines, "# TYPE miniodb_info gauge")
		lines = append(lines, fmt.Sprintf(`miniodb_info{version="%s",node_id="%s"} 1`, version.Get(), a.cfg.Server.NodeID))
		lines = append(lines, "# HELP miniodb_start_time_seconds MinIODB start time in unix timestamp")
		lines = append(lines, "# TYPE miniodb_start_time_seconds gauge")
		lines = append(lines, fmt.Sprintf("miniodb_start_time_seconds %d", time.Now().Unix()))
		for _, line := range lines {
			w.Write([]byte(line + "\n"))
		}
		a.logger.Sugar().Debugf("Metrics endpoint accessed - metric_lines: %d", len(lines))
	}
}
