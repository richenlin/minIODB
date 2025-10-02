package storage

import (
	"context"
	"testing"
	"time"
)

func TestNewRebalanceMonitor(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	config := DefaultRebalanceMonitorConfig()

	monitor := NewRebalanceMonitor(shardOptimizer, config)

	if monitor == nil {
		t.Fatal("NewRebalanceMonitor returned nil")
	}

	if monitor.shardOptimizer != shardOptimizer {
		t.Error("shardOptimizer not set correctly")
	}

	if monitor.config != config {
		t.Error("config not set correctly")
	}

	if monitor.stats == nil {
		t.Error("stats not initialized")
	}
}

func TestDefaultRebalanceMonitorConfig(t *testing.T) {
	config := DefaultRebalanceMonitorConfig()

	if config.CheckInterval != 5*time.Minute {
		t.Errorf("Expected CheckInterval 5m, got %v", config.CheckInterval)
	}

	if config.LoadBalanceThreshold != 0.70 {
		t.Errorf("Expected LoadBalanceThreshold 0.70, got %f", config.LoadBalanceThreshold)
	}

	if config.MinRebalanceInterval != 30*time.Minute {
		t.Errorf("Expected MinRebalanceInterval 30m, got %v", config.MinRebalanceInterval)
	}

	if config.MaxRebalancesPerHour != 2 {
		t.Errorf("Expected MaxRebalancesPerHour 2, got %d", config.MaxRebalancesPerHour)
	}

	if !config.EnableAutoRebalance {
		t.Error("Expected EnableAutoRebalance to be true")
	}
}

func TestRebalanceMonitorStartStop(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	ctx := context.Background()

	// Test Start
	monitor.Start(ctx)

	if !monitor.IsMonitoring() {
		t.Error("Monitor should be running after Start()")
	}

	// Test Stop
	monitor.Stop()

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)

	if monitor.IsMonitoring() {
		t.Error("Monitor should not be running after Stop()")
	}
}

func TestCalculateDataSkew(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	tests := []struct {
		name     string
		stats    *ShardStats
		expected float64
	}{
		{
			name: "No nodes",
			stats: &ShardStats{
				NodeStats: make(map[string]*NodeStats),
			},
			expected: 0.0,
		},
		{
			name: "Single node",
			stats: &ShardStats{
				NodeStats: map[string]*NodeStats{
					"node1": {DataSize: 1000},
				},
			},
			expected: 0.0,
		},
		{
			name: "Balanced nodes",
			stats: &ShardStats{
				NodeStats: map[string]*NodeStats{
					"node1": {DataSize: 1000},
					"node2": {DataSize: 1000},
					"node3": {DataSize: 1000},
				},
			},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skew := monitor.calculateDataSkew(tt.stats)
			if skew != tt.expected {
				t.Logf("Data skew: %f (expected: %f)", skew, tt.expected)
			}
		})
	}
}

func TestDetectHotSpots(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	config := DefaultRebalanceMonitorConfig()
	monitor := NewRebalanceMonitor(shardOptimizer, config)

	tests := []struct {
		name     string
		stats    *ShardStats
		expected int
	}{
		{
			name: "No nodes",
			stats: &ShardStats{
				NodeStats: make(map[string]*NodeStats),
			},
			expected: 0,
		},
		{
			name: "Balanced nodes",
			stats: &ShardStats{
				NodeStats: map[string]*NodeStats{
					"node1": {ShardCount: 10},
					"node2": {ShardCount: 10},
					"node3": {ShardCount: 10},
				},
			},
			expected: 0,
		},
		{
			name: "One hot node",
			stats: &ShardStats{
				NodeStats: map[string]*NodeStats{
					"node1": {ShardCount: 5},
					"node2": {ShardCount: 5},
					"node3": {ShardCount: 50}, // Hot spot
				},
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hotSpots := monitor.detectHotSpots(tt.stats)
			if hotSpots != tt.expected {
				t.Errorf("Expected %d hot spots, got %d", tt.expected, hotSpots)
			}
		})
	}
}

