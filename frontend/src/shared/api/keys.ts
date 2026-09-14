/**
 * TanStack Query key factory (spec §58).
 *
 * Keys are centralised so that an event handler, a mutation and a screen all
 * agree on which cache entries to read and invalidate. Keys are plain data —
 * never functions, never objects with behaviour — so they stay stable.
 */
import type { runtime } from '@wails/go/models'

export const keys = {
  profiles: {
    all: ['profiles'] as const,
    list: () => [...keys.profiles.all, 'list'] as const,
    detail: (profileId: string) => [...keys.profiles.all, 'detail', profileId] as const,
    templates: () => [...keys.profiles.all, 'templates'] as const,
    revisions: (profileId: string, limit: number) =>
      [...keys.profiles.all, 'revisions', profileId, limit] as const,
  },
  config: {
    all: ['config'] as const,
    active: () => [...keys.config.all, 'active'] as const,
    draft: (profileId: string) => [...keys.config.all, 'draft', profileId] as const,
    revision: (profileId: string, revisionId: string) =>
      [...keys.config.all, 'revision', profileId, revisionId] as const,
    diff: (profileId: string, leftId: string, rightId: string) =>
      [...keys.config.all, 'diff', profileId, leftId, rightId] as const,
    legacy: () => [...keys.config.all, 'legacy'] as const,
  },
  runtime: {
    all: ['runtime'] as const,
    status: () => [...keys.runtime.all, 'status'] as const,
    logs: (query: runtime.LogQuery) => [...keys.runtime.all, 'logs', query] as const,
  },
  binary: {
    all: ['binary'] as const,
    status: () => [...keys.binary.all, 'status'] as const,
    updateCheck: () => [...keys.binary.all, 'update-check'] as const,
  },
  settings: {
    all: ['settings'] as const,
    values: () => [...keys.settings.all, 'values'] as const,
    environment: () => [...keys.settings.all, 'environment'] as const,
  },
  traffic: {
    all: ['traffic'] as const,
    snapshot: () => [...keys.traffic.all, 'snapshot'] as const,
  },
  share: {
    all: ['share'] as const,
    schemes: () => [...keys.share.all, 'schemes'] as const,
  },
  apps: {
    all: ['apps'] as const,
    list: () => [...keys.apps.all, 'list'] as const,
  },
} as const
