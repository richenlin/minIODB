package config

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Redis: RedisConfig{
			Mode:     "standalone",
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
		MinIO: MinioConfig{
			Endpoint:        "localhost:9000",
			AccessKeyID:     "minioadmin",
			SecretAccessKey: "minioadmin",
			UseSSL:          false,
		},
		Server: ServerConfig{
			GrpcPort: "50051",
			RestPort: "8080",
			NodeID:   "node-1",
		},
		Buffer: BufferConfig{
			BufferSize:    1000,
			FlushInterval: 30 * time.Second,
		},
		Security: SecurityConfig{
			Mode: "none",
		},
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// 验证Redis配置
	assert.Equal(t, "standalone", config.Redis.Mode)
	assert.Equal(t, "localhost:6379", config.Redis.Addr)
	assert.Equal(t, "", config.Redis.Password)
	assert.Equal(t, 0, config.Redis.DB)

	// 验证MinIO配置
	assert.Equal(t, "localhost:9000", config.MinIO.Endpoint)
	assert.Equal(t, "minioadmin", config.MinIO.AccessKeyID)
	assert.Equal(t, "minioadmin", config.MinIO.SecretAccessKey)
	assert.Equal(t, false, config.MinIO.UseSSL)

	// 验证服务器配置
	assert.Equal(t, "50051", config.Server.GrpcPort)
	assert.Equal(t, "8080", config.Server.RestPort)
	assert.Equal(t, "node-1", config.Server.NodeID)

	// 验证缓冲区配置
	assert.Equal(t, 1000, config.Buffer.BufferSize)
	assert.Equal(t, 30*time.Second, config.Buffer.FlushInterval)

	// 验证安全配置
	assert.Equal(t, "none", config.Security.Mode)
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	// 创建临时配置文件
	configContent := `
redis:
  mode: standalone
  addr: localhost:6379
  password: ""
  db: 0
  bucket: default

minio:
  endpoint: localhost:9000
  access_key_id: minioadmin
  secret_access_key: minioadmin
  use_ssl: false
  region: us-east-1

server:
  grpc_port: "50051"
  rest_port: "8080"
  node_id: node-1

buffer:
  buffer_size: 1000
  flush_interval: 30s

log:
  level: info
  format: json
  output: stdout

monitoring:
  enabled: false
  port: "9090"
  path: /metrics

security:
  mode: none
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	// 加载配置
	config, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)

	// 验证配置
	assert.Equal(t, "standalone", config.Redis.Mode)
	assert.Equal(t, "localhost:6379", config.Redis.Addr)
	assert.Equal(t, "localhost:9000", config.MinIO.Endpoint)
	assert.Equal(t, "minioadmin", config.MinIO.AccessKeyID)
	assert.Equal(t, "minioadmin", config.MinIO.SecretAccessKey)
	assert.Equal(t, "50051", config.Server.GrpcPort)
	assert.Equal(t, "8080", config.Server.RestPort)
	assert.Equal(t, "node-1", config.Server.NodeID)
}

func TestLoadConfig_SentinelMode(t *testing.T) {
	configContent := `
redis:
  mode: sentinel
  master_name: mymaster
  sentinel_addrs: 
    - localhost:26379
    - localhost:26380
  sentinel_password: ""
  password: ""
  db: 0
  bucket: default
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)

	assert.Equal(t, "sentinel", config.Redis.Mode)
	assert.Equal(t, "mymaster", config.Redis.MasterName)
	assert.Equal(t, []string{"localhost:26379", "localhost:26380"}, config.Redis.SentinelAddrs)
}

func TestLoadConfig_ClusterMode(t *testing.T) {
	configContent := `
redis:
  mode: cluster
  cluster_addrs:
    - localhost:7000
    - localhost:7001
    - localhost:7002
  password: ""
  bucket: default
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)

	assert.Equal(t, "cluster", config.Redis.Mode)
	assert.Equal(t, []string{"localhost:7000", "localhost:7001", "localhost:7002"}, config.Redis.ClusterAddrs)
}

func TestLoadConfig_WithBackupMinIO(t *testing.T) {
	configContent := `
backup:
  enabled: true
  interval: 3600
  minio:
    endpoint: backup.minio.local:9000
    access_key_id: backupadmin
    secret_access_key: backupadmin
    use_ssl: false
    region: us-west-1
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)

	assert.True(t, config.Backup.Enabled)
	assert.Equal(t, 3600, config.Backup.Interval)
	assert.Equal(t, "backup.minio.local:9000", config.Backup.MinIO.Endpoint)
	assert.Equal(t, "backupadmin", config.Backup.MinIO.AccessKeyID)
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	// LoadConfig 在文件不存在时不会返回错误，而是返回默认配置
	config, err := LoadConfig("nonexistent.yaml")
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// 验证返回的是默认配置
	assert.Equal(t, ":8080", config.Server.GrpcPort)
	assert.Equal(t, ":8081", config.Server.RestPort)
	assert.Equal(t, "localhost:6379", config.Redis.Addr)
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("invalid: yaml: content: [")
	require.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	assert.Error(t, err)
}

