package rest

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/metadata"
	"minIODB/internal/security"
	"minIODB/internal/service"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server RESTful API服务器
type Server struct {
	olapService      *service.OlapService
	writeCoordinator *coordinator.WriteCoordinator
	queryCoordinator *coordinator.QueryCoordinator
	metadataManager  *metadata.Manager
	cfg              *config.Config
	router           *gin.Engine
	server           *http.Server

	// 安全相关
	authManager        *security.AuthManager
	securityMiddleware *security.SecurityMiddleware
}

// WriteRequest REST API写入请求
type WriteRequest struct {
	Table     string                 `json:"table,omitempty"` // 表名（新增）
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
	NodeID         *string          `json:"node_id,omitempty"`    // 恢复特定节点的所有数据
	IDRange        *IDRangeFilter   `json:"id_range,omitempty"`   // 恢复特定ID范围的数据
	TimeRange      *TimeRangeFilter `json:"time_range,omitempty"` // 恢复特定时间范围的数据
	ForceOverwrite bool             `json:"force_overwrite"`      // 是否强制覆盖已存在的数据
}

// IDRangeFilter ID范围过滤器
type IDRangeFilter struct {
	IDs       []string `json:"ids,omitempty"`        // 具体的ID列表
	IDPattern string   `json:"id_pattern,omitempty"` // ID模式匹配，如 "user-*"
}

// TimeRangeFilter 时间范围过滤器
type TimeRangeFilter struct {
	StartDate string   `json:"start_date"`    // 开始日期 YYYY-MM-DD
	EndDate   string   `json:"end_date"`      // 结束日期 YYYY-MM-DD
	IDs       []string `json:"ids,omitempty"` // 可选：限制特定ID
}

// RecoverDataResponse 数据恢复响应结构 - 与gRPC API保持一致
type RecoverDataResponse struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
	FilesRecovered int32    `json:"files_recovered"`
	RecoveredKeys  []string `json:"recovered_keys"` // 恢复的数据键列表
}

// StatsResponse 统计信息响应
type StatsResponse struct {
	Timestamp   string           `json:"timestamp"`
	BufferStats map[string]int64 `json:"buffer_stats"`
	RedisStats  map[string]int64 `json:"redis_stats"`
	MinioStats  map[string]int64 `json:"minio_stats"`
}

// HealthResponse 健康检查响应
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Version   string            `json:"version"`
	Details   map[string]string `json:"details"`
}

// NodeInfo 节点信息
type NodeInfo struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Type     string `json:"type"`
	Address  string `json:"address"`
	LastSeen int64  `json:"last_seen"`
}

// NodesResponse 节点列表响应
type NodesResponse struct {
	Nodes []NodeInfo `json:"nodes"`
	Total int32      `json:"total"`
}

// 表管理相关结构体定义

// CreateTableRequest 创建表请求
type CreateTableRequest struct {
	TableName   string       `json:"table_name" binding:"required"`
	Config      *TableConfig `json:"config"`
	IfNotExists bool         `json:"if_not_exists"`
}

// CreateTableResponse 创建表响应
type CreateTableResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// TableConfig 表配置
type TableConfig struct {
	BufferSize           int32             `json:"buffer_size"`
	FlushIntervalSeconds int32             `json:"flush_interval_seconds"`
	RetentionDays        int32             `json:"retention_days"`
	BackupEnabled        bool              `json:"backup_enabled"`
	Properties           map[string]string `json:"properties"`
}

// ListTablesRequest 列出表请求
type ListTablesRequest struct {
	Pattern string `json:"pattern,omitempty"`
}

// ListTablesResponse 列出表响应
type ListTablesResponse struct {
	Tables []TableInfo `json:"tables"`
	Total  int32       `json:"total"`
}

// TableInfo 表信息
type TableInfo struct {
	Name      string       `json:"name"`
	Config    *TableConfig `json:"config"`
	CreatedAt string       `json:"created_at"`
	LastWrite string       `json:"last_write"`
	Status    string       `json:"status"`
}

