package subscription

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"minIODB/config"
	"minIODB/pkg/logger"
	"minIODB/pkg/pool"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// RedisSubscriber Redis Streams 订阅者实现
type RedisSubscriber struct {
	*BaseSubscriber
	redisPool    *pool.RedisPool
	cfg          *config.RedisSubscriptionConfig
	handlers     map[string]EventHandler // table -> handler
	handlerMu    sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	consumerName string

	// 统计
	publishedCount int64
	consumedCount  int64
	failedCount    int64
}

// NewRedisSubscriber 创建 Redis Streams 订阅者
func NewRedisSubscriber(redisPool *pool.RedisPool, cfg *config.RedisSubscriptionConfig) (*RedisSubscriber, error) {
	if redisPool == nil {
		return nil, fmt.Errorf("redis pool is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	// 生成消费者名称
	consumerName := cfg.ConsumerName
	if consumerName == "" {
		hostname, _ := os.Hostname()
		consumerName = fmt.Sprintf("consumer-%s-%d", hostname, time.Now().UnixNano()%10000)
	}

	return &RedisSubscriber{
		BaseSubscriber: NewBaseSubscriber(SubscriberTypeRedis),
		redisPool:      redisPool,
		cfg:            cfg,
		handlers:       make(map[string]EventHandler),
		consumerName:   consumerName,
	}, nil
}

// Start 启动订阅
func (s *RedisSubscriber) Start(ctx context.Context) error {
	if s.Status() == StatusRunning {
		return ErrSubscriberAlreadyRunning
	}

	s.SetStatus(StatusStarting)
	s.ctx, s.cancel = context.WithCancel(ctx)

	// 验证 Redis 连接
	client := s.redisPool.GetClient()
	if client == nil {
		s.SetStatus(StatusError)
		return fmt.Errorf("failed to get Redis client")
	}

	if err := client.Ping(s.ctx).Err(); err != nil {
		s.SetStatus(StatusError)
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	s.SetStatus(StatusRunning)
	logger.GetLogger().Info("Redis subscriber started",
		zap.String("consumer_name", s.consumerName),
		zap.String("consumer_group", s.cfg.ConsumerGroup))

	return nil
}

// Stop 停止订阅
func (s *RedisSubscriber) Stop(ctx context.Context) error {
	if s.Status() != StatusRunning {
		return nil
	}

	s.SetStatus(StatusStopping)

	if s.cancel != nil {
		s.cancel()
	}

	// 等待所有 goroutine 完成
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 正常完成
	case <-ctx.Done():
		logger.GetLogger().Warn("Redis subscriber stop timeout")
	}

	s.SetStatus(StatusStopped)
	logger.GetLogger().Info("Redis subscriber stopped")
	return nil
}

// Publish 发布事件到 Redis Stream
func (s *RedisSubscriber) Publish(ctx context.Context, event *DataEvent) error {
	if s.Status() != StatusRunning {
		return ErrSubscriberNotRunning
	}

	if err := event.Validate(); err != nil {
		return &ErrPublishFailed{Event: event, Cause: err}
	}

	client := s.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("failed to get Redis client")
	}

	// 序列化事件
	eventJSON, err := event.ToJSON()
	if err != nil {
		return &ErrPublishFailed{Event: event, Cause: err}
	}

	// 构建 Stream key
	streamKey := event.StreamKey(s.cfg.StreamPrefix)

	// 构建消息字段
	values := map[string]interface{}{
		"event_id":   event.EventID,
		"event_type": string(event.EventType),
		"table":      event.Table,
		"timestamp":  event.Timestamp.Format(time.RFC3339Nano),
		"data":       string(eventJSON),
	}

	// 添加到 Stream
	args := &redis.XAddArgs{
		Stream: streamKey,
		Values: values,
	}

	// 如果配置了最大长度，使用 MAXLEN 参数
	if s.cfg.MaxLen > 0 {
		args.MaxLen = s.cfg.MaxLen
		args.Approx = true // 使用近似值以提高性能
	}

	msgID, err := client.XAdd(ctx, args).Result()
	if err != nil {
		atomic.AddInt64(&s.failedCount, 1)
		s.IncrFailed(1)
		return &ErrPublishFailed{Event: event, Cause: err}
	}

	atomic.AddInt64(&s.publishedCount, 1)
	s.IncrPublished(1)

	logger.GetLogger().Debug("Published event to Redis Stream",
		zap.String("stream", streamKey),
		zap.String("msg_id", msgID),
		zap.String("event_id", event.EventID))

	return nil
}

// PublishBatch 批量发布事件
func (s *RedisSubscriber) PublishBatch(ctx context.Context, events []*DataEvent) error {
	if s.Status() != StatusRunning {
		return ErrSubscriberNotRunning
	}

	if len(events) == 0 {
		return nil
	}

	client := s.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("failed to get Redis client")
	}

	// 使用 Pipeline 批量发送
	pipe := client.Pipeline()

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

		streamKey := event.StreamKey(s.cfg.StreamPrefix)
		values := map[string]interface{}{
			"event_id":   event.EventID,
			"event_type": string(event.EventType),
			"table":      event.Table,
			"timestamp":  event.Timestamp.Format(time.RFC3339Nano),
			"data":       string(eventJSON),
		}

		args := &redis.XAddArgs{
			Stream: streamKey,
			Values: values,
		}
		if s.cfg.MaxLen > 0 {
			args.MaxLen = s.cfg.MaxLen
			args.Approx = true
		}

		pipe.XAdd(ctx, args)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		atomic.AddInt64(&s.failedCount, int64(len(events)))
		s.IncrFailed(int64(len(events)))
		return fmt.Errorf("failed to execute pipeline: %w", err)
	}

	atomic.AddInt64(&s.publishedCount, int64(len(events)))
	s.IncrPublished(int64(len(events)))

	logger.GetLogger().Debug("Published batch events to Redis Stream",
		zap.Int("count", len(events)))

	return nil
}

