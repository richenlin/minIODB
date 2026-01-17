# MinIODB Kubernetes 部署指南

本目录包含在Kubernetes集群中部署MinIODB系统的所有配置文件和脚本。

## 系统架构

MinIODB系统包含以下组件：

- **MinIODB应用**: 主要的应用程序，提供HTTP和gRPC API
- **Redis**: 缓存和会话存储
- **MinIO**: 主要的对象存储服务
- **MinIO Backup**: 备份对象存储服务

## 目录结构

```
k8s/
├── deploy.sh                 # 自动化部署脚本
├── miniodb-config.yaml       # 命名空间、ConfigMap和Secrets
├── miniodb-deployment.yaml   # MinIODB应用部署配置
├── redis/
│   ├── redis-config.yaml     # Redis配置
│   ├── redis-service.yaml    # Redis服务配置
│   └── redis-statefulset.yaml # Redis StatefulSet配置
├── minio/
│   ├── minio-init-job.yaml   # MinIO初始化Job
│   ├── minio-service.yaml    # MinIO服务配置
│   └── minio-statefulset.yaml # MinIO StatefulSet配置
└── README.md                 # 本文档
```

## 前置要求

1. **Kubernetes集群**: 版本 1.20+
2. **kubectl**: 已配置并能访问集群
3. **存储类**: 集群中需要有可用的存储类 (默认使用 `standard`)
4. **容器镜像**: 确保以下镜像可用：
   - `miniodb:latest` (需要先构建应用镜像)
   - `redis:7-alpine`
   - `minio/minio:RELEASE.2025-04-22T22-12-26Z`
   - `minio/mc:latest`

## 快速部署

### 1. 使用自动化脚本部署

```bash
# 部署整个系统
./deploy.sh

# 查看部署状态
./deploy.sh status

# 清理部署
./deploy.sh cleanup
```

### 2. 手动部署

```bash
# 1. 创建命名空间和配置
kubectl apply -f miniodb-config.yaml

# 2. 部署Redis
kubectl apply -f redis/redis-config.yaml
kubectl apply -f redis/redis-service.yaml
kubectl apply -f redis/redis-statefulset.yaml

# 3. 部署MinIO
kubectl apply -f minio/minio-service.yaml
kubectl apply -f minio/minio-statefulset.yaml

# 4. 初始化MinIO存储桶
kubectl apply -f minio/minio-init-job.yaml

# 5. 部署MinIODB应用
kubectl apply -f miniodb-deployment.yaml
```

## 配置说明

### 环境变量配置

主要配置通过ConfigMap和Secrets管理：

**ConfigMap (miniodb-config)**:
- Redis连接配置
- MinIO存储桶配置
- 应用服务器配置
- 日志配置
- 限流配置

**Secrets (miniodb-secrets)**:
- Redis密码
- MinIO访问密钥
- JWT签名密钥

### 存储配置

- **Redis**: 10Gi PVC，用于持久化数据
- **MinIO**: 50Gi PVC，用于对象存储
- **MinIO Backup**: 50Gi PVC，用于备份存储

### 资源配置

**Redis**:
- 请求: 512Mi内存, 250m CPU
- 限制: 2Gi内存, 1000m CPU

**MinIO**:
- 请求: 1Gi内存, 500m CPU
- 限制: 4Gi内存, 2000m CPU

**MinIODB应用**:
- 请求: 1Gi内存, 500m CPU
- 限制: 2Gi内存, 1000m CPU

## 网络配置

### 内部服务

- **Redis**: `redis-service.miniodb-system.svc.cluster.local:6379`
- **MinIO**: `minio-service.miniodb-system.svc.cluster.local:9000`
- **MinIO Backup**: `minio-backup-service.miniodb-system.svc.cluster.local:9000`
- **MinIODB**: `miniodb-service.miniodb-system.svc.cluster.local:8080`

### 外部访问 (NodePort)

- **MinIODB HTTP API**: `NodeIP:30080`
- **MinIODB gRPC API**: `NodeIP:30090`
- **MinIO API**: `NodeIP:30900`
- **MinIO Console**: `NodeIP:30901`
- **MinIO Backup API**: `NodeIP:30902`
- **MinIO Backup Console**: `NodeIP:30903`

## 监控和健康检查

### 健康检查端点

- **MinIODB**: 
  - Liveness: `GET /health`
  - Readiness: `GET /ready`
- **Redis**: `redis-cli ping`
- **MinIO**: `GET /minio/health/live`

### 日志查看

```bash
# 查看应用日志
kubectl logs -f deployment/miniodb -n miniodb-system

# 查看Redis日志
kubectl logs -f statefulset/redis -n miniodb-system

# 查看MinIO日志
kubectl logs -f statefulset/minio -n miniodb-system

# 查看初始化日志
kubectl logs job/minio-init -n miniodb-system
```

## 故障排除

### 常见问题

1. **Pod启动失败**
   ```bash
   kubectl describe pod <pod-name> -n miniodb-system
   kubectl logs <pod-name> -n miniodb-system
   ```

2. **存储问题**
   ```bash
   kubectl get pvc -n miniodb-system
   kubectl describe pvc <pvc-name> -n miniodb-system
   ```

3. **网络连接问题**
   ```bash
   kubectl get svc -n miniodb-system
   kubectl get endpoints -n miniodb-system
   ```

4. **配置问题**
   ```bash
   kubectl get configmap -n miniodb-system
   kubectl get secret -n miniodb-system
   ```

### 调试命令

```bash
# 进入Pod进行调试
kubectl exec -it <pod-name> -n miniodb-system -- /bin/bash

# 端口转发进行本地测试
kubectl port-forward svc/miniodb-service 8080:8080 -n miniodb-system
kubectl port-forward svc/minio-service 9000:9000 -n miniodb-system
```

## 升级和维护

### 应用升级

```bash
# 更新应用镜像
kubectl set image deployment/miniodb miniodb=miniodb:new-version -n miniodb-system

# 查看升级状态
kubectl rollout status deployment/miniodb -n miniodb-system

# 回滚到上一个版本
kubectl rollout undo deployment/miniodb -n miniodb-system
```

### 扩缩容

```bash
# 扩展MinIODB应用副本
kubectl scale deployment miniodb --replicas=3 -n miniodb-system

# 查看扩缩容状态
kubectl get deployment miniodb -n miniodb-system
```

### 备份和恢复

```bash
# 备份Redis数据
kubectl exec -it redis-0 -n miniodb-system -- redis-cli --rdb /data/backup.rdb

# 备份MinIO数据
kubectl exec -it minio-0 -n miniodb-system -- mc mirror minio/miniodb-data /backup/
```

## 安全考虑

1. **密钥管理**: 生产环境中应使用外部密钥管理系统
2. **网络策略**: 建议配置NetworkPolicy限制Pod间通信
3. **RBAC**: 配置适当的角色和权限
4. **TLS加密**: 生产环境中应启用TLS
5. **镜像安全**: 使用受信任的镜像仓库和镜像扫描

## 性能优化

1. **资源限制**: 根据实际负载调整资源配置
2. **存储优化**: 使用高性能存储类
3. **网络优化**: 配置适当的网络策略
4. **缓存策略**: 优化Redis缓存配置
5. **副本数量**: 根据负载调整副本数量

## 支持

如有问题，请查看：
1. 应用日志
2. Kubernetes事件
3. 资源使用情况
4. 网络连接状态

更多信息请参考项目文档或联系开发团队。 