#!/bin/bash

# MinIODB 负载测试脚本
# 测试系统在高并发场景下的性能表现

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 测试配置
MINIODB_REST_API="http://localhost:8081"
CONCURRENT_USERS=${CONCURRENT_USERS:-10}
REQUESTS_PER_USER=${REQUESTS_PER_USER:-100}
TEST_DURATION=${TEST_DURATION:-60}
RAMP_UP_TIME=${RAMP_UP_TIME:-10}

# 测试结果文件
RESULTS_DIR="./results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RESULT_FILE="$RESULTS_DIR/load_test_$TIMESTAMP.json"

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
    echo -e "${PURPLE}$(printf '=%.0s' {1..80})${NC}"
    echo -e "${PURPLE}$1${NC}"
    echo -e "${PURPLE}$(printf '=%.0s' {1..80})${NC}"
}

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB 负载测试脚本

用法: $0 [选项]

选项:
  --users NUM          并发用户数 (默认: 10)
  --requests NUM       每用户请求数 (默认: 100)
  --duration SEC       测试持续时间秒数 (默认: 60)
  --rampup SEC         爬坡时间秒数 (默认: 10)
  --endpoint URL       MinIODB API端点 (默认: http://localhost:8081)
  -h, --help           显示帮助信息

环境变量:
  CONCURRENT_USERS     并发用户数
  REQUESTS_PER_USER    每用户请求数
  TEST_DURATION        测试持续时间
  RAMP_UP_TIME         爬坡时间

示例:
  $0                                    # 使用默认参数
  $0 --users 20 --requests 200          # 20个用户，每人200请求
  $0 --duration 120 --rampup 30         # 测试2分钟，爬坡30秒

EOF
}

# 检查依赖
check_dependencies() {
    log_info "检查依赖..."
    
    local missing_deps=()
    
    if ! command -v curl &> /dev/null; then
        missing_deps+=("curl")
    fi
    
    if ! command -v jq &> /dev/null; then
        missing_deps+=("jq")
    fi
    
    if ! command -v bc &> /dev/null; then
        missing_deps+=("bc")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        log_error "缺少依赖: ${missing_deps[*]}"
        exit 1
    fi
    
    log_success "依赖检查通过"
}

# 检查服务可用性
check_service_health() {
    log_info "检查服务健康状态..."
    
    if curl -sf "$MINIODB_REST_API/v1/health" >/dev/null 2>&1; then
        log_success "MinIODB服务正常"
        return 0
    else
        log_error "MinIODB服务不可用"
        return 1
    fi
}

# 准备测试环境
prepare_test_environment() {
    log_info "准备测试环境..."
    
    # 创建结果目录
    mkdir -p "$RESULTS_DIR"
    
    # 创建测试表
    log_info "创建测试表..."
    local payload='{"table_name":"load_test_table","config":{"buffer_size":10000,"flush_interval_seconds":5,"retention_days":1},"if_not_exists":true}'
    
    if curl -sf -X POST "$MINIODB_REST_API/v1/tables" \
        -H "Content-Type: application/json" \
        -d "$payload" >/dev/null 2>&1; then
        log_success "测试表创建成功"
    else
        log_warning "测试表创建失败，可能已存在"
    fi
}

# 单个用户测试函数
run_user_test() {
    local user_id=$1
    local requests=$2
    local test_start_time=$3
    
    local success_count=0
    local error_count=0
    local total_time=0
    
    for ((i=1; i<=requests; i++)); do
        local start_time=$(date +%s.%N)
        
        # 生成测试数据
        local record_id="load_user_${user_id}_${i}"
        local payload="{\"table\":\"load_test_table\",\"id\":\"$record_id\",\"timestamp\":$(date +%s),\"payload\":{\"user_id\":$user_id,\"sequence\":$i,\"test_data\":\"$(openssl rand -hex 16)\",\"timestamp\":\"$(date -Iseconds)\"}}"
        
        # 发送请求
        if curl -sf -X POST "$MINIODB_REST_API/v1/data" \
            -H "Content-Type: application/json" \
            -d "$payload" >/dev/null 2>&1; then
            success_count=$((success_count + 1))
        else
            error_count=$((error_count + 1))
        fi
        
        local end_time=$(date +%s.%N)
        local request_time=$(echo "$end_time - $start_time" | bc -l)
        total_time=$(echo "$total_time + $request_time" | bc -l)
        
        # 避免过于频繁的请求
        sleep 0.01
    done
    
    local avg_time=$(echo "scale=3; $total_time / $requests" | bc -l)
    
    # 输出用户测试结果
    echo "{\"user_id\":$user_id,\"requests\":$requests,\"success\":$success_count,\"errors\":$error_count,\"avg_response_time\":$avg_time,\"total_time\":$total_time}"
}

