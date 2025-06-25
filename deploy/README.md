# MinIODB 部署指南

这个目录包含了MinIODB系统的完整部署方案，支持多种环境和部署方式。

## 🌟 部署方式

- [**Docker Compose**](./docker/) - 单机部署，快速启动，适合开发和测试
- [**Kubernetes**](./k8s/) - 生产级集群部署，支持高可用和水平扩展
- [**Ansible**](./ansible/) - 多节点自动化部署，支持现有服务和容器服务两种模式
- [**部署脚本**](./scripts/) - 自动化部署和管理脚本

## 🏗️ 系统架构

MinIODB系统由以下核心组件构成：

```
┌─────────────────────────────────────────────────────────┐
│                    MinIODB System                       │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   MinIODB   │  │    Redis    │  │    MinIO    │      │
│  │   (应用层)   │  │  (元数据层)  │  │  (存储层)    │      │
│  │             │  │             │  │             │      │
│  │ gRPC: 8080  │  │ Port: 6379  │  │ API: 9000   │      │
│  │ REST: 8081  │  │             │  │ Console:9001│      │
│  │ Metrics:9090│  │             │  │             │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
├─────────────────────────────────────────────────────────┤
│                   Network Layer                         │
└─────────────────────────────────────────────────────────┘
```

### 🚀 部署方式对比

| 特性 | Docker Compose | Ansible | Kubernetes | 
|------|----------------|------------|---------|
| **适用场景** | 开发/测试 | 多节点生产 | 生产集群 | 
| **复杂度** | 简单 |  中等 | 复杂 |
| **扩展性** | 有限 |  良好 | 优秀 |
| **高可用** | 否 |  是 | 是 |
| **自动化** | 基础 |  高级 | 高级 |
| **学习成本** | 低 |  中等 | 高 |
| **运维难度** | 低 |  中等 | 高 |
| **节点数量** | 单节点 | 多节点 | 多节点 |
| **服务发现** | 内置 |  手动配置 | 内置 |

### 组件说明

- **MinIODB**: 核心应用服务，提供gRPC和REST API
- **Redis**: 元数据中心，负责服务发现、数据索引和一致性哈希
- **MinIO**: 对象存储服务，提供S3兼容的分布式存储
- **MinIO-Backup**: 备份存储实例，用于数据备份和恢复

## 🚀 快速开始

### 单机部署 (推荐)

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

### Ansible多节点部署

```bash
# 1. 安装Ansible
pip3 install ansible docker

# 2. 配置清单文件
cd deploy/ansible
cp inventory/auto-deploy.yml.example inventory/auto-deploy.yml
# 编辑清单文件，设置主机IP和服务分配

# 3. 配置认证信息
ansible-vault create group_vars/all/vault.yml
# 设置Redis、MinIO等服务的密码

# 4. 执行部署
./scripts/deploy-ansible.sh auto

# 5. 验证部署
ansible -i inventory/auto-deploy.yml all -m shell -a "docker ps"
```

### Kubernetes部署

```bash
# 1. 创建命名空间和基础资源
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.yaml

# 2. 部署存储服务
kubectl apply -f k8s/redis/
kubectl apply -f k8s/minio/

# 3. 部署应用服务
kubectl apply -f k8s/miniodb/

# 4. 验证部署
kubectl get pods -n miniodb-system
```

## 📋 部署要求

### 硬件要求

| 环境 | CPU | 内存 | 存储 | 网络 |
|------|-----|------|------|------|
| 开发环境 | 2核 | 4GB | 20GB | 1Gbps |
| 测试环境 | 4核 | 8GB | 100GB | 1Gbps |
| 生产环境 | 8核+ | 16GB+ | 500GB+ | 10Gbps |

### 软件要求

#### Docker Compose部署
- Docker 20.10+
- Docker Compose 2.0+
- 可用端口：6379, 8080, 8081, 9000, 9001, 9002, 9003, 9090

#### Kubernetes部署
- Kubernetes 1.20+
- kubectl 配置完成
- 存储类支持动态分配
- 负载均衡器支持 (生产环境)

