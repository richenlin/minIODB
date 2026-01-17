package query

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// ColumnPruner 列剪枝优化器
type ColumnPruner struct {
	cache   map[string][]string
	cacheMu sync.RWMutex
	enabled bool
}

// NewColumnPruner 创建列剪枝优化器
func NewColumnPruner(enabled bool) *ColumnPruner {
	return &ColumnPruner{
		cache:   make(map[string][]string),
		enabled: enabled,
	}
}

// ExtractRequiredColumns 从SQL中提取需要的列
func (cp *ColumnPruner) ExtractRequiredColumns(sql string) []string {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return []string{}
	}

	if !cp.enabled {
		return []string{"*"}
	}

	cp.cacheMu.RLock()
	if cols, ok := cp.cache[sql]; ok {
		cp.cacheMu.RUnlock()
		return cols
	}
	cp.cacheMu.RUnlock()

	// 如果是 SELECT * 或 COUNT(*)，返回所有列
	upperSQL := strings.ToUpper(sql)
	if strings.Contains(upperSQL, "SELECT *") || strings.Contains(upperSQL, "COUNT(*)") {
		return []string{"*"}
	}

	// 提取 SELECT 和 FROM 之间的部分
	selectPattern := regexp.MustCompile(`(?i)SELECT\s+(.*?)\s+FROM`)
	matches := selectPattern.FindStringSubmatch(sql)
	if len(matches) < 2 {
		return []string{"*"}
	}

	columnsStr := strings.TrimSpace(matches[1])
	if columnsStr == "" {
		return []string{}
	}

	// 分割列名（支持逗号分隔）
	columns := strings.Split(columnsStr, ",")
	var requiredColumns []string

	for _, col := range columns {
		col = strings.TrimSpace(col)

		// 跳过聚合函数中的列
		if cp.isAggregateFunction(col) {
			continue
		}

		// 处理表别名 (table.column)
		if strings.Contains(col, ".") {
			parts := strings.Split(col, ".")
			if len(parts) == 2 {
				col = parts[1]
			}
		}

		// 跳过DISTINCT关键字
		col = strings.TrimPrefix(col, "DISTINCT ")
		col = strings.TrimPrefix(col, "DISTINCT")
		col = strings.TrimSpace(col)

		// 添加列（保持原始大小写）
		if col != "" && !cp.contains(requiredColumns, col) {
			requiredColumns = append(requiredColumns, col)
		}
	}

	if len(requiredColumns) == 0 {
		return []string{"*"}
	}

	return requiredColumns
}

// isAggregateFunction 判断是否为聚合函数
func (cp *ColumnPruner) isAggregateFunction(col string) bool {
	aggregateFuncs := []string{
		"COUNT(", "SUM(", "AVG(", "MIN(", "MAX(",
		"STDDEV(", "VARIANCE(", "GROUP_CONCAT(",
		"COUNT_DISTINCT(",
	}

	colUpper := strings.ToUpper(col)
	for _, fn := range aggregateFuncs {
		if strings.Contains(colUpper, fn) {
			return true
		}
	}

	return false
}

// contains 检查列是否已存在
func (cp *ColumnPruner) contains(cols []string, col string) bool {
	for _, c := range cols {
		if strings.EqualFold(c, col) {
			return true
		}
	}
	return false
}

// BuildOptimizedViewSQL 构建优化的视图SQL（只包含需要的列）
func (cp *ColumnPruner) BuildOptimizedViewSQL(tableName string, files []string, requiredColumns []string) string {
	if !cp.enabled || len(requiredColumns) == 0 || (len(requiredColumns) == 1 && requiredColumns[0] == "*") {
		return cp.buildStandardViewSQL(tableName, files)
	}

	// 构建列列表
	columnList := strings.Join(requiredColumns, ", ")

	// 读取Parquet文件时只指定需要的列
	columnsClause := columnList

	filesClause := cp.buildFilesClause(files)

	return fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS SELECT %s FROM read_parquet(['%s'])`,
		tableName, columnsClause, filesClause)
}

// buildStandardViewSQL 构建标准视图SQL（不使用列剪枝）
func (cp *ColumnPruner) buildStandardViewSQL(tableName string, files []string) string {
	filesClause := cp.buildFilesClause(files)

	return fmt.Sprintf(`CREATE OR REPLACE VIEW %s AS SELECT * FROM read_parquet(['%s'])`,
		tableName, filesClause)
}

// buildFilesClause 构建文件列表子句
func (cp *ColumnPruner) buildFilesClause(files []string) string {
	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, "'"+file+"'")
	}
	return strings.Join(fileNames, ",")
}

// ClearCache 清除缓存
func (cp *ColumnPruner) ClearCache() {
	cp.cacheMu.Lock()
	defer cp.cacheMu.Unlock()
	cp.cache = make(map[string][]string)
}

// GetCacheStats 获取缓存统计
func (cp *ColumnPruner) GetCacheStats() map[string]interface{} {
	cp.cacheMu.RLock()
	defer cp.cacheMu.RUnlock()

	return map[string]interface{}{
		"cache_size":    len(cp.cache),
		"enabled":       cp.enabled,
		"total_queries": 0,
	}
}
