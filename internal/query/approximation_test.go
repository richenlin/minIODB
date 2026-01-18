package query

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHyperLogLog(t *testing.T) {
	hll := NewHyperLogLog(nil)

	assert.NotNil(t, hll)
	assert.Equal(t, uint8(defaultPrecision), hll.precision)
	assert.Equal(t, uint32(1)<<defaultPrecision, hll.m)
	assert.Equal(t, uint32(1)<<defaultPrecision-1, hll.mMask)
}

func TestNewHyperLogLogWithConfig(t *testing.T) {
	config := &HyperLogLogConfig{
		Precision: 10,
	}

	hll := NewHyperLogLog(config)

	assert.NotNil(t, hll)
	assert.Equal(t, uint8(10), hll.precision)
	assert.Equal(t, uint32(1024), hll.m)
}

func TestNewHyperLogLogInvalidPrecision(t *testing.T) {
	tests := []struct {
		name      string
		precision uint8
		expected  uint8
	}{
		{"Too low", 2, defaultPrecision},
		{"Too high", 20, defaultPrecision},
		{"Valid", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &HyperLogLogConfig{
				Precision: tt.precision,
			}
			hll := NewHyperLogLog(config)
			assert.Equal(t, tt.expected, hll.precision)
		})
	}
}

func TestHyperLogLogAdd(t *testing.T) {
	hll := NewHyperLogLog(nil)

	hll.Add("item1")
	hll.Add("item2")
	hll.Add("item1")

	cardinality := hll.Cardinality()
	assert.True(t, cardinality >= 1 && cardinality <= 3)
}

func TestHyperLogLogCardinality(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		errorMargin float64
	}{
		{"Medium set", 1000, 3.0},
		{"Large set", 10000, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hll := NewHyperLogLog(nil)

			for i := 0; i < tt.count; i++ {
				hll.Add(fmt.Sprintf("item-%d", i))
			}

			cardinality := hll.Cardinality()
			errorRate := math.Abs(float64(cardinality)-float64(tt.count)) / float64(tt.count)

			assert.InDelta(t, tt.count, cardinality, float64(tt.count)*tt.errorMargin,
				"Cardinality %d, expected %d, error rate %.2f", cardinality, tt.count, errorRate)
		})
	}
}

func TestHyperLogLogMerge(t *testing.T) {
	hll1 := NewHyperLogLog(nil)
	hll2 := NewHyperLogLog(nil)

	for i := 0; i < 1000; i++ {
		hll1.Add(fmt.Sprintf("set1-item-%d", i))
	}

	for i := 0; i < 500; i++ {
		hll2.Add(fmt.Sprintf("set2-item-%d", i))
	}

	cardinality1 := hll1.Cardinality()
	cardinality2 := hll2.Cardinality()

	hll1.Merge(hll2)

	cardinalityMerged := hll1.Cardinality()

	assert.True(t, cardinalityMerged >= cardinality1 && cardinalityMerged <= cardinality1+cardinality2,
		"Merged cardinality %d should be between %d and %d", cardinalityMerged, cardinality1, cardinality1+cardinality2)
}

func TestHyperLogLogCopy(t *testing.T) {
	hll := NewHyperLogLog(nil)

	for i := 0; i < 100; i++ {
		hll.Add(fmt.Sprintf("item-%d", i))
	}

	hllCopy := hll.Copy()

	assert.Equal(t, hll.Cardinality(), hllCopy.Cardinality())

	hll.Add("new-item")

	assert.NotEqual(t, hll.Cardinality(), hllCopy.Cardinality())
}

func TestHyperLogLogClear(t *testing.T) {
	hll := NewHyperLogLog(nil)

	hll.Add("item1")
	hll.Add("item2")

	assert.Greater(t, hll.Cardinality(), uint64(0))

	hll.Clear()

	assert.Equal(t, uint64(0), hll.Cardinality())
}

func TestHyperLogLogEmptyString(t *testing.T) {
	hll := NewHyperLogLog(nil)

	hll.Add("")
	hll.Add("item1")

	cardinality := hll.Cardinality()

	assert.Equal(t, uint64(1), cardinality)
}

