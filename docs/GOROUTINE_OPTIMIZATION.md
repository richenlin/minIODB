# Goroutine 优化与 Context 传递机制设计

> 创建日期: 2026-01-18
> 状态: 待实施

## 概述

本文档描述 MinIODB 项目中的 goroutine 使用问题分析、Context 传递机制设计以及优雅关闭方案。

## 问题分析

### 一、当前问题

通过全面的代码审查，发现以下**高风险**goroutine问题：

| 位置 | 行号 | 问题描述 | 风险等级 |
|------|------|---------|---------|
| `cmd/main.go` | 411-431 | `startBackupRoutine` 无退出机制 | **高** |
| `internal/security/middleware.go` | 178-203 | RateLimiter清理goroutine无退出 | **高** |
| `internal/security/token_manager.go` | 123-137 | `cleanupLocalBlacklist` 无退出 | **高** |
| `internal/security/interceptor.go` | 361-371 | `startRateLimitCleanup` 无退出 | **高** |
| `internal/security/smart_rate_limiter.go` | 315-335 | `cleanup` 无退出 | **高** |
| `internal/monitoring/metrics.go` | 240-249 | `updateSystemMetrics` 无退出 | **高** |

### 二、问题示例

#### 示例1: startBackupRoutine (cmd/main.go:411-431)

**问题代码**:
```go
func startBackupRoutine(primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
    ticker := time.NewTicker(backupInterval)
    defer ticker.Stop()

    for range ticker.C {  // 🔴 永远运行，无法停止
        logger.Logger.Info("Starting scheduled backup...")
        if err := performDataBackup(primaryMinio, backupMinio, cfg); err != nil {
            logger.Logger.Error("Backup failed", zap.Error(err))
        } else {
            logger.Logger.Info("Backup completed successfully")
        }
    }
}
```

**问题**:
- 使用 `for range ticker.C` 循环，但没有监听任何退出信号
- 进程退出时goroutine仍在运行，导致goroutine泄漏
- 无法优雅关闭，备份任务可能在中途被强制终止

#### 示例2: cleanupLocalBlacklist (internal/security/token_manager.go:123-137)

**问题代码**:
```go
func (tm *TokenManager) cleanupLocalBlacklist() {
    ticker := time.NewTicker(time.Hour)
    defer ticker.Stop()

    for range ticker.C {  // 🔴 永远运行
        tm.mutex.Lock()
        now := time.Now()
        for token, expiry := range tm.localBlacklist {
            if now.After(expiry) {
                delete(tm.localBlacklist, token)
            }
        }
        tm.mutex.Unlock()
    }
}
```

**问题**:
- 清理goroutine无法停止
- 没有Context传递，无法感知应用关闭
- TokenManager实例销毁时goroutine仍存在

#### 示例3: SmartRateLimiter.cleanup (internal/security/smart_rate_limiter.go:315-335)

**问题代码**:
```go
func (srl *SmartRateLimiter) cleanup() {
    ticker := time.NewTicker(srl.config.CleanupInterval)
    defer ticker.Stop()

    for range ticker.C {  // 🔴 永远运行
        srl.mutex.Lock()
        now := time.Now()
        for key, client := range srl.clients {
            // ... 清理逻辑
        }
        srl.mutex.Unlock()
    }
}
```

**问题**:
- 限流清理goroutine无退出机制
- 长时间运行的服务会产生多个无法停止的goroutine

### 三、设计良好的模式参考

#### 模式1: WaitGroup + Channel 退出

**文件**: `internal/buffer/concurrent_buffer.go`

```go
type ConcurrentBuffer struct {
    shutdown   chan struct{}  // 退出信号
    workerWg   sync.WaitGroup // 等待worker完成
    // ...
}

func (cb *ConcurrentBuffer) periodicFlush() {
    ticker := time.NewTicker(cb.flushInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            cb.flush()
        case <-cb.shutdown:  // ✅ 监听退出信号
            logger.Logger.Info("Periodic flush stopping")
            return
        }
    }
}

func (cb *ConcurrentBuffer) Stop() {
    close(cb.shutdown)
    cb.workerWg.Wait()  // ✅ 等待所有worker完成
}
```

#### 模式2: Context 取消

**文件**: `internal/coordinator/coordinator.go`

