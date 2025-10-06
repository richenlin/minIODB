# MinIODB 多语言 SDK

本目录包含 MinIODB 的多语言客户端 SDK，支持 Java、Python 和 Go 三种编程语言。

## 快速开始

### 前置要求

确保 MinIODB 服务已经启动并运行：

```bash
# 启动 MinIO
docker run -d -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address ":9001"

# 启动 MinIODB 服务
cd /path/to/minIODB
go run cmd/main.go -config config.simple.yaml
```

### 支持的语言

- **[Java SDK](java/README.md)** - 适用于企业级 Java 应用
- **[Python SDK](python/README.md)** - 适用于数据科学和快速开发
- **[Go SDK](golang/README.md)** - 适用于高性能 Go 应用
- **[Node.js SDK](nodejs/README.md)** - 适用于现代 Web 应用和微服务

## 核心功能

所有 SDK 都提供以下核心功能：

### 数据操作
- **写入数据**: 支持单条和批量数据写入
- **查询数据**: 支持 SQL 查询和分页
- **更新数据**: 支持按 ID 更新记录
- **删除数据**: 支持软删除和硬删除
- **流式操作**: 支持大数据量的流式读写

### 表管理
- **创建表**: 创建新的数据表
- **列出表**: 获取所有表的列表
- **获取表信息**: 获取表的详细信息和统计
- **删除表**: 删除表及其数据

### 元数据管理
- **备份元数据**: 手动或自动备份元数据
- **恢复元数据**: 从备份文件恢复元数据
- **列出备份**: 查看可用的备份文件
- **获取状态**: 获取元数据管理状态

### 监控和健康检查
- **健康检查**: 检查服务健康状态
- **获取状态**: 获取系统运行状态
- **获取指标**: 获取性能指标数据

### 认证
- **获取令牌**: 使用 API 密钥获取访问令牌
- **刷新令牌**: 刷新过期的访问令牌
- **撤销令牌**: 撤销不再需要的令牌

## 统一配置

所有 SDK 都支持以下配置选项：

```yaml
# MinIODB 连接配置
server:
  host: "localhost"
  grpc_port: 8080
  rest_port: 8081

# 认证配置（可选）
auth:
  api_key: "your-api-key"
  secret: "your-secret"

# 连接池配置
connection:
  max_connections: 10
  timeout: 30s
  retry_attempts: 3

# 日志配置
logging:
  level: "info"
  format: "json"
```

## 错误处理

所有 SDK 都提供统一的错误处理机制：

- **连接错误**: 网络连接问题
- **认证错误**: 认证失败或令牌过期
- **请求错误**: 请求参数错误或格式错误
- **服务错误**: 服务端内部错误
- **超时错误**: 请求超时

## 性能特性

- **连接池**: 自动管理 gRPC 连接池
- **重试机制**: 自动重试失败的请求
- **流式操作**: 支持大数据量的流式处理
- **异步操作**: 支持异步操作（语言特定）
- **批量操作**: 支持批量数据操作

## 示例用法

### 基本数据操作示例

```java
// Java
MinIODBClient client = new MinIODBClient(config);
client.writeData("users", record);
QueryResponse response = client.queryData("SELECT * FROM users LIMIT 10");
```

```python
# Python
client = MinIODBClient(config)
client.write_data("users", record)
response = client.query_data("SELECT * FROM users LIMIT 10")
```

```go
// Go
client, err := miniodb.NewClient(config)
client.WriteData(ctx, "users", record)
response, err := client.QueryData(ctx, "SELECT * FROM users LIMIT 10")
```

```typescript
// Node.js/TypeScript
const client = new MinIODBClient(config);
await client.writeData("users", record);
const response = await client.queryData("SELECT * FROM users LIMIT 10");
```

## 开发和贡献

### 代码生成

使用以下脚本生成 gRPC 客户端代码：

```bash
# 生成 Java 代码
./scripts/generate_java.sh

# 生成 Python 代码
./scripts/generate_python.sh

# 生成 Go 代码
./scripts/generate_go.sh

# 生成 Node.js 代码
./scripts/generate_nodejs.sh
```

### 测试

每个 SDK 都包含完整的单元测试和集成测试：

```bash
# Java
cd java && mvn test

# Python
cd python && python -m pytest

# Go
cd golang && go test ./...

# Node.js
cd nodejs && npm test
```

## 许可证

本项目采用与 MinIODB 相同的 BSD-3-Clause 许可证。
