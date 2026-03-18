package storage

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// MemoryOptimizer 内存优化器
type MemoryOptimizer struct {
	memoryPools     map[string]*MemoryPool
	bufferOptimizer *BufferOptimizer
	zeroCopyManager *ZeroCopyManager
	gcManager       *GCManager
	stats           *MemoryStats
	config          *MemoryConfig
	mutex           sync.RWMutex
	logger          *zap.Logger
}

// MemoryConfig 内存配置
type MemoryConfig struct {
	MaxMemoryUsage   int64          `json:"max_memory_usage"`
	PoolSizes        map[string]int `json:"pool_sizes"`
	GCInterval       time.Duration  `json:"gc_interval"`
	ZeroCopyEnabled  bool           `json:"zero_copy_enabled"`
	CompressionLevel int            `json:"compression_level"`
	EnableProfiling  bool           `json:"enable_profiling"`
}

// MemoryPool 内存池
type MemoryPool struct {
	name       string
	bufferSize int
	maxBuffers int
	buffers    chan []byte
	allocated  int64
	stats      *PoolStats
	mutex      sync.RWMutex
}

// PoolStats 内存池统计
type PoolStats struct {
	TotalAllocated    int64 `json:"total_allocated"`
	CurrentInUse      int64 `json:"current_in_use"`
	PoolHits          int64 `json:"pool_hits"`
	PoolMisses        int64 `json:"pool_misses"`
	AllocationCount   int64 `json:"allocation_count"`
	DeallocationCount int64 `json:"deallocation_count"`
}

// BufferOptimizer 缓冲区优化器
type BufferOptimizer struct {
	readBuffers  map[string]*ReadBuffer
	writeBuffers map[string]*WriteBuffer
	config       *BufferConfig
	stats        *BufferStats
	mutex        sync.RWMutex
}

// BufferConfig 缓冲区配置
type BufferConfig struct {
	ReadBufferSize  int     `json:"read_buffer_size"`
	WriteBufferSize int     `json:"write_buffer_size"`
	FlushThreshold  float64 `json:"flush_threshold"`
	CompressionType string  `json:"compression_type"`
	EnableBatching  bool    `json:"enable_batching"`
	BatchSize       int     `json:"batch_size"`
}

// ReadBuffer 读取缓冲区
type ReadBuffer struct {
	name       string
	data       []byte
	size       int
	position   int
	hits       int64
	misses     int64
	lastAccess time.Time
	compressed bool
	mutex      sync.RWMutex
}

// WriteBuffer 写入缓冲区
type WriteBuffer struct {
	name       string
	data       []byte
	size       int
	position   int
	flushCount int64
	lastFlush  time.Time
	dirty      bool
	mutex      sync.RWMutex
}

// BufferStats 缓冲区统计
type BufferStats struct {
	ReadHitRate      float64 `json:"read_hit_rate"`
	WriteBufferUsage float64 `json:"write_buffer_usage"`
	FlushFrequency   float64 `json:"flush_frequency"`
	CompressionRatio float64 `json:"compression_ratio"`
}

// ZeroCopyManager 零拷贝管理器
type ZeroCopyManager struct {
	mappedRegions map[string]*MappedRegion
	mmapManager   *MmapManager
	copyStats     *CopyStats
	enabled       bool
	mutex         sync.RWMutex
	logger        *zap.Logger
}

// MappedRegion 内存映射区域
type MappedRegion struct {
	name     string
	data     []byte
	size     int64
	readonly bool
	mapped   bool
	refCount int32
	lastUsed time.Time
	file     *os.File  // 映射的文件
	mmapFile *MmapFile // mmap 文件对象
	filePath string    // 文件路径（用于日志）
}

// CopyStats 拷贝统计
type CopyStats struct {
	ZeroCopyOperations int64         `json:"zero_copy_operations"`
	StandardCopyOps    int64         `json:"standard_copy_operations"`
	BytesSaved         int64         `json:"bytes_saved"`
	TimeSaved          time.Duration `json:"time_saved"`
}

