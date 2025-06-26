package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// WriteResponse 写入响应结构
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

func main1() {
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

	log.Printf("开始生成测试数据...")
	log.Printf("API URL: %s", apiURL)
	log.Printf("总记录数: %d", totalRecords)
	log.Printf("并发数: %d", concurrency)
	log.Printf("JWT Token: %s", maskToken(jwtToken))

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

			client := &http.Client{
				Timeout: 30 * time.Second,
			}

			for j := 0; j < recordsPerWorker; j++ {
				recordID := fmt.Sprintf("worker-%d-record-%d", workerID, j)

				// 创建测试数据
				req := WriteRequest{
					Table:     "performance_test",
					ID:        recordID,
					Timestamp: time.Now(),
					Payload: map[string]interface{}{
						"worker_id":    workerID,
						"record_index": j,
						"value":        fmt.Sprintf("test-value-%d-%d", workerID, j),
						"score":        float64(j * 10),
						"active":       j%2 == 0,
						"created_at":   time.Now().Unix(),
					},
				}

				// 发送HTTP请求
				if err := sendWriteRequest(client, apiURL+"/v1/data", req, jwtToken); err != nil {
					log.Printf("Worker %d 发送请求失败: %v", workerID, err)
					mu.Lock()
					errorCount++
					mu.Unlock()
				} else {
					mu.Lock()
					successCount++
					mu.Unlock()
				}

				// 每100个请求输出一次进度
				if (j+1)%100 == 0 {
					log.Printf("Worker %d 已完成 %d 个请求", workerID, j+1)
				}
			}

			log.Printf("Worker %d 完成所有请求", workerID)
		}(i)
	}

	// 等待所有worker完成
	wg.Wait()

	duration := time.Since(startTime)
	log.Printf("数据生成完成!")
	log.Printf("总耗时: %v", duration)
	log.Printf("成功请求: %d", successCount)
	log.Printf("失败请求: %d", errorCount)
	log.Printf("每秒请求数: %.2f", float64(successCount)/duration.Seconds())
}

// sendWriteRequest 发送写入请求
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
