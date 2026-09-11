/**
 * Per-type field schemas for the structured outbound / inbound / endpoint
 * editors (spec §6, §9, §35, §38).
 *
 * A schema entry is data, not JSX: `ResourceListEditor` and `SpecFields` render
 * whatever is declared here, so adding a new protocol touches only this file.
 * Keys mirror the sing-box JSON exactly — they are written straight into the
 * configuration document.
 */
import type { JsonObject } from './jsonDoc'

export type FieldKind =
  'text' | 'number' | 'secret' | 'boolean' | 'select' | 'list' | 'pairs' | 'nested'

export interface FieldOption {
  value: string
  label: string
}

export interface FieldSpec {
  key: string
  label: string
  kind: FieldKind
  /** Optional help line rendered under the control. */
  hint?: string
  placeholder?: string
  /** For `select`; `''` means "not set" and removes the key. */
  options?: FieldOption[]
  /** For `nested`: the fields of the sub-object. */
  fields?: FieldSpec[]
}

export interface SubFormSpec {
  key: string
  label: string
  description: string
  fields: FieldSpec[]
}

export interface ResourceTypeSpec {
  type: string
  label: string
  description: string
  fields: FieldSpec[]
  /** Values pre-filled when a new item of this type is added. */
  defaults: JsonObject
  /** Sub-forms (TLS / transport / multiplex) that apply to this type. */
  subForms?: string[]
}

const NOT_SET: FieldOption = { value: '', label: '— не задано —' }

function selectOptions(...options: string[]): FieldOption[] {
  return [NOT_SET, ...options.map((value) => ({ value, label: value }))]
}

const TAG: FieldSpec = {
  key: 'tag',
  label: 'Тег',
  kind: 'text',
  placeholder: 'proxy',
  hint: 'Уникальное имя, по которому правило маршрутизации выбирает этот прокси.',
}

const SERVER: FieldSpec = {
  key: 'server',
  label: 'Сервер',
  kind: 'text',
  placeholder: 'example.com',
}
const PORT: FieldSpec = {
  key: 'server_port',
  label: 'Порт',
  kind: 'number',
  placeholder: '443',
}

/** TLS sub-form — shared by outbounds and TLS-capable inbounds (spec §38). */
export const TLS_FORM: SubFormSpec = {
  key: 'tls',
  label: 'TLS',
  description: 'Шифрование транспорта, проверка сертификата и маскировка TLS-отпечатка.',
  fields: [
    {
      key: 'enabled',
      label: 'Включено',
      kind: 'boolean',
      hint: 'Для TLS-входящих sing-box требует явного enabled: true.',
    },
    { key: 'server_name', label: 'SNI (server_name)', kind: 'text', placeholder: 'example.com' },
    {
      key: 'insecure',
      label: 'Не проверять сертификат',
      kind: 'boolean',
      hint: 'Отключает проверку цепочки — снижает защиту от подмены сервера.',
    },
    {
      key: 'alpn',
      label: 'ALPN',
      kind: 'list',
      hint: 'Одно значение в строке, например h2 или http/1.1.',
    },
    {
      key: 'min_version',
      label: 'Минимальная версия TLS',
      kind: 'select',
      options: selectOptions('1.0', '1.1', '1.2', '1.3'),
    },
    {
      key: 'max_version',
      label: 'Максимальная версия TLS',
      kind: 'select',
      options: selectOptions('1.0', '1.1', '1.2', '1.3'),
    },
    { key: 'certificate_path', label: 'Путь к сертификату', kind: 'text' },
    { key: 'key_path', label: 'Путь к ключу', kind: 'text' },
    { key: 'client_certificate', label: 'Клиентский сертификат', kind: 'text' },
    { key: 'client_key', label: 'Клиентский ключ', kind: 'text' },
    {
      key: 'utls',
      label: 'uTLS',
      kind: 'nested',
      fields: [
        { key: 'enabled', label: 'Включено', kind: 'boolean' },
        {
          key: 'fingerprint',
          label: 'Отпечаток',
          kind: 'select',
          options: selectOptions(
            'chrome',
            'firefox',
            'safari',
            'ios',
            'android',
            'edge',
            'random',
            'randomized',
          ),
        },
      ],
    },
    {
      key: 'reality',
      label: 'Reality',
      kind: 'nested',
      fields: [
        { key: 'enabled', label: 'Включено', kind: 'boolean' },
        { key: 'public_key', label: 'Публичный ключ', kind: 'text' },
        { key: 'short_id', label: 'Short ID', kind: 'text' },
        { key: 'private_key', label: 'Приватный ключ (сервер)', kind: 'secret' },
        { key: 'handshake', label: 'Handshake-сервер', kind: 'list' },
      ],
    },
  ],
}

