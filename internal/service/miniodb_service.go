package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"minIODB/api/proto/miniodb/v1"
	"minIODB/config"
	"minIODB/internal/audit"
	"minIODB/internal/ingest"
	"minIODB/internal/metadata"
	"minIODB/internal/query"
	"minIODB/internal/security"
	"minIODB/internal/subscription"
	"minIODB/pkg/idgen"
	"minIODB/pkg/lock"
	"minIODB/pkg/pool"
	"minIODB/pkg/version"

	minioClient "github.com/minio/minio-go/v7"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// 包级正则表达式，只编译一次
var (
	// digitsRegex 用于从节点ID字符串中提取数字
	digitsRegex = regexp.MustCompile(`\d+`)
	// fromTableRegex 用于重写查询中的旧式表名（FROM table）
	fromTableRegex = regexp.MustCompile(`(?i)\bfrom\s+table\b`)
)

// BackupResult 备份结果
type BackupResult struct {
	BackupID      string            `json:"backup_id"`
	BackupType    string            `json:"backup_type"` // "full" 或 "table"
	TableName     string            `json:"table_name"`  // 表级备份时有效
	Timestamp     time.Time         `json:"timestamp"`
	Duration      time.Duration     `json:"duration"`
	FilesCount    int               `json:"files_count"`
	TotalSize     int64             `json:"total_size"`
	MetadataSaved bool              `json:"metadata_saved"`
	DataSaved     bool              `json:"data_saved"`
	ObjectName    string            `json:"object_name"` // MinIO 对象名
	Details       map[string]string `json:"details,omitempty"`
}

// TableBackupManifest 表备份清单
type TableBackupManifest struct {
	BackupID     string                `json:"backup_id"`
	BackupType   string                `json:"backup_type"`
	TableName    string                `json:"table_name"`
	SourceTable  string                `json:"source_table"` // 恢复时原始表名
	TargetTable  string                `json:"target_table"` // 恢复时目标表名
	Timestamp    time.Time             `json:"timestamp"`
	NodeID       string                `json:"node_id"`
	TableConfig  *config.TableConfig   `json:"table_config"`
	Files        []TableBackupFileInfo `json:"files"`
	MetadataKeys []string              `json:"metadata_keys"` // 相关的 Redis 键
	TotalSize    int64                 `json:"total_size"`
	Version      string                `json:"version"`
}

// TableBackupFileInfo 表备份文件信息
type TableBackupFileInfo struct {
	OriginalPath string `json:"original_path"` // 原始对象路径
	BackupPath   string `json:"backup_path"`   // 备份对象路径
	Size         int64  `json:"size"`
}

// FullBackupManifest 全量备份清单
type FullBackupManifest struct {
	BackupID     string                `json:"backup_id"`
	BackupType   string                `json:"backup_type"` // "full"
	Timestamp    time.Time             `json:"timestamp"`
	NodeID       string                `json:"node_id"`
	Tables       []TableBackupManifest `json:"tables"`
	MetadataFile string                `json:"metadata_file"` // 元数据备份文件
	TotalSize    int64                 `json:"total_size"`
	Version      string                `json:"version"`
}

// MinIODBService 实现MinIODBServiceServer接口
type MinIODBService struct {
	miniodb.UnimplementedMinIODBServiceServer
	cfg                 *config.Config
	ingester            *ingest.Ingester
	querier             *query.Querier
	redisPool           *pool.RedisPool
	tableManager        *TableManager
	metadataMgr         *metadata.Manager
	configManager       *TableConfigManager
	idGenerator         idgen.IDGenerator
	subscriptionManager *subscription.Manager            // 数据订阅管理器
	locker              lock.Locker                      // 分布式锁
	auditLogger         *audit.AuditLogger               // 审计日志记录器
	encryptionManager   *security.FieldEncryptionManager // 字段加密管理器
	logger              *zap.Logger
	startTime           time.Time
}

// NewMinIODBService 创建新的MinIODBService实例。
// primaryMinio 仅在 standalone 模式（redisPool == nil）下用于持久化表配置到 MinIO；有 Redis 时忽略。
func NewMinIODBService(cfg *config.Config, logger *zap.Logger, ingester *ingest.Ingester, querier *query.Querier,
	redisPool *pool.RedisPool, metadataMgr *metadata.Manager, primaryMinio ...*minioClient.Client) (*MinIODBService, error) {

	var minioC *minioClient.Client
	if len(primaryMinio) > 0 {
		minioC = primaryMinio[0]
	}
	tableManager := NewTableManager(redisPool, minioC, nil, cfg, logger)

	// 创建表配置管理器
	var redisPoolForConfig *pool.RedisPool
	if redisPool != nil {
		redisPoolForConfig = redisPool
	}
	configManager := NewTableConfigManager(redisPoolForConfig, cfg.Tables.DefaultConfig)

	// 创建ID生成器
	// 从配置中获取节点ID（用于Snowflake）
	nodeID := int64(1) // 默认节点ID
	if cfg.Server.NodeID != "" {
		// 尝试从NodeID字符串中提取数字
		if id, err := extractNodeIDNumber(cfg.Server.NodeID); err == nil {
			nodeID = id
		}
	}

	idGenerator, err := idgen.NewGenerator(&idgen.GeneratorConfig{
		NodeID:          nodeID,
		DefaultStrategy: cfg.Tables.DefaultConfig.IDStrategy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ID generator: %w", err)
	}

	// 创建分布式锁
	// 根据 Redis 模式自动选择：standalone -> 乐观锁，sentinel/cluster -> Redis 分布式锁
	var locker lock.Locker
	if redisPool != nil {
		redisMode := cfg.Redis.Mode
		if redisMode == "" {
			redisMode = cfg.Network.Pools.Redis.Mode
		}
		locker = lock.NewLockerFromRedisMode(redisMode, redisPool)
	} else {
		// 无 Redis 连接池时使用乐观锁
		locker = lock.NewOptimisticLocker()
	}

	// 创建审计日志记录器
	auditLogger, err := audit.NewAuditLogger(&audit.Config{
		Enabled:  true, // 默认启用审计日志
		FilePath: "logs/audit.log",
		NodeID:   cfg.Server.NodeID,
	}, logger)
	if err != nil {
		logger.Warn("Failed to create audit logger, audit logging disabled", zap.Error(err))
		// 创建一个禁用的审计日志记录器，避免 nil 检查
		auditLogger, _ = audit.NewAuditLogger(&audit.Config{
			Enabled: false,
			NodeID:  cfg.Server.NodeID,
		}, logger)
	}

	// 创建字段加密管理器（如果配置了）
	var encryptionManager *security.FieldEncryptionManager
	if cfg.Tables.DefaultConfig.FieldEncryption != nil && cfg.Tables.DefaultConfig.FieldEncryption.Enabled {
		encMgr, err := security.NewFieldEncryptionManager(cfg.Tables.DefaultConfig.FieldEncryption, logger)
		if err != nil {
			logger.Warn("Failed to create field encryption manager, encryption disabled", zap.Error(err))
		} else {
			encryptionManager = encMgr
			logger.Info("Field encryption manager created successfully",
				zap.Strings("encrypted_fields", cfg.Tables.DefaultConfig.FieldEncryption.EncryptedFields))
		}
	}

	// 将加密管理器传递给 ingester 和 querier
	if ingester != nil && encryptionManager != nil {
		ingester.SetEncryptionManager(encryptionManager)
	}
	if querier != nil && encryptionManager != nil {
		querier.SetEncryptionManager(encryptionManager)
	}

	return &MinIODBService{
		cfg:               cfg,
		ingester:          ingester,
		querier:           querier,
		redisPool:         redisPool,
		tableManager:      tableManager,
		metadataMgr:       metadataMgr,
		configManager:     configManager,
		idGenerator:       idGenerator,
		locker:            locker,
		auditLogger:       auditLogger,
		encryptionManager: encryptionManager,
		logger:            logger,
		startTime:         time.Now(),
	}, nil
}

// extractNodeIDNumber 从节点ID字符串中提取数字
func extractNodeIDNumber(nodeID string) (int64, error) {
	// 尝试直接解析
	if id, err := strconv.ParseInt(nodeID, 10, 64); err == nil {
		return id % 1024, nil // 限制在0-1023范围内
	}

	// 提取字符串中的数字
	matches := digitsRegex.FindStringSubmatch(nodeID)
	if len(matches) > 0 {
		if id, err := strconv.ParseInt(matches[0], 10, 64); err == nil {
			return id % 1024, nil
		}
	}

	// 使用哈希
	hash := int64(0)
	for _, c := range nodeID {
		hash = hash*31 + int64(c)
	}
	return (hash & 0x7FFFFFFF) % 1024, nil
}

// WriteData 写入数据
func (s *MinIODBService) WriteData(ctx context.Context, req *miniodb.WriteDataRequest) (*miniodb.WriteDataResponse, error) {
	// 处理表名：优先使用请求中的表名，如果为空则使用默认表
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	recordID := ""
	if req.Data != nil {
		recordID = req.Data.Id
	}

	s.logger.Sugar().Infof("Processing write request for table: %s, ID: %s", tableName, recordID)

	// 基础请求校验（不依赖表配置的部分）
	if req.Data == nil {
		err := status.Error(codes.InvalidArgument, "Data record is required")
		s.auditLogger.LogWrite(ctx, tableName, recordID, false, err, nil)
		return nil, err
	}
	if req.Data.Payload == nil {
		err := status.Error(codes.InvalidArgument, "Payload is required")
		s.auditLogger.LogWrite(ctx, tableName, recordID, false, err, nil)
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		err := fmt.Errorf("invalid table name: %s", tableName)
		s.auditLogger.LogWrite(ctx, tableName, recordID, false, err, nil)
		return &miniodb.WriteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", tableName),
			NodeId:  s.cfg.Server.NodeID,
		}, nil
	}

	// 确保表存在（在获取表配置之前，让表有机会被创建并初始化配置）
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to ensure table exists: %v", err)
		s.auditLogger.LogWrite(ctx, tableName, recordID, false, err, nil)
		return &miniodb.WriteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Table error: %v", err),
		}, nil
	}

	// 获取表配置（在 EnsureTableExists 之后，确保新表配置已写入 Redis）
	tableConfig := s.GetTableConfig(ctx, tableName)
	s.logger.Sugar().Infof("WriteData: table=%s, AutoGenerateID=%v, providedID=%q", tableName, tableConfig.AutoGenerateID, recordID)

	// 处理 ID：根据表配置的 id_strategy 决定行为
	recordID = req.Data.Id
	if recordID == "" {
		if idgen.IDGeneratorStrategy(tableConfig.IDStrategy) == idgen.StrategyUserProvided {
			// 表明确要求用户提供 ID
			err := status.Error(codes.InvalidArgument,
				fmt.Sprintf("table %q requires a user-provided ID (id_strategy=user_provided); please specify an ID when writing data", tableName))
			s.auditLogger.LogWrite(ctx, tableName, "", false, err, nil)
			return nil, err
		}
		if tableConfig.AutoGenerateID {
			generatedID, err := s.GenerateID(ctx, tableName, tableConfig)
			if err != nil {
				s.logger.Sugar().Errorf("Failed to generate ID for table %s: %v", tableName, err)
				s.auditLogger.LogWrite(ctx, tableName, "", false, err, nil)
				return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to generate ID: %v", err))
			}
			recordID = generatedID
			s.logger.Sugar().Infof("Auto-generated ID for table %s: %s (strategy=%s)", tableName, recordID, tableConfig.IDStrategy)
		}
		// recordID 仍为空：允许（兼容某些遗留写入行为）
	}

	// 转换为内部写入请求格式，Timestamp 为空时自动补充当前时间
	ts := req.Data.Timestamp
	if ts == nil {
		ts = timestamppb.Now()
	}
	ingestReq := &miniodb.WriteRequest{
		Table:     tableName,
		Id:        recordID,
		Timestamp: ts,
		Payload:   req.Data.Payload,
	}

	// 使用Ingester处理写入
	if err := s.ingester.IngestData(ingestReq); err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to ingest data for table %s, ID %s: %v", tableName, recordID, err)
		s.auditLogger.LogWrite(ctx, tableName, recordID, false, err, nil)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to ingest data: %v", err))
	}

	// 更新表的最后写入时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		s.logger.Sugar().Infof("WARN: Failed to update last write time for table %s: %v", tableName, err)
	}

	// 使查询缓存失效（写入数据后，之前的查询结果可能已过期）
	if s.querier != nil {
		if err := s.querier.InvalidateCache(ctx, []string{tableName}); err != nil {
			s.logger.Sugar().Warnf("Failed to invalidate query cache for table %s: %v", tableName, err)
		} else {
			s.logger.Sugar().Infof("Invalidated query cache for table %s after write", tableName)
		}
	}

	// 发布写入事件到订阅通道（异步，不影响主流程）
	if s.subscriptionManager != nil && s.subscriptionManager.IsRunning() {
		go s.publishWriteEvent(ctx, tableName, req.Data)
	}

	// 记录成功的审计日志
	s.auditLogger.LogWrite(ctx, tableName, recordID, true, nil, map[string]interface{}{
		"has_timestamp": req.Data.Timestamp != nil,
		"payload_size":  len(req.Data.Payload.GetFields()),
	})

	s.logger.Sugar().Infof("Successfully ingested data for table: %s, ID: %s", tableName, recordID)
	return &miniodb.WriteDataResponse{
		Success: true,
		Message: fmt.Sprintf("Data successfully ingested for table: %s, ID: %s", tableName, recordID),
		NodeId:  s.cfg.Server.NodeID,
	}, nil
}

