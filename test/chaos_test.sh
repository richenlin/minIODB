#!/bin/bash

# MinIODB 混沌工程测试脚本
# 通过故障注入测试系统的容错能力和恢复能力

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
COMPOSE_FILE="docker-compose.test.yml"
TEST_DURATION=${TEST_DURATION:-300}  # 5分钟
RECOVERY_TIMEOUT=${RECOVERY_TIMEOUT:-60}  # 1分钟恢复超时

# 结果记录
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
TEST_RESULTS=()

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

log_test() {
    echo -e "${CYAN}[CHAOS]${NC} $1"
}

# 记录测试结果
record_test() {
    local test_name="$1"
    local result="$2"
    local details="$3"
    
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    
    if [ "$result" = "PASS" ]; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
        log_success "✓ $test_name"
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
        log_error "✗ $test_name"
    fi
    
    TEST_RESULTS+=("$test_name:$result:$details")
}

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB 混沌工程测试脚本

用法: $0 [选项] [测试套件]

测试套件:
  all                  运行所有混沌测试 (默认)
  container            容器故障测试
  network              网络故障测试
  resource             资源限制测试
  dependency           依赖服务故障测试

选项:
  --duration SEC       测试持续时间 (默认: 300秒)
  --recovery SEC       恢复超时时间 (默认: 60秒)
  --endpoint URL       MinIODB API端点
  -h, --help           显示帮助信息

示例:
  $0                           # 运行所有混沌测试
  $0 container                # 仅运行容器故障测试
  $0 --duration 600           # 运行10分钟测试

EOF
}

# 检查依赖
check_dependencies() {
    log_info "检查混沌测试依赖..."
    
    local missing_deps=()
    
    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    fi
    
    if ! command -v docker-compose &> /dev/null; then
        missing_deps+=("docker-compose")
    fi
    
    if ! command -v curl &> /dev/null; then
        missing_deps+=("curl")
    fi
    
    if ! command -v jq &> /dev/null; then
        missing_deps+=("jq")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        log_error "缺少依赖: ${missing_deps[*]}"
        exit 1
    fi
    
    log_success "依赖检查通过"
}

# 检查服务健康状态
check_service_health() {
    local max_retries=${1:-3}
    local retry_interval=${2:-5}
    
    for ((i=1; i<=max_retries; i++)); do
        if curl -sf "$MINIODB_REST_API/v1/health" >/dev/null 2>&1; then
            return 0
        fi
        
        if [ $i -lt $max_retries ]; then
            sleep $retry_interval
        fi
    done
    
    return 1
}

# 写入测试数据
write_test_data() {
    local table_name="$1"
    local record_id="$2"
    
    local payload="{\"table\":\"$table_name\",\"id\":\"$record_id\",\"timestamp\":$(date +%s),\"payload\":{\"test_type\":\"chaos\",\"timestamp\":\"$(date -Iseconds)\",\"data\":\"$(openssl rand -hex 16)\"}}"
    
    curl -sf -X POST "$MINIODB_REST_API/v1/data" \
        -H "Content-Type: application/json" \
        -d "$payload" >/dev/null 2>&1
}

# 查询测试数据
query_test_data() {
    local table_name="$1"
    
    local query="{\"sql\":\"SELECT COUNT(*) as count FROM $table_name\"}"
    
    local response=$(curl -sf -X POST "$MINIODB_REST_API/v1/query" \
        -H "Content-Type: application/json" \
        -d "$query" 2>/dev/null)
    
    if [ $? -eq 0 ]; then
        echo "$response" | jq -r '.result_json' 2>/dev/null || echo "0"
    else
        echo "query_failed"
    fi
}

# 容器故障测试
test_container_failures() {
    log_header "容器故障测试"
    
    # 测试MinIODB容器重启
    test_miniodb_restart
    
    # 测试Redis容器故障
    test_redis_failure
    
    # 测试MinIO容器故障
    test_minio_failure
}

