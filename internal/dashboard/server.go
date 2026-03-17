package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	miniodbv1 "minIODB/api/proto/miniodb/v1"
	"minIODB/config"
	"minIODB/internal/dashboard/model"
	"minIODB/internal/dashboard/sse"
	"minIODB/internal/security"
	"minIODB/internal/service"
	"minIODB/pkg/version"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// statsRefreshInterval is configured per-instance via DashboardConfig.MetricsScrapeInterval.
// The minimum is 1 minute (to avoid overwhelming the DB with COUNT(*) queries);
// values below that threshold fall back to 5 minutes.

// statsCache holds pre-computed cluster statistics.
// All fields are protected by mu; reads return the last-known-good value
// immediately, even while a refresh is in progress.
type statsCache struct {
	mu            sync.RWMutex
	totalRecords  int64
	pendingWrites int64
	tablesCount   int
	nodesCount    int
	refreshedAt   time.Time
}

type contextKey string

const userContextKey contextKey = "user"

var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

var dangerousSQL = regexp.MustCompile(`(?i)(;|--|\b(DROP|ALTER|TRUNCATE|DELETE\s+FROM|INSERT\s+INTO|UPDATE\s+\w+\s+SET|UNION\s+(ALL\s+)?SELECT|INTO\s+OUTFILE|LOAD_FILE)\b)`)

func isSafeFilter(filter string) bool {
	return !dangerousSQL.MatchString(filter)
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return "********"
}

// Server is the Dashboard HTTP server: serves the Next.js SPA and the Dashboard API.
type Server struct {
	svc                  *service.MinIODBService
	cfg                  *config.Config
	cfgMu                sync.RWMutex
	logger               *zap.Logger
	authManager          *security.AuthManager
	hub                  *sse.Hub
	stats                *statsCache
	stopStats            context.CancelFunc
	statsRefreshInterval time.Duration
}

// NewServer creates a new Dashboard server and starts the background stats collector.
func NewServer(svc *service.MinIODBService, cfg *config.Config, logger *zap.Logger, authManager *security.AuthManager) (*Server, error) {
	hub := sse.NewHub()
	statsCtx, cancel := context.WithCancel(context.Background())

	statsInterval := cfg.Dashboard.MetricsScrapeInterval
	if statsInterval < 1*time.Minute {
		statsInterval = 5 * time.Minute
	}

	srv := &Server{
		svc:                  svc,
		cfg:                  cfg,
		logger:               logger,
		authManager:          authManager,
		hub:                  hub,
		stats:                &statsCache{},
		stopStats:            cancel,
		statsRefreshInterval: statsInterval,
	}

	go srv.runStatsCollector(statsCtx)

	return srv, nil
}

// runStatsCollector runs a periodic stats refresh in the background.
// It does NOT run in the hot request path; HTTP handlers only read from the cache.
func (s *Server) runStatsCollector(ctx context.Context) {
	s.refreshStats(ctx)
	ticker := time.NewTicker(s.statsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshStats(ctx)
		}
	}
}

// refreshStats collects cluster statistics and stores them in the cache.
// This is the only place that runs expensive operations (COUNT(*) per table).
func (s *Server) refreshStats(ctx context.Context) {
	// Use a short timeout so a slow core doesn't hold the goroutine forever.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// --- tables count ---------------------------------------------------------
	resp, err := s.svc.ListTables(ctx, &miniodbv1.ListTablesRequest{})
	tablesCount := 0
	if err == nil && resp != nil {
		tablesCount = len(resp.Tables)
	}

	// --- total records (COUNT(*) per table, concurrent) ----------------------
	var totalRecords int64
	if tablesCount > 0 {
		type countVal struct{ n int64 }
		ch := make(chan countVal, tablesCount)
		for _, t := range resp.Tables {
			go func(name string) {
				qr, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{
					Sql: fmt.Sprintf("SELECT COUNT(*) AS cnt FROM `%s`", name),
				})
				if err != nil || qr == nil || qr.ResultJson == "" {
					ch <- countVal{0}
					return
				}
				// Parse JSON result to get count
				var rows []map[string]interface{}
				if err := json.Unmarshal([]byte(qr.ResultJson), &rows); err != nil || len(rows) == 0 {
					ch <- countVal{0}
					return
				}
				var n int64
				for _, key := range []string{"cnt", "count_star()", "count(*)"} {
					if v, ok := rows[0][key]; ok {
						switch val := v.(type) {
						case float64:
							n = int64(val)
						case int64:
							n = val
						}
						break
					}
				}
				ch <- countVal{n}
			}(t.Name)
		}
		for range resp.Tables {
			totalRecords += (<-ch).n
		}
	}

	// --- pending writes (from status endpoint) --------------------------------
	var pendingWrites int64
	var nodesCount int
	if st, err := s.svc.GetStatus(ctx, &miniodbv1.GetStatusRequest{}); err == nil && st != nil {
		if pw, ok := st.BufferStats["pending_writes"]; ok {
			pendingWrites = pw
		}
		nodesCount = int(st.TotalNodes)
	}
	if nodesCount == 0 {
		nodesCount = 1
	}

	// --- atomically update the cache -----------------------------------------
	s.stats.mu.Lock()
	s.stats.totalRecords = totalRecords
	s.stats.pendingWrites = pendingWrites
	s.stats.tablesCount = tablesCount
	s.stats.nodesCount = nodesCount
	s.stats.refreshedAt = time.Now()
	s.stats.mu.Unlock()

	s.logger.Debug("stats cache refreshed",
		zap.Int64("total_records", totalRecords),
		zap.Int64("pending_writes", pendingWrites),
		zap.Int("tables", tablesCount),
	)
}

