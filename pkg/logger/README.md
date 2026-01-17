# Logger Package

MinIODB 统一日志系统 - 基于 Uber Zap 的高性能结构化日志

## 📖 概述

`pkg/logger` 是 MinIODB 的核心日志包，提供：
- 🚀 **高性能** - 基于 Zap，比标准 log 快 4-10 倍
- 📊 **结构化日志** - JSON 格式，易于解析和分析
- 🔍 **上下文追踪** - 自动注入 trace_id, request_id 等
- 📁 **日志轮转** - 基于 Lumberjack 的自动归档
- 🎯 **灵活配置** - 支持多种输出格式和级别
- 🔌 **业务集成** - 专为 MinIODB 业务定制的日志函数

---

## 🏗️ 架构设计

### 位置说明

```
minIODB/
├── pkg/
│   ├── logger/         # ✅ 统一日志系统（独立包）
│   │   ├── logger.go   # 核心实现
│   │   └── README.md   # 本文档
│   ├── retry/          # ✅ 使用 logger
│   ├── pool/           # ✅ 使用 logger
│   └── errors/         # ✅ 使用 logger
└── internal/
    ├── service/        # ✅ 使用 logger
    ├── storage/        # ✅ 使用 logger
    └── transport/      # ✅ 使用 logger
```

**为什么在 pkg/?**
1. ✅ **独立性** - 日志系统与业务逻辑解耦
2. ✅ **可重用** - 可被其他项目使用
3. ✅ **统一依赖** - pkg/ 和 internal/ 都可以使用
4. ✅ **最佳实践** - 符合 Go 项目标准结构

---

## 🚀 快速开始

### 基础使用

```go
import "minIODB/pkg/logger"

// 初始化（通常在 main 函数中）
logger.InitLogger(config.LogConfig{
    Level:      "info",
    Format:     "json",
    Output:     "both",
    Filename:   "logs/miniodb.log",
    MaxSize:    100,
    MaxBackups: 10,
    MaxAge:     30,
    Compress:   true,
})

// 基础日志
logger.LogInfo(ctx, "Server started",
    zap.Int("port", 8080))

logger.LogError(ctx, err, "Failed to connect",
    zap.String("host", "localhost"))
```

### 上下文日志

```go
// 设置追踪 ID
ctx = logger.SetTraceID(ctx, "trace-123")
ctx = logger.SetUserID(ctx, "user-456")
ctx = logger.SetOperation(ctx, "create_order")

// 所有日志自动包含上下文
logger.LogInfo(ctx, "Order created",
    zap.String("order_id", orderID))

// 输出:
// {
//   "level": "info",
//   "timestamp": "2026-01-17T23:30:00Z",
//   "msg": "Order created",
//   "trace_id": "trace-123",
//   "user_id": "user-456",
//   "operation": "create_order",
//   "order_id": "order-789"
// }
```

---

## 📚 API 参考

### 初始化

#### InitLogger

初始化全局 logger 实例。

```go
func InitLogger(cfg LogConfig) error

type LogConfig struct {
    Level      string // debug, info, warn, error, panic, fatal
    Format     string // json, console
    Output     string // stdout, file, both
    Filename   string // 日志文件路径
    MaxSize    int    // 单个文件最大大小（MB）
    MaxBackups int    // 保留的旧文件数量
    MaxAge     int    // 保留的天数
    Compress   bool   // 是否压缩
}
```

**示例:**
```go
logger.InitLogger(logger.LogConfig{
    Level:      "info",
    Format:     "json",
    Output:     "both",
    Filename:   "logs/miniodb.log",
    MaxSize:    100,
    MaxBackups: 10,
    MaxAge:     30,
    Compress:   true,
})
```

---

### 核心日志函数

#### LogInfo
记录信息级别日志。

```go
func LogInfo(ctx context.Context, msg string, fields ...zap.Field)
```

**示例:**
```go
logger.LogInfo(ctx, "Request processed",
    zap.String("method", "GET"),
    zap.Int("status", 200),
    zap.Duration("duration", time.Since(start)))
```

#### LogError
记录错误级别日志。

```go
func LogError(ctx context.Context, err error, msg string, fields ...zap.Field)
```

**示例:**
```go
logger.LogError(ctx, err, "Database query failed",
    zap.String("query", sql),
    zap.String("table", "users"))
```

#### LogWarn
记录警告级别日志。

```go
func LogWarn(ctx context.Context, msg string, fields ...zap.Field)
```

