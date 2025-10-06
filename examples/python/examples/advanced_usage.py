#!/usr/bin/env python3
"""
MinIODB Python SDK 高级使用示例

演示 MinIODB Python SDK 的高级功能，包括：
- 异步批量操作
- 流式处理
- 错误处理和重试
- 性能监控
"""

import asyncio
import json
import time
from datetime import datetime, timedelta
from typing import List, AsyncIterator

from miniodb_sdk import MinIODBConfig, AuthConfig, ConnectionConfig, LoggingConfig
from miniodb_sdk.models import DataRecord, TableConfig
from miniodb_sdk.exceptions import (
    MinIODBConnectionException,
    MinIODBTimeoutException,
    MinIODBServerException
)


async def advanced_usage_example():
    """高级使用示例"""
    print("MinIODB Python SDK 高级使用示例")
    print("=" * 50)
    
    # 创建高级配置
    config = MinIODBConfig(
        host="localhost",
        grpc_port=8080,
        auth=AuthConfig(
            api_key="demo-api-key",
            secret="demo-secret"
        ),
        connection=ConnectionConfig(
            max_connections=20,
            timeout=timedelta(seconds=60),
            retry_attempts=5,
            keepalive_time=timedelta(minutes=10)
        ),
        logging=LoggingConfig(
            level="INFO",
            format="JSON",
            enable_request_logging=True,
            enable_performance_logging=True
        )
    )
    
    print(f"高级配置: {json.dumps(config.to_dict(), indent=2, default=str)}")
    
    # 注意：以下是完整客户端实现后的示例代码
    # async with MinIODBClient(config) as client:
    #     await batch_operations_example(client)
    #     await streaming_example(client)
    #     await error_handling_example(client)
    #     await performance_monitoring_example(client)


async def batch_operations_example(client):
    """批量操作示例"""
    print("\n1. 批量操作示例")
    print("-" * 30)
    
    # 准备大量测试数据
    records = []
    for i in range(1000):
        record = DataRecord.create(
            payload={
                "user_id": f"user_{i:04d}",
                "name": f"User {i}",
                "email": f"user{i}@example.com",
                "age": 20 + (i % 50),
                "department": ["Engineering", "Sales", "Marketing", "HR"][i % 4],
                "salary": 50000 + (i * 100),
                "join_date": (datetime.now() - timedelta(days=i)).isoformat(),
                "active": i % 10 != 0,  # 90% 活跃用户
            }
        )
        records.append(record)
    
    print(f"准备了 {len(records)} 条测试记录")
    
    # 批量写入
    start_time = time.time()
    
    # 分批写入，每批 100 条记录
    batch_size = 100
    total_written = 0
    
    for i in range(0, len(records), batch_size):
        batch = records[i:i + batch_size]
        try:
            response = await client.stream_write("users", batch)
            if response.success:
                total_written += response.records_count
                print(f"批次 {i//batch_size + 1}: 写入 {response.records_count} 条记录")
            else:
                print(f"批次 {i//batch_size + 1}: 写入失败")
                for error in response.errors:
                    print(f"  错误: {error}")
        except Exception as e:
            print(f"批次 {i//batch_size + 1}: 异常 - {e}")
    
    elapsed_time = time.time() - start_time
    print(f"批量写入完成: {total_written} 条记录，耗时 {elapsed_time:.2f} 秒")
    print(f"写入速度: {total_written/elapsed_time:.0f} 记录/秒")


async def streaming_example(client):
    """流式处理示例"""
    print("\n2. 流式处理示例")
    print("-" * 30)
    
    # 流式查询大数据集
    query = """
    SELECT 
        department,
        COUNT(*) as count,
        AVG(age) as avg_age,
        AVG(salary) as avg_salary
    FROM users 
    WHERE active = true
    GROUP BY department
    ORDER BY count DESC
    """
    
    print("执行聚合查询...")
    
    try:
        # 流式查询
        total_records = 0
        async for batch in client.stream_query(query, batch_size=1000):
            batch_count = len(batch.records)
            total_records += batch_count
            
            print(f"处理批次: {batch_count} 条记录")
            
            # 处理每个批次的数据
            for record in batch.records:
                # 这里可以进行数据处理、转换等操作
                pass
            
            if not batch.has_more:
                break
        
        print(f"流式查询完成，总共处理 {total_records} 条记录")
        
    except Exception as e:
        print(f"流式查询失败: {e}")


