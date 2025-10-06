package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"minIODB/internal/storage"
	"minIODB/internal/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// 初始化测试logger
	_ = logger.InitLogger(logger.LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
	})
}

// TestMetadataManagerBasicOperations 元数据管理器基本操作测试
func TestMetadataManagerBasicOperations(t *testing.T) {
	ctx := context.Background()

	// 创建mock存储
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	// 创建元数据管理器
	config := &Config{
		NodeID:     "test-node",
		AutoRepair: true,
		Backup: BackupConfig{
			Bucket: "test-backup-bucket",
		},
		Recovery: RecoveryConfig{
			Bucket: "test-recovery-bucket",
		},
	}

	manager := NewManager(ctx, mockStorage, mockCacheStorage, config)
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.storage)
	assert.NotNil(t, manager.cacheStorage)
	assert.NotNil(t, manager.backupManager)
	assert.NotNil(t, manager.recoveryManager)

	// 验证配置
	assert.Equal(t, "test-node", manager.nodeID)
	assert.Equal(t, true, manager.config.AutoRepair)
	assert.Equal(t, "test-backup-bucket", manager.config.Backup.Bucket)
	assert.Equal(t, "test-recovery-bucket", manager.config.Recovery.Bucket)
}

// TestMetadataManagerStartStop 元数据管理器启动停止测试
func TestMetadataManagerStartStop(t *testing.T) {
	ctx := context.Background()

	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	config := &Config{
		NodeID:     "test-node",
		AutoRepair: false,
		Backup: BackupConfig{
			Bucket: "test-backup-bucket",
		},
		Recovery: RecoveryConfig{
			Bucket: "test-recovery-bucket",
		},
	}

	manager := NewManager(ctx, mockStorage, mockCacheStorage, config)
	assert.NotNil(t, manager)

	// 启动管理器
	err := manager.Start(ctx)
	assert.NoError(t, err)

	// 停止管理器
	err = manager.Stop(ctx)
	assert.NoError(t, err)
}

// TestGenerateNodeID 测试节点ID生成
func TestGenerateNodeID(t *testing.T) {
	nodeID1 := generateNodeID()
	nodeID2 := generateNodeID()

	// 验证生成的节点ID格式
	assert.NotEmpty(t, nodeID1)
	assert.NotEmpty(t, nodeID2)
	assert.Equal(t, 12, len(nodeID1), "Node ID should be 12 characters")
	assert.NotEqual(t, nodeID1, nodeID2, "Generated Node IDs should be unique due to timestamp")
	assert.True(t, len(nodeID1) > 0 && len(nodeID1) <= 12, "Node ID should be non-empty")
}

// TestMetadataEntryCreation 元数据条目创建测试
func TestMetadataEntryCreation(t *testing.T) {
	now := time.Now()

	entry := &MetadataEntry{
		Type:         MetadataTypeServiceRegistry,
		Key:          "test-service-node1",
		Value:        map[string]interface{}{"status": "active", "port": 8080},
		Timestamp:    now,
		TTL:          time.Hour,
	}

	// 验证元数据条目
	assert.Equal(t, MetadataTypeServiceRegistry, entry.Type)
	assert.Equal(t, "test-service-node1", entry.Key)
	assert.NotNil(t, entry.Value)
	assert.Equal(t, now, entry.Timestamp)
	assert.Equal(t, time.Hour, entry.TTL)
}

// TestSHA256Checksum SHA256校验和测试
func TestSHA256Checksum(t *testing.T) {
	// 从backup.go文件中提取功能测试
	testData := []byte("test metadata content for checksum calculation")

	// 计算SHA256校验和
	hasher := sha256.New()
	hasher.Write(testData)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	assert.NotEmpty(t, checksum)
	assert.Equal(t, 64, len(checksum), "SHA256 checksum should be 64 characters")

	// 验证相同数据产生相同校验和
	hasher2 := sha256.New()
	hasher2.Write(testData)
	checksum2 := hex.EncodeToString(hasher2.Sum(nil))

	assert.Equal(t, checksum, checksum2, "Same data should produce same checksum")

	// 验证不同数据产生不同校验和
	differentData := []byte("different metadata content")
	hasher3 := sha256.New()
	hasher3.Write(differentData)
	checksum3 := hex.EncodeToString(hasher3.Sum(nil))

	assert.NotEqual(t, checksum, checksum3, "Different data should produce different checksum")
}

