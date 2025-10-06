package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/miniodb/go-sdk/config"
	"github.com/miniodb/go-sdk/errors"
	"github.com/miniodb/go-sdk/models"
)

func main() {
	fmt.Println("MinIODB Go SDK 高级使用示例")
	fmt.Println(strings.Repeat("=", 50))

	// 创建高级配置
	cfg := &config.Config{
		Host:     "localhost",
		GRPCPort: 8080,
		RESTPort: 8081,
		Auth: &config.AuthConfig{
			APIKey:    "demo-api-key",
			Secret:    "demo-secret",
			TokenType: "Bearer",
		},
		Connection: &config.ConnectionConfig{
			MaxConnections:        20,
			Timeout:               60 * time.Second,
			RetryAttempts:         5,
			KeepAliveTime:         10 * time.Minute,
			KeepAliveTimeout:      10 * time.Second,
			MaxReceiveMessageSize: 8 * 1024 * 1024, // 8MB
			MaxSendMessageSize:    8 * 1024 * 1024, // 8MB
		},
		Logging: &config.LoggingConfig{
			Level:                    config.LogLevelInfo,
			Format:                   config.LogFormatJSON,
			EnableRequestLogging:     true,
			EnablePerformanceLogging: true,
		},
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置验证失败: %v", err)
	}

	configJSON, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Printf("高级配置:\n%s\n", configJSON)

	ctx := context.Background()

	// 注意：以下是完整客户端实现后的示例代码
	// client, err := client.NewClient(cfg)
	// if err != nil {
	//     log.Fatalf("创建客户端失败: %v", err)
	// }
	// defer client.Close()

	// 演示各种高级功能
	concurrentOperationsExample(ctx)
	contextControlExample(ctx)
	errorHandlingExample(ctx)
	performanceMonitoringExample(ctx)
	dataAnalysisExample(ctx)
	backupMaintenanceExample(ctx)
}

func concurrentOperationsExample(ctx context.Context) {
	fmt.Println("\n1. 并发操作示例")
	fmt.Println(strings.Repeat("-", 30))

	// 准备测试数据
	records := make([]*models.DataRecord, 1000)
	for i := 0; i < 1000; i++ {
		records[i] = &models.DataRecord{
			ID:        fmt.Sprintf("user_%04d", i),
			Timestamp: time.Now(),
			Payload: map[string]interface{}{
				"user_id":    fmt.Sprintf("user_%04d", i),
				"name":       fmt.Sprintf("User %d", i),
				"email":      fmt.Sprintf("user%d@example.com", i),
				"age":        20 + (i % 50),
				"department": []string{"Engineering", "Sales", "Marketing", "HR"}[i%4],
				"salary":     50000 + (i * 100),
				"join_date":  time.Now().AddDate(0, 0, -i).Format("2006-01-02"),
				"active":     i%10 != 0, // 90% 活跃用户
			},
		}
	}

	fmt.Printf("准备了 %d 条测试记录\n", len(records))

	// 演示并发写入的结构
	fmt.Println("并发写入示例结构:")
	fmt.Println(`
func concurrentWrites(client *client.Client, records []*models.DataRecord) error {
	const maxConcurrency = 10
	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var totalWritten int64
	var errors []error

	startTime := time.Now()

	for _, record := range records {
		wg.Add(1)
		go func(r *models.DataRecord) {
			defer wg.Done()
			
			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 创建带超时的上下文
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// 执行写入操作
			response, err := client.WriteData(ctx, "users", r)
			
			mu.Lock()
			if err != nil {
				errors = append(errors, err)
			} else if response.Success {
				totalWritten++
			}
			mu.Unlock()
		}(record)
	}

	wg.Wait()
	elapsed := time.Since(startTime)
	
	fmt.Printf("并发写入完成: %d 条记录，耗时 %v\n", totalWritten, elapsed)
	fmt.Printf("写入速度: %.0f 记录/秒\n", float64(totalWritten)/elapsed.Seconds())
	
	if len(errors) > 0 {
		fmt.Printf("发生 %d 个错误\n", len(errors))
	}
	
	return nil
}`)

	// 演示批量操作
	fmt.Println("\n批量操作示例:")
	batchSize := 100
	totalBatches := len(records) / batchSize

	fmt.Printf("将 %d 条记录分为 %d 个批次，每批 %d 条\n", len(records), totalBatches, batchSize)

	for i := 0; i < totalBatches; i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[start:end]
		fmt.Printf("批次 %d: %d 条记录 (索引 %d-%d)\n", i+1, len(batch), start, end-1)

		// 实际实现中这里会调用 client.StreamWrite
		// response, err := client.StreamWrite(ctx, "users", batch)
	}
}

