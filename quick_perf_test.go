package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const BaseURL = "http://localhost:8081/v1"

type WriteRequest struct {
	Table string                 `json:"table"`
	Data  map[string]interface{} `json:"data"`
}

type QueryRequest struct {
	SQL string `json:"sql"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

func main() {
	fmt.Println("🚀 MinIODB 快速性能测试")
	fmt.Println("========================")

	// 1. 健康检查性能测试
	fmt.Println("\n📊 健康检查性能测试...")
	healthStart := time.Now()
	healthSuccess := 0
	healthTotal := 10

	for i := 0; i < healthTotal; i++ {
		if err := healthCheck(); err == nil {
			healthSuccess++
		}
	}
	healthDuration := time.Since(healthStart)
	fmt.Printf("   测试次数: %d, 成功次数: %d, 总耗时: %v", healthTotal, healthSuccess, healthDuration)
	fmt.Printf("   平均响应时间: %v, QPS: %.1f\n", 
		healthDuration/time.Duration(healthTotal), 
		float64(healthSuccess)/healthDuration.Seconds())

	// 2. 写入性能测试
	fmt.Println("\n📝 写入性能测试...")
	writeStart := time.Now()
	writeSuccess := 0
	writeTotal := 10

	for i := 0; i < writeTotal; i++ {
		if err := writeData(fmt.Sprintf("quick_test_%d", i), i); err == nil {
			writeSuccess++
		}
		// 去除延迟，直接测试实际性能
	}
	writeDuration := time.Since(writeStart)
	fmt.Printf("   测试次数: %d, 成功次数: %d, 总耗时: %v", writeTotal, writeSuccess, writeDuration)
	fmt.Printf("   平均响应时间: %v, TPS: %.1f\n", 
		writeDuration/time.Duration(writeTotal), 
		float64(writeSuccess)/writeDuration.Seconds())

	// 3. 查询性能测试
	fmt.Println("\n🔍 查询性能测试...")
	queryStart := time.Now()
	querySuccess := 0
	queryTotal := 10

	for i := 0; i < queryTotal; i++ {
		if err := queryData("SELECT COUNT(*) FROM quick_test"); err == nil {
			querySuccess++
		}
	}
	queryDuration := time.Since(queryStart)
	fmt.Printf("   测试次数: %d, 成功次数: %d, 总耗时: %v", queryTotal, querySuccess, queryDuration)
	fmt.Printf("   平均响应时间: %v, QPS: %.1f\n", 
		queryDuration/time.Duration(queryTotal), 
		float64(querySuccess)/queryDuration.Seconds())

	// 4. 混合负载测试
	fmt.Println("\n⚡ 混合负载测试...")
	mixedStart := time.Now()
	mixedOps := 0
	
	// 5次写入 + 5次查询，交替进行
	for i := 0; i < 5; i++ {
		// 写入
		if err := writeData(fmt.Sprintf("mixed_test_%d", i), i); err == nil {
			mixedOps++
		}
		// 查询
		if err := queryData("SELECT COUNT(*) FROM mixed_test"); err == nil {
			mixedOps++
		}
	}
	mixedDuration := time.Since(mixedStart)
	fmt.Printf("   总操作数: %d, 总耗时: %v", mixedOps, mixedDuration)
	fmt.Printf("   平均操作时间: %v, OPS: %.1f\n", 
		mixedDuration/time.Duration(mixedOps), 
		float64(mixedOps)/mixedDuration.Seconds())

	// 5. 资源效率分析
	fmt.Println("\n💡 性能分析总结")
	fmt.Println("================")
	fmt.Printf("✅ 健康检查: %.1f QPS (平均 %v 延迟)\n", 
		float64(healthSuccess)/healthDuration.Seconds(),
		healthDuration/time.Duration(healthTotal))
	fmt.Printf("✅ 数据写入: %.1f TPS (平均 %v 延迟)\n", 
		float64(writeSuccess)/writeDuration.Seconds(),
		writeDuration/time.Duration(writeTotal))
	fmt.Printf("✅ 数据查询: %.1f QPS (平均 %v 延迟)\n", 
		float64(querySuccess)/queryDuration.Seconds(),
		queryDuration/time.Duration(queryTotal))
	fmt.Printf("✅ 混合负载: %.1f OPS (平均 %v 延迟)\n", 
		float64(mixedOps)/mixedDuration.Seconds(),
		mixedDuration/time.Duration(mixedOps))

	fmt.Println("\n🎉 快速性能测试完成！")
}

func healthCheck() error {
	resp, err := http.Get(BaseURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func writeData(name string, id int) error {
	data := WriteRequest{
		Table: "quick_test",
		Data: map[string]interface{}{
			"id":   id,
			"name": name,
			"value": id * 10,
		},
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := http.Post(BaseURL+"/write", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("write failed: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

func queryData(sql string) error {
	data := QueryRequest{SQL: sql}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	resp, err := http.Post(BaseURL+"/query", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("query failed: %d %s", resp.StatusCode, string(body))
	}
	return nil
} 