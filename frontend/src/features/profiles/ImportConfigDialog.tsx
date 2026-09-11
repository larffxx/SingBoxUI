/**
 * Import a sing-box configuration from disk (spec §11, §65, §66).
 *
 * The dialog offers the native picker first and a plain path field as the
 * fallback, so import also works where a dialog cannot open. Whichever way the
 * path arrives, the backend reads exactly that file and copies it into a new
 * profile: the original is never rewritten. A refused import keeps the dialog
 * open with the backend's error code and message, because the user still has to
 * fix the path.
 */
import { FolderInput } from 'lucide-react'
import * as React from 'react'

import { useImportConfigFileMutation } from '@/app/queries'
import { configApi, isDesktopRuntime, type profile } from '@/shared/api/bindings'
import {
  describeError,
  hintFor,
  toAppError,
  type AppError,
  type ErrorCode,
} from '@/shared/api/errors'
import { Alert, Button, Dialog, DialogContent, Field, Input, Spinner } from '@/shared/ui'

/**
 * Codes this flow can produce before the profile exists, with no global hint:
 * they are about the file the user pointed at, not about a profile.
 */
const IMPORT_HINTS: Partial<Record<ErrorCode, string>> = {
  NOT_FOUND: 'Проверьте путь: файл должен существовать и быть доступным для чтения.',
  INVALID_ARGUMENT: 'Путь ведёт не на файл конфигурации, либо файл слишком большой.',
  INTERNAL_ERROR: 'Не удалось прочитать файл. Проверьте права доступа.',
}

export function ImportConfigDialog({
  open,
  onOpenChange,
  onImported,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImported: (imported: profile.Profile) => void
}): React.ReactElement {
  const importConfig = useImportConfigFileMutation()
  const resetImport = importConfig.reset
  const desktop = isDesktopRuntime()

  const [path, setPath] = React.useState('')
  const [name, setName] = React.useState('')
  const [nameEdited, setNameEdited] = React.useState(false)
  const [picking, setPicking] = React.useState(false)
  const [pickFailure, setPickFailure] = React.useState<AppError | null>(null)

  React.useEffect(() => {
    if (!open) return
    setPath('')
    setName('')
    setNameEdited(false)
    setPickFailure(null)
    resetImport()
  }, [open, resetImport])

  const pick = async (): Promise<void> => {
    setPickFailure(null)
    setPicking(true)
    try {
      const chosen = await configApi.pickConfigFile()
      if (chosen.canceled === true || chosen.path === '') return
      setPath(chosen.path)
      // Only a name the user typed is authoritative; otherwise the file decides.
      if (!nameEdited) setName(profileNameFromPath(chosen.path))
    } catch (error) {
      setPickFailure(toAppError(error))
    } finally {
      setPicking(false)
    }
  }

  const failure = importConfig.error === null ? null : toAppError(importConfig.error)
  const failureHint = failure === null ? undefined : (hintFor(failure.code) ?? IMPORT_HINTS[failure.code])
  const trimmedPath = path.trim()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Импорт конфигурации из файла"
        description="Профиль создаётся из копии файла. Исходный файл остаётся нетронутым — приложение пишет только в свой каталог."
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (trimmedPath === '') return
            importConfig.mutate(
              { path: trimmedPath, name: name.trim() },
              {
                onSuccess: (payload) => {
                  onImported(payload.profile)
                },
              },
            )
          }}
        >
          <div className="flex flex-wrap items-end gap-2">
            <Field
              label="Путь к файлу конфигурации"
              htmlFor="import-path"
              className="min-w-[16rem] flex-1"
              hint="config.json, скачанный или сохранённый где угодно: путь можно вписать вручную."
            >
              <Input
                id="import-path"
                value={path}
                onChange={(event) => {
                  setPath(event.target.value)
                  if (!nameEdited) setName(profileNameFromPath(event.target.value))
                }}
                placeholder="/path/to/config.json"
              />
            </Field>
            <Button
              type="button"
              variant="secondary"
              disabled={picking || !desktop}
              onClick={() => {
                void pick()
              }}
            >
              <FolderInput className="h-4 w-4" aria-hidden />
              {picking ? 'Выбираем…' : 'Выбрать файл…'}
            </Button>
          </div>

          <Field
            label="Название профиля"
            htmlFor="import-name"
            hint="Оставьте пустым — имя возьмётся из имени файла."
          >
            <Input
              id="import-name"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                setNameEdited(true)
              }}
              placeholder="config"
            />
          </Field>

          {!desktop ? (
            <Alert tone="warning" title="Диалог выбора файла недоступен">
              Приложение открыто вне десктопного рантайма: впишите путь к файлу вручную.
            </Alert>
          ) : null}

          {pickFailure === null ? null : (
            <Alert
              tone="danger"
              title="Не удалось выбрать файл"
              code={pickFailure.code}
              details={pickFailure.details ?? []}
            >
              {describeError(pickFailure)}
            </Alert>
          )}

          {failure === null ? null : (
            <Alert
              tone="danger"
              title="Не удалось импортировать конфигурацию"
              code={failure.code}
              details={failure.details ?? []}
            >
              {describeError(failure)}
              {failureHint === undefined ? null : (
                <span className="mt-1 block text-xs">{failureHint}</span>
              )}
            </Alert>
          )}

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                onOpenChange(false)
              }}
            >
              Отмена
            </Button>
            <Button type="submit" disabled={importConfig.isPending || trimmedPath === ''}>
              {importConfig.isPending ? <Spinner label="Импортируем…" /> : null}
              Импортировать
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** `C:\Users\me\config.json` and `/etc/sing-box/config.json` both give `config`. */
export function profileNameFromPath(path: string): string {
  const base = path.split(/[\\/]/).pop() ?? ''
  return base.replace(/\.[^.]+$/, '').trim()
}
