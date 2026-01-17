# MinIODB - 基于MinIO、DuckDB和Redis的分布式OLAP系统

[![Go Version](https://img.shields.io/badge/Go-1.23+-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Supported-blue.svg)]()
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)]()

## 🚀 项目简介

MinIODB是一个极致轻量化、高性能、可水平扩展的分布式对象存储与OLAP查询分析系统。系统采用存算分离架构，以MinIO作为分布式存储底座，DuckDB作为高性能OLAP查询引擎，Redis作为元数据中心，提供企业级的数据分析能力。

### 🎯 核心特性

- **🚀 灵活部署** - 支持单节点模式和分布式模式，通过配置一键切换
- **⚡ 单节点模式** - 无需Redis依赖，仅需MinIO即可快速启动
- **📈 分布式扩展** - 支持水平扩展，通过增加节点线性提升处理能力
- **💾 资源占用少** - 轻量化设计，适合资源受限环境
- **🛡️ 服务健壮** - 内置故障恢复和自动重试机制
- **🔄 高可用** - 支持数据备份和灾难恢复
- **📊 表级管理** - 支持多表数据隔离和差异化配置
- **🔧 完全兼容** - 单节点和分布式模式API完全一致
- **🆔 智能ID生成** - 支持UUID、Snowflake、Custom三种策略，可选自动生成或手动提供

## 🏗️ 架构设计

### 架构模式

#### 🏢 分布式模式（生产推荐）
```
┌─────────────────┐    ┌─────────────────┐
│   gRPC Client   │    │  RESTful Client │
└─────────────────┘    └─────────────────┘
         │                       │
         └───────────┬───────────┘
                     │
    ┌────────────────▼────────────────┐
    │     API Gateway / Query Node    │
    │  - Request Parsing & Validation │
    │  - Query Coordination           │
    │  - Result Aggregation           │
    │  - Table Management             │
    │  - Metadata Management          │
    │  - System Monitoring            │
    └────────────────┬────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
        ▼            ▼            ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│   Redis     │ │ Worker Node │ │ Worker Node │
│ Metadata    │ │   + DuckDB  │ │   + DuckDB  │
│ & Discovery │ │ + TableMgr  │ │ + TableMgr  │
│ + TableMeta │ │ + BufferMgr │ │ + BufferMgr │
│ + Backup    │ │ + QueryEng  │ │ + QueryEng  │
└─────────────┘ └─────────────┘ └─────────────┘
                       │            │
                       └────────────┘
                              │
                    ┌─────────▼─────────┐
                    │   MinIO Cluster   │
                    │ (Object Storage)  │
                    │  TABLE/ID/DATE/   │
                    │   + Backup Data   │
                    └───────────────────┘
```

#### 🏠 单节点模式（开发推荐）
```
┌─────────────────┐    ┌─────────────────┐
│   gRPC Client   │    │  RESTful Client │
└─────────────────┘    └─────────────────┘
         │                       │
         └───────────┬───────────┘
                     │
    ┌────────────────▼────────────────┐
    │        Single Node Service      │
    │  - Request Parsing & Validation │
    │  - Local Query Processing       │
    │  - Direct Result Processing     │
    │  - Local Table Management       │
    │  - Local Metadata Management    │
    │  - Embedded DuckDB Engine       │
    │  - Local Buffer Management      │
    └────────────────┬────────────────┘
                     │
                    ▼
            ┌─────────────┐
            │ MinIO Server│
            │ (Object     │
            │  Storage)   │
            │ TABLE/ID/   │
            │ DATE/       │
            └─────────────┘
```

**部署模式对比：**

| 特性 | 单节点模式 | 分布式模式 |
|------|-----------|-----------|
| **配置** | `redis.enabled: false` | `redis.enabled: true` |
| **组件需求** | MinIO + MinIODB | Redis + MinIO + MinIODB集群 |
| **适用场景** | 开发测试、资源受限 | 生产环境、高可用 |
| **部署复杂度** | ⭐ 简单 | ⭐⭐⭐ 中等 |
| **资源占用** | ⭐⭐ 较少 | ⭐⭐⭐ 较多 |
| **扩展性** | ❌ 无 | ✅ 水平扩展 |
| **高可用** | ❌ 单点故障 | ✅ 多节点冗余 |
| **性能** | ⭐⭐⭐ 良好 | ⭐⭐⭐⭐ 优秀 |

