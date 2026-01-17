# MinIODB Architecture and Business Logic Analysis

## Project Overview

MinIODB is a distributed OLAP (Online Analytical Processing) database system built on:
- **MinIO** - S3-compatible object storage for Parquet files
- **DuckDB** - Embedded analytical database for query execution
- **Redis** - Caching, distributed coordination, and message queuing
- **Go** - Core implementation language

**Total Codebase**: ~19,500 lines of Go code

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Client Layer                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │   gRPC   │  │   REST   │  │   Redis  │  │  Kafka   │          │
│  │   API    │  │   API    │  │  Streams │  │   MQ     │          │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘          │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Transport Layer                              │
│  ┌──────────────────┐              ┌──────────────────┐               │
│  │   gRPC Server   │              │   REST Server    │               │
│  │   (port 8080)   │              │   (port 8081)   │               │
│  └──────────────────┘              └──────────────────┘               │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Service Layer                                   │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                     MinIODBService                                 │  │
│  │  - WriteData()     - QueryData()    - UpdateData()   - DeleteData()│  │
│  │  - StreamWrite()   - StreamQuery() - CreateTable()   - ListTables()│  │
│  │  - Backup/Restore Metadata                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                        │                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐           │
│  │   Ingester   │   │   Querier    │   │  Table Manager   │           │
│  └──────────────┘   └──────────────┘   └──────────────────┘           │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                    ┌───────────────────┼───────────────────┐
                    │                   │                   │
                    ▼                   ▼                   ▼
         ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
         │   Buffer Layer   │  │  Cache Layer    │  │ Metadata Layer   │
         │                 │  │                 │  │                 │
         │ ┌─────────────┐ │  │  Redis Cluster  │  │ Redis Metadata   │
         │ │ Memory Buffer│ │  │                 │  │ + MinIO Backup   │
         │ └─────────────┘ │  │                 │  │                 │
         └──────────────────┘  └──────────────────┘  └──────────────────┘
                    │
                    ▼
         ┌──────────────────┐
         │    Storage Layer │
         │                 │
         │ ┌─────────────┐ │
         │ │     WAL     │ │  (Write-Ahead Log)
         │ └─────────────┘ │
         └──────────────────┘
                    │
                    ▼
         ┌──────────────────┐
         │   MinIO / S3    │  (Parquet Files)
         └──────────────────┘
```

## Core Components

### 1. Ingester (Data Ingestion)

**Location**: `internal/ingest/`

**Responsibility**: Handle incoming write requests and persist them

**Workflow**:
```
1. Receive Write Request (gRPC/REST/Redis/Kafka)
   │
2. Validate Request
   ├─ Check table exists
   ├─ Validate ID format
   └─ Validate payload
   │
3. Write to WAL (Write-Ahead Log)
   ├─ Persist to Redis list
   └─ Acknowledge to client
   │
4. Buffer in Memory
   ├─ Group records by table
   └─ Track buffer size
   │
5. Flush to MinIO (Triggered by)
   ├─ Buffer size threshold
   ├─ Time interval (e.g., 60s)
   └─ Manual flush
   │
6. Convert to Parquet
   ├─ Collect records
   ├─ Create Parquet file
   └─ Upload to MinIO
   │
7. Update Metadata
   ├─ Record file location
   ├─ Update statistics
   └─ Clear WAL entries
```

**Key Features**:
- Asynchronous write (WAL first, then flush)
- Buffer-based batching for high throughput
- Automatic compaction of small files
- Support for multiple data formats

### 2. Querier (Query Execution)

**Location**: `internal/query/`

**Responsibility**: Execute analytical queries on stored data

**Workflow**:
```
1. Receive Query Request
   │
2. Validate and Sanitize SQL
   ├─ Check for injection
   ├─ Replace "table" keyword
   └─ Apply query limits
   │
3. Query Planning
   ├─ Parse SQL
   ├─ Identify required tables
   └─ Determine data files
   │
4. Load Data
   ├─ Check Redis cache first
   ├─ Download Parquet files from MinIO
   └─ Load into DuckDB
   │
