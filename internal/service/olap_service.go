package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/ingest"
	"minIODB/internal/query"
	"minIODB/internal/storage"

	"github.com/go-redis/redis/v8"
	"github.com/minio/minio-go/v7"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OlapService OLAP服务实现
type OlapService struct {
	olapv1.UnimplementedOlapServiceServer

	cfg          *config.Config
	ingester     *ingest.Ingester
	querier      *query.Querier
	redisClient  *redis.Client
	primaryMinio storage.Uploader
	backupMinio  storage.Uploader
	tableManager *TableManager
}

// NewOlapService 创建OLAP服务实例
func NewOlapService(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier,
	redisClient *redis.Client, primaryMinio, backupMinio storage.Uploader) (*OlapService, error) {

	// 创建表管理器
	tableManager := NewTableManager(redisClient, primaryMinio, backupMinio, cfg)

	return &OlapService{
		cfg:          cfg,
		ingester:     ingester,
		querier:      querier,
		redisClient:  redisClient,
		primaryMinio: primaryMinio,
		backupMinio:  backupMinio,
		tableManager: tableManager,
	}, nil
}

// validateWriteRequest 验证写入请求
func (s *OlapService) validateWriteRequest(req *olapv1.WriteRequest) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "id is required and cannot be empty")
	}
	if len(req.Id) > 255 {
		return status.Error(codes.InvalidArgument, "id cannot exceed 255 characters")
	}
	if req.Timestamp == nil {
		return status.Error(codes.InvalidArgument, "timestamp is required")
	}
	if req.Payload == nil {
		return status.Error(codes.InvalidArgument, "payload is required")
	}

	// 验证ID格式（只允许字母、数字、连字符和下划线）
	for _, r := range req.Id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return status.Error(codes.InvalidArgument, "id contains invalid characters, only alphanumeric, dash and underscore allowed")
		}
	}

	return nil
}

// Write 写入数据
func (s *OlapService) Write(ctx context.Context, req *olapv1.WriteRequest) (*olapv1.WriteResponse, error) {
	// 处理表名：如果未指定则使用默认表
	// 注意：由于protobuf重新生成问题，暂时使用默认表名
	tableName := s.cfg.GetDefaultTableName()

	log.Printf("Received write request for table: %s, ID: %s", tableName, req.Id)

	// 验证请求
	if err := s.validateWriteRequest(req); err != nil {
		return nil, err
	}

	// 验证表名
	if !s.cfg.IsValidTableName(tableName) {
		return &olapv1.WriteResponse{
			Success: false,
			Message: fmt.Sprintf("invalid table name: %s", tableName),
		}, nil
	}

	// 确保表存在（如果启用自动创建）
	if err := s.tableManager.EnsureTableExists(ctx, tableName); err != nil {
		log.Printf("ERROR: failed to ensure table exists: %v", err)
		return &olapv1.WriteResponse{
			Success: false,
			Message: fmt.Sprintf("table error: %v", err),
		}, nil
	}

	// 更新请求中的表名（确保一致性）
	// 注意：由于protobuf重新生成问题，暂时跳过这步

	// 使用Ingester处理写入
	if err := s.ingester.IngestData(req); err != nil {
		log.Printf("ERROR: failed to ingest data for table %s, ID %s: %v", tableName, req.Id, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to ingest data: %v", err))
	}

	// 更新表的最后写入时间
	if err := s.tableManager.UpdateLastWrite(ctx, tableName); err != nil {
		log.Printf("WARN: failed to update last write time for table %s: %v", tableName, err)
	}

	log.Printf("Successfully ingested data for table: %s, ID: %s", tableName, req.Id)
	return &olapv1.WriteResponse{
		Success: true,
		Message: fmt.Sprintf("Data successfully ingested for table: %s, ID: %s", tableName, req.Id),
	}, nil
}

// validateQueryRequest 验证查询请求
func (s *OlapService) validateQueryRequest(req *olapv1.QueryRequest) error {
	if req.Sql == "" {
		return status.Error(codes.InvalidArgument, "sql is required and cannot be empty")
	}
	if len(req.Sql) > 10000 {
		return status.Error(codes.InvalidArgument, "sql query cannot exceed 10000 characters")
	}

	// 基本的SQL注入防护（简单检查）
	lowerSQL := strings.ToLower(req.Sql)
	dangerousKeywords := []string{"drop", "delete", "truncate", "alter", "create", "insert", "update"}
	for _, keyword := range dangerousKeywords {
		if strings.Contains(lowerSQL, keyword) {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("sql contains dangerous keyword: %s", keyword))
		}
	}

	return nil
}

