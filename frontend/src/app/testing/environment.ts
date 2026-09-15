/**
 * jsdom bootstrap shared by the app-shell tests.
 *
 * Importing this module (for its side effects, once per test file) closes the
 * three gaps between jsdom and the browser the shell actually runs in:
 *
 * - Radix measures its switch/select internals with `ResizeObserver`, which
 *   jsdom does not implement. The bare `ReferenceError` used to take the whole
 *   `/settings` route down inside the router's catch boundary.
 * - TanStack Router restores scroll on every navigation and jsdom logs the
 *   missing `window.scrollTo` implementation each time.
 * - React Testing Library only registers its automatic cleanup when Vitest
 *   exposes the globals API, and this project does not enable `globals`. Without
 *   an explicit unmount the next `render()` piles a second shell onto the same
 *   document, so `getBy*` starts throwing "found multiple elements".
 */
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

/** Minimal stand-in: Radix only needs the observer to exist and be callable. */
class ResizeObserverStub {
  observe(): void {
    return undefined
  }

  unobserve(): void {
    return undefined
  }

  disconnect(): void {
    return undefined
  }
}

if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
}

/**
 * Radix's select/switch read pointer capture and scroll the active option into
 * view; jsdom implements neither, and the resulting `TypeError` aborts the
 * click before the popup opens.
 */
Element.prototype.hasPointerCapture = (): boolean => false
Element.prototype.setPointerCapture = (): void => undefined
Element.prototype.releasePointerCapture = (): void => undefined
Element.prototype.scrollIntoView = (): void => undefined

window.scrollTo = (() => undefined) as typeof window.scrollTo

afterEach(() => {
  cleanup()
})
