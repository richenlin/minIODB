package security

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// SQLSanitizer 提供 SQL 安全相关的工具函数
type SQLSanitizer struct {
	// 允许的表名正则表达式
	tableNameRegex *regexp.Regexp
	// 允许的 ID 正则表达式
	idRegex *regexp.Regexp
}

// NewSQLSanitizer 创建新的 SQL 清理器
func NewSQLSanitizer() *SQLSanitizer {
	return &SQLSanitizer{
		// 表名只允许字母、数字、下划线，且必须以字母开头
		tableNameRegex: regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`),
		// ID 只允许字母、数字、连字符和下划线
		idRegex: regexp.MustCompile(`^[a-zA-Z0-9_-]+$`),
	}
}

// QuoteIdentifier 安全地引用 SQL 标识符（表名、列名等）
// 使用双引号包裹并转义内部的双引号
func (s *SQLSanitizer) QuoteIdentifier(identifier string) string {
	// 将内部的双引号替换为两个双引号（SQL 标准转义方式）
	escaped := strings.ReplaceAll(identifier, `"`, `""`)
	return `"` + escaped + `"`
}

// QuoteLiteral 安全地引用 SQL 字符串字面量
// 使用单引号包裹并转义内部的单引号
func (s *SQLSanitizer) QuoteLiteral(literal string) string {
	// 将内部的单引号替换为两个单引号（SQL 标准转义方式）
	escaped := strings.ReplaceAll(literal, `'`, `''`)
	// 移除或转义可能的控制字符
	escaped = s.sanitizeControlChars(escaped)
	return `'` + escaped + `'`
}

// sanitizeControlChars 移除或转义控制字符
func (s *SQLSanitizer) sanitizeControlChars(str string) string {
	var result strings.Builder
	for _, r := range str {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			// 跳过大多数控制字符，但保留换行和制表符
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// ValidateTableName 验证表名是否安全
func (s *SQLSanitizer) ValidateTableName(tableName string) error {
	if tableName == "" {
		return fmt.Errorf("table name cannot be empty")
	}

	if len(tableName) > 128 {
		return fmt.Errorf("table name too long: maximum 128 characters")
	}

	if !s.tableNameRegex.MatchString(tableName) {
		return fmt.Errorf("invalid table name: %q, must start with letter and contain only letters, numbers, underscores", tableName)
	}

	// 检查 SQL 保留关键字
	if s.isSQLKeyword(tableName) {
		return fmt.Errorf("table name cannot be a SQL reserved keyword: %s", tableName)
	}

	return nil
}

// ValidateID 验证 ID 是否安全
func (s *SQLSanitizer) ValidateID(id string) error {
	if id == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	if len(id) > 255 {
		return fmt.Errorf("ID too long: maximum 255 characters")
	}

	if !s.idRegex.MatchString(id) {
		return fmt.Errorf("invalid ID: %q, must contain only letters, numbers, dashes, underscores", id)
	}

	return nil
}

// isSQLKeyword 检查是否为 SQL 保留关键字
func (s *SQLSanitizer) isSQLKeyword(word string) bool {
	keywords := map[string]bool{
		"select": true, "from": true, "where": true, "and": true, "or": true,
		"insert": true, "into": true, "values": true, "update": true, "set": true,
		"delete": true, "drop": true, "create": true, "table": true, "index": true,
		"alter": true, "add": true, "column": true, "truncate": true, "grant": true,
		"revoke": true, "union": true, "join": true, "left": true, "right": true,
		"inner": true, "outer": true, "on": true, "group": true, "by": true,
		"order": true, "having": true, "limit": true, "offset": true, "as": true,
		"distinct": true, "count": true, "sum": true, "avg": true, "max": true,
		"min": true, "null": true, "not": true, "in": true, "exists": true,
		"between": true, "like": true, "is": true, "case": true, "when": true,
		"then": true, "else": true, "end": true, "primary": true, "key": true,
		"foreign": true, "references": true, "constraint": true, "unique": true,
		"view": true, "database": true, "schema": true, "use": true, "exec": true,
		"execute": true, "procedure": true, "function": true, "trigger": true,
	}
	return keywords[strings.ToLower(word)]
}

// BuildSafeDeleteSQL 构建安全的 DELETE SQL 语句
func (s *SQLSanitizer) BuildSafeDeleteSQL(tableName, id string) (string, error) {
	// 验证表名
	if err := s.ValidateTableName(tableName); err != nil {
		return "", fmt.Errorf("invalid table name: %w", err)
	}

	// 验证 ID
	if err := s.ValidateID(id); err != nil {
		return "", fmt.Errorf("invalid ID: %w", err)
	}

	// 使用安全的引用方式构建 SQL
	// DuckDB 使用标准 SQL 语法
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = %s",
		s.QuoteIdentifier(tableName),
		s.QuoteLiteral(id))

	return sql, nil
}

// BuildSafeSelectSQL 构建安全的 SELECT SQL 语句（用于验证表存在性等）
func (s *SQLSanitizer) BuildSafeSelectSQL(tableName string, columns []string, whereClause string) (string, error) {
	// 验证表名
	if err := s.ValidateTableName(tableName); err != nil {
		return "", fmt.Errorf("invalid table name: %w", err)
	}

	// 构建列列表
	columnList := "*"
	if len(columns) > 0 {
		var quotedColumns []string
		for _, col := range columns {
			if err := s.ValidateTableName(col); err != nil {
				return "", fmt.Errorf("invalid column name %q: %w", col, err)
			}
			quotedColumns = append(quotedColumns, s.QuoteIdentifier(col))
		}
		columnList = strings.Join(quotedColumns, ", ")
	}

	sql := fmt.Sprintf("SELECT %s FROM %s", columnList, s.QuoteIdentifier(tableName))

	if whereClause != "" {
		sql += " WHERE " + whereClause
	}

	return sql, nil
}

// BuildSafeDropViewSQL 构建安全的 DROP VIEW SQL 语句
func (s *SQLSanitizer) BuildSafeDropViewSQL(viewName string) (string, error) {
	// 验证视图名
	if err := s.ValidateTableName(viewName); err != nil {
		return "", fmt.Errorf("invalid view name: %w", err)
	}

	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s", s.QuoteIdentifier(viewName))
	return sql, nil
}

// BuildSafeCreateViewSQL 构建安全的 CREATE VIEW SQL 语句
func (s *SQLSanitizer) BuildSafeCreateViewSQL(viewName string, filePaths []string) (string, error) {
	// 验证视图名
	if err := s.ValidateTableName(viewName); err != nil {
		return "", fmt.Errorf("invalid view name: %w", err)
	}

	// 安全地引用文件路径
	var quotedPaths []string
	for _, path := range filePaths {
		// 文件路径使用字面量引用
		quotedPaths = append(quotedPaths, s.QuoteLiteral(path))
	}

	sql := fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM read_parquet([%s])",
		s.QuoteIdentifier(viewName),
		strings.Join(quotedPaths, ", "))

	return sql, nil
}

// ValidateSelectQuery 验证 SELECT 查询是否安全
// 这是一个基本的验证，检查常见的危险模式
func (s *SQLSanitizer) ValidateSelectQuery(sql string) error {
	if sql == "" {
		return fmt.Errorf("SQL query cannot be empty")
	}

	if len(sql) > 10000 {
		return fmt.Errorf("SQL query too long: maximum 10000 characters")
	}

	// 规范化查询以便检查
	normalizedSQL := strings.ToLower(strings.TrimSpace(sql))

	// 检查是否是 SELECT 语句
	if !strings.HasPrefix(normalizedSQL, "select") {
		return fmt.Errorf("only SELECT statements are allowed")
	}

	// 检查危险的关键字组合
	dangerousPatterns := []struct {
		pattern string
		message string
	}{
		{"drop ", "DROP statements are not allowed"},
		{"delete ", "DELETE statements are not allowed"},
		{"truncate ", "TRUNCATE statements are not allowed"},
		{"alter ", "ALTER statements are not allowed"},
		{"create ", "CREATE statements are not allowed"},
		{"insert ", "INSERT statements are not allowed"},
		{"update ", "UPDATE statements are not allowed"},
		{"union ", "UNION statements are not allowed in basic queries"},
		{"; ", "Multiple statements are not allowed"},
		{"--", "SQL comments are not allowed"},
		{"/*", "SQL comments are not allowed"},
		{"xp_", "Extended stored procedures are not allowed"},
		{"exec ", "EXEC statements are not allowed"},
		{"execute ", "EXECUTE statements are not allowed"},
	}

	for _, dp := range dangerousPatterns {
		if strings.Contains(normalizedSQL, dp.pattern) {
			return fmt.Errorf("%s", dp.message)
		}
	}

	return nil
}

// DefaultSanitizer 是默认的 SQL 清理器实例
var DefaultSanitizer = NewSQLSanitizer()
