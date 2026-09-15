/**
 * Application-level flow test (spec §52: "Playwright where practical for
 * application-level flows").
 *
 * The real end-user surface of this application is the Wails webview, which a
 * browser cannot drive: the Go bindings only exist inside it. What *is* worth
 * covering in a browser is the shell itself against the real bundle — routing,
 * providers, the layout and the notice surface — and that is exactly the path
 * that must keep working when the bindings are absent (`vite dev` in a browser,
 * and any future preview build). Vitest covers the same components against
 * mocked modules in jsdom; this test covers them in a real browser, where no
 * mock can hide a missing export, a broken CSS build or a bad asset path.
 */
import { expect, test } from '@playwright/test'

test('the shell renders, routes and degrades gracefully without the Wails runtime', async ({
  page,
}) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(String(error)))

  await page.goto('/')

  // The shell renders the five top-level destinations (spec §52) and nothing
  // threw while booting in a browser that has no `window.go`.
  const nav = page.getByRole('navigation')
  for (const label of ['Обзор', 'Профили', 'Рантайм', 'Настройки', 'О программе']) {
    await expect(nav.getByRole('link', { name: label })).toBeVisible()
  }

  // Without the bindings the UI states the truth instead of showing empty data.
  const notice = page.getByTestId('notice-surface')
  await expect(notice).toBeVisible()
  await expect(notice).toContainText('Бэкенд недоступен')

  // Client-side routing works in the browser: no reload, URL and screen change.
  await nav.getByRole('link', { name: 'О программе' }).click()
  await expect(page).toHaveURL(/\/about$/)
  await expect(page.getByRole('link', { name: 'О программе' })).toHaveAttribute(
    'aria-current',
    'page',
  )

  expect(consoleErrors, `unexpected console errors:\n${consoleErrors.join('\n')}`).toEqual([])
})
