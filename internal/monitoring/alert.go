package monitoring

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// AlertSeverity 告警严重级别
type AlertSeverity string

const (
	SeverityLow      AlertSeverity = "low"
	SeverityMedium   AlertSeverity = "medium"
	SeverityHigh     AlertSeverity = "high"
	SeverityCritical AlertSeverity = "critical"
)

// AlertType 告警类型
type AlertType string

const (
	AlertTypeThreshold AlertType = "threshold" // 阈值告警
	AlertTypeTrend     AlertType = "trend"     // 趋势告警
	AlertTypeAnomaly   AlertType = "anomaly"   // 异常告警
)

// Alert 告警
type Alert struct {
	ID          string        `json:"id"`
	Type        AlertType     `json:"type"`
	Severity    AlertSeverity `json:"severity"`
	Component   string        `json:"component"`
	Metric      string        `json:"metric"`
	Message     string        `json:"message"`
	Value       float64       `json:"value"`
	Threshold   float64       `json:"threshold"`
	Timestamp   time.Time     `json:"timestamp"`
	Resolved    bool          `json:"resolved"`
	ResolvedAt  *time.Time    `json:"resolved_at,omitempty"`
	Count       int           `json:"count"`         // 聚合计数
	FirstSeenAt time.Time     `json:"first_seen_at"` // 首次出现时间
}

// AlertRule 告警规则
type AlertRule struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Component        string        `json:"component"`
	Metric           string        `json:"metric"`
	Severity         AlertSeverity `json:"severity"`
	ThresholdMin     float64       `json:"threshold_min"`
	ThresholdMax     float64       `json:"threshold_max"`
	Duration         time.Duration `json:"duration"`          // 持续时间
	EvaluationWindow time.Duration `json:"evaluation_window"` // 评估窗口
	Enabled          bool          `json:"enabled"`
}

// AlertManager 告警管理器
type AlertManager struct {
	alerts            map[string]*Alert        // 当前活跃告警
	alertHistory      []*Alert                 // 历史告警
	rules             map[string]*AlertRule    // 告警规则
	metrics           map[string]*MetricBuffer // 指标缓冲区
	deduplicateWindow time.Duration            // 去重时间窗口
	mutex             sync.RWMutex
	stopChan          chan struct{}
	notifiers         []AlertNotifier // 告警通知器
}

// MetricBuffer 指标缓冲区
type MetricBuffer struct {
	Values     []float64   `json:"values"`
	Timestamps []time.Time `json:"timestamps"`
	MaxSize    int         `json:"max_size"`
}

// AlertNotifier 告警通知接口
type AlertNotifier interface {
	Notify(alert *Alert) error
}

// Prometheus 告警指标
var (
	alertsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_alerts_total",
			Help: "Total number of alerts generated",
		},
		[]string{"severity", "component", "type"},
	)

	activeAlerts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "miniodb_active_alerts",
			Help: "Number of active alerts",
		},
		[]string{"severity", "component"},
	)

	alertResolutionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "miniodb_alert_resolution_duration_seconds",
			Help:    "Time taken to resolve alerts",
			Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400},
		},
		[]string{"severity", "component"},
	)
)

func init() {
	prometheus.MustRegister(alertsTotal, activeAlerts, alertResolutionDuration)
}

// NewAlertManager 创建告警管理器
func NewAlertManager(deduplicateWindow time.Duration) *AlertManager {
	am := &AlertManager{
		alerts:            make(map[string]*Alert),
		alertHistory:      make([]*Alert, 0),
		rules:             make(map[string]*AlertRule),
		metrics:           make(map[string]*MetricBuffer),
		deduplicateWindow: deduplicateWindow,
		stopChan:          make(chan struct{}),
		notifiers:         make([]AlertNotifier, 0),
	}

	// 添加默认告警规则
	am.addDefaultRules()

	return am
}

