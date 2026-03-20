'use client'

import { useState, useEffect, useCallback } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
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
  LayersIcon,
} from '@radix-ui/react-icons'
import {
  BarChart,
  Bar,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
} from 'recharts'
import type { QueryResult, TableResult } from '@/lib/api/types'

const PIE_COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899', '#84cc16']
const CHART_COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444']

type ChartType = 'line' | 'bar' | 'none'

interface ChartConfig {
  type: ChartType
  xKey: string
  yKeys: string[]
  data: Record<string, unknown>[]
}

function detectChartType(result: QueryResult): ChartConfig {
  const { columns, rows } = result
  if (!columns.length || !rows.length) return { type: 'none', xKey: '', yKeys: [], data: [] }

  const numericCols: string[] = []
  const timeCols: string[] = []

  for (const col of columns) {
    const firstVal = rows[0][col]
    const nameHint = /time|date|hour|ts|timestamp/i.test(col)
    const isDateVal = typeof firstVal === 'string' && firstVal.length > 8 && !isNaN(Date.parse(firstVal))
    if (nameHint || isDateVal) {
      timeCols.push(col)
    } else if (typeof firstVal === 'number') {
      numericCols.push(col)
    }
  }

  if (timeCols.length > 0 && numericCols.length > 0) {
    return { type: 'line', xKey: timeCols[0], yKeys: numericCols.slice(0, 3), data: rows as Record<string, unknown>[] }
  }
  if (numericCols.length > 0 && rows.length <= 50) {
    const xKey = columns.find(c => !numericCols.includes(c)) || columns[0]
    return { type: 'bar', xKey, yKeys: numericCols.slice(0, 2), data: rows as Record<string, unknown>[] }
  }
  return { type: 'none', xKey: '', yKeys: [], data: [] }
}

