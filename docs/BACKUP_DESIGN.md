# MinIODB 备份体系重构方案

**日期**：2026-03-19  
**版本**：v2.1  
**范围**：热备（主从复制）、冷备（备份计划）、Backup 节点缺失降级、**Standalone 模式元数据降级**  
**核心约束**：热备不得对主进程（数据库服务）产生可感知的性能影响  
**设计原则**：最大化复用项目现有模块，仅在必要处引入隔离

---

## 一、现状分析

### 1.1 热备（主从复制）

**位置**：`cmd/main.go:476-506`（`startBackupRoutine` + `performDataBackup`）

| 维度 | 现状 |
|------|------|
| 触发方式 | `time.Ticker` 按 `cfg.Backup.Interval`（秒）定时触发 |
| 复制策略 | 全量遍历 `primaryMinio.ListObjects` → 逐个 `CopyObject` 到 backupMinio |
| 增量支持 | 无 — 每次都复制所有对象，不区分新增/修改/未变更 |
| 并发控制 | 串行复制，无并发 worker |
| 进度追踪 | Redis `BackupSyncState` / `BackupProgress`，但无断点续传 |
| 错误处理 | 整体重试 3 次（指数退避），单对象失败只记 error 不中断 |
| 前置条件 | `cfg.Backup.Enabled && backupMinio != nil` 才启动 goroutine |
| 客户端 | 直接使用 `primaryMinio`（与主业务共享 `minio.Client` + `http.Transport`） |

**核心问题**：
1. 每次全量复制，数据量增长后耗时线性增长，带宽浪费
2. 与主业务共用 `minio.Client` / `http.Transport` — ListObjects + CopyObject 抢占主路径连接
3. 无状态水位线 — 重启后无法做增量

### 1.2 冷备（Dashboard 备份功能）

**位置**：`internal/dashboard/server.go`（备份 handler） + `internal/metadata/`（元数据备份核心）

| 维度 | 现状 |
|------|------|
| 元数据备份 | 成熟 — `metadata.Manager` 支持定时/手动/增量/全量/加密，备份到 MinIO |
| 全量数据备份 | **未实现** — `triggerFullBackup` 实际调用 `BackupMetadata` |
| 表级数据备份 | **未实现** — `triggerTableBackup` 同上 |
| 备份计划 | 单一计划 — 只有全局 `backup.interval` + `backup.metadata.interval` |
| 备份目标 | 强依赖 BackupMinIO — `isBackupAvailable()` 不可用时所有 API 返回 503 |
| 前端 UI | 完整 — 列表、触发、恢复、下载、验证、删除 |

### 1.3 Standalone 模式（无 Redis）现状

当系统以 standalone 模式运行（`redisPool == nil`）时，涉及元数据的全部存储路径均存在严重缺陷：

| 位置 | 无 Redis 时的行为 | 风险等级 |
|------|-----------------|---------|
| `metadata.Manager.GetTableConfig/SaveTableConfig` | 调用 `getRedisClient()` 失败，直接返回错误，**无任何降级** | **高** — 表配置无法读写，每次写入都使用系统默认值，自定义配置（id_strategy 等）全部丢失 |
| `metadata.Manager.performStartupSync` | 获取分布式锁失败，直接 return，跳过整个启动同步 | 中 |
| `metadata.BackupManager.Start()` | 备份失败被吞掉，`Start()` 返回 nil，**误以为备份正常运行** | 中（静默失败）|
| `service.MinIODBService.BackupMetadata()` | `metadataMgr == nil` 时 **nil pointer dereference panic** | **致命** |
| `service.MinIODBService.RestoreMetadata()` | 同上，**panic** | **致命** |
| `service.MinIODBService.ListBackups()` | 同上，**panic** | **致命** |
| `service.MinIODBService.GetMetadataStatus()` | 同上，**panic** | **致命** |
| `TableManager.getTableConfigFromRedis` | 返回 `cfg.Tables.DefaultConfig`，**表的自定义配置丢失** | 高 |
| `TableManager.TableExists` | 始终返回 `false`，**每次请求都触发"自动创建"逻辑** | 高 |
| `TableManager.ListTables` | 返回空列表 | 高 |
| `TableManager.CreateTable` | 配置合并后直接 return，**配置完全不持久化** | 高 |

**根本原因**：所有元数据持久化路径均强依赖 Redis，`cmd/main.go` 在 `cfg.Backup.Metadata.Enabled == false`（默认）时不创建 `metadataMgr`，导致 standalone 模式下既无 Redis 存储也无任何备用路径。

### 1.4 Backup 节点缺失时的行为

| 场景 | 现状行为 |
|------|---------|
| `minio_backup` 未配置 | `backupMinio = nil`，热备 goroutine 不启动（正常） |
| Dashboard 备份 API | 所有备份 API 返回 503（过于严格，元数据备份本可用 primaryMinio） |
| 元数据备份 | `metadata.Manager` 写入 `cfg.Backup.Metadata.Bucket`，**与 BackupMinIO 无关**，但 Dashboard 拦截导致无法调用 |

### 1.5 现有模块能力审计

| 模块 | 已有能力 | 可复用于备份 |
|------|---------|------------|
| `pkg/pool.MinIOPool` | 完整连接池（自定义 `http.Transport`、重试、健康检查、动态扩缩容、统计） | **热备隔离连接池** — 用独立 `MinIOPoolConfig` 创建第二个 `MinIOPool` 实例 |
| `pkg/pool.PoolManager` | 已管理 primary + backup 两个 `MinIOPool`，含 FailoverManager | **冷备** — 直接用 `GetBackupMinIOPool()` 或 `GetMinIOPool()` |
| `pkg/pool.FailoverManager` | 健康检查 + 异步同步（`EnqueueSync`）+ Prometheus 指标 | **热备补充** — 保留异步同步和健康监测能力，弱化自动切换（详见第十章） |
| `pkg/pool.RedisPool` | 连接池 + SCAN + 哨兵/集群支持 | **Checkpoint / 计划持久化** — 直接复用 |
| `storage.Uploader` 接口 | 统一 MinIO 操作抽象（Put/Get/List/Copy/Stat/Remove） | **热备 + 冷备** — 所有备份操作统一通过此接口 |
| `storage.MinioClientWrapper` | `Uploader` 实现，但创建时**无自定义 Transport** | 冷备可用；热备需要带隔离 Transport 的版本 |
| `storage.ObjectStorageImpl` | 通过 `PoolManager` 操作 MinIO，区分 primary/backup | 冷备可用 |
| `storage.StorageFactory` | `CreateBackupObjectStorage()` 方法 | 冷备目标创建 |
| `metadata.Manager` | 元数据备份/恢复全流程 | **冷备元数据部分** — 直接调用 |
| `metadata.BackupManager` | 全量/增量元数据备份、加密 | 冷备元数据 |
| `metadata.RecoveryManager` | 完整恢复流程（列表、校验、恢复） | 冷备恢复 |
| `metrics.SystemMonitor` | 采集 `runtime.MemStats` + goroutine 数 → 更新 Prometheus gauge | **统一到 RuntimeCollector** |
| `monitoring.HealthChecker` | 独立 `runtime.ReadMemStats` 检查系统健康 | 改为消费 RuntimeCollector |
| `dashboard.Server` | `monitorOverview()` / `startMetricsPush()` 独立调 `ReadMemStats` | 改为消费 RuntimeCollector |
| `config.BackupSchedule` | 已有调度模型（ID、Name、Interval、BackupType、Tables） | 冷备多计划 — 直接扩展 |
| `sse.Hub` | 事件广播（metrics / logs / node_health） | 热备/冷备状态推送 |

---

## 二、设计目标与原则

### 2.1 目标

1. **热备零影响**：对主路径写入/查询 P99 延迟影响 ≤ 2%
2. **热备增量化**：避免每次全量复制
3. **冷备完整化**：实现全量/表级数据备份，多计划并行
4. **优雅降级（BackupMinIO 缺失）**：热备停止、冷备回退 primaryMinio
5. **Standalone 模式可用**：无 Redis 时表配置、元数据操作降级到 MinIO，系统保持可运行状态，不 panic，不丢配置

### 2.2 设计原则

| 原则 | 说明 |
|------|------|
| **复用优先** | 能用现有模块就不新建。`MinIOPool`、`Uploader`、`PoolManager`、`RedisPool`、`metadata.Manager`、`config.BackupSchedule`、`sse.Hub` 全部复用 |
| **必要隔离** | 仅在性能隔离确实需要时才创建独立实例。具体而言只有一处：热备读 primary MinIO 需要独立的 `MinIOPool`（独立 `http.Transport`），因为共用连接池会抢占主业务 |
| **统一采样** | `runtime.ReadMemStats` 是 STW 操作，当前 4 处独立调用合并为一个 `RuntimeCollector`，所有消费方只读快照 |
| **接口一致** | 热备操作通过 `storage.Uploader` 接口，与主业务使用相同抽象 |

