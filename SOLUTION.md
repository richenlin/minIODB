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
*   **表级数据管理**: 引入表（Table）概念，将数据按业务类型进行逻辑分离，提供表级的管理、配置和权限控制能力，实现更精细化的数据组织和治理。

## 3. 架构设计

### 3.1. 整体架构图

```
+----------------+      +----------------+
|   gRPC Client  |      |  RESTful Client|
+----------------+      +----------------+
        |                      |
        v                      v
+------------------------------------------+
|     API Gateway / Query Node (Go)       |  <-- (任意一个节点都可以扮演此角色)
|                                          |
|  - Request Parsing & Validation          |
|  - Query Coordination                    |
|  - Result Aggregation                    |
|  - Metadata Manager (NEW)                |
|    ├─ Version Management                 |
|    ├─ Backup/Recovery Control            |
|    └─ Distributed Lock Management        |
+------------------------------------------+
      ^   |                    ^   |
      |   |                    |   |  (Service Discovery & Query Planning)
      |   v                    |   v
+------------------------------------------+
|        Connection Pool Manager (NEW)     |
|                                          |
|  Redis Pool      |    MinIO Pool         |
|  ├─ Standalone   |    ├─ Primary Pool    |
|  ├─ Sentinel     |    ├─ Backup Pool     |
|  ├─ Cluster      |    ├─ Health Check    |
|  └─ Health Check |    └─ Auto Failover   |
+------------------------------------------+
      ^   |                    ^   |
      |   v                    |   v
+----------------+      +---------------------------------+
| Redis          |      | Worker Nodes (Go Service)       |
| ---------------|      |---------------------------------|
| - Service Reg. |<-----| - Heartbeat & Registration      |
| - Data Index   |----->| - DuckDB Instance (embedded)    |
| - Hash Ring    |      | - Data Ingestion & Buffering    |
| - Table Meta   |      | - Parquet File Generation       |
| - Metadata Ver |      | - Read/Write to MinIO           |
| - Backup Index |      | - Table-level Processing        |
+----------------+      | - Connection Pool Client        |
                        +---------------------------------+
                                 ^         |
                                 |         | (S3 API via Pool)
                                 v         v
                       +-------------------------+
                       |   MinIO Cluster         |
                       | (Distributed Object     |
                       |      Storage)           |
                       | ├─ Primary Storage      |
                       | ├─ Backup Storage       |
                       | └─ TABLE/ID/YYYY-MM-DD/ |
                       +-------------------------+
```

### 3.2. 模块拆解

#### a. 数据接入与查询协调层 (Go Service)

这是系统的入口。它可以是集群中的任何一个节点。
*   **API接口**: 提供Restful和gRPC两种标准接口，接收数据写入和查询请求。
*   **元数据管理器 (Metadata Manager)** (NEW):
    1.  **版本管理系统**: 维护系统元数据的语义化版本，支持版本比较和冲突检测。
    2.  **备份控制器**: 协调元数据的自动和手动备份操作。
    3.  **恢复管理器**: 处理元数据恢复请求，支持完整恢复和增量恢复。
    4.  **分布式锁管理**: 使用Redis分布式锁防止多节点并发操作冲突。
    5.  **一致性检查**: 定期验证多节点间元数据的一致性。
*   **表管理器 (Table Manager)**:
    1.  负责表的创建、删除、配置管理等操作。
    2.  维护表的元数据信息，包括配置、统计信息、权限等。
    3.  提供表的生命周期管理，包括数据清理、备份等。
*   **查询协调器 (Query Coordinator)**:
    1.  接收到查询请求后，解析SQL中的表名，验证表的存在性和访问权限。
    2.  根据查询条件（如表名、ID范围、时间范围）访问Redis的`表级数据索引`。
    3.  获取所有相关的MinIO对象存储路径（即Parquet文件列表）。
    4.  访问Redis的`服务注册信息`，获取当前所有健康的Worker节点列表。
    5.  将文件列表分配给各个Worker节点，下发子查询任务。
    6.  等待所有Worker节点返回部分结果，并将结果聚合，最终返回给客户端。
*   **写入协调器 (Write Coordinator)**:
    1.  接收到写入请求，验证表名的有效性和写入权限。
    2.  根据表名和数据中的`ID`，使用**一致性哈希算法**（存储在Redis中）来决定这条数据应该由哪个Worker节点处理。
    3.  将数据请求转发给目标Worker节点。

