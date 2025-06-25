#!/bin/bash

# MinIODB 健康检查脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 默认配置
DOCKER_MODE=false
K8S_MODE=false
NAMESPACE="miniodb-system"
TIMEOUT=30
VERBOSE=false

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
MinIODB 健康检查脚本

用法:
    $0 [选项]

选项:
    -h, --help              显示帮助信息
    -d, --docker            检查 Docker Compose 部署
    -k, --k8s               检查 Kubernetes 部署
    -n, --namespace NAME    指定 Kubernetes 命名空间 (默认: miniodb-system)
    -t, --timeout SECONDS  超时时间 (默认: 30秒)
    -v, --verbose           详细输出

示例:
    $0 -d                   # 检查 Docker Compose 部署
    $0 -k                   # 检查 Kubernetes 部署
    $0 -k -n my-namespace   # 检查指定命名空间的 K8s 部署

EOF
}

# 检查 HTTP 服务
check_http_service() {
    local name=$1
    local url=$2
    local expected_status=${3:-200}
    
    if [[ "$VERBOSE" == "true" ]]; then
        log_info "检查 $name: $url"
    fi
    
    local status_code
    status_code=$(curl -s -o /dev/null -w "%{http_code}" --max-time "$TIMEOUT" "$url" 2>/dev/null || echo "000")
    
    if [[ "$status_code" == "$expected_status" ]]; then
        log_success "$name 健康检查通过 (HTTP $status_code)"
        return 0
    else
        log_error "$name 健康检查失败 (HTTP $status_code)"
        return 1
    fi
}

# 检查 TCP 端口
check_tcp_port() {
    local name=$1
    local host=$2
    local port=$3
    
    if [[ "$VERBOSE" == "true" ]]; then
        log_info "检查 $name TCP 连接: $host:$port"
    fi
    
    if timeout "$TIMEOUT" bash -c "</dev/tcp/$host/$port" 2>/dev/null; then
        log_success "$name TCP 连接正常"
        return 0
    else
        log_error "$name TCP 连接失败"
        return 1
    fi
}

# Docker Compose 健康检查
check_docker_health() {
    log_info "开始 Docker Compose 健康检查..."
    
    # 检查 Docker Compose 是否运行
    if ! docker-compose ps &>/dev/null; then
        log_error "Docker Compose 未运行或配置文件不存在"
        return 1
    fi
    
    local failed=0
    
    # 检查容器状态
    log_info "检查容器状态..."
    local containers=("miniodb-redis" "miniodb-minio" "miniodb-minio-backup" "miniodb-app")
    
    for container in "${containers[@]}"; do
        if docker ps --filter "name=$container" --filter "status=running" --format "{{.Names}}" | grep -q "$container"; then
            log_success "容器 $container 运行正常"
        else
            log_error "容器 $container 未运行"
            ((failed++))
        fi
    done
    
    # 检查服务端点
    log_info "检查服务端点..."
    
    # MinIODB REST API
    if ! check_http_service "MinIODB REST API" "http://localhost:8081/v1/health"; then
        ((failed++))
    fi
    
    # MinIODB gRPC (TCP 检查)
    if ! check_tcp_port "MinIODB gRPC" "localhost" "8080"; then
        ((failed++))
    fi
    
    # MinIODB Metrics
    if ! check_http_service "MinIODB Metrics" "http://localhost:9090/metrics"; then
        ((failed++))
    fi
    
    # MinIO API
    if ! check_http_service "MinIO API" "http://localhost:9000/minio/health/live"; then
        ((failed++))
    fi
    
    # MinIO Console
    if ! check_http_service "MinIO Console" "http://localhost:9001" "200"; then
        ((failed++))
    fi
    
    # MinIO Backup API
    if ! check_http_service "MinIO Backup API" "http://localhost:9002/minio/health/live"; then
        ((failed++))
    fi
    
    # Redis
    if ! check_tcp_port "Redis" "localhost" "6379"; then
        ((failed++))
    fi
    
    # 显示结果
    if [[ $failed -eq 0 ]]; then
        log_success "所有服务健康检查通过!"
        echo
        echo "服务访问地址:"
        echo "  REST API:      http://localhost:8081"
        echo "  gRPC API:      localhost:8080"
        echo "  MinIO Console: http://localhost:9001"
        echo "  MinIO Backup:  http://localhost:9003"
        echo "  监控指标:      http://localhost:9090/metrics"
        return 0
    else
        log_error "有 $failed 个服务健康检查失败"
        return 1
    fi
}

