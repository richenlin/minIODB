# MinIODB Dashboard 实施方案

**基于文档**：`docs/DASHBOARD_ARCHITECTURE.md` v1.2  
**编写日期**：2026-03-16  
**当前状态**：骨架已完成，核心功能待实现  

---

## 一、当前现状速查

### 1.1 已完成部分

| 层 | 文件 | 状态 |
|----|------|------|
| Go | `internal/dashboard/server.go` | ✅ 骨架（stub 路由，无真实数据） |
| Go | `internal/dashboard/embed.go` | ✅ |
| Go | `internal/dashboard/client/interface.go` | ⚠️ 空接口 `interface{}` |
| Go | `internal/dashboard/client/local.go` | ⚠️ 无方法实现 |
| Go | `internal/dashboard/client/remote.go` | ⚠️ 无方法实现 |
| Go | `cmd/dashboard/main.go` | ✅ 独立入口 |
| Go | `deploy/docker/Dockerfile` / `Dockerfile.arm` | ✅ 含 `-tags dashboard` |
| Go | `deploy/docker/Dockerfile.dashboard` | ✅ 独立镜像 |
| 前端 | 基础布局（Sidebar / Header / DashboardLayout） | ✅ |
| 前端 | 全部页面骨架（login / 总览 / 集群 / 节点 / 数据 / 日志 / 备份 / 监控 / 分析 / 设置） | ✅ 骨架，无完整功能 |

### 1.2 核心 service 方法可用情况

以下方法在 `internal/service/miniodb_service.go` 中**已存在**，可直接调用：

```
WriteData / QueryData / UpdateData / DeleteData
ListTables / GetTable / CreateTable / DeleteTable
GetStatus / GetMetrics
BackupMetadata / ListBackups
```

以下方法在 `internal/discovery/service_registry.go` 中**已存在**：

```
GetHealthyNodes() / DiscoverNodes() / GetNodeInfo()
IsNodeHealthy() / GetHashRing()
```

以下方法**尚不存在**，需新增：

```
FullBackup / FullRestore / BackupTable / RestoreTable
ConvertToDistributed
```

---

## 二、实施阶段总览

| 阶段 | 名称 | 预估工时 | 优先级 |
|------|------|---------|--------|
| Phase 0 | 认证强化（bcrypt + JWT） | 0.5d | P0 |
| Phase 1 | CoreClient 接口实现 | 2d | P0 |
| Phase 2 | Dashboard 后端重构（model/service/handler/SSE） | 5d | P0 |
| Phase 3 | 前端 API 层 + Hooks + Store | 1d | P1 |
| Phase 4 | 前端核心页面完善（总览/集群/节点/监控 SSE） | 3d | P1 |
| Phase 5 | 数据页：表管理 CRUD + SQL 控制台 | 3d | P1 |
| Phase 6 | 运维页：日志 SSE + 备份完整流程 | 3d | P2 |
| Phase 7 | 分析页：SQL 编辑器 + 趋势图 | 2d | P2 |
| Phase 8 | 扩展：单节点转分布式 + hash-password 工具 | 1.5d | P3 |
| Phase 9 | Docker 集成测试 | 1d | P3 |

**总计**：约 22 人天

---

## 三、Phase 0：认证强化

### 目标
将当前明文字符串对比改为 bcrypt 验证，并返回真实 JWT Token（含用户信息）。

### 3.1 修改 `internal/dashboard/server.go`

**当前问题**：
- `validateCredentials` 做明文 string 对比
- `login` 返回 `{token: api_key, expires_at}` — token 就是 api_key 本身
- `isValidToken` 也是 string 匹配

**修改方案**：

```go
// 在 Server 中持有 AuthManager
type Server struct {
    client      client.CoreClient
    cfg         *config.Config
    logger      *zap.Logger
    authManager *security.AuthManager  // 新增
}

// NewServer 时初始化 AuthManager
func NewServer(coreClient client.CoreClient, cfg *config.Config, logger *zap.Logger) *Server {
    authCfg := &security.AuthConfig{
        Mode:            cfg.Security.Mode,
        JWTSecret:       cfg.Security.JWTSecret,
        TokenExpiration: cfg.Security.TokenExpiration,
        APIKeyPairs:     buildSecurityKeyPairs(cfg),
    }
    am, _ := security.NewAuthManager(authCfg)
    return &Server{client: coreClient, cfg: cfg, logger: logger, authManager: am}
}
```

**login handler 改造**：

```go
func (s *Server) login(c *gin.Context) {
    var req struct {
        APIKey    string `json:"api_key" binding:"required"`
        APISecret string `json:"api_secret"`
    }
    c.ShouldBindJSON(&req)

    // 使用 bcrypt 验证
    matched, role, _ := s.authManager.ValidateCredentials(req.APIKey, req.APISecret)
    if !matched {
        c.JSON(401, gin.H{"error": "Invalid credentials"})
        return
    }

    // 生成 JWT
    token, _ := s.authManager.GenerateToken(req.APIKey, req.APIKey)
    expiresAt := time.Now().Add(s.authManager.TokenExpiration())

    c.JSON(200, gin.H{
        "token": token,
        "expires_at": expiresAt.Format(time.RFC3339),
        "user": gin.H{
            "key":          req.APIKey,
            "role":         role,
            "display_name": s.getDisplayName(req.APIKey),
        },
    })
}
```

**requireAuth 改造**：使用 `s.authManager.ValidateToken(token)` 替代字符串匹配。

### 3.2 新增 auth 路由

```go
api.POST("/auth/logout",          s.logout)
api.GET("/auth/me",               s.requireAuth, s.getMe)
api.POST("/auth/change-password", s.requireAuth, s.changePassword)
```

### 3.3 前端 `stores/auth-store.ts` 扩展

```typescript
interface AuthState {
    token: string | null
    user: { key: string; role: string; display_name: string } | null
    isAuthenticated: boolean
    setAuth: (token: string, user: AuthUser) => void
    logout: () => void
}
```

---

## 四、Phase 1：CoreClient 接口实现

### 4.1 完整接口定义

**文件**：`internal/dashboard/client/interface.go`

