'use client'

import { useEffect, useState } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
import { apiClient } from '@/lib/api/client'
import { NodeInfo } from '@/types/api'

function formatLastSeen(lastSeen: string): string {
  if (!lastSeen) return '-'
  const d = new Date(lastSeen)
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '-'
  return d.toLocaleString('zh-CN')
}

interface NodesData {
  nodes: NodeInfo[]
  total: number
}

export default function NodesPage() {
  const [data, setData] = useState<NodesData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selectedNode, setSelectedNode] = useState<NodeInfo | null>(null)
  const [sheetOpen, setSheetOpen] = useState(false)

  useEffect(() => {
    apiClient.get<NodesData>('/nodes')
      .then(setData)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const handleRowClick = (node: NodeInfo) => {
    setSelectedNode(node)
    setSheetOpen(true)
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div>
          <p className="text-sm text-muted-foreground">
            当前节点总数：{loading ? '...' : data?.total ?? 0}
          </p>
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {loading ? (
          <div className="h-80 rounded-lg border border-border bg-card animate-pulse" />
        ) : data?.nodes.length === 0 ? (
          <EmptyState message="暂无节点数据" description="集群中尚未发现活跃节点" />
        ) : (
          <Card>
            <CardContent className="p-0">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-muted/50">
                      <th className="px-4 py-3 text-left font-medium text-muted-foreground">节点 ID</th>
                      <th className="px-4 py-3 text-left font-medium text-muted-foreground">地址</th>
                      <th className="px-4 py-3 text-left font-medium text-muted-foreground">端口</th>
                      <th className="px-4 py-3 text-left font-medium text-muted-foreground">状态</th>
                      <th className="px-4 py-3 text-left font-medium text-muted-foreground">最后心跳</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data?.nodes.map((node) => (
                      <tr
                        key={node.id}
                        className="border-b border-border last:border-0 hover:bg-muted/30 cursor-pointer transition-colors"
                        onClick={() => handleRowClick(node)}
                      >
                        <td className="px-4 py-3 font-mono text-xs">{node.id.substring(0, 12)}...</td>
                        <td className="px-4 py-3">{node.address}</td>
                        <td className="px-4 py-3">{node.port}</td>
                        <td className="px-4 py-3">
                          <StatusBadge status={node.status} />
                        </td>
                        <td className="px-4 py-3 text-muted-foreground">
                          {formatLastSeen(node.last_seen)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        )}

        {/* Node Detail Sheet */}
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent>
            <SheetHeader>
              <SheetTitle>节点详情</SheetTitle>
              <SheetDescription>查看节点详细信息</SheetDescription>
            </SheetHeader>
            {selectedNode && (
              <div className="mt-6 space-y-6">
                  <div className="flex items-center gap-3">
                  <div className={`w-3 h-3 rounded-full ${
                    selectedNode.status === 'healthy' || selectedNode.status === 'online' || selectedNode.status === 'running' || selectedNode.status === 'active'
                      ? 'bg-green-500'
                      : 'bg-red-500'
                  }`} />
                  <div>
                    <p className="font-mono text-sm">{selectedNode.id}</p>
                    <p className="text-xs text-muted-foreground">节点 ID</p>
                  </div>
                </div>

                <Card>
                  <CardHeader className="pb-2">
                    <CardTitle className="text-sm">基本信息</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-3 text-sm">
                      <DetailRow label="地址" value={selectedNode.address} />
                      <DetailRow label="端口" value={selectedNode.port} />
                      <DetailRow label="状态" value={selectedNode.status === 'healthy' || selectedNode.status === 'online' || selectedNode.status === 'running' || selectedNode.status === 'active' ? 'running' : 'error'} highlight={selectedNode.status === 'healthy' || selectedNode.status === 'online' || selectedNode.status === 'running' || selectedNode.status === 'active'} />
                      <DetailRow
                        label="最后心跳"
                        value={formatLastSeen(selectedNode.last_seen)}
                      />
                    </div>
                  </CardContent>
                </Card>

                {selectedNode.metadata && Object.keys(selectedNode.metadata).length > 0 && (
                  <Card>
                    <CardHeader className="pb-2">
                      <CardTitle className="text-sm">元数据</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <div className="space-y-3 text-sm">
                        {Object.entries(selectedNode.metadata).map(([key, value]) => (
                          <DetailRow key={key} label={key} value={String(value)} />
                        ))}
                      </div>
                    </CardContent>
                  </Card>
                )}
              </div>
            )}
          </SheetContent>
        </Sheet>
      </div>
    </DashboardLayout>
  )
}

function StatusBadge({ status }: { status: string }) {
  const isOnline = status === 'healthy' || status === 'online' || status === 'running' || status === 'active'
  return (
    <Badge variant={isOnline ? 'success' : 'error'}>
      {isOnline ? 'running' : 'error'}
    </Badge>
  )
}

function DetailRow({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span className={`font-medium ${highlight ? 'text-green-600 dark:text-green-400' : ''}`}>{value}</span>
    </div>
  )
}

function EmptyState({ message, description }: { message: string; description?: string }) {
  return (
    <Card>
      <CardContent className="flex flex-col items-center justify-center py-16">
        <p className="text-foreground font-medium">{message}</p>
        {description && <p className="text-sm text-muted-foreground mt-1">{description}</p>}
      </CardContent>
    </Card>
  )
}
