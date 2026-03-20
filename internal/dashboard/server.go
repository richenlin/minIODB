package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	miniodbv1 "minIODB/api/proto/miniodb/v1"
	"minIODB/config"
	"minIODB/internal/backup"
	"minIODB/internal/dashboard/logbuffer"
	"minIODB/internal/dashboard/model"
	"minIODB/internal/dashboard/sse"
	"minIODB/internal/metrics"
	"minIODB/internal/replication"
	"minIODB/internal/security"
	"minIODB/internal/service"
	"minIODB/internal/storage"
	"minIODB/pkg/version"
)

// statsRefreshInterval is configured per-instance via DashboardConfig.MetricsScrapeInterval.
// The minimum is 1 minute (to avoid overwhelming the DB with COUNT(*) queries);
// values below that threshold fall back to 5 minutes.

type metricsSnapshot struct {
	Timestamp    int64
	WriteCount   int64
	QueryCount   int64
	TotalRecords int64
}

// statsCache holds pre-computed cluster statistics.
// All fields are protected by mu; reads return the last-known-good value
// immediately, even while a refresh is in progress.
type statsCache struct {
	mu              sync.RWMutex
	totalRecords    int64
	pendingWrites   int64
	tablesCount     int
	nodesCount      int
	refreshedAt     time.Time
	snapshots       []metricsSnapshot
	snapshotHead    int
	snapshotCount   int
	snapshotMu      sync.RWMutex
	prevTotalRecord int64
	prevQueryCount  int64
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
type ReplicatorStatsProvider interface {
	GetStats() replication.ReplicatorStats
	IsRunning() bool
}

type Server struct {
	svc                  *service.MinIODBService
	cfg                  *config.Config
	cfgMu                sync.RWMutex
	depsMu               sync.RWMutex
	logger               *zap.Logger
	authManager          *security.AuthManager
	hub                  *sse.Hub
	logBuffer            *logbuffer.LogBuffer
	stats                *statsCache
	stopStats            context.CancelFunc
	statsRefreshInterval time.Duration
	slaMonitor           *metrics.SLAMonitor
	backupScheduler      interface {
		ListPlans() []*config.BackupSchedule
		AddPlan(plan *config.BackupSchedule) error
		AddPlanIfAbsent(plan *config.BackupSchedule) error
		RemovePlan(id string) error
		TriggerExecution(id string) error
	}
	backupStore interface {
		ListExecutions(ctx context.Context, planID string, limit int) ([]*backup.BackupExecution, error)
	}
	replicator ReplicatorStatsProvider
}

// getMinIOClient returns a MinIO client for backup bucket operations (uses backup pool config when set).
// Caller must not cache the returned client if config can change.
func (s *Server) getMinIOClient() *minio.Client {
	s.cfgMu.RLock()
	cfg := s.cfg
	s.cfgMu.RUnlock()
	if cfg == nil {
		return nil
	}
	// Use backup MinIO config for backup object operations
	minioCfg := cfg.GetBackupMinIO()
	if minioCfg.Endpoint == "" {
		minioCfg = cfg.GetMinIO()
	}
	wrapper, err := storage.NewMinioClientWrapper(minioCfg, s.logger)
	if err != nil {
		return nil
	}
	return wrapper.GetClient()
}

// NewServer creates a new Dashboard server and starts the background stats collector.
func NewServer(svc *service.MinIODBService, cfg *config.Config, logger *zap.Logger, authManager *security.AuthManager, logBuffer *logbuffer.LogBuffer) (*Server, error) {
	hub := sse.NewHub()
	statsCtx, cancel := context.WithCancel(context.Background())

	statsInterval := cfg.Dashboard.MetricsScrapeInterval
	if statsInterval < 1*time.Minute {
		statsInterval = 5 * time.Minute
	}

	slaMonitor := metrics.NewSLAMonitor(metrics.DefaultSLAConfig(), logger)

	srv := &Server{
		svc:                  svc,
		cfg:                  cfg,
		logger:               logger,
		authManager:          authManager,
		hub:                  hub,
		logBuffer:            logBuffer,
		stats:                &statsCache{snapshots: make([]metricsSnapshot, 288)},
		stopStats:            cancel,
		statsRefreshInterval: statsInterval,
		slaMonitor:           slaMonitor,
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
				quoted := security.DefaultSanitizer.QuoteIdentifier(name)
				qr, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{
					Sql: fmt.Sprintf("SELECT COUNT(*) AS cnt FROM %s", quoted),
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

	// --- get query count from metrics -----------------------------------------
	var currentQueryCount int64
	if metricsResp, err := s.svc.GetMetrics(ctx, &miniodbv1.GetMetricsRequest{}); err == nil && metricsResp != nil {
		if v, ok := metricsResp.PerformanceMetrics["total_queries"]; ok {
			currentQueryCount = int64(v)
		}
	}

	// --- atomically update the cache (all diff calculations under mu) ---------
	var writeDiff, queryDiff int64
	s.stats.mu.Lock()
	prevTotal := s.stats.prevTotalRecord
	writeDiff = totalRecords - prevTotal
	if writeDiff < 0 {
		writeDiff = 0
	}
	s.stats.prevTotalRecord = totalRecords

	prevQuery := s.stats.prevQueryCount
	queryDiff = currentQueryCount - prevQuery
	if queryDiff < 0 {
		queryDiff = 0
	}
	s.stats.prevQueryCount = currentQueryCount

	s.stats.totalRecords = totalRecords
	s.stats.pendingWrites = pendingWrites
	s.stats.tablesCount = tablesCount
	s.stats.nodesCount = nodesCount
	s.stats.refreshedAt = time.Now()
	s.stats.mu.Unlock()

	s.stats.snapshotMu.Lock()
	snapshot := metricsSnapshot{
		Timestamp:    time.Now().Unix(),
		WriteCount:   writeDiff,
		QueryCount:   queryDiff,
		TotalRecords: totalRecords,
	}
	capacity := len(s.stats.snapshots)
	s.stats.snapshots[s.stats.snapshotHead] = snapshot
	s.stats.snapshotHead = (s.stats.snapshotHead + 1) % capacity
	if s.stats.snapshotCount < capacity {
		s.stats.snapshotCount++
	}
	s.stats.snapshotMu.Unlock()

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

func (s *Server) isBackupAvailable() bool {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.Network.Pools.BackupMinIO != nil && s.cfg.Network.Pools.BackupMinIO.Endpoint != ""
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
		auth.GET("/backups/availability", s.getBackupAvailability)
		auth.GET("/backups/status", s.getBackupsStatus)
		auth.POST("/backups/metadata", s.triggerMetadataBackup)
		auth.GET("/backups/schedule", s.getBackupSchedule)
		auth.POST("/backups/full", s.triggerFullBackup)
		auth.POST("/backups/table/:name", s.triggerTableBackup)
		auth.POST("/backups/:id/restore", s.restoreBackup)
		auth.GET("/backups/:id/download", s.downloadBackup)
		auth.POST("/backups/:id/verify", s.verifyBackup)
		auth.PUT("/backups/schedule", s.updateBackupSchedule)
		auth.GET("/backups/:id", s.getBackup)
		auth.DELETE("/backups/:id", s.deleteBackup)

		auth.GET("/backups/plans", s.listBackupPlans)
		auth.POST("/backups/plans", s.createBackupPlan)
		auth.PUT("/backups/plans/:id", s.updateBackupPlan)
		auth.DELETE("/backups/plans/:id", s.deleteBackupPlan)
		auth.POST("/backups/plans/:id/trigger", s.triggerBackupPlan)
		auth.GET("/backups/plans/:id/executions", s.listBackupPlanExecutions)

		auth.GET("/hot-backup/status", s.getHotBackupStatus)
		auth.GET("/hot-backup/availability", s.getHotBackupAvailability)

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
// Supports both Authorization header and query parameter (for SSE).
func (s *Server) requireAuth(c *gin.Context) {
	token := ""

	// First try Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		token = strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			token = ""
		}
	}

	// Fallback to query parameter (for SSE which doesn't support custom headers)
	if token == "" {
		token = c.Query("token")
	}

	if token == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
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
	var backupEndpoint, backupAccessKey, backupSecret, backupRegion, backupBucket string
	var backupUseSSL bool
	if cfg.Network.Pools.BackupMinIO != nil {
		p := cfg.Network.Pools.BackupMinIO
		backupEndpoint, backupAccessKey, backupSecret = p.Endpoint, p.AccessKeyID, p.SecretAccessKey
		backupUseSSL, backupRegion, backupBucket = p.UseSSL, p.Region, p.Bucket
	}
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

		// network.pools.minio_backup
		MinioBackupEndpoint:        backupEndpoint,
		MinioBackupAccessKeyID:     backupAccessKey,
		MinioBackupSecretAccessKey: maskSecret(backupSecret),
		MinioBackupUseSSL:          backupUseSSL,
		MinioBackupRegion:          backupRegion,
		MinioBackupBucket:          backupBucket,

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
	// network.pools.minio_backup
	hasBackupMinIOReq := req.MinioBackupEndpoint != "" || req.MinioBackupAccessKeyID != "" || req.MinioBackupSecretAccessKey != "" ||
		req.MinioBackupUseSSL != nil || req.MinioBackupRegion != "" || req.MinioBackupBucket != ""
	if hasBackupMinIOReq {
		if cfg.Network.Pools.BackupMinIO == nil {
			cfg.Network.Pools.BackupMinIO = &config.EnhancedMinIOConfig{
				UseSSL: false,
				Region: "us-east-1",
			}
		}
		if req.MinioBackupEndpoint != "" {
			cfg.Network.Pools.BackupMinIO.Endpoint = req.MinioBackupEndpoint
		}
		if req.MinioBackupAccessKeyID != "" {
			cfg.Network.Pools.BackupMinIO.AccessKeyID = req.MinioBackupAccessKeyID
		}
		if req.MinioBackupSecretAccessKey != "" {
			cfg.Network.Pools.BackupMinIO.SecretAccessKey = req.MinioBackupSecretAccessKey
		}
		if req.MinioBackupUseSSL != nil {
			cfg.Network.Pools.BackupMinIO.UseSSL = *req.MinioBackupUseSSL
		}
		if req.MinioBackupRegion != "" {
			cfg.Network.Pools.BackupMinIO.Region = req.MinioBackupRegion
		}
		if req.MinioBackupBucket != "" {
			cfg.Network.Pools.BackupMinIO.Bucket = req.MinioBackupBucket
		}
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

	// Generate YAML snippet for manual config update (matches unified config: network.pools.*)
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
	hasMinio := req.MinioEndpoint != "" || req.MinioAccessKeyID != "" || req.MinioSecretAccessKey != "" ||
		req.MinioUseSSL != nil || req.MinioRegion != "" || req.MinioBucket != ""
	if hasMinio {
		if !strings.Contains(snippet, "network:") {
			snippet += "network:\n  pools:\n"
		} else if !strings.Contains(snippet, "pools:") {
			snippet += "  pools:\n"
		}
		if !strings.Contains(snippet, "minio:") {
			snippet += "    minio:\n"
		}
		if req.MinioEndpoint != "" {
			snippet += fmt.Sprintf("      endpoint: %s\n", req.MinioEndpoint)
		}
		if req.MinioAccessKeyID != "" {
			snippet += fmt.Sprintf("      access_key_id: %s\n", req.MinioAccessKeyID)
		}
		if req.MinioSecretAccessKey != "" {
			snippet += "      secret_access_key: <your-secret>\n"
		}
		if req.MinioUseSSL != nil {
			snippet += fmt.Sprintf("      use_ssl: %v\n", *req.MinioUseSSL)
		}
		if req.MinioRegion != "" {
			snippet += fmt.Sprintf("      region: %s\n", req.MinioRegion)
		}
		if req.MinioBucket != "" {
			snippet += fmt.Sprintf("      bucket: %s\n", req.MinioBucket)
		}
	}
	hasMinioBackup := req.MinioBackupEndpoint != "" || req.MinioBackupAccessKeyID != "" || req.MinioBackupSecretAccessKey != "" ||
		req.MinioBackupUseSSL != nil || req.MinioBackupRegion != "" || req.MinioBackupBucket != ""
	if hasMinioBackup {
		if !strings.Contains(snippet, "network:") {
			snippet += "network:\n  pools:\n"
		} else if !strings.Contains(snippet, "pools:") {
			snippet += "  pools:\n"
		}
		snippet += "    minio_backup:\n"
		if req.MinioBackupEndpoint != "" {
			snippet += fmt.Sprintf("      endpoint: %s\n", req.MinioBackupEndpoint)
		}
		if req.MinioBackupAccessKeyID != "" {
			snippet += fmt.Sprintf("      access_key_id: %s\n", req.MinioBackupAccessKeyID)
		}
		if req.MinioBackupSecretAccessKey != "" {
			snippet += "      secret_access_key: <your-secret>\n"
		}
		if req.MinioBackupUseSSL != nil {
			snippet += fmt.Sprintf("      use_ssl: %v\n", *req.MinioBackupUseSSL)
		}
		if req.MinioBackupRegion != "" {
			snippet += fmt.Sprintf("      region: %s\n", req.MinioBackupRegion)
		}
		if req.MinioBackupBucket != "" {
			snippet += fmt.Sprintf("      bucket: %s\n", req.MinioBackupBucket)
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
		backupEnabled := t.Config.BackupEnabled
		result.Config = config.TableConfig{
			BufferSize:     int(t.Config.BufferSize),
			FlushInterval:  time.Duration(t.Config.FlushIntervalSeconds) * time.Second,
			RetentionDays:  int(t.Config.RetentionDays),
			BackupEnabled:  &backupEnabled,
			IDStrategy:     t.Config.IdStrategy,
			IDPrefix:       t.Config.IdPrefix,
			AutoGenerateID: t.Config.AutoGenerateId,
		}
		result.BufferSize = int(t.Config.BufferSize)
		result.FlushInterval = int64(t.Config.FlushIntervalSeconds)
		result.RetentionDays = int(t.Config.RetentionDays)
		result.BackupEnabled = t.Config.BackupEnabled
		result.IDStrategy = t.Config.IdStrategy
		result.AutoGenerateID = t.Config.AutoGenerateId
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
	// 只要 Config 中有任何字段被设置，就构建 proto Config（避免仅设 IDStrategy 时丢失）
	hasConfig := req.Config.BufferSize > 0 ||
		req.Config.RetentionDays > 0 ||
		req.Config.IDStrategy != "" ||
		req.Config.IDPrefix != "" ||
		req.Config.FlushInterval > 0 ||
		req.Config.BackupEnabled != nil
	if hasConfig {
		var backupEnabled bool
		if req.Config.BackupEnabled != nil {
			backupEnabled = *req.Config.BackupEnabled
		}
		protoReq.Config = &miniodbv1.TableConfig{
			BufferSize:           int32(req.Config.BufferSize),
			FlushIntervalSeconds: int32(req.Config.FlushInterval.Seconds()),
			RetentionDays:        int32(req.Config.RetentionDays),
			BackupEnabled:        backupEnabled,
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
	name := c.Param("name")
	if !s.cfg.IsValidTableName(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table name"})
		return
	}
	var req model.UpdateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	tableManager := s.svc.GetTableManager()
	if tableManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Table manager not available"})
		return
	}

	if err := tableManager.UpdateTableConfig(ctx, name, &req.Config); err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "cannot modify immutable field") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Table config updated successfully"})
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
	quotedTable := security.DefaultSanitizer.QuoteIdentifier(name)
	baseWhere := ""
	if params.Filter != "" {
		baseWhere = fmt.Sprintf(" WHERE %s", params.Filter)
	}

	// Dashboard 数据浏览按 ID 展示最新版本（解决更新采用 append-only 带来的重复版本问题）
	countSQL := fmt.Sprintf(
		"SELECT COUNT(*) AS cnt FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY id ORDER BY timestamp DESC) AS _rn FROM %s%s) t WHERE _rn = 1",
		quotedTable,
		baseWhere,
	)
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

	innerSQL := fmt.Sprintf("SELECT *, ROW_NUMBER() OVER (PARTITION BY id ORDER BY timestamp DESC) AS _rn FROM %s%s", quotedTable, baseWhere)
	sql := fmt.Sprintf("SELECT * EXCLUDE (_rn) FROM (%s) t WHERE _rn = 1", innerSQL)
	if params.SortBy != "" {
		order := "ASC"
		if strings.ToUpper(params.SortOrder) == "DESC" {
			order = "DESC"
		}
		quotedSortBy := security.DefaultSanitizer.QuoteIdentifier(params.SortBy)
		sql += fmt.Sprintf(" ORDER BY %s %s", quotedSortBy, order)
	} else {
		sql += " ORDER BY timestamp DESC"
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
		for _, row := range rows {
			if raw, ok := row["payload"]; ok {
				if normalized, ok := normalizeDashboardPayloadField(raw); ok {
					row["payload"] = normalized
				}
			}
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

	normalizedPayload := normalizeIncomingPayload(req.Payload, name)

	// Convert payload to protobuf Struct
	payload, err := structpb.NewStruct(normalizedPayload)
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
		normalizedPayload := normalizeIncomingPayload(req.Payload, name)
		payload, err := structpb.NewStruct(normalizedPayload)
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

	normalizedPayload := normalizeIncomingPayload(req.Payload, name)
	payload, err := structpb.NewStruct(normalizedPayload)
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

// normalizeIncomingPayload 兼容 Dashboard 误传的 payload 包装格式
// 兼容格式: {"payload":"{\"aa\":2}","table":"teststats"}
func normalizeIncomingPayload(payload map[string]interface{}, tableName string) map[string]interface{} {
	if payload == nil {
		return map[string]interface{}{}
	}

	if len(payload) <= 2 {
		rawPayload, hasPayload := payload["payload"]
		rawTable, hasTable := payload["table"]
		if hasPayload && hasTable {
			if table, ok := rawTable.(string); ok && table == tableName {
				switch v := rawPayload.(type) {
				case string:
					var decoded map[string]interface{}
					if err := json.Unmarshal([]byte(v), &decoded); err == nil {
						return decoded
					}
				case map[string]interface{}:
					return v
				}
			}
		}
	}

	return payload
}

// normalizeDashboardPayloadField 将查询结果中的 payload 字符串反序列化为对象，便于前端编辑
func normalizeDashboardPayloadField(raw interface{}) (map[string]interface{}, bool) {
	s, ok := raw.(string)
	if !ok {
		return nil, false
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(s), &decoded); err != nil {
		return nil, false
	}

	return decoded, true
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
	start := time.Now()
	resp, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{Sql: req.SQL})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if s.slaMonitor != nil && err == nil {
		s.slaMonitor.RecordQueryLatency(time.Since(start))
	}

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
	params := model.LogQueryParams{
		Level:    c.Query("level"),
		Keyword:  c.Query("keyword"),
		Page:     1,
		PageSize: 100,
	}
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			params.Page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			params.PageSize = v
		}
	}
	if st := c.Query("start_time"); st != "" {
		if v, err := strconv.ParseInt(st, 10, 64); err == nil {
			params.StartTime = v
		}
	}
	if et := c.Query("end_time"); et != "" {
		if v, err := strconv.ParseInt(et, 10, 64); err == nil {
			params.EndTime = v
		}
	}

	result := s.logBuffer.Query(params)
	c.JSON(http.StatusOK, result)
}

func (s *Server) streamLogs(c *gin.Context) {
	sse.ServeSSE(c, s.hub, []string{"logs"})
}

func (s *Server) listLogFiles(c *gin.Context) {
	// Log file listing is not implemented, return empty
	c.JSON(http.StatusOK, gin.H{"files": []interface{}{}})
}

func (s *Server) listBackups(c *gin.Context) {
	degraded := !s.isBackupAvailable()
	if degraded {
		s.logger.Warn("listBackups: BackupMinIO not configured, operating in degraded mode")
	}
	ctx := c.Request.Context()
	resp, err := s.svc.ListBackups(ctx, &miniodbv1.ListBackupsRequest{Days: 30})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "degraded": degraded})
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
		backupType := "metadata"
		if strings.Contains(b.ObjectName, "/full-backup/") {
			backupType = "full"
		} else if strings.Contains(b.ObjectName, "/table-backup/") {
			backupType = "table"
		}
		backups = append(backups, &model.BackupResult{
			ID:          b.ObjectName,
			Type:        backupType,
			Status:      "completed",
			SizeBytes:   b.Size,
			CreatedAt:   timestamp,
			CompletedAt: completedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups, "degraded": degraded})
}

func (s *Server) triggerMetadataBackup(c *gin.Context) {
	degraded := !s.isBackupAvailable()
	if degraded {
		s.logger.Warn("triggerMetadataBackup: BackupMinIO not configured, operating in degraded mode")
	}
	ctx := c.Request.Context()
	resp, err := s.svc.BackupMetadata(ctx, &miniodbv1.BackupMetadataRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "degraded": degraded})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message, "degraded": degraded})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   resp.Message,
		"backup_id": resp.BackupId,
		"degraded":  degraded,
	})
}

