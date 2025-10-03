package config

import (
	"context"
	"minIODB/internal/logger"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain 初始化测试环境
func TestMain(m *testing.M) {
	// 初始化logger（测试环境使用简单配置）
	_ = logger.InitLogger(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})

	// 运行测试
	code := m.Run()
	os.Exit(code)
}

// TestLoadSimpleConfig 测试加载简化配置
func TestLoadSimpleConfig(t *testing.T) {
	ctx := context.Background()

	// 查找 config.simple.yaml
	simpleConfigPath := "../../config.simple.yaml"
	if _, err := os.Stat(simpleConfigPath); os.IsNotExist(err) {
		t.Skipf("config.simple.yaml not found at %s", simpleConfigPath)
	}

	cfg, err := LoadConfig(ctx, simpleConfigPath)
	if err != nil {
		t.Fatalf("Failed to load simple config: %v", err)
	}

	// 验证必需配置项已加载
	if cfg.Server.GrpcPort == "" {
		t.Error("server.grpc_port should not be empty")
	}
	if cfg.Server.RestPort == "" {
		t.Error("server.rest_port should not be empty")
	}
	if cfg.MinIO.Endpoint == "" {
		t.Error("minio.endpoint should not be empty")
	}
	if cfg.MinIO.Bucket == "" {
		t.Error("minio.bucket should not be empty")
	}

	// 验证缺省项使用默认值
	if cfg.Tables.DefaultConfig.BufferSize <= 0 {
		t.Error("default buffer_size should be positive")
	}
	if cfg.Tables.DefaultConfig.FlushInterval <= 0 {
		t.Error("default flush_interval should be positive")
	}
	if cfg.Tables.DefaultConfig.RetentionDays <= 0 {
		t.Error("default retention_days should be positive")
	}

	// 验证Redis默认禁用（单节点模式）
	if cfg.Redis.Enabled {
		t.Error("Redis should be disabled by default in simple config")
	}

	t.Logf("Simple config loaded successfully:")
	t.Logf("  - gRPC Port: %s", cfg.Server.GrpcPort)
	t.Logf("  - REST Port: %s", cfg.Server.RestPort)
	t.Logf("  - MinIO Endpoint: %s", cfg.MinIO.Endpoint)
	t.Logf("  - MinIO Bucket: %s", cfg.MinIO.Bucket)
	t.Logf("  - Redis Enabled: %v", cfg.Redis.Enabled)
	t.Logf("  - Default Buffer Size: %d", cfg.Tables.DefaultConfig.BufferSize)
	t.Logf("  - Default Flush Interval: %v", cfg.Tables.DefaultConfig.FlushInterval)
}

// TestLoadConfigWithDefaults 测试加载空配置使用默认值
func TestLoadConfigWithDefaults(t *testing.T) {
	ctx := context.Background()

	// 创建临时空配置文件
	tmpDir := t.TempDir()
	emptyConfigPath := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(emptyConfigPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create empty config: %v", err)
	}

	cfg, err := LoadConfig(ctx, emptyConfigPath)
	if err != nil {
		t.Fatalf("Failed to load empty config: %v", err)
	}

	// 验证默认值
	if cfg.Server.GrpcPort != ":8080" {
		t.Errorf("Expected default grpc_port :8080, got %s", cfg.Server.GrpcPort)
	}
	if cfg.Server.RestPort != ":8081" {
		t.Errorf("Expected default rest_port :8081, got %s", cfg.Server.RestPort)
	}
	if cfg.Server.NodeID != "node-1" {
		t.Errorf("Expected default node_id node-1, got %s", cfg.Server.NodeID)
	}
	if cfg.Tables.DefaultConfig.BufferSize != 1000 {
		t.Errorf("Expected default buffer_size 1000, got %d", cfg.Tables.DefaultConfig.BufferSize)
	}
	if cfg.Tables.DefaultConfig.FlushInterval != 30*time.Second {
		t.Errorf("Expected default flush_interval 30s, got %v", cfg.Tables.DefaultConfig.FlushInterval)
	}
	if cfg.Tables.DefaultConfig.RetentionDays != 365 {
		t.Errorf("Expected default retention_days 365, got %d", cfg.Tables.DefaultConfig.RetentionDays)
	}

	t.Log("Empty config loaded successfully with all defaults")
}

