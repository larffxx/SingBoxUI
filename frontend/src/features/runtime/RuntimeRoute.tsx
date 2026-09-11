/**
 * Runtime screen (spec §22, §23, §45–§47).
 *
 * One screen for the supervised sing-box process: start/stop/restart against a
 * chosen profile, the live status the backend pushes on `runtime:status`, the
 * log stream with a level filter, the traffic sparkline fed by
 * `traffic:snapshot`, the Clash API endpoint and the start-at-login switch.
 *
 * Every value comes from the query cache (kept fresh by `@/app/EventBridge`) or
 * from the shell's log buffer — nothing here talks to the backend directly, and
 * with no desktop runtime the screen renders disabled controls plus a notice
 * instead of failing.
 */
import { Activity, Gauge, Play, RotateCw, Square, TriangleAlert } from 'lucide-react'
import * as React from 'react'

import { QueryErrorAlert } from '@/app/errors/AppErrorBoundary'
import { useLogBuffer } from '@/app/logs/logBuffer'
import {
  backendAvailable,
  useActiveProfile,
  useClearLogsMutation,
  useRuntimeLogsQuery,
  useRuntimeSummary,
  useSetAutostartMutation,
  useSettingsQuery,
  useStartRuntimeMutation,
  useStopRuntimeMutation,
  useRestartRuntimeMutation,
  useTrafficQuery,
} from '@/app/queries'
import { formatBytes, formatRate } from '@/shared/lib/format'
import { formatDateTime, formatDuration } from '@/shared/lib/time'
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  Field,
  KeyValueGrid,
  PageHeader,
  Select,
  StatusDot,
  SwitchField,
} from '@/shared/ui'

import { useClashEndpointQuery } from './clash'
import { LogViewer } from './LogViewer'
import {
  exitCodeLabel,
  isRuntimeBusy,
  isRuntimeRunning,
  runtimeStateLabel,
  runtimeStateTone,
} from './state'
import { toRates, toSample, pushSample, type TrafficSample } from './traffic'
import { TrafficSparkline } from './TrafficSparkline'

type ChartMode = 'rate' | 'total'

