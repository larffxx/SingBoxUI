/**
 * About screen (spec §52 — «О программе»).
 *
 * Everything on this screen is observed, never hardcoded: the version and the
 * build facts come from `settingsApi.environment()` (the same payload the
 * settings screen renders read-only), and the managed sing-box facts come from
 * `binaryApi.status()`. The pointers at the bottom name the files inside the
 * repository that carry the licence and the specification.
 */
import { Boxes, FileText, Info, Package } from 'lucide-react'
import * as React from 'react'

import { messageFor } from '@/app/errors/messageFor'
import { backendAvailable, useBinaryStatusQuery, useEnvironmentQuery } from '@/app/queries'
import {
  Alert,
  Badge,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  KeyValueGrid,
  PageHeader,
} from '@/shared/ui'
import { formatBytes } from '@/shared/lib/format'
import { formatDateTime } from '@/shared/lib/time'

const DASH = '—'

const SPEC_POINTERS: readonly { label: string; path: string }[] = [
  {
    label: 'Описание интерфейса и рабочих потоков',
    path: 'docs/architecture/frontend-workstreams.md',
  },
  { label: 'Внутренние контракты бэкенда', path: 'docs/architecture/internal-contracts.md' },
  { label: 'План миграции с Java-прототипа', path: 'docs/architecture/migration-plan.md' },
  { label: 'Текущее состояние проекта', path: 'docs/architecture/current-state.md' },
  { label: 'Лицензия (GNU GPL v3)', path: 'LICENSE' },
  { label: 'Общее описание проекта', path: 'README.md' },
]

export function AboutRoute(): React.ReactElement {
  const available = backendAvailable()
  const environment = useEnvironmentQuery()
  const binary = useBinaryStatusQuery()

  const env = environment.data?.environment
  const status = binary.data?.status
  const managed = status?.managed
  const update = status?.update

  return (
    <div className="space-y-4">
      <PageHeader
        title="О программе"
        description="SingBoxUI — графическая оболочка для управления sing-box."
        actions={<Badge tone="info">Версия {env?.version ?? DASH}</Badge>}
      />

      {!available ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Сведения о сборке и компоненте доступны только внутри приложения SingBoxUI.
        </Alert>
      ) : null}

      {environment.isError ? (
        <Alert
          tone="danger"
          title="Не удалось получить сведения об окружении"
          code={messageFor(environment.error).code}
        >
          {messageFor(environment.error).message}
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Info className="h-4 w-4" />
            Версия и сборка
          </CardTitle>
          <CardDescription>Сведения о запущенном экземпляре приложения.</CardDescription>
        </CardHeader>
        <CardContent>
          <KeyValueGrid
            columns={2}
            items={[
              { label: 'Версия приложения', value: env?.version ?? DASH },
              { label: 'Операционная система', value: env?.os ?? DASH },
              { label: 'Архитектура', value: env?.arch ?? DASH },
              {
                label: 'Поддержка платформы',
                value: env === undefined ? DASH : env.supported ? 'Поддерживается' : 'Ограничена',
              },
              { label: 'Исполняемый файл', value: env?.execPath ?? DASH, mono: true },
              { label: 'Каталог данных', value: env?.dataDir ?? DASH, mono: true },
              { label: 'Каталог конфигураций', value: env?.configDir ?? DASH, mono: true },
              { label: 'Каталог журналов', value: env?.logPath ?? DASH, mono: true },
              { label: 'Каталог компонента', value: env?.binDir ?? DASH, mono: true },
              { label: 'Рабочий каталог рантайма', value: env?.runtimeDir ?? DASH, mono: true },
              {
                label: 'Поддерживаемые схемы',
                value: (env?.shareSchemes ?? []).join(', ') || DASH,
              },
            ]}
          />
          {env?.supported === false ? (
            <div className="mt-3">
              <Alert tone="warning" title="Ограниченная поддержка платформы">
                {env.unsupportedReason ?? 'Часть возможностей может быть недоступна.'}
              </Alert>
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Package className="h-4 w-4" />
            Компонент sing-box
          </CardTitle>
          <CardDescription>
            Какой исполняемый файл используется и что о нём известно.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {binary.isError ? (
            <Alert
              tone="danger"
              title="Не удалось получить состояние компонента"
              code={messageFor(binary.error).code}
            >
              {messageFor(binary.error).message}
            </Alert>
          ) : null}
          <KeyValueGrid
            columns={2}
            items={[
              { label: 'Источник', value: status?.source ?? (available ? DASH : 'недоступно') },
              { label: 'Версия управляемого компонента', value: managed?.version ?? DASH },
              {
                label: 'Установлен',
                value: managed?.installed === true ? 'Да' : 'Нет',
              },
              { label: 'Дата установки', value: formatDateTime(managed?.installedAt) || DASH },
              { label: 'Каталог компонента', value: managed?.path ?? DASH, mono: true },
              { label: 'Активная версия', value: status?.activeVersion ?? DASH },
              {
                label: 'Активный файл',
                value: status?.activePath ?? (env ? env.activeConfigPath : DASH),
                mono: true,
              },
              {
                label: 'Работоспособность',
                value: status?.activeOk === true ? 'Проверен' : status ? 'Не проверен' : DASH,
              },
              {
                label: 'Последняя проверка обновлений',
                value: formatDateTime(status?.lastCheck) || DASH,
              },
              { label: 'Свой путь', value: status?.customPath ?? DASH, mono: true },
            ]}
          />
          {status?.activeError ? (
            <Alert tone="danger" title="Компонент не запускается">
              {status.activeError}
            </Alert>
          ) : null}
          {status?.checkError ? (
            <Alert tone="warning" title="Проверка обновлений не удалась">
              {status.checkError}
            </Alert>
          ) : null}
          {update ? (
            <Alert
              tone="info"
              title={`Доступно обновление sing-box ${update.version}`}
              details={[
                `Тег: ${update.tag}`,
                `Файл: ${update.assetName}`,
                `Размер: ${formatBytes(update.size)}`,
                `Опубликовано: ${formatDateTime(update.publishedAt) || DASH}`,
              ]}
            />
          ) : (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Boxes className="h-4 w-4" />
              Новых версий компонента не найдено.
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FileText className="h-4 w-4" />
            Лицензия и документы
          </CardTitle>
          <CardDescription>
            Проект распространяется под GNU GPL v3; sing-box — отдельный сторонний компонент со
            своей лицензией.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ul className="space-y-1.5 text-sm">
            {SPEC_POINTERS.map((pointer) => (
              <li key={pointer.path} className="flex flex-wrap items-baseline gap-2">
                <span>{pointer.label}</span>
                <span className="font-mono text-xs text-muted-foreground">{pointer.path}</span>
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}
