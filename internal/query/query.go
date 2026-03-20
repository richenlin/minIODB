package query

import (
	"container/list"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/internal/buffer"
	"minIODB/internal/security"
	"minIODB/internal/storage"
	"minIODB/pkg/pool"

	"github.com/go-redis/redis/v8"
	_ "github.com/marcboeker/go-duckdb"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

// ErrNoDataFiles is returned when a table has no data files available for querying
var ErrNoDataFiles = errors.New("no data files available for table")

// ErrStorageUnavailable is returned when the table has file references but all failed to read (e.g. MinIO auth/signature error)
var ErrStorageUnavailable = errors.New("storage unavailable: failed to read data files (check MinIO credentials and endpoint)")

// ErrIntegrityCheckFailed is returned when file integrity check fails
var ErrIntegrityCheckFailed = errors.New("file integrity check failed: ETag mismatch")

// ErrFileNotFound is returned when a file referenced in metadata is not found in storage
var ErrFileNotFound = errors.New("file referenced in metadata but not found in storage")

// stmtEntry 预编译语句缓存条目
type stmtEntry struct {
	key  string
	stmt *sql.Stmt
}

// stmtCache 预编译语句LRU缓存
type stmtCache struct {
	mu      sync.Mutex
	cache   map[string]*list.Element
	lru     *list.List
	maxSize int
}

// newStmtCache 创建预编译语句缓存
func newStmtCache(maxSize int) *stmtCache {
	return &stmtCache{
		cache:   make(map[string]*list.Element),
		lru:     list.New(),
		maxSize: maxSize,
	}
}

// Get 从缓存获取预编译语句
func (sc *stmtCache) Get(key string) (*sql.Stmt, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if elem, ok := sc.cache[key]; ok {
		sc.lru.MoveToFront(elem)
		return elem.Value.(*stmtEntry).stmt, true
	}
	return nil, false
}

// Put 将预编译语句放入缓存
func (sc *stmtCache) Put(key string, stmt *sql.Stmt) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if elem, ok := sc.cache[key]; ok {
		sc.lru.MoveToFront(elem)
		return
	}
	if sc.lru.Len() >= sc.maxSize {
		oldest := sc.lru.Back()
		if oldest != nil {
			entry := sc.lru.Remove(oldest).(*stmtEntry)
			delete(sc.cache, entry.key)
			// 关闭被淘汰的语句
			if entry.stmt != nil {
				entry.stmt.Close()
			}
		}
	}
	elem := sc.lru.PushFront(&stmtEntry{key: key, stmt: stmt})
	sc.cache[key] = elem
}

// Close 关闭所有预编译语句
func (sc *stmtCache) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for _, elem := range sc.cache {
		if entry, ok := elem.Value.(*stmtEntry); ok && entry.stmt != nil {
			entry.stmt.Close()
		}
	}
	sc.cache = make(map[string]*list.Element)
	sc.lru = list.New()
}

// DuckDBPool DuckDB连接池
type DuckDBPool struct {
	connections chan *sql.DB
	maxConns    int
	created     int
	mu          sync.Mutex
}

// QueryStats 查询统计信息
type QueryStats struct {
	TotalQueries   int64            `json:"total_queries"`
	CacheHits      int64            `json:"cache_hits"`
	CacheMisses    int64            `json:"cache_misses"`
	AvgQueryTime   time.Duration    `json:"avg_query_time"`
	TotalQueryTime time.Duration    `json:"total_query_time"`
	FastestQuery   time.Duration    `json:"fastest_query"`
	SlowestQuery   time.Duration    `json:"slowest_query"`
	ErrorCount     int64            `json:"error_count"`
	FileDownloads  int64            `json:"file_downloads"`
	FileCacheHits  int64            `json:"file_cache_hits"`
	TablesQueried  map[string]int64 `json:"tables_queried"`
	QueryTypes     map[string]int64 `json:"query_types"`
}

// Querier 增强的查询处理器
// 集成查询结果缓存、文件缓存和DuckDB连接池管理
type Querier struct {
	redisClient       *redis.Client
	redisPool         *pool.RedisPool
	minioClient       storage.Uploader
	db                *sql.DB
	buffer            *buffer.ConcurrentBuffer
	tableExtractor    *TableExtractor // 升级到增强版本
	logger            *zap.Logger
	tempDir           string
	config            *config.Config
	encryptionManager *security.FieldEncryptionManager // 字段加密管理器

	// 缓存组件
	queryCache *QueryCache
	fileCache  *FileCache

	// 性能优化组件
	dbPool             *DuckDBPool
	stmtCache          *stmtCache      // 预编译语句LRU缓存
	queryOptimizer     *QueryOptimizer // 查询优化器（文件剪枝、谓词下推）
	columnPruner       *ColumnPruner   // 列剪枝优化器
	slowQueryThreshold time.Duration   // 慢查询阈值

	// 统计信息
	queryStats *QueryStats
	statsLock  sync.RWMutex
}

// NewQuerier 创建查询器
func NewQuerier(redisPool *pool.RedisPool, minioClient storage.Uploader, cfg *config.Config, buf *buffer.ConcurrentBuffer, logger *zap.Logger) (*Querier, error) {
	// 初始化DuckDB连接池
	duckdbPool, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create DuckDB connection pool: %w", err)
	}

	// 初始化查询缓存和文件缓存（只有在Redis连接池可用时）
	var queryCache *QueryCache
	var fileCache *FileCache

	if redisPool != nil {
		// 从连接池获取Redis客户端
		redisClient := redisPool.GetRedisClient()
		if redisClient != nil {
			// 创建查询缓存配置
			cacheConfig := &CacheConfig{
				DefaultTTL:     30 * time.Minute,
				MaxCacheSize:   100 * 1024 * 1024, // 100MB
				EnableMetrics:  true,
				KeyPrefix:      "query_cache:",
				EvictionPolicy: "lru",
			}
			queryCache = NewQueryCache(redisClient, cacheConfig, logger)

			// 创建文件缓存配置
			fileCacheConfig := &FileCacheConfig{
				CacheDir:        filepath.Join(os.TempDir(), "miniodb_file_cache"),
				MaxCacheSize:    500 * 1024 * 1024, // 500MB
				MaxFileAge:      2 * time.Hour,
				CleanupInterval: 10 * time.Minute,
			}
			fileCache, err = NewFileCache(fileCacheConfig, redisClient, logger)
			if err != nil {
				logger.Warn("Failed to create file cache", zap.Error(err))
				fileCache = nil
			}
		}
	}

	// 如果Redis不可用，使用内存缓存或禁用缓存
	if queryCache == nil {
		logger.Info("Redis不可用，查询缓存已禁用")
	}
	if fileCache == nil {
		logger.Info("Redis不可用，文件缓存已禁用")
	}

	// 创建临时目录
	tempDir := filepath.Join(os.TempDir(), "miniodb_query")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// 初始化查询统计
	queryStats := &QueryStats{
		TablesQueried: make(map[string]int64),
		QueryTypes:    make(map[string]int64),
		FastestQuery:  time.Hour, // 初始化为一个大值
	}

	querier := &Querier{
		redisPool:          redisPool,
		minioClient:        minioClient,
		db:                 duckdbPool,
		queryCache:         queryCache,
		fileCache:          fileCache,
		config:             cfg,
		buffer:             buf,
		logger:             logger,
		tempDir:            tempDir,
		tableExtractor:     NewTableExtractor(),
		stmtCache:          newStmtCache(1000), // 预编译语句LRU缓存，maxSize=1000
		queryStats:         queryStats,
		queryOptimizer:     NewQueryOptimizer(logger),                                     // 查询优化器（文件剪枝、谓词下推）
		columnPruner:       NewColumnPruner(cfg.StorageEngine.Parquet.AutoSelectStrategy), // 列剪枝优化器
		slowQueryThreshold: cfg.QueryOptimization.DuckDB.SlowQueryThreshold,               // 慢查询阈值（从配置读取）
	}

	return querier, nil
}

