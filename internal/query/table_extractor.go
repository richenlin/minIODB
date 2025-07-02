package query

import (
	"regexp"
	"strings"
)

// TableExtractor 增强的表名提取器
// 支持更复杂的SQL语法和查询类型识别
type TableExtractor struct {
	// 缓存编译后的正则表达式以提高性能
	fromRegex     *regexp.Regexp
	joinRegex     *regexp.Regexp
	updateRegex   *regexp.Regexp
	insertRegex   *regexp.Regexp
	deleteRegex   *regexp.Regexp
	withRegex     *regexp.Regexp
	subqueryRegex *regexp.Regexp
}

// NewTableExtractor 创建增强的表名提取器
func NewTableExtractor() *TableExtractor {
	return &TableExtractor{
		// FROM子句匹配（支持别名、模式名）
		fromRegex: regexp.MustCompile(`(?i)\bfrom\s+(?:(\w+\.)?(\w+))(?:\s+(?:as\s+)?(\w+))?`),
		
		// JOIN子句匹配（支持各种JOIN类型）
		joinRegex: regexp.MustCompile(`(?i)\b(?:inner\s+|left\s+(?:outer\s+)?|right\s+(?:outer\s+)?|full\s+(?:outer\s+)?|cross\s+)?join\s+(?:(\w+\.)?(\w+))(?:\s+(?:as\s+)?(\w+))?`),
		
		// UPDATE语句
		updateRegex: regexp.MustCompile(`(?i)\bupdate\s+(?:(\w+\.)?(\w+))(?:\s+(?:as\s+)?(\w+))?`),
		
		// INSERT语句
		insertRegex: regexp.MustCompile(`(?i)\binsert\s+into\s+(?:(\w+\.)?(\w+))`),
		
		// DELETE语句
		deleteRegex: regexp.MustCompile(`(?i)\bdelete\s+from\s+(?:(\w+\.)?(\w+))(?:\s+(?:as\s+)?(\w+))?`),
		
		// WITH子句（CTE - Common Table Expressions）
		withRegex: regexp.MustCompile(`(?i)\bwith\s+(\w+)\s+as\s*\(`),
		
		// 子查询中的表（简化处理）
		subqueryRegex: regexp.MustCompile(`(?i)\(\s*select\s+.*?\bfrom\s+(\w+)`),
	}
}

// ExtractTableNames 从SQL中提取表名（增强版本）
func (e *TableExtractor) ExtractTableNames(sql string) []string {
	// 清理SQL，去除注释和多余空格
	cleanSQL := e.cleanSQL(sql)
	
	if cleanSQL == "" {
		return []string{}
	}

	var tables []string
	
	// 1. 提取FROM子句中的表名
	fromTables := e.extractFromTables(cleanSQL)
	tables = append(tables, fromTables...)

	// 2. 提取JOIN子句中的表名  
	joinTables := e.extractJoinTables(cleanSQL)
	tables = append(tables, joinTables...)
	
	// 3. 提取UPDATE语句中的表名
	updateTables := e.extractUpdateTables(cleanSQL)
	tables = append(tables, updateTables...)
	
	// 4. 提取INSERT语句中的表名
	insertTables := e.extractInsertTables(cleanSQL)
	tables = append(tables, insertTables...)
	
	// 5. 提取DELETE语句中的表名
	deleteTables := e.extractDeleteTables(cleanSQL)
	tables = append(tables, deleteTables...)
	
	// 6. 提取WITH子句中的CTE表名（排除，因为这些是临时表）
	// cteTables := e.extractCTETables(cleanSQL)
	
	// 7. 提取子查询中的表名
	subqueryTables := e.extractSubqueryTables(cleanSQL)
	tables = append(tables, subqueryTables...)

	// 去重并返回
	result := e.uniqueStrings(tables)
	if result == nil {
		return []string{}
	}
	return result
}

// cleanSQL 清理SQL语句
func (e *TableExtractor) cleanSQL(sql string) string {
	// 移除单行注释
	singleLineComment := regexp.MustCompile(`--.*$`)
	sql = singleLineComment.ReplaceAllString(sql, "")
	
	// 移除多行注释
	multiLineComment := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	sql = multiLineComment.ReplaceAllString(sql, "")
	
	// 标准化空白字符
	sql = strings.TrimSpace(sql)
	sql = regexp.MustCompile(`\s+`).ReplaceAllString(sql, " ")
	
	return sql
}

