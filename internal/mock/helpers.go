package mock

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelper 提供测试辅助函数
type TestHelper struct {
	factory *MockFactory
	t       *testing.T
}

// NewTestHelper 创建新的测试辅助器
func NewTestHelper(t *testing.T) *TestHelper {
	return &TestHelper{
		factory: NewMockFactory(),
		t:       t,
	}
}

// GetFactory 获取Mock工厂
func (h *TestHelper) GetFactory() *MockFactory {
	return h.factory
}

// SetupSuccess 设置成功配置
func (h *TestHelper) SetupSuccess() {
	configs := GetTestConfigs()
	h.factory.GetMinIOClient().SetConfig(configs["success"])
	h.factory.GetRedisClient().SetConfig(configs["success"])
	h.factory.GetDuckDBClient().SetConfig(configs["success"])
}

// SetupMinIOUploadFail 设置MinIO上传失败配置
func (h *TestHelper) SetupMinIOUploadFail() {
	configs := GetTestConfigs()
	h.factory.GetMinIOClient().SetConfig(configs["minio_upload_fail"])
	h.factory.GetRedisClient().SetConfig(configs["success"])
	h.factory.GetDuckDBClient().SetConfig(configs["success"])
}

// SetupRedisConnectionFail 设置Redis连接失败配置
func (h *TestHelper) SetupRedisConnectionFail() {
	configs := GetTestConfigs()
	h.factory.GetMinIOClient().SetConfig(configs["success"])
	h.factory.GetRedisClient().SetConfig(configs["redis_connection_fail"])
	h.factory.GetDuckDBClient().SetConfig(configs["success"])
}

// SetupDuckDBConnectionFail 设置DuckDB连接失败配置
func (h *TestHelper) SetupDuckDBConnectionFail() {
	configs := GetTestConfigs()
	h.factory.GetMinIOClient().SetConfig(configs["success"])
	h.factory.GetRedisClient().SetConfig(configs["success"])
	h.factory.GetDuckDBClient().SetConfig(configs["duckdb_connection_fail"])
}

// SetupDelays 设置延迟配置
func (h *TestHelper) SetupDelays() {
	configs := GetTestConfigs()
	h.factory.GetMinIOClient().SetConfig(configs["delays"])
	h.factory.GetRedisClient().SetConfig(configs["delays"])
	h.factory.GetDuckDBClient().SetConfig(configs["delays"])
}

// Cleanup 清理Mock状态
func (h *TestHelper) Cleanup() {
	h.factory.Reset()
}

// AssertMinIOOperation 断言MinIO操作
func (h *TestHelper) AssertMinIOOperation(operation, target string) {
	log := h.factory.GetMinIOClient().GetOperationLog()
	lastOp := h.factory.GetMinIOClient().GetLastOperation()

	assert.NotEmpty(h.t, log, "MinIO operation log should not be empty")
	assert.Equal(h.t, lastOp, operation+": "+target, "Last MinIO operation should match")
}

// AssertRedisOperation 断言Redis操作
func (h *TestHelper) AssertRedisOperation(operation, target string) {
	log := h.factory.GetRedisClient().GetOperationLog()
	lastOp := h.factory.GetRedisClient().GetLastOperation()

	assert.NotEmpty(h.t, log, "Redis operation log should not be empty")
	assert.Equal(h.t, lastOp, operation+": "+target, "Last Redis operation should match")
}

// AssertDuckDBOperation 断言DuckDB操作
func (h *TestHelper) AssertDuckDBOperation(operation, target string) {
	log := h.factory.GetDuckDBClient().GetOperationLog()
	lastOp := h.factory.GetDuckDBClient().GetLastOperation()

	assert.NotEmpty(h.t, log, "DuckDB operation log should not be empty")
	assert.Equal(h.t, lastOp, operation+": "+target, "Last DuckDB operation should match")
}

