package models

import (
	"time"
)

// DataRecord 数据记录模型
type DataRecord struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// TableConfig 表配置模型
type TableConfig struct {
	BufferSize           int               `json:"buffer_size"`
	FlushIntervalSeconds int               `json:"flush_interval_seconds"`
	RetentionDays        int               `json:"retention_days"`
	BackupEnabled        bool              `json:"backup_enabled"`
	Properties           map[string]string `json:"properties,omitempty"`
}

// TableStats 表统计信息模型
type TableStats struct {
	RecordCount  int64     `json:"record_count"`
	FileCount    int64     `json:"file_count"`
	SizeBytes    int64     `json:"size_bytes"`
	OldestRecord time.Time `json:"oldest_record"`
	NewestRecord time.Time `json:"newest_record"`
}

// TableInfo 表信息模型
type TableInfo struct {
	Name      string       `json:"name"`
	Config    *TableConfig `json:"config"`
	CreatedAt time.Time    `json:"created_at"`
	LastWrite *time.Time   `json:"last_write,omitempty"`
	Status    string       `json:"status"`
	Stats     *TableStats  `json:"stats,omitempty"`
}

// BackupInfo 备份信息模型
type BackupInfo struct {
	ObjectName   string    `json:"object_name"`
	NodeID       string    `json:"node_id"`
	Timestamp    time.Time `json:"timestamp"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// NodeInfo 节点信息模型
type NodeInfo struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Address  string `json:"address"`
	LastSeen int64  `json:"last_seen"`
}

// 响应模型

// WriteDataResponse 写入数据响应
type WriteDataResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

// QueryDataResponse 查询数据响应
type QueryDataResponse struct {
	ResultJSON string `json:"result_json"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// UpdateDataResponse 更新数据响应
type UpdateDataResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

// DeleteDataResponse 删除数据响应
type DeleteDataResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	DeletedCount int32  `json:"deleted_count"`
}

// StreamWriteResponse 流式写入响应
type StreamWriteResponse struct {
	Success      bool     `json:"success"`
	RecordsCount int64    `json:"records_count"`
	Errors       []string `json:"errors,omitempty"`
}

// StreamQueryResponse 流式查询响应
type StreamQueryResponse struct {
	Records []*DataRecord `json:"records"`
	HasMore bool          `json:"has_more"`
	Cursor  string        `json:"cursor,omitempty"`
}

// CreateTableResponse 创建表响应
type CreateTableResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ListTablesResponse 列出表响应
type ListTablesResponse struct {
	Tables []*TableInfo `json:"tables"`
	Total  int32        `json:"total"`
}

// GetTableResponse 获取表响应
type GetTableResponse struct {
	TableInfo *TableInfo `json:"table_info"`
}

// DeleteTableResponse 删除表响应
type DeleteTableResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	FilesDeleted int32  `json:"files_deleted"`
}

// BackupMetadataResponse 备份元数据响应
type BackupMetadataResponse struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	BackupID  string    `json:"backup_id"`
	Timestamp time.Time `json:"timestamp"`
}

// RestoreMetadataRequest 恢复元数据请求
type RestoreMetadataRequest struct {
	BackupFile  string            `json:"backup_file"`
	FromLatest  bool              `json:"from_latest"`
	DryRun      bool              `json:"dry_run"`
	Overwrite   bool              `json:"overwrite"`
	Validate    bool              `json:"validate"`
	Parallel    bool              `json:"parallel"`
	Filters     map[string]string `json:"filters,omitempty"`
	KeyPatterns []string          `json:"key_patterns,omitempty"`
}

// RestoreMetadataResponse 恢复元数据响应
type RestoreMetadataResponse struct {
	Success        bool              `json:"success"`
	Message        string            `json:"message"`
	BackupFile     string            `json:"backup_file"`
	EntriesTotal   int32             `json:"entries_total"`
	EntriesOK      int32             `json:"entries_ok"`
	EntriesSkipped int32             `json:"entries_skipped"`
	EntriesError   int32             `json:"entries_error"`
	Duration       string            `json:"duration"`
	Errors         []string          `json:"errors,omitempty"`
	Details        map[string]string `json:"details,omitempty"`
}

// ListBackupsResponse 列出备份响应
type ListBackupsResponse struct {
	Backups []*BackupInfo `json:"backups"`
	Total   int32         `json:"total"`
}

// GetMetadataStatusResponse 获取元数据状态响应
type GetMetadataStatusResponse struct {
	NodeID       string            `json:"node_id"`
	BackupStatus map[string]string `json:"backup_status"`
	LastBackup   *time.Time        `json:"last_backup,omitempty"`
	NextBackup   *time.Time        `json:"next_backup,omitempty"`
	HealthStatus string            `json:"health_status"`
}

// HealthCheckResponse 健康检查响应
type HealthCheckResponse struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Version   string            `json:"version"`
	Details   map[string]string `json:"details,omitempty"`
}

// GetStatusResponse 获取状态响应
type GetStatusResponse struct {
	Timestamp   time.Time        `json:"timestamp"`
	BufferStats map[string]int64 `json:"buffer_stats"`
	RedisStats  map[string]int64 `json:"redis_stats"`
	MinIOStats  map[string]int64 `json:"minio_stats"`
	Nodes       []*NodeInfo      `json:"nodes"`
	TotalNodes  int32            `json:"total_nodes"`
}

// GetMetricsResponse 获取指标响应
type GetMetricsResponse struct {
	Timestamp          time.Time          `json:"timestamp"`
	PerformanceMetrics map[string]float64 `json:"performance_metrics"`
	ResourceUsage      map[string]int64   `json:"resource_usage"`
	SystemInfo         map[string]string  `json:"system_info"`
}

// GetTokenResponse 获取令牌响应
type GetTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// RefreshTokenResponse 刷新令牌响应
type RefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// RevokeTokenResponse 撤销令牌响应
type RevokeTokenResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
