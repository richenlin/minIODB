'use client'

import { useRef, useImperativeHandle, forwardRef } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { sql } from '@codemirror/lang-sql'
import { oneDark } from '@codemirror/theme-one-dark'
import { Button } from '@/components/ui/button'
import { keymap } from '@codemirror/view'
import { EditorView } from '@codemirror/view'
import { EditorSelection } from '@codemirror/state'

interface SqlEditorProps {
  value: string
  onChange: (v: string) => void
  onExecute: () => void
  placeholder?: string
  loading?: boolean
}

export interface SqlEditorHandle {
  /** 选中编辑器内容中第一个 {placeholder} 占位符（如 {col}），使用户可直接输入替换 */
  selectFirstPlaceholder: () => void
}

export const SqlEditor = forwardRef<SqlEditorHandle, SqlEditorProps>(function SqlEditor({ 
  value, 
  onChange, 
  onExecute, 
  placeholder = 'SELECT * FROM table_name LIMIT 100',
  loading = false
}, ref) {
  const editorRef = useRef<EditorView | null>(null)

  useImperativeHandle(ref, () => ({
    selectFirstPlaceholder() {
      const editor = editorRef.current
      if (!editor) return
      const text = editor.state.doc.toString()
      const match = text.match(/\{[^}]+\}/)
      if (!match || match.index === undefined) return
      const from = match.index
      const to = from + match[0].length
      editor.dispatch({
        selection: EditorSelection.single(from, to),
        scrollIntoView: true,
      })
      editor.focus()
    }
  }))

  const handleEditorCreated = (editor: EditorView) => {
    editorRef.current = editor
  }

  // Create keymap for Ctrl/Cmd + Enter to execute SQL
  const executeKeymap = keymap.of([
    {
      key: 'Mod-Enter',
      run: () => {
        onExecute()
        return true
      }
    }
  ])

  return (
    <div className="space-y-2">
      <div className="border rounded-md overflow-hidden">
        <CodeMirror
          value={value}
          onChange={(v) => onChange(v)}
          extensions={[sql(), executeKeymap]}
          theme={oneDark}
          placeholder={placeholder}
          editable={!loading}
          onCreateEditor={handleEditorCreated}
          height="120px"
          style={{
            fontSize: '14px',
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace'
          }}
          basicSetup={{
            lineNumbers: true,
            highlightActiveLineGutter: true,
            highlightSpecialChars: true,
            history: true,
            foldGutter: true,
            drawSelection: true,
            dropCursor: true,
            allowMultipleSelections: true,
            indentOnInput: true,
            syntaxHighlighting: true,
            bracketMatching: true,
            closeBrackets: true,
            autocompletion: true,
            rectangularSelection: true,
            crosshairCursor: true,
            highlightActiveLine: true,
            highlightSelectionMatches: true,
            closeBracketsKeymap: true,
            defaultKeymap: true,
            searchKeymap: true,
            historyKeymap: true,
            foldKeymap: true,
            completionKeymap: true,
            lintKeymap: true
          }}
        />
      </div>
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
})