```go
//go:build dashboard

package client

import "context"

type CoreClient interface {
    // 集群与系统
    GetHealth(ctx context.Context) (*HealthResult, error)
    GetStatus(ctx context.Context) (*StatusResult, error)
    GetMetrics(ctx context.Context) (*MetricsResult, error)
    GetNodes(ctx context.Context) ([]*NodeResult, error)
    GetClusterConfig(ctx context.Context) (map[string]interface{}, error)

    // 表管理
    ListTables(ctx context.Context) ([]*TableResult, error)
    GetTable(ctx context.Context, name string) (*TableDetailResult, error)
    CreateTable(ctx context.Context, req *CreateTableRequest) error
    UpdateTable(ctx context.Context, name string, req *UpdateTableRequest) error
    DeleteTable(ctx context.Context, name string) error
    GetTableStats(ctx context.Context, name string) (*TableStatsResult, error)

    // 数据 CRUD
    BrowseData(ctx context.Context, table string, params *BrowseParams) (*BrowseResult, error)
    WriteRecord(ctx context.Context, table string, req *WriteRecordRequest) error
    WriteBatch(ctx context.Context, table string, records []*WriteRecordRequest) error
    UpdateRecord(ctx context.Context, table, id string, req *UpdateRecordRequest) error
    DeleteRecord(ctx context.Context, table, id, day string) error
    QuerySQL(ctx context.Context, sql string) (*QueryResult, error)

    // 备份
    ListBackups(ctx context.Context) ([]*BackupResult, error)
    TriggerMetadataBackup(ctx context.Context) (*BackupResult, error)
    TriggerFullBackup(ctx context.Context) (*BackupResult, error)
    TriggerTableBackup(ctx context.Context, tableName string) (*BackupResult, error)
    GetBackup(ctx context.Context, id string) (*BackupResult, error)
    RestoreBackup(ctx context.Context, id string, opts *RestoreOptions) error
    DeleteBackup(ctx context.Context, id string) error
    GetMetadataStatus(ctx context.Context) (*MetadataStatusResult, error)

    // 日志
    QueryLogs(ctx context.Context, params *LogQueryParams) (*LogQueryResult, error)
    ListLogFiles(ctx context.Context) ([]string, error)

    // 监控
    GetMonitorOverview(ctx context.Context) (*MonitorOverviewResult, error)
    GetSLA(ctx context.Context) (*SLAResult, error)
    ScrapePrometheus(ctx context.Context) ([]byte, error)

    // 分析
    AnalyticsQuery(ctx context.Context, sql string) (*AnalyticsQueryResult, error)
    GetAnalyticsOverview(ctx context.Context) (*AnalyticsOverviewResult, error)
}
```

### 4.2 结果类型（`internal/dashboard/model/types.go`）

**文件**：`internal/dashboard/model/types.go`（新建）

