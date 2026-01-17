# MinIODB OLAP Usage Scenarios

## Overview

MinIODB is designed for Online Analytical Processing (OLAP) workloads, providing fast analytics on large volumes of data. This document covers typical use cases, data modeling patterns, and best practices.

## Typical OLAP Scenarios

### 1. Web Analytics & User Behavior Tracking

**Use Case**: Track user actions, page views, and events for analytics dashboards

**Data Model**:
```sql
-- Event tracking table
CREATE TABLE user_events (
    id STRING,
    timestamp TIMESTAMP,
    user_id INTEGER,
    event_type STRING,
    page_url STRING,
    referrer STRING,
    device_type STRING,
    browser STRING,
    session_id STRING,
    properties STRUCT  -- Flexible JSON properties
);

-- Sample data
{
  "id": "evt_001",
  "timestamp": "2024-01-18T10:00:00Z",
  "user_id": 12345,
  "event_type": "page_view",
  "page_url": "/products/item-123",
  "referrer": "https://google.com",
  "device_type": "mobile",
  "browser": "Chrome",
  "session_id": "sess_abc123",
  "properties": {
    "utm_source": "google",
    "utm_medium": "cpc"
  }
}
```

**Common Queries**:
```sql
-- Daily active users
SELECT
    DATE_TRUNC('day', timestamp) as day,
    COUNT(DISTINCT user_id) as dau
FROM user_events
WHERE timestamp >= NOW() - INTERVAL '30 days'
GROUP BY day
ORDER BY day;

-- Top pages by views
SELECT
    page_url,
    COUNT(*) as views,
    COUNT(DISTINCT user_id) as unique_visitors
FROM user_events
WHERE event_type = 'page_view'
    AND timestamp >= NOW() - INTERVAL '7 days'
GROUP BY page_url
ORDER BY views DESC
LIMIT 10;

-- User funnel analysis
SELECT
    event_type,
    COUNT(DISTINCT user_id) as users
FROM user_events
WHERE timestamp >= NOW() - INTERVAL '7 days'
GROUP BY event_type
ORDER BY users DESC;
```

**Ingestion Pattern**:
```python
# Real-time event streaming
for event in event_stream:
    client.write_data(
        table='user_events',
        record_id=event.event_id,
        payload={
            'user_id': event.user_id,
            'event_type': event.type,
            'page_url': event.page_url,
            # ... more fields
        }
    )
```

---

### 2. E-commerce Analytics

**Use Case**: Analyze sales, orders, and customer behavior

**Data Model**:
```sql
-- Orders table
CREATE TABLE orders (
    id STRING,
    timestamp TIMESTAMP,
    order_id STRING,
    user_id INTEGER,
    total_amount DECIMAL,
    status STRING,
    payment_method STRING,
    items ARRAY<STRUCT<product_id, product_name, quantity, price>>
);

-- Products table (dimension)
CREATE TABLE products (
    id STRING,
    name STRING,
    category STRING,
    price DECIMAL,
    created_at TIMESTAMP
);
```

**Common Queries**:
```sql
-- Daily revenue
SELECT
    DATE_TRUNC('day', timestamp) as day,
    COUNT(*) as orders,
    SUM(total_amount) as revenue,
    AVG(total_amount) as avg_order_value
FROM orders
WHERE status = 'completed'
    AND timestamp >= NOW() - INTERVAL '30 days'
GROUP BY day
ORDER BY day;

-- Top-selling products
SELECT
    UNNEST(items) as item,
    item.product_id,
    item.product_name,
    SUM(item.quantity) as total_sold,
    SUM(item.quantity * item.price) as revenue
FROM orders
WHERE status = 'completed'
    AND timestamp >= NOW() - INTERVAL '30 days'
GROUP BY item.product_id, item.product_name
ORDER BY total_sold DESC
LIMIT 10;

-- Customer lifetime value
SELECT
    user_id,
    COUNT(*) as order_count,
    SUM(total_amount) as total_spent,
    AVG(total_amount) as avg_order_value,
    MIN(timestamp) as first_order,
    MAX(timestamp) as last_order
FROM orders
WHERE status = 'completed'
GROUP BY user_id
ORDER BY total_spent DESC;
```

