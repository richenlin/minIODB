# MinIODB Java SDK

MinIODB Java SDK 是用于与 MinIODB 服务交互的官方 Java 客户端库。

## 特性

- 🚀 **高性能**: 基于 gRPC 的高性能通信
- 🔄 **连接池**: 自动管理 gRPC 连接池
- 🛡️ **错误处理**: 完善的错误处理和重试机制
- 📊 **流式操作**: 支持大数据量的流式读写
- 🔐 **认证支持**: 支持 API 密钥认证
- 📝 **完整文档**: 完整的 JavaDoc 文档

## 快速开始

### 添加依赖

#### Maven
```xml
<dependency>
    <groupId>com.miniodb</groupId>
    <artifactId>miniodb-java-sdk</artifactId>
    <version>1.0.0</version>
</dependency>
```

#### Gradle
```gradle
implementation 'com.miniodb:miniodb-java-sdk:1.0.0'
```

### 基本用法

```java
import com.miniodb.client.MinIODBClient;
import com.miniodb.client.config.MinIODBConfig;
import com.miniodb.model.*;

// 创建配置
MinIODBConfig config = MinIODBConfig.builder()
    .host("localhost")
    .grpcPort(8080)
    .build();

// 创建客户端
try (MinIODBClient client = new MinIODBClient(config)) {
    
    // 写入数据
    DataRecord record = DataRecord.builder()
        .id("user-123")
        .timestamp(Instant.now())
        .payload(Map.of(
            "name", "John Doe",
            "age", 30,
            "email", "john@example.com"
        ))
        .build();
    
    WriteDataResponse writeResponse = client.writeData("users", record);
    System.out.println("写入成功: " + writeResponse.isSuccess());
    
    // 查询数据
    QueryDataResponse queryResponse = client.queryData(
        "SELECT * FROM users WHERE age > 25", 
        10, 
        null
    );
    
    System.out.println("查询结果: " + queryResponse.getResultJson());
    
    // 创建表
    TableConfig tableConfig = TableConfig.builder()
        .bufferSize(1000)
        .flushIntervalSeconds(30)
        .retentionDays(365)
        .backupEnabled(true)
        .build();
    
    CreateTableResponse createResponse = client.createTable("products", tableConfig, true);
    System.out.println("表创建成功: " + createResponse.isSuccess());
}
```

## 核心功能

### 数据操作

#### 写入数据
```java
// 单条记录写入
DataRecord record = DataRecord.builder()
    .id("record-id")
    .timestamp(Instant.now())
    .payload(dataMap)
    .build();

WriteDataResponse response = client.writeData("table_name", record);
```

#### 批量写入
```java
List<DataRecord> records = Arrays.asList(record1, record2, record3);
StreamWriteResponse response = client.streamWrite("table_name", records);
```

#### 查询数据
```java
// 基本查询
QueryDataResponse response = client.queryData("SELECT * FROM users", 100, null);

// 分页查询
String cursor = null;
do {
    QueryDataResponse page = client.queryData("SELECT * FROM users", 50, cursor);
    // 处理结果
    cursor = page.getNextCursor();
} while (page.isHasMore());
```

#### 流式查询
```java
Iterator<StreamQueryResponse> iterator = client.streamQuery(
    "SELECT * FROM large_table", 
    1000  // 批次大小
);

while (iterator.hasNext()) {
    StreamQueryResponse batch = iterator.next();
    // 处理批次数据
}
```

#### 更新数据
```java
UpdateDataResponse response = client.updateData(
    "users", 
    "user-123", 
    Map.of("age", 31, "status", "active"),
    Instant.now()
);
```

#### 删除数据
```java
// 软删除
DeleteDataResponse response = client.deleteData("users", "user-123", true);

// 硬删除
DeleteDataResponse response = client.deleteData("users", "user-123", false);
```

### 表管理

#### 创建表
```java
TableConfig config = TableConfig.builder()
    .bufferSize(2000)
    .flushIntervalSeconds(60)
    .retentionDays(730)
    .backupEnabled(true)
    .properties(Map.of("description", "用户数据表"))
    .build();

CreateTableResponse response = client.createTable("users", config, true);
```

#### 列出表
```java
ListTablesResponse response = client.listTables("user*");
for (TableInfo table : response.getTablesList()) {
    System.out.println("表名: " + table.getName());
    System.out.println("记录数: " + table.getStats().getRecordCount());
}
```

#### 获取表信息
```java
GetTableResponse response = client.getTable("users");
TableInfo info = response.getTableInfo();
System.out.println("表状态: " + info.getStatus());
System.out.println("记录数: " + info.getStats().getRecordCount());
```

#### 删除表
```java
DeleteTableResponse response = client.deleteTable("old_table", true, true);
```

### 元数据管理

#### 备份元数据
```java
BackupMetadataResponse response = client.backupMetadata(true);
System.out.println("备份ID: " + response.getBackupId());
```