// SetSubscriptionManager 设置订阅管理器
func (s *MinIODBService) SetSubscriptionManager(manager *subscription.Manager) {
	s.subscriptionManager = manager
}

// publishWriteEvent 发布写入事件到订阅通道
func (s *MinIODBService) publishWriteEvent(ctx context.Context, table string, data *miniodb.DataRecord) {
	// 将 protobuf Struct 转换为 map
	payload := make(map[string]interface{})
	if data.Payload != nil {
		for k, v := range data.Payload.Fields {
			payload[k] = structValueToInterface(v)
		}
	}

	// 创建数据记录
	record := subscription.DataRecord{
		ID:        data.Id,
		Timestamp: data.Timestamp.AsTime().UnixMilli(),
		Payload:   payload,
	}

	// 创建写入事件
	event := subscription.NewInsertEvent(table, record).
		WithSource(s.cfg.Server.NodeID)

	// 发布到所有订阅者
	if err := s.subscriptionManager.PublishToAll(ctx, event); err != nil {
		s.logger.Warn("Failed to publish write event",
			zap.String("table", table),
			zap.String("record_id", data.Id),
			zap.Error(err))
	}
}

// structValueToInterface 将 protobuf Value 转换为 interface{}
func structValueToInterface(v *structpb.Value) interface{} {
	if v == nil {
		return nil
	}
	switch v.Kind.(type) {
	case *structpb.Value_NullValue:
		return nil
	case *structpb.Value_NumberValue:
		return v.GetNumberValue()
	case *structpb.Value_StringValue:
		return v.GetStringValue()
	case *structpb.Value_BoolValue:
		return v.GetBoolValue()
	case *structpb.Value_StructValue:
		result := make(map[string]interface{})
		for k, sv := range v.GetStructValue().Fields {
			result[k] = structValueToInterface(sv)
		}
		return result
	case *structpb.Value_ListValue:
		list := v.GetListValue().Values
		result := make([]interface{}, len(list))
		for i, lv := range list {
			result[i] = structValueToInterface(lv)
		}
		return result
	default:
		return nil
	}
}

// validateWriteRequest 验证写入请求（基于表配置）
func (s *MinIODBService) validateWriteRequest(req *miniodb.WriteDataRequest) error {
	if req.Data == nil {
		return status.Error(codes.InvalidArgument, "Data record is required")
	}

	// 获取表配置
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}
	tableConfig := s.GetTableConfig(context.Background(), tableName)

	// ID验证逻辑：
	// - AutoGenerateID = true: 用户可选提供ID，不提供则自动生成
	// - AutoGenerateID = false: 用户必须提供ID
	if req.Data.Id == "" {
		// 如果ID为空且不允许自动生成，则报错
		if !tableConfig.AutoGenerateID {
			return status.Error(codes.InvalidArgument, "ID is required for this table (auto_generate_id is disabled)")
		}
		// 否则允许为空，后续会自动生成
	}

	// 如果ID不为空，进行验证
	if req.Data.Id != "" {
		// 验证长度
		maxLength := tableConfig.IDValidation.MaxLength
		if maxLength <= 0 {
			maxLength = 255 // 默认最大长度
		}
		if len(req.Data.Id) > maxLength {
			return status.Error(codes.InvalidArgument,
				fmt.Sprintf("ID length %d exceeds maximum %d", len(req.Data.Id), maxLength))
		}

		// 验证格式（使用配置的正则模式）
		pattern := tableConfig.IDValidation.Pattern
		if pattern == "" {
			pattern = "^[a-zA-Z0-9_-]+$" // 默认模式
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return status.Error(codes.Internal, "Invalid ID validation pattern in config")
		}
		if !re.MatchString(req.Data.Id) {
			return status.Error(codes.InvalidArgument,
				fmt.Sprintf("ID contains invalid characters, must match pattern: %s", pattern))
		}
	}

	if req.Data.Payload == nil {
		return status.Error(codes.InvalidArgument, "Payload is required")
	}

	return nil
}

// systemDefaultTableConfig 返回配置缺失时的系统兜底配置。
// 系统默认使用 snowflake + 自动生成 ID，确保数据总是能写入。
func (s *MinIODBService) systemDefaultTableConfig() config.TableConfig {
	base := s.cfg.Tables.DefaultConfig
	base.IDStrategy = string(idgen.StrategySnowflake)
	base.AutoGenerateID = true
	return base
}

// GetTableConfig 获取表配置
func (s *MinIODBService) GetTableConfig(ctx context.Context, tableName string) config.TableConfig {
	// 优先从元数据管理器获取
	if s.metadataMgr != nil {
		cfg, err := s.metadataMgr.GetTableConfig(ctx, tableName)
		if err == nil && cfg != nil {
			s.logger.Sugar().Debugf("GetTableConfig: using metadataMgr for table %s, AutoGenerateID=%v", tableName, cfg.AutoGenerateID)
			return *cfg
		}
		// 配置不存在（数据丢失、误操作等）：使用系统默认并自动补写，避免服务中断
		if err == nil && cfg == nil {
			recovered := s.systemDefaultTableConfig()
			s.logger.Sugar().Warnf("GetTableConfig: table config missing for %s, recovering with system default (snowflake, auto_generate_id=true)", tableName)
			go func() {
				if saveErr := s.metadataMgr.SaveTableConfig(context.Background(), tableName, recovered); saveErr != nil {
					s.logger.Sugar().Warnf("GetTableConfig: failed to persist recovered config for %s: %v", tableName, saveErr)
				} else {
					s.logger.Sugar().Infof("GetTableConfig: persisted recovered config for table %s", tableName)
				}
			}()
			return recovered
		}
	}

	// 后备：从configManager获取
	if s.configManager != nil {
		cfg := s.configManager.GetTableConfig(ctx, tableName)
		s.logger.Sugar().Debugf("GetTableConfig: using configManager for table %s, AutoGenerateID=%v", tableName, cfg.AutoGenerateID)
		return cfg
	}

	// 如果都不可用，返回系统默认配置（snowflake + 自动生成）
	recovered := s.systemDefaultTableConfig()
	s.logger.Sugar().Warnf("GetTableConfig: no config source available for table %s, using system default", tableName)
	return recovered
}

