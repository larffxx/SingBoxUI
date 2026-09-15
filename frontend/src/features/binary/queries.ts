/**
 * Binary manager data access (spec §17–§21).
 *
 * Same conventions as `@/app/queries`: shared query keys, gated on the desktop
 * runtime, and cache invalidation after every mutation that changes the
 * installed binary.
 */
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query'

import { backendAvailable, useBinaryStatusQuery } from '@/app/queries'
import { binaryApi } from '@/shared/api/bindings'
import type { desktop } from '@/shared/api/bindings'
import { keys } from '@/shared/api/keys'

export { useBinaryStatusQuery }

/** The last update check persisted by the backend (`LastUpdateCheck`). */
export function useLastUpdateCheckQuery(): UseQueryResult<desktop.BinaryCheckPayload> {
  return useQuery({
    queryKey: keys.binary.updateCheck(),
    queryFn: async () => binaryApi.lastUpdateCheck(),
    enabled: backendAvailable(),
    retry: false,
  })
}

function useBinaryInvalidation(): () => void {
  const client = useQueryClient()
  return () => {
    void client.invalidateQueries({ queryKey: keys.binary.all })
  }
}

export function useCheckForUpdatesMutation(): UseMutationResult<
  desktop.BinaryCheckPayload,
  unknown,
  void
> {
  const invalidate = useBinaryInvalidation()
  return useMutation({
    mutationFn: async () => binaryApi.checkForUpdates(),
    onSuccess: () => {
      invalidate()
    },
  })
}

export function useInstallStableUpdateMutation(): UseMutationResult<
  desktop.BinaryInstallPayload,
  unknown,
  void
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async () => binaryApi.installStableUpdate(),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.binary.all })
      void client.invalidateQueries({ queryKey: keys.runtime.all })
    },
  })
}

export function useSetBinarySourceMutation(): UseMutationResult<
  desktop.BinaryStatusPayload,
  unknown,
  { source: 'managed' | 'custom'; customPath: string }
> {
  const invalidate = useBinaryInvalidation()
  return useMutation({
    mutationFn: async (input: { source: 'managed' | 'custom'; customPath: string }) =>
      binaryApi.setSource(input.source, input.customPath),
    onSuccess: () => {
      invalidate()
    },
  })
}

/** Manual probe of an arbitrary binary path (`ProbeBinary`). */
export function useProbeBinaryMutation(): UseMutationResult<
  desktop.BinaryVersionPayload,
  unknown,
  string
> {
  const invalidate = useBinaryInvalidation()
  return useMutation({
    mutationFn: async (path: string) => binaryApi.probe(path),
    onSuccess: () => {
      invalidate()
    },
  })
}
