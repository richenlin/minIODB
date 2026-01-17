# MinIODB Single-Node Deployment Guide

## Overview

This guide covers deploying MinIODB in a single-node configuration for development, testing, or production use cases where a single server is sufficient.

## Prerequisites

### Hardware Requirements

| Component | Minimum | Recommended |
|-----------|----------|-------------|
| CPU | 2 cores | 4+ cores |
| RAM | 4GB | 16GB+ |
| Disk | 100GB SSD | 500GB+ NVMe SSD |
| Network | 1Gbps | 10Gbps |

### Software Requirements

- **Go**: 1.21+ (for building from source)
- **MinIO**: 2023.10.19+
- **Redis**: 7.0+
- **Docker** (optional): 20.10+
- **Docker Compose** (optional): 2.0+

### Operating System

- Linux (Ubuntu 20.04+, CentOS 8+, Debian 11+)
- macOS 12+ (development only)
- Windows 10+ with WSL2 (development only)

## Installation Methods

### Method 1: Docker Compose (Recommended)

Docker Compose is the easiest way to get started.

#### 1. Create Docker Compose File

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  # MinIO Storage
  minio:
    image: minio/minio:latest
    container_name: miniodb-minio
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    command: server /data --console-address ":9001"
    volumes:
      - minio_data:/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 30s
      timeout: 20s
      retries: 3

  # Redis Cache & Coordination
  redis:
    image: redis:7-alpine
    container_name: miniodb-redis
    ports:
      - "6379:6379"
    command: redis-server --appendonly yes --requirepass redispass
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

  # MinIODB Service
  miniodb:
    image: miniodb/miniodb:latest
    container_name: miniodb
    ports:
      - "8080:8080"  # gRPC
      - "8081:8081"  # REST API
      - "9090:9090"  # Metrics
    environment:
      # Server Configuration
      - MINIODB_SERVER_NODE_ID=node-1
      - MINIODB_SERVER_GRPC_PORT=8080
      - MINIODB_SERVER_REST_PORT=8081
      - MINIODB_SERVER_METRICS_PORT=9090
      
      # MinIO Configuration
      - MINIODB_STORAGE_MINIO_ENDPOINT=minio:9000
      - MINIODB_STORAGE_MINIO_ACCESS_KEY=minioadmin
      - MINIODB_STORAGE_MINIO_SECRET_KEY=minioadmin
      - MINIODB_STORAGE_MINIO_BUCKET=miniodb-data
      - MINIODB_STORAGE_MINIO_USE_SSL=false
      
      # Redis Configuration
      - MINIODB_REDIS_HOST=redis
      - MINIODB_REDIS_PORT=6379
      - MINIODB_REDIS_PASSWORD=redispass
      - MINIODB_REDIS_DB=0
      
      # Query Engine Configuration
      - MINIODB_QUERY_CACHE_ENABLED=true
      - MINIODB_QUERY_CACHE_TTL=3600
      
      # Buffer Configuration
      - MINIODB_BUFFER_SIZE=10000
      - MINIODB_BUFFER_FLUSH_INTERVAL=60
      
      # Metadata Configuration
      - MINIODB_METADATA_BACKUP_ENABLED=true
      - MINIODB_METADATA_BACKUP_INTERVAL=3600
    depends_on:
      minio:
        condition: service_healthy
      redis:
        condition: service_healthy
    volumes:
      - miniodb_config:/app/config
      - miniodb_data:/app/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8081/api/v1/health"]
      interval: 30s
      timeout: 10s
      retries: 3

volumes:
  minio_data:
  redis_data:
  miniodb_config:
  miniodb_data:
```

#### 2. Start Services

```bash
# Start all services
docker-compose up -d

# Check logs
docker-compose logs -f miniodb

# Check status
docker-compose ps
```

#### 3. Verify Deployment

```bash
# Check health
curl http://localhost:8081/api/v1/health

# Check system status
curl http://localhost:8081/api/v1/status

# List tables
curl http://localhost:8081/api/v1/tables
```

### Method 2: Manual Installation

#### 1. Install Dependencies

**Ubuntu/Debian:**
```bash
# Update packages
sudo apt update

# Install Redis
sudo apt install -y redis-server
sudo systemctl enable redis-server
sudo systemctl start redis-server

# Install MinIO
wget https://dl.min.io/server/minio/release/linux-amd64/minio
chmod +x minio
sudo mv minio /usr/local/bin/

# Create MinIO user and directories
sudo useradd -r minio-user -s /sbin/nologin
sudo mkdir -p /data/minio
sudo chown -R minio-user:minio-user /data/minio
```

**macOS:**
```bash
# Install dependencies
brew install redis minio/stable/minio