// Subscribe 订阅表的事件
func (s *RedisSubscriber) Subscribe(ctx context.Context, tables []string, handler EventHandler) error {
	if s.Status() != StatusRunning {
		return ErrSubscriberNotRunning
	}

	client := s.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("failed to get Redis client")
	}

	// 注册 handler
	s.handlerMu.Lock()
	for _, table := range tables {
		s.handlers[table] = handler
		s.AddSubscribedTable(table)
	}
	s.handlerMu.Unlock()

	// 为每个表创建消费者组（如果不存在）
	for _, table := range tables {
		streamKey := fmt.Sprintf("%s%s", s.cfg.StreamPrefix, table)

		// 创建 Consumer Group（从最新消息开始）
		err := client.XGroupCreateMkStream(ctx, streamKey, s.cfg.ConsumerGroup, "$").Err()
		if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
			logger.GetLogger().Warn("Failed to create consumer group",
				zap.String("stream", streamKey),
				zap.String("group", s.cfg.ConsumerGroup),
				zap.Error(err))
		}
	}

	// 启动消费 goroutine
	s.wg.Add(1)
	go s.consumeLoop(tables)

	logger.GetLogger().Info("Subscribed to tables",
		zap.Strings("tables", tables),
		zap.String("consumer_group", s.cfg.ConsumerGroup))

	return nil
}

// consumeLoop 消费循环
func (s *RedisSubscriber) consumeLoop(tables []string) {
	defer s.wg.Done()

	client := s.redisPool.GetClient()
	if client == nil {
		logger.GetLogger().Error("Redis client not available in consume loop")
		return
	}

	// 构建 streams 参数
	streams := make([]string, 0, len(tables)*2)
	for _, table := range tables {
		streamKey := fmt.Sprintf("%s%s", s.cfg.StreamPrefix, table)
		streams = append(streams, streamKey)
	}
	// 添加 ">" 表示只读取新消息
	for range tables {
		streams = append(streams, ">")
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// 使用 XREADGROUP 读取消息
		result, err := client.XReadGroup(s.ctx, &redis.XReadGroupArgs{
			Group:    s.cfg.ConsumerGroup,
			Consumer: s.consumerName,
			Streams:  streams,
			Count:    int64(s.cfg.BatchSize),
			Block:    s.cfg.BlockTimeout,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				// 没有新消息，继续
				continue
			}
			if s.ctx.Err() != nil {
				// context 被取消
				return
			}
			logger.GetLogger().Error("Failed to read from stream",
				zap.Error(err))
			time.Sleep(time.Second) // 错误后等待一下
			continue
		}

		// 处理消息
		for _, stream := range result {
			table := s.extractTableFromStreamKey(stream.Stream)

			s.handlerMu.RLock()
			handler := s.handlers[table]
			s.handlerMu.RUnlock()

			if handler == nil {
				logger.GetLogger().Warn("No handler for table",
					zap.String("table", table))
				continue
			}

			for _, msg := range stream.Messages {
				if err := s.processMessage(s.ctx, client, stream.Stream, msg, handler); err != nil {
					logger.GetLogger().Error("Failed to process message",
						zap.String("stream", stream.Stream),
						zap.String("msg_id", msg.ID),
						zap.Error(err))
				}
			}
		}
	}
}