async def error_handling_example(client):
    """错误处理和重试示例"""
    print("\n3. 错误处理和重试示例")
    print("-" * 30)
    
    async def reliable_operation(operation_name: str, operation_func):
        """带重试的可靠操作"""
        max_retries = 3
        base_delay = 1.0
        
        for attempt in range(max_retries + 1):
            try:
                result = await operation_func()
                print(f"{operation_name}: 成功 (尝试 {attempt + 1})")
                return result
                
            except MinIODBConnectionException as e:
                if attempt < max_retries:
                    delay = base_delay * (2 ** attempt)  # 指数退避
                    print(f"{operation_name}: 连接错误，{delay}秒后重试 (尝试 {attempt + 1}/{max_retries + 1})")
                    await asyncio.sleep(delay)
                else:
                    print(f"{operation_name}: 连接错误，重试次数已用完")
                    raise
                    
            except MinIODBTimeoutException as e:
                if attempt < max_retries:
                    delay = base_delay * (2 ** attempt)
                    print(f"{operation_name}: 超时错误，{delay}秒后重试 (尝试 {attempt + 1}/{max_retries + 1})")
                    await asyncio.sleep(delay)
                else:
                    print(f"{operation_name}: 超时错误，重试次数已用完")
                    raise
                    
            except MinIODBServerException as e:
                print(f"{operation_name}: 服务器错误，不重试: {e}")
                raise
                
            except Exception as e:
                print(f"{operation_name}: 未知错误: {e}")
                raise
    
    # 示例操作
    async def test_write():
        record = DataRecord.create(
            payload={"test": "error_handling", "timestamp": datetime.now().isoformat()}
        )
        return await client.write_data("test_table", record)
    
    async def test_query():
        return await client.query_data("SELECT COUNT(*) FROM users", limit=1)
    
    # 执行带重试的操作
    try:
        await reliable_operation("测试写入", test_write)
        await reliable_operation("测试查询", test_query)
    except Exception as e:
        print(f"操作最终失败: {e}")


async def performance_monitoring_example(client):
    """性能监控示例"""
    print("\n4. 性能监控示例")
    print("-" * 30)
    
    # 监控系统状态
    try:
        status_response = await client.get_status()
        print("系统状态:")
        print(f"  总节点数: {status_response.total_nodes}")
        print(f"  缓冲区统计: {status_response.buffer_stats}")
        print(f"  Redis 统计: {status_response.redis_stats}")
        print(f"  MinIO 统计: {status_response.minio_stats}")
        
        # 节点信息
        print("  节点信息:")
        for node in status_response.nodes:
            print(f"    - {node.id}: {node.status} ({node.type})")
    
    except Exception as e:
        print(f"获取系统状态失败: {e}")
    
    # 监控性能指标
    try:
        metrics_response = await client.get_metrics()
        print("\n性能指标:")
        print(f"  性能指标: {metrics_response.performance_metrics}")
        print(f"  资源使用: {metrics_response.resource_usage}")
        print(f"  系统信息: {metrics_response.system_info}")
        
        # 计算一些关键指标
        if "query_success_rate" in metrics_response.performance_metrics:
            success_rate = metrics_response.performance_metrics["query_success_rate"]
            print(f"  查询成功率: {success_rate:.2%}")
        
        if "avg_response_time_ms" in metrics_response.performance_metrics:
            avg_response = metrics_response.performance_metrics["avg_response_time_ms"]
            print(f"  平均响应时间: {avg_response:.2f} ms")
    
    except Exception as e:
        print(f"获取性能指标失败: {e}")


