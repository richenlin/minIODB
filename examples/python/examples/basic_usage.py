#!/usr/bin/env python3
"""
MinIODB Python SDK 基本使用示例

演示如何使用 MinIODB Python SDK 进行基本的数据操作。
"""

import asyncio
from datetime import datetime
from miniodb_sdk import MinIODBConfig
from miniodb_sdk.models import DataRecord, TableConfig


async def basic_usage_example():
    """基本使用示例"""
    print("MinIODB Python SDK 基本使用示例")
    print("=" * 40)
    
    # 创建配置
    config = MinIODBConfig(
        host="localhost",
        grpc_port=8080,
    )
    
    print(f"配置信息: {config.to_dict()}")
    
    # 注意：这里是示例代码，实际的客户端类还需要实现
    # async with MinIODBClient(config) as client:
    #     
    #     # 1. 创建表
    #     print("\n1. 创建表...")
    #     table_config = TableConfig(
    #         buffer_size=1000,
    #         flush_interval_seconds=30,
    #         retention_days=365,
    #         backup_enabled=True,
    #         properties={"description": "用户数据表", "owner": "user-service"}
    #     )
    #     
    #     create_response = await client.create_table("users", table_config, if_not_exists=True)
    #     print(f"表创建结果: {create_response.success}")
    #     
    #     # 2. 写入数据
    #     print("\n2. 写入数据...")
    #     record = DataRecord.create(
    #         payload={
    #             "name": "John Doe",
    #             "age": 30,
    #             "email": "john@example.com",
    #             "department": "Engineering",
    #             "salary": 75000
    #         }
    #     )
    #     
    #     write_response = await client.write_data("users", record)
    #     print(f"写入结果: {write_response.success}")
    #     print(f"处理节点: {write_response.node_id}")
    #     
    #     # 3. 批量写入
    #     print("\n3. 批量写入...")
    #     records = []
    #     for i in range(5):
    #         record = DataRecord.create(
    #             payload={
    #                 "name": f"User {i}",
    #                 "age": 25 + i,
    #                 "email": f"user{i}@example.com",
    #                 "department": "Sales" if i % 2 == 0 else "Marketing",
    #                 "salary": 60000 + i * 5000
    #             }
    #         )
    #         records.append(record)
    #     
    #     stream_response = await client.stream_write("users", records)
    #     print(f"批量写入结果: {stream_response.success}")
    #     print(f"写入记录数: {stream_response.records_count}")
    #     
    #     # 4. 查询数据
    #     print("\n4. 查询数据...")
    #     query_response = await client.query_data(
    #         "SELECT name, age, department FROM users WHERE age > 25 ORDER BY age",
    #         limit=10
    #     )
    #     
    #     print(f"查询结果: {query_response.result_json}")
    #     print(f"是否有更多数据: {query_response.has_more}")
    #     
    #     # 5. 流式查询
    #     print("\n5. 流式查询...")
    #     async for batch in client.stream_query(
    #         "SELECT * FROM users ORDER BY timestamp", 
    #         batch_size=3
    #     ):
    #         print(f"批次数据: {len(batch.records)} 条记录")
    #         for record in batch.records:
    #             print(f"  - {record.id}: {record.payload.get('name')}")
    #     
    #     # 6. 更新数据
    #     print("\n6. 更新数据...")
    #     update_response = await client.update_data(
    #         "users",
    #         record.id,  # 使用之前创建的记录ID
    #         {"age": 31, "status": "active", "last_update": datetime.now().isoformat()},
    #         timestamp=datetime.now()
    #     )
    #     print(f"更新结果: {update_response.success}")
    #     
    #     # 7. 列出表
    #     print("\n7. 列出表...")
    #     list_response = await client.list_tables()
    #     print(f"表总数: {list_response.total}")
    #     for table in list_response.tables:
    #         print(f"- 表名: {table.name}, 状态: {table.status}")
    #         if table.stats:
    #             print(f"  记录数: {table.stats.record_count}, 大小: {table.stats.size_bytes} 字节")
    #     
    #     # 8. 获取表信息
    #     print("\n8. 获取表信息...")
    #     table_response = await client.get_table("users")
    #     table_info = table_response.table_info
    #     print(f"表名: {table_info.name}")
    #     print(f"创建时间: {table_info.created_at}")
    #     print(f"配置: 缓冲区大小={table_info.config.buffer_size}")
    #     
    #     # 9. 健康检查
    #     print("\n9. 健康检查...")
    #     health_response = await client.health_check()
    #     print(f"服务状态: {health_response.status}")
    #     print(f"服务版本: {health_response.version}")
    #     print(f"检查时间: {health_response.timestamp}")
    #     
    #     # 10. 获取系统状态
    #     print("\n10. 获取系统状态...")
    #     status_response = await client.get_status()
    #     print(f"总节点数: {status_response.total_nodes}")
    #     print(f"缓冲区统计: {status_response.buffer_stats}")
    #     
    #     # 11. 获取性能指标
    #     print("\n11. 获取性能指标...")
    #     metrics_response = await client.get_metrics()
    #     print(f"性能指标: {metrics_response.performance_metrics}")
    #     print(f"资源使用: {metrics_response.resource_usage}")
    
    # 当前仅展示配置和模型的创建
    print(f"\n配置信息: {config}")
    
    sample_record = DataRecord.create(
        payload={
            "name": "Sample User",
            "age": 25,
            "email": "sample@example.com",
            "tags": ["python", "sdk", "example"]
        }
    )
    
    print(f"示例记录: {sample_record}")
    
    sample_config = TableConfig(
        buffer_size=2000,
        flush_interval_seconds=60,
        retention_days=730,
        backup_enabled=True,
        properties={"description": "示例表", "version": "1.0"}
    )
    
    print(f"示例表配置: {sample_config}")
    
    print("\n注意: 完整的客户端功能需要先生成 gRPC 代码并实现客户端类。")
    print("请运行: ./scripts/generate_python.sh 来生成必要的 gRPC 代码。")


