/**
 * The application shell (spec §52).
 *
 * Sidebar / content / status bar. The shell owns nothing but layout and
 * navigation: every value it shows comes from the query cache, which the event
 * bridge keeps current.
 */
import { Outlet } from '@tanstack/react-router'
import * as React from 'react'

import { NoticeSurface } from '@/app/shell/NoticeSurface'
import { RuntimeStatusBar } from '@/app/shell/RuntimeStatusBar'
import { Sidebar } from '@/app/shell/Sidebar'
import { TopBar } from '@/app/shell/TopBar'

export function AppShell(): React.ReactElement {
  return (
    <div className="flex h-full min-h-screen bg-background text-foreground">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar />
        <NoticeSurface />
        <main className="flex-1 overflow-y-auto px-6 py-5">
          <Outlet />
        </main>
        <RuntimeStatusBar />
      </div>
    </div>
  )
}