/** Transport sub-form (v2ray-style plugins) — outbounds only. */
export const TRANSPORT_FORM: SubFormSpec = {
  key: 'transport',
  label: 'Транспорт',
  description: 'Транспорт поверх TLS: WebSocket, gRPC, HTTP/2, QUIC.',
  fields: [
    {
      key: 'type',
      label: 'Тип',
      kind: 'select',
      options: selectOptions('http', 'ws', 'grpc', 'quic', 'httpupgrade'),
    },
    { key: 'host', label: 'Host', kind: 'text', placeholder: 'example.com' },
    { key: 'path', label: 'Path', kind: 'text', placeholder: '/ws' },
    { key: 'method', label: 'Метод (HTTP)', kind: 'text' },
    {
      key: 'headers',
      label: 'Заголовки',
      kind: 'pairs',
      hint: 'Формат: Key: Value, по одному в строке.',
    },
    { key: 'service_name', label: 'Service name (gRPC)', kind: 'text' },
    { key: 'idle_timeout', label: 'Idle timeout', kind: 'text', placeholder: '15s' },
    { key: 'ping_timeout', label: 'Ping timeout', kind: 'text' },
    { key: 'max_early_data', label: 'Max early data', kind: 'number' },
    { key: 'early_data_header_name', label: 'Early data header', kind: 'text' },
  ],
}

/** Multiplex sub-form — outbounds only. */
export const MULTIPLEX_FORM: SubFormSpec = {
  key: 'multiplex',
  label: 'Мультиплексирование',
  description: 'Несколько потоков через одно соединение (smux/yamux/h2mux или Brutal).',
  fields: [
    { key: 'enabled', label: 'Включено', kind: 'boolean' },
    {
      key: 'protocol',
      label: 'Протокол',
      kind: 'select',
      options: selectOptions('h2mux', 'smux', 'yamux', 'h3'),
    },
    { key: 'max_streams', label: 'Максимум потоков', kind: 'number' },
    { key: 'padding', label: 'Padding', kind: 'boolean' },
    {
      key: 'brutal',
      label: 'Brutal',
      kind: 'nested',
      fields: [
        { key: 'enabled', label: 'Включено', kind: 'boolean' },
        { key: 'up_mbps', label: 'Исходящая, Мбит/с', kind: 'number' },
        { key: 'down_mbps', label: 'Входящая, Мбит/с', kind: 'number' },
      ],
    },
  ],
}

export const OUTBOUND_SUB_FORMS = [TLS_FORM, TRANSPORT_FORM, MULTIPLEX_FORM]
export const INBOUND_SUB_FORMS = [TLS_FORM]