```go
//go:build dashboard

package model

import "time"

// ---------- 集群 ----------

type HealthResult struct {
    Status    string    `json:"status"`
    Timestamp time.Time `json:"timestamp"`
    Version   string    `json:"version"`
    NodeID    string    `json:"node_id"`
    Mode      string    `json:"mode"` // "single" | "distributed"
}

type StatusResult struct {
    Version     string `json:"version"`
    NodeID      string `json:"node_id"`
    Mode        string `json:"mode"`
    Tables      int    `json:"tables"`
    Goroutines  int    `json:"goroutines"`
    MemoryUsage int64  `json:"memory_usage"`
    Uptime      int64  `json:"uptime"`
}

type MetricsResult struct {
    Timestamp       int64              `json:"timestamp"`
    HTTPRequests    map[string]int64   `json:"http_requests"`
    QueryDuration   map[string]float64 `json:"query_duration"`
    BufferSize      int64              `json:"buffer_size"`
    MinioOps        map[string]int64   `json:"minio_ops"`
    RedisOps        map[string]int64   `json:"redis_ops"`
    Goroutines      int64              `json:"goroutines"`
    MemAlloc        int64              `json:"mem_alloc"`
    GCPauseNs       int64              `json:"gc_pause_ns"`
}

type NodeResult struct {
    ID       string            `json:"id"`
    Address  string            `json:"address"`
    Port     string            `json:"port"`
    Status   string            `json:"status"`
    LastSeen time.Time         `json:"last_seen"`
    Metadata map[string]string `json:"metadata"`
}

// ---------- 表 ----------

type TableResult struct {
    Name        string `json:"name"`
    RowCountEst int64  `json:"row_count_est"`
    SizeBytes   int64  `json:"size_bytes"`
    CreatedAt   int64  `json:"created_at"`
}

type TableDetailResult struct {
    TableResult
    Config       map[string]interface{} `json:"config"`
    BufferSize   int                    `json:"buffer_size"`
    FlushInterval string               `json:"flush_interval"`
    RetentionDays int                  `json:"retention_days"`
    BackupEnabled bool                 `json:"backup_enabled"`
    IDStrategy    string               `json:"id_strategy"`
}

type TableStatsResult struct {
    Name    string                   `json:"name"`
    Columns []ColumnStats            `json:"columns"`
}

type ColumnStats struct {
    Name      string      `json:"name"`
    Type      string      `json:"type"`
    Min       interface{} `json:"min"`
    Max       interface{} `json:"max"`
    Distinct  int64       `json:"distinct"`
    NullRate  float64     `json:"null_rate"`
}

// ---------- 数据 ----------

type BrowseParams struct {
    Page      int    `form:"page"`
    PageSize  int    `form:"page_size"`
    SortBy    string `form:"sort_by"`
    SortOrder string `form:"sort_order"`
    Filter    string `form:"filter"`
    ID        string `form:"id"`
}

type BrowseResult struct {
    Rows     []map[string]interface{} `json:"rows"`
    Total    int64                    `json:"total"`
    Page     int                      `json:"page"`
    PageSize int                      `json:"page_size"`
}

type WriteRecordRequest struct {
    ID        string                 `json:"id"`
    Timestamp string                 `json:"timestamp"`
    Payload   map[string]interface{} `json:"payload"`
}

type UpdateRecordRequest struct {
    Timestamp string                 `json:"timestamp"`
    Payload   map[string]interface{} `json:"payload"`
}

type QueryResult struct {
    Columns    []string                 `json:"columns"`
    Rows       []map[string]interface{} `json:"rows"`
    Total      int64                    `json:"total"`
    DurationMs int64                    `json:"duration_ms"`
}

// ---------- 备份 ----------

type BackupResult struct {
    ID          string    `json:"id"`
    Type        string    `json:"type"` // "metadata" | "full" | "table"
    TableName   string    `json:"table_name,omitempty"`
    Status      string    `json:"status"` // "pending" | "running" | "done" | "failed"
    SizeBytes   int64     `json:"size_bytes"`
    CreatedAt   time.Time `json:"created_at"`
    CompletedAt time.Time `json:"completed_at,omitempty"`
    Tables      []string  `json:"tables,omitempty"`
}

type RestoreOptions struct {
    Tables      []string `json:"tables,omitempty"`
    TargetTable string   `json:"target_table,omitempty"`
}

type MetadataStatusResult struct {
    LastBackup    string `json:"last_backup"`
    TablesCount   int    `json:"tables_count"`
    BackupEnabled bool   `json:"backup_enabled"`
    BackupInterval string `json:"backup_interval"`
}

// ---------- 日志 ----------

type LogQueryParams struct {
    Level     string `form:"level"`
    StartTime string `form:"start_time"`
    EndTime   string `form:"end_time"`
    Keyword   string `form:"keyword"`
    Page      int    `form:"page"`
    PageSize  int    `form:"page_size"`
}

type LogEntry struct {
    Level     string                 `json:"level"`
    Timestamp string                 `json:"timestamp"`
    Message   string                 `json:"message"`
    Fields    map[string]interface{} `json:"fields,omitempty"`
}

type LogQueryResult struct {
    Logs     []*LogEntry `json:"logs"`
    Total    int64       `json:"total"`
    Page     int         `json:"page"`
    PageSize int         `json:"page_size"`
}

// ---------- 监控 ----------

type MonitorOverviewResult struct {
    Goroutines  int64   `json:"goroutines"`
    MemAllocMB  float64 `json:"mem_alloc_mb"`
    GCPauseMs   float64 `json:"gc_pause_ms"`
    UptimeHours float64 `json:"uptime_hours"`
    CPUPercent  float64 `json:"cpu_percent"`
}

type SLAResult struct {
    QueryLatencyP50Ms float64 `json:"query_latency_p50_ms"`
    QueryLatencyP95Ms float64 `json:"query_latency_p95_ms"`
    QueryLatencyP99Ms float64 `json:"query_latency_p99_ms"`
    WriteLatencyP95Ms float64 `json:"write_latency_p95_ms"`
    CacheHitRate      float64 `json:"cache_hit_rate"`
    FilePruneRate     float64 `json:"file_prune_rate"`
    ErrorRate         float64 `json:"error_rate"`
}

// ---------- 分析 ----------

type AnalyticsQueryResult struct {
    Columns    []string                 `json:"columns"`
    Rows       []map[string]interface{} `json:"rows"`
    Total      int64                    `json:"total"`
    DurationMs int64                    `json:"duration_ms"`
    ExplainPlan string                  `json:"explain_plan,omitempty"`
}

type AnalyticsOverviewResult struct {
    WriteTrend []TimeSeriesPoint `json:"write_trend"`
    QueryTrend []TimeSeriesPoint `json:"query_trend"`
    DataVolume []TimeSeriesPoint `json:"data_volume"`
}

type TimeSeriesPoint struct {
    Timestamp int64   `json:"timestamp"`
    Value     float64 `json:"value"`
}

// ---------- 请求 ----------

type CreateTableRequest struct {
    TableName string                 `json:"table_name" binding:"required"`
    Config    map[string]interface{} `json:"config"`
}

type UpdateTableRequest struct {
    Config map[string]interface{} `json:"config" binding:"required"`
}
```

### 4.3 LocalClient 实现

**文件**：`internal/dashboard/client/local.go`（全量重写）

