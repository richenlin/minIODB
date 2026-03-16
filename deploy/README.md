# MinIODB 部署指南

MinIODB 提供 4 种部署模式，适应不同的场景和需求。

## 目录

- [快速开始](#快速开始)
- [依赖管理](#依赖管理)
- [部署模式对比](#部署模式对比)
- [1. 单机开发模式 (dev)](#1-单机开发模式-dev)
- [2. 单机集成测试模式 (test)](#2-单机集成测试模式-test)
- [3. 小型集群模式 (swarm)](#3-小型集群模式-swarm)
- [4. Kubernetes 集群模式 (k8s)](#4-kubernetes-集群模式-k8s)
- [配置说明](#配置说明)
- [故障排除](#故障排除)
- [安全建议](#安全建议)
- [监控和运维](#监控和运维)

## 快速开始

### 统一部署脚本

我们提供了统一的部署脚本，支持 4 种部署模式：

```bash
# 查看帮助
./deploy/deploy.sh --help

# 单机开发模式
./deploy/deploy.sh dev

# 单机集成测试模式
./deploy/deploy.sh test

# 小型集群模式
./deploy/deploy.sh swarm

# Kubernetes 集群模式
./deploy/deploy.sh k8s

# 清理部署
./deploy/deploy.sh dev --cleanup
```

## 依赖管理

MinIODB 部署脚本提供自动依赖检测和安装功能，支持在线自动安装和离线下载。

### 支持的平台

- **Linux**: x86-64、ARM64
- **macOS**: ARM64 (Apple Silicon)

### 依赖版本要求

| 组件 | 最低版本 | 推荐版本 |
|------|----------|----------|
| Docker | 20.10+ | 24.0+ |
| Docker Compose | 2.0+ | 2.23+ |
| kubectl | 1.20+ | 1.28+ |
| Ansible | 2.9+ | 2.15+ |

### 自动检测依赖

部署脚本会自动检测所需的依赖：

```bash
# 部署时会自动检查依赖
./deploy/deploy.sh dev
```

如果依赖缺失，脚本会提示缺少的组件。

### 自动安装依赖

支持在线自动安装依赖：

```bash
# 自动安装所有依赖
./deploy/deploy.sh --install-deps

# 检查并安装特定部署模式需要的依赖
./deploy/deploy.sh dev --install-deps
./deploy/deploy.sh swarm --install-deps
./deploy/deploy.sh k8s --install-deps
```

### 离线安装包下载

如果网络不可用或需要离线部署，可以获取离线安装包下载地址：

```bash
# 显示所有依赖的离线下载地址
./deploy/deploy.sh --show-deps
```

### 手动安装依赖

#### Linux (Ubuntu/Debian)

```bash
# Docker
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg lsb-release
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Ansible
sudo apt-get install -y ansible

# kubectl
curl -LO "https://dl.k8s.io/release/v1.28.4/bin/linux/amd64/kubectl"
sudo chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

#### Linux (CentOS/RHEL)

```bash
# Docker
sudo yum install -y yum-utils
sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo yum install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Ansible
sudo yum install -y ansible

# kubectl
curl -LO "https://dl.k8s.io/release/v1.28.4/bin/linux/amd64/kubectl"
sudo chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

#### Linux (Arch Linux)

```bash
# Docker 和 Docker Compose
sudo pacman -S docker docker-compose
sudo systemctl start docker
sudo systemctl enable docker

# Ansible
sudo pacman -S ansible

# kubectl
sudo pacman -S kubectl
```

#### macOS (ARM64)

```bash
# 安装 Homebrew（如果尚未安装）
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Docker Desktop (包含 Docker 和 Docker Compose)
brew install --cask docker

# Ansible
brew install ansible

# kubectl
brew install kubectl
```

### 离线包下载地址

以下链接提供了各平台和架构的离线安装包：

#### Docker

- **Linux x86-64**: https://download.docker.com/linux/static/stable/x86_64/docker-24.0.7.tgz
- **Linux ARM64**: https://download.docker.com/linux/static/stable/aarch64/docker-24.0.7.tgz
- **macOS ARM64**: https://desktop.docker.com/mac/main/arm64/Docker.dmg

安装方法：
```bash
# Linux
tar xzvf docker-24.0.7.tgz
sudo cp docker/* /usr/bin/

# macOS
# 下载后双击 Docker.dmg 安装
```

#### Docker Compose

- **Linux x86-64**: https://github.com/docker/compose/releases/download/v2.23.0/docker-compose-linux-x86_64
- **Linux ARM64**: https://github.com/docker/compose/releases/download/v2.23.0/docker-compose-linux-aarch64

安装方法：
```bash
sudo mv docker-compose-linux-* /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose
```

#### kubectl

- **Linux x86-64**: https://dl.k8s.io/release/v1.28.4/bin/linux/amd64/kubectl
- **Linux ARM64**: https://dl.k8s.io/release/v1.28.4/bin/linux/arm64/kubectl
- **macOS ARM64**: https://dl.k8s.io/release/v1.28.4/bin/darwin/arm64/kubectl

安装方法：
```bash
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

#### Ansible

Ansible 通常使用系统包管理器安装：

- **Ubuntu/Debian**: `sudo apt-get install -y ansible`
- **CentOS/RHEL**: `sudo yum install -y ansible`
- **Arch Linux**: `sudo pacman -S ansible`
- **macOS**: `brew install ansible`

## 部署模式对比

| 部署模式 | 适用场景 | 节点数 | 优点 | 缺点 |
|---------|---------|--------|------|------|
| **dev** | 本地开发、调试 | 1 | 简单快速、灵活调试 | 需手动启动应用 |
| **test** | 集成测试、CI/CD | 1 | 一键部署、自动化测试 | 单点故障 |
| **swarm** | 小型生产、资源受限 | 3 | 轻量集群、简单管理 | 扩展性有限 |
| **k8s** | 生产环境、云原生 | 4+ | 高可用、自动扩缩容 | 复杂度高 |

## 1. 单机开发模式 (dev)

### 场景说明

适用于本地开发、代码调试、功能测试。只启动基础设施服务，MinIODB 应用使用 `go run` 或调试工具启动，便于热重载和断点调试。

### 前置要求

- Docker 20.10+
- Docker Compose 2.0+
- Go 1.24+ (用于本地运行)
- 2GB+ 内存
- 10GB+ 磁盘空间

### 快速部署

```bash
# 1. 启动基础设施服务
./deploy/deploy.sh dev

# 2. 等待服务启动完成（约 30 秒）
docker ps

# 3. 启动 MinIODB 应用
go run cmd/main.go

# 或使用调试工具（VS Code / GoLand）
```

### 访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| Redis | localhost:6379 | 元数据中心 |
| MinIO API | http://localhost:9000 | 主存储 S3 API |
| MinIO Console | http://localhost:9001 | MinIO Web 控制台 |
| MinIO Backup API | http://localhost:9002 | 备份存储 S3 API |
| MinIO Backup Console | http://localhost:9003 | 备份存储控制台 |
| MinIODB REST API | http://localhost:8081 | REST API (手动启动) |
| MinIODB gRPC API | localhost:8080 | gRPC API (手动启动) |

### 开发调试

```bash
# 使用 dlv 调试
dlv debug cmd/main.go

# 使用 VS Code 调试配置
# .vscode/launch.json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Launch Package",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/cmd/main.go",
      "env": {
        "MINIODB_ENV": "development"
      }
    }
  ]
}
```

### 常用命令

```bash
# 查看服务日志
docker-compose -f deploy/docker/docker-compose.dev.yml logs -f

# 重启服务
docker-compose -f deploy/docker/docker-compose.dev.yml restart

# 停止服务
docker-compose -f deploy/docker/docker-compose.dev.yml down

# 清理所有数据
docker-compose -f deploy/docker/docker-compose.dev.yml down -v
```

## 2. 单机集成测试模式 (test)

### 场景说明

适用于集成测试、端到端测试、CI/CD 流程。启动所有服务（包括 MinIODB 容器），实现一键部署和自动化测试。

### 前置要求

- Docker 20.10+
- Docker Compose 2.0+
- 4GB+ 内存
- 20GB+ 磁盘空间

### 快速部署

```bash
# 1. 启动所有服务（自动构建镜像）
./deploy/deploy.sh test

# 2. 等待服务启动完成（约 1-2 分钟）
docker ps

# 3. 验证服务健康
curl http://localhost:8081/v1/health
```

### 访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| MinIODB REST API | http://localhost:8081 | REST API |
| MinIODB gRPC API | localhost:8080 | gRPC API |
| Prometheus Metrics | http://localhost:9090/metrics | 监控指标 |
| MinIO Console | http://localhost:9001 | MinIO Web 控制台 |
| MinIO Backup Console | http://localhost:9003 | 备份存储控制台 |

### 集成测试

```bash
# 运行测试套件
go test ./...

# 带覆盖率的测试
go test -cover ./...

# 端到端测试
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -d '{"name":"test_table","schema":{"columns":[{"name":"id","type":"int64"}]}}'
```

### 常用命令

```bash
# 查看所有服务状态
docker-compose -f deploy/docker/docker-compose.yml ps

# 查看 MinIODB 日志
docker-compose -f deploy/docker/docker-compose.yml logs -f miniodb

# 重启 MinIODB 服务
docker-compose -f deploy/docker/docker-compose.yml restart miniodb

# 停止所有服务
docker-compose -f deploy/docker/docker-compose.yml down

# 清理所有数据
docker-compose -f deploy/docker/docker-compose.yml down -v
```

## 3. 小型集群模式 (swarm)

### 场景说明

适用于小型生产环境、资源受限场景。基于 Docker Swarm 的轻量级集群部署，3 个节点分别运行不同服务，实现负载分担和资源优化。

### 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                    Docker Swarm Cluster                      │
├─────────────────────────────────────────────────────────────┤
│  Node 1 (Manager)     │  Node 2 (Worker)  │  Node 3 (Worker)│
│                       │                   │                  │
│  ┌───────────────┐    │  ┌──────────────┐ │ ┌──────────────┐ │
│  │   MinIODB     │    │  │    MinIO     │ │ │ MinIO Backup │ │
│  │   + Redis     │    │  │              │ │ │              │ │
│  │   :8081       │    │  │   :9000      │ │ │    :9002     │ │
│  └───────────────┘    │  └──────────────┘ │ └──────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 前置要求

- Docker 20.10+ (所有节点)
- Ansible 2.9+ (控制机)
- 3 台服务器（1 manager + 2 workers）
- 每台服务器 4GB+ 内存
- 服务器间网络互通

### 节点规划

| 节点 | 角色 | 服务 | IP 示例 |
|------|------|------|---------|
| Node 1 | Manager | MinIODB + Redis | 192.168.1.10 |
| Node 2 | Worker | MinIO (主存储) | 192.168.1.11 |
| Node 3 | Worker | MinIO Backup | 192.168.1.12 |

### 快速部署

```bash
# 1. 配置集群节点清单
cd deploy/ansible
cp inventory/swarm.ini inventory/my-swarm.ini
nano inventory/my-swarm.ini

# 2. 修改节点 IP 和认证信息
[all]
node1 ansible_host=192.168.1.10 ansible_user=root swarm_role=manager
node2 ansible_host=192.168.1.11 ansible_user=root swarm_role=worker
node3 ansible_host=192.168.1.12 ansible_user=root swarm_role=worker

# 3. 测试节点连接
ansible -i inventory/my-swarm.ini all -m ping

# 4. 执行部署
ansible-playbook -i inventory/my-swarm.ini swarm-deploy.yml

# 5. 检查部署状态
docker service ls
```

### 访问地址

| 服务 | 地址 | 说明 |
|------|------|------|
| MinIODB REST API | http://node1:8081 | REST API |
| MinIODB gRPC API | node1:8080 | gRPC API |
| MinIO Console | http://node2:9001 | MinIO Web 控制台 |
| MinIO Backup Console | http://node3:9003 | 备份存储控制台 |

### 集群管理

```bash
# 查看节点状态
docker node ls

# 查看服务状态
docker service ls

# 查看服务日志
docker service logs miniodb-swarm_miniodb

# 扩容服务
docker service scale miniodb-swarm_miniodb=3

# 删除服务
docker service rm miniodb-swarm_miniodb
```

### 清理集群

```bash
# 使用统一脚本清理
./deploy/deploy.sh swarm --cleanup

# 或手动清理
cd deploy/ansible
ansible-playbook -i inventory/my-swarm.ini swarm-cleanup.yml
```

## 4. Kubernetes 集群模式 (k8s)

### 场景说明

适用于生产环境、云原生部署。Kubernetes 提供 4 个 Pod（miniodb、redis、minio、minio-backup），支持高可用、自动扩缩容、滚动更新。

### 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                   Kubernetes Cluster                        │
│                  Namespace: miniodb-system                   │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Redis   │  │  MinIODB │  │   MinIO  │  │MinIO Backup│  │
│  │  Pod     │  │  (xN)    │  │   Pod    │  │    Pod     │   │
│  │ Stateful │  │ Deploy   │  │ Stateful │  │  Stateful  │   │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘   │
│       │             │             │             │           │
│       └─────────────┴─────────────┴─────────────┘           │
│                      Services (ClusterIP)                    │
│                   External Services (NodePort)               │
└─────────────────────────────────────────────────────────────┘
```

### 前置要求

- Kubernetes 1.20+
- kubectl 配置正确
- 集群至少 3 个节点
- 每个节点 4GB+ 内存
- 支持 StorageClass（用于持久化存储）

### 快速部署

```bash
# 1. 使用统一脚本部署
./deploy/deploy.sh k8s -n miniodb-system -r 3

# 2. 或手动分步部署
cd deploy/k8s
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f redis/
kubectl apply -f minio/
kubectl apply -f miniodb/
```

### 检查部署状态

```bash
# 查看 Pod 状态
kubectl get pods -n miniodb-system -w

# 查看所有资源
kubectl get all -n miniodb-system

# 查看服务
kubectl get svc -n miniodb-system

# 查看 PVC
kubectl get pvc -n miniodb-system
```

### 访问服务

```bash
# 端口转发 (开发调试)
kubectl port-forward svc/miniodb-service 8081:8081 -n miniodb-system
kubectl port-forward svc/minio-external 9001:9001 -n miniodb-system

# 通过 NodePort 访问
MINIODB_PORT=$(kubectl get svc miniodb-external -n miniodb-system -o jsonpath='{.spec.ports[0].nodePort}')
curl http://<node-ip>:$MINIODB_PORT/v1/health

# 使用 Ingress (需要配置 Ingress Controller)
# 访问: http://miniodb.yourdomain.com
```

### 扩缩容

```bash
# 扩容到 5 个副本
kubectl scale deployment miniodb --replicas=5 -n miniodb-system

# 自动扩缩容 (HPA)
kubectl autoscale deployment miniodb --cpu-percent=70 --min=2 --max=10 -n miniodb-system

# 查看 HPA 状态
kubectl get hpa -n miniodb-system
```

### 滚动更新

```bash
# 更新镜像版本
kubectl set image deployment/miniodb miniodb=miniodb:v1.1.0 -n miniodb-system

# 查看更新状态
kubectl rollout status deployment/miniodb -n miniodb-system

# 回滚到上一个版本
kubectl rollout undo deployment/miniodb -n miniodb-system

# 查看历史版本
kubectl rollout history deployment/miniodb -n miniodb-system
```

### 清理部署

```bash
# 使用统一脚本清理
./deploy/deploy.sh k8s -n miniodb-system --cleanup

# 或手动清理
kubectl delete namespace miniodb-system
```

## 配置说明

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `MINIODB_ENV` | production | 运行环境 (development/testing/production) |
| `LOG_LEVEL` | info | 日志级别 (debug/info/warn/error) |
| `REDIS_PASSWORD` | redis123 | Redis 密码 |
| `MINIO_ROOT_PASSWORD` | minioadmin123 | MinIO 管理员密码 |
| `MINIO_BACKUP_PASSWORD` | minioadmin123 | MinIO 备份密码 |
| `JWT_SECRET` | dev-secret... | JWT 签名密钥 |

### 端口配置

| 服务 | 默认端口 | 说明 |
|------|----------|------|
| gRPC API | 8080 | gRPC 服务端口 |
| REST API | 8081 | HTTP REST API |
| Metrics | 9090 | Prometheus 指标 |
| MinIO API | 9000 | MinIO S3 API |
| MinIO Console | 9001 | MinIO Web 控制台 |
| MinIO Backup API | 9002 | 备份 S3 API |
| MinIO Backup Console | 9003 | 备份 Web 控制台 |
| Redis | 6379 | Redis 数据库 |

### 资源要求

| 模式 | CPU | 内存 | 磁盘 | 节点数 |
|------|-----|------|------|--------|
| dev | 2 核 | 4GB | 20GB | 1 |
| test | 4 核 | 8GB | 50GB | 1 |
| swarm | 8 核 (总) | 12GB (总) | 100GB (总) | 3 |
| k8s | 12 核 (总) | 16GB (总) | 200GB (总) | 4+ |

## 故障排除

### 常见问题

#### 1. 服务启动失败

```bash
# 检查日志
docker logs miniodb-app
kubectl logs -l app.kubernetes.io/name=miniodb -n miniodb-system

# 检查依赖服务
docker ps
kubectl get pods -n miniodb-system
```

#### 2. 连接 Redis 失败

```bash
# 检查 Redis 状态
docker exec miniodb-dev-redis redis-cli -a redis123 ping
kubectl exec -it redis-0 -n miniodb-system -- redis-cli -a redis123 ping

# 检查密码配置
grep REDIS_PASSWORD deploy/docker/.env
kubectl get secret miniodb-secrets -n miniodb-system -o yaml
```

#### 3. MinIO 连接失败

```bash
# 检查 MinIO 状态
curl http://localhost:9000/minio/health/live

# 检查存储桶
docker exec miniodb-dev-minio-init mc ls minio/
kubectl logs -l app.kubernetes.io/name=minio-init -n miniodb-system
```

#### 4. Swarm 节点连接失败

```bash
# 检查 Swarm 状态
docker node ls

# 查看节点详情
docker node inspect node1

# 重新加入集群
docker swarm join --token <token> <manager-ip>:2377
```

#### 5. K8s Pod 无法启动

```bash
# 查看 Pod 事件
kubectl describe pod <pod-name> -n miniodb-system

# 查看 Pod 日志
kubectl logs <pod-name> -n miniodb-system --previous

# 检查资源配额
kubectl describe nodes
```

#### 6. Docker 拉取镜像超时 (i/o timeout)

若出现 `failed to do request: Head "https://registry-1.docker.io/...": dial tcp ...:443: i/o timeout`，说明无法连接 Docker Hub，常见于网络受限环境。

**方案 A：配置 Docker 镜像加速（推荐）**

- **Colima**：编辑 `~/.colima/default/colima.yaml`，在 `docker` 下增加 `registry-mirrors`，然后重启：
  ```yaml
  docker:
    registry-mirrors:
      - https://docker.1ms.run
  ```
  保存后执行 `colima stop` 再 `colima start`。

- **Docker Desktop / 系统 Docker**：在 `~/.docker/daemon.json`（Linux 为 `/etc/docker/daemon.json`）中配置：
  ```json
  {
    "registry-mirrors": ["https://docker.1ms.run"]
  }
  ```
  然后重启 Docker。

**方案 B：网络恢复后重试**

- 使用 VPN 或更换网络后再次执行 `./deploy/deploy.sh dev`。
- 或先手动拉取镜像再部署：
  ```bash
  docker pull redis:7-alpine
  docker pull minio/minio:RELEASE.2025-04-22T22-12-26Z
  docker pull minio/mc:latest
  ./deploy/deploy.sh dev
  ```

### 日志收集

```bash
# Docker Compose
docker-compose -f deploy/docker/docker-compose.yml logs --tail=100 > miniodb.log

# Docker Swarm
docker service logs miniodb-swarm_miniodb --tail=100 > miniodb.log

# Kubernetes
kubectl logs -l app.kubernetes.io/name=miniodb -n miniodb-system --tail=100 > miniodb.log
```

## 安全建议

### 生产环境配置

1. **更改默认密码**
   ```bash
   # 生成强密码
   openssl rand -base64 32
   ```

2. **启用 TLS**
   ```yaml
   # k8s/minio/minio-statefulset.yaml
   env:
     - name: MINIO_SERVER_URL
       value: "https://minio.miniodb-system.svc.cluster.local:9000"
   volumes:
     - name: certs
       secret:
         secretName: minio-certs
   ```

3. **网络隔离**
   - 使用 NetworkPolicy 限制 Pod 间通信
   - 配置防火墙规则
   - 使用 Ingress Controller 统一管理外部访问

4. **备份策略**
   ```bash
   # 定期备份到 MinIO Backup
   # 使用 k8s CronJob 配置定时备份
   ```

5. **RBAC 权限控制**
   ```yaml
   # k8s/miniodb/rbac.yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: RoleBinding
   metadata:
     name: miniodb
   subjects:
   - kind: ServiceAccount
     name: miniodb
   roleRef:
     kind: Role
     name: miniodb
   ```

## 监控和运维

### Prometheus 监控

MinIODB 内置 Prometheus 指标支持：

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'miniodb'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
            - miniodb-system
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
```

### Grafana 仪表板

导入预配置的 Grafana 仪表板监控 MinIODB 运行状态。

### 健康检查

```bash
# REST API 健康检查
curl http://localhost:8081/v1/health

# 详细状态检查
curl http://localhost:8081/v1/status

# Prometheus 指标
curl http://localhost:9090/metrics
```

### 日志聚合

推荐使用 ELK Stack 或 Loki 进行日志聚合和分析。

---

📝 **注意**: 本文档会随着项目更新而更新，请定期查看最新版本。
