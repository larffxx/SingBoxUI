/**
 * Backend event contract tests (spec §46, §47).
 *
 * The handlers are pure functions over a `QueryClient`, so they can be tested
 * without a webview: a `runtime:status` payload must land in the runtime cache
 * under the shared key, and a malformed payload must be rejected instead of
 * writing garbage.
 */
import { describe, expect, it } from 'vitest'

import {
  APPLY_DONE_STAGE,
  BINARY_PROGRESS_KEY,
  CONFIG_APPLY_PROGRESS_KEY,
  applyRuntimeStatus,
  handleProfilesChanged,
  handleRuntimeStatusEvent,
} from '@/app/events'
import { createAppQueryClient } from '@/app/queryClient'
import { keys } from '@/shared/api/keys'
import type { desktop, runtime } from '@/shared/api/bindings'

function status(overrides: Partial<runtime.Status> = {}): runtime.Status {
  return {
    state: 'RUNNING',
    pid: 4242,
    uptimeSeconds: 91,
    activeProfileId: 'profile-1',
    activeRevisionId: 'revision-7',
    binaryVersion: '1.11.0',
    configPath: '/tmp/singboxui/config.json',
    elevated: false,
    ...overrides,
  } as runtime.Status
}

describe('applyRuntimeStatus', () => {
  it('writes the payload into the runtime cache under the shared key', () => {
    const client = createAppQueryClient()

    applyRuntimeStatus(client, status())

    const cached = client.getQueryData<desktop.RuntimePayload>(keys.runtime.status())
    expect(cached?.status.state).toBe('RUNNING')
    expect(cached?.status.pid).toBe(4242)
    expect(cached?.binaryVersion).toBe('1.11.0')
  })

  it('merges a later event into the cached payload instead of replacing it', () => {
    const client = createAppQueryClient()
    // The Wails class carries `convertValues`/`createFrom` helpers that a plain
    // cached value never has, so the seed is typed as the data fields only.
    const seeded: Omit<desktop.RuntimePayload, 'convertValues' | 'createFrom'> = {
      status: status({ state: 'STOPPED', pid: 0 }),
      activeProfileName: 'Основной',
      binaryVersion: '1.10.0',
      trafficAvailable: true,
      shuttingDown: false,
    }
    client.setQueryData(keys.runtime.status(), seeded)

    applyRuntimeStatus(client, status({ state: 'STOPPING', binaryVersion: '' }))

    const cached = client.getQueryData<desktop.RuntimePayload>(keys.runtime.status())
    expect(cached?.status.state).toBe('STOPPING')
    expect(cached?.activeProfileName).toBe('Основной')
    expect(cached?.trafficAvailable).toBe(true)
    expect(cached?.binaryVersion).toBe('1.10.0')
  })
})

describe('handleRuntimeStatusEvent', () => {
  it('applies a well-formed payload and reports success', () => {
    const client = createAppQueryClient()

    expect(handleRuntimeStatusEvent(client, status({ state: 'STARTING', pid: 0 }))).toBe(true)

    const cached = client.getQueryData<desktop.RuntimePayload>(keys.runtime.status())
    expect(cached?.status.state).toBe('STARTING')
  })

  it('ignores a payload without a state and reports failure', () => {
    const client = createAppQueryClient()

    expect(handleRuntimeStatusEvent(client, { pid: 12 })).toBe(false)
    expect(handleRuntimeStatusEvent(client, 'nonsense')).toBe(false)
    expect(client.getQueryData(keys.runtime.status())).toBeUndefined()
  })
})

describe('event cache keys', () => {
  it('keeps the progress keys in the shared key space', () => {
    expect(BINARY_PROGRESS_KEY).toEqual(['binary-progress'])
    expect(CONFIG_APPLY_PROGRESS_KEY).toEqual(['config', 'apply-progress'])
    expect(APPLY_DONE_STAGE).toBe('done')
  })

  it('invalidates profile-derived queries when profiles change without a payload', () => {
    const client = createAppQueryClient()
    client.setQueryData(keys.profiles.all, { profiles: [], activeId: '' })

    handleProfilesChanged(client)

    expect(client.getQueryState(keys.profiles.all)?.isInvalidated).toBe(true)
  })
})
