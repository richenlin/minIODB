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
	"vitess.io/vitess/go/vt/sqlparser"
)

// QueryInfo 查询信息结构
type QueryInfo struct {
	QueryType   string                 // SELECT, INSERT, UPDATE, DELETE, etc.
	Tables      []string               // FROM 子句中的表名
	JoinTables  []string               // JOIN 子句中的表名
	Conditions  map[string]interface{} // WHERE 条件（仅支持简单等值条件）
	IsSupported bool                   // 是否完全支持解析
	OriginalSQL string                 // 原始 SQL 语句
}

// QueryFilter 查询过滤器（保持兼容性）
type QueryFilter struct {
	ID  string
	Day string
}

// SQLParser SQL 解析器
type SQLParser struct {
	// 可以在这里添加配置选项
}

// NewSQLParser 创建新的 SQL 解析器
func NewSQLParser() *SQLParser {
	return &SQLParser{}
}

// ParseQuery 解析 SQL 查询
func (p *SQLParser) ParseQuery(sql string) (*QueryInfo, error) {
	info := &QueryInfo{
		OriginalSQL: sql,
		Conditions:  make(map[string]interface{}),
		Tables:      []string{},
		JoinTables:  []string{},
	}

	// 1. 尝试使用 Vitess SQLParser 完整解析
	stmt, err := sqlparser.Parse(sql)
	if err != nil {
		log.Printf("Vitess parser failed, using fallback: %v", err)
		return p.fallbackParse(info)
	}

	// 2. 根据语句类型进行解析
	switch stmt := stmt.(type) {
	case *sqlparser.Select:
		return p.parseSelect(stmt, info)
	case *sqlparser.Insert:
		info.QueryType = "INSERT"
		info.Tables = p.extractTableNamesFromInsert(stmt)
		info.IsSupported = true
		return info, nil
	case *sqlparser.Update:
		info.QueryType = "UPDATE"
		info.Tables = p.extractTableNamesFromUpdate(stmt)
		info.IsSupported = true
		return info, nil
	case *sqlparser.Delete:
		info.QueryType = "DELETE"
		info.Tables = p.extractTableNamesFromDelete(stmt)
		info.IsSupported = true
		return info, nil
	default:
		log.Printf("Unsupported statement type: %T", stmt)
		return p.fallbackParse(info)
	}
}

// parseSelect 解析 SELECT 语句
func (p *SQLParser) parseSelect(stmt *sqlparser.Select, info *QueryInfo) (*QueryInfo, error) {
	info.QueryType = "SELECT"
	info.IsSupported = true

	// 提取 FROM 子句中的表名
	if stmt.From != nil {
		tables := p.extractTablesFromTableExprs(stmt.From)
		info.Tables = append(info.Tables, tables...)
	}

	// 提取 WHERE 条件
	if stmt.Where != nil {
		conditions, joinTables := p.extractWhereConditions(stmt.Where.Expr)
		info.Conditions = conditions
		info.JoinTables = append(info.JoinTables, joinTables...)
	}

	return info, nil
}

// extractTablesFromTableExprs 从 TableExprs 中提取表名
func (p *SQLParser) extractTablesFromTableExprs(tableExprs sqlparser.TableExprs) []string {
	var tables []string

	for _, tableExpr := range tableExprs {
		switch expr := tableExpr.(type) {
		case *sqlparser.AliasedTableExpr:
			if tableName := p.extractTableName(expr.Expr); tableName != "" {
				tables = append(tables, tableName)
			}
		case *sqlparser.JoinTableExpr:
			// 处理 JOIN
			leftTables := p.extractTablesFromTableExprs(sqlparser.TableExprs{expr.LeftExpr})
			rightTables := p.extractTablesFromTableExprs(sqlparser.TableExprs{expr.RightExpr})
			tables = append(tables, leftTables...)
			tables = append(tables, rightTables...)
		}
	}

	return tables
}

// extractTableName 从 SimpleTableExpr 中提取表名
func (p *SQLParser) extractTableName(expr sqlparser.SimpleTableExpr) string {
	switch table := expr.(type) {
	case sqlparser.TableName:
		return table.Name.String()
	}
	return ""
}

