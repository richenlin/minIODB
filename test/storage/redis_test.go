package storage

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// 初始化测试logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
	})
}

// TestRedisClientMockBasicOperations 使用Mock Redis测试基本操作
func TestRedisClientMockBasicOperations(t *testing.T) {
	mockRedis := NewMockRedisClient()

	ctx := context.Background()

	// 测试连接
	err := mockRedis.Ping(ctx)
	require.NoError(t, err)

	// 设置值
	err = mockRedis.Set(ctx, "test_key", "test_value", time.Hour)
	require.NoError(t, err)

	// 获取值
	value, err := mockRedis.Get(ctx, "test_key")
	require.NoError(t, err)
	assert.Equal(t, "test_value", value)

	// 检查存在性
	exists, err := mockRedis.Exists(ctx, "test_key")
	require.NoError(t, err)
	assert.True(t, exists)

	// 删除键
	err = mockRedis.Del(ctx, "test_key")
	require.NoError(t, err)

	// 再次检查存在性
	exists, err = mockRedis.Exists(ctx, "test_key")
	require.NoError(t, err)
	assert.False(t, exists)

	// 尝试获取已删除的键
	_, err = mockRedis.Get(ctx, "test_key")
	assert.Error(t, err)
}

// TestMockRedisOperations 测试Mock Redis的各种操作
func TestMockRedisOperations(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()

	// 测试整数存储和获取
	err := mockRedis.Set(ctx, "int_key", 123, time.Hour)
	require.NoError(t, err)

	value, err := mockRedis.Get(ctx, "int_key")
	require.NoError(t, err)
	assert.Equal(t, "123", value)

	// 测试浮点数
	err = mockRedis.Set(ctx, "float_key", 45.67, time.Hour)
	require.NoError(t, err)

	value, err = mockRedis.Get(ctx, "float_key")
	require.NoError(t, err)
	assert.Equal(t, "45.67", value)
}

// TestMockRedisExpiration 测试过期功能
func TestMockRedisExpiration(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()

	// 设置短过期时间的键
	err := mockRedis.Set(ctx, "expire_key", "expire_value", time.Millisecond*100)
	require.NoError(t, err)

	// 立即获取应该成功
	value, err := mockRedis.Get(ctx, "expire_key")
	require.NoError(t, err)
	assert.Equal(t, "expire_value", value)

	// 检查TTL
	ttl, err := mockRedis.TTL(ctx, "expire_key")
	require.NoError(t, err)
	assert.True(t, ttl > 0)

	// 等待过期
	time.Sleep(time.Millisecond * 200)

	// 过期后获取应该失败
	_, err = mockRedis.Get(ctx, "expire_key")
	assert.Error(t, err)
}

