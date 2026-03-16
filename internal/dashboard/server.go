//go:build dashboard

package dashboard

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/internal/dashboard/client"
	"minIODB/internal/dashboard/model"
	"minIODB/internal/dashboard/sse"
	"minIODB/internal/security"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// statsRefreshInterval controls how often the background stats collector runs.
// Heavy operations (COUNT(*) per table) only happen in this goroutine — never
// in the hot request path.
const statsRefreshInterval = 5 * time.Minute

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

// Server is the Dashboard HTTP server: serves the Next.js SPA and the Dashboard API.
type Server struct {
	client      client.CoreClient
	cfg         *config.Config
	logger      *zap.Logger
	authManager *security.AuthManager
	hub         *sse.Hub
	stats       *statsCache
	stopStats   context.CancelFunc
}

// NewServer creates a new Dashboard server and starts the background stats collector.
func NewServer(coreClient client.CoreClient, cfg *config.Config, logger *zap.Logger) (*Server, error) {
	authCfg := &security.AuthConfig{
		Mode:            "token",
		TokenExpiration: 24 * time.Hour,
		Issuer:          "miniodb-dashboard",
		Audience:        "miniodb-dashboard-api",
		// key 作为 JWT payload user_id，secret 作为该用户的 JWT 签名密钥
		APIKeyPairs: convertAPIKeyPairs(cfg.Auth.APIKeyPairs),
	}

	authManager, err := security.NewAuthManager(authCfg)
	if err != nil {
		return nil, err
	}

	hub := sse.NewHub()
	statsCtx, cancel := context.WithCancel(context.Background())

	srv := &Server{
		client:      coreClient,
		cfg:         cfg,
		logger:      logger,
		authManager: authManager,
		hub:         hub,
		stats:       &statsCache{},
		stopStats:   cancel,
	}

	// Kick off the background stats collector.
	// First refresh runs immediately (async) so the cache is warm within seconds.
	go srv.runStatsCollector(statsCtx)

	return srv, nil
}

