package buffer

import (
	"fmt"
	"os"
	"testing"
	"time"

	"minIODB/internal/config"
	mock_storage "minIODB/internal/storage/mock"

	"github.com/go-redis/redismock/v8"
	"github.com/golang/mock/gomock"
	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
)

const testFlushInterval = 50 * time.Millisecond
const testBufferSize = 2

func init() {
	// 确保测试临时目录存在
	os.MkdirAll(tempDir, 0755)
}

func TestSharedBuffer_FlushOnSize(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, redisMock := redismock.NewClientMock()
	mockUploader := mock_storage.NewMockUploader(ctrl)

	// 创建一个基本的配置
	cfg, _ := config.LoadConfig("")
	b := NewSharedBuffer(redisClient, mockUploader, nil, "", cfg)
	b.flushDone = make(chan struct{}, 1)

	row := DataRow{Table: "test", ID: "test-id", Timestamp: time.Now().UnixNano(), Payload: "{}"}
	dayStr := time.Unix(0, row.Timestamp).Format("2006-01-02")
	redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", row.Table, row.ID, dayStr)

	// Expectations - 使用正则表达式匹配任意值
	redisMock.Regexp().ExpectSAdd(redisKey, `.*`).SetVal(1)
	mockUploader.EXPECT().BucketExists(gomock.Any(), minioBucket).Return(true, nil)
	mockUploader.EXPECT().FPutObject(gomock.Any(), minioBucket, gomock.Any(), gomock.Any(), gomock.Any()).Return(minio.UploadInfo{}, nil)

	// Action
	b.Add(row)
	b.Add(row) // This should trigger the flush

	select {
	case <-b.flushDone:
		// Test passed
	case <-time.After(2 * time.Second): // 增加超时时间
		t.Fatal("timed out waiting for flush")
	}

	assert.NoError(t, redisMock.ExpectationsWereMet())
	b.Stop()
}

func TestSharedBuffer_FlushOnTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, redisMock := redismock.NewClientMock()
	mockUploader := mock_storage.NewMockUploader(ctrl)

	// 创建一个基本的配置
	cfg, _ := config.LoadConfig("")
	b := NewSharedBuffer(redisClient, mockUploader, nil, "", cfg)
	b.flushDone = make(chan struct{}, 1)

	row := DataRow{Table: "test", ID: "timed-id", Timestamp: time.Now().UnixNano(), Payload: "{}"}
	dayStr := time.Unix(0, row.Timestamp).Format("2006-01-02")
	redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", row.Table, row.ID, dayStr)

	// Expectations - 使用正则表达式匹配任意值
	redisMock.Regexp().ExpectSAdd(redisKey, `.*`).SetVal(1)
	mockUploader.EXPECT().BucketExists(gomock.Any(), minioBucket).Return(true, nil)
	mockUploader.EXPECT().FPutObject(gomock.Any(), minioBucket, gomock.Any(), gomock.Any(), gomock.Any()).Return(minio.UploadInfo{}, nil)

	// Action
	b.Add(row)

	select {
	case <-b.flushDone:
		// Test passed
	case <-time.After(2 * time.Second): // 增加超时时间
		t.Fatal("timed out waiting for flush")
	}

	assert.NoError(t, redisMock.ExpectationsWereMet())
	b.Stop()
}

func TestSharedBuffer_AutomaticBackup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, redisMock := redismock.NewClientMock()
	mockPrimary := mock_storage.NewMockUploader(ctrl)
	mockBackup := mock_storage.NewMockUploader(ctrl)

	backupBucketName := "olap-backup"
	// 创建一个基本的配置
	cfg, _ := config.LoadConfig("")
	b := NewSharedBuffer(redisClient, mockPrimary, mockBackup, backupBucketName, cfg)
	b.flushDone = make(chan struct{}, 1)

	row := DataRow{Table: "test", ID: "backup-test-id", Timestamp: time.Now().UnixNano(), Payload: "{}"}
	dayStr := time.Unix(0, row.Timestamp).Format("2006-01-02")
	redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", row.Table, row.ID, dayStr)

	// Expectations
	// Primary
	mockPrimary.EXPECT().BucketExists(gomock.Any(), minioBucket).Return(true, nil)
	mockPrimary.EXPECT().FPutObject(gomock.Any(), minioBucket, gomock.Any(), gomock.Any(), gomock.Any()).Return(minio.UploadInfo{}, nil)
	// Backup
	mockBackup.EXPECT().BucketExists(gomock.Any(), backupBucketName).Return(true, nil)
	mockBackup.EXPECT().FPutObject(gomock.Any(), backupBucketName, gomock.Any(), gomock.Any(), gomock.Any()).Return(minio.UploadInfo{}, nil)
	// Redis - 使用正则表达式匹配任意值
	redisMock.Regexp().ExpectSAdd(redisKey, `.*`).SetVal(1)

	// Action
	b.Add(row)
	b.Add(row) // Trigger flush

	select {
	case <-b.flushDone:
		// OK
	case <-time.After(2 * time.Second): // 增加超时时间
		t.Fatal("timed out waiting for flush")
	}

	assert.NoError(t, redisMock.ExpectationsWereMet())
	b.Stop()
}
