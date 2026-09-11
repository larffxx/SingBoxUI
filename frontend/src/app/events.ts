/**
 * Backend event contract (spec §46, §47).
 *
 * All payload normalisation lives here: the Wails bridge hands the callback raw
 * JSON, and every handler converts it into a domain-shaped value before it
 * touches the query cache. Keeping this in one module means the event names,
 * the cache keys they map to and the defensive parsing can be reviewed (and
 * tested) together.
 */
import type { QueryClient } from '@tanstack/react-query'

import type { desktop, runtime, traffic } from '@/shared/api/bindings'
import { keys } from '@/shared/api/keys'

/** Event names mirrored from internal/events/events.go — they must stay stable. */
export const APP_EVENTS = {
  runtimeStatus: 'runtime:status',
  runtimeLog: 'runtime:log',
  trafficSnapshot: 'traffic:snapshot',
  binaryProgress: 'binary:progress',
  configApply: 'config:apply',
  appNotice: 'app:notice',
  profilesChanged: 'profiles:changed',
} as const

export type AppEventName = (typeof APP_EVENTS)[keyof typeof APP_EVENTS]

/** Payload of `app:notice` (internal/events.Notice). */
export interface NoticePayload {
  level: string
  title: string
  message: string
  code?: string | undefined
  operation?: string | undefined
  details: string[]
  persistent: boolean
  occurredAt?: string | undefined
}

/** Payload of `binary:progress` (internal/app/binary `map[string]any`). */
export interface BinaryProgressPayload {
  stage: string
  version: string
  percent: number
}

/** Payload of `config:apply` (internal/app/config.Progress). */
export interface ConfigApplyProgressPayload {
  stage: string
  message: string
  percent: number
  occurredAt?: unknown
}

/** Payload of `runtime:log` (internal/app/runtime logBatch). */
export interface LogBatchPayload {
  revisionId: string
  records: runtime.LogRecord[]
  dropped: number
}

/** Cache key for the transient binary install progress (not a bound call). */
export const BINARY_PROGRESS_KEY = ['binary-progress'] as const

/**
 * Cache key for the last `config:apply` stage. The profile workspace renders the
 * apply progress; the shell only keeps the newest stage in the cache so the
 * screen can pick it up without its own subscription.
 */
export const CONFIG_APPLY_PROGRESS_KEY = ['config', 'apply-progress'] as const

/** Stage that marks the end of an apply run (internal/app/config progress stages). */
export const APPLY_DONE_STAGE = 'done'

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  return value as Record<string, unknown>
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function asNumber(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((entry): entry is string => typeof entry === 'string')
}

/**
 * `runtime:status` carries a `runtime.Status`. The generated model is a class
 * with a `convertValues` helper, so the raw JSON object is structurally the
 * right shape but never an instance — the cast is the single place where an
 * untyped event payload becomes a typed value.
 */
export function asRuntimeStatus(value: unknown): runtime.Status | undefined {
  const record = asRecord(value)
  if (!record || typeof record.state !== 'string') return undefined
  return record as unknown as runtime.Status
}

/** `traffic:snapshot` carries a `traffic.Snapshot`. */
export function asTrafficSnapshot(value: unknown): traffic.Snapshot | undefined {
  const record = asRecord(value)
  if (!record || typeof record.uploadTotal !== 'number') return undefined
  return record as unknown as traffic.Snapshot
}

/** `app:notice` carries an internal/events.Notice. */
export function asNotice(value: unknown): NoticePayload | undefined {
  const record = asRecord(value)
  if (!record || typeof record.title !== 'string') return undefined
  const code = asString(record.code)
  const operation = asString(record.operation)
  const occurredAt = asString(record.occurredAt)
  return {
    level: asString(record.level) || 'info',
    title: record.title,
    message: asString(record.message),
    ...(code ? { code } : {}),
    ...(operation ? { operation } : {}),
    details: asStringArray(record.details),
    persistent: record.persistent === true,
    ...(occurredAt ? { occurredAt } : {}),
  }
}

/**
 * `runtime:log` always delivers a batch (`{revisionId, records, dropped}`). A
 * bare array is accepted as well so a handler keeps working if the payload ever
 * degrades to a single record list.
 */
