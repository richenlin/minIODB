# pkg/pool

连接池管理包，提供Redis和MinIO的高性能连接池实现。

## 特性

- ✅ **Redis连接池** - 支持单机/哨兵/集群三种模式
- ✅ **MinIO连接池** - HTTP连接池优化
- ✅ **统一管理** - PoolManager统一管理多个连接池
- ✅ **健康检查** - 自动检测连接健康状态
- ✅ **高性能** - 优化的连接复用和超时配置
- ✅ **线程安全** - 支持并发访问

## 快速开始

### Redis连接池

```go
import "minIODB/pkg/pool"

// 单机模式
config := &pool.RedisPoolConfig{
    Mode:     pool.RedisModeStandalone,
    Addr:     "localhost:6379",
    Password: "password",
    PoolSize: 100,
}
redisPool, _ := pool.NewRedisPool(config)

// 获取客户端
client := redisPool.GetClient()
client.Set(ctx, "key", "value", 0)
```

### MinIO连接池

```go
// MinIO连接池配置
config := &pool.MinIOPoolConfig{
    Endpoint:        "localhost:9000",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
    UseSSL:          false,
    MaxIdleConnsPerHost: 100,
}
minioPool, _ := pool.NewMinIOPool(config)

// 获取客户端
client := minioPool.GetClient()
client.PutObject(...)
```

### 统一连接池管理器

```go
// 创建统一管理器
managerConfig := &pool.PoolManagerConfig{
    Redis: &pool.RedisPoolConfig{
        Mode: pool.RedisModeStandalone,
        Addr: "localhost:6379",
    },
    MinIO: &pool.MinIOPoolConfig{
        Endpoint: "localhost:9000",
        AccessKeyID: "minioadmin",
        SecretAccessKey: "minioadmin",
    },
    HealthCheckInterval: 30 * time.Second,
}

manager, _ := pool.NewPoolManager(managerConfig)

// 获取连接池
redisPool := manager.GetRedisPool()
minioPool := manager.GetMinIOPool()
```

## Redis连接池

### 支持的模式

#### 1. 单机模式 (Standalone)

```go
config := &pool.RedisPoolConfig{
    Mode:     pool.RedisModeStandalone,
    Addr:     "localhost:6379",
    Password: "password",
    DB:       0,
    PoolSize: 100,
}
```

#### 2. 哨兵模式 (Sentinel)

```go
config := &pool.RedisPoolConfig{
    Mode:             pool.RedisModeSentinel,
    MasterName:       "mymaster",
    SentinelAddrs:    []string{"localhost:26379", "localhost:26380"},
    SentinelPassword: "sentinel-password",
    Password:         "redis-password",
    PoolSize:         100,
}
```

#### 3. 集群模式 (Cluster)

```go
config := &pool.RedisPoolConfig{
    Mode:         pool.RedisModeCluster,
    ClusterAddrs: []string{
        "localhost:7000",
        "localhost:7001",
        "localhost:7002",
    },
    Password:    "password",
    PoolSize:    100,
    RouteRandomly: true,
}
```

### 配置选项

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| Mode | RedisMode | standalone | Redis模式 |
| Addr | string | localhost:6379 | 单机地址 |
| Password | string | "" | 密码 |
| DB | int | 0 | 数据库编号 |
| PoolSize | int | CPU*10 | 连接池大小 |
| MinIdleConns | int | CPU | 最小空闲连接 |
| MaxConnAge | time.Duration | 30m | 连接最大生存时间 |
| PoolTimeout | time.Duration | 4s | 获取连接超时 |
| IdleTimeout | time.Duration | 5m | 空闲连接超时 |
| DialTimeout | time.Duration | 5s | 连接超时 |
| ReadTimeout | time.Duration | 3s | 读取超时 |
| WriteTimeout | time.Duration | 3s | 写入超时 |
| MaxRetries | int | 3 | 最大重试次数 |

### 获取统计信息

```go
stats := redisPool.GetStats()
fmt.Printf("Hits: %d, Misses: %d, Timeouts: %d\n",
    stats.Hits, stats.Misses, stats.Timeouts)
```

### 健康检查

```go
err := redisPool.Ping(ctx)
if err != nil {
    log.Printf("Redis unhealthy: %v", err)
}
```

## MinIO连接池

### 配置选项

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| Endpoint | string | - | MinIO服务地址 |
| AccessKeyID | string | - | 访问密钥ID |
| SecretAccessKey | string | - | 访问密钥 |
| UseSSL | bool | false | 是否使用SSL |
| Region | string | - | 区域 |
| MaxIdleConns | int | 100 | 最大空闲连接数 |
| MaxIdleConnsPerHost | int | 100 | 每个主机的最大空闲连接数 |
| MaxConnsPerHost | int | 0 | 每个主机的最大连接数 |
| IdleConnTimeout | time.Duration | 90s | 空闲连接超时 |
| DialTimeout | time.Duration | 30s | 连接超时 |
| TLSHandshakeTimeout | time.Duration | 10s | TLS握手超时 |
| ResponseHeaderTimeout | time.Duration | 10s | 响应头超时 |

### HTTP传输优化