func (s *Server) getBackupAvailability(c *gin.Context) {
	available := s.isBackupAvailable()
	message := "Backup service unavailable: BackupMinIO not configured"
	if available {
		message = "Backup service available"
	}
	c.JSON(http.StatusOK, gin.H{
		"available": available,
		"message":   message,
	})
}

func (s *Server) getBackupsStatus(c *gin.Context) {
	backupMinIOAvailable := s.isBackupAvailable()

	var degradedMode bool
	var degradedBucket string

	s.cfgMu.RLock()
	cfg := s.cfg
	s.cfgMu.RUnlock()

	if cfg != nil {
		if cfg.Network.Pools.BackupMinIO == nil || cfg.Network.Pools.BackupMinIO.Endpoint == "" {
			degradedMode = true
			degradedBucket = cfg.Network.Pools.MinIO.Bucket
		}
	}

	ctx := c.Request.Context()
	resp, err := s.svc.GetMetadataStatus(ctx, &miniodbv1.GetMetadataStatusRequest{})
	var lastMetadataBackup time.Time
	if err == nil && resp != nil && resp.LastBackup != nil {
		lastMetadataBackup = resp.LastBackup.AsTime()
	}

	metadataBackupEnabled := false
	if cfg != nil {
		metadataBackupEnabled = cfg.Backup.Metadata.Enabled
	}

	s.depsMu.RLock()
	hotReplicationRunning := s.replicator != nil && s.replicator.IsRunning()
	s.depsMu.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"backup_minio_available":  backupMinIOAvailable,
		"degraded_mode":           degradedMode,
		"degraded_bucket":         degradedBucket,
		"hot_replication_running": hotReplicationRunning,
		"metadata_backup_enabled": metadataBackupEnabled,
		"last_metadata_backup":    lastMetadataBackup,
	})
}

