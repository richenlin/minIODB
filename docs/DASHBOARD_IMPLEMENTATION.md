# MinIODB Dashboard 详细实施方案

**基于架构设计 v1.2 制定 | 2026-03-13**  
**角色**：资深软件工程师  
**执行原则**：每个任务独立可执行，修改后通过 `go build ./...` 和 `go test ./...`；前端修改通过 `npm run build` 验证

---

## 执行约束

1. **不破坏现有 API 契约**：REST/gRPC 接口的请求/响应结构不变
2. **不引入不必要的依赖**：Go 端优先使用标准库和现有依赖；前端精选轻量库
3. **保持向后兼容**：配置新增字段必须有默认值，`dashboard.enabled: false` 为默认
4. **每阶段独立可交付**：每个 Phase 完成后系统可正常运行
5. **Build Tag 隔离**：不含 `dashboard` tag 编译时，零额外开销
6. **前端设计规范**：所有前端页面设计与实现必须遵循 `frontend-design` Skill（`~/.claude/skills/frontend-design/SKILL.md`）中的设计原则：
   - Mobile-first 响应式布局、Typography 字体规范、Color 主题系统（含 dark mode）
   - 交互反馈（loading skeleton、error recovery、optimistic updates）
   - Animation 动效规范、Accessibility 无障碍标准（对比度 4.5:1、焦点状态、语义化 HTML、键盘导航）
   - 技术栈与 Skill 推荐一致：Next.js（静态导出）+ shadcn/ui + Framer Motion + Tailwind CSS + ECharts

---

## Phase 0：核心改动（1-2 天）

### TASK-D000：扩展 APIKeyPair 支持角色

**涉及文件**：
- `config/config.go` — `APIKeyPair` 结构
- `internal/security/auth.go` — `APIKeyPair` 结构 + `AuthManager` 方法

为统一 Dashboard 登录和核心 API 认证，需要扩展 `APIKeyPair`：

```go
// config/config.go（约 L448）
type APIKeyPair struct {
    Key         string `yaml:"key"`
    Secret      string `yaml:"secret"`
    Role        string `yaml:"role"`                  // 预留字段，当前默认 "root"（全部权限）
    DisplayName string `yaml:"display_name"`          // 新增：显示名称
}
```

同步修改 `internal/security/auth.go`（约 L15）中的同名结构。

扩展 `AuthManager` 增加方法供 Dashboard 调用：

```go
// ValidateCredentials 验证凭证并返回角色信息（Dashboard 登录使用）
func (am *AuthManager) ValidateCredentials(apiKey, apiSecret string) (matched bool, role string, displayName string) {
    am.mu.RLock()
    defer am.mu.RUnlock()

    for _, kp := range am.config.APIKeyPairs {
        if kp.Key == apiKey {
            if err := bcrypt.CompareHashAndPassword([]byte(kp.Secret), []byte(apiSecret)); err == nil {
                role := kp.Role
                if role == "" {
                    role = "root"
                }
                return true, role, kp.DisplayName
            }
        }
    }
    return false, "", ""
}
```

同时在 `cmd/main.go` 增加 `--hash-password` 命令行参数：

```go
if len(os.Args) > 2 && os.Args[1] == "--hash-password" {
    hash, err := bcrypt.GenerateFromPassword([]byte(os.Args[2]), bcrypt.DefaultCost)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    fmt.Println(string(hash))
    os.Exit(0)
}
```

**当前阶段**：`Role` 为空时默认 `"root"`，所有账户拥有全部权限，无角色区分。后续迭代扩展精细 RBAC。

**验证**：`go build ./...` 和 `go test ./internal/security/...`

---

### TASK-D000a：核心新增表级备份/恢复方法

**涉及文件**：`internal/service/miniodb_service.go`、`internal/metadata/manager.go`

为 Dashboard 全量/表级备份提供底层支持：

```go
// internal/service/miniodb_service.go 新增方法
func (s *MinIODBService) BackupTable(ctx context.Context, tableName string) (*BackupResult, error)
func (s *MinIODBService) RestoreTable(ctx context.Context, backupID, tableName, targetTable string) error
func (s *MinIODBService) FullBackup(ctx context.Context) (*BackupResult, error)
func (s *MinIODBService) FullRestore(ctx context.Context, backupID string) error
```

这些方法基于已有的 `metadata.Manager.ManualBackup()`、`metadata.Manager.RecoverFromBackup()` 封装，新增表级别的数据文件复制逻辑。

**验证**：`go build ./...` 和 `go test ./internal/service/...`

---

## Phase 1：项目骨架（2-3 天）

### TASK-D001：创建目录结构

创建以下目录和占位文件：

```bash
mkdir -p cmd/dashboard
mkdir -p internal/dashboard/{handler,service,client,sse,model}
mkdir -p dashboard-ui/src/{api,components,pages,hooks,stores,layouts}
```

**验证**：目录结构完整，`go build ./...` 不受影响。

---

### TASK-D002：定义 DashboardConfig 配置结构

**文件**：`config/config.go`

在 `Config` 结构体中新增 `Dashboard` 字段：

```go
type Config struct {
    // ... 现有字段 ...
    Dashboard DashboardConfig `yaml:"dashboard"`
}

type DashboardConfig struct {
    Enabled               bool          `yaml:"enabled"`
    Port                  string        `yaml:"port"`
    BasePath              string        `yaml:"base_path"`
    CoreEndpoint          string        `yaml:"core_endpoint"`
    CoreGRPCEndpoint      string        `yaml:"core_grpc_endpoint"`
    MetricsScrapeInterval time.Duration `yaml:"metrics_scrape_interval"`
    LogDir                string        `yaml:"log_dir"`
    BackupDir             string        `yaml:"backup_dir"`
}
```

同时需要扩展核心 `APIKeyPair`（`config/config.go` 和 `internal/security/auth.go` 两处）：

```go
type APIKeyPair struct {
    Key         string `yaml:"key" json:"key"`
    Secret      string `yaml:"secret" json:"secret"`
    Role        string `yaml:"role" json:"role"`                  // 预留字段，当前默认 "root"（全部权限）
    DisplayName string `yaml:"display_name" json:"display_name"`
}
```

在 `setDefaults()` 函数（约第 900 行区域）中添加默认值：

```go
c.Dashboard = DashboardConfig{
    Enabled:               false,
    Port:                  ":9090",
    BasePath:              "/dashboard",
    MetricsScrapeInterval: 5 * time.Second,
}
```

在 `overrideWithEnv()` 函数中添加环境变量覆盖：

```go
if v := os.Getenv("DASHBOARD_ENABLED"); v != "" {
    c.Dashboard.Enabled = v == "true"
}
if v := os.Getenv("DASHBOARD_PORT"); v != "" {
    c.Dashboard.Port = v
}
if v := os.Getenv("DASHBOARD_CORE_ENDPOINT"); v != "" {
    c.Dashboard.CoreEndpoint = v
}
if v := os.Getenv("DASHBOARD_CORE_GRPC_ENDPOINT"); v != "" {
    c.Dashboard.CoreGRPCEndpoint = v
}
```

**验证**：`go build ./config/...`，现有配置文件加载不报错（新字段有默认值）。

---

### TASK-D003：定义 CoreClient 接口

**文件**：`internal/dashboard/client/interface.go`

