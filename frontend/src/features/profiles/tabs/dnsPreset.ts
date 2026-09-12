/**
 * Turning a DNS preset into the `dns` block of a configuration (spec §35, §36).
 *
 * The logic lives outside the tab component so the rule that decision-making
 * follows can be tested on its own: what the preset writes has to be something
 * the managed sing-box actually starts.
 */
import { getObject, getString, setObject, type JsonObject } from '../jsonDoc'
import type { DnsPreset } from '../resourceSpecs'

/**
 * Applies a preset: it replaces the whole `dns` block and names the resolver
 * sing-box should dial through.
 *
 * The resolver matters because sing-box 1.14 refuses a configuration that
 * declares DNS servers but no `route.default_domain_resolver`: it cannot dial a
 * domain, fails right after starting and the profile looks broken to the user
 * although nothing is wrong on their side. An explicit resolver the user already
 * set is never overwritten. The servers themselves are written without `detour`:
 * a DNS server dials directly already, and sing-box 1.14 rejects an explicit
 * `detour: direct` with "detour to an empty direct outbound makes no sense".
 */
export function applyDnsPreset(root: JsonObject, preset: DnsPreset): JsonObject {
  let next = setObject(root, 'dns', { servers: preset.servers, strategy: preset.strategy })
  const remote = preset.servers.find((server) => getString(server, 'type') !== 'local')
  const resolverTag = remote ? getString(remote, 'tag') : ''
  const route = getObject(root, 'route')
  if (resolverTag !== '' && route.default_domain_resolver === undefined) {
    next = setObject(
      next,
      'route',
      setObject(route, 'default_domain_resolver', { server: resolverTag }),
    )
  }
  return next
}