5. Execute Query
   ├─ Use DuckDB for execution
   ├─ Apply aggregations
   └─ Apply filters
   │
6. Return Results
   ├─ Format as JSON
   └─ Update cache
```

**Key Features**:
- SQL support (subset of DuckDB SQL)
- Result caching in Redis
- Query optimization
- Support for complex aggregations

### 3. Coordinator (Distributed Coordination)

**Location**: `internal/coordinator/`

**Responsibility**: Coordinate operations in distributed mode

**Workflow**:
```
1. Node Discovery
   ├─ Register node in Redis
   ├─ Heartbeat mechanism
   └─ Node status tracking
   │
2. Query Routing
   ├─ Analyze query requirements
   ├─ Route to relevant nodes
   └─ Aggregate results
   │
3. Write Distribution
   ├─ Distribute writes based on partitioning
   ├─ Consistent hashing
   └─ Replication handling
```

**Key Features**:
- Automatic node discovery
- Load balancing
- Fault tolerance
- Data partitioning

### 4. Storage Manager

**Location**: `internal/storage/`

**Responsibility**: Manage MinIO storage and file operations

**Workflow**:
```
1. File Upload
   ├─ Create Parquet file
   ├─ Compress if enabled
   └─ Upload to MinIO
   │
2. File Download
   ├─ Cache in local temp
   ├─ Download from MinIO
   └─ Keep in cache for TTL
   │
3. File Management
   ├─ List files by table
   ├─ Delete old files (TTL)
   └─ Prune duplicate data
```

**Key Features**:
- MinIO abstraction layer
- File caching
- Automatic retention cleanup
- Parquet format support

### 5. Metadata Manager

**Location**: `internal/metadata/`

**Responsibility**: Manage table and file metadata

**Workflow**:
```
1. Table Metadata
   ├─ Create/Delete tables
   ├─ Table configuration
   └─ Table statistics
   │
2. File Metadata
   ├─ Track Parquet files
   ├─ Record file locations
   └─ Update file status
   │
3. Backup & Recovery
   ├─ Periodic metadata backup
   ├─ Restore from backup
   └─ Disaster recovery
```

**Key Features**:
- Redis-based metadata storage
- MinIO backup for persistence
- Automatic backup scheduling
- Recovery mechanisms

### 6. WAL (Write-Ahead Log)

**Location**: `internal/wal/`

**Responsibility**: Ensure data durability before persisting

**Workflow**:
```
1. Write to WAL
   ├─ Append to Redis list
   ├─ Include timestamp
   └─ Acknowledge immediately
   │
2. Read from WAL
   ├─ Read unprocessed entries
   ├─ Retry failed operations
   └─ Clear on success
```

**Key Features**:
- Durable logging
- Failure recovery
- At-least-once semantics

### 7. Subscription Manager

**Location**: `internal/subscription/`

**Responsibility**: Handle data subscription and event publishing

**Workflow**:
```
1. Event Creation
   ├─ On write/update/delete
   ├─ Create DataEvent
   └─ Add metadata
   │
2. Event Publishing
   ├─ Publish to Redis Streams
   ├─ Publish to Kafka
   └─ Acknowledge success
```

**Key Features**:
- Redis Streams support
- Kafka support
- Event filtering
- Multiple subscriber support

## Data Flow

### Write Data Flow

```
Client
  │
  ├─ gRPC WriteData()
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │  Validation    │  Check table, ID format, payload
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │     WAL        │  Write to Redis list
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │    Buffer      │  Group by table, track size
  │ └─────────────────┘
  │   │
  │   ▼ (Trigger)
  │ ┌─────────────────┐
  │ │   Parquet      │  Convert to Parquet format
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │    MinIO       │  Upload object
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │   Metadata     │  Record file, update stats
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │  Subscription  │  Publish event (async)
  │ └─────────────────┘
  │
  └─> Response to Client