#### b. 连接池管理层 (Connection Pool Manager) (NEW)

统一管理系统中所有的数据库连接，提供高效、可靠的连接服务。
*   **Redis连接池管理器**:
    1.  **多模式支持**: 根据配置自动适配单机、哨兵、集群三种Redis部署模式。
    2.  **连接池优化**: 实现连接复用、预热、超时管理等优化策略。
    3.  **健康检查**: 定期检查Redis连接的健康状态，及时发现故障节点。
    4.  **故障切换**: 在哨兵和集群模式下支持自动主从切换和分片切换。
    5.  **性能监控**: 收集连接池使用情况、响应时间等性能指标。
*   **MinIO连接池管理器**:
    1.  **双池架构**: 维护主MinIO池和备份MinIO池，支持读写分离。
    2.  **自动故障切换**: 主池故障时自动切换到备份池，保证服务连续性。
    3.  **负载均衡**: 在多个MinIO实例间智能分配请求负载。
    4.  **连接优化**: 优化S3连接参数，提升上传下载性能。
    5.  **健康监控**: 持续监控MinIO服务状态和存储容量。
*   **统一管理接口**:
    1.  **配置管理**: 统一管理所有连接池的配置参数。
    2.  **状态监控**: 提供连接池状态查询和监控接口。
    3.  **动态调整**: 支持运行时动态调整连接池参数。
    4.  **故障告警**: 连接故障时及时发送告警通知。

#### c. Worker节点 (Go Service with Embedded DuckDB)

这是系统的工作负载核心，可以水平扩展。
*   **服务注册与心跳**: 启动时，向Redis注册自己的地址和端口。并定时发送心跳，更新其在Redis中的TTL（存活时间），表明自己处于健康状态。
*   **连接池客户端** (NEW): 通过连接池管理器获取到Redis和MinIO的连接，而不是直接连接。
*   **表级数据缓冲与写入**:
    1.  接收来自协调器的数据写入请求，按表名进行分离处理。
    2.  数据不会立即写入MinIO，而是在内存中按表进行缓冲和聚合（例如使用Go Channel和Ticker）。
    3.  每个表可以配置独立的缓冲区大小和刷新间隔。
    4.  当某个表的数据达到配置的阈值（如大小或时间间隔），将该表缓冲的数据批量转换为**Apache Parquet**格式。
    5.  生成一个唯一的文件名（如 `TABLE/ID/YYYY-MM-DD/timestamp_nanoseconds.parquet`），并通过连接池将其上传到MinIO。
    6.  上传成功后，在Redis的`表级数据索引`中添加一条记录，例如：`SADD index:table:{TABLE}:id:{ID}:{YYYY-MM-DD} file_path_in_minio`。
*   **查询执行**:
    1.  接收协调器下发的子查询任务（包含一组MinIO上的Parquet文件路径和表信息）。
    2.  调用内嵌的DuckDB Go客户端，支持多表查询。
    3.  执行SQL查询，DuckDB会通过连接池从MinIO读取这些Parquet文件进行分析。
    4.  将查询结果返回给协调器。

#### d. 元数据管理层 (Redis)

Redis是整个分布式系统的大脑，存储了所有状态和索引信息。
*   **服务注册与发现**:
    *   **Key**: `nodes:services`
    *   **Type**: `HASH`
    *   **Usage**: 存储所有健康节点的地址。`Key`为节点ID，`Value`为`IP:Port`。节点通过心跳定时刷新这个Key的TTL。协调器通过`HGETALL`获取所有可用节点。
*   **表级数据索引**:
    *   **Key**: `index:table:{TABLE}:id:{ID}:{YYYY-MM-DD}`
    *   **Type**: `SET`
    *   **Usage**: 存储每个表、每个ID、每天对应的数据文件。`Key`包含了表名、ID和天。`Value`是存储在MinIO上的Parquet文件路径集合。查询时可以通过`SMEMBERS`快速定位文件。
*   **表管理元数据**:
    *   **Key**: `tables:list` - 存储所有表名 (SET)
    *   **Key**: `table:{TABLE}:config` - 表配置信息 (HASH)
    *   **Key**: `table:{TABLE}:stats` - 表统计信息 (HASH)
    *   **Key**: `table:{TABLE}:created_at` - 表创建时间 (STRING)
    *   **Key**: `table:{TABLE}:last_write` - 最后写入时间 (STRING)
