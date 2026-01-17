package storage

import (
	"context"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
	"minIODB/internal/logger"
	"minIODB/internal/metrics"
	"minIODB/config"
)

// ObjectInfo 简化的对象信息结构
type ObjectInfo struct {
	Name         string
	Size         int64
	LastModified time.Time
}

// Uploader 定义了存储操作的接口
type Uploader interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error)
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	CopyObject(ctx context.Context, dest minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error)
	StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
	ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error)
	ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]ObjectInfo, error)
}

// MinioClient MinIO客户端封装
type MinioClient struct {
	client *minio.Client
}

// NewMinioClient 创建新的MinIO客户端
func NewMinioClient(cfg config.MinioConfig) (*MinioClient, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	return &MinioClient{
		client: client,
	}, nil
}

// MinioClientWrapper 带metrics的MinIO客户端包装器
type MinioClientWrapper struct {
	client *minio.Client
}

// NewMinioClientWrapper 创建新的MinIO客户端包装器
func NewMinioClientWrapper(cfg config.MinioConfig) (*MinioClientWrapper, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	return &MinioClientWrapper{
		client: client,
	}, nil
}

// BucketExists 检查存储桶是否存在
func (m *MinioClientWrapper) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	minioMetrics := metrics.NewMinIOMetrics("bucket_exists")
	defer func() {
		minioMetrics.Finish("success")
	}()

	exists, err := m.client.BucketExists(ctx, bucketName)
	if err != nil {
		minioMetrics.Finish("error")
		return false, err
	}
	return exists, nil
}

// MakeBucket 创建存储桶
func (m *MinioClientWrapper) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	minioMetrics := metrics.NewMinIOMetrics("make_bucket")
	defer func() {
		minioMetrics.Finish("success")
	}()

	err := m.client.MakeBucket(ctx, bucketName, opts)
	if err != nil {
		minioMetrics.Finish("error")
		return err
	}
	return nil
}

// FPutObject 从文件上传对象
func (m *MinioClientWrapper) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("fput_object")
	defer func() {
		minioMetrics.Finish("success")
	}()

	info, err := m.client.FPutObject(ctx, bucketName, objectName, filePath, opts)
	if err != nil {
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to upload file", 
			zap.Error(err), 
			zap.String("bucket", bucketName), 
			zap.String("object", objectName))
		return minio.UploadInfo{}, err
	}
	
	logger.GetLogger().Info("Successfully uploaded file", 
		zap.String("bucket", bucketName), 
		zap.String("object", objectName), 
		zap.Int64("size", info.Size))
	return info, nil
}

// PutObject 上传对象
func (m *MinioClientWrapper) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("put_object")
	defer func() {
		minioMetrics.Finish("success")
	}()

	info, err := m.client.PutObject(ctx, bucketName, objectName, reader, objectSize, opts)
	if err != nil {
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to upload object", 
			zap.Error(err), 
			zap.String("bucket", bucketName), 
			zap.String("object", objectName))
		return minio.UploadInfo{}, err
	}
	
	logger.GetLogger().Info("Successfully uploaded object", 
		zap.String("bucket", bucketName), 
		zap.String("object", objectName), 
		zap.Int64("size", info.Size))
	return info, nil
}

// GetObject 获取对象
func (m *MinioClientWrapper) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error) {
	minioMetrics := metrics.NewMinIOMetrics("get_object")
	defer func() {
		minioMetrics.Finish("success")
	}()

	object, err := m.client.GetObject(ctx, bucketName, objectName, opts)
	if err != nil {
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to get object", 
			zap.Error(err), 
			zap.String("bucket", bucketName), 
			zap.String("object", objectName))
		return nil, err
	}
	defer object.Close()

	data, err := io.ReadAll(object)
	if err != nil {
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to read object data", 
			zap.Error(err), 
			zap.String("bucket", bucketName), 
			zap.String("object", objectName))
		return nil, err
	}

	logger.GetLogger().Info("Successfully retrieved object", 
		zap.String("bucket", bucketName), 
		zap.String("object", objectName), 
		zap.Int("size", len(data)))
	return data, nil
}