---

## 三、详细设计

### 3.1 热备（主从复制）重构

> **核心约束：热备是后台低优先级任务，绝不能影响数据库主路径（写入、查询、flush）的延迟和吞吐。**

#### 3.1.1 性能隔离 — 复用 MinIOPool 架构，创建独立实例

**现状问题**：`startBackupRoutine` 直接使用 `primaryMinio`（`MinioClientWrapper`），而 `MinioClientWrapper` 创建时**不带自定义 Transport**，与主业务共享默认连接池。

**隔离方案**：不是从零造一个新的连接管理方案，而是复用已有的 `pkg/pool.MinIOPool`，用**低配参数**创建一个专属于热备的独立实例。

```
┌──────────────────────────────────────────────────────────────────────┐
│                        连接池隔离架构                                  │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  PoolManager（已有）                                                  │
│  ┌────────────────────────────────┐                                  │
│  │  primaryPool (*MinIOPool)      │ ← http.Transport A               │
│  │  MaxConnsPerHost: CPU*4 (≈32)  │   主业务专用                      │
│  ├────────────────────────────────┤                                  │
│  │  backupPool (*MinIOPool)       │ ← http.Transport B               │
│  │  MaxConnsPerHost: CPU*4 (≈32)  │   冷备写入目标、故障转移           │
│  └────────────────────────────────┘                                  │
│                                                                      │
│  Replicator（新增，独立于 PoolManager）                               │
│  ┌────────────────────────────────┐                                  │
│  │  replicaPool (*MinIOPool)      │ ← http.Transport C（独立创建）    │
│  │  MaxConnsPerHost: 10 (硬限制)   │   仅热备读 primary 使用           │
│  │  复用 MinIOPool 全部能力：      │                                  │
│  │  - 重试 (ExecuteWithRetry)     │                                  │
│  │  - 健康检查                     │                                  │
│  │  - 统计 (GetStats)             │                                  │
│  └────────────────────────────────┘                                  │
│                                                                      │
│  写入目标: 复用 PoolManager.GetBackupMinIOPool().GetClient()          │
│  通过 MinioClientWrapper 包装为 Uploader                              │
└──────────────────────────────────────────────────────────────────────┘
```

**关键：** 热备读 primary 使用独立 `MinIOPool`（独占 `http.Transport C`），热备写 backup 复用 `PoolManager.backupPool`。

**创建隔离连接池** — 完全复用 `pool.NewMinIOPool`，不新增任何连接池代码：

```go
// internal/replication/replicator.go

func newReplicaSourcePool(cfg config.MinioConfig, maxConns int, logger *zap.Logger) (*pool.MinIOPool, error) {
    poolCfg := pool.DefaultMinIOPoolConfig()
    poolCfg.Endpoint = cfg.Endpoint
    poolCfg.AccessKeyID = cfg.AccessKeyID
    poolCfg.SecretAccessKey = cfg.SecretAccessKey
    poolCfg.UseSSL = cfg.UseSSL

    // 关键：低配参数，与主业务拉开差距
    poolCfg.MaxConnsPerHost = maxConns          // 默认 10，远小于主业务的 CPU*4
    poolCfg.MaxIdleConnsPerHost = maxConns / 2  // 空闲连接减半
    poolCfg.MaxIdleConns = maxConns             // 总空闲连接
    poolCfg.RequestTimeout = 120 * time.Second  // 大对象可能需要更长超时

    return pool.NewMinIOPool(poolCfg, logger)
}
```

**Replicator 结构**：

```go
type Replicator struct {
    // 读 primary — 独立连接池
    srcPool     *pool.MinIOPool      // 复用 MinIOPool，独立 Transport
    srcUploader storage.Uploader     // 包装为 Uploader 接口

    // 写 backup — 复用 PoolManager 的 backupPool
    dstUploader storage.Uploader     // 来自 PoolManager.GetBackupMinIOPool() 的包装

    cfg         *config.Config
    logger      *zap.Logger
    checkpoint  *CheckpointStore     // 复用 RedisPool

    // 限流
    qpsLimiter  *rate.Limiter
    sem         chan struct{}         // 并发 worker 信号量

    // 自适应退让 — 消费 RuntimeCollector 快照，不独立采样
    // (无独立 monitor 字段，直接调 metrics.GlobalRuntimeCollector.Snapshot())
}
```

**对比旧方案的简化**：

| 旧方案 (v1.1) | 新方案 (v2.0) | 原因 |
|---------------|-------------|------|
| `newIsolatedMinIOClient()` — 手写 `http.Transport` | 复用 `pool.NewMinIOPool()` | MinIOPool 已有完整的 Transport 配置、重试、健康检查、统计 |
| `Replicator.srcClient *minio.Client` 裸客户端 | `Replicator.srcPool *pool.MinIOPool` | 获得 ExecuteWithRetry、GetStats、HealthCheck 能力 |
| `Replicator.dstClient *minio.Client` 独立写客户端 | 复用 `PoolManager.GetBackupMinIOPool()` | backup pool 已存在，冷备和热备共享写目标 |
| `iorate.Limiter` 第三方带宽限流 | 移除，用 `MaxConnsPerHost` + QPS limiter 间接限流 | 连接数本身就是带宽的天然上限，避免引入新依赖 |
| 独立 `ResourceMonitor` 自行采样 | 消费 `RuntimeCollector.Snapshot()` | 统一采样，避免重复 STW |

#### 3.1.2 限流与自适应退让

**第 1 层：连接池硬限制（已有能力）**

通过 `MinIOPool` 的 `MaxConnsPerHost` 天然限制热备对 primary MinIO 的并发连接数。默认 10 个连接，主业务默认 CPU*4 ≈ 32 个，物理上互不干扰。

**第 2 层：QPS + 并发 worker 限制**

```go
type ThrottleConfig struct {
    MaxQPS              int  // MinIO API QPS 上限（默认 50）
    MaxConcurrentCopies int  // 并发复制 worker 上限（默认 4）
}
```

**第 3 层：自适应退让 — 消费统一 RuntimeSnapshot**

> **设计决策**：不新建独立 ResourceMonitor，而是消费 `metrics.RuntimeCollector` 提供的统一快照。这解决了 `runtime.ReadMemStats`（STW 操作）在 4 处独立调用的问题。

**现状问题** — `runtime.ReadMemStats` 在 4 处独立调用：
1. `internal/metrics/metrics.go` — `SystemMonitor.updateMetrics()` 每 10s
2. `internal/dashboard/server.go` — `monitorOverview()` 每次 HTTP 请求
3. `internal/dashboard/server.go` — `startMetricsPush()` 每 5s SSE 推送
4. `internal/monitoring/health.go` — `checkSystemHealth()` 健康检查

**统一方案** — 新增 `RuntimeCollector`（替换 `SystemMonitor`），所有消费方只读快照：

```go
// internal/metrics/runtime_collector.go

type RuntimeSnapshot struct {
    Goroutines   int       `json:"goroutines"`
    HeapAllocMB  float64   `json:"heap_alloc_mb"`
    HeapSysMB    float64   `json:"heap_sys_mb"`
    GCPauseMs    float64   `json:"gc_pause_ms"`
    NumGC        uint32    `json:"num_gc"`

    // 通过 provider 注入的外部指标
    MinIOPoolUsage float64 `json:"minio_pool_usage"` // 主连接池使用率 0.0～1.0
    PendingWrites  int64   `json:"pending_writes"`   // 缓冲区积压

    // 派生
    LoadLevel     LoadLevel `json:"load_level"`
    ThrottleRatio float64   `json:"throttle_ratio"`

    CollectedAt time.Time `json:"collected_at"`
}

type LoadLevel int

const (
    LoadIdle       LoadLevel = iota // 热备全速
    LoadNormal                      // 标准速度
    LoadBusy                        // 降速
    LoadOverloaded                  // 暂停
)

type RuntimeCollector struct {
    snapshot atomic.Pointer[RuntimeSnapshot]

    // 外部指标源 — 注入式，只读
    poolStatsProvider   func() (activeConns, maxConns int64)
    bufferStatsProvider func() int64

    thresholds LoadThresholds
    interval   time.Duration // 固定 5s
    stopCh     chan struct{}
}
```

**消费方改造**（3 处改动 + 1 处替换）：

