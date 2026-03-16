//go:build dashboard

package client

import (
	"context"
	"errors"

	. "minIODB/internal/dashboard/model"
)

var ErrNotImplemented = errors.New("not implemented")

type CoreClient interface {
	GetHealth(ctx context.Context) (*HealthResult, error)
	GetStatus(ctx context.Context) (*StatusResult, error)
	GetMetrics(ctx context.Context) (*MetricsResult, error)
	GetNodes(ctx context.Context) ([]*NodeResult, error)
	GetClusterConfig(ctx context.Context) (map[string]interface{}, error)

	ListTables(ctx context.Context) ([]*TableResult, error)
	GetTable(ctx context.Context, name string) (*TableDetailResult, error)
	CreateTable(ctx context.Context, req *CreateTableRequest) error
	UpdateTable(ctx context.Context, name string, req *UpdateTableRequest) error
	DeleteTable(ctx context.Context, name string) error
	GetTableStats(ctx context.Context, name string) (*TableStatsResult, error)

	BrowseData(ctx context.Context, table string, params *BrowseParams) (*BrowseResult, error)
	WriteRecord(ctx context.Context, table string, req *WriteRecordRequest) error
	WriteBatch(ctx context.Context, table string, records []*WriteRecordRequest) error
	UpdateRecord(ctx context.Context, table, id string, req *UpdateRecordRequest) error
	DeleteRecord(ctx context.Context, table, id, day string) error
	QuerySQL(ctx context.Context, sql string) (*QueryResult, error)

	ListBackups(ctx context.Context) ([]*BackupResult, error)
	TriggerMetadataBackup(ctx context.Context) (*BackupResult, error)
	TriggerFullBackup(ctx context.Context) (*BackupResult, error)
	TriggerTableBackup(ctx context.Context, tableName string) (*BackupResult, error)
	GetBackup(ctx context.Context, id string) (*BackupResult, error)
	RestoreBackup(ctx context.Context, id string, opts *RestoreOptions) error
	DeleteBackup(ctx context.Context, id string) error
	GetMetadataStatus(ctx context.Context) (*MetadataStatusResult, error)

	QueryLogs(ctx context.Context, params *LogQueryParams) (*LogQueryResult, error)
	ListLogFiles(ctx context.Context) ([]string, error)

	GetMonitorOverview(ctx context.Context) (*MonitorOverviewResult, error)
	GetSLA(ctx context.Context) (*SLAResult, error)
	ScrapePrometheus(ctx context.Context) ([]byte, error)

	AnalyticsQuery(ctx context.Context, sql string) (*AnalyticsQueryResult, error)
	GetAnalyticsOverview(ctx context.Context) (*AnalyticsOverviewResult, error)

	// GetFullConfig returns the current editable configuration in a structured form.
	GetFullConfig(ctx context.Context) (*FullConfig, error)

	// UpdateConfig accepts a partial config update, applies it in-memory, and
	// returns a YAML snippet that the operator can persist to config.yaml before
	// restarting the node.
	UpdateConfig(ctx context.Context, req *ConfigUpdateRequest) (*ConfigUpdateResult, error)

	// EnableDistributedMode generates the config snippet required to switch to
	// distributed mode.  It does NOT restart the process; the caller must apply
	// the snippet and restart the node manually.
	EnableDistributedMode(ctx context.Context, req *EnableDistributedRequest) (*EnableDistributedResult, error)
}
