# MinIODB SDK Examples

This directory contains SDK examples for integrating with MinIODB's data subscription system in multiple programming languages.

## Overview

MinIODB supports data ingestion through multiple channels:

1. **Redis Streams** - Lightweight, real-time data streaming
2. **Kafka** - High-throughput, distributed messaging
3. **gRPC** - Direct API calls for low-latency writes

## Data Event Format

All SDKs use a unified data event format:

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

## Available SDKs

### Go SDK

```bash
cd go
go run main.go -mode redis -table user_events -count 10
go run main.go -mode kafka -kafka-brokers localhost:9092 -count 100 -batch
go run main.go -mode grpc -grpc-addr localhost:8080
```

### Python SDK

```bash
cd python
pip install -r requirements.txt
python miniodb_client.py --mode redis --table user_events --count 10
python miniodb_client.py --mode kafka --kafka-brokers localhost:9092 --batch
```

### Java SDK

```bash
cd java
mvn package
java -jar target/miniodb-sdk-example-1.0.0.jar -mode redis -count 10
java -jar target/miniodb-sdk-example-1.0.0.jar -mode kafka -kafka-brokers localhost:9092 -batch
```

Or run directly with Maven:

```bash
mvn compile exec:java -Dexec.args="-mode redis -count 10"
```

### Node.js SDK

```bash
cd nodejs
npm install
node miniodb-client.js --mode redis --table user_events --count 10
node miniodb-client.js --mode kafka --kafka-brokers localhost:9092 --batch
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

## Event Types

| Type | Description |
|------|-------------|
| `insert` | Insert a single record |
| `update` | Update an existing record |
| `delete` | Delete a record |
| `batch` | Insert multiple records in batch |

## Best Practices

1. **Use Batch Mode for High Volume**
   - Batch mode significantly improves throughput
   - Recommended for bulk data imports

2. **Set Appropriate Source**
   - Always set the `source` metadata field
   - Helps with debugging and data lineage

3. **Handle Errors Gracefully**
   - All SDKs include retry logic
   - Implement exponential backoff for production use

4. **Monitor Stream Lag**
   - Monitor Redis Stream length or Kafka consumer lag
   - Alert when lag exceeds acceptable thresholds

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
                                    │  Ingester   │
                                    └─────────────┘
                                           │
                                           ▼
                                    ┌─────────────┐
                                    │   Buffer    │
                                    │  (Memory)   │
                                    └─────────────┘
                                           │
                                           ▼
                                    ┌─────────────┐
                                    │   MinIO     │
                                    │  (Parquet)  │
                                    └─────────────┘
```

## License

MIT License