// addDefaultRules 添加默认告警规则
func (am *AlertManager) addDefaultRules() {
	defaultRules := []*AlertRule{
		{
			ID:               "high_memory_usage",
			Name:             "High Memory Usage",
			Component:        "system",
			Metric:           "memory_usage",
			Severity:         SeverityHigh,
			ThresholdMax:     0.85, // 85%
			Duration:         5 * time.Minute,
			EvaluationWindow: 1 * time.Minute,
			Enabled:          true,
		},
		{
			ID:               "high_cpu_usage",
			Name:             "High CPU Usage",
			Component:        "system",
			Metric:           "cpu_usage",
			Severity:         SeverityHigh,
			ThresholdMax:     0.80, // 80%
			Duration:         5 * time.Minute,
			EvaluationWindow: 1 * time.Minute,
			Enabled:          true,
		},
		{
			ID:               "high_error_rate",
			Name:             "High Error Rate",
			Component:        "query",
			Metric:           "error_rate",
			Severity:         SeverityCritical,
			ThresholdMax:     0.05, // 5%
			Duration:         2 * time.Minute,
			EvaluationWindow: 1 * time.Minute,
			Enabled:          true,
		},
		{
			ID:               "slow_query",
			Name:             "Slow Query Detected",
			Component:        "query",
			Metric:           "query_duration",
			Severity:         SeverityMedium,
			ThresholdMax:     30.0, // 30秒
			Duration:         1 * time.Minute,
			EvaluationWindow: 30 * time.Second,
			Enabled:          true,
		},
		{
			ID:               "connection_pool_exhaustion",
			Name:             "Connection Pool Nearly Exhausted",
			Component:        "connection_pool",
			Metric:           "pool_usage",
			Severity:         SeverityCritical,
			ThresholdMax:     0.90, // 90%
			Duration:         2 * time.Minute,
			EvaluationWindow: 30 * time.Second,
			Enabled:          true,
		},
		{
			ID:               "index_system_low_hit_rate",
			Name:             "Index System Low Hit Rate",
			Component:        "index_system",
			Metric:           "hit_rate",
			Severity:         SeverityMedium,
			ThresholdMin:     0.50, // 低于50%
			Duration:         10 * time.Minute,
			EvaluationWindow: 1 * time.Minute,
			Enabled:          true,
		},
		{
			ID:               "buffer_flush_failures",
			Name:             "Buffer Flush Failures",
			Component:        "buffer",
			Metric:           "flush_error_rate",
			Severity:         SeverityHigh,
			ThresholdMax:     0.10, // 10%
			Duration:         5 * time.Minute,
			EvaluationWindow: 1 * time.Minute,
			Enabled:          true,
		},
	}

	for _, rule := range defaultRules {
		am.rules[rule.ID] = rule
	}
}

// Start 启动告警管理器
func (am *AlertManager) Start(ctx context.Context) {
	log.Println("Starting alert manager...")
	go am.evaluationLoop(ctx)
}

// Stop 停止告警管理器
func (am *AlertManager) Stop() {
	close(am.stopChan)
	log.Println("Alert manager stopped")
}

// evaluationLoop 评估循环
func (am *AlertManager) evaluationLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // 每30秒评估一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-am.stopChan:
			return
		case <-ticker.C:
			am.evaluateRules()
		}
	}
}

// evaluateRules 评估告警规则
func (am *AlertManager) evaluateRules() {
	am.mutex.RLock()
	rules := make([]*AlertRule, 0, len(am.rules))
	for _, rule := range am.rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	am.mutex.RUnlock()

	for _, rule := range rules {
		am.evaluateRule(rule)
	}
}

