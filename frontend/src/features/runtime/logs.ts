/**
 * Log level model for the runtime log viewer.
 *
 * The backend emits sing-box log lines through `runtime:log` batches (spec §46)
 * and answers `RuntimeAPI.GetLogs` with `runtime.LogRecord` values. Everything
 * level-related — ordering, the filter, the "and above" semantics — lives here so
 * the viewer stays presentation-only and the logic stays unit-testable.
 *
 * The app's own log level (`settings.LogLevel`: debug/info/warn/error) is a
 * different scale and lives in `@/features/settings/schema`.
 */
import type { runtime } from '@/shared/api/bindings'
import { toDate } from '@/shared/lib/time'

/** sing-box log levels, quietest first. */
export const LOG_LEVELS = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'] as const

export type LogLevel = (typeof LOG_LEVELS)[number]

/** `ALL` disables level filtering. */
export type LogLevelFilter = 'ALL' | LogLevel

export interface LogLevelOption {
  value: LogLevelFilter
  label: string
}

/** Filter options for the level select; every entry means "this level and above". */
export const LOG_LEVEL_FILTERS: readonly LogLevelOption[] = [
  { value: 'ALL', label: 'Все уровни' },
  { value: 'TRACE', label: 'TRACE и выше' },
  { value: 'DEBUG', label: 'DEBUG и выше' },
  { value: 'INFO', label: 'INFO и выше' },
  { value: 'WARN', label: 'WARN и выше' },
  { value: 'ERROR', label: 'ERROR и выше' },
  { value: 'FATAL', label: 'Только FATAL' },
]

const RANK: Readonly<Record<LogLevel, number>> = {
  TRACE: 0,
  DEBUG: 1,
  INFO: 2,
  WARN: 3,
  ERROR: 4,
  FATAL: 5,
}

/**
 * normaliseLevel maps a backend level string onto the known scale.
 * sing-box writes `warning`, so it is folded onto `WARN`; anything unknown is
 * treated as `INFO` rather than dropped from view.
 */
export function normaliseLevel(level: string | undefined): LogLevel {
  const upper = (level ?? '').trim().toUpperCase()
  if (upper === 'WARNING') return 'WARN'
  return (LOG_LEVELS as readonly string[]).includes(upper) ? (upper as LogLevel) : 'INFO'
}

/** levelRank orders levels; higher is more severe. */
export function levelRank(level: string | undefined): number {
  return RANK[normaliseLevel(level)]
}

/** matchesLevelFilter implements the "and above" filter semantics. */
export function matchesLevelFilter(level: string | undefined, filter: LogLevelFilter): boolean {
  if (filter === 'ALL') return true
  return levelRank(level) >= RANK[filter]
}

/** The free-text half of the viewer filter. */
export function matchesSearch(record: runtime.LogRecord, search: string): boolean {
  const needle = search.trim().toLowerCase()
  if (needle === '') return true
  const haystack = `${record.level} ${record.source} ${record.message}`.toLowerCase()
  return haystack.includes(needle)
}

export interface LogFilter {
  level: LogLevelFilter
  search: string
}

/** filterLogs applies the level and search filters, preserving order. */
export function filterLogs(
  records: readonly runtime.LogRecord[],
  filter: LogFilter,
): runtime.LogRecord[] {
  return records.filter(
    (record) =>
      matchesLevelFilter(record.level, filter.level) && matchesSearch(record, filter.search),
  )
}

function pad(value: number, size = 2): string {
  return String(value).padStart(size, '0')
}

/** formatLogTime renders a bridge timestamp as `HH:MM:SS.mmm` in local time. */
export function formatLogTime(value: unknown): string {
  const date = toDate(value)
  if (!date) return '—'
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(
    date.getMilliseconds(),
    3,
  )}`
}

/** formatLogLine renders one record as a single line for the plain-text view. */
export function formatLogLine(record: runtime.LogRecord): string {
  const source = record.source ? `${record.source}: ` : ''
  return `${formatLogTime(record.time)} [${normaliseLevel(record.level)}] ${source}${record.message}`
}

export type LogFormat = 'text' | 'json'

export const LOG_FORMATS: readonly { value: LogFormat; label: string }[] = [
  { value: 'text', label: 'Текст' },
  { value: 'json', label: 'JSONL' },
]

/** toClipboardText serialises the visible records in the requested format. */
export function toClipboardText(records: readonly runtime.LogRecord[], format: LogFormat): string {
  if (format === 'json') {
    return records
      .map((record) =>
        JSON.stringify({
          seq: record.seq,
          time: record.time,
          level: normaliseLevel(record.level),
          source: record.source,
          message: record.message,
        }),
      )
      .join('\n')
  }
  return records.map(formatLogLine).join('\n')
}

export type Tone = 'neutral' | 'info' | 'success' | 'warning' | 'danger'

/** logLevelTone maps a level onto a badge/alert tone. */
export function logLevelTone(level: string | undefined): Tone {
  switch (normaliseLevel(level)) {
    case 'FATAL':
    case 'ERROR':
      return 'danger'
    case 'WARN':
      return 'warning'
    case 'DEBUG':
    case 'TRACE':
      return 'neutral'
    default:
      return 'info'
  }
}

/** How many lines the viewer keeps in the DOM before it is cut off. */
export const LOG_VIEWER_RENDER_CAP = 2000
