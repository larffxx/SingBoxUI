/**
 * Dashboard (spec §52 — the «Обзор» destination).
 *
 * The landing screen answers four questions: which profile is active, is
 * sing-box running (with quick start/stop), how much traffic moved, and which
 * config file the runtime is serving. It also surfaces the two prompts the
 * spec asks for: a pending sing-box update and a legacy config detected on
 * disk (`configApi.detectLegacy()`).
 */
import { useNavigate } from '@tanstack/react-router'
import {
  ArrowRight,
  Download,
  FileJson,
  Gauge,
  Layers,
  Play,
  Square,
  TriangleAlert,
} from 'lucide-react'
import * as React from 'react'

import { QueryErrorAlert } from '@/app/errors/AppErrorBoundary'
import {
  backendAvailable,
  useActiveConfigPathQuery,
  useActiveProfile,
  useBinaryStatusQuery,
  useImportLegacyConfigMutation,
  useLegacyConfigQuery,
  useRuntimeSummary,
  useSettingsQuery,
  useStartRuntimeMutation,
  useStopRuntimeMutation,
  useTrafficQuery,
} from '@/app/queries'
import { formatBytes, formatRate } from '@/shared/lib/format'
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
  EmptyState,
  KeyValueGrid,
  PageHeader,
  StatusDot,
} from '@/shared/ui'

import { ForeignRuntimeNotice } from '@/features/runtime/ForeignRuntimeNotice'
import {
  exitCodeLabel,
  isRuntimeBusy,
  isRuntimeRunning,
  runtimeStateLabel,
  runtimeStateTone,
} from '@/features/runtime/state'

