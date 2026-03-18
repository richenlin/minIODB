package query

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"minIODB/internal/storage"

	"go.uber.org/zap"
)

// =============================================================================
// 预编译正则表达式 - 避免热路径重复编译
// =============================================================================

// comparisonPattern 预编译的比较操作符正则模式
type comparisonPattern struct {
	regex    *regexp.Regexp
	operator string
}

// comparisonPatterns 预编译的谓词解析正则（用于 parseSimplePredicates）
var comparisonPatterns = []comparisonPattern{
	{regexp.MustCompile(`(\w+)\s*>=\s*'([^']*)'`), ">="},
	{regexp.MustCompile(`(\w+)\s*<=\s*'([^']*)'`), "<="},
	{regexp.MustCompile(`(\w+)\s*>\s*'([^']*)'`), ">"},
	{regexp.MustCompile(`(\w+)\s*<\s*'([^']*)'`), "<"},
	{regexp.MustCompile(`(\w+)\s*=\s*'([^']*)'`), "="},
	{regexp.MustCompile(`(\w+)\s*>=\s*(\d+)`), ">="},
	{regexp.MustCompile(`(\w+)\s*<=\s*(\d+)`), "<="},
	{regexp.MustCompile(`(\w+)\s*>\s*(\d+)`), ">"},
	{regexp.MustCompile(`(\w+)\s*<\s*(\d+)`), "<"},
	{regexp.MustCompile(`(\w+)\s*=\s*(\d+)`), "="},
}

// timeConditionPatterns 预编译的时间条件正则缓存
// key: columnName, value: map[string]*regexp.Regexp (operator -> pattern)
var timeConditionPatterns sync.Map

// getTimeConditionPattern 获取或创建时间条件正则
func getTimeConditionPattern(columnName, operator string) *regexp.Regexp {
	key := columnName + "_" + operator
	if v, ok := timeConditionPatterns.Load(key); ok {
		return v.(*regexp.Regexp)
	}

	var pattern string
	switch operator {
	case ">=":
		pattern = columnName + `\s*>=\s*'([^']*)'`
	case ">":
		pattern = columnName + `\s*>\s*'([^']*)'`
	case "<=":
		pattern = columnName + `\s*<=\s*'([^']*)'`
	case "<":
		pattern = columnName + `\s*<\s*'([^']*)'`
	case "=":
		pattern = columnName + `\s*=\s*'([^']*)'`
	}

	re := regexp.MustCompile(pattern)
	timeConditionPatterns.Store(key, re)
	return re
}

// datePartitionPattern 预编译的日期分区正则模式
type datePartitionPattern struct {
	regex  *regexp.Regexp
	layout string
}

