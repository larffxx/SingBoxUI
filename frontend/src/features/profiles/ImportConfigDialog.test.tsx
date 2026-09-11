/**
 * Configuration import (spec §11, §65, §66).
 *
 * A user must be able to hand the app a sing-box configuration that lives
 * anywhere on disk, on any platform. The two bindings that do it are asserted
 * here end to end: the picker returns a path (a dismissed dialog is not an
 * error), and the import copies the file into a new profile while leaving the
 * original alone.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

import '@/app/testing/environment'

import { ImportConfigDialog } from './ImportConfigDialog'

const mocks = vi.hoisted(() => ({
  pickConfigFile: vi.fn(),
  importConfigFile: vi.fn(),
}))

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  configApi: {
    pickConfigFile: mocks.pickConfigFile,
    importConfigFile: mocks.importConfigFile,
  },
}))

function renderDialog(props: Partial<React.ComponentProps<typeof ImportConfigDialog>> = {}) {
  const onImported = vi.fn()
  const onOpenChange = vi.fn()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const utils = render(
    <QueryClientProvider client={client}>
      <ImportConfigDialog
        open
        onOpenChange={onOpenChange}
        onImported={onImported}
        {...props}
      />
    </QueryClientProvider>,
  )
  return { ...utils, onImported, onOpenChange }
}

describe('import configuration from a file', () => {
  it('uses the native picker to choose the file and imports the chosen path', async () => {
    mocks.pickConfigFile.mockResolvedValue({ path: '/Users/test/Downloads/config.json', canceled: false })
    mocks.importConfigFile.mockResolvedValue({
      profile: { id: 'profile-imported', name: 'config' },
    })
    const user = userEvent.setup({ delay: null })
    const { onImported } = renderDialog()

    await user.click(screen.getByRole('button', { name: /Выбрать файл/ }))

    await waitFor(() => {
      expect(screen.getByLabelText(/Путь к файлу конфигурации/)).toHaveValue(
        '/Users/test/Downloads/config.json',
      )
    })
    // The name field is filled from the file name so the import needs no typing.
    expect(screen.getByLabelText(/Название профиля/)).toHaveValue('config')

    await user.click(screen.getByRole('button', { name: 'Импортировать' }))

    await waitFor(() => {
      expect(mocks.importConfigFile).toHaveBeenCalledWith({
        path: '/Users/test/Downloads/config.json',
        name: 'config',
      })
    })
    await waitFor(() => {
      expect(onImported).toHaveBeenCalledWith(expect.objectContaining({ id: 'profile-imported' }))
    })
  })

  it('accepts a typed path when the picker cannot be used', async () => {
    mocks.importConfigFile.mockResolvedValue({ profile: { id: 'profile-2', name: 'Свой' } })
    const user = userEvent.setup({ delay: null })
    renderDialog()

    await user.type(
      screen.getByLabelText(/Путь к файлу конфигурации/),
      'C:\\Users\\me\\config.json',
    )
    // The typed path already filled the name from the file name; the user overrides it.
    const nameField = screen.getByLabelText(/Название профиля/)
    await user.clear(nameField)
    await user.type(nameField, 'Свой')
    await user.click(screen.getByRole('button', { name: 'Импортировать' }))

    await waitFor(() => {
      expect(mocks.importConfigFile).toHaveBeenCalledWith({
        path: 'C:\\Users\\me\\config.json',
        name: 'Свой',
      })
    })
  })

  it('keeps the dialog open and shows the backend failure', async () => {
    const failure = Object.assign(new Error('no sing-box configuration'), {
      code: 'NOT_FOUND',
    })
    mocks.importConfigFile.mockRejectedValue(failure)
    const user = userEvent.setup({ delay: null })
    const { onImported } = renderDialog()

    await user.type(screen.getByLabelText(/Путь к файлу конфигурации/), '/tmp/missing.json')
    await user.click(screen.getByRole('button', { name: 'Импортировать' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('NOT_FOUND')
    })
    // The backend message is English; the flow adds the Russian hint.
    expect(screen.getByRole('alert')).toHaveTextContent('Проверьте путь')
    expect(onImported).not.toHaveBeenCalled()
  })

  it('treats a dismissed picker as a no-op', async () => {
    mocks.pickConfigFile.mockResolvedValue({ path: '', canceled: true })
    const user = userEvent.setup({ delay: null })
    renderDialog()

    await user.click(screen.getByRole('button', { name: /Выбрать файл/ }))

    await waitFor(() => {
      expect(mocks.pickConfigFile).toHaveBeenCalled()
    })
    expect(screen.getByLabelText(/Путь к файлу конфигурации/)).toHaveValue('')
    expect(mocks.importConfigFile).not.toHaveBeenCalled()
    expect(screen.queryByRole('alert')).toBeNull()
  })
})
