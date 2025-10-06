package mock

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// MockMinIOClient 是MinIO客户端的Mock实现
type MockMinIOClient struct {
	mu            sync.RWMutex
	objects       map[string][]byte // bucket -> object name -> data
	buckets       map[string]bool
	uploads       map[string]bool
	config        MockConfig
	lastOperation  string
	operationLog  []string
}

// NewMockMinIOClient 创建新的Mock MinIO客户端
func NewMockMinIOClient() *MockMinIOClient {
	return &MockMinIOClient{
		objects:      make(map[string][]byte),
		buckets:      make(map[string]bool),
		uploads:      make(map[string]bool),
		operationLog: make([]string, 0),
	}
}

// SetConfig 设置Mock配置
func (m *MockMinIOClient) SetConfig(config MockConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = config
}

// Reset 重置Mock状态
func (m *MockMinIOClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects = make(map[string][]byte)
	m.buckets = make(map[string]bool)
	m.uploads = make(map[string]bool)
	m.operationLog = make([]string, 0)
	m.lastOperation = ""
}

// MakeBucket 创建存储桶
func (m *MockMinIOClient) MakeBucket(ctx context.Context, bucketName string) error {
	m.logOperation("MakeBucket", bucketName)

	if m.config.MinIO.ShouldFailBucket {
		return fmt.Errorf("mock MinIO: failed to create bucket %s", bucketName)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.buckets[bucketName] {
		return fmt.Errorf("bucket %s already exists", bucketName)
	}

	m.buckets[bucketName] = true
	return nil
}

// BucketExists 检查存储桶是否存在
func (m *MockMinIOClient) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	m.logOperation("BucketExists", bucketName)

	m.mu.RLock()
	defer m.mu.RUnlock()

	exists := m.buckets[bucketName]
	return exists, nil
}

// PutObject 上传对象
func (m *MockMinIOClient) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, size int64) error {
	m.logOperation("PutObject", fmt.Sprintf("%s/%s", bucketName, objectName))

	if m.config.MinIO.ShouldFailUpload {
		return fmt.Errorf("mock MinIO: failed to upload object %s to bucket %s", objectName, bucketName)
	}

	if m.config.MinIO.UploadDelay > 0 {
		time.Sleep(m.config.MinIO.UploadDelay)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read object data: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s/%s", bucketName, objectName)
	m.objects[key] = data
	m.uploads[key] = true

	return nil
}

// GetObject 下载对象
func (m *MockMinIOClient) GetObject(ctx context.Context, bucketName, objectName string) (io.ReadCloser, error) {
	m.logOperation("GetObject", fmt.Sprintf("%s/%s", bucketName, objectName))

	if m.config.MinIO.ShouldFailDownload {
		return nil, fmt.Errorf("mock MinIO: failed to download object %s from bucket %s", objectName, bucketName)
	}

	if m.config.MinIO.DownloadDelay > 0 {
		time.Sleep(m.config.MinIO.DownloadDelay)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", bucketName, objectName)
	data, exists := m.objects[key]
	if !exists {
		return nil, fmt.Errorf("object %s not found in bucket %s", objectName, bucketName)
	}

	return io.NopCloser(strings.NewReader(string(data))), nil
}

// ObjectExists 检查对象是否存在
func (m *MockMinIOClient) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	m.logOperation("ObjectExists", fmt.Sprintf("%s/%s", bucketName, objectName))

	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", bucketName, objectName)
	_, exists := m.objects[key]
	return exists, nil
}

// ListObjects 列出对象
func (m *MockMinIOClient) ListObjects(ctx context.Context, bucketName, prefix string) ([]string, error) {
	m.logOperation("ListObjects", fmt.Sprintf("%s?prefix=%s", bucketName, prefix))

	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.buckets[bucketName] {
		return nil, fmt.Errorf("bucket %s does not exist", bucketName)
	}

	var objects []string
	for key := range m.objects {
		if strings.HasPrefix(key, fmt.Sprintf("%s/%s", bucketName, prefix)) {
			// Extract object name from key
			objectName := strings.TrimPrefix(key, fmt.Sprintf("%s/", bucketName))
			objects = append(objects, objectName)
		}
	}

	return objects, nil
}

// RemoveObject 删除对象
func (m *MockMinIOClient) RemoveObject(ctx context.Context, bucketName, objectName string) error {
	m.logOperation("RemoveObject", fmt.Sprintf("%s/%s", bucketName, objectName))

	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s/%s", bucketName, objectName)
	if _, exists := m.objects[key]; !exists {
		return fmt.Errorf("object %s not found in bucket %s", objectName, bucketName)
	}

	delete(m.objects, key)
	delete(m.uploads, key)
	return nil
}

// RemoveBucket 删除存储桶
func (m *MockMinIOClient) RemoveBucket(ctx context.Context, bucketName string) error {
	m.logOperation("RemoveBucket", bucketName)

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.buckets[bucketName] {
		return fmt.Errorf("bucket %s does not exist", bucketName)
	}

	// Check if bucket is empty
	for key := range m.objects {
		if strings.HasPrefix(key, fmt.Sprintf("%s/", bucketName)) {
			return fmt.Errorf("bucket %s is not empty", bucketName)
		}
	}

	delete(m.buckets, bucketName)
	return nil
}

// GetOperationLog 获取操作日志
func (m *MockMinIOClient) GetOperationLog() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	log := make([]string, len(m.operationLog))
	copy(log, m.operationLog)
	return log
}

// GetLastOperation 获取最后一次操作
func (m *MockMinIOClient) GetLastOperation() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastOperation
}

// GetObjectData 获取对象数据（测试用）
func (m *MockMinIOClient) GetObjectData(bucketName, objectName string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", bucketName, objectName)
	data, exists := m.objects[key]
	return data, exists
}

// GetBuckets 获取所有桶（测试用）
func (m *MockMinIOClient) GetBuckets() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	buckets := make([]string, 0, len(m.buckets))
	for bucket := range m.buckets {
		buckets = append(buckets, bucket)
	}
	return buckets
}

// logOperation 记录操作
func (m *MockMinIOClient) logOperation(operation, target string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastOperation = fmt.Sprintf("%s: %s", operation, target)
	m.operationLog = append(m.operationLog, m.lastOperation)
}