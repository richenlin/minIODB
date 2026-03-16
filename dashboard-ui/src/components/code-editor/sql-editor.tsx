'use client'

import { useState } from 'react'
import { Button } from '@/components/ui/button'

interface SqlEditorProps {
  value: string
  onChange: (v: string) => void
  onExecute: () => void
  placeholder?: string
  loading?: boolean
}

export function SqlEditor({ 
  value, 
  onChange, 
  onExecute, 
  placeholder = 'SELECT * FROM table_name LIMIT 100',
  loading = false
}: SqlEditorProps) {
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      onExecute()
    }
  }

  return (
    <div className="space-y-2">
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        disabled={loading}
        className="w-full h-32 p-3 font-mono text-sm border rounded-md resize-none bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent disabled:opacity-50"
        spellCheck={false}
      />
      <div className="flex justify-between items-center">
        <span className="text-xs text-muted-foreground">
          Ctrl/Cmd + Enter 执行
        </span>
        <Button onClick={onExecute} size="sm" disabled={loading || !value.trim()}>
          {loading ? '执行中...' : '执行'}
        </Button>
      </div>
    </div>
  )
}
