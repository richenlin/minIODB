package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	_ "github.com/marcboeker/go-duckdb"
)

// QueryFilter 查询过滤器
type QueryFilter struct {
	ID  string
	Day string
}

// Querier handles the query execution process
type Querier struct {
	redisClient *redis.Client
	minioClient storage.Uploader
	db          *sql.DB
	buffer      *buffer.SharedBuffer
}

// NewQuerier creates a new Querier
func NewQuerier(redisClient *redis.Client, minioClient storage.Uploader, cfg config.MinioConfig, buf *buffer.SharedBuffer) (*Querier, error) {
	db, err := sql.Open("duckdb", "") // In-memory DuckDB
	if err != nil {
		return nil, err
	}

	// Setup S3 access
	setup := fmt.Sprintf(`
		INSTALL httpfs;
		LOAD httpfs;
		SET s3_region='us-east-1';
		SET s3_access_key_id='%s';
		SET s3_secret_access_key='%s';
		SET s3_endpoint='%s';
		SET s3_use_ssl=%t;
		SET s3_url_style='path';
	`, cfg.AccessKeyID, cfg.SecretAccessKey, cfg.Endpoint, cfg.UseSSL)
	if _, err = db.Exec(setup); err != nil {
		return nil, fmt.Errorf("failed to configure duckdb for s3: %w", err)
	}

	return &Querier{
		redisClient: redisClient,
		minioClient: minioClient,
		db:          db,
		buffer:      buf,
	}, nil
}

// parseWhereClause 解析SQL的WHERE子句，提取id和day条件
func (q *Querier) parseWhereClause(sqlQuery string) (*QueryFilter, error) {
	filter := &QueryFilter{}
	
	// 转换为小写以便匹配
	lowerQuery := strings.ToLower(sqlQuery)
	
	// 查找WHERE子句
	whereIndex := strings.Index(lowerQuery, "where")
	if whereIndex == -1 {
		// 如果没有WHERE子句，返回空过滤器，将查询所有数据
		return filter, nil
	}
	
	whereClause := sqlQuery[whereIndex+5:] // 跳过"where"
	
	// 使用正则表达式提取id和day条件
	// 匹配 id='value' 或 id="value" 格式
	idRegex := regexp.MustCompile(`(?i)id\s*=\s*['"]([^'"]+)['"]`)
	idMatches := idRegex.FindStringSubmatch(whereClause)
	if len(idMatches) > 1 {
		filter.ID = idMatches[1]
	}
	
	// 匹配 day='value' 或 day="value" 格式
	dayRegex := regexp.MustCompile(`(?i)day\s*=\s*['"]([^'"]+)['"]`)
	dayMatches := dayRegex.FindStringSubmatch(whereClause)
	if len(dayMatches) > 1 {
		filter.Day = dayMatches[1]
	}
	
	log.Printf("Parsed WHERE clause: id='%s', day='%s'", filter.ID, filter.Day)
	return filter, nil
}

