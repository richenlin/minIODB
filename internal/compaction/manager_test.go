package compaction

import (
	"testing"
	"time"
)

func TestDefaultTieredCompactionConfig(t *testing.T) {
	config := DefaultTieredCompactionConfig()

	if !config.Enabled {
		t.Error("Tiered compaction should be enabled by default")
	}

	if len(config.Levels) != 4 {
		t.Errorf("Expected 4 compaction levels, got %d", len(config.Levels))
	}

	// 验证 L0 配置
	l0 := config.Levels[0]
	if l0.Name != "L0" {
		t.Errorf("Expected L0 name, got %s", l0.Name)
	}
	if l0.MaxFileSize != 16*1024*1024 {
		t.Errorf("L0 MaxFileSize should be 16MB, got %d", l0.MaxFileSize)
	}
	if l0.MinFilesToMerge != 5 {
		t.Errorf("L0 MinFilesToMerge should be 5, got %d", l0.MinFilesToMerge)
	}

	// 验证 L1 配置
	l1 := config.Levels[1]
	if l1.Name != "L1" {
		t.Errorf("Expected L1 name, got %s", l1.Name)
	}
	if l1.MaxFileSize != 64*1024*1024 {
		t.Errorf("L1 MaxFileSize should be 64MB, got %d", l1.MaxFileSize)
	}
	if l1.MinFilesToMerge != 3 {
		t.Errorf("L1 MinFilesToMerge should be 3, got %d", l1.MinFilesToMerge)
	}

	// 验证 L2 配置
	l2 := config.Levels[2]
	if l2.Name != "L2" {
		t.Errorf("Expected L2 name, got %s", l2.Name)
	}
	if l2.MaxFileSize != 256*1024*1024 {
		t.Errorf("L2 MaxFileSize should be 256MB, got %d", l2.MaxFileSize)
	}
	if l2.MinFilesToMerge != 2 {
		t.Errorf("L2 MinFilesToMerge should be 2, got %d", l2.MinFilesToMerge)
	}

	// 验证 L3 配置
	l3 := config.Levels[3]
	if l3.Name != "L3" {
		t.Errorf("Expected L3 name, got %s", l3.Name)
	}
	if l3.MaxFileSize != 1024*1024*1024 {
		t.Errorf("L3 MaxFileSize should be 1GB, got %d", l3.MaxFileSize)
	}
}

func TestGetFileLevel(t *testing.T) {
	config := DefaultTieredCompactionConfig()
	manager := &Manager{
		config: &Config{
			TieredConfig: config,
		},
	}

	tests := []struct {
		name      string
		fileSize  int64
		wantLevel string
	}{
		{"small file 1MB", 1 * 1024 * 1024, "L0"},
		{"L0 boundary 16MB", 16 * 1024 * 1024, "L0"},
		{"L1 file 32MB", 32 * 1024 * 1024, "L1"},
		{"L1 boundary 64MB", 64 * 1024 * 1024, "L1"},
		{"L2 file 128MB", 128 * 1024 * 1024, "L2"},
		{"L2 boundary 256MB", 256 * 1024 * 1024, "L2"},
		{"L3 file 512MB", 512 * 1024 * 1024, "L3"},
		{"L3 boundary 1GB", 1024 * 1024 * 1024, "L3"},
		{"huge file 2GB", 2 * 1024 * 1024 * 1024, "L3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLevel := manager.getFileLevel(tt.fileSize)
			if gotLevel != tt.wantLevel {
				t.Errorf("getFileLevel(%d) = %s, want %s", tt.fileSize, gotLevel, tt.wantLevel)
			}
		})
	}
}

func TestClassifyFilesByTier(t *testing.T) {
	config := DefaultTieredCompactionConfig()
	manager := &Manager{
		config: &Config{
			TieredConfig: config,
		},
	}

	files := []FileInfo{
		{Path: "table/file1.parquet", Size: 1 * 1024 * 1024, ModTime: time.Now()},    // L0
		{Path: "table/file2.parquet", Size: 8 * 1024 * 1024, ModTime: time.Now()},    // L0
		{Path: "table/file3.parquet", Size: 32 * 1024 * 1024, ModTime: time.Now()},   // L1
		{Path: "table/file4.parquet", Size: 128 * 1024 * 1024, ModTime: time.Now()},  // L2
		{Path: "table/file5.parquet", Size: 512 * 1024 * 1024, ModTime: time.Now()},  // L3
		{Path: "table/file6.parquet", Size: 2048 * 1024 * 1024, ModTime: time.Now()}, // L3 (超大文件)
	}

	tieredFiles := manager.classifyFilesByTier(files)

	if len(tieredFiles["L0"]) != 2 {
		t.Errorf("Expected 2 files in L0, got %d", len(tieredFiles["L0"]))
	}
	if len(tieredFiles["L1"]) != 1 {
		t.Errorf("Expected 1 file in L1, got %d", len(tieredFiles["L1"]))
	}
	if len(tieredFiles["L2"]) != 1 {
		t.Errorf("Expected 1 file in L2, got %d", len(tieredFiles["L2"]))
	}
	if len(tieredFiles["L3"]) != 2 {
		t.Errorf("Expected 2 files in L3, got %d", len(tieredFiles["L3"]))
	}
}

