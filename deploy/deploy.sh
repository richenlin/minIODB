#!/bin/bash

# MinIODB 统一部署脚本
# 支持4种部署模式：allinone、ansible、Swarm集群、K8s集群

set -e

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 引入依赖管理脚本
source "$SCRIPT_DIR/scripts/dependencies.sh" 2>/dev/null || true

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
    dev         单机 allinone 模式 (miniodb + dashboard 一体化，含所有基础设施)
    ansible     Ansible 直接部署 (client/server 分离，需要目标主机可 SSH 访问)
    swarm       Docker Swarm 集群模式 (client/server 分离，基于 Swarm 3节点部署)
    k8s         Kubernetes 集群模式 (client/server 分离，4个pod部署)

 选项:
    -c, --config FILE       指定配置文件路径
    -n, --namespace NS      Kubernetes 命名空间 (默认: miniodb-system)
    -r, --replicas NUM      副本数量 (默认: 2)
    -d, --data-path PATH    数据存储路径 (默认: ./data)
    --no-cache, --force-rebuild  强制重新构建镜像（所有使用镜像的模式均生效），不使用缓存
    --install-deps         自动检测并安装依赖
    --show-deps            显示离线安装包下载地址
    --dry-run              仅显示将要执行的命令，不实际执行
    --cleanup              清理已有部署
    --force                强制执行，跳过确认
    -h, --help             显示此帮助信息

示例:
    # allinone 模式部署（开发/单机）
    $0 dev

    # Ansible 直接部署
    $0 ansible

    # Swarm 集群部署
    $0 swarm

    # K8s 集群部署
    $0 k8s -n my-namespace -r 3

    # 清理 allinone 部署
    $0 dev --cleanup

    # 干运行 K8s 部署
    $0 k8s --dry-run

    # 强制重新构建镜像后部署（不使用 Docker 缓存）
    $0 dev --no-cache

环境变量:
    MINIODB_CONFIG_PATH     配置文件路径
    MINIODB_DATA_PATH       数据存储路径
    MINIODB_NAMESPACE       Kubernetes 命名空间
    MINIODB_REPLICAS        副本数量
    FORCE_REBUILD=true      等同于 --no-cache，强制重新构建镜像

EOF
}

# 检查依赖
check_dependencies() {
    local deploy_mode=$1

    local deps=""
    case $deploy_mode in
        "dev")
            deps="docker docker-compose"
            ;;
        "ansible")
            deps="docker docker-compose ansible"
            ;;
        "swarm")
            deps="docker docker-compose ansible"
            ;;
        "k8s")
            deps="kubectl"
            ;;
    esac

    # 检测系统信息
    detect_system 2>/dev/null || {
        OS=$(uname -s)
        ARCH=$(uname -m)
        log_info "检测到系统: $OS $ARCH"
    }

    # 使用依赖管理脚本检查
    if command -v check_and_install &> /dev/null; then
        if [[ "$AUTO_INSTALL_DEPS" == "true" ]]; then
            check_and_install "$deps"
        else
            # 只检查不安装
            for dep in $deps; do
                case $dep in
                    docker)
                        if ! command -v docker &> /dev/null; then
                            log_error "Docker 未安装或不在 PATH 中"
                            log_info "运行 '$0 --install-deps' 自动安装依赖"
                            exit 1
                        fi
                        ;;
                    docker-compose)
                        if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
                            log_error "Docker Compose 未安装或不在 PATH 中"
                            log_info "运行 '$0 --install-deps' 自动安装依赖"
                            exit 1
                        fi
                        ;;
                    kubectl)
                        if ! command -v kubectl &> /dev/null; then
                            log_error "kubectl 未安装或不在 PATH 中"
                            log_info "运行 '$0 --install-deps' 自动安装依赖"
                            exit 1
                        fi
                        ;;
                    ansible)
                        if ! command -v ansible-playbook &> /dev/null; then
                            log_error "Ansible 未安装或不在 PATH 中"
                            log_info "运行 '$0 --install-deps' 自动安装依赖"
                            exit 1
                        fi
                        ;;
                esac
            done
            log_success "依赖检查通过"
        fi
    else
        # 降级检查
        for dep in $deps; do
            case $dep in
                docker)
                    if ! command -v docker &> /dev/null; then
                        log_error "Docker 未安装或不在 PATH 中"
                        log_info "运行 '$0 --show-deps' 查看离线安装包下载地址"
                        exit 1
                    fi
                    ;;
                docker-compose)
                    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
                        log_error "Docker Compose 未安装或不在 PATH 中"
                        log_info "运行 '$0 --show-deps' 查看离线安装包下载地址"
                        exit 1
                    fi
                    ;;
                kubectl)
                    if ! command -v kubectl &> /dev/null; then
                        log_error "kubectl 未安装或不在 PATH 中"
                        log_info "运行 '$0 --show-deps' 查看离线安装包下载地址"
                        exit 1
                    fi
                    ;;
                ansible)
                    if ! command -v ansible-playbook &> /dev/null; then
                        log_error "Ansible 未安装或不在 PATH 中"
                        log_info "运行 '$0 --show-deps' 查看离线安装包下载地址"
                        exit 1
                    fi
                    ;;
            esac
        done
        log_success "依赖检查通过"
    fi

    # 特殊检查：Docker 服务状态和 K8s 集群连接
    case $deploy_mode in
        "swarm")
            if ! docker info &> /dev/null; then
                log_error "Docker 服务未运行"
                exit 1
            fi
            ;;
        "k8s")
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

    if [[ "${FORCE_REBUILD}" == "true" ]]; then
        log_info "构建 MinIODB Docker 镜像（--no-cache 强制重建）..."
    else
        log_info "构建 MinIODB Docker 镜像..."
    fi
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

    local build_opts="-f $dockerfile -t miniodb:latest ../.."
    [[ "${FORCE_REBUILD}" == "true" ]] && build_opts="--no-cache $build_opts"

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "docker build $build_opts"
        return
    fi

    docker build $build_opts
    log_success "Docker 镜像构建完成"
}