// GenerateID 生成ID
func (s *MinIODBService) GenerateID(ctx context.Context, tableName string, tableConfig config.TableConfig) (string, error) {
	if s.idGenerator == nil {
		return "", fmt.Errorf("ID generator not initialized")
	}

	strategy := idgen.IDGeneratorStrategy(tableConfig.IDStrategy)
	if strategy == "" {
		strategy = idgen.StrategyUUID // 默认使用UUID
	}

	return s.idGenerator.Generate(tableName, strategy, tableConfig.IDPrefix)
}

// QueryData 查询数据
func (s *MinIODBService) QueryData(ctx context.Context, req *miniodb.QueryDataRequest) (*miniodb.QueryDataResponse, error) {
	s.logger.Sugar().Infof("Processing query request: %s", req.Sql)

	// 验证请求
	if err := s.validateQueryRequest(req); err != nil {
		return nil, err
	}

	// 处理向后兼容：将旧的"table"关键字替换为默认表名
	sql := req.Sql
	rewrittenSQL, err := s.rewriteLegacyTable(sql)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if rewrittenSQL != sql {
		sql = rewrittenSQL
		s.logger.Sugar().Infof("Converted legacy SQL to use default table: %s", sql)
	}

	// 如果指定了限制，添加到SQL中
	if req.Limit > 0 {
		sql = fmt.Sprintf("%s LIMIT %d", sql, req.Limit)
	}

	// 最终验证：确保修改后的SQL仍然安全
	if err := s.validateQueryRequest(&miniodb.QueryDataRequest{Sql: sql}); err != nil {
		return nil, err
	}

	// 使用Querier执行查询
	result, err := s.querier.ExecuteQuery(sql)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Query failed: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Query execution failed: %v", err))
	}

	// 目前返回全部结果
	hasMore := false
	nextCursor := ""

	s.logger.Sugar().Infof("Query completed successfully, result length: %d characters", len(result))
	return &miniodb.QueryDataResponse{
		ResultJson: result,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// validateQueryRequest 验证查询请求
func (s *MinIODBService) validateQueryRequest(req *miniodb.QueryDataRequest) error {
	if err := security.DefaultSanitizer.ValidateSelectQuery(req.Sql); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return nil
}

// rewriteLegacyTable 重写查询中的旧式表名
// 将 "FROM table" 或 "from table" 替换为实际的默认表名
// 注意：只替换完整的单词"table"，不替换包含"table"的表名
func (s *MinIODBService) rewriteLegacyTable(sql string) (string, error) {
	if !strings.Contains(strings.ToLower(sql), "from table") {
		return sql, nil
	}

	defaultTable := s.cfg.TableManagement.DefaultTable

	if !s.cfg.IsValidTableName(defaultTable) {
		return "", fmt.Errorf("invalid default table name: %s", defaultTable)
	}

	quotedTable := security.DefaultSanitizer.QuoteIdentifier(defaultTable)

	// 精确匹配并替换：FROM 后跟空格，然后是完整的"table"单词
	// 使用正则边界匹配确保只替换"FROM table"或"from table"，不替换"FROM table_data"
	sql = fromTableRegex.ReplaceAllString(sql, "FROM "+quotedTable)

	return sql, nil
}

// UpdateData 更新数据
func (s *MinIODBService) UpdateData(ctx context.Context, req *miniodb.UpdateDataRequest) (*miniodb.UpdateDataResponse, error) {
	// 处理表名：优先使用请求中的表名，如果为空则使用默认表
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	s.logger.Sugar().Infof("Processing update request for table: %s, ID: %s", tableName, req.Id)

	// 验证请求
	if err := s.validateUpdateRequest(req); err != nil {
		s.auditLogger.LogUpdate(ctx, tableName, req.Id, false, err, nil)
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		err := fmt.Errorf("invalid table name: %s", tableName)
		s.auditLogger.LogUpdate(ctx, tableName, req.Id, false, err, nil)
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", tableName),
		}, nil
	}

	// 确保表存在
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to ensure table exists: %v", err)
		s.auditLogger.LogUpdate(ctx, tableName, req.Id, false, err, nil)
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: fmt.Sprintf("Table error: %v", err),
		}, nil
	}

	// 获取分布式锁，防止并发更新同一记录
	lockKey := lock.LockKey(tableName, req.Id)
	lockTTL := 30 * time.Second
	token, err := s.locker.Lock(ctx, lockKey, lockTTL)
	if err != nil {
		s.logger.Sugar().Warnf("Failed to acquire lock for update: %v", err)
		s.auditLogger.LogUpdate(ctx, tableName, req.Id, false, err, map[string]interface{}{"lock_failed": true})
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to acquire lock: %v", err),
		}, nil
	}
	defer func() {
		if unlockErr := s.locker.Unlock(context.Background(), lockKey, token); unlockErr != nil {
			s.logger.Sugar().Warnf("Failed to release lock: %v", unlockErr)
		}
	}()

	// OLAP系统中的更新策略：先插入后删除
	// 策略说明：先 INSERT 新版本数据，成功后再 DELETE 旧版本
	// - INSERT 失败 → 直接返回错误，旧数据保持不变（无数据丢失）
	// - DELETE 失败 → 短暂存在重复数据，但不丢失数据（可接受）
	// 这避免了"DELETE 成功但 INSERT 失败"导致的数据永久丢失问题

	// 0. 从 Buffer 中移除旧版本数据（未 flush 的数据）
	if s.ingester != nil {
		bufferRemoved := s.ingester.RemoveFromBuffer(tableName, req.Id)
		if bufferRemoved > 0 {
			s.logger.Sugar().Infof("Removed %d old records from buffer before update for table %s, ID %s", bufferRemoved, tableName, req.Id)
		}
	}

	// 1. 首先插入更新后的数据
	if s.ingester != nil {
		// 构建写入请求
		writeReq := &miniodb.WriteRequest{
			Table:     tableName,
			Id:        req.Id,
			Timestamp: req.Timestamp,
			Payload:   req.Payload,
		}

		// 如果没有提供时间戳，使用当前时间
		if writeReq.Timestamp == nil {
			writeReq.Timestamp = timestamppb.Now()
		}

		// 执行插入
		if err := s.ingester.IngestData(writeReq); err != nil {
			s.logger.Sugar().Infof("ERROR: Failed to ingest updated data for table %s, ID %s: %v", tableName, req.Id, err)
			s.auditLogger.LogUpdate(ctx, tableName, req.Id, false, err, map[string]interface{}{"phase": "insert"})
			return &miniodb.UpdateDataResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to update record: %v", err),
			}, nil
		}
	} else {
		err := fmt.Errorf("ingester not available for update operation")
		s.auditLogger.LogUpdate(ctx, tableName, req.Id, false, err, nil)
		return &miniodb.UpdateDataResponse{
			Success: false,
			Message: "Ingester not available for update operation",
		}, nil
	}

	// 2. INSERT 成功后，删除旧版本记录
	removedIndexKeys := 0
	if removed, err := s.removePersistedIndexByID(ctx, tableName, req.Id); err != nil {
		s.logger.Sugar().Warnf("Failed to remove persisted index for table %s, ID %s: %v", tableName, req.Id, err)
	} else {
		removedIndexKeys = removed
		if removedIndexKeys > 0 {
			s.logger.Sugar().Infof("Removed %d persisted index keys for table %s, ID %s", removedIndexKeys, tableName, req.Id)
		}
	}

	if s.querier != nil {
		deleteSQL, err := security.DefaultSanitizer.BuildSafeDeleteSQL(tableName, req.Id)
		if err != nil {
			// 构建删除语句失败，记录警告但不影响整体操作（新数据已插入）
			s.logger.Sugar().Warnf("Failed to build safe delete SQL for cleanup: %v", err)
		} else {
			_, err = s.querier.ExecuteUpdate(deleteSQL)
			if err != nil {
				errMsg := err.Error()
				if strings.Contains(errMsg, "Can only delete from base table") {
					s.logger.Sugar().Infof("Skip cleanup delete for view-based table %s (not supported)", tableName)
				} else {
					s.logger.Sugar().Warnf("Delete query during update failed (data may be duplicated temporarily): %v", err)
				}
			}
		}
	}

	// 3. 清理相关的缓存
	if s.querier != nil {
		if err := s.querier.InvalidateCache(ctx, []string{tableName}); err != nil {
			s.logger.Sugar().Warnf("Failed to invalidate query cache for table %s: %v", tableName, err)
		} else {
			s.logger.Sugar().Infof("Invalidated query cache for table %s after update", tableName)
		}
	}

	// 4. 更新表的最后写入时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		s.logger.Sugar().Infof("WARN: Failed to update last write time for table %s: %v", tableName, err)
	}

	// 记录成功的审计日志
	s.auditLogger.LogUpdate(ctx, tableName, req.Id, true, nil, map[string]interface{}{
		"has_timestamp":      req.Timestamp != nil,
		"payload_size":       len(req.Payload.GetFields()),
		"removed_index_keys": removedIndexKeys,
	})

	s.logger.Sugar().Infof("Successfully updated record %s in table %s", req.Id, tableName)
	return &miniodb.UpdateDataResponse{
		Success: true,
		Message: fmt.Sprintf("Record %s updated successfully in table %s", req.Id, tableName),
	}, nil
}

// validateUpdateRequest 验证更新请求
func (s *MinIODBService) validateUpdateRequest(req *miniodb.UpdateDataRequest) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "ID is required and cannot be empty")
	}

	if len(req.Id) > 255 {
		return status.Error(codes.InvalidArgument, "ID cannot exceed 255 characters")
	}

	if req.Payload == nil {
		return status.Error(codes.InvalidArgument, "Payload is required")
	}

	// 验证ID格式
	for _, r := range req.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "ID contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}

	return nil
}

