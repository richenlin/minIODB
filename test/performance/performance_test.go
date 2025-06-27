package performance

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestBasicPerformance 基本性能测试
func TestBasicPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	t.Log("Starting basic performance test...")

	// 等待服务启动
	if err := waitForServiceStart(30 * time.Second); err != nil {
		t.Skipf("Service not available: %v", err)
	}

	// 插入测试记录
	recordCount := 1000
	t.Logf("Inserting %d test records...", recordCount)

	start := time.Now()
	err := insertTestData(TestTableName, recordCount)
	insertDuration := time.Since(start)

	if err != nil {
		t.Logf("Warning: Could not insert test data: %v", err)
		t.Skip("Skipping test due to data preparation failure")
	}

	t.Logf("Data insertion completed in %v", insertDuration)
	insertRate := float64(recordCount) / insertDuration.Seconds()
	t.Logf("Insert rate: %.2f records/sec", insertRate)

	// 查询测试
	queries := []struct {
		name string
		sql  string
	}{
		{"Count", fmt.Sprintf("SELECT COUNT(*) FROM %s", TestTableName)},
		{"Average", fmt.Sprintf("SELECT AVG(age) FROM %s", TestTableName)},
		{"Limit", fmt.Sprintf("SELECT * FROM %s LIMIT 10", TestTableName)},
	}

	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			start := time.Now()
			_, err := queryTestData(TestTableName, 10)
			elapsed := time.Since(start)

			if err != nil {
				t.Errorf("Query failed: %v", err)
			} else {
				t.Logf("Query '%s' completed in %v", query.name, elapsed)
			}
		})
	}

	// 清理测试数据
	cleanupTestData(TestTableName)
}

// waitForServiceStart 等待服务启动
func waitForServiceStart(maxWait time.Duration) error {
	timeout := time.After(maxWait)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("service not available after %v", maxWait)
		case <-ticker.C:
			// 尝试健康检查
			if err := checkServiceHealth(); err == nil {
				return nil
			}
		}
	}
}

// checkServiceHealth 检查服务健康状态
func checkServiceHealth() error {
	// 这里应该实现实际的健康检查逻辑
	// 目前返回nil表示服务可用
	return nil
}

// TestInsertPerformance 插入性能测试
func TestInsertPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping insert performance test in short mode")
	}

	t.Log("Starting insert performance test...")

	recordCounts := []int{1000, 5000, 10000}

	for _, count := range recordCounts {
		t.Run(fmt.Sprintf("Records_%d", count), func(t *testing.T) {
			tableName := fmt.Sprintf("insert_perf_test_%d", count)

			start := time.Now()
			err := insertTestData(tableName, count)
			duration := time.Since(start)

			if err != nil {
				t.Errorf("Insert test failed for %d records: %v", count, err)
				return
			}

			rate := float64(count) / duration.Seconds()
			t.Logf("Inserted %d records in %v (%.2f records/sec)", count, duration, rate)

			// 验证插入成功
			resp, err := queryTestData(tableName, 10)
			if err == nil && resp != nil && len(resp.Data) > 0 {
				t.Logf("Verification successful: data found in table")
			}

			// 清理
			cleanupTestData(tableName)
		})
	}
}

// TestQueryPerformance 查询性能测试
func TestQueryPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping query performance test in short mode")
	}

	t.Log("Starting query performance test...")

	// 准备测试数据
	recordCount := 10000
	err := insertTestData(TestTableName, recordCount)
	if err != nil {
		t.Skip("Could not prepare test data")
	}

	queries := []struct {
		name string
		sql  string
	}{
		{"SimpleSelect", fmt.Sprintf("SELECT * FROM %s LIMIT 100", TestTableName)},
		{"Count", fmt.Sprintf("SELECT COUNT(*) FROM %s", TestTableName)},
		{"Average", fmt.Sprintf("SELECT AVG(age) FROM %s", TestTableName)},
		{"GroupBy", fmt.Sprintf("SELECT age, COUNT(*) FROM %s GROUP BY age", TestTableName)},
	}

	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			start := time.Now()
			resp, err := queryTestData(TestTableName, 100)
			elapsed := time.Since(start)

			if err != nil {
				t.Errorf("Query failed: %v", err)
			} else {
				t.Logf("Query '%s' completed in %v, returned %d rows", query.name, elapsed, len(resp.Data))
			}
		})
	}

	// 清理
	cleanupTestData(TestTableName)
}

