import { apiClient } from './client'
import { LogFileInfo, LogQueryParams, LogQueryResult } from './types'

export const logsApi = {
  queryLogs: (params: LogQueryParams) => {
    const query = new URLSearchParams()
    if (params.level) query.set('level', params.level)
    if (params.start_time !== undefined) query.set('start_time', String(params.start_time))
    if (params.end_time !== undefined) query.set('end_time', String(params.end_time))
    if (params.keyword) query.set('keyword', params.keyword)
    if (params.page !== undefined) query.set('page', String(params.page))
    if (params.page_size !== undefined) query.set('page_size', String(params.page_size))
    const queryString = query.toString()
    return apiClient.get<LogQueryResult>(`/logs${queryString ? `?${queryString}` : ''}`)
  },
  
  streamLogs: () => '/dashboard/api/v1/logs/stream',
  
  listLogFiles: () => apiClient.get<LogFileInfo[]>('/logs/files'),
}
