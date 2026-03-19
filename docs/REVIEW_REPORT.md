# MinIODB 备份体系 — 审查报告

---

# 第一部分：实现状态审查（对照 BACKUP_DESIGN.md v2.1）

**审查日期**：2026-03-19  
**对照文档**：`docs/BACKUP_DESIGN.md` v2.1  
**审查范围**：Phase 0 ~ Phase 6 + FailoverManager 定位调整  
**整体完成度**：约 90%

---

## Phase 0 — Standalone 模式元数据降级 ✅ 完成

| 设计项 | 状态 | 位置 |
|--------|------|------|
| `MinioConfigStore` 新增 | ✅ | `internal/service/minio_config_store.go` |
| `TableManager` 5 个方法降级分支 | ✅ | `internal/service/table_manager.go` |
| `metadata/table_config.go` Redis→MinIO 降级 | ✅ | 5 个方法均有降级路径 |
| 4 个 RPC 方法 nil 守卫 | ✅ | `internal/service/miniodb_service.go` |
| `systemDefaultTableConfig()` + 自动补写 | ✅ | `miniodb_service.go:491-519` |
| `metadataManager` 始终创建 | ✅ | `cmd/main.go:240-277` |
| `id_strategy` 字段解析修复 | ✅ | `table_manager.go:488-507` |

---

## Phase 1 — RuntimeCollector 统一采样 ✅ 完成

| 设计项 | 状态 | 位置 |
|--------|------|------|
| `RuntimeCollector` 新增 | ✅ | `internal/metrics/runtime_collector.go`（294 行） |
| `SystemMonitor` 已替换 | ✅ | 代码中已不存在 |
| Dashboard `monitorOverview` 读快照 | ✅ | `server.go:1685` |
| Dashboard `startMetricsPush` 读快照 | ✅ | `server.go:1793` |
| `HealthChecker` 读快照 | ✅ | `health.go:228` |
| 4 处 `runtime.ReadMemStats` 统一 | ✅ | 仅 RuntimeCollector 一处调用 |

---

## Phase 2 — 降级策略 + BackupTarget ⚠️ 大部分完成

| 设计项 | 状态 | 位置 |
|--------|------|------|
| `BackupTarget` 新增 | ✅ | `internal/backup/target.go`（108 行） |
| `NewMinioClientWrapperFromClient()` | ✅ | `internal/storage/minio.go:83` |
| Dashboard 备份 API 移除 503 | ⚠️ 部分 | 见下方详情 |

**Dashboard 503 清理详情**：

| API | 状态 | 说明 |
|-----|------|------|
| `listBackups` | ✅ 已改 | 支持降级模式 |
| `triggerMetadataBackup` | ✅ 已改 | 支持降级模式 |
| `getBackupsStatus` | ✅ 已改 | 返回降级信息 |
| `restoreBackup` | ❌ 仍 503 | BackupMinIO 不可用时拦截 |
| `downloadBackup` | ❌ 仍 503 | BackupMinIO 不可用时拦截 |
| `verifyBackup` | ❌ 仍 503 | BackupMinIO 不可用时拦截 |
| `deleteBackup` | ❌ 仍 503 | BackupMinIO 不可用时拦截 |
| `getBackupSchedule` | ❌ 仍 503 | BackupMinIO 不可用时拦截 |

---

## Phase 3 — 冷备全量/表级备份 ✅ 完成

| 设计项 | 状态 | 位置 |
|--------|------|------|
| `Executor` 新增 | ✅ | `internal/backup/executor.go`（818 行） |
| `FullBackup()` | ✅ | 含并发表复制、Manifest、过期清理 |
| `TableBackup()` | ✅ | 含配置持久化、Redis 元数据收集 |
| `Restore()` | ✅ | 含 RecoveryManager 元数据恢复 |

---

## Phase 4 — 冷备多计划调度 ✅ 完成

| 设计项 | 状态 | 位置 |
|--------|------|------|
| `Scheduler` 新增 | ✅ | `internal/backup/scheduler.go`（465 行） |
| `PlanStore`（Redis + Fallback） | ✅ | `internal/backup/store.go`（389 行） |
| `config.BackupSchedule` 扩展 `CronExpr` | ✅ | `config/config.go:331-365` |
| `config.ReplicationConfig` 新增 | ✅ | `config/config.go:315-327` |
| `config.TimeWindow` | ✅ | `config/config.go:308-312` |

---

## Phase 5 — 热备增量化 + 性能隔离 ✅ 完成

| 设计项 | 状态 | 位置 |
|--------|------|------|
| `Replicator` 新增 | ✅ | `internal/replication/replicator.go`（544 行） |
| `CheckpointStore`（Redis + 本地文件降级） | ✅ | `internal/replication/checkpoint.go`（345 行） |
| QPS + 自适应退让 | ✅ | `internal/replication/throttle.go`（120 行） |
| 旧 `startBackupRoutine` 已移除 | ✅ | `cmd/main.go` 中已被 Replicator 替代 |
| 独立 MinIOPool 创建 | ✅ | `cmd/main.go:485-510` |

---

## Phase 6 — Dashboard UI + 测试 ⚠️ 部分完成

| 设计项 | 状态 | 说明 |
|--------|------|------|
| 后端单元测试 | ✅ | `executor_test.go`, `target_test.go`, `scheduler_test.go`, `store_test.go`, `replicator_test.go`, `checkpoint_test.go`, `throttle_test.go` 均存在 |
| 热备状态 API | ✅ | 通过 `getBackupsStatus` 暴露 Replicator 状态 |
| 前端 UI 更新 | ❓ | 需进一步检查 `dashboard-ui/` |

---

## 第十章 — FailoverManager 定位调整 ⏳ 未开始

设计建议弱化自动切换（`switchToBackup`/`checkAndRecover`）、保留异步同步（`EnqueueSync`）和健康检查能力。标记为**低优先级**，可后续迭代。

---

## 实现状态总结

