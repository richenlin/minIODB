//go:build dashboard

package rest

import (
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

	srv := NewDashboardServer(coreClient, cfg, logger)
	srv.MountRoutes(router.Group(cfg.Dashboard.BasePath))
	logger.Info("Dashboard mounted in All-in-One mode", zap.String("base_path", cfg.Dashboard.BasePath))
}

func NewDashboardServer(coreClient client.CoreClient, cfg *config.Config, logger *zap.Logger) *dashboard.Server {
	return dashboard.NewServer(coreClient, cfg, logger)
}
