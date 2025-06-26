package performance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"testing"
	"time"
)

const (
	// API endpoints
	baseURL   = "http://localhost:8081"
	writeURL  = baseURL + "/v1/data"
	queryURL  = baseURL + "/v1/query"
	healthURL = baseURL + "/v1/health"

	// Test data configuration
	batchSize       = 1000
	numTables       = 10
	recordsPerTable = 100000 // 每个表100万条记录
)

// TestData 测试数据结构
type TestData struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
	Table     string                 `json:"table,omitempty"`
}

// QueryRequest 查询请求结构
type QueryRequest struct {
	SQL string `json:"sql"`
}

// WriteResponse 写入响应结构
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// QueryResponse 查询响应结构
type QueryResponse struct {
	ResultJSON string `json:"result_json"`
}

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
		_, err := executeQuery(client, sql)
		elapsed := time.Since(start)

		if err != nil {
			b.Errorf("Query failed: %v", err)
		}

		b.ReportMetric(elapsed.Seconds()*1000, "ms/query")
	}
}

// BenchmarkLargeDatasetQuery 测试大数据集查询性能
func BenchmarkLargeDatasetQuery(b *testing.B) {
	if !waitForService(b) {
		b.Skip("Service not available")
	}

	// 准备大量测试数据 (模拟TB级数据的一部分)
	b.Log("Preparing large dataset...")
	prepareTestData(b, 500000) // 准备50万条测试数据

	// 测试不同复杂度的查询
	largeQueries := []struct {
		name string
		sql  string
	}{
		{"FullTableScan", "SELECT COUNT(*) FROM default_table"},
		{"GroupByAggregation", "SELECT payload->>'category', COUNT(*), AVG(CAST(payload->>'value' AS FLOAT)) FROM default_table GROUP BY payload->>'category'"},
		{"ComplexJoinLike", "SELECT payload->>'user_id', COUNT(*) as event_count FROM default_table WHERE payload->>'event_type' IN ('login', 'purchase', 'view') GROUP BY payload->>'user_id' HAVING COUNT(*) > 5 ORDER BY event_count DESC LIMIT 100"},
		{"TimeSeriesAnalysis", "SELECT DATE(timestamp) as day, COUNT(*) as daily_count FROM default_table WHERE timestamp >= '2024-01-01' GROUP BY DATE(timestamp) ORDER BY day"},
	}

	for _, query := range largeQueries {
		b.Run(query.name, func(b *testing.B) {
			benchmarkLargeQuery(b, query.sql)
		})
	}
}

// benchmarkLargeQuery 执行大数据集查询基准测试
func benchmarkLargeQuery(b *testing.B, sql string) {
	client := &http.Client{Timeout: 300 * time.Second} // 5分钟超时

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		result, err := executeQuery(client, sql)
		elapsed := time.Since(start)

		if err != nil {
			b.Errorf("Large query failed: %v", err)
		}

		// 计算查询的数据量
		dataSize := len(result)
		throughput := float64(dataSize) / elapsed.Seconds() / (1024 * 1024) // MB/s

		b.ReportMetric(elapsed.Seconds()*1000, "ms/query")
		b.ReportMetric(throughput, "MB/s")

		b.Logf("Query completed in %v, processed %d bytes, throughput: %.2f MB/s",
			elapsed, dataSize, throughput)
	}
}

// BenchmarkConcurrentMixedWorkload 测试混合工作负载性能
func BenchmarkConcurrentMixedWorkload(b *testing.B) {
	if !waitForService(b) {
		b.Skip("Service not available")
	}

	// 准备基础数据
	prepareTestData(b, 10000)

	var wg sync.WaitGroup
	writeWorkers := 10
	queryWorkers := 5

	// 启动写入工作者
	for i := 0; i < writeWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := &http.Client{Timeout: 30 * time.Second}

			for j := 0; j < b.N/writeWorkers; j++ {
				data := generateTestData(workerID*1000 + j)
				if err := writeData(client, data); err != nil {
					b.Errorf("Write failed: %v", err)
				}
			}
		}(i)
	}

	// 启动查询工作者
	queries := []string{
		"SELECT COUNT(*) FROM default_table",
		"SELECT * FROM default_table LIMIT 100",
		"SELECT payload->>'category', COUNT(*) FROM default_table GROUP BY payload->>'category' LIMIT 10",
	}

	for i := 0; i < queryWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			client := &http.Client{Timeout: 60 * time.Second}

			for j := 0; j < b.N/queryWorkers; j++ {
				sql := queries[j%len(queries)]
				if _, err := executeQuery(client, sql); err != nil {
					b.Errorf("Query failed: %v", err)
				}
			}
		}(i)
	}

	b.ResetTimer()
	wg.Wait()
}

// 辅助函数

// waitForService 等待服务启动
func waitForService(b *testing.B) bool {
	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < 30; i++ { // 等待最多30秒
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return true
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}

	b.Log("Service health check failed")
	return false
}

// generateTestData 生成测试数据
func generateTestData(id int) TestData {
	categories := []string{"electronics", "clothing", "books", "home", "sports", "automotive"}
	eventTypes := []string{"view", "click", "purchase", "login", "logout", "search"}

	return TestData{
		ID:        fmt.Sprintf("test_%d", id),
		Timestamp: time.Now().Add(-time.Duration(rand.Intn(86400*30)) * time.Second), // 过去30天内的随机时间
		Payload: map[string]interface{}{
			"user_id":    fmt.Sprintf("user_%d", rand.Intn(10000)),
			"category":   categories[rand.Intn(len(categories))],
			"event_type": eventTypes[rand.Intn(len(eventTypes))],
			"value":      rand.Float64() * 1000,
			"timestamp":  time.Now().Unix(),
			"metadata": map[string]interface{}{
				"ip_address": fmt.Sprintf("192.168.%d.%d", rand.Intn(255), rand.Intn(255)),
				"user_agent": "BenchmarkClient/1.0",
				"session_id": fmt.Sprintf("session_%d", rand.Intn(1000)),
			},
		},
	}
}

// writeData 写入数据
func writeData(client *http.Client, data TestData) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := client.Post(writeURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("write failed with status: %d", resp.StatusCode)
	}

	return nil
}

// executeQuery 执行查询
func executeQuery(client *http.Client, sql string) (string, error) {
	query := QueryRequest{SQL: sql}
	jsonData, err := json.Marshal(query)
	if err != nil {
		return "", err
	}

	resp, err := client.Post(queryURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query failed with status: %d", resp.StatusCode)
	}

	var result QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ResultJSON, nil
}

// prepareTestData 准备测试数据
func prepareTestData(b *testing.B, count int) {
	client := &http.Client{Timeout: 30 * time.Second}

	// 并发写入数据
	concurrency := 20
	var wg sync.WaitGroup
	workChan := make(chan int, count)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range workChan {
				data := generateTestData(id)
				if err := writeData(client, data); err != nil {
					b.Logf("Failed to prepare data %d: %v", id, err)
				}
			}
		}()
	}

	for i := 0; i < count; i++ {
		workChan <- i
	}
	close(workChan)

	wg.Wait()

	// 等待数据刷新到存储
	time.Sleep(5 * time.Second)
	b.Logf("Prepared %d test records", count)
}
