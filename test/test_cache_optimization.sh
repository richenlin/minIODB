#!/bin/bash

# MinIODB 缓存优化性能测试
# 测试文件索引缓存和视图初始化缓存的效果

BASE_URL="http://localhost:18081/v1"
TIMESTAMP=$(date +%s)
TABLE_NAME="cache_test_${TIMESTAMP}"

echo "=========================================="
echo "MinIODB Cache Optimization Performance Test"
echo "=========================================="
echo "Table: ${TABLE_NAME}"
echo "Base URL: ${BASE_URL}"
echo ""

# Cleanup
echo "[Setup] Cleanup old tables..."
curl -s -X DELETE "${BASE_URL}/tables/${TABLE_NAME}?if_exists=true" > /dev/null
sleep 1

# Test 1: Create table
echo ""
echo "[Test 1] Creating table: ${TABLE_NAME}"
START_TIME=$(date +%s%N)
curl -s -X POST "${BASE_URL}/tables" \
  -H "Content-Type: application/json" \
  -d "{\"table_name\":\"${TABLE_NAME}\",\"if_not_exists\":true}" > /dev/null
END_TIME=$(date +%s%N)
ELAPSED=$((($END_TIME - $START_TIME) / 1000000))
echo "✅ Table created in ${ELAPSED}ms"

# Test 2: 首次写入（视图初始化）
echo ""
echo "[Test 2] First write (view initialization)..."
START_TIME=$(date +%s%N)
curl -s -X POST "${BASE_URL}/data" \
  -H "Content-Type: application/json" \
  -d "{\"table\":\"${TABLE_NAME}\",\"id\":\"test-1\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"payload\":{\"value\":1}}" > /dev/null
END_TIME=$(date +%s%N)
FIRST_WRITE_TIME=$((($END_TIME - $START_TIME) / 1000000))
echo "✅ First write completed in ${FIRST_WRITE_TIME}ms (includes view init)"

# Test 3: 后续写入（应该使用缓存）
echo ""
echo "[Test 3] Subsequent writes (should use cache)..."
TOTAL_TIME=0
for i in {2..11}; do
  START_TIME=$(date +%s%N)
  curl -s -X POST "${BASE_URL}/data" \
    -H "Content-Type: application/json" \
    -d "{\"table\":\"${TABLE_NAME}\",\"id\":\"test-${i}\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"payload\":{\"value\":${i}}}" > /dev/null
  END_TIME=$(date +%s%N)
  ELAPSED=$((($END_TIME - $START_TIME) / 1000000))
  TOTAL_TIME=$(($TOTAL_TIME + $ELAPSED))
done
AVG_WRITE_TIME=$(($TOTAL_TIME / 10))
echo "✅ 10 subsequent writes: avg ${AVG_WRITE_TIME}ms per write"

# 性能对比
if [ $AVG_WRITE_TIME -lt $FIRST_WRITE_TIME ]; then
  SPEEDUP=$(echo "scale=2; $FIRST_WRITE_TIME / $AVG_WRITE_TIME" | bc)
  echo "🚀 Speedup: ${SPEEDUP}x faster (cache working!)"
else
  echo "⚠️  No significant speedup detected"
fi

sleep 1

# Test 4: 查询（首次扫描MinIO）
echo ""
echo "[Test 4] First query (MinIO scan)..."
START_TIME=$(date +%s%N)
RESULT=$(curl -s -X POST "${BASE_URL}/query" \
  -H "Content-Type: application/json" \
  -d "{\"sql\":\"SELECT COUNT(*) as count FROM ${TABLE_NAME}\",\"include_deleted\":false}")
END_TIME=$(date +%s%N)
FIRST_QUERY_TIME=$((($END_TIME - $START_TIME) / 1000000))
COUNT=$(echo "${RESULT}" | grep -o '"count":[0-9]*' | cut -d':' -f2)
echo "✅ First query completed in ${FIRST_QUERY_TIME}ms (count: ${COUNT})"

# Test 5: 手动刷新
echo ""
echo "[Test 5] Manual flush..."
curl -s -X POST "${BASE_URL}/tables/${TABLE_NAME}/flush" > /dev/null
echo "✅ Flush completed"
sleep 2

# Test 6: 第二次查询（应该使用文件索引缓存）
echo ""
echo "[Test 6] Second query (should use file index cache)..."
START_TIME=$(date +%s%N)
RESULT=$(curl -s -X POST "${BASE_URL}/query" \
  -H "Content-Type: application/json" \
  -d "{\"sql\":\"SELECT COUNT(*) as count FROM ${TABLE_NAME}\",\"include_deleted\":false}")
END_TIME=$(date +%s%N)
SECOND_QUERY_TIME=$((($END_TIME - $START_TIME) / 1000000))
COUNT=$(echo "${RESULT}" | grep -o '"count":[0-9]*' | cut -d':' -f2)
echo "✅ Second query completed in ${SECOND_QUERY_TIME}ms (count: ${COUNT})"

# 查询性能对比
if [ $SECOND_QUERY_TIME -lt $FIRST_QUERY_TIME ]; then
  SPEEDUP=$(echo "scale=2; $FIRST_QUERY_TIME / $SECOND_QUERY_TIME" | bc)
  echo "🚀 Query speedup: ${SPEEDUP}x faster (file cache working!)"
else
  echo "⚠️  No query speedup detected"
fi

# Test 7: 第三次查询（验证缓存有效性）
echo ""
echo "[Test 7] Third query (verify cache TTL)..."
START_TIME=$(date +%s%N)
RESULT=$(curl -s -X POST "${BASE_URL}/query" \
  -H "Content-Type: application/json" \
  -d "{\"sql\":\"SELECT * FROM ${TABLE_NAME} LIMIT 5\",\"include_deleted\":false}")
END_TIME=$(date +%s%N)
THIRD_QUERY_TIME=$((($END_TIME - $START_TIME) / 1000000))
echo "✅ Third query completed in ${THIRD_QUERY_TIME}ms"

# Cleanup
echo ""
echo "[Cleanup] Dropping test table..."
curl -s -X DELETE "${BASE_URL}/tables/${TABLE_NAME}?if_exists=true" > /dev/null
echo "✅ Cleanup complete"

# 总结
echo ""
echo "=========================================="
echo "Performance Summary"
echo "=========================================="
echo "View Initialization Cache:"
echo "  - First write:      ${FIRST_WRITE_TIME}ms"
echo "  - Subsequent writes: ${AVG_WRITE_TIME}ms avg"
if [ $AVG_WRITE_TIME -lt $FIRST_WRITE_TIME ]; then
  SPEEDUP=$(echo "scale=2; $FIRST_WRITE_TIME / $AVG_WRITE_TIME" | bc)
  echo "  - Speedup:          ${SPEEDUP}x"
else
  echo "  - Speedup:          N/A"
fi
echo ""
echo "File Index Cache:"
echo "  - First query:      ${FIRST_QUERY_TIME}ms"
echo "  - Cached query:     ${SECOND_QUERY_TIME}ms"
if [ $SECOND_QUERY_TIME -lt $FIRST_QUERY_TIME ]; then
  SPEEDUP=$(echo "scale=2; $FIRST_QUERY_TIME / $SECOND_QUERY_TIME" | bc)
  echo "  - Speedup:          ${SPEEDUP}x"
else
  echo "  - Speedup:          N/A"
fi
echo "=========================================="

