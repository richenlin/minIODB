#!/bin/bash

#!/bin/bash

# MinIODB 统一部署脚本
# 支持4种部署模式：开发、集成测试、Swarm集群、K8s集群

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
    $0 [部署模式] [选项]

部署模式:
    dev         单机开发模式 (启动 redis、minio、minio-backup，miniodb手动启动)
    test        单机集成测试模式 (启动所有服务，包括miniodb容器)
    swarm       小型集群模式 (基于Docker Swarm的3节点部署)
    k8s         Kubernetes集群模式 (4个pod部署)

选项:
    -c, --config FILE       指定配置文件路径
    -n, --namespace NS      Kubernetes 命名空间 (默认: miniodb-system)
    -r, --replicas NUM      副本数量 (默认: 2)
    -d, --data-path PATH    数据存储路径 (默认: ./data)
    --dry-run              仅显示将要执行的命令，不实际执行
    --cleanup              清理已有部署
    --force                强制执行，跳过确认
    -h, --help             显示此帮助信息

示例:
    # 开发模式部署
    $0 dev

    # 集成测试模式部署
    $0 test

    # Swarm集群部署
    $0 swarm

    # K8s集群部署
    $0 k8s -n my-namespace -r 3

    # 清理开发模式
    $0 dev --cleanup

    # 干运行K8s部署
    $0 k8s --dry-run

环境变量:
    MINIODB_CONFIG_PATH     配置文件路径
    MINIODB_DATA_PATH       数据存储路径
    MINIODB_NAMESPACE       Kubernetes 命名空间
    MINIODB_REPLICAS        副本数量

EOF
}

# 检查依赖
check_dependencies() {
    local deploy_mode=$1

    case $deploy_mode in
        "dev"|"test")
            if ! command -v docker &> /dev/null; then
                log_error "Docker 未安装或不在 PATH 中"
                exit 1
            fi
            if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
                log_error "Docker Compose 未安装或不在 PATH 中"
                exit 1
            fi
            ;;
        "swarm")
            if ! command -v docker &> /dev/null; then
                log_error "Docker 未安装或不在 PATH 中"
                exit 1
            fi
            if ! docker info &> /dev/null; then
                log_error "Docker 服务未运行"
                exit 1
            fi
            if ! command -v ansible-playbook &> /dev/null; then
                log_error "Ansible 未安装或不在 PATH 中"
                exit 1
            fi
            ;;
        "k8s")
            if ! command -v kubectl &> /dev/null; then
                log_error "kubectl 未安装或不在 PATH 中"
                exit 1
            fi
            if ! kubectl cluster-info &> /dev/null; then
                log_error "无法连接到 Kubernetes 集群"
                exit 1
            fi
            ;;
    esac
}

# 编译构建
build_miniodb() {
    local data_path=${DATA_PATH:-./data}

    log_info "编译构建 MinIODB..."
    cd "$(dirname "$0")/.."

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "go build -o miniodb cmd/main.go"
        return
    fi

    go build -o miniodb cmd/main.go
    log_success "MinIODB 编译完成"
}

# Docker 镜像构建
build_docker_image() {
    local data_path=${DATA_PATH:-./data}

    log_info "构建 MinIODB Docker 镜像..."
    cd "$(dirname "$0")/docker"

    local arch=$(uname -m)
    local dockerfile="Dockerfile"

    if [[ "$arch" == "arm64" ]] || [[ "$arch" == "aarch64" ]]; then
        dockerfile="Dockerfile.arm"
        log_info "检测到 ARM64 架构，使用 $dockerfile"
    else
        dockerfile="Dockerfile"
        log_info "检测到 AMD64 架构，使用 $dockerfile"
    fi

    export DOCKERFILE=$dockerfile

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "docker build -f $dockerfile -t miniodb:latest ../.."
        return
    fi

    docker build -f "$dockerfile" -t miniodb:latest ../..
    log_success "Docker 镜像构建完成"
}

# 开发模式部署
deploy_dev() {
    local data_path=${DATA_PATH:-./data}

    log_info "开始单机开发模式部署..."
    log_info "数据路径: $data_path"

    cd "$(dirname "$0")/docker"

    if [[ ! -f .env ]]; then
        if [[ -f env.example ]]; then
            log_warning ".env 文件不存在，从 env.example 复制"
            cp env.example .env
        else
            log_error ".env 文件和 env.example 都不存在"
            exit 1
        fi
    fi

    mkdir -p "$data_path"/{redis,minio,minio-backup,logs}
    export DATA_PATH=$data_path

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "docker-compose -f docker-compose.dev.yml up -d"
        return
    fi

    if command -v docker-compose &> /dev/null; then
        docker-compose -f docker-compose.dev.yml up -d
    else
        docker compose -f docker-compose.dev.yml up -d
    fi

    log_success "开发模式部署完成!"
    log_info "基础设施服务已启动:"
    log_info "  Redis:      localhost:6379"
    log_info "  MinIO:      http://localhost:9000 (API), http://localhost:9001 (Console)"
    log_info "  MinIO Backup: http://localhost:9002 (API), http://localhost:9003 (Console)"
    echo ""
    log_warning "MinIODB 应用需要手动启动，请使用以下命令:"
    log_info "  go run cmd/main.go"
    log_info "  或使用调试工具 (VS Code / GoLand)"
}