func (s *Server) getBackupSchedule(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
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

	s.cfgMu.RLock()
	backupInterval := s.cfg.Backup.Interval
	backupEnabled := s.cfg.Backup.Enabled
	metadataEnabled := s.cfg.Backup.Metadata.Enabled
	metadataInterval := s.cfg.Backup.Metadata.Interval
	s.cfgMu.RUnlock()

	tablesResp, err := s.svc.ListTables(ctx, &miniodbv1.ListTablesRequest{})
	tablesCount := 0
	if err == nil && tablesResp != nil {
		tablesCount = len(tablesResp.Tables)
	}

	c.JSON(http.StatusOK, gin.H{
		"last_backup":       lastBackup,
		"tables_count":      tablesCount,
		"backup_enabled":    backupEnabled,
		"backup_interval":   backupInterval,
		"metadata_enabled":  metadataEnabled,
		"metadata_interval": metadataInterval.Seconds(),
	})
}

func (s *Server) monitorOverview(c *gin.Context) {
	snap := metrics.GetRuntimeSnapshot()

	overview := &model.MonitorOverviewResult{
		Goroutines:  snap.Goroutines,
		MemAllocMB:  snap.HeapAllocMB,
		GCPauseMs:   snap.GCPauseMs,
		CPUPercent:  snap.CPUPercent,
		UptimeHours: snap.UptimeSeconds / 3600,
	}

	c.JSON(http.StatusOK, overview)
}

