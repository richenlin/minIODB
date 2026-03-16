//go:build dashboard

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"minIODB/config"
	. "minIODB/internal/dashboard/model"
	"minIODB/pkg/version"

	"go.uber.org/zap"
)

// RemoteClient calls the minIODB core REST API (cfg.Dashboard.CoreEndpoint).
// It obtains a Bearer JWT once using the first api_key_pairs entry in config
// and caches it, refreshing transparently on expiry.
type RemoteClient struct {
	httpClient  *http.Client
	cfg         *config.Config
	logger      *zap.Logger
	baseURL     string // cfg.Dashboard.CoreEndpoint, e.g. "http://miniodb:8081"
	metricsURL  string // Prometheus scrape URL, e.g. "http://miniodb:8082/metrics"
	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
	startTime   time.Time
}

func NewRemoteClient(cfg *config.Config, logger *zap.Logger) CoreClient {
	base := strings.TrimRight(cfg.Dashboard.CoreEndpoint, "/")
	if base == "" {
		base = "http://localhost:8081"
	}

	// Build Prometheus scrape URL from the metrics port in config (default 8082).
	metricsPort := "8082"
	if cfg.Metrics.Port != "" {
		metricsPort = strings.TrimLeft(cfg.Metrics.Port, ":")
	}
	// Replace the port in base URL with the metrics port.
	metricsBase := replacePort(base, metricsPort)

	return &RemoteClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cfg:        cfg,
		logger:     logger,
		baseURL:    base,
		metricsURL: metricsBase + "/metrics",
		startTime:  time.Now(),
	}
}

// replacePort replaces the port portion of a host:port URL.
func replacePort(rawURL, newPort string) string {
	// rawURL is like http://miniodb:8081
	lastColon := strings.LastIndex(rawURL, ":")
	if lastColon == -1 {
		return rawURL + ":" + newPort
	}
	// Make sure the colon is part of host:port, not scheme.
	if strings.HasPrefix(rawURL[lastColon:], "://") {
		return rawURL + ":" + newPort
	}
	return rawURL[:lastColon+1] + newPort
}

// ---------- token management ----------

// token returns a cached JWT or fetches a fresh one.
func (c *RemoteClient) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.cachedToken, nil
	}

	if len(c.cfg.Auth.APIKeyPairs) == 0 {
		return "", fmt.Errorf("no api_key_pairs configured; cannot authenticate with core service")
	}

	kp := c.cfg.Auth.APIKeyPairs[0]
	body, _ := json.Marshal(map[string]string{
		"api_key":    kp.Key,
		"api_secret": kp.Secret,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/auth/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth/token request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth/token: status %d body %s", resp.StatusCode, raw)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // seconds
	}
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return "", fmt.Errorf("auth/token: parse response: %w", err)
	}

	c.cachedToken = tokenResp.AccessToken
	expiry := tokenResp.ExpiresIn
	if expiry <= 0 {
		expiry = 3600
	}
	// Refresh a minute before expiry.
	c.tokenExpiry = time.Now().Add(time.Duration(expiry-60) * time.Second)

	return c.cachedToken, nil
}

// ---------- HTTP helpers ----------

func (c *RemoteClient) get(ctx context.Context, path string, out interface{}) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *RemoteClient) post(ctx context.Context, path string, in, out interface{}) error {
	return c.doJSON(ctx, http.MethodPost, path, in, out)
}

func (c *RemoteClient) put(ctx context.Context, path string, in, out interface{}) error {
	return c.doJSON(ctx, http.MethodPut, path, in, out)
}

func (c *RemoteClient) delete(ctx context.Context, path string, in, out interface{}) error {
	return c.doJSON(ctx, http.MethodDelete, path, in, out)
}

func (c *RemoteClient) doJSON(ctx context.Context, method, path string, in, out interface{}) error {
	tok, err := c.token(ctx)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}

	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// If token expired, invalidate cache so next call re-fetches.
		if resp.StatusCode == http.StatusUnauthorized {
			c.mu.Lock()
			c.cachedToken = ""
			c.mu.Unlock()
		}
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &errBody)
		msg := errBody.Error
		if msg == "" {
			msg = string(raw)
		}
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, msg)
	}

	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// ---------- CoreClient implementation ----------