```go
package client

import "context"

type HealthStatus struct {
    Status    string `json:"status"`
    Timestamp int64  `json:"timestamp"`
    Version   string `json:"version"`
    NodeID    string `json:"node_id"`
    Mode      string `json:"mode"` // "single" 或 "distributed"
}

type SystemStatus struct {
    Version       string                 `json:"version"`
    NodeID        string                 `json:"node_id"`
    Uptime        float64                `json:"uptime"`
    Mode          string                 `json:"mode"`
    RedisEnabled  bool                   `json:"redis_enabled"`
    Tables        int                    `json:"tables"`
    Goroutines    int                    `json:"goroutines"`
    MemoryUsage   uint64                 `json:"memory_usage"`
    Extra         map[string]interface{} `json:"extra,omitempty"`
}

type NodeInfo struct {
    ID           string            `json:"id"`
    Address      string            `json:"address"`
    Port         string            `json:"port"`
    Status       string            `json:"status"`
    LastSeen     int64             `json:"last_seen"`
    Metadata     map[string]string `json:"metadata,omitempty"`
}

type TableInfo struct {
    Name          string                 `json:"name"`
    Config        map[string]interface{} `json:"config"`
    RowCountEst   int64                  `json:"row_count_est,omitempty"`
    SizeBytes     int64                  `json:"size_bytes,omitempty"`
    CreatedAt     int64                  `json:"created_at,omitempty"`
}

type TableDetail struct {
    TableInfo
    Columns       []ColumnInfo           `json:"columns,omitempty"`
    Statistics    map[string]interface{} `json:"statistics,omitempty"`
}

type ColumnInfo struct {
    Name     string `json:"name"`
    Type     string `json:"type"`
    Nullable bool   `json:"nullable"`
}

type QueryResult struct {
    Columns    []string                 `json:"columns"`
    Rows       []map[string]interface{} `json:"rows"`
    RowCount   int                      `json:"row_count"`
    DurationMs int64                    `json:"duration_ms"`
}

type BackupInfo struct {
    ID        string `json:"id"`
    Timestamp int64  `json:"timestamp"`
    Size      int64  `json:"size"`
    Status    string `json:"status"`
    NodeID    string `json:"node_id"`
}

type MetadataStatus struct {
    TotalEntries int  `json:"total_entries"`
    LastBackup   int64 `json:"last_backup"`
    AutoRepair   bool  `json:"auto_repair"`
}

type TokenPair struct {
    AccessToken  string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
    ExpiresIn    int64  `json:"expires_in"`
}

type MetricsSnapshot struct {
    Timestamp       int64              `json:"timestamp"`
    HTTPRequests    map[string]float64 `json:"http_requests"`
    GRPCRequests    map[string]float64 `json:"grpc_requests"`
    QueryDuration   map[string]float64 `json:"query_duration"`
    BufferSize      int64              `json:"buffer_size"`
    MinIOOps        map[string]float64 `json:"minio_ops"`
    RedisOps        map[string]float64 `json:"redis_ops"`
    GoRoutines      int                `json:"goroutines"`
    MemAlloc        uint64             `json:"mem_alloc"`
    MemSys          uint64             `json:"mem_sys"`
    GCPauseNs       uint64             `json:"gc_pause_ns"`
}

type WriteDataReq struct {
    Table     string                 `json:"table"`
    ID        string                 `json:"id"`
    Timestamp int64                  `json:"timestamp"`
    Payload   map[string]interface{} `json:"payload"`
}

type UpdateDataReq struct {
    Table     string                 `json:"table"`
    ID        string                 `json:"id"`
    Payload   map[string]interface{} `json:"payload"`
}

type DeleteDataReq struct {
    Table string `json:"table"`
    ID    string `json:"id"`
    Day   string `json:"day"`
}

type CreateTableReq struct {
    Name   string                 `json:"name"`
    Config map[string]interface{} `json:"config"`
}

type RedisConvertConfig struct {
    Addr     string `json:"addr"`
    Password string `json:"password"`
    Mode     string `json:"mode"` // standalone, sentinel, cluster
    DB       int    `json:"db"`
}

// CoreClient 核心服务客户端接口
// 所有 Dashboard Service 通过此接口与 MinIODB 核心通信
type CoreClient interface {
    // 集群与系统
    GetHealth(ctx context.Context) (*HealthStatus, error)
    GetStatus(ctx context.Context) (*SystemStatus, error)
    GetNodes(ctx context.Context) ([]NodeInfo, error)
    GetNodeDetail(ctx context.Context, nodeID string) (*NodeInfo, error)
    ConvertToDistributed(ctx context.Context, cfg RedisConvertConfig) error

    // 表管理
    ListTables(ctx context.Context) ([]TableInfo, error)
    GetTable(ctx context.Context, name string) (*TableDetail, error)
    CreateTable(ctx context.Context, req CreateTableReq) error
    DeleteTable(ctx context.Context, name string) error

    // 数据操作
    QueryData(ctx context.Context, sql string, table string) (*QueryResult, error)
    WriteData(ctx context.Context, req WriteDataReq) error
    UpdateData(ctx context.Context, req UpdateDataReq) error
    DeleteData(ctx context.Context, req DeleteDataReq) error

    // 备份
    CreateBackup(ctx context.Context) error
    RestoreBackup(ctx context.Context, backupID string) error
    ListBackups(ctx context.Context) ([]BackupInfo, error)
    GetMetadataStatus(ctx context.Context) (*MetadataStatus, error)

    // 认证
    GetToken(ctx context.Context, apiKey, apiSecret string) (*TokenPair, error)
    RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error)

    // 监控
    ScrapePrometheus(ctx context.Context) ([]byte, error)
    GetMetricsSnapshot(ctx context.Context) (*MetricsSnapshot, error)
}
```

**验证**：`go build ./internal/dashboard/client/...`

---

### TASK-D004：创建 Build Tag 文件

**文件 1**：`internal/dashboard/mount.go`（build tag: dashboard）

```go
//go:build dashboard

package dashboard

import (
    "minIODB/config"
    "minIODB/internal/dashboard/client"
    "minIODB/internal/discovery"
    "minIODB/internal/metadata"
    "minIODB/internal/service"
    "minIODB/pkg/pool"

    "github.com/gin-gonic/gin"
    "go.uber.org/zap"
)

type MountParams struct {
    Service         *service.MinIODBService
    Config          *config.Config
    Logger          *zap.Logger
    MetadataManager *metadata.Manager
    ServiceRegistry *discovery.ServiceRegistry
    RedisPool       *pool.RedisPool
}

func Mount(router *gin.Engine, params MountParams) {
    if !params.Config.Dashboard.Enabled {
        return
    }

    coreClient := client.NewLocalClient(
        params.Service,
        params.MetadataManager,
        params.ServiceRegistry,
        params.RedisPool,
        params.Config,
        params.Logger,
    )

    srv := NewServer(coreClient, params.Config, params.Logger)
    srv.MountRoutes(router.Group(params.Config.Dashboard.BasePath))
    params.Logger.Info("Dashboard mounted",
        zap.String("base_path", params.Config.Dashboard.BasePath))
}
```

**文件 2**：`internal/dashboard/mount_stub.go`（build tag: !dashboard）

```go
//go:build !dashboard

package dashboard

import "github.com/gin-gonic/gin"

type MountParams struct{}

func Mount(_ *gin.Engine, _ MountParams) {}
```

**验证**：
- `go build ./...` — 不含 dashboard 标签，编译通过
- `go build -tags dashboard ./...` — 含 dashboard 标签，编译通过（需完成后续 TASK 中的 Server 和 LocalClient）

---

### TASK-D005：创建 embed.go 嵌入前端文件

**文件**：`internal/dashboard/embed.go`

```go
package dashboard

import "embed"

//go:embed all:static
var staticFS embed.FS
```

创建占位目录 `internal/dashboard/static/`，放入一个 `index.html` 占位文件：

```html
<!DOCTYPE html>
<html><body><h1>MinIODB Dashboard</h1><p>Build with: npm run build</p></body></html>
```

此占位文件在 Phase 3 构建 Next.js SPA 后会被替换为构建产物。

**验证**：`go build ./internal/dashboard/...`

---

### TASK-D006：更新 Makefile

**文件**：`Makefile`

在现有 Makefile 中新增以下 target：

```makefile
# Dashboard targets
dashboard-ui:
	cd dashboard-ui && npm ci && npm run build
	rm -rf internal/dashboard/static
	cp -r dashboard-ui/out internal/dashboard/static

dashboard: dashboard-ui
	go build -tags dashboard $(LDFLAGS) -o bin/miniodb-dashboard ./cmd/dashboard/

build-with-dashboard: dashboard-ui
	go build -tags dashboard $(LDFLAGS) -o bin/miniodb ./cmd/

docker-dashboard:
	docker build -f Dockerfile.dashboard -t miniodb-dashboard:$(VERSION) .

docker-allinone: 
	docker build -f Dockerfile --build-arg BUILD_TAGS=dashboard -t miniodb:$(VERSION)-allinone .
```

**验证**：`make build` 仍然正常工作（不受影响）。

---

### TASK-D007：创建独立 Dashboard 入口

**文件**：`cmd/dashboard/main.go`

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "minIODB/config"
    "minIODB/internal/dashboard"
    dashclient "minIODB/internal/dashboard/client"
    "minIODB/pkg/logger"

    "github.com/gin-gonic/gin"
    "go.uber.org/zap"
)

func main() {
    configPath := "config/config.yaml"
    if len(os.Args) > 1 {
        configPath = os.Args[1]
    }

    cfg, err := config.LoadConfig(configPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
        os.Exit(1)
    }

    logger.InitLogger(cfg.Log)
    log := logger.Logger

    if cfg.Dashboard.CoreEndpoint == "" {
        log.Fatal("dashboard.core_endpoint must be configured in standalone mode")
    }

    coreClient := dashclient.NewRemoteClient(
        cfg.Dashboard.CoreEndpoint,
        cfg.Dashboard.CoreGRPCEndpoint,
        log,
    )

    gin.SetMode(gin.ReleaseMode)
    router := gin.New()
    router.Use(gin.Recovery())

    srv := dashboard.NewServer(coreClient, cfg, log)
    srv.MountRoutes(router.Group(cfg.Dashboard.BasePath))

    httpServer := &http.Server{
        Addr:         cfg.Dashboard.Port,
        Handler:      router,
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 30 * time.Second,
    }

    go func() {
        log.Info("Dashboard server starting",
            zap.String("port", cfg.Dashboard.Port),
            zap.String("core_endpoint", cfg.Dashboard.CoreEndpoint))
        if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatal("Dashboard server failed", zap.Error(err))
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Info("Shutting down dashboard server...")
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := httpServer.Shutdown(ctx); err != nil {
        log.Error("Dashboard server forced shutdown", zap.Error(err))
    }
    log.Info("Dashboard server stopped")
}
```

**验证**：`go build ./cmd/dashboard/...`（需完成 NewServer 和 NewRemoteClient 的桩实现）。

---

## Phase 2：Dashboard 后端（5-7 天）

### TASK-D008：实现 SSE Hub

**文件**：`internal/dashboard/sse/hub.go`

```go
package sse

import (
    "encoding/json"
    "fmt"
    "sync"
    "time"
)

type Event struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
    Time int64       `json:"time"`
}

func (e *Event) Format() string {
    data, _ := json.Marshal(e)
    return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Type, string(data))
}

type Client struct {
    ID     string
    Topics []string
    Send   chan string
}

type Hub struct {
    mu         sync.RWMutex
    clients    map[string]*Client           // clientID -> Client
    topics     map[string]map[string]bool   // topic -> set of clientIDs
    register   chan *Client
    unregister chan string
    broadcast  chan *Event
    stop       chan struct{}
}

func NewHub() *Hub {
    h := &Hub{
        clients:    make(map[string]*Client),
        topics:     make(map[string]map[string]bool),
        register:   make(chan *Client, 64),
        unregister: make(chan string, 64),
        broadcast:  make(chan *Event, 256),
        stop:       make(chan struct{}),
    }
    go h.run()
    return h
}

