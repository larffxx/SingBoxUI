/**
 * Persistent notice surface (spec §51).
 *
 * The model, the retention limits and the context live in `./notices`; this
 * module only owns the React state and exports the component.
 */
import * as React from 'react'

import {
  MAX_NOTICES,
  NoticeContext,
  TRANSIENT_NOTICE_TTL_MS,
  type AppNotice,
  type NewNotice,
  type NoticeContextValue,
} from './notices'

export function NoticeProvider({ children }: { children: React.ReactNode }): React.ReactElement {
  const [notices, setNotices] = React.useState<AppNotice[]>([])
  const seqRef = React.useRef(0)
  const timersRef = React.useRef(new Set<number>())

  React.useEffect(() => {
    const timers = timersRef.current
    return () => {
      for (const timer of timers) window.clearTimeout(timer)
      timers.clear()
    }
  }, [])

  const dismiss = React.useCallback((id: string) => {
    setNotices((current) => current.filter((notice) => notice.id !== id))
  }, [])

  const push = React.useCallback(
    (notice: NewNotice) => {
      seqRef.current += 1
      const id = `notice-${seqRef.current}`
      setNotices((current) => [...current, { ...notice, id }].slice(-MAX_NOTICES))
      if (!notice.persistent && typeof window !== 'undefined') {
        const timer = window.setTimeout(() => {
          timersRef.current.delete(timer)
          dismiss(id)
        }, TRANSIENT_NOTICE_TTL_MS)
        timersRef.current.add(timer)
      }
    },
    [dismiss],
  )

  const dismissAllTransient = React.useCallback(() => {
    setNotices((current) => current.filter((notice) => notice.persistent))
  }, [])

  const value = React.useMemo<NoticeContextValue>(
    () => ({ notices, push, dismiss, dismissAllTransient }),
    [notices, push, dismiss, dismissAllTransient],
  )

  return <NoticeContext.Provider value={value}>{children}</NoticeContext.Provider>
}