// RemoveObject 删除对象
func (m *MinioClientWrapper) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	minioMetrics := metrics.NewMinIOMetrics("remove_object")
	defer func() {
		minioMetrics.Finish("success")
	}()

	err := m.client.RemoveObject(ctx, bucketName, objectName, opts)
	if err != nil {
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to remove object", 
			zap.Error(err), 
			zap.String("bucket", bucketName), 
			zap.String("object", objectName))
		return err
	}
	
	logger.GetLogger().Info("Successfully removed object", 
		zap.String("bucket", bucketName), 
		zap.String("object", objectName))
	return nil
}

// ListObjects 列出对象
func (m *MinioClientWrapper) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	minioMetrics := metrics.NewMinIOMetrics("list_objects")
	defer func() {
		minioMetrics.Finish("success")
	}()

	return m.client.ListObjects(ctx, bucketName, opts)
}

// CopyObject 复制对象
func (m *MinioClientWrapper) CopyObject(ctx context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("copy_object")
	defer func() {
		minioMetrics.Finish("success")
	}()

	info, err := m.client.CopyObject(ctx, dst, src)
	if err != nil {
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to copy object", 
			zap.Error(err), 
			zap.String("bucket", src.Bucket), 
			zap.String("object", src.Object), 
			zap.String("destination_bucket", dst.Bucket), 
			zap.String("destination_object", dst.Object))
		return minio.UploadInfo{}, err
	}
	
	logger.GetLogger().Info("Successfully copied object", 
		zap.String("bucket", src.Bucket), 
		zap.String("object", src.Object), 
		zap.String("destination_bucket", dst.Bucket), 
		zap.String("destination_object", dst.Object))
	return info, nil
}

// StatObject 获取对象信息
func (m *MinioClientWrapper) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("stat_object")
	defer func() {
		minioMetrics.Finish("success")
	}()

	info, err := m.client.StatObject(ctx, bucketName, objectName, opts)
	if err != nil {
		minioMetrics.Finish("error")
		return minio.ObjectInfo{}, err
	}
	
	return info, nil
}

// GetClient 获取原始MinIO客户端（用于需要直接访问的场景）
func (m *MinioClientWrapper) GetClient() *minio.Client {
	return m.client
}

// ObjectExists 检查对象是否存在
func (m *MinioClientWrapper) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	minioMetrics := metrics.NewMinIOMetrics("object_exists")
	defer func() {
		minioMetrics.Finish("success")
	}()

	_, err := m.client.StatObject(ctx, bucketName, objectName, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			logger.GetLogger().Info("Object does not exist", 
				zap.String("bucket", bucketName), 
				zap.String("object", objectName))
			return false, nil
		}
		minioMetrics.Finish("error")
		logger.GetLogger().Error("Failed to check object existence", 
			zap.Error(err), 
			zap.String("bucket", bucketName), 
			zap.String("object", objectName))
		return false, err
	}

	logger.GetLogger().Info("Object exists", 
		zap.String("bucket", bucketName), 
		zap.String("object", objectName))
	return true, nil
}

// ListObjectsSimple 返回对象列表的简化版本
func (m *MinioClientWrapper) ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]ObjectInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("list_objects_simple")
	defer func() {
		minioMetrics.Finish("success")
	}()

	objectCh := m.client.ListObjects(ctx, bucketName, opts)
	var objects []ObjectInfo

	for object := range objectCh {
		if object.Err != nil {
			minioMetrics.Finish("error")
			logger.GetLogger().Error("Error listing objects", 
				zap.Error(object.Err), 
				zap.String("bucket", bucketName))
			return nil, object.Err
		}

		objects = append(objects, ObjectInfo{
			Name:         object.Key,
			Size:         object.Size,
			LastModified: object.LastModified,
		})
	}

	logger.GetLogger().Info("Successfully listed objects", 
		zap.String("bucket", bucketName), 
		zap.Int("count", len(objects)))
	return objects, nil
}
