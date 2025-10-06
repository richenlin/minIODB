package test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
)

// MockMinioClient Mock MinIO客户端实现
type MockMinioClient struct {
	buckets map[string]map[string]*MockObject
	mutex   sync.RWMutex
}

type ObjectInfo struct {
	Name         string
	Size         int64
	LastModified time.Time
}

// MockObject Mock对象存储
type MockObject struct {
	Name         string
	Data         []byte
	Size         int64
	LastModified time.Time
	ContentType  string
}

// NewMockMinioClient 创建新的Mock MinIO客户端
func NewMockMinioClient() *MockMinioClient {
	return &MockMinioClient{
		buckets: make(map[string]map[string]*MockObject),
	}
}

// BucketExists 检查存储桶是否存在
func (m *MockMinioClient) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	_, exists := m.buckets[bucketName]
	return exists, nil
}

// MakeBucket 创建存储桶
func (m *MockMinioClient) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.buckets[bucketName]; exists {
		return fmt.Errorf("bucket already exists: %s", bucketName)
	}

	m.buckets[bucketName] = make(map[string]*MockObject)
	return nil
}

// FPutObject 上传文件
func (m *MockMinioClient) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	// Mock实现：忽略文件路径，创建空对象
	return m.PutObject(ctx, bucketName, objectName, nil, 0, opts)
}

// PutObject 上传对象
func (m *MockMinioClient) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查存储桶是否存在，如果不存在则创建
	if _, exists := m.buckets[bucketName]; !exists {
		m.buckets[bucketName] = make(map[string]*MockObject)
	}

	// 读取数据
	var data []byte
	var err error
	if reader != nil {
		data, err = io.ReadAll(reader)
		if err != nil {
			return minio.UploadInfo{}, err
		}
	}

	// 存储对象
	m.buckets[bucketName][objectName] = &MockObject{
		Name:         objectName,
		Data:         data,
		Size:         int64(len(data)),
		LastModified: time.Now(),
		ContentType:  opts.ContentType,
	}

	return minio.UploadInfo{
		Bucket:   bucketName,
		Key:      objectName,
		Size:     int64(len(data)),
		ETag:     "mock-etag",
		Location: fmt.Sprintf("/mock/%s/%s", bucketName, objectName),
	}, nil
}

// GetObject 获取对象
func (m *MockMinioClient) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	bucket, exists := m.buckets[bucketName]
	if !exists {
		return nil, minio.ToErrorResponse(fmt.Errorf("bucket not found: %s", bucketName))
	}

	obj, exists := bucket[objectName]
	if !exists {
		return nil, minio.ToErrorResponse(fmt.Errorf("object not found: %s/%s", bucketName, objectName))
	}

	return obj.Data, nil
}

// RemoveObject 删除对象
func (m *MockMinioClient) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	bucket, exists := m.buckets[bucketName]
	if !exists {
		return minio.ToErrorResponse(fmt.Errorf("bucket not found: %s", bucketName))
	}

	if _, exists := bucket[objectName]; !exists {
		return minio.ToErrorResponse(fmt.Errorf("object not found: %s/%s", bucketName, objectName))
	}

	delete(bucket, objectName)
	return nil
}

// ListObjects 列举对象
func (m *MockMinioClient) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)

	go func() {
		defer close(ch)
		m.mutex.RLock()
		defer m.mutex.RUnlock()

		bucket, exists := m.buckets[bucketName]
		if !exists {
			return
		}

		for _, obj := range bucket {
			select {
			case <-ctx.Done():
				return
			case ch <- minio.ObjectInfo{
				Key:          obj.Name,
				Size:         obj.Size,
				LastModified: obj.LastModified,
			}:
			}
		}
	}()

	return ch
}

// CopyObject 复制对象
func (m *MockMinioClient) CopyObject(ctx context.Context, dest minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 获取源对象
	sourceBucket, exists := m.buckets[src.Bucket]
	if !exists {
		return minio.UploadInfo{}, minio.ToErrorResponse(fmt.Errorf("source bucket not found: %s", src.Bucket))
	}

	sourceObj, exists := sourceBucket[src.Object]
	if !exists {
		return minio.UploadInfo{}, minio.ToErrorResponse(fmt.Errorf("source object not found: %s/%s", src.Bucket, src.Object))
	}

	// 检查目标存储桶是否存在，如果不存在则创建
	if _, exists := m.buckets[dest.Bucket]; !exists {
		m.buckets[dest.Bucket] = make(map[string]*MockObject)
	}

	// 复制对象
	destData := make([]byte, len(sourceObj.Data))
	copy(destData, sourceObj.Data)

	m.buckets[dest.Bucket][dest.Object] = &MockObject{
		Name:         dest.Object,
		Data:         destData,
		Size:         sourceObj.Size,
		LastModified: time.Now(),
		ContentType:  sourceObj.ContentType,
	}

	return minio.UploadInfo{
		Bucket:   dest.Bucket,
		Key:      dest.Object,
		Size:     sourceObj.Size,
		ETag:     "mock-etag",
		Location: fmt.Sprintf("/mock/%s/%s", dest.Bucket, dest.Object),
	}, nil
}

// StatObject 获取对象信息
func (m *MockMinioClient) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	bucket, exists := m.buckets[bucketName]
	if !exists {
		return minio.ObjectInfo{}, minio.ToErrorResponse(fmt.Errorf("bucket not found: %s", bucketName))
	}

	obj, exists := bucket[objectName]
	if !exists {
		return minio.ObjectInfo{}, minio.ToErrorResponse(fmt.Errorf("object not found: %s/%s", bucketName, objectName))
	}

	return minio.ObjectInfo{
		Key:          obj.Name,
		Size:         obj.Size,
		LastModified: obj.LastModified,
	}, nil
}

// ObjectExists 检查对象是否存在
func (m *MockMinioClient) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	bucket, exists := m.buckets[bucketName]
	if !exists {
		return false, nil
	}

	_, exists = bucket[objectName]
	return exists, nil
}

// ListObjectsSimple 简单列举对象
func (m *MockMinioClient) ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]ObjectInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	bucket, exists := m.buckets[bucketName]
	if !exists {
		return []ObjectInfo{}, nil
	}

	var objects []ObjectInfo
	for _, obj := range bucket {
		objects = append(objects, ObjectInfo{
			Name:         obj.Name,
			Size:         obj.Size,
			LastModified: obj.LastModified,
		})
	}

	return objects, nil
}

// Helper methods for testing

// AddObject 为测试添加对象
func (m *MockMinioClient) AddObject(bucketName, objectName string, data []byte) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.buckets[bucketName]; !exists {
		m.buckets[bucketName] = make(map[string]*MockObject)
	}

	m.buckets[bucketName][objectName] = &MockObject{
		Name:         objectName,
		Data:         data,
		Size:         int64(len(data)),
		LastModified: time.Now(),
	}
}

// GetBucketObjects 获取存储桶中的所有对象（用于测试验证）
func (m *MockMinioClient) GetBucketObjects(bucketName string) []ObjectInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	bucket, exists := m.buckets[bucketName]
	if !exists {
		return []ObjectInfo{}
	}

	var objects []ObjectInfo
	for _, obj := range bucket {
		objects = append(objects, ObjectInfo{
			Name:         obj.Name,
			Size:         obj.Size,
			LastModified: obj.LastModified,
		})
	}

	return objects
}

// Clear 清空所有数据（用于测试清理）
func (m *MockMinioClient) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.buckets = make(map[string]map[string]*MockObject)
}