export function DashboardRoute(): React.ReactElement {
  const navigate = useNavigate()
  const available = backendAvailable()
  const summary = useRuntimeSummary()
  const { profile, activeId, profiles } = useActiveProfile()
  const trafficQuery = useTrafficQuery()
  const configPathQuery = useActiveConfigPathQuery()
  const binaryQuery = useBinaryStatusQuery()
  const legacyQuery = useLegacyConfigQuery()
  const settingsQuery = useSettingsQuery()

  const start = useStartRuntimeMutation()
  const stop = useStopRuntimeMutation()
  const importLegacy = useImportLegacyConfigMutation()

  const [legacyDismissed, setLegacyDismissed] = React.useState(false)

  const state = summary.state
  const running = isRuntimeRunning(state)
  const busy = isRuntimeBusy(state)
  const status = summary.payload?.status
  const snapshot = trafficQuery.data?.snapshot
  const configPath = configPathQuery.data || status?.configPath || ''
  const pendingUpdate = binaryQuery.data?.status.update
  const candidate = legacyQuery.data?.candidate
  const updatesEnabled = settingsQuery.data?.state.values.updateCheckEnabled !== false

  return (
    <div className="space-y-4">
      <PageHeader
        title="Обзор"
        description="Состояние подключения, активный профиль и быстрые действия."
        actions={
          <Badge tone={runtimeStateTone(state)}>
            <StatusDot tone={runtimeStateTone(state)} pulse={busy} />
            {runtimeStateLabel(state)}
          </Badge>
        }
      />

      {!available ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Приложение запущено вне окна SingBoxUI: данные не загружаются, а действия недоступны. Это
          ожидаемо при предварительном просмотре интерфейса.
        </Alert>
      ) : null}

      {candidate?.found && !legacyDismissed ? (
        <Alert
          tone="info"
          title="Найдена конфигурация предыдущей версии"
          details={candidate.warnings ?? []}
          actions={
            <>
              <Button
                type="button"
                size="sm"
                loading={importLegacy.isPending}
                onClick={() => importLegacy.mutate(candidate.configPath)}
              >
                <Download className="h-4 w-4" />
                Импортировать как профиль
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => setLegacyDismissed(true)}
              >
                Позже
              </Button>
            </>
          }
        >
          <p>
            Файл <span className="font-mono">{candidate.configPath}</span> ({' '}
            {formatBytes(candidate.size)}, {formatDateTime(candidate.modified)}) можно перенести в
            новый формат профилей.
          </p>
          {!candidate.valid ? (
            <p className="mt-1 text-amber-600 dark:text-amber-400">
              Конфигурация выглядит повреждённой — импорт может потребовать правок.
            </p>
          ) : null}
        </Alert>
      ) : null}

      {importLegacy.error ? (
        <QueryErrorAlert error={importLegacy.error} title="Не удалось импортировать конфигурацию" />
      ) : null}

      {pendingUpdate && updatesEnabled ? (
        <Alert
          tone="info"
          title={`Доступно обновление sing-box ${pendingUpdate.version}`}
          actions={
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => navigate({ to: '/binary' })}
            >
              Перейти к компоненту
              <ArrowRight className="h-4 w-4" />
            </Button>
          }
        >
          Опубликовано {formatDateTime(pendingUpdate.publishedAt)} · {pendingUpdate.assetName}
        </Alert>
      ) : null}

      {binaryQuery.data?.status.checkError && updatesEnabled ? (
        <Alert tone="warning" title="Не удалось проверить обновления">
          {binaryQuery.data.status.checkError}
        </Alert>
      ) : null}

      <ForeignRuntimeNotice />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Активный профиль</CardTitle>
            <CardDescription>Профиль, который использует рантайм.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {profile ? (
              <>
                <KeyValueGrid
                  columns={2}
                  items={[
                    { label: 'Название', value: profile.name },
                    { label: 'Идентификатор', value: profile.id, mono: true },
                    {
                      label: 'Активная ревизия',
                      value: profile.activeRevisionId || '—',
                      mono: true,
                    },
                    { label: 'Обновлён', value: formatDateTime(profile.updatedAt) },
                    { label: 'Последнее подключение', value: formatDateTime(profile.lastUsedAt) },
                    { label: 'Профилей всего', value: String(profiles.length) },
                  ]}
                />
                {profile.description ? (
                  <p className="text-sm text-muted-foreground">{profile.description}</p>
                ) : null}
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      navigate({ to: '/profiles/$profileId', params: { profileId: profile.id } })
                    }
                  >
                    <Layers className="h-4 w-4" />
                    Открыть рабочую область
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={() => navigate({ to: '/profiles' })}
                  >
                    Все профили
                  </Button>
                </div>
              </>
            ) : (
              <EmptyState
                title="Активный профиль не выбран"
                description="Создайте профиль или выберите существующий, чтобы запустить sing-box."
                action={
                  <Button type="button" size="sm" onClick={() => navigate({ to: '/profiles' })}>
                    <Layers className="h-4 w-4" />
                    Перейти к профилям
                  </Button>
                }
              />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Рантайм</CardTitle>
            <CardDescription>Процесс sing-box и быстрые действия.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: 'Состояние',
                  value: <Badge tone={runtimeStateTone(state)}>{runtimeStateLabel(state)}</Badge>,
                },
                { label: 'PID', value: status?.pid ? String(status.pid) : '—', mono: true },
                { label: 'Последний выход', value: exitCodeLabel(status?.lastExitCode) },
                { label: 'Версия sing-box', value: status?.binaryVersion || '—' },
              ]}
            />
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                loading={start.isPending}
                disabled={!available || busy || running || activeId === ''}
                onClick={() => start.mutate(activeId)}
              >
                <Play className="h-4 w-4" />
                Запустить
              </Button>
              <Button
                type="button"
                variant="outline"
                loading={stop.isPending}
                disabled={!available || !running}
                onClick={() => stop.mutate()}
              >
                <Square className="h-4 w-4" />
                Остановить
              </Button>
              <Button type="button" variant="ghost" onClick={() => navigate({ to: '/runtime' })}>
                <Gauge className="h-4 w-4" />
                Подробнее
              </Button>
            </div>
            {start.error ? (
              <QueryErrorAlert error={start.error} title="Запуск не выполнен" />
            ) : null}
            {stop.error ? (
              <QueryErrorAlert error={stop.error} title="Остановка не выполнена" />
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Трафик</CardTitle>
            <CardDescription>Суммарные счётчики Clash API.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <KeyValueGrid
              columns={2}
              items={[
                { label: 'Принято', value: formatBytes(snapshot?.downloadTotal) },
                { label: 'Отправлено', value: formatBytes(snapshot?.uploadTotal) },
                { label: 'Скорость приёма', value: formatRate(snapshot?.downloadRate) },
                { label: 'Скорость отдачи', value: formatRate(snapshot?.uploadRate) },
                { label: 'Активных соединений', value: String(snapshot?.connections ?? 0) },
                { label: 'Обновлено', value: formatDateTime(snapshot?.timestamp) },
              ]}
            />
            {trafficQuery.data && !trafficQuery.data.collectorRunning ? (
              <p className="text-xs text-muted-foreground">
                Сборщик трафика остановлен: счётчики обновляются только при запущенном подключении.
              </p>
            ) : null}
            {snapshot && !snapshot.available && snapshot.error ? (
              <p className="text-xs text-amber-600 dark:text-amber-400">{snapshot.error}</p>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Конфигурация и компонент</CardTitle>
            <CardDescription>Активный файл конфигурации и версия sing-box.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <KeyValueGrid
              columns={1}
              items={[
                { label: 'Активный файл', value: configPath || '—', mono: true },
                { label: 'Источник компонента', value: binaryQuery.data?.status.source || '—' },
                {
                  label: 'Версия компонента',
                  value: binaryQuery.data?.status.activeVersion || '—',
                },
              ]}
            />
            {configPathQuery.isError ? (
              <QueryErrorAlert
                error={configPathQuery.error}
                title="Путь к конфигурации недоступен"
              />
            ) : null}
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => navigate({ to: '/binary' })}
              >
                <Download className="h-4 w-4" />
                Обновления sing-box
              </Button>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => navigate({ to: '/profiles' })}
              >
                <FileJson className="h-4 w-4" />
                Редактор конфигурации
              </Button>
            </div>
            {binaryQuery.data && !binaryQuery.data.status.activeOk ? (
              <p className="text-xs text-amber-600 dark:text-amber-400">
                <TriangleAlert className="mr-1 inline h-3.5 w-3.5" />
                {binaryQuery.data.status.activeError ?? 'Исполняемый файл sing-box недоступен.'}
              </p>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
