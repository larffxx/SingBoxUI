/**
 * Immutable helpers for the sing-box configuration document.
 *
 * The workspace keeps exactly one source of truth — the raw JSON text the user
 * will persist — and every structured editor produces a new document from it.
 * These helpers never mutate their input and never re-encode numbers as
 * strings, so unknown sing-box options survive a round-trip (spec §36).
 */

export type JsonObject = Record<string, unknown>

export function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** parseDoc returns the root object or a human-readable reason it is unusable. */
export function parseDoc(text: string): { doc: JsonObject } | { error: string } {
  try {
    const parsed: unknown = JSON.parse(text)
    if (!isObject(parsed)) {
      return { error: 'Корнем конфигурации должен быть JSON-объект.' }
    }
    return { doc: parsed }
  } catch (error) {
    return { error: error instanceof Error ? error.message : 'Некорректный JSON.' }
  }
}

export function stringifyDoc(doc: JsonObject): string {
  return `${JSON.stringify(doc, null, 2)}\n`
}

export function cloneDoc(doc: JsonObject): JsonObject {
  return JSON.parse(JSON.stringify(doc)) as JsonObject
}

export function getObject(parent: JsonObject, key: string): JsonObject {
  const value = parent[key]
  return isObject(value) ? value : {}
}

export function getArray(parent: JsonObject, key: string): JsonObject[] {
  const value = parent[key]
  if (!Array.isArray(value)) return []
  return value.filter(isObject)
}

export function getString(parent: JsonObject, key: string): string {
  const value = parent[key]
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

export function getNumber(parent: JsonObject, key: string): number | undefined {
  const value = parent[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : undefined
  }
  return undefined
}

export function getBoolean(parent: JsonObject, key: string): boolean {
  return parent[key] === true
}

export function getStringList(parent: JsonObject, key: string): string[] {
  const value = parent[key]
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string')
}

/** setValue sets a key, or removes it when the value is empty. */
export function setValue(parent: JsonObject, key: string, value: unknown): JsonObject {
  const next: JsonObject = { ...parent }
  if (value === undefined || value === null || value === '') {
    delete next[key]
  } else {
    next[key] = value
  }
  return next
}

/** setObject drops a nested object once it has no keys left. */
export function setObject(parent: JsonObject, key: string, value: JsonObject): JsonObject {
  const next: JsonObject = { ...parent }
  if (Object.keys(value).length === 0) {
    delete next[key]
  } else {
    next[key] = value
  }
  return next
}

export function setList(parent: JsonObject, key: string, value: string[]): JsonObject {
  const cleaned = value.map((item) => item.trim()).filter((item) => item !== '')
  return setValue(parent, key, cleaned.length === 0 ? undefined : cleaned)
}

/** pairsToList renders a JSON object as `Ключ: значение` lines. */
export function pairsToList(value: unknown): string[] {
  if (!isObject(value)) return []
  return Object.entries(value).map(([key, item]) => {
    const rendered = typeof item === 'string' ? item : JSON.stringify(item)
    return `${key}: ${rendered ?? ''}`
  })
}

/** listToPairs rebuilds the object from `Ключ: значение` lines. */
export function listToPairs(lines: string[]): JsonObject {
  const out: JsonObject = {}
  for (const line of lines) {
    const index = line.indexOf(':')
    if (index <= 0) continue
    const key = line.slice(0, index).trim()
    const value = line.slice(index + 1).trim()
    if (key === '') continue
    out[key] = value
  }
  return out
}

/** splitLines is the editor representation of a string list. */
export function splitLines(value: unknown): string[] {
  const items = Array.isArray(value) ? value : typeof value === 'string' ? [value] : []
  return items
    .filter((item): item is string => typeof item === 'string')
    .join('\n')
    .split('\n')
}

/** joinLines turns a textarea value into the array sing-box expects. */
export function joinLines(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}
