#!/bin/bash

# 离线二进制包下载脚本
# 支持多架构下载MinIO、Redis、mc等组件

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 配置变量
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ANSIBLE_DIR="$(dirname "$SCRIPT_DIR")"
FILES_DIR="$ANSIBLE_DIR/files"
DOWNLOAD_DIR="$FILES_DIR/downloads"

# 版本配置
MINIO_VERSION="RELEASE.2024-01-16T16-07-38Z"
REDIS_VERSION="7.2.4"
MC_VERSION="RELEASE.2024-01-16T16-06-34Z"
MINIODB_VERSION="latest"

# 支持的架构
ARCHITECTURES=("amd64" "arm64")

# 创建目录结构
create_directories() {
    log_info "Creating directory structure..."
    
    for arch in "${ARCHITECTURES[@]}"; do
        mkdir -p "$FILES_DIR/$arch/bin"
        mkdir -p "$FILES_DIR/$arch/configs"
        mkdir -p "$FILES_DIR/$arch/systemd"
    done
    
    mkdir -p "$DOWNLOAD_DIR"
    mkdir -p "$FILES_DIR/configs"
    
    log_success "Directory structure created"
}

# 下载MinIO二进制
download_minio() {
    local arch=$1
    log_info "Downloading MinIO for $arch..."
    
    local url="https://dl.min.io/server/minio/release/linux-$arch/minio"
    local target="$FILES_DIR/$arch/bin/minio"
    
    if [ -f "$target" ]; then
        log_warning "MinIO binary for $arch already exists, skipping..."
        return 0
    fi
    
    if curl -fsSL "$url" -o "$target"; then
        chmod +x "$target"
        log_success "MinIO for $arch downloaded successfully"
    else
        log_error "Failed to download MinIO for $arch"
        return 1
    fi
}

# 下载MinIO Client (mc)
download_mc() {
    local arch=$1
    log_info "Downloading MinIO Client (mc) for $arch..."
    
    local url="https://dl.min.io/client/mc/release/linux-$arch/mc"
    local target="$FILES_DIR/$arch/bin/mc"
    
    if [ -f "$target" ]; then
        log_warning "mc binary for $arch already exists, skipping..."
        return 0
    fi
    
    if curl -fsSL "$url" -o "$target"; then
        chmod +x "$target"
        log_success "mc for $arch downloaded successfully"
    else
        log_error "Failed to download mc for $arch"
        return 1
    fi
}

# 下载Redis二进制
download_redis() {
    local arch=$1
    log_info "Downloading Redis for $arch..."
    
    local redis_dir="$DOWNLOAD_DIR/redis-$REDIS_VERSION-$arch"
    local target_dir="$FILES_DIR/$arch/redis"
    
    if [ -d "$target_dir" ]; then
        log_warning "Redis for $arch already exists, skipping..."
        return 0
    fi
    
    # Redis需要编译，这里提供预编译包下载地址
    # 实际生产环境中，可能需要从可信源下载或自行编译
    local url="http://download.redis.io/releases/redis-$REDIS_VERSION.tar.gz"
    local tar_file="$DOWNLOAD_DIR/redis-$REDIS_VERSION.tar.gz"
    
    # 下载源码包
    if [ ! -f "$tar_file" ]; then
        if curl -fsSL "$url" -o "$tar_file"; then
            log_success "Redis source downloaded"
        else
            log_error "Failed to download Redis source"
            return 1
        fi
    fi
    
    # 解压并准备
    mkdir -p "$redis_dir"
    tar -xzf "$tar_file" -C "$redis_dir" --strip-components=1
    
    # 创建编译脚本
    cat > "$redis_dir/build.sh" << 'EOF'
#!/bin/bash
set -e
make && make install PREFIX=./install
EOF
    chmod +x "$redis_dir/build.sh"
    
    # 移动到目标目录
    mv "$redis_dir" "$target_dir"
    
    log_success "Redis source prepared for $arch"
}

