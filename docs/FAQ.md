# MinIODB FAQ

## Installation & Setup

### Q: How do I install MinIODB?

A: You can install MinIODB using several methods:

1. **Docker Compose** (Recommended for quick start):
   ```bash
   git clone https://github.com/yourorg/miniodb.git
   cd miniodb
   docker-compose up -d
   ```

2. **Build from source**:
   ```bash
   git clone https://github.com/yourorg/miniodb.git
   cd miniodb
   go build -o bin/miniodb cmd/main.go
   ```

3. **Docker Image**:
   ```bash
   docker pull miniodb/miniodb:latest
   ```

See [Single-Node Deployment Guide](SINGLE_NODE_DEPLOYMENT.md) for details.

### Q: What are the system requirements?

A: 

**Minimum (Development)**:
- 2 CPU cores
- 4GB RAM
- 100GB SSD

**Recommended (Production)**:
- 8+ CPU cores
- 16GB+ RAM
- 500GB+ NVMe SSD

For distributed deployments, see [Distributed Deployment Guide](DISTRIBUTED_DEPLOYMENT.md).

### Q: Can I run MinIODB on Windows?

A: Yes, but it's recommended for development only. Use WSL2 for better Linux compatibility. Production deployments should use Linux.

### Q: How do I upgrade to a newer version?

A: 

**Docker**:
```bash
docker-compose pull
docker-compose up -d
```

**Binary**:
```bash
# Backup metadata first
curl -X POST http://localhost:8081/api/v1/metadata/backup

# Stop service
sudo systemctl stop miniodb

# Replace binary
sudo cp new-miniodb /usr/local/bin/miniodb

# Start service
sudo systemctl start miniodb
```

## Data Operations

### Q: How do I write data to MinIODB?

A: You can write data via multiple methods:

**gRPC**:
```bash
# See examples/sdk/go/api_client.go
go run examples/sdk/go/api_client.go
```

**REST API**:
```bash
curl -X POST http://localhost:8081/api/v1/data/write \
  -H "Content-Type: application/json" \
  -d '{
    "table": "my_table",
    "data": {
      "id": "record_001",
      "payload": {"field1": "value1"}
    }
  }'
```

**Redis Streams**:
```bash
# See examples/sdk/python/miniodb_client.py
python examples/sdk/python/miniodb_client.py --mode redis
```

**Kafka**:
```bash
# See examples/sdk/java/main.go
java -jar examples/sdk/java/target/miniodb-sdk.jar -mode kafka
```

### Q: How do I query data?

A: Use SQL queries via the Query API:

```bash
curl -X POST http://localhost:8081/api/v1/data/query \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT * FROM my_table WHERE timestamp >= NOW() - INTERVAL '7 days' LIMIT 100",
    "limit": 100
  }'
```

MinIODB supports DuckDB SQL syntax. See [OLAP Usage Scenarios](OLAP_SCENARIOS.md) for query examples.

### Q: What SQL dialect does MinIODB support?

A: MinIODB uses DuckDB, which supports a subset of PostgreSQL SQL. Common features:

- SELECT, FROM, WHERE, GROUP BY, HAVING, ORDER BY
- JOINs (INNER, LEFT, RIGHT, FULL)
- Window functions
- Aggregations (COUNT, SUM, AVG, MIN, MAX, STDDEV, etc.)
- Time functions (DATE_TRUNC, NOW, INTERVAL, etc.)
- JSON operators

See [OLAP Usage Scenarios](OLAP_SCENARIOS.md) for query examples.

### Q: Can I update existing records?

A: MinIODB is optimized for OLAP workloads and doesn't support traditional UPDATE operations. Use the DELETE + INSERT pattern:

```bash
# 1. Delete old record
curl -X DELETE http://localhost:8081/api/v1/data/my_table/record_001

# 2. Insert new record
curl -X POST http://localhost:8081/api/v1/data/write \
  -H "Content-Type: application/json" \
  -d '{
    "table": "my_table",
    "data": {
      "id": "record_001",
      "payload": {"field1": "new_value"}
    }
  }'
```

### Q: How is data stored?

A: MinIODB stores data as:

1. **WAL (Write-Ahead Log)**: In Redis for durability
2. **Buffer**: In memory for batching
3. **Parquet Files**: In MinIO for persistence

