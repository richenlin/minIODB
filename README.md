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

## ✨ 核心特性

### 🏗️ 架构特性
- **存算分离** - MinIO负责存储，DuckDB负责计算，实现极致弹性
- **元数据驱动** - Redis管理服务发现、节点状态和数据索引
- **自动分片** - 基于一致性哈希的透明数据分片
- **水平扩展** - 支持动态节点加入和数据重分布

### 💾 存储特性
- **列式存储** - 使用Apache Parquet格式，压缩率高
- **多级缓存** - 内存缓冲区 + 磁盘存储的多级架构
- **数据备份** - 支持自动和手动备份机制
- **灾难恢复** - 基于时间范围和ID的数据恢复

### 🔌 接口特性
- **双协议支持** - 同时提供gRPC和RESTful API
- **多语言客户端** - 支持Go、Java、Node.js等多种语言
- **标准SQL** - 支持标准SQL查询语法
- **流式处理** - 支持大数据量的流式读写

### 🛡️ 安全特性
- **JWT认证** - 支持JWT令牌认证
- **TLS加密** - 支持传输层加密
- **API密钥** - 支持API密钥认证

### 📊 运维特性
- **健康检查** - 内置健康检查接口
- **指标监控** - 集成Prometheus指标
- **日志管理** - 结构化日志输出
- **性能统计** - 详细的性能指标统计

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

#### 数据写入
```bash
curl -X POST http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "id": "user-123",
    "timestamp": "2024-01-01T10:00:00Z",
    "payload": {
      "name": "John Doe",
      "age": 30,
      "score": 95.5
    }
  }'
```

#### 数据查询
```bash
curl -X POST http://localhost:8081/v1/query \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "sql": "SELECT COUNT(*) FROM table WHERE id = '\''user-123'\'' AND age > 25"
  }'
```

#### 数据备份
```bash
curl -X POST http://localhost:8081/v1/backup/trigger \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "id": "user-123",
    "day": "2024-01-01"
  }'
```

#### 数据恢复
```bash
curl -X POST http://localhost:8081/v1/backup/recover \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "id_range": {
      "ids": ["user-123", "user-456"]
    },
    "force_overwrite": false
  }'
```

### gRPC API

```protobuf
service OlapService {
  rpc Write(WriteRequest) returns (WriteResponse);
  rpc Query(QueryRequest) returns (QueryResponse);
  rpc TriggerBackup(TriggerBackupRequest) returns (TriggerBackupResponse);
  rpc RecoverData(RecoverDataRequest) returns (RecoverDataResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc GetStats(GetStatsRequest) returns (GetStatsResponse);
  rpc GetNodes(GetNodesRequest) returns (GetNodesResponse);
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
    pb "minIODB/api/proto/olap/v1"
)

func main() {
    conn, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    client := pb.NewOlapServiceClient(conn)
    
    // 写入数据
    _, err = client.Write(context.Background(), &pb.WriteRequest{
        Id: "user-123",
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
}
```

### Java客户端
```java
OlapServiceGrpc.OlapServiceBlockingStub stub = 
    OlapServiceGrpc.newBlockingStub(channel);

WriteRequest request = WriteRequest.newBuilder()
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

client.write({
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
    └────────────────┬────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
        ▼            ▼            ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│   Redis     │ │ Worker Node │ │ Worker Node │
│ Metadata    │ │   + DuckDB  │ │   + DuckDB  │
│ & Discovery │ │             │ │             │
└─────────────┘ └─────────────┘ └─────────────┘
                       │            │
                       └────────────┘
                              │
                    ┌─────────▼─────────┐
                    │   MinIO Cluster   │
                    │ (Object Storage)  │
                    └───────────────────┘
```

### 核心组件

#### 1. API网关层
- **请求路由** - 智能路由到最优节点
- **负载均衡** - 请求分发和负载均衡
- **认证授权** - JWT令牌验证和权限控制
- **结果聚合** - 多节点查询结果聚合

#### 2. 计算节点层
- **DuckDB引擎** - 高性能OLAP查询引擎
- **数据缓冲** - 内存缓冲区管理
- **文件生成** - Parquet文件生成和上传
- **查询执行** - 分布式查询执行

#### 3. 元数据层
- **服务发现** - 节点注册和健康检查
- **数据索引** - 文件位置和元数据索引
- **哈希环** - 一致性哈希数据分片
- **配置管理** - 集群配置和状态管理

#### 4. 存储层
- **主存储** - MinIO对象存储集群
- **备份存储** - 独立的备份存储
- **数据格式** - Apache Parquet列式存储
- **数据组织** - 按ID和时间分区存储

## 🔄 核心流程

### 数据写入流程
1. 客户端发送写入请求到API网关
2. 网关根据ID计算目标节点（一致性哈希）
3. 目标节点将数据写入内存缓冲区
4. 缓冲区达到阈值时批量生成Parquet文件
5. 文件上传到MinIO主存储
6. 更新Redis中的数据索引
7. 异步备份到备份存储（如果启用）

### 数据查询流程
1. 客户端发送查询请求到API网关
2. 网关解析SQL并确定涉及的数据范围
3. 查询Redis获取相关文件列表
4. 将查询任务分发到相关计算节点
5. 各节点使用DuckDB执行子查询
6. 网关聚合所有节点的查询结果
7. 返回最终结果给客户端

### 节点扩容流程
1. 新节点启动并注册到Redis
2. 系统检测到节点变化
3. 重新计算一致性哈希环
4. 新数据自动路由到新节点
5. 历史数据保持原有分布（可选重分布）

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
miniodb_requests_total{method="write",status="success"}
miniodb_requests_duration_seconds{method="query"}
miniodb_buffer_size_bytes{node="node-1"}
miniodb_storage_objects_total{bucket="olap-data"}

# 业务指标
miniodb_data_points_total{id="user-123"}
miniodb_query_latency_seconds{sql_type="select"}
miniodb_backup_files_total{status="success"}
```

### 健康检查
```bash
# 基础健康检查
curl http://localhost:8081/v1/health

# 详细状态检查
curl http://localhost:8081/v1/stats
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

#### 3. 查询性能慢
```bash
# 查看查询统计
curl http://localhost:8081/v1/stats | jq '.query_stats'

# 检查索引状态
redis-cli keys "index:*" | wc -l
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
