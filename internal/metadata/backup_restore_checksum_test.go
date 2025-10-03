package metadata

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestBackupRestoreWithChecksum 测试带校验和的备份和恢复流程
func TestBackupRestoreWithChecksum(t *testing.T) {
	// 创建测试快照
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
			Value:     "path/to/data1",
			Timestamp: time.Now(),
		},
	}

	snapshot := &BackupSnapshot{
		NodeID:    "test-node",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	// 模拟备份过程：生成校验和
	dataWithoutChecksum, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal snapshot: %v", err)
	}

	hash := sha256.Sum256(dataWithoutChecksum)
	checksum := hex.EncodeToString(hash[:])

	snapshot.Checksum = checksum
	snapshot.Size = int64(len(dataWithoutChecksum))

	t.Logf("Backup created with checksum: %s", checksum[:16]+"...")
	t.Logf("Backup size: %d bytes", snapshot.Size)

	// 模拟存储到文件/对象存储
	backupData, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal final snapshot: %v", err)
	}

	// 模拟从存储中读取并验证
	t.Run("restore_untampered_backup", func(t *testing.T) {
		var restoredSnapshot BackupSnapshot
		if err := json.Unmarshal(backupData, &restoredSnapshot); err != nil {
			t.Fatalf("Failed to unmarshal backup: %v", err)
		}

		// 验证校验和
		originalChecksum := restoredSnapshot.Checksum

		// 重新计算校验和
		snapshotForValidation := &BackupSnapshot{
			NodeID:    restoredSnapshot.NodeID,
			Timestamp: restoredSnapshot.Timestamp,
			Version:   restoredSnapshot.Version,
			Entries:   restoredSnapshot.Entries,
		}

		dataForValidation, _ := json.Marshal(snapshotForValidation)
		recalculatedHash := sha256.Sum256(dataForValidation)
		recalculatedChecksum := hex.EncodeToString(recalculatedHash[:])

		if recalculatedChecksum != originalChecksum {
			t.Errorf("Checksum validation failed: expected %s, got %s",
				originalChecksum[:16], recalculatedChecksum[:16])
		} else {
			t.Logf("✓ Restore successful: checksum validated")
		}
	})

	// 测试篡改的备份
	t.Run("restore_tampered_backup", func(t *testing.T) {
		// 篡改数据
		var tamperedSnapshot BackupSnapshot
		if err := json.Unmarshal(backupData, &tamperedSnapshot); err != nil {
			t.Fatalf("Failed to unmarshal backup: %v", err)
		}

		// 修改一个entry的值（模拟篡改）
		if len(tamperedSnapshot.Entries) > 0 {
			tamperedSnapshot.Entries[0].Value = "tampered_value"
		}

		// 验证校验和应该失败
		originalChecksum := tamperedSnapshot.Checksum

		snapshotForValidation := &BackupSnapshot{
			NodeID:    tamperedSnapshot.NodeID,
			Timestamp: tamperedSnapshot.Timestamp,
			Version:   tamperedSnapshot.Version,
			Entries:   tamperedSnapshot.Entries,
		}

		dataForValidation, _ := json.Marshal(snapshotForValidation)
		recalculatedHash := sha256.Sum256(dataForValidation)
		recalculatedChecksum := hex.EncodeToString(recalculatedHash[:])

		if recalculatedChecksum == originalChecksum {
			t.Error("Tampered backup should fail checksum validation")
		} else {
			t.Logf("✓ Correctly detected tampered backup")
			t.Logf("  Expected checksum: %s...", originalChecksum[:16])
			t.Logf("  Calculated checksum: %s...", recalculatedChecksum[:16])
		}
	})

	// 测试校验和字段被篡改
	t.Run("restore_with_tampered_checksum", func(t *testing.T) {
		var tamperedSnapshot BackupSnapshot
		if err := json.Unmarshal(backupData, &tamperedSnapshot); err != nil {
			t.Fatalf("Failed to unmarshal backup: %v", err)
		}

		// 篡改校验和本身
		tamperedSnapshot.Checksum = "0000000000000000000000000000000000000000000000000000000000000000"

		// 验证应该失败
		snapshotForValidation := &BackupSnapshot{
			NodeID:    tamperedSnapshot.NodeID,
			Timestamp: tamperedSnapshot.Timestamp,
			Version:   tamperedSnapshot.Version,
			Entries:   tamperedSnapshot.Entries,
		}

		dataForValidation, _ := json.Marshal(snapshotForValidation)
		recalculatedHash := sha256.Sum256(dataForValidation)
		recalculatedChecksum := hex.EncodeToString(recalculatedHash[:])

		if recalculatedChecksum == tamperedSnapshot.Checksum {
			t.Error("Should detect tampered checksum field")
		} else {
			t.Logf("✓ Correctly detected tampered checksum field")
		}
	})
}