// evaluateRule 评估单个规则
func (am *AlertManager) evaluateRule(rule *AlertRule) {
	am.mutex.RLock()
	buffer, exists := am.metrics[rule.Metric]
	am.mutex.RUnlock()

	if !exists || len(buffer.Values) == 0 {
		return
	}

	// 获取评估窗口内的值
	now := time.Now()
	var recentValues []float64
	for i := len(buffer.Timestamps) - 1; i >= 0; i-- {
		if now.Sub(buffer.Timestamps[i]) <= rule.EvaluationWindow {
			recentValues = append([]float64{buffer.Values[i]}, recentValues...)
		} else {
			break
		}
	}

	if len(recentValues) == 0 {
		return
	}

	// 计算平均值
	sum := 0.0
	for _, v := range recentValues {
		sum += v
	}
	avgValue := sum / float64(len(recentValues))

	// 检查是否违反阈值
	violated := false
	if rule.ThresholdMax > 0 && avgValue > rule.ThresholdMax {
		violated = true
	}
	if rule.ThresholdMin > 0 && avgValue < rule.ThresholdMin {
		violated = true
	}

	if violated {
		am.triggerAlert(rule, avgValue)
	} else {
		am.resolveAlert(rule.ID)
	}
}

// RecordMetric 记录指标
func (am *AlertManager) RecordMetric(metric string, value float64) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	buffer, exists := am.metrics[metric]
	if !exists {
		buffer = &MetricBuffer{
			Values:     make([]float64, 0),
			Timestamps: make([]time.Time, 0),
			MaxSize:    1000,
		}
		am.metrics[metric] = buffer
	}

	now := time.Now()
	buffer.Values = append(buffer.Values, value)
	buffer.Timestamps = append(buffer.Timestamps, now)

	// 保持最大大小
	if len(buffer.Values) > buffer.MaxSize {
		buffer.Values = buffer.Values[1:]
		buffer.Timestamps = buffer.Timestamps[1:]
	}
}

// triggerAlert 触发告警
func (am *AlertManager) triggerAlert(rule *AlertRule, value float64) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	alertKey := fmt.Sprintf("%s_%s", rule.Component, rule.Metric)

	// 检查是否已存在相同告警（去重）
	if existingAlert, exists := am.alerts[alertKey]; exists {
		// 更新告警计数和时间
		existingAlert.Count++
		existingAlert.Timestamp = time.Now()
		existingAlert.Value = value
		return
	}

	// 创建新告警
	alert := &Alert{
		ID:          fmt.Sprintf("%s_%d", alertKey, time.Now().Unix()),
		Type:        AlertTypeThreshold,
		Severity:    rule.Severity,
		Component:   rule.Component,
		Metric:      rule.Metric,
		Message:     am.buildAlertMessage(rule, value),
		Value:       value,
		Threshold:   am.getThresholdValue(rule),
		Timestamp:   time.Now(),
		Resolved:    false,
		Count:       1,
		FirstSeenAt: time.Now(),
	}

	am.alerts[alertKey] = alert
	am.alertHistory = append(am.alertHistory, alert)

	// 更新Prometheus指标
	alertsTotal.WithLabelValues(string(alert.Severity), alert.Component, string(alert.Type)).Inc()
	activeAlerts.WithLabelValues(string(alert.Severity), alert.Component).Inc()

	// 发送通知
	go am.notifyAlert(alert)

	log.Printf("Alert triggered: [%s] %s - %s (value: %.2f)", alert.Severity, alert.Component, alert.Message, alert.Value)
}

// resolveAlert 解决告警
func (am *AlertManager) resolveAlert(ruleID string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	alertKey := ""
	for key, alert := range am.alerts {
		if alert.Component+"_"+alert.Metric == ruleID || key == ruleID {
			alertKey = key
			break
		}
	}

	if alertKey == "" {
		return
	}

	alert := am.alerts[alertKey]
	if !alert.Resolved {
		now := time.Now()
		alert.Resolved = true
		alert.ResolvedAt = &now

		// 更新Prometheus指标
		activeAlerts.WithLabelValues(string(alert.Severity), alert.Component).Dec()
		duration := now.Sub(alert.FirstSeenAt).Seconds()
		alertResolutionDuration.WithLabelValues(string(alert.Severity), alert.Component).Observe(duration)

		log.Printf("Alert resolved: [%s] %s - %s", alert.Severity, alert.Component, alert.Message)
	}

	delete(am.alerts, alertKey)
}

