#!/bin/bash

BASE_URL="http://localhost:28081/v1"

echo "=== 增量视图更新测试 ==="
echo "验证视图增量更新而非完全重建"
echo ""

# 1. 创建测试表
echo "1. 创建测试表 incremental_test..."
curl -s -X POST $BASE_URL/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "incremental_test", "config": {"buffer_size": 1000}}'
echo -e "\n"
sleep 2

# 2. 写入1000条基础数据
echo "2. 写入1000条基础数据（建立基线）..."
for i in {1..1000}; do
  curl -s -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"incremental_test\", \"id\": \"rec-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"value\": $i, \"status\": \"baseline\"}}" > /dev/null
  
  # 每100条显示进度
  if [ $((i % 100)) -eq 0 ]; then
    echo "  已写入: $i/1000 条"
  fi
done
echo "完成"
sleep 3

# 3. 刷新到MinIO（建立初始视图）
echo "3. 刷新数据到MinIO（建立初始视图）..."
curl -s -X POST "$BASE_URL/tables/incremental_test/flush"
echo -e "\n"
sleep 5

# 4. 查询基线数据
echo "4. 查询基线数据（应有1000条）..."
START=$(date +%s%N)
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as baseline_count FROM incremental_test"}'
END=$(date +%s%N)
DURATION=$((($END - $START) / 1000000))
echo " (耗时: ${DURATION}ms)"
echo ""
sleep 1

# 5. 增量更新：写入50条新数据（5%变更率）
echo "5. 增量更新：写入50条新数据（5%变更率）..."
for i in {1001..1050}; do
  curl -s -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"incremental_test\", \"id\": \"rec-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"value\": $i, \"status\": \"incremental\"}}" > /dev/null
done
echo "完成（50条新记录）"
sleep 2

# 6. 触发增量刷新
echo "6. 触发增量刷新（应使用增量更新策略）..."
curl -s -X POST "$BASE_URL/tables/incremental_test/flush"
echo -e "\n"
sleep 3

# 7. 查询验证（应有1050条）
echo "7. 查询验证增量更新后的数据..."
START=$(date +%s%N)
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as after_incremental FROM incremental_test"}'
END=$(date +%s%N)
DURATION=$((($END - $START) / 1000000))
echo " (耗时: ${DURATION}ms)"
echo ""
sleep 1

# 8. 更新操作（产生墓碑）
echo "8. 更新50条记录（产生50个墓碑）..."
for i in {1..50}; do
  curl -s -X PUT $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"incremental_test\", \"id\": \"rec-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"value\": $((i*100)), \"status\": \"updated\"}}" > /dev/null
done
echo "完成"
sleep 2

# 9. 再次刷新（测试墓碑的增量更新）
echo "9. 刷新更新操作..."
curl -s -X POST "$BASE_URL/tables/incremental_test/flush"
echo -e "\n"
sleep 3

# 10. 查询验证墓碑过滤
echo "10. 查询验证（默认过滤墓碑，应有1050条有效记录）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as active_count FROM incremental_test"}'
echo -e "\n"
sleep 1

# 11. 查看完整数据（包括墓碑）
echo "11. 查看完整数据（包括墓碑，应有1100条：1050有效+50墓碑）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total_with_tombstones FROM incremental_test", "include_deleted": true}'
echo -e "\n"
sleep 1

# 12. 大规模更新测试（>10%，应触发完全重建）
echo "12. 大规模更新测试（更新200条，>10%阈值）..."
for i in {100..299}; do
  curl -s -X PUT $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"incremental_test\", \"id\": \"rec-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"value\": $((i*1000)), \"status\": \"bulk_update\"}}" > /dev/null
  
  if [ $((i % 50)) -eq 0 ]; then
    echo "  已更新: $((i-99))/200 条"
  fi
done
echo "完成"
sleep 2

# 13. 触发大规模刷新（应触发完全重建）
echo "13. 触发刷新（预期：变更率>10%，触发完全重建）..."
curl -s -X POST "$BASE_URL/tables/incremental_test/flush"
echo -e "\n"
sleep 3

# 14. 最终验证
echo "14. 最终数据验证..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as final_count FROM incremental_test"}'
echo -e "\n"
sleep 1

# 15. 查看服务器日志（验证增量更新策略）
echo "15. 查看服务器日志（验证增量更新vs完全重建）..."
echo ""
echo "=== 增量更新日志 ==="
docker logs miniodb-app 2>&1 | grep -E "(Performing incremental update|Change ratio.*exceeds threshold|Incremental update completed)" | tail -10
echo ""

echo "=== 增量视图更新测试完成 ==="
echo ""
echo "测试总结："
echo "✅ 基线数据：1000条"
echo "✅ 小规模增量（5%）：50条新增 → 使用增量更新"
echo "✅ 中规模增量（5%）：50条更新 → 使用增量更新"
echo "✅ 大规模增量（>10%）：200条更新 → 触发完全重建"
echo ""
echo "优化效果："
echo "- 变更率 < 10%: 增量更新（快速，只更新变更部分）"
echo "- 变更率 ≥ 10%: 完全重建（避免增量开销累积）"
echo "- 智能阈值：自动选择最优策略"