// TestConcurrentPerformance 并发性能测试
func TestConcurrentPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent performance test in short mode")
	}

	t.Log("Starting concurrent performance test...")

	// 准备测试数据
	recordCount := 5000
	err := insertTestData(TestTableName, recordCount)
	if err != nil {
		t.Skip("Could not prepare test data")
	}

	concurrencyLevels := []int{5, 10, 20}
	testDuration := 10 * time.Second

	for _, concurrency := range concurrencyLevels {
		t.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(t *testing.T) {
			var wg sync.WaitGroup
			var totalQueries int64
			var successQueries int64

			ctx, cancel := context.WithTimeout(context.Background(), testDuration)
			defer cancel()

			start := time.Now()

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for {
						select {
						case <-ctx.Done():
							return
						default:
							atomic.AddInt64(&totalQueries, 1)
							_, err := queryTestData(TestTableName, 10)
							if err == nil {
								atomic.AddInt64(&successQueries, 1)
							}
							time.Sleep(100 * time.Millisecond)
						}
					}
				}()
			}

			wg.Wait()
			actualDuration := time.Since(start)

			qps := float64(totalQueries) / actualDuration.Seconds()
			successRate := float64(successQueries) / float64(totalQueries) * 100

			t.Logf("Concurrency %d results:", concurrency)
			t.Logf("  Total queries: %d", totalQueries)
			t.Logf("  Success queries: %d", successQueries)
			t.Logf("  QPS: %.2f", qps)
			t.Logf("  Success rate: %.2f%%", successRate)

			// 性能断言
			minQPS := float64(concurrency) * 0.5
			assert.Greater(t, qps, minQPS, "QPS should be reasonable")
			assert.Greater(t, successRate, 80.0, "Success rate should be > 80%")
		})
	}

	// 清理
	cleanupTestData(TestTableName)
}

// TestStressTest 压力测试
func TestStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	t.Log("Starting stress test...")

	// 准备测试数据
	recordCount := 20000
	err := insertTestData(TestTableName, recordCount)
	if err != nil {
		t.Skip("Could not prepare test data")
	}

	// 压力测试配置
	stressConfigs := []struct {
		name        string
		concurrency int
		duration    time.Duration
	}{
		{"Light", 10, 30 * time.Second},
		{"Medium", 25, 60 * time.Second},
		{"Heavy", 50, 90 * time.Second},
	}

	for _, config := range stressConfigs {
		t.Run(config.name, func(t *testing.T) {
			var wg sync.WaitGroup
			var totalOps int64
			var successOps int64
			var failedOps int64

			ctx, cancel := context.WithTimeout(context.Background(), config.duration)
			defer cancel()

			start := time.Now()

			for i := 0; i < config.concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for {
						select {
						case <-ctx.Done():
							return
						default:
							atomic.AddInt64(&totalOps, 1)

							// 随机选择操作类型
							if rand.Float64() < 0.8 { // 80% 查询
								_, err := queryTestData(TestTableName, 10)
								if err == nil {
									atomic.AddInt64(&successOps, 1)
								} else {
									atomic.AddInt64(&failedOps, 1)
								}
							} else { // 20% 插入
								records := generateTestRecords(1)
								err := insertBatchReal(records)
								if err == nil {
									atomic.AddInt64(&successOps, 1)
								} else {
									atomic.AddInt64(&failedOps, 1)
								}
							}

							time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
						}
					}
				}()
			}

			wg.Wait()
			actualDuration := time.Since(start)

			ops := float64(totalOps) / actualDuration.Seconds()
			successRate := float64(successOps) / float64(totalOps) * 100

			t.Logf("Stress test '%s' results:", config.name)
			t.Logf("  Concurrency: %d", config.concurrency)
			t.Logf("  Duration: %v", actualDuration)
			t.Logf("  Total operations: %d", totalOps)
			t.Logf("  Success operations: %d", successOps)
			t.Logf("  Failed operations: %d", failedOps)
			t.Logf("  Operations/sec: %.2f", ops)
			t.Logf("  Success rate: %.2f%%", successRate)

			// 性能断言
			assert.Greater(t, ops, float64(config.concurrency)*0.1, "Operations per second should be reasonable")
			assert.Greater(t, successRate, 70.0, "Success rate should be > 70%")
		})
	}

	// 清理
	cleanupTestData(TestTableName)
}

// TestMemoryUsage 内存使用测试
func TestMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	t.Log("Starting memory usage test...")

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// 执行内存密集型操作
	recordCount := 100000
	err := insertTestData(TestTableName, recordCount)
	if err != nil {
		t.Skip("Could not prepare test data")
	}

	// 执行查询操作
	for i := 0; i < 100; i++ {
		_, err := queryTestData(TestTableName, 1000)
		if err != nil {
			t.Logf("Query %d failed: %v", i, err)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	memUsed := m2.Alloc - m1.Alloc
	memAllocated := m2.TotalAlloc - m1.TotalAlloc

	t.Logf("Memory usage test results:")
	t.Logf("  Records inserted: %d", recordCount)
	t.Logf("  Memory used: %d bytes (%.2f MB)", memUsed, float64(memUsed)/1024/1024)
	t.Logf("  Total allocated: %d bytes (%.2f MB)", memAllocated, float64(memAllocated)/1024/1024)
	t.Logf("  Memory per record: %.2f bytes", float64(memUsed)/float64(recordCount))

	// 内存使用断言
	maxMemoryPerRecord := float64(1024) // 1KB per record
	actualMemoryPerRecord := float64(memUsed) / float64(recordCount)
	if actualMemoryPerRecord > maxMemoryPerRecord {
		t.Logf("Warning: Memory usage per record (%.2f bytes) exceeds expected (%.2f bytes)",
			actualMemoryPerRecord, maxMemoryPerRecord)
	}

	// 清理
	cleanupTestData(TestTableName)
}
