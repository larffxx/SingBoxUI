/**
 * Profiles screen (spec §11, §31, §33).
 *
 * The list is the entry point of the app: it shows what exists, which profile is
 * live, and it owns the profile lifecycle actions. Two things get extra care:
 *
 * * the first-launch flow — a sing-box config found on disk is offered for
 *   import, and the original file is never touched;
 * * the delete flow — deleting the active profile stops the runtime, so it goes
 *   through a confirmation that says so.
 */
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Copy,
  Download,
  FilePlus2,
  FolderInput,
  Pencil,
  Play,
  Plus,
  Trash2,
  Upload,
} from 'lucide-react'
import * as React from 'react'

import { QueryErrorAlert } from '@/app/errors/AppErrorBoundary'
import {
  backendAvailable,
  useImportLegacyConfigMutation,
  useLegacyConfigQuery,
  useProfilesQuery,
} from '@/app/queries'
import { describeError, hintFor, toAppError, type AppError } from '@/shared/api/errors'
import type { profile } from '@/shared/api/bindings'
import { formatBytes, truncate } from '@/shared/lib/format'
import { formatDateTime } from '@/shared/lib/time'
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  PageHeader,
  Select,
  Spinner,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
  Toolbar,
} from '@/shared/ui'

import { saveExport } from './exportFile'
import {
  REVISION_LIMIT,
  useApplyTemplateMutation,
  useCreateProfileMutation,
  useDeleteProfileMutation,
  useDuplicateProfileMutation,
  useExportProfileMutation,
  useRenameProfileMutation,
  useRevisionsQuery,
  useSetActiveProfileMutation,
  useTemplatesQuery,
} from './queries'

const EMPTY_TEMPLATE = ''

