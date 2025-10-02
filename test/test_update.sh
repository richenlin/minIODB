#!/bin/bash

BASE_URL="http://localhost:28081/v1"

echo "=== UPDATE接口优化测试 ==="
echo "测试标记删除旧记录 + 写入新记录策略"
echo ""

# 1. 创建测试表
echo "1. 创建测试表 update_test..."
curl -s -X POST $BASE_URL/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "update_test", "config": {"buffer_size": 100}}'
echo -e "\n"
sleep 2

# 2. 写入初始数据
echo "2. 写入初始记录 update-1..."
curl -s -X POST $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "update_test",
    "id": "update-1",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "payload": {
      "name": "张三",
      "age": 25,
      "version": 1
    }
  }'
echo -e "\n"
sleep 2

# 3. 查询初始数据
echo "3. 查询初始数据..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM update_test WHERE id = '\''update-1'\''"}'
echo -e "\n"
sleep 1

# 4. 第一次更新
echo "4. 第一次更新：修改年龄为30..."
curl -s -X PUT $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "update_test",
    "id": "update-1",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "payload": {
      "name": "张三",
      "age": 30,
      "version": 2
    }
  }'
echo -e "\n"
sleep 2

# 5. 查询更新后数据（应该看到墓碑记录 + 新记录）
echo "5. 查询第一次更新后的数据（应有2条：1墓碑+1新记录）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload, timestamp FROM update_test WHERE id = '\''update-1'\'' ORDER BY timestamp"}'
echo -e "\n"
sleep 1

# 6. 第二次更新
echo "6. 第二次更新：修改年龄为35..."
curl -s -X PUT $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "update_test",
    "id": "update-1",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "payload": {
      "name": "张三",
      "age": 35,
      "version": 3
    }
  }'
echo -e "\n"
sleep 2

# 7. 查询所有历史版本
echo "7. 查询所有历史版本（应有4条：2墓碑+2数据版本）..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload, timestamp FROM update_test WHERE id = '\''update-1'\'' ORDER BY timestamp"}'
echo -e "\n"
sleep 1

# 8. 查询所有记录（包括墓碑）
echo "8. 查询表中所有记录..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM update_test WHERE id = '\''update-1'\''"}'
echo -e "\n"
sleep 1

# 9. 写入另一条记录测试
echo "9. 写入新记录 update-2..."
curl -s -X POST $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "update_test",
    "id": "update-2",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "payload": {
      "name": "李四",
      "age": 28,
      "version": 1
    }
  }'
echo -e "\n"
sleep 2

# 10. 更新新记录
echo "10. 更新 update-2..."
curl -s -X PUT $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "update_test",
    "id": "update-2",
    "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
    "payload": {
      "name": "李四",
      "age": 29,
      "version": 2
    }
  }'
echo -e "\n"
sleep 2

# 11. 查询表统计
echo "11. 查询表统计信息..."
curl -s -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, COUNT(*) as record_count FROM update_test GROUP BY id ORDER BY id"}'
echo -e "\n"

echo "=== UPDATE接口优化测试完成 ==="
echo ""
echo "说明："
echo "- 更新操作会先写入墓碑记录标记删除旧版本"
echo "- 然后写入新版本数据"
echo "- 这种策略符合OLAP系统的不可变特性"
echo "- 所有历史版本都会保留，可用于数据审计和时间旅行"
echo "- 查询时可以通过时间戳或版本号过滤出需要的版本"

