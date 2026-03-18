package config

import "time"

// ─────────────────────────────────────────────────────────────────────────────
// gRPC 服务器网络调优参数（几乎无需用户修改）
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultGRPCMaxConnections        = 1000
	DefaultGRPCConnectionTimeout     = 30 * time.Second
	DefaultGRPCStreamTimeout         = 60 * time.Second
	DefaultGRPCKeepAliveTime         = 30 * time.Second
	DefaultGRPCKeepAliveTimeout      = 5 * time.Second
	DefaultGRPCMaxConnectionIdle     = 15 * time.Minute
	DefaultGRPCMaxConnectionAge      = 30 * time.Minute
	DefaultGRPCMaxConnectionAgeGrace = 5 * time.Second
	DefaultGRPCMaxSendMsgSize        = 4 * 1024 * 1024 // 4MB
	DefaultGRPCMaxRecvMsgSize        = 4 * 1024 * 1024 // 4MB
)

// ─────────────────────────────────────────────────────────────────────────────
// REST 服务器网络调优参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultRESTReadTimeout       = 30 * time.Second
	DefaultRESTWriteTimeout      = 30 * time.Second
	DefaultRESTIdleTimeout       = 60 * time.Second
	DefaultRESTReadHeaderTimeout = 10 * time.Second
	DefaultRESTMaxHeaderBytes    = 1024 * 1024 // 1MB
	DefaultRESTShutdownTimeout   = 30 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// Redis 连接池调优参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultRedisPoolSize        = 250
	DefaultRedisMinIdleConns    = 25
	DefaultRedisMaxConnAge      = 30 * time.Minute
	DefaultRedisPoolTimeout     = 3 * time.Second
	DefaultRedisIdleTimeout     = 5 * time.Minute
	DefaultRedisIdleCheckFreq   = time.Minute
	DefaultRedisDialTimeout     = 3 * time.Second
	DefaultRedisReadTimeout     = 2 * time.Second
	DefaultRedisWriteTimeout    = 2 * time.Second
	DefaultRedisMaxRetries      = 3
	DefaultRedisMinRetryBackoff = 8 * time.Millisecond
	DefaultRedisMaxRetryBackoff = 512 * time.Millisecond
	DefaultRedisMaxRedirects    = 8
	DefaultRedisRouteByLatency  = true
	DefaultRedisRouteRandomly   = false
	DefaultRedisReadOnly        = false
)

// ─────────────────────────────────────────────────────────────────────────────
// MinIO HTTP 连接池调优参数（主实例）
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultMinIOMaxIdleConns          = 300
	DefaultMinIOMaxIdleConnsPerHost   = 150
	DefaultMinIOMaxConnsPerHost       = 300
	DefaultMinIOIdleConnTimeout       = 90 * time.Second
	DefaultMinIODialTimeout           = 5 * time.Second
	DefaultMinIOTLSHandshakeTimeout   = 5 * time.Second
	DefaultMinIOResponseHeaderTimeout = 15 * time.Second
	DefaultMinIOExpectContinueTimeout = 1 * time.Second
	DefaultMinIOMaxRetries            = 3
	DefaultMinIORetryDelay            = 100 * time.Millisecond
	DefaultMinIORequestTimeout        = 60 * time.Second
	DefaultMinIOKeepAlive             = 30 * time.Second
	DefaultMinIODisableKeepAlive      = false
	DefaultMinIODisableCompression    = false
)

