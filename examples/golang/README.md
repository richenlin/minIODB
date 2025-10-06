# MinIODB Go SDK

MinIODB Go SDK 是用于与 MinIODB 服务交互的官方 Go 客户端库。

## 特性

- 🚀 **高性能**: 基于 gRPC 的高性能通信
- 🔄 **并发安全**: 完全支持 Go 并发模式
- 🛡️ **错误处理**: 完善的错误处理和重试机制
- 📊 **流式操作**: 支持大数据量的流式读写
- 🔐 **认证支持**: 支持 API 密钥认证
- ⚡ **上下文支持**: 完整的 context.Context 支持
- 🎯 **类型安全**: 强类型的 API 设计

## 安装

```bash
go get github.com/miniodb/go-sdk
```

## 快速开始

### 基本用法

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/miniodb/go-sdk/client"
    "github.com/miniodb/go-sdk/config"
    "github.com/miniodb/go-sdk/models"
)

func main() {
    // 创建配置
    cfg := &config.Config{
        Host:     "localhost",
        GRPCPort: 8080,
    }

    // 创建客户端
    client, err := client.NewClient(cfg)
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }
    defer client.Close()

    ctx := context.Background()

    // 写入数据
    record := &models.DataRecord{
        ID:        "user-123",
        Timestamp: time.Now(),
        Payload: map[string]interface{}{
            "name":  "John Doe",
            "age":   30,
            "email": "john@example.com",
        },
    }

    writeResp, err := client.WriteData(ctx, "users", record)
    if err != nil {
        log.Fatalf("写入数据失败: %v", err)
    }
    fmt.Printf("写入成功: %v\n", writeResp.Success)

    // 查询数据
    queryResp, err := client.QueryData(ctx, "SELECT * FROM users WHERE age > 25", 10, "")
    if err != nil {
        log.Fatalf("查询数据失败: %v", err)
    }
    fmt.Printf("查询结果: %s\n", queryResp.ResultJSON)

    // 创建表
    tableConfig := &models.TableConfig{
        BufferSize:           1000,
        FlushIntervalSeconds: 30,
        RetentionDays:        365,
        BackupEnabled:        true,
    }

    createResp, err := client.CreateTable(ctx, "products", tableConfig, true)
    if err != nil {
        log.Fatalf("创建表失败: %v", err)
    }
    fmt.Printf("表创建成功: %v\n", createResp.Success)
}
```

## 核心功能

### 数据操作

#### 写入数据
```go
record := &models.DataRecord{
    ID:        "record-id",
    Timestamp: time.Now(),
    Payload:   map[string]interface{}{"key": "value"},
}

response, err := client.WriteData(ctx, "table_name", record)
```

#### 批量写入
```go
records := []*models.DataRecord{record1, record2, record3}
response, err := client.StreamWrite(ctx, "table_name", records)
```

#### 查询数据
```go
// 基本查询
response, err := client.QueryData(ctx, "SELECT * FROM users", 100, "")

// 分页查询
cursor := ""
for {
    page, err := client.QueryData(ctx, "SELECT * FROM users", 50, cursor)
    if err != nil {
        break
    }
    // 处理结果
    if !page.HasMore {
        break
    }
    cursor = page.NextCursor
}
```

#### 流式查询
```go
stream, err := client.StreamQuery(ctx, "SELECT * FROM large_table", 1000, "")
if err != nil {
    log.Fatal(err)
}

for {
    batch, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    // 处理批次数据
    for _, record := range batch.Records {
        fmt.Printf("记录: %+v\n", record)
    }
}
```

#### 更新数据
```go
response, err := client.UpdateData(ctx, "users", "user-123", 
    map[string]interface{}{"age": 31, "status": "active"}, 
    time.Now())
```

#### 删除数据
```go
// 软删除
response, err := client.DeleteData(ctx, "users", "user-123", true)