function QueryResultChart({ config }: { config: ChartConfig }) {
  const formatXTick = (val: unknown) => {
    if (typeof val !== 'string') return String(val)
    const d = new Date(val)
    if (isNaN(d.getTime())) return val
    return d.toLocaleString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  if (config.type === 'line') {
    return (
      <ResponsiveContainer width="100%" height={280}>
        <LineChart data={config.data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey={config.xKey} tickFormatter={formatXTick} tick={{ fontSize: 11 }} />
          <YAxis tick={{ fontSize: 11 }} />
          <Tooltip />
          {config.yKeys.length > 1 && <Legend />}
          {config.yKeys.map((key, i) => (
            <Line key={key} type="monotone" dataKey={key} stroke={CHART_COLORS[i % CHART_COLORS.length]} dot={false} />
          ))}
        </LineChart>
      </ResponsiveContainer>
    )
  }

  if (config.type === 'bar') {
    return (
      <ResponsiveContainer width="100%" height={280}>
        <BarChart data={config.data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey={config.xKey} tick={{ fontSize: 11 }} />
          <YAxis tick={{ fontSize: 11 }} />
          <Tooltip />
          {config.yKeys.length > 1 && <Legend />}
          {config.yKeys.map((key, i) => (
            <Bar key={key} dataKey={key} fill={CHART_COLORS[i % CHART_COLORS.length]} />
          ))}
        </BarChart>
      </ResponsiveContainer>
    )
  }

  return null
}

const QUERY_TEMPLATES = [
  { name: '最新 100 条', sql: 'SELECT * FROM {table}\nORDER BY timestamp DESC\nLIMIT 100' },
  { name: '按小时统计', sql: "SELECT date_trunc('hour', timestamp) AS hour,\n  count(*) AS cnt\nFROM {table}\nGROUP BY 1\nORDER BY 1 DESC\nLIMIT 24" },
  { name: '字段值统计', sql: 'SELECT {col}, count(*) AS cnt\nFROM {table}\nGROUP BY 1\nORDER BY 2 DESC\nLIMIT 20' },
  { name: '数据时间范围', sql: 'SELECT min(timestamp) AS earliest,\n  max(timestamp) AS latest,\n  count(*) AS total\nFROM {table}' },
]

const HISTORY_KEY = 'miniodb-query-history'
const MAX_HISTORY = 20

interface QueryHistoryItem {
  sql: string
  executedAt: string
  rowCount: number
  durationMs: number
}

interface TableStats {
  name: string
  rowCount: number
  sizeBytes: number
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

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

function formatCount(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

export default function AnalyticsPage() {
  const [sqlQuery, setSqlQuery] = useState('')
  const [sqlResult, setSqlResult] = useState<QueryResult | null>(null)
  const [sqlLoading, setSqlLoading] = useState(false)
  const [sqlError, setSqlError] = useState('')
  const [queryHistory, setQueryHistory] = useState<QueryHistoryItem[]>([])
  const [viewMode, setViewMode] = useState<'table' | 'chart'>('table')
  const [chartConfig, setChartConfig] = useState<ChartConfig | null>(null)

  const [tables, setTables] = useState<TableResult[]>([])
  const [tableStats, setTableStats] = useState<TableStats[]>([])
  const [tablesLoading, setTablesLoading] = useState(false)
  const [selectedTable, setSelectedTable] = useState<string>('')

  useEffect(() => {
    setQueryHistory(loadHistory())
  }, [])

  useEffect(() => {
    const fetchTables = async () => {
      setTablesLoading(true)
      try {
        const tableList = await dataApi.listTables()
        setTables(tableList)
        if (tableList.length > 0 && !selectedTable) {
          setSelectedTable(tableList[0].name)
        }
        const stats: TableStats[] = []
        for (const t of tableList) {
          try {
            const detail = await dataApi.getTable(t.name)
            stats.push({
              name: t.name,
              rowCount: detail.row_count_est ?? 0,
              sizeBytes: detail.size_bytes ?? 0,
            })
          } catch {
            stats.push({ name: t.name, rowCount: 0, sizeBytes: 0 })
          }
        }
        setTableStats(stats)
      } catch (e) {
        console.error('Failed to fetch tables:', e)
      } finally {
        setTablesLoading(false)
      }
    }
    fetchTables()
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
      
      const config = detectChartType(result)
      setChartConfig(config)
      setViewMode(config.type !== 'none' ? 'chart' : 'table')
      
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

  const handleTemplateClick = (template: typeof QUERY_TEMPLATES[0]) => {
    const sql = template.sql.replace(/{table}/g, selectedTable)
    setSqlQuery(sql)
  }

  const toCsvCell = (value: unknown): string => {
    if (value === null || value === undefined) return ''
    if (typeof value === 'string') {
      const isoDateRe = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/
      if (isoDateRe.test(value)) {
        const d = new Date(value)
        if (!isNaN(d.getTime())) {
          value = d.toLocaleString('zh-CN', { hour12: false })
        }
      }
    }
    if (typeof value === 'number' || typeof value === 'boolean') {
      return String(value)
    }
    if (typeof value === 'object') {
      value = JSON.stringify(value)
    }
    const str = String(value)
    if (str.includes(',') || str.includes('"') || str.includes('\n')) {
      return `"${str.replace(/"/g, '""')}"`
    }
    return str
  }

  const handleExport = () => {
    if (!sqlResult || sqlResult.rows.length === 0) return
    
    const columns = sqlResult.columns
    const csvContent = [
      columns.join(','),
      ...sqlResult.rows.map(row => 
        columns.map(col => toCsvCell(row[col])).join(',')
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

  const totalTables = tableStats.length
  const totalRecords = tableStats.reduce((sum, t) => sum + t.rowCount, 0)
  const totalSize = tableStats.reduce((sum, t) => sum + t.sizeBytes, 0)

  const barChartData = tableStats
    .map(t => ({ name: t.name, count: t.rowCount }))
    .sort((a, b) => a.count - b.count)

  const pieChartData = tableStats
    .filter(t => t.sizeBytes > 0)
    .map(t => ({ name: t.name, value: t.sizeBytes }))

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div>
          <p className="text-sm text-muted-foreground">SQL 查询与数据分析</p>
        </div>

        <Tabs defaultValue="sql" className="w-full">
          <TabsList>
            <TabsTrigger value="sql">
              <PlayIcon className="h-4 w-4 mr-2" />
              SQL 查询
            </TabsTrigger>
            <TabsTrigger value="overview">
              <LayersIcon className="h-4 w-4 mr-2" />
              表概览
            </TabsTrigger>
          </TabsList>

          <TabsContent value="sql" className="mt-4">
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
                          {chartConfig && chartConfig.type !== 'none' && (
                            <Button 
                              variant="ghost" 
                              size="sm" 
                              onClick={() => setViewMode(v => v === 'chart' ? 'table' : 'chart')}
                            >
                              {viewMode === 'chart' ? '表格视图' : '图表视图'}
                            </Button>
                          )}
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
                      ) : viewMode === 'chart' && chartConfig && chartConfig.type !== 'none' ? (
                        <QueryResultChart config={chartConfig} />
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

              <div className="lg:col-span-1 space-y-4">
                <Card>
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium">查询模板</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {tables.length === 0 ? (
                      <p className="text-sm text-muted-foreground">暂无可用表</p>
                    ) : (
                      <>
                        <Select value={selectedTable} onValueChange={setSelectedTable}>
                          <SelectTrigger className="w-full">
                            <SelectValue placeholder="选择表" />
                          </SelectTrigger>
                          <SelectContent>
                            {tables.map(t => (
                              <SelectItem key={t.name} value={t.name}>{t.name}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <div className="space-y-2">
                          {QUERY_TEMPLATES.map((tpl) => (
                            <Button
                              key={tpl.name}
                              variant="outline"
                              size="sm"
                              className="w-full justify-start text-xs"
                              onClick={() => handleTemplateClick(tpl)}
                            >
                              {tpl.name}
                            </Button>
                          ))}
                        </div>
                      </>
                    )}
                  </CardContent>
                </Card>

                <Card className="flex-1">
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
                      <ScrollArea className="h-[300px]">
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
          </TabsContent>

          <TabsContent value="overview" className="mt-4">
            {tablesLoading ? (
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                {[1, 2, 3].map(i => (
                  <div key={i} className="h-24 rounded-lg border border-border bg-card animate-pulse" />
                ))}
              </div>
            ) : tableStats.length === 0 ? (
              <div className="text-center text-muted-foreground py-16">
                暂无数据表
              </div>
            ) : (
              <div className="space-y-6">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                  <Card>
                    <CardHeader className="pb-2">
                      <CardTitle className="text-sm font-medium text-muted-foreground">总表数</CardTitle>
                    </CardHeader>
                    <CardContent className="pt-0">
                      <p className="text-3xl font-bold">{totalTables}</p>
                    </CardContent>
                  </Card>
                  <Card>
                    <CardHeader className="pb-2">
                      <CardTitle className="text-sm font-medium text-muted-foreground">总记录数</CardTitle>
                    </CardHeader>
                    <CardContent className="pt-0">
                      <p className="text-3xl font-bold">{formatCount(totalRecords)}</p>
                    </CardContent>
                  </Card>
                  <Card>
                    <CardHeader className="pb-2">
                      <CardTitle className="text-sm font-medium text-muted-foreground">总存储大小</CardTitle>
                    </CardHeader>
                    <CardContent className="pt-0">
                      <p className="text-3xl font-bold">{formatBytes(totalSize)}</p>
                    </CardContent>
                  </Card>
                </div>

                <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                  <Card>
                    <CardHeader>
                      <CardTitle className="text-sm font-semibold">各表记录数</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <ResponsiveContainer width="100%" height={300}>
                        <BarChart data={barChartData} layout="vertical">
                          <CartesianGrid strokeDasharray="3 3" />
                          <XAxis type="number" tick={{ fontSize: 11 }} tickFormatter={formatCount} />
                          <YAxis 
                            dataKey="name" 
                            type="category" 
                            tick={{ fontSize: 11 }} 
                            width={100}
                          />
                          <Tooltip formatter={(value) => formatCount(Number(value))} />
                          <Bar dataKey="count" fill="#6366f1" />
                        </BarChart>
                      </ResponsiveContainer>
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader>
                      <CardTitle className="text-sm font-semibold">存储占比</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {pieChartData.length === 0 ? (
                        <div className="text-center text-muted-foreground py-12">
                          暂无存储数据
                        </div>
                      ) : (
                        <ResponsiveContainer width="100%" height={300}>
                          <PieChart>
                            <Pie
                              data={pieChartData}
                              cx="40%"
                              cy="50%"
                              innerRadius={60}
                              outerRadius={100}
                              paddingAngle={2}
                              dataKey="value"
                              label={({ name, percent }) => `${name} ${((percent ?? 0) * 100).toFixed(0)}%`}
                              labelLine={false}
                            >
                              {pieChartData.map((_, index) => (
                                <Cell key={`cell-${index}`} fill={PIE_COLORS[index % PIE_COLORS.length]} />
                              ))}
                            </Pie>
                            <Tooltip formatter={(value) => formatBytes(Number(value))} />
                          </PieChart>
                        </ResponsiveContainer>
                      )}
                    </CardContent>
                  </Card>
                </div>
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>
    </DashboardLayout>
  )
}
