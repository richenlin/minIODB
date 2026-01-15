# MinIODB 部署指南

MinIODB 提供多种部署方式，适应不同的环境和需求。

## 🚀 快速开始

### 统一部署脚本

我们提供了统一的部署脚本，支持三种部署方式：

```bash
# 查看帮助
./deploy.sh --help

# Docker Compose 开发环境部署
./deploy.sh docker -e development

# Kubernetes 生产环境部署
./deploy.sh k8s -e production -r 3

# Ansible 批量部署
./deploy.sh ansible -e production -c inventory/simple.yml
```

## 📦 部署方式对比

| 部署方式 | 适用场景 | 优点 | 缺点 |
|---------|---------|------|------|
| **Docker Compose** | 开发、测试、单机部署 | 简单快速、资源占用少 | 不支持高可用 |
| **Kubernetes** | 生产环境、云原生 | 高可用、自动扩缩容 | 复杂度高、资源要求高 |
| **Ansible** | 批量部署、传统环境 | 灵活配置、批量管理 | 需要 Ansible 知识 |

## 🐳 Docker Compose 部署

### 前置要求

- Docker 20.10+
- Docker Compose 2.0+
- 4GB+ 内存
- 20GB+ 磁盘空间

### 快速部署

```bash
# 1. 进入 Docker 目录
cd deploy/docker

# 2. 复制环境配置文件
cp env.simple .env

# 3. 编辑配置 (可选)
nano .env

# 4. 启动服务
docker-compose up -d

# 5. 检查服务状态
docker-compose ps
```

### 简化版部署

如果您只需要基本功能，可以使用简化版配置：

```bash
# 使用简化版 Docker Compose
docker-compose -f docker-compose.simple.yml up -d
```

### 访问地址

- **REST API**: http://localhost:8081
- **gRPC API**: localhost:8080
- **MinIO Console**: http://localhost:9001 (minioadmin/minioadmin123)
- **Prometheus Metrics**: http://localhost:9090/metrics

### 常用命令

```bash
# 查看日志
docker-compose logs -f miniodb

# 重启服务
docker-compose restart

# 停止服务
docker-compose down

# 完全清理 (包括数据)
docker-compose down -v
```

## ☸️ Kubernetes 部署

### 前置要求

- Kubernetes 1.20+
- kubectl 配置正确
- 集群至少 3 个节点
- 每个节点 4GB+ 内存

### 快速部署

```bash
# 1. 进入 K8s 目录
cd deploy/k8s

# 2. 一键部署 (推荐)
kubectl apply -f all-in-one.yaml

# 3. 或者分步部署
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f redis/
kubectl apply -f minio/
kubectl apply -f miniodb/
```

### 检查部署状态

```bash
# 查看所有资源
kubectl get all -n miniodb-system

# 查看 Pod 状态
kubectl get pods -n miniodb-system -w

# 查看服务
kubectl get svc -n miniodb-system
```

### 访问服务

```bash
# 获取 NodePort
kubectl get svc miniodb-external -n miniodb-system

# 端口转发 (开发调试)
kubectl port-forward svc/miniodb-service 8081:8081 -n miniodb-system
```

### 扩缩容

```bash
# 扩容到 5 个副本
kubectl scale deployment miniodb --replicas=5 -n miniodb-system

# 自动扩缩容 (需要 HPA)
kubectl autoscale deployment miniodb --cpu-percent=70 --min=2 --max=10 -n miniodb-system
```

## 🔧 Ansible 部署

### 前置要求

- Ansible 2.9+
- 目标服务器 SSH 访问权限
- 目标服务器 sudo 权限

### 快速部署

```bash
# 1. 进入 Ansible 目录
cd deploy/ansible

# 2. 复制并编辑清单文件
cp inventory/simple.yml inventory/my-servers.yml
nano inventory/my-servers.yml

# 3. 测试连接
ansible -i inventory/my-servers.yml miniodb_servers -m ping

# 4. 执行部署
ansible-playbook -i inventory/my-servers.yml simple-deploy.yml

# 5. 或使用统一脚本
../deploy.sh ansible -c inventory/my-servers.yml
```

### 安全配置

建议使用 Ansible Vault 保护敏感信息：

```bash
# 创建加密的变量文件
ansible-vault create group_vars/miniodb_servers/vault.yml

# 编辑内容
vault_redis_password: "your-strong-password"
vault_minio_root_password: "your-strong-password"
vault_jwt_secret: "your-256-bit-secret"

# 使用 vault 运行
ansible-playbook -i inventory/my-servers.yml simple-deploy.yml --ask-vault-pass
```

