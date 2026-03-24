# MinIODB Solution Architecture

## 版本信息

**当前版本**: v2.2  
**最后更新**: 2026-03-14  
**项目评分**: 8.5/10 (企业级)  
**代码规模**: 35,000+行 (internal目录)  
**测试覆盖**: 核心模块>70%  

## 1. 系统概述

MinIODB是一个极致轻量化、高性能、可水平扩展的分布式对象存储与OLAP查询分析系统。系统采用存算分离架构，以MinIO作为其分布式存储底座，保证数据的可靠性和扩展性；利用DuckDB作为高性能的OLAP查询引擎，实现对TB级数据的快速分析；并采用Redis作为核心元数据中心，负责服务发现、节点管理以及数据分片的索引。

该系统的核心目标是提供一个部署简单（支持单机单节点和分布式多节点）、资源占用少、服务健壮、具备高可用和数据备份能力、并能通过增加节点线性提升处理能力的现代化数据分析平台。

### v2.2核心特性

#### 🖥️ Dashboard管理控制台（新增）
- **Web管理界面**: 基于Next.js + shadcn/ui + Tailwind CSS的现代化管理界面
- **双模部署**: 支持All-in-One（单二进制）和Client+Server（独立镜像）两种部署模式
- **实时监控**: SSE实时推送监控指标、日志流、节点健康状态
- **功能模块**: 表管理、节点管理、数据浏览、日志查看、系统配置
- **零依赖运行**: 前端静态导出SPA通过embed.FS嵌入Go二进制，运行时无Node.js依赖

#### 🔐 安全增强（新增）
- **CORS配置**: 支持跨域请求配置，可配置允许的来源、方法、头信息
- **JWT Secret强制检查**: 安全模式下强制要求配置JWT Secret
- **API密钥管理**: 支持角色和显示名称字段，精细化权限控制

#### 📦 部署优化（新增）
- **4种部署模式**: 单节点/分布式/All-in-One/Dashboard独立
- **依赖自动检测**: 自动检测Redis、MinIO等依赖是否安装，支持一键安装
- **Dashboard独立镜像**: 支持独立部署Dashboard服务，通过HTTP/gRPC与核心服务通信

#### 🔥 性能优化特性
- **列剪枝优化**: 自动识别查询需要的列，减少50-80%数据读取量，查询性能提升2-3倍
- **混合查询**: 内存缓冲区与MinIO存储数据联合查询，数据可见延迟从15秒降到1-3秒
- **近似算法**: HyperLogLog基数估计、CountMinSketch频率估计，COUNT DISTINCT性能提升10-100倍
- **查询缓存**: Redis缓存查询结果，智能TTL策略，缓存命中延迟<10ms
- **文件缓存**: 本地磁盘缓存Parquet文件，LRU淘汰策略，提升70%+查询速度

#### 📊 监控与可观测性
- **性能监控与SLA系统**: 完整的性能指标采集、SLA合规性监控、P50/P95/P99延迟追踪
- **Prometheus指标**: 60+监控指标，支持Grafana集成和告警规则配置
- **Swagger API文档**: 完整的OpenAPI 3.0规范文档，支持在线浏览和调试（18个API接口）
- **日志系统优化**: 基于zap的结构化日志，全局使用Sugar模式提升性能10-20%

#### 💾 存储引擎特性
- **Write Ahead Log (WAL)**: 写入前先记录WAL，确保节点崩溃时数据可恢复，CRC32校验
- **真实Parquet引擎**: 基于parquet-go，支持ZSTD/Snappy/GZIP/LZ4多种压缩，压缩率5:1至10:1
- **异步Compaction**: 后台自动合并小文件，解决存储碎片化，减少70%+文件数
- **谓词下推**: 利用Parquet元数据和DuckDB谓词下推，跳过无关Row Groups，减少90%+无效读取

#### 🚀 分布式与并发特性
- **分布式聚合优化**: Map-Reduce风格的分布式聚合，支持AVG/GROUP BY/ORDER BY LIMIT
- **智能限流系统**: 分层限流策略（Health/Query/Write/Standard/Strict），路径级精细化控制
- **连接池管理**: Redis（Standalone/Sentinel/Cluster）和MinIO双池架构，健康检查与Prometheus指标，连接复用优化
- **Goroutine优化**: 统一Context传递机制，WaitGroup全程跟踪，优雅关闭流程，修复率91%（21/23）

#### 🛡️ 高可用与安全
- **三层高可用体系**: 应用层（多 MinIODB 实例 + 负载均衡）、中间件层（Redis Sentinel/Cluster）、存储层（MinIO 纠删码集群），各层独立保障，无需应用层做存储 failover
- **元数据备份与恢复**: 自动/手动备份，版本管理，增量恢复，分布式锁防止冲突
- **MinIO双池异步同步**: 主存储池+备份存储池，写入后异步复制到备份池做容灾副本，配合定期增量同步保证数据完整性
- **智能健康检查**: 15秒间隔健康检查，主备池健康状态 Prometheus 指标，告警驱动的运维决策
- **JWT认证系统**: 令牌认证、刷新、撤销，支持角色权限控制（Admin/User）
- **SQL注入防护**: 安全的SQL解析和验证，防止SQL注入攻击

#### 📦 数据管理特性
- **表级管理**: 支持多表数据隔离和差异化配置（缓冲区/刷新间隔/保留期/备份策略）
- **多索引系统**: Bloom Filter/MinMax/Inverted/Bitmap多级索引，误判率<1%
- **数据订阅**: 支持Redis Streams和Kafka订阅，实时数据推送，消费者组管理
- **ID生成策略**: 支持UUID/Snowflake/自定义ID生成器

## 2. 核心设计理念

