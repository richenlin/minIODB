package errors

import (
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
)

func TestAppError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{
			name: "error without details",
			err:  New(ErrCodeInvalidInput, "Invalid parameter"),
			want: "INVALID_INPUT: Invalid parameter",
		},
		{
			name: "error with details",
			err:  New(ErrCodeInvalidInput, "Invalid parameter").WithDetails("field 'name' is required"),
			want: "INVALID_INPUT: Invalid parameter (field 'name' is required)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("AppError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	err := New(ErrCodeTableNotFound, "Table 'users' not found")

	if err.Code != ErrCodeTableNotFound {
		t.Errorf("Code = %v, want %v", err.Code, ErrCodeTableNotFound)
	}

	if err.Message != "Table 'users' not found" {
		t.Errorf("Message = %v, want %v", err.Message, "Table 'users' not found")
	}

	if err.HTTPStatus != http.StatusNotFound {
		t.Errorf("HTTPStatus = %v, want %v", err.HTTPStatus, http.StatusNotFound)
	}

	if err.GRPCCode != codes.NotFound {
		t.Errorf("GRPCCode = %v, want %v", err.GRPCCode, codes.NotFound)
	}
}

func TestNewf(t *testing.T) {
	err := Newf(ErrCodeTableNotFound, "Table '%s' not found", "users")

	if err.Message != "Table 'users' not found" {
		t.Errorf("Message = %v, want %v", err.Message, "Table 'users' not found")
	}
}

func TestWrap(t *testing.T) {
	originalErr := fmt.Errorf("connection timeout")
	wrappedErr := Wrap(originalErr, ErrCodeConnectionFail, "Failed to connect to database")

	if wrappedErr.Code != ErrCodeConnectionFail {
		t.Errorf("Code = %v, want %v", wrappedErr.Code, ErrCodeConnectionFail)
	}

	if wrappedErr.Message != "Failed to connect to database" {
		t.Errorf("Message = %v, want %v", wrappedErr.Message, "Failed to connect to database")
	}

	if wrappedErr.Details != "connection timeout" {
		t.Errorf("Details = %v, want %v", wrappedErr.Details, "connection timeout")
	}

	if wrappedErr.Cause != originalErr {
		t.Errorf("Cause = %v, want %v", wrappedErr.Cause, originalErr)
	}
}

func TestWrapAppError(t *testing.T) {
	originalErr := New(ErrCodeStorageFailure, "Storage write failed")
	wrappedErr := Wrap(originalErr, ErrCodeInternal, "Operation failed")

	// 应该保持原有的错误代码
	if wrappedErr.Code != ErrCodeStorageFailure {
		t.Errorf("Code = %v, want %v", wrappedErr.Code, ErrCodeStorageFailure)
	}

	if wrappedErr.Message != "Operation failed" {
		t.Errorf("Message = %v, want %v", wrappedErr.Message, "Operation failed")
	}

	if wrappedErr.Details != "Storage write failed" {
		t.Errorf("Details = %v, want %v", wrappedErr.Details, "Storage write failed")
	}
}

func TestWrapNilError(t *testing.T) {
	wrappedErr := Wrap(nil, ErrCodeInternal, "This should be nil")

	if wrappedErr != nil {
		t.Errorf("Wrap(nil) should return nil, got %v", wrappedErr)
	}
}

func TestAppError_ToHTTPResponse(t *testing.T) {
	err := New(ErrCodeTableNotFound, "Table not found").WithDetails("Table 'users' does not exist")

	status, response := err.ToHTTPResponse()

	if status != http.StatusNotFound {
		t.Errorf("Status = %v, want %v", status, http.StatusNotFound)
	}

	if response["success"] != false {
		t.Errorf("Success = %v, want false", response["success"])
	}

	errorData, ok := response["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Error data should be a map")
	}

	if errorData["code"] != ErrCodeTableNotFound {
		t.Errorf("Error code = %v, want %v", errorData["code"], ErrCodeTableNotFound)
	}

	if errorData["message"] != "Table not found" {
		t.Errorf("Error message = %v, want %v", errorData["message"], "Table not found")
	}
}

