# 基于MinIO、DuckDB和Redis的分布式OLAP系统

## 1. 系统概述

本项目旨在构建一个极致轻量化、高性能、可水平扩展的分布式对象存储与OLAP查询分析系统。系统以MinIO作为其分布式存储底座，保证数据的可靠性和扩展性；利用DuckDB作为高性能的OLAP查询引擎，实现对TB级数据的快速分析；并采用Redis作为核心元数据中心，负责服务发现、节点管理以及数据分片的索引。

该系统的核心目标是提供一个部署简单（支持单机单节点）、资源占用少、服务健壮、具备高可用和数据备份能力、并能通过增加节点线性提升处理能力的现代化数据分析平台。

## 2. 核心设计理念

*   **存算分离**: 系统的核心是存储与计算的分离。MinIO负责"存储"，提供一个无限扩展的S3兼容对象存储池。DuckDB负责"计算"，它不持久化存储数据，而是在查询时直接从MinIO中流式读取数据进行分析。这种架构带来了极致的弹性和扩展性。
*   **轻量化与高性能**: 所有组件（MinIO, DuckDB, Redis）都是以高性能和低资源占用著称的。DuckDB尤其擅长直接查询Parquet等列式存储格式，无需数据导入，极大地提升了查询效率。主要满足资源受限场景下的OLAP分析场景。
*   **元数据驱动**: 系统的分布式协调能力完全由Redis中的元数据驱动。无论是新节点加入、数据分片位置，还是查询路由，都通过查询Redis完成，使得整个系统逻辑清晰，易于管理。
*   **自动化与自愈**: 通过服务注册与心跳机制，系统能自动感知节点的加入与离开。通过一致性哈希进行数据分片，节点扩展后新数据能自动路由到新节点，实现自动化的水平扩展。
*   **数据安全与备份**: 系统将每个存储实例视为独立节点。为了防止单点故障，内置了灵活的数据备份机制，支持将数据自动或手动备份到独立的备份存储中，确保数据安全。
  
## 3. 特性

1.  **MinIO+DuckDB**: 已作为核心组件进行设计。
2.  **Redis服务发现与索引**: 已在元数据管理层详细说明。
3.  **轻量化与单节点**: 所有组件均可单机部署，资源占用小。DuckDB的高效查询能力确保单节点也能处理TB级数据（只要内存足够执行计算）。
4.  **横向扩展与自动分片**: 通过一致性哈希和节点自注册，实现了简单的、对使用者透明的横向扩展能力。
5.  **ID+时间管理**: 数据在MinIO中的物理路径和在Redis中的索引键都严格遵循此规则。
6.  **双API接口**: 提供了Restful和gRPC的设计范例。
7.  **健壮与低资源**: 存算分离架构和心跳机制保证了系统的健壮性；所选组件均为业界公认的轻量高效方案。
8.  **集成开源组件**: 完全基于成熟的开源项目构建，最小化自研组件，降低了开发和维护成本。
9.  **数据安全**: 内置可配置的自动和手动备份机制，防止单点故障导致的数据丢失。

## 4. 架构设计

### 4.1. 整体架构图

```
+----------------+      +----------------+
|   gRPC Client  |      |  RESTful Client|
+----------------+      +----------------+
        |                      |
        v                      v
+------------------------------------------+
|          API Gateway / Query Node (Go)   |  <-- (任意一个节点都可以扮演此角色)
|                                          |
|  - Request Parsing & Validation          |
|  - Query Coordination                    |
|  - Result Aggregation                    |
+------------------------------------------+
      ^   |   ^   |
      |   |   |   |  (Service Discovery & Query Planning)
      |   v   |   v
+----------------+      +---------------------------------+
| Redis |  | Worker Nodes (Go Service) |
| ----- ||---------------------------------|
| - Service Reg. |----->| - Heartbeat & Registration      |
| - Data Index   |----->| - DuckDB Instance (embedded)    |
| - Hash Ring    |      | - Data Ingestion & Buffering    |
|                |      | - Parquet File Generation       |
+----------------+      | - Read/Write to MinIO           |
                        +---------------------------------+
                                 ^         |
                                 |         | (S3 API)
                                 v         v
                       +-------------------------+
                       |   MinIO Cluster         |
                       | (Distributed Object     |
                       |      Storage)           |
                       +-------------------------+
```

### 4.2. 模块拆解

#### a. 数据接入与查询协调层 (Go Service)

这是系统的入口。它可以是集群中的任何一个节点。
*   **API接口**: 提供Restful和gRPC两种标准接口，接收数据写入和查询请求。
*   **查询协调器 (Query Coordinator)**:
    1.  接收到查询请求后，根据查询条件（如ID范围、时间范围）访问Redis的`数据分片索引`。
    2.  获取所有相关的MinIO对象存储路径（即Parquet文件列表）。
    3.  访问Redis的`服务注册信息`，获取当前所有健康的Worker节点列表。
    4.  将文件列表分配给各个Worker节点，下发子查询任务。
    5.  等待所有Worker节点返回部分结果，并将结果聚合，最终返回给客户端。
