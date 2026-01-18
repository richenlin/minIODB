package query

import (
	"math"
	"sync"
)

const (
	defaultPrecision = 12
	smallThreshold   = 25
)

type HyperLogLog struct {
	registers []uint8
	precision uint8
	m         uint32
	mMask     uint32
	mu        sync.RWMutex
}

type HyperLogLogConfig struct {
	Precision uint8
}

func NewHyperLogLog(config *HyperLogLogConfig) *HyperLogLog {
	if config == nil {
		config = &HyperLogLogConfig{
			Precision: defaultPrecision,
		}
	}

	precision := config.Precision
	if precision < 4 || precision > 16 {
		precision = defaultPrecision
	}

	m := uint32(1) << precision

	return &HyperLogLog{
		registers: make([]uint8, m),
		precision: precision,
		m:         m,
		mMask:     m - 1,
	}
}

func (hll *HyperLogLog) Add(item string) {
	if item == "" {
		return
	}

	hash := hll.hash(item)

	idx := hash & hll.mMask

	w := hash >> hll.precision

	rho := uint8(1)
	for (w&1) == 0 && rho < 32-hll.precision {
		w = w >> 1
		rho++
	}

	hll.mu.Lock()
	if rho > hll.registers[idx] {
		hll.registers[idx] = rho
	}
	hll.mu.Unlock()
}

func (hll *HyperLogLog) Cardinality() uint64 {
	hll.mu.RLock()
	defer hll.mu.RUnlock()

	sum := 0.0
	zeroCount := 0

	for _, val := range hll.registers {
		sum += 1.0 / math.Pow(2, float64(val))
		if val == 0 {
			zeroCount++
		}
	}

	alpha := hll.alpha()

	if zeroCount > 0 && sum == 0 {
		return uint64(hll.linearCounting(zeroCount))
	}

	est := alpha * float64(hll.m*hll.m) / sum

	if est <= float64(5*hll.m)/2 && zeroCount != 0 {
		h := float64(hll.m) * math.Log(float64(hll.m)/float64(zeroCount))
		if h < float64(smallThreshold) {
			return uint64(math.Round(h))
		}
	}

	if est > (1.0/30.0)*math.Pow(2, 32) {
		return uint64(math.Round(-math.Pow(2, 32) * math.Log(1.0-est/math.Pow(2, 32))))
	}

	return uint64(math.Round(est))
}

func (hll *HyperLogLog) Merge(other *HyperLogLog) {
	if other == nil || hll.m != other.m {
		return
	}

	hll.mu.Lock()
	defer hll.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for i := 0; i < int(hll.m); i++ {
		if other.registers[i] > hll.registers[i] {
			hll.registers[i] = other.registers[i]
		}
	}
}

func (hll *HyperLogLog) Copy() *HyperLogLog {
	hll.mu.RLock()
	defer hll.mu.RUnlock()

	registers := make([]uint8, hll.m)
	copy(registers, hll.registers)

	return &HyperLogLog{
		registers: registers,
		precision: hll.precision,
		m:         hll.m,
		mMask:     hll.mMask,
	}
}

func (hll *HyperLogLog) Clear() {
	hll.mu.Lock()
	defer hll.mu.Unlock()

	for i := range hll.registers {
		hll.registers[i] = 0
	}
}

func (hll *HyperLogLog) hash(item string) uint32 {
	hash := uint32(2166136261)

	const prime32 = uint32(16777619)

	for _, c := range item {
		hash *= prime32
		hash ^= uint32(c)
	}

	hash ^= hash >> 16
	hash *= prime32
	hash ^= hash >> 13
	hash *= prime32
	hash ^= hash >> 16

	return hash
}

func (hll *HyperLogLog) alpha() float64 {
	m := float64(hll.m)

	switch hll.m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1 + 1.079/m)
	}
}

func (hll *HyperLogLog) linearCounting(zeroCount int) float64 {
	m := float64(hll.m)
	return m * math.Log(m/float64(zeroCount))
}

func (hll *HyperLogLog) GetRegisters() []uint8 {
	hll.mu.RLock()
	defer hll.mu.RUnlock()

	registers := make([]uint8, hll.m)
	copy(registers, hll.registers)
	return registers
}

func (hll *HyperLogLog) EstimateError() float64 {
	return 1.04 / math.Sqrt(float64(hll.m))
}

type CountMinSketch struct {
	matrix    [][]uint32
	hashFuncs []func(string) uint32
	width     uint32
	depth     uint32
	mu        sync.RWMutex
}

type CountMinSketchConfig struct {
	Width uint32
	Depth uint32
	Seed  uint32
}

func NewCountMinSketch(config *CountMinSketchConfig) *CountMinSketch {
	if config == nil {
		config = &CountMinSketchConfig{
			Width: 1000,
			Depth: 5,
			Seed:  42,
		}
	}

	cms := &CountMinSketch{
		matrix:    make([][]uint32, config.Depth),
		hashFuncs: make([]func(string) uint32, config.Depth),
		width:     config.Width,
		depth:     config.Depth,
	}

	for i := range cms.matrix {
		cms.matrix[i] = make([]uint32, config.Width)
		seed := config.Seed + uint32(i)
		cms.hashFuncs[i] = func(item string) uint32 {
			hash := uint32(seed)
			for _, c := range item {
				hash = hash*31 + uint32(c)
			}
			return hash % cms.width
		}
	}

	return cms
}

