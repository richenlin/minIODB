# Docker Compose 部署指南

这个目录包含了MinIODB系统的Docker Compose部署配置，支持单机环境快速启动完整的系统实例。

## 🏗️ 架构概览

Docker Compose部署包含以下服务：

```
┌─────────────────────────────────────────────────────────┐
│                Docker Compose Stack                     │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   miniodb   │  │    redis    │  │    minio    │      │
│  │   :8080     │  │    :6379    │  │   :9000     │      │
│  │   :8081     │  │             │  │   :9001     │      │
│  │   :9090     │  │             │  │             │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐                       │
│  │minio-backup │  │ minio-init  │                       │
│  │   :9002     │  │ (init-only) │                       │
│  │   :9003     │  │             │                       │
│  └─────────────┘  └─────────────┘                       │
├─────────────────────────────────────────────────────────┤
│              miniodb-network (172.20.0.0/16)           │
└─────────────────────────────────────────────────────────┘
```

### 服务说明

- **miniodb**: 核心应用服务，提供gRPC和REST API
- **redis**: 元数据中心，负责服务发现和数据索引
- **minio**: 主存储服务，提供S3兼容的对象存储
- **minio-backup**: 备份存储服务，用于数据备份和恢复
- **minio-init**: 初始化服务，创建存储桶和配置

## 🚀 快速开始

### 1. 环境准备

确保系统已安装：
- Docker 20.10+
- Docker Compose 2.0+

检查端口可用性：
```bash
# 检查必要端口是否被占用
netstat -tlnp | grep -E ':(6379|8080|8081|9000|9001|9002|9003|9090)'
```

### 2. 配置环境变量

```bash
# 复制环境变量模板
cp env.example .env

# 编辑配置文件
vim .env
```

**重要配置项：**
```bash
# 修改默认密码 (生产环境必须)
REDIS_PASSWORD=your-strong-redis-password
MINIO_ROOT_PASSWORD=your-strong-minio-password
JWT_SECRET=your-super-secret-jwt-key

# 设置数据存储路径
DATA_PATH=/path/to/your/data
```

### 3. 创建数据目录

```bash
# 创建数据存储目录
mkdir -p data/{redis,minio,minio-backup,logs}

# 设置权限 (如果需要)
chmod -R 755 data/
```

### 4. 启动服务

```bash
# 启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f
```

### 5. 验证部署

```bash
# 检查服务健康状态
curl http://localhost:8081/v1/health

# 访问MinIO管理界面
open http://localhost:9001

# 访问备份MinIO管理界面
open http://localhost:9003
```

## 📋 服务端点

### API服务

| 服务 | 端点 | 说明 |
|------|------|------|
| REST API | http://localhost:8081 | HTTP RESTful API |
| gRPC API | localhost:8080 | gRPC Protocol Buffers API |
| Metrics | http://localhost:9090/metrics | Prometheus监控指标 |
| Health Check | http://localhost:8081/v1/health | 健康检查端点 |

### 管理界面

| 服务 | 端点 | 默认账号 |
|------|------|----------|
| MinIO Console | http://localhost:9001 | minioadmin / minioadmin123 |
| MinIO Backup Console | http://localhost:9003 | minioadmin / minioadmin123 |

### 数据库连接

| 服务 | 端点 | 认证 |
|------|------|------|
| Redis | localhost:6379 | 密码: redis123 |

## 🔧 常用操作

### 服务管理

```bash
# 启动服务
docker-compose up -d

# 停止服务
docker-compose down

# 重启服务
docker-compose restart

# 查看服务状态
docker-compose ps

# 查看资源使用情况
docker-compose top
```

### 日志管理

```bash
# 查看所有服务日志
docker-compose logs

# 查看特定服务日志
docker-compose logs miniodb
docker-compose logs redis
docker-compose logs minio

# 实时跟踪日志
docker-compose logs -f miniodb

# 查看最近100行日志
docker-compose logs --tail=100 miniodb
```

### 数据管理

```bash
# 备份数据
docker-compose exec miniodb curl -X POST http://localhost:8081/v1/backup/trigger \
  -H "Content-Type: application/json" \
  -d '{"id": "user123", "day": "2024-01-15"}'

# 查看存储使用情况
docker-compose exec minio mc du minio/miniodb-data
docker-compose exec minio-backup mc du minio-backup/miniodb-backup

# 数据库操作
docker-compose exec redis redis-cli -a redis123 info
```

### 扩容操作

```bash
# 水平扩容MinIODB服务
docker-compose up -d --scale miniodb=3

# 查看扩容后的实例
docker-compose ps miniodb
```

## ⚙️ 配置说明

### 环境变量配置

主要配置项说明：

