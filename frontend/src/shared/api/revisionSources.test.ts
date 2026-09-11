/**
 * Revision sources (spec §12).
 *
 * The list is duplicated on purpose: this is the record of the contract, so a
 * source added here without a matching entry in `internal/domain/profile` — or
 * the other way round — shows up as a failing test instead of as a save that
 * fails in the user's hands.
 */
import { describe, expect, it } from 'vitest'

import {
  REVISION_SOURCES,
  REVISION_SOURCE_LABELS,
  REVISION_SOURCE_MANUAL,
  isRevisionSource,
  revisionSourceLabel,
} from './revisionSources'

/** The sources the specification enumerates, in its order. */
const SPEC_SOURCES = ['manual', 'import', 'share-import', 'template', 'rollback', 'migration']

describe('revision sources', () => {
  it('lists exactly the sources the specification enumerates', () => {
    expect([...REVISION_SOURCES]).toEqual(SPEC_SOURCES)
  })

  it('accepts every listed source and refuses anything else', () => {
    for (const source of SPEC_SOURCES) {
      expect(isRevisionSource(source), `${source} must be accepted`).toBe(true)
    }

    // `editor` is the value that reached the backend before this module existed:
    // it is not a source, and the save failed with INVALID_ARGUMENT.
    for (const other of ['editor', '', 'Manual', 'manual ', 'generated']) {
      expect(isRevisionSource(other), `${other} must be refused`).toBe(false)
    }
  })

  it('saves a hand-written revision as manual', () => {
    expect(REVISION_SOURCE_MANUAL).toBe('manual')
    expect(isRevisionSource(REVISION_SOURCE_MANUAL)).toBe(true)
  })

  it('labels every known source and passes unknown ones through unchanged', () => {
    expect(Object.keys(REVISION_SOURCE_LABELS).sort()).toEqual([...SPEC_SOURCES].sort())
    expect(revisionSourceLabel('manual')).toBe('вручную')
    expect(revisionSourceLabel('rollback')).toBe('откат')
    // A row written by an older build keeps its raw value rather than showing
    // nothing at all.
    expect(revisionSourceLabel('legacy')).toBe('legacy')
  })
})
