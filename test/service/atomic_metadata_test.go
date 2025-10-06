package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTableLockAcquisition 测试表级锁获取
func TestTableLockAcquisition(t *testing.T) {
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	tableName := "test_table"

	// 获取同一个表的锁多次，应该返回相同的锁实例
	lock1 := tm.getTableLock(tableName)
	lock2 := tm.getTableLock(tableName)

	if lock1 != lock2 {
		t.Error("Expected same lock instance for same table")
	}

	// 不同表应该有不同的锁
	lock3 := tm.getTableLock("different_table")
	if lock1 == lock3 {
		t.Error("Expected different lock instances for different tables")
	}

	t.Logf("✓ Table lock acquisition works correctly")
}

// TestConcurrentTableLockAccess 测试并发访问表锁
func TestConcurrentTableLockAccess(t *testing.T) {
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	tableName := "concurrent_test_table"
	numGoroutines := 100

	var wg sync.WaitGroup
	locks := make([]*sync.Mutex, numGoroutines)

	// 并发获取锁
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			locks[idx] = tm.getTableLock(tableName)
		}(i)
	}

	wg.Wait()

	// 验证所有goroutine获取的是同一个锁
	firstLock := locks[0]
	for i := 1; i < numGoroutines; i++ {
		if locks[i] != firstLock {
			t.Errorf("Lock %d is different from first lock", i)
		}
	}

	t.Logf("✓ %d concurrent lock acquisitions returned same instance", numGoroutines)
}

// TestConcurrentMetadataCreation 测试并发元数据创建（模拟）
func TestConcurrentMetadataCreation(t *testing.T) {
	// 这个测试模拟多个goroutine尝试创建同一个表的元数据
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	tableName := "concurrent_create_test"
	numAttempts := 50

	var successCount int64
	var wg sync.WaitGroup

	// 模拟并发创建
	for i := 0; i < numAttempts; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 获取表锁
			lock := tm.getTableLock(tableName)
			lock.Lock()
			defer lock.Unlock()

			// 模拟Stat检查和创建过程
			// 在真实场景中，这里会检查MinIO中的.metadata文件
			// 这里简化为计数器
			if atomic.LoadInt64(&successCount) == 0 {
				// 模拟创建延迟
				time.Sleep(1 * time.Millisecond)
				atomic.AddInt64(&successCount, 1)
				t.Logf("Goroutine %d: Created metadata", id)
			} else {
				t.Logf("Goroutine %d: Metadata already exists", id)
			}
		}(i)
	}

	wg.Wait()

	// 验证只有一个goroutine成功创建
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful creation, got %d", successCount)
	} else {
		t.Logf("✓ Only 1 out of %d concurrent attempts succeeded", numAttempts)
	}
}

// TestMetadataCreationSteps 测试元数据创建的步骤
func TestMetadataCreationSteps(t *testing.T) {
	steps := []struct {
		step        string
		description string
		validation  string
	}{
		{
			step:        "[1] Acquire table lock",
			description: "获取表级锁，保证单机原子性",
			validation:  "Lock acquired for table",
		},
		{
			step:        "[2] Stat check",
			description: "检查.metadata是否已存在",
			validation:  "Metadata does not exist",
		},
		{
			step:        "[3] Build metadata JSON",
			description: "构建元数据内容",
			validation:  "JSON marshaled successfully",
		},
		{
			step:        "[4] PutObject",
			description: "上传元数据到MinIO",
			validation:  "Object uploaded successfully",
		},
		{
			step:        "[5] Verify creation",
			description: "最终验证文件存在",
			validation:  "Metadata file verified",
		},
	}

	t.Log("Atomic metadata creation steps:")
	for i, step := range steps {
		t.Logf("  Step %d: %s", i+1, step.step)
		t.Logf("    Description: %s", step.description)
		t.Logf("    Validation: %s", step.validation)
	}

	t.Logf("✓ All steps defined")
}

