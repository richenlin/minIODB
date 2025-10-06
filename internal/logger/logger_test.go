package logger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestInitLogger_DefaultConfig(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	err := InitLogger(DefaultLogConfig)
	assert.NoError(t, err, "InitLogger should succeed with default config")
	assert.NotNil(t, Logger, "Logger should be initialized")
	assert.NotNil(t, Sugar, "Sugar logger should be initialized")
}

func TestInitLogger_CustomConfig(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	config := LogConfig{
		Level:      "debug",
		Format:     "console",
		Output:     "stdout",
		Filename:   "test.log",
		MaxSize:    50,
		MaxBackups: 3,
		MaxAge:     7,
		Compress:   false,
	}

	err := InitLogger(config)
	assert.NoError(t, err, "InitLogger should succeed with custom config")
	assert.NotNil(t, Logger, "Logger should be initialized")
	assert.NotNil(t, Sugar, "Sugar logger should be initialized")
}

func TestInitLogger_InvalidLevel(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	config := LogConfig{
		Level: "invalid",
	}

	err := InitLogger(config)
	assert.Error(t, err, "InitLogger should fail with invalid level")
	assert.Contains(t, err.Error(), "invalid log level")
}

func TestInitLogger_FileOutput(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	// Create a temporary directory for test logs
	tempDir := t.TempDir()
	testLogPath := filepath.Join(tempDir, "test.log")

	config := LogConfig{
		Level:      "info",
		Format:     "json",
		Output:     "file",
		Filename:   testLogPath,
		MaxSize:    1,
		MaxBackups: 1,
		MaxAge:     1,
		Compress:   false,
	}

	err := InitLogger(config)
	assert.NoError(t, err, "InitLogger should succeed with file output")

	// Test that log file was created
	_, err = os.Stat(testLogPath)
	assert.NoError(t, err, "Log file should be created")

	// Test logging to file
	Logger.Info("test message", zap.String("test", "value"))

	// Close logger to flush
	Sync()

	// Check that message was written to file
	content, err := os.ReadFile(testLogPath)
	assert.NoError(t, err, "Should be able to read log file")
	assert.Contains(t, string(content), "test message")
	assert.Contains(t, string(content), "test\":\"value\"")
}

func TestInitLogger_BothOutput(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	tempDir := t.TempDir()
	testLogPath := filepath.Join(tempDir, "test_both.log")

	config := LogConfig{
		Level:      "info",
		Format:     "console",
		Output:     "both",
		Filename:   testLogPath,
		MaxSize:    1,
		MaxBackups: 1,
		MaxAge:     1,
		Compress:   false,
	}

	err := InitLogger(config)
	assert.NoError(t, err, "InitLogger should succeed with both output")

	// Test that log file was created
	_, err = os.Stat(testLogPath)
	assert.NoError(t, err, "Log file should be created")
}

func TestWithContext_EmptyContext(t *testing.T) {
	// Initialize logger first
	err := InitLogger(DefaultLogConfig)
	require.NoError(t, err)

	ctx := context.Background()
	logger := WithContext(ctx)

	assert.NotNil(t, logger, "WithContext should return a logger")
}

func TestWithContext_WithTraceID(t *testing.T) {
	// Initialize logger first
	err := InitLogger(DefaultLogConfig)
	require.NoError(t, err)

	ctx := context.Background()
	ctx = SetTraceID(ctx, "trace-123")

	logger := WithContext(ctx)

	assert.NotNil(t, logger)
	// Note: We can't easily test the actual fields without capturing log output
}

func TestWithContext_WithMultipleFields(t *testing.T) {
	// Initialize logger first
	err := InitLogger(DefaultLogConfig)
	require.NoError(t, err)

	ctx := context.Background()
	ctx = SetTraceID(ctx, "trace-123")
	ctx = SetRequestID(ctx, "req-456")
	ctx = SetUserID(ctx, "user-789")
	ctx = SetOperation(ctx, "test-operation")

	logger := WithContext(ctx)

	assert.NotNil(t, logger)
}

func TestWithContextSugar(t *testing.T) {
	// Initialize logger first
	err := InitLogger(DefaultLogConfig)
	require.NoError(t, err)

	ctx := context.Background()
	ctx = SetTraceID(ctx, "trace-123")

	logger := WithContextSugar(ctx)

	assert.NotNil(t, logger)
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()

	// Test SetTraceID
	ctx = SetTraceID(ctx, "test-trace")
	traceID := GetTraceID(ctx)
	assert.Equal(t, "test-trace", traceID)

	// Test SetRequestID
	ctx = SetRequestID(ctx, "test-request")
	requestID := ctx.Value(requestIDKey)
	assert.Equal(t, "test-request", requestID)

	// Test SetUserID
	ctx = SetUserID(ctx, "test-user")
	userID := ctx.Value(userIDKey)
	assert.Equal(t, "test-user", userID)

	// Test SetOperation
	ctx = SetOperation(ctx, "test-operation")
	operation := ctx.Value(operationKey)
	assert.Equal(t, "test-operation", operation)
}

