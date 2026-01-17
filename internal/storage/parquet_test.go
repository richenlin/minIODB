package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParquetWriterBasic(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "parquet_test_basic")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	config := DefaultParquetWriterConfig(dir)
	config.MaxRowsPerFile = 10

	pw, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer pw.Close()

	records := []map[string]interface{}{
		{"id": "1", "timestamp": time.Now(), "table": "test", "data": map[string]string{"key": "value1"}},
		{"id": "2", "timestamp": time.Now(), "table": "test", "data": map[string]string{"key": "value2"}},
		{"id": "3", "timestamp": time.Now(), "table": "test", "data": map[string]string{"key": "value3"}},
	}

	if err := pw.Write(records); err != nil {
		t.Fatalf("Failed to write records: %v", err)
	}

	if err := pw.Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}

	stats := pw.GetStats()
	if stats.TotalRows != 3 {
		t.Errorf("Expected 3 rows, got %d", stats.TotalRows)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		t.Fatalf("Failed to glob files: %v", err)
	}

	if len(files) == 0 {
		t.Error("Expected at least one parquet file")
	}
}

func TestParquetWriterRotation(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "parquet_test_rotation")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	config := DefaultParquetWriterConfig(dir)
	config.MaxRowsPerFile = 5

	pw, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}
	defer pw.Close()

	for i := 0; i < 15; i++ {
		records := []map[string]interface{}{
			{"id": string(rune('a' + i)), "timestamp": time.Now(), "table": "test", "data": "test"},
		}
		if err := pw.Write(records); err != nil {
			t.Fatalf("Failed to write: %v", err)
		}
	}

	if err := pw.Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}

	stats := pw.GetStats()
	if stats.TotalFiles < 3 {
		t.Errorf("Expected at least 3 files due to rotation, got %d", stats.TotalFiles)
	}
}

func TestParquetReadWrite(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "parquet_test_readwrite")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	config := DefaultParquetWriterConfig(dir)
	pw, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	now := time.Now()
	records := []map[string]interface{}{
		{"id": "rec1", "timestamp": now, "table": "users", "data": `{"name":"Alice"}`},
		{"id": "rec2", "timestamp": now, "table": "users", "data": `{"name":"Bob"}`},
	}

	if err := pw.Write(records); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	if err := pw.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	pr := NewParquetReader(DefaultParquetReaderConfig())

	readRecords, err := pr.ReadDirectory(dir)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(readRecords) != 2 {
		t.Errorf("Expected 2 records, got %d", len(readRecords))
	}

	idFound := make(map[string]bool)
	for _, rec := range readRecords {
		if id, ok := rec["id"].(string); ok {
			idFound[id] = true
		}
	}

	if !idFound["rec1"] || !idFound["rec2"] {
		t.Errorf("Missing expected records: %v", idFound)
	}
}

func TestParquetMetadata(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "parquet_test_metadata")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	config := DefaultParquetWriterConfig(dir)
	pw, err := NewParquetWriter(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	records := []map[string]interface{}{
		{"id": "1", "timestamp": time.Now(), "table": "test", "data": "data1"},
		{"id": "2", "timestamp": time.Now(), "table": "test", "data": "data2"},
	}

	if err := pw.Write(records); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	pw.Close()

	pr := NewParquetReader(DefaultParquetReaderConfig())
	files, _ := pr.ListParquetFiles(dir)

	if len(files) == 0 {
		t.Fatal("No parquet files found")
	}

	metadata, err := pr.GetFileMetadata(files[0])
	if err != nil {
		t.Fatalf("Failed to get metadata: %v", err)
	}

	if metadata.RowCount != 2 {
		t.Errorf("Expected 2 rows in metadata, got %d", metadata.RowCount)
	}
}

func TestCompressionTypes(t *testing.T) {
	compressionTypes := []string{"snappy", "gzip", "none"}

	for _, ct := range compressionTypes {
		t.Run(ct, func(t *testing.T) {
			dir := filepath.Join(os.TempDir(), "parquet_test_"+ct)
			os.RemoveAll(dir)
			defer os.RemoveAll(dir)

			config := DefaultParquetWriterConfig(dir)
			config.CompressionType = ct

			pw, err := NewParquetWriter(config)
			if err != nil {
				t.Fatalf("Failed to create writer with %s: %v", ct, err)
			}

			records := []map[string]interface{}{
				{"id": "1", "timestamp": time.Now(), "table": "test", "data": "test data"},
			}

			if err := pw.Write(records); err != nil {
				t.Fatalf("Failed to write with %s: %v", ct, err)
			}

			if err := pw.Close(); err != nil {
				t.Fatalf("Failed to close with %s: %v", ct, err)
			}

			pr := NewParquetReader(DefaultParquetReaderConfig())
			readRecords, err := pr.ReadDirectory(dir)
			if err != nil {
				t.Fatalf("Failed to read with %s: %v", ct, err)
			}

			if len(readRecords) != 1 {
				t.Errorf("Expected 1 record with %s, got %d", ct, len(readRecords))
			}
		})
	}
}

func BenchmarkParquetWrite(b *testing.B) {
	dir := filepath.Join(os.TempDir(), "parquet_bench_write")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	config := DefaultParquetWriterConfig(dir)
	config.MaxRowsPerFile = 100000

	pw, err := NewParquetWriter(config)
	if err != nil {
		b.Fatalf("Failed to create writer: %v", err)
	}
	defer pw.Close()

	record := map[string]interface{}{
		"id":        "benchmark-id",
		"timestamp": time.Now(),
		"table":     "benchmark",
		"data":      `{"key":"value","number":12345,"nested":{"a":"b"}}`,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pw.Write([]map[string]interface{}{record})
	}
}