func contextControlExample(ctx context.Context) {
	fmt.Println("\n2. 上下文控制示例")
	fmt.Println(strings.Repeat("-", 30))

	// 超时控制示例
	fmt.Println("超时控制示例:")
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fmt.Printf("创建了 10 秒超时的上下文: %v\n", timeoutCtx.Deadline())

	// 模拟操作
	select {
	case <-time.After(5 * time.Second):
		fmt.Println("操作在超时前完成")
	case <-timeoutCtx.Done():
		fmt.Printf("操作超时: %v\n", timeoutCtx.Err())
	}

	// 取消控制示例
	fmt.Println("\n取消控制示例:")
	cancelCtx, cancelFunc := context.WithCancel(ctx)

	go func() {
		time.Sleep(3 * time.Second)
		fmt.Println("3秒后取消操作")
		cancelFunc()
	}()

	select {
	case <-time.After(5 * time.Second):
		fmt.Println("操作正常完成")
	case <-cancelCtx.Done():
		fmt.Printf("操作被取消: %v\n", cancelCtx.Err())
	}

	// 值传递示例
	fmt.Println("\n上下文值传递示例:")
	type contextKey string
	const userIDKey contextKey = "user_id"
	const requestIDKey contextKey = "request_id"

	valueCtx := context.WithValue(ctx, userIDKey, "user-123")
	valueCtx = context.WithValue(valueCtx, requestIDKey, "req-456")

	if userID := valueCtx.Value(userIDKey); userID != nil {
		fmt.Printf("用户ID: %s\n", userID)
	}
	if requestID := valueCtx.Value(requestIDKey); requestID != nil {
		fmt.Printf("请求ID: %s\n", requestID)
	}
}

func errorHandlingExample(ctx context.Context) {
	fmt.Println("\n3. 错误处理和重试示例")
	fmt.Println(strings.Repeat("-", 30))

	// 重试机制示例
	fmt.Println("重试机制示例:")

	type retryableOperation func() error

	executeWithRetry := func(operation retryableOperation, maxRetries int) error {
		var lastErr error

		for attempt := 0; attempt <= maxRetries; attempt++ {
			err := operation()
			if err == nil {
				if attempt > 0 {
					fmt.Printf("操作在第 %d 次尝试后成功\n", attempt+1)
				}
				return nil
			}

			lastErr = err

			// 检查错误类型决定是否重试
			if errors.IsAuthenticationError(err) || errors.IsRequestError(err) {
				fmt.Printf("不可重试的错误: %v\n", err)
				return err
			}

			if attempt < maxRetries {
				backoffTime := time.Duration(1<<attempt) * time.Second // 指数退避
				fmt.Printf("尝试 %d 失败: %v，%v 后重试\n", attempt+1, err, backoffTime)
				time.Sleep(backoffTime)
			}
		}

		fmt.Printf("所有重试都失败，最后错误: %v\n", lastErr)
		return lastErr
	}

	// 模拟不同类型的操作
	operations := []struct {
		name      string
		operation retryableOperation
	}{
		{
			name: "模拟连接错误",
			operation: func() error {
				// 模拟随机失败
				if time.Now().UnixNano()%3 == 0 {
					return errors.NewConnectionError("模拟连接失败", nil)
				}
				return nil
			},
		},
		{
			name: "模拟认证错误",
			operation: func() error {
				return errors.NewAuthenticationError("模拟认证失败", nil)
			},
		},
		{
			name: "模拟服务器错误",
			operation: func() error {
				if time.Now().UnixNano()%2 == 0 {
					return errors.NewServerError("模拟服务器错误", nil)
				}
				return nil
			},
		},
	}

	for _, op := range operations {
		fmt.Printf("\n执行操作: %s\n", op.name)
		err := executeWithRetry(op.operation, 3)
		if err != nil {
			fmt.Printf("操作最终失败: %v\n", err)
		} else {
			fmt.Printf("操作成功\n")
		}
	}
}

