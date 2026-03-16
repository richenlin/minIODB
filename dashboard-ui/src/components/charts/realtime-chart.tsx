'use client'

import { useEffect, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { useSSE } from '@/hooks/use-sse'

interface RealtimeChartProps {
  title: string
  valueKey: string
  unit?: string
  color?: string
  height?: number
}

export function RealtimeChart({ title, valueKey, unit = '', color = '#6366f1', height = 200 }: RealtimeChartProps) {
  const { data } = useSSE<{ data?: Record<string, unknown> }>('/monitor/stream')
  const [history, setHistory] = useState<number[]>(Array(30).fill(0))
  const [labels, setLabels] = useState<string[]>(Array(30).fill(''))

  useEffect(() => {
    if (!data?.data) return
    const value = Number(data.data[valueKey] ?? 0)
    setHistory(prev => [...prev.slice(1), value])
    setLabels(prev => [...prev.slice(1), new Date().toLocaleTimeString('zh-CN')])
  }, [data, valueKey])

  const option = {
    title: { text: title, textStyle: { fontSize: 13 } },
    grid: { left: 48, right: 8, top: 36, bottom: 24 },
    xAxis: { type: 'category', data: labels, axisLabel: { fontSize: 10 } },
    yAxis: { type: 'value', axisLabel: { formatter: `{value}${unit}` } },
    series: [{
      type: 'line',
      data: history,
      smooth: true,
      symbol: 'none',
      lineStyle: { color, width: 2 },
      areaStyle: { opacity: 0.1 },
    }],
    tooltip: { trigger: 'axis' },
  }
  return <ReactECharts option={option} style={{ height }} />
}
