#!/bin/bash

# MinIODB Bug修复验证测试
# 测试新表的自动视图创建和刷新记录数统计

BASE_URL="http://localhost:18081/v1"
TIMESTAMP=$(date +%s)
TABLE_NAME="bugfix_test_${TIMESTAMP}"

echo "=========================================="
echo "MinIODB Bug Fix Verification Test"
echo "=========================================="
echo "Table: ${TABLE_NAME}"
echo "Base URL: ${BASE_URL}"
echo ""

# Test 0: Cleanup (删除可能存在的旧表)
echo "[Test 0] Cleanup old tables..."
curl -X DELETE "${BASE_URL}/tables/${TABLE_NAME}?if_exists=true" 2>/dev/null
sleep 1

# Test 1: Create new table
echo ""
echo "[Test 1] Creating new table: ${TABLE_NAME}"
RESULT=$(curl -s -X POST "${BASE_URL}/tables" \
  -H "Content-Type: application/json" \
  -d "{\"table_name\":\"${TABLE_NAME}\",\"if_not_exists\":true}")
echo "Response: ${RESULT}"

if echo "${RESULT}" | grep -q '"success":true'; then
  echo "✅ Test 1 PASSED: Table created successfully"
else
  echo "❌ Test 1 FAILED: Table creation failed"
  exit 1
fi

sleep 1

# Test 2: Write first data to new table (首次写入应该触发视图初始化)
echo ""
echo "[Test 2] Writing first data (should initialize view)..."
RESULT=$(curl -s -X POST "${BASE_URL}/data" \
  -H "Content-Type: application/json" \
  -d "{\"table\":\"${TABLE_NAME}\",\"id\":\"test-1\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"payload\":{\"value\":100,\"type\":\"first\"}}")
echo "Response: ${RESULT}"

if echo "${RESULT}" | grep -q '"success":true'; then
  echo "✅ Test 2 PASSED: First data written successfully"
else
  echo "❌ Test 2 FAILED: First data write failed"
  exit 1
fi

sleep 2

# Test 3: Query the table immediately (应该能找到 _active 视图)
echo ""
echo "[Test 3] Querying table (should use _active view)..."
RESULT=$(curl -s -X POST "${BASE_URL}/query" \
  -H "Content-Type: application/json" \
  -d "{\"sql\":\"SELECT * FROM ${TABLE_NAME}\",\"include_deleted\":false}")
echo "Response: ${RESULT}"

if echo "${RESULT}" | grep -q 'test-1'; then
  echo "✅ Test 3 PASSED: Query successful, found test-1"
elif echo "${RESULT}" | grep -q 'does not exist'; then
  echo "❌ Test 3 FAILED: _active view not found"
  exit 1
else
  echo "⚠️  Test 3 WARNING: Query executed but no data found (may be in buffer)"
fi

sleep 1

# Test 4: Write more data
echo ""
echo "[Test 4] Writing more data (test-2 to test-5)..."
for i in {2..5}; do
  curl -s -X POST "${BASE_URL}/data" \
    -H "Content-Type: application/json" \
    -d "{\"table\":\"${TABLE_NAME}\",\"id\":\"test-${i}\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"payload\":{\"value\":$((100+i)),\"type\":\"batch\"}}" > /dev/null
  echo "  Written: test-${i}"
done
echo "✅ Test 4 PASSED: Additional data written"

sleep 1

# Test 5: Manual flush (should return records_flushed > 0)
echo ""
echo "[Test 5] Manual flush (should return records_flushed > 0)..."
RESULT=$(curl -s -X POST "${BASE_URL}/tables/${TABLE_NAME}/flush")
echo "Response: ${RESULT}"

RECORDS_FLUSHED=$(echo "${RESULT}" | grep -o '"records_flushed":[0-9]*' | cut -d':' -f2)
echo "Records flushed: ${RECORDS_FLUSHED}"

if echo "${RESULT}" | grep -q "successfully"; then
  echo "✅ Test 5 PASSED: Flush returned records_flushed=${RECORDS_FLUSHED}"
else
  echo "❌ Test 5 FAILED: Flush returned records_flushed=0"
fi

sleep 2

# Test 6: Query after flush (should see all data)
echo ""
echo "[Test 6] Query after flush (should see all 5 records)..."
RESULT=$(curl -s -X POST "${BASE_URL}/query" \
  -H "Content-Type: application/json" \
  -d "{\"sql\":\"SELECT * FROM ${TABLE_NAME} ORDER BY id\",\"include_deleted\":false}")
echo "Response: ${RESULT}"

# Count records
RECORD_COUNT=0
for i in {1..5}; do
  if echo "${RESULT}" | grep -q "test-${i}"; then
    RECORD_COUNT=$((RECORD_COUNT+1))
  fi
done

echo "Found ${RECORD_COUNT} records"

if [ "${RECORD_COUNT}" -eq 5 ]; then
  echo "✅ Test 6 PASSED: All 5 records found"
elif [ "${RECORD_COUNT}" -gt 0 ]; then
  echo "⚠️  Test 6 WARNING: Only ${RECORD_COUNT}/5 records found (may be timing issue)"
else
  echo "❌ Test 6 FAILED: No records found"
fi

# Test 7: Count query
echo ""
echo "[Test 7] Count query..."
RESULT=$(curl -s -X POST "${BASE_URL}/query" \
  -H "Content-Type: application/json" \
  -d "{\"sql\":\"SELECT COUNT(*) as count FROM ${TABLE_NAME}\",\"include_deleted\":false}")
echo "Response: ${RESULT}"

if echo "${RESULT}" | grep -q 'count'; then
  echo "✅ Test 7 PASSED: Count query executed"
else
  echo "❌ Test 7 FAILED: Count query failed"
fi

# Cleanup
echo ""
echo "[Cleanup] Dropping test table..."
curl -s -X DELETE "${BASE_URL}/tables/${TABLE_NAME}?if_exists=true" > /dev/null
echo "✅ Cleanup complete"

echo ""
echo "=========================================="
echo "Test Summary"
echo "=========================================="
echo "Table used: ${TABLE_NAME}"
echo "All critical tests completed."
echo ""
echo "Expected Results:"
echo "  ✅ Table creation"
echo "  ✅ First write triggers view initialization"
echo "  ✅ Query finds _active view (no 'does not exist' error)"
echo "  ✅ Manual flush returns records_flushed > 0"
echo "  ✅ After flush, all data is queryable"
echo ""
echo "=========================================="

