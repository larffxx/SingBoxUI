/**
 * Share-link export (spec §39).
 *
 * The link is produced by the backend (`shareApi.build`) so the frontend never
 * has to re-implement protocol-specific URL formats. Some protocols have no
 * share-link representation at all; that failure is surfaced verbatim instead
 * of producing a link that would not round-trip.
 */
import * as React from 'react'

import { shareApi } from '@/shared/api/bindings'
import { describeError, toAppError } from '@/shared/api/errors'
import type { AppError } from '@/shared/api/errors'
import { Alert, Button, Dialog, DialogContent, Field, Spinner, Textarea } from '@/shared/ui'

import type { JsonObject } from '../profiles/jsonDoc'

export function ShareExportDialog({
  open,
  onOpenChange,
  outbound,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  outbound: JsonObject
}) {
  const [link, setLink] = React.useState('')
  const [failure, setFailure] = React.useState<AppError | undefined>(undefined)
  const [pending, setPending] = React.useState(false)
  const [status, setStatus] = React.useState('')

  // The payload is read through a ref so the request is issued once per open,
  // not once per render of the parent list.
  const payload = JSON.stringify(outbound, null, 2)
  const payloadRef = React.useRef(payload)
  payloadRef.current = payload

  React.useEffect(() => {
    if (!open) {
      setLink('')
      setFailure(undefined)
      setStatus('')
      return
    }
    let cancelled = false
    setPending(true)
    setLink('')
    setFailure(undefined)
    setStatus('')
    shareApi
      .build(payloadRef.current)
      .then((value) => {
        if (!cancelled) setLink(value.link)
      })
      .catch((error: unknown) => {
        if (!cancelled) setFailure(toAppError(error))
      })
      .finally(() => {
        if (!cancelled) setPending(false)
      })
    return () => {
      cancelled = true
    }
  }, [open])

  const copy = () => {
    void navigator.clipboard
      .writeText(link)
      .then(() => {
        setStatus('Ссылка скопирована в буфер обмена.')
      })
      .catch(() => {
        setStatus('Автоматическое копирование недоступно — выделите ссылку вручную.')
      })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Экспорт в ссылку"
        description="Ссылка собирается приложением из выбранного outbound. Протоколы без формата ссылки экспортировать нельзя."
      >
        <div className="space-y-4">
          {pending ? <Spinner label="Собираем ссылку…" /> : null}
          {failure ? (
            <Alert tone="danger" code={failure.code} title="Экспорт невозможен">
              {describeError(failure)}
            </Alert>
          ) : null}
          {link !== '' ? (
            <>
              <Field label="Ссылка" htmlFor="share-export-link">
                <Textarea
                  id="share-export-link"
                  readOnly
                  rows={3}
                  value={link}
                  spellCheck={false}
                />
              </Field>
              <div className="flex items-center gap-2">
                <Button type="button" onClick={copy}>
                  Копировать
                </Button>
                {status === '' ? null : (
                  <span className="text-xs text-muted-foreground">{status}</span>
                )}
              </div>
            </>
          ) : null}
          <div className="flex justify-end">
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onOpenChange(false)
              }}
            >
              Закрыть
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