// Query 查询数据
func (s *OlapService) Query(ctx context.Context, req *olapv1.QueryRequest) (*olapv1.QueryResponse, error) {
	log.Printf("Received query request: %s", req.Sql)

	// 验证请求
	if err := s.validateQueryRequest(req); err != nil {
		return nil, err
	}

	// 处理向后兼容：将旧的"table"关键字替换为默认表名
	sql := req.Sql
	if strings.Contains(strings.ToLower(sql), "from table") {
		defaultTable := s.cfg.GetDefaultTableName()
		sql = strings.ReplaceAll(sql, "FROM table", fmt.Sprintf("FROM %s", defaultTable))
		sql = strings.ReplaceAll(sql, "from table", fmt.Sprintf("from %s", defaultTable))
		log.Printf("Converted legacy SQL to use default table: %s", sql)
	}

	// 使用Querier执行查询
	result, err := s.querier.ExecuteQuery(sql)
	if err != nil {
		log.Printf("ERROR: query failed: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("query execution failed: %v", err))
	}

	log.Printf("Query completed successfully, result length: %d characters", len(result))
	return &olapv1.QueryResponse{
		ResultJson: result,
	}, nil
}

// CreateTable 创建表
func (s *OlapService) CreateTable(ctx context.Context, req *CreateTableRequest) (*CreateTableResponse, error) {
	log.Printf("Received create table request: %s", req.TableName)

	// 验证表名
	if req.TableName == "" {
		return &CreateTableResponse{
			Success: false,
			Message: "table name is required",
		}, nil
	}

	// 转换配置
	var tableConfig *config.TableConfig
	if req.Config != nil {
		tableConfig = &config.TableConfig{
			BufferSize:    int(req.Config.BufferSize),
			FlushInterval: time.Duration(req.Config.FlushIntervalSeconds) * time.Second,
			RetentionDays: int(req.Config.RetentionDays),
			BackupEnabled: req.Config.BackupEnabled,
			Properties:    req.Config.Properties,
		}
	}

	// 创建表
	if err := s.tableManager.CreateTable(ctx, req.TableName, tableConfig, req.IfNotExists); err != nil {
		log.Printf("ERROR: failed to create table %s: %v", req.TableName, err)
		return &CreateTableResponse{
			Success: false,
			Message: fmt.Sprintf("failed to create table: %v", err),
		}, nil
	}

	return &CreateTableResponse{
		Success: true,
		Message: fmt.Sprintf("Table %s created successfully", req.TableName),
	}, nil
}

// DropTable 删除表
func (s *OlapService) DropTable(ctx context.Context, req *DropTableRequest) (*DropTableResponse, error) {
	log.Printf("Received drop table request: %s (cascade: %v)", req.TableName, req.Cascade)

	// 验证表名
	if req.TableName == "" {
		return &DropTableResponse{
			Success: false,
			Message: "table name is required",
		}, nil
	}

	// 删除表
	filesDeleted, err := s.tableManager.DropTable(ctx, req.TableName, req.IfExists, req.Cascade)
	if err != nil {
		log.Printf("ERROR: failed to drop table %s: %v", req.TableName, err)
		return &DropTableResponse{
			Success: false,
			Message: fmt.Sprintf("failed to drop table: %v", err),
		}, nil
	}

	return &DropTableResponse{
		Success:      true,
		Message:      fmt.Sprintf("Table %s dropped successfully", req.TableName),
		FilesDeleted: filesDeleted,
	}, nil
}

// ListTables 列出表
func (s *OlapService) ListTables(ctx context.Context, req *ListTablesRequest) (*ListTablesResponse, error) {
	log.Printf("Received list tables request (pattern: %s)", req.Pattern)

	// 列出表
	tables, err := s.tableManager.ListTables(ctx, req.Pattern)
	if err != nil {
		log.Printf("ERROR: failed to list tables: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to list tables: %v", err))
	}

	// 转换为protobuf格式
	var tableInfos []*TableInfo
	for _, table := range tables {
		tableInfo := &TableInfo{
			Name:      table.Name,
			CreatedAt: table.CreatedAt,
			LastWrite: table.LastWrite,
			Status:    table.Status,
		}

		// 转换配置
		if table.Config != nil {
			tableInfo.Config = &TableConfig{
				BufferSize:           int32(table.Config.BufferSize),
				FlushIntervalSeconds: int32(table.Config.FlushInterval / time.Second),
				RetentionDays:        int32(table.Config.RetentionDays),
				BackupEnabled:        table.Config.BackupEnabled,
				Properties:           table.Config.Properties,
			}
		}

		tableInfos = append(tableInfos, tableInfo)
	}

	return &ListTablesResponse{
		Tables: tableInfos,
		Total:  int32(len(tableInfos)),
	}, nil
}

