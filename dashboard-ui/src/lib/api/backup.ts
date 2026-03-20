import { apiClient } from './client'
import { BackupResult, MetadataStatusResult, RestoreOptions } from './types'

export interface HotBackupSyncState {
  last_sync_time: string
  last_sync_count: number
  last_sync_size: number
  last_sync_errors?: string[]
}

export interface HotBackupProgress {
  total_objects: number
  synced_objects: number
  total_size: number
  synced_size: number
  start_time: string
  is_running: boolean
}

export interface HotBackupStatus {
  available: boolean
  state: HotBackupSyncState | null
  progress: HotBackupProgress | null
  error?: string
}

export interface HotBackupAvailability {
  available: boolean
  message: string
}

export interface DownloadResult {
  download_url: string
  expires_at: string
}

export interface VerifyResult {
  valid: boolean
  size_bytes: number
  last_modified?: string
  error?: string
}

export interface RestoreResult {
  message: string
  success: boolean
  restored_tables?: string[]
}

export interface ScheduleUpdateRequest {
  backup_enabled?: boolean
  backup_interval?: number
  metadata_enabled?: boolean
  metadata_interval?: number
}

export interface AvailabilityResult {
  available: boolean
  message: string
}

export interface BackupSchedule {
  id: string
  name: string
  enabled: boolean
  interval: number
  cron_expr: string
  backup_type: string
  tables: string[]
  retention_days: number
  created_at: string
  updated_at: string
}

export interface CreateBackupPlanRequest {
  id: string
  name: string
  enabled: boolean
  cron_expr?: string
  interval?: string
  backup_type: string
  tables?: string[]
  retention_days?: number
}

export interface UpdateBackupPlanRequest {
  name?: string
  enabled?: boolean
  cron_expr?: string
  interval?: string
  backup_type?: string
  tables?: string[]
  retention_days?: number
}

export interface ListPlansResult {
  plans: BackupSchedule[]
  total: number
}

export const backupApi = {
  checkAvailability: () =>
    apiClient.get<AvailabilityResult>('/backups/availability'),

  getHotBackupStatus: () =>
    apiClient.get<HotBackupStatus>('/hot-backup/status'),

  getHotBackupAvailability: () =>
    apiClient.get<HotBackupAvailability>('/hot-backup/availability'),

  // Backend returns { backups: BackupResult[] }
  list: () =>
    apiClient.get<{ backups: BackupResult[] }>('/backups').then(r => r.backups ?? []),
  
  triggerMetadataBackup: () =>
    apiClient.post<BackupResult>('/backups/metadata', {}),
  
  getSchedule: () => apiClient.get<MetadataStatusResult>('/backups/schedule'),
  
  updateSchedule: (data: ScheduleUpdateRequest) =>
    apiClient.put<MetadataStatusResult>('/backups/schedule', data),
  
  triggerFullBackup: () =>
    apiClient.post<BackupResult>('/backups/full', {}),
  
  triggerTableBackup: (tableName: string) =>
    apiClient.post<BackupResult>(`/backups/table/${encodeURIComponent(tableName)}`, {}),
  
  get: (id: string) =>
    apiClient.get<BackupResult>(`/backups/${encodeURIComponent(id)}`),
  
  restore: (id: string, options?: RestoreOptions) =>
    apiClient.post<RestoreResult>(`/backups/${encodeURIComponent(id)}/restore`, options || {}),
  
  delete: (id: string) =>
    apiClient.delete<void>(`/backups/${encodeURIComponent(id)}`),
  
  download: (id: string) =>
    apiClient.get<DownloadResult>(`/backups/${encodeURIComponent(id)}/download`),
  
  verify: (id: string) =>
    apiClient.post<VerifyResult>(`/backups/${encodeURIComponent(id)}/verify`, {}),
  
  listPlans: () =>
    apiClient.get<ListPlansResult>('/backups/plans').then(r => r ?? { plans: [], total: 0 }),
  
  createPlan: (data: CreateBackupPlanRequest) =>
    apiClient.post<BackupSchedule>('/backups/plans', data),
  
  updatePlan: (id: string, data: UpdateBackupPlanRequest) =>
    apiClient.put<BackupSchedule>(`/backups/plans/${encodeURIComponent(id)}`, data),
  
  deletePlan: (id: string) =>
    apiClient.delete<void>(`/backups/plans/${encodeURIComponent(id)}`),
  
  triggerPlan: (id: string) =>
    apiClient.post<void>(`/backups/plans/${encodeURIComponent(id)}/trigger`, {}),
}
