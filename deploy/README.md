# MinIODB 部署指南

这个目录包含了MinIODB系统的完整部署方案，支持多种环境和部署方式，包括企业级离线部署解决方案。

## 🌟 部署方式

- [**Docker Compose**](./docker/) - 单机部署，快速启动，适合开发和测试
- [**Kubernetes**](./k8s/) - 生产级集群部署，支持高可用和水平扩展
- [**Ansible离线部署**](./ansible/) - 🔥 **企业级离线部署**，支持多架构、真正离线、二进制分发
- [**Ansible容器部署**](./ansible/) - 传统容器化自动部署方案
- [**部署脚本**](./scripts/) - 自动化部署和管理脚本

## 🏗️ 系统架构

MinIODB系统采用微服务架构，核心组件独立部署，存储桶初始化在独立阶段完成：

```
┌─────────────────────────────────────────────────────────────────┐
│                      MinIODB System                            │
├─────────────────────────────────────────────────────────────────┤
│ ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐     │
│ │   Init Stage    │ │   Service Stage  │ │  Runtime Stage  │     │
│ │   (独立初始化)   │ │   (服务启动)     │ │   (应用运行)     │     │
│ ├─────────────────┤ ├─────────────────┤ ├─────────────────┤     │
│ │ • 存储桶创建     │ │ • MinIO启动     │ │ • MinIODB应用   │     │
│ │ • 策略配置       │ │ • Redis启动     │ │ • API服务       │     │
│ │ • 版本控制       │ │ • 健康检查       │ │ • 监控指标       │     │
│ │ • 权限设置       │ │ • 依赖验证       │ │ • 备份任务       │     │
│ └─────────────────┘ └─────────────────┘ └─────────────────┘     │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   MinIODB   │  │    Redis    │  │    MinIO    │              │
│  │   (应用层)   │  │  (元数据层)  │  │  (存储层)    │              │
│  │             │  │             │  │             │              │
│  │ gRPC: 8080  │  │ Port: 6379  │  │ API: 9000   │              │
│  │ REST: 8081  │  │             │  │ Console:9001│              │
│  │ Metrics:9090│  │             │  │ Backup:9002 │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
├─────────────────────────────────────────────────────────────────┤
│                     Network Layer                              │
└─────────────────────────────────────────────────────────────────┘
```

### 🚀 部署方式对比

| 特性 | Docker Compose | Ansible容器 | **Ansible离线** | Kubernetes | 
|------|----------------|-------------|----------------|---------|
| **适用场景** | 开发/测试 | 传统生产 | 企业离线生产 | 云原生生产 | 
| **离线部署** | 部分支持 | 部分支持 | 完全离线 | 需要镜像仓库 |
| **多架构支持** | 有限 | 有限 | 自适应 | 良好 |
| **学习成本** | 低 | 中等 | 中等 | 高 |
| **运维难度** | 低 | 中等 | 中等 | 高 |
| **企业级特性** | 否 | 部分 | 完整 | 是 |
| **二进制分发** | 否 | 否 | 支持 | 否 |
| **网络依赖** | 高 | 高 | 无 | 中等 |
| **安全隔离** | 容器级 | 容器级 | 系统级 | 容器级 |

### 🏆 推荐部署方案

- **🥇 开发环境**: Docker Compose - 快速、简单
- **🥇 企业生产**: Ansible离线部署 - 安全、可控、离线
- **🥈 云原生**: Kubernetes - 弹性、自动化
- **🥉 传统生产**: Ansible容器 - 兼容性好

## 🚀 快速开始

### 单机开发部署 (推荐新手)

```bash
# 1. 克隆项目
git clone <repository-url>
cd minIODB/deploy/docker

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env 文件，设置必要的配置

# 3. 启动服务
docker-compose up -d

# 4. 验证部署
./scripts/health-check.sh
```

### 企业级离线部署 

```bash
# 1. 准备离线安装包
cd deploy/ansible

# 2. 下载二进制文件（在有网络的环境执行）
./scripts/download-binaries.sh

# 3. 配置部署清单
cp inventory/single-node.yml.example inventory/single-node.yml
# 编辑清单文件，设置目标主机信息

# 4. 执行离线部署
ansible-playbook -i inventory/single-node.yml site-binary.yml

# 5. 验证部署
ansible -i inventory/single-node.yml all -m shell -a "systemctl status miniodb"
```

### Ansible容器部署（传统方案）

```bash
# 1. 配置清单文件
cd deploy/ansible
cp inventory/auto-deploy.yml.example inventory/auto-deploy.yml
# 编辑清单文件，设置主机IP和服务分配

# 2. 配置认证信息
ansible-vault create group_vars/all/vault.yml
# 设置Redis、MinIO等服务的密码

# 3. 执行部署
ansible-playbook -i inventory/auto-deploy.yml site.yml

# 4. 验证部署
ansible -i inventory/auto-deploy.yml all -m shell -a "docker ps"
```

