/**
 * The app-level notice surface.
 *
 * Renders `app:notice` events pushed by the backend plus the "backend
 * unavailable" banner that keeps `vite dev` in a plain browser (and the unit
 * tests) usable. Persistent notices carry a dismiss button; transient ones are
 * removed by the provider's TTL.
 */
import * as React from 'react'

import { noticeTone, useNotices } from '@/app/notices/notices'
import { backendAvailable } from '@/app/queries'
import { Alert, Button } from '@/shared/ui'

export function NoticeSurface(): React.ReactElement | null {
  const { notices, dismiss } = useNotices()
  const backend = backendAvailable()

  if (backend && notices.length === 0) return null

  return (
    <div
      className="space-y-2 border-b border-border bg-background px-6 py-3"
      data-testid="notice-surface"
    >
      {!backend ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Приложение открыто вне окна Wails: привязки Go не подключены, данные не загружаются. Для
          полноценной работы запустите <code className="font-mono">wails dev</code>.
        </Alert>
      ) : null}
      {notices.map((notice) => (
        <Alert
          key={notice.id}
          tone={noticeTone(notice.level)}
          title={notice.title}
          code={notice.code}
          details={notice.details}
          actions={
            notice.persistent ? (
              <Button variant="ghost" size="sm" onClick={() => dismiss(notice.id)}>
                Скрыть
              </Button>
            ) : undefined
          }
        >
          {notice.message}
        </Alert>
      ))}
    </div>
  )
}
