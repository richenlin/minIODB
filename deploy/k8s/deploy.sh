#!/bin/bash

# MinIODB Kubernetes 部署脚本
# 用于自动化部署MinIODB系统到Kubernetes集群

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

# 检查kubectl是否可用
check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl 未安装或不在PATH中"
        exit 1
    fi
    
    if ! kubectl cluster-info &> /dev/null; then
        log_error "无法连接到Kubernetes集群"
        exit 1
    fi
    
    log_success "kubectl 检查通过"
}

# 创建命名空间
create_namespace() {
    log_info "创建命名空间..."
    kubectl apply -f miniodb-config.yaml
    log_success "命名空间创建完成"
}

# 部署Redis
deploy_redis() {
    log_info "部署Redis..."
    kubectl apply -f redis/redis-config.yaml
    kubectl apply -f redis/redis-service.yaml
    kubectl apply -f redis/redis-statefulset.yaml
    
    log_info "等待Redis就绪..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=redis -n miniodb-system --timeout=300s
    log_success "Redis部署完成"
}

# 部署MinIO
deploy_minio() {
    log_info "部署MinIO..."
    kubectl apply -f minio/minio-service.yaml
    kubectl apply -f minio/minio-statefulset.yaml
    
    log_info "等待MinIO就绪..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio -n miniodb-system --timeout=300s
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=minio-backup -n miniodb-system --timeout=300s
    log_success "MinIO部署完成"
}

# 初始化MinIO
init_minio() {
    log_info "初始化MinIO..."
    kubectl apply -f minio/minio-init-job.yaml
    
    log_info "等待MinIO初始化完成..."
    kubectl wait --for=condition=complete job/minio-init -n miniodb-system --timeout=600s
    log_success "MinIO初始化完成"
}

# 部署MinIODB应用
deploy_miniodb() {
    log_info "部署MinIODB应用..."
    kubectl apply -f miniodb-deployment.yaml
    
    log_info "等待MinIODB应用就绪..."
    kubectl wait --for=condition=available deployment/miniodb -n miniodb-system --timeout=300s
    log_success "MinIODB应用部署完成"
}

# 显示部署状态
show_status() {
    log_info "=== 部署状态 ==="
    echo ""
    
    log_info "命名空间:"
    kubectl get namespace miniodb-system
    echo ""
    
    log_info "Pods:"
    kubectl get pods -n miniodb-system
    echo ""
    
    log_info "Services:"
    kubectl get services -n miniodb-system
    echo ""
    
    log_info "StatefulSets:"
    kubectl get statefulsets -n miniodb-system
    echo ""
    
    log_info "Deployments:"
    kubectl get deployments -n miniodb-system
    echo ""
    
    log_info "Jobs:"
    kubectl get jobs -n miniodb-system
    echo ""
}

# 显示访问信息
show_access_info() {
    log_info "=== 访问信息 ==="
    echo ""
    
    # 获取NodePort信息
    MINIODB_HTTP_PORT=$(kubectl get service miniodb-external -n miniodb-system -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
    MINIODB_GRPC_PORT=$(kubectl get service miniodb-external -n miniodb-system -o jsonpath='{.spec.ports[?(@.name=="grpc")].nodePort}')
    MINIO_API_PORT=$(kubectl get service minio-external -n miniodb-system -o jsonpath='{.spec.ports[?(@.name=="api")].nodePort}')
    MINIO_CONSOLE_PORT=$(kubectl get service minio-external -n miniodb-system -o jsonpath='{.spec.ports[?(@.name=="console")].nodePort}')
    
    # 获取节点IP
    NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}')
    if [ -z "$NODE_IP" ]; then
        NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
    fi
    
    log_info "MinIODB 应用访问地址:"
    echo "  HTTP API: http://${NODE_IP}:${MINIODB_HTTP_PORT}"
    echo "  gRPC API: ${NODE_IP}:${MINIODB_GRPC_PORT}"
    echo ""
    
    log_info "MinIO 管理界面:"
    echo "  API: http://${NODE_IP}:${MINIO_API_PORT}"
    echo "  Console: http://${NODE_IP}:${MINIO_CONSOLE_PORT}"
    echo "  用户名: minioadmin"
    echo "  密码: minioadmin123"
    echo ""
    
    log_info "内部服务地址 (集群内访问):"
    echo "  MinIODB: miniodb-service.miniodb-system.svc.cluster.local:8080"
    echo "  Redis: redis-service.miniodb-system.svc.cluster.local:6379"
    echo "  MinIO: minio-service.miniodb-system.svc.cluster.local:9000"
    echo ""
}

# 清理部署
cleanup() {
    log_warning "开始清理部署..."
    
    # 删除应用
    kubectl delete -f miniodb-deployment.yaml --ignore-not-found=true
    
    # 删除MinIO初始化Job
    kubectl delete -f minio/minio-init-job.yaml --ignore-not-found=true
    
    # 删除MinIO
    kubectl delete -f minio/minio-statefulset.yaml --ignore-not-found=true
    kubectl delete -f minio/minio-service.yaml --ignore-not-found=true
    
    # 删除Redis
    kubectl delete -f redis/redis-statefulset.yaml --ignore-not-found=true
    kubectl delete -f redis/redis-service.yaml --ignore-not-found=true
    kubectl delete -f redis/redis-config.yaml --ignore-not-found=true
    
    # 删除配置
    kubectl delete -f miniodb-config.yaml --ignore-not-found=true
    
    # 删除PVC (可选)
    read -p "是否删除持久化数据 (PVC)? [y/N]: " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        kubectl delete pvc -l app.kubernetes.io/part-of=miniodb-system -n miniodb-system --ignore-not-found=true
        log_warning "持久化数据已删除"
    fi
    
    log_success "清理完成"
}

# 主函数
main() {
    case "${1:-deploy}" in
        "deploy")
            log_info "=== 开始部署MinIODB到Kubernetes ==="
            check_kubectl
            create_namespace
            deploy_redis
            deploy_minio
            init_minio
            deploy_miniodb
            show_status
            show_access_info
            log_success "=== 部署完成 ==="
            ;;
        "status")
            check_kubectl
            show_status
            show_access_info
            ;;
        "cleanup")
            check_kubectl
            cleanup
            ;;
        "help"|"-h"|"--help")
            echo "用法: $0 [命令]"
            echo ""
            echo "命令:"
            echo "  deploy   - 部署MinIODB系统 (默认)"
            echo "  status   - 显示部署状态"
            echo "  cleanup  - 清理部署"
            echo "  help     - 显示帮助信息"
            echo ""
            ;;
        *)
            log_error "未知命令: $1"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
            ;;
    esac
}

# 错误处理
trap 'log_error "脚本执行中断"; exit 1' INT TERM

# 执行主函数
main "$@" 