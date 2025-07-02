#!/bin/bash

# MinIODB 性能测试运行脚本
# 用于部署单节点测试环境并执行性能测试

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 配置变量
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEPLOY_DIR="$PROJECT_ROOT/deploy/docker"
TEST_DIR="$PROJECT_ROOT/test/performance"
RESULTS_DIR="$TEST_DIR/results"
LOG_FILE="$RESULTS_DIR/performance_test_$(date +%Y%m%d_%H%M%S).log"

# 创建结果目录
mkdir -p "$RESULTS_DIR"

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" | tee -a "$LOG_FILE"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" | tee -a "$LOG_FILE"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" | tee -a "$LOG_FILE"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$LOG_FILE"
}

log_header() {
    echo -e "${PURPLE}$(printf '=%.0s' {1..80})${NC}" | tee -a "$LOG_FILE"
    echo -e "${PURPLE}$1${NC}" | tee -a "$LOG_FILE"
    echo -e "${PURPLE}$(printf '=%.0s' {1..80})${NC}" | tee -a "$LOG_FILE"
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
    
    if ! command -v go &> /dev/null; then
        missing_deps+=("go")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        log_error "缺少依赖: ${missing_deps[*]}"
        log_info "请安装缺少的依赖后重试"
        exit 1
    fi
    
    log_success "所有依赖检查通过"
}

# 清理环境
cleanup_environment() {
    log_info "清理现有环境..."
    
    cd "$DEPLOY_DIR"
    
    # 停止并删除容器
    if docker-compose ps -q | grep -q .; then
        log_info "停止现有服务..."
        docker-compose down -v --remove-orphans
    fi
    
    # 清理数据目录
    if [ -d "./data" ]; then
        log_info "清理数据目录..."
        sudo rm -rf ./data
    fi
    
    # 清理Docker镜像（可选）
    if [ "$CLEAN_IMAGES" = "true" ]; then
        log_info "清理Docker镜像..."
        docker image prune -f
        docker rmi miniodb:latest 2>/dev/null || true
    fi
    
    log_success "环境清理完成"
}

# 修复Go环境
fix_go_environment() {
    log_info "修复Go环境..."
    
    # 修复GOROOT
    export GOROOT="/opt/homebrew/Cellar/go/1.24.4/libexec"
    export GOPROXY="https://goproxy.cn,direct"
    
    log_info "Go环境设置："
    log_info "  GOROOT: $GOROOT"
    log_info "  GOPATH: $GOPATH"
    log_info "  GOPROXY: $GOPROXY"
    
    log_success "Go环境修复完成"
}

# 构建应用
build_application() {
    log_info "构建MinIODB应用..."
    
    cd "$PROJECT_ROOT"
    
    # 修复Go环境
    fix_go_environment
    
    # 构建Go应用
    log_info "编译Go应用..."
    CGO_ENABLED=1 go build -o minIODB cmd/server/main.go
    
    if [ ! -f "minIODB" ]; then
        log_error "应用构建失败"
        exit 1
    fi
    
    log_success "应用构建完成"
}

# 编译性能测试工具
build_test_tools() {
    log_info "编译性能测试工具..."
    
    cd "$TEST_DIR/tools"
    
    # 修复Go环境
    fix_go_environment
    
    # 编译数据生成器
    log_info "编译数据生成器..."
    if go build -o data_generator data_generator.go; then
        log_success "数据生成器编译成功"
    else
        log_warning "数据生成器编译失败，测试将使用默认数据"
    fi
    
    # 编译JWT生成器
    log_info "编译JWT生成器..."
    if go build -o jwt_generator jwt_generator.go; then
        log_success "JWT生成器编译成功"
    else
        log_warning "JWT生成器编译失败，测试将使用默认token"
    fi
    
    log_success "测试工具编译完成"
}

# 部署服务
deploy_services() {
    log_info "部署MinIODB服务..."
    
    cd "$DEPLOY_DIR"
    
    # 使用start.sh脚本启动服务
    log_info "使用start.sh启动服务..."
    ./start.sh
    
    log_success "服务部署完成"
}

# 等待服务就绪
wait_for_services() {
    log_info "验证服务状态..."
    
    # start.sh已经等待了服务启动，这里只需要简单验证
    sleep 5
    
    # 验证MinIODB健康状态
    if curl -f http://localhost:8081/v1/health &>/dev/null; then
        log_success "MinIODB服务验证成功"
    else
        log_warning "MinIODB服务可能需要更多时间启动，继续等待..."
        sleep 10
        if curl -f http://localhost:8081/v1/health &>/dev/null; then
            log_success "MinIODB服务验证成功"
        else
            log_error "MinIODB服务启动失败"
            exit 1
        fi
    fi
    
    log_success "所有服务已就绪"
}

