package config

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v2"
)

// Config 应用配置
type Config struct {
	Server          ServerConfig          `yaml:"server"`
	Network         NetworkConfig         `yaml:"network"`          // 新增网络配置
	Redis           RedisConfig           `yaml:"redis"`            // 保持向后兼容，但优先使用Network.Pools.Redis
	MinIO           MinioConfig           `yaml:"minio"`            // 保持向后兼容，但优先使用Network.Pools.MinIO
	Buffer          BufferConfig          `yaml:"buffer"`
	Backup          BackupConfig          `yaml:"backup"`
	Security        SecurityConfig        `yaml:"security"`
	Metrics         MetricsConfig         `yaml:"metrics"`
	Tables          TablesConfig          `yaml:"tables"`
	TableManagement TableManagementConfig `yaml:"table_management"`
}

// TableConfig 表级配置
type TableConfig struct {
	BufferSize    int               `yaml:"buffer_size"`
	FlushInterval time.Duration     `yaml:"flush_interval"`
	RetentionDays int               `yaml:"retention_days"`
	BackupEnabled bool              `yaml:"backup_enabled"`
	Properties    map[string]string `yaml:"properties"`
}

// TablesConfig 表配置管理
type TablesConfig struct {
	DefaultConfig TableConfig            `yaml:"default_config"`
	Tables        map[string]TableConfig `yaml:",inline"`
}

// TableManagementConfig 表管理配置
type TableManagementConfig struct {
	AutoCreateTables bool   `yaml:"auto_create_tables"`
	DefaultTable     string `yaml:"default_table"`
	MaxTables        int    `yaml:"max_tables"`
	TableNamePattern string `yaml:"table_name_pattern"`
	tableNameRegex   *regexp.Regexp
}

// ServerConfig 服务器配置
type ServerConfig struct {
	GrpcPort string `yaml:"grpc_port"`
	RestPort string `yaml:"rest_port"`
	NodeID   string `yaml:"node_id"`
}

// NetworkConfig 网络配置 - 统一管理所有网络相关配置
type NetworkConfig struct {
	Server ServerNetworkConfig `yaml:"server"` // 服务器网络配置
	Pools  PoolsConfig         `yaml:"pools"`  // 连接池配置
}

// ServerNetworkConfig 服务器网络配置
type ServerNetworkConfig struct {
	GRPC GRPCNetworkConfig `yaml:"grpc"` // gRPC服务器网络配置
	REST RESTNetworkConfig `yaml:"rest"` // REST服务器网络配置
}

// GRPCNetworkConfig gRPC服务器网络配置
type GRPCNetworkConfig struct {
	MaxConnections          int           `yaml:"max_connections"`           // 最大并发连接数
	ConnectionTimeout       time.Duration `yaml:"connection_timeout"`        // 连接超时
	StreamTimeout          time.Duration `yaml:"stream_timeout"`            // 流超时
	KeepAliveTime          time.Duration `yaml:"keep_alive_time"`           // 保活时间
	KeepAliveTimeout       time.Duration `yaml:"keep_alive_timeout"`        // 保活超时
	MaxConnectionIdle      time.Duration `yaml:"max_connection_idle"`       // 最大连接空闲时间
	MaxConnectionAge       time.Duration `yaml:"max_connection_age"`        // 最大连接存活时间
	MaxConnectionAgeGrace  time.Duration `yaml:"max_connection_age_grace"`  // 连接存活优雅期
	MaxSendMsgSize         int           `yaml:"max_send_msg_size"`         // 最大发送消息大小
	MaxRecvMsgSize         int           `yaml:"max_recv_msg_size"`         // 最大接收消息大小
}

// RESTNetworkConfig REST服务器网络配置
type RESTNetworkConfig struct {
	ReadTimeout        time.Duration `yaml:"read_timeout"`         // 读取超时
	WriteTimeout       time.Duration `yaml:"write_timeout"`        // 写入超时
	IdleTimeout        time.Duration `yaml:"idle_timeout"`         // 空闲超时
	ReadHeaderTimeout  time.Duration `yaml:"read_header_timeout"`  // 读取头超时
	MaxHeaderBytes     int           `yaml:"max_header_bytes"`     // 最大头字节数
	ShutdownTimeout    time.Duration `yaml:"shutdown_timeout"`     // 优雅关闭超时
}