```go
// 1. Dashboard monitorOverview — 改为读快照
func (s *Server) monitorOverview(c *gin.Context) {
    snap := metrics.GlobalRuntimeCollector.Snapshot()
    overview := &model.MonitorOverviewResult{
        Goroutines: snap.Goroutines,
        MemAllocMB: snap.HeapAllocMB,
        GCPauseMs:  snap.GCPauseMs,
    }
    // ...
}

// 2. Dashboard SSE push — 改为读快照
func (s *Server) startMetricsPush(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    for {
        select {
        case <-ticker.C:
            snap := metrics.GlobalRuntimeCollector.Snapshot()
            s.hub.Publish("metrics", gin.H{
                "goroutines":   snap.Goroutines,
                "mem_alloc_mb": snap.HeapAllocMB,
                "load_level":   snap.LoadLevel,
            })
        }
    }
}

// 3. HealthChecker — 改为读快照
func (hc *HealthChecker) checkSystemHealth(ctx context.Context) *HealthStatus {
    snap := metrics.GlobalRuntimeCollector.Snapshot()
    if hc.cfg.System.MaxMemoryMB > 0 && snap.HeapAllocMB > float64(hc.cfg.System.MaxMemoryMB) {
        return &HealthStatus{Status: "unhealthy", ...}
    }
    // ...
}

// 4. SystemMonitor — 替换为 RuntimeCollector（Start/Stop 接口兼容）
```

**负载等级判定**：

| 监测指标 | idle | normal | busy | overloaded |
|---------|------|--------|------|-----------|
| Goroutine 数 | <500 | <2000 | <5000 | ≥5000 |
| 堆内存占比 (`HeapAlloc/HeapSys`) | <30% | <60% | <80% | ≥80% |
| MinIO 连接池活跃率 | <30% | <60% | <80% | ≥80% |
| GC Pause | <5ms | <20ms | <50ms | ≥50ms |
| 缓冲区积压 | <100 | <1000 | <5000 | ≥5000 |

最终 LoadLevel 取各维度**最高等级**（短板原则）。

**退让行为**：

| LoadLevel | throttleRatio | QPS | worker sleep | 行为 |
|-----------|--------------|-----|-------------|------|
| idle | 0.0 | MaxQPS | 0 | 全速 |
| normal | 0.3 | MaxQPS×0.7 | 10ms | 标准 |
| busy | 0.7 | MaxQPS×0.3 | 50ms | Warn 日志 |
| overloaded | 1.0 | 0 | 暂停 30s 后重采样 | Error 日志 |

**第 4 层：时间窗口（可选）**

```yaml
backup:
  replication:
    active_window:
      start: "01:00"
      end: "06:00"
      timezone: "Asia/Shanghai"
```

#### 3.1.3 增量同步机制

**Checkpoint 存储** — 复用现有 `RedisPool`：

```go
type CheckpointStore struct {
    redisPool   *pool.RedisPool  // 复用 PoolManager 的 Redis 连接池
    keyPrefix   string           // "miniodb:replication:checkpoint"
    fallbackDir string           // Redis 不可用时降级到本地 JSON 文件
}

type SyncCheckpoint struct {
    LastSyncTime   time.Time            `json:"last_sync_time"`
    ObjectVersions map[string]time.Time `json:"object_versions"` // objectKey → lastModified
    SyncedCount    int64                `json:"synced_count"`
    FailedObjects  []string             `json:"failed_objects"`
}
```

**增量检测流程**：

```
1. 检查 RuntimeSnapshot.LoadLevel → overloaded 则跳过本轮
2. 检查时间窗口 → 不在 active_window 则 sleep 到窗口开始
3. 加载 SyncCheckpoint（从 RedisPool）
4. srcPool.GetClient().ListObjects() — 使用独立连接池，不影响主业务
   对每个 object:
   ├── NOT IN checkpoint → NEW → 加入复制队列
   ├── LastModified > checkpoint[key] → MODIFIED → 加入复制队列
   └── ELSE → SKIP
5. 可选：检测已删除对象（sync_deletes）
6. 并发复制（受 sem 信号量 + QPS limiter 限制）
   每个 CopyObject 后检查 LoadLevel，busy 则降速
7. 持久化 checkpoint
8. 每 N 次增量后做全量校验
```

#### 3.1.4 性能影响量化预期

| 维度 | 无热备 (baseline) | 热备运行中 (目标) | 硬性上限 |
|------|------------------|-----------------|---------|
| 主路径写入 P99 延迟 | X ms | ≤ X × 1.02 | ≤ X × 1.05 |
| 主路径查询 P99 延迟 | Y ms | ≤ Y × 1.02 | ≤ Y × 1.05 |
| 主 MinIO 连接池可用率 | 100% | ≥ 95% | ≥ 90% |
| 堆内存增量 | 0 | ≤ 50MB | ≤ 100MB |
| Goroutine 增量 | 0 | ≤ workers + 5 | ≤ 20 |
| 主连接池连接数增量 | 0 | **0（独立 MinIOPool）** | 0 |

#### 3.1.5 配置

```yaml
backup:
  replication:
    enabled: true
    interval: 3600                   # 秒
    workers: 4                       # 并发 worker 上限
    max_qps: 50                      # MinIO API QPS 上限
    max_conns_to_source: 10          # 独立连接池大小
    full_verify_interval: 24         # 每 N 次增量后全量校验
    sync_deletes: false
    retry_max: 3                     # 复用 MinIOPool.ExecuteWithRetry
    retry_delay: "5s"
    checkpoint_key_prefix: "miniodb:replication:checkpoint"
    # active_window:
    #   start: "01:00"
    #   end: "06:00"
    #   timezone: "Asia/Shanghai"
```

```go
type ReplicationConfig struct {
    Enabled             bool          `yaml:"enabled"`
    Interval            int           `yaml:"interval"`
    Workers             int           `yaml:"workers"`
    MaxQPS              int           `yaml:"max_qps"`
    MaxConnsToSource    int           `yaml:"max_conns_to_source"`
    FullVerifyInterval  int           `yaml:"full_verify_interval"`
    SyncDeletes         bool          `yaml:"sync_deletes"`
    RetryMax            int           `yaml:"retry_max"`
    RetryDelay          time.Duration `yaml:"retry_delay"`
    CheckpointKeyPrefix string        `yaml:"checkpoint_key_prefix"`
    ActiveWindow        *TimeWindow   `yaml:"active_window,omitempty"`
}

type TimeWindow struct {
    Start    string `yaml:"start"`
    End      string `yaml:"end"`
    Timezone string `yaml:"timezone"`
}
```

#### 3.1.6 健康监控

Replicator 通过 `sse.Hub`（复用）推送同步事件：

| 指标 | 来源 | 备注 |
|------|------|------|
| 同步状态 | Replicator | running / idle / throttled / paused / error |
| 负载等级 | `RuntimeSnapshot.LoadLevel` | 与 Dashboard 同源 |
| 上次同步时间 | checkpoint.LastSyncTime | |
| 本次同步对象 | Replicator 统计 | new + modified |
| 跳过/失败 | Replicator 统计 | |
| 延迟 (lag) | `now - lastSyncTime` | |
| 独立连接池 | `srcPool.GetStats()` | 复用 MinIOPool 统计能力 |
| 系统指标 | `RuntimeSnapshot` | Goroutines / HeapAllocMB / GCPauseMs — Dashboard 同源 |

#### 3.1.7 降级策略

```go
// cmd/main.go — 启动逻辑
if cfg.Backup.Replication.Enabled {
    backupPool := poolManager.GetBackupMinIOPool()
    if backupPool == nil {
        logger.Sugar.Warn("Replication enabled but BackupMinIO not configured — replication disabled")
    } else {
        // 创建独立连接池读 primary（复用 MinIOPool，低配参数）
        srcPool, err := newReplicaSourcePool(cfg.GetMinIO(), cfg.Backup.Replication.MaxConnsToSource, logger.Logger)
        if err != nil {
            logger.Sugar.Errorf("Failed to create replication source pool: %v", err)
        } else {
            // 写目标复用 PoolManager 的 backupPool
            dstUploader := storage.NewMinioClientWrapperFromClient(backupPool.GetClient(), logger.Logger)

            replicator := replication.NewReplicator(
                srcPool,
                dstUploader,
                cfg,
                poolManager.GetRedisPool(),  // 复用 Redis
                dashSrv.GetHub(),            // 复用 SSE Hub
                logger.Logger,
            )
            go replicator.Start(ctx)
        }
    }
}
```

- BackupMinIO 未配置 → 不启动，Warn 日志
- 运行中 BackupMinIO 不可达 → 进入 error 状态，退避重试
- 主进程过载 → 自动暂停，待恢复后继续

#### 3.1.8 Replicator 与 FailoverManager.EnqueueSync 的协作

系统中存在两条数据复制到 Backup MinIO 的通道，它们**互补而非冗余**：

```
                        数据写入流程
                        ┌──────────┐
                        │  写入请求 │
                        └────┬─────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │ ConcurrentBuffer │
                    │   flush 到主池   │
                    └────┬────────────┘
                         │
                ┌────────┴────────┐
                ▼                 ▼
        ┌──────────────┐  ┌──────────────────┐
        │ 主 MinIO     │  │ EnqueueSync      │ ← 通道 A：即时异步复制（已有）
        │ (写入成功)   │  │ → 3 个 worker    │
        └──────────────┘  │ → 队列 1000      │
                          └────────┬─────────┘
                                   │
                                   ▼ 秒级延迟
                           ┌──────────────┐
                           │ Backup MinIO │
                           └──────────────┘
                                   ▲
                                   │ 分钟～小时级
                                   │ （增量同步 + 定期全量校验）
                           ┌──────────────────┐
                           │ Replicator       │ ← 通道 B：定期增量同步（新增）
                           │ 独立 MinIOPool   │
                           │ MaxConnsPerHost=10│
                           └──────────────────┘
```

