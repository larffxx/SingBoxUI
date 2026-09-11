/**
 * Revisions tab (spec §34, §37, §40).
 *
 * Three things live here: the history list, the comparison surface and the two
 * destructive transport actions (apply, roll back). Everything the backend
 * reports is rendered as reported — the config path after an apply, the
 * checksum-free validation status per revision, the rollback outcome — and no
 * transport action happens without a confirmation dialog.
 */
import { ArrowRight, GitCompare, RotateCcw, Save, Upload } from 'lucide-react'
import * as React from 'react'

import { CONFIG_APPLY_PROGRESS_KEY, type ConfigApplyProgressPayload } from '@/app/events'
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
  EmptyState,
  JsonDiff,
  Spinner,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Toolbar,
} from '@/shared/ui'
import { describeError, hintFor, toAppError, type AppError } from '@/shared/api/errors'
import { formatDateTime } from '@/shared/lib/time'
import { truncate } from '@/shared/lib/format'
import type { config } from '@/shared/api/bindings'

import { useCachedEvent } from '../progress'
import {
  REVISION_LIMIT,
  useApplyRevisionMutation,
  useCompareRevisionsQuery,
  useCompareWithActiveMutation,
  useRevisionQuery,
  useRevisionsQuery,
  useRollbackMutation,
} from '../queries'
import type { ValidationSummary } from '../validation'

const STAGE_LABELS: Record<string, string> = {
  validate: 'Проверка структуры',
  check: 'Проверка sing-box',
  materialize: 'Запись файла конфигурации',
  restart: 'Перезапуск рантайма',
  commit: 'Фиксация ревизии',
  done: 'Готово',
}

function stageLabel(stage: string): string {
  return STAGE_LABELS[stage] ?? stage
}

function statusTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const normalized = status.toLowerCase()
  if (normalized === 'ok' || normalized === 'passed' || normalized === 'valid') return 'success'
  if (normalized === 'skipped' || normalized === 'unknown' || normalized === '') return 'neutral'
  if (normalized === 'warnings' || normalized === 'warning') return 'warning'
  return 'danger'
}