# 下载MinIODB二进制（如果有预编译版本）
download_miniodb() {
    local arch=$1
    log_info "Preparing MinIODB for $arch..."
    
    local target="$FILES_DIR/$arch/bin/miniodb"
    local source_dir="$ANSIBLE_DIR/../../"
    
    # 创建构建脚本
    cat > "$FILES_DIR/$arch/build-miniodb.sh" << EOF
#!/bin/bash
set -e

# MinIODB构建脚本
echo "Building MinIODB for $arch..."

cd "$source_dir"

# 设置Go环境变量
export GOOS=linux
export GOARCH=$arch
export CGO_ENABLED=0

# 构建
go build -ldflags="-w -s" -o "$target" cmd/server/main.go

echo "MinIODB built successfully for $arch"
EOF
    
    chmod +x "$FILES_DIR/$arch/build-miniodb.sh"
    
    log_success "MinIODB build script prepared for $arch"
}

# 创建systemd服务文件
create_systemd_files() {
    log_info "Creating systemd service files..."
    
    # MinIO systemd服务
    cat > "$FILES_DIR/configs/minio.service" << 'EOF'
[Unit]
Description=MinIO Object Storage Server
Documentation=https://docs.min.io
Wants=network-online.target
After=network-online.target
AssertFileIsExecutable=/opt/miniodb/bin/minio

[Service]
WorkingDirectory=/opt/miniodb/data

User=minio
Group=minio

PermissionsStartOnly=true

EnvironmentFile=-/etc/default/minio
ExecStartPre=/bin/bash -c "if [ -z \"${MINIO_VOLUMES}\" ]; then echo \"Variable MINIO_VOLUMES not set in /etc/default/minio\"; exit 1; fi"
ExecStart=/opt/miniodb/bin/minio server $MINIO_OPTS $MINIO_VOLUMES

# Let systemd restart this service always
Restart=always

# Specifies the maximum file descriptor number that can be opened by this process
LimitNOFILE=1048576

# Specifies the maximum number of threads this process can create
TasksMax=infinity

# Disable timeout logic and wait until process is stopped
TimeoutStopSec=infinity
SendSIGKILL=no

[Install]
WantedBy=multi-user.target
EOF

    # Redis systemd服务
    cat > "$FILES_DIR/configs/redis.service" << 'EOF'
[Unit]
Description=Advanced key-value store
After=network.target
Documentation=http://redis.io/documentation

[Service]
Type=notify
ExecStart=/opt/miniodb/bin/redis-server /etc/redis/redis.conf
ExecStop=/opt/miniodb/bin/redis-cli shutdown
TimeoutStopSec=0
Restart=always
User=redis
Group=redis
RuntimeDirectory=redis
RuntimeDirectoryMode=0755

[Install]
WantedBy=multi-user.target
EOF

    # MinIODB systemd服务
    cat > "$FILES_DIR/configs/miniodb.service" << 'EOF'
[Unit]
Description=MinIODB OLAP System
Documentation=https://github.com/your-org/miniodb
Wants=network-online.target
After=network-online.target minio.service redis.service
Requires=minio.service redis.service

[Service]
Type=simple
User=miniodb
Group=miniodb
WorkingDirectory=/opt/miniodb
EnvironmentFile=-/etc/default/miniodb
ExecStart=/opt/miniodb/bin/miniodb /opt/miniodb/config/config.yaml
Restart=always
RestartSec=5

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/miniodb/data /opt/miniodb/logs

[Install]
WantedBy=multi-user.target
EOF

    log_success "Systemd service files created"
}

# 创建配置文件模板
create_config_templates() {
    log_info "Creating configuration templates..."
    
    # MinIO默认配置
    cat > "$FILES_DIR/configs/minio.env" << 'EOF'
# MinIO configuration
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin123
MINIO_VOLUMES="/opt/miniodb/data/minio"
MINIO_OPTS="--console-address :9001"
EOF

    # Redis配置
    cat > "$FILES_DIR/configs/redis.conf" << 'EOF'
# Redis configuration for MinIODB
bind 127.0.0.1
port 6379
daemonize no
supervised systemd
loglevel notice
logfile /var/log/redis/redis-server.log
dir /opt/miniodb/data/redis
requirepass redis123
maxmemory 2gb
maxmemory-policy allkeys-lru
appendonly yes
appendfsync everysec
EOF

    log_success "Configuration templates created"
}

