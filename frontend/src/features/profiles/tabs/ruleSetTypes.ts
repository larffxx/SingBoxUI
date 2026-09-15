/**
 * Rule-set specs (spec §35, §36).
 *
 * Rule-set declarations (`route.rule_set`) are declared once here and referenced
 * by tag from both the routing rules and the DNS rules. Declared as data so the
 * route tab stays a schema table and the component module exports only the
 * component.
 */
import type { ResourceTypeSpec } from '../resourceSpecs'

export const RULE_SET_TYPES: ResourceTypeSpec[] = [
  {
    type: 'local',
    label: 'Локальный набор (local)',
    description: 'Файл .srs или JSON рядом с конфигурацией — без обращений к сети.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'rs-local' },
      { key: 'path', label: 'Путь к файлу', kind: 'text', placeholder: 'ruleset/ads.srs' },
    ],
    defaults: { type: 'local', tag: 'rs-local' },
  },
  {
    type: 'remote',
    label: 'Удалённый набор (remote)',
    description: 'Набор скачивается по URL и кэшируется локально.',
    fields: [
      { key: 'tag', label: 'Тег', kind: 'text', placeholder: 'rs-remote' },
      { key: 'url', label: 'URL', kind: 'text', placeholder: 'https://example.com/rules.srs' },
      {
        key: 'format',
        label: 'Формат',
        kind: 'select',
        options: [
          { value: '', label: '— по расширению —' },
          { value: 'binary', label: 'binary (.srs)' },
          { value: 'source', label: 'source (.json)' },
        ],
      },
      {
        key: 'update_interval',
        label: 'Интервал обновления',
        kind: 'text',
        placeholder: '1d',
        hint: 'Формат длительности sing-box, например 12h или 7d.',
      },
      { key: 'download_detour', label: 'Detour для загрузки', kind: 'text', placeholder: 'direct' },
      { key: 'exclude_detour', label: 'Detour', kind: 'text', placeholder: 'direct' },
    ],
    defaults: { type: 'remote', tag: 'rs-remote', format: 'binary' },
  },
]
