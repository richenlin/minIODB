package config

import (
	"fmt"
	"time"
)

// Config MinIODB 客户端配置
type Config struct {
	Host       string            `json:"host" yaml:"host"`
	GRPCPort   int               `json:"grpc_port" yaml:"grpc_port"`
	RESTPort   int               `json:"rest_port" yaml:"rest_port"`
	Auth       *AuthConfig       `json:"auth,omitempty" yaml:"auth,omitempty"`
	Connection *ConnectionConfig `json:"connection,omitempty" yaml:"connection,omitempty"`
	Logging    *LoggingConfig    `json:"logging,omitempty" yaml:"logging,omitempty"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	APIKey    string `json:"api_key" yaml:"api_key"`
	Secret    string `json:"secret" yaml:"secret"`
	TokenType string `json:"token_type" yaml:"token_type"`
}

// ConnectionConfig 连接配置
type ConnectionConfig struct {
	MaxConnections        int           `json:"max_connections" yaml:"max_connections"`
	Timeout               time.Duration `json:"timeout" yaml:"timeout"`
	RetryAttempts         int           `json:"retry_attempts" yaml:"retry_attempts"`
	KeepAliveTime         time.Duration `json:"keepalive_time" yaml:"keepalive_time"`
	KeepAliveTimeout      time.Duration `json:"keepalive_timeout" yaml:"keepalive_timeout"`
	KeepAliveWithoutCalls bool          `json:"keepalive_without_calls" yaml:"keepalive_without_calls"`
	MaxReceiveMessageSize int           `json:"max_receive_message_size" yaml:"max_receive_message_size"`
	MaxSendMessageSize    int           `json:"max_send_message_size" yaml:"max_send_message_size"`
}

// LogLevel 日志级别
type LogLevel string

const (
	LogLevelTrace LogLevel = "TRACE"
	LogLevelDebug LogLevel = "DEBUG"
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
	LogLevelOff   LogLevel = "OFF"
)

// LogFormat 日志格式
type LogFormat string

const (
	LogFormatText LogFormat = "TEXT"
	LogFormatJSON LogFormat = "JSON"
)

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level                    LogLevel  `json:"level" yaml:"level"`
	Format                   LogFormat `json:"format" yaml:"format"`
	EnableRequestLogging     bool      `json:"enable_request_logging" yaml:"enable_request_logging"`
	EnableResponseLogging    bool      `json:"enable_response_logging" yaml:"enable_response_logging"`
	EnablePerformanceLogging bool      `json:"enable_performance_logging" yaml:"enable_performance_logging"`
}

// NewDefaultConfig 创建默认配置
func NewDefaultConfig() *Config {
	return &Config{
		Host:     "localhost",
		GRPCPort: 8080,
		RESTPort: 8081,
		Connection: &ConnectionConfig{
			MaxConnections:        10,
			Timeout:               30 * time.Second,
			RetryAttempts:         3,
			KeepAliveTime:         5 * time.Minute,
			KeepAliveTimeout:      10 * time.Second,
			KeepAliveWithoutCalls: false,
			MaxReceiveMessageSize: 4 * 1024 * 1024, // 4MB
			MaxSendMessageSize:    4 * 1024 * 1024, // 4MB
		},
		Logging: &LoggingConfig{
			Level:                    LogLevelInfo,
			Format:                   LogFormatText,
			EnableRequestLogging:     false,
			EnableResponseLogging:    false,
			EnablePerformanceLogging: true,
		},
	}
}

// GRPCAddress 获取 gRPC 服务器地址
func (c *Config) GRPCAddress() string {
	return fmt.Sprintf("%s:%d", c.Host, c.GRPCPort)
}

// RESTBaseURL 获取 REST API 基础 URL
func (c *Config) RESTBaseURL() string {
	return fmt.Sprintf("http://%s:%d", c.Host, c.RESTPort)
}

// IsAuthEnabled 检查是否启用了认证
func (c *Config) IsAuthEnabled() bool {
	return c.Auth != nil && c.Auth.APIKey != "" && c.Auth.Secret != ""
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("主机地址不能为空")
	}
	if c.GRPCPort <= 0 || c.GRPCPort > 65535 {
		return fmt.Errorf("gRPC 端口必须在 1-65535 范围内")
	}
	if c.RESTPort <= 0 || c.RESTPort > 65535 {
		return fmt.Errorf("REST 端口必须在 1-65535 范围内")
	}
	if c.Connection != nil {
		if c.Connection.MaxConnections <= 0 {
			return fmt.Errorf("最大连接数必须大于 0")
		}
		if c.Connection.Timeout <= 0 {
			return fmt.Errorf("超时时间必须大于 0")
		}
		if c.Connection.RetryAttempts < 0 {
			return fmt.Errorf("重试次数不能为负数")
		}
	}
	return nil
}
