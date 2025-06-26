package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// WriteRequest 写入请求结构
type WriteRequest struct {
	Table     string                 `json:"table,omitempty"`
	ID        string                 `json:"id"`
	Timestamp string                 `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// WriteResponse 写入响应结构
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

// 配置参数
type Config struct {
	MaxRetries      int
	RetryDelay      time.Duration
	RequestInterval time.Duration
	Timeout         time.Duration
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("使用方法: data_generator <API_URL> <总记录数> <并发数> [JWT_TOKEN]")
		fmt.Println("示例: data_generator http://localhost:8081 1000 10 your-jwt-token")
		os.Exit(1)
	}

	apiURL := os.Args[1]
	totalRecords, _ := strconv.Atoi(os.Args[2])
	concurrency, _ := strconv.Atoi(os.Args[3])

	// JWT Token
	jwtToken := ""
	if len(os.Args) > 4 {
		jwtToken = os.Args[4]
	} else {
		// 尝试从环境变量获取
		jwtToken = os.Getenv("JWT_TOKEN")
		if jwtToken == "" {
			log.Println("警告: 未提供JWT token，请求可能会失败")
		}
	}

	// 配置参数
	config := Config{
		MaxRetries:      3,
		RetryDelay:      100 * time.Millisecond,
		RequestInterval: 10 * time.Millisecond, // 请求间隔
		Timeout:         30 * time.Second,
	}

	log.Printf("开始生成测试数据...")
	log.Printf("API URL: %s", apiURL)
	log.Printf("总记录数: %d", totalRecords)
	log.Printf("并发数: %d", concurrency)
	log.Printf("JWT Token: %s", maskToken(jwtToken))
	log.Printf("最大重试次数: %d", config.MaxRetries)
	log.Printf("请求间隔: %v", config.RequestInterval)

	var wg sync.WaitGroup
	var successCount, errorCount int64
	var mu sync.Mutex

	recordsPerWorker := totalRecords / concurrency
	startTime := time.Now()

	// 启动worker goroutines
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// 创建优化的HTTP客户端
			client := createOptimizedHTTPClient(config.Timeout)
			apiEndpoint := apiURL + "/v1/data"

			for j := 0; j < recordsPerWorker; j++ {
				// 生成测试数据
				testData := WriteRequest{
					Table:     "test_table",
					ID:        fmt.Sprintf("record_%d_%d", workerID, j),
					Timestamp: time.Now().Format(time.RFC3339),
					Payload: map[string]interface{}{
						"worker_id":  workerID,
						"record_num": j,
						"value":      rand.Float64() * 100,
						"status":     "active",
						"timestamp":  time.Now().Unix(),
						"metadata": map[string]interface{}{
							"source": "data_generator",
							"batch":  j / 100,
						},
					},
				}

				// 发送请求（带重试机制）
				success := false
				for retry := 0; retry <= config.MaxRetries; retry++ {
					err := sendWriteRequest(client, apiEndpoint, testData, jwtToken)
					if err == nil {
						success = true
						break
					}

					if retry < config.MaxRetries {
						// 指数退避
						backoffDelay := config.RetryDelay * time.Duration(1<<retry)
						time.Sleep(backoffDelay)
						log.Printf("Worker %d 重试请求 %d/%d (延迟 %v): %v",
							workerID, retry+1, config.MaxRetries, backoffDelay, err)
					} else {
						log.Printf("Worker %d 请求最终失败: %v", workerID, err)
					}
				}

				mu.Lock()
				if success {
					successCount++
				} else {
					errorCount++
				}
				mu.Unlock()

				// 请求间隔控制
				if config.RequestInterval > 0 {
					time.Sleep(config.RequestInterval)
				}
			}

			log.Printf("Worker %d 完成所有请求", workerID)
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)
	log.Printf("数据生成完成!")
	log.Printf("总耗时: %v", duration)
	log.Printf("成功请求: %d", successCount)
	log.Printf("失败请求: %d", errorCount)
	log.Printf("成功率: %.2f%%", float64(successCount)/float64(totalRecords)*100)
	log.Printf("每秒请求数: %.2f", float64(totalRecords)/duration.Seconds())
	log.Printf("有效吞吐量: %.2f QPS", float64(successCount)/duration.Seconds())
}

// createOptimizedHTTPClient 创建优化的HTTP客户端
func createOptimizedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,              // 最大空闲连接数
			MaxIdleConnsPerHost: 100,              // 每个主机的最大空闲连接数
			MaxConnsPerHost:     200,              // 每个主机的最大连接数
			IdleConnTimeout:     90 * time.Second, // 空闲连接超时
			DisableKeepAlives:   false,            // 启用Keep-Alive
		},
	}
}

func sendWriteRequest(client *http.Client, url string, req WriteRequest, jwtToken string) error {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %v", err)
	}

	// 设置请求头
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Connection", "keep-alive") // 明确启用Keep-Alive
	if jwtToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+jwtToken)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP错误 %d: %s", resp.StatusCode, string(body))
	}

	var writeResp WriteResponse
	if err := json.Unmarshal(body, &writeResp); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if !writeResp.Success {
		return fmt.Errorf("写入失败: %s", writeResp.Message)
	}

	return nil
}

// maskToken 遮盖token用于日志输出
func maskToken(token string) string {
	if token == "" {
		return "未提供"
	}
	if len(token) <= 10 {
		return "*****"
	}
	return token[:5] + "..." + token[len(token)-5:]
}
