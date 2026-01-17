#!/bin/bash

# MinIODB 统一部署脚本
# 支持 Docker Compose、Kubernetes、Ansible 三种部署方式

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
MinIODB 统一部署脚本

用法:
    $0 [部署方式] [选项]

部署方式:
    docker      使用 Docker Compose 部署 (推荐开发和测试)
    k8s         使用 Kubernetes 部署 (推荐生产环境)
    ansible     使用 Ansible 部署 (推荐批量部署)

选项:
    -e, --env ENV           设置环境 (development|testing|production)
    -c, --config FILE       指定配置文件路径
    -n, --namespace NS      Kubernetes 命名空间 (默认: miniodb-system)
    -r, --replicas NUM      副本数量 (默认: 2)
    -d, --data-path PATH    数据存储路径 (默认: ./data)
    --dry-run              仅显示将要执行的命令，不实际执行
    --cleanup              清理已有部署
    --force                强制执行，跳过确认
    -h, --help             显示此帮助信息

示例:
    # Docker Compose 开发环境部署
    $0 docker -e development

    # Kubernetes 生产环境部署
    $0 k8s -e production -r 3

    # Ansible 批量部署
    $0 ansible -e production -c /path/to/inventory

    # 清理 Docker 部署
    $0 docker --cleanup

    # 干运行 Kubernetes 部署
    $0 k8s --dry-run

环境变量:
    MINIODB_ENV             部署环境 (development|testing|production)
    MINIODB_CONFIG_PATH     配置文件路径
    MINIODB_DATA_PATH       数据存储路径
    MINIODB_NAMESPACE       Kubernetes 命名空间
    MINIODB_REPLICAS        副本数量

EOF
}

# 检查依赖
check_dependencies() {
    local deploy_method=$1
    
    case $deploy_method in
        "docker")
            if ! command -v docker &> /dev/null; then
                log_error "Docker 未安装或不在 PATH 中"
                exit 1
            fi
            if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
                log_error "Docker Compose 未安装或不在 PATH 中"
                exit 1
            fi
            ;;
        "k8s")
            if ! command -v kubectl &> /dev/null; then
                log_error "kubectl 未安装或不在 PATH 中"
                exit 1
            fi
            # 检查 Kubernetes 集群连接
            if ! kubectl cluster-info &> /dev/null; then
                log_error "无法连接到 Kubernetes 集群"
                exit 1
            fi
            ;;
        "ansible")
            if ! command -v ansible-playbook &> /dev/null; then
                log_error "Ansible 未安装或不在 PATH 中"
                exit 1
            fi
            ;;
    esac
}

# Docker Compose 部署
deploy_docker() {
    local env=${ENV:-development}
    local data_path=${DATA_PATH:-./data}
    local config_file=${CONFIG_FILE:-}
    
    log_info "开始 Docker Compose 部署..."
    log_info "环境: $env"
    log_info "数据路径: $data_path"
    
    cd "$(dirname "$0")/docker"
    
    # 检测系统架构并设置相应的 Dockerfile
    local arch=$(uname -m)
    local dockerfile="Dockerfile"
    local platform_tag="latest"
    
    if [[ "$arch" == "arm64" ]] || [[ "$arch" == "aarch64" ]]; then
        dockerfile="Dockerfile.arm"
        platform_tag="latest-arm64"
        log_info "检测到 ARM64 架构，使用 $dockerfile"
    else
        dockerfile="Dockerfile"
        platform_tag="latest-amd64"
        log_info "检测到 AMD64 架构，使用 $dockerfile"
    fi
    
    # 检查 .env 文件
    if [[ ! -f .env ]]; then
        if [[ -f env.example ]]; then
            log_warning ".env 文件不存在，从 env.example 复制"
            cp env.example .env
        else
            log_error ".env 文件和 env.example 都不存在"
            exit 1
        fi
    fi
    
    # 创建数据目录
    mkdir -p "$data_path"/{redis,minio,minio-backup,logs}
    
    # 设置环境变量
    export MINIODB_ENV=$env
    export DATA_PATH=$data_path
    export DOCKERFILE=$dockerfile
    export PLATFORM_TAG=$platform_tag
    
    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "docker-compose up -d"
        return
    fi
    
    # 启动服务
    if command -v docker-compose &> /dev/null; then
        docker-compose up -d
    else
        docker compose up -d
    fi
    
    log_success "Docker Compose 部署完成!"
    log_info "访问地址:"
    log_info "  REST API: http://localhost:8081"
    log_info "  gRPC API: localhost:8080"
    log_info "  MinIO Console: http://localhost:9001"
    log_info "  MinIO Backup Console: http://localhost:9003"
    log_info "  Prometheus Metrics: http://localhost:9090/metrics"
}