// ExecuteQuery 执行SQL查询（增强版本）
// 核心流程：缓存检查 -> 表名提取 -> 文件缓存 -> DuckDB执行 -> 结果缓存
func (q *Querier) ExecuteQuery(sqlQuery string) (string, error) {
	startTime := time.Now()

	q.logger.Sugar().Infof("Executing enhanced query: %s", sqlQuery)

	// 0. 验证SQL查询的安全性（在提取表名之前执行，以防止恶意SQL）
	if err := security.DefaultSanitizer.ValidateSelectQuery(sqlQuery); err != nil {
		q.updateErrorStats()
		return "", fmt.Errorf("SQL query validation failed: %w", err)
	}

	// 1. 提取表名
	tables := q.tableExtractor.ExtractTableNames(sqlQuery)
	if len(tables) == 0 {
		q.updateErrorStats()
		return "", fmt.Errorf("no valid table names found in query")
	}

	validTables := q.tableExtractor.ValidateTableNames(tables)
	if len(validTables) == 0 {
		q.updateErrorStats()
		return "", fmt.Errorf("no valid table names found after validation")
	}

	q.logger.Sugar().Infof("Extracted tables: %v", validTables)

	// 2. 检查查询缓存（只有在Redis连接池可用时）
	ctx := context.Background()
	if q.queryCache != nil && q.redisPool != nil {
		if cacheEntry, found := q.queryCache.Get(ctx, sqlQuery, validTables); found {
			q.updateCacheHitStats(validTables, time.Since(startTime))
			q.logger.Sugar().Infof("Query cache HIT - returning cached result")
			return cacheEntry.Result, nil
		}
	}

	// 3. 缓存未命中，执行查询
	q.updateCacheMissStats()

	// 4. 获取数据库连接（提前获取，确保整个流程使用同一连接）
	db, err := q.getDBConnection()
	if err != nil {
		q.updateErrorStats()
		return "", fmt.Errorf("failed to get database connection: %w", err)
	}
	defer q.returnDBConnection(db)

	// 5. 为每个表准备数据文件（使用同一个数据库连接，支持文件剪枝）
	for _, tableName := range validTables {
		if err := q.prepareTableDataWithPruning(ctx, db, tableName, sqlQuery); err != nil {
			// 如果表没有数据文件，返回空结果
			if errors.Is(err, ErrNoDataFiles) {
				q.logger.Sugar().Infof("Returning empty result for table with no data: %s", tableName)
				return "[]", nil
			}
			// 有文件但全部读取失败（如 MinIO 签名/凭证错误），向用户返回明确错误
			if errors.Is(err, ErrStorageUnavailable) {
				q.updateErrorStats()
				return "", err
			}
			q.updateErrorStats()
			return "", fmt.Errorf("failed to prepare data for table %s: %w", tableName, err)
		}
	}

	// 6. 执行查询（使用相同的数据库连接）
	result, err := q.executeQueryWithOptimization(db, sqlQuery)
	if err != nil {
		q.updateErrorStats()
		return "", fmt.Errorf("query execution failed: %w", err)
	}

	// 7. 将结果存入缓存（只有在Redis连接池可用时）
	if q.queryCache != nil && q.redisPool != nil {
		if err := q.queryCache.Set(ctx, sqlQuery, result, validTables); err != nil {
			q.logger.Warn("Failed to cache query result", zap.Error(err))
		}
	}

	// 8. 更新统计信息
	queryTime := time.Since(startTime)
	q.updateQueryStats(validTables, queryTime, q.tableExtractor.GetQueryType(sqlQuery))

	// 9. 慢查询检测与日志
	if q.slowQueryThreshold > 0 && queryTime > q.slowQueryThreshold {
		q.logger.Warn("slow query detected",
			zap.String("sql", sqlQuery),
			zap.Duration("elapsed", queryTime),
			zap.Duration("threshold", q.slowQueryThreshold),
			zap.Int("result_size", len(result)),
			zap.Strings("tables", validTables),
		)
	}

	q.logger.Sugar().Infof("Query executed successfully in %v", queryTime)
	return result, nil
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
		q.logger.Sugar().Infof("No data files found for table: %s", tableName)
		return nil
	}

	return q.createTableView(tableName, allFiles)
}

// getBufferFilesForTable 获取表的缓冲区文件
func (q *Querier) getBufferFilesForTable(tableName string) []string {
	if q.buffer == nil {
		q.logger.Sugar().Infof("DEBUG: buffer is nil for table %s", tableName)
		return nil
	}

	// 使用正确的方法获取表的缓冲区键
	bufferKeys := q.buffer.GetTableKeys(tableName)
	q.logger.Sugar().Infof("DEBUG: found %d buffer keys for table %s: %v", len(bufferKeys), tableName, bufferKeys)
	var files []string

	for _, bufferKey := range bufferKeys {
		// 获取该键的数据
		rows := q.buffer.Get(bufferKey)
		q.logger.Sugar().Infof("DEBUG: got %d rows for buffer key %s", len(rows), bufferKey)
		if len(rows) > 0 {
			// 创建临时Parquet文件
			tempFile := q.createTempFilePath(bufferKey)
			if err := q.writeBufferToParquet(tempFile, rows); err != nil {
				q.logger.Sugar().Infof("WARN: failed to write buffer to parquet file %s: %v", tempFile, err)
				continue
			}
			files = append(files, tempFile)
		}
	}

	return files
}

// getStorageFilesForTable 获取表的存储文件
func (q *Querier) getStorageFilesForTable(ctx context.Context, tableName string) ([]string, error) {
	// 单节点模式：redisPool 为 nil，无法从 Redis 索引获取
	if q.redisPool == nil {
		return []string{}, nil
	}

	// 从连接池获取 Redis 客户端
	redisClient := q.redisPool.GetClient()
	if redisClient == nil {
		return []string{}, nil
	}

	// 使用架构设计的索引格式：index:table:tableName:id:*
	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	// 使用 SCAN 替代 KEYS，避免阻塞 Redis
	keys, err := pool.ScanKeys(ctx, redisClient, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get file index keys: %w", err)
	}

	var files []string
	for _, key := range keys {
		// 获取集合中的所有文件名（使用SMembers读取集合）
		objectNames, err := redisClient.SMembers(ctx, key).Result()
		if err != nil {
			q.logger.Sugar().Infof("WARN: failed to get object names for key %s: %v", key, err)
			continue
		}

		// 下载每个文件到临时目录
		for _, objectName := range objectNames {
			localPath, err := q.downloadToTemp(ctx, objectName)
			if err != nil {
				q.logger.Sugar().Infof("WARN: failed to download file %s: %v", objectName, err)
				continue
			}
			files = append(files, localPath)
		}
	}

	return files, nil
}

// createTableView 在DuckDB中创建表视图（保持向后兼容）
func (q *Querier) createTableView(tableName string, files []string) error {
	return q.createTableViewWithDB(q.db, tableName, files)
}