# Dashboard 镜像构建
build_dashboard_image() {
    if [[ "${FORCE_REBUILD}" == "true" ]]; then
        log_info "构建 Dashboard 镜像（--no-cache 强制重建）..."
    else
        log_info "构建 Dashboard 镜像..."
    fi
    cd "$(dirname "$0")/docker"

    local build_opts="-f Dockerfile.dashboard -t miniodb-dashboard:latest ../.."
    [[ "${FORCE_REBUILD}" == "true" ]] && build_opts="--no-cache $build_opts"

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "docker build $build_opts"
        return
    fi

    docker build $build_opts
    log_success "Dashboard 镜像构建完成"
}

# 开发 allinone 模式部署
deploy_dev() {
    local data_path=${DATA_PATH:-./data}

    log_info "开始单机 allinone 模式部署（miniodb + dashboard 一体化）..."
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

    # 根据架构选择 Dockerfile
    local arch=$(uname -m)
    if [[ "$arch" == "arm64" ]] || [[ "$arch" == "aarch64" ]]; then
        export DOCKERFILE=deploy/docker/Dockerfile.arm
        log_info "检测到 ARM64 架构，使用 Dockerfile.arm"
    else
        export DOCKERFILE=deploy/docker/Dockerfile
        log_info "检测到 AMD64 架构，使用 Dockerfile"
    fi

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        [[ "${FORCE_REBUILD}" == "true" ]] && echo "docker-compose build --no-cache"
        echo "docker-compose up -d"
        return
    fi

    # 优先使用 Docker Compose V2 插件（避免 V1 与新版 Docker 的 ContainerConfig 兼容性问题）
    local use_compose_v2=false
    if docker compose version &> /dev/null; then
        use_compose_v2=true
    fi

    # --no-cache 时先强制重新构建镜像
    if [[ "${FORCE_REBUILD}" == "true" ]]; then
        log_info "强制重新构建镜像（--no-cache）..."
        if $use_compose_v2; then
            docker compose build --no-cache
        else
            docker-compose build --no-cache
        fi
    fi

    if $use_compose_v2; then
        docker compose up -d
    else
        # 使用旧版 docker-compose 且刚重建镜像时，先删除 app 容器再 up，避免 recreate 时的 ContainerConfig KeyError
        if [[ "${FORCE_REBUILD}" == "true" ]]; then
            docker-compose rm -f miniodb 2>/dev/null || true
        fi
        docker-compose up -d
    fi

    log_info "等待服务启动..."
    sleep 30

    log_success "allinone 模式部署完成!"
    log_info "访问地址:"
    log_info "  REST API:      http://localhost:8081"
    log_info "  gRPC API:      localhost:8080"
    log_info "  Dashboard UI:  http://localhost:9090"
    log_info "  MinIO Console: http://localhost:9001"
    log_info "  MinIO Backup Console: http://localhost:9003"
}

