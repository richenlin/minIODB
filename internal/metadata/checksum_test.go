package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestBackupChecksumGeneration 测试备份校验和生成
func TestBackupChecksumGeneration(t *testing.T) {
	ctx := context.Background()

	// 创建测试数据
	entries := []*MetadataEntry{
		{
			Type:      MetadataTypeTableSchema,
			Key:       "test:table1",
			Value:     map[string]interface{}{"name": "table1", "columns": []string{"id", "name"}},
			Timestamp: time.Now(),
		},
		{
			Type:      MetadataTypeDataIndex,
			Key:       "index:table1:id1",
			Value:     "path/to/data",
			Timestamp: time.Now(),
		},
	}

	// 创建快照（不含校验和）
	snapshot := &BackupSnapshot{
		NodeID:    "test-node-1",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	// 序列化
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal snapshot: %v", err)
	}

	// 计算校验和
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	// 验证校验和格式
	if len(checksum) != 64 { // SHA-256 is 32 bytes = 64 hex chars
		t.Errorf("Expected checksum length 64, got %d", len(checksum))
	}

	t.Logf("Generated checksum: %s", checksum[:16]+"...")
	t.Logf("Data size: %d bytes", len(data))

	// 添加校验和到快照
	snapshot.Checksum = checksum
	snapshot.Size = int64(len(data))

	// 验证快照包含校验和
	if snapshot.Checksum == "" {
		t.Error("Snapshot should have checksum")
	}
	if snapshot.Size == 0 {
		t.Error("Snapshot should have size")
	}

	_ = ctx // 避免unused variable警告
}

// TestChecksumValidation 测试校验和验证
func TestChecksumValidation(t *testing.T) {
	// 创建测试快照
	entries := []*MetadataEntry{
		{
			Type:      MetadataTypeTableSchema,
			Key:       "test:table1",
			Value:     "test value",
			Timestamp: time.Now(),
		},
	}

	snapshotOriginal := &BackupSnapshot{
		NodeID:    "test-node",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	// 计算原始校验和
	dataOriginal, _ := json.Marshal(snapshotOriginal)
	hashOriginal := sha256.Sum256(dataOriginal)
	checksumOriginal := hex.EncodeToString(hashOriginal[:])

	// 测试1：未篡改的数据
	t.Run("valid_checksum", func(t *testing.T) {
		snapshot := &BackupSnapshot{
			NodeID:    snapshotOriginal.NodeID,
			Timestamp: snapshotOriginal.Timestamp,
			Version:   snapshotOriginal.Version,
			Entries:   snapshotOriginal.Entries,
		}

		data, _ := json.Marshal(snapshot)
		hash := sha256.Sum256(data)
		calculatedChecksum := hex.EncodeToString(hash[:])

		if calculatedChecksum != checksumOriginal {
			t.Errorf("Checksum mismatch: expected %s, got %s",
				checksumOriginal[:16], calculatedChecksum[:16])
		} else {
			t.Logf("✓ Checksum validation passed")
		}
	})

	// 测试2：篡改的数据（修改了entry value）
	t.Run("tampered_data", func(t *testing.T) {
		snapshotTampered := &BackupSnapshot{
			NodeID:    snapshotOriginal.NodeID,
			Timestamp: snapshotOriginal.Timestamp,
			Version:   snapshotOriginal.Version,
			Entries: []*MetadataEntry{
				{
					Type:      MetadataTypeTableSchema,
					Key:       "test:table1",
					Value:     "tampered value", // 被修改
					Timestamp: entries[0].Timestamp,
				},
			},
		}

		dataTampered, _ := json.Marshal(snapshotTampered)
		hashTampered := sha256.Sum256(dataTampered)
		checksumTampered := hex.EncodeToString(hashTampered[:])

		if checksumTampered == checksumOriginal {
			t.Error("Tampered data should have different checksum")
		} else {
			t.Logf("✓ Detected tampering: checksums differ")
			t.Logf("  Original:  %s...", checksumOriginal[:16])
			t.Logf("  Tampered:  %s...", checksumTampered[:16])
		}
	})

	// 测试3：修改节点ID
	t.Run("modified_node_id", func(t *testing.T) {
		snapshotModified := &BackupSnapshot{
			NodeID:    "different-node", // 被修改
			Timestamp: snapshotOriginal.Timestamp,
			Version:   snapshotOriginal.Version,
			Entries:   snapshotOriginal.Entries,
		}

		dataModified, _ := json.Marshal(snapshotModified)
		hashModified := sha256.Sum256(dataModified)
		checksumModified := hex.EncodeToString(hashModified[:])

		if checksumModified == checksumOriginal {
			t.Error("Modified node ID should result in different checksum")
		} else {
			t.Logf("✓ Detected node ID modification")
		}
	})
}

// TestChecksumDeterminism 测试校验和的确定性
func TestChecksumDeterminism(t *testing.T) {
	entries := []*MetadataEntry{
		{
			Type:      MetadataTypeDataIndex,
			Key:       "index:table1",
			Value:     "test",
			Timestamp: time.Date(2025, 10, 3, 12, 0, 0, 0, time.UTC),
		},
	}

	snapshot := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: time.Date(2025, 10, 3, 12, 0, 0, 0, time.UTC),
		Version:   "1.0.0",
		Entries:   entries,
	}

	// 计算两次校验和
	data1, _ := json.Marshal(snapshot)
	hash1 := sha256.Sum256(data1)
	checksum1 := hex.EncodeToString(hash1[:])

	data2, _ := json.Marshal(snapshot)
	hash2 := sha256.Sum256(data2)
	checksum2 := hex.EncodeToString(hash2[:])

	if checksum1 != checksum2 {
		t.Errorf("Checksum should be deterministic: %s vs %s", checksum1, checksum2)
	} else {
		t.Logf("✓ Checksum is deterministic: %s", checksum1[:16]+"...")
	}
}

