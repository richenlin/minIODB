package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/miniodb/go-sdk/config"
	"github.com/miniodb/go-sdk/errors"
	"github.com/miniodb/go-sdk/models"
)

func main() {
	fmt.Println("MinIODB Go SDK 基本使用示例")
	fmt.Println("=" + string(make([]byte, 39)))

	// 创建配置
	cfg := config.NewDefaultConfig()
	cfg.Host = "localhost"
	cfg.GRPCPort = 8080

	// 验证配置
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置验证失败: %v", err)
	}

	fmt.Printf("配置信息:\n")
	fmt.Printf("  gRPC 地址: %s\n", cfg.GRPCAddress())
	fmt.Printf("  REST 基础 URL: %s\n", cfg.RESTBaseURL())
	fmt.Printf("  认证启用: %v\n", cfg.IsAuthEnabled())

	// 注意：这里是示例代码，实际的客户端类还需要实现
	// client, err := client.NewClient(cfg)
	// if err != nil {
	//     log.Fatalf("创建客户端失败: %v", err)
	// }
	// defer client.Close()

	ctx := context.Background()

	// 演示数据模型的使用
	basicUsageExample(ctx)
	errorHandlingExample()
	concurrentExample(ctx)
}

func basicUsageExample(ctx context.Context) {
	fmt.Println("\n1. 基本使用示例")
	fmt.Println("-" + string(make([]byte, 29)))

	// 创建数据记录
	record := &models.DataRecord{
		ID:        "user-123",
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"name":       "John Doe",
			"age":        30,
			"email":      "john@example.com",
			"department": "Engineering",
			"salary":     75000,
		},
	}

	fmt.Printf("示例记录: %+v\n", record)

	// 创建表配置
	tableConfig := &models.TableConfig{
		BufferSize:           1000,
		FlushIntervalSeconds: 30,
		RetentionDays:        365,
		BackupEnabled:        true,
		Properties: map[string]string{
			"description": "用户数据表",
			"owner":       "user-service",
		},
	}

	fmt.Printf("示例表配置: %+v\n", tableConfig)

	// 演示响应模型
	writeResponse := &models.WriteDataResponse{
		Success: true,
		Message: "数据写入成功",
		NodeID:  "miniodb-node-1",
	}

	fmt.Printf("写入响应示例: %+v\n", writeResponse)

	queryResponse := &models.QueryDataResponse{
		ResultJSON: `[{"name":"John Doe","age":30,"email":"john@example.com"}]`,
		HasMore:    false,
		NextCursor: "",
	}

	fmt.Printf("查询响应示例: %+v\n", queryResponse)

	// 注意：实际的客户端操作需要实现客户端类
	//
	// // 写入数据
	// writeResp, err := client.WriteData(ctx, "users", record)
	// if err != nil {
	//     log.Printf("写入数据失败: %v", err)
	//     return
	// }
	// fmt.Printf("写入成功: %v, 节点: %s\n", writeResp.Success, writeResp.NodeID)
	//
	// // 查询数据
	// queryResp, err := client.QueryData(ctx, "SELECT * FROM users WHERE age > 25", 10, "")
	// if err != nil {
	//     log.Printf("查询数据失败: %v", err)
	//     return
	// }
	// fmt.Printf("查询结果: %s\n", queryResp.ResultJSON)
	//
	// // 创建表
	// createResp, err := client.CreateTable(ctx, "products", tableConfig, true)
	// if err != nil {
	//     log.Printf("创建表失败: %v", err)
	//     return
	// }
	// fmt.Printf("表创建成功: %v\n", createResp.Success)
}