Data is automatically converted from the buffer to Parquet files when:
- Buffer size threshold is reached
- Time interval elapses
- Manual flush is triggered

## Performance

### Q: What is the write throughput?

A: Write throughput depends on:

- **Configuration**: Buffer size and flush interval
- **Hardware**: CPU, RAM, disk I/O
- **Network**: Latency to MinIO and Redis

**Typical performance**:
- Single write: <10ms latency
- Batch write: 10,000+ records/second
- Throughput scales with buffer size

### Q: What is the query latency?

A: Query latency depends on:

- **Data size**: Number of records and files
- **Query complexity**: Filters, joins, aggregations
- **Cache hit rate**: Cached queries return in <10ms

**Typical performance**:
- Cache hit: <10ms
- Cache miss: 100ms - 1s (depends on query)
- Large scans: 1s - 10s

### Q: How do I improve query performance?

A: 

1. **Use caching** (enabled by default)
2. **Filter on timestamp** (partition key)
3. **Limit result sets**: Use LIMIT clause
4. **Use appropriate data types**: Use INTEGER instead of STRING for IDs
5. **Index commonly queried fields**: (if supported)
6. **Use materialized views**: Pre-aggregate data

See [OLAP Usage Scenarios](OLAP_SCENARIOS.md) for optimization tips.

### Q: How do I monitor performance?

A: MinIODB exposes Prometheus metrics at `http://localhost:9090/metrics`.

Key metrics:
- `miniodb_write_latency_seconds`
- `miniodb_query_latency_seconds`
- `miniodb_cache_hit_rate`
- `miniodb_buffer_size`
- `miniodb_query_count_total`

Use Grafana dashboard: `deploy/grafana/miniodb-dashboard.json`

## Configuration

### Q: How do I configure MinIODB?

A: Configuration can be provided via:

1. **YAML file**: `/etc/miniodb/config.yaml`
2. **Environment variables**: `MINIODB_*`
3. **Command-line flags**: `--config file.yaml`

Example:
```yaml
server:
  node_id: "node-1"
  grpc_port: ":8080"
  rest_port: ":8081"

storage:
  minio:
    endpoint: "localhost:9000"
    access_key: "minioadmin"
    secret_key: "minioadmin"
    bucket: "miniodb-data"

redis:
  host: "localhost"
  port: 6379
```

### Q: How do I change the buffer size?

A: Set in config.yaml:

```yaml
buffer:
  size: 10000  # Number of records
  flush_interval: 60  # Seconds
```

Larger buffer = higher throughput, higher memory usage.

### Q: How do I enable/disable caching?

A: Set in config.yaml:

```yaml
query:
  cache_enabled: true
  cache_ttl: 3600  # Seconds
  cache_size: 1000
```

### Q: How do I configure data retention?

A: Set per-table or globally:

```yaml
tables:
  default_config:
    retention_days: 90  # Delete data older than 90 days
```

## Storage & Backup

### Q: Where is data stored?

A: Data is stored in MinIO as Parquet files:

- **Default bucket**: `miniodb-data`
- **File format**: Parquet (columnar, compressed)
- **File path pattern**: `<table>/year/month/day/<timestamp>.parquet`

### Q: How do I backup data?

A: MinIODB provides two backup types:

1. **Metadata Backup**: Automatic, stores table and file metadata
   ```bash
   curl -X POST http://localhost:8081/api/v1/metadata/backup
   ```

2. **MinIO Backup**: MinIO provides object storage backup
   ```bash
   mc alias set local http://localhost:9000 minioadmin minioadmin
   mc mirror miniodb-data /backup/location
   ```

### Q: How do I restore from backup?

A: 

**Metadata restore**:
```bash
curl -X POST http://localhost:8081/api/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{"from_latest": true}'
```

**MinIO restore**:
```bash
   mc mirror /backup/location miniodb-data
```

### Q: What happens to data older than retention_days?

A: Data is automatically deleted when:

- Compaction runs
- TTL check passes
- File is older than `retention_days`

Data in MinIO is deleted; metadata in Redis is updated.

## Distributed Deployment

### Q: When should I use distributed deployment?

A: Consider distributed deployment when:

- Single node can't handle load (query latency >5s)
- Need high availability (HA)
- Need horizontal scaling
- Need multi-datacenter replication

See [Distributed Deployment Guide](DISTRIBUTED_DEPLOYMENT.md).

