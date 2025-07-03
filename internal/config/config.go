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
	Server            ServerConfig            `yaml:"server"`
	Network           NetworkConfig           `yaml:"network"` // 新增网络配置
	Redis             RedisConfig             `yaml:"redis"`   // 保持向后兼容，但优先使用Network.Pools.Redis
	MinIO             MinioConfig             `yaml:"minio"`   // 保持向后兼容，但优先使用Network.Pools.MinIO
	Buffer            BufferConfig            `yaml:"buffer"`
	Backup            BackupConfig            `yaml:"backup"`
	Security          SecurityConfig          `yaml:"security"`
	RateLimiting      SmartRateLimitConfig    `yaml:"rate_limiting"`      // 智能限流配置
	QueryOptimization QueryOptimizationConfig `yaml:"query_optimization"` // 查询优化配置
	StorageEngine     StorageEngineConfig     `yaml:"storage_engine"`     // 存储引擎优化配置
	Auth              AuthConfig              `yaml:"auth"`               // 认证配置
	Metrics           MetricsConfig           `yaml:"metrics"`
	Monitoring        MonitoringConfig        `yaml:"monitoring"` // 监控配置
	Log               LogConfig               `yaml:"log"`        // 日志配置
	Tables            TablesConfig            `yaml:"tables"`
	TableManagement   TableManagementConfig   `yaml:"table_management"`
	System            SystemConfig            `yaml:"system"` // 系统配置
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
	MaxConnections        int           `yaml:"max_connections"`          // 最大并发连接数
	ConnectionTimeout     time.Duration `yaml:"connection_timeout"`       // 连接超时
	StreamTimeout         time.Duration `yaml:"stream_timeout"`           // 流超时
	KeepAliveTime         time.Duration `yaml:"keep_alive_time"`          // 保活时间
	KeepAliveTimeout      time.Duration `yaml:"keep_alive_timeout"`       // 保活超时
	MaxConnectionIdle     time.Duration `yaml:"max_connection_idle"`      // 最大连接空闲时间
	MaxConnectionAge      time.Duration `yaml:"max_connection_age"`       // 最大连接存活时间
	MaxConnectionAgeGrace time.Duration `yaml:"max_connection_age_grace"` // 连接存活优雅期
	MaxSendMsgSize        int           `yaml:"max_send_msg_size"`        // 最大发送消息大小
	MaxRecvMsgSize        int           `yaml:"max_recv_msg_size"`        // 最大接收消息大小
}

// RESTNetworkConfig REST服务器网络配置
type RESTNetworkConfig struct {
	ReadTimeout       time.Duration `yaml:"read_timeout"`        // 读取超时
	WriteTimeout      time.Duration `yaml:"write_timeout"`       // 写入超时
	IdleTimeout       time.Duration `yaml:"idle_timeout"`        // 空闲超时
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"` // 读取头超时
	MaxHeaderBytes    int           `yaml:"max_header_bytes"`    // 最大头字节数
	ShutdownTimeout   time.Duration `yaml:"shutdown_timeout"`    // 优雅关闭超时
}

// PoolsConfig 连接池配置
type PoolsConfig struct {
	Redis               EnhancedRedisConfig  `yaml:"redis"`                  // Redis连接池配置
	MinIO               EnhancedMinIOConfig  `yaml:"minio"`                  // MinIO连接池配置
	BackupMinIO         *EnhancedMinIOConfig `yaml:"backup_minio,omitempty"` // 备份MinIO连接池配置（可选）
	HealthCheckInterval time.Duration        `yaml:"health_check_interval"`  // 健康检查间隔
}

// EnhancedRedisConfig 增强的Redis配置（包含所有pool配置参数）
type EnhancedRedisConfig struct {
	// Redis服务开关
	Enabled bool `yaml:"enabled"` // 是否启用Redis服务

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
	IdleCheckFreq   time.Duration `yaml:"idle_check_freq"`   // 空闲连接检查频率
	MaxRetries      int           `yaml:"max_retries"`       // 最大重试次数
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff"` // 最小重试间隔
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff"` // 最大重试间隔

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
	MaxIdleConns        int           `yaml:"max_idle_conns"`          // 最大空闲连接数
	MaxIdleConnsPerHost int           `yaml:"max_idle_conns_per_host"` // 每个主机的最大空闲连接数
	MaxConnsPerHost     int           `yaml:"max_conns_per_host"`      // 每个主机的最大连接数
	IdleConnTimeout     time.Duration `yaml:"idle_conn_timeout"`       // 空闲连接超时

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
	KeepAlive          time.Duration `yaml:"keep_alive"`          // TCP保活间隔
	DisableKeepAlive   bool          `yaml:"disable_keep_alive"`  // 禁用保活
	DisableCompression bool          `yaml:"disable_compression"` // 禁用压缩
}

