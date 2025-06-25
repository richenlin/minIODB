package rest

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/ingest"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/security"
	"minIODB/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server RESTful API服务器
type Server struct {
	ingester         *ingest.Ingester
	querier          *query.Querier
	bufferManager    *buffer.Manager
	writeCoordinator *coordinator.WriteCoordinator
	queryCoordinator *coordinator.QueryCoordinator
	redisClient      *storage.RedisClient
	primaryMinio     storage.Uploader
	backupMinio      storage.Uploader
	cfg              *config.Config
	router           *gin.Engine
	server           *http.Server
	
	// 安全相关
	authManager        *security.AuthManager
	securityMiddleware *security.SecurityMiddleware
}

// WriteRequest REST API写入请求
type WriteRequest struct {
	ID        string                 `json:"id" binding:"required"`
	Timestamp time.Time              `json:"timestamp" binding:"required"`
	Payload   map[string]interface{} `json:"payload" binding:"required"`
}

// WriteResponse REST API写入响应 - 扩展了gRPC版本，添加了NodeID字段用于分布式环境
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"` // 扩展字段：处理请求的节点ID
}

// QueryRequest REST API查询请求
type QueryRequest struct {
	SQL string `json:"sql" binding:"required"`
}

// QueryResponse REST API查询响应 - 与gRPC API保持一致
type QueryResponse struct {
	ResultJSON string `json:"result_json"` // 与gRPC的result_json字段保持一致
}

// TriggerBackupRequest REST API备份请求
type TriggerBackupRequest struct {
	ID  string `json:"id" binding:"required"`
	Day string `json:"day" binding:"required"`
}

// TriggerBackupResponse REST API备份响应
type TriggerBackupResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	FilesBackedUp int32  `json:"files_backed_up"`
}

// ErrorResponse 统一错误响应格式
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RecoverDataRequest 数据恢复请求结构 - 与gRPC API保持一致
type RecoverDataRequest struct {
	// 恢复模式：可以按ID范围、时间范围或节点ID恢复
	NodeID       *string           `json:"node_id,omitempty"`        // 恢复特定节点的所有数据
	IDRange      *IDRangeFilter    `json:"id_range,omitempty"`       // 恢复特定ID范围的数据
	TimeRange    *TimeRangeFilter  `json:"time_range,omitempty"`     // 恢复特定时间范围的数据
	ForceOverwrite bool            `json:"force_overwrite"`          // 是否强制覆盖已存在的数据
}

// IDRangeFilter ID范围过滤器
type IDRangeFilter struct {
	IDs       []string `json:"ids,omitempty"`        // 具体的ID列表
	IDPattern string   `json:"id_pattern,omitempty"` // ID模式匹配，如 "user-*"
}

// TimeRangeFilter 时间范围过滤器
type TimeRangeFilter struct {
	StartDate string   `json:"start_date"`         // 开始日期 YYYY-MM-DD
	EndDate   string   `json:"end_date"`           // 结束日期 YYYY-MM-DD
	IDs       []string `json:"ids,omitempty"`      // 可选：限制特定ID
}

// RecoverDataResponse 数据恢复响应结构 - 与gRPC API保持一致
type RecoverDataResponse struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
	FilesRecovered int32    `json:"files_recovered"`
	RecoveredKeys  []string `json:"recovered_keys"` // 恢复的数据键列表
}

