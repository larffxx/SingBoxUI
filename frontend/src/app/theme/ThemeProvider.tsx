/**
 * Theme handling (spec §49, §50).
 *
 * `tailwind.config.ts` uses the `class` strategy, so the single source of truth
 * is a `dark` class on <html>. This provider mirrors the persisted preference
 * onto the document and offers one writer used by both the top bar switch and
 * the settings screen. The shared helpers and the context live in `./theme`, so
 * this module exports the component only.
 */
import * as React from 'react'

import { useSettingsQuery, useUpdateSettingsMutation } from '@/app/queries'

import {
  applyThemeClass,
  normaliseTheme,
  resolveTheme,
  systemPrefersDark,
  ThemeContext,
  type ThemeContextValue,
  type ThemePreference,
} from './theme'

export function ThemeProvider({ children }: { children: React.ReactNode }): React.ReactElement {
  const settings = useSettingsQuery()
  const update = useUpdateSettingsMutation()
  const preference = normaliseTheme(settings.data?.state.values.theme)
  const [prefersDark, setPrefersDark] = React.useState<boolean>(() => systemPrefersDark())

  React.useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (event: MediaQueryListEvent): void => setPrefersDark(event.matches)
    setPrefersDark(media.matches)
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [])

  const resolved = resolveTheme(preference, prefersDark)

  React.useEffect(() => {
    applyThemeClass(resolved)
  }, [resolved])

  const { mutate } = update
  const setTheme = React.useCallback(
    (next: ThemePreference) => {
      mutate({ theme: next })
    },
    [mutate],
  )

  const value = React.useMemo<ThemeContextValue>(
    () => ({
      preference,
      resolved,
      setTheme,
      isSaving: update.isPending,
      isAvailable: settings.isSuccess,
    }),
    [preference, resolved, setTheme, update.isPending, settings.isSuccess],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
