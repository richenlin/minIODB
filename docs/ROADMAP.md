# MinIODB 技术改进路线图

> 基于 2026-01-17 代码审计结果制定

## 执行摘要

MinIODB 当前处于 **MVP (Minimum Viable Product)** 阶段。具备分布式系统骨架，但缺乏企业级生产环境必须的核心组件。

### 现状评估

| 维度 | 评分 | 说明 |
|------|------|------|
| 功能完整性 | 40% | 缺少 WAL、真实 Parquet 引擎、复杂查询支持 |
| 性能 | 50% | 有缓存设计，但缺乏 Compaction 导致 IO 瓶颈 |
| 稳定性 | 60% | 有重试和熔断，但数据不持久化 |

### 关键问题

1. **数据丢失风险**: Buffer 无 WAL，节点崩溃时内存数据丢失
2. **存储碎片化**: 无 Compaction，MinIO 上大量小文件影响查询性能
3. **查询能力有限**: 仅支持简单聚合，不支持跨节点 JOIN/GROUP BY
4. **Parquet 引擎为 Mock**: `internal/storage/parquet.go` 是模拟器，无真实编码

---

## Phase 1: 数据安全与持久化

**优先级**: Critical  
**预计工期**: 2-3 周  
**目标**: 确保数据零丢失

### F012: 实现 Write Ahead Log (WAL)

**问题分析**:
```go
// internal/buffer/manager.go - 当前实现
func (m *Manager) Add(id string, data []byte, timestamp time.Time) bool {
    m.mutex.Lock()
    defer m.mutex.Unlock()
    // 直接写入内存，无持久化
    m.buffer = append(m.buffer, DataPoint{...})
    return true
}
```

如果节点在 `Flush()` 前崩溃，`m.buffer` 中的数据永久丢失。

**解决方案**:

```
写入流程:
Client -> WAL (fsync) -> Memory Buffer -> Flush -> MinIO
                |
                v
         节点重启时重放
```

**技术设计**:

1. **WAL 文件格式**:
```
+----------+----------+----------+----------+
| Length   | CRC32    | Type     | Payload  |
| (4 bytes)| (4 bytes)| (1 byte) | (N bytes)|
+----------+----------+----------+----------+
```

2. **核心接口**:
```go
type WAL struct {
    file       *os.File
    mu         sync.Mutex
    lastSeqNum uint64
}

func (w *WAL) Append(data []byte) (seqNum uint64, err error)
func (w *WAL) Replay(handler func(seqNum uint64, data []byte) error) error
func (w *WAL) Truncate(beforeSeqNum uint64) error
```

3. **集成点**:
- `buffer.Manager.Add()` 先写 WAL，再写内存
- `buffer.Manager.Flush()` 成功后截断 WAL
- 服务启动时调用 `WAL.Replay()` 恢复数据

**文件变更**:
- 新增 `internal/wal/wal.go`
- 新增 `internal/wal/wal_test.go`
- 修改 `internal/buffer/manager.go`

---

### F013: 实现真实 Parquet 写入引擎

**问题分析**:
```go
// internal/storage/parquet.go - 当前是 Mock
type MockParquetWriter struct {
    FilePath            string
    PartitionStrategy   *PartitionStrategy
    CompressionStrategy *CompressionStrategy
}
// 没有实际的 Parquet 编码逻辑
```

**解决方案**:

使用 `github.com/xitongsys/parquet-go` 实现真实的 Parquet 编码。

**技术设计**:

1. **核心接口**:
```go
type ParquetWriter interface {
    Write(records []map[string]interface{}) error
    Close() error
    GetStats() *WriteStats
}

type ParquetWriterImpl struct {
    writer     *writer.ParquetWriter
    schema     *parquet.SchemaHandler
    file       source.ParquetFile
    strategy   *PartitionStrategy
    compression parquet.CompressionCodec
}
```