export function ProfilesRoute(): React.ReactElement {
  const navigate = useNavigate()
  const available = backendAvailable()
  const profilesQuery = useProfilesQuery()
  const templatesQuery = useTemplatesQuery()
  const legacyQuery = useLegacyConfigQuery()
  const importLegacy = useImportLegacyConfigMutation()

  const createProfile = useCreateProfileMutation()
  const renameProfile = useRenameProfileMutation()
  const duplicateProfile = useDuplicateProfileMutation()
  const deleteProfile = useDeleteProfileMutation()
  const setActive = useSetActiveProfileMutation()
  const exportProfile = useExportProfileMutation()
  const applyTemplate = useApplyTemplateMutation()

  const [legacyDismissed, setLegacyDismissed] = React.useState(false)
  const [createOpen, setCreateOpen] = React.useState(false)
  const [importOpen, setImportOpen] = React.useState(false)
  const [renameTarget, setRenameTarget] = React.useState<profile.Profile | null>(null)
  const [duplicateTarget, setDuplicateTarget] = React.useState<profile.Profile | null>(null)
  const [deleteTarget, setDeleteTarget] = React.useState<profile.Profile | null>(null)
  const [templateTarget, setTemplateTarget] = React.useState<profile.Profile | null>(null)
  const [notice, setNotice] = React.useState<string | null>(null)

  const profiles = profilesQuery.data?.profiles ?? []
  const activeId = profilesQuery.data?.activeId ?? ''
  const running = profilesQuery.data?.running ?? false
  const templates = templatesQuery.data?.templates ?? []
  const candidate = legacyQuery.data?.candidate
  const showLegacy = Boolean(candidate?.found) && !legacyDismissed

  const mutationError: AppError | null =
    toAppErrorOrNull(importLegacy.error) ??
    toAppErrorOrNull(createProfile.error) ??
    toAppErrorOrNull(renameProfile.error) ??
    toAppErrorOrNull(duplicateProfile.error) ??
    toAppErrorOrNull(deleteProfile.error) ??
    toAppErrorOrNull(setActive.error) ??
    toAppErrorOrNull(exportProfile.error) ??
    toAppErrorOrNull(applyTemplate.error)

  const resetFailures = () => {
    importLegacy.reset()
    createProfile.reset()
    renameProfile.reset()
    duplicateProfile.reset()
    deleteProfile.reset()
    setActive.reset()
    exportProfile.reset()
    applyTemplate.reset()
  }

  const openProfile = (profileId: string) => {
    void navigate({ to: '/profiles/$profileId', params: { profileId } })
  }

  const runImportLegacy = (path: string) => {
    setNotice(null)
    resetFailures()
    importLegacy.mutate(path, {
      onSuccess: (payload) => {
        setImportOpen(false)
        setLegacyDismissed(true)
        setNotice(`Профиль «${payload.profile.name}» создан из файла ${path}.`)
        openProfile(payload.profile.id)
      },
    })
  }

  const runExport = (target: profile.Profile) => {
    setNotice(null)
    resetFailures()
    exportProfile.mutate(target.id, {
      onSuccess: (payload) => {
        const exported = payload.export
        if (exported === undefined) {
          setNotice('Бэкенд не вернул содержимое для экспорта.')
          return
        }
        const written = saveExport(exported)
        setNotice(
          `Профиль «${target.name}» выгружен в ${written.fileName} (${formatBytes(written.bytes)}), ревизия ${truncate(written.revisionId, 12)}.`,
        )
      },
    })
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="Профили"
        description="Независимые конфигурации sing-box: у каждой своя история ревизий и свой активный файл."
        actions={
          <Toolbar>
            <Button
              variant="secondary"
              onClick={() => {
                setImportOpen(true)
              }}
            >
              <FolderInput className="h-4 w-4" aria-hidden />
              Импорт из файла
            </Button>
            <Button
              onClick={() => {
                setCreateOpen(true)
              }}
            >
              <Plus className="h-4 w-4" aria-hidden />
              Создать профиль
            </Button>
          </Toolbar>
        }
      />

      {!available ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Приложение открыто вне десктопного рантайма: список профилей пуст, действия отключены.
        </Alert>
      ) : null}

      {showLegacy && candidate !== undefined ? (
        <Card className="border-amber-500/40 bg-amber-500/5">
          <CardHeader>
            <CardTitle>Найдена существующая конфигурация sing-box</CardTitle>
            <CardDescription>
              Файл будет <strong>импортирован как новый профиль</strong>. Исходный файл остаётся на
              месте и не перезаписывается — изменения начнутся только после того, как вы примените
              конфигурацию вручную.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
              <div className="min-w-0">
                <dt className="text-xs uppercase tracking-wide text-muted-foreground">Путь</dt>
                <dd className="truncate font-mono text-xs">{candidate.configPath}</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wide text-muted-foreground">Размер</dt>
                <dd className="text-sm">{formatBytes(candidate.size)}</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wide text-muted-foreground">Изменён</dt>
                <dd className="text-sm">{formatDateTime(candidate.modified)}</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wide text-muted-foreground">
                  Разбор файла
                </dt>
                <dd className="text-sm">{candidate.valid ? 'успешно' : 'есть замечания'}</dd>
              </div>
              {candidate.binaryPath === undefined || candidate.binaryPath === '' ? null : (
                <div className="min-w-0">
                  <dt className="text-xs uppercase tracking-wide text-muted-foreground">
                    Рядом найден бинарник
                  </dt>
                  <dd className="truncate font-mono text-xs">{candidate.binaryPath}</dd>
                </div>
              )}
              {candidate.settingsPath === undefined || candidate.settingsPath === '' ? null : (
                <div className="min-w-0">
                  <dt className="text-xs uppercase tracking-wide text-muted-foreground">
                    Настройки предыдущей версии
                  </dt>
                  <dd className="truncate font-mono text-xs">{candidate.settingsPath}</dd>
                </div>
              )}
            </dl>

            {candidate.warnings === undefined || candidate.warnings.length === 0 ? null : (
              <Alert tone="info" title="Замечания к файлу" details={candidate.warnings}>
                Файл будет импортирован как есть: исправить замечания можно в редакторе профиля.
              </Alert>
            )}

            <Toolbar>
              <Button
                disabled={importLegacy.isPending}
                onClick={() => {
                  runImportLegacy(candidate.configPath)
                }}
              >
                <Upload className="h-4 w-4" aria-hidden />
                Импортировать как профиль
              </Button>
              <Button
                variant="secondary"
                onClick={() => {
                  setImportOpen(true)
                }}
              >
                Выбрать другой файл
              </Button>
              <Button
                variant="ghost"
                onClick={() => {
                  setLegacyDismissed(true)
                }}
              >
                Не сейчас
              </Button>
              {importLegacy.isPending ? <Spinner label="Импортируем…" /> : null}
            </Toolbar>
          </CardContent>
        </Card>
      ) : null}

      {notice === null ? null : (
        <Alert
          tone="success"
          title="Готово"
          actions={
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setNotice(null)
              }}
            >
              Скрыть
            </Button>
          }
        >
          {notice}
        </Alert>
      )}

      {mutationError === null ? null : (
        <Alert
          tone="danger"
          title="Действие не выполнено"
          code={mutationError.code}
          details={mutationError.details ?? []}
          actions={
            <Button size="sm" variant="secondary" onClick={resetFailures}>
              Сбросить
            </Button>
          }
        >
          {describeError(mutationError)}
          {hintFor(mutationError.code) === undefined ? null : (
            <span className="mt-1 block text-xs">{hintFor(mutationError.code)}</span>
          )}
        </Alert>
      )}

      <QueryErrorAlert error={profilesQuery.error} />

      <Card>
        <CardHeader>
          <CardTitle>Список профилей</CardTitle>
          <CardDescription>
            {profiles.length === 0
              ? 'Профилей пока нет.'
              : `Всего профилей: ${profiles.length}. Активный: ${activeId === '' ? 'не выбран' : truncate(activeId, 12)}.`}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {profilesQuery.isPending ? <Spinner label="Загружаем профили…" /> : null}

          {!profilesQuery.isPending && profiles.length === 0 ? (
            <EmptyState
              title="Ни одного профиля"
              description="Создайте профиль из шаблона или импортируйте существующую конфигурацию sing-box."
              action={
                <Button
                  onClick={() => {
                    setCreateOpen(true)
                  }}
                >
                  <FilePlus2 className="h-4 w-4" aria-hidden />
                  Создать профиль
                </Button>
              }
            />
          ) : null}

          {profiles.length === 0 ? null : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Имя</TableHead>
                  <TableHead>Описание</TableHead>
                  <TableHead>Статус</TableHead>
                  <TableHead>Ревизии</TableHead>
                  <TableHead>Обновлён</TableHead>
                  <TableHead className="text-right">Действия</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {profiles.map((item) => {
                  const isActive = item.id === activeId
                  return (
                    <TableRow key={item.id}>
                      <TableCell>
                        <div className="space-y-0.5">
                          <Link
                            to="/profiles/$profileId"
                            params={{ profileId: item.id }}
                            className="text-sm font-medium hover:underline"
                          >
                            {item.name}
                          </Link>
                          <p className="font-mono text-xs text-muted-foreground">
                            {truncate(item.id, 14)}
                          </p>
                        </div>
                      </TableCell>
                      <TableCell className="max-w-[22rem] text-xs text-muted-foreground">
                        {item.description === '' ? '—' : item.description}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1">
                          {isActive ? (
                            <Badge tone={running ? 'success' : 'info'}>
                              {running ? 'активный, запущен' : 'активный'}
                            </Badge>
                          ) : (
                            <Badge tone="neutral">не активен</Badge>
                          )}
                          <span className="text-xs text-muted-foreground">
                            создан {formatDateTime(item.createdAt)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-xs">
                        <RevisionCount profileId={item.id} />
                      </TableCell>
                      <TableCell className="text-xs">
                        <div className="space-y-0.5">
                          <p>{formatDateTime(item.updatedAt)}</p>
                          <p className="text-muted-foreground">
                            последний запуск:{' '}
                            {item.lastUsedAt === undefined ? '—' : formatDateTime(item.lastUsedAt)}
                          </p>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex flex-wrap justify-end gap-1">
                          {isActive ? null : (
                            <Button
                              size="sm"
                              variant="secondary"
                              disabled={setActive.isPending}
                              onClick={() => {
                                setNotice(null)
                                resetFailures()
                                setActive.mutate(item.id, {
                                  onSuccess: () => {
                                    setNotice(`Профиль «${item.name}» теперь активный.`)
                                  },
                                })
                              }}
                            >
                              <Play className="h-3.5 w-3.5" aria-hidden />
                              Сделать активным
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              openProfile(item.id)
                            }}
                          >
                            Открыть
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              setRenameTarget(item)
                            }}
                          >
                            <Pencil className="h-3.5 w-3.5" aria-hidden />
                            Переименовать
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              setDuplicateTarget(item)
                            }}
                          >
                            <Copy className="h-3.5 w-3.5" aria-hidden />
                            Дублировать
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={templates.length === 0}
                            onClick={() => {
                              setTemplateTarget(item)
                            }}
                          >
                            Шаблон
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={exportProfile.isPending}
                            onClick={() => {
                              runExport(item)
                            }}
                          >
                            <Download className="h-3.5 w-3.5" aria-hidden />
                            Экспорт
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              setDeleteTarget(item)
                            }}
                          >
                            <Trash2 className="h-3.5 w-3.5" aria-hidden />
                            Удалить
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <CreateProfileDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        templates={templates}
        pending={createProfile.isPending}
        onSubmit={(input) => {
          setNotice(null)
          resetFailures()
          createProfile.mutate(input, {
            onSuccess: (payload) => {
              setCreateOpen(false)
              setNotice(`Профиль «${payload.profile.name}» создан.`)
              openProfile(payload.profile.id)
            },
          })
        }}
      />

      <ImportPathDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        pending={importLegacy.isPending}
        onSubmit={runImportLegacy}
      />

      <RenameProfileDialog
        target={renameTarget}
        onOpenChange={(open) => {
          if (!open) setRenameTarget(null)
        }}
        pending={renameProfile.isPending}
        onSubmit={(name, description) => {
          if (renameTarget === null) return
          setNotice(null)
          resetFailures()
          renameProfile.mutate(
            { profileId: renameTarget.id, name, description },
            {
              onSuccess: (payload) => {
                setRenameTarget(null)
                setNotice(`Профиль переименован в «${payload.profile.name}».`)
              },
            },
          )
        }}
      />

      <DuplicateProfileDialog
        target={duplicateTarget}
        onOpenChange={(open) => {
          if (!open) setDuplicateTarget(null)
        }}
        pending={duplicateProfile.isPending}
        onSubmit={(name) => {
          if (duplicateTarget === null) return
          setNotice(null)
          resetFailures()
          duplicateProfile.mutate(
            { profileId: duplicateTarget.id, name },
            {
              onSuccess: (payload) => {
                setDuplicateTarget(null)
                setNotice(`Создана копия «${payload.profile.name}».`)
              },
            },
          )
        }}
      />

      <ApplyTemplateDialog
        target={templateTarget}
        templates={templates}
        onOpenChange={(open) => {
          if (!open) setTemplateTarget(null)
        }}
        pending={applyTemplate.isPending}
        onSubmit={(templateId) => {
          if (templateTarget === null) return
          setNotice(null)
          resetFailures()
          applyTemplate.mutate(
            { profileId: templateTarget.id, templateId },
            {
              onSuccess: (payload) => {
                setTemplateTarget(null)
                setNotice(
                  `Шаблон применён: создана ревизия ${truncate(payload.revision.id, 12)}. Открыть профиль, чтобы её применить.`,
                )
              },
            },
          )
        }}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
        title="Удалить профиль?"
        description={
          deleteTarget === null
            ? ''
            : `Профиль «${deleteTarget.name}» и вся его история ревизий будут удалены без возможности восстановления.`
        }
        confirmLabel="Удалить профиль"
        destructive
        pending={deleteProfile.isPending}
        onConfirm={() => {
          if (deleteTarget === null) return
          setNotice(null)
          resetFailures()
          deleteProfile.mutate(deleteTarget.id, {
            onSuccess: () => {
              setNotice(`Профиль «${deleteTarget.name}» удалён.`)
              setDeleteTarget(null)
            },
          })
        }}
      >
        {deleteTarget !== null && deleteTarget.id === activeId ? (
          <Alert tone="danger" title="Это активный профиль">
            Удаление активного профиля {running ? 'остановит рантайм и ' : ''}снимет активную
            конфигурацию. Запустите другой профиль до удаления, если это не задумано.
          </Alert>
        ) : null}
      </ConfirmDialog>
    </div>
  )
}