// extractFromTables 提取FROM子句中的表名
func (e *TableExtractor) extractFromTables(sql string) []string {
	matches := e.fromRegex.FindAllStringSubmatch(sql, -1)
	
	var tables []string
	for _, match := range matches {
		if len(match) >= 3 && match[2] != "" {
			// match[1] 是模式名（可选）
			// match[2] 是表名
			// match[3] 是别名（可选）
			tableName := match[2]
			if e.isValidTableName(tableName) {
				tables = append(tables, tableName)
			}
		}
	}
	
	return tables
}

// extractJoinTables 提取JOIN子句中的表名
func (e *TableExtractor) extractJoinTables(sql string) []string {
	matches := e.joinRegex.FindAllStringSubmatch(sql, -1)
	
	var tables []string
	for _, match := range matches {
		if len(match) >= 3 && match[2] != "" {
			tableName := match[2]
			if e.isValidTableName(tableName) {
				tables = append(tables, tableName)
			}
		}
	}
	
	return tables
}

// extractUpdateTables 提取UPDATE语句中的表名
func (e *TableExtractor) extractUpdateTables(sql string) []string {
	matches := e.updateRegex.FindAllStringSubmatch(sql, -1)
	
	var tables []string
	for _, match := range matches {
		if len(match) >= 3 && match[2] != "" {
			tableName := match[2]
			if e.isValidTableName(tableName) {
				tables = append(tables, tableName)
			}
		}
	}
	
	return tables
}

// extractInsertTables 提取INSERT语句中的表名
func (e *TableExtractor) extractInsertTables(sql string) []string {
	matches := e.insertRegex.FindAllStringSubmatch(sql, -1)
	
	var tables []string
	for _, match := range matches {
		if len(match) >= 3 && match[2] != "" {
			tableName := match[2]
			if e.isValidTableName(tableName) {
				tables = append(tables, tableName)
			}
		}
	}
	
	return tables
}

// extractDeleteTables 提取DELETE语句中的表名
func (e *TableExtractor) extractDeleteTables(sql string) []string {
	matches := e.deleteRegex.FindAllStringSubmatch(sql, -1)
	
	var tables []string
	for _, match := range matches {
		if len(match) >= 3 && match[2] != "" {
			tableName := match[2]
			if e.isValidTableName(tableName) {
				tables = append(tables, tableName)
			}
		}
	}
	
	return tables
}

// extractSubqueryTables 提取子查询中的表名（简化处理）
func (e *TableExtractor) extractSubqueryTables(sql string) []string {
	matches := e.subqueryRegex.FindAllStringSubmatch(sql, -1)
	
	var tables []string
	for _, match := range matches {
		if len(match) >= 2 && match[1] != "" {
			tableName := match[1]
			if e.isValidTableName(tableName) {
				tables = append(tables, tableName)
			}
		}
	}
	
	return tables
}

// isValidTableName 验证表名是否有效
func (e *TableExtractor) isValidTableName(tableName string) bool {
	// 检查是否为SQL关键字
	keywords := map[string]bool{
		"select": true, "from": true, "where": true, "join": true,
		"inner": true, "left": true, "right": true, "outer": true,
		"on": true, "as": true, "and": true, "or": true, "not": true,
		"in": true, "exists": true, "between": true, "like": true,
		"is": true, "null": true, "true": true, "false": true,
		"union": true, "group": true, "by": true, "having": true,
		"order": true, "limit": true, "offset": true, "distinct": true,
		"count": true, "sum": true, "avg": true, "max": true, "min": true,
		"case": true, "when": true, "then": true, "else": true, "end": true,
		"insert": true, "update": true, "delete": true, "create": true,
		"drop": true, "alter": true, "table": true, "index": true,
		"view": true, "database": true, "schema": true,
	}
	
	lowerName := strings.ToLower(tableName)
	if keywords[lowerName] {
		return false
	}
	
	// 基本的表名验证：字母、数字、下划线，不能以数字开头
	tableNameRe := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	return tableNameRe.MatchString(tableName)
}

