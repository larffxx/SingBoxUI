/**
 * Error presentation (spec §33).
 *
 * Screens never render a raw thrown value: everything the UI shows for a failed
 * bound call comes from here, so the stable `code` is always visible next to the
 * message and the backend `details` are never swallowed.
 */
import {
  BoundCallError,
  describeError,
  hintFor,
  toAppError,
  type ErrorCode,
} from '@/shared/api/errors'

export interface BoundErrorMessage {
  code: ErrorCode
  message: string
  details: string[]
  operation?: string | undefined
  /** Extra context from the code table, when it adds something to the message. */
  hint?: string | undefined
}

/**
 * messageFor turns any rejection into the code/message/details triple the UI
 * renders. The backend message wins; the code table only ever adds a hint.
 */
export function messageFor(value: unknown): BoundErrorMessage {
  const error =
    value instanceof BoundCallError
      ? {
          code: value.code,
          message: value.message,
          details: value.details,
          operation: value.operation,
        }
      : toAppError(value)
  const message = describeError(error)
  const hint = hintFor(error.code)
  return {
    code: error.code,
    message,
    details: error.details ?? [],
    ...(error.operation ? { operation: error.operation } : {}),
    ...(hint && hint !== message ? { hint } : {}),
  }
}