// NewServer 创建新的REST服务器
func NewServer(ingester *ingest.Ingester, querier *query.Querier, bufferManager *buffer.Manager, redisClient *storage.RedisClient, primaryMinio, backupMinio storage.Uploader, cfg *config.Config) *Server {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	
	// 初始化认证管理器
	authConfig := &security.AuthConfig{
		Mode:            cfg.Security.Mode,
		JWTSecret:       cfg.Security.JWTSecret,
		TokenExpiration: 24 * time.Hour,
		Issuer:          "miniodb",
		Audience:        "miniodb-rest",
		ValidTokens:     cfg.Security.ValidTokens,
	}

	authManager, err := security.NewAuthManager(authConfig)
	if err != nil {
		log.Fatalf("Failed to create auth manager: %v", err)
	}

	// 创建安全中间件
	securityMiddleware := security.NewSecurityMiddleware(authManager)

	server := &Server{
		ingester:           ingester,
		querier:            querier,
		bufferManager:      bufferManager,
		redisClient:        redisClient,
		primaryMinio:       primaryMinio,
		backupMinio:        backupMinio,
		cfg:                cfg,
		router:             router,
		authManager:        authManager,
		securityMiddleware: securityMiddleware,
	}

	server.setupMiddleware()
	server.setupRoutes()
	
	log.Printf("REST server initialized with authentication mode: %s", cfg.Security.Mode)
	
	return server
}

// setupMiddleware 设置中间件
func (s *Server) setupMiddleware() {
	// 基础中间件
	s.router.Use(gin.Logger())
	s.router.Use(gin.Recovery())
	
	// 安全中间件
	s.router.Use(s.securityMiddleware.CORS())
	s.router.Use(s.securityMiddleware.SecurityHeaders())
	s.router.Use(s.securityMiddleware.RequestLogger())
	
	// 可选的限流中间件
	s.router.Use(s.securityMiddleware.RateLimiter(60)) // 每分钟60个请求
}

// SetCoordinators 设置协调器
func (s *Server) SetCoordinators(writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator) {
	s.writeCoordinator = writeCoord
	s.queryCoordinator = queryCoord
}

// setupRoutes 设置路由
func (s *Server) setupRoutes() {
	// API版本前缀
	v1 := s.router.Group("/v1")
	{
		// 不需要认证的路由
		v1.GET("/health", s.healthCheck)
		
		// 需要认证的路由组
		authRequired := v1.Group("")
		authRequired.Use(s.securityMiddleware.AuthRequired())
		{
			// 数据写入
			authRequired.POST("/data", s.writeData)
			
			// 数据查询
			authRequired.POST("/query", s.queryData)
			
			// 手动备份
			authRequired.POST("/backup/trigger", s.triggerBackup)
			
			// 数据恢复
			authRequired.POST("/recover", s.recoverData)
			
			// 系统状态
			authRequired.GET("/stats", s.getStats)
			
			// 节点信息
			authRequired.GET("/nodes", s.getNodes)
		}
	}
}

// healthCheck 健康检查端点
func (s *Server) healthCheck(c *gin.Context) {
	// 记录HTTP指标
	httpMetrics := metrics.NewHTTPMetrics(c.Request.Method, "/health")
	
	// 检查Redis连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := s.redisClient.Ping(ctx); err != nil {
		httpMetrics.Finish("503")
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "redis_unavailable",
			Code:    503,
			Message: "Redis connection failed",
		})
		return
	}

	httpMetrics.Finish("200")
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
	})
}

// writeData 处理数据写入请求
func (s *Server) writeData(c *gin.Context) {
	// 记录HTTP指标
	httpMetrics := metrics.NewHTTPMetrics(c.Request.Method, "/data")
	
	var req WriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMetrics.Finish("400")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.GetUserFromContext(c); ok {
			log.Printf("Write request from user: %s (ID: %s) for data ID: %s", user.Username, user.ID, req.ID)
		}
	} else {
		log.Printf("Received Write request for ID: %s", req.ID)
	}

	// 转换为gRPC请求格式
	payloadStruct, err := structpb.NewStruct(req.Payload)
	if err != nil {
		httpMetrics.Finish("500")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "payload_conversion_error",
			Code:    500,
			Message: "Failed to convert payload",
		})
		return
	}

	grpcReq := &olapv1.WriteRequest{
		Id:        req.ID,
		Timestamp: timestamppb.New(req.Timestamp),
		Payload:   payloadStruct,
	}

	// 执行写入
	err = s.ingester.IngestData(grpcReq)
	if err != nil {
		log.Printf("Failed to ingest data: %v", err)
		httpMetrics.Finish("500")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "ingest_error",
			Code:    500,
			Message: "Failed to ingest data",
		})
		return
	}

	httpMetrics.Finish("200")
	c.JSON(http.StatusOK, WriteResponse{
		Success: true,
		Message: "Data written successfully",
		NodeID:  s.cfg.Server.NodeID,
	})
}