# 执行负载测试
run_load_test() {
    log_header "开始负载测试"
    
    log_info "测试配置:"
    echo "  并发用户: $CONCURRENT_USERS"
    echo "  每用户请求数: $REQUESTS_PER_USER"
    echo "  测试持续时间: $TEST_DURATION 秒"
    echo "  爬坡时间: $RAMP_UP_TIME 秒"
    echo "  API端点: $MINIODB_REST_API"
    echo ""
    
    local test_start_time=$(date +%s)
    local pids=()
    local user_results=()
    
    # 启动并发用户测试
    log_info "启动 $CONCURRENT_USERS 个并发用户..."
    for ((user=1; user<=CONCURRENT_USERS; user++)); do
        # 计算爬坡延迟
        local delay=$(echo "scale=2; ($user - 1) * $RAMP_UP_TIME / $CONCURRENT_USERS" | bc -l)
        
        (
            sleep "$delay"
            run_user_test "$user" "$REQUESTS_PER_USER" "$test_start_time"
        ) &
        
        pids+=($!)
        
        # 显示进度
        if [ $((user % 10)) -eq 0 ] || [ $user -eq $CONCURRENT_USERS ]; then
            log_info "已启动 $user/$CONCURRENT_USERS 个用户"
        fi
    done
    
    # 等待所有用户完成或超时
    log_info "等待测试完成..."
    local timeout_time=$((test_start_time + TEST_DURATION + RAMP_UP_TIME + 30))
    
    for pid in "${pids[@]}"; do
        local current_time=$(date +%s)
        if [ $current_time -lt $timeout_time ]; then
            local remaining_time=$((timeout_time - current_time))
            if timeout $remaining_time wait $pid 2>/dev/null; then
                # 收集用户结果
                wait $pid
            else
                log_warning "用户进程 $pid 超时，强制终止"
                kill $pid 2>/dev/null || true
            fi
        else
            log_warning "整体超时，终止剩余进程"
            kill $pid 2>/dev/null || true
        fi
    done
    
    local test_end_time=$(date +%s)
    local actual_duration=$((test_end_time - test_start_time))
    
    log_success "负载测试完成，实际用时: $actual_duration 秒"
}

# 运行查询性能测试
run_query_test() {
    log_header "查询性能测试"
    
    local query_tests=(
        "SELECT COUNT(*) FROM load_test_table"
        "SELECT * FROM load_test_table LIMIT 10"
        "SELECT user_id, COUNT(*) FROM load_test_table GROUP BY user_id LIMIT 5"
        "SELECT * FROM load_test_table WHERE user_id = 1"
    )
    
    for query in "${query_tests[@]}"; do
        log_info "测试查询: $query"
        
        local start_time=$(date +%s.%N)
        local payload="{\"sql\":\"$query\"}"
        
        local response=$(curl -sf -X POST "$MINIODB_REST_API/v1/query" \
            -H "Content-Type: application/json" \
            -d "$payload" 2>/dev/null)
        
        local end_time=$(date +%s.%N)
        local query_time=$(echo "$end_time - $start_time" | bc -l)
        
        if [ $? -eq 0 ]; then
            local rows=$(echo "$response" | jq -r '.rows_affected // 0' 2>/dev/null || echo "0")
            printf "  查询时间: %.3f秒, 影响行数: %s\n" "$query_time" "$rows"
        else
            printf "  查询失败，耗时: %.3f秒\n" "$query_time"
        fi
    done
}

