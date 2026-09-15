/**
 * Profiles list tests (spec §11, §31).
 *
 * The transport is mocked at the bindings boundary (no HTTP, no Wails). Two
 * behaviours are asserted: the list renders what the backend reported, and the
 * delete action is gated by a confirmation that names the active profile, since
 * deleting the live profile stops the runtime.
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

const mocks = vi.hoisted(() => ({ remove: vi.fn() }))

const profilesPayload = {
  profiles: [
    {
      id: 'profile-primary',
      name: 'Основной',
      description: 'Рабочая конфигурация',
      createdAt: '2026-09-11T10:00:00Z',
      updatedAt: '2026-09-11T11:00:00Z',
      activeRevisionId: 'revision-active',
    },
    {
      id: 'profile-secondary',
      name: 'Резервный',
      description: '',
      createdAt: '2026-09-10T09:00:00Z',
      updatedAt: '2026-09-10T09:30:00Z',
      activeRevisionId: '',
    },
  ],
  activeId: 'profile-primary',
  running: true,
}

const revision = {
  id: 'revision-active',
  createdAt: '2026-09-11T10:30:00Z',
  source: 'manual',
  comment: '',
  active: true,
  structuralValidationStatus: 'ok',
  singBoxValidationStatus: 'ok',
  singBoxVersion: '1.13.0',
}

const runtimePayload = {
  status: {
    state: 'RUNNING',
    pid: 4242,
    uptimeSeconds: 120,
    activeProfileId: 'profile-primary',
    activeRevisionId: 'revision-active',
    binaryVersion: '1.13.0',
    configPath:
      '/Users/test/Library/Application Support/SingBoxUI/profiles/profile-primary/config.json',
    elevated: false,
  },
  activeProfileName: 'Основной',
  binaryVersion: '1.13.0',
  trafficAvailable: false,
  shuttingDown: false,
}

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  profilesApi: {
    list: () => Promise.resolve(profilesPayload),
    listTemplates: () =>
      Promise.resolve({
        templates: [{ id: 'template-basic', name: 'Базовый', description: '' }],
      }),
    remove: mocks.remove,
  },
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
  configApi: {
    detectLegacy: () => Promise.resolve({ candidate: { found: false, warnings: [] } }),
    activeConfigPath: () => Promise.resolve(''),
    listRevisions: () => Promise.resolve({ revisions: [revision] }),
  },
}))

function renderProfiles() {
  const router = createAppRouter(createMemoryHistory({ initialEntries: ['/profiles'] }))
  return render(
    <AppProviders queryClient={createAppQueryClient()}>
      <AppRouterView router={router} />
    </AppProviders>,
  )
}

/** The row that owns a profile name; actions live in the same table row. */
async function rowFor(name: string): Promise<HTMLElement> {
  // The name is a link to the workspace; the shell header also prints the
  // active profile name, so anchor on the link role instead of bare text.
  const link = await screen.findByRole('link', { name })
  const row = link.closest('tr')
  if (row === null) throw new Error(`no table row for ${name}`)
  return row
}

beforeEach(() => {
  mocks.remove.mockReset()
  mocks.remove.mockResolvedValue(profilesPayload)
})

describe('profiles list', () => {
  it('renders the profiles the backend reported with their status', async () => {
    renderProfiles()

    const primary = await rowFor('Основной')
    expect(within(primary).getByText('Рабочая конфигурация')).toBeInTheDocument()
    expect(within(primary).getByText('активный, запущен')).toBeInTheDocument()

    const secondary = await rowFor('Резервный')
    expect(within(secondary).getByText('не активен')).toBeInTheDocument()

    // Revision count comes from configApi.listRevisions, not from the profile row.
    await waitFor(() => {
      expect(within(primary).getByText(/1 всего/)).toBeInTheDocument()
    })
  })

  it('deletes a profile only after the confirmation is accepted', async () => {
    const user = userEvent.setup({ delay: null })
    renderProfiles()

    const primary = await rowFor('Основной')
    await user.click(within(primary).getByRole('button', { name: 'Удалить' }))

    // Opening the dialog must not call the backend.
    expect(mocks.remove).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Удалить профиль?')).toBeInTheDocument()
    expect(
      within(dialog).getByText(/будут удалены без возможности восстановления/),
    ).toBeInTheDocument()
    // Deleting the active profile stops the runtime, and the dialog says so.
    expect(within(dialog).getByText(/остановит рантайм/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Удалить профиль' }))

    await waitFor(() => {
      expect(mocks.remove).toHaveBeenCalledWith('profile-primary')
    })
  })

  it('cancelling the confirmation leaves the profile in place', async () => {
    const user = userEvent.setup({ delay: null })
    renderProfiles()

    const secondary = await rowFor('Резервный')
    await user.click(within(secondary).getByRole('button', { name: 'Удалить' }))

    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(mocks.remove).not.toHaveBeenCalled()
  })
})