// PoolsConfig 连接池配置
type PoolsConfig struct {
	Redis               EnhancedRedisConfig `yaml:"redis"`                // Redis连接池配置
	MinIO               EnhancedMinIOConfig `yaml:"minio"`                // MinIO连接池配置
	BackupMinIO         *EnhancedMinIOConfig `yaml:"backup_minio,omitempty"` // 备份MinIO连接池配置（可选）
	HealthCheckInterval time.Duration       `yaml:"health_check_interval"` // 健康检查间隔
}

// EnhancedRedisConfig 增强的Redis配置（包含所有pool配置参数）
type EnhancedRedisConfig struct {
	// 继承原有RedisConfig的所有字段
	Mode         string        `yaml:"mode"`
	Addr         string        `yaml:"addr"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	PoolSize     int           `yaml:"pool_size"`
	MinIdleConns int           `yaml:"min_idle_conns"`
	MaxConnAge   time.Duration `yaml:"max_conn_age"`
	PoolTimeout  time.Duration `yaml:"pool_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`

	// 新增的池配置字段
	IdleCheckFreq   time.Duration `yaml:"idle_check_freq"`    // 空闲连接检查频率
	MaxRetries      int           `yaml:"max_retries"`        // 最大重试次数
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff"`  // 最小重试间隔
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff"`  // 最大重试间隔

	// 集群模式配置
	ClusterAddrs   []string `yaml:"cluster_addrs"`    // 集群地址列表
	MaxRedirects   int      `yaml:"max_redirects"`    // 最大重定向次数
	ReadOnly       bool     `yaml:"read_only"`        // 只读模式
	RouteByLatency bool     `yaml:"route_by_latency"` // 按延迟路由
	RouteRandomly  bool     `yaml:"route_randomly"`   // 随机路由

	// 哨兵模式配置
	MasterName       string   `yaml:"master_name"`       // 主节点名称
	SentinelAddrs    []string `yaml:"sentinel_addrs"`    // 哨兵地址列表
	SentinelPassword string   `yaml:"sentinel_password"` // 哨兵密码
}

// EnhancedMinIOConfig 增强的MinIO配置（包含所有连接池参数）
type EnhancedMinIOConfig struct {
	// 基础连接配置
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	UseSSL          bool   `yaml:"use_ssl"`
	Region          string `yaml:"region"`
	Bucket          string `yaml:"bucket"`
	
	// HTTP连接池配置
	MaxIdleConns        int           `yaml:"max_idle_conns"`         // 最大空闲连接数
	MaxIdleConnsPerHost int           `yaml:"max_idle_conns_per_host"` // 每个主机的最大空闲连接数
	MaxConnsPerHost     int           `yaml:"max_conns_per_host"`     // 每个主机的最大连接数
	IdleConnTimeout     time.Duration `yaml:"idle_conn_timeout"`      // 空闲连接超时
	
	// 超时配置
	DialTimeout           time.Duration `yaml:"dial_timeout"`            // 连接超时
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout"`   // TLS握手超时
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"` // 响应头超时
	ExpectContinueTimeout time.Duration `yaml:"expect_continue_timeout"` // Expect: 100-continue超时
	
	// 重试和背压配置
	MaxRetries     int           `yaml:"max_retries"`     // 最大重试次数
	RetryDelay     time.Duration `yaml:"retry_delay"`     // 重试延迟
	RequestTimeout time.Duration `yaml:"request_timeout"` // 请求超时
	
	// 连接保活配置
	KeepAlive          time.Duration `yaml:"keep_alive"`           // TCP保活间隔
	DisableKeepAlive   bool          `yaml:"disable_keep_alive"`   // 禁用保活
	DisableCompression bool          `yaml:"disable_compression"`  // 禁用压缩
}

// RedisConfig Redis配置
type RedisConfig struct {
	Mode         string        `yaml:"mode"`
	Addr         string        `yaml:"addr"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	PoolSize     int           `yaml:"pool_size"`
	MinIdleConns int           `yaml:"min_idle_conns"`
	MaxConnAge   time.Duration `yaml:"max_conn_age"`
	PoolTimeout  time.Duration `yaml:"pool_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`

	// 集群模式配置
	ClusterAddrs []string `yaml:"cluster_addrs"`
	MaxRedirects int      `yaml:"max_redirects"`
	ReadOnly     bool     `yaml:"read_only"`

	// 哨兵模式配置
	MasterName       string   `yaml:"master_name"`
	SentinelAddrs    []string `yaml:"sentinel_addrs"`
	SentinelPassword string   `yaml:"sentinel_password"`
}

