# MinIODB 部署配置更新说明

## 📅 更新日期

2025-10-02

## 🎯 更新概要

本次更新主要完成了以下工作：

### 1. 配置文件更新

#### ✅ 已完成

- **项目根目录** `/config.yaml`
  - ✅ 已包含所有新特性配置（网络、限流、查询优化、存储引擎等）
  - ✅ 适用于开发环境和本地测试
  
- **Docker 部署配置**
  - ✅ `/deploy/docker/config/config.yaml` - 分布式模式完整配置
  - ✅ `/deploy/docker/config/config.single.yaml` - 单节点模式配置（新增）
  
- **Kubernetes 部署配置**
  - ✅ `/deploy/k8s/config.yaml` - 分布式模式 ConfigMap（新增）
  - ✅ `/deploy/k8s/config.single.yaml` - 单节点模式 ConfigMap（新增）
  
- **Ansible 部署配置**
  - ✅ `/deploy/ansible/group_vars/distributed.yml` - 分布式模式变量（新增）
  - ✅ `/deploy/ansible/group_vars/single_node.yml` - 单节点模式变量（新增）

### 2. Dockerfile 优化

#### ✅ 已完成

- ✅ 移动 `Dockerfile` 到 `/deploy/` 目录
- ✅ 移动 `Dockerfile.arm` 到 `/deploy/` 目录
- ✅ 修正构建路径：`cmd/main.go` → `cmd/server/main.go`
- ✅ 更新 docker-compose.yml 中的 dockerfile 路径引用

**⚠️ 注意：** 根目录的 `Dockerfile` 和 `Dockerfile.arm` 文件仍然存在，可根据需要删除。

### 3. 部署脚本完善

#### ✅ Docker Compose

- ✅ `docker-compose.yml` - 分布式模式（Redis + MinIO + MinIODB + 备份MinIO）
- ✅ `docker-compose.single.yml` - 单节点模式（MinIO + MinIODB）
- ✅ 更新 dockerfile 路径为 `deploy/Dockerfile`

#### ✅ Kubernetes

- ✅ 新增完整配置 ConfigMap
- ✅ 新增单节点配置 ConfigMap
- ✅ 保留现有 Deployment 和 Service 配置

#### ✅ Ansible

- ✅ 新增分布式模式变量文件
- ✅ 新增单节点模式变量文件
- ✅ 保留现有 Playbook 和 Role 结构

### 4. 文档更新

#### ✅ 已完成

- ✅ `/deploy/DEPLOYMENT_GUIDE.md` - 全新的部署指南（新增）
  - 详细的配置说明
  - 单节点 vs 分布式模式对比
  - 3种部署方式的完整步骤
  - 配置调优建议
  - 故障排查指南

## 🔄 部署模式对比

### 配置差异总结

| 配置项 | 单节点模式 | 分布式模式 |
|--------|-----------|-----------|
| **Redis** | `enabled: false` | `enabled: true` |
| **查询缓存** | `query_cache.enabled: false` | `query_cache.enabled: true` |
| **文件缓存 Redis 索引** | `redis_index.enabled: false` | `redis_index.enabled: true` |
| **备份 MinIO** | `backup.enabled: false` | `backup.enabled: true` |
| **DuckDB 池大小** | `pool_size: 3` | `pool_size: 5` |
| **DuckDB 内存** | `memory_limit: "512MB"` | `memory_limit: "1GB"` |
| **DuckDB 线程** | `threads: 2` | `threads: 4` |
| **存储引擎优化间隔** | `optimize_interval: 3600s` | `optimize_interval: 1800s` |
| **Parquet 压缩** | `default_compression: "snappy"` | `default_compression: "zstd"` |
| **压缩分析** | `compression_analysis: false` | `compression_analysis: true` |
| **自动重平衡** | `auto_rebalance: false` | `auto_rebalance: true` |
| **冷热分离** | `hot_cold_separation: false` | `hot_cold_separation: true` |
| **索引类型** | `["bloom", "minmax"]` | 全部类型 |
| **索引维护间隔** | `maintenance_interval: 7200s` | `maintenance_interval: 3600s` |
| **最大内存使用** | `max_memory_usage: 1GB` | `max_memory_usage: 2GB` |
| **最大表数量** | `max_tables: 100` | `max_tables: 1000` |
| **数据保留天数** | `retention_days: 90` | `retention_days: 365` |

## 📋 新增配置项说明

### 1. 网络和连接池配置 (`network`)

统一管理所有网络连接配置：

- **Server 配置**：gRPC 和 REST 服务器网络参数
- **连接池配置**：Redis、MinIO、备份 MinIO 的连接池参数
- **健康检查间隔**：统一的健康检查策略