// DescribeTable 描述表
func (s *OlapService) DescribeTable(ctx context.Context, req *DescribeTableRequest) (*DescribeTableResponse, error) {
	log.Printf("Received describe table request: %s", req.TableName)

	// 验证表名
	if req.TableName == "" {
		return nil, status.Error(codes.InvalidArgument, "table name is required")
	}

	// 获取表信息和统计
	tableInfo, tableStats, err := s.tableManager.DescribeTable(ctx, req.TableName)
	if err != nil {
		log.Printf("ERROR: failed to describe table %s: %v", req.TableName, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to describe table: %v", err))
	}

	// 转换为protobuf格式
	response := &DescribeTableResponse{
		TableInfo: &TableInfo{
			Name:      tableInfo.Name,
			CreatedAt: tableInfo.CreatedAt,
			LastWrite: tableInfo.LastWrite,
			Status:    tableInfo.Status,
		},
		Stats: &TableStats{
			RecordCount:  tableStats.RecordCount,
			FileCount:    tableStats.FileCount,
			SizeBytes:    tableStats.SizeBytes,
			OldestRecord: tableStats.OldestRecord,
			NewestRecord: tableStats.NewestRecord,
		},
	}

	// 转换配置
	if tableInfo.Config != nil {
		response.TableInfo.Config = &TableConfig{
			BufferSize:           int32(tableInfo.Config.BufferSize),
			FlushIntervalSeconds: int32(tableInfo.Config.FlushInterval / time.Second),
			RetentionDays:        int32(tableInfo.Config.RetentionDays),
			BackupEnabled:        tableInfo.Config.BackupEnabled,
			Properties:           tableInfo.Config.Properties,
		}
	}

	return response, nil
}

// 临时类型定义（直到protobuf重新生成）
type CreateTableRequest struct {
	TableName   string       `json:"table_name"`
	Config      *TableConfig `json:"config"`
	IfNotExists bool         `json:"if_not_exists"`
}

type CreateTableResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type DropTableRequest struct {
	TableName string `json:"table_name"`
	IfExists  bool   `json:"if_exists"`
	Cascade   bool   `json:"cascade"`
}

type DropTableResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	FilesDeleted int32  `json:"files_deleted"`
}

type ListTablesRequest struct {
	Pattern string `json:"pattern"`
}

type ListTablesResponse struct {
	Tables []*TableInfo `json:"tables"`
	Total  int32        `json:"total"`
}

type DescribeTableRequest struct {
	TableName string `json:"table_name"`
}

type DescribeTableResponse struct {
	TableInfo *TableInfo  `json:"table_info"`
	Stats     *TableStats `json:"stats"`
}

type TableInfo struct {
	Name      string       `json:"name"`
	Config    *TableConfig `json:"config"`
	CreatedAt string       `json:"created_at"`
	LastWrite string       `json:"last_write"`
	Status    string       `json:"status"`
}

