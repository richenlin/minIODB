#!/bin/bash

echo "=== MinIODB 缓冲区数据实时查询修复验证 ==="

BASE_URL="http://localhost:8080"

# 测试用表名
TABLE_NAME="performance_test"

# 创建测试表
echo "1. 创建测试表: $TABLE_NAME"
curl -X POST "$BASE_URL/v1/tables" \
  -H "Content-Type: application/json" \
  -d "{
    \"table_name\": \"$TABLE_NAME\",
    \"if_not_exists\": true,
    \"config\": {
      \"buffer_size\": 10,
      \"flush_interval_seconds\": 5,
      \"retention_days\": 30,
      \"backup_enabled\": false
    }
  }"
echo -e "\n"

sleep 1

# 写入测试数据
echo "2. 写入测试数据到表: $TABLE_NAME"
curl -X POST "$BASE_URL/v1/data" \
  -H "Content-Type: application/json" \
  -d "{
    \"table\": \"$TABLE_NAME\",
    \"id\": \"test-fix-001\",
    \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
    \"payload\": {
      \"message\": \"缓冲区修复测试\",
      \"timestamp\": \"$(date)\",
      \"fix_version\": \"v1.0\"
    }
  }"
echo -e "\n"

# 立即查询（测试缓冲区数据是否可查）
echo "3. 立即查询缓冲区数据（修复前应该查不到，修复后应该能查到）"
curl -X POST "$BASE_URL/v1/query" \
  -H "Content-Type: application/json" \
  -d "{
    \"sql\": \"SELECT * FROM $TABLE_NAME WHERE id = 'test-fix-001'\"
  }"
echo -e "\n"

# 如果查询不到数据，手动刷新缓冲区再查询
echo "4. 手动刷新缓冲区（如果需要）"
curl -X POST "$BASE_URL/v1/flush" \
  -H "Content-Type: application/json"
echo -e "\n"

sleep 2

# 刷新后再次查询
echo "5. 刷新后再次查询"
curl -X POST "$BASE_URL/v1/query" \
  -H "Content-Type: application/json" \
  -d "{
    \"sql\": \"SELECT * FROM $TABLE_NAME WHERE id = 'test-fix-001'\"
  }"
echo -e "\n"

# 统计查询
echo "6. 统计查询"
curl -X POST "$BASE_URL/v1/query" \
  -H "Content-Type: application/json" \
  -d "{
    \"sql\": \"SELECT COUNT(*) as total_records FROM $TABLE_NAME\"
  }"
echo -e "\n"

echo "=== 测试完成 ==="
echo "如果修复成功："
echo "- 写入操作应该返回 success: true"
echo "- 立即查询应该能找到刚写入的数据（缓冲区实时查询）"
echo "- 如果立即查询不到，手动刷新后应该能查到（存储查询）"
echo "- 统计查询应该显示至少1条记录" 