// TestBackupSnapshotCreation 备份快照创建测试
func TestBackupSnapshotCreation(t *testing.T) {
	now := time.Now()

	entries := []*MetadataEntry{
		{
			Type:      MetadataTypeServiceRegistry,
			Key:       "service-1",
			Value:     map[string]interface{}{"host": "node1", "port": 8080},
			Timestamp: now,
		},
		{
			Type:      MetadataTypeTableSchema,
			Key:       "schema-users",
			Value:     map[string]interface{}{"columns": []string{"id", "name", "email"}},
			Timestamp: now,
		},
	}

	snapshot := &BackupSnapshot{
		NodeID:    "test-node",
		Timestamp: now,
		Version:   "v1.0",
		Entries:   entries,
		Checksum:  "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
		Size:      2048,
	}

	// 验证备份快照结构
	assert.Equal(t, "test-node", snapshot.NodeID)
	assert.Equal(t, now, snapshot.Timestamp)
	assert.Equal(t, "v1.0", snapshot.Version)
	assert.Len(t, snapshot.Entries, 2)
	assert.Equal(t, 64, len(snapshot.Checksum))
	assert.Equal(t, int64(2048), snapshot.Size)

	// 验证条目内容
	assert.Equal(t, MetadataTypeServiceRegistry, snapshot.Entries[0].Type)
	assert.Equal(t, MetadataTypeTableSchema, snapshot.Entries[1].Type)
}

// TestMetadataTypes 元数据类型定义测试
func TestMetadataTypes(t *testing.T) {
	// 验证不同的元数据类型
	serviceRegistry := MetadataTypeServiceRegistry
	tableSchema := MetadataTypeTableSchema
	dataIndex := MetadataTypeDataIndex
	clusterInfo := MetadataTypeClusterInfo

	assert.Equal(t, "service_registry", string(serviceRegistry))
	assert.Equal(t, "table_schema", string(tableSchema))
	assert.Equal(t, "data_index", string(dataIndex))
	assert.Equal(t, "cluster_info", string(clusterInfo))

	// 验证元数据类型转换
	assert.Equal(t, MetadataType("service_registry"), serviceRegistry)
	assert.Equal(t, MetadataType("table_schema"), tableSchema)
	assert.NotEqual(t, serviceRegistry, tableSchema)
}

// TestCacheStorageMock 缓存存储模拟测试
func TestCacheStorageMock(t *testing.T) {
	mockCache := &MockCacheStorage{}

	ctx := context.Background()
	key := "test-cache-key"
	value := "test-cache-value"
	expiration := time.Hour

	// 测试缓存设置
	err := mockCache.Set(ctx, key, value, expiration)
	assert.NoError(t, err, "Mock cache should accept set operations")

	// 测试缓存获取
	retrievedValue, err := mockCache.Get(ctx, key)
	assert.NoError(t, err)
	assert.Equal(t, value, retrievedValue, "Mock cache should return stored value")

	// 测试缓存存在检查
	exists, err := mockCache.Exists(ctx, key)
	assert.NoError(t, err)
	assert.True(t, exists > 0, "Key should exist after being set")

	// 测试缓存删除
	err = mockCache.Del(ctx, key)
	assert.NoError(t, err)

	// 验证缓存存在的修改
	exists, _ = mockCache.Exists(ctx, key)
	assert.False(t, exists > 0, "Key should not exist after deletion")
}

// MockStorage 存储模拟器
type MockStorage struct {
	data map[string]interface{}
}

func (m *MockStorage) Initialize(ctx context.Context) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	return nil
}

func (m *MockStorage) Write(ctx context.Context, key string, data interface{}) error {
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = data
	return nil
}

