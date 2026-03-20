# MinIODB 权限体系设计方案

## 1. 背景与目标

### 1.1 现状分析

当前系统已实现 JWT 认证机制（API Key + Secret 模式），但**缺少授权/鉴权层**：

- `APIKeyPair` 有 `Role` 字段（预留），但全局默认 `"root"`，未做任何权限校验
- 所有通过认证的用户拥有完全相同的访问权限
- Dashboard 的 `requireAuth` 中间件只检查认证状态，不检查角色
- REST API 和 gRPC 均无端点级/数据级权限控制
- 无表级别的访问控制——任何认证用户可对任意表执行任意操作

### 1.2 设计目标

实现**两级权限体系**：

| 层级 | 名称 | 粒度 | 控制对象 |
|------|------|------|----------|
| **Tier 1** | 功能权限 (Function Permission) | API 端点级 | 控制用户能否访问某个 API |
| **Tier 2** | 数据权限 (Data Permission) | 表级 | 控制用户能否对某个表执行 CRUD |

---

## 2. 角色定义

### 2.1 内置角色

| 角色 | 标识符 | 说明 |
|------|--------|------|
| 管理员 | `admin` | 全部权限，可管理系统配置、表结构、备份等 |
| 普通用户 | `viewer` | 只读为主，可查看数据和监控信息，对特定表可配置写入权限 |

> **扩展性**：角色定义为配置驱动，未来可扩展 `operator`（运维）、`writer`（写入者）等角色，无需改代码。

### 2.2 角色权限总览

```
admin:  [*] 全部功能权限 + 全部表的全部数据权限（硬编码 bypass）
viewer: [受限] 只读功能权限 + 按配置的表数据权限
```

---

## 3. Tier 1 — 功能权限设计

### 3.1 资源与操作定义

**操作 (Action)**：

| 操作 | 说明 |
|------|------|
| `create` | 新增 |
| `read` | 查看 |
| `update` | 修改 |
| `delete` | 删除 |
| `execute` | 执行（用于 SQL 查询等特殊操作） |

**资源 (Resource)**：

```go
// Dashboard 资源
"dashboard.cluster"     // 集群信息
"dashboard.config"      // 系统配置
"dashboard.nodes"       // 节点信息
"dashboard.tables"      // 表管理（DDL 级别：创建/修改/删除表结构）
"dashboard.data"        // 数据操作（DML 级别：表内数据 CRUD）
"dashboard.query"       // SQL 查询
"dashboard.logs"        // 日志查看
"dashboard.backups"     // 备份管理
"dashboard.hot_backup"  // 热备状态
"dashboard.monitor"     // 监控指标
"dashboard.analytics"   // 分析查询

// REST API 资源
"api.tables"            // 表管理
"api.data"              // 数据操作
"api.query"             // SQL 查询
"api.metadata"          // 元数据管理
"api.status"            // 系统状态
"api.metrics"           // 性能指标
"api.debug"             // Debug / pprof
```

### 3.2 Dashboard API 权限映射

