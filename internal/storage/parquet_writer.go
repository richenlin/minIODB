package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minIODB/pkg/logger"

	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
	"go.uber.org/zap"
)

type GenericRecord struct {
	ID        string `parquet:"name=id, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN"`
	Timestamp int64  `parquet:"name=timestamp, type=INT64"`
	Data      string `parquet:"name=data, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN"`
	Table     string `parquet:"name=table, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN"`
}

type ParquetWriterConfig struct {
	OutputDir       string
	RowGroupSize    int64
	PageSize        int64
	CompressionType string
	MaxRowsPerFile  int
}

func DefaultParquetWriterConfig(outputDir string) *ParquetWriterConfig {
	return &ParquetWriterConfig{
		OutputDir:       outputDir,
		RowGroupSize:    128 * 1024 * 1024,
		PageSize:        8 * 1024,
		CompressionType: "snappy",
		MaxRowsPerFile:  100000,
	}
}

type ParquetWriterImpl struct {
	config      *ParquetWriterConfig
	mu          sync.Mutex
	currentFile string
	pw          *writer.ParquetWriter
	fw          interface{ Close() error }
	rowCount    int
	totalRows   int64
	totalFiles  int
	stats       *ParquetWriteStats
}

type ParquetWriteStats struct {
	TotalRows       int64
	TotalFiles      int
	TotalBytes      int64
	AvgRowsPerFile  float64
	LastWriteTime   time.Time
	CompressionType string
}

func NewParquetWriter(config *ParquetWriterConfig) (*ParquetWriterImpl, error) {
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	return &ParquetWriterImpl{
		config: config,
		stats: &ParquetWriteStats{
			CompressionType: config.CompressionType,
		},
	}, nil
}

func (p *ParquetWriterImpl) openNewFile() error {
	filename := fmt.Sprintf("data_%d_%d.parquet", time.Now().UnixNano(), p.totalFiles)
	filepath := filepath.Join(p.config.OutputDir, filename)

	fw, err := local.NewLocalFileWriter(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file writer: %w", err)
	}

	pw, err := writer.NewParquetWriter(fw, new(GenericRecord), 4)
	if err != nil {
		fw.Close()
		return fmt.Errorf("failed to create parquet writer: %w", err)
	}

	pw.RowGroupSize = p.config.RowGroupSize
	pw.PageSize = p.config.PageSize
	pw.CompressionType = p.getCompressionCodec()

	p.fw = fw
	p.pw = pw
	p.currentFile = filepath
	p.rowCount = 0
	p.totalFiles++

	logger.GetLogger().Debug("Opened new Parquet file",
		zap.String("file", filepath),
		zap.String("compression", p.config.CompressionType))

	return nil
}

func (p *ParquetWriterImpl) getCompressionCodec() parquet.CompressionCodec {
	switch p.config.CompressionType {
	case "snappy":
		return parquet.CompressionCodec_SNAPPY
	case "gzip":
		return parquet.CompressionCodec_GZIP
	case "lz4":
		return parquet.CompressionCodec_LZ4
	case "zstd":
		return parquet.CompressionCodec_ZSTD
	case "none", "uncompressed":
		return parquet.CompressionCodec_UNCOMPRESSED
	default:
		return parquet.CompressionCodec_SNAPPY
	}
}

func (p *ParquetWriterImpl) Write(records []map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pw == nil {
		if err := p.openNewFile(); err != nil {
			return err
		}
	}

	for _, record := range records {
		genericRecord := p.convertToGenericRecord(record)

		if err := p.pw.Write(genericRecord); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}

		p.rowCount++
		p.totalRows++

		if p.rowCount >= p.config.MaxRowsPerFile {
			if err := p.rotateFile(); err != nil {
				return err
			}
		}
	}

	p.stats.TotalRows = p.totalRows
	p.stats.TotalFiles = p.totalFiles
	p.stats.LastWriteTime = time.Now()

	return nil
}

func (p *ParquetWriterImpl) convertToGenericRecord(record map[string]interface{}) *GenericRecord {
	gr := &GenericRecord{}

	if id, ok := record["id"].(string); ok {
		gr.ID = id
	}

	if ts, ok := record["timestamp"].(time.Time); ok {
		gr.Timestamp = ts.UnixNano()
	} else if tsInt, ok := record["timestamp"].(int64); ok {
		gr.Timestamp = tsInt
	} else {
		gr.Timestamp = time.Now().UnixNano()
	}

	if table, ok := record["table"].(string); ok {
		gr.Table = table
	}

	if data, ok := record["data"]; ok {
		switch v := data.(type) {
		case []byte:
			gr.Data = string(v)
		case string:
			gr.Data = v
		default:
			if jsonData, err := json.Marshal(v); err == nil {
				gr.Data = string(jsonData)
			}
		}
	} else {
		if jsonData, err := json.Marshal(record); err == nil {
			gr.Data = string(jsonData)
		}
	}

	return gr
}

func (p *ParquetWriterImpl) rotateFile() error {
	if err := p.closeCurrentFile(); err != nil {
		return err
	}
	return p.openNewFile()
}

func (p *ParquetWriterImpl) closeCurrentFile() error {
	if p.pw != nil {
		if err := p.pw.WriteStop(); err != nil {
			return fmt.Errorf("failed to stop parquet writer: %w", err)
		}
		p.pw = nil
	}

	if p.fw != nil {
		if err := p.fw.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}

		if info, err := os.Stat(p.currentFile); err == nil {
			p.stats.TotalBytes += info.Size()
		}

		p.fw = nil
	}

	if p.totalFiles > 0 {
		p.stats.AvgRowsPerFile = float64(p.totalRows) / float64(p.totalFiles)
	}

	logger.GetLogger().Debug("Closed Parquet file",
		zap.String("file", p.currentFile),
		zap.Int("rows", p.rowCount))

	return nil
}

func (p *ParquetWriterImpl) Flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.closeCurrentFile()
}

func (p *ParquetWriterImpl) Close() error {
	return p.Flush()
}

func (p *ParquetWriterImpl) GetStats() *ParquetWriteStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	statsCopy := *p.stats
	return &statsCopy
}

func (p *ParquetWriterImpl) CurrentFile() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentFile
}
