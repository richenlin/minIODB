package utils

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Validator 提供验证工具函数
type Validator struct {
	// 表名正则表达式
	tableNameRegex *regexp.Regexp
	// ID正则表达式
	idRegex *regexp.Regexp
}

// NewValidator 创建新的验证器
func NewValidator() *Validator {
	return &Validator{
		// 表名只允许字母、数字、下划线，且必须以字母开头
		tableNameRegex: regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`),
		// ID 只允许字母、数字、连字符和下划线
		idRegex: regexp.MustCompile(`^[a-zA-Z0-9_-]+$`),
	}
}

// ValidateTableName 验证表名是否安全
func (v *Validator) ValidateTableName(tableName string) error {
	if tableName == "" {
		return ErrTableNameEmpty
	}

	if len(tableName) < 1 || len(tableName) > 128 {
		return ErrTableNameInvalidLength
	}

	if !v.tableNameRegex.MatchString(tableName) {
		return ErrTableNameInvalidFormat
	}

	// 检查 SQL 保留关键字
	if v.isSQLKeyword(tableName) {
		return ErrTableNameReservedKeyword
	}

	return nil
}

// ValidateID 验证 ID 是否安全
func (v *Validator) ValidateID(id string) error {
	if id == "" {
		return ErrIDEmpty
	}

	if len(id) < 1 || len(id) > 255 {
		return ErrIDInvalidLength
	}

	if !v.idRegex.MatchString(id) {
		return ErrIDInvalidFormat
	}

	return nil
}

// ValidateEmail 验证邮箱地址格式
func (v *Validator) ValidateEmail(email string) error {
	if email == "" {
		return ErrEmailEmpty
	}

	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(email) {
		return ErrEmailInvalidFormat
	}

	return nil
}

// ValidateURL 验证URL格式
func (v *Validator) ValidateURL(url string) error {
	if url == "" {
		return ErrURLEmpty
	}

	urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(:[0-9]+)?(/.*)?$`)
	if !urlRegex.MatchString(url) {
		return ErrURLInvalidFormat
	}

	return nil
}

// SanitizeString 清理字符串中的危险字符
func (v *Validator) SanitizeString(str string) string {
	var result strings.Builder
	for _, r := range str {
		if !unicode.IsControl(r) || r == '\n' || r == '\r' || r == '\t' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// QuoteIdentifier 安全地引用 SQL 标识符
func (v *Validator) QuoteIdentifier(identifier string) string {
	escaped := strings.ReplaceAll(identifier, `"`, `""`)
	return `"` + escaped + `"`
}

// QuoteLiteral 安全地引用 SQL 字符串字面量
func (v *Validator) QuoteLiteral(literal string) string {
	escaped := v.SanitizeString(literal)
	escaped = strings.ReplaceAll(escaped, `'`, `''`)
	return `'` + escaped + `'`
}

// isSQLKeyword 检查是否为 SQL 保留关键字
func (v *Validator) isSQLKeyword(word string) bool {
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

// 错误定义
var (
	ErrTableNameEmpty           = fmt.Errorf("table name cannot be empty")
	ErrTableNameInvalidLength   = fmt.Errorf("table name length must be between 1 and 128 characters")
	ErrTableNameInvalidFormat   = fmt.Errorf("table name must start with a letter and contain only letters, numbers, and underscores")
	ErrTableNameReservedKeyword = fmt.Errorf("table name cannot be a SQL reserved keyword")

	ErrIDEmpty         = fmt.Errorf("ID cannot be empty")
	ErrIDInvalidLength = fmt.Errorf("ID length must be between 1 and 255 characters")
	ErrIDInvalidFormat = fmt.Errorf("ID must contain only letters, numbers, hyphens, and underscores")

	ErrEmailEmpty         = fmt.Errorf("email cannot be empty")
	ErrEmailInvalidFormat = fmt.Errorf("invalid email format")

	ErrURLEmpty         = fmt.Errorf("URL cannot be empty")
	ErrURLInvalidFormat = fmt.Errorf("invalid URL format, must start with http:// or https://")
)

// DefaultValidator 默认验证器实例
var DefaultValidator = NewValidator()
