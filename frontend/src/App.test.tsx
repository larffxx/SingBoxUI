/**
 * App smoke test.
 *
 * The shell is rendered *outside* the Wails webview on purpose: that is the
 * degraded case the app has to survive (no Go bindings, no events, no data), so
 * the assertions are about the shell itself rather than about any payload.
 */
import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { App } from './App'

vi.mock('@/shared/api/bindings', () => ({ isDesktopRuntime: () => false }))

describe('App', () => {
  it('renders the shell with navigation, the overview screen and the unavailable notice', async () => {
    render(<App />)

    expect(await screen.findByRole('heading', { level: 1, name: 'Обзор' })).toBeInTheDocument()

    for (const label of ['Обзор', 'Профили', 'Рантайм', 'Настройки', 'О программе']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }

    const notices = screen.getByTestId('notice-surface')
    expect(within(notices).getByText('Бэкенд недоступен')).toBeInTheDocument()
    expect(screen.getByText('Бэкенд недоступен — только интерфейс')).toBeInTheDocument()
  })
})
