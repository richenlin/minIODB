//go:build dashboard

package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	miniodb "minIODB/api/proto/miniodb/v1"
	"minIODB/config"
	. "minIODB/internal/dashboard/model"
	"minIODB/internal/discovery"
	"minIODB/internal/metadata"
	"minIODB/internal/security"
	"minIODB/internal/service"
	"minIODB/pkg/pool"
	"minIODB/pkg/version"

	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type LocalClient struct {
	svc       *service.MinIODBService
	mm        *metadata.Manager
	sr        *discovery.ServiceRegistry
	rp        *pool.RedisPool
	cfg       *config.Config
	logger    *zap.Logger
	startTime time.Time
}

func NewLocalClient(
	svc *service.MinIODBService,
	mm *metadata.Manager,
	sr *discovery.ServiceRegistry,
	rp *pool.RedisPool,
	cfg *config.Config,
	logger *zap.Logger,
) CoreClient {
	return &LocalClient{
		svc:       svc,
		mm:        mm,
		sr:        sr,
		rp:        rp,
		cfg:       cfg,
		logger:    logger,
		startTime: time.Now(),
	}
}

func (c *LocalClient) GetHealth(ctx context.Context) (*HealthResult, error) {
	status := "healthy"
	if err := c.svc.HealthCheck(ctx); err != nil {
		status = "unhealthy"
	}

	mode := "single-node"
	if c.sr != nil && c.cfg.Network.Pools.Redis.Enabled {
		mode = "distributed"
	}

	return &HealthResult{
		Status:    status,
		Timestamp: time.Now().Unix(),
		Version:   version.Get(),
		NodeID:    c.cfg.Server.NodeID,
		Mode:      mode,
	}, nil
}

func (c *LocalClient) GetStatus(ctx context.Context) (*StatusResult, error) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	tableCount := 0
	if c.mm != nil {
		if tables, err := c.mm.ListTableConfigs(ctx); err == nil {
			tableCount = len(tables)
		}
	}

	return &StatusResult{
		Version:     version.Get(),
		NodeID:      c.cfg.Server.NodeID,
		Mode:        c.getMode(),
		Tables:      tableCount,
		Goroutines:  runtime.NumGoroutine(),
		MemoryUsage: int64(memStats.Alloc),
		Uptime:      int64(time.Since(c.startTime).Seconds()),
	}, nil
}

func (c *LocalClient) GetMetrics(ctx context.Context) (*MetricsResult, error) {
	req := &miniodb.GetMetricsRequest{}
	resp, err := c.svc.GetMetrics(ctx, req)
	if err != nil {
		return nil, err
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return &MetricsResult{
		Timestamp:     resp.Timestamp.AsTime().Unix(),
		HTTPRequests:  resp.ResourceUsage["http_requests"],
		QueryDuration: resp.ResourceUsage["query_duration_ms"],
		BufferSize:    int(resp.ResourceUsage["buffer_pending_writes"]),
		MinioOps:      resp.ResourceUsage["minio_ops"],
		RedisOps:      resp.ResourceUsage["redis_total_keys"],
		Goroutines:    runtime.NumGoroutine(),
		MemAlloc:      int64(memStats.Alloc),
		GCPauseNs:     int64(memStats.PauseTotalNs),
	}, nil
}

func (c *LocalClient) GetNodes(ctx context.Context) ([]*NodeResult, error) {
	if c.sr == nil {
		return []*NodeResult{
			{
				ID:       c.cfg.Server.NodeID,
				Address:  "localhost",
				Port:     c.cfg.Server.GrpcPort,
				Status:   "healthy",
				LastSeen: time.Now(),
				Metadata: map[string]string{"mode": "single-node"},
			},
		}, nil
	}

	nodes, err := c.sr.GetHealthyNodes()
	if err != nil {
		return nil, err
	}

	var results []*NodeResult
	for _, n := range nodes {
		results = append(results, &NodeResult{
			ID:       n.ID,
			Address:  n.Address,
			Port:     n.Port,
			Status:   n.Status,
			LastSeen: n.LastSeen,
			Metadata: map[string]string{},
		})
	}
	return results, nil
}

func (c *LocalClient) GetClusterConfig(ctx context.Context) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"node_id":    c.cfg.Server.NodeID,
		"grpc_port":  c.cfg.Server.GrpcPort,
		"rest_port":  c.cfg.Server.RestPort,
		"mode":       c.getMode(),
		"version":    version.Get(),
		"redis_addr": c.cfg.Network.Pools.Redis.Addr,
		"minio_addr": c.cfg.Network.Pools.MinIO.Endpoint,
	}
	return result, nil
}

