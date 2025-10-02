#!/bin/bash

# MinIODB Docker Compose 启动脚本
# 用于简化部署过程

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

# 检查Docker权限
check_docker_permissions() {
    log_info "Checking Docker permissions..."
    
    if ! docker info > /dev/null 2>&1; then
        log_warning "Docker requires sudo permissions"
        DOCKER_CMD="sudo docker"
        DOCKER_COMPOSE_CMD="sudo docker-compose"
    else
        log_success "Docker permissions OK"
        DOCKER_CMD="docker"
        DOCKER_COMPOSE_CMD="docker-compose"
    fi
}

# 创建必要的目录
create_directories() {
    log_info "Creating necessary directories..."
    
    mkdir -p data/{redis,minio,minio-backup,logs}
    mkdir -p redis minio/certs certs
    
    log_success "Directories created successfully"
}

# 创建环境配置文件
create_env_file() {
    log_info "Creating environment configuration..."
    
    if [ ! -f .env ]; then
        cp env.example .env
        log_success "Environment file created from template"
    else
        log_info "Environment file already exists"
    fi
}

# 停止现有服务
stop_services() {
    log_info "Stopping existing services..."
    
    if $DOCKER_COMPOSE_CMD ps -q > /dev/null 2>&1; then
        $DOCKER_COMPOSE_CMD down
        log_success "Services stopped"
    else
        log_info "No running services found"
    fi
}

# 清理Docker资源
cleanup_docker() {
    log_info "Cleaning up Docker resources..."
    
    # 清理停止的容器
    $DOCKER_CMD container prune -f > /dev/null 2>&1 || true
    
    # 清理未使用的卷
    $DOCKER_CMD volume prune -f > /dev/null 2>&1 || true
    
    log_success "Docker cleanup completed"
}

# 构建和启动服务
start_services() {
    log_info "Building and starting services..."
    
    # 构建镜像
    log_info "Building MinIODB image..."
    $DOCKER_COMPOSE_CMD build --no-cache miniodb
    
    # 启动服务
    log_info "Starting all services..."
    $DOCKER_COMPOSE_CMD up -d
    
    log_success "Services started successfully"
}

# 等待服务就绪
wait_for_services() {
    log_info "Waiting for services to be ready..."
    
    local max_wait=300  # 5分钟
    local wait_time=0
    
    while [ $wait_time -lt $max_wait ]; do
        if $DOCKER_COMPOSE_CMD ps | grep -q "Up (healthy)"; then
            log_success "Services are healthy"
            break
        fi
        
        log_info "Waiting for services... (${wait_time}s/${max_wait}s)"
        sleep 10
        wait_time=$((wait_time + 10))
    done
    
    if [ $wait_time -ge $max_wait ]; then
        log_warning "Services may not be fully ready yet"
    fi
}

# 显示服务状态
show_status() {
    log_info "Service status:"
    $DOCKER_COMPOSE_CMD ps
    
    log_info ""
    log_info "Service URLs:"
    log_info "  MinIODB REST API: http://localhost:8081"
    log_info "  MinIODB gRPC API: localhost:8080"
    log_info "  MinIODB Metrics: http://localhost:9090"
    log_info "  MinIO Console: http://localhost:9001"
    log_info "  MinIO Backup Console: http://localhost:9003"
    log_info "  Redis: localhost:6379"
    
    log_info ""
    log_info "Health check:"
    log_info "  curl http://localhost:8081/v1/health"
}

# 主函数
main() {
    log_info "=== MinIODB Docker Deployment Started ==="
    
    # 切换到脚本目录
    cd "$(dirname "$0")"
    
    check_docker_permissions
    create_directories
    create_env_file
    stop_services
    cleanup_docker
    start_services
    wait_for_services
    show_status
    
    log_success "=== MinIODB Docker Deployment Completed ==="
}

# 执行主函数
main "$@"