func (s *Server) monitorStream(c *gin.Context) {
	sse.ServeSSE(c, s.hub, []string{"metrics"})
}

func (s *Server) slaMetrics(c *gin.Context) {
	if s.slaMonitor != nil && s.slaMonitor.GetMetricsHistory().Count() > 0 {
		history := s.slaMonitor.GetMetricsHistory()
		sla := &model.SLAResult{
			QueryLatencyP50Ms: history.GetP50() * 1000,
			QueryLatencyP95Ms: history.GetP95() * 1000,
			QueryLatencyP99Ms: history.GetP99() * 1000,
		}
		c.JSON(http.StatusOK, sla)
		return
	}

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
	start := time.Now()
	resp, err := s.svc.QueryData(ctx, &miniodbv1.QueryDataRequest{Sql: req.Query})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if s.slaMonitor != nil && err == nil {
		s.slaMonitor.RecordQueryLatency(time.Since(start))
	}

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
	s.stats.snapshotMu.RLock()
	snapshots := make([]metricsSnapshot, s.stats.snapshotCount)
	if s.stats.snapshotCount > 0 {
		capacity := len(s.stats.snapshots)
		for i := 0; i < s.stats.snapshotCount; i++ {
			idx := (s.stats.snapshotHead - s.stats.snapshotCount + i + capacity) % capacity
			snapshots[i] = s.stats.snapshots[idx]
		}
	}
	s.stats.snapshotMu.RUnlock()

	writeTrend := make([]map[string]interface{}, len(snapshots))
	queryTrend := make([]map[string]interface{}, len(snapshots))
	dataVolume := make([]map[string]interface{}, len(snapshots))
	for i, snap := range snapshots {
		writeTrend[i] = map[string]interface{}{"timestamp": snap.Timestamp, "value": snap.WriteCount}
		queryTrend[i] = map[string]interface{}{"timestamp": snap.Timestamp, "value": snap.QueryCount}
		dataVolume[i] = map[string]interface{}{"timestamp": snap.Timestamp, "value": snap.TotalRecords}
	}
	c.JSON(http.StatusOK, gin.H{
		"write_trend": writeTrend,
		"query_trend": queryTrend,
		"data_volume": dataVolume,
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
					snap := metrics.GetRuntimeSnapshot()
					s.hub.Publish("metrics", gin.H{
						"goroutines":   snap.Goroutines,
						"mem_alloc_mb": snap.HeapAllocMB,
						"gc_pause_ms":  snap.GCPauseMs,
						"load_level":   snap.LoadLevel,
						"uptime_hours": resp.SystemInfo["uptime_seconds"],
						"cpu_percent":  snap.CPUPercent,
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

func (s *Server) triggerFullBackup(c *gin.Context) {
	degraded := !s.isBackupAvailable()
	if degraded {
		s.logger.Warn("triggerFullBackup: BackupMinIO not configured, operating in degraded mode")
	}
	ctx := c.Request.Context()
	resp, err := s.svc.BackupMetadata(ctx, &miniodbv1.BackupMetadataRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "degraded": degraded})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message, "degraded": degraded})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   resp.Message,
		"backup_id": resp.BackupId,
		"type":      "metadata",
		"note":      "Full backup is not yet implemented. This is a metadata backup.",
		"degraded":  degraded,
	})
}

