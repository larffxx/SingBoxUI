/**
 * Typed access to the Wails bindings.
 *
 * The generated modules in `frontend/wailsjs` are the only transport layer the
 * UI uses (spec §32): nothing here talks HTTP, and no request/response model is
 * duplicated by hand. Each helper unwraps the payload envelope so a failure
 * surfaces as a thrown `BoundCallError` with a stable `code`.
 */
import * as AppsAPI from '@wails/go/desktop/AppsAPI'
import * as BinaryAPI from '@wails/go/desktop/BinaryAPI'
import * as ConfigAPI from '@wails/go/desktop/ConfigAPI'
import * as ProfileAPI from '@wails/go/desktop/ProfileAPI'
import * as RuntimeAPI from '@wails/go/desktop/RuntimeAPI'
import * as SettingsAPI from '@wails/go/desktop/SettingsAPI'
import * as ShareAPI from '@wails/go/desktop/ShareAPI'
import * as TrafficAPI from '@wails/go/desktop/TrafficAPI'
import type { config, desktop, runtime, settings } from '@wails/go/models'

import { unwrap } from './errors'

export type {
  apperr,
  applications,
  apps,
  binary,
  config,
  desktop,
  profile,
  profiles,
  runtime,
  settings,
  share,
  singbox,
  templates,
  traffic,
} from '@wails/go/models'

/** True when the UI runs inside the Wails webview with bindings attached. */
export function isDesktopRuntime(): boolean {
  return typeof window !== 'undefined' && 'go' in window
}

/** ProfileAPI — profile lifecycle (spec §11, §31). */
export const profilesApi = {
  list: async (): Promise<desktop.ProfilesPayload> => unwrap(await ProfileAPI.ListProfiles()),
  get: async (id: string): Promise<desktop.ProfilePayload> =>
    unwrap(await ProfileAPI.GetProfile(id)),
  create: async (input: desktop.CreateProfileRequest): Promise<desktop.ProfilePayload> =>
    unwrap(await ProfileAPI.CreateProfile(input)),
  import_: async (input: desktop.CreateProfileRequest): Promise<desktop.ProfilePayload> =>
    unwrap(await ProfileAPI.ImportProfile(input)),
  rename: async (id: string, name: string, description: string): Promise<desktop.ProfilePayload> =>
    unwrap(await ProfileAPI.RenameProfile(id, name, description)),
  duplicate: async (id: string, name: string): Promise<desktop.ProfilePayload> =>
    unwrap(await ProfileAPI.DuplicateProfile(id, name)),
  remove: async (id: string): Promise<desktop.ProfilesPayload> =>
    unwrap(await ProfileAPI.DeleteProfile(id)),
  setActive: async (id: string): Promise<desktop.ProfilesPayload> =>
    unwrap(await ProfileAPI.SetActiveProfile(id)),
  exportProfile: async (id: string): Promise<desktop.ExportPayload> =>
    unwrap(await ProfileAPI.ExportProfile(id)),
  listTemplates: async (): Promise<desktop.TemplatesPayload> =>
    unwrap(await ProfileAPI.ListTemplates()),
  applyTemplate: async (profileId: string, templateId: string): Promise<desktop.ProfilePayload> =>
    unwrap(await ProfileAPI.ApplyTemplateToProfile(profileId, templateId)),
  listRevisions: async (profileId: string, limit: number): Promise<desktop.RevisionsPayload> =>
    unwrap(await ProfileAPI.ListRevisions(profileId, limit)),
}

/** ConfigAPI — drafts, validation, revisions, apply/rollback (spec §12–§14). */
export const configApi = {
  activeConfigPath: async (): Promise<string> => ConfigAPI.ActiveConfigPath(),
  getActiveConfig: async (): Promise<desktop.ActiveConfigPayload> =>
    unwrap(await ConfigAPI.GetActiveConfig()),
  getDraft: async (profileId: string): Promise<desktop.DraftPayload> =>
    unwrap(await ConfigAPI.GetDraft(profileId)),
  validate: async (input: config.ValidateInput): Promise<desktop.ValidatePayload> =>
    unwrap(await ConfigAPI.ValidateConfig(input)),
  saveRevision: async (input: config.SaveInput): Promise<desktop.RevisionPayload> =>
    unwrap(await ConfigAPI.SaveRevision(input)),
  applyRevision: async (input: config.ApplyInput): Promise<desktop.ApplyPayload> =>
    unwrap(await ConfigAPI.ApplyRevision(input)),
  rollback: async (input: config.ApplyInput): Promise<desktop.RevisionPayload> =>
    unwrap(await ConfigAPI.RollbackToRevision(input)),
  applyTemplate: async (profileId: string, templateId: string): Promise<desktop.RevisionPayload> =>
    unwrap(await ConfigAPI.ApplyTemplate(profileId, templateId)),
  listRevisions: async (profileId: string, limit: number): Promise<desktop.RevisionListPayload> =>
    unwrap(await ConfigAPI.ListRevisions(profileId, limit)),
  getRevision: async (profileId: string, revisionId: string): Promise<desktop.RevisionPayload> =>
    unwrap(await ConfigAPI.GetRevision(profileId, revisionId)),
  compareRevisions: async (
    profileId: string,
    leftId: string,
    rightId: string,
  ): Promise<desktop.DiffPayload> =>
    unwrap(await ConfigAPI.CompareRevisions(profileId, leftId, rightId)),
  compareWithActive: async (
    profileId: string,
    revisionId: string,
    draftJson: string,
  ): Promise<desktop.DiffPayload> =>
    unwrap(await ConfigAPI.CompareWithActive(profileId, revisionId, draftJson)),
  detectLegacy: async (): Promise<desktop.LegacyPayload> =>
    unwrap(await ConfigAPI.DetectLegacyConfig()),
  importLegacy: async (path: string): Promise<desktop.ProfilePayload> =>
    unwrap(await ConfigAPI.ImportLegacyConfig(path)),
  /** Opens the native picker; a dismissed dialog comes back as `canceled`. */
  pickConfigFile: async (): Promise<desktop.PickConfigFilePayload> =>
    unwrap(await ConfigAPI.PickConfigFile()),
  /** Copies the configuration at `path` into a new profile (spec §11). */
  importConfigFile: async (input: {
    path: string
    name: string
  }): Promise<desktop.ProfilePayload> => unwrap(await ConfigAPI.ImportConfigFile(input)),
}

