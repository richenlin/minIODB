package constants

import "time"

// 端口范围常量
const (
	// 最小可用端口
	MinPort = 1024
	// 最大可用端口
	MaxPort = 65535

	// 默认端口
	DefaultGRPCPort = ":50051"
	DefaultRESTPort = ":8080"
	DefaultHTTPPort = ":8080"
)

// Bucket名称限制
const (
	// MinIO bucket名称最小长度
	MinBucketNameLength = 3
	// MinIO bucket名称最大长度
	MaxBucketNameLength = 63
	// 默认bucket名称
	DefaultBucketName = "olap-data"
)

// 表管理配置限制
const (
	// 默认最大表数量
	DefaultMaxTables = 1000
	// 最大表数量
	MaxTablesLimit = 10000

	// 默认缓冲区大小
	DefaultBufferSize = 1000
	// 最大缓冲区大小
	MaxBufferSize = 100000

	// 默认刷新间隔
	DefaultFlushInterval = 30 * time.Minute
	// 最大刷新间隔
	MaxFlushInterval = 1 * time.Hour

	// 默认数据保留天数
	DefaultRetentionDays = 365
	// 最大数据保留天数（10年）
	MaxRetentionDays = 3650
)

// 系统资源限制
const (
	// 最小内存限制（MB）
	MinMemoryMB = 256
	// 默认内存限制（MB）
	DefaultMemoryMB = 2048

	// 最小goroutine数量
	MinGoroutines = 100
	// 默认goroutine数量
	DefaultGoroutines = 1000
)

// 日志配置限制
const (
	// 日志文件最大大小（MB）
	DefaultLogMaxSize = 100
	MaxLogMaxSize     = 1000

	// 日志文件最大备份数
	DefaultLogMaxBackups = 5
	MaxLogMaxBackups     = 10

	// 日志文件最大天数
	DefaultLogMaxAge = 30
	MaxLogMaxAge     = 365

	// 日志级别
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
	LogLevelFatal = "fatal"

	// 日志格式
	LogFormatJSON = "json"
	LogFormatText = "text"

	// 日志输出
	LogOutputStdout = "stdout"
	LogOutputStderr = "stderr"
	LogOutputFile   = "file"
	LogOutputBoth   = "both"
)

// SQL查询限制
const (
	// SQL查询最大长度
	MaxSQLQueryLength = 10000

	// 查询结果默认限制
	DefaultQueryLimit = 100
	MaxQueryLimit     = 10000
	MinQueryLimit     = 1

	// 批量操作大小
	DefaultBatchSize = 1000
	MaxBatchSize     = 10000
	MinBatchSize     = 1
)

// 网络配置
const (
	// gRPC服务器默认配置
	DefaultGRPCMaxConnections        = 1000
	DefaultGRPCConnectionTimeout     = 30 * time.Second
	DefaultGRPCStreamTimeout         = 60 * time.Second
	DefaultGRPCKeepAliveTime         = 30 * time.Second
	DefaultGRPCMaxConnectionIdle     = 15 * time.Minute
	DefaultGRPCMaxConnectionAge      = 30 * time.Minute
	DefaultGRPCMaxConnectionAgeGrace = 5 * time.Second
	DefaultGRPCMaxSendMsgSize        = 4194304 // 4MB
	DefaultGRPCMaxRecvMsgSize        = 4194304 // 4MB

	// REST服务器默认配置
	DefaultRESTReadTimeout       = 30 * time.Second
	DefaultRESTWriteTimeout      = 30 * time.Second
	DefaultRESTIdleTimeout       = 60 * time.Second
	DefaultRESTReadHeaderTimeout = 10 * time.Second
	DefaultRESTMaxHeaderBytes    = 1048576 // 1MB
	DefaultRESTShutdownTimeout   = 30 * time.Second

	// 连接池默认配置
	DefaultRedisPoolSize        = 250
	DefaultRedisMinIdleConns    = 25
	DefaultRedisMaxConnAge      = 30 * time.Minute
	DefaultRedisPoolTimeout     = 3 * time.Second
	DefaultRedisIdleTimeout     = 5 * time.Minute
	DefaultRedisDialTimeout     = 3 * time.Second
	DefaultRedisReadTimeout     = 2 * time.Second
	DefaultRedisWriteTimeout    = 2 * time.Second
	DefaultRedisIdleCheckFreq   = time.Minute
	DefaultRedisMaxRetries      = 3
	DefaultRedisMinRetryBackoff = 8 * time.Millisecond
	DefaultRedisMaxRetryBackoff = 512 * time.Millisecond
	DefaultRedisMaxRedirects    = 8

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
	DefaultMinIOHealthCheckInterval   = 15 * time.Second
)

