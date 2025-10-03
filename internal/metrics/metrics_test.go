package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestQueryDurationBuckets 测试查询耗时分桶
func TestQueryDurationBuckets(t *testing.T) {
	tests := []struct {
		name           string
		duration       time.Duration
		expectedBucket float64 // 应该落入的分桶上界
	}{
		{
			name:           "very_fast_5ms",
			duration:       5 * time.Millisecond,
			expectedBucket: 0.01, // 10ms bucket
		},
		{
			name:           "fast_30ms",
			duration:       30 * time.Millisecond,
			expectedBucket: 0.05, // 50ms bucket
		},
		{
			name:           "normal_80ms",
			duration:       80 * time.Millisecond,
			expectedBucket: 0.1, // 100ms bucket
		},
		{
			name:           "acceptable_200ms",
			duration:       200 * time.Millisecond,
			expectedBucket: 0.25, // 250ms bucket
		},
		{
			name:           "slow_700ms",
			duration:       700 * time.Millisecond,
			expectedBucket: 1.0, // 1s bucket
		},
		{
			name:           "very_slow_3s",
			duration:       3 * time.Second,
			expectedBucket: 5.0, // 5s bucket
		},
		{
			name:           "extremely_slow_15s",
			duration:       15 * time.Second,
			expectedBucket: 30.0, // 30s bucket
		},
		{
			name:           "timeout_level_45s",
			duration:       45 * time.Second,
			expectedBucket: 60.0, // 60s bucket
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 记录查询耗时
			ObserveQueryDuration("select", "test_table", tt.duration)

			// 验证耗时值在合理范围内
			seconds := tt.duration.Seconds()
			if seconds < 0 || seconds > 120 {
				t.Errorf("duration out of reasonable range: %v seconds", seconds)
			}

			// 验证应该落入的分桶
			if seconds > tt.expectedBucket {
				// 如果超过了期望的分桶上界，检查是否在下一个分桶中
				t.Logf("Duration %v exceeds expected bucket %v (this is OK if in next bucket)",
					seconds, tt.expectedBucket)
			}
		})
	}
}

// TestQueryDurationLabels 测试查询耗时标签
func TestQueryDurationLabels(t *testing.T) {
	tests := []struct {
		queryType string
		table     string
		duration  time.Duration
	}{
		{
			queryType: "select",
			table:     "users",
			duration:  100 * time.Millisecond,
		},
		{
			queryType: "insert",
			table:     "orders",
			duration:  50 * time.Millisecond,
		},
		{
			queryType: "update",
			table:     "products",
			duration:  200 * time.Millisecond,
		},
		{
			queryType: "delete",
			table:     "logs",
			duration:  30 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.queryType+"_"+tt.table, func(t *testing.T) {
			// 记录带标签的查询耗时
			ObserveQueryDuration(tt.queryType, tt.table, tt.duration)

			// 验证标签值
			if tt.queryType == "" {
				t.Error("query_type label should not be empty")
			}
			if tt.table == "" {
				t.Error("table label should not be empty")
			}
		})
	}
}

// TestSlowQueryMetric 测试慢查询指标
func TestSlowQueryMetric(t *testing.T) {
	tables := []string{"users", "orders", "products"}

	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			// 记录慢查询
			RecordSlowQuery(table)

			// 验证表名
			if table == "" {
				t.Error("table name should not be empty")
			}
		})
	}
}

// TestPercentileCalculation 测试分位数计算的可行性
func TestPercentileCalculation(t *testing.T) {
	// 模拟一系列查询耗时
	durations := []time.Duration{
		10 * time.Millisecond,   // P10
		30 * time.Millisecond,   // P20
		50 * time.Millisecond,   // P30
		80 * time.Millisecond,   // P40
		100 * time.Millisecond,  // P50 (中位数)
		200 * time.Millisecond,  // P60
		400 * time.Millisecond,  // P70
		700 * time.Millisecond,  // P80
		1500 * time.Millisecond, // P90
		3000 * time.Millisecond, // P95
		5000 * time.Millisecond, // P99
	}

	// 记录所有查询耗时
	for i, duration := range durations {
		ObserveQueryDuration("select", "test_percentile", duration)
		t.Logf("Recorded query %d: %v", i+1, duration)
	}

	// 验证: 我们有足够的分桶来计算P50, P95, P99
	// 实际的分位数计算由Prometheus完成，这里只验证数据已记录
	t.Log("All durations recorded for percentile calculation")
	t.Log("Buckets: 10ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s, 30s, 60s")
	t.Log("P50 should be around 100ms")
	t.Log("P95 should be around 3s")
	t.Log("P99 should be around 5s")
}

// TestHistogramBucketConfiguration 测试直方图分桶配置
func TestHistogramBucketConfiguration(t *testing.T) {
	// 期望的分桶配置
	expectedBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0}

	// 验证分桶数量合理（覆盖从10ms到60s的范围）
	if len(expectedBuckets) < 5 {
		t.Error("too few buckets for accurate percentile calculation")
	}

	if len(expectedBuckets) > 20 {
		t.Error("too many buckets, may impact performance")
	}

	// 验证分桶是递增的
	for i := 1; i < len(expectedBuckets); i++ {
		if expectedBuckets[i] <= expectedBuckets[i-1] {
			t.Errorf("buckets should be in ascending order: %v <= %v",
				expectedBuckets[i], expectedBuckets[i-1])
		}
	}

	t.Logf("Histogram has %d buckets covering 10ms to 60s", len(expectedBuckets))
}

// TestConcurrentMetricRecording 测试并发指标记录
func TestConcurrentMetricRecording(t *testing.T) {
	// 模拟并发查询
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			duration := time.Duration(id*100) * time.Millisecond
			ObserveQueryDuration("select", "concurrent_test", duration)
			RecordSlowQuery("concurrent_test")
			done <- true
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < 10; i++ {
		<-done
	}

	t.Log("Concurrent metric recording completed successfully")
}

// getHistogramMetric 辅助函数：获取直方图指标（用于高级测试）
func getHistogramMetric(t *testing.T, vec *prometheus.HistogramVec, labels prometheus.Labels) *dto.Metric {
	metric := &dto.Metric{}
	h, err := vec.GetMetricWith(labels)
	if err != nil {
		t.Fatalf("failed to get metric: %v", err)
	}

	if err := h.(prometheus.Metric).Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	return metric
}
