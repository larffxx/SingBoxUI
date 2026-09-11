import { QueryClient } from '@tanstack/react-query'

/**
 * Application query client.
 *
 * Defaults follow spec §58: a single retry for reads (the desktop backend is
 * local, so a retry only helps with transient busy states), no retry for
 * mutations (they are never idempotent), and a short stale time so the screens
 * stay live without hammering the bindings.
 */
export const QUERY_STALE_TIME_MS = 5_000

export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 1,
        staleTime: QUERY_STALE_TIME_MS,
        refetchOnWindowFocus: false,
        networkMode: 'always',
      },
      mutations: {
        retry: 0,
        networkMode: 'always',
      },
    },
  })
}