func (h *Hub) run() {
    for {
        select {
        case client := <-h.register:
            h.mu.Lock()
            h.clients[client.ID] = client
            for _, topic := range client.Topics {
                if h.topics[topic] == nil {
                    h.topics[topic] = make(map[string]bool)
                }
                h.topics[topic][client.ID] = true
            }
            h.mu.Unlock()

        case clientID := <-h.unregister:
            h.mu.Lock()
            if client, ok := h.clients[clientID]; ok {
                for _, topic := range client.Topics {
                    delete(h.topics[topic], clientID)
                }
                close(client.Send)
                delete(h.clients, clientID)
            }
            h.mu.Unlock()

        case event := <-h.broadcast:
            formatted := event.Format()
            h.mu.RLock()

            allTopics := event.Type == "*"
            if allTopics {
                for _, client := range h.clients {
                    select {
                    case client.Send <- formatted:
                    default:
                        // 客户端缓冲区满，跳过
                    }
                }
            } else if subscribers, ok := h.topics[event.Type]; ok {
                for clientID := range subscribers {
                    if client, exists := h.clients[clientID]; exists {
                        select {
                        case client.Send <- formatted:
                        default:
                        }
                    }
                }
            }
            h.mu.RUnlock()

        case <-h.stop:
            h.mu.Lock()
            for _, client := range h.clients {
                close(client.Send)
            }
            h.clients = make(map[string]*Client)
            h.topics = make(map[string]map[string]bool)
            h.mu.Unlock()
            return
        }
    }
}

func (h *Hub) Register(client *Client) {
    h.register <- client
}

func (h *Hub) Unregister(clientID string) {
    h.unregister <- clientID
}

func (h *Hub) Publish(eventType string, data interface{}) {
    h.broadcast <- &Event{
        Type: eventType,
        Data: data,
        Time: time.Now().UnixMilli(),
    }
}

func (h *Hub) Stop() {
    close(h.stop)
}

func (h *Hub) ClientCount() int {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return len(h.clients)
}
```

**验证**：`go test ./internal/dashboard/sse/...` — 编写基本单测验证注册、发布、退订。

---

### TASK-D009：实现 Dashboard Model

**文件**：`internal/dashboard/model/types.go`

```go
package model

type LoginRequest struct {
    APIKey    string `json:"api_key" binding:"required"`
    APISecret string `json:"api_secret" binding:"required"`
}

type LoginResponse struct {
    Token     string   `json:"token"`
    ExpiresIn int64    `json:"expires_in"`
    User      UserInfo `json:"user"`
}

type UserInfo struct {
    Username    string `json:"username"`    // 即 api_key
    Role        string `json:"role"`
    DisplayName string `json:"display_name"`
}

type ChangePasswordRequest struct {
    OldPassword string `json:"old_password" binding:"required"`
    NewPassword string `json:"new_password" binding:"required,min=6"`
}

type CreateTableRequest struct {
    TableName string                 `json:"table_name" binding:"required"`
    Config    map[string]interface{} `json:"config"`
}

type UpdateTableRequest struct {
    Config map[string]interface{} `json:"config" binding:"required"`
}

type WriteDataRequest struct {
    ID        string                 `json:"id"`
    Timestamp string                 `json:"timestamp" binding:"required"`
    Payload   map[string]interface{} `json:"payload" binding:"required"`
}

type BatchWriteDataRequest struct {
    Records []WriteDataRequest `json:"records" binding:"required,min=1"`
}

type UpdateDataRequest struct {
    Timestamp string                 `json:"timestamp"`
    Payload   map[string]interface{} `json:"payload" binding:"required"`
}

type DataBrowseParams struct {
    Page      int    `form:"page,default=1"`
    PageSize  int    `form:"page_size,default=50"`
    SortBy    string `form:"sort_by"`
    SortOrder string `form:"sort_order,default=desc"`
    Filter    string `form:"filter"`
    ID        string `form:"id"`
}

type ConvertDistributedRequest struct {
    RedisAddr     string `json:"redis_addr" binding:"required"`
    RedisPassword string `json:"redis_password"`
    RedisMode     string `json:"redis_mode" binding:"required"` // standalone, sentinel, cluster
    RedisDB       int    `json:"redis_db"`
}

type ScaleCheckRequest struct {
    Action    string   `json:"action" binding:"required"` // "add" 或 "remove"
    NodeIDs   []string `json:"node_ids,omitempty"`
    NodeCount int      `json:"node_count,omitempty"`
}

type ScaleCheckResponse struct {
    Feasible      bool     `json:"feasible"`
    CurrentNodes  int      `json:"current_nodes"`
    TargetNodes   int      `json:"target_nodes"`
    Warnings      []string `json:"warnings,omitempty"`
    ConfigSnippet string   `json:"config_snippet,omitempty"` // 生成的配置片段
}

type LogEntry struct {
    Level     string                 `json:"level"`
    Timestamp int64                  `json:"timestamp"`
    Message   string                 `json:"message"`
    Caller    string                 `json:"caller,omitempty"`
    Fields    map[string]interface{} `json:"fields,omitempty"`
}

type LogQueryParams struct {
    Level   string `form:"level"`
    Keyword string `form:"keyword"`
    Start   int64  `form:"start"`
    End     int64  `form:"end"`
    Limit   int    `form:"limit"`
    Offset  int    `form:"offset"`
}

type LogFile struct {
    Name     string `json:"name"`
    Size     int64  `json:"size"`
    Modified int64  `json:"modified"`
}

type BackupInfo struct {
    ID            string   `json:"id"`
    Type          string   `json:"type"`           // "metadata", "full", "table"
    Status        string   `json:"status"`         // "running", "completed", "failed"
    Tables        []string `json:"tables"`         // 包含的表（表级备份时有值）
    Size          int64    `json:"size"`
    ObjectName    string   `json:"object_name"`    // MinIO 对象名
    CreatedAt     int64    `json:"created_at"`
    CompletedAt   int64    `json:"completed_at"`
    DurationMs    int64    `json:"duration_ms"`
    NodeID        string   `json:"node_id"`
    Error         string   `json:"error,omitempty"`
}

type BackupSchedule struct {
    MetadataEnabled  bool   `json:"metadata_enabled"`
    MetadataInterval string `json:"metadata_interval"` // "1h", "6h", "24h"
    FullEnabled      bool   `json:"full_enabled"`
    FullInterval     string `json:"full_interval"`
    Retain           int    `json:"retain"`
}

type RestoreRequest struct {
    BackupID    string   `json:"backup_id" binding:"required"`
    Tables      []string `json:"tables"`       // 空 = 全量恢复
    TargetTable string   `json:"target_table"` // 表级恢复时，可指定目标表名
    Mode        string   `json:"mode"`         // "overwrite" 或 "new_table"
}

type RestoreResult struct {
    BackupID   string   `json:"backup_id"`
    Tables     []string `json:"tables"`
    DurationMs int64    `json:"duration_ms"`
    Status     string   `json:"status"`
}

type TableBackupRequest struct {
    Comment string `json:"comment"` // 备份说明
}

type AnalyticsQueryRequest struct {
    SQL     string `json:"sql" binding:"required"`
    Table   string `json:"table"`
    Explain bool   `json:"explain"`
}

type AnalyticsQueryResponse struct {
    Result     interface{} `json:"result"`
    DurationMs int64       `json:"duration_ms"`
    RowCount   int         `json:"row_count"`
    Plan       string      `json:"plan,omitempty"` // EXPLAIN 输出
}

type QueryHistoryEntry struct {
    SQL        string `json:"sql"`
    Table      string `json:"table"`
    Timestamp  int64  `json:"timestamp"`
    DurationMs int64  `json:"duration_ms"`
    RowCount   int    `json:"row_count"`
    Success    bool   `json:"success"`
    Error      string `json:"error,omitempty"`
}

type ColumnStats struct {
    Name          string      `json:"name"`
    Type          string      `json:"type"`
    NullCount     int64       `json:"null_count"`
    DistinctCount int64       `json:"distinct_count"`
    Min           interface{} `json:"min,omitempty"`
    Max           interface{} `json:"max,omitempty"`
}

type AlertInfo struct {
    ID        string `json:"id"`
    Level     string `json:"level"` // "warning", "critical"
    Message   string `json:"message"`
    Source    string `json:"source"`
    Timestamp int64  `json:"timestamp"`
    Resolved  bool   `json:"resolved"`
}

type PageResponse struct {
    Data       interface{} `json:"data"`
    Total      int64       `json:"total"`
    Page       int         `json:"page"`
    PageSize   int         `json:"page_size"`
}

type APIResponse struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

**验证**：`go build ./internal/dashboard/model/...`

---

### TASK-D010：实现 LocalClient（All-in-One 模式）

**文件**：`internal/dashboard/client/local.go`

```go
//go:build dashboard

package client

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "runtime"
    "time"

    miniodb "minIODB/api/proto/miniodb/v1"
    "minIODB/config"
    "minIODB/internal/discovery"
    "minIODB/internal/metadata"
    "minIODB/internal/service"
    "minIODB/pkg/pool"

    "go.uber.org/zap"
    "google.golang.org/protobuf/types/known/structpb"
    "google.golang.org/protobuf/types/known/timestamppb"
)

type LocalClient struct {
    svc             *service.MinIODBService
    metadataManager *metadata.Manager
    registry        *discovery.ServiceRegistry
    redisPool       *pool.RedisPool
    cfg             *config.Config
    logger          *zap.Logger
}

func NewLocalClient(
    svc *service.MinIODBService,
    metadataManager *metadata.Manager,
    registry *discovery.ServiceRegistry,
    redisPool *pool.RedisPool,
    cfg *config.Config,
    logger *zap.Logger,
) CoreClient {
    return &LocalClient{
        svc:             svc,
        metadataManager: metadataManager,
        registry:        registry,
        redisPool:       redisPool,
        cfg:             cfg,
        logger:          logger,
    }
}
```