// ─────────────────────────────────────────────────────────────────────────────
// 备份 MinIO HTTP 连接池（保守配置，非关键路径）
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultBackupMinIOMaxIdleConns          = 100
	DefaultBackupMinIOMaxIdleConnsPerHost   = 50
	DefaultBackupMinIOMaxConnsPerHost       = 100
	DefaultBackupMinIOIdleConnTimeout       = 90 * time.Second
	DefaultBackupMinIODialTimeout           = 10 * time.Second
	DefaultBackupMinIOTLSHandshakeTimeout   = 10 * time.Second
	DefaultBackupMinIOResponseHeaderTimeout = 30 * time.Second
	DefaultBackupMinIOExpectContinueTimeout = 1 * time.Second
	DefaultBackupMinIOMaxRetries            = 3
	DefaultBackupMinIORetryDelay            = 100 * time.Millisecond
	DefaultBackupMinIORequestTimeout        = 120 * time.Second
	DefaultBackupMinIOKeepAlive             = 30 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// 故障切换 & 异步同步内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultFailoverHealthCheckInterval = 15 * time.Second
	DefaultPoolsHealthCheckInterval    = 15 * time.Second

	DefaultAsyncSyncQueueSize     = 1000
	DefaultAsyncSyncWorkerCount   = 3
	DefaultAsyncSyncRetryTimes    = 3
	DefaultAsyncSyncRetryInterval = time.Second
	DefaultAsyncSyncTimeout       = 60 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// 缓冲区（Buffer）内部处理参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultBufferWorkerPoolSize  = 20
	DefaultBufferTaskQueueSize   = 100
	DefaultBufferBatchFlushSize  = 5
	DefaultBufferEnableBatching  = true
	DefaultBufferFlushTimeout    = 60 * time.Second
	DefaultBufferMaxRetries      = 3
	DefaultBufferRetryDelay      = time.Second
	DefaultBufferTempDir         = "/tmp/miniodb_buffer"
	DefaultBufferParquetRowGroup = int64(10000)
	DefaultBufferDefaultBucket   = "miniodb-data"
	DefaultWALDir                = "data/wal"
	DefaultWALSyncOnWrite        = true
)

// ─────────────────────────────────────────────────────────────────────────────
// 限流（Rate Limiting）路径规则 & 响应头标志
// 路径与 API 路由强绑定，不开放用户修改
// ─────────────────────────────────────────────────────────────────────────────

// DefaultRateLimitPathRules 限流路径规则
var DefaultRateLimitPathRules = []RateLimitingPathRule{
	{Path: "/health", Method: "GET", Tier: "health"},
	{Path: "/v1/health", Method: "GET", Tier: "health"},
	{Path: "/metrics", Method: "GET", Tier: "health"},
	{Path: "/v1/query", Method: "POST", Tier: "query"},
	{Path: "/v1/write", Method: "POST", Tier: "write"},
	{Path: "/v1/backup/trigger", Method: "POST", Tier: "strict"},
	{Path: "/v1/recover", Method: "POST", Tier: "strict"},
}

// DefaultRateLimitResponse 限流响应头全部开启
var DefaultRateLimitResponse = RateLimitingResponse{
	IncludeTier:       true,
	IncludeLimit:      true,
	IncludeBurst:      true,
	IncludeWindow:     true,
	IncludeRetryAfter: true,
}

// ─────────────────────────────────────────────────────────────────────────────
// 查询缓存（Query Cache）内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultQueryCacheRedisKeyPrefix    = "qcache:"
	DefaultQueryCacheEvictionPolicy    = "lru"
	DefaultQueryCacheInvalidationDelay = 5 * time.Second
	DefaultQueryCacheStatsInterval     = time.Minute
)

// DefaultQueryCacheStrategies 各查询类型的缓存 TTL
var DefaultQueryCacheStrategies = map[string]time.Duration{
	"simple_select": 2 * time.Hour,
	"count_query":   time.Hour,
	"aggregation":   30 * time.Minute,
	"join_query":    15 * time.Minute,
	"complex_query": 10 * time.Minute,
}

// ─────────────────────────────────────────────────────────────────────────────
// 文件缓存（File Cache）内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultFileCacheCleanupInterval = 15 * time.Minute
	DefaultFileCacheRedisKeyPrefix  = "fcache:"
	DefaultFileCacheRedisIndexTTL   = 24 * time.Hour
	DefaultFileCacheStatsInterval   = time.Minute
)