// 硬删除
response, err := client.DeleteData(ctx, "users", "user-123", false)
```

### 表管理

#### 创建表
```go
config := &models.TableConfig{
    BufferSize:           2000,
    FlushIntervalSeconds: 60,
    RetentionDays:        730,
    BackupEnabled:        true,
    Properties: map[string]string{
        "description": "用户数据表",
    },
}

response, err := client.CreateTable(ctx, "users", config, true)
```

#### 列出表
```go
response, err := client.ListTables(ctx, "user*")
for _, table := range response.Tables {
    fmt.Printf("表名: %s, 记录数: %d\n", table.Name, table.Stats.RecordCount)
}
```

#### 获取表信息
```go
response, err := client.GetTable(ctx, "users")
info := response.TableInfo
fmt.Printf("表状态: %s\n", info.Status)
fmt.Printf("记录数: %d\n", info.Stats.RecordCount)
```

#### 删除表
```go
response, err := client.DeleteTable(ctx, "old_table", true, true)
```

### 元数据管理

#### 备份元数据
```go
response, err := client.BackupMetadata(ctx, true)
fmt.Printf("备份ID: %s\n", response.BackupID)
```

#### 恢复元数据
```go
response, err := client.RestoreMetadata(ctx, &models.RestoreMetadataRequest{
    BackupFile: "backup_20240115_103000.json",
    FromLatest: false,
    DryRun:     false,
    Overwrite:  true,
    Validate:   true,
    Parallel:   true,
    Filters: map[string]string{
        "table_pattern": "users*",
    },
    KeyPatterns: []string{"table:*", "index:*"},
})
```

#### 列出备份
```go
response, err := client.ListBackups(ctx, 7)
for _, backup := range response.Backups {
    fmt.Printf("备份: %s, 时间: %v\n", backup.ObjectName, backup.Timestamp)
}
```

### 监控和健康检查

#### 健康检查
```go
response, err := client.HealthCheck(ctx)
fmt.Printf("服务状态: %s\n", response.Status)
fmt.Printf("版本: %s\n", response.Version)
```

#### 获取系统状态
```go
response, err := client.GetStatus(ctx)
fmt.Printf("总节点数: %d\n", response.TotalNodes)
fmt.Printf("缓冲区统计: %+v\n", response.BufferStats)
```

#### 获取性能指标
```go
response, err := client.GetMetrics(ctx)
fmt.Printf("性能指标: %+v\n", response.PerformanceMetrics)
fmt.Printf("资源使用: %+v\n", response.ResourceUsage)
```

## 配置选项

### 基本配置
```go
cfg := &config.Config{
    Host:     "localhost",     // 服务器地址
    GRPCPort: 8080,           // gRPC 端口
    RESTPort: 8081,           // REST 端口（可选）
}
```

### 认证配置
```go
cfg := &config.Config{
    Host:     "localhost",
    GRPCPort: 8080,
    Auth: &config.AuthConfig{
        APIKey: "your-api-key",
        Secret: "your-secret",
    },
}
```

### 连接配置
```go
cfg := &config.Config{
    Host:     "localhost",
    GRPCPort: 8080,
    Connection: &config.ConnectionConfig{
        MaxConnections: 10,
        Timeout:        30 * time.Second,
        RetryAttempts:  3,
        KeepAliveTime:  5 * time.Minute,
    },
}
```

### 完整配置示例
```go
cfg := &config.Config{
    Host:     "miniodb-server",
    GRPCPort: 8080,
    RESTPort: 8081,
    Auth: &config.AuthConfig{
        APIKey: "your-api-key",
        Secret: "your-secret",
    },
    Connection: &config.ConnectionConfig{
        MaxConnections:         20,
        Timeout:               60 * time.Second,
        RetryAttempts:         5,
        KeepAliveTime:         10 * time.Minute,
        MaxReceiveMessageSize: 4 * 1024 * 1024, // 4MB
        MaxSendMessageSize:    4 * 1024 * 1024, // 4MB
    },
    Logging: &config.LoggingConfig{
        Level:                    "INFO",
        Format:                   "JSON",
        EnableRequestLogging:     true,
        EnablePerformanceLogging: true,
    },
}
```

## 错误处理

SDK 提供了完善的错误处理机制：

```go
import "github.com/miniodb/go-sdk/errors"