## 🚀 快速开始

### 前置要求

#### 单节点模式（推荐新手）
- Go 1.24+
- MinIO Server
- 4GB+ 内存推荐

#### 分布式模式（推荐生产）
- Go 1.24+
- Redis 6.0+
- MinIO Server
- 8GB+ 内存推荐

### 1. 克隆项目

```bash
git clone https://github.com/richenlin/minIODB.git
cd minIODB
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 配置文件

复制并编辑配置文件：

```bash
cp config.yaml config.local.yaml
# 编辑config.local.yaml中的连接信息
```

#### 单节点模式配置
```yaml
# 关闭Redis，启用单节点模式
redis:
  enabled: false

# 配置MinIO连接
minio:
  endpoint: "localhost:9000"
  access_key: "minioadmin"
  secret_key: "minioadmin"
  use_ssl: false
```

#### 分布式模式配置
```yaml
# 启用Redis，启用分布式模式
redis:
  enabled: true
  addr: "localhost:6379"
  password: "redis123"

# 配置MinIO连接
minio:
  endpoint: "localhost:9000"
  access_key: "minioadmin"
  secret_key: "minioadmin"
  use_ssl: false
```

### 4. 启动服务

#### 单节点模式
```bash
cd deploy/docker
cp env.example .env
# 编辑.env文件，设置 SINGLE_NODE=true
docker-compose -f docker-compose.single.yml up -d
```

#### 分布式模式 
1、 Ansible

```bash
cd deploy/ansible
# 编辑inventory文件
ansible-playbook -i inventory/auto-deploy.yml site.yml
```

2、Kubernetes

```bash
cd deploy/k8s
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f redis/
kubectl apply -f minio/
kubectl apply -f miniodb/
```

详细部署文档请参考：[部署指南](deploy/README.md)

### 5. 验证服务

```bash
# 健康检查
curl http://localhost:8081/v1/health

# 查看服务状态
curl http://localhost:8081/v1/stats

# 创建第一个表
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "users",
    "config": {
      "buffer_size": 1000,
      "flush_interval_seconds": 30,
      "retention_days": 365,
      "backup_enabled": true
    }
  }'

# 列出所有表
curl http://localhost:8081/v1/tables
```


## 📖 API文档

### RESTful API

#### 数据操作API

##### 数据写入
```bash
curl -X POST http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "table": "users",
    "data": {
    "id": "user-123",
    "timestamp": "2024-01-01T10:00:00Z",
    "payload": {
      "name": "John Doe",
      "age": 30,
      "score": 95.5
      }
    }
  }'
```

##### 数据查询
```bash
curl -X POST http://localhost:8081/v1/query \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "sql": "SELECT COUNT(*) FROM users WHERE age > 25",
    "limit": 100,
    "cursor": ""
  }'
```

##### 数据更新
```bash
curl -X PUT http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "table": "users",
    "id": "user-123",
    "payload": {
      "name": "John Smith",
      "age": 31,
      "score": 98.5
    },
    "timestamp": "2024-01-02T10:00:00Z"
  }'
```

##### 数据删除
```bash
curl -X DELETE http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "table": "users",
    "id": "user-123",
    "soft_delete": false
  }'
```

#### 流式操作API

##### 流式写入
```bash
# 使用gRPC的StreamWrite接口进行大批量数据写入
# 支持批量记录，提供错误统计和处理
```

##### 流式查询
```bash
# 使用gRPC的StreamQuery接口进行大数据量查询
# 支持分批返回结果，游标分页，减少内存占用
```

#### 表管理API

##### 创建表
```bash
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "table_name": "products",
    "config": {
      "buffer_size": 2000,
      "flush_interval_seconds": 60,
      "retention_days": 730,
      "backup_enabled": true,
      "properties": {
        "description": "产品数据表",
        "owner": "product-service"
    }
    },
    "if_not_exists": true
  }'
