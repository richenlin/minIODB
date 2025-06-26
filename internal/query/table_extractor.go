package query

import (
	"regexp"
	"strings"
)

// SimpleTableExtractor 简化的表名提取器
// 专注于从SQL中提取表名，不进行复杂的语法解析
type SimpleTableExtractor struct{}

// NewSimpleTableExtractor 创建新的简化表名提取器
func NewSimpleTableExtractor() *SimpleTableExtractor {
	return &SimpleTableExtractor{}
}

// ExtractTableNames 从SQL中提取表名
// 支持单表和多表查询，包括JOIN操作
func (e *SimpleTableExtractor) ExtractTableNames(sql string) []string {
	// 清理SQL，转换为小写便于匹配
	cleanSQL := strings.ToLower(strings.TrimSpace(sql))

	// 如果SQL为空，返回空切片
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

	// 3. 去重并返回
	result := e.uniqueStrings(tables)
	if result == nil {
		return []string{}
	}
	return result
}

// extractFromTables 提取FROM子句中的表名
func (e *SimpleTableExtractor) extractFromTables(sql string) []string {
	// 匹配 FROM table_name 或 FROM table_name AS alias
	// 支持多种格式：FROM users, FROM users u, FROM users AS u
	re := regexp.MustCompile(`(?i)\bfrom\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:as\s+[a-zA-Z_][a-zA-Z0-9_]*)?`)
	matches := re.FindAllStringSubmatch(sql, -1)

	var tables []string
	for _, match := range matches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	if tables == nil {
		return []string{}
	}
	return tables
}

// extractJoinTables 提取JOIN子句中的表名
func (e *SimpleTableExtractor) extractJoinTables(sql string) []string {
	// 匹配各种JOIN类型：JOIN, LEFT JOIN, RIGHT JOIN, INNER JOIN, OUTER JOIN
	// 格式：JOIN table_name 或 JOIN table_name AS alias
	re := regexp.MustCompile(`(?i)\b(?:left\s+|right\s+|inner\s+|outer\s+|full\s+)?join\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:as\s+[a-zA-Z_][a-zA-Z0-9_]*)?`)
	matches := re.FindAllStringSubmatch(sql, -1)

	var tables []string
	for _, match := range matches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	if tables == nil {
		return []string{}
	}
	return tables
}

// uniqueStrings 去重字符串切片
func (e *SimpleTableExtractor) uniqueStrings(input []string) []string {
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
func (e *SimpleTableExtractor) ValidateTableNames(tables []string) []string {
	if tables == nil || len(tables) == 0 {
		return []string{}
	}

	var validTables []string

	// 简单的表名验证：字母、数字、下划线，不能以数字开头
	tableNameRe := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	for _, table := range tables {
		if tableNameRe.MatchString(table) {
			validTables = append(validTables, table)
		}
	}

	if validTables == nil {
		return []string{}
	}
	return validTables
}