---

### 3. IoT Sensor Data Analytics

**Use Case**: Analyze time-series sensor data from IoT devices

**Data Model**:
```sql
-- Sensor readings table
CREATE TABLE sensor_readings (
    id STRING,
    timestamp TIMESTAMP,
    device_id STRING,
    sensor_type STRING,
    value FLOAT,
    unit STRING,
    location STRUCT<latitude, longitude>,
    status STRING
);
```

**Common Queries**:
```sql
-- Average sensor readings over time
SELECT
    device_id,
    sensor_type,
    DATE_TRUNC('hour', timestamp) as hour,
    AVG(value) as avg_value,
    MIN(value) as min_value,
    MAX(value) as max_value,
    STDDEV(value) as stddev
FROM sensor_readings
WHERE timestamp >= NOW() - INTERVAL '24 hours'
GROUP BY device_id, sensor_type, hour
ORDER BY hour;

-- Anomaly detection (values beyond 3 std deviations)
SELECT
    device_id,
    sensor_type,
    timestamp,
    value,
    avg_value,
    stddev_value,
    ABS(value - avg_value) / stddev_value as z_score
FROM (
    SELECT
        device_id,
        sensor_type,
        timestamp,
        value,
        AVG(value) OVER (
            PARTITION BY device_id, sensor_type
            ORDER BY timestamp
            RANGE BETWEEN INTERVAL '1 hour' PRECEDING AND CURRENT ROW
        ) as avg_value,
        STDDEV(value) OVER (
            PARTITION BY device_id, sensor_type
            ORDER BY timestamp
            RANGE BETWEEN INTERVAL '1 hour' PRECEDING AND CURRENT ROW
        ) as stddev_value
    FROM sensor_readings
    WHERE timestamp >= NOW() - INTERVAL '24 hours'
)
WHERE ABS(value - avg_value) / stddev_value > 3;
```

---

### 4. Log Analysis & Monitoring

**Use Case**: Analyze application logs and system metrics

**Data Model**:
```sql
-- Logs table
CREATE TABLE logs (
    id STRING,
    timestamp TIMESTAMP,
    level STRING,
    service STRING,
    host STRING,
    message STRING,
    error_code INTEGER,
    duration_ms INTEGER,
    properties STRUCT
);
```

**Common Queries**:
```sql
-- Error rate over time
SELECT
    DATE_TRUNC('hour', timestamp) as hour,
    service,
    COUNT(*) FILTER (WHERE level = 'ERROR') as error_count,
    COUNT(*) as total_count,
    ROUND(
        100.0 * COUNT(*) FILTER (WHERE level = 'ERROR') / COUNT(*),
        2
    ) as error_rate
FROM logs
WHERE timestamp >= NOW() - INTERVAL '24 hours'
GROUP BY hour, service
ORDER BY hour, service;

-- Slow request analysis
SELECT
    service,
    host,
    AVG(duration_ms) as avg_duration,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) as p95,
    PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms) as p99,
    COUNT(*) as request_count
FROM logs
WHERE level = 'INFO'
    AND duration_ms IS NOT NULL
    AND timestamp >= NOW() - INTERVAL '1 hour'
GROUP BY service, host
ORDER BY p99 DESC;

-- Top error messages
SELECT
    message,
    COUNT(*) as occurrences
FROM logs
WHERE level = 'ERROR'
    AND timestamp >= NOW() - INTERVAL '24 hours'
GROUP BY message
ORDER BY occurrences DESC
LIMIT 10;
```

---

### 5. Financial Analytics

**Use Case**: Analyze transactions, portfolio performance, and risk metrics

**Data Model**:
```sql
-- Transactions table
CREATE TABLE transactions (
    id STRING,
    timestamp TIMESTAMP,
    account_id STRING,
    transaction_type STRING,
    amount DECIMAL,
    currency STRING,
    counterparty STRING,
    category STRING,
    status STRING
);
```