# 创建安装脚本
create_install_script() {
    log_info "Creating install script..."
    
    cat > "$FILES_DIR/install.sh" << 'EOF'
#!/bin/bash

# MinIODB离线安装脚本
set -e

ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

INSTALL_DIR="/opt/miniodb"
CONFIG_DIR="/etc/miniodb"
LOG_DIR="/var/log/miniodb"
DATA_DIR="/opt/miniodb/data"

echo "Installing MinIODB for $ARCH..."

# 创建用户和目录
useradd -r -s /bin/false minio || true
useradd -r -s /bin/false redis || true
useradd -r -s /bin/false miniodb || true

mkdir -p $INSTALL_DIR/bin
mkdir -p $INSTALL_DIR/config
mkdir -p $CONFIG_DIR
mkdir -p $LOG_DIR
mkdir -p $DATA_DIR/{minio,redis}

# 安装二进制文件
cp $ARCH/bin/* $INSTALL_DIR/bin/
chmod +x $INSTALL_DIR/bin/*

# 安装配置文件
cp configs/* $INSTALL_DIR/config/

# 设置权限
chown -R minio:minio $DATA_DIR/minio
chown -R redis:redis $DATA_DIR/redis
chown -R miniodb:miniodb $INSTALL_DIR
chown -R miniodb:miniodb $LOG_DIR

echo "MinIODB installed successfully!"
echo "Please configure the services and start them."
EOF

    chmod +x "$FILES_DIR/install.sh"
    
    log_success "Install script created"
}

# 显示下载总结
show_summary() {
    log_info "=== Download Summary ==="
    
    for arch in "${ARCHITECTURES[@]}"; do
        echo ""
        log_info "Architecture: $arch"
        
        if [ -f "$FILES_DIR/$arch/bin/minio" ]; then
            echo "  ✅ MinIO binary"
        else
            echo "  ❌ MinIO binary"
        fi
        
        if [ -f "$FILES_DIR/$arch/bin/mc" ]; then
            echo "  ✅ MinIO Client (mc)"
        else
            echo "  ❌ MinIO Client (mc)"
        fi
        
        if [ -d "$FILES_DIR/$arch/redis" ]; then
            echo "  ✅ Redis source"
        else
            echo "  ❌ Redis source"
        fi
        
        if [ -f "$FILES_DIR/$arch/build-miniodb.sh" ]; then
            echo "  ✅ MinIODB build script"
        else
            echo "  ❌ MinIODB build script"
        fi
    done
    
    echo ""
    log_info "Configuration files:"
    echo "  ✅ Systemd service files"
    echo "  ✅ Configuration templates"
    echo "  ✅ Install script"
    
    echo ""
    log_success "All components prepared for offline deployment!"
    log_info "Files location: $FILES_DIR"
}

# 主函数
main() {
    log_info "=== MinIODB Offline Binary Downloader ==="
    log_info "Downloading binaries for architectures: ${ARCHITECTURES[*]}"
    
    create_directories
    
    for arch in "${ARCHITECTURES[@]}"; do
        log_info "Processing architecture: $arch"
        
        download_minio "$arch"
        download_mc "$arch"
        download_redis "$arch"
        download_miniodb "$arch"
    done
    
    create_systemd_files
    create_config_templates
    create_install_script
    
    show_summary
}

# 检查依赖
check_dependencies() {
    for cmd in curl tar; do
        if ! command -v "$cmd" &> /dev/null; then
            log_error "Required command '$cmd' not found"
            exit 1
        fi
    done
}

# 执行检查和主函数
check_dependencies
main "$@" 