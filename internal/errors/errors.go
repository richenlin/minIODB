package errors

import (
	"fmt"
	"net/http"
	"time"
)

// ErrorCode 错误码类型
type ErrorCode string

const (
	// 通用错误码
	ErrCodeInternal         ErrorCode = "INTERNAL_ERROR"
	ErrCodeInvalidRequest   ErrorCode = "INVALID_REQUEST"
	ErrCodeInvalidParameter ErrorCode = "INVALID_PARAMETER"
	ErrCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrCodeConflict         ErrorCode = "CONFLICT"
	ErrCodeTooManyRequests  ErrorCode = "TOO_MANY_REQUESTS"

	// 数据相关错误码
	ErrCodeDataNotFound  ErrorCode = "DATA_NOT_FOUND"
	ErrCodeDataInvalid   ErrorCode = "DATA_INVALID"
	ErrCodeDataTooLarge  ErrorCode = "DATA_TOO_LARGE"
	ErrCodeDataCorrupted ErrorCode = "DATA_CORRUPTED"
	ErrCodeDataExists    ErrorCode = "DATA_EXISTS"

	// 查询相关错误码
	ErrCodeQueryInvalid     ErrorCode = "QUERY_INVALID"
	ErrCodeQueryTimeout     ErrorCode = "QUERY_TIMEOUT"
	ErrCodeQueryTooComplex  ErrorCode = "QUERY_TOO_COMPLEX"
	ErrCodeQuerySyntaxError ErrorCode = "QUERY_SYNTAX_ERROR"

	// 存储相关错误码
	ErrCodeStorageUnavailable ErrorCode = "STORAGE_UNAVAILABLE"
	ErrCodeStorageFull        ErrorCode = "STORAGE_FULL"
	ErrCodeStorageTimeout     ErrorCode = "STORAGE_TIMEOUT"
	ErrCodeStorageCorrupted   ErrorCode = "STORAGE_CORRUPTED"

	// 网络相关错误码
	ErrCodeNetworkTimeout     ErrorCode = "NETWORK_TIMEOUT"
	ErrCodeNetworkUnavailable ErrorCode = "NETWORK_UNAVAILABLE"
	ErrCodeNetworkError       ErrorCode = "NETWORK_ERROR"

	// 服务相关错误码
	ErrCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeServiceTimeout     ErrorCode = "SERVICE_TIMEOUT"
	ErrCodeServiceOverloaded  ErrorCode = "SERVICE_OVERLOADED"

	// 配置相关错误码
	ErrCodeConfigInvalid ErrorCode = "CONFIG_INVALID"
	ErrCodeConfigMissing ErrorCode = "CONFIG_MISSING"

	// 认证相关错误码
	ErrCodeAuthInvalid     ErrorCode = "AUTH_INVALID"
	ErrCodeAuthExpired     ErrorCode = "AUTH_EXPIRED"
	ErrCodeAuthMissing     ErrorCode = "AUTH_MISSING"
	ErrCodeAuthInsufficent ErrorCode = "AUTH_INSUFFICIENT"
)

// ErrorType 错误类型
type ErrorType string

const (
	// 系统错误类型
	ErrTypeSystem     ErrorType = "system"
	ErrTypeNetwork    ErrorType = "network"
	ErrTypeStorage    ErrorType = "storage"
	ErrTypeValidation ErrorType = "validation"
	ErrTypeBusiness   ErrorType = "business"
	ErrTypeTimeout    ErrorType = "timeout"
	ErrTypeResource   ErrorType = "resource"
)

// ErrorSeverity 错误严重程度
type ErrorSeverity string

const (
	SeverityLow      ErrorSeverity = "low"
	SeverityMedium   ErrorSeverity = "medium"
	SeverityHigh     ErrorSeverity = "high"
	SeverityCritical ErrorSeverity = "critical"
)

// AppError 应用错误结构
type AppError struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	Details    string    `json:"details,omitempty"`
	HTTPStatus int       `json:"-"`
	Cause      error     `json:"-"`
}

// Error 实现error接口
func (e *AppError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 返回原始错误
func (e *AppError) Unwrap() error {
	return e.Cause
}

// New 创建新的应用错误
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: getHTTPStatus(code),
	}
}

// Newf 创建新的应用错误（格式化消息）
func Newf(code ErrorCode, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		HTTPStatus: getHTTPStatus(code),
	}
}