// DeleteData 删除数据
func (s *MinIODBService) DeleteData(ctx context.Context, req *miniodb.DeleteDataRequest) (*miniodb.DeleteDataResponse, error) {
	// 处理表名：优先使用请求中的表名，如果为空则使用默认表
	tableName := req.Table
	if tableName == "" {
		tableName = s.cfg.TableManagement.DefaultTable
	}

	s.logger.Sugar().Infof("Processing delete request for table: %s, ID: %s", tableName, req.Id)

	// 验证请求
	if err := s.validateDeleteRequest(req); err != nil {
		s.auditLogger.LogDelete(ctx, tableName, req.Id, false, err, nil)
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		err := fmt.Errorf("invalid table name: %s", tableName)
		s.auditLogger.LogDelete(ctx, tableName, req.Id, false, err, nil)
		return &miniodb.DeleteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", tableName),
		}, nil
	}

	// 确保表存在
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to ensure table exists: %v", err)
		s.auditLogger.LogDelete(ctx, tableName, req.Id, false, err, nil)
		return &miniodb.DeleteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Table error: %v", err),
		}, nil
	}

	// 获取分布式锁，防止并发删除同一记录
	lockKey := lock.LockKey(tableName, req.Id)
	lockTTL := 30 * time.Second
	token, err := s.locker.Lock(ctx, lockKey, lockTTL)
	if err != nil {
		s.logger.Sugar().Warnf("Failed to acquire lock for delete: %v", err)
		s.auditLogger.LogDelete(ctx, tableName, req.Id, false, err, map[string]interface{}{"lock_failed": true})
		return &miniodb.DeleteDataResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to acquire lock: %v", err),
		}, nil
	}
	defer func() {
		if unlockErr := s.locker.Unlock(context.Background(), lockKey, token); unlockErr != nil {
			s.logger.Sugar().Warnf("Failed to release lock: %v", unlockErr)
		}
	}()

	deletedCount := int32(0)
	foundInStorage := false

	// 1. 从 Buffer 中移除未 flush 的数据
	if s.ingester != nil {
		bufferRemoved := s.ingester.RemoveFromBuffer(tableName, req.Id)
		if bufferRemoved > 0 {
			deletedCount += int32(bufferRemoved)
			s.logger.Sugar().Infof("Removed %d records from buffer for table %s, ID %s", bufferRemoved, tableName, req.Id)
		}
	}

	// 2. 从已持久化的数据中删除（重写 parquet 文件，物理删除记录，同时清理 Redis 索引）
	if s.querier != nil {
		// 通过重写 parquet 文件实现物理删除（不依赖 DuckDB DELETE，视图也支持）
		rowsAffected, found, err := s.querier.RewriteParquetForDelete(ctx, tableName, req.Id)
		if err != nil {
			s.logger.Sugar().Warnf("WARN: Parquet rewrite delete failed for table %s id %s: %v", tableName, req.Id, err)
			s.auditLogger.LogDelete(ctx, tableName, req.Id, false, err, map[string]interface{}{"phase": "rewrite_parquet"})
			return &miniodb.DeleteDataResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to delete record from storage: %v", err),
			}, nil
		}
		if found {
			foundInStorage = true
		}
		if rowsAffected > 0 {
			deletedCount += int32(rowsAffected)
		}
	}

	// 2. 清理相关的缓存和索引
	if s.querier != nil {
		if err := s.querier.InvalidateCache(ctx, []string{tableName}); err != nil {
			s.logger.Sugar().Warnf("Failed to invalidate query cache for table %s: %v", tableName, err)
		} else {
			s.logger.Sugar().Infof("Invalidated query cache for table %s after delete", tableName)
		}
	}
	if s.redisPool != nil {
		redisClient := s.redisPool.GetClient()

		// 清理相关的缓存项
		cachePattern := fmt.Sprintf("cache:table:%s:id:%s:*", tableName, req.Id)
		if keys, err := pool.ScanKeys(ctx, redisClient, cachePattern); err == nil {
			if len(keys) > 0 {
				redisClient.Del(ctx, keys...)
				s.logger.Sugar().Infof("Cleaned %d cache entries for deleted record", len(keys))
			}
		}

		// 更新表的记录计数（只有实际删除了行才递减）
		if deletedCount > 0 || foundInStorage {
			recordCountKey := fmt.Sprintf("table:%s:record_count", tableName)
			redisClient.Decr(ctx, recordCountKey)
		}
	}

	// 3. 更新表的最后修改时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		s.logger.Sugar().Infof("WARN: Failed to update last write time for table %s: %v", tableName, err)
	}

	// 记录审计日志
	if deletedCount > 0 || foundInStorage {
		s.auditLogger.LogDelete(ctx, tableName, req.Id, true, nil, map[string]interface{}{
			"deleted_count": deletedCount,
		})
		s.logger.Sugar().Infof("Successfully deleted records for ID %s from table %s", req.Id, tableName)
		return &miniodb.DeleteDataResponse{
			Success:      true,
			Message:      fmt.Sprintf("Record %s deleted successfully from table %s", req.Id, tableName),
			DeletedCount: deletedCount,
		}, nil
	} else {
		// 记录审计日志（未找到记录）
		s.auditLogger.LogDelete(ctx, tableName, req.Id, false, nil, map[string]interface{}{
			"deleted_count": 0,
			"reason":        "record_not_found",
		})
		return &miniodb.DeleteDataResponse{
			Success:      false,
			Message:      fmt.Sprintf("No records found with ID %s in table %s", req.Id, tableName),
			DeletedCount: 0,
		}, nil
	}
}

// removePersistedIndexByID 删除某条记录在 Redis 中的持久化索引，避免旧文件继续参与查询
func (s *MinIODBService) removePersistedIndexByID(ctx context.Context, tableName, id string) (int, error) {
	if s.redisPool == nil {
		return 0, nil
	}
	redisClient := s.redisPool.GetClient()
	pattern := fmt.Sprintf("index:table:%s:id:%s:*", tableName, id)
	keys, err := pool.ScanKeys(ctx, redisClient, pattern)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	if err := redisClient.Del(ctx, keys...).Err(); err != nil {
		return 0, err
	}
	return len(keys), nil
}

// validateDeleteRequest 验证删除请求
func (s *MinIODBService) validateDeleteRequest(req *miniodb.DeleteDataRequest) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "ID is required and cannot be empty")
	}

	if len(req.Id) > 255 {
		return status.Error(codes.InvalidArgument, "ID cannot exceed 255 characters")
	}

	// 验证ID格式
	for _, r := range req.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "ID contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}

	return nil
}

// CleanupEmptyIDRecords 清理表中空ID的脏数据
func (s *MinIODBService) CleanupEmptyIDRecords(ctx context.Context, tableName string) (int64, error) {
	s.logger.Sugar().Infof("Starting cleanup of empty ID records for table: %s", tableName)

	if s.querier == nil {
		return 0, fmt.Errorf("querier not available")
	}

	deleteSQL, err := security.DefaultSanitizer.BuildSafeDeleteEmptyIDSQL(tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to build delete SQL: %w", err)
	}

	rowsAffected, err := s.querier.ExecuteUpdate(deleteSQL)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "does not exist") ||
			strings.Contains(errMsg, "no data files") ||
			strings.Contains(errMsg, query.ErrNoDataFiles.Error()) {
			s.logger.Sugar().Infof("Table %s has no persisted data, no empty ID records to clean", tableName)
			return 0, nil
		}
		s.logger.Sugar().Errorf("Failed to cleanup empty ID records for table %s: %v", tableName, err)
		return 0, fmt.Errorf("failed to execute cleanup: %w", err)
	}

	s.logger.Sugar().Infof("Cleaned up %d empty ID records from table %s", rowsAffected, tableName)
	s.auditLogger.LogDelete(ctx, tableName, "<empty_id_cleanup>", true, nil, map[string]interface{}{
		"cleanup_type":  "empty_id",
		"deleted_count": rowsAffected,
		"table":         tableName,
	})

	return rowsAffected, nil
}

// ConvertResultToRecords 将JSON结果转换为DataRecord列表
func (s *MinIODBService) ConvertResultToRecords(resultJson string) ([]*miniodb.DataRecord, error) {
	var rawData []map[string]interface{}
	if err := json.Unmarshal([]byte(resultJson), &rawData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON result: %w", err)
	}

	var records []*miniodb.DataRecord
	for _, row := range rawData {
		record := &miniodb.DataRecord{}

		// 提取ID
		if id, ok := row["id"].(string); ok {
			record.Id = id
		}

		// 提取时间戳
		if timestampStr, ok := row["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, timestampStr); err == nil {
				record.Timestamp = timestamppb.New(t)
			}
		}

		// 提取负载数据 - 移除已处理的字段
		payload := make(map[string]interface{})
		for k, v := range row {
			if k != "id" && k != "timestamp" {
				payload[k] = v
			}
		}

		// 将map转换为protobuf Struct
		protoStruct, err := s.mapToProtobufStruct(payload)
		if err != nil {
			s.logger.Sugar().Infof("WARN: Failed to convert payload to protobuf struct: %v", err)
			// 创建一个空的Struct而不是跳过整个记录
			protoStruct = &structpb.Struct{Fields: make(map[string]*structpb.Value)}
		}
		record.Payload = protoStruct

		records = append(records, record)
	}

	return records, nil
}