### Kubernetes部署 (推荐生产)

```bash
# 1. 创建命名空间和基础资源
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.yaml

# 2. 部署存储服务
kubectl apply -f k8s/redis/
kubectl apply -f k8s/minio/

# 3. 初始化存储桶（独立Job）
kubectl apply -f k8s/init-storage/

# 4. 部署应用服务
kubectl apply -f k8s/miniodb/

# 5. 验证部署
kubectl get pods -n miniodb-system
```

## 📋 部署要求

### 硬件要求

| 环境 | CPU | 内存 | 存储 | 网络 | 架构支持 |
|------|-----|------|------|------|---------|
| 开发环境 | 2核 | 4GB | 20GB | 1Gbps | AMD64 |
| 测试环境 | 4核 | 8GB | 100GB | 1Gbps | AMD64/ARM64 |
| 生产环境 | 8核+ | 16GB+ | 500GB+ | 10Gbps | AMD64/ARM64 |

### 软件要求

#### Docker Compose部署
- Docker 20.10+
- Docker Compose 2.0+
- 可用端口：6379, 8080, 8081, 9000, 9001, 9002, 9003, 9090

#### Ansible离线部署
- **操作系统**: Linux (RHEL/CentOS 7+, Ubuntu 18+, Debian 10+)
- **Python**: 3.6+
- **Ansible**: 2.9+ (仅在控制节点)
- **架构**: 自动检测AMD64/ARM64
- **网络**: 无需互联网连接（目标节点）
- **权限**: sudo权限
- **依赖**: 基础系统工具（curl, tar, systemctl）

#### Kubernetes部署
- Kubernetes 1.20+
- kubectl 配置完成
- 存储类支持动态分配
- 负载均衡器支持 (生产环境)

## 🏗️ Ansible离线部署详解

### 架构特点

1. **真正离线**: 所有组件二进制分发，无需Docker Hub
2. **多架构支持**: 自动检测AMD64/ARM64并分发对应二进制
3. **独立初始化**: 存储桶初始化在独立阶段完成，符合最佳实践
4. **systemd集成**: 原生Linux服务管理
5. **企业级配置**: 完整的安全配置和运维工具

### 部署流程

```bash
准备阶段 → 系统预检 → 二进制分发 → 服务配置 → 存储初始化 → 应用部署 → 健康验证
    ↓           ↓          ↓          ↓          ↓          ↓          ↓
架构检测    资源验证    文件分发    systemd配置  存储桶创建   应用启动    状态检查
用户创建    依赖检查    权限设置    服务启动    策略配置    健康检查    性能验证
目录创建    防火墙      环境配置    依赖等待    版本控制    监控启动    完成报告
```

### 离线包结构

```
files/
├── amd64/
│   ├── bin/          # AMD64二进制文件
│   │   ├── minio
│   │   ├── mc
│   │   └── miniodb
│   └── redis/        # Redis源码
├── arm64/
│   ├── bin/          # ARM64二进制文件
│   └── redis/
├── configs/          # 配置模板
│   ├── minio.service
│   ├── redis.service
│   ├── miniodb.service
│   ├── minio.env
│   ├── redis.conf
│   └── miniodb.env
└── install.sh        # 安装脚本
```

### 单机vs多机部署

```bash
# 单机部署
ansible-playbook -i inventory/single-node.yml site-binary.yml

# 多机部署
ansible-playbook -i inventory/multi-node.yml site-binary.yml \
  --extra-vars "deployment_type=cluster"
```

## ⚙️ 配置选项

### 核心配置变量

| 变量名 | 默认值 | 说明 | 部署方式 |
|--------|--------|------|----------|
| `deployment_mode` | `offline` | 部署模式 (offline/online) | Ansible |
| `deployment_type` | `single` | 部署类型 (single/cluster) | Ansible |
| `miniodb.install_dir` | `/opt/miniodb` | 安装目录 | Ansible |
| `minio.backup_enabled` | `true` | 启用备份MinIO | 所有 |
| `redis.max_memory` | `2gb` | Redis内存限制 | 所有 |

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `MINIODB_ENV` | `production` | 运行环境 |
| `MINIODB_LOG_LEVEL` | `info` | 日志级别 |
| `MINIO_ROOT_USER` | `minioadmin` | MinIO管理员用户名 |
| `MINIO_ROOT_PASSWORD` | `minioadmin123` | MinIO管理员密码 |
| `REDIS_PASSWORD` | `redis123` | Redis密码 |

### 存储桶配置

