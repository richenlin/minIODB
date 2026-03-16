//go:build dashboard

package model

import (
	"time"

	"minIODB/config"
)

type HealthResult struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
	Version   string `json:"version"`
	NodeID    string `json:"node_id"`
	Mode      string `json:"mode"`
}

type StatusResult struct {
	Version       string `json:"version"`
	NodeID        string `json:"node_id"`
	Mode          string `json:"mode"`
	Tables        int    `json:"tables"`
	Goroutines    int    `json:"goroutines"`
	MemoryUsage   int64  `json:"memory_usage"`
	Uptime        int64  `json:"uptime"`
	PendingWrites int64  `json:"pending_writes"`
}

type MetricsResult struct {
	Timestamp     int64 `json:"timestamp"`
	HTTPRequests  int64 `json:"http_requests"`
	QueryDuration int64 `json:"query_duration"`
	BufferSize    int   `json:"buffer_size"`
	MinioOps      int64 `json:"minio_ops"`
	RedisOps      int64 `json:"redis_ops"`
	Goroutines    int   `json:"goroutines"`
	MemAlloc      int64 `json:"mem_alloc"`
	GCPauseNs     int64 `json:"gc_pause_ns"`
}

type NodeResult struct {
	ID       string            `json:"id"`
	Address  string            `json:"address"`
	Port     string            `json:"port"`
	Status   string            `json:"status"`
	LastSeen time.Time         `json:"last_seen"`
	Metadata map[string]string `json:"metadata"`
	// Virtual marks synthetic infrastructure nodes (e.g. Redis coordinator)
	// injected by the topology handler for visualisation purposes only.
	Virtual bool `json:"virtual,omitempty"`
}