**两条通道的互补关系**：

| 维度 | 通道 A：`EnqueueSync` | 通道 B：`Replicator` |
|------|----------------------|---------------------|
| 触发时机 | 每次 flush 成功后立即入队 | 定期触发（配置间隔，如 1h） |
| 实时性 | 秒级 | 分钟～小时级 |
| 完整性保证 | **不保证** — 队列满时按策略丢弃或阻塞 | **保证** — 增量对比 + 全量校验兜底 |
| 连接池 | 使用 `PoolManager.backupPool`（与冷备共享） | 使用独立 `MinIOPool`（读 primary 隔离） |
| 覆盖范围 | 仅覆盖 Buffer flush 写入的对象 | 覆盖 primary MinIO 中的**所有**对象（含手动上传、Compaction 产物等） |
| 故障恢复 | 无断点续传 — 失败后重试 3 次即放弃 | 有 Checkpoint — 中断后从水位线继续 |
| 资源开销 | 极低（复用现有 backupPool） | 低（独立连接池，QPS 限流 + 自适应退让） |

**为什么需要两条通道**：

1. **`EnqueueSync` 的缺陷**：队列容量 1000，写入高峰时可能丢失任务（`QueueFullDropAndAlert`），且只覆盖 Buffer flush 路径，不覆盖 Compaction 合并后的新文件、手动上传的文件等
2. **`Replicator` 的缺陷**：定期触发，两次同步之间存在数据窗口，若此期间 Primary MinIO 故障，窗口内的数据只有通过 `EnqueueSync` 已复制的部分有副本
3. **互补效果**：`EnqueueSync` 提供**尽力而为的实时副本**，`Replicator` 提供**保证完整性的定期校验**，两者组合实现高实时性 + 高完整性

**运行时行为**：

```
时间线 ────────────────────────────────────────────────────▶

  写入 W1  W2  W3    W4  W5  W6    W7  W8
           │   │      │   │   │     │   │
  EnqueueSync ✓   ✓   ✓   ✗   ✓   ✓   ✓   ✓    ← W4 因队列满丢失
                       ▲                  ▲
                       │                  │
  Replicator      [增量同步]          [增量同步]  ← 发现 W4 在 primary 有
                  扫描 checkpoint       但 backup 无 → 补偿复制
                  W1-W3 已在 backup
                  无需重复复制
```

**Replicator 如何感知 EnqueueSync 已复制的对象**：

Replicator 的增量检测基于 `LastModified` 对比（`srcPool.ListObjects` vs `SyncCheckpoint`），而非依赖 `EnqueueSync` 的状态。如果 `EnqueueSync` 已成功复制某对象到 Backup MinIO，Replicator 在下次扫描时会：

1. 检测到 primary 和 backup 中该对象的 `LastModified` 一致
2. 标记为 SKIP（未变更），不重复复制
3. 更新 checkpoint 中的时间戳

因此两条通道**无需显式协调** — Replicator 天然跳过已同步的对象，只补偿 `EnqueueSync` 遗漏的部分。

---

### 3.2 冷备（Dashboard 备份计划）重构

#### 3.2.1 多备份计划 — 基于现有 `config.BackupSchedule` 扩展

项目已有 `BackupSchedule` 结构（ID/Name/Enabled/Interval/BackupType/Tables/RetentionDays），但缺少 cron 表达式和执行记录。扩展而非重建：

```go
// config/config.go — 扩展现有结构
type BackupSchedule struct {
    ID            string        `yaml:"id" json:"id"`
    Name          string        `yaml:"name" json:"name"`
    Enabled       bool          `yaml:"enabled" json:"enabled"`
    Interval      time.Duration `yaml:"interval" json:"interval"`        // 保留兼容
    CronExpr      string        `yaml:"cron_expr" json:"cron_expr"`      // 新增 cron 表达式
    BackupType    string        `yaml:"backup_type" json:"backup_type"`  // metadata / full / table
    Tables        []string      `yaml:"tables" json:"tables"`
    RetentionDays int           `yaml:"retention_days" json:"retention_days"`
    CreatedAt     time.Time     `yaml:"created_at" json:"created_at"`
    UpdatedAt     time.Time     `yaml:"updated_at" json:"updated_at"`
}
```

**调度器** — 新增，但组合现有能力：

```go
// internal/backup/scheduler.go

type Scheduler struct {
    plans     map[string]*config.BackupSchedule  // 复用已有模型
    executor  *Executor
    cron      *cron.Cron                         // robfig/cron
    store     PlanStore                          // Redis 持久化
    hub       *sse.Hub                           // 复用 SSE Hub 推送执行状态
    mu        sync.RWMutex
    logger    *zap.Logger
}

type PlanStore interface {
    ListPlans(ctx context.Context) ([]*config.BackupSchedule, error)
    SavePlan(ctx context.Context, plan *config.BackupSchedule) error
    DeletePlan(ctx context.Context, id string) error
    ListExecutions(ctx context.Context, planID string, limit int) ([]*BackupExecution, error)
    SaveExecution(ctx context.Context, exec *BackupExecution) error
}
```

**PlanStore 实现** — 复用 `RedisPool`：

```go
type RedisPlanStore struct {
    redisPool *pool.RedisPool  // 复用现有 Redis 连接池
    keyPrefix string           // "miniodb:backup:plans"
}
```

Redis 不可用时降级存储到 MinIO 对象 `_system/backup-plans.json`（通过 `storage.Uploader`）。

#### 3.2.2 全量/表级数据备份

**Executor** — 组合现有模块：

```go
type Executor struct {
    primaryUploader storage.Uploader      // 复用主业务 Uploader
    backupTarget    *BackupTarget         // 目标解析（含降级）
    metadataMgr     *metadata.Manager     // 复用元数据管理器
    redisPool       *pool.RedisPool       // 复用 Redis
    cfg             *config.Config
    logger          *zap.Logger
}
```

**全量备份流程**：

```
1. 生成 BackupID: "full-{nodeID}-{timestamp}"
2. 触发元数据快照 — 调用 metadataMgr.ManualBackup()（复用已有能力）
3. ListTables → 对每个表并发：
   a. primaryUploader.ListObjects(bucket, prefix="{tableName}/")
   b. 逐对象复制到 backupTarget.Uploader
   c. 记录 TableBackupManifest
4. 写入 FullBackupManifest
5. 更新执行记录（Redis）
6. 清理过期备份（根据 RetentionDays）
```

**表级备份流程**：

```
1. 生成 BackupID: "table-{tableName}-{nodeID}-{timestamp}"
2. 获取表配置（通过 metadataMgr.GetTableConfig）
3. primaryUploader.ListObjects → 复制到 backupTarget
4. 收集表相关 Redis 元数据 → 序列化存入备份
5. 写入 TableBackupManifest
```

**恢复** — 复用 `metadata.RecoveryManager` 处理元数据部分，数据文件通过 `Uploader.CopyObject` 回写。

#### 3.2.3 Dashboard API

| 方法 | 路径 | 描述 | 变更 |
|------|------|------|------|
| GET | `/backups/plans` | 列出所有备份计划 | **新增** |
| POST | `/backups/plans` | 创建备份计划 | **新增** |
| PUT | `/backups/plans/:id` | 更新备份计划 | **新增** |
| DELETE | `/backups/plans/:id` | 删除备份计划 | **新增** |
| POST | `/backups/plans/:id/trigger` | 手动触发计划 | **新增** |
| GET | `/backups/plans/:id/executions` | 执行历史 | **新增** |
| GET | `/backups/status` | 系统整体状态（含降级信息） | **新增** |
| POST | `/backups/full` | 触发全量备份 | **修改** — 实际执行 |
| POST | `/backups/table/:name` | 触发表级备份 | **修改** — 实际执行 |
| GET | `/backups/availability` | 备份可用性 | **修改** — 返回降级信息 |

---

### 3.3 Backup 节点缺失时的降级策略

#### 3.3.1 核心原则

| 功能 | BackupMinIO 存在 | BackupMinIO 缺失 |
|------|-----------------|-----------------|
| 热备（主从复制） | 正常运行 | **停止** — Warn 日志 |
| 冷备-元数据 | 写入 backupMinio bucket | **降级** — 写入 primaryMinio `{bucket}-backups` |
| 冷备-全量数据 | 复制到 backupMinio | **降级** — 复制到 primaryMinio `{bucket}-backups` |
| 冷备-表级数据 | 复制到 backupMinio | **降级** — 复制到 primaryMinio `{bucket}-backups` |
| 冷备-备份计划 | 正常调度 | **正常** — 使用降级目标 |
| Dashboard UI | 全功能 | 热备标记"不可用"，冷备正常（降级提示） |

