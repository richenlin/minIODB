//go:build experimental

package storage

import (
	"sync"
	"testing"

	"minIODB/pkg/logger"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestBloomFilterConcurrentTest(t *testing.T) {
	// 创建索引系统
	testLogger := zap.NewNop()
	if logger.GetLogger() != nil {
		testLogger = logger.GetLogger()
	}

	is := NewIndexSystem(nil, testLogger)

	// 创建 BloomFilter 索引
	err := is.CreateBloomFilter("test_bloom", "test_column")
	assert.NoError(t, err)

	// 添加一些元素
	for i := 0; i < 100; i++ {
		err := is.AddToBloomFilter("test_bloom", string(rune(i)))
		assert.NoError(t, err)
	}

	// 并发测试 TestBloomFilter，验证无数据竞争
	var wg sync.WaitGroup
	concurrency := 100
	iterations := 100

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, err := is.TestBloomFilter("test_bloom", string(rune(j)))
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// 验证统计计数正确（TotalQueries 应该等于 concurrency * iterations）
	index, exists := is.bloomFilters["test_bloom"]
	assert.True(t, exists)
	totalQueries := index.stats.TotalQueries.Load()
	expectedQueries := int64(concurrency * iterations)
	assert.Equal(t, expectedQueries, totalQueries, "TotalQueries should match concurrent calls")
}

func TestBloomFilterStatsAtomicOperations(t *testing.T) {
	// 测试 atomic 操作的正确性
	testLogger := zap.NewNop()
	is := NewIndexSystem(nil, testLogger)

	err := is.CreateBloomFilter("atomic_test", "test_column")
	assert.NoError(t, err)

	// 添加元素
	for i := 0; i < 50; i++ {
		err := is.AddToBloomFilter("atomic_test", string(rune(i)))
		assert.NoError(t, err)
	}

	// 执行测试并检查统计
	result, err := is.TestBloomFilter("atomic_test", string(rune(25)))
	assert.NoError(t, err)
	assert.True(t, result) // 应该返回 true，因为元素存在

	// 检查统计
	index := is.bloomFilters["atomic_test"]
	assert.Equal(t, int64(1), index.stats.TotalQueries.Load())
	assert.Equal(t, int64(1), index.stats.TruePositives.Load())
	assert.Equal(t, int64(0), index.stats.TrueNegatives.Load())

	// 测试不存在的元素
	result, err = is.TestBloomFilter("atomic_test", "non_existent_value")
	assert.NoError(t, err)
	assert.False(t, result)

	assert.Equal(t, int64(2), index.stats.TotalQueries.Load())
	assert.Equal(t, int64(1), index.stats.TrueNegatives.Load())
}