```go
config := &pool.MinIOPoolConfig{
    Endpoint:            "minio.example.com",
    MaxIdleConnsPerHost: 200,    // 提高空闲连接数
    MaxConnsPerHost:     0,      // 无限制
    IdleConnTimeout:     120 * time.Second,
    KeepAlive:           30 * time.Second,
    DisableCompression:  false,  // 启用压缩
}
```

## 连接池管理器

### 特性

- ✅ 统一管理多个连接池
- ✅ 自动健康检查
- ✅ 备份MinIO支持
- ✅ 优雅关闭

### 完整示例

```go
// 创建管理器
managerConfig := &pool.PoolManagerConfig{
    Redis: &pool.RedisPoolConfig{
        Mode:     pool.RedisModeStandalone,
        Addr:     "localhost:6379",
        PoolSize: 100,
    },
    MinIO: &pool.MinIOPoolConfig{
        Endpoint:        "localhost:9000",
        AccessKeyID:     "minioadmin",
        SecretAccessKey: "minioadmin",
    },
    BackupMinIO: &pool.MinIOPoolConfig{
        Endpoint:        "backup.localhost:9000",
        AccessKeyID:     "minioadmin",
        SecretAccessKey: "minioadmin",
    },
    HealthCheckInterval: 30 * time.Second,
}

manager, err := pool.NewPoolManager(managerConfig)
if err != nil {
    log.Fatal(err)
}
defer manager.Close()

// 使用连接池
redisPool := manager.GetRedisPool()
minioPool := manager.GetMinIOPool()
backupPool := manager.GetBackupMinIOPool()
```

## 最佳实践

### 1. 合理配置连接池大小

```go
// ✅ 推荐：基于CPU核心数
cpuCount := runtime.NumCPU()
config := &pool.RedisPoolConfig{
    PoolSize:     cpuCount * 10,  // 连接池大小
    MinIdleConns: cpuCount,       // 最小空闲连接
}

// ❌ 不推荐：固定值
config := &pool.RedisPoolConfig{
    PoolSize:     100,  // 可能不适合所有环境
    MinIdleConns: 10,
}
```

### 2. 使用上下文控制超时

```go
// ✅ 推荐：使用上下文
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

err := redisPool.Ping(ctx)

// ❌ 不推荐：无超时控制
err := redisPool.Ping(context.Background())
```

### 3. 单例模式

```go
var (
    poolManager *pool.PoolManager
    once        sync.Once
)

func GetPoolManager() *pool.PoolManager {
    once.Do(func() {
        config := &pool.PoolManagerConfig{...}
        poolManager, _ = pool.NewPoolManager(config)
    })
    return poolManager
}
```

### 4. 监控连接池状态

```go
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := redisPool.GetStats()
        log.Printf("Redis pool stats: Hits=%d, Misses=%d, Timeouts=%d",
            stats.Hits, stats.Misses, stats.Timeouts)
    }
}()
```

### 5. 优雅关闭

```go
func main() {
    manager, _ := pool.NewPoolManager(config)
    
    // 捕获信号
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    // 等待信号
    <-sigChan
    
    // 优雅关闭
    manager.Close()
}
```

## 性能调优

### Redis连接池

```go
// 高并发场景
config := &pool.RedisPoolConfig{
    PoolSize:      500,              // 增大连接池
    MinIdleConns:  50,               // 保持足够的空闲连接
    MaxConnAge:    30 * time.Minute, // 定期更新连接
    PoolTimeout:   2 * time.Second,  // 快速超时
    IdleTimeout:   10 * time.Minute, // 延长空闲时间
    MaxRetries:    3,                // 自动重试
}
```

### MinIO连接池

```go
// 大文件上传场景
config := &pool.MinIOPoolConfig{
    MaxIdleConnsPerHost:   200,             // 增加空闲连接
    IdleConnTimeout:       120 * time.Second,
    ResponseHeaderTimeout: 30 * time.Second, // 延长响应超时
    KeepAlive:             60 * time.Second,
}
```

## 故障排查

### Redis连接池错误

**问题**: `connection pool timeout`

**解决方案**:
```go
config.PoolSize = 200        // 增大连接池
config.PoolTimeout = 5 * time.Second
config.MinIdleConns = 20
```

**问题**: `too many connections`

**解决方案**:
```go
config.MaxConnAge = 10 * time.Minute  // 定期回收连接
config.IdleTimeout = 5 * time.Minute
```

### MinIO连接池错误

**问题**: `connection reset by peer`

**解决方案**:
```go
config.KeepAlive = 30 * time.Second
config.IdleConnTimeout = 90 * time.Second
```

## 性能基准

```
BenchmarkRedisPoolGet-8         5000000    250 ns/op
BenchmarkMinIOPoolGet-8         3000000    380 ns/op
BenchmarkHealthCheck-8          1000000   1200 ns/op
```

## 相关资源

- [go-redis文档](https://github.com/go-redis/redis)
- [MinIO Go Client](https://docs.min.io/docs/golang-client-quickstart-guide.html)
- [Go HTTP Transport](https://golang.org/pkg/net/http/#Transport)

## 许可

本包是 MinIODB 项目的一部分。
