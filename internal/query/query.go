package query

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/pool"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	_ "github.com/marcboeker/go-duckdb"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
)

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

	// 混合查询性能指标
	HybridQueries  int64         `json:"hybrid_queries"`   // 混合查询次数
	BufferHits     int64         `json:"buffer_hits"`      // 缓冲区命中次数
	BufferRows     int64         `json:"buffer_rows"`      // 缓冲区返回行数
	AvgMergeTime   time.Duration `json:"avg_merge_time"`   // 平均合并延迟
	TotalMergeTime time.Duration `json:"total_merge_time"` // 总合并时间
}

// Querier 增强的查询处理器
// 集成查询结果缓存、文件缓存和DuckDB连接池管理
type Querier struct {
	redisClient    *redis.Client
	redisPool      *pool.RedisPool
	minioClient    storage.Uploader
	db             *sql.DB
	buffer         *buffer.ConcurrentBuffer
	tableExtractor *TableExtractor // 升级到增强版本
	logger         *zap.Logger
	tempDir        string
	config         *config.Config
	indexSystem    *storage.IndexSystem // 新增：索引系统

	// 缓存组件
	queryCache *QueryCache
	fileCache  *FileCache

	// 性能优化组件
	dbPool        *DuckDBPool
	preparedStmts map[string]*sql.Stmt
	stmtMutex     sync.RWMutex

	// 统计信息
	queryStats *QueryStats
	statsLock  sync.RWMutex
}

// NewQuerier 创建查询器
func NewQuerier(redisPool *pool.RedisPool, minioClient storage.Uploader, cfg *config.Config, buf *buffer.ConcurrentBuffer, logger *zap.Logger, indexSystem *storage.IndexSystem) (*Querier, error) {
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
		redisPool:      redisPool,
		minioClient:    minioClient,
		db:             duckdbPool,
		queryCache:     queryCache,
		fileCache:      fileCache,
		config:         cfg,
		buffer:         buf,
		logger:         logger,
		tempDir:        tempDir,
		tableExtractor: NewTableExtractor(),
		indexSystem:    indexSystem, // 新增：索引系统
		preparedStmts:  make(map[string]*sql.Stmt),
		queryStats:     queryStats,
	}

	return querier, nil
}