```go
type QueryCoordinator struct {
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
    // ...
}

func NewQueryCoordinator(...) *QueryCoordinator {
    ctx, cancel := context.WithCancel(context.Background())
    qc := &QueryCoordinator{
        ctx:    ctx,
        cancel: cancel,
    }

    qc.wg.Add(1)
    go qc.monitorNodes()

    return qc
}

func (qc *QueryCoordinator) monitorNodes() {
    defer qc.wg.Done()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            qc.checkNodes()
        case <-qc.ctx.Done():  // ✅ 监听Context取消
            logger.Logger.Info("Node monitor stopped")
            return
        }
    }
}

func (qc *QueryCoordinator) Stop() {
    qc.cancel()  // ✅ 取消Context
    qc.wg.Wait() // ✅ 等待完成
}
```

#### 模式3: 双重退出检查

**文件**: `internal/compaction/manager.go`

```go
type Manager struct {
    ctx    context.Context
    cancel context.CancelFunc
    stopCh chan struct{}
    wg     sync.WaitGroup
    // ...
}

func (m *Manager) runCompactionLoop() {
    defer m.wg.Done()

    ticker := time.NewTicker(m.config.CheckInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // 执行压缩
        case <-m.ctx.Done():  // ✅ Context取消
            return
        case <-m.stopCh:     // ✅ stopCh信号
            return
        }
    }
}

func (m *Manager) Stop() {
    m.cancel()        // 取消Context
    close(m.stopCh)   // 关闭stopCh
    m.wg.Wait()      // 等待完成
}
```

## 设计方案

### 一、核心设计原则

1. **统一的 Context 传递**
   - 从 main 函数创建根 Context
   - 向下传递给所有子系统
   - 通过 Context 取消实现优雅关闭

2. **双重退出机制**
   - 使用 `Context + stopCh` 双重检查
   - 提高可靠性，避免goroutine泄漏

3. **WaitGroup 等待完成**
   - 使用 `sync.WaitGroup` 跟踪所有goroutine
   - 确保所有goroutine在关闭前完成

4. **向后兼容**
   - 保留原有构造函数
   - 添加带Context的新构造函数
   - 逐步迁移，不破坏现有代码

### 二、修改范围

**需要修改的文件**：

| 文件 | 修改内容 | 优先级 |
|------|---------|--------|
| `cmd/main.go` | 创建根Context和WaitGroup，传递到所有子系统 | 高 |
| `internal/security/token_manager.go` | 添加stopCh、Stop方法、新构造函数 | 高 |
| `internal/security/smart_rate_limiter.go` | 添加stopCh、Stop方法、新构造函数 | 高 |
| `internal/security/interceptor.go` | 添加stopCh、Stop方法 | 高 |
| `internal/monitoring/metrics.go` | 添加stopCh、Stop方法 | 高 |
| `internal/security/middleware.go` | 重构RateLimiter支持取消 | 高 |

### 三、详细设计

#### 3.1 cmd/main.go 改造

**改造要点**:
1. 在 main 函数开始时创建根 Context
2. 创建全局 WaitGroup 用于跟踪所有goroutine
3. 传递 Context 到所有需要它的组件
4. 在 waitForShutdown 中先 cancel 再等待 WaitGroup

**代码改造**:

```go
// cmd/main.go

import (
    "context"
    "sync"  // ✅ 新增
    // ... 其他导入
)

func main() {
    // 1. 创建根Context（可取消）
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 2. 创建全局WaitGroup用于等待所有goroutine
    var wg sync.WaitGroup

    // 3. 解析配置
    cfg, err := config.LoadConfig(configPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
        os.Exit(1)
    }

    // 4. 初始化日志
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

    // 5. 初始化连接池（传递Context）
    redisPool, err := pool.NewRedisPoolWithCtx(ctx, &cfg.Network.Pools.Redis)
    if err != nil {
        logger.Logger.Fatal("Failed to create Redis pool", zap.Error(err))
    }

    primaryMinio, err := pool.NewMinIOPoolWithCtx(ctx, &cfg.Network.Pools.MinIO)
    if err != nil {
        logger.Logger.Fatal("Failed to create MinIO pool", zap.Error(err))
    }

    // 6. 初始化认证管理器（传递Context）
    authManager := security.NewAuthManager(ctx, cfg.Auth)

    // 7. 初始化TokenManager（传递Context）
    tokenManager := security.NewTokenManagerWithContext(ctx, redisPool.Client())
    if cfg.Auth.EnableJWT {
        authManager.SetTokenManager(tokenManager)
    }

    // 8. 初始化限流器（传递Context）
    rateLimiter := security.NewSmartRateLimiterWithContext(ctx, cfg.RateLimiting)

    // 9. 创建服务实例
    miniodbService, err := service.NewMinIODBService(ctx, primaryMinio, redisPool, cfg)
    if err != nil {
        logger.Logger.Fatal("Failed to create MinIODB service", zap.Error(err))
    }

    // ... 其他组件初始化 ...

    // 10. 启动备份goroutine（传递Context）
    if cfg.Backup.Enabled && backupMinio != nil {
        wg.Add(1)
        go func() {
            defer wg.Done()
            startBackupRoutine(ctx, primaryMinio, backupMinio, *cfg)
        }()
    }

    logger.Logger.Info("MinIODB server started successfully")

    // 11. 等待中断信号（传递WaitGroup）
    waitForShutdown(ctx, cancel, &wg, grpcService, restServer, metricsServer,
        systemMonitor, metadataManager, redisPool, compactionManager, subscriptionManager,
        tokenManager, rateLimiter)

    logger.Logger.Info("MinIODB server stopped")
}

// ✅ 改造后的备份例程
func startBackupRoutine(ctx context.Context, primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
    backupInterval := time.Duration(cfg.Backup.Interval) * time.Hour
    if backupInterval == 0 {
        backupInterval = 24 * time.Hour
    }

    ticker := time.NewTicker(backupInterval)
    defer ticker.Stop()

    logger.Logger.Info("Backup routine started", zap.Duration("interval", backupInterval))

    for {
        select {
        case <-ticker.C:
            logger.Logger.Info("Starting scheduled backup...")
            if err := performDataBackup(primaryMinio, backupMinio, cfg); err != nil {
                logger.Logger.Error("Backup failed", zap.Error(err))
            } else {
                logger.Logger.Info("Backup completed successfully")
            }
        case <-ctx.Done():  // ✅ 监听Context取消
            logger.Logger.Info("Backup routine stopped gracefully")
            return
        }
    }
}

// ✅ 改造后的优雅关闭
func waitForShutdown(
    ctx context.Context,
    cancel context.CancelFunc,
    wg *sync.WaitGroup,  // ✅ 新增参数
    grpcService *grpcTransport.Server,
    restServer *restTransport.Server,
    metricsServer *http.Server,
    systemMonitor *metrics.SystemMonitor,
    metadataManager *metadata.Manager,
    redisPool *pool.RedisPool,
    compactionManager *compaction.Manager,
    subscriptionManager *subscription.Manager,
    tokenManager *security.TokenManager,     // ✅ 新增
    rateLimiter *security.SmartRateLimiter,  // ✅ 新增
) {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    <-sigChan
    logger.Logger.Info("Received shutdown signal")

    // 1. 首先取消Context，通知所有goroutine停止
    logger.Logger.Info("Canceling all contexts...")
    cancel()

    // 2. 停止接收新请求（快速关闭）
    logger.Logger.Info("Stopping gRPC server...")
    grpcService.Stop()
    logger.Logger.Info("gRPC server stopped")

    logger.Logger.Info("Stopping REST server...")
    ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
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

    // 3. 停止后台任务
    if tokenManager != nil {
        logger.Logger.Info("Stopping token manager...")
        tokenManager.Stop()  // ✅ 调用Stop方法
        logger.Logger.Info("Token manager stopped")
    }

    if rateLimiter != nil {
        logger.Logger.Info("Stopping rate limiter...")
        rateLimiter.Stop()  // ✅ 调用Stop方法
        logger.Logger.Info("Rate limiter stopped")
    }

    if subscriptionManager != nil {
        logger.Logger.Info("Stopping subscription manager...")
        if err := subscriptionManager.Stop(ctx); err != nil {
            logger.Logger.Error("Error stopping subscription manager", zap.Error(err))
        } else {
            logger.Logger.Info("Subscription manager stopped")
        }
    }

    if compactionManager != nil {
        logger.Logger.Info("Stopping compaction manager...")
        compactionManager.Stop()
        logger.Logger.Info("Compaction manager stopped")
    }

    if metadataManager != nil {
        logger.Logger.Info("Stopping metadata manager...")
        if err := metadataManager.Stop(ctx); err != nil {
            logger.Logger.Error("Error stopping metadata manager", zap.Error(err))
        } else {
            logger.Logger.Info("Metadata manager stopped")
        }
    }

    // 4. 关闭连接池
    logger.Logger.Info("Closing Redis pool...")
    redisPool.Close()
    logger.Logger.Info("Redis pool closed")

    // 5. 等待所有goroutine完成（带超时）
    logger.Logger.Info("Waiting for background tasks to complete...")
    done := make(chan struct{})
    go func() {
        wg.Wait()  // ✅ 等待所有goroutine
        close(done)
    }()

    select {
    case <-done:
        logger.Logger.Info("All goroutines stopped gracefully")
    case <-time.After(30 * time.Second):
        logger.Logger.Warn("Timeout waiting for goroutines, forcing shutdown")
    }

    logger.Logger.Info("Shutdown complete")
    os.Exit(0)
}
```