*   **写入协调器 (Write Coordinator)**:
    1.  接收到写入请求。
    2.  根据数据中的`ID`，使用**一致性哈希算法**（存储在Redis中）来决定这条数据应该由哪个Worker节点处理。
    3.  将数据请求转发给目标Worker节点。

#### b. Worker节点 (Go Service with Embedded DuckDB)

这是系统的工作负载核心，可以水平扩展。
*   **服务注册与心跳**: 启动时，向Redis注册自己的地址和端口。并定时发送心跳，更新其在Redis中的TTL（存活时间），表明自己处于健康状态。
*   **数据缓冲与写入**:
    1.  接收来自协调器的数据写入请求。
    2.  数据不会立即写入MinIO，而是在内存中进行缓冲和聚合（例如使用Go Channel和Ticker）。
    3.  当数据达到一定阈值（如大小或时间间隔），将缓冲的数据批量转换为**Apache Parquet**格式。
    4.  生成一个唯一的文件名（如 `ID/YYYY-MM-DD/timestamp_nanoseconds.parquet`），并将其上传到MinIO。
    5.  上传成功后，在Redis的`数据分片索引`中添加一条记录，例如：`HSET index:ID:12345:2023-10-27 file_path_in_minio 1`。
*   **查询执行**:
    1.  接收协调器下发的子查询任务（包含一组MinIO上的Parquet文件路径）。
    2.  调用内嵌的DuckDB Go客户端。
    3.  执行SQL查询，DuckDB会直接通过S3协议从MinIO读取这些Parquet文件进行分析。
    4.  将查询结果返回给协调器。

#### c. 元数据管理层 (Redis)

Redis是整个分布式系统的大脑，存储了所有状态和索引信息。
*   **服务注册与发现**:
    *   **Key**: `nodes:services`
    *   **Type**: `HASH`
    *   **Usage**: 存储所有健康节点的地址。`Key`为节点ID，`Value`为`IP:Port`。节点通过心跳定时刷新这个Key的TTL。协调器通过`HGETALL`获取所有可用节点。
*   **数据分片索引**:
    *   **Key**: `index:id:{ID}:{YYYY-MM-DD}`
    *   **Type**: `HASH` or `SET`
    *   **Usage**: 存储每个ID每天对应的数据文件。`Key`包含了ID和天。`Value`是存储在MinIO上的Parquet文件路径集合。查询时可以通过`SMEMBERS`或`HGETALL`快速定位文件。
*   **一致性哈希环**:
    *   **Key**: `cluster:hash_ring`
    *   **Type**: `Sorted Set` or `String`
    *   **Usage**: 存储一致性哈希环的信息，用于写入时的分片路由。当节点增加或减少时，只需更新此数据结构。

#### d. 分布式存储层 (MinIO)

*   **角色**: 最终的数据湖（Data Lake），提供可靠、高可用的S3兼容对象存储。
*   **数据格式**: **Apache Parquet**。这是一个列式存储格式，压缩率高，非常适合OLAP场景。DuckDB对其有原生的高性能支持。
*   **数据组织**:
    *   **Bucket**: 可以按业务或租户划分，例如 `main-data`。
    *   **Object Key (路径)**: 严格按照 `ID/YYYY-MM-DD/` 的格式组织。这不仅符合需求，还能在某些查询场景下利用MinIO本身的前缀查询能力进行初步过滤。

## 5. 核心流程分析

### 5.1. 节点注册与发现
1.  一个新的Go服务实例（Worker Node）启动。
2.  它连接到Redis，并在`nodes:services`哈希表中添加自己的`NodeID`和`IP:Port`，并设置一个TTL（如60秒）。
3.  该节点启动一个定时任务（如每30秒），重复上一步，以刷新TTL，这作为心跳机制。
4.  查询协调器需要查找可用节点时，只需从`nodes:services`读取所有成员即可。如果一个节点没有在TTL内续期，它会自动从哈希表中消失。

### 5.2. 数据写入与分片
1.  客户端向任意一个节点的API网关发送写入请求 `(ID, Time, DataPayload)`。
2.  该节点（作为协调器）从Redis获取`cluster:hash_ring`，根据数据的`ID`计算出应该处理此数据的Worker Node。
3.  协调器将请求转发给目标Worker Node。
4.  Worker Node在内存中缓冲数据。
5.  当缓冲区满或达到时间阈值，Worker Node将这批数据（例如，属于同一个ID和同一天）聚合并生成一个Parquet文件。
6.  Worker Node将Parquet文件上传到MinIO，路径为 `bucket-name/ID/YYYY-MM-DD/nanotimestamp.parquet`。
7.  上传成功后，Worker Node更新Redis索引：`SADD index:id:{ID}:{YYYY-MM-DD} "bucket-name/ID/YYYY-MM-DD/nanotimestamp.parquet"`。