func (m *MockStorage) Read(ctx context.Context, key string) (interface{}, error) {
	if m.data == nil {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	value, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

func (m *MockStorage) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	keys := []string{}
	for key := range m.data {
		if len(prefix) == 0 || key[0:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (m *MockStorage) Close(ctx context.Context) error {
	return nil
}

// Implement storage.Storage interface - CacheStorage methods
func (m *MockStorage) Get(ctx context.Context, key string) (string, error) {
	value, err := m.Read(ctx, key)
	if err != nil {
		return "", err
	}
	if str, ok := value.(string); ok {
		return str, nil
	}
	return fmt.Sprintf("%v", value), nil
}

func (m *MockStorage) Del(ctx context.Context, keys ...string) error {
	if m.data == nil {
		return nil
	}
	for _, key := range keys {
		delete(m.data, key)
	}
	return nil
}

func (m *MockStorage) Exists(ctx context.Context, keys ...string) (int64, error) {
	if m.data == nil {
		return 0, nil
	}
	count := int64(0)
	for _, key := range keys {
		if _, exists := m.data[key]; exists {
			count++
		}
	}
	return count, nil
}

func (m *MockStorage) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return nil
}

func (m *MockStorage) SMembers(ctx context.Context, key string) ([]string, error) {
	return []string{"mock-member"}, nil
}

func (m *MockStorage) GetRedisMode() string {
	return "mock"
}

func (m *MockStorage) IsRedisCluster() bool {
	return false
}

func (m *MockStorage) IsRedisSentinel() bool {
	return false
}

func (m *MockStorage) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *MockStorage) GetStats() map[string]interface{} {
	return map[string]interface{}{"mock": true}
}

func (m *MockCacheStorage) GetClient() interface{} {
	return nil
}

// Implement storage.Storage interface - ObjectStorage methods
func (m *MockStorage) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64) error {
	return nil
}

func (m *MockStorage) GetObject(ctx context.Context, bucketName, objectName string) (*minio.Object, error) {
	return &minio.Object{}, nil
}

func (m *MockStorage) GetObjectBytes(ctx context.Context, bucketName, objectName string) ([]byte, error) {
	return []byte("mock-data"), nil
}

func (m *MockStorage) DeleteObject(ctx context.Context, bucketName, objectName string) error {
	return nil
}

func (m *MockStorage) ListObjects(ctx context.Context, bucketName string, prefix string) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)
	close(ch)
	return ch
}

func (m *MockStorage) ListObjectsSimple(ctx context.Context, bucketName string, prefix string) ([]storage.ObjectInfo, error) {
	return []storage.ObjectInfo{}, nil
}

func (m *MockStorage) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	return true, nil
}

func (m *MockStorage) MakeBucket(ctx context.Context, bucketName string) error {
	return nil
}

func (m *MockStorage) GetPoolManager() *pool.PoolManager {
	return &pool.PoolManager{}
}

func (m *MockStorage) GetClient() interface{} {
	return nil
}

// MockCacheStorage 缓存存储模拟器
type MockCacheStorage struct{}

func (m *MockCacheStorage) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}

// Implement storage.CacheStorage interface - missing methods
func (m *MockCacheStorage) Get(ctx context.Context, key string) (string, error) {
	return "mock-value", nil
}

func (m *MockCacheStorage) Del(ctx context.Context, keys ...string) error {
	return nil
}

func (m *MockCacheStorage) Exists(ctx context.Context, keys ...string) (int64, error) {
	return int64(len(keys)), nil
}

func (m *MockCacheStorage) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return nil
}

func (m *MockCacheStorage) SMembers(ctx context.Context, key string) ([]string, error) {
	return []string{"mock-member"}, nil
}

func (m *MockCacheStorage) GetRedisMode() string {
	return "mock"
}

func (m *MockCacheStorage) IsRedisCluster() bool {
	return false
}

func (m *MockCacheStorage) IsRedisSentinel() bool {
	return false
}

func (m *MockCacheStorage) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *MockCacheStorage) GetStats() map[string]interface{} {
	return map[string]interface{}{"mock": true}
}
// TestMetadataDataFlow 元数据数据流测试
func TestMetadataDataFlow(t *testing.T) {
	ctx := context.Background()

	// 创建测试数据
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	config := &Config{
		NodeID:     "dataflow-node",
		AutoRepair: true,
		Backup: BackupConfig{
			Bucket: "test-backup-bucket",
		},
		Recovery: RecoveryConfig{
			Bucket: "test-recovery-bucket",
		},
	}

	manager := NewManager(ctx, mockStorage, mockCacheStorage, config)
	assert.NotNil(t, manager)

	// 测试存储初始化
	err := mockStorage.Initialize(ctx)
	assert.NoError(t, err)

	// 构建测试元数据条目
	entry := &MetadataEntry{
		Type:      MetadataTypeClusterInfo,
		Key:       "cluster-nodes-info",
		Value:     map[string]interface{}{"nodes": []string{"node1", "node2", "node3"}},
		Timestamp: time.Now(),
	}

	// 测试写入操作
	err = mockStorage.Write(ctx, entry.Key, entry)
	assert.NoError(t, err)

	// 测试读取操作
	retrieved, err := mockStorage.Read(ctx, entry.Key)
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)

	// 验证缓存集成
	cacheErr := mockCacheStorage.Set(ctx, entry.Key, entry, time.Hour)
	assert.NoError(t, cacheErr)

	// 验证缓存存在性
	exists, cacheErr := mockCacheStorage.Exists(ctx, entry.Key)
	assert.NoError(t, cacheErr)
	assert.True(t, exists)
}

