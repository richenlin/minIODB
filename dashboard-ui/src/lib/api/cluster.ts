import { apiClient } from './client'
import { ClusterInfo, ClusterTopology, HealthResult } from './types'

export const clusterApi = {
  getHealth: () => apiClient.get<HealthResult>('/health'),
  
  getInfo: () => apiClient.get<ClusterInfo>('/cluster/info'),
  
  getTopology: () => apiClient.get<ClusterTopology>('/cluster/topology'),
  
  getConfig: () => apiClient.get<Record<string, unknown>>('/cluster/config'),
}