func (c *RemoteClient) GetHealth(ctx context.Context) (*HealthResult, error) {
	// /v1/health does not require auth.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	status, _ := raw["status"].(string)
	ver, _ := raw["version"].(string)

	mode := "standalone"
	if c.cfg.Network.Pools.Redis.Enabled {
		mode = "distributed"
	}

	return &HealthResult{
		Status:    status,
		Timestamp: time.Now().Unix(),
		Version:   ver,
		NodeID:    c.cfg.Server.NodeID,
		Mode:      mode,
	}, nil
}

type coreStatus struct {
	Timestamp   interface{}              `json:"timestamp"`
	BufferStats map[string]interface{}   `json:"buffer_stats"`
	RedisStats  map[string]interface{}   `json:"redis_stats"`
	MinioStats  map[string]interface{}   `json:"minio_stats"`
	Nodes       []map[string]interface{} `json:"nodes"`
	TotalNodes  int                      `json:"total_nodes"`
}

func (c *RemoteClient) GetStatus(ctx context.Context) (*StatusResult, error) {
	var st coreStatus
	if err := c.get(ctx, "/v1/status", &st); err != nil {
		return nil, err
	}

	var tableCount int
	if tables, err := c.ListTables(ctx); err == nil {
		tableCount = len(tables)
	}

	var pendingWrites int64
	if pw, ok := st.BufferStats["pending_writes"]; ok {
		switch v := pw.(type) {
		case float64:
			pendingWrites = int64(v)
		case int64:
			pendingWrites = v
		}
	}

	clusterMode := "standalone"
	if c.cfg.Network.Pools.Redis.Enabled {
		clusterMode = "distributed"
	}

	return &StatusResult{
		Version:       version.Get(),
		NodeID:        c.cfg.Server.NodeID,
		Mode:          clusterMode,
		Tables:        tableCount,
		Uptime:        int64(time.Since(c.startTime).Seconds()),
		PendingWrites: pendingWrites,
	}, nil
}

func (c *RemoteClient) GetMetrics(ctx context.Context) (*MetricsResult, error) {
	var raw struct {
		Timestamp          interface{}            `json:"timestamp"`
		PerformanceMetrics map[string]interface{} `json:"performance_metrics"`
		ResourceUsage      map[string]int64       `json:"resource_usage"`
	}
	if err := c.get(ctx, "/v1/metrics", &raw); err != nil {
		return nil, err
	}

	ru := raw.ResourceUsage
	return &MetricsResult{
		Timestamp:     time.Now().Unix(),
		HTTPRequests:  ru["http_requests"],
		QueryDuration: ru["query_duration_ms"],
		BufferSize:    int(ru["buffer_pending_writes"]),
		MinioOps:      ru["minio_ops"],
		RedisOps:      ru["redis_total_keys"],
	}, nil
}

func (c *RemoteClient) GetNodes(ctx context.Context) ([]*NodeResult, error) {
	var st coreStatus
	if err := c.get(ctx, "/v1/status", &st); err != nil {
		return nil, err
	}

	if len(st.Nodes) == 0 {
		return []*NodeResult{
			{
				ID:       c.cfg.Server.NodeID,
				Address:  c.baseURL,
				Status:   "healthy",
				LastSeen: time.Now(),
				Metadata: map[string]string{"mode": "single-node"},
			},
		}, nil
	}

	var results []*NodeResult
	for _, n := range st.Nodes {
		id, _ := n["id"].(string)
		addr, _ := n["address"].(string)
		port, _ := n["port"].(string)
		status, _ := n["status"].(string)
		if status == "" {
			status = "healthy"
		}
		results = append(results, &NodeResult{
			ID:       id,
			Address:  addr,
			Port:     port,
			Status:   status,
			LastSeen: time.Now(),
			Metadata: map[string]string{},
		})
	}
	return results, nil
}

func (c *RemoteClient) GetClusterConfig(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"node_id":       c.cfg.Server.NodeID,
		"grpc_port":     c.cfg.Server.GrpcPort,
		"rest_port":     c.cfg.Server.RestPort,
		"core_endpoint": c.cfg.Dashboard.CoreEndpoint,
		"version":       version.Get(),
		"redis_addr":    c.cfg.Network.Pools.Redis.Addr,
		"minio_addr":    c.cfg.Network.Pools.MinIO.Endpoint,
	}, nil
}

// ---------- table ----------

// protoTimestamp handles both proto3 JSON encoding formats:
//   - object: {"seconds": 1234567890, "nanos": 0}
//   - string:  "2026-03-16T07:21:08Z"
type protoTimestamp struct{ time.Time }

