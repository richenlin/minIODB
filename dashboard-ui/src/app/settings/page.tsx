'use client'

import { useCallback, useEffect, useState } from 'react'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { clusterApi } from '@/lib/api/cluster'
import { ConfigUpdateRequest, ConfigUpdateResult, FullConfig } from '@/lib/api/types'
import { useAuthStore } from '@/stores/auth-store'

// ─────────────────────────── field definitions ────────────────────────────────

type FieldType = 'text' | 'password' | 'number' | 'select' | 'boolean'

interface FieldDef {
  key: keyof FullConfig & keyof ConfigUpdateRequest
  label: string
  type: FieldType
  options?: string[]
  hint?: string
  readOnly?: boolean
}

const SECTIONS: { title: string; fields: FieldDef[] }[] = [
  {
    title: '服务器',
    fields: [
      { key: 'node_id', label: '节点 ID', type: 'text', hint: '分布式部署时每个节点须唯一' },
      { key: 'grpc_port', label: 'gRPC 端口', type: 'text', hint: '例：:8080' },
      { key: 'rest_port', label: 'REST 端口', type: 'text', hint: '例：:8081' },
    ],
  },
  {
    title: 'Redis',
    fields: [
      {
        key: 'redis_mode', label: '模式', type: 'select',
        options: ['standalone', 'sentinel', 'cluster'],
        hint: 'standalone | sentinel | cluster',
      },
      { key: 'redis_addr', label: '地址 (standalone)', type: 'text', hint: 'host:port' },
      { key: 'redis_password', label: '密码', type: 'password', hint: '无密码留空' },
      { key: 'redis_db', label: 'DB', type: 'number', hint: '0–15' },
    ],
  },
  {
    title: 'MinIO',
    fields: [
      { key: 'minio_endpoint', label: '地址', type: 'text', hint: 'host:port' },
      { key: 'minio_access_key_id', label: 'Access Key ID', type: 'text' },
      { key: 'minio_secret_access_key', label: 'Secret Access Key', type: 'password' },
      {
        key: 'minio_use_ssl', label: '启用 SSL', type: 'select',
        options: ['false', 'true'],
      },
      { key: 'minio_region', label: 'Region', type: 'text', hint: '例：us-east-1' },
      { key: 'minio_bucket', label: 'Bucket', type: 'text' },
    ],
  },
  {
    title: 'Dashboard',
    fields: [
      { key: 'core_endpoint', label: 'Core API 地址', type: 'text', hint: 'http://host:8081' },
      { key: 'dashboard_port', label: 'Dashboard 端口', type: 'text', hint: '例：:9090' },
    ],
  },
  {
    title: '日志',
    fields: [
      {
        key: 'log_level', label: '日志级别', type: 'select',
        options: ['debug', 'info', 'warn', 'error'],
      },
      {
        key: 'log_format', label: '输出格式', type: 'select',
        options: ['json', 'text'],
      },
      {
        key: 'log_output', label: '输出目标', type: 'select',
        options: ['stdout', 'file', 'both'],
      },
      { key: 'log_filename', label: '日志文件路径', type: 'text', hint: 'logs/minIODB.log' },
    ],
  },
  {
    title: '缓冲区',
    fields: [
      { key: 'buffer_size', label: '缓冲区大小', type: 'number', hint: '条目数，建议 1000–50000' },
      { key: 'flush_interval', label: '刷写间隔', type: 'text', hint: '例：15s | 1m' },
    ],
  },
]

// ─────────────────────────── helper components ───────────────────────────────

function EnvBadge({ envVar }: { envVar: string }) {
  return (
    <span
      title={`当前由环境变量 ${envVar} 控制，config.yaml 中的值在重启后仍会被覆盖`}
      className="ml-2 inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium
                 bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30 cursor-help"
    >
      <svg className="w-2.5 h-2.5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
          d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
      </svg>
      env:{envVar.split('/')[0].trim()}
    </span>
  )
}

