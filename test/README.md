# MinIODB 集成测试

这个目录包含了MinIODB的集成测试套件，用于验证整个系统的功能完整性。

## 测试架构

集成测试基于Docker Compose环境，包含以下组件：

- **MinIODB**: 主服务，提供gRPC和REST API
- **Redis**: 分布式缓存和服务注册中心
- **MinIO**: 主要对象存储
- **MinIO Backup**: 备份对象存储

## 测试套件

### 1. 数据库操作测试 (database)
- 数据写入测试
- 数据查询测试
- 数据更新测试
- 数据删除测试
- 流式写入测试
- 流式查询测试

### 2. 表管理测试 (table)
- 创建表测试
- 列出表测试
- 获取表信息测试
- 删除表测试

### 3. 元数据管理测试 (metadata)
- 备份元数据测试
- 恢复元数据测试
- 列出备份测试
- 获取元数据状态测试

### 4. 服务注册发现测试 (discovery)
- 健康检查测试
- 获取服务状态测试
- 获取监控指标测试
- Redis连接测试

### 5. 备份恢复测试 (backup)
- MinIO连接测试
- 备份MinIO连接测试
- 数据备份流程测试
- 数据恢复流程测试

### 6. 节点扩容测试 (scaling)
- 单节点状态测试
- 负载均衡检查测试
- 容量检查测试

## 额外测试组件

### 负载测试 (load_test.sh)
高并发场景下的性能测试：
- 并发用户模拟
- 响应时间监控
- 吞吐量测试
- 查询性能测试
- HTML测试报告生成

### 混沌工程测试 (chaos_test.sh)
故障注入和容错能力测试：
- 容器故障模拟
- 网络分区测试
- 资源限制测试
- 依赖服务故障
- 多重故障场景

### 完整测试套件 (run_all_tests.sh)
运行所有测试类型的总脚本：
- 集成测试 → 负载测试 → 混沌测试
- 灵活的测试组合选项
- 错误恢复和重试机制
- 完整的测试报告

## 使用方法

### 前置要求

确保系统已安装以下工具：

```bash
# 必需依赖
curl
jq
docker
docker-compose

# 可选依赖 (用于gRPC测试)
grpcurl
```

### 运行所有测试

```bash
cd test
chmod +x integration_test.sh
./integration_test.sh
```

### 运行特定测试套件

```bash
# 仅运行数据库操作测试
./integration_test.sh database

# 仅运行表管理测试
./integration_test.sh table

# 仅运行元数据管理测试
./integration_test.sh metadata

# 仅运行服务注册发现测试
./integration_test.sh discovery

# 仅运行备份恢复测试
./integration_test.sh backup

# 仅运行节点扩容测试
./integration_test.sh scaling
```

### 测试选项

```bash
# 跳过环境设置 (如果服务已在运行)
./integration_test.sh --no-setup

# 跳过清理 (保留测试环境用于调试)
./integration_test.sh --no-cleanup

# 详细输出模式
./integration_test.sh --verbose

# 快速测试模式
./integration_test.sh --quick

# 组合使用
./integration_test.sh --no-cleanup database
```

### 运行负载测试

```bash
# 默认负载测试 (10用户，每人100请求)
./load_test.sh

# 自定义负载参数
./load_test.sh --users 20 --requests 200 --duration 300

# 高负载测试
./load_test.sh --users 50 --requests 500 --rampup 30
```

### 运行混沌测试

```bash
# 运行所有混沌测试
./chaos_test.sh

# 仅运行容器故障测试
./chaos_test.sh container

# 仅运行网络故障测试
./chaos_test.sh network

# 自定义测试时间
./chaos_test.sh --duration 600 --recovery 120
```

### 运行完整测试套件

```bash
# 运行所有测试
./run_all_tests.sh

# 仅运行集成测试
./run_all_tests.sh --integration-only

# 跳过混沌测试
./run_all_tests.sh --skip-chaos

# 出错时继续运行
./run_all_tests.sh --continue-on-error
```