# 运行性能测试
run_performance_tests() {
    log_header "开始性能测试"
    
    cd "$TEST_DIR"
    
    # 修复Go环境
    fix_go_environment
    
    # 设置Go模块
    if [ ! -f "go.mod" ]; then
        go mod init performance_test
        go mod tidy
    fi
    
    # 下载依赖
    log_info "下载测试依赖..."
    go get github.com/stretchr/testify/assert
    go get github.com/stretchr/testify/require
    
    # 运行性能测试
    log_info "执行性能测试套件..."
    
    # 设置测试环境变量
    export MINIODB_TEST_URL="http://localhost:8081"
    export MINIODB_TEST_TIMEOUT="300"
    
    # 运行测试并保存结果
    local test_output="$RESULTS_DIR/test_output_$(date +%Y%m%d_%H%M%S).log"
    
    if go test -v -timeout=30m ./... 2>&1 | tee "$test_output"; then
        log_success "性能测试执行完成"
    else
        log_warning "性能测试执行过程中出现问题，请检查详细日志"
    fi
    
    # 生成测试报告
    generate_test_report "$test_output"
}

# 生成测试报告
generate_test_report() {
    local test_output="$1"
    local report_file="$RESULTS_DIR/performance_report_$(date +%Y%m%d_%H%M%S).md"
    
    log_info "生成性能测试报告..."
    
    cat > "$report_file" << EOF
# MinIODB 性能测试报告

## 测试环境信息

- **测试时间**: $(date '+%Y-%m-%d %H:%M:%S')
- **测试环境**: 单节点Docker部署
- **系统信息**: $(uname -a)
- **Docker版本**: $(docker --version)
- **Go版本**: $(go version)

## 硬件配置

- **CPU**: $(lscpu | grep "Model name" | sed 's/Model name: *//')
- **内存**: $(free -h | awk '/^Mem:/ {print $2}')
- **磁盘**: $(df -h / | awk 'NR==2 {print $2}')

## 服务配置

### Redis配置
- **内存限制**: 2GB
- **持久化**: AOF + RDB
- **连接池大小**: 50

### MinIO配置
- **存储后端**: 本地文件系统
- **备份策略**: 双MinIO实例
- **数据分片**: 10个分区

### MinIODB配置
- **缓冲区大小**: 10,000条记录
- **批处理大小**: 1,000条记录
- **DuckDB内存限制**: 8GB
- **DuckDB线程数**: 8

## 测试结果摘要

EOF

    # 从测试输出中提取关键指标
    if [ -f "$test_output" ]; then
        # 提取批量插入性能
        if grep -q "BULK INSERT PERFORMANCE REPORT" "$test_output"; then
            echo "### 批量插入性能" >> "$report_file"
            echo "" >> "$report_file"
            grep -A 10 "BULK INSERT PERFORMANCE REPORT" "$test_output" | \
                sed 's/^/    /' >> "$report_file"
            echo "" >> "$report_file"
        fi
        
        # 提取查询性能
        if grep -q "QUERY PERFORMANCE REPORT" "$test_output"; then
            echo "### 查询性能" >> "$report_file"
            echo "" >> "$report_file"
            grep -A 20 "QUERY PERFORMANCE REPORT" "$test_output" | \
                sed 's/^/    /' >> "$report_file"
            echo "" >> "$report_file"
        fi
        
        # 提取混合负载性能
        if grep -q "MIXED WORKLOAD PERFORMANCE REPORT" "$test_output"; then
            echo "### 混合负载性能" >> "$report_file"
            echo "" >> "$report_file"
            grep -A 15 "MIXED WORKLOAD PERFORMANCE REPORT" "$test_output" | \
                sed 's/^/    /' >> "$report_file"
            echo "" >> "$report_file"
        fi
    fi
    
    # 添加系统资源使用情况
    cat >> "$report_file" << EOF

## 系统资源使用情况

### Docker容器状态
\`\`\`
$(docker-compose ps)
\`\`\`

### 容器资源使用
\`\`\`
$(docker stats --no-stream)
\`\`\`

### 磁盘使用情况
\`\`\`
$(df -h)
\`\`\`

## 性能分析与建议

### 性能瓶颈识别
- 检查CPU使用率是否达到瓶颈
- 检查内存使用是否充足
- 检查磁盘I/O是否成为限制因素
- 检查网络延迟对性能的影响

### 优化建议
1. **硬件优化**
   - 使用SSD存储提升I/O性能
   - 增加内存以支持更大的数据集
   - 使用多核CPU提升并行处理能力

2. **配置优化**
   - 调整DuckDB内存限制和线程数
   - 优化连接池大小
   - 调整批处理大小以平衡延迟和吞吐量

3. **架构优化**
   - 考虑水平扩展以支持更大负载
   - 实施读写分离以提升查询性能
   - 使用分布式缓存减少查询延迟

## 测试日志
详细测试日志请查看: \`$test_output\`

---
*报告生成时间: $(date '+%Y-%m-%d %H:%M:%S')*
EOF

    log_success "性能测试报告已生成: $report_file"
}

# 收集系统信息
collect_system_info() {
    log_info "收集系统信息..."
    
    local system_info_file="$RESULTS_DIR/system_info_$(date +%Y%m%d_%H%M%S).txt"
    
    {
        echo "=== 系统信息 ==="
        echo "时间: $(date)"
        echo "系统: $(uname -a)"
        echo ""
        
        echo "=== CPU信息 ==="
        if command -v lscpu &> /dev/null; then
            lscpu
        else
            # macOS使用sysctl获取CPU信息
            echo "CPU型号: $(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo 'Unknown')"
            echo "CPU核心数: $(sysctl -n hw.ncpu 2>/dev/null || echo 'Unknown')"
            echo "CPU架构: $(uname -m)"
        fi
        echo ""
        
        echo "=== 内存信息 ==="
        if command -v free &> /dev/null; then
            free -h
        else
            # macOS使用不同的命令获取内存信息
            echo "总内存: $(echo "$(sysctl -n hw.memsize 2>/dev/null || echo 0) / 1024 / 1024 / 1024" | bc 2>/dev/null || echo 'Unknown') GB"
            echo "可用内存: $(vm_stat | grep "Pages free" | awk '{print $3}' | sed 's/\.//' | awk '{print $1 * 4096 / 1024 / 1024}' 2>/dev/null || echo 'Unknown') MB"
        fi
        echo ""
        
        echo "=== 磁盘信息 ==="
        df -h
        echo ""
        
        echo "=== Docker信息 ==="
        docker version
        echo ""
        docker-compose version
        echo ""
        
        echo "=== Go信息 ==="
        go version
        echo ""
        go env
        echo ""
        
    } > "$system_info_file"
    
    log_success "系统信息已收集: $system_info_file"
}

# 显示服务状态
show_service_status() {
    log_header "服务状态信息"
    
    cd "$DEPLOY_DIR"
    
    echo -e "${CYAN}Docker Compose服务状态:${NC}"
    docker-compose ps
    echo ""
    
    echo -e "${CYAN}容器资源使用:${NC}"
    docker stats --no-stream
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

# 主函数
main() {
    log_header "MinIODB 性能测试启动"
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            --clean-images)
                CLEAN_IMAGES="true"
                shift
                ;;
            --skip-build)
                SKIP_BUILD="true"
                shift
                ;;
            --skip-deploy)
                SKIP_DEPLOY="true"
                shift
                ;;
            --test-only)
                TEST_ONLY="true"
                shift
                ;;
            -h|--help)
                echo "用法: $0 [选项]"
                echo "选项:"
                echo "  --clean-images    清理Docker镜像"
                echo "  --skip-build      跳过应用构建"
                echo "  --skip-deploy     跳过服务部署"
                echo "  --test-only       仅运行测试"
                echo "  -h, --help        显示帮助信息"
                exit 0
                ;;
            *)
                log_error "未知选项: $1"
                exit 1
                ;;
        esac
    done
    
    # 执行测试流程
    if [ "$TEST_ONLY" != "true" ]; then
        check_dependencies
        cleanup_environment
        
        if [ "$SKIP_BUILD" != "true" ]; then
            build_application
            build_test_tools
        fi
        
        if [ "$SKIP_DEPLOY" != "true" ]; then
            deploy_services
            wait_for_services
            show_service_status
        fi
    fi
    
    collect_system_info
    run_performance_tests
    
    log_header "性能测试完成"
    log_success "测试结果保存在: $RESULTS_DIR"
    log_info "查看详细报告: ls -la $RESULTS_DIR"
}

# 信号处理
trap 'log_warning "测试被中断"; exit 130' INT TERM

# 执行主函数
main "$@" 