func (c *LocalClient) GetFullConfig(_ context.Context) (*FullConfig, error) {
	redis := c.cfg.Network.Pools.Redis
	minio := c.cfg.Network.Pools.MinIO

	cfg := &FullConfig{
		NodeID:   c.cfg.Server.NodeID,
		GrpcPort: c.cfg.Server.GrpcPort,
		RestPort: c.cfg.Server.RestPort,

		RedisMode:       redis.Mode,
		RedisAddr:       redis.Addr,
		RedisPassword:   redis.Password,
		RedisDB:         redis.DB,
		RedisMasterName: redis.MasterName,
		SentinelAddrs:   redis.SentinelAddrs,
		ClusterAddrs:    redis.ClusterAddrs,

		MinioEndpoint:        minio.Endpoint,
		MinioAccessKeyID:     minio.AccessKeyID,
		MinioSecretAccessKey: minio.SecretAccessKey,
		MinioUseSSL:          minio.UseSSL,
		MinioRegion:          minio.Region,
		MinioBucket:          minio.Bucket,

		CoreEndpoint:  c.cfg.Dashboard.CoreEndpoint,
		DashboardPort: c.cfg.Dashboard.Port,

		LogLevel:    c.cfg.Log.Level,
		LogFormat:   c.cfg.Log.Format,
		LogOutput:   c.cfg.Log.Output,
		LogFilename: c.cfg.Log.Filename,

		BufferSize:    c.cfg.Buffer.BufferSize,
		FlushInterval: c.cfg.Buffer.FlushInterval.String(),

		Mode:    c.getMode(),
		Version: version.Get(),
	}

	// Detect which fields are currently overridden by environment variables.
	// These values are correct for the running process but will be re-applied
	// on every restart regardless of what config.yaml contains.
	cfg.EnvOverrides = detectEnvOverrides()
	return cfg, nil
}

// detectEnvOverrides returns a map of config-field JSON key → env-var name for
// every environment variable that is currently set and would override that field
// on the next config load.
func detectEnvOverrides() map[string]string {
	ov := make(map[string]string)
	if os.Getenv("REDIS_HOST") != "" {
		ov["redis_addr"] = "REDIS_HOST / REDIS_PORT"
	}
	if os.Getenv("REDIS_PASSWORD") != "" {
		ov["redis_password"] = "REDIS_PASSWORD"
	}
	if os.Getenv("REDIS_DB") != "" {
		ov["redis_db"] = "REDIS_DB"
	}
	if os.Getenv("MINIO_ENDPOINT") != "" {
		ov["minio_endpoint"] = "MINIO_ENDPOINT"
	}
	if os.Getenv("MINIO_ACCESS_KEY") != "" {
		ov["minio_access_key_id"] = "MINIO_ACCESS_KEY"
	}
	if os.Getenv("MINIO_SECRET_KEY") != "" {
		ov["minio_secret_access_key"] = "MINIO_SECRET_KEY"
	}
	if os.Getenv("MINIO_BUCKET") != "" {
		ov["minio_bucket"] = "MINIO_BUCKET"
	}
	if os.Getenv("MINIO_USE_SSL") != "" {
		ov["minio_use_ssl"] = "MINIO_USE_SSL"
	}
	if os.Getenv("GRPC_PORT") != "" {
		ov["grpc_port"] = "GRPC_PORT"
	}
	if os.Getenv("REST_PORT") != "" {
		ov["rest_port"] = "REST_PORT"
	}
	if os.Getenv("DASHBOARD_CORE_ENDPOINT") != "" {
		ov["core_endpoint"] = "DASHBOARD_CORE_ENDPOINT"
	}
	if os.Getenv("DASHBOARD_PORT") != "" {
		ov["dashboard_port"] = "DASHBOARD_PORT"
	}
	if len(ov) == 0 {
		return nil
	}
	return ov
}

