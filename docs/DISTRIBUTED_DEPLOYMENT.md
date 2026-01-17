# MinIODB Distributed Deployment Guide

## Overview

This guide covers deploying MinIODB in a distributed configuration for production environments requiring high availability, scalability, and fault tolerance.

## Architecture Overview

```
                    ┌─────────────────────────────────────────┐
                    │         Load Balancer (HAProxy)        │
                    │           LB: 8080, 8081, 9090         │
                    └─────────────────────────────────────────┘
                                        │
                    ┌───────────────┬───────────────┬───────────────┐
                    │               │               │               │
                    ▼               ▼               ▼               ▼
            ┌───────────┐   ┌───────────┐   ┌───────────┐   ┌───────────┐
            │  Node 1   │   │  Node 2   │   │  Node 3   │   │  Node N   │
            │  Primary   │   │  Replica  │   │  Replica  │   │  Replica  │
            └───────────┘   └───────────┘   └───────────┘   └───────────┘
                    │               │               │               │
                    └───────────────┴───────────────┴───────────────┘
                                        │
                    ┌───────────────┴───────────────┬───────────────┐
                    │                               │               │
                    ▼                               ▼               ▼
         ┌──────────────────┐          ┌──────────────┐  ┌──────────────┐
         │  Redis Cluster  │          │ MinIO Cluster │  │  Kafka (opt) │
         │  (Sentinel)    │          │  (Gateway)   │  │              │
         └──────────────────┘          └──────────────┘  └──────────────┘
```

## Prerequisites

### Hardware Requirements

| Role | Minimum | Recommended |
|------|----------|-------------|
| MinIODB Nodes (each) | 4 cores, 8GB RAM | 8+ cores, 32GB RAM |
| Load Balancer | 2 cores, 4GB RAM | 4+ cores, 8GB RAM |
| Redis Master | 2 cores, 4GB RAM | 4+ cores, 8GB RAM |
| Redis Sentinel (each) | 1 core, 1GB RAM | 2 cores, 2GB RAM |
| MinIO Gateway | 4 cores, 8GB RAM | 8+ cores, 16GB RAM |

### Software Requirements

Same as single-node, plus:
- **HAProxy**: 2.4+ (load balancer)
- **Redis Sentinel**: 7.0+
- **MinIO Gateway**: 2023.10.19+

## Network Architecture

### Network Topology

```
192.168.1.0/24
├── 192.168.1.10  - Load Balancer
├── 192.168.1.20-50 - MinIODB Nodes (Node 1-N)
├── 192.168.1.60-70 - Redis Cluster
└── 192.168.1.80-90 - MinIO Cluster
```

### Port Configuration

| Service | Port | Description |
|---------|------|-------------|
| MinIODB gRPC | 8080 | gRPC API |
| MinIODB REST | 8081 | REST API |
| MinIODB Metrics | 9090 | Prometheus metrics |
| Load Balancer | 8080, 8081, 9090 | Proxy to nodes |
| Redis | 6379 | Data store |
| Redis Sentinel | 26379 | Sentinel |
| MinIO API | 9000 | Object storage |
| MinIO Console | 9001 | Web console |

## Deployment Methods

### Method 1: Docker Swarm (Recommended)

#### 1. Initialize Docker Swarm

```bash
# On manager node
docker swarm init --advertise-addr <MANAGER_IP>

# On worker nodes
docker swarm join --token <TOKEN> <MANAGER_IP>:2377
```

#### 2. Create Docker Stack

Create `docker-stack.yml`:

```yaml
version: '3.8'

services:
  # HAProxy Load Balancer
  loadbalancer:
    image: haproxy:2.4
    ports:
      - "8080:8080"  # gRPC
      - "8081:8081"  # REST
      - "9090:9090"  # Metrics
    configs:
      - source: haproxy.cfg
        target: /usr/local/etc/haproxy/haproxy.cfg
    networks:
      - miniodb-net
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.role == manager

  # MinIO Gateway (Distributed)
  minio-gateway:
    image: minio/minio:latest
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    command: gateway s3 /data
    networks:
      - miniodb-net
    deploy:
      mode: replicated
      replicas: 1
    volumes:
      - minio_gateway_data:/data

  # MinIO Storage Nodes
  minio-node-1:
    image: minio/minio:latest
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    command: server http://minio-node-1{1...4}/data
    networks:
      - miniodb-net
    deploy:
      mode: replicated
      replicas: 4
    volumes:
      - minio_node_1_data:/data

  # Redis Master
  redis-master:
    image: redis:7-alpine
    command: redis-server --appendonly yes --replica-announce-ip redis-master
    networks:
      - miniodb-net
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.hostname == redis-1
    volumes:
      - redis_master_data:/data

  # Redis Replicas
  redis-replica:
    image: redis:7-alpine
    command: redis-server --appendonly yes --replicaof redis-master 6379 --replica-announce-ip redis-replica
    networks:
      - miniodb-net
    deploy:
      mode: replicated
      replicas: 2
    volumes:
      - redis_replica_data:/data

  # Redis Sentinel
  redis-sentinel:
    image: redis:7-alpine
    command: redis-sentinel /etc/redis/sentinel.conf
    configs:
      - source: sentinel.conf
        target: /etc/redis/sentinel.conf
    networks:
      - miniodb-net
    deploy:
      mode: global
    depends_on:
      - redis-master

  # MinIODB Nodes
  miniodb:
    image: miniodb/miniodb:latest
    environment:
      # Server
      - MINIODB_SERVER_NODE_ID={{.Service.Name}}-{{.Task.Slot}}
      - MINIODB_SERVER_GRPC_PORT=8080
      - MINIODB_SERVER_REST_PORT=8081
      - MINIODB_SERVER_METRICS_PORT=9090
      
      # Redis (Sentinel)
      - MINIODB_REDIS_SENTINEL_ENABLED=true
      - MINIODB_REDIS_SENTINEL_MASTER_NAME=mymaster
      - MINIODB_REDIS_SENTINEL_ADDERS=redis-sentinel:26379
      
      # MinIO Gateway
      - MINIODB_STORAGE_MINIO_ENDPOINT=minio-gateway:9000
      - MINIODB_STORAGE_MINIO_ACCESS_KEY=minioadmin
      - MINIODB_STORAGE_MINIO_SECRET_KEY=minioadmin
      - MINIODB_STORAGE_MINIO_BUCKET=miniodb-data
      - MINIODB_STORAGE_MINIO_USE_SSL=false
      
      # Distributed
      - MINIODB_COORDINATOR_ENABLED=true
      - MINIODB_COORDINATOR_MODE=distributed
      
      # Query
      - MINIODB_QUERY_CACHE_ENABLED=true
      - MINIODB_QUERY_CACHE_DISTRIBUTED=true
      
      # Buffer
      - MINIODB_BUFFER_SIZE=10000
      - MINIODB_BUFFER_FLUSH_INTERVAL=60
      
      # Metadata
      - MINIODB_METADATA_BACKUP_ENABLED=true
      - MINIODB_METADATA_BACKUP_INTERVAL=3600
    networks:
      - miniodb-net
    deploy:
      mode: replicated
      replicas: 3
      update_config:
        parallelism: 1
        delay: 10s
        order: start-first
      resources:
        limits:
          cpus: '4'
          memory: 8G
        reservations:
          cpus: '2'
          memory: 4G
    depends_on:
      - redis-master
      - minio-gateway
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8081/api/v1/health"]
      interval: 30s
      timeout: 10s
      retries: 3

networks:
  miniodb-net:
    driver: overlay
    attachable: true

configs:
  haproxy.cfg:
    file: ./deploy/haproxy/haproxy.cfg
  sentinel.conf:
    file: ./deploy/redis/sentinel.conf

volumes:
  minio_gateway_data:
  minio_node_1_data:
  redis_master_data:
  redis_replica_data:
```

#### 3. Deploy Stack

```bash
# Deploy stack
docker stack deploy -c docker-stack.yml miniodb

# Check services
docker stack services miniodb

# Check nodes
docker node ls

# View logs
docker service logs -f miniodb_miniodb
```

### Method 2: Kubernetes

#### 1. Create Namespace

```bash
kubectl create namespace miniodb
```

#### 2. Deploy Redis Cluster

```yaml
# redis-master.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis-master
  namespace: miniodb
spec:
  serviceName: redis-master
  replicas: 1
  selector:
    matchLabels:
      app: redis-master
  template:
    metadata:
      labels:
        app: redis-master
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
        command:
        - redis-server
        - --appendonly yes
        - --replica-announce-ip $(POD_IP)
        volumeMounts:
        - name: data
          mountPath: /data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi

---
apiVersion: v1
kind: Service
metadata:
  name: redis-master
  namespace: miniodb
spec:
  selector:
    app: redis-master
  ports:
  - port: 6379
    targetPort: 6379
  clusterIP: None
```

```bash
kubectl apply -f redis-master.yaml
```

#### 3. Deploy MinIO Gateway

