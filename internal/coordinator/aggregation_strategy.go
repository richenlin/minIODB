package coordinator

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type AggregationStrategy interface {
	MapSQL(originalSQL string, analyzed *AnalyzedQuery) string
	Reduce(results []string, analyzed *AnalyzedQuery) (string, error)
}

type StrategyFactory struct {
	strategies map[string]AggregationStrategy
}

func NewStrategyFactory() *StrategyFactory {
	sf := &StrategyFactory{
		strategies: make(map[string]AggregationStrategy),
	}

	sf.strategies["simple_aggregate"] = &SimpleAggregateStrategy{}
	sf.strategies["avg_decompose"] = &AvgDecomposeStrategy{}
	sf.strategies["group_aggregate"] = &GroupAggregateStrategy{}
	sf.strategies["topn_merge"] = &TopNMergeStrategy{}
	sf.strategies["union"] = &UnionStrategy{}

	return sf
}

func (sf *StrategyFactory) GetStrategy(name string) AggregationStrategy {
	if s, ok := sf.strategies[name]; ok {
		return s
	}
	return sf.strategies["union"]
}

type SimpleAggregateStrategy struct{}

func (s *SimpleAggregateStrategy) MapSQL(originalSQL string, analyzed *AnalyzedQuery) string {
	return originalSQL
}

func (s *SimpleAggregateStrategy) Reduce(results []string, analyzed *AnalyzedQuery) (string, error) {
	if len(results) == 0 {
		return "[]", nil
	}

	aggregated := make(map[string]float64)

	for _, result := range results {
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}

		for _, row := range data {
			for key, value := range row {
				if num, ok := toFloat64(value); ok {
					aggregated[key] += num
				}
			}
		}
	}

	// 如果没有分析到聚合函数，直接返回聚合后的所有数值
	if len(analyzed.Aggregations) == 0 {
		resultRow := make(map[string]interface{})
		for key, val := range aggregated {
			resultRow[key] = val
		}
		output, err := json.Marshal([]map[string]interface{}{resultRow})
		if err != nil {
			return "", err
		}
		return string(output), nil
	}

	resultRow := make(map[string]interface{})
	for _, agg := range analyzed.Aggregations {
		// 确定输出的 key
		outputKey := agg.Alias
		if outputKey == "" {
			outputKey = fmt.Sprintf("%s_%s", strings.ToLower(aggTypeName(agg.Type)), agg.Column)
		}

		// 尝试多种可能的输入 key 匹配
		possibleKeys := []string{
			outputKey,                              // 完整的 key，如 "count_*"
			strings.ToLower(aggTypeName(agg.Type)), // 只有聚合类型，如 "count"
			agg.Alias,                              // 别名
		}

		found := false
		for _, key := range possibleKeys {
			if val, ok := aggregated[key]; ok {
				resultRow[key] = val // 使用原始输入 key 保持与输入一致
				found = true
				break
			}
		}

		// 如果没有找到匹配，保留原始聚合结果中与聚合类型相关的项
		if !found {
			aggTypeLower := strings.ToLower(aggTypeName(agg.Type))
			for key, val := range aggregated {
				keyLower := strings.ToLower(key)
				// 检查 key 是否包含聚合类型名称（如 "count", "sum" 等）
				if keyLower == aggTypeLower || strings.Contains(keyLower, aggTypeLower) {
					resultRow[key] = val
					break
				}
			}
		}
	}

	output, err := json.Marshal([]map[string]interface{}{resultRow})
	if err != nil {
		return "", err
	}

	return string(output), nil
}

type AvgDecomposeStrategy struct{}

func (s *AvgDecomposeStrategy) MapSQL(originalSQL string, analyzed *AnalyzedQuery) string {
	sql := originalSQL

	for _, agg := range analyzed.Aggregations {
		if agg.Type == AggAvg {
			avgExpr := fmt.Sprintf("AVG(%s)", agg.Column)
			sumExpr := fmt.Sprintf("SUM(%s) as _sum_%s, COUNT(%s) as _count_%s",
				agg.Column, agg.Column, agg.Column, agg.Column)

			sql = strings.Replace(sql, avgExpr, sumExpr, 1)
			sql = strings.Replace(sql, strings.ToLower(avgExpr), sumExpr, 1)
		}
	}

	return sql
}

func (s *AvgDecomposeStrategy) Reduce(results []string, analyzed *AnalyzedQuery) (string, error) {
	sums := make(map[string]float64)
	counts := make(map[string]float64)

	for _, result := range results {
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}

		for _, row := range data {
			for key, value := range row {
				if num, ok := toFloat64(value); ok {
					if strings.HasPrefix(key, "_sum_") {
						col := strings.TrimPrefix(key, "_sum_")
						sums[col] += num
					} else if strings.HasPrefix(key, "_count_") {
						col := strings.TrimPrefix(key, "_count_")
						counts[col] += num
					}
				}
			}
		}
	}

	resultRow := make(map[string]interface{})

	for _, agg := range analyzed.Aggregations {
		if agg.Type == AggAvg {
			sum := sums[agg.Column]
			count := counts[agg.Column]

			key := agg.Alias
			if key == "" {
				key = fmt.Sprintf("avg_%s", agg.Column)
			}

			if count > 0 {
				resultRow[key] = sum / count
			} else {
				resultRow[key] = nil
			}
		}
	}

	output, err := json.Marshal([]map[string]interface{}{resultRow})
	if err != nil {
		return "", err
	}

	return string(output), nil
}

type GroupAggregateStrategy struct{}