// mapToProtobufStruct 将map[string]interface{}转换为protobuf Struct
func (s *MinIODBService) mapToProtobufStruct(data map[string]interface{}) (*structpb.Struct, error) {
	fields := make(map[string]*structpb.Value)

	for key, value := range data {
		protoValue, err := s.interfaceToProtobufValue(value)
		if err != nil {
			s.logger.Sugar().Infof("WARN: Failed to convert field %s: %v", key, err)
			// 跳过有问题的字段，而不是整个转换失败
			continue
		}
		fields[key] = protoValue
	}

	return &structpb.Struct{Fields: fields}, nil
}

// interfaceToProtobufValue 将interface{}转换为protobuf Value
func (s *MinIODBService) interfaceToProtobufValue(value interface{}) (*structpb.Value, error) {
	if value == nil {
		return structpb.NewNullValue(), nil
	}

	switch v := value.(type) {
	case bool:
		return structpb.NewBoolValue(v), nil
	case int:
		return structpb.NewNumberValue(float64(v)), nil
	case int32:
		return structpb.NewNumberValue(float64(v)), nil
	case int64:
		return structpb.NewNumberValue(float64(v)), nil
	case float32:
		return structpb.NewNumberValue(float64(v)), nil
	case float64:
		return structpb.NewNumberValue(v), nil
	case string:
		return structpb.NewStringValue(v), nil
	case []interface{}:
		// 处理数组
		var listValues []*structpb.Value
		for _, item := range v {
			itemValue, err := s.interfaceToProtobufValue(item)
			if err != nil {
				s.logger.Sugar().Infof("WARN: Failed to convert array item: %v", err)
				continue
			}
			listValues = append(listValues, itemValue)
		}
		return structpb.NewListValue(&structpb.ListValue{Values: listValues}), nil
	case map[string]interface{}:
		// 处理嵌套对象
		nestedStruct, err := s.mapToProtobufStruct(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert nested map: %w", err)
		}
		return structpb.NewStructValue(nestedStruct), nil
	default:
		// 对于未知类型，尝试转换为字符串
		return structpb.NewStringValue(fmt.Sprintf("%v", v)), nil
	}
}

// StreamWrite 流式写入数据
func (s *MinIODBService) StreamWrite(stream miniodb.MinIODBService_StreamWriteServer) error {
	s.logger.Sugar().Infof("Starting stream write session")

	ctx := stream.Context()
	successCount := int32(0)
	errorCount := int32(0)
	var lastError error

	for {
		select {
		case <-ctx.Done():
			s.logger.Sugar().Infof("Stream write cancelled by client")
			return ctx.Err()
		default:
		}

		// 接收写入请求
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// 客户端结束流，发送最终响应
				var errors []string
				if lastError != nil {
					errors = append(errors, lastError.Error())
				}

				finalResponse := &miniodb.StreamWriteResponse{
					Success:      errorCount == 0,
					RecordsCount: int64(successCount),
					Errors:       errors,
				}

				s.logger.Sugar().Infof("Stream write completed: %d success, %d errors", successCount, errorCount)
				return stream.SendAndClose(finalResponse)
			}
			s.logger.Sugar().Infof("ERROR: Failed to receive stream write request: %v", err)
			return status.Error(codes.Internal, fmt.Sprintf("Failed to receive request: %v", err))
		}

		// 处理批量写入请求
		batchSize := len(req.Records)
		if err := s.processStreamWriteRequest(ctx, req); err != nil {
			errorCount += int32(batchSize)
			lastError = err
			s.logger.Sugar().Infof("ERROR: Stream write failed for %d records in table %s: %v", batchSize, req.Table, err)
		} else {
			successCount += int32(batchSize)
			s.logger.Sugar().Infof("Stream write success for %d records in table %s", batchSize, req.Table)
		}

		// 可以选择是否在每次写入后发送确认（这里简化为只在最后发送）
		// 如果需要实时反馈，可以调用stream.Send()发送中间响应
	}
}

// processStreamWriteRequest 处理批量流式写入请求
func (s *MinIODBService) processStreamWriteRequest(ctx context.Context, req *miniodb.StreamWriteRequest) error {
	// 处理批量记录
	for _, record := range req.Records {
		// 转换为标准写入请求
		writeReq := &miniodb.WriteDataRequest{
			Table: req.Table,
			Data:  record,
		}

		// 复用现有的写入逻辑
		response, err := s.WriteData(ctx, writeReq)
		if err != nil {
			return fmt.Errorf("record %s failed: %v", record.Id, err)
		}

		if !response.Success {
			return fmt.Errorf("record %s failed: %s", record.Id, response.Message)
		}
	}

	return nil
}

// StreamQuery 流式查询数据
func (s *MinIODBService) StreamQuery(req *miniodb.StreamQueryRequest, stream miniodb.MinIODBService_StreamQueryServer) error {
	s.logger.Sugar().Infof("Processing stream query request: %s", req.Sql)

	// 验证请求
	if err := s.validateStreamQueryRequest(req); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// 设置默认批次大小
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100 // 默认批次大小
	}

	// 执行查询
	resultJson, err := s.querier.ExecuteQuery(req.Sql)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Stream query failed: %v", err)
		return status.Error(codes.Internal, fmt.Sprintf("Query failed: %v", err))
	}

	// 转换查询结果为记录
	records, err := s.ConvertResultToRecords(resultJson)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to convert query result to records: %v", err)
		return status.Error(codes.Internal, fmt.Sprintf("Failed to convert result: %v", err))
	}

	// 分批发送结果
	totalRecords := len(records)
	offset := 0

	// 处理游标（简单实现：游标表示起始位置）
	if req.Cursor != "" {
		if startOffset, err := strconv.Atoi(req.Cursor); err == nil && startOffset > 0 {
			offset = startOffset
		}
	}

	for offset < totalRecords {
		// 计算当前批次的结束位置
		end := offset + int(batchSize)
		if end > totalRecords {
			end = totalRecords
		}

		// 准备当前批次的记录
		batch := records[offset:end]

		// 检查是否有更多数据
		hasMore := end < totalRecords

		// 生成下一个游标
		var nextCursor string
		if hasMore {
			nextCursor = strconv.Itoa(end)
		}

		// 发送批次数据
		response := &miniodb.StreamQueryResponse{
			Records: batch,
			HasMore: hasMore,
			Cursor:  nextCursor,
		}

		if err := stream.Send(response); err != nil {
			s.logger.Sugar().Infof("ERROR: Failed to send stream response: %v", err)
			return status.Error(codes.Internal, fmt.Sprintf("Failed to send response: %v", err))
		}

		s.logger.Sugar().Infof("Sent batch of %d records (offset: %d, hasMore: %t)", len(batch), offset, hasMore)

		// 移动到下一批次
		offset = end

		// 检查上下文是否被取消
		if stream.Context().Err() != nil {
			s.logger.Sugar().Infof("Stream query cancelled by client")
			return status.Error(codes.Canceled, "Stream query cancelled")
		}
	}

	s.logger.Sugar().Infof("Stream query completed successfully, total records: %d", totalRecords)
	return nil
}

// validateStreamQueryRequest 验证流式查询请求
func (s *MinIODBService) validateStreamQueryRequest(req *miniodb.StreamQueryRequest) error {
	if req.Sql == "" {
		return fmt.Errorf("SQL query is required")
	}

	if req.BatchSize < 0 {
		return fmt.Errorf("batch_size must be non-negative")
	}

	if req.BatchSize > 10000 {
		return fmt.Errorf("batch_size too large, maximum is 10000")
	}

	return nil
}

// CreateTable 创建表
func (s *MinIODBService) CreateTable(ctx context.Context, req *miniodb.CreateTableRequest) (*miniodb.CreateTableResponse, error) {
	s.logger.Sugar().Infof("Processing create table request: %s", req.TableName)

	// 验证表名
	if req.TableName == "" {
		return &miniodb.CreateTableResponse{
			Success: false,
			Message: "Table name is required",
		}, nil
	}

	if !s.cfg.IsValidTableName(req.TableName) {
		return &miniodb.CreateTableResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid table name: %s", req.TableName),
		}, nil
	}

	// 转换配置
	var tableConfig *config.TableConfig
	if req.Config != nil {
		// ID验证规则
		idValidation := config.IDValidationRules{
			MaxLength:    255, // 默认值
			Pattern:      "^[a-zA-Z0-9_-]+$",
			AllowedChars: "",
		}
		if req.Config.IdValidation != nil {
			idValidation.MaxLength = int(req.Config.IdValidation.MaxLength)
			if req.Config.IdValidation.Pattern != "" {
				idValidation.Pattern = req.Config.IdValidation.Pattern
			}
			idValidation.AllowedChars = req.Config.IdValidation.AllowedChars
		}

		tableConfig = &config.TableConfig{
			BufferSize:     int(req.Config.BufferSize),
			FlushInterval:  time.Duration(req.Config.FlushIntervalSeconds) * time.Second,
			RetentionDays:  int(req.Config.RetentionDays),
			BackupEnabled:  req.Config.BackupEnabled,
			Properties:     req.Config.Properties,
			IDStrategy:     req.Config.IdStrategy,
			IDPrefix:       req.Config.IdPrefix,
			AutoGenerateID: req.Config.AutoGenerateId,
			IDValidation:   idValidation,
		}
	}

	// 创建表
	err := s.tableManager.CreateTable(ctx, req.TableName, tableConfig, req.IfNotExists)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to create table %s: %v", req.TableName, err)
		return &miniodb.CreateTableResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create table: %v", err),
		}, nil
	}

	// 保存表配置到元数据管理器（优先）
	if s.metadataMgr != nil && tableConfig != nil {
		if err := s.metadataMgr.SaveTableConfig(ctx, req.TableName, *tableConfig); err != nil {
			s.logger.Sugar().Infof("WARN: Failed to save table config to metadata: %v", err)
			// 不影响表创建成功，继续尝试保存到configManager
		}
	}

	// 兼容：同时保存到configManager（如果可用）
	if s.configManager != nil && tableConfig != nil {
		if err := s.configManager.SetTableConfig(ctx, req.TableName, *tableConfig); err != nil {
			s.logger.Sugar().Infof("WARN: Failed to save table config to cache: %v", err)
			// 不影响表创建成功
		}
	}

	idStrategy := ""
	if tableConfig != nil {
		idStrategy = tableConfig.IDStrategy
	}
	s.logger.Sugar().Infof("Successfully created table: %s with ID strategy: %s", req.TableName, idStrategy)
	return &miniodb.CreateTableResponse{
		Success: true,
		Message: fmt.Sprintf("Table %s created successfully", req.TableName),
	}, nil
}

