/**
 * Applications card of the routing tab (ADR 011, ADR 013).
 *
 * The routing rules of this application were always address-based: a domain, a
 * prefix, a port. A program is not reachable that way — the user knows
 * “Telegram”, not an IP range — so this card lists the programs of this machine
 * and writes the rule that selects one: a condition matched against the process
 * sing-box reports for a connection. Which condition that is comes from the
 * backend with the listing, because it differs per platform (a `process_path_regex`
 * anchored at a macOS bundle, a `process_name` for a Windows program).
 *
 * The rows read the current document, so the state of an application is what the
 * configuration says, not a separate bookkeeping the JSON could disagree with.
 */
import * as React from 'react'

import {
  Alert,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Field,
  Input,
  Select,
} from '@/shared/ui'

import { getArray, getObject, getString, type JsonObject } from '../jsonDoc'
import {
  appRulesCount,
  appRuleTarget,
  conditionDescription,
  setAppRoute,
  type AppTarget,
  type AppCondition,
} from '../appRules'
import { useApplicationsQuery } from '../queries'
import type { DocTabProps } from './types'

/** The outbound a new rule sends through the tunnel unless the user chooses one. */
function defaultProxyOutbound(route: JsonObject, outboundTags: string[]): string {
  const outbound = getString(route, 'final')
  if (isProxyTag(outbound)) return outbound
  return outboundTags.find((tag) => isProxyTag(tag)) ?? ''
}

/** isProxyTag reports whether a tag names an outbound the card can send traffic through. */
function isProxyTag(tag: string): boolean {
  return tag !== '' && tag !== 'direct' && tag !== 'block'
}

/** How many applications the list shows before it offers to unfold. */
const COLLAPSED_ROWS = 5

/** matchesFilter reports whether one program answers to the search box. */
function matchesFilter(query: string, needles: string[]): boolean {
  const needle = query.trim().toLowerCase()
  if (needle === '') return true
  return needles.some((value) => value.toLowerCase().includes(needle))
}

/** entryCondition is the rule condition the backend declared for one program. */
function entryCondition(app: { matchKey: string; matchValue: string }): AppCondition {
  return { key: app.matchKey, value: app.matchValue }
}

