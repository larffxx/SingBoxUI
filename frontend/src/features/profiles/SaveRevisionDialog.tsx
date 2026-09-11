/**
 * Save-revision dialog (spec §34, §37).
 *
 * Saving never happens silently and never applies on its own: the checkbox is
 * the only way to make a revision live, and it is off by default. A revision
 * without a comment is still allowed — the comment is what makes the history
 * readable later.
 */
import {
  Alert,
  Button,
  Dialog,
  DialogContent,
  Field,
  Spinner,
  SwitchField,
  Textarea,
} from '@/shared/ui'

import * as React from 'react'

import type { AppError } from '@/shared/api/errors'
import { describeError, hintFor, toAppError } from '@/shared/api/errors'
import { configApi } from '@/shared/api/bindings'

export function SaveRevisionDialog({
  open,
  onOpenChange,
  profileId,
  configJson,
  blockedByErrors,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  profileId: string
  configJson: string
  /** True while validation reports errors: saving is refused by the backend too. */
  blockedByErrors: boolean
  onSaved: (revisionId: string, applied: boolean) => void
}) {
  const [comment, setComment] = React.useState('')
  const [applyNow, setApplyNow] = React.useState(false)
  const [pending, setPending] = React.useState(false)
  const [failure, setFailure] = React.useState<AppError | null>(null)

  React.useEffect(() => {
    if (open) {
      setFailure(null)
    }
  }, [open])

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (pending || blockedByErrors) return
    setPending(true)
    setFailure(null)
    configApi
      .saveRevision({
        profileId,
        configJson,
        comment: comment.trim(),
        source: 'editor',
        apply: applyNow,
      })
      .then((payload) => {
        setPending(false)
        setComment('')
        setApplyNow(false)
        onSaved(payload.revision.id, applyNow)
        onOpenChange(false)
      })
      .catch((error: unknown) => {
        setPending(false)
        setFailure(toAppError(error))
      })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Сохранить ревизию"
        description="Ревизия — это неизменяемый снимок конфигурации. Применение к рантайму — отдельный шаг."
      >
        <form className="space-y-4" onSubmit={submit}>
          {blockedByErrors ? (
            <Alert tone="danger" title="Сохранение заблокировано">
              В конфигурации есть ошибки. Исправьте их на вкладке Raw JSON — сохранение станет
              доступным.
            </Alert>
          ) : null}

          <Field
            label="Комментарий"
            htmlFor="revision-comment"
            hint="Что изменилось: например «добавил hysteria2 для резерва»."
          >
            <Textarea
              id="revision-comment"
              value={comment}
              rows={3}
              placeholder="Кратко опишите изменение"
              onChange={(event) => {
                setComment(event.target.value)
              }}
            />
          </Field>

          <SwitchField
            id="revision-apply"
            label="Применить сразу после сохранения"
            description="Запишет конфигурацию в активный файл и перезапустит рантайм, если он запущен."
            checked={applyNow}
            onCheckedChange={setApplyNow}
          />

          {failure === null ? null : (
            <Alert tone="danger" title="Не удалось сохранить" code={failure.code}>
              {describeError(failure)}
              {hintFor(failure.code) === undefined ? null : (
                <span className="mt-1 block text-xs">{hintFor(failure.code)}</span>
              )}
            </Alert>
          )}

          <div className="flex items-center justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                onOpenChange(false)
              }}
            >
              Отмена
            </Button>
            <Button type="submit" disabled={pending || blockedByErrors || configJson.trim() === ''}>
              {pending ? <Spinner label="Сохраняем…" /> : null}
              Сохранить ревизию
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
