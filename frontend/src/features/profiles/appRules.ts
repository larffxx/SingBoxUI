/**
 * The routing rules behind the applications card (ADR 011).
 *
 * macOS has no address that identifies a program: the rule matches the real
 * path of the process, which is what sing-box reports for every connection,
 * helpers included. The expression itself comes from the backend next to the
 * listing, so this module only decides where a rule goes in the list — it never
 * invents a condition.
 */
import { getString, getStringList, type JsonObject } from './jsonDoc'

/** AppTarget is what the user can ask of one application. */
export type AppTarget = 'proxy' | 'direct' | 'off'

/** APP_RULE_KEY is the condition a generated application rule carries. */
export const APP_RULE_KEY = 'process_path_regex'

/**
 * CONDITIONLESS_KEYS are the keys a rule can carry without selecting anything.
 * A rule that has nothing else left is removed rather than kept: it would route
 * every connection of the machine.
 */
const CONDITIONLESS_KEYS = new Set(['action', 'outbound'])

/** appRuleIndex finds the rule that selects `pattern`, or -1. */
export function appRuleIndex(rules: JsonObject[], pattern: string): number {
  return rules.findIndex((rule) => getStringList(rule, APP_RULE_KEY).includes(pattern))
}

/** appRuleTarget reports how the document routes one application today. */
export function appRuleTarget(rules: JsonObject[], pattern: string): AppTarget {
  const index = appRuleIndex(rules, pattern)
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
  pattern: string,
  target: AppTarget,
  proxyOutbound: string,
): JsonObject[] {
  const index = appRuleIndex(rules, pattern)
  if (target === 'off') {
    if (index < 0) return rules
    const existing = rules[index]
    if (!existing) return rules
    const remaining = getStringList(existing, APP_RULE_KEY).filter((value) => value !== pattern)
    const next = { ...existing }
    if (remaining.length === 0) {
      delete next[APP_RULE_KEY]
    } else {
      next[APP_RULE_KEY] = remaining
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
    return [...rules, { action: 'route', outbound, [APP_RULE_KEY]: [pattern] }]
  }
  const updated = [...rules]
  updated[index] = { ...rules[index], action: 'route', outbound }
  return updated
}

/** appRulesCount counts the application rules of a document. */
export function appRulesCount(rules: JsonObject[]): number {
  return rules.filter((rule) => getStringList(rule, APP_RULE_KEY).length > 0).length
}
