package query

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	_ "github.com/marcboeker/go-duckdb"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// Querier 简化的查询处理器
// 专注于表名提取、数据定位和DuckDB查询执行
type Querier struct {
	redisClient    *redis.Client
	minioClient    storage.Uploader
	db             *sql.DB
	buffer         *buffer.SharedBuffer
	tableExtractor *SimpleTableExtractor
	logger         *zap.Logger
	tempDir        string
}

// NewQuerier 创建简化的查询处理器
func NewQuerier(redisClient *redis.Client, minioClient storage.Uploader,
	cfg config.MinioConfig, buf *buffer.SharedBuffer, logger *zap.Logger) (*Querier, error) {

	// 初始化DuckDB
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("failed to open DuckDB: %w", err)
	}

	// 创建临时目录
	tempDir := filepath.Join(os.TempDir(), "miniodb_query")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &Querier{
		redisClient:    redisClient,
		minioClient:    minioClient,
		db:             db,
		buffer:         buf,
		tableExtractor: NewSimpleTableExtractor(),
		logger:         logger,
		tempDir:        tempDir,
	}, nil
}

// ExecuteQuery 执行SQL查询
// 核心流程：提取表名 -> 定位数据文件 -> 加载到DuckDB -> 执行查询
func (q *Querier) ExecuteQuery(sqlQuery string) (string, error) {
	log.Printf("Executing query: %s", sqlQuery)

	// 1. 提取表名
	tables := q.tableExtractor.ExtractTableNames(sqlQuery)
	if len(tables) == 0 {
		return "", fmt.Errorf("no valid table names found in query")
	}

	validTables := q.tableExtractor.ValidateTableNames(tables)
	if len(validTables) == 0 {
		return "", fmt.Errorf("no valid table names found after validation")
	}

	log.Printf("Extracted tables: %v", validTables)

	// 2. 为每个表准备数据文件
	for _, tableName := range validTables {
		if err := q.prepareTableData(tableName); err != nil {
			return "", fmt.Errorf("failed to prepare data for table %s: %w", tableName, err)
		}
	}

	// 3. 执行查询
	rows, err := q.db.Query(sqlQuery)
	if err != nil {
		return "", fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// 4. 处理结果
	return q.processQueryResults(rows)
}

// prepareTableData 为指定表准备数据文件
func (q *Querier) prepareTableData(tableName string) error {
	// 1. 获取缓冲区数据文件
	bufferFiles := q.getBufferFilesForTable(tableName)

	// 2. 获取存储的数据文件
	ctx := context.Background()
	storageFiles, err := q.getStorageFilesForTable(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get storage files: %w", err)
	}

	// 3. 创建或更新DuckDB视图
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 {
		log.Printf("No data files found for table: %s", tableName)
		return nil
	}

	return q.createTableView(tableName, allFiles)
}

// getBufferFilesForTable 获取表的缓冲区文件
func (q *Querier) getBufferFilesForTable(tableName string) []string {
	if q.buffer == nil {
		return nil
	}

	// 使用正确的方法获取表的缓冲区键
	bufferKeys := q.buffer.GetTableKeys(tableName)
	var files []string

	for _, bufferKey := range bufferKeys {
		// 获取该键的数据
		rows := q.buffer.Get(bufferKey)
		if len(rows) > 0 {
			// 创建临时Parquet文件
			tempFile := q.createTempFilePath(bufferKey)
			if err := q.writeBufferToParquet(tempFile, rows); err != nil {
				log.Printf("WARN: failed to write buffer to parquet file %s: %v", tempFile, err)
				continue
			}
			files = append(files, tempFile)
		}
	}

	return files
}

// getStorageFilesForTable 获取表的存储文件
func (q *Querier) getStorageFilesForTable(ctx context.Context, tableName string) ([]string, error) {
	// 从Redis获取该表的文件索引
	pattern := fmt.Sprintf("file_index:%s:*", tableName)
	keys, err := q.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get file index keys: %w", err)
	}

	var files []string
	for _, key := range keys {
		// 获取文件信息
		objectName, err := q.redisClient.Get(ctx, key).Result()
		if err != nil {
			log.Printf("WARN: failed to get object name for key %s: %v", key, err)
			continue
		}

		// 下载文件到临时目录
		localPath, err := q.downloadToTemp(ctx, objectName)
		if err != nil {
			log.Printf("WARN: failed to download file %s: %v", objectName, err)
			continue
		}

		files = append(files, localPath)
	}

	return files, nil
}

