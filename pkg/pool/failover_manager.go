package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"minIODB/internal/metrics"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// FailoverManager 故障切换管理器（简化版）
type FailoverManager struct {
	poolManager *PoolManager
	redisClient redis.UniversalClient

	// 状态管理（只有两个状态：主池/备份池）
	usingBackup     bool
	lastHealthCheck time.Time

	// 配置
	config FailoverConfig

	// 异步同步队列
	syncQueue chan *SyncTask
	workers   []*syncWorker

	mutex  sync.RWMutex
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// FailoverConfig 故障切换配置
type FailoverConfig struct {
	Enabled             bool            // 是否启用故障切换
	HealthCheckInterval time.Duration   // 健康检查间隔（默认15秒）
	AsyncSync           AsyncSyncConfig // 异步同步配置
}

// AsyncSyncConfig 异步同步配置
type AsyncSyncConfig struct {
	QueueSize     int           // 队列大小（默认1000）
	WorkerCount   int           // 并发工作器数量（默认3）
	RetryTimes    int           // 重试次数（默认3）
	RetryInterval time.Duration // 重试间隔（默认1秒）
	SyncTimeout   time.Duration // 同步超时（默认60秒）
}

// SyncTask 异步同步任务
type SyncTask struct {
	Bucket     string
	ObjectName string
	FilePath   string
	Timestamp  time.Time
}

// syncWorker 同步工作器
type syncWorker struct {
	id      int
	manager *FailoverManager
	logger  *zap.Logger
}

// RedisFailoverState Redis中的故障切换状态
type RedisFailoverState struct {
	UsingBackup bool      `json:"using_backup"`
	LastUpdate  time.Time `json:"last_update"`
	NodeID      string    `json:"node_id"`
}

// Redis键定义
const (
	FailoverStateKey = "pool:failover:state" // 当前状态
)

// DefaultFailoverConfig 返回默认配置
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		Enabled:             true,
		HealthCheckInterval: 15 * time.Second,
		AsyncSync: AsyncSyncConfig{
			QueueSize:     1000,
			WorkerCount:   3,
			RetryTimes:    3,
			RetryInterval: time.Second,
			SyncTimeout:   60 * time.Second,
		},
	}
}