实现各接口方法时，直接调用 `s.svc.XXX()` 对应方法，将 protobuf 类型转换为 Dashboard model 类型。例如：

```go
func (c *LocalClient) GetHealth(ctx context.Context) (*HealthStatus, error) {
    resp, err := c.svc.HealthCheck(ctx, &miniodb.HealthCheckRequest{})
    if err != nil {
        return nil, err
    }
    mode := "single"
    if c.cfg.Redis.Enabled || c.cfg.Network.Pools.Redis.Enabled {
        mode = "distributed"
    }
    return &HealthStatus{
        Status:    resp.Status,
        Timestamp: time.Now().Unix(),
        Version:   resp.Version,
        NodeID:    c.cfg.Server.NodeID,
        Mode:      mode,
    }, nil
}

func (c *LocalClient) ListTables(ctx context.Context) ([]TableInfo, error) {
    resp, err := c.svc.ListTables(ctx, &miniodb.ListTablesRequest{})
    if err != nil {
        return nil, err
    }
    tables := make([]TableInfo, 0, len(resp.Tables))
    for _, t := range resp.Tables {
        tables = append(tables, TableInfo{
            Name:   t.TableName,
            Config: t.Config.AsMap(),
        })
    }
    return tables, nil
}

func (c *LocalClient) QueryData(ctx context.Context, sql string, table string) (*QueryResult, error) {
    start := time.Now()
    resp, err := c.svc.QueryData(ctx, &miniodb.QueryDataRequest{Sql: sql})
    if err != nil {
        return nil, err
    }
    duration := time.Since(start).Milliseconds()

    var rows []map[string]interface{}
    if err := json.Unmarshal([]byte(resp.ResultJson), &rows); err != nil {
        rows = []map[string]interface{}{{"raw": resp.ResultJson}}
    }

    var columns []string
    if len(rows) > 0 {
        for k := range rows[0] {
            columns = append(columns, k)
        }
    }

    return &QueryResult{
        Columns:    columns,
        Rows:       rows,
        RowCount:   len(rows),
        DurationMs: duration,
    }, nil
}

func (c *LocalClient) GetNodes(ctx context.Context) ([]NodeInfo, error) {
    if c.registry == nil {
        return []NodeInfo{{
            ID:       c.cfg.Server.NodeID,
            Address:  "localhost",
            Port:     c.cfg.Server.RestPort,
            Status:   "healthy",
            LastSeen: time.Now().Unix(),
        }}, nil
    }
    nodes := c.registry.DiscoverNodes()
    result := make([]NodeInfo, 0, len(nodes))
    for _, n := range nodes {
        result = append(result, NodeInfo{
            ID:       n.ID,
            Address:  n.Address,
            Port:     n.Port,
            Status:   n.Status,
            LastSeen: n.LastSeen.Unix(),
        })
    }
    return result, nil
}

func (c *LocalClient) GetMetricsSnapshot(ctx context.Context) (*MetricsSnapshot, error) {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    return &MetricsSnapshot{
        Timestamp:  time.Now().UnixMilli(),
        GoRoutines: runtime.NumGoroutine(),
        MemAlloc:   m.Alloc,
        MemSys:     m.Sys,
        GCPauseNs:  m.PauseNs[(m.NumGC+255)%256],
    }, nil
}

// ScrapePrometheus 通过本地 HTTP 调用 Prometheus 端点
func (c *LocalClient) ScrapePrometheus(ctx context.Context) ([]byte, error) {
    metricsPort := c.cfg.Metrics.Port
    if metricsPort == "" {
        metricsPort = ":8082"
    }
    resp, err := http.Get(fmt.Sprintf("http://localhost%s/metrics", metricsPort))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    return io.ReadAll(resp.Body)
}
```

其他方法（CreateBackup、RestoreBackup、ListBackups 等）采用相同模式：调用 `c.svc.XXX()` 或 `c.metadataManager.XXX()`，转换类型后返回。

**验证**：`go build -tags dashboard ./internal/dashboard/client/...`

---

### TASK-D011：实现 RemoteClient（独立部署模式）

**文件**：`internal/dashboard/client/remote.go`

```go
package client

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "go.uber.org/zap"
)

type RemoteClient struct {
    restEndpoint string
    grpcEndpoint string
    httpClient   *http.Client
    logger       *zap.Logger
    token        string
    tokenExpiry  time.Time
}

func NewRemoteClient(restEndpoint, grpcEndpoint string, logger *zap.Logger) CoreClient {
    return &RemoteClient{
        restEndpoint: restEndpoint,
        grpcEndpoint: grpcEndpoint,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,
        },
        logger: logger,
    }
}
```

每个接口方法通过 HTTP 调用 minIODB REST API。例如：

```go
func (c *RemoteClient) GetHealth(ctx context.Context) (*HealthStatus, error) {
    var result HealthStatus
    if err := c.doGet(ctx, "/v1/health", &result); err != nil {
        return nil, err
    }
    return &result, nil
}

func (c *RemoteClient) ListTables(ctx context.Context) ([]TableInfo, error) {
    var resp struct {
        Tables []TableInfo `json:"tables"`
    }
    if err := c.doGet(ctx, "/v1/tables", &resp); err != nil {
        return nil, err
    }
    return resp.Tables, nil
}

func (c *RemoteClient) QueryData(ctx context.Context, sql string, table string) (*QueryResult, error) {
    body := map[string]string{"sql": sql}
    var resp struct {
        ResultJSON string `json:"result_json"`
    }
    start := time.Now()
    if err := c.doPost(ctx, "/v1/query", body, &resp); err != nil {
        return nil, err
    }
    // 解析 result_json ...
}

// 内部 HTTP 辅助方法
func (c *RemoteClient) doGet(ctx context.Context, path string, result interface{}) error {
    req, err := http.NewRequestWithContext(ctx, "GET", c.restEndpoint+path, nil)
    if err != nil {
        return err
    }
    c.setAuthHeader(req)
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
    }
    return json.NewDecoder(resp.Body).Decode(result)
}

func (c *RemoteClient) doPost(ctx context.Context, path string, body interface{}, result interface{}) error {
    jsonBody, err := json.Marshal(body)
    if err != nil {
        return err
    }
    req, err := http.NewRequestWithContext(ctx, "POST", c.restEndpoint+path, bytes.NewReader(jsonBody))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    c.setAuthHeader(req)
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        respBody, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
    }
    if result != nil {
        return json.NewDecoder(resp.Body).Decode(result)
    }
    return nil
}

func (c *RemoteClient) setAuthHeader(req *http.Request) {
    if c.token != "" {
        req.Header.Set("Authorization", "Bearer "+c.token)
    }
}

// ensureToken 自动获取和刷新 token
func (c *RemoteClient) ensureToken(ctx context.Context) error {
    if c.token != "" && time.Now().Before(c.tokenExpiry) {
        return nil
    }
    // 调用 /v1/auth/token 获取 token
    // ...
    return nil
}
```

**验证**：`go build ./internal/dashboard/client/...`

---

### TASK-D012：实现 Dashboard Server

**文件**：`internal/dashboard/server.go`

```go
package dashboard

import (
    "net/http"
    "io/fs"

    "minIODB/config"
    "minIODB/internal/dashboard/client"
    "minIODB/internal/dashboard/handler"
    "minIODB/internal/dashboard/service"
    "minIODB/internal/dashboard/sse"

    "github.com/gin-gonic/gin"
    "go.uber.org/zap"
)

type Server struct {
    client  client.CoreClient
    cfg     *config.Config
    logger  *zap.Logger
    sseHub  *sse.Hub
}

func NewServer(coreClient client.CoreClient, cfg *config.Config, logger *zap.Logger) *Server {
    return &Server{
        client: coreClient,
        cfg:    cfg,
        logger: logger,
        sseHub: sse.NewHub(),
    }
}

func (s *Server) MountRoutes(group *gin.RouterGroup) {
    // 静态文件（Next.js 静态导出）
    staticSub, err := fs.Sub(staticFS, "static")
    if err == nil {
        group.StaticFS("/ui", http.FS(staticSub))
        // SPA 回退：所有未匹配的 /ui/* 路径返回 index.html
        group.NoRoute(func(c *gin.Context) {
            c.FileFromFS("index.html", http.FS(staticSub))
        })
    }

    // 初始化 Services
    clusterSvc := service.NewClusterService(s.client, s.cfg, s.logger)
    nodeSvc := service.NewNodeService(s.client, s.logger)
    dataSvc := service.NewDataService(s.client, s.logger)
    logSvc := service.NewLogService(s.cfg, s.logger)
    backupSvc := service.NewBackupService(s.client, s.logger)
    monitorSvc := service.NewMonitorService(s.client, s.sseHub, s.cfg, s.logger)
    analyticsSvc := service.NewAnalyticsService(s.client, s.logger)

    // 初始化 Handlers
    clusterHandler := handler.NewClusterHandler(clusterSvc)
    nodeHandler := handler.NewNodeHandler(nodeSvc)
    dataHandler := handler.NewDataHandler(dataSvc)
    logHandler := handler.NewLogHandler(logSvc)
    backupHandler := handler.NewBackupHandler(backupSvc)
    monitorHandler := handler.NewMonitorHandler(monitorSvc, s.sseHub)
    analyticsHandler := handler.NewAnalyticsHandler(analyticsSvc)

    // API 路由
    api := group.Group("/api/v1")
    {
        // 认证
        api.POST("/auth/login", handler.Login(s.cfg))

        // 需要认证的路由
        authed := api.Group("")
        authed.Use(handler.AuthMiddleware(s.cfg))
        {
            // 集群
            authed.GET("/cluster/info", clusterHandler.GetInfo)
            authed.GET("/cluster/topology", clusterHandler.GetTopology)
            authed.POST("/cluster/convert-to-distributed", clusterHandler.ConvertToDistributed)
            authed.GET("/cluster/config", clusterHandler.GetConfig)

            // 节点
            authed.GET("/nodes", nodeHandler.List)
            authed.GET("/nodes/:id", nodeHandler.Detail)
            authed.GET("/nodes/:id/metrics", nodeHandler.Metrics)
            authed.POST("/nodes/scale-check", nodeHandler.ScaleCheck)

            // 数据
            authed.GET("/tables", dataHandler.ListTables)
            authed.GET("/tables/:name", dataHandler.GetTable)
            authed.GET("/tables/:name/data", dataHandler.BrowseData)
            authed.GET("/tables/:name/stats", dataHandler.GetStats)
            authed.POST("/query", dataHandler.ExecuteQuery)
            authed.GET("/query/history", dataHandler.QueryHistory)

            // 日志
            authed.GET("/logs", logHandler.Query)
            authed.GET("/logs/stream", logHandler.Stream)
            authed.GET("/logs/files", logHandler.ListFiles)
            authed.GET("/logs/files/:name/download", logHandler.Download)

            // 备份
            authed.GET("/backups", backupHandler.List)
            authed.POST("/backups", backupHandler.Create)
            authed.POST("/backups/:id/restore", backupHandler.Restore)
            authed.DELETE("/backups/:id", backupHandler.Delete)
            authed.GET("/backups/schedule", backupHandler.GetSchedule)
            authed.PUT("/backups/schedule", backupHandler.UpdateSchedule)

            // 监控
            authed.GET("/monitor/overview", monitorHandler.Overview)
            authed.GET("/monitor/stream", monitorHandler.Stream)
            authed.GET("/monitor/prometheus/query", monitorHandler.PrometheusQuery)
            authed.GET("/monitor/sla", monitorHandler.SLA)
            authed.GET("/monitor/alerts", monitorHandler.Alerts)

            // 分析
            authed.POST("/analytics/query", analyticsHandler.Query)
            authed.GET("/analytics/suggestions", analyticsHandler.Suggestions)
            authed.GET("/analytics/overview", analyticsHandler.Overview)
        }
    }

    // 启动监控数据采集
    monitorSvc.StartCollectors()

    s.logger.Info("Dashboard routes mounted")
}
```