2. **动态 Schema 推断**:
```go
func InferSchema(sample map[string]interface{}) string {
    // 根据 JSON 数据推断 Parquet schema
    // 支持: string, int64, float64, bool, timestamp
}
```

3. **压缩策略应用**:
```go
func (pw *ParquetWriterImpl) applyCompression() {
    switch pw.strategy.CompressionType {
    case "snappy":
        pw.writer.CompressionType = parquet.CompressionCodec_SNAPPY
    case "zstd":
        pw.writer.CompressionType = parquet.CompressionCodec_ZSTD
    // ...
    }
}
```

**文件变更**:
- 重构 `internal/storage/parquet.go`
- 新增 `internal/storage/parquet_writer.go`
- 新增 `internal/storage/parquet_reader.go`
- 新增 `internal/storage/schema_inference.go`

---

## Phase 2: 存储引擎优化

**优先级**: High  
**预计工期**: 2-3 周  
**目标**: 解决小文件问题，提升查询性能

### F014: 异步 Compaction (文件合并)

**问题分析**:

每次 Buffer Flush 生成一个 Parquet 文件。高频写入场景下，MinIO 上会产生大量小文件:
```
bucket/
  table_a/
    2024-01-15/
      file_001.parquet (2MB)
      file_002.parquet (1MB)
      file_003.parquet (3MB)
      ... (数百个小文件)
```

DuckDB 查询时需要扫描所有文件，性能极差。

**解决方案**:

后台 Compaction 进程，将小文件合并为大文件:

```
合并前:                          合并后:
file_001.parquet (2MB)   -->    file_merged_001.parquet (128MB)
file_002.parquet (1MB)   
file_003.parquet (3MB)   
...
file_050.parquet (4MB)
```

**技术设计**:

1. **Compaction Manager**:
```go
type CompactionManager struct {
    minioClient  *minio.Client
    redisClient  *redis.Client
    targetSize   int64 // 目标文件大小 (e.g., 128MB)
    minFileCount int   // 触发合并的最小文件数
    interval     time.Duration
}

func (cm *CompactionManager) Start(ctx context.Context)
func (cm *CompactionManager) CompactTable(table string) error
func (cm *CompactionManager) CompactPartition(table, partition string) error
```

2. **合并策略**:
```go
type CompactionStrategy struct {
    TargetFileSize    int64         // 目标文件大小
    MinFilesToCompact int           // 最小合并文件数
    MaxFilesToCompact int           // 最大合并文件数
    CooldownPeriod    time.Duration // 文件冷却期 (避免合并正在写入的文件)
}
```

3. **合并流程**:
```
1. 扫描分区目录，找出小文件
2. 按时间范围分组
3. 下载文件到本地
4. 使用 DuckDB 合并: COPY (SELECT * FROM read_parquet(['f1','f2'...])) TO 'merged.parquet'
5. 上传合并后的文件
6. 更新 Redis 元数据
7. 删除原始小文件
```

4. **元数据更新 (原子性)**:
```go
func (cm *CompactionManager) atomicSwap(oldFiles []string, newFile string) error {
    // 使用 Redis 事务确保原子性
    pipe := cm.redisClient.TxPipeline()
    for _, old := range oldFiles {
        pipe.Del(ctx, "file_index:"+old)
    }
    pipe.HSet(ctx, "file_index:"+newFile, ...)
    _, err := pipe.Exec(ctx)
    return err
}
```

**文件变更**:
- 新增 `internal/compaction/manager.go`
- 新增 `internal/compaction/strategy.go`
- 新增 `internal/compaction/worker.go`
- 修改 `internal/service/miniodb_service.go` (启动 compaction)

---

### F015: 列存储优化与谓词下推

**问题分析**:

当前查询流程:
```
1. 从 MinIO 下载整个 Parquet 文件
2. DuckDB 加载全部数据
3. 执行 WHERE 过滤
```

对于 `SELECT * FROM logs WHERE timestamp > '2024-01-15'`，如果文件包含 2024-01-01 到 2024-01-31 的数据，只有一半是需要的，但会下载和解析全部数据。