### Q: How do nodes communicate in distributed mode?

A: Nodes communicate via:

- **Redis**: Shared state, coordination, cache
- **Load balancer**: Distributes requests
- **Gossip protocol**: Node discovery and health

### Q: What happens if a node fails?

A: 

1. **Health check detects failure**
2. **Load balancer removes failed node**
3. **Remaining nodes handle requests**
4. **Failed node is restarted automatically**
5. **Data replication** (if configured) ensures no data loss

### Q: How do I add more nodes?

A: 

**Docker Swarm**:
```bash
docker service scale miniodb_miniodb=5
```

**Kubernetes**:
```bash
kubectl scale statefulset miniodb --replicas=5
```

## Troubleshooting

### Q: Service won't start

A: Check common issues:

1. **Port already in use**:
   ```bash
   sudo lsof -i :8080
   sudo kill -9 <PID>
   ```

2. **MinIO connection failed**:
   ```bash
   curl http://localhost:9000/minio/health/live
   # Verify MinIO is running
   ```

3. **Redis connection failed**:
   ```bash
   redis-cli ping
   # Verify Redis is running
   ```

4. **Check logs**:
   ```bash
   docker-compose logs -f miniodb
   # or
   journalctl -u miniodb -f
   ```

### Q: Write operation failed

A: Common causes:

1. **Invalid table name**: Table doesn't exist or auto_create disabled
2. **Invalid ID format**: ID doesn't match validation regex
3. **MinIO error**: Storage issue (disk full, network)
4. **Redis error**: Cache/storage issue

Check logs for detailed error messages.

### Q: Query is slow

A: Optimization steps:

1. **Check if query uses partition key** (timestamp)
2. **Add LIMIT clause** to reduce result set
3. **Check cache hit rate**: Low cache hit means slow queries
4. **Review query plan**: Explain query execution
5. **Add indexes** (if supported)
6. **Pre-aggregate data**: Create materialized views

### Q: Out of memory error

A: Solutions:

1. **Reduce buffer size**:
   ```yaml
   buffer:
     size: 5000  # Was 10000
   ```

2. **Increase swap space**:
   ```bash
   sudo fallocate -l 4G /swapfile
   sudo chmod 600 /swapfile
   sudo mkswap /swapfile
   sudo swapon /swapfile
   ```

3. **Add more RAM**

4. **Use streaming queries** instead of loading all data

### Q: Data not showing up in queries

A: Check:

1. **Buffer flush**: Data may still be in memory buffer
   ```bash
   # Manual flush (if supported)
   curl -X POST http://localhost:8081/api/v1/admin/flush
   ```

2. **Wait for flush interval**: Default is 60 seconds

3. **Check WAL**: Write succeeded but buffer flush failed
   ```bash
   # Check MinIODB logs
   ```

4. **Query cache**: Old cached results
   ```bash
   # Clear cache (if supported)
   curl -X POST http://localhost:8081/api/v1/admin/cache/clear
   ```

## Security

### Q: How do I secure MinIODB?

A: Security best practices:

1. **Enable API key authentication**:
   ```yaml
   server:
     auth_enabled: true
     api_keys:
       - your-secure-api-key
   ```

2. **Enable TLS/SSL**:
   ```yaml
   server:
     tls_enabled: true
     tls_cert_file: "/etc/miniodb/cert.pem"
     tls_key_file: "/etc/miniodb/key.pem"
   ```

3. **Use firewall**:
   ```bash
   sudo ufw allow 8080/tcp
   sudo ufw allow 8081/tcp
   sudo ufw enable
   ```

4. **Secure Redis**: Enable password and bind to localhost

5. **Secure MinIO**: Enable TLS and access controls

### Q: How do I set up API keys?

A: 

**In config.yaml**:
```yaml
server:
  auth_enabled: true
  api_keys:
    - production-key-1
    - production-key-2
    - development-key-3
```

**Use in requests**:
```bash
curl -H "X-API-Key: production-key-1" \
  http://localhost:8081/api/v1/tables
```

### Q: How do I enable audit logging?

A: Set in config.yaml:

```yaml
server:
  audit_log_enabled: true
  audit_log_file: "/var/log/miniodb/audit.log"
  audit_log_level: "INFO"
```

## Integration

### Q: How do I integrate with my application?

A: Multiple integration methods:

1. **gRPC SDK**: Examples in `examples/sdk/go/`
2. **REST API**: Use HTTP client in your language
3. **Redis Streams**: Real-time event streaming
4. **Kafka**: Event streaming for high throughput

See [API Documentation](API.html) for API reference and SDK examples.

### Q: Which SDK should I use?

A: Choose based on your needs:

- **Go SDK**: Best performance, gRPC + REST + Redis + Kafka
- **Python SDK**: Easy integration, popular ML/Data Science ecosystem
- **Java SDK**: Enterprise Java applications
- **Node.js SDK**: Web applications, JavaScript/TypeScript

### Q: Can I use MinIODB with [Tool X]?

A: MinIODB is compatible with any tool that can:

- Make HTTP REST API calls
- Use gRPC (protobuf)
- Connect to Redis
- Connect to Kafka

Popular integrations:
- **BI Tools**: Tableau, Power BI, Superset (via SQL)
- **ETL Tools**: Apache Airflow, Apache NiFi
- **Stream Processing**: Apache Flink, Apache Spark
- **Data Science**: pandas, R, Jupyter (via SQL)

## Miscellaneous

### Q: What is the difference between MinIODB and MinIO?

A: 

- **MinIO**: S3-compatible object storage (files, images, videos)
- **MinIODB**: OLAP database (analytics, queries, aggregations)

MinIODB uses MinIO as storage backend, but adds:
- Query engine (DuckDB)
- Caching (Redis)
- SQL interface
- Data management APIs

### Q: Can I use MinIODB for transactional (OLTP) workloads?

A: MinIODB is optimized for OLAP (analytical) workloads:

- **OLAP**: Fast reads, aggregations, analytics
- **OLTP**: Fast writes, transactions, row-level updates

MinIODB is **not** suitable for OLTP workloads requiring:
- ACID transactions
- Real-time row updates
- Low latency point queries

Use PostgreSQL, MySQL, etc. for OLTP.

### Q: How does MinIODB compare to ClickHouse, Druid, etc.?

A: 

**MinIODB vs ClickHouse**:
- Similar: Columnar, Parquet, SQL
- MinIODB: Simpler, cloud-native (MinIO), easier setup
- ClickHouse: More mature, more features, more complex

**MinIODB vs Druid**:
- Similar: Real-time analytics, distributed
- MinIODB: Simpler, SQL-based
- Druid: More powerful, more complex, better for event analytics

**MinIODB vs Apache Spark**:
- MinIODB: Single binary, easier ops
- Spark: More features, batch + streaming, more complex

### Q: Is MinIODB production-ready?

A: MinIODB is production-ready for many use cases:

✅ **Ready for**:
- Web analytics
- Log analysis
- IoT sensor data
- Business intelligence
- Data warehousing (small-medium)

⚠️ **Consider alternatives for**:
- Very large datasets (>100PB)
- Extreme performance requirements (<1ms)
- Complex ETL pipelines
- Mission-critical OLTP

### Q: What is the roadmap?

A: Planned features:

- [ ] Advanced indexing
- [ ] Materialized views
- [ ] User-defined functions (UDF)
- [ ] More aggregation functions
- [ ] Time-series optimizations
- [ ] Cloud-native deployments (AWS, GCP, Azure)
- [ ] Managed service offering

## Getting Help

### Q: Where can I get support?

A: 

- **GitHub Issues**: Report bugs and feature requests
- **Documentation**: See `docs/` directory
- **Examples**: See `examples/` directory
- **Community**: Join our Slack/Discord (link in README)

### Q: How do I report a bug?

A: 

1. Check existing issues first
2. Create a new issue with:
   - Clear title
   - Steps to reproduce
   - Expected behavior
   - Actual behavior
   - Environment (OS, version, etc.)
   - Logs (if relevant)

### Q: How can I contribute?

A: We welcome contributions! See `CONTRIBUTING.md` for:

- Code of conduct
- Development setup
- Pull request process
- Coding standards

---

## Still have questions?

- Check the [Architecture Documentation](ARCHITECTURE.md)
- Review [OLAP Usage Scenarios](OLAP_SCENARIOS.md)
- See the [API Documentation](API.html)
- Read deployment guides:
  - [Single-Node](SINGLE_NODE_DEPLOYMENT.md)
  - [Distributed](DISTRIBUTED_DEPLOYMENT.md)
