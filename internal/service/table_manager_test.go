package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTableManagerBasicOperations 测试TableManager基本操作
func TestTableManagerBasicOperations(t *testing.T) {
	// 创建一个基本的TableManager实例（不依赖外部服务）
	tableManager := &TableManager{
		metadataCache:      make(map[string]*TableManagerInfo),
		metadataCacheTTL:   time.Hour,
		tableLocks:        make(map[string]*sync.Mutex),
	}

	assert.NotNil(t, tableManager, "TableManager should not be nil")
	assert.NotNil(t, tableManager.metadataCache, "Metadata cache should be initialized")
	assert.NotNil(t, tableManager.tableLocks, "Table locks should be initialized")
}

// TestTableManagerInfoCreation 测试TableManagerInfo创建
func TestTableManagerInfoCreation(t *testing.T) {
	info := &TableManagerInfo{
		Name:      "test_table",
		CreatedAt: "2023-01-01T00:00:00Z",
		LastWrite: "2023-01-01T01:00:00Z",
		Status:    "active",
	}

	assert.Equal(t, "test_table", info.Name, "Table name should match")
	assert.Equal(t, "2023-01-01T00:00:00Z", info.CreatedAt, "Created at should match")
	assert.Equal(t, "2023-01-01T01:00:00Z", info.LastWrite, "Last write should match")
	assert.Equal(t, "active", info.Status, "Status should match")
}

// TestTableManagerStatsCreation 测试TableManagerStats创建
func TestTableManagerStatsCreation(t *testing.T) {
	stats := &TableManagerStats{
		RecordCount: 1000,
		FileCount:   10,
		SizeBytes:   1024000,
	}

	assert.Equal(t, int64(1000), stats.RecordCount, "Record count should match")
	assert.Equal(t, int64(10), stats.FileCount, "File count should match")
	assert.Equal(t, int64(1024000), stats.SizeBytes, "Size bytes should match")
}

// TestTableManagerTableLocks 测试表锁功能
func TestTableManagerTableLocks(t *testing.T) {
	tableManager := &TableManager{
		metadataCache:      make(map[string]*TableManagerInfo),
		metadataCacheTTL:   time.Hour,
		tableLocks:        make(map[string]*sync.Mutex),
	}

	// 获取表锁
	lock1 := tableManager.getTableLock("test_table1")
	assert.NotNil(t, lock1, "Table lock should be created")

	lock2 := tableManager.getTableLock("test_table2")
	assert.NotNil(t, lock2, "Table lock should be created")

	// 相同表名应该返回相同的锁
	lock1Again := tableManager.getTableLock("test_table1")
	assert.Equal(t, lock1, lock1Again, "Same table should return same lock")
}

// TestTableManagerMetadataCache 测试元数据缓存功能
func TestTableManagerMetadataCache(t *testing.T) {
	ctx := context.Background()
	tableManager := &TableManager{
		metadataCache:      make(map[string]*TableManagerInfo),
		metadataCacheTTL:   time.Hour,
		tableLocks:        make(map[string]*sync.Mutex),
	}

	// 设置缓存表信息
	info := &TableManagerInfo{
		Name:      "cached_table",
		CreatedAt: "2023-01-01T00:00:00Z",
		LastWrite: "2023-01-01T01:00:00Z",
		Status:    "active",
	}
	tableManager.setCachedTableInfo(ctx, "cached_table", info)

	// 获取缓存的表信息
	cachedInfo, exists := tableManager.getCachedTableInfo(ctx, "cached_table")
	assert.True(t, exists, "Cached table should exist")
	assert.NotNil(t, cachedInfo, "Cached info should not be nil")
	assert.Equal(t, "cached_table", cachedInfo.Name, "Table name should match")

	// 获取不存在的表信息
	nonExistentInfo, nonExistent := tableManager.getCachedTableInfo(ctx, "non_existent_table")
	assert.False(t, nonExistent, "Non-existent table should not exist")
	assert.Nil(t, nonExistentInfo, "Non-existent info should be nil")
}

// TestTableManagerMetadataCacheInvalidation 测试元数据缓存失效
func TestTableManagerMetadataCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	tableManager := &TableManager{
		metadataCache:      make(map[string]*TableManagerInfo),
		metadataCacheTTL:   time.Hour,
		tableLocks:        make(map[string]*sync.Mutex),
	}

	// 设置缓存表信息
	info := &TableManagerInfo{
		Name:      "table_to_invalidate",
		CreatedAt: "2023-01-01T00:00:00Z",
		LastWrite: "2023-01-01T01:00:00Z",
		Status:    "active",
	}
	tableManager.setCachedTableInfo(ctx, "table_to_invalidate", info)

	// 失效缓存
	tableManager.InvalidateCachedTableInfo(ctx, "table_to_invalidate")

	// 验证缓存已失效
	cachedInfo, exists := tableManager.getCachedTableInfo(ctx, "table_to_invalidate")
	assert.False(t, exists, "Invalidated table should not exist")
	assert.Nil(t, cachedInfo, "Invalidated info should be nil")
}

// BenchmarkTableManagerGetTableLock 基准测试表锁获取性能
func BenchmarkTableManagerGetTableLock(b *testing.B) {
	tableManager := &TableManager{
		metadataCache:      make(map[string]*TableManagerInfo),
		metadataCacheTTL:   time.Hour,
		tableLocks:        make(map[string]*sync.Mutex),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tableManager.getTableLock("benchmark_table")
	}
}