| 端点 | Method | 资源 | 操作 | 最低角色 | 备注 |
|------|--------|------|------|----------|------|
| `/auth/login` | POST | - | - | 公开 | |
| `/auth/logout` | POST | - | - | 已认证 | |
| `/auth/me` | GET | - | - | 已认证 | |
| `/auth/change-password` | POST | - | - | 已认证 | |
| `/cluster/info` | GET | dashboard.cluster | read | viewer | |
| `/cluster/topology` | GET | dashboard.cluster | read | viewer | |
| `/cluster/config` | GET | dashboard.config | read | viewer | |
| `/cluster/config/full` | GET | dashboard.config | read | **admin** | 含敏感信息 |
| `/cluster/config` | PUT | dashboard.config | update | **admin** | 修改系统配置 |
| `/cluster/enable-distributed` | POST | dashboard.config | update | **admin** | 模式转换 |
| `/nodes` | GET | dashboard.nodes | read | viewer | |
| `/nodes/:id` | GET | dashboard.nodes | read | viewer | |
| `/tables` | GET | dashboard.tables | read | viewer | |
| `/tables` | POST | dashboard.tables | create | **admin** | 创建表 |
| `/tables/:name` | GET | dashboard.tables | read | viewer | |
| `/tables/:name` | PUT | dashboard.tables | update | **admin** | 修改表配置 |
| `/tables/:name` | DELETE | dashboard.tables | delete | **admin** | 删除表 |
| `/tables/:name/stats` | GET | dashboard.tables | read | viewer | |
| `/tables/:name/data` | GET | dashboard.data | read | viewer | **+ 表级 read** |
| `/tables/:name/data` | POST | dashboard.data | create | viewer | **+ 表级 create** |
| `/tables/:name/data/batch` | POST | dashboard.data | create | viewer | **+ 表级 create** |
| `/tables/:name/data/:id` | PUT | dashboard.data | update | viewer | **+ 表级 update** |
| `/tables/:name/data/:id` | DELETE | dashboard.data | delete | viewer | **+ 表级 delete** |
| `/query` | POST | dashboard.query | execute | viewer | **+ 表级 read (SQL 解析)** |
| `/logs` | GET | dashboard.logs | read | viewer | |
| `/logs/stream` | GET | dashboard.logs | read | viewer | |
| `/logs/files` | GET | dashboard.logs | read | viewer | |
| `/backups` | GET | dashboard.backups | read | viewer | |
| `/backups/availability` | GET | dashboard.backups | read | viewer | |
| `/backups/status` | GET | dashboard.backups | read | viewer | |
| `/backups/metadata` | POST | dashboard.backups | create | **admin** | 触发备份 |
| `/backups/schedule` | GET | dashboard.backups | read | viewer | |
| `/backups/schedule` | PUT | dashboard.backups | update | **admin** | 修改备份计划 |
| `/backups/full` | POST | dashboard.backups | create | **admin** | 触发全量备份 |
| `/backups/table/:name` | POST | dashboard.backups | create | **admin** | 触发表备份 |
| `/backups/:id/restore` | POST | dashboard.backups | update | **admin** | 恢复备份 |
| `/backups/:id/download` | GET | dashboard.backups | read | viewer | |
| `/backups/:id/verify` | POST | dashboard.backups | read | viewer | |
| `/backups/:id` | GET | dashboard.backups | read | viewer | |
| `/backups/:id` | DELETE | dashboard.backups | delete | **admin** | 删除备份 |
| `/backups/plans` | GET | dashboard.backups | read | viewer | |
| `/backups/plans` | POST | dashboard.backups | create | **admin** | 创建备份计划 |
| `/backups/plans/:id` | PUT | dashboard.backups | update | **admin** | 修改备份计划 |
| `/backups/plans/:id` | DELETE | dashboard.backups | delete | **admin** | 删除备份计划 |
| `/backups/plans/:id/trigger` | POST | dashboard.backups | create | **admin** | 手动触发 |
| `/backups/plans/:id/executions` | GET | dashboard.backups | read | viewer | |
| `/hot-backup/status` | GET | dashboard.hot_backup | read | viewer | |
| `/hot-backup/availability` | GET | dashboard.hot_backup | read | viewer | |
| `/monitor/overview` | GET | dashboard.monitor | read | viewer | |
| `/monitor/stream` | GET | dashboard.monitor | read | viewer | |
| `/monitor/sla` | GET | dashboard.monitor | read | viewer | |
| `/analytics/query` | POST | dashboard.analytics | read | viewer | |
| `/analytics/overview` | GET | dashboard.analytics | read | viewer | |

### 3.3 REST API 权限映射

| 端点 | Method | 资源 | 操作 | 最低角色 | 备注 |
|------|--------|------|------|----------|------|
| `/v1/auth/*` | * | - | - | 公开 | |
| `/v1/health` | GET | - | - | 公开 | |
| `/v1/data` | POST | api.data | create | viewer | **+ 表级 create** |
| `/v1/query` | POST | api.query | execute | viewer | **+ 表级 read (SQL 解析)** |
| `/v1/data` | PUT | api.data | update | viewer | **+ 表级 update** |
| `/v1/data` | DELETE | api.data | delete | viewer | **+ 表级 delete** |
| `/v1/data/cleanup-empty-ids` | POST | api.data | delete | **admin** | 批量清理 |
| `/v1/tables` | POST | api.tables | create | **admin** | 创建表（DDL） |
| `/v1/tables` | GET | api.tables | read | viewer | |
| `/v1/tables/:name` | GET | api.tables | read | viewer | |
| `/v1/tables/:name` | DELETE | api.tables | delete | **admin** | 删除表（DDL） |
| `/v1/metadata/backup` | POST | api.metadata | create | **admin** | |
| `/v1/metadata/restore` | POST | api.metadata | update | **admin** | |
| `/v1/metadata/backups` | GET | api.metadata | read | **admin** | |
| `/v1/metadata/status` | GET | api.metadata | read | **admin** | |
| `/v1/status` | GET | api.status | read | viewer | |
| `/v1/metrics` | GET | api.metrics | read | viewer | |
| `/v1/debug/pprof/*` | * | api.debug | read | **admin** | 敏感诊断信息 |

