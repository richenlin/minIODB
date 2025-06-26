package metrics

import (
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP请求指标
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "olap_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	// gRPC请求指标
	grpcRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_grpc_requests_total",
			Help: "Total number of gRPC requests",
		},
		[]string{"method", "status"},
	)

	grpcRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "olap_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)

	// 数据写入指标
	dataWritesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_data_writes_total",
			Help: "Total number of data writes",
		},
		[]string{"status"},
	)

	dataWriteSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "olap_data_write_size_bytes",
			Help:    "Size of data writes in bytes",
			Buckets: []float64{1024, 4096, 16384, 65536, 262144, 1048576, 4194304},
		},
		[]string{},
	)

	// 查询指标
	queriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_queries_total",
			Help: "Total number of queries",
		},
		[]string{"type", "status"},
	)

	queryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "olap_query_duration_seconds",
			Help:    "Query execution duration in seconds",
			Buckets: []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0},
		},
		[]string{"type"},
	)

	// 缓冲区指标
	bufferSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "olap_buffer_size",
			Help: "Current buffer size",
		},
		[]string{"buffer_type"},
	)

	bufferFlushesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_buffer_flushes_total",
			Help: "Total number of buffer flushes",
		},
		[]string{"status"},
	)

	// MinIO指标
	minioOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_minio_operations_total",
			Help: "Total number of MinIO operations",
		},
		[]string{"operation", "status"},
	)

	minioOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "olap_minio_operation_duration_seconds",
			Help:    "MinIO operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// Redis指标
	redisOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_redis_operations_total",
			Help: "Total number of Redis operations",
		},
		[]string{"operation", "status"},
	)

	redisOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "olap_redis_operation_duration_seconds",
			Help:    "Redis operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// 系统指标
	activeConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_active_connections",
			Help: "Number of active connections",
		},
	)

	nodeHealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "olap_node_health_status",
			Help: "Node health status (1 = healthy, 0 = unhealthy)",
		},
		[]string{"node_id"},
	)

	// 系统资源指标
	memoryUsage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		},
	)

	cpuUsage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_cpu_usage_percent",
			Help: "Current CPU usage percentage",
		},
	)

	diskUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "olap_disk_usage_bytes",
			Help: "Current disk usage in bytes",
		},
		[]string{"path"},
	)

	goroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_goroutines_count",
			Help: "Current number of goroutines",
		},
	)

	// Panic recovery metrics
	panicRecovered = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "panic_recovered_total",
			Help: "Total number of panics recovered",
		},
		[]string{"component"},
	)
)

func init() {
	// Register panic recovery metrics
	prometheus.MustRegister(panicRecovered)
}

// HTTPMetrics HTTP请求指标记录器
type HTTPMetrics struct {
	method   string
	endpoint string
	start    time.Time
}

// NewHTTPMetrics 创建HTTP指标记录器
func NewHTTPMetrics(method, endpoint string) *HTTPMetrics {
	return &HTTPMetrics{
		method:   method,
		endpoint: endpoint,
		start:    time.Now(),
	}
}

// Finish 完成HTTP请求指标记录
func (m *HTTPMetrics) Finish(status string) {
	duration := time.Since(m.start).Seconds()
	httpRequestsTotal.WithLabelValues(m.method, m.endpoint, status).Inc()
	httpRequestDuration.WithLabelValues(m.method, m.endpoint).Observe(duration)
}

// GRPCMetrics gRPC请求指标记录器
type GRPCMetrics struct {
	method string
	start  time.Time
}

// NewGRPCMetrics 创建gRPC指标记录器
func NewGRPCMetrics(method string) *GRPCMetrics {
	return &GRPCMetrics{
		method: method,
		start:  time.Now(),
	}
}

// Finish 完成gRPC请求指标记录
func (m *GRPCMetrics) Finish(status string) {
	duration := time.Since(m.start).Seconds()
	grpcRequestsTotal.WithLabelValues(m.method, status).Inc()
	grpcRequestDuration.WithLabelValues(m.method).Observe(duration)
}

// RecordDataWrite 记录数据写入指标
func RecordDataWrite(status string, size int64) {
	dataWritesTotal.WithLabelValues(status).Inc()
	dataWriteSize.WithLabelValues().Observe(float64(size))
}

// QueryMetrics 查询指标记录器
type QueryMetrics struct {
	queryType string
	start     time.Time
}

// NewQueryMetrics 创建查询指标记录器
func NewQueryMetrics(queryType string) *QueryMetrics {
	return &QueryMetrics{
		queryType: queryType,
		start:     time.Now(),
	}
}

