package config

import (
	"github.com/spf13/viper"
	"time"
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
	Pool       PoolConfig       `mapstructure:"pool"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	// 基础配置
	Mode     string `mapstructure:"mode"`     // Redis模式: standalone, sentinel, cluster
	Addr     string `mapstructure:"addr"`     // 单机模式地址
	Password string `mapstructure:"password"` // 密码
	DB       int    `mapstructure:"db"`       // 数据库编号（集群模式不支持）
	Bucket   string `mapstructure:"bucket"`   // 存储桶名称
	
	// 哨兵模式配置
	MasterName       string   `mapstructure:"master_name"`       // 主节点名称
	SentinelAddrs    []string `mapstructure:"sentinel_addrs"`    // 哨兵地址列表
	SentinelPassword string   `mapstructure:"sentinel_password"` // 哨兵密码
	
	// 集群模式配置
	ClusterAddrs []string `mapstructure:"cluster_addrs"` // 集群地址列表
	
	// 连接池配置
	PoolSize       int           `mapstructure:"pool_size"`        // 连接池大小
	MinIdleConns   int           `mapstructure:"min_idle_conns"`   // 最小空闲连接数
	MaxConnAge     time.Duration `mapstructure:"max_conn_age"`     // 连接最大生存时间
	PoolTimeout    time.Duration `mapstructure:"pool_timeout"`     // 获取连接超时
	IdleTimeout    time.Duration `mapstructure:"idle_timeout"`     // 空闲连接超时
	IdleCheckFreq  time.Duration `mapstructure:"idle_check_freq"`  // 空闲连接检查频率
	DialTimeout    time.Duration `mapstructure:"dial_timeout"`     // 连接超时
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`     // 读取超时
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`    // 写入超时
	MaxRetries     int           `mapstructure:"max_retries"`      // 最大重试次数
	MinRetryBackoff time.Duration `mapstructure:"min_retry_backoff"` // 最小重试间隔
	MaxRetryBackoff time.Duration `mapstructure:"max_retry_backoff"` // 最大重试间隔
	
	// 集群特定配置
	MaxRedirects   int  `mapstructure:"max_redirects"`   // 最大重定向次数
	ReadOnly       bool `mapstructure:"read_only"`       // 只读模式
	RouteByLatency bool `mapstructure:"route_by_latency"` // 按延迟路由
	RouteRandomly  bool `mapstructure:"route_randomly"`   // 随机路由
}

// MinioConfig MinIO配置
type MinioConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	Region          string `mapstructure:"region"`
	
	// HTTP连接池配置
	MaxIdleConns          int           `mapstructure:"max_idle_conns"`           // 最大空闲连接数
	MaxIdleConnsPerHost   int           `mapstructure:"max_idle_conns_per_host"`  // 每个主机最大空闲连接数
	MaxConnsPerHost       int           `mapstructure:"max_conns_per_host"`       // 每个主机最大连接数
	IdleConnTimeout       time.Duration `mapstructure:"idle_conn_timeout"`        // 空闲连接超时
	
	// 超时配置
	DialTimeout            time.Duration `mapstructure:"dial_timeout"`             // 连接超时
	TLSHandshakeTimeout    time.Duration `mapstructure:"tls_handshake_timeout"`    // TLS握手超时
	ResponseHeaderTimeout  time.Duration `mapstructure:"response_header_timeout"`  // 响应头超时
	ExpectContinueTimeout  time.Duration `mapstructure:"expect_continue_timeout"`  // Expect Continue超时
	
	// 重试和请求配置
	MaxRetries     int           `mapstructure:"max_retries"`      // 最大重试次数
	RetryDelay     time.Duration `mapstructure:"retry_delay"`      // 重试延迟
	RequestTimeout time.Duration `mapstructure:"request_timeout"`  // 请求超时
	
	// Keep-Alive配置
	KeepAlive         time.Duration `mapstructure:"keep_alive"`          // Keep-Alive时间
	DisableKeepAlive  bool          `mapstructure:"disable_keep_alive"`  // 禁用Keep-Alive
	DisableCompression bool         `mapstructure:"disable_compression"` // 禁用压缩
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
	// 基础配置
	BufferSize    int           `mapstructure:"buffer_size"`    // 缓冲区大小
	FlushInterval time.Duration `mapstructure:"flush_interval"` // 刷新间隔
	
	// 并发配置
	WorkerPoolSize   int           `mapstructure:"worker_pool_size"`   // 工作池大小
	TaskQueueSize    int           `mapstructure:"task_queue_size"`    // 任务队列大小
	BatchFlushSize   int           `mapstructure:"batch_flush_size"`   // 批量刷新大小
	EnableBatching   bool          `mapstructure:"enable_batching"`    // 启用批量处理
	FlushTimeout     time.Duration `mapstructure:"flush_timeout"`      // 刷新超时
	MaxRetries       int           `mapstructure:"max_retries"`        // 最大重试次数
	RetryDelay       time.Duration `mapstructure:"retry_delay"`        // 重试延迟
	
	// 性能优化配置
	EnableConcurrent bool `mapstructure:"enable_concurrent"` // 启用并发缓冲区
	EnableMetrics    bool `mapstructure:"enable_metrics"`    // 启用指标收集
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

// PoolConfig 连接池配置
type PoolConfig struct {
	Redis          RedisConfig  `mapstructure:"redis"`
	MinIO          MinioConfig  `mapstructure:"minio"`
	BackupMinIO    *MinioConfig `mapstructure:"backup_minio,omitempty"` // 可选的备份MinIO配置
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"` // 健康检查间隔
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
	if config.Buffer.BufferSize == 0 {
		config.Buffer.BufferSize = 1000
	}
	if config.Buffer.FlushTimeout == 0 {
		config.Buffer.FlushTimeout = 30 * time.Second
	}
	if config.Buffer.BatchFlushSize == 0 {
		config.Buffer.BatchFlushSize = 100
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
