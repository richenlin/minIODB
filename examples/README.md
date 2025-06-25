# MinIODB客户端示例

这个目录包含了MinIODB系统的多语言客户端示例，展示了如何使用不同编程语言连接和操作MinIODB系统。

## 🌟 支持的语言

- [**Java**](./java/) - 企业级Java客户端，支持Maven构建
- [**Go**](./go/) - 高性能Go客户端，原生支持gRPC
- [**Node.js**](./node/) - 现代JavaScript客户端，支持async/await
- [**Legacy Go**](./auth_example.go) - 原有的Go认证示例

## 📋 功能特性

所有客户端示例都包含以下完整功能：

### 🔌 连接方式
- **gRPC客户端** - 高性能的Protocol Buffers通信
- **REST客户端** - 标准的HTTP RESTful API

### 💾 数据操作
- **数据写入** - 支持结构化数据存储
- **数据查询** - 使用SQL查询语言
- **批量操作** - 高效的批量数据处理

### 🔧 系统管理
- **备份管理** - 手动触发数据备份
- **数据恢复** - 按时间范围或ID恢复数据
- **健康检查** - 系统状态监控
- **统计信息** - 系统性能指标
- **节点管理** - 集群节点信息

### 🔐 安全认证
- **JWT令牌认证** - 安全的身份验证
- **请求授权** - 基于角色的访问控制
- **连接加密** - 支持TLS/SSL加密传输

## 🚀 快速开始

### 1. 启动MinIODB服务器

```bash
# 确保MinIODB服务器正在运行
cd /path/to/minIODB
go run cmd/server/main.go
```

### 2. 选择语言并运行示例

#### Java示例
```bash
cd examples/java
mvn clean compile exec:java -Dexec.mainClass="com.miniodb.examples.RestClientExample"
```

#### Go示例
```bash
cd examples/go
go mod tidy
go run rest_client_example.go
```

#### Node.js示例
```bash
cd examples/node
npm install
npm run rest-example
```

## ⚙️ 配置说明

### 环境变量

所有客户端都支持以下环境变量配置：

```bash
# 服务器地址配置
export MINIODB_GRPC_HOST="localhost"
export MINIODB_GRPC_PORT="8080"
export MINIODB_REST_HOST="http://localhost:8081"

# 认证配置
export MINIODB_JWT_TOKEN="your-jwt-token-here"
```

### 默认配置

如果不设置环境变量，将使用以下默认配置：

- **gRPC服务器**: `localhost:8080`
- **REST服务器**: `http://localhost:8081`
- **JWT令牌**: `your-jwt-token-here`

## 📚 示例详情

### Java客户端特性
- Maven项目管理
- SLF4J日志框架
- Jackson JSON处理
- OkHttp HTTP客户端
- gRPC Java库
- 完整的异常处理

### Go客户端特性
- Go modules依赖管理
- Logrus结构化日志
- 原生JSON支持
- 标准HTTP客户端
- gRPC Go库
- 优雅的错误处理

### Node.js客户端特性
- NPM包管理
- Winston日志框架
- Axios HTTP客户端
- async/await异步处理
- gRPC Node.js库
- Promise错误处理

## 🔧 开发和测试

### 添加新的操作示例

1. 在对应语言目录中添加新的方法
2. 实现错误处理和日志记录
3. 添加到`runAllExamples`方法中
4. 更新README文档

### 调试技巧

```bash
# 启用详细日志
export LOG_LEVEL=debug

# 测试连接
curl http://localhost:8081/v1/health

# 查看gRPC服务状态
grpcurl -plaintext localhost:8080 list
```

## 📖 API文档

### REST API端点

- `GET /v1/health` - 健康检查
- `POST /v1/data` - 数据写入
- `POST /v1/query` - 数据查询
- `POST /v1/backup/trigger` - 触发备份
- `POST /v1/backup/recover` - 数据恢复
- `GET /v1/stats` - 系统统计
- `GET /v1/nodes` - 节点信息

### gRPC服务方法

- `HealthCheck` - 健康检查
- `Write` - 数据写入
- `Query` - 数据查询
- `TriggerBackup` - 触发备份
- `RecoverData` - 数据恢复
- `GetStats` - 系统统计
- `GetNodes` - 节点信息

## 🤝 贡献指南

1. 添加新语言的客户端示例
2. 改进现有示例的功能
3. 添加更多的使用场景
4. 优化错误处理和日志记录
5. 更新文档和注释

## 📄 许可证

这些示例代码遵循与MinIODB主项目相同的许可证。

## 🔗 相关链接

- [MinIODB主项目](../../README.md)
- [API文档](../../api/proto/olap/v1/olap.proto)
- [配置文档](../../config.yaml)
- [部署指南](../../docker-compose.yml) 