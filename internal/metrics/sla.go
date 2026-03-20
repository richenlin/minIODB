package metrics

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// SLAViolation SLA违规记录
type SLAViolation struct {
	MetricName    string
	ActualValue   float64
	Threshold     float64
	ViolationTime time.Time
	Severity      string
}

// SLAConfig SLA配置
type SLAConfig struct {
	QueryLatencyP50  time.Duration
	QueryLatencyP95  time.Duration
	QueryLatencyP99  time.Duration
	CacheHitRate     float64
	FilePruneRate    float64
	WriteLatencyP95  time.Duration
	AlertOnViolation bool
}

var (
	// SLA指标
	slaViolationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "olap_sla_violations_total",
			Help: "Total number of SLA violations",
		},
		[]string{"metric", "severity"},
	)

	slaComplianceRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "olap_sla_compliance_rate",
			Help: "SLA compliance rate percentage",
		},
		[]string{"metric"},
	)

	slaQueryLatencyP50 = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_sla_query_latency_p50_seconds",
			Help: "Query latency P50 in seconds",
		},
	)

	slaQueryLatencyP95 = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_sla_query_latency_p95_seconds",
			Help: "Query latency P95 in seconds",
		},
	)

	slaQueryLatencyP99 = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_sla_query_latency_p99_seconds",
			Help: "Query latency P99 in seconds",
		},
	)

	slaCacheHitRate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "olap_sla_cache_hit_rate",
			Help: "Cache hit rate percentage",
		},
	)
)

// SLAMonitor SLA监控器
type SLAMonitor struct {
	config       *SLAConfig
	logger       *zap.Logger
	violations   []SLAViolation
	violationsMu sync.RWMutex
	stopCh       chan struct{}
	metrics      *QueryMetricsHistory
}

// QueryMetricsHistory 查询指标历史
type QueryMetricsHistory struct {
	latencies []float64
	mu        sync.RWMutex
	maxSize   int
}

// NewQueryMetricsHistory 创建查询指标历史
func NewQueryMetricsHistory(maxSize int) *QueryMetricsHistory {
	return &QueryMetricsHistory{
		latencies: make([]float64, 0, maxSize),
		maxSize:   maxSize,
	}
}

// AddLatency 添加延迟记录
func (h *QueryMetricsHistory) AddLatency(duration float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.latencies = append(h.latencies, duration)
	if len(h.latencies) > h.maxSize {
		h.latencies = h.latencies[1:]
	}
}

// GetPercentile 获取百分位数
func (h *QueryMetricsHistory) GetPercentile(p float64) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.latencies) == 0 {
		return 0
	}

	if p >= 1 {
		p = 0.99
	}

	// 复制并排序
	sorted := make([]float64, len(h.latencies))
	copy(sorted, h.latencies)
	sort.Float64s(sorted)

	// 计算索引
	idx := int(float64(len(sorted)) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// GetP50 获取P50延迟
func (h *QueryMetricsHistory) GetP50() float64 {
	return h.GetPercentile(0.5)
}

// GetP95 获取P95延迟
func (h *QueryMetricsHistory) GetP95() float64 {
	return h.GetPercentile(0.95)
}

// GetP99 获取P99延迟
func (h *QueryMetricsHistory) GetP99() float64 {
	return h.GetPercentile(0.99)
}

// Count 返回已记录的延迟数量
func (h *QueryMetricsHistory) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.latencies)
}

// GetMetricsHistory 返回查询延迟历史记录
func (m *SLAMonitor) GetMetricsHistory() *QueryMetricsHistory {
	return m.metrics
}

// NewSLAMonitor 创建SLA监控器
func NewSLAMonitor(config *SLAConfig, logger *zap.Logger) *SLAMonitor {
	return &SLAMonitor{
		config:     config,
		logger:     logger,
		violations: make([]SLAViolation, 0),
		stopCh:     make(chan struct{}),
		metrics:    NewQueryMetricsHistory(10000),
	}
}

// RecordQueryLatency 记录查询延迟
func (m *SLAMonitor) RecordQueryLatency(duration time.Duration) {
	m.metrics.AddLatency(duration.Seconds())

	// 更新Prometheus指标
	slaQueryLatencyP50.Set(m.metrics.GetP50())
	slaQueryLatencyP95.Set(m.metrics.GetP95())
	slaQueryLatencyP99.Set(m.metrics.GetP99())
}

// RecordCacheHitRate 记录缓存命中率
func (m *SLAMonitor) RecordCacheHitRate(rate float64) {
	slaCacheHitRate.Set(rate)

	if m.config.CacheHitRate > 0 && rate < m.config.CacheHitRate {
		m.recordViolation("cache_hit_rate", rate, m.config.CacheHitRate, "medium")
	}

	slaComplianceRate.WithLabelValues("cache_hit_rate").Set(rate * 100)
}