// MinioConfig MinIO配置
type MinioConfig struct {
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	UseSSL          bool   `yaml:"use_ssl"`
	Bucket          string `yaml:"bucket"`
}

// BufferConfig 缓冲区配置（保持向后兼容）
type BufferConfig struct {
	BufferSize    int           `yaml:"buffer_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
}

// BackupConfig 备份配置
type BackupConfig struct {
	Enabled  bool        `yaml:"enabled"`
	Interval int         `yaml:"interval"`
	MinIO    MinioConfig `yaml:"minio"`

	// 元数据备份配置
	Metadata MetadataBackupConfig `yaml:"metadata"`
}

// MetadataBackupConfig 元数据备份配置
type MetadataBackupConfig struct {
	Enabled       bool          `yaml:"enabled"`        // 是否启用元数据备份
	Interval      time.Duration `yaml:"interval"`       // 备份间隔
	RetentionDays int           `yaml:"retention_days"` // 保留天数
	Bucket        string        `yaml:"bucket"`         // 存储桶
}

// RateLimitTier 限流等级配置
type RateLimitTier struct {
	Name            string        `yaml:"name"`
	RequestsPerSec  float64       `yaml:"requests_per_sec"`
	BurstSize       int           `yaml:"burst_size"`
	Window          time.Duration `yaml:"window"`
	BackoffDuration time.Duration `yaml:"backoff_duration"`
}

// UnmarshalYAML 自定义YAML解析以处理time.Duration字段
func (r *RateLimitTier) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// 定义一个临时结构体，使用字符串类型来接收duration字段
	type RateLimitTierRaw struct {
		Name            string  `yaml:"name"`
		RequestsPerSec  float64 `yaml:"requests_per_sec"`
		BurstSize       int     `yaml:"burst_size"`
		Window          string  `yaml:"window"`
		BackoffDuration string  `yaml:"backoff_duration"`
	}
	
	var raw RateLimitTierRaw
	if err := unmarshal(&raw); err != nil {
		return err
	}
	
	// 设置非duration字段
	r.Name = raw.Name
	r.RequestsPerSec = raw.RequestsPerSec
	r.BurstSize = raw.BurstSize
	
	// 解析duration字段
	if raw.Window != "" {
		if window, err := time.ParseDuration(raw.Window); err != nil {
			return fmt.Errorf("invalid window duration '%s': %w", raw.Window, err)
		} else {
			r.Window = window
		}
	}
	
	if raw.BackoffDuration != "" {
		if backoff, err := time.ParseDuration(raw.BackoffDuration); err != nil {
			return fmt.Errorf("invalid backoff_duration '%s': %w", raw.BackoffDuration, err)
		} else {
			r.BackoffDuration = backoff
		}
	}
	
	return nil
}

// PathRateLimit 路径限流配置
type PathRateLimit struct {
	Pattern string `yaml:"pattern"`
	Tier    string `yaml:"tier"`
	Enabled bool   `yaml:"enabled"`
}

// SmartRateLimitConfig 智能限流器配置
type SmartRateLimitConfig struct {
	Enabled         bool              `yaml:"enabled"`
	DefaultTier     string            `yaml:"default_tier"`
	Tiers           []RateLimitTier   `yaml:"tiers"`
	PathLimits      []PathRateLimit   `yaml:"path_limits"`
	CleanupInterval time.Duration     `yaml:"cleanup_interval"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	Mode            string               `yaml:"mode"`
	JWTSecret       string               `yaml:"jwt_secret"`
	EnableTLS       bool                 `yaml:"enable_tls"`
	ValidTokens     []string             `yaml:"valid_tokens"`
	RateLimit       RateLimitConfig      `yaml:"rate_limit"`        // 传统限流配置（保持向后兼容）
	SmartRateLimit  SmartRateLimitConfig `yaml:"smart_rate_limit"`  // 智能限流器配置
}

// RateLimitConfig 速率限制配置（传统配置，保持向后兼容）
type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerMinute int  `yaml:"requests_per_minute"`
}

