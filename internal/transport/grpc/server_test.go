package grpc

import (
	"context"
	"strings"
	"testing"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	mock_storage "minIODB/internal/storage/mock"

	"github.com/go-redis/redismock/v8"
	"github.com/golang/mock/gomock"
	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
)

func TestNewServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, _ := redismock.NewClientMock()
	mockPrimaryMinio := mock_storage.NewMockUploader(ctrl)
	mockBackupMinio := mock_storage.NewMockUploader(ctrl)

	cfg := config.Config{
		Minio: config.MinioConfig{
			Endpoint:        "localhost:9000",
			Bucket:          "olap-data",
			AccessKeyID:     "minioadmin",
			SecretAccessKey: "minioadmin",
			UseSSL:          false,
		},
		Backup: config.BackupConfig{
			Enabled: true,
			Minio: config.MinioConfig{
				Bucket:          "olap-backup",
				Endpoint:        "localhost:9000",
				AccessKeyID:     "minioadmin",
				SecretAccessKey: "minioadmin",
				UseSSL:          false,
			},
		},
	}

	server, err := NewServer(redisClient, mockPrimaryMinio, mockBackupMinio, cfg)
	if err != nil && strings.Contains(err.Error(), "HTTP Error: Failed to download extension") {
		t.Skip("Skipping test due to DuckDB extension download issue in test environment")
	}
	assert.NoError(t, err)
	assert.NotNil(t, server)
	assert.NotNil(t, server.ingester)
	assert.NotNil(t, server.querier)
	assert.NotNil(t, server.buffer)
	assert.Equal(t, redisClient, server.redisClient)
	assert.Equal(t, mockPrimaryMinio, server.primaryMinio)
	assert.Equal(t, mockBackupMinio, server.backupMinio)
}

func TestServer_TriggerBackup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, redisMock := redismock.NewClientMock()
	mockPrimaryMinio := mock_storage.NewMockUploader(ctrl)
	mockBackupMinio := mock_storage.NewMockUploader(ctrl)

	cfg := config.Config{
		Backup: config.BackupConfig{
			Enabled: true,
			Minio: config.MinioConfig{
				Bucket: "olap-backup",
			},
		},
	}

	// Since we are not testing the buffer/querier here, we can pass nils
	// for some parts, but NewServer needs to be constructed carefully.
	// We need to refactor NewServer to be more testable, or create a more
	// complete set of mocks. For now, we'll create the server and replace
	// the fields we care about testing. THIS IS NOT IDEAL but works around
	// the complex setup of NewServer.
	s := &Server{
		redisClient:  redisClient,
		primaryMinio: mockPrimaryMinio,
		backupMinio:  mockBackupMinio,
		cfg:          cfg,
	}

	req := &olapv1.TriggerBackupRequest{
		Id:  "test-id",
		Day: "2023-11-01",
	}
	redisKey := "index:id:test-id:2023-11-01"
	objects := []string{"obj1.parquet", "obj2.parquet"}

	// Expectations
	redisMock.ExpectSMembers(redisKey).SetVal(objects)
	mockBackupMinio.EXPECT().CopyObject(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).Return(minio.UploadInfo{}, nil)

	// Action
	res, err := s.TriggerBackup(context.Background(), req)

	// Assertions
	assert.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, int32(2), res.FilesBackedUp)
	assert.NoError(t, redisMock.ExpectationsWereMet())
}

func TestServer_RecoverData_ByNodeId(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, redisMock := redismock.NewClientMock()
	mockPrimaryMinio := mock_storage.NewMockUploader(ctrl)
	mockBackupMinio := mock_storage.NewMockUploader(ctrl)

	cfg := config.Config{
		Backup: config.BackupConfig{
			Enabled: true,
			Minio: config.MinioConfig{
				Bucket: "olap-backup",
			},
		},
	}

	s := &Server{
		redisClient:  redisClient,
		primaryMinio: mockPrimaryMinio,
		backupMinio:  mockBackupMinio,
		cfg:          cfg,
	}

	req := &olapv1.RecoverDataRequest{
		RecoveryMode: &olapv1.RecoverDataRequest_NodeId{
			NodeId: "node-2",
		},
		ForceOverwrite: true,
	}

	// Mock expectations
	nodeDataKey := "node:data:node-2"
	dataKeys := []string{"index:id:user-123:2023-11-01", "index:id:user-456:2023-11-01"}
	files1 := []string{"user-123/2023-11-01/file1.parquet", "user-123/2023-11-01/file2.parquet"}
	files2 := []string{"user-456/2023-11-01/file3.parquet"}

	// Expectations
	redisMock.ExpectSMembers(nodeDataKey).SetVal(dataKeys)
	redisMock.ExpectSMembers(dataKeys[0]).SetVal(files1)
	redisMock.ExpectSMembers(dataKeys[1]).SetVal(files2)

	// Mock file copy operations
	mockPrimaryMinio.EXPECT().CopyObject(gomock.Any(), gomock.Any(), gomock.Any()).Times(3).Return(minio.UploadInfo{}, nil)

	// Action
	res, err := s.RecoverData(context.Background(), req)

	// Assertions
	assert.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, int32(3), res.FilesRecovered)
	assert.Equal(t, 2, len(res.RecoveredKeys))
	assert.NoError(t, redisMock.ExpectationsWereMet())
}

func TestServer_RecoverData_ByTimeRange(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	redisClient, redisMock := redismock.NewClientMock()
	mockPrimaryMinio := mock_storage.NewMockUploader(ctrl)
	mockBackupMinio := mock_storage.NewMockUploader(ctrl)

	cfg := config.Config{
		Backup: config.BackupConfig{
			Enabled: true,
			Minio: config.MinioConfig{
				Bucket: "olap-backup",
			},
		},
	}

	s := &Server{
		redisClient:  redisClient,
		primaryMinio: mockPrimaryMinio,
		backupMinio:  mockBackupMinio,
		cfg:          cfg,
	}

	req := &olapv1.RecoverDataRequest{
		RecoveryMode: &olapv1.RecoverDataRequest_TimeRange{
			TimeRange: &olapv1.TimeRangeFilter{
				StartDate: "2023-11-01",
				EndDate:   "2023-11-01",
				Ids:       []string{"user-123"},
			},
		},
		ForceOverwrite: false,
	}

	// Mock expectations
	dataKey := "index:id:user-123:2023-11-01"
	files := []string{"user-123/2023-11-01/file1.parquet"}

	// Expectations
	redisMock.ExpectKeys("index:id:user-123:2023-11-01").SetVal([]string{dataKey})
	redisMock.ExpectSMembers(dataKey).SetVal(files)

	// Mock bucket exists check (file doesn't exist, so proceed with recovery)
	mockPrimaryMinio.EXPECT().BucketExists(gomock.Any(), "olap-data").Return(false, nil)
	mockPrimaryMinio.EXPECT().CopyObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(minio.UploadInfo{}, nil)

	// Action
	res, err := s.RecoverData(context.Background(), req)

	// Assertions
	assert.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, int32(1), res.FilesRecovered)
	assert.Equal(t, 1, len(res.RecoveredKeys))
	assert.NoError(t, redisMock.ExpectationsWereMet())
}