// ListTables 列出表
func (s *MinIODBService) ListTables(ctx context.Context, req *miniodb.ListTablesRequest) (*miniodb.ListTablesResponse, error) {
	s.logger.Sugar().Infof("Processing list tables request with pattern: %s", req.Pattern)

	tables, err := s.tableManager.ListTables(ctx, req.Pattern)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to list tables: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to list tables: %v", err))
	}

	var tableInfos []*miniodb.TableInfo
	for _, table := range tables {
		// 解析时间字符串为protobuf时间戳
		var createdAt *timestamppb.Timestamp
		if table.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, table.CreatedAt); err == nil {
				createdAt = timestamppb.New(t)
			}
		}

		var lastWrite *timestamppb.Timestamp
		if table.LastWrite != "" {
			if t, err := time.Parse(time.RFC3339, table.LastWrite); err == nil {
				lastWrite = timestamppb.New(t)
			}
		}

		// 转换配置
		var config *miniodb.TableConfig
		if table.Config != nil {
			config = &miniodb.TableConfig{
				BufferSize:           int32(table.Config.BufferSize),
				FlushIntervalSeconds: int32(table.Config.FlushInterval.Seconds()),
				RetentionDays:        int32(table.Config.RetentionDays),
				BackupEnabled:        table.Config.BackupEnabled,
				Properties:           table.Config.Properties,
			}
		}

		tableInfo := &miniodb.TableInfo{
			Name:      table.Name,
			Config:    config,
			CreatedAt: createdAt,
			LastWrite: lastWrite,
			Status:    table.Status,
			// Stats: 需要时再填充
		}
		tableInfos = append(tableInfos, tableInfo)
	}

	return &miniodb.ListTablesResponse{
		Tables: tableInfos,
	}, nil
}

// GetTable 获取表信息
func (s *MinIODBService) GetTable(ctx context.Context, req *miniodb.GetTableRequest) (*miniodb.GetTableResponse, error) {
	s.logger.Sugar().Infof("Processing get table request: %s", req.TableName)

	if req.TableName == "" {
		return nil, status.Error(codes.InvalidArgument, "Table name is required")
	}

	// 检查表是否存在
	exists, err := s.tableManager.TableExists(ctx, req.TableName)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to check table existence: %v", err))
	}

	if !exists {
		return &miniodb.GetTableResponse{
			TableInfo: nil,
		}, nil
	}

	// 获取表信息和统计
	tableInfo, tableStats, err := s.tableManager.DescribeTable(ctx, req.TableName)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to get table information: %v", err))
	}

	// 转换时间戳
	var createdAt *timestamppb.Timestamp
	if tableInfo.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, tableInfo.CreatedAt); err == nil {
			createdAt = timestamppb.New(t)
		}
	}

	var lastWrite *timestamppb.Timestamp
	if tableInfo.LastWrite != "" {
		if t, err := time.Parse(time.RFC3339, tableInfo.LastWrite); err == nil {
			lastWrite = timestamppb.New(t)
		}
	}

	// 转换配置
	var config *miniodb.TableConfig
	if tableInfo.Config != nil {
		config = &miniodb.TableConfig{
			BufferSize:           int32(tableInfo.Config.BufferSize),
			FlushIntervalSeconds: int32(tableInfo.Config.FlushInterval.Seconds()),
			RetentionDays:        int32(tableInfo.Config.RetentionDays),
			BackupEnabled:        tableInfo.Config.BackupEnabled,
			Properties:           tableInfo.Config.Properties,
		}
	}

	// 转换统计信息
	var stats *miniodb.TableStats
	if tableStats != nil {
		var oldestRecord, newestRecord *timestamppb.Timestamp
		if tableStats.OldestRecord != "" {
			if t, err := time.Parse(time.RFC3339, tableStats.OldestRecord); err == nil {
				oldestRecord = timestamppb.New(t)
			}
		}
		if tableStats.NewestRecord != "" {
			if t, err := time.Parse(time.RFC3339, tableStats.NewestRecord); err == nil {
				newestRecord = timestamppb.New(t)
			}
		}

		stats = &miniodb.TableStats{
			RecordCount:  tableStats.RecordCount,
			FileCount:    tableStats.FileCount,
			SizeBytes:    tableStats.SizeBytes,
			OldestRecord: oldestRecord,
			NewestRecord: newestRecord,
		}
	}

	table := &miniodb.TableInfo{
		Name:      tableInfo.Name,
		Config:    config,
		CreatedAt: createdAt,
		LastWrite: lastWrite,
		Status:    tableInfo.Status,
		Stats:     stats,
	}

	return &miniodb.GetTableResponse{
		TableInfo: table,
	}, nil
}

// DeleteTable 删除表
func (s *MinIODBService) DeleteTable(ctx context.Context, req *miniodb.DeleteTableRequest) (*miniodb.DeleteTableResponse, error) {
	s.logger.Sugar().Infof("Processing delete table request: %s", req.TableName)

	if req.TableName == "" {
		return nil, status.Error(codes.InvalidArgument, "Table name is required")
	}

	deletedFiles, err := s.tableManager.DropTable(ctx, req.TableName, req.IfExists, req.Cascade)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to delete table %s: %v", req.TableName, err)
		return &miniodb.DeleteTableResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to delete table: %v", err),
		}, nil
	}

	return &miniodb.DeleteTableResponse{
		Success:      true,
		Message:      fmt.Sprintf("Table %s deleted successfully", req.TableName),
		FilesDeleted: deletedFiles,
	}, nil
}

// BackupMetadata 备份元数据
func (s *MinIODBService) BackupMetadata(ctx context.Context, req *miniodb.BackupMetadataRequest) (*miniodb.BackupMetadataResponse, error) {
	s.logger.Sugar().Infof("Processing backup metadata request, force: %v", req.Force)

	if s.metadataMgr == nil {
		return &miniodb.BackupMetadataResponse{
			Success: false,
			Message: "Metadata manager not available (standalone mode or metadata backup disabled)",
		}, nil
	}

	// 获取备份管理器
	backupManager := s.metadataMgr.GetBackupManager()
	if backupManager == nil {
		return &miniodb.BackupMetadataResponse{
			Success:   false,
			Message:   "Backup manager not available",
			BackupId:  "",
			Timestamp: nil,
		}, nil
	}

	// 执行手动备份
	if err := s.metadataMgr.ManualBackup(ctx); err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to create backup: %v", err)
		return &miniodb.BackupMetadataResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to create backup: %v", err),
			BackupId:  "",
			Timestamp: nil,
		}, nil
	}

	// 获取最新备份信息以返回备份ID
	recoveryManager := s.metadataMgr.GetRecoveryManager()
	backupTime := time.Now()
	if recoveryManager != nil {
		if latestBackup, err := recoveryManager.GetLatestBackup(ctx); err == nil {
			return &miniodb.BackupMetadataResponse{
				Success:   true,
				Message:   "Backup completed successfully",
				BackupId:  latestBackup.ObjectName,
				Timestamp: timestamppb.New(latestBackup.Timestamp),
			}, nil
		}
	}

	// 如果无法获取备份信息，仍然返回成功
	return &miniodb.BackupMetadataResponse{
		Success:   true,
		Message:   "Backup completed successfully",
		BackupId:  fmt.Sprintf("backup_%d", backupTime.Unix()),
		Timestamp: timestamppb.New(backupTime),
	}, nil
}

// RestoreMetadata 恢复元数据
func (s *MinIODBService) RestoreMetadata(ctx context.Context, req *miniodb.RestoreMetadataRequest) (*miniodb.RestoreMetadataResponse, error) {
	s.logger.Sugar().Infof("Processing restore metadata request, backup_file: %s, from_latest: %v", req.BackupFile, req.FromLatest)

	if s.metadataMgr == nil {
		return &miniodb.RestoreMetadataResponse{
			Success: false,
			Message: "Metadata manager not available (standalone mode or metadata backup disabled)",
		}, nil
	}

	// 获取恢复管理器
	recoveryManager := s.metadataMgr.GetRecoveryManager()
	if recoveryManager == nil {
		return &miniodb.RestoreMetadataResponse{
			Success: false,
			Message: "Recovery manager not available",
		}, nil
	}

	// 构建恢复选项
	options := metadata.RecoveryOptions{
		Overwrite: req.Overwrite,
		Validate:  req.Validate,
		DryRun:    req.DryRun,
		Parallel:  req.Parallel,
		Filters:   make(map[string]interface{}),
	}

	// 转换filters
	for k, v := range req.Filters {
		options.Filters[k] = v
	}

	// 设置键模式过滤
	if len(req.KeyPatterns) > 0 {
		options.KeyPatterns = req.KeyPatterns
	}

	// 设置恢复模式
	if req.DryRun {
		options.Mode = metadata.RecoveryModeDryRun
	} else {
		options.Mode = metadata.RecoveryModeComplete
	}

	var result *metadata.RecoveryResult
	var err error

	// 执行恢复
	if req.FromLatest || req.BackupFile == "" {
		s.logger.Sugar().Infof("Recovering from latest backup")
		result, err = recoveryManager.RecoverFromLatest(ctx, options)
	} else {
		s.logger.Sugar().Infof("Recovering from backup file: %s", req.BackupFile)
		result, err = recoveryManager.RecoverFromBackup(ctx, req.BackupFile, options)
	}

	if err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to restore metadata: %v", err)
		return &miniodb.RestoreMetadataResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to restore metadata: %v", err),
		}, nil
	}

	// 构建响应
	response := &miniodb.RestoreMetadataResponse{
		Success:        result.Success,
		Message:        "Metadata restored successfully",
		BackupFile:     result.BackupObjectName,
		EntriesTotal:   int32(result.EntriesTotal),
		EntriesOk:      int32(result.EntriesOK),
		EntriesSkipped: int32(result.EntriesSkipped),
		EntriesError:   int32(result.EntriesError),
		Duration:       result.Duration.String(),
		Errors:         result.Errors,
		Details:        make(map[string]string),
	}

	// 转换details
	for k, v := range result.Details {
		if str, ok := v.(string); ok {
			response.Details[k] = str
		} else {
			response.Details[k] = fmt.Sprintf("%v", v)
		}
	}

	// 如果有错误，更新消息
	if !result.Success {
		response.Message = "Metadata restore completed with errors"
	}

	s.logger.Sugar().Infof("Restore completed: success=%v, total=%d, ok=%d, errors=%d",
		result.Success, result.EntriesTotal, result.EntriesOK, result.EntriesError)

	return response, nil
}

