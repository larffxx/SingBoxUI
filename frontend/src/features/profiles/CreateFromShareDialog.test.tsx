/**
 * Create-from-share-link tests (spec §39).
 *
 * One thing this file does not drive is the base listbox: a Radix Select inside a
 * Radix Dialog never settles under jsdom (the test times out in teardown however
 * the popup is opened), so what is covered here is that the picker is rendered
 * from the backend's list, that the TUN base warns about the elevation prompt, and
 * that the request carries a base. Which base produces which document — including
 * every declared base — is covered by the Go suite, where the generation actually
 * happens (`TestListShareLinkBasesDeclaresWhatCanBeGenerated`,
 * `TestGeneratedConfigurationsPassTheManagedValidator`).
 *
 * The dialog is the create-time counterpart of the outbounds-tab paste dialog: it
 * must let the user check what the backend understood *before* a profile exists,
 * send exactly what was pasted plus the base that was chosen, and keep the dialog
 * open with the backend's code when the generation is refused.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'
import { AppProviders } from '@/app/providers'
import { CreateFromShareDialog } from '@/features/profiles/CreateFromShareDialog'

const mocks = vi.hoisted(() => ({
  shareApi: { schemes: vi.fn(), parseMany: vi.fn(), parseOne: vi.fn(), build: vi.fn() },
  profilesApi: { listShareLinkBases: vi.fn(), createFromShareLinks: vi.fn() },
}))

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  shareApi: mocks.shareApi,
  profilesApi: mocks.profilesApi,
  configApi: {},
  binaryApi: {},
  runtimeApi: {},
  trafficApi: {},
  settingsApi: {},
}))

const VLESS = 'vless://11111111-2222-3333-4444-555555555555@example.com:443?security=tls#Москва'

afterEach(() => {
  cleanup()
  // A modal leaves `pointer-events: none` on <body>; clear it so the next render
  // starts from a clean document.
  document.body.style.pointerEvents = ''
})

beforeEach(() => {
  vi.clearAllMocks()
  mocks.profilesApi.listShareLinkBases.mockResolvedValue({
    bases: [
      {
        id: 'tun',
        name: 'VPN (TUN device)',
        description: 'System traffic goes through the server from the link.',
        requiresPrivilege: true,
        templateId: 'tun-vless-reality',
      },
      {
        id: 'local',
        name: 'Local port only (no TUN)',
        description: 'No TUN device: a local mixed port.',
        requiresPrivilege: false,
        templateId: 'socks-local',
      },
    ],
  })
  mocks.shareApi.parseMany.mockResolvedValue({
    parsed: [
      {
        tag: 'Москва',
        kind: 'vless',
        displayName: 'Москва',
        outbound: { type: 'vless', tag: 'Москва', server: 'example.com', server_port: 443 },
        warnings: [],
      },
    ],
    errors: [],
  })
  mocks.profilesApi.createFromShareLinks.mockResolvedValue({
    profile: { id: 'p-1', name: 'Москва' },
    kind: 'vless',
    count: 1,
    generatedName: 'Москва',
    warnings: [],
  })
})

function renderDialog() {
  const onCreated = vi.fn()
  const onOpenChange = vi.fn()
  const view = render(
    <AppProviders>
      <CreateFromShareDialog open onOpenChange={onOpenChange} onCreated={onCreated} />
    </AppProviders>,
  )
  return { ...view, onCreated, onOpenChange }
}

async function paste(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  // The textarea lives inside a modal (Radix sets `pointer-events: none` on the
  // document), so the paste is simulated with a change event.
  fireEvent.change(screen.getByLabelText('Ссылки'), { target: { value: VLESS } })
  await user.click(screen.getByRole('button', { name: 'Проверить ссылки' }))
  await waitFor(() => {
    expect(screen.getByText('Разобрано: 1.')).toBeInTheDocument()
  })
}

describe('CreateFromShareDialog', () => {
  it('offers the bases the backend declared and shows what the link became', async () => {
    const user = userEvent.setup({ delay: null })
    renderDialog()

    // The list comes from the backend, including the privilege note of the TUN base.
    await waitFor(() => {
      expect(screen.getByLabelText('Конфигурация')).toHaveTextContent('VPN (TUN device)')
    })
    expect(screen.getByText('Понадобятся права администратора')).toBeInTheDocument()

    await paste(user)
    const row = screen
      .getAllByRole('row')
      .find((item) => within(item).queryAllByText('Москва').length > 0)
    expect(row).toBeDefined()
    expect(within(row as HTMLElement).getByText('vless')).toBeInTheDocument()

    // Checking is not creating: no profile exists yet.
    expect(mocks.profilesApi.createFromShareLinks).not.toHaveBeenCalled()
  })

  it('lets the link decide the name when the user types none', async () => {
    const user = userEvent.setup({ delay: null })
    renderDialog()
    await waitFor(() => {
      expect(screen.getByLabelText('Конфигурация')).toHaveTextContent('VPN (TUN device)')
    })

    await paste(user)
    // The hint and the placeholder name the profile the backend would derive.
    expect(screen.getByText('Пустое имя: профиль назовётся «Москва».')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Создать профиль' }))
    await waitFor(() => {
      expect(mocks.profilesApi.createFromShareLinks).toHaveBeenCalledWith({
        name: '',
        description: '',
        links: VLESS,
        base: 'tun',
      })
    })
  })

  it('keeps the dialog open and shows the backend code when the paste is refused', async () => {
    mocks.profilesApi.createFromShareLinks.mockRejectedValue(
      Object.assign(new Error('the links are unusable'), { code: 'SHARE_LINK_INVALID' }),
    )
    const user = userEvent.setup({ delay: null })
    const { onCreated } = renderDialog()
    await waitFor(() => {
      expect(screen.getByLabelText('Конфигурация')).toHaveTextContent('VPN (TUN device)')
    })

    await paste(user)
    await user.click(screen.getByRole('button', { name: 'Создать профиль' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('SHARE_LINK_INVALID')
    })
    expect(screen.getByRole('alert')).toHaveTextContent('Ссылка не распознана')
    expect(onCreated).not.toHaveBeenCalled()
  })

  it('does not call the backend before the user asks', async () => {
    renderDialog()
    expect(mocks.profilesApi.createFromShareLinks).not.toHaveBeenCalled()
    expect(mocks.shareApi.parseMany).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Создать профиль' })).toBeDisabled()
  })
})
