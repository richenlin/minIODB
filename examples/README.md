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

## 🆕 新特性：表管理功能

所有示例都已更新以支持新的表（Table）管理功能，包括：
- 表的创建、删除、列表和描述
- 数据写入时指定表名
- 表级查询和跨表查询
- 表级配置和权限管理

## 📁 目录结构

```
examples/
├── go/                    # Go语言客户端示例
│   ├── rest_client_example.go    # REST API客户端示例
│   ├── go.mod                    # Go模块文件
│   └── README.md                 # Go示例说明
├── java/                  # Java客户端示例
│   ├── src/main/java/com/miniodb/examples/
│   │   ├── RestClientExample.java     # REST API客户端示例
│   │   └── GrpcClientExample.java     # gRPC客户端示例
│   ├── pom.xml                        # Maven配置文件
│   └── README.md                      # Java示例说明
├── node/                  # Node.js客户端示例
│   ├── rest-client-example.js    # REST API客户端示例
│   ├── package.json              # NPM配置文件
│   └── README.md                 # Node.js示例说明
├── auth_example.go        # 认证功能示例
├── buffer_query_example.go # 缓冲区查询示例
├── run_examples.sh        # 一键运行脚本
└── README.md             # 本文档
```

## 🚀 快速开始

### 1. 启动MinIODB服务

确保MinIODB服务已启动并运行在默认端口：
- gRPC: 8080
- REST: 8081

### 2. 运行所有示例

```bash
# 一键运行所有示例
./run_examples.sh

# 或者单独运行特定示例
cd go && go run rest_client_example.go
cd java && mvn compile exec:java -Dexec.mainClass="com.miniodb.examples.RestClientExample"
cd node && node rest-client-example.js
```

## 📖 示例说明

### Go语言示例

#### REST客户端示例 (`go/rest_client_example.go`)

演示完整的REST API使用流程，包含表管理功能：

**表管理功能：**
```go
// 创建表
client.CreateTable()

// 列出表
client.ListTables()

// 描述表
client.DescribeTable()

// 删除表
client.DropTable()
```

**数据操作（支持表）：**
```go
// 写入数据到指定表
request := WriteRequest{
    Table:     "users",  // 指定表名
    ID:        "user123",
    Timestamp: time.Now().Format(time.RFC3339),
    Payload:   map[string]interface{}{...},
}

// 查询指定表的数据
sql := "SELECT COUNT(*) FROM users WHERE user_id = 'user123'"
```

**跨表查询：**
```go
// 跨表JOIN查询
sql := `
    SELECT u.user_id, u.action, o.order_id, o.amount
    FROM users u 
    JOIN orders o ON u.user_id = o.user_id 
    WHERE u.user_id = 'user123'
`
```

### Java示例

#### REST客户端示例 (`java/src/main/java/com/miniodb/examples/RestClientExample.java`)

Java版本的REST API客户端，支持完整的表管理功能：

```java
// 创建表配置
ObjectNode config = objectMapper.createObjectNode();
config.put("buffer_size", 1000);
config.put("flush_interval_seconds", 30);
config.put("retention_days", 365);
config.put("backup_enabled", true);

// 创建表
ObjectNode requestData = objectMapper.createObjectNode();
requestData.put("table_name", "users");
requestData.set("config", config);
requestData.put("if_not_exists", true);

// 写入数据到表
ObjectNode writeData = objectMapper.createObjectNode();
writeData.put("table", "users");  // 指定表名
writeData.put("id", "user123");
writeData.put("timestamp", Instant.now().toString());
```

#### gRPC客户端示例 (`java/src/main/java/com/miniodb/examples/GrpcClientExample.java`)

使用gRPC协议的Java客户端，包含表管理功能：

```java
// 创建表
TableConfig config = TableConfig.newBuilder()
        .setBufferSize(1000)
        .setFlushIntervalSeconds(30)
        .setRetentionDays(365)
        .setBackupEnabled(true)
        .putProperties("description", "用户数据表")
        .build();

CreateTableRequest request = CreateTableRequest.newBuilder()
        .setTableName("users")
        .setConfig(config)
        .setIfNotExists(true)
        .build();

// 写入数据到表
WriteRequest writeRequest = WriteRequest.newBuilder()
        .setTable("users")  // 指定表名
        .setId("user123")
        .setTimestamp(Timestamp.newBuilder().setSeconds(Instant.now().getEpochSecond()).build())
        .setPayload(payloadBuilder.build())
        .build();
```