# Kubernetes 健康检查
check_k8s_health() {
    log_info "开始 Kubernetes 健康检查..."
    
    # 检查命名空间是否存在
    if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
        log_error "命名空间 $NAMESPACE 不存在"
        return 1
    fi
    
    local failed=0
    
    # 检查 Pod 状态
    log_info "检查 Pod 状态..."
    local pods
    pods=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null)
    
    if [[ -z "$pods" ]]; then
        log_error "命名空间 $NAMESPACE 中没有 Pod"
        return 1
    fi
    
    echo "$pods" | while read -r name ready status restarts age; do
        if [[ "$status" == "Running" && "$ready" =~ ^[0-9]+/[0-9]+$ ]]; then
            local ready_count ready_total
            ready_count=$(echo "$ready" | cut -d'/' -f1)
            ready_total=$(echo "$ready" | cut -d'/' -f2)
            
            if [[ "$ready_count" == "$ready_total" ]]; then
                log_success "Pod $name 运行正常 ($ready)"
            else
                log_warning "Pod $name 未完全就绪 ($ready)"
                ((failed++))
            fi
        else
            log_error "Pod $name 状态异常: $status ($ready)"
            ((failed++))
        fi
    done
    
    # 检查服务状态
    log_info "检查服务状态..."
    local services
    services=$(kubectl get services -n "$NAMESPACE" --no-headers 2>/dev/null)
    
    echo "$services" | while read -r name type cluster_ip external_ip ports age; do
        if [[ -n "$cluster_ip" && "$cluster_ip" != "<none>" ]]; then
            log_success "Service $name 正常 ($type)"
        else
            log_warning "Service $name 可能有问题"
        fi
    done
    
    # 检查 PVC 状态
    log_info "检查存储卷状态..."
    local pvcs
    pvcs=$(kubectl get pvc -n "$NAMESPACE" --no-headers 2>/dev/null)
    
    if [[ -n "$pvcs" ]]; then
        echo "$pvcs" | while read -r name status volume capacity access_modes storage_class age; do
            if [[ "$status" == "Bound" ]]; then
                log_success "PVC $name 绑定正常 ($capacity)"
            else
                log_error "PVC $name 状态异常: $status"
                ((failed++))
            fi
        done
    fi
    
    # 端口转发测试 (可选)
    if [[ "$VERBOSE" == "true" ]]; then
        log_info "测试服务连接..."
        
        # 测试 MinIODB REST API
        local pod_name
        pod_name=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=miniodb -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
        
        if [[ -n "$pod_name" ]]; then
            if kubectl exec -n "$NAMESPACE" "$pod_name" -- curl -f http://localhost:8081/v1/health &>/dev/null; then
                log_success "MinIODB 服务内部健康检查通过"
            else
                log_warning "MinIODB 服务内部健康检查失败"
            fi
        fi
    fi
    
    # 显示结果
    if [[ $failed -eq 0 ]]; then
        log_success "Kubernetes 部署健康检查通过!"
        echo
        echo "使用以下命令访问服务:"
        echo "  kubectl port-forward -n $NAMESPACE svc/miniodb-service 8081:8081"
        echo "  kubectl port-forward -n $NAMESPACE svc/minio-service 9001:9001"
        echo
        echo "查看详细状态:"
        echo "  kubectl get pods -n $NAMESPACE"
        echo "  kubectl get services -n $NAMESPACE"
        return 0
    else
        log_error "Kubernetes 部署健康检查失败"
        return 1
    fi
}

# 主函数
main() {
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -d|--docker)
                DOCKER_MODE=true
                shift
                ;;
            -k|--k8s)
                K8S_MODE=true
                shift
                ;;
            -n|--namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            *)
                log_error "未知参数: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 检查模式
    if [[ "$DOCKER_MODE" == "false" && "$K8S_MODE" == "false" ]]; then
        # 自动检测模式
        if docker-compose ps &>/dev/null; then
            DOCKER_MODE=true
            log_info "自动检测到 Docker Compose 部署"
        elif kubectl get namespace "$NAMESPACE" &>/dev/null; then
            K8S_MODE=true
            log_info "自动检测到 Kubernetes 部署"
        else
            log_error "未检测到部署，请指定 -d 或 -k 参数"
            exit 1
        fi
    fi
    
    if [[ "$DOCKER_MODE" == "true" && "$K8S_MODE" == "true" ]]; then
        log_error "不能同时指定 Docker 和 Kubernetes 模式"
        exit 1
    fi
    
    # 执行健康检查
    if [[ "$DOCKER_MODE" == "true" ]]; then
        # 切换到 Docker 部署目录
        if [[ -f "docker-compose.yml" ]]; then
            # 当前目录
            :
        elif [[ -f "deploy/docker/docker-compose.yml" ]]; then
            cd deploy/docker
        elif [[ -f "../docker/docker-compose.yml" ]]; then
            cd ../docker
        else
            log_error "找不到 docker-compose.yml 文件"
            exit 1
        fi
        
        check_docker_health
    elif [[ "$K8S_MODE" == "true" ]]; then
        check_k8s_health
    fi
}

# 脚本入口
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi 