/**
 * Bounded log buffer provider (spec §46).
 *
 * The buffer state, its cap and the merge helpers live in `./logBuffer`; this
 * module only wires them to React and exports the component.
 */
import * as React from 'react'

import type { runtime } from '@/shared/api/bindings'

import { accumulateDropped, appendLogRecords, LogBufferContext } from './logBuffer'
import type { LogBufferValue } from './logBuffer'

export function LogBufferProvider({ children }: { children: React.ReactNode }): React.ReactElement {
  const [records, setRecords] = React.useState<runtime.LogRecord[]>([])
  const [dropped, setDropped] = React.useState(0)

  const append = React.useCallback((incoming: runtime.LogRecord[], droppedDelta = 0) => {
    setDropped((current) => accumulateDropped(current, droppedDelta))
    setRecords((current) => appendLogRecords(current, incoming))
  }, [])

  const replace = React.useCallback((incoming: runtime.LogRecord[]) => {
    setRecords(appendLogRecords([], incoming))
  }, [])

  const clear = React.useCallback(() => {
    setRecords([])
    setDropped(0)
  }, [])

  const value = React.useMemo<LogBufferValue>(
    () => ({ records, dropped, append, replace, clear }),
    [records, dropped, append, replace, clear],
  )

  return <LogBufferContext.Provider value={value}>{children}</LogBufferContext.Provider>
}