#### LogDebug
记录调试级别日志。

```go
func LogDebug(ctx context.Context, msg string, fields ...zap.Field)
```

#### LogPanic
记录 panic 级别日志并触发 panic。

```go
func LogPanic(ctx context.Context, msg string, fields ...zap.Field)
```

#### LogFatal
记录致命错误并退出程序。

```go
func LogFatal(ctx context.Context, msg string, fields ...zap.Field)
```

---

### 业务日志函数

#### LogOperation
记录业务操作日志（带性能监控）。

```go
func LogOperation(ctx context.Context, operation string, duration time.Duration, err error, fields ...zap.Field)
```

**示例:**
```go
start := time.Now()
result, err := service.CreateOrder(ctx, order)
logger.LogOperation(ctx, "create_order", time.Since(start), err,
    zap.String("order_id", order.ID),
    zap.Int("items", len(order.Items)))
```

#### LogHTTPRequest
记录 HTTP 请求日志。

```go
func LogHTTPRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration, fields ...zap.Field)
```

**示例:**
```go
logger.LogHTTPRequest(ctx, "POST", "/api/v1/orders", 201, duration,
    zap.String("user_agent", req.UserAgent()))
```

#### LogGRPCRequest
记录 gRPC 请求日志。

```go
func LogGRPCRequest(ctx context.Context, method string, duration time.Duration, err error, fields ...zap.Field)
```

#### LogQuery
记录数据库查询日志。

```go
func LogQuery(ctx context.Context, query string, duration time.Duration, err error, fields ...zap.Field)
```

**示例:**
```go
start := time.Now()
rows, err := db.Query(ctx, sql)
logger.LogQuery(ctx, sql, time.Since(start), err,
    zap.String("table", "users"),
    zap.Int("rows", rows))
```

#### LogDataWrite
记录数据写入日志。

```go
func LogDataWrite(ctx context.Context, table, id string, size int64, duration time.Duration, err error)
```

#### LogBufferFlush
记录缓冲区刷新日志。

```go
func LogBufferFlush(ctx context.Context, table string, recordCount, byteSize int64, duration time.Duration, err error)
```

---

### 上下文管理

#### SetTraceID
设置追踪 ID。

```go
func SetTraceID(ctx context.Context, traceID string) context.Context
```

#### GetTraceID
获取追踪 ID。

```go
func GetTraceID(ctx context.Context) string
```

#### SetRequestID
设置请求 ID。

```go
func SetRequestID(ctx context.Context, requestID string) context.Context
```

#### SetUserID
设置用户 ID。

```go
func SetUserID(ctx context.Context, userID string) context.Context
```

#### SetOperation
设置操作名称。

```go
func SetOperation(ctx context.Context, operation string) context.Context
```

#### WithContext
从 context 中提取所有字段并返回带字段的 logger。

```go
func WithContext(ctx context.Context) *zap.Logger
```

**示例:**
```go
// 复杂场景：手动使用带上下文的 logger
contextLogger := logger.WithContext(ctx)
contextLogger.Info("Custom log",
    zap.String("custom_field", "value"))
```

---

### 获取 Logger

#### GetLogger
获取全局 zap.Logger 实例。

```go
func GetLogger() *zap.Logger
```

**用途:**
- 需要直接使用 Zap API
- 需要 Sugar 版本的格式化日志

**示例:**
```go
// 直接使用 Zap
logger.GetLogger().Info("message",
    zap.String("key", "value"))

// 使用 Sugar（格式化）
logger.GetLogger().Sugar().Infof("User %s logged in", username)
```

---

## 🎯 使用场景

### 1. HTTP 请求日志

```go
func HandleRequest(c *gin.Context) {
    start := time.Now()
    
    // 设置上下文
    ctx := logger.SetTraceID(c.Request.Context(), generateTraceID())
    ctx = logger.SetUserID(ctx, getUserID(c))
    
    // 处理请求
    result, err := processRequest(ctx, c)
    
    // 记录日志
    logger.LogHTTPRequest(ctx,
        c.Request.Method,
        c.Request.URL.Path,
        c.Writer.Status(),
        time.Since(start),
        zap.String("user_agent", c.Request.UserAgent()),
        zap.Error(err))
}
```

### 2. 数据库操作日志