*   **元数据版本管理** (NEW):
    *   **Key**: `metadata:version` - 当前元数据版本号 (STRING)
    *   **Key**: `metadata:version:history` - 版本变更历史 (LIST)
    *   **Key**: `metadata:backup:list` - 备份文件列表 (ZSET，按时间排序)
    *   **Key**: `metadata:lock:{operation}` - 分布式锁 (STRING with TTL)
*   **连接池状态** (NEW):
    *   **Key**: `pool:redis:status` - Redis连接池状态 (HASH)
    *   **Key**: `pool:minio:status` - MinIO连接池状态 (HASH)
    *   **Key**: `pool:health:last_check` - 最后健康检查时间 (STRING)
*   **一致性哈希环**:
    *   **Key**: `cluster:hash_ring`
    *   **Type**: `Sorted Set` or `String`
    *   **Usage**: 存储一致性哈希环的信息，用于写入时的分片路由。当节点增加或减少时，只需更新此数据结构。

#### e. 分布式存储层 (MinIO)

*   **角色**: 最终的数据湖（Data Lake），提供可靠、高可用的S3兼容对象存储。
*   **数据格式**: **Apache Parquet**。这是一个列式存储格式，压缩率高，非常适合OLAP场景。DuckDB对其有原生的高性能支持。
*   **存储架构** (NEW):
    *   **主存储集群**: 处理主要的读写请求，提供高性能访问。
    *   **备份存储集群**: 用于数据备份和灾难恢复，可以是地理位置分离的存储。
*   **数据组织**:
    *   **Bucket**: 可以按业务或租户划分，例如 `olap-data`、`olap-backup`。
    *   **Object Key (路径)**: 严格按照 `TABLE/ID/YYYY-MM-DD/` 的格式组织。例如：
        - `users/user-123/2024-01-15/1705123456789.parquet`
        - `orders/order-456/2024-01-15/1705123456790.parquet`
        - `logs/app-logs/2024-01-15/1705123456791.parquet`
*   **备份数据组织** (NEW):
    *   **元数据备份**: `metadata/backups/YYYY-MM-DD/backup_timestamp.json`
    *   **数据备份**: 与主存储相同的路径结构，但存储在备份bucket中

## 4. 核心流程分析

### 41. 节点注册与发现
1.  一个新的Go服务实例（Worker Node）启动。
2.  它连接到Redis，并在`nodes:services`哈希表中添加自己的`NodeID`和`IP:Port`，并设置一个TTL（如60秒）。
3.  该节点启动一个定时任务（如每30秒），重复上一步，以刷新TTL，这作为心跳机制。
4.  查询协调器需要查找可用节点时，只需从`nodes:services`读取所有成员即可。如果一个节点没有在TTL内续期，它会自动从哈希表中消失。

### 4.2. 表管理流程
1. **创建表**: 客户端发送创建表请求，指定表名和配置参数。
2. **验证表名**: 系统验证表名的合法性（符合命名规范、不重复等）。
3. **存储元数据**: 在Redis中存储表的配置信息、创建时间等元数据。
4. **更新表列表**: 将新表名添加到`tables:list`集合中。
5. **初始化统计**: 为新表初始化统计信息（记录数、文件数、大小等）。

### 4.3. 表级数据写入与分片
1.  客户端向任意一个节点的API网关发送写入请求 `(TABLE, ID, Time, DataPayload)`。
2.  该节点（作为协调器）验证表的存在性和写入权限。
3.  从Redis获取`cluster:hash_ring`，根据表名和数据中的`ID`计算出应该处理此数据的Worker Node。
4.  协调器将请求转发给目标Worker Node。
5.  Worker Node根据表的配置在内存中缓冲数据。
6.  当某个表的缓冲区满或达到时间阈值，Worker Node将这批数据（例如，属于同一个表、同一个ID和同一天）聚合并生成一个Parquet文件。
7.  Worker Node将Parquet文件上传到MinIO，路径为 `bucket-name/TABLE/ID/YYYY-MM-DD/nanotimestamp.parquet`。
8.  上传成功后，Worker Node更新Redis索引：`SADD index:table:{TABLE}:id:{ID}:{YYYY-MM-DD} "TABLE/ID/YYYY-MM-DD/nanotimestamp.parquet"`。
9.  更新表的统计信息，包括记录数、文件数、最后写入时间等。

