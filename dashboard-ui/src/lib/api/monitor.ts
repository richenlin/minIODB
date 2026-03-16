import { apiClient } from './client'
import { MonitorOverviewResult, SLAResult } from './types'

export const monitorApi = {
  getOverview: () => apiClient.get<MonitorOverviewResult>('/monitor/overview'),
  
  streamMetrics: () => '/dashboard/api/v1/monitor/stream',
  
  getSLA: () => apiClient.get<SLAResult>('/monitor/sla'),
}
