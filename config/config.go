package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	System            SystemConfig            `yaml:"system"`        // 系统配置
	Compaction        CompactionConfig        `yaml:"compaction"`    // Compaction 配置
	FileMetadata      FileMetadataConfig      `yaml:"file_metadata"` // 文件元数据配置
	Coordinator       CoordinatorConfig       `yaml:"coordinator"`   // 协调器配置
	Subscription      SubscriptionConfig      `yaml:"subscription"`  // 数据订阅配置
	SLA               SLAConfig               `yaml:"sla"`           // SLA监控配置
	Benchmark         BenchmarkConfig         `yaml:"benchmark"`     // 基准测试配置
	Swagger           SwaggerConfig           `yaml:"swagger"`       // Swagger文档配置
	Dashboard         DashboardConfig         `yaml:"dashboard"`     // Dashboard配置
}

type DashboardConfig struct {
	Enabled               bool          `yaml:"enabled"`
	Port                  string        `yaml:"port"` // Deprecated: 融合架构下不再使用，Dashboard 挂载到 REST Server 端口
	BasePath              string        `yaml:"base_path"`
	CoreEndpoint          string        `yaml:"core_endpoint"`      // Deprecated: 融合架构下不再使用，Dashboard 直接持有 service 依赖
	CoreGRPCEndpoint      string        `yaml:"core_grpc_endpoint"` // Deprecated: 融合架构下不再使用，Dashboard 直接持有 service 依赖
	MetricsScrapeInterval time.Duration `yaml:"metrics_scrape_interval"`
	LogDir                string        `yaml:"log_dir"`
	BackupDir             string        `yaml:"backup_dir"`
}

// TableConfig 表级配置
type TableConfig struct {
	BufferSize     int               `yaml:"buffer_size"`
	FlushInterval  time.Duration     `yaml:"flush_interval"`
	RetentionDays  int               `yaml:"retention_days"`
	BackupEnabled  bool              `yaml:"backup_enabled"`
	Properties     map[string]string `yaml:"properties"`
	IDStrategy     string            `yaml:"id_strategy" json:"id_strategy"`           // ID生成策略: uuid, snowflake, custom, user_provided
	IDPrefix       string            `yaml:"id_prefix" json:"id_prefix"`               // ID前缀（用于custom和snowflake策略）
	AutoGenerateID bool              `yaml:"auto_generate_id" json:"auto_generate_id"` // 是否自动生成ID（未提供时）
	IDValidation   IDValidationRules `yaml:"id_validation" json:"id_validation"`       // ID验证规则
}

// IDValidationRules ID验证规则
type IDValidationRules struct {
	MaxLength    int    `yaml:"max_length" json:"max_length"`       // 最大长度（默认255）
	Pattern      string `yaml:"pattern" json:"pattern"`             // 正则验证模式
	AllowedChars string `yaml:"allowed_chars" json:"allowed_chars"` // 允许的字符集
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
	Failover            FailoverConfig       `yaml:"failover"`               // 故障切换配置
	HealthCheckInterval time.Duration        `yaml:"health_check_interval"`  // 健康检查间隔
}

// FailoverConfig 故障切换配置
type FailoverConfig struct {
	Enabled             bool            `yaml:"enabled"`               // 是否启用故障切换
	HealthCheckInterval time.Duration   `yaml:"health_check_interval"` // 健康检查间隔
	AsyncSync           AsyncSyncConfig `yaml:"async_sync"`            // 异步同步配置
}