/** Outbound protocols with per-type dynamic fields (spec §6, §35). */
export const OUTBOUND_TYPES: ResourceTypeSpec[] = [
  {
    type: 'vless',
    label: 'VLESS',
    description: 'Лёгкий протокол без собственного шифрования — используйте вместе с TLS.',
    subForms: ['tls', 'transport', 'multiplex'],
    defaults: { server_port: 443 },
    fields: [
      TAG,
      SERVER,
      PORT,
      {
        key: 'uuid',
        label: 'UUID',
        kind: 'secret',
        hint: 'Идентификатор пользователя из ссылки vless://.',
      },
      {
        key: 'flow',
        label: 'Flow',
        kind: 'select',
        options: selectOptions('xtls-rprx-vision', 'xtls-rprx-vision-udp443'),
      },
      {
        key: 'packet_encoding',
        label: 'Packet encoding',
        kind: 'select',
        options: selectOptions('xudp', 'packetaddr'),
      },
    ],
  },
  {
    type: 'vmess',
    label: 'VMess',
    description: 'Классический протокол V2Ray со своим шифрованием.',
    subForms: ['tls', 'transport', 'multiplex'],
    defaults: { server_port: 443, security: 'auto', alter_id: 0 },
    fields: [
      TAG,
      SERVER,
      PORT,
      { key: 'uuid', label: 'UUID', kind: 'secret' },
      {
        key: 'security',
        label: 'Шифрование',
        kind: 'select',
        options: selectOptions('auto', 'none', 'zero', 'aes-128-gcm', 'chacha20-poly1305'),
      },
      { key: 'alter_id', label: 'Alter ID', kind: 'number' },
      { key: 'global_padding', label: 'Global padding', kind: 'boolean' },
      { key: 'authenticated_length', label: 'Authenticated length', kind: 'boolean' },
    ],
  },
  {
    type: 'trojan',
    label: 'Trojan',
    description: 'Маскируется под обычный HTTPS-трафик, требует TLS.',
    subForms: ['tls', 'transport', 'multiplex'],
    defaults: { server_port: 443 },
    fields: [TAG, SERVER, PORT, { key: 'password', label: 'Пароль', kind: 'secret' }],
  },
  {
    type: 'shadowsocks',
    label: 'Shadowsocks',
    description: 'Парольный шифр, опционально с плагином.',
    subForms: ['multiplex'],
    defaults: { server_port: 8388, method: '2022-blake3-aes-128-gcm' },
    fields: [
      TAG,
      SERVER,
      PORT,
      {
        key: 'method',
        label: 'Метод',
        kind: 'select',
        options: selectOptions(
          '2022-blake3-aes-128-gcm',
          '2022-blake3-aes-256-gcm',
          '2022-blake3-chacha20-poly1305',
          'aes-128-gcm',
          'aes-256-gcm',
          'chacha20-ietf-poly1305',
          'xchacha20-ietf-poly1305',
          'none',
        ),
      },
      { key: 'password', label: 'Пароль', kind: 'secret' },
      { key: 'plugin', label: 'Плагин', kind: 'text', placeholder: 'obfs-local' },
      { key: 'plugin_opts', label: 'Опции плагина', kind: 'text' },
      {
        key: 'udp_over_tcp',
        label: 'UDP over TCP',
        kind: 'nested',
        fields: [
          { key: 'enabled', label: 'Включено', kind: 'boolean' },
          { key: 'version', label: 'Версия', kind: 'number' },
        ],
      },
    ],
  },
  {
    type: 'hysteria2',
    label: 'Hysteria2',
    description: 'QUIC-протокол, устойчивый к потерям пакетов.',
    subForms: ['tls'],
    defaults: { server_port: 443 },
    fields: [
      TAG,
      SERVER,
      PORT,
      { key: 'password', label: 'Пароль', kind: 'secret' },
      { key: 'obfs', label: 'Обфускация', kind: 'select', options: selectOptions('salamander') },
      { key: 'obfs_password', label: 'Пароль обфускации', kind: 'secret' },
      { key: 'up_mbps', label: 'Исходящая, Мбит/с', kind: 'number' },
      { key: 'down_mbps', label: 'Входящая, Мбит/с', kind: 'number' },
      { key: 'network', label: 'Сеть', kind: 'select', options: selectOptions('tcp', 'udp') },
    ],
  },
  {
    type: 'tuic',
    label: 'TUIC',
    description: 'QUIC-протокол с управлением перегрузкой.',
    subForms: ['tls'],
    defaults: { server_port: 443 },
    fields: [
      TAG,
      SERVER,
      PORT,
      { key: 'uuid', label: 'UUID', kind: 'secret' },
      { key: 'password', label: 'Пароль', kind: 'secret' },
      {
        key: 'congestion_control',
        label: 'Congestion control',
        kind: 'select',
        options: selectOptions('cubic', 'new_reno', 'bbr'),
      },
      {
        key: 'udp_relay_mode',
        label: 'UDP relay mode',
        kind: 'select',
        options: selectOptions('native', 'quic'),
      },
      { key: 'zero_rtt_handshake', label: '0-RTT handshake', kind: 'boolean' },
      { key: 'heartbeat', label: 'Heartbeat', kind: 'text', placeholder: '10s' },
    ],
  },
  {
    type: 'wireguard',
    label: 'WireGuard',
    description: 'Встроенный WireGuard-клиент (sing-box ≥ 1.11).',
    defaults: { server_port: 51820, mtu: 1408 },
    fields: [
      TAG,
      SERVER,
      PORT,
      { key: 'private_key', label: 'Приватный ключ', kind: 'secret' },
      { key: 'peer_public_key', label: 'Публичный ключ пира', kind: 'secret' },
      { key: 'pre_shared_key', label: 'Pre-shared key', kind: 'secret' },
      {
        key: 'local_address',
        label: 'Локальные адреса',
        kind: 'list',
        hint: 'Один CIDR в строке, например 10.0.0.2/32.',
      },
      { key: 'mtu', label: 'MTU', kind: 'number' },
      { key: 'reserved', label: 'Reserved', kind: 'list' },
    ],
  },
  {
    type: 'selector',
    label: 'Selector',
    description: 'Ручной выбор одного из перечисленных прокси.',
    defaults: {},
    fields: [
      TAG,
      {
        key: 'outbounds',
        label: 'Состав',
        kind: 'list',
        hint: 'Теги других прокси, по одному в строке.',
      },
      { key: 'default', label: 'По умолчанию', kind: 'text' },
      { key: 'interrupt_exist_connections', label: 'Обрывать текущие соединения', kind: 'boolean' },
    ],
  },
  {
    type: 'urltest',
    label: 'URLTest',
    description: 'Автоматически выбирает самый быстрый прокси из состава.',
    defaults: { url: 'https://www.gstatic.com/generate_204', interval: '3m', tolerance: 50 },
    fields: [
      TAG,
      {
        key: 'outbounds',
        label: 'Состав',
        kind: 'list',
        hint: 'Теги других прокси, по одному в строке.',
      },
      { key: 'url', label: 'Проверочный URL', kind: 'text' },
      { key: 'interval', label: 'Интервал', kind: 'text', placeholder: '3m' },
      { key: 'tolerance', label: 'Допуск, мс', kind: 'number' },
      { key: 'idle_timeout', label: 'Idle timeout', kind: 'text', placeholder: '30m' },
      { key: 'interrupt_exist_connections', label: 'Обрывать текущие соединения', kind: 'boolean' },
    ],
  },
  {
    type: 'socks',
    label: 'SOCKS (исходящий)',
    description: 'Выход через внешний SOCKS-прокси.',
    defaults: { server_port: 1080 },
    fields: [
      TAG,
      SERVER,
      PORT,
      { key: 'version', label: 'Версия', kind: 'select', options: selectOptions('4', '4a', '5') },
      { key: 'username', label: 'Пользователь', kind: 'text' },
      { key: 'password', label: 'Пароль', kind: 'secret' },
    ],
  },
  {
    type: 'http',
    label: 'HTTP (исходящий)',
    description: 'Выход через внешний HTTP(S)-прокси.',
    defaults: { server_port: 8080 },
    fields: [
      TAG,
      SERVER,
      PORT,
      { key: 'username', label: 'Пользователь', kind: 'text' },
      { key: 'password', label: 'Пароль', kind: 'secret' },
      { key: 'path', label: 'Path', kind: 'text' },
      { key: 'headers', label: 'Заголовки', kind: 'pairs' },
    ],
  },
  {
    type: 'direct',
    label: 'Direct',
    description: 'Прямое подключение без прокси.',
    defaults: {},
    fields: [
      TAG,
      { key: 'override_address', label: 'Переопределить адрес', kind: 'text' },
      { key: 'override_port', label: 'Переопределить порт', kind: 'number' },
    ],
  },
]