// createTableViewWithDB 在指定的DuckDB连接中创建表视图
func (q *Querier) createTableViewWithDB(db *sql.DB, tableName string, files []string) error {
	if len(files) == 0 {
		return nil
	}

	// 使用安全的 SQL 构建器删除旧视图
	dropViewSQL, err := security.DefaultSanitizer.BuildSafeDropViewSQL(tableName)
	if err != nil {
		return fmt.Errorf("invalid table name for drop view: %w", err)
	}
	q.logger.Sugar().Infof("Executing DROP VIEW SQL: %s", dropViewSQL)
	if _, err := db.Exec(dropViewSQL); err != nil {
		q.logger.Sugar().Infof("WARN: failed to drop existing view for table %s: %v", tableName, err)
	} else {
		q.logger.Sugar().Infof("Successfully dropped existing view (if any) for table %s", tableName)
	}

	// 使用安全的 SQL 构建器创建视图
	createViewSQL, err := security.DefaultSanitizer.BuildSafeCreateViewSQL(tableName, files)
	if err != nil {
		return fmt.Errorf("invalid parameters for create view: %w", err)
	}

	q.logger.Sugar().Infof("Creating view for table %s with %d files using provided DB connection", tableName, len(files))
	q.logger.Sugar().Infof("Executing CREATE VIEW SQL: %s", createViewSQL)

	if _, err := db.Exec(createViewSQL); err != nil {
		q.logger.Sugar().Infof("ERROR: Failed to create view for table %s with SQL: %s", tableName, createViewSQL)
		q.logger.Sugar().Infof("ERROR: SQL execution error: %v", err)
		q.logger.Sugar().Infof("ERROR: File paths involved: %v", files)
		return fmt.Errorf("failed to create view for table %s: %w", tableName, err)
	}

	q.logger.Sugar().Infof("Successfully created view for table %s", tableName)

	// 使用安全的方式验证视图
	testSQL, err := security.DefaultSanitizer.BuildSafeSelectSQL(tableName, nil, "", "")
	if err != nil {
		return fmt.Errorf("invalid table name for verification: %w", err)
	}
	testSQL = testSQL + " LIMIT 0"
	q.logger.Sugar().Infof("Testing view existence with SQL: %s", testSQL)
	if _, err := db.Query(testSQL); err != nil {
		q.logger.Sugar().Infof("ERROR: View verification failed for table %s: %v", tableName, err)
		return fmt.Errorf("view creation verification failed for table %s: %w", tableName, err)
	}

	q.logger.Sugar().Infof("View verification successful for table %s", tableName)
	return nil
}

// createTableViewWithDBWithColumnPruning 在指定的DuckDB连接中创建表视图（带列剪枝优化）
func (q *Querier) createTableViewWithDBWithColumnPruning(db *sql.DB, tableName string, files []string, sqlQuery string) error {
	if len(files) == 0 {
		return nil
	}

	// 提取需要的列
	requiredColumns := q.columnPruner.ExtractRequiredColumns(sqlQuery)

	// 构建优化的视图SQL
	createViewSQL := q.columnPruner.BuildOptimizedViewSQL(tableName, files, requiredColumns)

	// 使用安全的 SQL 构建器删除旧视图
	dropViewSQL, err := security.DefaultSanitizer.BuildSafeDropViewSQL(tableName)
	if err != nil {
		return fmt.Errorf("invalid table name for drop view: %w", err)
	}
	q.logger.Sugar().Infof("Executing DROP VIEW SQL: %s", dropViewSQL)
	if _, err := db.Exec(dropViewSQL); err != nil {
		q.logger.Sugar().Infof("WARN: failed to drop existing view for table %s: %v", tableName, err)
	}

	// 执行优化的创建视图
	q.logger.Sugar().Infof("Creating optimized view for table %s with %d files, columns: %v", tableName, len(files), requiredColumns)
	q.logger.Sugar().Infof("Executing CREATE VIEW SQL: %s", createViewSQL)

	if _, err := db.Exec(createViewSQL); err != nil {
		q.logger.Sugar().Infof("ERROR: Failed to create optimized view for table %s: %v", tableName, err)
		q.logger.Sugar().Infof("ERROR: SQL execution error: %v", err)
		return fmt.Errorf("failed to create optimized view for table %s: %w", tableName, err)
	}

	q.logger.Sugar().Infof("Successfully created optimized view for table %s with column pruning", tableName)

	// 验证视图
	testSQL, err := security.DefaultSanitizer.BuildSafeSelectSQL(tableName, requiredColumns, "", "LIMIT 0")
	if err != nil {
		return fmt.Errorf("invalid table name for verification: %w", err)
	}

	if _, err := db.Query(testSQL); err != nil {
		q.logger.Sugar().Infof("ERROR: Optimized view verification failed for table %s: %v", tableName, err)
		return fmt.Errorf("optimized view creation verification failed for table %s: %w", tableName, err)
	}

	q.logger.Sugar().Infof("Optimized view verification successful for table %s", tableName)
	return nil
}

// downloadToTemp 下载文件到临时目录，并进行完整性校验
func (q *Querier) downloadToTemp(ctx context.Context, objectName string) (string, error) {
	localPath := filepath.Join(q.tempDir, filepath.Base(objectName))

	// 检查文件是否已存在
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	bucket := q.config.GetMinIO().Bucket

	// 获取对象信息以获取 ETag（用于完整性校验）
	objInfo, err := q.minioClient.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			q.logger.Warn("Parquet file referenced in metadata but not found in MinIO",
				zap.String("object", objectName),
				zap.String("bucket", bucket))
			return "", fmt.Errorf("file not found in storage: %s: %w", objectName, ErrFileNotFound)
		}
		q.logger.Sugar().Errorf("Failed to get object info for %s: %v", objectName, err)
		return "", fmt.Errorf("failed to get object info for %s: %w", objectName, err)
	}

	// 使用正确的MinIO方法下载文件
	data, err := q.minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		q.logger.Sugar().Errorf("Failed to download %s: %v", objectName, err)
		return "", fmt.Errorf("failed to download %s: %w", objectName, err)
	}

	// 写入本地文件
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		q.logger.Sugar().Errorf("Failed to write file %s: %v", localPath, err)
		return "", fmt.Errorf("failed to write file %s: %w", localPath, err)
	}

	// 完整性校验：对比本地文件 MD5 与 MinIO ETag
	if err := verifyFileIntegrity(localPath, objInfo.ETag); err != nil {
		// 校验失败，删除损坏的本地文件
		os.Remove(localPath)
		q.logger.Warn("File integrity check failed, file deleted",
			zap.String("object", objectName),
			zap.String("etag", objInfo.ETag),
			zap.Error(err))
		return "", fmt.Errorf("%w: %s", ErrIntegrityCheckFailed, objectName)
	}

	q.logger.Debug("File integrity verified",
		zap.String("object", objectName),
		zap.String("etag", objInfo.ETag))

	return localPath, nil
}

// verifyFileIntegrity 验证本地文件的 MD5 与 MinIO ETag 是否匹配
// MinIO ETag 通常是 MD5 的 hex 字符串（不含引号）
// 注意：分段上传的对象 ETag 格式为 "etag-N"，无法进行简单 MD5 比较
func verifyFileIntegrity(filePath string, expectedETag string) error {
	// 去除 ETag 中可能的引号
	expectedETag = strings.Trim(expectedETag, "\"")

	// 检查是否为分段上传的 ETag（格式：etag-N，其中 N 是分段数）
	// 分段上传的 ETag 无法进行简单的 MD5 比较，跳过校验
	if strings.Contains(expectedETag, "-") {
		// 分段上传的 ETag 格式不适用于 MD5 比较，跳过校验
		return nil
	}

	// 读取本地文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file for integrity check: %w", err)
	}

	// 计算 MD5
	hash := md5.Sum(data)
	actualETag := hex.EncodeToString(hash[:])

	// 比较 ETag
	if !strings.EqualFold(actualETag, expectedETag) {
		return fmt.Errorf("integrity check failed: expected ETag %s, got %s", expectedETag, actualETag)
	}

	return nil
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

		// Decrypt encrypted fields if encryption is enabled
		if q.encryptionManager != nil && q.encryptionManager.IsEnabled() {
			decryptedRow, err := q.encryptionManager.DecryptFields(row)
			if err != nil {
				q.logger.Warn("Failed to decrypt row fields, using original values",
					zap.Error(err))
				// Continue with original row on decryption failure
			} else {
				row = decryptedRow
			}
		}

		results = append(results, row)
	}

	// 简单的JSON格式化输出
	return q.formatResults(results), nil
}