```yaml
# minio-gateway.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: minio-gateway
  namespace: miniodb
spec:
  replicas: 1
  selector:
    matchLabels:
      app: minio-gateway
  template:
    metadata:
      labels:
        app: minio-gateway
    spec:
      containers:
      - name: minio
        image: minio/minio:latest
        ports:
        - containerPort: 9000
        - containerPort: 9001
        env:
        - name: MINIO_ROOT_USER
          value: minioadmin
        - name: MINIO_ROOT_PASSWORD
          value: minioadmin
        command:
        - gateway
        - s3
        - /data

---
apiVersion: v1
kind: Service
metadata:
  name: minio-gateway
  namespace: miniodb
spec:
  selector:
    app: minio-gateway
  ports:
  - port: 9000
    name: api
  - port: 9001
    name: console
```

```bash
kubectl apply -f minio-gateway.yaml
```

#### 4. Deploy MinIODB Nodes

```yaml
# miniodb.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: miniodb
  namespace: miniodb
spec:
  serviceName: miniodb
  replicas: 3
  selector:
    matchLabels:
      app: miniodb
  template:
    metadata:
      labels:
        app: miniodb
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values:
                - miniodb
            topologyKey: kubernetes.io/hostname
      containers:
      - name: miniodb
        image: miniodb/miniodb:latest
        ports:
        - containerPort: 8080  # gRPC
        - containerPort: 8081  # REST
        - containerPort: 9090  # Metrics
        env:
        # Server
        - name: MINIODB_SERVER_NODE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: MINIODB_SERVER_GRPC_PORT
          value: "8080"
        - name: MINIODB_SERVER_REST_PORT
          value: "8081"
        - name: MINIODB_SERVER_METRICS_PORT
          value: "9090"
        
        # Redis
        - name: MINIODB_REDIS_HOST
          value: redis-master
        - name: MINIODB_REDIS_PORT
          value: "6379"
        
        # MinIO
        - name: MINIODB_STORAGE_MINIO_ENDPOINT
          value: minio-gateway:9000
        - name: MINIODB_STORAGE_MINIO_ACCESS_KEY
          value: minioadmin
        - name: MINIODB_STORAGE_MINIO_SECRET_KEY
          value: minioadmin
        - name: MINIODB_STORAGE_MINIO_BUCKET
          value: miniodb-data
        
        # Distributed
        - name: MINIODB_COORDINATOR_ENABLED
          value: "true"
        - name: MINIODB_COORDINATOR_MODE
          value: "distributed"
        
        # Resources
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
          limits:
            cpu: "4"
            memory: "8Gi"
        livenessProbe:
          httpGet:
            path: /api/v1/health
            port: 8081
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /api/v1/health
            port: 8081
          initialDelaySeconds: 10
          periodSeconds: 5

---
apiVersion: v1
kind: Service
metadata:
  name: miniodb
  namespace: miniodb
spec:
  selector:
    app: miniodb
  ports:
  - port: 8080
    name: grpc
    targetPort: 8080
  - port: 8081
    name: rest
    targetPort: 8081
  - port: 9090
    name: metrics
    targetPort: 9090
  clusterIP: None

---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: miniodb-ingress
  namespace: miniodb
spec:
  rules:
  - host: miniodb.example.com
    http:
      paths:
      - path: /api
        pathType: Prefix
        backend:
          service:
            name: miniodb
            port:
              number: 8081
```

```bash
kubectl apply -f miniodb.yaml
```

#### 5. Deploy Load Balancer

Using Nginx Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: miniodb-ingress
  namespace: miniodb
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  rules:
  - host: miniodb.example.com
    http:
      paths:
      - path: /api/v1
        pathType: Prefix
        backend:
          service:
            name: miniodb
            port:
              number: 8081
```

## Configuration

### HAProxy Configuration

`deploy/haproxy/haproxy.cfg`:

```
defaults
  mode http
  timeout connect 5000ms
  timeout client 50000ms
  timeout server 50000ms

frontend miniodb-grpc
  bind *:8080
  mode tcp
  default_backend miniodb-grpc-backend

frontend miniodb-rest
  bind *:8081
  default_backend miniodb-rest-backend

frontend miniodb-metrics
  bind *:9090
  default_backend miniodb-metrics-backend

backend miniodb-grpc-backend
  mode tcp
  balance roundrobin
  option httpchk GET /api/v1/health
  server miniodb-1 miniodb-1:8080 check
  server miniodb-2 miniodb-2:8080 check
  server miniodb-3 miniodb-3:8080 check

backend miniodb-rest-backend
  balance roundrobin
  option httpchk GET /api/v1/health
  server miniodb-1 miniodb-1:8081 check
  server miniodb-2 miniodb-2:8081 check
  server miniodb-3 miniodb-3:8081 check