```go
//go:build dashboard

package client

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "time"

    miniodb "minIODB/api/proto/miniodb/v1"
    "minIODB/config"
    "minIODB/internal/discovery"
    "minIODB/internal/metadata"
    "minIODB/internal/service"
    "minIODB/pkg/pool"
    . "minIODB/internal/dashboard/model"

    "go.uber.org/zap"
)

type LocalClient struct {
    svc    *service.MinIODBService
    mm     *metadata.Manager
    sr     *discovery.ServiceRegistry
    rp     *pool.RedisPool
    cfg    *config.Config
    logger *zap.Logger
}

func NewLocalClient(svc *service.MinIODBService, mm *metadata.Manager,
    sr *discovery.ServiceRegistry, rp *pool.RedisPool,
    cfg *config.Config, logger *zap.Logger) CoreClient {
    return &LocalClient{svc: svc, mm: mm, sr: sr, rp: rp, cfg: cfg, logger: logger}
}

// GetHealth 调用健康状态
func (c *LocalClient) GetHealth(ctx context.Context) (*HealthResult, error) {
    // 通过 GetStatus 推断健康状态
    resp, err := c.svc.GetStatus(ctx, &miniodb.GetStatusRequest{})
    if err != nil {
        return &HealthResult{Status: "unhealthy"}, nil
    }
    mode := "single"
    if resp.TotalNodes > 1 {
        mode = "distributed"
    }
    return &HealthResult{
        Status:    "healthy",
        Timestamp: time.Now(),
        Version:   "1.0.0",
        NodeID:    c.cfg.Server.NodeID,
        Mode:      mode,
    }, nil
}

// GetStatus 获取系统状态
func (c *LocalClient) GetStatus(ctx context.Context) (*StatusResult, error) {
    resp, err := c.svc.GetStatus(ctx, &miniodb.GetStatusRequest{})
    if err != nil {
        return nil, err
    }
    var ms runtime.MemStats
    runtime.ReadMemStats(&ms)
    return &StatusResult{
        Version:     "1.0.0",
        NodeID:      c.cfg.Server.NodeID,
        Mode:        modeFromNodes(int(resp.TotalNodes)),
        Tables:      int(resp.TotalNodes),
        Goroutines:  runtime.NumGoroutine(),
        MemoryUsage: int64(ms.Alloc),
    }, nil
}

// GetMetrics 获取指标
func (c *LocalClient) GetMetrics(ctx context.Context) (*MetricsResult, error) {
    resp, err := c.svc.GetMetrics(ctx, &miniodb.GetMetricsRequest{})
    if err != nil {
        return nil, err
    }
    var ms runtime.MemStats
    runtime.ReadMemStats(&ms)
    result := &MetricsResult{
        Timestamp:  time.Now().UnixMilli(),
        Goroutines: int64(runtime.NumGoroutine()),
        MemAlloc:   int64(ms.Alloc),
        GCPauseNs:  int64(ms.PauseNs[(ms.NumGC+255)%256]),
    }
    if resp.PerformanceMetrics != nil {
        // 将 proto map 映射到结果
        result.HTTPRequests = int64MapFromProto(resp.PerformanceMetrics)
    }
    return result, nil
}

// GetNodes 获取节点列表
func (c *LocalClient) GetNodes(ctx context.Context) ([]*NodeResult, error) {
    if c.sr == nil {
        // 单节点模式：返回自身
        return []*NodeResult{{
            ID:       c.cfg.Server.NodeID,
            Address:  "localhost",
            Port:     c.cfg.Server.GrpcPort,
            Status:   "online",
            LastSeen: time.Now(),
        }}, nil
    }
    nodes, err := c.sr.GetHealthyNodes()
    if err != nil {
        return nil, err
    }
    result := make([]*NodeResult, 0, len(nodes))
    for _, n := range nodes {
        result = append(result, &NodeResult{
            ID:       n.ID,
            Address:  n.Address,
            Port:     n.Port,
            Status:   n.Status,
            LastSeen: n.LastSeen,
        })
    }
    return result, nil
}

// ListTables 获取表列表
func (c *LocalClient) ListTables(ctx context.Context) ([]*TableResult, error) {
    resp, err := c.svc.ListTables(ctx, &miniodb.ListTablesRequest{})
    if err != nil {
        return nil, err
    }
    result := make([]*TableResult, 0, len(resp.Tables))
    for _, t := range resp.Tables {
        result = append(result, &TableResult{Name: t.TableName})
    }
    return result, nil
}

// QuerySQL 执行 SQL 查询
func (c *LocalClient) QuerySQL(ctx context.Context, sql string) (*QueryResult, error) {
    start := time.Now()
    resp, err := c.svc.QueryData(ctx, &miniodb.QueryDataRequest{Sql: sql})
    if err != nil {
        return nil, err
    }
    duration := time.Since(start).Milliseconds()
    rows := make([]map[string]interface{}, 0, len(resp.Rows))
    for _, row := range resp.Rows {
        m := make(map[string]interface{})
        if err := json.Unmarshal([]byte(row), &m); err == nil {
            rows = append(rows, m)
        }
    }
    return &QueryResult{
        Rows:       rows,
        Total:      int64(len(rows)),
        DurationMs: duration,
    }, nil
}

// TriggerMetadataBackup 触发元数据备份
func (c *LocalClient) TriggerMetadataBackup(ctx context.Context) (*BackupResult, error) {
    resp, err := c.svc.BackupMetadata(ctx, &miniodb.BackupMetadataRequest{})
    if err != nil {
        return nil, err
    }
    return &BackupResult{
        ID:        resp.BackupId,
        Type:      "metadata",
        Status:    "done",
        CreatedAt: time.Now(),
    }, nil
}

// ListBackups 获取备份列表
func (c *LocalClient) ListBackups(ctx context.Context) ([]*BackupResult, error) {
    resp, err := c.svc.ListBackups(ctx, &miniodb.ListBackupsRequest{})
    if err != nil {
        return nil, err
    }
    result := make([]*BackupResult, 0, len(resp.Backups))
    for _, b := range resp.Backups {
        result = append(result, &BackupResult{
            ID:     b.BackupId,
            Type:   "metadata",
            Status: "done",
        })
    }
    return result, nil
}

// QueryLogs 从日志文件读取并过滤
func (c *LocalClient) QueryLogs(ctx context.Context, params *LogQueryParams) (*LogQueryResult, error) {
    logDir := c.cfg.Dashboard.LogDir
    if logDir == "" {
        logDir = filepath.Dir(c.cfg.Log.Filename)
    }
    entries, err := readLogsFromDir(logDir, params)
    if err != nil {
        return &LogQueryResult{Logs: []*LogEntry{}, Total: 0, Page: 1, PageSize: 50}, nil
    }
    return entries, nil
}

// GetMonitorOverview 系统资源概览
func (c *LocalClient) GetMonitorOverview(ctx context.Context) (*MonitorOverviewResult, error) {
    var ms runtime.MemStats
    runtime.ReadMemStats(&ms)
    return &MonitorOverviewResult{
        Goroutines: int64(runtime.NumGoroutine()),
        MemAllocMB: float64(ms.Alloc) / 1024 / 1024,
        GCPauseMs:  float64(ms.PauseNs[(ms.NumGC+255)%256]) / 1e6,
    }, nil
}

// ScrapePrometheus 抓取 Prometheus 指标
func (c *LocalClient) ScrapePrometheus(ctx context.Context) ([]byte, error) {
    endpoint := fmt.Sprintf("http://localhost%s/metrics", c.cfg.Metrics.Port)
    // 实际使用 http.Get 抓取，此处返回占位
    _ = endpoint
    return []byte("# Prometheus metrics"), nil
}

// AnalyticsQuery 执行分析 SQL
func (c *LocalClient) AnalyticsQuery(ctx context.Context, sql string) (*AnalyticsQueryResult, error) {
    qr, err := c.QuerySQL(ctx, sql)
    if err != nil {
        return nil, err
    }
    return &AnalyticsQueryResult{
        Columns:    qr.Columns,
        Rows:       qr.Rows,
        Total:      qr.Total,
        DurationMs: qr.DurationMs,
    }, nil
}

// --- 其他接口方法（CreateTable / DeleteTable / WriteRecord / ... 类似模式） ---
// 篇幅略，实现方式参照上述模式：调用 c.svc.XxxMethod(ctx, protoReq)
```

### 4.4 RemoteClient 实现

**文件**：`internal/dashboard/client/remote.go`（全量重写）

