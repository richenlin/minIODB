package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"

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
			tempFilePath := filepath.Join("temp_parquet", fmt.Sprintf("buffer_query_%s.parquet", strings.ReplaceAll(bufferKey, "/", "_")))
			if err := q.buffer.WriteTempParquetFile(tempFilePath, bufferedRows); err == nil {
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
	}
	
	return allFiles, nil
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

	// 3. 构造并执行最终的DuckDB查询
	fileList := "'" + strings.Join(allFiles, "','") + "'"
	finalQuery := strings.Replace(sqlQuery, "FROM table", fmt.Sprintf("FROM read_parquet([%s])", fileList), 1)
	
	log.Printf("Final DuckDB query: %s", finalQuery)

	rows, err := q.db.Query(finalQuery)
	if err != nil {
		return "", fmt.Errorf("duckdb query failed: %w", err)
	}
	defer rows.Close()

	// 4. 将结果序列化为JSON
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
