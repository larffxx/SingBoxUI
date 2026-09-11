/**
 * Timestamp normalisation.
 *
 * The Wails binding generator cannot express `time.Time`, so every timestamp
 * that crosses the bridge is typed `any` in `wailsjs/go/models.ts`. Go marshals
 * `time.Time` as an RFC 3339 string, so this module is the single place that
 * turns that raw value into a `Date`. Nothing else in the frontend reads a
 * timestamp field directly.
 */

/** toDate converts a bridge timestamp (RFC 3339 string) into a Date. */
export function toDate(value: unknown): Date | undefined {
  if (typeof value === 'string') {
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? undefined : parsed
  }
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? undefined : value
  }
  return undefined
}

export function formatDateTime(value: unknown): string {
  const date = toDate(value)
  return date ? date.toLocaleString() : '—'
}

export function formatTime(value: unknown): string {
  const date = toDate(value)
  return date ? date.toLocaleTimeString() : '—'
}

/** formatDuration renders a duration in seconds as a compact human string. */
export function formatDuration(seconds: number | undefined): string {
  if (seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return '—'
  const total = Math.floor(seconds)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}
