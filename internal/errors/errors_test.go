package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// AppError测试

func TestNew(t *testing.T) {
	err := New(ErrCodeInvalidRequest, "test error message")

	assert.NotNil(t, err)
	assert.Equal(t, ErrCodeInvalidRequest, err.Code)
	assert.Equal(t, "test error message", err.Message)
	assert.Equal(t, 400, err.HTTPStatus)
}

func TestNewf(t *testing.T) {
	err := Newf(ErrCodeInvalidRequest, "test %s with %d", "error", 123)

	assert.NotNil(t, err)
	assert.Contains(t, err.Message, "test error with 123")
}

func TestWrap(t *testing.T) {
	originalErr := errors.New("original error")
	wrappedErr := Wrap(originalErr, ErrCodeInternal, "wrapped message")

	assert.NotNil(t, wrappedErr)
	assert.Equal(t, ErrCodeInternal, wrappedErr.Code)
	assert.Equal(t, "wrapped message", wrappedErr.Message)
	assert.Equal(t, originalErr, wrappedErr.Cause)
}

func TestWrapf(t *testing.T) {
	originalErr := errors.New("original")
	wrappedErr := Wrapf(originalErr, ErrCodeInternal, "wrapped %s", "error")

	assert.NotNil(t, wrappedErr)
	assert.Contains(t, wrappedErr.Message, "wrapped error")
}

func TestAppError_Error(t *testing.T) {
	err := &AppError{
		Code:    ErrCodeInvalidRequest,
		Message: "test message",
	}
	result := err.Error()
	assert.Contains(t, result, "test message")
}

func TestAppError_WithDetails(t *testing.T) {
	err := New(ErrCodeInvalidRequest, "test")
	err = err.WithDetails("additional info")

	assert.Equal(t, "additional info", err.Details)
}

func TestAppError_WithHTTPStatus(t *testing.T) {
	err := New(ErrCodeInvalidRequest, "test")
	err = err.WithHTTPStatus(418)

	assert.Equal(t, 418, err.HTTPStatus)
}