// GCManager 垃圾回收管理器
type GCManager struct {
	gcInterval     time.Duration
	lastGC         time.Time
	gcStats        *GCStats
	forceGCTrigger chan struct{}
	stopChan       chan struct{}
	running        bool
	mutex          sync.RWMutex
	logger         *zap.Logger
}

// GCStats 垃圾回收统计
type GCStats struct {
	GCCount     int64         `json:"gc_count"`
	TotalGCTime time.Duration `json:"total_gc_time"`
	AvgGCTime   time.Duration `json:"avg_gc_time"`
	MemoryFreed int64         `json:"memory_freed"`
	LastGC      time.Time     `json:"last_gc"`
}

// MemoryStats 内存统计
type MemoryStats struct {
	TotalAllocated     int64     `json:"total_allocated"`
	CurrentUsage       int64     `json:"current_usage"`
	PeakUsage          int64     `json:"peak_usage"`
	PoolEfficiency     float64   `json:"pool_efficiency"`
	FragmentationRatio float64   `json:"fragmentation_ratio"`
	GCEfficiency       float64   `json:"gc_efficiency"`
	LastOptimization   time.Time `json:"last_optimization"`
	mutex              sync.RWMutex
}

// NewMemoryOptimizer 创建内存优化器
func NewMemoryOptimizer(config *MemoryConfig, logger *zap.Logger) *MemoryOptimizer {
	mo := &MemoryOptimizer{
		memoryPools:     make(map[string]*MemoryPool),
		bufferOptimizer: NewBufferOptimizer(),
		zeroCopyManager: NewZeroCopyManager(logger),
		gcManager:       NewGCManager(config.GCInterval, logger),
		stats:           &MemoryStats{},
		config:          config,
		logger:          logger,
	}

	// 初始化内存池
	mo.initMemoryPools()

	// 启动GC管理器
	mo.gcManager.Start()

	return mo
}

// NewBufferOptimizer 创建缓冲区优化器
func NewBufferOptimizer() *BufferOptimizer {
	return &BufferOptimizer{
		readBuffers:  make(map[string]*ReadBuffer),
		writeBuffers: make(map[string]*WriteBuffer),
		config: &BufferConfig{
			ReadBufferSize:  64 * 1024,  // 64KB
			WriteBufferSize: 128 * 1024, // 128KB
			FlushThreshold:  0.8,        // 80%
			CompressionType: "lz4",
			EnableBatching:  true,
			BatchSize:       100,
		},
		stats: &BufferStats{},
	}
}

// NewZeroCopyManager 创建零拷贝管理器
func NewZeroCopyManager(logger *zap.Logger) *ZeroCopyManager {
	return &ZeroCopyManager{
		mappedRegions: make(map[string]*MappedRegion),
		mmapManager:   NewMmapManager(),
		copyStats:     &CopyStats{},
		enabled:       true,
		logger:        logger,
	}
}

// NewGCManager 创建GC管理器
func NewGCManager(interval time.Duration, logger *zap.Logger) *GCManager {
	return &GCManager{
		gcInterval:     interval,
		gcStats:        &GCStats{},
		forceGCTrigger: make(chan struct{}, 1),
		stopChan:       make(chan struct{}),
		logger:         logger,
	}
}

// initMemoryPools 初始化内存池
func (mo *MemoryOptimizer) initMemoryPools() {
	poolConfigs := map[string]struct {
		bufferSize int
		maxBuffers int
	}{
		"small":  {4 * 1024, 1000},      // 4KB buffers
		"medium": {64 * 1024, 500},      // 64KB buffers
		"large":  {1024 * 1024, 100},    // 1MB buffers
		"xlarge": {4 * 1024 * 1024, 25}, // 4MB buffers
	}

	for name, config := range poolConfigs {
		pool := &MemoryPool{
			name:       name,
			bufferSize: config.bufferSize,
			maxBuffers: config.maxBuffers,
			buffers:    make(chan []byte, config.maxBuffers),
			stats:      &PoolStats{},
		}

		// 预分配一些buffer
		for i := 0; i < config.maxBuffers/4; i++ {
			buffer := make([]byte, config.bufferSize)
			select {
			case pool.buffers <- buffer:
				atomic.AddInt64(&pool.allocated, int64(config.bufferSize))
			default:
				break
			}
		}

		mo.memoryPools[name] = pool
		mo.logger.Sugar().Infof("Initialized memory pool: %s (bufferSize: %d, maxBuffers: %d)",
			name, config.bufferSize, config.maxBuffers)
	}
}