#### 3.2 TokenManager 改造

**改造要点**:
1. 添加 `stopCh chan struct{}` 字段
2. 添加 `wg sync.WaitGroup` 字段
3. 创建新构造函数 `NewTokenManagerWithContext`
4. 修改 `cleanupLocalBlacklist` 支持退出
5. 添加 `Stop()` 方法

**代码改造**:

```go
// internal/security/token_manager.go

type TokenManager struct {
    mutex            sync.RWMutex
    redisClient      *redis.Client
    localBlacklist   map[string]time.Time
    localRefreshMap  map[string]string
    useRedis         bool
    stopCh           chan struct{}  // ✅ 新增
    wg               sync.WaitGroup // ✅ 新增
}

// ✅ 保留原有构造函数（向后兼容）
func NewTokenManager(redisClient *redis.Client) *TokenManager {
    return NewTokenManagerWithContext(context.Background(), redisClient)
}

// ✅ 新增带Context的构造函数
func NewTokenManagerWithContext(ctx context.Context, redisClient *redis.Client) *TokenManager {
    tm := &TokenManager{
        redisClient:     redisClient,
        localBlacklist:  make(map[string]time.Time),
        localRefreshMap: make(map[string]string),
        useRedis:        redisClient != nil,
        stopCh:          make(chan struct{}),
    }

    if !tm.useRedis {
        tm.wg.Add(1)
        go tm.cleanupLocalBlacklist()
    }

    // ✅ 监听Context取消
    go func() {
        <-ctx.Done()
        logger.GetLogger().Sugar().Info("TokenManager received context cancellation, stopping...")
        tm.Stop()
    }()

    return tm
}

// ✅ 改造后的清理例程
func (tm *TokenManager) cleanupLocalBlacklist() {
    defer tm.wg.Done()

    ticker := time.NewTicker(time.Hour)
    defer ticker.Stop()

    logger.GetLogger().Sugar().Info("Token blacklist cleanup routine started")

    for {
        select {
        case <-ticker.C:
            tm.mutex.Lock()
            now := time.Now()
            cleaned := 0
            for token, expiry := range tm.localBlacklist {
                if now.After(expiry) {
                    delete(tm.localBlacklist, token)
                    cleaned++
                }
            }
            tm.mutex.Unlock()

            if cleaned > 0 {
                logger.GetLogger().Sugar().Debug("Cleaned up expired tokens",
                    zap.Int("count", cleaned))
            }

        case <-tm.stopCh:  // ✅ 监听停止信号
            logger.GetLogger().Sugar().Info("Token blacklist cleanup routine stopped")
            return
        }
    }
}

// ✅ 新增Stop方法
func (tm *TokenManager) Stop() {
    logger.GetLogger().Sugar().Info("Stopping TokenManager...")

    // 关闭stopCh通知goroutine退出
    close(tm.stopCh)

    // 等待所有goroutine完成
    tm.wg.Wait()

    logger.GetLogger().Sugar().Info("TokenManager stopped")
}

// ✅ 其他方法保持不变...
```

#### 3.3 SmartRateLimiter 改造

**代码改造**:

