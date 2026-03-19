'use client'

import { useEffect, useState } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { backupApi, VerifyResult, AvailabilityResult, HotBackupStatus, HotBackupAvailability } from '@/lib/api/backup'
import { dataApi } from '@/lib/api/data'
import { BackupResult, MetadataStatusResult, TableResult } from '@/lib/api/types'
import { ReloadIcon, FileIcon, ClockIcon, DownloadIcon, CheckCircledIcon, ExclamationTriangleIcon } from '@radix-ui/react-icons'

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
  const [availability, setAvailability] = useState<AvailabilityResult | null>(null)

  const [selectedBackup, setSelectedBackup] = useState<BackupResult | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [restoreOpen, setRestoreOpen] = useState(false)
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null)
  const [tableBackupOpen, setTableBackupOpen] = useState(false)
  const [tables, setTables] = useState<TableResult[]>([])
  const [selectedTable, setSelectedTable] = useState<string>('')
  const [hotBackupStatus, setHotBackupStatus] = useState<HotBackupStatus | null>(null)

  const fetchData = async () => {
    setLoading(true)
    setError('')
    try {
      const availabilityData = await backupApi.checkAvailability()
      setAvailability(availabilityData)
      if (!availabilityData.available) {
        setBackups([])
        setSchedule(null)
        setHotBackupStatus(null)
        return
      }
      const [backupsData, scheduleData, hotBackupData] = await Promise.all([
        backupApi.list(),
        backupApi.getSchedule(),
        backupApi.getHotBackupStatus().catch(() => null),
      ])
      setBackups(backupsData || [])
      setSchedule(scheduleData)
      setHotBackupStatus(hotBackupData)
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

  const handleFullBackup = async () => {
    if (!confirm('确定要执行全量备份吗？（注意：当前执行的是元数据备份，全量数据备份尚未实现）')) return
    setActionLoading('full')
    try {
      await backupApi.triggerFullBackup()
      await fetchData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Full backup failed')
    } finally {
      setActionLoading(null)
    }
  }

  const handleTableBackup = async (tableName: string) => {
    setActionLoading(`table-${tableName}`)
    try {
      await backupApi.triggerTableBackup(tableName)
      setTableBackupOpen(false)
      setSelectedTable('')
      await fetchData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Table backup failed')
    } finally {
      setActionLoading(null)
    }
  }

  const handleOpenTableBackup = async () => {
    try {
      const tablesData = await dataApi.listTables()
      setTables(tablesData)
      setTableBackupOpen(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load tables')
    }
  }

  const handleConfirmTableBackup = async () => {
    if (!selectedTable) return
    await handleTableBackup(selectedTable)
  }

  const handleViewDetail = (backup: BackupResult) => {
    setSelectedBackup(backup)
    setDetailOpen(true)
  }

  const handleRestore = (backup: BackupResult) => {
    setSelectedBackup(backup)
    setRestoreOpen(true)
  }

  const handleConfirmRestore = async () => {
    if (!selectedBackup) return
    setActionLoading(`restore-${selectedBackup.id}`)
    try {
      await backupApi.restore(selectedBackup.id)
      setRestoreOpen(false)
      await fetchData()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Restore failed')
    } finally {
      setActionLoading(null)
    }
  }

  const handleDownload = async (backup: BackupResult) => {
    setActionLoading(`download-${backup.id}`)
    try {
      const result = await backupApi.download(backup.id)
      if (result.download_url) {
        window.open(result.download_url, '_blank')
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Download failed')
    } finally {
      setActionLoading(null)
    }
  }

  const handleVerify = async (backup: BackupResult) => {
    setActionLoading(`verify-${backup.id}`)
    try {
      const result = await backupApi.verify(backup.id)
      setVerifyResult(result)
      setSelectedBackup(backup)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Verify failed')
    } finally {
      setActionLoading(null)
    }
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
            <p className="text-sm text-muted-foreground">数据备份状态与管理</p>
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

        {availability && !availability.available && (
          <div className="rounded-md bg-yellow-500/10 border border-yellow-500/20 px-4 py-3 text-sm text-yellow-600 dark:text-yellow-400">
            <div className="flex items-center gap-2">
              <ExclamationTriangleIcon className="w-4 h-4" />
              <span>{availability.message}</span>
            </div>
            <p className="mt-1 text-xs text-muted-foreground">
              请在配置文件中设置 network.pools.minio_backup 以启用备份功能。
            </p>
          </div>
        )}

        {hotBackupStatus && hotBackupStatus.available && (
          <div className="rounded-lg border border-border bg-card p-5">
            <h2 className="text-sm font-semibold text-foreground mb-4 flex items-center gap-2">
              <CheckCircledIcon className="w-4 h-4" />
              热备状态
            </h2>
            <div className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">运行状态</span>
                <Badge variant={hotBackupStatus.progress?.is_running ? 'default' : 'secondary'}>
                  {hotBackupStatus.progress?.is_running ? '同步中' : '空闲'}
                </Badge>
              </div>
              {hotBackupStatus.state && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">最后同步时间</span>
                  <span className="font-medium">{formatDateTime(hotBackupStatus.state.last_sync_time)}</span>
                </div>
              )}
              {hotBackupStatus.progress && (
                <>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">同步进度</span>
                    <span className="font-medium">
                      {hotBackupStatus.progress.synced_objects} / {hotBackupStatus.progress.total_objects} 对象
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">数据量</span>
                    <span className="font-medium">
                      {formatBytes(hotBackupStatus.progress.synced_size)} / {formatBytes(hotBackupStatus.progress.total_size)}
                    </span>
                  </div>
                </>
              )}
              {hotBackupStatus.state?.last_sync_errors && hotBackupStatus.state.last_sync_errors.length > 0 && (
                <div className="mt-2">
                  <span className="text-muted-foreground text-xs">同步错误</span>
                  <div className="mt-1 text-xs text-destructive">
                    {hotBackupStatus.state.last_sync_errors.slice(0, 3).map((err, i) => (
                      <div key={i}>{err}</div>
                    ))}
                  </div>
                </div>
              )}
            </div>
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
                <span className="font-medium">{schedule?.backup_interval ?? '-'} 秒</span>
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
              <Button onClick={handleMetadataBackup} disabled={!availability?.available || actionLoading === 'metadata'}>
                {actionLoading === 'metadata' && <ReloadIcon className="w-4 h-4 animate-spin mr-2" />}
                元数据备份
              </Button>
              <Button variant="outline" onClick={handleFullBackup} disabled={!availability?.available || actionLoading === 'full'}>
                {actionLoading === 'full' && <ReloadIcon className="w-4 h-4 animate-spin mr-2" />}
                全量备份
              </Button>
              <Button variant="outline" onClick={handleOpenTableBackup} disabled={!availability?.available || actionLoading?.startsWith('table-')}>
                {actionLoading?.startsWith('table-') && <ReloadIcon className="w-4 h-4 animate-spin mr-2" />}
                表备份
              </Button>
            </div>
            <p className="text-xs text-muted-foreground mt-3">
              元数据备份保存表结构和配置；全量备份和表备份当前执行元数据备份（全量数据备份尚未实现）。
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
                    <th className="px-4 py-3 text-right font-medium text-muted-foreground">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {backups.map(backup => {
                    const status = statusConfig[backup.status] || { label: backup.status, variant: 'secondary' as const }
                    return (
                      <tr key={backup.id} className="hover:bg-muted/30">
                        <td className="px-4 py-3 font-mono text-xs">{backup.id.slice(0, 12)}...</td>
                        <td className="px-4 py-3">{typeConfig[backup.type] || backup.type}</td>
                        <td className="px-4 py-3">
                          <Badge variant={status.variant}>{status.label}</Badge>
                        </td>
                        <td className="px-4 py-3">{formatBytes(backup.size_bytes)}</td>
                        <td className="px-4 py-3 text-muted-foreground">{formatDateTime(backup.created_at)}</td>
                        <td className="px-4 py-3 text-right space-x-1">
                          <Button variant="ghost" size="sm" onClick={() => handleViewDetail(backup)}>
                            详情
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => handleDownload(backup)} disabled={actionLoading === `download-${backup.id}`}>
                            <DownloadIcon className="w-4 h-4" />
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => handleVerify(backup)} disabled={actionLoading === `verify-${backup.id}`}>
                            <CheckCircledIcon className="w-4 h-4" />
                          </Button>
                          <Button variant="ghost" size="sm" onClick={() => handleRestore(backup)}>
                            恢复
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

        {/* 详情对话框 */}
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
                    <span className="text-muted-foreground">创建时间</span>
                    <p className="font-medium">{formatDateTime(selectedBackup.created_at)}</p>
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

        {/* 恢复对话框 */}
        <Dialog open={restoreOpen} onOpenChange={setRestoreOpen}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>恢复备份</DialogTitle>
              <DialogDescription>
                确定要恢复备份 {selectedBackup?.id?.slice(0, 12)}... 吗？
              </DialogDescription>
            </DialogHeader>
            <div className="text-sm text-muted-foreground py-4">
              恢复操作将覆盖当前的数据。请确保已做好相应准备。
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setRestoreOpen(false)}>取消</Button>
              <Button onClick={handleConfirmRestore} disabled={actionLoading?.startsWith('restore-')}>
                {actionLoading?.startsWith('restore-') && <ReloadIcon className="w-4 h-4 animate-spin mr-2" />}
                确认恢复
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 验证结果对话框 */}
        <Dialog open={!!verifyResult} onOpenChange={() => setVerifyResult(null)}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>备份验证结果</DialogTitle>
            </DialogHeader>
            {verifyResult && (
              <div className="space-y-3 text-sm">
                <div className="flex items-center gap-2">
                  {verifyResult.valid ? (
                    <CheckCircledIcon className="w-5 h-5 text-green-500" />
                  ) : (
                    <ExclamationTriangleIcon className="w-5 h-5 text-red-500" />
                  )}
                  <span className="font-medium">{verifyResult.valid ? '备份有效' : '备份无效'}</span>
                </div>
                {verifyResult.valid && (
                  <>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">文件大小</span>
                      <span>{formatBytes(verifyResult.size_bytes)}</span>
                    </div>
                    {verifyResult.last_modified && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">最后修改</span>
                        <span>{formatDateTime(verifyResult.last_modified)}</span>
                      </div>
                    )}
                  </>
                )}
                {verifyResult.error && (
                  <div className="text-destructive">错误: {verifyResult.error}</div>
                )}
              </div>
            )}
          </DialogContent>
        </Dialog>

        {/* 表选择对话框 */}
        <Dialog open={tableBackupOpen} onOpenChange={setTableBackupOpen}>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>表备份</DialogTitle>
              <DialogDescription>
                选择要备份的表。注意：当前执行的是元数据备份，全量数据备份尚未实现。
              </DialogDescription>
            </DialogHeader>
            <div className="py-4">
              <select
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={selectedTable}
                onChange={(e) => setSelectedTable(e.target.value)}
              >
                <option value="">请选择表</option>
                {tables.map((table) => (
                  <option key={table.name} value={table.name}>
                    {table.name}
                  </option>
                ))}
              </select>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setTableBackupOpen(false)}>取消</Button>
              <Button onClick={handleConfirmTableBackup} disabled={!selectedTable || actionLoading?.startsWith('table-')}>
                {actionLoading?.startsWith('table-') && <ReloadIcon className="w-4 h-4 animate-spin mr-2" />}
                确认备份
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </DashboardLayout>
  )
}
