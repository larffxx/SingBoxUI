/**
 * Log viewer tests (spec §22, §23).
 *
 * The viewer is presentation over `logs.ts`, so these tests check the two
 * behaviours a user actually depends on: the level select hides everything below
 * the chosen severity, and the rendered DOM is capped at
 * `LOG_VIEWER_RENDER_CAP` lines (newest kept) instead of growing with a chatty
 * sing-box build.
 */
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import '@/app/testing/environment'

import type { runtime } from '@/shared/api/bindings'

import { LogViewer } from './LogViewer'
import { LOG_VIEWER_RENDER_CAP } from './logs'

/** Builds a record shaped like `RuntimeAPI.GetLogs` returns. */
function record(seq: number, level: string, message: string): runtime.LogRecord {
  return {
    seq,
    time: '2026-09-11T10:00:00Z',
    source: 'sing-box',
    level,
    message,
  } as unknown as runtime.LogRecord
}

const RECORDS: runtime.LogRecord[] = [
  record(1, 'DEBUG', 'старт загрузки конфигурации'),
  record(2, 'INFO', 'входящий канал поднят'),
  record(3, 'WARN', 'устаревшая директива в конфигурации'),
  record(4, 'ERROR', 'не удалось подключиться к серверу'),
]

describe('LogViewer', () => {
  it('hides records below the selected level', () => {
    render(<LogViewer records={RECORDS} initialLevel="ERROR" />)

    const body = screen.getByTestId('log-body')
    expect(body.querySelectorAll('[data-level]')).toHaveLength(1)
    expect(screen.getByText(/не удалось подключиться к серверу/)).toBeInTheDocument()
    expect(screen.queryByText(/устаревшая директива/)).not.toBeInTheDocument()
    expect(body.querySelectorAll('[data-level="DEBUG"]')).toHaveLength(0)
  })

  it('caps the rendered lines at the view cap and keeps the newest ones', () => {
    const overflow = 3
    const many = Array.from({ length: LOG_VIEWER_RENDER_CAP + overflow }, (_, index) =>
      record(index + 1, 'INFO', `строка ${index + 1}`),
    )

    render(<LogViewer records={many} />)

    const body = screen.getByTestId('log-body')
    expect(body.querySelectorAll('[data-level]')).toHaveLength(LOG_VIEWER_RENDER_CAP)
    expect(screen.getByTestId('log-counter')).toHaveTextContent(
      `Показано ${LOG_VIEWER_RENDER_CAP} из ${many.length}`,
    )
    expect(screen.getByTestId('log-counter')).toHaveTextContent(
      `скрыто в окне просмотра: ${overflow}`,
    )
    // The oldest lines fall out of the window, the newest stay visible.
    expect(screen.queryByText(/строка 1$/)).not.toBeInTheDocument()
    expect(screen.getByText(/строка 2003/)).toBeInTheDocument()
  })

  it('filters by the search string and reports what the filter hid', async () => {
    render(<LogViewer records={RECORDS} />)

    await userEvent.type(screen.getByLabelText(/поиск/i), 'подключиться')

    expect(screen.getByTestId('log-body').querySelectorAll('[data-level]')).toHaveLength(1)
    expect(screen.getByTestId('log-counter')).toHaveTextContent('скрыто фильтром: 3')
  })

  // This test runs last and drives the select with fireEvent on purpose. Opening
  // Radix's select leaves jsdom's user-event pipeline stalling for about ten
  // seconds on every later interaction in the same file, and a synthesised
  // userEvent click on the trigger pays that stall itself. Opened with a plain
  // pointerdown and picked with a plain click, the test measures the "and above"
  // semantics rather than the widget's event plumbing, and nothing follows it.
  // The timeout is headroom for the two-core runners: mounting the popover is the
  // slowest thing the suite does, and it still finishes in about a second here.
  it('keeps the "and above" semantics when the level changes', () => {
    render(<LogViewer records={RECORDS} initialLevel="ALL" />)

    expect(screen.getByTestId('log-body').querySelectorAll('[data-level]')).toHaveLength(4)

    const trigger = screen.getByRole('combobox', { name: /уровень/i })
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: 'mouse' })
    fireEvent.click(trigger)
    fireEvent.click(screen.getByRole('option', { name: 'WARN и выше' }))

    expect(screen.getByTestId('log-body').querySelectorAll('[data-level]')).toHaveLength(2)
    expect(screen.getByText(/устаревшая директива/)).toBeInTheDocument()
    expect(screen.queryByText(/входящий канал поднят/)).not.toBeInTheDocument()
  }, 15_000)
})
