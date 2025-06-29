#!/bin/bash

# MinIODB 完整测试套件运行脚本
# 依次运行集成测试、负载测试和混沌测试

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

# 测试结果
TOTAL_SUITES=0
PASSED_SUITES=0
FAILED_SUITES=0
SUITE_RESULTS=()

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

log_suite() {
    echo -e "${CYAN}[SUITE]${NC} $1"
}

# 记录测试套件结果
record_suite() {
    local suite_name="$1"
    local result="$2"
    local details="$3"
    
    TOTAL_SUITES=$((TOTAL_SUITES + 1))
    
    if [ "$result" = "PASS" ]; then
        PASSED_SUITES=$((PASSED_SUITES + 1))
        log_success "✓ $suite_name 套件通过"
    else
        FAILED_SUITES=$((FAILED_SUITES + 1))
        log_error "✗ $suite_name 套件失败"
    fi
    
    SUITE_RESULTS+=("$suite_name:$result:$details")
}

# 显示帮助信息
show_help() {
    cat << EOF
MinIODB 完整测试套件运行脚本

用法: $0 [选项]

选项:
  --integration-only   仅运行集成测试
  --load-only          仅运行负载测试
  --chaos-only         仅运行混沌测试
  --skip-integration   跳过集成测试
  --skip-load          跳过负载测试
  --skip-chaos         跳过混沌测试
  --continue-on-error  测试失败时继续运行
  --quick              快速测试模式
  --no-cleanup         不清理测试环境
  -h, --help           显示帮助信息

示例:
  $0                          # 运行所有测试套件
  $0 --integration-only       # 仅运行集成测试
  $0 --skip-chaos             # 跳过混沌测试
  $0 --continue-on-error      # 出错时继续

EOF
}

# 检查环境
check_environment() {
    log_info "检查测试环境..."
    
    # 检查必要文件
    local required_files=(
        "integration_test.sh"
        "load_test.sh"
        "chaos_test.sh"
        "docker-compose.test.yml"
    )
    
    for file in "${required_files[@]}"; do
        if [ ! -f "$SCRIPT_DIR/$file" ]; then
            log_error "缺少必要文件: $file"
            exit 1
        fi
    done
    
    # 检查权限
    for file in "${required_files[@]%.yml}"; do
        if [ ! -x "$SCRIPT_DIR/$file" ]; then
            chmod +x "$SCRIPT_DIR/$file"
        fi
    done
    
    log_success "环境检查通过"
}

# 运行集成测试
run_integration_tests() {
    log_suite "运行集成测试套件"
    
    cd "$SCRIPT_DIR"
    
    local start_time=$(date +%s)
    
    if ./integration_test.sh; then
        local duration=$(($(date +%s) - start_time))
        record_suite "集成测试" "PASS" "耗时: ${duration}秒"
        return 0
    else
        local duration=$(($(date +%s) - start_time))
        record_suite "集成测试" "FAIL" "耗时: ${duration}秒"
        return 1
    fi
}

# 运行负载测试
run_load_tests() {
    log_suite "运行负载测试套件"
    
    cd "$SCRIPT_DIR"
    
    local start_time=$(date +%s)
    
    # 使用较小的负载进行快速测试
    if ./load_test.sh --users 5 --requests 20 --duration 60; then
        local duration=$(($(date +%s) - start_time))
        record_suite "负载测试" "PASS" "耗时: ${duration}秒"
        return 0
    else
        local duration=$(($(date +%s) - start_time))
        record_suite "负载测试" "FAIL" "耗时: ${duration}秒"
        return 1
    fi
}

# 运行混沌测试
run_chaos_tests() {
    log_suite "运行混沌测试套件"
    
    cd "$SCRIPT_DIR"
    
    local start_time=$(date +%s)
    
    if ./chaos_test.sh --duration 120; then
        local duration=$(($(date +%s) - start_time))
        record_suite "混沌测试" "PASS" "耗时: ${duration}秒"
        return 0
    else
        local duration=$(($(date +%s) - start_time))
        record_suite "混沌测试" "FAIL" "耗时: ${duration}秒"
        return 1
    fi
}

# 等待服务恢复
wait_for_service_recovery() {
    log_info "等待服务恢复..."
    
    local max_wait=60
    local wait_time=0
    
    while [ $wait_time -lt $max_wait ]; do
        if curl -sf "http://localhost:8081/v1/health" >/dev/null 2>&1; then
            log_success "服务已恢复"
            return 0
        fi
        
        sleep 5
        wait_time=$((wait_time + 5))
    done
    
    log_warning "服务恢复超时"
    return 1
}

