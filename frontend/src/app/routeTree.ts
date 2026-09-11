/**
 * Router graph.
 *
 * Route-object style (no file-based codegen): one flat tree whose leaves are the
 * screen exports. The profile and binary screens are owned by other workstreams;
 * this module only wires their exported route components in — it never imports
 * their internals and never keeps a copy of them. The tree and the factory live
 * here so `router.tsx` exports the view component only.
 */
import {
  createRootRoute,
  createRoute,
  createRouter,
  type RouterHistory,
} from '@tanstack/react-router'

import { AppShell } from '@/app/shell/AppShell'
import { AboutRoute } from '@/features/about/AboutRoute'
import { BinaryRoute } from '@/features/binary'
import { DashboardRoute } from '@/features/dashboard/DashboardRoute'
import { ProfilesRoute, ProfileWorkspaceRoute } from '@/features/profiles'
import { RuntimeRoute } from '@/features/runtime/RuntimeRoute'
import { SettingsRoute } from '@/features/settings/SettingsRoute'

const rootRoute = createRootRoute({ component: AppShell })

export const appRouteTree = rootRoute.addChildren([
  createRoute({ getParentRoute: () => rootRoute, path: '/', component: DashboardRoute }),
  createRoute({ getParentRoute: () => rootRoute, path: '/profiles', component: ProfilesRoute }),
  createRoute({
    getParentRoute: () => rootRoute,
    path: '/profiles/$profileId',
    component: ProfileWorkspaceRoute,
  }),
  createRoute({ getParentRoute: () => rootRoute, path: '/runtime', component: RuntimeRoute }),
  createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: SettingsRoute }),
  createRoute({ getParentRoute: () => rootRoute, path: '/binary', component: BinaryRoute }),
  createRoute({ getParentRoute: () => rootRoute, path: '/about', component: AboutRoute }),
])

/** Builds a fresh router; tests may pass their own history (e.g. memory). */
export function createAppRouter(history?: RouterHistory) {
  return createRouter({
    routeTree: appRouteTree,
    defaultPreload: 'intent',
    ...(history ? { history } : {}),
  })
}

export type AppRouter = ReturnType<typeof createAppRouter>

declare module '@tanstack/react-router' {
  interface Register {
    router: AppRouter
  }
}