| 存储桶 | 用途 | 版本控制 | 备份 |
|--------|------|----------|------|
| `miniodb-data` | 主要数据存储 | ✅ | ✅ |
| `miniodb-metadata` | 元数据存储 | ✅ | ✅ |
| `miniodb-backup` | 备份数据存储 | ✅ | ❌ |

### 端口配置

| 服务 | 端口 | 协议 | 说明 | 二进制路径 |
|------|------|------|------|-----------|
| MinIODB gRPC | 8080 | TCP | gRPC API服务 | `/opt/miniodb/bin/miniodb` |
| MinIODB REST | 8081 | HTTP | REST API服务 | `/opt/miniodb/bin/miniodb` |
| MinIODB Metrics | 9090 | HTTP | Prometheus监控 | `/opt/miniodb/bin/miniodb` |
| Redis | 6379 | TCP | Redis数据库 | `/opt/miniodb/bin/redis-server` |
| MinIO API | 9000 | HTTP | S3 API服务 | `/opt/miniodb/bin/minio` |
| MinIO Console | 9001 | HTTP | 管理界面 | `/opt/miniodb/bin/minio` |
| MinIO Backup API | 9002 | HTTP | 备份S3 API | `/opt/miniodb/bin/minio` |
| MinIO Backup Console | 9003 | HTTP | 备份管理界面 | `/opt/miniodb/bin/minio` |

## 🔧 运维管理

### 健康检查

```bash
# Docker环境
docker-compose ps
curl http://localhost:8081/v1/health

# Ansible二进制环境
systemctl status miniodb redis minio
curl http://localhost:8081/v1/health

# Kubernetes环境  
kubectl get pods -n miniodb-system
kubectl logs -f deployment/miniodb -n miniodb-system
```

### 服务管理（Ansible离线部署）

```bash
# 服务状态检查
systemctl status miniodb
systemctl status redis  
systemctl status minio
systemctl status minio-backup  # 如果启用备份

# 服务启停
sudo systemctl start|stop|restart miniodb
sudo systemctl start|stop|restart redis
sudo systemctl start|stop|restart minio

# 开机自启
sudo systemctl enable miniodb redis minio

# 查看服务日志
journalctl -u miniodb -f
journalctl -u redis -f  
journalctl -u minio -f
```

### 存储桶管理

```bash
# 使用mc客户端管理存储桶
/opt/miniodb/bin/mc alias set minio http://localhost:9000 minioadmin minioadmin123

# 列出存储桶
/opt/miniodb/bin/mc ls minio/

# 查看存储桶策略
/opt/miniodb/bin/mc policy get minio/miniodb-data

# 启用版本控制
/opt/miniodb/bin/mc version enable minio/miniodb-data
```

### 数据备份

```bash
# 手动备份
curl -X POST http://localhost:8081/v1/backup/trigger \
  -H "Content-Type: application/json" \
  -d '{"id": "user123", "day": "2024-01-15"}'

# 定时备份 (建议使用cron job)
0 2 * * * /opt/miniodb/bin/backup-script.sh

# 存储桶级别备份
/opt/miniodb/bin/mc mirror minio/miniodb-data minio-backup/miniodb-backup
```

### 日志查看

```bash
# Docker环境
docker-compose logs -f miniodb
docker-compose logs -f redis
docker-compose logs -f minio

# Ansible二进制环境
tail -f /var/log/miniodb/miniodb.log
tail -f /var/log/redis/redis-server.log
journalctl -u minio -f

# Kubernetes环境
kubectl logs -f deployment/miniodb -n miniodb-system
kubectl logs -f statefulset/redis -n miniodb-system
kubectl logs -f statefulset/minio -n miniodb-system
```

### 扩容操作

```bash
# Docker环境 (垂直扩容)
docker-compose up -d --scale miniodb=3

# Ansible环境 (添加新节点)
# 1. 更新inventory文件添加新主机
# 2. 重新执行playbook
ansible-playbook -i inventory/multi-node.yml site-binary.yml --limit new_nodes

# Kubernetes环境 (水平扩容)
kubectl scale deployment miniodb --replicas=3 -n miniodb-system
```

## 🔒 安全配置

### 认证配置

1. **JWT认证**: 在配置文件中设置JWT密钥
2. **MinIO认证**: 使用强密码和访问密钥
3. **Redis认证**: 启用密码认证
4. **网络安全**: 配置防火墙规则

### 离线环境安全加固

```bash
# 禁用不必要的网络服务
sudo systemctl disable NetworkManager-wait-online
sudo systemctl disable systemd-networkd-wait-online

# 配置本地DNS解析
echo "127.0.0.1 minio" >> /etc/hosts
echo "127.0.0.1 redis" >> /etc/hosts

# 设置严格的文件权限
chmod 600 /etc/default/minio
chmod 600 /etc/redis/redis.conf
chown -R miniodb:miniodb /opt/miniodb
```

