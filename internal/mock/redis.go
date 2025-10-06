package mock

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockRedisClient 是Redis客户端的Mock实现
type MockRedisClient struct {
	mu          sync.RWMutex
	data        map[string]string // key -> value
	hashes      map[string]map[string]string // key -> field -> value
	sets         map[string]map[string]bool // key -> member -> bool
	sortedSets  map[string]map[string]float64 // key -> member -> score
	lists        map[string][]string // key -> []string
	config      MockConfig
	operationLog []string
	lastOperation string
}

// NewMockRedisClient 创建新的Mock Redis客户端
func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data:        make(map[string]string),
		hashes:      make(map[string]map[string]string),
		sets:         make(map[string]map[string]bool),
		sortedSets:  make(map[string]map[string]float64),
		lists:        make(map[string][]string),
		operationLog: make([]string, 0),
	}
}

// SetConfig 设置Mock配置
func (r *MockRedisClient) SetConfig(config MockConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = config
}

// Reset 重置Mock状态
func (r *MockRedisClient) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data = make(map[string]string)
	r.hashes = make(map[string]map[string]string)
	r.sets = make(map[string]map[string]bool)
	r.sortedSets = make(map[string]map[string]float64)
	r.lists = make(map[string][]string)
	r.operationLog = make([]string, 0)
	r.lastOperation = ""
}

