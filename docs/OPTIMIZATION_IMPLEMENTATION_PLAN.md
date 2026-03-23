# MinIODB v2.2 优化实施方案

> **目标读者**: 工程 LLM（Claude/GPT 等自动化编码 Agent）
> **前置文档**: `docs/CODE_REVIEW_REPORT.md`
> **日期**: 2026-03-23

---

## 执行须知

### 给工程 LLM 的约定

1. **严格按 Phase → Task 顺序执行**，同一 Phase 内的 Task 可并行（除非标注依赖）
2. **每完成一个 Task**，运行该 Task 的验证命令，通过后独立 `git commit`
3. **不要修改 Task 未涉及的文件**，避免意外副作用
4. **所有代码修改必须通过 `go build ./...` 和 `go vet ./...`**
5. **如果某个 Task 的修改导致已有测试失败，必须先修复测试再继续**
6. **文件路径均相对于项目根目录** `/Volumes/ExternalSSD/Users/richen/Workspace/go/src/minIODB`

### 全局验证命令

每个 Phase 完成后，执行全局验证：

```bash
go build ./...
go vet ./...
go test ./... -count=1 -timeout 120s 2>&1 | tail -50
```

---

## Phase 1: 关键 Bug 修复

> **目标**: 修复影响系统稳定性和数据正确性的 P0 级问题
> **风险**: 低 — 均为明确的 bug 修复，不涉及架构变更
> **预估工期**: 1-2 天

---

### Task 1.1: 修复 Shutdown 路径 — 移除 `os.Exit(0)`

**问题**: `cmd/main.go:727` 的 `os.Exit(0)` 导致所有 `defer` 不执行（`storageInstance.Close()`、`logger.Close()`、`serviceRegistry.Stop()` 等），造成连接池泄漏和日志丢失。

**修改文件**: `cmd/main.go`

**修改内容**:

1. **删除 `os.Exit(0)`**（第 727 行）— `waitForShutdown` 函数正常返回即可，`main()` 函数结束时 Go runtime 会执行所有 defer

2. **将 `waitForShutdown` 中显式关闭的资源与 `main()` 中 defer 的资源对齐**，避免重复关闭。当前 `waitForShutdown` 已显式停止 gRPC/REST/Dashboard/Replicator/Backup/Subscription/Compaction，而 `defer storageInstance.Close()` 和 `defer logger.Close()` 在 `main()` 退出时执行，时序正确（先停服务，再关连接池，最后关日志）。

具体修改：

```go
// cmd/main.go — waitForShutdown 函数末尾
// 删除这两行:
//   logger.Sugar.Info("Shutdown complete")
//   os.Exit(0)
// 替换为:
    logger.Sugar.Info("Shutdown complete")
    // 正常返回，让 main() 的 defer 链执行资源清理
```

**验证**:

```bash
go build ./cmd/...
go vet ./cmd/...
```

---

### Task 1.2: 修复 Goroutine 中的 `Fatalf`

**问题**: `cmd/main.go:442, 481, 758` 在 goroutine 中调用 `logger.Sugar.Fatalf()`（内部调用 `os.Exit(1)`），跳过所有 graceful shutdown。

**修改文件**: `cmd/main.go`

**修改内容**:

1. 在 `main()` 函数中添加一个 error channel（在 `ctx, cancel` 之后）:

```go
// 在 cmd/main.go 的 ctx, cancel := ... 之后（约第 162 行后）添加：
fatalCh := make(chan error, 3) // 用于接收 goroutine 启动失败
```

2. 修改 gRPC 启动 goroutine（约第 440-444 行）:

```go
// 原始代码:
go func() {
    if err := grpcService.Start(cfg.Server.GrpcPort); err != nil {
        logger.Sugar.Fatalf("Failed to start gRPC server: %v", err)
    }
}()

// 修改为:
go func() {
    if err := grpcService.Start(cfg.Server.GrpcPort); err != nil {
        fatalCh <- fmt.Errorf("gRPC server failed: %w", err)
    }
}()
```

3. 修改 REST 启动 goroutine（约第 479-483 行）:

```go
// 原始代码:
go func() {
    if err := restServer.Start(cfg.Server.RestPort); err != nil {
        logger.Sugar.Fatalf("Failed to start REST server: %v", err)
    }
}()

// 修改为:
go func() {
    if err := restServer.Start(cfg.Server.RestPort); err != nil {
        fatalCh <- fmt.Errorf("REST server failed: %w", err)
    }
}()
```

4. 修改 metrics server 启动 goroutine（`startMetricsServer` 函数，约第 755-759 行）:

```go
// 原始代码:
go func() {
    logger.Sugar.Infof(...)
    if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Sugar.Fatalf("Failed to start metrics server: %v", err)
    }
}()

// 修改为:
go func() {
    logger.Sugar.Infof(...)
    if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Sugar.Errorf("Failed to start metrics server: %v", err)
    }
}()
```

> 注意：metrics server 启动失败不应导致整个进程退出，改为 Error 级别日志即可。

5. 在 `waitForShutdown` 函数的信号等待处，增加对 `fatalCh` 的监听。修改 `waitForShutdown` 的函数签名，增加 `fatalCh <-chan error` 参数：