---

## 4. Tier 2 — 数据权限设计（表级别）

### 4.1 权限模型

```
TablePermission = {role} × {table} → {allowed_actions: []Action}
```

- **Action 定义**：`create`（写入）、`read`（查询/浏览）、`update`（修改记录）、`delete`（删除记录）
- **admin 角色**：硬编码 bypass，对所有表拥有全部权限，不受此配置约束
- **其他角色**：先匹配 `table_permissions` 中的显式配置，未命中则使用角色的 `table_default`

### 4.2 权限判定流程

```
请求到达
  │
  ├─ Tier 1: 功能权限检查 (中间件)
  │   ├─ admin → 通过 (bypass)
  │   └─ viewer → 检查 (resource, action) 是否在白名单
  │       ├─ 不在 → 403 Forbidden
  │       └─ 在 → 继续
  │
  ├─ 涉及表操作？
  │   ├─ 否 → 放行
  │   └─ 是 → 进入 Tier 2
  │
  └─ Tier 2: 数据权限检查 (服务层)
      ├─ admin → 通过 (bypass)
      └─ 非 admin →
          ├─ 提取表名
          │   ├─ URL 参数: /tables/:name/data → name
          │   ├─ 请求体: {"table": "xxx"} → xxx
          │   └─ SQL 解析: SELECT FROM t1 JOIN t2 → [t1, t2]
          │
          ├─ 查找权限配置
          │   ├─ table_permissions 有显式配置 → 使用
          │   └─ 无显式配置 → 使用 role.table_default
          │
          └─ 检查操作是否在 allowed_actions 中
              ├─ 是 → 放行
              └─ 否 → 403 Forbidden
```

### 4.3 SQL 查询的表权限处理

SQL 查询（`/query`, `/v1/query`）是特殊场景：一条 SQL 可能涉及多个表。

**处理策略**：

1. 从请求体中提取 SQL 语句
2. 使用正则解析 SQL 中引用的表名（覆盖 SELECT FROM / JOIN / INSERT INTO / UPDATE / DELETE FROM / WITH ... AS）
3. 对 SQL 中涉及的**每一个表**分别检查当前用户的数据权限
4. 只要有**任一表**权限不足，整条 SQL 拒绝执行

**SQL 表名提取规则**（正则方案）：

```go
// 匹配的模式（大小写不敏感）：
// FROM <table>
// JOIN <table>
// INTO <table>
// UPDATE <table>
// DELETE FROM <table>
// WITH <alias> AS (... FROM <table> ...)
var tablePattern = regexp.MustCompile(
    `(?i)(?:FROM|JOIN|INTO|UPDATE)\s+` +   // keyword
    `(?:` +
    `"([^"]+)"` +                           // "quoted_table"
    `|([a-zA-Z_][a-zA-Z0-9_]*)` +          // unquoted_table
    `)`,
)
```

**权限映射**：

| SQL 操作 | 所需表权限 |
|----------|-----------|
| SELECT / WITH ... SELECT | `read` |
| INSERT INTO | `create` |
| UPDATE | `update` |
| DELETE FROM | `delete` |

> **注意**：若正则方案不能满足复杂 SQL 场景（如子查询、CTE 嵌套），后续可升级为使用 DuckDB 的 SQL parser 做 AST 级别的表名提取。V1 先用正则覆盖常见场景。

---

## 5. 配置设计

### 5.1 config.yaml 扩展

```yaml
auth:
  token_expiry: "24h"
  api_key_pairs:
    - key: "admin"
      secret: "admin123456"
      role: "admin"                    # 修改：从 "root" 改为 "admin"
      display_name: "管理员"
    - key: "reader"
      secret: "reader123456"
      role: "viewer"
      display_name: "只读用户"
    - key: "writer"
      secret: "writer123456"
      role: "viewer"
      display_name: "数据写入用户"

  # 新增：角色定义
  roles:
    admin:
      description: "管理员 - 全部权限"
      table_default: ["create", "read", "update", "delete"]
    viewer:
      description: "普通用户 - 默认只读"
      table_default: ["read"]

  # 新增：表级数据权限（覆盖角色默认值）
  table_permissions:
    - table: "events"
      permissions:
        viewer: ["create", "read"]     # viewer 可以写入和查看 events 表
    - table: "metrics"
      permissions:
        viewer: ["create", "read"]     # viewer 可以写入和查看 metrics 表
    - table: "user_actions"
      permissions:
        viewer: ["create", "read", "update", "delete"]  # viewer 对此表有完全权限
