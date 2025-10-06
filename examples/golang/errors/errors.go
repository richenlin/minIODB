package errors

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrorCode 错误码类型
type ErrorCode string

const (
	ErrorCodeUnknown        ErrorCode = "UNKNOWN"
	ErrorCodeConnection     ErrorCode = "CONNECTION_ERROR"
	ErrorCodeAuthentication ErrorCode = "AUTHENTICATION_ERROR"
	ErrorCodeRequest        ErrorCode = "REQUEST_ERROR"
	ErrorCodeServer         ErrorCode = "SERVER_ERROR"
	ErrorCodeTimeout        ErrorCode = "TIMEOUT_ERROR"
)

// MinIODBError MinIODB 客户端错误
type MinIODBError struct {
	Code       ErrorCode
	StatusCode int
	Message    string
	Cause      error
}

// Error 实现 error 接口
func (e *MinIODBError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("MinIODBError(code=%s, status=%d, message='%s', cause=%v)",
			e.Code, e.StatusCode, e.Message, e.Cause)
	}
	return fmt.Sprintf("MinIODBError(code=%s, status=%d, message='%s')",
		e.Code, e.StatusCode, e.Message)
}

// Unwrap 返回原始错误
func (e *MinIODBError) Unwrap() error {
	return e.Cause
}

// NewMinIODBError 创建新的 MinIODB 错误
func NewMinIODBError(code ErrorCode, statusCode int, message string, cause error) *MinIODBError {
	return &MinIODBError{
		Code:       code,
		StatusCode: statusCode,
		Message:    message,
		Cause:      cause,
	}
}

// NewConnectionError 创建连接错误
func NewConnectionError(message string, cause error) *MinIODBError {
	return NewMinIODBError(ErrorCodeConnection, 503, message, cause)
}

// NewAuthenticationError 创建认证错误
func NewAuthenticationError(message string, cause error) *MinIODBError {
	return NewMinIODBError(ErrorCodeAuthentication, 401, message, cause)
}

// NewRequestError 创建请求错误
func NewRequestError(message string, cause error) *MinIODBError {
	return NewMinIODBError(ErrorCodeRequest, 400, message, cause)
}

// NewServerError 创建服务器错误
func NewServerError(message string, cause error) *MinIODBError {
	return NewMinIODBError(ErrorCodeServer, 500, message, cause)
}

// NewTimeoutError 创建超时错误
func NewTimeoutError(message string, cause error) *MinIODBError {
	return NewMinIODBError(ErrorCodeTimeout, 408, message, cause)
}

// IsConnectionError 检查是否为连接错误
func IsConnectionError(err error) bool {
	if miniodbErr, ok := err.(*MinIODBError); ok {
		return miniodbErr.Code == ErrorCodeConnection
	}
	return false
}

// IsAuthenticationError 检查是否为认证错误
func IsAuthenticationError(err error) bool {
	if miniodbErr, ok := err.(*MinIODBError); ok {
		return miniodbErr.Code == ErrorCodeAuthentication
	}
	return false
}

// IsRequestError 检查是否为请求错误
func IsRequestError(err error) bool {
	if miniodbErr, ok := err.(*MinIODBError); ok {
		return miniodbErr.Code == ErrorCodeRequest
	}
	return false
}

// IsServerError 检查是否为服务器错误
func IsServerError(err error) bool {
	if miniodbErr, ok := err.(*MinIODBError); ok {
		return miniodbErr.Code == ErrorCodeServer
	}
	return false
}

// IsTimeoutError 检查是否为超时错误
func IsTimeoutError(err error) bool {
	if miniodbErr, ok := err.(*MinIODBError); ok {
		return miniodbErr.Code == ErrorCodeTimeout
	}
	return false
}

// FromGRPCError 从 gRPC 错误转换为 MinIODB 错误
func FromGRPCError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return NewServerError("未知的 gRPC 错误", err)
	}

	switch st.Code() {
	case codes.Unauthenticated:
		return NewAuthenticationError(st.Message(), err)
	case codes.InvalidArgument:
		return NewRequestError(st.Message(), err)
	case codes.DeadlineExceeded:
		return NewTimeoutError(st.Message(), err)
	case codes.Unavailable:
		return NewConnectionError(st.Message(), err)
	case codes.Internal:
		return NewServerError(st.Message(), err)
	default:
		return NewServerError(st.Message(), err)
	}
}