// 认证配置
const (
	// JWT默认配置
	DefaultJWTExpiry   = "24h"
	MinJWTSecretLength = 32

	// Token默认过期时间（小时）
	DefaultTokenExpiryHours = 24
	MinTokenExpiryHours     = 1
	MaxTokenExpiryHours     = 168 // 7天
)

// 限流配置
const (
	// 传统限流
	DefaultRateLimitPerMinute = 60
	MinRateLimitPerMinute     = 1
	MaxRateLimitPerMinute     = 10000

	// 智能限流 - 默认 tiers
	DefaultHealthTierRPS   = 200
	DefaultQueryTierRPS    = 100
	DefaultWriteTierRPS    = 80
	DefaultStandardTierRPS = 50
	DefaultStrictTierRPS   = 20

	DefaultHealthTierBurst   = 50
	DefaultQueryTierBurst    = 30
	DefaultWriteTierBurst    = 20
	DefaultStandardTierBurst = 15
	DefaultStrictTierBurst   = 5

	DefaultRateLimitWindow = time.Second
	DefaultCleanupInterval = 5 * time.Minute
)

// 缓存配置
const (
	// 查询缓存默认配置
	DefaultQueryCacheEnabled           = true
	DefaultQueryCacheTTL               = 1 * time.Hour
	DefaultQueryCacheMaxSize           = 209715200 // 200MB
	DefaultQueryCacheKeyPrefix         = "qcache:"
	DefaultQueryCacheEvictionPolicy    = "lru"
	DefaultQueryCacheStatsInterval     = time.Minute
	DefaultQueryCacheInvalidationDelay = 5 * time.Second

	// 文件缓存默认配置
	DefaultFileCacheEnabled         = true
	DefaultFileCacheDir             = "/tmp/miniodb_file_cache"
	DefaultFileCacheMaxSize         = 1073741824 // 1GB
	DefaultFileCacheMaxFileAge      = 4 * time.Hour
	DefaultFileCacheCleanupInterval = 15 * time.Minute
	DefaultFileCacheKeyPrefix       = "fcache:"
	DefaultFileCacheIndexTTL        = 24 * time.Hour
	DefaultFileCacheStatsInterval   = time.Minute
)

// 备份配置
const (
	// 数据备份默认配置
	DefaultBackupInterval   = 24 * time.Hour
	MaxBackupRetries        = 3
	DefaultBackupRetryDelay = 5 * time.Second

	// 元数据备份默认配置
	DefaultMetadataBackupInterval = 30 * time.Minute
	DefaultMetadataRetentionDays  = 7
	DefaultMetadataBackupBucket   = "olap-metadata"
)

// 性能配置
const (
	// 默认请求超时
	DefaultRequestTimeout = 30 * time.Second
	MinRequestTimeout     = 1 * time.Second
	MaxRequestTimeout     = 300 * time.Second

	// 默认连接超时
	DefaultConnectionTimeout = 5 * time.Second

	// 默认响应超时
	DefaultResponseTimeout = 30 * time.Second
)

// 指标和监控
const (
	// 指标服务器默认配置
	DefaultMetricsPort = ":9090"
	DefaultMetricsPath = "/metrics"

	// 监控服务器默认配置
	DefaultMonitoringPort = ":8082"
	DefaultMonitoringPath = "/metrics"
)

// 流处理
const (
	// 流处理默认配置
	DefaultStreamBatchSize            = 1000
	DefaultStreamBatchDelay           = 100 * time.Millisecond
	MaxStreamBatchSize                = 10000
	MinStreamBatchSize                = 10
	DefaultStreamBackpressureInterval = 1000 // 处理1000条记录暂停一下
)

// 字符串限制
const (
	// 表名限制
	MinTableNameLength = 1
	MaxTableNameLength = 128

	// ID限制
	MinIDLength = 1
	MaxIDLength = 255

	// 查询限制
	MaxQueryMessageSize = 4 * 1024 * 1024 // 4MB (gRPC默认)
)
