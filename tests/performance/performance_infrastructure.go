package performance

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/metrics"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PerformanceConfig 性能测试配置
type PerformanceConfig struct {
	Duration            time.Duration `yaml:"duration"`            // 测试持续时间
	Concurrency         int           `yaml:"concurrency"`         // 并发数
	WarmupDuration     time.Duration `yaml:"warmup_duration"`     // 预热时间
	ReportInterval      time.Duration `yaml:"report_interval"`      // 报告间隔
	MaxLatency          time.Duration `yaml:"max_latency"`          // 最大可接受的延迟
	MinThroughput      int64         `yaml:"min_throughput"`      // 最小吞吐量要求
	CustomMetrics       []string      `yaml:"custom_metrics"`      // 自定义指标
}

// PerformanceTestResult 性能测试结果
type PerformanceTestResult struct {
	TestName         string                 `json:"test_name"`
	StartTime        time.Time              `json:"start_time"`
	EndTime          time.Time              `json:"end_time"`
	Duration        time.Duration          `json:"duration"`
	TotalOperations  int64                 `json:"total_operations"`
	SuccessfulOps    int64                 `json:"successful_ops"`
	FailedOps        int64                 `json:"failed_ops"`
	AverageLatency  time.Duration          `json:"average_latency"`
	MinLatency       time.Duration          `json:"min_latency"`
	MaxLatency       time.Duration          `json:"max_latency"`
	P95Latency       time.Duration          `json:"p95_latency"`
	P99Latency       time.Duration          `json:"p99_latency"`
	Throughput       float64               `json:"throughput"` // ops/sec
	ErrorRate        float64               `json:"error_rate"`    // percentage
	CustomResults    map[string]interface{} `json:"custom_results"`
	Passed           bool                  `json:"passed"`
	FailureMessage   string                `json:"failure_message"`
}

// LatencyBucket 延迟统计桶
type LatencyBucket struct {
	MaxDuration time.Duration
	Count       int64
}

// PerformanceTestPerformance 性能测试接口
type PerformanceTestPerformance interface {
	Name() string
	Setup(ctx context.Context, config *PerformanceConfig) error
	Execute(ctx context.Context, operationID int64) error
	Teardown(ctx context.Context) error
	GetMetrics(ctx context.Context) (map[string]interface{}, error)
}

// PerformanceTestRunner 性能测试运行器
type PerformanceTestRunner struct {
	config  *PerformanceConfig
	metrics  *metrics.MetricsCollector
	logger   *log.Logger
	results  *PerformanceTestResult
}

// NewPerformanceTestRunner 创建性能测试运行器
func NewPerformanceTestRunner(config *PerformanceConfig, metricsCollector *metrics.MetricsCollector) *PerformanceTestRunner {
	return &PerformanceTestRunner{
		config: config,
		metrics: metricsCollector,
		logger:  log.Default(),
	}
}

// RunTest 运行性能测试
func (r *PerformanceTestRunner) RunTest(ctx context.Context, test PerformanceTestPerformance) (*PerformanceTestResult, error) {
	result := &PerformanceTestResult{
		TestName:    test.Name(),
		StartTime:    time.Now(),
		CustomResults: make(map[string]interface{}),
	}

	r.logger.Printf("Starting performance test: %s", test.Name())
	r.logger.Printf("Test configuration: duration=%v, concurrency=%d, warmup=%v",
		r.config.Duration, r.config.Concurrency, r.config.WarmupDuration)

	// 预热阶段
	if r.config.WarmupDuration > 0 {
		r.logger.Printf("Starting warmup phase: %s", r.config.WarmupDuration)
		warmupCtx, cancel := context.WithTimeout(ctx, r.config.WarmupDuration)
		defer cancel()

		err := r.runWarmup(warmupCtx, test)
		if err != nil {
			return nil, fmt.Errorf("warmup phase failed: %v", err)
		}

		r.logger.Printf("Warmup phase completed")
	}

	// 准备测试
	err := test.Setup(ctx, r.config)
	if err != nil {
		return nil, fmt.Errorf("test setup failed: %v", err)
	}
	defer func() {
		err := test.Teardown(ctx)
		if err != nil {
			r.logger.Printf("Warning: test teardown failed: %v", err)
		}
	}()

	// 运行测试
	r.logger.Printf("Starting test execution phase: %s", r.config.Duration)
	testCtx, cancel := context.WithTimeout(ctx, r.config.Duration)
	defer cancel()

	err = r.runTest(testCtx, test, result)
	if err != nil {
		return nil, fmt.Errorf("test execution failed: %v", err)
	}

	// 计算最终结果
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	r.calculateFinalResults(result)

	// 获取自定义指标
	customMetrics, err := test.GetMetrics(ctx)
	if err != nil {
		r.logger.Printf("Warning: failed to get custom metrics: %v", err)
	} else {
		result.CustomResults = customMetrics
	}

	// 验证测试结果
	r.validateTestResults(result)

	r.logger.Printf("Performance test completed: %s", test.Name())
	r.logger.Printf("Results: throughput=%.2f ops/sec, p95_latency=%v, p99_latency=%v",
		result.Throughput, result.P95Latency, result.P99Latency)

	return result, nil
}

