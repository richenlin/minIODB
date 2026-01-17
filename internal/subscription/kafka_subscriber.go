package subscription

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"minIODB/config"
	"minIODB/pkg/logger"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
	"go.uber.org/zap"
)

// KafkaSubscriber Kafka 订阅者实现
type KafkaSubscriber struct {
	*BaseSubscriber
	cfg       *config.KafkaSubscriptionConfig
	writers   map[string]*kafka.Writer // topic -> writer
	readers   map[string]*kafka.Reader // topic -> reader
	handlers  map[string]EventHandler  // table -> handler
	writerMu  sync.RWMutex
	readerMu  sync.RWMutex
	handlerMu sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// TLS 配置
	tlsConfig *tls.Config
	// SASL 配置
	saslMechanism sasl.Mechanism

	// 统计
	publishedCount int64
	consumedCount  int64
	failedCount    int64
}

// NewKafkaSubscriber 创建 Kafka 订阅者
func NewKafkaSubscriber(cfg *config.KafkaSubscriptionConfig) (*KafkaSubscriber, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("at least one broker is required")
	}

	s := &KafkaSubscriber{
		BaseSubscriber: NewBaseSubscriber(SubscriberTypeKafka),
		cfg:            cfg,
		writers:        make(map[string]*kafka.Writer),
		readers:        make(map[string]*kafka.Reader),
		handlers:       make(map[string]EventHandler),
	}

	// 配置 TLS
	if cfg.UseTLS {
		tlsConfig, err := s.createTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		s.tlsConfig = tlsConfig
	}

	// 配置 SASL
	if cfg.UseSASL {
		mechanism, err := s.createSASLMechanism()
		if err != nil {
			return nil, fmt.Errorf("failed to create SASL mechanism: %w", err)
		}
		s.saslMechanism = mechanism
	}

	return s, nil
}

