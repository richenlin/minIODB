package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogConfig 日志配置
type LogConfig struct {
	Level      string `mapstructure:"level"`       // 日志级别
	Format     string `mapstructure:"format"`      // 日志格式 (json/console)
	Output     string `mapstructure:"output"`      // 输出方式 (stdout/file/both)
	Filename   string `mapstructure:"filename"`    // 日志文件名
	MaxSize    int    `mapstructure:"max_size"`    // 单个日志文件最大大小(MB)
	MaxBackups int    `mapstructure:"max_backups"` // 保留的旧日志文件数量
	MaxAge     int    `mapstructure:"max_age"`     // 保留日志文件的最大天数
	Compress   bool   `mapstructure:"compress"`    // 是否压缩旧日志文件
}

// DefaultLogConfig 默认日志配置
var DefaultLogConfig = LogConfig{
	Level:      "info",
	Format:     "json",
	Output:     "both",
	Filename:   "logs/miniodb.log",
	MaxSize:    100,
	MaxBackups: 5,
	MaxAge:     30,
	Compress:   true,
}

// Logger 全局日志器
var Logger *zap.Logger
var Sugar *zap.SugaredLogger

// contextKey 上下文键类型
type contextKey string

const (
	traceIDKey    contextKey = "trace_id"
	requestIDKey  contextKey = "request_id"
	userIDKey     contextKey = "user_id"
	operationKey  contextKey = "operation"
)

// InitLogger 初始化日志器
func InitLogger(config LogConfig) error {
	// 解析日志级别
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}

	// 创建编码器配置
	var encoderConfig zapcore.EncoderConfig
	if config.Format == "json" {
		encoderConfig = zap.NewProductionEncoderConfig()
	} else {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.CallerKey = "caller"
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	// 创建编码器
	var encoder zapcore.Encoder
	if config.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// 创建写入器
	var writers []zapcore.WriteSyncer

	// 标准输出
	if config.Output == "stdout" || config.Output == "both" {
		writers = append(writers, zapcore.AddSync(os.Stdout))
	}

	// 文件输出
	if config.Output == "file" || config.Output == "both" {
		// 确保日志目录存在
		logDir := filepath.Dir(config.Filename)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		// 配置日志轮转
		fileWriter := &lumberjack.Logger{
			Filename:   config.Filename,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		writers = append(writers, zapcore.AddSync(fileWriter))
	}

	// 创建核心
	core := zapcore.NewCore(
		encoder,
		zapcore.NewMultiWriteSyncer(writers...),
		level,
	)

	// 创建日志器
	Logger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	Sugar = Logger.Sugar()

	return nil
}

// WithContext 从上下文中提取字段创建带上下文的日志器
func WithContext(ctx context.Context) *zap.Logger {
	logger := Logger

	// 添加trace ID
	if traceID := ctx.Value(traceIDKey); traceID != nil {
		logger = logger.With(zap.String("trace_id", traceID.(string)))
	}

	// 添加request ID
	if requestID := ctx.Value(requestIDKey); requestID != nil {
		logger = logger.With(zap.String("request_id", requestID.(string)))
	}

	// 添加user ID
	if userID := ctx.Value(userIDKey); userID != nil {
		logger = logger.With(zap.String("user_id", userID.(string)))
	}

	// 添加操作名称
	if operation := ctx.Value(operationKey); operation != nil {
		logger = logger.With(zap.String("operation", operation.(string)))
	}

	return logger
}

// WithContextSugar 从上下文中提取字段创建带上下文的糖化日志器
func WithContextSugar(ctx context.Context) *zap.SugaredLogger {
	return WithContext(ctx).Sugar()
}

// SetTraceID 设置trace ID到上下文
func SetTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// SetRequestID 设置request ID到上下文
func SetRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// SetUserID 设置user ID到上下文
func SetUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// SetOperation 设置操作名称到上下文
func SetOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey, operation)
}

// GetTraceID 从上下文获取trace ID
func GetTraceID(ctx context.Context) string {
	if traceID := ctx.Value(traceIDKey); traceID != nil {
		return traceID.(string)
	}
	return ""
}

// LogError 记录错误日志
func LogError(ctx context.Context, err error, msg string, fields ...zap.Field) {
	logger := WithContext(ctx)
	fields = append(fields, zap.Error(err))
	logger.Error(msg, fields...)
}