## 测试端点

测试期间会使用以下服务端点：

- **MinIODB REST API**: http://localhost:8081
- **MinIODB gRPC API**: localhost:8080
- **MinIODB监控**: http://localhost:9090/metrics
- **Redis**: localhost:6379
- **MinIO主存储**: http://localhost:9000
- **MinIO控制台**: http://localhost:9001
- **MinIO备份存储**: http://localhost:9002
- **MinIO备份控制台**: http://localhost:9003

## 测试数据

测试会创建以下测试数据：

- 表名: `test_table`, `integration_test_table`, `backup_test`
- 测试用户: `user001`, `batch001`, `backup001`
- 测试负载: JSON格式的用户数据

所有测试数据在测试完成后会被自动清理。

## 测试结果

测试运行过程中会生成各种报告和日志：

### 集成测试结果
- 实时控制台输出
- 测试通过/失败统计
- 详细的错误信息

### 负载测试结果
- `results/` 目录下的HTML报告
- 性能指标JSON文件
- 响应时间和吞吐量统计
- 系统监控数据

### 混沌测试结果
- 故障恢复时间记录
- 系统韧性评估
- 详细的故障模拟报告

### 查看测试结果

```bash
# 查看最新的负载测试报告
ls results/load_test_report_*.html

# 在浏览器中打开报告
open results/load_test_report_*.html

# 查看测试日志
docker-compose -f docker-compose.test.yml logs
```

## 常见问题

### 1. 端口冲突

如果遇到端口冲突，请确保以下端口未被占用：
- 8080 (gRPC)
- 8081 (REST API)
- 9090 (Metrics)
- 6379 (Redis)
- 9000-9003 (MinIO)

### 2. 内存不足

测试环境需要至少2GB可用内存。如果内存不足，可以：
- 关闭其他应用程序
- 使用`--quick`模式运行简化测试

### 3. 服务启动失败

如果服务启动失败，请检查：
- Docker守护进程是否运行
- 网络连接是否正常
- 磁盘空间是否充足

### 4. 测试超时

默认等待时间为5分钟，如果网络较慢可能需要：
- 确保网络连接稳定
- 检查Docker镜像是否需要重新下载

## 调试技巧

### 查看服务日志

```bash
# 查看所有服务日志
docker-compose -f test/docker-compose.test.yml logs

# 查看特定服务日志
docker-compose -f test/docker-compose.test.yml logs miniodb
docker-compose -f test/docker-compose.test.yml logs redis
docker-compose -f test/docker-compose.test.yml logs minio
```

### 手动测试API

```bash
# 健康检查
curl http://localhost:8081/v1/health

# 写入数据
curl -X POST http://localhost:8081/v1/data \
  -H "Content-Type: application/json" \
  -d '{"table":"debug_table","id":"debug001","payload":{"test":"data"}}'

# 查询数据
curl -X POST http://localhost:8081/v1/query \
  -H "Content-Type: application/json" \
  -d '{"sql":"SELECT * FROM debug_table"}'
```

### 保留环境进行调试

```bash
# 运行测试但不清理环境
./integration_test.sh --no-cleanup

# 稍后手动清理
docker-compose -f test/docker-compose.test.yml down -v
```

## 持续集成

这个测试套件设计用于CI/CD流水线：

```yaml
# GitHub Actions 示例
- name: Run Integration Tests
  run: |
    cd test
    chmod +x integration_test.sh
    ./integration_test.sh --verbose
```

## 性能测试

如需运行性能测试，请参考 `test/performance/` 目录下的专门测试套件。

## 贡献指南

添加新测试时请遵循以下规范：

1. 测试函数命名: `test_*`
2. 使用`record_test`记录测试结果
3. 确保测试数据在完成后被清理
4. 添加适当的错误处理和超时机制
5. 更新此README文档 