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

	"go.uber.org/zap"
)

const (
	RecordTypeData      byte = 1
	RecordTypeCommit    byte = 2
	RecordTypeDelete    byte = 3
	RecordTypeTombstone byte = 4

	HeaderSize    = 13
	MaxRecordSize = 64 * 1024 * 1024

	walFilePrefix  = "wal_"
	walFileSuffix  = ".log"
	maxWALFileSize = 64 * 1024 * 1024

	// File header constants
	MagicNumber     = "MWAL"
	MagicNumberSize = 4
	CurrentVersion  = 1
	VersionSize     = 2
	FileHeaderSize  = MagicNumberSize + VersionSize // 6 bytes
)

// walBufferPool 用于复用 WAL 写入时的临时 buffer
// 大小为 HeaderSize(13) + Timestamp(8) + 预留空间，通常足够大多数记录
const walBufferInitialSize = 256

var walBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, walBufferInitialSize)
		return &buf
	},
}

// getWALBuffer 从池中获取 buffer
func getWALBuffer() *[]byte {
	return walBufferPool.Get().(*[]byte)
}

// putWALBuffer 将 buffer 放回池中
func putWALBuffer(buf *[]byte) {
	*buf = (*buf)[:0] // 重置长度但保留容量
	// 如果 buffer 增长过大（超过 64KB），不放回池中，让 GC 回收
	if cap(*buf) > 64*1024 {
		return
	}
	walBufferPool.Put(buf)
}

// headerPool 用于复用 header buffer (13 bytes)
var headerPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, HeaderSize)
		return &buf
	},
}

// getHeaderBuffer 从池中获取 header buffer
func getHeaderBuffer() *[]byte {
	return headerPool.Get().(*[]byte)
}

// putHeaderBuffer 将 header buffer 放回池中
func putHeaderBuffer(buf *[]byte) {
	headerPool.Put(buf)
}

// timestampPool 用于复用 timestamp buffer (8 bytes)
var timestampPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 8)
		return &buf
	},
}

// getTimestampBuffer 从池中获取 timestamp buffer
func getTimestampBuffer() *[]byte {
	return timestampPool.Get().(*[]byte)
}

// putTimestampBuffer 将 timestamp buffer 放回池中
func putTimestampBuffer(buf *[]byte) {
	timestampPool.Put(buf)
}

// crcPool 用于复用 CRC buffer (4 bytes)
var crcPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 4)
		return &buf
	},
}

// getCRCBuffer 从池中获取 CRC buffer
func getCRCBuffer() *[]byte {
	return crcPool.Get().(*[]byte)
}

// putCRCBuffer 将 CRC buffer 放回池中
func putCRCBuffer(buf *[]byte) {
	crcPool.Put(buf)
}

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
	logger       *zap.Logger
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

func New(cfg *Config, logger *zap.Logger) (*WAL, error) {
	if err := os.MkdirAll(cfg.Dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	w := &WAL{
		dir:          cfg.Dir,
		syncOnWrite:  cfg.SyncOnWrite,
		maxFileSize:  cfg.MaxFileSize,
		syncInterval: cfg.SyncInterval,
		lastSyncTime: time.Now(),
		logger:       logger,
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

	// Write file header: Magic Number + Version
	fileHeader := make([]byte, FileHeaderSize)
	copy(fileHeader[0:MagicNumberSize], MagicNumber)
	binary.BigEndian.PutUint16(fileHeader[MagicNumberSize:FileHeaderSize], CurrentVersion)
	if _, err := f.Write(fileHeader); err != nil {
		f.Close()
		return fmt.Errorf("failed to write WAL file header: %w", err)
	}

	if w.file != nil {
		w.writer.Flush()
		w.file.Close()
	}

	w.file = f
	w.writer = bufio.NewWriterSize(f, 64*1024)
	w.currentSize = int64(FileHeaderSize)
	w.rotateCount++

	w.logger.Info("Opened new WAL file", zap.String("file", filename))
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
			w.logger.Error("Failed to rotate WAL file", zap.Error(err))
		}
	}

	return w.seqNum, nil
}

func (w *WAL) writeRecord(record *Record) error {
	// 从池中获取 header buffer
	headerBuf := getHeaderBuffer()
	defer putHeaderBuffer(headerBuf)
	header := *headerBuf

	binary.BigEndian.PutUint32(header[0:4], uint32(len(record.Data)))
	header[4] = record.Type
	binary.BigEndian.PutUint64(header[5:13], record.SeqNum)

	// 从池中获取 timestamp buffer
	timestampBuf := getTimestampBuffer()
	defer putTimestampBuffer(timestampBuf)
	timestampBytes := *timestampBuf
	binary.BigEndian.PutUint64(timestampBytes, uint64(record.Timestamp))

	// 从池中获取 CRC 计算用的 buffer
	crcDataBuf := getWALBuffer()
	defer putWALBuffer(crcDataBuf)
	crcData := *crcDataBuf

	// 确保容量足够
	neededSize := HeaderSize + 8 + len(record.Data)
	if cap(crcData) < neededSize {
		crcData = make([]byte, 0, neededSize)
	}
	crcData = append(crcData[:0], header...)
	crcData = append(crcData, timestampBytes...)
	crcData = append(crcData, record.Data...)
	crc := crc32.ChecksumIEEE(crcData)

	// 从池中获取 CRC buffer
	crcBuf := getCRCBuffer()
	defer putCRCBuffer(crcBuf)
	crcBytes := *crcBuf
	binary.BigEndian.PutUint32(crcBytes, crc)

	// Write order: Header -> CRC -> Timestamp -> Data
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
			w.logger.Warn("Error replaying WAL file",
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

	// Try to read file header
	hasFileHeader, version, err := w.readFileHeader(reader)
	if err != nil {
		return fmt.Errorf("failed to read file header: %w", err)
	}

	// If no file header (old format), reset reader to beginning
	if !hasFileHeader {
		if _, err := f.Seek(0, 0); err != nil {
			return fmt.Errorf("failed to seek WAL file: %w", err)
		}
		reader = bufio.NewReader(f)
		w.logger.Info("Replaying old format WAL file (no file header)",
			zap.String("file", filename))
	} else {
		w.logger.Debug("Replaying WAL file with header",
			zap.String("file", filename),
			zap.Uint16("version", version))
	}

	recordCount := 0

	for {
		record, err := w.readRecordWithFormat(reader, hasFileHeader)
		if err == io.EOF {
			break
		}
		if err != nil {
			w.logger.Warn("Error reading WAL record",
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

	w.logger.Info("Replayed WAL file",
		zap.String("file", filename),
		zap.Int("records", recordCount))

	return nil
}

// readFileHeader attempts to read and validate the file header.
// Returns (hasHeader, version, error). If no header is found (old format),
// returns (false, 0, nil).
func (w *WAL) readFileHeader(reader *bufio.Reader) (bool, uint16, error) {
	// Peek at the first 4 bytes to check for magic number
	magicBytes, err := reader.Peek(MagicNumberSize)
	if err != nil {
		if err == io.EOF {
			// Empty file or very small file, no header
			return false, 0, nil
		}
		return false, 0, err
	}

	// Check if magic number matches
	if string(magicBytes) != MagicNumber {
		// Old format file, no file header
		return false, 0, nil
	}

	// Read and discard magic number
	if _, err := reader.Discard(MagicNumberSize); err != nil {
		return false, 0, err
	}

	// Read version
	versionBytes := make([]byte, VersionSize)
	if _, err := io.ReadFull(reader, versionBytes); err != nil {
		return false, 0, fmt.Errorf("failed to read version: %w", err)
	}
	version := binary.BigEndian.Uint16(versionBytes)

	// Check version compatibility
	if version > CurrentVersion {
		return false, 0, fmt.Errorf("unsupported WAL file version: %d (current: %d)", version, CurrentVersion)
	}

	return true, version, nil
}

func (w *WAL) readRecord(reader *bufio.Reader) (*Record, error) {
	return w.readRecordWithFormat(reader, true)
}

func (w *WAL) readRecordWithFormat(reader *bufio.Reader, newCRCFormat bool) (*Record, error) {
	// 从池中获取 header buffer
	headerBuf := getHeaderBuffer()
	defer putHeaderBuffer(headerBuf)
	header := *headerBuf

	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}

	dataLen := binary.BigEndian.Uint32(header[0:4])
	if dataLen > MaxRecordSize {
		return nil, fmt.Errorf("WAL record size %d exceeds maximum %d", dataLen, MaxRecordSize)
	}
	recordType := header[4]
	seqNum := binary.BigEndian.Uint64(header[5:13])

	// 从池中获取 CRC buffer
	crcBuf := getCRCBuffer()
	defer putCRCBuffer(crcBuf)
	crcBytes := *crcBuf

	if _, err := io.ReadFull(reader, crcBytes); err != nil {
		return nil, err
	}
	expectedCRC := binary.BigEndian.Uint32(crcBytes)

	// 从池中获取 timestamp buffer
	timestampBuf := getTimestampBuffer()
	defer putTimestampBuffer(timestampBuf)
	timestampBytes := *timestampBuf

	if _, err := io.ReadFull(reader, timestampBytes); err != nil {
		return nil, err
	}
	timestamp := int64(binary.BigEndian.Uint64(timestampBytes))

	// data 需要返回给调用者，不能使用 pool
	data := make([]byte, dataLen)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}

	// Verify CRC
	var actualCRC uint32
	if newCRCFormat {
		// New format: CRC covers Header + Timestamp + Data
		// 从池中获取 CRC 计算用的 buffer
		crcDataBuf := getWALBuffer()
		defer putWALBuffer(crcDataBuf)
		crcData := *crcDataBuf

		neededSize := HeaderSize + 8 + len(data)
		if cap(crcData) < neededSize {
			crcData = make([]byte, 0, neededSize)
		}
		crcData = append(crcData[:0], header...)
		crcData = append(crcData, timestampBytes...)
		crcData = append(crcData, data...)
		actualCRC = crc32.ChecksumIEEE(crcData)
	} else {
		// Old format: CRC only covers Data
		actualCRC = crc32.ChecksumIEEE(data)
	}

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

// Truncate deletes all closed WAL files whose maximum sequence number is ≤ beforeSeqNum.
// This is safe because all records up to beforeSeqNum have been durably flushed
// to the backing store and no longer need WAL protection.
func (w *WAL) Truncate(beforeSeqNum uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	files, err := w.listWALFiles()
	if err != nil {
		return err
	}

	// Skip the currently-open file — it may still be receiving new appends.
	currentFile := ""
	if w.file != nil {
		currentFile = w.file.Name()
	}

	for _, filename := range files {
		if filename == currentFile {
			continue
		}

		maxSeq, err := w.getFileMaxSequence(filename)
		if err != nil {
			continue
		}

		// Use <= so a file whose last record exactly equals beforeSeqNum is also removed.
		if maxSeq <= beforeSeqNum {
			if err := os.Remove(filename); err != nil {
				w.logger.Warn("Failed to remove old WAL file",
					zap.String("file", filename),
					zap.Uint64("maxSeq", maxSeq))
			} else {
				w.logger.Info("Removed old WAL file",
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

	// Try to read file header
	hasFileHeader, _, err := w.readFileHeader(reader)
	if err != nil {
		return 0, err
	}

	// If no file header (old format), reset reader to beginning
	if !hasFileHeader {
		if _, err := f.Seek(0, 0); err != nil {
			return 0, fmt.Errorf("failed to seek WAL file: %w", err)
		}
		reader = bufio.NewReader(f)
	}

	var maxSeq uint64

	for {
		record, err := w.readRecordWithFormat(reader, hasFileHeader)
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

	w.logger.Info("WAL closed", zap.Uint64("lastSeqNum", w.seqNum))
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
