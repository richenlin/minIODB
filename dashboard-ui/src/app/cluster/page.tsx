'use client'

import { useEffect, useState, useCallback } from 'react'
import ReactECharts from 'echarts-for-react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { clusterApi } from '@/lib/api/cluster'
import {
  ClusterInfo,
  ClusterTopology,
  EnableDistributedRequest,
  EnableDistributedResult,
  HealthResult,
  NodeResult,
} from '@/lib/api/types'

// ─────────────────────────────── helpers ────────────────────────────────────

function shortId(id: string) {
  return id.length > 12 ? id.substring(0, 12) + '…' : id
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  const days = Math.floor(seconds / 86400)
  return `${days}d ${Math.floor((seconds % 86400) / 3600)}h`
}

/** Detect deployment platform hint from node address patterns */
function detectDeploymentHint(nodes: NodeResult[]): string | null {
  if (nodes.length <= 1) return null
  const addrs = nodes.filter(n => !n.virtual).map(n => n.address)
  if (addrs.some(a => /\.svc\.cluster\.local/.test(a) || /^10\.\d+\.\d+\.\d+$/.test(a)))
    return 'Kubernetes'
  if (addrs.some(a => /tasks\.|_tasks\./.test(a)))
    return 'Docker Swarm'
  return 'Ansible / VM'
}

// ─────────────────────────────── topology chart ──────────────────────────────

function buildGraphOption(topology: ClusterTopology) {
  const realNodes = topology.nodes.filter(n => !n.virtual)
  const redisNode = topology.nodes.find(n => n.virtual && n.metadata?.type === 'redis')

  const data = topology.nodes.map(node => {
    const isRedis = !!node.virtual
    const isOnline = node.status === 'healthy' || node.status === 'online' || node.status === 'running'
    const label = isRedis
      ? `Redis\n${node.address}`
      : `${shortId(node.id)}\n${node.address}${node.port ? ':' + node.port : ''}`
    return {
      id: node.id,
      name: label,
      symbolSize: isRedis ? 52 : 44,
      symbol: isRedis ? 'diamond' : 'circle',
      itemStyle: {
        color: isRedis
          ? '#a855f7'
          : isOnline
          ? '#22c55e'
          : '#ef4444',
        borderColor: isRedis ? '#7c3aed' : isOnline ? '#16a34a' : '#b91c1c',
        borderWidth: 2,
      },
      label: {
        show: true,
        fontSize: 10,
        color: '#e2e8f0',
        position: 'bottom',
        formatter: label,
      },
      tooltip: {
        formatter: isRedis
          ? `<b>Redis 协调器</b><br/>地址: ${node.address}<br/>模式: ${node.metadata?.mode ?? '-'}`
          : `<b>节点</b><br/>ID: ${node.id}<br/>地址: ${node.address}${node.port ? ':' + node.port : ''}<br/>状态: ${node.status}`,
      },
    }
  })

  const links = topology.edges.map(edge => ({
    source: edge.source,
    target: edge.target,
    lineStyle: {
      color: edge.type === 'coordination' ? '#a855f7' : '#6366f1',
      width: 1.5,
      type: edge.type === 'coordination' ? 'dashed' : 'solid',
      opacity: 0.7,
    },
  }))

  // If no Redis virtual node but multiple real nodes, add peer links
  if (!redisNode && realNodes.length > 1) {
    for (let i = 0; i < realNodes.length; i++) {
      for (let j = i + 1; j < realNodes.length; j++) {
        links.push({
          source: realNodes[i].id,
          target: realNodes[j].id,
          lineStyle: { color: '#6366f1', width: 1.5, type: 'solid', opacity: 0.5 },
        })
      }
    }
  }

  return {
    backgroundColor: 'transparent',
    tooltip: { trigger: 'item' },
    series: [{
      type: 'graph',
      layout: 'force',
      data,
      links,
      roam: true,
      label: { show: true, position: 'bottom' },
      force: {
        repulsion: realNodes.length > 4 ? 300 : 220,
        edgeLength: realNodes.length > 4 ? 160 : 130,
        gravity: 0.08,
      },
      emphasis: {
        focus: 'adjacency',
        lineStyle: { width: 3 },
      },
    }],
  }
}

