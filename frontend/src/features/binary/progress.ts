/**
 * Binary install progress (spec §19, §57).
 *
 * The desktop layer emits `binary:progress` for every stage of an update; the
 * app-level bridge (`src/app/events.ts`) is the only subscriber and parks the
 * payload in the query cache. This module reads it back and normalises it,
 * because the payload shape depends on the stage:
 *
 *   check    -> { stage, current, available, checkedAt }
 *   download -> { stage, version, percent }
 *   extract  -> { stage, version }
 *   verify   -> { stage, version }
 *   install  -> { stage, version }
 *   done     -> { stage, version, percent: 100 }
 *
 * Byte counters are not published by the backend (the download callback knows
 * them, the event does not carry them), so only `percent` is authoritative here
 * and the archive size is shown from the release metadata instead.
 */
import { BINARY_PROGRESS_KEY } from '@/app/events'

import { useCachedEvent } from '../profiles/progress'

export interface BinaryProgressView {
  stage: string
  /** 0–100; 0 for stages that do not report progress. */
  percent: number
  version: string
  /** Version currently installed, present on the `check` stage. */
  current: string
  /** Version found in the release channel, present on the `check` stage. */
  available: string
}

const STAGE_LABELS: Record<string, string> = {
  check: 'Проверяем релизы',
  download: 'Скачиваем архив',
  extract: 'Распаковываем архив',
  verify: 'Проверяем скачанный бинарник',
  install: 'Устанавливаем',
  done: 'Готово',
}

export function binaryStageLabel(stage: string): string {
  return STAGE_LABELS[stage] ?? stage
}

/** Stages after which nothing is left running in the background. */
export function isBinaryStageTerminal(stage: string): boolean {
  return stage === 'done'
}

export function useBinaryProgress(): BinaryProgressView | undefined {
  const raw = useCachedEvent<unknown>(BINARY_PROGRESS_KEY)
  if (raw === null || raw === undefined || typeof raw !== 'object') return undefined
  const record = raw as Record<string, unknown>
  const stage = typeof record.stage === 'string' ? record.stage : ''
  if (stage === '') return undefined
  return {
    stage,
    percent: typeof record.percent === 'number' ? record.percent : 0,
    version: typeof record.version === 'string' ? record.version : '',
    current: typeof record.current === 'string' ? record.current : '',
    available: typeof record.available === 'string' ? record.available : '',
  }
}