### Node.js示例

#### REST客户端示例 (`node/rest-client-example.js`)

Node.js版本的REST API客户端：

```javascript
// 创建表
const tableData = {
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
};

// 写入数据到表
const writeData = {
    table: 'users',  // 指定表名
    id: 'user123',
    timestamp: new Date().toISOString(),
    payload: {
        user_id: 'user123',
        action: 'login',
        score: 95.5,
        success: true
    }
};

// 跨表查询
const crossSql = `
    SELECT u.user_id, u.action, o.order_id, o.amount
    FROM users u 
    JOIN orders o ON u.user_id = o.user_id 
    WHERE u.user_id = 'user123'
`;
```

### 专用示例

#### 认证示例 (`auth_example.go`)

演示MinIODB的认证机制，包括表管理端点的认证：

```go
// 需要认证的表管理端点
fmt.Printf("  - POST /v1/tables (创建表)\n")
fmt.Printf("  - GET /v1/tables (列出表)\n")
fmt.Printf("  - GET /v1/tables/{table_name} (描述表)\n")
fmt.Printf("  - DELETE /v1/tables/{table_name} (删除表)\n")
```

#### 缓冲区查询示例 (`buffer_query_example.go`)

演示缓冲区的表级查询功能：

```go
// 写入数据到指定表
dataRow := buffer.DataRow{
    Table:     "users", // 指定表名
    ID:        "user-001",
    Timestamp: time.Now().UnixNano(),
    Payload:   payloadJson,
}

// 表级查询
sql := "SELECT * FROM users WHERE id='user-001'"
```

## 📊 示例数据流程

### 完整的表管理流程

1. **创建表** - 定义表名、配置和属性
2. **写入数据** - 向指定表写入数据
3. **查询数据** - 在表中查询数据
4. **跨表查询** - 执行多表JOIN查询
5. **表管理** - 列出、描述、删除表

### 示例输出

```
=== 创建表 ===
创建表结果:
  成功: true
  消息: Table 'users' created successfully

=== 数据写入 ===
数据写入结果:
  表名: users
  成功: true
  消息: Data written successfully
  节点ID: node-1

=== 数据查询 ===
查询结果:
  SQL: SELECT COUNT(*) FROM users WHERE user_id = 'user123'
  结果JSON: [{"total":1}]

=== 跨表查询 ===
跨表查询结果:
  SQL: SELECT u.user_id, o.order_id FROM users u JOIN orders o ON u.user_id = o.user_id
  结果JSON: [{"user_id":"user123","order_id":"order456"}]
```

## 🛠️ 开发指南

### 添加新的客户端示例

1. 创建新的目录或文件
2. 实现表管理功能：
   - CreateTable - 创建表
   - ListTables - 列出表
   - DescribeTable - 描述表
   - DropTable - 删除表
3. 更新数据写入，添加table字段
4. 更新查询示例，使用具体表名
5. 添加跨表查询示例
6. 更新README文档

### 测试示例

```bash
# 测试Go示例
cd go && go test

# 测试Java示例
cd java && mvn test

# 测试Node.js示例
cd node && npm test
```

## 🔍 故障排除

### 常见问题

1. **连接失败**
   - 检查MinIODB服务是否启动
   - 验证端口配置（gRPC: 8080, REST: 8081）

2. **认证失败**
   - 检查JWT令牌是否有效
   - 确认认证配置正确

3. **表不存在错误**
   - 确保先创建表再写入数据
   - 检查表名拼写是否正确

4. **查询失败**
   - 验证SQL语法正确
   - 确认表名存在
   - 检查字段名称

### 调试技巧

- 启用详细日志输出
- 使用健康检查端点验证服务状态
- 检查表列表确认表是否存在
- 使用简单查询测试连接

## 📚 更多资源

- [MinIODB API文档](../api/README.md)
- [配置指南](../docs/configuration.md)
- [部署指南](../deploy/README.md)
- [故障排除](../docs/troubleshooting.md)

## 🤝 贡献

欢迎提交新的客户端示例或改进现有示例！请确保：

1. 包含完整的表管理功能
2. 添加适当的错误处理
3. 提供清晰的注释和文档
4. 遵循项目的代码规范 