# MinIODB Docker Compose 部署指南

这个目录包含了MinIODB系统的Docker Compose部署配置，支持单机环境快速启动完整的系统实例。现已支持**多架构自动检测部署**，包括AMD64和ARM64架构。

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

## 🔧 多架构支持

MinIODB现在支持以下CPU架构的自动检测和部署：

- **AMD64 (x86_64)** - Intel/AMD 64位处理器
- **ARM64 (aarch64)** - ARM 64位处理器 (Apple Silicon M1/M2/M3, ARM服务器)

### 文件结构

```
deploy/docker/
├── Dockerfile          # AMD64架构专用Dockerfile
├── Dockerfile.arm      # ARM64架构专用Dockerfile
├── docker-compose.yml  # 多架构Docker Compose配置
├── detect-arch.sh      # 架构自动检测脚本
├── start.sh           # 多架构自动启动脚本
├── .env               # 环境配置文件
├── .env.arch          # 自动生成的架构配置
└── .env.merged        # 合并后的完整配置
```

### 架构检测原理

架构检测脚本通过以下步骤自动选择合适的Dockerfile：

1. 使用 `uname -m` 获取系统架构
2. 根据架构映射选择相应的Dockerfile：
   - `x86_64` → `Dockerfile` (AMD64)
   - `arm64`/`aarch64` → `Dockerfile.arm` (ARM64)
3. 生成架构特定的环境变量配置
4. 自动选择对应的镜像标签和构建平台

## 🚀 快速开始

### 方法一：多架构自动启动（推荐）

使用智能启动脚本，自动检测系统架构并选择合适的Dockerfile：

```bash
# 使用自动启动脚本（推荐）
./start.sh

# 强制重新构建镜像
./start.sh up --force-rebuild

# 查看帮助信息
./start.sh --help
```

### 方法二：手动架构检测

```bash
# 1. 检测系统架构
./detect-arch.sh

# 2. 查看生成的架构配置
cat .env.arch

# 3. 启动服务
docker-compose --env-file .env.merged up --build -d
```

### 方法三：传统部署方式

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

### 多架构启动脚本

智能启动脚本支持以下命令：

```bash
# 启动所有服务（默认）
./start.sh

# 停止所有服务
./start.sh down

# 重启所有服务
./start.sh restart

# 仅构建镜像
./start.sh build

# 查看日志
./start.sh logs

# 查看服务状态
./start.sh status

# 清理资源
./start.sh clean
```

### 启动脚本选项

| 选项 | 说明 |
|------|------|
| `--force-rebuild` | 强制重新构建镜像 |
| `--no-cache` | 构建时不使用缓存 |
| `--detach` | 后台运行（默认） |
| `--foreground` | 前台运行 |
| `-h, --help` | 显示帮助信息 |

### 传统服务管理

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

### 多架构相关问题

1. **架构检测失败**

**问题**: `不支持的架构: xxx`

**解决方案**:
```bash
# 检查系统架构
uname -m

# 手动设置架构（如果支持但未识别）
export DOCKERFILE=Dockerfile      # 或 Dockerfile.arm
export PLATFORM_TAG=amd64         # 或 arm64
```

2. **Docker构建失败**

**问题**: `failed to solve: process "/bin/sh -c go build..."`

**解决方案**:
```bash
# 清理Docker缓存
docker builder prune -f

# 强制重新构建
./start.sh up --force-rebuild --no-cache
```

3. **CGO编译错误**

**问题**: `gcc: error: unrecognized command-line option`

**解决方案**:
- 确保使用正确的架构Dockerfile
- 检查Docker Desktop是否支持目标架构
- 尝试使用 `--no-cache` 选项重新构建

### 常见问题

4. **端口被占用**
```bash
# 查找占用端口的进程
sudo lsof -i :8080
sudo netstat -tlnp | grep :8080

# 解决方案：修改端口映射或停止占用进程
```

5. **数据目录权限问题**
```bash
# 检查目录权限
ls -la data/

# 修复权限
sudo chown -R $USER:$USER data/
chmod -R 755 data/
```

6. **服务启动失败**
```bash
# 查看详细错误信息
./start.sh logs miniodb

# 检查配置文件
docker-compose config

# 重新构建镜像
./start.sh build --no-cache
```

7. **内存不足**
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

8. **服务启动超时**

**问题**: 服务健康检查失败

**解决方案**:
```bash
# 查看详细日志
./start.sh logs miniodb

# 检查端口占用
netstat -tlnp | grep -E ':(8080|8081|9000|9001|6379)'

# 重启服务
./start.sh restart
```

### 调试模式

启用详细日志进行调试：

```bash
# 设置调试模式
export DEBUG=1

# 前台运行查看详细输出
./start.sh up --foreground

# 查看架构检测详情
./detect-arch.sh
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

## 🔄 版本兼容性

### 支持的版本

- **Docker**: 20.10+
- **Docker Compose**: 2.0+
- **Go**: 1.24+
- **系统**: Linux, macOS

### 测试环境

以下环境已经过完整测试：

| 系统 | 架构 | Docker版本 | 状态 |
|------|------|------------|------|
| macOS Monterey+ | ARM64 (M1/M2/M3) | 24.0+ | ✅ 完全支持 |
| Ubuntu 20.04+ | AMD64 | 20.10+ | ✅ 完全支持 |
| Ubuntu 20.04+ | ARM64 | 20.10+ | ✅ 完全支持 |
| CentOS 8+ | AMD64 | 20.10+ | ✅ 完全支持 |

## 🔗 相关链接

- [MinIODB主项目](../../README.md)
- [性能测试文档](../../test/performance/README.md)
- [API使用示例](../../examples/README.md)
- [Kubernetes部署](../k8s/README.md)
- [部署脚本](../scripts/README.md)

## 🆘 获取帮助

如果遇到问题，请：

1. 查看 [故障排除](#故障排除) 部分
2. 检查 [常见问题](#常见问题)
3. 查看详细日志：`./start.sh logs`
4. 提交issue到项目仓库

---