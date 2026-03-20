import { apiClient } from './client'
import { MonitorOverviewResult, SLAResult } from './types'

const BACKEND_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081'

export const monitorApi = {
  getOverview: () => apiClient.get<MonitorOverviewResult>('/monitor/overview'),

  // Absolute URL to bypass Next.js proxy buffering of SSE streams.
  streamMetrics: () => `${BACKEND_URL}/dashboard/api/v1/monitor/stream`,

  getSLA: () => apiClient.get<SLAResult>('/monitor/sla'),
}