func performanceMonitoringExample(ctx context.Context) {
	fmt.Println("\n4. 性能监控示例")
	fmt.Println(strings.Repeat("-", 30))

	// 性能指标收集
	type PerformanceMetrics struct {
		OperationCount int64
		TotalDuration  time.Duration
		MinDuration    time.Duration
		MaxDuration    time.Duration
		ErrorCount     int64
		SuccessCount   int64
	}

	metrics := &PerformanceMetrics{
		MinDuration: time.Hour, // 初始化为很大的值
	}

	// 模拟操作监控
	simulateOperation := func(name string, duration time.Duration, success bool) {
		start := time.Now()

		// 模拟操作
		time.Sleep(duration)

		elapsed := time.Since(start)

		// 更新指标
		metrics.OperationCount++
		metrics.TotalDuration += elapsed

		if elapsed < metrics.MinDuration {
			metrics.MinDuration = elapsed
		}
		if elapsed > metrics.MaxDuration {
			metrics.MaxDuration = elapsed
		}

		if success {
			metrics.SuccessCount++
		} else {
			metrics.ErrorCount++
		}

		fmt.Printf("%s: 耗时 %v, 成功: %v\n", name, elapsed, success)
	}

	// 模拟一系列操作
	operations := []struct {
		name     string
		duration time.Duration
		success  bool
	}{
		{"写入操作", 50 * time.Millisecond, true},
		{"查询操作", 30 * time.Millisecond, true},
		{"更新操作", 45 * time.Millisecond, false},
		{"删除操作", 25 * time.Millisecond, true},
		{"批量操作", 200 * time.Millisecond, true},
	}

	fmt.Println("执行模拟操作:")
	for _, op := range operations {
		simulateOperation(op.name, op.duration, op.success)
	}

	// 计算和显示统计信息
	fmt.Printf("\n性能统计:\n")
	fmt.Printf("  总操作数: %d\n", metrics.OperationCount)
	fmt.Printf("  成功操作: %d\n", metrics.SuccessCount)
	fmt.Printf("  失败操作: %d\n", metrics.ErrorCount)
	fmt.Printf("  成功率: %.2f%%\n", float64(metrics.SuccessCount)/float64(metrics.OperationCount)*100)
	fmt.Printf("  总耗时: %v\n", metrics.TotalDuration)
	fmt.Printf("  平均耗时: %v\n", metrics.TotalDuration/time.Duration(metrics.OperationCount))
	fmt.Printf("  最小耗时: %v\n", metrics.MinDuration)
	fmt.Printf("  最大耗时: %v\n", metrics.MaxDuration)
}

func dataAnalysisExample(ctx context.Context) {
	fmt.Println("\n5. 数据分析示例")
	fmt.Println(strings.Repeat("-", 30))

	// 定义分析查询
	analysisQueries := []struct {
		name  string
		query string
	}{
		{
			name: "部门统计分析",
			query: `
				SELECT 
					department,
					COUNT(*) as employee_count,
					AVG(age) as avg_age,
					AVG(salary) as avg_salary,
					MIN(salary) as min_salary,
					MAX(salary) as max_salary
				FROM users 
				WHERE active = true
				GROUP BY department
				ORDER BY avg_salary DESC
			`,
		},
		{
			name: "年龄分布分析",
			query: `
				SELECT 
					CASE 
						WHEN age < 25 THEN '20-24'
						WHEN age < 30 THEN '25-29'
						WHEN age < 35 THEN '30-34'
						WHEN age < 40 THEN '35-39'
						ELSE '40+'
					END as age_group,
					COUNT(*) as count,
					AVG(salary) as avg_salary
				FROM users
				WHERE active = true
				GROUP BY age_group
				ORDER BY age_group
			`,
		},
		{
			name: "薪资趋势分析",
			query: `
				SELECT 
					strftime('%Y-%m', join_date) as join_month,
					COUNT(*) as new_hires,
					AVG(salary) as avg_starting_salary
				FROM users
				WHERE join_date >= date('now', '-12 months')
				GROUP BY join_month
				ORDER BY join_month
			`,
		},
	}

	for _, analysis := range analysisQueries {
		fmt.Printf("\n执行分析: %s\n", analysis.name)
		fmt.Printf("查询语句:\n%s\n", analysis.query)

		// 模拟查询执行
		start := time.Now()

		// 实际实现中这里会调用:
		// response, err := client.QueryData(ctx, analysis.query, 100, "")

		// 模拟查询耗时
		time.Sleep(time.Duration(50+len(analysis.query)/10) * time.Millisecond)

		elapsed := time.Since(start)
		fmt.Printf("查询耗时: %v\n", elapsed)

		// 模拟结果处理
		fmt.Printf("结果处理: 假设返回了多行数据\n")
	}
}

