import { apiClient } from './client'
import {
  ClusterInfo,
  ClusterTopology,
  ConfigUpdateRequest,
  ConfigUpdateResult,
  EnableDistributedRequest,
  EnableDistributedResult,
  FullConfig,
  HealthResult,
} from './types'

export const clusterApi = {
  getHealth: () => apiClient.get<HealthResult>('/health'),

  getInfo: () => apiClient.get<ClusterInfo>('/cluster/info'),

  getTopology: () => apiClient.get<ClusterTopology>('/cluster/topology'),

  getConfig: () => apiClient.get<Record<string, unknown>>('/cluster/config'),

  getFullConfig: () => apiClient.get<FullConfig>('/cluster/config/full'),

  updateConfig: (req: ConfigUpdateRequest) =>
    apiClient.put<ConfigUpdateResult>('/cluster/config', req),

  enableDistributed: (req: EnableDistributedRequest) =>
    apiClient.post<EnableDistributedResult>('/cluster/enable-distributed', req),
}
