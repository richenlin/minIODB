# Kubernetes 部署指南

这个目录包含了MinIODB系统在Kubernetes集群中的完整部署配置，支持生产级的高可用和水平扩展。

## 🏗️ 架构概览

Kubernetes部署包含以下组件：

```
┌─────────────────────────────────────────────────────────┐
│                Kubernetes Cluster                       │
├─────────────────────────────────────────────────────────┤
│  Namespace: miniodb-system                              │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   miniodb   │  │    redis    │  │    minio    │      │
│  │ Deployment  │  │StatefulSet  │  │StatefulSet  │      │
│  │ (2 replicas)│  │ (1 replica) │  │ (1 replica) │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │minio-backup │  │ ConfigMaps  │  │   Secrets   │      │
│  │StatefulSet  │  │             │  │             │      │
│  │ (1 replica) │  │             │  │             │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │  Services   │  │    PVCs     │  │  Ingress    │      │
│  │             │  │             │  │ (optional)  │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
└─────────────────────────────────────────────────────────┘
```

### 组件说明

- **Namespace**: `miniodb-system` - 隔离的命名空间
- **ConfigMaps**: 应用配置和Redis配置
- **Secrets**: 敏感信息（密码、密钥等）
- **StatefulSets**: 有状态服务（Redis、MinIO）
- **Deployment**: 无状态应用服务（MinIODB）
- **Services**: 服务发现和负载均衡
- **PVCs**: 持久化存储卷

## 🚀 快速开始

### 1. 环境准备

确保Kubernetes集群满足以下要求：
- Kubernetes 1.20+
- kubectl 已配置并可访问集群
- 存储类支持动态分配（推荐使用SSD）
- 至少3个工作节点（生产环境）

检查集群状态：
```bash
kubectl cluster-info
kubectl get nodes
kubectl get storageclass
```

### 2. 一键部署

使用部署脚本：
```bash
# 切换到项目根目录
cd /path/to/minIODB

# 执行一键部署
./deploy/scripts/deploy.sh k8s

# 或指定自定义命名空间
./deploy/scripts/deploy.sh -n my-namespace k8s
```

### 3. 手动部署

如果需要更精细的控制，可以手动执行：

```bash
# 1. 创建命名空间
kubectl apply -f namespace.yaml

# 2. 创建配置
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml

# 3. 部署Redis
kubectl apply -f redis/

# 4. 等待Redis就绪
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=redis -n miniodb-system --timeout=300s

# 5. 部署MinIO
kubectl apply -f minio/

# 6. 等待MinIO就绪
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio -n miniodb-system --timeout=300s
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio-backup -n miniodb-system --timeout=300s

# 7. 部署MinIODB应用
kubectl apply -f miniodb/

# 8. 等待应用就绪
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=miniodb -n miniodb-system --timeout=300s
```

### 4. 验证部署

```bash
# 检查所有Pod状态
kubectl get pods -n miniodb-system

# 检查服务状态
kubectl get services -n miniodb-system

# 检查存储卷状态
kubectl get pvc -n miniodb-system

# 执行健康检查
./deploy/scripts/health-check.sh -k
```

## 📋 配置说明

### 存储配置

默认使用 `standard` 存储类，建议根据集群情况修改：

```yaml
# 在 StatefulSet 的 volumeClaimTemplates 中修改
storageClassName: "fast-ssd"  # 替换为你的存储类
resources:
  requests:
    storage: 100Gi  # 根据需要调整大小
```

### 资源配置

默认资源配置：

| 组件 | CPU请求 | 内存请求 | CPU限制 | 内存限制 |
|------|---------|----------|---------|----------|
| MinIODB | 500m | 1Gi | 4000m | 8Gi |
| Redis | 250m | 512Mi | 1000m | 2Gi |
| MinIO | 500m | 1Gi | 2000m | 4Gi |
| MinIO Backup | 250m | 512Mi | 1000m | 2Gi |

根据实际需求调整资源配置：

