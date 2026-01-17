// MinIODB Go SDK 示例
//
// 本示例展示如何通过 Redis Streams 或 Kafka 向 MinIODB 发布数据
// 适用于第三方系统集成，实现业务实时数据同步
//
// 使用方法:
//
//	go run main.go -mode redis    # 使用 Redis Streams
//	go run main.go -mode kafka    # 使用 Kafka
//	go run main.go -mode grpc     # 使用 gRPC 直连
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "minIODB/api/proto/miniodb/v1"
)

// ===============================================
// 数据流格式定义（与 MinIODB 保持一致）
// ===============================================

// DataRecord 数据记录
type DataRecord struct {
	ID        string                 `json:"id"`
	Timestamp int64                  `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// EventMetadata 事件元数据
type EventMetadata struct {
	Source       string            `json:"source,omitempty"`
	BatchID      string            `json:"batch_id,omitempty"`
	PartitionKey string            `json:"partition_key,omitempty"`
	Priority     string            `json:"priority,omitempty"`
	TraceID      string            `json:"trace_id,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
}

// DataEvent 数据事件
type DataEvent struct {
	EventID   string        `json:"event_id"`
	EventType string        `json:"event_type"` // insert, update, delete, batch
	Table     string        `json:"table"`
	Timestamp time.Time     `json:"timestamp"`
	Records   []DataRecord  `json:"records"`
	Metadata  EventMetadata `json:"metadata"`
}

// NewDataEvent 创建新的数据事件
func NewDataEvent(eventType, table string, records []DataRecord) *DataEvent {
	return &DataEvent{
		EventID:   fmt.Sprintf("evt_%s", uuid.New().String()[:12]),
		EventType: eventType,
		Table:     table,
		Timestamp: time.Now().UTC(),
		Records:   records,
		Metadata: EventMetadata{
			Priority: "normal",
		},
	}
}

// ToJSON 序列化为 JSON
func (e *DataEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ===============================================
// Redis Streams 客户端
// ===============================================

// RedisStreamClient Redis Streams 客户端
type RedisStreamClient struct {
	client       *redis.Client
	streamPrefix string
}

// NewRedisStreamClient 创建 Redis Streams 客户端
func NewRedisStreamClient(addr, password string, db int, streamPrefix string) (*RedisStreamClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStreamClient{
		client:       client,
		streamPrefix: streamPrefix,
	}, nil
}

// Publish 发布事件到 Redis Stream
func (c *RedisStreamClient) Publish(ctx context.Context, event *DataEvent) error {
	eventJSON, err := event.ToJSON()
	if err != nil {
		return err
	}

	streamKey := fmt.Sprintf("%s%s", c.streamPrefix, event.Table)
	values := map[string]interface{}{
		"event_id":   event.EventID,
		"event_type": event.EventType,
		"table":      event.Table,
		"timestamp":  event.Timestamp.Format(time.RFC3339Nano),
		"data":       string(eventJSON),
	}

	_, err = c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: values,
	}).Result()

	return err
}

// PublishBatch 批量发布事件
func (c *RedisStreamClient) PublishBatch(ctx context.Context, events []*DataEvent) error {
	pipe := c.client.Pipeline()

	for _, event := range events {
		eventJSON, err := event.ToJSON()
		if err != nil {
			continue
		}

		streamKey := fmt.Sprintf("%s%s", c.streamPrefix, event.Table)
		values := map[string]interface{}{
			"event_id":   event.EventID,
			"event_type": event.EventType,
			"table":      event.Table,
			"timestamp":  event.Timestamp.Format(time.RFC3339Nano),
			"data":       string(eventJSON),
		}

		pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			Values: values,
		})
	}

	_, err := pipe.Exec(ctx)
	return err
}

// Close 关闭连接
func (c *RedisStreamClient) Close() error {
	return c.client.Close()
}

// ===============================================
// Kafka 客户端
// ===============================================

// KafkaClient Kafka 客户端
type KafkaClient struct {
	writers     map[string]*kafka.Writer
	topicPrefix string
	brokers     []string
}