// SetEncryptionManager sets the encryption manager for field decryption
func (q *Querier) SetEncryptionManager(manager *security.FieldEncryptionManager) {
	q.encryptionManager = manager
}

// formatResults 格式化查询结果为合法 JSON（兼容 time.Time、[]byte 等类型）
func (q *Querier) formatResults(results []map[string]interface{}) string {
	if len(results) == 0 {
		return "[]"
	}
	b, err := json.Marshal(results)
	if err != nil {
		q.logger.Warn("formatResults: json.Marshal failed, falling back to empty array", zap.Error(err))
		return "[]"
	}
	return string(b)
}

// createTempFilePath 创建临时文件路径，规范化 bufferKey 中的 "//" 避免路径非法（兼容历史/WAL 产生的 key）
func (q *Querier) createTempFilePath(bufferKey string) string {
	normalized := path.Clean(bufferKey)
	return filepath.Join(q.tempDir, fmt.Sprintf("buffer_%s.parquet", normalized))
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

// Stop 停止查询处理器（优雅关闭）
func (q *Querier) Stop() error {
	q.logger.Info("Stopping Querier...")

	// 停止QueryCache
	if q.queryCache != nil {
		q.queryCache.Stop()
	}

	// 关闭资源
	if err := q.Close(); err != nil {
		return err
	}

	q.logger.Info("Querier stopped gracefully")
	return nil
}

// Close 关闭查询处理器
func (q *Querier) Close() error {
	if q.db != nil {
		q.db.Close()
	}

	// 关闭连接池
	if q.dbPool != nil {
		q.dbPool.Close()
	}

	// 关闭所有预编译语句（使用LRU缓存）
	if q.stmtCache != nil {
		q.stmtCache.Close()
	}

	// 清理缓存
	if q.fileCache != nil {
		q.fileCache.Clear()
	}

	// 清理临时文件
	os.RemoveAll(q.tempDir)

	return nil
}

// =============================================================================
// DuckDB 连接池方法
// =============================================================================

// NewDuckDBPool 创建DuckDB连接池
func NewDuckDBPool(maxConns int, logger *zap.Logger) (*DuckDBPool, error) {
	pool := &DuckDBPool{
		connections: make(chan *sql.DB, maxConns),
		maxConns:    maxConns,
	}

	// 预创建一个连接以验证配置
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to create initial connection: %w", err)
	}

	// 配置DuckDB性能优化参数
	optimizations := []string{
		"SET memory_limit='1GB'",       // 设置内存限制
		"SET threads=4",                // 设置线程数
		"SET enable_object_cache=true", // 启用对象缓存
		"SET enable_httpfs=true",       // 启用HTTP文件系统
		"SET max_memory='1GB'",         // 最大内存
	}

	for _, opt := range optimizations {
		if _, err := db.Exec(opt); err != nil {
			logger.Sugar().Infof("WARN: failed to apply DuckDB optimization '%s': %v", opt, err)
		}
	}

	pool.connections <- db
	pool.created = 1

	return pool, nil
}

// Get 从连接池获取连接
func (p *DuckDBPool) Get() (*sql.DB, error) {
	select {
	case db := <-p.connections:
		return db, nil
	default:
		p.mu.Lock()
		if p.created < p.maxConns {
			db, err := sql.Open("duckdb", ":memory:")
			if err != nil {
				p.mu.Unlock()
				return nil, err
			}
			p.created++
			p.mu.Unlock()
			return db, nil
		}
		p.mu.Unlock()

		// 带超时等待
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case db := <-p.connections:
			return db, nil
		case <-timer.C:
			return nil, fmt.Errorf("DuckDB connection pool: timeout waiting for available connection")
		}
	}
}

// Put 将连接返回到池中
func (p *DuckDBPool) Put(db *sql.DB) {
	select {
	case p.connections <- db:
	default:
		// 池已满，关闭连接
		db.Close()
		p.mu.Lock()
		p.created--
		p.mu.Unlock()
	}
}

// Close 关闭连接池
func (p *DuckDBPool) Close() {
	close(p.connections)
	for db := range p.connections {
		db.Close()
	}
}

// =============================================================================
// 增强的查询方法
// =============================================================================

// prepareTableDataWithCache 使用文件缓存准备表数据
func (q *Querier) prepareTableDataWithCache(tableName string) error {
	// 1. 获取缓冲区数据文件
	bufferFiles := q.getBufferFilesForTable(tableName)

	// 2. 获取存储的数据文件（使用文件缓存）
	ctx := context.Background()
	storageFiles, err := q.getStorageFilesWithCache(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get storage files: %w", err)
	}

	// 3. 创建或更新DuckDB视图
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 {
		q.logger.Sugar().Infof("No data files found for table: %s", tableName)
		return nil
	}

	return q.createTableView(tableName, allFiles)
}

// getStorageFilesWithCache 使用文件缓存获取存储文件
func (q *Querier) getStorageFilesWithCache(ctx context.Context, tableName string) ([]string, error) {
	// 单节点模式：直接从 MinIO 列出文件
	if q.redisPool == nil {
		return q.getStorageFilesFromMinIO(ctx, tableName)
	}

	// 分布式模式：从 Redis 索引获取文件列表
	redisClient := q.redisPool.GetClient()

	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	// 使用 SCAN 替代 KEYS，避免阻塞 Redis
	keys, err := pool.ScanKeys(ctx, redisClient, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get file index keys: %w", err)
	}

	var files []string
	for _, key := range keys {
		objectNames, err := redisClient.SMembers(ctx, key).Result()
		if err != nil {
			q.logger.Sugar().Infof("WARN: failed to get object names for key %s: %v", key, err)
			continue
		}

		for _, objectName := range objectNames {
			localPath, err := q.getFileWithCache(ctx, objectName)
			if err != nil {
				q.logger.Sugar().Infof("WARN: failed to get cached file %s: %v", objectName, err)
				continue
			}
			files = append(files, localPath)
		}
	}

	return files, nil
}

// getStorageFilesFromMinIO 单节点模式下直接从 MinIO 列出文件
func (q *Querier) getStorageFilesFromMinIO(ctx context.Context, tableName string) ([]string, error) {
	if q.minioClient == nil {
		return []string{}, nil
	}

	prefix := tableName + "/"
	bucket := q.config.GetMinIO().Bucket

	q.logger.Sugar().Debugf("Listing MinIO objects with prefix: %s/%s", bucket, prefix)

	var files []string
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}
	objectCh := q.minioClient.ListObjects(ctx, bucket, opts)

	for object := range objectCh {
		if object.Err != nil {
			q.logger.Sugar().Warnf("Error listing object: %v", object.Err)
			continue
		}

		if filepath.Ext(object.Key) != ".parquet" {
			continue
		}

		localPath, err := q.getFileWithCache(ctx, object.Key)
		if err != nil {
			q.logger.Sugar().Warnf("Failed to download %s: %v", object.Key, err)
			continue
		}

		files = append(files, localPath)
	}

	q.logger.Sugar().Debugf("Found %d parquet files for table %s", len(files), tableName)
	return files, nil
}

