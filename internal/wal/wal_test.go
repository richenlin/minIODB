package wal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWALBasic(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "wal_test_basic")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	w, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	testData := []byte(`{"id":"test-1","name":"Test Record"}`)
	seq, err := w.Append(RecordTypeData, testData)
	if err != nil {
		t.Fatalf("Failed to append record: %v", err)
	}

	if seq != 1 {
		t.Errorf("Expected sequence 1, got %d", seq)
	}

	seq2, err := w.Append(RecordTypeData, []byte(`{"id":"test-2"}`))
	if err != nil {
		t.Fatalf("Failed to append second record: %v", err)
	}

	if seq2 != 2 {
		t.Errorf("Expected sequence 2, got %d", seq2)
	}
}

func TestWALReplay(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "wal_test_replay")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	w, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	records := [][]byte{
		[]byte(`{"id":"1","data":"first"}`),
		[]byte(`{"id":"2","data":"second"}`),
		[]byte(`{"id":"3","data":"third"}`),
	}

	for _, data := range records {
		if _, err := w.Append(RecordTypeData, data); err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	w.Close()

	w2, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	var replayed []*Record
	err = w2.Replay(func(record *Record) error {
		replayed = append(replayed, record)
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if len(replayed) != len(records) {
		t.Errorf("Expected %d records, got %d", len(records), len(replayed))
	}

	for i, record := range replayed {
		if string(record.Data) != string(records[i]) {
			t.Errorf("Record %d mismatch: expected %s, got %s", i, records[i], record.Data)
		}
	}
}

func TestWALTruncate(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "wal_test_truncate")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	cfg.MaxFileSize = 1024
	w, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	largeData := make([]byte, 500)
	for i := range largeData {
		largeData[i] = 'A'
	}

	for i := 0; i < 10; i++ {
		if _, err := w.Append(RecordTypeData, largeData); err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	w.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	if len(files) < 2 {
		t.Log("Expected multiple WAL files due to rotation")
	}

	w2, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	err = w2.Truncate(5)
	if err != nil {
		t.Fatalf("Failed to truncate: %v", err)
	}
}

func TestWALCRCValidation(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "wal_test_crc")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	w, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	testData := []byte(`{"important":"data"}`)
	_, err = w.Append(RecordTypeData, testData)
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	w.Close()

	w2, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	var replayed []*Record
	err = w2.Replay(func(record *Record) error {
		replayed = append(replayed, record)
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if len(replayed) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(replayed))
	}

	if string(replayed[0].Data) != string(testData) {
		t.Errorf("Data mismatch after replay")
	}
}

func TestWALSequenceContinuity(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "wal_test_seq")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	w, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	for i := 0; i < 5; i++ {
		w.Append(RecordTypeData, []byte(`test`))
	}

	lastSeq := w.LastSeqNum()
	w.Close()

	w2, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	newSeq, err := w2.Append(RecordTypeData, []byte(`new record`))
	if err != nil {
		t.Fatalf("Failed to append after reopen: %v", err)
	}

	if newSeq != lastSeq+1 {
		t.Errorf("Expected sequence %d, got %d", lastSeq+1, newSeq)
	}
}

func BenchmarkWALAppend(b *testing.B) {
	dir := filepath.Join(os.TempDir(), "wal_bench")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	cfg.SyncOnWrite = false
	w, err := New(cfg)
	if err != nil {
		b.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	data := []byte(`{"id":"bench","timestamp":1234567890,"payload":{"key":"value"}}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Append(RecordTypeData, data)
	}
}

func BenchmarkWALAppendSync(b *testing.B) {
	dir := filepath.Join(os.TempDir(), "wal_bench_sync")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	cfg := DefaultConfig(dir)
	cfg.SyncOnWrite = true
	w, err := New(cfg)
	if err != nil {
		b.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	data := []byte(`{"id":"bench","timestamp":1234567890,"payload":{"key":"value"}}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Append(RecordTypeData, data)
	}
}