/** Inbound protocols with per-type dynamic fields (spec §9, §35). */
export const INBOUND_TYPES: ResourceTypeSpec[] = [
  {
    type: 'tun',
    label: 'TUN',
    description: 'Виртуальный сетевой интерфейс — основной режим прозрачного проксирования.',
    subForms: ['tls'],
    defaults: { address: ['172.19.0.1/30'], auto_route: true, strict_route: true, stack: 'system' },
    fields: [
      TAG,
      { key: 'interface_name', label: 'Имя интерфейса', kind: 'text', placeholder: 'singbox-tun' },
      { key: 'address', label: 'Адреса', kind: 'list', hint: 'Один CIDR в строке.' },
      { key: 'mtu', label: 'MTU', kind: 'number' },
      {
        key: 'stack',
        label: 'Стек',
        kind: 'select',
        options: selectOptions('system', 'gvisor', 'mixed'),
      },
      { key: 'auto_route', label: 'Автомаршрутизация', kind: 'boolean' },
      { key: 'strict_route', label: 'Strict route', kind: 'boolean' },
      { key: 'auto_redirect', label: 'Auto redirect (Linux)', kind: 'boolean' },
      { key: 'endpoint_independent_nat', label: 'Endpoint independent NAT', kind: 'boolean' },
      { key: 'sniff', label: 'Перехват протокола', kind: 'boolean' },
      { key: 'sniff_override_destination', label: 'Подменять адрес по sniff', kind: 'boolean' },
      {
        key: 'domain_strategy',
        label: 'Стратегия домена',
        kind: 'select',
        options: selectOptions('prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only'),
      },
      { key: 'include_interface', label: 'Include interface', kind: 'list' },
      { key: 'exclude_interface', label: 'Exclude interface', kind: 'list' },
      { key: 'detour', label: 'Detour (тег исходящего)', kind: 'text' },
    ],
  },
  {
    type: 'http',
    label: 'HTTP',
    description: 'HTTP-прокси для локальных клиентов.',
    subForms: ['tls'],
    defaults: { listen: '127.0.0.1', listen_port: 2080 },
    fields: [
      TAG,
      { key: 'listen', label: 'Адрес прослушивания', kind: 'text', placeholder: '127.0.0.1' },
      { key: 'listen_port', label: 'Порт', kind: 'number' },
      { key: 'users', label: 'Пользователи', kind: 'list', hint: 'Формат: пользователь:пароль.' },
      { key: 'detour', label: 'Detour (тег исходящего)', kind: 'text' },
    ],
  },
  {
    type: 'socks',
    label: 'SOCKS',
    description: 'SOCKS4/5-прокси для локальных клиентов.',
    subForms: ['tls'],
    defaults: { listen: '127.0.0.1', listen_port: 2080 },
    fields: [
      TAG,
      { key: 'listen', label: 'Адрес прослушивания', kind: 'text' },
      { key: 'listen_port', label: 'Порт', kind: 'number' },
      { key: 'users', label: 'Пользователи', kind: 'list', hint: 'Формат: пользователь:пароль.' },
      {
        key: 'udp_override_dialer',
        label: 'UDP override dialer',
        kind: 'nested',
        fields: [{ key: 'enabled', label: 'Включено', kind: 'boolean' }],
      },
      { key: 'detour', label: 'Detour (тег исходящего)', kind: 'text' },
    ],
  },
  {
    type: 'mixed',
    label: 'Mixed',
    description: 'SOCKS и HTTP на одном порту — удобно для локальных приложений.',
    subForms: ['tls'],
    defaults: { listen: '127.0.0.1', listen_port: 2080 },
    fields: [
      TAG,
      { key: 'listen', label: 'Адрес прослушивания', kind: 'text' },
      { key: 'listen_port', label: 'Порт', kind: 'number' },
      { key: 'users', label: 'Пользователи', kind: 'list', hint: 'Формат: пользователь:пароль.' },
      { key: 'set_system_proxy', label: 'Включить системный прокси', kind: 'boolean' },
      { key: 'detour', label: 'Detour (тег исходящего)', kind: 'text' },
    ],
  },
]

