'use client'

import { useEffect, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { clusterApi } from '@/lib/api/cluster'
import { ClusterInfo, ClusterTopology, HealthResult } from '@/lib/api/types'

export default function ClusterPage() {
  const [health, setHealth] = useState<HealthResult | null>(null)
  const [clusterInfo, setClusterInfo] = useState<ClusterInfo | null>(null)
  const [topology, setTopology] = useState<ClusterTopology | null>(null)
  const [config, setConfig] = useState<Record<string, unknown> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [healthData, infoData, topologyData, configData] = await Promise.all([
          clusterApi.getHealth(),
          clusterApi.getInfo(),
          clusterApi.getTopology(),
          clusterApi.getConfig(),
        ])
        setHealth(healthData)
        setClusterInfo(infoData)
        setTopology(topologyData)
        setConfig(configData)
      } catch (err) {
        setError(err instanceof Error ? err.message : '数据加载失败')
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [])

  const isHealthy = health?.status === 'healthy'

  // Build ECharts Graph data
  const graphOption = topology ? {
    tooltip: {},
    series: [{
      type: 'graph',
      layout: 'force',
      data: topology.nodes.map((node, index) => ({
        id: node.id,
        name: node.id.substring(0, 8) + '...',
        symbolSize: 40,
        category: node.status === 'online' ? 0 : 1,
        itemStyle: {
          color: node.status === 'online' ? '#22c55e' : '#ef4444',
        },
        label: { show: true, fontSize: 10 },
      })),
      links: topology.edges.map(edge => ({
        source: edge.source,
        target: edge.target,
        lineStyle: {
          color: edge.type === 'data' ? '#6366f1' : '#94a3b8',
          width: 2,
          type: edge.type === 'data' ? 'solid' : 'dashed',
        },
      })),
      categories: [
        { name: 'Online' },
        { name: 'Offline' },
      ],
      roam: true,
      label: {
        show: true,
        position: 'bottom',
      },
      force: {
        repulsion: 200,
        edgeLength: 120,
      },
      emphasis: {
        focus: 'adjacency',
        lineStyle: { width: 4 },
      },
    }],
  } : null

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">查看集群整体状态与配置</p>
          </div>
          {!loading && (
            <Badge variant={isHealthy ? 'success' : 'error'}>
              {isHealthy ? '健康' : '异常'}
            </Badge>
          )}
        </div>

        {error && <ErrorBanner message={error} />}

        {loading ? (
          <div className="space-y-4">
            <div className="h-32 rounded-lg border border-border bg-card animate-pulse" />
            <div className="h-80 rounded-lg border border-border bg-card animate-pulse" />
          </div>
        ) : (
          <>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <StatCard title="集群模式" value={clusterInfo?.mode ?? '-'} />
              <StatCard title="节点数量" value={String(clusterInfo?.nodes_count ?? 0)} />
              <StatCard title="表数量" value={String(clusterInfo?.tables_count ?? 0)} />
              <StatCard title="版本" value={clusterInfo?.version ?? '-'} />
            </div>

            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-semibold">节点拓扑</CardTitle>
              </CardHeader>
              <CardContent>
                {topology && topology.nodes.length > 0 ? (
                  <ReactECharts option={graphOption!} style={{ height: 400 }} />
                ) : (
                  <div className="h-80 flex items-center justify-center text-muted-foreground">
                    暂无拓扑数据（单节点模式）
                  </div>
                )}
                <div className="flex gap-4 mt-4 text-xs text-muted-foreground">
                  <span className="flex items-center gap-2">
                    <span className="w-3 h-3 rounded-full bg-green-500" />
                    Online
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="w-3 h-3 rounded-full bg-red-500" />
                    Offline
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="w-8 h-0.5 bg-indigo-500" />
                    数据同步
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="w-8 h-0.5 bg-slate-400 border-dashed border-t border-slate-400" />
                    心跳
                  </span>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-semibold">集群配置</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-2 text-sm">
                  <InfoRow label="节点 ID" value={clusterInfo?.node_id ?? '-'} mono />
                  <InfoRow label="运行时间" value={formatUptime(clusterInfo?.uptime ?? 0)} />
                  {config && Object.entries(config).map(([key, value]) => (
                    <InfoRow key={key} label={key} value={String(value)} />
                  ))}
                </div>
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </DashboardLayout>
  )
}

function StatCard({ title, value }: { title: string; value: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="pt-0">
        <p className="text-2xl font-bold text-foreground">{value}</p>
      </CardContent>
    </Card>
  )
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-border last:border-0">
      <span className="text-muted-foreground">{label}</span>
      <span className={`font-medium ${mono ? 'font-mono text-xs' : ''}`}>{value}</span>
    </div>
  )
}

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
      {message}
    </div>
  )
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return `${days}d ${hours}h`
}