// createTLSConfig 创建 TLS 配置
func (s *KafkaSubscriber) createTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// 加载客户端证书（如果配置）
	if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.CertFile, s.cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// 加载 CA 证书（如果配置）
	if s.cfg.CAFile != "" {
		caCert, err := os.ReadFile(s.cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

// createSASLMechanism 创建 SASL 认证机制
func (s *KafkaSubscriber) createSASLMechanism() (sasl.Mechanism, error) {
	switch s.cfg.SASLMechanism {
	case "plain", "PLAIN":
		return plain.Mechanism{
			Username: s.cfg.SASLUsername,
			Password: s.cfg.SASLPassword,
		}, nil
	case "scram-sha-256", "SCRAM-SHA-256":
		mechanism, err := scram.Mechanism(scram.SHA256, s.cfg.SASLUsername, s.cfg.SASLPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-256 mechanism: %w", err)
		}
		return mechanism, nil
	case "scram-sha-512", "SCRAM-SHA-512":
		mechanism, err := scram.Mechanism(scram.SHA512, s.cfg.SASLUsername, s.cfg.SASLPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-512 mechanism: %w", err)
		}
		return mechanism, nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", s.cfg.SASLMechanism)
	}
}

// Start 启动订阅
func (s *KafkaSubscriber) Start(ctx context.Context) error {
	if s.Status() == StatusRunning {
		return ErrSubscriberAlreadyRunning
	}

	s.SetStatus(StatusStarting)
	s.ctx, s.cancel = context.WithCancel(ctx)

	// 验证 Kafka 连接
	dialer := s.createDialer()
	conn, err := dialer.DialContext(ctx, "tcp", s.cfg.Brokers[0])
	if err != nil {
		s.SetStatus(StatusError)
		return fmt.Errorf("failed to connect to Kafka: %w", err)
	}
	conn.Close()

	s.SetStatus(StatusRunning)
	logger.GetLogger().Info("Kafka subscriber started",
		zap.Strings("brokers", s.cfg.Brokers),
		zap.String("consumer_group", s.cfg.ConsumerGroup))

	return nil
}

// createDialer 创建 Kafka dialer
func (s *KafkaSubscriber) createDialer() *kafka.Dialer {
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	if s.tlsConfig != nil {
		dialer.TLS = s.tlsConfig
	}

	if s.saslMechanism != nil {
		dialer.SASLMechanism = s.saslMechanism
	}

	return dialer
}

// Stop 停止订阅
func (s *KafkaSubscriber) Stop(ctx context.Context) error {
	if s.Status() != StatusRunning {
		return nil
	}

	s.SetStatus(StatusStopping)

	if s.cancel != nil {
		s.cancel()
	}

	// 关闭所有 writers
	s.writerMu.Lock()
	for topic, writer := range s.writers {
		if err := writer.Close(); err != nil {
			logger.GetLogger().Warn("Failed to close Kafka writer",
				zap.String("topic", topic),
				zap.Error(err))
		}
	}
	s.writers = make(map[string]*kafka.Writer)
	s.writerMu.Unlock()

	// 关闭所有 readers
	s.readerMu.Lock()
	for topic, reader := range s.readers {
		if err := reader.Close(); err != nil {
			logger.GetLogger().Warn("Failed to close Kafka reader",
				zap.String("topic", topic),
				zap.Error(err))
		}
	}
	s.readers = make(map[string]*kafka.Reader)
	s.readerMu.Unlock()

	// 等待所有 goroutine 完成
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		logger.GetLogger().Warn("Kafka subscriber stop timeout")
	}

	s.SetStatus(StatusStopped)
	logger.GetLogger().Info("Kafka subscriber stopped")
	return nil
}

// getOrCreateWriter 获取或创建 writer
func (s *KafkaSubscriber) getOrCreateWriter(topic string) *kafka.Writer {
	s.writerMu.RLock()
	writer, exists := s.writers[topic]
	s.writerMu.RUnlock()

	if exists {
		return writer
	}

	s.writerMu.Lock()
	defer s.writerMu.Unlock()

	// 双重检查
	if writer, exists = s.writers[topic]; exists {
		return writer
	}

	// 创建新的 writer
	writerConfig := kafka.WriterConfig{
		Brokers:      s.cfg.Brokers,
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    s.cfg.BatchSize,
		BatchTimeout: s.cfg.BatchTimeout,
		MaxAttempts:  s.cfg.MaxRetries,
		Async:        false, // 同步写入
	}

	if s.tlsConfig != nil || s.saslMechanism != nil {
		transport := &kafka.Transport{}
		if s.tlsConfig != nil {
			transport.TLS = s.tlsConfig
		}
		if s.saslMechanism != nil {
			transport.SASL = s.saslMechanism
		}
		writer = &kafka.Writer{
			Addr:         kafka.TCP(s.cfg.Brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			BatchSize:    s.cfg.BatchSize,
			BatchTimeout: s.cfg.BatchTimeout,
			MaxAttempts:  s.cfg.MaxRetries,
			Transport:    transport,
		}
	} else {
		writer = kafka.NewWriter(writerConfig)
	}

	s.writers[topic] = writer
	return writer
}

// Publish 发布事件到 Kafka
func (s *KafkaSubscriber) Publish(ctx context.Context, event *DataEvent) error {
	if s.Status() != StatusRunning {
		return ErrSubscriberNotRunning
	}

	if err := event.Validate(); err != nil {
		return &ErrPublishFailed{Event: event, Cause: err}
	}

	// 序列化事件
	eventJSON, err := event.ToJSON()
	if err != nil {
		return &ErrPublishFailed{Event: event, Cause: err}
	}

	// 获取 topic
	topic := event.KafkaTopic(s.cfg.TopicPrefix)
	writer := s.getOrCreateWriter(topic)

	// 构建消息
	msg := kafka.Message{
		Key:   []byte(event.GetPartitionKey()),
		Value: eventJSON,
		Headers: []kafka.Header{
			{Key: "event_id", Value: []byte(event.EventID)},
			{Key: "event_type", Value: []byte(event.EventType)},
			{Key: "table", Value: []byte(event.Table)},
			{Key: "timestamp", Value: []byte(event.Timestamp.Format(time.RFC3339Nano))},
		},
	}

	// 发送消息
	err = writer.WriteMessages(ctx, msg)
	if err != nil {
		atomic.AddInt64(&s.failedCount, 1)
		s.IncrFailed(1)
		return &ErrPublishFailed{Event: event, Cause: err}
	}

	atomic.AddInt64(&s.publishedCount, 1)
	s.IncrPublished(1)

	logger.GetLogger().Debug("Published event to Kafka",
		zap.String("topic", topic),
		zap.String("event_id", event.EventID))

	return nil
}

// PublishBatch 批量发布事件
func (s *KafkaSubscriber) PublishBatch(ctx context.Context, events []*DataEvent) error {
	if s.Status() != StatusRunning {
		return ErrSubscriberNotRunning
	}

	if len(events) == 0 {
		return nil
	}

	// 按 topic 分组
	topicMessages := make(map[string][]kafka.Message)

	for _, event := range events {
		if err := event.Validate(); err != nil {
			logger.GetLogger().Warn("Skip invalid event",
				zap.String("event_id", event.EventID),
				zap.Error(err))
			continue
		}

		eventJSON, err := event.ToJSON()
		if err != nil {
			logger.GetLogger().Warn("Failed to serialize event",
				zap.String("event_id", event.EventID),
				zap.Error(err))
			continue
		}

		topic := event.KafkaTopic(s.cfg.TopicPrefix)
		msg := kafka.Message{
			Key:   []byte(event.GetPartitionKey()),
			Value: eventJSON,
			Headers: []kafka.Header{
				{Key: "event_id", Value: []byte(event.EventID)},
				{Key: "event_type", Value: []byte(event.EventType)},
				{Key: "table", Value: []byte(event.Table)},
				{Key: "timestamp", Value: []byte(event.Timestamp.Format(time.RFC3339Nano))},
			},
		}

		topicMessages[topic] = append(topicMessages[topic], msg)
	}

	// 发送到各个 topic
	var totalSent int64
	for topic, messages := range topicMessages {
		writer := s.getOrCreateWriter(topic)
		if err := writer.WriteMessages(ctx, messages...); err != nil {
			atomic.AddInt64(&s.failedCount, int64(len(messages)))
			s.IncrFailed(int64(len(messages)))
			logger.GetLogger().Error("Failed to write batch to Kafka",
				zap.String("topic", topic),
				zap.Error(err))
			continue
		}
		totalSent += int64(len(messages))
	}

	atomic.AddInt64(&s.publishedCount, totalSent)
	s.IncrPublished(totalSent)

	logger.GetLogger().Debug("Published batch events to Kafka",
		zap.Int64("count", totalSent))

	return nil
}

// Subscribe 订阅表的事件
func (s *KafkaSubscriber) Subscribe(ctx context.Context, tables []string, handler EventHandler) error {
	if s.Status() != StatusRunning {
		return ErrSubscriberNotRunning
	}

	// 注册 handler
	s.handlerMu.Lock()
	for _, table := range tables {
		s.handlers[table] = handler
		s.AddSubscribedTable(table)
	}
	s.handlerMu.Unlock()

	// 为每个表创建 reader
	for _, table := range tables {
		topic := fmt.Sprintf("%s%s", s.cfg.TopicPrefix, table)

		if err := s.createReader(topic, table); err != nil {
			logger.GetLogger().Error("Failed to create reader",
				zap.String("topic", topic),
				zap.Error(err))
			continue
		}

		// 启动消费 goroutine
		s.wg.Add(1)
		go s.consumeLoop(topic, table)
	}

	logger.GetLogger().Info("Subscribed to tables",
		zap.Strings("tables", tables),
		zap.String("consumer_group", s.cfg.ConsumerGroup))

	return nil
}

// createReader 创建 reader
func (s *KafkaSubscriber) createReader(topic, table string) error {
	s.readerMu.Lock()
	defer s.readerMu.Unlock()

	if _, exists := s.readers[topic]; exists {
		return nil
	}

	// 确定起始 offset
	startOffset := kafka.LastOffset
	if s.cfg.StartOffset == "earliest" {
		startOffset = kafka.FirstOffset
	}

	readerConfig := kafka.ReaderConfig{
		Brokers:        s.cfg.Brokers,
		GroupID:        s.cfg.ConsumerGroup,
		Topic:          topic,
		MinBytes:       1,
		MaxBytes:       10e6, // 10MB
		CommitInterval: s.cfg.CommitInterval,
		StartOffset:    startOffset,
		MaxWait:        s.cfg.BatchTimeout,
	}

	if s.tlsConfig != nil || s.saslMechanism != nil {
		dialer := s.createDialer()
		readerConfig.Dialer = dialer
	}

	reader := kafka.NewReader(readerConfig)
	s.readers[topic] = reader

	return nil
}

// consumeLoop 消费循环
func (s *KafkaSubscriber) consumeLoop(topic, table string) {
	defer s.wg.Done()

	s.readerMu.RLock()
	reader := s.readers[topic]
	s.readerMu.RUnlock()

	if reader == nil {
		logger.GetLogger().Error("Reader not found",
			zap.String("topic", topic))
		return
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// 读取消息
		msg, err := reader.FetchMessage(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			logger.GetLogger().Error("Failed to fetch message",
				zap.String("topic", topic),
				zap.Error(err))
			time.Sleep(time.Second)
			continue
		}

		// 获取 handler
		s.handlerMu.RLock()
		handler := s.handlers[table]
		s.handlerMu.RUnlock()

		if handler == nil {
			logger.GetLogger().Warn("No handler for table",
				zap.String("table", table))
			// 提交 offset 以跳过消息
			if s.cfg.AutoCommit {
				reader.CommitMessages(s.ctx, msg)
			}
			continue
		}

		// 处理消息
		if err := s.processMessage(s.ctx, reader, msg, handler); err != nil {
			logger.GetLogger().Error("Failed to process message",
				zap.String("topic", topic),
				zap.Int64("offset", msg.Offset),
				zap.Error(err))
		}
	}
}

// processMessage 处理单条消息
func (s *KafkaSubscriber) processMessage(ctx context.Context, reader *kafka.Reader, msg kafka.Message, handler EventHandler) error {
	// 解析消息
	event, err := FromJSON(msg.Value)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// 重试逻辑
	var lastErr error
	for i := 0; i <= s.cfg.MaxRetries; i++ {
		if i > 0 {
			time.Sleep(s.cfg.RetryBackoff * time.Duration(i))
			event.Metadata.RetryCount = i
		}

		if err := handler(ctx, event); err != nil {
			lastErr = err
			logger.GetLogger().Warn("Handler failed, retrying",
				zap.String("event_id", event.EventID),
				zap.Int("attempt", i+1),
				zap.Error(err))
			continue
		}

		// 处理成功
		atomic.AddInt64(&s.consumedCount, 1)
		s.IncrConsumed(1)

		// 提交 offset
		if s.cfg.AutoCommit {
			if err := reader.CommitMessages(ctx, msg); err != nil {
				logger.GetLogger().Warn("Failed to commit offset",
					zap.Int64("offset", msg.Offset),
					zap.Error(err))
			}
		}

		return nil
	}

	// 所有重试都失败
	atomic.AddInt64(&s.failedCount, 1)
	s.IncrFailed(1)
	s.HandleError(event, lastErr)

	// 即使处理失败也提交 offset（避免消息积压）
	if s.cfg.AutoCommit {
		reader.CommitMessages(ctx, msg)
	}

	return &ErrConsumeFailed{Event: event, Cause: lastErr}
}

// Unsubscribe 取消订阅
func (s *KafkaSubscriber) Unsubscribe(ctx context.Context, tables []string) error {
	s.handlerMu.Lock()
	for _, table := range tables {
		delete(s.handlers, table)
		s.RemoveSubscribedTable(table)
	}
	s.handlerMu.Unlock()

	// 关闭对应的 readers
	s.readerMu.Lock()
	for _, table := range tables {
		topic := fmt.Sprintf("%s%s", s.cfg.TopicPrefix, table)
		if reader, exists := s.readers[topic]; exists {
			reader.Close()
			delete(s.readers, topic)
		}
	}
	s.readerMu.Unlock()

	logger.GetLogger().Info("Unsubscribed from tables",
		zap.Strings("tables", tables))

	return nil
}

// Stats 获取统计信息
func (s *KafkaSubscriber) Stats() *SubscriberStats {
	stats := s.BaseSubscriber.Stats()
	stats.PublishedEvents = atomic.LoadInt64(&s.publishedCount)
	stats.ConsumedEvents = atomic.LoadInt64(&s.consumedCount)
	stats.FailedEvents = atomic.LoadInt64(&s.failedCount)
	return stats
}

// CreateTopic 创建 topic（管理功能）
func (s *KafkaSubscriber) CreateTopic(ctx context.Context, topic string, numPartitions int, replicationFactor int) error {
	dialer := s.createDialer()
	conn, err := dialer.DialContext(ctx, "tcp", s.cfg.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to Kafka: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topic,
			NumPartitions:     numPartitions,
			ReplicationFactor: replicationFactor,
		},
	}

	return controllerConn.CreateTopics(topicConfigs...)
}

// GetTopicMetadata 获取 topic 元数据
func (s *KafkaSubscriber) GetTopicMetadata(ctx context.Context, topic string) (*kafka.Partition, error) {
	dialer := s.createDialer()
	conn, err := dialer.DialContext(ctx, "tcp", s.cfg.Brokers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions(topic)
	if err != nil {
		return nil, fmt.Errorf("failed to read partitions: %w", err)
	}

	if len(partitions) == 0 {
		return nil, fmt.Errorf("no partitions found for topic %s", topic)
	}

	return &partitions[0], nil
}