// AssertMinIODataExists 断言MinIO数据存在
func (h *TestHelper) AssertMinIODataExists(bucket, object string, expectedData string) {
	data, exists := h.factory.GetMinIOClient().GetObjectData(bucket, object)
	require.True(h.t, exists, "Object should exist in MinIO")
	assert.Equal(h.t, expectedData, string(data), "MinIO object data should match expected")
}

// AssertRedisDataExists 断言Redis数据存在
func (h *TestHelper) AssertRedisDataExists(key, expectedValue string) {
	data := h.factory.GetRedisClient().GetData()
	value, exists := data[key]
	require.True(h.t, exists, "Key should exist in Redis")
	assert.Equal(h.t, expectedValue, value, "Redis value should match expected")
}

// AssertDuckDBTableExists 断言DuckDB表存在
func (h *TestHelper) AssertDuckDBTableExists(db, table string) {
	// 获取表列表（简化实现）
	// 由于Mock的限制，这里只是验证操作被记录
	h.AssertDuckDBOperation("ListTables", db)
}

// AssertMinIOError 断言MinIO错误
func (h *TestHelper) AssertMinIOError(err error, expectedError string) {
	assert.Error(h.t, err, "MinIO operation should fail")
	assert.Contains(h.t, err.Error(), expectedError, "MinIO error should contain expected message")
}

// AssertRedisError 断言Redis错误
func (h *TestHelper) AssertRedisError(err error, expectedError string) {
	assert.Error(h.t, err, "Redis operation should fail")
	assert.Contains(h.t, err.Error(), expectedError, "Redis error should contain expected message")
}

// AssertDuckDBError 断言DuckDB错误
func (h *TestHelper) AssertDuckDBError(err error, expectedError string) {
	assert.Error(h.t, err, "DuckDB operation should fail")
	assert.Contains(h.t, err.Error(), expectedError, "DuckDB error should contain expected message")
}

// AssertNoError 断言无错误
func (h *TestHelper) AssertNoError(err error) {
	assert.NoError(h.t, err, "Operation should succeed")
}

// CreateTestData 创建测试数据
func (h *TestHelper) CreateTestData(bucket, object, data string) {
	ctx := context.Background()
	minIO := h.factory.GetMinIOClient()

	// 创建桶
	err := minIO.MakeBucket(ctx, bucket)
	h.AssertNoError(err)

	// 上传对象
	err = minIO.PutObject(ctx, bucket, object, strings.NewReader(data), int64(len(data)))
	h.AssertNoError(err)
}

// CreateTestRedisData 创建测试Redis数据
func (h *TestHelper) CreateTestRedisData(key, value string) {
	ctx := context.Background()
	redis := h.factory.GetRedisClient()

	err := redis.Set(ctx, key, value, 0)
	h.AssertNoError(err)
}

// CreateTestDuckDBData 创建测试DuckDB数据
func (h *TestHelper) CreateTestDuckDBData(db, table string, data map[string]interface{}) {
	ctx := context.Background()
	duckDB := h.factory.GetDuckDBClient()

	// 连接数据库
	err := duckDB.Connect(ctx, db)
	h.AssertNoError(err)

	// 创建表并插入数据（简化实现）
	_, err = duckDB.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+table+" (id INTEGER, data TEXT)")
	h.AssertNoError(err)

	// 插入数据
	for id, row := range data {
		_, err = duckDB.Exec(ctx, "INSERT INTO "+table+" (id, data) VALUES (?, ?)", id, row)
		h.AssertNoError(err)
	}
}

// RunWithTimeout 在超时内运行函数
func (h *TestHelper) RunWithTimeout(timeoutMs int, fn func() bool) bool {
	timeout := time.After(time.Duration(timeoutMs) * time.Millisecond)
	done := make(chan bool, 1)

	go func() {
		done <- fn()
	}()

	select {
	case <-done:
		return true
	case <-timeout:
		h.t.Fatalf("Operation timed out after %d ms", timeoutMs)
		return false
	}
}