## 🔧 配置说明

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `MINIODB_ENV` | development | 运行环境 (development/testing/production) |
| `LOG_LEVEL` | info | 日志级别 (debug/info/warn/error) |
| `REDIS_PASSWORD` | redis123 | Redis 密码 |
| `MINIO_ROOT_PASSWORD` | minioadmin123 | MinIO 管理员密码 |
| `JWT_SECRET` | dev-secret... | JWT 签名密钥 |

### 端口配置

| 服务 | 默认端口 | 说明 |
|------|----------|------|
| gRPC API | 8080 | gRPC 服务端口 |
| REST API | 8081 | HTTP REST API |
| Metrics | 9090 | Prometheus 指标 |
| MinIO API | 9000 | MinIO S3 API |
| MinIO Console | 9001 | MinIO Web 控制台 |
| Redis | 6379 | Redis 数据库 |

### 资源要求

#### 最小配置 (开发/测试)
- CPU: 2 核
- 内存: 4GB
- 磁盘: 20GB

#### 推荐配置 (生产环境)
- CPU: 4 核
- 内存: 8GB
- 磁盘: 100GB SSD

#### 高负载配置
- CPU: 8 核
- 内存: 16GB
- 磁盘: 500GB SSD

## 🔍 故障排除

### 常见问题

#### 1. 服务启动失败

```bash
# 检查日志
docker-compose logs miniodb
kubectl logs -l app.kubernetes.io/name=miniodb -n miniodb-system

# 检查依赖服务
docker-compose ps
kubectl get pods -n miniodb-system
```

#### 2. 连接 Redis 失败

```bash
# 检查 Redis 状态
docker-compose exec redis redis-cli ping
kubectl exec -it redis-0 -n miniodb-system -- redis-cli ping

# 检查密码配置
grep REDIS_PASSWORD .env
```

#### 3. MinIO 连接失败

```bash
# 检查 MinIO 状态
curl http://localhost:9000/minio/health/live

# 检查存储桶
docker-compose exec minio-init mc ls minio/
```

#### 4. 性能问题

```bash
# 查看资源使用
docker stats
kubectl top pods -n miniodb-system

# 查看指标
curl http://localhost:9090/metrics
```

### 日志收集

```bash
# Docker Compose
docker-compose logs --tail=100 > miniodb.log

# Kubernetes
kubectl logs -l app.kubernetes.io/name=miniodb -n miniodb-system --tail=100 > miniodb.log
```

## 🔐 安全建议

### 生产环境配置

1. **更改默认密码**
   ```bash
   # 生成强密码
   openssl rand -base64 32
   ```

2. **启用 TLS**
   ```yaml
   # docker-compose.yml
   environment:
     - ENABLE_TLS=true
     - TLS_CERT_PATH=/app/certs/server.crt
     - TLS_KEY_PATH=/app/certs/server.key
   ```

3. **网络隔离**
   - 使用防火墙限制访问
   - 配置 VPN 或内网访问
   - 启用 MinIO 和 Redis 的 TLS

4. **备份策略**
   ```bash
   # 定期备份
   crontab -e
   0 2 * * * /opt/miniodb/scripts/backup.sh
   ```

## 📊 监控和运维

### Prometheus 监控

MinIODB 内置 Prometheus 指标支持：

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'miniodb'
    static_configs:
      - targets: ['localhost:9090']
```

### Grafana 仪表板

导入预配置的 Grafana 仪表板：

```bash
# 下载仪表板配置
curl -O https://raw.githubusercontent.com/your-org/minIODB/main/monitoring/grafana-dashboard.json
```

### 健康检查

```bash
# REST API 健康检查
curl http://localhost:8081/v1/health

# 详细状态检查
curl http://localhost:8081/v1/status
```

## 🆙 升级指南

### Docker Compose 升级

```bash
# 1. 备份数据
docker-compose exec miniodb /app/scripts/backup.sh

# 2. 拉取新镜像
docker-compose pull

# 3. 重启服务
docker-compose up -d
```

### Kubernetes 升级

```bash
# 1. 更新镜像版本
kubectl set image deployment/miniodb miniodb=miniodb:v1.1.0 -n miniodb-system

# 2. 等待滚动更新完成
kubectl rollout status deployment/miniodb -n miniodb-system
```

## 📞 支持

- **文档**: [项目 README](../README.md)
- **问题反馈**: [GitHub Issues](https://github.com/your-org/minIODB/issues)
- **讨论**: [GitHub Discussions](https://github.com/your-org/minIODB/discussions)

---

📝 **注意**: 本文档会随着项目更新而更新，请定期查看最新版本。