// Finish 完成查询指标记录
func (m *QueryMetrics) Finish(status string) {
	duration := time.Since(m.start).Seconds()
	queriesTotal.WithLabelValues(m.queryType, status).Inc()
	queryDuration.WithLabelValues(m.queryType).Observe(duration)
}

// UpdateBufferSize 更新缓冲区大小指标
func UpdateBufferSize(bufferType string, size float64) {
	bufferSize.WithLabelValues(bufferType).Set(size)
}

// RecordBufferFlush 记录缓冲区刷新指标
func RecordBufferFlush(status string) {
	bufferFlushesTotal.WithLabelValues(status).Inc()
}

// MinIOMetrics MinIO操作指标记录器
type MinIOMetrics struct {
	operation string
	start     time.Time
}

// NewMinIOMetrics 创建MinIO指标记录器
func NewMinIOMetrics(operation string) *MinIOMetrics {
	return &MinIOMetrics{
		operation: operation,
		start:     time.Now(),
	}
}

// Finish 完成MinIO操作指标记录
func (m *MinIOMetrics) Finish(status string) {
	duration := time.Since(m.start).Seconds()
	minioOperationsTotal.WithLabelValues(m.operation, status).Inc()
	minioOperationDuration.WithLabelValues(m.operation).Observe(duration)
}

// RedisMetrics Redis操作指标记录器
type RedisMetrics struct {
	operation string
	start     time.Time
}

// NewRedisMetrics 创建Redis指标记录器
func NewRedisMetrics(operation string) *RedisMetrics {
	return &RedisMetrics{
		operation: operation,
		start:     time.Now(),
	}
}

// Finish 完成Redis操作指标记录
func (m *RedisMetrics) Finish(status string) {
	duration := time.Since(m.start).Seconds()
	redisOperationsTotal.WithLabelValues(m.operation, status).Inc()
	redisOperationDuration.WithLabelValues(m.operation).Observe(duration)
}

// UpdateActiveConnections 更新活跃连接数
func UpdateActiveConnections(count float64) {
	activeConnections.Set(count)
}

// UpdateNodeHealthStatus 更新节点健康状态
func UpdateNodeHealthStatus(nodeID string, healthy bool) {
	status := 0.0
	if healthy {
		status = 1.0
	}
	nodeHealthStatus.WithLabelValues(nodeID).Set(status)
}

// UpdateMemoryUsage 更新内存使用指标
func UpdateMemoryUsage(usage float64) {
	memoryUsage.Set(usage)
}

// UpdateCPUUsage 更新CPU使用指标
func UpdateCPUUsage(usage float64) {
	cpuUsage.Set(usage)
}

// UpdateDiskUsage 更新磁盘使用指标
func UpdateDiskUsage(path string, usage float64) {
	diskUsage.WithLabelValues(path).Set(usage)
}

// UpdateGoroutineCount 更新goroutine数量指标
func UpdateGoroutineCount(count float64) {
	goroutineCount.Set(count)
}

// Handler 返回Prometheus指标处理器
func Handler() http.Handler {
	return promhttp.Handler()
}

// SystemMonitor 系统监控器
type SystemMonitor struct {
	stopCh chan struct{}
}

// NewSystemMonitor 创建系统监控器
func NewSystemMonitor() *SystemMonitor {
	return &SystemMonitor{
		stopCh: make(chan struct{}),
	}
}

// Start 启动系统监控
func (sm *SystemMonitor) Start() {
	go sm.collectSystemMetrics()
}

// Stop 停止系统监控
func (sm *SystemMonitor) Stop() {
	close(sm.stopCh)
}

// collectSystemMetrics 收集系统指标
func (sm *SystemMonitor) collectSystemMetrics() {
	ticker := time.NewTicker(10 * time.Second) // 每10秒收集一次
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopCh:
			return
		case <-ticker.C:
			sm.updateMetrics()
		}
	}
}

// updateMetrics 更新系统指标
func (sm *SystemMonitor) updateMetrics() {
	// 更新内存使用情况
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	UpdateMemoryUsage(float64(memStats.Alloc))

	// 更新goroutine数量
	UpdateGoroutineCount(float64(runtime.NumGoroutine()))

	// 注意：CPU和磁盘使用情况需要更复杂的实现
	// 这里只是示例，实际生产环境可能需要使用第三方库
}

// IncPanicRecovered 增加 panic 恢复计数
func IncPanicRecovered(component string) {
	panicRecovered.WithLabelValues(component).Inc()
}
