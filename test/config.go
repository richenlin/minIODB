// Package test 提供功能集成测试的统一配置和工具
package test

import (
	"context"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"

	"github.com/stretchr/testify/assert"
)

// TestConfig 功能测试统一配置
var TestConfig = &config.Config{
	Server: config.ServerConfig{
		NodeID:   "test-node",
		GrpcPort: "8080",
		RestPort: "8081",
	},
}

// InitTestLogger 初始化测试环境logger
func InitTestLogger(t *testing.T) {
	config := logger.LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
	}
	err := logger.InitLogger(config)
	assert.NoError(t, err, "should initialize test logger without error")
}

// GetTestContext 获取测试用的context
func GetTestContext() context.Context {
	return context.Background()
}

// GetTestTimeoutContext 获取带超时的测试context
func GetTestTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// CreateTestTimestamp 创建测试用的protobuf时间戳
func CreateTestTimestamp() time.Time {
	return time.Now().Truncate(time.Second)
}

// IsValidTestTableName 验证测试用的表名格式
func IsValidTestTableName(tableName string) bool {
	if tableName == "" || len(tableName) > 128 {
		return false
	}
	for _, r := range tableName {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// AssertErrorInRange 验证错误在指定范围内
func AssertErrorInRange(t *testing.T, err error, expectedError bool) {
	if expectedError {
		assert.Error(t, err, "expected an error but got nil")
	} else {
		assert.NoError(t, err, "expected no error but got an error")
	}
}
