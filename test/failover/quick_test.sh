#!/bin/bash

# 快速故障切换测试脚本
set -e

API="http://localhost:8081"
echo "====== MinIO双池故障切换快速测试 ======"

# 1. 健康检查
echo ""
echo "[1] 健康检查..."
HEALTH=$(curl -s ${API}/v1/health)
if echo "$HEALTH" | grep -q "healthy"; then
    echo "✓ 服务健康"
else
    echo "✗ 服务异常: $HEALTH"
    exit 1
fi

# 2. 检查故障切换管理器
echo ""
echo "[2] 检查故障切换管理器..."
cd ../../deploy/docker
if docker-compose logs miniodb | grep -q "Failover manager started successfully"; then
    echo "✓ 故障切换管理器已启动"
    WORKERS=$(docker-compose logs miniodb | grep "sync_workers" | tail -1)
    echo "  $WORKERS"
else
    echo "✗ 故障切换管理器未启动"
    exit 1
fi

# 3. 创建测试表
echo ""
echo "[3] 创建测试表（buffer_size=10000）..."
curl -s -X POST ${API}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "quick_test",
    "config": {
      "buffer_size": 10000,
      "flush_interval_seconds": 10,
      "backup_enabled": true
    }
  }' | jq -c '{success, message}'

# 4. 写入大量数据触发flush
echo ""
echo "[4] 写入15000条数据（超过buffer_size）..."
for i in $(seq 1 15000); do
    curl -s -X POST ${API}/v1/data \
      -H "Content-Type: application/json" \
      -d "{
        \"table\": \"quick_test\",
        \"id\": \"Q$(printf '%06d' $i)\",
        \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
        \"payload\": {\"num\": $i}
      }" > /dev/null
    
    if [ $((i % 1000)) -eq 0 ]; then
        echo "  写入进度: $i/15000"
    fi
done
echo "✓ 15000条数据写入完成"

# 5. 等待并检查flush日志
echo ""
echo "[5] 等待buffer刷新到MinIO..."
sleep 5

docker-compose logs miniodb --since 10s | grep -E "upload|flush|FPutObject|Successfully" | head -10

# 6. 停止主MinIO测试故障切换
echo ""
echo "[6] 停止主MinIO模拟故障..."
docker-compose stop minio
echo "  主MinIO已停止"
sleep 2

# 7. 写入更多数据触发故障切换
echo ""
echo "[7] 主MinIO故障后写入数据（应触发故障切换）..."
for i in $(seq 20001 35000); do
    curl -s -X POST ${API}/v1/data \
      -H "Content-Type: application/json" \
      -d "{
        \"table\": \"quick_test\",
        \"id\": \"Q$(printf '%06d' $i)\",
        \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
        \"payload\": {\"num\": $i}
      }" > /dev/null
    
    if [ $((i % 1000)) -eq 20001 ] || [ $((i % 5000)) -eq 1 ]; then
        echo "  写入进度: $((i - 20000))/15000"
    fi
done
echo "✓ 15000条数据写入完成"

# 8. 等待并检查故障切换日志
echo ""
echo "[8] 检查故障切换日志（等待15秒）..."
sleep 15

FAILOVER_LOG=$(docker-compose logs miniodb --since 20s | grep -E "FAILOVER|Switched.*backup|Primary pool.*failed")
if [ -n "$FAILOVER_LOG" ]; then
    echo "✓ 检测到故障切换！"
    echo "$FAILOVER_LOG"
else
    echo "⚠ 未检测到故障切换日志"
    echo "可能原因："
    echo "  1. 数据还在buffer中未flush"
    echo "  2. 故障切换在首次MinIO操作时触发"
fi

# 9. 恢复主MinIO
echo ""
echo "[9] 恢复主MinIO..."
docker-compose start minio
sleep 8
echo "  主MinIO已启动"

# 10. 检查恢复日志
echo ""
echo "[10] 检查自动恢复日志（等待20秒健康检查）..."
sleep 20

RECOVERY_LOG=$(docker-compose logs miniodb --since 25s | grep -E "RECOVERY|switched back|Primary pool recovered")
if [ -n "$RECOVERY_LOG" ]; then
    echo "✓ 检测到自动恢复！"
    echo "$RECOVERY_LOG"
else
    echo "⚠ 未检测到恢复日志"
fi

echo ""
echo "====== 测试完成 ======"
echo ""
echo "建议手动验证："
echo "1. docker-compose logs miniodb | grep FAILOVER"
echo "2. docker-compose logs miniodb | grep RECOVERY"
echo "3. curl http://localhost:9090/metrics | grep failover"
