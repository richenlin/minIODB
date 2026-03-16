'use client'

import { useEffect, useState, useRef, useCallback } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { logsApi } from '@/lib/api/logs'
import { LogEntry, LogQueryParams } from '@/lib/api/types'
import { PlayIcon, PauseIcon, ReloadIcon } from '@radix-ui/react-icons'

const LEVELS = ['all', 'debug', 'info', 'warn', 'error'] as const
type Level = typeof LEVELS[number]

const levelConfig: Record<string, { color: string; bg: string }> = {
  error: { color: 'text-red-600 dark:text-red-400', bg: 'bg-red-100 dark:bg-red-900/30' },
  warn: { color: 'text-yellow-600 dark:text-yellow-400', bg: 'bg-yellow-100 dark:bg-yellow-900/30' },
  info: { color: 'text-blue-600 dark:text-blue-400', bg: 'bg-blue-100 dark:bg-blue-900/30' },
  debug: { color: 'text-gray-600 dark:text-gray-400', bg: 'bg-gray-100 dark:bg-gray-800/30' },
}

function formatTimestamp(ts: number): string {
  const d = new Date(ts * 1000)
  return d.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

export default function LogsPage() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(100)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [levelFilter, setLevelFilter] = useState<Level>('all')
  const [keyword, setKeyword] = useState('')
  const [startTime, setStartTime] = useState('')
  const [endTime, setEndTime] = useState('')

  const [sseEnabled, setSseEnabled] = useState(false)
  const [ssePaused, setSsePaused] = useState(false)
  const [sseConnected, setSseConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)
  const logsContainerRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)

  const fetchLogs = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const params: LogQueryParams = { page, page_size: pageSize }
      if (levelFilter !== 'all') params.level = levelFilter
      if (keyword) params.keyword = keyword
      if (startTime) params.start_time = Math.floor(new Date(startTime).getTime() / 1000)
      if (endTime) params.end_time = Math.floor(new Date(endTime).getTime() / 1000)

      const result = await logsApi.queryLogs(params)
      setLogs(result.logs || [])
      setTotal(result.total || 0)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to fetch logs')
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, levelFilter, keyword, startTime, endTime])

  useEffect(() => {
    if (!sseEnabled) fetchLogs()
  }, [sseEnabled, fetchLogs])

  useEffect(() => {
    if (!sseEnabled || ssePaused) {
      if (esRef.current) {
        esRef.current.close()
        esRef.current = null
      }
      setSseConnected(false)
      return
    }

    const stored = localStorage.getItem('miniodb-auth')
    const token = stored ? JSON.parse(stored)?.state?.token : null
    const baseUrl = logsApi.streamLogs()
    const fullUrl = token ? `${baseUrl}?token=${token}` : baseUrl

    const es = new EventSource(fullUrl)
    esRef.current = es

    es.onopen = () => setSseConnected(true)
    es.onerror = () => setSseConnected(false)
    es.onmessage = (e) => {
      try {
        const entry: LogEntry = JSON.parse(e.data)
        if (levelFilter !== 'all' && entry.level !== levelFilter) return
        if (keyword && !entry.message.toLowerCase().includes(keyword.toLowerCase())) return
        setLogs(prev => {
          const newLogs = [...prev, entry]
          return newLogs.slice(-500)
        })
      } catch { /* ignore */ }
    }

    return () => { es.close(); setSseConnected(false) }
  }, [sseEnabled, ssePaused, levelFilter, keyword])

  useEffect(() => {
    if (autoScroll && logsContainerRef.current) {
      logsContainerRef.current.scrollTop = logsContainerRef.current.scrollHeight
    }
  }, [logs, autoScroll])

  const handleSearch = () => {
    setPage(1)
    if (sseEnabled) {
      setLogs([])
    } else {
      fetchLogs()
    }
  }

  const handleReset = () => {
    setLevelFilter('all')
    setKeyword('')
    setStartTime('')
    setEndTime('')
    setPage(1)
  }

  const totalPages = Math.ceil(total / pageSize)

  return (
    <DashboardLayout>
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">系统运行日志记录与实时尾随</p>
          </div>
          <div className="flex items-center gap-2">
            {sseEnabled && (
              <Badge variant={sseConnected ? 'default' : 'secondary'} className="text-xs">
                {sseConnected ? 'SSE 已连接' : 'SSE 断开'}
              </Badge>
            )}
          </div>
        </div>

        <div className="rounded-lg border border-border bg-card p-4 space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-1">
              {LEVELS.map(lv => (
                <Button
                  key={lv}
                  variant={levelFilter === lv ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setLevelFilter(lv)}
                  className="capitalize"
                >
                  {lv}
                </Button>
              ))}
            </div>

            <input
              type="text"
              placeholder="关键词搜索..."
              value={keyword}
              onChange={e => setKeyword(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSearch()}
              className="h-9 w-48 rounded-md border border-input bg-background px-3 text-sm"
            />

            <div className="flex items-center gap-2 text-sm">
              <span className="text-muted-foreground">时间:</span>
              <input
                type="datetime-local"
                value={startTime}
                onChange={e => setStartTime(e.target.value)}
                className="h-9 rounded-md border border-input bg-background px-2 text-sm"
              />
              <span className="text-muted-foreground">-</span>
              <input
                type="datetime-local"
                value={endTime}
                onChange={e => setEndTime(e.target.value)}
                className="h-9 rounded-md border border-input bg-background px-2 text-sm"
              />
            </div>

            <Button variant="outline" size="sm" onClick={handleSearch}>
              搜索
            </Button>
            <Button variant="ghost" size="sm" onClick={handleReset}>
              重置
            </Button>
          </div>

          <div className="flex items-center gap-3 pt-2 border-t border-border">
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">实时尾随:</span>
              <Button
                variant={sseEnabled ? 'default' : 'outline'}
                size="sm"
                onClick={() => { setSseEnabled(!sseEnabled); setLogs([]) }}
              >
                {sseEnabled ? '关闭' : '开启'}
              </Button>
            </div>

            {sseEnabled && (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSsePaused(!ssePaused)}
                >
                  {ssePaused ? <PlayIcon className="w-4 h-4" /> : <PauseIcon className="w-4 h-4" />}
                  {ssePaused ? '恢复' : '暂停'}
                </Button>
                <label className="flex items-center gap-1 text-sm">
                  <input
                    type="checkbox"
                    checked={autoScroll}
                    onChange={e => setAutoScroll(e.target.checked)}
                    className="rounded"
                  />
                  自动滚动
                </label>
              </>
            )}

            {!sseEnabled && (
              <Button variant="outline" size="sm" onClick={fetchLogs} disabled={loading}>
                <ReloadIcon className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                刷新
              </Button>
            )}
          </div>
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        <div
          ref={logsContainerRef}
          className="rounded-lg border border-border bg-card overflow-hidden font-mono text-xs max-h-[600px] overflow-y-auto"
        >
          {loading && !sseEnabled ? (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
              加载中...
            </div>
          ) : logs.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16">
              <p className="text-foreground font-medium">暂无日志</p>
              <p className="text-sm text-muted-foreground mt-1">
                {sseEnabled ? '等待新日志...' : '调整过滤条件后搜索'}
              </p>
            </div>
          ) : (
            logs.map((log, i) => {
              const cfg = levelConfig[log.level] || levelConfig.info
              return (
                <div
                  key={`${log.timestamp}-${i}`}
                  className={`flex items-start gap-3 px-4 py-2 border-b border-border last:border-0 hover:bg-muted/30 ${cfg.bg}`}
                >
                  <span className="text-muted-foreground shrink-0 w-32">
                    {formatTimestamp(log.timestamp)}
                  </span>
                  <span className={`uppercase font-bold w-12 shrink-0 ${cfg.color}`}>
                    {log.level}
                  </span>
                  <span className="text-foreground flex-1 break-all">{log.message}</span>
                  {log.fields && Object.keys(log.fields).length > 0 && (
                    <span className="text-muted-foreground shrink-0 text-right">
                      {Object.entries(log.fields).slice(0, 3).map(([k, v]) => (
                        <span key={k} className="ml-2">{k}={String(v)}</span>
                      ))}
                    </span>
                  )}
                </div>
              )
            })
          )}
        </div>

        {!sseEnabled && totalPages > 1 && (
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">
              共 {total} 条日志，第 {page}/{totalPages} 页
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => setPage(p => p - 1)}
              >
                上一页
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={page >= totalPages}
                onClick={() => setPage(p => p + 1)}
              >
                下一页
              </Button>
            </div>
          </div>
        )}
      </div>
    </DashboardLayout>
  )
}