// Wrap 包装现有错误
func Wrap(err error, code ErrorCode, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: getHTTPStatus(code),
		Cause:      err,
	}
}

// Wrapf 包装现有错误（格式化消息）
func Wrapf(err error, code ErrorCode, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		HTTPStatus: getHTTPStatus(code),
		Cause:      err,
	}
}

// WithDetails 添加详细信息
func (e *AppError) WithDetails(details string) *AppError {
	e.Details = details
	return e
}

// WithDetailsf 添加详细信息（格式化）
func (e *AppError) WithDetailsf(format string, args ...interface{}) *AppError {
	e.Details = fmt.Sprintf(format, args...)
	return e
}

// WithHTTPStatus 设置HTTP状态码
func (e *AppError) WithHTTPStatus(status int) *AppError {
	e.HTTPStatus = status
	return e
}

// IsAppError 检查是否为应用错误
func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

// GetAppError 获取应用错误
func GetAppError(err error) *AppError {
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return nil
}

// getHTTPStatus 根据错误码获取HTTP状态码
func getHTTPStatus(code ErrorCode) int {
	switch code {
	case ErrCodeInvalidRequest, ErrCodeInvalidParameter, ErrCodeDataInvalid, ErrCodeQueryInvalid, ErrCodeQuerySyntaxError, ErrCodeConfigInvalid:
		return http.StatusBadRequest
	case ErrCodeUnauthorized, ErrCodeAuthInvalid, ErrCodeAuthExpired, ErrCodeAuthMissing:
		return http.StatusUnauthorized
	case ErrCodeForbidden, ErrCodeAuthInsufficent:
		return http.StatusForbidden
	case ErrCodeNotFound, ErrCodeDataNotFound, ErrCodeConfigMissing:
		return http.StatusNotFound
	case ErrCodeConflict, ErrCodeDataExists:
		return http.StatusConflict
	case ErrCodeDataTooLarge:
		return http.StatusRequestEntityTooLarge
	case ErrCodeTooManyRequests:
		return http.StatusTooManyRequests
	case ErrCodeServiceUnavailable, ErrCodeStorageUnavailable, ErrCodeNetworkUnavailable:
		return http.StatusServiceUnavailable
	case ErrCodeQueryTimeout, ErrCodeStorageTimeout, ErrCodeNetworkTimeout, ErrCodeServiceTimeout:
		return http.StatusRequestTimeout
	case ErrCodeServiceOverloaded, ErrCodeStorageFull:
		return http.StatusInsufficientStorage
	default:
		return http.StatusInternalServerError
	}
}

// 预定义的常用错误
var (
	ErrInternalServer = New(ErrCodeInternal, "Internal server error")
	ErrInvalidRequest = New(ErrCodeInvalidRequest, "Invalid request")
	ErrNotFound       = New(ErrCodeNotFound, "Resource not found")
	ErrUnauthorized   = New(ErrCodeUnauthorized, "Unauthorized")
	ErrForbidden      = New(ErrCodeForbidden, "Forbidden")

	ErrDataNotFound  = New(ErrCodeDataNotFound, "Data not found")
	ErrDataInvalid   = New(ErrCodeDataInvalid, "Invalid data")
	ErrDataTooLarge  = New(ErrCodeDataTooLarge, "Data too large")
	ErrDataCorrupted = New(ErrCodeDataCorrupted, "Data corrupted")

	ErrQueryInvalid     = New(ErrCodeQueryInvalid, "Invalid query")
	ErrQueryTimeout     = New(ErrCodeQueryTimeout, "Query timeout")
	ErrQuerySyntaxError = New(ErrCodeQuerySyntaxError, "Query syntax error")

	ErrStorageUnavailable = New(ErrCodeStorageUnavailable, "Storage unavailable")
	ErrStorageTimeout     = New(ErrCodeStorageTimeout, "Storage timeout")

	ErrServiceUnavailable = New(ErrCodeServiceUnavailable, "Service unavailable")
	ErrServiceTimeout     = New(ErrCodeServiceTimeout, "Service timeout")
)