// TestLoadConfigMissingFile 测试加载不存在的配置文件
func TestLoadConfigMissingFile(t *testing.T) {
	ctx := context.Background()

	// 使用不存在的配置文件路径
	nonExistentPath := "/tmp/nonexistent-config-12345.yaml"
	cfg, err := LoadConfig(ctx, nonExistentPath)

	// 应该成功加载（使用默认配置）
	if err != nil {
		t.Fatalf("Should load with defaults when config file not found, got error: %v", err)
	}

	// 验证使用了默认值
	if cfg.Server.GrpcPort == "" {
		t.Error("Should have default grpc_port")
	}
	if cfg.MinIO.Endpoint == "" {
		t.Error("Should have default minio endpoint")
	}

	t.Log("Missing config file handled correctly with defaults")
}

// TestLoadConfigInvalidYAML 测试加载非法YAML
func TestLoadConfigInvalidYAML(t *testing.T) {
	ctx := context.Background()

	// 创建临时非法YAML文件
	tmpDir := t.TempDir()
	invalidYAMLPath := filepath.Join(tmpDir, "invalid.yaml")
	invalidYAML := `
server:
  grpc_port: :8080
  invalid_indent:
wrong indentation
`
	if err := os.WriteFile(invalidYAMLPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to create invalid yaml: %v", err)
	}

	_, err := LoadConfig(ctx, invalidYAMLPath)
	if err == nil {
		t.Error("Should return error for invalid YAML syntax")
	}

	// 错误信息应该包含提示
	if err != nil {
		t.Logf("Expected error received: %v", err)
	}
}

// TestLoadConfigMissingRequiredFields 测试缺失必需字段
func TestLoadConfigMissingRequiredFields(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	testCases := []struct {
		name       string
		yaml       string
		shouldFail bool
		errorMsg   string
	}{
		{
			name: "redis_enabled_missing_addr",
			yaml: `
server:
  grpc_port: ":8080"
  rest_port: ":8081"
minio:
  endpoint: "localhost:9000"
  bucket: "test-bucket"
network:
  pools:
    redis:
      enabled: true
      addr: ""
`,
			shouldFail: true,
			errorMsg:   "redis.addr is required when Redis is enabled",
		},
		{
			name: "valid_config_with_defaults",
			yaml: `
server:
  grpc_port: ":8080"
  rest_port: ":8081"
minio:
  endpoint: "localhost:9000"
  bucket: "test-bucket"
`,
			shouldFail: false,
			errorMsg:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tc.name+".yaml")
			if err := os.WriteFile(configPath, []byte(tc.yaml), 0644); err != nil {
				t.Fatalf("Failed to create test config: %v", err)
			}

			_, err := LoadConfig(ctx, configPath)
			if tc.shouldFail {
				if err == nil {
					t.Errorf("Expected error containing '%s', but got no error", tc.errorMsg)
				} else {
					t.Logf("Got expected error: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				} else {
					t.Logf("Config loaded successfully with defaults")
				}
			}
		})
	}
}