```go
// internal/security/smart_rate_limiter.go

type SmartRateLimiter struct {
    config  SmartRateLimitConfig
    tiers   map[string]RateLimitingTier
    pathRules []RateLimitingPathRule
    clients map[string]*clientData
    mutex   sync.RWMutex
    stopCh  chan struct{}  // ✅ 新增
    wg      sync.WaitGroup // ✅ 新增
}

// ✅ 保留原有构造函数
func NewSmartRateLimiter(config SmartRateLimitConfig) *SmartRateLimiter {
    return NewSmartRateLimiterWithContext(context.Background(), config)
}

// ✅ 新增带Context的构造函数
func NewSmartRateLimiterWithContext(ctx context.Context, config SmartRateLimitConfig) *SmartRateLimiter {
    srl := &SmartRateLimiter{
        config:     config,
        tiers:      config.Tiers,
        pathRules:  config.PathRules,
        clients:    make(map[string]*clientData),
        stopCh:     make(chan struct{}),
    }

    srl.wg.Add(1)
    go srl.cleanup()

    // ✅ 监听Context取消
    go func() {
        <-ctx.Done()
        logger.GetLogger().Sugar().Info("SmartRateLimiter received context cancellation, stopping...")
        srl.Stop()
    }()

    return srl
}

// ✅ 改造后的清理例程
func (srl *SmartRateLimiter) cleanup() {
    defer srl.wg.Done()

    ticker := time.NewTicker(srl.config.CleanupInterval)
    defer ticker.Stop()

    logger.GetLogger().Sugar().Info("SmartRateLimiter cleanup routine started")

    for {
        select {
        case <-ticker.C:
            srl.mutex.Lock()
            now := time.Now()
            cleaned := 0
            for key, client := range srl.clients {
                client.mutex.RLock()
                // 如果客户端长时间没有活动，删除它
                inactive := now.Sub(client.lastViolation) > 2*srl.config.CleanupInterval &&
                    now.After(client.backoffUntil)
                client.mutex.RUnlock()

                if inactive {
                    delete(srl.clients, key)
                    cleaned++
                }
            }
            srl.mutex.Unlock()

            if cleaned > 0 {
                logger.GetLogger().Sugar().Debug("Cleaned up inactive rate limit clients",
                    zap.Int("count", cleaned))
            }

        case <-srl.stopCh:  // ✅ 监听停止信号
            logger.GetLogger().Sugar().Info("SmartRateLimiter cleanup routine stopped")
            return
        }
    }
}

// ✅ 新增Stop方法
func (srl *SmartRateLimiter) Stop() {
    logger.GetLogger().Sugar().Info("Stopping SmartRateLimiter...")

    close(srl.stopCh)
    srl.wg.Wait()

    logger.GetLogger().Sugar().Info("SmartRateLimiter stopped")
}
```

#### 3.4 GRPCInterceptor 改造

**代码改造**:

```go
// internal/security/interceptor.go

type GRPCInterceptor struct {
    rateLimitClients map[string]*clientData
    rateLimitMutex   sync.RWMutex
    stopCh           chan struct{}  // ✅ 新增
    wg               sync.WaitGroup // ✅ 新增
}

// ✅ 构造函数中启动清理goroutine
func NewGRPCInterceptor(config SecurityConfig) *GRPCInterceptor {
    gi := &GRPCInterceptor{
        rateLimitClients: make(map[string]*clientData),
        stopCh:           make(chan struct{}),
    }

    // ✅ 启动清理goroutine
    gi.wg.Add(1)
    go gi.startRateLimitCleanup()

    return gi
}

// ✅ 改造后的清理例程
func (gi *GRPCInterceptor) startRateLimitCleanup() {
    defer gi.wg.Done()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    logger.GetLogger().Sugar().Info("gRPC rate limit cleanup routine started")

    for {
        select {
        case <-ticker.C:
            gi.cleanupRateLimitData()

        case <-gi.stopCh:  // ✅ 监听停止信号
            logger.GetLogger().Sugar().Info("gRPC rate limit cleanup routine stopped")
            return
        }
    }
}

// ✅ 新增Stop方法
func (gi *GRPCInterceptor) Stop() {
    logger.GetLogger().Sugar().Info("Stopping GRPCInterceptor...")

    close(gi.stopCh)
    gi.wg.Wait()

    logger.GetLogger().Sugar().Info("GRPCInterceptor stopped")
}
```

#### 3.5 MetricsCollector 改造

**代码改造**:

```go
// internal/monitoring/metrics.go

type MetricsCollector struct {
    startTime  time.Time
    stopCh     chan struct{}  // ✅ 新增
    wg         sync.WaitGroup // ✅ 新增
}

// ✅ 构造函数中启动goroutine
func NewMetricsCollector(...) *MetricsCollector {
    collector := &MetricsCollector{
        startTime: time.Now(),
        stopCh:    make(chan struct{}),
    }

    // ✅ 启动系统指标更新
    collector.wg.Add(1)
    go collector.updateSystemMetrics()

    return collector
}

// ✅ 改造后的指标更新例程
func (mc *MetricsCollector) updateSystemMetrics() {
    defer mc.wg.Done()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    logger.GetLogger().Sugar().Info("System metrics update routine started")

    for {
        select {
        case <-ticker.C:
            systemUptime.Set(time.Since(mc.startTime).Seconds())

        case <-mc.stopCh:  // ✅ 监听停止信号
            logger.GetLogger().Sugar().Info("System metrics update routine stopped")
            return
        }
    }
}

// ✅ 新增Stop方法
func (mc *MetricsCollector) Stop() {
    logger.GetLogger().Sugar().Info("Stopping MetricsCollector...")

    close(mc.stopCh)
    mc.wg.Wait()

    logger.GetLogger().Sugar().Info("MetricsCollector stopped")
}
```

#### 3.6 Middleware RateLimiter 改造

**代码改造**:

```go
// internal/security/middleware.go

// ✅ 创建可停止的RateLimiter
func RateLimiter(requestsPerMinute int) gin.HandlerFunc {
    stopCh := make(chan struct{})
    var wg sync.WaitGroup

    type clientData struct {
        requests []time.Time
        mutex    sync.Mutex
    }

    clients := make(map[string]*clientData)
    var globalMutex sync.RWMutex

    // ✅ 清理goroutine，每30秒清理一次过期数据
    wg.Add(1)
    go func() {
        defer wg.Done()

        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        logger.GetLogger().Sugar().Info("Rate limiter cleanup routine started")

        for {
            select {
            case <-ticker.C:
                globalMutex.Lock()
                now := time.Now()
                for ip, data := range clients {
                    data.mutex.Lock()
                    var validRequests []time.Time
                    for _, reqTime := range data.requests {
                        if now.Sub(reqTime) < time.Minute {
                            validRequests = append(validRequests, reqTime)
                        }
                    }
                    if len(validRequests) == 0 {
                        delete(clients, ip)
                    } else {
                        data.requests = validRequests
                    }
                    data.mutex.Unlock()
                }
                globalMutex.Unlock()

            case <-stopCh:  // ✅ 监听停止信号
                logger.GetLogger().Sugar().Info("Rate limiter cleanup routine stopped")
                return
            }
        }
    }()

    // ✅ 返回中间件和停止函数
    // 注意：由于这是一个中间件工厂函数，我们需要返回一个停止函数
    // 但这需要修改调用方代码，这里仅展示思路
    return func(c *gin.Context) {
        // ... 中间件逻辑保持不变 ...
    }
}
```

### 四、实施计划

#### 任务清单

| ID | 任务 | 文件 | 优先级 | 预估时间 |
|----|------|------|--------|---------|
| T1 | 改造 `cmd/main.go` - 创建根Context和WaitGroup | cmd/main.go | 高 | 30分钟 |
| T2 | 改造 `startBackupRoutine` 支持Context取消 | cmd/main.go | 高 | 15分钟 |
| T3 | 改造 `TokenManager` 添加stopCh和Stop方法 | internal/security/token_manager.go | 高 | 30分钟 |
| T4 | 改造 `SmartRateLimiter` 添加stopCh和Stop方法 | internal/security/smart_rate_limiter.go | 高 | 30分钟 |
| T5 | 改造 `GRPCInterceptor` 添加stopCh和Stop方法 | internal/security/interceptor.go | 高 | 20分钟 |
| T6 | 改造 `middleware.go` RateLimiter支持取消 | internal/security/middleware.go | 高 | 30分钟 |
| T7 | 改造 `MetricsCollector` 添加stopCh和Stop方法 | internal/monitoring/metrics.go | 高 | 20分钟 |
| T8 | 重构 `waitForShutdown` 实现优雅关闭 | cmd/main.go | 高 | 30分钟 |
| T9 | 测试优雅关闭流程 | - | 中 | 30分钟 |
| T10 | 更新progress.txt和feature_list.json | - | 中 | 10分钟 |