// OlapError 自定义错误结构
type OlapError struct {
	Type      ErrorType     `json:"type"`
	Severity  ErrorSeverity `json:"severity"`
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	Details   string        `json:"details,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Component string        `json:"component"`
	Operation string        `json:"operation"`
	Retryable bool          `json:"retryable"`
	Cause     error         `json:"-"`
}

// Error 实现 error 接口
func (e *OlapError) Error() string {
	return fmt.Sprintf("[%s][%s] %s: %s", e.Type, e.Severity, e.Code, e.Message)
}

// Unwrap 支持错误包装
func (e *OlapError) Unwrap() error {
	return e.Cause
}

// IsRetryable 检查错误是否可重试
func (e *OlapError) IsRetryable() bool {
	return e.Retryable
}

// NewOlapError 创建新的 OLAP 错误
func NewOlapError(errType ErrorType, severity ErrorSeverity, code, message, component, operation string) *OlapError {
	return &OlapError{
		Type:      errType,
		Severity:  severity,
		Code:      code,
		Message:   message,
		Timestamp: time.Now(),
		Component: component,
		Operation: operation,
		Retryable: false,
	}
}

// WithCause 添加根本原因
func (e *OlapError) WithCause(cause error) *OlapError {
	e.Cause = cause
	return e
}

// WithDetails 添加详细信息
func (e *OlapError) WithDetails(details string) *OlapError {
	e.Details = details
	return e
}

// WithRetryable 设置是否可重试
func (e *OlapError) WithRetryable(retryable bool) *OlapError {
	e.Retryable = retryable
	return e
}

// 预定义的错误构造函数

// NewSystemError 创建系统错误
func NewSystemError(code, message, component, operation string) *OlapError {
	return NewOlapError(ErrTypeSystem, SeverityHigh, code, message, component, operation)
}

// NewNetworkError 创建网络错误
func NewNetworkError(code, message, component, operation string) *OlapError {
	return NewOlapError(ErrTypeNetwork, SeverityMedium, code, message, component, operation).WithRetryable(true)
}

// NewStorageError 创建存储错误
func NewStorageError(code, message, component, operation string) *OlapError {
	return NewOlapError(ErrTypeStorage, SeverityHigh, code, message, component, operation)
}

// NewValidationError 创建验证错误
func NewValidationError(code, message, component, operation string) *OlapError {
	return NewOlapError(ErrTypeValidation, SeverityLow, code, message, component, operation)
}

// NewTimeoutError 创建超时错误
func NewTimeoutError(code, message, component, operation string) *OlapError {
	return NewOlapError(ErrTypeTimeout, SeverityMedium, code, message, component, operation).WithRetryable(true)
}

// NewResourceError 创建资源错误
func NewResourceError(code, message, component, operation string) *OlapError {
	return NewOlapError(ErrTypeResource, SeverityCritical, code, message, component, operation)
}

// ErrorHandler 错误处理器
type ErrorHandler struct {
	component string
}

// NewErrorHandler 创建错误处理器
func NewErrorHandler(component string) *ErrorHandler {
	return &ErrorHandler{
		component: component,
	}
}

// Handle 处理错误
func (h *ErrorHandler) Handle(err error, operation string) *OlapError {
	if err == nil {
		return nil
	}

	// 如果已经是 OlapError，直接返回
	if olapErr, ok := err.(*OlapError); ok {
		return olapErr
	}

	// 根据错误类型创建相应的 OlapError
	return h.classifyError(err, operation)
}

// classifyError 分类错误
func (h *ErrorHandler) classifyError(err error, operation string) *OlapError {
	errMsg := err.Error()

	// 简单的错误分类逻辑
	switch {
	case containsAny(errMsg, []string{"timeout", "deadline", "context deadline exceeded"}):
		return NewTimeoutError("TIMEOUT", errMsg, h.component, operation).WithCause(err)
	case containsAny(errMsg, []string{"connection", "network", "dial", "EOF"}):
		return NewNetworkError("NETWORK", errMsg, h.component, operation).WithCause(err)
	case containsAny(errMsg, []string{"storage", "minio", "redis", "database"}):
		return NewStorageError("STORAGE", errMsg, h.component, operation).WithCause(err)
	case containsAny(errMsg, []string{"validation", "invalid", "required", "format"}):
		return NewValidationError("VALIDATION", errMsg, h.component, operation).WithCause(err)
	case containsAny(errMsg, []string{"memory", "resource", "limit", "quota"}):
		return NewResourceError("RESOURCE", errMsg, h.component, operation).WithCause(err)
	default:
		return NewSystemError("SYSTEM", errMsg, h.component, operation).WithCause(err)
	}
}

// containsAny 检查字符串是否包含任意一个子字符串
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
