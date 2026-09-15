/**
 * Navigation model.
 *
 * The five top-level destinations from spec §52, plus the matcher that derives
 * the active item from the router pathname. Kept apart from `Sidebar.tsx` so the
 * component module exports only the component and the matcher stays unit-tested
 * on its own.
 */
import { Activity, Info, LayoutDashboard, Layers, Settings2 } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

/** The five top-level destinations, as literal route paths for `Link`. */
export type NavPath = '/' | '/profiles' | '/runtime' | '/settings' | '/about'

interface NavItem {
  to: NavPath
  label: string
  icon: LucideIcon
  /** True for `/`, which must not match every path. */
  exact: boolean
}

export const NAV_ITEMS: readonly NavItem[] = [
  { to: '/', label: 'Обзор', icon: LayoutDashboard, exact: true },
  { to: '/profiles', label: 'Профили', icon: Layers, exact: false },
  { to: '/runtime', label: 'Рантайм', icon: Activity, exact: false },
  { to: '/settings', label: 'Настройки', icon: Settings2, exact: false },
  { to: '/about', label: 'О программе', icon: Info, exact: false },
]

/** Whether `pathname` belongs to the nav item's section. */
export function isNavItemActive(item: Pick<NavItem, 'to' | 'exact'>, pathname: string): boolean {
  if (item.exact) return pathname === item.to
  return pathname === item.to || pathname.startsWith(`${item.to}/`)
}