type TableResult struct {
	Name        string    `json:"name"`
	RowCountEst int64     `json:"row_count_est"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

type TableDetailResult struct {
	TableResult
	Config        config.TableConfig `json:"config"`
	BufferSize    int                `json:"buffer_size"`
	FlushInterval int64              `json:"flush_interval"`
	RetentionDays int                `json:"retention_days"`
	BackupEnabled bool               `json:"backup_enabled"`
	IDStrategy    string             `json:"id_strategy"`
}

type ColumnStats struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Min      string  `json:"min"`
	Max      string  `json:"max"`
	Distinct int64   `json:"distinct"`
	NullRate float64 `json:"null_rate"`
}

type TableStatsResult struct {
	Name    string        `json:"name"`
	Columns []ColumnStats `json:"columns"`
}

type BrowseParams struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
	Filter    string `json:"filter"`
	ID        string `json:"id"`
}

type BrowseResult struct {
	Rows     []map[string]interface{} `json:"rows"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

type WriteRecordRequest struct {
	ID        string                 `json:"id"`
	Timestamp int64                  `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

type UpdateRecordRequest struct {
	Timestamp int64                  `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

type QueryResult struct {
	Columns    []string                 `json:"columns"`
	Rows       []map[string]interface{} `json:"rows"`
	Total      int64                    `json:"total"`
	DurationMs int64                    `json:"duration_ms"`
}

type BackupResult struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	TableName   string    `json:"table_name"`
	Status      string    `json:"status"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at"`
	Tables      []string  `json:"tables"`
}

type RestoreOptions struct {
	Tables      []string `json:"tables"`
	TargetTable string   `json:"target_table"`
}

type MetadataStatusResult struct {
	LastBackup     time.Time `json:"last_backup"`
	TablesCount    int       `json:"tables_count"`
	BackupEnabled  bool      `json:"backup_enabled"`
	BackupInterval int64     `json:"backup_interval"`
}

type LogQueryParams struct {
	Level     string `json:"level"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Keyword   string `json:"keyword"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
}

type LogEntry struct {
	Level     string                 `json:"level"`
	Timestamp int64                  `json:"timestamp"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields"`
}

type LogQueryResult struct {
	Logs     []LogEntry `json:"logs"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type MonitorOverviewResult struct {
	Goroutines  int     `json:"goroutines"`
	MemAllocMB  float64 `json:"mem_alloc_mb"`
	GCPauseMs   float64 `json:"gc_pause_ms"`
	UptimeHours float64 `json:"uptime_hours"`
	CPUPercent  float64 `json:"cpu_percent"`
}

type SLAResult struct {
	QueryLatencyP50Ms float64 `json:"query_latency_p50_ms"`
	QueryLatencyP95Ms float64 `json:"query_latency_p95_ms"`
	QueryLatencyP99Ms float64 `json:"query_latency_p99_ms"`
	WriteLatencyP95Ms float64 `json:"write_latency_p95_ms"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	FilePruneRate     float64 `json:"file_prune_rate"`
	ErrorRate         float64 `json:"error_rate"`
}

type AnalyticsQueryResult struct {
	Columns     []string                 `json:"columns"`
	Rows        []map[string]interface{} `json:"rows"`
	Total       int64                    `json:"total"`
	DurationMs  int64                    `json:"duration_ms"`
	ExplainPlan string                   `json:"explain_plan"`
}

type TimeSeriesPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type AnalyticsOverviewResult struct {
	WriteTrend []TimeSeriesPoint `json:"write_trend"`
	QueryTrend []TimeSeriesPoint `json:"query_trend"`
	DataVolume []TimeSeriesPoint `json:"data_volume"`
}

type CreateTableRequest struct {
	TableName string             `json:"table_name"`
	Config    config.TableConfig `json:"config"`
}

type UpdateTableRequest struct {
	Config config.TableConfig `json:"config"`
}

// FullConfig is the structured, editable configuration returned by GET /cluster/config.
// It mirrors the fields in config.yaml that operators commonly need to change.
type FullConfig struct {
	// server
	NodeID   string `json:"node_id"`
	GrpcPort string `json:"grpc_port"`
	RestPort string `json:"rest_port"`

	// network.pools.redis
	RedisMode        string   `json:"redis_mode"`
	RedisAddr        string   `json:"redis_addr"`
	RedisPassword    string   `json:"redis_password"`
	RedisDB          int      `json:"redis_db"`
	RedisMasterName  string   `json:"redis_master_name,omitempty"`
	SentinelAddrs    []string `json:"sentinel_addrs,omitempty"`
	ClusterAddrs     []string `json:"cluster_addrs,omitempty"`

	// network.pools.minio
	MinioEndpoint        string `json:"minio_endpoint"`
	MinioAccessKeyID     string `json:"minio_access_key_id"`
	MinioSecretAccessKey string `json:"minio_secret_access_key"`
	MinioUseSSL          bool   `json:"minio_use_ssl"`
	MinioRegion          string `json:"minio_region"`
	MinioBucket          string `json:"minio_bucket"`

	// dashboard
	CoreEndpoint string `json:"core_endpoint"`
	DashboardPort string `json:"dashboard_port"`

	// log
	LogLevel    string `json:"log_level"`
	LogFormat   string `json:"log_format"`
	LogOutput   string `json:"log_output"`
	LogFilename string `json:"log_filename"`

	// buffer
	BufferSize    int    `json:"buffer_size"`
	FlushInterval string `json:"flush_interval"`

	// runtime-read-only
	Mode    string `json:"mode"`
	Version string `json:"version"`

	// EnvOverrides maps config field JSON keys to the environment variable name
	// that is currently overriding them.  A field listed here will revert to the
	// env-var value on every restart, regardless of what config.yaml contains.
	// Example: {"redis_addr": "REDIS_HOST / REDIS_PORT"}
	EnvOverrides map[string]string `json:"env_overrides,omitempty"`
}

// ConfigUpdateRequest carries the fields the operator wants to change.
// Only non-zero / non-empty values are applied; boolean false is represented
// as a pointer so we can distinguish "not set" from "set to false".
type ConfigUpdateRequest struct {
	NodeID   string `json:"node_id,omitempty"`
	GrpcPort string `json:"grpc_port,omitempty"`
	RestPort string `json:"rest_port,omitempty"`

	RedisMode        string   `json:"redis_mode,omitempty"`
	RedisAddr        string   `json:"redis_addr,omitempty"`
	RedisPassword    string   `json:"redis_password,omitempty"`
	RedisDB          *int     `json:"redis_db,omitempty"`
	RedisMasterName  string   `json:"redis_master_name,omitempty"`
	SentinelAddrs    []string `json:"sentinel_addrs,omitempty"`
	ClusterAddrs     []string `json:"cluster_addrs,omitempty"`

	MinioEndpoint        string `json:"minio_endpoint,omitempty"`
	MinioAccessKeyID     string `json:"minio_access_key_id,omitempty"`
	MinioSecretAccessKey string `json:"minio_secret_access_key,omitempty"`
	MinioUseSSL          *bool  `json:"minio_use_ssl,omitempty"`
	MinioRegion          string `json:"minio_region,omitempty"`
	MinioBucket          string `json:"minio_bucket,omitempty"`

	CoreEndpoint  string `json:"core_endpoint,omitempty"`
	DashboardPort string `json:"dashboard_port,omitempty"`

	LogLevel    string `json:"log_level,omitempty"`
	LogFormat   string `json:"log_format,omitempty"`
	LogOutput   string `json:"log_output,omitempty"`
	LogFilename string `json:"log_filename,omitempty"`

	BufferSize    *int   `json:"buffer_size,omitempty"`
	FlushInterval string `json:"flush_interval,omitempty"`
}

// ConfigUpdateResult is returned by PUT /cluster/config.
type ConfigUpdateResult struct {
	ConfigSnippet   string `json:"config_snippet"`
	RestartRequired bool   `json:"restart_required"`
	Message         string `json:"message"`
}

// EnableDistributedRequest holds the Redis connection parameters needed to
// switch a standalone node into distributed (multi-node) mode.
type EnableDistributedRequest struct {
	// RedisMode is one of: standalone | sentinel | cluster
	RedisMode        string   `json:"redis_mode"`
	RedisAddr        string   `json:"redis_addr"`         // standalone mode
	RedisPassword    string   `json:"redis_password"`
	RedisDB          int      `json:"redis_db"`           // standalone mode
	MasterName       string   `json:"master_name"`        // sentinel mode
	SentinelAddrs    []string `json:"sentinel_addrs"`     // sentinel mode
	SentinelPassword string   `json:"sentinel_password"`  // sentinel mode
	ClusterAddrs     []string `json:"cluster_addrs"`      // cluster mode
}

// EnableDistributedResult is returned after processing an enable-distributed request.
type EnableDistributedResult struct {
	ConfigSnippet   string `json:"config_snippet"`
	RestartRequired bool   `json:"restart_required"`
	Message         string `json:"message"`
}