func (c *LocalClient) UpdateConfig(_ context.Context, req *ConfigUpdateRequest) (*ConfigUpdateResult, error) {
	// Apply changes in-memory so the dashboard reflects new values immediately.
	if req.NodeID != "" {
		c.cfg.Server.NodeID = req.NodeID
	}
	if req.GrpcPort != "" {
		c.cfg.Server.GrpcPort = req.GrpcPort
	}
	if req.RestPort != "" {
		c.cfg.Server.RestPort = req.RestPort
	}
	if req.RedisMode != "" {
		c.cfg.Network.Pools.Redis.Mode = req.RedisMode
		c.cfg.Redis.Mode = req.RedisMode
	}
	if req.RedisAddr != "" {
		c.cfg.Network.Pools.Redis.Addr = req.RedisAddr
		c.cfg.Redis.Addr = req.RedisAddr
	}
	if req.RedisPassword != "" {
		c.cfg.Network.Pools.Redis.Password = req.RedisPassword
		c.cfg.Redis.Password = req.RedisPassword
	}
	if req.RedisDB != nil {
		c.cfg.Network.Pools.Redis.DB = *req.RedisDB
		c.cfg.Redis.DB = *req.RedisDB
	}
	if req.RedisMasterName != "" {
		c.cfg.Network.Pools.Redis.MasterName = req.RedisMasterName
	}
	if len(req.SentinelAddrs) > 0 {
		c.cfg.Network.Pools.Redis.SentinelAddrs = req.SentinelAddrs
	}
	if len(req.ClusterAddrs) > 0 {
		c.cfg.Network.Pools.Redis.ClusterAddrs = req.ClusterAddrs
	}
	if req.MinioEndpoint != "" {
		c.cfg.Network.Pools.MinIO.Endpoint = req.MinioEndpoint
		c.cfg.MinIO.Endpoint = req.MinioEndpoint
	}
	if req.MinioAccessKeyID != "" {
		c.cfg.Network.Pools.MinIO.AccessKeyID = req.MinioAccessKeyID
		c.cfg.MinIO.AccessKeyID = req.MinioAccessKeyID
	}
	if req.MinioSecretAccessKey != "" {
		c.cfg.Network.Pools.MinIO.SecretAccessKey = req.MinioSecretAccessKey
		c.cfg.MinIO.SecretAccessKey = req.MinioSecretAccessKey
	}
	if req.MinioUseSSL != nil {
		c.cfg.Network.Pools.MinIO.UseSSL = *req.MinioUseSSL
		c.cfg.MinIO.UseSSL = *req.MinioUseSSL
	}
	if req.MinioRegion != "" {
		c.cfg.Network.Pools.MinIO.Region = req.MinioRegion
	}
	if req.MinioBucket != "" {
		c.cfg.Network.Pools.MinIO.Bucket = req.MinioBucket
		c.cfg.MinIO.Bucket = req.MinioBucket
	}
	if req.CoreEndpoint != "" {
		c.cfg.Dashboard.CoreEndpoint = req.CoreEndpoint
	}
	if req.DashboardPort != "" {
		c.cfg.Dashboard.Port = req.DashboardPort
	}
	if req.LogLevel != "" {
		c.cfg.Log.Level = req.LogLevel
	}
	if req.LogFormat != "" {
		c.cfg.Log.Format = req.LogFormat
	}
	if req.LogOutput != "" {
		c.cfg.Log.Output = req.LogOutput
	}
	if req.LogFilename != "" {
		c.cfg.Log.Filename = req.LogFilename
	}
	if req.BufferSize != nil {
		c.cfg.Buffer.BufferSize = *req.BufferSize
	}
	if req.FlushInterval != "" {
		if d, err := time.ParseDuration(req.FlushInterval); err == nil {
			c.cfg.Buffer.FlushInterval = d
		}
	}

	// Generate the YAML snippet for the changed sections.
	snippet := generateConfigYAML(c.cfg, req)

	// Warn about env var overrides that will take precedence over config.yaml.
	overrides := detectEnvOverrides()
	msg := "配置已在内存中生效（当前连接不受影响）。将以下片段写入 config.yaml 后重启节点，使变更永久生效。"
	if len(overrides) > 0 {
		var warned []string
		for field, envVar := range overrides {
			warned = append(warned, fmt.Sprintf("%s（由 %s 覆盖）", field, envVar))
		}
		sort.Strings(warned)
		msg += "\n\n⚠️ 注意：以下字段当前由环境变量控制，重启后环境变量仍会覆盖 config.yaml 中的值：\n" +
			strings.Join(warned, "\n")
	}

	return &ConfigUpdateResult{
		ConfigSnippet:   snippet,
		RestartRequired: true,
		Message:         msg,
	}, nil
}

