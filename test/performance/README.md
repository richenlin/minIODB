# MinIODB 性能测试

本目录包含MinIODB系统的性能测试工具和脚本，用于评估系统在不同负载下的性能表现。

## 文件说明

- `benchmark_test.go` - Go基准测试，包含各种性能测试场景
- `data_generator.go` - TB级测试数据生成器
- `run_performance_tests.sh` - 完整性能测试脚本
- `quick_test.sh` - 快速性能测试脚本
- `README.md` - 本说明文档

## 快速开始

### 1. 快速性能测试

快速测试使用10万条记录，适合验证系统基本功能和性能：

```bash
cd test/performance
./quick_test.sh
```

测试内容：
- 部署MinIODB单节点环境
- 生成10万条测试记录
- 执行基本查询测试
- 收集系统资源使用情况
- 生成测试报告

### 2. 完整性能测试

完整测试包含多种基准测试场景，生成详细的性能报告：

```bash
cd test/performance
./run_performance_tests.sh
```

测试内容：
- 写入吞吐量测试
- 查询性能测试
- 大数据集查询测试
- 并发混合负载测试
- 系统资源监控
- 性能瓶颈分析

### 3. 自定义数据生成

使用数据生成器创建大规模测试数据：

```bash
# 编译数据生成器
go build -o data_generator data_generator.go

# 生成100万条记录
./data_generator -records=1000000 -batch=1000 -concurrency=10

# 生成TB级数据（10亿条记录）
./data_generator -records=1000000000 -batch=5000 -concurrency=20
```

### 4. Go基准测试

运行Go基准测试：

```bash
# 运行所有基准测试
go test -bench=. -benchtime=30s -timeout=1h

# 运行特定测试
go test -bench=BenchmarkWriteThroughput -benchtime=10s
go test -bench=BenchmarkQueryPerformance -benchtime=10s
```

## 测试配置

### 环境要求

- Docker 和 Docker Compose
- Go 1.19+
- 至少 8GB 内存
- 至少 50GB 可用磁盘空间（TB级测试需要更多）

### 配置参数

#### 数据生成器参数

```bash
./data_generator [选项]
```

选项：
- `-url` - MinIODB服务器URL（默认：http://localhost:8081）
- `-records` - 总记录数（默认：1000000）
- `-batch` - 批次大小（默认：1000）
- `-concurrency` - 并发数（默认：10）
- `-table` - 表名（默认：performance_test）
- `-v` - 详细输出

#### 系统配置优化

编辑 `deploy/docker/.env` 文件优化性能：

```bash
# 缓冲区配置
BUFFER_SIZE=50000           # 增大缓冲区
BUFFER_TIMEOUT=5s          # 调整刷新间隔
BATCH_SIZE=5000            # 增大批次大小

# DuckDB配置
DUCKDB_MEMORY_LIMIT=8GB    # 增加内存限制
DUCKDB_THREADS=8           # 增加线程数

# Redis配置
REDIS_POOL_SIZE=50         # 增加连接池大小
REDIS_MAX_IDLE=10          # 最大空闲连接

# MinIO配置
MINIO_POOL_SIZE=20         # 增加连接池大小
```

## 测试场景

### 1. 写入性能测试

测试不同并发级别下的写入吞吐量：

- 单线程写入
- 5并发写入
- 10并发写入
- 20并发写入

指标：
- 写入吞吐量（records/sec）
- 写入延迟（ms/write）
- 数据传输速度（MB/sec）

### 2. 查询性能测试

测试各种SQL查询的执行时间：

- 简单查询（COUNT, SUM, AVG）
- 复杂聚合查询（GROUP BY, ORDER BY）
- 范围查询（WHERE条件）
- 连接查询（JOIN）

指标：
- 查询延迟（ms/query）
- 查询吞吐量（queries/sec）
- 数据扫描量（MB）

### 3. 大数据集测试

测试TB级数据的查询性能：

- 全表扫描
- 索引查询
- 分组聚合
- 排序查询

