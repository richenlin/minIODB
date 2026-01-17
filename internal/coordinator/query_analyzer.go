package coordinator

import (
	"regexp"
	"strings"
)

type QueryType int

const (
	QueryTypeSimple QueryType = iota
	QueryTypeAggregate
	QueryTypeGroupBy
	QueryTypeOrderBy
	QueryTypeJoin
)

type AggregationType int

const (
	AggNone AggregationType = iota
	AggCount
	AggSum
	AggAvg
	AggMin
	AggMax
)

type OrderDirection int

const (
	OrderAsc OrderDirection = iota
	OrderDesc
)

type Aggregation struct {
	Type   AggregationType
	Column string
	Alias  string
}

type OrderBy struct {
	Column    string
	Direction OrderDirection
}

type AnalyzedQuery struct {
	Type         QueryType
	Aggregations []Aggregation
	GroupByKeys  []string
	OrderByKeys  []OrderBy
	Limit        int
	HasDistinct  bool
	Tables       []string
	IsSubQuery   bool
}

type QueryAnalyzer struct {
	countPattern   *regexp.Regexp
	sumPattern     *regexp.Regexp
	avgPattern     *regexp.Regexp
	minPattern     *regexp.Regexp
	maxPattern     *regexp.Regexp
	groupByPattern *regexp.Regexp
	orderByPattern *regexp.Regexp
	limitPattern   *regexp.Regexp
}

func NewQueryAnalyzer() *QueryAnalyzer {
	return &QueryAnalyzer{
		countPattern:   regexp.MustCompile(`(?i)count\s*\(\s*(\*|\w+)\s*\)(?:\s+(?:as\s+)?(\w+))?`),
		sumPattern:     regexp.MustCompile(`(?i)sum\s*\(\s*(\w+)\s*\)(?:\s+(?:as\s+)?(\w+))?`),
		avgPattern:     regexp.MustCompile(`(?i)avg\s*\(\s*(\w+)\s*\)(?:\s+(?:as\s+)?(\w+))?`),
		minPattern:     regexp.MustCompile(`(?i)min\s*\(\s*(\w+)\s*\)(?:\s+(?:as\s+)?(\w+))?`),
		maxPattern:     regexp.MustCompile(`(?i)max\s*\(\s*(\w+)\s*\)(?:\s+(?:as\s+)?(\w+))?`),
		groupByPattern: regexp.MustCompile(`(?i)group\s+by\s+(\w+(?:\s*,\s*\w+)*)`),
		orderByPattern: regexp.MustCompile(`(?i)order\s+by\s+(\w+)(?:\s+(asc|desc))?`),
		limitPattern:   regexp.MustCompile(`(?i)limit\s+(\d+)`),
	}
}

func (qa *QueryAnalyzer) Analyze(sql string) *AnalyzedQuery {
	result := &AnalyzedQuery{
		Type:         QueryTypeSimple,
		Aggregations: make([]Aggregation, 0),
		GroupByKeys:  make([]string, 0),
		OrderByKeys:  make([]OrderBy, 0),
	}

	lowerSQL := strings.ToLower(sql)

	result.HasDistinct = strings.Contains(lowerSQL, "distinct")
	result.IsSubQuery = strings.Count(lowerSQL, "select") > 1

	qa.extractAggregations(sql, result)

	qa.extractGroupBy(sql, result)

	qa.extractOrderBy(sql, result)

	qa.extractLimit(sql, result)

	qa.determineQueryType(result)

	return result
}