func errorHandlingExample() {
	fmt.Println("\n2. 错误处理示例")
	fmt.Println("-" + string(make([]byte, 29)))

	// 演示不同类型的错误
	errorExamples := []error{
		errors.NewConnectionError("无法连接到服务器", nil),
		errors.NewAuthenticationError("认证失败", nil),
		errors.NewRequestError("请求参数错误", nil),
		errors.NewServerError("服务器内部错误", nil),
		errors.NewTimeoutError("请求超时", nil),
	}

	for _, err := range errorExamples {
		fmt.Printf("错误类型: %T\n", err)
		fmt.Printf("错误信息: %v\n", err)

		// 检查错误类型
		switch {
		case errors.IsConnectionError(err):
			fmt.Println("  -> 这是连接错误，可能需要检查网络连接")
		case errors.IsAuthenticationError(err):
			fmt.Println("  -> 这是认证错误，可能需要检查 API 密钥")
		case errors.IsRequestError(err):
			fmt.Println("  -> 这是请求错误，可能需要检查请求参数")
		case errors.IsServerError(err):
			fmt.Println("  -> 这是服务器错误，可能需要联系管理员")
		case errors.IsTimeoutError(err):
			fmt.Println("  -> 这是超时错误，可能需要增加超时时间或重试")
		}
		fmt.Println()
	}

	// 演示实际错误处理流程
	fmt.Println("实际错误处理示例:")
	// err := someOperation()
	// if err != nil {
	//     switch {
	//     case errors.IsConnectionError(err):
	//         fmt.Printf("连接错误: %v\n", err)
	//         // 可能需要重试或检查网络
	//     case errors.IsAuthenticationError(err):
	//         fmt.Printf("认证失败: %v\n", err)
	//         // 可能需要重新获取令牌
	//     case errors.IsTimeoutError(err):
	//         fmt.Printf("请求超时: %v\n", err)
	//         // 可能需要重试
	//     default:
	//         fmt.Printf("未知错误: %v\n", err)
	//     }
	// }
}

func concurrentExample(ctx context.Context) {
	fmt.Println("\n3. 并发操作示例")
	fmt.Println("-" + string(make([]byte, 29)))

	// 演示如何准备并发操作的数据
	records := make([]*models.DataRecord, 5)
	for i := 0; i < 5; i++ {
		records[i] = &models.DataRecord{
			ID:        fmt.Sprintf("user-%d", i),
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"name":  fmt.Sprintf("User %d", i),
				"age":   25 + i,
				"email": fmt.Sprintf("user%d@example.com", i),
			},
		}
	}

	fmt.Printf("准备了 %d 条记录用于并发操作\n", len(records))

	// 演示并发操作的结构（实际实现需要客户端）
	fmt.Println("并发写入示例结构:")
	fmt.Println(`
	func concurrentWrites(client *client.Client, records []*models.DataRecord) {
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 10) // 限制并发数

		for _, record := range records {
			wg.Add(1)
			go func(r *models.DataRecord) {
				defer wg.Done()
				semaphore <- struct{}{} // 获取信号量
				defer func() { <-semaphore }() // 释放信号量

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				_, err := client.WriteData(ctx, "users", r)
				if err != nil {
					log.Printf("写入失败: %v", err)
				}
			}(record)
		}

		wg.Wait()
	}`)

	// 演示上下文控制
	fmt.Println("\n上下文控制示例:")

	// 带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fmt.Printf("创建了带 10 秒超时的上下文: %v\n", timeoutCtx.Deadline())

	// 可取消的上下文
	cancelCtx, cancelFunc := context.WithCancel(ctx)
	go func() {
		time.Sleep(5 * time.Second)
		cancelFunc() // 5秒后取消操作
	}()

	select {
	case <-cancelCtx.Done():
		fmt.Printf("上下文已取消: %v\n", cancelCtx.Err())
	case <-time.After(6 * time.Second):
		fmt.Println("操作超时")
	}

	fmt.Println("\n注意: 完整的客户端功能需要先生成 gRPC 代码并实现客户端类。")
	fmt.Println("请运行: ./scripts/generate_go.sh 来生成必要的 gRPC 代码。")
}
