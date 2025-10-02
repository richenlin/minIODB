package storage

import (
	"context"
	"log"
	"sync"
	"time"
)

// RebalanceMonitor 自动重平衡监控器
type RebalanceMonitor struct {
	shardOptimizer *ShardOptimizer
	config         *RebalanceMonitorConfig
	isMonitoring   bool
	stopChan       chan struct{}
	mutex          sync.RWMutex
	lastRebalance  time.Time
	rebalanceCount int64
	stats          *RebalanceMonitorStats
}

// RebalanceMonitorConfig 重平衡监控配置
type RebalanceMonitorConfig struct {
	CheckInterval        time.Duration `json:"check_interval"`          // 检查间隔
	LoadBalanceThreshold float64       `json:"load_balance_threshold"`  // 负载均衡阈值
	MinRebalanceInterval time.Duration `json:"min_rebalance_interval"`  // 最小重平衡间隔
	MaxRebalancesPerHour int           `json:"max_rebalances_per_hour"` // 每小时最大重平衡次数
	EnableAutoRebalance  bool          `json:"enable_auto_rebalance"`   // 启用自动重平衡
	DataSkewThreshold    float64       `json:"data_skew_threshold"`     // 数据倾斜阈值
	HotSpotThreshold     float64       `json:"hot_spot_threshold"`      // 热点阈值
}

// RebalanceMonitorStats 重平衡监控统计
type RebalanceMonitorStats struct {
	TotalChecks         int64         `json:"total_checks"`
	TriggeredRebalances int64         `json:"triggered_rebalances"`
	SkippedRebalances   int64         `json:"skipped_rebalances"`
	LastCheckTime       time.Time     `json:"last_check_time"`
	LastRebalanceTime   time.Time     `json:"last_rebalance_time"`
	AverageLoadBalance  float64       `json:"average_load_balance"`
	CurrentDataSkew     float64       `json:"current_data_skew"`
	DetectedHotSpots    int           `json:"detected_hot_spots"`
	RebalanceDuration   time.Duration `json:"rebalance_duration"`
	mutex               sync.RWMutex
}

// DefaultRebalanceMonitorConfig 返回默认配置
func DefaultRebalanceMonitorConfig() *RebalanceMonitorConfig {
	return &RebalanceMonitorConfig{
		CheckInterval:        5 * time.Minute,  // 每5分钟检查一次
		LoadBalanceThreshold: 0.70,             // 负载均衡低于70%触发重平衡
		MinRebalanceInterval: 30 * time.Minute, // 最少30分钟间隔
		MaxRebalancesPerHour: 2,                // 每小时最多2次重平衡
		EnableAutoRebalance:  true,             // 启用自动重平衡
		DataSkewThreshold:    0.30,             // 数据倾斜超过30%触发
		HotSpotThreshold:     0.80,             // 热点访问超过80%触发
	}
}

// NewRebalanceMonitor 创建重平衡监控器
func NewRebalanceMonitor(shardOptimizer *ShardOptimizer, config *RebalanceMonitorConfig) *RebalanceMonitor {
	if config == nil {
		config = DefaultRebalanceMonitorConfig()
	}

	return &RebalanceMonitor{
		shardOptimizer: shardOptimizer,
		config:         config,
		stopChan:       make(chan struct{}),
		stats:          &RebalanceMonitorStats{},
	}
}

// Start 启动监控
func (rm *RebalanceMonitor) Start(ctx context.Context) {
	rm.mutex.Lock()
	if rm.isMonitoring {
		rm.mutex.Unlock()
		return
	}
	rm.isMonitoring = true
	rm.mutex.Unlock()

	log.Println("Starting rebalance monitor...")
	go rm.monitorLoop(ctx)
}

// Stop 停止监控
func (rm *RebalanceMonitor) Stop() {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if !rm.isMonitoring {
		return
	}

	close(rm.stopChan)
	rm.isMonitoring = false
	log.Println("Rebalance monitor stopped")
}

