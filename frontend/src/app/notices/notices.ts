/**
 * Notice model (spec §51).
 *
 * The backend pushes `app:notice` for things the user must not miss: startup
 * failures, failed auto-connect, downgraded functionality. Notices live at the
 * app level so they survive navigation; `persistent` ones stay until dismissed,
 * transient ones fade on their own. The types, limits, tone mapping and context
 * live here so `NoticeProvider.tsx` exports only the component.
 */
import { createContext, useContext } from 'react'

export interface AppNotice {
  id: string
  level: string
  title: string
  message: string
  code?: string | undefined
  operation?: string | undefined
  details: string[]
  persistent: boolean
  occurredAt?: string | undefined
}

export type NewNotice = Omit<AppNotice, 'id'>

export interface NoticeContextValue {
  notices: AppNotice[]
  push: (notice: NewNotice) => void
  dismiss: (id: string) => void
  dismissAllTransient: () => void
}

/** Only the newest notices are kept: the surface is not a log. */
export const MAX_NOTICES = 8

/** Transient notices fade after this long; persistent ones never do. */
export const TRANSIENT_NOTICE_TTL_MS = 8_000

export function noticeTone(level: string): 'info' | 'warning' | 'danger' | 'success' {
  switch (level.toLowerCase()) {
    case 'error':
    case 'critical':
      return 'danger'
    case 'warn':
    case 'warning':
      return 'warning'
    case 'success':
      return 'success'
    default:
      return 'info'
  }
}

export const NoticeContext = createContext<NoticeContextValue | undefined>(undefined)

export function useNotices(): NoticeContextValue {
  const value = useContext(NoticeContext)
  if (!value) throw new Error('useNotices must be used inside a NoticeProvider')
  return value
}
