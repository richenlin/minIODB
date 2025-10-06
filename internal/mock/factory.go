package mock

import (
	"time"
)

// MockFactory 是Mock对象的工厂
type MockFactory struct {
	minIOClient  *MockMinIOClient
	redisClient  *MockRedisClient
	duckDBClient *MockDuckDBClient
}

// NewMockFactory 创建新的Mock工厂
func NewMockFactory() *MockFactory {
	return &MockFactory{
		minIOClient:  NewMockMinIOClient(),
		redisClient:  NewMockRedisClient(),
		duckDBClient: NewMockDuckDBClient(),
	}
}

// GetMinIOClient 获取Mock MinIO客户端
func (f *MockFactory) GetMinIOClient() *MockMinIOClient {
	return f.minIOClient
}

// GetRedisClient 获取Mock Redis客户端
func (f *MockFactory) GetRedisClient() *MockRedisClient {
	return f.redisClient
}

// GetDuckDBClient 获取Mock DuckDB客户端
func (f *MockFactory) GetDuckDBClient() *MockDuckDBClient {
	return f.duckDBClient
}

// Reset 重置所有Mock对象状态
func (f *MockFactory) Reset() {
	f.minIOClient.Reset()
	f.redisClient.Reset()
	f.duckDBClient.Reset()
}

// MockResponse 表示Mock操作的响应
type MockResponse struct {
	Success bool
	Data    interface{}
	Error   error
}

// MockConfig 配置Mock对象的行为
type MockConfig struct {
	// MinIO配置
	MinIO MockConfigMinIO

	// Redis配置
	Redis MockConfigRedis

	// DuckDB配置
	DuckDB MockConfigDuckDB
}

// MockConfigMinIO MinIO配置
type MockConfigMinIO struct {
	ShouldFailUpload   bool
	ShouldFailDownload bool
	ShouldFailBucket   bool
	UploadDelay        time.Duration
	DownloadDelay      time.Duration
}

// MockConfigRedis Redis配置
type MockConfigRedis struct {
	ShouldFailConnect bool
	ShouldFailGet     bool
	ShouldFailSet     bool
	ShouldFailDelete  bool
	GetDelay          time.Duration
	SetDelay          time.Duration
}

// MockConfigDuckDB DuckDB配置
type MockConfigDuckDB struct {
	ShouldFailConnect bool
	ShouldFailQuery   bool
	ShouldFailExec    bool
	QueryDelay        time.Duration
	ExecDelay         time.Duration
}
