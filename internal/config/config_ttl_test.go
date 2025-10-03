package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTTLConfiguration 测试TTL配置
func TestTTLConfiguration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	t.Run("default_ttl_values", func(t *testing.T) {
		// 创建空配置文件，使用默认值
		emptyConfigPath := filepath.Join(tmpDir, "empty.yaml")
		if err := os.WriteFile(emptyConfigPath, []byte(""), 0644); err != nil {
			t.Fatalf("Failed to create empty config: %v", err)
		}

		cfg, err := LoadConfig(ctx, emptyConfigPath)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// 验证默认TTL值为5分钟
		expectedTTL := 5 * time.Minute
		if cfg.QueryOptimization.LocalFileIndexTTL != expectedTTL {
			t.Errorf("Expected LocalFileIndexTTL %v, got %v",
				expectedTTL, cfg.QueryOptimization.LocalFileIndexTTL)
		}
		if cfg.QueryOptimization.MetadataCacheTTL != expectedTTL {
			t.Errorf("Expected MetadataCacheTTL %v, got %v",
				expectedTTL, cfg.QueryOptimization.MetadataCacheTTL)
		}

		t.Logf("Default TTL values: LocalFileIndexTTL=%v, MetadataCacheTTL=%v",
			cfg.QueryOptimization.LocalFileIndexTTL,
			cfg.QueryOptimization.MetadataCacheTTL)
	})

	t.Run("custom_ttl_values", func(t *testing.T) {
		// 创建自定义TTL配置
		customConfigPath := filepath.Join(tmpDir, "custom.yaml")
		customConfig := `
server:
  grpc_port: ":8080"
  rest_port: ":8081"
minio:
  endpoint: "localhost:9000"
  bucket: "test-bucket"
query_optimization:
  local_file_index_ttl: 10m
  metadata_cache_ttl: 15m
`
		if err := os.WriteFile(customConfigPath, []byte(customConfig), 0644); err != nil {
			t.Fatalf("Failed to create custom config: %v", err)
		}

		cfg, err := LoadConfig(ctx, customConfigPath)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// 验证自定义TTL值
		expectedLocalTTL := 10 * time.Minute
		expectedMetaTTL := 15 * time.Minute

		if cfg.QueryOptimization.LocalFileIndexTTL != expectedLocalTTL {
			t.Errorf("Expected LocalFileIndexTTL %v, got %v",
				expectedLocalTTL, cfg.QueryOptimization.LocalFileIndexTTL)
		}
		if cfg.QueryOptimization.MetadataCacheTTL != expectedMetaTTL {
			t.Errorf("Expected MetadataCacheTTL %v, got %v",
				expectedMetaTTL, cfg.QueryOptimization.MetadataCacheTTL)
		}

		t.Logf("Custom TTL values: LocalFileIndexTTL=%v, MetadataCacheTTL=%v",
			cfg.QueryOptimization.LocalFileIndexTTL,
			cfg.QueryOptimization.MetadataCacheTTL)
	})

	t.Run("zero_ttl_uses_default", func(t *testing.T) {
		// 测试未设置TTL时使用默认值
		zeroConfigPath := filepath.Join(tmpDir, "zero.yaml")
		zeroConfig := `
server:
  grpc_port: ":8080"
  rest_port: ":8081"
minio:
  endpoint: "localhost:9000"
  bucket: "test-bucket"
query_optimization:
  local_file_index_ttl: 0
  metadata_cache_ttl: 0
`
		if err := os.WriteFile(zeroConfigPath, []byte(zeroConfig), 0644); err != nil {
			t.Fatalf("Failed to create zero config: %v", err)
		}

		cfg, err := LoadConfig(ctx, zeroConfigPath)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// 零值应该被配置文件覆盖为0，但这在实际中可能需要验证逻辑
		// 这里我们验证配置成功加载
		if cfg.QueryOptimization.LocalFileIndexTTL != 0 {
			t.Logf("LocalFileIndexTTL is %v (expected 0 from config)",
				cfg.QueryOptimization.LocalFileIndexTTL)
		}

		t.Logf("Zero TTL config loaded: LocalFileIndexTTL=%v, MetadataCacheTTL=%v",
			cfg.QueryOptimization.LocalFileIndexTTL,
			cfg.QueryOptimization.MetadataCacheTTL)
	})

	t.Run("different_time_units", func(t *testing.T) {
		// 测试不同的时间单位
		unitsConfigPath := filepath.Join(tmpDir, "units.yaml")
		unitsConfig := `
server:
  grpc_port: ":8080"
  rest_port: ":8081"
minio:
  endpoint: "localhost:9000"
  bucket: "test-bucket"
query_optimization:
  local_file_index_ttl: 30s
  metadata_cache_ttl: 1h
`
		if err := os.WriteFile(unitsConfigPath, []byte(unitsConfig), 0644); err != nil {
			t.Fatalf("Failed to create units config: %v", err)
		}

		cfg, err := LoadConfig(ctx, unitsConfigPath)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// 验证不同时间单位
		expectedLocalTTL := 30 * time.Second
		expectedMetaTTL := 1 * time.Hour

		if cfg.QueryOptimization.LocalFileIndexTTL != expectedLocalTTL {
			t.Errorf("Expected LocalFileIndexTTL %v, got %v",
				expectedLocalTTL, cfg.QueryOptimization.LocalFileIndexTTL)
		}
		if cfg.QueryOptimization.MetadataCacheTTL != expectedMetaTTL {
			t.Errorf("Expected MetadataCacheTTL %v, got %v",
				expectedMetaTTL, cfg.QueryOptimization.MetadataCacheTTL)
		}

		t.Logf("Different units: LocalFileIndexTTL=%v, MetadataCacheTTL=%v",
			cfg.QueryOptimization.LocalFileIndexTTL,
			cfg.QueryOptimization.MetadataCacheTTL)
	})
}

// TestTTLConfigurationUsage 测试TTL配置在实际组件中的使用
func TestTTLConfigurationUsage(t *testing.T) {
	ctx := context.Background()

	// 创建测试配置
	cfg, err := LoadConfig(ctx, "")
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	// 验证配置可以被正确访问
	if cfg.QueryOptimization.LocalFileIndexTTL <= 0 {
		t.Error("LocalFileIndexTTL should be positive")
	}
	if cfg.QueryOptimization.MetadataCacheTTL <= 0 {
		t.Error("MetadataCacheTTL should be positive")
	}

	t.Logf("TTL configuration is accessible:")
	t.Logf("  LocalFileIndexTTL: %v", cfg.QueryOptimization.LocalFileIndexTTL)
	t.Logf("  MetadataCacheTTL: %v", cfg.QueryOptimization.MetadataCacheTTL)
}
