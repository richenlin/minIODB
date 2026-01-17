#!/bin/bash

# MinIODB 创建表示例脚本
# 展示如何通过 REST API 创建不同配置的表

BASE_URL="http://localhost:8081"

echo "=========================================="
echo "MinIODB 创建表示例"
echo "=========================================="
echo ""

# 示例 1: 创建使用 UUID 策略的表
echo "示例 1: 创建使用 UUID 自动生成 ID 的表"
echo "------------------------------------------"
curl -X POST ${BASE_URL}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "events_uuid",
    "config": {
      "buffer_size": 5000,
      "flush_interval_seconds": 15,
      "retention_days": 90,
      "backup_enabled": true,
      "id_strategy": "uuid",
      "auto_generate_id": true,
      "id_validation": {
        "required": false,
        "max_length": 255
      }
    },
    "if_not_exists": true
  }'
echo -e "\n"

# 示例 2: 创建使用 Snowflake 策略的表
echo "示例 2: 创建使用 Snowflake 算法生成 ID 的表"
echo "------------------------------------------"
curl -X POST ${BASE_URL}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "orders_snowflake",
    "config": {
      "buffer_size": 10000,
      "flush_interval_seconds": 10,
      "retention_days": 365,
      "backup_enabled": true,
      "id_strategy": "snowflake",
      "id_prefix": "order-",
      "auto_generate_id": true,
      "id_validation": {
        "required": false,
        "max_length": 100
      }
    },
    "if_not_exists": true
  }'
echo -e "\n"

# 示例 3: 创建使用 Custom 策略的表
echo "示例 3: 创建使用自定义前缀策略的表"
echo "------------------------------------------"
curl -X POST ${BASE_URL}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "invoices_custom",
    "config": {
      "buffer_size": 3000,
      "flush_interval_seconds": 20,
      "retention_days": 730,
      "backup_enabled": true,
      "id_strategy": "custom",
      "id_prefix": "INV-2024-",
      "auto_generate_id": true,
      "id_validation": {
        "required": false,
        "max_length": 50,
        "pattern": "^INV-[0-9]{4}-[a-f0-9]+$"
      }
    },
    "if_not_exists": true
  }'
echo -e "\n"

# 示例 4: 创建要求用户提供 ID 的表（向后兼容模式）
echo "示例 4: 创建要求用户提供 ID 的表（向后兼容）"
echo "------------------------------------------"
curl -X POST ${BASE_URL}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "products_user_provided",
    "config": {
      "buffer_size": 5000,
      "flush_interval_seconds": 30,
      "retention_days": 365,
      "backup_enabled": true,
      "id_strategy": "user_provided",
      "auto_generate_id": false,
      "id_validation": {
        "required": true,
        "max_length": 100,
        "pattern": "^PROD-[0-9]+$"
      }
    },
    "if_not_exists": true
  }'
echo -e "\n"

# 示例 5: 创建混合模式的表（可选提供 ID）
echo "示例 5: 创建混合模式的表（用户可选择提供或自动生成）"
echo "------------------------------------------"
curl -X POST ${BASE_URL}/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "logs_hybrid",
    "config": {
      "buffer_size": 8000,
      "flush_interval_seconds": 12,
      "retention_days": 30,
      "backup_enabled": false,
      "id_strategy": "uuid",
      "auto_generate_id": true,
      "id_validation": {
        "required": false,
        "max_length": 255,
        "pattern": "^[a-zA-Z0-9_-]+$"
      }
    },
    "if_not_exists": true
  }'
echo -e "\n"

echo "=========================================="
echo "列出所有表"
echo "=========================================="
curl -X GET ${BASE_URL}/v1/tables
echo -e "\n"

echo "=========================================="
echo "获取特定表的详细信息"
echo "=========================================="
curl -X GET ${BASE_URL}/v1/tables/events_uuid
echo -e "\n"

echo "=========================================="
echo "测试写入数据（不提供 ID，让系统自动生成）"
echo "=========================================="

# 测试 UUID 表
echo "写入到 events_uuid 表："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "events_uuid",
    "timestamp": "2024-01-17T10:00:00Z",
    "payload": {
      "event_type": "user_login",
      "user_id": 12345
    }
  }' | python3 -m json.tool
echo -e "\n"

# 测试 Snowflake 表
echo "写入到 orders_snowflake 表："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "orders_snowflake",
    "timestamp": "2024-01-17T10:01:00Z",
    "payload": {
      "product": "laptop",
      "quantity": 2,
      "price": 1299.99
    }
  }' | python3 -m json.tool
echo -e "\n"

# 测试 Custom 表
echo "写入到 invoices_custom 表："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "invoices_custom",
    "timestamp": "2024-01-17T10:02:00Z",
    "payload": {
      "customer": "ABC Corp",
      "amount": 5000.00
    }
  }' | python3 -m json.tool
echo -e "\n"

# 测试 User Provided 表（必须提供 ID）
echo "写入到 products_user_provided 表（提供自定义 ID）："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "products_user_provided",
    "id": "PROD-1001",
    "timestamp": "2024-01-17T10:03:00Z",
    "payload": {
      "name": "Wireless Mouse",
      "category": "Electronics"
    }
  }' | python3 -m json.tool
echo -e "\n"

# 测试 User Provided 表（不提供 ID，应该失败）
echo "写入到 products_user_provided 表（不提供 ID，预期失败）："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "products_user_provided",
    "timestamp": "2024-01-17T10:04:00Z",
    "payload": {
      "name": "Keyboard",
      "category": "Electronics"
    }
  }' | python3 -m json.tool
echo -e "\n"

# 测试 Hybrid 表（不提供 ID）
echo "写入到 logs_hybrid 表（不提供 ID，自动生成）："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "logs_hybrid",
    "timestamp": "2024-01-17T10:05:00Z",
    "payload": {
      "level": "INFO",
      "message": "Application started"
    }
  }' | python3 -m json.tool
echo -e "\n"

# 测试 Hybrid 表（提供自定义 ID）
echo "写入到 logs_hybrid 表（提供自定义 ID）："
curl -X POST ${BASE_URL}/v1/data \
  -H "Content-Type: application/json" \
  -d '{
    "table": "logs_hybrid",
    "id": "custom-log-001",
    "timestamp": "2024-01-17T10:06:00Z",
    "payload": {
      "level": "ERROR",
      "message": "Connection timeout"
    }
  }' | python3 -m json.tool
echo -e "\n"

echo "=========================================="
echo "完成！"
echo "=========================================="
