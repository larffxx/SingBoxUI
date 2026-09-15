/**
 * Runtime state presentation.
 *
 * The states mirror internal/domain/runtime: STOPPED | STARTING | RUNNING |
 * STOPPING | FAILED. The shell, the status bar and the runtime screen all label
 * them through here so wording never drifts between screens.
 */

export type RuntimeStateTone = 'neutral' | 'success' | 'warning' | 'danger'

export const RUNTIME_STATES = ['STOPPED', 'STARTING', 'RUNNING', 'STOPPING', 'FAILED'] as const

export function runtimeStateLabel(state: string | undefined): string {
  switch ((state ?? '').toUpperCase()) {
    case 'RUNNING':
      return 'Запущен'
    case 'STARTING':
      return 'Запускается'
    case 'STOPPING':
      return 'Останавливается'
    case 'FAILED':
      return 'Ошибка'
    case 'STOPPED':
      return 'Остановлен'
    default:
      return 'Неизвестно'
  }
}

export function runtimeStateTone(state: string | undefined): RuntimeStateTone {
  switch ((state ?? '').toUpperCase()) {
    case 'RUNNING':
      return 'success'
    case 'STARTING':
    case 'STOPPING':
      return 'warning'
    case 'FAILED':
      return 'danger'
    default:
      return 'neutral'
  }
}

export function isRuntimeRunning(state: string | undefined): boolean {
  return (state ?? '').toUpperCase() === 'RUNNING'
}

export function isRuntimeBusy(state: string | undefined): boolean {
  const normalised = (state ?? '').toUpperCase()
  return normalised === 'STARTING' || normalised === 'STOPPING'
}

/** Human label for the last process exit code, if any. */
export function exitCodeLabel(code: number | undefined): string {
  if (code === undefined || code === null) return '—'
  return code === 0 ? '0 (успешно)' : String(code)
}