func TestValidateConfig_ValidConfig(t *testing.T) {
	config := DefaultConfig()
	validator := NewConfigValidator(*config)
	results, err := validator.ValidateAll()
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestValidateConfig_InvalidRedisMode(t *testing.T) {
	config := DefaultConfig()
	config.Redis.Mode = "invalid"

	validator := NewConfigValidator(*config)
	results, err := validator.ValidateAll()
	require.NoError(t, err)

	// 检查是否有错误结果
	hasError := false
	for _, result := range results {
		if result.Status == "error" {
			hasError = true
			break
		}
	}
	assert.True(t, hasError)
}

func TestValidateConfig_MissingRedisAddr(t *testing.T) {
	config := DefaultConfig()
	config.Redis.Addr = ""

	validator := NewConfigValidator(*config)
	results, err := validator.ValidateAll()
	require.NoError(t, err)

	// 检查是否有Redis相关的错误
	hasRedisError := false
	for _, result := range results {
		if result.Component == "redis" && result.Status == "error" {
			hasRedisError = true
			break
		}
	}
	assert.True(t, hasRedisError)
}

func TestValidateConfig_MissingMinIOEndpoint(t *testing.T) {
	config := DefaultConfig()
	config.MinIO.Endpoint = ""

	validator := NewConfigValidator(*config)
	results, err := validator.ValidateAll()
	require.NoError(t, err)

	// 检查是否有MinIO相关的错误
	hasMinIOError := false
	for _, result := range results {
		if result.Component == "minio" && result.Status == "error" {
			hasMinIOError = true
			break
		}
	}
	assert.True(t, hasMinIOError)
}

func TestValidateConfig_InvalidServerPort(t *testing.T) {
	config := DefaultConfig()
	config.Server.GrpcPort = "invalid"

	validator := NewConfigValidator(*config)
	results, err := validator.ValidateAll()
	require.NoError(t, err)

	// 检查是否有服务器相关的错误
	hasServerError := false
	for _, result := range results {
		if result.Component == "server" && result.Status == "error" {
			hasServerError = true
			break
		}
	}
	assert.True(t, hasServerError)
}

func TestValidateJWTSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "空密钥",
			secret:  "",
			wantErr: true,
			errMsg:  "required",
		},
		{
			name:    "弱密钥 - 太短",
			secret:  "short",
			wantErr: true,
			errMsg:  "at least 32 characters",
		},
		{
			name:    "弱密钥 - 已知默认值",
			secret:  "your-super-secret-jwt-key-change-this-in-production",
			wantErr: true,
			errMsg:  "weak or default pattern",
		},
		{
			name:    "弱密钥 - 常见弱密钥",
			secret:  "secret",
			wantErr: true,
			errMsg:  "weak or default pattern",
		},
		{
			name:    "弱密钥 - 足够长的常见密钥",
			secret:  "your-super-secret-jwt-key-change-this-in-production",
			wantErr: true,
			errMsg:  "weak or default pattern",
		},
		{
			name:    "有效密钥",
			secret:  "this-is-a-very-strong-jwt-secret-key-12345",
			wantErr: false,
		},
		{
			name:    "最小长度密钥",
			secret:  "12345678901234567890123456789012",
			wantErr: false,
		},
		{
			name:    "刚好31字符",
			secret:  "1234567890123456789012345678901",
			wantErr: true,
			errMsg:  "at least 32 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJWTSecret(tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateJWTSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantErr {
				if tt.errMsg != "" {
					if !containsString(err.Error(), tt.errMsg) {
						t.Errorf("error message should contain %q, got %q", tt.errMsg, err.Error())
					}
				}
			}
		})
	}
}

func TestLoadConfig_WithJWTSecret(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-jwt-secret-from-environment-variable-123")
	defer os.Unsetenv("JWT_SECRET")

	configContent := `
server:
  grpc_port: "50051"
  rest_port: "8080"
  node_id: node-1

minio:
  endpoint: localhost:9000
  access_key_id: minioadmin
  secret_access_key: minioadmin
  use_ssl: false
  bucket: default

redis:
  mode: standalone
  addr: localhost:6379
  password: ""
  db: 0
  bucket: default

auth:
  enable_jwt: true
  token_expiry: 24h

security:
  mode: none
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)

	assert.Equal(t, "test-jwt-secret-from-environment-variable-123", cfg.Auth.JWTSecret)
}

func TestLoadConfig_WithoutJWTSecret(t *testing.T) {
	os.Unsetenv("JWT_SECRET")

	configContent := `
server:
  grpc_port: "50051"
  rest_port: "8080"
  node_id: node-1

minio:
  endpoint: localhost:9000
  access_key_id: minioadmin
  secret_access_key: minioadmin
  use_ssl: false
  bucket: default

redis:
  mode: standalone
  addr: localhost:6379
  password: ""
  db: 0
  bucket: default

auth:
  enable_jwt: true
  token_expiry: 24h

security:
  mode: none
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "JWT secret")
}

func TestLoadConfig_WithWeakJWTSecret(t *testing.T) {
	os.Setenv("JWT_SECRET", "secret")
	defer os.Unsetenv("JWT_SECRET")

	configContent := `
server:
  grpc_port: "50051"
  rest_port: "8080"
  node_id: node-1

minio:
  endpoint: localhost:9000
  access_key_id: minioadmin
  secret_access_key: minioadmin
  use_ssl: false
  bucket: default

redis:
  mode: standalone
  addr: localhost:6379
  password: ""
  db: 0
  bucket: default

auth:
  enable_jwt: true
  token_expiry: 24h

security:
  mode: none
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "weak or default")
}

func TestLoadConfig_WithShortJWTSecret(t *testing.T) {
	os.Setenv("JWT_SECRET", "short-secret")
	defer os.Unsetenv("JWT_SECRET")

	configContent := `
server:
  grpc_port: "50051"
  rest_port: "8080"
  node_id: node-1

minio:
  endpoint: localhost:9000
  access_key_id: minioadmin
  secret_access_key: minioadmin
  use_ssl: false
  bucket: default

redis:
  mode: standalone
  addr: localhost:6379
  password: ""
  db: 0
  bucket: default

auth:
  enable_jwt: true
  token_expiry: 24h

security:
  mode: none
`

	tmpFile, err := ioutil.TempFile("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && contains(s[:len(s)-1], substr) || s[len(s)-len(substr):] == substr)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
