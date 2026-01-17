package wal

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minIODB/pkg/logger"

	"go.uber.org/zap"
)

const (
	RecordTypeData   byte = 1
	RecordTypeCommit byte = 2
	RecordTypeDelete byte = 3

	HeaderSize    = 13
	MaxRecordSize = 64 * 1024 * 1024

	walFilePrefix  = "wal_"
	walFileSuffix  = ".log"
	maxWALFileSize = 64 * 1024 * 1024
)

type Record struct {
	SeqNum    uint64
	Type      byte
	Timestamp int64
	Data      []byte
}

type WAL struct {
	dir          string
	file         *os.File
	writer       *bufio.Writer
	mu           sync.Mutex
	seqNum       uint64
	currentSize  int64
	syncOnWrite  bool
	maxFileSize  int64
	rotateCount  int
	lastSyncTime time.Time
	syncInterval time.Duration
	closed       bool
}

type Config struct {
	Dir          string
	SyncOnWrite  bool
	MaxFileSize  int64
	SyncInterval time.Duration
}

func DefaultConfig(dir string) *Config {
	return &Config{
		Dir:          dir,
		SyncOnWrite:  true,
		MaxFileSize:  maxWALFileSize,
		SyncInterval: 100 * time.Millisecond,
	}
}

func New(cfg *Config) (*WAL, error) {
	if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	w := &WAL{
		dir:          cfg.Dir,
		syncOnWrite:  cfg.SyncOnWrite,
		maxFileSize:  cfg.MaxFileSize,
		syncInterval: cfg.SyncInterval,
		lastSyncTime: time.Now(),
	}

	lastSeq, err := w.findLastSequence()
	if err != nil {
		return nil, fmt.Errorf("failed to find last sequence: %w", err)
	}
	w.seqNum = lastSeq

	if err := w.openNewFile(); err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	return w, nil
}

func (w *WAL) openNewFile() error {
	filename := filepath.Join(w.dir, fmt.Sprintf("%s%d%s", walFilePrefix, time.Now().UnixNano(), walFileSuffix))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	if w.file != nil {
		w.writer.Flush()
		w.file.Close()
	}

	w.file = f
	w.writer = bufio.NewWriterSize(f, 64*1024)
	w.currentSize = 0
	w.rotateCount++

	logger.GetLogger().Info("Opened new WAL file", zap.String("file", filename))
	return nil
}

func (w *WAL) Append(recordType byte, data []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("WAL is closed")
	}

	if len(data) > MaxRecordSize {
		return 0, fmt.Errorf("record too large: %d > %d", len(data), MaxRecordSize)
	}

	w.seqNum++
	record := &Record{
		SeqNum:    w.seqNum,
		Type:      recordType,
		Timestamp: time.Now().UnixNano(),
		Data:      data,
	}

	if err := w.writeRecord(record); err != nil {
		w.seqNum--
		return 0, err
	}

	if w.syncOnWrite {
		if err := w.sync(); err != nil {
			return 0, err
		}
	} else if time.Since(w.lastSyncTime) > w.syncInterval {
		w.sync()
	}

	if w.currentSize >= w.maxFileSize {
		if err := w.openNewFile(); err != nil {
			logger.GetLogger().Error("Failed to rotate WAL file", zap.Error(err))
		}
	}

	return w.seqNum, nil
}

func (w *WAL) writeRecord(record *Record) error {
	header := make([]byte, HeaderSize)

	binary.BigEndian.PutUint32(header[0:4], uint32(len(record.Data)))
	header[4] = record.Type
	binary.BigEndian.PutUint64(header[5:13], record.SeqNum)

	crc := crc32.ChecksumIEEE(record.Data)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)

	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, uint64(record.Timestamp))

	if _, err := w.writer.Write(header); err != nil {
		return err
	}
	if _, err := w.writer.Write(crcBytes); err != nil {
		return err
	}
	if _, err := w.writer.Write(timestampBytes); err != nil {
		return err
	}
	if _, err := w.writer.Write(record.Data); err != nil {
		return err
	}

	w.currentSize += int64(HeaderSize + 4 + 8 + len(record.Data))
	return nil
}

