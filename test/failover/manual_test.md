# MinIO双池故障切换手动测试指南

## 前提条件
服务已启动：
```bash
cd deploy/docker
docker-compose up -d
```

## 测试步骤

### 步骤1: 验证服务正常
```bash
# 检查所有服务状态
docker-compose ps

# 验证健康检查
curl http://localhost:8081/v1/health | jq .

# 检查故障切换管理器启动
docker-compose logs miniodb | grep "Failover manager"
# 预期输出: "Failover manager started successfully, sync_workers: 3"
```

### 步骤2: 检查当前池状态
```bash
# 查看日志确认使用主池
docker-compose logs miniodb | grep "using.*pool\|Using.*pool"

# 检查metrics（如果已注册）
curl -s http://localhost:9090/metrics | grep miniodb_pool_current_active
# 预期: miniodb_pool_current_active 0  （0=主池，1=备份池）
```

### 步骤3: 创建测试表并写入数据
```bash
# 创建表
curl -X POST http://localhost:8081/v1/tables \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "test_table",
    "config": {
      "buffer_size": 5000,
      "flush_interval_seconds": 15,
      "backup_enabled": true
    }
  }' | jq .

# 写入100条数据
for i in {1..100}; do
  curl -s -X POST http://localhost:8081/v1/data \
    -H "Content-Type: application/json" \
    -d "{
      \"table\": \"test_table\",
      \"id\": \"T$(printf '%05d' $i)\",
      \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
      \"payload\": {\"value\": $i}
    }" > /dev/null && echo -n "."
done
echo ""
```

### 步骤4: 触发buffer刷新并验证上传
```bash
# 等待15秒触发自动刷新
sleep 15

# 检查刷新日志
docker-compose logs miniodb --since 20s | grep -E "upload|flush|Parquet"

# 应该看到类似的日志：
# "Successfully uploaded file" 或 "FPutObject"
```

### 步骤5: 模拟主MinIO故障
```bash
# 停止主MinIO
docker-compose stop minio

# 验证主MinIO已停止
docker-compose ps minio
```

### 步骤6: 写入数据触发故障切换
```bash
# 写入新数据
for i in {200..250}; do
  curl -s -X POST http://localhost:8081/v1/data \
    -H "Content-Type: application/json" \
    -d "{
      \"table\": \"test_table\",
      \"id\": \"T$(printf '%05d' $i)\",
      \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
      \"payload\": {\"value\": $i}
    }" > /dev/null && echo -n "."
done
echo ""

# 等待刷新并检查故障切换日志
sleep 20
docker-compose logs miniodb --since 25s | grep -E "FAILOVER|Primary pool.*failed|Switched"

# 预期输出：
# "Primary pool operation failed, trying to switch to backup pool"
# "FAILOVER: Switched from primary pool to backup pool"
```

### 步骤7: 验证已切换到备份池
```bash
# 检查当前状态
curl -s http://localhost:9090/metrics | grep miniodb_pool_current_active
# 预期: miniodb_pool_current_active 1  （1=备份池）

# 检查故障切换次数
curl -s http://localhost:9090/metrics | grep miniodb_pool_failover_total
# 预期: miniodb_pool_failover_total{from_state="primary",to_state="backup"} 1
```

### 步骤8: 验证备份池正常工作
```bash
# 继续写入数据（应该写入备份池）
for i in {300..320}; do
  curl -s -X POST http://localhost:8081/v1/data \
    -H "Content-Type: application/json" \
    -d "{
      \"table\": \"test_table\",
      \"id\": \"T$(printf '%05d' $i)\",
      \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
      \"payload\": {\"value\": $i}
    }" > /dev/null && echo -n "."
done
echo ""

# 等待刷新
sleep 20

# 检查备份MinIO中的数据
docker-compose exec minio-backup ls /data/miniodb-backup/test_table/ 2>/dev/null || echo "需要手动检查"
```

### 步骤9: 恢复主MinIO
```bash
# 启动主MinIO
docker-compose start minio

# 等待健康检查
sleep 20

# 检查恢复日志
docker-compose logs miniodb --since 25s | grep -E "RECOVERY|switched back|Primary pool recovered"

# 预期输出：
# "RECOVERY: Primary pool recovered, switched back from backup pool"
```

### 步骤10: 验证已切回主池
```bash
# 检查当前状态
curl -s http://localhost:9090/metrics | grep miniodb_pool_current_active
# 预期: miniodb_pool_current_active 0  （0=主池）

# 检查恢复次数
curl -s http://localhost:9090/metrics | grep "from_state=\"backup\""
# 预期: miniodb_pool_failover_total{from_state="backup",to_state="primary"} 1
```

### 步骤11: 验证异步同步
```bash
# 检查异步同步指标
curl -s http://localhost:9090/metrics | grep backup_sync

# 预期输出：
# miniodb_backup_sync_queue_size 0
# miniodb_backup_sync_success_total X  (X > 0)
# miniodb_backup_sync_failed_total 0
```

### 步骤12: 数据一致性验证
```bash
# 检查主MinIO数据
docker-compose exec minio mc ls local/miniodb-data/test_table/

# 检查备份MinIO数据
docker-compose exec minio-backup mc ls local/miniodb-backup/test_table/

# 比对文件数量应该相近（考虑异步延迟）
```

## 预期结果

| 测试项 | 预期结果 | 验证方式 |
|--------|---------|---------|
| 故障切换管理器启动 | 成功启动，3个worker | 日志包含"Failover manager started" |
| 主池故障检测 | 自动检测并切换 | 日志包含"FAILOVER: Switched" |
| 备份池接管 | 数据正常写入备份池 | 备份MinIO中有数据 |
| 主池恢复检测 | 自动切回主池 | 日志包含"RECOVERY: Primary pool recovered" |
| 异步同步 | 数据同步到备份池 | backup_sync_success_total > 0 |
| Prometheus指标 | 所有指标正常更新 | metrics中包含failover指标 |

## 故障排查

### 问题1: Buffer数据未刷新
**解决方案**: 
- 检查buffer配置: `docker-compose logs miniodb | grep "buffer initialized"`
- 手动等待flush_interval时间
- 或写入超过buffer_size的数据

### 问题2: 故障切换未触发
**可能原因**:
- 主MinIO未真正故障（检查网络连接）
- Buffer数据未刷新到MinIO（无MinIO操作）
- Failover被禁用（检查配置和日志）

**解决方案**:
```bash
# 确认failover启用
docker-compose logs miniodb | grep -i "failover.*enabled\|failover.*disabled"

# 确认备份池初始化
docker-compose logs miniodb | grep "backup.*pool.*created\|Backup MinIO"
```

### 问题3: Metrics指标未注册
**已修复**: failover_manager.Start()中主动初始化指标
**验证**: 重启服务后检查metrics

## 清理
```bash
# 停止所有服务
docker-compose down

# 清理数据卷（可选）
docker-compose down -v
```