func (qa *QueryAnalyzer) extractAggregations(sql string, result *AnalyzedQuery) {
	if matches := qa.countPattern.FindAllStringSubmatch(sql, -1); matches != nil {
		for _, match := range matches {
			agg := Aggregation{Type: AggCount, Column: match[1]}
			if len(match) > 2 && match[2] != "" {
				agg.Alias = match[2]
			}
			result.Aggregations = append(result.Aggregations, agg)
		}
	}

	if matches := qa.sumPattern.FindAllStringSubmatch(sql, -1); matches != nil {
		for _, match := range matches {
			agg := Aggregation{Type: AggSum, Column: match[1]}
			if len(match) > 2 && match[2] != "" {
				agg.Alias = match[2]
			}
			result.Aggregations = append(result.Aggregations, agg)
		}
	}

	if matches := qa.avgPattern.FindAllStringSubmatch(sql, -1); matches != nil {
		for _, match := range matches {
			agg := Aggregation{Type: AggAvg, Column: match[1]}
			if len(match) > 2 && match[2] != "" {
				agg.Alias = match[2]
			}
			result.Aggregations = append(result.Aggregations, agg)
		}
	}

	if matches := qa.minPattern.FindAllStringSubmatch(sql, -1); matches != nil {
		for _, match := range matches {
			agg := Aggregation{Type: AggMin, Column: match[1]}
			if len(match) > 2 && match[2] != "" {
				agg.Alias = match[2]
			}
			result.Aggregations = append(result.Aggregations, agg)
		}
	}

	if matches := qa.maxPattern.FindAllStringSubmatch(sql, -1); matches != nil {
		for _, match := range matches {
			agg := Aggregation{Type: AggMax, Column: match[1]}
			if len(match) > 2 && match[2] != "" {
				agg.Alias = match[2]
			}
			result.Aggregations = append(result.Aggregations, agg)
		}
	}
}

func (qa *QueryAnalyzer) extractGroupBy(sql string, result *AnalyzedQuery) {
	if match := qa.groupByPattern.FindStringSubmatch(sql); match != nil {
		columns := strings.Split(match[1], ",")
		for _, col := range columns {
			col = strings.TrimSpace(col)
			if col != "" {
				result.GroupByKeys = append(result.GroupByKeys, col)
			}
		}
	}
}

func (qa *QueryAnalyzer) extractOrderBy(sql string, result *AnalyzedQuery) {
	if match := qa.orderByPattern.FindStringSubmatch(sql); match != nil {
		ob := OrderBy{Column: match[1], Direction: OrderAsc}
		if len(match) > 2 && strings.ToLower(match[2]) == "desc" {
			ob.Direction = OrderDesc
		}
		result.OrderByKeys = append(result.OrderByKeys, ob)
	}
}

func (qa *QueryAnalyzer) extractLimit(sql string, result *AnalyzedQuery) {
	if match := qa.limitPattern.FindStringSubmatch(sql); match != nil {
		var limit int
		if _, err := strings.NewReader(match[1]).Read(make([]byte, 10)); err == nil {
			for _, c := range match[1] {
				limit = limit*10 + int(c-'0')
			}
		}
		result.Limit = limit
	}
}

func (qa *QueryAnalyzer) determineQueryType(result *AnalyzedQuery) {
	if len(result.GroupByKeys) > 0 {
		result.Type = QueryTypeGroupBy
	} else if len(result.Aggregations) > 0 {
		result.Type = QueryTypeAggregate
	} else if len(result.OrderByKeys) > 0 {
		result.Type = QueryTypeOrderBy
	} else {
		result.Type = QueryTypeSimple
	}
}

func (qa *QueryAnalyzer) RequiresDistributedProcessing(analyzed *AnalyzedQuery) bool {
	if len(analyzed.Aggregations) > 0 {
		return true
	}

	if len(analyzed.GroupByKeys) > 0 {
		return true
	}

	if len(analyzed.OrderByKeys) > 0 && analyzed.Limit > 0 {
		return true
	}

	return false
}

func (qa *QueryAnalyzer) GetMapReduceStrategy(analyzed *AnalyzedQuery) string {
	if len(analyzed.GroupByKeys) > 0 {
		return "group_aggregate"
	}

	for _, agg := range analyzed.Aggregations {
		if agg.Type == AggAvg {
			return "avg_decompose"
		}
	}

	if len(analyzed.Aggregations) > 0 {
		return "simple_aggregate"
	}

	if len(analyzed.OrderByKeys) > 0 && analyzed.Limit > 0 {
		return "topn_merge"
	}

	return "union"
}
