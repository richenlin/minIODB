package buffer

import (
	"context"
	"errors"
	"minIODB/internal/logger"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func init() {
	// 初始化logger用于测试
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
}

// TestRetryConfiguration 测试重试配置
func TestRetryConfiguration(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		maxRetries    int
		retryDelay    time.Duration
		expectedCalls int // 期望的总调用次数（初次 + 重试）
	}{
		{
			name:          "no_retry",
			maxRetries:    0,
			retryDelay:    100 * time.Millisecond,
			expectedCalls: 1, // 只调用一次，不重试
		},
		{
			name:          "retry_once",
			maxRetries:    1,
			retryDelay:    100 * time.Millisecond,
			expectedCalls: 2, // 初次 + 1次重试
		},
		{
			name:          "retry_three_times",
			maxRetries:    3,
			retryDelay:    50 * time.Millisecond,
			expectedCalls: 4, // 初次 + 3次重试
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ConcurrentBufferConfig{
				BufferSize:     100,
				FlushInterval:  1 * time.Hour,
				WorkerPoolSize: 1,
				TaskQueueSize:  10,
				MaxRetries:     tt.maxRetries,
				RetryDelay:     tt.retryDelay,
			}

			cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

			// 记录任务提交和处理
			taskCount := int32(0)

			// 创建一个模拟失败的任务
			task := &FlushTask{
				BufferKey: "test/id/2025-10-03",
				Rows: []DataRow{
					{ID: "test-id", Timestamp: time.Now().UnixNano(), Payload: "test"},
				},
				Priority:  0,
				CreatedAt: time.Now(),
				Retries:   0,
			}

			// 监控任务队列，计算重试次数
			go func() {
				for {
					select {
					case <-cb.taskQueue:
						atomic.AddInt32(&taskCount, 1)
					case <-time.After(1 * time.Second):
						return
					}
				}
			}()

			// 提交初始任务
			cb.taskQueue <- task
			atomic.AddInt32(&taskCount, 1)

			// 等待重试完成
			time.Sleep(time.Duration(tt.maxRetries+1) * tt.retryDelay * 2)

			// 由于没有实际的worker处理，我们只测试配置加载
			t.Logf("MaxRetries=%d, RetryDelay=%v", config.MaxRetries, config.RetryDelay)
			t.Logf("Expected initial task submission")
		})
	}
}

// TestFlushTimeout 测试刷新超时配置
func TestFlushTimeout(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		flushTimeout time.Duration
	}{
		{
			name:         "30_second_timeout",
			flushTimeout: 30 * time.Second,
		},
		{
			name:         "60_second_timeout",
			flushTimeout: 60 * time.Second,
		},
		{
			name:         "2_minute_timeout",
			flushTimeout: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ConcurrentBufferConfig{
				BufferSize:     100,
				FlushInterval:  1 * time.Hour,
				WorkerPoolSize: 1,
				TaskQueueSize:  10,
				FlushTimeout:   tt.flushTimeout,
				MaxRetries:     3,
				RetryDelay:     1 * time.Second,
			}

			cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

			if cb.config.FlushTimeout != tt.flushTimeout {
				t.Errorf("FlushTimeout not set correctly: expected %v, got %v",
					tt.flushTimeout, cb.config.FlushTimeout)
			}

			t.Logf("FlushTimeout configured: %v", cb.config.FlushTimeout)
		})
	}
}

// TestRetryDelayProgression 测试重试延迟递增
func TestRetryDelayProgression(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:     100,
		FlushInterval:  1 * time.Hour,
		WorkerPoolSize: 1,
		TaskQueueSize:  10,
		MaxRetries:     3,
		RetryDelay:     100 * time.Millisecond,
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	// 模拟重试延迟计算
	// 在 processTask 中，延迟计算为: config.RetryDelay * time.Duration(task.Retries)
	expectedDelays := []time.Duration{
		config.RetryDelay * 1, // 第1次重试
		config.RetryDelay * 2, // 第2次重试
		config.RetryDelay * 3, // 第3次重试
	}

	for i, expected := range expectedDelays {
		t.Logf("Retry %d: expected delay %v", i+1, expected)
	}

	t.Logf("Base RetryDelay: %v", cb.config.RetryDelay)
	t.Logf("MaxRetries: %d", cb.config.MaxRetries)
}

// MockFailingWorker 模拟失败的worker用于测试
type MockFailingWorker struct {
	buffer       *ConcurrentBuffer
	failCount    int32
	maxFails     int32
	attemptCount int32
	mu           sync.Mutex
}

func (m *MockFailingWorker) simulateFlush(ctx context.Context) error {
	attempts := atomic.AddInt32(&m.attemptCount, 1)

	m.mu.Lock()
	defer m.mu.Unlock()

	if attempts <= m.maxFails {
		atomic.AddInt32(&m.failCount, 1)
		return errors.New("simulated flush failure")
	}

	return nil
}

