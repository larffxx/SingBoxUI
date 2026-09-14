/**
 * App-level data access.
 *
 * Every screen reads the backend through these hooks so the query keys, the
 * enablement gate and the post-mutation cache writes stay in one place. Outside
 * the desktop webview (`isDesktopRuntime() === false`, e.g. `vite dev` in a
 * browser or a unit test) the queries stay idle instead of failing, which is
 * what lets the shell render a "backend unavailable" notice instead of crashing.
 */
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query'

import {
  binaryApi,
  configApi,
  isDesktopRuntime,
  profilesApi,
  runtimeApi,
  settingsApi,
  trafficApi,
  type desktop,
  type profile,
  type runtime,
  type settings,
} from '@/shared/api/bindings'
import { keys } from '@/shared/api/keys'

/** True when the UI is embedded in the Wails webview with live bindings. */
export function backendAvailable(): boolean {
  return isDesktopRuntime()
}

function enabled(): boolean {
  return backendAvailable()
}

export function useRuntimePayloadQuery(): UseQueryResult<desktop.RuntimePayload> {
  return useQuery({
    queryKey: keys.runtime.status(),
    queryFn: () => runtimeApi.status(),
    enabled: enabled(),
  })
}

export function useProfilesQuery(): UseQueryResult<desktop.ProfilesPayload> {
  return useQuery({
    queryKey: keys.profiles.list(),
    queryFn: () => profilesApi.list(),
    enabled: enabled(),
  })
}

export function useSettingsQuery(): UseQueryResult<desktop.SettingsPayload> {
  return useQuery({
    queryKey: keys.settings.values(),
    queryFn: () => settingsApi.get(),
    enabled: enabled(),
  })
}

export function useEnvironmentQuery(): UseQueryResult<desktop.EnvironmentPayload> {
  return useQuery({
    queryKey: keys.settings.environment(),
    queryFn: () => settingsApi.environment(),
    enabled: enabled(),
  })
}

export function useTrafficQuery(): UseQueryResult<desktop.TrafficPayload> {
  return useQuery({
    queryKey: keys.traffic.snapshot(),
    queryFn: () => trafficApi.snapshot(),
    enabled: enabled(),
  })
}

export function useBinaryStatusQuery(): UseQueryResult<desktop.BinaryStatusPayload> {
  return useQuery({
    queryKey: keys.binary.status(),
    queryFn: () => binaryApi.status(),
    enabled: enabled(),
  })
}

export function useLegacyConfigQuery(): UseQueryResult<desktop.LegacyPayload> {
  return useQuery({
    queryKey: keys.config.legacy(),
    queryFn: () => configApi.detectLegacy(),
    enabled: enabled(),
  })
}

export function useActiveConfigPathQuery(): UseQueryResult<string> {
  return useQuery({
    queryKey: keys.config.active(),
    queryFn: () => configApi.activeConfigPath(),
    enabled: enabled(),
  })
}

/** The initial log snapshot the runtime screen seeds its buffer with. */
export const LOG_SNAPSHOT_QUERY: runtime.LogQuery = {
  limit: 500,
  afterSeq: 0,
  filter: '',
  level: '',
}

export function useRuntimeLogsQuery(
  query: runtime.LogQuery = LOG_SNAPSHOT_QUERY,
): UseQueryResult<desktop.LogsPayload> {
  return useQuery({
    queryKey: keys.runtime.logs(query),
    queryFn: () => runtimeApi.logs(query),
    enabled: enabled(),
  })
}

export interface ActiveProfile {
  activeId: string
  profile: profile.Profile | undefined
  profiles: profile.Profile[]
}

/** The active profile, resolved from the cached profile list. */
export function useActiveProfile(): ActiveProfile {
  const query = useProfilesQuery()
  const profiles = query.data?.profiles ?? []
  const activeId = query.data?.activeId ?? ''
  return {
    activeId,
    profile: profiles.find((candidate) => candidate.id === activeId),
    profiles,
  }
}

