/**
 * The rules the applications card writes (ADR 011).
 *
 * These are pure functions over the configuration document, so the behaviour
 * that decides what lands in a profile is covered without a webview: an
 * application must survive being switched between VPN and direct, and a rule
 * the user extended by hand must not lose its other conditions.
 */
import { describe, expect, it } from 'vitest'

import { appRulesCount, appRuleTarget, setAppRoute } from './appRules'
import type { JsonObject } from './jsonDoc'

const TELEGRAM = '^/Applications/Telegram\\.app/'
const DISCORD = '^/Applications/Discord\\.app/'

function rules(...items: JsonObject[]): JsonObject[] {
  return items
}

describe('appRuleTarget', () => {
  it('reports an application without a rule as unset', () => {
    expect(appRuleTarget(rules({ action: 'route', domain_suffix: ['.local'] }), TELEGRAM)).toBe(
      'off',
    )
  })

  it('reports the outbound an application is routed to', () => {
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'proxy', process_path_regex: [TELEGRAM] }),
        TELEGRAM,
      ),
    ).toBe('proxy')
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'direct', process_path_regex: [TELEGRAM] }),
        TELEGRAM,
      ),
    ).toBe('direct')
  })

  it('does not mistake another action for a routing decision', () => {
    // A rule that rejects or sniffs is not "через VPN" and not "напрямую".
    expect(
      appRuleTarget(rules({ action: 'reject', process_path_regex: [TELEGRAM] }), TELEGRAM),
    ).toBe('off')
  })

  it('ignores a rule that selects another application', () => {
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'proxy', process_path_regex: [DISCORD] }),
        TELEGRAM,
      ),
    ).toBe('off')
  })
})

describe('setAppRoute', () => {
  it('appends the rule for an application that has none', () => {
    const initial = rules({ action: 'route', ip_is_private: true, outbound: 'direct' })

    const next = setAppRoute(initial, TELEGRAM, 'proxy', 'proxy-vless')

    expect(next).toHaveLength(2)
    expect(next[1]).toEqual({
      action: 'route',
      outbound: 'proxy-vless',
      process_path_regex: [TELEGRAM],
    })
    // The existing rule keeps its priority: rules are evaluated in order.
    expect(next[0]).toEqual(initial[0])
    expect(initial).toHaveLength(1)
  })

  it('switches an application between the tunnel and the direct outbound', () => {
    const initial = rules({
      action: 'route',
      outbound: 'proxy-vless',
      process_path_regex: [TELEGRAM],
    })

    const direct = setAppRoute(initial, TELEGRAM, 'direct', 'proxy-vless')
    expect(direct).toEqual([
      { action: 'route', outbound: 'direct', process_path_regex: [TELEGRAM] },
    ])

    const back = setAppRoute(direct, TELEGRAM, 'proxy', 'proxy-vless')
    expect(back).toEqual([
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM] },
    ])
  })

  it('keeps the other conditions of a rule the user wrote by hand', () => {
    const initial = rules({
      action: 'route',
      outbound: 'direct',
      domain_suffix: ['.local'],
      process_path_regex: [TELEGRAM, DISCORD],
    })

    const next = setAppRoute(initial, TELEGRAM, 'off', 'proxy-vless')

    expect(next).toEqual([
      {
        action: 'route',
        outbound: 'direct',
        domain_suffix: ['.local'],
        process_path_regex: [DISCORD],
      },
    ])
  })

  it('removes a rule that only carried the application', () => {
    const initial = rules(
      { action: 'route', ip_is_private: true, outbound: 'direct' },
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM] },
    )

    const next = setAppRoute(initial, TELEGRAM, 'off', 'proxy-vless')

    expect(next).toEqual([{ action: 'route', ip_is_private: true, outbound: 'direct' }])
  })

  it('leaves a document without the application untouched', () => {
    const initial = rules({ action: 'route', outbound: 'direct' })

    expect(setAppRoute(initial, TELEGRAM, 'off', 'proxy-vless')).toEqual(initial)
  })
})

describe('appRulesCount', () => {
  it('counts the rules that select an application', () => {
    const list = rules(
      { action: 'route', outbound: 'direct' },
      { action: 'route', outbound: 'proxy', process_path_regex: [TELEGRAM] },
      { action: 'route', outbound: 'direct', process_path_regex: [DISCORD] },
    )

    expect(appRulesCount(list)).toBe(2)
  })
})
