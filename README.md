# MinIODB - 基于MinIO、DuckDB和Redis的分布式OLAP系统

[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Supported-blue.svg)]()
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)]()

## 项目简介

MinIODB是一个极致轻量化、高性能、可水平扩展的分布式对象存储与OLAP查询分析系统。系统采用存算分离架构，以MinIO作为分布式存储底座，DuckDB作为高性能OLAP查询引擎，Redis作为元数据中心，提供企业级的数据分析能力。

## 核心特性

- **🖥️ Dashboard控制台** - Web管理界面，支持实时监控、表管理、节点管理、日志查看
- **🚀 灵活部署** - 支持4种部署模式：单节点/分布式/All-in-One/Dashboard独立
- **⚡ 单节点模式** - 无需Redis依赖，仅需MinIO即可快速启动
- **📈 分布式扩展** - 支持水平扩展，通过增加节点线性提升处理能力
- **💾 资源占用少** - 轻量化设计，适合资源受限环境
- **🛡️ 服务健壮** - 内置故障恢复和自动重试机制
- **🔄 高可用** - 主备双池自动故障切换，支持数据备份和灾难恢复
- **📊 表级管理** - 支持多表数据隔离和差异化配置
  
## 架构设计

### 架构模式

#### 分布式模式（生产推荐）
```
┌─────────────────────────────────────────────────────────────┐
│                      Client (gRPC/REST)                      │
│                      Browser (Dashboard)                     │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                   API Gateway / Coordinator                  │
│  - Request Validation  - Query Optimization                  │
│  - JWT Authentication  - Load Balancing                      │
│  - Rate Limiting       - Result Aggregation                  │
│  - Dashboard Server    - SSE Real-time Events                │
└──────┬──────────────────────────────────────────────┬───────┘
       │                                               │
┌──────▼──────────┐                        ┌──────────▼────────┐
│  Redis Cluster  │                        │   Worker Nodes    │
│  - Metadata     │◄──────────────────────►│  - DuckDB (OLAP)  │
│  - Service Reg. │                        │  - Data Buffer    │
│  - Data Index   │                        │  - Query Cache    │
│  - Cache        │                        │  - File Cache     │
└─────────────────┘                        └─────────┬─────────┘
                                                     │
                                           ┌─────────▼─────────┐
                                           │  MinIO Cluster    │
                                           │  - Primary Pool   │
                                           │  - Backup Pool    │
                                           │  - Parquet Files  │
                                           └───────────────────┘
```

### 核心组件

- **MinIO** - S3兼容对象存储，负责数据持久化和分布式存储
- **DuckDB** - 嵌入式OLAP引擎，负责高性能列式查询计算
- **Redis** - 元数据中心，负责服务发现、数据索引、分布式协调
- **Apache Parquet** - 列式存储格式，支持压缩和谓词下推
- **Dashboard** - Web管理控制台，提供可视化监控和运维管理（Next.js + shadcn/ui）

详细架构设计：[docs/SOLUTION.md](docs/SOLUTION.md)

## 快速开始

### 前置要求

**单节点模式**（推荐开发测试）：
- Go 1.24+
- MinIO Server
- 4GB+ 内存

**分布式模式**（推荐生产环境）：
- Go 1.24+
- Redis 6.0+
- MinIO Cluster
- 8GB+ 内存

### 安装

```bash
git clone https://github.com/richenlin/minIODB.git
cd minIODB
go mod download
```

### 配置

复制并编辑配置文件：

```bash
cp config/config.yaml config/config.local.yaml
```

**单节点模式配置**：
```yaml
redis:
  enabled: false  # 关闭Redis，无需分布式协调

minio:
  endpoint: "localhost:9000"
  access_key: "minioadmin"
  secret_key: "minioadmin"
  bucket: "miniodb-data"
```

**分布式模式配置**：
```yaml
redis:
  enabled: true
  mode: "standalone"  # 或 sentinel/cluster
  addr: "localhost:6379"
  password: "redis123"

minio:
  endpoint: "minio-cluster:9000"
  access_key: "minioadmin"
  secret_key: "minioadmin"
  bucket: "miniodb-data"
```

### 启动服务

```bash
# 启动redis、minio和服务
./deploy/deploy.sh dev --install-deps
＃　启动go服务
go run cmd/main.go -c config/config.local.yaml

# 或仅启动核心服务（不含Dashboard）
go run cmd/main.go -c config/config.local.yaml -tags !dashboard
```

服务启动后：
- **gRPC端口**: :8080
- **REST API端口**: :8081
- **Dashboard控制台**: http://localhost:8081/dashboard/
- **Swagger UI**: http://localhost:8081/api-docs/index.html
- **Prometheus指标**: http://localhost:8081/metrics

### 验证服务

```bash
# 健康检查
curl http://localhost:8081/v1/health

# 创建表
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

# 写入数据
curl -X POST http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "users",
    "id": "user-001",
    "timestamp": "2024-01-18T10:00:00Z",
    "payload": {
      "name": "张三",
      "age": 25,
      "city": "北京"
    }
  }'

# 查询数据
curl -X POST http://localhost:8081/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT * FROM users WHERE id = '\''user-001'\''"
  }'
```

## 文档

### 核心文档
- [架构设计文档](docs/SOLUTION.md) - 完整的系统架构设计和技术细节
- [Dashboard架构设计](docs/DASHBOARD_ARCHITECTURE.md) - Dashboard管理控制台架构设计
- [Dashboard实现指南](docs/DASHBOARD_IMPLEMENTATION.md) - Dashboard详细实现说明
- [API文档](http://localhost:8081/api-docs/index.html) - Swagger UI在线文档
- [部署指南](deploy/README.md) - Docker/Kubernetes/Ansible部署方式

### 模块文档
- [连接池管理](pkg/pool/README.md) - Redis和MinIO连接池设计
- [ID生成器](pkg/idgen/README.md) - UUID/Snowflake/自定义ID生成
- [日志系统](pkg/logger/README.md) - 结构化日志使用指南

## 许可证

本项目采用BSD-3-Clause许可证。有关更多信息，请参阅[LICENSE](LICENSE)文件。

---

⭐ 如果这个项目对您有帮助，请给我们一个Star！