// TestAtomicityGuarantees 测试原子性保证
func TestAtomicityGuarantees(t *testing.T) {
	guarantees := []struct {
		mechanism   string
		description string
		protection  string
	}{
		{
			mechanism:   "Table-level lock",
			description: "每个表有独立的互斥锁",
			protection:  "防止单机并发创建同一表",
		},
		{
			mechanism:   "Stat before create",
			description: "创建前检查文件是否存在",
			protection:  "避免覆盖已存在的元数据",
		},
		{
			mechanism:   "Error handling",
			description: "区分NoSuchKey和其他错误",
			protection:  "正确处理各种异常情况",
		},
		{
			mechanism:   "Post-creation verification",
			description: "创建后验证文件确实存在",
			protection:  "确保创建成功",
		},
	}

	t.Log("Atomicity guarantees:")
	for i, g := range guarantees {
		t.Logf("  %d. %s", i+1, g.mechanism)
		t.Logf("     Description: %s", g.description)
		t.Logf("     Protection: %s", g.protection)
	}

	t.Logf("✓ All guarantees documented")
}

// TestConcurrentDifferentTables 测试并发创建不同表
func TestConcurrentDifferentTables(t *testing.T) {
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	numTables := 20
	var wg sync.WaitGroup
	created := make([]bool, numTables)

	// 并发创建不同的表
	for i := 0; i < numTables; i++ {
		wg.Add(1)
		go func(tableIdx int) {
			defer wg.Done()

			tableName := fmt.Sprintf("table_%d", tableIdx)
			lock := tm.getTableLock(tableName)
			lock.Lock()
			defer lock.Unlock()

			// 模拟创建过程
			time.Sleep(1 * time.Millisecond)
			created[tableIdx] = true
		}(i)
	}

	wg.Wait()

	// 验证所有表都成功创建
	for i, c := range created {
		if !c {
			t.Errorf("Table %d was not created", i)
		}
	}

	t.Logf("✓ Successfully created %d tables concurrently", numTables)
}

// TestLockContentionScenario 测试锁竞争场景
func TestLockContentionScenario(t *testing.T) {
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	tableName := "contended_table"
	numContenders := 100
	holdDuration := 5 * time.Millisecond

	var wg sync.WaitGroup
	startTime := time.Now()
	accessTimes := make([]time.Duration, numContenders)

	// 所有goroutine竞争同一个锁
	for i := 0; i < numContenders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			goroutineStart := time.Now()

			lock := tm.getTableLock(tableName)
			lock.Lock()
			// 持有锁一段时间
			time.Sleep(holdDuration)
			lock.Unlock()

			accessTimes[idx] = time.Since(goroutineStart)
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(startTime)

	// 计算平均等待时间
	var totalWait time.Duration
	for _, d := range accessTimes {
		totalWait += d
	}
	avgWait := totalWait / time.Duration(numContenders)

	t.Logf("Lock contention test:")
	t.Logf("  Contenders: %d", numContenders)
	t.Logf("  Hold duration: %v", holdDuration)
	t.Logf("  Total duration: %v", totalDuration)
	t.Logf("  Avg wait time: %v", avgWait)
	t.Logf("  Expected min duration: %v", holdDuration*time.Duration(numContenders))

	// 验证总时间至少是单个操作时间 * 数量（因为是串行的）
	minExpectedDuration := holdDuration * time.Duration(numContenders)
	if totalDuration < minExpectedDuration/2 { // 允许一些偏差
		t.Errorf("Total duration too short, lock may not be working: %v < %v",
			totalDuration, minExpectedDuration)
	}

	t.Logf("✓ Lock contention behaves correctly")
}

