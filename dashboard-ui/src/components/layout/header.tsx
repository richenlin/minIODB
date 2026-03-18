'use client'

import { useAuthStore } from '@/stores/auth-store'
import { useRouter, usePathname } from 'next/navigation'
import { PersonIcon, ExitIcon, MoonIcon, SunIcon } from '@radix-ui/react-icons'
import { useState, useEffect } from 'react'

const PAGE_TITLES: Record<string, string> = {
  '/': '总览',
  '/cluster': '集群管理',
  '/nodes': '节点管理',
  '/monitor': '监控',
  '/analytics': '分析',
  '/data': '数据管理',
  '/logs': '日志',
  '/backup': '备份管理',
  '/settings': '设置',
}

export function Header() {
  const { isAuthenticated, user, logout } = useAuthStore()
  const router = useRouter()
  const pathname = usePathname()
  const [isDark, setIsDark] = useState(false)

  const normalizedPath = pathname.length > 1 ? pathname.replace(/\/$/, '') : pathname
  const pageTitle = PAGE_TITLES[normalizedPath] ?? 'Dashboard'

  useEffect(() => {
    // Check initial dark mode state
    const isDarkMode = document.documentElement.classList.contains('dark')
    setIsDark(isDarkMode)
  }, [])

  const toggleDarkMode = () => {
    const newIsDark = !isDark
    setIsDark(newIsDark)
    if (newIsDark) {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
  }

  const handleLogout = () => {
    logout()
    router.push('/login')
  }

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-border bg-background/95 px-6 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex items-center gap-4">
        <h1 className="text-lg font-semibold">{pageTitle}</h1>
      </div>

      <div className="flex items-center gap-4">
        {/* Dark Mode Toggle */}
        <button
          onClick={toggleDarkMode}
          className="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          aria-label="Toggle dark mode"
        >
          {isDark ? <SunIcon className="h-5 w-5" /> : <MoonIcon className="h-5 w-5" />}
        </button>

        {/* User Info */}
        {isAuthenticated && (
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary text-primary-foreground">
              <PersonIcon className="h-4 w-4" />
            </div>
            <span className="text-sm font-medium">{user?.display_name || user?.key || '用户'}</span>
            <button
              onClick={handleLogout}
              className="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              aria-label="Logout"
            >
              <ExitIcon className="h-5 w-5" />
            </button>
          </div>
        )}
      </div>
    </header>
  )
}