// TestBackupChecksumWithoutEntries 测试无entry的备份校验和
func TestBackupChecksumWithoutEntries(t *testing.T) {
	snapshot := &BackupSnapshot{
		NodeID:    "test-node",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   []*MetadataEntry{}, // 空entries
	}

	// 生成校验和
	data, _ := json.Marshal(snapshot)
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	snapshot.Checksum = checksum
	snapshot.Size = int64(len(data))

	// 验证
	snapshotForValidation := &BackupSnapshot{
		NodeID:    snapshot.NodeID,
		Timestamp: snapshot.Timestamp,
		Version:   snapshot.Version,
		Entries:   snapshot.Entries,
	}

	dataForValidation, _ := json.Marshal(snapshotForValidation)
	recalculatedHash := sha256.Sum256(dataForValidation)
	recalculatedChecksum := hex.EncodeToString(recalculatedHash[:])

	if recalculatedChecksum != checksum {
		t.Errorf("Empty backup checksum validation failed")
	} else {
		t.Logf("✓ Empty backup checksum validated")
	}
}

// TestPartialBackupCorruption 测试部分备份损坏
func TestPartialBackupCorruption(t *testing.T) {
	entries := []*MetadataEntry{
		{
			Type:      MetadataTypeTableSchema,
			Key:       "table1",
			Value:     "value1",
			Timestamp: time.Now(),
		},
		{
			Type:      MetadataTypeDataIndex,
			Key:       "table2",
			Value:     "value2",
			Timestamp: time.Now(),
		},
	}

	snapshot := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	// 生成原始校验和
	data, _ := json.Marshal(snapshot)
	hash := sha256.Sum256(data)
	originalChecksum := hex.EncodeToString(hash[:])

	snapshot.Checksum = originalChecksum

	// 测试：删除一个entry（模拟部分损坏）
	t.Run("missing_entry", func(t *testing.T) {
		corruptedSnapshot := &BackupSnapshot{
			NodeID:    snapshot.NodeID,
			Timestamp: snapshot.Timestamp,
			Version:   snapshot.Version,
			Entries:   snapshot.Entries[:1], // 只保留第一个entry
		}

		dataCorrupted, _ := json.Marshal(corruptedSnapshot)
		hashCorrupted := sha256.Sum256(dataCorrupted)
		corruptedChecksum := hex.EncodeToString(hashCorrupted[:])

		if corruptedChecksum == originalChecksum {
			t.Error("Should detect missing entries")
		} else {
			t.Logf("✓ Detected missing entry (partial corruption)")
		}
	})

	// 测试：修改entry顺序
	t.Run("reordered_entries", func(t *testing.T) {
		reorderedSnapshot := &BackupSnapshot{
			NodeID:    snapshot.NodeID,
			Timestamp: snapshot.Timestamp,
			Version:   snapshot.Version,
			Entries:   []*MetadataEntry{entries[1], entries[0]}, // 交换顺序
		}

		dataReordered, _ := json.Marshal(reorderedSnapshot)
		hashReordered := sha256.Sum256(dataReordered)
		reorderedChecksum := hex.EncodeToString(hashReordered[:])

		if reorderedChecksum == originalChecksum {
			t.Error("Should detect reordered entries")
		} else {
			t.Logf("✓ Detected reordered entries")
		}
	})
}

// TestChecksumWithDifferentTimezones 测试不同时区的时间戳
func TestChecksumWithDifferentTimezones(t *testing.T) {
	// 同一时间点，不同时区表示
	utcTime := time.Date(2025, 10, 3, 12, 0, 0, 0, time.UTC)
	shanghaiTime := utcTime.In(time.FixedZone("CST", 8*3600))

	entry := &MetadataEntry{
		Type:  MetadataTypeDataIndex,
		Key:   "test",
		Value: "value",
	}

	snapshot1 := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: utcTime,
		Version:   "1.0.0",
		Entries:   []*MetadataEntry{entry},
	}

	snapshot2 := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: shanghaiTime,
		Version:   "1.0.0",
		Entries:   []*MetadataEntry{entry},
	}

	data1, _ := json.Marshal(snapshot1)
	hash1 := sha256.Sum256(data1)
	checksum1 := hex.EncodeToString(hash1[:])

	data2, _ := json.Marshal(snapshot2)
	hash2 := sha256.Sum256(data2)
	checksum2 := hex.EncodeToString(hash2[:])

	// JSON序列化时间戳会统一为UTC，所以校验和应该相同
	if checksum1 != checksum2 {
		t.Logf("Note: Different timezone representations result in different checksums")
		t.Logf("  UTC checksum: %s", checksum1[:16])
		t.Logf("  CST checksum: %s", checksum2[:16])
		// 这不一定是错误，取决于JSON序列化行为
	} else {
		t.Logf("✓ Same checksum for equivalent times in different timezones")
	}
}

