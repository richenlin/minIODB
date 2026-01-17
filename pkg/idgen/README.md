# pkg/idgen

分布式ID生成器包，提供多种ID生成策略。

## 特性

- ✅ **UUID v4** - 标准UUID生成
- ✅ **Snowflake** - Twitter Snowflake算法（分布式、时间有序）
- ✅ **Custom** - 自定义格式（前缀+时间戳+随机数）
- ✅ **验证器** - ID格式验证
- ✅ **高性能** - 优化的算法实现
- ✅ **线程安全** - 支持并发使用

## 快速开始

### 基本用法

```go
import "minIODB/pkg/idgen"

// 创建ID生成器
config := &idgen.GeneratorConfig{
    NodeID:          1,
    DefaultStrategy: "uuid",
}
generator, _ := idgen.NewGenerator(config)

// 生成UUID
id, _ := generator.Generate("users", idgen.StrategyUUID, "")
// 输出: "550e8400-e29b-41d4-a716-446655440000"
```

### Snowflake ID

```go
// 生成Snowflake ID（分布式、时间有序）
id, _ := generator.Generate("orders", idgen.StrategySnowflake, "order")
// 输出: "order-1234567890123456789"

// 不带前缀
id, _ := generator.Generate("orders", idgen.StrategySnowflake, "")
// 输出: "1234567890123456789"
```

### 自定义 ID

```go
// 生成自定义格式 ID（前缀-时间戳-随机数）
id, _ := generator.Generate("invoices", idgen.StrategyCustom, "INV")
// 输出: "INV-20260117150405123-a3f5b8c2d4e6"
```

### ID 验证

```go
// 验证ID格式
err := generator.Validate("user-123", 255, "^[a-zA-Z0-9_-]+$")
if err != nil {
    // 处理验证错误
}
```

## ID生成策略

### 1. UUID (StrategyUUID)

**特点:**
- 全局唯一
- 128位
- 符合RFC 4122标准
- 无需协调

**适用场景:**
- 分布式系统
- 不需要排序的场景
- 需要完全随机的ID

**示例:**
```go
id, _ := generator.Generate("table", idgen.StrategyUUID, "")
// "550e8400-e29b-41d4-a716-446655440000"
```

### 2. Snowflake (StrategySnowflake)

**特点:**
- 64位整数
- 时间有序
- 包含节点信息
- 高性能（每毫秒4096个ID）

**结构:**
```
64位 = 1位符号 + 41位时间戳 + 10位节点ID + 12位序列号
```

**适用场景:**
- 需要时间排序
- 分布式系统（支持1024个节点）
- 高并发场景
- 数据库主键

**示例:**
```go
config := &idgen.GeneratorConfig{
    NodeID: 1, // 节点ID (0-1023)
}
generator, _ := idgen.NewGenerator(config)
id, _ := generator.Generate("table", idgen.StrategySnowflake, "")
// "1234567890123456789"
```

### 3. Custom (StrategyCustom)

**特点:**
- 可自定义前缀
- 包含时间戳（可读）
- 包含随机后缀
- 易于识别

**格式:**
```
prefix-YYYYMMDDHHMMSSmmm-random
```

**适用场景:**
- 需要前缀标识
- 需要可读性
- 业务单号
- 文档编号

**示例:**
```go
id, _ := generator.Generate("table", idgen.StrategyCustom, "ORDER")
// "ORDER-20260117150405123-a3f5b8c2d4e6"
```

### 4. UserProvided (StrategyUserProvided)

用户自行提供ID，不进行生成。

## API 参考

### 配置

```go
type GeneratorConfig struct {
    NodeID          int64  // 节点ID (用于Snowflake, 0-1023)
    DefaultStrategy string // 默认策略 ("uuid", "snowflake", "custom")
}
```

### 策略常量

```go
const (
    StrategyUUID         = "uuid"
    StrategySnowflake    = "snowflake"
    StrategyCustom       = "custom"
    StrategyUserProvided = "user_provided"
)
```

### 主要接口

#### NewGenerator(config) (IDGenerator, error)

创建ID生成器。

**参数:**
- `config *GeneratorConfig` - 配置（可为nil，使用默认配置）

**返回:**
- `IDGenerator` - 生成器实例
- `error` - 错误信息

#### Generate(table, strategy, prefix) (string, error)

生成ID。

**参数:**
- `table string` - 表名（预留参数）
- `strategy IDGeneratorStrategy` - 生成策略
- `prefix string` - 前缀（用于Snowflake和Custom策略）

**返回:**
- `string` - 生成的ID
- `error` - 错误信息

#### Validate(id, maxLength, pattern) error

验证ID格式。

**参数:**
- `id string` - 要验证的ID
- `maxLength int` - 最大长度
- `pattern string` - 正则表达式模式

**返回:**
- `error` - 验证错误

## 使用示例

### 在HTTP服务中使用