| 阶段 | 状态 | 剩余工作 |
|------|------|---------|
| Phase 0 | ✅ 完成 | — |
| Phase 1 | ✅ 完成 | — |
| Phase 2 | ⚠️ 90% | 5 个 Dashboard API 仍返回 503，需改为降级模式 |
| Phase 3 | ✅ 完成 | — |
| Phase 4 | ✅ 完成 | — |
| Phase 5 | ✅ 完成 | — |
| Phase 6 | ⚠️ 60% | 前端 UI 更新待确认 |
| FailoverManager | ⏳ 未开始 | 低优先级，可后续迭代 |

### 待办优先级

1. **P0** — Phase 2 剩余：`restoreBackup`/`downloadBackup`/`verifyBackup`/`deleteBackup`/`getBackupSchedule` 在 BackupMinIO 缺失时应使用 `BackupTarget` 降级到 primaryMinio，而非直接返回 503
2. **P1** — Phase 6：前端 Dashboard 热备状态面板、降级模式提示、多计划管理 UI
3. **P2** — FailoverManager：弱化自动切换逻辑，改为告警 + 人工决策

---
---

# 第二部分：代码质量审查

**审查日期**：2026-03-19
**审查范围**：优化方案全部 20 个任务（Phase 1-6），涉及 51 个文件，+9845 / -580 行
**构建状态**：`go build ./...` PASS
**测试状态**：backup / replication / metrics 全部 PASS

---

## 总裁定：REJECT — 存在必须修复的阻断性问题

| 严重级别 | 数量 | 说明 |
|---------|------|------|
| **P0 Critical** | 1 | 核心功能正确性 bug |
| **P1 High** | 8 | 逻辑/架构/安全问题 |
| **P2 Medium** | 9 | 代码气味/可维护性 |
| **P3 Low** | 6 | 命名/风格建议 |

---

## P0 Critical — 必须立即修复

### 1. `replicator.go:364-377` — CopyObject 用于跨服务器复制，根本不工作

```go
func (r *Replicator) copyObject(ctx context.Context, bucket, objectKey string) error {
    src := minio.CopySrcOptions{Bucket: bucket, Object: objectKey}
    dst := minio.CopyDestOptions{Bucket: bucket, Object: objectKey}
    _, err := r.dstUploader.CopyObject(ctx, dst, src)
    // ...
}
```

`minio.CopyObject` 是**服务端拷贝**（server-side copy），要求 source 和 destination 在**同一个 MinIO 实例**内。热备场景是 primary MinIO → backup MinIO（不同服务器），`CopyObject` 只会在 backup 服务器上找不到 source 对象，返回 `NoSuchKey` 或静默失败。

**建议修复**：改为 `srcUploader.GetObject` + `dstUploader.PutObject` 流式传输：

```go
func (r *Replicator) copyObject(ctx context.Context, bucket, objectKey string) error {
    data, err := r.srcUploader.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
    if err != nil {
        return fmt.Errorf("get object %s from source: %w", objectKey, err)
    }
    _, err = r.dstUploader.PutObject(ctx, bucket, objectKey,
        bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
    if err != nil {
        return fmt.Errorf("put object %s to destination: %w", objectKey, err)
    }
    // ... checkpoint update
}
```

> 注意：当前 `GetObject` 返回 `[]byte`，大对象会占用大量内存。后续应考虑流式 `io.Reader` 版本。

---

## P1 High — 合并前必须修复

### 2. `runtime_collector.go:60,279-292` — GlobalRuntimeCollector 数据竞争

```go
var GlobalRuntimeCollector *RuntimeCollector
```

`InitGlobalRuntimeCollector` 写 + `GetRuntimeSnapshot` 读，无任何同步原语保护。违反 Go 内存模型，`-race` 会报错。

**建议**：改用 `atomic.Pointer[RuntimeCollector]`，或在 init 阶段保证先于所有 goroutine 启动写入（当前 main.go 顺序虽然安全，但 API 上不防御）。

### 3. `server.go:1686,1795` — Dashboard 仍在直接调用 runtime.ReadMemStats

```go
var memStats runtime.MemStats
runtime.ReadMemStats(&memStats)
```

`monitorOverview` 和 `monitorStream` 两处仍然独立调用 `runtime.ReadMemStats`，**完全抵消了 RuntimeCollector 统一采样的设计目标**。这是 Phase 1 的核心动机，不应遗漏。

**建议**：替换为 `snap := metrics.GetRuntimeSnapshot()`，从 snapshot 读取各字段。

### 4. `replicator.go:275` — skippedCount 声明但永远为零

```go
var syncedCount, skippedCount, failedCount int64
```

`skippedCount` 被声明、日志记录、存入 stats，但**没有任何代码路径对其执行 `++`**。要么是遗漏了跳过逻辑（throttle overload 时应 skip），要么应删除该变量。

**建议**：在 `ErrThrottleOverloaded` break 前，`skippedCount = int64(len(objectsToSync)) - syncedCount - failedCount`。

### 5. `replicator.go:379-390` — 每个对象都执行 Load→Save checkpoint，I/O 风暴

```go
checkpoint, loadErr := r.checkpoint.Load(ctx, bucket)
if loadErr != nil {
    checkpoint = NewSyncCheckpoint()
}
checkpoint.UpdateObjectVersion(objectKey, time.Now())
if saveErr := r.checkpoint.Save(ctx, bucket, checkpoint); saveErr != nil {
```

同步 10,000 个对象 = 10,000 次 Redis `GET` + 10,000 次 Redis `SET`（每次序列化完整 map）。

**建议**：在 `syncOnce` 层面持有 checkpoint 对象，批量更新，每 N 个对象或同步完成后统一 Save 一次。

### 6. `cmd/main.go:480` — Fatalf 导致可选功能崩溃整个服务

```go
if err != nil {
    logger.Sugar.Fatalf("Failed to create replicator: %v", err)
}
```

热备是可选功能（BackupMinIO 可以不配），但创建失败会 `Fatalf` 终止进程。同一文件中 metadata、compaction 等都是 `Warnf` + 跳过。