func TestSelectTieredCompactionCandidates(t *testing.T) {
	config := DefaultTieredCompactionConfig()
	manager := &Manager{
		config: &Config{
			TieredConfig: config,
		},
	}

	// 创建已过冷却期的文件
	oldTime := time.Now().Add(-10 * time.Minute)
	files := []FileInfo{
		{Path: "table/file1.parquet", Size: 1 * 1024 * 1024, ModTime: oldTime},
		{Path: "table/file2.parquet", Size: 2 * 1024 * 1024, ModTime: oldTime},
		{Path: "table/file3.parquet", Size: 3 * 1024 * 1024, ModTime: oldTime},
		{Path: "table/file4.parquet", Size: 4 * 1024 * 1024, ModTime: oldTime},
		{Path: "table/file5.parquet", Size: 5 * 1024 * 1024, ModTime: oldTime},
		// 新文件，未过冷却期
		{Path: "table/file6.parquet", Size: 1 * 1024 * 1024, ModTime: time.Now()},
	}

	level := config.Levels[0] // L0
	candidates := manager.selectTieredCompactionCandidates(files, level, 0)

	// 应该排除未过冷却期的文件
	if len(candidates) != 5 {
		t.Errorf("Expected 5 candidates (excluding new file), got %d", len(candidates))
	}

	// 验证按大小排序（小文件优先）
	for i := 1; i < len(candidates); i++ {
		if candidates[i].Size < candidates[i-1].Size {
			t.Errorf("Candidates not sorted by size: %d > %d", candidates[i-1].Size, candidates[i].Size)
		}
	}
}

func TestSelectTieredCompactionCandidatesMaxFiles(t *testing.T) {
	config := DefaultTieredCompactionConfig()
	manager := &Manager{
		config: &Config{
			TieredConfig: config,
		},
	}

	// 创建超过 MaxFilesToMerge 的文件
	oldTime := time.Now().Add(-10 * time.Minute)
	files := make([]FileInfo, 30)
	for i := 0; i < 30; i++ {
		files[i] = FileInfo{
			Path:    "table/file.parquet",
			Size:    int64(i+1) * 1024 * 1024,
			ModTime: oldTime,
		}
	}

	level := config.Levels[0] // L0
	candidates := manager.selectTieredCompactionCandidates(files, level, 0)

	// 应该限制在 MaxFilesToMerge
	if len(candidates) > config.MaxFilesToMerge {
		t.Errorf("Expected at most %d candidates, got %d", config.MaxFilesToMerge, len(candidates))
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.TieredConfig == nil {
		t.Error("DefaultConfig should have TieredConfig")
	}

	if !config.TieredConfig.Enabled {
		t.Error("TieredConfig should be enabled by default")
	}

	if config.CheckInterval != 10*time.Minute {
		t.Errorf("CheckInterval should be 10 minutes, got %v", config.CheckInterval)
	}
}

func TestTieredCompactionConfigLevelsOrder(t *testing.T) {
	config := DefaultTieredCompactionConfig()

	// 验证层级顺序和大小递增
	for i := 1; i < len(config.Levels); i++ {
		if config.Levels[i].MaxFileSize <= config.Levels[i-1].MaxFileSize {
			t.Errorf("Level %s MaxFileSize (%d) should be greater than level %s (%d)",
				config.Levels[i].Name, config.Levels[i].MaxFileSize,
				config.Levels[i-1].Name, config.Levels[i-1].MaxFileSize)
		}
	}

	// 验证 MinFilesToMerge 递减（高层需要更少文件触发合并）
	for i := 1; i < len(config.Levels); i++ {
		if config.Levels[i].MinFilesToMerge > config.Levels[i-1].MinFilesToMerge {
			t.Errorf("Level %s MinFilesToMerge (%d) should not be greater than level %s (%d)",
				config.Levels[i].Name, config.Levels[i].MinFilesToMerge,
				config.Levels[i-1].Name, config.Levels[i-1].MinFilesToMerge)
		}
	}
}
