# MinIODB - 基于MinIO、DuckDB和Redis的分布式OLAP系统

[![Go Version](https://img.shields.io/badge/Go-1.23+-blue.svg)](https://golang.org/)
[![Docker](https://img.shields.io/badge/Docker-Supported-blue.svg)]()
[![Kubernetes](https://img.shields.io/badge/Kubernetes-Ready-blue.svg)]()

## 🚀 项目简介

MinIODB是一个极致轻量化、高性能、可水平扩展的分布式对象存储与OLAP查询分析系统。系统采用存算分离架构，以MinIO作为分布式存储底座，DuckDB作为高性能OLAP查询引擎，Redis作为元数据中心，提供企业级的数据分析能力。

### 🎯 核心目标

- **部署简单** - 支持单机单节点部署，开箱即用
- **资源占用少** - 轻量化设计，适合资源受限环境
- **服务健壮** - 内置故障恢复和自动重试机制
- **高可用** - 支持数据备份和灾难恢复
- **线性扩展** - 通过增加节点线性提升处理能力
- **表级管理** - 支持多表数据隔离和差异化配置

## ✨ 核心特性

### 🏗️ 架构特性
- **存算分离** - MinIO负责存储，DuckDB负责计算，实现极致弹性
- **元数据驱动** - Redis管理服务发现、节点状态和数据索引
- **自动分片** - 基于一致性哈希的透明数据分片
- **水平扩展** - 支持动态节点加入和数据重分布
- **表级分离** - 支持多表数据逻辑隔离和独立管理

### 🔗 连接池特性
- **统一连接池管理** - 统一管理Redis和MinIO连接池，提供高效的资源管理
- **多模式支持** - Redis支持单机、哨兵、集群模式，MinIO支持主备双池
- **智能健康检查** - 实时监控连接池状态，自动故障检测和切换
- **性能优化** - 连接复用、超时管理、负载均衡等优化策略
- **监控指标** - 详细的连接池统计信息和性能监控

### 🛡️ 元数据备份恢复
- **版本管理系统** - 语义化版本控制，支持版本比较和冲突检测
- **分布式锁机制** - 防止多节点并发冲突，确保数据一致性
- **自动同步策略** - 启动时同步、增量同步、冲突解决
- **多种备份模式** - 自动定时备份、手动触发备份、备份验证
- **灾难恢复能力** - 完整的恢复流程，支持多种恢复模式
- **高可用保障** - 多节点协调、故障检测、自动恢复

### 💾 存储特性
- **列式存储** - 使用Apache Parquet格式，压缩率高
- **多级缓存** - 内存缓冲区 + 磁盘存储的多级架构
- **数据备份** - 支持自动和手动备份机制
- **灾难恢复** - 基于时间范围和ID的数据恢复
- **表级配置** - 每个表可以有独立的存储策略和保留策略
- **智能分区** - 按表、ID和时间的三级分区存储

### 🔌 接口特性
- **双协议支持** - 同时提供gRPC和RESTful API
- **多语言客户端** - 支持Go、Java、Node.js等多种语言
- **标准SQL** - 支持标准SQL查询语法
- **流式处理** - 支持大数据量的流式读写
- **表管理API** - 完整的表创建、删除、列表和描述接口
- **元数据管理API** - 完整的备份、恢复、状态查询接口

### 🛡️ 安全特性
- **JWT认证** - 支持JWT令牌认证
- **TLS加密** - 支持传输层加密
- **API密钥** - 支持API密钥认证

### 📊 运维特性
- **健康检查** - 内置健康检查接口，支持组件级状态监控
- **指标监控** - 集成Prometheus指标，包含连接池和备份恢复指标
- **日志管理** - 结构化日志输出，支持不同级别的日志记录
- **性能统计** - 详细的性能指标统计，包含连接池性能数据
- **故障恢复** - 自动故障检测和恢复机制
- **熔断器模式** - 防止级联故障的熔断器实现

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

#### 数据写入（支持表概念）
```bash
curl -X POST http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "table": "users",
    "id": "user-123",
    "timestamp": "2024-01-01T10:00:00Z",
    "payload": {
      "name": "John Doe",
      "age": 30,
      "score": 95.5
    }
  }'
```

#### 数据查询（支持表名）
```bash
curl -X POST http://localhost:8081/v1/query \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "sql": "SELECT COUNT(*) FROM users WHERE age > 25"
  }'
```

#### 跨表查询
```bash
curl -X POST http://localhost:8081/v1/query \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "sql": "SELECT u.name, o.amount FROM users u JOIN orders o ON u.id = o.user_id WHERE u.age > 25"
  }'
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
      "backup_enabled": true
    }
  }'
```

##### 列出表
```bash
curl http://localhost:8081/v1/tables \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 描述表
```bash
curl http://localhost:8081/v1/tables/users \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 删除表
```bash
curl -X DELETE http://localhost:8081/v1/tables/users?cascade=true \
  -H "Authorization: Bearer YOUR_TOKEN"
```

#### 元数据备份恢复API（新增）

##### 触发元数据备份
```bash
curl -X POST http://localhost:8081/v1/metadata/backup \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "backup_type": "full",
    "description": "Manual backup before system upgrade"
  }'
```

##### 列出元数据备份
```bash
curl http://localhost:8081/v1/metadata/backups \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 恢复元数据
```bash
curl -X POST http://localhost:8081/v1/metadata/recover \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "backup_file": "backup_20240115_103000.json",
    "mode": "complete",
    "force_overwrite": false,
    "backup_current": true
  }'
```

##### 获取元数据状态
```bash
curl http://localhost:8081/v1/metadata/status \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 验证元数据备份
```bash
curl -X POST http://localhost:8081/v1/metadata/validate \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "backup_file": "backup_20240115_103000.json"
  }'
```

#### 系统监控API

##### 健康检查
```bash
curl http://localhost:8081/v1/health
```

##### 连接池状态（新增）
```bash
curl http://localhost:8081/v1/pool/status \
  -H "Authorization: Bearer YOUR_TOKEN"
```

##### 系统统计信息
```bash
curl http://localhost:8081/v1/stats \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### gRPC API

```protobuf
service OlapService {
  // 数据操作
  rpc Write(WriteRequest) returns (WriteResponse);
  rpc Query(QueryRequest) returns (QueryResponse);
  rpc TriggerBackup(TriggerBackupRequest) returns (TriggerBackupResponse);
  rpc RecoverData(RecoverDataRequest) returns (RecoverDataResponse);
  
  // 系统管理
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc GetStats(GetStatsRequest) returns (GetStatsResponse);
  rpc GetNodes(GetNodesRequest) returns (GetNodesResponse);
  
  // 表管理（新增）
  rpc CreateTable(CreateTableRequest) returns (CreateTableResponse);
  rpc DropTable(DropTableRequest) returns (DropTableResponse);
  rpc ListTables(ListTablesRequest) returns (ListTablesResponse);
  rpc DescribeTable(DescribeTableRequest) returns (DescribeTableResponse);
}
```

完整API文档请参考：[API参考](api/README.md)

## 🔧 客户端示例

### Go客户端
```go
package main

import (
    "context"
    "log"
    "google.golang.org/grpc"
    "google.golang.org/protobuf/types/known/timestamppb"
    "google.golang.org/protobuf/types/known/structpb"
    pb "minIODB/api/proto/olap/v1"
)

func main() {
    conn, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    client := pb.NewOlapServiceClient(conn)
    
    // 创建表
    _, err = client.CreateTable(context.Background(), &pb.CreateTableRequest{
        TableName: "users",
        Config: &pb.TableConfig{
            BufferSize:           1000,
            FlushIntervalSeconds: 30,
            RetentionDays:        365,
            BackupEnabled:        true,
            Properties: map[string]string{
                "description": "用户数据表",
                "owner":       "user-service",
            },
        },
        IfNotExists: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // 写入数据（指定表名）
    _, err = client.Write(context.Background(), &pb.WriteRequest{
        Table:     "users",  // 新增：指定表名
        Id:        "user-123",
        Timestamp: timestamppb.Now(),
        Payload: &structpb.Struct{
            Fields: map[string]*structpb.Value{
                "name": structpb.NewStringValue("John"),
                "age":  structpb.NewNumberValue(30),
            },
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // 查询数据
    resp, err := client.Query(context.Background(), &pb.QueryRequest{
        Sql: "SELECT COUNT(*) FROM users WHERE age > 25",
    })
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Query result: %s", resp.ResultJson)
}
```

### Java客户端
```java
OlapServiceGrpc.OlapServiceBlockingStub stub = 
    OlapServiceGrpc.newBlockingStub(channel);

// 创建表
CreateTableRequest createTableRequest = CreateTableRequest.newBuilder()
    .setTableName("users")
    .setConfig(TableConfig.newBuilder()
        .setBufferSize(1000)
        .setFlushIntervalSeconds(30)
        .setRetentionDays(365)
        .setBackupEnabled(true)
        .putProperties("description", "用户数据表")
        .putProperties("owner", "user-service")
        .build())
    .setIfNotExists(true)
    .build();

CreateTableResponse createResp = stub.createTable(createTableRequest);

// 写入数据（指定表名）
WriteRequest request = WriteRequest.newBuilder()
    .setTable("users")  // 新增：指定表名
    .setId("user-123")
    .setTimestamp(Timestamps.fromMillis(System.currentTimeMillis()))
    .setPayload(Struct.newBuilder()
        .putFields("name", Value.newBuilder().setStringValue("John").build())
        .putFields("age", Value.newBuilder().setNumberValue(30).build())
        .build())
    .build();

WriteResponse response = stub.write(request);
```

### Node.js客户端
```javascript
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');

const packageDefinition = protoLoader.loadSync('olap.proto');
const olapProto = grpc.loadPackageDefinition(packageDefinition);

const client = new olapProto.olap.v1.OlapService(
    'localhost:8080', 
    grpc.credentials.createInsecure()
);

// 创建表
client.createTable({
    table_name: 'users',
    config: {
        buffer_size: 1000,
        flush_interval_seconds: 30,
        retention_days: 365,
        backup_enabled: true,
        properties: {
            description: '用户数据表',
            owner: 'user-service'
        }
    },
    if_not_exists: true
}, (error, response) => {
    if (error) {
        console.error('Create table error:', error);
    } else {
        console.log('Table created:', response);
    }
});

// 写入数据（指定表名）
client.write({
    table: 'users',  // 新增：指定表名
    id: 'user-123',
    timestamp: { seconds: Math.floor(Date.now() / 1000) },
    payload: {
        fields: {
            name: { string_value: 'John' },
            age: { number_value: 30 }
        }
    }
}, (error, response) => {
    if (error) {
        console.error(error);
    } else {
        console.log('Success:', response);
    }
});
```

更多客户端示例请参考：[examples/](examples/)

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
    │  - Table Management (NEW)       │
    └────────────────┬────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
        ▼            ▼            ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│   Redis     │ │ Worker Node │ │ Worker Node │
│ Metadata    │ │   + DuckDB  │ │   + DuckDB  │
│ & Discovery │ │ + TableMgr  │ │ + TableMgr  │
│ + TableMeta │ │             │ │             │
└─────────────┘ └─────────────┘ └─────────────┘
                       │            │
                       └────────────┘
                              │
                    ┌─────────▼─────────┐
                    │   MinIO Cluster   │
                    │ (Object Storage)  │
                    │  TABLE/ID/DATE/   │
                    └───────────────────┘
```

### 核心组件

#### 1. API网关层
- **请求路由** - 智能路由到最优节点
- **负载均衡** - 请求分发和负载均衡
- **认证授权** - JWT令牌验证和权限控制
- **结果聚合** - 多节点查询结果聚合
- **表管理** - 表的创建、删除、列表和描述管理

#### 2. 计算节点层
- **DuckDB引擎** - 高性能OLAP查询引擎
- **数据缓冲** - 内存缓冲区管理，支持表级分离
- **文件生成** - Parquet文件生成和上传，按表分区
- **查询执行** - 分布式查询执行，支持跨表查询
- **表管理器** - 表级配置和生命周期管理

#### 3. 元数据层
- **服务发现** - 节点注册和健康检查
- **数据索引** - 文件位置和元数据索引，支持表级索引
- **哈希环** - 一致性哈希数据分片
- **配置管理** - 集群配置和状态管理
- **表元数据** - 表配置、统计信息和权限管理

#### 4. 存储层
- **主存储** - MinIO对象存储集群
- **备份存储** - 独立的备份存储
- **数据格式** - Apache Parquet列式存储
- **数据组织** - 按表、ID和时间的三级分区存储

## 🔄 核心流程

### 数据写入流程（支持表）
1. 客户端发送写入请求到API网关（包含表名）
2. 网关验证表是否存在，如启用自动创建则自动创建表
3. 根据表名和ID计算目标节点（一致性哈希）
4. 目标节点将数据写入表级内存缓冲区
5. 缓冲区达到表级阈值时批量生成Parquet文件
6. 文件上传到MinIO主存储的表分区路径
7. 更新Redis中的表级数据索引
8. 异步备份到备份存储（如果表启用备份）

### 数据查询流程（支持跨表）
1. 客户端发送查询请求到API网关
2. 网关解析SQL并提取涉及的表名和数据范围
3. 验证表级权限和表存在性
4. 查询Redis获取相关表的文件列表
5. 将查询任务分发到相关计算节点
6. 各节点使用DuckDB执行子查询（支持跨表JOIN）
7. 网关聚合所有节点的查询结果
8. 返回最终结果给客户端

### 表管理流程
1. 客户端发送表管理请求（创建/删除/列表/描述）
2. 网关验证权限和表名合法性
3. 表管理器执行相应操作
4. 更新Redis中的表元数据
5. 对于删除操作，级联删除MinIO中的表数据
6. 返回操作结果给客户端

## 📊 性能特点

### 查询性能
- **列式存储** - Parquet格式，查询性能优异
- **向量化计算** - DuckDB向量化执行引擎
- **并行处理** - 多节点并行查询执行
- **智能缓存** - 多级缓存机制

### 存储性能
- **高压缩比** - Parquet格式压缩比高达10:1
- **快速写入** - 批量写入和异步处理
- **水平扩展** - 存储容量线性扩展
- **数据备份** - 自动备份不影响主流程

### 网络性能
- **协议优化** - gRPC高效二进制协议
- **连接复用** - HTTP/2连接复用
- **压缩传输** - 数据传输压缩
- **流式处理** - 大数据量流式传输

## 🛠️ 配置说明

### 基础配置
```yaml
server:
  grpc_port: ":8080"
  rest_port: ":8081"
  node_id: "node-1"

redis:
  mode: "standalone"
  addr: "localhost:6379"
  password: ""
  db: 0

minio:
  endpoint: "localhost:9000"
  access_key_id: "minioadmin"
  secret_access_key: "minioadmin"
  bucket: "olap-data"
```

### 高级配置
```yaml
buffer:
  buffer_size: 1000
  flush_interval: 30s
  worker_pool_size: 10
  batch_flush_size: 5

backup:
  enabled: true
  interval: 3600
  minio:
    endpoint: "backup-minio:9000"
    bucket: "olap-backup"

security:
  mode: "token"
  jwt_secret: "your-secret-key"
  enable_tls: false

# 表管理配置
tables:
  # 默认表配置
  default_config:
    buffer_size: 1000
    flush_interval: 30s
    retention_days: 365
    backup_enabled: true
  
  # 表级配置覆盖
  users:
    buffer_size: 2000
    flush_interval: 60s
    retention_days: 2555  # 7年
    backup_enabled: true
    properties:
      description: "用户数据表"
      owner: "user-service"
  
  orders:
    buffer_size: 5000
    flush_interval: 10s
    retention_days: 2555
    backup_enabled: true
    properties:
      description: "订单数据表"
      owner: "order-service"
  
  logs:
    buffer_size: 10000
    flush_interval: 5s
    retention_days: 30
    backup_enabled: false
    properties:
      description: "应用日志表"
      owner: "log-service"

# 表管理配置
table_management:
  auto_create_tables: true      # 是否自动创建表
  default_table: "default"      # 默认表名（向后兼容）
  max_tables: 1000             # 最大表数量限制
  table_name_pattern: "^[a-zA-Z][a-zA-Z0-9_]{0,63}$"  # 表名规则
```

完整配置说明请参考：[配置文档](docs/configuration.md)

## 🧪 测试

### 运行测试
```bash
# 运行所有测试
go test ./...

# 运行特定模块测试
go test ./internal/storage/...

# 运行基准测试
go test -bench=. ./internal/query/...

# 生成测试覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### 集成测试
```bash
# 启动测试环境
docker-compose -f test/docker-compose.test.yml up -d

# 运行集成测试
go test -tags=integration ./test/...
```

## 📈 监控

### Prometheus指标
```yaml
# 系统指标
miniodb_requests_total{method="write",status="success",table="users"}
miniodb_requests_duration_seconds{method="query",table="orders"}
miniodb_buffer_size_bytes{node="node-1",table="users"}
miniodb_storage_objects_total{bucket="olap-data",table="users"}

# 表级指标
miniodb_table_record_count{table="users"}
miniodb_table_file_count{table="orders"}
miniodb_table_size_bytes{table="logs"}
miniodb_table_buffer_utilization{table="users"}

# 业务指标
miniodb_data_points_total{id="user-123",table="users"}
miniodb_query_latency_seconds{sql_type="select",table="users"}
miniodb_backup_files_total{status="success",table="orders"}
```

### 健康检查
```bash
# 基础健康检查
curl http://localhost:8081/v1/health

# 详细状态检查
curl http://localhost:8081/v1/stats

# 表级状态检查
curl http://localhost:8081/v1/tables/users
```

## 🔍 故障排除

### 常见问题

#### 1. 连接Redis失败
```bash
# 检查Redis连接
redis-cli -h localhost -p 6379 ping

# 检查配置
grep -A 5 "redis:" config.yaml
```

#### 2. MinIO连接失败
```bash
# 检查MinIO服务
curl http://localhost:9000/minio/health/live

# 检查存储桶
mc ls minio/olap-data
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
```

##### 表配置问题
```bash
# 查看表配置
curl http://localhost:8081/v1/tables/your_table | jq '.table_info.config'

# 检查表级Redis配置
redis-cli hgetall "table:your_table:config"
```

##### 表数据查询慢
```bash
# 查看表统计信息
curl http://localhost:8081/v1/tables/your_table | jq '.stats'

# 检查表级索引
redis-cli keys "index:table:your_table:*" | wc -l
```

#### 4. 查询性能慢
```bash
# 查看查询统计
curl http://localhost:8081/v1/stats | jq '.query_stats'

# 检查表级缓冲区状态
curl http://localhost:8081/v1/stats | jq '.buffer_stats'
```

更多故障排除请参考：[故障排除指南](docs/troubleshooting.md)

## 🤝 贡献指南

我们欢迎所有形式的贡献！

### 开发环境设置
```bash
# 1. Fork项目
git clone https://github.com/your-username/minIODB.git

# 2. 创建功能分支
git checkout -b feature/new-feature

# 3. 安装开发依赖
go mod download
make dev-setup

# 4. 运行测试
make test

# 5. 提交更改
git commit -m "Add new feature"
git push origin feature/new-feature
```

### 代码规范
- 遵循Go官方代码规范
- 使用gofmt格式化代码
- 添加适当的注释和文档
- 编写单元测试和集成测试

### 提交PR
1. 确保所有测试通过
2. 更新相关文档
3. 填写PR模板
4. 等待代码审查

## 📄 许可证

本项目采用MIT许可证 - 详情请参考 [LICENSE](LICENSE) 文件。

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
