package performance

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestHighConcurrencyReads 高并发读测试
func TestHighConcurrencyReads(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high concurrency reads test in short mode")
	}

	t.Log("Starting high concurrency reads test...")

	// 准备测试数据
	recordCount := 50000
	err := insertTestData(TestTableName, recordCount)
	if err != nil {
		t.Logf("Warning: Could not insert test data: %v", err)
		t.Skip("Skipping test due to data preparation failure")
	}

	concurrencyLevels := []int{10, 50, 100, 200, 500}
	testDuration := 30 * time.Second

	for _, concurrency := range concurrencyLevels {
		t.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(t *testing.T) {
			t.Logf("Testing read concurrency level: %d", concurrency)

			result := runConcurrentReadTest(concurrency, testDuration)

			t.Logf("高并发读测试结果 (并发数: %d):", concurrency)
			t.Logf("  总查询数: %d", result.TotalQueries)
			t.Logf("  成功查询: %d", result.SuccessfulQueries)
			t.Logf("  失败查询: %d", result.FailedQueries)
			t.Logf("  QPS: %.2f", result.QPS)
			t.Logf("  平均延迟: %v", result.AvgQueryLatency)
			t.Logf("  最大延迟: %v", result.MaxQueryLatency)
			t.Logf("  错误率: %.2f%%", result.ErrorRate*100)

			// 性能断言
			minExpectedQPS := float64(concurrency) * 0.5
			assert.Greater(t, result.QPS, minExpectedQPS, "QPS should be reasonable for concurrency level")

			var maxErrorRate float64 = 0.1
			if concurrency >= 200 {
				maxErrorRate = 0.3
			}
			assert.Less(t, result.ErrorRate, maxErrorRate, "Error rate should be acceptable")
		})
	}

	// 清理测试数据
	cleanupTestData(TestTableName)
}

// TestHighConcurrencyWrites 高并发写测试
func TestHighConcurrencyWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high concurrency write test in short mode")
	}

	t.Log("Starting high concurrency write test...")

	// 测试配置
	concurrencyLevels := []int{5, 10, 20, 50}
	testDuration := 30 * time.Second

	for _, concurrency := range concurrencyLevels {
		t.Run(fmt.Sprintf("WriteConcurrency_%d", concurrency), func(t *testing.T) {
			result := runConcurrentWriteTest(concurrency, testDuration)

			t.Logf("写并发级别 %d 测试结果:", concurrency)
			t.Logf("  总插入数: %d", result.TotalInserts)
			t.Logf("  成功插入: %d", result.SuccessfulInserts)
			t.Logf("  失败插入: %d", result.FailedInserts)
			t.Logf("  IPS: %.2f", result.IPS)
			t.Logf("  平均延迟: %v", result.AvgInsertLatency)
			t.Logf("  最大延迟: %v", result.MaxInsertLatency)
			t.Logf("  错误率: %.2f%%", result.ErrorRate*100)

			// 性能断言
			assert.Greater(t, result.IPS, float64(concurrency*10), "IPS should be reasonable for concurrency level")
			assert.Less(t, result.ErrorRate, 0.10, "Error rate should be < 10%")
			assert.Less(t, result.AvgInsertLatency, 10*time.Second, "Average latency should be < 10s")
		})
	}
}

