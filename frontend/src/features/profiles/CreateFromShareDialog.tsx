/**
 * Create a profile from a share link (spec §39).
 *
 * The create-time counterpart of `SharePasteDialog`: instead of inserting parsed
 * outbounds into a profile that already exists, the dialog asks the backend to
 * generate a whole configuration from the pasted links and creates the profile
 * from it. Parsing stays on the backend — the preview is exactly what
 * `shareApi.parseMany` returned — and the base list is what
 * `profilesApi.listShareLinkBases` declares, so the dialog offers what the
 * backend can actually generate and nothing else.
 */
import { Link2 } from 'lucide-react'
import * as React from 'react'

import {
  useCreateFromShareLinksMutation,
  useShareLinkBasesQuery,
} from '@/features/profiles/queries'
import { shareApi } from '@/shared/api/bindings'
import type { profile, share } from '@/shared/api/bindings'
import { describeError, hintFor, toAppError, type AppError } from '@/shared/api/errors'
import {
  Alert,
  Badge,
  Button,
  Dialog,
  DialogContent,
  Field,
  Input,
  Select,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
} from '@/shared/ui'

export function CreateFromShareDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (created: profile.Profile, warnings: string[]) => void
}): React.ReactElement {
  const bases = useShareLinkBasesQuery()
  const create = useCreateFromShareLinksMutation()
  const resetCreate = create.reset

  const [links, setLinks] = React.useState('')
  const [name, setName] = React.useState('')
  const [base, setBase] = React.useState('')
  const [parsed, setParsed] = React.useState<share.Parsed[]>([])
  const [lineErrors, setLineErrors] = React.useState<string[]>([])
  const [failure, setFailure] = React.useState<AppError | undefined>(undefined)
  const [checking, setChecking] = React.useState(false)

  React.useEffect(() => {
    if (open) return
    setLinks('')
    setName('')
    setParsed([])
    setLineErrors([])
    setFailure(undefined)
    resetCreate()
  }, [open, resetCreate])

  // The default base is the first one the backend declares; a late answer must not
  // overwrite a choice the user already made.
  const declared = React.useMemo(() => bases.data?.bases ?? [], [bases.data])
  React.useEffect(() => {
    const first = declared[0]
    if (base === '' && first) setBase(first.id)
  }, [base, declared])

  const selectedBase = declared.find((item) => item.id === base)
  const derivedName = parsed[0]?.displayName ?? ''
  const trimmedName = name.trim()

  const check = async (): Promise<void> => {
    setChecking(true)
    setFailure(undefined)
    try {
      const payload = await shareApi.parseMany(links)
      setParsed(payload.parsed)
      setLineErrors(payload.errors ?? [])
    } catch (error) {
      setFailure(toAppError(error))
      setParsed([])
      setLineErrors([])
    } finally {
      setChecking(false)
    }
  }

  const submit = (): void => {
    setFailure(undefined)
    create.mutate(
      { name: trimmedName, description: '', links, base },
      {
        onSuccess: (payload) => {
          onCreated(payload.profile, payload.warnings ?? [])
        },
        onError: (error) => {
          setFailure(toAppError(error))
        },
      },
    )
  }

  const mutationFailure = create.error === null ? undefined : toAppError(create.error)
  const shown = failure ?? mutationFailure

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Профиль из ссылки"
        description="Вставьте ссылку провайдера (vless://, vmess://, trojan://, ss://, hysteria2://, tuic://) — приложение разберёт её и сгенерирует конфигурацию целиком: сервер, DNS и маршрутизацию."
        size="wide"
      >
        <div className="space-y-4">
          <Field
            label="Ссылки"
            htmlFor="create-share-links"
            hint="По одной ссылке в строке. Несколько ссылок попадут в автоматическую группу с выбором быстрейшего сервера."
          >
            <Textarea
              id="create-share-links"
              rows={5}
              value={links}
              spellCheck={false}
              placeholder="vless://uuid@example.com:443?type=tcp&security=reality#Москва"
              className="font-mono text-xs"
              onChange={(event) => {
                setLinks(event.target.value)
              }}
            />
          </Field>

          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              loading={checking}
              disabled={links.trim() === ''}
              onClick={() => {
                void check()
              }}
            >
              <Link2 className="h-4 w-4" aria-hidden />
              Проверить ссылки
            </Button>
            <span className="text-xs text-muted-foreground">
              {parsed.length === 0
                ? 'Проверка покажет, что приложение поняло из ссылки.'
                : `Разобрано: ${parsed.length}.`}
            </span>
          </div>

          {lineErrors.length > 0 ? (
            <Alert tone="warning" title="Часть строк пропущена" details={lineErrors}>
              Эти строки не попадут в конфигурацию.
            </Alert>
          ) : null}

          {parsed.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
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

          <Field
            label="Имя профиля"
            htmlFor="create-share-name"
            hint={
              derivedName === ''
                ? 'Пустое имя: возьмём его из ссылки.'
                : `Пустое имя: профиль назовётся «${derivedName}».`
            }
          >
            <Input
              id="create-share-name"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
              placeholder={derivedName === '' ? 'Например: Москва' : derivedName}
            />
          </Field>

          <Field
            label="Конфигурация"
            htmlFor="create-share-base"
            hint={
              selectedBase === undefined
                ? 'Список конфигураций приходит из приложения.'
                : selectedBase.description
            }
          >
            <Select
              id="create-share-base"
              value={base}
              onValueChange={setBase}
              options={declared.map((item) => ({ value: item.id, label: item.name }))}
            />
          </Field>

          {selectedBase?.requiresPrivilege === true ? (
            <Alert tone="info" title="Понадобятся права администратора">
              Профиль с TUN-устройством запускается через привилегированный хелпер: при старте
              приложение спросит подтверждение системы.
            </Alert>
          ) : null}

          {shown ? (
            <Alert tone="danger" code={shown.code} title="Не удалось создать профиль">
              {describeError(shown)}
              {hintFor(shown.code) ? <span className="block">{hintFor(shown.code)}</span> : null}
            </Alert>
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
              loading={create.isPending}
              disabled={links.trim() === '' || base === ''}
              onClick={submit}
            >
              Создать профиль
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