// NewFailoverManager 创建故障切换管理器
func NewFailoverManager(poolManager *PoolManager, redisClient redis.UniversalClient, config FailoverConfig, logger *zap.Logger) *FailoverManager {
	if logger == nil {
		panic("logger is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	fm := &FailoverManager{
		poolManager: poolManager,
		redisClient: redisClient,
		config:      config,
		syncQueue:   make(chan *SyncTask, config.AsyncSync.QueueSize),
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
	}

	return fm
}

// Start 启动故障切换管理器
func (fm *FailoverManager) Start() error {
	fm.logger.Info("Starting failover manager",
		zap.Bool("enabled", fm.config.Enabled),
		zap.Duration("health_check_interval", fm.config.HealthCheckInterval))

	// 初始化metrics指标（触发注册）
	metrics.CurrentActivePool.Set(0)   // 0表示使用主池
	metrics.BackupSyncQueueSize.Set(0) // 初始队列为空
	metrics.PrimaryPoolHealthy.Set(1)  // 假设主池健康
	metrics.BackupPoolHealthy.Set(1)   // 假设备份池健康

	// 从Redis加载状态
	if err := fm.loadStateFromRedis(); err != nil {
		fm.logger.Warn("Failed to load state from Redis, using default", zap.Error(err))
	}

	// 启动健康检查循环
	fm.wg.Add(1)
	go fm.healthCheckLoop()

	// 启动异步同步工作器
	fm.workers = make([]*syncWorker, fm.config.AsyncSync.WorkerCount)
	for i := 0; i < fm.config.AsyncSync.WorkerCount; i++ {
		worker := &syncWorker{
			id:      i,
			manager: fm,
			logger:  fm.logger.With(zap.Int("worker_id", i)),
		}
		fm.workers[i] = worker
		fm.wg.Add(1)
		go worker.run()
	}

	fm.logger.Info("Failover manager started successfully",
		zap.Int("sync_workers", fm.config.AsyncSync.WorkerCount))

	return nil
}

// Stop 停止故障切换管理器
func (fm *FailoverManager) Stop() error {
	fm.logger.Info("Stopping failover manager")

	fm.cancel()
	fm.wg.Wait()

	close(fm.syncQueue)

	fm.logger.Info("Failover manager stopped")
	return nil
}

// GetActivePool 获取当前活跃的MinIO池
func (fm *FailoverManager) GetActivePool() *MinIOPool {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	if fm.usingBackup {
		return fm.poolManager.GetBackupMinIOPool()
	}
	return fm.poolManager.GetMinIOPool()
}

// IsUsingBackup 是否正在使用备份池
func (fm *FailoverManager) IsUsingBackup() bool {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()
	return fm.usingBackup
}

// Execute 执行MinIO操作（带自动故障切换）
func (fm *FailoverManager) Execute(ctx context.Context, operation func(*MinIOPool) error) error {
	// 1. 尝试当前活跃池
	pool := fm.GetActivePool()
	err := operation(pool)

	// 2. 如果操作成功，直接返回
	if err == nil {
		return nil
	}

	// 3. 如果失败且当前使用主池，尝试切换到备份池
	if !fm.IsUsingBackup() {
		fm.logger.Warn("Primary pool operation failed, trying to switch to backup pool",
			zap.Error(err))

		if fm.switchToBackup(ctx) {
			// 切换成功，在备份池重试
			backupPool := fm.GetActivePool()
			retryErr := operation(backupPool)
			if retryErr != nil {
				fm.logger.Error("Backup pool operation also failed", zap.Error(retryErr))
				return fmt.Errorf("primary failed: %w, backup also failed: %v", err, retryErr)
			}
			fm.logger.Info("Successfully executed on backup pool after primary failure")
			return nil
		}
	}

	// 4. 切换失败或已经在使用备份池，返回错误
	return err
}

// switchToBackup 切换到备份池
func (fm *FailoverManager) switchToBackup(ctx context.Context) bool {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	// 已经在使用备份池，无需切换
	if fm.usingBackup {
		return false
	}

	// 检查备份池是否可用
	backupPool := fm.poolManager.GetBackupMinIOPool()
	if backupPool == nil {
		fm.logger.Error("Cannot switch to backup pool: backup pool not configured")
		return false
	}

	// 健康检查
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := backupPool.HealthCheck(checkCtx); err != nil {
		fm.logger.Error("Cannot switch to backup pool: backup pool unhealthy", zap.Error(err))
		return false
	}

	// 切换到备份池
	fm.usingBackup = true
	fm.lastHealthCheck = time.Now()

	// 持久化状态到Redis
	if err := fm.saveStateToRedis(ctx); err != nil {
		fm.logger.Warn("Failed to save failover state to Redis", zap.Error(err))
		// 不回滚，继续使用备份池
	}

	fm.logger.Error("FAILOVER: Switched from primary pool to backup pool",
		zap.Time("failover_time", time.Now()))

	// 更新监控指标
	metrics.FailoverTotal.WithLabelValues("primary", "backup").Inc()
	metrics.CurrentActivePool.Set(1) // 1表示使用备份池

	return true
}

// healthCheckLoop 健康检查循环
func (fm *FailoverManager) healthCheckLoop() {
	defer fm.wg.Done()

	ticker := time.NewTicker(fm.config.HealthCheckInterval)
	defer ticker.Stop()

	fm.logger.Info("Health check loop started")

	for {
		select {
		case <-fm.ctx.Done():
			fm.logger.Info("Health check loop stopped")
			return
		case <-ticker.C:
			fm.checkAndRecover()
		}
	}
}

// checkAndRecover 检查主池健康并尝试恢复
func (fm *FailoverManager) checkAndRecover() {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	fm.lastHealthCheck = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 检查主池健康状态
	primaryPool := fm.poolManager.GetMinIOPool()
	if primaryPool != nil {
		if err := primaryPool.HealthCheck(ctx); err != nil {
			metrics.PrimaryPoolHealthy.Set(0)
		} else {
			metrics.PrimaryPoolHealthy.Set(1)
		}
	}

	// 检查备份池健康状态
	backupPool := fm.poolManager.GetBackupMinIOPool()
	if backupPool != nil {
		if err := backupPool.HealthCheck(ctx); err != nil {
			metrics.BackupPoolHealthy.Set(0)
		} else {
			metrics.BackupPoolHealthy.Set(1)
		}
	}

	// 只有在使用备份池时才检查主池恢复
	if !fm.usingBackup {
		return
	}

	if primaryPool == nil {
		return
	}

	if err := primaryPool.HealthCheck(ctx); err != nil {
		fm.logger.Debug("Primary pool still unhealthy", zap.Error(err))
		return
	}

	// 主池已恢复，切回主池
	fm.usingBackup = false

	// 持久化状态到Redis
	if err := fm.saveStateToRedis(ctx); err != nil {
		fm.logger.Warn("Failed to save recovery state to Redis", zap.Error(err))
	}

	fm.logger.Info("RECOVERY: Primary pool recovered, switched back from backup pool",
		zap.Time("recovery_time", time.Now()))

	// 更新监控指标
	metrics.FailoverTotal.WithLabelValues("backup", "primary").Inc()
	metrics.CurrentActivePool.Set(0) // 0表示使用主池
}

// EnqueueSync 将同步任务加入队列
func (fm *FailoverManager) EnqueueSync(bucket, objectName, filePath string) {
	task := &SyncTask{
		Bucket:     bucket,
		ObjectName: objectName,
		FilePath:   filePath,
		Timestamp:  time.Now(),
	}

	select {
	case fm.syncQueue <- task:
		// 成功加入队列
		metrics.BackupSyncQueueSize.Set(float64(len(fm.syncQueue)))
	default:
		// 队列满，记录警告
		fm.logger.Warn("Sync queue full, dropping task",
			zap.String("object", objectName),
			zap.Int("queue_size", len(fm.syncQueue)))
		metrics.BackupSyncDropped.Inc()
	}
}

// syncWorker.run 同步工作器运行
func (w *syncWorker) run() {
	defer w.manager.wg.Done()

	w.logger.Info("Sync worker started")

	for {
		select {
		case <-w.manager.ctx.Done():
			w.logger.Info("Sync worker stopped")
			return
		case task, ok := <-w.manager.syncQueue:
			if !ok {
				w.logger.Info("Sync queue closed, worker exiting")
				return
			}
			w.performSync(task)
		}
	}
}

// syncWorker.performSync 执行同步任务
func (w *syncWorker) performSync(task *SyncTask) {
	backupPool := w.manager.poolManager.GetBackupMinIOPool()
	if backupPool == nil {
		w.logger.Debug("Backup pool not available, skipping sync")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), w.manager.config.AsyncSync.SyncTimeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt <= w.manager.config.AsyncSync.RetryTimes; attempt++ {
		err := backupPool.ExecuteWithRetry(ctx, func() error {
			client := backupPool.GetClient()
			// 这里需要根据FilePath判断是FPutObject还是PutObject
			// 简化处理：假设总是使用FPutObject
			_, err := client.FPutObject(ctx, task.Bucket, task.ObjectName, task.FilePath, minio.PutObjectOptions{})
			return err
		})

		if err == nil {
			w.logger.Debug("Successfully synced object to backup pool",
				zap.String("object", task.ObjectName))
			metrics.BackupSyncSuccess.Inc()
			return
		}

		lastErr = err
		if attempt < w.manager.config.AsyncSync.RetryTimes {
			time.Sleep(w.manager.config.AsyncSync.RetryInterval)
		}
	}

	w.logger.Warn("Failed to sync object to backup pool after retries",
		zap.String("object", task.ObjectName),
		zap.Error(lastErr),
		zap.Int("attempts", w.manager.config.AsyncSync.RetryTimes+1))
	metrics.BackupSyncFailed.Inc()
}

// saveStateToRedis 保存状态到Redis
func (fm *FailoverManager) saveStateToRedis(ctx context.Context) error {
	if fm.redisClient == nil {
		return fmt.Errorf("redis client not available")
	}

	state := RedisFailoverState{
		UsingBackup: fm.usingBackup,
		LastUpdate:  time.Now(),
		NodeID:      "default", // TODO: 从配置中获取节点ID
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// 设置5分钟过期时间
	return fm.redisClient.Set(ctx, FailoverStateKey, data, 5*time.Minute).Err()
}

// loadStateFromRedis 从Redis加载状态
func (fm *FailoverManager) loadStateFromRedis() error {
	if fm.redisClient == nil {
		return fmt.Errorf("redis client not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := fm.redisClient.Get(ctx, FailoverStateKey).Result()
	if err == redis.Nil {
		// 没有历史状态，使用默认值（使用主池）
		fm.logger.Info("No previous failover state found in Redis, using primary pool")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get state from redis: %w", err)
	}

	var state RedisFailoverState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	fm.mutex.Lock()
	fm.usingBackup = state.UsingBackup
	fm.mutex.Unlock()

	fm.logger.Info("Loaded failover state from Redis",
		zap.Bool("using_backup", state.UsingBackup),
		zap.Time("last_update", state.LastUpdate),
		zap.String("node_id", state.NodeID))

	return nil
}

// GetStats 获取故障切换统计信息
func (fm *FailoverManager) GetStats() map[string]interface{} {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	return map[string]interface{}{
		"using_backup":          fm.usingBackup,
		"last_health_check":     fm.lastHealthCheck,
		"sync_queue_size":       len(fm.syncQueue),
		"sync_worker_count":     len(fm.workers),
		"health_check_interval": fm.config.HealthCheckInterval.String(),
	}
}