// getMode determines the cluster mode from configuration
func (s *Server) getMode() string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	if s.cfg.Network.Pools.Redis.Mode == "cluster" || s.cfg.Network.Pools.Redis.Mode == "sentinel" {
		return "distributed"
	}
	return "standalone"
}

// MountRoutes registers all dashboard routes on the given gin group.
// The group should correspond to cfg.Dashboard.BasePath (e.g. "/dashboard").
// Note: Frontend is served separately; this only registers API routes.
func (s *Server) MountRoutes(group *gin.RouterGroup) {
	api := group.Group("/api/v1")
	api.GET("/health", s.health)
	api.POST("/auth/login", s.login)

	auth := api.Group("")
	auth.Use(s.requireAuth)
	{
		auth.POST("/auth/logout", s.logout)
		auth.GET("/auth/me", s.getMe)
		auth.POST("/auth/change-password", s.changePassword)

		auth.GET("/cluster/info", s.clusterInfo)
		auth.GET("/cluster/topology", s.clusterTopology)
		auth.GET("/cluster/config", s.clusterConfig)
		auth.GET("/cluster/config/full", s.clusterFullConfig)
		auth.PUT("/cluster/config", s.updateClusterConfig)
		auth.POST("/cluster/enable-distributed", s.enableDistributed)

		auth.GET("/nodes", s.listNodes)
		auth.GET("/nodes/:id", s.getNode)

		auth.GET("/tables", s.listTables)
		auth.GET("/tables/:name", s.getTable)
		auth.POST("/tables", s.createTable)
		auth.PUT("/tables/:name", s.updateTable)
		auth.DELETE("/tables/:name", s.deleteTable)
		auth.GET("/tables/:name/stats", s.tableStats)

		auth.GET("/tables/:name/data", s.browseData)
		auth.POST("/tables/:name/data", s.writeRecord)
		auth.POST("/tables/:name/data/batch", s.writeBatch)
		auth.PUT("/tables/:name/data/:id", s.updateRecord)
		auth.DELETE("/tables/:name/data/:id", s.deleteRecord)
		auth.POST("/query", s.querySQL)

		auth.GET("/logs", s.queryLogs)
		auth.GET("/logs/stream", s.streamLogs)
		auth.GET("/logs/files", s.listLogFiles)

		auth.GET("/backups", s.listBackups)
		auth.POST("/backups/metadata", s.triggerMetadataBackup)
		auth.GET("/backups/schedule", s.getBackupSchedule)

		auth.GET("/monitor/overview", s.monitorOverview)
		auth.GET("/monitor/stream", s.monitorStream)
		auth.GET("/monitor/sla", s.slaMetrics)

		auth.POST("/analytics/query", s.analyticsQuery)
		auth.GET("/analytics/overview", s.analyticsOverview)
	}
}

// ---------- auth ----------

type loginRequest struct {
	APIKey    string `json:"api_key" binding:"required"`
	APISecret string `json:"api_secret" binding:"required"` // maps to secret in config.yaml auth.api_key_pairs
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt string    `json:"expires_at"`
	User      *userInfo `json:"user"`
}

type userInfo struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
}

func (s *Server) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	matched, role, displayName := s.authManager.ValidateCredentials(req.APIKey, req.APISecret)
	if !matched {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid credentials. Check auth.api_key_pairs in config.yaml.",
		})
		return
	}

	userID := req.APIKey
	username := displayName
	if username == "" {
		username = req.APIKey
	}

	token, err := s.authManager.GenerateToken(userID, username)
	if err != nil {
		s.logger.Error("failed to generate token", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	c.JSON(http.StatusOK, loginResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		User: &userInfo{
			ID:          userID,
			Username:    username,
			Role:        role,
			DisplayName: displayName,
		},
	})
}

func (s *Server) logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully. Note: token remains valid until expiry."})
}

