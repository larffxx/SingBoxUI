/**
 * Reads a value the app-level event bridge wrote into the query cache.
 *
 * `src/app/events.ts` owns the single Wails subscription for progress events
 * (spec §19, §57); screens only read the newest stage. `useSyncExternalStore`
 * keeps that read live without opening a second subscription, and without any
 * module-level mutable state.
 */
import { useQueryClient } from '@tanstack/react-query'
import * as React from 'react'

export function useCachedEvent<T>(key: readonly unknown[]): T | undefined {
  const client = useQueryClient()
  const subscribe = React.useCallback(
    (notify: () => void) => client.getQueryCache().subscribe(notify),
    [client],
  )
  const read = React.useCallback(() => client.getQueryData<T>(key), [client, key])
  return React.useSyncExternalStore(subscribe, read, read)
}