// extractWhereConditions 提取 WHERE 条件（仅支持简单等值条件）
func (p *SQLParser) extractWhereConditions(expr sqlparser.Expr) (map[string]interface{}, []string) {
	conditions := make(map[string]interface{})
	var joinTables []string

	switch node := expr.(type) {
	case *sqlparser.ComparisonExpr:
		if node.Operator == sqlparser.EqualOp {
			// 处理等值比较
			if colName := p.extractColumnName(node.Left); colName != "" {
				if value := p.extractValue(node.Right); value != nil {
					conditions[colName] = value
				}
			}
		}
	case *sqlparser.AndExpr:
		// 处理 AND 条件
		leftConds, leftJoins := p.extractWhereConditions(node.Left)
		rightConds, rightJoins := p.extractWhereConditions(node.Right)

		for k, v := range leftConds {
			conditions[k] = v
		}
		for k, v := range rightConds {
			conditions[k] = v
		}
		joinTables = append(joinTables, leftJoins...)
		joinTables = append(joinTables, rightJoins...)
	}

	return conditions, joinTables
}

// extractColumnName 提取列名
func (p *SQLParser) extractColumnName(expr sqlparser.Expr) string {
	switch col := expr.(type) {
	case *sqlparser.ColName:
		return col.Name.String()
	}
	return ""
}

// extractValue 提取值
func (p *SQLParser) extractValue(expr sqlparser.Expr) interface{} {
	switch val := expr.(type) {
	case *sqlparser.Literal:
		switch val.Type {
		case sqlparser.StrVal:
			return string(val.Val)
		case sqlparser.IntVal:
			return string(val.Val) // 保持为字符串，避免类型转换问题
		}
	}
	return nil
}

// extractTableNamesFromInsert 从 INSERT 语句中提取表名
func (p *SQLParser) extractTableNamesFromInsert(stmt *sqlparser.Insert) []string {
	if stmt.Table.Name.String() != "" {
		return []string{stmt.Table.Name.String()}
	}
	return []string{}
}

// extractTableNamesFromUpdate 从 UPDATE 语句中提取表名
func (p *SQLParser) extractTableNamesFromUpdate(stmt *sqlparser.Update) []string {
	tables := p.extractTablesFromTableExprs(stmt.TableExprs)
	return tables
}

// extractTableNamesFromDelete 从 DELETE 语句中提取表名
func (p *SQLParser) extractTableNamesFromDelete(stmt *sqlparser.Delete) []string {
	tables := p.extractTablesFromTableExprs(stmt.TableExprs)
	return tables
}

// fallbackParse 降级解析器（基于正则表达式）
func (p *SQLParser) fallbackParse(info *QueryInfo) (*QueryInfo, error) {
	info.IsSupported = false

	// 识别查询类型
	info.QueryType = p.identifyQueryType(info.OriginalSQL)

	// 提取表名（无论什么语法都尽量提取）
	info.Tables = p.extractTableNamesRegex(info.OriginalSQL)

	// 提取 JOIN 表名
	info.JoinTables = p.extractJoinTableNamesRegex(info.OriginalSQL)

	// 尝试提取简单的 WHERE 条件
	info.Conditions = p.extractSimpleConditionsRegex(info.OriginalSQL)

	log.Printf("Fallback parsing completed: type=%s, tables=%v, joins=%v",
		info.QueryType, info.Tables, info.JoinTables)

	return info, nil
}

// identifyQueryType 识别查询类型
func (p *SQLParser) identifyQueryType(sql string) string {
	lowerSQL := strings.ToLower(strings.TrimSpace(sql))

	if strings.HasPrefix(lowerSQL, "select") {
		return "SELECT"
	} else if strings.HasPrefix(lowerSQL, "insert") {
		return "INSERT"
	} else if strings.HasPrefix(lowerSQL, "update") {
		return "UPDATE"
	} else if strings.HasPrefix(lowerSQL, "delete") {
		return "DELETE"
	} else if strings.HasPrefix(lowerSQL, "pivot") {
		return "PIVOT"
	} else if strings.Contains(lowerSQL, "qualify") {
		return "SELECT" // QUALIFY 通常用于 SELECT 语句
	}

	return "UNKNOWN"
}