**总预估时间**: 约 4 小时

#### 实施步骤

1. **准备阶段** (T1)
   - 在 main 函数开始时创建根 Context 和 WaitGroup
   - 修改waitForShutdown签名，添加WaitGroup参数

2. **核心组件改造** (T2-T5)
   - 改造 TokenManager（最优先，因为它不依赖其他组件）
   - 改造 SmartRateLimiter
   - 改造 GRPCInterceptor
   - 改造 MetricsCollector

3. **中间件改造** (T6)
   - 重构 RateLimiter，使其可停止

4. **集成与测试** (T7-T8)
   - 在 main.go 中传递 Context 到各组件
   - 实现完整的优雅关闭流程
   - 使用 Stop() 方法停止各组件

5. **验证与文档** (T9-T10)
   - 发送 SIGTERM 信号测试优雅关闭
   - 验证所有goroutine都能正常退出
   - 更新文档和进度记录

### 五、测试方案

#### 测试1: 正常启动

```bash
# 启动服务
./miniodb

# 验证日志中看到所有goroutine启动
# Token blacklist cleanup routine started
# SmartRateLimiter cleanup routine started
# gRPC rate limit cleanup routine started
# System metrics update routine started
# Backup routine started
```

#### 测试2: 优雅关闭

```bash
# 启动服务
./miniodb

# 在另一个终端发送SIGTERM信号
kill -TERM $(pgrep miniodb)

# 验证日志中的优雅关闭流程
# Received shutdown signal
# Canceling all contexts...
# Stopping gRPC server...
# gRPC server stopped
# Stopping REST server...
# REST server stopped
# Stopping token manager...
# Token manager stopped
# Stopping rate limiter...
# Rate limiter stopped
# Waiting for background tasks to complete...
# All goroutines stopped gracefully
# Shutdown complete
```

#### 测试3: 强制关闭

```bash
# 启动服务
./miniodb

# 发送SIGKILL信号
kill -KILL $(pgrep miniodb)

# 应该能看到goroutine泄漏警告
# Timeout waiting for goroutines, forcing shutdown
```

#### 测试4: 并发测试

```bash
# 启动服务
./miniodb

# 使用wrk进行并发请求
wrk -t12 -c400 -d30s http://localhost:8081/v1/health

# 在测试期间发送SIGTERM
# 验证优雅关闭不会丢失正在处理的请求
```

### 六、注意事项

#### 1. 向后兼容

- 保留原有构造函数 `NewXXX()`
- 添加带Context的新构造函数 `NewXXXWithContext()`
- 逐步迁移，不破坏现有代码

#### 2. 超时控制

- 优雅关闭时设置最大等待时间（30秒）
- 超时后强制退出，防止无限等待

#### 3. 日志记录

- 在关键位置添加日志
- 使用不同的日志级别（Info, Debug, Warn）
- 记录goroutine的启动和停止

#### 4. 错误处理

- Stop() 方法不返回错误，使用日志记录
- 所有goroutine的退出都应该有日志

#### 5. 测试覆盖

- 单元测试：测试 Stop() 方法
- 集成测试：测试完整的优雅关闭流程
- 压力测试：验证并发场景下的关闭

### 七、预期收益

1. **防止goroutine泄漏**
   - 所有goroutine都可以被优雅停止
   - 进程退出时不会有残留goroutine

2. **优雅关闭**
   - 所有正在处理的请求可以完成
   - 资源可以正确释放
   - 数据一致性得到保证

3. **提高可靠性**
   - 使用Context传递取消信号
   - 双重退出机制提高容错性
   - WaitGroup确保所有goroutine完成

4. **更好的可维护性**
   - 统一的Context传递机制
   - 清晰的启动/停止流程
   - 易于扩展新组件

### 八、参考资源

- [Go Context 最佳实践](https://go.dev/blog/context)
- [优雅关闭模式](https://github.com/golang/go/wiki/Graceful-shutdown)
- [sync.WaitGroup 文档](https://pkg.go.dev/sync#WaitGroup)
- [并发模式](https://github.com/golang/go/wiki/LockRace)

---

**文档版本**: v1.0
**最后更新**: 2026-01-18
**维护者**: 开发团队