```

### 5.2 Config 结构体扩展

```go
// config/config.go

// AuthConfig 认证配置（扩展）
type AuthConfig struct {
    TokenExpiry      string                 `yaml:"token_expiry"`
    APIKeyPairs      []APIKeyPair           `yaml:"api_key_pairs"`
    SkipAuthPaths    []string               `yaml:"skip_auth_paths"`
    RequireAuthPaths []string               `yaml:"require_auth_paths"`
    Roles            map[string]RoleConfig  `yaml:"roles"`             // 新增
    TablePermissions []TablePermissionEntry `yaml:"table_permissions"` // 新增
}

// RoleConfig 角色配置
type RoleConfig struct {
    Description  string   `yaml:"description"`
    TableDefault []string `yaml:"table_default"` // 角色对表的默认权限
}

// TablePermissionEntry 表级权限条目
type TablePermissionEntry struct {
    Table       string              `yaml:"table"`
    Permissions map[string][]string `yaml:"permissions"` // role -> actions
}
```

### 5.3 默认值与向后兼容

```go
// 默认角色配置（无 roles 配置时使用）
var DefaultRoles = map[string]RoleConfig{
    "admin": {
        Description:  "管理员",
        TableDefault: []string{"create", "read", "update", "delete"},
    },
    "root": {  // 兼容旧版 "root" 角色
        Description:  "超级管理员（兼容）",
        TableDefault: []string{"create", "read", "update", "delete"},
    },
    "viewer": {
        Description:  "普通用户",
        TableDefault: []string{"read"},
    },
}
```

**兼容规则**：
- `role` 字段未设置或为空 → 默认 `"admin"`（保持旧行为）
- `role: "root"` → 等同于 `"admin"`（兼容映射）
- 配置中无 `roles` 段 → 使用 `DefaultRoles`
- 配置中无 `table_permissions` 段 → 全部使用角色默认权限

---

## 6. 核心组件设计

### 6.1 组件架构

```
┌─────────────────────────────────────────────────────────────┐
│                    请求入口 (Gin / gRPC)                      │
├─────────────────────────────────────────────────────────────┤
│  JWT Auth Middleware (现有)                                   │
│  → 验证 token，提取 Claims{UserID, Username, Role}           │
├─────────────────────────────────────────────────────────────┤
│  Permission Middleware (新增)                                 │
│  → RequireRole() / RequirePermission()                      │
│  → Tier 1 功能权限检查                                       │
├─────────────────────────────────────────────────────────────┤
│  Handler / Service Layer                                    │
│  → PermissionManager.CheckTablePermission()                 │
│  → Tier 2 数据权限检查                                       │
├─────────────────────────────────────────────────────────────┤
│  Business Logic                                             │
└─────────────────────────────────────────────────────────────┘
```

### 6.2 新增文件清单

| 文件 | 职责 |
|------|------|
| `internal/security/rbac.go` | 角色、资源、操作的常量定义；功能权限矩阵 |
| `internal/security/permission.go` | `PermissionManager` — 权限判定核心逻辑 |
| `internal/security/permission_middleware.go` | Gin 中间件：`RequireRole()`, `RequirePermission()`, `RequireTableAccess()` |
| `internal/security/permission_interceptor.go` | gRPC 拦截器：功能权限 + 表权限检查 |
| `internal/security/sql_parser.go` | SQL 表名提取工具 |

### 6.3 PermissionManager

```go
// internal/security/permission.go

type PermissionManager struct {
    roles            map[string]*RoleConfig             // 角色定义
    tablePermissions map[string]map[string][]string     // table -> role -> actions
    adminRoles       map[string]bool                    // admin 角色集合 (bypass)
    mu               sync.RWMutex
    logger           *zap.Logger
}

// NewPermissionManager 从配置创建权限管理器
func NewPermissionManager(cfg *config.AuthConfig, logger *zap.Logger) *PermissionManager

