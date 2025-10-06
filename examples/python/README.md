# MinIODB Python SDK

MinIODB Python SDK 是用于与 MinIODB 服务交互的官方 Python 客户端库。

## 特性

- 🚀 **高性能**: 基于 gRPC 的高性能通信
- 🔄 **异步支持**: 完整的异步操作支持
- 🛡️ **错误处理**: 完善的错误处理和重试机制
- 📊 **流式操作**: 支持大数据量的流式读写
- 🔐 **认证支持**: 支持 API 密钥认证
- 📝 **类型提示**: 完整的 Python 类型提示
- 🐍 **Pythonic**: 符合 Python 编程习惯的 API 设计

## 安装

### 使用 pip 安装
```bash
pip install miniodb-sdk
```

### 从源码安装
```bash
git clone https://github.com/your-org/minIODB.git
cd minIODB/examples/python
pip install -e .
```

## 快速开始

### 基本用法

```python
import asyncio
from miniodb_sdk import MinIODBClient, MinIODBConfig
from miniodb_sdk.models import DataRecord, TableConfig
from datetime import datetime

async def main():
    # 创建配置
    config = MinIODBConfig(
        host="localhost",
        grpc_port=8080,
    )
    
    # 创建客户端
    async with MinIODBClient(config) as client:
        
        # 写入数据
        record = DataRecord(
            id="user-123",
            timestamp=datetime.now(),
            payload={
                "name": "John Doe",
                "age": 30,
                "email": "john@example.com"
            }
        )
        
        response = await client.write_data("users", record)
        print(f"写入成功: {response.success}")
        
        # 查询数据
        query_response = await client.query_data(
            "SELECT * FROM users WHERE age > 25",
            limit=10
        )
        
        print(f"查询结果: {query_response.result_json}")
        
        # 创建表
        table_config = TableConfig(
            buffer_size=1000,
            flush_interval_seconds=30,
            retention_days=365,
            backup_enabled=True
        )
        
        create_response = await client.create_table("products", table_config, if_not_exists=True)
        print(f"表创建成功: {create_response.success}")

# 运行示例
asyncio.run(main())
```

### 同步用法

```python
from miniodb_sdk import MinIODBSyncClient, MinIODBConfig
from miniodb_sdk.models import DataRecord

# 创建配置
config = MinIODBConfig(host="localhost", grpc_port=8080)

# 创建同步客户端
with MinIODBSyncClient(config) as client:
    
    # 写入数据
    record = DataRecord(
        id="user-456",
        timestamp=datetime.now(),
        payload={"name": "Jane Doe", "age": 28}
    )
    
    response = client.write_data("users", record)
    print(f"写入成功: {response.success}")
    
    # 查询数据
    query_response = client.query_data("SELECT COUNT(*) FROM users")
    print(f"用户总数: {query_response.result_json}")
```

## 核心功能

### 数据操作

#### 写入数据
```python
# 单条记录写入
record = DataRecord(
    id="record-id",
    timestamp=datetime.now(),
    payload={"key": "value"}
)

response = await client.write_data("table_name", record)
```

#### 批量写入
```python
records = [record1, record2, record3]
response = await client.stream_write("table_name", records)
```

#### 查询数据
```python
# 基本查询
response = await client.query_data("SELECT * FROM users", limit=100)

# 分页查询
cursor = None
while True:
    page = await client.query_data("SELECT * FROM users", limit=50, cursor=cursor)
    # 处理结果
    if not page.has_more:
        break
    cursor = page.next_cursor
```

#### 流式查询
```python
async for batch in client.stream_query("SELECT * FROM large_table", batch_size=1000):
    # 处理批次数据
    for record in batch.records:
        print(record)
```

#### 更新数据
```python
response = await client.update_data(
    "users", 
    "user-123", 
    {"age": 31, "status": "active"},
    timestamp=datetime.now()
)
```

#### 删除数据
```python
# 软删除
response = await client.delete_data("users", "user-123", soft_delete=True)

# 硬删除
response = await client.delete_data("users", "user-123", soft_delete=False)
```

### 表管理

#### 创建表
```python
config = TableConfig(
    buffer_size=2000,
    flush_interval_seconds=60,
    retention_days=730,
    backup_enabled=True,
    properties={"description": "用户数据表"}
)

response = await client.create_table("users", config, if_not_exists=True)
```

#### 列出表
```python
response = await client.list_tables(pattern="user*")
for table in response.tables:
    print(f"表名: {table.name}, 记录数: {table.stats.record_count}")
```

#### 获取表信息
```python
response = await client.get_table("users")
info = response.table_info
print(f"表状态: {info.status}")
print(f"记录数: {info.stats.record_count}")
```

#### 删除表
```python
response = await client.delete_table("old_table", if_exists=True, cascade=True)
```

### 元数据管理

#### 备份元数据
```python
response = await client.backup_metadata(force=True)
print(f"备份ID: {response.backup_id}")
```

#### 恢复元数据
```python
response = await client.restore_metadata(
    backup_file="backup_20240115_103000.json",
    from_latest=False,
    dry_run=False,
    overwrite=True,
    validate=True,
    parallel=True,
    filters={"table_pattern": "users*"},
    key_patterns=["table:*", "index:*"]
)
```

