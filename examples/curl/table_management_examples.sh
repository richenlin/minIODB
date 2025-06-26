#!/bin/bash

# MinIODB 表管理 REST API 示例
# 使用curl命令演示表管理功能

BASE_URL="http://localhost:8081"
AUTH_TOKEN="your_auth_token_here"

echo "=== MinIODB 表管理 API 示例 ==="

# 1. 健康检查
echo -e "\n1. 健康检查"
curl -X GET "${BASE_URL}/v1/health" \
  -H "Content-Type: application/json" \
  | jq '.'

# 2. 创建用户表
echo -e "\n2. 创建用户表"
curl -X POST "${BASE_URL}/v1/tables" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "table_name": "users",
    "config": {
      "buffer_size": 1000,
      "flush_interval_seconds": 30,
      "retention_days": 365,
      "backup_enabled": true,
      "properties": {
        "description": "用户数据表",
        "owner": "user-service"
      }
    },
    "if_not_exists": true
  }' | jq '.'

# 3. 创建订单表
echo -e "\n3. 创建订单表"
curl -X POST "${BASE_URL}/v1/tables" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "table_name": "orders",
    "config": {
      "buffer_size": 2000,
      "flush_interval_seconds": 15,
      "retention_days": 2555,
      "backup_enabled": true,
      "properties": {
        "description": "订单数据表",
        "owner": "order-service"
      }
    },
    "if_not_exists": true
  }' | jq '.'

# 4. 列出所有表
echo -e "\n4. 列出所有表"
curl -X GET "${BASE_URL}/v1/tables" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  | jq '.'

# 5. 使用模式匹配列出表
echo -e "\n5. 使用模式匹配列出表 (user*)"
curl -X GET "${BASE_URL}/v1/tables?pattern=user*" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  | jq '.'

# 6. 描述用户表
echo -e "\n6. 描述用户表"
curl -X GET "${BASE_URL}/v1/tables/users" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  | jq '.'

# 7. 写入用户数据
echo -e "\n7. 写入用户数据"
curl -X POST "${BASE_URL}/v1/data" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "table": "users",
    "id": "user-123",
    "timestamp": "2024-01-15T10:00:00Z",
    "payload": {
      "name": "John Doe",
      "age": 30,
      "email": "john@example.com",
      "score": 95.5
    }
  }' | jq '.'

# 8. 写入订单数据
echo -e "\n8. 写入订单数据"
curl -X POST "${BASE_URL}/v1/data" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "table": "orders",
    "id": "order-456",
    "timestamp": "2024-01-15T10:05:00Z",
    "payload": {
      "order_id": "order-456",
      "user_id": "user-123",
      "amount": 299.99,
      "status": "completed"
    }
  }' | jq '.'

# 9. 查询用户数据
echo -e "\n9. 查询用户数据"
curl -X POST "${BASE_URL}/v1/query" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "sql": "SELECT * FROM users WHERE id = '\''user-123'\''"
  }' | jq '.'

# 10. 跨表查询
echo -e "\n10. 跨表查询"
curl -X POST "${BASE_URL}/v1/query" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "sql": "SELECT u.payload.name, o.payload.amount FROM users u JOIN orders o ON u.id = o.payload.user_id WHERE u.id = '\''user-123'\''"
  }' | jq '.'

# 11. 再次描述用户表（查看统计信息更新）
echo -e "\n11. 再次描述用户表（查看统计信息更新）"
curl -X GET "${BASE_URL}/v1/tables/users" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  | jq '.'

# 12. 删除订单表（演示，注意这会删除数据）
echo -e "\n12. 删除订单表（可选 - 会删除所有数据）"
read -p "是否要删除订单表？这将删除所有订单数据。(y/N): " confirm
if [[ $confirm == [yY] || $confirm == [yY][eE][sS] ]]; then
  curl -X DELETE "${BASE_URL}/v1/tables/orders?cascade=true" \
    -H "Authorization: Bearer ${AUTH_TOKEN}" \
    -d '{
      "if_exists": true,
      "cascade": true
    }' | jq '.'
else
  echo "跳过删除订单表"
fi

# 13. 最终列出所有表
echo -e "\n13. 最终列出所有表"
curl -X GET "${BASE_URL}/v1/tables" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  | jq '.'

echo -e "\n=== 表管理示例完成 ==="
echo "提示：请根据实际情况修改 BASE_URL 和 AUTH_TOKEN 变量" 