# 集成测试模式部署
deploy_test() {
    local data_path=${DATA_PATH:-./data}

    log_info "开始单机集成测试模式部署..."
    log_info "数据路径: $data_path"

    cd "$(dirname "$0")/docker"

    if [[ ! -f .env ]]; then
        if [[ -f env.example ]]; then
            log_warning ".env 文件不存在，从 env.example 复制"
            cp env.example .env
        else
            log_error ".env 文件和 env.example 都不存在"
            exit 1
        fi
    fi

    mkdir -p "$data_path"/{redis,minio,minio-backup,logs}
    export DATA_PATH=$data_path

    build_docker_image

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "docker-compose up -d"
        return
    fi

    if command -v docker-compose &> /dev/null; then
        docker-compose up -d
    else
        docker compose up -d
    fi

    log_info "等待服务启动..."
    sleep 30

    log_success "集成测试模式部署完成!"
    log_info "访问地址:"
    log_info "  REST API:      http://localhost:8081"
    log_info "  gRPC API:      localhost:8080"
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

# Swarm 集群部署
deploy_swarm() {
    local data_path=${DATA_PATH:-./data}

    log_info "开始 Docker Swarm 集群部署..."
    log_info "数据路径: $data_path"

    cd "$(dirname "$0")/ansible"

    build_docker_image

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "ansible-playbook -i inventory/swarm.ini swarm-deploy.yml"
        return
    fi

    if [[ ! -f "inventory/swarm.ini" ]]; then
        log_error "Swarm 清单文件不存在: inventory/swarm.ini"
        log_info "请先创建 inventory/swarm.ini 文件配置集群节点"
        exit 1
    fi

    ansible-playbook -i inventory/swarm.ini swarm-deploy.yml

    log_success "Swarm 集群部署完成!"
    log_info "请检查各个节点的服务状态:"
    log_info "  节点1 (miniodb+redis): docker service ps miniodb_swarm_miniodb"
    log_info "  节点2 (minio): docker service ps miniodb_swarm_minio"
    log_info "  节点3 (minio-backup): docker service ps miniodb_swarm_minio-backup"
}

# 清理部署
cleanup_deployment() {
    local deploy_mode=$1

    case $deploy_mode in
        "dev")
            log_info "清理开发模式部署..."
            cd "$(dirname "$0")/docker"
            if command -v docker-compose &> /dev/null; then
                docker-compose -f docker-compose.dev.yml down -v
            else
                docker compose -f docker-compose.dev.yml down -v
            fi
            log_success "开发模式部署已清理"
            ;;
        "test")
            log_info "清理集成测试模式部署..."
            cd "$(dirname "$0")/docker"
            if command -v docker-compose &> /dev/null; then
                docker-compose down -v
            else
                docker compose down -v
            fi
            log_success "集成测试模式部署已清理"
            ;;
        "swarm")
            log_info "清理 Swarm 集群部署..."
            cd "$(dirname "$0")/ansible"
            ansible-playbook -i inventory/swarm.ini swarm-cleanup.yml || log_warning "清理脚本可能不存在，请手动清理"
            log_success "Swarm 集群部署已清理（或请手动清理）"
            ;;
        "k8s")
            local namespace=${NAMESPACE:-miniodb-system}
            log_info "清理 Kubernetes 部署..."
            kubectl delete namespace "$namespace" --ignore-not-found=true
            log_success "Kubernetes 部署已清理"
            ;;
    esac
}

# 主函数
main() {
    local deploy_mode=""
    local cleanup=false

    # 解析参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            dev|test|swarm|k8s)
                deploy_mode=$1
                shift
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

    # 检查部署模式
    if [[ -z "$deploy_mode" ]]; then
        log_error "请指定部署模式: dev, test, swarm, 或 k8s"
        show_help
        exit 1
    fi

    # 检查依赖
    check_dependencies "$deploy_mode"

    # 执行清理或部署
    if [[ $cleanup == "true" ]]; then
        if [[ $FORCE != "true" ]]; then
            read -p "确定要清理 $deploy_mode 部署吗? [y/N] " -n 1 -r
            echo
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                log_info "取消清理操作"
                exit 0
            fi
        fi
        cleanup_deployment "$deploy_mode"
    else
        # 执行部署
        case $deploy_mode in
            "dev")
                deploy_dev
                ;;
            "test")
                deploy_test
                ;;
            "swarm")
                deploy_swarm
                ;;
            "k8s")
                deploy_k8s
                ;;
        esac
    fi
}

# 设置默认值
DATA_PATH=${MINIODB_DATA_PATH:-./data}
NAMESPACE=${MINIODB_NAMESPACE:-miniodb-system}
REPLICAS=${MINIODB_REPLICAS:-2}
DRY_RUN=${DRY_RUN:-false}
FORCE=${FORCE:-false}

# 执行主函数
main "$@"