#### 列出备份
```python
response = await client.list_backups(days=7)
for backup in response.backups:
    print(f"备份: {backup.object_name}, 时间: {backup.timestamp}")
```

### 监控和健康检查

#### 健康检查
```python
response = await client.health_check()
print(f"服务状态: {response.status}")
print(f"版本: {response.version}")
```

#### 获取系统状态
```python
response = await client.get_status()
print(f"总节点数: {response.total_nodes}")
print(f"缓冲区统计: {response.buffer_stats}")
```

#### 获取性能指标
```python
response = await client.get_metrics()
print(f"性能指标: {response.performance_metrics}")
print(f"资源使用: {response.resource_usage}")
```

## 配置选项

### 基本配置
```python
from miniodb_sdk import MinIODBConfig

config = MinIODBConfig(
    host="localhost",          # 服务器地址
    grpc_port=8080,           # gRPC 端口
    rest_port=8081,           # REST 端口（可选）
)
```

### 认证配置
```python
from miniodb_sdk import MinIODBConfig, AuthConfig

config = MinIODBConfig(
    host="localhost",
    grpc_port=8080,
    auth=AuthConfig(
        api_key="your-api-key",
        secret="your-secret"
    )
)
```

### 连接配置
```python
from miniodb_sdk import MinIODBConfig, ConnectionConfig
from datetime import timedelta

config = MinIODBConfig(
    host="localhost",
    grpc_port=8080,
    connection=ConnectionConfig(
        max_connections=10,
        timeout=timedelta(seconds=30),
        retry_attempts=3,
        keepalive_time=timedelta(minutes=5)
    )
)
```

### 完整配置示例
```python
config = MinIODBConfig(
    host="miniodb-server",
    grpc_port=8080,
    rest_port=8081,
    auth=AuthConfig(
        api_key="your-api-key",
        secret="your-secret"
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
```

## 错误处理

SDK 提供了完善的错误处理机制：

```python
from miniodb_sdk.exceptions import (
    MinIODBConnectionException,
    MinIODBAuthenticationException,
    MinIODBRequestException,
    MinIODBServerException,
    MinIODBTimeoutException
)

try:
    response = await client.write_data("users", record)
    if not response.success:
        print(f"写入失败: {response.message}")
        
except MinIODBConnectionException as e:
    print(f"连接错误: {e}")
except MinIODBAuthenticationException as e:
    print(f"认证失败: {e}")
except MinIODBRequestException as e:
    print(f"请求错误: {e}")
except MinIODBServerException as e:
    print(f"服务器错误: {e}")
except MinIODBTimeoutException as e:
    print(f"请求超时: {e}")
```

## 异步操作

### 并发操作
```python
import asyncio

async def write_multiple_records():
    tasks = []
    for i in range(100):
        record = DataRecord(
            id=f"user-{i}",
            timestamp=datetime.now(),
            payload={"index": i, "data": f"data-{i}"}
        )
        task = client.write_data("users", record)
        tasks.append(task)
    
    # 并发执行所有写入操作
    results = await asyncio.gather(*tasks)
    success_count = sum(1 for r in results if r.success)
    print(f"成功写入 {success_count}/{len(results)} 条记录")
```

### 流式处理
```python
async def process_large_dataset():
    async for batch in client.stream_query(
        "SELECT * FROM large_table ORDER BY timestamp", 
        batch_size=1000
    ):
        # 异步处理每个批次
        await process_batch(batch.records)
```

## 最佳实践

### 1. 使用异步上下文管理器
```python
# 推荐
async with MinIODBClient(config) as client:
    await client.write_data("table", record)

# 或者手动管理
client = MinIODBClient(config)
try:
    await client.connect()
    await client.write_data("table", record)
finally:
    await client.close()
```

### 2. 批量操作
```python
# 推荐：批量写入大量数据
records = prepare_records()
response = await client.stream_write("table", records)

# 避免：逐条写入大量数据
for record in records:
    await client.write_data("table", record)  # 不推荐
```

### 3. 错误处理和重试
```python
from tenacity import retry, stop_after_attempt, wait_exponential

@retry(
    stop=stop_after_attempt(3),
    wait=wait_exponential(multiplier=1, min=4, max=10)
)
async def reliable_write(client, table, record):
    return await client.write_data(table, record)
```

### 4. 连接池管理
```python
# 推荐：配置合适的连接池大小
config = MinIODBConfig(
    host="localhost",
    grpc_port=8080,
    connection=ConnectionConfig(max_connections=20)
)
```

## 开发和测试

### 运行测试
```bash
# 安装开发依赖
pip install -e ".[dev]"

# 运行测试
pytest

# 运行测试并生成覆盖率报告
pytest --cov=miniodb_sdk --cov-report=html
```

### 代码格式化
```bash
# 格式化代码
black miniodb_sdk/
isort miniodb_sdk/

# 类型检查
mypy miniodb_sdk/

# 代码检查
flake8 miniodb_sdk/
```

### 生成文档
```bash
# 安装文档依赖
pip install -e ".[docs]"

# 生成文档
cd docs/
make html
```

## 许可证

本项目采用 BSD-3-Clause 许可证。详见 [LICENSE](../LICENSE) 文件。
