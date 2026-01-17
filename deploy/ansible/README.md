# Ansible 多节点部署指南

这个目录包含了MinIODB系统的Ansible多节点部署配置，支持在多台服务器上自动化部署和配置MinIODB集群。

## 🏗️ 架构概览

Ansible部署支持两种架构模式：

### 模式1：使用现有服务
```
┌─────────────────────────────────────────────────────────┐
│                Multi-Node Cluster                       │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   Node-1    │  │   Node-2    │  │   Node-3    │      │
│  │  MinIODB    │  │  MinIODB    │  │  MinIODB    │      │
│  │ (Docker)    │  │ (Docker)    │  │ (Docker)    │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
│         │                │                │             │
│         └────────────────┼────────────────┘             │
│                          │                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │ Existing    │  │ Existing    │  │ Existing    │      │
│  │ Redis       │  │ MinIO       │  │ MinIO       │      │
│  │ Cluster     │  │ Primary     │  │ Backup      │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
└─────────────────────────────────────────────────────────┘
```

### 模式2：自动部署容器服务
```
┌─────────────────────────────────────────────────────────┐
│                Multi-Node Cluster                       │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   Node-1    │  │   Node-2    │  │   Node-3    │      │
│  │  MinIODB    │  │  MinIODB    │  │  MinIODB    │      │
│  │  Redis      │  │  MinIO      │  │ MinIO-Backup│      │
│  │ (Docker)    │  │ (Docker)    │  │ (Docker)    │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
│         │                │                │             │
│         └────────────────┼────────────────┘             │
│                          │                              │
│  ┌─────────────────────────────────────────────────────┐ │
│  │            Docker Network Bridge                    │ │
│  └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

## 🚀 快速开始

### 1. 环境准备

#### 控制节点要求
- Ansible 2.10+
- Python 3.8+
- SSH密钥访问到所有目标节点

#### 目标节点要求
- Ubuntu 20.04+ / CentOS 8+ / RHEL 8+
- Docker 20.10+
- 至少2GB内存，2核CPU
- 网络互通

### 2. 安装Ansible

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install ansible python3-pip
pip3 install ansible-core docker

# CentOS/RHEL
sudo yum install epel-release
sudo yum install ansible python3-pip
pip3 install ansible-core docker

# macOS
brew install ansible
pip3 install docker
```

### 3. 配置清单文件

#### 模式1：使用现有服务

编辑 `inventory/existing-services.yml`：

```yaml
all:
  children:
    miniodb_nodes:
      hosts:
        node1:
          ansible_host: 192.168.1.10
          miniodb_role: primary
        node2:
          ansible_host: 192.168.1.11
          miniodb_role: worker
        node3:
          ansible_host: 192.168.1.12
          miniodb_role: worker
      vars:
        # 现有Redis配置
        redis_host: "192.168.1.100"
        redis_port: 6379
        redis_password: "your-redis-password"
        
        # 现有MinIO主存储配置
        minio_endpoint: "192.168.1.101:9000"
        minio_access_key: "minioadmin"
        minio_secret_key: "minioadmin123"
        minio_bucket: "miniodb-data"
        
        # 现有MinIO备份存储配置
        minio_backup_endpoint: "192.168.1.102:9000"
        minio_backup_access_key: "minioadmin"
        minio_backup_secret_key: "minioadmin123"
        minio_backup_bucket: "miniodb-backup"
```

#### 模式2：自动部署容器服务

编辑 `inventory/auto-deploy.yml`：

```yaml
all:
  children:
    miniodb_nodes:
      hosts:
        node1:
          ansible_host: 192.168.1.10
          miniodb_role: primary
          services: ["miniodb", "redis"]
        node2:
          ansible_host: 192.168.1.11
          miniodb_role: worker
          services: ["miniodb", "minio"]
        node3:
          ansible_host: 192.168.1.12
          miniodb_role: worker
          services: ["miniodb", "minio-backup"]
      vars:
        # 自动生成的服务配置
        auto_deploy_services: true
        docker_network: "miniodb-network"
        data_path: "/opt/miniodb/data"
```

### 4. 配置变量

编辑 `group_vars/all.yml`：

```yaml
# MinIODB应用配置
miniodb:
  version: "latest"
  log_level: "info"
  grpc_port: 8080
  rest_port: 8081
  metrics_port: 9090
  
# 认证配置
auth:
  mode: "jwt"
  jwt_secret: "{{ vault_jwt_secret }}"
  jwt_expiration: "24h"

# 缓冲区配置
buffer:
  size: 1000
  timeout: "30s"
  batch_size: 100

# 监控配置
monitoring:
  enabled: true
  prometheus_port: 9090
```

