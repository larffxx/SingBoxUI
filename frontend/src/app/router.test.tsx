/**
 * Shell navigation test.
 *
 * Renders the real provider stack and the real route tree over a `memory`
 * history, then drives the sidebar the way a user would: every top-level
 * destination must be reachable and must become the active item.
 */
import { createMemoryHistory } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'
import { AppRouterView } from '@/app/router'
import { createAppRouter } from '@/app/routeTree'
import { NAV_ITEMS, isNavItemActive } from '@/app/shell/nav'

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => false,
  profilesApi: { list: () => Promise.resolve({ profiles: [], activeId: '' }) },
  runtimeApi: { status: () => Promise.resolve({ status: { state: 'STOPPED' } }) },
}))

function renderAt(path: string) {
  const router = createAppRouter(createMemoryHistory({ initialEntries: [path] }))
  render(
    <AppProviders queryClient={createAppQueryClient()}>
      <AppRouterView router={router} />
    </AppProviders>,
  )
  return router
}

describe('app router', () => {
  it('exposes the five top-level destinations', () => {
    expect(NAV_ITEMS.map((item) => item.label)).toEqual([
      'Обзор',
      'Профили',
      'Рантайм',
      'Настройки',
      'О программе',
    ])
  })

  it('navigates from the overview screen to every other destination', async () => {
    const router = renderAt('/')
    expect(await screen.findByRole('heading', { level: 1, name: 'Обзор' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('link', { name: 'Рантайм' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Рантайм' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/runtime')

    await userEvent.click(screen.getByRole('link', { name: 'Настройки' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Настройки' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/settings')

    await userEvent.click(screen.getByRole('link', { name: 'О программе' }))
    expect(
      await screen.findByRole('heading', { level: 1, name: 'О программе' }),
    ).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/about')

    await userEvent.click(screen.getByRole('link', { name: 'Обзор' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Обзор' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/')
  })

  it('renders the profile routes owned by the profiles workstream', async () => {
    renderAt('/profiles')
    expect(await screen.findByRole('link', { name: 'Профили' })).toHaveAttribute(
      'aria-current',
      'page',
    )
  })

  it('marks a nav item active for its own path and for nested paths', () => {
    const profiles = { to: '/profiles', exact: false } as const
    expect(isNavItemActive(profiles, '/profiles')).toBe(true)
    expect(isNavItemActive(profiles, '/profiles/abc')).toBe(true)
    expect(isNavItemActive(profiles, '/runtimes')).toBe(false)

    const overview = { to: '/', exact: true } as const
    expect(isNavItemActive(overview, '/')).toBe(true)
    expect(isNavItemActive(overview, '/runtime')).toBe(false)
  })
})
