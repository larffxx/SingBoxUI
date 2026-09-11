/**
 * Monaco-based raw JSON editing (spec §34, §56).
 *
 * The raw configuration is a first-class artifact, so it gets a real editor —
 * syntax highlighting, folding, search, formatting and inline markers from the
 * backend validation result. The component is fully controlled: the caller owns
 * the text, the dirty flag and the save/apply actions.
 */
import Editor, { DiffEditor, type Monaco } from '@monaco-editor/react'
import type { editor } from 'monaco-editor'
import * as React from 'react'

import { cn } from '@/shared/lib/cn'

import { Button } from './primitives'

export interface JsonMarker {
  line: number
  column: number
  message: string
  severity: 'error' | 'warning' | 'info'
}

const SEVERITY: Record<JsonMarker['severity'], number> = { error: 8, warning: 4, info: 2 }

function configure(monaco: Monaco) {
  monaco.languages.json.jsonDefaults.setDiagnosticsOptions({
    validate: true,
    allowComments: false,
    schemaValidation: 'warning',
  })
  monaco.editor.defineTheme('singboxui-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: [],
    colors: { 'editor.background': '#0f172a' },
  })
}

/** JsonEditor is the raw config editor. `markers` come from backend validation. */
export function JsonEditor({
  value,
  onChange,
  markers = [],
  readOnly = false,
  height = '60vh',
  theme = 'vs-dark',
  className,
  onFormatRequested,
}: {
  value: string
  onChange: (value: string) => void
  markers?: JsonMarker[]
  readOnly?: boolean
  height?: string
  theme?: 'vs-dark' | 'light' | 'singboxui-dark'
  className?: string
  onFormatRequested?: (() => void) | undefined
}) {
  const editorRef = React.useRef<editor.IStandaloneCodeEditor | null>(null)
  const monacoRef = React.useRef<Monaco | null>(null)

  React.useEffect(() => {
    const monaco = monacoRef.current
    const model = editorRef.current?.getModel()
    if (!monaco || !model) return
    monaco.editor.setModelMarkers(
      model,
      'singboxui',
      markers.map((marker) => ({
        startLineNumber: Math.max(1, marker.line),
        startColumn: Math.max(1, marker.column),
        endLineNumber: Math.max(1, marker.line),
        endColumn: Math.max(1, marker.column) + 1,
        message: marker.message,
        severity: SEVERITY[marker.severity],
      })),
    )
  }, [markers])

  return (
    <div className={cn('overflow-hidden rounded-md border border-border', className)}>
      {onFormatRequested ? (
        <div className="flex items-center justify-end gap-2 border-b border-border bg-muted/40 px-2 py-1">
          <Button size="sm" variant="ghost" onClick={onFormatRequested} disabled={readOnly}>
            Форматировать
          </Button>
        </div>
      ) : null}
      <Editor
        height={height}
        defaultLanguage="json"
        value={value}
        theme={theme}
        onChange={(next) => {
          if (!readOnly) onChange(next ?? '')
        }}
        beforeMount={configure}
        onMount={(instance, monaco) => {
          editorRef.current = instance
          monacoRef.current = monaco
        }}
        options={{
          readOnly,
          minimap: { enabled: false },
          fontSize: 12,
          tabSize: 2,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          wordWrap: 'on',
          renderWhitespace: 'selection',
          bracketPairColorization: { enabled: true },
        }}
      />
    </div>
  )
}

/** JsonDiff renders a read-only diff between two revisions (spec §56). */
export function JsonDiff({
  original,
  modified,
  height = '55vh',
  originalLabel = 'Было',
  modifiedLabel = 'Стало',
}: {
  original: string
  modified: string
  height?: string
  originalLabel?: string
  modifiedLabel?: string
}) {
  return (
    <div className="overflow-hidden rounded-md border border-border">
      <div className="flex items-center gap-4 border-b border-border bg-muted/40 px-3 py-1 text-xs text-muted-foreground">
        <span>{originalLabel}</span>
        <span>{modifiedLabel}</span>
      </div>
      <DiffEditor
        height={height}
        language="json"
        original={original}
        modified={modified}
        theme="vs-dark"
        beforeMount={configure}
        options={{
          readOnly: true,
          minimap: { enabled: false },
          fontSize: 12,
          renderSideBySide: true,
          automaticLayout: true,
          scrollBeyondLastLine: false,
        }}
      />
    </div>
  )
}
