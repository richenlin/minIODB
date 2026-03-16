'use client'

import { useEffect, useState, useCallback } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Button } from '@/components/ui/button'
import { 
  Table, 
  TableBody, 
  TableCell, 
  TableHead, 
  TableHeader, 
  TableRow 
} from '@/components/ui/table'
import { 
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { 
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { dataApi } from '@/lib/api/data'
import { SqlEditor } from '@/components/code-editor/sql-editor'
import { DataGrid } from '@/components/data-grid'
import { 
  LayersIcon,
  PlayIcon,
  PlusIcon,
  Pencil1Icon,
  TrashIcon,
  DownloadIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ReloadIcon,
} from '@radix-ui/react-icons'
import type { 
  TableResult, 
  BrowseResult, 
  QueryResult 
} from '@/lib/api/types'

type ViewMode = 'data' | 'sql'

export default function DataPage() {
  // 表列表状态
  const [tables, setTables] = useState<TableResult[]>([])
  const [tablesLoading, setTablesLoading] = useState(true)
  const [tablesError, setTablesError] = useState('')
  
  // 侧边栏状态
  const [sidebarOpen, setSidebarOpen] = useState(true)
  
  // 当前选中的表
  const [selectedTable, setSelectedTable] = useState<string | null>(null)
  
  // 视图模式
  const [viewMode, setViewMode] = useState<ViewMode>('data')
  
  // 数据网格状态
  const [browseData, setBrowseData] = useState<BrowseResult | null>(null)
  const [dataLoading, setDataLoading] = useState(false)
  const [dataError, setDataError] = useState('')
  const [page, setPage] = useState(1)
  const pageSize = 20
  
  // SQL 控制台状态
  const [sqlQuery, setSqlQuery] = useState('')
  const [sqlResult, setSqlResult] = useState<QueryResult | null>(null)
  const [sqlLoading, setSqlLoading] = useState(false)
  const [sqlError, setSqlError] = useState('')
  
  // 编辑/新增对话框状态
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editingRow, setEditingRow] = useState<Record<string, unknown> | null>(null)
  const [editFormData, setEditFormData] = useState<Record<string, string>>({})
  
  // 删除确认对话框
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deletingRowId, setDeletingRowId] = useState<string | null>(null)
  const [deleteLoading, setDeleteLoading] = useState(false)

  // 加载表列表
  const loadTables = useCallback(async () => {
    setTablesLoading(true)
    setTablesError('')
    try {
      const result = await dataApi.listTables()
      setTables(result)
      if (result.length > 0 && !selectedTable) {
        setSelectedTable(result[0].name)
      }
    } catch (e) {
      setTablesError(e instanceof Error ? e.message : '加载表列表失败')
    } finally {
      setTablesLoading(false)
    }
  }, [selectedTable])

  // 加载表数据
  const loadTableData = useCallback(async () => {
    if (!selectedTable) return
    
    setDataLoading(true)
    setDataError('')
    try {
      const result = await dataApi.browseData(selectedTable, {
        page,
        page_size: pageSize,
      })
      setBrowseData(result)
    } catch (e) {
      setDataError(e instanceof Error ? e.message : '加载数据失败')
    } finally {
      setDataLoading(false)
    }
  }, [selectedTable, page])

  // 初始加载
  useEffect(() => {
    loadTables()
  }, [loadTables])

  // 表切换或分页时加载数据
  useEffect(() => {
    if (viewMode === 'data' && selectedTable) {
      loadTableData()
    }
  }, [viewMode, selectedTable, page, loadTableData])

  // 执行 SQL
  const handleExecuteSql = async () => {
    if (!sqlQuery.trim()) return
    
    setSqlLoading(true)
    setSqlError('')
    setSqlResult(null)
    try {
      const result = await dataApi.querySQL(sqlQuery)
      setSqlResult(result)
    } catch (e) {
      setSqlError(e instanceof Error ? e.message : '执行 SQL 失败')
    } finally {
      setSqlLoading(false)
    }
  }

  // 切换表
  const handleTableSelect = (tableName: string) => {
    setSelectedTable(tableName)
    setPage(1)
    setViewMode('data')
  }

  // 刷新数据
  const handleRefresh = () => {
    if (viewMode === 'data') {
      loadTableData()
    } else {
      handleExecuteSql()
    }
  }

  // 打开新增对话框
  const handleAddRow = () => {
    setEditingRow(null)
    setEditFormData({})
    setEditDialogOpen(true)
  }

  // 打开编辑对话框
  const handleEditRow = (row: Record<string, unknown>) => {
    setEditingRow(row)
    const formData: Record<string, string> = {}
    Object.entries(row).forEach(([key, value]) => {
      formData[key] = value === null || value === undefined ? '' : String(value)
    })
    setEditFormData(formData)
    setEditDialogOpen(true)
  }

  // 保存编辑
  const handleSaveEdit = async () => {
    if (!selectedTable) return
    
    try {
      const payload: Record<string, unknown> = {}
      Object.entries(editFormData).forEach(([key, value]) => {
        if (key !== 'id' && key !== 'timestamp') {
          payload[key] = value
        }
      })
      
      if (editingRow?.id) {
        await dataApi.updateRecord(selectedTable, String(editingRow.id), {
          payload,
        })
      } else {
        await dataApi.writeRecord(selectedTable, {
          payload,
        })
      }
      
      setEditDialogOpen(false)
      loadTableData()
    } catch (e) {
      console.error('Save failed:', e)
    }
  }

  // 打开删除确认
  const handleDeleteClick = (row: Record<string, unknown>) => {
    setDeletingRowId(String(row.id))
    setDeleteDialogOpen(true)
  }

  // 确认删除
  const handleConfirmDelete = async () => {
    if (!selectedTable || !deletingRowId) return
    
    setDeleteLoading(true)
    try {
      await dataApi.deleteRecord(selectedTable, deletingRowId)
      setDeleteDialogOpen(false)
      loadTableData()
    } catch (e) {
      console.error('Delete failed:', e)
    } finally {
      setDeleteLoading(false)
    }
  }

  // 导出数据
  const handleExport = () => {
    if (!browseData || browseData.rows.length === 0) return
    
    const columns = Object.keys(browseData.rows[0])
    const csvContent = [
      columns.join(','),
      ...browseData.rows.map(row => 
        columns.map(col => {
          const value = row[col]
          if (value === null || value === undefined) return ''
          const str = String(value)
          return str.includes(',') ? `"${str}"` : str
        }).join(',')
      )
    ].join('\n')
    
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `${selectedTable || 'export'}_${new Date().toISOString().split('T')[0]}.csv`
    link.click()
    URL.revokeObjectURL(link.href)
  }

  // 格式化字节
  const formatBytes = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
  }

  return (
    <DashboardLayout>
      <div className="flex h-[calc(100vh-4rem)] -m-4">
        {/* 左侧表列表侧边栏 */}
        <div 
          className={`border-r border-border bg-card transition-all duration-300 ${
            sidebarOpen ? 'w-64' : 'w-0 overflow-hidden'
          }`}
        >
          <div className="p-4">
            <div className="flex items-center justify-between mb-4">
              <h2 className="font-semibold text-foreground flex items-center gap-2">
                <LayersIcon className="h-4 w-4" />
                数据表
              </h2>
              <Button 
                variant="ghost" 
                size="icon" 
                onClick={() => setSidebarOpen(false)}
                className="h-6 w-6"
              >
                <ChevronLeftIcon className="h-4 w-4" />
              </Button>
            </div>
            
            {tablesLoading ? (
              <div className="space-y-2">
                {[1, 2, 3].map(i => (
                  <div key={i} className="h-10 bg-muted animate-pulse rounded" />
                ))}
              </div>
            ) : tablesError ? (
              <div className="text-sm text-destructive">{tablesError}</div>
            ) : tables.length === 0 ? (
              <div className="text-sm text-muted-foreground text-center py-4">
                暂无数据表
              </div>
            ) : (
              <div className="space-y-1">
                {tables.map(table => (
                  <button
                    key={table.name}
                    onClick={() => handleTableSelect(table.name)}
                    className={`w-full text-left px-3 py-2 rounded-md text-sm transition-colors ${
                      selectedTable === table.name
                        ? 'bg-primary text-primary-foreground'
                        : 'hover:bg-muted text-foreground'
                    }`}
                  >
                    <div className="font-medium truncate">{table.name}</div>
                    <div className={`text-xs ${
                      selectedTable === table.name 
                        ? 'text-primary-foreground/70' 
                        : 'text-muted-foreground'
                    }`}>
                      {table.row_count_est?.toLocaleString() ?? 0} 行 · {formatBytes(table.size_bytes ?? 0)}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* 折叠按钮 */}
        {!sidebarOpen && (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setSidebarOpen(true)}
            className="absolute left-2 top-20 z-10 h-8 w-8"
          >
            <ChevronRightIcon className="h-4 w-4" />
          </Button>
        )}

        {/* 右侧内容区 */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {/* 顶部工具栏 */}
          <div className="border-b border-border bg-card p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <h1 className="text-xl font-bold text-foreground">
                  {selectedTable || '数据管理'}
                </h1>
                <div className="flex rounded-md border border-border overflow-hidden">
                  <button
                    onClick={() => setViewMode('data')}
                    className={`px-3 py-1.5 text-sm font-medium transition-colors ${
                      viewMode === 'data'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-background hover:bg-muted'
                    }`}
                  >
                    <LayersIcon className="h-4 w-4 inline mr-1" />
                    数据浏览
                  </button>
                  <button
                    onClick={() => setViewMode('sql')}
                    className={`px-3 py-1.5 text-sm font-medium transition-colors ${
                      viewMode === 'sql'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-background hover:bg-muted'
                    }`}
                  >
                    <PlayIcon className="h-4 w-4 inline mr-1" />
                    SQL 控制台
                  </button>
                </div>
              </div>
              
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={handleRefresh}>
                  <ReloadIcon className="h-4 w-4 mr-1" />
                  刷新
                </Button>
                {viewMode === 'data' && selectedTable && (
                  <>
                    <Button variant="outline" size="sm" onClick={handleAddRow}>
                      <PlusIcon className="h-4 w-4 mr-1" />
                      新增
                    </Button>
                    <Button variant="outline" size="sm" onClick={handleExport} disabled={!browseData?.rows.length}>
                      <DownloadIcon className="h-4 w-4 mr-1" />
                      导出
                    </Button>
                  </>
                )}
              </div>
            </div>
          </div>

          {/* 主内容区 */}
          <div className="flex-1 overflow-auto p-4">
            {viewMode === 'sql' ? (
              // SQL 控制台模式
              <div className="space-y-4">
                <SqlEditor
                  value={sqlQuery}
                  onChange={setSqlQuery}
                  onExecute={handleExecuteSql}
                  loading={sqlLoading}
                  placeholder={`-- 输入 SQL 查询语句
SELECT * FROM ${selectedTable || 'table_name'} LIMIT 100;`}
                />
                
                {sqlError && (
                  <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
                    {sqlError}
                  </div>
                )}
                
                {sqlResult && (
                  <div className="space-y-2">
                    <div className="flex justify-between items-center text-sm text-muted-foreground">
                      <span>共 {sqlResult.total} 条结果</span>
                      <span>耗时 {sqlResult.duration_ms}ms</span>
                    </div>
                    <DataGrid
                      columns={sqlResult.columns}
                      rows={sqlResult.rows}
                      total={sqlResult.total}
                      page={1}
                      pageSize={sqlResult.rows.length}
                      onPageChange={() => {}}
                      onRowClick={handleEditRow}
                    />
                  </div>
                )}
              </div>
            ) : (
              // 数据浏览模式
              <div className="space-y-4">
                {dataError && (
                  <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
                    {dataError}
                  </div>
                )}
                
                {!selectedTable ? (
                  <div className="flex flex-col items-center justify-center rounded-lg border border-border bg-card py-16">
                    <LayersIcon className="h-12 w-12 text-muted-foreground mb-4" />
                    <p className="text-foreground font-medium">请选择一个数据表</p>
                    <p className="text-sm text-muted-foreground mt-1">
                      从左侧列表选择表来浏览数据
                    </p>
                  </div>
                ) : dataLoading && !browseData ? (
                  <div className="flex items-center justify-center py-16">
                    <ReloadIcon className="h-8 w-8 animate-spin text-muted-foreground" />
                  </div>
                ) : browseData ? (
                  <div className="space-y-4">
                    {/* 数据表格 */}
                    <div className="border rounded-md overflow-hidden">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead className="w-12">#</TableHead>
                            {browseData.rows.length > 0 && 
                              Object.keys(browseData.rows[0]).map(col => (
                                <TableHead key={col}>{col}</TableHead>
                              ))
                            }
                            <TableHead className="w-24">操作</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {browseData.rows.length === 0 ? (
                            <TableRow>
                              <TableCell 
                                colSpan={browseData.rows.length > 0 ? Object.keys(browseData.rows[0]).length + 2 : 2}
                                className="text-center text-muted-foreground py-8"
                              >
                                暂无数据
                              </TableCell>
                            </TableRow>
                          ) : (
                            browseData.rows.map((row, i) => (
                              <TableRow key={i}>
                                <TableCell className="text-muted-foreground">
                                  {(page - 1) * pageSize + i + 1}
                                </TableCell>
                                {Object.entries(row).map(([key, value]) => (
                                  <TableCell key={key} className="max-w-xs truncate">
                                    {value === null || value === undefined 
                                      ? '' 
                                      : typeof value === 'object' 
                                        ? JSON.stringify(value) 
                                        : String(value)
                                    }
                                  </TableCell>
                                ))}
                                <TableCell>
                                  <div className="flex gap-1">
                                    <Button
                                      variant="ghost"
                                      size="icon"
                                      className="h-7 w-7"
                                      onClick={() => handleEditRow(row)}
                                    >
                                      <Pencil1Icon className="h-3.5 w-3.5" />
                                    </Button>
                                    <Button
                                      variant="ghost"
                                      size="icon"
                                      className="h-7 w-7 text-destructive hover:text-destructive"
                                      onClick={() => handleDeleteClick(row)}
                                    >
                                      <TrashIcon className="h-3.5 w-3.5" />
                                    </Button>
                                  </div>
                                </TableCell>
                              </TableRow>
                            ))
                          )}
                        </TableBody>
                      </Table>
                    </div>
                    
                    {/* 分页 */}
                    <div className="flex justify-between items-center">
                      <span className="text-sm text-muted-foreground">
                        共 {browseData.total.toLocaleString()} 条，第 {page}/{Math.ceil(browseData.total / pageSize) || 1} 页
                      </span>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setPage(p => Math.max(1, p - 1))}
                          disabled={page <= 1}
                        >
                          <ChevronLeftIcon className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setPage(p => p + 1)}
                          disabled={page >= Math.ceil(browseData.total / pageSize)}
                        >
                          <ChevronRightIcon className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                  </div>
                ) : null}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* 编辑/新增对话框 */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {editingRow ? '编辑记录' : '新增记录'}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4 max-h-[60vh] overflow-y-auto">
            {Object.entries(editFormData).map(([key, value]) => (
              key !== 'id' && key !== 'timestamp' && (
                <div key={key} className="space-y-2">
                  <label className="text-sm font-medium text-foreground">
                    {key}
                  </label>
                  <input
                    type="text"
                    value={value}
                    onChange={(e) => setEditFormData(prev => ({
                      ...prev,
                      [key]: e.target.value
                    }))}
                    className="w-full px-3 py-2 border border-input rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
                  />
                </div>
              )
            ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleSaveEdit}>
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认对话框 */}
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            确定要删除这条记录吗？此操作无法撤销。
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              取消
            </Button>
            <Button 
              variant="destructive" 
              onClick={handleConfirmDelete}
              disabled={deleteLoading}
            >
              {deleteLoading ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </DashboardLayout>
  )
}
