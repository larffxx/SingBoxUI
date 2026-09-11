/**
 * Settings schema tests (spec §50).
 *
 * The schema mirrors `settings.UpdateInput`, so these cases are exactly the
 * documents the form must refuse before `settingsApi.update()` is reached.
 */
import { describe, expect, it } from 'vitest'

import { DEFAULT_SETTINGS_VALUES, isAbsolutePath, validateSettings } from './schema'

const valid = { ...DEFAULT_SETTINGS_VALUES }

describe('settings schema', () => {
  it('accepts the default document', () => {
    expect(validateSettings(valid)).toEqual({ ok: true })
  })

  it('rejects values the backend does not know', () => {
    const result = validateSettings({ ...valid, theme: 'solarized' as never })
    expect(result.ok).toBe(false)
    if (!result.ok) expect(result.issues.theme).toBe('Выберите тему оформления')
  })

  it('rejects an unknown log level and binary source', () => {
    const logLevel = validateSettings({ ...valid, logLevel: 'verbose' as never })
    const source = validateSettings({ ...valid, binarySource: 'download' as never })
    expect(logLevel.ok).toBe(false)
    expect(source.ok).toBe(false)
    if (!logLevel.ok) expect(logLevel.issues.logLevel).toBe('Выберите уровень журнала')
    if (!source.ok) expect(source.issues.binarySource).toBe('Выберите источник компонента')
  })

  it('requires a path when the custom source is selected', () => {
    const result = validateSettings({ ...valid, binarySource: 'custom', customBinaryPath: '' })
    expect(result.ok).toBe(false)
    if (!result.ok)
      expect(result.issues.customBinaryPath).toBe('Укажите путь к исполняемому файлу sing-box')
  })

  it('rejects a relative path and accepts absolute POSIX and Windows paths', () => {
    const relative = validateSettings({ ...valid, customBinaryPath: './sing-box' })
    expect(relative.ok).toBe(false)
    if (!relative.ok) {
      expect(relative.issues.customBinaryPath).toBe(
        'Путь должен быть абсолютным (/usr/local/bin/sing-box)',
      )
    }

    expect(validateSettings({ ...valid, customBinaryPath: '/usr/local/bin/sing-box' }).ok).toBe(
      true,
    )
    expect(validateSettings({ ...valid, customBinaryPath: 'C:\\sing-box\\sing-box.exe' }).ok).toBe(
      true,
    )
  })

  it('ignores surrounding whitespace in the path', () => {
    expect(validateSettings({ ...valid, customBinaryPath: '  /usr/local/bin/sing-box  ' }).ok).toBe(
      true,
    )
  })
})

describe('isAbsolutePath', () => {
  it('recognises POSIX and Windows absolute paths', () => {
    expect(isAbsolutePath('/usr/local/bin/sing-box')).toBe(true)
    expect(isAbsolutePath('C:\\sing-box\\sing-box.exe')).toBe(true)
    expect(isAbsolutePath('sing-box')).toBe(false)
    expect(isAbsolutePath('../sing-box')).toBe(false)
  })
})