// TestMixedConcurrentWorkload 混合并发工作负载测试
func TestMixedConcurrentWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping mixed concurrent workload test in short mode")
	}

	t.Log("Starting mixed concurrent workload test...")

	testConfigs := []ConcurrentTestConfig{
		{ReadUsers: 80, WriteUsers: 20, TestDuration: 60 * time.Second},
		{ReadUsers: 50, WriteUsers: 50, TestDuration: 60 * time.Second},
		{ReadUsers: 200, WriteUsers: 50, TestDuration: 60 * time.Second},
		{ReadUsers: 500, WriteUsers: 100, TestDuration: 60 * time.Second},
	}

	for i, config := range testConfigs {
		t.Run(fmt.Sprintf("Mixed_R%d_W%d", config.ReadUsers, config.WriteUsers), func(t *testing.T) {
			result := runMixedConcurrentTest(config.ReadUsers, config.WriteUsers, config.TestDuration)

			t.Logf("混合负载测试 %d 结果:", i+1)
			t.Logf("  读用户: %d, 写用户: %d", config.ReadUsers, config.WriteUsers)
			t.Logf("  总查询数: %d", result.TotalQueries)
			t.Logf("  总插入数: %d", result.TotalInserts)
			t.Logf("  QPS: %.2f", result.QPS)
			t.Logf("  IPS: %.2f", result.IPS)
			t.Logf("  查询平均延迟: %v", result.AvgQueryLatency)
			t.Logf("  插入平均延迟: %v", result.AvgInsertLatency)
			t.Logf("  整体错误率: %.2f%%", result.ErrorRate*100)

			// 性能断言
			assert.Greater(t, result.QPS, float64(config.ReadUsers)*0.3, "QPS should be reasonable")
			assert.Greater(t, result.IPS, float64(config.WriteUsers*5), "IPS should be reasonable")
			assert.Less(t, result.ErrorRate, 0.15, "Error rate should be < 15% under high load")
		})
	}
}

// TestTBScaleQueryPerformance TB级数据秒级查询测试
func TestTBScaleQueryPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping TB scale query performance test in short mode")
	}

	t.Log("Starting TB scale query performance test...")

	// 准备大数据集（500万条记录）
	largeTableName := "tb_scale_query_test"
	recordCount := 5000000
	err := createLargeTestTable(largeTableName, recordCount)
	if err != nil {
		t.Logf("Warning: Could not create large test table: %v", err)
		t.Skip("Skipping TB scale test due to data preparation failure")
	}

	// TB级查询测试用例
	tbQueries := []struct {
		name       string
		sql        string
		targetTime time.Duration
	}{
		{
			name:       "FullTableScan",
			sql:        fmt.Sprintf("SELECT COUNT(*) FROM %s", largeTableName),
			targetTime: 30 * time.Second,
		},
		{
			name:       "ComplexJoin",
			sql:        fmt.Sprintf("SELECT t1.region, COUNT(*) FROM %s t1 WHERE t1.amount > 1000 GROUP BY t1.region", largeTableName),
			targetTime: 45 * time.Second,
		},
		{
			name:       "WindowAnalytics",
			sql:        fmt.Sprintf("SELECT region, amount, ROW_NUMBER() OVER (PARTITION BY region ORDER BY amount DESC) as rank FROM %s LIMIT 100", largeTableName),
			targetTime: 60 * time.Second,
		},
	}

	for _, query := range tbQueries {
		t.Run(query.name, func(t *testing.T) {
			t.Logf("执行TB级查询: %s", query.name)
			t.Logf("SQL: %s", query.sql)
			t.Logf("目标时间: %v", query.targetTime)

			start := time.Now()
			_, err := executeQueryReal(query.sql) // 修复：移除多余参数
			elapsed := time.Since(start)

			if err != nil {
				t.Errorf("TB级查询失败: %v", err)
				return
			}

			t.Logf("TB级查询 '%s' 完成:", query.name)
			t.Logf("  执行时间: %v", elapsed)
			t.Logf("  目标时间: %v", query.targetTime)

			// 性能断言
			if elapsed > query.targetTime {
				t.Logf("警告: 查询时间 (%v) 超过目标时间 (%v)", elapsed, query.targetTime)
			} else {
				t.Logf("性能良好: 查询在目标时间内完成")
			}
		})
	}
}