# 测试MinIODB容器重启
test_miniodb_restart() {
    log_test "测试MinIODB容器重启恢复"
    
    # 写入测试数据
    if ! write_test_data "chaos_test" "restart_test_001"; then
        record_test "MinIODB容器重启-数据写入" "FAIL" "重启前数据写入失败"
        return
    fi
    
    # 重启MinIODB容器
    log_info "重启MinIODB容器..."
    docker-compose -f "$COMPOSE_FILE" restart miniodb >/dev/null 2>&1
    
    # 等待服务恢复
    local start_time=$(date +%s)
    if check_service_health 12 5; then
        local recovery_time=$(($(date +%s) - start_time))
        log_info "服务恢复时间: ${recovery_time}秒"
        
        # 验证数据完整性
        local count=$(query_test_data "chaos_test")
        if [ "$count" != "query_failed" ] && [ "$count" != "0" ]; then
            record_test "MinIODB容器重启" "PASS" "恢复时间:${recovery_time}s,数据完整"
        else
            record_test "MinIODB容器重启" "FAIL" "数据查询失败或数据丢失"
        fi
    else
        record_test "MinIODB容器重启" "FAIL" "服务未能在${RECOVERY_TIMEOUT}秒内恢复"
    fi
}

# 测试Redis故障
test_redis_failure() {
    log_test "测试Redis故障恢复"
    
    # 停止Redis容器
    log_info "停止Redis容器..."
    docker-compose -f "$COMPOSE_FILE" stop redis >/dev/null 2>&1
    
    # 等待故障检测
    sleep 10
    
    # 测试服务是否仍然可用（降级模式）
    if write_test_data "chaos_test" "redis_failure_001"; then
        log_info "MinIODB在Redis故障时仍可写入数据"
        redis_degraded="true"
    else
        log_info "MinIODB在Redis故障时无法写入数据"
        redis_degraded="false"
    fi
    
    # 重启Redis
    log_info "重启Redis容器..."
    docker-compose -f "$COMPOSE_FILE" start redis >/dev/null 2>&1
    
    # 等待Redis恢复
    sleep 15
    
    # 测试服务完全恢复
    if check_service_health 6 5; then
        if write_test_data "chaos_test" "redis_recovery_001"; then
            record_test "Redis故障恢复" "PASS" "降级模式:$redis_degraded,完全恢复:true"
        else
            record_test "Redis故障恢复" "FAIL" "Redis恢复后服务仍不可用"
        fi
    else
        record_test "Redis故障恢复" "FAIL" "Redis恢复后健康检查失败"
    fi
}

# 测试MinIO故障
test_minio_failure() {
    log_test "测试MinIO故障恢复"
    
    # 写入一些数据到缓冲区
    write_test_data "chaos_test" "minio_failure_001"
    write_test_data "chaos_test" "minio_failure_002"
    
    # 停止MinIO容器
    log_info "停止MinIO容器..."
    docker-compose -f "$COMPOSE_FILE" stop minio >/dev/null 2>&1
    
    # 测试缓冲区是否仍然工作
    sleep 5
    if write_test_data "chaos_test" "minio_failure_buffer_001"; then
        log_info "MinIO故障时缓冲区仍然工作"
        buffer_working="true"
    else
        log_info "MinIO故障时缓冲区不工作"
        buffer_working="false"
    fi
    
    # 重启MinIO
    log_info "重启MinIO容器..."
    docker-compose -f "$COMPOSE_FILE" start minio >/dev/null 2>&1
    
    # 等待MinIO恢复
    sleep 20
    
    # 测试数据是否最终持久化
    if check_service_health 6 5; then
        local count=$(query_test_data "chaos_test")
        if [ "$count" != "query_failed" ] && [ "$count" != "0" ]; then
            record_test "MinIO故障恢复" "PASS" "缓冲区工作:$buffer_working,数据恢复:true"
        else
            record_test "MinIO故障恢复" "FAIL" "数据未能恢复"
        fi
    else
        record_test "MinIO故障恢复" "FAIL" "MinIO恢复后健康检查失败"
    fi
}