/** Narrow view of the runtime payload used by the shell and the status bar. */
export interface RuntimeSummary {
  payload: desktop.RuntimePayload | undefined
  state: string
  isRunning: boolean
  isBusy: boolean
  isError: boolean
  /** The raw rejection from the status read, for `messageFor()`. */
  error: unknown
  isLoading: boolean
}

export function useRuntimeSummary(): RuntimeSummary {
  const query = useRuntimePayloadQuery()
  const state = query.data?.status?.state ?? 'STOPPED'
  return {
    payload: query.data,
    state,
    isRunning: state === 'RUNNING',
    isBusy: state === 'STARTING' || state === 'STOPPING',
    isError: query.isError,
    error: query.error,
    isLoading: query.isPending && query.fetchStatus !== 'idle',
  }
}

/**
 * Settings and runtime mutations.
 *
 * The backend stays authoritative: on success the payload it returned is what
 * lands in the cache, so the UI never re-derives state it did not observe.
 */
export function useUpdateSettingsMutation(): UseMutationResult<
  desktop.SettingsPayload,
  unknown,
  settings.UpdateInput
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: settings.UpdateInput) => settingsApi.update(input),
    onSuccess: (payload) => {
      client.setQueryData(keys.settings.values(), payload)
    },
  })
}

export function useSetAutostartMutation(): UseMutationResult<
  desktop.SettingsPayload,
  unknown,
  boolean
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (enabled: boolean) => settingsApi.setAutostart(enabled),
    onSuccess: (payload) => {
      client.setQueryData(keys.settings.values(), payload)
    },
  })
}

export function useRemoveLegacyAutostartMutation(): UseMutationResult<
  desktop.SettingsPayload,
  unknown,
  void
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: () => settingsApi.removeLegacyAutostart(),
    onSuccess: (payload) => {
      client.setQueryData(keys.settings.values(), payload)
    },
  })
}

export function useStartRuntimeMutation(): UseMutationResult<
  desktop.RuntimePayload,
  unknown,
  string
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (profileId: string) => runtimeApi.start(profileId),
    onSuccess: (payload) => {
      client.setQueryData(keys.runtime.status(), payload)
      void client.invalidateQueries({ queryKey: keys.traffic.all })
    },
  })
}

export function useStopRuntimeMutation(): UseMutationResult<desktop.RuntimePayload, unknown, void> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: () => runtimeApi.stop(),
    onSuccess: (payload) => {
      client.setQueryData(keys.runtime.status(), payload)
      void client.invalidateQueries({ queryKey: keys.traffic.all })
    },
  })
}

/**
 * Stops a sing-box this application did not start (ADR 012): a core that
 * outlived the window holds the TUN device and the ports, so nothing can be
 * started before it is gone.
 */
export function useStopForeignProcessesMutation(): UseMutationResult<
  desktop.RuntimePayload,
  unknown,
  void
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: () => runtimeApi.stopForeignProcesses(),
    onSuccess: (payload) => {
      client.setQueryData(keys.runtime.status(), payload)
      void client.invalidateQueries({ queryKey: keys.traffic.all })
    },
  })
}

export function useRestartRuntimeMutation(): UseMutationResult<
  desktop.RuntimePayload,
  unknown,
  string
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (profileId: string) => runtimeApi.restart(profileId),
    onSuccess: (payload) => {
      client.setQueryData(keys.runtime.status(), payload)
    },
  })
}

export function useClearLogsMutation(): UseMutationResult<desktop.LogsPayload, unknown, void> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: () => runtimeApi.clearLogs(),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.runtime.all })
    },
  })
}

/** Imports a config found by `configApi.detectLegacy()` as a new profile. */
export function useImportLegacyConfigMutation(): UseMutationResult<
  desktop.ProfilePayload,
  unknown,
  string
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (path: string) => configApi.importLegacy(path),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

/**
 * Imports any sing-box configuration on disk as a new profile (spec §11). The
 * backend reads the file and leaves it alone, so this only ever adds a profile.
 */
export function useImportConfigFileMutation(): UseMutationResult<
  desktop.ProfilePayload,
  unknown,
  { path: string; name: string }
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: { path: string; name: string }) => configApi.importConfigFile(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}