// GetBuffer 获取缓冲区
func (mo *MemoryOptimizer) GetBuffer(size int) []byte {
	poolName := mo.selectPool(size)
	pool := mo.memoryPools[poolName]

	if pool == nil {
		if !mo.checkMemoryLimit(int64(size)) {
			mo.logger.Sugar().Infof("WARN: memory limit exceeded, returning nil buffer")
			return nil
		}
		atomic.AddInt64(&mo.stats.TotalAllocated, int64(size))
		return make([]byte, size)
	}

	select {
	case buffer := <-pool.buffers:
		atomic.AddInt64(&pool.stats.PoolHits, 1)
		atomic.AddInt64(&pool.stats.CurrentInUse, int64(pool.bufferSize))
		return buffer[:size]
	default:
		if !mo.checkMemoryLimit(int64(pool.bufferSize)) {
			mo.logger.Sugar().Infof("WARN: memory limit exceeded, returning nil buffer")
			return nil
		}
		atomic.AddInt64(&pool.stats.PoolMisses, 1)
		atomic.AddInt64(&pool.stats.AllocationCount, 1)
		atomic.AddInt64(&pool.allocated, int64(pool.bufferSize))
		atomic.AddInt64(&mo.stats.TotalAllocated, int64(pool.bufferSize))

		return make([]byte, size)
	}
}

func (mo *MemoryOptimizer) checkMemoryLimit(requestedSize int64) bool {
	if mo.config == nil || mo.config.MaxMemoryUsage <= 0 {
		return true
	}
	currentUsage := atomic.LoadInt64(&mo.stats.TotalAllocated)
	return currentUsage+requestedSize <= mo.config.MaxMemoryUsage
}

// ReturnBuffer 归还缓冲区
func (mo *MemoryOptimizer) ReturnBuffer(buffer []byte) {
	size := len(buffer)
	poolName := mo.selectPool(size)
	pool := mo.memoryPools[poolName]

	if pool == nil {
		return
	}

	// 重置buffer
	for i := range buffer {
		buffer[i] = 0
	}

	select {
	case pool.buffers <- buffer:
		atomic.AddInt64(&pool.stats.DeallocationCount, 1)
		atomic.AddInt64(&pool.stats.CurrentInUse, -int64(pool.bufferSize))
	default:
		// 池已满，直接丢弃
	}
}

// selectPool 选择合适的内存池
func (mo *MemoryOptimizer) selectPool(size int) string {
	if size <= 4*1024 {
		return "small"
	} else if size <= 64*1024 {
		return "medium"
	} else if size <= 1024*1024 {
		return "large"
	} else {
		return "xlarge"
	}
}

// CreateReadBuffer 创建读取缓冲区
func (mo *MemoryOptimizer) CreateReadBuffer(name string, size int) *ReadBuffer {
	mo.bufferOptimizer.mutex.Lock()
	defer mo.bufferOptimizer.mutex.Unlock()

	buffer := &ReadBuffer{
		name:       name,
		data:       make([]byte, size),
		size:       size,
		position:   0,
		lastAccess: time.Now(),
		compressed: false,
	}

	mo.bufferOptimizer.readBuffers[name] = buffer
	mo.logger.Sugar().Infof("Created read buffer: %s (size: %d)", name, size)

	return buffer
}

