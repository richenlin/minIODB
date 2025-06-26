#!/bin/bash

# MinIODB 快速性能测试脚本
# 用于快速部署和测试基本性能

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
DOCKER_COMPOSE_PATH="../../deploy/docker"
TEST_RECORDS=100000  # 10万条记录用于快速测试
BATCH_SIZE=1000
CONCURRENCY=5

# 函数定义
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

# 检查依赖
check_dependencies() {
    log_info "检查依赖..."
    
    if ! command -v docker &> /dev/null; then
        log_error "Docker 未安装"
        exit 1
    fi
    
    if ! command -v sudo docker-compose &> /dev/null; then
        log_error "Docker Compose 未安装"
        exit 1
    fi
    
    if ! command -v go &> /dev/null; then
        log_error "Go 未安装"
        exit 1
    fi
    
    log_success "依赖检查通过"
}

# 部署服务
deploy_services() {
    log_info "部署MinIODB服务..."
    
    cd "$DOCKER_COMPOSE_PATH"
    
    # 停止现有服务
    sudo docker-compose down -v || true
    
    # 构建并启动服务
    sudo docker-compose up -d --build
    
    log_info "等待服务启动..."
    sleep 20
    
    # 健康检查
    max_attempts=20
    attempt=1
    while [ $attempt -le $max_attempts ]; do
        if curl -f http://localhost:8081/v1/health &> /dev/null; then
            log_success "服务健康检查通过"
            break
        fi
        
        log_info "等待服务就绪... ($attempt/$max_attempts)"
        sleep 3
        ((attempt++))
    done
    
    if [ $attempt -gt $max_attempts ]; then
        log_error "服务健康检查超时"
        sudo docker-compose logs miniodb
        exit 1
    fi
    
    cd - > /dev/null
}

# 生成测试数据
generate_test_data() {
    log_info "生成测试数据..."
    
    # 编译数据生成器
    cd tools && go build -o data_generator data_generator.go && cd ..
    
    # 生成测试数据
    ./tools/data_generator \
        -url="http://localhost:8081" \
        -records=$TEST_RECORDS \
        -batch=$BATCH_SIZE \
        -concurrency=$CONCURRENCY \
        -table="quick_test" \
        -v
    
    if [ $? -eq 0 ]; then
        log_success "测试数据生成完成"
    else
        log_error "测试数据生成失败"
        exit 1
    fi
}

# 运行简单查询测试
run_query_tests() {
    log_info "运行查询测试..."
    
    # 测试查询
    local queries=(
        "SELECT COUNT(*) FROM quick_test"
        "SELECT status, COUNT(*) FROM quick_test GROUP BY status"
        "SELECT category, AVG(amount) FROM quick_test GROUP BY category"
        "SELECT region, SUM(amount) FROM quick_test GROUP BY region ORDER BY SUM(amount) DESC LIMIT 5"
        "SELECT * FROM quick_test WHERE amount > 500 LIMIT 10"
    )
    
    for query in "${queries[@]}"; do
        log_info "执行查询: $query"
        
        start_time=$(date +%s.%N)
        
        response=$(curl -s -X POST http://localhost:8081/v1/query \
            -H "Content-Type: application/json" \
            -d "{\"sql\":\"$query\"}")
        
        end_time=$(date +%s.%N)
        duration=$(echo "$end_time - $start_time" | bc)
        
        if echo "$response" | grep -q '"success":true'; then
            log_success "查询成功，耗时: ${duration}s"
        else
            log_warning "查询可能失败: $response"
        fi
        
        sleep 1
    done
}

# 检查系统资源
check_system_resources() {
    log_info "系统资源使用情况:"
    
    echo "CPU使用率:"
    top -bn1 | grep "Cpu(s)" | head -1
    
    echo ""
    echo "内存使用:"
    free -h
    
    echo ""
    echo "磁盘使用:"
    df -h /
    
    echo ""
    echo "Docker容器状态:"
    cd "$DOCKER_COMPOSE_PATH"
    sudo docker-compose ps
    cd - > /dev/null
    
    echo ""
    echo "Docker容器资源使用:"
    docker stats --no-stream
}

# 获取应用统计
get_app_stats() {
    log_info "获取应用统计信息..."
    
    # 健康状态
    echo "健康状态:"
    curl -s http://localhost:8081/v1/health | jq . 2>/dev/null || curl -s http://localhost:8081/v1/health
    
    echo ""
    echo "应用统计:"
    curl -s http://localhost:8081/v1/stats | jq . 2>/dev/null || curl -s http://localhost:8081/v1/stats
}

# 清理环境
cleanup() {
    log_info "清理环境..."
    
    # 清理生成的文件
    rm -f tools/data_generator
    
    # 停止Docker服务
    cd "$DOCKER_COMPOSE_PATH"
    sudo docker-compose down -v
    cd - > /dev/null
    
    log_success "环境清理完成"
}

# 主函数
main() {
    echo "MinIODB 快速性能测试"
    echo "===================="
    echo "测试记录数: $TEST_RECORDS"
    echo "批次大小: $BATCH_SIZE"
    echo "并发数: $CONCURRENCY"
    echo "===================="
    echo ""
    
    # 检查参数
    if [[ "$1" == "--help" || "$1" == "-h" ]]; then
        echo "用法: $0 [--no-cleanup]"
        echo "选项:"
        echo "  --no-cleanup  测试完成后不清理Docker环境"
        echo "  --help, -h    显示帮助信息"
        exit 0
    fi
    
    # 执行测试流程
    check_dependencies
    deploy_services
    
    # 等待服务完全启动
    sleep 5
    
    # 生成测试数据
    generate_test_data
    
    # 等待数据处理
    sleep 5
    
    # 运行查询测试
    run_query_tests
    
    # 检查系统资源
    echo ""
    echo "===================="
    check_system_resources
    
    # 获取应用统计
    echo ""
    echo "===================="
    get_app_stats
    
    echo ""
    echo "===================="
    log_success "快速性能测试完成！"
    
    # 清理环境（可选）
    if [[ "$1" != "--no-cleanup" ]]; then
        cleanup
    else
        log_info "跳过环境清理（使用 --no-cleanup 选项）"
        log_info "可以手动运行以下命令清理环境:"
        log_info "cd $DOCKER_COMPOSE_PATH && sudo docker-compose down -v"
    fi
}

# 捕获中断信号
trap cleanup EXIT

# 运行主函数
main "$@" 