func (t *protoTimestamp) UnmarshalJSON(b []byte) error {
	// Try string format first.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		parsed, err := time.Parse(time.RFC3339, s)
		if err == nil {
			t.Time = parsed
			return nil
		}
	}
	// Fall back to proto object format {"seconds": N}.
	var obj struct {
		Seconds int64 `json:"seconds"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		t.Time = time.Unix(obj.Seconds, 0)
		return nil
	}
	return nil
}

type coreListTablesResp struct {
	Tables []struct {
		Name      string         `json:"name"`
		CreatedAt protoTimestamp `json:"created_at"`
	} `json:"tables"`
}

func (c *RemoteClient) ListTables(ctx context.Context) ([]*TableResult, error) {
	var resp coreListTablesResp
	if err := c.get(ctx, "/v1/tables", &resp); err != nil {
		return nil, err
	}

	var results []*TableResult
	for _, t := range resp.Tables {
		results = append(results, &TableResult{
			Name:      t.Name,
			CreatedAt: t.CreatedAt.Time,
		})
	}
	return results, nil
}

// GetTotalRecords queries COUNT(*) for every table concurrently and sums the results.
// Tables that have no flushed data (query returns an error) are counted as 0.
func (c *RemoteClient) GetTotalRecords(ctx context.Context) int64 {
	tables, err := c.ListTables(ctx)
	if err != nil || len(tables) == 0 {
		return 0
	}

	type countResult struct{ n int64 }
	ch := make(chan countResult, len(tables))

	for _, t := range tables {
		go func(name string) {
			r, err := c.QuerySQL(ctx, fmt.Sprintf("SELECT COUNT(*) AS cnt FROM `%s`", name))
			if err != nil || len(r.Rows) == 0 {
				ch <- countResult{0}
				return
			}
			var n int64
			// DuckDB may return the alias as "cnt" or keep "count_star()".
			for _, key := range []string{"cnt", "count_star()", "count(*)"} {
				if v, ok := r.Rows[0][key]; ok {
					switch val := v.(type) {
					case float64:
						n = int64(val)
					case int64:
						n = val
					}
					break
				}
			}
			ch <- countResult{n}
		}(t.Name)
	}

	var total int64
	for range tables {
		total += (<-ch).n
	}
	return total
}

type coreGetTableResp struct {
	TableInfo *struct {
		Name      string         `json:"name"`
		CreatedAt protoTimestamp `json:"created_at"`
		Config    *struct {
			BufferSize           int32             `json:"buffer_size"`
			FlushIntervalSeconds int32             `json:"flush_interval_seconds"`
			RetentionDays        int32             `json:"retention_days"`
			BackupEnabled        bool              `json:"backup_enabled"`
			Properties           map[string]string `json:"properties"`
			IDStrategy           string            `json:"id_strategy"`
			IDPrefix             string            `json:"id_prefix"`
			AutoGenerateID       bool              `json:"auto_generate_id"`
		} `json:"config"`
	} `json:"table_info"`
}

func (c *RemoteClient) GetTable(ctx context.Context, name string) (*TableDetailResult, error) {
	var resp coreGetTableResp
	if err := c.get(ctx, "/v1/tables/"+name, &resp); err != nil {
		return nil, err
	}
	if resp.TableInfo == nil {
		return nil, fmt.Errorf("table %s not found", name)
	}

	t := resp.TableInfo
	created := t.CreatedAt.Time

	res := &TableDetailResult{
		TableResult: TableResult{
			Name:      t.Name,
			CreatedAt: created,
		},
	}

	if t.Config != nil {
		res.BufferSize = int(t.Config.BufferSize)
		res.FlushInterval = int64(t.Config.FlushIntervalSeconds)
		res.RetentionDays = int(t.Config.RetentionDays)
		res.BackupEnabled = t.Config.BackupEnabled
		res.IDStrategy = t.Config.IDStrategy
	}
	return res, nil
}

func (c *RemoteClient) CreateTable(ctx context.Context, req *CreateTableRequest) error {
	body := map[string]interface{}{
		"table_name":   req.TableName,
		"if_not_exists": true,
		"config": map[string]interface{}{
			"buffer_size":            req.Config.BufferSize,
			"flush_interval_seconds": int(req.Config.FlushInterval.Seconds()),
			"retention_days":         req.Config.RetentionDays,
			"backup_enabled":         req.Config.BackupEnabled,
			"id_strategy":            req.Config.IDStrategy,
			"id_prefix":              req.Config.IDPrefix,
			"auto_generate_id":       req.Config.AutoGenerateID,
		},
	}
	var out map[string]interface{}
	return c.post(ctx, "/v1/tables", body, &out)
}

func (c *RemoteClient) UpdateTable(ctx context.Context, name string, req *UpdateTableRequest) error {
	// Core REST API has no direct PATCH/PUT table config endpoint; recreate is not safe.
	// Best-effort: return without error so the dashboard doesn't crash.
	return fmt.Errorf("update table config via remote client is not supported; modify config.yaml and restart the core service")
}

func (c *RemoteClient) DeleteTable(ctx context.Context, name string) error {
	var out map[string]interface{}
	return c.delete(ctx, "/v1/tables/"+name+"?if_exists=true", nil, &out)
}

func (c *RemoteClient) GetTableStats(ctx context.Context, name string) (*TableStatsResult, error) {
	// Core exposes basic stats inside getTable response.
	tbl, err := c.GetTable(ctx, name)
	if err != nil {
		return nil, err
	}
	return &TableStatsResult{
		Name:    tbl.Name,
		Columns: []ColumnStats{},
	}, nil
}

// ---------- data ----------

func (c *RemoteClient) BrowseData(ctx context.Context, table string, params *BrowseParams) (*BrowseResult, error) {
	if params == nil {
		params = &BrowseParams{Page: 1, PageSize: 50}
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 50
	}

	offset := (params.Page - 1) * params.PageSize
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SELECT * FROM `%s`", table))
	if params.Filter != "" {
		sb.WriteString(" WHERE " + params.Filter)
	}
	if params.SortBy != "" {
		order := "ASC"
		if strings.ToUpper(params.SortOrder) == "DESC" {
			order = "DESC"
		}
		sb.WriteString(fmt.Sprintf(" ORDER BY `%s` %s", params.SortBy, order))
	}
	sb.WriteString(fmt.Sprintf(" LIMIT %d OFFSET %d", params.PageSize, offset))

	result, err := c.QuerySQL(ctx, sb.String())
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

func (c *RemoteClient) WriteRecord(ctx context.Context, table string, req *WriteRecordRequest) error {
	ts := time.Now()
	if req.Timestamp > 0 {
		ts = time.UnixMilli(req.Timestamp)
	}
	body := map[string]interface{}{
		"table":     table,
		"id":        req.ID,
		"timestamp": ts.Format(time.RFC3339),
		"payload":   req.Payload,
	}
	var out map[string]interface{}
	return c.post(ctx, "/v1/data", body, &out)
}

func (c *RemoteClient) WriteBatch(ctx context.Context, table string, records []*WriteRecordRequest) error {
	for _, r := range records {
		if err := c.WriteRecord(ctx, table, r); err != nil {
			return err
		}
	}
	return nil
}

func (c *RemoteClient) UpdateRecord(ctx context.Context, table, id string, req *UpdateRecordRequest) error {
	body := map[string]interface{}{
		"table":   table,
		"id":      id,
		"payload": req.Payload,
	}
	var out map[string]interface{}
	return c.put(ctx, "/v1/data", body, &out)
}

func (c *RemoteClient) DeleteRecord(ctx context.Context, table, id, _ string) error {
	body := map[string]interface{}{
		"table": table,
		"ids":   []string{id},
	}
	var out map[string]interface{}
	return c.delete(ctx, "/v1/data", body, &out)
}

func (c *RemoteClient) QuerySQL(ctx context.Context, sql string) (*QueryResult, error) {
	body := map[string]interface{}{"sql": sql}
	var resp struct {
		ResultJSON string `json:"result_json"`
		HasMore    bool   `json:"has_more"`
	}
	if err := c.post(ctx, "/v1/query", body, &resp); err != nil {
		return nil, err
	}

	var rows []map[string]interface{}
	if resp.ResultJSON != "" {
		if err := json.Unmarshal([]byte(resp.ResultJSON), &rows); err != nil {
			return nil, fmt.Errorf("parse query result: %w", err)
		}
	}
	if rows == nil {
		rows = []map[string]interface{}{}
	}

	var columns []string
	if len(rows) > 0 {
		for k := range rows[0] {
			columns = append(columns, k)
		}
		sort.Strings(columns)
	}

	return &QueryResult{
		Columns: columns,
		Rows:    rows,
		Total:   int64(len(rows)),
	}, nil
}

// ---------- backup ----------

type coreBackupListResp struct {
	Backups []struct {
		ObjectName string `json:"object_name"`
		Timestamp  string `json:"timestamp"`
		Size       int64  `json:"size"`
	} `json:"backups"`
}

func (c *RemoteClient) ListBackups(ctx context.Context) ([]*BackupResult, error) {
	var resp coreBackupListResp
	if err := c.get(ctx, "/v1/metadata/backups?days=30", &resp); err != nil {
		return nil, err
	}

	var results []*BackupResult
	for _, b := range resp.Backups {
		ts, _ := time.Parse(time.RFC3339, b.Timestamp)
		results = append(results, &BackupResult{
			ID:        b.ObjectName,
			Type:      "metadata",
			Status:    "completed",
			SizeBytes: b.Size,
			CreatedAt: ts,
		})
	}
	return results, nil
}

func (c *RemoteClient) TriggerMetadataBackup(ctx context.Context) (*BackupResult, error) {
	var resp struct {
		Success   bool   `json:"success"`
		Message   string `json:"message"`
		BackupID  string `json:"backup_id"`
		Timestamp string `json:"timestamp"`
	}
	if err := c.post(ctx, "/v1/metadata/backup", map[string]bool{"force": true}, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Message)
	}
	ts, _ := time.Parse(time.RFC3339, resp.Timestamp)
	return &BackupResult{
		ID:        resp.BackupID,
		Type:      "metadata",
		Status:    "completed",
		CreatedAt: ts,
	}, nil
}

func (c *RemoteClient) TriggerFullBackup(ctx context.Context) (*BackupResult, error) {
	return nil, fmt.Errorf("full backup is not supported via the REST API")
}

func (c *RemoteClient) TriggerTableBackup(ctx context.Context, tableName string) (*BackupResult, error) {
	return nil, fmt.Errorf("table backup is not supported via the REST API")
}

func (c *RemoteClient) GetBackup(ctx context.Context, id string) (*BackupResult, error) {
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

func (c *RemoteClient) RestoreBackup(ctx context.Context, id string, opts *RestoreOptions) error {
	body := map[string]interface{}{
		"backup_file":  id,
		"from_latest":  id == "",
		"overwrite":    true,
	}
	var resp map[string]interface{}
	return c.post(ctx, "/v1/metadata/restore", body, &resp)
}

func (c *RemoteClient) DeleteBackup(ctx context.Context, id string) error {
	return fmt.Errorf("delete backup is not supported via the REST API")
}

func (c *RemoteClient) GetMetadataStatus(ctx context.Context) (*MetadataStatusResult, error) {
	var resp struct {
		LastBackup string `json:"last_backup"`
	}
	if err := c.get(ctx, "/v1/metadata/status", &resp); err != nil {
		return nil, err
	}

	lastBackup, _ := time.Parse(time.RFC3339, resp.LastBackup)

	tableCount := 0
	if tables, err := c.ListTables(ctx); err == nil {
		tableCount = len(tables)
	}

	return &MetadataStatusResult{
		LastBackup:    lastBackup,
		TablesCount:   tableCount,
		BackupEnabled: c.cfg.Backup.Enabled,
	}, nil
}

// ---------- logs ----------

func (c *RemoteClient) QueryLogs(_ context.Context, params *LogQueryParams) (*LogQueryResult, error) {
	if params == nil {
		params = &LogQueryParams{Page: 1, PageSize: 100}
	}
	// Core does not expose a log query endpoint; return empty result.
	return &LogQueryResult{
		Logs:     []LogEntry{},
		Total:    0,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

func (c *RemoteClient) ListLogFiles(_ context.Context) ([]string, error) {
	return []string{}, nil
}

// ---------- monitor ----------

func (c *RemoteClient) GetMonitorOverview(ctx context.Context) (*MonitorOverviewResult, error) {
	m, err := c.GetMetrics(ctx)
	if err != nil {
		return nil, err
	}
	uptime := time.Since(c.startTime).Hours()

	return &MonitorOverviewResult{
		Goroutines:  m.Goroutines,
		MemAllocMB:  float64(m.MemAlloc) / 1024 / 1024,
		GCPauseMs:   float64(m.GCPauseNs) / 1e6,
		UptimeHours: uptime,
	}, nil
}

func (c *RemoteClient) GetSLA(ctx context.Context) (*SLAResult, error) {
	var raw struct {
		PerformanceMetrics map[string]float64 `json:"performance_metrics"`
	}
	if err := c.get(ctx, "/v1/metrics", &raw); err != nil {
		// Non-fatal: return zeros.
		return &SLAResult{}, nil
	}
	pm := raw.PerformanceMetrics
	return &SLAResult{
		QueryLatencyP50Ms: pm["query_latency_p50_ms"],
		QueryLatencyP95Ms: pm["query_latency_p95_ms"],
		QueryLatencyP99Ms: pm["query_latency_p99_ms"],
		WriteLatencyP95Ms: pm["write_latency_p95_ms"],
		CacheHitRate:      pm["cache_hit_rate"],
		ErrorRate:         pm["error_rate"],
	}, nil
}

func (c *RemoteClient) ScrapePrometheus(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.metricsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ---------- analytics ----------

func (c *RemoteClient) AnalyticsQuery(ctx context.Context, sql string) (*AnalyticsQueryResult, error) {
	result, err := c.QuerySQL(ctx, sql)
	if err != nil {
		return nil, err
	}
	return &AnalyticsQueryResult{
		Columns:    result.Columns,
		Rows:       result.Rows,
		Total:      result.Total,
		DurationMs: result.DurationMs,
	}, nil
}

func (c *RemoteClient) GetAnalyticsOverview(_ context.Context) (*AnalyticsOverviewResult, error) {
	return &AnalyticsOverviewResult{
		WriteTrend: []TimeSeriesPoint{},
		QueryTrend: []TimeSeriesPoint{},
		DataVolume: []TimeSeriesPoint{},
	}, nil
}

// GetFullConfig reads the current configuration from c.cfg, which was loaded
// from the mounted config.yaml (and any env-var overrides) at startup.
// This avoids the need for a remote API call – the dashboard container already
// has the full config in memory.
func (c *RemoteClient) GetFullConfig(_ context.Context) (*FullConfig, error) {
	redis := c.cfg.Network.Pools.Redis
	minio := c.cfg.Network.Pools.MinIO

	mode := "standalone"
	if redis.Enabled {
		mode = "distributed"
	}

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

		Mode:    mode,
		Version: version.Get(),
	}
	cfg.EnvOverrides = detectEnvOverrides()
	return cfg, nil
}

// UpdateConfig applies partial changes to c.cfg in memory and returns a YAML
// snippet for the operator to persist.  Remote nodes are not modified – the
// operator must update config.yaml and restart the relevant service.
func (c *RemoteClient) UpdateConfig(_ context.Context, req *ConfigUpdateRequest) (*ConfigUpdateResult, error) {
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
	}
	if req.RedisAddr != "" {
		c.cfg.Network.Pools.Redis.Addr = req.RedisAddr
	}
	if req.RedisPassword != "" {
		c.cfg.Network.Pools.Redis.Password = req.RedisPassword
	}
	if req.RedisDB != nil {
		c.cfg.Network.Pools.Redis.DB = *req.RedisDB
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
	}
	if req.MinioAccessKeyID != "" {
		c.cfg.Network.Pools.MinIO.AccessKeyID = req.MinioAccessKeyID
	}
	if req.MinioSecretAccessKey != "" {
		c.cfg.Network.Pools.MinIO.SecretAccessKey = req.MinioSecretAccessKey
	}
	if req.MinioUseSSL != nil {
		c.cfg.Network.Pools.MinIO.UseSSL = *req.MinioUseSSL
	}
	if req.MinioRegion != "" {
		c.cfg.Network.Pools.MinIO.Region = req.MinioRegion
	}
	if req.MinioBucket != "" {
		c.cfg.Network.Pools.MinIO.Bucket = req.MinioBucket
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

	snippet := generateConfigYAML(c.cfg, req)

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

func (c *RemoteClient) EnableDistributedMode(_ context.Context, req *EnableDistributedRequest) (*EnableDistributedResult, error) {
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
	default:
		sb.WriteString(fmt.Sprintf("      addr: \"%s\"\n", req.RedisAddr))
	}

	if req.RedisPassword != "" {
		sb.WriteString(fmt.Sprintf("      password: \"%s\"\n", req.RedisPassword))
	}
	if req.RedisDB > 0 {
		sb.WriteString(fmt.Sprintf("      db: %d\n", req.RedisDB))
	}
	sb.WriteString("      enabled: true\n")

	return &EnableDistributedResult{
		ConfigSnippet:   strings.TrimRight(sb.String(), "\n"),
		RestartRequired: true,
		Message:         "将以上配置片段写入 config.yaml 的 network.pools.redis 节点后重启服务，即可开启分布式模式。",
	}, nil
}

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