**建议**：改为 `Warnf` + `replicator = nil` + skip start。

### 7. `scheduler.go:94` — Stop() 后无法重新 Start()，double-close panic 风险

```go
func (s *Scheduler) Stop() {
    s.mu.Lock()
    defer s.mu.Unlock()
    if !s.running { return }
    close(s.stopCh)
    s.cron.Stop()
    s.running = false
}
```

`stopCh` 在构造函数中创建一次，`Stop()` close 后不重建。若调用 `Start() → Stop() → Start()`，第二次 Start 使用已关闭的 channel，`select` 立即返回。

**建议**：`Start()` 中重新 `s.stopCh = make(chan struct{})`，或使用 `context.WithCancel` 替代。

### 8. `scheduler.go:240-280` — Plan 指针闭包捕获导致竞态

Scheduler 的 cron 回调通过闭包捕获 `*config.BackupSchedule` 指针。`AddPlan` / `UpdatePlan` 可能在 cron 触发时并发修改同一 plan 对象。

**建议**：在 cron 注册时对 plan 做深拷贝（值拷贝或显式 clone）。

### 9. `storage/minio.go:84` — panic 会崩溃服务

```go
func NewMinioClientWrapperFromClient(client *minio.Client, logger *zap.Logger) *MinioClientWrapper {
    if client == nil {
        panic("minio client is nil")
    }
```

生产代码中不应 `panic`。调用方可能传入 nil（如 pool 创建失败时），应返回 error。

**建议**：改为 `func NewMinioClientWrapperFromClient(...) (*MinioClientWrapper, error)`。

---

## P2 Medium — 建议合并前修复

| # | 文件:行 | 问题 |
|---|---------|------|
| 10 | `metrics.go:362-415` | `SystemMonitor` 已标记 deprecated 且无调用方，应彻底删除而非保留死代码 |
| 11 | `executor.go:353,372` | `json.MarshalIndent` 错误被静默忽略，可能上传空/损坏的 manifest |
| 12 | `store.go:77-87` | Redis key 由用户输入的 plan ID 直接拼接，无格式验证，存在 key 注入风险 |
| 13 | `executor.go` `backupTable` | 对象拷贝失败时仍报告 `Status: "completed"`，应追踪失败数并在有失败时标记 `partial` |
| 14 | `replicator.go:333-361` | `detectIncremental` 将所有对象 key 收入内存 slice + checkpoint 的 `ObjectVersions` map 无界增长，百万级 bucket 会 OOM |
| 15 | `health.go:228` | `GetRuntimeSnapshot` 无新鲜度检查，RuntimeCollector 停止后报告过期数据为"健康" |
| 16 | `cmd/main.go:76-534` | `main()` 已达 460 行，应至少提取 `initBackupSubsystem()` |
| 17 | `server.go:2271-2279` | `createBackupPlan` 的 `BackupType` 未做白名单校验 |
| 18 | `scheduler.go:242` | `executePlan` 使用 `context.Background()` 而非传播父 context，shutdown 时无法取消正在执行的备份 |

---

## P3 Low — 记录，不阻断

| # | 文件:行 | 建议 |
|---|---------|------|
| 19 | `runtime_collector.go:13-26` | 类型别名 re-export 扩大 API 表面，建议添加包文档说明 `throttle` 是 source of truth |
| 20 | `runtime_collector.go:104` | `Start()` 无防重入保护，二次调用会启动重复 goroutine |
| 21 | `runtime_collector.go:264-277` | 魔法数字 `0.3`, `0.7`, `1.0` 应提取为命名常量 |
| 22 | `runtime_collector_test.go:270,320,370` | 空子测试名 `t.Run("")`，应使用描述性名称 |
| 23 | `cmd/main.go:58` | `_ "minIODB/internal/metrics"` blank import 冗余 |
| 24 | `server.go:1620-1621` | `CPUPercent`/`UptimeHours` 固定返回 0，与字段语义不符（历史遗留） |

---

## 优先修复顺序建议

| 优先级 | 问题 # | 修复内容 | 预估工作量 |
|--------|--------|---------|-----------|
| 🔴 1 | #1 (P0) | CopyObject → GetObject + PutObject | 中（需改接口 + 测试） |
| 🟠 2 | #3 (P1) | Dashboard 替换 ReadMemStats | 小 |
| 🟠 3 | #5 (P1) | Checkpoint 批量保存 | 中 |
| 🟠 4 | #6 (P1) | Fatalf → Warnf | 小 |
| 🟠 5 | #2 (P1) | GlobalRuntimeCollector atomic | 小 |
| 🟠 6 | #7 (P1) | Scheduler stopCh 重建 | 小 |
| 🟠 7 | #9 (P1) | panic → error | 小 |
| 🟠 8 | #4 (P1) | skippedCount 修正 | 小 |
| 🟠 9 | #8 (P1) | Plan 深拷贝 | 小 |
| 🟡 10 | P2 批量 | 按表格逐项处理 | 中 |

---
---

# 第三部分：高可用架构方案

**日期**：2026-03-19  
**范围**：Standalone HA → 多节点 MinIODB + Redis Cluster + MinIO Cluster + 只读 Failover + 写缓冲  
**目标**：渐进式高可用 — 从最小部署到生产级集群，每一级都有明确的故障容忍能力

---

## 一、现有 HA 基础设施审计

### 1.1 已实现（可直接复用）