func (s *Server) triggerTableBackup(c *gin.Context) {
	degraded := !s.isBackupAvailable()
	if degraded {
		s.logger.Warn("triggerTableBackup: BackupMinIO not configured, operating in degraded mode")
	}
	tableName := c.Param("name")
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table name is required", "degraded": degraded})
		return
	}

	ctx := c.Request.Context()
	resp, err := s.svc.BackupMetadata(ctx, &miniodbv1.BackupMetadataRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "degraded": degraded})
		return
	}
	if !resp.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp.Message, "degraded": degraded})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   resp.Message,
		"backup_id": resp.BackupId,
		"table":     tableName,
		"type":      "metadata",
		"note":      "Table backup is not yet implemented. This is a metadata backup.",
		"degraded":  degraded,
	})
}

func (s *Server) restoreBackup(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup id is required"})
		return
	}

	ctx := c.Request.Context()
	req := &miniodbv1.RestoreMetadataRequest{}
	if id == "latest" || id == "" {
		req.FromLatest = true
	} else {
		req.BackupFile = id
	}
	resp, err := s.svc.RestoreMetadata(ctx, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       resp.Message,
		"success":       resp.Success,
		"backup_file":   resp.BackupFile,
		"entries_total": resp.EntriesTotal,
		"entries_ok":    resp.EntriesOk,
		"entries_error": resp.EntriesError,
		"duration":      resp.Duration,
	})
}