/** Endpoint types (sing-box ≥ 1.11) — a third list next to inbounds/outbounds. */
export const ENDPOINT_TYPES: ResourceTypeSpec[] = [
  {
    type: 'wireguard',
    label: 'WireGuard',
    description: 'WireGuard как самостоятельная точка входа.',
    defaults: { mtu: 1408, address: ['10.0.0.2/32'] },
    fields: [
      TAG,
      { key: 'system', label: 'Системный интерфейс', kind: 'boolean' },
      { key: 'mtu', label: 'MTU', kind: 'number' },
      { key: 'address', label: 'Адреса', kind: 'list' },
      { key: 'private_key', label: 'Приватный ключ', kind: 'secret' },
      { key: 'listen_port', label: 'Порт прослушивания', kind: 'number' },
      {
        key: 'peer',
        label: 'Пир',
        kind: 'nested',
        fields: [
          { key: 'address', label: 'Адрес', kind: 'text' },
          { key: 'port', label: 'Порт', kind: 'number' },
          { key: 'public_key', label: 'Публичный ключ', kind: 'secret' },
          { key: 'pre_shared_key', label: 'Pre-shared key', kind: 'secret' },
          { key: 'allowed_ips', label: 'Allowed IPs', kind: 'list' },
          { key: 'persistent_keepalive_interval', label: 'Keepalive, с', kind: 'number' },
        ],
      },
    ],
  },
  {
    type: 'tailscale',
    label: 'Tailscale',
    description: 'Подключение к tailnet через встроенный клиент.',
    defaults: { state_directory: 'tailscale' },
    fields: [
      TAG,
      { key: 'state_directory', label: 'Каталог состояния', kind: 'text' },
      { key: 'auth_key', label: 'Ключ авторизации', kind: 'secret' },
      { key: 'control_url', label: 'Control URL', kind: 'text' },
      { key: 'ephemeral', label: 'Ephemeral', kind: 'boolean' },
      { key: 'hostname', label: 'Имя узла', kind: 'text' },
      { key: 'exit_node', label: 'Exit node', kind: 'text' },
      { key: 'advertise_routes', label: 'Объявляемые маршруты', kind: 'list' },
      { key: 'advertise_exit_node', label: 'Объявлять как exit node', kind: 'boolean' },
      { key: 'accept_routes', label: 'Принимать маршруты', kind: 'boolean' },
    ],
  },
]

