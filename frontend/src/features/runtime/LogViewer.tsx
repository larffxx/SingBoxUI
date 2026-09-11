/**
 * Runtime log viewer (spec §22, §23).
 *
 * A presentational component: the runtime screen owns the buffer (fed by
 * `runtime:log` events, see `@/app/logs/logBuffer`) and passes the
 * records in. Level/search/format filtering goes through `logs.ts`, whose
 * "and above" semantics are unit-tested on their own.
 *
 * The DOM is capped at `LOG_VIEWER_RENDER_CAP` lines so a chatty sing-box build
 * cannot freeze the webview, and the newest lines are the ones kept.
 */
import { ArrowDownToLine, ClipboardCopy, Eraser, RefreshCw, Search } from 'lucide-react'
import * as React from 'react'

import type { runtime } from '@/shared/api/bindings'
import { cn } from '@/shared/lib/cn'
import { Button, EmptyState, Field, Input, Select, SwitchField, Toolbar } from '@/shared/ui'

import {
  filterLogs,
  formatLogLine,
  LOG_FORMATS,
  LOG_LEVEL_FILTERS,
  LOG_VIEWER_RENDER_CAP,
  logLevelTone,
  toClipboardText,
  type LogFormat,
  type LogLevelFilter,
} from './logs'

export interface LogViewerProps {
  records: readonly runtime.LogRecord[]
  /** Lines the backend dropped because its own ring buffer wrapped. */
  dropped?: number
  /** Undefined while the initial snapshot is still loading. */
  refreshing?: boolean
  onRefresh?: () => void
  onClear?: () => void
  /** Initial filter state; the runtime screen leaves it at "all levels". */
  initialLevel?: LogLevelFilter
  initialSearch?: string
  className?: string
}

export function LogViewer({
  records,
  dropped = 0,
  refreshing = false,
  onRefresh,
  onClear,
  initialLevel = 'ALL',
  initialSearch = '',
  className,
}: LogViewerProps): React.ReactElement {
  const [level, setLevel] = React.useState<LogLevelFilter>(initialLevel)
  const [search, setSearch] = React.useState(initialSearch)
  const [follow, setFollow] = React.useState(true)
  const [format, setFormat] = React.useState<LogFormat>('text')
  const [copied, setCopied] = React.useState<'idle' | 'done' | 'failed'>('idle')

  const visible = React.useMemo(
    () => filterLogs(records, { level, search }),
    [records, level, search],
  )
  const shown =
    visible.length > LOG_VIEWER_RENDER_CAP ? visible.slice(-LOG_VIEWER_RENDER_CAP) : visible
  const bodyRef = React.useRef<HTMLPreElement>(null)

  const body = React.useMemo(() => toClipboardText(shown, format), [shown, format])

  React.useEffect(() => {
    if (!follow) return
    const element = bodyRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [follow, body])

  const copy = React.useCallback((): void => {
    void (async () => {
      try {
        await navigator.clipboard.writeText(toClipboardText(visible, format))
        setCopied('done')
      } catch {
        setCopied('failed')
      }
    })()
  }, [visible, format])

  const hiddenByFilter = records.length - visible.length
  const hiddenByCap = visible.length - shown.length

  return (
    <div className={cn('space-y-3', className)} data-testid="log-viewer">
      <Toolbar>
        <Field label="Уровень" htmlFor="log-level" className="w-44">
          <Select
            id="log-level"
            value={level}
            onValueChange={(value) => setLevel(value as LogLevelFilter)}
            options={LOG_LEVEL_FILTERS.map((option) => ({ ...option }))}
          />
        </Field>
        <Field label="Поиск" htmlFor="log-search" className="w-56">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              id="log-search"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Строка или источник"
              className="pl-8"
            />
          </div>
        </Field>
        <Field label="Формат" htmlFor="log-format" className="w-32">
          <Select
            id="log-format"
            value={format}
            onValueChange={(value) => setFormat(value as LogFormat)}
            options={LOG_FORMATS.map((option) => ({ ...option }))}
          />
        </Field>
        <div className="w-44 self-end">
          <SwitchField
            id="log-follow"
            label="Следовать"
            description="Прокручивать к новым строкам"
            checked={follow}
            onCheckedChange={setFollow}
          />
        </div>
        <div className="ml-auto flex items-end gap-2 pb-1">
          {onRefresh ? (
            <Button variant="outline" size="sm" loading={refreshing} onClick={onRefresh}>
              <RefreshCw className="h-4 w-4" />
              Обновить
            </Button>
          ) : null}
          <Button variant="outline" size="sm" onClick={copy} aria-label="Скопировать журнал">
            <ClipboardCopy className="h-4 w-4" />
            {copied === 'done'
              ? 'Скопировано'
              : copied === 'failed'
                ? 'Ошибка копирования'
                : 'Копировать'}
          </Button>
          {onClear ? (
            <Button variant="outline" size="sm" onClick={onClear} aria-label="Очистить журнал">
              <Eraser className="h-4 w-4" />
              Очистить
            </Button>
          ) : null}
        </div>
      </Toolbar>

      <p className="text-xs text-muted-foreground" data-testid="log-counter">
        Показано {shown.length} из {records.length}
        {hiddenByFilter > 0 ? ` · скрыто фильтром: ${hiddenByFilter}` : ''}
        {hiddenByCap > 0 ? ` · скрыто в окне просмотра: ${hiddenByCap}` : ''}
      </p>

      {dropped > 0 ? (
        <p className="text-xs text-amber-600 dark:text-amber-400" data-testid="log-dropped">
          <ArrowDownToLine className="mr-1 inline h-3.5 w-3.5" />
          Бэкенд отбросил {dropped} ранних строк журнала.
        </p>
      ) : null}

      {visible.length === 0 ? (
        <EmptyState
          title={records.length === 0 ? 'Журнал пуст' : 'Ничего не найдено'}
          description={
            records.length === 0
              ? 'Строки появятся после запуска sing-box или при выбранном уровне журнала.'
              : 'Измените уровень или поисковую строку.'
          }
        />
      ) : (
        <pre
          ref={bodyRef}
          aria-label="Журнал рантайма"
          data-testid="log-body"
          className="max-h-80 overflow-auto rounded-md border border-border bg-muted/30 p-3 font-mono text-xs leading-relaxed"
        >
          {format === 'json'
            ? body
            : shown.map((record) => (
                <span
                  key={record.seq}
                  data-level={record.level}
                  data-tone={logLevelTone(record.level)}
                  className="block whitespace-pre-wrap"
                >
                  {formatLogLine(record)}
                </span>
              ))}
        </pre>
      )}
      <p className="text-xs text-muted-foreground">
        В окне просмотра хранится не более {LOG_VIEWER_RENDER_CAP} строк; приостановите следование,
        чтобы прочитать старые записи.
      </p>
    </div>
  )
}