// NewKafkaClient 创建 Kafka 客户端
func NewKafkaClient(brokers []string, topicPrefix string) (*KafkaClient, error) {
	return &KafkaClient{
		writers:     make(map[string]*kafka.Writer),
		topicPrefix: topicPrefix,
		brokers:     brokers,
	}, nil
}

// getWriter 获取或创建 writer
func (c *KafkaClient) getWriter(topic string) *kafka.Writer {
	if w, ok := c.writers[topic]; ok {
		return w
	}

	w := &kafka.Writer{
		Addr:         kafka.TCP(c.brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
	}
	c.writers[topic] = w
	return w
}

// Publish 发布事件到 Kafka
func (c *KafkaClient) Publish(ctx context.Context, event *DataEvent) error {
	eventJSON, err := event.ToJSON()
	if err != nil {
		return err
	}

	topic := fmt.Sprintf("%s%s", c.topicPrefix, event.Table)
	writer := c.getWriter(topic)

	return writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.Table),
		Value: eventJSON,
		Headers: []kafka.Header{
			{Key: "event_id", Value: []byte(event.EventID)},
			{Key: "event_type", Value: []byte(event.EventType)},
		},
	})
}

// PublishBatch 批量发布事件
func (c *KafkaClient) PublishBatch(ctx context.Context, events []*DataEvent) error {
	// 按 topic 分组
	topicMessages := make(map[string][]kafka.Message)

	for _, event := range events {
		eventJSON, err := event.ToJSON()
		if err != nil {
			continue
		}

		topic := fmt.Sprintf("%s%s", c.topicPrefix, event.Table)
		msg := kafka.Message{
			Key:   []byte(event.Table),
			Value: eventJSON,
			Headers: []kafka.Header{
				{Key: "event_id", Value: []byte(event.EventID)},
				{Key: "event_type", Value: []byte(event.EventType)},
			},
		}
		topicMessages[topic] = append(topicMessages[topic], msg)
	}

	for topic, messages := range topicMessages {
		writer := c.getWriter(topic)
		if err := writer.WriteMessages(ctx, messages...); err != nil {
			return err
		}
	}

	return nil
}

// Close 关闭连接
func (c *KafkaClient) Close() error {
	for _, w := range c.writers {
		w.Close()
	}
	return nil
}

// ===============================================
// gRPC 客户端
// ===============================================

// GRPCClient gRPC 客户端
type GRPCClient struct {
	conn   *grpc.ClientConn
	client pb.MinIODBServiceClient
}

// NewGRPCClient 创建 gRPC 客户端
func NewGRPCClient(addr string) (*GRPCClient, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	return &GRPCClient{
		conn:   conn,
		client: pb.NewMinIODBServiceClient(conn),
	}, nil
}

// WriteData 写入数据
func (c *GRPCClient) WriteData(ctx context.Context, table string, record DataRecord) error {
	// 转换 payload 为 protobuf Struct
	payloadStruct, err := structpb.NewStruct(record.Payload)
	if err != nil {
		return fmt.Errorf("failed to convert payload: %w", err)
	}

	req := &pb.WriteDataRequest{
		Table: table,
		Data: &pb.DataRecord{
			Id:        record.ID,
			Timestamp: timestamppb.New(time.UnixMilli(record.Timestamp)),
			Payload:   payloadStruct,
		},
	}

	resp, err := c.client.WriteData(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("write failed: %s", resp.Message)
	}

	return nil
}

// Close 关闭连接
func (c *GRPCClient) Close() error {
	return c.conn.Close()
}

// ===============================================
// 主程序
// ===============================================

func main() {
	// 命令行参数
	mode := flag.String("mode", "redis", "Client mode: redis, kafka, or grpc")
	redisAddr := flag.String("redis-addr", "localhost:6379", "Redis address")
	redisPassword := flag.String("redis-password", "", "Redis password")
	kafkaBrokers := flag.String("kafka-brokers", "localhost:9092", "Kafka brokers (comma-separated)")
	grpcAddr := flag.String("grpc-addr", "localhost:8080", "gRPC server address")
	table := flag.String("table", "user_events", "Target table name")
	count := flag.Int("count", 10, "Number of events to send")
	batch := flag.Bool("batch", false, "Use batch mode")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		cancel()
	}()

	switch *mode {
	case "redis":
		runRedisExample(ctx, *redisAddr, *redisPassword, *table, *count, *batch)
	case "kafka":
		runKafkaExample(ctx, *kafkaBrokers, *table, *count, *batch)
	case "grpc":
		runGRPCExample(ctx, *grpcAddr, *table, *count)
	default:
		log.Fatalf("Unknown mode: %s", *mode)
	}
}