### 5. 执行部署

#### 模式1：使用现有服务
```bash
# 检查连通性
ansible -i inventory/existing-services.yml all -m ping

# 执行部署
ansible-playbook -i inventory/existing-services.yml site.yml

# 或使用标签部署特定组件
ansible-playbook -i inventory/existing-services.yml site.yml --tags miniodb
```

#### 模式2：自动部署容器服务
```bash
# 检查连通性
ansible -i inventory/auto-deploy.yml all -m ping

# 执行完整部署
ansible-playbook -i inventory/auto-deploy.yml site.yml

# 或分步部署
ansible-playbook -i inventory/auto-deploy.yml site.yml --tags docker
ansible-playbook -i inventory/auto-deploy.yml site.yml --tags services
ansible-playbook -i inventory/auto-deploy.yml site.yml --tags miniodb
```

## 📁 目录结构

```
deploy/ansible/
├── README.md                    # 本文档
├── site.yml                     # 主playbook
├── requirements.yml             # Ansible依赖
├── ansible.cfg                  # Ansible配置
├── inventory/                   # 清单文件
│   ├── existing-services.yml    # 现有服务模式清单
│   ├── auto-deploy.yml         # 自动部署模式清单
│   └── group_vars/             # 组变量
│       ├── all.yml             # 全局变量
│       └── miniodb_nodes.yml   # MinIODB节点变量
├── host_vars/                  # 主机变量
├── roles/                      # Ansible角色
│   ├── docker/                 # Docker安装和配置
│   ├── redis/                  # Redis容器部署
│   ├── minio/                  # MinIO容器部署
│   ├── miniodb/                # MinIODB应用部署
│   └── monitoring/             # 监控配置
├── playbooks/                  # 专用playbook
│   ├── deploy.yml              # 部署playbook
│   ├── scale.yml               # 扩容playbook
│   ├── backup.yml              # 备份playbook
│   └── health-check.yml        # 健康检查playbook
├── templates/                  # 模板文件
│   ├── docker-compose.yml.j2   # Docker Compose模板
│   ├── config.yaml.j2          # MinIODB配置模板
│   └── nginx.conf.j2           # 负载均衡配置
└── scripts/                    # 辅助脚本
    ├── deploy-ansible.sh       # 一键部署脚本
    ├── scale-cluster.sh        # 集群扩容脚本
    └── health-check.sh         # 健康检查脚本
```

## ⚙️ 配置选项

### 服务配置

```yaml
# group_vars/all.yml

# Docker配置
docker:
  version: "20.10"
  data_root: "/var/lib/docker"
  log_driver: "json-file"
  log_opts:
    max-size: "10m"
    max-file: "3"

# Redis配置（自动部署模式）
redis:
  image: "redis:7-alpine"
  port: 6379
  password: "{{ vault_redis_password }}"
  memory_limit: "2g"
  data_path: "{{ data_path }}/redis"

# MinIO配置（自动部署模式）
minio:
  image: "minio/minio:RELEASE.2025-04-22T22-12-26Z"
  api_port: 9000
  console_port: 9001
  access_key: "{{ vault_minio_access_key }}"
  secret_key: "{{ vault_minio_secret_key }}"
  data_path: "{{ data_path }}/minio"

# MinIODB配置
miniodb:
  image: "miniodb:latest"
  build_from_source: false
  config_path: "/opt/miniodb/config"
  log_path: "/opt/miniodb/logs"
  data_path: "{{ data_path }}/miniodb"
```

### 网络配置

```yaml
# 网络设置
network:
  docker_network: "miniodb-network"
  subnet: "172.20.0.0/16"
  
# 防火墙配置
firewall:
  enabled: true
  allowed_ports:
    - 8080  # gRPC
    - 8081  # REST
    - 9090  # Metrics
    - 6379  # Redis (如果开放)
    - 9000  # MinIO API (如果开放)
    - 9001  # MinIO Console (如果开放)
```

### 安全配置

```yaml
# 安全设置
security:
  ssl_enabled: false
  ssl_cert_path: "/opt/miniodb/certs"
  
# 用户配置
miniodb_user: "miniodb"
miniodb_group: "miniodb"
miniodb_uid: 1001
miniodb_gid: 1001
```

## 🔧 运维操作

### 集群管理

```bash
# 查看集群状态
ansible -i inventory/auto-deploy.yml all -m shell -a "docker ps"

# 重启MinIODB服务
ansible-playbook -i inventory/auto-deploy.yml playbooks/restart.yml

# 更新MinIODB应用
ansible-playbook -i inventory/auto-deploy.yml playbooks/update.yml

# 扩容集群
ansible-playbook -i inventory/auto-deploy.yml playbooks/scale.yml -e "new_nodes=['node4','node5']"
```

