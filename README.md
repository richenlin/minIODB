# MinIODB - 基于MinIO、DuckDB和Redis的分布式OLAP系统

[![Go Version](https://img.shields.io/badge/Go-1.23+-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Supported-blue.svg)]()
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)]()

## 🚀 项目简介

MinIODB是一个极致轻量化、高性能、可水平扩展的分布式对象存储与OLAP查询分析系统。系统采用存算分离架构，以MinIO作为分布式存储底座，DuckDB作为高性能OLAP查询引擎，Redis作为元数据中心，提供企业级的数据分析能力。

### 🎯 核心目标

- **极致轻量化** - 部署简单，支持单机单节点部署，开箱即用。轻量化设计，适合资源受限环境；
- **服务健壮** - 内置故障恢复和自动重试机制，支持数据备份和灾难恢复，通过增加节点线性提升处理能力；支持自动和手动备份机制，提供基于时间范围和ID的数据恢复；
- **表级管理** - 支持多表数据隔离和差异化配置
- **存算分离** - MinIO负责存储，DuckDB负责计算，Redis管理服务发现、节点状态和数据索引；
- **自动分片** - 基于一致性哈希的透明数据分片
- **多模式支持** - Redis支持单机、哨兵、集群模式，MinIO支持主备双池

## 🏗️ 架构设计

### 整体架构图

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

## 🚀 快速开始

### 前置要求

- Go 1.24+
- Redis 6.0+
- MinIO Server
- 8GB+ 内存推荐

### 1. 克隆项目

```bash
git clone https://github.com/your-org/minIODB.git
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
# 编辑config.local.yaml中的Redis和MinIO连接信息
```

### 4. 启动服务

```bash
# 开发模式
go run cmd/server/main.go

# 或者构建后运行
go build -o miniodb cmd/server/main.go
./miniodb
```

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

## 📦 部署方式

### 🐳 Docker Compose（推荐新手）

```bash
cd deploy/docker
cp env.example .env
# 编辑.env文件
docker-compose up -d
```


### 🔧 Ansible（推荐批量部署）

```bash
cd deploy/ansible
# 编辑inventory文件
ansible-playbook -i inventory/auto-deploy.yml site.yml
```

### ☸️ Kubernetes（推荐生产）

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

### gRPC API

#### 完整服务定义
```protobuf
service MinIODBService {
  // 数据操作 (6个核心接口)
  rpc WriteData(WriteDataRequest) returns (WriteDataResponse);
  rpc QueryData(QueryDataRequest) returns (QueryDataResponse);
  rpc UpdateData(UpdateDataRequest) returns (UpdateDataResponse);    // 新增
  rpc DeleteData(DeleteDataRequest) returns (DeleteDataResponse);    // 新增
  
  // 流式操作（新增）
  rpc StreamWrite(stream StreamWriteRequest) returns (StreamWriteResponse);
  rpc StreamQuery(StreamQueryRequest) returns (stream StreamQueryResponse);
  
  // 表管理 (4个核心接口)
  rpc CreateTable(CreateTableRequest) returns (CreateTableResponse);
  rpc ListTables(ListTablesRequest) returns (ListTablesResponse);
  rpc GetTable(GetTableRequest) returns (GetTableResponse);
  rpc DeleteTable(DeleteTableRequest) returns (DeleteTableResponse);
  
  // 元数据管理 (4个核心接口)
  rpc BackupMetadata(BackupMetadataRequest) returns (BackupMetadataResponse);
  rpc RestoreMetadata(RestoreMetadataRequest) returns (RestoreMetadataResponse);
  rpc ListBackups(ListBackupsRequest) returns (ListBackupsResponse);
  rpc GetMetadataStatus(GetMetadataStatusRequest) returns (GetMetadataStatusResponse);
  
  // 健康检查和监控 (3个核心接口)
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc GetStatus(GetStatusRequest) returns (GetStatusResponse);        // 完善
  rpc GetMetrics(GetMetricsRequest) returns (GetMetricsResponse);     // 新增
}
```

## 🔧 客户端示例

客户端示例请参考：[examples/](examples/)

## 📈 监控

### Prometheus指标

