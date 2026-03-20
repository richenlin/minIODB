import { useEffect, useRef, useState } from 'react'

interface UseSSEOptions {
  url: string
  enabled?: boolean
  onMessage?: (data: unknown) => void
}

interface UseSSEResult<T> {
  data: T | null
  error: string | null
  connected: boolean
}

export function useSSE<T = unknown>(options: UseSSEOptions): UseSSEResult<T>
export function useSSE<T = unknown>(url: string, enabled?: boolean): UseSSEResult<T>

export function useSSE<T = unknown>(
  urlOrOptions: string | UseSSEOptions,
  enabled = true
): UseSSEResult<T> {
  const options = typeof urlOrOptions === 'string' 
    ? { url: urlOrOptions, enabled } 
    : urlOrOptions
  
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => {
    if (!options.enabled) return
    
    const stored = localStorage.getItem('miniodb-auth')
    const token = stored ? JSON.parse(stored)?.state?.token : null

    // Next.js rewrites 代理会缓冲响应，导致 SSE 消息无法实时到达客户端。
    // 直接连后端绝对 URL，完全绕过 Next.js 代理层。
    const backendOrigin = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081'
    const baseUrl = `${backendOrigin}/dashboard/api/v1${options.url}`
    const fullUrl = token ? `${baseUrl}?token=${token}` : baseUrl
    
    const es = new EventSource(fullUrl)
    esRef.current = es

    es.onopen = () => { setConnected(true); setError(null) }
    es.onerror = () => { setError('Connection lost'); setConnected(false) }
    es.onmessage = (e) => {
      try {
        const parsed = JSON.parse(e.data)
        setData(parsed)
        options.onMessage?.(parsed)
      } catch { /* ignore */ }
    }

    return () => { es.close(); setConnected(false) }
  }, [options.url, options.enabled])

  return { data, error, connected }
}
