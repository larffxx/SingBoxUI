/**
 * Route tab (spec §35, §36).
 *
 * Rules are evaluated in order and the first match wins, so the list is
 * reorderable and the default outbound (`route.final`) is always visible.
 * `route.rule_set` entries are declared here and referenced by tag from both the
 * routing rules and the DNS rules.
 */
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

import * as React from 'react'

import {
  getArray,
  getBoolean,
  getObject,
  getString,
  getStringList,
  setList,
  setObject,
  setValue,
  type JsonObject,
} from '../jsonDoc'
import { ROUTE_RULE_PRESETS, type FieldSpec } from '../resourceSpecs'
import { ResourceListEditor } from '../ResourceListEditor'
import { TagMultiSelect } from '../TagMultiSelect'
import { SpecFields } from '../fields'
import { ApplicationsCard } from './ApplicationsCard'
import { RULE_SET_TYPES } from './ruleSetTypes'
import type { DocTabProps } from './types'

/** Fields of one routing rule; `outbound` is filled from the declared tags. */
function ruleFields(outboundTags: string[]): FieldSpec[] {
  const notSet = { value: '', label: '— не ограничивать —' }
  const outboundOptions = [
    notSet,
    { value: 'direct', label: 'direct' },
    { value: 'block', label: 'block' },
    ...outboundTags.map((tag) => ({ value: tag, label: tag })),
  ]
  return [
    {
      key: 'action',
      label: 'Действие',
      kind: 'select',
      options: [
        notSet,
        { value: 'route', label: 'route — отправить в outbound' },
        { value: 'reject', label: 'reject — отклонить' },
        { value: 'hijack-dns', label: 'hijack-dns — перехватить DNS' },
        { value: 'sniff', label: 'sniff — определить протокол' },
        { value: 'resolve', label: 'resolve — разрешить домен' },
      ],
    },
    { key: 'outbound', label: 'Outbound', kind: 'select', options: outboundOptions },
    {
      key: 'protocol',
      label: 'Протокол',
      kind: 'select',
      options: [
        notSet,
        { value: 'tcp', label: 'tcp' },
        { value: 'udp', label: 'udp' },
        { value: 'quic', label: 'quic' },
        { value: 'dns', label: 'dns' },
        { value: 'http', label: 'http' },
        { value: 'tls', label: 'tls' },
      ],
    },
    {
      key: 'domain_suffix',
      label: 'Суффиксы доменов',
      kind: 'list',
      hint: 'По одному значению в строке, например .example.com',
    },
    { key: 'domain_keyword', label: 'Ключевые слова доменов', kind: 'list' },
    {
      key: 'ip_cidr',
      label: 'Префиксы IP',
      kind: 'list',
      hint: 'Например 10.0.0.0/8 или 2001:db8::/32',
    },
    { key: 'source_ip_cidr', label: 'Префиксы IP источника', kind: 'list' },
    { key: 'port', label: 'Порты', kind: 'list', hint: 'Номера портов, по одному в строке' },
    {
      key: 'process_name',
      label: 'Имена процессов',
      kind: 'list',
      hint: 'Например Telegram — имя исполняемого файла (macOS, Windows, Linux)',
    },
    {
      key: 'process_path',
      label: 'Пути процессов',
      kind: 'list',
      hint: 'Полный путь исполняемого файла, совпадение точное',
    },
    {
      key: 'process_path_regex',
      label: 'Пути процессов (регулярное выражение)',
      kind: 'list',
      hint: 'Так записывает приложения карточка выше, например ^/Applications/Telegram\\.app/',
    },
    { key: 'invert', label: 'Инвертировать условие', kind: 'boolean' },
    { key: 'ip_is_private', label: 'Только приватные адреса', kind: 'boolean' },
  ]
}

function RuleEditor({
  rule,
  index,
  onChange,
  outboundTags,
  ruleSetTags,
  inboundTags,
}: {
  rule: JsonObject
  index: number
  onChange: (next: JsonObject) => void
  outboundTags: string[]
  ruleSetTags: string[]
  inboundTags: string[]
}) {
  const fields = React.useMemo(() => ruleFields(outboundTags), [outboundTags])

  return (
    <div className="space-y-3">
      <SpecFields
        specs={fields}
        parent={rule}
        idPrefix={`route-rule-${index}`}
        onChange={onChange}
      />
      <TagMultiSelect
        id={`route-rule-${index}-rule-set`}
        label="rule_set"
        hint="Ссылки на наборы правил, объявленные ниже."
        values={getStringList(rule, 'rule_set')}
        options={ruleSetTags}
        onChange={(next) => {
          onChange(setList(rule, 'rule_set', next))
        }}
      />
      <TagMultiSelect
        id={`route-rule-${index}-inbound`}
        label="inbound"
        values={getStringList(rule, 'inbound')}
        options={inboundTags}
        onChange={(next) => {
          onChange(setList(rule, 'inbound', next))
        }}
      />
    </div>
  )
}