// IsAdmin 判断角色是否为管理员
func (pm *PermissionManager) IsAdmin(role string) bool

// CheckFunctionPermission 检查功能权限
// resource: "dashboard.config", action: "update"
func (pm *PermissionManager) CheckFunctionPermission(role, resource, action string) bool

// CheckTablePermission 检查表级数据权限
// role: "viewer", table: "events", action: "create"
func (pm *PermissionManager) CheckTablePermission(role, table, action string) bool

// CheckTablesPermission 批量检查多表权限（用于 SQL 查询）
func (pm *PermissionManager) CheckTablesPermission(role string, tables []string, action string) (bool, string)
// 返回 (是否全部通过, 第一个未通过的表名)

// Reload 热更新配置（预留）
func (pm *PermissionManager) Reload(cfg *config.AuthConfig)
```

### 6.4 RBAC 定义

```go
// internal/security/rbac.go

const (
    RoleAdmin  = "admin"
    RoleViewer = "viewer"
    RoleRoot   = "root"  // 兼容旧版，等同 admin

    ActionCreate  = "create"
    ActionRead    = "read"
    ActionUpdate  = "update"
    ActionDelete  = "delete"
    ActionExecute = "execute"
)

// FunctionPermissions 功能权限白名单
// key = resource, value = map[action][]allowedRoles
// admin 不需要出现在这里（硬编码 bypass）
var FunctionPermissions = map[string]map[string][]string{
    "dashboard.cluster":    {"read": {RoleViewer}},
    "dashboard.config":     {"read": {RoleViewer}},  // 注意：full config 单独标记为 admin-only
    "dashboard.nodes":      {"read": {RoleViewer}},
    "dashboard.tables":     {"read": {RoleViewer}},
    "dashboard.data":       {"create": {RoleViewer}, "read": {RoleViewer}, "update": {RoleViewer}, "delete": {RoleViewer}},
    "dashboard.query":      {"execute": {RoleViewer}},
    "dashboard.logs":       {"read": {RoleViewer}},
    "dashboard.backups":    {"read": {RoleViewer}},
    "dashboard.hot_backup": {"read": {RoleViewer}},
    "dashboard.monitor":    {"read": {RoleViewer}},
    "dashboard.analytics":  {"read": {RoleViewer}},
    "api.data":             {"create": {RoleViewer}, "read": {RoleViewer}, "update": {RoleViewer}, "delete": {RoleViewer}},
    "api.query":            {"execute": {RoleViewer}},
    "api.tables":           {"read": {RoleViewer}},
    "api.status":           {"read": {RoleViewer}},
    "api.metrics":          {"read": {RoleViewer}},
    // 未出现的 (resource, action) 组合默认只有 admin 可访问
}
```

### 6.5 JWT Claims 扩展

```go
// internal/security/auth.go

// Claims JWT 声明（扩展）
type Claims struct {
    UserID   string `json:"user_id"`
    Username string `json:"username"`
    Role     string `json:"role"`      // 新增：用户角色
    jwt.RegisteredClaims
}
```

**影响范围**：
- `AuthManager.GenerateToken()` — 签发时写入 role
- `AuthManager.ValidateToken()` — 解析时提取 role
- `JWTManager`（gRPC）— 同步添加 role
- Dashboard `login()` — 返回 role 给前端
- 中间件 — 从 Claims 中读取 role，写入 `gin.Context`

### 6.6 Permission Middleware

```go
// internal/security/permission_middleware.go

// RequireRole 要求用户具有指定角色之一
func RequireRole(pm *PermissionManager, roles ...string) gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("user_role")  // 由 JWT 中间件写入
        if pm.IsAdmin(role) {
            c.Next()
            return
        }
        for _, r := range roles {
            if role == r {
                c.Next()
                return
            }
        }
        c.AbortWithStatusJSON(403, gin.H{
            "error": "insufficient permissions",
            "required_role": roles,
            "current_role": role,
        })
    }
}

// RequirePermission 要求用户具有指定功能权限
func RequirePermission(pm *PermissionManager, resource, action string) gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("user_role")
        if !pm.CheckFunctionPermission(role, resource, action) {
            c.AbortWithStatusJSON(403, gin.H{
                "error":    "forbidden",
                "resource": resource,
                "action":   action,
                "message":  fmt.Sprintf("role '%s' cannot '%s' on '%s'", role, action, resource),
            })
            return
        }
        c.Next()
    }
}