// runWarmup 运行预热阶段
func (r *PerformanceTestRunner) runWarmup(ctx context.Context, test PerformanceTestPerformance) error {
	warmupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan error)
	go func() {
		// 简化的预热执行
		for i := int64(0); i < 100; i++ {
			select {
			case <-warmupCtx.Done():
				done <- warmupCtx.Err()
				return
			default:
				err := test.Execute(warmupCtx, i)
				if err != nil {
					done <- err
					return
				}
				// 小延迟避免过度使用CPU
				time.Sleep(time.Millisecond)
			}
		}
		done <- nil
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// runTest 运行测试执行
func (r *PerformanceTestRunner) runTest(ctx context.Context, test PerformanceTestPerformance, result *PerformanceTestResult) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	latencyData := make([]time.Duration, 0)
	errors := make([]error, 0)
	operationCounter := int64(0)

	// 创建工作者
	workers := r.config.Concurrency
	if workers <= 0 {
		workers = 1
	}

	// 启动工作者
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					startTime := time.Now()
					operationID := atomicAddInt64(&operationCounter, 1)

					err := test.Execute(ctx, operationID)
					latency := time.Since(startTime)

					mu.Lock()
					latencyData = append(latencyData, latency)
					if err != nil {
						errors = append(errors, err)
					}
					mu.Unlock()

					// 小延迟避免过度使用CPU
					time.Sleep(time.Nanosecond * 100)
				}
			}
		}(i)
	}

	// 定期报告进度
	reportTicker := time.NewTicker(r.config.ReportInterval)
	defer reportTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reportTicker.C:
				mu.Lock()
				currentOps := operationCounter
				currentErrors := len(errors)
				mu.Unlock()

				r.logger.Printf("Progress: %d operations, %d errors", currentOps, currentErrors)
			}
		}
	}()

	// 等待所有工作者完成
	wg.Wait()

	// 计算结果
	mu.Lock()
	defer mu.Unlock()

	result.TotalOperations = operationCounter
	result.FailedOps = int64(len(errors))
	result.SuccessfulOps = result.TotalOperations - result.FailedOps
	result.ErrorRate = float64(result.FailedOps) / float64(result.TotalOperations) * 100

	// 计算延迟统计
	if len(latencyData) > 0 {
		r.calculateLatencyStats(latencyData, result)
	}

	return nil
}

