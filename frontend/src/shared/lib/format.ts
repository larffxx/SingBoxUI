/** Presentation helpers shared by every screen. */

const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const

/** formatBytes renders a byte count with binary units. */
export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return '—'
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = unit === 0 ? 0 : value < 10 ? 2 : 1
  return `${value.toFixed(digits)} ${BYTE_UNITS[unit] ?? 'B'}`
}

/** formatRate renders a per-second byte rate. */
export function formatRate(bytesPerSecond: number | undefined): string {
  if (bytesPerSecond === undefined || !Number.isFinite(bytesPerSecond)) return '—'
  return `${formatBytes(bytesPerSecond)}/s`
}

/** truncate shortens a string for dense tables. */
export function truncate(value: string, max = 48): string {
  return value.length <= max ? value : `${value.slice(0, max - 1)}…`
}

/** titleCase renders an enum-ish backend value for humans. */
export function titleCase(value: string): string {
  return value
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
    .trim()
}
