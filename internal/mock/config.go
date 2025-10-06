package mock

import (
	"time"
)

// GetTestConfigs 返回测试用的配置集合
func GetTestConfigs() map[string]MockConfig {
	return map[string]MockConfig{
		"success": {
			// 成功配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"minio_upload_fail": {
			// MinIO上传失败配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   true,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"minio_download_fail": {
			// MinIO下载失败配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: true,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"redis_connection_fail": {
			// Redis连接失败配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: true,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"redis_operations_fail": {
			// Redis操作失败配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     true,
				ShouldFailSet:     true,
				ShouldFailDelete:  true,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"duckdb_connection_fail": {
			// DuckDB连接失败配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: true,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"duckdb_operations_fail": {
			// DuckDB操作失败配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        0,
				DownloadDelay:      0,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           0,
				SetDelay:            0,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  true,
				ShouldFailExec:   true,
				QueryDelay:         0,
				ExecDelay:          0,
			},
		},
		"delays": {
			// 延迟配置
			MinIO: MockConfigMinIO{
				ShouldFailUpload:   false,
				ShouldFailDownload: false,
				ShouldFailBucket:   false,
				UploadDelay:        100 * time.Millisecond,
				DownloadDelay:      150 * time.Millisecond,
			},
			Redis: MockConfigRedis{
				ShouldFailConnect: false,
				ShouldFailGet:     false,
				ShouldFailSet:     false,
				ShouldFailDelete:  false,
				GetDelay:           50 * time.Millisecond,
				SetDelay:            75 * time.Millisecond,
			},
			DuckDB: MockConfigDuckDB{
				ShouldFailConnect: false,
				ShouldFailQuery:  false,
				ShouldFailExec:   false,
				QueryDelay:         200 * time.Millisecond,
				ExecDelay:          100 * time.Millisecond,
			},
		},
	}
}