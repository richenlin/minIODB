#!/bin/bash

# MinIODB 一键部署脚本
# 支持 Docker Compose 和 Kubernetes 两种部署方式

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
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

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB 一键部署脚本

用法:
    $0 [选项] <部署方式>

部署方式:
    docker      使用 Docker Compose 部署 (单机)
    k8s         使用 Kubernetes 部署 (集群)

选项:
    -h, --help              显示帮助信息
    -v, --verbose           详细输出
    -c, --config FILE       指定配置文件
    -n, --namespace NAME    指定 Kubernetes 命名空间 (默认: miniodb-system)
    --skip-build           跳过镜像构建
    --skip-init            跳过初始化步骤
    --dev                  开发模式部署

示例:
    $0 docker                    # Docker Compose 部署
    $0 k8s                       # Kubernetes 部署
    $0 --dev docker              # 开发模式 Docker 部署
    $0 -n my-namespace k8s       # 指定命名空间的 K8s 部署

EOF
}

# 检查依赖
check_dependencies() {
    local deploy_type=$1
    
    log_info "检查部署依赖..."
    
    # 通用依赖
    if ! command -v git &> /dev/null; then
        log_error "git 未安装"
        exit 1
    fi
    
    if [[ "$deploy_type" == "docker" ]]; then
        # Docker 依赖
        if ! command -v docker &> /dev/null; then
            log_error "Docker 未安装"
            exit 1
        fi
        
        if ! command -v docker-compose &> /dev/null; then
            log_error "Docker Compose 未安装"
            exit 1
        fi
        
        # 检查 Docker 服务状态
        if ! docker info &> /dev/null; then
            log_error "Docker 服务未运行"
            exit 1
        fi
        
    elif [[ "$deploy_type" == "k8s" ]]; then
        # Kubernetes 依赖
        if ! command -v kubectl &> /dev/null; then
            log_error "kubectl 未安装"
            exit 1
        fi
        
        # 检查 kubectl 连接
        if ! kubectl cluster-info &> /dev/null; then
            log_error "无法连接到 Kubernetes 集群"
            exit 1
        fi
    fi
    
    log_success "依赖检查通过"
}

