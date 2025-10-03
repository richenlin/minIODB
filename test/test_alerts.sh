#!/bin/bash
# test_alerts.sh - MinIODB告警压测验证脚本
# 用途：验证连接池耗尽和尾部时延告警能够正确触发和恢复

set -e

# 配置
MINIODB_URL="${MINIODB_URL:-http://localhost:8081}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
METRICS_URL="${MINIODB_URL}/metrics"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_command() {
    if ! command -v $1 &> /dev/null; then
        log_error "$1 未安装，请先安装: $2"
        exit 1
    fi
}

echo "========================================"
echo "   MinIODB 告警压测验证"
echo "========================================"
echo ""

# 检查依赖
log_info "检查依赖工具..."
check_command "curl" "apt-get install curl"
check_command "jq" "apt-get install jq"

# 检查hey工具（可选）
if ! command -v hey &> /dev/null; then
    log_warn "hey工具未安装，将使用curl进行压测（性能较低）"
    log_warn "推荐安装: go install github.com/rakyll/hey@latest"
    USE_HEY=false
else
    USE_HEY=true
fi

# 1. 检查服务状态
echo ""
log_info "[1/6] 检查服务状态..."
if ! curl -sf ${MINIODB_URL}/v1/health > /dev/null 2>&1; then
    log_error "MinIODB未运行或无法访问: ${MINIODB_URL}"
    exit 1
fi
log_info "  ✓ MinIODB服务正常"

if ! curl -sf ${PROMETHEUS_URL}/-/healthy > /dev/null 2>&1; then
    log_warn "  ⚠ Prometheus未运行或无法访问: ${PROMETHEUS_URL}"
    log_warn "  将跳过告警验证部分"
    SKIP_ALERT_CHECK=true
else
    log_info "  ✓ Prometheus服务正常"
    SKIP_ALERT_CHECK=false
fi

# 2. 获取基线指标
echo ""
log_info "[2/6] 获取基线指标..."

BASELINE_REDIS_UTIL=$(curl -s ${METRICS_URL} | grep 'miniodb_redis_pool_utilization_percent' | grep -oP '\d+\.\d+' | head -1 || echo "0")
BASELINE_MINIO_UTIL=$(curl -s ${METRICS_URL} | grep 'miniodb_minio_pool_utilization_percent' | grep -oP '\d+\.\d+' | head -1 || echo "0")

log_info "  Redis连接池利用率: ${BASELINE_REDIS_UTIL}%"
log_info "  MinIO连接池利用率: ${BASELINE_MINIO_UTIL}%"

# 3. 启动压测
echo ""
log_info "[3/6] 启动压测..."

# 准备压测请求
QUERY_REQUEST='{"sql":"SELECT * FROM test_table LIMIT 1000"}'

if [ "$USE_HEY" = true ]; then
    log_info "  使用 hey 工具进行高并发压测..."
    hey -z 3m -c 50 -m POST \
        -H "Content-Type: application/json" \
        -d "${QUERY_REQUEST}" \
        ${MINIODB_URL}/v1/query > /tmp/hey_output.txt 2>&1 &
    LOAD_PID=$!
    log_info "  压测进程PID: $LOAD_PID (持续3分钟)"
else
    log_info "  使用 curl 进行并发压测（性能有限）..."
    for i in {1..100}; do
        (
            for j in {1..50}; do
                curl -s -X POST ${MINIODB_URL}/v1/query \
                    -H "Content-Type: application/json" \
                    -d "${QUERY_REQUEST}" > /dev/null 2>&1 &
                sleep 0.1
            done
            wait
        ) &
    done
    LOAD_PID=$!
    log_info "  压测进程PID: $LOAD_PID"
fi

# 4. 监控指标变化
echo ""
log_info "[4/6] 监控指标变化（3分钟）..."

for i in {1..18}; do
    sleep 10
    
    # 获取当前指标
    CURRENT_REDIS_UTIL=$(curl -s ${METRICS_URL} | grep 'miniodb_redis_pool_utilization_percent' | grep -oP '\d+\.\d+' | head -1 || echo "0")
    CURRENT_MINIO_UTIL=$(curl -s ${METRICS_URL} | grep 'miniodb_minio_pool_utilization_percent' | grep -oP '\d+\.\d+' | head -1 || echo "0")
    
    echo -n "  [$i/18] Redis: ${CURRENT_REDIS_UTIL}%, MinIO: ${CURRENT_MINIO_UTIL}%"
    
    # 检查是否触发阈值
    if (( $(echo "$CURRENT_REDIS_UTIL > 90" | bc -l) )) || (( $(echo "$CURRENT_MINIO_UTIL > 90" | bc -l) )); then
        echo -e " ${RED}[高利用率!]${NC}"
    else
        echo ""
    fi