#### 系统级指标
```yaml
# 核心业务指标
miniodb_requests_total{method="write",status="success",table="users"}
miniodb_requests_duration_seconds{method="query",table="orders"}
miniodb_buffer_size_bytes{node="node-1",table="users"}
miniodb_storage_objects_total{bucket="olap-data",table="users"}

# 数据操作指标
miniodb_write_operations_total{table="users",status="success"}
miniodb_update_operations_total{table="users",status="success"}    # 新增
miniodb_delete_operations_total{table="users",status="success"}    # 新增
miniodb_query_operations_total{table="users",status="success"}

# 流式操作指标（新增）
miniodb_stream_write_records_total{table="users",status="success"}
miniodb_stream_query_batches_total{table="orders",status="success"}
miniodb_stream_query_records_total{table="orders"}

# 表级指标
miniodb_table_record_count{table="users"}
miniodb_table_file_count{table="orders"}
miniodb_table_size_bytes{table="logs"}
miniodb_table_buffer_utilization{table="users"}

# 连接池指标
miniodb_redis_pool_active_connections{node="node-1"}
miniodb_redis_pool_idle_connections{node="node-1"}
miniodb_minio_pool_active_connections{node="node-1"}
miniodb_minio_pool_idle_connections{node="node-1"}

# 备份恢复指标（新增）
miniodb_backup_operations_total{status="success"}
miniodb_restore_operations_total{status="success"}
miniodb_backup_size_bytes{backup_id="backup_20240115"}
miniodb_backup_duration_seconds{type="full"}

# 缓存指标
miniodb_cache_hits_total{cache_type="query",table="users"}
miniodb_cache_misses_total{cache_type="file",table="orders"}
miniodb_cache_hit_ratio{cache_type="query"}

# 性能指标（新增）
miniodb_query_success_rate{table="users"}
miniodb_system_health_score
miniodb_memory_usage_bytes{component="buffer"}
miniodb_cpu_usage_percent{component="query_engine"}
```

#### 健康检查端点
```bash
# 基础健康检查
curl http://localhost:8081/v1/health

# 详细组件状态
curl http://localhost:8081/v1/status

# 系统性能指标
curl http://localhost:8081/v1/metrics

# Prometheus格式指标
curl http://localhost:8081/metrics
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

## 🧪 测试

### 运行测试
```bash
# 运行所有测试
go test ./...

# 运行特定模块测试
go test ./internal/storage/...
go test ./internal/query/...
go test ./internal/service/...

# 运行基准测试
go test -bench=. ./internal/query/...

# 生成测试覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 运行竞态检测
go test -race ./...
```

### 集成测试
```bash
# 启动测试环境
docker-compose -f test/docker-compose.test.yml up -d

# 运行集成测试
go test -tags=integration ./test/...

# 运行API测试
go test -tags=api ./test/api/...

# 运行性能测试
go test -tags=performance ./test/performance/...
```

### 功能测试
```bash
# 测试数据写入和查询
curl -X POST http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -d '{"table": "test", "data": {"id": "test-1", "timestamp": "2024-01-01T10:00:00Z", "payload": {"name": "test"}}}'

# 测试表管理
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "test_table", "if_not_exists": true}'

# 测试健康检查
curl http://localhost:8081/v1/health

# 测试系统指标
curl http://localhost:8081/v1/metrics

# 测试Prometheus指标
curl http://localhost:8081/metrics
```

## 🔍 故障排除

### 常见问题

#### 1. 连接Redis失败
```bash
# 检查Redis连接
redis-cli -h localhost -p 6379 ping

# 检查配置
grep -A 5 "redis:" config.yaml

# 检查连接池状态
curl http://localhost:8081/v1/status | jq '.redis_stats'
```

#### 2. MinIO连接失败
```bash
# 检查MinIO服务
curl http://localhost:9000/minio/health/live

# 检查存储桶
mc ls minio/olap-data

# 检查连接池状态
curl http://localhost:8081/v1/status | jq '.minio_stats'
```

#### 3. 表相关问题

##### 表不存在错误
```bash
# 检查表是否存在
curl http://localhost:8081/v1/tables | jq '.tables[] | select(.name=="your_table")'

# 创建缺失的表
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "your_table", "if_not_exists": true}'

