# pkg/retry

重试和熔断器包，提供可靠的错误处理和容错机制。

## 特性

- ✅ **指数退避重试** - 支持可配置的重试策略
- ✅ **熔断器模式** - 防止级联故障
- ✅ **上下文感知** - 支持取消和超时
- ✅ **可定制** - 灵活的配置选项
- ✅ **监控友好** - 提供指标回调接口

## 快速开始

### 基本重试

```go
import (
    "context"
    "minIODB/pkg/retry"
)

// 使用默认配置重试
err := retry.Do(
    context.Background(),
    retry.DefaultConfig,
    "my_operation",
    func(ctx context.Context) error {
        // 你的业务逻辑
        result, err := someOperation()
        if err != nil {
            // 返回可重试错误
            return retry.NewRetryableError(err)
        }
        return nil
    },
)
```

### 自定义重试配置

```go
config := retry.Config{
    MaxAttempts:     5,                      // 最多重试5次
    InitialInterval: 200 * time.Millisecond, // 初始间隔200ms
    MaxInterval:     10 * time.Second,       // 最大间隔10s
    Multiplier:      2.0,                    // 指数退避倍数
    MaxJitter:       100 * time.Millisecond, // 最大抖动100ms
}

err := retry.Do(ctx, config, "custom_operation", func(ctx context.Context) error {
    // 你的逻辑
    return nil
})
```

### 使用熔断器

```go
// 创建熔断器
cb := retry.NewCircuitBreaker("api_service", retry.DefaultCircuitBreakerConfig)

// 通过熔断器执行操作
err := cb.Execute(ctx, func(ctx context.Context) error {
    // 调用外部服务
    return externalService.Call()
})

// 检查熔断器状态
state := cb.GetState()
stats := cb.GetStats()
```

### 自定义熔断器配置

```go
config := retry.CircuitBreakerConfig{
    FailureThreshold:       10,               // 失败10次触发熔断
    RecoveryTimeout:        60 * time.Second, // 60秒后尝试恢复
    SuccessThreshold:       3,                // 半开状态下3次成功即恢复
    RequestVolumeThreshold: 20,               // 最小请求量阈值
    SlidingWindowSize:      120 * time.Second,// 滑动窗口2分钟
}

cb := retry.NewCircuitBreaker("my_service", config)
```

### 带监控指标的重试

```go
err := retry.DoWithMetrics(
    ctx,
    config,
    "monitored_operation",
    func(ctx context.Context) error {
        // 业务逻辑
        return nil
    },
    func(attempt int, err error, duration time.Duration) {
        // 记录指标到你的监控系统
        metrics.RecordRetryAttempt(attempt, err, duration)
    },
)
```

## API 参考

### 重试函数

#### `Do(ctx, config, operation, fn) error`

执行带重试的操作。

**参数:**
- `ctx context.Context` - 上下文，支持取消和超时
- `config Config` - 重试配置
- `operation string` - 操作名称（用于日志）
- `fn Func` - 要执行的函数

**返回:**
- `error` - 最终错误（如果所有重试都失败）

#### `DoWithMetrics(ctx, config, operation, fn, metrics) error`

执行带监控指标的重试操作。

**参数:**
- 与 `Do` 相同，加上:
- `metrics MetricsFunc` - 指标回调函数

### 错误类型

#### `NewRetryableError(err) *RetryableError`

创建可重试错误。只有可重试错误才会触发重试逻辑。

#### `IsRetryable(err) bool`

检查错误是否可重试。

### 熔断器

#### `NewCircuitBreaker(name, config) *CircuitBreaker`

创建新的熔断器实例。

#### `Execute(ctx, fn) error`

通过熔断器执行操作。

#### `GetState() CircuitBreakerState`

获取当前熔断器状态（Closed/Open/HalfOpen）。

#### `GetStats() Stats`

获取熔断器统计信息。

#### `Reset()`

重置熔断器到初始状态。

## 配置说明

### 重试配置 (Config)

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| MaxAttempts | int | 3 | 最大重试次数 |
| InitialInterval | time.Duration | 100ms | 初始重试间隔 |
| MaxInterval | time.Duration | 5s | 最大重试间隔 |
| Multiplier | float64 | 2.0 | 退避倍数 |
| MaxJitter | time.Duration | 100ms | 最大抖动时间 |

### 熔断器配置 (CircuitBreakerConfig)

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| FailureThreshold | int | 5 | 触发熔断的失败次数 |
| RecoveryTimeout | time.Duration | 30s | 熔断后恢复超时 |
| SuccessThreshold | int | 3 | 半开状态需要的成功次数 |
| RequestVolumeThreshold | int | 10 | 最小请求量阈值 |
| SlidingWindowSize | time.Duration | 60s | 滑动窗口大小 |

## 熔断器状态机

```
        失败次数 >= 阈值
Closed ──────────────────> Open
  ↑                          |
  |                          | 恢复超时
  |                          ↓
  └────────────── HalfOpen <┘
      成功次数 >= 阈值
```

## 最佳实践

### 1. 区分可重试和不可重试错误

```go
func callExternalAPI() error {
    resp, err := http.Get("https://api.example.com")
    if err != nil {
        // 网络错误通常可重试
        return retry.NewRetryableError(err)
    }
    
    if resp.StatusCode >= 500 {
        // 服务器错误可重试
        return retry.NewRetryableError(fmt.Errorf("server error: %d", resp.StatusCode))
    }
    
    if resp.StatusCode >= 400 {
        // 客户端错误不应重试
        return fmt.Errorf("client error: %d", resp.StatusCode)
    }
    
    return nil
}
```

### 2. 使用上下文控制超时

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := retry.Do(ctx, config, "operation", fn)
```

### 3. 合理配置重试参数

- **快速失败场景**: MaxAttempts=2-3, InitialInterval=50ms
- **容错场景**: MaxAttempts=5-10, InitialInterval=200ms
- **关键操作**: 使用熔断器 + 重试

### 4. 监控和告警

```go
err := retry.DoWithMetrics(ctx, config, "critical_operation", fn,
    func(attempt int, err error, duration time.Duration) {
        if attempt > 1 {
            // 重试发生，记录告警
            alerting.Warn("Operation retried", attempt, err)
        }
        
        metrics.RecordLatency("operation_latency", duration)
    },
)
```

### 5. 组合使用重试和熔断器

```go
cb := retry.NewCircuitBreaker("service", retry.DefaultCircuitBreakerConfig)

err := cb.Execute(ctx, func(ctx context.Context) error {
    return retry.Do(ctx, retry.DefaultConfig, "inner_operation",
        func(ctx context.Context) error {
            // 实际操作
            return externalCall()
        },
    )
})
```

## 性能考虑

- 重试会增加延迟，合理设置最大重试次数
- 熔断器使用轻量级互斥锁，开销很小
- 在高并发场景下，考虑使用抖动避免惊群效应
- 监控指标回调应该是异步的，避免阻塞主流程

## 相关资源

- [微软云设计模式 - 重试模式](https://docs.microsoft.com/en-us/azure/architecture/patterns/retry)
- [Martin Fowler - 熔断器模式](https://martinfowler.com/bliki/CircuitBreaker.html)
- [AWS - 指数退避和抖动](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
