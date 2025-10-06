package monitoring

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector()
	assert.NotNil(t, collector, "MetricsCollector should not be nil")
	assert.NotZero(t, collector.startTime, "Start time should be set")
}

func TestRecordHTTPRequest(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	method := "GET"
	endpoint := "/test"
	statusCode := 200
	duration := 100 * time.Millisecond

	// This should not panic
	assert.NotPanics(t, func() {
		RecordHTTPRequest(method, endpoint, statusCode, duration)
	})
}

func TestRecordGRPCRequest(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	method := "/test.Service/Method"
	status := "success"
	duration := 50 * time.Millisecond

	// This should not panic
	assert.NotPanics(t, func() {
		RecordGRPCRequest(method, status, duration)
	})
}

func TestRecordMinIOOperation(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	operation := "put"
	status := "success"
	duration := 200 * time.Millisecond

	// This should not panic
	assert.NotPanics(t, func() {
		RecordMinIOOperation(operation, status, duration)
	})
}

func TestRecordRedisOperation(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	operation := "set"
	status := "success"
	duration := 10 * time.Millisecond

	// This should not panic
	assert.NotPanics(t, func() {
		RecordRedisOperation(operation, status, duration)
	})
}

func TestRecordQuery(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	queryType := "select"
	status := "success"
	duration := 150 * time.Millisecond

	// This should not panic
	assert.NotPanics(t, func() {
		RecordQuery(queryType, status, duration)
	})
}

func TestRecordDataWrite(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	status := "success"
	size := int64(1024)

	// This should not panic
	assert.NotPanics(t, func() {
		RecordDataWrite(status, size)
	})
}

func TestUpdateBufferSize(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	bufferType := "memory"
	size := int64(2048)

	// This should not panic
	assert.NotPanics(t, func() {
		UpdateBufferSize(bufferType, size)
	})
}

func TestRecordBufferFlush(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	bufferType := "disk"
	status := "success"

	// This should not panic
	assert.NotPanics(t, func() {
		RecordBufferFlush(bufferType, status)
	})
}

func TestUpdateConnectionPool(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	poolType := "redis"
	active := 5
	idle := 10

	// This should not panic
	assert.NotPanics(t, func() {
		UpdateConnectionPool(poolType, active, idle)
	})
}

func TestRecordHealthCheck(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	component := "redis"
	duration := 5 * time.Millisecond

	// Test healthy
	assert.NotPanics(t, func() {
		RecordHealthCheck(component, true, duration)
	})

	// Test unhealthy
	assert.NotPanics(t, func() {
		RecordHealthCheck(component, false, duration)
	})
}

func TestUpdateClusterMetrics(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	totalNodes := 3
	healthyNodes := 2

	// This should not panic
	assert.NotPanics(t, func() {
		UpdateClusterMetrics(totalNodes, healthyNodes)
	})
}

