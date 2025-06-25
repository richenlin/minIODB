package storage

import (
	"context"
	"io"
	"minIODB/internal/config"
	"minIODB/internal/metrics"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Uploader defines the interface for MinIO operations, allowing for mocking.
type Uploader interface {
	FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	CopyObject(ctx context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error)
	ObjectExists(ctx context.Context, objectName string) (bool, error)
	GetObject(ctx context.Context, objectName string) (io.Reader, error)
	PutObject(ctx context.Context, objectName string, reader io.Reader) error
}

// MinioClientWrapper 包装MinIO客户端以实现Uploader接口
type MinioClientWrapper struct {
	client *minio.Client
	bucket string
}

// NewMinioClientWrapper 创建MinIO客户端包装器
func NewMinioClientWrapper(cfg config.MinioConfig) (*MinioClientWrapper, error) {
	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	return &MinioClientWrapper{
		client: minioClient,
		bucket: cfg.Bucket,
	}, nil
}

// FPutObject 实现Uploader接口
func (w *MinioClientWrapper) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("FPutObject")
	
	result, err := w.client.FPutObject(ctx, bucketName, objectName, filePath, opts)
	if err != nil {
		minioMetrics.Finish("error")
		return result, err
	}
	
	minioMetrics.Finish("success")
	return result, nil
}

// BucketExists 实现Uploader接口
func (w *MinioClientWrapper) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	minioMetrics := metrics.NewMinIOMetrics("BucketExists")
	
	result, err := w.client.BucketExists(ctx, bucketName)
	if err != nil {
		minioMetrics.Finish("error")
		return result, err
	}
	
	minioMetrics.Finish("success")
	return result, nil
}

// MakeBucket 实现Uploader接口
func (w *MinioClientWrapper) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	minioMetrics := metrics.NewMinIOMetrics("MakeBucket")
	
	err := w.client.MakeBucket(ctx, bucketName, opts)
	if err != nil {
		minioMetrics.Finish("error")
		return err
	}
	
	minioMetrics.Finish("success")
	return nil
}

// CopyObject 实现Uploader接口
func (w *MinioClientWrapper) CopyObject(ctx context.Context, dst minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	minioMetrics := metrics.NewMinIOMetrics("CopyObject")
	
	result, err := w.client.CopyObject(ctx, dst, src)
	if err != nil {
		minioMetrics.Finish("error")
		return result, err
	}
	
	minioMetrics.Finish("success")
	return result, nil
}

// ObjectExists 实现Uploader接口
func (w *MinioClientWrapper) ObjectExists(ctx context.Context, objectName string) (bool, error) {
	minioMetrics := metrics.NewMinIOMetrics("ObjectExists")
	
	_, err := w.client.StatObject(ctx, w.bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			minioMetrics.Finish("success")
			return false, nil
		}
		minioMetrics.Finish("error")
		return false, err
	}
	
	minioMetrics.Finish("success")
	return true, nil
}

// GetObject 实现Uploader接口
func (w *MinioClientWrapper) GetObject(ctx context.Context, objectName string) (io.Reader, error) {
	minioMetrics := metrics.NewMinIOMetrics("GetObject")
	
	result, err := w.client.GetObject(ctx, w.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		minioMetrics.Finish("error")
		return result, err
	}
	
	minioMetrics.Finish("success")
	return result, nil
}

// PutObject 实现Uploader接口
func (w *MinioClientWrapper) PutObject(ctx context.Context, objectName string, reader io.Reader) error {
	minioMetrics := metrics.NewMinIOMetrics("PutObject")
	
	_, err := w.client.PutObject(ctx, w.bucket, objectName, reader, -1, minio.PutObjectOptions{})
	if err != nil {
		minioMetrics.Finish("error")
		return err
	}
	
	minioMetrics.Finish("success")
	return nil
}

// NewMinioClient creates a new Minio client from the given configuration
func NewMinioClient(cfg config.MinioConfig) (*minio.Client, error) {
	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	return minioClient, nil
}