// MetricsConfig 指标配置
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    string `yaml:"port"`
	Path    string `yaml:"path"`
}

// GetTableConfig 获取指定表的配置，如果不存在则返回默认配置
func (c *Config) GetTableConfig(tableName string) *TableConfig {
	if tableConfig, exists := c.Tables.Tables[tableName]; exists {
		// 合并默认配置和表级配置
		config := c.Tables.DefaultConfig
		if tableConfig.BufferSize > 0 {
			config.BufferSize = tableConfig.BufferSize
		}
		if tableConfig.FlushInterval > 0 {
			config.FlushInterval = tableConfig.FlushInterval
		}
		if tableConfig.RetentionDays > 0 {
			config.RetentionDays = tableConfig.RetentionDays
		}
		// BackupEnabled 使用表级设置，如果未设置则使用默认值
		config.BackupEnabled = tableConfig.BackupEnabled

		// 合并属性
		if config.Properties == nil {
			config.Properties = make(map[string]string)
		}
		for k, v := range tableConfig.Properties {
			config.Properties[k] = v
		}

		return &config
	}

	// 返回默认配置的副本
	defaultConfig := c.Tables.DefaultConfig
	return &defaultConfig
}

// IsValidTableName 验证表名是否合法
func (c *Config) IsValidTableName(tableName string) bool {
	if c.TableManagement.tableNameRegex == nil {
		// 编译正则表达式
		regex, err := regexp.Compile(c.TableManagement.TableNamePattern)
		if err != nil {
			log.Printf("WARN: invalid table name pattern: %v, using default", err)
			regex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)
		}
		c.TableManagement.tableNameRegex = regex
	}

	return c.TableManagement.tableNameRegex.MatchString(tableName)
}

// GetDefaultTableName 获取默认表名
func (c *Config) GetDefaultTableName() string {
	if c.TableManagement.DefaultTable == "" {
		return "default"
	}
	return c.TableManagement.DefaultTable
}

