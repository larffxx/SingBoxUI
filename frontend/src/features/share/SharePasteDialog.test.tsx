/**
 * Share-link import tests (spec §39).
 *
 * The parser lives in the backend, so the dialog must be judged on how it
 * presents an answer: one row per link, the warnings the parser returned for the
 * parameters it could not map, and the lines it refused. Importing is a user
 * action — nothing is written to the profile until the confirm button is used.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppProviders } from '@/app/providers'
import { SharePasteDialog } from '@/features/share/SharePasteDialog'
import { keys } from '@/shared/api/keys'

const { shareApi } = vi.hoisted(() => ({
  shareApi: {
    schemes: vi.fn(),
    parseMany: vi.fn(),
    parseManyOne: vi.fn(),
    build: vi.fn(),
  },
}))

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => false,
  shareApi,
  profilesApi: { list: vi.fn() },
  configApi: {},
  binaryApi: {},
  runtimeApi: {},
  trafficApi: {},
  settingsApi: {},
}))

const VLESS = 'vless://11111111-2222-3333-4444-555555555555@example.com:443?security=tls#node-1'
const TROJAN = 'trojan://secret@example.org:8443?sni=example.org#node-2'

afterEach(() => {
  cleanup()
  // A modal leaves `pointer-events: none` on <body>; clear it so the next render
  // starts from a clean document.
  document.body.style.pointerEvents = ''
})

beforeEach(() => {
  vi.clearAllMocks()
  shareApi.schemes.mockResolvedValue(['vless', 'vmess', 'trojan', 'ss', 'hysteria2', 'tuic'])
  // The parser reports one unmapped parameter per link and refuses one line.
  shareApi.parseMany.mockResolvedValue({
    parsed: [
      {
        tag: 'node-1',
        kind: 'vless',
        displayName: 'example.com:443',
        outbound: {
          type: 'vless',
          server: 'example.com',
          server_port: 443,
          uuid: '11111111-2222-3333-4444-555555555555',
        },
        warnings: ['Параметр «fp» не поддерживается'],
        source: VLESS,
      },
      {
        tag: 'node-2',
        kind: 'trojan',
        displayName: 'example.org:8443',
        outbound: { type: 'trojan', server: 'example.org', server_port: 8443, password: 'secret' },
        warnings: ['Параметр «sni» перенесён в tls.server_name'],
        source: TROJAN,
      },
    ],
    errors: ['Строка 3: неизвестная схема «foo://».'],
  })
})

function renderDialog(existingTags: string[]) {
  const onImported = vi.fn()
  const onOpenChange = vi.fn()
  const view = render(
    <AppProviders>
      <SharePasteDialog
        open
        onOpenChange={onOpenChange}
        existingTags={existingTags}
        onImported={onImported}
      />
    </AppProviders>,
  )
  return { ...view, onImported, onOpenChange }
}

async function paste() {
  const user = userEvent.setup()
  // The textarea lives inside a modal (Radix sets `pointer-events: none` on the
  // document), so the paste is simulated with a change event.
  const area = screen.getByLabelText('Ссылки')
  fireEvent.change(area, { target: { value: `${VLESS}\n${TROJAN}` } })
  await user.click(screen.getByRole('button', { name: 'Разобрать' }))
  await waitFor(() => {
    expect(screen.getByText('Разобрано: 2, выбрано: 2.')).toBeInTheDocument()
  })
  return user
}

describe('SharePasteDialog', () => {
  it('renders the parsed links with their per-link warnings and the refused lines', async () => {
    renderDialog([])
    await paste()

    // One row per link, keyed by the tag the parser resolved.
    const rows = screen.getAllByRole('row')
    const first = rows.find((row) => within(row).queryByText('node-1') !== null)
    const second = rows.find((row) => within(row).queryByText('node-2') !== null)
    expect(first).toBeDefined()
    expect(second).toBeDefined()

    // Warnings are shown against the link they belong to, not aggregated away.
    expect(
      within(first as HTMLElement).getByText('Параметр «fp» не поддерживается'),
    ).toBeInTheDocument()
    expect(
      within(second as HTMLElement).getByText('Параметр «sni» перенесён в tls.server_name'),
    ).toBeInTheDocument()
    expect(within(first as HTMLElement).getByText('vless')).toBeInTheDocument()
    expect(within(second as HTMLElement).getByText('trojan')).toBeInTheDocument()

    // The unparsable line is reported separately and is not importable.
    expect(screen.getByText('Часть строк пропущена')).toBeInTheDocument()
    expect(screen.getByText('Строка 3: неизвестная схема «foo://».')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить (2)' })).toBeEnabled()
  })

  it('adds the parsed outbounds on confirm and renames a colliding tag instead of replacing it', async () => {
    const { onImported, onOpenChange } = renderDialog(['node-1'])
    const user = await paste()

    await user.click(screen.getByRole('button', { name: 'Добавить (2)' }))

    expect(onImported).toHaveBeenCalledTimes(1)
    const [outbounds, notes] = onImported.mock.calls[0] as [Record<string, unknown>[], string[]]
    expect(outbounds).toHaveLength(2)
    expect(outbounds[0]).toMatchObject({ type: 'vless', tag: 'node-1-2', server: 'example.com' })
    expect(outbounds[1]).toMatchObject({ type: 'trojan', tag: 'node-2' })
    expect(notes).toEqual(['Тег «node-1» уже занят — добавлен как «node-1-2».'])
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('does not call the backend until the user asks for a parse', async () => {
    renderDialog([])
    expect(shareApi.parseMany).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Добавить (0)' })).toBeDisabled()
    expect(shareApi.schemes).toHaveBeenCalledWith()
    expect(keys.share.schemes()).toBeDefined()
  })
})
