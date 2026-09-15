/**
 * Profile workspace (spec §34, §35, §36, §37).
 *
 * The raw JSON document is the single source of truth: it is parsed once here
 * and handed to the structured tabs as a plain object, and every structured edit
 * is re-serialised back into text. That keeps the editor honest — nothing can
 * drift from the file that will actually be written.
 *
 * Validation runs through the backend (`configApi.validate`) and is debounced
 * while typing; saving is blocked while it reports errors.
 */
import * as React from 'react'
import { useParams } from '@tanstack/react-router'
import { CheckCircle2, CircleAlert, RefreshCw, Save } from 'lucide-react'

import { QueryErrorAlert } from '@/app/errors/AppErrorBoundary'
import { useActiveConfigPathQuery, useProfilesQuery } from '@/app/queries'
import { formatBytes } from '@/shared/lib/format'
import { formatDateTime } from '@/shared/lib/time'
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  KeyValueGrid,
  PageHeader,
  Spinner,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Toolbar,
} from '@/shared/ui'

import { parseDoc, stringifyDoc, type JsonObject } from './jsonDoc'
import type { singbox } from '@/shared/api/bindings'
import { SaveRevisionDialog } from './SaveRevisionDialog'
import { useDraftQuery, useValidateMutation } from './queries'
import { DnsTab } from './tabs/DnsTab'
import { EndpointsTab } from './tabs/EndpointsTab'
import { ExperimentalTab } from './tabs/ExperimentalTab'
import { InboundsTab } from './tabs/InboundsTab'
import { OutboundsTab } from './tabs/OutboundsTab'
import { RawTab, type JumpTarget } from './tabs/RawTab'
import { RevisionsTab } from './tabs/RevisionsTab'
import { RouteTab } from './tabs/RouteTab'
import { EMPTY_SUMMARY, summarize, type ValidationSummary } from './validation'

/** Debounce before a keystroke is sent to the backend for validation. */
export const VALIDATION_DEBOUNCE_MS = 600

const TAB_OVERVIEW = 'overview'
const TAB_RAW = 'raw'

type TabId =
  | typeof TAB_OVERVIEW
  | 'route'
  | 'dns'
  | 'experimental'
  | 'inbounds'
  | 'outbounds'
  | 'endpoints'
  | typeof TAB_RAW