// TestMockRedisHashOperations 测试哈希操作
func TestMockRedisHashOperations(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()
	hashName := "test_hash"

	// 设置哈希字段
	err := mockRedis.HSet(ctx, hashName, "field1", "value1")
	require.NoError(t, err)
	err = mockRedis.HSet(ctx, hashName, "field2", "value2")
	require.NoError(t, err)

	// 获取单个字段
	value, err := mockRedis.HGet(ctx, hashName, "field1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	// 获取所有字段
	allFields, err := mockRedis.HGetAll(ctx, hashName)
	require.NoError(t, err)
	assert.Len(t, allFields, 2)
	assert.Equal(t, "value1", allFields["field1"])
	assert.Equal(t, "value2", allFields["field2"])
}

// TestMockRedisSetOperations 测试集合操作
func TestMockRedisSetOperations(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()
	setName := "test_set"

	// 添加成员
	err := mockRedis.Sadd(ctx, setName, "member1", "member2", "member3")
	require.NoError(t, err)

	// 检查成员是否存在
	isMember, err := mockRedis.Sismember(ctx, setName, "member1")
	require.NoError(t, err)
	assert.True(t, isMember)

	isMember, err = mockRedis.Sismember(ctx, setName, "nonexistent_member")
	require.NoError(t, err)
	assert.False(t, isMember)

	// 获取所有成员
	members, err := mockRedis.Smembers(ctx, setName)
	require.NoError(t, err)
	assert.Len(t, members, 3)
	assert.Contains(t, members, "member1")
	assert.Contains(t, members, "member2")
	assert.Contains(t, members, "member3")
}

// TestMockRedisListOperations 测试列表操作
func TestMockRedisListOperations(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()
	listName := "test_list"

	// 左侧推入元素
	err := mockRedis.Lpush(ctx, listName, "item1", "item2")
	require.NoError(t, err)

	// 右侧推入元素
	err = mockRedis.Rpush(ctx, listName, "item3", "item4")
	require.NoError(t, err)

	// 获取列表长度
	length, err := mockRedis.Llen(ctx, listName)
	require.NoError(t, err)
	assert.Equal(t, int64(4), length)

	// 获取列表范围
	rangeValues, err := mockRedis.Lrange(ctx, listName, 0, -1)
	require.NoError(t, err)
	assert.Len(t, rangeValues, 4)

	// 验证顺序（根据实际的Lpush/Lpush实现）
	assert.Equal(t, "item1", rangeValues[0])
	assert.Equal(t, "item2", rangeValues[1])
	assert.Equal(t, "item3", rangeValues[2])
	assert.Equal(t, "item4", rangeValues[3])
}

// TestMockRedisCounterOperations 测试计数器操作
func TestMockRedisCounterOperations(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()

	// 递增计数器
	value, err := mockRedis.Incr(ctx, "counter_key")
	require.NoError(t, err)
	assert.Equal(t, int64(1), value)

	value, err = mockRedis.Incr(ctx, "counter_key")
	require.NoError(t, err)
	assert.Equal(t, int64(2), value)

	// 递减计数器
	value, err = mockRedis.Decr(ctx, "counter_key")
	require.NoError(t, err)
	assert.Equal(t, int64(1), value)
}

// TestMockRedisMissingKey 测试访问不存在的键
func TestMockRedisMissingKey(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()

	// 获取不存在的键
	_, err := mockRedis.Get(ctx, "nonexistent_key")
	assert.Error(t, err)

	// 检查不存在的哈希字段
	exists, err := mockRedis.HExists(ctx, "nonexistent_hash", "field")
	require.NoError(t, err)
	assert.False(t, exists)

	// 获取不存在的哈希字段
	_, err = mockRedis.HGet(ctx, "nonexistent_hash", "field")
	assert.Error(t, err)

	// 检查不存在的集合成员
	isMember, err := mockRedis.Sismember(ctx, "nonexistent_set", "member")
	require.NoError(t, err)
	assert.False(t, isMember)

	// 获取不存在的列表范围
	rangeValues, err := mockRedis.Lrange(ctx, "nonexistent_list", 0, -1)
	require.NoError(t, err)
	assert.Empty(t, rangeValues)
}

// TestMockRedisClear 测试清空功能
func TestMockRedisClear(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()

	// 设置一些数据
	err := mockRedis.Set(ctx, "key1", "value1", time.Hour)
	require.NoError(t, err)

	err = mockRedis.HSet(ctx, "hash1", "field1", "hvalue1")
	require.NoError(t, err)

	err = mockRedis.Sadd(ctx, "set1", "member1", "member2")
	require.NoError(t, err)

	// 验证数据存在
	exists, err := mockRedis.Exists(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, exists)

	// 清空所有数据
	mockRedis.Clear()

	// 验证数据被删除
	exists, err = mockRedis.Exists(ctx, "key1")
	require.NoError(t, err)
	assert.False(t, exists)

	// 验证其他数据结构也被清空
	hashAll, err := mockRedis.HGetAll(ctx, "hash1")
	require.NoError(t, err)
	assert.Empty(t, hashAll)

	setMembers, err := mockRedis.Smembers(ctx, "set1")
	require.NoError(t, err)
	assert.Empty(t, setMembers)
}

// TestMockRedisDataTypes 测试不同数据类型
func TestMockRedisDataTypes(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()

	// 测试字符串
	err := mockRedis.Set(ctx, "str_key", "string_value", time.Hour)
	require.NoError(t, err)

	value, err := mockRedis.Get(ctx, "str_key")
	require.NoError(t, err)
	assert.Equal(t, "string_value", value)

	// 测试字节数组
	testBytes := []byte("test bytes content")
	mockRedis.Set(ctx, "bytes_key", testBytes, time.Hour)

	retrievedBytes, err := mockRedis.GetBytes(ctx, "bytes_key")
	require.NoError(t, err)
	assert.Equal(t, testBytes, retrievedBytes)
}

// TestMockRedisSetConnectionError 测试模拟连接错误
func TestMockRedisSetConnectionError(t *testing.T) {
	// 使用模拟错误创建客户端
	testError := assert.AnError
	mockRedis := NewMockRedisClientWithError(testError)
	ctx := context.Background()

	// Ping应该返回错误
	err := mockRedis.Ping(ctx)
	assert.Error(t, err)
	assert.Equal(t, testError, err)
}

// TestMockRedisHdel 测试哈希字段删除
func TestMockRedisHdel(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()
	hashName := "test_hash"

	// 设置多个字段
	err := mockRedis.HSet(ctx, hashName, "field1", "value1")
	require.NoError(t, err)
	err = mockRedis.HSet(ctx, hashName, "field2", "value2")
	require.NoError(t, err)
	err = mockRedis.HSet(ctx, hashName, "field3", "value3")
	require.NoError(t, err)

	// 删除指定字段
	err = mockRedis.HDel(ctx, hashName, "field1", "field3")
	require.NoError(t, err)

	// 验证剩余字段
	allFields, err := mockRedis.HGetAll(ctx, hashName)
	require.NoError(t, err)
	assert.Len(t, allFields, 1)
	assert.Equal(t, "value2", allFields["field2"])

	// 验证被删除的字段不存在
	_, exists := allFields["field1"]
	assert.False(t, exists)

	_, exists = allFields["field3"]
	assert.False(t, exists)
}

// TestMockRedisSrem 测试集合成员删除
func TestMockRedisSrem(t *testing.T) {
	mockRedis := NewMockRedisClient()
	ctx := context.Background()
	setName := "test_set"

	// 添加成员
	err := mockRedis.Sadd(ctx, setName, "member1", "member2", "member3")
	require.NoError(t, err)

	// 删除指定成员
	err = mockRedis.Srem(ctx, setName, "member1", "member3")
	require.NoError(t, err)

	// 验证剩余成员
	members, err := mockRedis.Smembers(ctx, setName)
	require.NoError(t, err)
	assert.Len(t, members, 1)
	assert.Equal(t, "member2", members[0])

	// 验证被删除的成员不存在
	assert.NotContains(t, members, "member1")
	assert.NotContains(t, members, "member3")
}