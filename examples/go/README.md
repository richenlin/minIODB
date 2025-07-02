# Go客户端示例

这个示例展示了如何使用Go客户端连接到MinIODB系统，支持gRPC和REST两种协议。

## 依赖要求

- Go 1.21+
- Go modules

## 快速开始

### 1. 安装依赖

```bash
cd examples/go
go mod tidy
```

### 2. 运行gRPC客户端示例

```bash
go run grpc_client_example.go
```

### 3. 运行REST客户端示例

```bash
go run rest_client_example.go
```

## 示例功能

### grpc_client_example.go
- 连接到gRPC服务器
- JWT认证
- 数据写入和查询
- 备份和恢复操作
- 错误处理和重试机制

### rest_client_example.go
- 使用HTTP客户端连接REST API
- 认证和授权
- 完整的CRUD操作
- JSON数据处理

## 配置

默认连接配置：
- gRPC服务器：`localhost:8080`
- REST服务器：`localhost:8081`
- 认证：JWT Token

可以通过环境变量修改这些设置：
- `MINIODB_GRPC_HOST`: gRPC服务器地址
- `MINIODB_REST_HOST`: REST服务器地址
- `MINIODB_JWT_TOKEN`: JWT认证令牌

## 特性

- 自动重连机制
- 连接池管理
- 超时控制
- 详细的错误日志
- 结构化JSON处理
- 并发安全 