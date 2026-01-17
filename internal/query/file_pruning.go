package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"minIODB/internal/storage"
	"minIODB/pkg/logger"

	"go.uber.org/zap"
)

type Predicate struct {
	Column   string
	Operator string
	Value    interface{}
}

type FilePruner struct {
	predicates []Predicate
}

func NewFilePruner() *FilePruner {
	return &FilePruner{
		predicates: make([]Predicate, 0),
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
	patterns := []struct {
		regex    *regexp.Regexp
		operator string
	}{
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

	for _, p := range patterns {
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
		logger.GetLogger().Debug("Pruned files based on predicates",
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

type RowGroupPruner struct{}

func NewRowGroupPruner() *RowGroupPruner {
	return &RowGroupPruner{}
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
	filePruner     *FilePruner
	rowGroupPruner *RowGroupPruner
}

func NewQueryOptimizer() *QueryOptimizer {
	return &QueryOptimizer{
		filePruner:     NewFilePruner(),
		rowGroupPruner: NewRowGroupPruner(),
	}
}

func (qo *QueryOptimizer) OptimizeQuery(sql string, files []*storage.FileMetadata) (*OptimizedQuery, error) {
	predicates := qo.filePruner.ExtractPredicates(sql)

	prunedFiles := qo.filePruner.PruneFiles(files, predicates)

	hints := qo.rowGroupPruner.GetPushdownHints(predicates)

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