// processMessage 处理单条消息
func (s *RedisSubscriber) processMessage(ctx context.Context, client redis.Cmdable, streamKey string, msg redis.XMessage, handler EventHandler) error {
	// 解析消息
	dataStr, ok := msg.Values["data"].(string)
	if !ok {
		return fmt.Errorf("invalid message format: missing data field")
	}

	event, err := FromJSONString(dataStr)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// 重试逻辑
	var lastErr error
	for i := 0; i <= s.cfg.MaxRetries; i++ {
		if i > 0 {
			time.Sleep(s.cfg.RetryDelay)
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

		// 自动确认消息
		if s.cfg.AutoAck {
			if err := client.XAck(ctx, streamKey, s.cfg.ConsumerGroup, msg.ID).Err(); err != nil {
				logger.GetLogger().Warn("Failed to ack message",
					zap.String("msg_id", msg.ID),
					zap.Error(err))
			}
		}

		return nil
	}

	// 所有重试都失败
	atomic.AddInt64(&s.failedCount, 1)
	s.IncrFailed(1)
	s.HandleError(event, lastErr)

	return &ErrConsumeFailed{Event: event, Cause: lastErr}
}

// extractTableFromStreamKey 从 stream key 中提取表名
func (s *RedisSubscriber) extractTableFromStreamKey(streamKey string) string {
	prefix := s.cfg.StreamPrefix
	if len(streamKey) > len(prefix) {
		return streamKey[len(prefix):]
	}
	return streamKey
}

// Unsubscribe 取消订阅
func (s *RedisSubscriber) Unsubscribe(ctx context.Context, tables []string) error {
	s.handlerMu.Lock()
	for _, table := range tables {
		delete(s.handlers, table)
		s.RemoveSubscribedTable(table)
	}
	s.handlerMu.Unlock()

	logger.GetLogger().Info("Unsubscribed from tables",
		zap.Strings("tables", tables))

	return nil
}

// Stats 获取统计信息
func (s *RedisSubscriber) Stats() *SubscriberStats {
	stats := s.BaseSubscriber.Stats()
	stats.PublishedEvents = atomic.LoadInt64(&s.publishedCount)
	stats.ConsumedEvents = atomic.LoadInt64(&s.consumedCount)
	stats.FailedEvents = atomic.LoadInt64(&s.failedCount)
	return stats
}

// AckMessage 手动确认消息（当 AutoAck 为 false 时使用）
func (s *RedisSubscriber) AckMessage(ctx context.Context, table string, msgID string) error {
	client := s.redisPool.GetClient()
	if client == nil {
		return fmt.Errorf("failed to get Redis client")
	}

	streamKey := fmt.Sprintf("%s%s", s.cfg.StreamPrefix, table)
	return client.XAck(ctx, streamKey, s.cfg.ConsumerGroup, msgID).Err()
}

// GetPendingMessages 获取待处理消息（用于死信队列处理）
func (s *RedisSubscriber) GetPendingMessages(ctx context.Context, table string, count int64) ([]redis.XPendingExt, error) {
	client := s.redisPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("failed to get Redis client")
	}

	streamKey := fmt.Sprintf("%s%s", s.cfg.StreamPrefix, table)
	return client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: streamKey,
		Group:  s.cfg.ConsumerGroup,
		Start:  "-",
		End:    "+",
		Count:  count,
	}).Result()
}

// ClaimPendingMessages 认领待处理消息（用于处理超时消息）
func (s *RedisSubscriber) ClaimPendingMessages(ctx context.Context, table string, minIdleTime time.Duration, msgIDs []string) ([]redis.XMessage, error) {
	client := s.redisPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("failed to get Redis client")
	}

	streamKey := fmt.Sprintf("%s%s", s.cfg.StreamPrefix, table)
	return client.XClaim(ctx, &redis.XClaimArgs{
		Stream:   streamKey,
		Group:    s.cfg.ConsumerGroup,
		Consumer: s.consumerName,
		MinIdle:  minIdleTime,
		Messages: msgIDs,
	}).Result()
}

// GetStreamInfo 获取 Stream 信息
func (s *RedisSubscriber) GetStreamInfo(ctx context.Context, table string) (*redis.XInfoStream, error) {
	client := s.redisPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("failed to get Redis client")
	}

	streamKey := fmt.Sprintf("%s%s", s.cfg.StreamPrefix, table)
	return client.XInfoStream(ctx, streamKey).Result()
}