**Common Queries**:
```sql
-- Daily transaction volume
SELECT
    DATE_TRUNC('day', timestamp) as day,
    transaction_type,
    COUNT(*) as transaction_count,
    SUM(amount) as total_amount
FROM transactions
WHERE status = 'completed'
    AND timestamp >= NOW() - INTERVAL '30 days'
GROUP BY day, transaction_type
ORDER BY day;

-- Cash flow analysis
SELECT
    DATE_TRUNC('month', timestamp) as month,
    transaction_type,
    category,
    SUM(amount) as net_amount
FROM transactions
WHERE status = 'completed'
    AND timestamp >= NOW() - INTERVAL '12 months'
GROUP BY month, transaction_type, category
ORDER BY month, transaction_type;

-- Risk metrics (value at risk)
WITH daily_returns AS (
    SELECT
        DATE_TRUNC('day', timestamp) as day,
        SUM(amount) as daily_total
    FROM transactions
    WHERE transaction_type IN ('buy', 'sell')
        AND timestamp >= NOW() - INTERVAL '365 days'
    GROUP BY day
),
percentiles AS (
    SELECT
        PERCENTILE_CONT(0.05) WITHIN GROUP (ORDER BY daily_total) as p05,
        PERCENTILE_CONT(0.01) WITHIN GROUP (ORDER BY daily_total) as p01
    FROM daily_returns
)
SELECT
    p05 as var_95,
    p01 as var_99
FROM percentiles;
```

---

## Data Modeling Best Practices

### 1. Table Design

**Do**:
- Use appropriate data types (STRING, INTEGER, FLOAT, TIMESTAMP, DECIMAL)
- Use STRUCT for nested data (up to 5 levels)
- Use ARRAY for repeated fields
- Partition by time (timestamp) for time-series data
- Add descriptive column names

**Don't**:
- Use TEXT for everything (use STRING for fixed-size text)
- Over-nest STRUCT structures (keep it shallow)
- Store large BLOBs in OLAP tables (use MinIO directly)

### 2. Timestamp Handling

```sql
-- Always store timestamps in UTC
SELECT
    timestamp as utc_time,
    CAST(timestamp AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Shanghai' AS TIMESTAMP) as local_time
FROM user_events
WHERE timestamp >= NOW() - INTERVAL '24 hours';
```

### 3. Partitioning Strategy

For large tables (>10M rows):
```sql
-- Time-based partitioning (automatic)
-- MinIODB handles this by grouping data files by date
SELECT * FROM user_events
WHERE DATE_TRUNC('day', timestamp) = '2024-01-18';

-- Query optimizer will only scan relevant files
```

---

## Performance Optimization

### 1. Query Optimization

```sql
-- Good: Filter on partition key first
SELECT *
FROM user_events
WHERE timestamp >= NOW() - INTERVAL '7 days'
    AND user_id = 12345;

-- Bad: Filter on non-partition key first
SELECT *
FROM user_events
WHERE user_id = 12345
    AND timestamp >= NOW() - INTERVAL '7 days';
```

### 2. Aggregation Optimization

```sql
-- Use approximations for large datasets
SELECT
    COUNT(DISTINCT user_id) as approx_users,  -- Exact count
    APPROX_COUNT_DISTINCT(user_id) as approx_users  -- Approximate
FROM user_events;

-- Use HLL for cardinality estimation (if supported)
SELECT HLL_COUNT(user_id) as unique_users
FROM user_events;
```

### 3. Data Type Optimization

```sql
-- Use smallest possible type
user_id INTEGER,      -- Not BIGINT
is_active BOOLEAN,     -- Not STRING
score FLOAT,          -- Not DOUBLE
status STRING(20)      -- Not TEXT
```

---

## Data Retention & Lifecycle Management

### 1. TTL Configuration

```yaml
# config.yaml
tables:
  default_config:
    retention_days: 90  # Delete data older than 90 days
  tables:
    user_events:
      retention_days: 30
    logs:
      retention_days: 7
```

### 2. Manual Data Pruning

```python
# Delete old data
client.delete_data(
    table='logs',
    record_id='old_record_id'
)

# Or use TTL auto-cleanup
# MinIODB automatically deletes files older than retention_days
```

---

## Real-time vs Batch Ingestion

