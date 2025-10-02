package config

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ValidationResult 验证结果
type ValidationResult struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ConfigValidator 配置验证器
type ConfigValidator struct {
	cfg Config
}

// NewConfigValidator 创建配置验证器
func NewConfigValidator(cfg Config) *ConfigValidator {
	return &ConfigValidator{cfg: cfg}
}

// ValidateAll 验证所有配置
func (v *ConfigValidator) ValidateAll() ([]ValidationResult, error) {
	var results []ValidationResult
	
	// 验证Redis配置
	if result := v.validateRedisConfig(); result != nil {
		results = append(results, *result)
	}
	
	// 验证MinIO配置
	if result := v.validateMinIOConfig(); result != nil {
		results = append(results, *result)
	}
	
	// 验证服务器配置
	if result := v.validateServerConfig(); result != nil {
		results = append(results, *result)
	}
	
	return results, nil
}

// validateRedisConfig 验证Redis配置
func (v *ConfigValidator) validateRedisConfig() *ValidationResult {
	cfg := v.cfg.Redis
	
	// 检查必需字段
	if cfg.Addr == "" {
		return &ValidationResult{
			Component: "redis",
			Status:    "error",
			Message:   "Redis address is required",
		}
	}
	
	// 验证地址格式
	parts := strings.Split(cfg.Addr, ":")
	if len(parts) != 2 {
		return &ValidationResult{
			Component: "redis",
			Status:    "error",
			Message:   "Invalid Redis address format (should be host:port)",
			Details: map[string]interface{}{
				"addr": cfg.Addr,
			},
		}
	}
	
	// 验证端口
	if port, err := strconv.Atoi(parts[1]); err != nil || port <= 0 || port > 65535 {
		return &ValidationResult{
			Component: "redis",
			Status:    "error",
			Message:   "Invalid Redis port",
			Details: map[string]interface{}{
				"port": parts[1],
			},
		}
	}
	
	// 测试连接
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	defer client.Close()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return &ValidationResult{
			Component: "redis",
			Status:    "error",
			Message:   "Failed to connect to Redis",
			Details: map[string]interface{}{
				"error": err.Error(),
				"addr":  cfg.Addr,
			},
		}
	}
	
	// 获取Redis信息
	info, err := client.Info(ctx).Result()
	if err != nil {
		return &ValidationResult{
			Component: "redis",
			Status:    "warning",
			Message:   "Connected to Redis but failed to get info",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}
	
	return &ValidationResult{
		Component: "redis",
		Status:    "ok",
		Message:   "Redis connection successful",
		Details: map[string]interface{}{
			"addr":    cfg.Addr,
			"db":      cfg.DB,
			"info":    info[:200] + "...", // 截取部分信息
		},
	}
}

// validateMinIOConfig 验证MinIO配置
func (v *ConfigValidator) validateMinIOConfig() *ValidationResult {
	cfg := v.cfg.MinIO
	
	if cfg.Endpoint == "" {
		return &ValidationResult{
			Component: "minio",
			Status:    "error",
			Message:   "MinIO endpoint is required",
		}
	}
	
	if cfg.AccessKeyID == "" {
		return &ValidationResult{
			Component: "minio",
			Status:    "error",
			Message:   "MinIO access key ID is required",
		}
	}
	
	if cfg.SecretAccessKey == "" {
		return &ValidationResult{
			Component: "minio",
			Status:    "error",
			Message:   "MinIO secret access key is required",
		}
	}
	
	// 注释掉bucket验证，因为MinioConfig没有BucketName字段
	// if cfg.BucketName == "" {
	// 	return &ValidationResult{
	// 		Component: "minio",
	// 		Status:    "error",
	// 		Message:   "MinIO bucket name is required",
	// 	}
	// }
	
	// 测试连接
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return &ValidationResult{
			Component: "minio",
			Status:    "error",
			Message:   "Failed to create MinIO client",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// 简单的连接测试
	_, err = client.ListBuckets(ctx)
	if err != nil {
		return &ValidationResult{
			Component: "minio",
			Status:    "error",
			Message:   "Failed to list MinIO buckets",
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}
	
	return &ValidationResult{
		Component: "minio",
		Status:    "ok",
		Message:   "MinIO connection validation successful",
		Details: map[string]interface{}{
			"endpoint": cfg.Endpoint,
			"ssl":      cfg.UseSSL,
		},
	}
}

// validateServerConfig 验证服务器配置
func (v *ConfigValidator) validateServerConfig() *ValidationResult {
	cfg := v.cfg.Server
	
	// 检查GRPC端口
	if cfg.GrpcPort == "" {
		return &ValidationResult{
			Component: "server",
			Status:    "error",
			Message:   "Server GRPC port is required",
		}
	}
	
	if port, err := strconv.Atoi(cfg.GrpcPort); err != nil || port <= 0 || port > 65535 {
		return &ValidationResult{
			Component: "server",
			Status:    "error",
			Message:   "Invalid server GRPC port",
			Details: map[string]interface{}{
				"port": cfg.GrpcPort,
			},
		}
	}
	
	// 检查端口是否被占用
	addr := fmt.Sprintf(":%s", cfg.GrpcPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return &ValidationResult{
			Component: "server",
			Status:    "error",
			Message:   "Server GRPC port is already in use",
			Details: map[string]interface{}{
				"port":  cfg.GrpcPort,
				"error": err.Error(),
			},
		}
	}
	listener.Close()
	
	// 检查REST端口（如果配置了）
	if cfg.RestPort != "" {
		if port, err := strconv.Atoi(cfg.RestPort); err != nil || port <= 0 || port > 65535 {
			return &ValidationResult{
				Component: "server",
				Status:    "error",
				Message:   "Invalid server REST port",
				Details: map[string]interface{}{
					"port": cfg.RestPort,
				},
			}
		}
		
		addr := fmt.Sprintf(":%s", cfg.RestPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return &ValidationResult{
				Component: "server",
				Status:    "error",
				Message:   "Server REST port is already in use",
				Details: map[string]interface{}{
					"port":  cfg.RestPort,
					"error": err.Error(),
				},
			}
		}
		listener.Close()
	}
	
	return &ValidationResult{
		Component: "server",
		Status:    "ok",
		Message:   "Server configuration is valid",
		Details: map[string]interface{}{
			"grpc_port": cfg.GrpcPort,
			"rest_port": cfg.RestPort,
		},
	}
}

// GetOverallStatus 获取整体状态
func (v *ConfigValidator) GetOverallStatus(results []ValidationResult) string {
	hasError := false
	hasWarning := false
	
	for _, result := range results {
		switch result.Status {
		case "error":
			hasError = true
		case "warning":
			hasWarning = true
		}
	}
	
	if hasError {
		return "error"
	} else if hasWarning {
		return "warning"
	}
	return "ok"
}

// PrintValidationResults 打印验证结果
func (v *ConfigValidator) PrintValidationResults(results []ValidationResult) {
	fmt.Println("=== Configuration Validation Results ===")
	
	for _, result := range results {
		status := result.Status
		switch status {
		case "ok":
			status = "✓ OK"
		case "warning":
			status = "⚠ WARNING"
		case "error":
			status = "✗ ERROR"
		}
		
		fmt.Printf("[%s] %s: %s\n", status, result.Component, result.Message)
		
		if result.Details != nil {
			for key, value := range result.Details {
				fmt.Printf("  %s: %v\n", key, value)
			}
		}
	}
	
	overallStatus := v.GetOverallStatus(results)
	fmt.Printf("\nOverall Status: %s\n", overallStatus)
} 