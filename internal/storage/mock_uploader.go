package storage

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
)

// MockUploader 是一个用于测试的mock实现
type MockUploader struct {
	objects map[string][]byte
}

// NewMockUploader 创建一个新的MockUploader
func NewMockUploader() *MockUploader {
	return &MockUploader{
		objects: make(map[string][]byte),
	}
}

func (m *MockUploader) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	return true, nil
}

func (m *MockUploader) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	return nil
}

func (m *MockUploader) FPutObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return minio.UploadInfo{
		Bucket: bucketName,
		Key:    objectName,
		Size:   1024,
	}, nil
}

func (m *MockUploader) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	
	key := bucketName + "/" + objectName
	m.objects[key] = data
	
	return minio.UploadInfo{
		Bucket: bucketName,
		Key:    objectName,
		Size:   int64(len(data)),
	}, nil
}

func (m *MockUploader) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) ([]byte, error) {
	key := bucketName + "/" + objectName
	if data, ok := m.objects[key]; ok {
		return data, nil
	}
	return nil, nil
}

func (m *MockUploader) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	key := bucketName + "/" + objectName
	delete(m.objects, key)
	return nil
}

func (m *MockUploader) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)
	go func() {
		defer close(ch)
		for key := range m.objects {
			if len(key) > len(bucketName)+1 && key[:len(bucketName)+1] == bucketName+"/" {
				objectName := key[len(bucketName)+1:]
				if opts.Prefix == "" || len(objectName) >= len(opts.Prefix) && objectName[:len(opts.Prefix)] == opts.Prefix {
					ch <- minio.ObjectInfo{
						Key:  objectName,
						Size: int64(len(m.objects[key])),
					}
				}
			}
		}
	}()
	return ch
}

func (m *MockUploader) CopyObject(ctx context.Context, dest minio.CopyDestOptions, src minio.CopySrcOptions) (minio.UploadInfo, error) {
	srcKey := src.Bucket + "/" + src.Object
	destKey := dest.Bucket + "/" + dest.Object
	
	if data, ok := m.objects[srcKey]; ok {
		m.objects[destKey] = data
	}
	
	return minio.UploadInfo{
		Bucket: dest.Bucket,
		Key:    dest.Object,
		Size:   1024,
	}, nil
}

func (m *MockUploader) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	key := bucketName + "/" + objectName
	if data, ok := m.objects[key]; ok {
		return minio.ObjectInfo{
			Key:  objectName,
			Size: int64(len(data)),
		}, nil
	}
	return minio.ObjectInfo{}, nil
}

func (m *MockUploader) ObjectExists(ctx context.Context, bucketName, objectName string) (bool, error) {
	key := bucketName + "/" + objectName
	_, exists := m.objects[key]
	return exists, nil
}

func (m *MockUploader) ListObjectsSimple(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) ([]ObjectInfo, error) {
	var objects []ObjectInfo
	for key := range m.objects {
		if len(key) > len(bucketName)+1 && key[:len(bucketName)+1] == bucketName+"/" {
			objectName := key[len(bucketName)+1:]
			if opts.Prefix == "" || len(objectName) >= len(opts.Prefix) && objectName[:len(opts.Prefix)] == opts.Prefix {
				objects = append(objects, ObjectInfo{
					Name: objectName,
					Size: int64(len(m.objects[key])),
				})
			}
		}
	}
	return objects, nil
} 