// getFileWithCache 使用缓存获取文件，如果缓存不可用则直接下载
func (q *Querier) getFileWithCache(ctx context.Context, objectName string) (string, error) {
	if q.fileCache != nil {
		return q.fileCache.Get(ctx, objectName, func(objName string) (string, error) {
			q.updateFileDownloadStats()
			return q.downloadToTemp(ctx, objName)
		})
	}

	q.updateFileDownloadStats()
	return q.downloadToTemp(ctx, objectName)
}

// getDBConnection 获取数据库连接
func (q *Querier) getDBConnection() (*sql.DB, error) {
	if q.dbPool != nil {
		return q.dbPool.Get()
	}
	return q.db, nil
}

// returnDBConnection 归还数据库连接
func (q *Querier) returnDBConnection(db *sql.DB) {
	if q.dbPool != nil && db != q.db {
		q.dbPool.Put(db)
	}
}

// executeQueryWithOptimization 使用优化执行查询
func (q *Querier) executeQueryWithOptimization(db *sql.DB, sqlQuery string) (string, error) {
	// 验证SQL查询的安全性
	if err := security.DefaultSanitizer.ValidateSelectQuery(sqlQuery); err != nil {
		return "", fmt.Errorf("SQL query validation failed: %w", err)
	}

	// 检查是否可以使用预编译语句
	if q.canUsePreparedStatement(sqlQuery) {
		return q.executeWithPreparedStatement(db, sqlQuery)
	}

	// 直接执行查询
	rows, err := db.Query(sqlQuery)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	return q.processQueryResults(rows)
}

// canUsePreparedStatement 检查是否可以使用预编译语句
func (q *Querier) canUsePreparedStatement(sqlQuery string) bool {
	// 简单检查：如果查询包含参数占位符或者是常见的查询模式
	queryLower := strings.ToLower(strings.TrimSpace(sqlQuery))

	// 聚合查询适合预编译
	if strings.Contains(queryLower, "count(") ||
		strings.Contains(queryLower, "sum(") ||
		strings.Contains(queryLower, "avg(") ||
		strings.Contains(queryLower, "group by") {
		return true
	}

	return false
}

// executeWithPreparedStatement 使用预编译语句执行查询
func (q *Querier) executeWithPreparedStatement(db *sql.DB, sqlQuery string) (string, error) {
	// 生成语句键
	stmtKey := fmt.Sprintf("%p_%s", db, sqlQuery)

	// 从LRU缓存获取预编译语句
	stmt, exists := q.stmtCache.Get(stmtKey)

	if !exists {
		// 创建预编译语句
		var err error
		stmt, err = db.Prepare(sqlQuery)
		if err != nil {
			// 如果预编译失败，回退到直接查询
			rows, err := db.Query(sqlQuery)
			if err != nil {
				return "", err
			}
			defer rows.Close()
			return q.processQueryResults(rows)
		}

		// 将预编译语句放入LRU缓存
		q.stmtCache.Put(stmtKey, stmt)
	}

	// 执行预编译语句
	rows, err := stmt.Query()
	if err != nil {
		return "", err
	}
	defer rows.Close()

	return q.processQueryResults(rows)
}

// ExecuteUpdate 执行更新/删除语句，返回受影响的行数
// 适用于 INSERT、UPDATE、DELETE 等不返回结果集的语句
func (q *Querier) ExecuteUpdate(sqlStatement string) (int64, error) {
	startTime := time.Now()

	q.logger.Sugar().Infof("Executing update statement: %s", sqlStatement)

	// 获取数据库连接
	db, err := q.getDBConnection()
	if err != nil {
		q.updateErrorStats()
		return 0, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer q.returnDBConnection(db)

	// 提取表名并为表准备数据视图
	tables := q.tableExtractor.ExtractTableNames(sqlStatement)
	hasData := false
	for _, tableName := range tables {
		if err := q.prepareTableDataWithDB(db, tableName); err != nil {
			if errors.Is(err, ErrNoDataFiles) {
				q.logger.Sugar().Infof("No data files for table %s, update will have no effect", tableName)
				continue
			}
			q.updateErrorStats()
			return 0, fmt.Errorf("failed to prepare table %s: %w", tableName, err)
		}
		hasData = true
	}

	// 如果没有任何表有数据文件，直接返回 0 行受影响
	if !hasData {
		q.logger.Sugar().Infof("No data files found for any table, returning 0 rows affected")
		return 0, nil
	}

	// 使用 Exec 执行语句，获取受影响的行数
	result, err := db.Exec(sqlStatement)
	if err != nil {
		q.updateErrorStats()
		return 0, fmt.Errorf("update execution failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		q.updateErrorStats()
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	q.logger.Sugar().Infof("Update completed: %d rows affected in %v", rowsAffected, time.Since(startTime))

	return rowsAffected, nil
}

// RewriteParquetForDelete 通过重写 parquet 文件实现物理删除记录。
// 仅重写包含目标 ID 的文件，其余文件不受影响，适合低频删除场景。
// 返回：实际删除的记录数、是否在持久化存储中找到该 ID、错误。
func (q *Querier) RewriteParquetForDelete(ctx context.Context, tableName, id string) (int64, bool, error) {
	if q.redisPool == nil || q.minioClient == nil {
		return 0, false, nil // 单节点无 Redis/MinIO，无持久化数据可删
	}

	startTime := time.Now()
	q.logger.Sugar().Infof("Starting parquet rewrite delete for table=%s id=%s", tableName, id)

	redisClient := q.redisPool.GetClient()
	pattern := fmt.Sprintf("index:table:%s:id:%s:*", tableName, id)
	idIndexKeys, err := pool.ScanKeys(ctx, redisClient, pattern)
	if err != nil {
		return 0, false, fmt.Errorf("failed to scan redis index for id %s: %w", id, err)
	}
	if len(idIndexKeys) == 0 {
		q.logger.Sugar().Infof("No persisted index found for table=%s id=%s, nothing to rewrite", tableName, id)
		return 0, false, nil
	}

	// 收集包含该 ID 的所有 parquet object names（一个文件可能被多个 id key 引用）
	objectNameSet := make(map[string]struct{})
	for _, key := range idIndexKeys {
		members, err := redisClient.SMembers(ctx, key).Result()
		if err != nil {
			q.logger.Sugar().Warnf("Failed to get members for key %s: %v", key, err)
			continue
		}
		for _, m := range members {
			objectNameSet[m] = struct{}{}
		}
	}

	if len(objectNameSet) == 0 {
		// Redis 索引键存在但集合为空，仍视为找到（清理悬挂索引）
		if err := redisClient.Del(ctx, idIndexKeys...).Err(); err != nil {
			q.logger.Sugar().Warnf("Failed to delete empty redis index keys for id %s: %v", id, err)
		}
		return 0, true, nil
	}

	bucket := q.config.GetMinIO().Bucket
	var totalDeleted int64

	for objectName := range objectNameSet {
		deleted, err := q.rewriteSingleParquet(ctx, bucket, objectName, tableName, id)
		if err != nil {
			q.logger.Sugar().Warnf("Failed to rewrite parquet %s: %v (skipping)", objectName, err)
			continue
		}
		totalDeleted += deleted
	}

	// 删除该 ID 在 Redis 中的所有索引 key
	if err := redisClient.Del(ctx, idIndexKeys...).Err(); err != nil {
		q.logger.Sugar().Warnf("Failed to delete redis index keys for id %s: %v", id, err)
	}

	q.logger.Sugar().Infof("Parquet rewrite delete completed: table=%s id=%s deleted=%d elapsed=%v",
		tableName, id, totalDeleted, time.Since(startTime))
	return totalDeleted, true, nil
}

// rewriteSingleParquet 下载单个 parquet 文件，过滤掉指定 id 的行，再上传回 MinIO。
// 如果过滤后文件为空，则删除 MinIO 对象并更新其他 ID 的索引引用。
func (q *Querier) rewriteSingleParquet(ctx context.Context, bucket, objectName, tableName, deleteID string) (int64, error) {
	// 1. 下载文件到本地
	localPath, err := q.downloadToTemp(ctx, objectName)
	if err != nil {
		return 0, fmt.Errorf("download failed: %w", err)
	}

	// 2. 用临时 DuckDB 连接读取并过滤
	tmpDB, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return 0, fmt.Errorf("failed to open tmp duckdb: %w", err)
	}
	defer tmpDB.Close()

	quotedPath := security.DefaultSanitizer.QuoteLiteral(localPath)
	quotedID := security.DefaultSanitizer.QuoteLiteral(deleteID)

	// 统计将要删除的行数
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s) WHERE id = %s", quotedPath, quotedID)
	var deletedCount int64
	if err := tmpDB.QueryRowContext(ctx, countSQL).Scan(&deletedCount); err != nil {
		return 0, fmt.Errorf("count query failed: %w", err)
	}
	if deletedCount == 0 {
		return 0, nil // 该文件不包含此 ID，无需处理
	}

	// 检查过滤后是否还有剩余行
	totalCountSQL := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s)", quotedPath)
	var totalCount int64
	if err := tmpDB.QueryRowContext(ctx, totalCountSQL).Scan(&totalCount); err != nil {
		return 0, fmt.Errorf("total count query failed: %w", err)
	}
	remainingCount := totalCount - deletedCount

	if remainingCount == 0 {
		// 文件只包含此 ID 的数据，直接删除 MinIO 对象
		q.logger.Sugar().Infof("Parquet file %s contains only records for id=%s, deleting object", objectName, deleteID)
		if err := q.minioClient.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{}); err != nil {
			q.logger.Sugar().Warnf("Failed to remove empty parquet object %s: %v", objectName, err)
		} else {
			q.logger.Sugar().Infof("Removed parquet object %s from MinIO", objectName)
		}
		// 清理其他 ID 在 Redis 中对该文件的引用
		q.removeObjectFromOtherIDIndexes(ctx, bucket, tableName, objectName, deleteID)
	} else {
		// 3. 将过滤后的数据写出到新临时文件
		newLocalPath := localPath + ".rewrite"
		copySQL := fmt.Sprintf(
			"COPY (SELECT * FROM read_parquet(%s) WHERE id != %s) TO %s (FORMAT PARQUET, COMPRESSION SNAPPY)",
			quotedPath, quotedID, security.DefaultSanitizer.QuoteLiteral(newLocalPath))
		if _, err := tmpDB.ExecContext(ctx, copySQL); err != nil {
			return 0, fmt.Errorf("copy to parquet failed: %w", err)
		}
		defer os.Remove(newLocalPath)

		// 4. 上传覆盖原 MinIO 对象
		_, err = q.minioClient.FPutObject(ctx, bucket, objectName, newLocalPath, minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		})
		if err != nil {
			return 0, fmt.Errorf("upload rewritten parquet failed: %w", err)
		}
		q.logger.Sugar().Infof("Rewrote parquet object %s: removed %d rows for id=%s, %d rows remain",
			objectName, deletedCount, deleteID, remainingCount)

		// 5. 让文件缓存失效（删除本地缓存的旧文件，下次查询会重新下载）
		os.Remove(localPath)
	}

	return deletedCount, nil
}