// queryData 处理数据查询请求
func (s *Server) queryData(c *gin.Context) {
	// 记录HTTP指标
	httpMetrics := metrics.NewHTTPMetrics(c.Request.Method, "/query")
	
	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMetrics.Finish("400")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.GetUserFromContext(c); ok {
			log.Printf("Query request from user: %s (ID: %s) with SQL: %s", user.Username, user.ID, req.SQL)
		}
	} else {
		log.Printf("Received Query request with SQL: %s", req.SQL)
	}

	// 执行查询
	result, err := s.querier.ExecuteQuery(req.SQL)
	if err != nil {
		log.Printf("Failed to execute query: %v", err)
		httpMetrics.Finish("500")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "query_execution_failed",
			Code:    500,
			Message: fmt.Sprintf("Failed to execute query: %v", err),
		})
		return
	}

	httpMetrics.Finish("200")
	c.JSON(http.StatusOK, QueryResponse{
		ResultJSON: result,
	})
}

// triggerBackup 处理手动备份请求
func (s *Server) triggerBackup(c *gin.Context) {
	// 记录HTTP指标
	httpMetrics := metrics.NewHTTPMetrics(c.Request.Method, "/backup/trigger")
	
	var req TriggerBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMetrics.Finish("400")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.GetUserFromContext(c); ok {
			log.Printf("TriggerBackup request from user: %s (ID: %s) for ID %s, Day %s", user.Username, user.ID, req.ID, req.Day)
		}
	} else {
		log.Printf("Received TriggerBackup request for ID %s, Day %s", req.ID, req.Day)
	}

	if s.backupMinio == nil {
		httpMetrics.Finish("503")
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "backup_not_enabled",
			Code:    503,
			Message: "Backup is not enabled in configuration",
		})
		return
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	redisKey := fmt.Sprintf("index:id:%s:%s", req.ID, req.Day)
	objectNames, err := s.redisClient.SMembers(ctx, redisKey)
	if err != nil {
		log.Printf("Failed to get objects from redis: %v", err)
		httpMetrics.Finish("500")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "redis_query_failed",
			Code:    500,
			Message: "Failed to query objects from Redis",
		})
		return
	}

	if len(objectNames) == 0 {
		httpMetrics.Finish("200")
		c.JSON(http.StatusOK, TriggerBackupResponse{
			Success:       true,
			Message:       "No objects found to back up",
			FilesBackedUp: 0,
		})
		return
	}

	var successCount int32 = 0
	for _, objName := range objectNames {
		src := minio.CopySrcOptions{
			Bucket: "olap-data", // Primary bucket
			Object: objName,
		}
		dst := minio.CopyDestOptions{
			Bucket: s.cfg.Backup.Minio.Bucket,
			Object: objName,
		}
		_, err := s.backupMinio.CopyObject(ctx, dst, src)
		if err != nil {
			log.Printf("ERROR: failed to back up object %s: %v", objName, err)
			// Continue with other objects
		} else {
			successCount++
		}
	}

	httpMetrics.Finish("200")
	c.JSON(http.StatusOK, TriggerBackupResponse{
		Success:       true,
		Message:       fmt.Sprintf("Backup process completed. Backed up %d files.", successCount),
		FilesBackedUp: successCount,
	})
}

// getStats 获取系统统计信息
func (s *Server) getStats(c *gin.Context) {
	stats := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"buffer_stats": map[string]interface{}{
			"size": s.bufferManager.Size(),
		},
		"redis_stats": map[string]interface{}{
			"connected": true,
		},
	}

	c.JSON(http.StatusOK, stats)
}

