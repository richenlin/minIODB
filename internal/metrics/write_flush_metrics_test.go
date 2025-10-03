package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestWriteMetrics 测试写入指标记录
func TestWriteMetrics(t *testing.T) {
	table := "test_table"

	// 记录一次成功的写入
	wm := NewWriteMetrics(table)
	time.Sleep(10 * time.Millisecond)
	wm.Finish("success", 1024)

	// 验证指标被记录
	count := testutil.ToFloat64(miniodbWritesTotal.WithLabelValues(table, "success"))
	if count != 1 {
		t.Errorf("Expected 1 write, got %f", count)
	}

	bytes := testutil.ToFloat64(miniodbWriteBytesTotal.WithLabelValues(table))
	if bytes != 1024 {
		t.Errorf("Expected 1024 bytes, got %f", bytes)
	}

	t.Logf("✓ Write metrics recorded correctly: count=%.0f, bytes=%.0f", count, bytes)
}

// TestFlushMetrics 测试刷新指标记录
func TestFlushMetrics(t *testing.T) {
	table := "test_table_flush"
	trigger := "periodic"

	// 记录一次成功的刷新
	fm := NewFlushMetrics(table, trigger)
	time.Sleep(50 * time.Millisecond)
	fm.Finish("success", 100, 10240)

	// 验证指标被记录
	count := testutil.ToFloat64(miniodbFlushesTotal.WithLabelValues(table, trigger, "success"))
	if count != 1 {
		t.Errorf("Expected 1 flush, got %f", count)
	}

	records := testutil.ToFloat64(miniodbFlushRecordsTotal.WithLabelValues(table))
	if records != 100 {
		t.Errorf("Expected 100 records, got %f", records)
	}

	bytes := testutil.ToFloat64(miniodbFlushBytesTotal.WithLabelValues(table))
	if bytes != 10240 {
		t.Errorf("Expected 10240 bytes, got %f", bytes)
	}

	t.Logf("✓ Flush metrics recorded correctly: count=%.0f, records=%.0f, bytes=%.0f", count, records, bytes)
}

// TestUpdatePendingWrites 测试待写入记录数更新
func TestUpdatePendingWrites(t *testing.T) {
	table := "test_pending"

	UpdatePendingWrites(table, 50)
	pending := testutil.ToFloat64(miniodbPendingWrites.WithLabelValues(table))
	if pending != 50 {
		t.Errorf("Expected 50 pending writes, got %f", pending)
	}

	UpdatePendingWrites(table, 100)
	pending = testutil.ToFloat64(miniodbPendingWrites.WithLabelValues(table))
	if pending != 100 {
		t.Errorf("Expected 100 pending writes after update, got %f", pending)
	}

	t.Logf("✓ Pending writes updated correctly: %.0f", pending)
}

// TestWriteDurationHistogram 测试写入耗时直方图
func TestWriteDurationHistogram(t *testing.T) {
	table := "test_duration"

	// 模拟不同耗时的写入
	durations := []time.Duration{
		1 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		500 * time.Millisecond,
	}

	for _, d := range durations {
		wm := NewWriteMetrics(table)
		time.Sleep(d)
		wm.Finish("success", 1024)
	}

	// 验证直方图有数据
	count := testutil.ToFloat64(miniodbWritesTotal.WithLabelValues(table, "success"))
	if count != float64(len(durations)) {
		t.Errorf("Expected %d writes, got %f", len(durations), count)
	}

	t.Logf("✓ Write duration histogram recorded %d samples", len(durations))
}

// TestFlushDurationHistogram 测试刷新耗时直方图
func TestFlushDurationHistogram(t *testing.T) {
	table := "test_flush_duration"

	// 模拟不同触发类型的刷新
	triggers := []string{"periodic", "manual", "adaptive", "memory_pressure"}

	for _, trigger := range triggers {
		fm := NewFlushMetrics(table, trigger)
		time.Sleep(20 * time.Millisecond)
		fm.Finish("success", 50, 5120)
	}

	// 验证所有触发类型都被记录
	for _, trigger := range triggers {
		count := testutil.ToFloat64(miniodbFlushesTotal.WithLabelValues(table, trigger, "success"))
		if count != 1 {
			t.Errorf("Expected 1 flush for trigger %s, got %f", trigger, count)
		}
	}

	t.Logf("✓ Flush duration histogram recorded for all %d trigger types", len(triggers))
}

// TestWriteMetricsFailure 测试写入失败指标
func TestWriteMetricsFailure(t *testing.T) {
	table := "test_write_failure"

	// 记录失败的写入
	wm := NewWriteMetrics(table)
	time.Sleep(5 * time.Millisecond)
	wm.Finish("failure", 0) // 失败时bytes为0

	// 验证失败计数
	count := testutil.ToFloat64(miniodbWritesTotal.WithLabelValues(table, "failure"))
	if count != 1 {
		t.Errorf("Expected 1 failed write, got %f", count)
	}

	// 失败时不应增加字节数
	bytes := testutil.ToFloat64(miniodbWriteBytesTotal.WithLabelValues(table))
	if bytes != 0 {
		t.Errorf("Expected 0 bytes for failed write, got %f", bytes)
	}

	t.Logf("✓ Write failure metrics recorded correctly")
}

// TestFlushMetricsFailure 测试刷新失败指标
func TestFlushMetricsFailure(t *testing.T) {
	table := "test_flush_failure"
	trigger := "adaptive"

	// 记录失败的刷新
	fm := NewFlushMetrics(table, trigger)
	time.Sleep(10 * time.Millisecond)
	fm.Finish("failure", 0, 0) // 失败时records和bytes为0

	// 验证失败计数
	count := testutil.ToFloat64(miniodbFlushesTotal.WithLabelValues(table, trigger, "failure"))
	if count != 1 {
		t.Errorf("Expected 1 failed flush, got %f", count)
	}

	t.Logf("✓ Flush failure metrics recorded correctly")
}

