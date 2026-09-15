/**
 * Applications card tests (ADR 011, ADR 013).
 *
 * The card is the only place where a user adds a program to the routing rules, so
 * what is covered here is the round trip: the row reads the state out of the
 * document, and a click writes the rule the platform declared for that program —
 * a process path on macOS, a process name on Windows. A machine without a
 * catalog explains itself instead of failing.
 */
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import * as React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'

import { ApplicationsCard } from './ApplicationsCard'
import type { JsonObject } from '../jsonDoc'

const TELEGRAM = '^/Applications/Telegram\\\\.app/'
const DISCORD = '^/Applications/Discord\\\\.app/'

const supportedPayload = {
  apps: {
    supported: true,
    reason: '',
    applications: [
      {
        name: 'Telegram',
        bundleId: 'ru.keepcoder.Telegram',
        path: '/Applications/Telegram.app',
        executable: 'Telegram',
        matchKey: 'process_path_regex',
        matchValue: TELEGRAM,
      },
      {
        name: 'Discord',
        bundleId: 'com.hnc.Discord',
        path: '/Applications/Discord.app',
        executable: 'Discord',
        matchKey: 'process_path_regex',
        matchValue: DISCORD,
      },
    ],
  },
}

/** The listing a Windows machine answers: the condition is the file name. */
const windowsPayload = {
  apps: {
    supported: true,
    reason: '',
    applications: [
      {
        name: 'Google Chrome',
        bundleId: '',
        path: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
        executable: 'chrome.exe',
        matchKey: 'process_name',
        matchValue: 'chrome.exe',
      },
      {
        name: 'Telegram Desktop',
        bundleId: '',
        path: 'C:\\Users\\someone\\AppData\\Roaming\\Telegram Desktop\\Telegram.exe',
        executable: 'Telegram.exe',
        matchKey: 'process_name',
        matchValue: 'Telegram.exe',
      },
    ],
  },
}

/** A machine with more programs than the list shows before it offers to unfold. */
const manyPayload = {
  apps: {
    supported: true,
    reason: '',
    applications: Array.from({ length: 7 }, (_, index) => ({
      name: `Program ${index + 1}`,
      bundleId: '',
      path: `/Applications/Program${index + 1}.app`,
      executable: `Program${index + 1}`,
      matchKey: 'process_path_regex',
      matchValue: `^/Applications/Program${index + 1}\\\\.app/`,
    })),
  },
}

const mocks = vi.hoisted(() => ({ list: vi.fn() }))

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  appsApi: { list: () => mocks.list() },
}))

function baseDocument(rules: JsonObject[] = []): JsonObject {
  return {
    outbounds: [
      { type: 'vless', tag: 'proxy-vless' },
      { type: 'direct', tag: 'direct' },
    ],
    route: { final: 'proxy-vless', rules },
  }
}

/**
 * renderCard mounts the card the way the workspace does: the document the card
 * reports comes back as its next input, so a click changes what the next click
 * sees.
 */
async function renderCard(initial: JsonObject, waitFor = 'Telegram') {
  const onChange = vi.fn()
  function Harness() {
    const [document, setDocument] = React.useState(initial)
    return (
      <ApplicationsCard
        root={document}
        onChange={(next) => {
          onChange(next)
          setDocument(next)
        }}
      />
    )
  }
  render(
    <AppProviders queryClient={createAppQueryClient()}>
      <Harness />
    </AppProviders>,
  )
  await screen.findByText(waitFor)
  return onChange
}

/** lastDocument is the document the card last reported to the workspace. */
function lastDocument(onChange: ReturnType<typeof vi.fn>): JsonObject {
  const call = onChange.mock.calls.at(-1)
  if (!call) {
    throw new Error('the card reported no change')
  }
  return call[0] as JsonObject
}

function rulesOf(document: JsonObject): JsonObject[] {
  const route = document.route as JsonObject
  return route.rules as JsonObject[]
}

beforeEach(() => {
  mocks.list.mockReset()
  mocks.list.mockResolvedValue(supportedPayload)
})

