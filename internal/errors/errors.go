package errors

import (
	"fmt"
	"net/http"
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
	ErrCodeDataNotFound     ErrorCode = "DATA_NOT_FOUND"
	ErrCodeDataInvalid      ErrorCode = "DATA_INVALID"
	ErrCodeDataTooLarge     ErrorCode = "DATA_TOO_LARGE"
	ErrCodeDataCorrupted    ErrorCode = "DATA_CORRUPTED"
	ErrCodeDataExists       ErrorCode = "DATA_EXISTS"

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