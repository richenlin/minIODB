import { apiClient } from './client'
import { NodeResult } from './types'

export const nodesApi = {
  list: () => apiClient.get<NodeResult[]>('/nodes'),
  
  get: (id: string) => apiClient.get<NodeResult>(`/nodes/${encodeURIComponent(id)}`),
}
