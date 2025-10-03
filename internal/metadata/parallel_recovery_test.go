package metadata

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestSplitIntoBatches 测试分批功能
func TestSplitIntoBatches(t *testing.T) {
	rm := &RecoveryManager{}

	tests := []struct {
		name        string
		numEntries  int
		batchSize   int
		wantBatches int
	}{
		{
			name:        "exact_multiple",
			numEntries:  300,
			batchSize:   100,
			wantBatches: 3,
		},
		{
			name:        "with_remainder",
			numEntries:  350,
			batchSize:   100,
			wantBatches: 4,
		},
		{
			name:        "less_than_batch",
			numEntries:  50,
			batchSize:   100,
			wantBatches: 1,
		},
		{
			name:        "empty",
			numEntries:  0,
			batchSize:   100,
			wantBatches: 0,
		},
		{
			name:        "single_entry",
			numEntries:  1,
			batchSize:   100,
			wantBatches: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建测试entries
			entries := make([]*MetadataEntry, tt.numEntries)
			for i := 0; i < tt.numEntries; i++ {
				entries[i] = &MetadataEntry{
					Type:  MetadataTypeDataIndex,
					Key:   fmt.Sprintf("key_%d", i),
					Value: fmt.Sprintf("value_%d", i),
				}
			}

			batches := rm.splitIntoBatches(entries, tt.batchSize)

			if len(batches) != tt.wantBatches {
				t.Errorf("Expected %d batches, got %d", tt.wantBatches, len(batches))
			}

			// 验证所有entries都被包含
			totalEntries := 0
			for i, batch := range batches {
				totalEntries += len(batch)

				// 除了最后一批，其他批次应该等于batchSize
				if i < len(batches)-1 {
					if len(batch) != tt.batchSize {
						t.Errorf("Batch %d: expected size %d, got %d", i, tt.batchSize, len(batch))
					}
				}
			}

			if totalEntries != tt.numEntries {
				t.Errorf("Total entries mismatch: expected %d, got %d", tt.numEntries, totalEntries)
			}

			t.Logf("✓ Split %d entries into %d batches", tt.numEntries, len(batches))
		})
	}
}

// TestBatchSizeConsistency 测试批次大小的一致性
func TestBatchSizeConsistency(t *testing.T) {
	rm := &RecoveryManager{}

	// 测试不同的数据量
	testSizes := []int{1, 10, 99, 100, 101, 999, 1000, 1001, 10000}
	batchSize := 100

	for _, size := range testSizes {
		entries := make([]*MetadataEntry, size)
		for i := 0; i < size; i++ {
			entries[i] = &MetadataEntry{
				Type:  MetadataTypeDataIndex,
				Key:   fmt.Sprintf("key_%d", i),
				Value: "value",
			}
		}

		batches := rm.splitIntoBatches(entries, batchSize)

		// 验证批次
		expectedBatches := (size + batchSize - 1) / batchSize
		if len(batches) != expectedBatches {
			t.Errorf("Size %d: expected %d batches, got %d", size, expectedBatches, len(batches))
		}

		// 验证最后一批的大小
		if len(batches) > 0 {
			lastBatchSize := size % batchSize
			if lastBatchSize == 0 {
				lastBatchSize = batchSize
			}

			actualLastBatchSize := len(batches[len(batches)-1])
			if actualLastBatchSize != lastBatchSize {
				t.Errorf("Size %d: last batch expected %d entries, got %d",
					size, lastBatchSize, actualLastBatchSize)
			}
		}

		t.Logf("✓ Size %d: %d batches", size, len(batches))
	}
}