// RequireTableAccess 要求用户具有指定表的数据权限
// tableSource: "param" (从 URL param :name 提取) 或 "body" (从请求体 table 字段提取)
func RequireTableAccess(pm *PermissionManager, action, tableSource string) gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("user_role")
        if pm.IsAdmin(role) {
            c.Next()
            return
        }

        var tableName string
        switch tableSource {
        case "param":
            tableName = c.Param("name")
        case "body":
            // 读取请求体中的 table 字段（需要 peek body，不消耗）
            tableName = extractTableFromBody(c)
        }

        if tableName == "" {
            c.Next() // 无法确定表名时放行（由 handler 层兜底）
            return
        }

        if !pm.CheckTablePermission(role, tableName, action) {
            c.AbortWithStatusJSON(403, gin.H{
                "error":   "table access denied",
                "table":   tableName,
                "action":  action,
                "message": fmt.Sprintf("role '%s' cannot '%s' on table '%s'", role, action, tableName),
            })
            return
        }
        c.Next()
    }
}
```

### 6.7 SQL 表名解析器

```go
// internal/security/sql_parser.go

// ExtractTablesFromSQL 从 SQL 语句中提取引用的表名
func ExtractTablesFromSQL(sql string) []string

// ClassifySQLAction 判断 SQL 操作类型 -> 所需的表权限
func ClassifySQLAction(sql string) string
// 返回: "read" (SELECT), "create" (INSERT), "update" (UPDATE), "delete" (DELETE)

// CheckSQLPermission 综合检查 SQL 查询的表权限
func (pm *PermissionManager) CheckSQLPermission(role, sql string) (bool, string, error)
// 返回: (是否通过, 拒绝原因, 解析错误)
```

---

## 7. 路由改造方案

### 7.1 Dashboard 路由 (`internal/dashboard/server.go`)

```go
func (s *Server) MountRoutes(group *gin.RouterGroup) {
    pm := s.permissionManager  // 新增：注入 PermissionManager

    api := group.Group("/api/v1")
    api.GET("/health", s.health)
    api.POST("/auth/login", s.login)

    // 已认证路由（不区分角色）
    auth := api.Group("")
    auth.Use(s.requireAuth)
    {
        auth.POST("/auth/logout", s.logout)
        auth.GET("/auth/me", s.getMe)
        auth.POST("/auth/change-password", s.changePassword)
    }

    // viewer 可访问的只读路由
    viewerRead := api.Group("")
    viewerRead.Use(s.requireAuth)
    {
        viewerRead.GET("/cluster/info", s.clusterInfo)
        viewerRead.GET("/cluster/topology", s.clusterTopology)
        viewerRead.GET("/cluster/config", s.clusterConfig)
        viewerRead.GET("/nodes", s.listNodes)
        viewerRead.GET("/nodes/:id", s.getNode)
        viewerRead.GET("/tables", s.listTables)
        viewerRead.GET("/tables/:name", s.getTable)
        viewerRead.GET("/tables/:name/stats", s.tableStats)
        viewerRead.GET("/logs", s.queryLogs)
        viewerRead.GET("/logs/stream", s.streamLogs)
        viewerRead.GET("/logs/files", s.listLogFiles)
        viewerRead.GET("/backups", s.listBackups)
        viewerRead.GET("/backups/availability", s.getBackupAvailability)
        viewerRead.GET("/backups/status", s.getBackupsStatus)
        viewerRead.GET("/backups/schedule", s.getBackupSchedule)
        viewerRead.GET("/backups/:id", s.getBackup)
        viewerRead.GET("/backups/:id/download", s.downloadBackup)
        viewerRead.GET("/backups/plans", s.listBackupPlans)
        viewerRead.GET("/backups/plans/:id/executions", s.listBackupPlanExecutions)
        viewerRead.GET("/hot-backup/status", s.getHotBackupStatus)
        viewerRead.GET("/hot-backup/availability", s.getHotBackupAvailability)
        viewerRead.GET("/monitor/overview", s.monitorOverview)
        viewerRead.GET("/monitor/stream", s.monitorStream)
        viewerRead.GET("/monitor/sla", s.slaMetrics)
        viewerRead.POST("/analytics/query", s.analyticsQuery)
        viewerRead.GET("/analytics/overview", s.analyticsOverview)
    }

    // admin 专属路由
    adminOnly := api.Group("")
    adminOnly.Use(s.requireAuth, RequireRole(pm, RoleAdmin))
    {
        adminOnly.GET("/cluster/config/full", s.clusterFullConfig)
        adminOnly.PUT("/cluster/config", s.updateClusterConfig)
        adminOnly.POST("/cluster/enable-distributed", s.enableDistributed)
        adminOnly.POST("/tables", s.createTable)
        adminOnly.PUT("/tables/:name", s.updateTable)
        adminOnly.DELETE("/tables/:name", s.deleteTable)
        adminOnly.POST("/backups/metadata", s.triggerMetadataBackup)
        adminOnly.PUT("/backups/schedule", s.updateBackupSchedule)
        adminOnly.POST("/backups/full", s.triggerFullBackup)
        adminOnly.POST("/backups/table/:name", s.triggerTableBackup)
        adminOnly.POST("/backups/:id/restore", s.restoreBackup)
        adminOnly.POST("/backups/:id/verify", s.verifyBackup)
        adminOnly.DELETE("/backups/:id", s.deleteBackup)
        adminOnly.POST("/backups/plans", s.createBackupPlan)
        adminOnly.PUT("/backups/plans/:id", s.updateBackupPlan)
        adminOnly.DELETE("/backups/plans/:id", s.deleteBackupPlan)
        adminOnly.POST("/backups/plans/:id/trigger", s.triggerBackupPlan)
    }

    // 表数据操作路由（需要表级权限二次验证）
    tableData := api.Group("")
    tableData.Use(s.requireAuth)
    {
        tableData.GET("/tables/:name/data",
            RequireTableAccess(pm, ActionRead, "param"), s.browseData)
        tableData.POST("/tables/:name/data",
            RequireTableAccess(pm, ActionCreate, "param"), s.writeRecord)
        tableData.POST("/tables/:name/data/batch",
            RequireTableAccess(pm, ActionCreate, "param"), s.writeBatch)
        tableData.PUT("/tables/:name/data/:id",
            RequireTableAccess(pm, ActionUpdate, "param"), s.updateRecord)
        tableData.DELETE("/tables/:name/data/:id",
            RequireTableAccess(pm, ActionDelete, "param"), s.deleteRecord)
    }

    // SQL 查询（表权限在 handler 内部通过 SQL 解析检查）
    queryRoute := api.Group("")
    queryRoute.Use(s.requireAuth)
    {
        queryRoute.POST("/query", s.querySQL) // handler 内部调用 pm.CheckSQLPermission()
    }
}
```

### 7.2 REST API 路由 (`internal/transport/rest/server.go`)

类似改造：将 `securedRoutes` 按权限级别拆分为多个路由组。

### 7.3 gRPC 拦截器

```go
// internal/security/permission_interceptor.go

