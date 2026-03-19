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
  ArrowUpIcon,
  ArrowDownIcon,
} from '@radix-ui/react-icons'
import type { 
  TableResult, 
  TableDetailResult,
  BrowseResult, 
  QueryResult,
  CreateTableRequest,
  WriteRecordRequest,
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
  
  // 排序状态
  const [sortBy, setSortBy] = useState<string | null>(null)
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc' | null>(null)
  
  // SQL 控制台状态
  const [sqlQuery, setSqlQuery] = useState('')
  const [sqlResult, setSqlResult] = useState<QueryResult | null>(null)
  const [sqlLoading, setSqlLoading] = useState(false)
  const [sqlError, setSqlError] = useState('')
  
  // 编辑/新增对话框状态
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editingRow, setEditingRow] = useState<Record<string, unknown> | null>(null)
  const [editFormData, setEditFormData] = useState<Record<string, string>>({})
  const [newRecordId, setNewRecordId] = useState('')
  const [newRecordPayload, setNewRecordPayload] = useState('')
  const [newRecordError, setNewRecordError] = useState('')
  const [newRecordLoading, setNewRecordLoading] = useState(false)
  
  // 删除确认对话框
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deletingRowId, setDeletingRowId] = useState<string | null>(null)
  const [deleteLoading, setDeleteLoading] = useState(false)
  
  // 当前选中表的详细配置（含 id_strategy 等）
  const [tableDetail, setTableDetail] = useState<TableDetailResult | null>(null)

  // 创建表对话框状态
  const [createTableDialogOpen, setCreateTableDialogOpen] = useState(false)
  const [newTableName, setNewTableName] = useState('')
  const [createTableLoading, setCreateTableLoading] = useState(false)
  const [createTableError, setCreateTableError] = useState('')
  
  // 删除表确认对话框状态
  const [deleteTableDialogOpen, setDeleteTableDialogOpen] = useState(false)
  const [deleteTableLoading, setDeleteTableLoading] = useState(false)
  const [deleteTableConfirmName, setDeleteTableConfirmName] = useState('')

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
        sort_by: sortBy || undefined,
        sort_order: sortOrder || undefined,
      })
      setBrowseData(result)
    } catch (e) {
      setDataError(e instanceof Error ? e.message : '加载数据失败')
    } finally {
      setDataLoading(false)
    }
  }, [selectedTable, page, sortBy, sortOrder])

  // 初始加载
  useEffect(() => {
    loadTables()
  }, [loadTables])

  // 表切换或分页时加载数据
  useEffect(() => {
    if (viewMode === 'data' && selectedTable) {
      loadTableData()
    }
  }, [viewMode, selectedTable, page, sortBy, sortOrder, loadTableData])

  // 表切换时加载表详细配置（含 id_strategy）
  useEffect(() => {
    if (!selectedTable) {
      setTableDetail(null)
      return
    }
    dataApi.getTable(selectedTable)
      .then(detail => setTableDetail(detail))
      .catch(() => setTableDetail(null))
  }, [selectedTable])

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
    setSortBy(null)
    setSortOrder(null)
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

  // 处理排序
  const handleSort = (column: string) => {
    if (sortBy === column) {
      // 当前列已排序，切换排序方向或清除排序
      if (sortOrder === 'asc') {
        setSortOrder('desc')
      } else if (sortOrder === 'desc') {
        setSortBy(null)
        setSortOrder(null)
      }
    } else {
      // 切换到新列，默认升序
      setSortBy(column)
      setSortOrder('asc')
    }
    setPage(1) // 排序变化时重置到第一页
  }

  // 渲染排序指示器
  const renderSortIndicator = (column: string) => {
    if (sortBy !== column) {
      return null
    }
    return sortOrder === 'asc' 
      ? <ArrowUpIcon className="h-3.5 w-3.5 ml-1 inline" />
      : <ArrowDownIcon className="h-3.5 w-3.5 ml-1 inline" />
  }

  // 打开新增对话框
  const handleAddRow = () => {
    setEditingRow(null)
    setNewRecordId('')
    setNewRecordPayload('{\n  \n}')
    setNewRecordError('')
    setEditDialogOpen(true)
  }

  // 打开编辑对话框
  const handleEditRow = (row: Record<string, unknown>) => {
    setEditingRow(row)
    const formData: Record<string, string> = {}
    Object.entries(row).forEach(([key, value]) => {
      if (key === 'payload') {
        if (typeof value === 'string') {
          formData[key] = value
        } else if (value === null || value === undefined) {
          formData[key] = '{}'
        } else {
          try {
            formData[key] = JSON.stringify(value, null, 2)
          } catch {
            formData[key] = String(value)
          }
        }
      } else {
        formData[key] = value === null || value === undefined ? '' : String(value)
      }
    })
    setNewRecordError('')
    setEditFormData(formData)
    setEditDialogOpen(true)
  }

  // 保存编辑
  const handleSaveEdit = async () => {
    if (!selectedTable) return
    
    try {
      if (editingRow) {
        // 编辑模式
        const rowId = editingRow.id !== null && editingRow.id !== undefined ? String(editingRow.id).trim() : ''
        if (!rowId) {
          setNewRecordError('该记录缺少 ID，无法更新。请删除后重新写入。')
          return
        }

        let payload: Record<string, unknown> = {}
        const payloadText = editFormData.payload?.trim() || '{}'
        try {
          const parsed = JSON.parse(payloadText)
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            payload = parsed as Record<string, unknown>
          } else {
            throw new Error('Payload 必须是 JSON 对象')
          }
        } catch (e) {
          setNewRecordError(e instanceof Error ? e.message : 'Payload JSON 格式错误')
          return
        }
        
        await dataApi.updateRecord(selectedTable, rowId, {
          payload,
        })
      } else {
        // 新增模式
        const strategy = tableDetail?.id_strategy ?? 'snowflake'
        const autoGenerate = tableDetail?.auto_generate_id ?? true
        const isRequired = strategy === 'user_provided' || !autoGenerate
        if (isRequired && !newRecordId.trim()) {
          setNewRecordError('该表要求手动指定 ID，请填写 ID 后再提交')
          return
        }

        let payload: Record<string, unknown> = {}
        try {
          const parsed = JSON.parse(newRecordPayload || '{}')
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            payload = parsed as Record<string, unknown>
          } else {
            throw new Error('Payload 必须是 JSON 对象')
          }
        } catch (e) {
          setNewRecordError(e instanceof Error ? e.message : 'Payload JSON 格式错误')
          return
        }

        const recordData: WriteRecordRequest = { payload }
        if (newRecordId.trim()) {
          recordData.id = newRecordId.trim()
        }
        
        await dataApi.writeRecord(selectedTable, recordData)
      }
      
      setEditDialogOpen(false)
      setNewRecordId('')
      setNewRecordPayload('{\n  \n}')
      setNewRecordError('')
      loadTableData()
    } catch (e) {
      console.error('Save failed:', e)
      setNewRecordError(e instanceof Error ? e.message : '保存失败')
    }
  }

  // 打开删除确认
  const handleDeleteClick = (row: Record<string, unknown>) => {
    if (row.id === null || row.id === undefined || String(row.id).trim() === '') {
      console.error('Delete failed: missing row id', row)
      return
    }
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
  // 将任意值序列化为 CSV 单元格字符串
  const toCsvCell = (value: unknown): string => {
    if (value === null || value === undefined) return ''
    // 日期字符串识别：ISO 8601 格式
    if (typeof value === 'string') {
      const isoDateRe = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/
      if (isoDateRe.test(value)) {
        const d = new Date(value)
        if (!isNaN(d.getTime())) {
          value = d.toLocaleString('zh-CN', { hour12: false })
        }
      }
    }
    // 数字/布尔直接转字符串
    if (typeof value === 'number' || typeof value === 'boolean') {
      return String(value)
    }
    // 对象（含 payload）序列化为 JSON
    if (typeof value === 'object') {
      value = JSON.stringify(value)
    }
    const str = String(value)
    // CSV 转义：含逗号、双引号或换行时用双引号包裹，内部双引号加倍
    if (str.includes(',') || str.includes('"') || str.includes('\n')) {
      return `"${str.replace(/"/g, '""')}"`
    }
    return str
  }

  const handleExport = () => {
    if (!browseData || browseData.rows.length === 0) return
    
    const columns = Object.keys(browseData.rows[0])
    const csvContent = [
      columns.join(','),
      ...browseData.rows.map(row => 
        columns.map(col => toCsvCell(row[col])).join(',')
      )
    ].join('\n')
    
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `${selectedTable || 'export'}_${new Date().toISOString().split('T')[0]}.csv`
    link.click()
    URL.revokeObjectURL(link.href)
  }

  // 打开创建表对话框
  const handleOpenCreateTable = () => {
    setNewTableName('')
    setCreateTableError('')
    setCreateTableDialogOpen(true)
  }

  // 创建表
  const handleCreateTable = async () => {
    if (!newTableName.trim()) {
      setCreateTableError('请输入表名')
      return
    }
    
    setCreateTableLoading(true)
    setCreateTableError('')
    try {
      const req: CreateTableRequest = {
        table_name: newTableName.trim(),
        config: {}
      }
      await dataApi.createTable(req)
      setCreateTableDialogOpen(false)
      // 刷新表列表并选中新表
      const result = await dataApi.listTables()
      setTables(result)
      setSelectedTable(newTableName.trim())
    } catch (e) {
      setCreateTableError(e instanceof Error ? e.message : '创建表失败')
    } finally {
      setCreateTableLoading(false)
    }
  }

  // 打开删除表确认
  const handleOpenDeleteTable = () => {
    setDeleteTableDialogOpen(true)
  }

  // 确认删除表
  const handleConfirmDeleteTable = async () => {
    if (!selectedTable) return
    
    setDeleteTableLoading(true)
    try {
      await dataApi.deleteTable(selectedTable)
      setDeleteTableDialogOpen(false)
      // 刷新表列表
      const result = await dataApi.listTables()
      setTables(result)
      // 选择第一个表或清空选择
      if (result.length > 0) {
        setSelectedTable(result[0].name)
      } else {
        setSelectedTable(null)
        setBrowseData(null)
      }
    } catch (e) {
      console.error('Delete table failed:', e)
    } finally {
      setDeleteTableLoading(false)
    }
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
      <div className="relative flex h-[calc(100vh-4rem)] -m-4">
        {/* 左侧表列表侧边栏 */}
        <div 
          className={`shrink-0 border-r border-border bg-card transition-all duration-300 ${
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
            
            {/* 表管理按钮 */}
            <div className="flex gap-2 mb-4">
              <Button 
                variant="outline" 
                size="sm" 
                className="flex-1"
                onClick={handleOpenCreateTable}
              >
                <PlusIcon className="h-4 w-4 mr-1" />
                新建表
              </Button>
              {selectedTable && (
                <Button 
                  variant="outline" 
                  size="sm"
                  className="text-destructive hover:text-destructive"
                  onClick={handleOpenDeleteTable}
                >
                  <TrashIcon className="h-4 w-4" />
                </Button>
              )}
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

        {/* 侧边栏收起后：左侧展开条，点击可再次展示表列表 */}
        {!sidebarOpen && (
          <button
            type="button"
            onClick={() => setSidebarOpen(true)}
            className="absolute left-0 top-0 z-20 flex h-full w-9 flex-col items-center justify-center border-r border-border bg-card text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-accent-foreground"
            title="展开表列表"
            aria-label="展开表列表"
          >
            <ChevronRightIcon className="h-5 w-5" />
          </button>
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
                                <TableHead 
                                  key={col}
                                  className="cursor-pointer hover:bg-muted/50 select-none"
                                  onClick={() => handleSort(col)}
                                >
                                  {col}
                                  {renderSortIndicator(col)}
                                </TableHead>
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
                                 {Object.entries(row).map(([key, value]) => {
                                   let display = ''
                                   if (value === null || value === undefined) {
                                     display = ''
                                   } else if (typeof value === 'object') {
                                     display = JSON.stringify(value)
                                   } else {
                                     display = String(value)
                                   }
                                   const truncated = display.length > 80 ? display.slice(0, 80) + '…' : display
                                   return (
                                     <TableCell
                                       key={key}
                                       className="max-w-xs"
                                       title={display.length > 80 ? display : undefined}
                                     >
                                       <span className="block truncate font-mono text-xs">
                                         {truncated}
                                       </span>
                                     </TableCell>
                                   )
                                 })}
                                <TableCell>
                                  <div className="flex gap-1">
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      className="h-7 w-7"
                                      disabled={row.id === null || row.id === undefined || String(row.id).trim() === ''}
                                      title={row.id === null || row.id === undefined || String(row.id).trim() === '' ? '该记录缺少 ID，无法编辑' : undefined}
                                      onClick={(e) => {
                                        e.preventDefault()
                                        e.stopPropagation()
                                        handleEditRow(row)
                                      }}
                                    >
                                      <Pencil1Icon className="h-3.5 w-3.5" />
                                    </Button>
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon"
                                      className="h-7 w-7 text-destructive hover:text-destructive"
                                      onClick={(e) => {
                                        e.preventDefault()
                                        e.stopPropagation()
                                        handleDeleteClick(row)
                                      }}
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
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              {editingRow ? '编辑记录' : '新增记录'}
            </DialogTitle>
          </DialogHeader>
          {editingRow ? (
            // 编辑模式：显示各字段输入框
            <div className="space-y-4 py-4 max-h-[60vh] overflow-y-auto">
              {newRecordError && (
                <p className="text-sm text-destructive">{newRecordError}</p>
              )}
              {/* 只读头部：表名 + ID */}
              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">表</label>
                  <input
                    type="text"
                    value={selectedTable || ''}
                    disabled
                    className="w-full px-3 py-2 border border-input rounded-md bg-muted text-muted-foreground"
                  />
                </div>
                <div className="space-y-2 col-span-2">
                  <label className="text-sm font-medium text-foreground">ID（不可修改）</label>
                  <input
                    type="text"
                    value={String(editingRow.id ?? '')}
                    disabled
                    className="w-full px-3 py-2 border border-input rounded-md bg-muted text-muted-foreground font-mono text-sm"
                  />
                </div>
              </div>
              {Object.entries(editFormData).map(([key, value]) => (
                key !== 'id' && key !== 'timestamp' && key !== 'table' && (
                  <div key={key} className="space-y-2">
                    <label className="text-sm font-medium text-foreground">
                      {key}
                    </label>
                    {key === 'payload' ? (
                      <textarea
                        value={value}
                        onChange={(e) => {
                          setNewRecordError('')
                          setEditFormData(prev => ({
                            ...prev,
                            [key]: e.target.value
                          }))
                        }}
                        rows={8}
                        className="w-full px-3 py-2 border border-input rounded-md bg-background text-foreground font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary resize-none"
                      />
                    ) : (
                      <input
                        type="text"
                        value={value}
                        onChange={(e) => setEditFormData(prev => ({
                          ...prev,
                          [key]: e.target.value
                        }))}
                        className="w-full px-3 py-2 border border-input rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
                      />
                    )}
                  </div>
                )
              ))}
              {/* 数据预览 */}
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  完整数据预览
                </label>
                <pre className="p-3 bg-muted rounded-md text-xs font-mono overflow-x-auto text-foreground">
                  {JSON.stringify({
                    table: selectedTable,
                    id: String(editingRow.id ?? ''),
                    payload: (() => {
                      try {
                        const raw = editFormData.payload?.trim() || '{}'
                        return JSON.parse(raw)
                      } catch {
                        return '(JSON 格式错误)'
                      }
                    })()
                  }, null, 2)}
                </pre>
              </div>
            </div>
          ) : (
            // 新增模式：ID + JSON Payload 输入 + 预览
            <div className="space-y-4 py-4">
              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">
                    表
                  </label>
                  <input
                    type="text"
                    value={selectedTable || ''}
                    disabled
                    className="w-full px-3 py-2 border border-input rounded-md bg-muted text-muted-foreground"
                  />
                </div>
                <div className="space-y-2 col-span-2">
                  {(() => {
                    const strategy = tableDetail?.id_strategy ?? 'snowflake'
                    const autoGenerate = tableDetail?.auto_generate_id ?? true
                    const isRequired = strategy === 'user_provided' || !autoGenerate
                    const strategyLabel: Record<string, string> = {
                      uuid: 'UUID',
                      snowflake: 'Snowflake',
                      custom: '自定义',
                      user_provided: '用户提供',
                    }
                    const hint = isRequired
                      ? `必填 — 该表要求手动指定 ID（策略：${strategyLabel[strategy] ?? strategy}）`
                      : `可选 — 留空将自动生成（策略：${strategyLabel[strategy] ?? strategy}）`
                    return (
                      <>
                        <label className="text-sm font-medium text-foreground">
                          ID{isRequired ? <span className="text-destructive ml-1">*</span> : <span className="text-muted-foreground text-xs ml-1">（可选）</span>}
                        </label>
                        <input
                          type="text"
                          value={newRecordId}
                          onChange={(e) => setNewRecordId(e.target.value)}
                          placeholder={isRequired ? '请输入 ID...' : '留空则自动生成...'}
                          className={`w-full px-3 py-2 border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary ${isRequired && !newRecordId.trim() ? 'border-destructive' : 'border-input'}`}
                        />
                        <p className="text-xs text-muted-foreground">{hint}</p>
                      </>
                    )
                  })()}
                </div>
              </div>
              
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  Payload（JSON格式）
                </label>
                <textarea
                  value={newRecordPayload}
                  onChange={(e) => {
                    setNewRecordPayload(e.target.value)
                    setNewRecordError('')
                  }}
                  placeholder={'{\n  "name": "张三",\n  "age": 25\n}'}
                  rows={6}
                  className="w-full px-3 py-2 border border-input rounded-md bg-background text-foreground font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary resize-none"
                />
                {newRecordError && (
                  <p className="text-sm text-destructive">{newRecordError}</p>
                )}
              </div>
              
              {/* 数据预览 */}
              <div className="space-y-2">
                <label className="text-sm font-medium text-foreground">
                  完整数据预览
                </label>
                <pre className="p-3 bg-muted rounded-md text-xs font-mono overflow-x-auto text-foreground">
                  {JSON.stringify({
                    table: selectedTable,
                    ...(newRecordId.trim() ? { id: newRecordId.trim() } : {}),
                    payload: (() => {
                      try {
                        return newRecordPayload ? JSON.parse(newRecordPayload) : {}
                      } catch {
                        return '(JSON 格式错误)'
                      }
                    })()
                  }, null, 2)}
                </pre>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleSaveEdit}>
              {editingRow ? '保存' : '提交'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认对话框 */}
      <Dialog
        open={deleteDialogOpen}
        onOpenChange={(open) => {
          setDeleteDialogOpen(open)
          if (!open) {
            setDeletingRowId(null)
          }
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            确定要删除这条记录吗？此操作无法撤销。
          </p>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              取消
            </Button>
            <Button 
              type="button"
              variant="destructive" 
              onClick={handleConfirmDelete}
              disabled={deleteLoading}
            >
              {deleteLoading ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 创建表对话框 */}
      <Dialog open={createTableDialogOpen} onOpenChange={setCreateTableDialogOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>新建数据表</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium text-foreground">
                表名
              </label>
              <input
                type="text"
                value={newTableName}
                onChange={(e) => setNewTableName(e.target.value)}
                placeholder="输入表名..."
                className="w-full px-3 py-2 border border-input rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    handleCreateTable()
                  }
                }}
              />
              {createTableError && (
                <p className="text-sm text-destructive">{createTableError}</p>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateTableDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreateTable} disabled={createTableLoading}>
              {createTableLoading ? '创建中...' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除表确认对话框 */}
      <Dialog open={deleteTableDialogOpen} onOpenChange={(open) => {
        setDeleteTableDialogOpen(open)
        if (!open) setDeleteTableConfirmName('')
      }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="text-destructive">确认删除表</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">
              此操作将永久删除表 <strong className="text-foreground">{selectedTable}</strong> 及其所有数据，且无法撤销。
            </p>
            <div className="rounded-md bg-destructive/10 border border-destructive/20 p-3">
              <p className="text-sm text-destructive font-medium">
                请输入表名 <code className="px-1 py-0.5 bg-destructive/20 rounded">{selectedTable}</code> 以确认删除：
              </p>
            </div>
            <input
              type="text"
              value={deleteTableConfirmName}
              onChange={(e) => setDeleteTableConfirmName(e.target.value)}
              placeholder="输入表名确认..."
              className="w-full px-3 py-2 border border-input rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-destructive"
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTableDialogOpen(false)}>
              取消
            </Button>
            <Button 
              variant="destructive" 
              onClick={handleConfirmDeleteTable}
              disabled={deleteTableLoading || deleteTableConfirmName !== selectedTable}
            >
              {deleteTableLoading ? '删除中...' : '确认删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </DashboardLayout>
  )
}
