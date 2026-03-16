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