async def data_analysis_example(client):
    """数据分析示例"""
    print("\n5. 数据分析示例")
    print("-" * 30)
    
    # 复杂的分析查询
    analysis_queries = [
        {
            "name": "部门统计",
            "query": """
                SELECT 
                    department,
                    COUNT(*) as employee_count,
                    AVG(age) as avg_age,
                    AVG(salary) as avg_salary,
                    MIN(salary) as min_salary,
                    MAX(salary) as max_salary
                FROM users 
                WHERE active = true
                GROUP BY department
                ORDER BY avg_salary DESC
            """
        },
        {
            "name": "年龄分布",
            "query": """
                SELECT 
                    CASE 
                        WHEN age < 25 THEN '20-24'
                        WHEN age < 30 THEN '25-29'
                        WHEN age < 35 THEN '30-34'
                        WHEN age < 40 THEN '35-39'
                        ELSE '40+'
                    END as age_group,
                    COUNT(*) as count,
                    AVG(salary) as avg_salary
                FROM users
                WHERE active = true
                GROUP BY age_group
                ORDER BY age_group
            """
        },
        {
            "name": "薪资趋势",
            "query": """
                SELECT 
                    strftime('%Y-%m', join_date) as join_month,
                    COUNT(*) as new_hires,
                    AVG(salary) as avg_starting_salary
                FROM users
                WHERE join_date >= date('now', '-12 months')
                GROUP BY join_month
                ORDER BY join_month
            """
        }
    ]
    
    for analysis in analysis_queries:
        print(f"\n执行分析: {analysis['name']}")
        try:
            start_time = time.time()
            response = await client.query_data(analysis["query"], limit=100)
            elapsed_time = time.time() - start_time
            
            print(f"  查询耗时: {elapsed_time:.3f} 秒")
            
            # 解析和显示结果
            if response.result_json:
                results = json.loads(response.result_json)
                print(f"  结果数量: {len(results)} 行")
                
                # 显示前几行结果
                for i, row in enumerate(results[:5]):
                    print(f"    {i+1}: {row}")
                
                if len(results) > 5:
                    print(f"    ... 还有 {len(results) - 5} 行")
        
        except Exception as e:
            print(f"  分析查询失败: {e}")


async def backup_and_maintenance_example(client):
    """备份和维护示例"""
    print("\n6. 备份和维护示例")
    print("-" * 30)
    
    # 手动触发备份
    try:
        print("触发元数据备份...")
        backup_response = await client.backup_metadata(force=True)
        
        if backup_response.success:
            print(f"备份成功:")
            print(f"  备份ID: {backup_response.backup_id}")
            print(f"  备份时间: {backup_response.timestamp}")
        else:
            print(f"备份失败: {backup_response.message}")
    
    except Exception as e:
        print(f"备份操作失败: {e}")
    
    # 列出最近的备份
    try:
        print("\n查询最近7天的备份...")
        backups_response = await client.list_backups(days=7)
        
        print(f"找到 {backups_response.total} 个备份:")
        for backup in backups_response.backups:
            print(f"  - {backup.object_name}")
            print(f"    节点: {backup.node_id}")
            print(f"    时间: {backup.timestamp}")
            print(f"    大小: {backup.size} 字节")
    
    except Exception as e:
        print(f"列出备份失败: {e}")
    
    # 获取元数据状态
    try:
        print("\n获取元数据状态...")
        status_response = await client.get_metadata_status()
        
        print("元数据状态:")
        print(f"  节点ID: {status_response.node_id}")
        print(f"  健康状态: {status_response.health_status}")
        print(f"  上次备份: {status_response.last_backup}")
        print(f"  下次备份: {status_response.next_backup}")
        print(f"  备份状态: {status_response.backup_status}")
    
    except Exception as e:
        print(f"获取元数据状态失败: {e}")


async def main():
    """主函数"""
    await advanced_usage_example()
    
    print("\n" + "=" * 50)
    print("高级示例完成")
    print("注意: 完整功能需要实现 MinIODBClient 类")
    print("请运行 ./scripts/generate_python.sh 生成 gRPC 代码")


if __name__ == "__main__":
    asyncio.run(main())