// getDataFiles 根据过滤器获取相关的数据文件
func (q *Querier) getDataFiles(ctx context.Context, filter *QueryFilter) ([]string, error) {
	var allFiles []string
	
	if filter.ID != "" && filter.Day != "" {
		// 精确查询：根据id和day获取文件
		redisKey := fmt.Sprintf("index:id:%s:%s", filter.ID, filter.Day)
		s3Files, err := q.redisClient.SMembers(ctx, redisKey).Result()
		if err != nil && err != redis.Nil {
			log.Printf("WARN: could not get files from redis for key %s: %v", redisKey, err)
		}
		for _, file := range s3Files {
			allFiles = append(allFiles, fmt.Sprintf("s3://%s/%s", "olap-data", file))
		}
		log.Printf("Found %d files in Redis for key %s", len(s3Files), redisKey)
		
		// 获取缓冲区中的热数据
		bufferKey := fmt.Sprintf("%s/%s", filter.ID, filter.Day)
		bufferedRows := q.buffer.Get(bufferKey)
		if len(bufferedRows) > 0 {
			tempFilePath := q.createTempFilePath(bufferKey)
			if err := q.ensureTempDir(); err != nil {
				log.Printf("WARN: failed to create temp directory: %v", err)
			} else if err := q.buffer.WriteTempParquetFile(tempFilePath, bufferedRows); err == nil {
				allFiles = append(allFiles, tempFilePath)
				log.Printf("Wrote %d buffered rows to %s", len(bufferedRows), tempFilePath)
			} else {
				log.Printf("WARN: could not write buffer to temp parquet file: %v", err)
			}
		}
	} else if filter.ID != "" {
		// 按ID查询：获取该ID的所有天数据
		pattern := fmt.Sprintf("index:id:%s:*", filter.ID)
		keys, err := q.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("WARN: could not get keys from redis for pattern %s: %v", pattern, err)
		}
		for _, key := range keys {
			s3Files, err := q.redisClient.SMembers(ctx, key).Result()
			if err != nil {
				continue
			}
			for _, file := range s3Files {
				allFiles = append(allFiles, fmt.Sprintf("s3://%s/%s", "olap-data", file))
			}
		}
		log.Printf("Found %d files for ID %s", len(allFiles), filter.ID)
		
		// 获取缓冲区中该ID的所有数据
		allBufferFiles := q.getBufferFilesForID(filter.ID)
		allFiles = append(allFiles, allBufferFiles...)
	} else if filter.Day != "" {
		// 按天查询：获取该天所有ID的数据
		pattern := fmt.Sprintf("index:id:*:%s", filter.Day)
		keys, err := q.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("WARN: could not get keys from redis for pattern %s: %v", pattern, err)
		}
		for _, key := range keys {
			s3Files, err := q.redisClient.SMembers(ctx, key).Result()
			if err != nil {
				continue
			}
			for _, file := range s3Files {
				allFiles = append(allFiles, fmt.Sprintf("s3://%s/%s", "olap-data", file))
			}
		}
		log.Printf("Found %d files for day %s", len(allFiles), filter.Day)
		
		// 获取缓冲区中该天的所有数据
		allBufferFiles := q.getBufferFilesForDay(filter.Day)
		allFiles = append(allFiles, allBufferFiles...)
	} else {
		// 全表扫描：获取所有数据（谨慎使用）
		pattern := "index:id:*"
		keys, err := q.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("WARN: could not get keys from redis for pattern %s: %v", pattern, err)
		}
		for _, key := range keys {
			s3Files, err := q.redisClient.SMembers(ctx, key).Result()
			if err != nil {
				continue
			}
			for _, file := range s3Files {
				allFiles = append(allFiles, fmt.Sprintf("s3://%s/%s", "olap-data", file))
			}
		}
		log.Printf("Full table scan: found %d files", len(allFiles))
		
		// 获取缓冲区中的所有数据（全表扫描）
		allBufferFiles := q.getAllBufferFiles()
		allFiles = append(allFiles, allBufferFiles...)
	}
	
	return allFiles, nil
}

// getBufferFilesForID 获取指定ID在缓冲区中的所有数据文件
func (q *Querier) getBufferFilesForID(id string) []string {
	var bufferFiles []string
	
	// 获取缓冲区中所有匹配该ID的键
	allBufferKeys := q.buffer.GetAllKeys()
	for _, bufferKey := range allBufferKeys {
		// bufferKey格式为 "id/day"
		if strings.HasPrefix(bufferKey, id+"/") {
			bufferedRows := q.buffer.Get(bufferKey)
			if len(bufferedRows) > 0 {
				tempFilePath := q.createTempFilePath(bufferKey)
				if err := q.ensureTempDir(); err != nil {
					log.Printf("WARN: failed to create temp directory: %v", err)
					continue
				}
				if err := q.buffer.WriteTempParquetFile(tempFilePath, bufferedRows); err == nil {
					bufferFiles = append(bufferFiles, tempFilePath)
					log.Printf("Wrote %d buffered rows for ID %s to %s", len(bufferedRows), id, tempFilePath)
				} else {
					log.Printf("WARN: could not write buffer to temp parquet file for ID %s: %v", id, err)
				}
			}
		}
	}
	
	return bufferFiles
}

// getBufferFilesForDay 获取指定日期在缓冲区中的所有数据文件
func (q *Querier) getBufferFilesForDay(day string) []string {
	var bufferFiles []string
	
	// 获取缓冲区中所有匹配该日期的键
	allBufferKeys := q.buffer.GetAllKeys()
	for _, bufferKey := range allBufferKeys {
		// bufferKey格式为 "id/day"
		if strings.HasSuffix(bufferKey, "/"+day) {
			bufferedRows := q.buffer.Get(bufferKey)
			if len(bufferedRows) > 0 {
				tempFilePath := q.createTempFilePath(bufferKey)
				if err := q.ensureTempDir(); err != nil {
					log.Printf("WARN: failed to create temp directory: %v", err)
					continue
				}
				if err := q.buffer.WriteTempParquetFile(tempFilePath, bufferedRows); err == nil {
					bufferFiles = append(bufferFiles, tempFilePath)
					log.Printf("Wrote %d buffered rows for day %s to %s", len(bufferedRows), day, tempFilePath)
				} else {
					log.Printf("WARN: could not write buffer to temp parquet file for day %s: %v", day, err)
				}
			}
		}
	}
	
	return bufferFiles
}