// removeObjectFromOtherIDIndexes 当 parquet 文件被完全删除时，
// 清理所有其他 ID 在 Redis 中对该文件的引用，避免悬挂引用。
func (q *Querier) removeObjectFromOtherIDIndexes(ctx context.Context, bucket, tableName, objectName, skipID string) {
	if q.redisPool == nil {
		return
	}
	redisClient := q.redisPool.GetClient()
	// 扫描该表所有 ID 索引
	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	keys, err := pool.ScanKeys(ctx, redisClient, pattern)
	if err != nil {
		q.logger.Sugar().Warnf("Failed to scan index keys for cleanup: %v", err)
		return
	}
	for _, key := range keys {
		parts := strings.Split(key, ":")
		if len(parts) >= 6 && parts[0] == "index" && parts[2] == tableName && parts[4] == skipID {
			continue
		}
		// 从集合中移除该 object name 引用
		if err := redisClient.SRem(ctx, key, objectName).Err(); err != nil {
			q.logger.Sugar().Warnf("Failed to remove object ref from key %s: %v", key, err)
		}
	}
}

// prepareTableDataWithDB 使用指定数据库连接为表准备数据
func (q *Querier) prepareTableDataWithDB(db *sql.DB, tableName string) error {
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
		return ErrNoDataFiles
	}

	return q.createTableViewWithDB(db, tableName, allFiles)
}

// =============================================================================
// 统计更新方法
// =============================================================================

// updateQueryStats 更新查询统计信息
func (q *Querier) updateQueryStats(tables []string, queryTime time.Duration, queryType string) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.TotalQueries++
	q.queryStats.TotalQueryTime += queryTime
	q.queryStats.AvgQueryTime = q.queryStats.TotalQueryTime / time.Duration(q.queryStats.TotalQueries)

	if queryTime < q.queryStats.FastestQuery {
		q.queryStats.FastestQuery = queryTime
	}
	if queryTime > q.queryStats.SlowestQuery {
		q.queryStats.SlowestQuery = queryTime
	}

	// 更新表统计
	for _, table := range tables {
		q.queryStats.TablesQueried[table]++
	}

	// 更新查询类型统计
	q.queryStats.QueryTypes[queryType]++
}

// updateCacheHitStats 更新缓存命中统计
func (q *Querier) updateCacheHitStats(tables []string, queryTime time.Duration) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.CacheHits++
	q.queryStats.TotalQueries++

	// 更新表统计
	for _, table := range tables {
		q.queryStats.TablesQueried[table]++
	}
}

// updateCacheMissStats 更新缓存未命中统计
func (q *Querier) updateCacheMissStats() {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.CacheMisses++
}

// updateErrorStats 更新错误统计
func (q *Querier) updateErrorStats() {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.ErrorCount++
}

// updateFileDownloadStats 更新文件下载统计
func (q *Querier) updateFileDownloadStats() {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.FileDownloads++
}

// GetQueryStats 获取查询统计信息
func (q *Querier) GetQueryStats() *QueryStats {
	q.statsLock.RLock()
	defer q.statsLock.RUnlock()

	// 创建副本以避免并发问题
	stats := &QueryStats{
		TotalQueries:   q.queryStats.TotalQueries,
		CacheHits:      q.queryStats.CacheHits,
		CacheMisses:    q.queryStats.CacheMisses,
		AvgQueryTime:   q.queryStats.AvgQueryTime,
		TotalQueryTime: q.queryStats.TotalQueryTime,
		FastestQuery:   q.queryStats.FastestQuery,
		SlowestQuery:   q.queryStats.SlowestQuery,
		ErrorCount:     q.queryStats.ErrorCount,
		FileDownloads:  q.queryStats.FileDownloads,
		FileCacheHits:  q.queryStats.FileCacheHits,
		TablesQueried:  make(map[string]int64),
		QueryTypes:     make(map[string]int64),
	}

	// 复制map
	for k, v := range q.queryStats.TablesQueried {
		stats.TablesQueried[k] = v
	}
	for k, v := range q.queryStats.QueryTypes {
		stats.QueryTypes[k] = v
	}

	return stats
}

