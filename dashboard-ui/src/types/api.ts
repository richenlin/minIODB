// API Response Types

export interface APIResponse<T = unknown> {
  code: number
  message?: string
  data?: T
}

// Cluster Info
export interface HealthStatus {
  status: string
  timestamp: number
  version: string
  node_id: string
  mode: 'single' | 'distributed'
}

// System Status (Monitor Overview)
export interface SystemStatus {
  version: string
  node_id: string
  uptime: number
  mode: string
  redis_enabled: boolean
  tables: number
  goroutines: number
  memory_usage: number
  extra?: Record<string, unknown>
}

// Node Info
export interface NodeInfo {
  id: string
  address: string
  port: string
  status: string
  last_seen: string
  metadata?: Record<string, string>
}

// Metrics Snapshot
export interface MetricsSnapshot {
  timestamp: number
  http_requests: Record<string, number>
  grpc_requests: Record<string, number>
  query_duration: Record<string, number>
  buffer_size: number
  minio_ops: Record<string, number>
  redis_ops: Record<string, number>
  goroutines: number
  mem_alloc: number
  mem_sys: number
  gc_pause_ns: number
}

// Table Info
export interface TableInfo {
  name: string
  config: Record<string, unknown>
  row_count_est?: number
  size_bytes?: number
  created_at?: number
}
