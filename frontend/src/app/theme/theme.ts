/**
 * Theme primitives (spec §49, §50).
 *
 * `tailwind.config.ts` uses the `class` strategy, so the single source of truth
 * is a `dark` class on <html>. The persisted preference lives in settings — the
 * provider mirrors it onto the document. Everything here is React-state free so
 * the provider, the settings screen and tests can share one definition.
 */
import { createContext, useContext } from 'react'

export type ThemePreference = 'system' | 'light' | 'dark'

export interface ThemeOption {
  value: ThemePreference
  label: string
}

export const THEME_OPTIONS: readonly ThemeOption[] = [
  { value: 'system', label: 'Как в системе' },
  { value: 'light', label: 'Светлая' },
  { value: 'dark', label: 'Тёмная' },
]

/** Coerces an arbitrary settings value into a known preference. */
export function normaliseTheme(value: string | undefined): ThemePreference {
  return value === 'light' || value === 'dark' ? value : 'system'
}

/** Resolves the preference against the OS setting into a concrete palette. */
export function resolveTheme(preference: ThemePreference, prefersDark: boolean): 'dark' | 'light' {
  if (preference === 'system') return prefersDark ? 'dark' : 'light'
  return preference
}

/** Writes the resolved palette onto <html>. */
export function applyThemeClass(resolved: 'dark' | 'light'): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.classList.toggle('dark', resolved === 'dark')
  root.style.colorScheme = resolved
}

/** Reads the OS preference, defaulting to light when matchMedia is missing. */
export function systemPrefersDark(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

export interface ThemeContextValue {
  preference: ThemePreference
  resolved: 'dark' | 'light'
  setTheme: (preference: ThemePreference) => void
  isSaving: boolean
  isAvailable: boolean
}

export const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('useTheme must be used inside a ThemeProvider')
  return value
}