```go
// RemoteClient 通过 HTTP 调用 miniodb REST API（独立部署模式）
// 每个方法构造 HTTP 请求，发送到 cfg.Dashboard.CoreEndpoint/v1/...
// Token 通过 remoteToken 字段缓存（调用 /v1/auth/token 获得）

type RemoteClient struct {
    httpClient  *http.Client
    cfg         *config.Config
    logger      *zap.Logger
    remoteToken string
    tokenExpiry time.Time
    mu          sync.Mutex
}

func (c *RemoteClient) ensureToken(ctx context.Context) error { ... }
func (c *RemoteClient) doGet(ctx, path string, result interface{}) error { ... }
func (c *RemoteClient) doPost(ctx, path string, body, result interface{}) error { ... }
// 各方法体直接调用 doGet/doPost 映射到 miniodb REST 端点
```

---

## 五、Phase 2：Dashboard 后端重构

### 5.1 SSE Hub

**文件**：`internal/dashboard/sse/hub.go`（新建）

```go
//go:build dashboard

package sse

import (
    "encoding/json"
    "sync"
    "time"
)

type Event struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
    Time int64       `json:"time"`
}

// Hub 管理 SSE 客户端订阅
type Hub struct {
    clients    map[string]map[chan Event]struct{}
    mu         sync.RWMutex
    register   chan subscription
    unregister chan subscription
    done       chan struct{}
}

type subscription struct {
    topic string
    ch    chan Event
}

func NewHub() *Hub {
    h := &Hub{
        clients:    make(map[string]map[chan Event]struct{}),
        register:   make(chan subscription, 16),
        unregister: make(chan subscription, 16),
        done:       make(chan struct{}),
    }
    go h.run()
    return h
}

func (h *Hub) Subscribe(topic string) chan Event {
    ch := make(chan Event, 32)
    h.register <- subscription{topic, ch}
    return ch
}

func (h *Hub) Unsubscribe(topic string, ch chan Event) {
    h.unregister <- subscription{topic, ch}
}

func (h *Hub) Publish(topic string, data interface{}) {
    event := Event{Type: topic, Data: data, Time: time.Now().UnixMilli()}
    h.mu.RLock()
    defer h.mu.RUnlock()
    for ch := range h.clients[topic] {
        select {
        case ch <- event:
        default: // 丢弃溢出事件，避免阻塞
        }
    }
}

func (h *Hub) run() { ... }

// ServeSSE 将 Hub 的事件推送到 Gin ResponseWriter
func ServeSSE(c *gin.Context, hub *Hub, topics []string) {
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")

    channels := make([]chan Event, len(topics))
    for i, t := range topics { channels[i] = hub.Subscribe(t) }
    defer func() {
        for i, t := range topics { hub.Unsubscribe(t, channels[i]) }
    }()

    ctx := c.Request.Context()
    for {
        select {
        case <-ctx.Done():
            return
        case ev := <-mergeChannels(channels...):
            data, _ := json.Marshal(ev)
            fmt.Fprintf(c.Writer, "data: %s\n\n", data)
            c.Writer.Flush()
        }
    }
}
```

### 5.2 Server 重构

**文件**：`internal/dashboard/server.go`（全量重写）

核心变化：
- `Server` 增加 `hub *sse.Hub` 和 `authManager *security.AuthManager` 字段
- `MountRoutes` 按模块拆分注册：

```go
func (s *Server) MountRoutes(group *gin.RouterGroup) {
    // 静态文件
    staticSub, _ := fs.Sub(staticFS, "static")
    group.StaticFS("/ui", http.FS(staticSub))

    api := group.Group("/api/v1")
    api.GET("/health", s.health)
    api.POST("/auth/login", s.login)

    auth := api.Group("")
    auth.Use(s.requireAuth)
    {
        auth.POST("/auth/logout",          s.logout)
        auth.GET("/auth/me",               s.getMe)
        auth.POST("/auth/change-password", s.changePassword)

        // 集群
        auth.GET("/cluster/info",                   s.clusterInfo)
        auth.GET("/cluster/topology",               s.clusterTopology)
        auth.POST("/cluster/convert-to-distributed",s.convertToDistributed)
        auth.GET("/cluster/config",                 s.clusterConfig)

        // 节点
        auth.GET("/nodes",                 s.listNodes)
        auth.GET("/nodes/:id",             s.getNode)
        auth.GET("/nodes/:id/metrics",     s.nodeMetrics)
        auth.POST("/nodes/scale-check",    s.scaleCheck)

        // 表
        auth.GET("/tables",                s.listTables)
        auth.GET("/tables/:name",          s.getTable)
        auth.POST("/tables",               s.createTable)
        auth.PUT("/tables/:name",          s.updateTable)
        auth.DELETE("/tables/:name",       s.deleteTable)
        auth.GET("/tables/:name/stats",    s.tableStats)

        // 数据 CRUD
        auth.GET("/tables/:name/data",              s.browseData)
        auth.POST("/tables/:name/data",             s.writeRecord)
        auth.POST("/tables/:name/data/batch",       s.writeBatch)
        auth.PUT("/tables/:name/data/:id",          s.updateRecord)
        auth.DELETE("/tables/:name/data/:id",       s.deleteRecord)
        auth.POST("/query",                         s.querySQL)
        auth.GET("/query/history",                  s.queryHistory)

        // 日志
        auth.GET("/logs",                           s.queryLogs)
        auth.GET("/logs/stream",                    s.streamLogs)    // SSE
        auth.GET("/logs/files",                     s.listLogFiles)
        auth.GET("/logs/files/:name/download",      s.downloadLogFile)

        // 备份
        auth.GET("/backups",                        s.listBackups)
        auth.POST("/backups/metadata",              s.triggerMetadataBackup)
        auth.POST("/backups/full",                  s.triggerFullBackup)
        auth.POST("/backups/table/:name",           s.triggerTableBackup)
        auth.GET("/backups/:id",                    s.getBackup)
        auth.POST("/backups/:id/restore",           s.restoreBackup)
        auth.DELETE("/backups/:id",                 s.deleteBackup)
        auth.GET("/backups/:id/download",           s.downloadBackup)
        auth.POST("/backups/validate/:id",          s.validateBackup)
        auth.GET("/backups/schedule",               s.getBackupSchedule)
        auth.PUT("/backups/schedule",               s.updateBackupSchedule)

        // 监控
        auth.GET("/monitor/overview",               s.monitorOverview)
        auth.GET("/monitor/stream",                 s.monitorStream)  // SSE
        auth.GET("/monitor/prometheus/query",       s.prometheusQuery)
        auth.GET("/monitor/sla",                    s.slaMetrics)
        auth.GET("/monitor/alerts",                 s.alerts)

        // 分析
        auth.POST("/analytics/query",              s.analyticsQuery)
        auth.GET("/analytics/suggestions",         s.analyticsSuggestions)
        auth.GET("/analytics/overview",            s.analyticsOverview)
    }
}
```