### TLS配置

```bash
# 生成自签名证书 (开发环境)
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /opt/miniodb/config/tls.key \
  -out /opt/miniodb/config/tls.crt

# 生产环境建议使用CA签发的证书
```

## 📊 监控告警

### Prometheus监控

MinIODB内置Prometheus metrics支持：

- 应用性能指标
- 系统资源指标  
- 业务操作指标
- 存储使用情况

### 监控端点

```bash
# 应用监控
curl http://localhost:9090/metrics

# MinIO监控  
curl http://localhost:9000/minio/v2/metrics/cluster

# Redis监控
/opt/miniodb/bin/redis-cli info stats
```

### 监控面板

推荐使用Grafana创建监控面板：

- 系统概览面板
- 性能分析面板
- 错误率监控面板
- 存储使用面板

## 🛠️ 故障排除

### 常见问题

1. **服务无法启动**
   ```bash
   # 检查端口占用
   netstat -tlnp | grep :8080
   
   # 检查磁盘空间
   df -h
   
   # 查看服务状态
   systemctl status miniodb
   journalctl -u miniodb -f
   ```

2. **连接超时**
   ```bash
   # 检查网络连接
   telnet localhost 8080
   
   # 检查防火墙设置
   firewall-cmd --list-ports
   
   # 验证服务健康状态
   curl http://localhost:8081/v1/health
   ```

3. **存储桶初始化失败**
   ```bash
   # 检查MinIO服务状态
   systemctl status minio
   
   # 手动运行存储桶初始化
   /opt/miniodb/bin/mc alias set minio http://localhost:9000 minioadmin minioadmin123
   /opt/miniodb/bin/mc mb minio/miniodb-data --ignore-existing
   ```

4. **架构不兼容**
   ```bash
   # 检查系统架构
   /opt/miniodb/scripts/detect-arch.sh info
   
   # 重新下载对应架构的二进制文件
   cd deploy/ansible
   ./scripts/download-binaries.sh
   ```

### 性能优化

1. **存储优化**
   - 使用SSD存储
   - 配置合适的缓冲区大小
   - 启用数据压缩

2. **网络优化**
   - 使用高速网络
   - 配置连接池
   - 启用Keep-Alive

3. **内存优化**
   - 调整Redis内存限制
   - 配置合适的连接池大小
   - 监控内存使用情况

## 🎯 最佳实践

### 生产环境部署建议

1. **使用Ansible离线部署** - 安全、可控、无网络依赖
2. **独立存储桶初始化** - 确保服务启动前存储就绪
3. **启用所有安全特性** - JWT、TLS、防火墙
4. **配置监控告警** - Prometheus + Grafana + AlertManager
5. **定期备份验证** - 自动备份 + 恢复测试
6. **多架构支持** - 提前测试ARM64环境兼容性

### 开发环境建议

1. **使用Docker Compose** - 快速启动和测试
2. **启用调试日志** - 便于问题排查
3. **禁用安全检查** - 简化开发流程
4. **使用开发配置** - 较小的资源配置

## 📚 相关文档

- [Docker部署详细说明](./docker/README.md)
- [Kubernetes部署详细说明](./k8s/README.md)
- [Ansible离线部署详细说明](./ansible/README.md)
- [部署脚本使用说明](./scripts/README.md)
- [API使用示例](../examples/README.md)
- [系统配置说明](../config.yaml)
- [架构设计文档](../docs/architecture.md)
- [存储桶管理指南](../docs/storage-management.md)

## 🤝 支持与反馈

如果在部署过程中遇到问题，请：

1. 查看相关文档和FAQ
2. 检查系统日志和错误信息
3. 使用故障排除工具进行诊断
4. 提交Issue或联系技术支持

### 问题报告模板

```markdown
**环境信息**
- 部署方式: [Docker/Ansible离线/Kubernetes]
- 操作系统: [Linux发行版 + 版本]
- 架构: [AMD64/ARM64]
- 系统配置: [CPU/内存/存储]

**问题描述**
[详细描述遇到的问题]

**错误日志**
[提供相关的错误日志]

**重现步骤**
1. [步骤1]
2. [步骤2]
3. [步骤3]
```

---

**⚠️ 重要提醒**: 
- 生产环境部署前，请仔细阅读安全配置章节
- 离线部署需要提前准备离线安装包
- 存储桶初始化在独立阶段完成，确保数据存储就绪
- 定期备份和监控是生产环境的必要措施 

# MinIODB Query Performance Monitoring

本文档介绍如何使用Prometheus和Grafana监控MinIODB的查询性能。

## 指标说明

### 1. miniodb_query_duration_seconds

**类型**: Histogram

**描述**: MinIODB查询执行耗时的直方图，使用细粒度分桶以支持精确的分位数计算。