# 生成测试报告
generate_report() {
    log_header "生成测试报告"
    
    local report_file="$RESULTS_DIR/load_test_report_$TIMESTAMP.html"
    
    # 获取系统监控数据
    local system_stats=""
    if curl -sf "$MINIODB_REST_API/v1/metrics" >/dev/null 2>&1; then
        system_stats=$(curl -sf "$MINIODB_REST_API/v1/metrics" 2>/dev/null)
    fi
    
    # 生成HTML报告
    cat > "$report_file" << EOF
<!DOCTYPE html>
<html>
<head>
    <title>MinIODB 负载测试报告 - $TIMESTAMP</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background: #f0f0f0; padding: 20px; border-radius: 5px; }
        .section { margin: 20px 0; padding: 15px; border: 1px solid #ddd; border-radius: 5px; }
        .metric { margin: 10px 0; }
        .pass { color: green; }
        .fail { color: red; }
        .warning { color: orange; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
    </style>
</head>
<body>
    <div class="header">
        <h1>MinIODB 负载测试报告</h1>
        <p>测试时间: $(date)</p>
        <p>测试配置: $CONCURRENT_USERS 并发用户, 每用户 $REQUESTS_PER_USER 请求</p>
    </div>
    
    <div class="section">
        <h2>测试摘要</h2>
        <div class="metric">总请求数: $((CONCURRENT_USERS * REQUESTS_PER_USER))</div>
        <div class="metric">并发用户: $CONCURRENT_USERS</div>
        <div class="metric">测试持续时间: $TEST_DURATION 秒</div>
        <div class="metric">API端点: $MINIODB_REST_API</div>
    </div>
    
    <div class="section">
        <h2>系统监控</h2>
        <pre>$system_stats</pre>
    </div>
    
    <div class="section">
        <h2>建议</h2>
        <ul>
            <li>监控系统资源使用情况，确保没有达到瓶颈</li>
            <li>检查错误率，如果超过5%需要进一步调查</li>
            <li>关注平均响应时间，超过1秒可能需要优化</li>
            <li>定期运行负载测试以监控性能趋势</li>
        </ul>
    </div>
</body>
</html>
EOF
    
    log_success "测试报告已生成: $report_file"
}

# 清理测试数据
cleanup_test_data() {
    log_info "清理测试数据..."
    
    # 删除测试表
    if curl -sf -X DELETE "$MINIODB_REST_API/v1/tables/load_test_table?if_exists=true" >/dev/null 2>&1; then
        log_success "测试表删除成功"
    else
        log_warning "测试表删除失败"
    fi
}

# 主函数
main() {
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            --users)
                CONCURRENT_USERS="$2"
                shift 2
                ;;
            --requests)
                REQUESTS_PER_USER="$2"
                shift 2
                ;;
            --duration)
                TEST_DURATION="$2"
                shift 2
                ;;
            --rampup)
                RAMP_UP_TIME="$2"
                shift 2
                ;;
            --endpoint)
                MINIODB_REST_API="$2"
                shift 2
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                log_error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 验证参数
    if [ "$CONCURRENT_USERS" -le 0 ] || [ "$REQUESTS_PER_USER" -le 0 ]; then
        log_error "用户数和请求数必须大于0"
        exit 1
    fi
    
    log_header "MinIODB 负载测试"
    
    # 执行测试流程
    check_dependencies
    
    if ! check_service_health; then
        log_error "服务健康检查失败，请确保MinIODB正在运行"
        exit 1
    fi
    
    prepare_test_environment
    run_load_test
    run_query_test
    generate_report
    
    # 询问是否清理测试数据
    echo ""
    read -p "是否清理测试数据? (y/N): " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        cleanup_test_data
    fi
    
    log_success "负载测试完成！"
}

# 信号处理
trap 'log_warning "测试被中断"; exit 130' INT TERM

# 执行主函数
main "$@" 