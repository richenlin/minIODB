package performance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 基准测试配置
const (
	// Test data configuration
	batchSize       = 1000
	numTables       = 10
	recordsPerTable = 100000 // 每个表100万条记录
)

// BenchmarkWriteThroughput 测试写入吞吐量
func BenchmarkWriteThroughput(b *testing.B) {
	// 等待服务启动
	if !waitForService(b) {
		b.Skip("Service not available")
	}

	b.ResetTimer()

	// 测试不同并发级别的写入性能
	concurrencyLevels := []int{1, 10, 50, 100, 200}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			benchmarkWriteWithConcurrency(b, concurrency)
		})
	}
}

// benchmarkWriteWithConcurrency 测试指定并发级别的写入性能
func benchmarkWriteWithConcurrency(b *testing.B, concurrency int) {
	var wg sync.WaitGroup

	// 创建工作通道
	workChan := make(chan int, b.N)

	// 启动工作协程
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := &http.Client{Timeout: 30 * time.Second}

			for range workChan {
				data := generateTestData(workerID)
				if err := writeData(client, data); err != nil {
					b.Errorf("Write failed: %v", err)
				}
			}
		}(i)
	}

	// 发送工作任务
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		workChan <- i
	}
	close(workChan)

	wg.Wait()

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "writes/sec")
	b.ReportMetric(elapsed.Seconds()/float64(b.N)*1000, "ms/write")
}

// BenchmarkQueryPerformance 测试查询性能
func BenchmarkQueryPerformance(b *testing.B) {
	if !waitForService(b) {
		b.Skip("Service not available")
	}

	// 先写入测试数据
	b.Log("Preparing test data...")
	prepareTestData(b, 50000) // 准备5万条测试数据

	queries := []struct {
		name string
		sql  string
	}{
		{"SimpleSelect", "SELECT COUNT(*) FROM default_table"},
		{"FilterQuery", "SELECT * FROM default_table WHERE payload->>'user_id' = 'user_1000' LIMIT 100"},
		{"AggregateQuery", "SELECT payload->>'category', COUNT(*) FROM default_table GROUP BY payload->>'category' LIMIT 10"},
		{"TimeRangeQuery", "SELECT * FROM default_table WHERE timestamp > '2024-01-01' AND timestamp < '2024-12-31' LIMIT 1000"},
		{"ComplexQuery", "SELECT payload->>'category', AVG(CAST(payload->>'value' AS FLOAT)) as avg_value FROM default_table WHERE timestamp > '2024-01-01' GROUP BY payload->>'category' HAVING COUNT(*) > 10 LIMIT 20"},
	}

	for _, query := range queries {
		b.Run(query.name, func(b *testing.B) {
			benchmarkQuery(b, query.sql)
		})
	}
}

// benchmarkQuery 执行查询基准测试
func benchmarkQuery(b *testing.B, sql string) {
	client := &http.Client{Timeout: 60 * time.Second}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		_, err := executeQuery1(client, sql)
		elapsed := time.Since(start)

		if err != nil {
			b.Errorf("Query failed: %v", err)
		}

		b.ReportMetric(elapsed.Seconds()*1000, "ms/query")
	}
}

// waitForService 等待服务启动
func waitForService(b *testing.B) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	maxAttempts := 12

	for i := 0; i < maxAttempts; i++ {
		resp, err := client.Get(fmt.Sprintf("%s/v1/health", BaseURL))
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			b.Logf("Service is ready after %d attempts", i+1)
			return true
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}

	b.Logf("Service not ready after %d attempts", maxAttempts)
	return false
}

// generateTestData 生成测试数据
func generateTestData(id int) TestData {
	return TestData{
		ID:        fmt.Sprintf("test_%d_%d", id, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"user_id":    fmt.Sprintf("user_%d", id%1000),
			"category":   fmt.Sprintf("category_%d", id%10),
			"value":      float64(id % 100),
			"event_type": []string{"login", "purchase", "view", "click"}[id%4],
		},
		Table: "default_table",
	}
}

// writeData 写入数据
func writeData(client *http.Client, data TestData) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := client.Post(fmt.Sprintf("%s/v1/data", BaseURL), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("write failed with status: %d", resp.StatusCode)
	}

	return nil
}

// executeQuery 执行查询
func executeQuery1(client *http.Client, sql string) (string, error) {
	queryReq := QueryRequest{SQL: sql}
	jsonData, err := json.Marshal(queryReq)
	if err != nil {
		return "", err
	}

	resp, err := client.Post(fmt.Sprintf("%s/v1/query", BaseURL), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query failed with status: %d", resp.StatusCode)
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	return buf.String(), err
}

// prepareTestData 准备测试数据
func prepareTestData(b *testing.B, count int) {
	client := &http.Client{Timeout: 60 * time.Second}

	batchSize := 1000
	batches := count / batchSize

	for i := 0; i < batches; i++ {
		for j := 0; j < batchSize; j++ {
			data := generateTestData(i*batchSize + j)
			if err := writeData(client, data); err != nil {
				b.Logf("Failed to prepare test data: %v", err)
			}
		}

		if i%10 == 0 {
			b.Logf("Prepared %d/%d batches", i, batches)
		}
	}

	b.Logf("Test data preparation completed: %d records", count)
}