// CheckSLA 检查SLA合规性
func (m *SLAMonitor) CheckSLA() []SLAViolation {
	var violations []SLAViolation

	// 检查P50延迟
	if m.config.QueryLatencyP50 > 0 {
		p50 := m.metrics.GetP50()
		if p50 > m.config.QueryLatencyP50.Seconds() {
			violations = append(violations, SLAViolation{
				MetricName:    "query_latency_p50",
				ActualValue:   p50,
				Threshold:     m.config.QueryLatencyP50.Seconds(),
				ViolationTime: time.Now(),
				Severity:      "medium",
			})
		}
	}

	// 检查P95延迟
	if m.config.QueryLatencyP95 > 0 {
		p95 := m.metrics.GetP95()
		if p95 > m.config.QueryLatencyP95.Seconds() {
			violations = append(violations, SLAViolation{
				MetricName:    "query_latency_p95",
				ActualValue:   p95,
				Threshold:     m.config.QueryLatencyP95.Seconds(),
				ViolationTime: time.Now(),
				Severity:      "high",
			})
		}

		slaComplianceRate.WithLabelValues("query_latency_p95").Set(complianceRate(p95, m.config.QueryLatencyP95.Seconds()))
	}

	// 检查P99延迟
	if m.config.QueryLatencyP99 > 0 {
		p99 := m.metrics.GetP99()
		if p99 > m.config.QueryLatencyP99.Seconds() {
			violations = append(violations, SLAViolation{
				MetricName:    "query_latency_p99",
				ActualValue:   p99,
				Threshold:     m.config.QueryLatencyP99.Seconds(),
				ViolationTime: time.Now(),
				Severity:      "critical",
			})
		}

		slaComplianceRate.WithLabelValues("query_latency_p99").Set(complianceRate(p99, m.config.QueryLatencyP99.Seconds()))
	}

	return violations
}

// recordViolation 记录SLA违规
func (m *SLAMonitor) recordViolation(metricName string, actualValue, threshold float64, severity string) {
	violation := SLAViolation{
		MetricName:    metricName,
		ActualValue:   actualValue,
		Threshold:     threshold,
		ViolationTime: time.Now(),
		Severity:      severity,
	}

	m.violationsMu.Lock()
	m.violations = append(m.violations, violation)
	m.violationsMu.Unlock()

	slaViolationsTotal.WithLabelValues(metricName, severity).Inc()

	if m.config.AlertOnViolation && m.logger != nil {
		m.logger.Warn("SLA violation detected",
			zap.String("metric", metricName),
			zap.Float64("actual", actualValue),
			zap.Float64("threshold", threshold),
			zap.String("severity", severity),
		)
	}
}

// GetViolations 获取SLA违规记录
func (m *SLAMonitor) GetViolations() []SLAViolation {
	m.violationsMu.RLock()
	defer m.violationsMu.RUnlock()

	copies := make([]SLAViolation, len(m.violations))
	copy(copies, m.violations)
	return copies
}

// ClearViolations 清除违规记录
func (m *SLAMonitor) ClearViolations() {
	m.violationsMu.Lock()
	defer m.violationsMu.Unlock()
	m.violations = make([]SLAViolation, 0)
}

// Start 启动SLA监控
func (m *SLAMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			violations := m.CheckSLA()
			if len(violations) > 0 && m.logger != nil {
				m.logger.Warn("SLA violations detected",
					zap.Int("count", len(violations)),
				)
			}
		}
	}
}

// Stop 停止SLA监控
func (m *SLAMonitor) Stop() {
	close(m.stopCh)
}

// complianceRate 计算合规率
func complianceRate(actual, threshold float64) float64 {
	if threshold <= 0 {
		return 100
	}

	if actual <= threshold {
		return 100
	}

	rate := (threshold / actual) * 100
	if rate > 100 {
		rate = 100
	}
	return rate
}

// GetSLAReport 获取SLA报告
func (m *SLAMonitor) GetSLAReport() map[string]interface{} {
	report := make(map[string]interface{})

	report["p50_latency"] = m.metrics.GetP50()
	report["p95_latency"] = m.metrics.GetP95()
	report["p99_latency"] = m.metrics.GetP99()
	report["p95_compliance"] = complianceRate(m.metrics.GetP95(), m.config.QueryLatencyP95.Seconds())
	report["p99_compliance"] = complianceRate(m.metrics.GetP99(), m.config.QueryLatencyP99.Seconds())
	report["total_violations"] = len(m.GetViolations())

	return report
}

// DefaultSLAConfig 默认SLA配置
func DefaultSLAConfig() *SLAConfig {
	return &SLAConfig{
		QueryLatencyP50:  100 * time.Millisecond,
		QueryLatencyP95:  1 * time.Second,
		QueryLatencyP99:  5 * time.Second,
		CacheHitRate:     0.7,
		FilePruneRate:    0.3,
		WriteLatencyP95:  500 * time.Millisecond,
		AlertOnViolation: true,
	}
}
