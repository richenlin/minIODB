#!/bin/bash

echo "=== MinIODB 性能基线建立 ==="

BASE_URL="http://localhost:8080"
TABLE_NAME="baseline_test"
NUM_RECORDS=1000

# 检查服务是否运行
echo "1. 检查服务状态"
curl -f "$BASE_URL/v1/health" > /dev/null 2>&1
if [ $? -ne 0 ]; then
    echo "ERROR: MinIODB服务未运行，请先启动服务"
    exit 1
fi
echo "✓ 服务正常运行"

# 创建测试表
echo "2. 创建测试表: $TABLE_NAME"
curl -s -X POST "$BASE_URL/v1/tables" \
  -H "Content-Type: application/json" \
  -d "{
    \"table_name\": \"$TABLE_NAME\",
    \"if_not_exists\": true,
    \"config\": {
      \"buffer_size\": 100,
      \"flush_interval_seconds\": 10,
      \"retention_days\": 30,
      \"backup_enabled\": false
    }
  }" | jq .

# 写入性能测试
echo "3. 写入性能测试 ($NUM_RECORDS 条记录)"
start_time=$(date +%s.%N)

for i in $(seq 1 $NUM_RECORDS); do
    curl -s -X POST "$BASE_URL/v1/data" \
      -H "Content-Type: application/json" \
      -d "{
        \"table\": \"$TABLE_NAME\",
        \"id\": \"baseline-$i\",
        \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
        \"payload\": {
          \"test_id\": $i,
          \"category\": \"baseline\",
          \"value\": $(($RANDOM % 1000)),
          \"description\": \"Performance baseline test record $i\"
        }
      }" > /dev/null
    
    # 显示进度
    if [ $((i % 100)) -eq 0 ]; then
        echo "已写入 $i/$NUM_RECORDS 条记录..."
    fi
done

end_time=$(date +%s.%N)
write_duration=$(echo "$end_time - $start_time" | bc)
write_tps=$(echo "scale=2; $NUM_RECORDS / $write_duration" | bc)

echo "✓ 写入完成"
echo "写入性能: $write_tps TPS (每秒事务数)"

# 手动刷新确保数据可查
echo "4. 刷新缓冲区"
curl -s -X POST "$BASE_URL/v1/flush" > /dev/null

sleep 2

# 查询性能测试
echo "5. 查询性能测试"
queries=(
    "SELECT COUNT(*) as total FROM $TABLE_NAME"
    "SELECT * FROM $TABLE_NAME LIMIT 10"
    "SELECT category, COUNT(*) as count FROM $TABLE_NAME GROUP BY category"
    "SELECT AVG(CAST(payload->>'value' AS INTEGER)) as avg_value FROM $TABLE_NAME"
    "SELECT * FROM $TABLE_NAME WHERE payload->>'test_id' = '500'"
)

total_query_time=0
query_count=0

for sql in "${queries[@]}"; do
    echo "执行查询: $sql"
    start_time=$(date +%s.%N)
    
    result=$(curl -s -X POST "$BASE_URL/v1/query" \
      -H "Content-Type: application/json" \
      -d "{\"sql\": \"$sql\"}")
    
    end_time=$(date +%s.%N)
    query_duration=$(echo "$end_time - $start_time" | bc)
    total_query_time=$(echo "$total_query_time + $query_duration" | bc)
    query_count=$((query_count + 1))
    
    echo "查询时间: ${query_duration}s"
    echo "结果: $(echo $result | jq -r .result_json | head -c 100)..."
    echo ""
done

avg_query_time=$(echo "scale=3; $total_query_time / $query_count" | bc)
qps=$(echo "scale=2; $query_count / $total_query_time" | bc)

echo "✓ 查询完成"
echo "平均查询时间: ${avg_query_time}s"
echo "查询性能: $qps QPS (每秒查询数)"

# 生成性能基线报告
echo "6. 生成性能基线报告"
cat > baseline_report.md << EOF
# MinIODB 性能基线报告

## 测试环境
- 测试时间: $(date)
- 测试表: $TABLE_NAME
- 测试记录数: $NUM_RECORDS

## 性能指标

### 写入性能
- **TPS (每秒事务数)**: $write_tps
- **总写入时间**: ${write_duration}s
- **记录数**: $NUM_RECORDS 条

### 查询性能
- **QPS (每秒查询数)**: $qps
- **平均查询时间**: ${avg_query_time}s
- **查询类型**: 统计、分组、过滤、聚合等

## 系统状态
$(curl -s "$BASE_URL/v1/stats" | jq .)

## 结论
基线性能已建立，可用于后续性能对比和回归检测。
EOF

echo "✓ 性能基线报告已生成: baseline_report.md"

echo "=== 性能基线建立完成 ===" 
 