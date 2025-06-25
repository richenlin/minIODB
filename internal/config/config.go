package config

import (
	"github.com/spf13/viper"
)

// Config 应用配置
type Config struct {
	Redis      RedisConfig      `mapstructure:"redis"`
	Minio      MinioConfig      `mapstructure:"minio"`
	Server     ServerConfig     `mapstructure:"server"`
	Backup     BackupConfig     `mapstructure:"backup"`
	Buffer     BufferConfig     `mapstructure:"buffer"`
	Log        LogConfig        `mapstructure:"log"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Security   SecurityConfig   `mapstructure:"security"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// MinioConfig MinIO配置
type MinioConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	Bucket          string `mapstructure:"bucket"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	GRPCPort string `mapstructure:"grpc_port"`
	RESTPort string `mapstructure:"rest_port"`
	NodeID   string `mapstructure:"node_id"`
}

// BackupConfig 备份配置
type BackupConfig struct {
	Enabled  bool        `mapstructure:"enabled"`
	Interval int         `mapstructure:"interval"` // 备份间隔（秒）
	Minio    MinioConfig `mapstructure:"minio"`
}

// BufferConfig 缓冲区配置
type BufferConfig struct {
	MaxSize      int `mapstructure:"max_size"`
	FlushTimeout int `mapstructure:"flush_timeout"`
	BatchSize    int `mapstructure:"batch_size"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Output     string `mapstructure:"output"`
	File       string `mapstructure:"file"`
	MaxSize    int    `mapstructure:"max_size"`    // 日志文件最大大小(MB)
	MaxBackups int    `mapstructure:"max_backups"` // 保留的旧日志文件最大数量
	MaxAge     int    `mapstructure:"max_age"`     // 保留旧日志文件的最大天数
	Compress   bool   `mapstructure:"compress"`    // 是否压缩旧日志文件
}

// MonitoringConfig 监控配置
type MonitoringConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	Port       string `mapstructure:"port"`
	Path       string `mapstructure:"path"`
	Prometheus bool   `mapstructure:"prometheus"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	Mode        string   `mapstructure:"mode"`         // "none" 或 "token"
	JWTSecret   string   `mapstructure:"jwt_secret"`   // JWT密钥
	ValidTokens []string `mapstructure:"valid_tokens"` // 预设的有效token列表
	EnableTLS   bool     `mapstructure:"enable_tls"`   // 是否启用TLS
	CertFile    string   `mapstructure:"cert_file"`    // 证书文件路径
	KeyFile     string   `mapstructure:"key_file"`     // 私钥文件路径
}

// LoadConfig 加载配置文件
func LoadConfig(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	config := &Config{}
	if err := viper.Unmarshal(config); err != nil {
		return nil, err
	}

	setDefaults(config)
	return config, nil
}

// setDefaults 设置默认值
func setDefaults(config *Config) {
	// Buffer defaults
	if config.Buffer.MaxSize == 0 {
		config.Buffer.MaxSize = 1000
	}
	if config.Buffer.FlushTimeout == 0 {
		config.Buffer.FlushTimeout = 30
	}
	if config.Buffer.BatchSize == 0 {
		config.Buffer.BatchSize = 100
	}

	// Log defaults
	if config.Log.Level == "" {
		config.Log.Level = "info"
	}
	if config.Log.Format == "" {
		config.Log.Format = "json"
	}
	if config.Log.Output == "" {
		config.Log.Output = "stdout"
	}

	// Monitoring defaults
	if config.Monitoring.Port == "" {
		config.Monitoring.Port = ":9090"
	}
	if config.Monitoring.Path == "" {
		config.Monitoring.Path = "/metrics"
	}

	// Server defaults
	if config.Server.NodeID == "" {
		config.Server.NodeID = "node-1"
	}
	
	// Backup defaults
	if config.Backup.Interval == 0 {
		config.Backup.Interval = 3600 // 默认1小时
	}
}