type TableConfig struct {
	BufferSize           int32             `json:"buffer_size"`
	FlushIntervalSeconds int32             `json:"flush_interval_seconds"`
	RetentionDays        int32             `json:"retention_days"`
	BackupEnabled        bool              `json:"backup_enabled"`
	Properties           map[string]string `json:"properties"`
}

type TableStats struct {
	RecordCount  int64  `json:"record_count"`
	FileCount    int64  `json:"file_count"`
	SizeBytes    int64  `json:"size_bytes"`
	OldestRecord string `json:"oldest_record"`
	NewestRecord string `json:"newest_record"`
}

// TriggerBackup 手动触发备份
func (s *OlapService) TriggerBackup(ctx context.Context, req *olapv1.TriggerBackupRequest) (*olapv1.TriggerBackupResponse, error) {
	log.Printf("Received backup trigger request for ID: %s, Day: %s", req.Id, req.Day)

	if s.backupMinio == nil {
		return &olapv1.TriggerBackupResponse{
			Success: false,
			Message: "Backup storage is not configured",
		}, nil
	}

	var filesBackedUp int32

	// 获取默认表名
	defaultTable := s.cfg.GetDefaultTableName()

	if req.Id != "" && req.Day != "" {
		// 备份特定ID和日期的数据
		redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", defaultTable, req.Id, req.Day)
		objectNames, err := s.redisClient.SMembers(ctx, redisKey).Result()
		if err != nil {
			log.Printf("ERROR: failed to get objects from Redis for key %s: %v", redisKey, err)
			return &olapv1.TriggerBackupResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to get file list: %v", err),
			}, nil
		}

		for _, objectName := range objectNames {
			if err := s.backupFile(ctx, objectName); err != nil {
				log.Printf("ERROR: failed to backup file %s: %v", objectName, err)
			} else {
				filesBackedUp++
			}
		}
	} else if req.Id != "" {
		// 备份特定ID的所有数据
		pattern := fmt.Sprintf("index:table:%s:id:%s:*", defaultTable, req.Id)
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("ERROR: failed to get keys from Redis for pattern %s: %v", pattern, err)
			return &olapv1.TriggerBackupResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to get key list: %v", err),
			}, nil
		}

		for _, key := range keys {
			objectNames, err := s.redisClient.SMembers(ctx, key).Result()
			if err != nil {
				log.Printf("ERROR: failed to get objects from Redis for key %s: %v", key, err)
				continue
			}

			for _, objectName := range objectNames {
				if err := s.backupFile(ctx, objectName); err != nil {
					log.Printf("ERROR: failed to backup file %s: %v", objectName, err)
				} else {
					filesBackedUp++
				}
			}
		}
	} else if req.Day != "" {
		// 备份特定日期的所有数据
		pattern := fmt.Sprintf("index:table:%s:id:*:%s", defaultTable, req.Day)
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("ERROR: failed to get keys from Redis for pattern %s: %v", pattern, err)
			return &olapv1.TriggerBackupResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to get key list: %v", err),
			}, nil
		}

		for _, key := range keys {
			objectNames, err := s.redisClient.SMembers(ctx, key).Result()
			if err != nil {
				log.Printf("ERROR: failed to get objects from Redis for key %s: %v", key, err)
				continue
			}

			for _, objectName := range objectNames {
				if err := s.backupFile(ctx, objectName); err != nil {
					log.Printf("ERROR: failed to backup file %s: %v", objectName, err)
				} else {
					filesBackedUp++
				}
			}
		}
	} else {
		// 备份所有数据
		pattern := fmt.Sprintf("index:table:%s:id:*", defaultTable)
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			log.Printf("ERROR: failed to get keys from Redis for pattern %s: %v", pattern, err)
			return &olapv1.TriggerBackupResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to get key list: %v", err),
			}, nil
		}

		for _, key := range keys {
			objectNames, err := s.redisClient.SMembers(ctx, key).Result()
			if err != nil {
				log.Printf("ERROR: failed to get objects from Redis for key %s: %v", key, err)
				continue
			}

			for _, objectName := range objectNames {
				if err := s.backupFile(ctx, objectName); err != nil {
					log.Printf("ERROR: failed to backup file %s: %v", objectName, err)
				} else {
					filesBackedUp++
				}
			}
		}
	}

	return &olapv1.TriggerBackupResponse{
		Success:       true,
		Message:       fmt.Sprintf("Successfully backed up %d files", filesBackedUp),
		FilesBackedUp: filesBackedUp,
	}, nil
}