```yaml
resources:
  requests:
    memory: "2Gi"
    cpu: "1000m"
  limits:
    memory: "8Gi"
    cpu: "4000m"
```

### 副本配置

```yaml
# MinIODB应用支持水平扩展
spec:
  replicas: 3  # 根据负载调整副本数

# Redis和MinIO为有状态服务，建议保持单副本
# 如需高可用，需要配置集群模式
```

## 🌐 服务访问

### 内部访问

在集群内部通过Service名称访问：

```yaml
# MinIODB服务
miniodb-service:8080    # gRPC
miniodb-service:8081    # REST
miniodb-service:9090    # Metrics

# Redis服务
redis-service:6379

# MinIO服务
minio-service:9000      # API
minio-service:9001      # Console
```

### 外部访问

#### 方式1：NodePort（默认）

```bash
# 查看NodePort端口
kubectl get services -n miniodb-system

# 访问服务（替换为实际的节点IP）
curl http://<NODE_IP>:30081/v1/health
```

#### 方式2：端口转发

```bash
# MinIODB REST API
kubectl port-forward -n miniodb-system svc/miniodb-service 8081:8081

# MinIO管理界面
kubectl port-forward -n miniodb-system svc/minio-service 9001:9001

# 然后通过localhost访问
curl http://localhost:8081/v1/health
open http://localhost:9001
```

#### 方式3：LoadBalancer

修改Service类型为LoadBalancer：

```yaml
spec:
  type: LoadBalancer  # 替换NodePort
```

#### 方式4：Ingress

创建Ingress资源：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: miniodb-ingress
  namespace: miniodb-system
spec:
  rules:
  - host: miniodb.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: miniodb-service
            port:
              number: 8081
```

## 🔧 运维管理

### 扩容操作

```bash
# 水平扩容MinIODB应用
kubectl scale deployment miniodb --replicas=5 -n miniodb-system

# 查看扩容状态
kubectl get pods -n miniodb-system -l app.kubernetes.io/name=miniodb
```

### 更新部署

```bash
# 更新镜像
kubectl set image deployment/miniodb miniodb=miniodb:v2.0.0 -n miniodb-system

# 查看更新状态
kubectl rollout status deployment/miniodb -n miniodb-system

# 回滚到上一个版本
kubectl rollout undo deployment/miniodb -n miniodb-system
```

### 日志查看

```bash
# 查看应用日志
kubectl logs -f deployment/miniodb -n miniodb-system

# 查看特定Pod日志
kubectl logs -f <pod-name> -n miniodb-system

# 查看所有容器日志
kubectl logs -f deployment/miniodb -c miniodb -n miniodb-system
```

### 配置管理

```bash
# 更新ConfigMap
kubectl apply -f configmap.yaml

# 重启Pod以应用新配置
kubectl rollout restart deployment/miniodb -n miniodb-system

# 查看配置
kubectl get configmap miniodb-config -n miniodb-system -o yaml
```

### 数据备份

```bash
# 手动触发备份
kubectl exec -n miniodb-system deployment/miniodb -- curl -X POST http://localhost:8081/v1/backup/trigger \
  -H "Content-Type: application/json" \
  -d '{"id": "user123", "day": "2024-01-15"}'

# 备份存储卷数据（PVC快照）
kubectl create volumesnapshot redis-backup --source-pvc=redis-data-redis-0 -n miniodb-system
```

## 🔒 安全配置

### RBAC权限

创建ServiceAccount和RBAC规则：

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: miniodb-sa
  namespace: miniodb-system

---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: miniodb-role
  namespace: miniodb-system
rules:
- apiGroups: [""]
  resources: ["pods", "services"]
  verbs: ["get", "list", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: miniodb-rolebinding
  namespace: miniodb-system
subjects:
- kind: ServiceAccount
  name: miniodb-sa
  namespace: miniodb-system
roleRef:
  kind: Role
  name: miniodb-role
  apiGroup: rbac.authorization.k8s.io
```

### 网络策略

