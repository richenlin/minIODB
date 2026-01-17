package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		wantErr bool
	}{
		{"有效端口:8080", "8080", false},
		{"有效端口::8080", ":8080", false},
		{"最小端口:1024", "1024", false},
		{"最大端口:65535", "65535", false},
		{"太小的端口:1023", "1023", true},
		{"太大的端口:65536", "65536", true},
		{"无效端口:abc", "abc", true},
		{"无效端口:8080extra", "8080extra", true},
		{"空端口", "", true},
		{"系统端口:1024", "1024", false},
		{"保留端口:1023", "1023", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePort(tt.port)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"有效地址:localhost:6379", "localhost:6379", false},
		{"有效地址:127.0.0.1:6379", "127.0.0.1:6379", false},
		{"有效地址:minio:9000", "minio:9000", false},
		{"空地址", "", true},
		{"缺少端口", "localhost", true},
		{"无效端口", "localhost:99999", true},
		{"端口为空", "localhost:", true},
		{"有效端口:8080", "192.168.1.1:8080", false},
		{"最小端口:1024", "localhost:1024", false},
		{"最大端口:65535", "localhost:65535", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAddress(tt.addr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateBucketName(t *testing.T) {
	tests := []struct {
		name    string
		bucket  string
		wantErr bool
	}{
		{"有效bucket", "my-bucket", false},
		{"带数字", "bucket123", false},
		{"带点", "bucket.data", false},
		{"带连字符", "test-bucket-name", false},
		{"混合:3字符", "abc", false},
		{"混合:63字符", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"太短", "ab", true},
		{"太长", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"大写字母", "MyBucket", true},
		{"下划线", "my_bucket", true},
		{"开头连字符", "-bucket", true},
		{"结尾连字符", "bucket-", true},
		{"开头点", ".bucket", true},
		{"结尾点", "bucket.", true},
		{"空bucket", "", true},
		{"带空格", "my bucket", true},
		{"带特殊字符", "my@bucket", true},
		{"连续点", "my..bucket", false},
		{"连续连字符", "my--bucket", false},
		{"点开头连字符", ".my-bucket", true},
		{"连字符开头点", "-my.bucket", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBucketName(tt.bucket)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateConfig_Enhanced(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "有效配置",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "50051",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
				TableManagement: TableManagementConfig{
					MaxTables:        1000,
					TableNamePattern: `^[a-zA-Z][a-zA-Z0-9_]*$`,
				},
				Tables: TablesConfig{
					DefaultConfig: TableConfig{
						BufferSize:    1000,
						FlushInterval: 30 * 60 * 1000000000,
						RetentionDays: 365,
					},
				},
				System: SystemConfig{
					MaxMemoryMB:   2048,
					MaxGoroutines: 1000,
				},
			},
			wantErr: false,
		},
		{
			name: "端口太小",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "1023",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
			},
			wantErr: true,
		},
		{
			name: "端口太大",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "65536",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
			},
			wantErr: true,
		},
		{
			name: "bucket名称无效",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "50051",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "My-Bucket",
				},
			},
			wantErr: true,
		},
		{
			name: "max_tables太大",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "50051",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
				TableManagement: TableManagementConfig{
					MaxTables:        10001,
					TableNamePattern: `^[a-zA-Z][a-zA-Z0-9_]*$`,
				},
			},
			wantErr: true,
		},
		{
			name: "buffer_size太大",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "50051",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
				Tables: TablesConfig{
					DefaultConfig: TableConfig{
						BufferSize:    100001,
						FlushInterval: 30 * 60 * 1000000000,
						RetentionDays: 365,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "retention_days太大",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "50051",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
				Tables: TablesConfig{
					DefaultConfig: TableConfig{
						BufferSize:    1000,
						FlushInterval: 30 * 60 * 1000000000,
						RetentionDays: 3651,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "max_memory_mb太小",
			config: &Config{
				Server: ServerConfig{
					GrpcPort: "50051",
					RestPort: "8080",
					NodeID:   "node-1",
				},
				Redis: RedisConfig{
					Addr: "localhost:6379",
				},
				MinIO: MinioConfig{
					Endpoint: "localhost:9000",
					Bucket:   "olap-data",
				},
				System: SystemConfig{
					MaxMemoryMB:   128,
					MaxGoroutines: 1000,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
