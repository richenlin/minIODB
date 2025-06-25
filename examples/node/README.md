# Node.js客户端示例

这个示例展示了如何使用Node.js客户端连接到MinIODB系统，支持gRPC和REST两种协议。

## 依赖要求

- Node.js 16+
- npm 或 yarn

## 快速开始

### 1. 安装依赖

```bash
cd examples/node
npm install
```

### 2. 运行gRPC客户端示例

```bash
npm run grpc-example
```

### 3. 运行REST客户端示例

```bash
npm run rest-example
```

## 示例功能

### grpc-client-example.js
- 连接到gRPC服务器
- JWT认证
- 数据写入和查询
- 备份和恢复操作
- 异步/await模式
- 错误处理和重试机制

### rest-client-example.js
- 使用axios HTTP客户端连接REST API
- 认证和授权
- 完整的CRUD操作
- JSON数据处理
- Promise链式调用

## 配置

默认连接配置：
- gRPC服务器：`localhost:8080`
- REST服务器：`http://localhost:8081`
- 认证：JWT Token

可以通过环境变量修改这些设置：
- `MINIODB_GRPC_HOST`: gRPC服务器地址
- `MINIODB_GRPC_PORT`: gRPC服务器端口
- `MINIODB_REST_HOST`: REST服务器地址
- `MINIODB_JWT_TOKEN`: JWT认证令牌

## 特性

- ES6+ 现代JavaScript语法
- async/await 异步处理
- 结构化错误处理
- 详细的日志输出
- TypeScript类型定义支持
- 自动重试机制
- 连接池管理 