**解决方案**:

1. **利用 Parquet 元数据**:
   - 每个 Row Group 包含 min/max 统计
   - 可以跳过不符合条件的 Row Group

2. **利用 DuckDB 的 Pushdown**:
   - DuckDB 原生支持 Parquet 谓词下推
   - 需要正确传递 WHERE 条件

**技术设计**:

1. **增强元数据索引**:
```go
type FileIndex struct {
    FilePath    string
    RowGroups   []RowGroupMeta
    MinValues   map[string]interface{} // 列级最小值
    MaxValues   map[string]interface{} // 列级最大值
    BloomFilter map[string][]byte      // 列级布隆过滤器
}

type RowGroupMeta struct {
    RowCount  int64
    MinValues map[string]interface{}
    MaxValues map[string]interface{}
}
```

2. **文件剪枝 (File Pruning)**:
```go
func (qe *QueryEngine) pruneFiles(files []FileIndex, predicates []Predicate) []FileIndex {
    var result []FileIndex
    for _, f := range files {
        if f.MayContain(predicates) {
            result = append(result, f)
        }
    }
    return result
}
```

3. **集成 DuckDB 谓词下推**:
```sql
-- DuckDB 自动下推 WHERE 条件到 Parquet Reader
SELECT * FROM read_parquet('s3://bucket/file.parquet')
WHERE timestamp > '2024-01-15'
```

**文件变更**:
- 修改 `internal/storage/parquet.go` (提取元数据)
- 新增 `internal/query/file_pruning.go`
- 修改 `internal/query/query.go` (集成剪枝)

---

## Phase 3: 查询引擎增强

**优先级**: Medium  
**预计工期**: 3-4 周  
**目标**: 支持复杂分析查询

### F016: 分布式聚合优化

**问题分析**:

当前实现 (`internal/coordinator/coordinator.go`):
```go
func (qc *QueryCoordinator) mergeResults(results []string, sql string) (string, error) {
    if strings.Contains(lowerSQL, "count(") {
        return qc.aggregateCountResults(results) // 简单求和
    }
    if strings.Contains(lowerSQL, "sum(") {
        return qc.aggregateSumResults(results) // 简单求和
    }
    return qc.unionResults(results) // 简单合并
}
```

**不支持的场景**:
- `AVG()`: 需要 SUM + COUNT
- `GROUP BY`: 需要 Map-Reduce
- `ORDER BY ... LIMIT`: 需要 Top-N 合并
- `JOIN`: 需要 Shuffle 或 Broadcast

**解决方案**:

实现 Map-Reduce 风格的分布式聚合:

```
          Query: SELECT region, SUM(amount) FROM orders GROUP BY region

节点A:                    节点B:                    节点C:
┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
│ region | sum    │      │ region | sum    │      │ region | sum    │
│ EAST   | 1000   │      │ EAST   | 500    │      │ WEST   | 800    │
│ WEST   | 2000   │      │ WEST   | 1500   │      │ NORTH  | 600    │
└─────────────────┘      └─────────────────┘      └─────────────────┘
         │                        │                        │
         └────────────────────────┼────────────────────────┘
                                  │
                                  v
                         ┌───────────────────┐
                         │ Coordinator Merge │
                         │ GROUP BY region   │
                         │ SUM(sum)          │
                         └───────────────────┘
                                  │
                                  v
                         ┌───────────────────┐
                         │ region | total    │
                         │ EAST   | 1500     │
                         │ WEST   | 4300     │
                         │ NORTH  | 600      │
                         └───────────────────┘
```

**技术设计**:

1. **查询分析器**:
```go
type QueryAnalyzer struct {}

func (qa *QueryAnalyzer) Analyze(sql string) (*QueryPlan, error) {
    // 解析 SQL，识别聚合函数和 GROUP BY
}

type QueryPlan struct {
    Type          QueryType // SIMPLE, AGGREGATE, JOIN
    Aggregations  []Aggregation
    GroupByKeys   []string
    OrderByKeys   []OrderBy
    Limit         int
}
```