*   **存算分离**: 系统的核心是存储与计算的分离。MinIO负责"存储"，提供一个无限扩展的S3兼容对象存储池。DuckDB负责"计算"，它不持久化存储数据，而是在查询时直接从MinIO中流式读取数据进行分析。这种架构带来了极致的弹性和扩展性。
*   **轻量化与高性能**: 所有组件（MinIO, DuckDB, Redis）都是以高性能和低资源占用著称的。DuckDB尤其擅长直接查询Parquet等列式存储格式，无需数据导入，极大地提升了查询效率。主要满足资源受限场景下的OLAP分析场景。
*   **元数据驱动**: 系统的分布式协调能力完全由Redis中的元数据驱动。无论是新节点加入、数据分片位置，还是查询路由，都通过查询Redis完成，使得整个系统逻辑清晰，易于管理。
*   **自动化与自愈**: 通过服务注册与心跳机制，系统能自动感知节点的加入与离开。通过一致性哈希进行数据分片，节点扩展后新数据能自动路由到新节点，实现自动化的水平扩展。
*   **灵活部署模式**: 系统支持两种部署模式：**单节点模式**（Redis关闭）和**分布式模式**（Redis开启）。单节点模式适合开发测试和资源受限环境，分布式模式适合生产环境和高可用场景。两种模式可以无缝切换，完全向后兼容。
*   **数据安全与备份**: 系统将每个存储实例视为独立节点。为了防止单点故障，内置了灵活的数据备份机制，支持将数据自动或手动备份到独立的备份存储中，确保数据安全。WAL机制确保写入过程中的数据持久化，CRC32校验保证数据完整性。
*   **表级数据管理**: 引入表（Table）概念，将数据按业务类型进行逻辑分离，提供表级的管理、配置和权限控制能力，实现更精细化的数据组织和治理。
*   **性能优先**: 通过**多层查询优化栈**（列剪枝→文件剪枝→谓词下推→混合查询→缓存），在大数据量场景下提供亚秒级响应时间。查询性能提升2-3倍，数据可见延迟从15秒降到1-3秒，COUNT DISTINCT性能提升10-100倍。
*   **可观测性**: 完整的性能监控、SLA追踪、60+ Prometheus指标输出，支持Grafana集成和告警规则配置。结构化日志全面覆盖关键路径，便于问题诊断和性能分析。

## 3. 架构设计

### 3.1. 整体架构图

#### 3.1.1. 分布式模式架构（Redis启用）

```
+----------------+      +----------------+      +----------------+
|   gRPC Client  |      |  RESTful Client|      |  Browser       |
+----------------+      +----------------+      +----------------+
        |                      |                       |
        v                      v                       v
+---------------------------------------------------------------+
|     API Gateway / Query Node (Go) - :8081                     |
|                                                               |
|  - Request Parsing & Validation    - Dashboard Server (新增)  |
|  - Query Coordination              - SSE Real-time Events     |
|  - Result Aggregation              - Static SPA (embedded)    |
|  - Metadata Manager                                           |
+---------------------------------------------------------------+
      ^   |                    ^   |                    ^   |
      |   |                    |   |                    |   | 
      |   v                    |   v                    |   v
+---------------------------------------------------------------+
|        Connection Pool Manager                                |
|                                                               |
|  Redis Pool      |    MinIO Pool         |   Dashboard Config|
|  ├─ Standalone   |    ├─ Primary Pool    |   ├─ All-in-One   |
|  ├─ Sentinel     |    ├─ Backup Pool     |   └─ Standalone   |
|  ├─ Cluster      |    ├─ Health Check    |                    |
|  └─ Health Check |    └─ Auto Failover   |                    |
+---------------------------------------------------------------+
      |   v                    |   v  (Service Discovery & Query Planning)
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

#### 3.1.2. 单节点模式架构（Redis关闭）

```
+----------------+      +----------------+
|   gRPC Client  |      |  RESTful Client|
+----------------+      +----------------+
        |                      |
        v                      v
+------------------------------------------+
|        Single Node (Go Service)          |
|                                          |
|  - Request Parsing & Validation          |
|  - Local Query Coordination              |
|  - Direct Result Processing              |
|  - Local Metadata Management             |
|    ├─ Local Table Management             |
|    ├─ Local Backup Control               |
|    └─ Direct MinIO Access                |
|                                          |
|  - DuckDB Instance (embedded)            |
|  - Data Ingestion & Buffering            |
|  - Parquet File Generation               |
|  - Table-level Processing                |
+------------------------------------------+
                                 |
                                 | (Direct S3 API)
                                 v
                       +-------------------------+
                       |   MinIO Server          |
                       | (Single Object Storage) |
                       | ├─ Primary Storage      |
                       | ├─ Backup Storage       |
                       | └─ TABLE/ID/YYYY-MM-DD/ |
                       +-------------------------+