// getNodes 获取集群节点信息
func (s *Server) getNodes(c *gin.Context) {
	// 这里需要从服务注册中获取节点信息
	// 由于没有直接访问serviceRegistry的方式，返回基本信息
	nodes := []map[string]interface{}{
		{
			"id":     s.cfg.Server.NodeID,
			"status": "healthy",
			"type":   "local",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"nodes": nodes,
		"total": len(nodes),
	})
}

// recoverData 处理数据恢复请求
func (s *Server) recoverData(c *gin.Context) {
	// 记录HTTP指标
	httpMetrics := metrics.NewHTTPMetrics(c.Request.Method, "/recover")
	
	var req RecoverDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMetrics.Finish("400")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// 获取用户信息（如果启用了认证）
	if s.authManager.IsEnabled() {
		if user, ok := security.GetUserFromContext(c); ok {
			log.Printf("RecoverData request from user: %s (ID: %s)", user.Username, user.ID)
		}
	} else {
		log.Printf("Received RecoverData request")
	}

	if s.backupMinio == nil {
		httpMetrics.Finish("503")
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "backup_not_enabled",
			Code:    503,
			Message: "Backup is not enabled in configuration",
		})
		return
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var dataKeys []string
	var err error

	// 根据不同的恢复模式获取需要恢复的数据键
	if req.NodeID != nil {
		dataKeys, err = s.getDataKeysByNodeId(ctx, *req.NodeID)
	} else if req.IDRange != nil {
		dataKeys, err = s.getDataKeysByIdRange(ctx, req.IDRange)
	} else if req.TimeRange != nil {
		dataKeys, err = s.getDataKeysByTimeRange(ctx, req.TimeRange)
	} else {
		httpMetrics.Finish("400")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_recovery_mode",
			Code:    400,
			Message: "Must specify one of: node_id, id_range, or time_range",
		})
		return
	}

	if err != nil {
		log.Printf("Failed to get data keys: %v", err)
		httpMetrics.Finish("500")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "data_key_query_failed",
			Code:    500,
			Message: "Failed to query data keys for recovery",
		})
		return
	}

	if len(dataKeys) == 0 {
		httpMetrics.Finish("200")
		c.JSON(http.StatusOK, RecoverDataResponse{
			Success:        true,
			Message:        "No data found to recover",
			FilesRecovered: 0,
			RecoveredKeys:  []string{},
		})
		return
	}

	log.Printf("Found %d data keys to recover", len(dataKeys))

	var totalFilesRecovered int32 = 0
	var recoveredKeys []string

	// 对每个数据键进行恢复
	for _, dataKey := range dataKeys {
		filesRecovered, err := s.recoverDataForKey(ctx, dataKey, req.ForceOverwrite)
		if err != nil {
			log.Printf("ERROR: failed to recover data for key %s: %v", dataKey, err)
			continue
		}
		totalFilesRecovered += filesRecovered
		if filesRecovered > 0 {
			recoveredKeys = append(recoveredKeys, dataKey)
		}
	}

	httpMetrics.Finish("200")
	c.JSON(http.StatusOK, RecoverDataResponse{
		Success:        true,
		Message:        fmt.Sprintf("Recovery process completed. Recovered %d files from %d keys.", totalFilesRecovered, len(recoveredKeys)),
		FilesRecovered: totalFilesRecovered,
		RecoveredKeys:  recoveredKeys,
	})
}

// Start 启动REST服务器
func (s *Server) Start(port string) error {
	s.server = &http.Server{
		Addr:    port,
		Handler: s.router,
	}
	
	log.Printf("Starting REST server on port %s", port)
	return s.server.ListenAndServe()
}

// Stop 停止服务器
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// getDataKeysByNodeId 根据节点ID获取数据键
func (s *Server) getDataKeysByNodeId(ctx context.Context, nodeId string) ([]string, error) {
	nodeDataKey := fmt.Sprintf("node:data:%s", nodeId)
	return s.redisClient.SMembers(ctx, nodeDataKey)
}

