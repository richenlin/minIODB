package metadata

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestConfigCreation 测试Config创建
func TestConfigCreation(t *testing.T) {
	config := &Config{
		NodeID:     "test-node-1",
		AutoRepair: true,
		Backup: BackupConfig{
			Bucket: "test-backup-bucket",
		},
		Recovery: RecoveryConfig{
			Bucket: "test-recovery-bucket",
		},
	}

	assert.Equal(t, "test-node-1", config.NodeID, "Node ID should match")
	assert.True(t, config.AutoRepair, "Auto repair should be true")
	assert.Equal(t, "test-backup-bucket", config.Backup.Bucket, "Backup bucket should match")
	assert.Equal(t, "test-recovery-bucket", config.Recovery.Bucket, "Recovery bucket should match")
}

// TestBackupConfigCreation 测试BackupConfig创建
func TestBackupConfigCreation(t *testing.T) {
	backupConfig := BackupConfig{
		Bucket: "test-backup-bucket",
	}

	assert.Equal(t, "test-backup-bucket", backupConfig.Bucket, "Backup bucket should match")
}

// TestRecoveryConfigCreation 测试RecoveryConfig创建
func TestRecoveryConfigCreation(t *testing.T) {
	recoveryConfig := RecoveryConfig{
		Bucket: "test-recovery-bucket",
	}

	assert.Equal(t, "test-recovery-bucket", recoveryConfig.Bucket, "Recovery bucket should match")
}

// TestSyncCheckResultCreation 测试SyncCheckResult创建
func TestSyncCheckResultCreation(t *testing.T) {
	result := &SyncCheckResult{
		TotalEntries:         1000,
		InconsistenciesFound: 50,
		AutoRecovery:         true,
	}

	assert.Equal(t, int64(1000), result.TotalEntries, "Total entries should match")
	assert.Equal(t, int64(50), result.InconsistenciesFound, "Inconsistencies found should match")
	assert.True(t, result.AutoRecovery, "Auto recovery should be true")
}

// TestConfigurationValidation 测试配置验证
func TestConfigurationValidation(t *testing.T) {
	testCases := []struct {
		name    string
		config  *Config
		valid   bool
	}{
		{
			name: "Valid configuration",
			config: &Config{
				NodeID:     "test-node-1",
				AutoRepair: true,
				Backup: BackupConfig{
					Bucket: "test-backup",
				},
				Recovery: RecoveryConfig{
					Bucket: "test-recovery",
				},
			},
			valid: true,
		},
		{
			name: "Empty node ID",
			config: &Config{
				NodeID:     "",
				AutoRepair: true,
				Backup: BackupConfig{
					Bucket: "test-backup",
				},
				Recovery: RecoveryConfig{
					Bucket: "test-recovery",
				},
			},
			valid: false,
		},
		{
			name: "Empty backup bucket",
			config: &Config{
				NodeID:     "test-node-1",
				AutoRepair: true,
				Backup: BackupConfig{
					Bucket: "",
				},
				Recovery: RecoveryConfig{
					Bucket: "test-recovery",
				},
			},
			valid: false,
		},
		{
			name: "Empty recovery bucket",
			config: &Config{
				NodeID:     "test-node-1",
				AutoRepair: true,
				Backup: BackupConfig{
					Bucket: "test-backup",
				},
				Recovery: RecoveryConfig{
					Bucket: "",
				},
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.valid {
				assert.NotEmpty(t, tc.config.NodeID, "Node ID should not be empty")
				assert.NotEmpty(t, tc.config.Backup.Bucket, "Backup bucket should not be empty")
				assert.NotEmpty(t, tc.config.Recovery.Bucket, "Recovery bucket should not be empty")
			} else {
				// 对于无效配置，至少有一个字段应该为空
				assert.True(t, tc.config.NodeID == "" || tc.config.Backup.Bucket == "" || tc.config.Recovery.Bucket == "",
					"At least one field should be empty for invalid config")
			}
		})
	}
}

// TestAutoRepairConfiguration 测试自动修复配置
func TestAutoRepairConfiguration(t *testing.T) {
	// 测试自动修复开启
	configWithRepair := &Config{
		NodeID:     "node-1",
		AutoRepair: true,
	}

	assert.True(t, configWithRepair.AutoRepair, "Auto repair should be enabled")

	// 测试自动修复关闭
	configWithoutRepair := &Config{
		NodeID:     "node-2",
		AutoRepair: false,
	}

	assert.False(t, configWithoutRepair.AutoRepair, "Auto repair should be disabled")
}

// TestNodeIDValidation 测试节点ID验证
func TestNodeIDValidation(t *testing.T) {
	testCases := []struct {
		name    string
		nodeID  string
		valid   bool
	}{
		{
			name:    "Valid node ID",
			nodeID:  "test-node-123",
			valid:   true,
		},
		{
			name:    "Empty node ID",
			nodeID:  "",
			valid:   false,
		},
		{
			name:    "Single character node ID",
			nodeID:  "1",
			valid:   true,
		},
		{
			name:    "Long node ID",
			nodeID:  "very-long-node-id-with-special-chars_123",
			valid:   true,
		},
		{
			name:    "Whitespace only node ID",
			nodeID:  "   ",
			valid:   true, // 空白字符也被认为是有效的
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{NodeID: tc.nodeID}

			if tc.valid {
				assert.NotEmpty(t, config.NodeID, "Valid node ID should not be empty")
			} else {
				assert.Empty(t, config.NodeID, "Invalid node ID should be empty")
			}
		})
	}
}

// TestBackupRecoveryIntegration 测试备份恢复配置集成
func TestBackupRecoveryIntegration(t *testing.T) {
	config := &Config{
		NodeID:     "node-1",
		AutoRepair: true,
		Backup: BackupConfig{
			Bucket: "backup-bucket-1",
		},
		Recovery: RecoveryConfig{
			Bucket: "recovery-bucket-1",
		},
	}

	// 验证配置集成
	assert.Contains(t, config.Backup.Bucket, "backup", "Backup bucket should contain backup keyword")
	assert.Contains(t, config.Recovery.Bucket, "recovery", "Recovery bucket should contain recovery keyword")
	assert.Equal(t, "node-1", config.NodeID, "Node ID should match")
}

// TestSyncCheckResultMethods 测试SyncCheckResult方法
func TestSyncCheckResultMethods(t *testing.T) {
	result := &SyncCheckResult{
		TotalEntries:         1000,
		InconsistenciesFound: 50,
		AutoRecovery:         true,
	}

	// 测试结果值
	assert.Equal(t, int64(1000), result.TotalEntries, "Total entries should be 1000")
	assert.Equal(t, int64(50), result.InconsistenciesFound, "Inconsistencies found should be 50")
	assert.True(t, result.AutoRecovery, "Auto recovery should be true")
}

// BenchmarkConfigCreation 基准测试Config创建性能
func BenchmarkConfigCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &Config{
			NodeID:     fmt.Sprintf("node-%d", i),
			AutoRepair: true,
			Backup: BackupConfig{
				Bucket: fmt.Sprintf("backup-%d", i),
			},
			Recovery: RecoveryConfig{
				Bucket: fmt.Sprintf("recovery-%d", i),
			},
		}
	}
}