// monitorLoop 监控循环
func (rm *RebalanceMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(rm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.stopChan:
			return
		case <-ticker.C:
			rm.performCheck(ctx)
		}
	}
}

// performCheck 执行检查
func (rm *RebalanceMonitor) performCheck(ctx context.Context) {
	rm.stats.mutex.Lock()
	rm.stats.TotalChecks++
	rm.stats.LastCheckTime = time.Now()
	rm.stats.mutex.Unlock()

	// 获取分片统计信息
	shardStats := rm.shardOptimizer.GetStats()

	// 更新统计信息
	rm.stats.mutex.Lock()
	rm.stats.AverageLoadBalance = shardStats.LoadBalance
	rm.stats.CurrentDataSkew = rm.calculateDataSkew(shardStats)
	rm.stats.DetectedHotSpots = rm.detectHotSpots(shardStats)
	rm.stats.mutex.Unlock()

	log.Printf("Rebalance check: LoadBalance=%.2f, DataSkew=%.2f, HotSpots=%d",
		shardStats.LoadBalance, rm.stats.CurrentDataSkew, rm.stats.DetectedHotSpots)

	// 检查是否需要重平衡
	if rm.shouldTriggerRebalance(shardStats) {
		rm.triggerRebalance(ctx, "automatic_threshold_triggered")
	}
}

// shouldTriggerRebalance 判断是否应该触发重平衡
func (rm *RebalanceMonitor) shouldTriggerRebalance(shardStats *ShardStats) bool {
	// 如果未启用自动重平衡，返回false
	if !rm.config.EnableAutoRebalance {
		return false
	}

	// 检查距离上次重平衡的时间间隔
	if time.Since(rm.lastRebalance) < rm.config.MinRebalanceInterval {
		rm.stats.mutex.Lock()
		rm.stats.SkippedRebalances++
		rm.stats.mutex.Unlock()
		log.Printf("Skipping rebalance: too soon since last rebalance (%.0f minutes ago)",
			time.Since(rm.lastRebalance).Minutes())
		return false
	}

	// 检查每小时重平衡次数限制
	if rm.isRebalanceRateLimited() {
		rm.stats.mutex.Lock()
		rm.stats.SkippedRebalances++
		rm.stats.mutex.Unlock()
		log.Println("Skipping rebalance: rate limit exceeded")
		return false
	}

	// 检查负载均衡阈值
	if shardStats.LoadBalance < rm.config.LoadBalanceThreshold {
		log.Printf("Load imbalance detected: %.2f < %.2f",
			shardStats.LoadBalance, rm.config.LoadBalanceThreshold)
		return true
	}

	// 检查数据倾斜
	if rm.stats.CurrentDataSkew > rm.config.DataSkewThreshold {
		log.Printf("Data skew detected: %.2f > %.2f",
			rm.stats.CurrentDataSkew, rm.config.DataSkewThreshold)
		return true
	}

	// 检查热点
	if rm.stats.DetectedHotSpots > 0 {
		log.Printf("Hot spots detected: %d nodes", rm.stats.DetectedHotSpots)
		return true
	}

	return false
}

// isRebalanceRateLimited 检查是否受速率限制
func (rm *RebalanceMonitor) isRebalanceRateLimited() bool {
	// 统计过去一小时的重平衡次数
	oneHourAgo := time.Now().Add(-1 * time.Hour)

	// 简化实现：检查最近的重平衡时间
	if rm.lastRebalance.After(oneHourAgo) && rm.rebalanceCount >= int64(rm.config.MaxRebalancesPerHour) {
		return true
	}

	// 如果超过一小时，重置计数
	if rm.lastRebalance.Before(oneHourAgo) {
		rm.rebalanceCount = 0
	}

	return false
}