```

##### 列出表
```bash
curl http://localhost:8081/v1/tables?pattern=user* \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 获取表信息
```bash
curl http://localhost:8081/v1/tables/users \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 删除表
```bash
curl -X DELETE http://localhost:8081/v1/tables/users?if_exists=true&cascade=true \
  -H "Authorization: Bearer YOUR_TOKEN"
```

#### 元数据管理API

##### 手动触发备份
```bash
curl -X POST http://localhost:8081/v1/metadata/backup \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "force": true
  }'
```

##### 恢复元数据
```bash
curl -X POST http://localhost:8081/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "backup_file": "backup_20240115_103000.json",
    "from_latest": false,
    "dry_run": false,
    "overwrite": true,
    "validate": true,
    "parallel": true,
    "filters": {
      "table_pattern": "users*"
    },
    "key_patterns": ["table:*", "index:*"]
  }'
```

##### 列出备份
```bash
curl http://localhost:8081/v1/metadata/backups?days=7 \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 获取元数据状态
```bash
curl http://localhost:8081/v1/metadata/status \
  -H "Authorization: Bearer YOUR_TOKEN"
```

#### 监控API

##### 健康检查
```bash
curl http://localhost:8081/v1/health
```

##### 系统状态
```bash
curl http://localhost:8081/v1/status \
  -H "Authorization: Bearer YOUR_TOKEN"

# 返回详细的系统状态信息：
# - 缓冲区状态和统计
# - Redis连接池状态
# - MinIO存储状态
# - 查询引擎性能指标
# - 节点发现和状态
```

##### 系统指标
```bash
curl http://localhost:8081/v1/metrics \
  -H "Authorization: Bearer YOUR_TOKEN"

# 返回性能指标：
# - 查询成功率和响应时间
# - 缓存命中率
# - 连接池使用率
# - 系统资源使用情况
# - 健康评分
```

##### Prometheus指标端点
```bash
curl http://localhost:8081/metrics

# 返回Prometheus格式的指标数据
# 包含系统配置、运行状态、性能指标等
```

#### 监控仪表板配置
```yaml
# Grafana仪表板配置示例
dashboard:
  title: "MinIODB监控仪表板"
  panels:
    - title: "写入TPS"
      targets:
        - expr: "rate(miniodb_write_operations_total[5m])"
    
    - title: "查询响应时间"
      targets:
        - expr: "histogram_quantile(0.95, miniodb_requests_duration_seconds)"
    
    - title: "表级存储使用"
      targets:
        - expr: "sum by (table) (miniodb_table_size_bytes)"
    
    - title: "连接池使用率"
      targets:
        - expr: "miniodb_redis_pool_active_connections / (miniodb_redis_pool_active_connections + miniodb_redis_pool_idle_connections)"
    
    - title: "系统健康评分"    # 新增
      targets:
        - expr: "miniodb_system_health_score"
    
    - title: "备份成功率"      # 新增
      targets:
        - expr: "rate(miniodb_backup_operations_total{status=\"success\"}[1h]) / rate(miniodb_backup_operations_total[1h])"
```

### 告警规则
```yaml
# Prometheus告警规则
groups:
  - name: miniodb
    rules:
      - alert: MinIODBHighErrorRate
        expr: rate(miniodb_requests_total{status="error"}[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "MinIODB error rate is high"
      
      - alert: MinIODBLowHealthScore    # 新增
        expr: miniodb_system_health_score < 70
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "MinIODB system health score is low"
      
      - alert: MinIODBBackupFailed      # 新增
        expr: increase(miniodb_backup_operations_total{status="error"}[1h]) > 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "MinIODB backup operation failed"
      
      - alert: MinIODBConnectionPoolExhausted
        expr: miniodb_redis_pool_active_connections / (miniodb_redis_pool_active_connections + miniodb_redis_pool_idle_connections) > 0.9
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "MinIODB connection pool is nearly exhausted"
```

## 📄 许可证

本项目采用BSD-3-Clause许可证。有关更多信息，请参阅[LICENSE](LICENSE)文件。

---

⭐ 如果这个项目对您有帮助，请给我们一个Star！