func TestHyperLogLogGetRegisters(t *testing.T) {
	hll := NewHyperLogLog(nil)

	hll.Add("item1")
	hll.Add("item2")

	registers := hll.GetRegisters()

	assert.NotNil(t, registers)
	assert.Equal(t, hll.m, uint32(len(registers)))

	nonZeroCount := 0
	for _, val := range registers {
		if val > 0 {
			nonZeroCount++
		}
	}

	assert.Greater(t, nonZeroCount, 0)
}

func TestHyperLogLogEstimateError(t *testing.T) {
	hll := NewHyperLogLog(nil)

	errorRate := hll.EstimateError()

	assert.Greater(t, errorRate, 0.0)
	assert.Less(t, errorRate, 0.2)
}

func TestHyperLogLogConcurrent(t *testing.T) {
	hll := NewHyperLogLog(nil)
	done := make(chan bool)

	for i := 0; i < 100; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				hll.Add(fmt.Sprintf("item-%d-%d", n, j))
			}
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	cardinality := hll.Cardinality()

	assert.Greater(t, cardinality, uint64(9000))
	assert.Less(t, cardinality, uint64(11000))
}

func TestNewCountMinSketch(t *testing.T) {
	cms := NewCountMinSketch(nil)

	assert.NotNil(t, cms)
	assert.Equal(t, uint32(1000), cms.width)
	assert.Equal(t, uint32(5), cms.depth)
	assert.Len(t, cms.matrix, 5)
}

func TestNewCountMinSketchWithConfig(t *testing.T) {
	config := &CountMinSketchConfig{
		Width: 500,
		Depth: 10,
		Seed:  123,
	}

	cms := NewCountMinSketch(config)

	assert.NotNil(t, cms)
	assert.Equal(t, uint32(500), cms.width)
	assert.Equal(t, uint32(10), cms.depth)
}

func TestCountMinSketchAdd(t *testing.T) {
	cms := NewCountMinSketch(nil)

	cms.Add("item1", 1)
	cms.Add("item2", 2)

	assert.Greater(t, cms.Count("item1"), uint32(0))
	assert.Greater(t, cms.Count("item2"), uint32(0))
}

func TestCountMinSketchCount(t *testing.T) {
	cms := NewCountMinSketch(nil)

	cms.Add("item1", 5)
	cms.Add("item1", 3)
	cms.Add("item2", 10)

	count1 := cms.Count("item1")
	count2 := cms.Count("item2")

	assert.GreaterOrEqual(t, count1, uint32(8))
	assert.GreaterOrEqual(t, count2, uint32(10))
}

func TestCountMinSketchMerge(t *testing.T) {
	cms1 := NewCountMinSketch(nil)
	cms2 := NewCountMinSketch(nil)

	cms1.Add("item1", 5)
	cms1.Add("item2", 3)

	cms2.Add("item1", 2)
	cms2.Add("item3", 10)

	cms1.Merge(cms2)

	assert.GreaterOrEqual(t, cms1.Count("item1"), uint32(7))
	assert.GreaterOrEqual(t, cms1.Count("item2"), uint32(3))
	assert.GreaterOrEqual(t, cms1.Count("item3"), uint32(10))
}

func TestCountMinSketchClear(t *testing.T) {
	cms := NewCountMinSketch(nil)

	cms.Add("item1", 10)
	assert.Greater(t, cms.Count("item1"), uint32(0))

	cms.Clear()
	assert.Equal(t, uint32(0), cms.Count("item1"))
}

func TestCountMinSketchEmptyString(t *testing.T) {
	cms := NewCountMinSketch(nil)

	cms.Add("", 10)
	cms.Add("item1", 5)

	assert.Equal(t, uint32(0), cms.Count(""))
	assert.Greater(t, cms.Count("item1"), uint32(0))
}

func TestCountMinSketchConcurrent(t *testing.T) {
	cms := NewCountMinSketch(nil)
	done := make(chan bool)

	for i := 0; i < 100; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				cms.Add(fmt.Sprintf("item-%d", n), 1)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	count := cms.Count("item-50")
	assert.GreaterOrEqual(t, count, uint32(100))
}

func TestNewApproximateQueryEngine(t *testing.T) {
	aqe := NewApproximateQueryEngine()

	assert.NotNil(t, aqe)
	assert.NotNil(t, aqe.hllMap)
	assert.NotNil(t, aqe.cmsMap)
}

