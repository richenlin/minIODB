package storage

import (
	"context"
	"io"
	"time"

	"minIODB/pkg/pool"

	"github.com/minio/minio-go/v7"
)

// CacheStorage Redis缓存存储接口
type CacheStorage interface {
	// 基本键值操作
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, keys ...string) (int64, error)

	// 集合操作
	SAdd(ctx context.Context, key string, members ...interface{}) error
	SMembers(ctx context.Context, key string) ([]string, error)

	// 模式检测
	GetRedisMode() string
	IsRedisCluster() bool
	IsRedisSentinel() bool

	// 健康检查和统计
	HealthCheck(ctx context.Context) error
	GetStats() map[string]interface{}

	// 客户端获取
	GetClient() interface{}

	// 连接管理
	Close() error
}

// ObjectStorage 对象存储接口
type ObjectStorage interface {
	// 基本对象操作
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64) error
	GetObject(ctx context.Context, bucketName, objectName string) (*minio.Object, error)
	GetObjectBytes(ctx context.Context, bucketName, objectName string) ([]byte, error)
	DeleteObject(ctx context.Context, bucketName, objectName string) error

	// 对象列表操作
	ListObjects(ctx context.Context, bucketName string, prefix string) <-chan minio.ObjectInfo
	ListObjectsSimple(ctx context.Context, bucketName string, prefix string) ([]ObjectInfo, error)

	// 存储桶操作
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string) error

	// 健康检查和统计
	HealthCheck(ctx context.Context) error
	GetStats() map[string]interface{}

	// 连接管理
	Close() error
}

// StorageFactory 存储工厂接口
type StorageFactory interface {
	CreateCacheStorage() (CacheStorage, error)
	CreateObjectStorage() (ObjectStorage, error)
	CreateBackupObjectStorage() (ObjectStorage, error) // 备份存储
	GetPoolManager() *pool.PoolManager
	Close() error
}

// 保持向后兼容的统一存储接口
type Storage interface {
	CacheStorage
	ObjectStorage
	GetPoolManager() *pool.PoolManager
}
