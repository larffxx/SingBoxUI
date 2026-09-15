/**
 * Profile workspace tests (spec §34, §37, §56).
 *
 * Two behaviours of the workspace matter here and nothing else:
 *
 * * a backend validation error becomes a Monaco marker on the reported line and
 *   blocks saving, with the issue list carrying the jump affordance;
 * * selecting two revisions renders the backend diff with both sides.
 *
 * Monaco itself is mocked (`@monaco-editor/react`); the JSON editor is only the
 * surface the markers are applied to, and jsdom cannot run the real editor.
 */
import { createMemoryHistory } from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import '@/app/testing/environment'

import { AppProviders } from '@/app/providers'
import { createAppQueryClient } from '@/app/queryClient'
import { AppRouterView } from '@/app/router'
import { createAppRouter } from '@/app/routeTree'

const DRAFT_JSON = [
  '{',
  '  "log": { "level": "info" },',
  '  "inbounds": [{ "type": "tun", "tag": "tun-in" }],',
  '  "outbounds": [{ "type": "direct", "tag": "direct" }],',
  '  "route": { "rules": [] }',
  '}',
].join('\n')

/** The backend reports the semantic problem without a path, only a line. */
const CHECK_ERROR = 'ERROR: unknown field "fake" at line 3'

const mocks = vi.hoisted(() => ({
  validate: vi.fn(),
  getRevision: vi.fn(),
  compareRevisions: vi.fn(),
}))

const monaco = vi.hoisted(() => ({
  setModelMarkers: vi.fn(),
}))

vi.mock('@monaco-editor/react', async () => {
  const React = await import('react')
  const model = { uri: { toString: () => 'inmemory://model/1' } }
  const instance = { getModel: () => model }
  const api = {
    languages: { json: { jsonDefaults: { setDiagnosticsOptions: vi.fn() } } },
    editor: { defineTheme: vi.fn(), setModelMarkers: monaco.setModelMarkers },
  }
  return {
    default: function MonacoEditorMock({
      value,
      onMount,
    }: {
      value: string
      onMount?: (i: unknown, m: unknown) => void
    }) {
      React.useEffect(() => {
        onMount?.(instance, api)
      }, [onMount])
      return React.createElement('pre', { 'data-testid': 'raw-editor' }, value)
    },
    // JsonDiff forwards the two documents to the diff surface; the labels are
    // rendered by JsonDiff itself, so only `original`/`modified` arrive here.
    DiffEditor: ({ original, modified }: { original: string; modified: string }) =>
      React.createElement('div', {
        'data-testid': 'json-diff',
        'data-original': original,
        'data-modified': modified,
      }),
  }
})

const profilesPayload = {
  profiles: [
    {
      id: 'profile-primary',
      name: 'Основной',
      description: '',
      createdAt: '2026-09-11T10:00:00Z',
      updatedAt: '2026-09-11T11:00:00Z',
      activeRevisionId: 'revision-old',
    },
  ],
  activeId: 'profile-primary',
  running: false,
}

const draftPayload = {
  draft: {
    profileId: 'profile-primary',
    profileName: 'Основной',
    baseRevisionId: 'revision-old',
    baseSource: 'manual',
    configJson: DRAFT_JSON,
    singBoxVersion: '1.13.0',
    validation: '',
    structural: { ok: true, errors: [], warnings: [] },
  },
}

function revisionView(id: string, configJson: string, active: boolean) {
  return {
    id,
    profileId: 'profile-primary',
    createdAt: '2026-09-11T10:30:00Z',
    source: 'manual',
    configJson,
    structuralValidationStatus: 'ok',
    singBoxValidationStatus: 'ok',
    singBoxVersion: '1.13.0',
    comment: '',
    active,
    size: configJson.length,
  }
}

const revisionOld = revisionView('revision-old', DRAFT_JSON, true)

const revisionNew = revisionView(
  'revision-new',
  DRAFT_JSON.replace('"tag": "direct"', '"tag": "proxy"'),
  false,
)

const runtimePayload = {
  status: {
    state: 'STOPPED',
    pid: 0,
    uptimeSeconds: 0,
    activeProfileId: 'profile-primary',
    activeRevisionId: 'revision-old',
    binaryVersion: '1.13.0',
    configPath:
      '/Users/test/Library/Application Support/SingBoxUI/profiles/profile-primary/config.json',
    elevated: false,
  },
  activeProfileName: 'Основной',
  binaryVersion: '1.13.0',
  trafficAvailable: false,
  shuttingDown: false,
}

vi.mock('@/shared/api/bindings', () => ({
  isDesktopRuntime: () => true,
  profilesApi: {
    list: () => Promise.resolve(profilesPayload),
    listTemplates: () => Promise.resolve({ templates: [] }),
  },
  runtimeApi: {
    status: () => Promise.resolve(runtimePayload),
    logs: () => Promise.resolve({ records: [] }),
  },
  trafficApi: {
    snapshot: () =>
      Promise.resolve({
        snapshot: {
          available: false,
          uploadTotalBytes: 0,
          downloadTotalBytes: 0,
          uploadRateBytes: 0,
          downloadRateBytes: 0,
          connections: 0,
          capturedAt: '2026-09-11T10:00:00Z',
        },
        collectorRunning: false,
      }),
  },
  configApi: {
    activeConfigPath: () =>
      Promise.resolve(
        '/Users/test/Library/Application Support/SingBoxUI/profiles/profile-primary/config.json',
      ),
    getDraft: () => Promise.resolve(draftPayload),
    validate: mocks.validate,
    saveRevision: () => Promise.resolve({ revision: revisionOld }),
    listRevisions: () => Promise.resolve({ revisions: [revisionNew, revisionOld] }),
    getRevision: mocks.getRevision,
    compareRevisions: mocks.compareRevisions,
    applyRevision: () => Promise.resolve({ result: undefined }),
    applyTemplate: () => Promise.resolve({ revision: revisionOld }),
  },
}))

