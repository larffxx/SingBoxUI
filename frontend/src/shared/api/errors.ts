/**
 * Application error model (spec §33).
 *
 * Every bound call returns a payload whose optional `error` field is an
 * `apperr.Error`. The frontend switches on `code` — never on `message` — so
 * translations and user-facing behaviour stay stable when backend wording
 * changes.
 */

/** ErrorCode mirrors the stable codes defined by internal/domain/apperr. */
export const ERROR_CODES = [
  'CONFIG_INVALID',
  'CONFIG_CHECK_FAILED',
  'CONFIG_APPLY_FAILED',
  'PROFILE_NOT_FOUND',
  'REVISION_NOT_FOUND',
  'PROFILE_NAME_CONFLICT',
  'PROFILE_DELETE_BLOCKED',
  'RUNTIME_ALREADY_RUNNING',
  'RUNTIME_NOT_RUNNING',
  'RUNTIME_START_FAILED',
  'RUNTIME_STOP_FAILED',
  'RUNTIME_BUSY',
  'PRIVILEGE_DENIED',
  'BINARY_NOT_FOUND',
  'BINARY_DOWNLOAD_FAILED',
  'BINARY_CHECKSUM_FAILED',
  'BINARY_UNSUPPORTED_PLATFORM',
  'BINARY_INSTALL_FAILED',
  'BINARY_SOURCE_INVALID',
  'BINARY_UPDATE_CHECK_FAILED',
  'BINARY_UPDATE_UNAVAILABLE',
  'SETTINGS_INVALID',
  'SHARE_LINK_INVALID',
  'DATABASE_ERROR',
  'INVALID_ARGUMENT',
  'NOT_FOUND',
  'NETWORK_UNAVAILABLE',
  'UPDATING_RESTRICTED',
  'APP_SHUTTING_DOWN',
  'INTERNAL_ERROR',
] as const

export type ErrorCode = (typeof ERROR_CODES)[number] | (string & {})

/** AppError is the transport shape of apperr.Error. */
export interface AppError {
  code: ErrorCode
  message: string
  details?: string[] | undefined
  operation?: string | undefined
}

/** Raised when a bound call reports a failure through its payload. */
export class BoundCallError extends Error {
  readonly code: ErrorCode
  readonly details: string[]
  readonly operation: string | undefined

  constructor(error: AppError) {
    super(error.message)
    this.name = 'BoundCallError'
    this.code = error.code
    this.details = error.details ?? []
    this.operation = error.operation
  }
}

/** toAppError narrows an unknown rejection/throw value to AppError. */
export function toAppError(value: unknown): AppError {
  if (isAppError(value)) return value
  if (value instanceof Error) {
    return { code: 'INTERNAL_ERROR', message: value.message }
  }
  if (typeof value === 'string') {
    return { code: 'INTERNAL_ERROR', message: value }
  }
  return { code: 'INTERNAL_ERROR', message: 'Unexpected error' }
}

function isAppError(value: unknown): value is AppError {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as { code?: unknown; message?: unknown }
  return typeof candidate.code === 'string' && typeof candidate.message === 'string'
}

/**
 * Human-readable hint per code. These are hints, not contracts: screens may
 * override them, and any code missing from this table falls back to the
 * backend message.
 */
const HINTS: Partial<Record<ErrorCode, string>> = {
  CONFIG_INVALID: 'Конфигурация структурно некорректна. Проверьте JSON и обязательные поля.',
  CONFIG_CHECK_FAILED: 'sing-box check отклонил конфигурацию. Подробности — в выводе ниже.',
  CONFIG_APPLY_FAILED: 'Не удалось применить конфигурацию. Предыдущая конфигурация восстановлена.',
  PROFILE_NOT_FOUND: 'Профиль не найден — возможно, он был удалён.',
  PROFILE_NAME_CONFLICT: 'Профиль с таким именем уже существует.',
  PROFILE_DELETE_BLOCKED: 'Профиль используется запущенным рантаймом. Сначала остановите его.',
  RUNTIME_ALREADY_RUNNING: 'sing-box уже запущен.',
  RUNTIME_NOT_RUNNING: 'sing-box сейчас не запущен.',
  RUNTIME_START_FAILED: 'Не удалось запустить sing-box.',
  RUNTIME_STOP_FAILED: 'Не удалось остановить sing-box.',
  RUNTIME_BUSY: 'Другая операция с рантаймом ещё выполняется.',
  PRIVILEGE_DENIED: 'Для TUN нужны права администратора — запрос отклонён или отменён.',
  BINARY_NOT_FOUND: 'Исполняемый файл sing-box не найден.',
  BINARY_DOWNLOAD_FAILED: 'Не удалось скачать релиз sing-box.',
  BINARY_CHECKSUM_FAILED: 'Контрольная сумма скачанного архива не совпала — установка отменена.',
  BINARY_UNSUPPORTED_PLATFORM: 'Для этой платформы нет официального сборки sing-box.',
  BINARY_INSTALL_FAILED: 'Не удалось установить скачанный sing-box.',
  BINARY_UPDATE_CHECK_FAILED: 'Не удалось проверить обновления (нет сети или лимит GitHub).',
  SETTINGS_INVALID: 'Некорректные настройки.',
  SHARE_LINK_INVALID: 'Ссылка не распознана или содержит некорректные параметры.',
  DATABASE_ERROR: 'Ошибка базы данных приложения.',
  NETWORK_UNAVAILABLE: 'Сеть недоступна — работа с уже установленным sing-box продолжается.',
  UPDATING_RESTRICTED: 'Операция недоступна во время обновления.',
  APP_SHUTTING_DOWN: 'Приложение завершает работу.',
}

/** describeError produces the message shown to the user. */
export function describeError(error: AppError): string {
  return error.message || HINTS[error.code] || 'Неизвестная ошибка'
}

/** hintFor returns the contextual hint for a code, if one exists. */
export function hintFor(code: ErrorCode): string | undefined {
  return HINTS[code]
}

/** Payloads that carry an optional error field. */
export interface ErrorCarrier {
  error?: AppError | undefined
}

/**
 * unwrap returns the payload when it carries no error and throws a
 * BoundCallError otherwise. Use it in query functions so TanStack Query's error
 * path receives a real AppError.
 */
export function unwrap<T extends ErrorCarrier>(payload: T): T {
  if (payload.error) throw new BoundCallError(payload.error)
  return payload
}
