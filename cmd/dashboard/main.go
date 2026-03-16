//go:build dashboard

// Standalone MinIODB Dashboard server.
// Used in ansible/swarm/k8s deployments where the dashboard runs as a separate service
// and connects to miniodb via HTTP (cfg.Dashboard.CoreEndpoint).
//
// Build:
//
//	go build -tags dashboard -o dashboard ./cmd/dashboard/
package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"minIODB/config"
	"minIODB/internal/dashboard"
	"minIODB/internal/dashboard/client"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync() //nolint:errcheck

	configPath := os.Getenv("MINIODB_CONFIG_PATH")
	if configPath == "" {
		configPath = "/app/config/config.yaml"
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Warn("config load failed, using defaults", zap.String("path", configPath), zap.Error(err))
		cfg, _ = config.LoadConfig("") // fallback to pure defaults + env overrides
	}

	// Ensure dashboard is enabled with sane defaults
	cfg.Dashboard.Enabled = true
	if cfg.Dashboard.Port == "" {
		cfg.Dashboard.Port = ":9090"
	}
	if cfg.Dashboard.BasePath == "" {
		cfg.Dashboard.BasePath = "/dashboard"
	}

	// RemoteClient proxies data calls to the miniodb REST API
	coreClient := client.NewRemoteClient(cfg, logger)
	srv, err := dashboard.NewServer(coreClient, cfg, logger)
	if err != nil {
		logger.Fatal("failed to create dashboard server", zap.Error(err))
	}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	// Top-level /health for container health-check probes (outside the dashboard group)
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	srv.MountRoutes(engine.Group(cfg.Dashboard.BasePath))

	httpSrv := &http.Server{
		Addr:    cfg.Dashboard.Port,
		Handler: engine,
	}

	go func() {
		logger.Info("Dashboard server starting",
			zap.String("addr", cfg.Dashboard.Port),
			zap.String("base_path", cfg.Dashboard.BasePath),
			zap.String("core_endpoint", cfg.Dashboard.CoreEndpoint),
		)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Dashboard server error", zap.Error(err))
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logger.Info("Dashboard server shutting down")
	httpSrv.Close() //nolint:errcheck
}
