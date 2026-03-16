'use client'

import { useEffect, useState } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { apiClient } from '@/lib/api/client'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { MiniLine } from '@/components/charts/mini-line'
import { clusterApi } from '@/lib/api/cluster'
import { HealthResult, ClusterInfo } from '@/lib/api/types'

interface TablesData {
  tables: unknown[]
  total: number
}

export default function OverviewPage() {
  const [health, setHealth] = useState<HealthResult | null>(null)
  const [clusterInfo, setClusterInfo] = useState<ClusterInfo | null>(null)
  const [tables, setTables] = useState<TablesData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Mock trend data for mini charts
  const [writeTrend, setWriteTrend] = useState<number[]>(Array(12).fill(0).map(() => Math.random() * 100))
  const [queryTrend, setQueryTrend] = useState<number[]>(Array(12).fill(0).map(() => Math.random() * 50))

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [healthData, clusterData, tablesData] = await Promise.all([
          clusterApi.getHealth(),
          clusterApi.getInfo(),
          apiClient.get<TablesData>('/tables'),
        ])
        setHealth(healthData)
        setClusterInfo(clusterData)
        setTables(tablesData)
      } catch (err) {
        setError(err instanceof Error ? err.message : '数据加载失败')
      } finally {
        setLoading(false)
      }
    }
    fetchData()

    // Simulate trend updates
    const interval = setInterval(() => {
      setWriteTrend(prev => [...prev.slice(1), Math.random() * 100])
      setQueryTrend(prev => [...prev.slice(1), Math.random() * 50])
    }, 2000)

    return () => clearInterval(interval)
  }, [])

  const isHealthy = health?.status === 'healthy'

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-foreground">总览</h1>
            <p className="text-sm text-muted-foreground mt-1">MinIODB 集群整体状态</p>
          </div>
          {!loading && (
            <Badge variant={isHealthy ? 'success' : 'error'}>
              {isHealthy ? '集群健康' : '集群异常'}
            </Badge>
          )}
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {loading ? (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="h-32 rounded-lg border border-border bg-card animate-pulse" />
            ))}
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <StatCard
              title="节点数"
              value={String(clusterInfo?.nodes_count ?? 0)}
              description="活跃节点"
            />
            <StatCard
              title="表数量"
              value={String(tables?.total ?? 0)}
              description="数据表总数"
            />
            <StatCard
              title="总记录数"
              value={formatCount((clusterInfo?.total_records ?? 0) + (clusterInfo?.pending_writes ?? 0))}
              description={
                clusterInfo
                  ? `落盘 ${formatCount(clusterInfo.total_records)} · buffer ${formatCount(clusterInfo.pending_writes)} · 统计${clusterInfo.stats_age_s >= 0 ? formatAge(clusterInfo.stats_age_s) + '前' : '加载中'}`
                  : '统计加载中…'
              }
            />
            <StatCard
              title="运行时间"
              value={formatUptime(clusterInfo?.uptime ?? 0)}
              description="服务运行时长"
            />
          </div>
        )}

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">写入速率趋势</CardTitle>
            </CardHeader>
            <CardContent className="pt-0">
              <MiniLine data={writeTrend} color="#22c55e" height={80} />
              <p className="text-xs text-muted-foreground mt-2">操作/秒 (模拟数据)</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">查询速率趋势</CardTitle>
            </CardHeader>
            <CardContent className="pt-0">
              <MiniLine data={queryTrend} color="#6366f1" height={80} />
              <p className="text-xs text-muted-foreground mt-2">查询/秒 (模拟数据)</p>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-semibold">系统信息</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2 text-sm">
              <InfoRow label="集群模式" value={clusterInfo?.mode ?? '-'} />
              <InfoRow label="节点 ID" value={clusterInfo?.node_id ?? '-'} mono />
              <InfoRow label="版本" value={health?.version ?? '-'} />
              <InfoRow label="最后心跳" value={health?.timestamp ? new Date(health.timestamp * 1000).toLocaleString('zh-CN') : '-'} />
            </div>
          </CardContent>
        </Card>
      </div>
    </DashboardLayout>
  )
}

function StatCard({ title, value, description }: { title: string; value: string; description?: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="pt-0">
        <p className="text-3xl font-bold text-foreground">{value}</p>
        {description && <p className="text-xs text-muted-foreground mt-1">{description}</p>}
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

function formatAge(s: number): string {
  if (s < 0) return '加载中'
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m`
  return `${Math.floor(s / 3600)}h`
}

function formatCount(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return `${days}d ${hours}h`
}