| 组件 | 位置 | 能力 | 就绪度 |
|------|------|------|--------|
| Redis 多模式 | `pkg/pool/redis_pool.go` | Standalone / Sentinel / Cluster 三种模式全支持 | ✅ 生产就绪 |
| 服务发现 | `internal/discovery/service_registry.go` | Redis 心跳注册（30s 周期 / 60s TTL）、节点发现、哈希环 | ✅ 可用 |
| 一致性哈希 | `pkg/consistenthash/consistenthash.go` | SHA-256 + 虚拟节点 | ✅ 生产就绪 |
| 写入路由 | `internal/coordinator/coordinator.go:182` | `WriteCoordinator.RouteWrite()` 按一致性哈希路由，gRPC 转发远程写入 | ✅ 可用 |
| 分布式查询 | `internal/coordinator/coordinator.go:282` | Scatter-Gather + MapReduce 聚合（5 种策略） | ✅ 可用 |
| 分布式锁 | `pkg/lock/redis_lock.go` | Redis SETNX + Lua 解锁，支持 Cluster 模式 | ✅ 生产就绪 |
| MinIO Failover | `pkg/pool/failover_manager.go` | 主备切换 + 健康检查 + 异步同步 + Prometheus 指标 | ⚠️ 需改造 |
| Replicator 热备 | `internal/replication/replicator.go` | 增量同步 + Checkpoint + 自适应退让 | ✅ 生产就绪 |
| EnqueueSync | `pkg/pool/failover_manager.go:487` | Buffer flush 后即时异步复制到 backup | ✅ 生产就绪 |
| WAL | `internal/wal/wal.go` | 本地文件 WAL，CRC32 校验，64MB 自动轮转，崩溃恢复回放 | ✅ 生产就绪 |
| gRPC + REST 双协议 | `internal/transport/` | 节点间通信 + 客户端 API | ✅ 生产就绪 |
| 事件订阅 | `internal/subscription/` | Redis Streams + Kafka 数据变更事件 | ✅ 生产就绪 |

### 1.2 已实现但存在缺陷

| 组件 | 问题 | 影响 |
|------|------|------|
| `ServiceRegistry.GetNodeForKey()` | 使用 `hash % len(nodeIDs)` 而非一致性哈希环 | 节点增减时大量 key 重映射 |
| `FailoverManager.Execute()` | 自动切换到 backup 写入 → 数据不一致 | 回切后 backup 写入丢失 |
| Querier MinIO 客户端 | 静态 `minioClient`，不经过 FailoverManager | MinIO 故障时查询无法自动切换 |
| `file_index:{table}:*` | 分布式查询依赖此索引，但 flush 路径未写入 | 分布式查询无法正确判断数据分布 |

### 1.3 缺失（需新建）

| 组件 | 用途 | 优先级 |
|------|------|--------|
| **ServiceMode 状态机** | 集群级 `READ_WRITE` / `DEGRADED_READ_ONLY` / `RECOVERING` | P0 |
| **FailoverAwareUploader** | 查询路径根据 ServiceMode 动态路由到 primary/backup MinIO | P0 |
| **Redis Leader Election** | 单实例任务（Replicator、Scheduler）仅 leader 执行 | P1 |
| **WAL 持久卷感知** | 节点故障后另一节点可接管并回放 WAL | P2 |
| **集群级健康聚合** | 多节点健康状态汇总 + 自动触发 ServiceMode 切换 | P1 |

---

## 二、渐进式高可用架构

### 核心原则

> **每一级都是完整可用的部署形态，不是半成品。** 用户根据可靠性需求选择部署级别，高级别兼容低级别的全部能力。

### 2.1 架构总览

```
Level 0         Level 1              Level 2               Level 3
Standalone      Standalone HA        Distributed           Full HA
────────────    ─────────────────    ──────────────────    ──────────────────

┌──────────┐    ┌──────────────┐     ┌─────────────┐      ┌──────────────────┐
│ MinIODB  │    │  MinIODB     │     │  MinIODB ×N │      │  MinIODB ×N      │
│ (single) │    │  (single)    │     │  + Redis ×3 │      │  + Redis Cluster │
│          │    │              │     │  + MinIO ×1 │      │  + MinIO Cluster │
│ MinIO ×1 │    │  MinIO ×1    │     │             │      │  + Backup MinIO  │
│          │    │  Backup ×1   │     └─────────────┘      │                  │
│ 无 Redis │    │              │                           │  只读 Failover   │
└──────────┘    │  无 Redis    │                           │  + 写缓冲        │
                │  只读Failover│                           └──────────────────┘
                └──────────────┘
```

---

### 2.2 Level 0 — Standalone（开发/测试）

**部署**：1 MinIODB + 1 MinIO + 0 Redis

```
┌─────────────────────────────┐
│          MinIODB            │
│  Buffer ──→ WAL (local)     │
│    │                        │
│    ▼ flush                  │
│  MinIO (single)             │
│                             │
│  表配置 → MinIO JSON 降级   │
│  （_system/table_configs/） │
└─────────────────────────────┘
```

| 维度 | 说明 |
|------|------|
| 故障容忍 | MinIODB 崩溃 → WAL 回放恢复未 flush 数据；MinIO 磁盘故障 → 数据丢失 |
| 元数据 | `MinioConfigStore` 降级到 MinIO JSON（Phase 0 已实现） |
| 适用场景 | 开发、测试、PoC |
| **已实现** | ✅ 完整可用 |

---

### 2.3 Level 1 — Standalone HA（小型生产）

**部署**：1 MinIODB + 1 Primary MinIO + 1 Backup MinIO + 0 Redis

> **关键洞察**：Standalone 模式下只要配置了 Backup MinIO，系统就具备实质性的高可用能力。无需 Redis、无需多节点 MinIODB，已有的 Replicator + EnqueueSync + 只读 Failover 机制即可提供单节点级别的存储容灾。

```
┌───────────────────────────────────────────────────────┐
│                     MinIODB (single)                  │
│                                                       │
│  Buffer ──→ WAL (local)                               │
│    │                                                  │
│    ▼ flush                                            │
│  ┌──────────────┐    EnqueueSync     ┌──────────────┐│
│  │ Primary MinIO│ ──────────────────→│ Backup MinIO ││
│  │              │                    │              ││
│  │              │  Replicator (增量) │              ││
│  │              │ ──────────────────→│              ││
│  └──────┬───────┘                    └──────┬───────┘│
│         │                                   │        │
│         │ ← READ_WRITE 正常读写             │        │
│         │                                   │        │
│         │    Primary 宕机时:                │        │
│         │    ─────────────                  │        │
│         ×    自动切换 ──────────────────────→│        │
│              DEGRADED_READ_ONLY             │        │
│              读 → Backup                    │        │
│              写 → Buffer + WAL（暂存）      │        │
│                                                       │
│  表配置 → MinIO JSON（_system/table_configs/）        │
│  Checkpoint → 本地 JSON（无 Redis 降级）              │
└───────────────────────────────────────────────────────┘
```

