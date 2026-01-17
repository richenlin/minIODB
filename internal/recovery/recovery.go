package recovery

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"minIODB/pkg/errors"
	"minIODB/internal/metrics"
)

// RecoveryHandler 恢复处理器
type RecoveryHandler struct {
	component string
	logger    *log.Logger
}

// NewRecoveryHandler 创建恢复处理器
func NewRecoveryHandler(component string, logger *log.Logger) *RecoveryHandler {
	return &RecoveryHandler{
		component: component,
		logger:    logger,
	}
}

// Recover 恢复处理
func (h *RecoveryHandler) Recover() {
	if r := recover(); r != nil {
		// 获取堆栈信息
		stack := make([]byte, 4096)
		length := runtime.Stack(stack, false)

		// 记录 panic 信息
		panicMsg := fmt.Sprintf("Panic recovered in %s: %v\nStack trace:\n%s",
			h.component, r, string(stack[:length]))

		if h.logger != nil {
			h.logger.Printf("PANIC: %s", panicMsg)
		}

		// 更新指标
		metrics.IncPanicRecovered(h.component)
	}
}

// SafeGo 安全地启动 goroutine
func (h *RecoveryHandler) SafeGo(fn func()) {
	go func() {
		defer h.Recover()
		fn()
	}()
}

// SafeGoWithContext 安全地启动 goroutine（带上下文）
func (h *RecoveryHandler) SafeGoWithContext(ctx context.Context, fn func(context.Context)) {
	go func() {
		defer h.Recover()
		fn(ctx)
	}()
}

// HealthChecker 健康检查器
type HealthChecker struct {
	checks   map[string]HealthCheck
	mutex    sync.RWMutex
	interval time.Duration
	timeout  time.Duration
	logger   *log.Logger
}

// HealthCheck 健康检查接口
type HealthCheck interface {
	Name() string
	Check(ctx context.Context) error
}

// HealthStatus 健康状态
type HealthStatus struct {
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(interval, timeout time.Duration, logger *log.Logger) *HealthChecker {
	return &HealthChecker{
		checks:   make(map[string]HealthCheck),
		interval: interval,
		timeout:  timeout,
		logger:   logger,
	}
}

// AddCheck 添加健康检查
func (hc *HealthChecker) AddCheck(check HealthCheck) {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()
	hc.checks[check.Name()] = check
}

// RemoveCheck 移除健康检查
func (hc *HealthChecker) RemoveCheck(name string) {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()
	delete(hc.checks, name)
}

// CheckAll 检查所有健康检查
func (hc *HealthChecker) CheckAll(ctx context.Context) []HealthStatus {
	hc.mutex.RLock()
	checks := make([]HealthCheck, 0, len(hc.checks))
	for _, check := range hc.checks {
		checks = append(checks, check)
	}
	hc.mutex.RUnlock()

	results := make([]HealthStatus, len(checks))
	var wg sync.WaitGroup

	for i, check := range checks {
		wg.Add(1)
		go func(idx int, c HealthCheck) {
			defer wg.Done()
			results[idx] = hc.runCheck(ctx, c)
		}(i, check)
	}

	wg.Wait()
	return results
}

// runCheck 运行单个健康检查
func (hc *HealthChecker) runCheck(ctx context.Context, check HealthCheck) HealthStatus {
	checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	start := time.Now()
	err := check.Check(checkCtx)
	duration := time.Since(start)

	status := HealthStatus{
		Name:      check.Name(),
		Timestamp: start,
		Duration:  duration,
	}

	if err != nil {
		status.Status = "unhealthy"
		status.Error = err.Error()
	} else {
		status.Status = "healthy"
	}

	return status
}

// Start 启动健康检查器
func (hc *HealthChecker) Start(ctx context.Context) {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			results := hc.CheckAll(ctx)
			hc.logResults(results)
		}
	}
}

// logResults 记录检查结果
func (hc *HealthChecker) logResults(results []HealthStatus) {
	if hc.logger == nil {
		return
	}

	for _, result := range results {
		if result.Status != "healthy" {
			hc.logger.Printf("Health check failed: %s - %s (took %v)",
				result.Name, result.Error, result.Duration)
		}
	}
}

// DatabaseHealthCheck 数据库健康检查
type DatabaseHealthCheck struct {
	name string
	ping func() error
}

// NewDatabaseHealthCheck 创建数据库健康检查
func NewDatabaseHealthCheck(name string, ping func() error) *DatabaseHealthCheck {
	return &DatabaseHealthCheck{
		name: name,
		ping: ping,
	}
}

// Name 返回检查名称
func (dhc *DatabaseHealthCheck) Name() string {
	return dhc.name
}

// Check 执行健康检查
func (dhc *DatabaseHealthCheck) Check(ctx context.Context) error {
	return dhc.ping()
}