#### 恢复元数据
```java
RestoreMetadataResponse response = client.restoreMetadata(
    "backup_20240115_103000.json",
    false,  // from_latest
    false,  // dry_run
    true,   // overwrite
    true,   // validate
    true,   // parallel
    Map.of("table_pattern", "users*"),
    Arrays.asList("table:*", "index:*")
);
```

#### 列出备份
```java
ListBackupsResponse response = client.listBackups(7);
for (BackupInfo backup : response.getBackupsList()) {
    System.out.println("备份: " + backup.getObjectName());
    System.out.println("时间: " + backup.getTimestamp());
}
```

### 监控和健康检查

#### 健康检查
```java
HealthCheckResponse response = client.healthCheck();
System.out.println("服务状态: " + response.getStatus());
System.out.println("版本: " + response.getVersion());
```

#### 获取系统状态
```java
GetStatusResponse response = client.getStatus();
System.out.println("总节点数: " + response.getTotalNodes());
System.out.println("缓冲区统计: " + response.getBufferStatsMap());
```

#### 获取性能指标
```java
GetMetricsResponse response = client.getMetrics();
System.out.println("性能指标: " + response.getPerformanceMetricsMap());
System.out.println("资源使用: " + response.getResourceUsageMap());
```

## 配置选项

### 基本配置
```java
MinIODBConfig config = MinIODBConfig.builder()
    .host("localhost")              // 服务器地址
    .grpcPort(8080)                // gRPC 端口
    .restPort(8081)                // REST 端口（可选）
    .build();
```

### 认证配置
```java
MinIODBConfig config = MinIODBConfig.builder()
    .host("localhost")
    .grpcPort(8080)
    .auth(AuthConfig.builder()
        .apiKey("your-api-key")
        .secret("your-secret")
        .build())
    .build();
```

### 连接池配置
```java
MinIODBConfig config = MinIODBConfig.builder()
    .host("localhost")
    .grpcPort(8080)
    .connection(ConnectionConfig.builder()
        .maxConnections(10)
        .timeout(Duration.ofSeconds(30))
        .retryAttempts(3)
        .keepAliveTime(Duration.ofMinutes(5))
        .build())
    .build();
```

### 完整配置示例
```java
MinIODBConfig config = MinIODBConfig.builder()
    .host("miniodb-server")
    .grpcPort(8080)
    .restPort(8081)
    .auth(AuthConfig.builder()
        .apiKey("your-api-key")
        .secret("your-secret")
        .build())
    .connection(ConnectionConfig.builder()
        .maxConnections(20)
        .timeout(Duration.ofSeconds(60))
        .retryAttempts(5)
        .keepAliveTime(Duration.ofMinutes(10))
        .build())
    .logging(LoggingConfig.builder()
        .level("INFO")
        .format("JSON")
        .build())
    .build();
```

## 错误处理

SDK 提供了完善的错误处理机制：

```java
try {
    WriteDataResponse response = client.writeData("users", record);
    if (!response.isSuccess()) {
        System.err.println("写入失败: " + response.getMessage());
    }
} catch (MinIODBConnectionException e) {
    System.err.println("连接错误: " + e.getMessage());
} catch (MinIODBAuthenticationException e) {
    System.err.println("认证失败: " + e.getMessage());
} catch (MinIODBRequestException e) {
    System.err.println("请求错误: " + e.getMessage());
} catch (MinIODBServerException e) {
    System.err.println("服务器错误: " + e.getMessage());
} catch (MinIODBTimeoutException e) {
    System.err.println("请求超时: " + e.getMessage());
}
```

## 最佳实践

### 1. 使用连接池
```java
// 推荐：使用连接池配置
MinIODBConfig config = MinIODBConfig.builder()
    .host("localhost")
    .grpcPort(8080)
    .connection(ConnectionConfig.builder()
        .maxConnections(10)
        .build())
    .build();
```

### 2. 批量操作
```java
// 推荐：批量写入大量数据
List<DataRecord> records = prepareRecords();
StreamWriteResponse response = client.streamWrite("table", records);

// 避免：逐条写入大量数据
for (DataRecord record : records) {
    client.writeData("table", record);  // 不推荐
}
```

### 3. 异步操作
```java
// 使用 CompletableFuture 进行异步操作
CompletableFuture<WriteDataResponse> future = CompletableFuture.supplyAsync(() -> {
    return client.writeData("users", record);
});

future.thenAccept(response -> {
    System.out.println("写入完成: " + response.isSuccess());
});
```

### 4. 资源管理
```java
// 推荐：使用 try-with-resources
try (MinIODBClient client = new MinIODBClient(config)) {
    // 使用客户端
} // 自动关闭连接

// 或者手动管理
MinIODBClient client = new MinIODBClient(config);
try {
    // 使用客户端
} finally {
    client.close();
}
```

## 构建和测试

### 构建项目
```bash
mvn clean compile
```

### 运行测试
```bash
mvn test
```

### 生成文档
```bash
mvn javadoc:javadoc
```

### 打包
```bash
mvn clean package
```

## 许可证

本项目采用 BSD-3-Clause 许可证。详见 [LICENSE](../LICENSE) 文件。
