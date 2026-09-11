/**
 * Mapping of backend validation output to Monaco markers (spec §34, §37).
 *
 * The backend reports structural problems from `domain/config` and semantic
 * problems from `sing-box check` as plain strings — they carry a location only
 * in some shapes (`outbounds[2]: missing tag`, `route.rules[0] …`, `at line 12`).
 * This module turns both lists into `JsonMarker`s with a best-effort line and
 * column, so the raw editor underlines the offending entry instead of dumping
 * free text.
 */
import type { config, singbox } from '@/shared/api/bindings'
import type { JsonMarker } from '@/shared/ui'

export interface ValidationIssue {
  message: string
  severity: 'error' | 'warning'
  line: number
  column: number
}

export interface ValidationSummary {
  issues: ValidationIssue[]
  markers: JsonMarker[]
  errorCount: number
  warningCount: number
  valid: boolean
  structuralOk: boolean
  checkOk: boolean
  checkOutput: string
  version: string
}

interface Position {
  line: number
  column: number
}

function positionAt(text: string, index: number): Position {
  const before = text.slice(0, index)
  const line = before.split('\n').length
  const lastBreak = before.lastIndexOf('\n')
  return { line, column: index - lastBreak }
}

/** findKeyLine locates the line of a `"key"` occurrence at or after `from`. */
function findKeyLine(text: string, key: string, from = 0): Position | undefined {
  const index = text.indexOf(`"${key}"`, from)
  return index < 0 ? undefined : positionAt(text, index)
}

function findKeyIndex(text: string, key: string, from = 0): number {
  return text.indexOf(`"${key}"`, from)
}

/**
 * findEntryLine walks to the `index`-th object of an array. `path` is the key
 * chain (for example `['route', 'rules']`).
 */
export function findEntryLine(text: string, path: string[], index: number): Position | undefined {
  let cursor = 0
  let lastKey: Position | undefined
  for (const key of path) {
    const keyIndex = findKeyIndex(text, key, cursor)
    if (keyIndex < 0) return lastKey
    cursor = keyIndex + key.length + 2
    lastKey = positionAt(text, keyIndex)
  }
  const open = text.indexOf('[', cursor)
  if (open < 0) return lastKey

  let depth = 0
  let entry = -1
  let inString = false
  let escaped = false
  for (let i = open; i < text.length; i += 1) {
    const char = text[i]
    if (inString) {
      if (escaped) escaped = false
      else if (char === '\\') escaped = true
      else if (char === '"') inString = false
      continue
    }
    if (char === '"') {
      inString = true
      continue
    }
    if (char === '[' || char === '{') {
      depth += 1
      if (char === '{' && depth === 2) {
        entry += 1
        if (entry === index) return positionAt(text, i)
      }
      continue
    }
    if (char === ']' || char === '}') {
      depth -= 1
      if (depth <= 0) break
    }
  }
  return lastKey
}

const LINE_PATTERN = /\bline\s+(\d+)(?:\s*[:.,]\s*(?:col(?:umn)?\s*)?(\d+))?/i
const COMPACT_PATTERN = /\((\d+):(\d+)\)/
const ARRAY_PATTERN = /^([a-zA-Z_]+)((?:\.?[a-zA-Z_]+)*)\[(\d+)\]/
const DOTTED_PATTERN = /^([a-zA-Z_]+)((?:\.?[a-zA-Z_]+)*)\b/
const TAG_PATTERN = /^(?:outbound|inbound|endpoint) "([^"]+)"/

/** locate resolves a single backend message to a position in the raw text. */
export function locate(text: string, message: string): Position {
  const line = LINE_PATTERN.exec(message)
  if (line?.[1]) {
    return { line: Number(line[1]), column: line[2] ? Number(line[2]) : 1 }
  }
  const compact = COMPACT_PATTERN.exec(message)
  if (compact?.[1] && compact[2]) {
    return { line: Number(compact[1]), column: Number(compact[2]) }
  }

  const array = ARRAY_PATTERN.exec(message)
  if (array?.[1] && array[3] !== undefined) {
    const segments = [array[1], ...(array[2] ?? '').split('.').filter((part) => part !== '')]
    const found = findEntryLine(text, segments, Number(array[3]))
    if (found) return found
    const fallback = findKeyLine(text, array[1])
    if (fallback) return fallback
  }

  const tag = TAG_PATTERN.exec(message)
  if (tag?.[1]) {
    const exact = text.indexOf(`"tag": "${tag[1]}"`)
    if (exact >= 0) return positionAt(text, exact)
    const bare = text.indexOf(`"${tag[1]}"`)
    if (bare >= 0) return positionAt(text, bare)
  }

  const dotted = DOTTED_PATTERN.exec(message)
  if (dotted?.[1]) {
    const segments = [dotted[1], ...(dotted[2] ?? '').split('.').filter((part) => part !== '')]
    const found = findEntryLine(text, segments, 0) ?? findKeyLine(text, dotted[1])
    if (found) return found
  }
  return { line: 1, column: 1 }
}

function toIssue(
  text: string,
  message: string,
  severity: ValidationIssue['severity'],
): ValidationIssue {
  const position = locate(text, message)
  return { message, severity, line: position.line, column: position.column }
}

/** summarize validates output into markers and an ordered issue list. */
export function summarize(
  text: string,
  structural: config.Result | undefined,
  check: singbox.CheckResult | undefined,
  valid: boolean,
): ValidationSummary {
  const issues: ValidationIssue[] = []
  for (const message of structural?.errors ?? []) issues.push(toIssue(text, message, 'error'))
  for (const message of structural?.warnings ?? []) issues.push(toIssue(text, message, 'warning'))
  for (const message of check?.errors ?? []) {
    if (message.trim() === '') continue
    issues.push(toIssue(text, message, 'error'))
  }

  const structuralOk = structural ? structural.ok : true
  const checkOk = check ? check.ok : true
  const errorCount = issues.filter((issue) => issue.severity === 'error').length

  return {
    issues,
    markers: issues.map((issue) => ({
      line: issue.line,
      column: issue.column,
      message: issue.message,
      severity: issue.severity,
    })),
    errorCount,
    warningCount: issues.length - errorCount,
    valid: valid && structuralOk && errorCount === 0,
    structuralOk,
    checkOk,
    checkOutput: check?.output ?? '',
    version: check?.version ?? '',
  }
}

export const EMPTY_SUMMARY: ValidationSummary = {
  issues: [],
  markers: [],
  errorCount: 0,
  warningCount: 0,
  valid: true,
  structuralOk: true,
  checkOk: true,
  checkOutput: '',
  version: '',
}
