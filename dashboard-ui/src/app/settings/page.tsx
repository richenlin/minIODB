'use client'

import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { useAuthStore } from '@/stores/auth-store'

export default function SettingsPage() {
  const { token } = useAuthStore()

  return (
    <DashboardLayout>
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold text-foreground">设置</h1>
          <p className="text-sm text-muted-foreground mt-1">Dashboard 连接配置</p>
        </div>

        <div className="rounded-lg border border-border bg-card p-5 space-y-4">
          <h2 className="text-sm font-semibold text-foreground">认证信息</h2>
          <div className="space-y-0">
            <InfoRow
              label="API Key"
              value={token ? `${token.slice(0, 8)}${'•'.repeat(Math.max(0, token.length - 8))}` : '-'}
            />
            <InfoRow label="认证状态" value={token ? '已登录' : '未登录'} highlight={!!token} />
          </div>
        </div>

        <div className="rounded-lg border border-border bg-card p-5 space-y-4">
          <h2 className="text-sm font-semibold text-foreground">API 端点</h2>
          <div className="space-y-0">
            <InfoRow label="Dashboard API" value="/dashboard/api/v1" />
            <InfoRow label="REST API" value="http://localhost:8081/v1" />
            <InfoRow label="Prometheus 指标" value="http://localhost:9090/metrics" />
          </div>
        </div>
      </div>
    </DashboardLayout>
  )
}

function InfoRow({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-border last:border-0">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className={`text-sm font-medium font-mono ${highlight ? 'text-green-600 dark:text-green-400' : 'text-foreground'}`}>
        {value}
      </span>
    </div>
  )
}
