/**
 * Settings screen (spec §50).
 *
 * Every field of `settings.UpdateInput` is editable here, plus the three things
 * that are not part of the preference document: the OS autostart entry (an action
 * of its own, with the legacy-entry cleanup), the resolved environment/paths
 * block (read-only) and the theme switch, which applies immediately because the
 * `dark` class on <html> is what the user sees while editing.
 *
 * Validation is zod-backed through React Hook Form, so the form refuses exactly
 * the documents `settingsApi.update()` refuses. The backend stays authoritative:
 * a rejected update is rendered from `messageFor()` (code + message + details)
 * rather than a raw error object.
 */
import { useForm, type FieldErrors, type Resolver } from 'react-hook-form'
import { RotateCcw, Save, ShieldAlert, Trash2 } from 'lucide-react'
import * as React from 'react'

import { messageFor } from '@/app/errors/messageFor'
import {
  backendAvailable,
  useEnvironmentQuery,
  useProfilesQuery,
  useRemoveLegacyAutostartMutation,
  useSetAutostartMutation,
  useSettingsQuery,
  useUpdateSettingsMutation,
} from '@/app/queries'
import { THEME_OPTIONS, useTheme, type ThemePreference } from '@/app/theme/theme'
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
  Field,
  Input,
  KeyValueGrid,
  PageHeader,
  Select,
  SwitchField,
  type SelectOption,
} from '@/shared/ui'

import {
  BINARY_SOURCE_VALUES,
  DEFAULT_SETTINGS_VALUES,
  LOG_LEVEL_VALUES,
  THEME_VALUES,
  settingsValuesSchema,
  type SettingsFormValues,
} from './schema'

/**
 * Narrows one wire value onto a known literal, falling back to the default when
 * the backend sends something this build does not know yet (a newer enum member,
 * say). The backend stays authoritative for writes; this only keeps the form
 * renderable instead of crashing on an unknown value.
 */
function oneOf<T extends string>(allowed: readonly T[], value: unknown, fallback: T): T {
  return typeof value === 'string' && (allowed as readonly string[]).includes(value)
    ? (value as T)
    : fallback
}

/** The persisted document as the wire sends it (enum fields are plain strings). */
interface SettingsDocument {
  theme: string
  logLevel: string
  binarySource: string
  customBinaryPath: string
  lastProfileId: string
  autoStartApplication: boolean
  autoConnect: boolean
  managedStableChannel: boolean
  updateCheckEnabled: boolean
}

/** Maps a settings document into the values the form edits. */
function toFormValues(values: SettingsDocument): SettingsFormValues {
  return {
    theme: oneOf(THEME_VALUES, values.theme, 'system'),
    logLevel: oneOf(LOG_LEVEL_VALUES, values.logLevel, 'info'),
    binarySource: oneOf(BINARY_SOURCE_VALUES, values.binarySource, 'managed'),
    customBinaryPath: values.customBinaryPath ?? '',
    lastProfileId: values.lastProfileId ?? '',
    autoStartApplication: values.autoStartApplication,
    autoConnect: values.autoConnect,
    managedStableChannel: values.managedStableChannel,
    updateCheckEnabled: values.updateCheckEnabled,
  }
}

const LOG_LEVEL_OPTIONS: SelectOption[] = [
  { value: 'debug', label: 'Подробный (debug)' },
  { value: 'info', label: 'Обычный (info)' },
  { value: 'warn', label: 'Предупреждения (warn)' },
  { value: 'error', label: 'Только ошибки (error)' },
]

const LOG_LEVEL_LABELS: Record<string, string> = {
  debug: 'Подробный (debug)',
  info: 'Обычный (info)',
  warn: 'Предупреждения (warn)',
  error: 'Только ошибки (error)',
}

const BINARY_SOURCE_OPTIONS: SelectOption[] = [
  { value: BINARY_SOURCE_VALUES[0], label: 'Управляемый (скачивается приложением)' },
  { value: BINARY_SOURCE_VALUES[1], label: 'Свой путь к sing-box' },
]

/**
 * The zod schema is the single source of truth; the resolver only adapts its
 * issues to React Hook Form's shape.
 */