export function ApplicationsCard({ root, onChange }: DocTabProps) {
  const query = useApplicationsQuery()
  const [proxy, setProxy] = React.useState('')
  const [filter, setFilter] = React.useState('')
  const [onlyMarked, setOnlyMarked] = React.useState(false)
  const [expanded, setExpanded] = React.useState(false)

  // A new search is a new list: the unfolded state of the previous one would only
  // make the answer harder to read.
  React.useEffect(() => {
    setExpanded(false)
  }, [filter, onlyMarked])

  const route = getObject(root, 'route')
  const rules = getArray(route, 'rules')
  const outboundTags = getArray(root, 'outbounds')
    .map((item) => getString(item, 'tag'))
    .filter((tag) => tag !== '')
  const proxyOptions = outboundTags.filter((tag) => tag !== 'direct' && tag !== 'block')
  const proxyOutbound = proxy !== '' ? proxy : defaultProxyOutbound(route, outboundTags)

  const list = query.data?.apps
  if (!list) {
    // Outside the desktop runtime the query never runs, and the tab shows the
    // rest of its fields: an empty stand-in card would only be in the way.
    if (query.isLoading) {
      return (
        <Card>
          <CardHeader>
            <CardTitle>Приложения</CardTitle>
            <CardDescription>Ищу установленные программы…</CardDescription>
          </CardHeader>
        </Card>
      )
    }
    return null
  }

  const setRules = (next: JsonObject[]) => {
    onChange({ ...root, route: { ...route, rules: next } })
  }

  const choose = (condition: AppCondition, target: AppTarget) => {
    setRules(setAppRoute(rules, condition, target, proxyOutbound))
  }

  // Every application already has a direction: the one the configuration gives it. `route.final`
  // decides what a program without a rule does, so the row shows that as the current state — with
  // the tunnel as the final, "через VPN" is the default and a rule is the exception; with a
  // direct final (the TUN-basic starter, say) the default is "напрямую" and a rule is how a
  // program is taken *into* the tunnel.
  const defaultTarget: AppTarget = isProxyTag(getString(route, 'final')) ? 'proxy' : 'direct'
  const targetOf = (condition: AppCondition): AppTarget => {
    const explicit = appRuleTarget(rules, condition)
    return explicit === 'off' ? defaultTarget : explicit
  }

  // A program the backend could not describe with a condition cannot be routed
  // from here: the rule editor is the tool for it.
  const described = list.applications.filter((app) => app.matchKey !== '' && app.matchValue !== '')
  const applications = described.filter((app) =>
    matchesFilter(filter, [app.name, app.bundleId, app.executable, app.path]),
  )
  // "Отмеченные" means the applications the user gave a rule of their own: everything else
  // follows the configuration's default and needs no entry.
  const visible = onlyMarked
    ? applications.filter((app) => appRuleTarget(rules, entryCondition(app)) !== 'off')
    : applications
  const conditionKey = described[0]?.matchKey ?? ''
  const shown = expanded ? visible : visible.slice(0, COLLAPSED_ROWS)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Приложения</CardTitle>
        <CardDescription>
          В конфигурации весь трафик идёт через VPN, поэтому каждая программа по умолчанию — «через
          VPN». Здесь отмечают исключения: «Напрямую» — для программ, которым нужен прямой выход.
          Правило выбирает программу по ней самой, а не по домену или адресу; правила применяются по
          порядку, поэтому новое добавляется в конец списка ниже.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.error ? (
          <Alert tone="danger" title="Список приложений недоступен">
            {query.error instanceof Error ? query.error.message : 'Неизвестная ошибка.'}
          </Alert>
        ) : null}

        {!list.supported ? (
          <Alert tone="info" title="Выбор приложений недоступен">
            {list.reason || 'Эта система не предоставляет список установленных приложений.'}
          </Alert>
        ) : null}

        {list.supported ? (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field
                label="Outbound для «через VPN»"
                htmlFor="apps-proxy"
                hint={
                  proxyOutbound === ''
                    ? 'В конфигурации нет outbound, кроме direct — добавьте прокси на вкладке Outbounds.'
                    : 'Куда уходит трафик программ, отмеченных «через VPN».'
                }
              >
                <Select
                  id="apps-proxy"
                  value={proxyOutbound}
                  options={proxyOptions.map((tag) => ({ value: tag, label: tag }))}
                  onValueChange={(value) => {
                    setProxy(value)
                  }}
                />
              </Field>
              <Field label="Поиск" htmlFor="apps-filter">
                <Input
                  id="apps-filter"
                  value={filter}
                  placeholder="Telegram, Chrome, chrome.exe…"
                  spellCheck={false}
                  autoComplete="off"
                  onChange={(event) => {
                    setFilter(event.target.value)
                  }}
                />
              </Field>
            </div>

            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={onlyMarked}
                onChange={(event) => {
                  setOnlyMarked(event.target.checked)
                }}
              />
              Показывать только с правилом
            </label>

            {visible.length === 0 ? (
              <Alert tone="info" title="Ничего не найдено">
                {onlyMarked
                  ? 'Ни одной программе не назначено своё правило: все идут через VPN, как в конфигурации.'
                  : 'Ни одна программа не подошла под запрос.'}
              </Alert>
            ) : (
              <ul className="divide-y rounded-md border">
                {shown.map((app) => {
                  const condition = entryCondition(app)
                  const target = targetOf(condition)
                  return (
                    <li
                      key={app.path}
                      className="flex flex-wrap items-center justify-between gap-3 p-3"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">{app.name}</p>
                        <p className="truncate font-mono text-xs text-muted-foreground">
                          {app.bundleId === '' ? app.path : `${app.bundleId} — ${app.path}`}
                        </p>
                      </div>
                      <div className="flex flex-wrap gap-1.5" role="group" aria-label={app.name}>
                        <Button
                          type="button"
                          size="sm"
                          variant={target === 'proxy' ? 'default' : 'outline'}
                          aria-pressed={target === 'proxy'}
                          disabled={proxyOutbound === ''}
                          onClick={() => {
                            choose(condition, target === 'proxy' ? 'off' : 'proxy')
                          }}
                        >
                          Через VPN
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          variant={target === 'direct' ? 'default' : 'outline'}
                          aria-pressed={target === 'direct'}
                          onClick={() => {
                            choose(condition, target === 'direct' ? 'off' : 'direct')
                          }}
                        >
                          Напрямую
                        </Button>
                      </div>
                    </li>
                  )
                })}
              </ul>
            )}

            {visible.length > COLLAPSED_ROWS ? (
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => {
                  setExpanded(!expanded)
                }}
              >
                {expanded ? 'Свернуть' : `Показать все (${visible.length})`}
              </Button>
            ) : null}

            {conditionKey === '' ? null : (
              <p className="text-xs text-muted-foreground">
                Правил для приложений: {appRulesCount(rules)}. Условие — <code>{conditionKey}</code>
                : {conditionDescription(conditionKey)} Другое условие для программы или целой папки
                (например, каталога игровой библиотеки) можно добавить на вкладке правил.
              </p>
            )}
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
