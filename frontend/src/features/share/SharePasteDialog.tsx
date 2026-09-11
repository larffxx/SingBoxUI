/**
 * Share-link import (spec §39).
 *
 * Parsing belongs to the backend: the dialog sends the pasted text to
 * `shareApi.parseMany` and only presents what comes back — the resolved
 * outbound, the detected protocol and, crucially, the parameter warnings the
 * parser produced for keys it could not map. Nothing is added to the profile
 * until the user confirms, and an existing tag is never overwritten.
 */
import { useQuery } from '@tanstack/react-query'
import * as React from 'react'

import { shareApi } from '@/shared/api/bindings'
import type { share } from '@/shared/api/bindings'
import { describeError, toAppError } from '@/shared/api/errors'
import type { AppError } from '@/shared/api/errors'
import { keys } from '@/shared/api/keys'
import {
  Alert,
  Badge,
  Button,
  Dialog,
  DialogContent,
  Field,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
} from '@/shared/ui'

import type { JsonObject } from '../profiles/jsonDoc'
import { uniqueTag } from '../profiles/tags'

export function SharePasteDialog({
  open,
  onOpenChange,
  existingTags,
  onImported,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Tags already present in the profile; conflicts are renamed, never replaced. */
  existingTags: string[]
  onImported: (outbounds: JsonObject[], notes: string[]) => void
}) {
  const [text, setText] = React.useState('')
  const [parsed, setParsed] = React.useState<share.Parsed[]>([])
  const [errors, setErrors] = React.useState<string[]>([])
  const [failure, setFailure] = React.useState<AppError | undefined>(undefined)
  const [pending, setPending] = React.useState(false)
  const [excluded, setExcluded] = React.useState<string[]>([])

  const schemes = useQuery({
    queryKey: keys.share.schemes(),
    queryFn: () => shareApi.schemes(),
    staleTime: Number.POSITIVE_INFINITY,
  })

  React.useEffect(() => {
    if (!open) {
      setParsed([])
      setErrors([])
      setFailure(undefined)
      setExcluded([])
      setText('')
    }
  }, [open])

  const parse = async () => {
    setPending(true)
    setFailure(undefined)
    try {
      const payload = await shareApi.parseMany(text)
      setParsed(payload.parsed)
      setErrors(payload.errors ?? [])
    } catch (error) {
      setFailure(toAppError(error))
      setParsed([])
      setErrors([])
    } finally {
      setPending(false)
    }
  }

  const include = (item: share.Parsed, index: number): boolean => {
    const key = item.tag === '' ? `#${index}` : item.tag
    return !excluded.includes(key)
  }

  const toggle = (item: share.Parsed, index: number) => {
    const key = item.tag === '' ? `#${index}` : item.tag
    setExcluded((current) =>
      current.includes(key) ? current.filter((value) => value !== key) : [...current, key],
    )
  }

  const selected = parsed.filter(include)

  const importSelected = () => {
    const used = new Set(existingTags)
    const outbounds: JsonObject[] = []
    const notes: string[] = []
    for (const item of selected) {
      const requested = item.tag === '' ? `imported-${outbounds.length + 1}` : item.tag
      const tag = uniqueTag(used, requested)
      if (tag !== requested) {
        notes.push(`Тег «${requested}» уже занят — добавлен как «${tag}».`)
      }
      used.add(tag)
      outbounds.push({ ...(item.outbound as JsonObject), tag })
    }
    onImported(outbounds, notes)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Импорт из ссылок"
        description="Вставьте одну или несколько ссылок (vless://, vmess://, trojan://, ss://, hysteria2://, tuic://). Разбор выполняет приложение, конфигурация меняется только после подтверждения."
        size="wide"
      >
        <div className="space-y-4">
          {schemes.data && schemes.data.length > 0 ? (
            <p className="text-xs text-muted-foreground">
              Поддерживаемые схемы: {schemes.data.map((scheme) => `${scheme}://`).join(', ')}
            </p>
          ) : null}

          <Field
            label="Ссылки"
            htmlFor="share-links"
            hint="По одной ссылке в строке. Параметры, которые sing-box не поддерживает, будут показаны как предупреждения."
          >
            <Textarea
              id="share-links"
              rows={6}
              value={text}
              spellCheck={false}
              placeholder="vless://uuid@example.com:443?type=ws&security=tls#node-1"
              className="font-mono text-xs"
              onChange={(event) => {
                setText(event.target.value)
              }}
            />
          </Field>

          <div className="flex items-center gap-2">
            <Button
              type="button"
              loading={pending}
              disabled={text.trim() === ''}
              onClick={() => {
                void parse()
              }}
            >
              Разобрать
            </Button>
            <span className="text-xs text-muted-foreground">
              {parsed.length === 0
                ? 'Ссылки ещё не разобраны.'
                : `Разобрано: ${parsed.length}, выбрано: ${selected.length}.`}
            </span>
          </div>

          {failure ? (
            <Alert tone="danger" code={failure.code} title="Не удалось разобрать ссылки">
              {describeError(failure)}
            </Alert>
          ) : null}

          {errors.length > 0 ? (
            <Alert tone="warning" title="Часть строк пропущена" details={errors}>
              Строки ниже не удалось разобрать — они не будут добавлены.
            </Alert>
          ) : null}

          {parsed.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Добавить</TableHead>
                  <TableHead>Тег</TableHead>
                  <TableHead>Протокол</TableHead>
                  <TableHead>Имя</TableHead>
                  <TableHead>Предупреждения</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {parsed.map((item, index) => {
                  const warnings = item.warnings ?? []
                  return (
                    <TableRow key={`${index}-${item.tag}`}>
                      <TableCell>
                        <input
                          type="checkbox"
                          aria-label={`Добавить ${item.tag}`}
                          checked={include(item, index)}
                          onChange={() => {
                            toggle(item, index)
                          }}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-xs">{item.tag}</TableCell>
                      <TableCell>
                        <Badge tone="info">{item.kind}</Badge>
                      </TableCell>
                      <TableCell className="text-xs">{item.displayName}</TableCell>
                      <TableCell>
                        {warnings.length === 0 ? (
                          <span className="text-xs text-muted-foreground">Нет</span>
                        ) : (
                          <ul className="space-y-1">
                            {warnings.map((warning) => (
                              <li key={warning} className="text-xs text-amber-500">
                                {warning}
                              </li>
                            ))}
                          </ul>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : null}

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onOpenChange(false)
              }}
            >
              Отмена
            </Button>
            <Button
              type="button"
              disabled={selected.length === 0}
              onClick={() => {
                importSelected()
              }}
            >
              {`Добавить (${selected.length})`}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