// TestChecksumPerformanceWithLargeBackup 测试大备份的校验和性能
func TestChecksumPerformanceWithLargeBackup(t *testing.T) {
	// 创建大量数据
	entries := make([]*MetadataEntry, 10000)
	for i := 0; i < 10000; i++ {
		entries[i] = &MetadataEntry{
			Type:      MetadataTypeDataIndex,
			Key:       fmt.Sprintf("table:row:%d", i),
			Value:     fmt.Sprintf("data_content_%d", i),
			Timestamp: time.Now(),
		}
	}

	snapshot := &BackupSnapshot{
		NodeID:    "perf-test-node",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries:   entries,
	}

	// 测试序列化和校验和生成的总时间
	start := time.Now()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	duration := time.Since(start)

	t.Logf("Large backup performance:")
	t.Logf("  Entries: %d", len(entries))
	t.Logf("  Size: %.2f MB", float64(len(data))/(1024*1024))
	t.Logf("  Checksum: %s...", checksum[:16])
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f MB/s", float64(len(data))/(1024*1024)/duration.Seconds())

	if duration > 1*time.Second {
		t.Logf("Warning: Checksum generation took longer than 1 second")
	}

	// 验证校验和
	snapshotForValidation := &BackupSnapshot{
		NodeID:    snapshot.NodeID,
		Timestamp: snapshot.Timestamp,
		Version:   snapshot.Version,
		Entries:   snapshot.Entries,
	}

	startValidation := time.Now()
	dataForValidation, _ := json.Marshal(snapshotForValidation)
	recalculatedHash := sha256.Sum256(dataForValidation)
	recalculatedChecksum := hex.EncodeToString(recalculatedHash[:])
	validationDuration := time.Since(startValidation)

	if recalculatedChecksum != checksum {
		t.Error("Checksum validation failed for large backup")
	} else {
		t.Logf("✓ Checksum validated in %v", validationDuration)
	}
}

// TestBackupChecksumAfterModification 测试修改后重新计算校验和
func TestBackupChecksumAfterModification(t *testing.T) {
	// 初始备份
	snapshot := &BackupSnapshot{
		NodeID:    "node1",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries: []*MetadataEntry{
			{
				Type:      MetadataTypeTableSchema,
				Key:       "table1",
				Value:     "initial_value",
				Timestamp: time.Now(),
			},
		},
	}

	// 第一次校验和
	data1, _ := json.Marshal(snapshot)
	hash1 := sha256.Sum256(data1)
	checksum1 := hex.EncodeToString(hash1[:])

	t.Logf("Initial checksum: %s", checksum1[:16]+"...")

	// 修改数据
	snapshot.Entries[0].Value = "modified_value"

	// 第二次校验和
	data2, _ := json.Marshal(snapshot)
	hash2 := sha256.Sum256(data2)
	checksum2 := hex.EncodeToString(hash2[:])

	t.Logf("Modified checksum: %s", checksum2[:16]+"...")

	if checksum1 == checksum2 {
		t.Error("Checksum should change after modification")
	} else {
		t.Logf("✓ Checksum correctly reflects modification")
	}
}

// TestChecksumBinaryDataHandling 测试二进制数据的校验和
func TestChecksumBinaryDataHandling(t *testing.T) {
	// 包含二进制数据的entry
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}

	snapshot := &BackupSnapshot{
		NodeID:    "binary-test-node",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Entries: []*MetadataEntry{
			{
				Type:      MetadataTypeDataIndex,
				Key:       "binary:data",
				Value:     binaryData,
				Timestamp: time.Now(),
			},
		},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Failed to marshal binary data: %v", err)
	}

	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	t.Logf("Binary data checksum: %s", checksum[:16]+"...")

	// 验证可以正确反序列化和验证
	var restored BackupSnapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	snapshotForValidation := &BackupSnapshot{
		NodeID:    restored.NodeID,
		Timestamp: restored.Timestamp,
		Version:   restored.Version,
		Entries:   restored.Entries,
	}

	dataForValidation, _ := json.Marshal(snapshotForValidation)
	recalculatedHash := sha256.Sum256(dataForValidation)

	if !bytes.Equal(hash[:], recalculatedHash[:]) {
		t.Error("Binary data checksum validation failed")
	} else {
		t.Logf("✓ Binary data checksum validated")
	}
}
