package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	_ "github.com/marcboeker/go-duckdb"
)

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

// ExecuteQuery performs the actual query against buffer and persistent storage.
func (q *Querier) ExecuteQuery(sqlQuery string) (string, error) {
	log.Printf("Executing query: %s", sqlQuery)
	ctx := context.Background()

	// 1. Parse SQL (placeholder)
	// For now, we assume a simple query like "SELECT * FROM table WHERE id='...' AND day='...'"
	// and we just extract the id and day for demonstration. A real implementation
	// would require a proper SQL parser.
	id := "test-id-123"
	day := "2023-10-27"

	allFiles := []string{}

	// 2. Get file list from Redis
	redisKey := fmt.Sprintf("index:id:%s:%s", id, day)
	s3Files, err := q.redisClient.SMembers(ctx, redisKey).Result()
	if err != nil && err != redis.Nil {
		log.Printf("WARN: could not get files from redis for key %s: %v", redisKey, err)
	}
	for _, file := range s3Files {
		allFiles = append(allFiles, fmt.Sprintf("s3://%s/%s", "olap-data", file))
	}
	log.Printf("Found %d files in Redis for key %s", len(s3Files), redisKey)

	// 3. Get hot data from buffer and write to a temp parquet file
	bufferKey := fmt.Sprintf("%s/%s", id, day)
	bufferedRows := q.buffer.Get(bufferKey)
	if len(bufferedRows) > 0 {
		tempFilePath := filepath.Join("temp_parquet", fmt.Sprintf("buffer_query_%s.parquet", bufferKey))
		if err := q.buffer.WriteTempParquetFile(tempFilePath, bufferedRows); err == nil {
			allFiles = append(allFiles, tempFilePath)
			// defer os.Remove(tempFilePath) // This needs careful handling with query execution
			log.Printf("Wrote %d buffered rows to %s", len(bufferedRows), tempFilePath)
		} else {
			log.Printf("WARN: could not write buffer to temp parquet file: %v", err)
		}
	}

	if len(allFiles) == 0 {
		return "[]", nil // No data found, return empty JSON array
	}

	// 4. Construct and execute the final DuckDB query
	fileList := "'" + strings.Join(allFiles, "','") + "'"
	finalQuery := strings.Replace(sqlQuery, "FROM table", fmt.Sprintf("FROM read_parquet([%s])", fileList), 1)

	rows, err := q.db.Query(finalQuery)
	if err != nil {
		return "", fmt.Errorf("duckdb query failed: %w", err)
	}
	defer rows.Close()

	// 5. Marshal results to JSON
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
