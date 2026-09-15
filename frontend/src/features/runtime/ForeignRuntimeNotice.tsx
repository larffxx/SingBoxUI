/**
 * A sing-box that outlived the window (ADR 012).
 *
 * Its parent is gone, so nothing in the application holds a handle to stop it —
 * and while it runs, it holds the TUN device, the Clash API port and the routing
 * table, so the next «Запустить» fails with a bind error from inside the child
 * that explains nothing. This notice names the process instead and offers the one
 * action that helps: stopping it through the same narrow helper that stops a
 * supervised one.
 *
 * A process launched by hand has no run record, so it cannot be stopped from
 * here; it is named all the same, so the user knows where to look.
 */
import * as React from 'react'

import { QueryErrorAlert } from '@/app/errors/AppErrorBoundary'
import { useRuntimeSummary, useStopForeignProcessesMutation } from '@/app/queries'
import { formatDateTime } from '@/shared/lib/time'
import { Alert, Button } from '@/shared/ui'

export function ForeignRuntimeNotice(): React.ReactElement | null {
  const summary = useRuntimeSummary()
  const stopForeign = useStopForeignProcessesMutation()
  const foreign = summary.payload?.foreignProcesses ?? []

  if (foreign.length === 0) {
    return null
  }

  const stoppable = foreign.filter((process) => process.pidPath !== '')
  const names = foreign.map((process) => `pid ${process.pid}`).join(', ')

  return (
    <Alert tone="warning" title="Ядро sing-box работает вне приложения">
      <div className="space-y-3">
        <p>
          {names} — процесс запущен не этим экземпляром приложения. Пока он работает, он держит
          TUN-устройство и порты, поэтому профиль не запустится.
        </p>
        <ul className="space-y-1 font-mono text-xs text-muted-foreground">
          {foreign.map((process) => (
            <li key={process.pid}>
              pid {process.pid} · ревизия {process.revisionId || '—'}
              {process.startedAt ? ` · запущен ${formatDateTime(process.startedAt)}` : ''}
            </li>
          ))}
        </ul>
        {stoppable.length === 0 ? (
          <p>Остановите его там, где он был запущен: у приложения нет записи об этом процессе.</p>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            loading={stopForeign.isPending}
            onClick={() => {
              stopForeign.mutate()
            }}
          >
            Остановить
          </Button>
        )}
        {stopForeign.error ? (
          <QueryErrorAlert error={stopForeign.error} title="Чужое ядро не остановлено" />
        ) : null}
      </div>
    </Alert>
  )
}
