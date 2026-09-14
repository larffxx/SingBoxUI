/**
 * The routing rules behind the applications card (ADR 011, ADR 013).
 *
 * A program is not an address: the rule that selects one matches the process
 * sing-box reports for a connection. Which process condition identifies a program
 * is a property of the operating system, not of this module — macOS selects a
 * bundle by its path, Windows a program by its file name — so the backend
 * declares the condition next to the listing and everything here only decides
 * where a rule goes in the list and what clearing an application does to it.
 * This module never invents a condition.
 */
import { getString, getStringList, type JsonObject } from './jsonDoc'

/** AppTarget is what the user can ask of one application. */
export type AppTarget = 'proxy' | 'direct' | 'off'

/**
 * AppCondition is the pair that selects one program: the field of the route rule
 * and the value it has to carry.
 */
export interface AppCondition {
  /** key is the route-rule field, such as process_name. */
  key: string
  /** value is what that field must carry, such as chrome.exe. */
  value: string
}

/**
 * PROCESS_KEYS are the route-rule fields that select a process. They are the
 * conditions a rule can carry as long as it is about a program.
 */
export const PROCESS_KEYS = ['process_name', 'process_path', 'process_path_regex'] as const

/**
 * CONDITIONLESS_KEYS are the keys a rule can carry without selecting anything.
 * A rule that has nothing else left is removed rather than kept: it would route
 * every connection of the machine.
 */
const CONDITIONLESS_KEYS = new Set(['action', 'outbound'])

/** appRuleIndex finds the rule that selects `condition`, or -1. */
export function appRuleIndex(rules: JsonObject[], condition: AppCondition): number {
  return rules.findIndex((rule) => getStringList(rule, condition.key).includes(condition.value))
}

/** appRuleTarget reports how the document routes one application today. */
export function appRuleTarget(rules: JsonObject[], condition: AppCondition): AppTarget {
  const index = appRuleIndex(rules, condition)
  if (index < 0) return 'off'
  const rule = rules[index]
  if (!rule) return 'off'
  const outbound = getString(rule, 'outbound')
  const action = getString(rule, 'action')
  // A rule that cannot route (reject, sniff, …) is not a routing decision the
  // card can show as “через VPN” or “напрямую”.
  if (action !== '' && action !== 'route') return 'off'
  if (outbound === 'direct') return 'direct'
  if (outbound === '') return 'off'
  return 'proxy'
}

/**
 * setAppRoute rewrites the list for one application.
 *
 * A new rule is appended: rules are evaluated in order, and the screen must not
 * silently move a condition above the rules the user wrote by hand. The ↑/↓
 * buttons of the rule list are how a rule is given priority.
 *
 * A rule the user extended by hand keeps its other conditions when the
 * application is cleared; a rule that was created for the application alone is
 * removed with it — an action and an outbound without a condition would route
 * the whole machine, which is never what clearing one application means.
 */
export function setAppRoute(
  rules: JsonObject[],
  condition: AppCondition,
  target: AppTarget,
  proxyOutbound: string,
): JsonObject[] {
  const index = appRuleIndex(rules, condition)
  if (target === 'off') {
    if (index < 0) return rules
    const existing = rules[index]
    if (!existing) return rules
    const remaining = getStringList(existing, condition.key).filter(
      (value) => value !== condition.value,
    )
    const next = { ...existing }
    if (remaining.length === 0) {
      delete next[condition.key]
    } else {
      next[condition.key] = remaining
    }
    const keepsConditions = Object.keys(next).some((key) => !CONDITIONLESS_KEYS.has(key))
    const updated = [...rules]
    if (keepsConditions) {
      updated[index] = next
    } else {
      updated.splice(index, 1)
    }
    return updated
  }

  const outbound = target === 'direct' ? 'direct' : proxyOutbound
  if (index < 0) {
    return [...rules, { action: 'route', outbound, [condition.key]: [condition.value] }]
  }
  const updated = [...rules]
  updated[index] = { ...rules[index], action: 'route', outbound }
  return updated
}

/**
 * appRulesCount counts the rules of a document that select a program. Any of the
 * process conditions counts: the card writes one of them, the rule editor writes
 * whichever the user chose, and both are application rules.
 */
export function appRulesCount(rules: JsonObject[]): number {
  return rules.filter((rule) => PROCESS_KEYS.some((key) => getStringList(rule, key).length > 0))
    .length
}

/**
 * conditionDescription explains one process condition, in the words of the
 * platform it came from: a file name survives an update that moves the program,
 * a path is what a bundle or a whole folder is selected by.
 */
export function conditionDescription(key: string): string {
  switch (key) {
    case 'process_name':
      return 'sing-box сопоставляет его с именем файла запущенного процесса, поэтому правило переживает обновление программы, которое переносит её в другую папку.'
    case 'process_path':
      return 'sing-box сопоставляет его с полным путём запущенного процесса.'
    default:
      return 'sing-box сопоставляет его с реальным путём запущенного процесса, поэтому правило действует и на вспомогательные процессы внутри приложения.'
  }
}