// generateConfigYAML produces a minimal YAML snippet covering only the sections
// that were touched by the update request.
func generateConfigYAML(cfg *config.Config, req *ConfigUpdateRequest) string {
	var sb strings.Builder

	if req.NodeID != "" || req.GrpcPort != "" || req.RestPort != "" {
		sb.WriteString("server:\n")
		sb.WriteString(fmt.Sprintf("  node_id: %q\n", cfg.Server.NodeID))
		sb.WriteString(fmt.Sprintf("  grpc_port: %q\n", cfg.Server.GrpcPort))
		sb.WriteString(fmt.Sprintf("  rest_port: %q\n", cfg.Server.RestPort))
		sb.WriteString("\n")
	}

	redisChanged := req.RedisMode != "" || req.RedisAddr != "" || req.RedisPassword != "" ||
		req.RedisDB != nil || req.RedisMasterName != "" || len(req.SentinelAddrs) > 0 || len(req.ClusterAddrs) > 0
	if redisChanged {
		r := cfg.Network.Pools.Redis
		sb.WriteString("network:\n  pools:\n    redis:\n")
		sb.WriteString(fmt.Sprintf("      mode: %q\n", r.Mode))
		switch r.Mode {
		case "sentinel":
			sb.WriteString(fmt.Sprintf("      master_name: %q\n", r.MasterName))
			sb.WriteString("      sentinel_addrs:\n")
			for _, a := range r.SentinelAddrs {
				sb.WriteString(fmt.Sprintf("        - %q\n", a))
			}
		case "cluster":
			sb.WriteString("      cluster_addrs:\n")
			for _, a := range r.ClusterAddrs {
				sb.WriteString(fmt.Sprintf("        - %q\n", a))
			}
		default:
			sb.WriteString(fmt.Sprintf("      addr: %q\n", r.Addr))
			if r.DB > 0 {
				sb.WriteString(fmt.Sprintf("      db: %d\n", r.DB))
			}
		}
		if r.Password != "" {
			sb.WriteString(fmt.Sprintf("      password: %q\n", r.Password))
		}
		sb.WriteString("\n")
	}

	minioChanged := req.MinioEndpoint != "" || req.MinioAccessKeyID != "" || req.MinioSecretAccessKey != "" ||
		req.MinioUseSSL != nil || req.MinioRegion != "" || req.MinioBucket != ""
	if minioChanged {
		m := cfg.Network.Pools.MinIO
		sb.WriteString("    minio:\n")
		sb.WriteString(fmt.Sprintf("      endpoint: %q\n", m.Endpoint))
		sb.WriteString(fmt.Sprintf("      access_key_id: %q\n", m.AccessKeyID))
		sb.WriteString(fmt.Sprintf("      secret_access_key: %q\n", m.SecretAccessKey))
		sb.WriteString(fmt.Sprintf("      use_ssl: %v\n", m.UseSSL))
		sb.WriteString(fmt.Sprintf("      region: %q\n", m.Region))
		sb.WriteString(fmt.Sprintf("      bucket: %q\n", m.Bucket))
		sb.WriteString("\n")
	}

	if req.CoreEndpoint != "" || req.DashboardPort != "" {
		sb.WriteString("dashboard:\n")
		sb.WriteString(fmt.Sprintf("  core_endpoint: %q\n", cfg.Dashboard.CoreEndpoint))
		sb.WriteString(fmt.Sprintf("  port: %q\n", cfg.Dashboard.Port))
		sb.WriteString("\n")
	}

	if req.LogLevel != "" || req.LogFormat != "" || req.LogOutput != "" || req.LogFilename != "" {
		l := cfg.Log
		sb.WriteString("log:\n")
		sb.WriteString(fmt.Sprintf("  level: %q\n", l.Level))
		sb.WriteString(fmt.Sprintf("  format: %q\n", l.Format))
		sb.WriteString(fmt.Sprintf("  output: %q\n", l.Output))
		if l.Filename != "" {
			sb.WriteString(fmt.Sprintf("  filename: %q\n", l.Filename))
		}
		sb.WriteString("\n")
	}

	if req.BufferSize != nil || req.FlushInterval != "" {
		sb.WriteString("buffer:\n")
		sb.WriteString(fmt.Sprintf("  buffer_size: %d\n", cfg.Buffer.BufferSize))
		sb.WriteString(fmt.Sprintf("  flush_interval: %s\n", cfg.Buffer.FlushInterval))
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

func (c *LocalClient) EnableDistributedMode(_ context.Context, req *EnableDistributedRequest) (*EnableDistributedResult, error) {
	var sb strings.Builder
	sb.WriteString("network:\n  pools:\n    redis:\n")
	sb.WriteString(fmt.Sprintf("      mode: \"%s\"\n", req.RedisMode))

	switch req.RedisMode {
	case "sentinel":
		sb.WriteString(fmt.Sprintf("      master_name: \"%s\"\n", req.MasterName))
		sb.WriteString("      sentinel_addrs:\n")
		for _, addr := range req.SentinelAddrs {
			sb.WriteString(fmt.Sprintf("        - \"%s\"\n", addr))
		}
		if req.SentinelPassword != "" {
			sb.WriteString(fmt.Sprintf("      sentinel_password: \"%s\"\n", req.SentinelPassword))
		}
	case "cluster":
		sb.WriteString("      cluster_addrs:\n")
		for _, addr := range req.ClusterAddrs {
			sb.WriteString(fmt.Sprintf("        - \"%s\"\n", addr))
		}
	default: // standalone
		if req.RedisAddr != "" {
			sb.WriteString(fmt.Sprintf("      addr: \"%s\"\n", req.RedisAddr))
		}
		if req.RedisDB > 0 {
			sb.WriteString(fmt.Sprintf("      db: %d\n", req.RedisDB))
		}
	}
	if req.RedisPassword != "" {
		sb.WriteString(fmt.Sprintf("      password: \"%s\"\n", req.RedisPassword))
	}

	return &EnableDistributedResult{
		ConfigSnippet:   sb.String(),
		RestartRequired: true,
		Message:         "请将以下 Redis 配置替换 config.yaml 中的 network.pools.redis 节，然后重启服务以开启分布式模式",
	}, nil
}

func (c *LocalClient) ListTables(ctx context.Context) ([]*TableResult, error) {
	req := &miniodb.ListTablesRequest{}
	resp, err := c.svc.ListTables(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []*TableResult
	for _, t := range resp.Tables {
		createdAt := time.Time{}
		if t.CreatedAt != nil {
			createdAt = t.CreatedAt.AsTime()
		}
		results = append(results, &TableResult{
			Name:        t.Name,
			RowCountEst: 0,
			SizeBytes:   0,
			CreatedAt:   createdAt,
		})
	}
	return results, nil
}

func (c *LocalClient) GetTable(ctx context.Context, name string) (*TableDetailResult, error) {
	req := &miniodb.GetTableRequest{TableName: name}
	resp, err := c.svc.GetTable(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.TableInfo == nil {
		return nil, fmt.Errorf("table %s not found", name)
	}

	t := resp.TableInfo
	var tblConfig config.TableConfig
	if t.Config != nil {
		tblConfig = config.TableConfig{
			BufferSize:     int(t.Config.BufferSize),
			FlushInterval:  time.Duration(t.Config.FlushIntervalSeconds) * time.Second,
			RetentionDays:  int(t.Config.RetentionDays),
			BackupEnabled:  t.Config.BackupEnabled,
			Properties:     t.Config.Properties,
			IDStrategy:     t.Config.IdStrategy,
			IDPrefix:       t.Config.IdPrefix,
			AutoGenerateID: t.Config.AutoGenerateId,
		}
	}

	createdAt := time.Time{}
	if t.CreatedAt != nil {
		createdAt = t.CreatedAt.AsTime()
	}

	return &TableDetailResult{
		TableResult: TableResult{
			Name:      t.Name,
			CreatedAt: createdAt,
		},
		Config:        tblConfig,
		BufferSize:    tblConfig.BufferSize,
		FlushInterval: int64(tblConfig.FlushInterval.Seconds()),
		RetentionDays: tblConfig.RetentionDays,
		BackupEnabled: tblConfig.BackupEnabled,
		IDStrategy:    tblConfig.IDStrategy,
	}, nil
}

func (c *LocalClient) CreateTable(ctx context.Context, req *CreateTableRequest) error {
	var protoConfig *miniodb.TableConfig
	if req.Config.IDStrategy != "" {
		protoConfig = &miniodb.TableConfig{
			BufferSize:           int32(req.Config.BufferSize),
			FlushIntervalSeconds: int32(req.Config.FlushInterval.Seconds()),
			RetentionDays:        int32(req.Config.RetentionDays),
			BackupEnabled:        req.Config.BackupEnabled,
			Properties:           req.Config.Properties,
			IdStrategy:           req.Config.IDStrategy,
			IdPrefix:             req.Config.IDPrefix,
			AutoGenerateId:       req.Config.AutoGenerateID,
		}
	}

	protoReq := &miniodb.CreateTableRequest{
		TableName:   req.TableName,
		Config:      protoConfig,
		IfNotExists: true,
	}

	resp, err := c.svc.CreateTable(ctx, protoReq)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Message)
	}
	return nil
}

func (c *LocalClient) UpdateTable(ctx context.Context, name string, req *UpdateTableRequest) error {
	if c.mm == nil {
		return fmt.Errorf("metadata manager not available")
	}
	if err := c.mm.SaveTableConfig(ctx, name, req.Config); err != nil {
		return fmt.Errorf("failed to update table config: %w", err)
	}
	return nil
}

func (c *LocalClient) DeleteTable(ctx context.Context, name string) error {
	req := &miniodb.DeleteTableRequest{TableName: name}
	resp, err := c.svc.DeleteTable(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Message)
	}
	return nil
}

func (c *LocalClient) GetTableStats(ctx context.Context, name string) (*TableStatsResult, error) {
	req := &miniodb.GetTableRequest{TableName: name}
	resp, err := c.svc.GetTable(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &TableStatsResult{Name: name}
	if resp.TableInfo != nil && resp.TableInfo.Stats != nil {
		result.Columns = []ColumnStats{}
	}
	return result, nil
}

func (c *LocalClient) BrowseData(ctx context.Context, table string, params *BrowseParams) (*BrowseResult, error) {
	if params == nil {
		params = &BrowseParams{Page: 1, PageSize: 50}
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 50
	}

	if !c.cfg.IsValidTableName(table) {
		return nil, fmt.Errorf("invalid table name: %s", table)
	}
	quotedTable := security.DefaultSanitizer.QuoteIdentifier(table)

	offset := (params.Page - 1) * params.PageSize
	var sqlBuilder strings.Builder
	sqlBuilder.WriteString("SELECT * FROM ")
	sqlBuilder.WriteString(quotedTable)

	if params.Filter != "" {
		sqlBuilder.WriteString(" WHERE ")
		sqlBuilder.WriteString(params.Filter)
	}

	if params.SortBy != "" {
		if err := security.DefaultSanitizer.ValidateTableName(params.SortBy); err != nil {
			return nil, fmt.Errorf("invalid sort column: %w", err)
		}
		order := "ASC"
		if strings.ToUpper(params.SortOrder) == "DESC" {
			order = "DESC"
		}
		sqlBuilder.WriteString(" ORDER BY ")
		sqlBuilder.WriteString(security.DefaultSanitizer.QuoteIdentifier(params.SortBy))
		sqlBuilder.WriteString(" ")
		sqlBuilder.WriteString(order)
	}

	sqlBuilder.WriteString(fmt.Sprintf(" LIMIT %d OFFSET %d", params.PageSize, offset))

	result, err := c.QuerySQL(ctx, sqlBuilder.String())
	if err != nil {
		return nil, err
	}

	return &BrowseResult{
		Rows:     result.Rows,
		Total:    result.Total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

func (c *LocalClient) WriteRecord(ctx context.Context, table string, req *WriteRecordRequest) error {
	payload, err := structFromMap(req.Payload)
	if err != nil {
		return err
	}

	protoReq := &miniodb.WriteDataRequest{
		Table: table,
		Data: &miniodb.DataRecord{
			Id:        req.ID,
			Timestamp: timestamppb.New(time.UnixMilli(req.Timestamp)),
			Payload:   payload,
		},
	}

	resp, err := c.svc.WriteData(ctx, protoReq)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Message)
	}
	return nil
}

func (c *LocalClient) WriteBatch(ctx context.Context, table string, records []*WriteRecordRequest) error {
	for _, r := range records {
		if err := c.WriteRecord(ctx, table, r); err != nil {
			return err
		}
	}
	return nil
}

func (c *LocalClient) UpdateRecord(ctx context.Context, table, id string, req *UpdateRecordRequest) error {
	payload, err := structFromMap(req.Payload)
	if err != nil {
		return err
	}

	protoReq := &miniodb.UpdateDataRequest{
		Table:     table,
		Id:        id,
		Timestamp: timestamppb.New(time.UnixMilli(req.Timestamp)),
		Payload:   payload,
	}

	resp, err := c.svc.UpdateData(ctx, protoReq)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Message)
	}
	return nil
}

