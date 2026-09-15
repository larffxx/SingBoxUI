/**
 * Raw tab (spec §34, §37).
 *
 * The JSON is the source of truth; this tab is the escape hatch for everything
 * the structured tabs do not model yet. Validation happens in the backend
 * (`configApi.validate`) and is mapped onto Monaco markers by `validation.ts`;
 * the same summary drives the issue list below the editor, which is how a user
 * finds a line without reading the whole document.
 */
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  JsonEditor,
} from '@/shared/ui'
import { Spinner } from '@/shared/ui'

import * as React from 'react'

import { parseDoc, stringifyDoc } from '../jsonDoc'
import type { ValidationSummary } from '../validation'

/** A requested jump; `nonce` makes repeated jumps to the same line re-run. */
export interface JumpTarget {
  line: number
  column: number
  nonce: number
}

interface MonacoLike {
  editor: {
    getEditors: () => {
      getDomNode: () => HTMLElement | null
      revealLineInCenter: (line: number) => void
      setPosition: (position: { lineNumber: number; column: number }) => void
      focus: () => void
    }[]
  }
}

/**
 * Reveals a line in the Monaco editor.
 *
 * `JsonEditor` (shared layer) keeps its editor instance private, so the jump
 * walks the editors Monaco publishes on `window` and picks the one living inside
 * this tab's container. When that namespace is absent — unit tests, or a build
 * that does not publish it — the marker is still rendered on the right line and
 * the issue list keeps the position visible, so nothing is lost.
 */
function revealLine(container: HTMLElement | null, line: number, column: number): boolean {
  if (container === null) return false
  const monaco = (window as unknown as { monaco?: MonacoLike }).monaco
  if (monaco === undefined) return false
  const editor = monaco.editor.getEditors().find((candidate) => {
    const node = candidate.getDomNode()
    return node !== null && container.contains(node)
  })
  if (editor === undefined) return false
  editor.revealLineInCenter(line)
  editor.setPosition({ lineNumber: line, column })
  editor.focus()
  return true
}

export function RawTab({
  text,
  onChange,
  summary,
  jump,
  onJump,
  validating,
}: {
  text: string
  onChange: (next: string) => void
  summary: ValidationSummary
  jump: JumpTarget | null
  onJump: (line: number, column: number) => void
  validating: boolean
}) {
  const containerRef = React.useRef<HTMLDivElement | null>(null)
  const [formatError, setFormatError] = React.useState('')
  const [jumpUnavailable, setJumpUnavailable] = React.useState(false)

  React.useEffect(() => {
    if (jump === null) return
    setJumpUnavailable(!revealLine(containerRef.current, jump.line, jump.column))
  }, [jump])

  const format = () => {
    const parsed = parseDoc(text)
    if ('error' in parsed) {
      setFormatError(parsed.error)
      return
    }
    setFormatError('')
    onChange(stringifyDoc(parsed.doc))
  }

  const errors = summary.issues.filter((issue) => issue.severity === 'error')
  const warnings = summary.issues.filter((issue) => issue.severity === 'warning')

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          {validating ? <Spinner label="Проверяем…" /> : null}
          <Badge tone={summary.errorCount > 0 ? 'danger' : 'success'}>
            {summary.errorCount > 0 ? `${summary.errorCount} ошибок` : 'Ошибок нет'}
          </Badge>
          {summary.warningCount > 0 ? (
            <Badge tone="warning">{`${summary.warningCount} предупреждений`}</Badge>
          ) : null}
          {summary.version === '' ? null : (
            <span className="text-xs text-muted-foreground">sing-box {summary.version}</span>
          )}
        </div>
        <Button type="button" variant="outline" size="sm" onClick={format}>
          Форматировать
        </Button>
      </div>

      {formatError === '' ? null : (
        <Alert tone="danger" title="Не удалось отформатировать">
          {formatError}
        </Alert>
      )}

      <div ref={containerRef} className="overflow-hidden rounded-md border border-border">
        <JsonEditor value={text} onChange={onChange} markers={summary.markers} height="52vh" />
      </div>

      {jumpUnavailable ? (
        <p className="text-xs text-muted-foreground">
          Переход выполнен по маркеру: строка {jump?.line ?? 0}, столбец {jump?.column ?? 1}.
        </p>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Проблемы ({summary.issues.length})</CardTitle>
          <CardDescription>Ошибки блокируют сохранение, предупреждения — нет.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {summary.issues.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Конфигурация прошла и структурную проверку, и <code>sing-box check</code>.
            </p>
          ) : (
            <ul className="space-y-2">
              {[...errors, ...warnings].map((issue, index) => (
                <li
                  key={`${issue.severity}-${issue.line}-${issue.column}-${index}`}
                  className="flex flex-wrap items-start justify-between gap-2 rounded-md border border-border/70 p-2"
                >
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <Badge tone={issue.severity === 'error' ? 'danger' : 'warning'}>
                        {issue.severity === 'error' ? 'Ошибка' : 'Предупреждение'}
                      </Badge>
                      <span className="font-mono text-xs text-muted-foreground">
                        {issue.line}:{issue.column}
                      </span>
                    </div>
                    <p className="text-sm">{issue.message}</p>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      onJump(issue.line, issue.column)
                    }}
                  >
                    Перейти к строке {issue.line}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          {summary.checkOutput === '' ? null : (
            <details className="text-xs text-muted-foreground">
              <summary className="cursor-pointer">Вывод sing-box check</summary>
              <pre className="mt-2 whitespace-pre-wrap font-mono">{summary.checkOutput}</pre>
            </details>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