### 2. 智能限流系统 (`rate_limiting`)

分层限流策略：

- **健康检查层** (`health`): 200 RPS，高优先级
- **查询层** (`query`): 100 RPS，中等优先级
- **写入层** (`write`): 80 RPS，适中优先级
- **标准层** (`standard`): 50 RPS，默认
- **严格层** (`strict`): 20 RPS，敏感操作

### 3. 查询性能优化 (`query_optimization`)

三层缓存策略：

- **查询缓存**：基于 Redis 的查询结果缓存（仅分布式模式）
- **文件缓存**：本地磁盘的 Parquet 文件缓存
- **DuckDB 连接池**：连接复用和预编译语句

### 4. 存储引擎优化 (`storage_engine`)

四大优化系统：

- **Parquet 优化**：压缩算法、分区策略
- **智能分片**：负载均衡、冷热分离
- **索引系统**：Bloom Filter、MinMax、倒排索引等
- **内存优化**：内存池、零拷贝、GC 优化

### 5. 表管理配置 (`table_management`)

精细化表级控制：

- **表名验证**：正则表达式模式匹配
- **最大表数量**：防止资源滥用
- **默认表配置**：缓冲区大小、刷新间隔、保留天数等
- **表级属性**：自定义 key-value 属性

## 🚀 快速开始

### Docker Compose 单节点模式

```bash
cd deploy/docker
docker-compose -f docker-compose.single.yml up -d
```

### Docker Compose 分布式模式

```bash
cd deploy/docker
docker-compose -f docker-compose.yml up -d
```

### Kubernetes 单节点模式

```bash
cd deploy/k8s
kubectl apply -f namespace.yaml
kubectl apply -f config.single.yaml
kubectl apply -f minio/minio-single.yaml
kubectl apply -f miniodb/miniodb-single.yaml
```

### Kubernetes 分布式模式

```bash
cd deploy/k8s
kubectl apply -f namespace.yaml
kubectl apply -f config.yaml
kubectl apply -f redis/
kubectl apply -f minio/
kubectl apply -f init-storage/
kubectl apply -f miniodb/
```

### Ansible 单节点模式

```bash
cd deploy/ansible
ansible-playbook -i inventory/single-node.yml site-binary.yml
```

### Ansible 分布式模式

```bash
cd deploy/ansible
ansible-playbook -i inventory/distributed.yml site.yml
```

## ⚠️ 迁移注意事项

### 从旧版本升级

1. **配置文件兼容性**
   - ✅ 保持向后兼容
   - ✅ 旧配置项继续有效
   - 🆕 新增配置项有默认值

2. **Redis 依赖变更**
   - 🔄 单节点模式现在可以完全禁用 Redis
   - 🔄 需要显式设置 `redis.enabled: false`

3. **存储引擎默认行为**
   - 🆕 默认启用 `storage_engine.enabled: true`
   - 🆕 自动优化默认启用
   - ⚙️ 可通过配置禁用

4. **查询缓存行为**
   - 🔄 分布式模式默认启用查询缓存
   - 🔄 单节点模式需要禁用查询缓存
   - ⚠️ 错误配置可能导致启动失败

## 📖 详细文档

请参阅以下文档获取详细信息：

- **部署指南**：[/deploy/DEPLOYMENT_GUIDE.md](./DEPLOYMENT_GUIDE.md)
- **README**：[/deploy/README.md](./README.md)
- **变更日志**：[/CHANGELOG.md](../CHANGELOG.md)
- **配置示例**：
  - [/config.yaml](../config.yaml) - 根目录配置（开发环境）
  - [/deploy/docker/config/config.yaml](./docker/config/config.yaml) - 分布式模式
  - [/deploy/docker/config/config.single.yaml](./docker/config/config.single.yaml) - 单节点模式

## 🐛 已知问题

1. **根目录 Dockerfile**
   - ⚠️ 根目录的 `Dockerfile` 和 `Dockerfile.arm` 已复制到 `deploy/` 目录
   - 💡 建议：删除根目录的旧文件，避免混淆
   - 📝 命令：`rm Dockerfile Dockerfile.arm`

## 🔮 后续计划

- [ ] 创建 Helm Chart（Kubernetes 包管理）
- [ ] 完善 Ansible 离线部署包
- [ ] 添加更多配置模板（高性能、低资源等）
- [ ] 完善监控和告警配置示例

---

**如有问题，请参考 [DEPLOYMENT_GUIDE.md](./DEPLOYMENT_GUIDE.md) 或提交 Issue。**

