import { apiClient } from './client'
import { AnalyticsOverviewResult, AnalyticsQueryResult } from './types'

export interface AnalyticsQueryRequest {
  sql: string
  explain?: boolean
}

export const analyticsApi = {
  query: (req: AnalyticsQueryRequest) =>
    apiClient.post<AnalyticsQueryResult>('/analytics/query', req),
  
  getOverview: () => apiClient.get<AnalyticsOverviewResult>('/analytics/overview'),
}
