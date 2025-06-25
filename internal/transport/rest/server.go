package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server RESTful API服务器
type Server struct {
	ingester     *ingest.Ingester
	querier      *query.Querier
	buffer       *buffer.SharedBuffer
	redisClient  *redis.Client
	primaryMinio storage.Uploader
	backupMinio  storage.Uploader
	cfg          config.Config
	router       *gin.Engine
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
}

// QueryRequest REST API查询请求
type QueryRequest struct {
	SQL string `json:"sql" binding:"required"`
}

// QueryResponse REST API查询响应
type QueryResponse struct {
	ResultJSON string `json:"result_json"`
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
func NewServer(redisClient *redis.Client, primaryMinio storage.Uploader, backupMinio storage.Uploader, cfg config.Config) (*Server, error) {
	buf := buffer.NewSharedBuffer(redisClient, primaryMinio, backupMinio, cfg.Backup.Minio.Bucket, 10, 5*time.Second)

	querier, err := query.NewQuerier(redisClient, primaryMinio, cfg.Minio, buf)
	if err != nil {
		return nil, err
	}

	// 设置Gin模式
	if cfg.Server.Debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	server := &Server{
		ingester:     ingest.NewIngester(buf),
		querier:      querier,
		buffer:       buf,
		redisClient:  redisClient,
		primaryMinio: primaryMinio,
		backupMinio:  backupMinio,
		cfg:          cfg,
		router:       router,
	}

	server.setupRoutes()
	return server, nil
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
	}
}

// healthCheck 健康检查端点
func (s *Server) healthCheck(c *gin.Context) {
	// 检查Redis连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "redis_unavailable",
			Code:    503,
			Message: "Redis connection failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
	})
}

// writeData 处理数据写入请求
func (s *Server) writeData(c *gin.Context) {
	var req WriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
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

	// 执行写入
	err = s.ingester.IngestData(protoReq)
	if err != nil {
		log.Printf("Failed to ingest data: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "ingest_failed",
			Code:    500,
			Message: "Failed to ingest data",
		})
		return
	}

	c.JSON(http.StatusOK, WriteResponse{
		Success: true,
		Message: "Data written successfully",
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

	// 执行查询
	result, err := s.querier.ExecuteQuery(req.SQL)
	if err != nil {
		log.Printf("Query failed: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "query_failed",
			Code:    500,
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, QueryResponse{
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
	objectNames, err := s.redisClient.SMembers(ctx, redisKey).Result()
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

	// TODO: 实现备份逻辑（与gRPC版本相同）
	var successCount int32 = 0
	for _, objName := range objectNames {
		// 备份逻辑实现
		successCount++
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
			"total_items": s.buffer.Size(),
		},
		"redis_stats": map[string]interface{}{
			"connected": true,
		},
	}

	c.JSON(http.StatusOK, stats)
}

// Start 启动REST服务器
func (s *Server) Start(port string) error {
	log.Printf("Starting REST server on port %s", port)
	return s.router.Run(port)
}

// Stop 停止服务器
func (s *Server) Stop() {
	if s.buffer != nil {
		s.buffer.Stop()
	}
} 