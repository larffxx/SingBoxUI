/**
 * Profile and config data access (spec §34, §37, §58).
 *
 * Mirrors the conventions of `@/app/queries`: query keys come from the shared
 * factory, every query is gated on the desktop runtime so the screens stay
 * renderable outside the webview, and a mutation writes the payload the backend
 * returned into the cache instead of re-deriving state locally.
 */
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query'

import { backendAvailable } from '@/app/queries'
import { appsApi, configApi, profilesApi, type config, type desktop } from '@/shared/api/bindings'
import { keys } from '@/shared/api/keys'

/** How many revisions the history list asks for. */
export const REVISION_LIMIT = 50

function enabled(): boolean {
  return backendAvailable()
}

/**
 * The applications of this machine (ADR 011). The listing only changes when the
 * user installs something, so it is refreshed on an ordinary navigation rather
 * than on every mount: the backend keeps its own shorter cache on top of this.
 */
export function useApplicationsQuery(): UseQueryResult<desktop.AppsPayload> {
  return useQuery({
    queryKey: keys.apps.list(),
    queryFn: () => appsApi.list(),
    enabled: enabled(),
    staleTime: Number.POSITIVE_INFINITY,
  })
}

export function useTemplatesQuery(): UseQueryResult<desktop.TemplatesPayload> {
  return useQuery({
    queryKey: keys.profiles.templates(),
    queryFn: () => profilesApi.listTemplates(),
    enabled: enabled(),
  })
}

export function useDraftQuery(profileId: string): UseQueryResult<desktop.DraftPayload> {
  return useQuery({
    queryKey: keys.config.draft(profileId),
    queryFn: () => configApi.getDraft(profileId),
    enabled: enabled() && profileId !== '',
  })
}

export function useRevisionsQuery(
  profileId: string,
  limit: number = REVISION_LIMIT,
): UseQueryResult<desktop.RevisionListPayload> {
  return useQuery({
    queryKey: keys.profiles.revisions(profileId, limit),
    queryFn: () => configApi.listRevisions(profileId, limit),
    enabled: enabled() && profileId !== '',
  })
}

export function useRevisionQuery(
  profileId: string,
  revisionId: string,
): UseQueryResult<desktop.RevisionPayload> {
  return useQuery({
    queryKey: keys.config.revision(profileId, revisionId),
    queryFn: () => configApi.getRevision(profileId, revisionId),
    enabled: enabled() && profileId !== '' && revisionId !== '',
  })
}

/** Backend diff of two revisions; the UI never diffs JSON on its own. */
export function useCompareRevisionsQuery(
  profileId: string,
  leftId: string,
  rightId: string,
): UseQueryResult<desktop.DiffPayload> {
  return useQuery({
    queryKey: keys.config.diff(profileId, leftId, rightId),
    queryFn: () => configApi.compareRevisions(profileId, leftId, rightId),
    enabled: enabled() && profileId !== '' && leftId !== '' && rightId !== '',
  })
}

export function useValidateMutation(): UseMutationResult<
  desktop.ValidatePayload,
  unknown,
  config.ValidateInput
> {
  return useMutation({
    mutationFn: (input: config.ValidateInput) => configApi.validate(input),
  })
}

/** Compares the current draft against the active configuration (spec §56). */
export function useCompareWithActiveMutation(): UseMutationResult<
  desktop.DiffPayload,
  unknown,
  { profileId: string; revisionId: string; draftJson: string }
> {
  return useMutation({
    mutationFn: ({ profileId, revisionId, draftJson }) =>
      configApi.compareWithActive(profileId, revisionId, draftJson),
  })
}

export function useSaveRevisionMutation(): UseMutationResult<
  desktop.RevisionPayload,
  unknown,
  config.SaveInput
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: config.SaveInput) => configApi.saveRevision(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

/** Applies a revision to the active config file; the runtime may restart. */
export function useApplyRevisionMutation(): UseMutationResult<
  desktop.ApplyPayload,
  unknown,
  config.ApplyInput
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: config.ApplyInput) => configApi.applyRevision(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
      void client.invalidateQueries({ queryKey: keys.runtime.all })
    },
  })
}

/** Rolls the active config back to an older revision by creating a new one. */
export function useRollbackMutation(): UseMutationResult<
  desktop.RevisionPayload,
  unknown,
  config.ApplyInput
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: config.ApplyInput) => configApi.rollback(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

export function useApplyTemplateMutation(): UseMutationResult<
  desktop.RevisionPayload,
  unknown,
  { profileId: string; templateId: string }
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ profileId, templateId }) => configApi.applyTemplate(profileId, templateId),
    onSuccess: (_payload, variables) => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.draft(variables.profileId) })
    },
  })
}

export function useCreateProfileMutation(): UseMutationResult<
  desktop.ProfilePayload,
  unknown,
  desktop.CreateProfileRequest
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: desktop.CreateProfileRequest) => profilesApi.create(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
    },
  })
}

/**
 * The shapes a configuration generated from a share link can take. The backend declares them,
 * so the dialog offers exactly what it can generate; the list only changes with a release.
 */
export function useShareLinkBasesQuery(): UseQueryResult<desktop.ShareLinkBasesPayload> {
  return useQuery({
    queryKey: keys.profiles.shareLinkBases(),
    queryFn: () => profilesApi.listShareLinkBases(),
    enabled: enabled(),
    staleTime: Number.POSITIVE_INFINITY,
  })
}

/** Creates a profile whose first revision is generated from pasted share links (spec §39). */
export function useCreateFromShareLinksMutation(): UseMutationResult<
  desktop.ShareLinkProfilePayload,
  unknown,
  desktop.CreateProfileFromShareLinksRequest
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: desktop.CreateProfileFromShareLinksRequest) =>
      profilesApi.createFromShareLinks(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

/** Imports a config file as a new profile (also used for the legacy flow). */
export function useImportProfileMutation(): UseMutationResult<
  desktop.ProfilePayload,
  unknown,
  desktop.CreateProfileRequest
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: desktop.CreateProfileRequest) => profilesApi.import_(input),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

export function useRenameProfileMutation(): UseMutationResult<
  desktop.ProfilePayload,
  unknown,
  { profileId: string; name: string; description: string }
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ profileId, name, description }) =>
      profilesApi.rename(profileId, name, description),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
    },
  })
}

export function useDuplicateProfileMutation(): UseMutationResult<
  desktop.ProfilePayload,
  unknown,
  { profileId: string; name: string }
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ profileId, name }) => profilesApi.duplicate(profileId, name),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
    },
  })
}

export function useDeleteProfileMutation(): UseMutationResult<
  desktop.ProfilesPayload,
  unknown,
  string
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (profileId: string) => profilesApi.remove(profileId),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

export function useSetActiveProfileMutation(): UseMutationResult<
  desktop.ProfilesPayload,
  unknown,
  string
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (profileId: string) => profilesApi.setActive(profileId),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.profiles.all })
      void client.invalidateQueries({ queryKey: keys.config.all })
    },
  })
}

export function useExportProfileMutation(): UseMutationResult<
  desktop.ExportPayload,
  unknown,
  string
> {
  return useMutation({
    mutationFn: (profileId: string) => profilesApi.exportProfile(profileId),
  })
}
