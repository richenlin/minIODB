#!/usr/bin/env python3
"""
MinIODB Python SDK 示例

本示例展示如何通过 Redis Streams 或 Kafka 向 MinIODB 发布数据
适用于第三方系统集成，实现业务实时数据同步

依赖安装:
    pip install redis kafka-python grpcio grpcio-tools

使用方法:
    python miniodb_client.py --mode redis    # 使用 Redis Streams
    python miniodb_client.py --mode kafka    # 使用 Kafka
    python miniodb_client.py --mode grpc     # 使用 gRPC 直连
"""

import json
import time
import uuid
import argparse
from datetime import datetime
from typing import Dict, List, Any, Optional
from dataclasses import dataclass, field, asdict


# ===============================================
# 数据流格式定义（与 MinIODB 保持一致）
# ===============================================

@dataclass
class DataRecord:
    """数据记录"""
    id: str
    timestamp: int  # 毫秒时间戳
    payload: Dict[str, Any]
    version: Optional[int] = None
    deleted_at: Optional[int] = None


@dataclass
class EventMetadata:
    """事件元数据"""
    source: Optional[str] = None
    batch_id: Optional[str] = None
    partition_key: Optional[str] = None
    priority: str = "normal"
    trace_id: Optional[str] = None
    tags: Dict[str, str] = field(default_factory=dict)
    retry_count: int = 0


@dataclass
class DataEvent:
    """数据事件"""
    event_id: str
    event_type: str  # insert, update, delete, batch
    table: str
    timestamp: str  # ISO 8601 格式
    records: List[DataRecord]
    metadata: EventMetadata

    def to_json(self) -> str:
        """序列化为 JSON"""
        data = {
            "event_id": self.event_id,
            "event_type": self.event_type,
            "table": self.table,
            "timestamp": self.timestamp,
            "records": [asdict(r) for r in self.records],
            "metadata": asdict(self.metadata)
        }
        return json.dumps(data)

    @classmethod
    def from_json(cls, json_str: str) -> "DataEvent":
        """从 JSON 反序列化"""
        data = json.loads(json_str)
        records = [DataRecord(**r) for r in data["records"]]
        metadata = EventMetadata(**data["metadata"])
        return cls(
            event_id=data["event_id"],
            event_type=data["event_type"],
            table=data["table"],
            timestamp=data["timestamp"],
            records=records,
            metadata=metadata
        )


def new_event(event_type: str, table: str, records: List[DataRecord], 
              source: str = None) -> DataEvent:
    """创建新的数据事件"""
    event_id = f"evt_{str(uuid.uuid4())[:12]}"
    timestamp = datetime.utcnow().isoformat() + "Z"
    metadata = EventMetadata(source=source)
    return DataEvent(
        event_id=event_id,
        event_type=event_type,
        table=table,
        timestamp=timestamp,
        records=records,
        metadata=metadata
    )


def new_insert_event(table: str, record: DataRecord, source: str = None) -> DataEvent:
    """创建插入事件"""
    return new_event("insert", table, [record], source)


def new_batch_event(table: str, records: List[DataRecord], source: str = None) -> DataEvent:
    """创建批量插入事件"""
    return new_event("batch", table, records, source)


# ===============================================
# Redis Streams 客户端
# ===============================================

class RedisStreamClient:
    """Redis Streams 客户端"""

    def __init__(self, host: str = "localhost", port: int = 6379, 
                 password: str = None, db: int = 0,
                 stream_prefix: str = "miniodb:stream:"):
        import redis
        self.client = redis.Redis(
            host=host, 
            port=port, 
            password=password, 
            db=db,
            decode_responses=True
        )
        self.stream_prefix = stream_prefix
        # 测试连接
        self.client.ping()

    def publish(self, event: DataEvent) -> str:
        """发布事件到 Redis Stream"""
        stream_key = f"{self.stream_prefix}{event.table}"
        fields = {
            "event_id": event.event_id,
            "event_type": event.event_type,
            "table": event.table,
            "timestamp": event.timestamp,
            "data": event.to_json()
        }
        return self.client.xadd(stream_key, fields)

    def publish_batch(self, events: List[DataEvent]) -> List[str]:
        """批量发布事件"""
        pipe = self.client.pipeline()
        for event in events:
            stream_key = f"{self.stream_prefix}{event.table}"
            fields = {
                "event_id": event.event_id,
                "event_type": event.event_type,
                "table": event.table,
                "timestamp": event.timestamp,
                "data": event.to_json()
            }
            pipe.xadd(stream_key, fields)
        return pipe.execute()

    def close(self):
        """关闭连接"""
        self.client.close()


# ===============================================
# Kafka 客户端
# ===============================================

class KafkaClient:
    """Kafka 客户端"""

    def __init__(self, brokers: List[str], topic_prefix: str = "miniodb-"):
        from kafka import KafkaProducer
        self.producer = KafkaProducer(
            bootstrap_servers=brokers,
            value_serializer=lambda v: v.encode('utf-8'),
            key_serializer=lambda k: k.encode('utf-8') if k else None
        )
        self.topic_prefix = topic_prefix

    def publish(self, event: DataEvent):
        """发布事件到 Kafka"""
        topic = f"{self.topic_prefix}{event.table}"
        future = self.producer.send(
            topic,
            key=event.table,
            value=event.to_json(),
            headers=[
                ("event_id", event.event_id.encode()),
                ("event_type", event.event_type.encode())
            ]
        )
        return future.get(timeout=10)

    def publish_batch(self, events: List[DataEvent]):
        """批量发布事件"""
        futures = []
        for event in events:
            topic = f"{self.topic_prefix}{event.table}"
            future = self.producer.send(
                topic,
                key=event.table,
                value=event.to_json(),
                headers=[
                    ("event_id", event.event_id.encode()),
                    ("event_type", event.event_type.encode())
                ]
            )
            futures.append(future)
        
        # 等待所有消息发送完成
        results = []
        for future in futures:
            results.append(future.get(timeout=10))
        return results

    def close(self):
        """关闭连接"""
        self.producer.flush()
        self.producer.close()


