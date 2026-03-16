'use client'

import { 
  Table, 
  TableBody, 
  TableCell, 
  TableHead, 
  TableHeader, 
  TableRow 
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { ChevronLeftIcon, ChevronRightIcon } from '@radix-ui/react-icons'

interface DataGridProps {
  columns: string[]
  rows: Record<string, unknown>[]
  total: number
  page: number
  pageSize: number
  onPageChange: (page: number) => void
  onRowClick?: (row: Record<string, unknown>) => void
  loading?: boolean
}

export function DataGrid({ 
  columns, 
  rows, 
  total, 
  page, 
  pageSize, 
  onPageChange, 
  onRowClick,
  loading = false
}: DataGridProps) {
  const totalPages = Math.ceil(total / pageSize)
  
  const formatCellValue = (value: unknown): string => {
    if (value === null || value === undefined) return ''
    if (typeof value === 'object') return JSON.stringify(value)
    return String(value)
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <div className="border rounded-md">
          <Table>
            <TableHeader>
              <TableRow>
                {columns.map((col) => (
                  <TableHead key={col}>{col}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
          </Table>
        </div>
        <div className="h-10 animate-pulse bg-muted rounded" />
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center rounded-lg border border-border bg-card py-12">
        <p className="text-foreground font-medium">暂无数据</p>
        <p className="text-sm text-muted-foreground mt-1">当前条件下没有匹配的记录</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="border rounded-md overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((col) => (
                <TableHead key={col} className="font-medium">
                  {col}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row, i) => (
              <TableRow 
                key={i} 
                onClick={() => onRowClick?.(row)} 
                className={onRowClick ? 'cursor-pointer' : ''}
              >
                {columns.map((col) => (
                  <TableCell key={col} className="max-w-xs truncate">
                    {formatCellValue(row[col])}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className="flex justify-between items-center">
        <span className="text-sm text-muted-foreground">
          共 {total.toLocaleString()} 条，第 {page}/{totalPages || 1} 页
        </span>
        <div className="flex gap-2">
          <Button 
            variant="outline" 
            size="sm" 
            onClick={() => onPageChange(page - 1)} 
            disabled={page <= 1}
          >
            <ChevronLeftIcon className="h-4 w-4" />
          </Button>
          <Button 
            variant="outline" 
            size="sm" 
            onClick={() => onPageChange(page + 1)} 
            disabled={page >= totalPages}
          >
            <ChevronRightIcon className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  )
}