### 4.4. 表级数据查询
1.  客户端向任意一个节点的API网关发送查询请求（例如，SQL: `SELECT * FROM users WHERE id = 'user-123' AND timestamp >= '2024-01-01'`）。
2.  协调器解析SQL，提取表名（如`users`），验证表的存在性和查询权限。
3.  根据WHERE条件，生成需要查询的`{TABLE}`、`{ID}`和`{YYYY-MM-DD}`的组合。
4.  协调器并发地向Redis查询所有匹配的Key（如 `KEYS index:table:users:id:user-123:*`），并获取所有相关的Parquet文件路径列表。
5.  协调器获取`nodes:services`中的所有健康Worker节点，并制定查询计划（例如，平均分配文件列表）。
6.  协调器向每个Worker Node下发子查询任务，包含文件列表和要执行的SQL语句。
7.  每个Worker Node的DuckDB实例执行查询，例如：`SELECT * FROM read_parquet(['s3://olap-data/users/user-123/2024-01-01/file1.parquet', 's3://olap-data/users/user-123/2024-01-02/file2.parquet']) WHERE ...`。DuckDB会自动处理S3认证和数据读取。
8.  Worker Node将部分结果返回给协调器。
9.  协调器聚合所有结果，并返回给客户端。

### 4.5. 多表查询流程
1.  客户端发送包含多表JOIN的SQL查询，例如：`SELECT u.name, COUNT(o.id) FROM users u LEFT JOIN orders o ON u.id = o.user_id GROUP BY u.name`。
2.  协调器解析SQL，提取所有涉及的表名（`users`, `orders`），验证表的存在性和查询权限。
3.  为每个表分别获取相关的Parquet文件路径列表。
4.  协调器制定多表查询计划，将不同表的文件分配给Worker节点。
5.  Worker节点在DuckDB中创建虚拟表（VIEW），每个表对应一组Parquet文件。
6.  执行原始的多表JOIN SQL，DuckDB自动处理表间关联。
7.  返回聚合结果给客户端。