```go
func (s *Service) CreateUser(ctx context.Context, user *User) error {
    start := time.Now()
    
    query := "INSERT INTO users (id, name, email) VALUES (?, ?, ?)"
    result, err := s.db.ExecContext(ctx, query, user.ID, user.Name, user.Email)
    
    logger.LogQuery(ctx, query, time.Since(start), err,
        zap.String("table", "users"),
        zap.String("user_id", user.ID))
    
    return err
}
```

### 3. 业务操作日志

```go
func (s *Service) ProcessOrder(ctx context.Context, orderID string) error {
    start := time.Now()
    ctx = logger.SetOperation(ctx, "process_order")
    
    // 业务逻辑
    order, err := s.getOrder(ctx, orderID)
    if err != nil {
        return err
    }
    
    err = s.validateOrder(ctx, order)
    if err != nil {
        return err
    }
    
    err = s.chargePayment(ctx, order)
    
    // 记录操作日志
    logger.LogOperation(ctx, "process_order", time.Since(start), err,
        zap.String("order_id", orderID),
        zap.Float64("amount", order.Amount))
    
    return err
}
```

### 4. 错误处理

```go
func (s *Service) DoSomething(ctx context.Context) error {
    result, err := s.externalService.Call(ctx)
    if err != nil {
        logger.LogError(ctx, err, "External service call failed",
            zap.String("service", "external_api"),
            zap.String("endpoint", "/api/v1/data"))
        return fmt.Errorf("service call failed: %w", err)
    }
    
    return nil
}
```

### 5. 分布式追踪

```go
func HandleDistributedRequest(ctx context.Context) {
    // 从 HTTP Header 获取 trace ID
    traceID := extractTraceID(req)
    ctx = logger.SetTraceID(ctx, traceID)
    
    // 调用服务 A
    logger.LogInfo(ctx, "Calling service A")
    resultA, err := serviceA.Call(ctx)
    
    // 调用服务 B
    logger.LogInfo(ctx, "Calling service B")
    resultB, err := serviceB.Call(ctx)
    
    // 所有日志都包含相同的 trace_id，便于追踪
}
```

### 6. pkg 包中使用

```go
// pkg/retry/retry.go
package retry

import (
    "minIODB/pkg/logger"
    "go.uber.org/zap"
)

func Do(ctx context.Context, fn func() error) error {
    err := fn()
    if err != nil {
        logger.LogWarn(ctx, "Retry attempt failed",
            zap.Int("attempt", attempt),
            zap.Error(err))
    }
    return err
}
```

---

## ⚙️ 配置

### 配置文件示例 (config.yaml)

```yaml
log:
  # 日志级别: debug, info, warn, error, panic, fatal
  level: info
  
  # 日志格式: json（生产）, console（开发）
  format: json
  
  # 输出位置: stdout, file, both
  output: both
  
  # 文件配置
  filename: logs/miniodb.log
  max_size: 100        # MB
  max_backups: 10      # 保留文件数
  max_age: 30          # 保留天数
  compress: true       # 是否压缩
```

### 环境配置

**开发环境:**
```yaml
log:
  level: debug
  format: console
  output: stdout
```

**生产环境:**
```yaml
log:
  level: info
  format: json
  output: both
  compress: true
```

**测试环境:**
```yaml
log:
  level: warn
  format: json
  output: file
```

---

## 📊 日志格式

### JSON 格式（生产环境）

```json
{
  "level": "info",
  "timestamp": "2026-01-17T23:30:00.123Z",
  "caller": "service/handler.go:42",
  "msg": "Request completed",
  "trace_id": "trace-abc123",
  "request_id": "req-xyz789",
  "user_id": "user-456",
  "operation": "create_order",
  "method": "POST",
  "path": "/api/v1/orders",
  "status_code": 201,
  "duration": "45ms"
}
```

### Console 格式（开发环境）

```
2026-01-17T23:30:00.123Z  INFO  service/handler.go:42  Request completed
    trace_id=trace-abc123
    request_id=req-xyz789
    user_id=user-456
    operation=create_order
    method=POST
    path=/api/v1/orders
    status_code=201
    duration=45ms
```

---

## 🔧 高级特性

### 1. 性能采样

高频日志自动采样，避免日志过多影响性能。

```go
// 内部已实现采样逻辑
// 生产环境下，debug 日志会被采样
logger.LogDebug(ctx, "Cache hit", zap.String("key", key))
```

### 2. 日志轮转

基于 Lumberjack 的自动日志轮转：
- ✅ 按大小轮转（默认 100MB）
- ✅ 按时间保留（默认 30 天）
- ✅ 自动压缩旧日志
- ✅ 保留指定数量的备份