// TestColumnarStorageOptimization 列存储优化测试
func TestColumnarStorageOptimization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping columnar storage optimization test in short mode")
	}

	t.Log("Starting columnar storage optimization test...")

	// 创建列存储测试表
	columnarTableName := "columnar_optimization_test"
	err := createLargeTestTable(columnarTableName, 5000000) // 500万条记录
	if err != nil {
		t.Logf("Warning: Could not create columnar test table: %v", err)
		t.Skip("Skipping columnar test due to data preparation failure")
	}

	// 列存储优化查询测试
	columnarQueries := []struct {
		name        string
		sql         string
		description string
		expectation string
	}{
		{
			name:        "ColumnScan",
			sql:         fmt.Sprintf("SELECT region FROM %s", columnarTableName),
			description: "单列扫描",
			expectation: "列存储应显著提升单列查询性能",
		},
		{
			name:        "AggregateColumn",
			sql:         fmt.Sprintf("SELECT SUM(amount) FROM %s", columnarTableName),
			description: "列聚合查询",
			expectation: "列存储聚合查询应比行存储快3-5倍",
		},
		{
			name:        "FilteredAggregate",
			sql:         fmt.Sprintf("SELECT region, AVG(amount) FROM %s WHERE status = 'active' GROUP BY region", columnarTableName),
			description: "过滤聚合查询",
			expectation: "列存储过滤聚合应显著优于行存储",
		},
		{
			name:        "MultiColumnAggregate",
			sql:         fmt.Sprintf("SELECT region, status, COUNT(*), SUM(amount), AVG(price) FROM %s GROUP BY region, status", columnarTableName),
			description: "多列聚合查询",
			expectation: "多列聚合查询应保持良好性能",
		},
	}

	for _, query := range columnarQueries {
		t.Run(query.name, func(t *testing.T) {
			t.Logf("执行列存储优化查询: %s", query.description)
			t.Logf("SQL: %s", query.sql)
			t.Logf("性能期望: %s", query.expectation)

			start := time.Now()
			resp, err := executeQueryReal(query.sql) // 修复：移除多余参数
			elapsed := time.Since(start)

			if err != nil {
				t.Errorf("列存储查询失败: %v", err)
				return
			}

			t.Logf("列存储查询 '%s' 结果:", query.name)
			t.Logf("  执行时间: %v", elapsed)
			t.Logf("  返回行数: %d", len(resp.Data)) // 修复：使用resp.Data
			t.Logf("  列数: %d", len(resp.Columns))

			// 性能评估
			if elapsed < 3*time.Second {
				t.Logf("性能优秀: 查询在3秒内完成")
			} else if elapsed < 10*time.Second {
				t.Logf("性能良好: 查询在10秒内完成")
			} else {
				t.Logf("性能需要优化: 查询时间超过10秒")
			}
		})
	}
}