### 4.6. 节点扩容
1.  启动一个新的Worker Node实例。
2.  它会自动执行[5.1](#51-节点注册与发现)中的注册流程。
3.  一个独立的控制器或由主节点检测到`nodes:services`发生变化，会重新计算`cluster:hash_ring`并更新到Redis中。
4.  此后，新的数据写入请求会根据新环的规则，自动开始向新节点分发，实现了自动化的数据分片扩展。
5.  **注意**: 此方案中，历史数据不会自动迁移，它仍然保留在原来的位置。但由于查询是基于Redis的全局索引，无论文件是哪个节点写入的，都能被正确查询到。这大大简化了扩容逻辑，符合轻量化的设计理念。如果需要历史数据重分布，可以开发一个离线的、低优先级的后台任务来完成。

### 4.7. 数据备份与高可用
为了解决将每个MinIO实例视为独立存储节点而带来的单点故障风险，系统引入了数据备份机制。

#### a. 自动备份
- **触发时机**: 当内存缓冲区的数据成功写入主存储节点后触发。
- **逻辑**: 如果配置文件中启用了自动备份，`SharedBuffer`服务会立即发起一个异步任务，将刚刚上传到主节点的Parquet文件再次上传到指定的备份存储节点。
- **健壮性**: 主流程的成功不受备份流程影响。如果备份失败，系统会记录错误日志，为后续实现重试机制（如基于Redis的失败任务队列）提供基础。
- **表级配置**: 每个表可以独立配置是否启用备份，支持差异化的备份策略。

#### b. 手动备份
- **API接口**: 提供`POST /v1/backup/trigger` (RESTful) 和 `rpc TriggerBackup(...)` (gRPC)接口。
- **功能**: 调用者可以指定`表名`、`ID`和`日期`，手动触发对这部分数据的备份。
- **逻辑**: 服务端接收到请求后，会扫描主存储节点上对应表的所有Parquet文件，并将它们逐一复制到备份存储节点。此功能可用于数据恢复、迁移或对特定重要数据进行强制备份。

### 4.8. 连接池管理流程

#### a. 连接池初始化流程
1. **系统启动阶段**:
   - 读取连接池配置（Redis模式、MinIO集群信息等）
   - 创建连接池管理器实例
   - 根据配置初始化Redis连接池（单机/哨兵/集群）
   - 初始化MinIO连接池（主池/备份池）

2. **连接池预热**:
   - 创建初始连接数量的连接到连接池
   - 验证所有初始连接的有效性
   - 记录连接池初始化状态和性能基准

3. **健康检查机制启动**:
   - 启动定时健康检查任务（默认每30秒）
   - 检查Redis连接的响应时间和可用性
   - 检查MinIO连接的存储状态和访问权限
   - 将健康检查结果更新到Redis元数据中

#### b. 连接获取与释放流程
1. **连接请求处理**:
   - Worker节点请求Redis/MinIO连接
   - 连接池管理器检查连接池状态
   - 如果有可用连接，直接返回；否则创建新连接
   - 记录连接使用统计信息

2. **连接健康验证**:
   - 返回连接前验证连接有效性
   - 对于无效连接，自动创建新连接替代
   - 更新连接池健康状态指标

3. **连接回收管理**:
   - 监控连接使用时长，超时自动回收
   - 定期清理长时间未使用的连接
   - 维护连接池大小在配置范围内

#### c. 故障检测与切换流程
1. **Redis故障处理**:
   - 检测到Redis主节点故障时，触发故障切换
   - 在哨兵模式下，自动发现新的主节点
   - 在集群模式下，重新路由到健康的分片节点
   - 更新连接池配置和路由表

2. **MinIO故障处理**:
   - 检测到主MinIO池故障时，自动切换到备份池
   - 通知所有Worker节点更新MinIO连接配置
   - 启动主池恢复检测，恢复后自动切回
   - 记录故障切换日志和性能影响

3. **连接池扩缩容**:
   - 根据负载情况动态调整连接池大小
   - 高负载时自动增加连接数
   - 低负载时回收多余连接，节省资源
   - 保证连接池大小在最小值和最大值之间

### 4.9. 元数据备份恢复流程

#### a. 版本管理流程
1. **系统启动版本检查**:
   - 节点启动时从Redis读取当前元数据版本
   - 比较本地缓存版本与Redis版本
   - 如果版本不一致，触发同步流程
   - 记录版本检查结果和同步状态

2. **版本冲突检测**:
   - 多个节点同时修改元数据时，检测版本冲突
   - 使用分布式锁防止并发修改
   - 冲突发生时，采用"最后写入获胜"或"合并策略"
   - 记录冲突解决过程和结果

3. **版本更新同步**:
   - 元数据修改时，先获取分布式锁
   - 更新元数据版本号（采用语义化版本控制）
   - 将版本变更记录到历史日志
   - 通知其他节点进行版本同步

#### b. 自动备份流程
1. **定时备份触发**:
   - 根据配置的备份间隔（如每小时、每天）触发备份
   - 检查是否有其他节点正在执行备份（分布式锁）
   - 获取备份锁后，开始备份流程

2. **元数据收集**:
   - 从Redis收集所有表元数据、索引信息
   - 收集服务注册信息、配置信息
   - 收集连接池状态和性能统计
   - 生成完整的元数据快照

3. **备份文件生成**:
   - 将元数据快照序列化为JSON格式
   - 添加备份时间戳和版本信息
   - 计算备份文件的校验和
   - 上传备份文件到MinIO备份存储

4. **备份索引更新**:
   - 将新备份信息添加到备份文件列表
   - 清理过期的备份文件（根据保留策略）
   - 更新备份统计信息
   - 释放分布式锁

#### c. 恢复流程
1. **恢复请求处理**:
   - 验证恢复请求的权限和参数
   - 获取恢复操作的分布式锁
   - 检查指定的备份文件是否存在和有效

2. **备份文件验证**:
   - 下载指定的备份文件
   - 验证文件完整性（校验和）
   - 解析备份文件内容
   - 检查备份版本与当前系统的兼容性

3. **数据恢复执行**:
   - 根据恢复模式（完整/增量/选择性）执行恢复
   - 备份当前元数据（如果配置了备份选项）
   - 清理或合并现有元数据
   - 导入备份的元数据到Redis

4. **一致性检查**:
   - 验证恢复后的数据完整性
   - 检查表索引和文件路径的一致性
   - 验证服务注册信息的有效性
   - 更新元数据版本号

5. **恢复后处理**:
   - 通知所有节点重新加载元数据
   - 触发连接池重新初始化
   - 记录恢复操作日志
   - 释放分布式锁

#### d. 高可用保障流程
1. **多节点协调**:
   - 使用Redis分布式锁确保同一时间只有一个节点执行备份/恢复
   - 节点间通过Redis发布/订阅机制同步状态
   - 主节点故障时，自动选举新的主节点执行管理任务

2. **故障检测与自动恢复**:
   - 定期检查元数据一致性
   - 检测到数据不一致时，自动触发修复流程
   - 节点重新加入集群时，自动同步最新元数据
   - 网络分区恢复后，执行数据一致性检查和修复

3. **数据校验与修复**:
   - 定期执行元数据完整性检查
   - 检查Redis中的索引与MinIO实际文件的一致性
   - 发现不一致时，自动修复或生成告警
   - 维护详细的校验和修复日志

## 5. API 设计 (示例)

### 5.1. RESTful API

#### 5.1.1. 数据操作

*   **写入数据**
    *   `POST /v1/data`
    *   **Body**:
        ```json
        {
          "table": "users",
          "id": "user-123",
          "timestamp": "2023-10-27T10:00:00Z",
          "payload": {
            "name": "张三",
            "age": 25,
            "city": "北京"
          }
        }
        ```

*   **查询数据**
    *   `POST /v1/query`
    *   **Body**:
        ```json
        {
          "sql": "SELECT COUNT(*) FROM users WHERE id = 'user-123' AND timestamp >= '2023-10-01' AND payload.age > 20"
        }
        ```

*   **多表查询**
    *   `POST /v1/query`
    *   **Body**:
        ```json
        {
          "sql": "SELECT u.payload.name, COUNT(o.id) as order_count FROM users u LEFT JOIN orders o ON u.id = o.payload.user_id GROUP BY u.payload.name"
        }
        ```

#### 5.1.2. 表管理

*   **创建表**
    *   `POST /v1/tables`
    *   **Body**:
        ```json
        {
          "table_name": "products",
          "config": {
            "buffer_size": 2000,
            "flush_interval_seconds": 60,
            "retention_days": 730,
            "backup_enabled": true
          }
        }
        ```

*   **列出表**
    *   `GET /v1/tables?pattern=user*&page=1&limit=20`
    *   **Response**:
        ```json
        {
          "tables": [
            {
              "name": "users",
              "created_at": "2024-01-15T10:00:00Z",
              "last_write": "2024-01-15T15:30:00Z",
              "status": "active",
              "record_count": 50000,
              "file_count": 25,
              "size_bytes": 1024000
            }
          ],
          "total": 1,
          "page": 1,
          "limit": 20
        }
        ```

*   **描述表**
    *   `GET /v1/tables/{table_name}`
    *   **Response**:
        ```json
        {
          "table_info": {
            "name": "users",
            "config": {
              "buffer_size": 2000,
              "flush_interval_seconds": 30,
              "retention_days": 365,
              "backup_enabled": true
            },
            "created_at": "2024-01-15T10:00:00Z",
            "last_write": "2024-01-15T15:30:00Z",
            "status": "active"
          },
          "stats": {
            "record_count": 50000,
            "file_count": 25,
            "size_bytes": 1024000,
            "oldest_record": "2024-01-01T00:00:00Z",
            "newest_record": "2024-01-15T15:30:00Z"
          }
        }
        ```

*   **删除表**
    *   `DELETE /v1/tables/{table_name}?cascade=true`

*   **手动备份**
    *   `POST /v1/backup/trigger`
    *   **Body**:
        ```json
        {
          "table": "users",
          "id": "user-123",
          "day": "2024-01-15"
        }
        ```

### 5.2. gRPC API

```protobuf
syntax = "proto3";

package olap.v1;

import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";

service OlapService {
  // 数据操作
  rpc Write (WriteRequest) returns (WriteResponse);
  rpc Query (QueryRequest) returns (QueryResponse);
  
  // 表管理
  rpc CreateTable(CreateTableRequest) returns (CreateTableResponse);
  rpc DropTable(DropTableRequest) returns (DropTableResponse);
  rpc ListTables(ListTablesRequest) returns (ListTablesResponse);
  rpc DescribeTable(DescribeTableRequest) returns (DescribeTableResponse);
  
  // 备份和运维
  rpc TriggerBackup(TriggerBackupRequest) returns (TriggerBackupResponse);
  rpc RecoverData(RecoverDataRequest) returns (RecoverDataResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc GetStats(GetStatsRequest) returns (GetStatsResponse);
  rpc GetNodes(GetNodesRequest) returns (GetNodesResponse);
}

// 基础数据操作消息
message WriteRequest {
  string table = 1;                    // 表名
  string id = 2;                       // 记录ID
  google.protobuf.Timestamp timestamp = 3;  // 时间戳
  google.protobuf.Struct payload = 4;  // 数据载荷
}

message WriteResponse {
  bool success = 1;
  string message = 2;
}

message QueryRequest {
  string sql = 1;  // 支持多表查询的SQL
}

message QueryResponse {
  string result_json = 1;
}

// 表管理相关消息
message CreateTableRequest {
  string table_name = 1;
  TableConfig config = 2;
  bool if_not_exists = 3;
}

message CreateTableResponse {
  bool success = 1;
  string message = 2;
}

message DropTableRequest {
  string table_name = 1;
  bool if_exists = 2;
  bool cascade = 3;  // 是否级联删除数据
}

message DropTableResponse {
  bool success = 1;
  string message = 2;
  int32 files_deleted = 3;
}

message ListTablesRequest {
  string pattern = 1;  // 表名模式匹配
  int32 page = 2;
  int32 limit = 3;
}

message ListTablesResponse {
  repeated TableInfo tables = 1;
  int32 total = 2;
  int32 page = 3;
  int32 limit = 4;
}

message DescribeTableRequest {
  string table_name = 1;
}

message DescribeTableResponse {
  TableInfo table_info = 1;
  TableStats stats = 2;
}

message TableInfo {
  string name = 1;
  TableConfig config = 2;
  string created_at = 3;
  string last_write = 4;
  string status = 5;  // active, archived, deleting
}

message TableConfig {
  int32 buffer_size = 1;
  int32 flush_interval_seconds = 2;
  int32 retention_days = 3;
  bool backup_enabled = 4;
  map<string, string> properties = 5;
}

message TableStats {
  int64 record_count = 1;
  int64 file_count = 2;
  int64 size_bytes = 3;
  string oldest_record = 4;
  string newest_record = 5;
}

// 备份和运维消息
message TriggerBackupRequest {
  string table = 1;  // 表名
  string id = 2;     // 可选，特定ID
  string day = 3;    // 可选，特定日期 format: YYYY-MM-DD
}

message TriggerBackupResponse {
  bool success = 1;
  string message = 2;
  int32 files_backed_up = 3;
}

message RecoverDataRequest {
  string table = 1;
  string backup_source = 2;  // 备份存储位置
  string target_date = 3;    // 恢复到指定日期
}

message RecoverDataResponse {
  bool success = 1;
  string message = 2;
  int32 files_recovered = 3;
}

message HealthCheckRequest {
  repeated string components = 1;  // redis, minio, nodes
}

message HealthCheckResponse {
  string overall_status = 1;  // healthy, degraded, unhealthy
  map<string, ComponentHealth> component_status = 2;
}

message ComponentHealth {
  string status = 1;
  string message = 2;
  double response_time_ms = 3;
}

message GetStatsRequest {
  string duration = 1;  // 1h, 24h, 7d
}

message GetStatsResponse {
  int64 total_queries = 1;
  int64 total_writes = 2;
  double avg_query_time_ms = 3;
  int64 total_data_size_bytes = 4;
  int32 active_tables = 5;
}

message GetNodesRequest {
  bool include_inactive = 1;
}

message GetNodesResponse {
  repeated NodeInfo nodes = 1;
  int32 total_nodes = 2;
  int32 healthy_nodes = 3;
}

message NodeInfo {
  string node_id = 1;
  string address = 2;
  string status = 3;  // healthy, unhealthy, unknown
  string last_heartbeat = 4;
  int32 active_queries = 5;
}
```