// LoadConfig 从文件加载配置
func LoadConfig(configPath string) (*Config, error) {
	config := &Config{}

	// 设置默认值
	config.setDefaults()

	if configPath != "" && fileExists(configPath) {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// 使用环境变量覆盖配置
	config.overrideWithEnv()

	// 验证配置
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// setDefaults 设置默认配置值
func (c *Config) setDefaults() {
	// 服务器默认配置
	c.Server = ServerConfig{
		GrpcPort: ":8080",
		RestPort: ":8081",
		NodeID:   "node-1",
	}

	// 网络配置默认值 - 性能优化
	c.Network = NetworkConfig{
		Server: ServerNetworkConfig{
			GRPC: GRPCNetworkConfig{
				MaxConnections:         1000,
				ConnectionTimeout:      30 * time.Second,
				StreamTimeout:         60 * time.Second,
				KeepAliveTime:         30 * time.Second,
				KeepAliveTimeout:      5 * time.Second,
				MaxConnectionIdle:     15 * time.Minute,
				MaxConnectionAge:      30 * time.Minute,
				MaxConnectionAgeGrace: 5 * time.Second,
				MaxSendMsgSize:        4194304, // 4MB
				MaxRecvMsgSize:        4194304, // 4MB
			},
			REST: RESTNetworkConfig{
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      30 * time.Second,
				IdleTimeout:       60 * time.Second,
				ReadHeaderTimeout: 10 * time.Second,
				MaxHeaderBytes:    1048576, // 1MB
				ShutdownTimeout:   30 * time.Second,
			},
		},
		Pools: PoolsConfig{
			Redis: EnhancedRedisConfig{
				Mode:            "standalone",
				Addr:            "localhost:6379",
				Password:        "",
				DB:              0,
				PoolSize:        250,
				MinIdleConns:    25,
				MaxConnAge:      30 * time.Minute,
				PoolTimeout:     3 * time.Second,
				IdleTimeout:     5 * time.Minute,
				DialTimeout:     3 * time.Second,
				ReadTimeout:     2 * time.Second,
				WriteTimeout:    2 * time.Second,
				IdleCheckFreq:   time.Minute,
				MaxRetries:      3,
				MinRetryBackoff: 8 * time.Millisecond,
				MaxRetryBackoff: 512 * time.Millisecond,
				MaxRedirects:    8,
				ReadOnly:        false,
				RouteByLatency:  true,
				RouteRandomly:   false,
			},
			MinIO: EnhancedMinIOConfig{
				Endpoint:              "localhost:9000",
				AccessKeyID:           "minioadmin",
				SecretAccessKey:       "minioadmin",
				UseSSL:                false,
				Region:                "us-east-1",
				Bucket:                "olap-data",
				MaxIdleConns:          300,
				MaxIdleConnsPerHost:   150,
				MaxConnsPerHost:       300,
				IdleConnTimeout:       90 * time.Second,
				DialTimeout:           5 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxRetries:            3,
				RetryDelay:            100 * time.Millisecond,
				RequestTimeout:        60 * time.Second,
				KeepAlive:             30 * time.Second,
				DisableKeepAlive:      false,
				DisableCompression:    false,
			},
			HealthCheckInterval: 15 * time.Second,
		},
	}

	// Redis默认配置（保持向后兼容）
	c.Redis = RedisConfig{
		Mode:         "standalone",
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     100,
		MinIdleConns: 10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	// MinIO默认配置（保持向后兼容）
	c.MinIO = MinioConfig{
		Endpoint:        "localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		UseSSL:          false,
		Bucket:          "olap-data",
	}

	// 缓冲区默认配置（向后兼容）
	c.Buffer = BufferConfig{
		BufferSize:    1000,
		FlushInterval: 30 * time.Second,
	}

	// 备份默认配置
	c.Backup = BackupConfig{
		Enabled:  false,
		Interval: 3600,
		Metadata: MetadataBackupConfig{
			Enabled:       true,
			Interval:      30 * time.Minute, // 30分钟备份一次
			RetentionDays: 7,                // 保留7天
			Bucket:        "olap-metadata",  // 默认元数据存储桶
		},
	}

	// 安全默认配置
	c.Security = SecurityConfig{
		Mode:      "disabled",
		JWTSecret: "",
		EnableTLS: false,
		RateLimit: RateLimitConfig{
			Enabled:           false, // 默认禁用传统限流器，优先使用智能限流器
			RequestsPerMinute: 60, // 默认每分钟60个请求
		},
		SmartRateLimit: SmartRateLimitConfig{
			Enabled:         true, // 默认启用智能限流器
			DefaultTier:     "standard",
			CleanupInterval: 5 * time.Minute,
			Tiers:           []RateLimitTier{},
			PathLimits:      []PathRateLimit{},
		},
	}

	// 指标默认配置
	c.Metrics = MetricsConfig{
		Enabled: true,
		Port:    "9090",
		Path:    "/metrics",
	}

	// 表管理默认配置
	c.TableManagement = TableManagementConfig{
		AutoCreateTables: true,
		DefaultTable:     "default",
		MaxTables:        1000,
		TableNamePattern: `^[a-zA-Z][a-zA-Z0-9_]{0,63}$`,
	}

	// 表级默认配置
	c.Tables = TablesConfig{
		DefaultConfig: TableConfig{
			BufferSize:    1000,
			FlushInterval: 30 * time.Second,
			RetentionDays: 365,
			BackupEnabled: true,
			Properties:    make(map[string]string),
		},
		Tables: make(map[string]TableConfig),
	}
}

// overrideWithEnv 使用环境变量覆盖配置
func (c *Config) overrideWithEnv() {
	// Redis配置环境变量覆盖
	if redisHost := os.Getenv("REDIS_HOST"); redisHost != "" {
		redisPort := os.Getenv("REDIS_PORT")
		if redisPort == "" {
			redisPort = "6379"
		}
		c.Redis.Addr = redisHost + ":" + redisPort
	}
	if redisPassword := os.Getenv("REDIS_PASSWORD"); redisPassword != "" {
		c.Redis.Password = redisPassword
	}
	if redisDB := os.Getenv("REDIS_DB"); redisDB != "" {
		if db, err := fmt.Sscanf(redisDB, "%d", &c.Redis.DB); err == nil && db == 1 {
			// DB值已设置
		}
	}

	// MinIO配置环境变量覆盖
	if minioEndpoint := os.Getenv("MINIO_ENDPOINT"); minioEndpoint != "" {
		c.MinIO.Endpoint = minioEndpoint
	}
	if minioAccessKey := os.Getenv("MINIO_ACCESS_KEY"); minioAccessKey != "" {
		c.MinIO.AccessKeyID = minioAccessKey
	}
	if minioSecretKey := os.Getenv("MINIO_SECRET_KEY"); minioSecretKey != "" {
		c.MinIO.SecretAccessKey = minioSecretKey
	}
	if minioBucket := os.Getenv("MINIO_BUCKET"); minioBucket != "" {
		c.MinIO.Bucket = minioBucket
	}
	if minioUseSSL := os.Getenv("MINIO_USE_SSL"); minioUseSSL == "true" {
		c.MinIO.UseSSL = true
	}

	// 服务器配置环境变量覆盖
	if grpcPort := os.Getenv("GRPC_PORT"); grpcPort != "" {
		if grpcPort[0] != ':' {
			grpcPort = ":" + grpcPort
		}
		c.Server.GrpcPort = grpcPort
	}
	if restPort := os.Getenv("REST_PORT"); restPort != "" {
		if restPort[0] != ':' {
			restPort = ":" + restPort
		}
		c.Server.RestPort = restPort
	}

	// 备份配置环境变量覆盖
	if minioBackupEndpoint := os.Getenv("MINIO_BACKUP_ENDPOINT"); minioBackupEndpoint != "" {
		c.Backup.MinIO.Endpoint = minioBackupEndpoint
		c.Backup.Enabled = true
	}
	if minioBackupAccessKey := os.Getenv("MINIO_BACKUP_ACCESS_KEY"); minioBackupAccessKey != "" {
		c.Backup.MinIO.AccessKeyID = minioBackupAccessKey
	}
	if minioBackupSecretKey := os.Getenv("MINIO_BACKUP_SECRET_KEY"); minioBackupSecretKey != "" {
		c.Backup.MinIO.SecretAccessKey = minioBackupSecretKey
	}
	if minioBackupBucket := os.Getenv("MINIO_BACKUP_BUCKET"); minioBackupBucket != "" {
		c.Backup.MinIO.Bucket = minioBackupBucket
	}

	// 元数据备份配置环境变量覆盖
	if metadataBucket := os.Getenv("METADATA_BACKUP_BUCKET"); metadataBucket != "" {
		c.Backup.Metadata.Bucket = metadataBucket
	}

	// 认证配置环境变量覆盖
	if authMode := os.Getenv("AUTH_MODE"); authMode != "" {
		c.Security.Mode = authMode
	}
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		c.Security.JWTSecret = jwtSecret
	}
}

// validate 验证配置
func (c *Config) validate() error {
	// 验证服务器配置
	if c.Server.GrpcPort == "" {
		return fmt.Errorf("grpc_port is required")
	}
	if c.Server.RestPort == "" {
		return fmt.Errorf("rest_port is required")
	}

	// 验证Redis配置
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis addr is required")
	}

	// 验证MinIO配置
	if c.MinIO.Endpoint == "" {
		return fmt.Errorf("minio endpoint is required")
	}
	if c.MinIO.Bucket == "" {
		return fmt.Errorf("minio bucket is required")
	}

	// 验证表管理配置
	if c.TableManagement.MaxTables <= 0 {
		return fmt.Errorf("max_tables must be positive")
	}
	if c.TableManagement.TableNamePattern == "" {
		return fmt.Errorf("table_name_pattern is required")
	}

	// 验证默认表配置
	if c.Tables.DefaultConfig.BufferSize <= 0 {
		return fmt.Errorf("default buffer_size must be positive")
	}
	if c.Tables.DefaultConfig.FlushInterval <= 0 {
		return fmt.Errorf("default flush_interval must be positive")
	}

	// 如果有旧的buffer配置，将其映射到默认表配置
	if c.Buffer.BufferSize > 0 || c.Buffer.FlushInterval > 0 {
		log.Printf("INFO: migrating legacy buffer config to default table config")
		if c.Buffer.BufferSize > 0 {
			c.Tables.DefaultConfig.BufferSize = c.Buffer.BufferSize
		}
		if c.Buffer.FlushInterval > 0 {
			c.Tables.DefaultConfig.FlushInterval = c.Buffer.FlushInterval
		}
	}

	return nil
}

// fileExists 检查文件是否存在
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
