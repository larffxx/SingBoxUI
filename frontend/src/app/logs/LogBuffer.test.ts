/**
 * Bounded log buffer tests (spec §46).
 *
 * `runtime:log` delivers batches that can overlap (a replayed tail after a
 * reconnect) or arrive out of order, so the buffer has to deduplicate by
 * sequence number and always keep the newest `LOG_BUFFER_CAP` lines.
 */
import { describe, expect, it } from 'vitest'

import type { runtime } from '@/shared/api/bindings'

import { LOG_BUFFER_CAP, accumulateDropped, appendLogRecords } from './logBuffer'

function record(seq: number, message = `строка ${seq}`): runtime.LogRecord {
  return {
    seq,
    time: '2026-09-11T10:00:00Z',
    source: 'sing-box',
    level: 'INFO',
    message,
  } as unknown as runtime.LogRecord
}

describe('appendLogRecords', () => {
  it('appends a fresh batch in order', () => {
    const merged = appendLogRecords([record(1)], [record(2), record(3)])
    expect(merged.map((entry) => entry.seq)).toEqual([1, 2, 3])
  })

  it('drops records whose sequence number was already buffered', () => {
    const merged = appendLogRecords([record(1), record(2)], [record(2), record(3)])
    expect(merged.map((entry) => entry.seq)).toEqual([1, 2, 3])
  })

  it('re-sorts a batch that arrives out of order', () => {
    const merged = appendLogRecords([record(5)], [record(4), record(6)])
    expect(merged.map((entry) => entry.seq)).toEqual([4, 5, 6])
  })

  it('caps the buffer and keeps the newest lines', () => {
    const existing = Array.from({ length: LOG_BUFFER_CAP }, (_, index) => record(index + 1))
    const merged = appendLogRecords(existing, [record(LOG_BUFFER_CAP + 1)])

    expect(merged).toHaveLength(LOG_BUFFER_CAP)
    expect(merged[0]?.seq).toBe(2)
    expect(merged[merged.length - 1]?.seq).toBe(LOG_BUFFER_CAP + 1)
  })

  it('trims an over-long existing buffer even when the batch is empty', () => {
    const existing = Array.from({ length: LOG_BUFFER_CAP + 10 }, (_, index) => record(index + 1))
    const merged = appendLogRecords(existing, [])

    expect(merged).toHaveLength(LOG_BUFFER_CAP)
    expect(merged[0]?.seq).toBe(11)
  })
})

describe('accumulateDropped', () => {
  it('adds only positive deltas', () => {
    expect(accumulateDropped(3, 4)).toBe(7)
    expect(accumulateDropped(3, 0)).toBe(3)
    expect(accumulateDropped(3, -1)).toBe(3)
  })
})