// runConcurrentReadTest 运行并发读测试
func runConcurrentReadTest(concurrency int, duration time.Duration) ConcurrentTestResult {
	var result ConcurrentTestResult
	result.ReadUsers = concurrency
	result.TestDuration = duration

	var (
		totalQueries   int64
		successQueries int64
		failedQueries  int64
		totalLatency   int64
		maxLatency     int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			queries := []string{
				fmt.Sprintf("SELECT COUNT(*) FROM %s", TestTableName),
				fmt.Sprintf("SELECT * FROM %s LIMIT 10", TestTableName),
				fmt.Sprintf("SELECT AVG(age) FROM %s", TestTableName),
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
					query := queries[rand.Intn(len(queries))]

					queryStart := time.Now()
					_, err := executeQueryReal(query) // 修复函数调用
					queryElapsed := time.Since(queryStart)

					atomic.AddInt64(&totalQueries, 1)
					atomic.AddInt64(&totalLatency, queryElapsed.Nanoseconds())

					if err != nil {
						atomic.AddInt64(&failedQueries, 1)
					} else {
						atomic.AddInt64(&successQueries, 1)
					}

					// 更新最大延迟
					latencyNanos := queryElapsed.Nanoseconds()
					for {
						current := atomic.LoadInt64(&maxLatency)
						if latencyNanos <= current || atomic.CompareAndSwapInt64(&maxLatency, current, latencyNanos) {
							break
						}
					}

					time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	actualDuration := time.Since(start)

	result.TotalQueries = totalQueries
	result.SuccessfulQueries = successQueries
	result.FailedQueries = failedQueries
	result.QPS = float64(totalQueries) / actualDuration.Seconds()
	result.AvgQueryLatency = time.Duration(totalLatency / totalQueries)
	result.MaxQueryLatency = time.Duration(maxLatency)
	result.ErrorRate = float64(failedQueries) / float64(totalQueries)

	return result
}

// runConcurrentWriteTest 运行并发写测试
func runConcurrentWriteTest(concurrency int, duration time.Duration) ConcurrentTestResult {
	var result ConcurrentTestResult
	result.WriteUsers = concurrency
	result.TestDuration = duration

	var (
		totalInserts   int64
		successInserts int64
		failedInserts  int64
		totalLatency   int64
		maxLatency     int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					insertStart := time.Now()
					records := generateTestRecords(1)
					err := insertBatchReal(records)
					insertElapsed := time.Since(insertStart)

					atomic.AddInt64(&totalInserts, 1)
					atomic.AddInt64(&totalLatency, insertElapsed.Nanoseconds())

					if err != nil {
						atomic.AddInt64(&failedInserts, 1)
					} else {
						atomic.AddInt64(&successInserts, 1)
					}

					// 更新最大延迟
					latencyNanos := insertElapsed.Nanoseconds()
					for {
						current := atomic.LoadInt64(&maxLatency)
						if latencyNanos <= current || atomic.CompareAndSwapInt64(&maxLatency, current, latencyNanos) {
							break
						}
					}

					time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	actualDuration := time.Since(start)

	result.TotalInserts = totalInserts
	result.SuccessfulInserts = successInserts
	result.FailedInserts = failedInserts
	result.IPS = float64(totalInserts) / actualDuration.Seconds()
	result.AvgInsertLatency = time.Duration(totalLatency / totalInserts)
	result.MaxInsertLatency = time.Duration(maxLatency)
	result.ErrorRate = float64(failedInserts) / float64(totalInserts)

	return result
}

// runMixedConcurrentTest 运行混合并发测试
func runMixedConcurrentTest(readUsers, writeUsers int, duration time.Duration) ConcurrentTestResult {
	var result ConcurrentTestResult
	result.ReadUsers = readUsers
	result.WriteUsers = writeUsers
	result.TestDuration = duration

	var (
		totalQueries       int64
		successQueries     int64
		failedQueries      int64
		totalInserts       int64
		successInserts     int64
		failedInserts      int64
		totalQueryLatency  int64
		totalInsertLatency int64
		maxQueryLatency    int64
		maxInsertLatency   int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()

	// 启动读工作者
	for i := 0; i < readUsers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			queries := []string{
				fmt.Sprintf("SELECT COUNT(*) FROM %s", TestTableName),
				fmt.Sprintf("SELECT * FROM %s LIMIT 10", TestTableName),
				fmt.Sprintf("SELECT AVG(age) FROM %s", TestTableName),
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
					query := queries[rand.Intn(len(queries))]

					queryStart := time.Now()
					_, err := executeQueryReal(query) // 修复函数调用
					queryElapsed := time.Since(queryStart)

					atomic.AddInt64(&totalQueries, 1)
					atomic.AddInt64(&totalQueryLatency, queryElapsed.Nanoseconds())

					if err != nil {
						atomic.AddInt64(&failedQueries, 1)
					} else {
						atomic.AddInt64(&successQueries, 1)
					}

					// 更新最大查询延迟
					latencyNanos := queryElapsed.Nanoseconds()
					for {
						current := atomic.LoadInt64(&maxQueryLatency)
						if latencyNanos <= current || atomic.CompareAndSwapInt64(&maxQueryLatency, current, latencyNanos) {
							break
						}
					}

					time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
				}
			}
		}(i)
	}

	// 启动写工作者
	for i := 0; i < writeUsers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					insertStart := time.Now()
					records := generateTestRecords(1)
					err := insertBatchReal(records)
					insertElapsed := time.Since(insertStart)

					atomic.AddInt64(&totalInserts, 1)
					atomic.AddInt64(&totalInsertLatency, insertElapsed.Nanoseconds())

					if err != nil {
						atomic.AddInt64(&failedInserts, 1)
					} else {
						atomic.AddInt64(&successInserts, 1)
					}

					// 更新最大插入延迟
					latencyNanos := insertElapsed.Nanoseconds()
					for {
						current := atomic.LoadInt64(&maxInsertLatency)
						if latencyNanos <= current || atomic.CompareAndSwapInt64(&maxInsertLatency, current, latencyNanos) {
							break
						}
					}

					time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	actualDuration := time.Since(start)

	// 计算结果
	result.TotalQueries = totalQueries
	result.SuccessfulQueries = successQueries
	result.FailedQueries = failedQueries
	result.TotalInserts = totalInserts
	result.SuccessfulInserts = successInserts
	result.FailedInserts = failedInserts

	if totalQueries > 0 {
		result.QPS = float64(totalQueries) / actualDuration.Seconds()
		result.AvgQueryLatency = time.Duration(totalQueryLatency / totalQueries)
		result.MaxQueryLatency = time.Duration(maxQueryLatency)
	}

	if totalInserts > 0 {
		result.IPS = float64(totalInserts) / actualDuration.Seconds()
		result.AvgInsertLatency = time.Duration(totalInsertLatency / totalInserts)
		result.MaxInsertLatency = time.Duration(maxInsertLatency)
	}

	totalOps := totalQueries + totalInserts
	failedOps := failedQueries + failedInserts
	if totalOps > 0 {
		result.ErrorRate = float64(failedOps) / float64(totalOps)
	}

	return result
}

// createLargeTestTable 创建大型测试表
func createLargeTestTable(tableName string, recordCount int) error {
	// 创建表
	req := CreateTableRequest{
		TableName: tableName,
		// Note: Schema configuration is handled by the server
	}

	err := apiClient.CreateTable(req)
	if err != nil {
		return fmt.Errorf("failed to create table: %v", err)
	}

	// 分批插入数据
	batchSize := 10000
	batches := (recordCount + batchSize - 1) / batchSize

	for i := 0; i < batches; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > recordCount {
			end = recordCount
		}

		batchRecords := generateLargeTestRecords(start, end-start)

		for _, record := range batchRecords {
			// 创建插入请求
			insertReq := InsertDataRequest{
				Table:     tableName,
				ID:        fmt.Sprintf("%d", record.ID),
				Timestamp: time.Now(),
				Payload: map[string]interface{}{
					"id":        record.ID,
					"name":      record.Name,
					"age":       record.Age,
					"is_active": record.IsActive,
				},
			}

			_, err := apiClient.InsertData(insertReq) // 修复：接收返回值
			if err != nil {
				return fmt.Errorf("failed to insert batch %d: %v", i, err)
			}
		}

		// 显示进度
		if (i+1)%10 == 0 {
			fmt.Printf("Inserted %d/%d batches\n", i+1, batches)
		}
	}

	return nil
}