目标性能：
- 查询延迟 < 1秒（简单查询）
- 查询延迟 < 10秒（复杂聚合）
- 扫描速度 > 100MB/s

### 4. 混合负载测试

同时进行读写操作：

- 70%读 + 30%写
- 50%读 + 50%写
- 30%读 + 70%写

指标：
- 整体吞吐量
- 读写延迟分布
- 系统资源使用率

## 性能基准

### 硬件配置

推荐测试环境：
- CPU: 8核心以上
- 内存: 16GB以上
- 存储: SSD，1TB以上
- 网络: 千兆以太网

### 性能目标

#### 写入性能
- 单节点写入：> 10,000 records/sec
- 批量写入：> 50,000 records/sec
- 写入延迟：< 10ms (P99)

#### 查询性能
- 简单查询：< 100ms
- 聚合查询：< 1s
- 复杂查询：< 10s
- 扫描速度：> 100MB/s

#### 系统资源
- CPU使用率：< 80%
- 内存使用率：< 85%
- 磁盘I/O：< 80%利用率

## 故障排除

### 常见问题

1. **服务启动失败**
   ```bash
   # 检查Docker服务状态
   docker-compose -f ../../deploy/docker/docker-compose.yml ps
   
   # 查看服务日志
   docker-compose -f ../../deploy/docker/docker-compose.yml logs miniodb
   ```

2. **写入性能低**
   - 增大缓冲区大小（BUFFER_SIZE）
   - 调整批次大小（BATCH_SIZE）
   - 增加并发数
   - 检查磁盘I/O性能

3. **查询性能差**
   - 增加DuckDB内存限制
   - 优化查询语句
   - 检查数据分布
   - 考虑添加索引

4. **内存不足**
   - 减少缓冲区大小
   - 降低并发数
   - 增加系统内存
   - 优化数据结构

### 监控和调试

1. **系统监控**
   ```bash
   # 实时监控系统资源
   htop
   iotop
   nethogs
   
   # 监控Docker容器
   docker stats
   ```

2. **应用监控**
   ```bash
   # 健康检查
   curl http://localhost:8081/v1/health
   
   # 应用统计
   curl http://localhost:8081/v1/stats
   
   # Prometheus指标
   curl http://localhost:9090/metrics
   ```

3. **日志分析**
   ```bash
   # 查看应用日志
   docker-compose -f ../../deploy/docker/docker-compose.yml logs -f miniodb
   
   # 查看Redis日志
   docker-compose -f ../../deploy/docker/docker-compose.yml logs -f redis
   
   # 查看MinIO日志
   docker-compose -f ../../deploy/docker/docker-compose.yml logs -f minio
   ```

## 扩展测试

### 多节点测试

部署多节点集群进行分布式性能测试：

1. 修改Docker Compose配置
2. 配置一致性哈希
3. 测试数据分片
4. 验证故障转移

### 长时间稳定性测试

运行长时间测试验证系统稳定性：

```bash
# 运行24小时稳定性测试
./data_generator -records=100000000 -batch=1000 -concurrency=5
```

### 压力测试

测试系统极限性能：

```bash
# 极限写入测试
./data_generator -records=1000000000 -batch=10000 -concurrency=50

# 极限查询测试
go test -bench=BenchmarkConcurrentMixedWorkload -benchtime=60s
```

## 报告分析

性能测试完成后，分析生成的报告：

1. **吞吐量分析** - 比较不同场景下的处理能力
2. **延迟分析** - 关注P50, P95, P99延迟指标
3. **资源使用** - 监控CPU、内存、磁盘、网络使用情况
4. **瓶颈识别** - 找出性能限制因素
5. **优化建议** - 基于测试结果提出改进方案

## 参考资料

- [MinIODB架构文档](../../SOLUTION.md)
- [部署指南](../../deploy/README.md)
- [Docker配置](../../deploy/docker/README.md)
- [API文档](../../docs/api.md) 