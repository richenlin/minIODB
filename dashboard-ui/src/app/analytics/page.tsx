'use client'

import { useState, useEffect, useCallback } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { 
  Table, 
  TableBody, 
  TableCell, 
  TableHead, 
  TableHeader, 
  TableRow 
} from '@/components/ui/table'
import { SqlEditor } from '@/components/code-editor/sql-editor'
import { dataApi } from '@/lib/api/data'
import { 
  PlayIcon,
  ClockIcon,
  DownloadIcon,
  TrashIcon,
  CounterClockwiseClockIcon,
} from '@radix-ui/react-icons'
import type { QueryResult } from '@/lib/api/types'

const HISTORY_KEY = 'miniodb-query-history'
const MAX_HISTORY = 20

interface QueryHistoryItem {
  sql: string
  executedAt: string
  rowCount: number
  durationMs: number
}

function loadHistory(): QueryHistoryItem[] {
  if (typeof window === 'undefined') return []
  try {
    const data = localStorage.getItem(HISTORY_KEY)
    return data ? JSON.parse(data) : []
  } catch {
    return []
  }
}

function saveHistory(history: QueryHistoryItem[]): void {
  if (typeof window === 'undefined') return
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(history.slice(0, MAX_HISTORY)))
  } catch (e) {
    console.error('Failed to save query history:', e)
  }
}

