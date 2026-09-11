/**
 * Binary manager (spec §17–§21).
 *
 * The screen answers four questions in order: what is installed, where it came
 * from, whether a newer release exists, and what exactly happens when you press
 * install. The last one is the reason the security posture is spelled out on the
 * screen instead of living in the docs: the archive digest is verified against
 * the one published by the release, there is no code signature to verify, and
 * nothing is ever installed silently.
 */
import * as React from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  HardDriveDownload,
  RefreshCw,
  ShieldCheck,
} from 'lucide-react'

import { QueryErrorAlert } from '@/app/errors/AppErrorBoundary'
import { backendAvailable } from '@/app/queries'
import { describeError, hintFor, toAppError, type AppError } from '@/shared/api/errors'
import { formatBytes } from '@/shared/lib/format'
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
  Field,
  Input,
  KeyValueGrid,
  PageHeader,
  Select,
  Spinner,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Toolbar,
} from '@/shared/ui'

import { binaryStageLabel, useBinaryProgress } from './progress'
import {
  useBinaryStatusQuery,
  useCheckForUpdatesMutation,
  useInstallStableUpdateMutation,
  useLastUpdateCheckQuery,
  useProbeBinaryMutation,
  useSetBinarySourceMutation,
} from './queries'

const SOURCE_MANAGED = 'managed'
const SOURCE_CUSTOM = 'custom'