func (c *LocalClient) DeleteRecord(ctx context.Context, table, id, day string) error {
	protoReq := &miniodb.DeleteDataRequest{
		Table: table,
		Id:    id,
	}

	resp, err := c.svc.DeleteData(ctx, protoReq)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Message)
	}
	return nil
}

func (c *LocalClient) QuerySQL(ctx context.Context, sql string) (*QueryResult, error) {
	req := &miniodb.QueryDataRequest{Sql: sql}
	resp, err := c.svc.QueryData(ctx, req)
	if err != nil {
		return nil, err
	}

	var rows []map[string]interface{}
	if resp.ResultJson != "" {
		if err := json.Unmarshal([]byte(resp.ResultJson), &rows); err != nil {
			return nil, fmt.Errorf("failed to parse query result: %w", err)
		}
	}

	var columns []string
	if len(rows) > 0 {
		for k := range rows[0] {
			columns = append(columns, k)
		}
		sort.Strings(columns)
	}

	return &QueryResult{
		Columns:    columns,
		Rows:       rows,
		Total:      int64(len(rows)),
		DurationMs: 0,
	}, nil
}

func (c *LocalClient) ListBackups(ctx context.Context) ([]*BackupResult, error) {
	req := &miniodb.ListBackupsRequest{Days: 30}
	resp, err := c.svc.ListBackups(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []*BackupResult
	for _, b := range resp.Backups {
		results = append(results, &BackupResult{
			ID:        b.ObjectName,
			Type:      "metadata",
			Status:    "completed",
			SizeBytes: b.Size,
			CreatedAt: b.Timestamp.AsTime(),
		})
	}
	return results, nil
}

func (c *LocalClient) TriggerMetadataBackup(ctx context.Context) (*BackupResult, error) {
	req := &miniodb.BackupMetadataRequest{}
	resp, err := c.svc.BackupMetadata(ctx, req)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, errors.New(resp.Message)
	}

	return &BackupResult{
		ID:        resp.BackupId,
		Type:      "metadata",
		Status:    "completed",
		CreatedAt: resp.Timestamp.AsTime(),
	}, nil
}