**标签**:
- `query_type`: 查询类型 (select, insert, update, delete, etc.)
- `table`: 涉及的表名

**分桶配置**:
```
0.01s (10ms)
0.05s (50ms)
0.1s (100ms)
0.25s (250ms)
0.5s (500ms)
1.0s (1秒)
2.5s (2.5秒)
5.0s (5秒)
10.0s (10秒)
30.0s (30秒)
60.0s (60秒)
+Inf
```

这些分桶覆盖了从极快查询（10ms）到超时级别查询（60s）的完整范围，可以精确计算P50、P95、P99等分位数。

### 2. miniodb_slow_queries_total

**类型**: Counter

**描述**: 超过慢查询阈值的查询总数。

**标签**:
- `table`: 涉及的表名

## Prometheus查询示例

### 计算P50 (中位数)
```promql
histogram_quantile(0.50, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le))
```

### 计算P95
```promql
histogram_quantile(0.95, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le))
```

### 计算P99
```promql
histogram_quantile(0.99, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le))
```

### 按查询类型计算P95
```promql
histogram_quantile(0.95, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le, query_type))
```

### 按表计算P99
```promql
histogram_quantile(0.99, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le, table))
```

### 慢查询速率
```promql
sum(rate(miniodb_slow_queries_total[5m])) by (table)
```

### 平均查询耗时
```promql
sum(rate(miniodb_query_duration_seconds_sum[5m])) / sum(rate(miniodb_query_duration_seconds_count[5m]))
```

## Grafana面板配置

### 导入面板

1. 在Grafana中，点击 "+" -> "Import"
2. 上传 `grafana-query-performance-dashboard.json` 文件
3. 选择Prometheus数据源
4. 点击 "Import"

### 面板说明

面板包含以下可视化：

#### 1. Query Duration Percentiles (P50/P95/P99)
显示查询耗时的三个关键分位数：
- **P50 (中位数)**: 50%的查询在此时间内完成
- **P95**: 95%的查询在此时间内完成
- **P99**: 99%的查询在此时间内完成

**用途**: 快速了解整体查询性能和尾部延迟

#### 2. Query Duration by Type (P95)
按查询类型（SELECT, INSERT, UPDATE等）显示P95耗时。

**用途**: 识别哪种类型的查询性能较差

#### 3. Query Duration by Table (P99)
按表显示P99耗时。

**用途**: 识别哪些表的查询最慢

#### 4. Slow Queries Rate
显示每秒慢查询的数量（按表分组）。

**用途**: 监控慢查询趋势，及时发现性能问题

#### 5. Query Duration Heatmap
查询耗时的热力图，显示不同时间段的耗时分布。

**用途**: 可视化查询性能的时间模式

#### 6. Query Count by Duration Bucket
显示各个耗时区间的查询数量。

**用途**: 了解查询耗时的整体分布

#### 7. Average Query Duration (5m)
显示最近5分钟的平均查询耗时。

**用途**: 快速查看当前平均性能

#### 8. Total Slow Queries
显示累计的慢查询总数。

**用途**: 监控慢查询的总体情况

## 告警规则示例

### 1. 查询P99过高告警
```yaml
groups:
  - name: miniodb_query_performance
    rules:
      - alert: HighQueryP99
        expr: histogram_quantile(0.99, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le)) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "MinIODB query P99 is too high"
          description: "P99 query duration is {{ $value }}s (threshold: 5s)"
```

### 2. 慢查询率过高告警
```yaml
      - alert: HighSlowQueryRate
        expr: sum(rate(miniodb_slow_queries_total[5m])) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Too many slow queries"
          description: "Slow query rate is {{ $value }} queries/sec"
```

### 3. 特定表查询性能下降
```yaml
      - alert: TableQueryPerformanceDegraded
        expr: histogram_quantile(0.95, sum(rate(miniodb_query_duration_seconds_bucket{table="users"}[5m])) by (le)) > 2
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Query performance degraded for table: users"
          description: "P95 for users table is {{ $value }}s"
```

## 性能优化建议

根据监控指标进行性能优化：

### 如果P50高
- 大部分查询都慢，考虑：
  - 优化查询本身（索引、查询逻辑）
  - 增加资源（CPU、内存）
  - 检查是否有热点数据

### 如果P95/P99高但P50正常
- 存在尾部延迟问题，考虑：
  - 查看慢查询日志定位具体查询
  - 检查是否有大查询或复杂JOIN
  - 调整慢查询阈值
  - 优化缓存策略

### 如果特定表的查询慢
- 针对该表优化：
  - 检查表结构和数据量
  - 优化索引
  - 考虑分区或分片
  - 检查该表的写入压力

### 如果慢查询率突然增加
- 可能的原因：
  - 数据量增长
  - 查询模式改变
  - 资源不足
  - 缓存失效