const settingsResolver: Resolver<SettingsFormValues> = (values) => {
  const parsed = settingsValuesSchema.safeParse(values)
  if (parsed.success) return { values: parsed.data, errors: {} }
  const errors: FieldErrors<SettingsFormValues> = {}
  for (const issue of parsed.error.issues) {
    const key = issue.path[0]
    if (typeof key !== 'string') continue
    const field = key as keyof SettingsFormValues
    if (errors[field]) continue
    errors[field] = { type: 'validation', message: issue.message }
  }
  return { values: {}, errors }
}

export function SettingsRoute(): React.ReactElement {
  const available = backendAvailable()
  const settings = useSettingsQuery()
  const environment = useEnvironmentQuery()
  const profiles = useProfilesQuery()
  const update = useUpdateSettingsMutation()
  const setAutostart = useSetAutostartMutation()
  const removeLegacyAutostart = useRemoveLegacyAutostartMutation()
  const { setTheme } = useTheme()

  const form = useForm<SettingsFormValues>({
    resolver: settingsResolver,
    defaultValues: DEFAULT_SETTINGS_VALUES,
    mode: 'onSubmit',
  })
  const [saved, setSaved] = React.useState(false)
  const [resetOpen, setResetOpen] = React.useState(false)

  const loaded = settings.data?.state.values
  const { reset } = form
  React.useEffect(() => {
    if (!loaded) return
    if (form.formState.isDirty) return
    reset(toFormValues(loaded))
  }, [loaded, form, reset])

  const values = form.watch()
  const source = values.binarySource
  const autostart = settings.data?.state.autostart
  const environment_ = environment.data?.environment
  const updateError = update.isError ? messageFor(update.error) : undefined

  const profileOptions: SelectOption[] = React.useMemo(
    () => [
      { value: '', label: 'Не выбран' },
      ...(profiles.data?.profiles ?? []).map((candidate) => ({
        value: candidate.id,
        label: candidate.name,
      })),
    ],
    [profiles.data],
  )

  const submit = React.useCallback(
    (next: SettingsFormValues): void => {
      update.mutate(next, {
        onSuccess: () => {
          setSaved(true)
          reset(next)
        },
      })
    },
    [update, reset],
  )

  const onInvalid = React.useCallback((): void => {
    setSaved(false)
  }, [])

  const applyTheme = React.useCallback(
    (next: string): void => {
      const preference: ThemePreference = next === 'light' || next === 'dark' ? next : 'system'
      form.setValue('theme', preference, { shouldDirty: true })
      setTheme(preference)
    },
    [form, setTheme],
  )

  const confirmReset = React.useCallback((): void => {
    update.mutate(DEFAULT_SETTINGS_VALUES, {
      onSuccess: () => {
        setSaved(true)
        reset(DEFAULT_SETTINGS_VALUES)
        setResetOpen(false)
      },
    })
  }, [update, reset])

  const themeOptions: SelectOption[] = THEME_OPTIONS.map((option) => ({
    value: option.value,
    label: option.label,
  }))

  return (
    <div className="space-y-4">
      <PageHeader
        title="Настройки"
        description="Применяются после сохранения; тема переключается сразу."
        actions={
          <Button
            type="submit"
            form="settings-form"
            loading={update.isPending}
            disabled={!available || update.isPending}
          >
            <Save className="h-4 w-4" />
            Сохранить
          </Button>
        }
      />

      {!available ? (
        <Alert tone="warning" title="Бэкенд недоступен">
          Настройки можно просмотреть, но не изменить: приложение открыто вне среды SingBoxUI.
        </Alert>
      ) : null}

      {settings.isError ? (
        <Alert tone="danger" title="Не удалось загрузить настройки">
          Повторите попытку позже — приложение использует значения по умолчанию.
        </Alert>
      ) : null}

      {updateError ? (
        <Alert
          tone="danger"
          title="Настройки не сохранены"
          code={updateError.code}
          details={updateError.details}
        >
          {updateError.message}
        </Alert>
      ) : null}

      {saved && !update.isPending ? (
        <Alert tone="success" title="Настройки сохранены">
          Изменения применены и записаны в файл настроек.
        </Alert>
      ) : null}

      <form
        id="settings-form"
        className="space-y-4"
        onSubmit={(event) => {
          setSaved(false)
          void form.handleSubmit(submit, onInvalid)(event)
        }}
      >
        <Card>
          <CardHeader>
            <CardTitle>Оформление и журнал</CardTitle>
            <CardDescription>Тема приложения и подробность журнала рантайма.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            <Field label="Тема" htmlFor="settings-theme">
              <Select
                id="settings-theme"
                value={values.theme}
                onValueChange={applyTheme}
                options={themeOptions}
                disabled={!available}
              />
            </Field>
            <Field
              label="Уровень журнала"
              htmlFor="settings-log-level"
              hint="Определяет, какие записи sing-box сохраняются и показываются."
            >
              <Select
                id="settings-log-level"
                value={values.logLevel}
                onValueChange={(next) =>
                  form.setValue('logLevel', next as SettingsFormValues['logLevel'], {
                    shouldDirty: true,
                  })
                }
                options={LOG_LEVEL_OPTIONS}
                disabled={!available}
              />
            </Field>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Компонент sing-box</CardTitle>
            <CardDescription>
              Откуда берётся исполняемый файл и следят ли за обновлениями.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Field label="Источник" htmlFor="settings-binary-source">
              <Select
                id="settings-binary-source"
                value={source}
                onValueChange={(next) =>
                  form.setValue('binarySource', next as SettingsFormValues['binarySource'], {
                    shouldDirty: true,
                  })
                }
                options={BINARY_SOURCE_OPTIONS}
                disabled={!available}
              />
            </Field>
            <Field
              label="Путь к исполняемому файлу"
              htmlFor="settings-custom-path"
              hint="Используется только при источнике «свой путь»."
              error={form.formState.errors.customBinaryPath?.message}
            >
              <Input
                id="settings-custom-path"
                placeholder="/usr/local/bin/sing-box"
                disabled={!available}
                {...form.register('customBinaryPath')}
              />
            </Field>
            <SwitchField
              id="settings-managed-stable"
              label="Стабильный канал обновлений"
              description="Не переключаться на предварительные версии sing-box."
              checked={values.managedStableChannel}
              onCheckedChange={(checked) =>
                form.setValue('managedStableChannel', checked, { shouldDirty: true })
              }
              disabled={!available}
            />
            <SwitchField
              id="settings-update-check"
              label="Проверять обновления sing-box"
              description="Периодическая проверка новых версий компонента."
              checked={values.updateCheckEnabled}
              onCheckedChange={(checked) =>
                form.setValue('updateCheckEnabled', checked, { shouldDirty: true })
              }
              disabled={!available}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Подключение и запуск</CardTitle>
            <CardDescription>Что делать при старте приложения.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <SwitchField
              id="settings-autostart-application"
              label="Запускать приложение при входе"
              description="SingBoxUI открывается автоматически вместе с системой."
              checked={values.autoStartApplication}
              onCheckedChange={(checked) =>
                form.setValue('autoStartApplication', checked, { shouldDirty: true })
              }
              disabled={!available}
            />
            <SwitchField
              id="settings-auto-connect"
              label="Подключаться при запуске"
              description="Поднимать соединение последнего активного профиля."
              checked={values.autoConnect}
              onCheckedChange={(checked) =>
                form.setValue('autoConnect', checked, { shouldDirty: true })
              }
              disabled={!available}
            />
            <Field
              label="Профиль по умолчанию"
              htmlFor="settings-last-profile"
              hint="Профиль, который открывается и подключается первым."
            >
              <Select
                id="settings-last-profile"
                value={values.lastProfileId}
                onValueChange={(next) =>
                  form.setValue('lastProfileId', next, { shouldDirty: true })
                }
                options={profileOptions}
                disabled={!available}
              />
            </Field>
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: 'Уровень журнала',
                  value: LOG_LEVEL_LABELS[values.logLevel] ?? values.logLevel,
                },
                { label: 'Тема', value: values.theme },
                { label: 'Источник компонента', value: values.binarySource },
              ]}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Автозапуск в системе</CardTitle>
            <CardDescription>
              Отдельная от настроек приложения запись в системе: создаётся и удаляется сразу.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={autostart?.enabled === true ? 'success' : 'neutral'}>
                {autostart?.supported !== true
                  ? 'Не поддерживается'
                  : autostart.enabled
                    ? 'Включён'
                    : 'Выключен'}
              </Badge>
              {autostart?.legacyEntry ? (
                <Badge tone="warning">Найдена запись прежней версии</Badge>
              ) : null}
            </div>
            <SwitchField
              id="settings-autostart"
              label="Автозапуск sing-box при входе в систему"
              description="Системная служба поднимает соединение до открытия приложения."
              checked={autostart?.enabled === true}
              onCheckedChange={(checked) => setAutostart.mutate(checked)}
              disabled={!available || autostart?.supported !== true || setAutostart.isPending}
            />
            {autostart?.legacyEntry ? (
              <div className="space-y-2 rounded-md border border-border p-3">
                <p className="text-sm">
                  Запись прежней версии:{' '}
                  <span className="font-mono text-xs">{autostart.legacyEntry}</span>
                </p>
                <Button
                  type="button"
                  variant="outline"
                  loading={removeLegacyAutostart.isPending}
                  disabled={!available || removeLegacyAutostart.isPending}
                  onClick={() => removeLegacyAutostart.mutate()}
                >
                  <Trash2 className="h-4 w-4" />
                  Удалить запись прежней версии
                </Button>
              </div>
            ) : null}
            {autostart?.error ? (
              <Alert tone="danger" title="Автозапуск недоступен">
                {autostart.error}
              </Alert>
            ) : null}
            {setAutostart.isError || removeLegacyAutostart.isError ? (
              <Alert tone="danger" title="Не удалось изменить автозапуск">
                {messageFor(setAutostart.error ?? removeLegacyAutostart.error).message}
              </Alert>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Окружение и пути</CardTitle>
            <CardDescription>Только для чтения — значения определяет приложение.</CardDescription>
          </CardHeader>
          <CardContent>
            {environment.isError ? (
              <Alert tone="danger" title="Не удалось получить окружение">
                {messageFor(environment.error).message}
              </Alert>
            ) : (
              <KeyValueGrid
                columns={2}
                items={[
                  { label: 'Версия приложения', value: environment_?.version ?? '—' },
                  {
                    label: 'Платформа',
                    value: environment_ ? `${environment_.os}/${environment_.arch}` : '—',
                  },
                  { label: 'Исполняемый файл', value: environment_?.execPath ?? '—', mono: true },
                  { label: 'Каталог данных', value: environment_?.dataDir ?? '—', mono: true },
                  {
                    label: 'Каталог конфигураций',
                    value: environment_?.configDir ?? '—',
                    mono: true,
                  },
                  {
                    label: 'Активная конфигурация',
                    value: environment_?.activeConfigPath ?? '—',
                    mono: true,
                  },
                  {
                    label: 'Последняя рабочая конфигурация',
                    value: environment_?.lastGoodConfigPath ?? '—',
                    mono: true,
                  },
                  {
                    label: 'Журнал',
                    value: environment_?.logPath ?? settings.data?.state.logPath ?? '—',
                    mono: true,
                  },
                  { label: 'Каталог компонента', value: environment_?.binDir ?? '—', mono: true },
                  {
                    label: 'Рабочий каталог рантайма',
                    value: environment_?.runtimeDir ?? '—',
                    mono: true,
                  },
                  {
                    label: 'Каталог настроек',
                    value: settings.data?.state.dataDir ?? '—',
                    mono: true,
                  },
                  {
                    label: 'Путь к sing-box',
                    value: settings.data?.state.binaryPath ?? '—',
                    mono: true,
                  },
                ]}
              />
            )}
            {environment_?.supported === false ? (
              <Alert tone="warning" title="Платформа не поддерживается">
                {environment_.unsupportedReason ?? 'Часть возможностей может быть недоступна.'}
              </Alert>
            ) : null}
          </CardContent>
        </Card>
      </form>

      <Card>
        <CardHeader>
          <CardTitle>Сброс настроек</CardTitle>
          <CardDescription>
            Возвращает документ настроек к значениям по умолчанию: тема «как в системе», обычный
            журнал, подключение не выполняется автоматически.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            type="button"
            variant="outline"
            disabled={!available || update.isPending}
            onClick={() => setResetOpen(true)}
          >
            <RotateCcw className="h-4 w-4" />
            Сбросить настройки
          </Button>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title="Сбросить настройки?"
        description="Все изменения в настройках будут заменены значениями по умолчанию."
        confirmLabel="Сбросить"
        destructive
        pending={update.isPending}
        onConfirm={confirmReset}
      >
        <Alert tone="warning" title="Действие необратимо">
          <span className="inline-flex items-center gap-1">
            <ShieldAlert className="h-3.5 w-3.5" />
            Пути и запись автозапуска в системе не затрагиваются — сбрасывается только документ
            настроек приложения.
          </span>
        </Alert>
      </ConfirmDialog>
    </div>
  )
}