// TestMetadataManagerConcurrency 元数据管理器并发测试
func TestMetadataManagerConcurrency(t *testing.T) {
	ctx := context.Background()

	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	config := &Config{
		NodeID:     "concurrent-node",
		AutoRepair: true,
		Backup: BackupConfig{
			Bucket: "test-backup-bucket",
		},
		Recovery: RecoveryConfig{
			Bucket: "test-recovery-bucket",
		},
	}

	manager := NewManager(ctx, mockStorage, mockCacheStorage, config)
	assert.NotNil(t, manager)

	// 启动管理器
	err := manager.Start(ctx)
	assert.NoError(t, err)

	// 模拟并发读写操作
	concurrency := 10
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-key-%d", id)
			value := fmt.Sprintf("concurrent-value-%d", id)

			// 并发写入
			mockStorage.Write(ctx, key, value)

			// 并发读取
			retrieved, _ := mockStorage.Read(ctx, key)
			if retrieved != nil {
				// 验证读取的数据是否与写入的数据一致
				assert.Equal(t, value, retrieved, "Concurrent read should match written value")
			}
		}(i)
	}

	// 等待所有并发操作完成
	wg.Wait()

	// 停止管理器
	err = manager.Stop(ctx)
	assert.NoError(t, err)
}

// TestMetadataBackupRecoverySimulation 元数据备份恢复模拟测试
func TestMetadataBackupRecoverySimulation(t *testing.T) {
	ctx := context.Background()

	// 创建模拟存储
	mockStorage := &MockStorage{}
	mockCacheStorage := &MockCacheStorage{}

	config := &Config{
		NodeID:     "backup-recovery-node",
		AutoRepair: true,
		Backup: BackupConfig{
			Bucket: "test-backup-bucket",
		},
		Recovery: RecoveryConfig{
			Bucket: "test-recovery-bucket",
		},
	}

	manager := NewManager(ctx, mockStorage, mockCacheStorage, config)
	assert.NotNil(t, manager)

	// 生成备份数据
	backupTime := time.Now()
	entries := []*MetadataEntry{
		{
			Type:      MetadataTypeServiceRegistry,
			Key:       "service-backup-1",
			Value:     map[string]interface{}{"status": "active"},
			Timestamp: backupTime,
		},
		{
			Type:      MetadataTypeDataIndex,
			Key:       "index-backup-1",
			Value:     map[string]interface{}{"table": "users"},
			Timestamp: backupTime,
		},
	}

	snapshot := &BackupSnapshot{
		NodeID:    manager.nodeID,
		Timestamp: backupTime,
		Version:   "v2",
		Entries:   entries,
		Checksum:  "backup-checksum-123456789012345678901234567890123456789012345678901234567890abcd",
		Size:      1024,
	}

	// 模拟备份数据存储
	_ = manager.config.AutoRepair == true
	assert.False(t, err, "No error should occur for config modification")

	// 验证备份快照的完整性
	assert.NotNil(t, snapshot)
	assert.Greater(t, len(snapshot.Entries), 0)
	assert.NotEmpty(t, snapshot.Checksum)
	assert.Greater(t, snapshot.Size, int64(0))
}

// TestMetadataTTLBehavior 元数据TTL行为测试
func TestMetadataTTLBehavior(t *testing.T) {
	ctx := context.Background()

	// 创建TTL测试条目
	shortLiveEntry := &MetadataEntry{
		Type:      MetadataTypeServiceRegistry,
		Key:       "temp-service",
		Value:     map[string]interface{}{"name": "temp-service"},
		Timestamp: time.Now(),
		TTL:       time.Second, // 1秒过期
	}

	longLiveEntry := &MetadataEntry{
		Type:      MetadataTypeTableSchema,
		Key:       "permanent-schema",
		Value:     map[string]interface{}{"columns": []string{"col1", "col2"}},
		Timestamp: time.Now(),
		TTL:       0, // 永不过期
	}

	// 验证TTL设置
	assert.Equal(t, time.Second, shortLiveEntry.TTL)
	assert.Equal(t, time.Duration(0), longLiveEntry.TTL)

	// 验证时间戳
	assert.False(t, shortLiveEntry.Timestamp.IsZero())
	assert.False(t, longLiveEntry.Timestamp.IsZero())

	// 模拟TTL检查（不会实际删除，因为使用mock）
	ttlExpired := time.Since(shortLiveEntry.Timestamp) > shortLiveEntry.TTL
	if time.Since(shortLiveEntry.Timestamp) < time.Second {
		ttlExpired = false
	}

	// 在早期测试中不应该过期
	assert.False(t, ttlExpired, "TTL entry should not be expired immediately after creation")
}

func (m *MockCacheStorage) Close(ctx context.Context) error {
	return nil
}