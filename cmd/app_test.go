package main

import (
	"context"
	"testing"
	"time"

	"minIODB/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func newTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			GrpcPort: "50051",
			RestPort: "8080",
			NodeID:   "test-node-1",
		},
		MinIO: config.MinioConfig{
			Endpoint:        "localhost:9000",
			AccessKeyID:     "minioadmin",
			SecretAccessKey: "minioadmin",
			UseSSL:          false,
			Bucket:          "test-bucket",
		},
		Backup: config.BackupConfig{
			Enabled: false,
			Metadata: config.MetadataBackupConfig{
				Enabled: false,
			},
		},
		Buffer: config.BufferConfig{
			BufferSize:    100,
			FlushInterval: 30 * time.Second,
		},
		Metrics: config.MetricsConfig{
			Enabled: false,
			Port:    "9090",
		},
		Dashboard: config.DashboardConfig{
			Enabled: false,
		},
		Compaction: config.CompactionConfig{
			Enabled:           false,
			CheckInterval:     5 * time.Minute,
			TargetFileSize:    134217728,
			MinFilesToCompact: 4,
			MaxFilesToCompact: 100,
			CooldownPeriod:    10 * time.Minute,
		},
		Subscription: config.SubscriptionConfig{
			Enabled: false,
		},
		Tables: config.TablesConfig{
			DefaultConfig: config.TableConfig{
				BufferSize:    100,
				FlushInterval: 30 * time.Second,
			},
		},
		TableManagement: config.TableManagementConfig{
			DefaultTable: "test_table",
		},
	}
}

func newTestLogger(t *testing.T) *zap.Logger {
	return zaptest.NewLogger(t)
}

func TestNewApp(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)

	app := NewApp(cfg, logger, "/tmp/test-config.yaml")

	require.NotNil(t, app)
	assert.Equal(t, cfg, app.cfg)
	assert.Equal(t, logger, app.logger)
	assert.Equal(t, "/tmp/test-config.yaml", app.configPath)
	assert.NotNil(t, app.ctx)
	assert.NotNil(t, app.cancel)
	assert.NotNil(t, app.fatalCh)
	assert.False(t, app.startTime.IsZero())
}

func TestNewApp_NilConfig(t *testing.T) {
	logger := newTestLogger(t)

	app := NewApp(nil, logger, "")

	require.NotNil(t, app)
	assert.Nil(t, app.cfg)
	assert.Equal(t, logger, app.logger)
}

func TestNewApp_NilLogger(t *testing.T) {
	cfg := newTestConfig()

	app := NewApp(cfg, nil, "")

	require.NotNil(t, app)
	assert.Equal(t, cfg, app.cfg)
	assert.Nil(t, app.logger)
}

func TestApp_FatalCh(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	ch := app.FatalCh()

	assert.NotNil(t, ch)
}

func TestApp_Context(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	ctx := app.Context()

	assert.NotNil(t, ctx)
	assert.Equal(t, app.ctx, ctx)
}

func TestApp_Cancel(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	cancel := app.Cancel()

	require.NotNil(t, cancel)

	ctx := app.Context()
	require.Nil(t, ctx.Err(), "Context should not be canceled initially")

	cancel()

	select {
	case <-ctx.Done():
		assert.Equal(t, context.Canceled, ctx.Err())
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be canceled after cancel() is called")
	}
}

func TestApp_Close_NilDependencies(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	assert.NotPanics(t, func() {
		app.Close()
	})
}

func TestApp_ContextCancellation(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	ctx := app.Context()
	assert.Nil(t, ctx.Err(), "Context should not be canceled initially")

	app.Cancel()()

	assert.Eventually(t, func() bool {
		return ctx.Err() == context.Canceled
	}, 100*time.Millisecond, 10*time.Millisecond, "Context should be canceled after Cancel() is called")
}

func TestApp_FatalCh_Capacity(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	ch := app.FatalCh()

	assert.Equal(t, 3, cap(ch), "fatalCh should have capacity of 3")
}

func TestApp_StartTime(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)

	beforeCreate := time.Now()
	app := NewApp(cfg, logger, "")
	afterCreate := time.Now()

	assert.True(t, app.startTime.After(beforeCreate) || app.startTime.Equal(beforeCreate))
	assert.True(t, app.startTime.Before(afterCreate) || app.startTime.Equal(afterCreate))
}

func TestApp_MultipleNewAppCalls(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)

	app1 := NewApp(cfg, logger, "path1")
	app2 := NewApp(cfg, logger, "path2")

	assert.NotSame(t, app1, app2, "Each NewApp call should create a new instance")
	assert.NotSame(t, app1.ctx, app2.ctx, "Each app should have its own context")
}

func TestApp_IndependentContexts(t *testing.T) {
	cfg := newTestConfig()
	logger := newTestLogger(t)

	app1 := NewApp(cfg, logger, "")
	app2 := NewApp(cfg, logger, "")

	ctx1 := app1.Context()
	ctx2 := app2.Context()

	require.Nil(t, ctx1.Err())
	require.Nil(t, ctx2.Err())

	app1.Cancel()()

	assert.Eventually(t, func() bool {
		return ctx1.Err() == context.Canceled
	}, 100*time.Millisecond, 10*time.Millisecond, "app1 context should be canceled")

	assert.Nil(t, ctx2.Err(), "app2 context should NOT be canceled when app1 is canceled")
}

func TestApp_ConfigPath(t *testing.T) {
	testCases := []struct {
		name       string
		configPath string
	}{
		{"empty path", ""},
		{"relative path", "./config.yaml"},
		{"absolute path", "/etc/miniodb/config.yaml"},
		{"path with spaces", "/path with spaces/config.yaml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig()
			logger := newTestLogger(t)

			app := NewApp(cfg, logger, tc.configPath)

			assert.Equal(t, tc.configPath, app.configPath)
		})
	}
}