**Level 1 的 HA 能力**：

| 故障场景 | 系统行为 | 数据影响 |
|---------|---------|---------|
| MinIODB 进程崩溃 | 重启 + WAL 回放 | 无丢失（WAL 保护） |
| Primary MinIO 宕机 | 自动切到 Backup 只读 + 写缓冲 | 查询可用（可能滞后秒级）；写入暂存 buffer |
| Primary MinIO 恢复 | 自动 flush buffer → 切回 READ_WRITE | 无丢失 |
| Backup MinIO 宕机 | 主路径不受影响；Replicator 暂停并 Error 日志 | 仅失去容灾副本 |
| 主机断电（MinIODB + MinIO 同机） | 重启后 WAL + MinIO 数据恢复 | 未 flush 数据 WAL 恢复 |

**为什么 Standalone 也能只读 Failover**：

1. **Replicator 不依赖 Redis** — `CheckpointStore` 已有本地 JSON 降级（`checkpoint.go`）
2. **EnqueueSync 不依赖 Redis** — 直接操作 backup MinIO pool
3. **查询不依赖 Redis** — Standalone Querier 通过 `ListObjects` 发现 `.parquet` 文件
4. **ServiceMode 可用本地 atomic** — 无需 Redis 协调（单节点无竞争）
5. **表配置不依赖 Redis** — `MinioConfigStore` 已降级到 MinIO JSON

**Standalone HA 与分布式模式的差异**：

| 维度 | Standalone HA (Level 1) | Distributed (Level 2+) |
|------|------------------------|----------------------|
| ServiceMode 存储 | 本地 `atomic.Value` | Redis key（集群共享） |
| Failover 检测 | 单节点健康检查 | 多节点投票 quorum |
| 写缓冲上限 | 单节点内存（需保守） | 可分散到多节点 |
| 切换延迟 | 15s（单次健康检查失败即可判定） | 30s+（需 quorum 确认） |
| 元数据存储 | MinIO JSON | Redis（Sentinel/Cluster HA） |

**Level 1 需要新增/改造的部分**：

| 改造项 | 说明 | 工作量 |
|--------|------|--------|
| `ServiceMode` 本地状态机 | `atomic.Value` 存储 `READ_WRITE`/`DEGRADED_READ_ONLY`/`RECOVERING` | 1d |
| `FailoverAwareUploader` | 实现 `storage.Uploader`，根据 ServiceMode 路由到 primary/backup | 1d |
| Querier 客户端替换 | 将静态 `minioClient` 替换为 `FailoverAwareUploader` | 0.5d |
| `FailoverManager` 改造 | `switchToBackup()` → `switchToReadOnly()`，不改变写路径 active pool | 1d |
| Service 层写入守卫 | `WriteData`/`UpdateData`/`DeleteData` 检查 ServiceMode | 0.5d |
| 健康端点增强 | 返回 `service_mode` + `data_freshness` + `buffer_usage` | 0.5d |
| **合计** | | **4-5d** |

---

### 2.4 Level 2 — Distributed（标准生产）

**部署**：N MinIODB + Redis Sentinel (3 节点) + MinIO (单实例或集群)

```
                    ┌──────────────┐
                    │ Load Balancer│
                    └──────┬───────┘
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │MinIODB-1 │ │MinIODB-2 │ │MinIODB-3 │
        │ NodeID:1 │ │ NodeID:2 │ │ NodeID:3 │
        │ Buffer   │ │ Buffer   │ │ Buffer   │
        │ WAL      │ │ WAL      │ │ WAL      │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │             │             │
             │   ┌─────────┴──────────┐  │
             │   │  ServiceRegistry   │  │
             │   │  (Redis 心跳发现)  │  │
             │   │  WriteCoordinator  │  │
             │   │  (一致性哈希路由)  │  │
             │   │  QueryCoordinator  │  │
             │   │  (Scatter-Gather)  │  │
             │   └────────────────────┘  │
             │                           │
        ┌────┴───────────────────────────┴────┐
        │         Redis Sentinel (×3)         │
        │  Master + 2 Replica + 3 Sentinel    │
        │  分布式锁 / 索引 / 表配置 / 心跳    │
        └─────────────────────────────────────┘
                         │
        ┌────────────────┴────────────────────┐
        │           MinIO (集群或单机)         │
        │     所有 MinIODB 节点共享存储        │
        └─────────────────────────────────────┘
```

**写入流程**：

```
Client → LB → MinIODB-X
                  │
                  ▼
         WriteCoordinator.RouteWrite()
                  │
         hashRing.Get(record.ID) → NodeID
                  │
            ┌─────┴─────┐
            │ 本节点?    │
            ├─ YES ──→ Buffer.Add() → WAL → flush → MinIO
            │
            └─ NO ───→ gRPC Forward → MinIODB-Y.WriteData()
                                          │
                                     Buffer.Add() → WAL → flush → MinIO
```

**查询流程**：

```
Client → LB → MinIODB-X
                  │
                  ▼
         QueryCoordinator.ExecuteDistributedQuery()
                  │
         createQueryPlan() → 查 Redis file_index 确定数据分布
                  │
         ┌────────┼────────┐
         ▼        ▼        ▼
      Node-1   Node-2   Node-3    ← 并行 gRPC 查询
         │        │        │
         ▼        ▼        ▼
      mergeResults()               ← MapReduce 聚合
         │
         ▼
      返回客户端
```

**Leader Election（新增）**：