describe('applications card', () => {
  it('shows every application as going through the tunnel by default', async () => {
    // The configuration routes everything through the tunnel (route.final = proxy-vless), so the
    // absence of a rule means "через VPN" and not "не задано": a rule is the exception.
    await renderCard(baseDocument())

    for (const name of ['Telegram', 'Discord']) {
      const row = screen.getByRole('group', { name })
      expect(within(row).getByRole('button', { name: 'Через VPN' })).toHaveAttribute(
        'aria-pressed',
        'true',
      )
      expect(within(row).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
        'aria-pressed',
        'false',
      )
    }
  })

  it('shows an application the user routed directly as direct, and the rest as through the tunnel', async () => {
    await renderCard(
      baseDocument([{ action: 'route', outbound: 'direct', process_path_regex: [DISCORD] }]),
    )

    const telegram = screen.getByRole('group', { name: 'Telegram' })
    const discord = screen.getByRole('group', { name: 'Discord' })
    expect(within(telegram).getByRole('button', { name: 'Через VPN' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(within(discord).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('reads the default from the configuration when the final is not the tunnel', async () => {
    // A profile that routes everything directly (the TUN-basic starter, say): the row
    // says "напрямую" until the program gets a rule of its own — and «Через VPN» is how
    // a program is taken *into* the tunnel there.
    const user = userEvent.setup({ delay: null })
    const directFinal: JsonObject = {
      outbounds: [
        { type: 'vless', tag: 'proxy-vless' },
        { type: 'direct', tag: 'direct' },
      ],
      route: { final: 'direct', rules: [] },
    }
    const onChange = await renderCard(directFinal)

    const row = screen.getByRole('group', { name: 'Telegram' })
    expect(within(row).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )

    await user.click(within(row).getByRole('button', { name: 'Через VPN' }))
    expect(rulesOf(lastDocument(onChange))).toEqual([
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM] },
    ])
    expect(within(row).getByRole('button', { name: 'Через VPN' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('shows five applications and unfolds the rest on demand', async () => {
    mocks.list.mockResolvedValue(manyPayload)
    const user = userEvent.setup({ delay: null })
    await renderCard(baseDocument(), 'Program 1')

    // Five rows, and a button that names how many there are in total.
    expect(screen.getAllByRole('group')).toHaveLength(5)
    expect(screen.queryByText('Program 6')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Показать все (7)' }))

    expect(screen.getAllByRole('group')).toHaveLength(7)
    expect(screen.getByText('Program 7')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Свернуть' }))
    expect(screen.getAllByRole('group')).toHaveLength(5)
  })

  it('writes no rule for an application that already follows the configuration default', async () => {
    // The configuration routes everything through the tunnel, so pressing "Через VPN"
    // on a program without a rule asks for what is already true: nothing is written.
    const user = userEvent.setup({ delay: null })
    const onChange = await renderCard(baseDocument())

    await user.click(
      within(screen.getByRole('group', { name: 'Telegram' })).getByRole('button', {
        name: 'Через VPN',
      }),
    )

    expect(rulesOf(lastDocument(onChange))).toEqual([])
  })

  it('writes a rule that selects the application, not an address', async () => {
    const user = userEvent.setup({ delay: null })
    const onChange = await renderCard(baseDocument())

    await user.click(
      within(screen.getByRole('group', { name: 'Telegram' })).getByRole('button', {
        name: 'Напрямую',
      }),
    )

    expect(rulesOf(lastDocument(onChange))).toEqual([
      { action: 'route', outbound: 'direct', process_path_regex: [TELEGRAM] },
    ])
  })

  it('writes the condition the platform declared for a program', async () => {
    // Windows selects a program by its file name; the card takes the condition
    // from the listing and never derives one itself (ADR 013).
    mocks.list.mockResolvedValue(windowsPayload)
    const user = userEvent.setup({ delay: null })
    const onChange = await renderCard(baseDocument(), 'Google Chrome')

    await user.click(
      within(screen.getByRole('group', { name: 'Google Chrome' })).getByRole('button', {
        name: 'Напрямую',
      }),
    )

    expect(rulesOf(lastDocument(onChange))).toEqual([
      { action: 'route', outbound: 'direct', process_name: ['chrome.exe'] },
    ])
    expect(screen.getByText(/process_name/)).toBeInTheDocument()
  })

  it('routes an application directly and back again', async () => {
    const user = userEvent.setup({ delay: null })
    const onChange = await renderCard(baseDocument())

    const row = screen.getByRole('group', { name: 'Discord' })
    await user.click(within(row).getByRole('button', { name: 'Напрямую' }))
    expect(rulesOf(lastDocument(onChange))).toEqual([
      {
        action: 'route',
        outbound: 'direct',
        process_path_regex: [DISCORD],
      },
    ])
    expect(within(row).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )

    await user.click(within(row).getByRole('button', { name: 'Напрямую' }))
    expect(rulesOf(lastDocument(onChange))).toEqual([])
    // Removing the exception puts the program back on the configuration's default,
    // which is the tunnel — the row says so.
    expect(within(row).getByRole('button', { name: 'Через VPN' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(within(row).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
      'aria-pressed',
      'false',
    )
  })

  it('shows the state the document describes', async () => {
    await renderCard(
      baseDocument([{ action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM] }]),
    )

    const row = screen.getByRole('group', { name: 'Telegram' })
    expect(within(row).getByRole('button', { name: 'Через VPN' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(within(row).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
      'aria-pressed',
      'false',
    )
  })

  it('switches one application without moving the others of the same rule', async () => {
    // The regression the user reported: one rule held several programs, and
    // switching one of them rewrote the outbound of that whole rule, so every
    // program in it changed direction.
    const user = userEvent.setup({ delay: null })
    const onChange = await renderCard(
      baseDocument([
        { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM, DISCORD] },
      ]),
    )

    await user.click(
      within(screen.getByRole('group', { name: 'Discord' })).getByRole('button', {
        name: 'Напрямую',
      }),
    )

    expect(rulesOf(lastDocument(onChange))).toEqual([
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM] },
      { action: 'route', outbound: 'direct', process_path_regex: [DISCORD] },
    ])

    // The rows read back what the document now says: only Discord is direct.
    const telegram = screen.getByRole('group', { name: 'Telegram' })
    const discord = screen.getByRole('group', { name: 'Discord' })
    expect(within(telegram).getByRole('button', { name: 'Через VPN' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(within(discord).getByRole('button', { name: 'Напрямую' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('explains a platform that has no catalog to offer', async () => {
    mocks.list.mockResolvedValue({
      apps: {
        supported: false,
        reason: 'эта система не даёт список установленных программ',
        applications: [],
      },
    })

    render(
      <AppProviders queryClient={createAppQueryClient()}>
        <ApplicationsCard root={baseDocument()} onChange={vi.fn()} />
      </AppProviders>,
    )

    expect(await screen.findByText(/не даёт список установленных программ/)).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Telegram' })).not.toBeInTheDocument()
  })

  it('filters the list by name', async () => {
    const user = userEvent.setup({ delay: null })
    await renderCard(baseDocument())

    await user.type(screen.getByLabelText('Поиск'), 'disc')

    expect(screen.queryByText('Telegram')).not.toBeInTheDocument()
    expect(screen.getByText('Discord')).toBeInTheDocument()
  })

  it('filters the list by the file a program is selected by', async () => {
    // On Windows the row is a program whose name the user may not know: the
    // path and the executable are searchable as well.
    mocks.list.mockResolvedValue(windowsPayload)
    const user = userEvent.setup({ delay: null })
    await renderCard(baseDocument(), 'Google Chrome')

    await user.type(screen.getByLabelText('Поиск'), 'chrome.exe')

    expect(screen.getByText('Google Chrome')).toBeInTheDocument()
    expect(screen.queryByText('Telegram Desktop')).not.toBeInTheDocument()
  })
})