# 检查端口占用
check_ports() {
    local ports=("6379" "8080" "8081" "9000" "9001" "9002" "9003" "9090")
    local occupied_ports=()
    
    log_info "检查端口占用情况..."
    
    for port in "${ports[@]}"; do
        if netstat -tlnp 2>/dev/null | grep -q ":$port "; then
            occupied_ports+=("$port")
        fi
    done
    
    if [[ ${#occupied_ports[@]} -gt 0 ]]; then
        log_warning "以下端口被占用: ${occupied_ports[*]}"
        log_warning "请确保这些端口可用，或修改配置文件中的端口设置"
        read -p "是否继续部署? (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "部署已取消"
            exit 0
        fi
    else
        log_success "所有端口可用"
    fi
}

# 构建 Docker 镜像
build_docker_image() {
    if [[ "$SKIP_BUILD" == "true" ]]; then
        log_info "跳过镜像构建"
        return
    fi
    
    log_info "构建 MinIODB Docker 镜像..."
    
    # 切换到项目根目录
    cd "$(dirname "$0")/../.."
    
    # 构建镜像
    if [[ "$VERBOSE" == "true" ]]; then
        docker build -t miniodb:latest .
    else
        docker build -t miniodb:latest . > /dev/null 2>&1
    fi
    
    log_success "Docker 镜像构建完成"
}

# Docker Compose 部署
deploy_docker() {
    log_info "开始 Docker Compose 部署..."
    
    # 切换到 Docker 部署目录
    cd "$(dirname "$0")/../docker"
    
    # 检查环境变量文件
    if [[ ! -f ".env" ]]; then
        log_info "创建环境变量文件..."
        cp env.example .env
        log_warning "请编辑 .env 文件设置必要的配置"
    fi
    
    # 创建数据目录
    log_info "创建数据目录..."
    mkdir -p data/{redis,minio,minio-backup,logs}
    
    # 构建镜像
    build_docker_image
    
    # 启动服务
    log_info "启动 Docker Compose 服务..."
    if [[ "$VERBOSE" == "true" ]]; then
        docker-compose up -d
    else
        docker-compose up -d > /dev/null 2>&1
    fi
    
    # 等待服务启动
    log_info "等待服务启动..."
    sleep 30
    
    # 检查服务状态
    log_info "检查服务状态..."
    docker-compose ps
    
    # 健康检查
    log_info "执行健康检查..."
    if curl -f http://localhost:8081/v1/health &> /dev/null; then
        log_success "MinIODB 服务启动成功!"
        echo
        echo "服务访问地址:"
        echo "  REST API:      http://localhost:8081"
        echo "  gRPC API:      localhost:8080"
        echo "  MinIO Console: http://localhost:9001"
        echo "  监控指标:      http://localhost:9090/metrics"
    else
        log_error "服务健康检查失败"
        log_info "查看服务日志:"
        docker-compose logs miniodb
        exit 1
    fi
}

# Kubernetes 部署
deploy_k8s() {
    log_info "开始 Kubernetes 部署..."
    
    # 切换到 K8s 部署目录
    cd "$(dirname "$0")/../k8s"
    
    # 构建并推送镜像 (如果需要)
    if [[ "$SKIP_BUILD" != "true" ]]; then
        build_docker_image
        
        # 如果有镜像仓库配置，推送镜像
        if [[ -n "$DOCKER_REGISTRY" ]]; then
            log_info "推送镜像到仓库..."
            docker tag miniodb:latest "$DOCKER_REGISTRY/miniodb:latest"
            docker push "$DOCKER_REGISTRY/miniodb:latest"
        fi
    fi
    
    # 创建命名空间
    log_info "创建命名空间..."
    kubectl apply -f namespace.yaml
    
    # 应用配置
    log_info "应用配置文件..."
    kubectl apply -f configmap.yaml
    kubectl apply -f secret.yaml
    
    # 部署 Redis
    log_info "部署 Redis..."
    kubectl apply -f redis/
    
    # 等待 Redis 就绪
    log_info "等待 Redis 服务就绪..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=redis -n "$NAMESPACE" --timeout=300s
    
    # 部署 MinIO
    log_info "部署 MinIO..."
    kubectl apply -f minio/
    
    # 等待 MinIO 就绪
    log_info "等待 MinIO 服务就绪..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio -n "$NAMESPACE" --timeout=300s
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio-backup -n "$NAMESPACE" --timeout=300s
    
    # 部署 MinIODB
    log_info "部署 MinIODB 应用..."
    kubectl apply -f miniodb/
    
    # 等待 MinIODB 就绪
    log_info "等待 MinIODB 服务就绪..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=miniodb -n "$NAMESPACE" --timeout=300s
    
    # 检查部署状态
    log_info "检查部署状态..."
    kubectl get pods -n "$NAMESPACE"
    
    # 获取服务访问信息
    log_info "获取服务访问信息..."
    kubectl get services -n "$NAMESPACE"
    
    log_success "Kubernetes 部署完成!"
    echo
    echo "使用以下命令查看服务状态:"
    echo "  kubectl get pods -n $NAMESPACE"
    echo "  kubectl get services -n $NAMESPACE"
    echo
    echo "端口转发访问服务 (示例):"
    echo "  kubectl port-forward -n $NAMESPACE svc/miniodb-service 8081:8081"
    echo "  kubectl port-forward -n $NAMESPACE svc/minio-service 9001:9001"
}

# 初始化配置
init_config() {
    if [[ "$SKIP_INIT" == "true" ]]; then
        log_info "跳过初始化步骤"
        return
    fi
    
    log_info "初始化配置..."
    
    # 生成随机密码
    if command -v openssl &> /dev/null; then
        REDIS_PASSWORD=$(openssl rand -base64 12)
        JWT_SECRET=$(openssl rand -base64 32)
        MINIO_PASSWORD=$(openssl rand -base64 12)
        
        log_info "生成随机密码完成"
    else
        log_warning "openssl 未安装，使用默认密码"
    fi
}

# 主函数
main() {
    # 默认值
    DEPLOY_TYPE=""
    VERBOSE=false
    CONFIG_FILE=""
    NAMESPACE="miniodb-system"
    SKIP_BUILD=false
    SKIP_INIT=false
    DEV_MODE=false
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -c|--config)
                CONFIG_FILE="$2"
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --skip-build)
                SKIP_BUILD=true
                shift
                ;;
            --skip-init)
                SKIP_INIT=true
                shift
                ;;
            --dev)
                DEV_MODE=true
                shift
                ;;
            docker|k8s)
                DEPLOY_TYPE="$1"
                shift
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 检查部署类型
    if [[ -z "$DEPLOY_TYPE" ]]; then
        log_error "请指定部署方式: docker 或 k8s"
        show_help
        exit 1
    fi
    
    # 显示配置信息
    log_info "部署配置:"
    log_info "  部署方式: $DEPLOY_TYPE"
    log_info "  详细输出: $VERBOSE"
    log_info "  命名空间: $NAMESPACE"
    log_info "  跳过构建: $SKIP_BUILD"
    log_info "  开发模式: $DEV_MODE"
    echo
    
    # 检查依赖
    check_dependencies "$DEPLOY_TYPE"
    
    # 检查端口 (仅 Docker 模式)
    if [[ "$DEPLOY_TYPE" == "docker" ]]; then
        check_ports
    fi
    
    # 初始化配置
    init_config
    
    # 执行部署
    case $DEPLOY_TYPE in
        docker)
            deploy_docker
            ;;
        k8s)
            deploy_k8s
            ;;
    esac
    
    log_success "部署完成!"
}

# 脚本入口
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi 