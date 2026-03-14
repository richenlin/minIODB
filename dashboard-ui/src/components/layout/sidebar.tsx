'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'
import {
  DashboardIcon,
  PersonIcon,
  CubeIcon,
  LayersIcon,
  FileTextIcon,
  ArchiveIcon,
  ActivityLogIcon,
  BarChartIcon,
  GearIcon,
} from '@radix-ui/react-icons'

interface NavItem {
  name: string
  href: string
  icon: React.ReactNode
}

const navItems: NavItem[] = [
  { name: '总览', href: '/dashboard/ui', icon: <DashboardIcon /> },
  { name: '集群', href: '/dashboard/ui/cluster', icon: <CubeIcon /> },
  { name: '节点', href: '/dashboard/ui/nodes', icon: <PersonIcon /> },
  { name: '数据', href: '/dashboard/ui/data', icon: <LayersIcon /> },
  { name: '日志', href: '/dashboard/ui/logs', icon: <FileTextIcon /> },
  { name: '备份', href: '/dashboard/ui/backup', icon: <ArchiveIcon /> },
  { name: '监控', href: '/dashboard/ui/monitor', icon: <ActivityLogIcon /> },
  { name: '分析', href: '/dashboard/ui/analytics', icon: <BarChartIcon /> },
]

export function Sidebar() {
  const pathname = usePathname()

  return (
    <aside className="fixed left-0 top-0 z-40 h-screen w-64 border-r border-border bg-card">
      <div className="flex h-full flex-col">
        {/* Logo */}
        <div className="flex h-16 items-center gap-2 border-b border-border px-6">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <LayersIcon className="h-5 w-5" />
          </div>
          <span className="text-lg font-semibold">MinIODB</span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 space-y-1 p-4">
          {navItems.map((item) => {
            const isActive =
              item.href === '/dashboard/ui'
                ? pathname === '/dashboard/ui' || pathname === '/dashboard/ui/'
                : pathname.startsWith(item.href)

            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                )}
              >
                {item.icon}
                {item.name}
              </Link>
            )
          })}
        </nav>

        {/* Settings */}
        <div className="border-t border-border p-4">
          <Link
            href="/dashboard/ui/settings"
            className={cn(
              'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground',
              pathname.startsWith('/dashboard/ui/settings') &&
                'bg-accent text-accent-foreground'
            )}
          >
            <GearIcon />
            设置
          </Link>
        </div>
      </div>
    </aside>
  )
}
