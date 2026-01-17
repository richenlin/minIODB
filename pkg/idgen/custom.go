package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// CustomGenerator 自定义ID生成器
// 生成格式: prefix-timestamp-random
type CustomGenerator struct{}

// NewCustomGenerator 创建自定义生成器
func NewCustomGenerator() *CustomGenerator {
	return &CustomGenerator{}
}

// Generate 生成自定义ID
// 格式: prefix-YYYYMMDDHHMMSSmmm-random (如果有前缀)
// 或者: YYYYMMDDHHMMSSmmm-random (如果没有前缀)
func (g *CustomGenerator) Generate(prefix string) (string, error) {
	// 生成时间戳部分（精确到毫秒）
	now := time.Now()
	timestamp := now.Format("20060102150405")
	millis := fmt.Sprintf("%03d", now.Nanosecond()/1e6)
	
	// 生成随机部分（6字节 = 12个十六进制字符）
	randomBytes := make([]byte, 6)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	randomStr := hex.EncodeToString(randomBytes)

	// 组装ID
	if prefix != "" {
		return fmt.Sprintf("%s-%s%s-%s", prefix, timestamp, millis, randomStr), nil
	}
	return fmt.Sprintf("%s%s-%s", timestamp, millis, randomStr), nil
}
