'use client'

import ReactECharts from 'echarts-for-react'

interface MiniLineProps {
  data: number[]
  color?: string
  height?: number
}

export function MiniLine({ data, color = '#6366f1', height = 48 }: MiniLineProps) {
  const option = {
    grid: { left: 0, right: 0, top: 4, bottom: 0 },
    xAxis: { show: false, type: 'category' },
    yAxis: { show: false, type: 'value' },
    series: [{
      type: 'line',
      data,
      smooth: true,
      symbol: 'none',
      lineStyle: { color, width: 2 },
      areaStyle: { color, opacity: 0.1 },
    }],
  }
  return <ReactECharts option={option} style={{ height, width: '100%' }} />
}