func runRedisExample(ctx context.Context, addr, password, table string, count int, batch bool) {
	log.Printf("Connecting to Redis at %s...", addr)

	client, err := NewRedisStreamClient(addr, password, 0, "miniodb:stream:")
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	log.Printf("Connected to Redis. Publishing %d events to table '%s'...", count, table)

	if batch {
		// 批量发送
		events := generateEvents(table, count)
		start := time.Now()
		if err := client.PublishBatch(ctx, events); err != nil {
			log.Fatalf("Failed to publish batch: %v", err)
		}
		log.Printf("Published %d events in batch mode in %v", count, time.Since(start))
	} else {
		// 逐条发送
		for i := 0; i < count; i++ {
			event := generateEvent(table, i)
			if err := client.Publish(ctx, event); err != nil {
				log.Printf("Failed to publish event %d: %v", i, err)
				continue
			}
			log.Printf("Published event %d: %s", i+1, event.EventID)
		}
	}

	log.Println("Done!")
}

func runKafkaExample(ctx context.Context, brokers, table string, count int, batch bool) {
	log.Printf("Connecting to Kafka at %s...", brokers)

	client, err := NewKafkaClient([]string{brokers}, "miniodb-")
	if err != nil {
		log.Fatalf("Failed to create Kafka client: %v", err)
	}
	defer client.Close()

	log.Printf("Connected to Kafka. Publishing %d events to table '%s'...", count, table)

	if batch {
		// 批量发送
		events := generateEvents(table, count)
		start := time.Now()
		if err := client.PublishBatch(ctx, events); err != nil {
			log.Fatalf("Failed to publish batch: %v", err)
		}
		log.Printf("Published %d events in batch mode in %v", count, time.Since(start))
	} else {
		// 逐条发送
		for i := 0; i < count; i++ {
			event := generateEvent(table, i)
			if err := client.Publish(ctx, event); err != nil {
				log.Printf("Failed to publish event %d: %v", i, err)
				continue
			}
			log.Printf("Published event %d: %s", i+1, event.EventID)
		}
	}

	log.Println("Done!")
}

func runGRPCExample(ctx context.Context, addr, table string, count int) {
	log.Printf("Connecting to gRPC server at %s...", addr)

	client, err := NewGRPCClient(addr)
	if err != nil {
		log.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer client.Close()

	log.Printf("Connected to gRPC server. Writing %d records to table '%s'...", count, table)

	for i := 0; i < count; i++ {
		record := DataRecord{
			ID:        fmt.Sprintf("record_%d_%d", time.Now().UnixNano(), i),
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]interface{}{
				"user_id":   i + 1,
				"action":    "click",
				"page":      fmt.Sprintf("/page/%d", i%10),
				"timestamp": time.Now().Format(time.RFC3339),
			},
		}

		if err := client.WriteData(ctx, table, record); err != nil {
			log.Printf("Failed to write record %d: %v", i, err)
			continue
		}
		log.Printf("Written record %d: %s", i+1, record.ID)
	}

	log.Println("Done!")
}

func generateEvent(table string, index int) *DataEvent {
	record := DataRecord{
		ID:        fmt.Sprintf("record_%d_%d", time.Now().UnixNano(), index),
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]interface{}{
			"user_id":   index + 1,
			"action":    "click",
			"page":      fmt.Sprintf("/page/%d", index%10),
			"timestamp": time.Now().Format(time.RFC3339),
		},
	}

	event := NewDataEvent("insert", table, []DataRecord{record})
	event.Metadata.Source = "go-sdk-example"
	return event
}

func generateEvents(table string, count int) []*DataEvent {
	events := make([]*DataEvent, count)
	for i := 0; i < count; i++ {
		events[i] = generateEvent(table, i)
	}
	return events
}
