/**
 * DNS server specs (spec §35, §36).
 *
 * `dns.servers[]` — the transport is the `type`, exactly like outbounds. Declared
 * as data so the DNS tab stays a schema table and the component module exports
 * only the component.
 */
import type { ResourceTypeSpec } from '../resourceSpecs'

export const DNS_SERVER_TYPES: ResourceTypeSpec[] = [
  {
    type: 'udp',
    label: 'UDP (udp)',
    description: 'Классический DNS. По умолчанию используется системный резолвер.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'dns-udp' },
      { key: 'server', label: 'Адрес сервера', kind: 'text', placeholder: '1.1.1.1' },
      { key: 'server_port', label: 'Порт', kind: 'number', placeholder: '53' },
      { key: 'detour', label: 'Detour', kind: 'text', placeholder: 'direct' },
      {
        key: 'strategy',
        label: 'Стратегия',
        kind: 'select',
        options: [
          { value: '', label: '— по умолчанию —' },
          { value: 'prefer_ipv4', label: 'prefer_ipv4' },
          { value: 'prefer_ipv6', label: 'prefer_ipv6' },
          { value: 'ipv4_only', label: 'ipv4_only' },
          { value: 'ipv6_only', label: 'ipv6_only' },
        ],
      },
    ],
    defaults: { type: 'udp', tag: 'dns-udp', server: '1.1.1.1' },
  },
  {
    type: 'tls',
    label: 'DNS over TLS (tls)',
    description: 'Шифрованный DNS на порту 853.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'dns-tls' },
      { key: 'server', label: 'Адрес сервера', kind: 'text', placeholder: '1.1.1.1' },
      { key: 'server_port', label: 'Порт', kind: 'number', placeholder: '853' },
      { key: 'detour', label: 'Detour', kind: 'text', placeholder: 'direct' },
      {
        key: 'strategy',
        label: 'Стратегия',
        kind: 'select',
        options: [
          { value: '', label: '— по умолчанию —' },
          { value: 'prefer_ipv4', label: 'prefer_ipv4' },
          { value: 'prefer_ipv6', label: 'prefer_ipv6' },
          { value: 'ipv4_only', label: 'ipv4_only' },
          { value: 'ipv6_only', label: 'ipv6_only' },
        ],
      },
    ],
    defaults: { type: 'tls', tag: 'dns-tls', server: '1.1.1.1', server_port: 853 },
    subForms: ['tls'],
  },
  {
    type: 'https',
    label: 'DNS over HTTPS (https)',
    description: 'DoH поверх HTTPS — работает там, где UDP:53 заблокирован.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'dns-https' },
      { key: 'server', label: 'Адрес сервера', kind: 'text', placeholder: '1.1.1.1' },
      { key: 'server_port', label: 'Порт', kind: 'number', placeholder: '443' },
      { key: 'path', label: 'Путь', kind: 'text', placeholder: '/dns-query' },
      { key: 'detour', label: 'Detour', kind: 'text', placeholder: 'direct' },
      {
        key: 'strategy',
        label: 'Стратегия',
        kind: 'select',
        options: [
          { value: '', label: '— по умолчанию —' },
          { value: 'prefer_ipv4', label: 'prefer_ipv4' },
          { value: 'prefer_ipv6', label: 'prefer_ipv6' },
          { value: 'ipv4_only', label: 'ipv4_only' },
          { value: 'ipv6_only', label: 'ipv6_only' },
        ],
      },
    ],
    defaults: {
      type: 'https',
      tag: 'dns-https',
      server: '1.1.1.1',
      server_port: 443,
      path: '/dns-query',
    },
    subForms: ['tls'],
  },
  {
    type: 'quic',
    label: 'DNS over QUIC (quic)',
    description: 'DoQ — минимальные задержки, требует UDP.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'dns-quic' },
      { key: 'server', label: 'Адрес сервера', kind: 'text', placeholder: '1.1.1.1' },
      { key: 'server_port', label: 'Порт', kind: 'number', placeholder: '853' },
      { key: 'detour', label: 'Detour', kind: 'text', placeholder: 'direct' },
    ],
    defaults: { type: 'quic', tag: 'dns-quic', server: '1.1.1.1', server_port: 853 },
    subForms: ['tls'],
  },
  {
    type: 'local',
    label: 'Системный резолвер (local)',
    description: 'Отдаёт запросы операционной системе.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'dns-local' },
      {
        key: 'strategy',
        label: 'Стратегия',
        kind: 'select',
        options: [
          { value: '', label: '— по умолчанию —' },
          { value: 'prefer_ipv4', label: 'prefer_ipv4' },
          { value: 'prefer_ipv6', label: 'prefer_ipv6' },
        ],
      },
    ],
    defaults: { type: 'local', tag: 'dns-local' },
  },
]
