package monitoring

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP请求指标
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	// gRPC请求指标
	grpcRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_grpc_requests_total",
			Help: "Total number of gRPC requests",
		},
		[]string{"method", "status"},
	)

	grpcRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)

	// MinIO操作指标
	minioOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_minio_operations_total",
			Help: "Total number of MinIO operations",
		},
		[]string{"operation", "status"},
	)

	minioOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_minio_operation_duration_seconds",
			Help:    "MinIO operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// Redis操作指标
	redisOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_redis_operations_total",
			Help: "Total number of Redis operations",
		},
		[]string{"operation", "status"},
	)

	redisOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_redis_operation_duration_seconds",
			Help:    "Redis operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// 查询指标
	queryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_queries_total",
			Help: "Total number of queries executed",
		},
		[]string{"type", "status"},
	)

	queryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_query_duration_seconds",
			Help:    "Query execution duration in seconds",
			Buckets: []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0},
		},
		[]string{"type"},
	)

	// 数据写入指标
	dataWritesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_data_writes_total",
			Help: "Total number of data writes",
		},
		[]string{"status"},
	)

	dataWriteSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_data_write_size_bytes",
			Help:    "Size of data writes in bytes",
			Buckets: []float64{1024, 4096, 16384, 65536, 262144, 1048576, 4194304},
		},
		[]string{},
	)

	// 缓冲区指标
	bufferSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "miniodb_buffer_size_bytes",
			Help: "Current buffer size in bytes",
		},
		[]string{"buffer_type"},
	)

	bufferFlushes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_buffer_flushes_total",
			Help: "Total number of buffer flushes",
		},
		[]string{"buffer_type", "status"},
	)

	// 连接池指标
	connectionPoolActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "miniodb_connection_pool_active",
			Help: "Number of active connections in pool",
		},
		[]string{"pool_type"},
	)

	connectionPoolIdle = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "miniodb_connection_pool_idle",
			Help: "Number of idle connections in pool",
		},
		[]string{"pool_type"},
	)

	// 健康检查指标
	healthCheckStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "miniodb_health_check_status",
			Help: "Health check status (1=healthy, 0=unhealthy)",
		},
		[]string{"component"},
	)

	healthCheckDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_health_check_duration_seconds",
			Help:    "Health check duration in seconds",
			Buckets: []float64{0.001, 0.01, 0.1, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"component"},
	)

	// 系统指标
	systemUptime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_system_uptime_seconds",
			Help: "System uptime in seconds",
		},
	)

	// 集群指标
	clusterNodesTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_cluster_nodes_total",
			Help: "Total number of nodes in cluster",
		},
	)

	clusterNodesHealthy = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_cluster_nodes_healthy",
			Help: "Number of healthy nodes in cluster",
		},
	)
)

// MetricsCollector 指标收集器
type MetricsCollector struct {
	startTime time.Time
}

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector() *MetricsCollector {
	// 注册所有指标
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		grpcRequestsTotal,
		grpcRequestDuration,
		minioOperationsTotal,
		minioOperationDuration,
		redisOperationsTotal,
		redisOperationDuration,
		queryTotal,
		queryDuration,
		dataWritesTotal,
		dataWriteSize,
		bufferSize,
		bufferFlushes,
		connectionPoolActive,
		connectionPoolIdle,
		healthCheckStatus,
		healthCheckDuration,
		systemUptime,
		clusterNodesTotal,
		clusterNodesHealthy,
	)

	collector := &MetricsCollector{
		startTime: time.Now(),
	}

	// 启动系统指标更新
	go collector.updateSystemMetrics()

	return collector
}

// updateSystemMetrics 更新系统指标
func (mc *MetricsCollector) updateSystemMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			systemUptime.Set(time.Since(mc.startTime).Seconds())
		}
	}
}

// RecordHTTPRequest 记录HTTP请求
func RecordHTTPRequest(method, endpoint string, statusCode int, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, endpoint, strconv.Itoa(statusCode)).Inc()
	httpRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// RecordGRPCRequest 记录gRPC请求
func RecordGRPCRequest(method, status string, duration time.Duration) {
	grpcRequestsTotal.WithLabelValues(method, status).Inc()
	grpcRequestDuration.WithLabelValues(method).Observe(duration.Seconds())
}

// RecordMinIOOperation 记录MinIO操作
func RecordMinIOOperation(operation, status string, duration time.Duration) {
	minioOperationsTotal.WithLabelValues(operation, status).Inc()
	minioOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordRedisOperation 记录Redis操作
func RecordRedisOperation(operation, status string, duration time.Duration) {
	redisOperationsTotal.WithLabelValues(operation, status).Inc()
	redisOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordQuery 记录查询
func RecordQuery(queryType, status string, duration time.Duration) {
	queryTotal.WithLabelValues(queryType, status).Inc()
	queryDuration.WithLabelValues(queryType).Observe(duration.Seconds())
}

// RecordDataWrite 记录数据写入
func RecordDataWrite(status string, size int64) {
	dataWritesTotal.WithLabelValues(status).Inc()
	dataWriteSize.WithLabelValues().Observe(float64(size))
}

// UpdateBufferSize 更新缓冲区大小
func UpdateBufferSize(bufferType string, size int64) {
	bufferSize.WithLabelValues(bufferType).Set(float64(size))
}

// RecordBufferFlush 记录缓冲区刷新
func RecordBufferFlush(bufferType, status string) {
	bufferFlushes.WithLabelValues(bufferType, status).Inc()
}

// UpdateConnectionPool 更新连接池指标
func UpdateConnectionPool(poolType string, active, idle int) {
	connectionPoolActive.WithLabelValues(poolType).Set(float64(active))
	connectionPoolIdle.WithLabelValues(poolType).Set(float64(idle))
}

// RecordHealthCheck 记录健康检查
func RecordHealthCheck(component string, healthy bool, duration time.Duration) {
	status := 0.0
	if healthy {
		status = 1.0
	}
	healthCheckStatus.WithLabelValues(component).Set(status)
	healthCheckDuration.WithLabelValues(component).Observe(duration.Seconds())
}

// UpdateClusterMetrics 更新集群指标
func UpdateClusterMetrics(totalNodes, healthyNodes int) {
	clusterNodesTotal.Set(float64(totalNodes))
	clusterNodesHealthy.Set(float64(healthyNodes))
}

// GetMetricsHandler 获取Prometheus指标处理器
func GetMetricsHandler() http.Handler {
	return promhttp.Handler()
} 