export function parseLogBatch(value: unknown): LogBatchPayload {
  const record = asRecord(value)
  const rawRecords = Array.isArray(value) ? value : record ? record.records : undefined
  const records: runtime.LogRecord[] = []
  if (Array.isArray(rawRecords)) {
    for (const entry of rawRecords) {
      const item = asRecord(entry)
      if (!item || typeof item.seq !== 'number' || typeof item.level !== 'string') continue
      records.push(item as unknown as runtime.LogRecord)
    }
  }
  return {
    revisionId: record ? asString(record.revisionId) : '',
    records,
    dropped: record ? asNumber(record.dropped) : 0,
  }
}

export function asBinaryProgress(value: unknown): BinaryProgressPayload | undefined {
  const record = asRecord(value)
  if (!record || typeof record.stage !== 'string') return undefined
  return {
    stage: record.stage,
    version: asString(record.version),
    percent: asNumber(record.percent),
  }
}

export function asConfigApplyProgress(value: unknown): ConfigApplyProgressPayload | undefined {
  const record = asRecord(value)
  if (!record || typeof record.stage !== 'string') return undefined
  const occurredAt = record.occurredAt
  return {
    stage: record.stage,
    message: asString(record.message),
    percent: asNumber(record.percent),
    ...(typeof occurredAt === 'string' ? { occurredAt } : {}),
  }
}

/**
 * Merges a status pushed by the backend into the cached runtime payload. The
 * cache — not component state — is the single source of truth for runtime
 * state, so the shell, the status bar and the runtime screen all update from the
 * same event.
 */
export function applyRuntimeStatus(client: QueryClient, status: runtime.Status): void {
  const key = keys.runtime.status()
  const previous = client.getQueryData<desktop.RuntimePayload>(key)
  const payload = previous
    ? {
        ...previous,
        status,
        binaryVersion: status.binaryVersion || previous.binaryVersion,
      }
    : {
        status,
        activeProfileName: '',
        binaryVersion: status.binaryVersion,
        trafficAvailable: false,
        shuttingDown: false,
      }
  client.setQueryData(key, payload as desktop.RuntimePayload)
}

/** Merges a traffic snapshot into the cached traffic payload. */
export function applyTrafficSnapshot(client: QueryClient, snapshot: traffic.Snapshot): void {
  const key = keys.traffic.snapshot()
  const previous = client.getQueryData<desktop.TrafficPayload>(key)
  const payload = previous
    ? { ...previous, snapshot, collectorRunning: snapshot.available }
    : { snapshot, collectorRunning: snapshot.available }
  client.setQueryData(key, payload as desktop.TrafficPayload)
}

/** Stores install progress so the binary screen can render it without polling. */
export function applyBinaryProgress(client: QueryClient, progress: BinaryProgressPayload): void {
  client.setQueryData<BinaryProgressPayload>(BINARY_PROGRESS_KEY, progress)
  if (progress.stage === 'done') {
    void client.invalidateQueries({ queryKey: keys.binary.all })
  }
}

/** `profiles:changed` has no payload: the list and everything derived is stale. */
export function handleProfilesChanged(client: QueryClient): void {
  void client.invalidateQueries({ queryKey: keys.profiles.all })
  void client.invalidateQueries({ queryKey: keys.config.all })
}

/** Stores the newest apply stage; the final stage invalidates what it rewrote. */
export function applyConfigApplyProgress(
  client: QueryClient,
  progress: ConfigApplyProgressPayload,
): void {
  client.setQueryData<ConfigApplyProgressPayload>(CONFIG_APPLY_PROGRESS_KEY, progress)
  if (progress.stage === APPLY_DONE_STAGE) {
    void client.invalidateQueries({ queryKey: keys.config.all })
    void client.invalidateQueries({ queryKey: keys.runtime.all })
    void client.invalidateQueries({ queryKey: keys.profiles.all })
  }
}

/** A restart triggered outside a bound call leaves the runtime state stale. */
export function handleRuntimeStatusEvent(client: QueryClient, payload: unknown): boolean {
  const status = asRuntimeStatus(payload)
  if (!status) return false
  applyRuntimeStatus(client, status)
  return true
}