func (s *GroupAggregateStrategy) MapSQL(originalSQL string, analyzed *AnalyzedQuery) string {
	return originalSQL
}

func (s *GroupAggregateStrategy) Reduce(results []string, analyzed *AnalyzedQuery) (string, error) {
	groups := make(map[string]map[string]float64)
	groupCounts := make(map[string]map[string]float64)

	for _, result := range results {
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}

		for _, row := range data {
			groupKey := s.buildGroupKey(row, analyzed.GroupByKeys)

			if _, ok := groups[groupKey]; !ok {
				groups[groupKey] = make(map[string]float64)
				groupCounts[groupKey] = make(map[string]float64)
			}

			for key, value := range row {
				if num, ok := toFloat64(value); ok {
					isGroupKey := false
					for _, gk := range analyzed.GroupByKeys {
						if key == gk {
							isGroupKey = true
							break
						}
					}

					if !isGroupKey {
						groups[groupKey][key] += num
						groupCounts[groupKey][key]++
					}
				}
			}
		}
	}

	var resultRows []map[string]interface{}

	for groupKey, aggValues := range groups {
		row := make(map[string]interface{})

		parts := strings.Split(groupKey, "|")
		for i, gk := range analyzed.GroupByKeys {
			if i < len(parts) {
				row[gk] = parts[i]
			}
		}

		for key, value := range aggValues {
			for _, agg := range analyzed.Aggregations {
				if agg.Type == AggAvg {
					count := groupCounts[groupKey][key]
					if count > 0 {
						row[key] = value / count
					}
				} else {
					row[key] = value
				}
			}
		}

		resultRows = append(resultRows, row)
	}

	output, err := json.Marshal(resultRows)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func (s *GroupAggregateStrategy) buildGroupKey(row map[string]interface{}, groupByKeys []string) string {
	var parts []string
	for _, key := range groupByKeys {
		if val, ok := row[key]; ok {
			parts = append(parts, fmt.Sprintf("%v", val))
		} else {
			parts = append(parts, "")
		}
	}
	return strings.Join(parts, "|")
}

type TopNMergeStrategy struct{}

func (s *TopNMergeStrategy) MapSQL(originalSQL string, analyzed *AnalyzedQuery) string {
	return originalSQL
}

func (s *TopNMergeStrategy) Reduce(results []string, analyzed *AnalyzedQuery) (string, error) {
	var allRows []map[string]interface{}

	for _, result := range results {
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}
		allRows = append(allRows, data...)
	}

	if len(analyzed.OrderByKeys) > 0 {
		ob := analyzed.OrderByKeys[0]
		sort.Slice(allRows, func(i, j int) bool {
			vi, oki := allRows[i][ob.Column]
			vj, okj := allRows[j][ob.Column]

			if !oki || !okj {
				return oki
			}

			cmp := compareInterface(vi, vj)
			if ob.Direction == OrderDesc {
				return cmp > 0
			}
			return cmp < 0
		})
	}

	if analyzed.Limit > 0 && len(allRows) > analyzed.Limit {
		allRows = allRows[:analyzed.Limit]
	}

	output, err := json.Marshal(allRows)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

type UnionStrategy struct{}

func (s *UnionStrategy) MapSQL(originalSQL string, analyzed *AnalyzedQuery) string {
	return originalSQL
}

func (s *UnionStrategy) Reduce(results []string, analyzed *AnalyzedQuery) (string, error) {
	var allRows []map[string]interface{}

	for _, result := range results {
		var data []map[string]interface{}
		if err := json.Unmarshal([]byte(result), &data); err != nil {
			continue
		}
		allRows = append(allRows, data...)
	}

	output, err := json.Marshal(allRows)
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}

func aggTypeName(t AggregationType) string {
	switch t {
	case AggCount:
		return "count"
	case AggSum:
		return "sum"
	case AggAvg:
		return "avg"
	case AggMin:
		return "min"
	case AggMax:
		return "max"
	default:
		return "unknown"
	}
}

func compareInterface(a, b interface{}) int {
	switch va := a.(type) {
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
	case int64:
		if vb, ok := b.(int64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	}
	return 0
}

type rowHeapItem struct {
	row      map[string]interface{}
	orderKey string
	index    int
}

type rowHeap struct {
	items     []*rowHeapItem
	ascending bool
}

func (h rowHeap) Len() int { return len(h.items) }
func (h rowHeap) Less(i, j int) bool {
	if h.ascending {
		return h.items[i].orderKey < h.items[j].orderKey
	}
	return h.items[i].orderKey > h.items[j].orderKey
}
func (h rowHeap) Swap(i, j int)       { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *rowHeap) Push(x interface{}) { h.items = append(h.items, x.(*rowHeapItem)) }
func (h *rowHeap) Pop() interface{} {
	old := h.items
	n := len(old)
	item := old[n-1]
	h.items = old[0 : n-1]
	return item
}

func mergeTopNWithHeap(results [][]map[string]interface{}, orderBy OrderBy, limit int) []map[string]interface{} {
	h := &rowHeap{ascending: orderBy.Direction == OrderAsc}
	heap.Init(h)

	for _, rows := range results {
		for _, row := range rows {
			key := fmt.Sprintf("%v", row[orderBy.Column])
			item := &rowHeapItem{row: row, orderKey: key}
			heap.Push(h, item)
		}
	}

	var result []map[string]interface{}
	for h.Len() > 0 && len(result) < limit {
		item := heap.Pop(h).(*rowHeapItem)
		result = append(result, item.row)
	}

	return result
}