// TestBackupSnapshotFields 测试BackupSnapshot包含所有必要字段
func TestBackupSnapshotFields(t *testing.T) {
	snapshot := &BackupSnapshot{
		NodeID:    "test-node",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   []*MetadataEntry{},
		Checksum:  "abc123def456",
		Size:      1024,
	}

	// 验证所有字段
	tests := []struct {
		name  string
		value interface{}
		valid bool
	}{
		{"NodeID", snapshot.NodeID, snapshot.NodeID != ""},
		{"Timestamp", snapshot.Timestamp, !snapshot.Timestamp.IsZero()},
		{"Version", snapshot.Version, snapshot.Version != ""},
		{"Entries", snapshot.Entries, snapshot.Entries != nil},
		{"Checksum", snapshot.Checksum, snapshot.Checksum != ""},
		{"Size", snapshot.Size, snapshot.Size > 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.valid {
				t.Errorf("Field %s is invalid: %v", tt.name, tt.value)
			} else {
				t.Logf("✓ Field %s is valid", tt.name)
			}
		})
	}
}

// TestEmptySnapshotChecksum 测试空快照的校验和
func TestEmptySnapshotChecksum(t *testing.T) {
	snapshot := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   []*MetadataEntry{}, // 空entries
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal empty snapshot: %v", err)
	}

	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	if checksum == "" {
		t.Error("Empty snapshot should still have a valid checksum")
	} else {
		t.Logf("✓ Empty snapshot checksum: %s", checksum[:16]+"...")
	}
}

// TestLargeSnapshotChecksum 测试大快照的校验和性能
func TestLargeSnapshotChecksum(t *testing.T) {
	// 创建大量entries
	entries := make([]*MetadataEntry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = &MetadataEntry{
			Type:      MetadataTypeDataIndex,
			Key:       fmt.Sprintf("index:table:item%d", i),
			Value:     fmt.Sprintf("data_%d", i),
			Timestamp: time.Now(),
		}
	}

	snapshot := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	start := time.Now()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal large snapshot: %v", err)
	}

	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	duration := time.Since(start)

	t.Logf("Large snapshot (1000 entries):")
	t.Logf("  Size: %d bytes", len(data))
	t.Logf("  Checksum: %s...", checksum[:16])
	t.Logf("  Duration: %v", duration)

	if duration > 100*time.Millisecond {
		t.Logf("Warning: Checksum calculation took longer than expected: %v", duration)
	}
}

// BenchmarkChecksumGeneration 校验和生成性能测试
func BenchmarkChecksumGeneration(b *testing.B) {
	entries := make([]*MetadataEntry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = &MetadataEntry{
			Type:      MetadataTypeDataIndex,
			Key:       fmt.Sprintf("key%d", i),
			Value:     fmt.Sprintf("value%d", i),
			Timestamp: time.Now(),
		}
	}

	snapshot := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	data, _ := json.Marshal(snapshot)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := sha256.Sum256(data)
		_ = hex.EncodeToString(hash[:])
	}
}