func (s *Server) getBackup(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup id is required"})
		return
	}

	ctx := c.Request.Context()
	backups, err := s.svc.ListBackups(ctx, &miniodbv1.ListBackupsRequest{Days: 365})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, b := range backups.Backups {
		if b.ObjectName == id {
			c.JSON(http.StatusOK, b)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
}

func (s *Server) downloadBackup(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup id is required"})
		return
	}

	ctx := c.Request.Context()
	backups, err := s.svc.ListBackups(ctx, &miniodbv1.ListBackupsRequest{Days: 365})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, b := range backups.Backups {
		if b.ObjectName == id {
			minioClient := s.getMinIOClient()
			if minioClient == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "MinIO client not available"})
				return
			}
			s.cfgMu.RLock()
			bucket := s.cfg.Backup.Metadata.Bucket
			if bucket == "" {
				bucket = s.cfg.GetMinIO().Bucket
			}
			s.cfgMu.RUnlock()

			downloadURL, err := minioClient.Presign(ctx, "GET", bucket, id, 24*time.Hour, nil)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"download_url": downloadURL.String(),
				"expires_at":   time.Now().Add(24 * time.Hour),
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
}

func (s *Server) verifyBackup(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup id is required"})
		return
	}

	ctx := c.Request.Context()
	backups, err := s.svc.ListBackups(ctx, &miniodbv1.ListBackupsRequest{Days: 365})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, b := range backups.Backups {
		if b.ObjectName == id {
			minioClient := s.getMinIOClient()
			if minioClient == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "MinIO client not available"})
				return
			}
			s.cfgMu.RLock()
			bucket := s.cfg.Backup.Metadata.Bucket
			if bucket == "" {
				bucket = s.cfg.GetMinIO().Bucket
			}
			s.cfgMu.RUnlock()

			obj, err := minioClient.GetObject(ctx, bucket, id, minio.GetObjectOptions{})
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"valid":      false,
					"error":      err.Error(),
					"size_bytes": 0,
				})
				return
			}
			defer obj.Close()

			stat, err := obj.Stat()
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"valid":      false,
					"error":      "failed to get object stats: " + err.Error(),
					"size_bytes": 0,
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"valid":         true,
				"size_bytes":    stat.Size,
				"last_modified": stat.LastModified,
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
}