// DescribeTableResponse 描述表响应
type DescribeTableResponse struct {
	TableInfo *TableInfo  `json:"table_info"`
	Stats     *TableStats `json:"stats"`
}

// TableStats 表统计
type TableStats struct {
	RecordCount  int64  `json:"record_count"`
	FileCount    int64  `json:"file_count"`
	SizeBytes    int64  `json:"size_bytes"`
	OldestRecord string `json:"oldest_record"`
	NewestRecord string `json:"newest_record"`
}

// DropTableRequest 删除表请求
type DropTableRequest struct {
	TableName string `json:"table_name" binding:"required"`
	IfExists  bool   `json:"if_exists"`
	Cascade   bool   `json:"cascade"`
}

// DropTableResponse 删除表响应
type DropTableResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	FilesDeleted int32  `json:"files_deleted"`
}

// 元数据管理相关结构体

// TriggerMetadataBackupRequest 触发元数据备份请求
type TriggerMetadataBackupRequest struct {
	Force bool `json:"force"` // 是否强制备份
}

// TriggerMetadataBackupResponse 触发元数据备份响应
type TriggerMetadataBackupResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	BackupID  string `json:"backup_id"`
	Timestamp string `json:"timestamp"`
}

// ListMetadataBackupsRequest 列出元数据备份请求
type ListMetadataBackupsRequest struct {
	Days int `json:"days"` // 查询最近多少天的备份，默认30天
}

// ListMetadataBackupsResponse 列出元数据备份响应
type ListMetadataBackupsResponse struct {
	Backups []MetadataBackupInfo `json:"backups"`
	Total   int                  `json:"total"`
}