```go
// internal/discovery/leader.go
// 基于 Redis SETNX + TTL 的轻量 Leader 选举

type LeaderElection struct {
    redisPool   *pool.RedisPool
    nodeID      string
    leaderKey   string          // "cluster:leader"
    leaseTTL    time.Duration   // 30s
    renewTicker *time.Ticker    // 10s
}

// 仅 Leader 执行的任务：
// - Replicator（热备增量同步）
// - Scheduler（冷备计划调度）
// - Compaction 协调（避免多节点重复合并）
```

**Level 2 的 HA 能力**：

| 故障场景 | 系统行为 | 数据影响 |
|---------|---------|---------|
| 1 个 MinIODB 节点宕机 | 服务发现 60s 内剔除；哈希环自动 rehash；其他节点接管写入 | 宕机节点未 flush 数据依赖 WAL 恢复（需持久卷） |
| Redis Master 故障 | Sentinel 秒级自动提升 Replica | 极短暂中断（秒级） |
| MinIO 短暂不可用 | Buffer 持续接收写入，flush 重试 | 写入延迟增加，无丢失 |
| Leader 节点宕机 | 30s 租约到期，其他节点竞选新 Leader | Replicator/Scheduler 短暂暂停 |

**Level 2 在 Level 1 基础上新增**：

| 改造项 | 说明 | 工作量 |
|--------|------|--------|
| Leader Election | Redis SETNX 轻量选举 + 租约续期 | 2d |
| `ServiceRegistry.GetNodeForKey()` 修复 | 替换 `hash % N` 为一致性哈希环 | 0.5d |
| `file_index` 写入 | Buffer flush 成功后写入 `file_index:{table}:{nodeID}` | 1d |
| Replicator Leader 化 | 仅 Leader 启动 Replicator 和 Scheduler | 1d |
| 集群健康聚合 | 多节点健康状态汇总到 Redis + Dashboard 展示 | 1.5d |
| **合计** | | **6d** |

---

### 2.5 Level 3 — Full HA（高可用生产 / 异地容灾）

**部署**：N MinIODB + Redis Cluster (6+节点) + MinIO Cluster (4+节点) + Backup MinIO + 只读 Failover

```
                          ┌───────────────┐
                          │ Load Balancer │
                          └───────┬───────┘
                ┌─────────────────┼─────────────────┐
                ▼                 ▼                  ▼
          ┌──────────┐      ┌──────────┐      ┌──────────┐
          │MinIODB-1 │      │MinIODB-2 │      │MinIODB-3 │
          │ Leader ★ │      │ Follower │      │ Follower │
          │ Buffer   │      │ Buffer   │      │ Buffer   │
          │ WAL (PV) │      │ WAL (PV) │      │ WAL (PV) │
          └────┬─────┘      └────┬─────┘      └────┬─────┘
               │                 │                  │
               │     ┌───────────┴───────────┐      │
               │     │    ServiceMode        │      │
               │     │  (Redis 集群共享)      │      │
               │     │  READ_WRITE           │      │
               │     │  DEGRADED_READ_ONLY   │      │
               │     │  RECOVERING           │      │
               │     └───────────────────────┘      │
               │                                    │
          ┌────┴────────────────────────────────────┴────┐
          │            Redis Cluster (6+ 节点)           │
          │  3 Master + 3 Replica，自动分片 + 故障转移    │
          │  分布式锁 / 索引 / Leader / ServiceMode      │
          └──────────────────┬──────────────────────────┘
                             │
          ┌──────────────────┴──────────────────────────┐
          │          Primary MinIO Cluster               │
          │        4+ 节点纠删码 (EC:2)                  │
          │        任意 2 节点故障仍可读写               │
          └──────────────────┬──────────────────────────┘
                             │
                   EnqueueSync (秒级)
                   Replicator (分钟级, Leader 执行)
                             │
                             ▼
          ┌──────────────────────────────────────────────┐
          │          Backup MinIO (独立 / 异地)           │
          │        灾备副本，只读 Failover 数据源         │
          └──────────────────────────────────────────────┘
```

**只读 Failover + 写缓冲 状态机**：

```
             所有节点持续检测 Primary MinIO 健康状态
             检测失败 → 向 Redis 投票 "minio_primary_unhealthy"
                              │
                    ┌─────────┴─────────┐
                    │ Quorum 达成?       │
                    │ (N/2 + 1 节点同意) │
                    └─────────┬─────────┘
                              │ YES
                              ▼
┌────────────────┐    ┌───────────────────────┐    ┌────────────────┐
│  READ_WRITE    │───▶│  DEGRADED_READ_ONLY   │───▶│  RECOVERING    │
│  (Normal)      │    │                       │    │                │
│                │    │  所有节点:             │    │  Primary 恢复  │
│                │    │   读 → Backup MinIO   │    │  (Quorum 确认) │
│                │    │   写 → Buffer + WAL   │    │                │
│                │    │   flush 暂停          │    │  各节点:       │
│                │◀───┤                       │    │  flush buffer  │
│                │    │  Buffer 满 →          │    │  → Primary     │
│                │    │   拒绝新写入          │    │  全部完成后 →  │
│                │    │                       │    │  READ_WRITE    │
└────────────────┘    └───────────────────────┘    └───────┬────────┘
        ▲                                                  │
        └──────────────────────────────────────────────────┘
```

**ServiceMode 集群协调**：

```go
// internal/ha/service_mode.go

type ServiceMode int32

const (
    ModeReadWrite       ServiceMode = 0  // 正常
    ModeDegradedReadOnly ServiceMode = 1  // Primary MinIO 不可用
    ModeRecovering      ServiceMode = 2  // Primary 恢复中，flush 积压
)

type ServiceModeManager struct {
    // Standalone: atomic.Value（本地）
    // Distributed: Redis key "cluster:service_mode"
    redisPool     *pool.RedisPool    // nil = standalone
    localMode     atomic.Int32       // standalone 使用
    modeKey       string             // "cluster:service_mode"
    voteKey       string             // "cluster:minio_health_votes"
    quorumSize    int                // N/2 + 1
    nodeID        string

    // 回调
    onModeChange  []func(old, new ServiceMode)
}

// Quorum 投票切换逻辑
func (m *ServiceModeManager) VoteMinIOUnhealthy(ctx context.Context) error
func (m *ServiceModeManager) VoteMinIOHealthy(ctx context.Context) error
func (m *ServiceModeManager) GetMode() ServiceMode

// Standalone 模式：直接切换（无 quorum）
// Distributed 模式：Redis 原子计数 + 阈值判定
```