func (w *WAL) sync() error {
	if err := w.writer.Flush(); err != nil {
		return err
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	w.lastSyncTime = time.Now()
	return nil
}

func (w *WAL) Replay(handler func(record *Record) error) error {
	files, err := w.listWALFiles()
	if err != nil {
		return err
	}

	for _, filename := range files {
		if err := w.replayFile(filename, handler); err != nil {
			logger.GetLogger().Warn("Error replaying WAL file",
				zap.String("file", filename),
				zap.Error(err))
		}
	}

	return nil
}

func (w *WAL) replayFile(filename string, handler func(record *Record) error) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	recordCount := 0

	for {
		record, err := w.readRecord(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.GetLogger().Warn("Error reading WAL record",
				zap.String("file", filename),
				zap.Int("record", recordCount),
				zap.Error(err))
			break
		}

		if err := handler(record); err != nil {
			return fmt.Errorf("handler error at record %d: %w", record.SeqNum, err)
		}

		recordCount++
	}

	logger.GetLogger().Info("Replayed WAL file",
		zap.String("file", filename),
		zap.Int("records", recordCount))

	return nil
}

func (w *WAL) readRecord(reader *bufio.Reader) (*Record, error) {
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}

	dataLen := binary.BigEndian.Uint32(header[0:4])
	recordType := header[4]
	seqNum := binary.BigEndian.Uint64(header[5:13])

	crcBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, crcBytes); err != nil {
		return nil, err
	}
	expectedCRC := binary.BigEndian.Uint32(crcBytes)

	timestampBytes := make([]byte, 8)
	if _, err := io.ReadFull(reader, timestampBytes); err != nil {
		return nil, err
	}
	timestamp := int64(binary.BigEndian.Uint64(timestampBytes))

	data := make([]byte, dataLen)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}

	actualCRC := crc32.ChecksumIEEE(data)
	if actualCRC != expectedCRC {
		return nil, fmt.Errorf("CRC mismatch: expected %d, got %d", expectedCRC, actualCRC)
	}

	return &Record{
		SeqNum:    seqNum,
		Type:      recordType,
		Timestamp: timestamp,
		Data:      data,
	}, nil
}

func (w *WAL) Truncate(beforeSeqNum uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	files, err := w.listWALFiles()
	if err != nil {
		return err
	}

	for _, filename := range files {
		maxSeq, err := w.getFileMaxSequence(filename)
		if err != nil {
			continue
		}

		if maxSeq < beforeSeqNum {
			if err := os.Remove(filename); err != nil {
				logger.GetLogger().Warn("Failed to remove old WAL file",
					zap.String("file", filename),
					zap.Error(err))
			} else {
				logger.GetLogger().Info("Removed old WAL file",
					zap.String("file", filename),
					zap.Uint64("maxSeq", maxSeq))
			}
		}
	}

	return nil
}

func (w *WAL) getFileMaxSequence(filename string) (uint64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var maxSeq uint64

	for {
		record, err := w.readRecord(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if record.SeqNum > maxSeq {
			maxSeq = record.SeqNum
		}
	}

	return maxSeq, nil
}

func (w *WAL) listWALFiles() ([]string, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == walFileSuffix {
			files = append(files, filepath.Join(w.dir, entry.Name()))
		}
	}

	return files, nil
}

func (w *WAL) findLastSequence() (uint64, error) {
	files, err := w.listWALFiles()
	if err != nil {
		return 0, err
	}

	var maxSeq uint64
	for _, filename := range files {
		seq, err := w.getFileMaxSequence(filename)
		if err != nil {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}

	return maxSeq, nil
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	if w.writer != nil {
		w.writer.Flush()
	}
	if w.file != nil {
		w.file.Sync()
		w.file.Close()
	}

	logger.GetLogger().Info("WAL closed", zap.Uint64("lastSeqNum", w.seqNum))
	return nil
}

func (w *WAL) LastSeqNum() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seqNum
}

func (w *WAL) Dir() string {
	return w.dir
}
