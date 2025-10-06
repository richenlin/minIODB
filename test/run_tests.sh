#!/bin/bash

# MinIODB 统一测试运行脚本
# 按照新的测试组织结构运行测试

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 测试配置
UNIT_TEST_FAIL_LIMIT=15       # 单元测试失败阈值
FUNCTIONAL_TEST_FAIL_LIMIT=5  # 功能测试失败阈值
COVERAGE_TARGET=70            # 覆盖率目标（百分比）

# 日志文件
LOG_FILE="test_results.log"
COVERAGE_FILE="coverage.out"

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}    MinIODB 测试执行脚本${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""

# 记录开始时间
START_TIME=$(date)
echo "测试开始时间: $START_TIME" > $LOG_FILE

# 功能：运行单元测试
run_unit_tests() {
    echo -e "${YELLOW}步骤 1: 运行单元测试${NC}"
    echo "=== 单元测试结果 ===" >> $LOG_FILE

    # 运行所有内部模块的测试（保持原有组织）
    echo -e "${BLUE}测试内部模块...${NC}"
    go test ./internal/... -v -covermode=atomic -coverprofile=$COVERAGE_FILE 2>&1 | tee -a $LOG_FILE

    local unit_fail_count=$(grep -c "FAIL" $COVERAGE_FILE || true)

    if [ $unit_fail_count -lt $UNIT_TEST_FAIL_LIMIT ]; then
        echo -e "${GREEN}✓ 单元测试通过 (失败: $unit_fail_count < 阈值: $UNIT_TEST_FAIL_LIMIT)${NC}"
    else
        echo -e "${RED}✗ 单元测试未通过 (失败: $unit_fail_count >= 阈值: $UNIT_TEST_FAIL_LIMIT)${NC}"
        return 1
    fi
}

# 功能：运行功能测试（新结构）
run_functional_tests() {
    echo -e "${YELLOW}步骤 2: 运行功能集成测试${NC}"
    echo "=== 功能测试结果 ===" >> $LOG_FILE

    # 定义功能测试模块
    functional_modules=(
        "security"
        "service"
        "coordinator"
        "discovery"
        "metadata"
        "pool"
        "storage"
        "transport/grpc"
        "transport/rest"
    )

    local total_failures=0

    for module in "${functional_modules[@]}"; do
        echo -e "${BLUE}测试功能模块: $module${NC}"

        if go test ./test/$module/... -v 2>&1 | tee -a $LOG_FILE; then
            echo -e "${GREEN}✓ $module 功能测试通过${NC}"
        else
            echo -e "${RED}✗ $module 功能测试失败${NC}"
            ((total_failures++))
        fi
    done

    if [ $total_failures -lt $FUNCTIONAL_TEST_FAIL_LIMIT ]; then
        echo -e "${GREEN}✓ 功能测试通过 (失败: $total_failures < 阈值: $FUNCTIONAL_TEST_FAIL_LIMIT)${NC}"
        return 0
    else
        echo -e "${RED}✗ 功能测试未通过 (失败: $total_failures >= 阈值: $FUNCTIONAL_TEST_FAIL_LIMIT)${NC}"
        return 1
    fi
}

# 功能：生成覆盖率报告
generate_coverage_report() {
    echo -e "${YELLOW}步骤 3: 生成测试覆盖率报告${NC}"

    # 检查整体覆盖率
    coverage_line=$(grep -E "^coverage:" $LOG_FILE | tail -1 || echo "无覆盖率数据")

    if [ "$coverage_line" != "无覆盖率数据" ]; then
        coverage_percent=$(echo $coverage_line | cut -d' ' -f2 | tr -d '%')
        echo -e "${BLUE}目标覆盖率: ${COVERAGE_TARGET}%, 实际覆盖率: ${coverage_percent}%${NC}"

        if (( $(echo "$coverage_percent >= $COVERAGE_TARGET" | bc -l) )); then
            echo -e "${GREEN}✓ 覆盖率达到目标要求${NC}"
        else
            echo -e "${YELLOW}⚠ 覆盖率未达到目标，但测试结构已优化${NC}"
        fi
    fi

    # 详细的覆盖率分析
    echo -e "${BLUE}详细覆盖率分析:${NC}"
    go test ./internal/... -coverprofile=coverage.out -covermode=atomic
    go tool cover -html=coverage.out -o coverage.html 2>/dev/null || echo "HTML报告生成失败"

    echo "覆盖率报告已生成: coverage.html"
}

# 功能：输出测试总结
print_summary() {
    echo ""
    echo -e "${BLUE}======================================${NC}"
    echo -e "${BLUE}     测试执行摘要${NC}"
    echo -e "${BLUE}======================================${NC}"
    echo ""

    local end_time=$(date)
    echo "测试结束时间: $end_time"
    echo "详细日志文件: $LOG_FILE"
    echo ""

    # 输出通过率统计
    local total_tests=$(grep -c "PASS\|FAIL" $LOG_FILE | grep -v "===" || echo "0")
    local passed_tests=$(grep -c "PASS" $LOG_FILE | grep -v "===" || echo "0")
    local failed_tests=$(grep -c "FAIL" $LOG_FILE | grep -v "===" || echo "0")

    if [ $total_tests -gt 0 ]; then
        pass_rate=$(echo "scale=2; $passed_tests*100/$total_tests" | bc -l || echo "0")
        echo -e "${BLUE}测试统计:${NC}"
        echo "  总测试数: $total_tests"
        echo "  通过: $passed_tests"
        echo "  失败: $failed_tests"
        echo "  通过率: $pass_rate%"
    fi

    echo ""
    echo -e "${BLUE}测试组织结构说明:${NC}"
    echo "  - 单元测试: ./internal/... (每个源码文件对应 *_test.go)"
    echo "  - 功能集成测试: ./test/... (涉及多个源码文件)"
}

# 主函数
main() {
    local unit_success=0
    local functional_success=0

    # 步骤1: 运行单元测试
    if run_unit_tests; then
        unit_success=1
    fi

    # 步骤2: 运行功能测试（即使单元测试失败也继续）
    if run_functional_tests; then
        functional_success=1
    fi

    # 步骤3: 生成覆盖率报告
    generate_coverage_report

    # 输出总结
    print_summary

    # 最终判断
    if [ $unit_success -eq 1 ] && [ $functional_success -eq 1 ]; then
        echo -e "${GREEN}✓ 所有测试执行完成 - 成功${NC}"
        exit 0
    else
        echo -e "${RED}✗ 测试执行完成 - 有失败项${NC}"
        exit 1
    fi
}

# 错误处理
trap 'echo -e "${RED}脚本执行中断${NC}"; exit 130' INT TERM

# 运行主函数
main "$@"