export function BinaryRoute(): React.ReactElement {
  const available = backendAvailable()
  const statusQuery = useBinaryStatusQuery()
  const lastCheckQuery = useLastUpdateCheckQuery()

  const check = useCheckForUpdatesMutation()
  const install = useInstallStableUpdateMutation()
  const setSource = useSetBinarySourceMutation()
  const probe = useProbeBinaryMutation()
  const progress = useBinaryProgress()

  const [installOpen, setInstallOpen] = React.useState(false)
  const [probeOpen, setProbeOpen] = React.useState(false)
  const [probePath, setProbePath] = React.useState('')
  const [source, setSourceMode] = React.useState(SOURCE_MANAGED)
  const [customPath, setCustomPath] = React.useState('')
  const [probedPath, setProbedPath] = React.useState('')
  const [notice, setNotice] = React.useState<string | null>(null)

  const status = statusQuery.data?.status
  const lastCheck = lastCheckQuery.data?.check ?? status?.lastCheck
  const installed = status?.managed

  // Keep the source form in sync with the backend until the user edits it.
  const syncedSource = React.useRef('')
  React.useEffect(() => {
    if (status === undefined) return
    const signature = `${status.source}::${status.customPath ?? ''}`
    if (syncedSource.current === signature) return
    syncedSource.current = signature
    setSourceMode(status.source === SOURCE_CUSTOM ? SOURCE_CUSTOM : SOURCE_MANAGED)
    setCustomPath(status.customPath ?? '')
  }, [status])

  const failure =
    toAppErrorOrNull(check.error) ??
    toAppErrorOrNull(install.error) ??
    toAppErrorOrNull(setSource.error) ??
    toAppErrorOrNull(probe.error)

  const update = lastCheck?.update
  const prerelease = update !== undefined && update.version.includes('-')
  const downgrade =
    update !== undefined && compareVersions(update.version, lastCheck?.currentVersion ?? '') < 0
  const customSelected = source === SOURCE_CUSTOM
  const customReady = customSelected && customPath.trim() !== '' && probedPath === customPath.trim()

  return (
    <div className="space-y-4">
      <PageHeader
        title="Бинарник sing-box"
        description="Управляемая версия: приложение скачивает релиз, проверяет контрольную сумму и хранит предыдущую версию для отката."
        actions={
          <Toolbar>
            <Button
              variant="secondary"
              disabled={check.isPending || !available}
              onClick={() => {
                setNotice(null)
                check.mutate(undefined, {
                  onSuccess: (payload) => {
                    const found = payload.check.update
                    setNotice(
                      payload.check.updateAvailable && found !== undefined
                        ? `Доступна версия ${found.version} (${found.tag}).`
                        : `Обновлений нет. Текущая версия: ${payload.check.currentVersion === '' ? 'не определена' : payload.check.currentVersion}.`,
                    )
                  },
                })
              }}
            >
              <RefreshCw className="h-4 w-4" aria-hidden />
              Проверить обновления
            </Button>
            <Button
              disabled={!available || install.isPending || update === undefined}
              onClick={() => {
                setInstallOpen(true)
              }}
            >
              <Download className="h-4 w-4" aria-hidden />
              Установить стабильную версию
            </Button>
          </Toolbar>
        }
      />

      {!available ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Менеджер бинарника работает только внутри десктопного приложения.
        </Alert>
      ) : null}

      {failure === null ? null : (
        <Alert
          tone="danger"
          title="Операция не выполнена"
          code={failure.code}
          details={failure.details ?? []}
          actions={
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                check.reset()
                install.reset()
                setSource.reset()
                probe.reset()
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

      <QueryErrorAlert error={statusQuery.error} />
      <QueryErrorAlert error={lastCheckQuery.error} />

      <Card>
        <CardHeader>
          <CardTitle>Что установлено</CardTitle>
          <CardDescription>Источник, активный файл и результат последней проверки.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {statusQuery.isPending ? <Spinner label="Читаем состояние…" /> : null}

          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={status?.source === SOURCE_CUSTOM ? 'info' : 'neutral'}>
              источник: {status?.source === SOURCE_CUSTOM ? 'свой бинарник' : 'управляемый'}
            </Badge>
            <Badge tone={status?.activeOk === true ? 'success' : 'danger'}>
              {status?.activeOk === true ? 'бинарник рабочий' : 'бинарник не проверен'}
            </Badge>
            {status?.unsupported === true ? (
              <Badge tone="warning">платформа не поддержана</Badge>
            ) : null}
            {installed?.installed === true ? (
              <Badge tone="info">управляемая версия {installed.version ?? '—'}</Badge>
            ) : (
              <Badge tone="neutral">управляемая версия не установлена</Badge>
            )}
          </div>

          {status?.activeError === undefined || status.activeError === '' ? null : (
            <Alert tone="danger" title="Активный бинарник не отвечает">
              {status.activeError}
            </Alert>
          )}

          {status?.checkError === undefined || status.checkError === '' ? null : (
            <Alert tone="warning" title="Последняя проверка обновлений не удалась">
              {status.checkError}
            </Alert>
          )}

          <KeyValueGrid
            columns={2}
            items={[
              { label: 'Платформа', value: status?.platform ?? '—' },
              { label: 'Активный файл', value: status?.activePath ?? '—', mono: true },
              { label: 'Активная версия', value: status?.activeVersion ?? '—' },
              {
                label: 'Путь управляемой версии',
                value: installed?.path ?? '—',
                mono: true,
              },
              {
                label: 'SHA-256 управляемой версии',
                value: installed?.sha256 ?? '—',
                mono: true,
              },
              {
                label: 'Установлена',
                value:
                  installed?.installedAt === undefined
                    ? '—'
                    : formatDateTime(installed.installedAt),
              },
              { label: 'Свой путь', value: status?.customPath ?? '—', mono: true },
              {
                label: 'Последняя проверка',
                value:
                  lastCheck?.checkedAt === undefined ? '—' : formatDateTime(lastCheck.checkedAt),
              },
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Источник бинарника</CardTitle>
          <CardDescription>
            Управляемый режим сам скачивает релизы. Свой бинарник принимается только после успешной
            проверки: приложение запускает файл с <code>--version</code> и убеждается, что это
            действительно sing-box.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <Field label="Режим" htmlFor="binary-source">
            <Select
              id="binary-source"
              value={source}
              onValueChange={(value) => {
                setSourceMode(value)
                setProbedPath('')
              }}
              options={[
                { value: SOURCE_MANAGED, label: 'Управляемый (релизы GitHub)' },
                { value: SOURCE_CUSTOM, label: 'Свой бинарник по пути' },
              ]}
            />
          </Field>

          {customSelected ? (
            <Field
              label="Путь к бинарнику"
              htmlFor="binary-custom-path"
              hint="Путь проверяется до применения: без успешной проверки источник не переключится."
            >
              <Input
                id="binary-custom-path"
                value={customPath}
                onChange={(event) => {
                  setCustomPath(event.target.value)
                  setProbedPath('')
                }}
                placeholder="/usr/local/bin/sing-box"
              />
            </Field>
          ) : null}

          <Toolbar>
            {customSelected ? (
              <Button
                variant="secondary"
                disabled={probe.isPending || customPath.trim() === ''}
                onClick={() => {
                  setNotice(null)
                  probe.mutate(customPath.trim(), {
                    onSuccess: (payload) => {
                      setProbedPath(customPath.trim())
                      setNotice(`Файл проверен: sing-box ${payload.version}.`)
                    },
                  })
                }}
              >
                Проверить путь
              </Button>
            ) : null}
            <Button
              disabled={
                setSource.isPending ||
                (customSelected && !customReady) ||
                (!customSelected && status?.source === SOURCE_MANAGED)
              }
              onClick={() => {
                setNotice(null)
                setSource.mutate(
                  {
                    source: customSelected ? SOURCE_CUSTOM : SOURCE_MANAGED,
                    customPath: customPath.trim(),
                  },
                  {
                    onSuccess: (payload) => {
                      syncedSource.current = `${payload.status.source}::${
                        payload.status.customPath ?? ''
                      }`
                      setNotice(
                        `Источник переключён: ${payload.status.source === SOURCE_CUSTOM ? 'свой бинарник' : 'управляемый'}.`,
                      )
                    },
                  },
                )
              }}
            >
              Применить источник
            </Button>
            {customSelected && !customReady ? (
              <span className="text-xs text-muted-foreground">
                Сначала проверьте путь: непроверенный файл не принимается.
              </span>
            ) : null}
            {customSelected && probedPath !== '' ? (
              <Badge tone="success">путь проверен: {probedPath}</Badge>
            ) : null}
          </Toolbar>

          <Toolbar>
            <Button
              variant="ghost"
              onClick={() => {
                setProbeOpen(true)
              }}
            >
              Проверить произвольный файл
            </Button>
          </Toolbar>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Обновления</CardTitle>
          <CardDescription>
            Последняя известная проверка релиза. Ничего не устанавливается автоматически.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {lastCheck === undefined ? (
            <p className="text-sm text-muted-foreground">
              Проверок ещё не было. Нажмите «Проверить обновления».
            </p>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={lastCheck.updateAvailable ? 'success' : 'neutral'}>
                  {lastCheck.updateAvailable ? 'доступно обновление' : 'обновлений нет'}
                </Badge>
                {lastCheck.unsupported ? (
                  <Badge tone="warning">платформа не поддержана</Badge>
                ) : null}
                {lastCheck.noInstalledBinary ? (
                  <Badge tone="warning">бинарник ещё не установлен</Badge>
                ) : null}
                {prerelease ? <Badge tone="warning">предрелиз</Badge> : null}
                {downgrade ? <Badge tone="danger">понижение версии</Badge> : null}
              </div>

              {prerelease ? (
                <Alert tone="warning" title="Это предрелиз">
                  Версия {update?.version} помечена как предрелизная. Стабильный канал не должен
                  предлагать её — проверьте тег перед установкой.
                </Alert>
              ) : null}

              {downgrade ? (
                <Alert tone="danger" title="Предлагаемая версия старее установленной">
                  Установлена {lastCheck.currentVersion}, предлагается {update?.version}. Это откат:
                  ревизии, применённые на новой версии, придётся применить снова.
                </Alert>
              ) : null}

              {update === undefined ? (
                <p className="text-sm text-muted-foreground">
                  Текущая версия:{' '}
                  {lastCheck.currentVersion === '' ? 'не определена' : lastCheck.currentVersion}.
                </p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Поле релиза</TableHead>
                      <TableHead>Значение</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableRow>
                      <TableCell>Версия</TableCell>
                      <TableCell className="font-mono text-xs">{update.version}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Тег</TableCell>
                      <TableCell className="font-mono text-xs">{update.tag}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Ассет платформы</TableCell>
                      <TableCell className="font-mono text-xs">{update.assetName}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Размер архива</TableCell>
                      <TableCell>{formatBytes(update.size)}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Контрольная сумма</TableCell>
                      <TableCell>
                        {update.sha256 === '' ? (
                          <Badge tone="danger">не опубликована</Badge>
                        ) : (
                          <span className="font-mono text-xs">{update.sha256}</span>
                        )}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Опубликован</TableCell>
                      <TableCell>{formatDateTime(update.publishedAt)}</TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {install.isPending || progress !== undefined ? (
        <Card>
          <CardHeader>
            <CardTitle>Установка</CardTitle>
            <CardDescription>
              Прогресс приходит из события <code>binary:progress</code>; проценты, а не байты —
              байтовые счётчики в событии не передаются.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full bg-primary transition-all"
                style={{ width: `${clampPercent(progress?.percent ?? 0)}%` }}
              />
            </div>
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              {install.isPending ? <Spinner label="Идёт установка…" /> : null}
              <span>этап: {binaryStageLabel(progress?.stage ?? '')}</span>
              <span>{clampPercent(progress?.percent ?? 0)}%</span>
              {progress?.version === undefined || progress.version === '' ? null : (
                <span className="font-mono">версия: {progress.version}</span>
              )}
            </div>
            {install.isPending ? (
              <p className="text-xs text-muted-foreground">
                Не закрывайте приложение: установка идёт в фоне, но состояние читается после её
                завершения.
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {install.data === undefined ? null : (
        <Card>
          <CardHeader>
            <CardTitle>Результат установки</CardTitle>
            <CardDescription>
              Предыдущая версия сохранена на диске — откат возможен без повторного скачивания.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="success">
                <CheckCircle2 className="h-3 w-3" aria-hidden /> установлено{' '}
                {install.data.install.version}
              </Badge>
              <Badge tone={install.data.install.staleRevisions > 0 ? 'warning' : 'neutral'}>
                ревизий к повторному применению: {install.data.install.staleRevisions}
              </Badge>
            </div>
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: 'Статус',
                  value: install.data.install.status.activeOk ? 'работает' : 'не проверен',
                },
                { label: 'Путь', value: install.data.install.path, mono: true },
                { label: 'SHA-256', value: install.data.install.sha256, mono: true },
                {
                  label: 'Предыдущая версия',
                  value: install.data.install.previousPath ?? 'не было',
                  mono: true,
                },
                { label: 'Установлено', value: formatDateTime(install.data.install.installedAt) },
              ]}
            />
            {install.data.install.staleRevisions > 0 ? (
              <Alert tone="warning" title="Ревизии нужно применить повторно">
                После смены версии sing-box {install.data.install.staleRevisions} ревизий помечены
                как требующие повторной проверки. Откройте профиль и примените активную ревизию.
              </Alert>
            ) : null}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Безопасность обновления</CardTitle>
          <CardDescription>Что именно проверяется перед запуском нового бинарника.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <div className="flex items-start gap-2">
            <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            <p>
              Архив скачивается по HTTPS из GitHub Releases и сверяется с SHA-256, опубликованным в
              релизе. Несовпадение прерывает установку с кодом <code>BINARY_CHECKSUM_FAILED</code> —
              неверный архив на диск не попадает.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" aria-hidden />
            <p>
              Подпись релиза не проверяется: у sing-box нет подписи/нотаризации, проверяется только
              контрольная сумма. Это ограничение канала, а не приложения.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <HardDriveDownload className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
            <p>
              Обновления никогда не ставятся молча: скачивание и подмена файла запускаются только
              кнопкой и подтверждением. После установки приложение сообщает путь, хеш и сохранённую
              предыдущую версию.
            </p>
          </div>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        title="Установить стабильную версию?"
        description={
          update === undefined
            ? 'Версия релиза неизвестна — сначала проверьте обновления.'
            : `Будет скачан и установлен sing-box ${update.version} (${update.assetName}).`
        }
        confirmLabel="Скачать и установить"
        pending={install.isPending}
        onConfirm={() => {
          setNotice(null)
          install.mutate(undefined, {
            onSuccess: (payload) => {
              setInstallOpen(false)
              setNotice(`sing-box ${payload.install.version} установлен в ${payload.install.path}.`)
            },
          })
        }}
      >
        <div className="space-y-2 text-sm">
          <p>
            Приложение сверит SHA-256 архива с опубликованным в релизе и запустит новый бинарник в
            режиме проверки версии перед тем, как сделать его активным.
          </p>
          {update === undefined ? null : (
            <p className="text-xs text-muted-foreground">
              Ассет: <span className="font-mono">{update.assetName}</span>, размер{' '}
              {formatBytes(update.size)}, опубликован {formatDateTime(update.publishedAt)}.
            </p>
          )}
          {downgrade ? (
            <Alert tone="danger" title="Это понижение версии">
              Текущая версия выше предлагаемой. Продолжайте только если это осознанный откат.
            </Alert>
          ) : null}
        </div>
      </ConfirmDialog>

      <ProbeDialog
        open={probeOpen}
        onOpenChange={setProbeOpen}
        pending={probe.isPending}
        result={probe.data === undefined ? undefined : probe.data.version.Raw}
        path={probePath}
        onPathChange={setProbePath}
        onSubmit={(path) => {
          setNotice(null)
          probe.mutate(path, {
            onSuccess: (payload) => {
              setNotice(`Файл проверен: sing-box ${payload.version.Raw}.`)
            },
          })
        }}
      />
    </div>
  )
}

/** Standalone `ProbeBinary` tool: check any file without changing the source. */
function ProbeDialog({
  open,
  onOpenChange,
  pending,
  result,
  path,
  onPathChange,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  pending: boolean
  result: string | undefined
  path: string
  onPathChange: (next: string) => void
  onSubmit: (path: string) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Проверить бинарник"
        description="Запускает файл с --version и показывает, что он вернул. Источник бинарника не меняется."
      >
        <div className="space-y-4">
          <Field label="Путь к файлу" htmlFor="probe-path">
            <Input
              id="probe-path"
              value={path}
              onChange={(event) => {
                onPathChange(event.target.value)
              }}
              placeholder="/usr/local/bin/sing-box"
            />
          </Field>

          {result === undefined ? null : (
            <Alert tone="success" title="Файл отвечает">
              Версия: <span className="font-mono">{result}</span>
            </Alert>
          )}

          <div className="flex justify-end gap-2">
            <Button
              variant="ghost"
              onClick={() => {
                onOpenChange(false)
              }}
            >
              Закрыть
            </Button>
            <Button
              disabled={pending || path.trim() === ''}
              onClick={() => {
                onSubmit(path.trim())
              }}
            >
              {pending ? <Spinner label="Проверяем…" /> : null}
              Проверить
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function clampPercent(value: number): number {
  if (Number.isNaN(value)) return 0
  return Math.max(0, Math.min(100, Math.round(value)))
}

/**
 * Compares dotted versions with an optional `-prerelease` suffix. Strings that
 * are not versions compare as equal, so an unknown current version never
 * produces a bogus "downgrade" warning.
 */
function compareVersions(left: string, right: string): number {
  const a = parseVersion(left)
  const b = parseVersion(right)
  if (a === null || b === null) return 0
  for (let index = 0; index < 3; index += 1) {
    const diff = (a.numbers[index] ?? 0) - (b.numbers[index] ?? 0)
    if (diff !== 0) return diff < 0 ? -1 : 1
  }
  if (a.prerelease === b.prerelease) return 0
  if (a.prerelease === '') return 1
  if (b.prerelease === '') return -1
  return a.prerelease < b.prerelease ? -1 : 1
}

function parseVersion(value: string): { numbers: number[]; prerelease: string } | null {
  const trimmed = value.trim().replace(/^v/, '')
  if (trimmed === '') return null
  const dash = trimmed.indexOf('-')
  const core = dash < 0 ? trimmed : trimmed.slice(0, dash)
  const prerelease = dash < 0 ? '' : trimmed.slice(dash + 1)
  const numbers = core.split('.').map((part) => Number.parseInt(part, 10))
  if (numbers.length < 2 || numbers.some((part) => Number.isNaN(part))) return null
  return { numbers, prerelease }
}

function toAppErrorOrNull(value: unknown): AppError | null {
  if (value === null || value === undefined) return null
  return toAppError(value)
}