// getDataKeysByIdRange 根据ID范围获取数据键
func (s *Server) getDataKeysByIdRange(ctx context.Context, idRange *IDRangeFilter) ([]string, error) {
	var allKeys []string

	// 处理具体的ID列表
	for _, id := range idRange.IDs {
		pattern := fmt.Sprintf("index:id:%s:*", id)
		keys, err := s.redisClient.Keys(ctx, pattern)
		if err != nil {
			return nil, err
		}
		allKeys = append(allKeys, keys...)
	}

	// 处理ID模式匹配
	if idRange.IDPattern != "" {
		pattern := fmt.Sprintf("index:id:%s:*", idRange.IDPattern)
		keys, err := s.redisClient.Keys(ctx, pattern)
		if err != nil {
			return nil, err
		}
		allKeys = append(allKeys, keys...)
	}

	return allKeys, nil
}

// getDataKeysByTimeRange 根据时间范围获取数据键
func (s *Server) getDataKeysByTimeRange(ctx context.Context, timeRange *TimeRangeFilter) ([]string, error) {
	var allKeys []string

	// 解析时间范围
	startDate, err := time.Parse("2006-01-02", timeRange.StartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start date format: %w", err)
	}
	endDate, err := time.Parse("2006-01-02", timeRange.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid end date format: %w", err)
	}

	// 生成日期范围内的所有日期
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		dayStr := d.Format("2006-01-02")

		if len(timeRange.IDs) > 0 {
			// 如果指定了ID列表，只查询这些ID
			for _, id := range timeRange.IDs {
				pattern := fmt.Sprintf("index:id:%s:%s", id, dayStr)
				keys, err := s.redisClient.Keys(ctx, pattern)
				if err != nil {
					return nil, err
				}
				allKeys = append(allKeys, keys...)
			}
		} else {
			// 否则查询该日期的所有数据
			pattern := fmt.Sprintf("index:id:*:%s", dayStr)
			keys, err := s.redisClient.Keys(ctx, pattern)
			if err != nil {
				return nil, err
			}
			allKeys = append(allKeys, keys...)
		}
	}

	return allKeys, nil
}

// recoverDataForKey 恢复特定数据键的所有文件
func (s *Server) recoverDataForKey(ctx context.Context, dataKey string, forceOverwrite bool) (int32, error) {
	// 从备份存储获取文件列表
	backupFiles, err := s.redisClient.SMembers(ctx, dataKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get backup files from redis: %w", err)
	}

	if len(backupFiles) == 0 {
		log.Printf("No backup files found for key %s", dataKey)
		return 0, nil
	}

	var successCount int32 = 0

	for _, fileName := range backupFiles {
		// 检查主存储中是否已存在该文件
		if !forceOverwrite {
			exists, err := s.primaryMinio.ObjectExists(ctx, s.cfg.Minio.Bucket, fileName)
			if err == nil && exists {
				log.Printf("File %s already exists in primary storage, skipping (use force_overwrite to override)", fileName)
				continue
			}
		}

		// 从备份存储获取数据
		data, err := s.backupMinio.GetObject(ctx, s.cfg.Backup.Minio.Bucket, fileName, minio.GetObjectOptions{})
		if err != nil {
			log.Printf("ERROR: failed to get backup file %s: %v", fileName, err)
			continue
		}

		// 恢复到主存储
		reader := bytes.NewReader(data)
		_, err = s.primaryMinio.PutObject(ctx, s.cfg.Minio.Bucket, fileName, reader, int64(len(data)), minio.PutObjectOptions{})
		if err != nil {
			log.Printf("ERROR: failed to recover file %s: %v", fileName, err)
			continue
		}

		successCount++
		log.Printf("Successfully recovered file %s", fileName)
	}

	return successCount, nil
} 