# ===============================================
# gRPC 客户端（简化版，需要生成 protobuf 代码）
# ===============================================

class GRPCClient:
    """gRPC 客户端（简化版）
    
    注意: 完整使用需要先生成 protobuf Python 代码:
    python -m grpc_tools.protoc -I../../api/proto --python_out=. --grpc_python_out=. miniodb/v1/miniodb.proto
    """

    def __init__(self, addr: str = "localhost:8080"):
        import grpc
        self.channel = grpc.insecure_channel(addr)
        # 注意: 需要导入生成的 protobuf 代码
        # from miniodb.v1 import miniodb_pb2, miniodb_pb2_grpc
        # self.stub = miniodb_pb2_grpc.MinIODBServiceStub(self.channel)
        print("注意: gRPC 模式需要先生成 protobuf Python 代码")

    def write_data(self, table: str, record: DataRecord):
        """写入数据"""
        # 需要 protobuf 代码
        raise NotImplementedError("需要先生成 protobuf Python 代码")

    def close(self):
        """关闭连接"""
        self.channel.close()


# ===============================================
# 主程序
# ===============================================

def generate_record(index: int) -> DataRecord:
    """生成测试数据记录"""
    return DataRecord(
        id=f"record_{int(time.time() * 1000)}_{index}",
        timestamp=int(time.time() * 1000),
        payload={
            "user_id": index + 1,
            "action": "click",
            "page": f"/page/{index % 10}",
            "timestamp": datetime.utcnow().isoformat()
        }
    )


def run_redis_example(host: str, port: int, password: str, table: str, 
                      count: int, batch: bool):
    """Redis 示例"""
    print(f"连接到 Redis {host}:{port}...")
    
    client = RedisStreamClient(host=host, port=port, password=password)
    
    print(f"已连接. 发布 {count} 个事件到表 '{table}'...")
    
    if batch:
        # 批量发送
        records = [generate_record(i) for i in range(count)]
        events = [new_insert_event(table, r, "python-sdk") for r in records]
        
        start = time.time()
        client.publish_batch(events)
        elapsed = time.time() - start
        
        print(f"批量发布 {count} 个事件，耗时 {elapsed:.3f} 秒")
    else:
        # 逐条发送
        for i in range(count):
            record = generate_record(i)
            event = new_insert_event(table, record, "python-sdk")
            msg_id = client.publish(event)
            print(f"发布事件 {i+1}: {event.event_id} -> {msg_id}")
    
    client.close()
    print("完成!")


def run_kafka_example(brokers: str, table: str, count: int, batch: bool):
    """Kafka 示例"""
    broker_list = brokers.split(",")
    print(f"连接到 Kafka {brokers}...")
    
    client = KafkaClient(brokers=broker_list)
    
    print(f"已连接. 发布 {count} 个事件到表 '{table}'...")
    
    if batch:
        # 批量发送
        records = [generate_record(i) for i in range(count)]
        events = [new_insert_event(table, r, "python-sdk") for r in records]
        
        start = time.time()
        client.publish_batch(events)
        elapsed = time.time() - start
        
        print(f"批量发布 {count} 个事件，耗时 {elapsed:.3f} 秒")
    else:
        # 逐条发送
        for i in range(count):
            record = generate_record(i)
            event = new_insert_event(table, record, "python-sdk")
            client.publish(event)
            print(f"发布事件 {i+1}: {event.event_id}")
    
    client.close()
    print("完成!")


def main():
    parser = argparse.ArgumentParser(description="MinIODB Python SDK 示例")
    parser.add_argument("--mode", choices=["redis", "kafka", "grpc"], 
                        default="redis", help="客户端模式")
    parser.add_argument("--redis-host", default="localhost", help="Redis 主机")
    parser.add_argument("--redis-port", type=int, default=6379, help="Redis 端口")
    parser.add_argument("--redis-password", default="", help="Redis 密码")
    parser.add_argument("--kafka-brokers", default="localhost:9092", 
                        help="Kafka brokers (逗号分隔)")
    parser.add_argument("--grpc-addr", default="localhost:8080", help="gRPC 地址")
    parser.add_argument("--table", default="user_events", help="目标表名")
    parser.add_argument("--count", type=int, default=10, help="事件数量")
    parser.add_argument("--batch", action="store_true", help="使用批量模式")
    
    args = parser.parse_args()
    
    if args.mode == "redis":
        run_redis_example(
            args.redis_host, 
            args.redis_port,
            args.redis_password or None,
            args.table, 
            args.count, 
            args.batch
        )
    elif args.mode == "kafka":
        run_kafka_example(args.kafka_brokers, args.table, args.count, args.batch)
    elif args.mode == "grpc":
        print("gRPC 模式需要先生成 protobuf Python 代码")
        print("运行: python -m grpc_tools.protoc -I../../api/proto --python_out=. --grpc_python_out=. miniodb/v1/miniodb.proto")
    else:
        print(f"未知模式: {args.mode}")


if __name__ == "__main__":
    main()