// buildAlertMessage 构建告警消息
func (am *AlertManager) buildAlertMessage(rule *AlertRule, value float64) string {
	threshold := am.getThresholdValue(rule)
	if rule.ThresholdMax > 0 {
		return fmt.Sprintf("%s: %s exceeded threshold (%.2f > %.2f)", rule.Name, rule.Metric, value, threshold)
	}
	return fmt.Sprintf("%s: %s below threshold (%.2f < %.2f)", rule.Name, rule.Metric, value, threshold)
}

// getThresholdValue 获取阈值
func (am *AlertManager) getThresholdValue(rule *AlertRule) float64 {
	if rule.ThresholdMax > 0 {
		return rule.ThresholdMax
	}
	return rule.ThresholdMin
}

// notifyAlert 通知告警
func (am *AlertManager) notifyAlert(alert *Alert) {
	for _, notifier := range am.notifiers {
		if err := notifier.Notify(alert); err != nil {
			log.Printf("Failed to send alert notification: %v", err)
		}
	}
}

// GetActiveAlerts 获取活跃告警
func (am *AlertManager) GetActiveAlerts() []*Alert {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	alerts := make([]*Alert, 0, len(am.alerts))
	for _, alert := range am.alerts {
		alerts = append(alerts, alert)
	}
	return alerts
}

// GetAlertHistory 获取告警历史
func (am *AlertManager) GetAlertHistory(limit int) []*Alert {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	start := 0
	if len(am.alertHistory) > limit {
		start = len(am.alertHistory) - limit
	}
	return am.alertHistory[start:]
}

// GetAlertRules 获取所有告警规则
func (am *AlertManager) GetAlertRules() []*AlertRule {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	rules := make([]*AlertRule, 0, len(am.rules))
	for _, rule := range am.rules {
		rules = append(rules, rule)
	}
	return rules
}

// AddAlertRule 添加告警规则
func (am *AlertManager) AddAlertRule(rule *AlertRule) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	am.rules[rule.ID] = rule
}

// RemoveAlertRule 移除告警规则
func (am *AlertManager) RemoveAlertRule(ruleID string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	delete(am.rules, ruleID)
}

// UpdateAlertRule 更新告警规则
func (am *AlertManager) UpdateAlertRule(rule *AlertRule) error {
	am.mutex.Lock()
	defer am.mutex.Unlock()

	if _, exists := am.rules[rule.ID]; !exists {
		return fmt.Errorf("rule not found: %s", rule.ID)
	}

	am.rules[rule.ID] = rule
	return nil
}

// AddNotifier 添加告警通知器
func (am *AlertManager) AddNotifier(notifier AlertNotifier) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	am.notifiers = append(am.notifiers, notifier)
}

// GetAlertStats 获取告警统计
func (am *AlertManager) GetAlertStats() map[string]interface{} {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_active_alerts"] = len(am.alerts)
	stats["total_historical_alerts"] = len(am.alertHistory)
	stats["total_rules"] = len(am.rules)

	// 按严重级别统计
	severityCount := make(map[AlertSeverity]int)
	for _, alert := range am.alerts {
		severityCount[alert.Severity]++
	}
	stats["alerts_by_severity"] = severityCount

	// 按组件统计
	componentCount := make(map[string]int)
	for _, alert := range am.alerts {
		componentCount[alert.Component]++
	}
	stats["alerts_by_component"] = componentCount

	return stats
}

// LogNotifier 日志通知器
type LogNotifier struct{}

// Notify 发送日志通知
func (ln *LogNotifier) Notify(alert *Alert) error {
	log.Printf("[ALERT] [%s] %s - %s: %s (value: %.2f, threshold: %.2f)",
		alert.Severity, alert.Component, alert.Metric, alert.Message, alert.Value, alert.Threshold)
	return nil
}
