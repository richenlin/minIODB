package rest

import (
	"minIODB/pkg/logger"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	miniodbv1 "minIODB/api/proto/miniodb/v1"
	"minIODB/config"
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
	miniodbService   *service.MinIODBService
	writeCoordinator *coordinator.WriteCoordinator
	queryCoordinator *coordinator.QueryCoordinator
	metadataManager  *metadata.Manager
	cfg              *config.Config
	router           *gin.Engine
	server           *http.Server

	authManager        *security.AuthManager
	securityMiddleware *security.SecurityMiddleware
	smartRateLimiter   *security.SmartRateLimiter
	tokenManager       *security.TokenManager
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

	// ID生成配置
	IDStrategy     string             `json:"id_strategy,omitempty"`      // ID生成策略: uuid, snowflake, custom, user_provided
	IDPrefix       string             `json:"id_prefix,omitempty"`        // ID前缀（用于custom和snowflake策略）
	AutoGenerateID bool               `json:"auto_generate_id,omitempty"` // 是否自动生成ID（未提供时）
	IDValidation   *IDValidationRules `json:"id_validation,omitempty"`    // ID验证规则
}

// IDValidationRules ID验证规则
type IDValidationRules struct {
	Required     bool   `json:"required"`                // 是否必须提供ID
	MaxLength    int32  `json:"max_length"`              // 最大长度
	Pattern      string `json:"pattern,omitempty"`       // 正则验证模式
	AllowedChars string `json:"allowed_chars,omitempty"` // 允许的字符集
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
func NewServer(miniodbService *service.MinIODBService, cfg *config.Config) *Server {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// 初始化认证管理器
	authConfig := &security.AuthConfig{
		Mode:            cfg.Security.Mode,
		JWTSecret:       cfg.Security.JWTSecret,
		TokenExpiration: 24 * time.Hour,
		Issuer:          "miniodb",
		Audience:        "miniodb-api",
		ValidTokens:     cfg.Security.ValidTokens,
	}

	authManager, err := security.NewAuthManager(authConfig)
	if err != nil {
		logger.GetLogger().Sugar().Info("Warning: Failed to initialize auth manager: %v, security features will be limited", err)
	}

	// 创建安全中间件
	securityMiddleware := security.NewSecurityMiddleware(authManager)

	// 创建智能限流器
	var smartRateLimiter *security.SmartRateLimiter

	if cfg.Security.SmartRateLimit.Enabled {
		// 转换配置格式
		smartRateLimitConfig := security.SmartRateLimiterConfig{
			Enabled:         cfg.Security.SmartRateLimit.Enabled,
			DefaultTier:     cfg.Security.SmartRateLimit.DefaultTier,
			CleanupInterval: cfg.Security.SmartRateLimit.CleanupInterval,
		}

		// 转换限流等级
		for _, tier := range cfg.Security.SmartRateLimit.Tiers {
			smartRateLimitConfig.Tiers = append(smartRateLimitConfig.Tiers, security.RateLimitTier{
				Name:            tier.Name,
				RequestsPerSec:  tier.RequestsPerSec,
				BurstSize:       tier.BurstSize,
				Window:          tier.Window,
				BackoffDuration: tier.BackoffDuration,
			})
		}

		// 转换路径限制
		for _, pathLimit := range cfg.Security.SmartRateLimit.PathLimits {
			smartRateLimitConfig.PathLimits = append(smartRateLimitConfig.PathLimits, security.PathRateLimit{
				Pattern: pathLimit.Pattern,
				Tier:    pathLimit.Tier,
				Enabled: pathLimit.Enabled,
			})
		}

		smartRateLimiter = security.NewSmartRateLimiter(smartRateLimitConfig)
		logger.GetLogger().Sugar().Info("REST smart rate limiter initialized with %d tiers and %d path rules",
			len(smartRateLimitConfig.Tiers), len(smartRateLimitConfig.PathLimits))
	} else {
		// 使用默认配置但禁用
		defaultConfig := security.GetDefaultSmartRateLimiterConfig()
		defaultConfig.Enabled = false
		smartRateLimiter = security.NewSmartRateLimiter(defaultConfig)
		logger.GetLogger().Sugar().Info("REST smart rate limiter disabled")
	}

	tokenManager := security.NewTokenManager(nil)

	server := &Server{
		miniodbService:     miniodbService,
		cfg:                cfg,
		router:             router,
		authManager:        authManager,
		securityMiddleware: securityMiddleware,
		smartRateLimiter:   smartRateLimiter,
		tokenManager:       tokenManager,
	}

	server.setupMiddleware()
	server.setupRoutes()

	logger.GetLogger().Sugar().Info("REST server initialized with authentication mode: %s", cfg.Security.Mode)

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

	// 智能限流中间件（优先使用）
	if s.cfg.Security.SmartRateLimit.Enabled && s.smartRateLimiter != nil {
		s.router.Use(s.smartRateLimiter.Middleware())
		logger.GetLogger().Sugar().Info("Smart rate limiter middleware enabled for REST API")
	} else if s.cfg.Security.RateLimit.Enabled {
		// 传统限流中间件（向后兼容）
		s.router.Use(s.securityMiddleware.RateLimiter(s.cfg.Security.RateLimit.RequestsPerMinute))
		logger.GetLogger().Sugar().Info("Traditional rate limiter middleware enabled for REST API")
	} else {
		logger.GetLogger().Sugar().Info("Rate limiting disabled for REST API")
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
	api := s.router.Group("/v1")

	// 认证路由 - 不需要JWT验证
	authGroup := api.Group("/auth")
	{
		authGroup.POST("/token", s.getToken)
		authGroup.POST("/refresh", s.refreshToken)
		authGroup.DELETE("/token", s.revokeToken)
	}

	// 健康检查路由 - 不需要JWT验证
	api.GET("/health", s.healthCheck)

	// 需要JWT验证的路由
	securedRoutes := api.Group("")
	if s.authManager != nil && s.authManager.IsEnabled() {
		logger.GetLogger().Sugar().Info("JWT authentication enabled for REST API")
		// 使用简单的JWT验证中间件
		securedRoutes.Use(s.jwtAuthMiddleware())
	} else {
		logger.GetLogger().Sugar().Info("JWT authentication disabled for REST API")
	}

	// 数据操作
	securedRoutes.POST("/data", s.writeData)
	securedRoutes.POST("/query", s.queryData)
	securedRoutes.PUT("/data", s.updateData)
	securedRoutes.DELETE("/data", s.deleteData)

	// 表管理
	securedRoutes.POST("/tables", s.createTable)
	securedRoutes.GET("/tables", s.listTables)
	securedRoutes.GET("/tables/:name", s.getTable)
	securedRoutes.DELETE("/tables/:name", s.deleteTable)

	// 元数据管理
	securedRoutes.POST("/metadata/backup", s.backupMetadata)
	securedRoutes.POST("/metadata/restore", s.restoreMetadata)
	securedRoutes.GET("/metadata/backups", s.listBackups)
	securedRoutes.GET("/metadata/status", s.getMetadataStatus)

	// 系统状态与监控
	securedRoutes.GET("/status", s.getStatus)   // 主状态接口（包含统计和节点信息）
	securedRoutes.GET("/metrics", s.getMetrics) // 性能指标接口
}

// healthCheck 处理健康检查请求
func (s *Server) healthCheck(c *gin.Context) {
	// 调用统一服务
	err := s.miniodbService.HealthCheck(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":    "unhealthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"version":   "1.0.0",
			"details": map[string]string{
				"error": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
		"details": map[string]string{
			"message": "All systems operational",
		},
	})
}

// writeData 处理数据写入请求
func (s *Server) writeData(c *gin.Context) {
	var req struct {
		Table     string                 `json:"table"`
		ID        string                 `json:"id"` // 移除 binding:"required"，改为可选
		Timestamp time.Time              `json:"timestamp" binding:"required"`
		Payload   map[string]interface{} `json:"payload" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 确定表名
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	// ID自动生成逻辑
	if req.ID == "" {
		// 获取表配置
		tableConfig := s.miniodbService.GetTableConfig(c.Request.Context(), tableName)

		// 如果表配置允许自动生成ID
		if tableConfig.AutoGenerateID {
			// 使用服务层的ID生成器
			generatedID, err := s.generateID(c.Request.Context(), tableName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Failed to generate ID: " + err.Error(),
				})
				return
			}
			req.ID = generatedID
			logger.GetLogger().Sugar().Info("Auto-generated ID for table %s: %s", tableName, req.ID)
		} else {
			// 如果不允许自动生成且未提供ID，返回错误
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "ID is required for this table. Set auto_generate_id=true in table config to enable auto-generation.",
			})
			return
		}
	}

	// 构建Protobuf请求
	payload, err := structpb.NewStruct(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse payload: " + err.Error()})
		return
	}

	dataRecord := &miniodbv1.DataRecord{
		Id:        req.ID,
		Timestamp: timestamppb.New(req.Timestamp),
		Payload:   payload,
	}

	protoReq := &miniodbv1.WriteDataRequest{
		Table: req.Table,
		Data:  dataRecord,
	}

	// 调用统一服务
	resp, err := s.miniodbService.WriteData(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": resp.Success,
		"message": resp.Message,
		"node_id": resp.NodeId,
		"id":      req.ID, // 返回使用的ID（可能是生成的）
	})
}

// generateID 生成ID（辅助方法）
func (s *Server) generateID(ctx context.Context, tableName string) (string, error) {
	// 获取表配置
	tableConfig := s.miniodbService.GetTableConfig(ctx, tableName)

	// 通过gRPC调用MinIODB服务的ID生成器
	// 这里我们需要调用服务层的方法
	// 为简单起见，直接使用内部方法
	return s.miniodbService.GenerateID(ctx, tableName, tableConfig)
}

// queryData 处理数据查询请求
func (s *Server) queryData(c *gin.Context) {
	var req struct {
		SQL    string `json:"sql" binding:"required"`
		Limit  int32  `json:"limit,omitempty"`
		Cursor string `json:"cursor,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	protoReq := &miniodbv1.QueryDataRequest{
		Sql:    req.SQL,
		Limit:  req.Limit,
		Cursor: req.Cursor,
	}

	// 调用统一服务
	resp, err := s.miniodbService.QueryData(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"result_json": resp.ResultJson,
		"has_more":    resp.HasMore,
		"next_cursor": resp.NextCursor,
	})
}

// updateData 处理数据更新请求
func (s *Server) updateData(c *gin.Context) {
	var req struct {
		Table   string                 `json:"table" binding:"required"`
		ID      string                 `json:"id" binding:"required"`
		Payload map[string]interface{} `json:"payload" binding:"required"`
		Partial bool                   `json:"partial"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 构建Protobuf请求
	payload, err := structpb.NewStruct(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse payload: " + err.Error()})
		return
	}

	protoReq := &miniodbv1.UpdateDataRequest{
		Table:     req.Table,
		Id:        req.ID,
		Payload:   payload,
		Timestamp: timestamppb.New(time.Now()),
	}

	// 调用统一服务
	resp, err := s.miniodbService.UpdateData(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": resp.Success,
		"message": resp.Message,
	})
}

// deleteData 处理数据删除请求
func (s *Server) deleteData(c *gin.Context) {
	var req struct {
		Table string   `json:"table" binding:"required"`
		IDs   []string `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one ID is required"})
		return
	}

	var totalDeleted int32
	var errors []string

	for _, id := range req.IDs {
		protoReq := &miniodbv1.DeleteDataRequest{
			Table: req.Table,
			Id:    id,
		}

		resp, err := s.miniodbService.DeleteData(c.Request.Context(), protoReq)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete %s: %v", id, err))
			continue
		}

		if resp.Success {
			totalDeleted += resp.DeletedCount
		} else {
			errors = append(errors, fmt.Sprintf("failed to delete %s: %s", id, resp.Message))
		}
	}

	success := len(errors) == 0
	message := fmt.Sprintf("deleted %d records from table %s", totalDeleted, req.Table)
	if len(errors) > 0 {
		message = fmt.Sprintf("deleted %d records with %d errors", totalDeleted, len(errors))
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       success,
		"message":       message,
		"deleted_count": totalDeleted,
		"errors":        errors,
	})
}

