package query

import (
	"minIODB/pkg/logger"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// FileCache 本地文件缓存管理器
type FileCache struct {
	cacheDir     string
	maxCacheSize int64
	maxFileAge   time.Duration
	redisClient  *redis.Client
	logger       *zap.Logger
	mu           sync.RWMutex
	currentSize  int64
	cacheIndex   map[string]*FileCacheEntry
	accessStats  map[string]time.Time
	stopChan     chan struct{}
}

// FileCacheConfig 文件缓存配置
type FileCacheConfig struct {
	CacheDir        string        `yaml:"cache_dir"`        // 缓存目录
	MaxCacheSize    int64         `yaml:"max_cache_size"`   // 最大缓存大小（字节）
	MaxFileAge      time.Duration `yaml:"max_file_age"`     // 文件最大保留时间
	CleanupInterval time.Duration `yaml:"cleanup_interval"` // 清理间隔
}

// FileCacheEntry 文件缓存条目
type FileCacheEntry struct {
	ObjectName   string    `json:"object_name"`   // 对象存储中的文件名
	LocalPath    string    `json:"local_path"`    // 本地文件路径
	Size         int64     `json:"size"`          // 文件大小
	CreatedAt    time.Time `json:"created_at"`    // 创建时间
	LastAccessed time.Time `json:"last_accessed"` // 最后访问时间
	AccessCount  int64     `json:"access_count"`  // 访问次数
	Hash         string    `json:"hash"`          // 文件哈希
}

// FileCacheStats 文件缓存统计
type FileCacheStats struct {
	TotalFiles  int64   `json:"total_files"`
	TotalSize   int64   `json:"total_size"`
	CacheHits   int64   `json:"cache_hits"`
	CacheMisses int64   `json:"cache_misses"`
	HitRatio    float64 `json:"hit_ratio"`
	OldestFile  string  `json:"oldest_file"`
	NewestFile  string  `json:"newest_file"`
	AvgFileSize int64   `json:"avg_file_size"`
}

// NewFileCache 创建文件缓存管理器
func NewFileCache(config *FileCacheConfig, redisClient *redis.Client, logger *zap.Logger) (*FileCache, error) {
	if config == nil {
		config = &FileCacheConfig{
			CacheDir:        filepath.Join(os.TempDir(), "miniodb_file_cache"),
			MaxCacheSize:    500 * 1024 * 1024, // 500MB
			MaxFileAge:      2 * time.Hour,
			CleanupInterval: 10 * time.Minute,
		}
	}

	// 确保缓存目录存在
	if err := os.MkdirAll(config.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	fc := &FileCache{
		cacheDir:     config.CacheDir,
		maxCacheSize: config.MaxCacheSize,
		maxFileAge:   config.MaxFileAge,
		redisClient:  redisClient,
		logger:       logger,
		cacheIndex:   make(map[string]*FileCacheEntry),
		accessStats:  make(map[string]time.Time),
		stopChan:     make(chan struct{}),
	}

	// 初始化缓存索引
	if err := fc.loadCacheIndex(); err != nil {
		logger.Warn("Failed to load cache index", zap.Error(err))
	}

	// 启动清理协程
	go fc.startCleanupRoutine(config.CleanupInterval)

	return fc, nil
}

// Get 获取缓存文件，如果不存在则下载
func (fc *FileCache) Get(ctx context.Context, objectName string, downloadFunc func(string) (string, error)) (string, error) {
	// 生成缓存键
	cacheKey := fc.generateCacheKey(objectName)

	fc.mu.RLock()
	entry, exists := fc.cacheIndex[cacheKey]
	fc.mu.RUnlock()

	// 检查缓存是否存在且有效
	if exists && fc.isCacheValid(entry) {
		// 更新访问时间和统计
		fc.updateAccessStats(cacheKey, entry)
		logger.GetLogger().Sugar().Infof("File cache HIT: %s", objectName)
		return entry.LocalPath, nil
	}

	// 缓存未命中，需要下载文件
	logger.GetLogger().Sugar().Infof("File cache MISS: %s", objectName)

	// 下载文件
	tempPath, err := downloadFunc(objectName)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}

	// 将文件移动到缓存目录
	cachedPath, err := fc.cacheFile(objectName, tempPath)
	if err != nil {
		// 清理临时文件
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to cache file: %w", err)
	}

	return cachedPath, nil
}

// Put 直接将文件放入缓存
func (fc *FileCache) Put(objectName, filePath string) (string, error) {
	return fc.cacheFile(objectName, filePath)
}

// cacheFile 将文件放入缓存
func (fc *FileCache) cacheFile(objectName, sourcePath string) (string, error) {
	// 获取文件信息
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to get file info: %w", err)
	}

	// 检查缓存空间
	if err := fc.ensureCacheSpace(fileInfo.Size()); err != nil {
		return "", fmt.Errorf("failed to ensure cache space: %w", err)
	}

	// 生成缓存路径
	cacheKey := fc.generateCacheKey(objectName)
	cachedPath := filepath.Join(fc.cacheDir, cacheKey)

	// 复制文件到缓存目录
	if err := fc.copyFile(sourcePath, cachedPath); err != nil {
		return "", fmt.Errorf("failed to copy file to cache: %w", err)
	}

	// 计算文件哈希
	hash, err := fc.calculateFileHash(cachedPath)
	if err != nil {
		logger.GetLogger().Sugar().Infof("WARN: failed to calculate file hash: %v", err)
		hash = ""
	}

	// 创建缓存条目
	entry := &FileCacheEntry{
		ObjectName:   objectName,
		LocalPath:    cachedPath,
		Size:         fileInfo.Size(),
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
		AccessCount:  1,
		Hash:         hash,
	}

	// 更新缓存索引
	fc.mu.Lock()
	fc.cacheIndex[cacheKey] = entry
	fc.currentSize += fileInfo.Size()
	fc.mu.Unlock()

	// 持久化缓存索引
	if err := fc.saveCacheIndex(); err != nil {
		fc.logger.Warn("Failed to save cache index", zap.Error(err))
	}

	logger.GetLogger().Sugar().Infof("File cached: %s -> %s (size: %d bytes)", objectName, cachedPath, fileInfo.Size())
	return cachedPath, nil
}

// generateCacheKey 生成缓存键
func (fc *FileCache) generateCacheKey(objectName string) string {
	// 使用MD5哈希生成文件名，避免路径问题
	hash := md5.Sum([]byte(objectName))
	hashStr := hex.EncodeToString(hash[:])

	// 保留原始文件扩展名
	ext := filepath.Ext(objectName)
	if ext == "" {
		ext = ".parquet" // 默认扩展名
	}

	return hashStr + ext
}

// isCacheValid 检查缓存是否有效
func (fc *FileCache) isCacheValid(entry *FileCacheEntry) bool {
	// 检查文件是否存在
	if _, err := os.Stat(entry.LocalPath); os.IsNotExist(err) {
		return false
	}

	// 检查文件年龄
	if time.Since(entry.CreatedAt) > fc.maxFileAge {
		return false
	}

	// 检查文件内容完整性（如果有哈希值）
	if entry.Hash != "" {
		if !fc.validateFileHash(entry.LocalPath, entry.Hash) {
			return false
		}
	}

	return true
}

// validateFileHash 验证文件哈希值
func (fc *FileCache) validateFileHash(filePath, expectedHash string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	return actualHash == expectedHash
}

// updateAccessStats 更新访问统计
func (fc *FileCache) updateAccessStats(cacheKey string, entry *FileCacheEntry) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	entry.LastAccessed = time.Now()
	entry.AccessCount++
	fc.accessStats[cacheKey] = time.Now()
}

// ensureCacheSpace 确保缓存空间足够
func (fc *FileCache) ensureCacheSpace(requiredSpace int64) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// 检查是否需要清理空间
	if fc.currentSize+requiredSpace <= fc.maxCacheSize {
		return nil
	}

	// 需要清理空间，按LRU策略删除文件
	freedSpace := int64(0)

	// 按最后访问时间排序
	var entries []*FileCacheEntry
	for _, entry := range fc.cacheIndex {
		entries = append(entries, entry)
	}

	// 简单的LRU实现：删除最久未访问的文件
	for _, entry := range entries {
		if freedSpace >= requiredSpace {
			break
		}

		if err := fc.removeFromCache(entry); err != nil {
			fc.logger.Warn("Failed to remove file from cache",
				zap.String("file", entry.ObjectName), zap.Error(err))
			continue
		}

		freedSpace += entry.Size
		logger.GetLogger().Sugar().Infof("Evicted cached file: %s (size: %d)", entry.ObjectName, entry.Size)
	}

	return nil
}

// removeFromCache 从缓存中移除文件
func (fc *FileCache) removeFromCache(entry *FileCacheEntry) error {
	// 删除物理文件
	if err := os.Remove(entry.LocalPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// 从索引中移除
	cacheKey := fc.generateCacheKey(entry.ObjectName)
	delete(fc.cacheIndex, cacheKey)
	delete(fc.accessStats, cacheKey)
	fc.currentSize -= entry.Size

	return nil
}

// copyFile 复制文件
func (fc *FileCache) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// calculateFileHash 计算文件哈希（使用SHA256）
func (fc *FileCache) calculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// loadCacheIndex 加载缓存索引
func (fc *FileCache) loadCacheIndex() error {
	// 扫描缓存目录，重建索引
	entries, err := os.ReadDir(fc.cacheDir)
	if err != nil {
		return err
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.currentSize = 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(fc.cacheDir, entry.Name())
		fileInfo, err := entry.Info()
		if err != nil {
			continue
		}

		// 从Redis加载缓存元数据（如果存在）
		cacheKey := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		metadataKey := "file_cache_meta:" + cacheKey

		var objectName string
		var createdAt time.Time
		var accessCount int64

		if fc.redisClient != nil {
			metadata, err := fc.redisClient.HGetAll(context.Background(), metadataKey).Result()
			if err == nil && len(metadata) > 0 {
				objectName = metadata["object_name"]
				if createdAtStr := metadata["created_at"]; createdAtStr != "" {
					if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
						createdAt = parsed
					}
				}
			}
		}

		if objectName == "" {
			objectName = entry.Name() // 使用文件名作为回退
		}
		if createdAt.IsZero() {
			createdAt = fileInfo.ModTime()
		}

		cacheEntry := &FileCacheEntry{
			ObjectName:   objectName,
			LocalPath:    filePath,
			Size:         fileInfo.Size(),
			CreatedAt:    createdAt,
			LastAccessed: fileInfo.ModTime(),
			AccessCount:  accessCount,
		}

		fc.cacheIndex[entry.Name()] = cacheEntry
		fc.currentSize += fileInfo.Size()
	}

	logger.GetLogger().Sugar().Infof("Loaded file cache index: %d files, total size: %d bytes",
		len(fc.cacheIndex), fc.currentSize)
	return nil
}

// saveCacheIndex 保存缓存索引到Redis
func (fc *FileCache) saveCacheIndex() error {
	if fc.redisClient == nil {
		return nil // 如果没有Redis客户端，跳过保存
	}

	ctx := context.Background()
	for cacheKey, entry := range fc.cacheIndex {
		metadataKey := "file_cache_meta:" + strings.TrimSuffix(cacheKey, filepath.Ext(cacheKey))

		metadata := map[string]interface{}{
			"object_name":   entry.ObjectName,
			"local_path":    entry.LocalPath,
			"size":          entry.Size,
			"created_at":    entry.CreatedAt.Format(time.RFC3339),
			"last_accessed": entry.LastAccessed.Format(time.RFC3339),
			"access_count":  entry.AccessCount,
		}

		if err := fc.redisClient.HMSet(ctx, metadataKey, metadata).Err(); err != nil {
			fc.logger.Warn("Failed to save cache metadata",
				zap.String("key", metadataKey), zap.Error(err))
		}

		// 设置TTL
		fc.redisClient.Expire(ctx, metadataKey, fc.maxFileAge*2)
	}

	return nil
}

// startCleanupRoutine 启动清理协程
func (fc *FileCache) startCleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fc.cleanup()
		case <-fc.stopChan:
			return
		}
	}
}

// cleanup 清理过期文件
func (fc *FileCache) cleanup() {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	var toRemove []*FileCacheEntry
	now := time.Now()

	for _, entry := range fc.cacheIndex {
		// 检查文件是否过期
		if now.Sub(entry.CreatedAt) > fc.maxFileAge {
			toRemove = append(toRemove, entry)
		}
	}

	// 移除过期文件
	for _, entry := range toRemove {
		if err := fc.removeFromCache(entry); err != nil {
			fc.logger.Warn("Failed to remove expired file",
				zap.String("file", entry.ObjectName), zap.Error(err))
		} else {
			logger.GetLogger().Sugar().Infof("Removed expired cached file: %s", entry.ObjectName)
		}
	}

	if len(toRemove) > 0 {
		fc.saveCacheIndex()
	}
}

// GetStats 获取缓存统计
func (fc *FileCache) GetStats() *FileCacheStats {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	stats := &FileCacheStats{
		TotalFiles: int64(len(fc.cacheIndex)),
		TotalSize:  fc.currentSize,
	}

	if stats.TotalFiles > 0 {
		stats.AvgFileSize = stats.TotalSize / stats.TotalFiles

		// 找到最新和最旧的文件
		var oldest, newest time.Time
		for _, entry := range fc.cacheIndex {
			if oldest.IsZero() || entry.CreatedAt.Before(oldest) {
				oldest = entry.CreatedAt
				stats.OldestFile = entry.ObjectName
			}
			if newest.IsZero() || entry.CreatedAt.After(newest) {
				newest = entry.CreatedAt
				stats.NewestFile = entry.ObjectName
			}
		}
	}

	return stats
}

// Clear 清空缓存
func (fc *FileCache) Clear() error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// 删除所有缓存文件
	for _, entry := range fc.cacheIndex {
		os.Remove(entry.LocalPath)
	}

	// 清空索引
	fc.cacheIndex = make(map[string]*FileCacheEntry)
	fc.accessStats = make(map[string]time.Time)
	fc.currentSize = 0

	logger.GetLogger().Info("File cache cleared")
	return nil
}

// Stop 停止文件缓存清理协程
func (fc *FileCache) Stop() {
	if fc.stopChan != nil {
		close(fc.stopChan)
	}
}

// Contains 检查文件是否在缓存中
func (fc *FileCache) Contains(objectName string) bool {
	cacheKey := fc.generateCacheKey(objectName)

	fc.mu.RLock()
	entry, exists := fc.cacheIndex[cacheKey]
	fc.mu.RUnlock()

	return exists && fc.isCacheValid(entry)
}
