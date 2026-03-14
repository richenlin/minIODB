//go:build !dashboard

package rest

import (
	"net/http"

	"minIODB/config"
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

func MountDashboardToRouter(_ *gin.Engine, cfg *config.Config, logger *zap.Logger, _ DashboardParams) {
	if cfg.Dashboard.Enabled {
		logger.Warn("Dashboard enabled but binary was built without dashboard support. Rebuild with -tags dashboard")
	}
}

func startStandaloneDashboardServer(_ *Server, _ http.HandlerFunc, _ *discovery.ServiceRegistry, _ *pool.RedisPool) {}