**FailoverAwareUploader（读路径动态路由）**：

```go
// internal/storage/failover_uploader.go

type FailoverAwareUploader struct {
    primary      storage.Uploader
    backup       storage.Uploader       // 可为 nil
    modeManager  *ha.ServiceModeManager
    logger       *zap.Logger
}

// 实现 storage.Uploader 接口
func (u *FailoverAwareUploader) GetObject(ctx, bucket, key, opts) ([]byte, error) {
    if u.modeManager.GetMode() == ha.ModeDegradedReadOnly && u.backup != nil {
        return u.backup.GetObject(ctx, bucket, key, opts)
    }
    return u.primary.GetObject(ctx, bucket, key, opts)
}

// ListObjects、StatObject 等同理
// PutObject 始终走 primary（失败由 buffer 层重试处理）
```

**写入守卫**：

```go
// internal/service/miniodb_service.go

func (s *MinIODBService) WriteData(ctx, req) (*resp, error) {
    mode := s.modeManager.GetMode()

    switch mode {
    case ha.ModeReadWrite:
        // 正常写入
    case ha.ModeDegradedReadOnly:
        // 检查 buffer 容量
        if s.buffer.Usage() > s.cfg.HA.MaxBufferUsagePercent {
            return nil, status.Errorf(codes.Unavailable,
                "primary storage unavailable, write buffer full (%.0f%%)", usage)
        }
        // 接受写入（进 buffer + WAL，不 flush）
        // 返回 warning header
    case ha.ModeRecovering:
        // 正在恢复，等待 flush 完成
        return nil, status.Errorf(codes.Unavailable, "recovering, please retry")
    }
    // ... 正常 ingest 逻辑
}
```

**Level 3 的完整故障矩阵**：

| 故障场景 | 影响范围 | 系统行为 | 数据安全 | RTO | RPO |
|---------|---------|---------|---------|-----|-----|
| **1 MinIODB 节点宕机** | 部分写入延迟 | 60s 内发现剔除；哈希 rehash；其他节点接管 | WAL 恢复（持久卷） | ~60s | 0（PV） |
| **所有 MinIODB 宕机** | 全面中断 | 重启 + WAL 回放 | WAL 保护 | 分钟级 | 0（PV） |
| **Redis 1 Master 故障** | 极短暂 | Cluster 自动 failover | 无影响 | ~2s | 0 |
| **Redis 少数派不可用** | 无 | Cluster 自动处理 | 无影响 | 0 | 0 |
| **Redis 多数派不可用** | 元数据不可用 | MinIODB 降级 standalone 模式 | 表配置降级 MinIO JSON | ~30s | 0 |
| **MinIO Cluster 1-2 节点** | 无 | 纠删码自动修复 | 无影响 | 0 | 0 |
| **MinIO Cluster 全部宕机** | 查询降级 | Quorum → DEGRADED_READ_ONLY；读 Backup | 写入 buffer 暂存 | ~30s | 秒级（EnqueueSync 延迟） |
| **MinIO Cluster 恢复** | 恢复中 | RECOVERING → flush buffer → READ_WRITE | 无丢失 | 取决于 buffer 量 | 0 |
| **Backup MinIO 宕机** | 仅容灾能力丧失 | Replicator 暂停；主路径不受影响 | 主数据安全 | 0 | N/A |
| **网络分区（MinIODB 节点间）** | 部分查询不完整 | Scatter-gather 跳过不可达节点，返回 partial 结果 | 各节点本地数据安全 | 0 | 0 |
| **网络分区（MinIODB ↔ MinIO）** | 写入暂停 | Buffer 持续积累；flush 重试 | WAL 保护 | 网络恢复后 | 0 |
| **灾难（整个主站不可用）** | 全面中断 | 人工切 Backup MinIO 为 primary | 从最后备份恢复 | 分钟级 | Replicator 间隔 |

---

## 三、各 Level 配置参考

### 3.1 Level 0 — Standalone

```yaml
server:
  node_id: "node-1"
  grpc_port: "8080"
  rest_port: "8081"

minio:
  endpoint: "localhost:9000"
  bucket: "miniodb-data"

# redis: 不配置
# minio_backup: 不配置
# backup.replication.enabled: false
```

### 3.2 Level 1 — Standalone HA

```yaml
server:
  node_id: "node-1"

minio:
  endpoint: "localhost:9000"
  bucket: "miniodb-data"

minio_backup:
  endpoint: "backup-host:9000"
  bucket: "miniodb-data"           # 与 primary 同名

# redis: 不配置（standalone 模式）

backup:
  replication:
    enabled: true
    interval: 1800                 # 30 分钟增量同步
    workers: 2
    max_qps: 30
    max_conns_to_source: 5

ha:
  failover:
    enabled: true
    health_check_interval: 15s
    mode: "read_only"              # 故障时只读（非读写切换）
    max_buffer_usage_percent: 80   # buffer 使用率超 80% 拒绝写入
    recovery_stability_window: 30s # primary 恢复后等待 30s 确认稳定
```

### 3.3 Level 2 — Distributed

```yaml
server:
  node_id: "node-1"               # 每节点不同

minio:
  endpoint: "minio-cluster:9000"
  bucket: "miniodb-data"

redis:
  mode: "sentinel"
  addrs: ["redis-1:26379", "redis-2:26379", "redis-3:26379"]
  master_name: "miniodb-master"

coordinator:
  hash_ring_replicas: 150
  distributed_query_timeout: 30s

ha:
  leader_election:
    enabled: true
    lease_ttl: 30s
    renew_interval: 10s
```