func (cms *CountMinSketch) Add(item string, count uint32) {
	if item == "" || count == 0 {
		return
	}

	cms.mu.Lock()
	defer cms.mu.Unlock()

	for i := uint32(0); i < cms.depth; i++ {
		idx := cms.hashFuncs[i](item)
		cms.matrix[i][idx] += count
	}
}

func (cms *CountMinSketch) Count(item string) uint32 {
	if item == "" {
		return 0
	}

	cms.mu.RLock()
	defer cms.mu.RUnlock()

	minCount := uint32(math.MaxUint32)

	for i := uint32(0); i < cms.depth; i++ {
		idx := cms.hashFuncs[i](item)
		if cms.matrix[i][idx] < minCount {
			minCount = cms.matrix[i][idx]
		}
	}

	return minCount
}

func (cms *CountMinSketch) Merge(other *CountMinSketch) {
	if other == nil || cms.width != other.width || cms.depth != other.depth {
		return
	}

	cms.mu.Lock()
	defer cms.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for i := uint32(0); i < cms.depth; i++ {
		for j := uint32(0); j < cms.width; j++ {
			cms.matrix[i][j] += other.matrix[i][j]
		}
	}
}

func (cms *CountMinSketch) Clear() {
	cms.mu.Lock()
	defer cms.mu.Unlock()

	for i := range cms.matrix {
		for j := range cms.matrix[i] {
			cms.matrix[i][j] = 0
		}
	}
}

type ApproximateQueryEngine struct {
	hllMap map[string]*HyperLogLog
	cmsMap map[string]*CountMinSketch
	mu     sync.RWMutex
}

func NewApproximateQueryEngine() *ApproximateQueryEngine {
	return &ApproximateQueryEngine{
		hllMap: make(map[string]*HyperLogLog),
		cmsMap: make(map[string]*CountMinSketch),
	}
}

func (aqe *ApproximateQueryEngine) getHLLKey(table, column string) string {
	return table + ":" + column
}

func (aqe *ApproximateQueryEngine) getOrCreateHLL(table, column string) *HyperLogLog {
	key := aqe.getHLLKey(table, column)

	aqe.mu.RLock()
	hll, exists := aqe.hllMap[key]
	aqe.mu.RUnlock()

	if !exists {
		aqe.mu.Lock()
		if hll, exists = aqe.hllMap[key]; !exists {
			hll = NewHyperLogLog(&HyperLogLogConfig{
				Precision: defaultPrecision,
			})
			aqe.hllMap[key] = hll
		}
		aqe.mu.Unlock()
	}

	return hll
}

func (aqe *ApproximateQueryEngine) getOrCreateCMS(table, column string) *CountMinSketch {
	key := aqe.getHLLKey(table, column)

	aqe.mu.RLock()
	cms, exists := aqe.cmsMap[key]
	aqe.mu.RUnlock()

	if !exists {
		aqe.mu.Lock()
		if cms, exists = aqe.cmsMap[key]; !exists {
			cms = NewCountMinSketch(&CountMinSketchConfig{
				Width: 1000,
				Depth: 5,
				Seed:  42,
			})
			aqe.cmsMap[key] = cms
		}
		aqe.mu.Unlock()
	}

	return cms
}

func (aqe *ApproximateQueryEngine) AddDistinctValue(table, column, value string) {
	if table == "" || column == "" || value == "" {
		return
	}

	hll := aqe.getOrCreateHLL(table, column)
	hll.Add(value)
}

func (aqe *ApproximateQueryEngine) ApproximateCountDistinct(table, column string) uint64 {
	if table == "" || column == "" {
		return 0
	}

	hll := aqe.getOrCreateHLL(table, column)
	return hll.Cardinality()
}

func (aqe *ApproximateQueryEngine) AddFrequency(table, column, value string, count uint32) {
	if table == "" || column == "" || value == "" {
		return
	}

	cms := aqe.getOrCreateCMS(table, column)
	cms.Add(value, count)
}

func (aqe *ApproximateQueryEngine) ApproximateCount(table, column, value string) uint32 {
	if table == "" || column == "" || value == "" {
		return 0
	}

	cms := aqe.getOrCreateCMS(table, column)
	return cms.Count(value)
}

func (aqe *ApproximateQueryEngine) Clear() {
	aqe.mu.Lock()
	defer aqe.mu.Unlock()

	aqe.hllMap = make(map[string]*HyperLogLog)
	aqe.cmsMap = make(map[string]*CountMinSketch)
}

func (aqe *ApproximateQueryEngine) Merge(other *ApproximateQueryEngine) {
	if other == nil {
		return
	}

	other.mu.RLock()
	defer other.mu.RUnlock()

	aqe.mu.Lock()
	defer aqe.mu.Unlock()

	for key, otherHLL := range other.hllMap {
		if hll, exists := aqe.hllMap[key]; exists {
			hll.Merge(otherHLL)
		} else {
			aqe.hllMap[key] = otherHLL.Copy()
		}
	}

	for key, otherCMS := range other.cmsMap {
		if cms, exists := aqe.cmsMap[key]; exists {
			cms.Merge(otherCMS)
		} else {
			newCMS := NewCountMinSketch(&CountMinSketchConfig{
				Width: 1000,
				Depth: 5,
				Seed:  42,
			})
			newCMS.Merge(otherCMS)
			aqe.cmsMap[key] = newCMS
		}
	}
}

func (aqe *ApproximateQueryEngine) GetErrorEstimate(table, column string) float64 {
	if table == "" || column == "" {
		return 0.0
	}

	hll := aqe.getOrCreateHLL(table, column)
	return hll.EstimateError()
}
