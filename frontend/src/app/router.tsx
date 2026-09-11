/**
 * Router view.
 *
 * The route tree and the router factory live in `./routeTree`; this module only
 * owns the React-side provider, so it exports the view component only.
 */
import { RouterProvider } from '@tanstack/react-router'
import * as React from 'react'

import { createAppRouter, type AppRouter } from './routeTree'

export function AppRouterView({ router }: { router?: AppRouter }): React.ReactElement {
  const [instance] = React.useState<AppRouter>(() => router ?? createAppRouter())
  return <RouterProvider router={instance} />
}