### 5.3 监控 SSE 后台推送

Server 启动时开启 goroutine，每 5s 抓取指标并推送到 Hub：

```go
func (s *Server) startMetricsPush(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                overview, err := s.client.GetMonitorOverview(ctx)
                if err == nil {
                    s.hub.Publish("metrics", overview)
                }
            }
        }
    }()
}
```

---

## 六、Phase 3：前端 API 层 + Hooks + Store

### 6.1 API 模块拆分

新建以下文件（调用 `apiClient.get/post`）：

**`lib/api/cluster.ts`**：

```typescript
import { apiClient } from './client'

export const clusterApi = {
    getInfo:    () => apiClient.get<ClusterInfo>('/cluster/info'),
    getTopology:() => apiClient.get<TopologyData>('/cluster/topology'),
    getConfig:  () => apiClient.get<Record<string,unknown>>('/cluster/config'),
    convertToDistributed: (body: ConvertRequest) =>
        apiClient.post<void>('/cluster/convert-to-distributed', body),
}
```

同理创建：`nodes.ts` / `data.ts` / `logs.ts` / `backup.ts` / `monitor.ts` / `analytics.ts`

### 6.2 `hooks/use-sse.ts`

```typescript
import { useEffect, useRef, useState } from 'react'

export function useSSE<T>(url: string, enabled = true) {
    const [data, setData] = useState<T | null>(null)
    const [error, setError] = useState<string | null>(null)
    const [connected, setConnected] = useState(false)
    const esRef = useRef<EventSource | null>(null)

    useEffect(() => {
        if (!enabled) return
        const token = localStorage.getItem('miniodb-auth')
            ? JSON.parse(localStorage.getItem('miniodb-auth')!).state.token
            : null
        const fullUrl = `/dashboard/api/v1${url}${token ? `?token=${token}` : ''}`
        const es = new EventSource(fullUrl)
        esRef.current = es

        es.onopen = () => setConnected(true)
        es.onerror = () => { setError('连接断开'); setConnected(false) }
        es.onmessage = (e) => {
            try { setData(JSON.parse(e.data)) } catch {}
        }
        return () => { es.close(); setConnected(false) }
    }, [url, enabled])

    return { data, error, connected }
}
```

### 6.3 `hooks/use-query.ts`

```typescript
export function useQuery<T>(
    fetcher: () => Promise<T>,
    deps: unknown[] = []
) {
    const [data, setData] = useState<T | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)

    const refetch = useCallback(async () => {
        setLoading(true)
        setError(null)
        try { setData(await fetcher()) }
        catch (e) { setError(e instanceof Error ? e.message : '请求失败') }
        finally { setLoading(false) }
    }, deps)

    useEffect(() => { refetch() }, [refetch])
    return { data, loading, error, refetch }
}
```

### 6.4 `stores/cluster-store.ts`

```typescript
interface ClusterState {
    nodes: NodeInfo[]
    clusterInfo: ClusterInfo | null
    fetchCluster: () => Promise<void>
}

export const useClusterStore = create<ClusterState>((set) => ({
    nodes: [],
    clusterInfo: null,
    fetchCluster: async () => {
        const [info, nodes] = await Promise.all([
            clusterApi.getInfo(), nodesApi.list()
        ])
        set({ clusterInfo: info, nodes })
    },
}))
```

---

## 七、Phase 4：前端核心页面完善

### 7.1 总览页（`app/page.tsx`）完善

新增内容：
- 集群健康徽章（绿色 healthy / 红色 degraded）
- 最近 5min 写入速率迷你折线图（ECharts mini）
- 最近 5min 查询速率迷你折线图

需安装（已在 package.json）：`echarts` + `echarts-for-react`

**迷你图组件** `components/charts/mini-line.tsx`：

```tsx
'use client'
import ReactECharts from 'echarts-for-react'

interface MiniLineProps {
    data: number[]
    color?: string
}

export function MiniLine({ data, color = '#6366f1' }: MiniLineProps) {
    const option = {
        grid: { left: 0, right: 0, top: 4, bottom: 0 },
        xAxis: { show: false, type: 'category' },
        yAxis: { show: false, type: 'value' },
        series: [{
            type: 'line',
            data,
            smooth: true,
            symbol: 'none',
            lineStyle: { color, width: 2 },
            areaStyle: { color, opacity: 0.1 },
        }],
    }
    return <ReactECharts option={option} style={{ height: 48, width: '100%' }} />
}
```

### 7.2 监控页（`app/monitor/page.tsx`）SSE 实时图

**实时折线图组件** `components/charts/realtime-chart.tsx`：

```tsx
'use client'
import { useEffect, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { useSSE } from '@/hooks/use-sse'

interface RealtimeChartProps {
    title: string
    valueKey: keyof MonitorOverview
    unit?: string
    color?: string
}

export function RealtimeChart({ title, valueKey, unit = '', color }: RealtimeChartProps) {
    const { data } = useSSE<{ type: string; data: MonitorOverview }>('/monitor/stream')
    const [history, setHistory] = useState<number[]>(Array(30).fill(0))
    const [labels, setLabels] = useState<string[]>(Array(30).fill(''))

    useEffect(() => {
        if (!data?.data) return
        const value = Number(data.data[valueKey] ?? 0)
        setHistory(prev => [...prev.slice(1), value])
        setLabels(prev => [...prev.slice(1), new Date().toLocaleTimeString('zh-CN')])
    }, [data, valueKey])

    const option = {
        title: { text: title, textStyle: { fontSize: 13 } },
        grid: { left: 48, right: 8, top: 36, bottom: 24 },
        xAxis: { type: 'category', data: labels, axisLabel: { fontSize: 10 } },
        yAxis: { type: 'value', axisLabel: { formatter: `{value}${unit}` } },
        series: [{
            type: 'line',
            data: history,
            smooth: true,
            symbol: 'none',
            lineStyle: { color: color ?? '#6366f1', width: 2 },
            areaStyle: { opacity: 0.1 },
        }],
        tooltip: { trigger: 'axis' },
    }
    return <ReactECharts option={option} style={{ height: 200 }} />
}
```

