package test

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockRedisClient Mock Redis客户端实现
type MockRedisClient struct {
	data    map[string]*mockRedisValue
	mutex   sync.RWMutex
	pingErr error // 用于模拟连接错误
}

// mockRedisValue Redis值的内部表示
type mockRedisValue struct {
	value      interface{}
	expiration time.Time
	exists     bool
}

// NewMockRedisClient 创建新的Mock Redis客户端
func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data: make(map[string]*mockRedisValue),
	}
}

// NewMockRedisClientWithError 创建带有模拟错误的Mock Redis客户端
func NewMockRedisClientWithError(pingErr error) *MockRedisClient {
	return &MockRedisClient{
		data:    make(map[string]*mockRedisValue),
		pingErr: pingErr,
	}
}

// Ping 测试连接
func (m *MockRedisClient) Ping(ctx context.Context) error {
	if m.pingErr != nil {
		return m.pingErr
	}
	return nil
}

// Set 设置键值对
func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	expTime := time.Time{}
	if expiration > 0 {
		expTime = time.Now().Add(expiration)
	}

	m.data[key] = &mockRedisValue{
		value:      value,
		expiration: expTime,
		exists:     true,
	}

	return nil
}

// Get 获取键值
func (m *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	val, exists := m.data[key]
	if !exists || !val.exists {
		return "", fmt.Errorf("redis: nil")
	}

	// 检查过期时间
	if !val.expiration.IsZero() && time.Now().After(val.expiration) {
		return "", fmt.Errorf("redis: nil")
	}

	switch v := val.value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// GetBytes 获取字节数组
func (m *MockRedisClient) GetBytes(ctx context.Context, key string) ([]byte, error) {
	value, err := m.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}

// Del 删除键
func (m *MockRedisClient) Del(ctx context.Context, keys ...string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, key := range keys {
		delete(m.data, key)
	}

	return nil
}

// Exists 检查键是否存在
func (m *MockRedisClient) Exists(ctx context.Context, key string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	val, exists := m.data[key]
	if !exists || !val.exists {
		return false, nil
	}

	// 检查过期时间
	if !val.expiration.IsZero() && time.Now().After(val.expiration) {
		return false, nil
	}

	return true, nil
}

// Expire 设置过期时间
func (m *MockRedisClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	val, exists := m.data[key]
	if !exists || !val.exists {
		return fmt.Errorf("redis: nil")
	}

	val.expiration = time.Now().Add(expiration)
	return nil
}

// TTL 获取剩余生存时间
func (m *MockRedisClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	val, exists := m.data[key]
	if !exists || !val.exists {
		return -time.Second, nil // Redis约定：-1表示键不存在
	}

	if val.expiration.IsZero() {
		return -1, nil // Redis约定：-1表示永不过期
	}

	ttl := val.expiration.Sub(time.Now())
	if ttl <= 0 {
		return 0, nil // Redis约定：0表示已过期
	}

	return ttl, nil
}

// HSet 设置哈希字段
func (m *MockRedisClient) HSet(ctx context.Context, key string, field string, value interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	hashKey := fmt.Sprintf("h:%s:%s", key, field)
	m.data[hashKey] = &mockRedisValue{
		value:      value,
		expiration: time.Time{},
		exists:     true,
	}

	return nil
}

// HGet 获取哈希字段值
func (m *MockRedisClient) HGet(ctx context.Context, key string, field string) (string, error) {
	hashKey := fmt.Sprintf("h:%s:%s", key, field)
	return m.Get(ctx, hashKey)
}

// HGetAll 获取所有哈希字段
func (m *MockRedisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]string)
	hashPrefix := fmt.Sprintf("h:%s:", key)

	for k, val := range m.data {
		if len(k) > len(hashPrefix) && k[:len(hashPrefix)] == hashPrefix {
			if val.exists {
				field := k[len(hashPrefix):]
				switch v := val.value.(type) {
				case string:
					result[field] = v
				case []byte:
					result[field] = string(v)
				default:
					result[field] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	return result, nil
}

// HDel 删除哈希字段
func (m *MockRedisClient) HDel(ctx context.Context, key string, fields ...string) error {
	for _, field := range fields {
		hashKey := fmt.Sprintf("h:%s:%s", key, field)
		m.Del(ctx, hashKey)
	}
	return nil
}

// HExists 检查哈希字段是否存在
func (m *MockRedisClient) HExists(ctx context.Context, key string, field string) (bool, error) {
	hashKey := fmt.Sprintf("h:%s:%s", key, field)
	return m.Exists(ctx, hashKey)
}

// Sadd 向集合添加成员
func (m *MockRedisClient) Sadd(ctx context.Context, key string, members ...interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	setKey := fmt.Sprintf("s:%s", key)
	var set map[string]bool

	if val, exists := m.data[setKey]; exists && val.exists {
		if existingSet, ok := val.value.(map[string]bool); ok {
			set = existingSet
		} else {
			set = make(map[string]bool)
		}
	} else {
		set = make(map[string]bool)
	}

	for _, member := range members {
		memberStr := fmt.Sprintf("%v", member)
		set[memberStr] = true
	}

	m.data[setKey] = &mockRedisValue{
		value:      set,
		expiration: time.Time{},
		exists:     true,
	}

	return nil
}

// Srem 从集合删除成员
func (m *MockRedisClient) Srem(ctx context.Context, key string, members ...interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	setKey := fmt.Sprintf("s:%s", key)
	val, exists := m.data[setKey]
	if !exists || !val.exists {
		return nil
	}

	set, ok := val.value.(map[string]bool)
	if !ok {
		return nil
	}

	for _, member := range members {
		memberStr := fmt.Sprintf("%v", member)
		delete(set, memberStr)
	}

	return nil
}

// Smembers 获取集合所有成员
func (m *MockRedisClient) Smembers(ctx context.Context, key string) ([]string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	setKey := fmt.Sprintf("s:%s", key)
	val, exists := m.data[setKey]
	if !exists || !val.exists {
		return []string{}, nil
	}

	set, ok := val.value.(map[string]bool)
	if !ok {
		return []string{}, nil
	}

	var members []string
	for member := range set {
		members = append(members, member)
	}

	return members, nil
}

// Sismember 检查成员是否属于集合
func (m *MockRedisClient) Sismember(ctx context.Context, key string, member interface{}) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	setKey := fmt.Sprintf("s:%s", key)
	val, exists := m.data[setKey]
	if !exists || !val.exists {
		return false, nil
	}

	set, ok := val.value.(map[string]bool)
	if !ok {
		return false, nil
	}

	memberStr := fmt.Sprintf("%v", member)
	return set[memberStr], nil
}

// Lpush 向列表左侧推入元素
func (m *MockRedisClient) Lpush(ctx context.Context, key string, values ...interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	listKey := fmt.Sprintf("l:%s", key)
	var list []string

	if val, exists := m.data[listKey]; exists && val.exists {
		if existingList, ok := val.value.([]string); ok {
			list = existingList
		} else {
			list = []string{}
		}
	} else {
		list = []string{}
	}

	// 左侧推入（倒序）
	for i := len(values) - 1; i >= 0; i-- {
		valueStr := fmt.Sprintf("%v", values[i])
		list = append([]string{valueStr}, list...)
	}

	m.data[listKey] = &mockRedisValue{
		value:      list,
		expiration: time.Time{},
		exists:     true,
	}

	return nil
}

// Rpush 向列表右侧推入元素
func (m *MockRedisClient) Rpush(ctx context.Context, key string, values ...interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	listKey := fmt.Sprintf("l:%s", key)
	var list []string

	if val, exists := m.data[listKey]; exists && val.exists {
		if existingList, ok := val.value.([]string); ok {
			list = existingList
		} else {
			list = []string{}
		}
	} else {
		list = []string{}
	}

	// 右侧推入
	for _, value := range values {
		valueStr := fmt.Sprintf("%v", value)
		list = append(list, valueStr)
	}

	m.data[listKey] = &mockRedisValue{
		value:      list,
		expiration: time.Time{},
		exists:     true,
	}

	return nil
}

// Lrange 获取列表范围
func (m *MockRedisClient) Lrange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	listKey := fmt.Sprintf("l:%s", key)
	val, exists := m.data[listKey]
	if !exists || !val.exists {
		return []string{}, nil
	}

	list, ok := val.value.([]string)
	if !ok {
		return []string{}, nil
	}

	listLen := int64(len(list))

	// 处理负数索引
	if start < 0 {
		start = listLen + start
	}
	if stop < 0 {
		stop = listLen + stop
	}

	// 确保索引有效
	if start < 0 {
		start = 0
	}
	if stop >= listLen {
		stop = listLen - 1
	}
	if start > stop {
		return []string{}, nil
	}

	return list[start : stop+1], nil
}

// Llen 获取列表长度
func (m *MockRedisClient) Llen(ctx context.Context, key string) (int64, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	listKey := fmt.Sprintf("l:%s", key)
	val, exists := m.data[listKey]
	if !exists || !val.exists {
		return 0, nil
	}

	list, ok := val.value.([]string)
	if !ok {
		return 0, nil
	}

	return int64(len(list)), nil
}

// Incr 自增计数器
func (m *MockRedisClient) Incr(ctx context.Context, key string) (int64, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	val, exists := m.data[key]
	var current int64 = 0

	if exists && val.exists {
		switch v := val.value.(type) {
		case int64:
			current = v
		case int:
			current = int64(v)
		case string:
			if n, err := fmt.Sscanf(v, "%d", &current); err == nil && n == 1 {
				// 成功解析整数
			} else {
				current = 0 // 解析失败，重置为0
			}
		}
	}

	current++
	m.data[key] = &mockRedisValue{
		value:      current,
		expiration: time.Time{},
		exists:     true,
	}

	return current, nil
}

// Decr 自减计数器
func (m *MockRedisClient) Decr(ctx context.Context, key string) (int64, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	val, exists := m.data[key]
	var current int64 = 0

	if exists && val.exists {
		switch v := val.value.(type) {
		case int64:
			current = v
		case int:
			current = int64(v)
		case string:
			if n, err := fmt.Sscanf(v, "%d", &current); err == nil && n == 1 {
				// 成功解析整数
			} else {
				current = 0 // 解析失败，重置为0
			}
		}
	}

	current--
	m.data[key] = &mockRedisValue{
		value:      current,
		expiration: time.Time{},
		exists:     true,
	}

	return current, nil
}

// Helper methods for testing

// SetConnectionError 设置连接错误（用于测试连接失败场景）
func (m *MockRedisClient) SetConnectionError(err error) {
	m.pingErr = err
}

// Clear 清空所有数据
func (m *MockRedisClient) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.data = make(map[string]*mockRedisValue)
}

// GetData 获取内部数据（用于测试验证）
func (m *MockRedisClient) GetData(key string) (interface{}, bool, time.Time) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	val, exists := m.data[key]
	if !exists {
		return nil, false, time.Time{}
	}

	return val.value, val.exists, val.expiration
}

// GetAllKeys 获取所有键（用于测试验证）
func (m *MockRedisClient) GetAllKeys() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	keys := make([]string, 0, len(m.data))
	for key := range m.data {
		keys = append(keys, key)
	}

	return keys
}
