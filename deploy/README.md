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