func (s *Server) getMe(c *gin.Context) {
	claims, exists := c.Get(string(userContextKey))
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found in context"})
		return
	}

	userClaims, ok := claims.(*security.Claims)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user context"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": &userInfo{
			ID:       userClaims.UserID,
			Username: userClaims.Username,
		},
	})
}

func (s *Server) changePassword(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "Password change is not yet supported. Credentials are managed via config.yaml api_key_pairs.",
	})
}

// requireAuth middleware validates the Bearer token on secured routes.
func (s *Server) requireAuth(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" || token == authHeader {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
		return
	}

	claims, err := s.authManager.ValidateToken(token)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		return
	}

	c.Set(string(userContextKey), claims)
	c.Next()
}

// ---------- API handlers ----------

func (s *Server) health(c *gin.Context) {
	ctx := c.Request.Context()
	mode := s.getMode()
	if err := s.svc.HealthCheck(ctx); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":    "degraded",
			"timestamp": time.Now().Unix(),
			"version":   version.Get(),
			"node_id":   s.cfg.Server.NodeID,
			"mode":      mode,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"version":   version.Get(),
		"node_id":   s.cfg.Server.NodeID,
		"mode":      mode,
	})
}

func (s *Server) clusterInfo(c *gin.Context) {
	ctx := c.Request.Context()
	mode := s.getMode()

	// HealthCheck is a lightweight ping — do it live so the status badge is fresh.
	if err := s.svc.HealthCheck(ctx); err != nil {
		s.logger.Warn("cluster/info: HealthCheck failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
			"hint":  "ensure MinIODB Core is running",
		})
		return
	}

	// Uptime is read from the metrics endpoint
	var uptime int64
	if metrics, err := s.svc.GetMetrics(ctx, &miniodbv1.GetMetricsRequest{}); err == nil && metrics != nil {
		if uptimeStr, ok := metrics.SystemInfo["uptime_seconds"]; ok {
			if secs, err := time.ParseDuration(uptimeStr + "s"); err == nil {
				uptime = int64(secs.Seconds())
			}
		}
	}

	// Heavy stats (record counts, table list, nodes) come from the cache.
	// The cache is refreshed every statsRefreshInterval by a background goroutine.
	s.stats.mu.RLock()
	totalRecords := s.stats.totalRecords
	pendingWrites := s.stats.pendingWrites
	tablesCount := s.stats.tablesCount
	nodesCount := s.stats.nodesCount
	refreshedAt := s.stats.refreshedAt
	s.stats.mu.RUnlock()

	if nodesCount == 0 {
		nodesCount = 1 // at least the local node
	}

	var statsAgeS int64 = -1
	if !refreshedAt.IsZero() {
		statsAgeS = int64(time.Since(refreshedAt).Seconds())
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "healthy",
		"version":        version.Get(),
		"node_id":        s.cfg.Server.NodeID,
		"mode":           mode,
		"nodes_count":    nodesCount,
		"tables_count":   tablesCount,
		"uptime":         uptime,
		"total_records":  totalRecords,
		"pending_writes": pendingWrites,
		"stats_age_s":    statsAgeS,
	})
}

