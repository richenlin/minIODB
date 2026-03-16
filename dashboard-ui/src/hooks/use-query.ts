import { useState, useEffect, useCallback } from 'react'

interface UseQueryOptions<T> {
  enabled?: boolean
  initialData?: T
  onSuccess?: (data: T) => void
  onError?: (error: Error) => void
}

interface UseQueryResult<T> {
  data: T | null
  loading: boolean
  error: string | null
  refetch: () => Promise<void>
}

export function useQuery<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = [],
  options: UseQueryOptions<T> = {}
): UseQueryResult<T> {
  const { enabled = true, initialData, onSuccess, onError } = options
  
  const [data, setData] = useState<T | null>(initialData ?? null)
  const [loading, setLoading] = useState(enabled)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await fetcher()
      setData(result)
      onSuccess?.(result)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Request failed'
      setError(msg)
      onError?.(e instanceof Error ? e : new Error(msg))
    } finally {
      setLoading(false)
    }
  }, deps)

  useEffect(() => {
    if (enabled) refetch()
  }, [refetch, enabled])

  return { data, loading, error, refetch }
}