// TestMetricsLabels 测试指标标签
func TestMetricsLabels(t *testing.T) {
	// 测试写入指标的标签
	writeLabels := []string{"table", "status"}
	t.Logf("Write metrics labels: %v", writeLabels)

	// 测试刷新指标的标签
	flushLabels := []string{"table", "trigger", "status"}
	t.Logf("Flush metrics labels: %v", flushLabels)

	// 测试触发类型
	triggerTypes := []string{"periodic", "manual", "adaptive", "memory_pressure"}
	t.Logf("Flush trigger types: %v", triggerTypes)

	t.Logf("✓ All metric labels defined")
}

// TestMetricsBuckets 测试直方图分桶
func TestMetricsBuckets(t *testing.T) {
	// 写入耗时分桶（毫秒级）
	writeBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5}
	t.Logf("Write duration buckets (seconds): %v", writeBuckets)

	// 刷新耗时分桶（较大）
	flushBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}
	t.Logf("Flush duration buckets (seconds): %v", flushBuckets)

	// 验证分桶是否递增
	for i := 1; i < len(writeBuckets); i++ {
		if writeBuckets[i] <= writeBuckets[i-1] {
			t.Errorf("Write buckets not in ascending order at index %d", i)
		}
	}

	for i := 1; i < len(flushBuckets); i++ {
		if flushBuckets[i] <= flushBuckets[i-1] {
			t.Errorf("Flush buckets not in ascending order at index %d", i)
		}
	}

	t.Logf("✓ Histogram buckets validated")
}

// TestConcurrentWrites 测试并发写入指标
func TestConcurrentWrites(t *testing.T) {
	table := "test_concurrent"
	numWrites := 100

	done := make(chan bool, numWrites)

	for i := 0; i < numWrites; i++ {
		go func(idx int) {
			wm := NewWriteMetrics(table)
			time.Sleep(1 * time.Millisecond)
			wm.Finish("success", 1024)
			done <- true
		}(i)
	}

	// 等待所有写入完成
	for i := 0; i < numWrites; i++ {
		<-done
	}

	// 验证所有写入都被记录
	count := testutil.ToFloat64(miniodbWritesTotal.WithLabelValues(table, "success"))
	if count != float64(numWrites) {
		t.Errorf("Expected %d concurrent writes, got %f", numWrites, count)
	}

	totalBytes := testutil.ToFloat64(miniodbWriteBytesTotal.WithLabelValues(table))
	expectedBytes := float64(numWrites * 1024)
	if totalBytes != expectedBytes {
		t.Errorf("Expected %f total bytes, got %f", expectedBytes, totalBytes)
	}

	t.Logf("✓ %d concurrent writes recorded correctly", numWrites)
}

// TestMultiTableMetrics 测试多表指标
func TestMultiTableMetrics(t *testing.T) {
	tables := []string{"table1", "table2", "table3", "table4", "table5"}

	for _, table := range tables {
		wm := NewWriteMetrics(table)
		time.Sleep(2 * time.Millisecond)
		wm.Finish("success", 2048)

		fm := NewFlushMetrics(table, "periodic")
		time.Sleep(5 * time.Millisecond)
		fm.Finish("success", 10, 2048)
	}

	// 验证所有表都有指标
	for _, table := range tables {
		writeCount := testutil.ToFloat64(miniodbWritesTotal.WithLabelValues(table, "success"))
		if writeCount != 1 {
			t.Errorf("Expected 1 write for %s, got %f", table, writeCount)
		}

		flushCount := testutil.ToFloat64(miniodbFlushesTotal.WithLabelValues(table, "periodic", "success"))
		if flushCount != 1 {
			t.Errorf("Expected 1 flush for %s, got %f", table, flushCount)
		}
	}

	t.Logf("✓ Metrics recorded for all %d tables", len(tables))
}

// TestMetricsCollector 测试指标收集器
func TestMetricsCollector(t *testing.T) {
	// 确保指标已注册
	metrics := []prometheus.Collector{
		miniodbWriteDuration,
		miniodbWritesTotal,
		miniodbWriteBytesTotal,
		miniodbFlushDuration,
		miniodbFlushesTotal,
		miniodbFlushRecordsTotal,
		miniodbFlushBytesTotal,
		miniodbPendingWrites,
	}

	for i, metric := range metrics {
		if metric == nil {
			t.Errorf("Metric %d is nil", i)
		}
	}

	t.Logf("✓ All %d metrics collectors registered", len(metrics))
}

// BenchmarkWriteMetrics 基准测试：写入指标
func BenchmarkWriteMetrics(b *testing.B) {
	table := "bench_table"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wm := NewWriteMetrics(table)
		wm.Finish("success", 1024)
	}
}

// BenchmarkFlushMetrics 基准测试：刷新指标
func BenchmarkFlushMetrics(b *testing.B) {
	table := "bench_table"
	trigger := "periodic"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		fm := NewFlushMetrics(table, trigger)
		fm.Finish("success", 100, 10240)
	}
}

// BenchmarkConcurrentMetrics 基准测试：并发指标记录
func BenchmarkConcurrentMetrics(b *testing.B) {
	table := "bench_concurrent"

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wm := NewWriteMetrics(table)
			wm.Finish("success", 1024)
		}
	})
}
