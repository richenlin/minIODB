package storage

import (
	"context"
	"fmt"
	"io"

	"minIODB/internal/pool"

	"github.com/minio/minio-go/v7"
)

// ObjectStorageImpl 对象存储实现
type ObjectStorageImpl struct {
	poolManager *pool.PoolManager
	isBackup    bool // 标识是否为备份存储
}

// NewObjectStorage 创建新的对象存储实例
func NewObjectStorage(poolManager *pool.PoolManager) ObjectStorage {
	return &ObjectStorageImpl{
		poolManager: poolManager,
		isBackup:    false,
	}
}

// NewBackupObjectStorage 创建新的备份对象存储实例
func NewBackupObjectStorage(poolManager *pool.PoolManager) ObjectStorage {
	return &ObjectStorageImpl{
		poolManager: poolManager,
		isBackup:    true,
	}
}

// getMinIOPool 获取相应的MinIO连接池
func (o *ObjectStorageImpl) getMinIOPool() *pool.MinIOPool {
	if o.isBackup {
		return o.poolManager.GetBackupMinIOPool()
	}
	return o.poolManager.GetMinIOPool()
}

// PutObject 上传对象
func (o *ObjectStorageImpl) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64) error {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端不可用")
	}

	_, err := client.PutObject(ctx, bucketName, objectName, reader, objectSize, minio.PutObjectOptions{})
	return err
}

// GetObject 获取对象
func (o *ObjectStorageImpl) GetObject(ctx context.Context, bucketName, objectName string) (*minio.Object, error) {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return nil, fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("MinIO客户端不可用")
	}

	return client.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
}

// GetObjectBytes 获取对象字节数据
func (o *ObjectStorageImpl) GetObjectBytes(ctx context.Context, bucketName, objectName string) ([]byte, error) {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return nil, fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("MinIO客户端不可用")
	}

	obj, err := client.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	// 读取对象内容
	data := make([]byte, 0)
	buffer := make([]byte, 1024)
	for {
		n, err := obj.Read(buffer)
		if n > 0 {
			data = append(data, buffer[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
	}

	return data, nil
}

// DeleteObject 删除对象
func (o *ObjectStorageImpl) DeleteObject(ctx context.Context, bucketName, objectName string) error {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端不可用")
	}

	return client.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
}

// ListObjects 列出对象（通道版本）
func (o *ObjectStorageImpl) ListObjects(ctx context.Context, bucketName string, prefix string) <-chan minio.ObjectInfo {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		// 返回一个关闭的通道
		ch := make(chan minio.ObjectInfo)
		close(ch)
		return ch
	}

	client := minioPool.GetClient()
	if client == nil {
		// 返回一个关闭的通道
		ch := make(chan minio.ObjectInfo)
		close(ch)
		return ch
	}

	return client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
}

// ListObjectsSimple 简单列出对象
func (o *ObjectStorageImpl) ListObjectsSimple(ctx context.Context, bucketName string, prefix string) ([]ObjectInfo, error) {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return nil, fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return nil, fmt.Errorf("MinIO客户端不可用")
	}

	objectCh := client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var objects []ObjectInfo
	for obj := range objectCh {
		if obj.Err != nil {
			return nil, obj.Err
		}
		objects = append(objects, ObjectInfo{
			Name:         obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
		})
	}

	return objects, nil
}

// BucketExists 检查存储桶是否存在
func (o *ObjectStorageImpl) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return false, fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return false, fmt.Errorf("MinIO客户端不可用")
	}

	return client.BucketExists(ctx, bucketName)
}

// MakeBucket 创建存储桶
func (o *ObjectStorageImpl) MakeBucket(ctx context.Context, bucketName string) error {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO连接池不可用")
	}

	client := minioPool.GetClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端不可用")
	}

	return client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
}

// HealthCheck 健康检查
func (o *ObjectStorageImpl) HealthCheck(ctx context.Context) error {
	minioPool := o.getMinIOPool()
	if minioPool == nil {
		return fmt.Errorf("MinIO pool not available")
	}

	if err := minioPool.HealthCheck(ctx); err != nil {
		return fmt.Errorf("MinIO health check failed: %w", err)
	}

	return nil
}

// GetStats 获取统计信息
func (o *ObjectStorageImpl) GetStats() map[string]interface{} {
	if o.poolManager == nil {
		return nil
	}

	stats := o.poolManager.GetStats()
	if stats == nil {
		return nil
	}

	// 返回MinIO相关的统计信息
	minioStats := make(map[string]interface{})
	if o.isBackup {
		if backup, ok := stats["backup_minio"]; ok {
			minioStats["backup_minio"] = backup
		}
	} else {
		if minio, ok := stats["minio"]; ok {
			minioStats["minio"] = minio
		}
	}

	return minioStats
}

// Close 关闭连接
func (o *ObjectStorageImpl) Close(ctx context.Context) error {
	// 注意：这里不关闭poolManager，因为它可能被其他组件共享
	// 实际的关闭操作应该由工厂或主应用程序负责
	return nil
}
