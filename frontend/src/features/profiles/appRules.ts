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
 * setAppRoute rewrites the list for one application, and for that application
 * only.
 *
 * A rule may hold several programs under one outbound — that is what a
 * hand-written rule looks like (`process_name` with four names) and what the
 * screen produces when the same direction is asked for twice. Switching one
 * program therefore never rewrites such a rule: the program leaves every routing
 * rule that selects it (those rules keep their other programs and conditions, and
 * a rule whose conditions are exhausted is removed with it) and gets a rule of its
 * own, appended, because rules are evaluated in order and the screen must not
 * silently move a condition above the rules the user wrote by hand. The ↑/↓
 * buttons of the rule list are how a rule is given priority.
 *
 * Asking for the direction a program already has changes nothing: rewriting the
 * rule would move every other program that shares it.
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
    return removeAppValue(rules, condition)
  }
  if (index >= 0 && appRuleTarget(rules, condition) === target) {
    return rules
  }
  const outbound = target === 'direct' ? 'direct' : proxyOutbound
  const cleaned = index < 0 ? rules : removeAppValue(rules, condition)
  return [...cleaned, { action: 'route', outbound, [condition.key]: [condition.value] }]
}

/**
 * removeAppValue takes one program out of every rule that routes it.
 *
 * Only routing decisions are touched: a `reject` or `sniff` rule that happens to
 * name the program is not a direction the card can change, so its condition stays
 * where the user put it. A rule that keeps other conditions (or other programs)
 * stays in the list with them; one that is left with an action and an outbound and
 * nothing else is removed, because it would route the whole machine.
 */
function removeAppValue(rules: JsonObject[], condition: AppCondition): JsonObject[] {
  const out: JsonObject[] = []
  for (const rule of rules) {
    if (!routesProgram(rule, condition)) {
      out.push(rule)
      continue
    }
    const remaining = getStringList(rule, condition.key).filter(
      (value) => value !== condition.value,
    )
    const next = { ...rule }
    if (remaining.length === 0) {
      delete next[condition.key]
    } else {
      next[condition.key] = remaining
    }
    const keepsConditions = Object.keys(next).some((key) => !CONDITIONLESS_KEYS.has(key))
    if (keepsConditions) out.push(next)
  }
  return out
}

/**
 * routesProgram reports whether a rule is a routing decision that selects this
 * program. An empty action means `route` in sing-box, which is why the rule the
 * card itself writes carries the field explicitly.
 */
function routesProgram(rule: JsonObject, condition: AppCondition): boolean {
  if (!getStringList(rule, condition.key).includes(condition.value)) return false
  const action = getString(rule, 'action')
  return action === '' || action === 'route'
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