# 检查表统计信息
curl http://localhost:8081/v1/tables/your_table | jq '.table_info.stats'
```

##### 表配置问题
```bash
# 查看表配置
curl http://localhost:8081/v1/tables/your_table | jq '.table_info.config'

# 检查表级Redis配置
redis-cli hgetall "table:your_table:config"

# 查看表缓冲区状态
curl http://localhost:8081/v1/status | jq '.buffer_stats'
```

#### 4. 查询性能问题

##### 查询慢问题
```bash
# 查看查询统计
curl http://localhost:8081/v1/metrics | jq '.query_metrics'

# 检查缓存命中率
curl http://localhost:8081/v1/metrics | jq '.cache_metrics'

# 查看DuckDB连接池状态
curl http://localhost:8081/v1/status | jq '.query_engine_stats'
```

##### 流式查询问题
```bash
# 检查流式查询配置
grep -A 10 "query:" config.yaml

# 调整批次大小
# 在StreamQueryRequest中设置合适的batch_size

# 监控内存使用
curl http://localhost:8081/v1/metrics | jq '.memory_usage'
```

#### 5. 备份恢复问题

##### 备份失败
```bash
# 检查备份状态
curl http://localhost:8081/v1/metadata/status

# 手动触发备份
curl -X POST http://localhost:8081/v1/metadata/backup \
  -H "Content-Type: application/json" \
  -d '{"force": true}'

# 查看备份列表
curl http://localhost:8081/v1/metadata/backups?days=7
```

##### 恢复失败
```bash
# 干运行测试恢复
curl -X POST http://localhost:8081/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{"backup_file": "backup_xxx.json", "dry_run": true}'

# 检查备份文件完整性
curl -X POST http://localhost:8081/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{"backup_file": "backup_xxx.json", "validate": true, "dry_run": true}'
```

#### 6. 系统监控问题

##### 健康评分低
```bash
# 查看详细健康状态
curl http://localhost:8081/v1/status | jq '.health_status'

# 查看系统指标
curl http://localhost:8081/v1/metrics

# 检查各组件状态
curl http://localhost:8081/v1/status | jq '.components'
```

##### 指标收集问题
```bash
# 检查Prometheus指标
curl http://localhost:8081/metrics

# 验证指标配置
grep -A 10 "monitoring:" config.yaml

# 检查指标收集间隔
curl http://localhost:8081/v1/status | jq '.metrics_collection_stats'
```

### 日志分析
```bash
# 查看服务日志
tail -f logs/miniodb.log

# 过滤错误日志
grep "ERROR" logs/miniodb.log | tail -20

# 查看表级操作日志
grep "table:" logs/miniodb.log | tail -20

# 监控查询性能日志
grep "query duration" logs/miniodb.log | tail -20

# 查看备份恢复日志
grep -E "(backup|restore)" logs/miniodb.log | tail -20
```

### 性能调优
```bash
# 查看缓冲区使用情况
curl http://localhost:8081/v1/status | jq '.buffer_stats'

# 监控连接池使用率
curl http://localhost:8081/v1/metrics | jq '.connection_pool_metrics'

# 查看查询缓存效果
curl http://localhost:8081/v1/metrics | jq '.cache_metrics'

# 分析慢查询
grep "slow query" logs/miniodb.log | tail -10
```

更多故障排除请参考：[故障排除指南](docs/troubleshooting.md)

## 📄 许可证

本项目采用BSD-3-Clause许可证。有关更多信息，请参阅[LICENSE](LICENSE)文件。

## 🙏 致谢

感谢以下开源项目：
- [MinIO](https://github.com/minio/minio) - 高性能对象存储
- [DuckDB](https://github.com/duckdb/duckdb) - 内存OLAP数据库
- [Redis](https://github.com/redis/redis) - 内存数据结构存储
- [Gin](https://github.com/gin-gonic/gin) - Go Web框架
- [gRPC](https://github.com/grpc/grpc-go) - 高性能RPC框架

## 📞 联系我们

- 项目主页：https://github.com/richenlin/minIODB
- 问题反馈：https://github.com/richenlin/minIODB/issues

---

⭐ 如果这个项目对您有帮助，请给我们一个Star！
