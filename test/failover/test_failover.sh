#!/bin/bash

# MinIO双池故障切换全链路测试脚本

set -e

API_BASE="http://localhost:8081"
METRICS_URL="http://localhost:9090/metrics"

echo "===== MinIO双池故障切换全链路测试 ====="
echo ""

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 测试计数器
PASSED=0
FAILED=0

# 测试函数
test_step() {
    local name="$1"
    echo -e "${YELLOW}[测试] $name${NC}"
}

test_pass() {
    local msg="$1"
    echo -e "${GREEN}✓ PASS${NC}: $msg"
    ((PASSED++))
}

test_fail() {
    local msg="$1"
    echo -e "${RED}✗ FAIL${NC}: $msg"
    ((FAILED++))
}

# ============ 测试1: 健康检查 ============
test_step "测试1: 健康检查API"
HEALTH=$(curl -s ${API_BASE}/v1/health)
if echo "$HEALTH" | grep -q "healthy"; then
    test_pass "健康检查返回正常状态"
else
    test_fail "健康检查失败: $HEALTH"
fi
echo ""

# ============ 测试2: Prometheus指标 ============
test_step "测试2: Prometheus监控指标"
METRICS=$(curl -s ${METRICS_URL})
if echo "$METRICS" | grep -q "miniodb_info"; then
    test_pass "Prometheus指标可访问"
else
    test_fail "Prometheus指标不可访问"
fi

# 检查故障切换指标
if echo "$METRICS" | grep -q "miniodb_pool"; then
    test_pass "故障切换指标已注册"
else
    test_fail "故障切换指标未找到"
fi
echo ""

# ============ 测试3: 创建表 ============
test_step "测试3: 创建表（test_failover_table）"
CREATE_RESULT=$(curl -s -X POST ${API_BASE}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "test_failover_table",
    "config": {
      "buffer_size": 50,
      "flush_interval_seconds": 5,
      "retention_days": 365,
      "backup_enabled": true
    }
  }')

if echo "$CREATE_RESULT" | grep -q "success.*true"; then
    test_pass "表创建成功"
else
    test_fail "表创建失败: $CREATE_RESULT"
fi
echo ""

# ============ 测试4: 写入数据 ============
test_step "测试4: 写入100条测试数据"
WRITE_COUNT=0
for i in {1..100}; do
    RESULT=$(curl -s -X POST ${API_BASE}/v1/data \
      -H "Content-Type: application/json" \
      -d "{
        \"table\": \"test_failover_table\",
        \"id\": \"record-$(printf '%05d' $i)\",
        \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
        \"payload\": {
          \"index\": $i,
          \"value\": $((1000 + $i)),
          \"tag\": \"test-tag-$(($i % 10))\"
        }
      }")
    
    if echo "$RESULT" | grep -q "success\|written"; then
        ((WRITE_COUNT++))
    fi
    
    # 每10条显示进度
    if [ $((i % 10)) -eq 0 ]; then
        echo "  已写入 $i/100 条数据"
    fi
done

if [ $WRITE_COUNT -eq 100 ]; then
    test_pass "100条数据全部写入成功"
else
    test_fail "仅写入 $WRITE_COUNT/100 条数据"
fi
echo ""

# ============ 测试5: 等待数据刷新并查询 ============
test_step "测试5: 等待数据刷新并查询"
echo "  等待10秒让数据刷新到MinIO..."
sleep 10

QUERY_RESULT=$(curl -s -X POST ${API_BASE}/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT COUNT(*) as total FROM test_failover_table"
  }')

echo "  查询结果: $QUERY_RESULT"
if echo "$QUERY_RESULT" | grep -q "\"total\""; then
    test_pass "数据查询成功，已刷新到MinIO"
else
    test_fail "数据查询失败或未刷新: $QUERY_RESULT"
fi
echo ""

# ============ 测试6: 聚合查询 ============
test_step "测试6: 聚合查询测试"
AGG_RESULT=$(curl -s -X POST ${API_BASE}/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT tag, COUNT(*) as count, AVG(value) as avg_value FROM test_failover_table GROUP BY tag ORDER BY count DESC LIMIT 5"
  }')

if echo "$AGG_RESULT" | grep -q "tag"; then
    test_pass "聚合查询执行成功"
else
    test_fail "聚合查询失败: $AGG_RESULT"
fi
echo ""

# ============ 测试7: 检查异步同步指标 ============
test_step "测试7: 检查异步同步到备份池的指标"
sleep 3  # 等待异步同步
SYNC_METRICS=$(curl -s ${METRICS_URL} | grep "backup_sync")
if [ -n "$SYNC_METRICS" ]; then
    echo "$SYNC_METRICS"
    test_pass "异步同步指标已更新"
else
    test_fail "异步同步指标未找到"
fi
echo ""

# ============ 测试8: 故障切换模拟 ============
test_step "测试8: 模拟主MinIO池故障（停止主MinIO服务）"
echo -e "${YELLOW}[提示] 需要手动停止主MinIO容器来测试故障切换${NC}"
echo "执行命令: docker-compose stop minio"
echo "观察日志中的 FAILOVER 消息"
echo "然后执行查询操作，应该自动切换到备份池"
echo ""

# ============ 总结 ============
echo "========================================="
echo "测试总结"
echo "========================================="
echo -e "通过: ${GREEN}$PASSED${NC}"
echo -e "失败: ${RED}$FAILED${NC}"
echo "========================================="

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}部分测试失败，请检查日志${NC}"
    exit 1
fi
