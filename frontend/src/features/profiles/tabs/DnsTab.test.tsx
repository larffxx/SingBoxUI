/**
 * DNS presets have to produce a block the managed sing-box accepts.
 *
 * sing-box 1.14 removed the legacy `address` server form, so a preset built
 * from it hands the user a profile that refuses to start — and the reason stays
 * invisible unless the app reports the decoder output (spec §37, §38).
 *
 * The second property covers the other half of the same failure: a
 * configuration that declares DNS servers but no `route.default_domain_resolver`
 * is refused by sing-box 1.14 as well, because dials that need a domain have no
 * resolver to use.
 */
import { describe, expect, it } from 'vitest'

import { getArray, getObject, getString, type JsonObject } from '../jsonDoc'
import { DNS_PRESETS, type DnsPreset } from '../resourceSpecs'
import { applyDnsPreset } from './dnsPreset'

/** requirePreset fails loudly instead of indexing into the preset list. */
function requirePreset(id: string): DnsPreset {
  const preset = DNS_PRESETS.find((candidate) => candidate.id === id)
  if (!preset) {
    throw new Error(`the ${id} preset is missing`)
  }
  return preset
}

/** requireFirst is the checked read of the first entry of a written list. */
function requireFirst(list: JsonObject[], what: string): JsonObject {
  const first = list[0]
  if (!first) {
    throw new Error(`no ${what} was written`)
  }
  return first
}

describe('DNS presets', () => {
  it('declares every server with the modern type/server form', () => {
    for (const preset of DNS_PRESETS) {
      expect(preset.servers.length, `${preset.id} declares no server`).toBeGreaterThan(0)
      for (const server of preset.servers) {
        expect(
          server.address,
          `${preset.id} still uses the legacy address form that sing-box 1.14 removed`,
        ).toBeUndefined()
        const type = getString(server, 'type')
        expect(['https', 'tls', 'local'], `${preset.id} server type`).toContain(type)
        if (type !== 'local') {
          expect(getString(server, 'tag'), `${preset.id} server tag`).not.toBe('')
          expect(getString(server, 'server'), `${preset.id} server address`).not.toBe('')
        }
      }
    }
  })

  it('replaces the whole dns block and names a resolver', () => {
    const preset = requirePreset('cloudflare')
    const root: JsonObject = { dns: { servers: [{ tag: 'old', address: 'local' }] }, outbounds: [] }

    const next = applyDnsPreset(root, preset)
    const servers = getArray(getObject(next, 'dns'), 'servers')

    expect(servers).toHaveLength(preset.servers.length)
    const declared = requireFirst(preset.servers, 'preset server')
    expect(getString(requireFirst(servers, 'dns server'), 'tag')).toBe(getString(declared, 'tag'))

    const resolver = getObject(getObject(next, 'route'), 'default_domain_resolver')
    expect(getString(resolver, 'server')).toBe(getString(declared, 'tag'))
  })

  it('keeps a resolver the user already chose', () => {
    const root: JsonObject = {
      route: { default_domain_resolver: { server: 'my-resolver' } },
    }

    const next = applyDnsPreset(root, requirePreset('google'))
    const resolver = getObject(getObject(next, 'route'), 'default_domain_resolver')

    expect(getString(resolver, 'server')).toBe('my-resolver')
  })

  it('never detours a server through the implicit direct outbound', () => {
    // sing-box 1.14 fails right after starting with "detour to an empty direct
    // outbound makes no sense" when a DNS server names `direct`, and `check`
    // does not catch it — the profile simply never comes up.
    for (const preset of DNS_PRESETS) {
      for (const server of preset.servers) {
        expect(getString(server, 'detour'), `${preset.id} sets a detour`).not.toBe('direct')
      }
    }
  })

  it('leaves the outbounds of the profile alone', () => {
    const root: JsonObject = { outbounds: [{ type: 'vless', tag: 'proxy' }] }

    expect(applyDnsPreset(root, requirePreset('cloudflare')).outbounds).toEqual(root.outbounds)
  })

  it('leaves the resolver alone for the system preset', () => {
    const next = applyDnsPreset({}, requirePreset('system'))

    expect(getObject(next, 'route').default_domain_resolver).toBeUndefined()
  })
})