export function RouteTab({ root, onChange }: DocTabProps) {
  const route = getObject(root, 'route')
  const rules = getArray(route, 'rules')
  const ruleSets = getArray(route, 'rule_set')
  const outboundTags = getArray(root, 'outbounds')
    .map((item) => getString(item, 'tag'))
    .filter((tag) => tag !== '')
  const inboundTags = getArray(root, 'inbounds')
    .map((item) => getString(item, 'tag'))
    .filter((tag) => tag !== '')
  const ruleSetTags = ruleSets.map((item) => getString(item, 'tag')).filter((tag) => tag !== '')

  const setRoute = (next: JsonObject) => {
    onChange(setObject(root, 'route', next))
  }

  const setRules = (next: JsonObject[]) => {
    setRoute(setValue(route, 'rules', next))
  }

  const replaceRule = (index: number, next: JsonObject) => {
    setRules(rules.map((rule, position) => (position === index ? next : rule)))
  }

  const moveRule = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= rules.length) return
    const next = [...rules]
    const current = next[index]
    const swapped = next[target]
    if (!current || !swapped) return
    next[index] = swapped
    next[target] = current
    setRules(next)
  }

  const finalOptions = [
    { value: '', label: '— не задан —' },
    { value: 'direct', label: 'direct' },
    { value: 'block', label: 'block' },
    ...outboundTags.map((tag) => ({ value: tag, label: tag })),
  ]

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Маршрутизация по умолчанию</CardTitle>
          <CardDescription>
            Всё, что не совпало ни с одним правилом, уходит в этот outbound.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Field
            label="Outbound по умолчанию (route.final)"
            htmlFor="route-final"
            hint="Обычно указывает на selector или автоматический urltest."
          >
            <Select
              id="route-final"
              value={getString(route, 'final')}
              options={finalOptions}
              onValueChange={(value) => {
                setRoute(setValue(route, 'final', value))
              }}
            />
          </Field>

          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Метка маршрутов (route.default_mark)" htmlFor="route-mark">
              <Input
                id="route-mark"
                value={getString(route, 'default_mark')}
                placeholder="0"
                onChange={(event) => {
                  setRoute(setValue(route, 'default_mark', event.target.value))
                }}
              />
            </Field>
            <Field
              label="Резолвер по умолчанию (route.default_domain_resolver)"
              htmlFor="route-resolver"
            >
              <Input
                id="route-resolver"
                value={getString(route, 'default_domain_resolver')}
                placeholder="dns-local"
                onChange={(event) => {
                  setRoute(setValue(route, 'default_domain_resolver', event.target.value))
                }}
              />
            </Field>
          </div>

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={getBoolean(route, 'auto_detect_interface')}
              onChange={(event) => {
                setRoute(setValue(route, 'auto_detect_interface', event.target.checked))
              }}
            />
            Автоматически определять интерфейс (auto_detect_interface)
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={getBoolean(route, 'override_android_vpn')}
              onChange={(event) => {
                setRoute(setValue(route, 'override_android_vpn', event.target.checked))
              }}
            />
            Переопределять VPN Android (override_android_vpn)
          </label>
        </CardContent>
      </Card>

      <ApplicationsCard root={root} onChange={onChange} />

      <Card>
        <CardHeader>
          <CardTitle>Быстрые правила</CardTitle>
          <CardDescription>Заготовки, которые можно доработать вручную.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          {ROUTE_RULE_PRESETS.map((preset) => (
            <Button
              key={preset.id}
              type="button"
              variant="outline"
              size="sm"
              title={preset.description}
              onClick={() => {
                setRules([...rules, { ...preset.rule }])
              }}
            >
              {preset.label}
            </Button>
          ))}
        </CardContent>
      </Card>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="text-sm font-semibold">Правила маршрутизации ({rules.length})</h3>
          <Button
            type="button"
            size="sm"
            onClick={() => {
              setRules([...rules, { action: 'route', outbound: outboundTags[0] ?? 'direct' }])
            }}
          >
            Добавить правило
          </Button>
        </div>

        {rules.length === 0 ? (
          <Alert tone="info" title="Правил нет">
            Весь трафик пойдёт через outbound по умолчанию. Добавьте правило или примените заготовку
            выше.
          </Alert>
        ) : (
          <ul className="space-y-3">
            {rules.map((rule, index) => (
              <li key={index}>
                <Card>
                  <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">#{index + 1}</span>
                      <code className="text-xs">{getString(rule, 'action') || 'route'}</code>
                      {getString(rule, 'outbound') === '' ? null : (
                        <code className="text-xs text-muted-foreground">
                          → {getString(rule, 'outbound')}
                        </code>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-1">
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Поднять правило ${index + 1}`}
                        disabled={index === 0}
                        onClick={() => {
                          moveRule(index, -1)
                        }}
                      >
                        ↑
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Опустить правило ${index + 1}`}
                        disabled={index === rules.length - 1}
                        onClick={() => {
                          moveRule(index, 1)
                        }}
                      >
                        ↓
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setRules([
                            ...rules.slice(0, index + 1),
                            { ...rule },
                            ...rules.slice(index + 1),
                          ])
                        }}
                      >
                        Дублировать
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setRules(rules.filter((_rule, position) => position !== index))
                        }}
                      >
                        Удалить
                      </Button>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <RuleEditor
                      rule={rule}
                      index={index}
                      onChange={(next) => {
                        replaceRule(index, next)
                      }}
                      outboundTags={outboundTags}
                      ruleSetTags={ruleSetTags}
                      inboundTags={inboundTags}
                    />
                  </CardContent>
                </Card>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold">Наборы правил (route.rule_set)</h3>
        <p className="text-xs text-muted-foreground">
          Наборы объявляются один раз и переиспользуются правилами маршрутизации и DNS по тегу.
        </p>
        <ResourceListEditor
          specs={RULE_SET_TYPES}
          items={ruleSets}
          onChange={(next) => {
            setRoute(setValue(route, 'rule_set', next))
          }}
          idPrefix="rule-set"
          tagPrefix="rs"
          addLabel="Добавить набор"
          emptyTitle="Наборов правил нет"
          emptyDescription="Добавьте набор, чтобы переиспользовать правила из файла или по ссылке."
          defaultType="remote"
        />
      </section>
    </div>
  )
}
