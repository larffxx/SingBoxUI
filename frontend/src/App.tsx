/**
 * Application entry component.
 *
 * `App` is the whole application: the provider stack (query client, notices, log
 * buffer, event bridge, theme) wrapped around the router. `main.tsx` mounts it
 * and tests render it directly, so both paths exercise the same tree.
 */
import * as React from 'react'

import { AppProviders } from '@/app/providers'
import { AppRouterView } from '@/app/router'

export function App(): React.ReactElement {
  return (
    <AppProviders>
      <AppRouterView />
    </AppProviders>
  )
}

export default App