## ⚙️ 配置选项

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `MINIODB_ENV` | `production` | 运行环境 |
| `MINIODB_LOG_LEVEL` | `info` | 日志级别 |
| `MINIO_ROOT_USER` | `minioadmin` | MinIO管理员用户名 |
| `MINIO_ROOT_PASSWORD` | `minioadmin123` | MinIO管理员密码 |
| `REDIS_PASSWORD` | `redis123` | Redis密码 |

### 端口配置

| 服务 | 端口 | 协议 | 说明 |
|------|------|------|------|
| MinIODB gRPC | 8080 | TCP | gRPC API服务 |
| MinIODB REST | 8081 | HTTP | REST API服务 |
| MinIODB Metrics | 9090 | HTTP | Prometheus监控 |
| Redis | 6379 | TCP | Redis数据库 |
| MinIO API | 9000 | HTTP | S3 API服务 |
| MinIO Console | 9001 | HTTP | 管理界面 |
| MinIO Backup API | 9002 | HTTP | 备份S3 API |
| MinIO Backup Console | 9003 | HTTP | 备份管理界面 |

## 🔧 运维管理

### 健康检查

```bash
# Docker环境
docker-compose ps
curl http://localhost:8081/v1/health

# Kubernetes环境  
kubectl get pods -n miniodb-system
kubectl logs -f deployment/miniodb -n miniodb-system
```

### 日志查看

```bash
# Docker环境
docker-compose logs -f miniodb
docker-compose logs -f redis
docker-compose logs -f minio

# Kubernetes环境
kubectl logs -f deployment/miniodb -n miniodb-system
kubectl logs -f statefulset/redis -n miniodb-system
kubectl logs -f statefulset/minio -n miniodb-system
```

### 数据备份

```bash
# 手动备份
curl -X POST http://localhost:8081/v1/backup/trigger \
  -H "Content-Type: application/json" \
  -d '{"id": "user123", "day": "2024-01-15"}'

# 定时备份 (建议使用cron job)
0 2 * * * /path/to/backup-script.sh
```

### 扩容操作

```bash
# Docker环境 (垂直扩容)
docker-compose up -d --scale miniodb=3

# Kubernetes环境 (水平扩容)
kubectl scale deployment miniodb --replicas=3 -n miniodb-system
```

## 🔒 安全配置

### 认证配置

1. **JWT认证**: 在配置文件中设置JWT密钥
2. **MinIO认证**: 使用强密码和访问密钥
3. **Redis认证**: 启用密码认证
4. **网络安全**: 配置防火墙规则

### TLS配置

```bash
# 生成自签名证书 (开发环境)
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout tls.key -out tls.crt

# 生产环境建议使用CA签发的证书
```

## 📊 监控告警

### Prometheus监控

MinIODB内置Prometheus metrics支持：

- 应用性能指标
- 系统资源指标  
- 业务操作指标

### 监控面板

推荐使用Grafana创建监控面板：

- 系统概览面板
- 性能分析面板
- 错误率监控面板

## 🛠️ 故障排除

### 常见问题

1. **服务无法启动**
   - 检查端口占用: `netstat -tlnp | grep :8080`
   - 检查磁盘空间: `df -h`
   - 查看服务日志: `docker-compose logs miniodb`

2. **连接超时**
   - 检查网络连接: `telnet localhost 8080`
   - 检查防火墙设置
   - 验证服务健康状态

3. **数据不一致**
   - 检查Redis连接状态
   - 验证MinIO存储状态
   - 执行数据一致性检查

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
   - 调整JVM参数
   - 配置Redis内存限制
   - 监控内存使用情况

## 📚 相关文档

- [Docker部署详细说明](./docker/README.md)
- [Kubernetes部署详细说明](./k8s/README.md)
- [部署脚本使用说明](./scripts/README.md)
- [API使用示例](../examples/README.md)
- [系统配置说明](../config.yaml)

## 🤝 支持与反馈

如果在部署过程中遇到问题，请：

1. 查看相关文档和FAQ
2. 检查系统日志和错误信息
3. 提交Issue或联系技术支持

---

**注意**: 生产环境部署前，请仔细阅读安全配置章节，确保系统安全性。 