export const ALL_SUB_FORMS: SubFormSpec[] = [TLS_FORM, TRANSPORT_FORM, MULTIPLEX_FORM]

export function subFormsFor(spec: ResourceTypeSpec): SubFormSpec[] {
  const keys = spec.subForms ?? []
  return ALL_SUB_FORMS.filter((form) => keys.includes(form.key))
}

export function findTypeSpec(
  specs: ResourceTypeSpec[],
  type: string,
): ResourceTypeSpec | undefined {
  return specs.find((spec) => spec.type === type)
}

/** Router strategies offered as presets in the DNS tab. */
export interface DnsPreset {
  id: string
  label: string
  description: string
  servers: JsonObject[]
  strategy: string
}

export const DNS_PRESETS: DnsPreset[] = [
  {
    id: 'cloudflare',
    label: 'Cloudflare DoH',
    description: '1.1.1.1 и 1.0.0.1 по HTTPS.',
    strategy: 'prefer_ipv4',
    servers: [
      { tag: 'dns-cf', address: 'https://1.1.1.1/dns-query', detour: 'direct' },
      { tag: 'dns-cf-alt', address: 'https://1.0.0.1/dns-query', detour: 'direct' },
    ],
  },
  {
    id: 'google',
    label: 'Google DoH',
    description: '8.8.8.8 и 8.8.4.4 по HTTPS.',
    strategy: 'prefer_ipv4',
    servers: [
      { tag: 'dns-google', address: 'https://8.8.8.8/dns-query', detour: 'direct' },
      { tag: 'dns-google-alt', address: 'https://8.8.4.4/dns-query', detour: 'direct' },
    ],
  },
  {
    id: 'quad9',
    label: 'Quad9 DoT',
    description: 'DNS-over-TLS с блокировкой вредоносных доменов.',
    strategy: 'prefer_ipv4',
    servers: [{ tag: 'dns-quad9', address: 'tls://9.9.9.9', detour: 'direct' }],
  },
  {
    id: 'alidns',
    label: 'AliDNS',
    description: 'Быстрые DNS-серверы для региона CN.',
    strategy: 'prefer_ipv4',
    servers: [
      { tag: 'dns-ali', address: 'https://223.5.5.5/dns-query', detour: 'direct' },
      { tag: 'dns-ali-alt', address: 'https://223.6.6.6/dns-query', detour: 'direct' },
    ],
  },
  {
    id: 'system',
    label: 'Системный DNS',
    description: 'Резолвер операционной системы.',
    strategy: 'prefer_ipv4',
    servers: [{ tag: 'dns-local', address: 'local' }],
  },
]

/** Route presets for the "быстрые правила" actions in the Route tab. */
export interface RouteRulePreset {
  id: string
  label: string
  description: string
  rule: JsonObject
}

export const ROUTE_RULE_PRESETS: RouteRulePreset[] = [
  {
    id: 'private-direct',
    label: 'Локальные сети напрямую',
    description: 'Приватные диапазоны и localhost не идут через прокси.',
    rule: {
      action: 'route',
      outbound: 'direct',
      ip_is_private: true,
    },
  },
  {
    id: 'dns-hijack',
    label: 'Перехват DNS',
    description: 'DNS-запросы обрабатываются самим sing-box.',
    rule: { action: 'hijack-dns', protocol: 'dns' },
  },
  {
    id: 'quic-reject',
    label: 'Отклонять QUIC',
    description: 'Убирает проблемы с UDP-блокировками QUIC.',
    rule: { action: 'reject', protocol: 'quic' },
  },
  {
    id: 'ru-direct',
    label: 'Домены региона — напрямую',
    description: 'Заготовка для доменов и IP региона.',
    rule: { action: 'route', outbound: 'direct', domain_suffix: ['.ru', '.рф'] },
  },
]
