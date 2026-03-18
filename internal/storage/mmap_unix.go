//go:build !windows
// +build !windows

package storage

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// MmapFile 内存映射文件
type MmapFile struct {
	data     []byte
	file     *os.File
	size     int64
	readonly bool
	mutex    sync.RWMutex
}

// MmapManager mmap 管理器
type MmapManager struct {
	files   map[string]*MmapFile
	mutex   sync.RWMutex
	enabled bool
}

// NewMmapManager 创建 mmap 管理器
func NewMmapManager() *MmapManager {
	return &MmapManager{
		files:   make(map[string]*MmapFile),
		enabled: true,
	}
}

// Mmap 创建内存映射
func Mmap(f *os.File, offset int64, length int, readonly bool) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("file is nil")
	}

	if length <= 0 {
		return nil, fmt.Errorf("invalid length: %d", length)
	}

	prot := unix.PROT_READ
	if !readonly {
		prot |= unix.PROT_WRITE
	}

	flags := unix.MAP_SHARED

	data, err := unix.Mmap(int(f.Fd()), offset, length, prot, flags)
	if err != nil {
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	return data, nil
}

// Munmap 解除内存映射
func Munmap(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return unix.Munmap(data)
}

// Msync 同步内存映射到文件
func Msync(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return unix.Msync(data, unix.MS_SYNC)
}

// CreateMmapFile 创建内存映射文件
func (mm *MmapManager) CreateMmapFile(name string, f *os.File, size int64, readonly bool) (*MmapFile, error) {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	if !mm.enabled {
		return nil, fmt.Errorf("mmap manager disabled")
	}

	// 检查是否已存在
	if existing, ok := mm.files[name]; ok {
		return existing, nil
	}

	// 获取文件大小
	if size <= 0 {
		stat, err := f.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to get file stat: %w", err)
		}
		size = stat.Size()
	}

	if size == 0 {
		return nil, fmt.Errorf("cannot mmap empty file")
	}

	data, err := Mmap(f, 0, int(size), readonly)
	if err != nil {
		return nil, err
	}

	mf := &MmapFile{
		data:     data,
		file:     f,
		size:     size,
		readonly: readonly,
	}

	mm.files[name] = mf
	return mf, nil
}

// GetMmapFile 获取内存映射文件
func (mm *MmapManager) GetMmapFile(name string) (*MmapFile, bool) {
	mm.mutex.RLock()
	defer mm.mutex.RUnlock()

	mf, ok := mm.files[name]
	return mf, ok
}

// CloseMmapFile 关闭并释放内存映射文件
func (mm *MmapManager) CloseMmapFile(name string) error {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	mf, ok := mm.files[name]
	if !ok {
		return nil
	}

	delete(mm.files, name)
	return mf.Close()
}

// CloseAll 关闭所有内存映射文件
func (mm *MmapManager) CloseAll() error {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	var lastErr error
	for name, mf := range mm.files {
		if err := mf.Close(); err != nil {
			lastErr = err
		}
		delete(mm.files, name)
	}

	return lastErr
}

// Data 获取映射数据
func (mf *MmapFile) Data() []byte {
	mf.mutex.RLock()
	defer mf.mutex.RUnlock()
	return mf.data
}

// Size 获取映射大小
func (mf *MmapFile) Size() int64 {
	return mf.size
}

// ReadAt 从指定位置读取数据（零拷贝）
func (mf *MmapFile) ReadAt(offset, length int64) ([]byte, error) {
	mf.mutex.RLock()
	defer mf.mutex.RUnlock()

	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("invalid offset or length")
	}

	if offset+length > mf.size {
		return nil, fmt.Errorf("read beyond file boundary")
	}

	// 返回数据切片的引用（零拷贝）
	return mf.data[offset : offset+length], nil
}

// Sync 同步到文件
func (mf *MmapFile) Sync() error {
	mf.mutex.RLock()
	defer mf.mutex.RUnlock()

	if mf.readonly {
		return nil
	}

	return Msync(mf.data)
}

// Close 关闭内存映射
func (mf *MmapFile) Close() error {
	mf.mutex.Lock()
	defer mf.mutex.Unlock()

	if mf.data == nil {
		return nil
	}

	err := Munmap(mf.data)
	mf.data = nil

	// 注意：不关闭 file，由调用者管理文件生命周期
	return err
}

// IsClosed 检查是否已关闭
func (mf *MmapFile) IsClosed() bool {
	mf.mutex.RLock()
	defer mf.mutex.RUnlock()
	return mf.data == nil
}