export function ProfileWorkspaceRoute(): React.ReactElement {
  const params = useParams({ strict: false }) as { profileId?: string }
  const profileId = params.profileId ?? ''

  const profilesQuery = useProfilesQuery()
  const draftQuery = useDraftQuery(profileId)
  const activeConfigPathQuery = useActiveConfigPathQuery()
  const validate = useValidateMutation()

  const [text, setText] = React.useState('')
  const [tab, setTab] = React.useState<TabId>(TAB_OVERVIEW)
  const [summary, setSummary] = React.useState<ValidationSummary>(EMPTY_SUMMARY)
  const [jump, setJump] = React.useState<JumpTarget | null>(null)
  const [saveOpen, setSaveOpen] = React.useState(false)
  const [notice, setNotice] = React.useState<string | null>(null)

  const loadedSignature = React.useRef('')
  const validateRef = React.useRef(validate)
  validateRef.current = validate

  const draft = draftQuery.data?.draft
  const profile = (profilesQuery.data?.profiles ?? []).find((item) => item.id === profileId)

  // The draft that is on disk right now: the buffer is reset only when a
  // different draft arrives, so an in-progress edit survives a refetch.
  React.useEffect(() => {
    if (draft === undefined || profileId === '') return
    const signature = `${profileId}::${draft.baseRevisionId}::${draft.configJson}`
    if (signature === loadedSignature.current) return
    loadedSignature.current = signature
    setText(draft.configJson)
    const check = parseCheck(draft.validation)
    setSummary(
      summarize(
        draft.configJson,
        draft.structural,
        check,
        draft.structural.ok && (check?.ok ?? true),
      ),
    )
  }, [draft, profileId])

  const runValidation = React.useCallback(
    (next: string) => {
      if (profileId === '' || next.trim() === '') return
      validateRef.current.mutate(
        { profileId, configJson: next, skipSingBoxCheck: false },
        {
          onSuccess: (payload) => {
            const result = payload.result
            setSummary(summarize(next, result.structural, result.check, result.valid))
          },
        },
      )
    },
    [profileId],
  )

  // Debounced validation while typing. `next` is captured so the summary always
  // describes the text it was produced from.
  React.useEffect(() => {
    if (profileId === '' || text.trim() === '') return
    const handle = window.setTimeout(() => {
      runValidation(text)
    }, VALIDATION_DEBOUNCE_MS)
    return () => {
      window.clearTimeout(handle)
    }
  }, [text, profileId, runValidation])

  const parsed = React.useMemo(() => parseDoc(text), [text])
  const doc: JsonObject | null = 'doc' in parsed ? parsed.doc : null
  const parseError = 'error' in parsed ? parsed.error : null

  const dirty = draft !== undefined && text !== draft.configJson
  const blocked = summary.errorCount > 0
  const validating = validate.isPending

  const applyEdit = (next: JsonObject) => {
    setText(stringifyDoc(next))
  }

  if (profileId === '') {
    return (
      <Alert tone="danger" title="Профиль не выбран">
        Откройте профиль из списка.
      </Alert>
    )
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title={profile?.name ?? 'Профиль'}
        description={
          profile?.description === undefined || profile.description === ''
            ? 'Редактор конфигурации sing-box.'
            : profile.description
        }
        actions={
          <Toolbar>
            <Badge tone={dirty ? 'warning' : 'neutral'}>
              {dirty ? 'есть несохранённые изменения' : 'без изменений'}
            </Badge>
            <Button
              variant="secondary"
              disabled={validating || text.trim() === ''}
              onClick={() => {
                runValidation(text)
              }}
            >
              <RefreshCw className="h-4 w-4" aria-hidden />
              Проверить
            </Button>
            <Button
              disabled={blocked || saveOpen || text.trim() === ''}
              onClick={() => {
                setSaveOpen(true)
              }}
            >
              <Save className="h-4 w-4" aria-hidden />
              Сохранить ревизию
            </Button>
          </Toolbar>
        }
      />

      {profileId !== '' && (profilesQuery.data?.activeId ?? '') !== profileId ? (
        <Alert tone="info" title="Это не активный профиль">
          Правки применяются к копии конфигурации этого профиля. Активная конфигурация изменится
          только после применения ревизии.
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Валидация</CardTitle>
          <CardDescription>
            Структурная проверка и разбор реальным бинарником sing-box. Сохранение заблокировано,
            пока есть ошибки.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            {validating ? <Spinner label="Проверяем…" /> : null}
            <Badge tone={summary.structuralOk ? 'success' : 'danger'}>
              структура: {summary.structuralOk ? 'ок' : 'ошибки'}
            </Badge>
            <Badge tone={summary.checkOk ? 'success' : 'danger'}>
              sing-box: {summary.checkOk ? 'ок' : 'ошибки'}
            </Badge>
            <Badge tone={summary.errorCount > 0 ? 'danger' : 'neutral'}>
              ошибок: {summary.errorCount}
            </Badge>
            <Badge tone={summary.warningCount > 0 ? 'warning' : 'neutral'}>
              предупреждений: {summary.warningCount}
            </Badge>
            {summary.version === '' ? null : <Badge tone="info">sing-box {summary.version}</Badge>}
          </div>

          {blocked ? (
            <Alert tone="danger" title="Сохранение заблокировано">
              Исправьте ошибки ниже. Пока они есть, ревизия не сохраняется — так в историю не
              попадают заведомо нерабочие конфигурации.
            </Alert>
          ) : null}

          {parseError === null ? null : (
            <Alert tone="danger" title="JSON не разобран">
              {parseError}
            </Alert>
          )}

          {summary.issues.length === 0 ? (
            <p className="text-sm text-muted-foreground">Замечаний нет.</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {summary.issues.map((issue, index) => (
                <li key={`${issue.line}:${issue.column}:${issue.message}`} className="flex gap-2">
                  <span className="w-16 shrink-0 font-mono text-xs text-muted-foreground">
                    {issue.line}:{issue.column}
                  </span>
                  <span
                    className={issue.severity === 'error' ? 'text-destructive' : 'text-amber-600'}
                  >
                    {issue.severity === 'error' ? 'ошибка' : 'предупреждение'}
                  </span>
                  <span className="min-w-0 flex-1">{issue.message}</span>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setTab(TAB_RAW)
                      setJump({ line: issue.line, column: issue.column, nonce: index + 1 })
                    }}
                  >
                    Перейти
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      {notice === null ? null : (
        <Alert
          tone="success"
          title="Готово"
          actions={
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setNotice(null)
              }}
            >
              Скрыть
            </Button>
          }
        >
          {notice}
        </Alert>
      )}

      <QueryErrorAlert error={draftQuery.error} />

      <Tabs
        value={tab}
        onValueChange={(value) => {
          setTab(value as TabId)
        }}
      >
        <TabsList>
          <TabsTrigger value={TAB_OVERVIEW}>Обзор</TabsTrigger>
          <TabsTrigger value="route">Route</TabsTrigger>
          <TabsTrigger value="dns">DNS</TabsTrigger>
          <TabsTrigger value="experimental">Experimental</TabsTrigger>
          <TabsTrigger value="inbounds">Inbounds</TabsTrigger>
          <TabsTrigger value="outbounds">Outbounds</TabsTrigger>
          <TabsTrigger value="endpoints">Endpoints</TabsTrigger>
          <TabsTrigger value={TAB_RAW}>Raw JSON</TabsTrigger>
        </TabsList>

        <TabsContent value={TAB_OVERVIEW} className="pt-4">
          {draftQuery.isPending ? (
            <Spinner label="Загружаем черновик…" />
          ) : (
            <OverviewTab
              text={text}
              doc={doc}
              profileName={profile?.name ?? ''}
              profileId={profileId}
              activeRevisionId={draft?.baseRevisionId ?? ''}
              baseSource={draft?.baseSource ?? ''}
              updatedAt={profile?.updatedAt ?? ''}
              summary={summary}
            />
          )}
        </TabsContent>

        <TabsContent value="route" className="pt-4">
          {doc === null ? (
            <RawFallback
              onChange={() => {
                setTab(TAB_RAW)
              }}
            />
          ) : (
            <RouteTab root={doc} onChange={applyEdit} />
          )}
        </TabsContent>

        <TabsContent value="dns" className="pt-4">
          {doc === null ? (
            <RawFallback
              onChange={() => {
                setTab(TAB_RAW)
              }}
            />
          ) : (
            <DnsTab root={doc} onChange={applyEdit} />
          )}
        </TabsContent>

        <TabsContent value="experimental" className="pt-4">
          {doc === null ? (
            <RawFallback
              onChange={() => {
                setTab(TAB_RAW)
              }}
            />
          ) : (
            <ExperimentalTab root={doc} onChange={applyEdit} />
          )}
        </TabsContent>

        <TabsContent value="inbounds" className="pt-4">
          {doc === null ? (
            <RawFallback
              onChange={() => {
                setTab(TAB_RAW)
              }}
            />
          ) : (
            <InboundsTab root={doc} onChange={applyEdit} />
          )}
        </TabsContent>

        <TabsContent value="outbounds" className="pt-4">
          {doc === null ? (
            <RawFallback
              onChange={() => {
                setTab(TAB_RAW)
              }}
            />
          ) : (
            <OutboundsTab root={doc} onChange={applyEdit} />
          )}
        </TabsContent>

        <TabsContent value="endpoints" className="pt-4">
          {doc === null ? (
            <RawFallback
              onChange={() => {
                setTab(TAB_RAW)
              }}
            />
          ) : (
            <EndpointsTab root={doc} onChange={applyEdit} />
          )}
        </TabsContent>

        <TabsContent value={TAB_RAW} className="pt-4">
          <RawTab
            text={text}
            onChange={setText}
            summary={summary}
            jump={jump}
            onJump={(line, column) => {
              setJump((current) => ({
                line,
                column,
                nonce: (current?.nonce ?? 0) + 1,
              }))
            }}
            validating={validating}
          />
        </TabsContent>
      </Tabs>

      <SaveRevisionDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        profileId={profileId}
        configJson={text}
        blockedByErrors={blocked}
        onSaved={(revisionId, applied) => {
          setNotice(
            applied
              ? `Ревизия ${revisionId.slice(0, 12)} сохранена и применена.`
              : `Ревизия ${revisionId.slice(0, 12)} сохранена. История обновлена.`,
          )
        }}
      />

      <RevisionsPanel
        profileId={profileId}
        text={text}
        summary={summary}
        onRequestSave={() => {
          setSaveOpen(true)
        }}
        activeConfigPath={activeConfigPathQuery.data ?? ''}
      />
    </div>
  )
}

/**
 * Overview tab: a read-only summary of what the document actually contains. It
 * is deliberately derived from the parsed JSON rather than from a separate
 * backend call, so it never disagrees with the editor.
 */
function OverviewTab({
  text,
  doc,
  profileName,
  profileId,
  activeRevisionId,
  baseSource,
  updatedAt,
  summary,
}: {
  text: string
  doc: JsonObject | null
  profileName: string
  profileId: string
  activeRevisionId: string
  baseSource: string
  updatedAt: string
  summary: ValidationSummary
}) {
  const counts =
    doc === null
      ? null
      : {
          inbounds: arrayLength(doc, 'inbounds'),
          outbounds: arrayLength(doc, 'outbounds'),
          endpoints: arrayLength(doc, 'endpoints'),
          routeRules: arrayLength(nested(doc, 'route'), 'rules'),
          ruleSets: arrayLength(nested(doc, 'route'), 'rule_set'),
          dnsServers: arrayLength(nested(doc, 'dns'), 'servers'),
          dnsRules: arrayLength(nested(doc, 'dns'), 'rules'),
        }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>Документ</CardTitle>
          <CardDescription>{formatBytes(new TextEncoder().encode(text).length)}</CardDescription>
        </CardHeader>
        <CardContent>
          <KeyValueGrid
            columns={2}
            items={[
              { label: 'Профиль', value: profileName === '' ? profileId : profileName },
              { label: 'Обновлён', value: updatedAt === '' ? '—' : formatDateTime(updatedAt) },
              {
                label: 'Базовая ревизия',
                value: activeRevisionId === '' ? 'нет' : activeRevisionId.slice(0, 12),
              },
              { label: 'Источник базы', value: baseSource === '' ? '—' : baseSource },
              { label: 'Inbounds', value: countLabel(counts?.inbounds) },
              { label: 'Outbounds', value: countLabel(counts?.outbounds) },
              { label: 'Endpoints', value: countLabel(counts?.endpoints) },
              { label: 'Правила Route', value: countLabel(counts?.routeRules) },
              { label: 'Rule-set', value: countLabel(counts?.ruleSets) },
              { label: 'DNS-серверы', value: countLabel(counts?.dnsServers) },
              { label: 'Правила DNS', value: countLabel(counts?.dnsRules) },
              {
                label: 'Статус',
                value: summary.errorCount > 0 ? `ошибок ${summary.errorCount}` : 'валидно',
              },
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Порядок работы</CardTitle>
          <CardDescription>
            Правки живут в черновике профиля. Активная конфигурация и рантайм меняются только после
            применения ревизии.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {summary.errorCount > 0 ? (
            <Badge tone="danger">
              <CircleAlert className="h-3 w-3" aria-hidden /> есть ошибки
            </Badge>
          ) : (
            <Badge tone="success">
              <CheckCircle2 className="h-3 w-3" aria-hidden /> ошибок нет
            </Badge>
          )}
          <span>1. Отредактировать документ</span>
          <span>2. Проверить</span>
          <span>3. Сохранить ревизию</span>
          <span>4. Применить ревизию во вкладке «Ревизии»</span>
        </CardContent>
      </Card>
    </div>
  )
}

function RevisionsPanel({
  profileId,
  text,
  summary,
  onRequestSave,
  activeConfigPath,
}: {
  profileId: string
  text: string
  summary: ValidationSummary
  onRequestSave: () => void
  activeConfigPath: string
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Ревизии</CardTitle>
        <CardDescription>
          История, сравнение и применение. Действия с рантаймом просят подтверждение.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <RevisionsTab
          profileId={profileId}
          draftJson={text}
          summary={summary}
          onRequestSave={onRequestSave}
          activeConfigPath={activeConfigPath}
        />
      </CardContent>
    </Card>
  )
}

function RawFallback({ onChange }: { onChange: () => void }) {
  return (
    <Alert
      tone="warning"
      title="Документ не разобран"
      actions={
        <Button size="sm" variant="secondary" onClick={onChange}>
          Открыть Raw JSON
        </Button>
      }
    >
      Структурированные редакторы работают только с корректным JSON.
    </Alert>
  )
}

function arrayLength(parent: JsonObject | null, key: string): number {
  const value = parent === null ? undefined : parent[key]
  return Array.isArray(value) ? value.length : 0
}

function nested(parent: JsonObject | null, key: string): JsonObject | null {
  if (parent === null) return null
  const value = parent[key]
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as JsonObject)
    : null
}

function countLabel(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
}

/**
 * The draft carries the sing-box `check` output as a JSON string (the backend
 * stores it that way so a revision can be compared byte-for-byte). Parsing here
 * keeps the failure mode boring: unreadable text simply means "no check result".
 */
function parseCheck(json: string): singbox.CheckResult | undefined {
  if (json.trim() === '') return undefined
  try {
    const value: unknown = JSON.parse(json)
    if (typeof value !== 'object' || value === null) return undefined
    return value as singbox.CheckResult
  } catch {
    return undefined
  }
}
