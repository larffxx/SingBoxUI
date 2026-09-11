/**
 * Experimental block specs (spec §35).
 *
 * `experimental` is where sing-box hides its control surfaces: the on-disk cache,
 * the Clash API dashboard and the V2Ray stats API. Every block stays off unless
 * the user turns it on, because all three open a local port or write to disk.
 * Declared as data so the whole tab stays one schema table.
 */
import type { FieldSpec } from '../resourceSpecs'

export const EXPERIMENTAL_FIELDS: FieldSpec[] = [
  {
    key: 'cache_file',
    label: 'Файловый кэш (cache_file)',
    kind: 'nested',
    hint: 'Ускоряет запуск и сохраняет кэш DNS между перезапусками.',
    fields: [
      { key: 'enabled', label: 'Включить файловый кэш', kind: 'boolean' },
      { key: 'path', label: 'Путь к файлу', kind: 'text', placeholder: 'cache.db' },
      { key: 'store_fakeip', label: 'Сохранять fake-IP', kind: 'boolean' },
    ],
  },
  {
    key: 'clash_api',
    label: 'Clash API (clash_api)',
    kind: 'nested',
    hint: 'Локальный HTTP-интерфейс. Не выставляйте его в локальную сеть без secret.',
    fields: [
      { key: 'enabled', label: 'Включить Clash API', kind: 'boolean' },
      {
        key: 'external_controller',
        label: 'Адрес прослушивания',
        kind: 'text',
        placeholder: '127.0.0.1:9090',
      },
      { key: 'secret', label: 'Секрет', kind: 'secret' },
      {
        key: 'default_mode',
        label: 'Режим по умолчанию',
        kind: 'select',
        options: [
          { value: '', label: '— не задан —' },
          { value: 'rule', label: 'rule' },
          { value: 'global', label: 'global' },
          { value: 'direct', label: 'direct' },
        ],
      },
    ],
  },
  {
    key: 'v2ray_api',
    label: 'V2Ray API (v2ray_api)',
    kind: 'nested',
    hint: 'Нужен для статистики трафика по outbound. Открывает gRPC-порт.',
    fields: [
      { key: 'enabled', label: 'Включить V2Ray API', kind: 'boolean' },
      { key: 'listen', label: 'Адрес прослушивания', kind: 'text', placeholder: '127.0.0.1:8080' },
      {
        key: 'stats',
        label: 'Статистика (stats)',
        kind: 'nested',
        fields: [
          { key: 'enabled', label: 'Собирать статистику', kind: 'boolean' },
          { key: 'inbounds', label: 'Inbounds', kind: 'list' },
          { key: 'outbounds', label: 'Outbounds', kind: 'list' },
          { key: 'users', label: 'Пользователи', kind: 'list' },
        ],
      },
    ],
  },
]