监控页使用 4 个 `RealtimeChart`：goroutines / mem_alloc_mb / gc_pause_ms / 自定义 QPS。

### 7.3 集群拓扑图（`app/cluster/page.tsx`）

```tsx
// 使用 ECharts Graph 图（force-directed）展示节点拓扑
const topologyOption = {
    series: [{
        type: 'graph',
        layout: 'force',
        data: nodes.map(n => ({
            name: n.id, symbolSize: 40,
            itemStyle: { color: n.status === 'online' ? '#22c55e' : '#ef4444' }
        })),
        edges: [],  // 单节点无边，分布式有边连接
        force: { repulsion: 100 },
        label: { show: true },
    }]
}
```

---

## 八、Phase 5：数据页完善

### 8.1 表管理侧边栏

```
数据页布局：
┌─────────────┬───────────────────────────────┐
│  表列表      │  右侧内容区                     │
│  [+ 创建表]  │  数据网格 / SQL 控制台          │
│  > orders   │                               │
│  > users    │  [切换: 数据 | SQL]            │
└─────────────┴───────────────────────────────┘
```

### 8.2 SQL 控制台（`components/code-editor/sql-editor.tsx`）

CodeMirror 6 安装：

```bash
npm install @codemirror/lang-sql @codemirror/state @codemirror/view @codemirror/basic-setup
```

```tsx
'use client'
import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, basicSetup } from '@codemirror/basic-setup'
import { sql } from '@codemirror/lang-sql'

interface SqlEditorProps {
    value: string
    onChange: (v: string) => void
    onExecute: () => void
}

export function SqlEditor({ value, onChange, onExecute }: SqlEditorProps) {
    const containerRef = useRef<HTMLDivElement>(null)
    const viewRef = useRef<EditorView | null>(null)

    useEffect(() => {
        if (!containerRef.current) return
        const state = EditorState.create({
            doc: value,
            extensions: [
                basicSetup,
                sql(),
                EditorView.updateListener.of((update) => {
                    if (update.docChanged) onChange(update.state.doc.toString())
                }),
                // Ctrl+Enter 执行
                EditorView.domEventHandlers({
                    keydown(e) {
                        if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                            e.preventDefault()
                            onExecute()
                        }
                    }
                })
            ]
        })
        viewRef.current = new EditorView({ state, parent: containerRef.current })
        return () => viewRef.current?.destroy()
    }, [])

    return <div ref={containerRef} className="border border-border rounded-md min-h-[120px]" />
}
```

### 8.3 数据网格组件（`components/data-grid.tsx`）

```
功能：
- 分页（PageSize 50/100/200）
- 列排序（点击列头）
- 新增行（+ 按钮打开表单 Dialog）
- 编辑行（行内编辑或 Dialog）
- 删除行（确认后调用 DELETE API）
- 导出（下载 JSON）
```

---

## 九、Phase 6：运维页完善

### 9.1 日志页 SSE 实时尾随

```tsx
const { data: logEvent, connected } = useSSE<LogEvent>('/logs/stream', isStreaming)

useEffect(() => {
    if (logEvent?.data) {
        setLogs(prev => [logEvent.data, ...prev].slice(0, 500))
    }
}, [logEvent])
```

日志级别过滤 + 时间范围选择器（使用 `dayjs`）。

### 9.2 备份完整流程

UI 流程：

```
备份列表页
├── [全量备份] → POST /backups/full → 刷新列表
├── [元数据备份] → POST /backups/metadata → 刷新列表
├── [表备份] → 弹出 Select 选择表 → POST /backups/table/:name
└── 每行备份项
    ├── [恢复] → 弹出恢复向导 Dialog
    │   ├── Step 1: 选择恢复模式（全量/选表）
    │   ├── Step 2: 选择目标表名（新表/覆盖）
    │   └── Step 3: 确认 → POST /backups/:id/restore
    ├── [下载] → GET /backups/:id/download
    ├── [验证] → POST /backups/validate/:id
    └── [删除] → 确认 → DELETE /backups/:id
```

---

## 十、Phase 7：分析页

### 10.1 SQL 编辑器布局

```
┌─────────────────────────────────────────────┐
│  SQL 编辑器（CodeMirror）          [执行 Ctrl+Enter] │
├─────────────────────────────────────────────┤
│  执行结果表格（含行数 / 耗时）                │
├─────────────────────────────────────────────┤
│  查询历史列表（点击可复用 SQL）               │
└─────────────────────────────────────────────┘
```

### 10.2 数据量趋势图

```tsx
// 调用 GET /analytics/overview
// 展示 WriteTrend / QueryTrend / DataVolume 三条折线
const option = {
    series: [
        { name: '写入量', type: 'line', data: overview.write_trend.map(p => p.value) },
        { name: '查询量', type: 'line', data: overview.query_trend.map(p => p.value) },
    ]
}
```

---

## 十一、Phase 8：扩展功能

### 11.1 单节点转分布式（`app/cluster/page.tsx`）

UI 流程：
1. 判断当前为单节点模式（`mode === 'single'`）时显示"升级为分布式"按钮
2. 弹出表单：输入 Redis 地址、密码、模式（standalone/sentinel/cluster）
3. 调用 `POST /cluster/convert-to-distributed`
4. 提示重启以完全生效

后端需在 `internal/service/miniodb_service.go` 新增：

```go
func (s *MinIODBService) ConvertToDistributed(ctx context.Context, cfg RedisConvertConfig) error {
    // 1. 验证当前为单节点
    // 2. 测试 Redis 连通性
    // 3. 初始化 Redis 连接池
    // 4. 注册节点到服务发现
    // 5. 更新运行配置 + 持久化到 config.yaml
}
```

