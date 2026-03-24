// @title           MinIODB API
// @version         1.0
// @description     基于MinIO+DuckDB+Redis的分布式OLAP系统API
// @description     支持高性能数据写入、SQL查询、表管理、元数据备份等功能

// @contact.name   MinIODB Support
// @contact.url    https://github.com/richenlin/minIODB

// @license.name  MIT
// @license.url    https://opensource.org/licenses/MIT

// @host      localhost:8081
// @BasePath  /v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT Bearer Token认证，格式: Bearer {token}

// @tag.name 认证
// @tag.description 用户认证和Token管理

// @tag.name 数据操作
// @tag.description 数据写入、查询、更新、删除

// @tag.name 表管理
// @tag.description 表的创建、查询、删除

// @tag.name 元数据
// @tag.description 元数据备份、恢复、状态查询

// @tag.name 系统监控
// @tag.description 健康检查、状态查询、性能指标

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"minIODB/config"
	"minIODB/internal/recovery"
	"minIODB/pkg/logger"
)

func main() {
	// 处理 --hash-password 命令行参数（用于生成密码哈希）
	if len(os.Args) > 1 && os.Args[1] == "--hash-password" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: miniodb --hash-password <password>")
			os.Exit(1)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(os.Args[2]), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(hash))
		os.Exit(0)
	}

	// 解析命令行参数，支持多个默认配置文件路径
	configPath := resolveConfigPath()

	// 加载配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	initLogger(cfg)
	defer logger.Close()

	logger.Sugar.Infof("Starting MinIODB server with config: %s", configPath)

	// JWT Secret 强制检查
	validateJWTConfig(cfg)

	// 创建 App 实例
	app := NewApp(cfg, logger.Logger, configPath)

	// 创建恢复处理器
	recoveryHandler := recovery.NewRecoveryHandler("main", logger.Logger)
	defer recoveryHandler.Recover()

	// 初始化各层组件（按依赖顺序）
	if err := app.InitStorage(); err != nil {
		logger.Sugar.Fatalf("Failed to initialize storage: %v", err)
	}
	defer app.Close()

	// InitMetadata 需要在 InitServices 之前，因为 miniodbService 依赖 metadataManager
	if err := app.InitMetadata(); err != nil {
		logger.Sugar.Fatalf("Failed to initialize metadata: %v", err)
	}

	// InitBackgroundServices 需要在 InitServices 之前，因为 miniodbService 依赖 subscriptionMgr
	if err := app.InitBackgroundServices(); err != nil {
		logger.Sugar.Fatalf("Failed to initialize background services: %v", err)
	}

	// InitServices 依赖 storage、metadata、subscription
	if err := app.InitServices(); err != nil {
		logger.Sugar.Fatalf("Failed to initialize services: %v", err)
	}

	// InitMetrics 依赖 buffer（在 InitServices 中创建）
	if err := app.InitMetrics(); err != nil {
		logger.Sugar.Fatalf("Failed to initialize metrics: %v", err)
	}

	// InitTransport 必须在 InitServices 之后
	if err := app.InitTransport(); err != nil {
		logger.Sugar.Fatalf("Failed to initialize transport: %v", err)
	}

	// 初始化复制和备份子系统
	if err := app.InitReplication(); err != nil {
		logger.Sugar.Warnf("Replication initialization failed: %v", err)
	}

	if err := app.InitBackup(); err != nil {
		logger.Sugar.Warnf("Backup initialization failed: %v", err)
	}

	// WireDashboard 必须在 InitReplication + InitBackup 之后
	app.WireDashboard()

	// 启动所有服务
	app.Start()

	// 等待关闭信号
	waitForShutdownSignal(app)

	logger.Sugar.Info("MinIODB server stopped")
}

// resolveConfigPath 解析配置文件路径
func resolveConfigPath() string {
	if len(os.Args) > 1 && os.Args[1] != "--hash-password" {
		return os.Args[1]
	}

	// 尝试默认配置文件路径
	defaultPaths := []string{
		"config/config.yaml",
		"config.yaml",
		"./config/config.yaml",
	}
	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// initLogger 初始化日志系统
func initLogger(cfg *config.Config) {
	logCfg := logger.LogConfig{
		Level:      cfg.Log.Level,
		Format:     cfg.Log.Format,
		Output:     cfg.Log.Output,
		Filename:   cfg.Log.Filename,
		MaxSize:    cfg.Log.MaxSize,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,
		Compress:   cfg.Log.Compress,
	}
	logger.InitLogger(logCfg)
}

// validateJWTConfig 验证 JWT 配置
func validateJWTConfig(cfg *config.Config) {
	if cfg.Security.Mode == "token" || cfg.Security.Mode == "jwt" {
		if cfg.Security.JWTSecret == "" {
			logger.Sugar.Fatalf("FATAL: JWT secret is required when auth mode is '%s'. Please set JWT_SECRET environment variable or configure security.jwt_secret in config file", cfg.Security.Mode)
		}
	}
}

// waitForShutdownSignal 等待关闭信号并优雅关闭
func waitForShutdownSignal(app *App) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logger.Sugar.Infof("Received shutdown signal: %v", sig)
	case err := <-app.FatalCh():
		logger.Sugar.Errorf("Fatal error from background service: %v", err)
	}

	// 创建独立的 shutdownCtx 用于关闭操作
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 优雅关闭
	app.Shutdown(shutdownCtx)
}