限制网络访问：

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: miniodb-netpol
  namespace: miniodb-system
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/part-of: miniodb-system
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: miniodb-system
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: miniodb-system
```

### Pod安全策略

```yaml
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: miniodb-psp
spec:
  privileged: false
  allowPrivilegeEscalation: false
  requiredDropCapabilities:
    - ALL
  volumes:
    - 'configMap'
    - 'emptyDir'
    - 'projected'
    - 'secret'
    - 'downwardAPI'
    - 'persistentVolumeClaim'
  runAsUser:
    rule: 'MustRunAsNonRoot'
  seLinux:
    rule: 'RunAsAny'
  fsGroup:
    rule: 'RunAsAny'
```

## 📊 监控配置

### Prometheus监控

MinIODB内置Prometheus metrics支持，创建ServiceMonitor：

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: miniodb-metrics
  namespace: miniodb-system
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: miniodb
  endpoints:
  - port: metrics
    path: /metrics
    interval: 30s
```

### Grafana仪表板

导入预配置的Grafana仪表板：

```bash
# 获取仪表板JSON
kubectl get configmap miniodb-grafana-dashboard -n miniodb-system -o jsonpath='{.data.dashboard\.json}'
```

### 告警规则

配置Prometheus告警规则：

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: miniodb-alerts
  namespace: miniodb-system
spec:
  groups:
  - name: miniodb
    rules:
    - alert: MinIODBDown
      expr: up{job="miniodb"} == 0
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "MinIODB instance is down"
```

## 🛠️ 故障排除

### 常见问题

1. **Pod无法启动**
```bash
# 查看Pod详细信息
kubectl describe pod <pod-name> -n miniodb-system

# 查看事件
kubectl get events -n miniodb-system --sort-by='.lastTimestamp'
```

2. **存储卷问题**
```bash
# 检查PVC状态
kubectl get pvc -n miniodb-system

# 检查存储类
kubectl get storageclass

# 查看PV详情
kubectl describe pv <pv-name>
```

3. **网络连接问题**
```bash
# 测试Pod间连接
kubectl exec -n miniodb-system <pod-name> -- nslookup redis-service

# 检查Service端点
kubectl get endpoints -n miniodb-system
```

4. **资源不足**
```bash
# 检查节点资源
kubectl top nodes

# 检查Pod资源使用
kubectl top pods -n miniodb-system

# 查看资源配额
kubectl describe quota -n miniodb-system
```

### 性能优化

1. **存储优化**
```yaml
# 使用高性能存储类
storageClassName: "fast-ssd"

# 启用存储卷扩展
allowVolumeExpansion: true
```

2. **网络优化**
```yaml
# 使用主机网络（谨慎使用）
hostNetwork: true

# 配置DNS策略
dnsPolicy: ClusterFirstWithHostNet
```

3. **调度优化**
```yaml
# 节点亲和性
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: node-type
          operator: In
          values:
          - compute-optimized
```

## 📚 相关命令

### 常用kubectl命令

```bash
# 查看资源状态
kubectl get all -n miniodb-system

# 查看配置
kubectl get configmap,secret -n miniodb-system

# 查看详细信息
kubectl describe deployment miniodb -n miniodb-system

# 进入Pod调试
kubectl exec -it <pod-name> -n miniodb-system -- /bin/sh

# 拷贝文件
kubectl cp <pod-name>:/path/to/file ./local-file -n miniodb-system
```

### 管理命令

```bash
# 暂停部署
kubectl patch deployment miniodb -p '{"spec":{"replicas":0}}' -n miniodb-system

# 恢复部署
kubectl patch deployment miniodb -p '{"spec":{"replicas":2}}' -n miniodb-system

# 强制删除Pod
kubectl delete pod <pod-name> --grace-period=0 --force -n miniodb-system

# 清理资源
kubectl delete namespace miniodb-system
```

## 🔗 相关链接

- [MinIODB主项目](../../README.md)
- [Docker部署](../docker/README.md)
- [部署脚本](../scripts/README.md)
- [API使用示例](../../examples/README.md) 