```

**单节点模式特点：**
- ✅ **无需Redis依赖**：通过配置`redis.enabled: false`即可启用
- ✅ **本地节点路由**：所有操作直接在本节点处理
- ✅ **直接MinIO访问**：通过MinIO对象路径直接获取数据索引
- ✅ **简化部署**：只需MinIO + MinIODB服务即可运行
- ✅ **完全兼容**：API接口与分布式模式完全一致

### 3.2. 模块拆解

#### a. 数据接入与查询协调层 (Go Service)

这是系统的入口。它可以是集群中的任何一个节点。
*   **API接口**: 提供RESTful和gRPC两种标准接口，接收数据写入和查询请求。支持Swagger/OpenAPI 3.0文档，提供18个核心API端点（认证3个、数据操作4个、表管理4个、元数据4个、监控2个、系统1个）。
*   **元数据管理器 (Metadata Manager)**:
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

#### b. 连接池管理层 (Connection Pool Manager)

统一管理系统中所有的数据库连接，提供高效、可靠的连接服务。
*   **Redis连接池管理器**:
    1.  **多模式支持**: 根据配置自动适配单机、哨兵、集群三种Redis部署模式。
    2.  **连接池优化**: 实现连接复用、预热、超时管理等优化策略。
    3.  **健康检查**: 定期检查Redis连接的健康状态，及时发现故障节点。
    4.  **故障切换**: 在哨兵和集群模式下支持自动主从切换和分片切换。
    5.  **性能监控**: 收集连接池使用情况、响应时间等性能指标。
*   **MinIO连接池管理器**:
    1.  **双池架构**: 维护主MinIO池和备份MinIO池，主池负责所有读写，备份池用于容灾副本存储。
    2.  **异步数据同步**: 主池写入成功后，通过 `FailoverManager.EnqueueSync` 异步复制到备份池（队列大小1000，3个worker并发），不阻塞主流程。
    3.  **智能健康检查**: 15秒间隔检查主池和备份池健康状态，故障快速发现，Prometheus 指标告警。
    4.  **连接优化**: 可配置的 `http.Transport` 参数（MaxConnsPerHost、IdleConnTimeout等），优化S3连接性能。
    5.  **性能监控**: Prometheus指标监控同步队列深度、成功/失败率、主备池健康状态。
    6.  **热备隔离**: 热备 Replicator 使用独立的 `MinIOPool` 实例（低配参数），与主业务连接池物理隔离。
*   **统一管理接口**:
    1.  **配置管理**: 统一管理所有连接池的配置参数。
    2.  **状态监控**: 提供连接池状态查询和监控接口。
    3.  **动态调整**: 支持运行时动态调整连接池参数。
    4.  **故障告警**: 连接故障时及时发送告警通知。

#### c. Worker节点 (Go Service with Embedded DuckDB)

这是系统的工作负载核心，可以水平扩展。
*   **服务注册与心跳**: 启动时，向Redis注册自己的地址和端口。并定时发送心跳，更新其在Redis中的TTL（存活时间），表明自己处于健康状态。
*   **连接池客户端**: 通过连接池管理器获取到Redis和MinIO的连接，而不是直接连接。
*   **表级数据缓冲与写入**:
    1.  接收来自协调器的数据写入请求，按表名进行分离处理。
    2.  **WAL记录**: 数据写入内存缓冲区之前，先写入Write Ahead Log，记录操作日志并CRC32校验，确保节点崩溃时可恢复。
    3.  数据不会立即写入MinIO，而是在内存中按表进行缓冲和聚合（使用ConcurrentBuffer，支持20个并发worker）。
    4.  每个表可以配置独立的缓冲区大小（默认5000条）和刷新间隔（默认15秒）。
    5.  当某个表的数据达到配置的阈值（如大小或时间间隔），将该表缓冲的数据批量转换为**Apache Parquet**格式（使用parquet-go，支持ZSTD/Snappy/GZIP/LZ4压缩）。
    6.  生成一个唯一的文件名（如 `TABLE/ID/YYYY-MM-DD/timestamp_nanoseconds.parquet`），并通过连接池将其上传到MinIO。
    7.  上传成功后，在Redis的`表级数据索引`中添加一条记录，例如：`SADD index:table:{TABLE}:id:{ID}:{YYYY-MM-DD} file_path_in_minio`。
    8.  **异步Compaction**: 后台Compaction Manager定期扫描小文件（默认<10MB），合并为大文件（默认>100MB），减少文件碎片化。
*   **查询执行**:
    1.  接收协调器下发的子查询任务（包含一组MinIO上的Parquet文件路径和表信息）。
    2.  **查询优化**: 
        - **列剪枝**: ColumnPruner自动分析SQL，提取需要的列，生成优化的Parquet读取视图。
        - **文件剪枝**: FilePruner利用Parquet元数据（min/max统计），跳过无关文件。
        - **谓词下推**: DuckDB自动将WHERE条件下推到Parquet读取层。
        - **混合查询**: HybridQueryEngine联合查询内存缓冲区和MinIO存储的数据。
        - **查询缓存**: QueryCache缓存查询结果到Redis，TTL根据查询类型智能调整。
        - **文件缓存**: FileCache在本地磁盘缓存热点Parquet文件，LRU淘汰策略。
    3.  调用内嵌的DuckDB Go客户端，支持多表查询。
    4.  执行SQL查询，DuckDB会通过连接池从MinIO或本地缓存读取Parquet文件进行分析。
    5.  将查询结果返回给协调器。

#### d. Dashboard管理控制台（新增）

提供Web可视化管理界面，支持系统监控和运维管理。
*   **双模部署**:
    1.  **All-in-One模式**: Dashboard嵌入核心服务二进制，通过`go build -tags dashboard`编译，单端口运行
    2.  **独立模式**: Dashboard作为独立服务运行，通过HTTP/gRPC与核心服务通信
*   **前端架构**:
    1.  **Next.js SPA**: 使用Next.js框架，静态导出后嵌入Go二进制
    2.  **shadcn/ui**: 现代化UI组件库，基于Radix UI
    3.  **Tailwind CSS**: 原子化CSS框架，轻量高效
    4.  **ECharts**: 数据可视化图表库
*   **后端服务**:
    1.  **Dashboard API**: Gin路由处理`/dashboard/api/*`请求
    2.  **Dashboard Service**: 业务逻辑层，通过CoreClient接口与核心通信
    3.  **SSE Hub**: Server-Sent Events实时推送监控数据、日志流
*   **功能模块**:
    1.  **系统概览**: 集群状态、资源使用、关键指标
    2.  **表管理**: 创建/删除表、配置管理、数据浏览
    3.  **节点管理**: 节点列表、健康状态、负载均衡
    4.  **监控告警**: Prometheus指标集成、告警规则配置
    5.  **日志查看**: 实时日志流、日志搜索和过滤
    6.  **系统配置**: 运行时配置查看和修改

#### e. 元数据管理层 (Redis)

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
*   **元数据版本管理**:
    *   **Key**: `metadata:version` - 当前元数据版本号 (STRING)
    *   **Key**: `metadata:version:history` - 版本变更历史 (LIST)
    *   **Key**: `metadata:backup:list` - 备份文件列表 (ZSET，按时间排序)
    *   **Key**: `metadata:lock:{operation}` - 分布式锁 (STRING with TTL)
*   **连接池状态**:
    *   **Key**: `pool:redis:status` - Redis连接池状态 (HASH)
    *   **Key**: `pool:minio:status` - MinIO连接池状态 (HASH)
    *   **Key**: `pool:health:last_check` - 最后健康检查时间 (STRING)
*   **一致性哈希环**:
    *   **Key**: `cluster:hash_ring`
    *   **Type**: `Sorted Set` or `String`
    *   **Usage**: 存储一致性哈希环的信息，用于写入时的分片路由。当节点增加或减少时，只需更新此数据结构。

#### f. 分布式存储层 (MinIO)

*   **角色**: 最终的数据湖（Data Lake），提供可靠、高可用的S3兼容对象存储。
*   **数据格式**: **Apache Parquet**。这是一个列式存储格式，压缩率高，非常适合OLAP场景。DuckDB对其有原生的高性能支持。
*   **存储架构**:
    *   **主存储集群**: 处理主要的读写请求，提供高性能访问。
    *   **备份存储集群**: 用于数据备份和灾难恢复，可以是地理位置分离的存储。
*   **数据组织**:
    *   **Bucket**: 可以按业务或租户划分，例如 `olap-data`、`olap-backup`。
    *   **Object Key (路径)**: 严格按照 `TABLE/ID/YYYY-MM-DD/` 的格式组织。例如：
        - `users/user-123/2024-01-15/1705123456789.parquet`
        - `orders/order-456/2024-01-15/1705123456790.parquet`
        - `logs/app-logs/2024-01-15/1705123456791.parquet`
*   **备份数据组织**:
    *   **元数据备份**: `metadata/backups/YYYY-MM-DD/backup_timestamp.json`
    *   **数据备份**: 与主存储相同的路径结构，但存储在备份bucket中

## 4. 核心流程分析

### 4.0. 部署模式选择

系统支持四种部署模式，通过配置文件和编译标签进行控制：

#### 4.0.1. 单节点模式（推荐用于开发/测试）
- **配置方式**: `redis.enabled: false`
- **适用场景**: 开发测试、资源受限环境、项目初期
- **优势**: 部署简单、无需Redis、资源占用少
- **组件需求**: 仅需MinIO + MinIODB服务

#### 4.0.2. 分布式模式（推荐用于生产）
- **配置方式**: `redis.enabled: true`
- **适用场景**: 生产环境、高可用需求、大规模数据处理
- **优势**: 高可用、水平扩展、负载均衡
- **组件需求**: Redis + MinIO集群 + MinIODB服务集群

#### 4.0.3. All-in-One模式（推荐用于快速体验）
- **配置方式**: `dashboard.enabled: true` + 编译标签`-tags dashboard`
- **适用场景**: 快速体验、演示环境、小型部署
- **优势**: 单二进制、单端口、包含Dashboard管理界面
- **组件需求**: 核心服务 + Dashboard（同一二进制）

#### 4.0.4. Dashboard独立模式（推荐用于大型生产）
- **配置方式**: 独立Dashboard服务，配置`core_endpoint`指向核心服务
- **适用场景**: 大型生产环境、多集群管理
- **优势**: Dashboard可独立扩展、不影响核心服务性能
- **组件需求**: 核心服务集群 + 独立Dashboard服务

### 4.1. 节点注册与发现

#### 4.1.1. 分布式模式下的节点注册与发现
1.  一个新的Go服务实例（Worker Node）启动。
2.  它连接到Redis，并在`nodes:services`哈希表中添加自己的`NodeID`和`IP:Port`，并设置一个TTL（如60秒）。
3.  该节点启动一个定时任务（如每30秒），重复上一步，以刷新TTL，这作为心跳机制。
4.  查询协调器需要查找可用节点时，只需从`nodes:services`读取所有成员即可。如果一个节点没有在TTL内续期，它会自动从哈希表中消失。

#### 4.1.2. 单节点模式下的服务处理
1.  MinIODB服务启动时检测到`redis.enabled: false`配置。
2.  自动跳过Redis连接池创建和服务注册流程。
3.  所有查询和写入操作直接路由到本地节点。
4.  服务发现返回当前本地节点信息，标记为单节点模式。
5.  无需心跳机制和分布式协调，简化了系统复杂度。

### 4.2. 表管理流程

#### 4.2.1. 分布式模式下的表管理
1. **创建表**: 客户端发送创建表请求，指定表名和配置参数。
2. **验证表名**: 系统验证表名的合法性（符合命名规范、不重复等）。
3. **存储元数据**: 在Redis中存储表的配置信息、创建时间等元数据。
4. **更新表列表**: 将新表名添加到`tables:list`集合中。
5. **初始化统计**: 为新表初始化统计信息（记录数、文件数、大小等）。

#### 4.2.2. 单节点模式下的表管理
1. **创建表**: 客户端发送创建表请求，指定表名和配置参数。
2. **验证表名**: 系统验证表名的合法性（符合命名规范、不重复等）。
3. **本地存储**: 在本地内存中维护表配置信息和元数据。
4. **本地索引**: 在本地维护表列表和统计信息。
5. **直接访问**: 所有表操作直接通过本地逻辑处理，无需Redis协调。

### 4.3. 表级数据写入与分片

#### 4.3.1. 分布式模式下的数据写入（完整流程）
1.  客户端向任意一个节点的API网关发送写入请求 `(TABLE, ID, Time, DataPayload)`。
2.  该节点（作为协调器）验证表的存在性和写入权限（JWT认证+角色权限检查）。
3.  从Redis获取`cluster:hash_ring`，根据表名和数据中的`ID`计算出应该处理此数据的Worker Node。
4.  协调器将请求转发给目标Worker Node。
5.  **WAL记录**: Worker Node先将数据写入Write Ahead Log，fsync到磁盘，CRC32校验，确保持久化。
6.  Worker Node根据表的配置在内存中缓冲数据（ConcurrentBuffer，20个并发worker）。
7.  当某个表的缓冲区满（默认5000条）或达到时间阈值（默认15秒），Worker Node将这批数据聚合并生成一个Parquet文件：
    - **列式存储**: 使用parquet-go编码，支持嵌套结构和复杂类型
    - **智能压缩**: 根据数据类型自动选择压缩算法（ZSTD/Snappy/GZIP/LZ4），压缩率5:1至10:1
    - **元数据索引**: 记录每列的min/max统计信息，用于后续文件剪枝
8.  Worker Node将Parquet文件上传到MinIO主存储池，路径为 `bucket-name/TABLE/ID/YYYY-MM-DD/nanotimestamp.parquet`。
9.  上传成功后，Worker Node更新Redis索引：`SADD index:table:{TABLE}:id:{ID}:{YYYY-MM-DD} "TABLE/ID/YYYY-MM-DD/nanotimestamp.parquet"`。
10. **自动备份**: 如果表配置启用备份（`backup_enabled: true`），异步复制Parquet文件到MinIO备份存储池。
11. **WAL清理**: 标记已成功写入的WAL条目为可删除，定期轮转和清理旧日志。
12. 更新表的统计信息，包括记录数、文件数、大小、最后写入时间等。
13. **性能监控**: 记录写入延迟到SLAMonitor，更新Prometheus指标（`miniodb_write_operations_total`）。

#### 4.3.2. 单节点模式下的数据写入
1.  客户端向MinIODB服务发送写入请求 `(TABLE, ID, Time, DataPayload)`。
2.  服务验证表的存在性和写入权限。
3.  直接路由到本地节点处理（无需一致性哈希计算）。
4.  本地节点根据表的配置在内存中缓冲数据。
5.  当某个表的缓冲区满或达到时间阈值，本地节点将这批数据聚合并生成一个Parquet文件。
6.  本地节点将Parquet文件上传到MinIO，路径为 `bucket-name/TABLE/ID/YYYY-MM-DD/nanotimestamp.parquet`。
7.  上传成功后，在本地维护文件索引信息（无需Redis）。
8.  更新本地表的统计信息，包括记录数、文件数、最后写入时间等。

### 4.4. 表级数据查询（优化流程）

#### 4.4.1. 分布式模式下的数据查询（完整优化栈）
1.  客户端向任意一个节点的API网关发送查询请求（例如，SQL: `SELECT name, age FROM users WHERE id = 'user-123' AND timestamp >= '2024-01-01' AND age > 20`）。
2.  **查询缓存检查**: QueryCache先检查Redis是否有缓存的查询结果（根据SQL+参数生成缓存键），如果命中直接返回，延迟<10ms。
3.  **SQL验证**: 协调器验证SQL语法和安全性（防止SQL注入），提取表名（如`users`），验证表的存在性和查询权限（JWT+角色权限）。
4.  **列剪枝**: ColumnPruner分析SQL，提取需要的列（`name`, `age`, `id`, `timestamp`），生成优化的Parquet读取视图，减少50-80%数据读取量。
5.  根据WHERE条件，生成需要查询的`{TABLE}`、`{ID}`和`{YYYY-MM-DD}`的组合。
6.  协调器并发地向Redis查询所有匹配的Key（如 `KEYS index:table:users:id:user-123:*`），并获取所有相关的Parquet文件路径列表。
7.  **文件剪枝**: FilePruner读取Parquet元数据（min/max统计），根据谓词（`age > 20`）跳过无关文件，减少90%+无效读取。
8.  **混合查询**: HybridQueryEngine检查内存缓冲区是否有符合条件的数据，将缓冲区数据和MinIO存储数据合并查询，数据可见延迟从15秒降到1-3秒。
9.  协调器获取`nodes:services`中的所有健康Worker节点，并制定查询计划（例如，平均分配文件列表，负载均衡）。
10. 协调器向每个Worker Node下发子查询任务，包含文件列表、优化的列列表和要执行的SQL语句。
11. 每个Worker Node执行查询优化流程：
    - **文件缓存检查**: FileCache先检查本地磁盘是否有缓存的Parquet文件，如果有直接读取，否则从MinIO下载。
    - **谓词下推**: DuckDB自动将WHERE条件下推到Parquet读取层，利用Row Group级别的统计信息跳过无关数据。
    - **列式读取**: 只读取需要的列（`name`, `age`, `id`, `timestamp`），减少IO。
    - **DuckDB查询**: 执行优化的SQL，例如：`SELECT name, age FROM read_parquet(['file1.parquet', 'file2.parquet'], columns=['name', 'age', 'id', 'timestamp']) WHERE age > 20`。
12. Worker Node将部分结果返回给协调器。
13. **分布式聚合**: 如果查询包含聚合（GROUP BY/COUNT/AVG），协调器使用Map-Reduce策略合并结果。
14. 协调器聚合所有结果，并返回给客户端。
15. **性能监控**: 记录查询延迟到SLAMonitor，更新Prometheus指标（P50/P95/P99延迟、缓存命中率、SLA合规率）。
16. **缓存写入**: QueryCache将查询结果写入Redis缓存，TTL根据查询类型智能调整（聚合查询30分钟、点查询5分钟）。

#### 4.4.2. 单节点模式下的数据查询
1.  客户端向MinIODB服务发送查询请求（例如，SQL: `SELECT * FROM users WHERE id = 'user-123' AND timestamp >= '2024-01-01'`）。
2.  服务解析SQL，提取表名（如`users`），验证表的存在性和查询权限。
3.  根据WHERE条件，生成需要查询的`{TABLE}`、`{ID}`和`{YYYY-MM-DD}`的组合。
4.  直接扫描MinIO路径`TABLE/ID/YYYY-MM-DD/`获取相关的Parquet文件路径列表（无需Redis索引）。
5.  本地DuckDB实例执行查询，例如：`SELECT * FROM read_parquet(['s3://olap-data/users/user-123/2024-01-01/file1.parquet', 's3://olap-data/users/user-123/2024-01-02/file2.parquet']) WHERE ...`。
6.  直接返回查询结果给客户端（无需结果聚合）。

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

### 4.7. 高可用与数据备份

MinIODB 采用**三层高可用体系**，各层独立保障，职责清晰。

```
┌─────────────────────────────────────────────────────────────────────┐
│                         三层高可用体系                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─── 应用层 ──────────────────────────────────────────────────┐   │
│  │  多 MinIODB 实例 + 负载均衡 (Nginx/HAProxy/K8s Service)    │   │
│  │  • 任一实例故障，流量自动切到其他实例                        │   │
│  │  • 水平扩展，增加实例即增加吞吐                             │   │
│  │  • 无状态设计 — 所有状态在 Redis/MinIO 中                   │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                              │                                      │
│  ┌─── 中间件层 ────────────────────────────────────────────────┐   │
│  │  Redis Sentinel 或 Redis Cluster                            │   │
│  │  • Sentinel: 主从复制 + 自动故障转移（秒级切换）             │   │
│  │  • Cluster: 分片 + 多副本 + 自动重平衡                      │   │
│  │  • MinIODB 的 RedisPool 已支持三种模式自动适配               │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                              │                                      │
│  ┌─── 存储层 ──────────────────────────────────────────────────┐   │
│  │  MinIO 纠删码集群 (Erasure Coding)                          │   │
│  │  • 4+ 节点部署，任意 N/2-1 节点故障仍可读写                 │   │
│  │  • 存储层自身保证数据冗余和高可用                            │   │
│  │  • MinIODB 无需在应用层做存储 failover                      │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌─── 容灾层（可选）───────────────────────────────────────────┐   │
│  │  Backup MinIO + 热备 Replicator                             │   │
│  │  • 异步增量同步到独立 Backup MinIO（异地容灾）               │   │
│  │  • 写入即时异步复制 (EnqueueSync) + 定期全量校验             │   │
│  │  • 用于灾难恢复，不用于实时故障切换                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

#### 4.7.1 应用层高可用：多 MinIODB 实例 + 负载均衡

MinIODB 的应用进程是**无状态**的 — 所有持久化状态存储在 Redis（元数据/索引）和 MinIO（数据文件）中。因此可以直接部署多个实例，通过负载均衡器分发请求：

```
                  ┌──────────────────────┐
                  │  Load Balancer       │
                  │  (Nginx/HAProxy/K8s) │
                  └──────┬───────────────┘
                         │
              ┌──────────┼──────────┐
              ▼          ▼          ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │MinIODB-1 │ │MinIODB-2 │ │MinIODB-3 │
        │ :8081    │ │ :8081    │ │ :8081    │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │             │             │
             └─────────────┼─────────────┘
                           │
                    ┌──────┴──────┐
                    │ Redis       │  ← 共享状态
                    │ MinIO       │  ← 共享存储
                    └─────────────┘
```

**要点**：
- 每个 MinIODB 实例持有相同配置，连接同一套 Redis 和 MinIO
- 写入请求通过一致性哈希路由到目标 Worker，多实例间通过 Redis 协调
- 查询请求可发到任一实例，由该实例协调分布式查询
- 任一实例宕机，负载均衡器自动摘除，其他实例继续服务
- 实例重启后自动重新注册到 Redis，恢复服务

#### 4.7.2 中间件层高可用：Redis Sentinel / Cluster

Redis 是 MinIODB 的元数据中心，其高可用直接影响系统可用性。MinIODB 的 `RedisPool` 已原生支持三种模式：

| 模式 | 配置 | 高可用能力 | 适用场景 |
|------|------|-----------|---------|
| **Standalone** | `redis.mode: standalone` | 无 — 单节点故障即不可用 | 开发/测试 |
| **Sentinel** | `redis.mode: sentinel` | 主从复制 + 自动故障转移（秒级切换），go-redis 自动发现新主节点 | 标准生产 |
| **Cluster** | `redis.mode: cluster` | 多分片 + 每分片多副本 + 自动重平衡，go-redis 自动路由 | 大规模生产 |

```yaml
# config.yaml — Sentinel 模式示例
redis:
  enabled: true
  mode: sentinel
  sentinel:
    master_name: "miniodb-master"
    addresses:
      - "sentinel-1:26379"
      - "sentinel-2:26379"
      - "sentinel-3:26379"
```

```yaml
# config.yaml — Cluster 模式示例
redis:
  enabled: true
  mode: cluster
  cluster:
    addresses:
      - "redis-1:6379"
      - "redis-2:6379"
      - "redis-3:6379"
      - "redis-4:6379"
      - "redis-5:6379"
      - "redis-6:6379"
```

**MinIODB 侧无需任何代码变更** — `RedisPool` 根据配置自动选择连接模式，go-redis 库处理 Sentinel 故障转移和 Cluster 分片路由。

#### 4.7.3 存储层高可用：MinIO 纠删码集群

**核心观点**：存储高可用应在存储层自身解决，而非应用层做 failover。

MinIO 纠删码（Erasure Coding）部署可在 N 个节点中容忍 N/2-1 个节点故障：

| 部署规模 | 节点数 | 容忍故障 | 存储效率 |
|---------|--------|---------|---------|
| 最小集群 | 4 | 1 节点 | 50% |
| 标准集群 | 8 | 3 节点 | 50% |
| 大型集群 | 16 | 7 节点 | 50% |

**MinIODB 侧只需将 MinIO 端点指向集群负载均衡地址**，无需代码修改：

```yaml
# config.yaml — MinIO 集群端点
minio:
  endpoint: "minio-cluster-lb:9000"   # 集群负载均衡地址
  access_key_id: "admin"
  secret_access_key: "password"
  use_ssl: false
  bucket: "miniodb-data"
```

**为什么不在应用层做存储 failover**：

当前 `FailoverManager` 已实现主池故障自动切换到备份池的机制，但经过审计发现此方案存在根本性缺陷（详见 `BACKUP_DESIGN.md` 第十章）：

- **数据不一致**：切到备池写入的数据，主池恢复后不会自动回写，导致数据丢失
- **索引断裂**：Redis 中的文件索引指向主池路径，切换后索引与实际文件位置不匹配
- **覆盖率不足**：当前只有 Buffer flush 和 StorageImpl.GetObject 走了 failover 路径，Querier 等核心读路径完全未保护

因此 `FailoverManager` 的定位调整为：
- **保留**：异步同步能力（`EnqueueSync`）— 写入后即时复制到备池做容灾副本
- **保留**：健康检查和 Prometheus 指标 — 监控基础设施
- **弱化**：自动切换逻辑 — 改为告警驱动的人工决策

#### 4.7.4 容灾：数据备份体系

在三层高可用之外，系统提供完整的数据备份能力用于灾难恢复（详见 `BACKUP_DESIGN.md`）：

##### a. 热备（主从复制）
- **机制**：独立 `MinIOPool` 实例（与主业务隔离的连接池）定期增量同步 Primary MinIO → Backup MinIO
- **性能隔离**：独立 `http.Transport`（MaxConnsPerHost=10），QPS 限流 + 自适应退让，对主业务 P99 延迟影响 ≤ 2%
- **数据完整性**：增量同步 + 定期全量校验，配合 `EnqueueSync` 的即时异步复制实现双重保障

##### b. 冷备（备份计划）
- **元数据备份**：`metadata.Manager` 支持定时/手动/增量/全量备份到 MinIO
- **全量数据备份**：复制所有表的 Parquet 文件到备份目标，生成 Manifest
- **表级数据备份**：按表粒度备份，支持选择性恢复
- **多计划调度**：cron 表达式驱动，多计划并行，独立保留策略

##### c. 优雅降级
- Backup MinIO 未配置时：热备停止，冷备回退到 Primary MinIO 的独立桶（`{bucket}-backups`）
- Dashboard 备份功能始终可用，降级模式下给出明确提示

#### 4.7.5 部署推荐

| 部署规模 | 应用层 | 中间件层 | 存储层 | 容灾 |
|---------|--------|---------|--------|------|
| **开发/测试** | 单实例 | Redis Standalone | 单节点 MinIO | 无 |
| **小型生产** | 2 实例 + LB | Redis Sentinel (3 节点) | 单节点 MinIO + Backup MinIO | 热备 |
| **标准生产** | 3+ 实例 + LB | Redis Sentinel (3 节点) | MinIO 4 节点纠删码 | 冷备计划 |
| **高可用生产** | 3+ 实例 + LB | Redis Cluster (6 节点) | MinIO 8+ 节点纠删码 + 异地 Backup MinIO | 热备 + 冷备 |

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

#### c. 故障检测与监控流程
1. **Redis故障处理**（由 Redis 自身高可用机制保障）:
   - **Sentinel 模式**: go-redis 自动感知主节点切换，应用层无需干预
   - **Cluster 模式**: go-redis 自动路由到健康分片，分片迁移时自动重试
   - **Standalone 模式**: 连接中断后按重试策略恢复

2. **MinIO故障处理**（由 MinIO 集群或告警驱动）:
   - **MinIO 集群部署**: 纠删码保证节点故障时读写不受影响，MinIODB 无感知
   - **MinIO 单节点部署**: `FailoverManager` 15 秒间隔检测健康状态，故障时通过 Prometheus 指标（`miniodb_pool_primary_healthy`）触发告警，人工决策是否启用备份池
   - **异步同步**: 主池写入后 `EnqueueSync` 即时复制到备份池，保证容灾副本实时性
   - **健康状态持久化**: 健康检查结果存储到 Redis，分布式节点共享

3. **连接池扩缩容**:
   - 根据负载情况动态调整连接池大小（`StartDynamicAdjustment`）
   - 高负载（>80% 使用率）时自动扩容
   - 低负载（<20% 使用率）时回收多余连接
   - 保证连接池大小在配置的最小值和最大值之间

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

> 元数据备份/恢复的高可用依赖三层体系（详见 4.7 节），以下是元数据层面的协调机制：

1. **多节点协调**:
   - 使用Redis分布式锁确保同一时间只有一个节点执行备份/恢复
   - 节点间通过Redis发布/订阅机制同步状态
   - 任一 MinIODB 实例都可接管备份/恢复任务（无状态设计）

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

4. **各层故障影响分析**:

| 故障场景 | 影响 | 恢复方式 |
|---------|------|---------|
| 单个 MinIODB 实例宕机 | 负载均衡器自动摘除，其他实例继续服务 | 实例重启后自动注册恢复 |
| Redis Sentinel 主节点故障 | Sentinel 秒级自动切换，MinIODB 短暂重连 | 自动恢复 |
| MinIO 集群单节点故障 | 纠删码保证读写不受影响 | MinIO 自动修复 |
| Redis 全部不可用 | 写入/查询中断（元数据不可达） | 恢复 Redis 后自动重连 |
| MinIO 全部不可用 | 写入/查询中断（存储不可达） | 恢复 MinIO 或从 Backup MinIO 灾备恢复 |

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

## 5. 备份体系设计

### 5.1 备份类型

MinIODB 提供完整的备份能力用于灾难恢复：

#### a. 热备（主从复制）

**机制**：独立数据库 MinIOPool 实例，定期增量同步 Primary MinIO → Backup MinIO

**性能隔离**：
- 独立 http.Transport（MaxConnsPerHost=10）
- QPS 限流 + 自适应退让
- 对主业务 P99 延迟影响 ≤ 2%

**数据完整性**：
- 增量同步 + 定期全量校验
- Checkpoint 存储在 Redis
- 与 `EnqueueSync` 互补（实时副本 + 定期校验）

**配置示例**：
```yaml
backup:
  replication:
    enabled: true
    interval: 3600
    workers: 4
    max_qps: 50
    max_conns_to_source: 10
    full_verify_interval: 24
```

#### b. 冷备（备份计划）

**元数据备份**：`metadata.Manager` 支持定时/手动备份到 MinIO

**全量数据备份**：复制所有表的 Parquet 文件到备份目标

**表级数据备份**：按表粒度备份，支持选择性恢复

**多计划调度**：cron 表达式驱动，多计划并行

#### c. 优雅降级

- Backup MinIO 未配置时：热备停止，冷备回退到 Primary MinIO 的独立桶（`{bucket}-backups`）
- Dashboard 备份功能始终可用，降级模式下给出明确提示

### 5.2 Standalone 模式元数据降级

当系统以 standalone 模式运行（`redisPool == nil`）时：

| 场景 | 降级行为 |
|------|---------|
| 表配置存储 | 写入 MinIO `_system/table_configs/{tableName}.json` |
| 元数据操作 | 返回"standalone mode"提示，不panic |
| 备份功能 | 使用 Primary MinIO 的独立桶 |

**元数据存储三级链路**：
```
Level 1（优先）→ Redis metadata:table_config:{name}
Level 2（降级）→ MinIO _system/table_configs/{name}.json
Level 3（兜底）→ 系统默认配置（snowflake + auto_generate=true）
```

## 6. 权限体系设计

### 6.1 两级权限体系

| 层级 | 名称 | 粒度 | 控制对象 |
|------|------|------|----------|
| Tier 1 | 功能权限 | API 端点级 | 控制用户能否访问某个 API |
| Tier 2 | 数据权限 | 表级 | 控制用户能否对某个表执行 CRUD |

### 6.2 角色定义

| 角色 | 标识符 | 说明 |
|------|--------|------|
| 管理员 | `admin` | 全部权限，可管理系统配置、表结构、备份等 |
| 普通用户 | `viewer` | 只读为主，可查看数据和监控信息，对特定表可配置写入权限 |

### 6.3 功能权限映射（部分）

| 端点 | Method | 资源 | 操作 | 最低角色 |
|------|--------|------|------|----------|
| `/tables` | POST | dashboard.tables | create | **admin** |
| `/tables/:name` | DELETE | dashboard.tables | delete | **admin** |
| `/tables/:name/data` | GET | dashboard.data | read | viewer |
| `/query` | POST | dashboard.query | execute | viewer |
| `/backups/metadata` | POST | dashboard.backups | create | **admin** |
| `/cluster/config` | PUT | dashboard.config | update | **admin** |

### 6.4 数据权限（表级别）

**权限判定流程**：
```
请求到达
│
├─ Tier 1: 功能权限检查 (中间件)
│   └─ admin → 通过 (bypass)
│   └─ viewer → 检查 是否在白名单
│
└─ Tier 2: 数据权限检查 (服务层)
    ├─ admin → 通过 (bypass)
    └─ 非 admin →
        ├─ 提取表名（URL参数/请求体/SQL解析）
        ├─ 查找权限配置（显式配置 > 角色默认）
        └─ 检查操作是否在 allowed_actions 中
```

### 6.5 配置示例

```yaml
auth:
  token_expiry: "24h"
  api_key_pairs:
    - key: "admin"
      secret: "$2a$10$..."
      role: "admin"
      display_name: "管理员"
    - key: "reader"
      secret: "$2a$10$..."
      role: "viewer"
      display_name: "只读用户"

  roles:
    admin:
      description: "管理员 - 全部权限"
      table_default: ["create", "read", "update", "delete"]
    viewer:
      description: "普通用户 - 默认只读"
      table_default: ["read"]

  table_permissions:
    - table: "events"
      permissions:
        viewer: ["create", "read"]
    - table: "metrics"
      permissions:
        viewer: ["create", "read"]
```

## 7. 常见问题 (FAQ)

### 安装与配置

**Q: 如何安装 MinIODB？**

A: 支持三种方式：
1. Docker Compose（推荐快速启动）
2. 从源码编译：`go build -o bin/miniodb cmd/main.go`
3. Docker 镜像：`docker pull miniodb/miniodb:latest`

**Q: 系统要求？**

A: 
- 最小配置（开发）：2 核 CPU，4GB 内存，100GB SSD
- 推荐配置（生产）：8+ 核 CPU，16GB+ 内存，500GB+ NVMe SSD

### 数据操作

**Q: 支持的 SQL 方言？**

A: 使用 DuckDB，支持 PostgreSQL SQL 子集，包括：
- SELECT, FROM, WHERE, GROUP BY, HAVING, ORDER BY
- JOINs（INNER, LEFT, RIGHT, FULL）
- 窗口函数
- 聚合函数（COUNT, SUM, AVG, MIN, MAX 等）
- 时间函数（DATE_TRUNC, NOW, INTERVAL 等）

**Q: 如何更新已有记录？**

A: MinIODB 针对 OLAP 优化，不支持传统 UPDATE。使用 DELETE + INSERT 模式。

### 性能

**Q: 写入吞吐量？**

A: 典型性能：
- 单条写入延迟 <10ms
- 批量写入 10,000+ records/second
- 可通过调整 buffer_size 和 flush_interval 优化

**Q: 查询延迟？**

A: 典型性能：
- 缓存命中 <10ms
- 缓存未命中 100ms - 1s（取决于查询复杂度）
- 大数据扫描 1s - 10s

**Q: 如何提升查询性能？**

A: 
1. 使用缓存（默认启用）
2. 在时间戳字段上过滤（分区键）
3. 使用 LIMIT 限制结果集
4. 选择合适的数据类型
5. 使用物化视图预聚合

### 分布式部署

**Q: 何时使用分布式部署？**

A: 当以下情况时考虑分布式：
- 单节点无法处理负载（查询延迟 >5s）
- 需要高可用（HA）
- 需要水平扩展
- 需要多数据中心复制

**Q: 节点如何通信？**

A: 通过：
- Redis：共享状态、协调、缓存
- 负载均衡器：分发请求
- Gossip 协议：节点发现和健康检查

### 故障排除

**Q: 服务无法启动？**

A: 检查：
1. 端口占用：`lsof -i :8080`
2. MinIO 连接：`curl http://localhost:9000/minio/health/live`
3. Redis 连接：`redis-cli ping`
4. 查看日志：`docker-compose logs -f miniodb`

**Q: 查询慢？**

A: 优化步骤：
1. 检查是否使用分区键（timestamp）
2. 添加 LIMIT 子句
3. 检查缓存命中率
4. 预聚合数据
