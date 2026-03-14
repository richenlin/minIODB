# MinIODB Dashboard 管理控制台架构设计

**设计日期**：2026-03-13  
**版本**：v1.2  
**设计目标**：轻量化、高性能、可水平扩展、与核心程序解耦

---

## 一、设计原则

- **解耦分离**：Dashboard 是独立模块，不侵入 minIODB 核心业务逻辑，通过接口抽象层与核心通信
- **轻量化**：Next.js 静态导出 SPA 通过 `embed.FS` 嵌入 Go 二进制，运行时无 Node.js 依赖
- **双模部署**：支持 All-in-One（单二进制）和 Client+Server（独立镜像）两种部署模式
- **API 透传**：Dashboard 后端不复制核心逻辑，通过 `CoreClient` 接口调用现有 minIODB REST/gRPC API
- **SSE 实时推送**：Server-Sent Events 用于监控指标、日志流、节点健康状态的实时推送

---

## 二、整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                          Browser                                  │
│              Next.js SPA (shadcn/ui + Tailwind + ECharts)        │
└───────────────────────────┬──────────────────────────────────────┘
                            │ HTTP /dashboard/api/*
                            │ SSE  /dashboard/api/v1/events
┌───────────────────────────▼──────────────────────────────────────┐
│                    Dashboard Server                               │
│  ┌────────────────┐  ┌──────────────────┐  ┌─────────────────┐  │
│  │ Dashboard API   │  │ Dashboard Service │  │   SSE Hub       │  │
│  │ (Gin Handler)   │──│ (Business Logic)  │──│ (Event Broker)  │  │
│  └────────┬───────┘  └────────┬─────────┘  └───────┬─────────┘  │
│           └───────────────────┼──────────────────────┘            │
│                               │                                    │
│                    ┌──────────▼──────────┐                        │
│                    │   CoreClient IF     │                        │
│                    │ ┌────────┬────────┐ │                        │
│                    │ │ Local  │ Remote │ │                        │
│                    │ │ Client │ Client │ │                        │
│                    │ └────┬───┴────┬───┘ │                        │
│                    └──────┼────────┼─────┘                        │
└───────────────────────────┼────────┼─────────────────────────────┘
          All-in-One ───────┘        └─────── Standalone
          (Direct Go call)           (HTTP/gRPC)
┌───────────────────────────────────────────────────────────────────┐
│                     MinIODB Core                                   │
│          REST API :8081 / gRPC :8080 / Prometheus :8082           │
└───────────────────────────────────────────────────────────────────┘
```

---

## 三、部署模式

### 模式 A：All-in-One（单二进制部署）

```
┌──────────────────────────────────────────────┐
│         Single Binary (go build -tags dashboard)  │
│                                                    │
│  Gin Engine :8081                                  │
│  ├── /v1/*           → Core API Routes            │
│  ├── /dashboard/api/* → Dashboard API Routes      │
│  └── /dashboard/ui/*  → Embedded Next.js SPA      │
└──────────────────────────────────────────────┘
```

- Dashboard 路由挂载在已有的 Gin Router 上，路径前缀 `/dashboard/`
- 通过 `go build -tags dashboard ./cmd/` 编译时包含 Dashboard
- Dashboard Service 通过 `LocalClient` 直接调用核心 Go 接口（零网络开销）
- 单二进制、单端口、单容器

### 模式 B：Client + Server（独立镜像部署）

```
┌─────────────────────────┐      ┌──────────────────────┐
│  Dashboard Image :9090  │      │  MinIODB Image :8081  │
│                         │      │                       │
│  dashboard binary       │      │  miniodb binary       │
│  Embedded Next.js SPA   │      │  REST/gRPC API        │
│  HTTP/gRPC Client ──────│──────│→ Core Services        │
└─────────────────────────┘      └──────────────────────┘
```

- Dashboard 作为独立 Go 二进制 (`cmd/dashboard/main.go`) 运行在独立容器
- 通过 REST/gRPC 与 minIODB Core 通信（配置 endpoint）
- 前端 SPA 嵌入 Dashboard 二进制
- 独立 Docker 镜像：`miniodb-dashboard:VERSION`

---

## 四、目录结构

```
minIODB/
├── cmd/
│   ├── main.go                              # 现有核心入口
│   └── dashboard/
│       └── main.go                          # 独立 Dashboard 入口
├── internal/
│   └── dashboard/                           # Dashboard 后端（Go）
│       ├── server.go                        # Dashboard HTTP Server
│       ├── embed.go                         # go:embed 前端静态文件
│       ├── mount.go                         # All-in-One 挂载（build tag: dashboard）
│       ├── mount_stub.go                    # 空实现（build tag: !dashboard）
│       ├── handler/                         # HTTP 处理器（Gin）
│       │   ├── cluster.go                   # 集群信息与模式转换
│       │   ├── node.go                      # 节点列表、健康、扩缩容
│       │   ├── data.go                      # 数据浏览、查询、导出
│       │   ├── log.go                       # 日志查询与流式推送
│       │   ├── backup.go                    # 备份 CRUD 与恢复
│       │   ├── monitor.go                   # 监控概览与 SSE 推送
│       │   └── analytics.go                 # 统计分析与 SQL 控制台
│       ├── service/                         # 业务逻辑层
│       │   ├── cluster_service.go           # 集群管理
│       │   ├── node_service.go              # 节点管理
│       │   ├── data_service.go              # 数据管理
│       │   ├── log_service.go               # 日志管理
│       │   ├── backup_service.go            # 备份管理
│       │   ├── monitor_service.go           # 监控服务
│       │   └── analytics_service.go         # 分析服务
│       ├── client/                          # 核心客户端抽象
│       │   ├── interface.go                 # CoreClient 接口定义
│       │   ├── local.go                     # 进程内实现（All-in-One）
│       │   └── remote.go                    # HTTP/gRPC 实现（独立部署）
│       ├── sse/                             # SSE 事件中心
│       │   └── hub.go                       # 事件广播器
│       └── model/                           # Dashboard 数据模型
│           └── types.go                     # 请求/响应类型定义
├── dashboard-ui/                            # Next.js SPA（非 Go 包）
│   ├── package.json
│   ├── next.config.ts                       # Next.js 配置（output: 'export'）
│   ├── tailwind.config.ts
│   ├── tsconfig.json
│   ├── components.json                      # shadcn/ui 配置
│   ├── src/
│   │   ├── app/                             # Next.js App Router
│   │   │   ├── layout.tsx                   # 根布局
│   │   │   ├── page.tsx                     # 重定向到 /dashboard/ui
│   │   │   └── dashboard/ui/
│   │   │       ├── layout.tsx               # Dashboard 布局（侧边栏+顶栏）
│   │   │       ├── page.tsx                 # Overview 总览
│   │   │       ├── login/page.tsx           # 登录页
│   │   │       ├── cluster/page.tsx         # 集群拓扑与模式
│   │   │       ├── nodes/page.tsx           # 节点管理
│   │   │       ├── data/page.tsx            # 数据浏览与查询控制台
│   │   │       ├── logs/page.tsx            # 日志查看器
│   │   │       ├── backup/page.tsx          # 备份管理
│   │   │       ├── monitor/page.tsx         # 实时监控
│   │   │       └── analytics/page.tsx       # 统计分析
│   │   ├── components/
│   │   │   ├── ui/                          # shadcn/ui 组件
│   │   │   ├── charts/                      # ECharts 图表封装
│   │   │   ├── code-editor/                 # SQL 编辑器封装
│   │   │   └── layout/                      # 侧边栏、顶栏等布局组件
│   │   ├── lib/
│   │   │   ├── utils.ts                     # cn() 等工具函数
│   │   │   └── api/                         # API 客户端层
│   │   │       ├── client.ts                # HTTP 客户端封装
│   │   │       ├── cluster.ts               # 集群 API
│   │   │       ├── nodes.ts                 # 节点 API
│   │   │       ├── data.ts                  # 数据 API
│   │   │       ├── logs.ts                  # 日志 API
│   │   │       ├── backup.ts                # 备份 API
│   │   │       └── monitor.ts               # 监控 API
│   │   ├── hooks/
│   │   │   ├── use-sse.ts                   # SSE 连接管理
│   │   │   └── use-query.ts                 # 数据请求封装
│   │   └── stores/                          # Zustand 状态管理
│   │       ├── auth-store.ts                # 认证状态
│   │       └── cluster-store.ts             # 集群状态
│   └── out/                                 # 静态导出产物（被 Go embed）
├── Dockerfile.dashboard                     # Dashboard 独立镜像
└── deploy/docker/docker-compose.dashboard.yml
```

---

## 五、核心客户端抽象层

所有 Dashboard Service 依赖 `CoreClient` 接口，通过两种实现完成解耦：

```go
// internal/dashboard/client/interface.go
type CoreClient interface {
    // 集群
    GetHealth(ctx context.Context) (*HealthStatus, error)
    GetStatus(ctx context.Context) (*SystemStatus, error)
    GetMetrics(ctx context.Context) (*Metrics, error)
    GetNodes(ctx context.Context) ([]NodeInfo, error)

    // 表与数据
    ListTables(ctx context.Context) ([]TableInfo, error)
    GetTable(ctx context.Context, name string) (*TableDetail, error)
    CreateTable(ctx context.Context, req CreateTableReq) error
    DeleteTable(ctx context.Context, name string) error
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

    // Prometheus 原始指标
    ScrapePrometheus(ctx context.Context) ([]byte, error)
}
```

- **`LocalClient`**（All-in-One）：持有 `*service.MinIODBService`、`*metadata.Manager` 等直接引用，零序列化开销
- **`RemoteClient`**（独立部署）：使用 `net/http` + `google.golang.org/grpc` 调用 minIODB REST/gRPC 端点

---

## 六、Dashboard API 设计

所有 Dashboard API 前缀为 `/dashboard/api/v1/`。

### 6.1 认证管理

使用核心 `auth.api_key_pairs` 统一账户，详细设计见 **第十一节**。

| 方法 | 路径 | 描述 | 认证 |
|------|------|------|------|
| POST | `/auth/login` | 登录（api_key + api_secret），返回 JWT Token | 无 |
| POST | `/auth/logout` | 登出，废弃当前 Token | 需要 |
| GET | `/auth/me` | 获取当前用户信息（api_key、role、display_name） | 需要 |
| POST | `/auth/change-password` | 修改密码（验证旧密码，bcrypt 新密码写回配置） | 需要 |

### 6.2 集群管理

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/cluster/info` | 集群概览（模式、节点数、数据统计） |
| GET | `/cluster/topology` | 节点拓扑图数据 |
| POST | `/cluster/convert-to-distributed` | 单向：单节点转分布式 |
| GET | `/cluster/config` | 当前运行配置（脱敏） |

**单节点转分布式流程：**

```
Dashboard UI                 Dashboard API                MinIODB Core
    │                             │                            │
    │ POST /convert-to-distributed│                            │
    │ {redis_addr, password, mode}│                            │
    │────────────────────────────>│                            │
    │                             │ 1. 验证当前为单节点模式      │
    │                             │ 2. 测试 Redis 连通性        │
    │                             │────────────────────────────>│
    │                             │ 3. 初始化 Redis 连接池      │
    │                             │ 4. 注册节点到服务发现        │
    │                             │ 5. 启动心跳 goroutine       │
    │                             │ 6. 更新运行配置              │
    │                             │ 7. 持久化配置到磁盘          │
    │                             │<────────────────────────────│
    │ 200 OK {message, new_mode}  │                            │
    │<────────────────────────────│                            │
    │                             │                            │
    │  注: 模式转换为单向操作，建议重启以完全生效                   │
```

核心需要新增的方法：

```go
// internal/service/miniodb_service.go
func (s *MinIODBService) ConvertToDistributed(ctx context.Context, redisConfig RedisConvertConfig) error
```

### 6.3 节点管理

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/nodes` | 节点列表（含健康状态） |
| GET | `/nodes/:id` | 节点详情（资源用量、任务） |
| GET | `/nodes/:id/metrics` | 节点级 Prometheus 指标 |
| POST | `/nodes/scale-check` | 扩缩容前置检查 |

扩缩容通过部署工具完成（Docker/K8s），Dashboard 提供：
- **可视化**：节点健康状态、数据分布
- **引导**：生成新节点配置 YAML / docker-compose 片段
- **校验**：Redis 连通性检查、哈希环平衡预估

### 6.3 表管理

| 方法 | 路径 | 描述 | 权限 |
|------|------|------|------|
| GET | `/tables` | 表列表（含行数估算、大小） | authed |
| GET | `/tables/:name` | 表结构、配置、统计信息 | authed |
| POST | `/tables` | 创建新表（指定名称和配置） | authed |
| PUT | `/tables/:name` | 修改表配置（buffer_size、flush_interval 等） | authed |
| DELETE | `/tables/:name` | 删除表（含确认机制） | authed |
| GET | `/tables/:name/stats` | 列级统计（min, max, distinct, null_rate） | authed |

**创建表请求体**：
```json
{
  "table_name": "orders",
  "config": {
    "buffer_size": 1000,
    "flush_interval_seconds": 30,
    "retention_days": 365,
    "backup_enabled": true,
    "id_strategy": "snowflake",
    "id_prefix": "ord"
  }
}
```

**修改表配置请求体**（仅允许修改运行时参数，不允许修改 id_strategy 等不可变配置）：
```json
{
  "config": {
    "buffer_size": 2000,
    "flush_interval_seconds": 60,
    "retention_days": 180,
    "backup_enabled": false
  }
}
```

### 6.4 数据管理（CRUD）

| 方法 | 路径 | 描述 | 权限 |
|------|------|------|------|
| GET | `/tables/:name/data` | 分页数据浏览（支持过滤和排序） | authed |
| POST | `/tables/:name/data` | 写入单条数据 | authed |
| POST | `/tables/:name/data/batch` | 批量写入数据 | authed |
| PUT | `/tables/:name/data/:id` | 更新指定数据 | authed |
| DELETE | `/tables/:name/data/:id` | 删除指定数据（需提供 day 参数） | authed |
| POST | `/query` | 执行 Ad-hoc SQL 查询（SELECT only） | authed |
| GET | `/query/history` | 最近查询历史 | authed |

**数据浏览参数** `GET /tables/:name/data`：

| 参数 | 类型 | 描述 |
|------|------|------|
| page | int | 页码（默认 1） |
| page_size | int | 每页行数（默认 50，最大 500） |
| sort_by | string | 排序字段 |
| sort_order | string | asc / desc |
| filter | string | 简单过滤表达式（如 `age>25`） |
| id | string | 按 ID 精确查找 |

**写入单条数据请求体**：
```json
{
  "id": "user-001",
  "timestamp": "2026-03-13T10:00:00Z",
  "payload": {
    "name": "张三",
    "age": 25,
    "city": "北京"
  }
}
```

**批量写入请求体**：
```json
{
  "records": [
    {"id": "user-001", "timestamp": "...", "payload": {...}},
    {"id": "user-002", "timestamp": "...", "payload": {...}}
  ]
}
```

**更新数据请求体**：
```json
{
  "timestamp": "2026-03-13T10:00:00Z",
  "payload": {
    "name": "张三",
    "age": 26,
    "city": "上海"
  }
}
```

**删除数据参数**：`DELETE /tables/:name/data/:id?day=2026-03-13`

### 6.5 日志管理

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/logs` | 查询日志条目（级别、时间、关键词过滤） |
| GET | `/logs/stream` | SSE 实时日志尾随 |
| GET | `/logs/files` | 列出日志文件（lumberjack 轮转） |
| GET | `/logs/files/:name/download` | 下载指定日志文件 |

实现方式：读取日志目录中的文件，解析 Zap JSON 结构化日志，提供过滤和搜索。

### 6.6 备份与恢复

详细设计见 **第十二节 数据备份与恢复**。

| 方法 | 路径 | 描述 | 权限 |
|------|------|------|------|
| GET | `/backups` | 备份列表（元数据 + 数据备份） | authed |
| POST | `/backups/metadata` | 触发元数据手动备份 | authed |
| POST | `/backups/full` | 触发全量数据备份 | authed |
| POST | `/backups/table/:name` | 触发指定表备份 | authed |
| GET | `/backups/:id` | 备份详情 | authed |
| POST | `/backups/:id/restore` | 从备份恢复（全量/指定表） | authed |
| DELETE | `/backups/:id` | 删除备份 | authed |
| GET | `/backups/:id/download` | 下载备份文件 | authed |
| POST | `/backups/validate/:id` | 验证备份完整性 | authed |
| GET | `/backups/schedule` | 获取备份计划 | authed |
| PUT | `/backups/schedule` | 更新备份计划 | authed |

### 6.7 监控（实时）

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/monitor/overview` | 系统概览（CPU、内存、goroutine、uptime） |
| GET | `/monitor/stream` | SSE 实时指标推送（5s 间隔） |
| GET | `/monitor/prometheus/query` | Prometheus PromQL 代理 |
| GET | `/monitor/sla` | SLA 仪表盘（P50/P95/P99 延迟、缓存命中率） |
| GET | `/monitor/alerts` | 活跃告警与历史 |

**SSE Hub 设计：**

```go
type Hub struct {
    clients    map[string]map[chan Event]bool  // topic -> client channels
    mu         sync.RWMutex
    register   chan Registration
    unregister chan Registration
}

type Event struct {
    Type string      `json:"type"`   // "metrics", "log", "node_health", "alert"
    Data interface{} `json:"data"`
    Time int64       `json:"time"`
}
```

事件主题：
- `metrics` — 每 5 秒系统指标（从 Prometheus 端点采集）
- `logs` — 新日志条目（tail -f 日志文件）
- `node_health` — 节点上下线事件
- `alerts` — SLA 阈值违规告警

### 6.8 统计分析

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/analytics/query` | 执行分析 SQL（含耗时和 EXPLAIN） |
| GET | `/analytics/suggestions` | 基于表结构的查询建议 |
| GET | `/analytics/overview` | 数据量趋势、写入/查询速率图表 |

---

## 七、前端页面设计

### 7.1 布局

```
+─────────────────────────────────────────────+
│  Logo   MinIODB Dashboard          User ▼   │
+──────────+──────────────────────────────────+
│          │                                   │
│  [总览]  │      页面内容区域                   │
│  集群    │                                   │
│  节点    │      （根据页面变化）                │
│  数据    │                                   │
│  日志    │                                   │
│  备份    │                                   │
│  监控    │                                   │
│  分析    │                                   │
│          │                                   │
+──────────+──────────────────────────────────+
```

### 7.2 页面说明

- **总览**：集群模式标识、节点数、表数量、数据量、最近写入/查询速率迷你图、系统健康徽章
- **集群**：拓扑可视化（ECharts 图表）、模式标记（单节点/分布式）、转换按钮、配置查看器
- **节点**：节点列表（ID、地址、状态、运行时间、数据分布）、节点详情抽屉含资源指标
- **数据**：表列表侧边栏（含创建/删除表按钮）、数据网格含分页和排序、新增/编辑/删除数据按钮、SQL 查询控制台（CodeMirror）、查询结果表、导出按钮
- **日志**：日志级别过滤标签、时间范围选择器、关键词搜索、虚拟化日志列表、实时尾随开关（SSE）
- **备份**：备份列表（元数据/全量/表级）、手动备份按钮（全量/表级选择）、恢复向导（全量/选表/新表名）、备份详情（大小、状态、包含表）、备份下载、完整性验证、计划配置
- **监控**：实时图表（QPS、延迟 P50/P95/P99、缓冲区使用、MinIO/Redis 操作速率、Go 运行时资源）
- **分析**：SQL 编辑器、列统计卡片、数据量趋势图、查询历史列表

### 7.3 前端技术栈

遵循 `frontend-design` Skill 推荐栈（`~/.claude/skills/frontend-design/`）。

| 库 | 用途 | 说明 |
|----|------|------|
| Next.js 14+ | 框架 | App Router，`output: 'export'` 静态导出供 Go embed |
| TypeScript | 语言 | 类型安全 |
| Tailwind CSS v4 | Utility CSS | 与 shadcn/ui 集成 |
| shadcn/ui | UI 组件库 | 可定制、Accessible，含 Table/Card/Dialog/Sheet 等 |
| Framer Motion | 动效 | 声明式动画，页面过渡、stagger reveal |
| Zustand | 状态管理 | 轻量、无模板代码 |
| ECharts | 图表与图形 | 实时监控 SSE 图表、拓扑图、仪表盘 |
| CodeMirror 6 | SQL 编辑器 | 语法高亮、自动补全 |
| dayjs | 日期处理 | 轻量日期库 |

**关键约束**：Next.js 必须配置 `output: 'export'` 生成纯静态文件（`out/` 目录），以便通过 Go `embed.FS` 嵌入二进制。不使用 SSR / API Routes / Middleware 等服务端功能。

---

## 八、Build Tag 编译机制

### 包含 Dashboard（All-in-One）

```go
//go:build dashboard

package dashboard

func Mount(router *gin.Engine, svc *service.MinIODBService, cfg *config.Config, ...) {
    coreClient := client.NewLocalClient(svc, metadataManager, ...)
    dashServer := NewServer(coreClient, cfg)
    dashServer.MountRoutes(router.Group("/dashboard"))
}
```

### 不包含 Dashboard

```go
//go:build !dashboard

package dashboard

func Mount(router *gin.Engine, _ ...interface{}) {
    // no-op: dashboard not included in this build
}
```

编译命令：
- **含 Dashboard**：`go build -tags dashboard -o bin/miniodb ./cmd/`
- **不含 Dashboard**：`go build -o bin/miniodb ./cmd/`
- **独立 Dashboard**：`go build -o bin/dashboard ./cmd/dashboard/`

---

## 九、配置扩展

在 `config/config.yaml` 中新增：

```yaml
# 账户配置（Dashboard 复用此配置登录，见第十一节）
auth:
  enable_jwt: true
  jwt_secret: "your-strong-secret-key"
  api_key_pairs:                       # Dashboard 和核心 API 共用账户
    - key: "admin"
      secret: "$2a$10$..."            # bcrypt 哈希
      display_name: "管理员"           # role 默认 root，当前无需配置
    - key: "operator"
      secret: "$2a$10$..."
      display_name: "运维人员"

# Dashboard 专用配置
dashboard:
  enabled: true                        # 是否启用 Dashboard
  port: ":9090"                        # 独立模式端口（All-in-One 忽略）
  base_path: "/dashboard"              # URL 路径前缀
  core_endpoint: ""                    # 独立模式: minIODB REST 地址
  core_grpc_endpoint: ""               # 独立模式: minIODB gRPC 地址
  metrics_scrape_interval: 5s          # Prometheus 采集间隔
  log_dir: ""                          # 日志目录覆盖（默认从 log 配置读取）
  backup_dir: ""                       # 备份下载临时目录
```

**密码哈希生成**：

```bash
# 内置命令行工具
./bin/miniodb --hash-password "your-password"
# 输出: $2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
```

---

## 十、Docker 构建

### All-in-One Dockerfile（修改现有）

```dockerfile
FROM node:22-alpine AS ui-builder
WORKDIR /app/dashboard-ui
COPY dashboard-ui/package*.json ./
RUN npm ci
COPY dashboard-ui/ ./
RUN npm run build

FROM golang:1.24 AS builder
# ... 现有构建步骤 ...
COPY --from=ui-builder /app/dashboard-ui/out /app/dashboard-ui/out
RUN CGO_ENABLED=1 go build -tags dashboard -o miniodb cmd/main.go
```

### 独立 Dashboard Dockerfile

```dockerfile
FROM node:22-alpine AS ui-builder
WORKDIR /app/dashboard-ui
COPY dashboard-ui/ ./
RUN npm ci && npm run build

FROM golang:1.24 AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
COPY --from=ui-builder /app/dashboard-ui/out /app/dashboard-ui/out
RUN CGO_ENABLED=1 go build -o dashboard cmd/dashboard/main.go

FROM ubuntu:24.04
COPY --from=builder /app/dashboard /app/dashboard
EXPOSE 9090
ENTRYPOINT ["/app/dashboard"]
```

### Docker Compose 扩展

```yaml
dashboard:
  build:
    context: ../..
    dockerfile: Dockerfile.dashboard
  ports:
    - "9090:9090"
  environment:
    - DASHBOARD_CORE_ENDPOINT=http://miniodb:8081
    - DASHBOARD_CORE_GRPC_ENDPOINT=miniodb:8080
  depends_on:
    miniodb:
      condition: service_healthy
```

---

## 十一、认证与安全设计

### 11.1 统一账户体系（与核心共用）

Dashboard 登录账户与 minIODB 核心 API 账户**完全一致**，通过扩展核心 `APIKeyPair` 增加 `display_name` 和预留 `role` 字段。

> **当前阶段**：项目尚未实现精细角色权限控制，所有账户默认 `root` 角色，拥有全部权限。`role` 字段预留，后续迭代扩展账户体系时实现表级别的细粒度 RBAC。

**核心改动**：扩展 `APIKeyPair` 增加 `role` 和 `display_name`：

```go
// config/config.go 和 internal/security/auth.go
type APIKeyPair struct {
    Key         string `yaml:"key" json:"key"`                    // API Key（同时作为登录用户名）
    Secret      string `yaml:"secret" json:"secret"`              // bcrypt 哈希（同时作为登录密码）
    Role        string `yaml:"role" json:"role"`                  // 预留字段，当前默认 "root"（全部权限）
    DisplayName string `yaml:"display_name" json:"display_name"`  // 显示名称
}
```

**配置示例**：

```yaml
auth:
  enable_jwt: true
  jwt_secret: "your-strong-secret-key"
  api_key_pairs:
    - key: "admin"
      secret: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
      display_name: "管理员"
    - key: "operator"
      secret: "$2a$10$..."
      display_name: "运维人员"
```

**密码管理**：
- `secret` 字段使用 **bcrypt** 哈希（项目已有 `golang.org/x/crypto`）
- 命令行生成哈希：`miniodb --hash-password <明文密码>`
- 核心 API (`/v1/auth/token`) 和 Dashboard 登录使用同一套凭证
- `role` 字段为空时默认 `"root"`，当前所有账户等价

**登录流程**：

```
Browser                    Dashboard API             Core AuthManager
  │                             │                          │
  │ POST /auth/login            │                          │
  │ {api_key, api_secret}       │                          │
  │────────────────────────────>│                          │
  │                             │ ValidateCredentials ────>│
  │                             │   bcrypt.Compare         │
  │                             │   return ok, role ──────>│
  │                             │<─────────────────────────│
  │                             │ 生成 JWT (sub, role, exp)│
  │ 200 {token, user:{...}}     │                          │
  │<────────────────────────────│                          │
```

**当前权限**：所有已认证用户（root）拥有全部操作权限。

**Token**：有效期使用 `auth.token_expiry`，Claims 含 `sub`/`role`/`display_name`/`exp`，签名使用 `auth.jwt_secret`。

### 11.2 其他安全措施

- Dashboard 调用核心 API 时自动携带 JWT Token（RemoteClient 模式）
- SQL 查询控制台复用 `SQLSanitizer`（仅允许 SELECT）
- 嵌入模式下前端与 API 同源，无 CORS 问题

### 11.3 后续扩展：精细角色权限（Future）

后续迭代计划实现表级别的细粒度 RBAC：
- `role` 字段启用，支持 `root`、`admin`、`viewer`、`custom` 等角色
- 表级别权限控制：按表配置读/写/删除权限
- 角色权限矩阵与 `AdminOnly` / `ViewerOnly` 中间件
- 前端根据角色动态隐藏/禁用操作按钮

---

## 十二、数据备份与恢复

### 12.1 备份体系

Dashboard 整合核心已有的元数据备份（`metadata.Manager`）和 MinIO 对象备份能力，并增加表级备份。

```
备份类型:
├── 全量备份（Full Backup）
│   ├── 元数据备份 — Redis 索引、表配置、服务注册信息 → MinIO 对象
│   └── 数据备份 — MinIO Parquet 文件 → 备份 MinIO 池
├── 表级备份（Table Backup）
│   ├── 表配置快照
│   └── 表数据文件（指定表的所有 Parquet 文件）
└── 定时计划
    ├── 元数据自动备份（已有，间隔可配）
    └── 全量/表级定时备份（新增）
```

### 12.2 备份 API（Dashboard）

| 方法 | 路径 | 描述 | 权限 |
|------|------|------|------|
| GET | `/backups` | 备份列表（含元数据备份和数据备份） | authed |
| POST | `/backups/metadata` | 触发元数据手动备份 | authed |
| POST | `/backups/full` | 触发全量数据备份（MinIO → 备份池） | authed |
| POST | `/backups/table/:name` | 触发指定表的备份 | authed |
| GET | `/backups/:id` | 备份详情（大小、状态、包含的表） | authed |
| POST | `/backups/:id/restore` | 从备份恢复（支持全量或指定表） | authed |
| DELETE | `/backups/:id` | 删除备份 | authed |
| GET | `/backups/:id/download` | 下载备份文件（元数据备份为 JSON） | authed |
| GET | `/backups/schedule` | 获取备份计划配置 | authed |
| PUT | `/backups/schedule` | 更新备份计划 | authed |
| POST | `/backups/validate/:id` | 验证备份完整性 | authed |

### 12.3 恢复流程

**全量恢复**：
```
1. 选择备份点 → 查看备份详情
2. 确认恢复（含警告提示）
3. 恢复元数据（Redis 索引重建）
4. 恢复数据文件（从备份 MinIO 池复制到主池）
5. 验证恢复结果
```

**表级恢复**：
```
1. 选择备份点 → 选择要恢复的表
2. 选择恢复模式：
   a. 覆盖恢复 — 替换当前表数据
   b. 新表恢复 — 恢复到新表名（如 orders_restored）
3. 执行恢复 → 查看进度
```

### 12.4 核心需新增的方法

```go
// internal/service/miniodb_service.go
func (s *MinIODBService) BackupTable(ctx context.Context, tableName string) (*BackupResult, error)
func (s *MinIODBService) RestoreTable(ctx context.Context, backupID, tableName, targetTable string) error
func (s *MinIODBService) FullBackup(ctx context.Context) (*BackupResult, error)
func (s *MinIODBService) FullRestore(ctx context.Context, backupID string) error
```

---

## 十三、核心需新增/修改的 API 和结构

| 改动 | 描述 | 涉及文件 |
|------|------|---------|
| 扩展 `APIKeyPair` | 增加 `role`、`display_name` 字段 | `config/config.go`、`internal/security/auth.go` |
| 扩展 `AuthManager` | 增加 `ValidateCredentials` 返回角色 | `internal/security/auth.go` |
| `GET /v1/nodes` | 节点列表 | `internal/transport/rest/server.go` |
| `GET /v1/nodes/:id` | 节点详情 | `internal/transport/rest/server.go` |
| `POST /v1/cluster/convert` | 单节点转分布式 | `internal/service/miniodb_service.go` |
| `GET /v1/logs/query` | 结构化日志查询 | 新增 handler |
| `GET /v1/tables/:name/stats` | 表级统计 | 新增 handler |
| 表级备份/恢复方法 | BackupTable / RestoreTable | `internal/service/miniodb_service.go` |
| 全量备份/恢复方法 | FullBackup / FullRestore | `internal/service/miniodb_service.go` |
| 密码哈希工具 | `--hash-password` 命令行参数 | `cmd/main.go` 或 `cmd/dashboard/main.go` |

---

## 十四、实施阶段

| 阶段 | 内容 | 预估工时 |
|------|------|---------|
| Phase 0 | 核心改动：扩展 APIKeyPair + AuthManager 角色支持 + 密码哈希工具 | 1-2d |
| Phase 1 | 项目骨架：目录结构、Build 系统、CoreClient 接口、embed 机制 | 2-3d |
| Phase 2 | Dashboard 后端：Server、Handler、Service、SSE Hub、Local/Remote Client | 5-7d |
| Phase 3 | Next.js SPA 骨架：工程初始化、shadcn/ui、App Router、布局、API 层、登录页面 | 2-3d |
| Phase 4 | 核心页面：总览、集群、节点、监控（含 SSE 实时图表） | 5-7d |
| Phase 5 | 数据页面：表管理 CRUD、数据 CRUD、SQL 控制台、统计分析 | 5-6d |
| Phase 6 | 运维页面：日志查看器（SSE 流式）、全量/表级备份管理 | 4-5d |
| Phase 7 | Docker 集成：Dockerfile、Compose 文件、All-in-One 与独立构建 | 2-3d |
| Phase 8 | 节点扩缩容：单节点转分布式 API + UI、扩容检查、配置生成 | 3-4d |

**总计**：约 30-40 人天
