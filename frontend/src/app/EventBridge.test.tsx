/**
 * Event wiring test (spec §46, §47).
 *
 * Installs a fake `window.runtime` the way the Wails webview does and asserts
 * that the provider stack subscribes to every documented event, that a pushed
 * `runtime:status` payload reaches the cache, and that unmounting unsubscribes.
 */
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { APP_EVENTS } from '@/app/events'
import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'
import { keys } from '@/shared/api/keys'
import type { desktop } from '@/shared/api/bindings'

vi.mock('@/shared/api/bindings', () => ({ isDesktopRuntime: () => true }))

type Handler = (...data: unknown[]) => void

const runtime = vi.hoisted(() => {
  const handlers = new Map<string, Handler>()
  const subscribed: string[] = []
  const unsubscribed: string[] = []
  return { handlers, subscribed, unsubscribed }
})

const fakeWindow = window as Window & { runtime?: unknown }
const EVENT_COUNT = Object.keys(APP_EVENTS).length

beforeEach(() => {
  runtime.handlers.clear()
  runtime.subscribed.length = 0
  runtime.unsubscribed.length = 0
  fakeWindow.runtime = {
    EventsOn(name: string, callback: Handler) {
      runtime.handlers.set(name, callback)
      runtime.subscribed.push(name)
      return () => {
        runtime.unsubscribed.push(name)
        runtime.handlers.delete(name)
      }
    },
    EventsOff(name: string) {
      runtime.unsubscribed.push(name)
      runtime.handlers.delete(name)
    },
  }
})

afterEach(() => {
  delete fakeWindow.runtime
})

function renderShell(): {
  client: ReturnType<typeof createAppQueryClient>
  view: ReturnType<typeof render>
} {
  const client = createAppQueryClient()
  const view = render(
    <AppProviders queryClient={client}>
      <div data-testid="shell" />
    </AppProviders>,
  )
  return { client, view }
}

describe('app event bridge', () => {
  it('subscribes to every event in the backend contract', async () => {
    renderShell()

    await waitFor(() => {
      expect(runtime.subscribed).toHaveLength(EVENT_COUNT)
    })
    expect([...runtime.subscribed].sort()).toEqual(Object.values(APP_EVENTS).sort())
  })

  it('applies a pushed runtime:status payload to the cache', async () => {
    const { client } = renderShell()
    await waitFor(() => {
      expect(runtime.handlers.has(APP_EVENTS.runtimeStatus)).toBe(true)
    })

    runtime.handlers.get(APP_EVENTS.runtimeStatus)?.({ state: 'RUNNING', pid: 5150 })

    await waitFor(() => {
      const cached = client.getQueryData<desktop.RuntimePayload>(keys.runtime.status())
      expect(cached?.status.pid).toBe(5150)
      expect(cached?.status.state).toBe('RUNNING')
    })
  })

  it('unsubscribes from every event on unmount', async () => {
    const { view } = renderShell()
    await waitFor(() => {
      expect(runtime.subscribed).toHaveLength(EVENT_COUNT)
    })

    view.unmount()

    expect([...runtime.unsubscribed].sort()).toEqual(Object.values(APP_EVENTS).sort())
  })

  it('renders and stays usable when no Wails runtime is injected', () => {
    delete fakeWindow.runtime
    const { client } = renderShell()

    expect(screen.getByTestId('shell')).toBeInTheDocument()
    expect(runtime.subscribed).toHaveLength(0)
    expect(client.getQueryData(keys.runtime.status())).toBeUndefined()
  })
})
