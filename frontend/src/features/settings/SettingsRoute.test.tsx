/**
 * Settings form tests (spec §50).
 *
 * The transport is mocked at the bindings boundary. Two behaviours matter for
 * the form: zod rejects a document before it is sent (`settingsApi.update` must
 * not be called at all), and a rejection coming back from the backend is shown
 * with its `BoundCallError` code, message and details instead of a raw error.
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

const UPDATE_CODE = 'SETTINGS_INVALID'
const UPDATE_MESSAGE = 'Тема оформления недоступна в этой сборке'
const RELATIVE_PATH_ERROR = 'Путь должен быть абсолютным (/usr/local/bin/sing-box)'

const mocks = vi.hoisted(() => ({ update: vi.fn() }))

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

const settingsPayload = {
  state: {
    values: {
      theme: 'system',
      logLevel: 'info',
      binarySource: 'managed',
      customBinaryPath: '',
      lastProfileId: '',
      autoStartApplication: false,
      autoConnect: false,
      managedStableChannel: true,
      updateCheckEnabled: true,
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
    status: () => Promise.resolve(runtimePayload),
    logs: () => Promise.resolve({ records: [] }),
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
    update: mocks.update,
    setAutostart: () => Promise.resolve(settingsPayload),
    removeLegacyAutostart: () => Promise.resolve(settingsPayload),
  },
  binaryApi: { status: () => Promise.resolve({ status: {} }) },
  configApi: {
    detectLegacy: () => Promise.resolve({ candidate: { found: false, warnings: [] } }),
    activeConfigPath: () => Promise.resolve(''),
  },
}))

function renderSettings() {
  const router = createAppRouter(createMemoryHistory({ initialEntries: ['/settings'] }))
  return render(
    <AppProviders queryClient={createAppQueryClient()}>
      <AppRouterView router={router} />
    </AppProviders>,
  )
}

async function save(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Сохранить/ }))
}

beforeEach(() => {
  mocks.update.mockReset()
  mocks.update.mockResolvedValue(settingsPayload)
})

describe('settings form', () => {
  it('rejects a relative custom path before calling the backend', async () => {
    const user = userEvent.setup({ delay: null })
    renderSettings()

    const path = await screen.findByLabelText('Путь к исполняемому файлу')
    await user.type(path, 'sing-box')
    await save(user)

    expect(await screen.findByText(RELATIVE_PATH_ERROR)).toBeInTheDocument()
    expect(mocks.update).not.toHaveBeenCalled()
    expect(screen.queryByText('Настройки сохранены')).not.toBeInTheDocument()
  })

  it('shows the backend validation error with its code and details', async () => {
    mocks.update.mockRejectedValue(
      new BoundCallError({
        code: UPDATE_CODE,
        message: UPDATE_MESSAGE,
        details: ['SettingsAPI.Update(theme=dark)', 'not supported in this build'],
      }),
    )
    const user = userEvent.setup({ delay: null })
    renderSettings()

    await screen.findByRole('heading', { level: 1, name: 'Настройки' })
    await save(user)

    const title = await screen.findByText('Настройки не сохранены')
    const alert = title.closest('[role="alert"]')
    expect(alert).not.toBeNull()
    expect(within(alert as HTMLElement).getByText(UPDATE_CODE)).toBeInTheDocument()
    expect(within(alert as HTMLElement).getByText(UPDATE_MESSAGE)).toBeInTheDocument()
    expect(
      within(alert as HTMLElement).getByText('SettingsAPI.Update(theme=dark)'),
    ).toBeInTheDocument()
  })

  it('submits the loaded document once the values pass validation', async () => {
    const user = userEvent.setup({ delay: null })
    renderSettings()

    await screen.findByRole('heading', { level: 1, name: 'Настройки' })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Сохранить/ })).toBeEnabled()
    })
    await save(user)

    expect(await screen.findByText('Настройки сохранены')).toBeInTheDocument()
    expect(mocks.update).toHaveBeenCalledTimes(1)
    expect(mocks.update.mock.calls[0]?.[0]).toMatchObject({
      theme: 'system',
      logLevel: 'info',
      binarySource: 'managed',
      customBinaryPath: '',
    })
  })
})
