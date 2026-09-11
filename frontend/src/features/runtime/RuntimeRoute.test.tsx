/**
 * Runtime screen tests (spec §22, §23, §33).
 *
 * The transport is mocked at the bindings boundary, so these tests describe what
 * the screen does with backend answers: a rejected bound call has to surface the
 * `BoundCallError` code and message instead of crashing the webview, and a
 * failed status read must degrade to a visible error while the shell keeps
 * rendering.
 */
import { createMemoryHistory } from '@tanstack/react-router'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'
import { AppRouterView } from '@/app/router'
import { createAppRouter } from '@/app/routeTree'
import { BoundCallError } from '@/shared/api/errors'
import type { QueryClient } from '@tanstack/react-query'

const START_CODE = 'RUNTIME_START_FAILED'
const START_MESSAGE = 'Не удалось запустить sing-box: адрес уже используется'
const STATUS_CODE = 'RUNTIME_STATUS_FAILED'
const STATUS_MESSAGE = 'Не удалось прочитать состояние процесса'

const state = vi.hoisted(() => ({
  startFailure: undefined as unknown,
  statusFailure: undefined as unknown,
}))

const runtimePayload = {
  status: {
    state: 'STOPPED',
    pid: 0,
    uptimeSeconds: 0,
    activeProfileId: 'profile-1',
    activeRevisionId: 'revision-1',
    binaryVersion: '',
    configPath: '',
    elevated: false,
  },
  activeProfileName: 'Основной',
  binaryVersion: '',
  trafficAvailable: false,
  shuttingDown: false,
}

const profilesPayload = {
  profiles: [
    {
      id: 'profile-1',
      name: 'Основной',
      description: '',
      createdAt: '2026-09-11T10:00:00Z',
      updatedAt: '2026-09-11T10:00:00Z',
      activeRevisionId: 'revision-1',
    },
  ],
  activeId: 'profile-1',
  running: false,
}

const settingsPayload = {
  state: {
    values: {
      theme: 'system',
      logLevel: 'info',
      binarySource: 'managed',
      customBinaryPath: '',
    },
    autostart: { enabled: false, supported: true, legacyEntry: false, error: '' },
    dataDir: '/Users/test/Library/Application Support/SingBoxUI',
    logPath: '/Users/test/Library/Logs/SingBoxUI/singboxui.log',
    binaryPath: '/Users/test/Library/Application Support/SingBoxUI/bin/sing-box',
  },
  error: '',
}

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  profilesApi: { list: () => Promise.resolve(profilesPayload) },
  runtimeApi: {
    status: () =>
      state.statusFailure ? Promise.reject(state.statusFailure) : Promise.resolve(runtimePayload),
    start: () => Promise.reject(state.startFailure),
    stop: () => Promise.resolve(runtimePayload),
    restart: () => Promise.resolve(runtimePayload),
    logs: () => Promise.resolve({ records: [] }),
    clearLogs: () => Promise.resolve({ records: [] }),
  },
  trafficApi: {
    snapshot: () =>
      Promise.resolve({
        snapshot: {
          available: false,
          uploadTotalBytes: 0,
          downloadTotalBytes: 0,
          uploadRateBytes: 0,
          downloadRateBytes: 0,
          connections: 0,
          capturedAt: '2026-09-11T10:00:00Z',
        },
        collectorRunning: false,
      }),
  },
  settingsApi: {
    get: () => Promise.resolve(settingsPayload),
    environment: () => Promise.resolve({}),
    update: () => Promise.resolve(settingsPayload),
    setAutostart: () => Promise.resolve(settingsPayload),
    removeLegacyAutostart: () => Promise.resolve(settingsPayload),
  },
  binaryApi: { status: () => Promise.resolve({ status: {} }) },
  configApi: {
    detectLegacy: () => Promise.resolve({ candidate: { found: false, warnings: [] } }),
    activeConfigPath: () => Promise.resolve(''),
  },
}))

function renderRuntime(client?: QueryClient) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: ['/runtime'] }))
  const view = render(
    <AppProviders queryClient={client ?? createAppQueryClient()}>
      <AppRouterView router={router} />
    </AppProviders>,
  )
  return view
}

beforeEach(() => {
  state.startFailure = undefined
  state.statusFailure = undefined
})

describe('runtime screen', () => {
  it('renders the process state and the profile picker', async () => {
    renderRuntime()

    expect(await screen.findByRole('heading', { level: 1, name: 'Рантайм' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /Запустить/ })).toBeInTheDocument()

    // The state label shows up in several places (top bar, status bar, header
    // badge), so the assertion is scoped to the process-state row itself.
    const stateRow = screen.getByText('Состояние').closest('div')
    expect(stateRow).not.toBeNull()
    expect(within(stateRow as HTMLElement).getByText('Остановлен')).toBeInTheDocument()
  })

  it('renders the bound-call code and message when a control call is rejected', async () => {
    state.startFailure = new BoundCallError({
      code: START_CODE,
      message: START_MESSAGE,
      details: ['RuntimeAPI.Start(profile-1)', 'exit status 1'],
    })
    renderRuntime()

    const start = await screen.findByRole('button', { name: /Запустить/ })
    await waitFor(() => {
      expect(start).toBeEnabled()
    })
    await userEvent.click(start)

    const title = await screen.findByText('Запуск не выполнен')
    const alert = title.closest('[role="alert"]')
    expect(alert).not.toBeNull()
    expect(within(alert as HTMLElement).getByText(START_CODE)).toBeInTheDocument()
    expect(within(alert as HTMLElement).getByText(START_MESSAGE)).toBeInTheDocument()
    expect(
      within(alert as HTMLElement).getByText('RuntimeAPI.Start(profile-1)'),
    ).toBeInTheDocument()

    // The screen is still there: a rejected call must not take the webview down.
    expect(screen.getByRole('heading', { level: 1, name: 'Рантайм' })).toBeInTheDocument()
  })

  it('shows a readable error when the status read itself fails', async () => {
    state.statusFailure = new BoundCallError({ code: STATUS_CODE, message: STATUS_MESSAGE })
    const client = createAppQueryClient()
    client.setDefaultOptions({ queries: { retry: false } })

    renderRuntime(client)

    const alert = await screen.findByRole('alert')
    expect(within(alert).getByText(STATUS_CODE)).toBeInTheDocument()
    expect(within(alert).getByText(STATUS_MESSAGE)).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: 'Рантайм' })).toBeInTheDocument()
  })
})