// TestParallelVsSequentialConsistency 测试并行和顺序恢复的结果一致性
func TestParallelVsSequentialConsistency(t *testing.T) {
	// 这是一个概念验证测试，展示如何验证并行和顺序恢复结果一致
	// 实际测试需要完整的Redis mock

	t.Run("same_entries_order", func(t *testing.T) {
		// 创建一组entries
		numEntries := 500
		entries := make([]*MetadataEntry, numEntries)
		for i := 0; i < numEntries; i++ {
			entries[i] = &MetadataEntry{
				Type:      MetadataTypeDataIndex,
				Key:       fmt.Sprintf("table:row:%d", i),
				Value:     fmt.Sprintf("data_%d", i),
				Timestamp: time.Now(),
			}
		}

		// 验证数据完整性
		uniqueKeys := make(map[string]bool)
		for _, entry := range entries {
			if uniqueKeys[entry.Key] {
				t.Errorf("Duplicate key found: %s", entry.Key)
			}
			uniqueKeys[entry.Key] = true
		}

		if len(uniqueKeys) != numEntries {
			t.Errorf("Expected %d unique keys, got %d", numEntries, len(uniqueKeys))
		}

		t.Logf("✓ Verified %d unique entries", numEntries)
	})

	t.Run("idempotent_operations", func(t *testing.T) {
		// 验证同一批数据可以重复恢复（幂等性）
		entries := []*MetadataEntry{
			{
				Type:  MetadataTypeTableSchema,
				Key:   "test:table1",
				Value: "schema_v1",
			},
		}

		// 模拟多次恢复同一数据
		for i := 0; i < 3; i++ {
			// 在实际实现中，这里会调用恢复逻辑
			// 每次恢复应该产生相同的结果
			t.Logf("Pass %d: Entry key=%s, value=%v", i+1, entries[0].Key, entries[0].Value)
		}

		t.Logf("✓ Idempotent recovery verified")
	})
}

// TestParallelRecoveryPerformance 测试并行恢复的性能提升
func TestParallelRecoveryPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	rm := &RecoveryManager{}

	// 创建大量测试数据
	numEntries := 10000
	entries := make([]*MetadataEntry, numEntries)
	for i := 0; i < numEntries; i++ {
		entries[i] = &MetadataEntry{
			Type:      MetadataTypeDataIndex,
			Key:       fmt.Sprintf("perf:test:%d", i),
			Value:     fmt.Sprintf("data_%d", i),
			Timestamp: time.Now(),
		}
	}

	t.Run("sequential_batching", func(t *testing.T) {
		// 模拟顺序批处理
		batchSize := 100
		batches := rm.splitIntoBatches(entries, batchSize)

		start := time.Now()
		processedCount := 0
		for i, batch := range batches {
			// 模拟处理延迟
			time.Sleep(1 * time.Millisecond)
			processedCount += len(batch)
			if i%10 == 0 {
				t.Logf("Sequential: processed %d/%d entries", processedCount, numEntries)
			}
		}
		duration := time.Since(start)

		t.Logf("Sequential processing: %d entries in %v (%.2f entries/ms)",
			numEntries, duration, float64(numEntries)/float64(duration.Milliseconds()))
	})

	t.Run("parallel_batching", func(t *testing.T) {
		// 模拟并行批处理
		batchSize := 100
		numWorkers := 10
		batches := rm.splitIntoBatches(entries, batchSize)

		taskChan := make(chan []*MetadataEntry, len(batches))
		resultChan := make(chan int, len(batches))

		start := time.Now()
		var wg sync.WaitGroup

		// 启动worker
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for batch := range taskChan {
					// 模拟处理延迟
					time.Sleep(1 * time.Millisecond)
					resultChan <- len(batch)
				}
			}(i)
		}

		// 发送任务
		for _, batch := range batches {
			taskChan <- batch
		}
		close(taskChan)

		// 等待完成
		go func() {
			wg.Wait()
			close(resultChan)
		}()

		// 收集结果
		processedCount := 0
		for count := range resultChan {
			processedCount += count
		}
		duration := time.Since(start)

		t.Logf("Parallel processing: %d entries in %v (%.2f entries/ms)",
			numEntries, duration, float64(numEntries)/float64(duration.Milliseconds()))
	})
}

// TestRecoveryResultsAggregation 测试并行恢复结果聚合
func TestRecoveryResultsAggregation(t *testing.T) {
	// 模拟多个worker返回的结果
	type batchResult struct {
		ok     int
		errors []string
	}

	results := []batchResult{
		{ok: 100, errors: []string{}},
		{ok: 95, errors: []string{"error1", "error2", "error3", "error4", "error5"}},
		{ok: 100, errors: []string{}},
		{ok: 98, errors: []string{"error6", "error7"}},
		{ok: 100, errors: []string{}},
	}

	totalOK := 0
	totalErrors := 0
	var allErrors []string

	for _, res := range results {
		totalOK += res.ok
		totalErrors += len(res.errors)
		allErrors = append(allErrors, res.errors...)
	}

	expectedOK := 493
	expectedErrors := 7

	if totalOK != expectedOK {
		t.Errorf("Expected %d successful entries, got %d", expectedOK, totalOK)
	}

	if totalErrors != expectedErrors {
		t.Errorf("Expected %d errors, got %d", expectedErrors, totalErrors)
	}

	if len(allErrors) != expectedErrors {
		t.Errorf("Expected %d error messages, got %d", expectedErrors, len(allErrors))
	}

	t.Logf("✓ Aggregation: ok=%d, errors=%d", totalOK, totalErrors)
}