// ─────────────────────────────────────────────────────────────────────────────
// DuckDB 连接池内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultDuckDBMaxIdleTime             = 30 * time.Minute
	DefaultDuckDBConnectionTimeout       = 30 * time.Second
	DefaultDuckDBEnableObjectCache       = true
	DefaultDuckDBTempDirectory           = "/tmp/duckdb"
	DefaultDuckDBPreparedStmtCacheSize   = 100
	DefaultDuckDBAutoPrepareAggregations = true
	DefaultDuckDBConnectionReuseEnabled  = true
	DefaultDuckDBMaxReuseCount           = 1000
	DefaultDuckDBConnHealthCheckInterval = 5 * time.Minute
	DefaultSlowQueryThreshold            = 1 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// 文件元数据（File Metadata）内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultFileMetaKeyPrefix        = "file_meta:"
	DefaultFileMetaTTL              = 30 * 24 * time.Hour // 30天
	DefaultFileMetaEnableSidecar    = true
	DefaultFileMetaRedisPingTimeout = 2 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// 协调器（Coordinator）内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultCoordinatorHashRingReplicas        = 150
	DefaultCoordinatorWriteTimeout            = 10 * time.Second
	DefaultCoordinatorDistributedQueryTimeout = 30 * time.Second
	DefaultCoordinatorRemoteQueryTimeout      = 10 * time.Second
	DefaultCoordinatorNodeMonitorInterval     = 30 * time.Second
)

// ─────────────────────────────────────────────────────────────────────────────
// Compaction 内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultCompactionMinFilesToCompact = 5
	DefaultCompactionMaxFilesToCompact = 20
	DefaultCompactionCooldownPeriod    = 5 * time.Minute
	DefaultCompactionCheckInterval     = 10 * time.Minute
	DefaultCompactionTempDir           = "/tmp/miniodb_compaction"
	DefaultCompactionCompressionType   = "snappy"
	DefaultCompactionMaxRowsPerFile    = int64(1000000)
)

// ─────────────────────────────────────────────────────────────────────────────
// Redis 订阅（Subscription）内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultRedisSubStreamPrefix  = "miniodb:stream:"
	DefaultRedisSubConsumerGroup = "miniodb-workers"
	DefaultRedisSubBatchSize     = 100
	DefaultRedisSubBlockTimeout  = 5 * time.Second
	DefaultRedisSubMaxRetries    = 3
	DefaultRedisSubRetryDelay    = time.Second
	DefaultRedisSubMaxLen        = int64(0) // 不限制
	DefaultRedisSubAutoAck       = true
)

// ─────────────────────────────────────────────────────────────────────────────
// Kafka 订阅内部参数
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultKafkaTopicPrefix     = "miniodb-"
	DefaultKafkaConsumerGroup   = "miniodb-workers"
	DefaultKafkaBatchSize       = 100
	DefaultKafkaBatchTimeout    = time.Second
	DefaultKafkaMaxRetries      = 3
	DefaultKafkaRetryBackoff    = 100 * time.Millisecond
	DefaultKafkaSessionTimeout  = 30 * time.Second
	DefaultKafkaHeartbeatPeriod = 3 * time.Second
	DefaultKafkaAutoCommit      = true
	DefaultKafkaCommitInterval  = time.Second
	DefaultKafkaStartOffset     = "latest"
	DefaultKafkaSASLMechanism   = "plain"
)

// ─────────────────────────────────────────────────────────────────────────────
// Swagger 静态信息
// ─────────────────────────────────────────────────────────────────────────────

const (
	DefaultSwaggerBasePath    = "/v1"
	DefaultSwaggerTitle       = "MinIODB API"
	DefaultSwaggerDescription = "基于MinIO+DuckDB+Redis的分布式OLAP系统API"
)

// ─────────────────────────────────────────────────────────────────────────────
// 认证默认路径（与 API 路由强绑定）
// ─────────────────────────────────────────────────────────────────────────────

var DefaultSkipAuthPaths = []string{
	"/health",
	"/metrics",
	"/v1/health",
}

var DefaultRequireAuthPaths = []string{
	"/v1/write",
	"/v1/query",
	"/v1/backup/trigger",
	"/v1/recover",
}