// getStatus 处理获取状态请求（合并了节点和统计信息）
func (s *Server) getStatus(c *gin.Context) {
	protoReq := &miniodbv1.GetStatusRequest{}

	// 调用统一服务
	resp, err := s.miniodbService.GetStatus(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp":    resp.Timestamp,
		"buffer_stats": resp.BufferStats,
		"redis_stats":  resp.RedisStats,
		"minio_stats":  resp.MinioStats,
		"nodes":        resp.Nodes,
		"total_nodes":  resp.TotalNodes,
	})
}

// getMetrics 处理获取性能指标请求
func (s *Server) getMetrics(c *gin.Context) {
	protoReq := &miniodbv1.GetMetricsRequest{}

	// 调用统一服务
	resp, err := s.miniodbService.GetMetrics(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp":           resp.Timestamp,
		"performance_metrics": resp.PerformanceMetrics,
		"resource_usage":      resp.ResourceUsage,
		"system_info":         resp.SystemInfo,
	})
}

// getToken 处理获取JWT令牌请求
func (s *Server) getToken(c *gin.Context) {
	var req struct {
		APIKey    string `json:"api_key" binding:"required"`
		APISecret string `json:"api_secret" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.APIKey == "" || req.APISecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key and secret are required"})
		return
	}

	accessToken, err := s.authManager.GenerateToken(req.APIKey, req.APIKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	refreshToken, err := s.tokenManager.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	if err := s.tokenManager.StoreRefreshToken(c.Request.Context(), refreshToken, req.APIKey); err != nil {
		logger.GetLogger().Sugar().Info("WARN: Failed to store refresh token: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    3600,
		"token_type":    "Bearer",
	})
}

// refreshToken 处理刷新JWT令牌请求
func (s *Server) refreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Refresh token is required"})
		return
	}

	userID, err := s.tokenManager.ValidateRefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired refresh token"})
		return
	}

	if err := s.tokenManager.RevokeRefreshToken(c.Request.Context(), req.RefreshToken); err != nil {
		logger.GetLogger().Sugar().Info("WARN: Failed to revoke old refresh token: %v", err)
	}

	accessToken, err := s.authManager.GenerateToken(userID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate new token"})
		return
	}

	newRefreshToken, err := s.tokenManager.GenerateRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate refresh token"})
		return
	}

	if err := s.tokenManager.StoreRefreshToken(c.Request.Context(), newRefreshToken, userID); err != nil {
		logger.GetLogger().Sugar().Info("WARN: Failed to store new refresh token: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": newRefreshToken,
		"expires_in":    3600,
		"token_type":    "Bearer",
	})
}

// revokeToken 处理撤销JWT令牌请求
func (s *Server) revokeToken(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token is required"})
		return
	}

	if err := s.tokenManager.RevokeAccessToken(c.Request.Context(), req.Token); err != nil {
		logger.GetLogger().Sugar().Info("WARN: Failed to revoke token: %v", err)
	}

	tokenPreview := req.Token
	if len(tokenPreview) > 10 {
		tokenPreview = tokenPreview[:10] + "..."
	}
	logger.GetLogger().Sugar().Info("INFO: Token revoked via REST API: %s", tokenPreview)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Token revoked successfully",
	})
}

// createTable 处理创建表请求
func (s *Server) createTable(c *gin.Context) {
	var req CreateTableRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 转换为Protobuf请求
	protoReq := &miniodbv1.CreateTableRequest{
		TableName:   req.TableName,
		IfNotExists: req.IfNotExists,
	}

	// 转换表配置
	if req.Config != nil {
		protoConfig := &miniodbv1.TableConfig{
			BufferSize:           req.Config.BufferSize,
			FlushIntervalSeconds: req.Config.FlushIntervalSeconds,
			RetentionDays:        req.Config.RetentionDays,
			BackupEnabled:        req.Config.BackupEnabled,
			Properties:           req.Config.Properties,
			IdStrategy:           req.Config.IDStrategy,
			IdPrefix:             req.Config.IDPrefix,
			AutoGenerateId:       req.Config.AutoGenerateID,
		}

		// 转换ID验证规则
		if req.Config.IDValidation != nil {
			protoConfig.IdValidation = &miniodbv1.IDValidationRules{
				MaxLength:    req.Config.IDValidation.MaxLength,
				Pattern:      req.Config.IDValidation.Pattern,
				AllowedChars: req.Config.IDValidation.AllowedChars,
			}
		}

		protoReq.Config = protoConfig
	}

	// 调用统一服务
	resp, err := s.miniodbService.CreateTable(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// listTables 处理列出表请求
func (s *Server) listTables(c *gin.Context) {
	pattern := c.DefaultQuery("pattern", "")

	protoReq := &miniodbv1.ListTablesRequest{
		Pattern: pattern,
	}

	// 调用统一服务
	resp, err := s.miniodbService.ListTables(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// getTable 处理获取表信息请求
func (s *Server) getTable(c *gin.Context) {
	tableName := c.Param("name")
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Table name is required"})
		return
	}

	protoReq := &miniodbv1.GetTableRequest{
		TableName: tableName,
	}

	// 调用统一服务
	resp, err := s.miniodbService.GetTable(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// deleteTable 处理删除表请求
func (s *Server) deleteTable(c *gin.Context) {
	tableName := c.Param("name")
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Table name is required"})
		return
	}

	ifExists, _ := strconv.ParseBool(c.DefaultQuery("if_exists", "false"))
	cascade, _ := strconv.ParseBool(c.DefaultQuery("cascade", "false"))

	protoReq := &miniodbv1.DeleteTableRequest{
		TableName: tableName,
		IfExists:  ifExists,
		Cascade:   cascade,
	}

	// 调用统一服务
	resp, err := s.miniodbService.DeleteTable(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// backupMetadata 处理备份元数据请求
func (s *Server) backupMetadata(c *gin.Context) {
	var req miniodbv1.BackupMetadataRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 调用统一服务
	resp, err := s.miniodbService.BackupMetadata(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// restoreMetadata 处理恢复元数据请求
func (s *Server) restoreMetadata(c *gin.Context) {
	var req miniodbv1.RestoreMetadataRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 调用统一服务
	resp, err := s.miniodbService.RestoreMetadata(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// listBackups 处理列出备份请求
func (s *Server) listBackups(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))

	protoReq := &miniodbv1.ListBackupsRequest{
		Days: int32(days),
	}

	// 调用统一服务
	resp, err := s.miniodbService.ListBackups(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// getMetadataStatus 处理获取元数据状态请求
func (s *Server) getMetadataStatus(c *gin.Context) {
	protoReq := &miniodbv1.GetMetadataStatusRequest{}

	// 调用统一服务
	resp, err := s.miniodbService.GetMetadataStatus(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// jwtAuthMiddleware JWT认证中间件
func (s *Server) jwtAuthMiddleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		if s.authManager == nil || !s.authManager.IsEnabled() {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, bearerPrefix)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token is required"})
			c.Abort()
			return
		}

		if s.tokenManager != nil && s.tokenManager.IsTokenRevoked(c.Request.Context(), token) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token has been revoked"})
			c.Abort()
			return
		}

		claims, err := s.authManager.ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)

		c.Next()
	})
}

// Start 启动服务器
func (s *Server) Start(port string) error {
	// 获取REST网络配置
	restNetworkConfig := getRESTNetworkConfig(s.cfg)

	// 创建优化的HTTP服务器配置
	s.server = &http.Server{
		Addr:              port,
		Handler:           s.router,
		ReadTimeout:       restNetworkConfig.ReadTimeout,       // 30s
		WriteTimeout:      restNetworkConfig.WriteTimeout,      // 30s
		IdleTimeout:       restNetworkConfig.IdleTimeout,       // 60s
		ReadHeaderTimeout: restNetworkConfig.ReadHeaderTimeout, // 10s
		MaxHeaderBytes:    restNetworkConfig.MaxHeaderBytes,    // 1MB
	}

	logger.GetLogger().Sugar().Info("REST server starting on %s with optimized network config", port)
	logger.GetLogger().Sugar().Info("REST server timeouts - Read: %v, Write: %v, Idle: %v, ReadHeader: %v",
		restNetworkConfig.ReadTimeout,
		restNetworkConfig.WriteTimeout,
		restNetworkConfig.IdleTimeout,
		restNetworkConfig.ReadHeaderTimeout)

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start REST server: %w", err)
	}

	return nil
}

// Stop 停止服务器
func (s *Server) Stop(ctx context.Context) error {
	logger.GetLogger().Sugar().Info("Stopping REST server...")
	if s.server != nil {
		// 获取优雅关闭超时配置
		restNetworkConfig := getRESTNetworkConfig(s.cfg)

		// 创建带超时的context
		shutdownCtx, cancel := context.WithTimeout(ctx, restNetworkConfig.ShutdownTimeout)
		defer cancel()

		logger.GetLogger().Sugar().Info("REST server graceful shutdown timeout: %v", restNetworkConfig.ShutdownTimeout)
		return s.server.Shutdown(shutdownCtx)
	}
	return nil
}

// getRESTNetworkConfig 获取REST网络配置，优先使用Network配置，否则使用默认值
func getRESTNetworkConfig(cfg *config.Config) *config.RESTNetworkConfig {
	// 检查是否有新的网络配置
	if cfg.Network.Server.REST.ReadTimeout > 0 {
		return &cfg.Network.Server.REST
	}

	// 返回默认配置
	return &config.RESTNetworkConfig{
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1048576, // 1MB
		ShutdownTimeout:   30 * time.Second,
	}
}
