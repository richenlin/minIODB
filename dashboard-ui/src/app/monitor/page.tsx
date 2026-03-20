'use client'

import { useEffect, useState } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { RealtimeChart } from '@/components/charts/realtime-chart'
import { monitorApi } from '@/lib/api/monitor'
import { SLAResult } from '@/lib/api/types'
import { useSSE } from '@/hooks/use-sse'

// SSE "metrics" 事件的数据结构（与后端 startMetricsPush 对齐）
interface MetricsPayload {
  goroutines: number
  mem_alloc_mb: number
  gc_pause_ms: number
  cpu_percent: number
  uptime_hours: number
  load_level: string
}

export default function MonitorPage() {
  const [metrics, setMetrics] = useState<MetricsPayload | null>(null)
  const [sla, setSLA] = useState<SLAResult | null>(null)
  const [slaError, setSlaError] = useState('')

  // 单条 SSE 连接，卡片和图表共用
  const { data, connected } = useSSE<{ data?: MetricsPayload }>('/monitor/stream')

  // 每次 SSE 推送新数据时更新卡片
  useEffect(() => {
    if (data?.data) {
      setMetrics(data.data)
    }
  }, [data])

  // SLA 数据只需一次性加载（变化慢，不需要实时刷新）
  useEffect(() => {
    monitorApi.getSLA()
      .then(setSLA)
      .catch(err => setSlaError(err instanceof Error ? err.message : '加载失败'))
  }, [])

  const loading = metrics === null

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">实时性能指标</p>
          <Badge variant={connected ? 'success' : 'secondary'}>
            {connected ? 'SSE 已连接' : 'SSE 断开'}
          </Badge>
        </div>

        {loading ? (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="h-24 rounded-lg border border-border bg-card animate-pulse" />
            ))}
          </div>
        ) : (
          <>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <MetricCard
                title="Goroutines"
                value={String(metrics.goroutines)}
                description="当前协程数"
              />
              <MetricCard
                title="内存分配"
                value={`${metrics.mem_alloc_mb.toFixed(1)} MB`}
                description="已分配内存"
              />
              <MetricCard
                title="GC 暂停"
                value={`${metrics.gc_pause_ms.toFixed(2)} ms`}
                description="最近 GC 暂停"
              />
              <MetricCard
                title="CPU 使用率"
                value={`${metrics.cpu_percent.toFixed(1)}%`}
                description="CPU 占用"
              />
            </div>

            {/* 实时折线图 */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <Card>
                <CardContent className="pt-6">
                  <RealtimeChart title="Goroutines" valueKey="goroutines" color="#6366f1" height={200} />
                </CardContent>
              </Card>
              <Card>
                <CardContent className="pt-6">
                  <RealtimeChart title="内存分配 (MB)" valueKey="mem_alloc_mb" unit=" MB" color="#22c55e" height={200} />
                </CardContent>
              </Card>
              <Card>
                <CardContent className="pt-6">
                  <RealtimeChart title="GC 暂停 (ms)" valueKey="gc_pause_ms" unit=" ms" color="#f59e0b" height={200} />
                </CardContent>
              </Card>
              <Card>
                <CardContent className="pt-6">
                  <RealtimeChart title="CPU 使用率 (%)" valueKey="cpu_percent" unit="%" color="#ef4444" height={200} />
                </CardContent>
              </Card>
            </div>

            {/* SLA 指标 */}
            {slaError && (
              <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
                SLA 数据加载失败：{slaError}
              </div>
            )}
            {sla && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm font-semibold">SLA 指标</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-4">
                    <SLAMetric label="P50 延迟" value={`${sla.query_latency_p50_ms.toFixed(1)} ms`} />
                    <SLAMetric label="P95 延迟" value={`${sla.query_latency_p95_ms.toFixed(1)} ms`} />
                    <SLAMetric label="P99 延迟" value={`${sla.query_latency_p99_ms.toFixed(1)} ms`} />
                    <SLAMetric label="缓存命中率" value={`${(sla.cache_hit_rate * 100).toFixed(1)}%`} />
                    <SLAMetric label="错误率" value={`${(sla.error_rate * 100).toFixed(2)}%`} highlight={sla.error_rate > 0.01} />
                    <SLAMetric label="写入 P95" value={`${sla.write_latency_p95_ms.toFixed(1)} ms`} />
                  </div>
                </CardContent>
              </Card>
            )}

            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-semibold">Prometheus 指标</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">
                  原始 Prometheus 指标可通过{' '}
                  <a href="/metrics" target="_blank" rel="noopener noreferrer"
                    className="text-primary underline underline-offset-2">/metrics</a>{' '}
                  端点访问，兼容 Prometheus / Grafana 采集。
                </p>
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </DashboardLayout>
  )
}

function MetricCard({ title, value, description }: { title: string; value: string; description?: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="pt-0">
        <p className="text-2xl font-bold text-foreground">{value}</p>
        {description && <p className="text-xs text-muted-foreground mt-1">{description}</p>}
      </CardContent>
    </Card>
  )
}

function SLAMetric({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="text-center">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`text-sm font-semibold mt-1 ${highlight ? 'text-destructive' : 'text-foreground'}`}>
        {value}
      </p>
    </div>
  )
}