**验证**：`go build -tags dashboard ./internal/dashboard/...`

---

### TASK-D013：实现各 Handler

每个 Handler 遵循统一模式。以 `handler/cluster.go` 为例：

**文件**：`internal/dashboard/handler/cluster.go`

```go
package handler

import (
    "net/http"

    "minIODB/internal/dashboard/model"
    "minIODB/internal/dashboard/service"

    "github.com/gin-gonic/gin"
)

type ClusterHandler struct {
    svc *service.ClusterService
}

func NewClusterHandler(svc *service.ClusterService) *ClusterHandler {
    return &ClusterHandler{svc: svc}
}

func (h *ClusterHandler) GetInfo(c *gin.Context) {
    info, err := h.svc.GetClusterInfo(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, model.APIResponse{
            Code: 500, Message: err.Error(),
        })
        return
    }
    c.JSON(http.StatusOK, model.APIResponse{Code: 200, Data: info})
}

func (h *ClusterHandler) GetTopology(c *gin.Context) {
    topo, err := h.svc.GetTopology(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, model.APIResponse{
            Code: 500, Message: err.Error(),
        })
        return
    }
    c.JSON(http.StatusOK, model.APIResponse{Code: 200, Data: topo})
}

func (h *ClusterHandler) ConvertToDistributed(c *gin.Context) {
    var req model.ConvertDistributedRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, model.APIResponse{
            Code: 400, Message: err.Error(),
        })
        return
    }
    if err := h.svc.ConvertToDistributed(c.Request.Context(), req); err != nil {
        c.JSON(http.StatusInternalServerError, model.APIResponse{
            Code: 500, Message: err.Error(),
        })
        return
    }
    c.JSON(http.StatusOK, model.APIResponse{
        Code: 200, Message: "Successfully converted to distributed mode",
    })
}

func (h *ClusterHandler) GetConfig(c *gin.Context) {
    cfg := h.svc.GetSanitizedConfig()
    c.JSON(http.StatusOK, model.APIResponse{Code: 200, Data: cfg})
}
```

其他 Handler（node, data, log, backup, monitor, analytics）遵循相同模式：
- 绑定请求参数
- 调用对应 Service
- 统一 `model.APIResponse` 返回格式

**Monitor Handler 的 SSE 端点**示例（`handler/monitor.go`）：

```go
func (h *MonitorHandler) Stream(c *gin.Context) {
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")
    c.Header("X-Accel-Buffering", "no")

    clientID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
    topics := c.QueryArray("topics")
    if len(topics) == 0 {
        topics = []string{"metrics", "node_health", "alerts"}
    }

    client := &sse.Client{
        ID:     clientID,
        Topics: topics,
        Send:   make(chan string, 64),
    }
    h.hub.Register(client)
    defer h.hub.Unregister(clientID)

    c.Stream(func(w io.Writer) bool {
        select {
        case msg, ok := <-client.Send:
            if !ok {
                return false
            }
            c.SSEvent("message", msg)
            return true
        case <-c.Request.Context().Done():
            return false
        }
    })
}
```

**验证**：`go build -tags dashboard ./internal/dashboard/handler/...`

---

### TASK-D014：实现各 Service

每个 Service 封装业务逻辑。以 `service/monitor_service.go` 为例：

```go
package service

import (
    "context"
    "time"

    "minIODB/config"
    "minIODB/internal/dashboard/client"
    "minIODB/internal/dashboard/sse"

    "go.uber.org/zap"
)

type MonitorService struct {
    client client.CoreClient
    hub    *sse.Hub
    cfg    *config.Config
    logger *zap.Logger
    stopCh chan struct{}
}

func NewMonitorService(client client.CoreClient, hub *sse.Hub, cfg *config.Config, logger *zap.Logger) *MonitorService {
    return &MonitorService{
        client: client,
        hub:    hub,
        cfg:    cfg,
        logger: logger,
        stopCh: make(chan struct{}),
    }
}

func (s *MonitorService) StartCollectors() {
    interval := s.cfg.Dashboard.MetricsScrapeInterval
    if interval == 0 {
        interval = 5 * time.Second
    }

    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                s.collectAndPublish()
            case <-s.stopCh:
                return
            }
        }
    }()
}

func (s *MonitorService) collectAndPublish() {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    snapshot, err := s.client.GetMetricsSnapshot(ctx)
    if err != nil {
        s.logger.Warn("Failed to collect metrics", zap.Error(err))
        return
    }
    s.hub.Publish("metrics", snapshot)
}

func (s *MonitorService) GetOverview(ctx context.Context) (interface{}, error) {
    return s.client.GetStatus(ctx)
}

func (s *MonitorService) Stop() {
    close(s.stopCh)
}
```

`service/log_service.go` 的日志文件读取和 SSE 流实现：

```go
package service

import (
    "bufio"
    "encoding/json"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "minIODB/config"
    "minIODB/internal/dashboard/model"

    "go.uber.org/zap"
)

type LogService struct {
    cfg    *config.Config
    logger *zap.Logger
}

func NewLogService(cfg *config.Config, logger *zap.Logger) *LogService {
    return &LogService{cfg: cfg, logger: logger}
}

func (s *LogService) getLogDir() string {
    if s.cfg.Dashboard.LogDir != "" {
        return s.cfg.Dashboard.LogDir
    }
    if s.cfg.Log.Filename != "" {
        return filepath.Dir(s.cfg.Log.Filename)
    }
    return "logs"
}

func (s *LogService) QueryLogs(params model.LogQueryParams) ([]model.LogEntry, int64, error) {
    logDir := s.getLogDir()
    // 读取日志文件，解析 JSON 行，应用过滤条件
    // 支持 level、keyword、time range 过滤
    // 返回分页结果
    // ...
    return nil, 0, nil
}

func (s *LogService) ListLogFiles() ([]model.LogFile, error) {
    logDir := s.getLogDir()
    entries, err := os.ReadDir(logDir)
    if err != nil {
        return nil, err
    }
    files := make([]model.LogFile, 0)
    for _, entry := range entries {
        if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
            continue
        }
        info, err := entry.Info()
        if err != nil {
            continue
        }
        files = append(files, model.LogFile{
            Name:     entry.Name(),
            Size:     info.Size(),
            Modified: info.ModTime().Unix(),
        })
    }
    sort.Slice(files, func(i, j int) bool {
        return files[i].Modified > files[j].Modified
    })
    return files, nil
}
```

**验证**：`go build -tags dashboard ./internal/dashboard/service/...`

---

### TASK-D014a：实现 BackupService（全量+表级）

**文件**：`internal/dashboard/service/backup_service.go`

