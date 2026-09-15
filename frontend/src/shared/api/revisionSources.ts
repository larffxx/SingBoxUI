/**
 * Revision sources (spec §12).
 *
 * Wails generates `profile.Source` as a plain `string`, so nothing in the type
 * system stops the UI from sending a value the backend does not know: the save
 * simply fails with `INVALID_ARGUMENT unknown revision source "…"`. The list
 * below is the list the spec enumerates, and `internal/domain/profile` compares
 * the two in a test so they cannot drift apart.
 */
export const REVISION_SOURCES = [
  'manual',
  'import',
  'share-import',
  'template',
  'rollback',
  'migration',
] as const

export type RevisionSource = (typeof REVISION_SOURCES)[number]

/** A revision written by hand in the editor (spec §13). */
export const REVISION_SOURCE_MANUAL: RevisionSource = 'manual'

/** What the history list shows for each source. */
export const REVISION_SOURCE_LABELS: Record<RevisionSource, string> = {
  manual: 'вручную',
  import: 'импорт',
  'share-import': 'импорт ссылки',
  template: 'шаблон',
  rollback: 'откат',
  migration: 'миграция',
}

/** Narrows an arbitrary backend string to a source the backend accepts. */
export function isRevisionSource(value: string): value is RevisionSource {
  return (REVISION_SOURCES as readonly string[]).includes(value)
}

/** Labels a source for display and falls back to the raw value. */
export function revisionSourceLabel(value: string): string {
  return isRevisionSource(value) ? REVISION_SOURCE_LABELS[value] : value
}
