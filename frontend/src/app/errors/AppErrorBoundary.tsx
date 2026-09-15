/**
 * Render-error boundary and query-error surface.
 *
 * `AppErrorBoundary` catches a thrown value during render (an unguarded bound
 * call, a bug in a screen) and renders `messageFor()` output — code, message and
 * details — instead of a blank page. `QueryErrorAlert` does the same for the
 * error half of a TanStack Query result, which is the normal shape of a failed
 * bound call in a screen.
 */
import * as React from 'react'

import { Alert, Button } from '@/shared/ui'

import { messageFor } from './messageFor'

interface AppErrorBoundaryProps {
  children: React.ReactNode
  /** Heading shown above the error message. */
  title?: string
  /** Called when the user asks to retry; also resets the boundary. */
  onReset?: () => void
}

interface AppErrorBoundaryState {
  error: unknown
}

export class AppErrorBoundary extends React.Component<
  AppErrorBoundaryProps,
  AppErrorBoundaryState
> {
  override state: AppErrorBoundaryState = { error: undefined }

  static getDerivedStateFromError(error: unknown): AppErrorBoundaryState {
    return { error }
  }

  override componentDidCatch(error: unknown, info: React.ErrorInfo): void {
    // The console is the only diagnostic channel inside the webview; production
    // builds keep the message but never a raw stack in the UI.
    console.error('Unhandled UI error', error, info.componentStack)
  }

  private readonly reset = (): void => {
    this.setState({ error: undefined })
    this.props.onReset?.()
  }

  override render(): React.ReactNode {
    const { error } = this.state
    if (error === undefined) return this.props.children
    const info = messageFor(error)
    return (
      <Alert
        tone="danger"
        title={this.props.title ?? 'Интерфейс столкнулся с ошибкой'}
        code={info.code}
        details={info.details}
        actions={
          <Button variant="outline" size="sm" onClick={this.reset}>
            Повторить
          </Button>
        }
      >
        {info.message}
      </Alert>
    )
  }
}

/** Renders the error half of a query result through `messageFor`. */
export function QueryErrorAlert({
  error,
  title = 'Не удалось получить данные от бэкенда',
  actions,
}: {
  error: unknown
  title?: string
  actions?: React.ReactNode
}): React.ReactElement | null {
  if (error === undefined || error === null) return null
  const info = messageFor(error)
  return (
    <Alert tone="danger" title={title} code={info.code} details={info.details} actions={actions}>
      {info.message}
    </Alert>
  )
}