### Real-time (Low Latency)

```python
# gRPC API
client.write_data(
    table='user_events',
    record_id='evt_001',
    payload={...}
)
# Latency: 1-10ms
```

### Batch (High Throughput)

```python
# Stream API
records = [generate_record(i) for i in range(10000)]
client.stream_write(
    table='user_events',
    records=records
)
# Throughput: 10,000+ records/sec
```

### Streaming (Event-driven)

```python
# Redis Streams
redis_client.publish(
    event=DataEvent(...),
    stream='miniodb:stream:user_events'
)

# Kafka
kafka_client.publish(
    event=DataEvent(...),
    topic='miniodb-user_events'
)
```

---

## Monitoring & Alerting

### Key Metrics

1. **Write Latency**: <100ms (p95)
2. **Query Latency**: <1s (p95) for typical queries
3. **Cache Hit Rate**: >70%
4. **Buffer Size**: Monitor for backpressure
5. **Storage Growth**: Monitor for capacity planning

### Alert Thresholds

```yaml
alerts:
  write_latency_p95: 500ms
  query_latency_p95: 5s
  cache_hit_rate: 50%
  storage_usage: 80%
  error_rate: 5%
```

---

## Common Patterns

### 1. Upsert Pattern

```sql
-- MinIODB doesn't support UPDATE directly
-- Use DELETE + INSERT pattern

-- 1. Delete old record
DELETE FROM user_events
WHERE id = 'evt_001';

-- 2. Insert new record
INSERT INTO user_events (...)
VALUES (...);
```

### 2. Incremental Aggregation

```sql
-- Store daily aggregates
CREATE TABLE daily_user_stats (
    id STRING,
    date DATE,
    user_id INTEGER,
    page_views INTEGER,
    events_count INTEGER
);

-- Query aggregates directly
SELECT
    user_id,
    SUM(page_views) as total_views,
    SUM(events_count) as total_events
FROM daily_user_stats
WHERE date >= DATE_TRUNC('day', NOW() - INTERVAL '30 days')
GROUP BY user_id;
```

### 3. Time-Series Pattern

```sql
-- Use window functions for time-series analysis
SELECT
    timestamp,
    value,
    AVG(value) OVER (
        ORDER BY timestamp
        RANGE BETWEEN INTERVAL '5 minutes' PRECEDING AND CURRENT ROW
    ) as moving_avg_5min
FROM sensor_readings
WHERE device_id = 'sensor_001'
ORDER BY timestamp;
```

---

## Scaling Considerations

### When to Scale Out

- Single node > 10M records
- Query latency >5s
- Write throughput >10K/sec
- Storage >10TB

### Scaling Strategy

1. **Vertical**: Increase buffer size, add more workers
2. **Horizontal**: Add more MinIODB nodes
3. **Partition**: Use table partitioning by time range

---

## Example: Complete Analytics Pipeline

```python
# 1. Ingest data
for event in event_stream:
    client.write_data(
        table='user_events',
        record_id=event.id,
        payload={
            'user_id': event.user_id,
            'event_type': event.type,
            'timestamp': event.timestamp.isoformat(),
            # ... more fields
        }
    )

# 2. Query analytics
result = client.query_data("""
    SELECT
        DATE_TRUNC('hour', timestamp) as hour,
        event_type,
        COUNT(*) as events,
        COUNT(DISTINCT user_id) as unique_users
    FROM user_events
    WHERE timestamp >= NOW() - INTERVAL '24 hours'
    GROUP BY hour, event_type
    ORDER BY hour, events DESC
""", limit=10000)

# 3. Process results
for row in result['data']:
    print(f"{row['hour']}: {row['event_type']} = {row['events']} events")
```

---

## Summary

MinIODB excels at:
- High-volume data ingestion (10K+ records/sec)
- Fast analytical queries (sub-second for typical queries)
- Real-time analytics via streaming
- Time-series analysis
- Large-scale aggregations
- Flexible schema with JSON/STRUCT

Best practices:
- Use appropriate data types
- Filter on partition keys first
- Use caching for repeated queries
- Monitor performance metrics
- Plan for scaling