func TestGetMetricsHandler(t *testing.T) {
	handler := GetMetricsHandler()
	assert.NotNil(t, handler, "Metrics handler should not be nil")

	// Test that it's a proper HTTP handler
	req := httptest.NewRequest("GET", "/metrics", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.NotEmpty(t, string(body))

	// Should contain some Prometheus metrics content
	assert.Contains(t, string(body), "# HELP", "Response should contain Prometheus help text")
	assert.Contains(t, string(body), "# TYPE", "Response should contain Prometheus type information")
}

func TestUpdateIndexSystemHitRate(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	indexType := "btree"
	hitRate := 0.85

	// This should not panic
	assert.NotPanics(t, func() {
		UpdateIndexSystemHitRate(indexType, hitRate)
	})
}

func TestUpdateMemoryOptimizerUsage(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	poolType := "query"
	usage := int64(4096)

	// This should not panic
	assert.NotPanics(t, func() {
		UpdateMemoryOptimizerUsage(poolType, usage)
	})
}

func TestUpdateStorageEnginePerformance(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	metricName := "throughput"
	value := 1000.5

	// This should not panic
	assert.NotPanics(t, func() {
		UpdateStorageEnginePerformance(metricName, value)
	})
}

func TestRecordTableACLCheck(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	table := "users"
	permission := "read"
	result := "allowed"

	// This should not panic
	assert.NotPanics(t, func() {
		RecordTableACLCheck(table, permission, result)
	})
}

func TestUpdatePerformanceBottleneck(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	component := "query"
	bottleneckType := "cpu"

	// Test detected
	assert.NotPanics(t, func() {
		UpdatePerformanceBottleneck(component, bottleneckType, true)
	})

	// Test not detected
	assert.NotPanics(t, func() {
		UpdatePerformanceBottleneck(component, bottleneckType, false)
	})
}

func TestMetricsCollector_SystemMetricsUpdate(t *testing.T) {
	collector := NewMetricsCollector()
	assert.NotNil(t, collector)

	// Wait a short time for system metrics to update
	time.Sleep(100 * time.Millisecond)

	// The collector should be running and updating system metrics
	// We can't easily test the actual values without mocking time,
	// but we can verify the collector was created properly
	assert.True(t, time.Since(collector.startTime) > 0, "Start time should be in the past")
}

func TestMultipleRecordCalls(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	// Test multiple calls to ensure no panics
	for i := 0; i < 5; i++ {
		assert.NotPanics(t, func() {
			RecordHTTPRequest("GET", "/test", 200, 100*time.Millisecond)
			RecordGRPCRequest("/test.Service/Method", "success", 50*time.Millisecond)
			RecordQuery("select", "success", 150*time.Millisecond)
		})
	}
}

func TestMetricsHandlerWithDefaultPrometheusHandler(t *testing.T) {
	handler := GetMetricsHandler()

	// Verify it's using the default Prometheus handler
	assert.IsType(t, http.Handler(nil), handler)
}

func TestRecordWithDifferentStatusCodes(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	statusCodes := []int{200, 400, 404, 500, 503}
	method := "GET"
	endpoint := "/test"
	duration := 100 * time.Millisecond

	for _, code := range statusCodes {
		assert.NotPanics(t, func() {
			RecordHTTPRequest(method, endpoint, code, duration)
		})
	}
}

func TestRecordWithDifferentOperations(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	minioOperations := []string{"get", "put", "delete", "list"}
	redisOperations := []string{"get", "set", "del", "hget", "hset"}
	queryTypes := []string{"select", "insert", "update", "delete"}

	duration := 50 * time.Millisecond

	for _, op := range minioOperations {
		assert.NotPanics(t, func() {
			RecordMinIOOperation(op, "success", duration)
		})
	}

	for _, op := range redisOperations {
		assert.NotPanics(t, func() {
			RecordRedisOperation(op, "success", duration)
		})
	}

	for _, qt := range queryTypes {
		assert.NotPanics(t, func() {
			RecordQuery(qt, "success", duration)
		})
	}
}

func TestRecordWithEmptyStrings(t *testing.T) {
	// Initialize metrics collector to register metrics
	_ = NewMetricsCollector()

	// Test with empty strings - should not panic
	assert.NotPanics(t, func() {
		RecordHTTPRequest("", "", 200, 100*time.Millisecond)
		RecordGRPCRequest("", "", 50*time.Millisecond)
		RecordMinIOOperation("", "", 50*time.Millisecond)
		RecordQuery("", "", 50*time.Millisecond)
		RecordDataWrite("", 0)
		UpdateBufferSize("", 0)
		RecordBufferFlush("", "")
		UpdateConnectionPool("", 0, 0)
		RecordHealthCheck("", true, 50*time.Millisecond)
	})
}

func TestMetricsHandlerContentType(t *testing.T) {
	handler := GetMetricsHandler()

	req := httptest.NewRequest("GET", "/metrics", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	contentType := resp.Header.Get("Content-Type")
	assert.Contains(t, contentType, "text/plain", "Content-Type should be text/plain")
}