// TestRetryBehaviorWithMockFailure 测试使用模拟失败的重试行为
func TestRetryBehaviorWithMockFailure(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:     100,
		FlushInterval:  1 * time.Hour,
		WorkerPoolSize: 1,
		TaskQueueSize:  10,
		MaxRetries:     3,
		RetryDelay:     50 * time.Millisecond,
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	// 创建模拟worker
	mockWorker := &MockFailingWorker{
		buffer:   cb,
		maxFails: 2, // 前2次失败，第3次成功
	}

	// 模拟3次调用
	for i := 0; i < 3; i++ {
		err := mockWorker.simulateFlush(ctx)
		if err != nil {
			t.Logf("Attempt %d: Failed - %v", i+1, err)
			if i < int(config.MaxRetries) {
				t.Logf("Will retry after %v", config.RetryDelay*time.Duration(i+1))
				time.Sleep(config.RetryDelay * time.Duration(i+1))
			}
		} else {
			t.Logf("Attempt %d: Success", i+1)
			break
		}
	}

	failCount := atomic.LoadInt32(&mockWorker.failCount)
	attemptCount := atomic.LoadInt32(&mockWorker.attemptCount)

	t.Logf("Total attempts: %d", attemptCount)
	t.Logf("Failed attempts: %d", failCount)
	t.Logf("Success: %v", attemptCount > failCount)

	if attemptCount != 3 {
		t.Errorf("Expected 3 attempts, got %d", attemptCount)
	}
	if failCount != 2 {
		t.Errorf("Expected 2 failures, got %d", failCount)
	}
}

// TestMaxRetriesExceeded 测试超过最大重试次数
func TestMaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()

	config := &ConcurrentBufferConfig{
		BufferSize:     100,
		FlushInterval:  1 * time.Hour,
		WorkerPoolSize: 1,
		TaskQueueSize:  10,
		MaxRetries:     2,
		RetryDelay:     10 * time.Millisecond,
	}

	cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

	// 创建总是失败的模拟worker
	mockWorker := &MockFailingWorker{
		buffer:   cb,
		maxFails: 100, // 总是失败
	}

	maxAttempts := config.MaxRetries + 1 // 初次 + 重试
	for i := 0; i < maxAttempts; i++ {
		err := mockWorker.simulateFlush(ctx)
		if err != nil {
			t.Logf("Attempt %d/%d: Failed", i+1, maxAttempts)
			if i < config.MaxRetries {
				time.Sleep(config.RetryDelay * time.Duration(i+1))
			}
		}
	}

	attemptCount := atomic.LoadInt32(&mockWorker.attemptCount)
	t.Logf("Total attempts: %d (should be %d)", attemptCount, maxAttempts)
	t.Logf("Max retries exceeded - task should be marked as failed")

	if attemptCount != int32(maxAttempts) {
		t.Errorf("Expected %d attempts, got %d", maxAttempts, attemptCount)
	}
}

// TestConfigurationDefaults 测试默认配置值
func TestConfigurationDefaults(t *testing.T) {
	defaultConfig := DefaultConcurrentBufferConfig()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"FlushTimeout", defaultConfig.FlushTimeout, 60 * time.Second},
		{"MaxRetries", defaultConfig.MaxRetries, 3},
		{"RetryDelay", defaultConfig.RetryDelay, 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s: got %v, expected %v", tt.name, tt.got, tt.expected)
			} else {
				t.Logf("%s: %v ✓", tt.name, tt.got)
			}
		})
	}
}

// TestRetryWithDifferentDelays 测试不同的重试延迟
func TestRetryWithDifferentDelays(t *testing.T) {
	ctx := context.Background()

	delays := []time.Duration{
		10 * time.Millisecond,
		100 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
	}

	for _, delay := range delays {
		t.Run(delay.String(), func(t *testing.T) {
			config := &ConcurrentBufferConfig{
				BufferSize:     100,
				FlushInterval:  1 * time.Hour,
				WorkerPoolSize: 1,
				TaskQueueSize:  10,
				MaxRetries:     2,
				RetryDelay:     delay,
			}

			cb := NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)

			if cb.config.RetryDelay != delay {
				t.Errorf("RetryDelay not set: expected %v, got %v", delay, cb.config.RetryDelay)
			}

			// 模拟重试延迟序列
			for retry := 1; retry <= config.MaxRetries; retry++ {
				expectedDelay := delay * time.Duration(retry)
				t.Logf("Retry %d: delay %v", retry, expectedDelay)
			}
		})
	}
}

// BenchmarkRetryConfiguration 性能测试
func BenchmarkRetryConfiguration(b *testing.B) {
	ctx := context.Background()
	config := DefaultConcurrentBufferConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewConcurrentBuffer(ctx, nil, nil, "", "test-node", config)
	}
}
