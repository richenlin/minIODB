'use client'

import { useEffect, useState } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { backupApi } from '@/lib/api/backup'
import { BackupResult, MetadataStatusResult } from '@/lib/api/types'
import { ReloadIcon, FileIcon, ClockIcon } from '@radix-ui/react-icons'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function formatDateTime(iso: string): string {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString('zh-CN')
  } catch {
    return iso
  }
}

const statusConfig: Record<string, { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' }> = {
  completed: { label: '已完成', variant: 'default' },
  pending: { label: '进行中', variant: 'secondary' },
  failed: { label: '失败', variant: 'destructive' },
  running: { label: '运行中', variant: 'outline' },
}

const typeConfig: Record<string, string> = {
  metadata: '元数据备份',
  full: '全量备份',
  table: '表备份',
}

export default function BackupPage() {
  const [backups, setBackups] = useState<BackupResult[]>([])
  const [schedule, setSchedule] = useState<MetadataStatusResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [actionLoading, setActionLoading] = useState<string | null>(null)

  const [selectedBackup, setSelectedBackup] = useState<BackupResult | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)

  const fetchData = async () => {
    setLoading(true)
    setError('')
    try {
      const [backupsData, scheduleData] = await Promise.all([
        backupApi.list(),
        backupApi.getSchedule(),
      ])
      setBackups(backupsData || [])
      setSchedule(scheduleData)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch data')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  const handleMetadataBackup = async () => {
    setActionLoading('metadata')
    try {
      await backupApi.triggerMetadataBackup()
      await fetchData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Backup failed')
    } finally {
      setActionLoading(null)
    }
  }

  const handleFullBackup = () => {
    alert('全量备份功能开发中，后端 API 尚未实现')
  }

  const handleTableBackup = () => {
    alert('表备份功能开发中，后端 API 尚未实现')
  }

  const handleViewDetail = (backup: BackupResult) => {
    setSelectedBackup(backup)
    setDetailOpen(true)
  }

  const handleDelete = async (id: string) => {
    if (!confirm('确定要删除此备份吗？')) return
    setActionLoading(`delete-${id}`)
    try {
      await backupApi.delete(id)
      await fetchData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Delete failed')
    } finally {
      setActionLoading(null)
    }
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-foreground">备份管理</h1>
            <p className="text-sm text-muted-foreground mt-1">数据备份状态与管理</p>
          </div>
          <Button variant="outline" size="sm" onClick={fetchData} disabled={loading}>
            <ReloadIcon className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            刷新
          </Button>
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        <div className="grid gap-4 lg:grid-cols-3">
          <div className="rounded-lg border border-border bg-card p-5">
            <h2 className="text-sm font-semibold text-foreground mb-4 flex items-center gap-2">
              <ClockIcon className="w-4 h-4" />
              备份计划
            </h2>
            <div className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">备份功能</span>
                <Badge variant={schedule?.backup_enabled ? 'default' : 'secondary'}>
                  {schedule?.backup_enabled ? '已启用' : '未启用'}
                </Badge>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">备份间隔</span>
                <span className="font-medium">{schedule?.backup_interval ?? '-'} 小时</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">上次备份</span>
                <span className="font-medium">{schedule?.last_backup ? formatDateTime(schedule.last_backup) : '从未备份'}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">表数量</span>
                <span className="font-medium">{schedule?.tables_count ?? 0}</span>
              </div>
            </div>
          </div>

          <div className="rounded-lg border border-border bg-card p-5 lg:col-span-2">
            <h2 className="text-sm font-semibold text-foreground mb-4">手动备份</h2>
            <div className="flex flex-wrap gap-3">
              <Button onClick={handleMetadataBackup} disabled={actionLoading === 'metadata'}>
                {actionLoading === 'metadata' && <ReloadIcon className="w-4 h-4 animate-spin mr-2" />}
                元数据备份
              </Button>
              <Button variant="outline" disabled title="功能开发中">
                全量备份（开发中）
              </Button>
              <Button variant="outline" disabled title="功能开发中">
                表备份（开发中）
              </Button>
            </div>
            <p className="text-xs text-muted-foreground mt-3">
              元数据备份会保存所有表结构和配置信息。全量备份和表备份功能的后端 API 尚未实现。
            </p>
          </div>
        </div>

        <div className="rounded-lg border border-border bg-card overflow-hidden">
          <div className="px-5 py-4 border-b border-border">
            <h2 className="text-sm font-semibold text-foreground flex items-center gap-2">
              <FileIcon className="w-4 h-4" />
              备份列表
            </h2>
          </div>

          {loading ? (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
              加载中...
            </div>
          ) : backups.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16">
              <p className="text-foreground font-medium">暂无备份记录</p>
              <p className="text-sm text-muted-foreground mt-1">点击"元数据备份"创建第一个备份</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted/50">
                  <tr>
                    <th className="px-4 py-3 text-left font-medium text-muted-foreground">ID</th>
                    <th className="px-4 py-3 text-left font-medium text-muted-foreground">类型</th>
                    <th className="px-4 py-3 text-left font-medium text-muted-foreground">状态</th>
                    <th className="px-4 py-3 text-left font-medium text-muted-foreground">大小</th>
                    <th className="px-4 py-3 text-left font-medium text-muted-foreground">创建时间</th>
                    <th className="px-4 py-3 text-left font-medium text-muted-foreground">完成时间</th>
                    <th className="px-4 py-3 text-right font-medium text-muted-foreground">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {backups.map(backup => {
                    const status = statusConfig[backup.status] || { label: backup.status, variant: 'secondary' as const }
                    return (
                      <tr key={backup.id} className="hover:bg-muted/30">
                        <td className="px-4 py-3 font-mono text-xs">{backup.id.slice(0, 8)}...</td>
                        <td className="px-4 py-3">{typeConfig[backup.type] || backup.type}</td>
                        <td className="px-4 py-3">
                          <Badge variant={status.variant}>{status.label}</Badge>
                        </td>
                        <td className="px-4 py-3">{formatBytes(backup.size_bytes)}</td>
                        <td className="px-4 py-3 text-muted-foreground">{formatDateTime(backup.created_at)}</td>
                        <td className="px-4 py-3 text-muted-foreground">{formatDateTime(backup.completed_at)}</td>
                        <td className="px-4 py-3 text-right space-x-2">
                          <Button variant="ghost" size="sm" onClick={() => handleViewDetail(backup)}>
                            详情
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-destructive hover:text-destructive"
                            onClick={() => handleDelete(backup.id)}
                            disabled={actionLoading === `delete-${backup.id}`}
                          >
                            删除
                          </Button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>备份详情</DialogTitle>
              <DialogDescription>
                备份 ID: {selectedBackup?.id}
              </DialogDescription>
            </DialogHeader>
            {selectedBackup && (
              <div className="space-y-4 text-sm">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <span className="text-muted-foreground">类型</span>
                    <p className="font-medium">{typeConfig[selectedBackup.type] || selectedBackup.type}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">状态</span>
                    <p className="font-medium">
                      <Badge variant={statusConfig[selectedBackup.status]?.variant || 'secondary'}>
                        {statusConfig[selectedBackup.status]?.label || selectedBackup.status}
                      </Badge>
                    </p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">大小</span>
                    <p className="font-medium">{formatBytes(selectedBackup.size_bytes)}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">表名</span>
                    <p className="font-medium">{selectedBackup.table_name || '-'}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">创建时间</span>
                    <p className="font-medium">{formatDateTime(selectedBackup.created_at)}</p>
                  </div>
                  <div>
                    <span className="text-muted-foreground">完成时间</span>
                    <p className="font-medium">{formatDateTime(selectedBackup.completed_at)}</p>
                  </div>
                </div>
                {selectedBackup.tables && selectedBackup.tables.length > 0 && (
                  <div>
                    <span className="text-muted-foreground">包含表</span>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {selectedBackup.tables.map(t => (
                        <Badge key={t} variant="outline">{t}</Badge>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </DialogContent>
        </Dialog>
      </div>
    </DashboardLayout>
  )
}