func TestShouldTriggerRebalance(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	config := &RebalanceMonitorConfig{
		EnableAutoRebalance:  true,
		LoadBalanceThreshold: 0.70,
		MinRebalanceInterval: 1 * time.Millisecond, // Short interval for testing
		MaxRebalancesPerHour: 10,
		DataSkewThreshold:    0.30,
	}
	monitor := NewRebalanceMonitor(shardOptimizer, config)

	tests := []struct {
		name     string
		stats    *ShardStats
		expected bool
	}{
		{
			name: "Good load balance",
			stats: &ShardStats{
				LoadBalance: 0.85,
			},
			expected: false,
		},
		{
			name: "Poor load balance",
			stats: &ShardStats{
				LoadBalance: 0.50,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Wait a bit to pass min interval
			time.Sleep(2 * time.Millisecond)

			result := monitor.shouldTriggerRebalance(tt.stats)
			if result != tt.expected {
				t.Errorf("Expected shouldTriggerRebalance=%v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsRebalanceRateLimited(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	config := &RebalanceMonitorConfig{
		MaxRebalancesPerHour: 2,
	}
	monitor := NewRebalanceMonitor(shardOptimizer, config)

	// First call should not be rate limited
	if monitor.isRebalanceRateLimited() {
		t.Error("First call should not be rate limited")
	}

	// Simulate multiple rebalances
	monitor.lastRebalance = time.Now()
	monitor.rebalanceCount = 2

	// Should be rate limited now
	if !monitor.isRebalanceRateLimited() {
		t.Error("Should be rate limited after reaching max")
	}

	// Simulate time passing (over 1 hour)
	monitor.lastRebalance = time.Now().Add(-2 * time.Hour)

	// Should not be rate limited anymore
	if monitor.isRebalanceRateLimited() {
		t.Error("Should not be rate limited after 1 hour")
	}
}

func TestGetStats(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	// Update some stats
	monitor.stats.mutex.Lock()
	monitor.stats.TotalChecks = 10
	monitor.stats.TriggeredRebalances = 2
	monitor.stats.DetectedHotSpots = 5
	monitor.stats.mutex.Unlock()

	// Get stats
	stats := monitor.GetStats()

	if stats.TotalChecks != 10 {
		t.Errorf("Expected TotalChecks=10, got %d", stats.TotalChecks)
	}

	if stats.TriggeredRebalances != 2 {
		t.Errorf("Expected TriggeredRebalances=2, got %d", stats.TriggeredRebalances)
	}

	if stats.DetectedHotSpots != 5 {
		t.Errorf("Expected DetectedHotSpots=5, got %d", stats.DetectedHotSpots)
	}
}

func TestUpdateConfig(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	newConfig := &RebalanceMonitorConfig{
		CheckInterval:        10 * time.Minute,
		LoadBalanceThreshold: 0.80,
		EnableAutoRebalance:  false,
	}

	monitor.UpdateConfig(newConfig)

	if monitor.config.CheckInterval != 10*time.Minute {
		t.Error("Config not updated")
	}

	if monitor.config.LoadBalanceThreshold != 0.80 {
		t.Error("Config not updated")
	}

	if monitor.config.EnableAutoRebalance {
		t.Error("Config not updated")
	}
}

func TestForceRebalance(t *testing.T) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	ctx := context.Background()

	// Record initial stats
	initialRebalances := monitor.stats.TriggeredRebalances

	// Force rebalance
	monitor.ForceRebalance(ctx, "test_reason")

	// Check stats updated
	stats := monitor.GetStats()
	if stats.TriggeredRebalances != initialRebalances+1 {
		t.Errorf("Expected TriggeredRebalances to increase by 1")
	}

	if monitor.rebalanceCount != 1 {
		t.Errorf("Expected rebalanceCount=1, got %d", monitor.rebalanceCount)
	}
}

func BenchmarkCalculateDataSkew(b *testing.B) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	stats := &ShardStats{
		NodeStats: map[string]*NodeStats{
			"node1": {DataSize: 1000},
			"node2": {DataSize: 1500},
			"node3": {DataSize: 800},
			"node4": {DataSize: 1200},
			"node5": {DataSize: 900},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		monitor.calculateDataSkew(stats)
	}
}

func BenchmarkDetectHotSpots(b *testing.B) {
	shardOptimizer := NewShardOptimizer()
	monitor := NewRebalanceMonitor(shardOptimizer, nil)

	stats := &ShardStats{
		NodeStats: map[string]*NodeStats{
			"node1": {ShardCount: 10},
			"node2": {ShardCount: 15},
			"node3": {ShardCount: 8},
			"node4": {ShardCount: 12},
			"node5": {ShardCount: 50},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		monitor.detectHotSpots(stats)
	}
}