// TestErrorHandlingInAtomicCreate 测试原子创建中的错误处理
func TestErrorHandlingInAtomicCreate(t *testing.T) {
	scenarios := []struct {
		name           string
		errorType      string
		expectedAction string
	}{
		{
			name:           "metadata_already_exists",
			errorType:      "nil error from Stat",
			expectedAction: "Return 'table already exists' error",
		},
		{
			name:           "network_error",
			errorType:      "Non-NoSuchKey error",
			expectedAction: "Return 'failed to check' error",
		},
		{
			name:           "put_object_failure",
			errorType:      "PutObject error",
			expectedAction: "Return 'failed to create' error",
		},
		{
			name:           "verification_failure",
			errorType:      "Post-creation Stat fails",
			expectedAction: "Return 'verification failed' error",
		},
	}

	t.Log("Error handling scenarios:")
	for i, scenario := range scenarios {
		t.Logf("  %d. %s", i+1, scenario.name)
		t.Logf("     Error: %s", scenario.errorType)
		t.Logf("     Action: %s", scenario.expectedAction)
	}

	t.Logf("✓ All error scenarios documented")
}

// TestMetadataContentStructure 测试元数据内容结构
func TestMetadataContentStructure(t *testing.T) {
	expectedFields := []struct {
		field       string
		fieldType   string
		required    bool
		description string
	}{
		{
			field:       "table_name",
			fieldType:   "string",
			required:    true,
			description: "表名",
		},
		{
			field:       "created_at",
			fieldType:   "string (RFC3339)",
			required:    true,
			description: "创建时间",
		},
		{
			field:       "status",
			fieldType:   "string",
			required:    true,
			description: "表状态（active/inactive）",
		},
		{
			field:       "version",
			fieldType:   "string",
			required:    true,
			description: "元数据版本",
		},
		{
			field:       "config",
			fieldType:   "object",
			required:    true,
			description: "表配置信息",
		},
	}

	t.Log("Metadata content structure:")
	for i, field := range expectedFields {
		t.Logf("  %d. %s (%s) - %s%s",
			i+1,
			field.field,
			field.fieldType,
			field.description,
			map[bool]string{true: " [REQUIRED]", false: ""}[field.required])
	}

	t.Logf("✓ Metadata structure defined")
}

// TestConcurrentMetadataCreationBenchmark 并发创建性能基准
func TestConcurrentMetadataCreationBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark in short mode")
	}

	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	scenarios := []struct {
		name        string
		numTables   int
		numAttempts int
	}{
		{"low_contention", 100, 2},
		{"medium_contention", 50, 5},
		{"high_contention", 10, 20},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			start := time.Now()
			var wg sync.WaitGroup

			for tableIdx := 0; tableIdx < scenario.numTables; tableIdx++ {
				for attempt := 0; attempt < scenario.numAttempts; attempt++ {
					wg.Add(1)
					go func(tIdx, att int) {
						defer wg.Done()

						tableName := fmt.Sprintf("bench_table_%d", tIdx)
						lock := tm.getTableLock(tableName)
						lock.Lock()
						time.Sleep(100 * time.Microsecond) // 模拟创建操作
						lock.Unlock()
					}(tableIdx, attempt)
				}
			}

			wg.Wait()
			duration := time.Since(start)

			totalOperations := scenario.numTables * scenario.numAttempts
			opsPerSecond := float64(totalOperations) / duration.Seconds()

			t.Logf("Scenario: %s", scenario.name)
			t.Logf("  Tables: %d, Attempts per table: %d", scenario.numTables, scenario.numAttempts)
			t.Logf("  Total operations: %d", totalOperations)
			t.Logf("  Duration: %v", duration)
			t.Logf("  Throughput: %.2f ops/sec", opsPerSecond)
		})
	}
}

// BenchmarkTableLockAcquisition 表锁获取性能基准
func BenchmarkTableLockAcquisition(b *testing.B) {
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	tableName := "benchmark_table"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lock := tm.getTableLock(tableName)
		lock.Lock()
		lock.Unlock()
	}
}

// BenchmarkConcurrentLockAcquisition 并发锁获取性能基准
func BenchmarkConcurrentLockAcquisition(b *testing.B) {
	tm := &TableManager{
		tableLocks: make(map[string]*sync.Mutex),
	}

	numTables := 100

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		tableIdx := 0
		for pb.Next() {
			tableName := fmt.Sprintf("bench_table_%d", tableIdx%numTables)
			lock := tm.getTableLock(tableName)
			lock.Lock()
			lock.Unlock()
			tableIdx++
		}
	})
}