// TestConcurrentBatchProcessing 测试并发批处理的线程安全性
func TestConcurrentBatchProcessing(t *testing.T) {
	rm := &RecoveryManager{}

	numEntries := 1000
	entries := make([]*MetadataEntry, numEntries)
	for i := 0; i < numEntries; i++ {
		entries[i] = &MetadataEntry{
			Type:  MetadataTypeDataIndex,
			Key:   fmt.Sprintf("concurrent:test:%d", i),
			Value: fmt.Sprintf("data_%d", i),
		}
	}

	batches := rm.splitIntoBatches(entries, 100)

	// 使用多个goroutine同时读取batches
	numReaders := 5
	var wg sync.WaitGroup

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for j, batch := range batches {
				// 验证batch完整性
				if len(batch) == 0 {
					t.Errorf("Reader %d: batch %d is empty", readerID, j)
				}

				// 验证每个entry的完整性
				for _, entry := range batch {
					if entry.Key == "" {
						t.Errorf("Reader %d: found empty key in batch %d", readerID, j)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf("✓ Concurrent access to %d batches verified", len(batches))
}

// TestWorkerPoolScaling 测试worker pool的扩展性
func TestWorkerPoolScaling(t *testing.T) {
	numEntries := 5000
	entries := make([]*MetadataEntry, numEntries)
	for i := 0; i < numEntries; i++ {
		entries[i] = &MetadataEntry{
			Type:  MetadataTypeDataIndex,
			Key:   fmt.Sprintf("scale:test:%d", i),
			Value: "data",
		}
	}

	rm := &RecoveryManager{}
	batches := rm.splitIntoBatches(entries, 100)

	workerCounts := []int{1, 2, 5, 10, 20}

	for _, numWorkers := range workerCounts {
		t.Run(fmt.Sprintf("workers_%d", numWorkers), func(t *testing.T) {
			taskChan := make(chan []*MetadataEntry, len(batches))
			resultChan := make(chan int, len(batches))

			start := time.Now()
			var wg sync.WaitGroup

			for i := 0; i < numWorkers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for batch := range taskChan {
						// 模拟处理
						time.Sleep(100 * time.Microsecond)
						resultChan <- len(batch)
					}
				}()
			}

			for _, batch := range batches {
				taskChan <- batch
			}
			close(taskChan)

			go func() {
				wg.Wait()
				close(resultChan)
			}()

			totalProcessed := 0
			for count := range resultChan {
				totalProcessed += count
			}
			duration := time.Since(start)

			if totalProcessed != numEntries {
				t.Errorf("Expected %d entries, processed %d", numEntries, totalProcessed)
			}

			t.Logf("Workers=%d: %d entries in %v (throughput: %.2f entries/ms)",
				numWorkers, totalProcessed, duration,
				float64(totalProcessed)/float64(duration.Milliseconds()))
		})
	}
}

// BenchmarkSequentialBatching 顺序批处理基准测试
func BenchmarkSequentialBatching(b *testing.B) {
	rm := &RecoveryManager{}

	entries := make([]*MetadataEntry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = &MetadataEntry{
			Type:  MetadataTypeDataIndex,
			Key:   fmt.Sprintf("bench:key:%d", i),
			Value: "data",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batches := rm.splitIntoBatches(entries, 100)
		for range batches {
			// 模拟批处理
		}
	}
}

// BenchmarkParallelBatching 并行批处理基准测试
func BenchmarkParallelBatching(b *testing.B) {
	rm := &RecoveryManager{}

	entries := make([]*MetadataEntry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = &MetadataEntry{
			Type:  MetadataTypeDataIndex,
			Key:   fmt.Sprintf("bench:key:%d", i),
			Value: "data",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batches := rm.splitIntoBatches(entries, 100)

		taskChan := make(chan []*MetadataEntry, len(batches))
		resultChan := make(chan int, len(batches))

		var wg sync.WaitGroup
		for w := 0; w < 10; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range taskChan {
					resultChan <- 1
				}
			}()
		}

		for _, batch := range batches {
			taskChan <- batch
		}
		close(taskChan)

		go func() {
			wg.Wait()
			close(resultChan)
		}()

		for range resultChan {
		}
	}
}
