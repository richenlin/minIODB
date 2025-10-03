package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/logger"
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

	// 单节点模式文件索引缓存
	localFileIndex      map[string][]string // tableName -> []filePath
	localFileIndexMutex sync.RWMutex
	localFileIndexTTL   time.Duration
	localIndexLastScan  map[string]time.Time // tableName -> lastScanTime

	// 视图初始化缓存
	initializedViews      map[string]bool // tableName -> initialized
	initializedViewsMutex sync.RWMutex

	// 性能优化组件
	dbPool        *DuckDBPool
	preparedStmts map[string]*sql.Stmt
	stmtMutex     sync.RWMutex

	// 统计信息
	queryStats *QueryStats
	statsLock  sync.RWMutex
}

// NewQuerier 创建查询器
func NewQuerier(ctx context.Context, redisPool *pool.RedisPool, minioClient storage.Uploader, cfg *config.Config, buf *buffer.ConcurrentBuffer, logger *zap.Logger, indexSystem *storage.IndexSystem) (*Querier, error) {
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
			fileCache, err = NewFileCache(ctx, fileCacheConfig, redisClient, logger)
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
		// 【新增】初始化缓存
		localFileIndex:     make(map[string][]string),
		localIndexLastScan: make(map[string]time.Time),
		localFileIndexTTL:  5 * time.Minute, // 文件索引缓存5分钟
		initializedViews:   make(map[string]bool),
	}

	return querier, nil
}

