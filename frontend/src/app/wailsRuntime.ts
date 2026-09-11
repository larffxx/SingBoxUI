/**
 * Wails runtime event bridge.
 *
 * The Go side pushes state to the UI through Wails events (spec §46, §47) and
 * never through HTTP. `wailsjs/runtime` exposes `EventsOn`, but importing that
 * module from the app layer would pull the generated wrapper into every test;
 * the wrapper is a thin indirection over the `window.runtime` object Wails
 * injects into the webview. Reading that global keeps the transport boundary
 * where it belongs (`src/shared/api`) while still letting the shell subscribe
 * and letting tests install a fake runtime.
 */

/** Minimal typed surface of the injected runtime that the app actually uses. */
export interface WailsEventBridge {
  EventsOn(eventName: string, callback: (...data: unknown[]) => void): unknown
  EventsOff(eventName: string, ...additionalEventNames: string[]): void
}

type RuntimeGlobal = Window & { runtime?: unknown }

const NOOP = (): void => undefined

/** Returns the Wails event bridge, or `undefined` outside the desktop webview. */
export function eventBridge(): WailsEventBridge | undefined {
  if (typeof window === 'undefined') return undefined
  const candidate = (window as RuntimeGlobal).runtime
  if (typeof candidate !== 'object' || candidate === null) return undefined
  const bridge = candidate as Partial<WailsEventBridge>
  if (typeof bridge.EventsOn !== 'function') return undefined
  return bridge as WailsEventBridge
}

export type EventCallback = (...data: unknown[]) => void

/**
 * Subscribes to an event and always returns an unsubscribe function, whether or
 * not the runtime's `EventsOn` returns one itself.
 */
export function subscribeToEvent(
  name: string,
  callback: EventCallback,
  bridge: WailsEventBridge | undefined = eventBridge(),
): () => void {
  if (!bridge) return NOOP
  const unsubscribe: unknown = bridge.EventsOn(name, callback)
  return () => {
    if (typeof unsubscribe === 'function') {
      ;(unsubscribe as () => void)()
      return
    }
    bridge.EventsOff(name)
  }
}
