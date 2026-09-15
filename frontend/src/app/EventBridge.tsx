/**
 * Backend event wiring (spec §46, §47).
 *
 * One component owns every subscription: it is mounted once by the providers and
 * renders nothing. Events update the query cache or invalidate the matching key
 * group; nothing subscribes twice, and every subscription is torn down on
 * unmount. When no Wails runtime is present the effect is a no-op, so the app
 * still renders in a plain browser.
 */
import { useQueryClient } from '@tanstack/react-query'
import * as React from 'react'

import { useLogBuffer } from '@/app/logs/logBuffer'
import { useNotices } from '@/app/notices/notices'
import {
  APP_EVENTS,
  applyBinaryProgress,
  applyConfigApplyProgress,
  applyRuntimeStatus,
  applyTrafficSnapshot,
  asBinaryProgress,
  asConfigApplyProgress,
  asNotice,
  asRuntimeStatus,
  asTrafficSnapshot,
  handleProfilesChanged,
  parseLogBatch,
} from '@/app/events'
import { eventBridge, subscribeToEvent } from '@/app/wailsRuntime'

export function AppEventBridge(): null {
  const client = useQueryClient()
  const { push } = useNotices()
  const { append } = useLogBuffer()

  React.useEffect(() => {
    const bridge = eventBridge()
    if (!bridge) return undefined

    const disposers: Array<() => void> = []
    const on = (name: string, handler: (...data: unknown[]) => void): void => {
      disposers.push(subscribeToEvent(name, handler, bridge))
    }

    on(APP_EVENTS.runtimeStatus, (payload) => {
      const status = asRuntimeStatus(payload)
      if (status) applyRuntimeStatus(client, status)
    })

    on(APP_EVENTS.runtimeLog, (payload) => {
      const batch = parseLogBatch(payload)
      if (batch.records.length > 0 || batch.dropped > 0) append(batch.records, batch.dropped)
    })

    on(APP_EVENTS.trafficSnapshot, (payload) => {
      const snapshot = asTrafficSnapshot(payload)
      if (snapshot) applyTrafficSnapshot(client, snapshot)
    })

    on(APP_EVENTS.binaryProgress, (payload) => {
      const progress = asBinaryProgress(payload)
      if (progress) applyBinaryProgress(client, progress)
    })

    on(APP_EVENTS.configApply, (payload) => {
      const progress = asConfigApplyProgress(payload)
      if (progress) applyConfigApplyProgress(client, progress)
    })

    on(APP_EVENTS.appNotice, (payload) => {
      const notice = asNotice(payload)
      if (notice) push(notice)
    })

    on(APP_EVENTS.profilesChanged, () => {
      handleProfilesChanged(client)
    })

    return () => {
      for (const dispose of disposers) dispose()
    }
  }, [client, push, append])

  return null
}
