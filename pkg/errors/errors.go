package errors

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrorCode 定义错误代码
type ErrorCode string

// ErrorType 错误类型
type ErrorType string

const (
	// 通用错误
	ErrCodeInternal     ErrorCode = "INTERNAL_ERROR"
	ErrCodeInvalidInput ErrorCode = "INVALID_INPUT"
	ErrCodeNotFound     ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden    ErrorCode = "FORBIDDEN"

	// 业务错误
	ErrCodeTableNotFound    ErrorCode = "TABLE_NOT_FOUND"
	ErrCodeTableExists      ErrorCode = "TABLE_EXISTS"
	ErrCodeInvalidTableName ErrorCode = "INVALID_TABLE_NAME"
	ErrCodeDataNotFound     ErrorCode = "DATA_NOT_FOUND"
	ErrCodeInvalidQuery     ErrorCode = "INVALID_QUERY"

	// 存储错误
	ErrCodeStorageFailure ErrorCode = "STORAGE_FAILURE"
	ErrCodeCacheFailure   ErrorCode = "CACHE_FAILURE"
	ErrCodeConnectionFail ErrorCode = "CONNECTION_FAILURE"

	// 安全错误
	ErrCodeTokenInvalid ErrorCode = "TOKEN_INVALID"
	ErrCodeTokenExpired ErrorCode = "TOKEN_EXPIRED"
	ErrCodeRateLimited  ErrorCode = "RATE_LIMITED"
	ErrCodeSQLInjection ErrorCode = "SQL_INJECTION_DETECTED"

	// 配置错误
	ErrCodeInvalidConfig  ErrorCode = "INVALID_CONFIG"
	ErrCodeConfigNotFound ErrorCode = "CONFIG_NOT_FOUND"

	// 验证错误
	ErrCodeValidationError ErrorCode = "VALIDATION_ERROR"

	// 备份错误
	ErrCodeBackupFailed  ErrorCode = "BACKUP_FAILED"
	ErrCodeBackupInvalid ErrorCode = "BACKUP_INVALID"

	// 超时错误
	ErrCodeTimeout ErrorCode = "TIMEOUT"

	// 资源限制错误
	ErrCodeResourceExhausted ErrorCode = "RESOURCE_EXHAUSTED"
)

const (
	// 错误类型
	ErrorTypeNetwork ErrorType = "network"
	ErrorTypeStorage ErrorType = "storage"
	ErrorTypeOLAP    ErrorType = "olap"
)

// AppError 应用错误结构
type AppError struct {
	Code       ErrorCode  `json:"code"`
	Message    string     `json:"message"`
	Details    string     `json:"details,omitempty"`
	Cause      error      `json:"-"`
	HTTPStatus int        `json:"-"`
	GRPCCode   codes.Code `json:"-"`
}

// Error 实现error接口
func (e *AppError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 支持错误链
func (e *AppError) Unwrap() error {
	return e.Cause
}

// New 创建新的应用错误
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: getDefaultHTTPStatus(code),
		GRPCCode:   getDefaultGRPCCode(code),
	}
}

// Newf 创建格式化的应用错误
func Newf(code ErrorCode, format string, args ...interface{}) *AppError {
	return New(code, fmt.Sprintf(format, args...))
}

// Wrap 包装现有错误
func Wrap(err error, code ErrorCode, message string) *AppError {
	if err == nil {
		return nil
	}

	// 如果已经是AppError，保持原有的code
	if appErr, ok := err.(*AppError); ok {
		return &AppError{
			Code:       appErr.Code,
			Message:    message,
			Details:    appErr.Message,
			Cause:      appErr.Cause,
			HTTPStatus: appErr.HTTPStatus,
			GRPCCode:   appErr.GRPCCode,
		}
	}

	return &AppError{
		Code:       code,
		Message:    message,
		Details:    err.Error(),
		Cause:      err,
		HTTPStatus: getDefaultHTTPStatus(code),
		GRPCCode:   getDefaultGRPCCode(code),
	}
}

// Wrapf 包装现有错误（格式化消息）
func Wrapf(err error, code ErrorCode, format string, args ...interface{}) *AppError {
	return Wrap(err, code, fmt.Sprintf(format, args...))
}

// WithDetails 添加详细信息
func (e *AppError) WithDetails(details string) *AppError {
	e.Details = details
	return e
}

// WithHTTPStatus 设置HTTP状态码
func (e *AppError) WithHTTPStatus(status int) *AppError {
	e.HTTPStatus = status
	return e
}

// WithGRPCCode 设置gRPC状态码
func (e *AppError) WithGRPCCode(code codes.Code) *AppError {
	e.GRPCCode = code
	return e
}