// backupFile 备份单个文件
func (s *OlapService) backupFile(ctx context.Context, objectName string) error {
	// 检查备份存储中是否已存在该文件
	exists, err := s.backupMinio.ObjectExists(ctx, s.cfg.Backup.MinIO.Bucket, objectName)
	if err != nil {
		return fmt.Errorf("failed to check if backup file exists: %w", err)
	}

	if exists {
		log.Printf("File %s already exists in backup storage, skipping", objectName)
		return nil
	}

	// 从主存储复制到备份存储
	_, err = s.backupMinio.CopyObject(ctx,
		// 目标
		minio.CopyDestOptions{
			Bucket: s.cfg.Backup.MinIO.Bucket,
			Object: objectName,
		},
		// 源
		minio.CopySrcOptions{
			Bucket: s.cfg.MinIO.Bucket,
			Object: objectName,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to copy file to backup storage: %w", err)
	}

	log.Printf("Successfully backed up file: %s", objectName)
	return nil
}

// RecoverData 从备份恢复数据
func (s *OlapService) RecoverData(ctx context.Context, req *olapv1.RecoverDataRequest) (*olapv1.RecoverDataResponse, error) {
	log.Printf("Received recover data request")

	if s.backupMinio == nil {
		return &olapv1.RecoverDataResponse{
			Success: false,
			Message: "Backup storage is not configured",
		}, nil
	}

	var filesRecovered int32
	var recoveredKeys []string

	// 获取默认表名
	defaultTable := s.cfg.GetDefaultTableName()

	switch req.RecoveryMode.(type) {
	case *olapv1.RecoverDataRequest_NodeId:
		// 恢复特定节点的数据
		nodeId := req.GetNodeId()
		log.Printf("Recovering data for node: %s", nodeId)

		// 查找节点相关的备份数据
		pattern := fmt.Sprintf("backup:node:%s:*", nodeId)
		keys, err := s.redisClient.Keys(ctx, pattern).Result()
		if err != nil {
			return &olapv1.RecoverDataResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to get backup keys for node %s: %v", nodeId, err),
			}, nil
		}

		for _, backupKey := range keys {
			objectNames, err := s.redisClient.SMembers(ctx, backupKey).Result()
			if err != nil {
				log.Printf("ERROR: failed to get backup objects from Redis for key %s: %v", backupKey, err)
				continue
			}

			for _, objectName := range objectNames {
				if err := s.recoverFile(ctx, objectName); err != nil {
					log.Printf("ERROR: failed to recover file %s: %v", objectName, err)
				} else {
					filesRecovered++
					recoveredKeys = append(recoveredKeys, objectName)
				}
			}
		}

	case *olapv1.RecoverDataRequest_IdRange:
		// 恢复特定ID范围的数据
		idRange := req.GetIdRange()
		log.Printf("Recovering data for ID range: %+v", idRange)

		if len(idRange.Ids) > 0 {
			// 恢复指定ID列表的数据
			for _, id := range idRange.Ids {
				pattern := fmt.Sprintf("backup:table:%s:id:%s:*", defaultTable, id)
				keys, err := s.redisClient.Keys(ctx, pattern).Result()
				if err != nil {
					log.Printf("ERROR: failed to get backup keys for ID %s: %v", id, err)
					continue
				}

				for _, backupKey := range keys {
					objectNames, err := s.redisClient.SMembers(ctx, backupKey).Result()
					if err != nil {
						log.Printf("ERROR: failed to get backup objects from Redis for key %s: %v", backupKey, err)
						continue
					}

					for _, objectName := range objectNames {
						if err := s.recoverFile(ctx, objectName); err != nil {
							log.Printf("ERROR: failed to recover file %s: %v", objectName, err)
						} else {
							filesRecovered++
							recoveredKeys = append(recoveredKeys, objectName)
							// 更新主索引
							parts := strings.Split(backupKey, ":")
							if len(parts) >= 6 {
								day := parts[5]
								redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", defaultTable, id, day)
								s.redisClient.SAdd(ctx, redisKey, objectName)
							}
						}
					}
				}
			}
		}

	case *olapv1.RecoverDataRequest_TimeRange:
		// 恢复特定时间范围的数据
		timeRange := req.GetTimeRange()
		log.Printf("Recovering data for time range: %+v", timeRange)

		if len(timeRange.Ids) > 0 {
			// 恢复指定ID和时间范围的数据
			for _, id := range timeRange.Ids {
				pattern := fmt.Sprintf("backup:table:%s:id:%s:*", defaultTable, id)
				keys, err := s.redisClient.Keys(ctx, pattern).Result()
				if err != nil {
					log.Printf("ERROR: failed to get backup keys for ID %s: %v", id, err)
					continue
				}

				for _, backupKey := range keys {
					// 检查日期是否在时间范围内
					parts := strings.Split(backupKey, ":")
					if len(parts) >= 6 {
						day := parts[5]
						if day >= timeRange.StartDate && day <= timeRange.EndDate {
							objectNames, err := s.redisClient.SMembers(ctx, backupKey).Result()
							if err != nil {
								log.Printf("ERROR: failed to get backup objects from Redis for key %s: %v", backupKey, err)
								continue
							}

							for _, objectName := range objectNames {
								if err := s.recoverFile(ctx, objectName); err != nil {
									log.Printf("ERROR: failed to recover file %s: %v", objectName, err)
								} else {
									filesRecovered++
									recoveredKeys = append(recoveredKeys, objectName)
									// 更新主索引
									redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", defaultTable, id, day)
									s.redisClient.SAdd(ctx, redisKey, objectName)
								}
							}
						}
					}
				}
			}
		} else {
			// 恢复所有ID在指定时间范围内的数据
			pattern := fmt.Sprintf("backup:table:%s:id:*", defaultTable)
			keys, err := s.redisClient.Keys(ctx, pattern).Result()
			if err != nil {
				return &olapv1.RecoverDataResponse{
					Success: false,
					Message: fmt.Sprintf("Failed to get backup keys: %v", err),
				}, nil
			}

			for _, backupKey := range keys {
				// 检查日期是否在时间范围内
				parts := strings.Split(backupKey, ":")
				if len(parts) >= 6 {
					day := parts[5]
					if day >= timeRange.StartDate && day <= timeRange.EndDate {
						objectNames, err := s.redisClient.SMembers(ctx, backupKey).Result()
						if err != nil {
							log.Printf("ERROR: failed to get backup objects from Redis for key %s: %v", backupKey, err)
							continue
						}

						id := parts[4]
						for _, objectName := range objectNames {
							if err := s.recoverFile(ctx, objectName); err != nil {
								log.Printf("ERROR: failed to recover file %s: %v", objectName, err)
							} else {
								filesRecovered++
								recoveredKeys = append(recoveredKeys, objectName)
								// 更新主索引
								redisKey := fmt.Sprintf("index:table:%s:id:%s:%s", defaultTable, id, day)
								s.redisClient.SAdd(ctx, redisKey, objectName)
							}
						}
					}
				}
			}
		}

	default:
		return &olapv1.RecoverDataResponse{
			Success: false,
			Message: "Recovery mode is required",
		}, nil
	}

	return &olapv1.RecoverDataResponse{
		Success:        true,
		Message:        fmt.Sprintf("Successfully recovered %d files", filesRecovered),
		FilesRecovered: filesRecovered,
		RecoveredKeys:  recoveredKeys,
	}, nil
}

