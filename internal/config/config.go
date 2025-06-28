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
	Redis           RedisConfig           `yaml:"redis"`
	MinIO           MinioConfig           `yaml:"minio"`
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

	// Redis默认配置
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

	// MinIO默认配置
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
			Enabled:           true,
			RequestsPerMinute: 60, // 默认每分钟60个请求
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