// GetCacheStats 获取缓存统计信息
func (q *Querier) GetCacheStats(ctx context.Context) map[string]interface{} {
	stats := make(map[string]interface{})

	// 查询缓存统计
	if q.queryCache != nil {
		stats["query_cache"] = q.queryCache.GetStats(ctx)
	}

	// 文件缓存统计
	if q.fileCache != nil {
		stats["file_cache"] = q.fileCache.GetStats()
	}

	// 整体查询统计
	stats["query_stats"] = q.GetQueryStats()

	return stats
}

// InvalidateCache 失效相关缓存
func (q *Querier) InvalidateCache(ctx context.Context, tables []string) error {
	if q.queryCache != nil {
		return q.queryCache.InvalidateByTables(ctx, tables)
	}
	return nil
}

// prepareTableDataWithCacheAndDB 使用文件缓存准备表数据并使用数据库连接
func (q *Querier) prepareTableDataWithCacheAndDB(ctx context.Context, db *sql.DB, tableName string) error {
	// 1. 获取缓冲区数据文件
	bufferFiles := q.getBufferFilesForTable(tableName)

	// 2. 获取存储的数据文件（使用文件缓存）
	storageFiles, err := q.getStorageFilesWithCache(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get storage files: %w", err)
	}

	// 3. 创建或更新DuckDB视图（使用传入的数据库连接）
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 {
		q.logger.Sugar().Infof("No data files found for table: %s", tableName)
		return nil
	}

	return q.createTableViewWithDB(db, tableName, allFiles)
}

// prepareTableDataWithPruning 使用文件剪枝准备表数据
// 当文件元数据可用时，此方法会使用 QueryOptimizer 进行智能文件剪枝
func (q *Querier) prepareTableDataWithPruning(ctx context.Context, db *sql.DB, tableName string, sqlQuery string) error {
	// 1. 获取缓冲区数据文件
	bufferFiles := q.getBufferFilesForTable(tableName)

	// 2. 获取存储文件的元数据（用于剪枝）
	storageFilesMetadata, err := q.getStorageFilesMetadata(ctx, tableName)
	if err != nil {
		q.logger.Warn("Failed to get file metadata, falling back to no pruning",
			zap.String("table", tableName),
			zap.Error(err))
		// 回退到普通方式
		return q.prepareTableDataWithCacheAndDB(ctx, db, tableName)
	}

	var fileNotFoundCount int
	var otherErrorCount int

	// 3. 使用查询优化器进行文件剪枝
	var storageFiles []string
	if q.queryOptimizer != nil && len(storageFilesMetadata) > 0 {
		optimized, err := q.queryOptimizer.OptimizeQuery(sqlQuery, storageFilesMetadata)
		if err != nil {
			q.logger.Warn("Query optimization failed, using all files",
				zap.Error(err))
			// 使用所有文件
			for _, meta := range storageFilesMetadata {
				localPath, err := q.getFileWithCache(ctx, meta.FilePath)
				if err == nil {
					storageFiles = append(storageFiles, localPath)
				} else if errors.Is(err, ErrFileNotFound) {
					fileNotFoundCount++
				} else {
					otherErrorCount++
				}
			}
		} else {
			// 使用剪枝后的文件
			for _, meta := range optimized.SelectedFiles {
				localPath, err := q.getFileWithCache(ctx, meta.FilePath)
				if err == nil {
					storageFiles = append(storageFiles, localPath)
				} else if errors.Is(err, ErrFileNotFound) {
					fileNotFoundCount++
				} else {
					otherErrorCount++
				}
			}
			if optimized.FilesSkipped > 0 {
				q.logger.Info("File pruning applied",
					zap.String("table", tableName),
					zap.Int("files_skipped", optimized.FilesSkipped),
					zap.Int("files_selected", len(optimized.SelectedFiles)),
					zap.Int64("estimated_rows", optimized.EstimatedRows))
			}
		}
	} else {
		// 无优化器或无元数据，使用所有文件
		for _, meta := range storageFilesMetadata {
			localPath, err := q.getFileWithCache(ctx, meta.FilePath)
			if err == nil {
				storageFiles = append(storageFiles, localPath)
			} else if errors.Is(err, ErrFileNotFound) {
				fileNotFoundCount++
			} else {
				otherErrorCount++
			}
		}
	}

	// 4. 创建或更新DuckDB视图（带列剪枝优化）
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 {
		candidatesCount := len(bufferFiles) + len(storageFilesMetadata)
		if candidatesCount > 0 {
			if otherErrorCount > 0 {
				q.logger.Sugar().Infof("Table %s has %d file(s) but all failed to read (e.g. MinIO auth error)", tableName, candidatesCount)
				return ErrStorageUnavailable
			}
			if fileNotFoundCount > 0 {
				q.logger.Warn("All data files for table are missing from storage (metadata may be stale)",
					zap.String("table", tableName),
					zap.Int("missing_files", fileNotFoundCount))
				return ErrNoDataFiles
			}
			q.logger.Sugar().Infof("No data files found for table: %s", tableName)
			return ErrNoDataFiles
		}
		q.logger.Sugar().Infof("No data files found for table: %s", tableName)
		return ErrNoDataFiles
	}

	// 使用列剪枝优化创建视图
	return q.createTableViewWithDBWithColumnPruning(db, tableName, allFiles, sqlQuery)
}

// getStorageFilesMetadata 获取存储文件的元数据（包括 MinValues/MaxValues 用于文件剪枝）
func (q *Querier) getStorageFilesMetadata(ctx context.Context, tableName string) ([]*storage.FileMetadata, error) {
	var files []*storage.FileMetadata

	// 单节点模式：直接从 MinIO 列出文件
	if q.redisPool == nil {
		return q.getStorageFilesMetadataFromMinIO(ctx, tableName)
	}

	// 分布式模式：从 Redis 索引获取文件列表
	redisClient := q.redisPool.GetClient()

	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	// 使用 SCAN 替代 KEYS，避免阻塞 Redis
	keys, err := pool.ScanKeys(ctx, redisClient, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get file index keys: %w", err)
	}

	for _, key := range keys {
		objectNames, err := redisClient.SMembers(ctx, key).Result()
		if err != nil {
			continue
		}

		for _, objectName := range objectNames {
			// 从 Redis 读取完整的文件元数据
			metadata := q.loadFileMetadataFromRedis(ctx, redisClient, objectName)
			files = append(files, metadata)
		}
	}

	return files, nil
}

