/**
 * The rules the applications card writes (ADR 011, ADR 013).
 *
 * These are pure functions over the configuration document, so the behaviour
 * that decides what lands in a profile is covered without a webview: an
 * application must survive being switched between VPN and direct, a rule the
 * user extended by hand must not lose its other conditions, and the condition
 * itself must be the one the platform declared — a path on macOS, a file name on
 * Windows.
 */
import { describe, expect, it } from 'vitest'

import {
  appRulesCount,
  appRuleTarget,
  conditionDescription,
  setAppRoute,
  type AppCondition,
} from './appRules'
import type { JsonObject } from './jsonDoc'

/** TELEGRAM is a macOS application: the backend selects it by its bundle path. */
const TELEGRAM: AppCondition = {
  key: 'process_path_regex',
  value: '^/Applications/Telegram\\\\.app/',
}

/** DISCORD is a second macOS application. */
const DISCORD: AppCondition = {
  key: 'process_path_regex',
  value: '^/Applications/Discord\\\\.app/',
}

/** CHROME is a Windows program: the backend selects it by its file name. */
const CHROME: AppCondition = { key: 'process_name', value: 'chrome.exe' }

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
        rules({ action: 'route', outbound: 'proxy', process_path_regex: [TELEGRAM.value] }),
        TELEGRAM,
      ),
    ).toBe('proxy')
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'direct', process_path_regex: [TELEGRAM.value] }),
        TELEGRAM,
      ),
    ).toBe('direct')
  })

  it('reads the condition of a Windows program out of the document', () => {
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'proxy', process_name: ['chrome.exe'] }),
        CHROME,
      ),
    ).toBe('proxy')
    // A rule under another process condition is not this program's rule.
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'proxy', process_path_regex: ['chrome.exe'] }),
        CHROME,
      ),
    ).toBe('off')
  })

  it('does not mistake another action for a routing decision', () => {
    // A rule that rejects or sniffs is not "через VPN" and not "напрямую".
    expect(
      appRuleTarget(rules({ action: 'reject', process_path_regex: [TELEGRAM.value] }), TELEGRAM),
    ).toBe('off')
  })

  it('ignores a rule that selects another application', () => {
    expect(
      appRuleTarget(
        rules({ action: 'route', outbound: 'proxy', process_path_regex: [DISCORD.value] }),
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
      process_path_regex: [TELEGRAM.value],
    })
    // The existing rule keeps its priority: rules are evaluated in order.
    expect(next[0]).toEqual(initial[0])
    expect(initial).toHaveLength(1)
  })

  it('writes the condition the platform declared', () => {
    // Windows: the rule selects the program by its file name, and the condition
    // comes from the backend rather than from this module (ADR 013).
    const next = setAppRoute(rules(), CHROME, 'proxy', 'proxy-vless')

    expect(next).toEqual([
      { action: 'route', outbound: 'proxy-vless', process_name: ['chrome.exe'] },
    ])
  })

  it('switches an application between the tunnel and the direct outbound', () => {
    const initial = rules({
      action: 'route',
      outbound: 'proxy-vless',
      process_path_regex: [TELEGRAM.value],
    })

    const direct = setAppRoute(initial, TELEGRAM, 'direct', 'proxy-vless')
    expect(direct).toEqual([
      { action: 'route', outbound: 'direct', process_path_regex: [TELEGRAM.value] },
    ])

    const back = setAppRoute(direct, TELEGRAM, 'proxy', 'proxy-vless')
    expect(back).toEqual([
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM.value] },
    ])
  })

  it('keeps the other conditions of a rule the user wrote by hand', () => {
    const initial = rules({
      action: 'route',
      outbound: 'direct',
      domain_suffix: ['.local'],
      process_path_regex: [TELEGRAM.value, DISCORD.value],
    })

    const next = setAppRoute(initial, TELEGRAM, 'off', 'proxy-vless')

    expect(next).toEqual([
      {
        action: 'route',
        outbound: 'direct',
        domain_suffix: ['.local'],
        process_path_regex: [DISCORD.value],
      },
    ])
  })

  it('removes a rule that only carried the application', () => {
    const initial = rules(
      { action: 'route', ip_is_private: true, outbound: 'direct' },
      { action: 'route', outbound: 'proxy-vless', process_path_regex: [TELEGRAM.value] },
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
  it('counts the rules that select a program, under any process condition', () => {
    const list = rules(
      { action: 'route', outbound: 'direct' },
      { action: 'route', outbound: 'proxy', process_path_regex: [TELEGRAM.value] },
      { action: 'route', outbound: 'direct', process_path_regex: [DISCORD.value] },
      { action: 'route', outbound: 'direct', process_name: ['chrome.exe'] },
      { action: 'route', outbound: 'direct', process_path: ['C:\\\\Steam\\\\steam.exe'] },
      { action: 'route', outbound: 'direct', domain_suffix: ['.local'] },
    )

    expect(appRulesCount(list)).toBe(4)
  })
})

describe('conditionDescription', () => {
  it('explains each process condition in its own terms', () => {
    expect(conditionDescription('process_name')).toMatch(/именем файла/i)
    expect(conditionDescription('process_path_regex')).toMatch(/пут[её]/i)
    // The two platforms do not share a sentence: one matches a file name, the
    // other a path, and the card has to say which one is in force.
    expect(conditionDescription('process_name')).not.toBe(
      conditionDescription('process_path_regex'),
    )
    // An unknown condition still gets the path explanation rather than nothing.
    expect(conditionDescription('something_else')).not.toBe('')
  })
})