```go
package service

import (
    "context"
    "minIODB/internal/dashboard/client"
    "minIODB/internal/dashboard/model"
    "go.uber.org/zap"
)

type BackupService struct {
    client client.CoreClient
    logger *zap.Logger
}

func NewBackupService(client client.CoreClient, logger *zap.Logger) *BackupService {
    return &BackupService{client: client, logger: logger}
}

func (s *BackupService) ListBackups(ctx context.Context) ([]model.BackupInfo, error) {
    return s.client.ListBackups(ctx)
}

func (s *BackupService) TriggerMetadataBackup(ctx context.Context) (*model.BackupInfo, error) {
    return s.client.TriggerMetadataBackup(ctx)
}

func (s *BackupService) TriggerFullBackup(ctx context.Context) (*model.BackupInfo, error) {
    return s.client.TriggerFullBackup(ctx)
}

func (s *BackupService) TriggerTableBackup(ctx context.Context, tableName string, req model.TableBackupRequest) (*model.BackupInfo, error) {
    return s.client.TriggerTableBackup(ctx, tableName, req)
}

func (s *BackupService) GetBackup(ctx context.Context, id string) (*model.BackupInfo, error) {
    return s.client.GetBackup(ctx, id)
}

func (s *BackupService) RestoreFromBackup(ctx context.Context, req model.RestoreRequest) (*model.RestoreResult, error) {
    return s.client.RestoreFromBackup(ctx, req)
}

func (s *BackupService) DeleteBackup(ctx context.Context, id string) error {
    return s.client.DeleteBackup(ctx, id)
}

func (s *BackupService) DownloadBackup(ctx context.Context, id string) ([]byte, string, error) {
    return s.client.DownloadBackup(ctx, id)
}

func (s *BackupService) ValidateBackup(ctx context.Context, id string) error {
    return s.client.ValidateBackup(ctx, id)
}

func (s *BackupService) GetSchedule(ctx context.Context) (*model.BackupSchedule, error) {
    return s.client.GetBackupSchedule(ctx)
}

func (s *BackupService) UpdateSchedule(ctx context.Context, schedule model.BackupSchedule) error {
    return s.client.UpdateBackupSchedule(ctx, schedule)
}
```

对应的 `CoreClient` 接口需新增方法：

```go
// client/interface.go 新增
TriggerMetadataBackup(ctx context.Context) (*model.BackupInfo, error)
TriggerFullBackup(ctx context.Context) (*model.BackupInfo, error)
TriggerTableBackup(ctx context.Context, tableName string, req model.TableBackupRequest) (*model.BackupInfo, error)
GetBackup(ctx context.Context, id string) (*model.BackupInfo, error)
RestoreFromBackup(ctx context.Context, req model.RestoreRequest) (*model.RestoreResult, error)
DeleteBackup(ctx context.Context, id string) error
DownloadBackup(ctx context.Context, id string) (data []byte, filename string, err error)
ValidateBackup(ctx context.Context, id string) error
GetBackupSchedule(ctx context.Context) (*model.BackupSchedule, error)
UpdateBackupSchedule(ctx context.Context, schedule model.BackupSchedule) error
```

**验证**：`go build -tags dashboard ./internal/dashboard/service/...`

---

### TASK-D015：实现 Auth（复用核心 APIKeyPair 账户）

**文件**：`internal/dashboard/handler/auth.go`

Dashboard 登录直接使用核心 `auth.api_key_pairs` 凭证。`api_key` 作为用户名，`api_secret` 作为密码，`role` 决定权限。

```go
package handler

import (
    "net/http"
    "strings"
    "time"

    "minIODB/config"
    "minIODB/internal/dashboard/model"

    "github.com/gin-gonic/gin"
    "golang.org/x/crypto/bcrypt"
    "github.com/golang-jwt/jwt/v5"
)

// findAPIKeyPair 从配置中查找匹配的 API Key
func findAPIKeyPair(cfg *config.Config, apiKey string) *config.APIKeyPair {
    for i := range cfg.Auth.APIKeyPairs {
        if cfg.Auth.APIKeyPairs[i].Key == apiKey {
            return &cfg.Auth.APIKeyPairs[i]
        }
    }
    return nil
}

func Login(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req model.LoginRequest
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
            return
        }

        // 在核心 api_key_pairs 中查找账户
        keyPair := findAPIKeyPair(cfg, req.APIKey)
        if keyPair == nil {
            c.JSON(http.StatusUnauthorized, model.APIResponse{Code: 401, Message: "Invalid credentials"})
            return
        }

        // bcrypt 验证密码
        if err := bcrypt.CompareHashAndPassword([]byte(keyPair.Secret), []byte(req.APISecret)); err != nil {
            c.JSON(http.StatusUnauthorized, model.APIResponse{Code: 401, Message: "Invalid credentials"})
            return
        }

        // role 默认值处理（当前阶段全部为 root）
        role := keyPair.Role
        if role == "" {
            role = "root"
        }

        secret := cfg.Auth.JWTSecret
        if secret == "" {
            secret = cfg.Security.JWTSecret
        }

        token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
            "sub":          keyPair.Key,
            "role":         role,
            "display_name": keyPair.DisplayName,
            "type":         "dashboard",
            "exp":          time.Now().Add(24 * time.Hour).Unix(),
            "iat":          time.Now().Unix(),
        })
        tokenString, err := token.SignedString([]byte(secret))
        if err != nil {
            c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: "Token generation failed"})
            return
        }

        c.JSON(http.StatusOK, model.APIResponse{
            Code: 200,
            Data: model.LoginResponse{
                Token:     tokenString,
                ExpiresIn: 86400,
                User: model.UserInfo{
                    Username:    keyPair.Key,
                    Role:        role,
                    DisplayName: keyPair.DisplayName,
                },
            },
        })
    }
}

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        if !strings.HasPrefix(authHeader, "Bearer ") {
            c.JSON(http.StatusUnauthorized, model.APIResponse{Code: 401, Message: "Missing authorization"})
            c.Abort()
            return
        }
        tokenString := authHeader[7:]

        secret := cfg.Auth.JWTSecret
        if secret == "" {
            secret = cfg.Security.JWTSecret
        }

        token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
            return []byte(secret), nil
        })
        if err != nil || !token.Valid {
            c.JSON(http.StatusUnauthorized, model.APIResponse{Code: 401, Message: "Invalid token"})
            c.Abort()
            return
        }

        claims, ok := token.Claims.(jwt.MapClaims)
        if !ok {
            c.JSON(http.StatusUnauthorized, model.APIResponse{Code: 401, Message: "Invalid claims"})
            c.Abort()
            return
        }

        c.Set("username", claims["sub"])
        c.Set("role", claims["role"])
        c.Set("display_name", claims["display_name"])
        c.Next()
    }
}

// ChangePassword 修改密码 handler
func ChangePassword(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        var req model.ChangePasswordRequest
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, model.APIResponse{Code: 400, Message: err.Error()})
            return
        }

        username, _ := c.Get("username")
        keyPair := findAPIKeyPair(cfg, username.(string))
        if keyPair == nil {
            c.JSON(http.StatusNotFound, model.APIResponse{Code: 404, Message: "User not found"})
            return
        }

        // 验证旧密码
        if err := bcrypt.CompareHashAndPassword([]byte(keyPair.Secret), []byte(req.OldPassword)); err != nil {
            c.JSON(http.StatusUnauthorized, model.APIResponse{Code: 401, Message: "Old password incorrect"})
            return
        }

        // 生成新密码哈希
        newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
        if err != nil {
            c.JSON(http.StatusInternalServerError, model.APIResponse{Code: 500, Message: "Failed to hash password"})
            return
        }

        // 更新内存中的配置（注意：需要持久化到配置文件才能永久生效）
        keyPair.Secret = string(newHash)

        c.JSON(http.StatusOK, model.APIResponse{
            Code: 200, Message: "Password changed successfully. Restart required to persist.",
        })
    }
}
```

对应的 Model 更新（`model/types.go` 中 `LoginRequest` 使用 api_key/api_secret）：

```go
type LoginRequest struct {
    APIKey    string `json:"api_key" binding:"required"`
    APISecret string `json:"api_secret" binding:"required"`
}
```

Server 路由中，所有操作路由注册在 `authed` 组下（当前所有账户为 root 全权限）：

```go
// 当前阶段无角色区分，所有已认证用户拥有全部权限
// 后续扩展 RBAC 时可在此添加 AdminOnly() 等中间件
{
    authed.POST("/tables", dataHandler.CreateTable)
    authed.PUT("/tables/:name", dataHandler.UpdateTable)
    authed.DELETE("/tables/:name", dataHandler.DeleteTable)
    authed.POST("/tables/:name/data", dataHandler.WriteData)
    authed.POST("/tables/:name/data/batch", dataHandler.BatchWriteData)
    authed.PUT("/tables/:name/data/:id", dataHandler.UpdateData)
    authed.DELETE("/tables/:name/data/:id", dataHandler.DeleteData)
    authed.POST("/backups/metadata", backupHandler.CreateMetadata)
    authed.POST("/backups/full", backupHandler.CreateFull)
    authed.POST("/backups/table/:name", backupHandler.CreateTable)
    authed.POST("/backups/:id/restore", backupHandler.Restore)
    authed.DELETE("/backups/:id", backupHandler.Delete)
    authed.PUT("/backups/schedule", backupHandler.UpdateSchedule)
    authed.POST("/cluster/convert-to-distributed", clusterHandler.ConvertToDistributed)
    authed.POST("/auth/change-password", handler.ChangePassword(s.cfg))
}
```

**验证**：`go build -tags dashboard ./internal/dashboard/handler/...`

---

## Phase 3：Next.js SPA 骨架（2-3 天）

> **前端设计规范**：本阶段及后续所有前端页面开发，执行前必须先读取
> `~/.claude/skills/frontend-design/SKILL.md` 及其关联文件（`stack.md`、`typography.md`、
> `colors.md`、`mobile.md`、`animation.md`）中的设计规范并严格遵循。
> 技术栈：Next.js（`output: 'export'` 静态导出）+ shadcn/ui + Framer Motion + Tailwind CSS + ECharts + Zustand。

### TASK-D016：初始化 Next.js 工程

```bash
cd dashboard-ui
npx create-next-app@latest . --typescript --tailwind --eslint --app --src-dir --import-alias "@/*"
# 安装 shadcn/ui
npx shadcn@latest init
npx shadcn@latest add button card dialog table tabs sheet accordion navigation-menu badge separator skeleton toast dropdown-menu input label select textarea
# 安装其他依赖
npm install framer-motion zustand dayjs
npm install echarts echarts-for-react
npm install @codemirror/lang-sql @codemirror/view @codemirror/state codemirror
```

