package monitoring

import (
	"context"
	"testing"
	"time"
)

func TestNewAlertManager(t *testing.T) {
	deduplicateWindow := 5 * time.Minute
	manager := NewAlertManager(deduplicateWindow)

	if manager == nil {
		t.Fatal("NewAlertManager returned nil")
	}

	if manager.deduplicateWindow != deduplicateWindow {
		t.Errorf("Expected deduplicateWindow %v, got %v", deduplicateWindow, manager.deduplicateWindow)
	}
}

func TestLogNotifier(t *testing.T) {
	notifier := &LogNotifier{}

	alert := &Alert{
		ID:        "test_alert_1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityHigh,
		Component: "test",
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	// Should not error
	err := notifier.Notify(alert)
	if err != nil {
		t.Errorf("LogNotifier.Notify returned error: %v", err)
	}
}

func TestAddNotifier(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)
	notifier := &LogNotifier{}

	manager.AddNotifier(notifier)

	if len(manager.notifiers) != 1 {
		t.Errorf("Expected 1 notifier, got %d", len(manager.notifiers))
	}
}

func TestAddAlertRule(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	rule := &AlertRule{
		ID:           "test_rule",
		Name:         "Test Alert Rule",
		Component:    "test",
		Metric:       "test_metric",
		Severity:     SeverityHigh,
		ThresholdMax: 100.0,
		Enabled:      true,
	}

	manager.AddAlertRule(rule)

	// Verify rule was added
	rules := manager.GetAlertRules()
	if len(rules) == 0 {
		t.Error("Rule was not added")
	}
}

func TestRemoveAlertRule(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	rule := &AlertRule{
		ID:           "test_rule",
		Name:         "Test Alert Rule",
		Component:    "test",
		Metric:       "test_metric",
		Severity:     SeverityHigh,
		ThresholdMax: 100.0,
		Enabled:      true,
	}

	manager.AddAlertRule(rule)
	manager.RemoveAlertRule("test_rule")

	// Verify rule was removed
	rules := manager.GetAlertRules()
	found := false
	for _, r := range rules {
		if r.ID == "test_rule" {
			found = true
			break
		}
	}

	if found {
		t.Error("Rule was not removed")
	}
}

func TestRecordMetric(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	metric := "test.metric"
	value := 50.0

	manager.RecordMetric(metric, value)

	// Verify metric was recorded
	manager.mutex.RLock()
	buffer, exists := manager.metrics[metric]
	manager.mutex.RUnlock()

	if !exists {
		t.Error("Metric was not recorded")
	}

	if len(buffer.Values) != 1 {
		t.Errorf("Expected 1 value, got %d", len(buffer.Values))
	}

	if buffer.Values[0] != value {
		t.Errorf("Expected value %f, got %f", value, buffer.Values[0])
	}
}

func TestGetAlertRules(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	// Manager has default rules added automatically
	rules := manager.GetAlertRules()

	if len(rules) == 0 {
		t.Error("Expected default rules to be present")
	}

	// Verify rules have required fields
	for _, rule := range rules {
		if rule.ID == "" {
			t.Error("Rule has empty ID")
		}
		if rule.Name == "" {
			t.Error("Rule has empty name")
		}
		if rule.Severity == "" {
			t.Errorf("Rule %s has empty severity", rule.ID)
		}
	}
}

func TestStartStop(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)
	ctx := context.Background()

	// Start monitoring
	manager.Start(ctx)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop monitoring
	manager.Stop()

	// Test passes if no panic occurs
}

func TestGetActiveAlerts(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	// Initially should be empty
	alerts := manager.GetActiveAlerts()
	if len(alerts) != 0 {
		t.Errorf("Expected 0 active alerts, got %d", len(alerts))
	}

	// Add an alert manually for testing
	alert := &Alert{
		ID:        "test_1",
		Severity:  SeverityHigh,
		Timestamp: time.Now(),
	}

	manager.mutex.Lock()
	manager.alerts[alert.ID] = alert
	manager.mutex.Unlock()

	// Should now have 1 alert
	alerts = manager.GetActiveAlerts()
	if len(alerts) != 1 {
		t.Errorf("Expected 1 active alert, got %d", len(alerts))
	}
}

func TestGetAlertHistory(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	// Initially should be empty
	history := manager.GetAlertHistory(10)
	if len(history) != 0 {
		t.Errorf("Expected 0 history alerts, got %d", len(history))
	}

	// Add alerts to history manually for testing
	for i := 0; i < 5; i++ {
		alert := &Alert{
			ID:        "test_alert",
			Severity:  SeverityMedium,
			Timestamp: time.Now(),
		}
		manager.mutex.Lock()
		manager.alertHistory = append(manager.alertHistory, alert)
		manager.mutex.Unlock()
	}

	// Should retrieve history
	history = manager.GetAlertHistory(10)
	if len(history) != 5 {
		t.Errorf("Expected 5 history alerts, got %d", len(history))
	}

	// Test limit
	history = manager.GetAlertHistory(3)
	if len(history) != 3 {
		t.Errorf("Expected 3 history alerts (limited), got %d", len(history))
	}
}

func TestGetAlertStats(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	// Get stats
	stats := manager.GetAlertStats()

	if stats == nil {
		t.Error("GetAlertStats returned nil")
	}

	// Should have expected keys
	expectedKeys := []string{"total_rules", "total_active_alerts", "total_historical_alerts"}
	for _, key := range expectedKeys {
		if _, exists := stats[key]; !exists {
			t.Errorf("Stats missing key: %s", key)
		}
	}
}

func TestUpdateAlertRule(t *testing.T) {
	manager := NewAlertManager(5 * time.Minute)

	rule := &AlertRule{
		ID:           "test_rule",
		Name:         "Test Alert Rule",
		Component:    "test",
		Metric:       "test_metric",
		Severity:     SeverityHigh,
		ThresholdMax: 100.0,
		Enabled:      true,
	}

	manager.AddAlertRule(rule)

	// Update rule
	rule.ThresholdMax = 200.0
	err := manager.UpdateAlertRule(rule)
	if err != nil {
		t.Errorf("UpdateAlertRule failed: %v", err)
	}
}

func BenchmarkRecordMetric(b *testing.B) {
	manager := NewAlertManager(5 * time.Minute)

	metric := "test.metric"
	value := 50.0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.RecordMetric(metric, value)
	}
}

func BenchmarkNotifyAlert(b *testing.B) {
	manager := NewAlertManager(5 * time.Minute)

	manager.AddNotifier(&LogNotifier{})

	alert := &Alert{
		ID:        "benchmark_alert",
		Type:      AlertTypeThreshold,
		Severity:  SeverityMedium,
		Component: "benchmark",
		Message:   "Benchmark test",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, notifier := range manager.notifiers {
			notifier.Notify(alert)
		}
	}
}

func BenchmarkGetActiveAlerts(b *testing.B) {
	manager := NewAlertManager(5 * time.Minute)

	// Add some alerts
	for i := 0; i < 10; i++ {
		alert := &Alert{
			ID:        "alert_" + string(rune(i)),
			Severity:  SeverityMedium,
			Timestamp: time.Now(),
		}
		manager.mutex.Lock()
		manager.alerts[alert.ID] = alert
		manager.mutex.Unlock()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.GetActiveAlerts()
	}
}
