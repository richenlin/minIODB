package constants

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPortConstants(t *testing.T) {
	assert.Equal(t, 1024, MinPort)
	assert.Equal(t, 65535, MaxPort)
	assert.Equal(t, ":50051", DefaultGRPCPort)
	assert.Equal(t, ":8080", DefaultRESTPort)
}

func TestBucketConstants(t *testing.T) {
	assert.Equal(t, 3, MinBucketNameLength)
	assert.Equal(t, 63, MaxBucketNameLength)
	assert.Equal(t, "olap-data", DefaultBucketName)
}

func TestTableManagementConstants(t *testing.T) {
	assert.Equal(t, 1000, DefaultMaxTables)
	assert.Equal(t, 10000, MaxTablesLimit)
	assert.Equal(t, 1000, DefaultBufferSize)
	assert.Equal(t, 100000, MaxBufferSize)
	assert.Equal(t, 365, DefaultRetentionDays)
	assert.Equal(t, 3650, MaxRetentionDays)
}

func TestSystemResourceConstants(t *testing.T) {
	assert.Equal(t, 256, MinMemoryMB)
	assert.Equal(t, 2048, DefaultMemoryMB)
	assert.Equal(t, 100, MinGoroutines)
	assert.Equal(t, 1000, DefaultGoroutines)
}

func TestLogConstants(t *testing.T) {
	assert.Equal(t, 100, DefaultLogMaxSize)
	assert.Equal(t, 1000, MaxLogMaxSize)
	assert.Equal(t, 5, DefaultLogMaxBackups)
	assert.Equal(t, 30, DefaultLogMaxAge)
}

func TestSQLQueryConstants(t *testing.T) {
	assert.Equal(t, 10000, MaxSQLQueryLength)
	assert.Equal(t, 100, DefaultQueryLimit)
	assert.Equal(t, 10000, MaxQueryLimit)
}

func TestRateLimitConstants(t *testing.T) {
	assert.Equal(t, 60, DefaultRateLimitPerMinute)
	assert.Equal(t, 200, DefaultHealthTierRPS)
	assert.Equal(t, 100, DefaultQueryTierRPS)
	assert.Equal(t, 80, DefaultWriteTierRPS)
	assert.Equal(t, 50, DefaultStandardTierRPS)
	assert.Equal(t, 20, DefaultStrictTierRPS)
}

func TestCacheConstants(t *testing.T) {
	assert.Equal(t, int64(209715200), int64(DefaultQueryCacheMaxSize))
	assert.Equal(t, int64(1073741824), int64(DefaultFileCacheMaxSize))
	assert.Equal(t, "qcache:", DefaultQueryCacheKeyPrefix)
	assert.Equal(t, "fcache:", DefaultFileCacheKeyPrefix)
}

func TestBackupConstants(t *testing.T) {
	assert.Equal(t, 3, MaxBackupRetries)
	assert.Equal(t, "olap-metadata", DefaultMetadataBackupBucket)
}

func TestPerformanceConstants(t *testing.T) {
	assert.Equal(t, int(30*time.Second), int(DefaultRequestTimeout))
	assert.Equal(t, int(5*time.Second), int(DefaultConnectionTimeout))
}

func TestStreamConstants(t *testing.T) {
	assert.Equal(t, 1000, DefaultStreamBatchSize)
	assert.Equal(t, 10000, MaxStreamBatchSize)
	assert.Equal(t, 10, MinStreamBatchSize)
	assert.Equal(t, 1000, DefaultStreamBackpressureInterval)
}