func TestIsAppError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "AppError类型",
			err:      New(ErrCodeInvalidRequest, "test"),
			expected: true,
		},
		{
			name:     "标准error类型",
			err:      errors.New("standard error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAppError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetAppError(t *testing.T) {
	appErr := New(ErrCodeInvalidRequest, "test")
	result := GetAppError(appErr)
	assert.NotNil(t, result)
	assert.Equal(t, ErrCodeInvalidRequest, result.Code)

	stdErr := errors.New("standard")
	result = GetAppError(stdErr)
	assert.Nil(t, result)
}

func TestGetHTTPStatus(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected int
	}{
		{ErrCodeInvalidRequest, 400},
		{ErrCodeUnauthorized, 401},
		{ErrCodeForbidden, 403},
		{ErrCodeNotFound, 404},
		{ErrCodeConflict, 409},
		{ErrCodeTooManyRequests, 429},
		{ErrCodeInternal, 500},
		{ErrCodeServiceUnavailable, 503},
		{ErrorCode("UNKNOWN"), 500},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			result := getHTTPStatus(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// OlapError测试

func TestNewOlapError(t *testing.T) {
	err := NewOlapError(
		ErrTypeSystem,
		SeverityCritical,
		"TEST001",
		"test message",
		"test-component",
		"test-operation",
	)

	assert.NotNil(t, err)
	assert.Equal(t, ErrTypeSystem, err.Type)
	assert.Equal(t, SeverityCritical, err.Severity)
	assert.Equal(t, "TEST001", err.Code)
}

func TestOlapError_Error(t *testing.T) {
	err := NewNetworkError("NET001", "connection failed", "network", "connect")
	result := err.Error()
	assert.Contains(t, result, "NET001")
	assert.Contains(t, result, "connection failed")
}

func TestOlapError_WithCause(t *testing.T) {
	originalErr := errors.New("root cause")
	olapErr := NewSystemError("SYS001", "system error", "system", "op")
	olapErr = olapErr.WithCause(originalErr)

	assert.Equal(t, originalErr, olapErr.Cause)
}

func TestOlapError_WithRetryable(t *testing.T) {
	err := NewSystemError("SYS001", "system error", "system", "op")
	err = err.WithRetryable(true)

	assert.True(t, err.Retryable)
	assert.True(t, err.IsRetryable())
}

func TestNewSystemError(t *testing.T) {
	err := NewSystemError("SYS001", "system failure", "system", "start")

	assert.NotNil(t, err)
	assert.Equal(t, ErrTypeSystem, err.Type)
	assert.False(t, err.Retryable)
}

func TestNewNetworkError(t *testing.T) {
	err := NewNetworkError("NET001", "connection timeout", "network", "connect")

	assert.NotNil(t, err)
	assert.Equal(t, ErrTypeNetwork, err.Type)
	assert.True(t, err.Retryable)
}

func TestNewStorageError(t *testing.T) {
	err := NewStorageError("STG001", "disk full", "storage", "write")

	assert.NotNil(t, err)
	assert.Equal(t, ErrTypeStorage, err.Type)
}

func TestNewValidationError(t *testing.T) {
	err := NewValidationError("VAL001", "invalid input", "validator", "validate")

	assert.NotNil(t, err)
	assert.Equal(t, ErrTypeValidation, err.Type)
	assert.False(t, err.Retryable)
}

func TestNewTimeoutError(t *testing.T) {
	err := NewTimeoutError("TO001", "operation timeout", "service", "process")

	assert.NotNil(t, err)
	assert.Equal(t, ErrTypeTimeout, err.Type)
	assert.True(t, err.Retryable)
}

// ErrorHandler测试

func TestNewErrorHandler(t *testing.T) {
	handler := NewErrorHandler("test-component")

	assert.NotNil(t, handler)
}

func TestErrorHandler_Handle(t *testing.T) {
	handler := NewErrorHandler("test-component")

	// nil error
	result := handler.Handle(nil, "test")
	assert.Nil(t, result)

	// OlapError直接返回
	olapErr := NewSystemError("SYS001", "test", "comp", "op")
	result = handler.Handle(olapErr, "test")
	assert.NotNil(t, result)
	assert.Equal(t, ErrTypeSystem, result.Type)

	// 标准error分类
	stdErr := errors.New("connection refused")
	result = handler.Handle(stdErr, "test")
	assert.NotNil(t, result)
	assert.Equal(t, ErrTypeNetwork, result.Type)
}

func TestErrorHandler_ClassifyError(t *testing.T) {
	handler := NewErrorHandler("test-component")

	tests := []struct {
		name         string
		errMsg       string
		expectedType ErrorType
	}{
		{"网络错误", "connection refused", ErrTypeNetwork},
		{"超时错误", "context deadline exceeded", ErrTypeTimeout},
		{"存储错误", "storage unavailable", ErrTypeStorage},
		{"验证错误", "invalid format", ErrTypeValidation},
		{"资源错误", "memory limit exceeded", ErrTypeResource},
		{"系统错误", "unknown error", ErrTypeSystem},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			result := handler.classifyError(err, "test-op")

			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedType, result.Type)
		})
	}
}

func TestContainsAny(t *testing.T) {
	assert.True(t, containsAny("connection refused", []string{"refused", "timeout"}))
	assert.False(t, containsAny("unknown error", []string{"connection", "timeout"}))
	assert.False(t, containsAny("test", []string{}))
}

// 集成测试

func TestErrorChaining(t *testing.T) {
	rootErr := errors.New("root cause")
	wrappedErr := Wrap(rootErr, ErrCodeInternal, "wrapped once")
	doubleWrapped := Wrap(wrappedErr, ErrCodeServiceUnavailable, "wrapped twice")

	assert.Contains(t, doubleWrapped.Error(), "wrapped twice")

	unwrapped1 := errors.Unwrap(doubleWrapped)
	assert.NotNil(t, unwrapped1)
}

// 性能测试

func BenchmarkNewError(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = New(ErrCodeInternal, "benchmark error")
	}
}

func BenchmarkWrapError(b *testing.B) {
	baseErr := errors.New("base")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = Wrap(baseErr, ErrCodeInternal, "wrapped")
	}
}

func BenchmarkErrorHandler_Handle(b *testing.B) {
	handler := NewErrorHandler("bench")
	testErr := errors.New("test error")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = handler.Handle(testErr, "operation")
	}
}