**next.config.ts**（静态导出，供 Go `embed.FS` 嵌入）：

```typescript
import type { NextConfig } from 'next'

const nextConfig: NextConfig = {
  output: 'export',
  basePath: '/dashboard/ui',
  trailingSlash: true,
  images: {
    unoptimized: true,
  },
}

export default nextConfig
```

**lib/utils.ts**（shadcn/ui 工具函数）：

```typescript
import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

**验证**：`npm run build` 生成 `out/` 目录（静态文件）。

---

### TASK-D017：创建 App Router 路由和布局

Next.js App Router 使用文件系统路由，无需手动配置 router。

**目录结构**（即路由结构）：

```
src/app/
├── layout.tsx              # 根布局（字体、全局样式、ThemeProvider）
├── page.tsx                # / → 重定向到 /dashboard/ui
└── dashboard/ui/
    ├── layout.tsx           # Dashboard 布局（侧边栏+顶栏+内容区）
    ├── page.tsx             # Overview 总览（默认页）
    ├── login/page.tsx       # 登录页（独立布局，无侧边栏）
    ├── cluster/page.tsx     # 集群
    ├── nodes/page.tsx       # 节点
    ├── data/page.tsx        # 数据浏览
    ├── logs/page.tsx        # 日志
    ├── backup/page.tsx      # 备份
    ├── monitor/page.tsx     # 监控
    └── analytics/page.tsx   # 统计分析
```

**src/app/dashboard/ui/layout.tsx** — 侧边栏 + 顶栏 + 内容区：

```tsx
'use client'

import { usePathname } from 'next/navigation'
import { Sidebar } from '@/components/layout/sidebar'
import { TopBar } from '@/components/layout/top-bar'

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  if (pathname?.includes('/login')) return <>{children}</>

  return (
    <div className="flex h-screen">
      <Sidebar />
      <div className="flex-1 flex flex-col overflow-hidden">
        <TopBar />
        <main className="flex-1 overflow-y-auto p-6">{children}</main>
      </div>
    </div>
  )
}
```

侧边栏使用 shadcn/ui `NavigationMenu` + `Sheet`（移动端抽屉），导航项通过 `next/link` 实现。

**src/lib/api/client.ts** — 统一 HTTP 客户端，自动携带 JWT Token：

```typescript
const BASE_URL = '/dashboard/api/v1'

class APIClient {
  private token: string | null = null

  setToken(token: string) { this.token = token }
  clearToken() { this.token = null }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    if (this.token) headers['Authorization'] = `Bearer ${this.token}`

    const resp = await fetch(`${BASE_URL}${path}`, {
      method, headers,
      body: body ? JSON.stringify(body) : undefined,
    })

    const json = await resp.json()
    if (json.code !== 200) throw new Error(json.message)
    return json.data
  }

  get<T>(path: string) { return this.request<T>('GET', path) }
  post<T>(path: string, body: unknown) { return this.request<T>('POST', path, body) }
  put<T>(path: string, body: unknown) { return this.request<T>('PUT', path, body) }
  del<T>(path: string) { return this.request<T>('DELETE', path) }
}

export const apiClient = new APIClient()
```

**src/hooks/use-sse.ts** — SSE 连接管理 Hook：

```typescript
import { useEffect, useRef, useCallback } from 'react'

export function useSSE(url: string, topics: string[], onEvent: (event: any) => void) {
  const eventSourceRef = useRef<EventSource | null>(null)

  const connect = useCallback(() => {
    const params = topics.map(t => `topics=${t}`).join('&')
    const es = new EventSource(`${url}?${params}`)

    es.onmessage = (e) => {
      try { onEvent(JSON.parse(e.data)) }
      catch { /* ignore parse errors */ }
    }

    es.onerror = () => {
      es.close()
      setTimeout(connect, 3000) // 自动重连
    }

    eventSourceRef.current = es
  }, [url, topics, onEvent])

  useEffect(() => {
    connect()
    return () => eventSourceRef.current?.close()
  }, [connect])
}
```

**验证**：`npm run build` 成功，`npm run dev` 可看到空白布局页面。

---

## Phase 4：核心页面（5-7 天）

> **设计规范**：遵循 `frontend-design` Skill，参见 Phase 3 说明。

### TASK-D018：Overview 总览页

展示内容：
- 集群模式标识卡片（单节点/分布式）
- 4 个 KPI 卡片：节点数、表数量、数据总量、健康状态
- QPS 迷你折线图（ECharts，SSE 实时更新）
- 最近写入/查询速率
- 系统资源使用条形图（内存、goroutine）

数据来源：`GET /cluster/info` + SSE `metrics` 主题。

---

### TASK-D019：Cluster 集群页

展示内容：
- 集群拓扑可视化（ECharts graph 图类型）
  - 节点为圆形、MinIO 为矩形、Redis 为菱形
  - 连线表示通信关系
  - 节点颜色表示健康状态（绿/黄/红）
- 模式切换区域：
  - 单节点模式：显示"转换为分布式"按钮
  - 分布式模式：显示节点数量和哈希环分布
- 配置查看器（只读，敏感信息脱敏）

数据来源：`GET /cluster/info`、`GET /cluster/topology`、`GET /cluster/config`。

---

### TASK-D020：Nodes 节点页

展示内容：
- 节点列表表格（shadcn/ui Table）
  - 列：ID、地址、状态、运行时间、数据分布百分比、最后心跳
- 节点详情抽屉（Drawer 组件）
  - 资源指标：CPU、内存、goroutine、连接数
  - Prometheus 指标图表
- 扩缩容辅助：
  - "扩容检查"按钮 → 输入新节点数 → 显示预估的哈希环重分布
  - 生成配置 YAML 片段

数据来源：`GET /nodes`、`GET /nodes/:id`、`POST /nodes/scale-check`。

---

### TASK-D021：Monitor 监控页

展示内容（全部 SSE 实时更新）：
- QPS 折线图（HTTP + gRPC 请求总量）
- 延迟分布图（P50/P95/P99）
- 缓冲区使用量面积图
- MinIO/Redis 操作速率
- Go 运行时资源（内存分配、GC 停顿、goroutine 数量）
- SLA 仪表盘（目标 vs 实际）

ECharts 配置使用 `echarts-for-react`，通过 `use-sse` hook 接收 `metrics` 事件并实时更新图表。

每个图表组件维护一个固定大小的时间窗口数组（如最近 60 个数据点 = 5 分钟），SSE 推送新数据时追加并移除最旧的点。

---

## Phase 5：数据页面（4-5 天）

> **设计规范**：遵循 `frontend-design` Skill，参见 Phase 3 说明。

### TASK-D022：Data 数据浏览页

展示内容：
- 左侧面板：表列表，点击选中
- 右侧面板：
  - 表信息卡片（配置、行数估算）
  - 数据网格：分页展示（shadcn/ui Table），支持列排序
  - 底部分页器
- 数据操作：导出 CSV/JSON

数据来源：`GET /tables`、`GET /tables/:name/data?page=1&page_size=50`。

---

### TASK-D023：SQL Query Console

展示内容：
- 上方：CodeMirror 6 SQL 编辑器（语法高亮、自动补全）
  - 自动补全从 `GET /analytics/suggestions` 获取表名和列名
- 执行按钮 + 耗时显示
- 下方：查询结果表格
- 右侧面板：查询历史列表

```tsx
// 使用 CodeMirror 6 的 SQL 语言支持
import { sql } from '@codemirror/lang-sql'
import { EditorView } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
```

数据来源：`POST /query`、`GET /query/history`。

---

### TASK-D024：Analytics 统计分析页

展示内容：
- 数据量趋势图（按天统计写入量）
- 各表数据分布饼图
- 列级统计卡片（选择表后展示各列 min/max/distinct/null_rate）
- 写入/查询速率对比图

数据来源：`GET /analytics/overview`、`GET /tables/:name/stats`。

---

## Phase 6：运维页面（4-5 天）

> **设计规范**：遵循 `frontend-design` Skill，参见 Phase 3 说明。

### TASK-D025：Logs 日志查看器

展示内容：
- 筛选栏：级别多选（DEBUG/INFO/WARN/ERROR）、时间范围选择器、关键词输入
- 虚拟化日志列表：高性能展示大量日志（大数据量时按需分页加载）
- 实时尾随开关：开启后通过 SSE `logs` 主题接收新日志
- 日志详情展开：点击单条日志展示完整 JSON 字段
- 日志文件管理：列表 + 下载

数据来源：`GET /logs`、`GET /logs/stream`（SSE）、`GET /logs/files`。

---

### TASK-D026：Backup 备份管理页

展示内容：
- **备份列表表格**：ID、类型（metadata/full/table）、包含的表、时间、大小、状态、操作按钮
- **新建备份**：
  - 元数据备份（一键触发）
  - 全量备份（确认对话框 → 进度反馈）
  - 表级备份（选择表 → 备份说明 → 触发）
- **恢复向导**：
  - 选择恢复模式：全量恢复 / 选表恢复
  - 表级恢复支持：覆盖恢复 / 恢复为新表（自定义目标表名）
  - 恢复前警告提示 → 确认 → 进度反馈
- **备份详情页**：备份 ID、类型、大小、创建时间、包含的表列表、验证状态
- **操作**：下载备份、验证完整性、删除备份
- **计划配置**：元数据备份间隔、全量备份间隔、保留备份数

数据来源：`GET /backups`、`POST /backups/metadata`、`POST /backups/full`、`POST /backups/table/:name`、`POST /backups/:id/restore`、`GET /backups/:id/download`、`POST /backups/validate/:id`、`GET/PUT /backups/schedule`。

---

## Phase 7：Docker 集成（2-3 天）

### TASK-D027：创建 Dockerfile.dashboard

**文件**：`Dockerfile.dashboard`

```dockerfile
# Stage 1: Build Next.js SPA
FROM node:22-alpine AS ui-builder
WORKDIR /app/dashboard-ui
COPY dashboard-ui/package*.json ./
RUN npm ci --production=false
COPY dashboard-ui/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24 AS builder
WORKDIR /app
ENV CGO_ENABLED=1 GOOS=linux GOPROXY='https://goproxy.cn,direct'
RUN apt-get update && apt-get install -y gcc g++ libc6-dev pkg-config && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /app/dashboard-ui/out ./internal/dashboard/static
RUN go build -ldflags="-s -w" -o dashboard cmd/dashboard/main.go