## 配置慢查询阈值

在 `config.yaml` 中调整慢查询阈值：

```yaml
query_optimization:
  slow_query_threshold: 1s  # 超过1秒的查询被认为是慢查询
```

## 最佳实践

1. **定期查看P95和P99**: 这些指标能更好地反映用户体验
2. **设置告警**: 不要等到用户投诉才发现性能问题
3. **建立基线**: 记录正常情况下的性能指标作为对比基准
4. **关注趋势**: 性能的缓慢下降往往预示着潜在问题
5. **结合日志**: Prometheus指标告诉你"有问题"，日志告诉你"什么问题"

## 故障排查流程

1. **发现问题**: 通过Grafana面板或告警发现性能异常
2. **确定范围**: 
   - 是所有查询还是特定表？
   - 是特定类型的查询（SELECT/UPDATE等）吗？
   - 什么时候开始的？
3. **查看慢查询日志**: 在应用日志中搜索 "Slow query detected"
4. **分析具体查询**: 
   - SQL语句是否可以优化？
   - 涉及的数据量是否过大？
   - 是否有缺失的索引？
5. **采取行动**: 
   - 优化查询
   - 调整配置
   - 扩容资源
6. **验证效果**: 查看指标是否改善
# MinIODB 告警配置指南

本指南介绍如何配置和使用MinIODB的Prometheus告警规则，帮助您及时发现和解决连接池耗尽、查询性能下降等问题。

