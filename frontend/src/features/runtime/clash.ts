/**
 * Clash API endpoint discovery.
 *
 * The runtime screen shows the Clash/external-controller endpoint sing-box is
 * listening on. The controller address is config data, not runtime status, so it
 * is read from the applied config JSON and derived with a pure function that the
 * unit tests cover directly.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query'

import { backendAvailable } from '@/app/queries'
import { configApi } from '@/shared/api/bindings'

/** Screen-local derived key: the endpoint is a projection of the active config. */
export const CLASH_ENDPOINT_KEY = ['runtime', 'clash-endpoint'] as const

interface ClashApiSection {
  external_controller?: unknown
  externalController?: unknown
}

/**
 * parseClashEndpoint extracts `experimental.clash_api.external_controller`
 * (snake_case as sing-box expects it, camelCase tolerated) from config JSON.
 * Returns undefined when the config is empty, unparsable or has no clash_api.
 */
export function parseClashEndpoint(configJson: string | undefined): string | undefined {
  if (!configJson || configJson.trim() === '') return undefined
  let parsed: unknown
  try {
    parsed = JSON.parse(configJson)
  } catch {
    return undefined
  }
  if (typeof parsed !== 'object' || parsed === null) return undefined
  const experimental = (parsed as { experimental?: unknown }).experimental
  if (typeof experimental !== 'object' || experimental === null) return undefined
  const clashApi = (experimental as { clash_api?: unknown }).clash_api
  if (typeof clashApi !== 'object' || clashApi === null) return undefined
  const section = clashApi as ClashApiSection
  const controller = section.external_controller ?? section.externalController
  if (typeof controller !== 'string') return undefined
  const trimmed = controller.trim()
  return trimmed === '' ? undefined : trimmed
}

/** Reads the applied config and derives the Clash API endpoint from it. */
export function useClashEndpointQuery(): UseQueryResult<string | undefined> {
  return useQuery({
    queryKey: CLASH_ENDPOINT_KEY,
    queryFn: async () => parseClashEndpoint((await configApi.getActiveConfig()).configJson),
    enabled: backendAvailable(),
  })
}