def sync_usage_example():
    """同步使用示例"""
    print("\nMinIODB Python SDK 同步使用示例")
    print("=" * 40)
    
    # 创建配置
    config = MinIODBConfig(
        host="localhost",
        grpc_port=8080,
    )
    
    # 注意：这里是示例代码，实际的同步客户端类还需要实现
    # with MinIODBSyncClient(config) as client:
    #     
    #     # 同步写入数据
    #     record = DataRecord.create(
    #         payload={"name": "Sync User", "age": 28}
    #     )
    #     
    #     response = client.write_data("users", record)
    #     print(f"同步写入结果: {response.success}")
    #     
    #     # 同步查询数据
    #     query_response = client.query_data("SELECT COUNT(*) as total FROM users")
    #     print(f"用户总数: {query_response.result_json}")
    
    print("同步客户端示例（需要实现 MinIODBSyncClient 类）")


async def error_handling_example():
    """错误处理示例"""
    print("\nMinIODB Python SDK 错误处理示例")
    print("=" * 40)
    
    from miniodb_sdk.exceptions import (
        MinIODBConnectionException,
        MinIODBAuthenticationException,
        MinIODBRequestException,
        MinIODBServerException,
        MinIODBTimeoutException
    )
    
    # 演示异常类型
    exceptions = [
        MinIODBConnectionException("无法连接到服务器"),
        MinIODBAuthenticationException("认证失败"),
        MinIODBRequestException("请求参数错误"),
        MinIODBServerException("服务器内部错误"),
        MinIODBTimeoutException("请求超时")
    ]
    
    for exc in exceptions:
        print(f"异常类型: {type(exc).__name__}")
        print(f"错误码: {exc.error_code}")
        print(f"状态码: {exc.status_code}")
        print(f"消息: {exc.message}")
        print("-" * 30)


async def main():
    """主函数"""
    await basic_usage_example()
    sync_usage_example()
    await error_handling_example()


if __name__ == "__main__":
    asyncio.run(main())
