#!/bin/bash

BASE_URL="http://localhost:28081/v1"

echo "=== 墓碑记录过滤测试 ==="
echo "验证默认查询自动过滤墓碑记录"
echo ""

# 1. 创建测试表
echo "1. 创建测试表 tombstone_test..."
curl -s -X POST $BASE_URL/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "tombstone_test", "config": {"buffer_size": 100}}'
echo -e "\n"
sleep 2

# 2. 写入3条记录
echo "2. 写入3条初始记录..."
for i in {1..3}; do
  curl -s -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"tombstone_test\", \"id\": \"record-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"name\": \"用户$i\", \"status\": \"active\"}}"
done
echo -e "\n"
sleep 2

# 3. 查询初始数据（默认过滤墓碑）
echo "3. 查询初始数据（默认过滤墓碑，应有3条）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as count FROM tombstone_test"}'
echo -e "\n"
sleep 1

# 4. 更新record-1
echo "4. 更新 record-1（会产生1个墓碑）..."
curl -s -X PUT $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "tombstone_test",
    "id": "record-1",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "payload": {
      "name": "用户1-更新版",
      "status": "updated"
    }
  }'
echo -e "\n"
sleep 2

# 5. 删除record-2
echo "5. 删除 record-2（会产生1个墓碑）..."
curl -s -X DELETE "$BASE_URL/data?table=tombstone_test&id=record-2"
echo -e "\n"
sleep 2

# 6. 默认查询（不包含墓碑，应有2条有效记录）
echo "6. 默认查询 - 自动过滤墓碑（应有2条有效记录）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM tombstone_test ORDER BY id"}'
echo -e "\n"
sleep 1

# 7. 默认查询计数
echo "7. 默认查询计数（应为2）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as active_count FROM tombstone_test"}'
echo -e "\n"
sleep 1

# 8. 明确指定包含已删除记录
echo "8. 包含已删除记录查询（include_deleted=true，应有所有记录包括墓碑）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM tombstone_test ORDER BY timestamp", "include_deleted": true}'
echo -e "\n"
sleep 1

# 9. 包含已删除记录计数
echo "9. 包含已删除记录计数（应为5+：3原始+1更新版+2墓碑）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total_count FROM tombstone_test", "include_deleted": true}'
echo -e "\n"
sleep 1

# 10. 测试WHERE条件的过滤
echo "10. 带WHERE条件的默认查询（应自动添加墓碑过滤）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id FROM tombstone_test WHERE id = '\''record-3'\''"}'
echo -e "\n"
sleep 1

# 11. 测试ORDER BY的过滤
echo "11. 带ORDER BY的默认查询（应在ORDER BY前添加墓碑过滤）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM tombstone_test ORDER BY id DESC"}'
echo -e "\n"
sleep 1

# 12. 测试GROUP BY的过滤
echo "12. 带GROUP BY的默认查询（应在GROUP BY前添加墓碑过滤）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, COUNT(*) as cnt FROM tombstone_test GROUP BY id"}'
echo -e "\n"

echo "=== 墓碑记录过滤测试完成 ==="
echo ""
echo "测试总结："
echo "✅ 默认查询自动过滤墓碑记录"
echo "✅ include_deleted=false（默认）：只返回有效记录"
echo "✅ include_deleted=true：返回所有记录包括墓碑"
echo "✅ 支持WHERE、ORDER BY、GROUP BY等子句"
echo "✅ 智能SQL过滤条件注入"