// loadFileMetadataFromRedis 从 Redis 加载文件元数据
func (q *Querier) loadFileMetadataFromRedis(ctx context.Context, redisClient redis.Cmdable, objectName string) *storage.FileMetadata {
	metadataKey := fmt.Sprintf("metadata:file:%s", objectName)

	// 创建基础元数据
	metadata := &storage.FileMetadata{
		FilePath:  objectName,
		MinValues: make(map[string]interface{}),
		MaxValues: make(map[string]interface{}),
	}

	// 尝试从 Redis 获取完整元数据
	fields, err := redisClient.HGetAll(ctx, metadataKey).Result()
	if err != nil || len(fields) == 0 {
		// 元数据不存在，返回基础元数据
		return metadata
	}

	// 解析元数据字段
	if fileSizeStr, ok := fields["file_size"]; ok {
		if fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64); err == nil {
			metadata.FileSize = fileSize
		}
	}

	if rowCountStr, ok := fields["row_count"]; ok {
		if rowCount, err := strconv.ParseInt(rowCountStr, 10, 64); err == nil {
			metadata.RowCount = rowCount
		}
	}

	if rowGroupsStr, ok := fields["row_groups"]; ok {
		if rowGroups, err := strconv.Atoi(rowGroupsStr); err == nil {
			metadata.RowGroupCount = rowGroups
		}
	}

	if compression, ok := fields["compression"]; ok {
		metadata.CompressionType = compression
	}

	if createdAtStr, ok := fields["created_at"]; ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			metadata.CreatedAt = createdAt
		}
	}

	// 解析 MinValues
	if minValuesJSON, ok := fields["min_values"]; ok {
		var minValues map[string]interface{}
		if err := json.Unmarshal([]byte(minValuesJSON), &minValues); err == nil {
			metadata.MinValues = q.convertMetadataValues(minValues)
		}
	}

	// 解析 MaxValues
	if maxValuesJSON, ok := fields["max_values"]; ok {
		var maxValues map[string]interface{}
		if err := json.Unmarshal([]byte(maxValuesJSON), &maxValues); err == nil {
			metadata.MaxValues = q.convertMetadataValues(maxValues)
		}
	}

	// 解析 NullCounts
	if nullCountsJSON, ok := fields["null_counts"]; ok {
		var nullCounts map[string]int64
		if err := json.Unmarshal([]byte(nullCountsJSON), &nullCounts); err == nil {
			metadata.NullCounts = nullCounts
		}
	}

	q.logger.Debug("Loaded file metadata from Redis",
		zap.String("object", objectName),
		zap.Int64("row_count", metadata.RowCount),
		zap.Int("min_values_count", len(metadata.MinValues)),
		zap.Int("max_values_count", len(metadata.MaxValues)))

	return metadata
}

// convertMetadataValues 转换元数据值为适当的类型（用于文件剪枝比较）
func (q *Querier) convertMetadataValues(values map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range values {
		switch v := value.(type) {
		case string:
			// 尝试解析为数字
			if intVal, err := strconv.ParseInt(v, 10, 64); err == nil {
				result[key] = intVal
			} else if floatVal, err := strconv.ParseFloat(v, 64); err == nil {
				result[key] = floatVal
			} else {
				result[key] = v
			}
		case float64:
			// JSON 数字默认解析为 float64
			if v == float64(int64(v)) {
				result[key] = int64(v)
			} else {
				result[key] = v
			}
		default:
			result[key] = v
		}
	}
	return result
}

// getStorageFilesMetadataFromMinIO 从 MinIO 获取文件元数据
// 元数据加载优先级：Redis > MinIO sidecar > 基础信息
func (q *Querier) getStorageFilesMetadataFromMinIO(ctx context.Context, tableName string) ([]*storage.FileMetadata, error) {
	if q.minioClient == nil {
		return []*storage.FileMetadata{}, nil
	}

	prefix := tableName + "/"
	bucket := q.config.GetMinIO().Bucket

	// 获取 Redis 客户端（如果可用）
	var redisClient redis.Cmdable
	if q.redisPool != nil {
		redisClient = q.redisPool.GetClient()
	}

	var files []*storage.FileMetadata
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}
	objectCh := q.minioClient.ListObjects(ctx, bucket, opts)

	for object := range objectCh {
		if object.Err != nil {
			continue
		}

		// 跳过非 parquet 文件和 sidecar 元数据文件
		if filepath.Ext(object.Key) != ".parquet" {
			continue
		}
		if strings.HasSuffix(object.Key, ".meta.json") {
			continue
		}

		var metadata *storage.FileMetadata
		var loaded bool

		// 1. 优先尝试从 Redis 加载
		if redisClient != nil {
			metadata = q.loadFileMetadataFromRedis(ctx, redisClient, object.Key)
			if len(metadata.MinValues) > 0 || len(metadata.MaxValues) > 0 {
				loaded = true
			}
		}

		// 2. 回退到 MinIO sidecar
		if !loaded {
			sidecarMetadata := q.loadFileMetadataFromMinIOSidecar(ctx, bucket, object.Key)
			if sidecarMetadata != nil && (len(sidecarMetadata.MinValues) > 0 || len(sidecarMetadata.MaxValues) > 0) {
				metadata = sidecarMetadata
				loaded = true
				// 如果 Redis 可用，缓存到 Redis
				if redisClient != nil {
					go q.cacheMetadataToRedis(context.Background(), redisClient, object.Key, metadata)
				}
			}
		}

		// 3. 如果都没有加载到，使用基础信息
		if metadata == nil {
			metadata = &storage.FileMetadata{
				FilePath:  object.Key,
				MinValues: make(map[string]interface{}),
				MaxValues: make(map[string]interface{}),
			}
		}

		// 补充基础信息
		if metadata.FileSize == 0 {
			metadata.FileSize = object.Size
		}
		if metadata.CreatedAt.IsZero() {
			metadata.CreatedAt = object.LastModified
		}
		if metadata.FilePath == "" {
			metadata.FilePath = object.Key
		}

		files = append(files, metadata)
	}

	return files, nil
}

// loadFileMetadataFromMinIOSidecar 从 MinIO sidecar 文件加载元数据
func (q *Querier) loadFileMetadataFromMinIOSidecar(ctx context.Context, bucket, objectName string) *storage.FileMetadata {
	if q.minioClient == nil {
		return nil
	}

	metaObjectName := objectName + ".meta.json"

	// 尝试获取 sidecar 文件（GetObject 返回 []byte）
	data, err := q.minioClient.GetObject(ctx, bucket, metaObjectName, minio.GetObjectOptions{})
	if err != nil {
		// sidecar 文件不存在或获取失败
		return nil
	}

	if len(data) == 0 {
		return nil
	}

	var metadata storage.FileMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		q.logger.Debug("Failed to parse sidecar metadata",
			zap.String("object", metaObjectName),
			zap.Error(err))
		return nil
	}

	// 转换值类型
	metadata.MinValues = q.convertMetadataValues(metadata.MinValues)
	metadata.MaxValues = q.convertMetadataValues(metadata.MaxValues)

	q.logger.Debug("Loaded metadata from MinIO sidecar",
		zap.String("object", objectName),
		zap.Int64("row_count", metadata.RowCount),
		zap.Int("min_values_count", len(metadata.MinValues)))

	return &metadata
}

// cacheMetadataToRedis 将元数据缓存到 Redis（异步）
func (q *Querier) cacheMetadataToRedis(ctx context.Context, redisClient redis.Cmdable, objectName string, metadata *storage.FileMetadata) {
	metadataKey := fmt.Sprintf("metadata:file:%s", objectName)

	minValuesJSON, _ := json.Marshal(metadata.MinValues)
	maxValuesJSON, _ := json.Marshal(metadata.MaxValues)
	nullCountsJSON, _ := json.Marshal(metadata.NullCounts)

	fields := map[string]interface{}{
		"file_path":   objectName,
		"file_size":   metadata.FileSize,
		"row_count":   metadata.RowCount,
		"row_groups":  metadata.RowGroupCount,
		"min_values":  string(minValuesJSON),
		"max_values":  string(maxValuesJSON),
		"null_counts": string(nullCountsJSON),
		"compression": metadata.CompressionType,
		"created_at":  metadata.CreatedAt.Format(time.RFC3339),
	}

	if err := redisClient.HSet(ctx, metadataKey, fields).Err(); err != nil {
		q.logger.Debug("Failed to cache metadata to Redis",
			zap.String("object", objectName),
			zap.Error(err))
		return
	}

	// 设置 30 天过期
	redisClient.Expire(ctx, metadataKey, 30*24*time.Hour)
}