// runStatsCollector runs a periodic stats refresh in the background.
// It does NOT run in the hot request path; HTTP handlers only read from the cache.
func (s *Server) runStatsCollector(ctx context.Context) {
	// Refresh immediately, then on every tick.
	s.refreshStats(ctx)
	ticker := time.NewTicker(statsRefreshInterval)
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
	tables, err := s.client.ListTables(ctx)
	tablesCount := 0
	if err == nil {
		tablesCount = len(tables)
	}

	// --- total records (COUNT(*) per table, concurrent) ----------------------
	var totalRecords int64
	if len(tables) > 0 {
		type countVal struct{ n int64 }
		ch := make(chan countVal, len(tables))
		for _, t := range tables {
			go func(name string) {
				qr, err := s.client.QuerySQL(ctx,
					fmt.Sprintf("SELECT COUNT(*) AS cnt FROM `%s`", name))
				if err != nil || len(qr.Rows) == 0 {
					ch <- countVal{0}
					return
				}
				var n int64
				for _, key := range []string{"cnt", "count_star()", "count(*)"} {
					if v, ok := qr.Rows[0][key]; ok {
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
		for range tables {
			totalRecords += (<-ch).n
		}
	}

	// --- pending writes (from status endpoint) --------------------------------
	var pendingWrites int64
	var nodesCount int
	if st, err := s.client.GetStatus(ctx); err == nil && st != nil {
		pendingWrites = st.PendingWrites
	}
	if nodes, err := s.client.GetNodes(ctx); err == nil {
		nodesCount = len(nodes)
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

func convertAPIKeyPairs(pairs []config.APIKeyPair) []security.APIKeyPair {
	result := make([]security.APIKeyPair, len(pairs))
	for i, p := range pairs {
		result[i] = security.APIKeyPair{
			Key:         p.Key,
			Secret:      p.Secret,
			Role:        p.Role,
			DisplayName: p.DisplayName,
		}
	}
	return result
}

// MountRoutes registers all dashboard routes on the given gin group.
// The group should correspond to cfg.Dashboard.BasePath (e.g. "/dashboard").
func (s *Server) MountRoutes(group *gin.RouterGroup) {
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		s.logger.Error("dashboard: failed to load embedded static files", zap.Error(err))
	} else {
		group.StaticFS("/ui", http.FS(staticSub))
	}

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
	APISecret string `json:"api_secret" binding:"required"` // 对应 config.yaml auth.api_key_pairs 中的 secret
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
			"error": "无效的凭证，请检查 config.yaml 中 auth.api_key_pairs 配置",
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
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
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

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
}

func (s *Server) changePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

	_ = userClaims.UserID

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully. Please login again with new credentials."})
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
	result, err := s.client.GetHealth(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
			"version":   "",
			"node_id":   "",
			"mode":      "",
		})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) clusterInfo(c *gin.Context) {
	ctx := c.Request.Context()

	// GetHealth is a lightweight ping — do it live so the status badge is fresh.
	health, err := s.client.GetHealth(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Uptime is read from the status endpoint; it's cheap (one HTTP call).
	var uptime int64
	if st, err := s.client.GetStatus(ctx); err == nil && st != nil {
		uptime = st.Uptime
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
		"status":         health.Status,
		"version":        health.Version,
		"node_id":        health.NodeID,
		"mode":           health.Mode,
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

	nodes, err := s.client.GetNodes(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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

	// Ask the client for the actual runtime mode so we don't accidentally show
	// the Redis coordinator when Redis is configured only for caching (standalone
	// cluster mode) but service-discovery is not active.
	isDistributed := false
	if health, herr := s.client.GetHealth(ctx); herr == nil {
		isDistributed = health.Mode == "distributed"
	}

	// Inject a virtual Redis coordinator whenever the cluster is actually running
	// in distributed mode.  This correctly reflects the architecture even for a
	// single-node distributed deployment (e.g. during initial scale-up).
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
	full, err := s.client.GetFullConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, full)
}

func (s *Server) updateClusterConfig(c *gin.Context) {
	var req model.ConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := s.client.UpdateConfig(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) enableDistributed(c *gin.Context) {
	var req model.EnableDistributedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := s.client.EnableDistributedMode(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) clusterConfig(c *gin.Context) {
	cfg, err := s.client.GetClusterConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

func (s *Server) getNode(c *gin.Context) {
	id := c.Param("id")
	nodes, err := s.client.GetNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, n := range nodes {
		if n.ID == id {
			c.JSON(http.StatusOK, n)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
}

func (s *Server) listNodes(c *gin.Context) {
	nodes, err := s.client.GetNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "total": len(nodes)})
}

func (s *Server) listTables(c *gin.Context) {
	tables, err := s.client.ListTables(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tables": tables, "total": len(tables)})
}

func (s *Server) getTable(c *gin.Context) {
	name := c.Param("name")
	table, err := s.client.GetTable(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, table)
}

func (s *Server) createTable(c *gin.Context) {
	var req model.CreateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.client.CreateTable(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Table created"})
}

func (s *Server) updateTable(c *gin.Context) {
	name := c.Param("name")
	var req model.UpdateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.client.UpdateTable(c.Request.Context(), name, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Table updated"})
}

func (s *Server) deleteTable(c *gin.Context) {
	name := c.Param("name")
	if err := s.client.DeleteTable(c.Request.Context(), name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Table deleted"})
}

func (s *Server) tableStats(c *gin.Context) {
	name := c.Param("name")
	stats, err := s.client.GetTableStats(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (s *Server) browseData(c *gin.Context) {
	name := c.Param("name")
	var params model.BrowseParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := s.client.BrowseData(c.Request.Context(), name, &params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) writeRecord(c *gin.Context) {
	name := c.Param("name")
	var req model.WriteRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.client.WriteRecord(c.Request.Context(), name, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	if err := s.client.WriteBatch(c.Request.Context(), name, records); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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
	if err := s.client.UpdateRecord(c.Request.Context(), name, id, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Record updated"})
}

func (s *Server) deleteRecord(c *gin.Context) {
	name := c.Param("name")
	id := c.Param("id")
	day := c.DefaultQuery("day", "")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	if err := s.client.DeleteRecord(c.Request.Context(), name, id, day); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	result, err := s.client.QuerySQL(c.Request.Context(), req.SQL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) queryLogs(c *gin.Context) {
	var params model.LogQueryParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := s.client.QueryLogs(c.Request.Context(), &params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) streamLogs(c *gin.Context) {
	sse.ServeSSE(c, s.hub, []string{"logs"})
}

func (s *Server) listLogFiles(c *gin.Context) {
	files, err := s.client.ListLogFiles(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (s *Server) listBackups(c *gin.Context) {
	backups, err := s.client.ListBackups(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

func (s *Server) triggerMetadataBackup(c *gin.Context) {
	result, err := s.client.TriggerMetadataBackup(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) getBackupSchedule(c *gin.Context) {
	status, err := s.client.GetMetadataStatus(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

func (s *Server) monitorOverview(c *gin.Context) {
	overview, err := s.client.GetMonitorOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, overview)
}

func (s *Server) monitorStream(c *gin.Context) {
	sse.ServeSSE(c, s.hub, []string{"metrics"})
}

func (s *Server) slaMetrics(c *gin.Context) {
	sla, err := s.client.GetSLA(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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
	result, err := s.client.AnalyticsQuery(c.Request.Context(), req.Query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) analyticsOverview(c *gin.Context) {
	overview, err := s.client.GetAnalyticsOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, overview)
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
				overview, err := s.client.GetMonitorOverview(ctx)
				if err == nil {
					s.hub.Publish("metrics", overview)
				}
			}
		}
	}()
}