// MetadataBackupInfo 元数据备份信息
type MetadataBackupInfo struct {
	ObjectName   string `json:"object_name"`
	NodeID       string `json:"node_id"`
	Timestamp    string `json:"timestamp"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}

// RecoverMetadataRequest 恢复元数据请求
type RecoverMetadataRequest struct {
	BackupFile  string                 `json:"backup_file,omitempty"`  // 指定备份文件，为空则使用最新备份
	FromLatest  bool                   `json:"from_latest"`            // 是否从最新备份恢复
	DryRun      bool                   `json:"dry_run"`                // 是否为干运行
	Overwrite   bool                   `json:"overwrite"`              // 是否覆盖现有数据
	Validate    bool                   `json:"validate"`               // 是否验证数据
	Parallel    bool                   `json:"parallel"`               // 是否并行执行
	Filters     map[string]interface{} `json:"filters,omitempty"`      // 过滤选项
	KeyPatterns []string               `json:"key_patterns,omitempty"` // 键模式过滤
}

// RecoverMetadataResponse 恢复元数据响应
type RecoverMetadataResponse struct {
	Success        bool                   `json:"success"`
	Message        string                 `json:"message"`
	BackupFile     string                 `json:"backup_file"`
	EntriesTotal   int                    `json:"entries_total"`
	EntriesOK      int                    `json:"entries_ok"`
	EntriesSkipped int                    `json:"entries_skipped"`
	EntriesError   int                    `json:"entries_error"`
	Duration       string                 `json:"duration"`
	Errors         []string               `json:"errors,omitempty"`
	Details        map[string]interface{} `json:"details,omitempty"`
}

// MetadataStatusResponse 元数据状态响应
type MetadataStatusResponse struct {
	NodeID       string                 `json:"node_id"`
	BackupStatus map[string]interface{} `json:"backup_status"`
	LastBackup   string                 `json:"last_backup"`
	NextBackup   string                 `json:"next_backup"`
	HealthStatus string                 `json:"health_status"`
}

// ValidateMetadataBackupRequest 验证元数据备份请求
type ValidateMetadataBackupRequest struct {
	BackupFile string `json:"backup_file" binding:"required"`
}

// ValidateMetadataBackupResponse 验证元数据备份响应
type ValidateMetadataBackupResponse struct {
	Valid   bool     `json:"valid"`
	Message string   `json:"message"`
	Errors  []string `json:"errors,omitempty"`
}

// NewServer 创建新的REST服务器
func NewServer(olapService *service.OlapService, cfg *config.Config) *Server {
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
		olapService:        olapService,
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
	if s.cfg.Security.RateLimit.Enabled {
		s.router.Use(s.securityMiddleware.RateLimiter(s.cfg.Security.RateLimit.RequestsPerMinute))
	}
}

// SetCoordinators 设置协调器
func (s *Server) SetCoordinators(writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator) {
	s.writeCoordinator = writeCoord
	s.queryCoordinator = queryCoord
}

// SetMetadataManager 设置元数据管理器
func (s *Server) SetMetadataManager(manager *metadata.Manager) {
	s.metadataManager = manager
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

			// 表管理
			authRequired.POST("/tables", s.createTable)
			authRequired.GET("/tables", s.listTables)
			authRequired.GET("/tables/:table_name", s.describeTable)
			authRequired.DELETE("/tables/:table_name", s.dropTable)

			// 备份相关
			authRequired.POST("/backup/trigger", s.triggerBackup)
			authRequired.POST("/backup/recover", s.recoverData)

			// 系统信息
			authRequired.GET("/stats", s.getStats)
			authRequired.GET("/nodes", s.getNodes)

			// 元数据管理
			authRequired.POST("/metadata/backup", s.triggerMetadataBackup)
			authRequired.GET("/metadata/backups", s.listMetadataBackups)
			authRequired.POST("/metadata/recover", s.recoverMetadata)
			authRequired.GET("/metadata/status", s.getMetadataStatus)
			authRequired.POST("/metadata/validate", s.validateMetadataBackup)
		}
	}
}

// healthCheck 健康检查
func (s *Server) healthCheck(c *gin.Context) {
	// TODO: 添加REST指标收集

	// 委托给service层处理
	grpcResp, err := s.olapService.HealthCheck(c.Request.Context(), &olapv1.HealthCheckRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "internal_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Health check failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := HealthResponse{
		Status:    grpcResp.Status,
		Timestamp: grpcResp.Timestamp,
		Version:   grpcResp.Version,
		Details:   grpcResp.Details,
	}

	if grpcResp.Status == "healthy" {
		c.JSON(http.StatusOK, response)
	} else {
		c.JSON(http.StatusServiceUnavailable, response)
	}
}

// writeData 写入数据
func (s *Server) writeData(c *gin.Context) {
	// TODO: 添加REST指标收集

	var req WriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 转换为gRPC请求格式
	payloadStruct, err := structpb.NewStruct(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "payload_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid payload format: %v", err),
		})
		return
	}

	grpcReq := &olapv1.WriteRequest{
		Table:     req.Table, // 添加表名支持
		Id:        req.ID,
		Timestamp: timestamppb.New(req.Timestamp),
		Payload:   payloadStruct,
	}

	// 使用写入协调器进行分布式路由
	if s.writeCoordinator != nil {
		targetNode, err := s.writeCoordinator.RouteWrite(grpcReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "write_error",
				Code:    http.StatusInternalServerError,
				Message: fmt.Sprintf("Failed to route write: %v", err),
			})
			return
		}

		// 如果路由到本地节点，则直接处理
		if targetNode == "local" {
			grpcResp, err := s.olapService.Write(c.Request.Context(), grpcReq)
			if err != nil {
				c.JSON(http.StatusInternalServerError, ErrorResponse{
					Error:   "write_error",
					Code:    http.StatusInternalServerError,
					Message: fmt.Sprintf("Write operation failed: %v", err),
				})
				return
			}

			response := WriteResponse{
				Success: grpcResp.Success,
				Message: grpcResp.Message,
				NodeID:  s.cfg.Server.NodeID,
			}
			c.JSON(http.StatusOK, response)
			return
		}

		// 如果路由到远程节点，返回成功
		response := WriteResponse{
			Success: true,
			Message: fmt.Sprintf("Write routed to node: %s", targetNode),
			NodeID:  s.cfg.Server.NodeID,
		}
		c.JSON(http.StatusOK, response)
		return
	}

	// 如果没有协调器，回退到本地处理
	grpcResp, err := s.olapService.Write(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "write_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Write operation failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := WriteResponse{
		Success: grpcResp.Success,
		Message: grpcResp.Message,
		NodeID:  s.cfg.Server.NodeID, // 添加节点ID信息
	}

	c.JSON(http.StatusOK, response)
}

// queryData 查询数据
func (s *Server) queryData(c *gin.Context) {
	// TODO: 添加REST指标收集

	var req QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 使用查询协调器进行分布式查询
	if s.queryCoordinator != nil {
		result, err := s.queryCoordinator.ExecuteDistributedQuery(req.SQL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{
				Error:   "query_error",
				Code:    http.StatusInternalServerError,
				Message: fmt.Sprintf("Distributed query execution failed: %v", err),
			})
			return
		}

		// 转换为REST响应格式
		response := QueryResponse{
			ResultJSON: result,
		}

		c.JSON(http.StatusOK, response)
		return
	}

	// 如果没有协调器，回退到本地处理
	grpcResp, err := s.olapService.Query(c.Request.Context(), &olapv1.QueryRequest{
		Sql: req.SQL,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "query_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Query execution failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := QueryResponse{
		ResultJSON: grpcResp.ResultJson,
	}

	c.JSON(http.StatusOK, response)
}

// triggerBackup 触发备份
func (s *Server) triggerBackup(c *gin.Context) {
	// TODO: 添加REST指标收集

	var req TriggerBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 转换为gRPC请求格式
	grpcReq := &olapv1.TriggerBackupRequest{
		Id:  req.ID,
		Day: req.Day,
	}

	// 委托给service层处理
	grpcResp, err := s.olapService.TriggerBackup(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "backup_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Backup operation failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := TriggerBackupResponse{
		Success:       grpcResp.Success,
		Message:       grpcResp.Message,
		FilesBackedUp: grpcResp.FilesBackedUp,
	}

	c.JSON(http.StatusOK, response)
}

// recoverData 恢复数据
func (s *Server) recoverData(c *gin.Context) {
	// TODO: 添加REST指标收集

	var req RecoverDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 转换为gRPC请求格式
	grpcReq := &olapv1.RecoverDataRequest{
		ForceOverwrite: req.ForceOverwrite,
	}

	// 根据请求类型设置恢复模式
	if req.IDRange != nil {
		grpcReq.RecoveryMode = &olapv1.RecoverDataRequest_IdRange{
			IdRange: &olapv1.IdRangeFilter{
				Ids:       req.IDRange.IDs,
				IdPattern: req.IDRange.IDPattern,
			},
		}
	} else if req.TimeRange != nil {
		grpcReq.RecoveryMode = &olapv1.RecoverDataRequest_TimeRange{
			TimeRange: &olapv1.TimeRangeFilter{
				StartDate: req.TimeRange.StartDate,
				EndDate:   req.TimeRange.EndDate,
				Ids:       req.TimeRange.IDs,
			},
		}
	} else {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: "Either id_range or time_range must be specified",
		})
		return
	}

	// 委托给service层处理
	grpcResp, err := s.olapService.RecoverData(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "recovery_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Data recovery failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := RecoverDataResponse{
		Success:        grpcResp.Success,
		Message:        grpcResp.Message,
		FilesRecovered: grpcResp.FilesRecovered,
		RecoveredKeys:  grpcResp.RecoveredKeys,
	}

	c.JSON(http.StatusOK, response)
}

// getStats 获取统计信息
func (s *Server) getStats(c *gin.Context) {
	// TODO: 添加REST指标收集

	// 委托给service层处理
	grpcResp, err := s.olapService.GetStats(c.Request.Context(), &olapv1.GetStatsRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "stats_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Failed to get stats: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := StatsResponse{
		Timestamp:   grpcResp.Timestamp,
		BufferStats: grpcResp.BufferStats,
		RedisStats:  grpcResp.RedisStats,
		MinioStats:  grpcResp.MinioStats,
	}

	c.JSON(http.StatusOK, response)
}

// getNodes 获取节点信息
func (s *Server) getNodes(c *gin.Context) {
	// TODO: 添加REST指标收集

	// 委托给service层处理
	grpcResp, err := s.olapService.GetNodes(c.Request.Context(), &olapv1.GetNodesRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "nodes_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Failed to get nodes: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	var nodes []NodeInfo
	for _, node := range grpcResp.Nodes {
		nodes = append(nodes, NodeInfo{
			ID:       node.Id,
			Status:   node.Status,
			Type:     node.Type,
			Address:  node.Address,
			LastSeen: node.LastSeen,
		})
	}

	response := NodesResponse{
		Nodes: nodes,
		Total: grpcResp.Total,
	}

	c.JSON(http.StatusOK, response)
}

// Start 启动服务器
func (s *Server) Start(port string) error {
	s.server = &http.Server{
		Addr:    port,
		Handler: s.router,
	}

	log.Printf("REST server starting on %s", port)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start REST server: %w", err)
	}

	return nil
}

// Stop 停止服务器
func (s *Server) Stop(ctx context.Context) error {
	log.Println("Stopping REST server...")
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// 表管理相关处理函数

// createTable 创建表
func (s *Server) createTable(c *gin.Context) {
	var req CreateTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 转换为gRPC请求格式
	grpcReq := &service.CreateTableRequest{
		TableName:   req.TableName,
		IfNotExists: req.IfNotExists,
	}

	// 转换配置
	if req.Config != nil {
		grpcReq.Config = &service.TableConfig{
			BufferSize:           req.Config.BufferSize,
			FlushIntervalSeconds: req.Config.FlushIntervalSeconds,
			RetentionDays:        req.Config.RetentionDays,
			BackupEnabled:        req.Config.BackupEnabled,
			Properties:           req.Config.Properties,
		}
	}

	// 委托给service层处理
	grpcResp, err := s.olapService.CreateTable(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "create_table_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Create table failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := CreateTableResponse{
		Success: grpcResp.Success,
		Message: grpcResp.Message,
	}

	c.JSON(http.StatusOK, response)
}

// listTables 列出表
func (s *Server) listTables(c *gin.Context) {
	// 获取查询参数
	pattern := c.Query("pattern")

	// 转换为gRPC请求格式
	grpcReq := &service.ListTablesRequest{
		Pattern: pattern,
	}

	// 委托给service层处理
	grpcResp, err := s.olapService.ListTables(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "list_tables_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("List tables failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	var tables []TableInfo
	for _, table := range grpcResp.Tables {
		tableInfo := TableInfo{
			Name:      table.Name,
			CreatedAt: table.CreatedAt,
			LastWrite: table.LastWrite,
			Status:    table.Status,
		}

		// 转换配置
		if table.Config != nil {
			tableInfo.Config = &TableConfig{
				BufferSize:           table.Config.BufferSize,
				FlushIntervalSeconds: table.Config.FlushIntervalSeconds,
				RetentionDays:        table.Config.RetentionDays,
				BackupEnabled:        table.Config.BackupEnabled,
				Properties:           table.Config.Properties,
			}
		}

		tables = append(tables, tableInfo)
	}

	response := ListTablesResponse{
		Tables: tables,
		Total:  grpcResp.Total,
	}

	c.JSON(http.StatusOK, response)
}

// describeTable 描述表
func (s *Server) describeTable(c *gin.Context) {
	tableName := c.Param("table_name")
	if tableName == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: "table_name is required",
		})
		return
	}

	// 转换为gRPC请求格式
	grpcReq := &service.DescribeTableRequest{
		TableName: tableName,
	}

	// 委托给service层处理
	grpcResp, err := s.olapService.DescribeTable(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "describe_table_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Describe table failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := DescribeTableResponse{
		TableInfo: &TableInfo{
			Name:      grpcResp.TableInfo.Name,
			CreatedAt: grpcResp.TableInfo.CreatedAt,
			LastWrite: grpcResp.TableInfo.LastWrite,
			Status:    grpcResp.TableInfo.Status,
		},
		Stats: &TableStats{
			RecordCount:  grpcResp.Stats.RecordCount,
			FileCount:    grpcResp.Stats.FileCount,
			SizeBytes:    grpcResp.Stats.SizeBytes,
			OldestRecord: grpcResp.Stats.OldestRecord,
			NewestRecord: grpcResp.Stats.NewestRecord,
		},
	}

	// 转换配置
	if grpcResp.TableInfo.Config != nil {
		response.TableInfo.Config = &TableConfig{
			BufferSize:           grpcResp.TableInfo.Config.BufferSize,
			FlushIntervalSeconds: grpcResp.TableInfo.Config.FlushIntervalSeconds,
			RetentionDays:        grpcResp.TableInfo.Config.RetentionDays,
			BackupEnabled:        grpcResp.TableInfo.Config.BackupEnabled,
			Properties:           grpcResp.TableInfo.Config.Properties,
		}
	}

	c.JSON(http.StatusOK, response)
}

// dropTable 删除表
func (s *Server) dropTable(c *gin.Context) {
	tableName := c.Param("table_name")
	if tableName == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: "table_name is required",
		})
		return
	}

	var req DropTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 如果没有请求体，使用默认值
		req = DropTableRequest{
			TableName: tableName,
			IfExists:  false,
			Cascade:   false,
		}
	} else {
		// 确保路径参数和请求体中的表名一致
		req.TableName = tableName
	}

	// 从查询参数获取cascade参数
	if cascadeParam := c.Query("cascade"); cascadeParam == "true" {
		req.Cascade = true
	}

	// 转换为gRPC请求格式
	grpcReq := &service.DropTableRequest{
		TableName: req.TableName,
		IfExists:  req.IfExists,
		Cascade:   req.Cascade,
	}

	// 委托给service层处理
	grpcResp, err := s.olapService.DropTable(c.Request.Context(), grpcReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "drop_table_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Drop table failed: %v", err),
		})
		return
	}

	// 转换为REST响应格式
	response := DropTableResponse{
		Success:      grpcResp.Success,
		Message:      grpcResp.Message,
		FilesDeleted: grpcResp.FilesDeleted,
	}

	c.JSON(http.StatusOK, response)
}

// 元数据管理处理方法

// triggerMetadataBackup 触发元数据备份
func (s *Server) triggerMetadataBackup(c *gin.Context) {
	if s.metadataManager == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "service_unavailable",
			Code:    http.StatusServiceUnavailable,
			Message: "Metadata manager not available",
		})
		return
	}

	var req TriggerMetadataBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 触发手动备份
	if err := s.metadataManager.TriggerBackup(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "backup_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Failed to trigger backup: %v", err),
		})
		return
	}

	response := TriggerMetadataBackupResponse{
		Success:   true,
		Message:   "Metadata backup triggered successfully",
		BackupID:  fmt.Sprintf("backup_%d", time.Now().Unix()),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, response)
}

// listMetadataBackups 列出元数据备份
func (s *Server) listMetadataBackups(c *gin.Context) {
	if s.metadataManager == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "service_unavailable",
			Code:    http.StatusServiceUnavailable,
			Message: "Metadata manager not available",
		})
		return
	}

	// 获取查询参数
	days := 30 // 默认30天
	if daysParam := c.Query("days"); daysParam != "" {
		if parsedDays, err := strconv.Atoi(daysParam); err == nil && parsedDays > 0 {
			days = parsedDays
		}
	}

	// 获取备份列表
	backups, err := s.metadataManager.ListBackups(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "list_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Failed to list backups: %v", err),
		})
		return
	}

	// 转换为响应格式
	var backupInfos []MetadataBackupInfo
	for _, backup := range backups {
		backupInfos = append(backupInfos, MetadataBackupInfo{
			ObjectName:   backup.ObjectName,
			NodeID:       backup.NodeID,
			Timestamp:    backup.Timestamp.Format(time.RFC3339),
			Size:         backup.Size,
			LastModified: backup.LastModified.Format(time.RFC3339),
		})
	}

	response := ListMetadataBackupsResponse{
		Backups: backupInfos,
		Total:   len(backupInfos),
	}

	c.JSON(http.StatusOK, response)
}

// recoverMetadata 恢复元数据
func (s *Server) recoverMetadata(c *gin.Context) {
	if s.metadataManager == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "service_unavailable",
			Code:    http.StatusServiceUnavailable,
			Message: "Metadata manager not available",
		})
		return
	}

	var req RecoverMetadataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 构建恢复选项
	options := metadata.RecoveryOptions{
		Overwrite:   req.Overwrite,
		Validate:    req.Validate,
		DryRun:      req.DryRun,
		Parallel:    req.Parallel,
		Filters:     req.Filters,
		KeyPatterns: req.KeyPatterns,
	}

	// 设置恢复模式
	if req.DryRun {
		options.Mode = metadata.RecoveryModeDryRun
	} else {
		options.Mode = metadata.RecoveryModeComplete
	}

	var result *metadata.RecoveryResult
	var err error

	// 根据请求类型执行恢复
	if req.FromLatest || req.BackupFile == "" {
		result, err = s.metadataManager.RecoverFromLatest(c.Request.Context(), options)
	} else {
		result, err = s.metadataManager.RecoverFromBackup(c.Request.Context(), req.BackupFile, options)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "recovery_error",
			Code:    http.StatusInternalServerError,
			Message: fmt.Sprintf("Failed to recover metadata: %v", err),
		})
		return
	}

	response := RecoverMetadataResponse{
		Success:        result.Success,
		Message:        "Metadata recovery completed",
		BackupFile:     result.BackupFile,
		EntriesTotal:   result.EntriesTotal,
		EntriesOK:      result.EntriesOK,
		EntriesSkipped: result.EntriesSkipped,
		EntriesError:   result.EntriesError,
		Duration:       result.Duration.String(),
		Errors:         result.Errors,
		Details:        result.Details,
	}

	c.JSON(http.StatusOK, response)
}

// getMetadataStatus 获取元数据状态
func (s *Server) getMetadataStatus(c *gin.Context) {
	if s.metadataManager == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "service_unavailable",
			Code:    http.StatusServiceUnavailable,
			Message: "Metadata manager not available",
		})
		return
	}

	// 获取管理器状态
	status := s.metadataManager.GetStatus()

	response := MetadataStatusResponse{
		NodeID:       s.cfg.Server.NodeID,
		BackupStatus: status,
		LastBackup:   "", // TODO: 从状态中获取
		NextBackup:   "", // TODO: 从状态中获取
		HealthStatus: "healthy",
	}

	c.JSON(http.StatusOK, response)
}

// validateMetadataBackup 验证元数据备份
func (s *Server) validateMetadataBackup(c *gin.Context) {
	if s.metadataManager == nil {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "service_unavailable",
			Code:    http.StatusServiceUnavailable,
			Message: "Metadata manager not available",
		})
		return
	}

	var req ValidateMetadataBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "validation_error",
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// 验证备份文件
	if err := s.metadataManager.ValidateBackup(c.Request.Context(), req.BackupFile); err != nil {
		response := ValidateMetadataBackupResponse{
			Valid:   false,
			Message: fmt.Sprintf("Backup validation failed: %v", err),
			Errors:  []string{err.Error()},
		}
		c.JSON(http.StatusOK, response)
		return
	}

	response := ValidateMetadataBackupResponse{
		Valid:   true,
		Message: "Backup validation passed",
	}

	c.JSON(http.StatusOK, response)
}