export default function AnalyticsPage() {
  const [sqlQuery, setSqlQuery] = useState('')
  const [sqlResult, setSqlResult] = useState<QueryResult | null>(null)
  const [sqlLoading, setSqlLoading] = useState(false)
  const [sqlError, setSqlError] = useState('')
  const [queryHistory, setQueryHistory] = useState<QueryHistoryItem[]>([])

  useEffect(() => {
    setQueryHistory(loadHistory())
  }, [])

  const handleExecuteSql = useCallback(async () => {
    if (!sqlQuery.trim()) return
    
    setSqlLoading(true)
    setSqlError('')
    setSqlResult(null)
    
    const startTime = Date.now()
    
    try {
      const result = await dataApi.querySQL(sqlQuery)
      setSqlResult(result)
      
      const historyItem: QueryHistoryItem = {
        sql: sqlQuery,
        executedAt: new Date().toISOString(),
        rowCount: result.total,
        durationMs: result.duration_ms || (Date.now() - startTime),
      }
      
      const newHistory = [historyItem, ...queryHistory.filter(h => h.sql !== sqlQuery)]
        .slice(0, MAX_HISTORY)
      setQueryHistory(newHistory)
      saveHistory(newHistory)
    } catch (e) {
      setSqlError(e instanceof Error ? e.message : '执行 SQL 失败')
    } finally {
      setSqlLoading(false)
    }
  }, [sqlQuery, queryHistory])

  const handleHistoryClick = (item: QueryHistoryItem) => {
    setSqlQuery(item.sql)
  }

  const handleClearHistory = () => {
    setQueryHistory([])
    saveHistory([])
  }

  const handleExport = () => {
    if (!sqlResult || sqlResult.rows.length === 0) return
    
    const columns = sqlResult.columns
    const csvContent = [
      columns.join(','),
      ...sqlResult.rows.map(row => 
        columns.map(col => {
          const value = row[col]
          if (value === null || value === undefined) return ''
          const str = String(value)
          return str.includes(',') || str.includes('"') || str.includes('\n')
            ? `"${str.replace(/"/g, '""')}"` 
            : str
        }).join(',')
      )
    ].join('\n')
    
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `query_result_${new Date().toISOString().split('T')[0]}.csv`
    link.click()
    URL.revokeObjectURL(link.href)
  }

  const formatTime = (isoString: string): string => {
    const date = new Date(isoString)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMins = Math.floor(diffMs / 60000)
    
    if (diffMins < 1) return '刚刚'
    if (diffMins < 60) return `${diffMins} 分钟前`
    if (diffMins < 1440) return `${Math.floor(diffMins / 60)} 小时前`
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div>
          <p className="text-sm text-muted-foreground">SQL 查询与数据分析</p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 space-y-4">
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-lg flex items-center gap-2">
                  <PlayIcon className="h-5 w-5" />
                  SQL 编辑器
                </CardTitle>
              </CardHeader>
              <CardContent>
                <SqlEditor
                  value={sqlQuery}
                  onChange={setSqlQuery}
                  onExecute={handleExecuteSql}
                  loading={sqlLoading}
                  placeholder="-- 输入 SQL 查询语句&#10;SELECT * FROM table_name LIMIT 100;"
                />
              </CardContent>
            </Card>

            {sqlError && (
              <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
                {sqlError}
              </div>
            )}

            {sqlResult && (
              <Card>
                <CardHeader className="pb-3">
                  <div className="flex justify-between items-center">
                    <CardTitle className="text-lg">执行结果</CardTitle>
                    <div className="flex items-center gap-4">
                      <div className="flex items-center gap-4 text-sm text-muted-foreground">
                        <span>{sqlResult.total.toLocaleString()} 行</span>
                        <span>{sqlResult.duration_ms}ms</span>
                      </div>
                      <Button 
                        variant="outline" 
                        size="sm" 
                        onClick={handleExport}
                        disabled={!sqlResult.rows.length}
                      >
                        <DownloadIcon className="h-4 w-4 mr-1" />
                        导出 CSV
                      </Button>
                    </div>
                  </div>
                </CardHeader>
                <CardContent>
                  {sqlResult.rows.length === 0 ? (
                    <div className="text-center text-muted-foreground py-8">
                      查询返回空结果
                    </div>
                  ) : (
                    <div className="border rounded-md overflow-hidden">
                      <ScrollArea className="max-h-[400px]">
                        <Table>
                          <TableHeader className="sticky top-0 bg-muted">
                            <TableRow>
                              {sqlResult.columns.map((col) => (
                                <TableHead key={col} className="font-medium whitespace-nowrap">
                                  {col}
                                </TableHead>
                              ))}
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {sqlResult.rows.map((row, i) => (
                              <TableRow key={i}>
                                {sqlResult.columns.map((col) => (
                                  <TableCell key={col} className="max-w-[200px] truncate">
                                    {row[col] === null || row[col] === undefined
                                      ? <span className="text-muted-foreground italic">NULL</span>
                                      : typeof row[col] === 'object'
                                        ? JSON.stringify(row[col])
                                        : String(row[col])}
                                  </TableCell>
                                ))}
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      </ScrollArea>
                    </div>
                  )}
                </CardContent>
              </Card>
            )}
          </div>

          <div className="lg:col-span-1">
            <Card className="h-full">
              <CardHeader className="pb-3">
                <div className="flex justify-between items-center">
                  <CardTitle className="text-lg flex items-center gap-2">
                    <CounterClockwiseClockIcon className="h-5 w-5" />
                    查询历史
                  </CardTitle>
                  {queryHistory.length > 0 && (
                    <Button 
                      variant="ghost" 
                      size="sm" 
                      onClick={handleClearHistory}
                      className="h-7 text-xs text-muted-foreground hover:text-destructive"
                    >
                      <TrashIcon className="h-3 w-3 mr-1" />
                      清空
                    </Button>
                  )}
                </div>
              </CardHeader>
              <CardContent>
                {queryHistory.length === 0 ? (
                  <div className="text-center text-muted-foreground py-8 text-sm">
                    暂无查询历史
                  </div>
                ) : (
                  <ScrollArea className="h-[500px]">
                    <div className="space-y-2 pr-2">
                      {queryHistory.map((item, index) => (
                        <button
                          key={index}
                          onClick={() => handleHistoryClick(item)}
                          className="w-full text-left p-3 rounded-md border border-border hover:border-primary hover:bg-muted/50 transition-colors"
                        >
                          <div className="flex items-start justify-between gap-2 mb-1">
                            <code className="text-xs font-mono text-foreground line-clamp-2 flex-1 break-all">
                              {item.sql}
                            </code>
                          </div>
                          <div className="flex items-center gap-3 text-xs text-muted-foreground">
                            <span className="flex items-center gap-1">
                              <ClockIcon className="h-3 w-3" />
                              {formatTime(item.executedAt)}
                            </span>
                            <span>{item.rowCount} 行</span>
                            <span>{item.durationMs}ms</span>
                          </div>
                        </button>
                      ))}
                    </div>
                  </ScrollArea>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </DashboardLayout>
  )
}