// ListBackups 列出备份
func (s *MinIODBService) ListBackups(ctx context.Context, req *miniodb.ListBackupsRequest) (*miniodb.ListBackupsResponse, error) {
	s.logger.Sugar().Infof("Processing list backups request, days: %d", req.Days)

	if s.metadataMgr == nil {
		return &miniodb.ListBackupsResponse{
			Backups: []*miniodb.BackupInfo{},
			Total:   0,
		}, nil
	}

	// 获取恢复管理器
	recoveryManager := s.metadataMgr.GetRecoveryManager()
	if recoveryManager == nil {
		return &miniodb.ListBackupsResponse{
			Backups: []*miniodb.BackupInfo{},
			Total:   0,
		}, nil
	}

	// 设置默认天数
	days := int(req.Days)
	if days <= 0 {
		days = 30 // 默认查询30天内的备份
	}

	// 获取备份列表
	backupInfos, err := recoveryManager.ListBackups(ctx, days)
	if err != nil {
		s.logger.Sugar().Infof("ERROR: Failed to list backups: %v", err)
		return &miniodb.ListBackupsResponse{
			Backups: []*miniodb.BackupInfo{},
			Total:   0,
		}, nil
	}

	// 转换为protobuf格式
	var protoBackups []*miniodb.BackupInfo
	for _, backup := range backupInfos {
		protoBackup := &miniodb.BackupInfo{
			ObjectName:   backup.ObjectName,
			NodeId:       backup.NodeID,
			Timestamp:    timestamppb.New(backup.Timestamp),
			Size:         backup.Size,
			LastModified: timestamppb.New(backup.LastModified),
		}
		protoBackups = append(protoBackups, protoBackup)
	}

	s.logger.Sugar().Infof("Found %d backups in the last %d days", len(protoBackups), days)

	return &miniodb.ListBackupsResponse{
		Backups: protoBackups,
		Total:   int32(len(protoBackups)),
	}, nil
}

// GetMetadataStatus 获取元数据状态
func (s *MinIODBService) GetMetadataStatus(ctx context.Context, req *miniodb.GetMetadataStatusRequest) (*miniodb.GetMetadataStatusResponse, error) {
	s.logger.Sugar().Infof("Processing get metadata status request")

	if s.metadataMgr == nil {
		return &miniodb.GetMetadataStatusResponse{
			BackupStatus: map[string]string{"status": "not_configured"},
			HealthStatus: "standalone",
		}, nil
	}

	// 获取备份管理器和恢复管理器
	backupManager := s.metadataMgr.GetBackupManager()
	recoveryManager := s.metadataMgr.GetRecoveryManager()

	// 构建备份状态
	backupStatus := make(map[string]string)
	var lastBackup, nextBackup *timestamppb.Timestamp
	healthStatus := "healthy"

	if backupManager == nil {
		backupStatus["status"] = "not_configured"
		healthStatus = "degraded"
	} else {
		// 获取备份统计信息
		stats := backupManager.GetStats()
		for k, v := range stats {
			backupStatus[k] = fmt.Sprintf("%v", v)
		}

		// 设置状态
		if backupManager.IsEnabled() {
			backupStatus["status"] = "enabled"
		} else {
			backupStatus["status"] = "disabled"
			healthStatus = "degraded"
		}
	}

	// 获取最新备份信息
	if recoveryManager != nil {
		if latestBackup, err := recoveryManager.GetLatestBackup(ctx); err == nil {
			lastBackup = timestamppb.New(latestBackup.Timestamp)
			backupStatus["last_backup_size"] = fmt.Sprintf("%d", latestBackup.Size)
			backupStatus["last_backup_object"] = latestBackup.ObjectName
		}
	}

	// 计算下次备份时间（这是一个估算，实际逻辑可能更复杂）
	if lastBackup != nil && backupManager != nil && backupManager.IsEnabled() {
		// 假设备份间隔为1小时，这个可以从配置中获取
		nextBackupTime := lastBackup.AsTime().Add(1 * time.Hour)
		nextBackup = timestamppb.New(nextBackupTime)
		backupStatus["next_backup_estimated"] = nextBackupTime.Format(time.RFC3339)
	}

	// 获取节点ID
	nodeID := s.metadataMgr.GetNodeID()
	if nodeID == "" {
		nodeID = s.cfg.Server.NodeID
	}

	// 检查元数据管理器健康状态
	if err := s.metadataMgr.HealthCheck(ctx); err != nil {
		healthStatus = "unhealthy"
		backupStatus["health_check_error"] = err.Error()
	}

	response := &miniodb.GetMetadataStatusResponse{
		NodeId:       nodeID,
		BackupStatus: backupStatus,
		LastBackup:   lastBackup,
		NextBackup:   nextBackup,
		HealthStatus: healthStatus,
	}

	s.logger.Sugar().Infof("Metadata status: nodeID=%s, health=%s, backups_enabled=%s",
		nodeID, healthStatus, backupStatus["status"])

	return response, nil
}

// HealthCheck 健康检查
func (s *MinIODBService) HealthCheck(ctx context.Context) error {
	// 检查Redis连接池
	if s.redisPool != nil {
		if err := s.redisPool.HealthCheck(ctx); err != nil {
			return fmt.Errorf("redis health check failed: %w", err)
		}
	}

	// 检查配置是否有效
	if s.cfg == nil {
		return fmt.Errorf("configuration is nil")
	}

	return nil
}

func (s *MinIODBService) GetRedisPool() *pool.RedisPool {
	return s.redisPool
}

// GetStatus 获取状态
func (s *MinIODBService) GetStatus(ctx context.Context, req *miniodb.GetStatusRequest) (*miniodb.GetStatusResponse, error) {
	s.logger.Sugar().Infof("Processing get status request")

	// 收集缓冲区统计信息
	bufferStats := make(map[string]int64)
	if s.ingester != nil {
		if stats := s.ingester.GetBufferStats(); stats != nil {
			bufferStats["total_tasks"] = stats.TotalTasks
			bufferStats["completed_tasks"] = stats.CompletedTasks
			bufferStats["failed_tasks"] = stats.FailedTasks
			bufferStats["queued_tasks"] = stats.QueuedTasks
			bufferStats["active_workers"] = stats.ActiveWorkers
			bufferStats["buffer_size"] = stats.BufferSize
			bufferStats["pending_writes"] = stats.PendingWrites
			bufferStats["avg_flush_time_ms"] = stats.AvgFlushTime
			bufferStats["last_flush_time"] = stats.LastFlushTime
		}
	}

	// 收集Redis统计信息
	redisStats := make(map[string]int64)
	if s.redisPool != nil {
		redisClient := s.redisPool.GetClient()
		if poolStats := s.redisPool.GetStats(); poolStats != nil {
			redisStats["hits"] = int64(poolStats.Hits)
			redisStats["misses"] = int64(poolStats.Misses)
			redisStats["timeouts"] = int64(poolStats.Timeouts)
			redisStats["total_conns"] = int64(poolStats.TotalConns)
			redisStats["idle_conns"] = int64(poolStats.IdleConns)
			redisStats["stale_conns"] = int64(poolStats.StaleConns)
		}

		// 获取Redis内存使用情况
		if info, err := redisClient.Info(ctx, "memory").Result(); err == nil {
			lines := strings.Split(info, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "used_memory:") {
					if parts := strings.Split(line, ":"); len(parts) == 2 {
						if memUsage, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
							redisStats["used_memory_bytes"] = memUsage
						}
					}
				}
			}
		}

		// 获取Redis键数量统计
		if dbSize, err := redisClient.DBSize(ctx).Result(); err == nil {
			redisStats["total_keys"] = dbSize
		}
	}

	// 收集MinIO统计信息
	minioStats := make(map[string]int64)
	// 注意：这里只是示例，实际的MinIO统计信息需要通过MinIO管理API获取
	minioStats["connection_status"] = 1 // 1表示连接正常，0表示连接异常

	// 收集查询引擎统计信息
	if s.querier != nil {
		if queryStats := s.querier.GetQueryStats(); queryStats != nil {
			bufferStats["total_queries"] = queryStats.TotalQueries
			bufferStats["cache_hits"] = queryStats.CacheHits
			bufferStats["cache_misses"] = queryStats.CacheMisses
			bufferStats["error_count"] = queryStats.ErrorCount
			bufferStats["file_downloads"] = queryStats.FileDownloads
			bufferStats["file_cache_hits"] = queryStats.FileCacheHits
			bufferStats["avg_query_time_ms"] = int64(queryStats.AvgQueryTime.Milliseconds())
			bufferStats["fastest_query_ms"] = int64(queryStats.FastestQuery.Milliseconds())
			bufferStats["slowest_query_ms"] = int64(queryStats.SlowestQuery.Milliseconds())
		}
	}

	// 收集节点信息
	var nodes []*miniodb.NodeInfo

	// 当前节点信息
	currentNode := &miniodb.NodeInfo{
		Id:       s.cfg.Server.NodeID,
		Status:   "running",
		Type:     "primary", // 可以根据实际配置调整
		Address:  fmt.Sprintf("localhost:%s", strings.TrimPrefix(s.cfg.Server.GrpcPort, ":")),
		LastSeen: time.Now().Unix(),
	}
	nodes = append(nodes, currentNode)

	// 从Redis发现其他节点（如果有的话）
	if s.redisPool != nil {
		nodePattern := "service:nodes:*"
		if keys, err := pool.ScanKeys(ctx, s.redisPool.GetClient(), nodePattern); err == nil {
			for _, key := range keys {
				if nodeInfo, err := s.redisPool.GetClient().HGetAll(ctx, key).Result(); err == nil {
					nodeID := strings.TrimPrefix(key, "service:nodes:")
					if nodeID != s.cfg.Server.NodeID { // 排除当前节点
						lastSeen, _ := strconv.ParseInt(nodeInfo["last_seen"], 10, 64)
						node := &miniodb.NodeInfo{
							Id:       nodeID,
							Status:   nodeInfo["status"],
							Type:     nodeInfo["type"],
							Address:  nodeInfo["address"],
							LastSeen: lastSeen,
						}
						nodes = append(nodes, node)
					}
				}
			}
		}
	}

	// 更新当前节点在Redis中的状态
	if s.redisPool != nil {
		nodeKey := fmt.Sprintf("service:nodes:%s", s.cfg.Server.NodeID)
		nodeData := map[string]interface{}{
			"status":    "running",
			"type":      "primary",
			"address":   fmt.Sprintf("localhost:%s", strings.TrimPrefix(s.cfg.Server.GrpcPort, ":")),
			"last_seen": time.Now().Unix(),
		}
		if err := s.redisPool.GetClient().HMSet(ctx, nodeKey, nodeData).Err(); err != nil {
			s.logger.Warn("Failed to update node status in Redis", zap.Error(err))
		}
		if err := s.redisPool.GetClient().Expire(ctx, nodeKey, 60*time.Second).Err(); err != nil {
			s.logger.Warn("Failed to set node status expiry in Redis", zap.Error(err))
		}
	}

	response := &miniodb.GetStatusResponse{
		Timestamp:   timestamppb.New(time.Now()),
		BufferStats: bufferStats,
		RedisStats:  redisStats,
		MinioStats:  minioStats,
		Nodes:       nodes,
		TotalNodes:  int32(len(nodes)),
	}

	s.logger.Sugar().Infof("Status collected: %d nodes, buffer_pending=%d, redis_keys=%d",
		len(nodes), bufferStats["pending_writes"], redisStats["total_keys"])

	return response, nil
}

