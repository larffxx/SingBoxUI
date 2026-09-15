/**
 * DNS tab (spec §35, §36).
 *
 * DNS servers are declared once and referenced by tag from `dns.rules` and from
 * tunnels/outbounds. The presets write a complete `dns` object, because a
 * half-configured DNS block is the most common way to break a profile.
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
  Select,
} from '@/shared/ui'

import * as React from 'react'

import {
  getArray,
  getObject,
  getString,
  getStringList,
  setList,
  setObject,
  setValue,
  type JsonObject,
} from '../jsonDoc'
import { DNS_PRESETS, type FieldSpec } from '../resourceSpecs'
import { ResourceListEditor } from '../ResourceListEditor'
import { SpecFields } from '../fields'
import { TagMultiSelect } from '../TagMultiSelect'
import { DNS_SERVER_TYPES } from './dnsServerTypes'
import { applyDnsPreset } from './dnsPreset'
import type { DocTabProps } from './types'

/** Fields of one DNS rule; `server` is filled from the declared server tags. */
function dnsRuleFields(serverTags: string[]): FieldSpec[] {
  return [
    {
      key: 'server',
      label: 'DNS-сервер',
      kind: 'select',
      options: [
        { value: '', label: '— не задан —' },
        ...serverTags.map((tag) => ({ value: tag, label: tag })),
      ],
    },
    {
      key: 'action',
      label: 'Действие',
      kind: 'select',
      options: [
        { value: '', label: '— по умолчанию —' },
        { value: 'route', label: 'route' },
        { value: 'reject', label: 'reject' },
        { value: 'predefined', label: 'predefined' },
      ],
    },
    { key: 'domain_suffix', label: 'Суффиксы доменов', kind: 'list' },
    { key: 'domain_keyword', label: 'Ключевые слова доменов', kind: 'list' },
    { key: 'query_type', label: 'Типы запросов', kind: 'list', hint: 'Например A, AAAA, HTTPS' },
    { key: 'client_subnet', label: 'Подсеть клиента', kind: 'text', placeholder: '203.0.113.0/24' },
    { key: 'disable_cache', label: 'Отключить кэш', kind: 'boolean' },
  ]
}

export function DnsTab({ root, onChange }: DocTabProps) {
  const dns = getObject(root, 'dns')
  const servers = getArray(dns, 'servers')
  const rules = getArray(dns, 'rules')
  const serverTags = servers.map((item) => getString(item, 'tag')).filter((tag) => tag !== '')
  const ruleSetTags = getArray(getObject(root, 'route'), 'rule_set')
    .map((item) => getString(item, 'tag'))
    .filter((tag) => tag !== '')

  const setDns = (next: JsonObject) => {
    onChange(setObject(root, 'dns', next))
  }

  const setRules = (next: JsonObject[]) => {
    setDns(setValue(dns, 'rules', next))
  }

  const ruleFields = React.useMemo(() => dnsRuleFields(serverTags), [serverTags])

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Готовые наборы</CardTitle>
          <CardDescription>
            Набор заменяет весь блок `dns` целиком: серверы, правила и стратегию. Если резолвер
            (route.default_domain_resolver) ещё не задан, набор задаёт его сам: без него sing-box
            1.14 пишет «не запускается» и не объясняет почему.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          {DNS_PRESETS.map((preset) => (
            <Button
              key={preset.id}
              type="button"
              variant="outline"
              size="sm"
              title={preset.description}
              onClick={() => {
                onChange(applyDnsPreset(root, preset))
              }}
            >
              {preset.label}
            </Button>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Стратегия разрешения имён</CardTitle>
          <CardDescription>
            Определяет, какой адрес предпочитать, когда у домена есть и A, и AAAA.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <Field label="Стратегия (dns.strategy)" htmlFor="dns-strategy">
            <Select
              id="dns-strategy"
              value={getString(dns, 'strategy')}
              options={[
                { value: '', label: '— по умолчанию —' },
                { value: 'prefer_ipv4', label: 'prefer_ipv4' },
                { value: 'prefer_ipv6', label: 'prefer_ipv6' },
                { value: 'ipv4_only', label: 'ipv4_only' },
                { value: 'ipv6_only', label: 'ipv6_only' },
              ]}
              onValueChange={(value) => {
                setDns(setValue(dns, 'strategy', value))
              }}
            />
          </Field>
          <Field
            label="Сервер по умолчанию (dns.final)"
            htmlFor="dns-final"
            hint="Если правило не выбрало сервер, запрос уходит сюда."
          >
            <Select
              id="dns-final"
              value={getString(dns, 'final')}
              options={[
                { value: '', label: '— не задан —' },
                ...serverTags.map((tag) => ({ value: tag, label: tag })),
              ]}
              onValueChange={(value) => {
                setDns(setValue(dns, 'final', value))
              }}
            />
          </Field>
        </CardContent>
      </Card>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold">DNS-серверы (dns.servers)</h3>
        <p className="text-xs text-muted-foreground">
          Порядок серверов — это порядок опроса: первый сервер, ответивший первым, и выигрывает.
        </p>
        <ResourceListEditor
          specs={DNS_SERVER_TYPES}
          items={servers}
          onChange={(next) => {
            setDns(setValue(dns, 'servers', next))
          }}
          idPrefix="dns-server"
          tagPrefix="dns"
          addLabel="Добавить сервер"
          emptyTitle="DNS-серверы не заданы"
          emptyDescription="Без своего DNS sing-box использует системный резолвер."
          defaultType="udp"
        />
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="text-sm font-semibold">Правила DNS ({rules.length})</h3>
          <Button
            type="button"
            size="sm"
            onClick={() => {
              setRules([...rules, { server: serverTags[0] ?? '' }])
            }}
          >
            Добавить правило DNS
          </Button>
        </div>

        {rules.length === 0 ? (
          <Alert tone="info" title="Правил DNS нет">
            Все запросы разрешаются серверами выше.
          </Alert>
        ) : (
          <ul className="space-y-3">
            {rules.map((rule, index) => (
              <li key={index}>
                <Card>
                  <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">#{index + 1}</span>
                      <code className="text-xs">{getString(rule, 'server') || '—'}</code>
                    </div>
                    <div className="flex items-center gap-1">
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Удалить правило DNS ${index + 1}`}
                        onClick={() => {
                          setRules(rules.filter((_rule, position) => position !== index))
                        }}
                      >
                        Удалить
                      </Button>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    <SpecFields
                      specs={ruleFields}
                      parent={rule}
                      idPrefix={`dns-rule-${index}`}
                      onChange={(next) => {
                        setRules(rules.map((item, position) => (position === index ? next : item)))
                      }}
                    />
                    <TagMultiSelect
                      id={`dns-rule-${index}-rule-set`}
                      label="rule_set"
                      hint="Наборы правил объявлены на вкладке Route (route.rule_set)."
                      values={getStringList(rule, 'rule_set')}
                      options={ruleSetTags}
                      onChange={(next) => {
                        setRules(
                          rules.map((item, position) =>
                            position === index ? setList(item, 'rule_set', next) : item,
                          ),
                        )
                      }}
                    />
                  </CardContent>
                </Card>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