/** RuntimeAPI — supervised sing-box process (spec §22, §23). */
export const runtimeApi = {
  status: async (): Promise<desktop.RuntimePayload> => unwrap(await RuntimeAPI.GetRuntimeStatus()),
  start: async (profileId: string): Promise<desktop.RuntimePayload> =>
    unwrap(await RuntimeAPI.StartRuntime(profileId)),
  stop: async (): Promise<desktop.RuntimePayload> => unwrap(await RuntimeAPI.StopRuntime()),
  restart: async (profileId: string): Promise<desktop.RuntimePayload> =>
    unwrap(await RuntimeAPI.RestartRuntime(profileId)),
  logs: async (query: runtime.LogQuery): Promise<desktop.LogsPayload> =>
    unwrap(await RuntimeAPI.GetLogs(query)),
  tailLogs: async (afterSeq: number): Promise<desktop.LogsPayload> =>
    unwrap(await RuntimeAPI.TailLogs(afterSeq)),
  clearLogs: async (): Promise<desktop.LogsPayload> => unwrap(await RuntimeAPI.ClearLogs()),
}

/** BinaryAPI — managed sing-box release channel (spec §17–§21). */
export const binaryApi = {
  status: async (): Promise<desktop.BinaryStatusPayload> =>
    unwrap(await BinaryAPI.GetBinaryStatus()),
  probe: async (path: string): Promise<desktop.BinaryVersionPayload> =>
    unwrap(await BinaryAPI.ProbeBinary(path)),
  setSource: async (source: string, customPath: string): Promise<desktop.BinaryStatusPayload> =>
    unwrap(await BinaryAPI.SetBinarySource(source, customPath)),
  checkForUpdates: async (): Promise<desktop.BinaryCheckPayload> =>
    unwrap(await BinaryAPI.CheckForUpdates()),
  lastUpdateCheck: async (): Promise<desktop.BinaryCheckPayload> =>
    unwrap(await BinaryAPI.LastUpdateCheck()),
  installStableUpdate: async (): Promise<desktop.BinaryInstallPayload> =>
    unwrap(await BinaryAPI.InstallStableUpdate()),
}

/** SettingsAPI — typed settings, autostart, environment (spec §49–§51). */
export const settingsApi = {
  get: async (): Promise<desktop.SettingsPayload> => unwrap(await SettingsAPI.GetSettings()),
  update: async (input: settings.UpdateInput): Promise<desktop.SettingsPayload> =>
    unwrap(await SettingsAPI.UpdateSettings(input)),
  setAutostart: async (enabled: boolean): Promise<desktop.SettingsPayload> =>
    unwrap(await SettingsAPI.SetAutostart(enabled)),
  removeLegacyAutostart: async (): Promise<desktop.SettingsPayload> =>
    unwrap(await SettingsAPI.RemoveLegacyAutostart()),
  environment: async (): Promise<desktop.EnvironmentPayload> =>
    unwrap(await SettingsAPI.GetEnvironment()),
}

/** ShareAPI — share-link parsing/building (spec §39, §40). */
export const shareApi = {
  parse: async (link: string): Promise<desktop.ShareParsePayload> =>
    unwrap(await ShareAPI.ParseShareLink(link)),
  parseMany: async (text: string): Promise<desktop.ShareListPayload> =>
    unwrap(await ShareAPI.ParseShareLinks(text)),
  build: async (outboundJson: string): Promise<desktop.ShareBuildPayload> =>
    unwrap(await ShareAPI.BuildShareLink(outboundJson)),
  schemes: async (): Promise<string[]> => ShareAPI.SupportedShareSchemes(),
}

/** TrafficAPI — latest snapshot from the Clash API collector (spec §45, §46). */
export const trafficApi = {
  snapshot: async (): Promise<desktop.TrafficPayload> =>
    unwrap(await TrafficAPI.GetTrafficSnapshot()),
}

/** AppsAPI — the programs a routing rule can select by name (ADR 011). */
export const appsApi = {
  list: async (): Promise<desktop.AppsPayload> => unwrap(await AppsAPI.ListApplications()),
}
