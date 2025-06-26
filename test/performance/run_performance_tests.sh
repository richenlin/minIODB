#!/bin/bash

# MinIODB 性能测试脚本
# 用于部署环境并运行性能测试

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
DOCKER_COMPOSE_PATH="../../deploy/docker"
TEST_RESULTS_DIR="./results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RESULTS_FILE="${TEST_RESULTS_DIR}/performance_report_${TIMESTAMP}.txt"

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
    
    if ! command -v docker-compose &> /dev/null; then
        log_error "Docker Compose 未安装"
        exit 1
    fi
    
    if ! command -v go &> /dev/null; then
        log_error "Go 未安装"
        exit 1
    fi
    
    log_success "依赖检查通过"
}

# 创建结果目录
create_results_dir() {
    mkdir -p "$TEST_RESULTS_DIR"
    log_info "创建结果目录: $TEST_RESULTS_DIR"
}

# 部署服务
deploy_services() {
    log_info "部署MinIODB服务..."
    
    cd "$DOCKER_COMPOSE_PATH"
    
    # 停止现有服务
    docker-compose down -v || true
    
    # 构建并启动服务
    docker-compose up -d --build
    
    log_info "等待服务启动..."
    sleep 30
    
    # 检查服务状态
    if ! docker-compose ps | grep -q "Up"; then
        log_error "服务启动失败"
        docker-compose logs
        exit 1
    fi
    
    # 健康检查
    max_attempts=30
    attempt=1
    while [ $attempt -le $max_attempts ]; do
        if curl -f http://localhost:8081/v1/health &> /dev/null; then
            log_success "服务健康检查通过"
            break
        fi
        
        log_info "等待服务就绪... ($attempt/$max_attempts)"
        sleep 2
        ((attempt++))
    done
    
    if [ $attempt -gt $max_attempts ]; then
        log_error "服务健康检查超时"
        docker-compose logs miniodb
        exit 1
    fi
    
    cd - > /dev/null
}

# 运行性能测试
run_performance_tests() {
    log_info "开始性能测试..."
    
    # 创建测试报告头部
    cat > "$RESULTS_FILE" << EOF
# MinIODB 性能测试报告
测试时间: $(date)
测试环境: Docker Compose 单节点部署
系统信息: $(uname -a)
Go版本: $(go version)

## 系统资源信息
CPU: $(lscpu | grep "Model name" | head -1 | awk -F: '{print $2}' | xargs)
CPU核心数: $(nproc)
内存: $(free -h | grep "Mem:" | awk '{print $2}')
磁盘: $(df -h / | tail -1 | awk '{print $2}')

## Docker服务状态
EOF
    
    cd "$DOCKER_COMPOSE_PATH"
    docker-compose ps >> "$RESULTS_FILE" 2>&1
    cd - > /dev/null
    
    echo "" >> "$RESULTS_FILE"
    echo "## 性能测试结果" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # 运行不同的性能测试
    local tests=(
        "BenchmarkWriteThroughput"
        "BenchmarkQueryPerformance"  
        "BenchmarkLargeDatasetQuery"
        "BenchmarkConcurrentMixedWorkload"
    )
    
    for test in "${tests[@]}"; do
        log_info "运行测试: $test"
        
        echo "### $test" >> "$RESULTS_FILE"
        echo "\`\`\`" >> "$RESULTS_FILE"
        
        # 运行基准测试，设置较短的运行时间以便快速获得结果
        if go test -bench="^$test$" -benchtime=10s -timeout=30m -v . >> "$RESULTS_FILE" 2>&1; then
            log_success "测试 $test 完成"
        else
            log_warning "测试 $test 可能出现问题，请检查结果"
        fi
        
        echo "\`\`\`" >> "$RESULTS_FILE"
        echo "" >> "$RESULTS_FILE"
    done
}

# 收集系统指标
collect_system_metrics() {
    log_info "收集系统指标..."
    
    echo "## 系统指标" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # CPU使用率
    echo "### CPU使用率" >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    top -bn1 | grep "Cpu(s)" >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # 内存使用
    echo "### 内存使用" >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    free -h >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # 磁盘使用
    echo "### 磁盘使用" >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    df -h >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # Docker容器资源使用
    echo "### Docker容器资源使用" >> "$RESULTS_FILE"
    echo "\`\`\`" >> "$RESULTS_FILE"
    docker stats --no-stream >> "$RESULTS_FILE" 2>&1
    echo "\`\`\`" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
}