func (s *Server) clusterTopology(c *gin.Context) {
	ctx := c.Request.Context()

	st, err := s.svc.GetStatus(ctx, &miniodbv1.GetStatusRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	nodes := make([]*model.NodeResult, 0, len(st.Nodes))
	for _, n := range st.Nodes {
		nodes = append(nodes, &model.NodeResult{
			ID:       n.Id,
			Address:  n.Address,
			Port:     "",
			Status:   n.Status,
			LastSeen: time.Unix(n.LastSeen, 0),
			Metadata: map[string]string{"type": n.Type},
		})
	}

	type topoEdge struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
	}

	edges := make([]topoEdge, 0)

	// Resolve the effective Redis address for display, covering all three modes.
	redis := s.cfg.Network.Pools.Redis
	redisMode := redis.Mode
	redisAddr := redis.Addr
	switch redisMode {
	case "sentinel":
		if len(redis.SentinelAddrs) > 0 {
			redisAddr = redis.SentinelAddrs[0] // representative address
		}
	case "cluster":
		if len(redis.ClusterAddrs) > 0 {
			redisAddr = redis.ClusterAddrs[0]
		}
	}

	// Inject a virtual Redis coordinator whenever the cluster is actually running
	// in distributed mode.
	isDistributed := s.getMode() == "distributed"
	if isDistributed && redisAddr != "" {
		const redisID = "redis-coordinator"
		redisNode := &model.NodeResult{
			ID:      redisID,
			Address: redisAddr,
			Port:    "",
			Status:  "healthy",
			Metadata: map[string]string{
				"type": "redis",
				"role": "coordinator",
				"mode": redisMode,
			},
			Virtual: true,
		}
		nodes = append(nodes, redisNode)
		for _, n := range nodes[:len(nodes)-1] {
			edges = append(edges, topoEdge{
				Source: n.ID,
				Target: redisID,
				Type:   "coordination",
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"nodes":      nodes,
		"edges":      edges,
		"redis_addr": redisAddr,
		"redis_mode": redisMode,
	})
}

func (s *Server) clusterFullConfig(c *gin.Context) {
	s.cfgMu.RLock()
	cfg := s.cfg
	full := &model.FullConfig{
		// server
		NodeID:   cfg.Server.NodeID,
		GrpcPort: cfg.Server.GrpcPort,
		RestPort: cfg.Server.RestPort,

		// network.pools.redis
		RedisMode:       cfg.Network.Pools.Redis.Mode,
		RedisAddr:       cfg.Network.Pools.Redis.Addr,
		RedisPassword:   maskSecret(cfg.Network.Pools.Redis.Password),
		RedisDB:         cfg.Network.Pools.Redis.DB,
		RedisMasterName: cfg.Network.Pools.Redis.MasterName,
		SentinelAddrs:   cfg.Network.Pools.Redis.SentinelAddrs,
		ClusterAddrs:    cfg.Network.Pools.Redis.ClusterAddrs,

		// network.pools.minio
		MinioEndpoint:        cfg.Network.Pools.MinIO.Endpoint,
		MinioAccessKeyID:     cfg.Network.Pools.MinIO.AccessKeyID,
		MinioSecretAccessKey: maskSecret(cfg.Network.Pools.MinIO.SecretAccessKey),
		MinioUseSSL:          cfg.Network.Pools.MinIO.UseSSL,
		MinioRegion:          cfg.Network.Pools.MinIO.Region,
		MinioBucket:          cfg.Network.Pools.MinIO.Bucket,

		// dashboard
		CoreEndpoint:  cfg.Dashboard.CoreEndpoint,
		DashboardPort: cfg.Dashboard.Port,

		// log
		LogLevel:    cfg.Log.Level,
		LogFormat:   cfg.Log.Format,
		LogOutput:   cfg.Log.Output,
		LogFilename: cfg.Log.Filename,

		// buffer
		BufferSize:    cfg.Buffer.BufferSize,
		FlushInterval: cfg.Buffer.FlushInterval.String(),

		// runtime-read-only
		Mode:    s.getMode(),
		Version: version.Get(),
	}
	s.cfgMu.RUnlock()
	c.JSON(http.StatusOK, full)
}

func (s *Server) updateClusterConfig(c *gin.Context) {
	var req model.ConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Apply changes to in-memory config
	s.cfgMu.Lock()
	cfg := s.cfg
	if req.NodeID != "" {
		cfg.Server.NodeID = req.NodeID
	}
	if req.GrpcPort != "" {
		cfg.Server.GrpcPort = req.GrpcPort
	}
	if req.RestPort != "" {
		cfg.Server.RestPort = req.RestPort
	}
	if req.RedisMode != "" {
		cfg.Network.Pools.Redis.Mode = req.RedisMode
	}
	if req.RedisAddr != "" {
		cfg.Network.Pools.Redis.Addr = req.RedisAddr
	}
	if req.RedisPassword != "" {
		cfg.Network.Pools.Redis.Password = req.RedisPassword
	}
	if req.RedisDB != nil {
		cfg.Network.Pools.Redis.DB = *req.RedisDB
	}
	if req.RedisMasterName != "" {
		cfg.Network.Pools.Redis.MasterName = req.RedisMasterName
	}
	if len(req.SentinelAddrs) > 0 {
		cfg.Network.Pools.Redis.SentinelAddrs = req.SentinelAddrs
	}
	if len(req.ClusterAddrs) > 0 {
		cfg.Network.Pools.Redis.ClusterAddrs = req.ClusterAddrs
	}
	if req.MinioEndpoint != "" {
		cfg.Network.Pools.MinIO.Endpoint = req.MinioEndpoint
	}
	if req.MinioAccessKeyID != "" {
		cfg.Network.Pools.MinIO.AccessKeyID = req.MinioAccessKeyID
	}
	if req.MinioSecretAccessKey != "" {
		cfg.Network.Pools.MinIO.SecretAccessKey = req.MinioSecretAccessKey
	}
	if req.MinioUseSSL != nil {
		cfg.Network.Pools.MinIO.UseSSL = *req.MinioUseSSL
	}
	if req.MinioRegion != "" {
		cfg.Network.Pools.MinIO.Region = req.MinioRegion
	}
	if req.MinioBucket != "" {
		cfg.Network.Pools.MinIO.Bucket = req.MinioBucket
	}
	if req.CoreEndpoint != "" {
		cfg.Dashboard.CoreEndpoint = req.CoreEndpoint
	}
	if req.DashboardPort != "" {
		cfg.Dashboard.Port = req.DashboardPort
	}
	if req.LogLevel != "" {
		cfg.Log.Level = req.LogLevel
	}
	if req.LogFormat != "" {
		cfg.Log.Format = req.LogFormat
	}
	if req.LogOutput != "" {
		cfg.Log.Output = req.LogOutput
	}
	if req.LogFilename != "" {
		cfg.Log.Filename = req.LogFilename
	}
	if req.BufferSize != nil {
		cfg.Buffer.BufferSize = *req.BufferSize
	}
	if req.FlushInterval != "" {
		if d, err := time.ParseDuration(req.FlushInterval); err == nil {
			cfg.Buffer.FlushInterval = d
		}
	}
	s.cfgMu.Unlock()

	// Generate YAML snippet for manual config update
	snippet := "# Configuration updated in memory. Add to config.yaml:\n"
	if req.RedisMode != "" || req.RedisAddr != "" {
		snippet += "network:\n  pools:\n    redis:\n"
		if req.RedisMode != "" {
			snippet += fmt.Sprintf("      mode: %s\n", req.RedisMode)
		}
		if req.RedisAddr != "" {
			snippet += fmt.Sprintf("      addr: %s\n", req.RedisAddr)
		}
	}

	c.JSON(http.StatusOK, &model.ConfigUpdateResult{
		ConfigSnippet:   snippet,
		RestartRequired: true,
		Message:         "Configuration updated in memory. Restart required for persistence.",
	})
}

func (s *Server) enableDistributed(c *gin.Context) {
	var req model.EnableDistributedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update config in memory
	s.cfgMu.Lock()
	cfg := s.cfg
	cfg.Network.Pools.Redis.Mode = req.RedisMode
	cfg.Network.Pools.Redis.Addr = req.RedisAddr
	cfg.Network.Pools.Redis.Password = req.RedisPassword
	cfg.Network.Pools.Redis.DB = req.RedisDB
	cfg.Network.Pools.Redis.MasterName = req.MasterName
	cfg.Network.Pools.Redis.SentinelAddrs = req.SentinelAddrs
	cfg.Network.Pools.Redis.ClusterAddrs = req.ClusterAddrs
	s.cfgMu.Unlock()

	// Generate YAML snippet
	snippet := fmt.Sprintf(`# Add to config.yaml and restart:
network:
  pools:
    redis:
      mode: %s
      addr: %s
`, req.RedisMode, req.RedisAddr)

	if req.RedisMode == "sentinel" && len(req.SentinelAddrs) > 0 {
		snippet += "      sentinel_addrs:\n"
		for _, addr := range req.SentinelAddrs {
			snippet += fmt.Sprintf("        - %s\n", addr)
		}
	}

	c.JSON(http.StatusOK, &model.EnableDistributedResult{
		ConfigSnippet:   snippet,
		RestartRequired: true,
		Message:         "Distributed mode enabled. Restart required.",
	})
}

func (s *Server) clusterConfig(c *gin.Context) {
	s.cfgMu.RLock()
	cfg := s.cfg
	c.JSON(http.StatusOK, gin.H{
		"node_id":        cfg.Server.NodeID,
		"grpc_port":      cfg.Server.GrpcPort,
		"rest_port":      cfg.Server.RestPort,
		"redis_mode":     cfg.Network.Pools.Redis.Mode,
		"minio_endpoint": cfg.Network.Pools.MinIO.Endpoint,
		"log_level":      cfg.Log.Level,
		"buffer_size":    cfg.Buffer.BufferSize,
		"mode":           s.getMode(),
	})
	s.cfgMu.RUnlock()
}

func (s *Server) getNode(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	st, err := s.svc.GetStatus(ctx, &miniodbv1.GetStatusRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, n := range st.Nodes {
		if n.Id == id {
			c.JSON(http.StatusOK, &model.NodeResult{
				ID:       n.Id,
				Address:  n.Address,
				Port:     "",
				Status:   n.Status,
				LastSeen: time.Unix(n.LastSeen, 0),
				Metadata: map[string]string{"type": n.Type},
			})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
}

func (s *Server) listNodes(c *gin.Context) {
	ctx := c.Request.Context()

	st, err := s.svc.GetStatus(ctx, &miniodbv1.GetStatusRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var nodes []*model.NodeResult
	for _, n := range st.Nodes {
		nodes = append(nodes, &model.NodeResult{
			ID:       n.Id,
			Address:  n.Address,
			Port:     "",
			Status:   n.Status,
			LastSeen: time.Unix(n.LastSeen, 0),
			Metadata: map[string]string{"type": n.Type},
		})
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "total": len(nodes)})
}

func (s *Server) listTables(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := s.svc.ListTables(ctx, &miniodbv1.ListTablesRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var tables []*model.TableResult
	for _, t := range resp.Tables {
		var createdAt time.Time
		if t.CreatedAt != nil {
			createdAt = t.CreatedAt.AsTime()
		}
		tables = append(tables, &model.TableResult{
			Name:      t.Name,
			CreatedAt: createdAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"tables": tables, "total": len(tables)})
}

func (s *Server) getTable(c *gin.Context) {
	name := c.Param("name")
	ctx := c.Request.Context()

	resp, err := s.svc.GetTable(ctx, &miniodbv1.GetTableRequest{TableName: name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if resp.TableInfo == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Table not found"})
		return
	}

	t := resp.TableInfo
	var createdAt time.Time
	if t.CreatedAt != nil {
		createdAt = t.CreatedAt.AsTime()
	}

	result := &model.TableDetailResult{
		TableResult: model.TableResult{
			Name:      t.Name,
			CreatedAt: createdAt,
		},
	}

	if t.Config != nil {
		result.Config = config.TableConfig{
			BufferSize:     int(t.Config.BufferSize),
			FlushInterval:  time.Duration(t.Config.FlushIntervalSeconds) * time.Second,
			RetentionDays:  int(t.Config.RetentionDays),
			BackupEnabled:  t.Config.BackupEnabled,
			IDStrategy:     t.Config.IdStrategy,
			IDPrefix:       t.Config.IdPrefix,
			AutoGenerateID: t.Config.AutoGenerateId,
		}
		result.BufferSize = int(t.Config.BufferSize)
		result.FlushInterval = int64(t.Config.FlushIntervalSeconds)
		result.RetentionDays = int(t.Config.RetentionDays)
		result.BackupEnabled = t.Config.BackupEnabled
		result.IDStrategy = t.Config.IdStrategy
	}

	if t.Stats != nil {
		result.RowCountEst = t.Stats.RecordCount
		result.SizeBytes = t.Stats.SizeBytes
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) createTable(c *gin.Context) {
	var req model.CreateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	protoReq := &miniodbv1.CreateTableRequest{
		TableName:   req.TableName,
		IfNotExists: true,
	}
	if req.Config.BufferSize > 0 || req.Config.RetentionDays > 0 {
		protoReq.Config = &miniodbv1.TableConfig{
			BufferSize:           int32(req.Config.BufferSize),
			FlushIntervalSeconds: int32(req.Config.FlushInterval.Seconds()),
			RetentionDays:        int32(req.Config.RetentionDays),
			BackupEnabled:        req.Config.BackupEnabled,
			IdStrategy:           req.Config.IDStrategy,
			IdPrefix:             req.Config.IDPrefix,
			AutoGenerateId:       req.Config.AutoGenerateID,
		}
	}

	resp, err := s.svc.CreateTable(ctx, protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Table created"})
}

func (s *Server) updateTable(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "Table config update is not yet supported"})
}

func (s *Server) deleteTable(c *gin.Context) {
	name := c.Param("name")
	ctx := c.Request.Context()

	resp, err := s.svc.DeleteTable(ctx, &miniodbv1.DeleteTableRequest{
		TableName: name,
		IfExists:  true,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Table deleted"})
}

func (s *Server) tableStats(c *gin.Context) {
	name := c.Param("name")
	ctx := c.Request.Context()

	resp, err := s.svc.GetTable(ctx, &miniodbv1.GetTableRequest{TableName: name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if resp.TableInfo == nil || resp.TableInfo.Stats == nil {
		c.JSON(http.StatusOK, gin.H{"name": name, "columns": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"name":         name,
		"record_count": resp.TableInfo.Stats.RecordCount,
		"file_count":   resp.TableInfo.Stats.FileCount,
		"size_bytes":   resp.TableInfo.Stats.SizeBytes,
	})
}

func (s *Server) browseData(c *gin.Context) {
	name := c.Param("name")
	var params model.BrowseParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if params.Filter != "" {
		if !isSafeFilter(params.Filter) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Filter contains disallowed SQL keywords"})
			return
		}
	}

	if params.SortBy != "" {
		if !validIdentifier.MatchString(params.SortBy) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sort_by: must be a valid column name"})
			return
		}
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	ctx := c.Request.Context()

	var total int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) AS cnt FROM `%s`", name)
	if params.Filter != "" {
		countSQL += fmt.Sprintf(" WHERE %s", params.Filter)
	}
	countResp, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{Sql: countSQL})
	if err == nil && countResp.ResultJson != "" {
		var countRows []map[string]interface{}
		if json.Unmarshal([]byte(countResp.ResultJson), &countRows) == nil && len(countRows) > 0 {
			for _, key := range []string{"cnt", "count_star()", "count(*)"} {
				if v, ok := countRows[0][key]; ok {
					if val, ok := v.(float64); ok {
						total = int64(val)
					}
					break
				}
			}
		}
	}

	sql := fmt.Sprintf("SELECT * FROM `%s`", name)
	if params.Filter != "" {
		sql += fmt.Sprintf(" WHERE %s", params.Filter)
	}
	if params.SortBy != "" {
		order := "ASC"
		if strings.ToUpper(params.SortOrder) == "DESC" {
			order = "DESC"
		}
		sql += fmt.Sprintf(" ORDER BY `%s` %s", params.SortBy, order)
	}
	sql += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)

	resp, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{Sql: sql})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var rows []map[string]interface{}
	if resp.ResultJson != "" {
		if err := json.Unmarshal([]byte(resp.ResultJson), &rows); err != nil {
			s.logger.Warn("browseData: failed to parse ResultJson", zap.String("table", name), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse result: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, &model.BrowseResult{
		Rows:     rows,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (s *Server) writeRecord(c *gin.Context) {
	name := c.Param("name")
	var req model.WriteRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Convert payload to protobuf Struct
	payload, err := structpb.NewStruct(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid payload: %v", err)})
		return
	}

	var ts *timestamppb.Timestamp
	if req.Timestamp > 0 {
		ts = timestamppb.New(time.UnixMilli(req.Timestamp))
	} else {
		ts = timestamppb.Now()
	}

	protoReq := &miniodbv1.WriteDataRequest{
		Table: name,
		Data: &miniodbv1.DataRecord{
			Id:        req.ID,
			Timestamp: ts,
			Payload:   payload,
		},
	}

	resp, err := s.svc.WriteData(ctx, protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Record created"})
}

func (s *Server) writeBatch(c *gin.Context) {
	name := c.Param("name")
	var records []*model.WriteRecordRequest
	if err := c.ShouldBindJSON(&records); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	for _, req := range records {
		payload, err := structpb.NewStruct(req.Payload)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid payload: %v", err)})
			return
		}

		var ts *timestamppb.Timestamp
		if req.Timestamp > 0 {
			ts = timestamppb.New(time.UnixMilli(req.Timestamp))
		} else {
			ts = timestamppb.Now()
		}

		protoReq := &miniodbv1.WriteDataRequest{
			Table: name,
			Data: &miniodbv1.DataRecord{
				Id:        req.ID,
				Timestamp: ts,
				Payload:   payload,
			},
		}

		resp, err := s.svc.WriteData(ctx, protoReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !resp.Success {
			c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Records created"})
}

func (s *Server) updateRecord(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")
	var req model.UpdateRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	payload, err := structpb.NewStruct(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid payload: %v", err)})
		return
	}

	var ts *timestamppb.Timestamp
	if req.Timestamp > 0 {
		ts = timestamppb.New(time.UnixMilli(req.Timestamp))
	} else {
		ts = timestamppb.Now()
	}

	protoReq := &miniodbv1.UpdateDataRequest{
		Table:     name,
		Id:        id,
		Payload:   payload,
		Timestamp: ts,
	}

	resp, err := s.svc.UpdateData(ctx, protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Record updated"})
}

func (s *Server) deleteRecord(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")
	day := c.DefaultQuery("day", "")
	_ = day // not used in proto

	ctx := c.Request.Context()

	protoReq := &miniodbv1.DeleteDataRequest{
		Table: name,
		Id:    id,
	}

	resp, err := s.svc.DeleteData(ctx, protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Record deleted"})
}

func (s *Server) querySQL(c *gin.Context) {
	var req struct {
		SQL string `json:"sql" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	resp, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{Sql: req.SQL})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Parse JSON result
	var rows []map[string]interface{}
	var columns []string
	if resp.ResultJson != "" {
		if err := json.Unmarshal([]byte(resp.ResultJson), &rows); err == nil && len(rows) > 0 {
			for k := range rows[0] {
				columns = append(columns, k)
			}
		}
	}

	c.JSON(http.StatusOK, &model.QueryResult{
		Columns:    columns,
		Rows:       rows,
		Total:      int64(len(rows)),
		DurationMs: 0,
	})
}

func (s *Server) queryLogs(c *gin.Context) {
	// Log query is not implemented in service, return empty
	c.JSON(http.StatusOK, gin.H{
		"logs":     []interface{}{},
		"total":    0,
		"page":     1,
		"pageSize": 20,
	})
}

func (s *Server) streamLogs(c *gin.Context) {
	sse.ServeSSE(c, s.hub, []string{"logs"})
}

func (s *Server) listLogFiles(c *gin.Context) {
	// Log file listing is not implemented, return empty
	c.JSON(http.StatusOK, gin.H{"files": []interface{}{}})
}

func (s *Server) listBackups(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := s.svc.ListBackups(ctx, &miniodbv1.ListBackupsRequest{Days: 30})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var backups []*model.BackupResult
	for _, b := range resp.Backups {
		var timestamp, completedAt time.Time
		if b.Timestamp != nil {
			timestamp = b.Timestamp.AsTime()
		}
		if b.LastModified != nil {
			completedAt = b.LastModified.AsTime()
		}
		backups = append(backups, &model.BackupResult{
			ID:          b.ObjectName,
			Type:        "metadata",
			Status:      "completed",
			SizeBytes:   b.Size,
			CreatedAt:   timestamp,
			CompletedAt: completedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

func (s *Server) triggerMetadataBackup(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := s.svc.BackupMetadata(ctx, &miniodbv1.BackupMetadataRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   resp.Message,
		"backup_id": resp.BackupId,
	})
}

func (s *Server) getBackupSchedule(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := s.svc.GetMetadataStatus(ctx, &miniodbv1.GetMetadataStatusRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var lastBackup time.Time
	if resp.LastBackup != nil {
		lastBackup = resp.LastBackup.AsTime()
	}

	c.JSON(http.StatusOK, gin.H{
		"last_backup":     lastBackup,
		"tables_count":    0,
		"backup_enabled":  resp.BackupStatus["status"] == "enabled",
		"backup_interval": 3600,
	})
}

func (s *Server) monitorOverview(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := s.svc.GetMetrics(ctx, &miniodbv1.GetMetricsRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	overview := &model.MonitorOverviewResult{
		Goroutines:  runtime.NumGoroutine(),
		MemAllocMB:  float64(memStats.Alloc) / 1024 / 1024,
		GCPauseMs:   float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6,
		CPUPercent:  0,
		UptimeHours: 0,
	}

	if resp.SystemInfo != nil {
		if uptime, ok := resp.SystemInfo["uptime_seconds"]; ok {
			if secs, err := time.ParseDuration(uptime + "s"); err == nil {
				overview.UptimeHours = secs.Hours()
			}
		}
	}

	c.JSON(http.StatusOK, overview)
}

func (s *Server) monitorStream(c *gin.Context) {
	sse.ServeSSE(c, s.hub, []string{"metrics"})
}

func (s *Server) slaMetrics(c *gin.Context) {
	ctx := c.Request.Context()
	resp, err := s.svc.GetMetrics(ctx, &miniodbv1.GetMetricsRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	avgQueryMs := resp.PerformanceMetrics["avg_query_time_seconds"] * 1000
	slowestQueryMs := resp.PerformanceMetrics["slowest_query_seconds"] * 1000
	avgFlushMs := resp.PerformanceMetrics["avg_flush_time_seconds"] * 1000

	sla := &model.SLAResult{
		QueryLatencyP50Ms: avgQueryMs,
		QueryLatencyP95Ms: slowestQueryMs,
		QueryLatencyP99Ms: slowestQueryMs,
		WriteLatencyP95Ms: avgFlushMs,
		CacheHitRate:      resp.PerformanceMetrics["cache_hit_rate"],
		ErrorRate:         1 - resp.PerformanceMetrics["query_success_rate"],
	}

	c.JSON(http.StatusOK, sla)
}

func (s *Server) analyticsQuery(c *gin.Context) {
	var req struct {
		Query string `json:"query" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	resp, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{Sql: req.Query})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Parse JSON result
	var rows []map[string]interface{}
	var columns []string
	if resp.ResultJson != "" {
		if err := json.Unmarshal([]byte(resp.ResultJson), &rows); err == nil && len(rows) > 0 {
			for k := range rows[0] {
				columns = append(columns, k)
			}
		}
	}

	c.JSON(http.StatusOK, &model.AnalyticsQueryResult{
		Columns:    columns,
		Rows:       rows,
		Total:      int64(len(rows)),
		DurationMs: 0,
	})
}

func (s *Server) analyticsOverview(c *gin.Context) {
	// Return empty analytics overview
	c.JSON(http.StatusOK, gin.H{
		"write_trend": []interface{}{},
		"query_trend": []interface{}{},
		"data_volume": []interface{}{},
	})
}

func (s *Server) Start(ctx context.Context) {
	s.startMetricsPush(ctx)
}

func (s *Server) startMetricsPush(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				resp, err := s.svc.GetMetrics(ctx, &miniodbv1.GetMetricsRequest{})
				if err == nil {
					var memStats runtime.MemStats
					runtime.ReadMemStats(&memStats)
					s.hub.Publish("metrics", gin.H{
						"goroutines":   runtime.NumGoroutine(),
						"mem_alloc_mb": float64(memStats.Alloc) / 1024 / 1024,
						"uptime_hours": resp.SystemInfo["uptime_seconds"],
						"cpu_percent":  0,
					})
				}
			}
		}
	}()
}

func (s *Server) Stop() {
	if s.stopStats != nil {
		s.stopStats()
	}
	s.hub.Close()
}
