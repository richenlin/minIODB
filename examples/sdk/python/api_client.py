#!/usr/bin/env python3
"""
MinIODB Python gRPC 客户端示例

本示例展示如何使用 gRPC 调用 MinIODB 的各种 API
包括数据操作、表管理、流式操作等

依赖安装:
    pip install grpcio grpcio-tools

需要先编译 proto 文件:
    python -m grpc_tools.protoc -I../../api/proto --python_out=. --grpc_python_out=. miniodb/v1/miniodb.proto

使用方法:
    python api_client.py --addr localhost:8080
"""

import argparse
import json
import logging
import time
from datetime import datetime
from typing import Dict, List, Optional, Any
import grpc

# 注意: 需要先生成 protobuf Python 代码
# import miniodb.v1.miniodb_pb2 as miniodb
# import miniodb.v1.miniodb_pb2_grpc as miniodb_grpc

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


# 模拟 protobuf 消息类（实际使用时应该从生成的代码导入）
# 以下是示例代码结构，实际使用需要先生成 protobuf 代码
class MinioDBClient:
    """MinIODB gRPC 客户端封装"""

    def __init__(self, addr: str):
        """创建 MinIODB 客户端

        Args:
            addr: MinIODB gRPC 服务器地址
        """
        self.channel = grpc.insecure_channel(addr)
        self.addr = addr

        # 注意: 需要从生成的代码导入
        # self.stub = miniodb_grpc.MinIODBServiceStub(self.channel)

        logger.info(f"Connected to MinIODB at {addr}")

    def close(self):
        """关闭连接"""
        if self.channel:
            self.channel.close()
            logger.info("Connection closed")

    # ===============================================
    # 数据操作
    # ===============================================

    def write_data(self, table: str, record_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        """写入数据

        Args:
            table: 表名
            record_id: 记录 ID
            payload: 数据负载

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.WriteDataRequest(
        #     table=table,
        #     data=miniodb.DataRecord(
        #         id=record_id,
        #         timestamp=google.protobuf.Timestamp(seconds=int(time.time())),
        #         payload=google.protobuf.Struct(fields=payload)
        #     )
        # )
        #
        # resp = self.stub.WriteData(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': f"Data successfully ingested for table: {table}, ID: {record_id}",
            'node_id': 'node-1'
        }

        if resp['success']:
            logger.info(f"✓ Data written successfully to table '{table}', ID: {record_id}, Node: {resp['node_id']}")
        else:
            logger.error(f"✗ Write failed: {resp['message']}")

        return resp

    def query_data(self, sql: str, limit: int = 100) -> Dict[str, Any]:
        """查询数据

        Args:
            sql: SQL 查询语句
            limit: 返回记录数限制

        Returns:
            查询结果
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.QueryDataRequest(
        #     sql=sql,
        #     limit=limit
        # )
        #
        # resp = self.stub.QueryData(req)

        # 模拟响应
        resp = {
            'result_json': json.dumps([
                {'id': 'user_0', 'name': 'User 0', 'email': 'user0@example.com', 'age': 25},
                {'id': 'user_1', 'name': 'User 1', 'email': 'user1@example.com', 'age': 26},
                {'id': 'user_2', 'name': 'User 2', 'email': 'user2@example.com', 'age': 27},
            ]),
            'has_more': False,
            'next_cursor': ''
        }

        logger.info(f"✓ Query executed successfully, has_more: {resp['has_more']}")
        return resp

    def update_data(self, table: str, record_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
        """更新数据

        Args:
            table: 表名
            record_id: 记录 ID
            payload: 更新的数据

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.UpdateDataRequest(
        #     table=table,
        #     id=record_id,
        #     timestamp=google.protobuf.Timestamp(seconds=int(time.time())),
        #     payload=google.protobuf.Struct(fields=payload)
        # )
        #
        # resp = self.stub.UpdateData(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': f"Record {record_id} updated successfully in table {table}",
            'node_id': 'node-1'
        }

        if resp['success']:
            logger.info(f"✓ Data updated successfully, ID: {record_id}")
        else:
            logger.error(f"✗ Update failed: {resp['message']}")

        return resp

    def delete_data(self, table: str, record_id: str, soft_delete: bool = False) -> Dict[str, Any]:
        """删除数据

        Args:
            table: 表名
            record_id: 记录 ID
            soft_delete: 是否软删除

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.DeleteDataRequest(
        #     table=table,
        #     id=record_id,
        #     soft_delete=soft_delete
        # )
        #
        # resp = self.stub.DeleteData(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': f"Record {record_id} deleted successfully from table {table}",
            'deleted_count': 1
        }

        if resp['success']:
            logger.info(f"✓ Data deleted successfully, deleted_count: {resp['deleted_count']}")
        else:
            logger.error(f"✗ Delete failed: {resp['message']}")

        return resp

    # ===============================================
    # 流式操作
    # ===============================================

    def stream_write(self, table: str, records: List[Dict]) -> Dict[str, Any]:
        """批量流式写入

        Args:
            table: 表名
            records: 记录列表

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # stream = self.stub.StreamWrite()
        #
        # batch_size = 100
        # for i in range(0, len(records), batch_size):
        #     batch = records[i:i + batch_size]
        #     req = miniodb.StreamWriteRequest(
        #         table=table,
        #         records=[miniodb.DataRecord(**r) for r in batch]
        #     )
        #     stream.send(req)
        #
        # resp = stream.close_and_receive()

        # 模拟响应
        resp = {
            'success': True,
            'records_count': len(records),
            'errors': []
        }

        logger.info(f"✓ Stream write completed: {resp['records_count']} records, {len(resp['errors'])} errors")
        return resp

    def stream_query(self, sql: str, batch_size: int = 100) -> List[Dict]:
        """流式查询

        Args:
            sql: SQL 查询语句
            batch_size: 批次大小

        Returns:
            所有记录列表
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.StreamQueryRequest(
        #     sql=sql,
        #     batch_size=batch_size
        # )
        #
        # stream = self.stub.StreamQuery(req)
        #
        # all_records = []
        # for resp in stream:
        #     all_records.extend(resp.records)
        #     logger.info(f"  Received batch: {len(resp.records)} records, has_more: {resp.has_more}")
        #     if not resp.has_more:
        #         break

        # 模拟响应
        all_records = [
            {'id': f'user_{i}', 'name': f'User {i}', 'batch': True}
            for i in range(10, 20)
        ]

        logger.info(f"✓ Stream query completed: {len(all_records)} total records")
        return all_records

    # ===============================================
    # 表管理
    # ===============================================

    def create_table(self, table_name: str, config: Dict = None) -> Dict[str, Any]:
        """创建表

        Args:
            table_name: 表名
            config: 表配置

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.CreateTableRequest(
        #     table_name=table_name,
        #     config=miniodb.TableConfig(**config) if config else None,
        #     if_not_exists=True
        # )
        #
        # resp = self.stub.CreateTable(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': f"Table {table_name} created successfully"
        }

        if resp['success']:
            logger.info(f"✓ Table '{table_name}' created successfully")
        else:
            logger.error(f"✗ Create table failed: {resp['message']}")

        return resp

    def list_tables(self, pattern: str = "*") -> List[Dict]:
        """列出表

        Args:
            pattern: 表名模式过滤

        Returns:
            表信息列表
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.ListTablesRequest(pattern=pattern)
        # resp = self.stub.ListTables(req)

        # 模拟响应
        resp = {
            'tables': [
                {
                    'name': 'users',
                    'status': 'active',
                    'created_at': datetime.now().isoformat(),
                    'stats': {
                        'record_count': 20,
                        'file_count': 2,
                        'size_bytes': 1024000
                    }
                },
                {
                    'name': 'orders',
                    'status': 'active',
                    'created_at': datetime.now().isoformat(),
                    'stats': {
                        'record_count': 100,
                        'file_count': 5,
                        'size_bytes': 5120000
                    }
                }
            ],
            'total': 2
        }

        logger.info(f"✓ Listed {resp['total']} tables")
        return resp['tables']

    def get_table(self, table_name: str) -> Optional[Dict]:
        """获取表信息

        Args:
            table_name: 表名

        Returns:
            表信息
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.GetTableRequest(table_name=table_name)
        # resp = self.stub.GetTable(req)

        # 模拟响应
        resp = {
            'table_info': {
                'name': table_name,
                'status': 'active',
                'created_at': datetime.now().isoformat(),
                'stats': {
                    'record_count': 20,
                    'file_count': 2,
                    'size_bytes': 1024000
                }
            }
        }

        logger.info(f"✓ Got table info for '{table_name}'")
        return resp['table_info']

    def delete_table(self, table_name: str) -> Dict[str, Any]:
        """删除表

        Args:
            table_name: 表名

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.DeleteTableRequest(
        #     table_name=table_name,
        #     if_exists=False,
        #     cascade=False
        # )
        #
        # resp = self.stub.DeleteTable(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': f"Table {table_name} deleted successfully",
            'files_deleted': 2
        }

        if resp['success']:
            logger.info(f"✓ Table '{table_name}' deleted successfully, files deleted: {resp['files_deleted']}")
        else:
            logger.error(f"✗ Delete table failed: {resp['message']}")

        return resp

    # ===============================================
    # 元数据管理
    # ===============================================

    def backup_metadata(self) -> Dict[str, Any]:
        """备份元数据

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.BackupMetadataRequest(force=False)
        # resp = self.stub.BackupMetadata(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': "Backup completed successfully",
            'backup_id': f"backup_{int(time.time())}",
            'timestamp': datetime.now().isoformat()
        }

        if resp['success']:
            logger.info(f"✓ Metadata backed up successfully, ID: {resp['backup_id']}")
        else:
            logger.error(f"✗ Backup failed: {resp['message']}")

        return resp

    def restore_metadata(self, from_latest: bool = True) -> Dict[str, Any]:
        """恢复元数据

        Args:
            from_latest: 是否从最新备份恢复

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.RestoreMetadataRequest(
        #     from_latest=from_latest,
        #     dry_run=False,
        #     overwrite=False,
        #     validate=True,
        #     parallel=True
        # )
        #
        # resp = self.stub.RestoreMetadata(req)

        # 模拟响应
        resp = {
            'success': True,
            'message': "Metadata restored successfully",
            'backup_file': 'backup_1234567890',
            'entries_total': 100,
            'entries_ok': 95,
            'entries_skipped': 3,
            'entries_error': 2,
            'duration': '2.5s'
        }

        if resp['success']:
            logger.info(f"✓ Metadata restored successfully: total={resp['entries_total']}, "
                       f"ok={resp['entries_ok']}, skipped={resp['entries_skipped']}, errors={resp['entries_error']}")
        else:
            logger.error(f"✗ Restore failed: {resp['message']}")

        return resp

    def list_backups(self, days: int = 30) -> List[Dict]:
        """列出备份

        Args:
            days: 查询最近多少天的备份

        Returns:
            备份信息列表
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.ListBackupsRequest(days=days)
        # resp = self.stub.ListBackups(req)

        # 模拟响应
        resp = {
            'backups': [
                {
                    'object_name': 'backup_1234567890',
                    'node_id': 'node-1',
                    'timestamp': datetime.now().isoformat(),
                    'size': 1024000,
                    'last_modified': datetime.now().isoformat()
                }
            ],
            'total': 1
        }

        logger.info(f"✓ Listed {resp['total']} backups")
        return resp['backups']

    # ===============================================
    # 健康检查和监控
    # ===============================================

    def health_check(self) -> Dict[str, Any]:
        """健康检查

        Returns:
            响应数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.HealthCheckRequest()
        # resp = self.stub.HealthCheck(req)

        # 模拟响应
        resp = {
            'status': 'healthy',
            'timestamp': datetime.now().isoformat(),
            'version': '1.0.0',
            'details': {
                'redis': 'ok',
                'minio': 'ok',
                'duckdb': 'ok'
            }
        }

        logger.info(f"✓ Health check: status={resp['status']}, version={resp['version']}")
        return resp

    def get_status(self) -> Dict[str, Any]:
        """获取状态

        Returns:
            状态数据
        """
        # 注意: 实际使用时使用生成的 protobuf 类
        # req = miniodb.GetStatusRequest()
        # resp = self.stub.GetStatus(req)

        # 模拟响应
        resp = {
            'timestamp': datetime.now().isoformat(),
            'buffer_stats': {
                'total_tasks': 100,
                'completed_tasks': 95,
                'failed_tasks': 2,
                'queued_tasks': 3
            },
            'redis_stats': {
                'hits': 1000,
                'misses': 100,
                'total_conns': 10
            },
            'nodes': [
                {
                    'id': 'node-1',
                    'status': 'running',
                    'type': 'primary',
                    'address': 'localhost:8080',
                    'last_seen': int(time.time())
                }
            ],
            'total_nodes': 1
        }

        logger.info(f"✓ Got status: total_nodes={resp['total_nodes']}")
        return resp


def main():
    """主函数"""
    parser = argparse.ArgumentParser(description="MinIODB Python gRPC Client Example")
    parser.add_argument("--addr", default="localhost:8080", help="MinIODB gRPC server address")
    args = parser.parse_args()

    # 创建客户端
    client = MinioDBClient(args.addr)

    try:
        print("=" * 40)
        print("MinIODB Python gRPC Client Example")
        print(f"Server: {args.addr}")
        print("=" * 40)

        # 1. 健康检查
        print("\n--- Health Check ---")
        client.health_check()

        # 2. 创建表
        print("\n--- Create Table ---")
        table_name = "users"
        config = {
            'buffer_size': 1000,
            'flush_interval_seconds': 60,
            'retention_days': 30,
            'backup_enabled': True,
            'id_strategy': 'uuid',
            'auto_generate_id': True,
            'id_validation': {
                'max_length': 255,
                'pattern': '^[a-zA-Z0-9_-]+$'
            }
        }
        client.create_table(table_name, config)

        # 3. 写入数据
        print("\n--- Write Data ---")
        for i in range(5):
            payload = {
                'name': f'User {i}',
                'email': f'user{i}@example.com',
                'age': 25 + i,
                'active': True,
                'created_at': datetime.now().isoformat()
            }
            record_id = f'user_{i}'
            client.write_data(table_name, record_id, payload)
            time.sleep(0.1)

        # 4. 查询数据
        print("\n--- Query Data ---")
        result = client.query_data(f"SELECT * FROM {table_name} LIMIT 3", 3)
        result_data = json.loads(result['result_json'])
        print(f"Query result:\n{json.dumps(result_data, indent=2)}")

        # 5. 更新数据
        print("\n--- Update Data ---")
        update_payload = {
            'name': 'Updated User 0',
            'updated_at': datetime.now().isoformat()
        }
        client.update_data(table_name, 'user_0', update_payload)

        # 6. 删除数据
        print("\n--- Delete Data ---")
        client.delete_data(table_name, 'user_4')

        # 7. 流式写入
        print("\n--- Stream Write ---")
        stream_records = [
            {
                'id': f'stream_user_{i}',
                'timestamp': datetime.now().isoformat(),
                'payload': {
                    'name': f'Stream User {i}',
                    'batch': True,
                    'created_at': datetime.now().isoformat()
                }
            }
            for i in range(10, 20)
        ]
        client.stream_write(table_name, stream_records)

        # 8. 列出表
        print("\n--- List Tables ---")
        tables = client.list_tables('*')
        for t in tables:
            print(f"  - Table: {t['name']}, Status: {t['status']}")
            if 'stats' in t:
                print(f"    Records: {t['stats']['record_count']}, Files: {t['stats']['file_count']}")

        # 9. 获取表信息
        print("\n--- Get Table Info ---")
        table_info = client.get_table(table_name)
        if table_info and 'stats' in table_info:
            print(f"  - Record count: {table_info['stats']['record_count']}")
            print(f"  - File count: {table_info['stats']['file_count']}")
            print(f"  - Size: {table_info['stats']['size_bytes']} bytes")

        # 10. 获取状态
        print("\n--- Get Status ---")
        status = client.get_status()
        print(f"  - Total nodes: {status['total_nodes']}")
        print(f"  - Buffer stats: {json.dumps(status['buffer_stats'], indent=4)}")
        print(f"  - Redis stats: {json.dumps(status['redis_stats'], indent=4)}")

        # 11. 列出备份
        print("\n--- List Backups ---")
        backups = client.list_backups(30)
        print(f"  Found {len(backups)} backups in the last 30 days")
        for b in backups:
            print(f"  - Backup: {b['object_name']}, Size: {b['size']}, Node: {b['node_id']}")

        print("\n" + "=" * 40)
        print("Example completed successfully!")
        print("=" * 40)

    finally:
        client.close()


if __name__ == "__main__":
    main()
