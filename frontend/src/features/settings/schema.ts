/**
 * Settings form schema (spec §50).
 *
 * The zod schema mirrors `settings.UpdateInput` 1:1 so the form rejects exactly
 * the values the backend rejects: an unknown theme / log level / binary source,
 * a custom binary that is not an absolute path, and a custom source with no path
 * at all. `validateSettings` is the single entry point used by the form resolver
 * and by unit tests.
 */
import { z } from 'zod'

export const THEME_VALUES = ['system', 'light', 'dark'] as const
export const LOG_LEVEL_VALUES = ['debug', 'info', 'warn', 'error'] as const
export const BINARY_SOURCE_VALUES = ['managed', 'custom'] as const

export type ThemeValue = (typeof THEME_VALUES)[number]
export type LogLevelValue = (typeof LOG_LEVEL_VALUES)[number]
export type BinarySourceValue = (typeof BINARY_SOURCE_VALUES)[number]

/** An absolute POSIX or Windows path — what the backend accepts for a binary. */
export function isAbsolutePath(value: string): boolean {
  return value.startsWith('/') || /^[a-zA-Z]:[\\/]/.test(value)
}

/**
 * Mirrors `settings.Settings` — the persisted preference document.
 *
 * Cross-field rules are part of the schema (not the submit handler) so the form
 * resolver, the settings API guard and the tests all agree on one definition.
 */
export const settingsValuesSchema = z
  .object({
    theme: z.enum(THEME_VALUES, { message: 'Выберите тему оформления' }),
    logLevel: z.enum(LOG_LEVEL_VALUES, { message: 'Выберите уровень журнала' }),
    binarySource: z.enum(BINARY_SOURCE_VALUES, { message: 'Выберите источник компонента' }),
    customBinaryPath: z.string().trim().max(1024, 'Путь слишком длинный'),
    lastProfileId: z.string().trim().max(256, 'Идентификатор слишком длинный'),
    autoStartApplication: z.boolean(),
    autoConnect: z.boolean(),
    managedStableChannel: z.boolean(),
    updateCheckEnabled: z.boolean(),
  })
  .superRefine((values, ctx) => {
    if (values.binarySource === 'custom' && values.customBinaryPath === '') {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['customBinaryPath'],
        message: 'Укажите путь к исполняемому файлу sing-box',
      })
      return
    }
    if (values.customBinaryPath !== '' && !isAbsolutePath(values.customBinaryPath)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['customBinaryPath'],
        message: 'Путь должен быть абсолютным (/usr/local/bin/sing-box)',
      })
    }
  })

export type SettingsFormValues = z.infer<typeof settingsValuesSchema>

/** The values the reset action restores (spec §50). */
export const DEFAULT_SETTINGS_VALUES: SettingsFormValues = {
  theme: 'system',
  logLevel: 'info',
  binarySource: 'managed',
  customBinaryPath: '',
  lastProfileId: '',
  autoStartApplication: false,
  autoConnect: false,
  managedStableChannel: true,
  updateCheckEnabled: true,
}

/**
 * Validates a whole document.
 *
 * Returns the issues keyed by field so callers can either feed them to a form
 * resolver or render them inline; `ok: true` means `settingsApi.update()` will
 * accept the document.
 */
export function validateSettings(
  values: SettingsFormValues,
): { ok: true } | { ok: false; issues: Partial<Record<keyof SettingsFormValues, string>> } {
  const parsed = settingsValuesSchema.safeParse(values)
  if (parsed.success) return { ok: true }
  const issues: Partial<Record<keyof SettingsFormValues, string>> = {}
  for (const issue of parsed.error.issues) {
    const key = issue.path[0]
    if (typeof key === 'string' && !(key in issues)) {
      issues[key as keyof SettingsFormValues] = issue.message
    }
  }
  return { ok: false, issues }
}