// extractTableNamesRegex 使用正则表达式提取表名
func (p *SQLParser) extractTableNamesRegex(sql string) []string {
	var tables []string
	lowerSQL := strings.ToLower(sql)

	// 匹配 FROM 子句
	fromRegex := regexp.MustCompile(`(?i)from\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	fromMatches := fromRegex.FindAllStringSubmatch(lowerSQL, -1)
	for _, match := range fromMatches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	// 匹配 PIVOT 语句中的表名
	pivotRegex := regexp.MustCompile(`(?i)pivot\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	pivotMatches := pivotRegex.FindAllStringSubmatch(lowerSQL, -1)
	for _, match := range pivotMatches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	return p.uniqueStrings(tables)
}

// extractJoinTableNamesRegex 使用正则表达式提取 JOIN 表名
func (p *SQLParser) extractJoinTableNamesRegex(sql string) []string {
	var tables []string
	lowerSQL := strings.ToLower(sql)

	// 匹配各种 JOIN 子句
	joinRegex := regexp.MustCompile(`(?i)(?:inner\s+join|left\s+join|right\s+join|full\s+join|join)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	joinMatches := joinRegex.FindAllStringSubmatch(lowerSQL, -1)
	for _, match := range joinMatches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	return p.uniqueStrings(tables)
}

// extractSimpleConditionsRegex 使用正则表达式提取简单条件
func (p *SQLParser) extractSimpleConditionsRegex(sql string) map[string]interface{} {
	conditions := make(map[string]interface{})

	// 查找 WHERE 子句
	lowerSQL := strings.ToLower(sql)
	whereIndex := strings.Index(lowerSQL, "where")
	if whereIndex == -1 {
		return conditions
	}

	whereClause := sql[whereIndex+5:] // 跳过 "where"

	// 匹配 column='value' 或 column="value" 格式
	condRegex := regexp.MustCompile(`(?i)([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*['"]([^'"]+)['"]`)
	matches := condRegex.FindAllStringSubmatch(whereClause, -1)

	for _, match := range matches {
		if len(match) > 2 {
			conditions[strings.ToLower(match[1])] = match[2]
		}
	}

	return conditions
}

// uniqueStrings 去重字符串切片
func (p *SQLParser) uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, str := range input {
		if !seen[str] && str != "" {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}

// ConvertToQueryFilter 将 QueryInfo 转换为 QueryFilter（保持向后兼容）
func (info *QueryInfo) ConvertToQueryFilter() *QueryFilter {
	filter := &QueryFilter{}

	if id, exists := info.Conditions["id"]; exists {
		if idStr, ok := id.(string); ok {
			filter.ID = idStr
		}
	}

	if day, exists := info.Conditions["day"]; exists {
		if dayStr, ok := day.(string); ok {
			filter.Day = dayStr
		}
	}

	return filter
}

// Querier handles the query execution process
type Querier struct {
	redisClient *redis.Client
	minioClient storage.Uploader
	db          *sql.DB
	buffer      *buffer.SharedBuffer
	sqlParser   *SQLParser // 新增 SQL 解析器
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
		sqlParser:   NewSQLParser(), // 初始化 SQL 解析器
	}, nil
}

// parseWhereClause 解析SQL的WHERE子句，提取table、id和day条件（保持向后兼容）
func (q *Querier) parseWhereClause(sqlQuery string) (*QueryFilter, error) {
	// 使用新的解析器
	queryInfo, err := q.sqlParser.ParseQuery(sqlQuery)
	if err != nil {
		log.Printf("Failed to parse query: %v", err)
		// 降级到原始实现
		return q.parseWhereClauseLegacy(sqlQuery)
	}

	// 转换为 QueryFilter
	filter := queryInfo.ConvertToQueryFilter()
	log.Printf("Parsed WHERE clause: id='%s', day='%s' (supported: %v)",
		filter.ID, filter.Day, queryInfo.IsSupported)

	return filter, nil
}

// parseWhereClauseLegacy 原始的 WHERE 子句解析（作为备用）
func (q *Querier) parseWhereClauseLegacy(sqlQuery string) (*QueryFilter, error) {
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

	log.Printf("Legacy parsed WHERE clause: id='%s', day='%s'", filter.ID, filter.Day)
	return filter, nil
}

// extractTablesFromSQL 从SQL中提取表名（保持向后兼容）
func (q *Querier) extractTablesFromSQL(sqlQuery string) []string {
	// 使用新的解析器
	queryInfo, err := q.sqlParser.ParseQuery(sqlQuery)
	if err != nil {
		log.Printf("Failed to parse query for table extraction: %v", err)
		// 降级到原始实现
		return q.extractTablesFromSQLLegacy(sqlQuery)
	}

	// 合并 FROM 表和 JOIN 表
	allTables := append(queryInfo.Tables, queryInfo.JoinTables...)
	allTables = q.sqlParser.uniqueStrings(allTables)

	log.Printf("Extracted tables: %v (supported: %v)", allTables, queryInfo.IsSupported)
	return allTables
}

// extractTablesFromSQLLegacy 原始的表名提取（作为备用）
func (q *Querier) extractTablesFromSQLLegacy(sqlQuery string) []string {
	var tables []string

	// 简单的表名提取逻辑，匹配FROM和JOIN后的表名
	lowerQuery := strings.ToLower(sqlQuery)

	// 匹配FROM子句
	fromRegex := regexp.MustCompile(`(?i)from\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	fromMatches := fromRegex.FindAllStringSubmatch(lowerQuery, -1)
	for _, match := range fromMatches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	// 匹配JOIN子句
	joinRegex := regexp.MustCompile(`(?i)join\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	joinMatches := joinRegex.FindAllStringSubmatch(sqlQuery, -1)
	for _, match := range joinMatches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	// 去重
	uniqueTables := make([]string, 0)
	seen := make(map[string]bool)
	for _, table := range tables {
		if !seen[table] {
			uniqueTables = append(uniqueTables, table)
			seen[table] = true
		}
	}

	log.Printf("Legacy extracted tables: %v", uniqueTables)
	return uniqueTables
}

// getDataFiles 根据过滤器和表名获取相关的数据文件
func (q *Querier) getDataFiles(ctx context.Context, filter *QueryFilter, tables []string) ([]string, error) {
	var allFiles []string

	// 如果没有指定表，使用默认表
	if len(tables) == 0 {
		tables = []string{"default"} // 或从配置获取默认表名
	}

	for _, tableName := range tables {
		tableFiles, err := q.getDataFilesForTable(ctx, filter, tableName)
		if err != nil {
			log.Printf("WARN: failed to get files for table %s: %v", tableName, err)
			continue
		}
		allFiles = append(allFiles, tableFiles...)
	}

	return allFiles, nil
}

// getDataFilesForTable 获取指定表的数据文件
func (q *Querier) getDataFilesForTable(ctx context.Context, filter *QueryFilter, tableName string) ([]string, error) {
	var allFiles []string

	if filter.ID != "" && filter.Day != "" {
		// 精确查询：根据table、id和day获取文件
		redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", tableName, filter.ID, filter.Day)
		s3Files, err := q.redisClient.SMembers(ctx, redisKey).Result()
		if err != nil && err != redis.Nil {
			log.Printf("WARN: could not get files from redis for key %s: %v", redisKey, err)
		}
		for _, file := range s3Files {
			allFiles = append(allFiles, fmt.Sprintf("s3://%s/%s", "olap-data", file))
		}
		log.Printf("Found %d files in Redis for key %s", len(s3Files), redisKey)

		// 获取缓冲区中的热数据
		bufferKey := fmt.Sprintf("%s/%s/%s", tableName, filter.ID, filter.Day)
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
		// 按ID查询：获取该表中该ID的所有天数据
		pattern := fmt.Sprintf("index:table:%s:id:%s:*", tableName, filter.ID)
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
		log.Printf("Found %d files for table %s, ID %s", len(allFiles), tableName, filter.ID)

		// 获取缓冲区中该表该ID的所有数据
		tableBufferFiles := q.getBufferFilesForTableAndID(tableName, filter.ID)
		allFiles = append(allFiles, tableBufferFiles...)
	} else if filter.Day != "" {
		// 按天查询：获取该表该天所有ID的数据
		pattern := fmt.Sprintf("index:table:%s:id:*:%s", tableName, filter.Day)
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
		log.Printf("Found %d files for table %s, day %s", len(allFiles), tableName, filter.Day)

		// 获取缓冲区中该表该天的所有数据
		tableBufferFiles := q.getBufferFilesForTableAndDay(tableName, filter.Day)
		allFiles = append(allFiles, tableBufferFiles...)
	} else {
		// 全表扫描：获取该表的所有数据
		pattern := fmt.Sprintf("index:table:%s:id:*", tableName)
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
		log.Printf("Table scan for %s: found %d files", tableName, len(allFiles))

		// 获取缓冲区中该表的所有数据
		tableBufferFiles := q.getAllBufferFilesForTable(tableName)
		allFiles = append(allFiles, tableBufferFiles...)
	}

	return allFiles, nil
}

// getBufferFilesForTableAndID 获取指定表和ID在缓冲区中的所有数据文件
func (q *Querier) getBufferFilesForTableAndID(tableName, id string) []string {
	var tempFiles []string
	bufferKeys := q.buffer.GetTableBufferKeys(tableName)

	idPrefix := fmt.Sprintf("%s/%s/", tableName, id)
	for _, key := range bufferKeys {
		if strings.HasPrefix(key, idPrefix) {
			rows := q.buffer.Get(key)
			if len(rows) > 0 {
				tempFilePath := q.createTempFilePath(key)
				if err := q.ensureTempDir(); err == nil {
					if err := q.buffer.WriteTempParquetFile(tempFilePath, rows); err == nil {
						tempFiles = append(tempFiles, tempFilePath)
						log.Printf("Wrote %d buffered rows to %s", len(rows), tempFilePath)
					}
				}
			}
		}
	}
	return tempFiles
}

// getBufferFilesForTableAndDay 获取指定表和天在缓冲区中的所有数据文件
func (q *Querier) getBufferFilesForTableAndDay(tableName, day string) []string {
	var tempFiles []string
	bufferKeys := q.buffer.GetTableBufferKeys(tableName)

	daySuffix := fmt.Sprintf("/%s", day)
	for _, key := range bufferKeys {
		if strings.HasSuffix(key, daySuffix) {
			rows := q.buffer.Get(key)
			if len(rows) > 0 {
				tempFilePath := q.createTempFilePath(key)
				if err := q.ensureTempDir(); err == nil {
					if err := q.buffer.WriteTempParquetFile(tempFilePath, rows); err == nil {
						tempFiles = append(tempFiles, tempFilePath)
						log.Printf("Wrote %d buffered rows to %s", len(rows), tempFilePath)
					}
				}
			}
		}
	}
	return tempFiles
}

// getAllBufferFilesForTable 获取指定表在缓冲区中的所有数据文件
func (q *Querier) getAllBufferFilesForTable(tableName string) []string {
	var tempFiles []string
	bufferKeys := q.buffer.GetTableBufferKeys(tableName)

	for _, key := range bufferKeys {
		rows := q.buffer.Get(key)
		if len(rows) > 0 {
			tempFilePath := q.createTempFilePath(key)
			if err := q.ensureTempDir(); err == nil {
				if err := q.buffer.WriteTempParquetFile(tempFilePath, rows); err == nil {
					tempFiles = append(tempFiles, tempFilePath)
					log.Printf("Wrote %d buffered rows to %s", len(rows), tempFilePath)
				}
			}
		}
	}
	return tempFiles
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

	// 2. 提取表名
	tables := q.extractTablesFromSQL(sqlQuery)

	// 3. 根据过滤器和表名获取相关的数据文件
	allFiles, err := q.getDataFiles(ctx, filter, tables)
	if err != nil {
		return "", fmt.Errorf("failed to get data files: %w", err)
	}

	if len(allFiles) == 0 {
		return "[]", nil // No data found, return empty JSON array
	}

	// 4. 记录临时文件以便后续清理
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

	// 5. 构造并执行最终的DuckDB查询
	fileList := "'" + strings.Join(allFiles, "','") + "'"
	finalQuery := strings.Replace(sqlQuery, "FROM table", fmt.Sprintf("FROM read_parquet([%s])", fileList), 1)

	log.Printf("Final DuckDB query: %s", finalQuery)

	rows, err := q.db.Query(finalQuery)
	if err != nil {
		return "", fmt.Errorf("duckdb query failed: %w", err)
	}
	defer rows.Close()

	// 6. 将结果序列化为JSON
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
