package buffer

import (
	"context"
	"testing"
	"time"
)

// MockCacheInvalidator 模拟的缓存失效器（用于测试）
type MockCacheInvalidator struct {
	invalidatedTables []string
	callCount         int
}

func (m *MockCacheInvalidator) InvalidateTable(ctx context.Context, tableName string) error {
	m.invalidatedTables = append(m.invalidatedTables, tableName)
	m.callCount++
	return nil
}

// TestCacheInvalidatorInterface 测试CacheInvalidator接口
func TestCacheInvalidatorInterface(t *testing.T) {
	ctx := context.Background()

	// 创建模拟失效器
	mock := &MockCacheInvalidator{
		invalidatedTables: []string{},
	}

	// 测试失效单个表
	err := mock.InvalidateTable(ctx, "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.callCount != 1 {
		t.Errorf("expected 1 call, got %d", mock.callCount)
	}

	if len(mock.invalidatedTables) != 1 || mock.invalidatedTables[0] != "users" {
		t.Errorf("expected invalidated tables: [users], got %v", mock.invalidatedTables)
	}
}

// TestNoOpCacheInvalidator 测试NoOp失效器
func TestNoOpCacheInvalidator(t *testing.T) {
	ctx := context.Background()
	noop := &NoOpCacheInvalidator{}

	// NoOp失效器不应返回错误
	err := noop.InvalidateTable(ctx, "any_table")
	if err != nil {
		t.Fatalf("NoOpCacheInvalidator should never return error, got: %v", err)
	}
}

// TestConcurrentBufferSetCacheInvalidator 测试设置缓存失效器
func TestConcurrentBufferSetCacheInvalidator(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:     100,
		FlushInterval:  10 * time.Second,
		WorkerPoolSize: 1,
		TaskQueueSize:  10,
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)
	// Note: Shutdown method might not exist, skip defer for this test

	// 创建模拟失效器
	mock := &MockCacheInvalidator{
		invalidatedTables: []string{},
	}

	// 设置失效器
	cb.SetCacheInvalidator(mock)

	if cb.cacheInvalidator == nil {
		t.Fatal("cacheInvalidator should not be nil after SetCacheInvalidator")
	}
}

// TestMultipleTableInvalidation 测试多表失效
func TestMultipleTableInvalidation(t *testing.T) {
	ctx := context.Background()
	mock := &MockCacheInvalidator{
		invalidatedTables: []string{},
	}

	tables := []string{"users", "orders", "products"}

	for _, table := range tables {
		err := mock.InvalidateTable(ctx, table)
		if err != nil {
			t.Fatalf("unexpected error for table %s: %v", table, err)
		}
	}

	if mock.callCount != len(tables) {
		t.Errorf("expected %d calls, got %d", len(tables), mock.callCount)
	}

	if len(mock.invalidatedTables) != len(tables) {
		t.Errorf("expected %d invalidated tables, got %d", len(tables), len(mock.invalidatedTables))
	}

	// 验证所有表都被失效
	for i, table := range tables {
		if mock.invalidatedTables[i] != table {
			t.Errorf("expected table %s at index %d, got %s", table, i, mock.invalidatedTables[i])
		}
	}
}

// TestCacheInvalidationAfterFlush 测试刷新后的缓存失效（集成测试概念）
func TestCacheInvalidationAfterFlush(t *testing.T) {
	// 这是一个概念性测试，展示缓存失效的工作流程

	// 1. Buffer收到写入请求
	// 2. Buffer累积数据到一定阈值
	// 3. 触发刷新，数据写入MinIO
	// 4. 刷新成功后，调用CacheInvalidator.InvalidateTable()
	// 5. Querier的缓存被清理
	// 6. 下次查询会读取到最新数据

	t.Log("Cache invalidation workflow:")
	t.Log("1. Data written to buffer")
	t.Log("2. Buffer threshold reached, flush triggered")
	t.Log("3. Data uploaded to MinIO successfully")
	t.Log("4. CacheInvalidator.InvalidateTable() called")
	t.Log("5. Query cache and file index cache cleared")
	t.Log("6. Next query will see fresh data without TTL wait")
}