```

### Query Data Flow

```
Client
  │
  ├─ gRPC QueryData()
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │   Sanitize SQL  │  Prevent injection
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │   Cache Check  │  Check Redis for cached result
  │ └─────────────────┘
  │   │
  │   ├─ Cache Hit ──> Return cached result
  │   │
  │   ▼ Cache Miss
  │ ┌─────────────────┐
  │ │  Get Files      │  Query metadata for data files
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │ Download Files  │  Download from MinIO (if not cached)
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │   DuckDB       │  Load Parquet, execute query
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │  Format Result │  Convert to JSON
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │  Cache Result  │  Store in Redis
  │ └─────────────────┘
  │   │
  │   ▼
  │ ┌─────────────────┐
  │ │  Update Stats  │  Track query metrics
  │ └─────────────────┘
  │
  └─> Response to Client
```

## Key Design Decisions

### 1. Why MinIO + DuckDB?

**MinIO**:
- S3-compatible, cloud-native
- Excellent for storing large Parquet files
- Cost-effective object storage
- Distributed by default

**DuckDB**:
- Embedded analytical engine
- Excellent query performance
- Columnar storage optimized
- SQL-compatible

### 2. Why Redis?

- Fast in-memory caching
- Distributed coordination
- Message queue (Streams)
- Metadata storage
- Pub/Sub for events

### 3. Why Parquet?

- Columnar storage format
- Compression efficient
- Fast for analytical queries
- Widely supported

### 4. Write Path: WAL + Buffer

- **WAL**: Ensures durability
- **Buffer**: Batches writes for efficiency
- **Parquet**: Optimized for analytics

### 5. Query Path: Cache + DuckDB

- **Cache**: Speed up repeated queries
- **DuckDB**: Fast in-memory query
- **MinIO**: Persistent storage

## Concurrency Model

### Goroutine Usage

1. **Main Goroutine**: gRPC/REST server
2. **Worker Pool**: Buffer flushing (configurable)
3. **Async Tasks**: Subscription publishing
4. **Background Tasks**: Backup, compaction, TTL cleanup

### Synchronization

- **Redis**: Distributed locking
- **Channels**: Go communication
- **Context**: Cancellation and timeout

## Error Handling

### Error Levels

1. **Retryable**: Network issues, temporary failures
2. **Fatal**: Configuration errors, storage unavailability
3. **Warning**: Non-critical issues (e.g., cache miss)

### Retry Strategy

- Exponential backoff
- Max retry count (configurable)
- Circuit breaker pattern

## Performance Characteristics

### Write Performance

- **Throughput**: Depends on buffer size and flush interval
- **Latency**: ~1-10ms (WAL write)
- **Batch Size**: Configurable (default: 1000 records)

### Query Performance

- **Latency**: Depends on data size and query complexity
- **Cache Hit**: <10ms
- **Cache Miss**: 100ms - 1s (depends on data)
- **Throughput**: ~100-1000 queries/sec

### Storage Efficiency

- **Compression**: Parquet compression (snappy/gzip)
- **Compression Ratio**: 5-10x (typical)
- **Storage Cost**: S3-compatible storage pricing

## Security

### Authentication

- API key based (gRPC metadata)
- JWT tokens (optional)
- Redis password protection

### Authorization

- Table-level access control
- Query permission checks
- SQL injection prevention

## Scalability

### Horizontal Scaling

- Add more nodes (coordinator handles)
- Automatic data distribution
- Load balancing

### Vertical Scaling

- Increase buffer size
- Add more worker goroutines
- Increase Redis memory

## Monitoring

### Metrics Tracked

- Write latency
- Query latency
- Cache hit rate
- Buffer size
- File count
- Node health

### Health Checks

- Redis connectivity
- MinIO connectivity
- DuckDB health
- Disk space
- Memory usage

## Summary

MinIODB provides:
1. **Fast writes** via WAL + buffer
2. **Fast queries** via DuckDB + caching
3. **Durability** via WAL + MinIO backup
4. **Scalability** via distributed architecture
5. **Flexibility** via multiple ingestion methods
6. **Reliability** via backup/recovery