// ─────────────────────────────── standalone card ─────────────────────────────

function StandaloneNode({ node, onEnable }: { node: NodeResult; onEnable: () => void }) {
  return (
    <div className="flex flex-col items-center gap-4 py-10">
      <div className="relative">
        <div className="w-24 h-24 rounded-full border-4 border-green-500/60 bg-green-500/10 flex items-center justify-center">
          <div className="w-14 h-14 rounded-full bg-green-500/20 border-2 border-green-500 flex items-center justify-center">
            <svg className="w-7 h-7 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M5 12h14M12 5l7 7-7 7" />
            </svg>
          </div>
        </div>
        <span className="absolute -top-1 -right-1 w-4 h-4 rounded-full bg-green-500 border-2 border-background" />
      </div>
      <div className="text-center space-y-1">
        <p className="text-sm font-mono text-muted-foreground">{node.id}</p>
        <p className="text-xs text-muted-foreground">{node.address}{node.port ? ':' + node.port : ''}</p>
        <Badge variant="secondary">单节点模式</Badge>
      </div>
    </div>
  )
}

// ─────────────────────────────── enable distributed dialog ───────────────────

/**
 * RedisDeployType 是 Redis 服务自身的部署方式，与 MinIODB 集群模式无关：
 *   - single   单实例 Redis（最常见，对应 config 中的 standalone/addr）
 *   - sentinel 哨兵高可用
 *   - cluster  Redis Cluster 分片集群
 */
type RedisDeployType = 'single' | 'sentinel' | 'cluster'

const REDIS_DEPLOY_OPTIONS: { value: RedisDeployType; label: string; desc: string }[] = [
  { value: 'single',   label: '单实例',          desc: '独立 Redis 节点，最简单的部署方式' },
  { value: 'sentinel', label: 'Sentinel 高可用',  desc: '多哨兵节点，主节点故障自动切换' },
  { value: 'cluster',  label: 'Redis Cluster',   desc: '官方分片集群，数据自动分布' },
]

interface EnableDialogProps {
  open: boolean
  onClose: () => void
}