// calculateLatencyStats 计算延迟统计
func (r *PerformanceTestRunner) calculateLatencyStats(latencyData []time.Duration, result *PerformanceTestResult) {
	// 排序数据
	sort.Slice(latencyData, func(i, j int) bool {
		return latencyData[i] < latencyData[j]
	})

	// 基本统计
	var totalLatency time.Duration
	for _, latency := range latencyData {
		totalLatency += latency
	}

	result.AverageLatency = totalLatency / time.Duration(len(latencyData))
	result.MinLatency = latencyData[0]
	result.MaxLatency = latencyData[len(latencyData)-1]

	// 计算百分位数
	result.P95Latency = latencyData[int(float64(len(latencyData))*0.95]
	result.P99Latency = latencyData[int(float64(len(latencyData))*0.99]
}

// calculateFinalResults 计算最终结果
func (r *PerformanceTestRunner) calculateFinalResults(result *PerformanceTestResult) {
	if result.Duration > 0 {
		result.Throughput = float64(result.SuccessfulOps) / result.Duration.Seconds()
	}
}

// validateTestResults 验证测试结果
func (r *PerformanceTestRunner) validateTestResults(result *PerformanceTestResult) {
	result.Passed = true

	// 验证最大延迟
	if r.config.MaxLatency > 0 && result.P99Latency > r.config.MaxLatency {
		result.Passed = false
		result.FailureMessage = fmt.Sprintf("Max latency exceeded: %v > %v", result.P99Latency, r.config.MaxLatency)
		return
	}

	// 验证最小吞吐量
	if r.config.MinThroughput > 0 && result.Throughput < float64(r.config.MinThroughput) {
		result.Passed = false
		result.FailureMessage = fmt.Sprintf("Min throughput not met: %.2f < %d", result.Throughput, r.config.MinThroughput)
		return
	}

	// 验证错误率
	if result.ErrorRate > 1.0 { // 1% error rate
		result.Passed = false
		result.FailureMessage = fmt.Sprintf("Error rate too high: %.2f%%", result.ErrorRate)
	}
}

// atomicAddInt64 原子性增加int64
func atomicAddInt64(ptr *int64, delta int64) int64 {
	return *ptr + delta
}

// LatencyDistribution 延迟分布
type LatencyDistribution struct {
	Min       time.Duration
	P25       time.Duration
	P50       time.Duration
	P75       time.Duration
	P90       time.Duration
	P95       time.Duration
	P99       time.Duration
	Max       time.Duration
	OperationCount int64
}

// CalculateLatencyDistribution 计算延迟分布
func CalculateLatencyDistribution(latencyData []time.Duration) *LatencyDistribution {
	if len(latencyData) == 0 {
		return nil
	}

	sort.Slice(latencyData, func(i, j int) bool {
		return latencyData[i] < latencyData[j]
	})

	dist := &LatencyDistribution{
		Min:           latencyData[0],
		Max:           latencyData[len(latencyData)-1],
		OperationCount: int64(len(latencyData)),
	}

	percentiles := []float64{0.25, 0.5, 0.75, 0.9, 0.95, 0.99}
	for i, p := range percentiles {
		index := int(math.Round(float64(len(latencyData)) * p))
		if index >= len(latencyData) {
			index = len(latencyData) - 1
		}

		switch i {
		case 0:
			dist.P25 = latencyData[index]
		case 1:
			dist.P50 = latencyData[index]
		case 2:
			dist.P75 = latencyData[index]
		case 3:
			dist.P90 = latencyData[index]
		case 4:
			dist.P95 = latencyData[index]
		case 5:
			dist.P99 = latencyData[index]
		}
	}

	return dist
}

// ThroughputStatistics 吞吐量统计
type ThroughputStatistics struct {
	Average  float64
	Min       float64
	Max       float64
	P95       float64
	P99       float64
	Variance  float64
	StdDev    float64
}

// CalculateThroughputStatistics 计算吞吐量统计
func CalculateThroughputStatistics(values []float64) *ThroughputStatistics {
	if len(values) == 0 {
		return nil
	}

	// 排序数据
	sort.Float64s(values)

	stats := &ThroughputStatistics{
		Min: values[0],
		Max: values[len(values)-1],
	}

	// 计算平均值
	var sum float64
	for _, v := range values {
		sum += v
	}
	stats.Average = sum / float64(len(values))

	// 计算方差和标准差
	for _, v := range values {
		stats.Variance += math.Pow(v-stats.Average, 2)
	}
	stats.Variance /= float64(len(values))
	stats.StdDev = math.Sqrt(stats.Variance)

	// 计算百分位数
	percentiles := []float64{0.95, 0.99}
	for i, p := range percentiles {
		index := int(math.Round(float64(len(values)) * p))
		if index >= len(values) {
			index = len(values) - 1
		}

		switch i {
		case 0:
			stats.P95 = values[index]
		case 1:
			stats.P99 = values[index]
		}
	}

	return stats
}