/**
 * Runtime status bar.
 *
 * A dense, always-visible strip under the content: process state, PID, uptime,
 * the active config path and whether the traffic collector is feeding the UI.
 * Everything comes from the same cached runtime payload the events update.
 */
import * as React from 'react'

import { backendAvailable, useRuntimeSummary } from '@/app/queries'
import { runtimeStateLabel, runtimeStateTone } from '@/features/runtime/state'
import { formatDuration } from '@/shared/lib/time'
import { StatusDot } from '@/shared/ui'

export function RuntimeStatusBar(): React.ReactElement {
  const { payload, state, isRunning, isBusy } = useRuntimeSummary()
  const status = payload?.status
  const backend = backendAvailable()

  return (
    <footer className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border bg-card px-6 py-2 text-xs text-muted-foreground">
      <span className="inline-flex items-center gap-1.5">
        <StatusDot tone={runtimeStateTone(state)} pulse={isBusy} title={runtimeStateLabel(state)} />
        <span className="font-medium text-foreground">{runtimeStateLabel(state)}</span>
      </span>
      {status?.pid ? <span>PID {status.pid}</span> : null}
      {isRunning ? <span>Uptime {formatDuration(status?.uptimeSeconds)}</span> : null}
      {status?.binaryVersion ? <span>sing-box {status.binaryVersion}</span> : null}
      {status?.configPath ? (
        <span className="min-w-0 truncate font-mono">{status.configPath}</span>
      ) : null}
      {isRunning && payload?.trafficAvailable === false ? (
        <span>Сбор статистики недоступен</span>
      ) : null}
      <span className="ml-auto">
        {backend ? 'Привязки Go подключены' : 'Бэкенд недоступен — только интерфейс'}
      </span>
    </footer>
  )
}