// AsyncSyncConfig 异步同步配置
type AsyncSyncConfig struct {
	QueueSize     int           `yaml:"queue_size"`     // 队列大小
	WorkerCount   int           `yaml:"worker_count"`   // 并发工作器数量
	RetryTimes    int           `yaml:"retry_times"`    // 重试次数
	RetryInterval time.Duration `yaml:"retry_interval"` // 重试间隔
	SyncTimeout   time.Duration `yaml:"sync_timeout"`   // 同步超时
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

// BufferConfig 缓冲区配置
type BufferConfig struct {
	BufferSize      int           `yaml:"buffer_size"`
	FlushInterval   time.Duration `yaml:"flush_interval"`
	WorkerPoolSize  int           `yaml:"worker_pool_size"`
	TaskQueueSize   int           `yaml:"task_queue_size"`
	BatchFlushSize  int           `yaml:"batch_flush_size"`
	EnableBatching  bool          `yaml:"enable_batching"`
	FlushTimeout    time.Duration `yaml:"flush_timeout"`
	MaxRetries      int           `yaml:"max_retries"`
	RetryDelay      time.Duration `yaml:"retry_delay"`
	TempDir         string        `yaml:"temp_dir"`          // 临时文件目录
	ParquetRowGroup int64         `yaml:"parquet_row_group"` // Parquet row group 大小
	DefaultBucket   string        `yaml:"default_bucket"`    // 默认存储桶

	// WAL 配置
	WAL WALConfig `yaml:"wal"`
}

// WALConfig WAL 配置
type WALConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Dir         string `yaml:"dir"`
	SyncOnWrite bool   `yaml:"sync_on_write"`
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

// CORSConfig CORS 跨域配置
type CORSConfig struct {
	AllowedOrigins   []string `yaml:"allowed_origins"`   // 允许的来源白名单，空列表表示不允许任何跨域请求
	AllowMethods     []string `yaml:"allow_methods"`     // 允许的 HTTP 方法
	AllowHeaders     []string `yaml:"allow_headers"`     // 允许的请求头
	AllowCredentials bool     `yaml:"allow_credentials"` // 是否允许携带凭证
	MaxAge           int      `yaml:"max_age"`           // 预检请求缓存时间（秒）
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	Mode           string                  `yaml:"mode"`
	JWTSecret      string                  `yaml:"jwt_secret"`
	EnableTLS      bool                    `yaml:"enable_tls"`
	ValidTokens    []string                `yaml:"valid_tokens"`
	RateLimit      RateLimitConfig         `yaml:"rate_limit"`       // 传统限流配置（保持向后兼容）
	SmartRateLimit SmartRateLimitConfigOld `yaml:"smart_rate_limit"` // 智能限流器配置（旧版本）
	CORS           CORSConfig              `yaml:"cors"`             // CORS 跨域配置
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

// SLAConfig SLA监控配置
type SLAConfig struct {
	Enabled          bool          `yaml:"enabled"`
	QueryLatencyP50  time.Duration `yaml:"query_latency_p50"`
	QueryLatencyP95  time.Duration `yaml:"query_latency_p95"`
	QueryLatencyP99  time.Duration `yaml:"query_latency_p99"`
	CacheHitRate     float64       `yaml:"cache_hit_rate"`
	FilePruneRate    float64       `yaml:"file_prune_rate"`
	WriteLatencyP95  time.Duration `yaml:"write_latency_p95"`
	AlertOnViolation bool          `yaml:"alert_on_violation"`
}

// BenchmarkConfig 基准测试配置
type BenchmarkConfig struct {
	Enabled     bool          `yaml:"enabled"`
	AutoRun     bool          `yaml:"auto_run"`
	RunInterval time.Duration `yaml:"run_interval"`
	Timeout     time.Duration `yaml:"timeout"`
	MaxRetries  int           `yaml:"max_retries"`
}

// SwaggerConfig Swagger文档配置
type SwaggerConfig struct {
	Enabled     bool   `yaml:"enabled"`     // 是否启用Swagger UI
	Host        string `yaml:"host"`        // API主机地址
	BasePath    string `yaml:"base_path"`   // API基础路径
	Title       string `yaml:"title"`       // API标题
	Description string `yaml:"description"` // API描述
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

// APIKeyPair API凭证对（包含Key和Secret）
type APIKeyPair struct {
	Key         string `yaml:"key"`
	Secret      string `yaml:"secret"`
	Role        string `yaml:"role" json:"role"`                 // 预留字段，当前默认 "root"（全部权限）
	DisplayName string `yaml:"display_name" json:"display_name"` // 显示名称
}

// AuthConfig 认证配置
type AuthConfig struct {
	TokenExpiry      string       `yaml:"token_expiry"`
	APIKeyPairs      []APIKeyPair `yaml:"api_key_pairs"` // key 为 JWT payload user_id，secret 为该用户的 HMAC 签名密钥
	SkipAuthPaths    []string     `yaml:"skip_auth_paths"`
	RequireAuthPaths []string     `yaml:"require_auth_paths"`
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

// CompactionConfig Compaction 配置
type CompactionConfig struct {
	Enabled           bool          `yaml:"enabled"`              // 是否启用 Compaction
	TargetFileSize    int64         `yaml:"target_file_size"`     // 目标文件大小 (bytes)
	MinFilesToCompact int           `yaml:"min_files_to_compact"` // 触发 Compaction 的最小文件数
	MaxFilesToCompact int           `yaml:"max_files_to_compact"` // 单次 Compaction 的最大文件数
	CooldownPeriod    time.Duration `yaml:"cooldown_period"`      // 文件创建后多久才能被 Compaction
	CheckInterval     time.Duration `yaml:"check_interval"`       // 检查 Compaction 的间隔
	TempDir           string        `yaml:"temp_dir"`             // 临时目录
	CompressionType   string        `yaml:"compression_type"`     // 压缩类型 (snappy, zstd, gzip)
	MaxRowsPerFile    int64         `yaml:"max_rows_per_file"`    // 每个文件最大行数
}

// FileMetadataConfig 文件元数据配置
type FileMetadataConfig struct {
	KeyPrefix        string        `yaml:"key_prefix"`         // Redis key 前缀
	TTL              time.Duration `yaml:"ttl"`                // 元数据 TTL
	EnableSidecar    bool          `yaml:"enable_sidecar"`     // 是否启用 MinIO sidecar
	RedisPingTimeout time.Duration `yaml:"redis_ping_timeout"` // Redis ping 超时
}

// CoordinatorConfig 协调器配置
type CoordinatorConfig struct {
	HashRingReplicas        int           `yaml:"hash_ring_replicas"`        // 一致性哈希虚拟节点数
	WriteTimeout            time.Duration `yaml:"write_timeout"`             // 写超时
	DistributedQueryTimeout time.Duration `yaml:"distributed_query_timeout"` // 分布式查询超时
	RemoteQueryTimeout      time.Duration `yaml:"remote_query_timeout"`      // 远程查询超时
	NodeMonitorInterval     time.Duration `yaml:"node_monitor_interval"`     // 节点监控间隔
}

// SubscriptionConfig 数据订阅配置
type SubscriptionConfig struct {
	Enabled bool                    `yaml:"enabled"` // 是否启用订阅
	Redis   RedisSubscriptionConfig `yaml:"redis"`   // Redis 订阅配置
	Kafka   KafkaSubscriptionConfig `yaml:"kafka"`   // Kafka 订阅配置
}

// RedisSubscriptionConfig Redis 订阅配置（使用 Redis Streams）
type RedisSubscriptionConfig struct {
	Enabled       bool          `yaml:"enabled"`        // 是否启用 Redis 订阅
	StreamPrefix  string        `yaml:"stream_prefix"`  // Stream key 前缀
	ConsumerGroup string        `yaml:"consumer_group"` // 消费者组名称
	ConsumerName  string        `yaml:"consumer_name"`  // 消费者名称（可选，默认自动生成）
	BatchSize     int           `yaml:"batch_size"`     // 批量读取大小
	BlockTimeout  time.Duration `yaml:"block_timeout"`  // 阻塞读取超时
	MaxRetries    int           `yaml:"max_retries"`    // 最大重试次数
	RetryDelay    time.Duration `yaml:"retry_delay"`    // 重试延迟
	MaxLen        int64         `yaml:"max_len"`        // Stream 最大长度（0 表示不限制）
	AutoAck       bool          `yaml:"auto_ack"`       // 是否自动确认
}

// KafkaSubscriptionConfig Kafka 订阅配置
type KafkaSubscriptionConfig struct {
	Enabled         bool          `yaml:"enabled"`          // 是否启用 Kafka 订阅
	Brokers         []string      `yaml:"brokers"`          // Kafka broker 地址列表
	TopicPrefix     string        `yaml:"topic_prefix"`     // Topic 前缀
	ConsumerGroup   string        `yaml:"consumer_group"`   // 消费者组名称
	BatchSize       int           `yaml:"batch_size"`       // 批量读取大小
	BatchTimeout    time.Duration `yaml:"batch_timeout"`    // 批量超时
	MaxRetries      int           `yaml:"max_retries"`      // 最大重试次数
	RetryBackoff    time.Duration `yaml:"retry_backoff"`    // 重试退避时间
	SessionTimeout  time.Duration `yaml:"session_timeout"`  // 会话超时
	HeartbeatPeriod time.Duration `yaml:"heartbeat_period"` // 心跳周期
	AutoCommit      bool          `yaml:"auto_commit"`      // 是否自动提交 offset
	CommitInterval  time.Duration `yaml:"commit_interval"`  // 提交间隔
	StartOffset     string        `yaml:"start_offset"`     // 起始 offset: earliest, latest
	// TLS 配置
	UseTLS   bool   `yaml:"use_tls"`   // 是否使用 TLS
	CertFile string `yaml:"cert_file"` // 证书文件路径
	KeyFile  string `yaml:"key_file"`  // 密钥文件路径
	CAFile   string `yaml:"ca_file"`   // CA 证书文件路径
	// SASL 认证配置
	UseSASL       bool   `yaml:"use_sasl"`       // 是否使用 SASL 认证
	SASLUsername  string `yaml:"sasl_username"`  // SASL 用户名
	SASLPassword  string `yaml:"sasl_password"`  // SASL 密码
	SASLMechanism string `yaml:"sasl_mechanism"` // SASL 机制: plain, scram-sha-256, scram-sha-512
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
			// 使用默认的表名验证模式
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

	// 网络配置默认值
	c.Network = NetworkConfig{
		Server: ServerNetworkConfig{
			GRPC: GRPCNetworkConfig{
				MaxConnections:        DefaultGRPCMaxConnections,
				ConnectionTimeout:     DefaultGRPCConnectionTimeout,
				StreamTimeout:         DefaultGRPCStreamTimeout,
				KeepAliveTime:         DefaultGRPCKeepAliveTime,
				KeepAliveTimeout:      DefaultGRPCKeepAliveTimeout,
				MaxConnectionIdle:     DefaultGRPCMaxConnectionIdle,
				MaxConnectionAge:      DefaultGRPCMaxConnectionAge,
				MaxConnectionAgeGrace: DefaultGRPCMaxConnectionAgeGrace,
				MaxSendMsgSize:        DefaultGRPCMaxSendMsgSize,
				MaxRecvMsgSize:        DefaultGRPCMaxRecvMsgSize,
			},
			REST: RESTNetworkConfig{
				ReadTimeout:       DefaultRESTReadTimeout,
				WriteTimeout:      DefaultRESTWriteTimeout,
				IdleTimeout:       DefaultRESTIdleTimeout,
				ReadHeaderTimeout: DefaultRESTReadHeaderTimeout,
				MaxHeaderBytes:    DefaultRESTMaxHeaderBytes,
				ShutdownTimeout:   DefaultRESTShutdownTimeout,
			},
		},
		Pools: PoolsConfig{
			Redis: EnhancedRedisConfig{
				Enabled:         true,
				Mode:            "standalone",
				Addr:            "localhost:6379",
				Password:        "",
				DB:              0,
				PoolSize:        DefaultRedisPoolSize,
				MinIdleConns:    DefaultRedisMinIdleConns,
				MaxConnAge:      DefaultRedisMaxConnAge,
				PoolTimeout:     DefaultRedisPoolTimeout,
				IdleTimeout:     DefaultRedisIdleTimeout,
				DialTimeout:     DefaultRedisDialTimeout,
				ReadTimeout:     DefaultRedisReadTimeout,
				WriteTimeout:    DefaultRedisWriteTimeout,
				IdleCheckFreq:   DefaultRedisIdleCheckFreq,
				MaxRetries:      DefaultRedisMaxRetries,
				MinRetryBackoff: DefaultRedisMinRetryBackoff,
				MaxRetryBackoff: DefaultRedisMaxRetryBackoff,
				MaxRedirects:    DefaultRedisMaxRedirects,
				ReadOnly:        DefaultRedisReadOnly,
				RouteByLatency:  DefaultRedisRouteByLatency,
				RouteRandomly:   DefaultRedisRouteRandomly,
			},
			MinIO: EnhancedMinIOConfig{
				Endpoint:              "localhost:9000",
				AccessKeyID:           "minioadmin",
				SecretAccessKey:       "minioadmin",
				UseSSL:                false,
				Region:                "us-east-1",
				Bucket:                "olap-data",
				MaxIdleConns:          DefaultMinIOMaxIdleConns,
				MaxIdleConnsPerHost:   DefaultMinIOMaxIdleConnsPerHost,
				MaxConnsPerHost:       DefaultMinIOMaxConnsPerHost,
				IdleConnTimeout:       DefaultMinIOIdleConnTimeout,
				DialTimeout:           DefaultMinIODialTimeout,
				TLSHandshakeTimeout:   DefaultMinIOTLSHandshakeTimeout,
				ResponseHeaderTimeout: DefaultMinIOResponseHeaderTimeout,
				ExpectContinueTimeout: DefaultMinIOExpectContinueTimeout,
				MaxRetries:            DefaultMinIOMaxRetries,
				RetryDelay:            DefaultMinIORetryDelay,
				RequestTimeout:        DefaultMinIORequestTimeout,
				KeepAlive:             DefaultMinIOKeepAlive,
				DisableKeepAlive:      DefaultMinIODisableKeepAlive,
				DisableCompression:    DefaultMinIODisableCompression,
			},
			Failover: FailoverConfig{
				Enabled:             true,
				HealthCheckInterval: DefaultFailoverHealthCheckInterval,
				AsyncSync: AsyncSyncConfig{
					QueueSize:     DefaultAsyncSyncQueueSize,
					WorkerCount:   DefaultAsyncSyncWorkerCount,
					RetryTimes:    DefaultAsyncSyncRetryTimes,
					RetryInterval: DefaultAsyncSyncRetryInterval,
					SyncTimeout:   DefaultAsyncSyncTimeout,
				},
			},
			HealthCheckInterval: DefaultPoolsHealthCheckInterval,
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

	// 缓冲区默认配置
	c.Buffer = BufferConfig{
		BufferSize:      1000,
		FlushInterval:   30 * time.Second,
		WorkerPoolSize:  DefaultBufferWorkerPoolSize,
		TaskQueueSize:   DefaultBufferTaskQueueSize,
		BatchFlushSize:  DefaultBufferBatchFlushSize,
		EnableBatching:  DefaultBufferEnableBatching,
		FlushTimeout:    DefaultBufferFlushTimeout,
		MaxRetries:      DefaultBufferMaxRetries,
		RetryDelay:      DefaultBufferRetryDelay,
		TempDir:         DefaultBufferTempDir,
		ParquetRowGroup: DefaultBufferParquetRowGroup,
		DefaultBucket:   DefaultBufferDefaultBucket,
		WAL: WALConfig{
			Enabled:     true,
			Dir:         DefaultWALDir,
			SyncOnWrite: DefaultWALSyncOnWrite,
		},
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
		CORS: CORSConfig{
			AllowedOrigins:   []string{},                                                    // 空列表表示不允许任何跨域请求（安全默认值）
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},           // 常用 HTTP 方法
			AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"}, // 常用请求头
			AllowCredentials: false,                                                         // 默认不允许携带凭证
			MaxAge:           86400,                                                         // 预检请求缓存 24 小时
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
			BufferSize:     1000,
			FlushInterval:  30 * time.Second,
			RetentionDays:  365,
			BackupEnabled:  true,
			Properties:     make(map[string]string),
			IDStrategy:     "user_provided", // 默认要求用户提供ID（向后兼容）
			IDPrefix:       "",
			AutoGenerateID: false, // 默认不自动生成（向后兼容）
			IDValidation: IDValidationRules{
				MaxLength:    255,
				Pattern:      "^[a-zA-Z0-9_-]+$",
				AllowedChars: "",
			},
		},
		Tables: make(map[string]TableConfig),
	}

	// 智能限流配置默认值（PathRules 与 Response 作为常量固定，Tiers 可在配置文件中覆盖）
	c.RateLimiting = SmartRateLimitConfig{
		Enabled: true,
		Tiers: map[string]RateLimitingTier{
			"health":   {RequestsPerSecond: 200, BurstSize: 50, WindowSize: time.Second},
			"query":    {RequestsPerSecond: 100, BurstSize: 30, WindowSize: time.Second},
			"write":    {RequestsPerSecond: 80, BurstSize: 20, WindowSize: time.Second},
			"standard": {RequestsPerSecond: 50, BurstSize: 15, WindowSize: time.Second},
			"strict":   {RequestsPerSecond: 20, BurstSize: 5, WindowSize: time.Second},
		},
		PathRules:   DefaultRateLimitPathRules,
		DefaultTier: "standard",
		Response:    DefaultRateLimitResponse,
	}

	// 查询优化配置默认值
	c.QueryOptimization = QueryOptimizationConfig{
		QueryCache: QueryCacheConfig{
			Enabled:         true,
			RedisKeyPrefix:  DefaultQueryCacheRedisKeyPrefix,
			DefaultTTL:      time.Hour,
			MaxCacheSize:    200 * 1024 * 1024, // 200MB
			EvictionPolicy:  DefaultQueryCacheEvictionPolicy,
			CacheStrategies: DefaultQueryCacheStrategies,
			TableInvalidation: TableInvalidationConfig{
				Enabled:           true,
				InvalidationDelay: DefaultQueryCacheInvalidationDelay,
			},
			EnableStats:   true,
			StatsInterval: DefaultQueryCacheStatsInterval,
		},
		FileCache: FileCacheConfig{
			Enabled:         true,
			CacheDir:        "/tmp/miniodb_cache",
			MaxCacheSize:    1024 * 1024 * 1024, // 1GB
			MaxFileAge:      4 * time.Hour,
			CleanupInterval: DefaultFileCacheCleanupInterval,
			RedisIndex: FileCacheRedisConfig{
				Enabled:   true,
				KeyPrefix: DefaultFileCacheRedisKeyPrefix,
				IndexTTL:  DefaultFileCacheRedisIndexTTL,
			},
			Metadata: FileCacheMetadataConfig{
				TrackAccessCount:       true,
				TrackCreationTime:      true,
				EnableHashVerification: true,
			},
			EnableStats:   true,
			StatsInterval: DefaultFileCacheStatsInterval,
		},
		DuckDB: DuckDBConfig{
			Enabled:           true,
			PoolSize:          5,
			MaxIdleTime:       DefaultDuckDBMaxIdleTime,
			ConnectionTimeout: DefaultDuckDBConnectionTimeout,
			Performance: DuckDBPerformanceConfig{
				MemoryLimit:       "1GB",
				Threads:           4,
				EnableObjectCache: DefaultDuckDBEnableObjectCache,
				TempDirectory:     DefaultDuckDBTempDirectory,
			},
			PreparedStatements: DuckDBPreparedStmtsConfig{
				Enabled:                 true,
				CacheSize:               DefaultDuckDBPreparedStmtCacheSize,
				AutoPrepareAggregations: DefaultDuckDBAutoPrepareAggregations,
			},
			ConnectionReuse: DuckDBConnectionReuseConfig{
				Enabled:             DefaultDuckDBConnectionReuseEnabled,
				MaxReuseCount:       DefaultDuckDBMaxReuseCount,
				HealthCheckInterval: DefaultDuckDBConnHealthCheckInterval,
			},
		},
	}

	// 认证配置默认值
	// 安全警告：默认不提供任何凭证，生产环境必须通过配置文件或环境变量配置 api_key_pairs
	c.Auth = AuthConfig{
		TokenExpiry:      "24h",
		APIKeyPairs:      []APIKeyPair{},
		SkipAuthPaths:    DefaultSkipAuthPaths,
		RequireAuthPaths: DefaultRequireAuthPaths,
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

	// Compaction 配置默认值
	c.Compaction = CompactionConfig{
		Enabled:           true,
		TargetFileSize:    128 * 1024 * 1024, // 128MB
		MinFilesToCompact: DefaultCompactionMinFilesToCompact,
		MaxFilesToCompact: DefaultCompactionMaxFilesToCompact,
		CooldownPeriod:    DefaultCompactionCooldownPeriod,
		CheckInterval:     DefaultCompactionCheckInterval,
		TempDir:           DefaultCompactionTempDir,
		CompressionType:   DefaultCompactionCompressionType,
		MaxRowsPerFile:    DefaultCompactionMaxRowsPerFile,
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

	// 文件元数据配置默认值
	c.FileMetadata = FileMetadataConfig{
		KeyPrefix:        DefaultFileMetaKeyPrefix,
		TTL:              DefaultFileMetaTTL,
		EnableSidecar:    DefaultFileMetaEnableSidecar,
		RedisPingTimeout: DefaultFileMetaRedisPingTimeout,
	}

	// 协调器配置默认值
	c.Coordinator = CoordinatorConfig{
		HashRingReplicas:        DefaultCoordinatorHashRingReplicas,
		WriteTimeout:            DefaultCoordinatorWriteTimeout,
		DistributedQueryTimeout: DefaultCoordinatorDistributedQueryTimeout,
		RemoteQueryTimeout:      DefaultCoordinatorRemoteQueryTimeout,
		NodeMonitorInterval:     DefaultCoordinatorNodeMonitorInterval,
	}

	// 数据订阅配置默认值
	c.Subscription = SubscriptionConfig{
		Enabled: false, // 默认不启用
		Redis: RedisSubscriptionConfig{
			Enabled:       false,
			StreamPrefix:  DefaultRedisSubStreamPrefix,
			ConsumerGroup: DefaultRedisSubConsumerGroup,
			ConsumerName:  "", // 空表示自动生成
			BatchSize:     DefaultRedisSubBatchSize,
			BlockTimeout:  DefaultRedisSubBlockTimeout,
			MaxRetries:    DefaultRedisSubMaxRetries,
			RetryDelay:    DefaultRedisSubRetryDelay,
			MaxLen:        DefaultRedisSubMaxLen,
			AutoAck:       DefaultRedisSubAutoAck,
		},
		Kafka: KafkaSubscriptionConfig{
			Enabled:         false,
			Brokers:         []string{"localhost:9092"},
			TopicPrefix:     DefaultKafkaTopicPrefix,
			ConsumerGroup:   DefaultKafkaConsumerGroup,
			BatchSize:       DefaultKafkaBatchSize,
			BatchTimeout:    DefaultKafkaBatchTimeout,
			MaxRetries:      DefaultKafkaMaxRetries,
			RetryBackoff:    DefaultKafkaRetryBackoff,
			SessionTimeout:  DefaultKafkaSessionTimeout,
			HeartbeatPeriod: DefaultKafkaHeartbeatPeriod,
			AutoCommit:      DefaultKafkaAutoCommit,
			CommitInterval:  DefaultKafkaCommitInterval,
			StartOffset:     DefaultKafkaStartOffset,
			UseTLS:          false,
			UseSASL:         false,
			SASLMechanism:   DefaultKafkaSASLMechanism,
		},
	}

	// Swagger配置默认值
	c.Swagger = SwaggerConfig{
		Enabled:     true,
		Host:        "localhost:8081",
		BasePath:    DefaultSwaggerBasePath,
		Title:       DefaultSwaggerTitle,
		Description: DefaultSwaggerDescription,
	}

	// Dashboard配置默认值
	c.Dashboard = DashboardConfig{
		Enabled:               false,
		Port:                  ":9090",
		BasePath:              "/dashboard",
		CoreEndpoint:          "http://localhost:8081",
		CoreGRPCEndpoint:      "localhost:8080",
		MetricsScrapeInterval: 5 * time.Second,
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
	if v := os.Getenv("DASHBOARD_ENABLED"); v == "true" || v == "1" {
		c.Dashboard.Enabled = true
	}
	if v := os.Getenv("DASHBOARD_PORT"); v != "" {
		if v[0] != ':' {
			v = ":" + v
		}
		c.Dashboard.Port = v
	}
	if v := os.Getenv("DASHBOARD_BASE_PATH"); v != "" {
		c.Dashboard.BasePath = v
	}
	if v := os.Getenv("DASHBOARD_CORE_ENDPOINT"); v != "" {
		c.Dashboard.CoreEndpoint = v
	}
	if v := os.Getenv("DASHBOARD_CORE_GRPC_ENDPOINT"); v != "" {
		c.Dashboard.CoreGRPCEndpoint = v
	}

	// 备份MinIO配置环境变量覆盖
	if minioBackupEndpoint := os.Getenv("MINIO_BACKUP_ENDPOINT"); minioBackupEndpoint != "" {
		c.Backup.MinIO.Endpoint = minioBackupEndpoint
		c.Backup.Enabled = true
		// 同时更新Network.Pools.BackupMinIO配置
		if c.Network.Pools.BackupMinIO == nil {
			// 初始化BackupMinIO配置，复制主MinIO的配置作为基础
			c.Network.Pools.BackupMinIO = &EnhancedMinIOConfig{
				UseSSL:                false,
				Region:                "us-east-1",
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   50,
				MaxConnsPerHost:       100,
				IdleConnTimeout:       90 * time.Second,
				DialTimeout:           10 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxRetries:            3,
				RetryDelay:            100 * time.Millisecond,
				RequestTimeout:        120 * time.Second,
				KeepAlive:             30 * time.Second,
				DisableKeepAlive:      false,
				DisableCompression:    false,
			}
		}
		c.Network.Pools.BackupMinIO.Endpoint = minioBackupEndpoint
	}
	if minioBackupAccessKey := os.Getenv("MINIO_BACKUP_ACCESS_KEY"); minioBackupAccessKey != "" {
		c.Backup.MinIO.AccessKeyID = minioBackupAccessKey
		if c.Network.Pools.BackupMinIO != nil {
			c.Network.Pools.BackupMinIO.AccessKeyID = minioBackupAccessKey
		}
	}
	if minioBackupSecretKey := os.Getenv("MINIO_BACKUP_SECRET_KEY"); minioBackupSecretKey != "" {
		c.Backup.MinIO.SecretAccessKey = minioBackupSecretKey
		if c.Network.Pools.BackupMinIO != nil {
			c.Network.Pools.BackupMinIO.SecretAccessKey = minioBackupSecretKey
		}
	}
	if minioBackupBucket := os.Getenv("MINIO_BACKUP_BUCKET"); minioBackupBucket != "" {
		c.Backup.MinIO.Bucket = minioBackupBucket
		if c.Network.Pools.BackupMinIO != nil {
			c.Network.Pools.BackupMinIO.Bucket = minioBackupBucket
		}
	}
	if minioBackupUseSSL := os.Getenv("MINIO_BACKUP_USE_SSL"); minioBackupUseSSL == "true" {
		if c.Network.Pools.BackupMinIO != nil {
			c.Network.Pools.BackupMinIO.UseSSL = true
		}
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
	if err := validatePort(c.Server.GrpcPort); err != nil {
		return fmt.Errorf("grpc_port validation failed: %w", err)
	}

	if c.Server.RestPort == "" {
		return fmt.Errorf("rest_port is required")
	}
	if err := validatePort(c.Server.RestPort); err != nil {
		return fmt.Errorf("rest_port validation failed: %w", err)
	}

	// 验证Redis配置
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis addr is required")
	}
	if err := validateAddress(c.Redis.Addr); err != nil {
		return fmt.Errorf("redis addr validation failed: %w", err)
	}

	// 验证MinIO配置
	if c.MinIO.Endpoint == "" {
		return fmt.Errorf("minio endpoint is required")
	}
	if err := validateAddress(c.MinIO.Endpoint); err != nil {
		return fmt.Errorf("minio endpoint validation failed: %w", err)
	}
	if c.MinIO.Bucket == "" {
		return fmt.Errorf("minio bucket is required")
	}
	if err := validateBucketName(c.MinIO.Bucket); err != nil {
		return fmt.Errorf("minio bucket name validation failed: %w", err)
	}

	// 验证表管理配置
	if c.TableManagement.MaxTables <= 0 {
		return fmt.Errorf("max_tables must be positive")
	}
	if c.TableManagement.MaxTables > 10000 {
		return fmt.Errorf("max_tables too large, maximum is 10000")
	}
	if c.TableManagement.TableNamePattern == "" {
		return fmt.Errorf("table_name_pattern is required")
	}

	// 验证默认表配置
	if c.Tables.DefaultConfig.BufferSize <= 0 {
		return fmt.Errorf("default buffer_size must be positive")
	}
	if c.Tables.DefaultConfig.BufferSize > 100000 {
		return fmt.Errorf("default buffer_size too large, maximum is 100000")
	}
	if c.Tables.DefaultConfig.FlushInterval <= 0 {
		return fmt.Errorf("default flush_interval must be positive")
	}
	if c.Tables.DefaultConfig.FlushInterval > 1*time.Hour {
		return fmt.Errorf("default flush_interval too large, maximum is 1 hour")
	}
	if c.Tables.DefaultConfig.RetentionDays <= 0 {
		return fmt.Errorf("default retention_days must be positive")
	}
	if c.Tables.DefaultConfig.RetentionDays > 3650 {
		return fmt.Errorf("default retention_days too large, maximum is 3650 (10 years)")
	}

	// 验证系统资源配置
	if c.System.MaxMemoryMB > 0 && c.System.MaxMemoryMB < 256 {
		return fmt.Errorf("max_memory_mb too small, minimum is 256")
	}
	if c.System.MaxGoroutines > 0 && c.System.MaxGoroutines < 100 {
		return fmt.Errorf("max_goroutines too small, minimum is 100")
	}

	// 验证日志配置
	if c.Log.Filename != "" {
		logDir := filepath.Dir(c.Log.Filename)
		if !fileExists(logDir) {
			// 自动创建日志目录
			if err := os.MkdirAll(logDir, 0755); err != nil {
				return fmt.Errorf("failed to create log directory: %w", err)
			}
		}
		if c.Log.MaxSize <= 0 {
			return fmt.Errorf("log max_size must be positive")
		}
		if c.Log.MaxSize > 1000 {
			return fmt.Errorf("log max_size too large, maximum is 1000 MB")
		}
	}

	// 验证指标配置
	if c.Metrics.Enabled {
		if err := validatePort(c.Metrics.Port); err != nil {
			return fmt.Errorf("metrics port validation failed: %w", err)
		}
	}

	// 验证监控配置
	if c.Monitoring.Enabled {
		if err := validatePort(c.Monitoring.Port); err != nil {
			return fmt.Errorf("monitoring port validation failed: %w", err)
		}
	}

	// 如果有旧的buffer配置，将其映射到默认表配置
	if c.Buffer.BufferSize > 0 || c.Buffer.FlushInterval > 0 {
		// 迁移配置（静默处理，不需要日志）
		if c.Buffer.BufferSize > 0 {
			c.Tables.DefaultConfig.BufferSize = c.Buffer.BufferSize
		}
		if c.Buffer.FlushInterval > 0 {
			c.Tables.DefaultConfig.FlushInterval = c.Buffer.FlushInterval
		}
	}

	// 验证 CORS 配置
	if err := c.validateCORSConfig(); err != nil {
		return fmt.Errorf("CORS config validation failed: %w", err)
	}

	return nil
}

// validateCORSConfig 验证 CORS 配置的安全性
func (c *Config) validateCORSConfig() error {
	cors := c.Security.CORS

	// 规则1: AllowCredentials: true 时禁止 AllowedOrigins 为空
	// 因为空列表 + credentials 在某些浏览器中可能被解释为允许所有来源
	if cors.AllowCredentials && len(cors.AllowedOrigins) == 0 {
		return fmt.Errorf("when allow_credentials is true, allowed_origins cannot be empty - this creates an ambiguous security policy")
	}

	// 规则2: AllowCredentials: true 时禁止 AllowedOrigins 包含 "*"
	// 根据 CORS 规范，* 与 credentials 不能同时使用
	if cors.AllowCredentials {
		for _, origin := range cors.AllowedOrigins {
			if origin == "*" {
				return fmt.Errorf("when allow_credentials is true, allowed_origins cannot contain '*' - this violates CORS specification")
			}
		}
	}

	// 规则3: 如果 AllowedOrigins 包含 "*"，确保 AllowCredentials 为 false
	for _, origin := range cors.AllowedOrigins {
		if origin == "*" && cors.AllowCredentials {
			return fmt.Errorf("allowed_origins contains '*' and allow_credentials is true - this violates CORS specification")
		}
	}

	return nil
}

// validatePort 验证端口号
func validatePort(port string) error {
	// 去掉可能的冒号前缀
	port = strings.TrimPrefix(port, ":")

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port number: %s", port)
	}

	if portNum < 1024 || portNum > 65535 {
		return fmt.Errorf("port must be in range 1024-65535, got %d", portNum)
	}

	return nil
}

// validateAddress 验证地址格式
func validateAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("address cannot be empty")
	}

	parts := strings.Split(addr, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid address format, expected host:port")
	}

	host := parts[0]
	port := parts[len(parts)-1]

	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	return validatePort(port)
}

// validateBucketName 验证bucket名称
func validateBucketName(name string) error {
	if name == "" {
		return fmt.Errorf("bucket name cannot be empty")
	}

	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("bucket name must be between 3 and 63 characters, got %d", len(name))
	}

	lower := strings.ToLower(name)
	if name != lower {
		return fmt.Errorf("bucket name must be lowercase")
	}

	// 检查有效的DNS标签格式
	for i, char := range name {
		valid := (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '.'

		if !valid {
			return fmt.Errorf("bucket name contains invalid character at position %d: %c", i, char)
		}
	}

	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("bucket name cannot start or end with hyphen")
	}

	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("bucket name cannot start or end with period")
	}

	return nil
}

// validateJWTSecret 验证JWT密钥强度
func validateJWTSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("JWT secret is required but not set. Please set JWT_SECRET environment variable or configure it in config file")
	}

	// 检查是否使用常见的弱密钥或默认密钥
	weakSecrets := []string{
		"your-super-secret-jwt-key-change-this-in-production",
		"secret",
		"jwt-secret",
		"change-me",
		"default-secret",
		"test-secret",
	}

	for _, weak := range weakSecrets {
		if secret == weak {
			return fmt.Errorf("JWT secret matches a known weak or default pattern. Please use a strong, unique secret")
		}
	}

	if len(secret) < 32 {
		return fmt.Errorf("JWT secret must be at least 32 characters long, got %d characters", len(secret))
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