// datePartitionPatterns 预编译的日期分区正则（用于 extractPartitionTimeFromPath）
var datePartitionPatterns = []datePartitionPattern{
	// YYYY-MM-DD 格式
	{regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})$`), "2006-01-02"},
	// date=YYYY-MM-DD 格式
	{regexp.MustCompile(`^date=(\d{4}-\d{2}-\d{2})$`), "2006-01-02"},
	// YYYYMMDD 格式
	{regexp.MustCompile(`^(\d{8})$`), "20060102"},
	// date=YYYYMMDD 格式
	{regexp.MustCompile(`^date=(\d{8})$`), "20060102"},
}

type Predicate struct {
	Column   string
	Operator string
	Value    interface{}
}

type FilePruner struct {
	predicates []Predicate
	logger     *zap.Logger
}

func NewFilePruner(logger *zap.Logger) *FilePruner {
	return &FilePruner{
		predicates: make([]Predicate, 0),
		logger:     logger,
	}
}

func (fp *FilePruner) ExtractPredicates(sql string) []Predicate {
	fp.predicates = fp.predicates[:0]

	sql = strings.ToLower(sql)

	whereIdx := strings.Index(sql, "where")
	if whereIdx == -1 {
		return fp.predicates
	}

	whereClause := sql[whereIdx+5:]

	endKeywords := []string{" group ", " order ", " limit ", " having "}
	for _, kw := range endKeywords {
		if idx := strings.Index(whereClause, kw); idx != -1 {
			whereClause = whereClause[:idx]
		}
	}

	fp.parseSimplePredicates(whereClause)

	return fp.predicates
}

func (fp *FilePruner) parseSimplePredicates(whereClause string) {
	// 使用预编译的正则表达式，避免热路径重复编译
	for _, p := range comparisonPatterns {
		matches := p.regex.FindAllStringSubmatch(whereClause, -1)
		for _, match := range matches {
			if len(match) >= 3 {
				column := match[1]
				value := match[2]

				var typedValue interface{}
				if num, err := strconv.ParseInt(value, 10, 64); err == nil {
					typedValue = num
				} else if num, err := strconv.ParseFloat(value, 64); err == nil {
					typedValue = num
				} else {
					typedValue = value
				}

				fp.predicates = append(fp.predicates, Predicate{
					Column:   column,
					Operator: p.operator,
					Value:    typedValue,
				})
			}
		}
	}
}

func (fp *FilePruner) PruneFiles(files []*storage.FileMetadata, predicates []Predicate) []*storage.FileMetadata {
	if len(predicates) == 0 {
		return files
	}

	var result []*storage.FileMetadata

	for _, file := range files {
		if fp.fileMatchesPredicates(file, predicates) {
			result = append(result, file)
		}
	}

	prunedCount := len(files) - len(result)
	if prunedCount > 0 {
		fp.logger.Debug("Pruned files based on predicates",
			zap.Int("original", len(files)),
			zap.Int("remaining", len(result)),
			zap.Int("pruned", prunedCount))
	}

	return result
}

func (fp *FilePruner) fileMatchesPredicates(file *storage.FileMetadata, predicates []Predicate) bool {
	for _, pred := range predicates {
		minVal, hasMin := file.MinValues[pred.Column]
		maxVal, hasMax := file.MaxValues[pred.Column]

		if !hasMin || !hasMax {
			continue
		}

		if !fp.rangeMatchesPredicate(minVal, maxVal, pred) {
			return false
		}
	}

	return true
}

func (fp *FilePruner) rangeMatchesPredicate(minVal, maxVal interface{}, pred Predicate) bool {
	switch pred.Operator {
	case ">":
		return fp.compareValues(maxVal, pred.Value) > 0
	case ">=":
		return fp.compareValues(maxVal, pred.Value) >= 0
	case "<":
		return fp.compareValues(minVal, pred.Value) < 0
	case "<=":
		return fp.compareValues(minVal, pred.Value) <= 0
	case "=":
		return fp.compareValues(minVal, pred.Value) <= 0 && fp.compareValues(maxVal, pred.Value) >= 0
	}

	return true
}

func (fp *FilePruner) compareValues(a, b interface{}) int {
	switch va := a.(type) {
	case int64:
		if vb, ok := b.(int64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case float64:
		if vb, ok := b.(float64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case string:
		if vb, ok := b.(string); ok {
			return strings.Compare(va, vb)
		}
	case time.Time:
		if vb, ok := b.(time.Time); ok {
			if va.Before(vb) {
				return -1
			} else if va.After(vb) {
				return 1
			}
			return 0
		}
	}

	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return strings.Compare(aStr, bStr)
}

type RowGroupPruner struct {
	logger *zap.Logger
}

func NewRowGroupPruner(logger *zap.Logger) *RowGroupPruner {
	return &RowGroupPruner{
		logger: logger,
	}
}

func (rp *RowGroupPruner) GeneratePushdownSQL(originalSQL string, predicates []Predicate) string {
	return originalSQL
}

func (rp *RowGroupPruner) GetPushdownHints(predicates []Predicate) map[string]interface{} {
	hints := make(map[string]interface{})

	var columns []string
	for _, p := range predicates {
		columns = append(columns, p.Column)
	}

	if len(columns) > 0 {
		hints["filter_columns"] = columns
		hints["pushdown_enabled"] = true
	}

	return hints
}

type QueryOptimizer struct {
	filePruner          *FilePruner
	rowGroupPruner      *RowGroupPruner
	timePartitionPruner *TimePartitionPruner
}

func NewQueryOptimizer(logger *zap.Logger) *QueryOptimizer {
	return &QueryOptimizer{
		filePruner:          NewFilePruner(logger),
		rowGroupPruner:      NewRowGroupPruner(logger),
		timePartitionPruner: NewTimePartitionPruner(logger),
	}
}

func (qo *QueryOptimizer) OptimizeQuery(sql string, files []*storage.FileMetadata) (*OptimizedQuery, error) {
	// 1. 提取谓词条件
	predicates := qo.filePruner.ExtractPredicates(sql)

	// 2. 提取时间范围并进行时间分区裁剪
	qo.timePartitionPruner.ExtractTimeRange(sql)
	prunedByTime := qo.timePartitionPruner.PruneFilesByTimePartition(files)
	timePrunedCount := len(files) - len(prunedByTime)

	// 3. 基于谓词进行文件级剪枝
	prunedFiles := qo.filePruner.PruneFiles(prunedByTime, predicates)
	predicatePrunedCount := len(prunedByTime) - len(prunedFiles)

	hints := qo.rowGroupPruner.GetPushdownHints(predicates)
	hints["time_partition_pruned"] = timePrunedCount
	hints["predicate_pruned"] = predicatePrunedCount

	return &OptimizedQuery{
		OriginalSQL:   sql,
		OptimizedSQL:  sql,
		SelectedFiles: prunedFiles,
		Predicates:    predicates,
		PushdownHints: hints,
		FilesSkipped:  len(files) - len(prunedFiles),
		EstimatedRows: qo.estimateRows(prunedFiles),
	}, nil
}

func (qo *QueryOptimizer) estimateRows(files []*storage.FileMetadata) int64 {
	var total int64
	for _, f := range files {
		total += f.RowCount
	}
	return total
}

type OptimizedQuery struct {
	OriginalSQL   string
	OptimizedSQL  string
	SelectedFiles []*storage.FileMetadata
	Predicates    []Predicate
	PushdownHints map[string]interface{}
	FilesSkipped  int
	EstimatedRows int64
}

// =============================================================================
// TimePartitionPruner - 时间分区裁剪器
// =============================================================================

// TimeRange 表示时间范围
type TimeRange struct {
	MinTime time.Time
	MaxTime time.Time
	HasMin  bool
	HasMax  bool
}

// TimePartitionPruner 时间分区裁剪器
// 用于根据 SQL 中的时间范围条件裁剪分区，避免扫描全部日期目录
type TimePartitionPruner struct {
	timeRange TimeRange
	logger    *zap.Logger
}

// NewTimePartitionPruner 创建时间分区裁剪器
func NewTimePartitionPruner(logger *zap.Logger) *TimePartitionPruner {
	return &TimePartitionPruner{
		logger: logger,
	}
}

// ExtractTimeRange 从 SQL 中提取时间范围条件
// 支持 timestamp/time/created_at/date 等常见时间列名
func (tp *TimePartitionPruner) ExtractTimeRange(sql string) TimeRange {
	tp.timeRange = TimeRange{}

	sqlLower := strings.ToLower(sql)

	whereIdx := strings.Index(sqlLower, "where")
	if whereIdx == -1 {
		return tp.timeRange
	}

	whereClause := sqlLower[whereIdx+5:]

	// 截断到其他子句
	endKeywords := []string{" group ", " order ", " limit ", " having "}
	for _, kw := range endKeywords {
		if idx := strings.Index(whereClause, kw); idx != -1 {
			whereClause = whereClause[:idx]
		}
	}

	// 常见的时间列名模式
	timeColumns := []string{"timestamp", "time", "created_at", "updated_at", "date", "datetime", "event_time", "occurred_at"}

	for _, col := range timeColumns {
		tp.extractTimeConditions(whereClause, col)
		if tp.timeRange.HasMin || tp.timeRange.HasMax {
			break
		}
	}

	return tp.timeRange
}

// extractTimeConditions 从 WHERE 子句中提取指定列的时间条件
func (tp *TimePartitionPruner) extractTimeConditions(whereClause, columnName string) {
	// 支持的时间格式
	timeFormats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999Z",
		"2006-01-02",
		time.RFC3339,
		time.RFC3339Nano,
	}

	// >= 条件 - 使用缓存的预编译正则
	gePattern := getTimeConditionPattern(columnName, ">=")
	if matches := gePattern.FindStringSubmatch(whereClause); len(matches) > 1 {
		if t, err := tp.parseTime(matches[1], timeFormats); err == nil {
			tp.timeRange.MinTime = t
			tp.timeRange.HasMin = true
		}
	}

	// > 条件 (转换为 >= 下一天) - 使用缓存的预编译正则
	gtPattern := getTimeConditionPattern(columnName, ">")
	if matches := gtPattern.FindStringSubmatch(whereClause); len(matches) > 1 {
		if t, err := tp.parseTime(matches[1], timeFormats); err == nil {
			tp.timeRange.MinTime = t.Add(time.Nanosecond)
			tp.timeRange.HasMin = true
		}
	}

	// <= 条件 - 使用缓存的预编译正则
	lePattern := getTimeConditionPattern(columnName, "<=")
	if matches := lePattern.FindStringSubmatch(whereClause); len(matches) > 1 {
		if t, err := tp.parseTime(matches[1], timeFormats); err == nil {
			tp.timeRange.MaxTime = t
			tp.timeRange.HasMax = true
		}
	}

	// < 条件 (转换为 <= 前一时刻) - 使用缓存的预编译正则
	ltPattern := getTimeConditionPattern(columnName, "<")
	if matches := ltPattern.FindStringSubmatch(whereClause); len(matches) > 1 {
		if t, err := tp.parseTime(matches[1], timeFormats); err == nil {
			tp.timeRange.MaxTime = t.Add(-time.Nanosecond)
			tp.timeRange.HasMax = true
		}
	}

	// = 条件 (单日查询) - 使用缓存的预编译正则
	eqPattern := getTimeConditionPattern(columnName, "=")
	if matches := eqPattern.FindStringSubmatch(whereClause); len(matches) > 1 {
		if t, err := tp.parseTime(matches[1], timeFormats); err == nil {
			// 如果只有日期部分，设置当天范围
			if len(matches[1]) <= 10 { // YYYY-MM-DD 格式
				tp.timeRange.MinTime = t
				tp.timeRange.MaxTime = t.Add(24*time.Hour - time.Second)
			} else {
				tp.timeRange.MinTime = t
				tp.timeRange.MaxTime = t
			}
			tp.timeRange.HasMin = true
			tp.timeRange.HasMax = true
		}
	}
}

// parseTime 尝试用多种格式解析时间
func (tp *TimePartitionPruner) parseTime(value string, formats []string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, format := range formats {
		if t, err := time.Parse(format, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", value)
}

// ShouldScanPartition 判断分区是否应该被扫描
// partitionTime 是分区对应的时间（通常是日期）
func (tp *TimePartitionPruner) ShouldScanPartition(partitionTime time.Time) bool {
	// 如果没有时间范围限制，扫描所有分区
	if !tp.timeRange.HasMin && !tp.timeRange.HasMax {
		return true
	}

	// 检查下界
	if tp.timeRange.HasMin && partitionTime.Before(tp.timeRange.MinTime) {
		// 分区时间早于最小时间，跳过
		// 但需要考虑分区可能是日期级别，而查询条件包含时间
		partitionEnd := tp.getEndOfDay(partitionTime)
		if partitionEnd.Before(tp.timeRange.MinTime) {
			return false
		}
	}

	// 检查上界
	if tp.timeRange.HasMax && partitionTime.After(tp.timeRange.MaxTime) {
		// 分区时间晚于最大时间，跳过
		partitionStart := tp.getStartOfDay(partitionTime)
		if partitionStart.After(tp.timeRange.MaxTime) {
			return false
		}
	}

	return true
}

// getStartOfDay 获取一天的开始时间（00:00:00）
func (tp *TimePartitionPruner) getStartOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// getEndOfDay 获取一天的结束时间（23:59:59）
func (tp *TimePartitionPruner) getEndOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
}

// PruneFilesByTimePartition 根据时间分区裁剪文件
// 从文件路径中提取日期分区并判断是否在查询时间范围内
func (tp *TimePartitionPruner) PruneFilesByTimePartition(files []*storage.FileMetadata) []*storage.FileMetadata {
	// 如果没有时间范围限制，返回所有文件
	if !tp.timeRange.HasMin && !tp.timeRange.HasMax {
		return files
	}

	var result []*storage.FileMetadata
	prunedCount := 0

	for _, file := range files {
		partitionTime, err := tp.extractPartitionTimeFromPath(file.FilePath)
		if err != nil {
			// 无法提取分区时间，保留文件（保守策略）
			result = append(result, file)
			continue
		}

		if tp.ShouldScanPartition(partitionTime) {
			result = append(result, file)
		} else {
			prunedCount++
		}
	}

	if prunedCount > 0 {
		tp.logger.Debug("Pruned files by time partition",
			zap.Int("original", len(files)),
			zap.Int("remaining", len(result)),
			zap.Int("pruned", prunedCount),
			zap.Time("min_time", tp.timeRange.MinTime),
			zap.Time("max_time", tp.timeRange.MaxTime))
	}

	return result
}

// extractPartitionTimeFromPath 从文件路径中提取分区时间
// 支持的路径格式：
// - table/2024-01-15/id/file.parquet
// - table/20240115/id/file.parquet
// - table/id/2024-01-15/file.parquet
// - table/date=2024-01-15/id/file.parquet
func (tp *TimePartitionPruner) extractPartitionTimeFromPath(filePath string) (time.Time, error) {
	// 清理路径，统一使用正斜杠
	filePath = filepath.ToSlash(filePath)
	filePath = strings.ReplaceAll(filePath, "\\", "/")
	parts := strings.Split(filePath, "/")

	// 使用预编译的日期分区正则
	for _, part := range parts {
		for _, pattern := range datePartitionPatterns {
			if pattern.regex.MatchString(part) {
				// 提取日期部分
				matches := pattern.regex.FindStringSubmatch(part)
				if len(matches) > 1 {
					dateStr := matches[1]
					// 使用对应的 layout 解析
					if t, err := time.Parse(pattern.layout, dateStr); err == nil {
						return t, nil
					}
				}
			}
		}
	}

	return time.Time{}, fmt.Errorf("no date partition found in path: %s", filePath)
}

// GetTimeRange 获取当前时间范围
func (tp *TimePartitionPruner) GetTimeRange() TimeRange {
	return tp.timeRange
}

// SetTimeRange 设置时间范围（用于手动设置）
func (tp *TimePartitionPruner) SetTimeRange(tr TimeRange) {
	tp.timeRange = tr
}