# Start services
brew services start redis
```

#### 2. Start MinIO

```bash
# Set MinIO credentials
export MINIO_ROOT_USER=minioadmin
export MINIO_ROOT_PASSWORD=minioadmin

# Start MinIO server
minio server /data/minio --console-address ":9001"
```

MinIO will be accessible at:
- API: http://localhost:9000
- Console: http://localhost:9001

#### 3. Configure Redis

```bash
# Start Redis
redis-server --requirepass yourpassword --appendonly yes
```

#### 4. Build MinIODB

```bash
# Clone repository
git clone https://github.com/yourorg/miniodb.git
cd miniodb

# Build binary
go build -o bin/miniodb cmd/main.go

# Install
sudo cp bin/miniodb /usr/local/bin/
```

#### 5. Configure MinIODB

Create `/etc/miniodb/config.yaml`:

```yaml
server:
  node_id: "node-1"
  grpc_port: ":8080"
  rest_port: ":8081"
  metrics_port: ":9090"

storage:
  minio:
    endpoint: "localhost:9000"
    access_key: "minioadmin"
    secret_key: "minioadmin"
    bucket: "miniodb-data"
    use_ssl: false
    region: "us-east-1"

redis:
  host: "localhost"
  port: 6379
  password: ""
  db: 0
  pool_size: 10

query:
  cache_enabled: true
  cache_ttl: 3600
  max_open_files: 1000

buffer:
  size: 10000
  flush_interval: 60
  worker_count: 4

metadata:
  backup_enabled: true
  backup_interval: 3600
  backup_retention_days: 30

table_management:
  default_table: "default"
  auto_create_tables: false

tables:
  default_config:
    buffer_size: 1000
    flush_interval_seconds: 60
    retention_days: 90
    backup_enabled: true
    id_strategy: "uuid"
    auto_generate_id: true

subscription:
  enabled: true
  redis:
    enabled: true
    stream_prefix: "miniodb:stream:"
    consumer_group: "miniodb-workers"
```

#### 6. Start MinIODB

```bash
# Start MinIODB
miniodb

# Or as a service
sudo systemctl start miniodb
```

### Method 3: Systemd Service

Create `/etc/systemd/system/miniodb.service`:

```ini
[Unit]
Description=MinIODB OLAP Database
After=network.target redis.service minio.service

[Service]
Type=simple
User=miniodb
Group=miniodb
WorkingDirectory=/var/lib/miniodb
ExecStart=/usr/local/bin/miniodb -config /etc/miniodb/config.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable miniodb
sudo systemctl start miniodb
sudo systemctl status miniodb
```

## Configuration

### Environment Variables

MinIODB supports configuration via environment variables:

```bash
# Server
export MINIODB_SERVER_NODE_ID=node-1
export MINIODB_SERVER_GRPC_PORT=8080
export MINIODB_SERVER_REST_PORT=8081

# Storage
export MINIODB_STORAGE_MINIO_ENDPOINT=localhost:9000
export MINIODB_STORAGE_MINIO_ACCESS_KEY=minioadmin
export MINIODB_STORAGE_MINIO_SECRET_KEY=minioadmin
export MINIODB_STORAGE_MINIO_BUCKET=miniodb-data

# Redis
export MINIODB_REDIS_HOST=localhost
export MINIODB_REDIS_PORT=6379
export MINIODB_REDIS_PASSWORD=
```

### Configuration File

Configuration can also be provided via YAML file (see Method 2 above).

## Verification

### 1. Health Check

```bash
curl http://localhost:8081/api/v1/health
```

Expected response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-18T10:00:00Z",
  "version": "1.0.0",
  "details": {
    "redis": "ok",
    "minio": "ok",
    "duckdb": "ok"
  }
}
```

### 2. Create Test Table

```bash
curl -X POST http://localhost:8081/api/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "test_table",
    "config": {
      "buffer_size": 1000,
      "retention_days": 30
    }
  }'
```

### 3. Write Test Data

```bash
curl -X POST http://localhost:8081/api/v1/data/write \
  -H "Content-Type: application/json" \
  -d '{
    "table": "test_table",
    "data": {
      "id": "test_001",
      "payload": {
        "message": "Hello, MinIODB!",
        "value": 42
      }
    }
  }'
```

### 4. Query Test Data

```bash
curl -X POST http://localhost:8081/api/v1/data/query \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT * FROM test_table LIMIT 10"
  }'
```

## Performance Tuning

### Buffer Configuration

For high write throughput:

```yaml
buffer:
  size: 50000           # Larger buffer
  flush_interval: 120     # Less frequent flushes
  worker_count: 8         # More workers
```

### Cache Configuration

For fast queries:

```yaml
query:
  cache_enabled: true
  cache_ttl: 7200        # 2 hours
  cache_size: 1000        # Number of cached queries
```

