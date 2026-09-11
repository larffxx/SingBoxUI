/**
 * Left navigation.
 *
 * The destinations and the active-item matcher live in `./nav`; this module only
 * renders them. The active item is derived from the router pathname rather than
 * from `activeProps`, so a nested route such as `/profiles/<id>` still
 * highlights «Профили».
 */
import { Link, useRouterState } from '@tanstack/react-router'
import * as React from 'react'

import { cn } from '@/shared/lib/cn'

import { isNavItemActive, NAV_ITEMS } from './nav'

export function Sidebar(): React.ReactElement {
  const pathname = useRouterState({ select: (state) => state.location.pathname })

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-border bg-card">
      <div className="flex items-center gap-2 px-4 py-4">
        <span className="inline-block h-6 w-6 rounded-md bg-primary/15" aria-hidden />
        <span className="text-sm font-semibold tracking-tight">SingBoxUI</span>
      </div>
      <nav aria-label="Основная навигация" className="flex flex-1 flex-col gap-1 px-2 py-2">
        {NAV_ITEMS.map((item) => {
          const active = isNavItemActive(item, pathname)
          const Icon = item.icon
          return (
            <Link
              key={item.to}
              to={item.to}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                active
                  ? 'bg-accent font-medium text-accent-foreground'
                  : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
              )}
            >
              <Icon className="h-4 w-4 shrink-0" aria-hidden />
              {item.label}
            </Link>
          )
        })}
      </nav>
      <p className="px-4 py-3 text-xs text-muted-foreground">Локальное приложение</p>
    </aside>
  )
}