### 配置管理

```bash
# 更新配置
ansible-playbook -i inventory/auto-deploy.yml site.yml --tags config

# 重新加载配置
ansible -i inventory/auto-deploy.yml miniodb_nodes -m shell -a "docker restart miniodb-app"
```

### 监控和日志

```bash
# 收集日志
ansible-playbook -i inventory/auto-deploy.yml playbooks/collect-logs.yml

# 健康检查
ansible-playbook -i inventory/auto-deploy.yml playbooks/health-check.yml

# 性能监控
ansible -i inventory/auto-deploy.yml all -m uri -a "url=http://{{ ansible_host }}:9090/metrics"
```

### 备份和恢复

```bash
# 执行备份
ansible-playbook -i inventory/auto-deploy.yml playbooks/backup.yml

# 恢复数据
ansible-playbook -i inventory/auto-deploy.yml playbooks/restore.yml -e "backup_date=2024-01-15"
```

## 🔒 安全最佳实践

### 1. 使用Ansible Vault

```bash
# 创建加密变量文件
ansible-vault create group_vars/all/vault.yml

# 编辑内容
vault_jwt_secret: "super-secret-jwt-key"
vault_redis_password: "strong-redis-password"
vault_minio_access_key: "minio-access-key"
vault_minio_secret_key: "minio-secret-key"

# 使用vault运行playbook
ansible-playbook -i inventory/auto-deploy.yml site.yml --ask-vault-pass
```

### 2. SSH密钥配置

```bash
# 生成SSH密钥
ssh-keygen -t rsa -b 4096 -f ~/.ssh/miniodb_rsa

# 分发公钥到所有节点
ssh-copy-id -i ~/.ssh/miniodb_rsa.pub user@node1
ssh-copy-id -i ~/.ssh/miniodb_rsa.pub user@node2
ssh-copy-id -i ~/.ssh/miniodb_rsa.pub user@node3

# 配置ansible.cfg
[defaults]
private_key_file = ~/.ssh/miniodb_rsa
```

### 3. 防火墙配置

```yaml
# 在playbook中配置防火墙
- name: Configure firewall
  ufw:
    rule: allow
    port: "{{ item }}"
    proto: tcp
  loop:
    - 8080
    - 8081
    - 9090
  tags: firewall
```

## 🛠️ 故障排除

### 常见问题

1. **连接超时**
```bash
# 检查SSH连通性
ansible -i inventory/auto-deploy.yml all -m ping

# 检查防火墙状态
ansible -i inventory/auto-deploy.yml all -m shell -a "ufw status"
```

2. **Docker服务问题**
```bash
# 检查Docker状态
ansible -i inventory/auto-deploy.yml all -m shell -a "systemctl status docker"

# 重启Docker服务
ansible -i inventory/auto-deploy.yml all -m systemd -a "name=docker state=restarted"
```

3. **容器启动失败**
```bash
# 查看容器日志
ansible -i inventory/auto-deploy.yml all -m shell -a "docker logs miniodb-app"

# 检查容器状态
ansible -i inventory/auto-deploy.yml all -m shell -a "docker ps -a"
```

### 调试模式

```bash
# 启用详细输出
ansible-playbook -i inventory/auto-deploy.yml site.yml -vvv

# 检查模式（不执行实际操作）
ansible-playbook -i inventory/auto-deploy.yml site.yml --check

# 差异模式
ansible-playbook -i inventory/auto-deploy.yml site.yml --diff
```

## 📊 性能优化

### 1. 并发控制

```yaml
# ansible.cfg
[defaults]
forks = 10
host_key_checking = False
timeout = 30

[ssh_connection]
ssh_args = -o ControlMaster=auto -o ControlPersist=60s
pipelining = True
```

### 2. 资源优化

```yaml
# 在playbook中设置资源限制
- name: Deploy MinIODB with resource limits
  docker_container:
    name: miniodb-app
    image: miniodb:latest
    memory: "4g"
    cpus: "2.0"
    ulimits:
      - "nofile:65536:65536"
```

### 3. 网络优化

```yaml
# 配置Docker网络
- name: Create custom network
  docker_network:
    name: miniodb-network
    driver: bridge
    ipam_config:
      - subnet: "172.20.0.0/16"
        gateway: "172.20.0.1"
```

## 🔗 相关链接

- [Ansible官方文档](https://docs.ansible.com/)
- [Docker Ansible模块](https://docs.ansible.com/ansible/latest/collections/community/docker/)
- [MinIODB主项目](../../README.md)
- [Docker部署](../docker/README.md)
- [Kubernetes部署](../k8s/README.md) 