func backupMaintenanceExample(ctx context.Context) {
	fmt.Println("\n6. 备份和维护示例")
	fmt.Println(strings.Repeat("-", 30))

	// 备份操作示例
	fmt.Println("备份操作示例:")

	// 模拟备份操作
	backupOperations := []struct {
		name      string
		operation func() error
	}{
		{
			name: "触发元数据备份",
			operation: func() error {
				fmt.Println("  正在备份元数据...")
				time.Sleep(100 * time.Millisecond)
				fmt.Println("  备份ID: backup_20240115_103000")
				fmt.Println("  备份时间:", time.Now().Format("2006-01-02 15:04:05"))
				return nil
			},
		},
		{
			name: "列出最近备份",
			operation: func() error {
				fmt.Println("  查询最近7天的备份...")
				time.Sleep(50 * time.Millisecond)

				backups := []struct {
					name string
					time string
					size string
				}{
					{"backup_20240115_103000.json", "2024-01-15 10:30:00", "2.5MB"},
					{"backup_20240114_103000.json", "2024-01-14 10:30:00", "2.4MB"},
					{"backup_20240113_103000.json", "2024-01-13 10:30:00", "2.3MB"},
				}

				fmt.Printf("  找到 %d 个备份:\n", len(backups))
				for _, backup := range backups {
					fmt.Printf("    - %s (%s, %s)\n", backup.name, backup.time, backup.size)
				}
				return nil
			},
		},
		{
			name: "获取元数据状态",
			operation: func() error {
				fmt.Println("  获取元数据状态...")
				time.Sleep(30 * time.Millisecond)

				fmt.Println("  元数据状态:")
				fmt.Println("    节点ID: miniodb-node-1")
				fmt.Println("    健康状态: healthy")
				fmt.Println("    上次备份: 2024-01-15 10:30:00")
				fmt.Println("    下次备份: 2024-01-16 10:30:00")
				return nil
			},
		},
	}

	for _, op := range backupOperations {
		fmt.Printf("\n执行: %s\n", op.name)
		if err := op.operation(); err != nil {
			fmt.Printf("操作失败: %v\n", err)
		} else {
			fmt.Printf("操作成功\n")
		}
	}

	// 维护任务示例
	fmt.Println("\n维护任务示例:")
	maintenanceTasks := []struct {
		name        string
		description string
		duration    time.Duration
	}{
		{"清理过期数据", "删除超过保留期的数据", 200 * time.Millisecond},
		{"优化索引", "重建和优化数据库索引", 500 * time.Millisecond},
		{"压缩数据文件", "压缩存储文件以节省空间", 300 * time.Millisecond},
		{"健康检查", "检查系统组件健康状态", 100 * time.Millisecond},
	}

	for _, task := range maintenanceTasks {
		fmt.Printf("\n执行维护任务: %s\n", task.name)
		fmt.Printf("描述: %s\n", task.description)

		start := time.Now()
		time.Sleep(task.duration) // 模拟任务执行
		elapsed := time.Since(start)

		fmt.Printf("任务完成，耗时: %v\n", elapsed)
	}
}