// uniqueStrings 去重字符串切片
func (e *TableExtractor) uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, str := range input {
		if str != "" && !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}

// ValidateTableNames 验证提取的表名是否有效
func (e *TableExtractor) ValidateTableNames(tables []string) []string {
	if tables == nil || len(tables) == 0 {
		return []string{}
	}

	var validTables []string
	for _, table := range tables {
		if e.isValidTableName(table) {
			validTables = append(validTables, table)
		}
	}

	if validTables == nil {
		return []string{}
	}
	return validTables
}

// GetQueryType 获取查询类型
func (e *TableExtractor) GetQueryType(sql string) string {
	sqlLower := strings.ToLower(strings.TrimSpace(sql))
	
	// 根据SQL语句开头确定类型
	if strings.HasPrefix(sqlLower, "select") {
		// 进一步细分SELECT查询类型
		if strings.Contains(sqlLower, "count(") {
			return "count"
		} else if strings.Contains(sqlLower, "sum(") || 
				  strings.Contains(sqlLower, "avg(") ||
				  strings.Contains(sqlLower, "max(") ||
				  strings.Contains(sqlLower, "min(") {
			return "aggregation"
		} else if strings.Contains(sqlLower, "group by") {
			return "group_by"
		} else if strings.Contains(sqlLower, "join") {
			return "join"
		} else if strings.Contains(sqlLower, "union") {
			return "union"
		} else {
			return "select"
		}
	} else if strings.HasPrefix(sqlLower, "insert") {
		return "insert"
	} else if strings.HasPrefix(sqlLower, "update") {
		return "update"
	} else if strings.HasPrefix(sqlLower, "delete") {
		return "delete"
	} else if strings.HasPrefix(sqlLower, "create") {
		return "create"
	} else if strings.HasPrefix(sqlLower, "drop") {
		return "drop"
	} else if strings.HasPrefix(sqlLower, "alter") {
		return "alter"
	} else if strings.HasPrefix(sqlLower, "with") {
		return "cte" // Common Table Expression
	} else {
		return "unknown"
	}
}

// GetQueryComplexity 获取查询复杂度评估
func (e *TableExtractor) GetQueryComplexity(sql string) string {
	sqlLower := strings.ToLower(sql)
	
	complexity := 0
	
	// 基础复杂度因子
	if strings.Contains(sqlLower, "join") {
		complexity += 2
	}
	if strings.Contains(sqlLower, "subquery") || strings.Count(sqlLower, "(") > 1 {
		complexity += 3
	}
	if strings.Contains(sqlLower, "group by") {
		complexity += 2
	}
	if strings.Contains(sqlLower, "having") {
		complexity += 1
	}
	if strings.Contains(sqlLower, "order by") {
		complexity += 1
	}
	if strings.Contains(sqlLower, "union") {
		complexity += 2
	}
	if strings.Contains(sqlLower, "with") {
		complexity += 2
	}
	
	// 表数量复杂度
	tables := e.ExtractTableNames(sql)
	complexity += len(tables) - 1 // 单表查询不增加复杂度
	
	// 函数复杂度
	functions := []string{"count(", "sum(", "avg(", "max(", "min(", "case"}
	for _, fn := range functions {
		if strings.Contains(sqlLower, fn) {
			complexity += 1
		}
	}
	
	// 返回复杂度等级
	if complexity <= 2 {
		return "simple"
	} else if complexity <= 5 {
		return "medium"
	} else if complexity <= 10 {
		return "complex"
	} else {
		return "very_complex"
	}
}

// ShouldCacheQuery 判断查询是否应该被缓存
func (e *TableExtractor) ShouldCacheQuery(sql string) bool {
	queryType := e.GetQueryType(sql)
	complexity := e.GetQueryComplexity(sql)
	
	// 不缓存的查询类型
	noCacheTypes := map[string]bool{
		"insert": true,
		"update": true,
		"delete": true,
		"create": true,
		"drop":   true,
		"alter":  true,
	}
	
	if noCacheTypes[queryType] {
		return false
	}
	
	// 简单查询和SELECT查询适合缓存
	if queryType == "select" || queryType == "count" || queryType == "aggregation" {
		// 复杂查询结果更有价值，更应该缓存
		return complexity == "medium" || complexity == "complex"
	}
	
	return false
} 