# Ansible 直接部署（client/server 分离）
deploy_ansible() {
    log_info "开始 Ansible 直接部署（client/server 分离）..."

    build_docker_image
    build_dashboard_image

    cd "$(dirname "$0")/ansible"

    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        echo "ansible-playbook -i inventory/hosts.ini site.yml"
        return
    fi

    if [[ ! -f "inventory/hosts.ini" ]]; then
        log_error "Ansible 清单文件不存在: inventory/hosts.ini"
        log_info "请从模板创建清单文件并填入实际主机信息:"
        log_info "  cp deploy/ansible/inventory/hosts.ini.example deploy/ansible/inventory/hosts.ini"
        log_info "  然后编辑 hosts.ini，替换 IP 地址和凭据"
        exit 1
    fi

    ansible-playbook -i inventory/hosts.ini site.yml

    log_success "Ansible 部署完成!"
    log_info "服务架构: client(dashboard) + server(miniodb)"
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
    
    # --no-cache 时先强制重新构建镜像（供 k8s 使用本地镜像）
    if [[ "${FORCE_REBUILD}" == "true" ]]; then
        log_info "强制重新构建镜像（--no-cache）..."
        build_docker_image
        [[ -d "$(dirname "$0")/k8s/dashboard" ]] && build_dashboard_image
    fi
    
    cd "$(dirname "$0")/k8s"
    
    if [[ $DRY_RUN == "true" ]]; then
        log_info "干运行模式 - 将要执行的命令:"
        [[ "${FORCE_REBUILD}" == "true" ]] && echo "docker build (miniodb + dashboard, --no-cache)"
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

    # 部署 Dashboard（client/server 分离）
    if [[ -d "dashboard" ]]; then
        kubectl apply -f dashboard/
        log_info "等待 Dashboard 部署完成..."
        kubectl wait --for=condition=available deployment/miniodb-dashboard -n "$namespace" --timeout=300s || log_warning "Dashboard 部署超时，请手动检查"
    else
        log_warning "未找到 k8s/dashboard/ 目录，跳过 Dashboard 部署"
    fi
    
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

# Swarm 集群部署（client/server 分离）
deploy_swarm() {
    local data_path=${DATA_PATH:-./data}

    log_info "开始 Docker Swarm 集群部署（client/server 分离）..."
    log_info "数据路径: $data_path"

    build_docker_image
    build_dashboard_image

    cd "$(dirname "$0")/ansible"

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
    log_info "服务架构: client(dashboard) + server(miniodb)"
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
                docker-compose down -v
            else
                docker compose down -v
            fi
            log_success "开发模式部署已清理"
            ;;
        "ansible")
            log_info "清理 Ansible 部署..."
            cd "$(dirname "$0")/ansible"
            if [[ ! -f "inventory/hosts.ini" ]]; then
                log_warning "清单文件不存在，请手动清理"
                return
            fi
            ansible-playbook -i inventory/hosts.ini site.yml --tags cleanup || log_warning "清理 playbook 执行失败，请手动清理"
            log_success "Ansible 部署已清理（或请手动清理）"
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
    local show_deps=false
    local auto_install_deps=false

    # 解析参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            dev|ansible|swarm|k8s)
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
            --install-deps)
                auto_install_deps=true
                AUTO_INSTALL_DEPS=true
                shift
                ;;
            --show-deps)
                show_deps=true
                shift
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --no-cache|--force-rebuild)
                FORCE_REBUILD=true
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

    # 显示离线下载地址
    if [[ "$show_deps" == "true" ]]; then
        if command -v show_offline_downloads &> /dev/null; then
            detect_system 2>/dev/null
            detect_package_manager 2>/dev/null
            show_offline_downloads "docker docker-compose kubectl ansible"
        else
            log_error "依赖管理脚本不可用"
        fi
        exit 0
    fi

    # 自动安装依赖
    if [[ "$auto_install_deps" == "true" ]]; then
        if command -v check_and_install &> /dev/null; then
            detect_system 2>/dev/null
            detect_package_manager 2>/dev/null
            check_and_install "docker docker-compose kubectl ansible"
            exit 0
        else
            log_error "依赖管理脚本不可用"
            exit 1
        fi
    fi

    # 检查部署模式
    if [[ -z "$deploy_mode" ]]; then
        log_error "请指定部署模式: dev, ansible, swarm, 或 k8s"
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
            "ansible")
                deploy_ansible
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
FORCE_REBUILD=${FORCE_REBUILD:-false}

# 执行主函数
main "$@"
exit 0