/** Revision count published per row; a capped count is marked as `50+`. */
function RevisionCount({ profileId }: { profileId: string }): React.ReactElement {
  const revisions = useRevisionsQuery(profileId, REVISION_LIMIT)
  if (revisions.isPending) return <span className="text-muted-foreground">…</span>
  if (revisions.error !== null && revisions.error !== undefined) {
    return <span className="text-muted-foreground">—</span>
  }
  const list = revisions.data?.revisions ?? []
  const active = list.find((revision) => revision.active)
  const capped = list.length >= REVISION_LIMIT
  return (
    <div className="space-y-0.5">
      <p>
        {list.length}
        {capped ? '+' : ''} всего
      </p>
      <p className="text-muted-foreground">
        последняя применённая: {active === undefined ? '—' : truncate(active.id, 12)}
      </p>
    </div>
  )
}

function CreateProfileDialog({
  open,
  onOpenChange,
  templates,
  pending,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  templates: readonly templateItem[]
  pending: boolean
  onSubmit: (input: {
    name: string
    description: string
    templateId: string
    configJson: string
  }) => void
}) {
  const [name, setName] = React.useState('')
  const [description, setDescription] = React.useState('')
  const [templateId, setTemplateId] = React.useState(EMPTY_TEMPLATE)

  React.useEffect(() => {
    if (open) {
      setName('')
      setDescription('')
      setTemplateId(templates[0]?.id ?? EMPTY_TEMPLATE)
    }
  }, [open, templates])

  const trimmed = name.trim()
  const selected = templates.find((template) => template.id === templateId)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Новый профиль"
        description="Профиль начинается с шаблона или с пустого документа. Пустой документ можно заполнить вручную или импортом ссылки."
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (trimmed === '') return
            onSubmit({ name: trimmed, description: description.trim(), templateId, configJson: '' })
          }}
        >
          <Field label="Имя" htmlFor="profile-name">
            <Input
              id="profile-name"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
              placeholder="Например: Домашний"
            />
          </Field>

          <Field label="Описание" htmlFor="profile-description">
            <Textarea
              id="profile-description"
              value={description}
              rows={2}
              onChange={(event) => {
                setDescription(event.target.value)
              }}
              placeholder="Зачем этот профиль и что в нём особенного"
            />
          </Field>

          <Field
            label="Шаблон"
            htmlFor="profile-template"
            hint={
              selected?.requiresPrivilege === true
                ? 'Шаблон включает TUN: для запуска потребуются права администратора.'
                : 'Шаблон задаёт стартовую конфигурацию; активным профиль станет отдельно.'
            }
          >
            <Select
              id="profile-template"
              value={templateId}
              onValueChange={setTemplateId}
              options={[
                { value: EMPTY_TEMPLATE, label: 'Пустой документ' },
                ...templates.map((template) => ({ value: template.id, label: template.name })),
              ]}
            />
          </Field>

          {selected === undefined ? null : (
            <p className="text-xs text-muted-foreground">{selected.description}</p>
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
            <Button type="submit" disabled={pending || trimmed === ''}>
              {pending ? <Spinner label="Создаём…" /> : null}
              Создать
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ImportPathDialog({
  open,
  onOpenChange,
  pending,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  pending: boolean
  onSubmit: (path: string) => void
}) {
  const [path, setPath] = React.useState('')

  React.useEffect(() => {
    if (open) setPath('')
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Импорт конфигурации из файла"
        description="Профиль создаётся из копии файла. Исходный файл остаётся нетронутым."
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            const trimmed = path.trim()
            if (trimmed === '') return
            onSubmit(trimmed)
          }}
        >
          <Field
            label="Путь к файлу конфигурации"
            htmlFor="import-path"
            hint="Абсолютный путь, например /etc/sing-box/config.json или ~/.config/sing-box/config.json"
          >
            <Input
              id="import-path"
              value={path}
              onChange={(event) => {
                setPath(event.target.value)
              }}
              placeholder="/path/to/config.json"
            />
          </Field>

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
            <Button type="submit" disabled={pending || path.trim() === ''}>
              {pending ? <Spinner label="Импортируем…" /> : null}
              Импортировать
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RenameProfileDialog({
  target,
  onOpenChange,
  pending,
  onSubmit,
}: {
  target: profile.Profile | null
  onOpenChange: (open: boolean) => void
  pending: boolean
  onSubmit: (name: string, description: string) => void
}) {
  const [name, setName] = React.useState('')
  const [description, setDescription] = React.useState('')

  React.useEffect(() => {
    if (target !== null) {
      setName(target.name)
      setDescription(target.description)
    }
  }, [target])

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent title="Переименовать профиль" description="Имя видно в списке и в шапке.">
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (name.trim() === '') return
            onSubmit(name.trim(), description.trim())
          }}
        >
          <Field label="Имя" htmlFor="rename-name">
            <Input
              id="rename-name"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Field>
          <Field label="Описание" htmlFor="rename-description">
            <Textarea
              id="rename-description"
              value={description}
              rows={2}
              onChange={(event) => {
                setDescription(event.target.value)
              }}
            />
          </Field>
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
            <Button type="submit" disabled={pending || name.trim() === ''}>
              Сохранить
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DuplicateProfileDialog({
  target,
  onOpenChange,
  pending,
  onSubmit,
}: {
  target: profile.Profile | null
  onOpenChange: (open: boolean) => void
  pending: boolean
  onSubmit: (name: string) => void
}) {
  const [name, setName] = React.useState('')

  React.useEffect(() => {
    if (target !== null) setName(`${target.name} (копия)`)
  }, [target])

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent
        title="Дублировать профиль"
        description="Копия получает текущий черновик и историю ревизий исходного профиля."
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (name.trim() === '') return
            onSubmit(name.trim())
          }}
        >
          <Field label="Имя копии" htmlFor="duplicate-name">
            <Input
              id="duplicate-name"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Field>
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
            <Button type="submit" disabled={pending || name.trim() === ''}>
              {pending ? <Spinner label="Копируем…" /> : null}
              Дублировать
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ApplyTemplateDialog({
  target,
  templates,
  onOpenChange,
  pending,
  onSubmit,
}: {
  target: profile.Profile | null
  templates: readonly templateItem[]
  onOpenChange: (open: boolean) => void
  pending: boolean
  onSubmit: (templateId: string) => void
}) {
  const [templateId, setTemplateId] = React.useState('')

  React.useEffect(() => {
    if (target !== null) setTemplateId(templates[0]?.id ?? '')
  }, [target, templates])

  const selected = templates.find((template) => template.id === templateId)

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent
        title="Применить шаблон"
        description="Шаблон записывается как новая ревизия профиля — активная конфигурация не меняется, пока вы её не примените."
      >
        <div className="space-y-4">
          <Field label="Шаблон" htmlFor="apply-template">
            <Select
              id="apply-template"
              value={templateId}
              onValueChange={setTemplateId}
              options={templates.map((template) => ({
                value: template.id,
                label: template.name,
              }))}
            />
          </Field>
          {selected === undefined ? null : (
            <p className="text-xs text-muted-foreground">{selected.description}</p>
          )}
          <div className="flex justify-end gap-2">
            <Button
              variant="ghost"
              onClick={() => {
                onOpenChange(false)
              }}
            >
              Отмена
            </Button>
            <Button
              disabled={pending || templateId === ''}
              onClick={() => {
                onSubmit(templateId)
              }}
            >
              {pending ? <Spinner label="Применяем…" /> : null}
              Применить
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** Narrow shape of `templates.Template` used by the pickers. */
interface templateItem {
  id: string
  name: string
  description: string
  requiresPrivilege: boolean
}

function toAppErrorOrNull(value: unknown): AppError | null {
  if (value === null || value === undefined) return null
  return toAppError(value)
}