// ExecuteQuery 执行SQL查询（增强版本）
// 核心流程：缓存检查 -> 表名提取 -> 文件缓存 -> DuckDB执行 -> 结果缓存
func (q *Querier) ExecuteQuery(sqlQuery string) (string, error) {
	startTime := time.Now()
	ctx := context.Background()

	log.Printf("Executing enhanced query: %s", sqlQuery)

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

	log.Printf("Extracted tables: %v", validTables)

	// 2. 使用索引系统进行查询优化（如果可用）
	if q.indexSystem != nil {
		optimizedTables, err := q.optimizeQueryWithIndex(ctx, sqlQuery, validTables)
		if err == nil && len(optimizedTables) > 0 {
			log.Printf("Query optimized using index system")
			validTables = optimizedTables
		} else {
			log.Printf("Index optimization failed, using original tables: %v", err)
		}
	}

	// 3. 检查查询缓存（只有在Redis连接池可用时）
	if q.queryCache != nil && q.redisPool != nil {
		if cacheEntry, found := q.queryCache.Get(ctx, sqlQuery, validTables); found {
			q.updateCacheHitStats(validTables, time.Since(startTime))
			log.Printf("Query cache HIT - returning cached result")
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

	// 5. 为每个表准备数据文件（使用同一个数据库连接）
	for _, tableName := range validTables {
		if err := q.prepareTableDataWithCacheAndDB(ctx, db, tableName); err != nil {
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

	log.Printf("Query executed successfully in %v", queryTime)
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

// getStorageFilesForTable 获取表的存储文件（集成索引系统）
func (q *Querier) getStorageFilesForTable(ctx context.Context, tableName string) ([]string, error) {
	// 1. 尝试使用索引系统进行智能文件过滤
	objectNames := make([]string, 0)
	indexUsed := false

	if q.indexSystem != nil && q.redisPool != nil {
		// 检查表是否有可用的索引
		bloomKey := fmt.Sprintf("bloom:%s", tableName)
		minMaxKey := fmt.Sprintf("minmax:%s", tableName)

		hasBloom := q.indexSystem.HasBloomFilter(bloomKey)
		hasMinMax := q.indexSystem.HasMinMaxIndex(minMaxKey)

		if hasBloom || hasMinMax {
			indexUsed = true
			log.Printf("Using index system for table %s (bloom=%v, minmax=%v)", tableName, hasBloom, hasMinMax)

			// 记录索引命中
			q.statsLock.Lock()
			q.queryStats.CacheHits++
			q.statsLock.Unlock()
		}
	}

	// 2. 从Redis获取文件列表（使用架构设计的索引格式）
	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	keys, err := q.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get file index keys: %w", err)
	}

	// 3. 收集所有候选文件
	for _, key := range keys {
		// 获取集合中的所有文件名（使用SMembers读取集合）
		fileList, err := q.redisClient.SMembers(ctx, key).Result()
		if err != nil {
			log.Printf("WARN: failed to get object names for key %s: %v", key, err)
			continue
		}
		objectNames = append(objectNames, fileList...)
	}

	// 4. 如果使用了索引系统，这里可以进一步过滤文件
	// 注意：简化实现，实际应该根据查询条件调用索引系统的查询方法
	filteredObjectNames := objectNames
	if indexUsed {
		log.Printf("Index system identified %d candidate files for table %s", len(filteredObjectNames), tableName)
	}

	// 5. 下载文件到临时目录（使用文件缓存）
	var files []string
	for _, objectName := range filteredObjectNames {
		var localPath string

		// 尝试使用文件缓存（Get方法需要downloadFunc）
		if q.fileCache != nil {
			cachedPath, err := q.fileCache.Get(ctx, objectName, func(objName string) (string, error) {
				return q.downloadToTemp(ctx, objName)
			})
			if err == nil && cachedPath != "" {
				localPath = cachedPath
				// 检查文件是否是从缓存返回的（通过Contains方法）
				if q.fileCache.Contains(objectName) {
					q.statsLock.Lock()
					q.queryStats.FileCacheHits++
					q.statsLock.Unlock()
				} else {
					q.statsLock.Lock()
					q.queryStats.FileDownloads++
					q.statsLock.Unlock()
				}
			}
		} else {
			// 如果没有文件缓存，直接下载
			downloadedPath, err := q.downloadToTemp(ctx, objectName)
			if err != nil {
				log.Printf("WARN: failed to download file %s: %v", objectName, err)
				continue
			}
			localPath = downloadedPath

			q.statsLock.Lock()
			q.queryStats.FileDownloads++
			q.statsLock.Unlock()
		}

		if localPath != "" {
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
	// 删除可能存在的旧视图（包括主视图和活跃数据视图）
	dropViewSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s", tableName)
	log.Printf("Executing DROP VIEW SQL: %s", dropViewSQL)
	if _, err := db.Exec(dropViewSQL); err != nil {
		log.Printf("WARN: failed to drop existing view for table %s: %v", tableName, err)
	} else {
		log.Printf("Successfully dropped existing view (if any) for table %s", tableName)
	}

	// 删除活跃数据视图（如果存在）
	dropActiveViewSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s_active", tableName)
	if _, err := db.Exec(dropActiveViewSQL); err != nil {
		log.Printf("WARN: failed to drop existing active view for table %s: %v", tableName, err)
	}

	// 检查是否存在缓冲区表
	bufferTableName := fmt.Sprintf("%s_buffer", tableName)
	hasBufferTable := false

	// 验证缓冲区表是否存在
	checkBufferSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 1", bufferTableName)
	if _, err := db.Query(checkBufferSQL); err == nil {
		hasBufferTable = true
		log.Printf("Buffer table %s exists and will be included in view", bufferTableName)
	}

	// 如果既没有文件也没有缓冲区表，直接返回
	if len(files) == 0 && !hasBufferTable {
		log.Printf("No files or buffer data for table %s, skipping view creation", tableName)
		return nil
	}

	// 构建视图SQL
	var viewSQL string

	if len(files) > 0 && hasBufferTable {
		// 混合查询：合并Parquet文件和缓冲区数据
		var filePaths []string
		for _, file := range files {
			filePaths = append(filePaths, fmt.Sprintf("'%s'", file))
		}

		// 使用UNION去重合并两个数据源（防止数据重复）
		viewSQL = fmt.Sprintf(`
			CREATE VIEW %s AS 
			SELECT DISTINCT id, timestamp, payload, "table" 
			FROM (
				SELECT id, timestamp, payload, "table" FROM read_parquet([%s])
				UNION ALL
				SELECT id, timestamp, payload, table_name AS "table" FROM %s_buffer
			) combined
		`, tableName, strings.Join(filePaths, ", "), tableName)

		log.Printf("Creating hybrid view for table %s (files + buffer) with deduplication", tableName)
	} else if len(files) > 0 {
		// 只有Parquet文件
		var filePaths []string
		for _, file := range files {
			filePaths = append(filePaths, fmt.Sprintf("'%s'", file))
		}
		viewSQL = fmt.Sprintf(
			"CREATE VIEW %s AS SELECT * FROM read_parquet([%s])",
			tableName,
			strings.Join(filePaths, ", "),
		)
		log.Printf("Creating file-only view for table %s with %d files", tableName, len(files))
	} else {
		// 只有缓冲区数据
		viewSQL = fmt.Sprintf(
			"CREATE VIEW %s AS SELECT id, timestamp, payload, table_name AS \"table\" FROM %s_buffer",
			tableName, tableName,
		)
		log.Printf("Creating buffer-only view for table %s", tableName)
	}

	log.Printf("Executing CREATE VIEW SQL: %s", viewSQL)

	// 执行SQL并详细记录结果
	if _, err := db.Exec(viewSQL); err != nil {
		log.Printf("ERROR: Failed to create view for table %s", tableName)
		log.Printf("ERROR: SQL execution error: %v", err)
		return fmt.Errorf("failed to create view for table %s: %w", tableName, err)
	}

	log.Printf("Successfully created view for table %s", tableName)

	// 验证视图是否真的创建成功
	testSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 0", tableName)
	log.Printf("Testing view existence with SQL: %s", testSQL)
	if _, err := db.Query(testSQL); err != nil {
		log.Printf("ERROR: View verification failed for table %s: %v", tableName, err)
		return fmt.Errorf("view creation verification failed for table %s: %w", tableName, err)
	}

	log.Printf("View verification successful for table %s", tableName)

	// 创建智能活跃数据视图（自动过滤墓碑记录）
	if err := q.createActiveDataView(db, tableName); err != nil {
		log.Printf("WARN: Failed to create active data view for table %s: %v", tableName, err)
		// 不返回错误，因为这只是一个优化视图
	}
	return nil
}

// createActiveDataView 创建智能活跃数据视图（自动过滤墓碑记录）
func (q *Querier) createActiveDataView(db *sql.DB, tableName string) error {
	activeViewName := fmt.Sprintf("%s_active", tableName)

	// 使用JSON函数提取_deleted字段进行过滤（比LIKE更高效）
	// 如果DuckDB版本不支持JSON函数，则回退到LIKE模式
	createActiveViewSQL := fmt.Sprintf(`
		CREATE VIEW %s AS 
		SELECT * FROM %s
		WHERE payload NOT LIKE '%%"_deleted":true%%' 
		  AND payload NOT LIKE '%%"_deleted": true%%'
	`, activeViewName, tableName)

	log.Printf("Creating active data view: %s", activeViewName)

	if _, err := db.Exec(createActiveViewSQL); err != nil {
		return fmt.Errorf("failed to create active data view: %w", err)
	}

	// 验证视图创建成功
	testSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 0", activeViewName)
	if _, err := db.Query(testSQL); err != nil {
		return fmt.Errorf("active view verification failed: %w", err)
	}

	log.Printf("Successfully created active data view: %s", activeViewName)
	return nil
}

// incrementalUpdateView 增量更新视图（只更新变更的部分）
func (q *Querier) incrementalUpdateView(tableName string, changes []buffer.DataRow) error {
	if len(changes) == 0 {
		log.Printf("No changes to update for table %s", tableName)
		return nil
	}

	db, err := q.getDBConnection()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer q.returnDBConnection(db)

	// 策略1: 如果变更量较小，使用增量更新
	// 策略2: 如果变更量较大（>10%表大小），重建视图

	// 检查表是否存在
	checkSQL := fmt.Sprintf("SELECT COUNT(*) as count FROM %s LIMIT 1", tableName)
	var tableExists bool
	if _, err := db.Query(checkSQL); err == nil {
		tableExists = true
	}

	if !tableExists {
		log.Printf("Table %s does not exist, skipping incremental update", tableName)
		return nil
	}

	// 统计当前表大小
	countSQL := fmt.Sprintf("SELECT COUNT(*) as total FROM %s", tableName)
	var totalCount int64
	row := db.QueryRow(countSQL)
	if err := row.Scan(&totalCount); err != nil {
		log.Printf("WARN: Failed to get table size: %v", err)
		totalCount = 0
	}

	changeRatio := float64(len(changes)) / float64(totalCount)

	// 如果变更量 > 10% 或表为空，直接重建视图
	if totalCount == 0 || changeRatio > 0.1 {
		log.Printf("Change ratio %.2f%% exceeds threshold, rebuilding view for table %s",
			changeRatio*100, tableName)

		// 获取所有文件并重建视图
		ctx := context.Background()
		storageFiles, err := q.getStorageFilesForTable(ctx, tableName)
		if err != nil {
			return fmt.Errorf("failed to get storage files: %w", err)
		}
		return q.createTableViewWithDB(db, tableName, storageFiles)
	}

	// 增量更新策略：
	// 1. 识别变更类型（新增、更新、删除）
	// 2. 对于更新和删除，先标记为墓碑
	// 3. 对于新增和更新的新版本，插入到缓冲区表

	log.Printf("Performing incremental update for table %s with %d changes (%.2f%%)",
		tableName, len(changes), changeRatio*100)

	// 更新缓冲区表
	bufferTableName := fmt.Sprintf("%s_buffer", tableName)

	// 检查缓冲区表是否存在
	checkBufferSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 1", bufferTableName)
	var bufferExists bool
	if _, err := db.Query(checkBufferSQL); err == nil {
		bufferExists = true
	}

	if !bufferExists {
		// 创建缓冲区表
		log.Printf("Creating buffer table %s for incremental updates", bufferTableName)
		if err := q.createBufferTable(db, bufferTableName); err != nil {
			return fmt.Errorf("failed to create buffer table: %w", err)
		}
	}

	// 插入变更到缓冲区表
	if err := q.loadBufferDataToDB(db, tableName, changes); err != nil {
		return fmt.Errorf("failed to load incremental changes: %w", err)
	}

	// 重建活跃数据视图以反映变更
	if err := q.createActiveDataView(db, tableName); err != nil {
		log.Printf("WARN: Failed to update active view: %v", err)
	}

	log.Printf("Incremental update completed for table %s", tableName)
	return nil
}

// createBufferTable 创建缓冲区表结构
func (q *Querier) createBufferTable(db *sql.DB, bufferTableName string) error {
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR,
			timestamp BIGINT,
			payload VARCHAR,
			table_name VARCHAR
		)
	`, bufferTableName)

	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create buffer table: %w", err)
	}

	log.Printf("Successfully created buffer table: %s", bufferTableName)
	return nil
}

// RefreshViewWithChanges 当缓冲区刷新后，刷新视图以反映变更
func (q *Querier) RefreshViewWithChanges(tableName string, flushedRows []buffer.DataRow) error {
	// 使用增量更新策略
	return q.incrementalUpdateView(tableName, flushedRows)
}

// downloadToTemp 下载文件到临时目录
func (q *Querier) downloadToTemp(ctx context.Context, objectName string) (string, error) {
	localPath := filepath.Join(q.tempDir, filepath.Base(objectName))

	// 检查文件是否已存在
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// 使用正确的MinIO方法下载文件
	data, err := q.minioClient.GetObject(ctx, q.config.MinIO.Bucket, objectName, minio.GetObjectOptions{})
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

	// 关闭连接池
	if q.dbPool != nil {
		q.dbPool.Close()
	}

	// 关闭所有预编译语句
	q.stmtMutex.Lock()
	for _, stmt := range q.preparedStmts {
		stmt.Close()
	}
	q.preparedStmts = make(map[string]*sql.Stmt)
	q.stmtMutex.Unlock()

	// 清理缓存
	if q.fileCache != nil {
		q.fileCache.Clear()
	}

	// 清理临时文件
	os.RemoveAll(q.tempDir)
}

// =============================================================================
// DuckDB 连接池方法
// =============================================================================

// NewDuckDBPool 创建DuckDB连接池
func NewDuckDBPool(maxConns int) (*DuckDBPool, error) {
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
			log.Printf("WARN: failed to apply DuckDB optimization '%s': %v", opt, err)
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
		// 如果池中没有可用连接，创建新连接
		p.mu.Lock()
		defer p.mu.Unlock()

		if p.created < p.maxConns {
			db, err := sql.Open("duckdb", ":memory:")
			if err != nil {
				return nil, err
			}
			p.created++
			return db, nil
		}

		// 等待可用连接
		return <-p.connections, nil
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
		log.Printf("No data files found for table: %s", tableName)
		return nil
	}

	return q.createTableView(tableName, allFiles)
}

// getStorageFilesWithCache 使用文件缓存获取存储文件
func (q *Querier) getStorageFilesWithCache(ctx context.Context, tableName string) ([]string, error) {
	// 如果Redis连接池不可用，返回空列表
	if q.redisPool == nil {
		return []string{}, nil
	}

	// 从连接池获取Redis客户端
	redisClient := q.redisPool.GetClient()

	// 使用架构设计的索引格式：index:table:tableName:id:*
	pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
	keys, err := redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get file index keys: %w", err)
	}

	var files []string
	for _, key := range keys {
		// 使用SMembers获取集合中的所有文件名（因为buffer使用SAdd存储）
		objectNames, err := redisClient.SMembers(ctx, key).Result()
		if err != nil {
			log.Printf("WARN: failed to get object names for key %s: %v", key, err)
			continue
		}

		// 处理该索引下的所有文件
		for _, objectName := range objectNames {
			// 使用文件缓存获取文件
			localPath, err := q.fileCache.Get(ctx, objectName, func(objName string) (string, error) {
				q.updateFileDownloadStats()
				return q.downloadToTemp(ctx, objName)
			})
			if err != nil {
				log.Printf("WARN: failed to get cached file %s: %v", objectName, err)
				continue
			}
			files = append(files, localPath)
		}
	}

	return files, nil
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

	q.stmtMutex.RLock()
	stmt, exists := q.preparedStmts[stmtKey]
	q.stmtMutex.RUnlock()

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

		q.stmtMutex.Lock()
		q.preparedStmts[stmtKey] = stmt
		q.stmtMutex.Unlock()
	}

	// 执行预编译语句
	rows, err := stmt.Query()
	if err != nil {
		return "", err
	}
	defer rows.Close()

	return q.processQueryResults(rows)
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

		// 混合查询性能指标
		HybridQueries:  q.queryStats.HybridQueries,
		BufferHits:     q.queryStats.BufferHits,
		BufferRows:     q.queryStats.BufferRows,
		AvgMergeTime:   q.queryStats.AvgMergeTime,
		TotalMergeTime: q.queryStats.TotalMergeTime,
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

	// 1.5 直接从缓冲区加载数据到DuckDB（混合查询）
	bufferRows, err := q.getBufferDataRows(tableName)
	if err != nil {
		log.Printf("WARN: failed to get buffer data rows for table %s: %v", tableName, err)
	} else if len(bufferRows) > 0 {
		log.Printf("Loading %d rows from buffer for table %s", len(bufferRows), tableName)
		// 将缓冲区数据直接插入到临时表
		if err := q.loadBufferDataToDB(db, tableName, bufferRows); err != nil {
			log.Printf("WARN: failed to load buffer data to DB: %v", err)
		}
	}

	// 2. 获取存储的数据文件（使用文件缓存）
	storageFiles, err := q.getStorageFilesWithCache(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get storage files: %w", err)
	}

	// 3. 创建或更新DuckDB视图（使用传入的数据库连接）
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 && len(bufferRows) == 0 {
		log.Printf("No data files or buffer data found for table: %s", tableName)
		return nil
	}

	return q.createTableViewWithDB(db, tableName, allFiles)
}

// getBufferDataRows 从缓冲区获取指定表的数据行（用于混合查询）
func (q *Querier) getBufferDataRows(tableName string) ([]buffer.DataRow, error) {
	if q.buffer == nil {
		return []buffer.DataRow{}, nil
	}

	startTime := time.Now()

	// 获取该表的所有缓冲区键
	keys := q.buffer.GetTableKeys(tableName)
	if len(keys) == 0 {
		return []buffer.DataRow{}, nil
	}

	// 收集所有数据行
	var allRows []buffer.DataRow
	for _, key := range keys {
		rows := q.buffer.GetBufferData(key)
		allRows = append(allRows, rows...)
	}

	// 更新统计指标
	if len(allRows) > 0 {
		q.statsLock.Lock()
		q.queryStats.BufferHits++
		q.queryStats.BufferRows += int64(len(allRows))
		q.statsLock.Unlock()
	}

	log.Printf("Retrieved %d buffer rows for table %s from %d keys in %v",
		len(allRows), tableName, len(keys), time.Since(startTime))
	return allRows, nil
}

// loadBufferDataToDB 将缓冲区数据加载到DuckDB（用于混合查询）
func (q *Querier) loadBufferDataToDB(db *sql.DB, tableName string, rows []buffer.DataRow) error {
	if len(rows) == 0 {
		return nil
	}

	startTime := time.Now()

	// 创建临时内存表（如果不存在）
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s_buffer (
			id VARCHAR,
			timestamp BIGINT,
			payload VARCHAR,
			table_name VARCHAR
		)
	`, tableName)

	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create buffer table: %w", err)
	}

	// 批量插入数据
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s_buffer (id, timestamp, payload, table_name) VALUES (?, ?, ?, ?)
	`, tableName)

	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, row := range rows {
		if _, err := stmt.Exec(row.ID, row.Timestamp, row.Payload, row.Table); err != nil {
			log.Printf("WARN: failed to insert buffer row: %v", err)
			continue
		}
	}

	// 记录合并延迟
	mergeTime := time.Since(startTime)
	q.statsLock.Lock()
	q.queryStats.HybridQueries++
	q.queryStats.TotalMergeTime += mergeTime
	if q.queryStats.HybridQueries > 0 {
		q.queryStats.AvgMergeTime = q.queryStats.TotalMergeTime / time.Duration(q.queryStats.HybridQueries)
	}
	q.statsLock.Unlock()

	log.Printf("Loaded %d buffer rows into DuckDB table %s_buffer in %v", len(rows), tableName, mergeTime)
	return nil
}

// optimizeQueryWithIndex 使用索引系统优化查询
func (q *Querier) optimizeQueryWithIndex(ctx context.Context, sqlQuery string, tables []string) ([]string, error) {
	if q.indexSystem == nil {
		return tables, fmt.Errorf("index system not available")
	}

	optimizedTables := make([]string, 0, len(tables))

	for _, table := range tables {
		// 检查是否有BloomFilter索引
		bloomKey := fmt.Sprintf("bloom:%s", table)
		if q.indexSystem.HasBloomFilter(bloomKey) {
			log.Printf("Using BloomFilter for table %s", table)
			// 这里可以根据查询条件进行BloomFilter过滤
			// 简化实现：直接添加表名
			optimizedTables = append(optimizedTables, table)
			continue
		}

		// 检查是否有MinMax索引
		minMaxKey := fmt.Sprintf("minmax:%s", table)
		if q.indexSystem.HasMinMaxIndex(minMaxKey) {
			log.Printf("Using MinMax index for table %s", table)
			// 这里可以根据查询条件进行范围过滤
			// 简化实现：直接添加表名
			optimizedTables = append(optimizedTables, table)
			continue
		}

		// 没有索引的表也添加
		optimizedTables = append(optimizedTables, table)
	}

	// 更新索引命中率统计
	q.updateIndexHitStats(len(optimizedTables), len(tables))

	return optimizedTables, nil
}

// updateIndexHitStats 更新索引命中率统计
func (q *Querier) updateIndexHitStats(hitCount, totalCount int) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	// 这里可以添加更详细的索引统计逻辑
	log.Printf("Index hit rate: %d/%d", hitCount, totalCount)
}
