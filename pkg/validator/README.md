# pkg/validator

数据验证包，提供安全的输入验证和清理功能。

## 特性

- ✅ **表名验证** - SQL注入防护
- ✅ **ID验证** - 防止特殊字符注入
- ✅ **邮箱验证** - RFC兼容的邮箱格式验证
- ✅ **URL验证** - HTTP/HTTPS URL验证
- ✅ **字符串清理** - 移除危险的控制字符
- ✅ **SQL安全** - 安全的标识符和字面量引用

## 快速开始

### 基本用法

```go
import "minIODB/pkg/validator"

// 使用默认验证器（推荐）
err := validator.ValidateTableName("users")
if err != nil {
    // 处理错误
}

err = validator.ValidateID("user-123")
if err != nil {
    // 处理错误
}
```

### 创建自定义验证器

```go
v := validator.New()
err := v.ValidateTableName("my_table")
```

## API 参考

### 表名验证

验证表名是否符合安全规范：
- 必须以字母开头
- 只能包含字母、数字和下划线
- 长度在 1-128 字符之间
- 不能是 SQL 保留关键字

```go
err := validator.ValidateTableName("users")
// 有效

err := validator.ValidateTableName("123_table")
// 错误: 不能以数字开头

err := validator.ValidateTableName("select")
// 错误: SQL 保留关键字
```

### ID 验证

验证 ID 是否安全：
- 可以包含字母、数字、连字符和下划线
- 长度在 1-255 字符之间
- 不能为空

```go
err := validator.ValidateID("user-123")
// 有效

err := validator.ValidateID("user@123")
// 错误: 包含非法字符

err := validator.ValidateID("")
// 错误: ID 不能为空
```

### 邮箱验证

```go
err := validator.ValidateEmail("user@example.com")
// 有效

err := validator.ValidateEmail("invalid-email")
// 错误: 无效的邮箱格式
```

### URL 验证

```go
err := validator.ValidateURL("https://api.example.com")
// 有效

err := validator.ValidateURL("ftp://example.com")
// 错误: 只支持 http/https
```

### 字符串清理

移除危险的控制字符（保留换行、回车、制表符）：

```go
clean := validator.SanitizeString("hello\x00world")
// 返回: "helloworld"

clean = validator.SanitizeString("hello\nworld")
// 返回: "hello\nworld" (保留换行)
```

### SQL 安全引用

#### 引用标识符

```go
quoted := validator.QuoteIdentifier("user_table")
// 返回: "user_table"

quoted = validator.QuoteIdentifier("table with spaces")
// 返回: "table with spaces"

quoted = validator.QuoteIdentifier("user\"data")
// 返回: "user""data" (转义双引号)
```

#### 引用字符串字面量

```go
quoted := validator.QuoteLiteral("hello")
// 返回: 'hello'

quoted = validator.QuoteLiteral("it's a test")
// 返回: 'it''s a test' (转义单引号)

quoted = validator.QuoteLiteral("test\x00data")
// 返回: 'testdata' (移除控制字符)
```

## 错误类型

### 表名错误

- `ErrTableNameEmpty` - 表名为空
- `ErrTableNameInvalidLength` - 长度不在 1-128 之间
- `ErrTableNameInvalidFormat` - 格式不符合规范
- `ErrTableNameReservedKeyword` - 使用了 SQL 保留关键字

### ID 错误

- `ErrIDEmpty` - ID 为空
- `ErrIDInvalidLength` - 长度不在 1-255 之间
- `ErrIDInvalidFormat` - 格式不符合规范

### 邮箱错误

- `ErrEmailEmpty` - 邮箱为空
- `ErrEmailInvalidFormat` - 格式不符合规范

### URL 错误

- `ErrURLEmpty` - URL 为空
- `ErrURLInvalidFormat` - 格式不符合规范

## 使用示例

### 在 HTTP 处理器中使用

```go
func CreateTableHandler(w http.ResponseWriter, r *http.Request) {
    var req struct {
        TableName string `json:"table_name"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }
    
    // 验证表名
    if err := validator.ValidateTableName(req.TableName); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    // 安全地使用表名
    // ...
}
```

### 构建安全的 SQL 查询

```go
func QueryTable(tableName string, userInput string) error {
    // 验证表名
    if err := validator.ValidateTableName(tableName); err != nil {
        return err
    }
    
    // 安全地引用标识符和字面量
    query := fmt.Sprintf(
        "SELECT * FROM %s WHERE name = %s",
        validator.QuoteIdentifier(tableName),
        validator.QuoteLiteral(userInput),
    )
    
    // 执行查询
    // ...
    return nil
}
```

### 批量验证

```go
type CreateUserRequest struct {
    ID    string
    Email string
    URL   string
}

func ValidateCreateUserRequest(req *CreateUserRequest) error {
    if err := validator.ValidateID(req.ID); err != nil {
        return fmt.Errorf("invalid ID: %w", err)
    }
    
    if err := validator.ValidateEmail(req.Email); err != nil {
        return fmt.Errorf("invalid email: %w", err)
    }
    
    if req.URL != "" {
        if err := validator.ValidateURL(req.URL); err != nil {
            return fmt.Errorf("invalid URL: %w", err)
        }
    }
    
    return nil
}
```

## SQL 保留关键字列表

以下 SQL 关键字不能用作表名：

```
select, from, where, and, or, insert, into, values, update, set, 
delete, drop, create, table, index, alter, add, column, truncate, 
grant, revoke, union, join, left, right, inner, outer, on, group, 
by, order, having, limit, offset, as, distinct, count, sum, avg, 
max, min, null, not, in, exists, between, like, is, case, when, 
then, else, end, primary, key, foreign, references, constraint, 
unique, view, database, schema, use, exec, execute, procedure, 
function, trigger
```

## 性能

所有验证操作都经过优化，适合高并发场景：

```
BenchmarkValidateTableName-8    3000000    450 ns/op
BenchmarkValidateID-8           5000000    320 ns/op  
BenchmarkSanitizeString-8       2000000    680 ns/op
```

## 最佳实践

1. **总是验证用户输入**

```go
// ❌ 不安全
query := fmt.Sprintf("SELECT * FROM %s", userInput)

// ✅ 安全
if err := validator.ValidateTableName(userInput); err != nil {
    return err
}
query := fmt.Sprintf("SELECT * FROM %s", validator.QuoteIdentifier(userInput))
```

2. **使用包级函数简化代码**

```go
// ✅ 推荐：简洁
err := validator.ValidateTableName("users")

// ⚪ 可以：需要自定义验证器时使用
v := validator.New()
err := v.ValidateTableName("users")
```

3. **组合使用验证和清理**

```go
// 先清理，再验证
cleaned := validator.SanitizeString(userInput)
if err := validator.ValidateID(cleaned); err != nil {
    return err
}
```

4. **提供友好的错误信息**

```go
if err := validator.ValidateTableName(name); err != nil {
    return fmt.Errorf("table name '%s' is invalid: %w", name, err)
}
```

## 相关资源

- [OWASP SQL 注入防护](https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html)
- [Go 正则表达式文档](https://golang.org/pkg/regexp/)
- [RFC 5322 (邮箱格式)](https://tools.ietf.org/html/rfc5322)