// ToHTTPResponse 转换为HTTP响应格式
func (e *AppError) ToHTTPResponse() (int, map[string]interface{}) {
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    e.Code,
			"message": e.Message,
		},
		"success": false,
	}

	if e.Details != "" && !isProduction() {
		response["error"].(map[string]interface{})["details"] = e.Details
	}

	return e.HTTPStatus, response
}

// ToGRPCError 转换为gRPC错误
func (e *AppError) ToGRPCError() error {
	return status.Error(e.GRPCCode, e.Message)
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

// getDefaultHTTPStatus 获取默认HTTP状态码
func getDefaultHTTPStatus(code ErrorCode) int {
	switch code {
	case ErrCodeInvalidInput, ErrCodeInvalidTableName, ErrCodeInvalidQuery, ErrCodeSQLInjection:
		return http.StatusBadRequest
	case ErrCodeUnauthorized, ErrCodeTokenInvalid, ErrCodeTokenExpired:
		return http.StatusUnauthorized
	case ErrCodeForbidden:
		return http.StatusForbidden
	case ErrCodeNotFound, ErrCodeTableNotFound, ErrCodeDataNotFound:
		return http.StatusNotFound
	case ErrCodeTableExists:
		return http.StatusConflict
	case ErrCodeRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// getDefaultGRPCCode 获取默认gRPC状态码
func getDefaultGRPCCode(code ErrorCode) codes.Code {
	switch code {
	case ErrCodeInvalidInput, ErrCodeInvalidTableName, ErrCodeInvalidQuery, ErrCodeSQLInjection:
		return codes.InvalidArgument
	case ErrCodeUnauthorized, ErrCodeTokenInvalid, ErrCodeTokenExpired:
		return codes.Unauthenticated
	case ErrCodeForbidden:
		return codes.PermissionDenied
	case ErrCodeNotFound, ErrCodeTableNotFound, ErrCodeDataNotFound:
		return codes.NotFound
	case ErrCodeTableExists:
		return codes.AlreadyExists
	case ErrCodeRateLimited:
		return codes.ResourceExhausted
	case ErrCodeConnectionFail:
		return codes.Unavailable
	case ErrCodeTimeout:
		return codes.DeadlineExceeded
	case ErrCodeStorageFailure, ErrCodeCacheFailure:
		return codes.Unavailable
	case ErrCodeBackupFailed:
		return codes.Internal
	default:
		return codes.Internal
	}
}

// isProduction 检查是否为生产环境
func isProduction() bool {
	// 简单实现，实际应该从配置中读取
	return strings.ToLower(strings.TrimSpace(fmt.Sprintf("%s", "development"))) == "production"
}

// 梢定义常用错误
var (
	ErrInternalServer = New(ErrCodeInternal, "Internal server error")
	ErrInvalidInput   = New(ErrCodeInvalidInput, "Invalid input parameters")
	ErrNotFound       = New(ErrCodeNotFound, "Resource not found")
	ErrUnauthorized   = New(ErrCodeUnauthorized, "Unauthorized access")
	ErrForbidden      = New(ErrCodeForbidden, "Access forbidden")

	ErrTableNotFound    = New(ErrCodeTableNotFound, "Table not found")
	ErrTableExists      = New(ErrCodeTableExists, "Table already exists")
	ErrInvalidTableName = New(ErrCodeInvalidTableName, "Invalid table name")
	ErrDataNotFound     = New(ErrCodeDataNotFound, "Data not found")
	ErrInvalidQuery     = New(ErrCodeInvalidQuery, "Invalid query")

	ErrStorageFailure = New(ErrCodeStorageFailure, "Storage operation failed")
	ErrCacheFailure   = New(ErrCodeCacheFailure, "Cache operation failed")
	ErrConnectionFail = New(ErrCodeConnectionFail, "Connection failed")

	ErrTokenInvalid = New(ErrCodeTokenInvalid, "Invalid token")
	ErrTokenExpired = New(ErrCodeTokenExpired, "Token expired")
	ErrRateLimited  = New(ErrCodeRateLimited, "Rate limit exceeded")
	ErrSQLInjection = New(ErrCodeSQLInjection, "SQL injection detected")

	ErrInvalidConfig  = New(ErrCodeInvalidConfig, "Invalid configuration")
	ErrConfigNotFound = New(ErrCodeConfigNotFound, "Configuration not found")

	ErrValidation = New(ErrCodeValidationError, "Validation failed")

	ErrBackupFailed  = New(ErrCodeBackupFailed, "Backup operation failed")
	ErrBackupInvalid = New(ErrCodeBackupInvalid, "Invalid backup parameters")

	ErrTimeout = New(ErrCodeTimeout, "Operation timeout")

	ErrResourceExhausted = New(ErrCodeResourceExhausted, "Resource limit exceeded")

	// 带详细信息的预定义错误
	ErrTableNotFoundWithName = func(tableName string) *AppError {
		return Newf(ErrCodeTableNotFound, "Table %s not found", tableName)
	}

	ErrTableExistsWithName = func(tableName string) *AppError {
		return Newf(ErrCodeTableExists, "Table %s already exists", tableName)
	}

	ErrInvalidTableNameWithName = func(tableName string, reason string) *AppError {
		return Newf(ErrCodeInvalidTableName, "Invalid table name %s: %s", tableName, reason)
	}

	ErrDataNotFoundWithID = func(id string) *AppError {
		return Newf(ErrCodeDataNotFound, "Data with id %s not found", id)
	}

	ErrQueryExecutionFailed = func(query string, err error) *AppError {
		return Wrapf(err, ErrCodeInternal, "Query execution failed: %s", query)
	}

	ErrStorageOperationFailed = func(operation string, err error) *AppError {
		return Wrapf(err, ErrCodeStorageFailure, "Storage operation %s failed", operation)
	}

	ErrCacheOperationFailed = func(operation string, err error) *AppError {
		return Wrapf(err, ErrCodeCacheFailure, "Cache operation %s failed", operation)
	}

	ErrConnectionFailed = func(component string, err error) *AppError {
		return Wrapf(err, ErrCodeConnectionFail, "%s connection failed", component)
	}

	ErrConfigKeyNotFound = func(key string) *AppError {
		return Newf(ErrCodeConfigNotFound, "Configuration key %s not found", key)
	}

	ErrConfigValueInvalid = func(key string, value interface{}, reason string) *AppError {
		return Newf(ErrCodeInvalidConfig, "Invalid configuration value for %s: %v (%s)", key, value, reason)
	}

	ErrBackupOperationFailed = func(operation string, err error) *AppError {
		return Wrapf(err, ErrCodeBackupFailed, "Backup operation %s failed", operation)
	}

	ErrOperationTimeout = func(operation string, timeout time.Duration) *AppError {
		return Newf(ErrCodeTimeout, "Operation %s timed out after %v", operation, timeout)
	}

	ErrRateLimitExceeded = func(limit int, window time.Duration) *AppError {
		return Newf(ErrCodeRateLimited, "Rate limit exceeded: %d requests per %v", limit, window)
	}

	ErrSQLInjectionDetected = func(query string) *AppError {
		return Newf(ErrCodeSQLInjection, "SQL injection detected in query: %s", query)
	}
)

// GetErrorCode 从错误中提取错误码
func GetErrorCode(err error) ErrorCode {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code
	}
	return ErrCodeInternal
}

// GetHTTPStatus 获取错误对应的HTTP状态码
func GetHTTPStatus(err error) int {
	if appErr, ok := err.(*AppError); ok {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}

// GetGRPCCode 获取错误对应的gRPC状态码
func GetGRPCCode(err error) codes.Code {
	if appErr, ok := err.(*AppError); ok {
		return appErr.GRPCCode
	}
	return codes.Internal
}

// IsNotFoundError 检查是否为"未找到"类错误
func IsNotFoundError(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeNotFound ||
			appErr.Code == ErrCodeTableNotFound ||
			appErr.Code == ErrCodeDataNotFound ||
			appErr.Code == ErrCodeConfigNotFound
	}
	return false
}

// IsRetryableError 检查错误是否可重试
func IsRetryableError(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		switch appErr.Code {
		case ErrCodeStorageFailure, ErrCodeCacheFailure, ErrCodeConnectionFail,
			ErrCodeTimeout, ErrCodeRateLimited:
			return true
		}
	}
	return false
}

// IsValidationError 检查是否为验证错误
func IsValidationError(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeInvalidInput ||
			appErr.Code == ErrCodeInvalidTableName ||
			appErr.Code == ErrCodeInvalidQuery ||
			appErr.Code == ErrCodeValidationError ||
			appErr.Code == ErrCodeInvalidConfig ||
			appErr.Code == ErrCodeTokenInvalid
	}
	return false
}

// FormatErrorMessage 格式化错误消息用于日志
func FormatErrorMessage(err error) string {
	if appErr, ok := err.(*AppError); ok {
		if appErr.Details != "" {
			return fmt.Sprintf("[%s] %s: %s", appErr.Code, appErr.Message, appErr.Details)
		}
		return fmt.Sprintf("[%s] %s", appErr.Code, appErr.Message)
	}
	return fmt.Sprintf("[UNKNOWN] %v", err)
}
