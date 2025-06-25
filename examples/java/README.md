# Java客户端示例

这个示例展示了如何使用Java客户端连接到MinIODB系统，支持gRPC和REST两种协议。

## 依赖要求

- Java 8+
- Maven 3.6+

## 快速开始

### 1. 编译和运行

```bash
cd examples/java
mvn clean compile exec:java -Dexec.mainClass="com.miniodb.examples.GrpcClientExample"
```

### 2. 运行REST客户端示例

```bash
mvn exec:java -Dexec.mainClass="com.miniodb.examples.RestClientExample"
```

## 示例功能

### GrpcClientExample.java
- 连接到gRPC服务器
- JWT认证
- 数据写入和查询
- 备份和恢复操作
- 错误处理

### RestClientExample.java
- 使用HTTP客户端连接REST API
- 认证和授权
- 完整的CRUD操作
- JSON数据处理

## 配置

默认连接配置：
- gRPC服务器：`localhost:8080`
- REST服务器：`localhost:8081`
- 认证：JWT Token

可以通过环境变量或配置文件修改这些设置。

## 错误处理

示例包含了完整的错误处理机制：
- 连接超时
- 认证失败
- 请求重试
- 异常日志记录 