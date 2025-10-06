package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"minIODB/internal/config"
	"minIODB/internal/logger"
	"minIODB/internal/mock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TestSuite 提供基础的测试套件
type TestSuite struct {
	suite.Suite
	Ctx        context.Context
	Cancel     context.CancelFunc
	MockHelper *mock.TestHelper
	Config     *config.Config
	Logger     logger.Logger
	SetupDone  bool
}

// SetupSuite 设置测试套件
func (s *TestSuite) SetupSuite() {
	s.Ctx, s.Cancel = context.WithCancel(context.Background())
	s.MockHelper = mock.NewTestHelper(s.T())
	s.MockHelper.SetupSuccess()

	// 加载测试配置
	s.setupTestConfig()

	// 初始化日志
	s.setupLogger()

	s.SetupDone = true
}

// TearDownSuite 清理测试套件
func (s *TestSuite) TearDownSuite() {
	if s.MockHelper != nil {
		s.MockHelper.Cleanup()
	}

	if s.Cancel != nil {
		s.Cancel()
	}

	s.SetupDone = false
}

// SetupTest 设置每个测试
func (s *TestSuite) SetupTest() {
	if !s.SetupDone {
		s.SetupSuite()
	}

	// 重置Mock状态
	if s.MockHelper != nil {
		s.MockHelper.GetFactory().Reset()
		s.MockHelper.SetupSuccess()
	}
}

// TearDownTest 清理每个测试
func (s *TestSuite) TearDownTest() {
	// 清理测试特定数据
	if s.MockHelper != nil {
		s.MockHelper.GetFactory().Reset()
	}
}

// setupTestConfig 设置测试配置
func (s *TestSuite) setupTestConfig() {
	// 获取当前文件的绝对路径
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)

	// 构建测试配置文件路径
	configPath := filepath.Join(currentDir, "../../config.test.yaml")

	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 如果配置文件不存在，使用默认配置
		s.Config = config.GetDefaultConfig()
		s.Config.Test.Enabled = true
		s.Config.Test.UseMocks = true
	} else {
		// 加载测试配置
		var err error
		s.Config, err = config.LoadConfig(configPath)
		if err != nil {
			// 如果加载失败，使用默认配置
			s.Config = config.GetDefaultConfig()
			s.Config.Test.Enabled = true
			s.Config.Test.UseMocks = true
		}
	}

	// 确保测试模式启用
	s.Config.Test.Enabled = true
	s.Config.Test.UseMocks = true
}

// setupLogger 设置日志
func (s *TestSuite) setupLogger() {
	logConfig := logger.LogConfig{
		Level:      s.Config.Logging.Level,
		Format:     s.Config.Logging.Format,
		Output:     s.Config.Logging.Output,
		Filename:   "",
		MaxSize:    100,
		MaxBackups: 3,
		MaxAge:     1,
		Compress:   true,
	}

	s.Logger = logger.InitLogger(logConfig)
}

// RequireSuccess 要求操作成功
func (s *TestSuite) RequireSuccess(err error, msgAndArgs ...interface{}) {
	s.Require().NoError(err, msgAndArgs...)
}

// AssertSuccess 断言操作成功
func (s *TestSuite) AssertSuccess(err error, msgAndArgs ...interface{}) {
	s.Assert().NoError(err, msgAndArgs...)
}

// AssertError 断言操作失败
func (s *TestSuite) AssertError(err error, expectedError string, msgAndArgs ...interface{}) {
	s.Assert().Error(err, msgAndArgs...)
	s.Assert().Contains(err.Error(), expectedError, msgAndArgs...)
}

// RunWithTimeout 在超时内运行函数
func (s *TestSuite) RunWithTimeout(timeout time.Duration, fn func() error) error {
	done := make(chan error, 1)

	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("operation timed out after %v", timeout)
	}
}

// SetupMockFailure 设置Mock失败场景
func (s *TestSuite) SetupMockFailure(service string, failureType string) {
	configs := mock.GetTestConfigs()

	switch service {
	case "minio":
		switch failureType {
		case "upload_fail":
			s.MockHelper.SetupMinIOUploadFail()
		case "download_fail":
			s.MockHelper.GetFactory().GetMinIOClient().SetConfig(configs["minio_download_fail"])
		case "bucket_fail":
			s.MockHelper.GetFactory().GetMinIOClient().SetConfig(configs["minio_bucket_fail"])
		}
	case "redis":
		switch failureType {
		case "connection_fail":
			s.MockHelper.SetupRedisConnectionFail()
		case "operations_fail":
			s.MockHelper.GetFactory().GetRedisClient().SetConfig(configs["redis_operations_fail"])
		}
	case "duckdb":
		switch failureType {
		case "connection_fail":
			s.MockHelper.SetupDuckDBConnectionFail()
		case "operations_fail":
			s.MockHelper.GetFactory().GetDuckDBClient().SetConfig(configs["duckdb_operations_fail"])
		}
	}
}

// NewTestSuite 创建新的测试套件
func NewTestSuite() *TestSuite {
	return &TestSuite{}
}

// RunSuite 运行测试套件
func RunSuite(t *testing.T, suiteInterface suite.TestingSuite) {
	suite.Run(t, suiteInterface)
}

// IntegrationTestSuite 集成测试套件
type IntegrationTestSuite struct {
	TestSuite
}

// SetupSuite 设置集成测试套件
func (s *IntegrationTestSuite) SetupSuite() {
	s.TestSuite.SetupSuite()

	// 集成测试特定设置
	if s.Config != nil {
		s.Config.Test.UseMocks = false // 集成测试可能需要真实服务
	}
}

// PerformanceTestSuite 性能测试套件
type PerformanceTestSuite struct {
	TestSuite
	StartTime time.Time
}

// SetupTest 设置性能测试
func (s *PerformanceTestSuite) SetupTest() {
	s.TestSuite.SetupTest()
	s.StartTime = time.Now()
}

// TearDownTest 清理性能测试
func (s *PerformanceTestSuite) TearDownTest() {
	duration := time.Since(s.StartTime)
	s.T().Logf("Test took %v", duration)

	s.TestSuite.TearDownTest()
}

// AssertDuration 断言测试执行时间在预期范围内
func (s *PerformanceTestSuite) AssertDuration(maxDuration time.Duration, msgAndArgs ...interface{}) {
	duration := time.Since(s.StartTime)
	s.Assert().LessOrEqual(duration, maxDuration, msgAndArgs...)
}