```go
func waitForShutdown(ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup,
    fatalCh <-chan error,
    grpcService *grpcTransport.Server, restServer *restTransport.Server,
    // ... 其余参数不变
```

将原始的 `<-sigChan` 改为 select：

```go
// 原始代码:
<-sigChan
logger.Sugar.Info("Received shutdown signal")

// 修改为:
select {
case sig := <-sigChan:
    logger.Sugar.Infof("Received shutdown signal: %v", sig)
case err := <-fatalCh:
    logger.Sugar.Errorf("Fatal error from background service: %v", err)
}
```

6. 更新 `main()` 中对 `waitForShutdown` 的调用（约第 514 行），传入 `fatalCh`。

**验证**:

```bash
go build ./cmd/...
go vet ./cmd/...
```

---

### Task 1.3: 修复 WaitGroup 死代码

**问题**: `cmd/main.go:95` 声明 `var wg sync.WaitGroup` 但从未调用 `wg.Add()`/`wg.Done()`，`waitForShutdown` 中 `wg.Wait()` 立即返回。

**修改文件**: `cmd/main.go`

**修改内容**: 这个 WaitGroup 本意是追踪后台 goroutine。有两个选择：

**方案 A（推荐）: 移除无用的 WaitGroup**

删除第 95 行 `var wg sync.WaitGroup`，从 `waitForShutdown` 签名中移除 `wg *sync.WaitGroup` 参数，删除 `waitForShutdown` 内的 `wg.Wait()` 相关代码块（第 711-724 行）：

```go
// 删除以下代码块（约第 711-724 行）:
logger.Sugar.Info("Waiting for background tasks to complete...")
done := make(chan struct{})
go func() {
    wg.Wait()
    close(done)
}()

select {
case <-done:
    logger.Sugar.Info("All goroutines stopped gracefully")
case <-time.After(30 * time.Second):
    logger.Sugar.Warn("Timeout waiting for goroutines, forcing shutdown")
}
```

> 当前 shutdown 已通过逐个调用各组件 Stop() 实现有序关闭，WaitGroup 是冗余的。

**验证**:

```bash
go build ./cmd/...
```

---

### Task 1.4: 修复查询缓存 `normalizeQuery` 误匹配

**问题**: `internal/query/query_cache.go:412-417` 的 `normalizeQuery()` 对整个 SQL 做 `strings.ToLower()`，包括字符串字面量。导致 `WHERE name='John'` 和 `WHERE name='john'` 产生相同 cache key，返回错误结果。

**修改文件**: `internal/query/query_cache.go`

**修改内容**: 重写 `normalizeQuery` 方法（约第 412-417 行），在 ToLower 前保护字符串字面量：

```go
// normalizeQuery 标准化查询语句（保留字符串字面量的大小写）
func (qc *QueryCache) normalizeQuery(query string) string {
    // 移除多余空格
    normalized := strings.Join(strings.Fields(strings.TrimSpace(query)), " ")

    // 提取并保护字符串字面量，对非字面量部分做 ToLower
    var result strings.Builder
    inSingleQuote := false
    escaped := false

    for i := 0; i < len(normalized); i++ {
        ch := normalized[i]

        if escaped {
            result.WriteByte(ch)
            escaped = false
            continue
        }

        if ch == '\\' {
            result.WriteByte(ch)
            escaped = true
            continue
        }

        if ch == '\'' {
            inSingleQuote = !inSingleQuote
            result.WriteByte(ch)
            continue
        }

        if inSingleQuote {
            // 字符串字面量内保持原样
            result.WriteByte(ch)
        } else {
            // 非字面量部分转小写
            if ch >= 'A' && ch <= 'Z' {
                result.WriteByte(ch + 32)
            } else {
                result.WriteByte(ch)
            }
        }
    }

    return result.String()
}
```

**验证**:

```bash
go test ./internal/query/ -run TestQueryCache -v -count=1
```

如果没有现成测试覆盖此场景，**新增测试**到 `internal/query/query_cache_test.go`（如果不存在则参考已有测试文件的命名风格创建）：

```go
func TestNormalizeQuery_PreservesStringLiterals(t *testing.T) {
    qc := &QueryCache{}
    tests := []struct {
        input    string
        expected string
    }{
        {
            input:    "SELECT * FROM users WHERE name = 'John'",
            expected: "select * from users where name = 'John'",
        },
        {
            input:    "SELECT * FROM users WHERE name = 'john'",
            expected: "select * from users where name = 'john'",
        },
        {
            input:    "SELECT * FROM T WHERE a = 'Hello World' AND b = 1",
            expected: "select * from t where a = 'Hello World' and b = 1",
        },
    }
    for _, tt := range tests {
        result := qc.normalizeQuery(tt.input)
        if result != tt.expected {
            t.Errorf("normalizeQuery(%q) = %q, want %q", tt.input, result, tt.expected)
        }
    }
}
```

---

### Task 1.5: 修复 ColumnPruner SQL 注入防御缺口

**问题**: `internal/query/column_pruning.go:131-160` 的 `BuildOptimizedViewSQL` 和 `buildFilesClause` 直接拼接 `tableName` 和文件路径，未使用 `security.QuoteIdentifier()` / `security.QuoteLiteral()`。

**修改文件**: `internal/query/column_pruning.go`

**修改内容**:

1. 在文件顶部添加 import（如未导入）:

```go
import "minIODB/internal/security"
```

2. 修改 `BuildOptimizedViewSQL`（约第 131-143 行）:

```go
func (cp *ColumnPruner) BuildOptimizedViewSQL(tableName string, files []string, requiredColumns []string) string {
    if !cp.enabled || len(requiredColumns) == 0 || (len(requiredColumns) == 1 && requiredColumns[0] == "*") {
        return cp.buildStandardViewSQL(tableName, files)
    }

    columnList := strings.Join(requiredColumns, ", ")
    filesClause := cp.buildFilesClause(files)

    // 使用 QuoteIdentifier 防止 tableName 注入
    safeTableName := security.DefaultSanitizer.QuoteIdentifier(tableName)

    return fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS SELECT %s FROM read_parquet([%s], union_by_name=true)`,
        safeTableName, columnList, filesClause)
}
```

3. 修改 `buildStandardViewSQL`（约第 146-151 行）同理：

```go
func (cp *ColumnPruner) buildStandardViewSQL(tableName string, files []string) string {
    filesClause := cp.buildFilesClause(files)
    safeTableName := security.DefaultSanitizer.QuoteIdentifier(tableName)

    return fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS SELECT * FROM read_parquet([%s], union_by_name=true)`,
        safeTableName, filesClause)
}
```

4. 修改 `buildFilesClause`（约第 154-160 行）使用 `QuoteLiteral`:

```go
func (cp *ColumnPruner) buildFilesClause(files []string) string {
    var fileNames []string
    for _, file := range files {
        fileNames = append(fileNames, security.DefaultSanitizer.QuoteLiteral(file))
    }
    return strings.Join(fileNames, ",")
}
```

5. 同样修改 `internal/query/hybrid_query.go:175-176` 中的 SQL 拼接：

```go
// 原始代码:
createViewSQL := fmt.Sprintf(`CREATE OR REPLACE VIEW buffer_view AS SELECT * FROM read_parquet('%s')`, tempFile)
dropViewSQL := fmt.Sprintf(`DROP VIEW IF EXISTS buffer_view`)

// 修改为:
safeTempFile := security.DefaultSanitizer.QuoteLiteral(tempFile)
createViewSQL := fmt.Sprintf(`CREATE OR REPLACE VIEW buffer_view AS SELECT * FROM read_parquet(%s)`, safeTempFile)
dropViewSQL := `DROP VIEW IF EXISTS buffer_view`
```

**验证**:

```bash
go build ./internal/query/...
go test ./internal/query/ -run TestColumnPruning -v -count=1
```

---

### Phase 1 验证

```bash
go build ./...
go vet ./...
go test ./... -count=1 -timeout 120s
```

---

## Phase 2: 查询引擎性能优化

> **目标**: 解决查询路径的 P0/P1 性能瓶颈
> **风险**: 中 — 涉及 DuckDB 连接管理和文件缓存逻辑变更
> **预估工期**: 3-5 天
> **依赖**: Phase 1 完成

---

### Task 2.1: 激活 DuckDB 连接池

**问题**: `internal/query/query.go:975` 的 `getDBConnection()` 中 `dbPool` 始终为 nil，所有查询串行走单一 `*sql.DB`。配置 `config.DuckDBConfig` 和池实现 `NewDuckDBPool()` 已存在，但 `NewQuerier()` 从未初始化 `dbPool`。

**修改文件**: `internal/query/query.go`

**关键约束**: DuckDB `:memory:` 模式下，每个 `sql.DB` 连接拥有独立的内存空间。View 创建在 A 连接上，对 B 连接不可见。当前代码的 `ExecuteQuery()`（第 253-345 行）中，`getDBConnection()` 获取连接后，在**同一连接上**执行 `prepareTableDataWithPruning`（创建 View）和 `executeQueryWithOptimization`（执行查询），最后 `returnDBConnection` 归还。这个模式天然兼容连接池 — 只要 View 创建和查询发生在同一次借用周期内即可。

**修改内容**: 修改 `NewQuerier()`（约第 168-248 行），在创建单连接后，根据配置初始化连接池：

```go
func NewQuerier(redisPool *pool.RedisPool, minioClient storage.Uploader, cfg *config.Config, buf *buffer.ConcurrentBuffer, logger *zap.Logger) (*Querier, error) {
    // 初始化DuckDB — 单连接作为 fallback
    duckdbConn, err := sql.Open("duckdb", ":memory:")
    if err != nil {
        return nil, fmt.Errorf("failed to create DuckDB connection: %w", err)
    }

    // 初始化 DuckDB 连接池（如果配置启用）
    var dbPool *DuckDBPool
    duckdbCfg := cfg.QueryOptimization.DuckDB
    if duckdbCfg.Enabled && duckdbCfg.PoolSize > 0 {
        dbPool, err = NewDuckDBPool(duckdbCfg.PoolSize, logger)
        if err != nil {
            logger.Warn("Failed to create DuckDB connection pool, falling back to single connection",
                zap.Error(err))
        } else {
            logger.Info("DuckDB connection pool initialized",
                zap.Int("pool_size", duckdbCfg.PoolSize))
        }
    }

    // ... 其余初始化不变 ...

    querier := &Querier{
        // ... 已有字段 ...
        db:     duckdbConn,
        dbPool: dbPool,   // 新增：将池赋值给字段
        // ... 其余字段 ...
    }

    return querier, nil
}
```

同时修改 `Close()` 方法（约第 737-763 行），增加连接池关闭：

```go
func (q *Querier) Close() error {
    // 关闭 DuckDB 连接池（如果启用）
    if q.dbPool != nil {
        q.dbPool.Close()
    }

    // 关闭单连接
    if q.db != nil {
        q.db.Close()
    }

    // ... 其余清理不变 ...
}
```

**配置激活**: 确认 `config/config.yaml` 中有 DuckDB 配置块（如果没有，添加示例）：

```yaml
query_optimization:
  duckdb:
    enabled: true
    pool_size: 4
    performance:
      memory_limit: "1GB"
      threads: 4
      enable_object_cache: true
```

**验证**:

```bash
go build ./internal/query/...
go test ./internal/query/ -v -count=1 -timeout 60s
```

---

### Task 2.2: 添加 View 缓存避免每次查询重建

**问题**: `internal/query/query.go:453-501` 每次查询都执行 `DROP VIEW IF EXISTS` + `CREATE VIEW`，即使文件列表未变。DuckDB View 创建涉及 Parquet 文件扫描，开销显著。

**修改文件**: `internal/query/query.go`

**设计**: 在 `Querier` 结构体中添加 per-connection 的 View 状态缓存。由于启用连接池后每个连接有独立的 DuckDB 内存空间，View 缓存需要以 `(连接指针, 表名)` 为 key，以 `文件列表 hash` 为 value。

**修改内容**:

1. 在 `Querier` 结构体中添加 View 缓存字段（约第 140 行附近）:

```go
type Querier struct {
    // ... 已有字段 ...

    // View 缓存：key = "连接地址:表名", value = 文件列表的 hash
    viewCache   map[string]string
    viewCacheMu sync.RWMutex
}
```

2. 在 `NewQuerier()` 中初始化：

```go
viewCache: make(map[string]string),
```

3. 添加辅助函数：

```go
// viewCacheKey 生成 View 缓存键
func viewCacheKey(db *sql.DB, tableName string) string {
    return fmt.Sprintf("%p:%s", db, tableName)
}

// filesHash 计算文件列表的 hash
func filesHash(files []string) string {
    sorted := make([]string, len(files))
    copy(sorted, files)
    sort.Strings(sorted)
    h := sha256.Sum256([]byte(strings.Join(sorted, "|")))
    return hex.EncodeToString(h[:8]) // 前 8 字节足够
}

// isViewCurrent 检查 View 是否已是最新
func (q *Querier) isViewCurrent(db *sql.DB, tableName string, files []string) bool {
    key := viewCacheKey(db, tableName)
    hash := filesHash(files)
    q.viewCacheMu.RLock()
    defer q.viewCacheMu.RUnlock()
    return q.viewCache[key] == hash
}

// updateViewCache 更新 View 缓存
func (q *Querier) updateViewCache(db *sql.DB, tableName string, files []string) {
    key := viewCacheKey(db, tableName)
    hash := filesHash(files)
    q.viewCacheMu.Lock()
    q.viewCache[key] = hash
    q.viewCacheMu.Unlock()
}
```

4. 修改 `createTableViewWithDB`（约第 453 行）和 `createTableViewWithDBWithColumnPruning`（约第 504 行），在开头添加缓存检查：

```go
func (q *Querier) createTableViewWithDB(db *sql.DB, tableName string, files []string) error {
    if len(files) == 0 {
        return nil
    }

    // 检查 View 是否已是最新（文件列表未变则跳过重建）
    if q.isViewCurrent(db, tableName, files) {
        q.logger.Debug("View cache hit, skipping recreation",
            zap.String("table", tableName),
            zap.Int("files", len(files)))
        return nil
    }

    // ... 原有的 DROP + CREATE 逻辑不变 ...

    // 成功创建后更新缓存
    q.updateViewCache(db, tableName, files)
    return nil
}
```

对 `createTableViewWithDBWithColumnPruning` 同理添加相同的缓存检查逻辑（注意缓存 key 需要包含列信息，或简单地不为列裁剪路径做缓存）。

5. 在 `Close()` 中清空缓存：

```go
q.viewCacheMu.Lock()
q.viewCache = nil
q.viewCacheMu.Unlock()
```

**验证**:

```bash
go build ./internal/query/...
go test ./internal/query/ -v -count=1
```

---

### Task 2.3: 修复 `downloadToTemp` 文件名碰撞

**问题**: `internal/query/query.go:555` 使用 `filepath.Base(objectName)` 作为本地文件名。不同表的同名文件（如 `tableA/data_001.parquet` 和 `tableB/data_001.parquet`）会映射到同一个本地路径，导致数据覆盖。

**修改文件**: `internal/query/query.go`

**修改内容**: 修改 `downloadToTemp`（约第 554-555 行），使用完整 object path 的 hash 或子目录结构防止碰撞：

```go
func (q *Querier) downloadToTemp(ctx context.Context, objectName string) (string, error) {
    // 使用 objectName 的 MD5 前缀 + 原始文件名，避免碰撞
    hash := md5.Sum([]byte(objectName))
    safePrefix := hex.EncodeToString(hash[:4])
    localPath := filepath.Join(q.tempDir, safePrefix+"_"+filepath.Base(objectName))

    // ... 其余逻辑不变 ...
```

需要在文件顶部添加 `"crypto/md5"` 和 `"encoding/hex"` import（如未导入）。

**验证**:

```bash
go build ./internal/query/...
go test ./internal/query/ -v -count=1
```

---

### Task 2.4: 添加临时文件周期清理

**问题**: `internal/query/query.go:761` 的 `os.RemoveAll(q.tempDir)` 仅在进程退出时调用。长时间运行的服务会累积大量临时 Parquet 文件。

**修改文件**: `internal/query/query.go`

**修改内容**:

1. 在 `Querier` 结构体中添加清理 goroutine 控制字段：

```go
type Querier struct {
    // ... 已有字段 ...
    stopCleanup chan struct{}
}
```

2. 在 `NewQuerier()` 末尾（return 前）启动后台清理 goroutine：

```go
querier.stopCleanup = make(chan struct{})
go querier.tempFileCleanupLoop()
```

3. 添加清理函数：

```go
// tempFileCleanupLoop 周期性清理过期的临时文件
func (q *Querier) tempFileCleanupLoop() {
    ticker := time.NewTicker(10 * time.Minute)
    defer ticker.Stop()

    maxAge := 30 * time.Minute // 临时文件最大保留时间

    for {
        select {
        case <-q.stopCleanup:
            return
        case <-ticker.C:
            q.cleanExpiredTempFiles(maxAge)
        }
    }
}

// cleanExpiredTempFiles 清理超过 maxAge 的临时文件
func (q *Querier) cleanExpiredTempFiles(maxAge time.Duration) {
    entries, err := os.ReadDir(q.tempDir)
    if err != nil {
        return
    }

    cutoff := time.Now().Add(-maxAge)
    cleaned := 0

    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }
        info, err := entry.Info()
        if err != nil {
            continue
        }
        if info.ModTime().Before(cutoff) {
            if err := os.Remove(filepath.Join(q.tempDir, entry.Name())); err == nil {
                cleaned++
            }
        }
    }

    if cleaned > 0 {
        q.logger.Info("Cleaned expired temp files",
            zap.Int("count", cleaned),
            zap.Duration("max_age", maxAge))
    }
}
```

4. 在 `Close()` 中停止清理 goroutine（在 `os.RemoveAll(q.tempDir)` 之前）：

```go
if q.stopCleanup != nil {
    close(q.stopCleanup)
}
```

**验证**:

```bash
go build ./internal/query/...
go vet ./internal/query/...
```

---

### Task 2.5: 废弃 HybridQueryExecutor

**问题**: `internal/query/hybrid_query.go` 的混合查询合并在第 257-264 行是空实现（丢弃 Buffer 数据），且常规查询路径 `ExecuteQuery` 已通过 `getBufferFilesForTable`（`query.go:863`）将 Buffer 数据注入 DuckDB View。HybridQueryExecutor 的存在既冗余又有 bug。

**修改文件**: `internal/query/hybrid_query.go`

**修改内容**: 

**方案（推荐）: 标记废弃 + 强制走常规路径**

在 `HybridQueryExecutor.ExecuteQuery()` 方法中，无论 `config.Enabled` 值如何，始终走常规查询路径：

```go
// ExecuteQuery 执行查询
// 注意：Buffer 数据已在常规查询路径（Querier.ExecuteQuery → getBufferFilesForTable）中合并。
// HybridQueryExecutor 保留用于接口兼容，但内部直接委派给 Querier.ExecuteQuery。
func (hq *HybridQueryExecutor) ExecuteQuery(ctx context.Context, tableName, sqlQuery string) (*HybridQueryResult, error) {
    startTime := time.Now()

    // Buffer 数据已在 Querier.ExecuteQuery 的 prepareTableDataWithPruning 中
    // 通过 getBufferFilesForTable 合并到 DuckDB View，无需额外处理
    data, err := hq.querier.ExecuteQuery(sqlQuery)
    
    source := "storage"
    if hq.checkBufferHasData(tableName) {
        source = "hybrid" // Buffer 中有数据时标记来源
    }

    return &HybridQueryResult{
        Source:      source,
        Data:        data,
        Duration:    time.Since(startTime),
        CompletedAt: time.Now(),
        Error:       err,
    }, nil
}
```

同时在文件顶部添加 Deprecated 注释：

```go
// Deprecated: HybridQueryExecutor 已废弃。Buffer 数据合并已在 Querier.ExecuteQuery 的
// prepareTableDataWithPruning → getBufferFilesForTable 路径中实现。
// 此组件仅保留用于接口兼容，将在未来版本中移除。
type HybridQueryExecutor struct {
```

**验证**:

```bash
go build ./internal/query/...
go test ./internal/query/ -run TestHybrid -v -count=1
```

---

### Phase 2 验证

```bash
go build ./...
go vet ./...
go test ./... -count=1 -timeout 120s
```

---

## Phase 3: 生命周期与资源管理

> **目标**: 修复 goroutine 泄漏、完善 shutdown 序列
> **风险**: 低 — 增量修改，不影响核心逻辑
> **预估工期**: 2-3 天
> **依赖**: Phase 1 完成

---

### Task 3.1: MetadataManager 纳入 Shutdown 序列

**问题**: `cmd/main.go:635-639` 的 `waitForShutdown` 接收了 `metadataManager *metadata.Manager` 参数，但函数体中从未调用其 Stop 方法。

**修改文件**: `cmd/main.go`

**修改内容**: 在 `waitForShutdown` 函数中，compaction manager 停止之后（约第 709 行后），添加 MetadataManager 停止逻辑：

```go
    if compactionManager != nil {
        logger.Sugar.Info("Stopping compaction manager...")
        compactionManager.Stop()
        logger.Sugar.Info("Compaction manager stopped")
    }

    // 新增：停止 MetadataManager
    if metadataManager != nil {
        logger.Sugar.Info("Stopping metadata manager...")
        metadataManager.Stop()
        logger.Sugar.Info("Metadata manager stopped")
    }
```

**前置检查**: 确认 `metadata.Manager` 有 `Stop()` 方法。搜索 `internal/metadata/manager.go` 中的 `func (m *Manager) Stop` 或 `func (m *Manager) Close`。如果方法名不同，使用实际的方法名。如果不存在停止方法，则需要添加一个：

```go
// Stop 停止 MetadataManager 的后台 goroutine
func (m *Manager) Stop() {
    m.cancel()   // 取消内部 context
    m.wg.Wait()  // 等待后台 goroutine 退出
}
```

**验证**:

```bash
go build ./cmd/...
go build ./internal/metadata/...
```

---

### Task 3.2: 修复 fire-and-forget goroutine

**问题**: 3 处 goroutine 没有生命周期管理：
1. `internal/query/query.go:1747` — `go q.cacheMetadataToRedis(context.Background(), ...)`
2. `internal/service/miniodb_service.go:332` — `go s.publishWriteEvent(ctx, ...)`
3. `internal/dashboard/sse/hub.go:60` — Unsubscribe 超时时 spawn 的 drain goroutine

**修改文件**: 分别修改上述三个文件

**修改内容**:

#### 3.2.1 — `query.go:1747`

将 `context.Background()` 替换为带超时的 context，并确保不泄漏：

```go
// 原始代码:
go q.cacheMetadataToRedis(context.Background(), redisClient, object.Key, metadata)

// 修改为:
go func() {
    cacheCtx, cacheCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cacheCancel()
    q.cacheMetadataToRedis(cacheCtx, redisClient, object.Key, metadata)
}()
```

#### 3.2.2 — `miniodb_service.go:332`

给 `MinIODBService` 添加 WaitGroup 追踪：

在 `MinIODBService` 结构体中添加字段（搜索结构体定义，通常在文件前部）：

```go
asyncWg sync.WaitGroup // 追踪异步 goroutine
```

修改 fire-and-forget 调用：

```go
// 原始代码:
go s.publishWriteEvent(ctx, tableName, req.Data)

// 修改为:
s.asyncWg.Add(1)
go func() {
    defer s.asyncWg.Done()
    s.publishWriteEvent(ctx, tableName, req.Data)
}()
```

在 `Close()` 或 `Shutdown()` 方法中等待：

```go
s.asyncWg.Wait()
```

#### 3.2.3 — `dashboard/sse/hub.go:60`

这个修复较复杂且风险低（仅超时场景触发），**可选择跳过或标注 TODO**。如果要修复：在 `Hub` 结构体中添加 `drainWg sync.WaitGroup`，在 spawn drain goroutine 时 `Add(1)`，在 goroutine 结束时 `Done()`，在 `Hub.Stop()` 中 `Wait()`。

**验证**:

```bash
go build ./internal/query/...
go build ./internal/service/...
go build ./internal/dashboard/...
```

---

### Task 3.3: 预编译正则表达式

**问题**: `internal/query/table_extractor.go:94-111` 的 `cleanSQL()` 每次调用时重新编译 5 个正则表达式。

**修改文件**: `internal/query/table_extractor.go`

**修改内容**: 将正则提升为包级变量（在文件顶部，函数外）：

```go
var (
    reSingleLineComment = regexp.MustCompile(`--.*$`)
    reMultiLineComment  = regexp.MustCompile(`/\*[\s\S]*?\*/`)
    reDoubleQuotedIdent = regexp.MustCompile(`"(\w+)"`)
    reBacktickIdent     = regexp.MustCompile("`(\\w+)`")
    reWhitespace        = regexp.MustCompile(`\s+`)
)
```

修改 `cleanSQL` 使用预编译变量：

```go
func (e *TableExtractor) cleanSQL(sql string) string {
    sql = reSingleLineComment.ReplaceAllString(sql, "")
    sql = reMultiLineComment.ReplaceAllString(sql, "")
    sql = reDoubleQuotedIdent.ReplaceAllString(sql, "$1")
    sql = reBacktickIdent.ReplaceAllString(sql, "$1")
    sql = strings.TrimSpace(sql)
    sql = reWhitespace.ReplaceAllString(sql, " ")
    return sql
}
```

**验证**:

```bash
go test ./internal/query/ -run TestTableExtractor -v -count=1
```

---

### Task 3.4: ColumnPruner 缓存添加 LRU 上限

**问题**: `internal/query/column_pruning.go:96-98` 的 `cp.cache` map 无限增长。

**修改文件**: `internal/query/column_pruning.go`

**修改内容**: 在 `ExtractRequiredColumns` 中，写缓存前检查 size 上限，超出时清空：

```go
// 在写入缓存之前（约第 96 行）:
cp.cacheMu.Lock()
if len(cp.cache) >= 10000 { // 最多缓存 10000 条
    cp.cache = make(map[string][]string)
}
cp.cache[sql] = requiredColumns
cp.cacheMu.Unlock()
```

或者采用更优雅的方案：使用 `container/list` 实现真正的 LRU（但考虑到这是低优先级问题，简单的 size 上限 + 全清策略已足够）。

**验证**:

```bash
go build ./internal/query/...
go test ./internal/query/ -run TestColumnPruning -v -count=1
```

---

### Phase 3 验证

```bash
go build ./...
go vet ./...
go test ./... -count=1 -timeout 120s
```

---

## Phase 4: 架构清理 — 死代码隔离

> **目标**: 将 55% 的存储层死代码隔离为 experimental build tag
> **风险**: 低 — 仅文件移动和 build tag 标记，不改动任何运行中的代码
> **预估工期**: 1-2 天
> **依赖**: 无

---

### Task 4.1: 隔离存储层死代码

**问题**: `internal/storage/` 中 5 个文件共 4,351 行从未在运行路径中调用。

**修改内容**:

1. 为以下文件添加 `//go:build experimental` 构建标签（在文件第 1 行，`package storage` 之前）：

| 文件 | 行数 | 操作 |
|------|------|------|
| `internal/storage/engine.go` | 910 | 添加 `//go:build experimental` |
| `internal/storage/index_system.go` | 1,160 | 添加 `//go:build experimental` |
| `internal/storage/memory.go` | 920 | 添加 `//go:build experimental` |
| `internal/storage/shard.go` | 1,021 | 添加 `//go:build experimental` |
| `internal/storage/storage.go` | ~340 | 见下方特殊处理 |

2. **`storage.go` 特殊处理**：此文件包含 `NewStorage()` 函数（活跃代码，第 25-28 行），其余为死代码 `StorageImpl`。将 `NewStorage()` 函数移至 `factory.go` 文件末尾，然后给 `storage.go` 整体添加 `//go:build experimental`。

3. 同时为这些文件的测试文件添加相同的 build tag：
   - `internal/storage/index_bloom_test.go` → 添加 `//go:build experimental`

4. 确认常规构建不再编译这些文件：

```bash
go build ./internal/storage/...
# 应成功，且不包含 experimental 文件
```

5. 确认带 tag 构建仍然通过：

```bash
go build -tags experimental ./internal/storage/...
```

**验证**:

```bash
go build ./...
go vet ./...
go test ./... -count=1 -timeout 120s
```

---

## Phase 5: cmd/main.go 重构

> **目标**: 将 438 行的 god function 拆分为清晰的模块化结构
> **风险**: 中高 — 大规模重构，需仔细保持初始化顺序
> **预估工期**: 3-5 天
> **依赖**: Phase 1, Phase 3 完成

---

### Task 5.1: 提取 App 结构体

**修改文件**: 新建 `cmd/app.go`，修改 `cmd/main.go`

**设计**: 将 `main()` 中的组件聚合为 `App` 结构体，初始化拆分为独立方法。

```go
// cmd/app.go

package main

type App struct {
    cfg    *config.Config
    logger *zap.Logger

    // Infrastructure
    storageInstance storage.Storage
    poolManager     *pool.PoolManager
    redisPool       *pool.RedisPool
    primaryMinio    *storage.MinioClientWrapper

    // Core Services
    buffer             *buffer.ConcurrentBuffer
    ingester           *ingest.Ingester
    querier            *query.Querier
    miniodbService     *service.MinIODBService
    metadataManager    *metadata.Manager

    // Coordination
    serviceRegistry *discovery.ServiceRegistry
    writeCoord      *coordinator.WriteCoordinator
    queryCoord      *coordinator.QueryCoordinator

    // Transport
    grpcServer  *grpcTransport.Server
    restServer  *restTransport.Server
    dashSrv     *dashboard.Server

    // Background Services
    replicator         *replication.Replicator
    backupScheduler    *backup.Scheduler
    compactionManager  *compaction.Manager
    subscriptionMgr    *subscription.Manager
    metricsServer      *http.Server

    // Lifecycle
    ctx     context.Context
    cancel  context.CancelFunc
    fatalCh chan error
}
```

**方法拆分**:

```go
func NewApp(cfg *config.Config, logger *zap.Logger) *App { ... }

func (a *App) InitStorage() error { ... }        // 原 main.go:168-194
func (a *App) InitServices() error { ... }       // 原 main.go:197-234
func (a *App) InitMetadata() error { ... }       // 原 main.go:241-277
func (a *App) InitMetrics() error { ... }        // 原 main.go:280-308
func (a *App) InitBackgroundServices() error { ... } // Compaction + Subscription
func (a *App) InitTransport() error { ... }      // gRPC + REST + Dashboard
func (a *App) InitReplication() error { ... }     // 原 initReplication
func (a *App) InitBackup() error { ... }         // 原 initBackupSubsystem
func (a *App) WireDashboard() { ... }            // 原 main.go:494-509 的 Set* 调用
func (a *App) Start() error { ... }              // 启动所有服务器 goroutine
func (a *App) WaitForShutdown() { ... }          // 原 waitForShutdown
func (a *App) Shutdown() { ... }                 // 有序关闭所有组件
```

**重构后的 `main()`**:

```go
func main() {
    // --hash-password 子命令处理（保持原样）

    cfg, err := config.LoadConfig(configPath)
    // ...

    logger.InitLogger(logCfg)
    defer logger.Close()

    app := NewApp(cfg, logger.Logger)
    defer app.Shutdown()

    if err := app.InitStorage(); err != nil {
        logger.Sugar.Fatalf("Storage init failed: %v", err)
    }
    if err := app.InitServices(); err != nil {
        logger.Sugar.Fatalf("Services init failed: %v", err)
    }
    if err := app.InitMetadata(); err != nil {
        logger.Sugar.Warnf("Metadata init degraded: %v", err)
    }
    if err := app.InitMetrics(); err != nil {
        logger.Sugar.Warnf("Metrics init failed: %v", err)
    }
    if err := app.InitBackgroundServices(); err != nil {
        logger.Sugar.Warnf("Background services partially failed: %v", err)
    }
    if err := app.InitTransport(); err != nil {
        logger.Sugar.Fatalf("Transport init failed: %v", err)
    }
    if err := app.InitReplication(); err != nil {
        logger.Sugar.Warnf("Replication init failed: %v", err)
    }
    if err := app.InitBackup(); err != nil {
        logger.Sugar.Warnf("Backup init failed: %v", err)
    }

    app.WireDashboard()
    app.Start()
    app.WaitForShutdown()
}
```

**关键约束**:
- **InitTransport 必须在 InitServices 之后** — Transport 依赖 MinIODBService
- **WireDashboard 必须在 InitReplication + InitBackup 之后** — Dashboard 需要 Replicator 和 BackupScheduler
- **保持原有的 `Fatalf` vs `Warnf` 语义** — 存储和传输层失败是 Fatal，其余是 Warn（降级运行）
- **每个 Init 方法返回 error**，由 `main()` 决定是 Fatal 还是 Warn

**验证**:

```bash
go build ./cmd/...
go vet ./cmd/...
# 启动测试（如果有集成测试环境）
```

---

## 任务依赖关系图

```
Phase 1 (Bug Fixes)
  ├── Task 1.1 (os.Exit)
  ├── Task 1.2 (Fatalf) ─── depends_on: Task 1.1
  ├── Task 1.3 (WaitGroup) ─ depends_on: Task 1.2
  ├── Task 1.4 (normalizeQuery)
  └── Task 1.5 (ColumnPruner SQL)

Phase 2 (Query Perf) ─── depends_on: Phase 1
  ├── Task 2.1 (DuckDB Pool)
  ├── Task 2.2 (View Cache) ── depends_on: Task 2.1
  ├── Task 2.3 (filepath collision)
  ├── Task 2.4 (temp cleanup)
  └── Task 2.5 (Deprecate Hybrid)

Phase 3 (Lifecycle) ─── depends_on: Phase 1
  ├── Task 3.1 (MetadataManager shutdown)
  ├── Task 3.2 (goroutine lifecycle)
  ├── Task 3.3 (regex precompile)
  └── Task 3.4 (ColumnPruner LRU)

Phase 4 (Dead Code) ─── no dependency
  └── Task 4.1 (experimental build tag)

Phase 5 (main.go refactor) ─── depends_on: Phase 1 + Phase 3
  └── Task 5.1 (App struct)
```

> Phase 2、Phase 3、Phase 4 之间无依赖，可并行执行。
> Phase 5 依赖 Phase 1（shutdown 修复）和 Phase 3（MetadataManager 停止方法），必须最后执行。

---

## 验收标准

| 标准 | 检查方式 |
|------|----------|
| `go build ./...` 通过 | CI 编译 |
| `go vet ./...` 无警告 | CI 静态检查 |
| 全部已有测试通过 | `go test ./... -count=1` |
| 无 `-race` 竞态告警 | `go test -race ./... -count=1` |
| shutdown 后连接池正确关闭 | 查看 shutdown 日志，确认 "Storage closed" 等输出 |
| 查询并发吞吐提升 | benchmark 对比（启用 DuckDB 连接池前后） |
| 临时文件不再无限累积 | 运行 30 分钟后检查 tempDir 大小 |

---

## 风险提示

| Phase | 风险 | 缓解措施 |
|-------|------|----------|
| Phase 2 Task 2.1 | DuckDB `:memory:` 模式下每个连接独立内存空间，View 不共享 | 当前代码已在同一次借用周期内创建 View + 执行查询，天然兼容 |
| Phase 2 Task 2.2 | View 缓存可能导致数据变更后查询到旧数据 | 写入路径的缓存失效机制（`QueryCache.InvalidateByTables`）需要同步清除 View 缓存 |
| Phase 4 Task 4.1 | experimental 文件中可能有被间接引用的类型 | build tag 添加后执行 `go build` 验证，逐个修复编译错误 |
| Phase 5 Task 5.1 | 大规模重构可能遗漏初始化顺序依赖 | 严格保持原有顺序，逐个方法提取并验证 |