export function RevisionsTab({
  profileId,
  draftJson,
  summary,
  activeConfigPath,
  onRequestSave,
}: {
  profileId: string
  draftJson: string
  summary: ValidationSummary
  activeConfigPath: string
  onRequestSave: () => void
}): React.ReactElement {
  const revisions = useRevisionsQuery(profileId, REVISION_LIMIT)
  const apply = useApplyRevisionMutation()
  const rollback = useRollbackMutation()
  const compareWithActive = useCompareWithActiveMutation()

  const [selected, setSelected] = React.useState<readonly string[]>([])
  const [applyTarget, setApplyTarget] = React.useState<config.RevisionView | null>(null)
  const [rollbackTarget, setRollbackTarget] = React.useState<config.RevisionView | null>(null)
  const [localError, setLocalError] = React.useState<AppError | null>(null)

  const progress = useCachedEvent<ConfigApplyProgressPayload>(CONFIG_APPLY_PROGRESS_KEY)
  const list = revisions.data?.revisions ?? []
  const blocked = summary.errorCount > 0 || !summary.valid
  const leftId = selected[0] ?? ''
  const rightId = selected[1] ?? ''
  const left = useRevisionQuery(profileId, leftId)
  const right = useRevisionQuery(profileId, rightId)
  const diff = useCompareRevisionsQuery(profileId, leftId, rightId)

  const activeRevision = list.find((revision) => revision.active)

  // A successful apply ends the pending state; the progress event is only a
  // live hint, never the source of truth for completion.
  const applyResult = apply.data?.result
  const rollbackRevision = rollback.data?.revision
  const applyError = apply.error === null ? null : toAppError(apply.error)
  const rollbackError = rollback.error === null ? null : toAppError(rollback.error)
  const failure = localError ?? applyError ?? rollbackError

  const toggleSelection = (revisionId: string) => {
    setSelected((current) => {
      if (current.includes(revisionId)) return current.filter((id) => id !== revisionId)
      if (current.length >= 2) {
        const second = current[1]
        return second === undefined ? [revisionId] : [second, revisionId]
      }
      return [...current, revisionId]
    })
  }

  const confirmApply = () => {
    if (applyTarget === null) return
    setLocalError(null)
    apply.mutate(
      { profileId, revisionId: applyTarget.id },
      {
        onSuccess: () => {
          setApplyTarget(null)
        },
      },
    )
  }

  const confirmRollback = () => {
    if (rollbackTarget === null) return
    setLocalError(null)
    rollback.mutate(
      { profileId, revisionId: rollbackTarget.id },
      {
        onSuccess: () => {
          setRollbackTarget(null)
        },
      },
    )
  }

  const compareDraft = () => {
    const base = draftBaseId(draftJson, list)
    if (base === '') {
      setLocalError({
        code: 'NOT_FOUND',
        message: 'Нет ревизии, с которой можно сравнить черновик: сохраните первую ревизию.',
      })
      return
    }
    setLocalError(null)
    compareWithActive.mutate({ profileId, revisionId: base, draftJson })
  }

  const draftDiff = compareWithActive.data?.diff

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>История ревизий</CardTitle>
          <CardDescription>
            Каждое сохранение и каждое применение создаёт неизменяемый снимок. Последние{' '}
            {REVISION_LIMIT} записей.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <Toolbar>
            <Button variant="secondary" onClick={onRequestSave} disabled={blocked}>
              <Save className="h-4 w-4" aria-hidden />
              Сохранить ревизию
            </Button>
            <Button
              variant="ghost"
              onClick={compareDraft}
              disabled={compareWithActive.isPending || draftJson.trim() === ''}
            >
              <GitCompare className="h-4 w-4" aria-hidden />
              Сравнить черновик с активной
            </Button>
            {blocked ? (
              <span className="text-xs text-muted-foreground">
                Сохранение станет доступным после исправления ошибок.
              </span>
            ) : null}
          </Toolbar>

          {blocked ? (
            <Alert tone="warning" title="Черновик не проходит валидацию">
              Ошибок: {summary.errorCount}, предупреждений: {summary.warningCount}. Ревизию можно
              сохранить только из валидного черновика.
            </Alert>
          ) : null}

          {revisions.isPending ? <Spinner label="Загружаем историю…" /> : null}

          {revisions.error === null || revisions.error === undefined ? null : (
            <Alert tone="danger" title="Не удалось получить историю ревизий">
              {describeError(toAppError(revisions.error))}
            </Alert>
          )}

          {!revisions.isPending && list.length === 0 ? (
            <EmptyState
              title="Ревизий пока нет"
              description="Первая ревизия появится, когда вы сохраните черновик."
            />
          ) : null}

          {list.length === 0 ? null : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Сравнение</TableHead>
                  <TableHead>Ревизия</TableHead>
                  <TableHead>Создана</TableHead>
                  <TableHead>Источник</TableHead>
                  <TableHead>Комментарий</TableHead>
                  <TableHead>Валидация</TableHead>
                  <TableHead className="text-right">Действия</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((revision) => (
                  <TableRow key={revision.id}>
                    <TableCell>
                      <label className="flex items-center gap-2 text-xs">
                        <input
                          type="checkbox"
                          className="h-3.5 w-3.5"
                          aria-label={`Сравнить ревизию ${truncate(revision.id, 12)}`}
                          checked={selected.includes(revision.id)}
                          onChange={() => {
                            toggleSelection(revision.id)
                          }}
                        />
                      </label>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {truncate(revision.id, 14)}
                      {revision.active ? (
                        <Badge tone="success" className="ml-2">
                          активная
                        </Badge>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-xs">{formatDateTime(revision.createdAt)}</TableCell>
                    <TableCell className="text-xs">{revision.source}</TableCell>
                    <TableCell className="text-xs">{revision.comment ?? '—'}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-1">
                        <Badge tone={statusTone(revision.structuralValidationStatus)}>
                          структура
                        </Badge>
                        <Badge tone={statusTone(revision.singBoxValidationStatus)}>sing-box</Badge>
                        {revision.singBoxVersion === '' ? null : (
                          <span className="text-xs text-muted-foreground">
                            v{revision.singBoxVersion}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="secondary"
                          disabled={apply.isPending}
                          onClick={() => {
                            setApplyTarget(revision)
                          }}
                        >
                          <Upload className="h-3.5 w-3.5" aria-hidden />
                          Применить
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={rollback.isPending}
                          onClick={() => {
                            setRollbackTarget(revision)
                          }}
                        >
                          <RotateCcw className="h-3.5 w-3.5" aria-hidden />
                          Откатить
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          <p className="text-xs text-muted-foreground">
            Выберите две ревизии, чтобы увидеть построчное сравнение.
          </p>
        </CardContent>
      </Card>

      {selected.length < 2 ? null : (
        <Card>
          <CardHeader>
            <CardTitle>Сравнение ревизий</CardTitle>
            <CardDescription>
              {diff.data?.diff.leftLabel ?? truncate(leftId, 14)}{' '}
              <ArrowRight className="inline h-3 w-3" />{' '}
              {diff.data?.diff.rightLabel ?? truncate(rightId, 14)}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {diff.isPending ? <Spinner label="Считаем различия…" /> : null}
            {diff.error === undefined || diff.error === null ? null : (
              <Alert tone="danger" title="Не удалось сравнить ревизии">
                {describeError(toAppError(diff.error))}
              </Alert>
            )}
            {diff.data === undefined ? null : (
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge tone={diff.data.diff.identical ? 'success' : 'info'}>
                  {diff.data.diff.identical ? 'идентичны' : 'отличаются'}
                </Badge>
                <span>добавлено строк: {diff.data.diff.added}</span>
                <span>удалено строк: {diff.data.diff.removed}</span>
              </div>
            )}
            {left.isPending || right.isPending ? <Spinner label="Загружаем ревизии…" /> : null}
            {left.data === undefined || right.data === undefined ? null : (
              <JsonDiff
                original={left.data.revision.configJson}
                modified={right.data.revision.configJson}
                originalLabel={truncate(leftId, 12)}
                modifiedLabel={truncate(rightId, 12)}
              />
            )}
          </CardContent>
        </Card>
      )}

      {draftDiff === undefined ? null : (
        <Card>
          <CardHeader>
            <CardTitle>Черновик и активная конфигурация</CardTitle>
            <CardDescription>
              {draftDiff.identical
                ? 'Черновик совпадает с активной конфигурацией.'
                : `Добавлено строк: ${String(draftDiff.added)}, удалено: ${String(draftDiff.removed)}.`}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <DiffLines lines={draftDiff.lines} />
          </CardContent>
        </Card>
      )}

      {apply.isPending || (progress !== undefined && progress.stage !== 'done') ? (
        <Card>
          <CardHeader>
            <CardTitle>Применение конфигурации</CardTitle>
            <CardDescription>
              Этап: {progress === undefined ? 'выполняется' : stageLabel(progress.stage)} —{' '}
              {progress?.percent ?? 0}%
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full bg-primary transition-all"
                style={{ width: `${String(progress?.percent ?? 0)}%` }}
              />
            </div>
            {progress?.message === undefined ? null : (
              <p className="mt-2 font-mono text-xs text-muted-foreground">{progress.message}</p>
            )}
          </CardContent>
        </Card>
      ) : null}

      {applyResult === undefined ? null : (
        <Alert
          tone="success"
          title="Конфигурация применена"
          details={[
            `Ревизия: ${applyResult.revisionId}`,
            `Файл: ${applyResult.activeConfigPath || activeConfigPath}`,
            `Рантайм перезапущен: ${applyResult.restarted ? 'да' : 'нет'}`,
          ]}
        >
          Изменения записаны в активный файл конфигурации.
          {applyResult.warning === undefined ? null : (
            <span className="mt-1 block text-xs">{applyResult.warning}</span>
          )}
        </Alert>
      )}

      {rollbackRevision === undefined ? null : (
        <Alert
          tone="info"
          title="Откат выполнен"
          details={[
            `Новая ревизия: ${rollbackRevision.id}`,
            `Источник: ${rollbackRevision.source}`,
            `Создана: ${formatDateTime(rollbackRevision.createdAt)}`,
          ]}
        >
          Откат создаёт новую ревизию с содержимым выбранной. Активная конфигурация не меняется,
          пока вы не нажмёте «Применить».
        </Alert>
      )}

      {failure === null ? null : (
        <Alert
          tone="danger"
          title="Операция завершилась ошибкой"
          code={failure.code}
          details={failure.details ?? []}
          actions={
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                setLocalError(null)
                apply.reset()
                rollback.reset()
              }}
            >
              Сбросить
            </Button>
          }
        >
          {describeError(failure)}
          {hintFor(failure.code) === undefined ? null : (
            <span className="mt-1 block text-xs">{hintFor(failure.code)}</span>
          )}
        </Alert>
      )}

      <ConfirmDialog
        open={applyTarget !== null}
        onOpenChange={(open) => {
          if (!open) setApplyTarget(null)
        }}
        title="Применить ревизию?"
        description={
          applyTarget === null
            ? ''
            : `Ревизия ${truncate(applyTarget.id, 14)} будет записана в активный файл конфигурации. Если рантайм запущен, он будет перезапущен.`
        }
        confirmLabel="Применить"
        pending={apply.isPending}
        onConfirm={confirmApply}
      >
        <p className="text-xs text-muted-foreground">
          Активный файл: <span className="font-mono">{activeConfigPath || '—'}</span>
        </p>
      </ConfirmDialog>

      <ConfirmDialog
        open={rollbackTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRollbackTarget(null)
        }}
        title="Откатить конфигурацию?"
        description={
          rollbackTarget === null
            ? ''
            : `Будет создана новая ревизия с содержимым ${truncate(rollbackTarget.id, 14)}. Применение к рантайму остаётся отдельным шагом.`
        }
        confirmLabel="Откатить"
        destructive
        pending={rollback.isPending}
        onConfirm={confirmRollback}
      />

      {activeRevision === undefined ? null : (
        <p className="text-xs text-muted-foreground">
          Активная ревизия: <span className="font-mono">{truncate(activeRevision.id, 14)}</span> от{' '}
          {formatDateTime(activeRevision.createdAt)}.
        </p>
      )}
    </div>
  )
}

/**
 * The draft carries the revision it was based on; the diff is only meaningful
 * against that revision, so fall back to the active one.
 */
function draftBaseId(draftJson: string, revisions: readonly config.RevisionView[]): string {
  if (draftJson.trim() === '') return ''
  const active = revisions.find((revision) => revision.active)
  return active?.id ?? revisions[0]?.id ?? ''
}

/** Caps the rendered lines so a large config cannot stall the screen (spec §56). */
const DIFF_LINE_LIMIT = 600

/**
 * Renders the backend's line diff. The two documents are not kept in memory
 * here: `config.Diff` already carries the classified lines and the counts.
 */
function DiffLines({ lines }: { lines: readonly config.DiffLine[] }): React.ReactElement {
  if (lines.length === 0) {
    return <p className="text-xs text-muted-foreground">Различий нет.</p>
  }
  const shown = lines.slice(0, DIFF_LINE_LIMIT)
  return (
    <div className="space-y-2">
      <div className="max-h-[45vh] overflow-auto rounded-md border border-border font-mono text-xs">
        {shown.map((line, index) => (
          <div
            key={`${String(line.left)}:${String(line.right)}:${String(index)}`}
            className={
              line.kind === 'added'
                ? 'flex bg-success/10'
                : line.kind === 'removed'
                  ? 'flex bg-destructive/10'
                  : 'flex'
            }
          >
            <span className="w-12 shrink-0 select-none pr-2 text-right text-muted-foreground">
              {line.kind === 'added' ? '+' : line.kind === 'removed' ? '-' : ' '}
              {line.left === 0 ? '' : String(line.left)}
            </span>
            <span className="whitespace-pre">{line.text}</span>
          </div>
        ))}
      </div>
      {lines.length > shown.length ? (
        <p className="text-xs text-muted-foreground">
          Показаны первые {shown.length} из {lines.length} строк различий.
        </p>
      ) : null}
    </div>
  )
}