function renderWorkspace() {
  const router = createAppRouter(
    createMemoryHistory({ initialEntries: ['/profiles/profile-primary'] }),
  )
  return render(
    <AppProviders queryClient={createAppQueryClient()}>
      <AppRouterView router={router} />
    </AppProviders>,
  )
}

/** Last marker set passed to Monaco, as `JsonEditor` would publish them. */
function lastMarkers(): { startLineNumber: number; message: string; severity: number }[] {
  const calls = monaco.setModelMarkers.mock.calls
  const last = calls[calls.length - 1]
  if (last === undefined) return []
  return last[2] as { startLineNumber: number; message: string; severity: number }[]
}

beforeEach(() => {
  mocks.validate.mockReset()
  mocks.getRevision.mockReset()
  mocks.compareRevisions.mockReset()
  monaco.setModelMarkers.mockClear()

  mocks.validate.mockResolvedValue({
    result: {
      structural: { ok: true, errors: [], warnings: [] },
      check: { ok: false, version: '1.13.0', output: CHECK_ERROR, errors: [CHECK_ERROR] },
      valid: false,
      version: '1.13.0',
    },
  })
  mocks.getRevision.mockImplementation((_profileId: string, revisionId: string) =>
    Promise.resolve({
      revision: revisionId === 'revision-new' ? revisionNew : revisionOld,
    }),
  )
  mocks.compareRevisions.mockResolvedValue({
    diff: {
      leftLabel: 'revision-old',
      rightLabel: 'revision-new',
      added: 1,
      removed: 1,
      identical: false,
      lines: [],
    },
  })
})

describe('profile workspace', () => {
  it('maps a backend validation error to a marker and blocks saving', async () => {
    const user = userEvent.setup({ delay: null })
    renderWorkspace()

    await screen.findByRole('heading', { level: 1, name: 'Основной' })
    await user.click(screen.getByRole('tab', { name: 'Raw JSON' }))

    // The header toolbar and the revisions card both offer "Сохранить ревизию".
    const save = screen.getAllByRole('button', { name: 'Сохранить ревизию' })[0]
    if (save === undefined) throw new Error('no save button')
    await waitFor(() => {
      expect(save).toBeEnabled()
    })

    await user.click(screen.getByRole('button', { name: 'Проверить' }))

    await waitFor(() => {
      expect(mocks.validate).toHaveBeenCalledWith({
        profileId: 'profile-primary',
        configJson: DRAFT_JSON,
        skipSingBoxCheck: false,
      })
    })

    // The marker lands on the line the backend named, not on line 1.
    await waitFor(() => {
      expect(lastMarkers()).toHaveLength(1)
    })
    expect(lastMarkers()[0]).toMatchObject({ startLineNumber: 3, message: CHECK_ERROR })

    // Inline issue with the jump affordance. The message appears twice: in the
    // validation card and in the raw tab's issue list.
    await waitFor(() => {
      expect(screen.getAllByText(CHECK_ERROR).length).toBeGreaterThan(0)
    })
    expect(screen.getByRole('button', { name: 'Перейти к строке 3' })).toBeInTheDocument()

    // Saving is gated while the validation reports an error.
    await waitFor(() => {
      expect(save).toBeDisabled()
    })
  })

  it('renders both sides of the backend diff for two selected revisions', async () => {
    const user = userEvent.setup({ delay: null })
    renderWorkspace()

    const boxes = await screen.findAllByRole('checkbox')
    expect(boxes).toHaveLength(2)
    const [first, second] = boxes
    if (first === undefined || second === undefined) throw new Error('missing revision checkboxes')

    await user.click(first)
    await user.click(second)

    await waitFor(() => {
      expect(mocks.compareRevisions).toHaveBeenCalledWith(
        'profile-primary',
        'revision-new',
        'revision-old',
      )
    })

    const diff = await screen.findByTestId('json-diff')
    // Both sides are the full revision documents, and they differ exactly in the
    // tag the fixture changed — the backend diff did not invent a side.
    const left = diff.getAttribute('data-original') ?? ''
    const right = diff.getAttribute('data-modified') ?? ''
    expect(left).toContain('"log"')
    expect(right).toContain('"log"')
    expect(left).toContain('"tag": "proxy"')
    expect(right).toContain('"tag": "direct"')

    expect(screen.getByRole('heading', { name: 'Сравнение ревизий' })).toBeInTheDocument()
    expect(screen.getByText('отличаются')).toBeInTheDocument()
    expect(screen.getByText('добавлено строк: 1')).toBeInTheDocument()
    expect(screen.getByText('удалено строк: 1')).toBeInTheDocument()

    expect(mocks.getRevision).toHaveBeenCalledWith('profile-primary', 'revision-new')
    expect(mocks.getRevision).toHaveBeenCalledWith('profile-primary', 'revision-old')
  })
})
