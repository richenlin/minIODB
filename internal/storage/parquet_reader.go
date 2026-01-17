package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"minIODB/pkg/logger"

	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/reader"
	"go.uber.org/zap"
)

type ParquetReaderConfig struct {
	ParallelNumber int64
}

func DefaultParquetReaderConfig() *ParquetReaderConfig {
	return &ParquetReaderConfig{
		ParallelNumber: 4,
	}
}

type ParquetReaderImpl struct {
	config *ParquetReaderConfig
	mu     sync.Mutex
}

type ParquetReadStats struct {
	TotalRows  int64
	TotalFiles int
	ReadErrors int
}

func NewParquetReader(config *ParquetReaderConfig) *ParquetReaderImpl {
	if config == nil {
		config = DefaultParquetReaderConfig()
	}
	return &ParquetReaderImpl{
		config: config,
	}
}

func (p *ParquetReaderImpl) ReadFile(filePath string) ([]map[string]interface{}, error) {
	fr, err := local.NewLocalFileReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer fr.Close()

	pr, err := reader.NewParquetReader(fr, new(GenericRecord), p.config.ParallelNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to create parquet reader: %w", err)
	}
	defer pr.ReadStop()

	numRows := int(pr.GetNumRows())
	records := make([]*GenericRecord, numRows)

	if err := pr.Read(&records); err != nil {
		return nil, fmt.Errorf("failed to read records: %w", err)
	}

	result := make([]map[string]interface{}, 0, numRows)
	for _, rec := range records {
		if rec == nil {
			continue
		}

		m := map[string]interface{}{
			"id":        rec.ID,
			"timestamp": rec.Timestamp,
			"table":     rec.Table,
		}

		if rec.Data != "" {
			var data interface{}
			if err := json.Unmarshal([]byte(rec.Data), &data); err == nil {
				m["data"] = data
			} else {
				m["data"] = rec.Data
			}
		}

		result = append(result, m)
	}

	logger.GetLogger().Debug("Read Parquet file",
		zap.String("file", filePath),
		zap.Int("rows", len(result)))

	return result, nil
}

func (p *ParquetReaderImpl) ReadDirectory(dirPath string) ([]map[string]interface{}, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var allRecords []map[string]interface{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if filepath.Ext(entry.Name()) != ".parquet" {
			continue
		}

		filePath := filepath.Join(dirPath, entry.Name())
		records, err := p.ReadFile(filePath)
		if err != nil {
			logger.GetLogger().Warn("Failed to read Parquet file",
				zap.String("file", filePath),
				zap.Error(err))
			continue
		}

		allRecords = append(allRecords, records...)
	}

	return allRecords, nil
}

func (p *ParquetReaderImpl) GetFileMetadata(filePath string) (*FileMetadata, error) {
	fr, err := local.NewLocalFileReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer fr.Close()

	pr, err := reader.NewParquetReader(fr, new(GenericRecord), 1)
	if err != nil {
		return nil, fmt.Errorf("failed to create parquet reader: %w", err)
	}
	defer pr.ReadStop()

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	metadata := &FileMetadata{
		FilePath:      filePath,
		FileSize:      info.Size(),
		RowCount:      pr.GetNumRows(),
		RowGroupCount: len(pr.Footer.RowGroups),
		CreatedAt:     info.ModTime(),
		MinValues:     make(map[string]interface{}),
		MaxValues:     make(map[string]interface{}),
		NullCounts:    make(map[string]int64),
		BloomFilters:  make(map[string]bool),
		Partitions:    make(map[string]string),
	}

	if len(pr.Footer.RowGroups) > 0 {
		for _, col := range pr.Footer.RowGroups[0].Columns {
			if col.MetaData != nil && col.MetaData.Statistics != nil {
				colName := col.MetaData.PathInSchema[len(col.MetaData.PathInSchema)-1]
				stats := col.MetaData.Statistics

				if stats.MinValue != nil {
					metadata.MinValues[colName] = string(stats.MinValue)
				}
				if stats.MaxValue != nil {
					metadata.MaxValues[colName] = string(stats.MaxValue)
				}
				if stats.NullCount != nil {
					metadata.NullCounts[colName] = *stats.NullCount
				}
			}
		}
	}

	return metadata, nil
}

func (p *ParquetReaderImpl) ListParquetFiles(dirPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".parquet" {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}