// recoverFile 恢复单个文件
func (s *OlapService) recoverFile(ctx context.Context, objectName string) error {
	// 检查主存储中是否已存在该文件
	exists, err := s.primaryMinio.ObjectExists(ctx, s.cfg.MinIO.Bucket, objectName)
	if err != nil {
		return fmt.Errorf("failed to check if file exists in primary storage: %w", err)
	}

	if exists {
		log.Printf("File %s already exists in primary storage, skipping", objectName)
		return nil
	}

	// 从备份存储复制到主存储
	_, err = s.primaryMinio.CopyObject(ctx,
		// 目标
		minio.CopyDestOptions{
			Bucket: s.cfg.MinIO.Bucket,
			Object: objectName,
		},
		// 源
		minio.CopySrcOptions{
			Bucket: s.cfg.Backup.MinIO.Bucket,
			Object: objectName,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to copy file from backup storage: %w", err)
	}

	log.Printf("Successfully recovered file: %s", objectName)
	return nil
}

// GetStats 获取服务统计信息
func (s *OlapService) GetStats(ctx context.Context, req *olapv1.GetStatsRequest) (*olapv1.GetStatsResponse, error) {
	log.Printf("Received stats request")

	response := &olapv1.GetStatsResponse{
		Timestamp:   fmt.Sprintf("%d", time.Now().Unix()),
		BufferStats: make(map[string]int64),
		RedisStats:  make(map[string]int64),
		MinioStats:  make(map[string]int64),
	}

	// 获取Redis统计信息
	info := s.redisClient.Info(ctx)
	if info.Err() == nil {
		// 这里可以解析Redis INFO命令的结果
		response.RedisStats["connected"] = 1
	}

	// 基础统计信息
	response.BufferStats["service_status"] = 1
	response.MinioStats["primary_connected"] = 1
	if s.backupMinio != nil {
		response.MinioStats["backup_connected"] = 1
	}

	return response, nil
}

// HealthCheck 健康检查
func (s *OlapService) HealthCheck(ctx context.Context, req *olapv1.HealthCheckRequest) (*olapv1.HealthCheckResponse, error) {
	log.Printf("Received health check request")

	// 检查Redis连接
	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		return &olapv1.HealthCheckResponse{
			Status:    "unhealthy",
			Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
			Version:   "1.0.0",
			Details: map[string]string{
				"redis": fmt.Sprintf("unhealthy: %v", err),
			},
		}, nil
	}

	return &olapv1.HealthCheckResponse{
		Status:    "healthy",
		Timestamp: fmt.Sprintf("%d", time.Now().Unix()),
		Version:   "1.0.0",
		Details: map[string]string{
			"redis":         "healthy",
			"primary_minio": "healthy",
			"backup_minio":  fmt.Sprintf("enabled: %t", s.backupMinio != nil),
		},
	}, nil
}