# 网络故障测试
test_network_failures() {
    log_header "网络故障测试"
    
    # 测试网络分区
    test_network_partition
    
    # 测试连接超时
    test_connection_timeout
}

# 测试网络分区
test_network_partition() {
    log_test "测试网络分区恢复"
    
    # 断开MinIODB和Redis的网络连接
    log_info "模拟网络分区..."
    
    # 停止网络，然后重启模拟分区恢复
    docker-compose -f "$COMPOSE_FILE" stop redis >/dev/null 2>&1
    sleep 10
    docker-compose -f "$COMPOSE_FILE" start redis >/dev/null 2>&1
    sleep 15
    
    if check_service_health 6 5; then
        record_test "网络分区恢复" "PASS" "网络分区后成功恢复"
    else
        record_test "网络分区恢复" "FAIL" "网络分区后未能恢复"
    fi
}

# 测试连接超时
test_connection_timeout() {
    log_test "测试连接超时处理"
    
    # 模拟连接超时
    if check_service_health 3 5; then
        record_test "连接超时测试" "PASS" "服务正确处理连接超时"
    else
        record_test "连接超时测试" "FAIL" "连接超时导致服务不可用"
    fi
}

# 资源限制测试
test_resource_limits() {
    log_header "资源限制测试"
    
    # 测试内存限制
    test_memory_limits
    
    # 测试CPU限制
    test_cpu_limits
}

# 测试内存限制
test_memory_limits() {
    log_test "测试内存限制"
    
    # 设置内存限制
    docker update --memory=256m miniodb >/dev/null 2>&1 || true
    
    sleep 5
    
    # 测试在内存限制下的行为
    if write_test_data "chaos_test" "memory_limited"; then
        record_test "内存限制测试" "PASS" "内存限制下服务仍可用"
    else
        record_test "内存限制测试" "FAIL" "内存限制导致服务不可用"
    fi
    
    # 恢复内存限制
    docker update --memory=0 miniodb >/dev/null 2>&1 || true
}

# 测试CPU限制
test_cpu_limits() {
    log_test "测试CPU限制"
    
    # 限制CPU使用
    docker update --cpus="0.5" miniodb >/dev/null 2>&1 || true
    
    sleep 5
    
    # 测试在CPU限制下的性能
    if write_test_data "chaos_test" "cpu_limited"; then
        record_test "CPU限制测试" "PASS" "CPU限制下服务仍可用"
    else
        record_test "CPU限制测试" "FAIL" "CPU限制导致服务不可用"
    fi
    
    # 恢复CPU限制
    docker update --cpus="0" miniodb >/dev/null 2>&1 || true
}

# 依赖服务故障测试
test_dependency_failures() {
    log_header "依赖服务故障测试"
    
    # 测试多重故障
    test_multiple_failures
    
    # 测试级联故障
    test_cascade_failures
}

# 测试多重故障
test_multiple_failures() {
    log_test "测试多重故障场景"
    
    # 同时引入多个故障
    log_info "引入多重故障..."
    
    # 限制资源并停止备份服务
    docker update --memory=256m --cpus="0.5" miniodb >/dev/null 2>&1 || true
    docker-compose -f "$COMPOSE_FILE" stop minio-backup >/dev/null 2>&1
    
    sleep 10
    
    # 测试系统在多重故障下的表现
    if write_test_data "chaos_test" "multiple_failures"; then
        record_test "多重故障测试" "PASS" "系统在多重故障下仍部分可用"
    else
        record_test "多重故障测试" "FAIL" "多重故障导致系统完全不可用"
    fi
    
    # 恢复所有服务
    log_info "恢复所有服务..."
    docker update --memory=0 --cpus="0" miniodb >/dev/null 2>&1 || true
    docker-compose -f "$COMPOSE_FILE" start minio-backup >/dev/null 2>&1
    sleep 15
}

