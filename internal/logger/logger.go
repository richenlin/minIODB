package logger

import (
	"io"
	"os"

	"minIODB/internal/config"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var log *logrus.Logger

// Init initializes the logger with the given configuration
func Init(cfg config.LogConfig) error {
	log = logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		return err
	}
	log.SetLevel(level)

	// Set log format
	if cfg.Format == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	}

	// Set output
	var output io.Writer
	switch cfg.Output {
	case "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		// File output with rotation
		output = &lumberjack.Logger{
			Filename:   cfg.Output,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
		}
	}
	log.SetOutput(output)

	return nil
}

// GetLogger returns the configured logger instance
func GetLogger() *logrus.Logger {
	if log == nil {
		// Fallback to default logger if not initialized
		log = logrus.New()
	}
	return log
}

// WithFields creates a new logger entry with the given fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return GetLogger().WithFields(fields)
}

// WithField creates a new logger entry with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

// Info logs an info message
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

// Fatal logs a fatal message and exits
func Fatal(args ...interface{}) {
	GetLogger().Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	GetLogger().Fatalf(format, args...)
}

// Panic logs a panic message and panics
func Panic(args ...interface{}) {
	GetLogger().Panic(args...)
}

// Panicf logs a formatted panic message and panics
func Panicf(format string, args ...interface{}) {
	GetLogger().Panicf(format, args...)
} 