func TestAppError_ToGRPCError(t *testing.T) {
	err := New(ErrCodeInvalidInput, "Invalid parameter")
	grpcErr := err.ToGRPCError()

	if grpcErr == nil {
		t.Fatal("gRPC error should not be nil")
	}

	// 检查错误消息
	if grpcErr.Error() != "rpc error: code = InvalidArgument desc = Invalid parameter" {
		t.Errorf("gRPC error = %v", grpcErr.Error())
	}
}

func TestIsAppError(t *testing.T) {
	appErr := New(ErrCodeInternal, "Internal error")
	stdErr := fmt.Errorf("standard error")

	if !IsAppError(appErr) {
		t.Error("IsAppError should return true for AppError")
	}

	if IsAppError(stdErr) {
		t.Error("IsAppError should return false for standard error")
	}

	if IsAppError(nil) {
		t.Error("IsAppError should return false for nil")
	}
}

func TestGetAppError(t *testing.T) {
	appErr := New(ErrCodeInternal, "Internal error")
	stdErr := fmt.Errorf("standard error")

	if GetAppError(appErr) != appErr {
		t.Error("GetAppError should return the same AppError")
	}

	if GetAppError(stdErr) != nil {
		t.Error("GetAppError should return nil for standard error")
	}

	if GetAppError(nil) != nil {
		t.Error("GetAppError should return nil for nil")
	}
}

func TestDefaultHTTPStatus(t *testing.T) {
	tests := []struct {
		code   ErrorCode
		status int
	}{
		{ErrCodeInvalidInput, http.StatusBadRequest},
		{ErrCodeUnauthorized, http.StatusUnauthorized},
		{ErrCodeForbidden, http.StatusForbidden},
		{ErrCodeNotFound, http.StatusNotFound},
		{ErrCodeTableExists, http.StatusConflict},
		{ErrCodeRateLimited, http.StatusTooManyRequests},
		{ErrCodeInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			err := New(tt.code, "test message")
			if err.HTTPStatus != tt.status {
				t.Errorf("HTTPStatus = %v, want %v", err.HTTPStatus, tt.status)
			}
		})
	}
}

func TestDefaultGRPCCode(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		grpcCode codes.Code
	}{
		{ErrCodeInvalidInput, codes.InvalidArgument},
		{ErrCodeUnauthorized, codes.Unauthenticated},
		{ErrCodeForbidden, codes.PermissionDenied},
		{ErrCodeNotFound, codes.NotFound},
		{ErrCodeTableExists, codes.AlreadyExists},
		{ErrCodeRateLimited, codes.ResourceExhausted},
		{ErrCodeConnectionFail, codes.Unavailable},
		{ErrCodeInternal, codes.Internal},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			err := New(tt.code, "test message")
			if err.GRPCCode != tt.grpcCode {
				t.Errorf("GRPCCode = %v, want %v", err.GRPCCode, tt.grpcCode)
			}
		})
	}
}

func TestPredefinedErrors(t *testing.T) {
	errors := []*AppError{
		ErrInternalServer,
		ErrInvalidInput,
		ErrNotFound,
		ErrUnauthorized,
		ErrForbidden,
		ErrTableNotFound,
		ErrTableExists,
		ErrInvalidTableName,
		ErrDataNotFound,
		ErrInvalidQuery,
		ErrStorageFailure,
		ErrCacheFailure,
		ErrConnectionFail,
		ErrTokenInvalid,
		ErrTokenExpired,
		ErrRateLimited,
		ErrSQLInjection,
	}

	for _, err := range errors {
		if err.Code == "" {
			t.Errorf("Predefined error has empty code: %v", err)
		}
		if err.Message == "" {
			t.Errorf("Predefined error has empty message: %v", err)
		}
	}
}