2. **聚合策略**:
```go
type AggregationStrategy interface {
    // Map 阶段: 每个节点执行的 SQL
    MapSQL(originalSQL string) string
    // Reduce 阶段: 合并结果
    Reduce(results [][]map[string]interface{}) ([]map[string]interface{}, error)
}

// SUM 策略
type SumStrategy struct {}
func (s *SumStrategy) MapSQL(sql string) string { return sql }
func (s *SumStrategy) Reduce(results ...) { /* 按 key 合并求和 */ }

// AVG 策略 (需要转换为 SUM + COUNT)
type AvgStrategy struct {}
func (a *AvgStrategy) MapSQL(sql string) string {
    // SELECT AVG(x) -> SELECT SUM(x) as sum_x, COUNT(x) as count_x
}
func (a *AvgStrategy) Reduce(results ...) {
    // total_sum / total_count
}
```

3. **Top-N 合并**:
```go
func mergeTopN(results [][]Row, orderBy []OrderBy, limit int) []Row {
    // 使用堆合并多个有序结果
}
```

**文件变更**:
- 新增 `internal/coordinator/query_analyzer.go`
- 新增 `internal/coordinator/aggregation_strategy.go`
- 重构 `internal/coordinator/coordinator.go`

---

## 实施计划

### 里程碑

| 阶段 | 任务 | 工期 | 交付物 |
|------|------|------|--------|
| Phase 1.1 | F012: WAL 实现 | 1 周 | `internal/wal/` |
| Phase 1.2 | F013: Parquet 引擎 | 1.5 周 | `internal/storage/parquet_*.go` |
| Phase 2.1 | F014: Compaction | 2 周 | `internal/compaction/` |
| Phase 2.2 | F015: 谓词下推 | 1 周 | `internal/query/file_pruning.go` |
| Phase 3 | F016: 分布式聚合 | 3 周 | `internal/coordinator/` 重构 |

### 优先级排序

```
Critical (数据安全):
  F012 WAL ──────────> F013 Parquet 引擎
                              │
High (性能):                  v
  F014 Compaction <───────────┘
        │
        v
  F015 谓词下推
        │
Medium (功能):               v
  F016 分布式聚合 <───────────┘
```

### 测试策略

1. **单元测试**: 每个新组件 > 80% 覆盖率
2. **集成测试**: 端到端数据写入-查询流程
3. **故障注入测试**: 模拟节点崩溃，验证 WAL 恢复
4. **性能基准测试**: 
   - Compaction 前后查询性能对比
   - 百万级数据写入吞吐量

---

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| WAL 实现复杂度 | 可能引入新 bug | 参考成熟实现 (etcd/wal) |
| Compaction 期间服务不可用 | 查询失败 | 实现在线 Compaction，不锁定读取 |
| 分布式聚合内存溢出 | OOM | 实现流式聚合，限制内存使用 |
| Schema 推断不准确 | 数据类型错误 | 支持显式 Schema 定义 |

---

## 附录

### 参考资料

- [Parquet Format Specification](https://parquet.apache.org/docs/file-format/)
- [DuckDB Parquet Integration](https://duckdb.org/docs/data/parquet/overview)
- [Write Ahead Log Design (etcd)](https://etcd.io/docs/v3.5/learning/data_model/)
- [Compaction in LSM-Tree (RocksDB)](https://github.com/facebook/rocksdb/wiki/Compaction)

### 代码审计记录

审计日期: 2026-01-17

关键文件:
- `internal/buffer/manager.go`: 无 WAL，数据丢失风险
- `internal/storage/parquet.go`: Mock 实现，无真实编码
- `internal/coordinator/coordinator.go`: 仅支持简单聚合
- `internal/query/file_cache.go`: LRU 缓存实现良好