### 5.3. 数据查询
1.  客户端向任意一个节点的API网关发送查询请求（例如，查询ID为X，时间在T1到T2之间的数据）。
2.  协调器解析请求，生成需要查询的`{ID}`和`{YYYY-MM-DD}`的组合。
3.  协调器并发地向Redis查询所有匹配的Key（如 `KEYS index:id:X:*`），并获取所有相关的Parquet文件路径列表。
4.  协调器获取`nodes:services`中的所有健康Worker节点，并制定查询计划（例如，平均分配文件列表）。
5.  协调器向每个Worker Node下发子查询任务，包含文件列表和要执行的SQL语句。
6.  每个Worker Node的DuckDB实例执行查询 `SELECT * FROM 's3://<bucket>/path/to/file1.parquet', 's3://<bucket>/path/to/file2.parquet' WHERE ...`。DuckDB会自动处理S3认证和数据读取。
7.  Worker Node将部分结果返回给协调器。
8.  协调器聚合所有结果，并返回给客户端。

### 5.4. 节点扩容
1.  启动一个新的Worker Node实例。
2.  它会自动执行[4.1](#41-节点注册与发现)中的注册流程。
3.  一个独立的控制器或由主节点检测到`nodes:services`发生变化，会重新计算`cluster:hash_ring`并更新到Redis中。
4.  此后，新的数据写入请求会根据新环的规则，自动开始向新节点分发，实现了自动化的数据分片扩展。
5.  **注意**: 此方案中，历史数据不会自动迁移，它仍然保留在原来的位置。但由于查询是基于Redis的全局索引，无论文件是哪个节点写入的，都能被正确查询到。这大大简化了扩容逻辑，符合轻量化的设计理念。如果需要历史数据重分布，可以开发一个离线的、低优先级的后台任务来完成。

### 5.5. 数据备份与高可用
为了解决将每个MinIO实例视为独立存储节点而带来的单点故障风险，系统引入了数据备份机制。

#### a. 自动备份
- **触发时机**: 当内存缓冲区的数据成功写入主存储节点后触发。
- **逻辑**: 如果配置文件中启用了自动备份，`SharedBuffer`服务会立即发起一个异步任务，将刚刚上传到主节点的Parquet文件再次上传到指定的备份存储节点。
- **健壮性**: 主流程的成功不受备份流程影响。如果备份失败，系统会记录错误日志，为后续实现重试机制（如基于Redis的失败任务队列）提供基础。

#### a. 手动备份
- **API接口**: 提供`POST /v1/backup/trigger` (RESTful) 和 `rpc TriggerBackup(...)` (gRPC)接口。
- **功能**: 调用者可以指定`ID`和`日期`，手动触发对这部分数据的备份。
- **逻辑**: 服务端接收到请求后，会扫描主存储节点上对应`ID`和`日期`的所有Parquet文件，并将它们逐一复制到备份存储节点。此功能可用于数据恢复、迁移或对特定重要数据进行强制备份。

## 6. API 设计 (示例)

### 6.1. Restful API

*   **写入数据**
    *   `POST /v1/data`
    *   **Body**:
        ```json
        {
          "id": "user-123",
          "timestamp": "2023-10-27T10:00:00Z",
          "payload": {
            "key1": "value1",
            "key2": 100
          }
        }
        ```

*   **查询数据**
    *   `POST /v1/query`
    *   **Body**:
        ```json
        {
          "sql": "SELECT COUNT(*) FROM table WHERE id = 'user-123' AND time >= '2023-10-01' AND payload.key2 > 50"
        }
        ```
    *   *注：在后端，`table`这个概念会被协调器翻译成一组Parquet文件路径列表*

### 6.2. gRPC API

```protobuf
syntax = "proto3";

package olap.v1;

import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";

service OlapService {
  // 写入数据
  rpc Write (WriteRequest) returns (WriteResponse);
  // 执行查询
  rpc Query (QueryRequest) returns (QueryResponse);
  // 手动触发数据备份
  rpc TriggerBackup(TriggerBackupRequest) returns (TriggerBackupResponse);
}

message WriteRequest {
  string id = 1;
  google.protobuf.Timestamp timestamp = 2;
  google.protobuf.Struct payload = 3;
}

message WriteResponse {
  bool success = 1;
  string message = 2;
}

message QueryRequest {
  string sql = 1;
}

message QueryResponse {
  // 结果可以用JSON字符串或者更复杂的结构表示
  string result_json = 1;
}

message TriggerBackupRequest {
  string id = 1;
  string day = 2; // format: YYYY-MM-DD
}

message TriggerBackupResponse {
  bool success = 1;
  string message = 2;
  int32 files_backed_up = 3;
}
```