// ServiceHealthCheck 服务健康检查
type ServiceHealthCheck struct {
	name    string
	checker func(context.Context) error
}

// NewServiceHealthCheck 创建服务健康检查
func NewServiceHealthCheck(name string, checker func(context.Context) error) *ServiceHealthCheck {
	return &ServiceHealthCheck{
		name:    name,
		checker: checker,
	}
}

// Name 返回检查名称
func (shc *ServiceHealthCheck) Name() string {
	return shc.name
}

// Check 执行健康检查
func (shc *ServiceHealthCheck) Check(ctx context.Context) error {
	return shc.checker(ctx)
}

// AutoRecovery 自动恢复器
type AutoRecovery struct {
	strategies map[string]RecoveryStrategy
	mutex      sync.RWMutex
	logger     *log.Logger
}

// RecoveryStrategy 恢复策略接口
type RecoveryStrategy interface {
	CanRecover(err error) bool
	Recover(ctx context.Context, err error) error
}

// NewAutoRecovery 创建自动恢复器
func NewAutoRecovery(logger *log.Logger) *AutoRecovery {
	return &AutoRecovery{
		strategies: make(map[string]RecoveryStrategy),
		logger:     logger,
	}
}

// AddStrategy 添加恢复策略
func (ar *AutoRecovery) AddStrategy(name string, strategy RecoveryStrategy) {
	ar.mutex.Lock()
	defer ar.mutex.Unlock()
	ar.strategies[name] = strategy
}

// TryRecover 尝试恢复
func (ar *AutoRecovery) TryRecover(ctx context.Context, err error) error {
	ar.mutex.RLock()
	defer ar.mutex.RUnlock()

	for name, strategy := range ar.strategies {
		if strategy.CanRecover(err) {
			if ar.logger != nil {
				ar.logger.Printf("Attempting recovery with strategy: %s", name)
			}

			if recoveryErr := strategy.Recover(ctx, err); recoveryErr == nil {
				if ar.logger != nil {
					ar.logger.Printf("Recovery successful with strategy: %s", name)
				}
				return nil
			}
		}
	}

	return err
}

// ConnectionRecoveryStrategy 连接恢复策略
type ConnectionRecoveryStrategy struct {
	reconnect  func() error
	maxRetries int
}

// NewConnectionRecoveryStrategy 创建连接恢复策略
func NewConnectionRecoveryStrategy(reconnect func() error, maxRetries int) *ConnectionRecoveryStrategy {
	return &ConnectionRecoveryStrategy{
		reconnect:  reconnect,
		maxRetries: maxRetries,
	}
}

// CanRecover 检查是否可以恢复
func (crs *ConnectionRecoveryStrategy) CanRecover(err error) bool {
	if olapErr, ok := err.(*errors.AppError); ok {
		return olapErr.Code == errors.ErrCodeConnectionFail
	}
	return false
}

// Recover 执行恢复
func (crs *ConnectionRecoveryStrategy) Recover(ctx context.Context, err error) error {
	for i := 0; i < crs.maxRetries; i++ {
		if reconnectErr := crs.reconnect(); reconnectErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * time.Second):
			// 继续重试
		}
	}

	return fmt.Errorf("failed to recover after %d attempts", crs.maxRetries)
}

// GracefulShutdown 优雅关闭
type GracefulShutdown struct {
	shutdownFuncs []func(context.Context) error
	timeout       time.Duration
	logger        *log.Logger
}

// NewGracefulShutdown 创建优雅关闭器
func NewGracefulShutdown(timeout time.Duration, logger *log.Logger) *GracefulShutdown {
	return &GracefulShutdown{
		timeout: timeout,
		logger:  logger,
	}
}

// AddShutdownFunc 添加关闭函数
func (gs *GracefulShutdown) AddShutdownFunc(fn func(context.Context) error) {
	gs.shutdownFuncs = append(gs.shutdownFuncs, fn)
}

// Shutdown 执行优雅关闭
func (gs *GracefulShutdown) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, gs.timeout)
	defer cancel()

	var wg sync.WaitGroup
	errChan := make(chan error, len(gs.shutdownFuncs))

	for _, fn := range gs.shutdownFuncs {
		wg.Add(1)
		go func(shutdownFunc func(context.Context) error) {
			defer wg.Done()
			if err := shutdownFunc(shutdownCtx); err != nil {
				errChan <- err
			}
		}(fn)
	}

	// 等待所有关闭函数完成
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(errChan)
		// 收集所有错误
		var errors []error
		for err := range errChan {
			errors = append(errors, err)
		}

		if len(errors) > 0 {
			return fmt.Errorf("shutdown errors: %v", errors)
		}
		return nil
	case <-shutdownCtx.Done():
		return fmt.Errorf("shutdown timeout exceeded")
	}
}