### Memory Configuration

Allocate sufficient memory for your workload:

```yaml
# For 10M+ records
buffer:
  size: 100000
  
query:
  max_open_files: 5000
```

## Monitoring

### Logs

```bash
# Docker logs
docker-compose logs -f miniodb

# Systemd logs
journalctl -u miniodb -f

# Log file location
tail -f /var/log/miniodb/miniodb.log
```

### Metrics

MinIODB exposes Prometheus metrics at `http://localhost:9090/metrics`.

Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'miniodb'
    static_configs:
      - targets: ['localhost:9090']
```

### Grafana Dashboard

Import the MinIODB dashboard from `deploy/grafana/miniodb-dashboard.json`.

## Backup and Recovery

### Metadata Backup

```bash
# Manual backup
curl -X POST http://localhost:8081/api/v1/metadata/backup \
  -H "Content-Type: application/json" \
  -d '{"force": true}'
```

### List Backups

```bash
curl http://localhost:8081/api/v1/metadata/backups?days=7
```

### Restore from Backup

```bash
curl -X POST http://localhost:8081/api/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{
    "from_latest": true,
    "dry_run": false
  }'
```

## Troubleshooting

### Common Issues

**1. MinIO Connection Failed**

```bash
# Check MinIO is running
curl http://localhost:9000/minio/health/live

# Verify credentials
export MINIO_ROOT_USER=minioadmin
export MINIO_ROOT_PASSWORD=minioadmin
```

**2. Redis Connection Failed**

```bash
# Check Redis is running
redis-cli ping

# Verify password
redis-cli -a yourpassword ping
```

**3. Port Already in Use**

```bash
# Find process using port
sudo lsof -i :8080

# Kill process
sudo kill -9 <PID>
```

**4. Out of Memory**

```bash
# Check memory usage
free -h

# Reduce buffer size
# Edit config.yaml
buffer:
  size: 5000
```

### Logs Analysis

```bash
# View error logs
grep ERROR /var/log/miniodb/miniodb.log

# View recent logs
tail -n 100 /var/log/miniodb/miniodb.log

# Monitor logs in real-time
tail -f /var/log/miniodb/miniodb.log
```

## Security

### Authentication

Enable API key authentication:

```yaml
server:
  auth_enabled: true
  api_keys:
    - your-api-key-here
```

Use API key in requests:

```bash
curl -H "X-API-Key: your-api-key-here" \
  http://localhost:8081/api/v1/tables
```

### TLS/SSL

Enable HTTPS:

```yaml
server:
  rest_port: ":8443"
  tls_enabled: true
  tls_cert_file: "/etc/miniodb/cert.pem"
  tls_key_file: "/etc/miniodb/key.pem"
```

### Network Security

```bash
# Firewall configuration
sudo ufw allow 8080/tcp  # gRPC
sudo ufw allow 8081/tcp  # REST API
sudo ufw allow 9090/tcp  # Metrics
sudo ufw enable
```

## Scaling Considerations

### When to Scale Up

- Query latency >5s
- Memory usage >80%
- Disk I/O bottleneck
- Write throughput insufficient

### Scaling Options

1. **Vertical**: Increase RAM, CPU, SSD
2. **Horizontal**: Move to distributed deployment (see Distributed Deployment Guide)

## Maintenance

### Data Retention

Configure automatic cleanup:

```yaml
tables:
  default_config:
    retention_days: 30  # Delete data older than 30 days
```

### Compaction

Enable automatic compaction:

```yaml
metadata:
  compaction_enabled: true
  compaction_interval: 86400  # Daily
  min_file_size: 10485760  # 10MB
```

### Version Upgrade

```bash
# Backup metadata first
curl -X POST http://localhost:8081/api/v1/metadata/backup

# Stop service
docker-compose down

# Pull new image
docker-compose pull

# Start service
docker-compose up -d
```

## Uninstallation

### Docker Compose

```bash
# Stop and remove containers
docker-compose down

# Remove volumes
docker-compose down -v

# Remove images
docker rmi minio/minio redis:7-alpine miniodb/miniodb
```

### Manual Installation

```bash
# Stop service
sudo systemctl stop miniodb

# Disable service
sudo systemctl disable miniodb

# Remove binary
sudo rm /usr/local/bin/miniodb

# Remove config
sudo rm -rf /etc/miniodb

# Remove data
sudo rm -rf /var/lib/miniodb
```

## Next Steps

- Review [Distributed Deployment Guide](DISTRIBUTED_DEPLOYMENT.md) for multi-node setup
- Check [OLAP Usage Scenarios](OLAP_SCENARIOS.md) for use cases
- See [FAQ](FAQ.md) for common questions
- Read [API Documentation](API.html) for API reference