```bash
# 基础配置
MINIODB_ENV=production          # 运行环境
MINIODB_LOG_LEVEL=info         # 日志级别
DATA_PATH=./data               # 数据存储路径

# 认证配置
AUTH_MODE=jwt                  # 认证模式
JWT_SECRET=<strong-secret>     # JWT密钥
REDIS_PASSWORD=<password>      # Redis密码

# 存储配置
MINIO_ROOT_USER=minioadmin     # MinIO用户名
MINIO_ROOT_PASSWORD=<password> # MinIO密码
MINIO_BUCKET=miniodb-data      # 主存储桶
MINIO_BACKUP_BUCKET=miniodb-backup # 备份存储桶

# 性能配置
BUFFER_SIZE=1000               # 缓冲区大小
BUFFER_TIMEOUT=30s             # 缓冲区超时
BATCH_SIZE=100                 # 批处理大小
```

### 自定义配置

如需自定义配置，可以：

1. **修改配置文件**：编辑 `config/config.yaml`
2. **环境变量覆盖**：在 `.env` 文件中添加环境变量
3. **挂载配置**：将自定义配置文件挂载到容器

### 网络配置

默认网络配置：
- 网络名称: `miniodb-network`
- 子网: `172.20.0.0/16`
- 驱动: `bridge`

如需修改网络配置，编辑 `docker-compose.yml` 中的 `networks` 部分。

## 🔒 安全配置

### 生产环境安全检查清单

- [ ] 修改所有默认密码
- [ ] 使用强JWT密钥
- [ ] 配置TLS证书
- [ ] 限制网络访问
- [ ] 启用访问日志
- [ ] 配置防火墙规则

### TLS配置

1. **生成证书**：
```bash
# 创建证书目录
mkdir -p certs

# 生成自签名证书 (开发环境)
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout certs/server.key -out certs/server.crt
```

2. **配置环境变量**：
```bash
ENABLE_TLS=true
TLS_CERT_PATH=/app/certs/server.crt
TLS_KEY_PATH=/app/certs/server.key
```

### 访问控制

配置网络访问限制：
```yaml
# 在docker-compose.yml中添加
services:
  miniodb:
    ports:
      - "127.0.0.1:8080:8080"  # 仅本地访问
      - "127.0.0.1:8081:8081"  # 仅本地访问
```

## 📊 监控配置

### Prometheus监控

MinIODB内置Prometheus metrics支持：

```bash
# 访问监控指标
curl http://localhost:9090/metrics

# 查看可用指标
curl http://localhost:9090/metrics | grep miniodb_
```

### 健康检查

```bash
# 应用健康检查
curl http://localhost:8081/v1/health

# 服务健康检查
docker-compose ps --filter health=healthy
```

### 日志监控

配置日志收集：
```yaml
# 在docker-compose.yml中配置
services:
  miniodb:
    logging:
      driver: "fluentd"
      options:
        fluentd-address: "localhost:24224"
        tag: "miniodb"
```

## 🛠️ 故障排除

### 常见问题

1. **端口被占用**
```bash
# 查找占用端口的进程
sudo lsof -i :8080
sudo netstat -tlnp | grep :8080

# 解决方案：修改端口映射或停止占用进程
```

2. **数据目录权限问题**
```bash
# 检查目录权限
ls -la data/

# 修复权限
sudo chown -R $USER:$USER data/
chmod -R 755 data/
```

3. **服务启动失败**
```bash
# 查看详细错误信息
docker-compose logs miniodb

# 检查配置文件
docker-compose config

# 重新构建镜像
docker-compose build --no-cache miniodb
```

4. **内存不足**
```bash
# 检查系统资源
docker stats

# 调整内存限制
# 在docker-compose.yml中添加:
deploy:
  resources:
    limits:
      memory: 2G
```

### 性能优化

1. **存储优化**
```bash
# 使用SSD存储
DATA_PATH=/path/to/ssd/storage

# 配置存储驱动
# 在docker-compose.yml中:
volumes:
  minio_data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /path/to/fast/storage
```

2. **网络优化**
```bash
# 使用host网络模式 (生产环境谨慎使用)
network_mode: "host"

# 调整网络参数
sysctls:
  - net.core.somaxconn=65535
```

3. **内存优化**
```yaml
# 在docker-compose.yml中配置
environment:
  - DUCKDB_MEMORY_LIMIT=4GB
  - REDIS_MAXMEMORY=2gb
```

## 📚 相关命令

### Docker Compose常用命令

```bash
# 查看配置
docker-compose config

# 拉取最新镜像
docker-compose pull

# 重新构建镜像
docker-compose build

# 清理未使用的资源
docker system prune -f

# 导出/导入配置
docker-compose config > docker-compose-export.yml
```

### 数据操作命令

```bash
# 数据备份
docker-compose exec miniodb /app/scripts/backup.sh

# 数据恢复
docker-compose exec miniodb /app/scripts/restore.sh

# 数据清理
docker-compose down -v  # 删除所有数据
```

## 🔗 相关链接

- [MinIODB主项目](../../README.md)
- [API使用示例](../../examples/README.md)
- [Kubernetes部署](../k8s/README.md)
- [部署脚本](../scripts/README.md) 