function EnableDistributedDialog({ open, onClose }: EnableDialogProps) {
  const [deployType, setDeployType] = useState<RedisDeployType>('single')
  const [addr, setAddr] = useState('')
  const [password, setPassword] = useState('')
  const [db, setDb] = useState('0')
  const [masterName, setMasterName] = useState('mymaster')
  const [sentinelAddrs, setSentinelAddrs] = useState('')
  const [sentinelPassword, setSentinelPassword] = useState('')
  const [clusterAddrs, setClusterAddrs] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<EnableDistributedResult | null>(null)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)

  const reset = () => {
    setResult(null)
    setError('')
    setCopied(false)
  }

  const handleClose = () => {
    reset()
    onClose()
  }

  // deployType → redis_mode: single maps to "standalone" in config
  const toRedisMode = (t: RedisDeployType) => t === 'single' ? 'standalone' : t

  const handleSubmit = async () => {
    setLoading(true)
    setError('')
    try {
      const req: EnableDistributedRequest = {
        redis_mode: toRedisMode(deployType),
        redis_password: password || undefined,
      }
      if (deployType === 'single') {
        req.redis_addr = addr || 'localhost:6379'
        const dbNum = parseInt(db, 10)
        if (!isNaN(dbNum) && dbNum > 0) req.redis_db = dbNum
      } else if (deployType === 'sentinel') {
        req.master_name = masterName
        req.sentinel_addrs = sentinelAddrs.split('\n').map(s => s.trim()).filter(Boolean)
        req.sentinel_password = sentinelPassword || undefined
      } else {
        req.cluster_addrs = clusterAddrs.split('\n').map(s => s.trim()).filter(Boolean)
      }
      const res = await clusterApi.enableDistributed(req)
      setResult(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : '请求失败')
    } finally {
      setLoading(false)
    }
  }

  const handleCopy = () => {
    if (!result) return
    navigator.clipboard.writeText(result.config_snippet)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const inputCls = 'w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring'

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) handleClose() }}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>开启分布式模式</DialogTitle>
          <DialogDescription>
            MinIODB 通过 Redis 实现服务发现，配置 Redis 后即可开启多节点分布式集群。
            请填写 Redis 的连接信息，系统生成 config.yaml 片段，应用并重启后生效。
          </DialogDescription>
        </DialogHeader>

        {!result ? (
          <div className="space-y-4 py-2">
            {/* Redis deploy type selector */}
            <div>
              <label className="text-xs font-medium text-muted-foreground mb-1.5 block">
                Redis 部署方式
                <span className="ml-1.5 text-xs font-normal text-muted-foreground/70">（Redis 服务自身的部署类型，与集群模式无关）</span>
              </label>
              <div className="flex flex-col gap-2">
                {REDIS_DEPLOY_OPTIONS.map(opt => (
                  <button key={opt.value} onClick={() => { setDeployType(opt.value); reset() }}
                    className={`flex items-center gap-3 rounded-md border px-3 py-2 text-left text-xs transition-colors ${
                      deployType === opt.value
                        ? 'border-primary bg-primary/10'
                        : 'border-border hover:border-border/80 hover:bg-muted/40'
                    }`}>
                    <span className={`w-3.5 h-3.5 rounded-full border-2 flex-shrink-0 ${
                      deployType === opt.value ? 'border-primary bg-primary' : 'border-muted-foreground'
                    }`} />
                    <div>
                      <span className={`font-medium ${deployType === opt.value ? 'text-primary' : 'text-foreground'}`}>
                        {opt.label}
                      </span>
                      <span className="ml-2 text-muted-foreground">{opt.desc}</span>
                    </div>
                  </button>
                ))}
              </div>
            </div>

            {/* single instance fields */}
            {deployType === 'single' && (
              <>
                <Field label="Redis 地址">
                  <input className={inputCls} placeholder="localhost:6379" value={addr}
                    onChange={e => setAddr(e.target.value)} />
                </Field>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="密码（可选）">
                    <input className={inputCls} type="password" placeholder="无密码留空"
                      value={password} onChange={e => setPassword(e.target.value)} />
                  </Field>
                  <Field label="DB 编号">
                    <input className={inputCls} placeholder="0" value={db}
                      onChange={e => setDb(e.target.value)} />
                  </Field>
                </div>
              </>
            )}

            {/* sentinel fields */}
            {deployType === 'sentinel' && (
              <>
                <Field label="Master 名称">
                  <input className={inputCls} placeholder="mymaster" value={masterName}
                    onChange={e => setMasterName(e.target.value)} />
                </Field>
                <Field label="Sentinel 节点地址（每行一个）">
                  <textarea className={`${inputCls} min-h-[80px] resize-y font-mono`}
                    placeholder={'sentinel-1:26379\nsentinel-2:26379\nsentinel-3:26379'}
                    value={sentinelAddrs} onChange={e => setSentinelAddrs(e.target.value)} />
                </Field>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="Sentinel 密码（可选）">
                    <input className={inputCls} type="password" placeholder="无密码留空"
                      value={sentinelPassword} onChange={e => setSentinelPassword(e.target.value)} />
                  </Field>
                  <Field label="Redis 实例密码（可选）">
                    <input className={inputCls} type="password" placeholder="无密码留空"
                      value={password} onChange={e => setPassword(e.target.value)} />
                  </Field>
                </div>
              </>
            )}

            {/* redis cluster fields */}
            {deployType === 'cluster' && (
              <>
                <Field label="Redis Cluster 节点地址（每行一个）">
                  <textarea className={`${inputCls} min-h-[80px] resize-y font-mono`}
                    placeholder={'redis-node-1:7000\nredis-node-2:7001\nredis-node-3:7002'}
                    value={clusterAddrs} onChange={e => setClusterAddrs(e.target.value)} />
                </Field>
                <Field label="密码（可选）">
                  <input className={inputCls} type="password" placeholder="无密码留空"
                    value={password} onChange={e => setPassword(e.target.value)} />
                </Field>
              </>
            )}

            {error && (
              <p className="text-xs text-destructive bg-destructive/10 rounded px-3 py-2">{error}</p>
            )}
          </div>
        ) : (
          <div className="space-y-3 py-2">
            <div className="rounded-md bg-amber-500/10 border border-amber-500/30 px-3 py-2 text-xs text-amber-600 dark:text-amber-400 whitespace-pre-wrap">
              {result.message}
            </div>
            <div className="relative">
              <pre className="rounded-md bg-muted border border-border px-4 py-3 text-xs font-mono overflow-auto max-h-56 whitespace-pre">
                {result.config_snippet}
              </pre>
              <button onClick={handleCopy}
                className="absolute top-2 right-2 rounded px-2 py-0.5 text-xs border border-border bg-background hover:bg-accent transition-colors">
                {copied ? '已复制' : '复制'}
              </button>
            </div>
            <p className="text-xs text-muted-foreground">
              将上方配置片段替换 <code className="bg-muted px-1 rounded">config.yaml</code> 中的{' '}
              <code className="bg-muted px-1 rounded">network.pools.redis</code> 节，然后重启各节点即可开启分布式模式。
            </p>
          </div>
        )}

        <DialogFooter>
          {!result ? (
            <>
              <Button variant="outline" onClick={handleClose} disabled={loading}>取消</Button>
              <Button onClick={handleSubmit} disabled={loading}>
                {loading ? '生成中…' : '生成配置'}
              </Button>
            </>
          ) : (
            <Button onClick={handleClose}>完成</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="text-xs font-medium text-muted-foreground mb-1.5 block">{label}</label>
      {children}
    </div>
  )
}

