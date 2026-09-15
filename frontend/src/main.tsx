/**
 * Webview bootstrap.
 *
 * Everything the application needs at runtime lives inside `App`: providers,
 * router and shell. This file only locates the mount point, imports the global
 * stylesheet and hands control over.
 */
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from '@/App'

import './index.css'

const container = document.getElementById('root')
if (!container) {
  throw new Error('Не найден элемент #root для монтирования приложения')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
