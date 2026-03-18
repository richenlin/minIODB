export interface HealthResult {
  status: string
  timestamp: number
  version: string
  node_id: string
  mode: string
}

export interface StatusResult {
  version: string
  node_id: string
  mode: string
  tables: number
  goroutines: number
  memory_usage: number
  uptime: number
}

export interface MetricsResult {
  timestamp: number
  http_requests: number
  query_duration: number
  buffer_size: number
  minio_ops: number
  redis_ops: number
  goroutines: number
  mem_alloc: number
  gc_pause_ns: number
}

export interface NodeResult {
  id: string
  address: string
  port: string
  status: string
  last_seen: string
  metadata: Record<string, string>
  /** True for synthetic infrastructure nodes (e.g. Redis coordinator) */
  virtual?: boolean
}

export interface TableResult {
  name: string
  row_count_est: number
  size_bytes: number
  created_at: string
}

export interface TableConfig {
  buffer_size?: number
  flush_interval?: string
  retention_days?: number
  backup_enabled?: boolean
  id_strategy?: string
  [key: string]: unknown
}

export interface TableDetailResult extends TableResult {
  config: TableConfig
  buffer_size: number
  flush_interval: number
  retention_days: number
  backup_enabled: boolean
  id_strategy: string
}

export interface ColumnStats {
  name: string
  type: string
  min: string
  max: string
  distinct: number
  null_rate: number
}

export interface TableStatsResult {
  name: string
  columns: ColumnStats[]
}

export interface BrowseParams {
  page?: number
  page_size?: number
  sort_by?: string
  sort_order?: string
  filter?: string
  id?: string
}

export interface BrowseResult {
  rows: Record<string, unknown>[]
  total: number
  page: number
  page_size: number
}

export interface WriteRecordRequest {
  id?: string
  timestamp?: number
  payload: Record<string, unknown>
}

export interface UpdateRecordRequest {
  timestamp?: number
  payload: Record<string, unknown>
}

export interface QueryResult {
  columns: string[]
  rows: Record<string, unknown>[]
  total: number
  duration_ms: number
}

export interface BackupResult {
  id: string
  type: string
  table_name: string
  status: string
  size_bytes: number
  created_at: string
  completed_at: string
  tables: string[]
}

export interface RestoreOptions {
  tables?: string[]
  target_table?: string
}

export interface MetadataStatusResult {
  last_backup: string
  tables_count: number
  backup_enabled: boolean
  backup_interval: number
}

export interface LogQueryParams {
  level?: string
  start_time?: number
  end_time?: number
  keyword?: string
  page?: number
  page_size?: number
}

export interface LogEntry {
  level: string
  timestamp: number
  message: string
  fields: Record<string, unknown>
}

export interface LogQueryResult {
  logs: LogEntry[]
  total: number
  page: number
  page_size: number
}

export interface LogFileInfo {
  name: string
  size: number
  modified_at: string
}

export interface MonitorOverviewResult {
  goroutines: number
  mem_alloc_mb: number
  gc_pause_ms: number
  uptime_hours: number
  cpu_percent: number
}

export interface SLAResult {
  query_latency_p50_ms: number
  query_latency_p95_ms: number
  query_latency_p99_ms: number
  write_latency_p95_ms: number
  cache_hit_rate: number
  file_prune_rate: number
  error_rate: number
}

export interface AnalyticsQueryResult {
  columns: string[]
  rows: Record<string, unknown>[]
  total: number
  duration_ms: number
  explain_plan: string
}

export interface TimeSeriesPoint {
  timestamp: number
  value: number
}

export interface AnalyticsOverviewResult {
  write_trend: TimeSeriesPoint[]
  query_trend: TimeSeriesPoint[]
  data_volume: TimeSeriesPoint[]
}

export interface CreateTableRequest {
  table_name: string
  config: TableConfig
}

export interface UpdateTableRequest {
  config: TableConfig
}

export interface UserInfo {
  key: string
  role: string
  display_name: string
}

export interface LoginResponse {
  token: string
  expires_at: string
  user: UserInfo
}

export interface ClusterTopology {
  nodes: NodeResult[]
  edges: { source: string; target: string; type: string }[]
  redis_addr?: string
  redis_mode?: string
}

/** Structured, editable configuration returned by GET /cluster/config/full */
export interface FullConfig {
  // server
  node_id: string
  grpc_port: string
  rest_port: string
  // redis
  redis_mode: string
  redis_addr: string
  redis_password: string
  redis_db: number
  redis_master_name?: string
  sentinel_addrs?: string[]
  cluster_addrs?: string[]
  // minio
  minio_endpoint: string
  minio_access_key_id: string
  minio_secret_access_key: string
  minio_use_ssl: boolean
  minio_region: string
  minio_bucket: string
  // minio_backup (network.pools.minio_backup)
  minio_backup_endpoint: string
  minio_backup_access_key_id: string
  minio_backup_secret_access_key: string
  minio_backup_use_ssl: boolean
  minio_backup_region: string
  minio_backup_bucket: string
  // log
  log_level: string
  log_format: string
  log_output: string
  log_filename: string
  // buffer
  buffer_size: number
  flush_interval: string
  // read-only
  mode: string
  version: string
  /**
   * Maps config field keys to the environment variable name that is currently
   * overriding them.  These values are correct for the running process but will
   * be re-applied on every restart regardless of config.yaml.
   * Example: { "redis_addr": "REDIS_HOST / REDIS_PORT" }
   */
  env_overrides?: Record<string, string>
}

export interface ConfigUpdateRequest {
  node_id?: string
  grpc_port?: string
  rest_port?: string
  redis_mode?: string
  redis_addr?: string
  redis_password?: string
  redis_db?: number
  redis_master_name?: string
  sentinel_addrs?: string[]
  cluster_addrs?: string[]
  minio_endpoint?: string
  minio_access_key_id?: string
  minio_secret_access_key?: string
  minio_use_ssl?: boolean
  minio_region?: string
  minio_bucket?: string
  minio_backup_endpoint?: string
  minio_backup_access_key_id?: string
  minio_backup_secret_access_key?: string
  minio_backup_use_ssl?: boolean
  minio_backup_region?: string
  minio_backup_bucket?: string
  log_level?: string
  log_format?: string
  log_output?: string
  log_filename?: string
  buffer_size?: number
  flush_interval?: string
}

export interface ConfigUpdateResult {
  config_snippet: string
  restart_required: boolean
  message: string
}

export interface EnableDistributedRequest {
  redis_mode: 'standalone' | 'sentinel' | 'cluster'
  redis_addr?: string
  redis_password?: string
  redis_db?: number
  master_name?: string
  sentinel_addrs?: string[]
  sentinel_password?: string
  cluster_addrs?: string[]
}

export interface EnableDistributedResult {
  config_snippet: string
  restart_required: boolean
  message: string
}

export interface ClusterInfo {
  mode: string
  node_id: string
  nodes_count: number
  tables_count: number
  uptime: number
  version: string
  total_records: number
  pending_writes: number
  stats_age_s: number   // seconds since last background stats refresh
}
