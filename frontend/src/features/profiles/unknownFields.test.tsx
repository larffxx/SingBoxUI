/**
 * Unknown sing-box options survive structured edits (spec §36, §85).
 *
 * The structured tabs are a view over one source of truth — the raw JSON
 * document — so an option this UI has never heard of must come through a known
 * neighbour's edit untouched. Without that guarantee, opening a tab and touching
 * a single control would silently strip every option the editor does not model,
 * and `sing-box check` would reject a configuration the user never edited.
 *
 * The tabs are driven the way the workspace drives them: the document lives in
 * state, each tab reports the next document, and the assertion runs on what was
 * reported. The fixtures carry unknown options on every level — a sibling of a
 * known nested block, a sibling of a known field, and an option nested inside a
 * block the specs do not model at all.
 */
import { render, screen } from '@testing-library/react'
import * as React from 'react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { listToPairs, pairsToList, type JsonObject } from './jsonDoc'
import { ExperimentalTab } from './tabs/ExperimentalTab'
import { OutboundsTab } from './tabs/OutboundsTab'

// The share dialogs are the only part of the outbounds tab that reaches for the
// Wails bindings, and they stay closed here.
vi.mock('@/features/share', () => ({
  SharePasteDialog: () => null,
  ShareExportDialog: () => null,
}))

/** A document with options no editor models, at every level it can appear. */
const ROOT: JsonObject = {
  log: { level: 'info', output: 'singboxui.log', timestamp: true },
  experimental: {
    cache_file: { enabled: false, store_fakeip: true, store_rdrc: true, cache_id: 'shared' },
    clash_api: { enabled: true, external_controller: '127.0.0.1:9090', secret: 'local' },
    v2ray_api: { listen: '127.0.0.1:8080', stats: { enabled: true } },
    cache_l1: { enabled: true, size: 4096 },
  },
  outbounds: [
    {
      type: 'vless',
      tag: 'proxy',
      server: 'example.com',
      server_port: 443,
      uuid: '00000000-0000-0000-0000-000000000000',
      // Unknown to the specs:
      tcp_fast_open: true,
      domain_strategy: 'prefer_ipv4',
      tls: {
        enabled: true,
        utls: { enabled: true, fingerprint: 'chrome' },
        reality: { enabled: true, public_key: 'public-key' },
      },
    },
  ],
}

/**
 * The harnesses hold the document the way the workspace does: rendering a tab
 * with a frozen `root` and no state would fight React's controlled inputs.
 */
function ExperimentalHarness({ report }: { report: (next: JsonObject) => void }) {
  const [root, onChange] = React.useState(ROOT)
  return (
    <ExperimentalTab
      root={root}
      onChange={(next) => {
        onChange(next)
        report(next)
      }}
    />
  )
}

function OutboundsHarness({ report }: { report: (next: JsonObject) => void }) {
  const [root, onChange] = React.useState(ROOT)
  return (
    <OutboundsTab
      root={root}
      onChange={(next) => {
        onChange(next)
        report(next)
      }}
    />
  )
}

function lastCall(onChange: ReturnType<typeof vi.fn>): JsonObject {
  const calls = onChange.mock.calls
  const last = calls[calls.length - 1]
  if (last === undefined) throw new Error('onChange was never called')
  return last[0] as JsonObject
}

describe('structured editors preserve unknown sing-box options', () => {
  it('keeps unknown options when a known nested switch is toggled', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<ExperimentalHarness report={onChange} />)

    await user.click(screen.getByRole('switch', { name: 'Включить файловый кэш' }))

    const experimental = lastCall(onChange)['experimental'] as JsonObject

    // The edited block keeps the options the specs never mention.
    expect(experimental['cache_file']).toEqual({
      enabled: true,
      store_fakeip: true,
      store_rdrc: true,
      cache_id: 'shared',
    })
  })

  it('leaves blocks the user never touched byte-identical', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<ExperimentalHarness report={onChange} />)

    await user.click(screen.getByRole('switch', { name: 'Включить файловый кэш' }))

    const next = lastCall(onChange)
    const experimental = next['experimental'] as JsonObject
    const original = ROOT['experimental'] as JsonObject

    expect(experimental['clash_api']).toEqual(original['clash_api'])
    expect(experimental['v2ray_api']).toEqual(original['v2ray_api'])
    expect(experimental['cache_l1']).toEqual(original['cache_l1'])
    expect(next['outbounds']).toEqual(ROOT['outbounds'])
    expect(next['log']).toEqual(ROOT['log'])
  })

  it('keeps unknown outbound options when a known one is edited', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<OutboundsHarness report={onChange} />)

    await user.click(screen.getByRole('button', { name: 'Развернуть proxy' }))
    await user.clear(screen.getByLabelText('Тег'))
    // Re-queried: clearing deletes the key, so the control is re-rendered.
    await user.type(screen.getByLabelText('Тег'), 'edge')

    const outbound = (lastCall(onChange)['outbounds'] as JsonObject[])[0] as JsonObject

    expect(outbound['tag']).toBe('edge')
    expect(outbound['server_port']).toBe(443)
    // Options the specs do not model, including a whole unmodelled sub-block.
    expect(outbound['tcp_fast_open']).toBe(true)
    expect(outbound['domain_strategy']).toBe('prefer_ipv4')
    expect(outbound['tls']).toEqual({
      enabled: true,
      utls: { enabled: true, fingerprint: 'chrome' },
      reality: { enabled: true, public_key: 'public-key' },
    })
  })

  it('keeps the neighbours of a text field while it is retyped', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<ExperimentalHarness report={onChange} />)

    // Queried by id: the label text is shared by more than one rendered node.
    const controller = document.getElementById('experimental-clash_api-external_controller')
    expect(controller).not.toBeNull()
    await user.clear(controller as HTMLElement)
    await user.type(controller as HTMLElement, '127.0.0.1:9091')

    const experimental = lastCall(onChange)['experimental'] as JsonObject
    expect((experimental['clash_api'] as JsonObject)['external_controller']).toBe('127.0.0.1:9091')
    expect((experimental['clash_api'] as JsonObject)['secret']).toBe('local')
    expect(experimental['cache_file']).toEqual((ROOT['experimental'] as JsonObject)['cache_file'])
  })

  // Known limitation, recorded rather than hidden: the `Key: value` editor works
  // on text lines, so a header value that is not a string comes back as its JSON
  // rendering in a string. Keys are never lost; values can change type.
  it('documents the headers editor widening non-string values to text', () => {
    expect(listToPairs(pairsToList({ 'X-Token': 'abc', 'X-Retry': 8 }))).toEqual({
      'X-Token': 'abc',
      'X-Retry': '8',
    })
  })
})
