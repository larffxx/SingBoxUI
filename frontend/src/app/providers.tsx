/**
 * Application providers.
 *
 * Ordering matters: the query client is outermost (events, theme and screens all
 * read it), then the notice and log buffers that the event bridge writes into,
 * then the theme provider, and finally the render-error boundary around the
 * router.
 */
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import * as React from 'react'

import { AppErrorBoundary } from '@/app/errors/AppErrorBoundary'
import { AppEventBridge } from '@/app/EventBridge'
import { LogBufferProvider } from '@/app/logs/LogBufferProvider'
import { NoticeProvider } from '@/app/notices/NoticeProvider'
import { createAppQueryClient } from '@/app/queryClient'
import { ThemeProvider } from '@/app/theme/ThemeProvider'
import { TooltipProvider } from '@/shared/ui'

export function AppProviders({
  children,
  queryClient,
}: {
  children: React.ReactNode
  /** Injected by tests; production builds one client for the whole app. */
  queryClient?: QueryClient
}): React.ReactElement {
  const [client] = React.useState<QueryClient>(() => queryClient ?? createAppQueryClient())

  return (
    <QueryClientProvider client={client}>
      <TooltipProvider delayDuration={200}>
        <NoticeProvider>
          <LogBufferProvider>
            <ThemeProvider>
              <AppEventBridge />
              <AppErrorBoundary>{children}</AppErrorBoundary>
            </ThemeProvider>
          </LogBufferProvider>
        </NoticeProvider>
      </TooltipProvider>
    </QueryClientProvider>
  )
}