# 收集应用指标
collect_app_metrics() {
    log_info "收集应用指标..."
    
    echo "## 应用指标" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # Prometheus指标
    if curl -s http://localhost:9090/metrics > /dev/null 2>&1; then
        echo "### Prometheus指标 (部分)" >> "$RESULTS_FILE"
        echo "\`\`\`" >> "$RESULTS_FILE"
        curl -s http://localhost:9090/metrics | grep -E "(miniodb_|go_|process_)" | head -20 >> "$RESULTS_FILE"
        echo "\`\`\`" >> "$RESULTS_FILE"
        echo "" >> "$RESULTS_FILE"
    fi
    
    # 应用统计
    if curl -s http://localhost:8081/v1/stats > /dev/null 2>&1; then
        echo "### 应用统计" >> "$RESULTS_FILE"
        echo "\`\`\`" >> "$RESULTS_FILE"
        curl -s http://localhost:8081/v1/stats | jq . >> "$RESULTS_FILE" 2>&1 || curl -s http://localhost:8081/v1/stats >> "$RESULTS_FILE"
        echo "\`\`\`" >> "$RESULTS_FILE"
        echo "" >> "$RESULTS_FILE"
    fi
}

# 分析性能瓶颈
analyze_performance() {
    log_info "分析性能瓶颈..."
    
    echo "## 性能分析" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # 分析测试结果
    echo "### 测试结果分析" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # 提取关键指标
    local write_throughput=$(grep -E "writes/sec|ms/write" "$RESULTS_FILE" | head -5 | tail -1 || echo "未找到写入吞吐量数据")
    local query_latency=$(grep -E "ms/query" "$RESULTS_FILE" | head -5 | tail -1 || echo "未找到查询延迟数据")
    
    cat >> "$RESULTS_FILE" << EOF
**关键性能指标:**
- 写入吞吐量: $write_throughput
- 查询延迟: $query_latency

**性能瓶颈识别:**
1. **写入性能**: 
   - 如果写入吞吐量低于1000 writes/sec，可能的瓶颈：
     - 缓冲区配置过小
     - 磁盘I/O性能不足
     - 网络延迟过高
     - MinIO存储性能限制

2. **查询性能**:
   - 如果查询延迟超过100ms，可能的瓶颈：
     - DuckDB内存配置不足
     - 数据文件过多导致查询计划复杂
     - 缺少适当的索引
     - 并发查询过多

3. **系统资源**:
   - CPU使用率持续超过80%: 需要增加CPU资源或优化算法
   - 内存使用率超过85%: 需要增加内存或优化内存使用
   - 磁盘I/O wait过高: 需要使用更快的存储设备

**优化建议:**
1. **配置优化**:
   - 增大缓冲区大小 (BUFFER_SIZE)
   - 调整刷新间隔 (BUFFER_TIMEOUT)
   - 优化DuckDB内存限制
   - 增加连接池大小

2. **架构优化**:
   - 实现数据分区策略
   - 优化查询计划生成
   - 添加查询结果缓存
   - 实现读写分离

3. **硬件优化**:
   - 使用SSD存储
   - 增加内存容量
   - 使用更快的网络
   - 增加CPU核心数

EOF
}

# 生成性能报告
generate_report() {
    log_info "生成性能报告..."
    
    echo "## 测试总结" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    echo "测试完成时间: $(date)" >> "$RESULTS_FILE"
    echo "报告文件: $RESULTS_FILE" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # 显示报告摘要
    log_success "性能测试完成！"
    echo ""
    echo "报告摘要:"
    echo "=========="
    grep -E "writes/sec|ms/query|MB/s" "$RESULTS_FILE" | head -10 || echo "未找到性能指标"
    echo ""
    echo "完整报告: $RESULTS_FILE"
}

# 清理环境
cleanup() {
    log_info "清理环境..."
    
    cd "$DOCKER_COMPOSE_PATH"
    docker-compose down -v
    cd - > /dev/null
    
    log_success "环境清理完成"
}

# 主函数
main() {
    log_info "开始MinIODB性能测试"
    
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
    create_results_dir
    deploy_services
    
    # 运行测试
    run_performance_tests
    
    # 收集指标
    collect_system_metrics
    collect_app_metrics
    
    # 分析结果
    analyze_performance
    generate_report
    
    # 清理环境（可选）
    if [[ "$1" != "--no-cleanup" ]]; then
        cleanup
    else
        log_info "跳过环境清理（使用 --no-cleanup 选项）"
    fi
    
    log_success "性能测试流程完成！"
}

# 捕获中断信号
trap cleanup EXIT

# 运行主函数
main "$@" 