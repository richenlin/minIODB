package metrics

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// BenchmarkResult 基准测试结果
type BenchmarkResult struct {
	TestName    string
	Duration    time.Duration
	Success     bool
	Error       error
	RowCount    int64
	BytesRead   int64
	Metadata    map[string]interface{}
	CompletedAt time.Time
}

// BenchmarkScenario 基准测试场景
type BenchmarkScenario struct {
	Name        string
	Description string
	Query       string
	ExpectedP95 time.Duration
}

// BenchmarkConfig 基准测试配置
type BenchmarkConfig struct {
	EnableAutoBenchmark bool
	RunInterval         time.Duration
	Timeout             time.Duration
	MaxRetries          int
}

// BenchmarkManager 基准测试管理器
type BenchmarkManager struct {
	config  *BenchmarkConfig
	logger  *zap.Logger
	results []BenchmarkResult
	mu      sync.RWMutex
	stopCh  chan struct{}
	querier Querier
}

// Querier 查询器接口
type Querier interface {
	ExecuteQuery(sql string) (string, error)
}

// NewBenchmarkManager 创建基准测试管理器
func NewBenchmarkManager(config *BenchmarkConfig, logger *zap.Logger) *BenchmarkManager {
	return &BenchmarkManager{
		config:  config,
		logger:  logger,
		results: make([]BenchmarkResult, 0),
		stopCh:  make(chan struct{}),
	}
}

// SetQuerier 设置查询器
func (bm *BenchmarkManager) SetQuerier(querier Querier) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.querier = querier
}

// RunBenchmark 运行基准测试
func (bm *BenchmarkManager) RunBenchmark(scenario BenchmarkScenario) *BenchmarkResult {
	result := &BenchmarkResult{
		TestName:    scenario.Name,
		Metadata:    make(map[string]interface{}),
		CompletedAt: time.Now(),
	}

	startTime := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), bm.config.Timeout)
	defer cancel()

	var err error

	// 重试机制
	for attempt := 1; attempt <= bm.config.MaxRetries; attempt++ {
		if attempt > 1 {
			if bm.logger != nil {
				bm.logger.Info("Retrying benchmark",
					zap.String("scenario", scenario.Name),
					zap.Int("attempt", attempt),
				)
			}
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		select {
		case <-ctx.Done():
			err = fmt.Errorf("benchmark timeout after %v", bm.config.Timeout)
			break
		default:
			if bm.querier != nil {
				_, err = bm.querier.ExecuteQuery(scenario.Query)
			} else {
				err = fmt.Errorf("querier not set")
			}

			if err == nil {
				break
			}
		}
	}

	duration := time.Since(startTime)
	result.Duration = duration
	result.Success = (err == nil)
	result.Error = err

	if err != nil {
		if bm.logger != nil {
			bm.logger.Error("Benchmark failed",
				zap.String("scenario", scenario.Name),
				zap.Error(err),
				zap.Duration("duration", duration),
			)
		}
	} else {
		if bm.logger != nil {
			bm.logger.Info("Benchmark completed",
				zap.String("scenario", scenario.Name),
				zap.Duration("duration", duration),
				zap.Duration("expected_p95", scenario.ExpectedP95),
			)

			if scenario.ExpectedP95 > 0 && duration > scenario.ExpectedP95 {
				bm.logger.Warn("Benchmark performance below expected",
					zap.String("scenario", scenario.Name),
					zap.Duration("actual", duration),
					zap.Duration("expected", scenario.ExpectedP95),
					zap.Float64("slowdown", float64(duration)/float64(scenario.ExpectedP95)),
				)
			}
		}
	}

	bm.mu.Lock()
	bm.results = append(bm.results, *result)
	bm.mu.Unlock()

	return result
}

// RunAllBenchmarks 运行所有基准测试
func (bm *BenchmarkManager) RunAllBenchmarks(scenarios []BenchmarkScenario) []BenchmarkResult {
	results := make([]BenchmarkResult, 0, len(scenarios))

	for _, scenario := range scenarios {
		result := bm.RunBenchmark(scenario)
		results = append(results, *result)
	}

	return results
}

// GetResults 获取基准测试结果
func (bm *BenchmarkManager) GetResults() []BenchmarkResult {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	copies := make([]BenchmarkResult, len(bm.results))
	copy(copies, bm.results)
	return copies
}

// GetLatestResult 获取最新的基准测试结果
func (bm *BenchmarkManager) GetLatestResult(scenarioName string) *BenchmarkResult {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	for i := len(bm.results) - 1; i >= 0; i-- {
		if bm.results[i].TestName == scenarioName {
			result := bm.results[i]
			return &result
		}
	}

	return nil
}