## 目录
- [快速开始](#快速开始)
- [告警规则说明](#告警规则说明)
- [部署配置](#部署配置)
- [压测验证](#压测验证)
- [告警处理流程](#告警处理流程)

---

## 快速开始

### 1. 导入告警规则

将 `deploy/prometheus-alerts.yml` 添加到Prometheus配置中：

```yaml
# prometheus.yml
rule_files:
  - '/etc/prometheus/alerts/miniodb-alerts.yml'

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['localhost:9093']  # Alertmanager地址
```

### 2. 重启Prometheus

```bash
# Docker方式
docker-compose restart prometheus

# 系统服务方式
systemctl reload prometheus
```

### 3. 验证规则加载

访问 Prometheus UI：`http://localhost:9090/alerts`，确认MinIODB告警规则已加载。

---

## 告警规则说明

### 1. 连接池耗尽告警

#### Redis连接池告警

| 告警名称                       | 触发条件     | 持续时间 | 严重程度 | 说明             |
| ------------------------------ | ------------ | -------- | -------- | ---------------- |
| `RedisPoolHighUtilization`     | 利用率 > 90% | 2分钟    | Warning  | 连接池利用率过高 |
| `RedisPoolCriticalUtilization` | 利用率 > 95% | 1分钟    | Critical | 连接池接近耗尽   |

**PromQL查询**:
```promql
miniodb_redis_pool_utilization_percent > 90
```

**指标说明**:
- `miniodb_redis_pool_active_conns`: 活跃连接数
- `miniodb_redis_pool_idle_conns`: 空闲连接数
- `miniodb_redis_pool_total_conns`: 总连接数
- `miniodb_redis_pool_utilization_percent`: 利用率 (active/total * 100)

#### MinIO连接池告警

| 告警名称                       | 触发条件     | 持续时间 | 严重程度 | 说明             |
| ------------------------------ | ------------ | -------- | -------- | ---------------- |
| `MinIOPoolHighUtilization`     | 利用率 > 90% | 2分钟    | Warning  | 连接池利用率过高 |
| `MinIOPoolCriticalUtilization` | 利用率 > 95% | 1分钟    | Critical | 连接池接近耗尽   |

**PromQL查询**:
```promql
miniodb_minio_pool_utilization_percent > 90
```

### 2. 尾部时延告警

#### 查询P99时延告警

| 告警名称                  | 触发条件   | 持续时间 | 严重程度 | 说明             |
| ------------------------- | ---------- | -------- | -------- | ---------------- |
| `QueryP99LatencyHigh`     | P99 > 5秒  | 3分钟    | Warning  | 查询时延过高     |
| `QueryP99LatencyCritical` | P99 > 10秒 | 2分钟    | Critical | 查询时延严重过高 |

**PromQL查询**:
```promql
histogram_quantile(0.99, 
  sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le, table)
) > 5
```

#### 写入P99时延告警

| 告警名称                  | 触发条件    | 持续时间 | 严重程度 |
| ------------------------- | ----------- | -------- | -------- |
| `WriteP99LatencyHigh`     | P99 > 1秒   | 3分钟    | Warning  |
| `WriteP99LatencyCritical` | P99 > 2.5秒 | 2分钟    | Critical |

#### 刷新P99时延告警

| 告警名称              | 触发条件  | 持续时间 | 严重程度 |
| --------------------- | --------- | -------- | -------- |
| `FlushP99LatencyHigh` | P99 > 5秒 | 3分钟    | Warning  |

### 3. 恢复通知

系统自动检测性能恢复并发送info级别通知：

- `RedisPoolUtilizationRecovered`: Redis连接池利用率降至70%以下
- `MinIOPoolUtilizationRecovered`: MinIO连接池利用率降至70%以下
- `QueryLatencyRecovered`: 查询P99时延降至3秒以下

### 4. 综合告警

#### 系统高压力告警

**触发条件**: 连接池利用率 > 90% **且** 查询P99时延 > 5秒

```promql
(miniodb_redis_pool_utilization_percent > 90 or miniodb_minio_pool_utilization_percent > 90)
and
histogram_quantile(0.99, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le)) > 5
```

**紧急处理步骤**:
1. 立即扩容连接池
2. 检查并优化慢查询
3. 检查系统资源（CPU/内存/网络）
4. 考虑限流或降级
5. 通知运维团队

---

## 部署配置

### Prometheus配置示例

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 30s

rule_files:
  - '/etc/prometheus/alerts/miniodb-alerts.yml'

scrape_configs:
  - job_name: 'miniodb'
    static_configs:
      - targets: ['localhost:8080']  # MinIODB metrics endpoint
    metrics_path: '/metrics'

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

### Alertmanager配置示例

```yaml
# alertmanager.yml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'component']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'miniodb-team'
  
  routes:
    - match:
        severity: critical
      receiver: 'miniodb-oncall'
      continue: true

    - match:
        severity: warning
      receiver: 'miniodb-team'

    - match:
        severity: info
      receiver: 'miniodb-notifications'

receivers:
  - name: 'miniodb-oncall'
    webhook_configs:
      - url: 'http://your-oncall-system/webhook'
    
  - name: 'miniodb-team'
    email_configs:
      - to: 'team@example.com'
        from: 'alertmanager@example.com'
        smarthost: 'smtp.example.com:587'
    
  - name: 'miniodb-notifications'
    webhook_configs:
      - url: 'http://your-notification-system/webhook'

inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname', 'component']
```

---

## 压测验证

### 1. 连接池耗尽压测

**目标**: 触发Redis连接池高利用率告警

```bash
# 使用 hey 工具进行压测
hey -z 5m -c 100 -m POST \
  -H "Content-Type: application/json" \
  -d '{"sql":"SELECT * FROM test_table LIMIT 1000"}' \
  http://localhost:8081/v1/query

# 或使用 wrk
wrk -t 20 -c 200 -d 5m \
  -s post_query.lua \
  http://localhost:8081/v1/query
```

**预期结果**:
1. 2分钟后触发 `RedisPoolHighUtilization` (Warning)
2. 如果继续压测，1分钟后触发 `RedisPoolCriticalUtilization` (Critical)

**验证方法**:
```bash
# 查询当前连接池利用率
curl -s http://localhost:8080/metrics | grep miniodb_redis_pool_utilization_percent

# 检查Prometheus告警状态
curl -s http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname | contains("RedisPool"))'
```

### 2. 查询P99时延压测

**目标**: 触发查询P99时延告警

```bash
# 执行大量复杂查询
for i in {1..1000}; do
  curl -X POST http://localhost:8081/v1/query \
    -H "Content-Type: application/json" \
    -d "{\"sql\":\"SELECT * FROM large_table WHERE timestamp > $(date -d '30 days ago' +%s) ORDER BY timestamp DESC LIMIT 10000\"}" &
done

# 等待并检查告警
sleep 180
curl -s http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname == "QueryP99LatencyHigh")'
```

**预期结果**:
1. 3分钟后触发 `QueryP99LatencyHigh` (Warning)
2. 如果P99持续超过10秒，2分钟后触发 `QueryP99LatencyCritical` (Critical)

### 3. 恢复验证

**停止压测后**:
```bash
# 杀掉所有压测进程
pkill -f hey
pkill -f wrk

# 监控恢复情况（约5分钟）
watch -n 10 'curl -s http://localhost:8080/metrics | grep -E "miniodb_redis_pool_utilization_percent|miniodb_query_duration_seconds"'

# 1分钟后应收到恢复通知
curl -s http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname | contains("Recovered"))'
```

### 4. 压测脚本示例

```bash
#!/bin/bash
# test_alerts.sh - 完整的告警压测脚本

set -e

MINIODB_URL="http://localhost:8081"
PROMETHEUS_URL="http://localhost:9090"

echo "=== MinIODB 告警压测验证 ==="

# 1. 检查服务状态
echo "[1/5] 检查服务状态..."
curl -sf ${MINIODB_URL}/v1/health || { echo "MinIODB未运行"; exit 1; }
curl -sf ${PROMETHEUS_URL}/-/healthy || { echo "Prometheus未运行"; exit 1; }

# 2. 获取基线指标
echo "[2/5] 获取基线指标..."
BASELINE_UTIL=$(curl -s ${MINIODB_URL}/metrics | grep -oP 'miniodb_redis_pool_utilization_percent \K[0-9.]+' | head -1)
echo "当前Redis连接池利用率: ${BASELINE_UTIL}%"

# 3. 启动压测
echo "[3/5] 启动连接池压测（5分钟）..."
hey -z 5m -c 100 -m POST \
  -H "Content-Type: application/json" \
  -d '{"sql":"SELECT * FROM test_table LIMIT 1000"}' \
  ${MINIODB_URL}/v1/query > /dev/null 2>&1 &

LOAD_PID=$!

# 4. 监控告警触发
echo "[4/5] 监控告警状态（等待3分钟）..."
for i in {1..18}; do
  sleep 10
  ALERTS=$(curl -s ${PROMETHEUS_URL}/api/v1/alerts | jq -r '.data.alerts[] | select(.labels.alertname | contains("Pool")) | .labels.alertname' | wc -l)
  if [ "$ALERTS" -gt 0 ]; then
    echo "✓ 告警已触发 ($(date))"
    curl -s ${PROMETHEUS_URL}/api/v1/alerts | jq '.data.alerts[] | {alert: .labels.alertname, state: .state, value: .annotations.description}'
    break
  fi
  echo "  等待告警触发... ($i/18)"
done

# 5. 停止压测并验证恢复
echo "[5/5] 停止压测，验证恢复..."
kill $LOAD_PID 2>/dev/null || true

echo "等待恢复（2分钟）..."
sleep 120

RECOVERED=$(curl -s ${PROMETHEUS_URL}/api/v1/alerts | jq -r '.data.alerts[] | select(.labels.alertname | contains("Recovered")) | .labels.alertname' | wc -l)
if [ "$RECOVERED" -gt 0 ]; then
  echo "✓ 恢复通知已触发"
else
  echo "⚠ 未检测到恢复通知（可能需要更长时间）"
fi

echo "=== 压测完成 ==="
```

---

## 告警处理流程

### Warning级别告警处理

1. **确认告警**
   - 查看Grafana仪表板确认指标趋势
   - 检查是否为误报或临时波动

2. **分析原因**
   - 检查应用日志
   - 查看慢查询日志
   - 分析系统资源使用情况

3. **优化措施**
   - 优化查询性能
   - 调整连接池配置
   - 增加缓存

4. **记录和监控**
   - 记录问题和解决方案
   - 持续监控指标变化

### Critical级别告警处理

1. **立即响应**（5分钟内）
   - 确认告警严重性
   - 通知相关团队

2. **紧急缓解**（10分钟内）
   - 快速扩容连接池
   - 临时限流
   - 降级非关键功能

3. **根因分析**（30分钟内）
   - 查看详细日志和指标
   - 定位性能瓶颈
   - 识别异常查询或操作

4. **永久修复**（按优先级）
   - 优化代码或查询
   - 调整系统配置
   - 扩展基础设施

5. **复盘总结**
   - 记录事件时间线
   - 分析响应流程
   - 优化告警规则

---

## 常见问题

### Q1: 如何调整告警阈值？

编辑 `deploy/prometheus-alerts.yml`，修改 `expr` 中的阈值：

```yaml
# 将Redis连接池告警阈值从90%改为85%
- alert: RedisPoolHighUtilization
  expr: miniodb_redis_pool_utilization_percent > 85  # 修改这里
  for: 2m
```

### Q2: 如何临时禁用某个告警？

在Alertmanager中添加静默规则：

```bash
# 静默2小时
amtool silence add \
  alertname="RedisPoolHighUtilization" \
  --duration=2h \
  --author="ops@example.com" \
  --comment="Planned maintenance"
```

### Q3: 告警太频繁怎么办？

1. 增加 `for` 持续时间（例如从2m改为5m）
2. 调整 `group_interval` 和 `repeat_interval`
3. 使用 `inhibit_rules` 避免重复告警

### Q4: 如何集成到企业微信/钉钉？

配置Alertmanager的webhook_configs：

```yaml
receivers:
  - name: 'wechat'
    webhook_configs:
      - url: 'http://your-wechat-bot-url'
        send_resolved: true
```

---

## 相关文档

- [Prometheus告警文档](https://prometheus.io/docs/alerting/latest/overview/)
- [Alertmanager配置](https://prometheus.io/docs/alerting/latest/configuration/)
- [MinIODB监控指标](./METRICS.md)
- [Grafana仪表板](../deploy/grafana-write-flush-panels.json)

---
