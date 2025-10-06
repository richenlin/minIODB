package storage

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/minio/minio-go/v7"
)

// TestMinioClientBasicOperations 测试MinIO客户端基本操作
func TestMinioClientBasicOperations(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "test-bucket"
	objectName := "test-object.txt"
	testData := []byte("Hello, MinIO!")

	// 测试存储桶是否存在
	exists, err := mockClient.BucketExists(ctx, bucketName)
	require.NoError(t, err)
	assert.False(t, exists, "Bucket should not exist initially")

	// 创建存储桶
	err = mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 再次检查存储桶是否存在
	exists, err = mockClient.BucketExists(ctx, bucketName)
	require.NoError(t, err)
	assert.True(t, exists, "Bucket should exist after creation")

	// 重复创建存储桶应该报错
	err = mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	assert.Error(t, err, "Creating duplicate bucket should error")
	assert.Contains(t, err.Error(), "already exists")

	// 上传对象
	reader := bytes.NewReader(testData)
	uploadInfo, err := mockClient.PutObject(ctx, bucketName, objectName, reader, int64(len(testData)), minio.PutObjectOptions{
		ContentType: "text/plain",
	})
	require.NoError(t, err)
	assert.Equal(t, bucketName, uploadInfo.Bucket)
	assert.Equal(t, objectName, uploadInfo.Key)
	assert.Equal(t, int64(len(testData)), uploadInfo.Size)

	// 检查对象是否存在
	objExists, err := mockClient.ObjectExists(ctx, bucketName, objectName)
	require.NoError(t, err)
	assert.True(t, objExists, "Object should exist after upload")

	// 获取对象信息
	objInfo, err := mockClient.StatObject(ctx, bucketName, objectName, minio.StatObjectOptions{})
	require.NoError(t, err)
	assert.Equal(t, objectName, objInfo.Key)
	assert.Equal(t, int64(len(testData)), objInfo.Size)
	assert.False(t, objInfo.LastModified.IsZero())

	// 获取对象数据
	data, err := mockClient.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
	require.NoError(t, err)
	assert.Equal(t, testData, data)

	// 删除对象
	err = mockClient.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
	require.NoError(t, err)

	// 再次检查对象是否存在
	objExists, err = mockClient.ObjectExists(ctx, bucketName, objectName)
	require.NoError(t, err)
	assert.False(t, objExists, "Object should not exist after deletion")
}

// TestMinioClientListOperations 测试MinIO列表操作
func TestMinioClientListOperations(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "list-test-bucket"

	// 创建存储桶
	err := mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 上传多个对象
	objects := []struct {
		name string
		data []byte
	}{
		{"file1.txt", []byte("content1")},
		{"file2.txt", []byte("content2")},
		{"dir/file3.txt", []byte("content3")},
		{"dir/subdir/file4.txt", []byte("content4")},
	}

	for _, obj := range objects {
		mockClient.AddObject(bucketName, obj.name, obj.data)
	}

	// 测试简单列表
	objectsList, err := mockClient.ListObjectsSimple(ctx, bucketName, minio.ListObjectsOptions{})
	require.NoError(t, err)
	assert.Len(t, objectsList, len(objects))

	// 验证列表内容（由于使用map遍历，顺序不确定，需要验证包含关系）
	objectNames := make(map[string]bool)
	for _, obj := range objects {
		objectNames[obj.name] = true
	}

	listNames := make(map[string]bool)
	for _, objInfo := range objectsList {
		listNames[objInfo.Name] = true
		for _, obj := range objects {
			if obj.name == objInfo.Name {
				assert.Equal(t, int64(len(obj.data)), objInfo.Size)
				break
			}
		}
	}

	// 验证所有对象都已在列表中
	assert.Equal(t, len(objects), len(objectsList))
	for name := range objectNames {
		assert.True(t, listNames[name], "Expected object %s to be in list", name)
	}

	// 测试channels列表（遍历方式）
	objChan := mockClient.ListObjects(ctx, bucketName, minio.ListObjectsOptions{})
	count := 0
	for objInfo := range objChan {
		assert.NotEmpty(t, objInfo.Key)
		assert.True(t, objInfo.Size >= 0)
		count++
	}
	assert.Equal(t, len(objects), count)
}

// TestMinioClientCopyOperations 测试MinIO复制操作
func TestMinioClientCopyOperations(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	sourceBucket := "source-bucket"
	destBucket := "dest-bucket"
	sourceObject := "source.txt"
	destObject := "dest.txt"
	testData := []byte("Source content for copying")

	// 创建源存储桶
	err := mockClient.MakeBucket(ctx, sourceBucket, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 创建目标存储桶
	err = mockClient.MakeBucket(ctx, destBucket, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 添加源对象
	mockClient.AddObject(sourceBucket, sourceObject, testData)

	// 执行复制操作
	uploadInfo, err := mockClient.CopyObject(ctx,
		minio.CopyDestOptions{
			Bucket: destBucket,
			Object: destObject,
		},
		minio.CopySrcOptions{
			Bucket: sourceBucket,
			Object: sourceObject,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, destBucket, uploadInfo.Bucket)
	assert.Equal(t, destObject, uploadInfo.Key)
	assert.Equal(t, int64(len(testData)), uploadInfo.Size)

	// 验证目标对象
	copiedData, err := mockClient.GetObject(ctx, destBucket, destObject, minio.GetObjectOptions{})
	require.NoError(t, err)
	assert.Equal(t, testData, copiedData)
}

// TestMinioClientErrorHandling 测试MinIO错误处理
func TestMinioClientErrorHandling(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	nonExistentBucket := "no-exist-bucket"
	nonExistentObject := "no-exist-object.txt"

	// 测试不存在的存储桶
	exists, err := mockClient.BucketExists(ctx, nonExistentBucket)
	require.NoError(t, err)
	assert.False(t, exists)

	// 访问不存在的存储桶
	_, err = mockClient.GetObject(ctx, nonExistentBucket, nonExistentObject, minio.GetObjectOptions{})
	assert.Error(t, err)

	// 删除不存在的存储桶中的对象
	err = mockClient.RemoveObject(ctx, nonExistentBucket, nonExistentObject, minio.RemoveObjectOptions{})
	assert.Error(t, err)

	// 获取不存在对象的信息
	_, err = mockClient.StatObject(ctx, nonExistentBucket, nonExistentObject, minio.StatObjectOptions{})
	assert.Error(t, err)

	// 复制不存在的对象
	_, err = mockClient.CopyObject(ctx,
		minio.CopyDestOptions{
			Bucket: "dest-bucket",
			Object: "dest-object",
		},
		minio.CopySrcOptions{
			Bucket: nonExistentBucket,
			Object: nonExistentObject,
		},
	)
	assert.Error(t, err)
}

// TestMinioClientConcurrentOperations 测试MinIO并发操作
func TestMinioClientConcurrentOperations(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "concurrent-bucket"

	// 创建存储桶
	err := mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 并发上传多个对象
	concurrentOps := 10
	done := make(chan bool, concurrentOps)

	for i := 0; i < concurrentOps; i++ {
		go func(index int) {
			objectName := fmt.Sprintf("concurrent-%d.txt", index)
			data := []byte(fmt.Sprintf("Content %d", index))

			// 使用AddObject直接添加（无需reader）
			mockClient.AddObject(bucketName, objectName, data)
			done <- true
		}(i)
	}

	// 等待所有操作完成
	for i := 0; i < concurrentOps; i++ {
		<-done
	}

	// 验证所有对象都已创建
	objects, err := mockClient.ListObjectsSimple(ctx, bucketName, minio.ListObjectsOptions{})
	require.NoError(t, err)
	assert.Len(t, objects, concurrentOps)

	// 验证每个对象的内容
	for i := 0; i < concurrentOps; i++ {
		objectName := fmt.Sprintf("concurrent-%d.txt", i)
		expectedData := []byte(fmt.Sprintf("Content %d", i))

		data, err := mockClient.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
		require.NoError(t, err)
		assert.Equal(t, expectedData, data)
	}
}

// TestMinioClientFPutObject 测试FPutObject方法
func TestMinioClientFPutObject(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "fput-bucket"
	objectName := "file-from-path.txt"
	filePath := "/tmp/test-file.txt"

	// 创建存储桶
	err := mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 测试FPutObject（Mock实现会忽略文件路径）
	uploadInfo, err := mockClient.FPutObject(ctx, bucketName, objectName, filePath, minio.PutObjectOptions{
		ContentType: "text/plain",
	})
	require.NoError(t, err)
	assert.Equal(t, bucketName, uploadInfo.Bucket)
	assert.Equal(t, objectName, uploadInfo.Key)

	// 注意：由于Mock实现会忽略文件内容，对象大小为0
	assert.Equal(t, int64(0), uploadInfo.Size)
}

// TestMinioClientContextCancellation 测试上下文取消
func TestMinioClientContextCancellation(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx, cancel := context.WithCancel(context.Background())
	bucketName := "cancel-test-bucket"

	// 立即取消上下文
	cancel()

	// 创建存储桶（可能被取消）
	err := mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	// Mock实现不支持实际取消，但不会产生panic
	assert.NoError(t, err)
}

// TestMinioClientClear 测试清空功能
func TestMinioClientClear(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "clear-bucket"
	object1 := "obj1.txt"
	object2 := "obj2.txt"
	object3 := "obj3.txt"

	// 添加一些对象
	mockClient.AddObject(bucketName, object1, []byte("data1"))
	mockClient.AddObject(bucketName, object2, []byte("data2"))
	mockClient.AddObject(bucketName, object3, []byte("data3"))

	// 验证对象存在
	objects, err := mockClient.ListObjectsSimple(ctx, bucketName, minio.ListObjectsOptions{})
	require.NoError(t, err)
	assert.Len(t, objects, 3)

	// 清空Mock客户端
	mockClient.Clear()

	// 验证所有对象都被删除
	objects, err = mockClient.ListObjectsSimple(ctx, bucketName, minio.ListObjectsOptions{})
	require.NoError(t, err)
	assert.Len(t, objects, 0)
}

// TestMinioClientLargeObjects 测试大对象
func TestMinioClientLargeObjects(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "large-objects-bucket"

	// 创建存储桶
	err := mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	require.NoError(t, err)

	// 创建1MB的数据
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	largeObjectName := "large-object.bin"

	// 上传大对象
	largeReader := bytes.NewReader(largeData)
	uploadInfo, err := mockClient.PutObject(ctx, bucketName, largeObjectName, largeReader, int64(len(largeData)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	require.NoError(t, err)
	assert.Equal(t, bucketName, uploadInfo.Bucket)
	assert.Equal(t, largeObjectName, uploadInfo.Key)
	assert.Equal(t, int64(len(largeData)), uploadInfo.Size)

	// 获取大对象
	retrievedData, err := mockClient.GetObject(ctx, bucketName, largeObjectName, minio.GetObjectOptions{})
	require.NoError(t, err)
	assert.Len(t, retrievedData, len(largeData))
}

// TestMinioClientSpecialCharacters 测试特殊字符
func TestMinioClientSpecialCharacters(t *testing.T) {
	mockClient := NewMockMinioClient()

	ctx := context.Background()
	bucketName := "special-chars-bucket"

	// 创建存储桶
	err := mockClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	require.NoError(t, err)

	specialCharsNames := []string{
		"object-with-dash.txt",
		"object_with_underscore.txt",
		"object.with.dots.txt",
		"object with spaces.txt",
		"中文对象.txt",
		"emojis-😀😁😂.txt",
	}

	for _, name := range specialCharsNames {
		testData := []byte("Content for " + name)
		mockClient.AddObject(bucketName, name, testData)

		// 验证可以正常获取
		data, err := mockClient.GetObject(ctx, bucketName, name, minio.GetObjectOptions{})
		require.NoError(t, err)
		assert.Equal(t, testData, data)
	}

	// 验证列表包含所有对象
	objects, err := mockClient.ListObjectsSimple(ctx, bucketName, minio.ListObjectsOptions{})
	require.NoError(t, err)
	assert.Len(t, objects, len(specialCharsNames))
}