func TestApp_BuildCompactionConfig_Disabled(t *testing.T) {
	cfg := newTestConfig()
	cfg.Compaction.Tiered.Enabled = false
	cfg.Compaction.TargetFileSize = 1000000
	cfg.Compaction.MinFilesToCompact = 3
	cfg.Compaction.MaxFilesToCompact = 50
	cfg.Compaction.CooldownPeriod = 5 * time.Minute
	cfg.Compaction.CheckInterval = 2 * time.Minute
	cfg.Compaction.TempDir = "/tmp/compaction"
	cfg.Compaction.CompressionType = "snappy"
	cfg.Compaction.MaxRowsPerFile = 10000

	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	result := app.buildCompactionConfig()

	require.NotNil(t, result)
	assert.Equal(t, int64(1000000), result.TargetFileSize)
	assert.Equal(t, 3, result.MinFilesToCompact)
	assert.Equal(t, 50, result.MaxFilesToCompact)
	assert.Equal(t, 5*time.Minute, result.CooldownPeriod)
	assert.Equal(t, 2*time.Minute, result.CheckInterval)
	assert.Equal(t, "/tmp/compaction", result.TempDir)
	assert.Equal(t, "snappy", result.CompressionType)
	assert.Equal(t, int64(10000), result.MaxRowsPerFile)
	assert.Nil(t, result.TieredConfig)
}

func TestApp_BuildCompactionConfig_TieredEnabled(t *testing.T) {
	cfg := newTestConfig()
	cfg.Compaction.Tiered.Enabled = true
	cfg.Compaction.Tiered.MaxFilesToMerge = 10
	cfg.Compaction.Tiered.Levels = []config.CompactionLevelConfig{
		{
			Name:            "L0",
			MaxFileSize:     1000000,
			TargetFileSize:  500000,
			MinFilesToMerge: 4,
			CooldownPeriod:  5 * time.Minute,
		},
		{
			Name:            "L1",
			MaxFileSize:     5000000,
			TargetFileSize:  2500000,
			MinFilesToMerge: 3,
			CooldownPeriod:  10 * time.Minute,
		},
	}

	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	result := app.buildCompactionConfig()

	require.NotNil(t, result)
	require.NotNil(t, result.TieredConfig)
	assert.True(t, result.TieredConfig.Enabled)
	assert.Equal(t, 10, result.TieredConfig.MaxFilesToMerge)
	assert.Len(t, result.TieredConfig.Levels, 2)
	assert.Equal(t, "L0", result.TieredConfig.Levels[0].Name)
	assert.Equal(t, int64(1000000), result.TieredConfig.Levels[0].MaxFileSize)
	assert.Equal(t, "L1", result.TieredConfig.Levels[1].Name)
	assert.Equal(t, int64(5000000), result.TieredConfig.Levels[1].MaxFileSize)
}

func TestApp_InjectSystemPlans_NoMetadataPlan(t *testing.T) {
	cfg := newTestConfig()
	cfg.Backup.Metadata.Enabled = true
	cfg.Backup.Metadata.Interval = 15 * time.Minute
	cfg.Backup.Schedules = nil

	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	app.injectSystemPlans()

	assert.Len(t, cfg.Backup.Schedules, 1)
	assert.Equal(t, "system-metadata-backup", cfg.Backup.Schedules[0].ID)
	assert.Equal(t, "System Metadata Backup", cfg.Backup.Schedules[0].Name)
	assert.True(t, cfg.Backup.Schedules[0].Enabled)
	assert.Equal(t, "metadata", cfg.Backup.Schedules[0].BackupType)
	assert.Equal(t, 15*time.Minute, cfg.Backup.Schedules[0].Interval)
	assert.Equal(t, 7, cfg.Backup.Schedules[0].RetentionDays)
	assert.True(t, cfg.Backup.Schedules[0].System)
}

func TestApp_InjectSystemPlans_HasMetadataPlan(t *testing.T) {
	cfg := newTestConfig()
	cfg.Backup.Metadata.Enabled = true
	cfg.Backup.Metadata.Interval = 15 * time.Minute
	cfg.Backup.Schedules = []config.BackupSchedule{
		{
			ID:         "existing-metadata-plan",
			Name:       "Existing Metadata Plan",
			Enabled:    true,
			BackupType: "metadata",
			Interval:   10 * time.Minute,
		},
	}

	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	app.injectSystemPlans()

	assert.Len(t, cfg.Backup.Schedules, 1)
	assert.Equal(t, "existing-metadata-plan", cfg.Backup.Schedules[0].ID)
}

func TestApp_InjectSystemPlans_MetadataDisabled(t *testing.T) {
	cfg := newTestConfig()
	cfg.Backup.Metadata.Enabled = false
	cfg.Backup.Schedules = nil

	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	app.injectSystemPlans()

	assert.Len(t, cfg.Backup.Schedules, 0)
}

func TestApp_InjectSystemPlans_DefaultInterval(t *testing.T) {
	cfg := newTestConfig()
	cfg.Backup.Metadata.Enabled = true
	cfg.Backup.Metadata.Interval = 0
	cfg.Backup.Schedules = nil

	logger := newTestLogger(t)
	app := NewApp(cfg, logger, "")

	app.injectSystemPlans()

	assert.Len(t, cfg.Backup.Schedules, 1)
	assert.Equal(t, 30*time.Minute, cfg.Backup.Schedules[0].Interval, "Should use default interval when interval is <= 0")
}
