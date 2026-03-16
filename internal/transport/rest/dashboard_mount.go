//go:build dashboard

package rest

import (
	"net/http"

	"minIODB/config"
	"minIODB/internal/dashboard"
	"minIODB/internal/dashboard/client"
	"minIODB/internal/discovery"
	"minIODB/internal/metadata"
	"minIODB/internal/service"
	"minIODB/pkg/pool"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type DashboardParams struct {
	Service         *service.MinIODBService
	MetadataManager *metadata.Manager
	ServiceRegistry *discovery.ServiceRegistry
	RedisPool       *pool.RedisPool
}

func MountDashboardToRouter(router *gin.Engine, cfg *config.Config, logger *zap.Logger, params DashboardParams) {
	if !cfg.Dashboard.Enabled {
		return
	}

	coreClient := client.NewLocalClient(
		params.Service,
		params.MetadataManager,
		params.ServiceRegistry,
		params.RedisPool,
		cfg,
		logger,
	)

	srv, err := NewDashboardServer(coreClient, cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create dashboard server", zap.Error(err))
		return
	}
	srv.MountRoutes(router.Group(cfg.Dashboard.BasePath))
	logger.Info("Dashboard mounted in All-in-One mode", zap.String("base_path", cfg.Dashboard.BasePath))
}

func NewDashboardServer(coreClient client.CoreClient, cfg *config.Config, logger *zap.Logger) (*dashboard.Server, error) {
	return dashboard.NewServer(coreClient, cfg, logger)
}

// startStandaloneDashboardServer 在 Dashboard.Port（如 9090）上启动仅含 Dashboard 与 /metrics 的 HTTP 服务
func startStandaloneDashboardServer(s *Server, metricsHandler http.HandlerFunc, serviceRegistry *discovery.ServiceRegistry, redisPool *pool.RedisPool) {
	if !s.cfg.Dashboard.Enabled || s.cfg.Dashboard.Port == "" {
		return
	}
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	// /metrics 由 allinone 与 Prometheus 共用
	if metricsHandler != nil {
		engine.Any("/metrics", gin.WrapF(metricsHandler))
	}
	params := DashboardParams{
		Service:         s.miniodbService,
		MetadataManager: s.metadataManager,
		ServiceRegistry: serviceRegistry,
		RedisPool:       redisPool,
	}
	MountDashboardToRouter(engine, s.cfg, s.logger, params)
	srv := &http.Server{Addr: s.cfg.Dashboard.Port, Handler: engine}
	go func() {
		s.logger.Info("Standalone dashboard server starting", zap.String("port", s.cfg.Dashboard.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatal("Standalone dashboard server failed", zap.Error(err))
		}
	}()
}
