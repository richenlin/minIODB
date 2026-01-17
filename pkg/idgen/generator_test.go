package idgen

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUUIDGenerator(t *testing.T) {
	gen := NewUUIDGenerator()

	// 测试生成UUID
	id1, err := gen.Generate()
	require.NoError(t, err)
	assert.NotEmpty(t, id1)
	assert.Equal(t, 36, len(id1)) // UUID v4格式长度

	// 测试生成的UUID不重复
	id2, err := gen.Generate()
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2)

	// 验证UUID格式 (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)
	parts := strings.Split(id1, "-")
	assert.Equal(t, 5, len(parts))
}

func TestSnowflakeGenerator(t *testing.T) {
	nodeID := int64(1)
	gen, err := NewSnowflakeGenerator(nodeID)
	require.NoError(t, err)

	// 测试生成ID
	id1, err := gen.Generate()
	require.NoError(t, err)
	assert.Greater(t, id1, int64(0))

	// 测试ID递增
	id2, err := gen.Generate()
	require.NoError(t, err)
	assert.Greater(t, id2, id1)

	// 解析ID验证
	timestamp, parsedNodeID, sequence := ParseSnowflakeID(id1)
	assert.Greater(t, timestamp, epoch)
	assert.Equal(t, nodeID, parsedNodeID)
	assert.GreaterOrEqual(t, sequence, int64(0))
	assert.LessOrEqual(t, sequence, maxSequence)
}

func TestSnowflakeGenerator_InvalidNodeID(t *testing.T) {
	// 测试无效节点ID
	_, err := NewSnowflakeGenerator(-1)
	assert.Error(t, err)

	_, err = NewSnowflakeGenerator(1025)
	assert.Error(t, err)
}

func TestSnowflakeGenerator_Concurrent(t *testing.T) {
	gen, err := NewSnowflakeGenerator(1)
	require.NoError(t, err)

	// 并发生成ID
	count := 10000
	ids := make([]int64, count)
	var wg sync.WaitGroup
	wg.Add(count)

	for i := 0; i < count; i++ {
		go func(idx int) {
			defer wg.Done()
			id, err := gen.Generate()
			require.NoError(t, err)
			ids[idx] = id
		}(i)
	}

	wg.Wait()

	// 验证所有ID唯一
	idMap := make(map[int64]bool)
	for _, id := range ids {
		assert.False(t, idMap[id], "Duplicate ID found: %d", id)
		idMap[id] = true
	}
}

func TestCustomGenerator(t *testing.T) {
	gen := NewCustomGenerator()

	// 测试无前缀
	id1, err := gen.Generate("")
	require.NoError(t, err)
	assert.NotEmpty(t, id1)
	assert.Contains(t, id1, "-") // 应该包含分隔符

	// 测试带前缀
	id2, err := gen.Generate("user")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(id2, "user-"))

	// 测试生成的ID不重复
	time.Sleep(2 * time.Millisecond) // 确保时间戳不同
	id3, err := gen.Generate("user")
	require.NoError(t, err)
	assert.NotEqual(t, id2, id3)
}

func TestDefaultGenerator(t *testing.T) {
	config := &GeneratorConfig{
		NodeID:          1,
		DefaultStrategy: string(StrategyUUID),
	}
	gen, err := NewGenerator(config)
	require.NoError(t, err)

	// 测试UUID策略
	id1, err := gen.Generate("test", StrategyUUID, "")
	require.NoError(t, err)
	assert.NotEmpty(t, id1)

	// 测试Snowflake策略
	id2, err := gen.Generate("test", StrategySnowflake, "")
	require.NoError(t, err)
	assert.NotEmpty(t, id2)

	// 测试Snowflake策略带前缀
	id3, err := gen.Generate("test", StrategySnowflake, "order")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(id3, "order-"))

	// 测试Custom策略
	id4, err := gen.Generate("test", StrategyCustom, "log")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(id4, "log-"))

	// 测试默认策略
	id5, err := gen.Generate("test", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, id5)

	// 测试用户提供策略（应该报错）
	_, err = gen.Generate("test", StrategyUserProvided, "")
	assert.Error(t, err)
}

func TestIDValidator(t *testing.T) {
	validator := NewIDValidator()

	// 测试空ID
	err := validator.Validate("", 255, "")
	assert.Error(t, err)

	// 测试长度验证
	err = validator.Validate("test-id", 5, "")
	assert.Error(t, err)

	err = validator.Validate("test-id", 10, "")
	assert.NoError(t, err)

	// 测试正则验证
	pattern := "^[a-zA-Z0-9_-]+$"
	err = validator.Validate("valid-id_123", 255, pattern)
	assert.NoError(t, err)

	err = validator.Validate("invalid id with spaces", 255, pattern)
	assert.Error(t, err)

	// 测试无效正则
	err = validator.Validate("test", 255, "[invalid(")
	assert.Error(t, err)
}

func TestDefaultGenerator_Validate(t *testing.T) {
	config := &GeneratorConfig{
		NodeID:          1,
		DefaultStrategy: string(StrategyUUID),
	}
	gen, err := NewGenerator(config)
	require.NoError(t, err)

	// 测试有效ID
	err = gen.Validate("user-123", 255, "^[a-zA-Z0-9_-]+$")
	assert.NoError(t, err)

	// 测试无效ID
	err = gen.Validate("invalid id", 255, "^[a-zA-Z0-9_-]+$")
	assert.Error(t, err)
}

func BenchmarkUUIDGenerator(b *testing.B) {
	gen := NewUUIDGenerator()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}

func BenchmarkSnowflakeGenerator(b *testing.B) {
	gen, _ := NewSnowflakeGenerator(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate()
	}
}

func BenchmarkCustomGenerator(b *testing.B) {
	gen := NewCustomGenerator()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gen.Generate("test")
	}
}