# Kubernetes 部署
deploy_k8s() {
    local env=${ENV:-production}
    local namespace=${NAMESPACE:-miniodb-system}
    local replicas=${REPLICAS:-2}
    
    log_info "开始 Kubernetes 部署..."
    log_info "环境: $env"
    log_info "命名空间: $namespace"
    log_info "副本数: $replicas"
    
    cd "$(dirname "$0")/k8s"
    
    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "kubectl create namespace $namespace"
        echo "kubectl apply -f namespace.yaml"
        echo "kubectl apply -f configmap.yaml"
        echo "kubectl apply -f secret.yaml"
        echo "kubectl apply -f redis/"
        echo "kubectl apply -f minio/"
        echo "kubectl apply -f miniodb/"
        return
    fi
    
    # 创建命名空间
    kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
    
    # 应用配置
    kubectl apply -f namespace.yaml
    kubectl apply -f configmap.yaml
    kubectl apply -f secret.yaml
    
    # 部署 Redis
    kubectl apply -f redis/
    
    # 部署 MinIO
    kubectl apply -f minio/
    
    # 等待 MinIO 就绪
    log_info "等待 MinIO 服务就绪..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio -n "$namespace" --timeout=300s
    
    # 部署 MinIODB
    kubectl apply -f miniodb/
    
    # 等待部署完成
    log_info "等待 MinIODB 部署完成..."
    kubectl wait --for=condition=available deployment/miniodb -n "$namespace" --timeout=300s
    
    log_success "Kubernetes 部署完成!"
    log_info "获取服务状态:"
    kubectl get pods,svc -n "$namespace"
    
    # 获取访问地址
    local nodeport=$(kubectl get svc miniodb-external -n "$namespace" -o jsonpath='{.spec.ports[0].nodePort}')
    local node_ip=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
    if [[ -z "$node_ip" ]]; then
        node_ip=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
    fi
    
    log_info "访问地址:"
    log_info "  REST API: http://$node_ip:$nodeport"
    log_info "  gRPC API: $node_ip:30090"
}

# Ansible 部署
deploy_ansible() {
    local env=${ENV:-production}
    local inventory=${CONFIG_FILE:-inventory/auto-deploy.yml}
    
    log_info "开始 Ansible 部署..."
    log_info "环境: $env"
    log_info "清单文件: $inventory"
    
    cd "$(dirname "$0")/ansible"
    
    if [[ ! -f "$inventory" ]]; then
        log_error "清单文件不存在: $inventory"
        exit 1
    fi
    
    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "ansible-playbook -i $inventory site.yml --check"
        return
    fi
    
    # 执行部署
    ansible-playbook -i "$inventory" site.yml
    
    log_success "Ansible 部署完成!"
}

# 清理部署
cleanup_deployment() {
    local deploy_method=$1
    
    case $deploy_method in
        "docker")
            log_info "清理 Docker Compose 部署..."
            cd "$(dirname "$0")/docker"
            if command -v docker-compose &> /dev/null; then
                docker-compose down -v
            else
                docker compose down -v
            fi
            log_success "Docker 部署已清理"
            ;;
        "k8s")
            local namespace=${NAMESPACE:-miniodb-system}
            log_info "清理 Kubernetes 部署..."
            kubectl delete namespace "$namespace" --ignore-not-found=true
            log_success "Kubernetes 部署已清理"
            ;;
        "ansible")
            log_warning "Ansible 清理需要手动执行相应的清理剧本"
            ;;
    esac
}

# 主函数
main() {
    local deploy_method=""
    local cleanup=false
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            docker|k8s|ansible)
                deploy_method=$1
                shift
                ;;
            -e|--env)
                ENV=$2
                shift 2
                ;;
            -c|--config)
                CONFIG_FILE=$2
                shift 2
                ;;
            -n|--namespace)
                NAMESPACE=$2
                shift 2
                ;;
            -r|--replicas)
                REPLICAS=$2
                shift 2
                ;;
            -d|--data-path)
                DATA_PATH=$2
                shift 2
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --cleanup)
                cleanup=true
                shift
                ;;
            --force)
                FORCE=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 检查部署方式
    if [[ -z "$deploy_method" ]]; then
        log_error "请指定部署方式: docker, k8s, 或 ansible"
        show_help
        exit 1
    fi
    
    # 检查依赖
    check_dependencies "$deploy_method"
    
    # 执行清理或部署
    if [[ $cleanup == "true" ]]; then
        if [[ $FORCE != "true" ]]; then
            read -p "确定要清理 $deploy_method 部署吗? [y/N] " -n 1 -r
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                log_info "取消清理操作"
                exit 0
            fi
        fi
        cleanup_deployment "$deploy_method"
    else
        # 执行部署
        case $deploy_method in
            "docker")
                deploy_docker
                ;;
            "k8s")
                deploy_k8s
                ;;
            "ansible")
                deploy_ansible
                ;;
        esac
    fi
}

# 设置默认值
ENV=${MINIODB_ENV:-development}
DATA_PATH=${MINIODB_DATA_PATH:-./data}
NAMESPACE=${MINIODB_NAMESPACE:-miniodb-system}
REPLICAS=${MINIODB_REPLICAS:-2}
DRY_RUN=${DRY_RUN:-false}
FORCE=${FORCE:-false}

# 执行主函数
main "$@"