// Package test 提供功能集成测试的基础测试框架
package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// BaseTestSuite 功能测试基础套件
// 所有功能集成测试都应该嵌入此套件
// 提供通用的测试环境、配置初始化和清理功能
type BaseTestSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	startTime  time.Time
	suiteName  string
}

// SetupSuite 在测试套件开始时调用
func (s *BaseTestSuite) SetupSuite() {
	s.T().Logf("=== Starting test suite: %s ===", s.suiteName)
	s.ctx, s.cancel = GetTestTimeoutContext(30 * time.Second)
	s.startTime = time.Now()

	// 初始化测试logger
	InitTestLogger(s.T())
}

// TearDownSuite 在测试套件结束时调用
func (s *BaseTestSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel()
	}
	testDuration := time.Since(s.startTime)
	s.T().Logf("=== Finished test suite: %s (took %v) ===", s.suiteName, testDuration)
}

// SetupTest 在每个测试函数前调用
func (s *BaseTestSuite) SetupTest() {
	s.T().Logf("Running test: %s", s.T().Name())
}

// TearDownTest 在每个测试函数后调用
func (s *BaseTestSuite) TearDownTest() {
	s.T().Logf("Completed test: %s", s.T().Name())
}

// AssertValidDuration 验证持续时间合理性
func (s *BaseTestSuite) AssertValidDuration(duration time.Duration, maxDuration time.Duration) {
	s.LessOrEqual(duration, maxDuration, "操作超时：期望在%v内完成，实际耗时%v", maxDuration, duration)
}

// AssertContextValid 验证context有效
func (s *BaseTestSuite) AssertContextValid(ctx context.Context) {
	s.NotNil(ctx, "context不應該為nil")
	s.NoError(ctx.Err(), "context不應該已經被取消或超時")
}

// GetSuiteName 获取当前套件名称
func (s *BaseTestSuite) GetSuiteName() string {
	return s.suiteName
}

// SetSuiteName 设置套件名称
func (s *BaseTestSuite) SetSuiteName(name string) {
	s.suiteName = name
}

// NewBaseTestSuite 创建基础测试套件实例
func NewBaseTestSuite(suiteName string) *BaseTestSuite {
	return &BaseTestSuite{
		suiteName: suiteName,
	}
}