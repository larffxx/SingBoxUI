/**
 * Bounded log buffer (spec §46).
 *
 * `runtime:log` delivers batches, not single lines, so the shell owns one
 * bounded, deduplicated buffer that the runtime screen reads. Keeping the buffer
 * above the screen means switching routes never loses the stream, and the cap
 * keeps a chatty listener from growing memory without bound. The pure merge
 * helpers and the context live here so `LogBufferProvider.tsx` exports only the
 * component and the helpers stay directly unit-testable.
 */
import { createContext, useContext } from 'react'

import type { runtime } from '@/shared/api/bindings'

/** Hard ceiling on buffered records; the oldest are dropped first. */
export const LOG_BUFFER_CAP = 2_000

export interface LogBufferValue {
  records: runtime.LogRecord[]
  /** Records the backend evicted before they could be delivered. */
  dropped: number
  append: (records: runtime.LogRecord[], dropped?: number) => void
  replace: (records: runtime.LogRecord[]) => void
  clear: () => void
}

/**
 * Merges `incoming` into `existing`, dropping duplicate sequence numbers and
 * keeping only the newest `cap` records. Pure so it can be unit-tested directly.
 */
export function appendLogRecords(
  existing: runtime.LogRecord[],
  incoming: runtime.LogRecord[],
  cap: number = LOG_BUFFER_CAP,
): runtime.LogRecord[] {
  if (incoming.length === 0)
    return existing.length > cap ? existing.slice(existing.length - cap) : existing

  const seen = new Set<number>()
  for (const record of existing) seen.add(record.seq)

  const last = existing.length > 0 ? existing[existing.length - 1] : undefined
  const firstIncoming = incoming[0]
  const ordered = last === undefined || firstIncoming === undefined || firstIncoming.seq > last.seq

  const merged = [...existing]
  for (const record of incoming) {
    if (seen.has(record.seq)) continue
    seen.add(record.seq)
    merged.push(record)
  }
  if (!ordered) merged.sort((left, right) => left.seq - right.seq)

  return merged.length > cap ? merged.slice(merged.length - cap) : merged
}

/** Newest records the backend evicted since the previous batch. */
export function accumulateDropped(current: number, delta: number): number {
  return delta > 0 ? current + delta : current
}

export const LogBufferContext = createContext<LogBufferValue | undefined>(undefined)

export function useLogBuffer(): LogBufferValue {
  const value = useContext(LogBufferContext)
  if (!value) throw new Error('useLogBuffer must be used inside a LogBufferProvider')
  return value
}
