/**
 * Binary manager tests (spec §17–§21).
 *
 * The transport is mocked at the bindings boundary. Three things are asserted:
 * the screen reports what is installed and what the release channel offers
 * (including the checksum it will verify and the signature it cannot), an update
 * is only installed after an explicit confirmation, and the `binary:progress`
 * event — parsed and stored by the app-level bridge — is what drives the
 * progress UI.
 */
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { applyBinaryProgress, asBinaryProgress } from '@/app/events'
import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'
import { BinaryRoute } from '@/features/binary/BinaryRoute'

const CHECKSUM = 'a'.repeat(64)
const PATH = '/Users/test/Library/Application Support/SingBoxUI/bin/sing-box'

const statusPayload = {
  status: {
    source: 'managed',
    platform: 'darwin-arm64',
    unsupported: false,
    activePath: PATH,
    activeVersion: '1.11.0',
    activeOk: true,
    activeError: '',
    customPath: '',
    managed: {
      installed: true,
      version: '1.11.0',
      path: PATH,
      sha256: 'b'.repeat(64),
      installedAt: '2026-09-01T10:00:00Z',
    },
    lastCheck: '2026-09-10T09:00:00Z',
    checkError: '',
  },
  error: undefined,
}

const checkPayload = {
  check: {
    currentVersion: '1.11.0',
    checkedAt: '2026-09-10T09:00:00Z',
    update: {
      version: '1.12.0',
      tag: 'v1.12.0',
      assetName: 'sing-box-1.12.0-darwin-arm64.tar.gz',
      size: 24_000_000,
      sha256: CHECKSUM,
      publishedAt: '2026-09-09T18:30:00Z',
      notes: '',
    },
    unsupported: false,
    noInstalledBinary: false,
    downloadHost: 'api.github.com',
    updateAvailable: true,
  },
  error: undefined,
}

const installPayload = {
  install: {
    version: '1.12.0',
    path: PATH,
    sha256: CHECKSUM,
    previousPath: `${PATH}.previous`,
    staleRevisions: 0,
    installedAt: '2026-09-11T12:00:00Z',
    status: {
      source: 'managed',
      platform: 'darwin-arm64',
      unsupported: false,
      activePath: PATH,
      activeVersion: '1.12.0',
      activeOk: true,
      customPath: '',
      managed: { installed: true, version: '1.12.0', path: PATH, sha256: CHECKSUM },
    },
  },
  error: undefined,
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
    binaryPath: PATH,
  },
  error: '',
}

const mocks = vi.hoisted(() => ({
  status: vi.fn(),
  lastUpdateCheck: vi.fn(),
  checkForUpdates: vi.fn(),
  installStableUpdate: vi.fn(),
  setSource: vi.fn(),
  probe: vi.fn(),
}))

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  binaryApi: mocks,
  profilesApi: { list: () => Promise.resolve({ profiles: [], activeId: '', running: false }) },
  configApi: {},
  shareApi: {},
  runtimeApi: {},
  trafficApi: {},
  settingsApi: { get: () => Promise.resolve(settingsPayload) },
}))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.status.mockResolvedValue(statusPayload)
  mocks.lastUpdateCheck.mockResolvedValue(checkPayload)
  mocks.installStableUpdate.mockResolvedValue(installPayload)
})

function renderScreen() {
  const client = createAppQueryClient()
  const view = render(
    <AppProviders queryClient={client}>
      <BinaryRoute />
    </AppProviders>,
  )
  return { ...view, client }
}

describe('BinaryRoute', () => {
  it('reports the installed binary, the offered release and the verification it can and cannot do', async () => {
    renderScreen()

    // What is installed, and where it came from. The path is reported twice
    // (active file and managed version), which is deliberate.
    expect(await screen.findByText('управляемая версия 1.11.0')).toBeInTheDocument()
    expect(screen.getAllByText(PATH)).toHaveLength(2)
    expect(screen.getByText('источник: управляемый')).toBeInTheDocument()
    expect(screen.getByText('бинарник рабочий')).toBeInTheDocument()

    // What the last check found, field by field.
    expect(await screen.findByText('v1.12.0')).toBeInTheDocument()
    expect(screen.getByText('sing-box-1.12.0-darwin-arm64.tar.gz')).toBeInTheDocument()
    expect(screen.getByText(CHECKSUM)).toBeInTheDocument()
    expect(screen.getByText('доступно обновление')).toBeInTheDocument()

    // The security posture is on the screen, not in the docs.
    expect(screen.getByText(/сверяется с SHA-256, опубликованным в релизе/)).toBeInTheDocument()
    expect(screen.getByText(/Подпись релиза не проверяется/)).toBeInTheDocument()
    expect(screen.getByText(/Ничего не устанавливается автоматически/)).toBeInTheDocument()
  })

  it('does not install anything until the confirmation is accepted', async () => {
    renderScreen()
    const user = userEvent.setup()

    const install = await screen.findByRole('button', { name: 'Установить стабильную версию' })
    // The offer only arrives with the update check, so wait for the button to
    // leave the disabled state instead of assuming the first paint has data.
    await waitFor(() => {
      expect(install).toBeEnabled()
    })
    await user.click(install)

    // The dialog explains what will be verified before the download starts.
    expect(await screen.findByText('Скачать и установить')).toBeInTheDocument()
    expect(mocks.installStableUpdate).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Скачать и установить' }))

    await waitFor(() => {
      expect(mocks.installStableUpdate).toHaveBeenCalledTimes(1)
    })

    // The outcome names the installed version and path, and the rollback source.
    expect(await screen.findByText(`sing-box 1.12.0 установлен в ${PATH}.`)).toBeInTheDocument()
    expect(screen.getByText('Результат установки')).toBeInTheDocument()
    expect(screen.getByText(/откат возможен без повторного скачивания/)).toBeInTheDocument()
  })

  it('renders the stage and percentage carried by the binary:progress event', async () => {
    const { client } = renderScreen()
    await screen.findByText('управляемая версия 1.11.0')

    // Exactly the production path: the raw event payload is parsed by the app
    // bridge and parked in the query cache, which the screen reads back.
    await act(async () => {
      const parsed = asBinaryProgress({ stage: 'download', version: '1.12.0', percent: 42 })
      expect(parsed).toBeDefined()
      applyBinaryProgress(client, parsed as { stage: string; version: string; percent: number })
    })

    expect(await screen.findByText('Установка')).toBeInTheDocument()
    expect(screen.getByText('этап: Скачиваем архив')).toBeInTheDocument()
    expect(screen.getByText('42%')).toBeInTheDocument()
    expect(screen.getByText('версия: 1.12.0')).toBeInTheDocument()

    // A later stage replaces the previous one instead of piling up.
    await act(async () => {
      const parsed = asBinaryProgress({ stage: 'done', version: '1.12.0', percent: 100 })
      applyBinaryProgress(client, parsed as { stage: string; version: string; percent: number })
    })

    expect(await screen.findByText('этап: Готово')).toBeInTheDocument()
    expect(screen.getByText('100%')).toBeInTheDocument()
    expect(screen.queryByText('этап: Скачиваем архив')).not.toBeInTheDocument()

    // The screen states that only percentages arrive; byte counters are not
    // invented client-side.
    expect(screen.getByText(/проценты, а не байты/)).toBeInTheDocument()
  })
})