// GetMetrics 获取监控指标
func (s *MinIODBService) GetMetrics(ctx context.Context, req *miniodb.GetMetricsRequest) (*miniodb.GetMetricsResponse, error) {
	s.logger.Sugar().Infof("Processing get metrics request")

	// 收集性能指标
	performanceMetrics := make(map[string]float64)

	// 查询引擎性能指标
	if s.querier != nil {
		if queryStats := s.querier.GetQueryStats(); queryStats != nil {
			// 计算查询性能指标
			if queryStats.TotalQueries > 0 {
				performanceMetrics["query_success_rate"] = float64(queryStats.TotalQueries-queryStats.ErrorCount) / float64(queryStats.TotalQueries)
				performanceMetrics["avg_query_time_seconds"] = queryStats.AvgQueryTime.Seconds()
				performanceMetrics["fastest_query_seconds"] = queryStats.FastestQuery.Seconds()
				performanceMetrics["slowest_query_seconds"] = queryStats.SlowestQuery.Seconds()
			}

			// 缓存命中率
			totalCacheRequests := queryStats.CacheHits + queryStats.CacheMisses
			if totalCacheRequests > 0 {
				performanceMetrics["cache_hit_rate"] = float64(queryStats.CacheHits) / float64(totalCacheRequests)
			}

			// 文件缓存命中率
			totalFileRequests := queryStats.FileDownloads + queryStats.FileCacheHits
			if totalFileRequests > 0 {
				performanceMetrics["file_cache_hit_rate"] = float64(queryStats.FileCacheHits) / float64(totalFileRequests)
			}
		}
	}

	// 缓冲区性能指标
	if s.ingester != nil {
		if bufferStats := s.ingester.GetBufferStats(); bufferStats != nil {
			// 计算缓冲区效率指标
			if bufferStats.TotalTasks > 0 {
				performanceMetrics["buffer_success_rate"] = float64(bufferStats.CompletedTasks) / float64(bufferStats.TotalTasks)
				performanceMetrics["buffer_error_rate"] = float64(bufferStats.FailedTasks) / float64(bufferStats.TotalTasks)
			}

			// 缓冲区处理时间
			if bufferStats.AvgFlushTime > 0 {
				performanceMetrics["avg_flush_time_seconds"] = float64(bufferStats.AvgFlushTime) / 1000.0
			}
		}
	}

	// 收集资源使用情况
	resourceUsage := make(map[string]int64)

	// Redis资源使用
	if s.redisPool != nil {
		redisClient := s.redisPool.GetClient()
		if poolStats := s.redisPool.GetStats(); poolStats != nil {
			resourceUsage["redis_total_connections"] = int64(poolStats.TotalConns)
			resourceUsage["redis_idle_connections"] = int64(poolStats.IdleConns)
			resourceUsage["redis_active_connections"] = int64(poolStats.TotalConns - poolStats.IdleConns)
		}

		// Redis内存使用
		if info, err := redisClient.Info(ctx, "memory").Result(); err == nil {
			lines := strings.Split(info, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "used_memory:") {
					if parts := strings.Split(line, ":"); len(parts) == 2 {
						if memUsage, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
							resourceUsage["redis_memory_bytes"] = memUsage
						}
					}
				} else if strings.HasPrefix(line, "used_memory_peak:") {
					if parts := strings.Split(line, ":"); len(parts) == 2 {
						if memPeak, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
							resourceUsage["redis_memory_peak_bytes"] = memPeak
						}
					}
				}
			}
		}

		// Redis键数量
		if dbSize, err := redisClient.DBSize(ctx).Result(); err == nil {
			resourceUsage["redis_total_keys"] = dbSize
		}
	}

	// 缓冲区资源使用
	if s.ingester != nil {
		if bufferStats := s.ingester.GetBufferStats(); bufferStats != nil {
			resourceUsage["buffer_pending_writes"] = bufferStats.PendingWrites
			resourceUsage["buffer_active_workers"] = bufferStats.ActiveWorkers
			resourceUsage["buffer_queued_tasks"] = bufferStats.QueuedTasks
			resourceUsage["buffer_total_tasks"] = bufferStats.TotalTasks
		}
	}

	// 收集系统信息
	systemInfo := make(map[string]string)
	systemInfo["node_id"] = s.cfg.Server.NodeID
	systemInfo["version"] = version.Get()
	systemInfo["build_time"] = time.Now().Format(time.RFC3339)
	systemInfo["uptime_seconds"] = fmt.Sprintf("%.0f", time.Since(s.startTime).Seconds())

	// 配置信息
	systemInfo["grpc_port"] = s.cfg.Server.GrpcPort
	systemInfo["rest_port"] = s.cfg.Server.RestPort
	systemInfo["redis_mode"] = "standalone" // 可以从Redis客户端获取

	// 表统计信息
	if s.tableManager != nil {
		if tableList, err := s.tableManager.ListTables(ctx, "*"); err == nil {
			systemInfo["total_tables"] = fmt.Sprintf("%d", len(tableList))
		}
	}

	// 元数据管理器信息
	if s.metadataMgr != nil {
		stats := s.metadataMgr.GetStats()
		for k, v := range stats {
			systemInfo[fmt.Sprintf("metadata_%s", k)] = fmt.Sprintf("%v", v)
		}

		// 备份状态
		if backupManager := s.metadataMgr.GetBackupManager(); backupManager != nil {
			if backupManager.IsEnabled() {
				systemInfo["backup_enabled"] = "true"
			} else {
				systemInfo["backup_enabled"] = "false"
			}
		}
	}

	// 计算一些高级指标
	if performanceMetrics["query_success_rate"] > 0 {
		if performanceMetrics["query_success_rate"] >= 0.95 {
			systemInfo["service_health"] = "excellent"
		} else if performanceMetrics["query_success_rate"] >= 0.9 {
			systemInfo["service_health"] = "good"
		} else if performanceMetrics["query_success_rate"] >= 0.8 {
			systemInfo["service_health"] = "fair"
		} else {
			systemInfo["service_health"] = "poor"
		}
	} else {
		systemInfo["service_health"] = "unknown"
	}

	response := &miniodb.GetMetricsResponse{
		Timestamp:          timestamppb.New(time.Now()),
		PerformanceMetrics: performanceMetrics,
		ResourceUsage:      resourceUsage,
		SystemInfo:         systemInfo,
	}

	s.logger.Sugar().Infof("Metrics collected: %d performance metrics, %d resource metrics, health=%s",
		len(performanceMetrics), len(resourceUsage), systemInfo["service_health"])

	return response, nil
}

// Close 关闭服务
func (s *MinIODBService) Close() error {
	s.logger.Sugar().Infof("Closing MinIODBService")

	if s.querier != nil {
		s.querier.Close()
	}

	// 连接池会由池管理器关闭，这里不需要手动关闭

	return nil
}