export function RuntimeRoute(): React.ReactElement {
  const available = backendAvailable()
  const summary = useRuntimeSummary()
  const { profiles, activeId, profile } = useActiveProfile()
  const settingsQuery = useSettingsQuery()
  const trafficQuery = useTrafficQuery()
  const clashQuery = useClashEndpointQuery()
  const logsQuery = useRuntimeLogsQuery()
  const buffer = useLogBuffer()

  const start = useStartRuntimeMutation()
  const stop = useStopRuntimeMutation()
  const restart = useRestartRuntimeMutation()
  const setAutostart = useSetAutostartMutation()
  const clearLogs = useClearLogsMutation()

  const [selectedProfileId, setSelectedProfileId] = React.useState('')
  const [samples, setSamples] = React.useState<readonly TrafficSample[]>([])
  const [chartMode, setChartMode] = React.useState<ChartMode>('rate')
  const [confirmStop, setConfirmStop] = React.useState(false)

  // Default the picker to the active profile (or the first one) once known.
  React.useEffect(() => {
    if (selectedProfileId !== '') return
    const fallback = activeId !== '' ? activeId : (profiles[0]?.id ?? '')
    if (fallback !== '') setSelectedProfileId(fallback)
  }, [activeId, profiles, selectedProfileId])

  // Seed the buffer from the initial snapshot once; after that `runtime:log`
  // events keep it current.
  const seeded = React.useRef(false)
  React.useEffect(() => {
    if (seeded.current) return
    const records = logsQuery.data?.records
    if (!records || records.length === 0) return
    seeded.current = true
    buffer.replace(records)
  }, [buffer, logsQuery.data])

  // `traffic:snapshot` events land in the cache; append each new snapshot once.
  const snapshot = trafficQuery.data?.snapshot
  const seenStamp = React.useRef<unknown>(undefined)
  React.useEffect(() => {
    if (!snapshot || snapshot.timestamp === seenStamp.current) return
    seenStamp.current = snapshot.timestamp
    setSamples((history) => pushSample(history, toSample(snapshot)))
  }, [snapshot])

  const state = summary.state
  const running = isRuntimeRunning(state)
  const busy = isRuntimeBusy(state)
  const status = summary.payload?.status
  const autostart = settingsQuery.data?.state.autostart
  const series = chartMode === 'rate' ? toRates(samples) : [...samples]

  const startError = start.error ?? restart.error ?? null
  const controlError = stop.error ?? null

  return (
    <div className="space-y-4">
      <PageHeader
        title="Рантайм"
        description="Управление процессом sing-box, состояние и журнал."
        actions={
          <Badge tone={runtimeStateTone(state)}>
            <StatusDot tone={runtimeStateTone(state)} pulse={busy} />
            {runtimeStateLabel(state)}
          </Badge>
        }
      />

      {!available ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Управление процессом работает только внутри приложения SingBoxUI. В браузере доступен
          предварительный просмотр интерфейса.
        </Alert>
      ) : null}

      {summary.isError ? (
        <QueryErrorAlert error={summary.error} title="Не удалось получить состояние рантайма" />
      ) : null}

      {status?.lastError ? (
        <Alert
          tone="danger"
          title="Последний запуск завершился с ошибкой"
          code={status.lastErrorCode}
          details={status.lastErrorCode ? [] : [status.lastError]}
        >
          {status.lastError}
        </Alert>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Управление</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <Field
              label="Профиль"
              htmlFor="runtime-profile"
              {...(profiles.length === 0
                ? { hint: 'Профили не найдены — создайте профиль на вкладке «Профили».' }
                : {})}
            >
              <Select
                id="runtime-profile"
                value={selectedProfileId}
                onValueChange={setSelectedProfileId}
                disabled={!available || profiles.length === 0}
                placeholder="Выберите профиль"
                options={profiles.map((candidate) => ({
                  value: candidate.id,
                  label: candidate.name,
                }))}
              />
            </Field>

            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                loading={start.isPending}
                disabled={!available || busy || running || selectedProfileId === ''}
                onClick={() => start.mutate(selectedProfileId)}
              >
                <Play className="h-4 w-4" />
                Запустить
              </Button>
              <Button
                type="button"
                variant="outline"
                loading={restart.isPending}
                disabled={!available || !running || selectedProfileId === ''}
                onClick={() => restart.mutate(selectedProfileId)}
              >
                <RotateCw className="h-4 w-4" />
                Перезапустить
              </Button>
              <Button
                type="button"
                variant="outline"
                loading={stop.isPending}
                disabled={!available || !running}
                onClick={() => setConfirmStop(true)}
              >
                <Square className="h-4 w-4" />
                Остановить
              </Button>
            </div>

            {startError ? <QueryErrorAlert error={startError} title="Запуск не выполнен" /> : null}
            {controlError ? (
              <QueryErrorAlert error={controlError} title="Остановка не выполнена" />
            ) : null}
            {clearLogs.error ? (
              <QueryErrorAlert error={clearLogs.error} title="Не удалось очистить журнал" />
            ) : null}

            <div className="border-t border-border pt-3">
              <SwitchField
                id="runtime-autostart"
                label="Запускать при входе в систему"
                description={
                  autostart?.supported === false
                    ? 'Автозапуск в этой системе не поддерживается.'
                    : 'Приложение стартует при входе в систему и подключает профиль, выбранный ниже в разделе «Автозапуск подключения».'
                }
                checked={autostart?.enabled ?? false}
                disabled={!available || autostart?.supported === false || setAutostart.isPending}
                onCheckedChange={(checked) => setAutostart.mutate(checked)}
              />
              {setAutostart.error ? (
                <QueryErrorAlert
                  error={setAutostart.error}
                  title="Не удалось изменить автозапуск"
                />
              ) : null}
              {autostart?.legacyEntry ? (
                <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
                  <TriangleAlert className="mr-1 inline h-3.5 w-3.5" />
                  Найдена старая запись автозапуска: {autostart.legacyEntry}. Удалить её можно в
                  «Настройках».
                </p>
              ) : null}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Состояние процесса</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: 'Состояние',
                  value: <Badge tone={runtimeStateTone(state)}>{runtimeStateLabel(state)}</Badge>,
                },
                {
                  label: 'Профиль',
                  value: summary.payload?.activeProfileName || profile?.name || '—',
                },
                { label: 'PID', value: status?.pid ? String(status.pid) : '—', mono: true },
                { label: 'Время работы', value: formatDuration(status?.uptimeSeconds) },
                { label: 'Запущен', value: formatDateTime(status?.startedAt) },
                {
                  label: 'Последний выход',
                  value: exitCodeLabel(status?.lastExitCode),
                  mono: status?.lastExitCode !== undefined,
                },
                { label: 'Версия sing-box', value: status?.binaryVersion || '—' },
                {
                  label: 'Повышенные права',
                  value: status?.elevated ? 'Да' : 'Нет',
                },
                {
                  label: 'Конфигурация',
                  value: status?.configPath || '—',
                  mono: true,
                },
                {
                  label: 'Активная ревизия',
                  value: status?.activeRevisionId || '—',
                  mono: true,
                },
              ]}
            />
            {status?.lastErrorCode ? (
              <p className="text-xs text-muted-foreground">
                Код последней ошибки: <span className="font-mono">{status.lastErrorCode}</span>
              </p>
            ) : null}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between gap-2">
          <CardTitle>Трафик</CardTitle>
          <div className="flex items-center gap-2">
            <Badge tone={trafficQuery.data?.collectorRunning ? 'success' : 'warning'}>
              <Activity className="mr-1 h-3.5 w-3.5" />
              {trafficQuery.data?.collectorRunning ? 'Сборщик активен' : 'Сборщик остановлен'}
            </Badge>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setChartMode(chartMode === 'rate' ? 'total' : 'rate')}
            >
              <Gauge className="h-4 w-4" />
              {chartMode === 'rate' ? 'Показывать сумму' : 'Показывать скорость'}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {snapshot && !snapshot.available ? (
            <Alert tone="warning" title="Данные о трафике недоступны">
              {snapshot.error ??
                'Clash API не отвечает — проверьте внешний контроллер в конфигурации.'}
            </Alert>
          ) : null}
          <TrafficSparkline
            samples={series}
            ariaLabel={chartMode === 'rate' ? 'Скорость трафика' : 'Суммарный трафик'}
          />
          <KeyValueGrid
            columns={3}
            items={[
              { label: 'Принято', value: formatBytes(snapshot?.downloadTotal) },
              { label: 'Отправлено', value: formatBytes(snapshot?.uploadTotal) },
              { label: 'Скорость приёма', value: formatRate(snapshot?.downloadRate) },
              { label: 'Скорость отдачи', value: formatRate(snapshot?.uploadRate) },
              { label: 'Соединений', value: String(snapshot?.connections ?? 0) },
              { label: 'Обновлено', value: formatDateTime(snapshot?.timestamp) },
            ]}
          />
          <Field
            label="Clash API"
            htmlFor="runtime-clash"
            hint="Внешний контроллер из активной конфигурации."
          >
            <input
              id="runtime-clash"
              readOnly
              value={clashQuery.data ?? '—'}
              className="w-full rounded-md border border-border bg-muted/30 px-3 py-2 font-mono text-xs"
            />
          </Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Журнал</CardTitle>
        </CardHeader>
        <CardContent>
          <LogViewer
            records={buffer.records}
            dropped={buffer.dropped}
            refreshing={logsQuery.isFetching}
            onRefresh={() => {
              void logsQuery.refetch()
            }}
            onClear={() => {
              buffer.clear()
              clearLogs.mutate()
            }}
          />
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title="Остановить рантайм?"
        description="Текущее подключение будет разорвано, sing-box завершится."
        confirmLabel="Остановить"
        destructive
        pending={stop.isPending}
        onConfirm={() => {
          setConfirmStop(false)
          stop.mutate()
        }}
      />
    </div>
  )
}