# 生成最终测试报告
generate_final_report() {
    log_header "完整测试套件报告"
    
    echo -e "${CYAN}测试套件统计:${NC}"
    echo -e "  总套件数: $TOTAL_SUITES"
    echo -e "  通过: ${GREEN}$PASSED_SUITES${NC}"
    echo -e "  失败: ${RED}$FAILED_SUITES${NC}"
    
    local success_rate=0
    if [ $TOTAL_SUITES -gt 0 ]; then
        success_rate=$(( (PASSED_SUITES * 100) / TOTAL_SUITES ))
    fi
    echo -e "  成功率: ${CYAN}${success_rate}%${NC}"
    
    echo ""
    echo -e "${CYAN}套件详情:${NC}"
    for result in "${SUITE_RESULTS[@]}"; do
        IFS=':' read -r name status details <<< "$result"
        if [ "$status" = "PASS" ]; then
            echo -e "  ${GREEN}✓${NC} $name ($details)"
        else
            echo -e "  ${RED}✗${NC} $name ($details)"
        fi
    done
    
    echo ""
    echo -e "${CYAN}测试建议:${NC}"
    echo "- 定期运行完整测试套件以确保系统稳定性"
    echo "- 在生产部署前务必通过所有测试"
    echo "- 关注失败的测试用例，及时修复问题"
    echo "- 根据业务需求调整负载测试参数"
    
    if [ $FAILED_SUITES -eq 0 ]; then
        echo ""
        log_success "🎉 恭喜！所有测试套件都通过了！"
        echo "系统已准备好用于生产环境"
        return 0
    else
        echo ""
        log_error "❌ 有测试套件失败，请检查并修复问题"
        return 1
    fi
}

# 清理所有测试环境
cleanup_all() {
    log_info "清理所有测试环境..."
    
    cd "$SCRIPT_DIR"
    
    # 停止所有测试容器
    docker-compose -f docker-compose.test.yml down -v --remove-orphans >/dev/null 2>&1 || true
    
    # 清理Docker资源
    docker system prune -f >/dev/null 2>&1 || true
    
    log_success "环境清理完成"
}

# 主函数
main() {
    local run_integration=true
    local run_load=true
    local run_chaos=true
    local continue_on_error=false
    local quick_mode=false
    local no_cleanup=false
    
    # 解析命令行参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            --integration-only)
                run_integration=true
                run_load=false
                run_chaos=false
                shift
                ;;
            --load-only)
                run_integration=false
                run_load=true
                run_chaos=false
                shift
                ;;
            --chaos-only)
                run_integration=false
                run_load=false
                run_chaos=true
                shift
                ;;
            --skip-integration)
                run_integration=false
                shift
                ;;
            --skip-load)
                run_load=false
                shift
                ;;
            --skip-chaos)
                run_chaos=false
                shift
                ;;
            --continue-on-error)
                continue_on_error=true
                shift
                ;;
            --quick)
                quick_mode=true
                shift
                ;;
            --no-cleanup)
                no_cleanup=true
                shift
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
    
    log_header "MinIODB 完整测试套件"
    
    local total_start_time=$(date +%s)
    
    # 检查环境
    check_environment
    
    # 运行测试套件
    local overall_result=0
    
    if [ "$run_integration" = true ]; then
        if ! run_integration_tests; then
            overall_result=1
            if [ "$continue_on_error" = false ]; then
                log_error "集成测试失败，停止运行"
                generate_final_report
                exit 1
            fi
        fi
        
        # 在测试之间等待服务恢复
        wait_for_service_recovery
    fi
    
    if [ "$run_load" = true ]; then
        if ! run_load_tests; then
            overall_result=1
            if [ "$continue_on_error" = false ]; then
                log_error "负载测试失败，停止运行"
                generate_final_report
                exit 1
            fi
        fi
        
        # 在测试之间等待服务恢复
        wait_for_service_recovery
    fi
    
    if [ "$run_chaos" = true ]; then
        if ! run_chaos_tests; then
            overall_result=1
            if [ "$continue_on_error" = false ]; then
                log_error "混沌测试失败，停止运行"
                generate_final_report
                exit 1
            fi
        fi
    fi
    
    local total_duration=$(($(date +%s) - total_start_time))
    
    echo ""
    log_info "总测试时间: ${total_duration}秒"
    
    # 生成最终报告
    local report_result=0
    generate_final_report || report_result=1
    
    # 清理环境
    if [ "$no_cleanup" = false ]; then
        cleanup_all
    fi
    
    # 返回最终结果
    if [ $overall_result -eq 0 ] && [ $report_result -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

# 信号处理
trap 'log_warning "测试被中断"; cleanup_all; exit 130' INT TERM

# 执行主函数
main "$@" 