// ─────────────────────────────── main page ───────────────────────────────────

export default function ClusterPage() {
  const [health, setHealth] = useState<HealthResult | null>(null)
  const [clusterInfo, setClusterInfo] = useState<ClusterInfo | null>(null)
  const [topology, setTopology] = useState<ClusterTopology | null>(null)
  const [config, setConfig] = useState<Record<string, unknown> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showEnableDialog, setShowEnableDialog] = useState(false)

  const fetchData = useCallback(async () => {
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
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const isHealthy = health?.status === 'healthy'
  // Standalone: no Redis service-discovery configured (mode is 'standalone' or 'single-node').
  // Distributed: Redis is configured and the cluster has multi-node coordination enabled.
  const isStandalone = !clusterInfo || (clusterInfo.mode !== 'distributed')
  const realNodes = topology?.nodes.filter(n => !n.virtual) ?? []
  const deploymentHint = topology ? detectDeploymentHint(topology.nodes) : null

  // Human-readable cluster mode label
  const clusterModeLabel = (mode: string) => {
    if (mode === 'distributed') return '分布式'
    if (mode === 'standalone' || mode === 'single-node') return '单节点'
    return mode
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">

        {/* subtitle + health badge */}
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">查看集群整体状态与配置</p>
          <div className="flex items-center gap-2">
            {!loading && deploymentHint && (
              <Badge variant="secondary">{deploymentHint}</Badge>
            )}
            {!loading && (
              <Badge variant={isHealthy ? 'success' : 'error'}>
                {isHealthy ? '健康' : '异常'}
              </Badge>
            )}
          </div>
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {loading ? (
          <div className="space-y-4">
            <div className="h-32 rounded-lg border border-border bg-card animate-pulse" />
            <div className="h-80 rounded-lg border border-border bg-card animate-pulse" />
          </div>
        ) : (
          <>
            {/* stat cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <StatCard
                title="集群模式"
                value={clusterInfo ? clusterModeLabel(clusterInfo.mode) : '-'}
                subValue={clusterInfo && clusterInfo.mode !== clusterModeLabel(clusterInfo.mode) ? clusterInfo.mode : undefined}
              />
              <StatCard title="节点数量" value={String(clusterInfo?.nodes_count ?? 0)} />
              <StatCard title="表数量" value={String(clusterInfo?.tables_count ?? 0)} />
              <StatCard title="版本" value={clusterInfo?.version ?? '-'} />
            </div>

            {/* topology */}
            <Card>
              <CardHeader className="flex flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-semibold">节点拓扑</CardTitle>
                {isStandalone && (
                  <Button variant="outline" size="sm"
                    onClick={() => setShowEnableDialog(true)}
                    className="h-7 text-xs border-primary/40 text-primary hover:bg-primary/10">
                    开启分布式模式
                  </Button>
                )}
              </CardHeader>
              <CardContent>
                {realNodes.length === 1 && isStandalone ? (
                  <StandaloneNode node={realNodes[0]} onEnable={() => setShowEnableDialog(true)} />
                ) : topology && topology.nodes.length > 0 ? (
                  <>
                    <ReactECharts
                      option={buildGraphOption(topology)}
                      style={{ height: realNodes.length > 6 ? 500 : 380 }}
                      theme="dark"
                    />
                    {/* legend — only show Redis items when coordinator is present */}
                    {(() => {
                      const hasRedisNode = topology.nodes.some(n => n.virtual && n.metadata?.type === 'redis')
                      return (
                        <div className="flex flex-wrap gap-4 mt-3 text-xs text-muted-foreground">
                          <span className="flex items-center gap-1.5">
                            <span className="w-3 h-3 rounded-full bg-green-500" />在线节点
                          </span>
                          <span className="flex items-center gap-1.5">
                            <span className="w-3 h-3 rounded-full bg-red-500" />离线节点
                          </span>
                          {hasRedisNode && (
                            <>
                              <span className="flex items-center gap-1.5">
                                <span className="w-3 h-3 rotate-45 inline-block bg-purple-500" />Redis 协调器
                              </span>
                              <span className="flex items-center gap-1.5">
                                <span className="w-6 border-t border-dashed border-purple-400" />协调链路
                              </span>
                            </>
                          )}
                        </div>
                      )
                    })()}
                    {/* node list */}
                    <div className="mt-4 space-y-1.5">
                      {realNodes.map(node => (
                        <div key={node.id}
                          className="flex items-center justify-between rounded-md px-3 py-2 text-xs bg-muted/40 border border-border/60">
                          <div className="flex items-center gap-2 min-w-0">
                            <span className={`w-2 h-2 rounded-full flex-shrink-0 ${
                              node.status === 'healthy' || node.status === 'online' || node.status === 'running'
                                ? 'bg-green-500' : 'bg-red-500'
                            }`} />
                            <span className="font-mono text-foreground truncate">{node.id}</span>
                          </div>
                          <div className="flex items-center gap-3 flex-shrink-0 ml-3">
                            <span className="text-muted-foreground">{node.address}{node.port ? ':' + node.port : ''}</span>
                            <Badge variant={node.status === 'healthy' || node.status === 'online' || node.status === 'running' ? 'success' : 'error'}>
                              {node.status === 'healthy' || node.status === 'online' || node.status === 'running' ? 'running' : 'error'}
                            </Badge>
                          </div>
                        </div>
                      ))}
                      {topology.redis_addr && (
                        <div key={topology.redis_addr}
                          className="flex items-center justify-between rounded-md px-3 py-2 text-xs bg-muted/40 border border-border/60">
                          <div className="flex items-center gap-2 min-w-0">
                            <span className={`w-2 h-2 rounded-full flex-shrink-0 bg-green-500`} />
                            <span className="font-mono text-foreground truncate">Redis</span>
                          </div>
                          <div className="flex items-center gap-3 flex-shrink-0 ml-3">
                            <span className="text-muted-foreground">{topology.redis_addr}</span>
                            <Badge variant="success">
                              running
                            </Badge>
                          </div>
                        </div>
                    )}
                    </div>
                    
                  </>
                ) : (
                  <div className="h-40 flex items-center justify-center text-sm text-muted-foreground">
                    暂无拓扑数据
                  </div>
                )}
              </CardContent>
            </Card>

            {/* cluster config */}
            <Card>
              <CardHeader>
                <CardTitle className="text-sm font-semibold">集群配置</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-1 text-sm">
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

      <EnableDistributedDialog
        open={showEnableDialog}
        onClose={() => setShowEnableDialog(false)}
      />
    </DashboardLayout>
  )
}

// ─────────────────────────────── sub-components ──────────────────────────────

function StatCard({ title, value, subValue }: { title: string; value: string; subValue?: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="pt-0">
        <p className="text-2xl font-bold text-foreground">{value}</p>
        {subValue && <p className="text-xs text-muted-foreground mt-0.5 font-mono">{subValue}</p>}
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