### 3.4 Level 3 — Full HA

```yaml
server:
  node_id: "node-1"

minio:
  endpoint: "minio-cluster:9000"   # 4+ 节点纠删码集群
  bucket: "miniodb-data"

minio_backup:
  endpoint: "dr-site-minio:9000"   # 异地灾备
  bucket: "miniodb-data"

redis:
  mode: "cluster"
  cluster_addrs:
    - "redis-1:6379"
    - "redis-2:6379"
    - "redis-3:6379"
    - "redis-4:6379"
    - "redis-5:6379"
    - "redis-6:6379"

backup:
  replication:
    enabled: true
    interval: 3600
    workers: 4
    max_qps: 50
    max_conns_to_source: 10

ha:
  failover:
    enabled: true
    health_check_interval: 15s
    mode: "read_only"
    quorum_size: 2                  # N/2 + 1 节点确认才切换
    max_buffer_usage_percent: 80
    recovery_stability_window: 30s
  leader_election:
    enabled: true
    lease_ttl: 30s
    renew_interval: 10s
```

---

## 四、实施路线

| 阶段 | 内容 | 解锁 Level | 优先级 | 预估工时 |
|------|------|-----------|--------|---------|
| **HA-Phase 1** | ServiceMode 状态机 + FailoverAwareUploader + 写入守卫 | **Level 1** | P0 | 4-5d |
| | - `internal/ha/service_mode.go`（本地 atomic + Redis 双模式） | | | |
| | - `internal/storage/failover_uploader.go`（Uploader 动态路由） | | | |
| | - `FailoverManager` 改造为只读切换 | | | |
| | - Querier 替换静态 minioClient 为 FailoverAwareUploader | | | |
| | - Service 层 WriteData/UpdateData/DeleteData 守卫 | | | |
| | - 健康端点增强（service_mode + data_freshness） | | | |
| **HA-Phase 2** | Leader Election + 集群健康聚合 | **Level 2** | P1 | 3-4d |
| | - `internal/discovery/leader.go`（Redis SETNX 选举） | | | |
| | - Replicator / Scheduler Leader 化 | | | |
| | - `ServiceRegistry.GetNodeForKey()` 修复为一致性哈希 | | | |
| | - 集群健康聚合到 Redis + Dashboard | | | |
| **HA-Phase 3** | Quorum 投票 + `file_index` 补全 | **Level 3** | P1 | 3-4d |
| | - Quorum 投票 ServiceMode 切换 | | | |
| | - Buffer flush 写入 `file_index:{table}:{nodeID}` | | | |
| | - 分布式查询基于 file_index 精确路由 | | | |
| | - SSE 推送 ServiceMode 变更事件 | | | |
| **HA-Phase 4** | 健壮性加固 | 全 Level | P2 | 2-3d |
| | - WAL 持久卷感知（K8s PVC 场景） | | | |
| | - Buffer 容量感知 + 反压（back-pressure） | | | |
| | - 端到端故障注入测试 | | | |

**总计**：约 12-16 人天

### 与 BACKUP_DESIGN.md 实施计划的关系

| BACKUP_DESIGN Phase | 状态 | 与 HA 方案的关系 |
|---------------------|------|-----------------|
| Phase 0（Standalone 降级） | ✅ 已完成 | HA Level 0/1 的基础 |
| Phase 1（RuntimeCollector） | ✅ 已完成 | HA 自适应退让的数据源 |
| Phase 2（BackupTarget 降级） | ⚠️ 90% | HA Level 1 的前置条件（需补完 Dashboard 503） |
| Phase 3（冷备 Executor） | ✅ 已完成 | HA Level 3 灾难恢复的基础 |
| Phase 4（冷备 Scheduler） | ✅ 已完成 | HA Level 2+ Leader 化后仅 Leader 执行 |
| Phase 5（热备 Replicator） | ✅ 已完成 | HA Level 1+ 的容灾数据源 |
| FailoverManager 定位调整 | ⏳ 未开始 | **合并到 HA-Phase 1** — 改造为只读切换 |

---

## 五、设计决策记录

### 5.1 为什么不用 Raft / Paxos 共识协议？

| 考量 | 分析 |
|------|------|
| 实现成本 | 正确实现 Raft 需要 3-6 个月，且极易出错 |
| 场景适配 | MinIODB 是 OLAP 系统，不是强一致性事务数据库。读多写少，最终一致性可接受 |
| 已有基础设施 | Redis Cluster 本身就是共识系统（Raft-like），复用其一致性保证 |
| 数据持久层 | MinIO 集群已有纠删码保证存储层一致性 |
| 结论 | **Redis 作为协调层 + MinIO 作为持久层**，MinIODB 节点是无状态计算层（buffer 为唯一本地状态，WAL 保护） |

### 5.2 为什么只读 Failover 而非读写 Failover？

| 维度 | 只读 Failover | 读写 Failover |
|------|-------------|-------------|
| 数据一致性 | ✅ 完美 — 不写入 backup | ❌ 致命 — 回切后 backup 写入丢失 |
| 实现复杂度 | 低 — 仅切换读路径 | 极高 — 索引重建 + reconciliation |
| OLAP 适配 | ✅ 查询可用 > 一切 | ❌ 为写可用牺牲一致性不值得 |
| 恢复干净度 | ✅ Primary 恢复后 flush buffer 即可 | ❌ 需要复杂的双向合并 |

### 5.3 为什么 Standalone 也值得做 HA？

| 考量 | 说明 |
|------|------|
| 用户基数 | 大部分用户初期部署为单节点，跳过 Redis 直接用 Standalone |
| 边际成本 | Level 1 的改造（ServiceMode + FailoverAwareUploader）对 Level 2/3 同样需要，不是额外工作 |
| 投入产出比 | 4-5 天工作量，显著提升单节点部署的容灾能力 |
| 渐进路径 | 用户从 Level 1 升级到 Level 2 只需加 Redis + 多节点，代码无需变更 |
