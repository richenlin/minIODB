# MinIODB SDK Examples

This directory contains SDK examples for integrating with MinIODB in multiple programming languages.

## Overview

MinIODB provides multiple integration methods:

1. **Data Subscription** - Ingest data via Redis Streams or Kafka
2. **gRPC API** - Direct API calls for data operations, table management, etc.

## Available SDKs

### Go SDK

**Data Subscription (Redis/Kafka):**
```bash
cd go
go run main.go -mode redis -table user_events -count 10
go run main.go -mode kafka -kafka-brokers localhost:9092 -count 100 -batch
```

**gRPC API Client:**
```bash
cd go
go run api_client.go -addr localhost:8080
```

### Python SDK

**Data Subscription (Redis/Kafka):**
```bash
cd python
pip install -r requirements.txt
python miniodb_client.py --mode redis --table user_events --count 10
python miniodb_client.py --mode kafka --kafka-brokers localhost:9092 --batch
```

**gRPC API Client:**
```bash
cd python
pip install grpcio grpcio-tools
python api_client.py --addr localhost:8080
```

### Java SDK

**Data Subscription (Redis/Kafka):**
```bash
cd java
mvn package
java -jar target/miniodb-sdk-example-1.0.0.jar -mode redis -count 10
java -jar target/miniodb-sdk-example-1.0.0.jar -mode kafka -kafka-brokers localhost:9092 -batch
```

**gRPC API Client:**
```bash
cd java
mvn compile exec:java -Dexec.mainClass="com.miniodb.sdk.MinioDBGrpcClient" -Dexec.args="-addr localhost:8080"
```

Or build and run:
```bash
mvn package
java -jar target/miniodb-sdk-example-1.0.0.jar com.miniodb.sdk.MinioDBGrpcClient -addr localhost:8080
```

### Node.js SDK

**Data Subscription (Redis/Kafka):**
```bash
cd nodejs
npm install
node miniodb-client.js --mode redis --table user_events --count 10
node miniodb-client.js --mode kafka --kafka-brokers localhost:9092 --batch
```

**gRPC API Client:**
```bash
cd nodejs
npm install @grpc/grpc-js @grpc/proto-loader
node api_client.js --addr localhost:8080
```

## API Operations

The gRPC API client examples demonstrate the following operations:

### Data Operations
- `WriteData` - Write a single record
- `QueryData` - Execute SQL queries
- `UpdateData` - Update existing records
- `DeleteData` - Delete records
- `StreamWrite` - Batch write via streaming
- `StreamQuery` - Batch query via streaming

### Table Management
- `CreateTable` - Create a new table with configuration
- `ListTables` - List all tables (with pattern matching)
- `GetTable` - Get detailed table information and statistics
- `DeleteTable` - Delete a table

### Metadata Management
- `BackupMetadata` - Create a metadata backup
- `RestoreMetadata` - Restore from a backup
- `ListBackups` - List available backups

### Health & Monitoring
- `HealthCheck` - Check system health status
- `GetStatus` - Get comprehensive system status

## Data Event Format

All subscription SDKs use a unified data event format:

```json
{
  "event_id": "evt_abc123",
  "event_type": "insert",
  "table": "user_events",
  "timestamp": "2024-01-18T10:30:00Z",
  "records": [
    {
      "id": "rec_001",
      "timestamp": 1705570200000,
      "payload": {
        "user_id": 123,
        "action": "login"
      }
    }
  ],
  "metadata": {
    "source": "my-app",
    "priority": "normal"
  }
}
```

## Configuration

### Redis Streams

| Parameter | Default | Description |
|-----------|---------|-------------|
| host | localhost | Redis server host |
| port | 6379 | Redis server port |
| password | - | Redis password (optional) |
| stream_prefix | miniodb:stream: | Stream key prefix |

### Kafka

| Parameter | Default | Description |
|-----------|---------|-------------|
| brokers | localhost:9092 | Kafka broker addresses |
| topic_prefix | miniodb- | Topic name prefix |

### gRPC

| Parameter | Default | Description |
|-----------|---------|-------------|
| addr | localhost:8080 | gRPC server address |

## Example Usage

### Go

```go
import "minIODB/api/proto/miniodb/v1"

client, _ := NewMinioDBClient("localhost:8080")
defer client.Close()

// Write data
client.WriteData(ctx, "users", "user_1", map[string]interface{}{
    "name": "John Doe",
    "email": "john@example.com",
})

// Query data
result, _ := client.QueryData(ctx, "SELECT * FROM users LIMIT 10", 10)
```

### Python

```python
client = MinioDBClient('localhost:8080')

# Write data
client.write_data('users', 'user_1', {
    'name': 'John Doe',
    'email': 'john@example.com'
})

# Query data
result = client.query_data('SELECT * FROM users LIMIT 10', 10)
```

### Java

```java
MinioDBGrpcClient client = new MinioDBGrpcClient("localhost:8080");

try {
    // Write data
    Map<String, Object> payload = new HashMap<>();
    payload.put("name", "John Doe");
    payload.put("email", "john@example.com");
    client.writeData("users", "user_1", payload);

    // Query data
    String result = client.queryData("SELECT * FROM users LIMIT 10", 10);
} finally {
    client.shutdown();
}
```

### Node.js

```javascript
const client = new MinioDBGrpcClient('localhost:8080');

// Write data
await client.writeData('users', 'user_1', {
    name: 'John Doe',
    email: 'john@example.com'
});

// Query data
const result = await client.queryData('SELECT * FROM users LIMIT 10', 10);
```

## Best Practices

1. **Use Batch Mode for High Volume**
   - Batch mode significantly improves throughput
   - Recommended for bulk data imports

2. **Handle Errors Gracefully**
   - All SDKs include retry logic
   - Implement exponential backoff for production use

3. **Set Appropriate Source**
   - Always set `source` metadata field for subscription events
   - Helps with debugging and data lineage

4. **Monitor Stream Lag**
   - Monitor Redis Stream length or Kafka consumer lag
   - Alert when lag exceeds acceptable thresholds

5. **Use Connection Pooling**
   - Reuse gRPC connections
   - Close connections when done

## Architecture

```
Your Application
      │
      ├─── Redis Streams ──────────────────┐
      │    (miniodb:stream:{table})        │
      │                                    │
      ├─── Kafka ──────────────────────────┤
      │    (miniodb-{table})               │
      │                                    │
      └─── gRPC API ───────────────────────┘
                                           │
                                           ▼
                                    ┌─────────────┐
                                    │  MinIODB    │
                                    │   Service   │
                                    └─────────────┘
                                           │
                              ┌────────────┴────────────┐
                              │                         │
                              ▼                         ▼
                       ┌──────────┐              ┌──────────┐
                       │ Ingester │              │ Querier  │
                       └──────────┘              └──────────┘
                              │                         │
                              ▼                         ▼
                       ┌──────────┐              ┌──────────┐
                       │  Buffer  │              │  DuckDB  │
                       └──────────┘              └──────────┘
                              │
                              ▼
                       ┌──────────┐
                       │   MinIO  │
                       │ (Parquet)│
                       └──────────┘
```

## License

MIT License
