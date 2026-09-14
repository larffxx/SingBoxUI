/**
 * Applications card tests (ADR 011).
 *
 * The card is the only place where a user adds a program to the routing rules, so
 * what is covered here is the round trip: the row reads the state out of the
 * document, and a click writes a rule that selects the application by its
 * process path. A machine without a catalog explains itself instead of failing.
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

const TELEGRAM = '^/Applications/Telegram\\.app/'

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
        processPathRegex: TELEGRAM,
      },
      {
        name: 'Discord',
        bundleId: 'com.hnc.Discord',
        path: '/Applications/Discord.app',
        executable: 'Discord',
        processPathRegex: '^/Applications/Discord\\.app/',
      },
    ],
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
async function renderCard(initial: JsonObject) {
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
  await screen.findByText('Telegram')
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
  it('writes a rule that selects the application, not an address', async () => {
    const user = userEvent.setup({ delay: null })
    const onChange = await renderCard(baseDocument())

    await user.click(
      within(screen.getByRole('group', { name: 'Telegram' })).getByRole('button', {
        name: 'Через VPN',
      }),
    )

    expect(rulesOf(lastDocument(onChange))).toEqual([
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM] },
    ])
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
        process_path_regex: ['^/Applications/Discord\\.app/'],
      },
    ])

    await user.click(within(row).getByRole('button', { name: 'Напрямую' }))
    expect(rulesOf(lastDocument(onChange))).toEqual([])
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

  it('explains a platform that has no catalog to offer', async () => {
    mocks.list.mockResolvedValue({
      apps: {
        supported: false,
        reason: 'Windows не предоставляет список установленных программ',
        applications: [],
      },
    })

    render(
      <AppProviders queryClient={createAppQueryClient()}>
        <ApplicationsCard root={baseDocument()} onChange={vi.fn()} />
      </AppProviders>,
    )

    expect(await screen.findByText(/Windows не предоставляет список/)).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Telegram' })).not.toBeInTheDocument()
  })

  it('filters the list by name', async () => {
    const user = userEvent.setup({ delay: null })
    await renderCard(baseDocument())

    await user.type(screen.getByLabelText('Поиск'), 'disc')

    expect(screen.queryByText('Telegram')).not.toBeInTheDocument()
    expect(screen.getByText('Discord')).toBeInTheDocument()
  })
})