### 3. 动态日志级别

```go
// 运行时调整日志级别（通过信号或 API）
logger.SetLevel("debug")  // 开启 debug 日志
logger.SetLevel("info")   // 恢复到 info
```

### 4. 上下文自动传递

所有日志函数都接受 `context.Context`，自动提取：
- `trace_id` - 分布式追踪 ID
- `request_id` - 请求 ID
- `user_id` - 用户 ID
- `operation` - 操作名称

---

## 📈 性能

### 基准测试

```
BenchmarkZapLogger        2000000    800 ns/op    0 allocs/op
BenchmarkStandardLog       500000   3200 ns/op    2 allocs/op
```

**性能优势:**
- ✅ **4-10x 更快** - 比标准 log 包快
- ✅ **零分配** - 大多数场景下零内存分配
- ✅ **高并发** - 优秀的并发性能

### 最佳实践

1. **使用结构化字段**
   ```go
   // ✅ 好
   logger.LogInfo(ctx, "User logged in",
       zap.String("user_id", userID),
       zap.String("ip", clientIP))
   
   // ❌ 差
   logger.GetLogger().Sugar().Infof("User %s logged in from %s", userID, clientIP)
   ```

2. **避免在循环中记录高频日志**
   ```go
   // ❌ 差：可能产生大量日志
   for _, item := range items {
       logger.LogDebug(ctx, "Processing item", zap.String("id", item.ID))
   }
   
   // ✅ 好：批量记录
   logger.LogInfo(ctx, "Processing items", zap.Int("count", len(items)))
   ```

3. **使用适当的日志级别**
   - `Debug`: 详细的开发信息
   - `Info`: 重要的业务事件
   - `Warn`: 警告但不影响功能
   - `Error`: 错误需要关注
   - `Fatal`: 致命错误，程序退出

---

## 🐛 故障排查

### 问题: 日志文件不生成

**检查:**
1. 文件路径是否正确
2. 是否有写入权限
3. `output` 配置是否正确

```bash
# 检查日志目录
ls -la logs/

# 检查权限
chmod 755 logs/
```

### 问题: 日志级别不生效

**原因:** 日志级别配置错误或未重启服务。

**解决:**
```go
// 确保正确初始化
logger.InitLogger(logger.LogConfig{
    Level: "debug", // 检查此配置
})
```

### 问题: 性能问题

**检查:**
1. 是否使用了 Sugar API（性能较低）
2. 日志级别是否过低（debug 会产生大量日志）
3. 是否在循环中记录日志

---

## 🔗 集成示例

### 与 Gin 集成

```go
func LoggerMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        
        // 设置上下文
        ctx := c.Request.Context()
        ctx = logger.SetTraceID(ctx, c.GetHeader("X-Trace-ID"))
        ctx = logger.SetRequestID(ctx, generateRequestID())
        c.Request = c.Request.WithContext(ctx)
        
        // 处理请求
        c.Next()
        
        // 记录日志
        logger.LogHTTPRequest(ctx,
            c.Request.Method,
            c.Request.URL.Path,
            c.Writer.Status(),
            time.Since(start))
    }
}
```

### 与 gRPC 集成

```go
func LoggerInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        start := time.Now()
        
        // 设置上下文
        ctx = logger.SetOperation(ctx, info.FullMethod)
        
        // 处理请求
        resp, err := handler(ctx, req)
        
        // 记录日志
        logger.LogGRPCRequest(ctx, info.FullMethod, time.Since(start), err)
        
        return resp, err
    }
}
```

---

## 📦 依赖

- [uber-go/zap](https://github.com/uber-go/zap) - 高性能日志库
- [natefinch/lumberjack](https://github.com/natefinch/lumberjack) - 日志轮转

---

## 📝 总结

`pkg/logger` 提供了一个**统一、高性能、功能完善**的日志系统：

✅ **统一接口** - 全项目使用同一日志 API  
✅ **高性能** - 基于 Zap，4-10x 性能提升  
✅ **结构化** - JSON 格式，易于分析  
✅ **上下文追踪** - 分布式追踪支持  
✅ **日志轮转** - 自动归档和清理  
✅ **业务集成** - 专为 MinIODB 定制  
✅ **生产就绪** - 经过充分测试和验证  

---

**更新时间**: 2026-01-17  
**版本**: 2.0  
**维护者**: MinIODB Team