backend miniodb-metrics-backend
  balance roundrobin
  server miniodb-1 miniodb-1:9090 check
  server miniodb-2 miniodb-2:9090 check
  server miniodb-3 miniodb-3:9090 check
```

### Redis Sentinel Configuration

`deploy/redis/sentinel.conf`:

```
port 26379
sentinel monitor mymaster redis-master 6379 2
sentinel down-after-milliseconds mymaster 30000
sentinel parallel-syncs mymaster 1
sentinel failover-timeout mymaster 180000
sentinel auth-pass mymaster mypassword
sentinel auth-pass mymaster-password
sentinel announce-ip redis-sentinel
sentinel announce-port 26379
```

## Monitoring

### Prometheus Configuration

```yaml
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'miniodb'
    consul_sd_configs:
      - server: 'localhost:8500'
        services: ['miniodb']
    relabel_configs:
      - source_labels: [__meta_consul_service_address]
        separator: ;
        regex: '(.*)'
        target_label: __address__
        replacement: $1:9090

  - job_name: 'haproxy'
    static_configs:
      - targets: ['loadbalancer:8404']
```

### Grafana Dashboard

MinIODB provides a pre-configured Grafana dashboard for monitoring distributed deployments.

Import `deploy/grafana/distributed-dashboard.json`.

## High Availability

### Failover Scenarios

1. **Redis Master Failure**
   - Sentinel promotes replica to master
   - MinIODB nodes reconnect to new master
   - No data loss (replication was active)

2. **MinIO Gateway Failure**
   - MinIO storage nodes continue serving data
   - Gateway restarts automatically (Swarm/K8s)
   - Minimal impact

3. **MinIODB Node Failure**
   - Load balancer routes to healthy nodes
   - Failed node is automatically removed
   - Health check brings it back online

### Data Consistency

- **Write Path**: WAL + Buffer → Parquet → MinIO
- **Replication**: Redis master-replica
- **Consistency**: Eventual consistency for distributed reads

## Scaling

### Horizontal Scaling

Add more MinIODB nodes:

```bash
# Docker Swarm
docker service scale miniodb_miniodb=5

# Kubernetes
kubectl scale statefulset miniodb --replicas=5
```

### Vertical Scaling

Increase resources:

```yaml
# In StatefulSet or Service spec
resources:
  limits:
    cpu: "8"
    memory: "16Gi"
  requests:
    cpu: "4"
    memory: "8Gi"
```

## Disaster Recovery

### Backup Strategy

1. **Daily Metadata Backup**
   - Automatic backup to MinIO
   - Retention: 30 days

2. **Weekly Full Backup**
   - MinIO data replication
   - Offsite backup

### Restore Procedure

```bash
# 1. Restore metadata
curl -X POST http://loadbalancer:8081/api/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{"from_latest": false, "backup_file": "backup_20240118.json"}'

# 2. Restore MinIO data
# Use MinIO mc client to restore from backup

# 3. Restart services
docker stack deploy miniodb
```

## Security

### Network Security

```bash
# Network segmentation
# Frontend network: Load balancer only
# Backend network: MinIODB nodes
# Data network: Redis, MinIO
```

### Authentication

```yaml
# Enable API key authentication
server:
  auth_enabled: true
  api_keys:
    - production-key-1
    - production-key-2
```

### TLS/SSL

```yaml
# Enable TLS for all services
server:
  tls_enabled: true
  tls_cert_file: "/etc/miniodb/cert.pem"
  tls_key_file: "/etc/miniodb/key.pem"

storage:
  minio:
    use_ssl: true
```

## Troubleshooting

### Node Not Joining Cluster

```bash
# Check Redis connectivity
redis-cli -h redis-master -p 6379 ping

# Check network
ping redis-master
telnet redis-master 6379

# Check logs
docker service logs miniodb_miniodb
```

### Split Brain Scenario

Prevent split brain with Redis Sentinel:

```yaml
# sentinel.conf
sentinel down-after-milliseconds mymaster 30000  # 30s
sentinel failover-timeout mymaster 180000    # 3 minutes
```

### Performance Issues

```bash
# Check resource usage
docker stats

# Check slow queries
curl http://loadbalancer:9090/metrics | grep query_duration

# Check cache hit rate
curl http://loadbalancer:9090/metrics | grep cache_hit
```

## Next Steps

- See [Single-Node Deployment Guide](SINGLE_NODE_DEPLOYMENT.md) for basic setup
- Review [OLAP Usage Scenarios](OLAP_SCENARIOS.md) for use cases
- Check [FAQ](FAQ.md) for common questions
- Read [API Documentation](API.html) for API reference