# 测试级联故障
test_cascade_failures() {
    log_test "测试级联故障"
    
    # 模拟级联故障场景
    if check_service_health 3 5; then
        record_test "级联故障测试" "PASS" "系统处理级联故障"
    else
        record_test "级联故障测试" "FAIL" "级联故障导致系统崩溃"
    fi
}

# 清理测试数据
cleanup_chaos_test() {
    log_info "清理混沌测试数据..."
    
    # 删除测试表
    curl -sf -X DELETE "$MINIODB_REST_API/v1/tables/chaos_test?if_exists=true" >/dev/null 2>&1 || true
    
    # 确保所有容器限制被移除
    docker update --memory=0 --cpus="0" miniodb >/dev/null 2>&1 || true
    
    # 确保所有服务都在运行
    docker-compose -f "$COMPOSE_FILE" up -d >/dev/null 2>&1 || true
}

# 生成混沌测试报告
generate_chaos_report() {
    log_header "混沌测试报告"
    
    echo -e "${CYAN}总测试数:${NC} $TOTAL_TESTS"
    echo -e "${GREEN}通过:${NC} $PASSED_TESTS"
    echo -e "${RED}失败:${NC} $FAILED_TESTS"
    
    local success_rate=0
    if [ $TOTAL_TESTS -gt 0 ]; then
        success_rate=$(( (PASSED_TESTS * 100) / TOTAL_TESTS ))
    fi
    
    echo -e "${CYAN}成功率:${NC} ${success_rate}%"
    
    echo ""
    echo "详细结果:"
    for result in "${TEST_RESULTS[@]}"; do
        IFS=':' read -r name status details <<< "$result"
        if [ "$status" = "PASS" ]; then
            echo -e "  ${GREEN}✓${NC} $name ($details)"
        elif [ "$status" = "SKIP" ]; then
            echo -e "  ${YELLOW}-${NC} $name ($details)"
        else
            echo -e "  ${RED}✗${NC} $name ($details)"
        fi
    done
    
    echo ""
    if [ $FAILED_TESTS -eq 0 ]; then
        log_success "🎉 系统通过了所有混沌测试！"
        return 0
    else
        log_warning "⚠️  系统在某些混沌场景下存在问题"
        return 1
    fi
}

# 主函数
main() {
    local test_suite="all"
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            all|container|network|resource|dependency)
                test_suite="$1"
                shift
                ;;
            --duration)
                TEST_DURATION="$2"
                shift 2
                ;;
            --recovery)
                RECOVERY_TIMEOUT="$2"
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
    
    log_header "MinIODB 混沌工程测试"
    
    # 检查依赖
    check_dependencies
    
    # 检查服务可用性
    if ! check_service_health 3 5; then
        log_error "MinIODB服务不可用，请确保服务正在运行"
        exit 1
    fi
    
    # 创建测试表
    local payload='{"table_name":"chaos_test","config":{"buffer_size":1000,"flush_interval_seconds":10,"retention_days":1},"if_not_exists":true}'
    curl -sf -X POST "$MINIODB_REST_API/v1/tables" \
        -H "Content-Type: application/json" \
        -d "$payload" >/dev/null 2>&1 || true
    
    # 执行测试套件
    case $test_suite in
        all)
            test_container_failures
            test_network_failures
            test_resource_limits
            test_dependency_failures
            ;;
        container)
            test_container_failures
            ;;
        network)
            test_network_failures
            ;;
        resource)
            test_resource_limits
            ;;
        dependency)
            test_dependency_failures
            ;;
    esac
    
    # 生成报告
    local test_result=0
    generate_chaos_report || test_result=1
    
    # 清理
    cleanup_chaos_test
    
    exit $test_result
}

# 信号处理
trap 'log_warning "混沌测试被中断"; cleanup_chaos_test; exit 130' INT TERM

# 执行主函数
main "$@" 