### 11.2 `--hash-password` CLI 工具

在 `cmd/main.go` 的 `main()` 顶部：

```go
// 支持 ./miniodb --hash-password <明文密码>
if len(os.Args) == 3 && os.Args[1] == "--hash-password" {
    hash, err := bcrypt.GenerateFromPassword([]byte(os.Args[2]), bcrypt.DefaultCost)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    fmt.Println(string(hash))
    os.Exit(0)
}
```

---

## 十二、Phase 9：Docker 集成测试

### 12.1 测试矩阵

| 场景 | 命令 | 验证点 |
|------|------|--------|
| AMD64 allinone | `./deploy/deploy.sh dev --no-cache` | `http://localhost:9090/dashboard/ui/` 可登录 |
| ARM64 allinone | 同上（Apple Silicon） | 同上 |
| 独立 Dashboard | `docker build -f deploy/docker/Dockerfile.dashboard` | `http://localhost:9090/health` 返回 ok |
| 冷启动 | 首次拉取镜像 | 无 panic，服务健康 |

### 12.2 端到端冒烟测试

```bash
# 1. 健康检查
curl http://localhost:8081/v1/health

# 2. Dashboard 登录
curl -X POST http://localhost:9090/dashboard/api/v1/auth/login \
     -d '{"api_key":"api-key-1234567890abcdef","api_secret":""}'

# 3. 集群状态
TOKEN=<上步返回的 token>
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:9090/dashboard/api/v1/cluster/info

# 4. 表列表
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:9090/dashboard/api/v1/tables
```

---

## 十三、关键注意事项

### 13.1 静态导出约束
- **禁止** `getServerSideProps` / `generateStaticParams` 以外的服务端逻辑
- SSE 通过 `EventSource` 客户端发起，无需 API Routes
- 所有路由须在 `app/` 下有对应的 `page.tsx` 才会生成静态 HTML

### 13.2 bcrypt 密码迁移
当前 `config.yaml` 的 `api_keys` 是明文。新的 `api_key_pairs` 需 bcrypt 哈希：

```yaml
auth:
  api_key_pairs:
    - key: "admin"
      secret: "$2a$10$..."    # 用 ./miniodb --hash-password <密码> 生成
      display_name: "管理员"
```

迁移步骤：
1. 构建含 `--hash-password` 的 miniodb
2. 生成各账户密码哈希
3. 更新 `deploy/docker/config/config.yaml`
4. 重新部署

### 13.3 embed.FS 占位目录
`internal/dashboard/static/index.html` 在本地开发时是占位，Docker 构建时被 `npm run build` 的产物替换。开发时若需要本地 Go 编译通过，保持该占位文件存在。

### 13.4 日志读取
`QueryLogs` 读取 `cfg.Log.Filename` 所在目录的 Zap JSON 格式日志文件，逐行解析 `{"level":"info","ts":...,"msg":"..."}` 结构，支持按 level/时间/关键词过滤。日志文件可能被 lumberjack 轮转（`.log.1`, `.log.2`）。

### 13.5 构建加速
在 Dockerfile 中 `npm ci` 使用 `--registry=https://registry.npmmirror.com`，Go 编译使用 `GOPROXY=https://goproxy.cn,direct`，已配置。

---

## 十四、文件变更清单

### 新增文件

```
internal/dashboard/model/types.go
internal/dashboard/sse/hub.go
dashboard-ui/src/hooks/use-sse.ts
dashboard-ui/src/hooks/use-query.ts
dashboard-ui/src/stores/cluster-store.ts
dashboard-ui/src/lib/api/cluster.ts
dashboard-ui/src/lib/api/nodes.ts
dashboard-ui/src/lib/api/data.ts
dashboard-ui/src/lib/api/logs.ts
dashboard-ui/src/lib/api/backup.ts
dashboard-ui/src/lib/api/monitor.ts
dashboard-ui/src/lib/api/analytics.ts
dashboard-ui/src/components/charts/mini-line.tsx
dashboard-ui/src/components/charts/realtime-chart.tsx
dashboard-ui/src/components/charts/topology-graph.tsx
dashboard-ui/src/components/code-editor/sql-editor.tsx
dashboard-ui/src/components/data-grid.tsx
```

### 全量重写文件

```
internal/dashboard/server.go         — 增加完整路由、auth manager、SSE
internal/dashboard/client/interface.go  — 完整 15 方法接口
internal/dashboard/client/local.go   — 调用真实 service 方法
internal/dashboard/client/remote.go  — HTTP 转发到 miniodb REST
```

### 重大修改文件

```
dashboard-ui/src/app/page.tsx           — 迷你图、健康徽章
dashboard-ui/src/app/cluster/page.tsx   — 拓扑图、转分布式
dashboard-ui/src/app/nodes/page.tsx     — 详情抽屉、资源指标
dashboard-ui/src/app/data/page.tsx      — 表管理 + SQL 控制台 + 数据网格
dashboard-ui/src/app/logs/page.tsx      — SSE 尾随、过滤
dashboard-ui/src/app/backup/page.tsx    — 完整备份流程
dashboard-ui/src/app/monitor/page.tsx   — SSE 实时图表
dashboard-ui/src/app/analytics/page.tsx — SQL 编辑器、趋势图
dashboard-ui/src/stores/auth-store.ts   — 增加 user 字段
deploy/docker/config/config.yaml        — api_key_pairs + bcrypt
```

### 新增核心服务方法（`internal/service/miniodb_service.go`）

```go
ConvertToDistributed(ctx, RedisConvertConfig) error
FullBackup(ctx) (*BackupResult, error)
FullRestore(ctx, backupID string) error
BackupTable(ctx, tableName string) (*BackupResult, error)
RestoreTable(ctx, backupID, tableName, targetTable string) error
```

### 新增 REST 端点（`internal/transport/rest/server.go`）

```
GET  /v1/nodes          — 节点列表
GET  /v1/nodes/:id      — 节点详情
POST /v1/cluster/convert — 单节点转分布式
GET  /v1/logs/query     — 结构化日志查询
GET  /v1/tables/:name/stats — 列统计
```