// Set 设置键值
func (r *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	r.logOperation("Set", key)

	if r.config.Redis.ShouldFailSet {
		return fmt.Errorf("mock Redis: failed to set key %s", key)
	}

	if r.config.Redis.SetDelay > 0 {
		time.Sleep(r.config.Redis.SetDelay)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.data[key] = fmt.Sprintf("%v", value)
	return nil
}

// Get 获取键值
func (r *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	r.logOperation("Get", key)

	if r.config.Redis.ShouldFailGet {
		return "", fmt.Errorf("mock Redis: failed to get key %s", key)
	}

	if r.config.Redis.GetDelay > 0 {
		time.Sleep(r.config.Redis.GetDelay)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	value, exists := r.data[key]
	if !exists {
		return "", nil
	}
	return value, nil
}

// Del 删除键
func (r *MockRedisClient) Del(ctx context.Context, keys ...string) (int64, error) {
	r.logOperation("Del", strings.Join(keys, ","))

	if r.config.Redis.ShouldFailDelete {
		return 0, fmt.Errorf("mock Redis: failed to delete keys %v", keys)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for _, key := range keys {
		if _, exists := r.data[key]; exists {
			delete(r.data, key)
			delete(r.hashes, key)
			delete(r.sets, key)
			delete(r.sortedSets, key)
			delete(r.lists, key)
			count++
		}
	}
	return count, nil
}

// Exists 检查键是否存在
func (r *MockRedisClient) Exists(ctx context.Context, keys ...string) (int64, error) {
	r.logOperation("Exists", strings.Join(keys, ","))

	r.mu.RLock()
	defer r.mu.RUnlock()

	var count int64
	for _, key := range keys {
		if _, exists := r.data[key]; exists {
			count++
		}
	}
	return count, nil
}

// HSet 设置哈希字段
func (r *MockRedisClient) HSet(ctx context.Context, key string, values ...interface{}) error {
	r.logOperation("HSet", fmt.Sprintf("%s (%d fields)", key, len(values)/2))

	if r.config.Redis.ShouldFailSet {
		return fmt.Errorf("mock Redis: failed to hset key %s", key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hashes[key] == nil {
		r.hashes[key] = make(map[string]string)
	}

	for i := 0; i < len(values); i += 2 {
		field := fmt.Sprintf("%v", values[i])
		value := fmt.Sprintf("%v", values[i+1])
		r.hashes[key][field] = value
	}

	return nil
}

// HGet 获取哈希字段值
func (r *MockRedisClient) HGet(ctx context.Context, key, field string) (string, error) {
	r.logOperation("HGet", fmt.Sprintf("%s/%s", key, field))

	if r.config.Redis.ShouldFailGet {
		return "", fmt.Errorf("mock Redis: failed to hget key %s field %s", key, field)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if hash, exists := r.hashes[key]; exists {
		if value, fieldExists := hash[field]; fieldExists {
			return value, nil
		}
	}
	return "", nil
}

// HGetAll 获取哈希所有字段和值
func (r *MockRedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	r.logOperation("HGetAll", key)

	if r.config.Redis.ShouldFailGet {
		return nil, fmt.Errorf("mock Redis: failed to hgetall key %s", key)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string)
	if hash, exists := r.hashes[key]; exists {
		for field, value := range hash {
			result[field] = value
		}
	}
	return result, nil
}

// HDel 删除哈希字段
func (r *MockRedisClient) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	r.logOperation("HDel", fmt.Sprintf("%s/%v", key, fields))

	if r.config.Redis.ShouldFailDelete {
		return 0, fmt.Errorf("mock Redis: failed to hdel key %s", key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	if hash, exists := r.hashes[key]; exists {
		for _, field := range fields {
			if _, fieldExists := hash[field]; fieldExists {
				delete(hash, field)
				count++
			}
		}
		if len(hash) == 0 {
			delete(r.hashes, key)
		}
	}
	return count, nil
}

// HExists 检查哈希字段是否存在
func (r *MockRedisClient) HExists(ctx context.Context, key, field string) (bool, error) {
	r.logOperation("HExists", fmt.Sprintf("%s/%s", key, field))

	r.mu.RLock()
	defer r.mu.RUnlock()

	if hash, exists := r.hashes[key]; exists {
		_, fieldExists := hash[field]
		return fieldExists, nil
	}
	return false, nil
}

// Keys 获取所有匹配模式的键
func (r *MockRedisClient) Keys(ctx context.Context, pattern string) ([]string, error) {
	r.logOperation("Keys", pattern)

	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []string
	for key := range r.data {
		if strings.Contains(key, strings.Replace(pattern, "*", "", -1)) {
			result = append(result, key)
		}
	}
	return result, nil
}

// Expire 设置键的过期时间
func (r *MockRedisClient) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	r.logOperation("Expire", fmt.Sprintf("%s (%v)", key, expiration))

	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.data[key]
	return exists, nil
}

// TTL 获取键的剩余生存时间
func (r *MockRedisClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	r.logOperation("TTL", key)

	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, exists := r.data[key]; exists {
		return time.Hour, nil // Mock TTL
	}
	return 0, nil
}

// Ping 检查连接
func (r *MockRedisClient) Ping(ctx context.Context) (string, error) {
	r.logOperation("Ping", "")

	if r.config.Redis.ShouldFailConnect {
		return "", fmt.Errorf("mock Redis: connection failed")
	}
	return "PONG", nil
}

// Close 关闭连接
func (r *MockRedisClient) Close() error {
	r.logOperation("Close", "")
	return nil
}

// GetOperationLog 获取操作日志
func (r *MockRedisClient) GetOperationLog() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	log := make([]string, len(r.operationLog))
	copy(log, r.operationLog)
	return log
}

// GetLastOperation 获取最后一次操作
func (r *MockRedisClient) GetLastOperation() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastOperation
}

// GetData 获取所有数据（测试用）
func (r *MockRedisClient) GetData() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data := make(map[string]string)
	for k, v := range r.data {
		data[k] = v
	}
	return data
}

// GetHashes 获取所有哈希数据（测试用）
func (r *MockRedisClient) GetHashes() map[string]map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hashes := make(map[string]map[string]string)
	for k, v := range r.hashes {
		hash := make(map[string]string)
		for field, value := range v {
			hash[field] = value
		}
		hashes[k] = hash
	}
	return hashes
}

// logOperation 记录操作
func (r *MockRedisClient) logOperation(operation, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastOperation = fmt.Sprintf("%s: %s", operation, target)
	r.operationLog = append(r.operationLog, r.lastOperation)
}