func PermissionUnaryInterceptor(pm *PermissionManager) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
        handler grpc.UnaryHandler) (interface{}, error) {

        // 从 context 提取 role（由 auth interceptor 写入）
        role, ok := ctx.Value("user_role").(string)
        if !ok {
            return handler(ctx, req) // 未认证，交给 auth interceptor 处理
        }

        // 根据 gRPC method 映射到 resource + action
        resource, action := mapGRPCMethodToPermission(info.FullMethod)
        if resource == "" {
            return handler(ctx, req) // 未定义权限映射，放行
        }

        if !pm.CheckFunctionPermission(role, resource, action) {
            return nil, status.Errorf(codes.PermissionDenied,
                "role '%s' cannot '%s' on '%s'", role, action, resource)
        }

        return handler(ctx, req)
    }
}
```

---

## 8. 错误响应规范

### 8.1 HTTP 状态码

| 状态码 | 场景 |
|--------|------|
| `401 Unauthorized` | 未认证（缺少/无效 token） |
| `403 Forbidden` | 已认证但权限不足 |

### 8.2 响应格式

```json
// 功能权限不足
{
    "error": "forbidden",
    "resource": "dashboard.config",
    "action": "update",
    "message": "role 'viewer' cannot 'update' on 'dashboard.config'"
}

// 表数据权限不足
{
    "error": "table access denied",
    "table": "user_data",
    "action": "delete",
    "message": "role 'viewer' cannot 'delete' on table 'user_data'"
}