// RedisConfig Redis配置
type RedisConfig struct {
	Enabled      bool          `yaml:"enabled"` // 是否启用Redis服务
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

// SmartRateLimitConfig 智能限流器配置（旧版本，保持向后兼容）
type SmartRateLimitConfigOld struct {
	Enabled         bool            `yaml:"enabled"`
	DefaultTier     string          `yaml:"default_tier"`
	Tiers           []RateLimitTier `yaml:"tiers"`
	PathLimits      []PathRateLimit `yaml:"path_limits"`
	CleanupInterval time.Duration   `yaml:"cleanup_interval"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	Mode           string                  `yaml:"mode"`
	JWTSecret      string                  `yaml:"jwt_secret"`
	EnableTLS      bool                    `yaml:"enable_tls"`
	ValidTokens    []string                `yaml:"valid_tokens"`
	RateLimit      RateLimitConfig         `yaml:"rate_limit"`       // 传统限流配置（保持向后兼容）
	SmartRateLimit SmartRateLimitConfigOld `yaml:"smart_rate_limit"` // 智能限流器配置（旧版本）
}

// RateLimitConfig 速率限制配置（传统配置，保持向后兼容）
type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerMinute int  `yaml:"requests_per_minute"`
}

// MetricsConfig 指标配置
type MetricsConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Port       string `yaml:"port"`
	Path       string `yaml:"path"`
	Prometheus bool   `yaml:"prometheus"`
}

// MonitoringConfig 监控配置
type MonitoringConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Port       string `yaml:"port"`
	Path       string `yaml:"path"`
	Prometheus bool   `yaml:"prometheus"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	Output     string `yaml:"output"`
	Filename   string `yaml:"filename"`
	MaxSize    int    `yaml:"max_size"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAge     int    `yaml:"max_age"`
	Compress   bool   `yaml:"compress"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	EnableJWT        bool     `yaml:"enable_jwt"`
	JWTSecret        string   `yaml:"jwt_secret"`
	TokenExpiry      string   `yaml:"token_expiry"`
	EnableAPIKey     bool     `yaml:"enable_api_key"`
	APIKeys          []string `yaml:"api_keys"`
	SkipAuthPaths    []string `yaml:"skip_auth_paths"`
	RequireAuthPaths []string `yaml:"require_auth_paths"`
}

// QueryOptimizationConfig 查询优化配置
type QueryOptimizationConfig struct {
	QueryCache QueryCacheConfig `yaml:"query_cache"`
	FileCache  FileCacheConfig  `yaml:"file_cache"`
	DuckDB     DuckDBConfig     `yaml:"duckdb"`
}

// QueryCacheConfig 查询缓存配置
type QueryCacheConfig struct {
	Enabled           bool                     `yaml:"enabled"`
	RedisKeyPrefix    string                   `yaml:"redis_key_prefix"`
	DefaultTTL        time.Duration            `yaml:"default_ttl"`
	MaxCacheSize      int64                    `yaml:"max_cache_size"`
	EvictionPolicy    string                   `yaml:"eviction_policy"`
	CacheStrategies   map[string]time.Duration `yaml:"cache_strategies"`
	TableInvalidation TableInvalidationConfig  `yaml:"table_invalidation"`
	EnableStats       bool                     `yaml:"enable_stats"`
	StatsInterval     time.Duration            `yaml:"stats_interval"`
}

// TableInvalidationConfig 表级缓存失效配置
type TableInvalidationConfig struct {
	Enabled           bool          `yaml:"enabled"`
	InvalidationDelay time.Duration `yaml:"invalidation_delay"`
}

// FileCacheConfig 文件缓存配置
type FileCacheConfig struct {
	Enabled         bool                    `yaml:"enabled"`
	CacheDir        string                  `yaml:"cache_dir"`
	MaxCacheSize    int64                   `yaml:"max_cache_size"`
	MaxFileAge      time.Duration           `yaml:"max_file_age"`
	CleanupInterval time.Duration           `yaml:"cleanup_interval"`
	RedisIndex      FileCacheRedisConfig    `yaml:"redis_index"`
	Metadata        FileCacheMetadataConfig `yaml:"metadata"`
	EnableStats     bool                    `yaml:"enable_stats"`
	StatsInterval   time.Duration           `yaml:"stats_interval"`
}

// FileCacheRedisConfig 文件缓存Redis索引配置
type FileCacheRedisConfig struct {
	Enabled   bool          `yaml:"enabled"`
	KeyPrefix string        `yaml:"key_prefix"`
	IndexTTL  time.Duration `yaml:"index_ttl"`
}

// FileCacheMetadataConfig 文件缓存元数据配置
type FileCacheMetadataConfig struct {
	TrackAccessCount       bool `yaml:"track_access_count"`
	TrackCreationTime      bool `yaml:"track_creation_time"`
	EnableHashVerification bool `yaml:"enable_hash_verification"`
}

// DuckDBConfig DuckDB配置
type DuckDBConfig struct {
	Enabled            bool                        `yaml:"enabled"`
	PoolSize           int                         `yaml:"pool_size"`
	MaxIdleTime        time.Duration               `yaml:"max_idle_time"`
	ConnectionTimeout  time.Duration               `yaml:"connection_timeout"`
	Performance        DuckDBPerformanceConfig     `yaml:"performance"`
	PreparedStatements DuckDBPreparedStmtsConfig   `yaml:"prepared_statements"`
	ConnectionReuse    DuckDBConnectionReuseConfig `yaml:"connection_reuse"`
}

// DuckDBPerformanceConfig DuckDB性能配置
type DuckDBPerformanceConfig struct {
	MemoryLimit       string `yaml:"memory_limit"`
	Threads           int    `yaml:"threads"`
	EnableObjectCache bool   `yaml:"enable_object_cache"`
	TempDirectory     string `yaml:"temp_directory"`
}

// DuckDBPreparedStmtsConfig DuckDB预编译语句配置
type DuckDBPreparedStmtsConfig struct {
	Enabled                 bool `yaml:"enabled"`
	CacheSize               int  `yaml:"cache_size"`
	AutoPrepareAggregations bool `yaml:"auto_prepare_aggregations"`
}

// DuckDBConnectionReuseConfig DuckDB连接复用配置
type DuckDBConnectionReuseConfig struct {
	Enabled             bool          `yaml:"enabled"`
	MaxReuseCount       int           `yaml:"max_reuse_count"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
}

// RateLimitingTier 限流等级配置
type RateLimitingTier struct {
	RequestsPerSecond float64       `yaml:"requests_per_second"`
	BurstSize         int           `yaml:"burst_size"`
	WindowSize        time.Duration `yaml:"window_size"`
}

// RateLimitingPathRule 路径规则配置
type RateLimitingPathRule struct {
	Path   string `yaml:"path"`
	Method string `yaml:"method"`
	Tier   string `yaml:"tier"`
}

// RateLimitingResponse 限流响应配置
type RateLimitingResponse struct {
	IncludeTier       bool `yaml:"include_tier"`
	IncludeLimit      bool `yaml:"include_limit"`
	IncludeBurst      bool `yaml:"include_burst"`
	IncludeWindow     bool `yaml:"include_window"`
	IncludeRetryAfter bool `yaml:"include_retry_after"`
}

// SmartRateLimitConfig 智能限流器配置（重新定义以匹配新的配置结构）
type SmartRateLimitConfig struct {
	Enabled     bool                        `yaml:"enabled"`
	Tiers       map[string]RateLimitingTier `yaml:"tiers"`
	PathRules   []RateLimitingPathRule      `yaml:"path_rules"`
	DefaultTier string                      `yaml:"default_tier"`
	Response    RateLimitingResponse        `yaml:"response"`
}

// StorageEngineConfig 存储引擎优化配置
// 注意：这些高级存储优化功能当前已禁用以保持系统简单性
// 相关实现文件保留在 internal/storage/ 目录下供未来使用
// 包括: engine.go, index_system.go, memory.go, shard.go 等
type StorageEngineConfig struct {
	Enabled          bool                  `yaml:"enabled"`           // 当前固定为false
	AutoOptimization bool                  `yaml:"auto_optimization"` // 当前未使用
	OptimizeInterval time.Duration         `yaml:"optimize_interval"` // 当前未使用
	PerformanceMode  string                `yaml:"performance_mode"`  // 当前未使用
	EnableMonitoring bool                  `yaml:"enable_monitoring"` // 当前未使用
	EnableProfiling  bool                  `yaml:"enable_profiling"`  // 当前未使用
	Parquet          StorageParquetConfig  `yaml:"parquet"`           // 高级Parquet优化（未使用）
	Sharding         StorageShardingConfig `yaml:"sharding"`          // 智能分片优化（未使用）
	Indexing         StorageIndexingConfig `yaml:"indexing"`          // 高级索引系统（未使用）
	Memory           StorageMemoryConfig   `yaml:"memory"`            // 内存优化器（未使用）
}

// StorageParquetConfig Parquet存储优化配置
type StorageParquetConfig struct {
	DefaultCompression  string            `yaml:"default_compression"`
	DefaultPartition    string            `yaml:"default_partition"`
	AutoSelectStrategy  bool              `yaml:"auto_select_strategy"`
	CompressionAnalysis bool              `yaml:"compression_analysis"`
	MetadataIndexing    bool              `yaml:"metadata_indexing"`
	CustomStrategies    map[string]string `yaml:"custom_strategies"`
}

// StorageShardingConfig 智能分片优化配置
type StorageShardingConfig struct {
	DefaultStrategy      string  `yaml:"default_strategy"`
	AutoRebalance        bool    `yaml:"auto_rebalance"`
	HotColdSeparation    bool    `yaml:"hot_cold_separation"`
	LocalityOptimization bool    `yaml:"locality_optimization"`
	RebalanceThreshold   float64 `yaml:"rebalance_threshold"`
	MigrationLimit       int     `yaml:"migration_limit"`
}

// StorageIndexingConfig 索引系统优化配置
type StorageIndexingConfig struct {
	AutoIndexCreation   bool                   `yaml:"auto_index_creation"`
	IndexTypes          []string               `yaml:"index_types"`
	BloomFilterEnabled  bool                   `yaml:"bloom_filter_enabled"`
	MinMaxEnabled       bool                   `yaml:"minmax_enabled"`
	InvertedEnabled     bool                   `yaml:"inverted_enabled"`
	CompositeEnabled    bool                   `yaml:"composite_enabled"`
	MaintenanceInterval time.Duration          `yaml:"maintenance_interval"`
	CustomConfig        map[string]interface{} `yaml:"custom_config"`
}

// StorageMemoryConfig 内存优化配置
type StorageMemoryConfig struct {
	EnablePooling      bool           `yaml:"enable_pooling"`
	EnableZeroCopy     bool           `yaml:"enable_zero_copy"`
	BufferOptimization bool           `yaml:"buffer_optimization"`
	GCOptimization     bool           `yaml:"gc_optimization"`
	MaxMemoryUsage     int64          `yaml:"max_memory_usage"`
	MemoryPoolSizes    map[string]int `yaml:"memory_pool_sizes"`
	GCInterval         time.Duration  `yaml:"gc_interval"`
}

// SystemConfig 系统配置
type SystemConfig struct {
	MaxMemoryMB   int `yaml:"max_memory_mb"`  // 最大内存使用量（MB）
	MaxGoroutines int `yaml:"max_goroutines"` // 最大协程数量
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
				MaxConnections:        1000,
				ConnectionTimeout:     30 * time.Second,
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
				Enabled:         true,
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
		Enabled:      true,
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
			RequestsPerMinute: 60,    // 默认每分钟60个请求
		},
		SmartRateLimit: SmartRateLimitConfigOld{
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

	// 智能限流配置默认值（覆盖旧的配置）
	c.RateLimiting = SmartRateLimitConfig{
		Enabled: true,
		Tiers: map[string]RateLimitingTier{
			"health": {
				RequestsPerSecond: 200,
				BurstSize:         50,
				WindowSize:        time.Second,
			},
			"query": {
				RequestsPerSecond: 100,
				BurstSize:         30,
				WindowSize:        time.Second,
			},
			"write": {
				RequestsPerSecond: 80,
				BurstSize:         20,
				WindowSize:        time.Second,
			},
			"standard": {
				RequestsPerSecond: 50,
				BurstSize:         15,
				WindowSize:        time.Second,
			},
			"strict": {
				RequestsPerSecond: 20,
				BurstSize:         5,
				WindowSize:        time.Second,
			},
		},
		PathRules: []RateLimitingPathRule{
			{Path: "/health", Method: "GET", Tier: "health"},
			{Path: "/v1/health", Method: "GET", Tier: "health"},
			{Path: "/metrics", Method: "GET", Tier: "health"},
			{Path: "/v1/query", Method: "POST", Tier: "query"},
			{Path: "/v1/write", Method: "POST", Tier: "write"},
			{Path: "/v1/backup/trigger", Method: "POST", Tier: "strict"},
			{Path: "/v1/recover", Method: "POST", Tier: "strict"},
		},
		DefaultTier: "standard",
		Response: RateLimitingResponse{
			IncludeTier:       true,
			IncludeLimit:      true,
			IncludeBurst:      true,
			IncludeWindow:     true,
			IncludeRetryAfter: true,
		},
	}

	// 查询优化配置默认值
	c.QueryOptimization = QueryOptimizationConfig{
		QueryCache: QueryCacheConfig{
			Enabled:        true,
			RedisKeyPrefix: "qcache:",
			DefaultTTL:     time.Hour,
			MaxCacheSize:   209715200, // 200MB
			EvictionPolicy: "lru",
			CacheStrategies: map[string]time.Duration{
				"simple_select": 2 * time.Hour,
				"count_query":   time.Hour,
				"aggregation":   30 * time.Minute,
				"join_query":    15 * time.Minute,
				"complex_query": 10 * time.Minute,
			},
			TableInvalidation: TableInvalidationConfig{
				Enabled:           true,
				InvalidationDelay: 5 * time.Second,
			},
			EnableStats:   true,
			StatsInterval: time.Minute,
		},
		FileCache: FileCacheConfig{
			Enabled:         true,
			CacheDir:        "/tmp/miniodb_cache",
			MaxCacheSize:    1073741824, // 1GB
			MaxFileAge:      4 * time.Hour,
			CleanupInterval: 15 * time.Minute,
			RedisIndex: FileCacheRedisConfig{
				Enabled:   true,
				KeyPrefix: "fcache:",
				IndexTTL:  24 * time.Hour,
			},
			Metadata: FileCacheMetadataConfig{
				TrackAccessCount:       true,
				TrackCreationTime:      true,
				EnableHashVerification: true,
			},
			EnableStats:   true,
			StatsInterval: time.Minute,
		},
		DuckDB: DuckDBConfig{
			Enabled:           true,
			PoolSize:          5,
			MaxIdleTime:       30 * time.Minute,
			ConnectionTimeout: 30 * time.Second,
			Performance: DuckDBPerformanceConfig{
				MemoryLimit:       "1GB",
				Threads:           4,
				EnableObjectCache: true,
				TempDirectory:     "/tmp/duckdb",
			},
			PreparedStatements: DuckDBPreparedStmtsConfig{
				Enabled:                 true,
				CacheSize:               100,
				AutoPrepareAggregations: true,
			},
			ConnectionReuse: DuckDBConnectionReuseConfig{
				Enabled:             true,
				MaxReuseCount:       1000,
				HealthCheckInterval: 5 * time.Minute,
			},
		},
	}

	// 认证配置默认值
	c.Auth = AuthConfig{
		EnableJWT:    true,
		JWTSecret:    "your-super-secret-jwt-key-change-this-in-production",
		TokenExpiry:  "24h",
		EnableAPIKey: true,
		APIKeys: []string{
			"api-key-1234567890abcdef",
			"api-key-0987654321fedcba",
		},
		SkipAuthPaths: []string{
			"/health",
			"/metrics",
			"/v1/health",
		},
		RequireAuthPaths: []string{
			"/v1/write",
			"/v1/query",
			"/v1/backup/trigger",
			"/v1/recover",
		},
	}

	// 监控配置默认值
	c.Monitoring = MonitoringConfig{
		Enabled:    true,
		Port:       ":8082",
		Path:       "/metrics",
		Prometheus: true,
	}

	// 日志配置默认值
	c.Log = LogConfig{
		Level:      "info",
		Format:     "json",
		Output:     "both",
		Filename:   "logs/minIODB.log",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}

	// 存储引擎优化配置默认值 - 当前禁用以保持系统简单性
	c.StorageEngine = StorageEngineConfig{
		Enabled:          false, // 禁用高级存储引擎功能
		AutoOptimization: false, // 禁用自动优化
		OptimizeInterval: 30 * time.Minute,
		PerformanceMode:  "balanced",
		EnableMonitoring: false, // 禁用监控
		EnableProfiling:  false,
		Parquet: StorageParquetConfig{
			DefaultCompression:  "zstd",
			DefaultPartition:    "analytical",
			AutoSelectStrategy:  true,
			CompressionAnalysis: true,
			MetadataIndexing:    true,
			CustomStrategies: map[string]string{
				"analytics_workload": "compression_optimized",
				"streaming_workload": "streaming",
				"mixed_workload":     "analytical",
			},
		},
		Sharding: StorageShardingConfig{
			DefaultStrategy:      "hash_uniform",
			AutoRebalance:        true,
			HotColdSeparation:    true,
			LocalityOptimization: true,
			RebalanceThreshold:   0.8,
			MigrationLimit:       3,
		},
		Indexing: StorageIndexingConfig{
			AutoIndexCreation:   true,
			IndexTypes:          []string{"bloom", "minmax", "inverted", "bitmap", "composite"},
			BloomFilterEnabled:  true,
			MinMaxEnabled:       true,
			InvertedEnabled:     true,
			CompositeEnabled:    true,
			MaintenanceInterval: time.Hour,
			CustomConfig: map[string]interface{}{
				"bloom_filter_fpp":             0.01,
				"inverted_min_term_length":     2,
				"bitmap_cardinality_threshold": 1000,
			},
		},
		Memory: StorageMemoryConfig{
			EnablePooling:      true,
			EnableZeroCopy:     true,
			BufferOptimization: true,
			GCOptimization:     true,
			MaxMemoryUsage:     2147483648, // 2GB
			MemoryPoolSizes: map[string]int{
				"small":  1024,    // 1KB
				"medium": 16384,   // 16KB
				"large":  262144,  // 256KB
				"xlarge": 1048576, // 1MB
			},
			GCInterval: 5 * time.Minute,
		},
	}
}

// overrideWithEnv 使用环境变量覆盖配置
func (c *Config) overrideWithEnv() {
	// Redis配置环境变量覆盖 (同时更新新旧配置以确保兼容性)
	if redisHost := os.Getenv("REDIS_HOST"); redisHost != "" {
		redisPort := os.Getenv("REDIS_PORT")
		if redisPort == "" {
			redisPort = "6379"
		}
		redisAddr := redisHost + ":" + redisPort
		c.Redis.Addr = redisAddr               // 保持向后兼容
		c.Network.Pools.Redis.Addr = redisAddr // 更新新的配置
	}
	if redisPassword := os.Getenv("REDIS_PASSWORD"); redisPassword != "" {
		c.Redis.Password = redisPassword               // 保持向后兼容
		c.Network.Pools.Redis.Password = redisPassword // 更新新的配置
	}
	if redisDB := os.Getenv("REDIS_DB"); redisDB != "" {
		if db, err := fmt.Sscanf(redisDB, "%d", &c.Redis.DB); err == nil && db == 1 {
			// DB值已设置
		}
		// 同时更新新配置
		c.Network.Pools.Redis.DB = c.Redis.DB
	}

	// MinIO配置环境变量覆盖 (同时更新新旧配置以确保兼容性)
	if minioEndpoint := os.Getenv("MINIO_ENDPOINT"); minioEndpoint != "" {
		c.MinIO.Endpoint = minioEndpoint               // 保持向后兼容
		c.Network.Pools.MinIO.Endpoint = minioEndpoint // 更新新的配置
	}
	if minioAccessKey := os.Getenv("MINIO_ACCESS_KEY"); minioAccessKey != "" {
		c.MinIO.AccessKeyID = minioAccessKey               // 保持向后兼容
		c.Network.Pools.MinIO.AccessKeyID = minioAccessKey // 更新新的配置
	}
	if minioSecretKey := os.Getenv("MINIO_SECRET_KEY"); minioSecretKey != "" {
		c.MinIO.SecretAccessKey = minioSecretKey               // 保持向后兼容
		c.Network.Pools.MinIO.SecretAccessKey = minioSecretKey // 更新新的配置
	}
	if minioBucket := os.Getenv("MINIO_BUCKET"); minioBucket != "" {
		c.MinIO.Bucket = minioBucket               // 保持向后兼容
		c.Network.Pools.MinIO.Bucket = minioBucket // 更新新的配置
	}
	if minioUseSSL := os.Getenv("MINIO_USE_SSL"); minioUseSSL == "true" {
		c.MinIO.UseSSL = true               // 保持向后兼容
		c.Network.Pools.MinIO.UseSSL = true // 更新新的配置
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
