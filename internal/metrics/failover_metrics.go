package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// FailoverTotal 故障切换次数
	FailoverTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "miniodb_pool_failover_total",
			Help: "Total number of pool failovers",
		},
		[]string{"from_state", "to_state"},
	)

	// CurrentActivePool 当前使用的池（0=主池，1=备份池）
	CurrentActivePool = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_pool_current_active",
			Help: "Current active pool (0=primary, 1=backup)",
		},
	)

	// BackupSyncQueueSize 异步同步队列大小
	BackupSyncQueueSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_backup_sync_queue_size",
			Help: "Current backup sync queue size",
		},
	)

	// BackupSyncSuccess 异步同步成功计数
	BackupSyncSuccess = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "miniodb_backup_sync_success_total",
			Help: "Total successful backup syncs",
		},
	)

	// BackupSyncFailed 异步同步失败计数
	BackupSyncFailed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "miniodb_backup_sync_failed_total",
			Help: "Total failed backup syncs",
		},
	)

	// BackupSyncDropped 异步同步丢弃计数（队列满）
	BackupSyncDropped = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "miniodb_backup_sync_dropped_total",
			Help: "Total dropped backup syncs (queue full)",
		},
	)

	// PrimaryPoolHealthy 主池健康状态（0=不健康，1=健康）
	PrimaryPoolHealthy = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_pool_primary_healthy",
			Help: "Primary pool health status (0=unhealthy, 1=healthy)",
		},
	)

	// BackupPoolHealthy 备份池健康状态（0=不健康，1=健康）
	BackupPoolHealthy = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "miniodb_pool_backup_healthy",
			Help: "Backup pool health status (0=unhealthy, 1=healthy)",
		},
	)

	// SyncQueueDropped 同步队列满时丢弃的任务计数（用于告警）
	SyncQueueDropped = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "miniodb_sync_queue_dropped_total",
			Help: "Total number of sync tasks dropped due to queue full (alert metric)",
		},
	)

	// SyncQueueBlocked 同步队列满时阻塞等待的任务计数
	SyncQueueBlocked = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "miniodb_sync_queue_blocked_total",
			Help: "Total number of sync tasks that blocked waiting for queue space",
		},
	)
)
