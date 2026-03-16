# Dashboard 与 MinIODB 完全分离方案

## 目标

1. Dashboard 与 MinIODB 彻底分离，仅通过 HTTP 访问 MinIODB 通用 REST API
2. 非通用能力（如配置生成、集群拓扑、日志流）由 Dashboard 自身 handler 处理，不依赖 Core 内部实现

## 架构对比

### 当前（历史遗留）

```
┌─────────────────────────────────────────────────────────────────┐
│ All-in-One 模式 (go build -tags dashboard)                      │
│                                                                  │
│  main.go ──► REST Server :8081 ──┬── /v1/* (Core API)            │
│                                  └── /dashboard/* (Dashboard)    │
│                                       │                          │
│                                       └── LocalClient ──► 直接调用 metadata.Manager, discovery.ServiceRegistry, MinIODBService
│                                                           （深度耦合）                          │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│ Standalone 模式 (cmd/dashboard)                                  │
│                                                                  │
│  dashboard binary :9090 ──► RemoteClient ──HTTP──► Core :8081 /v1/* │
└─────────────────────────────────────────────────────────────────┘
```

### 目标（完全分离）

```
┌─────────────────────────────────────────────────────────────────┐
│ MinIODB Core（无 dashboard build tag）                           │
│                                                                  │
│  main.go ──► REST Server :8081 ──► /v1/* 通用 REST API           │
│             （表、数据、查询、元数据、认证、状态、指标）              │
└─────────────────────────────────────────────────────────────────┘
                                    ▲
                                    │ HTTP + Bearer JWT
                                    │
┌─────────────────────────────────────────────────────────────────┐
│ Dashboard（独立服务，仅此一种部署方式）                             │
│                                                                  │
│  dashboard binary :9090                                          │
│  ├── /dashboard/ui/*     → 静态 SPA                              │
│  ├── /dashboard/api/v1/* → 透传/聚合 Core API + 自有 handler     │
│  │   ├── 通用 API 透传：tables, data, query, auth, backups...   │
│  │   └── 非通用 handler：cluster/config, logs, monitor/stream   │
│  └── CoreClient = RemoteClient only（仅 HTTP 调用 Core）          │
└─────────────────────────────────────────────────────────────────┘
```

## API 映射

### 通用 API（Dashboard 透传 Core）

| Dashboard 路由 | Core API | 说明 |
|----------------|----------|------|
| POST /auth/login | 内部：调用 Core POST /v1/auth/token，返回自签 JWT | Dashboard 认证层 |
| GET /tables | GET /v1/tables | 透传 |
| GET /tables/:name | GET /v1/tables/:name | 透传 |
| POST /tables | POST /v1/tables | 透传 |
| PUT /tables/:name | Core 无 PUT，Dashboard 用 metadata 更新 | 需 Core 增加 PUT /v1/tables/:name |
| DELETE /tables/:name | DELETE /v1/tables/:name | 透传 |
| GET /tables/:name/stats | Core 无 | 需 Core 增加 GET /v1/tables/:name/stats |
| GET /tables/:name/data | POST /v1/query (构造 SELECT) | RemoteClient 已实现 |
| POST /tables/:name/data | POST /v1/data | 透传 |
| PUT /tables/:name/data/:id | PUT /v1/data | 透传 |
| DELETE /tables/:name/data/:id | DELETE /v1/data | 透传 |
| POST /query | POST /v1/query | 透传 |
| GET /backups | GET /v1/metadata/backups | 透传 |
| POST /backups/metadata | POST /v1/metadata/backup | 透传 |

### 非通用 API（Dashboard 自有 handler）

| 路由 | 处理方式 |
|------|----------|
| cluster/info, topology, config | 从 Core GET /v1/status 聚合；config 从 Core GET /v1/config（需新增）或本地生成 |
| cluster/enable-distributed | 纯 YAML 片段生成，无 Core 调用 |
| nodes, nodes/:id | 从 Core GET /v1/status 的 nodes 字段解析 |
| logs, logs/stream, logs/files | Core 无日志 API → Dashboard 返回空或对接外部日志服务 |
| monitor/overview, monitor/stream | 轮询 Core GET /v1/metrics，SSE 推送 |
| analytics/overview | 聚合 /v1/query 结果或返回空 |
| GetFullConfig, UpdateConfig | 生成 YAML 片段，不修改 Core；若需持久化由运维操作 |

## 实施步骤（已完成）

1. ~~Core 补充通用 API~~ — 暂不扩展，RemoteClient 已能覆盖主要能力

2. **移除 LocalClient** ✅
   - 已删除 internal/dashboard/client/local.go
   - CoreClient 仅保留 RemoteClient 实现

3. **移除 All-in-One 挂载** ✅
   - 已删除 internal/transport/rest/dashboard_mount.go、dashboard_mount_stub.go
   - cmd/main.go 不再挂载 Dashboard

4. **Dashboard 仅 Standalone** ✅
   - cmd/dashboard/main.go 为唯一入口
   - 配置 dashboard.core_endpoint 指向 MinIODB REST 地址（如 http://localhost:8081）

5. **非通用 handler** — RemoteClient 已实现：GetFullConfig/UpdateConfig/EnableDistributed 从本地 config 生成 YAML；logs 返回空；monitor 轮询 /v1/metrics
