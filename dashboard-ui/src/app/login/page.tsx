'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { useAuthStore } from '@/stores/auth-store'
import { apiClient } from '@/lib/api/client'
import { LayersIcon, InfoCircledIcon } from '@radix-ui/react-icons'

export default function LoginPage() {
  const [apiKey, setApiKey] = useState('')
  const [apiSecret, setApiSecret] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { setAuth, isAuthenticated } = useAuthStore()
  const router = useRouter()

  useEffect(() => {
    if (isAuthenticated) {
      router.push('/')
    }
  }, [isAuthenticated, router])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!apiKey.trim()) {
      setError('请输入 API Key')
      return
    }
    setLoading(true)
    setError('')
    try {
      const response = await apiClient.login(apiKey.trim(), apiSecret.trim())
      setAuth(response.token, response.user)
      router.push('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败，请检查凭证')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="w-full max-w-md px-4">
        <div className="rounded-xl border border-border bg-card p-8 shadow-sm">
          {/* Logo */}
          <div className="flex flex-col items-center mb-8">
            <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary text-primary-foreground mb-3">
              <LayersIcon className="h-6 w-6" />
            </div>
            <h1 className="text-xl font-bold text-foreground">MinIODB Dashboard</h1>
            <p className="text-sm text-muted-foreground mt-1">请输入凭证以继续</p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-sm font-medium text-foreground">
                API Key
                <span className="ml-1 text-xs text-destructive">*</span>
              </label>
              <input
                type="text"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="来自 config.yaml auth.api_key_pairs 的 key"
                required
                autoFocus
                autoComplete="username"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring transition-colors font-mono"
              />
            </div>

            <div className="space-y-1.5">
              <label className="text-sm font-medium text-foreground">
                API Secret
                <span className="ml-1 text-xs text-destructive">*</span>
              </label>
              <input
                type="password"
                value={apiSecret}
                onChange={(e) => setApiSecret(e.target.value)}
                placeholder="来自 config.yaml auth.api_key_pairs 的 secret"
                required
                autoComplete="current-password"
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring transition-colors"
              />
            </div>

            {/* 凭证来源说明 */}
            <div className="rounded-md bg-muted/50 border border-border px-3 py-2.5 text-xs text-muted-foreground">
              <div className="flex items-start gap-1.5">
                <InfoCircledIcon className="h-3.5 w-3.5 mt-0.5 shrink-0" />
                <p>
                  凭证来自 <code className="bg-muted rounded px-1">config.yaml → auth.api_key_pairs</code> 的 <code className="bg-muted rounded px-1">key</code> 和 <code className="bg-muted rounded px-1">secret</code>。
                  Secret 同时作为 JWT 的签名密钥。
                </p>
              </div>
            </div>

            {error && (
              <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 text-sm text-destructive">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="w-full rounded-md bg-primary py-2.5 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {loading ? '登录中...' : '登 录'}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}