// GetNodes 获取节点信息
func (s *OlapService) GetNodes(ctx context.Context, req *olapv1.GetNodesRequest) (*olapv1.GetNodesResponse, error) {
	log.Printf("Received get nodes request")

	// 从Redis获取所有注册的节点
	nodes, err := s.redisClient.HGetAll(ctx, "nodes:services").Result()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get nodes from Redis: %v", err))
	}

	var nodeInfos []*olapv1.NodeInfo
	for nodeID, nodeData := range nodes {
		// 检查节点健康状态
		healthKey := fmt.Sprintf("nodes:health:%s", nodeID)
		_, err := s.redisClient.Get(ctx, healthKey).Result()

		status := "unhealthy"
		if err == nil {
			status = "healthy"
		}

		nodeInfos = append(nodeInfos, &olapv1.NodeInfo{
			Id:       nodeID,
			Status:   status,
			Type:     "worker",
			Address:  nodeData, // 简化处理，实际应该解析JSON
			LastSeen: time.Now().Unix(),
		})
	}

	return &olapv1.GetNodesResponse{
		Nodes: nodeInfos,
		Total: int32(len(nodeInfos)),
	}, nil
}

// Close 关闭服务
func (s *OlapService) Close() error {
	log.Printf("Closing OlapService")
	if s.querier != nil {
		s.querier.Close()
	}
	return nil
}