#### 3.3.2 备份目标解析

```go
// internal/backup/target.go

type BackupTarget struct {
    Uploader storage.Uploader
    Bucket   string
    Degraded bool
    Mode     string // "backup_minio" | "primary_minio_isolated"
}

func ResolveBackupTarget(cfg *config.Config, poolManager *pool.PoolManager) *BackupTarget {
    backupPool := poolManager.GetBackupMinIOPool()
    if backupPool != nil && cfg.GetBackupMinIO().Endpoint != "" {
        return &BackupTarget{
            Uploader: storage.NewMinioClientWrapperFromClient(backupPool.GetClient(), nil),
            Bucket:   cfg.GetBackupMinIO().Bucket,
            Degraded: false,
            Mode:     "backup_minio",
        }
    }

    // 降级: 复用 primaryPool
    primaryPool := poolManager.GetMinIOPool()
    return &BackupTarget{
        Uploader: storage.NewMinioClientWrapperFromClient(primaryPool.GetClient(), nil),
        Bucket:   cfg.GetMinIO().Bucket + "-backups",
        Degraded: true,
        Mode:     "primary_minio_isolated",
    }
}
```

降级桶自动创建：

```go
func (t *BackupTarget) EnsureBucket(ctx context.Context) error {
    exists, err := t.Uploader.BucketExists(ctx, t.Bucket)
    if err != nil {
        return fmt.Errorf("check bucket: %w", err)
    }
    if !exists {
        return t.Uploader.MakeBucket(ctx, t.Bucket, minio.MakeBucketOptions{})
    }
    return nil
}
```

Dashboard API 修改：移除冷备 API 的 `isBackupAvailable()` 503 拦截，热备面板单独检测。

---

### 3.4 Standalone 模式元数据降级

> **目标**：无 Redis 时系统依然可以启动、正常写入数据、跨重启保留表配置，不引发 panic，不丢失配置。

#### 3.4.1 整体降级链路

```
┌─────────────────────────────────────────────────────────────────────┐
│                     元数据存储三级链路                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Level 1（优先）  →  Level 2（降级）  →  Level 3（兜底）             │
│                                                                     │
│  Redis                  MinIO                  系统默认              │
│  metadata:table_config  _system/table_configs  cfg.DefaultConfig    │
│  :{tableName}           /{tableName}.json      (snowflake +         │
│                                                 auto_generate=true) │
│                                                                     │
│  适用场景：                                                          │
│  - 有 Redis 时           - Standalone（无 Redis）                    │
│  - 分布式部署            - Redis 临时故障                            │
│                          - 元数据备份未启用                          │
└─────────────────────────────────────────────────────────────────────┘
```

#### 3.4.2 MinIO 表配置存储

**MinIO 对象路径规范**：

```
<bucket>/_system/table_configs/<tableName>.json
```

- 前缀 `_system/` 将系统配置与业务数据隔离，避免误被 Compaction 或备份操作处理
- 与 `service.MinioConfigStore`（`internal/service/minio_config_store.go`）使用相同路径规范，两层降级路径互不干扰

**存储格式**（JSON）：

```json
{
  "table_name": "teststats",
  "config": {
    "buffer_size": 1000,
    "flush_interval": 30000000000,
    "retention_days": 365,
    "backup_enabled": true,
    "id_strategy": "snowflake",
    "id_prefix": "",
    "auto_generate_id": true,
    "id_validation": {
      "max_length": 255,
      "pattern": "^[a-zA-Z0-9_-]+$",
      "allowed_chars": ""
    }
  },
  "created_at": "2026-03-19T08:00:00Z",
  "updated_at": "2026-03-19T12:30:00Z"
}
```

#### 3.4.3 降级实现层级

降级逻辑在**两个层**各自独立实现，互为补充：

**Layer A — `internal/service/table_manager.go`（`TableManager`）**

`TableManager` 直接管理 Redis Hash（`table:{name}:config`），以下方法在 `redisPool == nil` 时降级到 `MinioConfigStore`：

| 方法 | 有 Redis | Standalone |
|------|---------|-----------|
| `getTableConfigFromRedis` | 读 Redis Hash | 读 MinIO JSON |
| `CreateTable` | 写 Redis Hash + Set | 写 MinIO JSON |
| `TableExists` | `SIsMember` | 检查 MinIO JSON 是否存在 |
| `ListTables` | `SMembers` | 列举 `_system/table_configs/` 下的对象 |
| `DropTable` | 删 Redis 键 | 删 MinIO JSON |

`MinioConfigStore` 在 `NewTableManager` 中自动初始化（当 `redisPool == nil && primaryMinio != nil`）：

```go
func NewTableManager(redisPool *pool.RedisPool, primaryMinio *minio.Client,
    backupMinio *minio.Client, cfg *config.Config, logger *zap.Logger) *TableManager {

    var minioConfigStore *MinioConfigStore
    if redisPool == nil && primaryMinio != nil {
        bucket := cfg.GetMinIO().Bucket
        minioConfigStore = NewMinioConfigStore(primaryMinio, bucket, logger)
    }
    return &TableManager{..., minioConfigStore: minioConfigStore}
}
```

**Layer B — `internal/metadata/table_config.go`（`metadata.Manager`）**

`metadata.Manager` 管理 Redis String（`metadata:table_config:{name}`，JSON 序列化），以下方法在 `getRedisClient()` 失败时降级到 `m.storage`（`storage.Storage` 接口）：

| 方法 | 有 Redis | Standalone |
|------|---------|-----------|
| `GetTableConfig` | `GET metadata:table_config:{name}` | `GetObjectBytes(bucket, "_system/table_configs/{name}.json")` |
| `SaveTableConfig` | `SET metadata:table_config:{name}` | `PutObject(...)` |
| `DeleteTableConfig` | `DEL` + `SRem` | `DeleteObject(...)` |
| `ListTableConfigs` | `SMembers metadata:table_configs` | `ListObjectsSimple(bucket, "_system/table_configs/")` |
| `GetTableConfigMetadata` | `GET` + 解析完整 entry | MinIO JSON 解析（含 created_at/updated_at） |

**Bucket 确定策略**（`m.minioTableConfigBucket()`）：

```
cfg.Backup.Bucket（元数据专用 bucket）
  ↓ 空时回落
cfg.GetMinIO().Bucket（主业务 bucket）
```

**两层降级的关系**：

- Layer A（`table:{name}:config` Hash）与 Layer B（`metadata:table_config:{name}` String）在 Redis 中是**不同的 key 格式**，但降级到 MinIO 时写**同一路径**（`_system/table_configs/{name}.json`）
- 这种合并是有意为之：standalone 模式下只有一套持久化来源，避免数据分裂
- 有 Redis 时两层各自独立运行，互不干扰

#### 3.4.4 Panic 防护

`metadataMgr` 为 nil（旧版本 standalone 或 `Backup.Metadata.Enabled=false`）时，以下四个 RPC 方法原本会 panic：

```go
// 旧代码 — 直接调用，无 nil 检查
backupManager := s.metadataMgr.GetBackupManager()     // panic if nil
recoveryManager := s.metadataMgr.GetRecoveryManager() // panic if nil
```

**修复**：在四个方法入口添加统一守卫，返回语义正确的降级响应：

```go
func (s *MinIODBService) BackupMetadata(ctx context.Context, req *...) (*..., error) {
    if s.metadataMgr == nil {
        return &miniodb.BackupMetadataResponse{
            Success: false,
            Message: "Metadata manager not available (standalone mode or metadata backup disabled)",
        }, nil
    }
    // ... 正常逻辑
}
```

涉及方法：`BackupMetadata`、`RestoreMetadata`、`ListBackups`、`GetMetadataStatus`。

#### 3.4.5 `metadataManager` 始终创建

`cmd/main.go` 原逻辑：只有 `cfg.Backup.Metadata.Enabled == true` 时才创建 `metadataManager`。

**新逻辑**：始终创建 `metadataManager`，按模式分类启动：

```go
// cmd/main.go
{
    standaloneBucket := cfg.GetMinIO().Bucket
    backupBucket := cfg.Backup.Metadata.Bucket
    if backupBucket == "" {
        backupBucket = standaloneBucket // standalone 回落到主 bucket
    }

    metadataConfig := metadata.Config{
        NodeID: cfg.Server.NodeID,
        Backup: metadata.BackupConfig{
            Enabled: cfg.Backup.Metadata.Enabled,
            Bucket:  backupBucket,
            // ...
        },
    }

    metadataManager = metadata.NewManager(storageInstance, storageInstance, &metadataConfig, logger)

    if cfg.Backup.Metadata.Enabled {
        // 完整模式：启动备份定时任务
        metadataManager.Start()
    } else if redisPool == nil {
        // Standalone 模式：不启动备份调度，仅提供表配置 MinIO 降级能力
        logger.Sugar.Info("Metadata manager created in standalone mode (table config MinIO fallback only)")
    }
}
```

