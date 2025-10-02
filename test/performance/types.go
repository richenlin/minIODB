package performance

import (
	"time"
)

// 通用API响应结构
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
	Code    int         `json:"code"`
}

// 查询请求结构
type QueryRequest struct {
	SQL string `json:"sql"`
}

// 查询响应结构
type QueryResponse struct {
	Data      []map[string]interface{} `json:"data" yaml:"data"`
	Rows      int                      `json:"rows" yaml:"rows"`
	Columns   []string                 `json:"columns" yaml:"columns"`
	Count     int                      `json:"count" yaml:"count"`
	Elapsed   string                   `json:"elapsed" yaml:"elapsed"`
	Success   bool                     `json:"success" yaml:"success"`
	Message   string                   `json:"message" yaml:"message"`
	Error     string                   `json:"error,omitempty" yaml:"error,omitempty"`
	Timestamp string                   `json:"timestamp" yaml:"timestamp"`
}

// 创建表请求结构
type CreateTableRequest struct {
	TableName   string       `json:"table_name"`
	Config      *TableConfig `json:"config,omitempty"`
	IfNotExists bool         `json:"if_not_exists,omitempty"`
}

// 表配置结构
type TableConfig struct {
	BufferSize           int32             `json:"buffer_size,omitempty"`
	FlushIntervalSeconds int32             `json:"flush_interval_seconds,omitempty"`
	RetentionDays        int32             `json:"retention_days,omitempty"`
	BackupEnabled        bool              `json:"backup_enabled,omitempty"`
	Properties           map[string]string `json:"properties,omitempty"`
}

// 插入数据请求结构 (匹配REST API的WriteRequest)
type InsertDataRequest struct {
	Table     string                 `json:"table,omitempty"`
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// 写入响应结构
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// 测试数据结构
type TestData struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
	Table     string                 `json:"table,omitempty"`
}

// 测试记录结构
type TestRecord struct {
	ID        int                    `json:"id"`
	Name      string                 `json:"name"`
	Email     string                 `json:"email"`
	Age       int                    `json:"age"`
	Score     float64                `json:"score"`
	IsActive  bool                   `json:"is_active"`
	Tags      []string               `json:"tags"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// 并发测试配置结构
type ConcurrentTestConfig struct {
	ReadUsers    int           `yaml:"read_users"`
	WriteUsers   int           `yaml:"write_users"`
	TestDuration time.Duration `yaml:"test_duration"`
	RampUp       time.Duration `yaml:"ramp_up"`
	QueryTypes   []string      `yaml:"query_types"`
	BatchSize    int           `yaml:"batch_size"`
	RecordSize   int           `yaml:"record_size"`
	ReadWeight   float64       `yaml:"read_weight"`
	WriteWeight  float64       `yaml:"write_weight"`
}

// 并发测试结果结构
type ConcurrentTestResult struct {
	TestName          string        `json:"test_name"`
	ReadUsers         int           `json:"read_users"`
	WriteUsers        int           `json:"write_users"`
	TestDuration      time.Duration `json:"test_duration"`
	ConcurrentUsers   int           `json:"concurrent_users"`
	Duration          time.Duration `json:"duration"`
	TotalQueries      int64         `json:"total_queries"`
	SuccessfulQueries int64         `json:"successful_queries"`
	FailedQueries     int64         `json:"failed_queries"`
	TotalInserts      int64         `json:"total_inserts"`
	SuccessfulInserts int64         `json:"successful_inserts"`
	FailedInserts     int64         `json:"failed_inserts"`
	QPS               float64       `json:"qps"`
	IPS               float64       `json:"ips"`
	AvgQueryLatency   time.Duration `json:"avg_query_latency"`
	AvgInsertLatency  time.Duration `json:"avg_insert_latency"`
	MaxQueryLatency   time.Duration `json:"max_query_latency"`
	MaxInsertLatency  time.Duration `json:"max_insert_latency"`
	MinLatency        time.Duration `json:"min_latency"`
	ErrorRate         float64       `json:"error_rate"`
	StartTime         time.Time     `json:"start_time"`
	EndTime           time.Time     `json:"end_time"`
}

// TB级测试配置结构
type TBScaleTestConfig struct {
	TableName       string `yaml:"table_name"`
	RecordCount     int    `yaml:"record_count"`
	BatchSize       int    `yaml:"batch_size"`
	TargetSize      int    `yaml:"target_size"`
	MaxDuration     int    `yaml:"max_duration"`
	Description     string `yaml:"description"`
	ParallelWorkers int    `yaml:"parallel_workers"`
	PartitionCount  int    `yaml:"partition_count"`
	PartitionKey    string `yaml:"partition_key"`
}

// TB级查询测试用例结构
type TBQueryTestCase struct {
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	SQL          string        `yaml:"sql"`
	TargetTime   time.Duration `yaml:"target_time"`
	ExpectedRows int           `yaml:"expected_rows"`
	Category     string        `yaml:"category"`
}

// TB级测试结果结构
type TBScaleTestResult struct {
	TableName         string        `json:"table_name"`
	ActualRecords     int64         `json:"actual_records"`
	TotalDuration     time.Duration `json:"total_duration"`
	IngestionRate     float64       `json:"ingestion_rate"`
	DataSizeMB        float64       `json:"data_size_mb"`
	ThroughputMBPS    float64       `json:"throughput_mbps"`
	SuccessfulBatches int64         `json:"successful_batches"`
	FailedBatches     int64         `json:"failed_batches"`
	ErrorRate         float64       `json:"error_rate"`
	TotalQueries      int64         `json:"total_queries"`
	SuccessfulQueries int64         `json:"successful_queries"`
	FailedQueries     int64         `json:"failed_queries"`
	QPS               float64       `json:"qps"`
	AvgLatency        time.Duration `json:"avg_latency"`
	P95Latency        time.Duration `json:"p95_latency"`
	P99Latency        time.Duration `json:"p99_latency"`
	MaxLatency        time.Duration `json:"max_latency"`
}

// 性能测试配置
type PerformanceConfig struct {
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Name     string `yaml:"name"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
	} `yaml:"database"`

	Tests struct {
		BasicPerformance struct {
			Enabled     bool `yaml:"enabled"`
			RecordCount int  `yaml:"record_count"`
			BatchSize   int  `yaml:"batch_size"`
		} `yaml:"basic_performance"`

		Concurrent ConcurrentTestConfig `yaml:"concurrent"`

		TBScale struct {
			Enabled bool                `yaml:"enabled"`
			Configs []TBScaleTestConfig `yaml:"configs"`
		} `yaml:"tb_scale"`
	} `yaml:"tests"`

	Monitoring struct {
		Enabled         bool          `yaml:"enabled"`
		Interval        time.Duration `yaml:"interval"`
		MetricsEndpoint string        `yaml:"metrics_endpoint"`
	} `yaml:"monitoring"`
}

// 常量定义
const (
	TestTableName = "performance_test_table"
	BaseURL       = "http://localhost:8080"
)