func TestApproximateQueryEngineAddDistinctValue(t *testing.T) {
	aqe := NewApproximateQueryEngine()

	aqe.AddDistinctValue("users", "id", "user1")
	aqe.AddDistinctValue("users", "id", "user2")
	aqe.AddDistinctValue("users", "id", "user1")

	count := aqe.ApproximateCountDistinct("users", "id")

	assert.InDelta(t, uint64(2), count, 1)
}

func TestApproximateQueryEngineApproximateCountDistinct(t *testing.T) {
	aqe := NewApproximateQueryEngine()

	for i := 0; i < 1000; i++ {
		aqe.AddDistinctValue("users", "id", fmt.Sprintf("user-%d", i))
	}

	count := aqe.ApproximateCountDistinct("users", "id")

	assert.InDelta(t, uint64(1000), count, 3000)
}

func TestApproximateQueryEngineAddFrequency(t *testing.T) {
	aqe := NewApproximateQueryEngine()

	aqe.AddFrequency("events", "type", "click", 10)
	aqe.AddFrequency("events", "type", "click", 5)
	aqe.AddFrequency("events", "type", "view", 20)

	clickCount := aqe.ApproximateCount("events", "type", "click")
	viewCount := aqe.ApproximateCount("events", "type", "view")

	assert.GreaterOrEqual(t, clickCount, uint32(15))
	assert.GreaterOrEqual(t, viewCount, uint32(20))
}

func TestApproximateQueryEngineClear(t *testing.T) {
	aqe := NewApproximateQueryEngine()

	aqe.AddDistinctValue("users", "id", "user1")
	aqe.AddFrequency("events", "type", "click", 10)

	assert.Greater(t, aqe.ApproximateCountDistinct("users", "id"), uint64(0))
	assert.Greater(t, aqe.ApproximateCount("events", "type", "click"), uint32(0))

	aqe.Clear()

	assert.Equal(t, uint64(0), aqe.ApproximateCountDistinct("users", "id"))
	assert.Equal(t, uint32(0), aqe.ApproximateCount("events", "type", "click"))
}

func TestApproximateQueryEngineMerge(t *testing.T) {
	aqe1 := NewApproximateQueryEngine()
	aqe2 := NewApproximateQueryEngine()

	aqe1.AddDistinctValue("users", "id", "user1")
	aqe1.AddFrequency("events", "type", "click", 10)

	aqe2.AddDistinctValue("users", "id", "user2")
	aqe2.AddFrequency("events", "type", "click", 5)

	count1 := aqe1.ApproximateCountDistinct("users", "id")
	freq1 := aqe1.ApproximateCount("events", "type", "click")

	aqe1.Merge(aqe2)

	countMerged := aqe1.ApproximateCountDistinct("users", "id")
	freqMerged := aqe1.ApproximateCount("events", "type", "click")

	assert.GreaterOrEqual(t, countMerged, count1)
	assert.GreaterOrEqual(t, freqMerged, freq1+5)
}

func TestApproximateQueryEngineGetErrorEstimate(t *testing.T) {
	aqe := NewApproximateQueryEngine()

	errorRate := aqe.GetErrorEstimate("users", "id")

	assert.Greater(t, errorRate, 0.0)
	assert.Less(t, errorRate, 0.2)
}

func BenchmarkHyperLogLogAdd(b *testing.B) {
	hll := NewHyperLogLog(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hll.Add(fmt.Sprintf("item-%d", i))
	}
}

func BenchmarkHyperLogLogCardinality(b *testing.B) {
	hll := NewHyperLogLog(nil)

	for i := 0; i < 100000; i++ {
		hll.Add(fmt.Sprintf("item-%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hll.Cardinality()
	}
}

func BenchmarkCountMinSketchAdd(b *testing.B) {
	cms := NewCountMinSketch(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cms.Add(fmt.Sprintf("item-%d", i), 1)
	}
}

func BenchmarkCountMinSketchCount(b *testing.B) {
	cms := NewCountMinSketch(nil)

	for i := 0; i < 100000; i++ {
		cms.Add(fmt.Sprintf("item-%d", i), 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cms.Count(fmt.Sprintf("item-%d", i))
	}
}
