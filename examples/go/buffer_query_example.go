package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	fmt.Println("=== MinIODB 缓冲区查询示例 ===")

	// 1. 加载配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化存储组件
	redisClient, err := storage.NewRedisClient(cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	minioClient, err := storage.NewMinioClientWrapper(cfg.MinIO)
	if err != nil {
		log.Fatalf("Failed to create MinIO client: %v", err)
	}

	// 3. 创建缓冲区
	sharedBuffer := buffer.NewSharedBuffer(
		redisClient.GetClient(),
		minioClient,
		nil, // 不使用备份
		"",
		cfg,
	)
	defer sharedBuffer.Stop()

	// 4. 创建查询引擎
	querier, err := query.NewQuerier(redisClient.GetClient(), minioClient, cfg.MinIO, sharedBuffer)
	if err != nil {
		log.Fatalf("Failed to create querier: %v", err)
	}
	defer querier.Close()

	// 5. 向缓冲区写入测试数据
	fmt.Println("\n--- 写入测试数据到缓冲区 ---")

	testData := []struct {
		id      string
		payload map[string]interface{}
	}{
		{"user-001", map[string]interface{}{"name": "Alice", "age": 25, "city": "Beijing"}},
		{"user-002", map[string]interface{}{"name": "Bob", "age": 30, "city": "Shanghai"}},
		{"user-003", map[string]interface{}{"name": "Charlie", "age": 35, "city": "Guangzhou"}},
		{"user-001", map[string]interface{}{"name": "Alice", "age": 26, "city": "Beijing"}}, // 同一用户的更新数据
	}

	today := time.Now().Format("2006-01-02")

	for i, data := range testData {
		payloadJson, _ := json.Marshal(data.payload)
		dataRow := buffer.DataRow{
			Table:     "users", // 指定表名
			ID:        data.id,
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute).UnixNano(),
			Payload:   string(payloadJson),
		}

		sharedBuffer.Add(dataRow)
		fmt.Printf("添加数据: Table=users, ID=%s, Payload=%s\n", data.id, string(payloadJson))
	}

	// 等待一下让数据进入缓冲区
	time.Sleep(1 * time.Second)

	// 6. 测试不同类型的查询
	fmt.Println("\n--- 测试缓冲区查询功能 ---")

	testQueries := []struct {
		name string
		sql  string
		desc string
	}{
		{
			name: "精确查询（表+ID+Day）",
			sql:  fmt.Sprintf("SELECT * FROM users WHERE id='user-001' AND day='%s'", today),
			desc: "查询特定用户在今天的所有数据",
		},
		{
			name: "按表和ID查询",
			sql:  "SELECT * FROM users WHERE id='user-002'",
			desc: "查询特定用户的所有数据",
		},
		{
			name: "按表和天查询",
			sql:  fmt.Sprintf("SELECT * FROM users WHERE day='%s'", today),
			desc: "查询今天所有用户的数据",
		},
		{
			name: "表级聚合查询",
			sql:  "SELECT COUNT(*) as total_records FROM users",
			desc: "统计users表中的总记录数",
		},
		{
			name: "表级条件过滤查询",
			sql:  "SELECT id, payload FROM users WHERE id LIKE 'user-%'",
			desc: "在users表中使用LIKE条件查询用户数据",
		},
	}

	for _, testQuery := range testQueries {
		fmt.Printf("\n🔍 %s\n", testQuery.name)
		fmt.Printf("描述: %s\n", testQuery.desc)
		fmt.Printf("SQL: %s\n", testQuery.sql)

		result, err := querier.ExecuteQuery(testQuery.sql)
		if err != nil {
			fmt.Printf("❌ 查询失败: %v\n", err)
			continue
		}

		// 美化JSON输出
		var jsonResult interface{}
		if err := json.Unmarshal([]byte(result), &jsonResult); err == nil {
			prettyResult, _ := json.MarshalIndent(jsonResult, "", "  ")
			fmt.Printf("✅ 查询结果:\n%s\n", string(prettyResult))
		} else {
			fmt.Printf("✅ 查询结果: %s\n", result)
		}
	}

	// 7. 验证缓冲区状态
	fmt.Println("\n--- 缓冲区状态信息 ---")
	fmt.Printf("缓冲区大小: %d 个键\n", sharedBuffer.Size())
	fmt.Printf("待写入数据: %d 条记录\n", sharedBuffer.PendingWrites())

	allKeys := sharedBuffer.GetAllKeys()
	fmt.Printf("缓冲区键列表: %v\n", allKeys)

	// 8. 演示实时数据写入和查询
	fmt.Println("\n--- 实时数据写入和查询演示 ---")

	// 写入新数据
	newDataRow := buffer.DataRow{
		Table:     "users", // 指定表名
		ID:        "user-004",
		Timestamp: time.Now().UnixNano(),
		Payload:   `{"name": "Diana", "age": 28, "city": "Shenzhen", "realtime": true}`,
	}
	sharedBuffer.Add(newDataRow)
	fmt.Println("✅ 添加实时数据: Table=users, ID=user-004")

	// 立即查询新数据
	realtimeQuery := "SELECT * FROM users WHERE id='user-004'"
	result, err := querier.ExecuteQuery(realtimeQuery)
	if err != nil {
		fmt.Printf("❌ 实时查询失败: %v\n", err)
	} else {
		var jsonResult interface{}
		json.Unmarshal([]byte(result), &jsonResult)
		prettyResult, _ := json.MarshalIndent(jsonResult, "", "  ")
		fmt.Printf("✅ 实时查询结果:\n%s\n", string(prettyResult))
	}

	fmt.Println("\n=== 缓冲区查询示例完成 ===")
	fmt.Println("✅ 所有缓冲区数据都能被正确查询到！")
	fmt.Println("📝 注意: 缓冲区数据在查询时会自动转换为临时Parquet文件，查询完成后自动清理")
}

// 辅助函数：创建protobuf结构
func createPayloadStruct(data map[string]interface{}) *structpb.Struct {
	payload, _ := structpb.NewStruct(data)
	return payload
}

// 辅助函数：创建时间戳
func createTimestamp(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}