func (c *LocalClient) TriggerFullBackup(ctx context.Context) (*BackupResult, error) {
	return nil, fmt.Errorf("not implemented: full backup requires distributed coordinator")
}

func (c *LocalClient) TriggerTableBackup(ctx context.Context, tableName string) (*BackupResult, error) {
	return nil, fmt.Errorf("not implemented: table backup requires distributed coordinator")
}

func (c *LocalClient) GetBackup(ctx context.Context, id string) (*BackupResult, error) {
	backups, err := c.ListBackups(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range backups {
		if b.ID == id {
			return b, nil
		}
	}
	return nil, fmt.Errorf("backup %s not found", id)
}

func (c *LocalClient) RestoreBackup(ctx context.Context, id string, opts *RestoreOptions) error {
	return fmt.Errorf("not implemented: restore requires distributed coordinator")
}

func (c *LocalClient) DeleteBackup(ctx context.Context, id string) error {
	return fmt.Errorf("not implemented: delete backup requires distributed coordinator")
}

func (c *LocalClient) GetMetadataStatus(ctx context.Context) (*MetadataStatusResult, error) {
	req := &miniodb.GetMetadataStatusRequest{}
	resp, err := c.svc.GetMetadataStatus(ctx, req)
	if err != nil {
		return nil, err
	}

	var lastBackup time.Time
	if resp.LastBackup != nil {
		lastBackup = resp.LastBackup.AsTime()
	}

	tableCount := 0
	if c.mm != nil {
		if tables, err := c.mm.ListTableConfigs(ctx); err == nil {
			tableCount = len(tables)
		}
	}

	return &MetadataStatusResult{
		LastBackup:     lastBackup,
		TablesCount:    tableCount,
		BackupEnabled:  resp.BackupStatus["status"] == "enabled",
		BackupInterval: 3600,
	}, nil
}

func (c *LocalClient) QueryLogs(ctx context.Context, params *LogQueryParams) (*LogQueryResult, error) {
	if params == nil {
		params = &LogQueryParams{Page: 1, PageSize: 100}
	}

	logDir := c.getLogDir()

	files, err := c.ListLogFiles(ctx)
	if err != nil {
		return nil, err
	}

	var logs []LogEntry
	for _, f := range files {
		path := filepath.Join(logDir, f)
		entries, err := c.parseLogFile(path, params)
		if err != nil {
			c.logger.Warn("failed to parse log file", zap.String("file", path), zap.Error(err))
			continue
		}
		logs = append(logs, entries...)
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp > logs[j].Timestamp
	})

	total := int64(len(logs))
	start := (params.Page - 1) * params.PageSize
	end := start + params.PageSize
	if start > int(total) {
		start = int(total)
	}
	if end > int(total) {
		end = int(total)
	}

	return &LogQueryResult{
		Logs:     logs[start:end],
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

func (c *LocalClient) ListLogFiles(ctx context.Context) ([]string, error) {
	logDir := c.getLogDir()

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".log") || strings.HasSuffix(e.Name(), ".json")) {
			files = append(files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files, nil
}

func (c *LocalClient) GetMonitorOverview(ctx context.Context) (*MonitorOverviewResult, error) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(c.startTime)

	return &MonitorOverviewResult{
		Goroutines:  runtime.NumGoroutine(),
		MemAllocMB:  float64(memStats.Alloc) / 1024 / 1024,
		GCPauseMs:   float64(memStats.PauseTotalNs) / 1e6,
		UptimeHours: uptime.Hours(),
		CPUPercent:  0,
	}, nil
}

func (c *LocalClient) GetSLA(ctx context.Context) (*SLAResult, error) {
	return &SLAResult{
		QueryLatencyP50Ms: 0,
		QueryLatencyP95Ms: 0,
		QueryLatencyP99Ms: 0,
		WriteLatencyP95Ms: 0,
		CacheHitRate:      0,
		FilePruneRate:     0,
		ErrorRate:         0,
	}, nil
}

func (c *LocalClient) ScrapePrometheus(ctx context.Context) ([]byte, error) {
	metricsPort := c.cfg.Metrics.Port
	if metricsPort == "" {
		metricsPort = "9090"
	}

	url := fmt.Sprintf("http://localhost:%s/metrics", metricsPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (c *LocalClient) AnalyticsQuery(ctx context.Context, sql string) (*AnalyticsQueryResult, error) {
	result, err := c.QuerySQL(ctx, sql)
	if err != nil {
		return nil, err
	}

	return &AnalyticsQueryResult{
		Columns:     result.Columns,
		Rows:        result.Rows,
		Total:       result.Total,
		DurationMs:  result.DurationMs,
		ExplainPlan: "",
	}, nil
}

func (c *LocalClient) GetAnalyticsOverview(ctx context.Context) (*AnalyticsOverviewResult, error) {
	return &AnalyticsOverviewResult{
		WriteTrend: []TimeSeriesPoint{},
		QueryTrend: []TimeSeriesPoint{},
		DataVolume: []TimeSeriesPoint{},
	}, nil
}

func (c *LocalClient) getMode() string {
	if c.sr != nil && c.cfg.Network.Pools.Redis.Enabled {
		return "distributed"
	}
	return "single-node"
}

func (c *LocalClient) getLogDir() string {
	if c.cfg.Dashboard.LogDir != "" {
		return c.cfg.Dashboard.LogDir
	}
	if c.cfg.Log.Filename != "" {
		return filepath.Dir(c.cfg.Log.Filename)
	}
	return "./logs"
}

func (c *LocalClient) parseLogFile(path string, params *LogQueryParams) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var logs []LogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry struct {
			Level     string                 `json:"level"`
			Timestamp string                 `json:"ts"`
			Msg       string                 `json:"msg"`
			Fields    map[string]interface{} `json:"-"`
		}

		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if params.Level != "" && !strings.EqualFold(entry.Level, params.Level) {
			continue
		}

		if params.Keyword != "" && !strings.Contains(entry.Msg, params.Keyword) {
			continue
		}

		var ts int64
		if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
			ts = t.UnixMilli()
		}

		if params.StartTime > 0 && ts < params.StartTime {
			continue
		}
		if params.EndTime > 0 && ts > params.EndTime {
			continue
		}

		logs = append(logs, LogEntry{
			Level:     entry.Level,
			Timestamp: ts,
			Message:   entry.Msg,
			Fields:    map[string]interface{}{},
		})
	}

	return logs, scanner.Err()
}

func structFromMap(m map[string]interface{}) (*structpb.Struct, error) {
	if m == nil {
		return nil, nil
	}
	return structpb.NewStruct(m)
}