// GetAverageDuration 获取平均持续时间
func (bm *BenchmarkManager) GetAverageDuration(scenarioName string) time.Duration {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	var total time.Duration
	count := 0

	for _, result := range bm.results {
		if result.TestName == scenarioName && result.Success {
			total += result.Duration
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return total / time.Duration(count)
}

// GetP95Duration 获取P95持续时间
func (bm *BenchmarkManager) GetP95Duration(scenarioName string) time.Duration {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	var durations []time.Duration

	for _, result := range bm.results {
		if result.TestName == scenarioName && result.Success {
			durations = append(durations, result.Duration)
		}
	}

	if len(durations) == 0 {
		return 0
	}

	// 排序
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	// 计算P95索引
	idx := int(float64(len(durations)) * 0.95)
	if idx >= len(durations) {
		idx = len(durations) - 1
	}

	return durations[idx]
}

// sortDurations 排序持续时间
func sortDurations(durations []time.Duration) {
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})
}

// ClearResults 清除基准测试结果
func (bm *BenchmarkManager) ClearResults() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.results = make([]BenchmarkResult, 0)
}

// StartAutoBenchmark 启动自动基准测试
func (bm *BenchmarkManager) StartAutoBenchmark(scenarios []BenchmarkScenario) {
	ticker := time.NewTicker(bm.config.RunInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.stopCh:
			return
		case <-ticker.C:
			if bm.logger != nil {
				bm.logger.Info("Running auto benchmark")
			}
			bm.RunAllBenchmarks(scenarios)
		}
	}
}

// Stop 停止基准测试
func (bm *BenchmarkManager) Stop() {
	close(bm.stopCh)
}

// GetBenchmarkReport 获取基准测试报告
func (bm *BenchmarkManager) GetBenchmarkReport() map[string]interface{} {
	report := make(map[string]interface{})

	results := bm.GetResults()
	report["total_benchmarks"] = len(results)
	report["success_count"] = 0
	report["failure_count"] = 0
	report["scenarios"] = make(map[string]interface{})

	scenarios := make(map[string][]BenchmarkResult)
	scenarioNames := make(map[string]struct{})

	for _, result := range results {
		if result.Success {
			report["success_count"] = report["success_count"].(int) + 1
		} else {
			report["failure_count"] = report["failure_count"].(int) + 1
		}

		if _, exists := scenarioNames[result.TestName]; !exists {
			scenarioNames[result.TestName] = struct{}{}
		}

		scenarios[result.TestName] = append(scenarios[result.TestName], result)
	}

	for name, scenarioResults := range scenarios {
		scenarioReport := make(map[string]interface{})
		scenarioReport["total_runs"] = len(scenarioResults)
		scenarioReport["success_count"] = 0
		scenarioReport["failure_count"] = 0

		var totalDuration time.Duration
		successCount := 0

		for _, result := range scenarioResults {
			if result.Success {
				scenarioReport["success_count"] = scenarioReport["success_count"].(int) + 1
				totalDuration += result.Duration
				successCount++
			} else {
				scenarioReport["failure_count"] = scenarioReport["failure_count"].(int) + 1
			}
		}

		if successCount > 0 {
			scenarioReport["avg_duration"] = (totalDuration / time.Duration(successCount)).String()
			scenarioReport["p95_duration"] = bm.GetP95Duration(name).String()
		}

		report["scenarios"].(map[string]interface{})[name] = scenarioReport
	}

	return report
}

// DefaultBenchmarkConfig 默认基准测试配置
func DefaultBenchmarkConfig() *BenchmarkConfig {
	return &BenchmarkConfig{
		EnableAutoBenchmark: false,
		RunInterval:         1 * time.Hour,
		Timeout:             30 * time.Second,
		MaxRetries:          3,
	}
}

// DefaultBenchmarkScenarios 默认基准测试场景
func DefaultBenchmarkScenarios() []BenchmarkScenario {
	return []BenchmarkScenario{
		{
			Name:        "simple_query",
			Description: "Simple SELECT query",
			Query:       "SELECT COUNT(*) FROM default WHERE timestamp >= NOW() - INTERVAL '1 day'",
			ExpectedP95: 100 * time.Millisecond,
		},
		{
			Name:        "complex_aggregation",
			Description: "Complex aggregation query",
			Query:       "SELECT DATE_TRUNC('hour', timestamp) as hour, COUNT(DISTINCT id) as count FROM default WHERE timestamp >= NOW() - INTERVAL '7 days' GROUP BY hour",
			ExpectedP95: 1 * time.Second,
		},
		{
			Name:        "count_distinct",
			Description: "COUNT DISTINCT query",
			Query:       "SELECT COUNT(DISTINCT id) FROM default WHERE timestamp >= NOW() - INTERVAL '30 days'",
			ExpectedP95: 5 * time.Second,
		},
	}
}