// SQL 查询涉及无权限的表
{
    "error": "query permission denied",
    "denied_table": "secrets",
    "action": "read",
    "message": "role 'viewer' cannot 'read' on table 'secrets' referenced in SQL query"
}
```

---

## 9. Dashboard 前端适配

### 9.1 角色信息传递

`/auth/me` 接口已返回角色信息，前端可直接使用：

```json
{
    "user_id": "reader",
    "username": "只读用户",
    "role": "viewer"
}
```

### 9.2 前端 UI 适配建议

| 场景 | 处理方式 |
|------|----------|
| admin-only 按钮（如删除表、修改配置） | `viewer` 角色时隐藏或置灰 |
| admin-only 页面（如备份恢复操作） | 页面级路由守卫，`viewer` 重定向到只读视图 |
| 表数据操作按钮 | 根据 `/auth/me` 返回的角色 + 表权限配置动态显隐 |
| 403 响应 | 显示友好的 "权限不足" 提示 |

### 9.3 新增前端接口（可选）

```
GET /api/v1/auth/permissions
```

返回当前用户的完整权限摘要，前端据此渲染 UI：

```json
{
    "role": "viewer",
    "is_admin": false,
    "function_permissions": {
        "dashboard.config": ["read"],
        "dashboard.tables": ["read"],
        "dashboard.data": ["create", "read", "update", "delete"],
        ...
    },
    "table_permissions": {
        "_default": ["read"],
        "events": ["create", "read"],
        "metrics": ["create", "read"]
    }
}
```

---

## 10. 实施计划

### Phase 1：基础框架（优先级 P0）

1. 扩展 `config.go` — 添加 `RoleConfig`、`TablePermissionEntry` 结构体
2. 扩展 `config.yaml` — 添加 `roles` 和 `table_permissions` 配置段
3. 扩展 JWT Claims — 添加 `Role` 字段
4. 实现 `internal/security/rbac.go` — 角色/资源/操作定义
5. 实现 `internal/security/permission.go` — PermissionManager 核心
6. 实现 `internal/security/permission_middleware.go` — Gin 中间件

### Phase 2：路由改造（优先级 P0）

7. 改造 `internal/dashboard/server.go` — 按权限级别拆分路由组
8. 改造 `internal/transport/rest/server.go` — 同上
9. 改造 `internal/security/middleware.go` — JWT 中间件写入 role 到 context

### Phase 3：表级权限（优先级 P1）

10. 实现 `internal/security/sql_parser.go` — SQL 表名提取
11. 在 Dashboard `querySQL` handler 中集成 SQL 权限检查
12. 在 REST `queryData` handler 中集成 SQL 权限检查
13. 在数据操作 handler 中集成表级权限检查

### Phase 4：gRPC 支持（优先级 P2）

14. 实现 `internal/security/permission_interceptor.go` — gRPC 拦截器
15. 扩展 `internal/security/interceptor.go` — 写入 role 到 context
16. 改造 `internal/transport/grpc/server.go` — 添加权限拦截器

### Phase 5：前端适配（优先级 P2）

17. 新增 `/auth/permissions` 接口
18. 前端根据角色动态渲染 UI 元素
19. 前端 403 错误处理

### Phase 6：测试与文档（优先级 P1）

20. 单元测试：PermissionManager、SQL 表名提取
21. 集成测试：端到端权限验证
22. API 文档更新（Swagger 注解）

---

## 11. 风险与注意事项

| 风险 | 缓解措施 |
|------|----------|
| SQL 解析正则不完善 | V1 覆盖常见场景，记录未匹配的 SQL 到日志；V2 升级为 AST 解析 |
| 旧版客户端未传 role | 兼容处理：无 role = admin（旧行为）；配置中 role 为空 = admin |
| 配置错误导致锁死 | admin 角色硬编码 bypass，无法被配置限制 |
| 性能影响 | PermissionManager 内存计算，无 IO；SQL 解析用编译后的正则 |
| gRPC 与 REST 权限不一致 | 共享 PermissionManager 实例，统一权限判定逻辑 |
| Token 中 role 与配置不同步 | Token 过期后重新签发；可选：短期 token + 强制刷新 |

---

## 12. 审计日志集成

现有 `internal/audit/` 模块记录写入/更新/删除操作。权限系统上线后，建议增加以下审计事件：

```go
// 权限拒绝事件
audit.Log(audit.Event{
    Type:     "permission_denied",
    UserID:   claims.UserID,
    Role:     claims.Role,
    Resource: resource,
    Action:   action,
    Table:    tableName,  // 如涉及表
    Details:  "insufficient permissions",
})
```

这有助于安全审计和入侵检测。
