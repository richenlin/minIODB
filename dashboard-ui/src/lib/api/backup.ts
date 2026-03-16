import { apiClient } from './client'
import { BackupResult, MetadataStatusResult, RestoreOptions } from './types'

export const backupApi = {
  list: () => apiClient.get<BackupResult[]>('/backups'),
  
  triggerMetadataBackup: () =>
    apiClient.post<BackupResult>('/backups/metadata', {}),
  
  getSchedule: () => apiClient.get<MetadataStatusResult>('/backups/schedule'),
  
  triggerFullBackup: () =>
    apiClient.post<BackupResult>('/backups/full', {}),
  
  triggerTableBackup: (tableName: string) =>
    apiClient.post<BackupResult>(`/backups/table/${encodeURIComponent(tableName)}`, {}),
  
  get: (id: string) =>
    apiClient.get<BackupResult>(`/backups/${encodeURIComponent(id)}`),
  
  restore: (id: string, options?: RestoreOptions) =>
    apiClient.post<void>(`/backups/${encodeURIComponent(id)}/restore`, options || {}),
  
  delete: (id: string) =>
    apiClient.delete<void>(`/backups/${encodeURIComponent(id)}`),
}