**三种运行状态对比**：

| 模式 | metadataManager | 表配置存储 | 备份调度 | RPC（Backup/Restore/List）|
|------|----------------|----------|---------|--------------------------|
| 完整模式（有 Redis + Metadata.Enabled=true） | 创建 + Start() | Redis | 启用 | 全功能 |
| 有 Redis，Metadata.Enabled=false | 创建，不 Start() | Redis | 不启用 | 返回"not configured" |
| **Standalone（无 Redis）** | 创建，不 Start() | **MinIO** | 不启用 | 返回"standalone mode" |

#### 3.4.6 `id_strategy` 与 `auto_generate_id` 读取修复

**新增问题**（已在本次同步修复）：`TableManager.getTableConfigFromRedis` 原本在从 Redis Hash 读取配置时，**漏掉了对 `id_strategy`、`auto_generate_id`、`id_prefix`、`id_validation_*` 字段的解析**，导致：

- 配置写入 Redis 时包含这些字段（`CreateTable` 已正确写入）
- 读取时始终是零值（`IDStrategy=""`, `AutoGenerateID=false`）
- 最终效果：`user_provided` 策略且用户未传 id → 写入空字符串 id → 后续编辑 404

**修复**（`internal/service/table_manager.go`）：

```go
// 解析 ID 生成配置（新增）
if idStrategy, ok := configData["id_strategy"]; ok {
    tableConfig.IDStrategy = idStrategy
}
if autoGenStr, ok := configData["auto_generate_id"]; ok {
    tableConfig.AutoGenerateID = autoGenStr == "true" || autoGenStr == "1"
}
// ...
```

#### 3.4.7 Standalone 模式下的系统默认配置

当所有存储来源都不可用（Redis 和 MinIO 均故障），`GetTableConfig` 最终兜底到 `systemDefaultTableConfig()`：

```go
func (s *MinIODBService) systemDefaultTableConfig() config.TableConfig {
    base := s.cfg.Tables.DefaultConfig
    // 强制使用 snowflake + 自动生成，避免空 ID 写入
    base.IDStrategy = string(idgen.StrategySnowflake)
    base.AutoGenerateID = true
    return base
}
```

同时触发异步自动补写：当从 `metadataMgr.GetTableConfig()` 得到 `(nil, nil)`（配置缺失）时，使用系统默认配置异步补写到 MinIO，避免每次都走降级路径。

---

## 四、模块复用 vs 新建 总览

| 能力 | 复用的模块 | 复用方式 | 新建 |
|------|----------|---------|------|
| 热备读 primary 连接池 | `pool.MinIOPool` | 独立实例，低配参数 | — |
| 热备写 backup | `PoolManager.GetBackupMinIOPool()` | 直接使用 | — |
| 写入即时异步复制 | `FailoverManager.EnqueueSync` | 保留现有能力，与 Replicator 互补 | — |
| 主备池健康监测 | `FailoverManager.healthCheckLoop` | 保留，提供 Prometheus 指标 | — |
| 热备 MinIO 操作 | `storage.Uploader` 接口 | srcPool 包装为 Uploader | `NewMinioClientWrapperFromClient()` 便捷构造 |
| 热备重试 | `MinIOPool.ExecuteWithRetry` | 直接调用 | — |
| 热备统计 | `MinIOPool.GetStats()` | 直接调用 | — |
| Checkpoint 存储 | `pool.RedisPool` | 直接使用 | `CheckpointStore`（thin wrapper） |
| 运行时采样 | `metrics.SystemMonitor` | **替换**为 `RuntimeCollector` | `RuntimeCollector` + `RuntimeSnapshot` |
| Prometheus 指标 | `metrics.*` gauges | 继续更新 | — |
| 元数据备份 | `metadata.Manager` | 直接调用 `.ManualBackup()` | — |
| 元数据恢复 | `metadata.RecoveryManager` | 直接调用 | — |
| 冷备计划模型 | `config.BackupSchedule` | 扩展 `CronExpr` 字段 | — |
| 冷备计划持久化 | `pool.RedisPool` | 直接使用 | `RedisPlanStore`（thin wrapper） |
| SSE 状态推送 | `sse.Hub` | 直接 `.Publish()` | — |
| 自适应退让指标 | — | — | `RuntimeCollector`（统一采样） |
| 冷备调度 | — | — | `backup.Scheduler`（cron 调度） |
| 冷备执行 | — | — | `backup.Executor`（全量/表级） |
| 备份目标解析 | `PoolManager` | 从中获取 pool | `backup.BackupTarget`（解析逻辑） |
| QPS 限流 | — | — | `rate.Limiter`（Go 标准库 `x/time/rate`） |
| **Standalone 表配置存储（service 层）** | `storage.Storage`（`m.storage` 字段，已有） | `TableManager` → `MinioConfigStore` → MinIO JSON | `internal/service/minio_config_store.go`（新增） |
| **Standalone 表配置存储（metadata 层）** | `storage.Storage`（`m.storage` 字段，已有） | `metadata.Manager` 方法降级调用 `m.storage.PutObject/GetObjectBytes` | 新增 MinIO 辅助函数（在 `table_config.go` 内） |
| **Standalone panic 防护** | — | — | `BackupMetadata` 等 4 个方法头部加 `metadataMgr == nil` 守卫 |
| **系统默认配置兜底** | `config.Tables.DefaultConfig`（已有） | `systemDefaultTableConfig()` 强制 snowflake+AutoGenerate，并异步补写 | `systemDefaultTableConfig()` 辅助函数 |

**新增文件数**：9 个（v2.1 在 v2.0 基础上新增 `internal/service/minio_config_store.go`）  
**新增外部依赖**：`robfig/cron`（调度）、`x/time/rate`（QPS 限流）  
**移除外部依赖**：`c9s/goprocinfo/iorate`（v1.1 方案中的带宽限流器，不再需要）

---

## 五、配置变更

### 5.1 config/config.go

```go
type BackupConfig struct {
    Enabled     bool                 `yaml:"enabled"`
    Interval    int                  `yaml:"interval"`     // 兼容旧配置
    Metadata    MetadataBackupConfig `yaml:"metadata"`
    Replication ReplicationConfig    `yaml:"replication"`  // 新增
    Schedules   []BackupSchedule     `yaml:"schedules"`    // 已有，扩展 CronExpr
}
```

### 5.2 config.yaml

```yaml
backup:
  enabled: true
  interval: 3600               # 兼容旧配置

  metadata:
    enabled: true
    interval: 30m
    retention_days: 7
    bucket: "miniodb-metadata"

  replication:
    enabled: true
    interval: 3600
    workers: 4
    max_qps: 50
    max_conns_to_source: 10
    full_verify_interval: 24
    sync_deletes: false
    retry_max: 3
    retry_delay: "5s"
    # active_window:
    #   start: "01:00"
    #   end: "06:00"
    #   timezone: "Asia/Shanghai"

  schedules:
    - id: "default-metadata"
      name: "每日元数据备份"
      enabled: true
      backup_type: "metadata"
      cron_expr: "0 2 * * *"
      retention_days: 7
```

---

## 六、文件清单

| 文件路径 | 类型 | 说明 |
|---------|------|------|
| `internal/replication/replicator.go` | 新增 | 增量同步引擎（使用独立 MinIOPool 实例 + Uploader 接口） |
| `internal/replication/checkpoint.go` | 新增 | 同步水位线（基于复用 RedisPool） |
| `internal/replication/throttle.go` | 新增 | QPS limiter + 并发信号量 + 自适应退让（消费 RuntimeSnapshot） |
| `internal/metrics/runtime_collector.go` | 新增 | 统一运行时快照采集器（替换 SystemMonitor，供 Dashboard / 热备 / 健康检查共用） |
| `internal/backup/scheduler.go` | 新增 | 备份计划调度器（cron + RedisPool） |
| `internal/backup/executor.go` | 新增 | 备份执行器（组合 Uploader + metadata.Manager） |
| `internal/backup/target.go` | 新增 | 备份目标解析（组合 PoolManager，含降级逻辑） |
| `internal/backup/store.go` | 新增 | 计划/执行记录持久化（基于复用 RedisPool） |
| `config/config.go` | 修改 | 新增 `ReplicationConfig`、`TimeWindow`；`BackupSchedule` 加 `CronExpr` 字段 |
| `config/config.yaml` | 修改 | 新增 `replication` 段 |
| `cmd/main.go` | 修改 | 替换 `startBackupRoutine` 为 Replicator + Scheduler |
| `internal/metrics/metrics.go` | 修改 | `SystemMonitor` → 委托给 `RuntimeCollector` |
| `internal/dashboard/server.go` | 修改 | monitorOverview/startMetricsPush 改读快照；备份 API 降级逻辑；多计划 API |
| `internal/monitoring/health.go` | 修改 | checkSystemHealth 改读快照 |
| `internal/storage/minio.go` | 修改 | 新增 `NewMinioClientWrapperFromClient()` 便捷构造函数 |
| `internal/service/minio_config_store.go` | **新增** | MinIO 表配置 KV 存储（Standalone 模式专用，路径 `_system/table_configs/{name}.json`） |
| `internal/service/table_manager.go` | 修改 | 新增 `minioConfigStore` 字段；5 个方法加 Standalone 降级分支 |
| `internal/metadata/table_config.go` | 修改 | `SaveTableConfig/GetTableConfig/DeleteTableConfig/ListTableConfigs` 加 Redis 不可用时降级到 `m.storage` 的 MinIO 路径 |
| `internal/service/miniodb_service.go` | 修改 | `BackupMetadata/RestoreMetadata/ListBackups/GetMetadataStatus` 加 `metadataMgr == nil` 守卫；新增 `systemDefaultTableConfig()` + `GetTableConfig` 自动补写逻辑；`NewMinIODBService` 接受可选 `primaryMinio` 参数 |
| `cmd/main.go` | 修改 | `metadataManager` 始终创建（不再依赖 `Backup.Metadata.Enabled`）；向 `NewMinIODBService` 注入 MinIO client |