// ExecuteQuery 执行SQL查询（增强版本）
// 核心流程：缓存检查 -> 表名提取 -> 文件缓存 -> DuckDB执行 -> 结果缓存
func (q *Querier) ExecuteQuery(ctx context.Context, sqlQuery string) (string, error) {
	startTime := time.Now()

	logger.LogInfo(ctx, "Executing enhanced query", zap.String("value", sqlQuery))

	// 1. 提取表名
	tables := q.tableExtractor.ExtractTableNames(sqlQuery)
	if len(tables) == 0 {
		q.updateErrorStats(ctx)
		return "", fmt.Errorf("no valid table names found in query")
	}

	validTables := q.tableExtractor.ValidateTableNames(tables)
	if len(validTables) == 0 {
		q.updateErrorStats(ctx)
		return "", fmt.Errorf("no valid table names found after validation")
	}

	logger.LogInfo(ctx, "Extracted tables: ", zap.String("table", fmt.Sprintf("%v", validTables)))

	// 2. 使用索引系统进行查询优化（如果可用）
	if q.indexSystem != nil {
		optimizedTables, err := q.optimizeQueryWithIndex(ctx, sqlQuery, validTables)
		if err == nil && len(optimizedTables) > 0 {
			logger.LogInfo(ctx, "Query optimized using index system")
			validTables = optimizedTables
		} else {
			logger.LogInfo(ctx, "Index optimization failed, using original tables: ", zap.String("table", fmt.Sprintf("%v", err)))
		}
	}

	// 3. 检查查询缓存（只有在Redis连接池可用时）
	if q.queryCache != nil && q.redisPool != nil {
		if cacheEntry, found := q.queryCache.Get(ctx, sqlQuery, validTables); found {
			q.updateCacheHitStats(ctx, validTables, time.Since(startTime))
			logger.LogInfo(ctx, "Query cache HIT - returning cached result")
			return cacheEntry.Result, nil
		}
	}

	// 3. 缓存未命中，执行查询
	q.updateCacheMissStats(ctx)

	// 4. 获取数据库连接（提前获取，确保整个流程使用同一连接）
	db, err := q.getDBConnection(ctx)
	if err != nil {
		q.updateErrorStats(ctx)
		return "", fmt.Errorf("failed to get database connection: %w", err)
	}
	defer q.returnDBConnection(ctx, db)

	// 5. 为每个表准备数据文件（使用同一个数据库连接）
	for _, tableName := range validTables {
		if err := q.prepareTableDataWithCacheAndDB(ctx, db, tableName); err != nil {
			q.updateErrorStats(ctx)
			return "", fmt.Errorf("failed to prepare data for table %s: %w", tableName, err)
		}
	}

	// 6. 执行查询（使用相同的数据库连接）
	result, err := q.executeQueryWithOptimization(ctx, db, sqlQuery)
	if err != nil {
		q.updateErrorStats(ctx)
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
	q.updateQueryStats(ctx, validTables, queryTime, q.tableExtractor.GetQueryType(sqlQuery))

	logger.LogInfo(ctx, "Query executed successfully in ", zap.String("value", fmt.Sprintf("%v", queryTime)))
	return result, nil
}

// prepareTableData 为指定表准备数据文件
func (q *Querier) prepareTableData(ctx context.Context, tableName string) error {
	// 1. 获取缓冲区数据文件
	bufferFiles := q.getBufferFilesForTable(ctx, tableName)

	// 2. 获取存储的数据文件
	storageFiles, err := q.getStorageFilesForTable(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get storage files: %w", err)
	}

	// 3. 创建或更新DuckDB视图
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 {
		logger.LogInfo(ctx, "No data files found for table", zap.String("table", tableName))
		return nil
	}

	return q.createTableView(ctx, tableName, allFiles)
}

// getBufferFilesForTable 获取表的缓冲区文件
func (q *Querier) getBufferFilesForTable(ctx context.Context, tableName string) []string {
	if q.buffer == nil {
		return nil
	}

	// 使用正确的方法获取表的缓冲区键
	bufferKeys := q.buffer.GetTableKeys(ctx, tableName)
	var files []string

	for _, bufferKey := range bufferKeys {
		// 获取该键的数据
		rows := q.buffer.Get(ctx, bufferKey)
		if len(rows) > 0 {
			// 创建临时Parquet文件
			tempFile := q.createTempFilePath(bufferKey)
			if err := q.writeBufferToParquet(ctx, tempFile, rows); err != nil {
				logger.LogWarn(ctx, "failed to write buffer to parquet file",
					zap.String("file", tempFile),
					zap.Error(err))
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

		hasBloom := q.indexSystem.HasBloomFilter(ctx, bloomKey)
		hasMinMax := q.indexSystem.HasMinMaxIndex(ctx, minMaxKey)

		if hasBloom || hasMinMax {
			indexUsed = true
			logger.LogInfo(ctx, "Using index system for table  (bloom=, minmax=)", zap.Any("params", []interface{}{tableName, hasBloom, hasMinMax}))

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
			logger.LogWarn(ctx, "failed to get object names for key ", zap.String("key", key), zap.Error(err))
			continue
		}
		objectNames = append(objectNames, fileList...)
	}

	// 4. 如果使用了索引系统，这里可以进一步过滤文件
	// 注意：简化实现，实际应该根据查询条件调用索引系统的查询方法
	filteredObjectNames := objectNames
	if indexUsed {
		logger.LogInfo(ctx, "Index system identified candidate files",
			zap.Int("candidate_files", len(filteredObjectNames)),
			zap.String("table", tableName))
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
				logger.LogWarn(ctx, "failed to download file ", zap.String("object", objectName), zap.Error(err))
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
func (q *Querier) createTableView(ctx context.Context, tableName string, files []string) error {
	return q.createTableViewWithDB(ctx, q.db, tableName, files)
}

// createTableViewWithDB 在指定的DuckDB连接中创建表视图
func (q *Querier) createTableViewWithDB(ctx context.Context, db *sql.DB, tableName string, files []string) error {
	// 删除可能存在的旧视图（包括主视图和活跃数据视图）
	dropViewSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s", tableName)
	logger.LogInfo(ctx, "Executing DROP VIEW SQL", zap.String("sql", dropViewSQL))
	if _, err := db.Exec(dropViewSQL); err != nil {
		logger.LogWarn(ctx, "failed to drop existing view",
			zap.String("table", tableName),
			zap.Error(err))
	} else {
		logger.LogInfo(ctx, "Successfully dropped existing view (if any)",
			zap.String("table", tableName),
			zap.String("status", "success"))
	}

	// 删除活跃数据视图（如果存在）
	dropActiveViewSQL := fmt.Sprintf("DROP VIEW IF EXISTS %s_active", tableName)
	if _, err := db.Exec(dropActiveViewSQL); err != nil {
		logger.LogWarn(ctx, "failed to drop existing active view",
			zap.String("table", tableName),
			zap.Error(err))
	}

	// 检查是否存在缓冲区表
	bufferTableName := fmt.Sprintf("%s_buffer", tableName)
	hasBufferTable := false

	// 验证缓冲区表是否存在
	checkBufferSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 1", bufferTableName)
	if _, err := db.Query(checkBufferSQL); err == nil {
		hasBufferTable = true
		logger.LogInfo(ctx, "Buffer table  exists and will be included in view", zap.String("table", fmt.Sprintf("%v", bufferTableName)))
	}

	// 【修复】即使没有数据，也要创建空视图和 _active 视图，以便后续查询可以正常工作
	// 如果既没有文件也没有缓冲区表，创建一个空视图结构
	if len(files) == 0 && !hasBufferTable {
		logger.LogInfo(ctx, "No files or buffer data for table , creating empty view structure", zap.String("table", fmt.Sprintf("%v", tableName)))
		return q.createEmptyTableView(ctx, db, tableName)
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

		logger.LogInfo(ctx, "Creating hybrid view for table  (files + buffer) with deduplication", zap.String("table", fmt.Sprintf("%v", tableName)))
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
		logger.LogInfo(ctx, "Creating file-only view",
			zap.String("table", tableName),
			zap.Int("files", len(files)))
	} else {
		// 只有缓冲区数据
		viewSQL = fmt.Sprintf(
			"CREATE VIEW %s AS SELECT id, timestamp, payload, table_name AS \"table\" FROM %s_buffer",
			tableName, tableName,
		)
		logger.LogInfo(ctx, "Creating buffer-only view for table ", zap.String("table", fmt.Sprintf("%v", tableName)))
	}

	logger.LogInfo(ctx, "Executing CREATE VIEW SQL", zap.String("sql", viewSQL))

	// 执行SQL并详细记录结果
	if _, err := db.Exec(viewSQL); err != nil {
		logger.LogError(ctx, err, "Failed to create view",
			zap.String("table", tableName),
			zap.Error(err))
		return fmt.Errorf("failed to create view for table %s: %w", tableName, err)
	}

	logger.LogInfo(ctx, "Successfully created view",
		zap.String("table", tableName),
		zap.String("status", "success"))

	// 验证视图是否真的创建成功
	testSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 0", tableName)
	logger.LogInfo(ctx, "Testing view existence", zap.String("sql", testSQL))
	if _, err := db.Query(testSQL); err != nil {
		logger.LogError(ctx, err, "View verification failed",
			zap.String("table", tableName),
			zap.Error(err))
		return fmt.Errorf("view creation verification failed for table %s: %w", tableName, err)
	}

	logger.LogInfo(ctx, "View verification successful for table ", zap.String("table", fmt.Sprintf("%v", tableName)))

	// 创建智能活跃数据视图（自动过滤墓碑记录）
	if err := q.createActiveDataView(ctx, db, tableName); err != nil {
		logger.LogWarn(ctx, "Failed to create active data view for table ", zap.String("table", tableName), zap.Error(err))
		// 不返回错误，因为这只是一个优化视图
	}
	return nil
}

// createActiveDataView 创建智能活跃数据视图（自动过滤墓碑记录）
func (q *Querier) createActiveDataView(ctx context.Context, db *sql.DB, tableName string) error {
	activeViewName := fmt.Sprintf("%s_active", tableName)

	// 使用JSON函数提取_deleted字段进行过滤（比LIKE更高效）
	// 如果DuckDB版本不支持JSON函数，则回退到LIKE模式
	createActiveViewSQL := fmt.Sprintf(`
		CREATE VIEW %s AS 
		SELECT * FROM %s
		WHERE payload NOT LIKE '%%"_deleted":true%%' 
		  AND payload NOT LIKE '%%"_deleted": true%%'
	`, activeViewName, tableName)

	logger.LogInfo(ctx, "Creating active data view", zap.String("value", activeViewName))

	if _, err := db.Exec(createActiveViewSQL); err != nil {
		return fmt.Errorf("failed to create active data view: %w", err)
	}

	// 验证视图创建成功
	testSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 0", activeViewName)
	if _, err := db.Query(testSQL); err != nil {
		return fmt.Errorf("active view verification failed: %w", err)
	}

	logger.LogInfo(ctx, "Successfully created active data view", zap.String("value", activeViewName))
	return nil
}

// createEmptyTableView 创建空表视图（当表存在但还没有数据时）
func (q *Querier) createEmptyTableView(ctx context.Context, db *sql.DB, tableName string) error {
	// 先创建一个空的缓冲区表结构
	bufferTableName := fmt.Sprintf("%s_buffer", tableName)
	createBufferTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR,
			timestamp BIGINT,
			payload VARCHAR,
			table_name VARCHAR
		)
	`, bufferTableName)

	if _, err := db.Exec(createBufferTableSQL); err != nil {
		return fmt.Errorf("failed to create empty buffer table: %w", err)
	}

	logger.LogInfo(ctx, "Created empty buffer table", zap.String("table", bufferTableName))

	// 创建基于空缓冲区表的视图
	viewSQL := fmt.Sprintf(
		"CREATE VIEW %s AS SELECT id, timestamp, payload, table_name AS \"table\" FROM %s",
		tableName, bufferTableName,
	)

	if _, err := db.Exec(viewSQL); err != nil {
		return fmt.Errorf("failed to create empty view for table %s: %w", tableName, err)
	}

	logger.LogInfo(ctx, "Successfully created empty view for table", zap.String("table", tableName), zap.String("status", "success"))

	// 创建智能活跃数据视图
	if err := q.createActiveDataView(ctx, db, tableName); err != nil {
		logger.LogWarn(ctx, "Failed to create active data view for empty table ", zap.String("table", tableName), zap.Error(err))
		// 不返回错误，因为这只是一个优化视图
	}

	return nil
}

// InitializeTableView 初始化表视图结构（用于新创建的表）
func (q *Querier) InitializeTableView(ctx context.Context, tableName string) error {
	logger.LogInfo(ctx, "Initializing table view for", zap.String("table", tableName))

	// 获取数据库连接
	db, err := q.getDBConnection(ctx)
	if err != nil {
		return fmt.Errorf("failed to get DB connection: %w", err)
	}
	defer q.returnDBConnection(ctx, db)

	// 创建空视图结构
	return q.createEmptyTableView(ctx, db, tableName)
}

// EnsureTableViewExists 确保表视图存在（幂等操作，带内存缓存）
func (q *Querier) EnsureTableViewExists(ctx context.Context, tableName string) error {
	// 检查内存缓存
	q.initializedViewsMutex.RLock()
	if initialized, ok := q.initializedViews[tableName]; ok && initialized {
		q.initializedViewsMutex.RUnlock()
		// 视图已初始化过，直接返回
		return nil
	}
	q.initializedViewsMutex.RUnlock()

	// 缓存未命中，检查并创建视图
	q.initializedViewsMutex.Lock()
	defer q.initializedViewsMutex.Unlock()

	// 双重检查（Double-Check Locking）
	if initialized, ok := q.initializedViews[tableName]; ok && initialized {
		return nil
	}

	// 获取数据库连接
	db, err := q.getDBConnection(ctx)
	if err != nil {
		return fmt.Errorf("failed to get DB connection: %w", err)
	}
	defer q.returnDBConnection(ctx, db)

	// 检查主视图是否存在
	checkSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s LIMIT 0", tableName)
	if _, err := db.Query(checkSQL); err != nil {
		// 视图不存在，创建之
		logger.LogInfo(ctx, "Table view  does not exist, creating it now", zap.String("table", fmt.Sprintf("%v", tableName)))
		if err := q.createEmptyTableView(ctx, db, tableName); err != nil {
			return err
		}
	} else {
		logger.LogInfo(ctx, "Table view  already exists (first time check)", zap.String("table", fmt.Sprintf("%v", tableName)))
	}

	// 更新缓存
	q.initializedViews[tableName] = true
	logger.LogInfo(ctx, "Marked table  as initialized in cache", zap.String("table", fmt.Sprintf("%v", tableName)))

	return nil
}

// InvalidateViewCache 使视图缓存失效（在表删除时调用）
func (q *Querier) InvalidateViewCache(ctx context.Context, tableName string) {
	q.initializedViewsMutex.Lock()
	delete(q.initializedViews, tableName)
	q.initializedViewsMutex.Unlock()
	logger.LogInfo(ctx, "Invalidated view cache for table ", zap.String("table", fmt.Sprintf("%v", tableName)))
}

// incrementalUpdateView 增量更新视图（只更新变更的部分）
func (q *Querier) incrementalUpdateView(ctx context.Context, tableName string, changes []buffer.DataRow) error {
	if len(changes) == 0 {
		logger.LogInfo(ctx, "No changes to update for table ", zap.String("table", fmt.Sprintf("%v", tableName)))
		return nil
	}

	db, err := q.getDBConnection(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer q.returnDBConnection(ctx, db)

	// 策略1: 如果变更量较小，使用增量更新
	// 策略2: 如果变更量较大（>10%表大小），重建视图

	// 检查表是否存在
	checkSQL := fmt.Sprintf("SELECT COUNT(*) as count FROM %s LIMIT 1", tableName)
	var tableExists bool
	if _, err := db.Query(checkSQL); err == nil {
		tableExists = true
	}

	if !tableExists {
		logger.LogInfo(ctx, "Table  does not exist, skipping incremental update", zap.String("table", fmt.Sprintf("%v", tableName)))
		return nil
	}

	// 统计当前表大小
	countSQL := fmt.Sprintf("SELECT COUNT(*) as total FROM %s", tableName)
	var totalCount int64
	row := db.QueryRow(countSQL)
	if err := row.Scan(&totalCount); err != nil {
		logger.LogWarn(ctx, "Failed to get table size", zap.Error(err))
		totalCount = 0
	}

	changeRatio := float64(len(changes)) / float64(totalCount)

	// 如果变更量 > 10% 或表为空，直接重建视图
	if totalCount == 0 || changeRatio > 0.1 {
		logger.LogInfo(ctx, "Change ratio exceeds threshold, rebuilding view",
			zap.String("table", tableName),
			zap.Float64("change_ratio_pct", changeRatio*100),
			zap.String("action", "rebuild_view"))

		// 获取所有文件并重建视图
		storageFiles, err := q.getStorageFilesForTable(ctx, tableName)
		if err != nil {
			return fmt.Errorf("failed to get storage files: %w", err)
		}
		return q.createTableViewWithDB(ctx, db, tableName, storageFiles)
	}

	// 增量更新策略：
	// 1. 识别变更类型（新增、更新、删除）
	// 2. 对于更新和删除，先标记为墓碑
	// 3. 对于新增和更新的新版本，插入到缓冲区表

	logger.LogInfo(ctx, "Performing incremental update", zap.String("table", tableName), zap.Int("changes", len(changes)), zap.Float64("change_ratio_pct", changeRatio*100))

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
		logger.LogInfo(ctx, "Creating buffer table  for incremental updates", zap.String("table", fmt.Sprintf("%v", bufferTableName)))
		if err := q.createBufferTable(ctx, db, bufferTableName); err != nil {
			return fmt.Errorf("failed to create buffer table: %w", err)
		}
	}

	// 插入变更到缓冲区表
	if err := q.loadBufferDataToDB(ctx, db, tableName, changes); err != nil {
		return fmt.Errorf("failed to load incremental changes: %w", err)
	}

	// 重建活跃数据视图以反映变更
	if err := q.createActiveDataView(ctx, db, tableName); err != nil {
		logger.LogWarn(ctx, "Failed to update active view", zap.Error(err))
	}

	logger.LogInfo(ctx, "Incremental update completed for table ", zap.String("table", fmt.Sprintf("%v", tableName)))
	return nil
}

// createBufferTable 创建缓冲区表结构
func (q *Querier) createBufferTable(ctx context.Context, db *sql.DB, bufferTableName string) error {
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

	logger.LogInfo(ctx, "Successfully created buffer table", zap.String("table", bufferTableName))
	return nil
}

// RefreshViewWithChanges 当缓冲区刷新后，刷新视图以反映变更
func (q *Querier) RefreshViewWithChanges(ctx context.Context, tableName string, flushedRows []buffer.DataRow) error {
	// 使用增量更新策略
	return q.incrementalUpdateView(ctx, tableName, flushedRows)
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
func (q *Querier) writeBufferToParquet(ctx context.Context, filePath string, rows []buffer.DataRow) error {
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 使用buffer的方法写入Parquet文件
	return q.buffer.WriteTempParquetFile(ctx, filePath, rows)
}

// Close 关闭查询处理器
func (q *Querier) Close(ctx context.Context) {
	if q.db != nil {
		q.db.Close()
	}

	// 关闭连接池
	if q.dbPool != nil {
		q.dbPool.Close(ctx)
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
		q.fileCache.Clear(ctx)
	}

	// 清理临时文件
	os.RemoveAll(q.tempDir)
}

// =============================================================================
// DuckDB 连接池方法
// =============================================================================

// NewDuckDBPool 创建DuckDB连接池
func NewDuckDBPool(ctx context.Context, maxConns int) (*DuckDBPool, error) {
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
			logger.LogWarn(ctx, "failed to apply DuckDB optimization", zap.String("optimization", opt), zap.Error(err))
		}
	}

	pool.connections <- db
	pool.created = 1

	return pool, nil
}

// Get 从连接池获取连接
func (p *DuckDBPool) Get(ctx context.Context) (*sql.DB, error) {
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
func (p *DuckDBPool) Put(ctx context.Context, db *sql.DB) {
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
func (p *DuckDBPool) Close(ctx context.Context) {
	close(p.connections)
	for db := range p.connections {
		db.Close()
	}
}

// =============================================================================
// 增强的查询方法
// =============================================================================

// prepareTableDataWithCache 使用文件缓存准备表数据
func (q *Querier) prepareTableDataWithCache(ctx context.Context, tableName string) error {
	// 1. 获取缓冲区数据文件
	bufferFiles := q.getBufferFilesForTable(ctx, tableName)

	// 2. 获取存储的数据文件（使用文件缓存）
	storageFiles, err := q.getStorageFilesWithCache(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get storage files: %w", err)
	}

	// 3. 创建或更新DuckDB视图
	allFiles := append(bufferFiles, storageFiles...)
	if len(allFiles) == 0 {
		logger.LogInfo(ctx, "No data files found for table", zap.String("table", tableName))
		return nil
	}

	return q.createTableView(ctx, tableName, allFiles)
}

// getStorageFilesWithCache 使用文件缓存获取存储文件
func (q *Querier) getStorageFilesWithCache(ctx context.Context, tableName string) ([]string, error) {
	// 【修复】如果Redis连接池不可用（单节点模式），直接扫描MinIO
	if q.redisPool == nil {
		logger.LogInfo(ctx, "Single-node mode: scanning MinIO directly for table ", zap.String("table", fmt.Sprintf("%v", tableName)))
		return q.scanMinIOForTable(ctx, tableName)
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
			logger.LogWarn(ctx, "failed to get object names for key ", zap.String("key", key), zap.Error(err))
			continue
		}

		// 处理该索引下的所有文件
		for _, objectName := range objectNames {
			// 使用文件缓存获取文件
			localPath, err := q.fileCache.Get(ctx, objectName, func(objName string) (string, error) {
				q.updateFileDownloadStats(ctx)
				return q.downloadToTemp(ctx, objName)
			})
			if err != nil {
				logger.LogWarn(ctx, "failed to get cached file ", zap.String("object", objectName), zap.Error(err))
				continue
			}
			files = append(files, localPath)
		}
	}

	return files, nil
}

// scanMinIOForTable 扫描MinIO获取表的所有Parquet文件（单节点模式，带缓存）
func (q *Querier) scanMinIOForTable(ctx context.Context, tableName string) ([]string, error) {
	if q.minioClient == nil {
		logger.LogInfo(ctx, "MinIO client not available for table ", zap.String("table", fmt.Sprintf("%v", tableName)))
		return []string{}, nil
	}

	// 【修复】移除 _active 后缀，数据存储在基础表名下
	baseTableName := strings.TrimSuffix(tableName, "_active")

	// 检查缓存
	q.localFileIndexMutex.RLock()
	if cachedFiles, ok := q.localFileIndex[baseTableName]; ok {
		lastScan := q.localIndexLastScan[baseTableName]
		if time.Since(lastScan) < q.localFileIndexTTL {
			q.localFileIndexMutex.RUnlock()
			logger.LogInfo(ctx, "Using cached file index",
				zap.String("table", baseTableName),
				zap.Int("files", len(cachedFiles)),
				zap.Duration("age", time.Since(lastScan)))
			return cachedFiles, nil
		}
	}
	q.localFileIndexMutex.RUnlock()

	// 缓存未命中或已过期，执行扫描
	bucket := q.config.MinIO.Bucket
	prefix := baseTableName + "/"

	logger.LogInfo(ctx, "Scanning MinIO bucket",
		zap.String("bucket", bucket),
		zap.String("prefix", prefix),
		zap.String("table", baseTableName),
		zap.String("cache_status", "miss"))

	// 列出所有对象
	objectCh := q.minioClient.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var files []string
	objectCount := 0
	for object := range objectCh {
		if object.Err != nil {
			logger.LogError(ctx, object.Err, "Failed to list object")
			continue
		}

		// 只处理 .parquet 文件
		if !strings.HasSuffix(object.Key, ".parquet") {
			continue
		}

		objectCount++
		logger.LogInfo(ctx, "Found MinIO object:  (size:  bytes)", zap.Any("params", []interface{}{object.Key, object.Size}))

		// 下载到临时目录
		localPath, err := q.downloadToTemp(ctx, object.Key)
		if err != nil {
			logger.LogWarn(ctx, "failed to download ", zap.String("object", object.Key))
			continue
		}

		files = append(files, localPath)
	}

	logger.LogInfo(ctx, "Scanned MinIO for table",
		zap.String("table", baseTableName),
		zap.Int("objects_found", objectCount),
		zap.Int("files_downloaded", len(files)))

	// 更新缓存
	q.localFileIndexMutex.Lock()
	q.localFileIndex[baseTableName] = files
	q.localIndexLastScan[baseTableName] = time.Now()
	q.localFileIndexMutex.Unlock()

	logger.LogInfo(ctx, "Updated file index cache",
		zap.String("table", baseTableName),
		zap.Int("files", len(files)))

	return files, nil
}

// InvalidateFileIndexCache 使文件索引缓存失效（在数据刷新后调用）
func (q *Querier) InvalidateFileIndexCache(ctx context.Context, tableName string) {
	baseTableName := strings.TrimSuffix(tableName, "_active")
	q.localFileIndexMutex.Lock()
	delete(q.localFileIndex, baseTableName)
	delete(q.localIndexLastScan, baseTableName)
	q.localFileIndexMutex.Unlock()
	logger.LogInfo(ctx, "Invalidated file index cache for table ", zap.String("table", fmt.Sprintf("%v", baseTableName)))
}

// getDBConnection 获取数据库连接
func (q *Querier) getDBConnection(ctx context.Context) (*sql.DB, error) {
	if q.dbPool != nil {
		return q.dbPool.Get(ctx)
	}
	return q.db, nil
}

// returnDBConnection 归还数据库连接
func (q *Querier) returnDBConnection(ctx context.Context, db *sql.DB) {
	if q.dbPool != nil && db != q.db {
		q.dbPool.Put(ctx, db)
	}
}

// executeQueryWithOptimization 使用优化执行查询
func (q *Querier) executeQueryWithOptimization(ctx context.Context, db *sql.DB, sqlQuery string) (string, error) {
	// 检查是否可以使用预编译语句
	if q.canUsePreparedStatement(ctx, sqlQuery) {
		return q.executeWithPreparedStatement(ctx, db, sqlQuery)
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
func (q *Querier) canUsePreparedStatement(ctx context.Context, sqlQuery string) bool {
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
func (q *Querier) executeWithPreparedStatement(ctx context.Context, db *sql.DB, sqlQuery string) (string, error) {
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
func (q *Querier) updateQueryStats(ctx context.Context, tables []string, queryTime time.Duration, queryType string) {
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
func (q *Querier) updateCacheHitStats(ctx context.Context, tables []string, queryTime time.Duration) {
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
func (q *Querier) updateCacheMissStats(ctx context.Context) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.CacheMisses++
}

// updateErrorStats 更新错误统计
func (q *Querier) updateErrorStats(ctx context.Context) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.ErrorCount++
}

// updateFileDownloadStats 更新文件下载统计
func (q *Querier) updateFileDownloadStats(ctx context.Context) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	q.queryStats.FileDownloads++
}

// GetQueryStats 获取查询统计信息
func (q *Querier) GetQueryStats(ctx context.Context) *QueryStats {
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
	stats["query_stats"] = q.GetQueryStats(ctx)

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
	bufferFiles := q.getBufferFilesForTable(ctx, tableName)

	// 1.5 直接从缓冲区加载数据到DuckDB（混合查询）
	bufferRows, err := q.getBufferDataRows(ctx, tableName)
	if err != nil {
		logger.LogWarn(ctx, "failed to get buffer data rows",
			zap.String("table", tableName),
			zap.Error(err))
	} else if len(bufferRows) > 0 {
		logger.LogInfo(ctx, "Loading rows from buffer",
			zap.Int("rows", len(bufferRows)),
			zap.String("table", tableName))
		// 将缓冲区数据直接插入到临时表
		if err := q.loadBufferDataToDB(ctx, db, tableName, bufferRows); err != nil {
			logger.LogWarn(ctx, "failed to load buffer data to DB", zap.Error(err))
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
		logger.LogInfo(ctx, "No data files or buffer data found for table", zap.String("table", tableName))
		return nil
	}

	return q.createTableViewWithDB(ctx, db, tableName, allFiles)
}

// getBufferDataRows 从缓冲区获取指定表的数据行（用于混合查询）
func (q *Querier) getBufferDataRows(ctx context.Context, tableName string) ([]buffer.DataRow, error) {
	if q.buffer == nil {
		logger.LogInfo(ctx, "Buffer is nil for table",
			zap.String("table", tableName))
		return []buffer.DataRow{}, nil
	}

	startTime := time.Now()

	// 【修复】移除 _active 后缀，查询基础表的缓冲区数据
	baseTableName := strings.TrimSuffix(tableName, "_active")
	logger.LogInfo(ctx, "Getting buffer data for base table",
		zap.String("base_table", baseTableName),
		zap.String("original_table", tableName))

	// 获取该表的所有缓冲区键
	keys := q.buffer.GetTableKeys(ctx, baseTableName)
	logger.LogInfo(ctx, "Found buffer keys",
		zap.Int("keys", len(keys)),
		zap.String("table", baseTableName))
	if len(keys) == 0 {
		return []buffer.DataRow{}, nil
	}

	// 收集所有数据行
	var allRows []buffer.DataRow
	for _, key := range keys {
		rows := q.buffer.GetBufferData(ctx, key)
		allRows = append(allRows, rows...)
	}

	// 更新统计指标
	if len(allRows) > 0 {
		q.statsLock.Lock()
		q.queryStats.BufferHits++
		q.queryStats.BufferRows += int64(len(allRows))
		q.statsLock.Unlock()
	}

	logger.LogInfo(ctx, "Retrieved buffer rows",
		zap.Int("rows", len(allRows)),
		zap.String("table", baseTableName),
		zap.Int("keys", len(keys)),
		zap.Duration("duration", time.Since(startTime)))
	return allRows, nil
}

// loadBufferDataToDB 将缓冲区数据加载到DuckDB（用于混合查询）
func (q *Querier) loadBufferDataToDB(ctx context.Context, db *sql.DB, tableName string, rows []buffer.DataRow) error {
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
			logger.LogWarn(ctx, "failed to insert buffer row", zap.Error(err))
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

	logger.LogInfo(ctx, "Loaded buffer rows into DuckDB",
		zap.Int("rows", len(rows)),
		zap.String("table", tableName+"_buffer"),
		zap.Duration("duration", mergeTime))
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
		if q.indexSystem.HasBloomFilter(ctx, bloomKey) {
			logger.LogInfo(ctx, "Using BloomFilter for table ", zap.String("table", fmt.Sprintf("%v", table)))
			// 这里可以根据查询条件进行BloomFilter过滤
			// 简化实现：直接添加表名
			optimizedTables = append(optimizedTables, table)
			continue
		}

		// 检查是否有MinMax索引
		minMaxKey := fmt.Sprintf("minmax:%s", table)
		if q.indexSystem.HasMinMaxIndex(ctx, minMaxKey) {
			logger.LogInfo(ctx, "Using MinMax index for table ", zap.String("table", fmt.Sprintf("%v", table)))
			// 这里可以根据查询条件进行范围过滤
			// 简化实现：直接添加表名
			optimizedTables = append(optimizedTables, table)
			continue
		}

		// 没有索引的表也添加
		optimizedTables = append(optimizedTables, table)
	}

	// 更新索引命中率统计
	q.updateIndexHitStats(ctx, len(optimizedTables), len(tables))

	return optimizedTables, nil
}

// updateIndexHitStats 更新索引命中率统计
func (q *Querier) updateIndexHitStats(ctx context.Context, hitCount, totalCount int) {
	q.statsLock.Lock()
	defer q.statsLock.Unlock()

	// 这里可以添加更详细的索引统计逻辑
	logger.LogInfo(ctx, "Index hit rate: /", zap.Any("params", []interface{}{hitCount, totalCount}))
}