func TestGetTraceID_NotSet(t *testing.T) {
	ctx := context.Background()
	traceID := GetTraceID(ctx)
	assert.Empty(t, traceID, "GetTraceID should return empty string when not set")
}

func TestLogFunctions(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()

	// Test that all log functions work without panicking
	assert.NotPanics(t, func() {
		LogError(ctx, assert.AnError, "test error", zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogInfo(ctx, "test info", zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogWarn(ctx, "test warning", zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogDebug(ctx, "test debug", zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogPanic(ctx, "test panic", zap.String("field", "value"))
	})

	// Note: LogFatal will call os.Exit(1), so we can't test it in unit tests
}

func TestLogOperation(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 100 * time.Millisecond

	assert.NotPanics(t, func() {
		LogOperation(ctx, "test-operation", duration, nil, zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogOperation(ctx, "test-operation-error", duration, assert.AnError, zap.String("field", "value"))
	})
}

func TestLogHTTPRequest(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 50 * time.Millisecond

	assert.NotPanics(t, func() {
		LogHTTPRequest(ctx, "GET", "/test", 200, duration, zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogHTTPRequest(ctx, "POST", "/error", 500, duration, zap.String("field", "value"))
	})
}

func TestLogGRPCRequest(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 75 * time.Millisecond

	assert.NotPanics(t, func() {
		LogGRPCRequest(ctx, "/test.Service/Method", duration, nil, zap.String("field", "value"))
	})

	assert.NotPanics(t, func() {
		LogGRPCRequest(ctx, "/test.Service/ErrorMethod", duration, assert.AnError, zap.String("field", "value"))
	})
}

func TestLogQuery(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 25 * time.Millisecond

	assert.NotPanics(t, func() {
		LogQuery(ctx, "SELECT * FROM test", duration, 10, nil)
	})

	assert.NotPanics(t, func() {
		LogQuery(ctx, "SELECT * FROM error", duration, 0, assert.AnError)
	})
}

func TestLogDataWrite(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 30 * time.Millisecond

	assert.NotPanics(t, func() {
		LogDataWrite(ctx, "data-123", 1024, duration, nil)
	})

	assert.NotPanics(t, func() {
		LogDataWrite(ctx, "data-error", 2048, duration, assert.AnError)
	})
}

func TestLogBufferFlush(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 15 * time.Millisecond

	assert.NotPanics(t, func() {
		LogBufferFlush(ctx, "memory", 100, 4096, duration, nil)
	})

	assert.NotPanics(t, func() {
		LogBufferFlush(ctx, "disk", 200, 8192, duration, assert.AnError)
	})
}

func TestGetCaller(t *testing.T) {
	caller := GetCaller(0)
	assert.NotEmpty(t, caller, "GetCaller should return caller information")
	assert.Contains(t, caller, "logger_test.go", "Caller should contain current file name")
}

func TestSync(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	// Test that Sync doesn't panic
	assert.NotPanics(t, func() {
		Sync()
	})
}

func TestClose(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	// Test that Close doesn't panic
	assert.NotPanics(t, func() {
		Close()
	})
}

func TestGetLogger_NotInitialized(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	// Set logger to nil to simulate uninitialized state
	Logger = nil
	Sugar = nil

	logger := GetLogger()
	assert.NotNil(t, logger, "GetLogger should return a logger even when not initialized")
}

func TestGetSugaredLogger_NotInitialized(t *testing.T) {
	// Save original logger
	originalLogger := Logger
	originalSugar := Sugar
	defer func() {
		Logger = originalLogger
		Sugar = originalSugar
	}()

	// Set logger to nil to simulate uninitialized state
	Logger = nil
	Sugar = nil

	logger := GetSugaredLogger()
	assert.NotNil(t, logger, "GetSugaredLogger should return a logger even when not initialized")
}

func TestLogQuery_SQLCleanup(t *testing.T) {
	// Initialize logger first
	err := InitLogger(LogConfig{
		Level:  "debug",
		Output: "stdout",
		Format: "console",
	})
	require.NoError(t, err)

	ctx := context.Background()
	duration := 10 * time.Millisecond

	// Test SQL with newlines and extra spaces
	sqlWithNewlines := "SELECT * \n FROM test \n WHERE id = 1"

	assert.NotPanics(t, func() {
		LogQuery(ctx, sqlWithNewlines, duration, 1, nil)
	})

	// Test very long SQL (should be truncated)
	longSQL := "SELECT " + string(make([]byte, 300)) + " FROM test"
	assert.NotPanics(t, func() {
		LogQuery(ctx, longSQL, duration, 1, nil)
	})
}

func TestLogConfig_DefaultValues(t *testing.T) {
	assert.Equal(t, "info", DefaultLogConfig.Level)
	assert.Equal(t, "json", DefaultLogConfig.Format)
	assert.Equal(t, "both", DefaultLogConfig.Output)
	assert.Equal(t, "logs/miniodb.log", DefaultLogConfig.Filename)
	assert.Equal(t, 100, DefaultLogConfig.MaxSize)
	assert.Equal(t, 5, DefaultLogConfig.MaxBackups)
	assert.Equal(t, 30, DefaultLogConfig.MaxAge)
	assert.True(t, DefaultLogConfig.Compress)
}