// CreateWriteBuffer 创建写入缓冲区
func (mo *MemoryOptimizer) CreateWriteBuffer(name string, size int) *WriteBuffer {
	mo.bufferOptimizer.mutex.Lock()
	defer mo.bufferOptimizer.mutex.Unlock()

	buffer := &WriteBuffer{
		name:      name,
		data:      make([]byte, size),
		size:      size,
		position:  0,
		lastFlush: time.Now(),
		dirty:     false,
	}

	mo.bufferOptimizer.writeBuffers[name] = buffer
	mo.logger.Sugar().Infof("Created write buffer: %s (size: %d)", name, size)

	return buffer
}

// Read 从读取缓冲区读取数据
func (rb *ReadBuffer) Read(data []byte) (int, error) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	rb.lastAccess = time.Now()

	if rb.position >= rb.size {
		return 0, fmt.Errorf("buffer overflow")
	}

	n := copy(data, rb.data[rb.position:])
	rb.position += n

	if n > 0 {
		atomic.AddInt64(&rb.hits, 1)
	} else {
		atomic.AddInt64(&rb.misses, 1)
	}

	return n, nil
}

// Write 写入数据到写入缓冲区
func (wb *WriteBuffer) Write(data []byte) (int, error) {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	if wb.position+len(data) > wb.size {
		return 0, fmt.Errorf("buffer overflow")
	}

	n := copy(wb.data[wb.position:], data)
	wb.position += n
	wb.dirty = true

	return n, nil
}

// Flush 刷新写入缓冲区
func (wb *WriteBuffer) Flush() error {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	if !wb.dirty {
		return nil
	}

	// 这里应该实际写入到存储
	// 简化起见，只是重置状态
	wb.position = 0
	wb.dirty = false
	wb.lastFlush = time.Now()
	atomic.AddInt64(&wb.flushCount, 1)

	return nil
}

// CreateMappedRegion 创建内存映射区域（纯内存，无文件）
// 已弃用：建议使用 CreateMappedRegionFromFile 进行真正的文件 mmap
func (zcm *ZeroCopyManager) CreateMappedRegion(name string, size int64, readonly bool) (*MappedRegion, error) {
	zcm.mutex.Lock()
	defer zcm.mutex.Unlock()

	if !zcm.enabled {
		return nil, fmt.Errorf("zero copy disabled")
	}

	region := &MappedRegion{
		name:     name,
		data:     make([]byte, size), // 纯内存实现（无文件时回退）
		size:     size,
		readonly: readonly,
		mapped:   false, // 标记为非真正的 mmap
		refCount: 1,
		lastUsed: time.Now(),
	}

	zcm.mappedRegions[name] = region
	zcm.logger.Sugar().Infof("Created in-memory region: %s (size: %d, readonly: %v)", name, size, readonly)

	return region, nil
}

// CreateMappedRegionFromFile 从文件创建内存映射区域（真正的 mmap）
func (zcm *ZeroCopyManager) CreateMappedRegionFromFile(name string, f *os.File, readonly bool) (*MappedRegion, error) {
	zcm.mutex.Lock()
	defer zcm.mutex.Unlock()

	if !zcm.enabled {
		return nil, fmt.Errorf("zero copy disabled")
	}

	// 检查是否已存在
	if existing, ok := zcm.mappedRegions[name]; ok {
		atomic.AddInt32(&existing.refCount, 1)
		existing.lastUsed = time.Now()
		return existing, nil
	}

	// 获取文件信息
	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file stat: %w", err)
	}

	size := stat.Size()
	if size == 0 {
		return nil, fmt.Errorf("cannot mmap empty file")
	}

	// 使用 MmapManager 创建映射
	mmapFile, err := zcm.mmapManager.CreateMmapFile(name, f, size, readonly)
	if err != nil {
		// mmap 失败，回退到读取整个文件到内存
		zcm.logger.Warn("mmap failed, falling back to read entire file",
			zap.String("name", name),
			zap.Error(err))

		data := make([]byte, size)
		if _, readErr := f.ReadAt(data, 0); readErr != nil {
			return nil, fmt.Errorf("fallback read failed: %w", readErr)
		}

		region := &MappedRegion{
			name:     name,
			data:     data,
			size:     size,
			readonly: readonly,
			mapped:   false,
			refCount: 1,
			file:     f,
			lastUsed: time.Now(),
		}

		zcm.mappedRegions[name] = region
		return region, nil
	}

	region := &MappedRegion{
		name:     name,
		data:     mmapFile.Data(),
		size:     size,
		readonly: readonly,
		mapped:   true, // 标记为真正的 mmap
		refCount: 1,
		file:     f,
		mmapFile: mmapFile,
		lastUsed: time.Now(),
	}

	zcm.mappedRegions[name] = region
	zcm.logger.Sugar().Infof("Created mmap region: %s (size: %d, readonly: %v)", name, size, readonly)

	return region, nil
}

// CreateMappedRegionFromPath 从文件路径创建内存映射区域
func (zcm *ZeroCopyManager) CreateMappedRegionFromPath(name, filePath string, readonly bool) (*MappedRegion, error) {
	// 打开文件
	flag := os.O_RDONLY
	if !readonly {
		flag = os.O_RDWR
	}

	f, err := os.OpenFile(filePath, flag, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}

	region, err := zcm.CreateMappedRegionFromFile(name, f, readonly)
	if err != nil {
		f.Close()
		return nil, err
	}

	region.filePath = filePath
	return region, nil
}

// ZeroCopyRead 零拷贝读取
func (zcm *ZeroCopyManager) ZeroCopyRead(regionName string, offset, length int64) ([]byte, error) {
	zcm.mutex.RLock()
	region, exists := zcm.mappedRegions[regionName]
	zcm.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("mapped region not found: %s", regionName)
	}

	if offset+length > region.size {
		return nil, fmt.Errorf("read beyond region boundary")
	}

	atomic.AddInt32(&region.refCount, 1)
	atomic.AddInt64(&zcm.copyStats.ZeroCopyOperations, 1)
	atomic.AddInt64(&zcm.copyStats.BytesSaved, length)

	region.lastUsed = time.Now()

	// 返回数据切片的引用（零拷贝）
	return region.data[offset : offset+length], nil
}

// ReleaseMappedRegion 释放内存映射区域
func (zcm *ZeroCopyManager) ReleaseMappedRegion(regionName string) error {
	zcm.mutex.Lock()
	defer zcm.mutex.Unlock()

	region, exists := zcm.mappedRegions[regionName]
	if !exists {
		return fmt.Errorf("mapped region not found: %s", regionName)
	}

	newRefCount := atomic.AddInt32(&region.refCount, -1)
	if newRefCount <= 0 {
		// 引用计数归零，执行清理
		delete(zcm.mappedRegions, regionName)

		// 如果是真正的 mmap，解除映射
		if region.mapped && region.mmapFile != nil {
			if err := zcm.mmapManager.CloseMmapFile(regionName); err != nil {
				zcm.logger.Warn("failed to unmap region",
					zap.String("region", regionName),
					zap.Error(err))
			}
		}

		// 关闭文件（如果由本模块打开）
		if region.file != nil && region.filePath != "" {
			region.file.Close()
		}

		zcm.logger.Sugar().Infof("Released mapped region: %s", regionName)
	}

	return nil
}

// Close 关闭 ZeroCopyManager，释放所有资源
func (zcm *ZeroCopyManager) Close() error {
	zcm.mutex.Lock()
	defer zcm.mutex.Unlock()

	var lastErr error
	for name, region := range zcm.mappedRegions {
		// 如果是真正的 mmap，解除映射
		if region.mapped && region.mmapFile != nil {
			if err := zcm.mmapManager.CloseMmapFile(name); err != nil {
				lastErr = err
				zcm.logger.Warn("failed to unmap region during close",
					zap.String("region", name),
					zap.Error(err))
			}
		}

		// 关闭文件（如果由本模块打开）
		if region.file != nil && region.filePath != "" {
			region.file.Close()
		}

		delete(zcm.mappedRegions, name)
	}

	zcm.enabled = false
	return lastErr
}

// Start 启动GC管理器
func (gcm *GCManager) Start() {
	gcm.mutex.Lock()
	if gcm.running {
		gcm.mutex.Unlock()
		return
	}
	gcm.running = true
	gcm.mutex.Unlock()

	go gcm.gcLoop()
	gcm.logger.Sugar().Infof("GC manager started with interval: %v", gcm.gcInterval)
}

// Stop 停止GC管理器
func (gcm *GCManager) Stop() {
	gcm.mutex.Lock()
	defer gcm.mutex.Unlock()

	if !gcm.running {
		return
	}

	close(gcm.stopChan)
	gcm.running = false
	gcm.logger.Info("GC manager stopped")
}

// gcLoop GC循环
func (gcm *GCManager) gcLoop() {
	ticker := time.NewTicker(gcm.gcInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gcm.performGC()
		case <-gcm.forceGCTrigger:
			gcm.performGC()
		case <-gcm.stopChan:
			return
		}
	}
}

// performGC 执行垃圾回收
func (gcm *GCManager) performGC() {
	startTime := time.Now()

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	gcTime := time.Since(startTime)
	memoryFreed := int64(memBefore.Alloc - memAfter.Alloc)

	gcm.mutex.Lock()
	gcm.gcStats.GCCount++
	gcm.gcStats.TotalGCTime += gcTime
	gcm.gcStats.AvgGCTime = gcm.gcStats.TotalGCTime / time.Duration(gcm.gcStats.GCCount)
	gcm.gcStats.MemoryFreed += memoryFreed
	gcm.gcStats.LastGC = time.Now()
	gcm.lastGC = time.Now()
	gcm.mutex.Unlock()

	gcm.logger.Sugar().Infof("GC completed: freed %d bytes in %v", memoryFreed, gcTime)
}

// ForceGC 强制垃圾回收
func (gcm *GCManager) ForceGC() {
	select {
	case gcm.forceGCTrigger <- struct{}{}:
	default:
		// 已有GC请求在队列中
	}
}

// OptimizeMemory 优化内存使用
func (mo *MemoryOptimizer) OptimizeMemory(ctx context.Context) error {
	mo.logger.Info("Starting memory optimization...")

	startTime := time.Now()

	// 优化内存池
	mo.optimizeMemoryPools()

	// 优化缓冲区
	mo.optimizeBuffers()

	// 清理零拷贝区域
	mo.cleanupZeroCopyRegions()

	// 强制GC
	mo.gcManager.ForceGC()

	// 更新统计信息
	mo.updateMemoryStats()

	optimizationTime := time.Since(startTime)
	mo.stats.mutex.Lock()
	mo.stats.LastOptimization = time.Now()
	mo.stats.mutex.Unlock()

	mo.logger.Sugar().Infof("Memory optimization completed in %v", optimizationTime)
	return nil
}

// optimizeMemoryPools 优化内存池
func (mo *MemoryOptimizer) optimizeMemoryPools() {
	for name, pool := range mo.memoryPools {
		pool.mutex.Lock()

		// 计算池效率
		totalOps := pool.stats.PoolHits + pool.stats.PoolMisses
		if totalOps > 0 {
			hitRate := float64(pool.stats.PoolHits) / float64(totalOps)
			mo.logger.Sugar().Infof("Pool %s hit rate: %.2f%%", name, hitRate*100)

			// 如果命中率过低，考虑调整池大小
			if hitRate < 0.5 && len(pool.buffers) < pool.maxBuffers {
				// 增加预分配的buffer
				for i := 0; i < 10 && len(pool.buffers) < pool.maxBuffers; i++ {
					buffer := make([]byte, pool.bufferSize)
					select {
					case pool.buffers <- buffer:
						atomic.AddInt64(&pool.allocated, int64(pool.bufferSize))
					default:
						break
					}
				}
			}
		}

		pool.mutex.Unlock()
	}
}

// optimizeBuffers 优化缓冲区
func (mo *MemoryOptimizer) optimizeBuffers() {
	mo.bufferOptimizer.mutex.Lock()
	defer mo.bufferOptimizer.mutex.Unlock()

	now := time.Now()

	// 清理长时间未使用的读取缓冲区
	for name, buffer := range mo.bufferOptimizer.readBuffers {
		if now.Sub(buffer.lastAccess) > time.Hour {
			delete(mo.bufferOptimizer.readBuffers, name)
			mo.logger.Sugar().Infof("Removed unused read buffer: %s", name)
		}
	}

	// 刷新写入缓冲区
	for name, buffer := range mo.bufferOptimizer.writeBuffers {
		if buffer.dirty && now.Sub(buffer.lastFlush) > 5*time.Minute {
			buffer.Flush()
			mo.logger.Sugar().Infof("Flushed write buffer: %s", name)
		}
	}
}

// cleanupZeroCopyRegions 清理零拷贝区域
func (mo *MemoryOptimizer) cleanupZeroCopyRegions() {
	mo.zeroCopyManager.mutex.Lock()
	defer mo.zeroCopyManager.mutex.Unlock()

	now := time.Now()

	for name, region := range mo.zeroCopyManager.mappedRegions {
		if atomic.LoadInt32(&region.refCount) <= 0 && now.Sub(region.lastUsed) > time.Hour {
			// 如果是真正的 mmap，解除映射
			if region.mapped && region.mmapFile != nil {
				if err := mo.zeroCopyManager.mmapManager.CloseMmapFile(name); err != nil {
					mo.logger.Warn("failed to unmap region during cleanup",
						zap.String("region", name),
						zap.Error(err))
				}
			}

			// 关闭文件（如果由本模块打开）
			if region.file != nil && region.filePath != "" {
				region.file.Close()
			}

			delete(mo.zeroCopyManager.mappedRegions, name)
			mo.logger.Sugar().Infof("Cleaned up unused mapped region: %s", name)
		}
	}
}

// updateMemoryStats 更新内存统计
func (mo *MemoryOptimizer) updateMemoryStats() {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	mo.stats.mutex.Lock()
	defer mo.stats.mutex.Unlock()

	mo.stats.CurrentUsage = int64(stats.Alloc)
	if int64(stats.Alloc) > mo.stats.PeakUsage {
		mo.stats.PeakUsage = int64(stats.Alloc)
	}

	// 计算池效率
	totalHits := int64(0)
	totalOps := int64(0)

	for _, pool := range mo.memoryPools {
		totalHits += pool.stats.PoolHits
		totalOps += pool.stats.PoolHits + pool.stats.PoolMisses
	}

	if totalOps > 0 {
		mo.stats.PoolEfficiency = float64(totalHits) / float64(totalOps)
	}

	// 计算碎片化比例（简化计算）
	mo.stats.FragmentationRatio = float64(stats.Sys-stats.Alloc) / float64(stats.Sys)
}

// GetStats 获取内存统计信息
func (mo *MemoryOptimizer) GetStats() *MemoryStats {
	mo.stats.mutex.RLock()
	defer mo.stats.mutex.RUnlock()

	return &MemoryStats{
		TotalAllocated:     mo.stats.TotalAllocated,
		CurrentUsage:       mo.stats.CurrentUsage,
		PeakUsage:          mo.stats.PeakUsage,
		PoolEfficiency:     mo.stats.PoolEfficiency,
		FragmentationRatio: mo.stats.FragmentationRatio,
		GCEfficiency:       mo.stats.GCEfficiency,
		LastOptimization:   mo.stats.LastOptimization,
	}
}

// GetPoolStats 获取内存池统计信息
func (mo *MemoryOptimizer) GetPoolStats() map[string]*PoolStats {
	mo.mutex.RLock()
	defer mo.mutex.RUnlock()

	stats := make(map[string]*PoolStats)
	for name, pool := range mo.memoryPools {
		poolStats := *pool.stats
		stats[name] = &poolStats
	}

	return stats
}

// GetGCStats 获取GC统计信息
func (mo *MemoryOptimizer) GetGCStats() *GCStats {
	mo.gcManager.mutex.RLock()
	defer mo.gcManager.mutex.RUnlock()

	statsCopy := *mo.gcManager.gcStats
	return &statsCopy
}
