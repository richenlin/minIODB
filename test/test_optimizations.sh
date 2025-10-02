#!/bin/bash

# 优化测试脚本
echo "======================================"
echo "MinIODB 优化功能测试"
echo "======================================"
echo ""

BASE_URL="http://localhost:18081/v1"

# 1. 创建表测试（在MinIO中创建元数据标记文件）
echo "✅ 测试1: 创建表（应在MinIO中创建元数据标记）"
curl -X POST $BASE_URL/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "test_hybrid", "config": {"buffer_size": 100, "flush_interval_seconds": 60}}'
echo -e "\n"
sleep 2

# 2. 验证表是否可以立即列出
echo "✅ 测试2: 列出表（应能看到刚创建的表）"
curl -s $BASE_URL/tables
echo -e "\n\n"
sleep 1

# 3. 写入数据到缓冲区（不刷新）
echo "✅ 测试3: 写入数据到缓冲区"
for i in {1..5}; do
  curl -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"test_hybrid\", \"id\": \"opt-test-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"message\": \"优化测试消息 $i\", \"index\": $i}}" \
    2>/dev/null
  echo ""
done
sleep 1

# 4. 立即查询（混合查询，应能查到缓冲区数据）
echo -e "\n✅ 测试4: 立即查询（应能查到缓冲区中的数据 - 混合查询）"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM test_hybrid ORDER BY timestamp DESC LIMIT 10"}'
echo -e "\n\n"
sleep 1

# 5. 手动刷新
echo "✅ 测试5: 手动刷新数据"
curl -X POST $BASE_URL/tables/test_hybrid/flush
echo -e "\n\n"
sleep 2

# 6. 再次查询（应仍能查到数据）
echo "✅ 测试6: 刷新后查询（数据应持久化到MinIO）"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM test_hybrid"}'
echo -e "\n\n"
sleep 1

# 7. 再写入更多数据（测试持续的混合查询）
echo "✅ 测试7: 再写入数据（测试持续混合查询）"
for i in {6..8}; do
  curl -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"test_hybrid\", \"id\": \"opt-test-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"message\": \"第二批数据 $i\", \"index\": $i}}" \
    2>/dev/null
  echo ""
done
sleep 1

# 8. 最终查询（应看到MinIO数据 + 新缓冲区数据）
echo -e "\n✅ 测试8: 最终混合查询（应看到8条记录）"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM test_hybrid ORDER BY id"}'
echo -e "\n\n"

# 9. 删除测试
echo "✅ 测试9: 删除数据"
curl -X DELETE "$BASE_URL/data?table=test_hybrid&id=opt-test-1"
echo -e "\n\n"
sleep 1

# 10. 验证删除
echo "✅ 测试10: 验证删除（应只剩7条记录）"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM test_hybrid"}'
echo -e "\n\n"

echo "======================================"
echo "优化测试完成！"
echo "======================================"

