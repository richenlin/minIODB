#!/bin/bash

# 分布式模式API功能测试脚本
echo "======================================"
echo "MinIODB 分布式模式 API功能测试"
echo "======================================"
echo ""

BASE_URL="http://localhost:28081/v1"

# 1. 健康检查
echo "✅ 测试1: 健康检查"
curl -s $BASE_URL/health
echo -e "\n\n"
sleep 1

# 2. 创建表
echo "✅ 测试2: 创建表 (distributed_test)"
curl -X POST $BASE_URL/tables \
  -H "Content-Type: application/json" \
  -d '{"table_name": "distributed_test", "config": {"buffer_size": 1000, "flush_interval_seconds": 30}}'
echo -e "\n\n"
sleep 2

# 3. 列出表
echo "✅ 测试3: 列出所有表"
curl -s $BASE_URL/tables
echo -e "\n\n"
sleep 1

# 4. 获取表详情
echo "✅ 测试4: 获取表详情"
curl -s $BASE_URL/tables/distributed_test
echo -e "\n\n"
sleep 1

# 5. 写入数据（测试分布式写入）
echo "✅ 测试5: 写入数据 (10条记录)"
for i in {1..10}; do
  curl -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"distributed_test\", \"id\": \"dist-record-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"message\": \"分布式测试记录 $i\", \"index\": $i, \"node\": \"node-1\"}}" \
    2>/dev/null
  echo ""
done
sleep 2

# 6. 查询数据（测试混合查询）
echo -e "\n✅ 测试6: 查询数据 (应能看到缓冲区数据)"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM distributed_test"}'
echo -e "\n\n"
sleep 1

# 7. 查询详细数据
echo "✅ 测试7: 查询详细数据 (前5条)"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM distributed_test ORDER BY id LIMIT 5"}'
echo -e "\n\n"
sleep 1

# 8. 手动刷新
echo "✅ 测试8: 手动刷新表数据"
curl -X POST $BASE_URL/tables/distributed_test/flush
echo -e "\n\n"
sleep 3

# 9. 刷新后查询
echo "✅ 测试9: 刷新后查询 (验证持久化)"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM distributed_test"}'
echo -e "\n\n"
sleep 1

# 10. 更新数据
echo "✅ 测试10: 更新数据"
curl -X PUT $BASE_URL/data \
  -H "Content-Type: application/json" \
  -d '{"table": "distributed_test", "id": "dist-record-1", "payload": {"message": "更新后的记录", "updated": true}}'
echo -e "\n\n"
sleep 1

# 11. 验证更新
echo "✅ 测试11: 验证数据更新"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM distributed_test WHERE id = '\''dist-record-1'\''"}'
echo -e "\n\n"
sleep 1

# 12. 删除数据
echo "✅ 测试12: 删除数据"
curl -X DELETE "$BASE_URL/data?table=distributed_test&id=dist-record-10"
echo -e "\n\n"
sleep 1

# 13. 验证删除
echo "✅ 测试13: 验证删除 (应剩9条记录)"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM distributed_test"}'
echo -e "\n\n"
sleep 1

# 14. 获取系统状态
echo "✅ 测试14: 获取系统状态"
curl -s $BASE_URL/status | head -c 500
echo -e "...\n\n"
sleep 1

# 15. 获取性能指标
echo "✅ 测试15: 获取性能指标（包含混合查询指标）"
curl -s $BASE_URL/metrics | grep -E '"hybrid_queries"|"buffer_hits"|"buffer_rows"|"total_queries"' | head -5
echo -e "\n\n"
sleep 1

# 16. 再次写入数据（测试连续写入）
echo "✅ 测试16: 连续写入测试 (5条记录)"
for i in {11..15}; do
  curl -X POST $BASE_URL/data \
    -H "Content-Type: application/json" \
    -d "{\"table\": \"distributed_test\", \"id\": \"dist-record-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"payload\": {\"message\": \"连续写入 $i\", \"batch\": 2}}" \
    2>/dev/null
done
echo -e "\n"
sleep 1

# 17. 最终查询统计
echo "✅ 测试17: 最终查询统计 (应有14条记录)"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT COUNT(*) as total FROM distributed_test"}'
echo -e "\n\n"
sleep 1

# 18. 按条件查询
echo "✅ 测试18: 按条件查询 (WHERE clause)"
curl -X POST $BASE_URL/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT id, payload FROM distributed_test WHERE id LIKE '\''dist-record-1%'\'' ORDER BY id"}'
echo -e "\n\n"
sleep 1

# 19. 检查Redis连接
echo "✅ 测试19: 验证Redis连接 (分布式模式特有)"
docker exec miniodb-redis redis-cli -a redis123 ping 2>/dev/null
echo ""
docker exec miniodb-redis redis-cli -a redis123 keys "table:*" 2>/dev/null | head -5
echo -e "\n"

# 20. 检查MinIO bucket
echo "✅ 测试20: 检查MinIO存储"
curl -s http://localhost:9000/minio/health/live && echo " - MinIO主存储健康"
curl -s http://localhost:9002/minio/health/live && echo " - MinIO备份存储健康"
echo ""

echo "======================================"
echo "分布式模式测试完成！"
echo "======================================"
echo ""
echo "测试总结："
echo "- REST API: http://localhost:28081"
echo "- gRPC API: localhost:28080"
echo "- Metrics: http://localhost:29090"
echo "- MinIO Console: http://localhost:9001"
echo "- MinIO Backup Console: http://localhost:9003"
echo "- Redis: localhost:6379"

