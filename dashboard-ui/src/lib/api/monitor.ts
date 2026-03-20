import { apiClient } from './client'
import { SLAResult } from './types'

const BACKEND_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081'

export const monitorApi = {
  // Absolute URL to bypass Next.js proxy buffering of SSE streams.
  streamMetrics: () => `${BACKEND_URL}/dashboard/api/v1/monitor/stream`,

  getSLA: () => apiClient.get<SLAResult>('/monitor/sla'),
}