---

## 七、实施阶段

| 阶段 | 内容 | 预估工时 | 优先级 |
|------|------|---------|--------|
| **Phase 0** ✅ | **Standalone 模式元数据降级（已完成）** | — | P0 |
| | - `metadataMgr == nil` panic 防护（4 个 RPC 方法） | | |
| | - `metadata/table_config.go`：Redis → MinIO 降级路径 | | |
| | - `internal/service/minio_config_store.go` 新增 | | |
| | - `table_manager.go`：5 个方法加 Standalone 降级分支 | | |
| | - `cmd/main.go`：`metadataManager` 始终创建 | | |
| | - `id_strategy/auto_generate_id` Redis 字段解析修复 | | |
| | - `systemDefaultTableConfig()` + 配置缺失自动补写 | | |
| **Phase 1** | RuntimeCollector 统一采样 | 1-2d | P0 |
| | - `internal/metrics/runtime_collector.go` | | |
| | - 改造 Dashboard monitorOverview / startMetricsPush | | |
| | - 改造 HealthChecker.checkSystemHealth | | |
| | - 替换 SystemMonitor | | |
| **Phase 2** | 降级策略 + BackupTarget | 1-2d | P0 |
| | - `internal/backup/target.go` | | |
| | - `storage.NewMinioClientWrapperFromClient()` | | |
| | - 修改 Dashboard 备份 API 移除 503 | | |
| | - `GET /backups/status` API | | |
| **Phase 3** | 冷备-全量/表级备份 | 3-4d | P0 |
| | - `internal/backup/executor.go` | | |
| | - FullBackup / TableBackup / Restore | | |
| | - Manifest 读写 | | |
| **Phase 4** | 冷备-多计划调度 | 2-3d | P1 |
| | - `internal/backup/scheduler.go` + `store.go` | | |
| | - Dashboard 计划管理 API | | |
| **Phase 5** | 热备增量化 + 性能隔离 | 3-4d | P1 |
| | - `newReplicaSourcePool()` 创建独立 MinIOPool 实例 | | |
| | - `internal/replication/replicator.go` + `checkpoint.go` + `throttle.go` | | |
| | - 替换 `cmd/main.go` 中的 `startBackupRoutine` | | |
| | - 性能基准测试 | | |
| **Phase 6** | Dashboard UI + 测试 | 2-3d | P2 |
| | - 热备状态面板、降级提示、多计划管理 | | |
| | - 单元测试 + 集成测试 | | |

**总计（剩余）**：约 12-18 人天（Phase 0 已完成交付，不计入估算；对比 v1.1 的 15-22 人天，减少约 20%）

---

## 八、兼容性

| 方面 | 策略 |
|------|------|
| 旧配置 `backup.interval` | 映射到 `replication.interval`，打 deprecation 日志 |
| 旧配置 `backup.enabled` | 保持总开关，`replication.enabled` 细粒度控制 |
| 现有元数据备份 | 不变 — `metadata.Manager` 继续独立运行 |
| 现有备份 API | 保持路径兼容，行为升级 |
| `BackupSchedule.Interval` | 保留字段兼容，`CronExpr` 优先 |
| 数据库迁移 | 无 — 新增 Redis key / MinIO 对象不影响现有数据 |

---

## 九、风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| **热备共享连接池抢占主业务** | 写入/查询延迟飙升 | 独立 `MinIOPool` 实例（独立 `http.Transport`，MaxConnsPerHost=10） |
| **热备 ListObjects 风暴** | MinIO 服务端 CPU 飙升 | QPS limiter + 增量检测 + 分页间 sleep |
| **多处 `runtime.ReadMemStats` STW** | 所有 goroutine 多次暂停 | `RuntimeCollector` 统一采样，4 处消费方只读快照 |
| 主进程高负载时热备未退让 | 雪崩效应 | RuntimeSnapshot 自适应暂停（5s 采样，overloaded 立即暂停） |
| 增量同步遗漏对象 | 数据不一致 | 定期全量校验 (`full_verify_interval`) |
| 降级桶与数据桶同 MinIO | 单点故障 | UI 提示配置独立 BackupMinIO |
| 全量备份期间写入 | 一致性窗口 | Manifest 记录快照时间点 |
| 大表备份耗时过长 | 阻塞调度 | 异步执行 + 进度追踪 |
| Redis 不可用 | Checkpoint/计划丢失 | 降级到本地文件 / MinIO 对象 |
| **应用层 failover 自动切换导致数据不一致** | 切到备池的写入在回切后丢失；Redis 索引与文件位置不匹配 | **不做完整热切换** — FailoverManager 保留异步同步和健康监测，弱化自动切换逻辑（详见第十章） |
| Primary MinIO 宕机 | 写入/查询中断 | 引导用户部署 MinIO 纠删码集群（存储层解决高可用） |
| **Standalone：两层降级写同一 MinIO 路径竞争** | 并发写可能相互覆盖 | PutObject 原子覆写，两层写入内容结构一致，覆写结果等幂，影响极小 |
| **Standalone：MinIO 故障时表配置丢失** | 重启触发自动创建，自定义配置丢失 | `systemDefaultTableConfig()` 兜底防 panic；Error 日志提示；引导配置 MinIO 高可用 |
| **Standalone：`metadataManager` 始终创建** | 无用时消耗少量内存 | 对象极轻量（仅字段引用），未调用 `Start()` 则无调度 goroutine，成本可忽略 |

---

## 十、FailoverManager 定位与故障切换决策

### 10.1 现有 FailoverManager 审计

`pkg/pool/failover_manager.go` 已实现一套完整的故障切换框架：

| 能力 | 实现状态 |
|------|---------|
| 主/备双池管理 | 完整 — `PoolManager` 管理 `minioPool` + `backupPool` |
| 自动健康检查 | 完整 — `healthCheckLoop()` 每 15s 检测主备池 |
| 主池故障 → 自动切备 | 完整 — `switchToBackup()` 含健康检查 + 状态持久化 + Prometheus 指标 |
| 主池恢复 → 自动回切 | 完整 — `checkAndRecover()` |
| 状态持久化 | 完整 — Redis `pool:failover:state`（5 分钟 TTL） |
| 异步同步到备池 | 完整 — `EnqueueSync` → 3 个 `syncWorker`，队列容量 1000 |
| 队列满策略 | 完整 — Block / ReturnError / DropAndAlert 三种策略 |
| Prometheus 指标 | 完整 — 6 项指标（`FailoverTotal`, `CurrentActivePool`, `*Healthy`, `SyncQueue*`） |

### 10.2 实际覆盖率分析

| 数据路径 | 是否受 failover 保护 | 位置 |
|---------|---------------------|------|
| **写入（Buffer flush）** | **是** | `concurrent_buffer.go` → `ExecuteWithFailover()` |
| **写入后异步复制** | **是** | `concurrent_buffer.go` → `EnqueueSync()` |
| **StorageImpl.GetObject** | **是** | `storage.go:127` — 唯一走 `ExecuteWithFailover` 的读操作 |
| Querier 查询（读文件） | **否** | `query.go` 直接用 `primaryMinio`（`Uploader`），不经过 PoolManager |
| StorageImpl.PutObject | **否** | `storage.go:108` — 直接 `GetMinIOPool()` |
| StorageImpl.ListObjects | **否** | `storage.go:142` — 直接 `GetMinIOPool()` |
| StorageImpl.DeleteObject | **否** | `storage.go:166` — 直接 `GetMinIOPool()` |
| StorageImpl.GetObjectBytes | **否** | `storage.go:301` — 直接 `GetMinIOPool()` |
| StorageImpl.BucketExists | **否** | `storage.go:271` — 直接 `GetMinIOPool()` |
| Dashboard backup handlers | **否** | `getMinIOClient()` 每次新建客户端 |