# Stage 3: Runtime
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*
RUN groupadd -g 1001 dashboard && useradd -u 1001 -g dashboard -m dashboard
WORKDIR /app
COPY --from=builder /app/dashboard /app/dashboard
COPY config/config.yaml /app/config/config.yaml
RUN chown -R dashboard:dashboard /app
USER dashboard
EXPOSE 9090
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD curl -f http://localhost:9090/dashboard/api/v1/cluster/info || exit 1
ENTRYPOINT ["/app/dashboard"]
```

---

### TASK-D028：修改主 Dockerfile 支持 All-in-One

在现有 `Dockerfile` 中支持可选的 `BUILD_TAGS` 参数：

在 Go 构建阶段前添加 `ARG BUILD_TAGS=""`，修改 build 命令：

```dockerfile
ARG BUILD_TAGS=""
RUN go build -tags "${BUILD_TAGS}" -ldflags="-s -w" -o miniodb cmd/main.go
```

All-in-One 构建需要增加前端构建阶段（与 `Dockerfile.dashboard` 类似）。

---

### TASK-D029：Docker Compose 配置

**文件**：`deploy/docker/docker-compose.dashboard.yml`

```yaml
version: '3.8'

services:
  dashboard:
    build:
      context: ../..
      dockerfile: Dockerfile.dashboard
    container_name: miniodb-dashboard
    ports:
      - "9090:9090"
    environment:
      - DASHBOARD_ENABLED=true
      - DASHBOARD_CORE_ENDPOINT=http://miniodb:8081
      - DASHBOARD_CORE_GRPC_ENDPOINT=miniodb:8080
      - DASHBOARD_SESSION_SECRET=change-me-in-production
    volumes:
      - ../../config/config.yaml:/app/config/config.yaml:ro
      - miniodb-logs:/app/logs:ro
    depends_on:
      miniodb:
        condition: service_healthy
    networks:
      - miniodb-network
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/dashboard/api/v1/cluster/info"]
      interval: 30s
      timeout: 5s
      retries: 3
    restart: unless-stopped

volumes:
  miniodb-logs:
    external: true

networks:
  miniodb-network:
    external: true
```

**验证**：`docker compose -f deploy/docker/docker-compose.dashboard.yml config` 验证语法。

---

## Phase 8：节点扩缩容（3-4 天）

### TASK-D030：核心新增 ConvertToDistributed 方法

**文件**：`internal/service/miniodb_service.go`

在 `MinIODBService` 中新增方法：

```go
type RedisConvertConfig struct {
    Addr     string `json:"addr"`
    Password string `json:"password"`
    Mode     string `json:"mode"`
    DB       int    `json:"db"`
}

func (s *MinIODBService) ConvertToDistributed(ctx context.Context, redisCfg RedisConvertConfig) error {
    // 1. 验证当前为单节点模式
    if s.cfg.Redis.Enabled || s.cfg.Network.Pools.Redis.Enabled {
        return fmt.Errorf("already in distributed mode")
    }

    // 2. 测试 Redis 连通性
    testClient := redis.NewClient(&redis.Options{
        Addr:     redisCfg.Addr,
        Password: redisCfg.Password,
        DB:       redisCfg.DB,
    })
    defer testClient.Close()
    if err := testClient.Ping(ctx).Err(); err != nil {
        return fmt.Errorf("redis connectivity test failed: %w", err)
    }

    // 3. 更新运行配置
    s.cfg.Redis.Enabled = true
    s.cfg.Redis.Addr = redisCfg.Addr
    s.cfg.Redis.Password = redisCfg.Password
    s.cfg.Redis.Mode = redisCfg.Mode
    s.cfg.Network.Pools.Redis.Enabled = true
    s.cfg.Network.Pools.Redis.Address = redisCfg.Addr
    s.cfg.Network.Pools.Redis.Password = redisCfg.Password

    // 4. 初始化 Redis 连接池（需要连接池管理器支持热加载）
    // 5. 注册到服务发现
    // 6. 持久化配置变更

    s.logger.Info("Converted to distributed mode",
        zap.String("redis_addr", redisCfg.Addr),
        zap.String("redis_mode", redisCfg.Mode))

    return nil
}
```

注意：完整实现需要 `PoolManager` 支持运行时重新初始化 Redis 连接池，`ServiceRegistry` 支持延迟启动。建议此阶段以"配置变更 + 重启提示"为最小可行方案。

---

### TASK-D031：核心新增节点列表 API

**文件**：`internal/transport/rest/server.go`

在路由注册区域新增：

```go
authed.GET("/v1/nodes", s.listNodes)
authed.GET("/v1/nodes/:id", s.getNode)
```

Handler 实现：

```go
func (s *Server) listNodes(c *gin.Context) {
    if s.serviceRegistry == nil {
        // 单节点模式，返回当前节点
        c.JSON(http.StatusOK, gin.H{
            "nodes": []gin.H{{
                "id":      s.cfg.Server.NodeID,
                "address": "localhost",
                "port":    s.cfg.Server.RestPort,
                "status":  "healthy",
            }},
        })
        return
    }
    nodes := s.serviceRegistry.DiscoverNodes()
    c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}
```

需要在 `Server` 结构体中添加 `serviceRegistry *discovery.ServiceRegistry` 字段，并在 `cmd/main.go` 中传入。

---

### TASK-D032：前端扩缩容引导 UI

在 Cluster 页面和 Nodes 页面中实现：

- **转换对话框**：
  - 输入 Redis 连接信息（地址、密码、模式选择）
  - 连通性测试按钮
  - 确认转换（含警告："此操作不可逆"）

- **扩容引导**：
  - 输入期望节点数
  - 显示当前哈希环分布 vs 预估新分布
  - 生成配置 YAML 片段和 docker-compose 片段供复制

---

## 任务依赖关系

```
Phase 1（可并行）:
  D001 (目录) ──独立──
  D002 (配置) ──独立──
  D003 (接口) ──独立──
  D004 (Build Tag) ← 依赖 D003
  D005 (Embed) ──独立──
  D006 (Makefile) ──独立──
  D007 (入口) ← 依赖 D002, D003

Phase 2（依赖 Phase 1）:
  D008 (SSE Hub) ──独立──
  D009 (Model) ──独立──
  D010 (LocalClient) ← 依赖 D003
  D011 (RemoteClient) ← 依赖 D003
  D012 (Server) ← 依赖 D004, D005, D008
  D013 (Handlers) ← 依赖 D009, D012
  D014 (Services) ← 依赖 D008, D010/D011
  D015 (Auth) ← 依赖 D002

Phase 3（独立，可与 Phase 2 并行）:
  D016 (Next.js 工程) ──独立──
  D017 (路由布局) ← 依赖 D016

Phase 4-6（依赖 Phase 2 + Phase 3）:
  D018-D026 按页面独立并行开发

Phase 7（依赖 Phase 1-6）:
  D027-D029 Docker 构建与配置

Phase 8（依赖 Phase 2）:
  D030 (核心方法) ──独立──
  D031 (核心 API) ──独立──
  D032 (前端 UI) ← 依赖 Phase 4 + D030
```

---

## 验收标准

| 阶段 | 验收条件 |
|------|---------|
| Phase 1 | `go build ./...` 和 `go build -tags dashboard ./...` 均编译通过；`make build` 不受影响 |
| Phase 2 | 所有 Dashboard API 可通过 curl 测试；SSE 端点推送数据；JWT 认证生效 |
| Phase 3 | `npm run build` 产出 `out/` 目录（静态导出）；`npm run dev` 页面可访问 |
| Phase 4 | 总览、集群、节点、监控 4 个核心页面功能完整；SSE 实时图表正常刷新 |
| Phase 5 | 数据浏览分页正常；SQL 控制台可执行查询；统计图表正确展示 |
| Phase 6 | 日志可查询可实时尾随；全量/表级备份可创建/恢复/下载/验证/删除；计划配置可保存 |
| Phase 7 | `docker build` 成功；All-in-One 模式和独立部署模式均可工作 |
| Phase 8 | 单节点转分布式流程完整；扩容引导生成正确配置 |

---

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| CGO + Node.js 双构建链 | Docker 构建时间增加 | 多阶段构建，前端构建缓存 npm cache |
| SSE 长连接资源占用 | 大量客户端时内存增长 | Hub 层限制最大客户端数，超时自动断开 |
| 日志文件直读方式 | 大日志文件性能问题 | 仅读取最近 N 行，支持按文件分页 |
| Build Tag 条件编译 | 开发体验复杂 | IDE 配置 `-tags dashboard`，Makefile 简化命令 |
| 单节点转分布式热切换 | 运行时状态一致性 | 最小方案：配置变更 + 提示重启；后续迭代支持热加载 |

---

**文档版本**：v1.0  
**生成日期**：2026-03-13  
**参考**：`docs/DASHBOARD_ARCHITECTURE.md` v1.0
