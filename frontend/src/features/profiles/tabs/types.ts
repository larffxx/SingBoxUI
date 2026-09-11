/** Props shared by every document tab of the profile workspace (spec §35). */
import type { JsonObject } from '../jsonDoc'

export interface DocTabProps {
  /** The whole configuration document — the single source of truth. */
  root: JsonObject
  /** Reports the next document; the caller re-serialises it to JSON text. */
  onChange: (next: JsonObject) => void
}
