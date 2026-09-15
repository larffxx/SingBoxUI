/**
 * Top bar: the active profile and the runtime state, plus the theme switch.
 *
 * Both values come from the query cache, so a `runtime:status` or
 * `profiles:changed` event updates this bar without a reload.
 */
import { Moon, Sun } from 'lucide-react'
import * as React from 'react'

import { useActiveProfile, useRuntimeSummary } from '@/app/queries'
import { useTheme } from '@/app/theme/theme'
import { runtimeStateLabel, runtimeStateTone } from '@/features/runtime/state'
import { Badge, Button, StatusDot, Tooltip } from '@/shared/ui'

export function TopBar(): React.ReactElement {
  const { state, isRunning } = useRuntimeSummary()
  const { profile, activeId } = useActiveProfile()
  const { resolved, isAvailable, setTheme, isSaving } = useTheme()
  const tone = runtimeStateTone(state)

  const profileLabel = profile?.name ?? (activeId ? activeId : 'Профиль не выбран')

  return (
    <header className="flex flex-wrap items-center gap-3 border-b border-border bg-background px-6 py-3">
      <div className="min-w-0 flex-1">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">Активный профиль</p>
        <p className="truncate text-sm font-medium">{profileLabel}</p>
      </div>
      <Badge tone={tone}>
        <StatusDot tone={tone} pulse={!isRunning && state !== 'STOPPED'} />
        {runtimeStateLabel(state)}
      </Badge>
      <Tooltip label={resolved === 'dark' ? 'Светлая тема' : 'Тёмная тема'}>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Переключить тему"
          disabled={!isAvailable || isSaving}
          onClick={() => setTheme(resolved === 'dark' ? 'light' : 'dark')}
        >
          {resolved === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </Button>
      </Tooltip>
    </header>
  )
}