// triggerRebalance 触发重平衡
func (rm *RebalanceMonitor) triggerRebalance(ctx context.Context, reason string) {
	log.Printf("Triggering rebalance: %s", reason)

	startTime := time.Now()

	// 调用分片优化器的重平衡功能
	rm.shardOptimizer.rebalancer.TriggerRebalance(reason)

	duration := time.Since(startTime)

	// 更新统计信息
	rm.mutex.Lock()
	rm.lastRebalance = time.Now()
	rm.rebalanceCount++
	rm.mutex.Unlock()

	rm.stats.mutex.Lock()
	rm.stats.TriggeredRebalances++
	rm.stats.LastRebalanceTime = time.Now()
	rm.stats.RebalanceDuration = duration
	rm.stats.mutex.Unlock()

	log.Printf("Rebalance completed in %v", duration)
}

// calculateDataSkew 计算数据倾斜度
func (rm *RebalanceMonitor) calculateDataSkew(shardStats *ShardStats) float64 {
	if len(shardStats.NodeStats) < 2 {
		return 0.0
	}

	// 收集所有节点的数据大小
	var sizes []int64
	var sum int64
	for _, nodeStats := range shardStats.NodeStats {
		sizes = append(sizes, nodeStats.DataSize)
		sum += nodeStats.DataSize
	}

	if sum == 0 {
		return 0.0
	}

	// 计算平均值
	avg := float64(sum) / float64(len(sizes))

	// 计算标准差
	var variance float64
	for _, size := range sizes {
		diff := float64(size) - avg
		variance += diff * diff
	}
	variance /= float64(len(sizes))
	stdDev := variance // 简化，不开平方

	// 计算变异系数（CV = 标准差 / 平均值）
	if avg == 0 {
		return 0.0
	}

	return stdDev / (avg * avg)
}

// detectHotSpots 检测热点节点
func (rm *RebalanceMonitor) detectHotSpots(shardStats *ShardStats) int {
	if len(shardStats.NodeStats) == 0 {
		return 0
	}

	// 计算平均负载
	var totalLoad int64
	for _, nodeStats := range shardStats.NodeStats {
		totalLoad += int64(nodeStats.ShardCount)
	}

	avgLoad := float64(totalLoad) / float64(len(shardStats.NodeStats))

	// 检测超过阈值的节点
	hotSpots := 0
	threshold := avgLoad * (1.0 + rm.config.HotSpotThreshold)

	for _, nodeStats := range shardStats.NodeStats {
		if float64(nodeStats.ShardCount) > threshold {
			hotSpots++
		}
	}

	return hotSpots
}

// GetStats 获取监控统计
func (rm *RebalanceMonitor) GetStats() *RebalanceMonitorStats {
	rm.stats.mutex.RLock()
	defer rm.stats.mutex.RUnlock()

	// 返回副本
	statsCopy := &RebalanceMonitorStats{
		TotalChecks:         rm.stats.TotalChecks,
		TriggeredRebalances: rm.stats.TriggeredRebalances,
		SkippedRebalances:   rm.stats.SkippedRebalances,
		LastCheckTime:       rm.stats.LastCheckTime,
		LastRebalanceTime:   rm.stats.LastRebalanceTime,
		AverageLoadBalance:  rm.stats.AverageLoadBalance,
		CurrentDataSkew:     rm.stats.CurrentDataSkew,
		DetectedHotSpots:    rm.stats.DetectedHotSpots,
		RebalanceDuration:   rm.stats.RebalanceDuration,
	}

	return statsCopy
}

// ForceRebalance 强制执行重平衡
func (rm *RebalanceMonitor) ForceRebalance(ctx context.Context, reason string) {
	log.Printf("Force rebalance requested: %s", reason)
	rm.triggerRebalance(ctx, "manual_"+reason)
}

// UpdateConfig 更新配置
func (rm *RebalanceMonitor) UpdateConfig(config *RebalanceMonitorConfig) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()
	rm.config = config
	log.Println("Rebalance monitor configuration updated")
}

// IsMonitoring 检查是否正在监控
func (rm *RebalanceMonitor) IsMonitoring() bool {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()
	return rm.isMonitoring
}