done

# 5. 检查告警状态
echo ""
if [ "$SKIP_ALERT_CHECK" = false ]; then
    log_info "[5/6] 检查Prometheus告警状态..."
    
    ALERT_COUNT=$(curl -s ${PROMETHEUS_URL}/api/v1/alerts 2>/dev/null | \
        jq -r '.data.alerts[] | select(.labels.alertname | contains("Pool") or contains("Latency")) | .labels.alertname' 2>/dev/null | wc -l || echo "0")
    
    if [ "$ALERT_COUNT" -gt 0 ]; then
        log_info "  ✓ 检测到 $ALERT_COUNT 个告警"
        curl -s ${PROMETHEUS_URL}/api/v1/alerts | \
            jq -r '.data.alerts[] | select(.labels.alertname | contains("Pool") or contains("Latency")) | "  - \(.labels.alertname): \(.state) (\(.labels.severity))"' 2>/dev/null || true
    else
        log_warn "  ⚠ 未检测到告警（可能需要更长时间或更高负载）"
    fi
else
    log_info "[5/6] 跳过告警检查（Prometheus不可用）"
fi

# 6. 停止压测并验证恢复
echo ""
log_info "[6/6] 停止压测，监控恢复..."

# 停止压测进程
if [ ! -z "$LOAD_PID" ]; then
    kill $LOAD_PID 2>/dev/null || true
    wait $LOAD_PID 2>/dev/null || true
fi

# 杀掉所有后台curl进程
pkill -f "curl.*${MINIODB_URL}" 2>/dev/null || true

if [ "$USE_HEY" = true ] && [ -f /tmp/hey_output.txt ]; then
    log_info "  压测统计:"
    grep -E "Requests/sec|Total:" /tmp/hey_output.txt | sed 's/^/    /' || true
    rm -f /tmp/hey_output.txt
fi

log_info "  等待系统恢复（监控2分钟）..."

RECOVERED=false
for i in {1..12}; do
    sleep 10
    
    CURRENT_REDIS_UTIL=$(curl -s ${METRICS_URL} | grep 'miniodb_redis_pool_utilization_percent' | grep -oP '\d+\.\d+' | head -1 || echo "0")
    CURRENT_MINIO_UTIL=$(curl -s ${METRICS_URL} | grep 'miniodb_minio_pool_utilization_percent' | grep -oP '\d+\.\d+' | head -1 || echo "0")
    
    echo "  [$i/12] Redis: ${CURRENT_REDIS_UTIL}%, MinIO: ${CURRENT_MINIO_UTIL}%"
    
    # 检查是否恢复到70%以下
    if (( $(echo "$CURRENT_REDIS_UTIL < 70" | bc -l) )) && (( $(echo "$CURRENT_MINIO_UTIL < 70" | bc -l) )); then
        RECOVERED=true
        break
    fi
done

if [ "$RECOVERED" = true ]; then
    log_info "  ✓ 连接池利用率已恢复正常"
else
    log_warn "  ⚠ 连接池利用率仍然较高，可能需要更长恢复时间"
fi

# 检查恢复告警
if [ "$SKIP_ALERT_CHECK" = false ]; then
    sleep 30
    RECOVERY_ALERTS=$(curl -s ${PROMETHEUS_URL}/api/v1/alerts 2>/dev/null | \
        jq -r '.data.alerts[] | select(.labels.alertname | contains("Recovered")) | .labels.alertname' 2>/dev/null | wc -l || echo "0")
    
    if [ "$RECOVERY_ALERTS" -gt 0 ]; then
        log_info "  ✓ 检测到恢复通知"
        curl -s ${PROMETHEUS_URL}/api/v1/alerts | \
            jq -r '.data.alerts[] | select(.labels.alertname | contains("Recovered")) | "  - \(.labels.alertname): \(.annotations.summary)"' 2>/dev/null || true
    else
        log_info "  ℹ 未检测到恢复通知（可能需要更长时间）"
    fi
fi

# 总结
echo ""
echo "========================================"
echo "   压测验证完成"
echo "========================================"
echo ""
log_info "测试总结:"
echo "  基线: Redis ${BASELINE_REDIS_UTIL}%, MinIO ${BASELINE_MINIO_UTIL}%"
echo "  峰值: Redis ${CURRENT_REDIS_UTIL}%, MinIO ${CURRENT_MINIO_UTIL}%"
echo "  恢复: $([ "$RECOVERED" = true ] && echo "是" || echo "否")"
echo ""

if [ "$SKIP_ALERT_CHECK" = false ]; then
    log_info "查看详细告警: ${PROMETHEUS_URL}/alerts"
fi
log_info "查看详细指标: ${METRICS_URL}"
echo ""

