# MinIODB 告警配置指南

本指南介绍如何配置和使用MinIODB的Prometheus告警规则，帮助您及时发现和解决连接池耗尽、查询性能下降等问题。

## 目录
- [快速开始](#快速开始)
- [告警规则说明](#告警规则说明)
- [部署配置](#部署配置)
- [压测验证](#压测验证)
- [告警处理流程](#告警处理流程)

---

## 快速开始

### 1. 导入告警规则

将 `deploy/prometheus-alerts.yml` 添加到Prometheus配置中：

```yaml
# prometheus.yml
rule_files:
  - '/etc/prometheus/alerts/miniodb-alerts.yml'

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['localhost:9093']  # Alertmanager地址
```

### 2. 重启Prometheus

```bash
# Docker方式
docker-compose restart prometheus

# 系统服务方式
systemctl reload prometheus
```

### 3. 验证规则加载

访问 Prometheus UI：`http://localhost:9090/alerts`，确认MinIODB告警规则已加载。

---

## 告警规则说明

### 1. 连接池耗尽告警

#### Redis连接池告警

| 告警名称 | 触发条件 | 持续时间 | 严重程度 | 说明 |
|---------|---------|---------|---------|------|
| `RedisPoolHighUtilization` | 利用率 > 90% | 2分钟 | Warning | 连接池利用率过高 |
| `RedisPoolCriticalUtilization` | 利用率 > 95% | 1分钟 | Critical | 连接池接近耗尽 |

**PromQL查询**:
```promql
miniodb_redis_pool_utilization_percent > 90
```

**指标说明**:
- `miniodb_redis_pool_active_conns`: 活跃连接数
- `miniodb_redis_pool_idle_conns`: 空闲连接数
- `miniodb_redis_pool_total_conns`: 总连接数
- `miniodb_redis_pool_utilization_percent`: 利用率 (active/total * 100)

#### MinIO连接池告警

| 告警名称 | 触发条件 | 持续时间 | 严重程度 | 说明 |
|---------|---------|---------|---------|------|
| `MinIOPoolHighUtilization` | 利用率 > 90% | 2分钟 | Warning | 连接池利用率过高 |
| `MinIOPoolCriticalUtilization` | 利用率 > 95% | 1分钟 | Critical | 连接池接近耗尽 |

**PromQL查询**:
```promql
miniodb_minio_pool_utilization_percent > 90
```

### 2. 尾部时延告警

#### 查询P99时延告警

| 告警名称 | 触发条件 | 持续时间 | 严重程度 | 说明 |
|---------|---------|---------|---------|------|
| `QueryP99LatencyHigh` | P99 > 5秒 | 3分钟 | Warning | 查询时延过高 |
| `QueryP99LatencyCritical` | P99 > 10秒 | 2分钟 | Critical | 查询时延严重过高 |

**PromQL查询**:
```promql
histogram_quantile(0.99, 
  sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le, table)
) > 5
```

#### 写入P99时延告警

| 告警名称 | 触发条件 | 持续时间 | 严重程度 |
|---------|---------|---------|---------|
| `WriteP99LatencyHigh` | P99 > 1秒 | 3分钟 | Warning |
| `WriteP99LatencyCritical` | P99 > 2.5秒 | 2分钟 | Critical |

#### 刷新P99时延告警

| 告警名称 | 触发条件 | 持续时间 | 严重程度 |
|---------|---------|---------|---------|
| `FlushP99LatencyHigh` | P99 > 5秒 | 3分钟 | Warning |

### 3. 恢复通知

系统自动检测性能恢复并发送info级别通知：

- `RedisPoolUtilizationRecovered`: Redis连接池利用率降至70%以下
- `MinIOPoolUtilizationRecovered`: MinIO连接池利用率降至70%以下
- `QueryLatencyRecovered`: 查询P99时延降至3秒以下

### 4. 综合告警

#### 系统高压力告警

**触发条件**: 连接池利用率 > 90% **且** 查询P99时延 > 5秒

```promql
(miniodb_redis_pool_utilization_percent > 90 or miniodb_minio_pool_utilization_percent > 90)
and
histogram_quantile(0.99, sum(rate(miniodb_query_duration_seconds_bucket[5m])) by (le)) > 5
```

**紧急处理步骤**:
1. 立即扩容连接池
2. 检查并优化慢查询
3. 检查系统资源（CPU/内存/网络）
4. 考虑限流或降级
5. 通知运维团队

---

## 部署配置

### Prometheus配置示例

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 30s

rule_files:
  - '/etc/prometheus/alerts/miniodb-alerts.yml'

scrape_configs:
  - job_name: 'miniodb'
    static_configs:
      - targets: ['localhost:8080']  # MinIODB metrics endpoint
    metrics_path: '/metrics'

alerting:
  alertmanagers:
    - static_configs:
        - targets: ['alertmanager:9093']
```

### Alertmanager配置示例

```yaml
# alertmanager.yml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'component']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'miniodb-team'
  
  routes:
    - match:
        severity: critical
      receiver: 'miniodb-oncall'
      continue: true

    - match:
        severity: warning
      receiver: 'miniodb-team'

    - match:
        severity: info
      receiver: 'miniodb-notifications'

receivers:
  - name: 'miniodb-oncall'
    webhook_configs:
      - url: 'http://your-oncall-system/webhook'
    
  - name: 'miniodb-team'
    email_configs:
      - to: 'team@example.com'
        from: 'alertmanager@example.com'
        smarthost: 'smtp.example.com:587'
    
  - name: 'miniodb-notifications'
    webhook_configs:
      - url: 'http://your-notification-system/webhook'

inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname', 'component']
```

---

## 压测验证

### 1. 连接池耗尽压测

**目标**: 触发Redis连接池高利用率告警

```bash
# 使用 hey 工具进行压测
hey -z 5m -c 100 -m POST \
  -H "Content-Type: application/json" \
  -d '{"sql":"SELECT * FROM test_table LIMIT 1000"}' \
  http://localhost:8081/v1/query

# 或使用 wrk
wrk -t 20 -c 200 -d 5m \
  -s post_query.lua \
  http://localhost:8081/v1/query
```

**预期结果**:
1. 2分钟后触发 `RedisPoolHighUtilization` (Warning)
2. 如果继续压测，1分钟后触发 `RedisPoolCriticalUtilization` (Critical)

**验证方法**:
```bash
# 查询当前连接池利用率
curl -s http://localhost:8080/metrics | grep miniodb_redis_pool_utilization_percent

# 检查Prometheus告警状态
curl -s http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname | contains("RedisPool"))'
```

### 2. 查询P99时延压测

**目标**: 触发查询P99时延告警

```bash
# 执行大量复杂查询
for i in {1..1000}; do
  curl -X POST http://localhost:8081/v1/query \
    -H "Content-Type: application/json" \
    -d "{\"sql\":\"SELECT * FROM large_table WHERE timestamp > $(date -d '30 days ago' +%s) ORDER BY timestamp DESC LIMIT 10000\"}" &
done

# 等待并检查告警
sleep 180
curl -s http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname == "QueryP99LatencyHigh")'
```

**预期结果**:
1. 3分钟后触发 `QueryP99LatencyHigh` (Warning)
2. 如果P99持续超过10秒，2分钟后触发 `QueryP99LatencyCritical` (Critical)

### 3. 恢复验证

**停止压测后**:
```bash
# 杀掉所有压测进程
pkill -f hey
pkill -f wrk

# 监控恢复情况（约5分钟）
watch -n 10 'curl -s http://localhost:8080/metrics | grep -E "miniodb_redis_pool_utilization_percent|miniodb_query_duration_seconds"'

# 1分钟后应收到恢复通知
curl -s http://localhost:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname | contains("Recovered"))'
```

### 4. 压测脚本示例

```bash
#!/bin/bash
# test_alerts.sh - 完整的告警压测脚本

set -e

MINIODB_URL="http://localhost:8081"
PROMETHEUS_URL="http://localhost:9090"

echo "=== MinIODB 告警压测验证 ==="

# 1. 检查服务状态
echo "[1/5] 检查服务状态..."
curl -sf ${MINIODB_URL}/v1/health || { echo "MinIODB未运行"; exit 1; }
curl -sf ${PROMETHEUS_URL}/-/healthy || { echo "Prometheus未运行"; exit 1; }

# 2. 获取基线指标
echo "[2/5] 获取基线指标..."
BASELINE_UTIL=$(curl -s ${MINIODB_URL}/metrics | grep -oP 'miniodb_redis_pool_utilization_percent \K[0-9.]+' | head -1)
echo "当前Redis连接池利用率: ${BASELINE_UTIL}%"

# 3. 启动压测
echo "[3/5] 启动连接池压测（5分钟）..."
hey -z 5m -c 100 -m POST \
  -H "Content-Type: application/json" \
  -d '{"sql":"SELECT * FROM test_table LIMIT 1000"}' \
  ${MINIODB_URL}/v1/query > /dev/null 2>&1 &

LOAD_PID=$!

# 4. 监控告警触发
echo "[4/5] 监控告警状态（等待3分钟）..."
for i in {1..18}; do
  sleep 10
  ALERTS=$(curl -s ${PROMETHEUS_URL}/api/v1/alerts | jq -r '.data.alerts[] | select(.labels.alertname | contains("Pool")) | .labels.alertname' | wc -l)
  if [ "$ALERTS" -gt 0 ]; then
    echo "✓ 告警已触发 ($(date))"
    curl -s ${PROMETHEUS_URL}/api/v1/alerts | jq '.data.alerts[] | {alert: .labels.alertname, state: .state, value: .annotations.description}'
    break
  fi
  echo "  等待告警触发... ($i/18)"
done

# 5. 停止压测并验证恢复
echo "[5/5] 停止压测，验证恢复..."
kill $LOAD_PID 2>/dev/null || true

echo "等待恢复（2分钟）..."
sleep 120

RECOVERED=$(curl -s ${PROMETHEUS_URL}/api/v1/alerts | jq -r '.data.alerts[] | select(.labels.alertname | contains("Recovered")) | .labels.alertname' | wc -l)
if [ "$RECOVERED" -gt 0 ]; then
  echo "✓ 恢复通知已触发"
else
  echo "⚠ 未检测到恢复通知（可能需要更长时间）"
fi

echo "=== 压测完成 ==="
```

---

## 告警处理流程

### Warning级别告警处理

1. **确认告警**
   - 查看Grafana仪表板确认指标趋势
   - 检查是否为误报或临时波动

2. **分析原因**
   - 检查应用日志
   - 查看慢查询日志
   - 分析系统资源使用情况

3. **优化措施**
   - 优化查询性能
   - 调整连接池配置
   - 增加缓存

4. **记录和监控**
   - 记录问题和解决方案
   - 持续监控指标变化

### Critical级别告警处理

1. **立即响应**（5分钟内）
   - 确认告警严重性
   - 通知相关团队

2. **紧急缓解**（10分钟内）
   - 快速扩容连接池
   - 临时限流
   - 降级非关键功能

3. **根因分析**（30分钟内）
   - 查看详细日志和指标
   - 定位性能瓶颈
   - 识别异常查询或操作

4. **永久修复**（按优先级）
   - 优化代码或查询
   - 调整系统配置
   - 扩展基础设施

5. **复盘总结**
   - 记录事件时间线
   - 分析响应流程
   - 优化告警规则

---

## 常见问题

### Q1: 如何调整告警阈值？

编辑 `deploy/prometheus-alerts.yml`，修改 `expr` 中的阈值：

```yaml
# 将Redis连接池告警阈值从90%改为85%
- alert: RedisPoolHighUtilization
  expr: miniodb_redis_pool_utilization_percent > 85  # 修改这里
  for: 2m
```

### Q2: 如何临时禁用某个告警？

在Alertmanager中添加静默规则：

```bash
# 静默2小时
amtool silence add \
  alertname="RedisPoolHighUtilization" \
  --duration=2h \
  --author="ops@example.com" \
  --comment="Planned maintenance"
```

### Q3: 告警太频繁怎么办？

1. 增加 `for` 持续时间（例如从2m改为5m）
2. 调整 `group_interval` 和 `repeat_interval`
3. 使用 `inhibit_rules` 避免重复告警

### Q4: 如何集成到企业微信/钉钉？

配置Alertmanager的webhook_configs：

```yaml
receivers:
  - name: 'wechat'
    webhook_configs:
      - url: 'http://your-wechat-bot-url'
        send_resolved: true
```

---

## 相关文档

- [Prometheus告警文档](https://prometheus.io/docs/alerting/latest/overview/)
- [Alertmanager配置](https://prometheus.io/docs/alerting/latest/configuration/)
- [MinIODB监控指标](./METRICS.md)
- [Grafana仪表板](../deploy/grafana-write-flush-panels.json)

---

**更新时间**: 2025-10-03  
**维护者**: MinIODB Team