// LogInfo 记录信息日志
func LogInfo(ctx context.Context, msg string, fields ...zap.Field) {
	logger := WithContext(ctx)
	logger.Info(msg, fields...)
}

// LogWarn 记录警告日志
func LogWarn(ctx context.Context, msg string, fields ...zap.Field) {
	logger := WithContext(ctx)
	logger.Warn(msg, fields...)
}

// LogDebug 记录调试日志
func LogDebug(ctx context.Context, msg string, fields ...zap.Field) {
	logger := WithContext(ctx)
	logger.Debug(msg, fields...)
}

// LogPanic 记录panic日志
func LogPanic(ctx context.Context, msg string, fields ...zap.Field) {
	logger := WithContext(ctx)
	logger.Panic(msg, fields...)
}

// LogFatal 记录致命错误日志
func LogFatal(ctx context.Context, msg string, fields ...zap.Field) {
	logger := WithContext(ctx)
	logger.Fatal(msg, fields...)
}

// LogOperation 记录操作日志
func LogOperation(ctx context.Context, operation string, duration time.Duration, err error, fields ...zap.Field) {
	logger := WithContext(ctx)
	
	// 添加操作相关字段
	fields = append(fields,
		zap.String("operation", operation),
		zap.Duration("duration", duration),
	)

	if err != nil {
		fields = append(fields, zap.Error(err))
		logger.Error("Operation failed", fields...)
	} else {
		logger.Info("Operation completed", fields...)
	}
}

// LogHTTPRequest 记录HTTP请求日志
func LogHTTPRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration, fields ...zap.Field) {
	logger := WithContext(ctx)
	
	fields = append(fields,
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status_code", statusCode),
		zap.Duration("duration", duration),
	)

	if statusCode >= 400 {
		logger.Error("HTTP request failed", fields...)
	} else {
		logger.Info("HTTP request completed", fields...)
	}
}

// LogGRPCRequest 记录gRPC请求日志
func LogGRPCRequest(ctx context.Context, method string, duration time.Duration, err error, fields ...zap.Field) {
	logger := WithContext(ctx)
	
	fields = append(fields,
		zap.String("grpc_method", method),
		zap.Duration("duration", duration),
	)

	if err != nil {
		fields = append(fields, zap.Error(err))
		logger.Error("gRPC request failed", fields...)
	} else {
		logger.Info("gRPC request completed", fields...)
	}
}

// LogQuery 记录查询日志
func LogQuery(ctx context.Context, sql string, duration time.Duration, rowCount int, err error) {
	logger := WithContext(ctx)
	
	// 清理SQL语句（移除多余空格和换行）
	cleanSQL := strings.ReplaceAll(strings.TrimSpace(sql), "\n", " ")
	if len(cleanSQL) > 200 {
		cleanSQL = cleanSQL[:200] + "..."
	}

	fields := []zap.Field{
		zap.String("sql", cleanSQL),
		zap.Duration("duration", duration),
		zap.Int("row_count", rowCount),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		logger.Error("Query failed", fields...)
	} else {
		logger.Info("Query completed", fields...)
	}
}

// LogDataWrite 记录数据写入日志
func LogDataWrite(ctx context.Context, dataID string, size int64, duration time.Duration, err error) {
	logger := WithContext(ctx)
	
	fields := []zap.Field{
		zap.String("data_id", dataID),
		zap.Int64("size_bytes", size),
		zap.Duration("duration", duration),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		logger.Error("Data write failed", fields...)
	} else {
		logger.Info("Data write completed", fields...)
	}
}

// LogBufferFlush 记录缓冲区刷新日志
func LogBufferFlush(ctx context.Context, bufferType string, itemCount int, size int64, duration time.Duration, err error) {
	logger := WithContext(ctx)
	
	fields := []zap.Field{
		zap.String("buffer_type", bufferType),
		zap.Int("item_count", itemCount),
		zap.Int64("size_bytes", size),
		zap.Duration("duration", duration),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		logger.Error("Buffer flush failed", fields...)
	} else {
		logger.Info("Buffer flush completed", fields...)
	}
}

// GetCaller 获取调用者信息
func GetCaller(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

// Sync 同步日志器
func Sync() {
	if Logger != nil {
		Logger.Sync()
	}
}

// Close 关闭日志器
func Close() {
	if Logger != nil {
		Logger.Sync()
	}
} 