```go
type UserService struct {
    idGenerator idgen.IDGenerator
}

func NewUserService() *UserService {
    config := &idgen.GeneratorConfig{
        NodeID:          1,
        DefaultStrategy: "snowflake",
    }
    generator, _ := idgen.NewGenerator(config)
    
    return &UserService{
        idGenerator: generator,
    }
}

func (s *UserService) CreateUser(name string) (*User, error) {
    // 生成用户ID
    userID, err := s.idGenerator.Generate("users", idgen.StrategySnowflake, "")
    if err != nil {
        return nil, err
    }
    
    user := &User{
        ID:   userID,
        Name: name,
    }
    
    // 保存用户...
    return user, nil
}
```

### 多策略使用

```go
func GenerateIDs() {
    generator, _ := idgen.NewGenerator(nil)
    
    // UUID用于用户ID
    userID, _ := generator.Generate("users", idgen.StrategyUUID, "")
    fmt.Println("User ID:", userID)
    
    // Snowflake用于订单ID（需要排序）
    orderID, _ := generator.Generate("orders", idgen.StrategySnowflake, "ORD")
    fmt.Println("Order ID:", orderID)
    
    // Custom用于发票号（需要前缀和可读性）
    invoiceID, _ := generator.Generate("invoices", idgen.StrategyCustom, "INV")
    fmt.Println("Invoice ID:", invoiceID)
}
```

### 批量生成

```go
func GenerateBatchIDs(count int) ([]string, error) {
    generator, _ := idgen.NewGenerator(&idgen.GeneratorConfig{
        NodeID: 1,
    })
    
    ids := make([]string, count)
    for i := 0; i < count; i++ {
        id, err := generator.Generate("", idgen.StrategySnowflake, "")
        if err != nil {
            return nil, err
        }
        ids[i] = id
    }
    
    return ids, nil
}
```

## Snowflake 配置

### 节点ID分配

在分布式环境中，每个节点需要唯一的节点ID：

```go
// 节点1
config1 := &idgen.GeneratorConfig{
    NodeID: 1,
}

// 节点2
config2 := &idgen.GeneratorConfig{
    NodeID: 2,
}
```

### 性能特点

- **生成速度**: 每毫秒可生成4096个ID
- **时间范围**: 可用69年（从2022-01-01开始）
- **节点数量**: 支持1024个节点
- **并发安全**: 使用互斥锁保证线程安全

## 性能

### 基准测试结果

```
BenchmarkUUIDGenerate-8         3000000    420 ns/op
BenchmarkSnowflakeGenerate-8    5000000    280 ns/op
BenchmarkCustomGenerate-8       2000000    650 ns/op
```

### 性能建议

1. **UUID**: 适合低频生成场景
2. **Snowflake**: 适合高并发场景
3. **Custom**: 适合需要可读性的场景

## 最佳实践

### 1. 选择合适的策略

```go
// ✅ 推荐：根据场景选择策略
// 用户ID - UUID（全局唯一，无需排序）
userID, _ := generator.Generate("users", idgen.StrategyUUID, "")

// 订单ID - Snowflake（需要时间排序）
orderID, _ := generator.Generate("orders", idgen.StrategySnowflake, "")

// 业务单号 - Custom（需要前缀识别）
docID, _ := generator.Generate("docs", idgen.StrategyCustom, "DOC")
```

### 2. 合理分配节点ID

```go
// ✅ 推荐：使用环境变量或配置文件
nodeID := os.Getenv("NODE_ID") // 从环境变量读取
config := &idgen.GeneratorConfig{
    NodeID: parseInt(nodeID),
}

// ❌ 不推荐：硬编码节点ID
config := &idgen.GeneratorConfig{
    NodeID: 1, // 多节点时会冲突
}
```

### 3. 单例模式

```go
// ✅ 推荐：应用启动时创建一次
var (
    idGenerator idgen.IDGenerator
    once        sync.Once
)

func GetIDGenerator() idgen.IDGenerator {
    once.Do(func() {
        idGenerator, _ = idgen.NewGenerator(&idgen.GeneratorConfig{
            NodeID: 1,
        })
    })
    return idGenerator
}
```

### 4. 错误处理

```go
// ✅ 推荐：检查所有错误
id, err := generator.Generate("table", idgen.StrategySnowflake, "")
if err != nil {
    log.Printf("Failed to generate ID: %v", err)
    return err
}

// ❌ 不推荐：忽略错误
id, _ := generator.Generate("table", idgen.StrategySnowflake, "")
```

## 故障排查

### Snowflake时钟回拨

**问题**: `clock moved backwards`

**原因**: 系统时钟回退

**解决方案**:
1. 使用NTP同步时间
2. 避免手动调整时间
3. 在虚拟机中使用时间同步

### 节点ID冲突

**问题**: 生成重复ID

**原因**: 多个节点使用相同的节点ID

**解决方案**:
1. 确保每个节点有唯一ID
2. 使用配置管理工具分配ID
3. 监控ID冲突

## 相关资源

- [Twitter Snowflake 论文](https://blog.twitter.com/engineering/en_us/a/2010/announcing-snowflake.html)
- [RFC 4122 (UUID)](https://tools.ietf.org/html/rfc4122)
- [分布式ID生成方案对比](https://www.infoq.com/articles/distributed-id-generation/)

## 许可

本包是 MinIODB 项目的一部分。
