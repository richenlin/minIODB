## 🔍 故障排除

### 常见问题

#### 1. 连接Redis失败
```bash
# 检查Redis连接
redis-cli -h localhost -p 6379 ping

# 检查配置
grep -A 5 "redis:" config.yaml

# 检查连接池状态
curl http://localhost:8081/v1/status | jq '.redis_stats'
```

#### 2. MinIO连接失败
```bash
# 检查MinIO服务
curl http://localhost:9000/minio/health/live

# 检查存储桶
mc ls minio/olap-data

# 检查连接池状态
curl http://localhost:8081/v1/status | jq '.minio_stats'
```

#### 3. 表相关问题

##### 表不存在错误
```bash
# 检查表是否存在
curl http://localhost:8081/v1/tables | jq '.tables[] | select(.name=="your_table")'

# 创建缺失的表
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "your_table", "if_not_exists": true}'

# 检查表统计信息
curl http://localhost:8081/v1/tables/your_table | jq '.table_info.stats'
```

##### 表配置问题
```bash
# 查看表配置
curl http://localhost:8081/v1/tables/your_table | jq '.table_info.config'

# 检查表级Redis配置
redis-cli hgetall "table:your_table:config"

# 查看表缓冲区状态
curl http://localhost:8081/v1/status | jq '.buffer_stats'
```

#### 4. 查询性能问题

##### 查询慢问题
```bash
# 查看查询统计
curl http://localhost:8081/v1/metrics | jq '.query_metrics'

# 检查缓存命中率
curl http://localhost:8081/v1/metrics | jq '.cache_metrics'

# 查看DuckDB连接池状态
curl http://localhost:8081/v1/status | jq '.query_engine_stats'
```

##### 流式查询问题
```bash
# 检查流式查询配置
grep -A 10 "query:" config.yaml

# 调整批次大小
# 在StreamQueryRequest中设置合适的batch_size

# 监控内存使用
curl http://localhost:8081/v1/metrics | jq '.memory_usage'
```

#### 5. 备份恢复问题

##### 备份失败
```bash
# 检查备份状态
curl http://localhost:8081/v1/metadata/status

# 手动触发备份
curl -X POST http://localhost:8081/v1/metadata/backup \
  -H "Content-Type: application/json" \
  -d '{"force": true}'

# 查看备份列表
curl http://localhost:8081/v1/metadata/backups?days=7
```

##### 恢复失败
```bash
# 干运行测试恢复
curl -X POST http://localhost:8081/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{"backup_file": "backup_xxx.json", "dry_run": true}'

# 检查备份文件完整性
curl -X POST http://localhost:8081/v1/metadata/restore \
  -H "Content-Type: application/json" \
  -d '{"backup_file": "backup_xxx.json", "validate": true, "dry_run": true}'
```

#### 6. 系统监控问题

##### 健康评分低
```bash
# 查看详细健康状态
curl http://localhost:8081/v1/status | jq '.health_status'

# 查看系统指标
curl http://localhost:8081/v1/metrics

# 检查各组件状态
curl http://localhost:8081/v1/status | jq '.components'
```

##### 指标收集问题
```bash
# 检查Prometheus指标
curl http://localhost:8081/metrics

# 验证指标配置
grep -A 10 "monitoring:" config.yaml

# 检查指标收集间隔
curl http://localhost:8081/v1/status | jq '.metrics_collection_stats'
```

### 日志分析
```bash
# 查看服务日志
tail -f logs/miniodb.log

# 过滤错误日志
grep "ERROR" logs/miniodb.log | tail -20

# 查看表级操作日志
grep "table:" logs/miniodb.log | tail -20

# 监控查询性能日志
grep "query duration" logs/miniodb.log | tail -20

# 查看备份恢复日志
grep -E "(backup|restore)" logs/miniodb.log | tail -20
```

### 性能调优
```bash
# 查看缓冲区使用情况
curl http://localhost:8081/v1/status | jq '.buffer_stats'

# 监控连接池使用率
curl http://localhost:8081/v1/metrics | jq '.connection_pool_metrics'

# 查看查询缓存效果
curl http://localhost:8081/v1/metrics | jq '.cache_metrics'

# 分析慢查询
grep "slow query" logs/miniodb.log | tail -10
```

更多故障排除请参考：[故障排除指南](docs/troubleshooting.md)