func (s *Server) deleteBackup(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup id is required"})
		return
	}

	ctx := c.Request.Context()
	backups, err := s.svc.ListBackups(ctx, &miniodbv1.ListBackupsRequest{Days: 365})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, b := range backups.Backups {
		if b.ObjectName == id {
			minioClient := s.getMinIOClient()
			if minioClient == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "MinIO client not available"})
				return
			}
			s.cfgMu.RLock()
			bucket := s.cfg.Backup.Metadata.Bucket
			if bucket == "" {
				bucket = s.cfg.GetMinIO().Bucket
			}
			s.cfgMu.RUnlock()

			err = minioClient.RemoveObject(ctx, bucket, id, minio.RemoveObjectOptions{})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"message": "backup deleted successfully"})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
}

func (s *Server) updateBackupSchedule(c *gin.Context) {
	if !s.isBackupAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup service unavailable: BackupMinIO not configured"})
		return
	}
	var req struct {
		BackupEnabled    *bool `json:"backup_enabled"`
		BackupInterval   *int  `json:"backup_interval"`
		MetadataEnabled  *bool `json:"metadata_enabled"`
		MetadataInterval *int  `json:"metadata_interval"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s.cfgMu.Lock()
	if req.BackupEnabled != nil {
		s.cfg.Backup.Enabled = *req.BackupEnabled
	}
	if req.BackupInterval != nil && *req.BackupInterval > 0 {
		s.cfg.Backup.Interval = *req.BackupInterval
	}
	if req.MetadataEnabled != nil {
		s.cfg.Backup.Metadata.Enabled = *req.MetadataEnabled
	}
	if req.MetadataInterval != nil && *req.MetadataInterval > 0 {
		s.cfg.Backup.Metadata.Interval = time.Duration(*req.MetadataInterval) * time.Second
	}

	backupEnabled := s.cfg.Backup.Enabled
	backupInterval := s.cfg.Backup.Interval
	metadataEnabled := s.cfg.Backup.Metadata.Enabled
	metadataInterval := s.cfg.Backup.Metadata.Interval.Seconds()
	s.cfgMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message":           "backup schedule updated successfully",
		"backup_enabled":    backupEnabled,
		"backup_interval":   backupInterval,
		"metadata_enabled":  metadataEnabled,
		"metadata_interval": metadataInterval,
	})
}

func (s *Server) GetHub() *sse.Hub {
	return s.hub
}

func (s *Server) SetBackupScheduler(scheduler interface {
	ListPlans() []*config.BackupSchedule
	AddPlan(plan *config.BackupSchedule) error
	AddPlanIfAbsent(plan *config.BackupSchedule) error
	RemovePlan(id string) error
	TriggerExecution(id string) error
}) {
	s.depsMu.Lock()
	s.backupScheduler = scheduler
	s.depsMu.Unlock()
}

func (s *Server) SetBackupStore(store interface {
	ListExecutions(ctx context.Context, planID string, limit int) ([]*backup.BackupExecution, error)
}) {
	s.depsMu.Lock()
	s.backupStore = store
	s.depsMu.Unlock()
}

func (s *Server) SetReplicator(replicator ReplicatorStatsProvider) {
	s.depsMu.Lock()
	s.replicator = replicator
	s.depsMu.Unlock()
}

func (s *Server) GetLogBuffer() *logbuffer.LogBuffer {
	return s.logBuffer
}

const maxExecutionLimit = 1000

func (s *Server) getHotBackupStatus(c *gin.Context) {
	s.depsMu.RLock()
	replicator := s.replicator
	s.depsMu.RUnlock()
	if replicator == nil {
		c.JSON(http.StatusOK, gin.H{
			"available":      false,
			"status":         "idle",
			"load_level":     "idle",
			"last_sync_time": nil,
			"synced_count":   0,
			"skipped_count":  0,
			"failed_count":   0,
			"pool_stats":     nil,
			"message":        "Hot backup not enabled. Configure BackupMinIO to enable.",
		})
		return
	}

	stats := replicator.GetStats()

	var loadLevel string
	if snap := metrics.GetRuntimeSnapshot(); snap != nil {
		loadLevel = snap.LoadLevel.String()
	} else {
		loadLevel = "idle"
	}

	var poolStats interface{}
	if snap := metrics.GetRuntimeSnapshot(); snap != nil {
		poolStats = gin.H{
			"minio_pool_usage": snap.MinIOPoolUsage,
			"goroutines":       snap.Goroutines,
			"heap_alloc_mb":    snap.HeapAllocMB,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"available":      true,
		"status":         stats.Status,
		"load_level":     loadLevel,
		"last_sync_time": stats.LastSyncTime,
		"synced_count":   stats.LastSyncCount,
		"skipped_count":  stats.LastSyncSkipped,
		"failed_count":   stats.LastSyncFailed,
		"sync_cycles":    stats.SyncCycles,
		"current_bucket": stats.CurrentBucket,
		"error_msg":      stats.ErrorMsg,
		"pool_stats":     poolStats,
	})
}

func (s *Server) getHotBackupAvailability(c *gin.Context) {
	s.cfgMu.RLock()
	var available bool
	var degradedMode bool
	var degradedBucket string
	cfg := s.cfg
	if cfg != nil {
		available = cfg.Network.Pools.BackupMinIO != nil && cfg.Network.Pools.BackupMinIO.Endpoint != ""
		if !available {
			degradedMode = true
			degradedBucket = cfg.Network.Pools.MinIO.Bucket
		}
	}
	s.cfgMu.RUnlock()

	message := ""
	if !available {
		message = "BackupMinIO not configured. Hot backup requires BackupMinIO configuration in network.pools.minio_backup."
	} else if degradedMode {
		message = "Operating in degraded mode: using primary MinIO bucket for backup."
	}

	c.JSON(http.StatusOK, gin.H{
		"available":       available,
		"degraded_mode":   degradedMode,
		"degraded_bucket": degradedBucket,
		"message":         message,
	})
}

func (s *Server) listBackupPlans(c *gin.Context) {
	s.depsMu.RLock()
	scheduler := s.backupScheduler
	s.depsMu.RUnlock()
	if scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup scheduler not configured"})
		return
	}

	plans := scheduler.ListPlans()
	c.JSON(http.StatusOK, gin.H{"plans": plans, "total": len(plans)})
}

var validBackupTypes = map[string]bool{
	"metadata": true,
	"full":     true,
	"table":    true,
}

func validateBackupType(backupType string) error {
	if backupType == "" {
		return errors.New("backup_type is required")
	}
	if !validBackupTypes[backupType] {
		return fmt.Errorf("invalid backup_type: %s, must be one of: metadata, full, table", backupType)
	}
	return nil
}

type createBackupPlanRequest struct {
	ID            string   `json:"id" binding:"required"`
	Name          string   `json:"name" binding:"required"`
	Enabled       bool     `json:"enabled"`
	CronExpr      string   `json:"cron_expr"`
	Interval      string   `json:"interval"`
	BackupType    string   `json:"backup_type" binding:"required"`
	Tables        []string `json:"tables"`
	RetentionDays int      `json:"retention_days"`
}

func (s *Server) createBackupPlan(c *gin.Context) {
	s.depsMu.RLock()
	scheduler := s.backupScheduler
	s.depsMu.RUnlock()
	if scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup scheduler not configured"})
		return
	}

	var req createBackupPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := validateBackupType(req.BackupType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	plan := &config.BackupSchedule{
		ID:            req.ID,
		Name:          req.Name,
		Enabled:       req.Enabled,
		CronExpr:      req.CronExpr,
		BackupType:    req.BackupType,
		Tables:        req.Tables,
		RetentionDays: req.RetentionDays,
	}

	if req.Interval != "" {
		interval, err := time.ParseDuration(req.Interval)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid interval format: " + err.Error()})
			return
		}
		plan.Interval = interval
	}

	if err := scheduler.AddPlanIfAbsent(plan); err != nil {
		if errors.Is(err, backup.ErrPlanAlreadyExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "plan with this id already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Backup plan created", "plan": plan})
}

type updateBackupPlanRequest struct {
	Name          string   `json:"name"`
	Enabled       *bool    `json:"enabled"`
	CronExpr      *string  `json:"cron_expr"`
	Interval      string   `json:"interval"`
	BackupType    string   `json:"backup_type"`
	Tables        []string `json:"tables"`
	RetentionDays *int     `json:"retention_days"`
}

func (s *Server) updateBackupPlan(c *gin.Context) {
	s.depsMu.RLock()
	scheduler := s.backupScheduler
	s.depsMu.RUnlock()
	if scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup scheduler not configured"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan id is required"})
		return
	}

	var req updateBackupPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	plans := scheduler.ListPlans()
	var existingPlan *config.BackupSchedule
	for _, p := range plans {
		if p.ID == id {
			existingPlan = p
			break
		}
	}

	if existingPlan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}

	if req.Name != "" {
		existingPlan.Name = req.Name
	}
	if req.Enabled != nil {
		existingPlan.Enabled = *req.Enabled
	}
	if req.CronExpr != nil {
		existingPlan.CronExpr = *req.CronExpr
	}
	if req.Interval != "" {
		interval, err := time.ParseDuration(req.Interval)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid interval format: " + err.Error()})
			return
		}
		existingPlan.Interval = interval
	}
	if req.BackupType != "" {
		if err := validateBackupType(req.BackupType); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		existingPlan.BackupType = req.BackupType
	}
	if req.Tables != nil {
		existingPlan.Tables = req.Tables
	}
	if req.RetentionDays != nil {
		existingPlan.RetentionDays = *req.RetentionDays
	}
	existingPlan.UpdatedAt = time.Now()

	if err := scheduler.AddPlan(existingPlan); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Backup plan updated", "plan": existingPlan})
}

func (s *Server) deleteBackupPlan(c *gin.Context) {
	s.depsMu.RLock()
	scheduler := s.backupScheduler
	s.depsMu.RUnlock()
	if scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup scheduler not configured"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan id is required"})
		return
	}

	if err := scheduler.RemovePlan(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Backup plan deleted"})
}

func (s *Server) triggerBackupPlan(c *gin.Context) {
	s.depsMu.RLock()
	scheduler := s.backupScheduler
	s.depsMu.RUnlock()
	if scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup scheduler not configured"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan id is required"})
		return
	}

	if err := scheduler.TriggerExecution(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Backup plan triggered", "plan_id": id})
}

func (s *Server) listBackupPlanExecutions(c *gin.Context) {
	s.depsMu.RLock()
	store := s.backupStore
	s.depsMu.RUnlock()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Backup store not configured"})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plan id is required"})
		return
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxExecutionLimit {
		limit = maxExecutionLimit
	}

	ctx := c.Request.Context()
	executions, err := store.ListExecutions(ctx, id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"executions": executions, "total": len(executions)})
}