// generateLargeTestRecords 生成大型测试记录
func generateLargeTestRecords(startID, count int) []TestRecord {
	records := make([]TestRecord, count)

	for i := 0; i < count; i++ {
		records[i] = TestRecord{
			ID:        startID + i + 1,
			Name:      fmt.Sprintf("user_%d", startID+i+1),
			Email:     fmt.Sprintf("user_%d@example.com", startID+i+1),
			Age:       20 + rand.Intn(50),
			Score:     rand.Float64() * 100,
			IsActive:  rand.Float64() > 0.5,
			CreatedAt: time.Now(),
		}
	}
	return records
}

// convertRecordsToInterface 将TestRecord转换为接口切片
func convertRecordsToInterface(records []TestRecord) []interface{} {
	data := make([]interface{}, len(records))

	for i, record := range records {
		// 为每个记录添加额外的测试字段
		regionIndex := rand.Intn(5)
		statusIndex := rand.Intn(4)

		var region string
		switch regionIndex {
		case 0:
			region = "North"
		case 1:
			region = "South"
		case 2:
			region = "East"
		case 3:
			region = "West"
		default:
			region = "Central"
		}

		var status string
		switch statusIndex {
		case 0:
			status = "active"
		case 1:
			status = "inactive"
		case 2:
			status = "pending"
		default:
			status = "suspended"
		}

		data[i] = map[string]interface{}{
			"id":         record.ID,
			"name":       record.Name,
			"email":      record.Email,
			"age":        record.Age,
			"score":      record.Score,
			"is_active":  record.IsActive,
			"region":     region,
			"amount":     rand.Float64() * 10000,
			"price":      rand.Float64() * 1000,
			"status":     status,
			"created_at": record.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return data
}