function FieldInput({
  def,
  value,
  editing,
  envVar,
  onChange,
}: {
  def: FieldDef
  value: string | number | boolean | undefined
  editing: boolean
  envVar?: string
  onChange: (val: string) => void
}) {
  const strVal = value === undefined || value === null ? '' : String(value)
  const base =
    'w-full rounded-md border border-border bg-background px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50'
  const envBorderCls = envVar ? 'border-amber-500/50 focus:ring-amber-500/40' : ''

  if (!editing) {
    const display =
      def.type === 'password' && strVal
        ? strVal.slice(0, 3) + '•'.repeat(Math.max(0, strVal.length - 3))
        : strVal || '-'
    return (
      <span className="flex items-center gap-1 flex-wrap">
        <span className={`text-sm font-medium font-mono text-foreground ${def.type === 'password' ? 'tracking-widest' : ''}`}>
          {display}
        </span>
        {envVar && <EnvBadge envVar={envVar} />}
      </span>
    )
  }

  if (def.type === 'select') {
    return (
      <div className="space-y-1">
        <select className={`${base} ${envBorderCls}`} value={strVal} onChange={e => onChange(e.target.value)}>
          {def.options?.map(o => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
        {envVar && (
          <p className="text-[11px] text-amber-600 dark:text-amber-400">
            ⚠ 当前由 <code className="font-mono">{envVar}</code> 覆盖，修改 config.yaml 后需同时取消该环境变量
          </p>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-1">
      <input
        className={`${base} ${envBorderCls}`}
        type={def.type === 'password' ? 'password' : def.type === 'number' ? 'number' : 'text'}
        value={strVal}
        placeholder={def.hint}
        onChange={e => onChange(e.target.value)}
      />
      {envVar && (
        <p className="text-[11px] text-amber-600 dark:text-amber-400">
          ⚠ 当前由 <code className="font-mono">{envVar}</code> 覆盖，修改 config.yaml 后需同时取消该环境变量
        </p>
      )}
    </div>
  )
}

// ─────────────────────────── result panel ────────────────────────────────────

function ResultPanel({ result, onClose }: { result: ConfigUpdateResult; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  const copy = () => {
    navigator.clipboard.writeText(result.config_snippet)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <div className="space-y-3">
      <div className="rounded-md bg-amber-500/10 border border-amber-500/30 px-4 py-3 text-sm text-amber-600 dark:text-amber-400">
        {result.message}
      </div>
      <div className="relative">
        <pre className="rounded-md bg-muted border border-border px-4 py-3 text-xs font-mono overflow-auto max-h-72 whitespace-pre">
          {result.config_snippet}
        </pre>
        <button
          onClick={copy}
          className="absolute top-2 right-2 rounded px-2 py-0.5 text-xs border border-border bg-background hover:bg-accent transition-colors"
        >
          {copied ? '已复制 ✓' : '复制'}
        </button>
      </div>
      <p className="text-xs text-muted-foreground">
        将片段写入 <code className="bg-muted px-1 rounded">config.yaml</code> 后重启节点，变更永久生效。
      </p>
      <div className="flex justify-end">
        <Button variant="outline" size="sm" onClick={onClose}>关闭</Button>
      </div>
    </div>
  )
}

// ─────────────────────────── main page ───────────────────────────────────────

export default function SettingsPage() {
  const { token } = useAuthStore()
  const [config, setConfig] = useState<FullConfig | null>(null)
  const [draft, setDraft] = useState<Record<string, string>>({})
  const [editingSection, setEditingSection] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [result, setResult] = useState<ConfigUpdateResult | null>(null)
  const [error, setError] = useState('')

  const fetchConfig = useCallback(async () => {
    setError('')
    try {
      const data = await clusterApi.getFullConfig()
      setConfig(data)
    } catch (e) {
      // New endpoint not yet available (backend not rebuilt). Fall back to the
      // legacy /cluster/config endpoint and map the fields we can get.
      try {
        const legacy = await clusterApi.getConfig()
        setConfig({
          node_id: String(legacy.node_id ?? ''),
          grpc_port: String(legacy.grpc_port ?? ''),
          rest_port: String(legacy.rest_port ?? ''),
          redis_mode: 'standalone',
          redis_addr: String(legacy.redis_addr ?? ''),
          redis_password: '',
          redis_db: 0,
          minio_endpoint: String(legacy.minio_addr ?? ''),
          minio_access_key_id: '',
          minio_secret_access_key: '',
          minio_use_ssl: false,
          minio_region: '',
          minio_bucket: '',
          core_endpoint: String(legacy.core_endpoint ?? ''),
          dashboard_port: '',
          log_level: 'info',
          log_format: 'json',
          log_output: 'both',
          log_filename: 'logs/minIODB.log',
          buffer_size: 0,
          flush_interval: '',
          mode: String(legacy.mode ?? ''),
          version: String(legacy.version ?? ''),
        } as FullConfig)
      } catch {
        setError(`加载配置失败：${e instanceof Error ? e.message : String(e)}。请确认后端已编译并正常运行。`)
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchConfig() }, [fetchConfig])

  const startEdit = (sectionTitle: string) => {
    const section = SECTIONS.find(s => s.title === sectionTitle)!
    const initial: Record<string, string> = {}
    for (const f of section.fields) {
      if (config) {
        const v = config[f.key as keyof FullConfig]
        initial[f.key] = Array.isArray(v)
          ? (v as string[]).join(', ')
          : v === undefined || v === null ? '' : String(v)
      } else {
        initial[f.key] = ''
      }
    }
    setDraft(initial)
    setEditingSection(sectionTitle)
    setResult(null)
    setError('')
  }

  const cancelEdit = () => {
    setEditingSection(null)
    setDraft({})
  }

  const saveSection = async (sectionTitle: string) => {
    setSaving(true)
    setError('')
    const section = SECTIONS.find(s => s.title === sectionTitle)!
    const req: ConfigUpdateRequest = {}
    for (const f of section.fields) {
      const raw = draft[f.key]
      if (raw === undefined || raw === '') continue
      if (f.type === 'number') {
        const n = parseInt(raw, 10)
        if (!isNaN(n)) (req as Record<string, unknown>)[f.key] = n
      } else if (f.type === 'boolean' || f.key === 'minio_use_ssl') {
        (req as Record<string, unknown>)[f.key] = raw === 'true'
      } else {
        (req as Record<string, unknown>)[f.key] = raw
      }
    }
    try {
      const res = await clusterApi.updateConfig(req)
      // Refresh displayed values from the server-side in-memory update
      await fetchConfig()
      setResult(res)
      setEditingSection(null)
      setDraft({})
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <p className="text-sm text-muted-foreground">集群节点运行时配置，保存后显示可写入 config.yaml 的 YAML 片段</p>

        {/* result banner */}
        {result && (
          <Card className="border-amber-500/30">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-semibold text-amber-600 dark:text-amber-400">配置变更结果</CardTitle>
            </CardHeader>
            <CardContent>
              <ResultPanel result={result} onClose={() => setResult(null)} />
            </CardContent>
          </Card>
        )}

        {/* global error */}
        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {/* editable config sections */}
        {loading ? (
          <div className="space-y-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="h-28 rounded-lg border border-border bg-card animate-pulse" />
            ))}
          </div>
        ) : (
          SECTIONS.map(section => {
            const isEditing = editingSection === section.title
            return (
              <Card key={section.title}>
                <CardHeader className="flex flex-row items-center justify-between pb-2">
                  <CardTitle className="text-sm font-semibold">{section.title}</CardTitle>
                  {!isEditing ? (
                    <Button variant="ghost" size="sm"
                      className="h-7 text-xs"
                      onClick={() => startEdit(section.title)}>
                      编辑
                    </Button>
                  ) : (
                    <div className="flex gap-2">
                      <Button variant="ghost" size="sm" className="h-7 text-xs"
                        onClick={cancelEdit} disabled={saving}>
                        取消
                      </Button>
                      <Button size="sm" className="h-7 text-xs"
                        onClick={() => saveSection(section.title)} disabled={saving}>
                        {saving ? '保存中…' : '应用配置'}
                      </Button>
                    </div>
                  )}
                </CardHeader>
                <CardContent>
                  {/* env-override section banner shown only while editing */}
                  {isEditing && (() => {
                    const overriddenFields = section.fields
                      .filter(f => config?.env_overrides?.[f.key])
                      .map(f => `${f.label}（${config!.env_overrides![f.key]}）`)
                    if (overriddenFields.length === 0) return null
                    return (
                      <div className="mb-3 rounded-md bg-amber-500/10 border border-amber-500/30 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
                        <span className="font-medium">⚠ 环境变量覆盖提示：</span>{' '}
                        {overriddenFields.join('、')} 当前由环境变量控制。
                        修改 config.yaml 后，需同时取消对应环境变量，否则重启后仍会被覆盖。
                      </div>
                    )
                  })()}
                  <div className="space-y-0">
                    {section.fields.map(f => (
                      <div key={f.key}
                        className="grid grid-cols-[180px_1fr] items-center gap-4 py-2.5 border-b border-border last:border-0">
                        <div>
                          <span className="text-sm text-muted-foreground">{f.label}</span>
                          {f.hint && !isEditing && (
                            <p className="text-[11px] text-muted-foreground/60 mt-0.5">{f.hint}</p>
                          )}
                        </div>
                        <FieldInput
                          def={f}
                          value={(() => {
                            if (isEditing) return draft[f.key]
                            const v = config?.[f.key as keyof FullConfig]
                            if (Array.isArray(v)) return (v as string[]).join(', ')
                            return v as string | number | boolean | undefined
                          })()}
                          editing={isEditing}
                          envVar={config?.env_overrides?.[f.key]}
                          onChange={val => setDraft(prev => ({ ...prev, [f.key]: val }))}
                        />
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )
          })
        )}

        {/* auth / connection info (read-only) */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold">认证 &amp; 连接信息</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-0">
              <InfoRow label="API Key（截断）"
                value={token ? `${token.slice(0, 12)}••••••••••••••••••••` : '-'} />
              <InfoRow label="认证状态"
                value={token ? '已登录' : '未登录'}
                badge={token ? 'success' : 'error'} />
              <InfoRow label="运行模式"
                value={config?.mode ?? '-'} />
              <InfoRow label="服务版本"
                value={config?.version ?? '-'} />
              <InfoRow label="Dashboard API"
                value="/dashboard/api/v1" />
            </div>
          </CardContent>
        </Card>
      </div>
    </DashboardLayout>
  )
}

function InfoRow({
  label,
  value,
  badge,
}: {
  label: string
  value: string
  badge?: 'success' | 'error'
}) {
  return (
    <div className="flex items-center gap-4 py-2.5 border-b border-border last:border-0 min-w-0">
      <span className="text-sm text-muted-foreground shrink-0">{label}</span>
      {badge ? (
        <div className="ml-auto shrink-0">
          <Badge variant={badge}>{value}</Badge>
        </div>
      ) : (
        <span className="ml-auto text-sm font-medium font-mono text-foreground truncate max-w-[60%]" title={value}>
          {value}
        </span>
      )}
    </div>
  )
}