**结论**：框架完整度高，但实际覆盖率低 — 只有 Buffer flush 和 StorageImpl.GetObject 两处受保护。

### 10.3 是否应该补全故障热切换？

**决策：不做完整的存储层故障热切换。**

#### 10.3.1 原因

**MinIODB 的存储高可用应该在 MinIO 层解决，而非应用层。**

```
    不推荐：应用层 failover                    推荐：存储层自身高可用
    ┌─────────────────────────┐               ┌──────────────────────────────┐
    │  MinIODB                │               │  MinIODB                     │
    │    ↓ failover ↓         │               │    ↓                         │
    │  Pool A    Pool B       │               │  MinIOPool                   │
    │    ↓          ↓         │               │    ↓                         │
    │  MinIO-1   MinIO-2      │               │  MinIO 集群 (4节点纠删码)    │
    │  (独立)    (独立)       │               │  ┌──┐ ┌──┐ ┌──┐ ┌──┐       │
    │  → 数据不一致风险高     │               │  │N1│ │N2│ │N3│ │N4│       │
    └─────────────────────────┘               │  └──┘ └──┘ └──┘ └──┘       │
                                              │  任何 2 节点故障仍可读写     │
                                              └──────────────────────────────┘
```

| 对比维度 | 应用层 failover | MinIO 集群部署 |
|---------|----------------|---------------|
| 数据一致性 | **致命缺陷** — 切到备池写入的数据，回切后主池没有；只有主→备单向同步，无备→主回写 | MinIO 纠删码保证一致性 |
| 查询完整性 | **严重** — Redis 索引指向主池路径，切换后索引与实际文件位置不匹配 | 透明，单一存储端点 |
| 实现复杂度 | 极高 — Querier、StorageImpl 所有方法、metadata.Manager 全部改造 | 零 — MinIODB 代码无需修改 |
| Parquet 缓存 | 切换后本地缓存失效，DuckDB 查询中断 | 无影响 |
| 测试成本 | 极高 — 需模拟各种时序的故障注入测试 | 交给 MinIO 自身保证 |
| 运维成本 | 低（只需两个独立 MinIO 实例） | 中（需 4+ 节点） |

**核心论点**：MinIODB 是 OLAP 数据库，核心诉求是**数据准确性**和**查询性能**。短暂的不可用（分钟级）远比数据不一致可接受。应用层做存储 failover 在解决"可用性"的同时引入了更严重的"一致性"问题。

#### 10.3.2 不补全 failover 的风险评估

| 场景 | 现有行为 | 可接受性 |
|------|---------|---------|
| Primary MinIO 单节点部署，节点宕机 | 写入/查询全部失败，直到 MinIO 恢复 | 对 OLAP 可接受 — 不是实时交易系统 |
| Primary MinIO 集群部署，单节点故障 | MinIO 自动处理，MinIODB 无感知 | 完全可接受 |
| Primary MinIO 集群部署，超过容忍度 | 写入/查询失败 | 属于灾难级故障，应走灾备恢复流程 |

### 10.4 FailoverManager 在备份体系中的正确定位

FailoverManager 的价值不在"热切换"，而在以下两点：

| 保留的能力 | 用途 | 与备份体系的关系 |
|-----------|------|-----------------|
| **异步同步** (`EnqueueSync`) | Buffer flush 后即时复制到备池 | 作为**热备的补充** — 与 Replicator 的定期增量同步互补，提供接近实时的数据副本 |
| **健康检查** (`healthCheckLoop`) | 监测主备池健康状态 | 为 Dashboard 提供健康指标；为热备提供备池可达性判断 |
| **Prometheus 指标** | `PrimaryPoolHealthy` / `BackupPoolHealthy` 等 | 告警基础设施 |

| 弱化/移除的能力 | 原因 |
|----------------|------|
| `switchToBackup` 自动切换 | 切换后数据一致性无法保证，改为**告警 + 人工决策** |
| `checkAndRecover` 自动回切 | 同上 |
| `Execute()` 中的自动重试切换 | 保留重试，移除自动切换到备池 |

#### 10.4.1 建议的改造（低优先级，可后续迭代）

```go
// FailoverManager.Execute — 改为：重试当前池，不自动切换
func (fm *FailoverManager) Execute(ctx context.Context, operation func(*MinIOPool) error) error {
    pool := fm.GetActivePool() // 始终返回主池
    err := pool.ExecuteWithRetry(ctx, func() error {
        return operation(pool)
    })
    if err != nil {
        // 不切换，只告警
        fm.logger.Error("Primary MinIO operation failed, manual intervention may be needed",
            zap.Error(err))
        metrics.PrimaryPoolHealthy.Set(0)
    }
    return err
}
```

#### 10.4.2 热备 Replicator 与 FailoverManager.EnqueueSync 的协作

> 详细设计见 [3.1.8 节](#318-replicator-与-failovermanagerenqueuesync-的协作)。

核心结论：`EnqueueSync`（秒级即时复制，可能丢失）与 `Replicator`（定期增量同步，保证完整性）互补而非冗余，两条通道无需显式协调 — Replicator 天然跳过已同步对象，只补偿 EnqueueSync 遗漏的部分。

### 10.5 对部署架构的建议

在文档和 Dashboard 中引导用户选择正确的高可用策略：

| 部署规模 | 推荐方案 | MinIODB 侧配置 |
|---------|---------|----------------|
| 开发/测试 | 单节点 MinIO，无备份 | `backup.enabled: false` |
| 小型生产 | 单节点 MinIO + 独立 Backup MinIO | `backup.replication.enabled: true` — 热备做容灾副本 |
| 标准生产 | **MinIO 4 节点纠删码集群** | `backup.replication.enabled: false` — 存储层自身高可用，无需热备 |
| 高可用生产 | MinIO 集群 + 独立 Backup MinIO（异地） | `backup.replication.enabled: true` — 异地容灾 |

---

## 附录：版本变更对照

### v1.1 → v2.0

| 维度 | v1.1 | v2.0 | 原因 |
|------|------|------|------|
| 热备读连接 | 手写 `http.Transport` + `minio.New()` | 复用 `pool.NewMinIOPool()` 低配实例 | 获得重试、健康检查、统计能力，不重复造轮子 |
| 热备写连接 | 独立 `*minio.Client` | 复用 `PoolManager.GetBackupMinIOPool()` | backup pool 已存在 |
| 带宽限流 | `iorate.Limiter`（新依赖） | 移除，用连接池 `MaxConnsPerHost` 间接限流 | 连接数天然限制带宽，减少依赖 |
| 自适应退让 | 独立 `ResourceMonitor` 自行 `ReadMemStats` | 消费 `RuntimeCollector.Snapshot()` | 统一采样，避免 STW |
| 冷备计划模型 | 全新 `backup.BackupPlan` | 扩展已有 `config.BackupSchedule` | 避免重复定义 |
| Checkpoint 存储 | 全新 Redis wrapper | 复用 `pool.RedisPool` | 薄封装即可 |
| 备份目标 | 全新 `BackupTarget` + `Uploader` | 通过 `PoolManager` 获取 pool → 包装为 Uploader | 与系统其余部分一致的模式 |
| SSE 推送 | 全新推送逻辑 | 复用 `sse.Hub.Publish()` | Hub 已支持任意 topic |
| 新增文件 | 11 个 | 8 个 | 减少 3 个文件（monitor.go 合并、plan.go 复用已有模型、独立 minio 创建函数不再需要） |
| 新增依赖 | `robfig/cron` + `x/time/rate` + `c9s/goprocinfo/iorate` | `robfig/cron` + `x/time/rate` | 减少 1 个外部依赖 |
| 预估工时 | 15-22 人天 | 12-18 人天 | 复用减少约 20% 工作量 |

### v2.0 → v2.1（Standalone 元数据降级，已完成）

| 维度 | v2.0 | v2.1 |
|------|------|------|
| Standalone 表配置存储 | 无（无 Redis 时直接失败） | `MinioConfigStore`：`_system/table_configs/{name}.json` |
| metadata.Manager 降级 | 无（Redis 失败直接 return error） | `GetTableConfig` 等方法降级到 `m.storage`（MinIO） |
| Metadata RPC panic 防护 | 无（`metadataMgr == nil` 时 panic） | 4 个方法入口加 nil 守卫，返回语义正确的降级响应 |
| metadataManager 创建时机 | 仅 `Backup.Metadata.Enabled=true` | 始终创建，按模式决定是否 `Start()` |
| `id_strategy` Redis 读取 | 漏字段解析，始终为零值 | 补齐 `id_strategy/auto_generate_id/id_prefix` 解析 |
| 配置缺失兜底 | 使用 `DefaultConfig`（可能为 `user_provided`） | `systemDefaultTableConfig()` 强制 snowflake+AutoGenerate |
| 新增文件 | 8 个（计划中） | +1：`internal/service/minio_config_store.go` |
