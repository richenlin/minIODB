package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/ingest"
	"minIODB/internal/metrics"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"github.com/gin-gonic/gin"
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
	primaryMinio     *storage.MinioClient
	backupMinio      *storage.MinioClient
	cfg              *config.Config
	router           *gin.Engine
	server           *http.Server
}

// WriteRequest REST API写入请求
type WriteRequest struct {
	ID        string                 `json:"id" binding:"required"`
	Timestamp time.Time              `json:"timestamp" binding:"required"`
	Payload   map[string]interface{} `json:"payload" binding:"required"`
}

// WriteResponse REST API写入响应
type WriteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

// QueryRequest REST API查询请求
type QueryRequest struct {
	SQL string `json:"sql" binding:"required"`
}

// QueryResponse REST API查询响应
type QueryResponse struct {
	Success    bool   `json:"success"`
	ResultJSON string `json:"result_json"`
	Message    string `json:"message,omitempty"`
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

// NewServer 创建新的REST服务器
func NewServer(ingester *ingest.Ingester, querier *query.Querier, bufferManager *buffer.Manager, redisClient *storage.RedisClient, primaryMinio, backupMinio *storage.MinioClient, cfg *config.Config) *Server {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	server := &Server{
		ingester:      ingester,
		querier:       querier,
		bufferManager: bufferManager,
		redisClient:   redisClient,
		primaryMinio:  primaryMinio,
		backupMinio:   backupMinio,
		cfg:           cfg,
		router:        router,
	}

	server.setupRoutes()
	return server
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
		// 健康检查
		v1.GET("/health", s.healthCheck)
		
		// 数据写入
		v1.POST("/data", s.writeData)
		
		// 数据查询
		v1.POST("/query", s.queryData)
		
		// 手动备份
		v1.POST("/backup/trigger", s.triggerBackup)
		
		// 系统状态
		v1.GET("/stats", s.getStats)
		
		// 节点信息
		v1.GET("/nodes", s.getNodes)
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
	httpMetrics := metrics.NewHTTPMetrics(c.Request.Method, "/write")
	
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

	// 转换为protobuf格式
	payload, err := structpb.NewStruct(req.Payload)
	if err != nil {
		httpMetrics.Finish("400")
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_payload",
			Code:    400,
			Message: "Failed to parse payload",
		})
		return
	}

	protoReq := &olapv1.WriteRequest{
		Id:        req.ID,
		Timestamp: timestamppb.New(req.Timestamp),
		Payload:   payload,
	}

	// 使用协调器路由写入请求
	var nodeID string
	var writeErr error
	
	if s.writeCoordinator != nil {
		nodeID, writeErr = s.writeCoordinator.RouteWrite(protoReq)
		if writeErr != nil {
			log.Printf("Failed to route write request: %v", writeErr)
			httpMetrics.Finish("500")
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "write_routing_failed",
				Code:    500,
				Message: "Failed to route write request",
			})
			return
		}
		
		// 如果是本地写入
		if nodeID == "local" {
			writeErr = s.ingester.Write(req.ID, string(payload.String()), req.Timestamp.UnixNano())
		}
	} else {
		// 直接本地写入
		writeErr = s.ingester.Write(req.ID, string(payload.String()), req.Timestamp.UnixNano())
		nodeID = "local"
	}

	if writeErr != nil {
		log.Printf("Failed to write data: %v", writeErr)
		httpMetrics.Finish("500")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "write_failed",
			Code:    500,
			Message: "Failed to write data",
		})
		return
	}

	httpMetrics.Finish("200")
	c.JSON(http.StatusOK, WriteResponse{
		Success: true,
		Message: "Data written successfully",
		NodeID:  nodeID,
	})
}

// queryData 处理查询请求
func (s *Server) queryData(c *gin.Context) {
	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// 使用协调器执行分布式查询
	var result string
	var queryErr error
	
	if s.queryCoordinator != nil {
		result, queryErr = s.queryCoordinator.ExecuteDistributedQuery(req.SQL)
	} else {
		// 直接本地查询
		result, queryErr = s.querier.Query(req.SQL)
	}

	if queryErr != nil {
		log.Printf("Query failed: %v", queryErr)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "query_failed",
			Code:    500,
			Message: queryErr.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, QueryResponse{
		Success:    true,
		ResultJSON: result,
	})
}

// triggerBackup 处理备份请求
func (s *Server) triggerBackup(c *gin.Context) {
	var req TriggerBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_request",
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	if s.backupMinio == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "backup_disabled",
			Code:    503,
			Message: "Backup is not enabled in configuration",
		})
		return
	}

	ctx := context.Background()
	redisKey := fmt.Sprintf("index:id:%s:%s", req.ID, req.Day)
	objectNames, err := s.redisClient.SMembers(ctx, redisKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "redis_error",
			Code:    500,
			Message: "Failed to get objects from Redis",
		})
		return
	}

	if len(objectNames) == 0 {
		c.JSON(http.StatusOK, TriggerBackupResponse{
			Success:       true,
			Message:       "No objects found to back up",
			FilesBackedUp: 0,
		})
		return
	}

	// 执行备份逻辑
	var successCount int32 = 0
	for _, objName := range objectNames {
		// 检查备份中是否已存在
		exists, err := s.backupMinio.ObjectExists(ctx, objName)
		if err != nil {
			log.Printf("Failed to check backup object existence: %v", err)
			continue
		}

		if !exists {
			// 从主存储读取数据
			data, err := s.primaryMinio.GetObject(ctx, objName)
			if err != nil {
				log.Printf("Failed to get object for backup: %v", err)
				continue
			}

			// 写入备份存储
			if err := s.backupMinio.PutObject(ctx, objName, data); err != nil {
				log.Printf("Failed to backup object: %v", err)
				continue
			}

			successCount++
		}
	}

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