// createTableView 在DuckDB中创建表视图
func (q *Querier) createTableView(tableName string, files []string) error {
	if len(files) == 0 {
		return nil
	}

	// 删除可能存在的旧视图
	dropViewSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s", tableName)
	if _, err := q.db.Exec(dropViewSQL); err != nil {
		log.Printf("WARN: failed to drop existing view for table %s: %v", tableName, err)
	}

	// 构建文件列表
	var filePaths []string
	for _, file := range files {
		filePaths = append(filePaths, fmt.Sprintf("'%s'", file))
	}

	// 创建新视图，支持多个Parquet文件
	createViewSQL := fmt.Sprintf(
		"CREATE VIEW %s AS SELECT * FROM read_parquet([%s])",
		tableName,
		strings.Join(filePaths, ", "),
	)

	log.Printf("Creating view for table %s with %d files", tableName, len(files))

	if _, err := q.db.Exec(createViewSQL); err != nil {
		return fmt.Errorf("failed to create view for table %s: %w", tableName, err)
	}

	return nil
}

// downloadToTemp 下载文件到临时目录
func (q *Querier) downloadToTemp(ctx context.Context, objectName string) (string, error) {
	localPath := filepath.Join(q.tempDir, filepath.Base(objectName))

	// 检查文件是否已存在
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// 使用正确的MinIO方法下载文件
	data, err := q.minioClient.GetObject(ctx, "olap-data", objectName, minio.GetObjectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", objectName, err)
	}

	// 写入本地文件
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", localPath, err)
	}

	return localPath, nil
}

// processQueryResults 处理查询结果
func (q *Querier) processQueryResults(rows *sql.Rows) (string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))

		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	// 简单的JSON格式化输出
	return q.formatResults(results), nil
}

// formatResults 格式化查询结果
func (q *Querier) formatResults(results []map[string]interface{}) string {
	if len(results) == 0 {
		return "[]"
	}

	var output strings.Builder
	output.WriteString("[\n")

	for i, row := range results {
		if i > 0 {
			output.WriteString(",\n")
		}
		output.WriteString("  {")

		first := true
		for key, value := range row {
			if !first {
				output.WriteString(", ")
			}
			first = false
			output.WriteString(fmt.Sprintf("\"%s\": ", key))

			switch v := value.(type) {
			case string:
				output.WriteString(fmt.Sprintf("\"%s\"", v))
			case nil:
				output.WriteString("null")
			default:
				output.WriteString(fmt.Sprintf("%v", v))
			}
		}
		output.WriteString("}")
	}

	output.WriteString("\n]")
	return output.String()
}

// createTempFilePath 创建临时文件路径
func (q *Querier) createTempFilePath(bufferKey string) string {
	return filepath.Join(q.tempDir, fmt.Sprintf("buffer_%s.parquet", bufferKey))
}

// writeBufferToParquet 将缓冲区数据写入Parquet文件
func (q *Querier) writeBufferToParquet(filePath string, rows []buffer.DataRow) error {
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 使用buffer的方法写入Parquet文件
	return q.buffer.WriteTempParquetFile(filePath, rows)
}

// Close 关闭查询处理器
func (q *Querier) Close() {
	if q.db != nil {
		q.db.Close()
	}
	// 清理临时文件
	os.RemoveAll(q.tempDir)
}
