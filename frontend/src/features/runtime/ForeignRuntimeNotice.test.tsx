/**
 * A sing-box that outlived the window (ADR 012).
 *
 * The notice is the only affordance for the failure it answers: the process is
 * named, and the one action that helps — stopping it through the privileged
 * helper — is offered where the user is looking. Without a run record there is
 * nothing to stop, and the notice says so instead of offering a button that
 * cannot work.
 */
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'

import { ForeignRuntimeNotice } from './ForeignRuntimeNotice'

const mocks = vi.hoisted(() => ({ status: vi.fn(), stopForeign: vi.fn() }))

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  runtimeApi: {
    status: () => mocks.status(),
    stopForeignProcesses: () => mocks.stopForeign(),
  },
}))

function payload(foreign: unknown[]) {
  return {
    status: { state: 'STOPPED' },
    activeProfileName: '',
    binaryVersion: '',
    trafficAvailable: false,
    shuttingDown: false,
    foreignProcesses: foreign,
  }
}

function renderNotice() {
  return render(
    <AppProviders queryClient={createAppQueryClient()}>
      <ForeignRuntimeNotice />
    </AppProviders>,
  )
}

beforeEach(() => {
  mocks.status.mockReset()
  mocks.stopForeign.mockReset()
})

describe('foreign runtime notice', () => {
  it('names the process that outlived the application', async () => {
    mocks.status.mockResolvedValue(
      payload([
        {
          pid: 4242,
          revisionId: 'rev-hy',
          pidPath: '/data/run/rev-hy/sing-box.pid',
          binaryPath: '/data/bin/sing-box/1.14.0/sing-box',
          startedAt: '2026-09-14T13:26:12.565087Z',
        },
      ]),
    )

    renderNotice()

    expect(await screen.findByText(/Ядро sing-box работает вне приложения/)).toBeInTheDocument()
    expect(screen.getByText(/— процесс запущен не этим экземпляром/)).toBeInTheDocument()
    expect(screen.getByText(/ревизия.*rev-hy/)).toBeInTheDocument()
  })

  it('stops the process and hides itself once it is gone', async () => {
    const user = userEvent.setup({ delay: null })
    mocks.status.mockResolvedValue(
      payload([
        {
          pid: 4242,
          revisionId: 'rev-hy',
          pidPath: '/data/run/rev-hy/sing-box.pid',
          binaryPath: '/data/bin/sing-box/1.14.0/sing-box',
          startedAt: '2026-09-14T13:26:12.565087Z',
        },
      ]),
    )
    mocks.stopForeign.mockResolvedValue(payload([]))

    renderNotice()
    await user.click(await screen.findByRole('button', { name: 'Остановить' }))

    expect(mocks.stopForeign).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/Ядро sing-box работает вне приложения/)).not.toBeInTheDocument()
  })

  it('offers nothing to stop for a process started by hand', async () => {
    mocks.status.mockResolvedValue(
      payload([
        {
          pid: 4243,
          revisionId: '',
          pidPath: '',
          binaryPath: '',
          startedAt: '',
        },
      ]),
    )

    renderNotice()

    expect(await screen.findByText(/— процесс запущен не этим экземпляром/)).toHaveTextContent(
      '4243',
    )
    expect(screen.queryByRole('button', { name: 'Остановить' })).not.toBeInTheDocument()
    expect(screen.getByText(/Остановите его там, где он был запущен/)).toBeInTheDocument()
  })

  it('renders nothing when no process is running outside the application', async () => {
    mocks.status.mockResolvedValue(payload([]))

    const { container } = renderNotice()

    await waitFor(() => {
      expect(mocks.status).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })
})
