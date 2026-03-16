import { create } from 'zustand'
import { clusterApi, nodesApi } from '@/lib/api'
import type { ClusterInfo, NodeResult } from '@/lib/api/types'

export interface ClusterState {
  clusterInfo: ClusterInfo | null
  nodes: NodeResult[]
  loading: boolean
  error: string | null
  fetchCluster: () => Promise<void>
  fetchNodes: () => Promise<void>
  refresh: () => Promise<void>
}

export const useClusterStore = create<ClusterState>((set, get) => ({
  clusterInfo: null,
  nodes: [],
  loading: false,
  error: null,

  fetchCluster: async () => {
    try {
      const info = await clusterApi.getInfo()
      set({ clusterInfo: info })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to fetch cluster info' })
    }
  },

  fetchNodes: async () => {
    try {
      const nodes = await nodesApi.list()
      set({ nodes })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to fetch nodes' })
    }
  },

  refresh: async () => {
    set({ loading: true, error: null })
    await Promise.all([get().fetchCluster(), get().fetchNodes()])
    set({ loading: false })
  },
}))