// TestEnvOverride 测试环境变量覆盖
func TestEnvOverride(t *testing.T) {
	ctx := context.Background()

	// 设置环境变量
	testEnvVars := map[string]string{
		"REDIS_ENABLED":  "true",
		"REDIS_HOST":     "test-redis",
		"REDIS_PORT":     "6380",
		"REDIS_PASSWORD": "test-pass",
		"MINIO_ENDPOINT": "test-minio:9000",
		"MINIO_BUCKET":   "test-bucket",
		"GRPC_PORT":      "9090",
		"REST_PORT":      "9091",
	}

	// 保存原始环境变量
	originalEnvVars := make(map[string]string)
	for key, value := range testEnvVars {
		originalEnvVars[key] = os.Getenv(key)
		os.Setenv(key, value)
	}

	// 测试后恢复环境变量
	defer func() {
		for key, originalValue := range originalEnvVars {
			if originalValue == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, originalValue)
			}
		}
	}()

	// 加载配置（使用空配置让环境变量生效）
	cfg, err := LoadConfig(ctx, "")
	if err != nil {
		t.Fatalf("Failed to load config with env override: %v", err)
	}

	// 验证环境变量覆盖
	if !cfg.Redis.Enabled {
		t.Error("REDIS_ENABLED should override config")
	}
	expectedRedisAddr := "test-redis:6380"
	if cfg.Redis.Addr != expectedRedisAddr {
		t.Errorf("Expected redis addr %s, got %s", expectedRedisAddr, cfg.Redis.Addr)
	}
	if cfg.Redis.Password != "test-pass" {
		t.Errorf("Expected redis password test-pass, got %s", cfg.Redis.Password)
	}
	if cfg.MinIO.Endpoint != "test-minio:9000" {
		t.Errorf("Expected minio endpoint test-minio:9000, got %s", cfg.MinIO.Endpoint)
	}
	if cfg.MinIO.Bucket != "test-bucket" {
		t.Errorf("Expected minio bucket test-bucket, got %s", cfg.MinIO.Bucket)
	}
	if cfg.Server.GrpcPort != ":9090" {
		t.Errorf("Expected grpc port :9090, got %s", cfg.Server.GrpcPort)
	}
	if cfg.Server.RestPort != ":9091" {
		t.Errorf("Expected rest port :9091, got %s", cfg.Server.RestPort)
	}

	t.Log("Environment variable override works correctly")
}

// TestTableConfig 测试表级配置
func TestTableConfig(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// 创建包含表级配置的文件
	configPath := filepath.Join(tmpDir, "table_config.yaml")
	configYAML := `
server:
  grpc_port: ":8080"
  rest_port: ":8081"
minio:
  endpoint: "localhost:9000"
  bucket: "test-bucket"
tables:
  default_config:
    buffer_size: 1000
    flush_interval: 30s
    retention_days: 365
    backup_enabled: true
  special_table:
    buffer_size: 5000
    flush_interval: 60s
    retention_days: 730
    backup_enabled: false
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := LoadConfig(ctx, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 测试默认表配置
	defaultTableCfg := cfg.GetTableConfig("nonexistent_table")
	if defaultTableCfg.BufferSize != 1000 {
		t.Errorf("Expected default buffer_size 1000, got %d", defaultTableCfg.BufferSize)
	}

	// 测试特定表配置
	specialTableCfg := cfg.GetTableConfig("special_table")
	if specialTableCfg.BufferSize != 5000 {
		t.Errorf("Expected special_table buffer_size 5000, got %d", specialTableCfg.BufferSize)
	}
	if specialTableCfg.FlushInterval != 60*time.Second {
		t.Errorf("Expected special_table flush_interval 60s, got %v", specialTableCfg.FlushInterval)
	}
	if specialTableCfg.RetentionDays != 730 {
		t.Errorf("Expected special_table retention_days 730, got %d", specialTableCfg.RetentionDays)
	}
	if specialTableCfg.BackupEnabled {
		t.Error("Expected special_table backup_enabled false")
	}

	t.Log("Table-level config works correctly")
}

// TestTableNameValidation 测试表名验证
func TestTableNameValidation(t *testing.T) {
	ctx := context.Background()

	cfg, err := LoadConfig(ctx, "")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	testCases := []struct {
		name      string
		tableName string
		valid     bool
	}{
		{"valid_simple", "users", true},
		{"valid_with_underscore", "user_events", true},
		{"valid_with_numbers", "table123", true},
		{"valid_mixed", "User_Events_2024", true},
		{"invalid_starts_with_number", "123table", false},
		{"invalid_special_char", "user-events", false},
		{"invalid_empty", "", false},
		{"invalid_too_long", "a" + string(make([]byte, 64)), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isValid := cfg.IsValidTableName(ctx, tc.tableName)
			if isValid != tc.valid {
				t.Errorf("Table name '%s': expected valid=%v, got valid=%v", tc.tableName, tc.valid, isValid)
			}
		})
	}
}
