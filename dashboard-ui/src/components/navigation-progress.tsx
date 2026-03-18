'use client'

import { useNavStore } from '@/stores/nav-store'

/**
 * Thin progress bar at the top of the viewport, shown when the user has
 * clicked a nav link and the new page is loading. Clears when the route changes.
 */
export function NavigationProgress() {
  const isNavigating = useNavStore((s) => s.isNavigating)

  if (!isNavigating) return null

  return (
    <div
      className="fixed left-0 top-0 z-[100] h-0.5 w-full overflow-hidden bg-primary/80"
      role="progressbar"
      aria-label="页面加载中"
    >
      <div
        className="h-full w-1/3 bg-primary-foreground/40"
        style={{
          animation: 'nav-progress 1.2s ease-in-out infinite',
        }}
      />
    </div>
  )
}