response, err := client.WriteData(ctx, "users", record)
if err != nil {
    switch {
    case errors.IsConnectionError(err):
        fmt.Printf("连接错误: %v\n", err)
    case errors.IsAuthenticationError(err):
        fmt.Printf("认证失败: %v\n", err)
    case errors.IsRequestError(err):
        fmt.Printf("请求错误: %v\n", err)
    case errors.IsServerError(err):
        fmt.Printf("服务器错误: %v\n", err)
    case errors.IsTimeoutError(err):
        fmt.Printf("请求超时: %v\n", err)
    default:
        fmt.Printf("未知错误: %v\n", err)
    }
    return
}

if !response.Success {
    fmt.Printf("操作失败: %s\n", response.Message)
}
```

## 并发操作

### 并发写入
```go
func concurrentWrites(client *client.Client, records []*models.DataRecord) {
    var wg sync.WaitGroup
    semaphore := make(chan struct{}, 10) // 限制并发数

    for _, record := range records {
        wg.Add(1)
        go func(r *models.DataRecord) {
            defer wg.Done()
            semaphore <- struct{}{} // 获取信号量
            defer func() { <-semaphore }() // 释放信号量

            ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()

            _, err := client.WriteData(ctx, "users", r)
            if err != nil {
                log.Printf("写入失败: %v", err)
            }
        }(record)
    }

    wg.Wait()
}
```

### 上下文控制
```go
// 带超时的操作
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

response, err := client.QueryData(ctx, "SELECT * FROM large_table", 1000, "")

// 可取消的操作
ctx, cancel := context.WithCancel(context.Background())
go func() {
    time.Sleep(5 * time.Second)
    cancel() // 5秒后取消操作
}()

response, err := client.StreamQuery(ctx, "SELECT * FROM huge_table", 1000, "")
```

## 最佳实践

### 1. 连接管理
```go
// 推荐：重用客户端连接
var globalClient *client.Client

func init() {
    cfg := &config.Config{
        Host:     "localhost",
        GRPCPort: 8080,
        Connection: &config.ConnectionConfig{
            MaxConnections: 20,
        },
    }
    
    var err error
    globalClient, err = client.NewClient(cfg)
    if err != nil {
        log.Fatal(err)
    }
}

// 在程序退出时关闭连接
func cleanup() {
    if globalClient != nil {
        globalClient.Close()
    }
}
```

### 2. 批量操作
```go
// 推荐：批量写入大量数据
records := prepareRecords()
response, err := client.StreamWrite(ctx, "table", records)

// 避免：逐条写入大量数据
for _, record := range records {
    client.WriteData(ctx, "table", record) // 不推荐
}
```

### 3. 错误处理和重试
```go
import "github.com/cenkalti/backoff/v4"

func writeWithRetry(client *client.Client, table string, record *models.DataRecord) error {
    operation := func() error {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        
        _, err := client.WriteData(ctx, table, record)
        return err
    }

    return backoff.Retry(operation, backoff.NewExponentialBackOff())
}
```

### 4. 资源管理
```go
// 推荐：使用 defer 确保资源清理
func processData() error {
    client, err := client.NewClient(cfg)
    if err != nil {
        return err
    }
    defer client.Close() // 确保连接被关闭

    // 使用客户端进行操作
    return nil
}
```

## 构建和测试

### 构建项目
```bash
go build ./...
```

### 运行测试
```bash
go test ./...
```

### 运行基准测试
```bash
go test -bench=. ./...
```

### 生成代码覆盖率报告
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 许可证

本项目采用 BSD-3-Clause 许可证。详见 [LICENSE](../LICENSE) 文件。
