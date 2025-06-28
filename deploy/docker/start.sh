#!/bin/bash

# MinIODB 多架构自动启动脚本
# 自动检测系统架构并使用相应的Dockerfile启动服务

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

log_header() {
    echo -e "${PURPLE}$(printf '=%.0s' {1..60})${NC}"
    echo -e "${PURPLE}$1${NC}"
    echo -e "${PURPLE}$(printf '=%.0s' {1..60})${NC}"
}

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB 多架构启动脚本

用法: $0 [选项] [命令]

命令:
  up           启动所有服务 (默认)
  down         停止所有服务
  restart      重启所有服务
  build        仅构建镜像
  logs         查看日志
  status       查看服务状态
  clean        清理资源

选项:
  --force-rebuild    强制重新构建镜像
  --no-cache         构建时不使用缓存
  --detach           后台运行 (默认)
  --foreground       前台运行
  -h, --help         显示帮助信息

示例:
  $0                    # 启动所有服务
  $0 up --force-rebuild # 强制重新构建并启动
  $0 down              # 停止所有服务
  $0 logs miniodb      # 查看miniodb服务日志

EOF
}

# 检测系统架构
detect_architecture() {
    log_header "检测系统架构"
    
    cd "$SCRIPT_DIR"
    
    # 运行架构检测脚本
    if ./detect-arch.sh; then
        log_success "架构检测完成"
        
        # 读取生成的架构配置
        if [ -f ".env.arch" ]; then
            source .env.arch
        else
            log_error "架构配置文件未找到"
            exit 1
        fi
    else
        log_error "架构检测失败"
        exit 1
    fi
}

# 检查依赖
check_dependencies() {
    log_info "检查系统依赖..."
    
    local missing_deps=()
    
    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    fi
    
    if ! command -v docker-compose &> /dev/null; then
        missing_deps+=("docker-compose")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        log_error "缺少依赖: ${missing_deps[*]}"
        log_info "请安装缺少的依赖后重试"
        exit 1
    fi
    
    log_success "所有依赖检查通过"
}

# 准备环境
prepare_environment() {
    log_info "准备部署环境..."
    
    cd "$SCRIPT_DIR"
    
    # 创建数据目录
    mkdir -p ./data/{redis,minio,minio-backup,logs}
    
    # 合并环境变量文件
    if [ -f ".env" ] && [ -f ".env.arch" ]; then
        # 创建临时的合并配置文件
        cat .env > .env.merged
        cat .env.arch >> .env.merged
        log_success "环境配置准备完成"
    else
        if [ ! -f ".env" ]; then
            log_error ".env 文件不存在，请从 env.example 复制并配置"
            exit 1
        fi
    fi
}

# 启动服务
start_services() {
    local force_rebuild=${1:-false}
    local no_cache=${2:-false}
    local detach=${3:-true}
    
    log_header "启动MinIODB服务"
    
    cd "$SCRIPT_DIR"
    
    # 构建参数
    local build_args=""
    if [ "$force_rebuild" = "true" ]; then
        build_args="$build_args --build"
    fi
    if [ "$no_cache" = "true" ]; then
        build_args="$build_args --no-cache"
    fi
    
    # 运行参数
    local run_args=""
    if [ "$detach" = "true" ]; then
        run_args="$run_args -d"
    fi
    
    # 使用合并的环境变量文件启动服务
    if docker-compose --env-file .env.merged up $build_args $run_args; then
        log_success "服务启动成功"
        
        # 显示服务状态
        show_service_status
    else
        log_error "服务启动失败"
        exit 1
    fi
}

# 停止服务
stop_services() {
    log_header "停止MinIODB服务"
    
    cd "$SCRIPT_DIR"
    
    if docker-compose down; then
        log_success "服务停止成功"
    else
        log_error "服务停止失败"
        exit 1
    fi
}

# 重启服务
restart_services() {
    log_header "重启MinIODB服务"
    stop_services
    start_services
}

# 构建镜像
build_images() {
    local no_cache=${1:-false}
    
    log_header "构建MinIODB镜像"
    
    cd "$SCRIPT_DIR"
    
    local build_args=""
    if [ "$no_cache" = "true" ]; then
        build_args="$build_args --no-cache"
    fi
    
    if docker-compose --env-file .env.merged build $build_args; then
        log_success "镜像构建成功"
    else
        log_error "镜像构建失败"
        exit 1
    fi
}

# 查看日志
show_logs() {
    local service=${1:-}
    
    cd "$SCRIPT_DIR"
    
    if [ -n "$service" ]; then
        docker-compose logs -f "$service"
    else
        docker-compose logs -f
    fi
}

# 显示服务状态
show_service_status() {
    log_header "服务状态"
    
    cd "$SCRIPT_DIR"
    
    echo -e "${CYAN}Docker Compose服务状态:${NC}"
    docker-compose ps
    echo ""
    
    echo -e "${CYAN}服务端点:${NC}"
    echo "- MinIODB REST API: http://localhost:8081"
    echo "- MinIODB gRPC API: http://localhost:8080"
    echo "- MinIODB监控: http://localhost:9090/metrics"
    echo "- MinIO控制台: http://localhost:9001"
    echo "- MinIO备份控制台: http://localhost:9003"
    echo "- Redis: localhost:6379"
    echo ""
}

# 清理资源
clean_resources() {
    log_header "清理系统资源"
    
    cd "$SCRIPT_DIR"
    
    # 停止并删除容器
    docker-compose down -v --remove-orphans
    
    # 删除未使用的镜像
    docker image prune -f
    
    # 清理临时文件
    rm -f .env.merged .env.arch
    
    log_success "资源清理完成"
}

# 主函数
main() {
    local command="up"
    local force_rebuild=false
    local no_cache=false
    local detach=true
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            up|down|restart|build|logs|status|clean)
                command="$1"
                shift
                ;;
            --force-rebuild)
                force_rebuild=true
                shift
                ;;
            --no-cache)
                no_cache=true
                shift
                ;;
            --detach)
                detach=true
                shift
                ;;
            --foreground)
                detach=false
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                if [ "$command" = "logs" ] && [ -z "$2" ]; then
                    # logs命令后跟服务名
                    show_logs "$1"
                    exit 0
                else
                    log_error "未知选项: $1"
                    show_help
                    exit 1
                fi
                ;;
        esac
    done
    
    # 基础检查
    check_dependencies
    detect_architecture
    prepare_environment
    
    # 执行命令
    case $command in
        up)
            start_services "$force_rebuild" "$no_cache" "$detach"
            ;;
        down)
            stop_services
            ;;
        restart)
            restart_services
            ;;
        build)
            build_images "$no_cache"
            ;;
        logs)
            show_logs
            ;;
        status)
            show_service_status
            ;;
        clean)
            clean_resources
            ;;
        *)
            log_error "未知命令: $command"
            show_help
            exit 1
            ;;
    esac
}

# 信号处理
trap 'log_warning "操作被中断"; exit 130' INT TERM

# 执行主函数
main "$@" 