// getAllBufferFiles 获取缓冲区中的所有数据文件（用于全表扫描）
func (q *Querier) getAllBufferFiles() []string {
	var bufferFiles []string
	
	// 获取缓冲区中所有的键
	allBufferKeys := q.buffer.GetAllKeys()
	for _, bufferKey := range allBufferKeys {
		bufferedRows := q.buffer.Get(bufferKey)
		if len(bufferedRows) > 0 {
			tempFilePath := q.createTempFilePath(bufferKey)
			if err := q.ensureTempDir(); err != nil {
				log.Printf("WARN: failed to create temp directory: %v", err)
				continue
			}
			if err := q.buffer.WriteTempParquetFile(tempFilePath, bufferedRows); err == nil {
				bufferFiles = append(bufferFiles, tempFilePath)
				log.Printf("Wrote %d buffered rows for full scan to %s", len(bufferedRows), tempFilePath)
			} else {
				log.Printf("WARN: could not write buffer to temp parquet file for full scan: %v", err)
			}
		}
	}
	
	return bufferFiles
}

// createTempFilePath 创建临时文件路径
func (q *Querier) createTempFilePath(bufferKey string) string {
	safeKey := strings.ReplaceAll(bufferKey, "/", "_")
	return filepath.Join("temp_parquet", fmt.Sprintf("buffer_query_%s_%d.parquet", safeKey, time.Now().UnixNano()))
}

// ensureTempDir 确保临时目录存在
func (q *Querier) ensureTempDir() error {
	return os.MkdirAll("temp_parquet", 0755)
}

// ExecuteQuery performs the actual query against buffer and persistent storage.
func (q *Querier) ExecuteQuery(sqlQuery string) (string, error) {
	log.Printf("Executing query: %s", sqlQuery)
	ctx := context.Background()

	// 1. 解析SQL查询的WHERE条件
	filter, err := q.parseWhereClause(sqlQuery)
	if err != nil {
		return "", fmt.Errorf("failed to parse WHERE clause: %w", err)
	}

	// 2. 根据过滤器获取相关的数据文件
	allFiles, err := q.getDataFiles(ctx, filter)
	if err != nil {
		return "", fmt.Errorf("failed to get data files: %w", err)
	}

	if len(allFiles) == 0 {
		return "[]", nil // No data found, return empty JSON array
	}

	// 3. 记录临时文件以便后续清理
	var tempFiles []string
	for _, file := range allFiles {
		if strings.HasPrefix(file, "temp_parquet/") {
			tempFiles = append(tempFiles, file)
		}
	}
	
	// 确保查询结束后清理临时文件
	defer func() {
		for _, tempFile := range tempFiles {
			if err := os.Remove(tempFile); err != nil && !os.IsNotExist(err) {
				log.Printf("WARN: failed to remove temp file %s: %v", tempFile, err)
			} else {
				log.Printf("Cleaned up temp file: %s", tempFile)
			}
		}
	}()

	// 4. 构造并执行最终的DuckDB查询
	fileList := "'" + strings.Join(allFiles, "','") + "'"
	finalQuery := strings.Replace(sqlQuery, "FROM table", fmt.Sprintf("FROM read_parquet([%s])", fileList), 1)
	
	log.Printf("Final DuckDB query: %s", finalQuery)

	rows, err := q.db.Query(finalQuery)
	if err != nil {
		return "", fmt.Errorf("duckdb query failed: %w", err)
	}
	defer rows.Close()

	// 5. 将结果序列化为JSON
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", err
		}
		rowMap := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			// DuckDB might return byte slices for strings
			if b, ok := val.([]byte); ok {
				rowMap[col] = string(b)
			} else {
				rowMap[col] = val
			}
		}
		results = append(results, rowMap)
	}

	resultBytes, err := json.Marshal(results)
	if err != nil {
		return "", err
	}

	return string(resultBytes), nil
}

// Close closes the underlying database connection
func (q *Querier) Close() {
	q.db.Close()
}
